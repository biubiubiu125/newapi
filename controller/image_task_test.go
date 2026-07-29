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
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
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
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.ImageTaskClientTaskIDLock{}))

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldRedisEnabled := common.RedisEnabled
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	return db, func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.RedisEnabled = oldRedisEnabled
		_ = sqlDB.Close()
	}
}

func TestCloneImageTaskRequestHeadersDropsCredentialHeaders(t *testing.T) {
	headers := cloneImageTaskRequestHeaders(map[string]string{
		"Authorization":      "Bearer auth-secret",
		"Mj-Api-Secret":      "sk-mj-secret",
		"X-Provider-Secret":  "provider-secret",
		"X-Auth-Token":       "auth-token",
		"X-Goog-Api-Key":     "goog-secret",
		"X-Request-Trace-Id": "trace-123",
	})

	require.Equal(t, map[string]string{"X-Request-Trace-Id": "trace-123"}, headers)
}

func TestAcquireImageTaskPersistedRequestClaimsMiddlewareCopy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/image-tasks/generations", strings.NewReader("not valid json"))
	ctx.Request.Header.Set("Content-Type", "application/json")
	original := &imageTaskPersistedRequest{
		Path:         filepath.Join(t.TempDir(), "request.json"),
		ContentType:  "application/json",
		Size:         2,
		Body:         []byte(`{}`),
		ClientTaskID: "client-1",
		Fingerprint:  "fingerprint-1",
	}
	handoff := stageImageTaskPersistedRequest(ctx, original)

	acquired, err := acquireImageTaskPersistedRequest(ctx, relayconstant.RelayModeImagesGenerations, "gpt-image-1")

	require.NoError(t, err)
	require.Same(t, original, acquired)
	require.True(t, handoff.claimed)
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
	return newImageTaskSyncBridgeE2ERouterWithCreateGate(nil)
}

func newImageTaskSyncBridgeE2ERouterWithCreateGate(createGate gin.HandlerFunc) *gin.Engine {
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

	imageTaskRouter := router.Group("/v1/image-tasks")
	imageTaskRouter.Use(middleware.RouteTag("relay"))
	imageTaskRouter.Use(middleware.SystemPerformanceCheck())
	imageTaskCreateRouter := imageTaskRouter.Group("")
	imageTaskCreateRouter.Use(middleware.TokenAuthForImageTaskCreation())
	generationHandlers := []gin.HandlerFunc{
		RequirePublicImageTaskContentType(relayconstant.RelayModeImagesGenerations),
		ReusePublicImageTaskGenerationIfExists,
	}
	if createGate != nil {
		generationHandlers = append(generationHandlers, createGate)
	}
	generationHandlers = append(generationHandlers,
		middleware.RejectExhaustedTokenForImageTaskCreation(),
		middleware.ImageTaskCreateAdmission(),
		middleware.ModelRequestRateLimit(),
		middleware.Distribute(),
		func(c *gin.Context) { CreatePublicImageTask(c, relayconstant.RelayModeImagesGenerations) },
	)
	imageTaskCreateRouter.POST("/generations", generationHandlers...)
	editHandlers := []gin.HandlerFunc{
		RequirePublicImageTaskContentType(relayconstant.RelayModeImagesEdits),
		ReusePublicImageTaskEditIfExists,
	}
	if createGate != nil {
		editHandlers = append(editHandlers, createGate)
	}
	editHandlers = append(editHandlers,
		middleware.RejectExhaustedTokenForImageTaskCreation(),
		middleware.ImageTaskCreateAdmission(),
		middleware.ModelRequestRateLimit(),
		middleware.Distribute(),
		func(c *gin.Context) { CreatePublicImageTask(c, relayconstant.RelayModeImagesEdits) },
	)
	imageTaskCreateRouter.POST("/edits", editHandlers...)
	imageTaskAccessRouter := imageTaskRouter.Group("")
	imageTaskAccessRouter.Use(middleware.TokenAuthForTaskAccess())
	imageTaskAccessRouter.GET("", ListPublicImageTasks)
	imageTaskAccessRouter.GET("/:task_id", GetPublicImageTask)
	imageTaskAccessRouter.GET("/:task_id/result", GetPublicImageTaskResult)
	imageTaskAccessRouter.POST("/:task_id/ack", AcknowledgePublicImageTaskResult)
	imageTaskAccessRouter.POST("/:task_id/cancel", CancelPublicImageTask)
	return router
}

func TestPublicImageTaskCreateReturnsAcceptedAndEnforcesIdempotency(t *testing.T) {
	router := setupImageTaskSyncBridgeE2E(t)

	request := func(prompt string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/image-tasks/generations",
			strings.NewReader(fmt.Sprintf(`{"model":"gpt-image-1","prompt":%q,"n":1}`, prompt)),
		)
		req.Header.Set("Authorization", "Bearer sk-testtoken")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "public-image-idem-1")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}

	startedAt := time.Now()
	first := request("draw a cat")
	require.Less(t, time.Since(startedAt), time.Second)
	require.Equal(t, http.StatusAccepted, first.Code, first.Body.String())
	var firstResponse dto.PublicImageTask
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &firstResponse))
	require.NotEmpty(t, firstResponse.TaskID)
	require.Equal(t, "public-image-idem-1", firstResponse.ClientTaskID)
	require.Equal(t, "queued", firstResponse.Status)

	var task model.Task
	require.NoError(t, model.DB.First(&task, "task_id = ?", firstResponse.TaskID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusQueued), task.Status)
	require.Equal(t, constant.TaskActionImageGeneration, task.Action)
	require.Equal(t, 201, task.PrivateData.TokenId)

	duplicate := request("draw a cat")
	require.Equal(t, http.StatusAccepted, duplicate.Code, duplicate.Body.String())
	var duplicateResponse dto.PublicImageTask
	require.NoError(t, json.Unmarshal(duplicate.Body.Bytes(), &duplicateResponse))
	require.Equal(t, firstResponse.TaskID, duplicateResponse.TaskID)

	conflict := request("draw a dog")
	require.Equal(t, http.StatusConflict, conflict.Code, conflict.Body.String())
	require.Equal(t, "idempotency_conflict", publicImageTaskErrorCode(t, conflict.Body.Bytes()))

	require.NoError(t, model.DB.Create(&model.Token{
		Id:          202,
		UserId:      101,
		Key:         "othertesttoken",
		Status:      common.TokenStatusEnabled,
		Name:        "other-image-token",
		ExpiredTime: -1,
		RemainQuota: 1_000_000_000,
	}).Error)
	otherTokenRequest := httptest.NewRequest(
		http.MethodPost,
		"/v1/image-tasks/generations",
		strings.NewReader(`{"model":"gpt-image-1","prompt":"draw a cat","n":1}`),
	)
	otherTokenRequest.Header.Set("Authorization", "Bearer sk-othertesttoken")
	otherTokenRequest.Header.Set("Content-Type", "application/json")
	otherTokenRequest.Header.Set("Idempotency-Key", "public-image-idem-1")
	otherTokenRecorder := httptest.NewRecorder()
	router.ServeHTTP(otherTokenRecorder, otherTokenRequest)
	require.Equal(t, http.StatusConflict, otherTokenRecorder.Code, otherTokenRecorder.Body.String())
	require.Equal(t, "idempotency_conflict", publicImageTaskErrorCode(t, otherTokenRecorder.Body.Bytes()))
	require.NotContains(t, otherTokenRecorder.Body.String(), firstResponse.TaskID)

	var count int64
	require.NoError(t, model.DB.Model(&model.Task{}).Where("platform = ?", constant.TaskPlatformImage).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestPublicImageTaskCreateRejectsUnsupportedMediaTypes(t *testing.T) {
	router := setupImageTaskSyncBridgeE2E(t)

	t.Run("generation requires json", func(t *testing.T) {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		require.NoError(t, writer.WriteField("model", "gpt-image-1"))
		require.NoError(t, writer.WriteField("prompt", "draw a cat"))
		require.NoError(t, writer.Close())

		request := httptest.NewRequest(http.MethodPost, "/v1/image-tasks/generations", &body)
		request.Header.Set("Authorization", "Bearer sk-testtoken")
		request.Header.Set("Content-Type", writer.FormDataContentType())
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)

		require.Equal(t, http.StatusUnsupportedMediaType, response.Code, response.Body.String())
		require.Equal(t, "unsupported_media_type", publicImageTaskErrorCode(t, response.Body.Bytes()))
	})

	t.Run("edit requires multipart", func(t *testing.T) {
		request := httptest.NewRequest(
			http.MethodPost,
			"/v1/image-tasks/edits",
			strings.NewReader(`{"model":"gpt-image-1","prompt":"edit this image","image":"fake"}`),
		)
		request.Header.Set("Authorization", "Bearer sk-testtoken")
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)

		require.Equal(t, http.StatusUnsupportedMediaType, response.Code, response.Body.String())
		require.Equal(t, "unsupported_media_type", publicImageTaskErrorCode(t, response.Body.Bytes()))
	})

	t.Run("edit requires multipart boundary", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/v1/image-tasks/edits", strings.NewReader("invalid multipart"))
		request.Header.Set("Authorization", "Bearer sk-testtoken")
		request.Header.Set("Content-Type", "multipart/form-data")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)

		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		require.Equal(t, "invalid_request", publicImageTaskErrorCode(t, response.Body.Bytes()))
	})

	var taskCount int64
	require.NoError(t, model.DB.Model(&model.Task{}).Where("public_image_task = ?", true).Count(&taskCount).Error)
	require.Zero(t, taskCount)
}

