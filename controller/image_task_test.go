package controller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupImageTaskControllerTestDB(t *testing.T) (*gorm.DB, func()) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	oldDB := model.DB
	model.DB = db
	return db, func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	}
}

func setupImageTaskSyncBridgeE2E(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	tempRoot := t.TempDir()
	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldSQLitePath := common.SQLitePath
	oldMainDatabaseType := common.MainDatabaseType()
	oldLogDatabaseType := common.LogDatabaseType()
	oldIsMasterNode := common.IsMasterNode
	oldRedisEnabled := common.RedisEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	oldLogConsumeEnabled := common.LogConsumeEnabled
	oldDiskCacheConfig := common.GetDiskCacheConfig()
	oldUpdateTask := constant.UpdateTask
	oldImageTaskWorkerEnabled := constant.ImageTaskWorkerEnabled
	oldRunImageTasks := service.RunImageTasksFunc
	oldCountToken := constant.CountToken
	oldImageTaskRequestBodyBase64MaxMB := constant.ImageTaskRequestBodyBase64MaxMB
	oldImageTaskFileCacheShared := constant.ImageTaskFileCacheShared
	oldImageTaskFileCacheSharedTrusted := constant.ImageTaskFileCacheSharedTrusted
	oldImageTaskLocalFileCacheAffinity := constant.ImageTaskLocalFileCacheAffinity
	oldImageTaskSharedCacheDisabled := common.ImageTaskSharedCacheDisabled()
	oldSensitiveEnabled := setting.CheckSensitiveEnabled
	oldSensitivePromptEnabled := setting.CheckSensitiveOnPromptEnabled
	oldModelRateLimitEnabled := setting.ModelRequestRateLimitEnabled
	oldSQLDSN, hadSQLDSN := os.LookupEnv("SQL_DSN")
	oldLogSQLDSN, hadLogSQLDSN := os.LookupEnv("LOG_SQL_DSN")
	oldStartupTimeout, hadStartupTimeout := os.LookupEnv("DB_STARTUP_CONNECT_TIMEOUT_SECONDS")
	oldStartupInterval, hadStartupInterval := os.LookupEnv("DB_STARTUP_CONNECT_RETRY_INTERVAL_MS")

	restoreEnv := func(key string, value string, hadValue bool) {
		if hadValue {
			_ = os.Setenv(key, value)
			return
		}
		_ = os.Unsetenv(key)
	}

	_ = os.Unsetenv("SQL_DSN")
	_ = os.Unsetenv("LOG_SQL_DSN")
	_ = os.Setenv("DB_STARTUP_CONNECT_TIMEOUT_SECONDS", "1")
	_ = os.Setenv("DB_STARTUP_CONNECT_RETRY_INTERVAL_MS", "10")
	common.SQLitePath = filepath.Join(tempRoot, "newapi-test.db") + "?_busy_timeout=30000"
	common.IsMasterNode = true
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = false
	common.SetDiskCacheConfig(common.DiskCacheConfig{
		Path:        filepath.Join(tempRoot, "cache"),
		ThresholdMB: 1,
		MaxSizeMB:   1024,
	})
	common.SetImageTaskSharedCacheDisabled(false)
	constant.UpdateTask = true
	constant.ImageTaskWorkerEnabled = true
	constant.CountToken = false
	constant.ImageTaskRequestBodyBase64MaxMB = 16
	constant.ImageTaskFileCacheShared = false
	constant.ImageTaskFileCacheSharedTrusted = false
	constant.ImageTaskLocalFileCacheAffinity = false
	service.RunImageTasksFunc = func(context.Context, []*model.Task) error { return nil }
	setting.CheckSensitiveEnabled = false
	setting.CheckSensitiveOnPromptEnabled = false
	setting.ModelRequestRateLimitEnabled = false
	ratio_setting.InitRatioSettings()

	require.NoError(t, model.InitDB())
	require.NoError(t, model.InitLogDB())
	testDB := model.DB
	testLogDB := model.LOG_DB
	t.Cleanup(func() {
		closeGormDB(testLogDB, testDB)
		closeGormDB(testDB, nil)
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.SQLitePath = oldSQLitePath
		common.SetDatabaseTypes(oldMainDatabaseType, oldLogDatabaseType)
		common.IsMasterNode = oldIsMasterNode
		common.RedisEnabled = oldRedisEnabled
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
		common.LogConsumeEnabled = oldLogConsumeEnabled
		common.SetDiskCacheConfig(oldDiskCacheConfig)
		common.SetImageTaskSharedCacheDisabled(oldImageTaskSharedCacheDisabled)
		constant.UpdateTask = oldUpdateTask
		constant.ImageTaskWorkerEnabled = oldImageTaskWorkerEnabled
		constant.CountToken = oldCountToken
		constant.ImageTaskRequestBodyBase64MaxMB = oldImageTaskRequestBodyBase64MaxMB
		constant.ImageTaskFileCacheShared = oldImageTaskFileCacheShared
		constant.ImageTaskFileCacheSharedTrusted = oldImageTaskFileCacheSharedTrusted
		constant.ImageTaskLocalFileCacheAffinity = oldImageTaskLocalFileCacheAffinity
		service.RunImageTasksFunc = oldRunImageTasks
		setting.CheckSensitiveEnabled = oldSensitiveEnabled
		setting.CheckSensitiveOnPromptEnabled = oldSensitivePromptEnabled
		setting.ModelRequestRateLimitEnabled = oldModelRateLimitEnabled
		restoreEnv("SQL_DSN", oldSQLDSN, hadSQLDSN)
		restoreEnv("LOG_SQL_DSN", oldLogSQLDSN, hadLogSQLDSN)
		restoreEnv("DB_STARTUP_CONNECT_TIMEOUT_SECONDS", oldStartupTimeout, hadStartupTimeout)
		restoreEnv("DB_STARTUP_CONNECT_RETRY_INTERVAL_MS", oldStartupInterval, hadStartupInterval)
	})

	seedImageTaskSyncBridgeE2EData(t)
	return newImageTaskSyncBridgeE2ERouter()
}

