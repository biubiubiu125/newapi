package controller

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func countMultipartTempFiles(t *testing.T) int {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), "multipart-*"))
	require.NoError(t, err)
	return len(matches)
}

// isolateMultipartTempDir 把 multipart 临时文件重定向到本用例独占目录，
// 避免与其他包并行运行时互相统计到对方的临时文件。
func isolateMultipartTempDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	t.Setenv("TMP", dir)
	t.Setenv("TEMP", dir)
}

func TestPublicImageTaskIdempotencyCandidateCachesMultipartFormUntilRequestCleanup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	isolateMultipartTempDir(t)
	oldMaxFileDownloadMB := constant.MaxFileDownloadMB
	constant.MaxFileDownloadMB = 1
	t.Cleanup(func() { constant.MaxFileDownloadMB = oldMaxFileDownloadMB })

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-1"))
	require.NoError(t, writer.WriteField("prompt", "edit this image"))
	require.NoError(t, writer.WriteField("client_task_id", "leak-check-1"))
	part, err := writer.CreateFormFile("image", "input.png")
	require.NoError(t, err)
	// 超过 multipart 内存阈值，强制落盘产生临时文件。
	_, err = part.Write(bytes.Repeat([]byte("a"), (2<<20)+1))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/image-tasks/edits", nil)
	ctx.Request.Header.Set("Content-Type", writer.FormDataContentType())
	storage, err := common.CreateBodyStorage(body.Bytes())
	require.NoError(t, err)
	t.Cleanup(func() { storage.Close() })
	ctx.Set(common.KeyBodyStorage, storage)

	before := countMultipartTempFiles(t)
	clientTaskID, err := publicImageTaskIdempotencyCandidate(ctx, relayconstant.RelayModeImagesEdits)
	require.NoError(t, err)
	require.Equal(t, "leak-check-1", clientTaskID)
	require.NotNil(t, ctx.Request.MultipartForm)
	require.Greater(t, countMultipartTempFiles(t), before)
	require.NoError(t, ctx.Request.MultipartForm.RemoveAll())
	ctx.Request.MultipartForm = nil
	require.Equal(t, before, countMultipartTempFiles(t))
}

func TestPublicImageTaskResponseWaitsForSettlement(t *testing.T) {
	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_public_pending",
		ClientTaskID:     "client_pending",
		Platform:         constant.TaskPlatformImage,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusPending,
		Progress:         "100%",
		FinishTime:       now,
		ResultExpiresAt:  now + 12*60*60,
	}
	task.SetData(map[string]any{"data": []any{map[string]any{"url": "https://example.com/image.png"}}})

	response := publicImageTaskResponse(task, now)

	require.Equal(t, "finalizing", response.Status)
	require.False(t, response.ResultAvailable)
	require.Equal(t, int64(0), response.ResultExpiresAt)
}

func TestPublicImageTaskResponseExposesSettledResultMetadata(t *testing.T) {
	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_public_completed",
		ClientTaskID:     "client_completed",
		Platform:         constant.TaskPlatformImage,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusSettled,
		Progress:         "100%",
		FinishTime:       now,
		ResultExpiresAt:  now + 12*60*60,
	}
	task.SetData(map[string]any{"data": []any{map[string]any{"b64_json": "aGVsbG8="}}})

	response := publicImageTaskResponse(task, now)

	require.Equal(t, "completed", response.Status)
	require.True(t, response.ResultAvailable)
	require.Equal(t, task.ResultExpiresAt, response.ResultExpiresAt)
}

func TestPublicImageTaskResponseHidesInternalFailureReason(t *testing.T) {
	task := &model.Task{
		TaskID:     "task_public_failed_safe_message",
		Platform:   constant.TaskPlatformImage,
		Status:     model.TaskStatusFailure,
		FailReason: `upstream failed: key=sk-provider-secret base_url=http://10.0.0.8:8080`,
	}

	response := publicImageTaskResponse(task, time.Now().Unix())

	require.Equal(t, "failed", response.Status)
	require.NotNil(t, response.Error)
	require.Equal(t, "image_task_failed", response.Error.Code)
	require.Equal(t, "image task failed", response.Error.Message)
	require.NotContains(t, response.Error.Message, "sk-provider-secret")
	require.Equal(t, `upstream failed: key=sk-provider-secret base_url=http://10.0.0.8:8080`, task.FailReason)
}