func TestPublicImageTaskCreationRecordsSharedRequestStorage(t *testing.T) {
	router := setupImageTaskSyncBridgeE2E(t)
	constant.ImageTaskFileCacheShared = true
	constant.ImageTaskFileCacheSharedTrusted = true
	common.SetImageTaskSharedCacheDisabled(false)

	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/image-tasks/generations",
		strings.NewReader(`{"model":"gpt-image-1","prompt":"shared request storage","n":1}`),
	)
	request.Header.Set("Authorization", "Bearer sk-testtoken")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusAccepted, response.Code, response.Body.String())

	var created dto.PublicImageTask
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &created))
	var task model.Task
	require.NoError(t, model.DB.First(&task, "task_id = ?", created.TaskID).Error)
	require.NotEmpty(t, task.PrivateData.RequestBodyPath)
	require.True(t, task.PrivateData.RequestBodyShared)
	require.Empty(t, task.PrivateData.RequestBodyBase64)
}

// publicImageTaskErrorCode 解析 /v1/image-tasks/* 的统一错误信封并返回 error.code。
// 断言必须落在机器可读的 code 上：只断言 message 含某个词，无法发现同一个 409 在两条
// 检测路径上给出不同 code 的问题。
func publicImageTaskErrorCode(t *testing.T, body []byte) string {
	t.Helper()
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	require.NoError(t, common.Unmarshal(body, &payload), string(body))
	require.Equal(t, "image_task_error", payload.Error.Type, string(body))
	require.NotEmpty(t, payload.Error.Message, string(body))
	return payload.Error.Code
}

