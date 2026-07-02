package controller

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestImageTaskAllowedByTokenModelLimitUsesTaskOriginModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	task := &model.Task{}
	task.Properties.OriginModelName = "gpt-image-1"

	require.True(t, imageTaskAllowedByTokenModelLimit(ctx, task))

	common.SetContextKey(ctx, constant.ContextKeyTokenModelLimitEnabled, true)
	common.SetContextKey(ctx, constant.ContextKeyTokenModelLimit, map[string]bool{
		"gpt-image-1": true,
	})

	require.True(t, imageTaskAllowedByTokenModelLimit(ctx, task))

	common.SetContextKey(ctx, constant.ContextKeyTokenModelLimit, map[string]bool{
		"gpt-4o": true,
	})

	require.False(t, imageTaskAllowedByTokenModelLimit(ctx, task))
}

func TestValidateImageTaskModeRequestRejectsGPTImage2APIAsyncMultipleImages(t *testing.T) {
	n := uint(2)
	err := validateImageTaskModeRequest(&dto.ImageRequest{N: &n}, dto.ImageTaskModeGPTImage2APIAsync)
	require.ErrorContains(t, err, "n 大于 1")

	require.NoError(t, validateImageTaskModeRequest(&dto.ImageRequest{N: &n}, dto.ImageTaskModeSyncWrapper))

	one := uint(1)
	require.NoError(t, validateImageTaskModeRequest(&dto.ImageRequest{N: &one}, dto.ImageTaskModeGPTImage2APIAsync))
	require.NoError(t, validateImageTaskModeRequest(&dto.ImageRequest{}, dto.ImageTaskModeGPTImage2APIAsync))
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

func TestPersistImageTaskRequestWritesDefaultGenerationModelAndClientTaskID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/image-tasks/generations", nil)
	ctx.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.CreateBodyStorage([]byte(`{"prompt":"draw a cat","client_task_id":"task_local_123"}`))
	require.NoError(t, err)
	defer storage.Close()
	ctx.Set(common.KeyBodyStorage, storage)

	persisted, err := persistImageTaskRequest(ctx, relayconstant.RelayModeImagesGenerations, dto.ImageTaskDefaultGenerationModel)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove(persisted.Path)
	})

	require.JSONEq(t, `{"client_task_id":"task_local_123","model":"dall-e","prompt":"draw a cat","stream":false}`, string(persisted.Body))
	require.Equal(t, "task_local_123", persisted.ClientTaskID)
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
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/image-tasks/generations", nil)
	ctx.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.CreateBodyStorage([]byte(`{"prompt":"draw","client_task_id":"task_local_fallback"}`))
	require.NoError(t, err)
	defer storage.Close()
	ctx.Set(common.KeyBodyStorage, storage)

	persisted, err := persistImageTaskRequest(ctx, relayconstant.RelayModeImagesGenerations, dto.ImageTaskDefaultGenerationModel)
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

func TestImageTaskToResponseLoadsStoredResultFile(t *testing.T) {
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

	resp := imageTaskToResponse(task)

	require.JSONEq(t, string(result), string(resp.Result))
}

func TestImageTaskToResponseHidesPendingSettlementResult(t *testing.T) {
	result := []byte(`{"data":[{"b64_json":"not-yet-public"}],"usage":{"total_tokens":1}}`)
	task := &model.Task{
		TaskID:           "task_pending_settlement",
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusPending,
		Progress:         "100%",
		Data:             result,
	}

	resp := imageTaskToResponse(task)

	require.Equal(t, dto.ImageTaskStatusRunning, resp.Status)
	require.Empty(t, resp.Result)
	require.Empty(t, resp.Error)
}

func TestImageTaskToResponseHidesAppliedSettlementResult(t *testing.T) {
	result := []byte(`{"data":[{"b64_json":"applied-not-settled"}],"usage":{"total_tokens":1}}`)
	task := &model.Task{
		TaskID:           "task_applied_settlement",
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusApplied,
		Progress:         "100%",
		Data:             result,
	}

	resp := imageTaskToResponse(task)

	require.Equal(t, dto.ImageTaskStatusRunning, resp.Status)
	require.Empty(t, resp.Result)
	require.Empty(t, resp.Error)
}

func TestImageTaskToResponseMarksSettlementReviewAsFailed(t *testing.T) {
	task := &model.Task{
		TaskID:           "task_settlement_review",
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusReview,
		Progress:         "100%",
		FailReason:       "image task settlement requires manual review",
		Data:             json.RawMessage(`{"data":[{"url":"https://example.com/image.png"}]}`),
	}

	resp := imageTaskToResponse(task)

	require.Equal(t, dto.ImageTaskStatusFailed, resp.Status)
	require.Equal(t, "image task settlement requires manual review", resp.Error)
	require.Empty(t, resp.Result)
}

func TestImageTaskToResponseMarksExpiredStoredResultFileExpired(t *testing.T) {
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

	resp := imageTaskToResponse(task)

	require.Empty(t, resp.Result)
	require.Equal(t, imageTaskResultExpiredMessage, resp.Error)
}

func TestImageTaskToResponseMarksMissingStoredResultFileExpired(t *testing.T) {
	task := &model.Task{
		TaskID: "task_missing_result_file",
		Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			ResultBodyPath: "missing-result.json",
		},
		Data: []byte(`{"_newapi_result_file":true}`),
	}

	resp := imageTaskToResponse(task)

	require.Empty(t, resp.Result)
	require.Equal(t, imageTaskResultExpiredMessage, resp.Error)
}