func TestPublicImageTaskResultExpiryClampsLegacyStoredExpiryToTwelveHours(t *testing.T) {
	oldRetention := constant.ImageTaskResultRetentionMinutes
	constant.ImageTaskResultRetentionMinutes = 720
	t.Cleanup(func() {
		constant.ImageTaskResultRetentionMinutes = oldRetention
	})

	finishedAt := time.Now().Add(-time.Hour).Unix()
	task := &model.Task{
		FinishTime: finishedAt,
		PrivateData: model.TaskPrivateData{
			ResultExpiresAt: finishedAt + 24*60*60,
		},
	}

	require.Equal(t, finishedAt+12*60*60, publicImageTaskResultExpiry(task))
}

func TestPublicImageTaskResultExpiryDoesNotExtendAtSettlement(t *testing.T) {
	oldRetention := constant.ImageTaskResultRetentionMinutes
	constant.ImageTaskResultRetentionMinutes = 720
	t.Cleanup(func() {
		constant.ImageTaskResultRetentionMinutes = oldRetention
	})

	now := time.Now().Unix()
	settlementExpiry := now + 12*60*60
	resultStoredAt := now - 13*60*60
	task := &model.Task{
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusSettled,
		FinishTime:       resultStoredAt,
		ResultExpiresAt:  settlementExpiry,
		PrivateData: model.TaskPrivateData{
			ResultStoredAt:  resultStoredAt,
			ResultExpiresAt: settlementExpiry,
		},
	}

	require.Equal(t, resultStoredAt+12*60*60, publicImageTaskResultExpiry(task))
}

func TestPublicImageTaskResultRejectsExpiredAndAcknowledgedResults(t *testing.T) {
	now := time.Now().Unix()
	tests := []struct {
		name string
		task *model.Task
	}{
		{
			name: "expired",
			task: &model.Task{
				Status:           model.TaskStatusSuccess,
				SettlementStatus: model.TaskSettlementStatusSettled,
				ResultExpiresAt:  now - 1,
			},
		},
		{
			name: "ack grace elapsed",
			task: &model.Task{
				Status:               model.TaskStatusSuccess,
				SettlementStatus:     model.TaskSettlementStatusSettled,
				ResultExpiresAt:      now + 60,
				ResultAcknowledgedAt: now - 121,
				ResultDeleteAfter:    now - 1,
			},
		},
		{
			name: "cleaned",
			task: &model.Task{
				Status:           model.TaskStatusSuccess,
				SettlementStatus: model.TaskSettlementStatusSettled,
				ResultExpiresAt:  now + 60,
				ResultCleanedAt:  now - 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.False(t, publicImageTaskResultAvailable(tt.task, now))
		})
	}
}