// 竞态窗口：两个同键请求的预检 handler 都看不到任务和预约时会同时放行，冲突只能在
// createImageTaskInternal 里才被发现。这条路径必须和预检路径给出同一个 error.code。
func TestPublicImageTaskCreateConflictInsideCreateFlowKeepsPublicErrorCode(t *testing.T) {
	setupImageTaskSyncBridgeE2E(t)
	gin.SetMode(gin.TestMode)

	existing := &model.Task{
		TaskID:       "task_existing_for_race",
		ClientTaskID: "public-image-race-1",
		Platform:     constant.TaskPlatformImage,
		UserId:       101,
		Status:       model.TaskStatusQueued,
		PrivateData: model.TaskPrivateData{
			PublicImageTask:    true,
			TokenId:            201,
			RequestFingerprint: "fingerprint-of-a-different-request",
		},
	}
	require.NoError(t, existing.Insert())

	engine := gin.New()
	engine.POST("/v1/image-tasks/generations", func(c *gin.Context) {
		// 直接进入创建流程，跳过预检 handler，模拟竞态放行后的分支。
		c.Set("id", 101)
		c.Set("token_id", 201)
		CreatePublicImageTask(c, relayconstant.RelayModeImagesGenerations)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/image-tasks/generations",
		strings.NewReader(`{"model":"gpt-image-1","prompt":"draw a dog","n":1,"client_task_id":"public-image-race-1"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
	require.Equal(t, "idempotency_conflict", publicImageTaskErrorCode(t, recorder.Body.Bytes()))
}

func TestPublicImageTaskInternalErrorDoesNotExposeDatabaseDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	respondPublicImageTaskAPIError(ctx, types.NewError(
		errors.New("UNIQUE constraint failed: tasks.client_task_id"),
		types.ErrorCodeUpdateDataError,
	))

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, "image task request failed", payload.Error.Message)
	require.NotContains(t, recorder.Body.String(), "tasks.client_task_id")
}

func TestRespondPublicImageTaskRequestStorageErrorClassifiesCapacityAsRetryable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	respondPublicImageTaskRequestStorageError(ctx, fmt.Errorf("reserve request body: %w", common.ErrDiskCacheCapacityUnavailable))

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
	require.Equal(t, "1", recorder.Header().Get("Retry-After"))
	require.Equal(t, "image_task_unavailable", publicImageTaskErrorCode(t, recorder.Body.Bytes()))
	require.NotContains(t, recorder.Body.String(), "disk cache capacity")
}

func TestRespondPublicImageTaskRequestStorageErrorKeepsTrueOversizeAsClientError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	respondPublicImageTaskRequestStorageError(ctx, common.ErrRequestBodyTooLarge)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code, recorder.Body.String())
	require.Empty(t, recorder.Header().Get("Retry-After"))
	require.Equal(t, "invalid_request", publicImageTaskErrorCode(t, recorder.Body.Bytes()))
}

func TestRespondPublicImageTaskRequestStorageErrorMasksFilesystemFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	respondPublicImageTaskRequestStorageError(ctx, &os.PathError{
		Op:   "open",
		Path: `C:\\secret\\image-task-cache`,
		Err:  os.ErrPermission,
	})

	require.Equal(t, http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	require.Equal(t, "internal_error", publicImageTaskErrorCode(t, recorder.Body.Bytes()))
	require.NotContains(t, recorder.Body.String(), "image-task-cache")
	require.NotContains(t, recorder.Body.String(), "permission")
}

func TestPublicImageTaskInProgressConflictUsesDistinctErrorCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	respondPublicImageTaskAPIError(ctx, imageTaskIdempotencyInProgressError())

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Equal(t, "idempotency_in_progress", publicImageTaskErrorCode(t, recorder.Body.Bytes()))
}

// 幂等键的复用窗口和结果保留期对齐。超出窗口后旧任务的结果已经被清理，继续把它当作
// 幂等命中返回会让这个键永久无法生成新图，因此必须允许同键重新创建一条新任务。
func TestPublicImageTaskCreateReusesIdempotencyKeyAfterResultRetention(t *testing.T) {
	router := setupImageTaskSyncBridgeE2E(t)

	reuseWindow := int64(common.GetImageTaskIdempotencyReuseWindow().Seconds())
	now := time.Now().Unix()
	expired := &model.Task{
		TaskID:       "task_expired_idempotency_key",
		ClientTaskID: "public-image-idem-expired",
		Platform:     constant.TaskPlatformImage,
		UserId:       101,
		Status:       model.TaskStatusSuccess,
		FinishTime:   now - reuseWindow - 60,
		PrivateData: model.TaskPrivateData{
			PublicImageTask: true,
			TokenId:         201,
		},
	}
	require.NoError(t, expired.Insert())
	require.NoError(t, model.DB.Create(&model.ImageTaskClientTaskIDLock{
		UserID:        101,
		ClientTaskID:  expired.ClientTaskID,
		TaskPrimaryID: expired.ID,
		PublicTaskID:  expired.TaskID,
	}).Error)

	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/image-tasks/generations",
		strings.NewReader(`{"model":"gpt-image-1","prompt":"draw a fresh cat","n":1}`),
	)
	request.Header.Set("Authorization", "Bearer sk-testtoken")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", expired.ClientTaskID)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusAccepted, recorder.Code, recorder.Body.String())
	var response dto.PublicImageTask
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.NotEqual(t, expired.TaskID, response.TaskID, "expired idempotency key must not keep returning the dead task")
	require.Equal(t, expired.ClientTaskID, response.ClientTaskID)
	require.Equal(t, "queued", response.Status)

	// 旧任务记录仍然保留，只是不再被幂等复用。
	var count int64
	require.NoError(t, model.DB.Model(&model.Task{}).
		Where("client_task_id = ?", expired.ClientTaskID).Count(&count).Error)
	require.EqualValues(t, 2, count)
}

func TestPublicImageTaskCreateReusesIdempotencyKeyAfterFailedTask(t *testing.T) {
	router := setupImageTaskSyncBridgeE2E(t)

	failed := &model.Task{
		TaskID:       "task_failed_idempotency_key",
		ClientTaskID: "public-image-idem-failed",
		Platform:     constant.TaskPlatformImage,
		UserId:       101,
		Status:       model.TaskStatusFailure,
		FinishTime:   time.Now().Unix(),
		PrivateData: model.TaskPrivateData{
			PublicImageTask: true,
			TokenId:         201,
		},
	}
	require.NoError(t, failed.Insert())
	require.NoError(t, model.DB.Create(&model.ImageTaskClientTaskIDLock{
		UserID:        101,
		ClientTaskID:  failed.ClientTaskID,
		TaskPrimaryID: failed.ID,
		PublicTaskID:  failed.TaskID,
	}).Error)

	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/image-tasks/generations",
		strings.NewReader(`{"model":"gpt-image-1","prompt":"draw a retry cat","n":1}`),
	)
	request.Header.Set("Authorization", "Bearer sk-testtoken")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", failed.ClientTaskID)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusAccepted, recorder.Code, recorder.Body.String())
	var response dto.PublicImageTask
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.NotEqual(t, failed.TaskID, response.TaskID, "failed idempotency key must be reusable for a new task immediately")
	require.Equal(t, failed.ClientTaskID, response.ClientTaskID)
	require.Equal(t, "queued", response.Status)

	var count int64
	require.NoError(t, model.DB.Model(&model.Task{}).
		Where("client_task_id = ?", failed.ClientTaskID).Count(&count).Error)
	require.EqualValues(t, 2, count)
}

func TestPublicImageTaskCreateAllowsSameIdempotencyKeyAfterSettlementReview(t *testing.T) {
	router := setupImageTaskSyncBridgeE2E(t)
	requestBody := `{"model":"gpt-image-1","prompt":"draw a review retry cat","n":1}`
	clientTaskID := "public-image-idem-review"

	firstRequest := httptest.NewRequest(http.MethodPost, "/v1/image-tasks/generations", strings.NewReader(requestBody))
	firstRequest.Header.Set("Authorization", "Bearer sk-testtoken")
	firstRequest.Header.Set("Content-Type", "application/json")
	firstRequest.Header.Set("Idempotency-Key", clientTaskID)
	firstRecorder := httptest.NewRecorder()
	router.ServeHTTP(firstRecorder, firstRequest)
	require.Equal(t, http.StatusAccepted, firstRecorder.Code, firstRecorder.Body.String())

	var firstResponse dto.PublicImageTask
	require.NoError(t, common.Unmarshal(firstRecorder.Body.Bytes(), &firstResponse))
	var firstTask model.Task
	require.NoError(t, model.DB.First(&firstTask, "task_id = ?", firstResponse.TaskID).Error)
	firstTask.SetData(map[string]any{"data": []any{map[string]any{"url": "https://example.com/review.png"}}})
	now := time.Now().Unix()
	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", firstTask.ID).Updates(map[string]any{
		"status":            model.TaskStatusSuccess,
		"settlement_status": model.TaskSettlementStatusReview,
		"progress":          "100%",
		"finish_time":       now,
		"result_expires_at": now + 3600,
		"data":              firstTask.Data,
	}).Error)

	replayRequest := httptest.NewRequest(http.MethodPost, "/v1/image-tasks/generations", strings.NewReader(requestBody))
	replayRequest.Header.Set("Authorization", "Bearer sk-testtoken")
	replayRequest.Header.Set("Content-Type", "application/json")
	replayRequest.Header.Set("Idempotency-Key", clientTaskID)
	replayRecorder := httptest.NewRecorder()
	router.ServeHTTP(replayRecorder, replayRequest)

	require.Equal(t, http.StatusAccepted, replayRecorder.Code, replayRecorder.Body.String())
	var replayResponse dto.PublicImageTask
	require.NoError(t, common.Unmarshal(replayRecorder.Body.Bytes(), &replayResponse))
	require.NotEqual(t, firstResponse.TaskID, replayResponse.TaskID)
	require.Equal(t, clientTaskID, replayResponse.ClientTaskID)
	require.Equal(t, "queued", replayResponse.Status)
}

func TestPublicImageTaskCreateAllowsSameIdempotencyKeyAfterSettledTaskWithoutResult(t *testing.T) {
	router := setupImageTaskSyncBridgeE2E(t)
	requestBody := `{"model":"gpt-image-1","prompt":"draw a missing result retry cat","n":1}`
	clientTaskID := "public-image-idem-no-result"

	firstRequest := httptest.NewRequest(http.MethodPost, "/v1/image-tasks/generations", strings.NewReader(requestBody))
	firstRequest.Header.Set("Authorization", "Bearer sk-testtoken")
	firstRequest.Header.Set("Content-Type", "application/json")
	firstRequest.Header.Set("Idempotency-Key", clientTaskID)
	firstRecorder := httptest.NewRecorder()
	router.ServeHTTP(firstRecorder, firstRequest)
	require.Equal(t, http.StatusAccepted, firstRecorder.Code, firstRecorder.Body.String())

	var firstResponse dto.PublicImageTask
	require.NoError(t, common.Unmarshal(firstRecorder.Body.Bytes(), &firstResponse))
	var firstTask model.Task
	require.NoError(t, model.DB.First(&firstTask, "task_id = ?", firstResponse.TaskID).Error)
	now := time.Now().Unix()
	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", firstTask.ID).Updates(map[string]any{
		"status":                   model.TaskStatusSuccess,
		"settlement_status":        model.TaskSettlementStatusSettled,
		"progress":                 "100%",
		"finish_time":              now,
		"result_expires_at":        now + 3600,
		"data":                     nil,
		"image_task_result_stored": false,
	}).Error)

	replayRequest := httptest.NewRequest(http.MethodPost, "/v1/image-tasks/generations", strings.NewReader(requestBody))
	replayRequest.Header.Set("Authorization", "Bearer sk-testtoken")
	replayRequest.Header.Set("Content-Type", "application/json")
	replayRequest.Header.Set("Idempotency-Key", clientTaskID)
	replayRecorder := httptest.NewRecorder()
	router.ServeHTTP(replayRecorder, replayRequest)

	require.Equal(t, http.StatusAccepted, replayRecorder.Code, replayRecorder.Body.String())
	var replayResponse dto.PublicImageTask
	require.NoError(t, common.Unmarshal(replayRecorder.Body.Bytes(), &replayResponse))
	require.NotEqual(t, firstResponse.TaskID, replayResponse.TaskID)
	require.Equal(t, clientTaskID, replayResponse.ClientTaskID)
	require.Equal(t, "queued", replayResponse.Status)
}

func TestPublicImageTaskCreateReplaysExistingTaskWhenModelRateLimitIsFull(t *testing.T) {
	router := setupImageTaskSyncBridgeE2E(t)
	oldModelRateLimitEnabled := setting.ModelRequestRateLimitEnabled
	oldModelRequestRateLimitCount := setting.ModelRequestRateLimitCount
	oldModelRequestRateLimitDurationMinutes := setting.ModelRequestRateLimitDurationMinutes
	oldModelRequestRateLimitSuccessCount := setting.ModelRequestRateLimitSuccessCount
	setting.ModelRequestRateLimitEnabled = true
	setting.ModelRequestRateLimitCount = 1
	setting.ModelRequestRateLimitDurationMinutes = 1
	setting.ModelRequestRateLimitSuccessCount = 1000
	t.Cleanup(func() {
		setting.ModelRequestRateLimitEnabled = oldModelRateLimitEnabled
		setting.ModelRequestRateLimitCount = oldModelRequestRateLimitCount
		setting.ModelRequestRateLimitDurationMinutes = oldModelRequestRateLimitDurationMinutes
		setting.ModelRequestRateLimitSuccessCount = oldModelRequestRateLimitSuccessCount
	})
	require.NoError(t, model.DB.Create(&model.Token{
		Id:          209,
		UserId:      101,
		Key:         "ratelimitreplaytoken",
		Status:      common.TokenStatusEnabled,
		Name:        "rate-limit-replay-token",
		ExpiredTime: -1,
		RemainQuota: 1_000_000_000,
	}).Error)

	requestBody := `{"model":"gpt-image-1","prompt":"draw a rate limit replay cat","n":1}`
	request := func(clientTaskID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/image-tasks/generations", strings.NewReader(requestBody))
		req.Header.Set("Authorization", "Bearer sk-ratelimitreplaytoken")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", clientTaskID)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}

	first := request("public-image-idem-model-rate-limit")
	require.Equal(t, http.StatusAccepted, first.Code, first.Body.String())
	var created dto.PublicImageTask
	require.NoError(t, common.Unmarshal(first.Body.Bytes(), &created))

	replayed := request("public-image-idem-model-rate-limit")
	require.Equal(t, http.StatusAccepted, replayed.Code, replayed.Body.String())
	var replayedTask dto.PublicImageTask
	require.NoError(t, common.Unmarshal(replayed.Body.Bytes(), &replayedTask))
	require.Equal(t, created.TaskID, replayedTask.TaskID)

	blockedNewTask := request("public-image-idem-model-rate-limit-new")
	require.Equal(t, http.StatusTooManyRequests, blockedNewTask.Code, blockedNewTask.Body.String())
	require.Equal(t, "rate_limit_exceeded", publicImageTaskErrorCode(t, blockedNewTask.Body.Bytes()))
}

func TestPublicImageTaskCreateReservesIdempotencyBeforeNewTaskGuards(t *testing.T) {
	setupImageTaskSyncBridgeE2E(t)
	firstEnteredGate := make(chan struct{})
	releaseFirst := make(chan struct{})
	var gateHits atomic.Int32
	createGate := func(c *gin.Context) {
		if gateHits.Add(1) == 1 {
			close(firstEnteredGate)
			<-releaseFirst
			c.Next()
			return
		}
		publicImageTaskError(c, http.StatusTooManyRequests, "rate_limit_exceeded", "new task guard should not handle idempotent replay")
		c.Abort()
	}
	router := newImageTaskSyncBridgeE2ERouterWithCreateGate(createGate)
	const clientTaskID = "public-image-concurrent-pre-admission"
	requestBody := `{"model":"gpt-image-1","prompt":"draw a concurrent idempotent cat","n":1}`
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/image-tasks/generations", strings.NewReader(requestBody))
		req.Header.Set("Authorization", "Bearer sk-testtoken")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", clientTaskID)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}

	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		firstDone <- request()
	}()
	select {
	case <-firstEnteredGate:
	case <-time.After(time.Second):
		t.Fatal("first request did not reach the new-task guard")
	}

	lock, exists, err := model.GetImageTaskClientTaskIDLock(101, clientTaskID)
	require.NoError(t, err)
	require.True(t, exists, "idempotency reservation must exist before new-task guards run")
	require.NotNil(t, lock)
	require.Zero(t, lock.TaskPrimaryID)
	require.NotEmpty(t, lock.Fingerprint)

	secondDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		secondDone <- request()
	}()
	close(releaseFirst)

	first := <-firstDone
	require.Equal(t, http.StatusAccepted, first.Code, first.Body.String())
	var firstTask dto.PublicImageTask
	require.NoError(t, common.Unmarshal(first.Body.Bytes(), &firstTask))

	var second *httptest.ResponseRecorder
	select {
	case second = <-secondDone:
	case <-time.After(2 * time.Second):
		t.Fatal("idempotent replay did not finish after first request committed")
	}
	require.Equal(t, http.StatusAccepted, second.Code, second.Body.String())
	var secondTask dto.PublicImageTask
	require.NoError(t, common.Unmarshal(second.Body.Bytes(), &secondTask))
	require.Equal(t, firstTask.TaskID, secondTask.TaskID)
	require.Equal(t, int32(1), gateHits.Load(), "idempotent replay must bypass new-task guards")
}

func TestPublicImageTaskCreatePersistsIdempotentRequestOnlyOnce(t *testing.T) {
	router := setupImageTaskSyncBridgeE2E(t)
	oldConfig := common.GetDiskCacheConfig()
	common.SetDiskCacheConfig(common.DiskCacheConfig{
		Path:      filepath.Join(t.TempDir(), "single-write-cache"),
		MaxSizeMB: 1,
	})
	common.ResetDiskCacheUsage()
	t.Cleanup(func() {
		common.SetDiskCacheConfig(oldConfig)
		common.ResetDiskCacheUsage()
	})

	prompt := strings.Repeat("a", 600*1024)
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/image-tasks/generations",
		strings.NewReader(fmt.Sprintf(`{"model":"gpt-image-1","prompt":%q,"n":1}`, prompt)),
	)
	req.Header.Set("Authorization", "Bearer sk-testtoken")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "public-single-persist")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusAccepted, recorder.Code, recorder.Body.String())
	var task model.Task
	require.NoError(t, model.DB.First(&task, "client_task_id = ?", "public-single-persist").Error)
	require.NotEmpty(t, task.PrivateData.RequestBodyPath)
	require.FileExists(t, task.PrivateData.RequestBodyPath)
	require.Less(t, common.GetDiskCacheStats().CurrentDiskUsageBytes, int64(1<<20))
	t.Cleanup(func() { _ = common.RemoveDiskCacheFile(task.PrivateData.RequestBodyPath) })
}

func TestPublicImageTaskCreateReclaimsStaleIdempotencyReservation(t *testing.T) {
	router := setupImageTaskSyncBridgeE2E(t)

	const clientTaskID = "public-stale-reservation"
	reservation := &model.ImageTaskClientTaskIDLock{
		UserID:       101,
		ClientTaskID: clientTaskID,
	}
	require.NoError(t, model.DB.Create(reservation).Error)
	staleAt := time.Now().Add(-11 * time.Minute).Unix()
	require.NoError(t, model.DB.Model(&model.ImageTaskClientTaskIDLock{}).
		Where("id = ?", reservation.ID).
		UpdateColumns(map[string]any{"created_at": staleAt, "updated_at": staleAt}).Error)

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/image-tasks/generations",
		strings.NewReader(`{"model":"gpt-image-1","prompt":"recover stale reservation","n":1}`),
	)
	req.Header.Set("Authorization", "Bearer sk-testtoken")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", clientTaskID)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusAccepted, recorder.Code, recorder.Body.String())
	var response dto.PublicImageTask
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, clientTaskID, response.ClientTaskID)
	require.NotEmpty(t, response.TaskID)

	var task model.Task
	require.NoError(t, model.DB.First(&task, "task_id = ?", response.TaskID).Error)
	require.Equal(t, clientTaskID, task.ClientTaskID)
}

func TestPublicImageTaskIdempotentRetryDoesNotRequireAvailableChannel(t *testing.T) {
	router := setupImageTaskSyncBridgeE2E(t)
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/image-tasks/generations",
			strings.NewReader(`{"model":"gpt-image-1","prompt":"stable retry","n":1}`),
		)
		req.Header.Set("Authorization", "Bearer sk-testtoken")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "public-no-channel-retry")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}

	first := request()
	require.Equal(t, http.StatusAccepted, first.Code, first.Body.String())
	var created dto.PublicImageTask
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &created))
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 301).Update("status", common.ChannelStatusManuallyDisabled).Error)

	retry := request()
	require.Equal(t, http.StatusAccepted, retry.Code, retry.Body.String())
	var retried dto.PublicImageTask
	require.NoError(t, json.Unmarshal(retry.Body.Bytes(), &retried))
	require.Equal(t, created.TaskID, retried.TaskID)
}

func TestPublicImageTaskGenerationValidatesContractAndDefaultsModel(t *testing.T) {
	router := setupImageTaskSyncBridgeE2E(t)
	oldModelPrices := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldModelPrices))
	})
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"dall-e":0.01,"gpt-image-1":0.01}`))
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 301).
		Update("models", "gpt-image-1,dall-e").Error)
	priority := int64(0)
	require.NoError(t, model.DB.Create(&model.Ability{
		Group:     "default",
		Model:     "dall-e",
		ChannelId: 301,
		Enabled:   true,
		Priority:  &priority,
		Weight:    100,
	}).Error)

	withoutModel := httptest.NewRequest(
		http.MethodPost,
		"/v1/image-tasks/generations",
		strings.NewReader(`{"prompt":"use the documented default model","n":1}`),
	)
	withoutModel.Header.Set("Authorization", "Bearer sk-testtoken")
	withoutModel.Header.Set("Content-Type", "application/json")
	withoutModelRecorder := httptest.NewRecorder()
	router.ServeHTTP(withoutModelRecorder, withoutModel)
	require.Equal(t, http.StatusAccepted, withoutModelRecorder.Code, withoutModelRecorder.Body.String())

	var created dto.PublicImageTask
	require.NoError(t, json.Unmarshal(withoutModelRecorder.Body.Bytes(), &created))
	var task model.Task
	require.NoError(t, model.DB.First(&task, "task_id = ?", created.TaskID).Error)
	require.Equal(t, "dall-e", task.Properties.OriginModelName)

	withoutPrompt := httptest.NewRequest(
		http.MethodPost,
		"/v1/image-tasks/generations",
		strings.NewReader(`{"model":"gpt-image-1","n":1}`),
	)
	withoutPrompt.Header.Set("Authorization", "Bearer sk-testtoken")
	withoutPrompt.Header.Set("Content-Type", "application/json")
	withoutPromptRecorder := httptest.NewRecorder()
	router.ServeHTTP(withoutPromptRecorder, withoutPrompt)
	require.Equal(t, http.StatusBadRequest, withoutPromptRecorder.Code, withoutPromptRecorder.Body.String())
	require.Contains(t, withoutPromptRecorder.Body.String(), "prompt is required")
}

