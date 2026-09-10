package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const testPublicImageTaskResultArtifactKey = "image-result"

func setupSignedImageTaskResultTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	oldDiskCacheConfig := common.GetDiskCacheConfig()
	common.SetDiskCacheConfig(common.DiskCacheConfig{Enabled: true, Path: t.TempDir()})
	t.Cleanup(func() { common.SetDiskCacheConfig(oldDiskCacheConfig) })
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Task{}))

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldRedisEnabled := common.RedisEnabled
	oldSecret := common.CryptoSecret
	oldPublicAddress := system_setting.TaskPublicAddress
	oldServerAddress := system_setting.ServerAddress
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	common.CryptoSecret = "signed-image-result-test-secret"
	system_setting.TaskPublicAddress = "https://media.example/gateway/prefix/"
	system_setting.ServerAddress = "https://server.example/"
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.RedisEnabled = oldRedisEnabled
		common.CryptoSecret = oldSecret
		system_setting.TaskPublicAddress = oldPublicAddress
		system_setting.ServerAddress = oldServerAddress
		_ = sqlDB.Close()
	})
	return db
}

func newSignedImageTaskResultFixture(t *testing.T, taskID string, userID int, public bool, platform constant.TaskPlatform) *model.Task {
	t.Helper()
	now := time.Now().Unix()
	result := []byte(`{"data":[{"b64_json":"c3RvcmVkLXJlc3VsdA=="}]}`)
	path, err := common.WriteImageTaskResultCacheFile(result)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(path) })
	sum := sha256.Sum256(result)
	task := &model.Task{
		TaskID:                  taskID,
		Platform:                platform,
		UserId:                  userID,
		Status:                  model.TaskStatusSuccess,
		SettlementStatus:        model.TaskSettlementStatusSettled,
		FinishTime:              now - 10,
		ResultExpiresAt:         now + 3600,
		PublicImageTask:         public,
		PublicImageTaskTokenID:  80,
		ImageTaskResultStored:   true,
		ImageTaskResultStoredAt: now - 10,
		PrivateData: model.TaskPrivateData{
			PublicImageTask:   public,
			TokenId:           80,
			ResultBodyPath:    path,
			ResultBodySize:    int64(len(result)),
			ResultBodySHA256:  hex.EncodeToString(sum[:]),
			ResultContentType: "application/json",
			ResultStoredAt:    now - 10,
			ResultExpiresAt:   now + 3600,
		},
		Data: []byte(`{"_newapi_result_file":true}`),
	}
	require.NoError(t, task.Insert())
	return task
}