func TestGetPublicImageTaskResultServesSmallStoredFileAtomically(t *testing.T) {
	_, cleanup := setupImageTaskControllerTestDB(t)
	t.Cleanup(cleanup)
	gin.SetMode(gin.TestMode)

	result := []byte(`{"data":[{"b64_json":"streamed-public-result"}],"usage":{"total_tokens":3}}`)
	path, err := common.WriteImageTaskResultCacheFile(result)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(path) })
	sum := sha256.Sum256(result)

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:                  "task_stream_public_result",
		Platform:                constant.TaskPlatformImage,
		UserId:                  8,
		Status:                  model.TaskStatusSuccess,
		SettlementStatus:        model.TaskSettlementStatusSettled,
		FinishTime:              now - 10,
		ResultExpiresAt:         now + 3600,
		ImageTaskResultStored:   true,
		ImageTaskResultStoredAt: now - 10,
		PrivateData: model.TaskPrivateData{
			TokenId:           80,
			PublicImageTask:   true,
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

	engine := gin.New()
	engine.GET("/v1/image-tasks/:task_id/result", func(c *gin.Context) {
		c.Set("id", 8)
		c.Set("token_id", 80)
		GetPublicImageTaskResult(c)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/image-tasks/"+task.TaskID+"/result", nil)
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.JSONEq(t, string(result), recorder.Body.String())
	// Small payloads use c.Data (atomic body); Content-Length is still present.
	require.Equal(t, strconv.Itoa(len(result)), recorder.Header().Get("Content-Length"))
	require.Equal(t, hex.EncodeToString(sum[:]), recorder.Header().Get("X-NewAPI-Result-SHA256"))
}

func TestWritePublicImageTaskStoredResultStreamsLargePayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldInline := constant.ImageTaskResultInlineMaxMB
	constant.ImageTaskResultInlineMaxMB = 1 // 1 MiB atomic threshold
	t.Cleanup(func() { constant.ImageTaskResultInlineMaxMB = oldInline })

	// Just over the atomic threshold so the stream path is used.
	payload := append([]byte(`{"data":[{"b64_json":"`), bytes.Repeat([]byte("A"), (1<<20)+64)...)
	payload = append(payload, []byte(`"}]}`)...)
	path, err := common.WriteImageTaskResultCacheFile(payload)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(path) })
	sum := sha256.Sum256(payload)

	file, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })

	task := &model.Task{
		TaskID: "task_large_stream_result",
		PrivateData: model.TaskPrivateData{
			ResultBodySize:   int64(len(payload)),
			ResultBodySHA256: hex.EncodeToString(sum[:]),
		},
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/image-tasks/task_large_stream_result/result", nil)

	writePublicImageTaskStoredResult(ctx, task, file)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, strconv.Itoa(len(payload)), recorder.Header().Get("Content-Length"))
	require.Equal(t, hex.EncodeToString(sum[:]), recorder.Header().Get("X-NewAPI-Result-SHA256"))
	require.Equal(t, payload, recorder.Body.Bytes())
}

func TestGetPublicImageTaskResultEnforcesDownloadConcurrency(t *testing.T) {
	_, cleanup := setupImageTaskControllerTestDB(t)
	t.Cleanup(cleanup)
	gin.SetMode(gin.TestMode)

	oldGlobal := constant.ImageTaskResultDownloadConcurrency
	oldToken := constant.ImageTaskResultDownloadTokenConcurrency
	constant.ImageTaskResultDownloadConcurrency = 1
	constant.ImageTaskResultDownloadTokenConcurrency = 1
	t.Cleanup(func() {
		constant.ImageTaskResultDownloadConcurrency = oldGlobal
		constant.ImageTaskResultDownloadTokenConcurrency = oldToken
		service.ResetImageTaskResultDownloadLimiterForTest()
	})
	service.ResetImageTaskResultDownloadLimiterForTest()

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_download_limit",
		Platform:         constant.TaskPlatformImage,
		UserId:           8,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusSettled,
		ResultExpiresAt:  now + 3600,
		PrivateData: model.TaskPrivateData{
			TokenId:         80,
			PublicImageTask: true,
		},
	}
	task.SetData(map[string]any{"data": []any{map[string]any{"url": "https://example.com/limit.png"}}})
	require.NoError(t, task.Insert())

	release, err := service.AcquireImageTaskResultDownloadSlot(80)
	require.NoError(t, err)
	t.Cleanup(release)

	engine := gin.New()
	engine.GET("/v1/image-tasks/:task_id/result", func(c *gin.Context) {
		c.Set("id", 8)
		c.Set("token_id", 80)
		GetPublicImageTaskResult(c)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/image-tasks/"+task.TaskID+"/result", nil)
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code, recorder.Body.String())
	require.Equal(t, "1", recorder.Header().Get("Retry-After"))
	require.Contains(t, recorder.Body.String(), "rate_limit_exceeded")
}