func TestPublicImageTaskFixedPriceNPreConsumesOnce(t *testing.T) {
	router := setupImageTaskSyncBridgeE2E(t)
	oldModelPrices := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldModelPrices))
	})
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"gpt-image-1":0.01}`))
	otherSettings, err := common.Marshal(dto.ChannelOtherSettings{ImageTaskMode: dto.ImageTaskModeSyncWrapper})
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 301).
		Update("settings", string(otherSettings)).Error)

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/image-tasks/generations",
		strings.NewReader(`{"model":"gpt-image-1","prompt":"two fixed-price images","n":2}`),
	)
	req.Header.Set("Authorization", "Bearer sk-testtoken")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusAccepted, recorder.Code, recorder.Body.String())

	var created dto.PublicImageTask
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &created))
	var task model.Task
	require.NoError(t, model.DB.First(&task, "task_id = ?", created.TaskID).Error)
	require.Equal(t, 10_000, task.Quota)
}

func TestPublicImageTaskEditValidatesRequiredMultipartFields(t *testing.T) {
	router := setupImageTaskSyncBridgeE2E(t)

	request := func(prompt string, modelName string, includeImage bool) *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if prompt != "" {
			require.NoError(t, writer.WriteField("prompt", prompt))
		}
		if modelName != "" {
			require.NoError(t, writer.WriteField("model", modelName))
		}
		if includeImage {
			part, err := writer.CreateFormFile("image", "input.png")
			require.NoError(t, err)
			_, err = part.Write([]byte("test-image"))
			require.NoError(t, err)
		}
		require.NoError(t, writer.Close())

		req := httptest.NewRequest(http.MethodPost, "/v1/image-tasks/edits", bytes.NewReader(body.Bytes()))
		req.Header.Set("Authorization", "Bearer sk-testtoken")
		req.Header.Set("Content-Type", writer.FormDataContentType())
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}

	withoutPrompt := request("", "gpt-image-1", true)
	require.Equal(t, http.StatusBadRequest, withoutPrompt.Code, withoutPrompt.Body.String())
	require.Contains(t, withoutPrompt.Body.String(), "prompt is required")

	withoutImage := request("missing image", "gpt-image-1", false)
	require.Equal(t, http.StatusBadRequest, withoutImage.Code, withoutImage.Body.String())
	require.Contains(t, withoutImage.Body.String(), "image is required")
}

func TestPublicImageTaskFullLifecycleEndToEnd(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/image-tasks/generations":
			_, _ = w.Write([]byte(`{"task_id":"upstream_public_lifecycle","status":"queued"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/image-tasks":
			if r.URL.Query().Get("include_image_data") == "true" {
				_, _ = w.Write([]byte(`{"items":[{"task_id":"upstream_public_lifecycle","status":"completed","progress":"100%","result":{"data":[{"url":"https://example.com/public-lifecycle.png"}],"usage":{"total_tokens":17}}}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[{"task_id":"upstream_public_lifecycle","status":"completed","progress":"100%"}]}`))
		default:
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer upstream.Close()

	router := setupImageTaskSyncBridgeE2E(t)
	oldRetention := constant.ImageTaskResultRetentionMinutes
	constant.ImageTaskResultRetentionMinutes = 720
	t.Cleanup(func() {
		constant.ImageTaskResultRetentionMinutes = oldRetention
	})
	service.RunImageTasksFunc = relay.RunImageTasks
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 301).Update("base_url", upstream.URL).Error)

	createRequest := httptest.NewRequest(
		http.MethodPost,
		"/v1/image-tasks/generations",
		strings.NewReader(`{"model":"gpt-image-1","prompt":"public lifecycle","n":1,"client_task_id":"public-lifecycle-key"}`),
	)
	createRequest.Header.Set("Authorization", "Bearer sk-testtoken")
	createRequest.Header.Set("Content-Type", "application/json")
	createRecorder := httptest.NewRecorder()
	router.ServeHTTP(createRecorder, createRequest)
	require.Equal(t, http.StatusAccepted, createRecorder.Code, createRecorder.Body.String())
	var created dto.PublicImageTask
	require.NoError(t, json.Unmarshal(createRecorder.Body.Bytes(), &created))
	require.Equal(t, "queued", created.Status)

	workerCtx, workerCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer workerCancel()
	settlementWindowStart := time.Now().Unix()
	require.NoError(t, <-driveImageTaskSyncBridgeWorkerUntilSettled(workerCtx))
	settlementWindowEnd := time.Now().Unix()

	call := func(method string, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer sk-testtoken")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}
	status := call(http.MethodGet, "/v1/image-tasks/"+created.TaskID)
	require.Equal(t, http.StatusOK, status.Code, status.Body.String())
	require.Contains(t, status.Body.String(), `"status":"completed"`)
	require.NotContains(t, status.Body.String(), "public-lifecycle.png")

	result := call(http.MethodGet, "/v1/image-tasks/"+created.TaskID+"/result")
	require.Equal(t, http.StatusOK, result.Code, result.Body.String())
	require.JSONEq(t, `{"data":[{"url":"https://example.com/public-lifecycle.png"}],"usage":{"total_tokens":17}}`, result.Body.String())

	ack := call(http.MethodPost, "/v1/image-tasks/"+created.TaskID+"/ack")
	require.Equal(t, http.StatusOK, ack.Code, ack.Body.String())

	var task model.Task
	require.NoError(t, model.DB.First(&task, "task_id = ?", created.TaskID).Error)
	require.True(t, task.PrivateData.PublicImageTask)
	require.GreaterOrEqual(t, task.ResultExpiresAt, settlementWindowStart+12*60*60)
	require.LessOrEqual(t, task.ResultExpiresAt, settlementWindowEnd+12*60*60)
	require.Equal(t, task.ResultExpiresAt, task.PrivateData.ResultExpiresAt)
	require.Positive(t, task.ResultAcknowledgedAt)
	require.Equal(t, task.ResultAcknowledgedAt+120, task.ResultDeleteAfter)
	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Update("result_delete_after", time.Now().Add(-time.Second).Unix()).Error)
	_, err := model.CleanupExpiredImageTaskResults(time.Now().Unix(), 12*time.Hour, 100)
	require.NoError(t, err)

	expiredResult := call(http.MethodGet, "/v1/image-tasks/"+created.TaskID+"/result")
	require.Equal(t, http.StatusGone, expiredResult.Code, expiredResult.Body.String())
	reloaded, exists, err := model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	require.Equal(t, model.TaskSettlementStatusSettled, reloaded.SettlementStatus)
	require.NotContains(t, string(reloaded.Data), "public-lifecycle.png")
	_, settlementExists, err := model.GetTaskSettlementRecord(task.ID)
	require.NoError(t, err)
	require.True(t, settlementExists)
}

