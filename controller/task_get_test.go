package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetTaskReturnsResultURLAndHidesSuccessDiagnostics(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	task := &model.Task{
		UserId:     11,
		TaskID:     "task-get-result",
		Status:     model.TaskStatusSuccess,
		Progress:   "100%",
		FailReason: "provider completed with diagnostics",
	}
	task.PrivateData.ResultURL = "https://provider.example/video.mp4"
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task-get-result", nil)
	ctx.Params = gin.Params{{Key: "key", Value: "task-get-result"}}
	ctx.Set("id", 11)

	GetTask(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, "task-get-result", body["task_id"])
	require.Equal(t, "https://provider.example/video.mp4", body["result_url"])
	require.Equal(t, "", body["fail_reason"])
	require.Equal(t, string(model.TaskStatusSuccess), body["status"])
}

func TestGetTaskMapsImageSettlementReviewToFailure(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	task := &model.Task{
		UserId:           11,
		TaskID:           "task-image-review",
		Platform:         constant.TaskPlatformImage,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SettlementStatus: model.TaskSettlementStatusReview,
	}
	task.PrivateData.ResultURL = "https://provider.example/image.png"
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task-image-review", nil)
	ctx.Params = gin.Params{{Key: "key", Value: "task-image-review"}}
	ctx.Set("id", 11)

	GetTask(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, string(model.TaskStatusFailure), body["status"])
	require.Equal(t, "", body["result_url"])
	require.Equal(t, "", body["fail_reason"])
}

func TestGetTaskKeepsRetryableImageSettlementReviewInProgress(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	now := time.Now().Unix()
	task := &model.Task{
		UserId:           11,
		TaskID:           "task-image-retryable-review",
		Platform:         constant.TaskPlatformImage,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		FailReason:       "image task settlement requires manual review",
		SettlementStatus: model.TaskSettlementStatusReview,
		FinishTime:       now,
		NextPollAt:       now + 60,
		Data:             json.RawMessage(`{"ok":true}`),
	}
	task.PrivateData.ResultURL = "https://provider.example/image.png"
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task-image-retryable-review", nil)
	ctx.Params = gin.Params{{Key: "key", Value: "task-image-retryable-review"}}
	ctx.Set("id", 11)

	GetTask(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, string(model.TaskStatusInProgress), body["status"])
	require.Equal(t, "99%", body["progress"])
	require.Equal(t, float64(0), body["finished_at"])
	require.Equal(t, "", body["result_url"])
	require.Equal(t, "", body["fail_reason"])
}

func TestGetTaskExposesUnrecoverableImageSettlementReviewReason(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	reason := "image task result expired before settlement completed"
	task := &model.Task{
		UserId:           11,
		TaskID:           "task-image-unrecoverable-review",
		Platform:         constant.TaskPlatformImage,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		FailReason:       reason,
		SettlementStatus: model.TaskSettlementStatusReview,
		ResultCleanedAt:  time.Now().Unix(),
	}
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task-image-unrecoverable-review", nil)
	ctx.Params = gin.Params{{Key: "key", Value: "task-image-unrecoverable-review"}}
	ctx.Set("id", 11)

	GetTask(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, string(model.TaskStatusFailure), body["status"])
	require.Equal(t, "", body["result_url"])
	require.Equal(t, reason, body["fail_reason"])
}

func TestGetTaskHidesAccountingInternalFailReason(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	task := &model.Task{
		UserId:     11,
		TaskID:     "task-accounting-leak",
		Status:     model.TaskStatusFailure,
		Progress:   "100%",
		FailReason: "billing accounting failed after task submission: pq: password authentication failed",
	}
	task.PrivateData.SettlementError = "pq: password authentication failed"
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task-accounting-leak", nil)
	ctx.Params = gin.Params{{Key: "key", Value: "task-accounting-leak"}}
	ctx.Set("id", 11)

	GetTask(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, string(model.TaskStatusFailure), body["status"])
	require.Equal(t, model.TaskPublicAccountingFailReason, body["fail_reason"])
	require.NotContains(t, recorder.Body.String(), "pq:")
	require.NotContains(t, recorder.Body.String(), "password authentication")
}