func TestPublicImageTaskMetadataHydratesResultStoredAt(t *testing.T) {
	_, cleanup := setupImageTaskControllerTestDB(t)
	t.Cleanup(cleanup)

	now := time.Now().Unix()
	storedAt := now - 30*60
	task := &model.Task{
		TaskID:                  "task_status_result_stored_at",
		Platform:                constant.TaskPlatformImage,
		UserId:                  8,
		Status:                  model.TaskStatusSuccess,
		SettlementStatus:        model.TaskSettlementStatusSettled,
		FinishTime:              storedAt + 5,
		ResultExpiresAt:         storedAt + 12*60*60 + 1000, // intentionally longer; expiry must clamp to storedAt+12h
		ImageTaskResultStoredAt: storedAt,
		PublicImageTask:         true,
		PublicImageTaskTokenID:  80,
		ImageTaskResultStored:   false,
	}
	task.SetData(map[string]any{"data": []any{map[string]any{"url": "https://example.com/status.png"}}})
	require.NoError(t, task.Insert())

	loaded, exists, err := model.GetPublicImageTaskByTaskID(8, 80, task.TaskID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, storedAt, loaded.PrivateData.ResultStoredAt)
	require.Equal(t, storedAt+12*60*60, publicImageTaskResultExpiry(loaded))
}

func TestGetPublicImageTaskResultRequiresOwnerToken(t *testing.T) {
	_, cleanup := setupImageTaskControllerTestDB(t)
	t.Cleanup(cleanup)
	gin.SetMode(gin.TestMode)

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_private_result",
		Platform:         constant.TaskPlatformImage,
		UserId:           7,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusSettled,
		ResultExpiresAt:  now + 3600,
		PrivateData: model.TaskPrivateData{
			TokenId:         70,
			PublicImageTask: true,
		},
	}
	task.SetData(map[string]any{"data": []any{map[string]any{"url": "https://example.com/private.png"}}})
	require.NoError(t, task.Insert())

	engine := gin.New()
	engine.GET("/v1/image-tasks/:task_id/result", func(c *gin.Context) {
		c.Set("id", 7)
		c.Set("token_id", 71)
		GetPublicImageTaskResult(c)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/image-tasks/"+task.TaskID+"/result", nil)
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "private.png")
}

func TestGetPublicImageTaskResultReportsUnreadableStoredResultAsRetryable(t *testing.T) {
	_, cleanup := setupImageTaskControllerTestDB(t)
	t.Cleanup(cleanup)
	gin.SetMode(gin.TestMode)

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_unreadable_result",
		Platform:         constant.TaskPlatformImage,
		UserId:           9,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusSettled,
		FinishTime:       now - 60,
		ResultExpiresAt:  now + 3600,
		PrivateData: model.TaskPrivateData{
			TokenId:         90,
			PublicImageTask: true,
			// 结果仍登记为文件态且未被清理，但文件读不出来：共享缓存抖动的典型形态。
			ResultBodyPath:  "image-task-result-does-not-exist.json",
			ResultExpiresAt: now + 3600,
		},
	}
	task.Data = []byte(`{"_newapi_result_file":true}`)
	require.NoError(t, task.Insert())

	engine := gin.New()
	engine.GET("/v1/image-tasks/:task_id/result", func(c *gin.Context) {
		c.Set("id", 9)
		c.Set("token_id", 90)
		GetPublicImageTaskResult(c)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/image-tasks/"+task.TaskID+"/result", nil)
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
	require.Equal(t, "5", recorder.Header().Get("Retry-After"))
	require.Contains(t, recorder.Body.String(), "result_temporarily_unavailable")
	require.NotContains(t, recorder.Body.String(), "result_expired")
}

func TestGetPublicImageTaskResultReportsCleanedStoredResultAsGone(t *testing.T) {
	_, cleanup := setupImageTaskControllerTestDB(t)
	t.Cleanup(cleanup)
	gin.SetMode(gin.TestMode)

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_cleaned_result",
		Platform:         constant.TaskPlatformImage,
		UserId:           9,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusSettled,
		FinishTime:       now - 60,
		ResultExpiresAt:  now + 3600,
		PrivateData: model.TaskPrivateData{
			TokenId:         90,
			PublicImageTask: true,
			ResultBodyPath:  "image-task-result-does-not-exist.json",
			ResultExpiresAt: now + 3600,
		},
	}
	task.Data = []byte(`{"_newapi_result_file":true}`)
	require.NoError(t, task.Insert())
	// 已登记清理的任务才允许对外报永久过期。
	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", task.ID).
		Update("result_cleaned_at", now-1).Error)

	engine := gin.New()
	engine.GET("/v1/image-tasks/:task_id/result", func(c *gin.Context) {
		c.Set("id", 9)
		c.Set("token_id", 90)
		GetPublicImageTaskResult(c)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/image-tasks/"+task.TaskID+"/result", nil)
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusGone, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "result_expired")
}