func TestPublicImageTaskCancelRefundsQueuedTaskAndIsIdempotent(t *testing.T) {
	router := setupImageTaskSyncBridgeE2E(t)

	var initialUser model.User
	require.NoError(t, model.DB.First(&initialUser, 101).Error)
	var initialToken model.Token
	require.NoError(t, model.DB.First(&initialToken, 201).Error)

	createRequest := httptest.NewRequest(
		http.MethodPost,
		"/v1/image-tasks/generations",
		strings.NewReader(`{"model":"gpt-image-1","prompt":"cancel this task","n":1}`),
	)
	createRequest.Header.Set("Authorization", "Bearer sk-testtoken")
	createRequest.Header.Set("Content-Type", "application/json")
	createRecorder := httptest.NewRecorder()
	router.ServeHTTP(createRecorder, createRequest)
	require.Equal(t, http.StatusAccepted, createRecorder.Code, createRecorder.Body.String())
	var created dto.PublicImageTask
	require.NoError(t, json.Unmarshal(createRecorder.Body.Bytes(), &created))

	var queued model.Task
	require.NoError(t, model.DB.First(&queued, "task_id = ?", created.TaskID).Error)
	require.Positive(t, queued.Quota)
	require.NotEmpty(t, queued.PrivateData.RequestBodyPath)
	requestBodyPath := queued.PrivateData.RequestBodyPath
	require.FileExists(t, requestBodyPath)

	cancel := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/image-tasks/"+created.TaskID+"/cancel", nil)
		req.Header.Set("Authorization", "Bearer sk-testtoken")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}

	firstCancel := cancel()
	require.Equal(t, http.StatusOK, firstCancel.Code, firstCancel.Body.String())
	require.Contains(t, firstCancel.Body.String(), `"status":"cancelled"`)
	require.NoFileExists(t, requestBodyPath)

	reloaded, exists, err := model.GetTaskByID(queued.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status)
	require.Positive(t, reloaded.PrivateData.CancelledAt)
	require.Zero(t, reloaded.Quota)
	require.Empty(t, reloaded.PrivateData.RequestBodyPath)

	var userAfter model.User
	require.NoError(t, model.DB.First(&userAfter, 101).Error)
	require.Equal(t, initialUser.Quota, userAfter.Quota)
	var tokenAfter model.Token
	require.NoError(t, model.DB.First(&tokenAfter, 201).Error)
	require.Equal(t, initialToken.RemainQuota, tokenAfter.RemainQuota)

	secondCancel := cancel()
	require.Equal(t, http.StatusOK, secondCancel.Code, secondCancel.Body.String())
	require.Contains(t, secondCancel.Body.String(), `"status":"cancelled"`)
}

func TestPublicImageTaskCancelResumesRefundAfterInterruptedCancellation(t *testing.T) {
	router := setupImageTaskSyncBridgeE2E(t)

	var initialUser model.User
	require.NoError(t, model.DB.First(&initialUser, 101).Error)
	var initialToken model.Token
	require.NoError(t, model.DB.First(&initialToken, 201).Error)

	createRequest := httptest.NewRequest(
		http.MethodPost,
		"/v1/image-tasks/generations",
		strings.NewReader(`{"model":"gpt-image-1","prompt":"resume cancellation refund","n":1}`),
	)
	createRequest.Header.Set("Authorization", "Bearer sk-testtoken")
	createRequest.Header.Set("Content-Type", "application/json")
	createRecorder := httptest.NewRecorder()
	router.ServeHTTP(createRecorder, createRequest)
	require.Equal(t, http.StatusAccepted, createRecorder.Code, createRecorder.Body.String())
	var created dto.PublicImageTask
	require.NoError(t, json.Unmarshal(createRecorder.Body.Bytes(), &created))

	var task model.Task
	require.NoError(t, model.DB.First(&task, "task_id = ?", created.TaskID).Error)
	require.Positive(t, task.Quota)
	interruptedAt := time.Now().Unix()
	task.Status = model.TaskStatusFailure
	task.Progress = "100%"
	task.FailReason = "image task cancelled by client"
	task.FinishTime = interruptedAt
	task.PrivateData.CancelledAt = interruptedAt
	task.PrivateData.CancelledReason = task.FailReason
	require.NoError(t, model.DB.Select("status", "progress", "fail_reason", "finish_time", "private_data").Save(&task).Error)

	cancelRequest := httptest.NewRequest(http.MethodPost, "/v1/image-tasks/"+task.TaskID+"/cancel", nil)
	cancelRequest.Header.Set("Authorization", "Bearer sk-testtoken")
	cancelRecorder := httptest.NewRecorder()
	router.ServeHTTP(cancelRecorder, cancelRequest)

	require.Equal(t, http.StatusOK, cancelRecorder.Code, cancelRecorder.Body.String())
	reloaded, exists, err := model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Zero(t, reloaded.Quota)
	var userAfter model.User
	require.NoError(t, model.DB.First(&userAfter, 101).Error)
	require.Equal(t, initialUser.Quota, userAfter.Quota)
	var tokenAfter model.Token
	require.NoError(t, model.DB.First(&tokenAfter, 201).Error)
	require.Equal(t, initialToken.RemainQuota, tokenAfter.RemainQuota)
}

func TestPublicImageTaskCancelReportsRefundInProgress(t *testing.T) {
	router := setupImageTaskSyncBridgeE2E(t)

	createRequest := httptest.NewRequest(
		http.MethodPost,
		"/v1/image-tasks/generations",
		strings.NewReader(`{"model":"gpt-image-1","prompt":"concurrent cancellation refund","n":1}`),
	)
	createRequest.Header.Set("Authorization", "Bearer sk-testtoken")
	createRequest.Header.Set("Content-Type", "application/json")
	createRecorder := httptest.NewRecorder()
	router.ServeHTTP(createRecorder, createRequest)
	require.Equal(t, http.StatusAccepted, createRecorder.Code, createRecorder.Body.String())

	var created dto.PublicImageTask
	require.NoError(t, json.Unmarshal(createRecorder.Body.Bytes(), &created))
	var task model.Task
	require.NoError(t, model.DB.First(&task, "task_id = ?", created.TaskID).Error)
	require.Positive(t, task.Quota)
	task.Status = model.TaskStatusFailure
	task.Progress = "100%"
	task.FailReason = "image task cancelled by client"
	task.FinishTime = time.Now().Unix()
	task.Quota = 0
	task.RefundPending = true
	task.PrivateData.CancelledAt = task.FinishTime
	task.PrivateData.CancelledReason = task.FailReason
	require.NoError(t, model.DB.Select("*").Updates(&task).Error)
	require.NoError(t, model.DB.Create(&model.TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        model.TaskSettlementRecordStatusApplying,
		Operation:     "refund",
		UpdatedAt:     time.Now().Unix(),
	}).Error)

	cancelRequest := httptest.NewRequest(http.MethodPost, "/v1/image-tasks/"+task.TaskID+"/cancel", nil)
	cancelRequest.Header.Set("Authorization", "Bearer sk-testtoken")
	cancelRecorder := httptest.NewRecorder()
	router.ServeHTTP(cancelRecorder, cancelRequest)

	require.Equal(t, http.StatusConflict, cancelRecorder.Code, cancelRecorder.Body.String())
	require.Equal(t, "1", cancelRecorder.Header().Get("Retry-After"))
	require.Contains(t, cancelRecorder.Body.String(), "cancel_refund_in_progress")

	reloaded, exists, err := model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Zero(t, reloaded.Quota)
	require.True(t, reloaded.RefundPending)
}