func closeGormDB(db *gorm.DB, sameAs *gorm.DB) {
	if db == nil || db == sameAs {
		return
	}
	sqlDB, err := db.DB()
	if err == nil {
		_ = sqlDB.Close()
	}
}

func seedImageTaskSyncBridgeE2EData(t *testing.T) {
	t.Helper()
	priority := int64(0)
	weight := uint(100)
	baseURL := "https://async-task-bridge.example"
	otherSettings, err := common.Marshal(dto.ChannelOtherSettings{
		ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
	})
	require.NoError(t, err)

	require.NoError(t, model.DB.Create(&model.User{
		Id:       101,
		Username: "imageuser",
		Password: "password123",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    1_000_000_000,
	}).Error)
	require.NoError(t, model.DB.Create(&model.Token{
		Id:          201,
		UserId:      101,
		Key:         "testtoken",
		Status:      common.TokenStatusEnabled,
		Name:        "sync-image-token",
		ExpiredTime: -1,
		RemainQuota: 1_000_000_000,
	}).Error)
	require.NoError(t, model.DB.Create(&model.Channel{
		Id:            301,
		Type:          constant.ChannelTypeOpenAI,
		Key:           "upstream-key",
		Status:        common.ChannelStatusEnabled,
		Name:          "async-image-channel",
		Weight:        &weight,
		BaseURL:       &baseURL,
		Models:        "gpt-image-1",
		Group:         "default",
		Priority:      &priority,
		OtherSettings: string(otherSettings),
	}).Error)
	require.NoError(t, model.DB.Create(&model.Ability{
		Group:     "default",
		Model:     "gpt-image-1",
		ChannelId: 301,
		Enabled:   true,
		Priority:  &priority,
		Weight:    weight,
	}).Error)
}

func newImageTaskSyncBridgeE2ERouter() *gin.Engine {
	router := gin.New()
	router.Use(middleware.CORS())
	router.Use(middleware.DecompressRequestMiddleware())
	router.Use(middleware.BodyStorageCleanup())
	router.Use(middleware.StatsMiddleware())

	relayV1Router := router.Group("/v1")
	relayV1Router.Use(middleware.RouteTag("relay"))
	relayV1Router.Use(middleware.SystemPerformanceCheck())
	relayV1Router.Use(middleware.TokenAuth())
	relayV1Router.Use(middleware.ModelRequestRateLimit())

	httpRouter := relayV1Router.Group("")
	httpRouter.Use(middleware.Distribute())
	httpRouter.POST("/images/generations", func(c *gin.Context) {
		Relay(c, types.RelayFormatOpenAIImage)
	})
	httpRouter.POST("/images/edits", func(c *gin.Context) {
		Relay(c, types.RelayFormatOpenAIImage)
	})
	return router
}

func TestValidateImageTaskModeRequestRejectsAsyncTaskBridgeMultipleImages(t *testing.T) {
	n := uint(2)
	err := validateImageTaskModeRequest(&dto.ImageRequest{N: &n}, dto.ImageTaskModeAsyncTaskBridge)
	require.ErrorContains(t, err, "n 大于 1")

	require.NoError(t, validateImageTaskModeRequest(&dto.ImageRequest{N: &n}, dto.ImageTaskModeSyncWrapper))

	one := uint(1)
	require.NoError(t, validateImageTaskModeRequest(&dto.ImageRequest{N: &one}, dto.ImageTaskModeAsyncTaskBridge))
	require.NoError(t, validateImageTaskModeRequest(&dto.ImageRequest{}, dto.ImageTaskModeAsyncTaskBridge))
}

func TestTryRelayImageTaskSyncBridgeSkipsNonAsyncWithoutInitializingChannelMeta(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	common.SetContextKey(ctx, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
		ImageTaskMode: dto.ImageTaskModeSyncWrapper,
	})
	relayInfo := &relaycommon.RelayInfo{}

	handled, err := tryRelayImageTaskSyncBridge(ctx, &dto.ImageRequest{}, relayInfo)

	require.False(t, handled)
	require.Nil(t, err)
	require.Nil(t, relayInfo.ChannelMeta)
}

func TestTryRelayImageTaskSyncBridgeSkipsUnsupportedRelayMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	common.SetContextKey(ctx, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
		ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
	})
	relayInfo := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeEdits}

	handled, err := tryRelayImageTaskSyncBridge(ctx, &dto.ImageRequest{}, relayInfo)

	require.False(t, handled)
	require.Nil(t, err)
	require.Nil(t, relayInfo.ChannelMeta)
}

func TestImageTaskRelayModeRecognizesOpenAIImageEditPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", nil)

	require.Equal(t, relayconstant.RelayModeImagesEdits, imageTaskRelayMode(ctx))
}