func TestGetPublicImageTaskResultServesStoredResultWithSignedAccess(t *testing.T) {
	setupSignedImageTaskResultTestDB(t)
	gin.SetMode(gin.TestMode)
	require.NoError(t, model.DB.Create(&model.User{
		Id:       801,
		Username: "signed-result-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)
	task := newSignedImageTaskResultFixture(t, "signed-result-task", 801, true, constant.TaskPlatformImage)
	access, err := service.IssueTaskArtifactAccess(task.TaskID, testPublicImageTaskResultArtifactKey)
	require.NoError(t, err)

	engine := gin.New()
	engine.GET("/v1/image-tasks/:task_id/result", GetPublicImageTaskResult)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/image-tasks/"+url.PathEscape(task.TaskID)+"/result?access="+url.QueryEscape(access), nil)
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.JSONEq(t, `{"data":[{"b64_json":"c3RvcmVkLXJlc3VsdA=="}]}`, recorder.Body.String())
}

func TestGetPublicImageTaskResultServesStoredResultWithRedactedSignedAccess(t *testing.T) {
	setupSignedImageTaskResultTestDB(t)
	gin.SetMode(gin.TestMode)
	require.NoError(t, model.DB.Create(&model.User{
		Id:       802,
		Username: "signed-result-redacted-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)
	task := newSignedImageTaskResultFixture(t, "signed-result-redacted-task", 802, true, constant.TaskPlatformImage)
	access, err := service.IssueTaskArtifactAccess(task.TaskID, testPublicImageTaskResultArtifactKey)
	require.NoError(t, err)

	var capturedRequestURI string
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Next()
		capturedRequestURI = c.Request.RequestURI
	})
	engine.GET("/v1/image-tasks/:task_id/result",
		middleware.RedactTaskArtifactAccessQuery(),
		middleware.TokenAuthForImageTaskResultAccess(),
		middleware.ImageTaskResultAccessRateLimit(),
		GetPublicImageTaskResult,
	)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/v1/image-tasks/"+url.PathEscape(task.TaskID)+"/result?access="+url.QueryEscape(access),
		nil,
	)
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.JSONEq(t, `{"data":[{"b64_json":"c3RvcmVkLXJlc3VsdA=="}]}`, recorder.Body.String())
	assert.NotContains(t, capturedRequestURI, "access=")
}

func TestGetPublicImageTaskResultRejectsTamperedAccessBeforeTaskLookup(t *testing.T) {
	oldDB := model.DB
	model.DB = nil
	t.Cleanup(func() { model.DB = oldDB })
	oldSecret := common.CryptoSecret
	common.CryptoSecret = "signed-image-result-test-secret"
	t.Cleanup(func() { common.CryptoSecret = oldSecret })

	access, err := service.IssueTaskArtifactAccess("known-task", testPublicImageTaskResultArtifactKey)
	require.NoError(t, err)
	tampered := "x" + access[1:]
	engine := gin.New()
	engine.GET("/v1/image-tasks/:task_id/result", GetPublicImageTaskResult)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/image-tasks/known-task/result?access="+url.QueryEscape(tampered), nil)
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusNotFound, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "database")
}

func TestGetPublicImageTaskResultRejectsPrivateNonImageAndDisabledOwner(t *testing.T) {
	db := setupSignedImageTaskResultTestDB(t)
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name       string
		taskID     string
		userID     int
		public     bool
		platform   constant.TaskPlatform
		userStatus int
	}{
		{name: "private", taskID: "private-result-task", userID: 802, public: false, platform: constant.TaskPlatformImage, userStatus: common.UserStatusEnabled},
		{name: "non-image", taskID: "non-image-result-task", userID: 803, public: true, platform: constant.TaskPlatform("video"), userStatus: common.UserStatusEnabled},
		{name: "disabled-owner", taskID: "disabled-owner-result-task", userID: 804, public: true, platform: constant.TaskPlatformImage, userStatus: common.UserStatusDisabled},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, db.Create(&model.User{
				Id: test.userID, Username: test.name + "-owner", Password: "password123", Status: test.userStatus,
			}).Error)
			task := newSignedImageTaskResultFixture(t, test.taskID, test.userID, test.public, test.platform)
			access, err := service.IssueTaskArtifactAccess(task.TaskID, testPublicImageTaskResultArtifactKey)
			require.NoError(t, err)

			engine := gin.New()
			engine.GET("/v1/image-tasks/:task_id/result", GetPublicImageTaskResult)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/v1/image-tasks/"+task.TaskID+"/result?access="+url.QueryEscape(access), nil)
			engine.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusNotFound, recorder.Code, recorder.Body.String())
		})
	}
}

func TestPublicImageTaskResponseIncludesSignedResultURLOnlyWhenAvailable(t *testing.T) {
	oldSecret := common.CryptoSecret
	oldPublicAddress := system_setting.TaskPublicAddress
	oldServerAddress := system_setting.ServerAddress
	common.CryptoSecret = "signed-image-result-test-secret"
	system_setting.TaskPublicAddress = "https://media.example/gateway/prefix/"
	system_setting.ServerAddress = "https://server.example/"
	t.Cleanup(func() {
		common.CryptoSecret = oldSecret
		system_setting.TaskPublicAddress = oldPublicAddress
		system_setting.ServerAddress = oldServerAddress
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "url-result-task",
		Platform:         constant.TaskPlatformImage,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusSettled,
		FinishTime:       now,
		ResultExpiresAt:  now + 3600,
	}
	task.SetData(map[string]any{"data": []any{map[string]any{"b64_json": "aGVsbG8="}}})

	response := publicImageTaskResponse(task, now)
	require.NotEmpty(t, response.ResultURL)
	parsed, err := url.Parse(response.ResultURL)
	require.NoError(t, err)
	assert.Equal(t, "/gateway/prefix/v1/image-tasks/url-result-task/result", parsed.Path)
	assert.True(t, service.VerifyTaskArtifactAccess(parsed.Query().Get(service.TaskArtifactAccessQueryParameter), task.TaskID, testPublicImageTaskResultArtifactKey))

	system_setting.TaskPublicAddress = ""
	response = publicImageTaskResponse(task, now)
	assert.Contains(t, response.ResultURL, "https://server.example/")
}