func TestPublicImageTaskEditIdempotencyIgnoresMultipartBoundary(t *testing.T) {
	router := setupImageTaskSyncBridgeE2E(t)

	request := func(image []byte) *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		require.NoError(t, writer.WriteField("model", "gpt-image-1"))
		require.NoError(t, writer.WriteField("prompt", "edit this image"))
		require.NoError(t, writer.WriteField("n", "1"))
		part, err := writer.CreateFormFile("image", "input.png")
		require.NoError(t, err)
		_, err = part.Write(image)
		require.NoError(t, err)
		require.NoError(t, writer.Close())

		req := httptest.NewRequest(http.MethodPost, "/v1/image-tasks/edits", &body)
		req.Header.Set("Authorization", "Bearer sk-testtoken")
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Idempotency-Key", "public-edit-idem-1")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}

	first := request([]byte("same image"))
	require.Equal(t, http.StatusAccepted, first.Code, first.Body.String())
	var firstResponse dto.PublicImageTask
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &firstResponse))

	duplicate := request([]byte("same image"))
	require.Equal(t, http.StatusAccepted, duplicate.Code, duplicate.Body.String())
	var duplicateResponse dto.PublicImageTask
	require.NoError(t, json.Unmarshal(duplicate.Body.Bytes(), &duplicateResponse))
	require.Equal(t, firstResponse.TaskID, duplicateResponse.TaskID)

	conflict := request([]byte("different image"))
	require.Equal(t, http.StatusConflict, conflict.Code, conflict.Body.String())

	var task model.Task
	require.NoError(t, model.DB.First(&task, "task_id = ?", firstResponse.TaskID).Error)
	require.Equal(t, constant.TaskActionImageEdit, task.Action)
	require.NotEmpty(t, task.PrivateData.RequestFingerprint)
}

func TestPublicImageTaskStatusAllowsExhaustedOwnerToken(t *testing.T) {
	router := setupImageTaskSyncBridgeE2E(t)

	createRequest := httptest.NewRequest(
		http.MethodPost,
		"/v1/image-tasks/generations",
		strings.NewReader(`{"model":"gpt-image-1","prompt":"use remaining quota","n":1}`),
	)
	createRequest.Header.Set("Authorization", "Bearer sk-testtoken")
	createRequest.Header.Set("Content-Type", "application/json")
	createRecorder := httptest.NewRecorder()
	router.ServeHTTP(createRecorder, createRequest)
	require.Equal(t, http.StatusAccepted, createRecorder.Code, createRecorder.Body.String())
	var created dto.PublicImageTask
	require.NoError(t, json.Unmarshal(createRecorder.Body.Bytes(), &created))

	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 201).Updates(map[string]any{
		"status":       common.TokenStatusExhausted,
		"remain_quota": 0,
	}).Error)
	statusRequest := httptest.NewRequest(http.MethodGet, "/v1/image-tasks/"+created.TaskID, nil)
	statusRequest.Header.Set("Authorization", "Bearer sk-testtoken")
	statusRecorder := httptest.NewRecorder()
	router.ServeHTTP(statusRecorder, statusRequest)

	require.Equal(t, http.StatusOK, statusRecorder.Code, statusRecorder.Body.String())
	require.Contains(t, statusRecorder.Body.String(), created.TaskID)
}

func TestPublicImageTaskCreateAllowsExhaustedTokenOnlyForIdempotentReplay(t *testing.T) {
	router := setupImageTaskSyncBridgeE2E(t)

	request := func(idempotencyKey string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/image-tasks/generations",
			strings.NewReader(`{"model":"gpt-image-1","prompt":"recover lost accepted response","n":1}`),
		)
		req.Header.Set("Authorization", "Bearer sk-testtoken")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", idempotencyKey)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}

	first := request("public-image-exhausted-replay")
	require.Equal(t, http.StatusAccepted, first.Code, first.Body.String())
	var created dto.PublicImageTask
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &created))

	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 201).Updates(map[string]any{
		"status":       common.TokenStatusExhausted,
		"remain_quota": 0,
	}).Error)

	replayed := request("public-image-exhausted-replay")
	require.Equal(t, http.StatusAccepted, replayed.Code, replayed.Body.String())
	var replayedTask dto.PublicImageTask
	require.NoError(t, json.Unmarshal(replayed.Body.Bytes(), &replayedTask))
	require.Equal(t, created.TaskID, replayedTask.TaskID)

	newRequest := request("public-image-exhausted-new-task")
	require.Equal(t, http.StatusForbidden, newRequest.Code, newRequest.Body.String())
	require.Contains(t, newRequest.Body.String(), `"code":"insufficient_token_quota"`)

	var taskCount int64
	require.NoError(t, model.DB.Model(&model.Task{}).Where("public_image_task = ?", true).Count(&taskCount).Error)
	require.Equal(t, int64(1), taskCount)
}

func TestPublicImageTaskStatusRejectsDisabledOwnerToken(t *testing.T) {
	router := setupImageTaskSyncBridgeE2E(t)

	createRequest := httptest.NewRequest(
		http.MethodPost,
		"/v1/image-tasks/generations",
		strings.NewReader(`{"model":"gpt-image-1","prompt":"disable owner token","n":1}`),
	)
	createRequest.Header.Set("Authorization", "Bearer sk-testtoken")
	createRequest.Header.Set("Content-Type", "application/json")
	createRecorder := httptest.NewRecorder()
	router.ServeHTTP(createRecorder, createRequest)
	require.Equal(t, http.StatusAccepted, createRecorder.Code, createRecorder.Body.String())
	var created dto.PublicImageTask
	require.NoError(t, json.Unmarshal(createRecorder.Body.Bytes(), &created))

	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 201).Update("status", common.TokenStatusDisabled).Error)
	statusRequest := httptest.NewRequest(http.MethodGet, "/v1/image-tasks/"+created.TaskID, nil)
	statusRequest.Header.Set("Authorization", "Bearer sk-testtoken")
	statusRecorder := httptest.NewRecorder()
	router.ServeHTTP(statusRecorder, statusRequest)

	require.Equal(t, http.StatusUnauthorized, statusRecorder.Code, statusRecorder.Body.String())
	require.NotContains(t, statusRecorder.Body.String(), created.TaskID)
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

func TestCancelImageTaskSyncBridgeWaitDoesNotExportRefundWithoutConsume(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, cleanup := setupImageTaskControllerTestDB(t)
	t.Cleanup(cleanup)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Log{}, &model.QuotaData{}, &model.TokenUsageDaily{}, &model.Channel{}))
	oldDataExportEnabled := common.DataExportEnabled
	oldNodeName := common.NodeName
	common.DataExportEnabled = true
	common.NodeName = "sync-cancel-node"
	model.CacheQuotaDataLock.Lock()
	model.CacheQuotaData = make(map[string]*model.QuotaData)
	model.CacheQuotaDataLock.Unlock()
	t.Cleanup(func() {
		common.DataExportEnabled = oldDataExportEnabled
		common.NodeName = oldNodeName
		model.CacheQuotaDataLock.Lock()
		model.CacheQuotaData = make(map[string]*model.QuotaData)
		model.CacheQuotaDataLock.Unlock()
	})
	require.NoError(t, db.Create(&model.User{Id: 2, Username: "image-owner", Password: "password123", Quota: 10000, Status: common.UserStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Token{Id: 2, UserId: 2, Name: "image-token", Key: "sk-image", RemainQuota: 5000, Status: common.TokenStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Channel{Id: 2, Name: "image-channel", Status: common.ChannelStatusEnabled}).Error)
	bodyPath, err := common.WriteImageTaskBodyCacheFile([]byte(`{"prompt":"draw"}`))
	require.NoError(t, err)
	task := &model.Task{
		TaskID:    "task_sync_bridge_export_cancel",
		Platform:  constant.TaskPlatformImage,
		UserId:    2,
		ChannelId: 2,
		Status:    model.TaskStatusQueued,
		Progress:  "0%",
		Quota:     3000,
		Group:     "default",
		Properties: model.Properties{
			OriginModelName: "gpt-image-1",
		},
		PrivateData: model.TaskPrivateData{
			RequestBodyPath: bodyPath,
			TokenId:         2,
			NodeName:        "sync-cancel-node",
			BillingSource:   service.BillingSourceWallet,
		},
	}
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	body, apiErr := cancelImageTaskSyncBridgeWait(ctx, task, "image generation timed out")
	model.SaveQuotaDataCache()

	require.Empty(t, body)
	require.NotNil(t, apiErr)
	var rows []model.QuotaData
	require.NoError(t, db.Find(&rows).Error)
	require.Empty(t, rows)
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

func TestPersistImageTaskRequestRejectsMismatchedIdempotencyIdentifiers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/image-tasks/generations", nil)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Request.Header.Set("Idempotency-Key", "header_task_id")
	storage, err := common.CreateBodyStorage([]byte(`{"prompt":"draw a cat","client_task_id":"body_task_id"}`))
	require.NoError(t, err)
	defer storage.Close()
	ctx.Set(common.KeyBodyStorage, storage)

	persisted, err := persistImageTaskRequest(ctx, relayconstant.RelayModeImagesGenerations, "gpt-image-1")

	require.Nil(t, persisted)
	require.ErrorContains(t, err, "must match")
}

func TestPersistPublicImageTaskRequestStripsClientTaskIDFromUpstreamBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/image-tasks/generations", nil)
	ctx.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.CreateBodyStorage([]byte(`{"prompt":"draw a cat","client_task_id":"public_task_id"}`))
	require.NoError(t, err)
	defer storage.Close()
	ctx.Set(common.KeyBodyStorage, storage)

	persisted, err := persistImageTaskRequest(ctx, relayconstant.RelayModeImagesGenerations, "gpt-image-1")
	require.NoError(t, err)
	require.Equal(t, "public_task_id", persisted.ClientTaskID)
	require.JSONEq(t, `{"model":"gpt-image-1","prompt":"draw a cat","stream":false}`, string(persisted.Body))
	t.Cleanup(func() {
		_ = os.Remove(persisted.Path)
	})
}