func TestPublicImageTaskAuthorizationRejectsLegacyTaskWithoutTokenOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("id", 7)
	ctx.Set("token_id", 70)
	task := &model.Task{
		Platform: constant.TaskPlatformImage,
		UserId:   7,
	}

	require.False(t, publicImageTaskAuthorized(ctx, task))
}

func TestPublicImageTaskAuthorizationRejectsInternalImageTask(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("id", 7)
	ctx.Set("token_id", 70)
	task := &model.Task{
		Platform: constant.TaskPlatformImage,
		UserId:   7,
		PrivateData: model.TaskPrivateData{
			TokenId: 70,
		},
	}

	require.False(t, publicImageTaskAuthorized(ctx, task))
}

func TestCancelPublicImageTaskRejectsStartedTask(t *testing.T) {
	_, cleanup := setupImageTaskControllerTestDB(t)
	t.Cleanup(cleanup)
	gin.SetMode(gin.TestMode)

	task := &model.Task{
		TaskID:      "task_started",
		Platform:    constant.TaskPlatformImage,
		UserId:      8,
		Status:      model.TaskStatusInProgress,
		Progress:    "1%",
		StartTime:   time.Now().Unix(),
		LockOwner:   "worker-1",
		LockUntil:   time.Now().Add(time.Minute).Unix(),
		PrivateData: model.TaskPrivateData{TokenId: 80, PublicImageTask: true},
	}
	require.NoError(t, task.Insert())

	engine := gin.New()
	engine.POST("/v1/image-tasks/:task_id/cancel", func(c *gin.Context) {
		c.Set("id", 8)
		c.Set("token_id", 80)
		CancelPublicImageTask(c)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/image-tasks/"+task.TaskID+"/cancel", nil)
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, recorder.Body.String(), "not_cancellable")
}

func TestCancelPublicImageTaskRejectsQueuedTaskWithUpstreamID(t *testing.T) {
	_, cleanup := setupImageTaskControllerTestDB(t)
	t.Cleanup(cleanup)
	gin.SetMode(gin.TestMode)

	task := &model.Task{
		TaskID:   "task_queued_with_upstream",
		Platform: constant.TaskPlatformImage,
		UserId:   8,
		Status:   model.TaskStatusQueued,
		Progress: "0%",
		PrivateData: model.TaskPrivateData{
			TokenId:         80,
			PublicImageTask: true,
			UpstreamTaskID:  "upstream-x",
		},
	}
	require.NoError(t, task.Insert())

	engine := gin.New()
	engine.POST("/v1/image-tasks/:task_id/cancel", func(c *gin.Context) {
		c.Set("id", 8)
		c.Set("token_id", 80)
		CancelPublicImageTask(c)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/image-tasks/"+task.TaskID+"/cancel", nil)
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, recorder.Body.String(), "not_cancellable")
	reloaded, exists, err := model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, model.TaskStatus(model.TaskStatusQueued), reloaded.Status)
	require.Equal(t, "upstream-x", reloaded.PrivateData.UpstreamTaskID)
}

func TestCancelPublicImageTaskRejectsQueuedTaskWithSyncSubmissionStarted(t *testing.T) {
	_, cleanup := setupImageTaskControllerTestDB(t)
	t.Cleanup(cleanup)
	gin.SetMode(gin.TestMode)

	task := &model.Task{
		TaskID:                  "task_queued_sync_submission_started",
		Platform:                constant.TaskPlatformImage,
		UserId:                  8,
		Status:                  model.TaskStatusQueued,
		Progress:                "0%",
		SyncSubmissionStartedAt: time.Now().Unix(),
		PrivateData: model.TaskPrivateData{
			TokenId:         80,
			PublicImageTask: true,
		},
	}
	require.NoError(t, task.Insert())

	engine := gin.New()
	engine.POST("/v1/image-tasks/:task_id/cancel", func(c *gin.Context) {
		c.Set("id", 8)
		c.Set("token_id", 80)
		CancelPublicImageTask(c)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/image-tasks/"+task.TaskID+"/cancel", nil)
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, recorder.Body.String(), "not_cancellable")
	reloaded, exists, err := model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, model.TaskStatus(model.TaskStatusQueued), reloaded.Status)
	require.Positive(t, reloaded.SyncSubmissionStartedAt)
}

func TestListPublicImageTasksReportsNotFoundIDs(t *testing.T) {
	_, cleanup := setupImageTaskControllerTestDB(t)
	t.Cleanup(cleanup)
	gin.SetMode(gin.TestMode)

	now := time.Now().Unix()
	owned := &model.Task{
		TaskID:                 "task_list_owned",
		Platform:               constant.TaskPlatformImage,
		UserId:                 8,
		Status:                 model.TaskStatusQueued,
		CreatedAt:              now,
		UpdatedAt:              now,
		PublicImageTask:        true,
		PublicImageTaskTokenID: 80,
		PrivateData: model.TaskPrivateData{
			TokenId:         80,
			PublicImageTask: true,
		},
	}
	otherToken := &model.Task{
		TaskID:                 "task_list_other_token",
		Platform:               constant.TaskPlatformImage,
		UserId:                 8,
		Status:                 model.TaskStatusQueued,
		CreatedAt:              now,
		UpdatedAt:              now,
		PublicImageTask:        true,
		PublicImageTaskTokenID: 81,
		PrivateData: model.TaskPrivateData{
			TokenId:         81,
			PublicImageTask: true,
		},
	}
	require.NoError(t, owned.Insert())
	require.NoError(t, otherToken.Insert())

	engine := gin.New()
	engine.GET("/v1/image-tasks", func(c *gin.Context) {
		c.Set("id", 8)
		c.Set("token_id", 80)
		ListPublicImageTasks(c)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/v1/image-tasks?ids=task_list_owned,task_list_other_token,task_list_missing",
		nil,
	)
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var payload dto.PublicImageTaskList
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Len(t, payload.Data, 1)
	require.Equal(t, "task_list_owned", payload.Data[0].TaskID)
	require.Equal(t, []string{"task_list_other_token", "task_list_missing"}, payload.NotFoundIDs)
}

func TestCancelPublicImageTaskAllowsExpiredQueuedLeaseBeforeSubmission(t *testing.T) {
	db, cleanup := setupImageTaskControllerTestDB(t)
	t.Cleanup(cleanup)
	require.NoError(t, db.AutoMigrate(&model.TaskSettlementRecord{}))
	gin.SetMode(gin.TestMode)

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:      "task_expired_queued_lease",
		Platform:    constant.TaskPlatformImage,
		UserId:      8,
		Status:      model.TaskStatusQueued,
		Progress:    "0%",
		LockOwner:   "stale-worker",
		LockUntil:   now - 1,
		PrivateData: model.TaskPrivateData{TokenId: 80, PublicImageTask: true},
	}
	require.NoError(t, task.Insert())

	engine := gin.New()
	engine.POST("/v1/image-tasks/:task_id/cancel", func(c *gin.Context) {
		c.Set("id", 8)
		c.Set("token_id", 80)
		CancelPublicImageTask(c)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/image-tasks/"+task.TaskID+"/cancel", nil)
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), `"status":"cancelled"`)
	reloaded, exists, err := model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status)
	require.NotZero(t, reloaded.PrivateData.CancelledAt)
	require.Empty(t, reloaded.LockOwner)
	require.Zero(t, reloaded.LockUntil)
}