func TestRelayImageTaskSyncBridgeRejectsWhenTaskExecutionDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldUpdateTask := constant.UpdateTask
	oldRunImageTasks := service.RunImageTasksFunc
	constant.UpdateTask = false
	service.RunImageTasksFunc = func(context.Context, []*model.Task) error { return nil }
	t.Cleanup(func() {
		constant.UpdateTask = oldUpdateTask
		service.RunImageTasksFunc = oldRunImageTasks
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	err := relayImageTaskSyncBridge(ctx, &dto.ImageRequest{}, &relaycommon.RelayInfo{})

	require.NotNil(t, err)
	require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
}

func TestImageGenerationRouteUsesSyncBridgeForAsyncTaskBridgeChannel(t *testing.T) {
	router := setupImageTaskSyncBridgeE2E(t)
	updateErr := completeFirstImageTaskWhenCreated(constant.TaskActionImageGeneration, json.RawMessage(`{"data":[{"url":"https://example.com/sync-bridge.png"}],"usage":{"total_tokens":7}}`))

	reqCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/images/generations",
		strings.NewReader(`{"model":"gpt-image-1","prompt":"draw a cat","n":1}`),
	).WithContext(reqCtx)
	req.Header.Set("Authorization", "Bearer sk-testtoken")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	require.NoError(t, <-updateErr)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"data":[{"url":"https://example.com/sync-bridge.png"}],"usage":{"total_tokens":7}}`, recorder.Body.String())

	var task model.Task
	require.NoError(t, model.DB.First(&task, "platform = ?", constant.TaskPlatformImage).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	require.Equal(t, model.TaskSettlementStatusSettled, task.SettlementStatus)
	require.Equal(t, constant.TaskActionImageGeneration, task.Action)
	require.Equal(t, dto.ImageTaskModeAsyncTaskBridge, task.PrivateData.ImageTaskMode)
	require.Equal(t, "/v1/images/generations", task.PrivateData.RequestPath)
	require.Equal(t, "application/json", task.PrivateData.RequestContentType)
	require.Equal(t, "gpt-image-1", task.Properties.OriginModelName)
	require.NotEmpty(t, task.ClientTaskID)
	require.NotEmpty(t, task.PrivateData.RequestBodyPath)
	require.NotContains(t, recorder.Body.String(), "task_id")
	require.Equal(t, task.TaskID, recorder.Header().Get("X-NewAPI-Image-Task-ID"))
	require.Equal(t, task.ClientTaskID, recorder.Header().Get("X-NewAPI-Image-Client-Task-ID"))
	require.Empty(t, recorder.Header().Get("X-NewAPI-Retry-Idempotency-Key"))
}

func TestImageEditRouteUsesSyncBridgeForAsyncTaskBridgeChannel(t *testing.T) {
	router := setupImageTaskSyncBridgeE2E(t)
	updateErr := completeFirstImageTaskWhenCreated(constant.TaskActionImageEdit, json.RawMessage(`{"data":[{"url":"https://example.com/edit-sync-bridge.png"}],"usage":{"total_tokens":9}}`))

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-1"))
	require.NoError(t, writer.WriteField("prompt", "edit this image"))
	require.NoError(t, writer.WriteField("n", "1"))
	part, err := writer.CreateFormFile("image", "input.png")
	require.NoError(t, err)
	_, err = part.Write([]byte("fake image"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	reqCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body).WithContext(reqCtx)
	req.Header.Set("Authorization", "Bearer sk-testtoken")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	require.NoError(t, <-updateErr)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"data":[{"url":"https://example.com/edit-sync-bridge.png"}],"usage":{"total_tokens":9}}`, recorder.Body.String())

	var task model.Task
	require.NoError(t, model.DB.First(&task, "platform = ? AND action = ?", constant.TaskPlatformImage, constant.TaskActionImageEdit).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	require.Equal(t, model.TaskSettlementStatusSettled, task.SettlementStatus)
	require.Equal(t, dto.ImageTaskModeAsyncTaskBridge, task.PrivateData.ImageTaskMode)
	require.Equal(t, "/v1/images/edits", task.PrivateData.RequestPath)
	require.Contains(t, task.PrivateData.RequestContentType, "multipart/form-data")
	require.Equal(t, "gpt-image-1", task.Properties.OriginModelName)
	require.NotEmpty(t, task.ClientTaskID)
	require.NotEmpty(t, task.PrivateData.RequestBodyPath)
	require.NotContains(t, recorder.Body.String(), "task_id")
	require.Equal(t, task.TaskID, recorder.Header().Get("X-NewAPI-Image-Task-ID"))
	require.Equal(t, task.ClientTaskID, recorder.Header().Get("X-NewAPI-Image-Client-Task-ID"))
	require.Empty(t, recorder.Header().Get("X-NewAPI-Retry-Idempotency-Key"))
}