func TestImageTaskToResponseMarksStoredResultMarkerWithoutPathExpired(t *testing.T) {
	task := &model.Task{
		TaskID: "task_marker_only",
		Status: model.TaskStatusSuccess,
		Data:   []byte(`{"_newapi_result_file":true}`),
	}

	resp := imageTaskToResponse(task)

	require.Empty(t, resp.Result)
	require.Equal(t, imageTaskResultExpiredMessage, resp.Error)
}

func TestImageTaskToResponseDoesNotTreatMarkerTextAsPlaceholder(t *testing.T) {
	result := []byte(`{"data":[{"revised_prompt":"literal _newapi_result_file text"}]}`)
	task := &model.Task{
		TaskID: "task_marker_text",
		Status: model.TaskStatusSuccess,
		Data:   result,
	}

	resp := imageTaskToResponse(task)

	require.JSONEq(t, string(result), string(resp.Result))
	require.Empty(t, resp.Error)
}

func TestGetImageTasksKeepsBatchWhenStoredResultMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})

	require.NoError(t, db.Create(&model.Task{
		TaskID:   "task_ok",
		Platform: constant.TaskPlatformImage,
		UserId:   1,
		Status:   model.TaskStatusSuccess,
		Data:     []byte(`{"data":[{"b64_json":"ok"}]}`),
	}).Error)
	require.NoError(t, db.Create(&model.Task{
		TaskID:   "task_expired",
		Platform: constant.TaskPlatformImage,
		UserId:   1,
		Status:   model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			ResultBodyPath: "missing-result.json",
		},
		Data: []byte(`{"_newapi_result_file":true}`),
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/image-tasks?ids=task_ok,task_expired", nil)
	ctx.Set("id", 1)

	GetImageTasks(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload struct {
		Data  []dto.ImageTaskResponse         `json:"data"`
		Items []dto.ImageTaskGPTImage2APIItem `json:"items"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Len(t, payload.Data, 2)
	byID := make(map[string]dto.ImageTaskResponse, len(payload.Data))
	for _, item := range payload.Data {
		byID[item.TaskID] = item
	}
	itemsByID := make(map[string]dto.ImageTaskGPTImage2APIItem, len(payload.Items))
	for _, item := range payload.Items {
		itemsByID[item.TaskID] = item
	}
	require.JSONEq(t, `{"data":[{"b64_json":"ok"}]}`, string(byID["task_ok"].Result))
	require.JSONEq(t, `[{"b64_json":"ok"}]`, string(itemsByID["task_ok"].Data))
	require.Equal(t, imageTaskResultExpiredMessage, byID["task_expired"].Error)
	require.Empty(t, byID["task_expired"].Result)
	require.Equal(t, imageTaskResultExpiredMessage, itemsByID["task_expired"].Error)
	require.Empty(t, itemsByID["task_expired"].Data)
}

func TestGetImageTasksSupportsClientTaskIDAndCanHideResultData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})

	require.NoError(t, db.Create(&model.Task{
		TaskID:       "upstream_task_123",
		ClientTaskID: "client_task_123",
		Platform:     constant.TaskPlatformImage,
		UserId:       1,
		Status:       model.TaskStatusSuccess,
		Data:         []byte(`{"data":[{"b64_json":"hidden"}]}`),
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/image-tasks?client_task_id=client_task_123&include_image_data=false", nil)
	ctx.Set("id", 1)

	GetImageTasks(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload struct {
		Data  []dto.ImageTaskResponse         `json:"data"`
		Items []dto.ImageTaskGPTImage2APIItem `json:"items"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Len(t, payload.Data, 1)
	require.Len(t, payload.Items, 1)
	require.Equal(t, "upstream_task_123", payload.Data[0].TaskID)
	require.Equal(t, "client_task_123", payload.Data[0].ClientTaskID)
	require.Empty(t, payload.Data[0].Result)
	require.Equal(t, "upstream_task_123", payload.Items[0].ID)
	require.Equal(t, "client_task_123", payload.Items[0].ClientTaskID)
	require.Empty(t, payload.Items[0].Data)
	require.Empty(t, payload.Items[0].Result)
}

func TestGetImageTasksReturnsItemsAndMissingIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})

	require.NoError(t, db.Create(&model.Task{
		TaskID:   "task_existing",
		Platform: constant.TaskPlatformImage,
		UserId:   1,
		Status:   model.TaskStatusSuccess,
		Data:     []byte(`{"data":[{"url":"https://example.com/a.png"}]}`),
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/image-tasks?ids=task_existing,task_missing&include_image_data=false", nil)
	ctx.Set("id", 1)

	GetImageTasks(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload struct {
		Data       []dto.ImageTaskResponse         `json:"data"`
		Items      []dto.ImageTaskGPTImage2APIItem `json:"items"`
		MissingIDs []string                        `json:"missing_ids"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Len(t, payload.Data, 1)
	require.Len(t, payload.Items, 1)
	require.Equal(t, "task_existing", payload.Data[0].TaskID)
	require.Equal(t, "task_existing", payload.Items[0].ID)
	require.Equal(t, "task_existing", payload.Items[0].TaskID)
	require.Empty(t, payload.Items[0].Data)
	require.Equal(t, []string{"task_missing"}, payload.MissingIDs)
}

func TestGetImageTasksRejectsTooManyIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rawIDs := ""
	for i := 0; i <= maxImageTaskBatchQueryIDs; i++ {
		if i > 0 {
			rawIDs += ","
		}
		rawIDs += "task_" + strconv.Itoa(i)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/image-tasks?ids="+rawIDs, nil)
	ctx.Set("id", 1)

	GetImageTasks(ctx)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
}