func TestCleanupCancelledPublicImageTaskFilesKeepsForeignNodeRequestFile(t *testing.T) {
	_, cleanup := setupImageTaskControllerTestDB(t)
	t.Cleanup(cleanup)
	gin.SetMode(gin.TestMode)

	oldNodeName := common.NodeName
	common.NodeName = "api-node-b"
	t.Cleanup(func() { common.NodeName = oldNodeName })

	bodyPath := filepath.Join(t.TempDir(), "request-body.json")
	require.NoError(t, os.WriteFile(bodyPath, []byte(`{"prompt":"private input"}`), 0o600))
	now := time.Now().Unix()
	task := &model.Task{
		TaskID:      "task_cancel_foreign_node",
		Platform:    constant.TaskPlatformImage,
		UserId:      8,
		Status:      model.TaskStatusFailure,
		StorageNode: "api-node-a",
		PrivateData: model.TaskPrivateData{
			TokenId:         80,
			PublicImageTask: true,
			CancelledAt:     now,
			RequestBodyPath: bodyPath,
			NodeName:        "api-node-a",
		},
	}
	require.NoError(t, task.Insert())

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	cleanupCancelledPublicImageTaskFiles(ctx, task)

	_, err := os.Stat(bodyPath)
	require.NoError(t, err)
	reloaded, exists, err := model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, bodyPath, reloaded.PrivateData.RequestBodyPath)
	require.True(t, reloaded.RequestCleanupPending)
	require.LessOrEqual(t, reloaded.RequestDeleteAfter, time.Now().Unix())
}