func TestImageGenerationRouteRunsAsyncTaskBridgeEndToEnd(t *testing.T) {
	var submitCount int
	var statusOnlyCount int
	var fullResultCount int
	var submitBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/image-tasks/generations":
			submitCount++
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			submitBody = append([]byte(nil), body...)
			_, _ = w.Write([]byte(`{"task_id":"upstream_sync_bridge_e2e","status":"queued"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/image-tasks":
			if r.URL.Query().Get("ids") != "upstream_sync_bridge_e2e" {
				http.Error(w, "unexpected ids", http.StatusBadRequest)
				return
			}
			if r.URL.Query().Get("include_image_data") == "true" {
				fullResultCount++
				_, _ = w.Write([]byte(`{
					"items": [{
						"task_id": "upstream_sync_bridge_e2e",
						"status": "completed",
						"progress": "100%",
						"result": {
							"data": [{"url": "https://example.com/full-chain.png"}],
							"usage": {"total_tokens": 11}
						}
					}]
				}`))
				return
			}
			statusOnlyCount++
			_, _ = w.Write([]byte(`{
				"items": [{
					"task_id": "upstream_sync_bridge_e2e",
					"status": "completed",
					"progress": "100%"
				}]
			}`))
		default:
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer upstream.Close()

	router := setupImageTaskSyncBridgeE2E(t)
	service.RunImageTasksFunc = relay.RunImageTasks
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 301).Update("base_url", upstream.URL).Error)
	workerCtx, workerCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer workerCancel()
	workerDone := driveImageTaskSyncBridgeWorkerUntilSettled(workerCtx)

	reqCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/images/generations",
		strings.NewReader(`{"model":"gpt-image-1","prompt":"draw the full chain","n":1}`),
	).WithContext(reqCtx)
	req.Header.Set("Authorization", "Bearer sk-testtoken")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"data":[{"url":"https://example.com/full-chain.png"}],"usage":{"total_tokens":11}}`, recorder.Body.String())
	require.NoError(t, <-workerDone)
	require.Equal(t, 1, submitCount)
	require.GreaterOrEqual(t, statusOnlyCount, 1)
	require.Equal(t, 1, fullResultCount)

	var submitted map[string]any
	require.NoError(t, json.Unmarshal(submitBody, &submitted))
	require.Equal(t, "gpt-image-1", submitted["model"])
	require.Equal(t, "draw the full chain", submitted["prompt"])
	require.Equal(t, false, submitted["stream"])
	require.NotEmpty(t, submitted["client_task_id"])

	var task model.Task
	require.NoError(t, model.DB.First(&task, "platform = ?", constant.TaskPlatformImage).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	require.Equal(t, model.TaskSettlementStatusSettled, task.SettlementStatus)
	require.Equal(t, "upstream_sync_bridge_e2e", task.PrivateData.UpstreamTaskID)
	require.Equal(t, constant.TaskActionImageGeneration, task.Action)
	require.Equal(t, dto.ImageTaskModeAsyncTaskBridge, task.PrivateData.ImageTaskMode)
}

func TestImageEditRouteRunsAsyncTaskBridgeEndToEnd(t *testing.T) {
	var submitCount int
	var statusOnlyCount int
	var fullResultCount int
	var submitBody []byte
	var submitContentType string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/image-tasks/edits":
			submitCount++
			submitContentType = r.Header.Get("Content-Type")
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			submitBody = append([]byte(nil), body...)
			_, _ = w.Write([]byte(`{"task_id":"upstream_sync_bridge_edit_e2e","status":"queued"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/image-tasks":
			if r.URL.Query().Get("ids") != "upstream_sync_bridge_edit_e2e" {
				http.Error(w, "unexpected ids", http.StatusBadRequest)
				return
			}
			if r.URL.Query().Get("include_image_data") == "true" {
				fullResultCount++
				_, _ = w.Write([]byte(`{
					"items": [{
						"task_id": "upstream_sync_bridge_edit_e2e",
						"status": "completed",
						"progress": "100%",
						"result": {
							"data": [{"url": "https://example.com/full-edit-chain.png"}],
							"usage": {"total_tokens": 13}
						}
					}]
				}`))
				return
			}
			statusOnlyCount++
			_, _ = w.Write([]byte(`{
				"items": [{
					"task_id": "upstream_sync_bridge_edit_e2e",
					"status": "completed",
					"progress": "100%"
				}]
			}`))
		default:
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer upstream.Close()

	router := setupImageTaskSyncBridgeE2E(t)
	service.RunImageTasksFunc = relay.RunImageTasks
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 301).Update("base_url", upstream.URL).Error)
	workerCtx, workerCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer workerCancel()
	workerDone := driveImageTaskSyncBridgeWorkerUntilSettled(workerCtx)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-1"))
	require.NoError(t, writer.WriteField("prompt", "edit the full chain"))
	require.NoError(t, writer.WriteField("n", "1"))
	part, err := writer.CreateFormFile("image", "input.png")
	require.NoError(t, err)
	_, err = part.Write([]byte("fake edit image"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	reqCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body).WithContext(reqCtx)
	req.Header.Set("Authorization", "Bearer sk-testtoken")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"data":[{"url":"https://example.com/full-edit-chain.png"}],"usage":{"total_tokens":13}}`, recorder.Body.String())
	require.NoError(t, <-workerDone)
	require.Equal(t, 1, submitCount)
	require.GreaterOrEqual(t, statusOnlyCount, 1)
	require.Equal(t, 1, fullResultCount)

	mediaType, params, err := mime.ParseMediaType(submitContentType)
	require.NoError(t, err)
	require.Equal(t, "multipart/form-data", mediaType)
	require.NotEmpty(t, params["boundary"])
	fields := make(map[string]string)
	files := make(map[string][]byte)
	reader := multipart.NewReader(bytes.NewReader(submitBody), params["boundary"])
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		data, err := io.ReadAll(part)
		require.NoError(t, err)
		if part.FileName() != "" {
			files[part.FormName()] = data
			continue
		}
		fields[part.FormName()] = string(data)
	}
	require.Equal(t, "gpt-image-1", fields["model"])
	require.Equal(t, "edit the full chain", fields["prompt"])
	require.Equal(t, "1", fields["n"])
	require.Equal(t, "false", fields["stream"])
	require.NotEmpty(t, fields["client_task_id"])
	require.Equal(t, []byte("fake edit image"), files["image"])

	var task model.Task
	require.NoError(t, model.DB.First(&task, "platform = ?", constant.TaskPlatformImage).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	require.Equal(t, model.TaskSettlementStatusSettled, task.SettlementStatus)
	require.Equal(t, "upstream_sync_bridge_edit_e2e", task.PrivateData.UpstreamTaskID)
	require.Equal(t, constant.TaskActionImageEdit, task.Action)
	require.Equal(t, dto.ImageTaskModeAsyncTaskBridge, task.PrivateData.ImageTaskMode)
}

func driveImageTaskSyncBridgeWorkerUntilSettled(ctx context.Context) <-chan error {
	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			now := time.Now().Unix()
			if err := model.DB.Model(&model.Task{}).
				Where("platform = ? AND status != ?", constant.TaskPlatformImage, model.TaskStatusFailure).
				Update("next_poll_at", now).Error; err != nil {
				done <- err
				return
			}
			tasks := model.GetRunnableImageTasks(20, now)
			if len(tasks) > 0 {
				service.DispatchImageTasks(ctx, tasks)
			}
			var settledCount int64
			if err := model.DB.Model(&model.Task{}).
				Where("platform = ? AND status = ? AND settlement_status = ?", constant.TaskPlatformImage, model.TaskStatusSuccess, model.TaskSettlementStatusSettled).
				Count(&settledCount).Error; err != nil {
				done <- err
				return
			}
			if settledCount > 0 {
				done <- nil
				return
			}
			select {
			case <-ctx.Done():
				done <- ctx.Err()
				return
			case <-ticker.C:
			}
		}
	}()
	return done
}

func completeFirstImageTaskWhenCreated(action string, result json.RawMessage) <-chan error {
	updateErr := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		deadline := time.After(3 * time.Second)
		for {
			select {
			case <-deadline:
				updateErr <- fmt.Errorf("image task was not created")
				return
			case <-ticker.C:
				var task model.Task
				err := model.DB.Where("platform = ? AND action = ?", constant.TaskPlatformImage, action).
					First(&task).Error
				if err != nil {
					continue
				}
				updateErr <- model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Updates(map[string]any{
					"status":            model.TaskStatusSuccess,
					"settlement_status": model.TaskSettlementStatusSettled,
					"progress":          "100%",
					"finish_time":       time.Now().Unix(),
					"data":              result,
				}).Error
				return
			}
		}
	}()
	return updateErr
}