func TestWritePublicImageTaskMultipartRequestStripsClientTaskID(t *testing.T) {
	form := &multipart.Form{Value: map[string][]string{
		"prompt":         {"draw a cat"},
		"client_task_id": {"public_multipart_id"},
	}}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writeImageTaskMultipartRequest(writer, form, "gpt-image-1", true))
	require.NoError(t, writer.Close())

	reader := multipart.NewReader(bytes.NewReader(body.Bytes()), writer.Boundary())
	fields := make(map[string]string)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		value, err := io.ReadAll(part)
		require.NoError(t, err)
		fields[part.FormName()] = string(value)
	}
	require.NotContains(t, fields, "client_task_id")
	require.Equal(t, "draw a cat", fields["prompt"])
}

func TestWaitForImageTaskIdempotencyReservationReturnsConcurrentTask(t *testing.T) {
	_, cleanup := setupImageTaskControllerTestDB(t)
	t.Cleanup(cleanup)
	lock, reserved, err := model.ReserveImageTaskClientTaskID(7, "concurrent_client_id", "same_fingerprint")
	require.NoError(t, err)
	require.True(t, reserved)
	require.Equal(t, "same_fingerprint", lock.Fingerprint)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("id", 7)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/image-tasks/generations", nil)
	inserted := make(chan error, 1)
	go func() {
		time.Sleep(50 * time.Millisecond)
		task := &model.Task{
			TaskID:       "task_concurrent_idempotency",
			ClientTaskID: "concurrent_client_id",
			Platform:     constant.TaskPlatformImage,
			UserId:       7,
			Status:       model.TaskStatusQueued,
		}
		inserted <- task.Insert()
	}()

	task, exists, released, err := waitForImageTaskIdempotencyReservation(ctx, "concurrent_client_id", time.Now().Add(time.Second))
	require.NoError(t, err)
	require.True(t, exists)
	require.False(t, released)
	require.Equal(t, "task_concurrent_idempotency", task.TaskID)
	require.NoError(t, <-inserted)
}

func TestPersistImageTaskRequestKeepsSyncBodyIdempotencyIdentifier(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Request.Header.Set("Idempotency-Key", "header_task_id")
	storage, err := common.CreateBodyStorage([]byte(`{"prompt":"draw a cat","client_task_id":"body_task_id"}`))
	require.NoError(t, err)
	defer storage.Close()
	ctx.Set(common.KeyBodyStorage, storage)

	persisted, err := persistImageTaskRequest(ctx, relayconstant.RelayModeImagesGenerations, "gpt-image-1")
	require.NoError(t, err)
	require.Equal(t, "body_task_id", persisted.ClientTaskID)
	t.Cleanup(func() {
		_ = os.Remove(persisted.Path)
	})
}

func TestImageTaskRequestFingerprintNormalizesJSONAndExcludesClientTaskID(t *testing.T) {
	first, err := imageTaskRequestFingerprint(
		relayconstant.RelayModeImagesGenerations,
		"application/json",
		[]byte(`{"prompt":"draw a cat","client_task_id":"first","stream":false}`),
	)
	require.NoError(t, err)
	second, err := imageTaskRequestFingerprint(
		relayconstant.RelayModeImagesGenerations,
		"application/json; charset=utf-8",
		[]byte("{\n  \"stream\": false, \"client_task_id\": \"second\", \"prompt\": \"draw a cat\"\n}"),
	)
	require.NoError(t, err)
	different, err := imageTaskRequestFingerprint(
		relayconstant.RelayModeImagesGenerations,
		"application/json",
		[]byte(`{"prompt":"draw a dog","stream":false}`),
	)
	require.NoError(t, err)

	require.Equal(t, first, second)
	require.NotEqual(t, first, different)
}