// 取消清理与后台退款恢复在多节点上并发运行。清理只负责自己那几列，不能整体覆盖
// private_data，否则会把并发退款刚写入的 settlement_error 审计信息冲掉。
func TestCleanupCancelledPublicImageTaskFilesKeepsConcurrentSettlementAudit(t *testing.T) {
	_, cleanup := setupImageTaskControllerTestDB(t)
	t.Cleanup(cleanup)
	gin.SetMode(gin.TestMode)

	oldNodeName := common.NodeName
	common.NodeName = "api-node-a"
	t.Cleanup(func() { common.NodeName = oldNodeName })

	bodyPath := filepath.Join(t.TempDir(), "request-body.json")
	require.NoError(t, os.WriteFile(bodyPath, []byte(`{"prompt":"private input"}`), 0o600))
	resultPath := filepath.Join(t.TempDir(), "result-body.json")
	require.NoError(t, os.WriteFile(resultPath, []byte(`{"data":[]}`), 0o600))

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:   "task_cancel_concurrent_audit",
		Platform: constant.TaskPlatformImage,
		UserId:   8,
		Status:   model.TaskStatusFailure,
		PrivateData: model.TaskPrivateData{
			TokenId:         80,
			PublicImageTask: true,
			CancelledAt:     now,
			RequestBodyPath: bodyPath,
			ResultBodyPath:  resultPath,
			NodeName:        "api-node-a",
		},
	}
	require.NoError(t, task.Insert())

	// 模拟并发退款恢复在本节点读到任务之后写入了审计信息。
	persisted, exists, err := model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	persisted.PrivateData.SettlementError = "concurrent refund review marker"
	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", task.ID).
		Update("private_data", persisted.PrivateData).Error)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	cleanupCancelledPublicImageTaskFiles(ctx, task)

	reloaded, exists, err := model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, "concurrent refund review marker", reloaded.PrivateData.SettlementError)
	require.Empty(t, reloaded.PrivateData.ResultBodyPath)
	require.Empty(t, reloaded.PrivateData.RequestBodyPath)
	_, statErr := os.Stat(resultPath)
	require.True(t, os.IsNotExist(statErr))
	_, statErr = os.Stat(bodyPath)
	require.True(t, os.IsNotExist(statErr))
}