func TestWaitImageTaskSyncBridgeResultReturnsAsyncResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, cleanup := setupImageTaskControllerTestDB(t)
	task := &model.Task{
		TaskID:   "task_sync_bridge_success",
		Platform: constant.TaskPlatformImage,
		UserId:   1,
		Status:   model.TaskStatusQueued,
		Progress: "0%",
	}
	require.NoError(t, db.Create(task).Error)
	t.Cleanup(cleanup)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = db.Model(&model.Task{}).Where("id = ?", task.ID).Updates(map[string]any{
			"status":            model.TaskStatusSuccess,
			"settlement_status": model.TaskSettlementStatusSettled,
			"progress":          "100%",
			"data":              json.RawMessage(`{"data":[{"url":"https://example.com/image.png"}]}`),
		}).Error
	}()

	body, err := waitImageTaskSyncBridgeResult(ctx, task)

	require.Nil(t, err)
	require.JSONEq(t, `{"data":[{"url":"https://example.com/image.png"}]}`, string(body))
}

func TestWaitImageTaskSyncBridgeResultPreservesFailedTaskStatusCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, cleanup := setupImageTaskControllerTestDB(t)
	t.Cleanup(cleanup)

	cases := []struct {
		name       string
		reason     string
		statusCode int
	}{
		{name: "timeout", reason: "image generation timed out", statusCode: http.StatusGatewayTimeout},
		{name: "client closed", reason: "client closed request", statusCode: 499},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := &model.Task{
				TaskID:     "task_sync_bridge_failed_" + strings.ReplaceAll(tc.name, " ", "_"),
				Platform:   constant.TaskPlatformImage,
				UserId:     1,
				Status:     model.TaskStatusFailure,
				Progress:   "100%",
				FailReason: tc.reason,
			}
			require.NoError(t, db.Create(task).Error)

			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

			body, apiErr := waitImageTaskSyncBridgeResult(ctx, task)

			require.Empty(t, body)
			require.NotNil(t, apiErr)
			require.Equal(t, tc.statusCode, apiErr.StatusCode)
			require.Contains(t, apiErr.ToOpenAIError().Message, tc.reason)
		})
	}
}

func TestCancelImageTaskSyncBridgeWaitFailsOpenTaskAndRemovesBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, cleanup := setupImageTaskControllerTestDB(t)
	t.Cleanup(cleanup)
	bodyPath, err := common.WriteImageTaskBodyCacheFile([]byte(`{"prompt":"draw"}`))
	require.NoError(t, err)
	task := &model.Task{
		TaskID:   "task_sync_bridge_timeout",
		Platform: constant.TaskPlatformImage,
		UserId:   1,
		Status:   model.TaskStatusQueued,
		Progress: "0%",
		PrivateData: model.TaskPrivateData{
			RequestBodyPath: bodyPath,
		},
	}
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	setImageTaskSyncBridgeTaskHeaders(ctx, task)
	body, apiErr := cancelImageTaskSyncBridgeWait(ctx, task, "image generation timed out")

	require.Empty(t, body)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusGatewayTimeout, apiErr.StatusCode)
	require.Empty(t, recorder.Header().Get("X-NewAPI-Image-Task-ID"))
	require.Empty(t, recorder.Header().Get("X-NewAPI-Image-Client-Task-ID"))
	require.Empty(t, recorder.Header().Get("X-NewAPI-Retry-Idempotency-Key"))
	reloaded, exist, err := model.GetByTaskId(1, task.TaskID)
	require.NoError(t, err)
	require.True(t, exist)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status)
	require.Equal(t, "image generation timed out", reloaded.FailReason)
	require.Empty(t, reloaded.PrivateData.RequestBodyPath)
	_, statErr := os.Stat(bodyPath)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestCancelImageTaskSyncBridgeWaitKeepsSubmittedTaskRunning(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, cleanup := setupImageTaskControllerTestDB(t)
	t.Cleanup(cleanup)
	bodyPath, err := common.WriteImageTaskBodyCacheFile([]byte(`{"prompt":"draw"}`))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove(bodyPath)
	})
	task := &model.Task{
		TaskID:   "task_sync_bridge_submitted",
		Platform: constant.TaskPlatformImage,
		UserId:   1,
		Status:   model.TaskStatusSubmitted,
		Progress: "0%",
		PrivateData: model.TaskPrivateData{
			RequestBodyPath: bodyPath,
			UpstreamTaskID:  "upstream_submitted",
		},
	}
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	body, apiErr := cancelImageTaskSyncBridgeWait(ctx, task, "client closed request")

	require.Empty(t, body)
	require.Nil(t, apiErr)
	require.Equal(t, "task_sync_bridge_submitted", recorder.Header().Get("X-NewAPI-Image-Task-ID"))
	require.Equal(t, "task_sync_bridge_submitted", recorder.Header().Get("X-NewAPI-Image-Client-Task-ID"))
	require.Equal(t, "task_sync_bridge_submitted", recorder.Header().Get("X-NewAPI-Retry-Idempotency-Key"))
	reloaded, exist, err := model.GetByTaskId(1, task.TaskID)
	require.NoError(t, err)
	require.True(t, exist)
	require.Equal(t, model.TaskStatus(model.TaskStatusSubmitted), reloaded.Status)
	require.Equal(t, "upstream_submitted", reloaded.PrivateData.UpstreamTaskID)
	require.Equal(t, bodyPath, reloaded.PrivateData.RequestBodyPath)
	require.FileExists(t, bodyPath)
}