func TestImageTaskRequestFingerprintIsStableAcrossKeyOrderAndNumberLiterals(t *testing.T) {
	fingerprint := func(body string) string {
		value, err := imageTaskRequestFingerprint(relayconstant.RelayModeImagesGenerations, "application/json", []byte(body))
		require.NoError(t, err)
		return value
	}

	// 键序不同、嵌套对象键序不同，指纹必须一致（依赖 common.Marshal 的键排序）。
	require.Equal(t,
		fingerprint(`{"model":"gpt-image-1","prompt":"cat","n":1,"meta":{"a":1,"b":2}}`),
		fingerprint(`{"meta":{"b":2,"a":1},"n":1,"prompt":"cat","model":"gpt-image-1"}`),
	)

	// 数字字面量必须原样保留：1 与 1.0 是不同请求，不能被浮点归一化成同一指纹。
	require.NotEqual(t,
		fingerprint(`{"model":"gpt-image-1","n":1}`),
		fingerprint(`{"model":"gpt-image-1","n":1.0}`),
	)

	// 大整数不能因浮点精度丢失而与相邻值碰撞。
	require.NotEqual(t,
		fingerprint(`{"model":"gpt-image-1","seed":10000000000000001}`),
		fingerprint(`{"model":"gpt-image-1","seed":10000000000000002}`),
	)
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

func TestCloneImageTaskBillingRequestInputForStorageCapturesOnlyReferencedParams(t *testing.T) {
	input := imageTaskBillingRequestInputFromPersistedRequest(&imageTaskPersistedRequest{
		ContentType: "application/json",
		Body:        []byte(`{"quality":"high","prompt":"private prompt"}`),
	}, map[string]string{"X-Test": " trace-123 "})
	snapshot := &billingexpr.BillingSnapshot{
		BillingMode: "tiered_expr",
		ExprString:  `param("quality") == "high" ? tier("high", p * 2) : tier("normal", p)`,
	}

	cloned, captured := cloneImageTaskBillingRequestInputForStorage(input, snapshot)

	require.NotNil(t, cloned)
	require.True(t, captured)
	require.Empty(t, cloned.Body)
	require.Equal(t, "trace-123", cloned.Headers["X-Test"])
	require.Equal(t, "high", cloned.Params["quality"])
	encoded, err := json.Marshal(cloned)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private prompt")

	bodyOnlyInput := imageTaskBillingRequestInputFromPersistedRequest(&imageTaskPersistedRequest{
		ContentType: "application/json",
		Body:        []byte(`{"stream":false}`),
	}, nil)
	cloned, captured = cloneImageTaskBillingRequestInputForStorage(bodyOnlyInput, nil)
	require.Nil(t, cloned)
	require.True(t, captured)
}

func TestMultipartImageTaskBillingInputCapturesValidatedTierParams(t *testing.T) {
	n := uint(2)
	input := imageTaskBillingRequestInputFromPersistedRequest(&imageTaskPersistedRequest{
		ContentType: "multipart/form-data; boundary=test-boundary",
	}, nil, &dto.ImageRequest{
		Prompt:  "private edit prompt",
		Model:   "gpt-image-1",
		Quality: "high",
		Size:    "1536x1024",
		N:       &n,
	})
	snapshot := &billingexpr.BillingSnapshot{
		BillingMode: "tiered_expr",
		ExprString:  `param("quality") == "high" && param("size") == "1536x1024" && param("n") == 2 ? tier("high", p * 2) : tier("normal", p)`,
	}

	cloned, captured := cloneImageTaskBillingRequestInputForStorage(input, snapshot)

	require.True(t, captured)
	require.NotNil(t, cloned)
	require.Equal(t, "high", cloned.Params["quality"])
	require.Equal(t, "1536x1024", cloned.Params["size"])
	require.EqualValues(t, 2, cloned.Params["n"])
	encoded, err := json.Marshal(cloned)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private edit prompt")
}

func TestMultipartImageTaskBillingInputCapturesForwardedExtensionParams(t *testing.T) {
	persisted, err := persistImageTaskMultipartRequestInMemory(&multipart.Form{Value: map[string][]string{
		"provider_tier":  {"premium"},
		"client_task_id": {"must-not-reach-upstream"},
		"stream":         {"true"},
	}}, "gpt-image-1", "client-task-id", "fingerprint", true)
	require.NoError(t, err)

	n := uint(1)
	input := imageTaskBillingRequestInputFromPersistedRequest(persisted, nil, &dto.ImageRequest{
		Prompt: "private edit prompt",
		Model:  "gpt-image-1",
		N:      &n,
	})
	snapshot := &billingexpr.BillingSnapshot{
		BillingMode: "tiered_expr",
		ExprString:  `param("provider_tier") == "premium" && param("n") == 1 && param("stream") == false ? tier("premium", p * 2) : tier("normal", p)`,
	}

	cloned, captured := cloneImageTaskBillingRequestInputForStorage(input, snapshot)

	require.True(t, captured)
	require.NotNil(t, cloned)
	require.Equal(t, "premium", cloned.Params["provider_tier"])
	require.EqualValues(t, 1, cloned.Params["n"])
	require.Equal(t, false, cloned.Params["stream"])
	require.NotContains(t, cloned.Params, "client_task_id")
}

func TestImageTaskRequestBodyBase64ForStorageFollowsFallbackPolicy(t *testing.T) {
	oldShared := constant.ImageTaskFileCacheShared
	oldTrusted := constant.ImageTaskFileCacheSharedTrusted
	oldAffinity := constant.ImageTaskLocalFileCacheAffinity
	oldNode := common.NodeName
	oldNodeManual := common.NodeNameManuallyConfigured
	oldSharedDisabled := common.ImageTaskSharedCacheDisabled()
	oldBase64MaxMB := constant.ImageTaskRequestBodyBase64MaxMB
	oldWorkerEnabled := constant.ImageTaskWorkerEnabled
	oldMaster := common.IsMasterNode
	oldRunner := service.RunImageTasksFunc
	t.Cleanup(func() {
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskFileCacheSharedTrusted = oldTrusted
		constant.ImageTaskLocalFileCacheAffinity = oldAffinity
		constant.ImageTaskRequestBodyBase64MaxMB = oldBase64MaxMB
		constant.ImageTaskWorkerEnabled = oldWorkerEnabled
		common.NodeName = oldNode
		common.NodeNameManuallyConfigured = oldNodeManual
		common.IsMasterNode = oldMaster
		common.SetImageTaskSharedCacheDisabled(oldSharedDisabled)
		service.RunImageTasksFunc = oldRunner
	})
	common.SetImageTaskSharedCacheDisabled(false)
	constant.ImageTaskRequestBodyBase64MaxMB = 16
	// Pin a local-capable node so affinity can keep bodies on disk without
	// the API-only portable fallback interfering.
	service.RunImageTasksFunc = func(context.Context, []*model.Task) error { return nil }
	constant.ImageTaskWorkerEnabled = true
	common.IsMasterNode = false

	persisted := &imageTaskPersistedRequest{Body: []byte(`{"model":"gpt-image-1"}`)}
	constant.ImageTaskFileCacheShared = false
	constant.ImageTaskLocalFileCacheAffinity = false
	value, err := imageTaskRequestBodyBase64ForStorage(persisted)
	require.NoError(t, err)
	require.Equal(t, base64.StdEncoding.EncodeToString(persisted.Body), value)

	common.NodeName = "node-a"
	common.NodeNameManuallyConfigured = true
	constant.ImageTaskLocalFileCacheAffinity = true
	value, err = imageTaskRequestBodyBase64ForStorage(persisted)
	require.NoError(t, err)
	require.Empty(t, value, "stable NODE_NAME affinity node may keep request body on local disk")

	// Unstable hostname NODE_NAME forces portable bodies under affinity.
	common.NodeNameManuallyConfigured = false
	value, err = imageTaskRequestBodyBase64ForStorage(persisted)
	require.NoError(t, err)
	require.Equal(t, base64.StdEncoding.EncodeToString(persisted.Body), value)

	// API-only nodes cannot claim local files; force portable even with affinity.
	common.NodeNameManuallyConfigured = true
	constant.ImageTaskWorkerEnabled = false
	common.IsMasterNode = false
	value, err = imageTaskRequestBodyBase64ForStorage(persisted)
	require.NoError(t, err)
	require.Equal(t, base64.StdEncoding.EncodeToString(persisted.Body), value)

	// Restore local capability for the remaining shared-cache branches.
	constant.ImageTaskWorkerEnabled = true
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

func TestImageTaskCreateBodyNotExecutableOnAPIOnlyNodeWithoutPortableOrShared(t *testing.T) {
	oldShared := constant.ImageTaskFileCacheShared
	oldTrusted := constant.ImageTaskFileCacheSharedTrusted
	oldWorker := constant.ImageTaskWorkerEnabled
	oldMaster := common.IsMasterNode
	oldRunner := service.RunImageTasksFunc
	oldDisabled := common.ImageTaskSharedCacheDisabled()
	t.Cleanup(func() {
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskFileCacheSharedTrusted = oldTrusted
		constant.ImageTaskWorkerEnabled = oldWorker
		common.IsMasterNode = oldMaster
		service.RunImageTasksFunc = oldRunner
		common.SetImageTaskSharedCacheDisabled(oldDisabled)
	})
	service.RunImageTasksFunc = func(context.Context, []*model.Task) error { return nil }
	common.SetImageTaskSharedCacheDisabled(false)
	common.IsMasterNode = false
	constant.ImageTaskWorkerEnabled = false
	constant.ImageTaskFileCacheShared = false
	constant.ImageTaskFileCacheSharedTrusted = false

	require.True(t, imageTaskCreateBodyNotExecutable(false))
	require.False(t, imageTaskCreateBodyNotExecutable(true), "portable body is executable by any worker")

	constant.ImageTaskFileCacheShared = true
	constant.ImageTaskFileCacheSharedTrusted = true
	require.False(t, imageTaskCreateBodyNotExecutable(false), "trusted shared cache can serve non-portable bodies")

	constant.ImageTaskWorkerEnabled = true
	constant.ImageTaskFileCacheShared = false
	constant.ImageTaskFileCacheSharedTrusted = false
	require.False(t, imageTaskCreateBodyNotExecutable(false), "local execution can use node-local body files")
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

	body, availability, resultErr := imageTaskResponseResult(task)

	require.Empty(t, resultErr)
	require.Equal(t, imageTaskResultReady, availability)
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

	body, availability, resultErr := imageTaskResponseResult(task)

	require.Empty(t, body)
	require.Equal(t, imageTaskResultGone, availability)
	require.Equal(t, imageTaskResultExpiredMessage, resultErr)
}

func TestImageTaskResponseResultMarksMissingStoredResultFileUnreadable(t *testing.T) {
	task := &model.Task{
		TaskID: "task_missing_result_file",
		Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			ResultBodyPath: "missing-result.json",
		},
		Data: []byte(`{"_newapi_result_file":true}`),
	}

	body, availability, resultErr := imageTaskResponseResult(task)

	require.Empty(t, body)
	require.Equal(t, imageTaskResultUnreadable, availability)
	require.Equal(t, imageTaskResultUnreadableMessage, resultErr)
}

func TestImageTaskResponseResultMarksCleanedStoredResultFileExpired(t *testing.T) {
	task := &model.Task{
		TaskID:          "task_cleaned_result_file",
		Status:          model.TaskStatusSuccess,
		ResultCleanedAt: time.Now().Add(-time.Minute).Unix(),
		PrivateData: model.TaskPrivateData{
			ResultBodyPath: "missing-result.json",
		},
		Data: []byte(`{"_newapi_result_file":true}`),
	}

	body, availability, resultErr := imageTaskResponseResult(task)

	require.Empty(t, body)
	require.Equal(t, imageTaskResultGone, availability)
	require.Equal(t, imageTaskResultExpiredMessage, resultErr)
}

func TestImageTaskResponseResultMarksCorruptedStoredResultUnreadable(t *testing.T) {
	result := []byte(`{"data":[{"b64_json":"stored-b64"}]}`)
	path, err := common.WriteImageTaskResultCacheFile(result)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove(path)
	})
	task := &model.Task{
		TaskID: "task_corrupted_result_file",
		Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			ResultBodyPath:   path,
			ResultBodySize:   int64(len(result)),
			ResultBodySHA256: hex.EncodeToString(sha256.New().Sum(nil)),
			ResultExpiresAt:  time.Now().Add(time.Hour).Unix(),
		},
		Data: []byte(`{"_newapi_result_file":true}`),
	}

	body, availability, resultErr := imageTaskResponseResult(task)

	require.Empty(t, body)
	require.Equal(t, imageTaskResultUnreadable, availability)
	require.Equal(t, imageTaskResultUnreadableMessage, resultErr)
}

func TestImageTaskResponseResultMarksStoredResultMarkerWithoutPathExpired(t *testing.T) {
	task := &model.Task{
		TaskID: "task_marker_only",
		Status: model.TaskStatusSuccess,
		Data:   []byte(`{"_newapi_result_file":true}`),
	}

	body, availability, resultErr := imageTaskResponseResult(task)

	require.Empty(t, body)
	require.Equal(t, imageTaskResultGone, availability)
	require.Equal(t, imageTaskResultExpiredMessage, resultErr)
}

func TestImageTaskResponseResultDoesNotTreatMarkerTextAsPlaceholder(t *testing.T) {
	result := []byte(`{"data":[{"revised_prompt":"literal _newapi_result_file text"}]}`)
	task := &model.Task{
		TaskID: "task_marker_text",
		Status: model.TaskStatusSuccess,
		Data:   result,
	}

	body, availability, resultErr := imageTaskResponseResult(task)

	require.JSONEq(t, string(result), string(body))
	require.Equal(t, imageTaskResultReady, availability)
	require.Empty(t, resultErr)
}