func TestAcknowledgePublicImageTaskResultIsIdempotent(t *testing.T) {
	_, cleanup := setupImageTaskControllerTestDB(t)
	t.Cleanup(cleanup)
	gin.SetMode(gin.TestMode)

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_ack_idempotent",
		Platform:         constant.TaskPlatformImage,
		UserId:           9,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusSettled,
		FinishTime:       now,
		ResultExpiresAt:  now + 12*60*60,
		PrivateData:      model.TaskPrivateData{TokenId: 90, PublicImageTask: true},
	}
	task.SetData(map[string]any{"data": []any{map[string]any{"url": "https://example.com/ack.png"}}})
	require.NoError(t, task.Insert())

	engine := gin.New()
	engine.POST("/v1/image-tasks/:task_id/ack", func(c *gin.Context) {
		c.Set("id", 9)
		c.Set("token_id", 90)
		AcknowledgePublicImageTaskResult(c)
	})
	ack := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/v1/image-tasks/"+task.TaskID+"/ack", nil)
		engine.ServeHTTP(recorder, request)
		return recorder
	}

	first := ack()
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	reloaded, exists, err := model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Positive(t, reloaded.ResultAcknowledgedAt)
	require.Equal(t, reloaded.ResultAcknowledgedAt+int64(publicImageTaskAckGrace.Seconds()), reloaded.ResultDeleteAfter)
	acknowledgedAt := reloaded.ResultAcknowledgedAt
	deleteAfter := reloaded.ResultDeleteAfter

	// ACK 响应必须把 result_expires_at 收敛到 2 分钟缓冲，而不是继续报 12 小时。
	var firstPayload dto.PublicImageTask
	require.NoError(t, common.Unmarshal(first.Body.Bytes(), &firstPayload))
	require.Equal(t, deleteAfter, firstPayload.ResultExpiresAt)
	require.Equal(t, acknowledgedAt, firstPayload.ResultAcknowledgedAt)
	require.True(t, firstPayload.ResultAvailable)
	require.Equal(t, deleteAfter, publicImageTaskResultExpiry(reloaded))

	second := ack()
	require.Equal(t, http.StatusOK, second.Code, second.Body.String())
	reloaded, exists, err = model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, acknowledgedAt, reloaded.ResultAcknowledgedAt)
	require.Equal(t, deleteAfter, reloaded.ResultDeleteAfter)
}

func TestPublicImageTaskResponseExposesCancellationRefundLifecycle(t *testing.T) {
	now := time.Now().Unix()
	pending := &model.Task{
		TaskID:        "task_cancel_refund_pending",
		Status:        model.TaskStatusFailure,
		Quota:         100,
		RefundPending: true,
		PrivateData: model.TaskPrivateData{
			CancelledAt: now,
		},
	}

	pendingResponse := publicImageTaskResponse(pending, now)
	require.Equal(t, "cancelling", pendingResponse.Status)
	require.Nil(t, pendingResponse.Error)

	review := &model.Task{
		TaskID:           "task_cancel_refund_review",
		Status:           model.TaskStatusFailure,
		Quota:            100,
		SettlementStatus: model.TaskSettlementStatusReview,
		PrivateData: model.TaskPrivateData{
			CancelledAt: now,
		},
	}
	reviewResponse := publicImageTaskResponse(review, now)
	require.Equal(t, "failed", reviewResponse.Status)
	require.NotNil(t, reviewResponse.Error)
	require.Equal(t, "refund_review", reviewResponse.Error.Code)

	completed := &model.Task{
		TaskID: "task_cancel_refund_completed",
		Status: model.TaskStatusFailure,
		PrivateData: model.TaskPrivateData{
			CancelledAt: now,
		},
	}
	require.Equal(t, "cancelled", publicImageTaskResponse(completed, now).Status)
}

func TestPublicImageTaskResponseExposesFailureRefundLifecycle(t *testing.T) {
	now := time.Now().Unix()

	pending := &model.Task{
		TaskID:        "task_failure_refund_pending",
		Status:        model.TaskStatusFailure,
		Quota:         100,
		RefundPending: true,
	}
	pendingResponse := publicImageTaskResponse(pending, now)
	require.Equal(t, "failed", pendingResponse.Status)
	require.NotNil(t, pendingResponse.Error)
	require.Equal(t, "refund_pending", pendingResponse.Error.Code)

	review := &model.Task{
		TaskID:           "task_failure_refund_review",
		Status:           model.TaskStatusFailure,
		Quota:            100,
		RefundPending:    true,
		SettlementStatus: model.TaskSettlementStatusReview,
	}
	reviewResponse := publicImageTaskResponse(review, now)
	require.Equal(t, "failed", reviewResponse.Status)
	require.NotNil(t, reviewResponse.Error)
	require.Equal(t, "refund_review", reviewResponse.Error.Code)

	executionReview := &model.Task{
		TaskID:           "task_execution_review",
		Status:           model.TaskStatusFailure,
		Quota:            100,
		SettlementStatus: model.TaskSettlementStatusReview,
	}
	executionReviewResponse := publicImageTaskResponse(executionReview, now)
	require.Equal(t, "failed", executionReviewResponse.Status)
	require.NotNil(t, executionReviewResponse.Error)
	require.Equal(t, "settlement_review", executionReviewResponse.Error.Code)
}