func TestCancelImageTaskSyncBridgeWaitKeepsLeasedQueuedTaskRunning(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, cleanup := setupImageTaskControllerTestDB(t)
	t.Cleanup(cleanup)
	bodyPath, err := common.WriteImageTaskBodyCacheFile([]byte(`{"prompt":"draw"}`))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove(bodyPath)
	})
	task := &model.Task{
		TaskID:    "task_sync_bridge_leased",
		Platform:  constant.TaskPlatformImage,
		UserId:    1,
		Status:    model.TaskStatusQueued,
		Progress:  "0%",
		LockOwner: "worker-owner",
		LockUntil: time.Now().Add(time.Minute).Unix(),
		PrivateData: model.TaskPrivateData{
			RequestBodyPath: bodyPath,
		},
	}
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	body, apiErr := cancelImageTaskSyncBridgeWait(ctx, task, "image generation timed out")

	require.Empty(t, body)
	require.Nil(t, apiErr)
	require.Equal(t, "task_sync_bridge_leased", recorder.Header().Get("X-NewAPI-Image-Task-ID"))
	require.Equal(t, "task_sync_bridge_leased", recorder.Header().Get("X-NewAPI-Image-Client-Task-ID"))
	require.Equal(t, "task_sync_bridge_leased", recorder.Header().Get("X-NewAPI-Retry-Idempotency-Key"))
	reloaded, exist, err := model.GetByTaskId(1, task.TaskID)
	require.NoError(t, err)
	require.True(t, exist)
	require.Equal(t, model.TaskStatus(model.TaskStatusQueued), reloaded.Status)
	require.Equal(t, "worker-owner", reloaded.LockOwner)
	require.Equal(t, bodyPath, reloaded.PrivateData.RequestBodyPath)
	require.FileExists(t, bodyPath)
}

func TestImageTaskSyncBridgeWaitStoppedErrorIncludesRetryHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	task := &model.Task{
		TaskID:       "task_sync_bridge_retry",
		ClientTaskID: "client_retry_123",
		Platform:     constant.TaskPlatformImage,
		UserId:       1,
		Status:       model.TaskStatusSubmitted,
		Progress:     "0%",
	}

	apiErr := imageTaskSyncBridgeWaitStoppedError(ctx, task, "image generation timed out", http.StatusGatewayTimeout)

	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusGatewayTimeout, apiErr.StatusCode)
	require.Contains(t, apiErr.ToOpenAIError().Message, "Idempotency-Key: client_retry_123")
	require.Equal(t, "task_sync_bridge_retry", recorder.Header().Get("X-NewAPI-Image-Task-ID"))
	require.Equal(t, "client_retry_123", recorder.Header().Get("X-NewAPI-Image-Client-Task-ID"))
	require.Equal(t, "client_retry_123", recorder.Header().Get("X-NewAPI-Retry-Idempotency-Key"))
}

func TestImageTaskBillingRequestInputUsesPersistedJSONBody(t *testing.T) {
	persisted := &imageTaskPersistedRequest{
		ContentType: "application/json",
		Body:        []byte(`{"model":"gpt-image-1","stream":false}`),
	}

	input := imageTaskBillingRequestInputFromPersistedRequest(persisted, map[string]string{
		"X-Test":        " trace-123 ",
		"Authorization": "Bearer secret",
	})

	require.NotNil(t, input)
	require.JSONEq(t, `{"model":"gpt-image-1","stream":false}`, string(input.Body))
	require.Equal(t, "trace-123", input.Headers["X-Test"])
	require.NotContains(t, input.Headers, "Authorization")
}

func TestPersistImageTaskRequestWritesRelayModelAndClientTaskID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	ctx.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.CreateBodyStorage([]byte(`{"prompt":"draw a cat","client_task_id":"task_local_123"}`))
	require.NoError(t, err)
	defer storage.Close()
	ctx.Set(common.KeyBodyStorage, storage)

	persisted, err := persistImageTaskRequest(ctx, relayconstant.RelayModeImagesGenerations, "gpt-image-1")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove(persisted.Path)
	})

	require.JSONEq(t, `{"client_task_id":"task_local_123","model":"gpt-image-1","prompt":"draw a cat","stream":false}`, string(persisted.Body))
	require.Equal(t, "task_local_123", persisted.ClientTaskID)
}

func TestPersistImageTaskRequestMultipartWithoutBoundaryFallsBackToJSONForGeneration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	ctx.Request.Header.Set("Content-Type", "multipart/form-data")
	storage, err := common.CreateBodyStorage([]byte(`{"prompt":"draw a cat","client_task_id":"task_json_fallback"}`))
	require.NoError(t, err)
	defer storage.Close()
	ctx.Set(common.KeyBodyStorage, storage)

	persisted, err := persistImageTaskRequest(ctx, relayconstant.RelayModeImagesGenerations, "gpt-image-1")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove(persisted.Path)
	})

	require.JSONEq(t, `{"client_task_id":"task_json_fallback","model":"gpt-image-1","prompt":"draw a cat","stream":false}`, string(persisted.Body))
	require.Equal(t, "task_json_fallback", persisted.ClientTaskID)
}

func TestPersistImageTaskRequestUsesIdempotencyKeyAsClientTaskID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Request.Header.Set("Idempotency-Key", "idem_local_123")
	storage, err := common.CreateBodyStorage([]byte(`{"prompt":"draw a cat"}`))
	require.NoError(t, err)
	defer storage.Close()
	ctx.Set(common.KeyBodyStorage, storage)

	persisted, err := persistImageTaskRequest(ctx, relayconstant.RelayModeImagesGenerations, "gpt-image-1")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove(persisted.Path)
	})

	require.JSONEq(t, `{"model":"gpt-image-1","prompt":"draw a cat","stream":false}`, string(persisted.Body))
	require.Equal(t, "idem_local_123", persisted.ClientTaskID)
}

func TestPersistImageTaskRequestFallsBackToBase64WhenBodyCacheUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldShared := constant.ImageTaskFileCacheShared
	oldTrusted := constant.ImageTaskFileCacheSharedTrusted
	oldAffinity := constant.ImageTaskLocalFileCacheAffinity
	oldBase64MaxMB := constant.ImageTaskRequestBodyBase64MaxMB
	oldDiskCacheConfig := common.GetDiskCacheConfig()
	cacheRootFile, err := os.CreateTemp(t.TempDir(), "cache-root-file")
	require.NoError(t, err)
	require.NoError(t, cacheRootFile.Close())
	brokenCacheConfig := oldDiskCacheConfig
	brokenCacheConfig.Path = cacheRootFile.Name()
	constant.ImageTaskFileCacheShared = false
	constant.ImageTaskFileCacheSharedTrusted = false
	constant.ImageTaskLocalFileCacheAffinity = false
	constant.ImageTaskRequestBodyBase64MaxMB = 16
	common.SetDiskCacheConfig(brokenCacheConfig)
	t.Cleanup(func() {
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskFileCacheSharedTrusted = oldTrusted
		constant.ImageTaskLocalFileCacheAffinity = oldAffinity
		constant.ImageTaskRequestBodyBase64MaxMB = oldBase64MaxMB
		common.SetDiskCacheConfig(oldDiskCacheConfig)
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	ctx.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.CreateBodyStorage([]byte(`{"prompt":"draw","client_task_id":"task_local_fallback"}`))
	require.NoError(t, err)
	defer storage.Close()
	ctx.Set(common.KeyBodyStorage, storage)

	persisted, err := persistImageTaskRequest(ctx, relayconstant.RelayModeImagesGenerations, "gpt-image-1")
	require.NoError(t, err)

	require.Empty(t, persisted.Path)
	require.NotEmpty(t, persisted.Body)
	require.Equal(t, "task_local_fallback", persisted.ClientTaskID)
	value, err := imageTaskRequestBodyBase64ForStorage(persisted)
	require.NoError(t, err)
	require.Equal(t, base64.StdEncoding.EncodeToString(persisted.Body), value)
}

func TestCloneImageTaskBillingRequestInputForStorageDropsBody(t *testing.T) {
	input := imageTaskBillingRequestInputFromPersistedRequest(&imageTaskPersistedRequest{
		ContentType: "application/json",
		Body:        []byte(`{"stream":false}`),
	}, map[string]string{"X-Test": " trace-123 "})

	cloned := cloneImageTaskBillingRequestInputForStorage(input)

	require.NotNil(t, cloned)
	require.Empty(t, cloned.Body)
	require.Equal(t, "trace-123", cloned.Headers["X-Test"])

	bodyOnlyInput := imageTaskBillingRequestInputFromPersistedRequest(&imageTaskPersistedRequest{
		ContentType: "application/json",
		Body:        []byte(`{"stream":false}`),
	}, nil)
	require.Nil(t, cloneImageTaskBillingRequestInputForStorage(bodyOnlyInput))
}

func TestImageTaskRequestBodyBase64ForStorageFollowsFallbackPolicy(t *testing.T) {
	oldShared := constant.ImageTaskFileCacheShared
	oldTrusted := constant.ImageTaskFileCacheSharedTrusted
	oldAffinity := constant.ImageTaskLocalFileCacheAffinity
	oldNode := common.NodeName
	oldSharedDisabled := common.ImageTaskSharedCacheDisabled()
	oldBase64MaxMB := constant.ImageTaskRequestBodyBase64MaxMB
	t.Cleanup(func() {
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskFileCacheSharedTrusted = oldTrusted
		constant.ImageTaskLocalFileCacheAffinity = oldAffinity
		constant.ImageTaskRequestBodyBase64MaxMB = oldBase64MaxMB
		common.NodeName = oldNode
		common.SetImageTaskSharedCacheDisabled(oldSharedDisabled)
	})
	common.SetImageTaskSharedCacheDisabled(false)
	constant.ImageTaskRequestBodyBase64MaxMB = 16

	persisted := &imageTaskPersistedRequest{Body: []byte(`{"model":"gpt-image-1"}`)}
	constant.ImageTaskFileCacheShared = false
	constant.ImageTaskLocalFileCacheAffinity = false
	value, err := imageTaskRequestBodyBase64ForStorage(persisted)
	require.NoError(t, err)
	require.Equal(t, base64.StdEncoding.EncodeToString(persisted.Body), value)

	common.NodeName = "node-a"
	constant.ImageTaskLocalFileCacheAffinity = true
	value, err = imageTaskRequestBodyBase64ForStorage(persisted)
	require.NoError(t, err)
	require.Empty(t, value)

	constant.ImageTaskFileCacheShared = true
	constant.ImageTaskFileCacheSharedTrusted = false
	value, err = imageTaskRequestBodyBase64ForStorage(persisted)
	require.NoError(t, err)
	require.Equal(t, base64.StdEncoding.EncodeToString(persisted.Body), value)

	constant.ImageTaskFileCacheSharedTrusted = true
	value, err = imageTaskRequestBodyBase64ForStorage(persisted)
	require.NoError(t, err)
	require.Empty(t, value)
}

func TestImageTaskRequestBodyBase64ForStorageRejectsOversize(t *testing.T) {
	oldShared := constant.ImageTaskFileCacheShared
	oldAffinity := constant.ImageTaskLocalFileCacheAffinity
	oldBase64MaxMB := constant.ImageTaskRequestBodyBase64MaxMB
	t.Cleanup(func() {
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskLocalFileCacheAffinity = oldAffinity
		constant.ImageTaskRequestBodyBase64MaxMB = oldBase64MaxMB
	})

	constant.ImageTaskFileCacheShared = false
	constant.ImageTaskLocalFileCacheAffinity = false
	constant.ImageTaskRequestBodyBase64MaxMB = 1
	persisted := &imageTaskPersistedRequest{Body: make([]byte, (1<<20)+1)}

	value, err := imageTaskRequestBodyBase64ForStorage(persisted)

	require.ErrorIs(t, err, common.ErrRequestBodyTooLarge)
	require.Empty(t, value)
}

func TestImageTaskStorageNodeForRequestUsesPortableSentinel(t *testing.T) {
	oldNode := common.NodeName
	common.NodeName = "node-a"
	t.Cleanup(func() {
		common.NodeName = oldNode
	})

	require.Equal(t, model.ImageTaskPortableStorageNode, imageTaskStorageNodeForRequest(true))
	require.Equal(t, "node-a", imageTaskStorageNodeForRequest(false))
}

func TestImageTaskResponseResultLoadsStoredResultFile(t *testing.T) {
	result := []byte(`{"data":[{"b64_json":"stored-b64"}],"usage":{"total_tokens":1}}`)
	path, err := common.WriteImageTaskResultCacheFile(result)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove(path)
	})
	sum := sha256.Sum256(result)
	task := &model.Task{
		TaskID:    "task_result_file",
		Status:    model.TaskStatusSuccess,
		Progress:  "100%",
		CreatedAt: 1,
		UpdatedAt: 2,
		PrivateData: model.TaskPrivateData{
			ResultBodyPath:    path,
			ResultBodySize:    int64(len(result)),
			ResultBodySHA256:  hex.EncodeToString(sum[:]),
			ResultContentType: "application/json",
			ResultExpiresAt:   time.Now().Add(time.Hour).Unix(),
		},
		Data: []byte(`{"_newapi_result_file":true}`),
	}

	body, resultErr := imageTaskResponseResult(task)

	require.Empty(t, resultErr)
	require.JSONEq(t, string(result), string(body))
}

func TestImageTaskResponseResultHidesPendingSettlementResult(t *testing.T) {
	result := []byte(`{"data":[{"b64_json":"not-yet-public"}],"usage":{"total_tokens":1}}`)
	task := &model.Task{
		TaskID:           "task_pending_settlement",
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusPending,
		Progress:         "100%",
		Data:             result,
	}

	require.False(t, imageTaskResponseResultVisible(task))
}

func TestImageTaskResponseResultHidesAppliedSettlementResult(t *testing.T) {
	result := []byte(`{"data":[{"b64_json":"applied-not-settled"}],"usage":{"total_tokens":1}}`)
	task := &model.Task{
		TaskID:           "task_applied_settlement",
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusApplied,
		Progress:         "100%",
		Data:             result,
	}

	require.False(t, imageTaskResponseResultVisible(task))
}

func TestImageTaskResponseResultHidesSettlementReviewResult(t *testing.T) {
	task := &model.Task{
		TaskID:           "task_settlement_review",
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusReview,
		Progress:         "100%",
		FailReason:       "image task settlement requires manual review",
		Data:             json.RawMessage(`{"data":[{"url":"https://example.com/image.png"}]}`),
	}

	require.False(t, imageTaskResponseResultVisible(task))
}

func TestImageTaskResponseResultMarksExpiredStoredResultFileExpired(t *testing.T) {
	result := []byte(`{"data":[{"b64_json":"stored-b64"}]}`)
	path, err := common.WriteImageTaskResultCacheFile(result)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove(path)
	})
	sum := sha256.Sum256(result)
	task := &model.Task{
		TaskID: "task_expired_result_file",
		Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			ResultBodyPath:    path,
			ResultBodySize:    int64(len(result)),
			ResultBodySHA256:  hex.EncodeToString(sum[:]),
			ResultContentType: "application/json",
			ResultExpiresAt:   time.Now().Add(-time.Minute).Unix(),
		},
		Data: []byte(`{"_newapi_result_file":true}`),
	}

	body, resultErr := imageTaskResponseResult(task)

	require.Empty(t, body)
	require.Equal(t, imageTaskResultExpiredMessage, resultErr)
}

func TestImageTaskResponseResultMarksMissingStoredResultFileExpired(t *testing.T) {
	task := &model.Task{
		TaskID: "task_missing_result_file",
		Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			ResultBodyPath: "missing-result.json",
		},
		Data: []byte(`{"_newapi_result_file":true}`),
	}

	body, resultErr := imageTaskResponseResult(task)

	require.Empty(t, body)
	require.Equal(t, imageTaskResultExpiredMessage, resultErr)
}

func TestImageTaskResponseResultMarksStoredResultMarkerWithoutPathExpired(t *testing.T) {
	task := &model.Task{
		TaskID: "task_marker_only",
		Status: model.TaskStatusSuccess,
		Data:   []byte(`{"_newapi_result_file":true}`),
	}

	body, resultErr := imageTaskResponseResult(task)

	require.Empty(t, body)
	require.Equal(t, imageTaskResultExpiredMessage, resultErr)
}

func TestImageTaskResponseResultDoesNotTreatMarkerTextAsPlaceholder(t *testing.T) {
	result := []byte(`{"data":[{"revised_prompt":"literal _newapi_result_file text"}]}`)
	task := &model.Task{
		TaskID: "task_marker_text",
		Status: model.TaskStatusSuccess,
		Data:   result,
	}

	body, resultErr := imageTaskResponseResult(task)

	require.JSONEq(t, string(result), string(body))
	require.Empty(t, resultErr)
}
