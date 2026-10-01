package relay

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
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
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func withImageTaskAsyncTimeoutMinutes(t *testing.T, minutes int) {
	t.Helper()
	old := constant.TaskTimeoutMinutes
	constant.TaskTimeoutMinutes = minutes
	t.Cleanup(func() {
		constant.TaskTimeoutMinutes = old
	})
}

func withTempImageTaskCache(t *testing.T) {
	t.Helper()
	oldConfig := common.GetDiskCacheConfig()
	config := oldConfig
	config.Path = t.TempDir()
	common.SetDiskCacheConfig(config)
	t.Cleanup(func() {
		common.SetDiskCacheConfig(oldConfig)
	})
}

func largeValidImageTaskTestPNG(t *testing.T) []byte {
	t.Helper()
	const width = 1024
	const height = 1024
	pngImage := image.NewRGBA(image.Rect(0, 0, width, height))
	var state uint32 = 0x12345678
	for i := 0; i < len(pngImage.Pix); i += 4 {
		state = state*1664525 + 1013904223
		pngImage.Pix[i] = byte(state >> 24)
		state = state*1664525 + 1013904223
		pngImage.Pix[i+1] = byte(state >> 24)
		state = state*1664525 + 1013904223
		pngImage.Pix[i+2] = byte(state >> 24)
		pngImage.Pix[i+3] = color.RGBA{A: 0xff}.A
	}
	var body bytes.Buffer
	require.NoError(t, png.Encode(&body, pngImage))
	require.Greater(t, body.Len(), 1<<20)
	return body.Bytes()
}

func TestCloneImageTaskStringMapDropsCredentialHeaders(t *testing.T) {
	headers := cloneImageTaskStringMap(map[string]string{
		"Authorization":      "Bearer auth-secret",
		"Mj-Api-Secret":      "sk-mj-secret",
		"X-Provider-Secret":  "provider-secret",
		"X-Auth-Token":       "auth-token",
		"X-Goog-Api-Key":     "goog-secret",
		"X-Request-Trace-Id": "trace-123",
	})

	require.Equal(t, map[string]string{"X-Request-Trace-Id": "trace-123"}, headers)
}

func TestTaskModel2DtoMarksImageSettlementReviewAsFailure(t *testing.T) {
	task := &model.Task{
		TaskID:           "task_review_dto",
		Platform:         constant.TaskPlatformImage,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusReview,
	}
	task.PrivateData.ResultURL = "https://provider.example/image.png"

	resp := TaskModel2Dto(task)

	require.Equal(t, string(model.TaskStatusFailure), resp.Status)
	require.Equal(t, model.TaskSettlementStatusReview, resp.SettlementStatus)
	require.Empty(t, resp.ResultURL)
}

func TestPublicTaskStatusKeepsRetryableImageSettlementReviewInProgress(t *testing.T) {
	task := &model.Task{
		TaskID:           "task_retryable_review_status",
		Platform:         constant.TaskPlatformImage,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusReview,
		NextPollAt:       time.Now().Unix() + 60,
		Data:             json.RawMessage(`{"ok":true}`),
	}
	task.PrivateData.ResultURL = "https://provider.example/image.png"

	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), PublicTaskStatus(task))
	require.Empty(t, PublicResultURL(task))
}

func TestTaskModel2DtoKeepsRetryableImageSettlementReviewInProgress(t *testing.T) {
	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_retryable_review_dto",
		Platform:         constant.TaskPlatformImage,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusReview,
		FailReason:       "image task settlement requires manual review",
		Progress:         "100%",
		FinishTime:       now,
		NextPollAt:       now + 60,
		Data:             json.RawMessage(`{"ok":true}`),
	}
	task.PrivateData.ResultURL = "https://provider.example/image.png"

	resp := TaskModel2Dto(task)

	require.Equal(t, string(model.TaskStatusInProgress), resp.Status)
	require.Equal(t, model.TaskSettlementStatusReview, resp.SettlementStatus)
	require.Equal(t, "99%", resp.Progress)
	require.Equal(t, int64(0), resp.FinishTime)
	require.Empty(t, resp.ResultURL)
	require.Empty(t, resp.FailReason)
}

func TestPublicTaskStatusMapsRetryableVideoSettlementReviewToInProgress(t *testing.T) {
	task := &model.Task{
		TaskID:           "task_video_retryable_review_status",
		Platform:         constant.TaskPlatform("gemini"),
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusReview,
		Progress:         "100%",
		FinishTime:       time.Now().Unix(),
		NextPollAt:       time.Now().Unix() + 60,
	}
	task.PrivateData.ResultURL = "https://cdn.example/video.mp4"

	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), PublicTaskStatus(task))
	require.Empty(t, PublicResultURL(task))
	require.Equal(t, "99%", PublicTaskProgress(task))
	require.Zero(t, PublicTaskFinishTime(task))
}

func TestTaskModel2DtoKeepsRetryableVideoSettlementReviewInProgress(t *testing.T) {
	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_video_retryable_review_dto",
		Platform:         constant.TaskPlatform("gemini"),
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusReview,
		FailReason:       "billing settlement requires manual review",
		Progress:         "100%",
		FinishTime:       now,
		NextPollAt:       now + 60,
	}
	task.PrivateData.ResultURL = "https://cdn.example/video.mp4"

	resp := TaskModel2Dto(task)

	require.Equal(t, string(model.TaskStatusInProgress), resp.Status)
	require.Equal(t, model.TaskSettlementStatusReview, resp.SettlementStatus)
	require.Equal(t, "99%", resp.Progress)
	require.Equal(t, int64(0), resp.FinishTime)
	require.Empty(t, resp.ResultURL)
	require.Empty(t, resp.FailReason)
}

func TestTaskModel2DtoHidesPendingVideoResultURL(t *testing.T) {
	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_video_pending_dto",
		Platform:         constant.TaskPlatform("gemini"),
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusPending,
		Progress:         "100%",
		FinishTime:       now,
	}
	task.PrivateData.ResultURL = "https://cdn.example/video.mp4"

	resp := TaskModel2Dto(task)

	require.Equal(t, string(model.TaskStatusInProgress), resp.Status)
	require.Empty(t, resp.ResultURL)
}

func TestTaskModel2DtoKeepsSettledVideoResultURL(t *testing.T) {
	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_video_settled_dto",
		Platform:         constant.TaskPlatform("gemini"),
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusSettled,
		Progress:         "100%",
		FinishTime:       now,
	}
	task.PrivateData.ResultURL = "https://cdn.example/video.mp4"

	resp := TaskModel2Dto(task)

	require.Equal(t, string(model.TaskStatusSuccess), resp.Status)
	require.Equal(t, "https://cdn.example/video.mp4", resp.ResultURL)
}

func TestTaskModel2PublicDtoHidesRetryableVideoSettlementReviewURL(t *testing.T) {
	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_video_public_review_dto",
		Platform:         constant.TaskPlatform("gemini"),
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusReview,
		FailReason:       "billing settlement requires manual review",
		Progress:         "100%",
		FinishTime:       now,
		NextPollAt:       now + 60,
	}
	task.PrivateData.ResultURL = "https://cdn.example/video.mp4"

	resp := TaskModel2PublicDto(task)

	require.Equal(t, string(model.TaskStatusInProgress), resp.Status)
	require.Empty(t, resp.SettlementStatus)
	require.Equal(t, "99%", resp.Progress)
	require.Equal(t, int64(0), resp.FinishTime)
	require.Empty(t, resp.ResultURL)
	require.Empty(t, resp.FailReason)
}

func TestTaskModel2DtoExposesUnrecoverableImageSettlementReviewReason(t *testing.T) {
	task := &model.Task{
		TaskID:           "task_unrecoverable_review_dto",
		Platform:         constant.TaskPlatformImage,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusReview,
		FailReason:       "image task result expired before settlement completed",
		ResultCleanedAt:  time.Now().Unix(),
	}
	task.PrivateData.ResultURL = "https://provider.example/image.png"

	resp := TaskModel2Dto(task)

	require.Equal(t, string(model.TaskStatusFailure), resp.Status)
	require.Equal(t, "image task result expired before settlement completed", resp.FailReason)
	require.Empty(t, resp.ResultURL)
}

func TestTaskModel2DtoHidesLegacyURLFailReasonOnUnrecoverableReview(t *testing.T) {
	task := &model.Task{
		TaskID:           "task_unrecoverable_review_url_reason",
		Platform:         constant.TaskPlatformImage,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusReview,
		FailReason:       "https://provider.example/image.png",
	}

	resp := TaskModel2Dto(task)

	require.Equal(t, string(model.TaskStatusFailure), resp.Status)
	require.Empty(t, resp.FailReason)
	require.Empty(t, resp.ResultURL)
}

func TestTaskModel2DtoIncludesSettlementReviewDetails(t *testing.T) {
	task := &model.Task{
		TaskID:           "task_review_details_dto",
		Platform:         constant.TaskPlatformSuno,
		Status:           model.TaskStatusNotStart,
		SettlementStatus: model.TaskSettlementStatusReview,
	}
	task.PrivateData.SettlementAttemptQuota = 321
	task.PrivateData.SettlementError = "record consume log failed"

	resp := TaskModel2Dto(task)

	require.Equal(t, model.TaskSettlementStatusReview, resp.SettlementStatus)
	require.Equal(t, 321, resp.SettlementAttemptQuota)
	require.Equal(t, "record consume log failed", resp.SettlementError)
}

func TestTaskModel2DtoClearsLegacySuccessResultURLFromFailReason(t *testing.T) {
	task := &model.Task{
		TaskID:     "task_legacy_result_dto",
		Status:     model.TaskStatusSuccess,
		FailReason: "https://provider.example/video.mp4",
	}

	resp := TaskModel2Dto(task)

	require.Empty(t, resp.FailReason)
	require.Equal(t, "https://provider.example/video.mp4", resp.ResultURL)
}

func TestTaskModel2PublicDtoOmitsInternalFields(t *testing.T) {
	task := &model.Task{
		ID:               88,
		TaskID:           "task_public_dto",
		Platform:         constant.TaskPlatformSuno,
		UserId:           12,
		Group:            "vip",
		ChannelId:        34,
		Quota:            56,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusSettled,
		Username:         "alice",
	}
	task.PrivateData.ResultURL = "https://provider.example/video.mp4"
	task.PrivateData.SettlementError = "record consume log failed"
	task.PrivateData.SettlementAttemptQuota = 321

	resp := TaskModel2PublicDto(task)

	require.Equal(t, "task_public_dto", resp.TaskID)
	require.Equal(t, "https://provider.example/video.mp4", resp.ResultURL)
	require.Zero(t, resp.UserId)
	require.Empty(t, resp.Group)
	require.Zero(t, resp.ChannelId)
	require.Zero(t, resp.Quota)
	require.Empty(t, resp.SettlementStatus)
	require.Empty(t, resp.SettlementError)
	require.Zero(t, resp.SettlementAttemptQuota)
	require.Empty(t, resp.Username)
}

func TestTaskModel2PublicDtoOmitsProviderDataForVideo(t *testing.T) {
	task := &model.Task{
		TaskID:   "task_public_video_data",
		Platform: constant.TaskPlatform("kling"),
		Status:   model.TaskStatusSuccess,
		Data:     json.RawMessage(`{"bytesBase64Encoded":"AAAA","videos":[{"url":"https://cdn.example/raw.mp4"}]}`),
		Properties: model.Properties{
			Input:             "a cat",
			OriginModelName:   "kling-v1",
			UpstreamModelName: "secret-upstream",
		},
	}
	task.PrivateData.ResultURL = "https://cdn.example/video.mp4"

	resp := TaskModel2PublicDto(task)

	require.Equal(t, "https://cdn.example/video.mp4", resp.ResultURL)
	require.Nil(t, resp.Data)
	props, ok := resp.Properties.(model.Properties)
	require.True(t, ok)
	require.Equal(t, "a cat", props.Input)
	require.Equal(t, "kling-v1", props.OriginModelName)
	require.Empty(t, props.UpstreamModelName)
}

func TestTaskModel2PublicDtoKeepsSunoData(t *testing.T) {
	task := &model.Task{
		TaskID:   "task_public_suno_data",
		Platform: constant.TaskPlatformSuno,
		Status:   model.TaskStatusSuccess,
		Data:     json.RawMessage(`{"id":"song_1","audio_url":"https://cdn.example/song.mp3"}`),
	}

	resp := TaskModel2PublicDto(task)

	require.JSONEq(t, `{"id":"song_1","audio_url":"https://cdn.example/song.mp3"}`, string(resp.Data))
}

func TestTaskModel2PublicDtoHidesSunoDataDuringRetryableSettlementReview(t *testing.T) {
	task := &model.Task{
		TaskID:           "task_public_suno_review_data",
		Platform:         constant.TaskPlatformSuno,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusReview,
		NextPollAt:       time.Now().Unix() + 60,
		Data:             json.RawMessage(`{"id":"song_1","audio_url":"https://cdn.example/song.mp3"}`),
	}

	resp := TaskModel2PublicDto(task)

	require.Equal(t, string(model.TaskStatusInProgress), resp.Status)
	require.Empty(t, resp.Data)
}

func TestTaskModel2DtoHidesSuccessfulDiagnosticReason(t *testing.T) {
	task := &model.Task{
		TaskID:     "task_success_reason_dto",
		Status:     model.TaskStatusSuccess,
		FailReason: "provider completed with diagnostics",
	}
	task.PrivateData.ResultURL = "https://provider.example/video.mp4"

	resp := TaskModel2Dto(task)

	require.Empty(t, resp.FailReason)
	require.Equal(t, "https://provider.example/video.mp4", resp.ResultURL)
}

func TestImageTaskShouldRetryStaleSyncExecutionBeforeTaskTimeout(t *testing.T) {
	withImageTaskAsyncTimeoutMinutes(t, 30)
	task := &model.Task{
		Status:    model.TaskStatusInProgress,
		StartTime: time.Now().Add(-imageTaskSyncTimeout - time.Second).Unix(),
	}

	require.False(t, imageTaskShouldFailStaleExecution(task, dto.ImageTaskModeSyncWrapper))
	require.True(t, imageTaskCanStart(task))
}

func TestImageTaskShouldFailStaleSyncExecutionAfterTaskTimeout(t *testing.T) {
	withImageTaskAsyncTimeoutMinutes(t, 1)
	task := &model.Task{
		Status:    model.TaskStatusInProgress,
		StartTime: time.Now().Add(-2 * time.Minute).Unix(),
	}

	require.True(t, imageTaskShouldFailStaleExecution(task, dto.ImageTaskModeSyncWrapper))
}

func TestImageTaskShouldNotFailSubmittedUpstreamAsyncPoll(t *testing.T) {
	task := &model.Task{
		Status:    model.TaskStatusInProgress,
		StartTime: time.Now().Add(-imageTaskSyncTimeout - time.Second).Unix(),
	}
	task.PrivateData.UpstreamTaskID = "upstream_123"

	require.False(t, imageTaskShouldFailStaleExecution(task, dto.ImageTaskModeAsyncTaskBridge))
}

func TestImageTaskShouldNotGenericFailStaleUpstreamAsyncSubmission(t *testing.T) {
	task := &model.Task{
		Status:    model.TaskStatusInProgress,
		StartTime: time.Now().Add(-imageTaskSyncTimeout - time.Second).Unix(),
	}

	require.False(t, imageTaskShouldFailStaleExecution(task, dto.ImageTaskModeAsyncTaskBridge))
	require.True(t, imageTaskExecutionTimedOut(task))
}

func TestImageTaskShouldFailUnrecoveredSubmittedAsyncSubmissionAfterTimeout(t *testing.T) {
	withImageTaskAsyncTimeoutMinutes(t, 30)
	task := &model.Task{
		Status:    model.TaskStatusSubmitted,
		StartTime: time.Now().Add(-imageTaskAsyncTimeout() - time.Second).Unix(),
	}

	require.False(t, imageTaskExecutionTimedOut(task))
	require.True(t, imageTaskShouldFailLongRunningUpstreamStatus(task))
}

func TestImageTaskShouldRecoverPendingAsyncSubmission(t *testing.T) {
	pending := &model.Task{
		Status:    model.TaskStatusInProgress,
		StartTime: time.Now().Unix(),
	}
	require.True(t, imageTaskShouldRecoverPendingAsyncSubmission(pending))

	submittedWithoutUpstreamID := &model.Task{
		Status:    model.TaskStatusSubmitted,
		StartTime: time.Now().Unix(),
	}
	require.True(t, imageTaskShouldRecoverPendingAsyncSubmission(submittedWithoutUpstreamID))

	queued := &model.Task{
		Status:    model.TaskStatusQueued,
		StartTime: time.Now().Unix(),
	}
	require.False(t, imageTaskShouldRecoverPendingAsyncSubmission(queued))

	withUpstreamID := &model.Task{
		Status:    model.TaskStatusInProgress,
		StartTime: time.Now().Unix(),
	}
	withUpstreamID.PrivateData.UpstreamTaskID = "upstream_123"
	require.False(t, imageTaskShouldRecoverPendingAsyncSubmission(withUpstreamID))
}

func TestImageTaskShouldFailLongRunningUpstreamStatusAfterTimeout(t *testing.T) {
	withImageTaskAsyncTimeoutMinutes(t, 30)
	for _, status := range []model.TaskStatus{
		model.TaskStatusQueued,
		model.TaskStatusSubmitted,
		model.TaskStatusInProgress,
	} {
		task := &model.Task{
			Status:    status,
			StartTime: time.Now().Add(-imageTaskAsyncTimeout() - time.Second).Unix(),
		}
		require.True(t, imageTaskShouldFailLongRunningUpstreamStatus(task))
	}

	recentTask := &model.Task{
		Status:    model.TaskStatusInProgress,
		StartTime: time.Now().Add(-imageTaskAsyncTimeout() + time.Second).Unix(),
	}
	require.False(t, imageTaskShouldFailLongRunningUpstreamStatus(recentTask))

	doneTask := &model.Task{
		Status:    model.TaskStatusSuccess,
		StartTime: time.Now().Add(-imageTaskAsyncTimeout() - time.Second).Unix(),
	}
	require.False(t, imageTaskShouldFailLongRunningUpstreamStatus(doneTask))
}

func TestImageTaskAsyncStatusDoesNotUseSyncWrapperTimeout(t *testing.T) {
	withImageTaskAsyncTimeoutMinutes(t, 30)
	task := &model.Task{
		Status:    model.TaskStatusSubmitted,
		StartTime: time.Now().Add(-imageTaskSyncTimeout - time.Second).Unix(),
	}

	require.False(t, imageTaskShouldFailLongRunningUpstreamStatus(task))
}

func TestImageTaskFixedUpstreamKeyPrefersStoredKey(t *testing.T) {
	task := &model.Task{}
	task.PrivateData.Key = " fixed-key "

	require.Equal(t, "fixed-key", imageTaskFixedUpstreamKey(task, "next-key"))
	require.Equal(t, "next-key", imageTaskFixedUpstreamKey(&model.Task{}, " next-key "))
	require.Empty(t, imageTaskFixedUpstreamKey(nil, " "))
}

func TestPriceDataFromTaskRestoresFullBillingSnapshot(t *testing.T) {
	task := &model.Task{
		Quota: 123,
		PrivateData: model.TaskPrivateData{
			BillingContext: &model.TaskBillingContext{
				ModelPrice:           0.01,
				GroupRatio:           1.2,
				GroupSpecialRatio:    1.1,
				GroupHasSpecialRatio: true,
				ModelRatio:           2,
				CompletionRatio:      3,
				CacheRatio:           0.5,
				CacheCreationRatio:   1.4,
				CacheCreation5mRatio: 1.5,
				CacheCreation1hRatio: 2.4,
				ImageRatio:           4,
				AudioRatio:           5,
				AudioCompletionRatio: 6,
				OtherRatios: map[string]float64{
					"n": 2,
				},
				PerCallBilling: true,
			},
		},
	}

	priceData := priceDataFromTask(task)

	require.Equal(t, 123, priceData.Quota)
	require.Equal(t, 123, priceData.QuotaToPreConsume)
	require.Equal(t, 0.01, priceData.ModelPrice)
	require.Equal(t, 1.2, priceData.GroupRatioInfo.GroupRatio)
	require.Equal(t, 1.1, priceData.GroupRatioInfo.GroupSpecialRatio)
	require.True(t, priceData.GroupRatioInfo.HasSpecialRatio)
	require.Equal(t, 2.0, priceData.ModelRatio)
	require.Equal(t, 3.0, priceData.CompletionRatio)
	require.Equal(t, 0.5, priceData.CacheRatio)
	require.Equal(t, 1.4, priceData.CacheCreationRatio)
	require.Equal(t, 1.5, priceData.CacheCreation5mRatio)
	require.Equal(t, 2.4, priceData.CacheCreation1hRatio)
	require.Equal(t, 4.0, priceData.ImageRatio)
	require.Equal(t, 5.0, priceData.AudioRatio)
	require.Equal(t, 6.0, priceData.AudioCompletionRatio)
	require.Equal(t, 2.0, priceData.OtherRatios()["n"])
	require.True(t, priceData.UsePrice)
}

func TestImageTaskUsageFromResultNormalizesOpenAIImageUsage(t *testing.T) {
	result := json.RawMessage(`{
		"created": 1710000000,
		"data": [{"url": "https://example.com/a.png"}],
		"usage": {
			"input_tokens": 3,
			"output_tokens": 4,
			"total_tokens": 7,
			"input_tokens_details": {
				"image_tokens": 2,
				"text_tokens": 1
			}
		}
	}`)

	usage, ok := imageTaskUsageFromResult(result)

	require.True(t, ok)
	require.Equal(t, 3, usage.PromptTokens)
	require.Equal(t, 4, usage.CompletionTokens)
	require.Equal(t, 7, usage.TotalTokens)
	require.Equal(t, 2, usage.PromptTokensDetails.ImageTokens)
	require.Equal(t, 1, usage.PromptTokensDetails.TextTokens)
}

func TestImageTaskUsageFromResultFindsNestedUsage(t *testing.T) {
	result := json.RawMessage(`{
		"openai_response": {
			"data": [{"b64_json": "abc"}],
			"usage": {
				"input_tokens": 8,
				"output_tokens": 9,
				"input_tokens_details": {
					"image_tokens": 5
				}
			}
		}
	}`)

	usage, ok := imageTaskUsageFromResult(result)

	require.True(t, ok)
	require.Equal(t, 8, usage.PromptTokens)
	require.Equal(t, 9, usage.CompletionTokens)
	require.Equal(t, 17, usage.TotalTokens)
	require.Equal(t, 5, usage.PromptTokensDetails.ImageTokens)
}

func TestImageTaskUsageFromResultWithoutUsageFallsBack(t *testing.T) {
	usage, ok := imageTaskUsageFromResult(json.RawMessage(`{"data":[{"url":"https://example.com/a.png"}]}`))

	require.False(t, ok)
	require.Nil(t, usage)
}

func TestImageTaskBillingRequestInputFromStoredBodyUsesDiskBodyWithSnapshotOnly(t *testing.T) {
	withTempImageTaskCache(t)
	path, err := common.WriteImageTaskBodyCacheFile([]byte(`{"model":"gpt-image-1","quality":"high","stream":false}`))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = common.RemoveDiskCacheFile(path)
	})

	task := &model.Task{}
	task.PrivateData.RequestBodyPath = path
	task.PrivateData.RequestContentType = "application/json"
	task.PrivateData.RequestHeaders = map[string]string{
		"X-Trace": " trace-123 ",
	}
	task.PrivateData.TieredBillingSnapshot = &billingexpr.BillingSnapshot{}

	input, err := imageTaskBillingRequestInputFromStoredBody(task)

	require.NoError(t, err)
	require.NotNil(t, input)
	require.JSONEq(t, `{"model":"gpt-image-1","quality":"high","stream":false}`, string(input.Body))
	require.Equal(t, "trace-123", input.Headers["X-Trace"])
}

func TestCloneImageTaskBillingRequestInputPreservesCapturedParams(t *testing.T) {
	source := &billingexpr.RequestInput{
		Headers: map[string]string{"X-Test": "trace"},
		Params:  map[string]any{"quality": "high"},
	}

	cloned := cloneImageTaskBillingRequestInput(source)

	require.NotNil(t, cloned)
	require.Equal(t, "high", cloned.Params["quality"])
	cloned.Params["quality"] = "low"
	require.Equal(t, "high", source.Params["quality"])
}

func TestCaptureImageTaskSettlementBillingEvidenceDropsRawBody(t *testing.T) {
	expr := `param("quality") == "high" ? tier("high", p * 2) : tier("normal", p)`
	task := &model.Task{PrivateData: model.TaskPrivateData{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr",
			ExprString:  expr,
		},
	}}
	input := &billingexpr.RequestInput{
		Headers: map[string]string{"X-Trace": "trace-123"},
		Body:    []byte(`{"quality":"high","prompt":"private prompt"}`),
	}

	evidence, err := captureImageTaskSettlementBillingEvidence(task, input)

	require.NoError(t, err)
	require.NotNil(t, evidence)
	require.Empty(t, evidence.Body)
	require.Empty(t, evidence.Headers)
	require.Equal(t, "high", evidence.Params["quality"])
	encoded, err := json.Marshal(evidence)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private prompt")
}

func TestCaptureImageTaskSettlementBillingEvidenceKeepsReferencedHeader(t *testing.T) {
	expr := `header("X-Billing-Tier") == "fast" ? tier("fast", p * 2) : tier("normal", p)`
	task := &model.Task{PrivateData: model.TaskPrivateData{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{BillingMode: "tiered_expr", ExprString: expr},
	}}
	input := &billingexpr.RequestInput{Headers: map[string]string{
		"X-Billing-Tier": "fast",
		"X-Trace-Secret": "must-not-persist",
	}}

	evidence, err := captureImageTaskSettlementBillingEvidence(task, input)

	require.NoError(t, err)
	require.Equal(t, map[string]string{"x-billing-tier": "fast"}, evidence.Headers)
}

func TestImageTaskBillingRequestInputFromStoredBodyRejectsOversizeBeforeRead(t *testing.T) {
	oldMaxMB := constant.ImageTaskRequestBodyBase64MaxMB
	oldDiskCacheConfig := common.GetDiskCacheConfig()
	common.ResetDiskCacheUsage()
	common.ResetDiskCacheStats()
	constant.ImageTaskRequestBodyBase64MaxMB = 1
	common.SetDiskCacheConfig(common.DiskCacheConfig{
		MaxSizeMB: 8,
		Path:      t.TempDir(),
	})
	t.Cleanup(func() {
		constant.ImageTaskRequestBodyBase64MaxMB = oldMaxMB
		common.ResetDiskCacheUsage()
		common.ResetDiskCacheStats()
		common.SetDiskCacheConfig(oldDiskCacheConfig)
	})

	path, err := common.WriteImageTaskBodyCacheFile(bytes.Repeat([]byte("a"), (1<<20)+1))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = common.RemoveDiskCacheFile(path)
	})

	task := &model.Task{}
	task.PrivateData.RequestBodyPath = path
	task.PrivateData.RequestBodySize = (1 << 20) + 1
	task.PrivateData.RequestContentType = "application/json"
	task.PrivateData.TieredBillingSnapshot = &billingexpr.BillingSnapshot{}

	input, err := imageTaskBillingRequestInputFromStoredBody(task)

	require.ErrorIs(t, err, common.ErrRequestBodyTooLarge)
	require.NotNil(t, input)
	require.Empty(t, input.Body)
}

func TestOpenImageTaskBodyStorageFallsBackToStoredBase64(t *testing.T) {
	body := []byte(`{"model":"gpt-image-1","stream":false}`)
	task := &model.Task{
		PrivateData: model.TaskPrivateData{
			RequestContentType: "application/json",
			RequestBodyPath:    "missing-request-body.json",
			RequestBodyBase64:  base64.StdEncoding.EncodeToString(body),
		},
	}

	storage, contentType, err := openImageTaskBodyStorage(task)
	require.NoError(t, err)
	defer storage.Close()

	got, err := storage.Bytes()
	require.NoError(t, err)
	require.Equal(t, "application/json", contentType)
	require.JSONEq(t, string(body), string(got))
}

func TestOpenImageTaskBodyStorageFallsBackWhenDiskBodySizeMismatches(t *testing.T) {
	withTempImageTaskCache(t)
	body := []byte(`{"model":"gpt-image-1","stream":false}`)
	path, err := common.WriteImageTaskBodyCacheFile([]byte(`{"bad":true}`))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = common.RemoveDiskCacheFile(path)
	})
	task := &model.Task{
		TaskID: "task_body_size_mismatch",
		PrivateData: model.TaskPrivateData{
			RequestContentType: "application/json",
			RequestBodyPath:    path,
			RequestBodyBase64:  base64.StdEncoding.EncodeToString(body),
			RequestBodySize:    int64(len(body)),
		},
	}

	storage, contentType, err := openImageTaskBodyStorage(task)
	require.NoError(t, err)
	defer storage.Close()

	got, err := storage.Bytes()
	require.NoError(t, err)
	require.Equal(t, "application/json", contentType)
	require.JSONEq(t, string(body), string(got))
}

func TestOpenImageTaskBodyStoragePrefersBase64ForPortableBody(t *testing.T) {
	withTempImageTaskCache(t)
	body := []byte(`{"b":2}`)
	path, err := common.WriteImageTaskBodyCacheFile([]byte(`{"a":1}`))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = common.RemoveDiskCacheFile(path)
	})
	task := &model.Task{
		TaskID: "task_body_portable",
		PrivateData: model.TaskPrivateData{
			RequestContentType:  "application/json",
			RequestBodyPath:     path,
			RequestBodyBase64:   base64.StdEncoding.EncodeToString(body),
			RequestBodyPortable: true,
			RequestBodySize:     int64(len(body)),
		},
	}

	storage, contentType, err := openImageTaskBodyStorage(task)
	require.NoError(t, err)
	defer storage.Close()

	got, err := storage.Bytes()
	require.NoError(t, err)
	require.Equal(t, "application/json", contentType)
	require.JSONEq(t, string(body), string(got))
}

func TestStoreImageTaskResultDataKeepsB64InlineWhenFileCacheNotShared(t *testing.T) {
	oldShared := constant.ImageTaskFileCacheShared
	oldTrusted := constant.ImageTaskFileCacheSharedTrusted
	oldDiskCacheConfig := common.GetDiskCacheConfig()
	constant.ImageTaskFileCacheShared = false
	constant.ImageTaskFileCacheSharedTrusted = false
	diskCacheConfig := oldDiskCacheConfig
	diskCacheConfig.Path = t.TempDir()
	common.SetDiskCacheConfig(diskCacheConfig)
	t.Cleanup(func() {
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskFileCacheSharedTrusted = oldTrusted
		common.SetDiskCacheConfig(oldDiskCacheConfig)
	})

	result := json.RawMessage(`{"data":[{"b64_json":"inline-b64"}]}`)
	task := &model.Task{}

	path, err := storeImageTaskResultData(task, result, time.Now().Unix())

	require.NoError(t, err)
	require.Empty(t, path)
	require.Empty(t, task.PrivateData.ResultBodyPath)
	require.JSONEq(t, string(result), string(task.Data))
}

func TestStoreImageTaskResultDataStartsExpiryAtResultPersistence(t *testing.T) {
	oldRetention := constant.ImageTaskResultRetentionMinutes
	constant.ImageTaskResultRetentionMinutes = 720
	t.Cleanup(func() { constant.ImageTaskResultRetentionMinutes = oldRetention })

	storedAt := time.Now().Unix()
	task := &model.Task{
		ResultAcknowledgedAt: storedAt - 10,
		ResultDeleteAfter:    storedAt - 5,
		ResultCleanedAt:      storedAt - 1,
	}
	result := json.RawMessage(`{"data":[{"b64_json":"inline-b64"}]}`)

	path, err := storeImageTaskResultData(task, result, storedAt)

	require.NoError(t, err)
	require.Empty(t, path)
	require.Equal(t, storedAt+12*60*60, task.ResultExpiresAt)
	require.Equal(t, storedAt, task.PrivateData.ResultStoredAt)
	require.Equal(t, storedAt+12*60*60, task.PrivateData.ResultExpiresAt)
	require.Zero(t, task.ResultAcknowledgedAt)
	require.Zero(t, task.ResultDeleteAfter)
	require.Zero(t, task.ResultCleanedAt)
	require.JSONEq(t, string(result), string(task.Data))
}

func TestMarkImageTaskSettlementSettledDoesNotExtendResultRetention(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	oldDB := model.DB
	oldRetention := constant.ImageTaskResultRetentionMinutes
	model.DB = db
	constant.ImageTaskResultRetentionMinutes = 720
	t.Cleanup(func() {
		model.DB = oldDB
		constant.ImageTaskResultRetentionMinutes = oldRetention
		_ = sqlDB.Close()
	})

	storedAt := time.Now().Add(-13 * time.Hour).Unix()
	task := &model.Task{
		TaskID:           "task_settled_keeps_result_retention",
		Platform:         constant.TaskPlatformImage,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusApplied,
		FinishTime:       storedAt,
		ResultExpiresAt:  storedAt + 12*60*60,
		PrivateData: model.TaskPrivateData{
			ResultStoredAt:  storedAt,
			ResultExpiresAt: storedAt + 12*60*60,
		},
	}
	task.SetData(map[string]any{"data": []any{map[string]any{"url": "https://example.com/settled.png"}}})
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, markImageTaskSettlementSettled(context.Background(), task, model.TaskSettlementStatusApplied))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskSettlementStatusSettled, reloaded.SettlementStatus)
	require.Equal(t, storedAt+12*60*60, reloaded.ResultExpiresAt)
	require.Equal(t, reloaded.ResultExpiresAt, reloaded.PrivateData.ResultExpiresAt)
}

func TestStoreImageTaskResultDataKeepsB64InlineWhenSharedCacheIsNotTrusted(t *testing.T) {
	oldShared := constant.ImageTaskFileCacheShared
	oldTrusted := constant.ImageTaskFileCacheSharedTrusted
	oldDiskCacheConfig := common.GetDiskCacheConfig()
	constant.ImageTaskFileCacheShared = true
	constant.ImageTaskFileCacheSharedTrusted = false
	diskCacheConfig := oldDiskCacheConfig
	diskCacheConfig.Path = t.TempDir()
	common.SetDiskCacheConfig(diskCacheConfig)
	t.Cleanup(func() {
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskFileCacheSharedTrusted = oldTrusted
		common.SetDiskCacheConfig(oldDiskCacheConfig)
	})

	result := json.RawMessage(`{"data":[{"b64_json":"inline-b64"}]}`)
	task := &model.Task{}

	path, err := storeImageTaskResultData(task, result, time.Now().Unix())

	require.NoError(t, err)
	require.Empty(t, path)
	require.Empty(t, task.PrivateData.ResultBodyPath)
	require.JSONEq(t, string(result), string(task.Data))
}

func TestStoreImageTaskResultDataKeepsLargeB64InlineWithoutTrustedCache(t *testing.T) {
	oldShared := constant.ImageTaskFileCacheShared
	oldTrusted := constant.ImageTaskFileCacheSharedTrusted
	oldDiskCacheConfig := common.GetDiskCacheConfig()
	constant.ImageTaskFileCacheShared = true
	constant.ImageTaskFileCacheSharedTrusted = false
	diskCacheConfig := oldDiskCacheConfig
	diskCacheConfig.Path = t.TempDir()
	common.SetDiskCacheConfig(diskCacheConfig)
	t.Cleanup(func() {
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskFileCacheSharedTrusted = oldTrusted
		common.SetDiskCacheConfig(oldDiskCacheConfig)
	})

	result := json.RawMessage(`{"data":[{"b64_json":"` + strings.Repeat("x", 2<<20) + `"}]}`)
	task := &model.Task{}

	path, err := storeImageTaskResultData(task, result, time.Now().Unix())

	require.NoError(t, err)
	require.Empty(t, path)
	require.JSONEq(t, string(result), string(task.Data))
	require.Empty(t, task.PrivateData.ResultBodyPath)
}

func TestImageTaskResultStorageActionIgnoresB64JSONTextValue(t *testing.T) {
	result := []byte(`{"data":[{"revised_prompt":"b64_json"}]}`)

	offload, err := imageTaskResultStorageAction(result)

	require.NoError(t, err)
	require.False(t, offload)
}

func TestStoreImageTaskResultDataOffloadsB64WhenSharedCacheIsTrusted(t *testing.T) {
	oldShared := constant.ImageTaskFileCacheShared
	oldTrusted := constant.ImageTaskFileCacheSharedTrusted
	oldSharedDisabled := common.ImageTaskSharedCacheDisabled()
	oldDiskCacheConfig := common.GetDiskCacheConfig()
	constant.ImageTaskFileCacheShared = true
	constant.ImageTaskFileCacheSharedTrusted = true
	common.SetImageTaskSharedCacheDisabled(false)
	diskCacheConfig := oldDiskCacheConfig
	diskCacheConfig.Path = t.TempDir()
	common.SetDiskCacheConfig(diskCacheConfig)
	t.Cleanup(func() {
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskFileCacheSharedTrusted = oldTrusted
		common.SetImageTaskSharedCacheDisabled(oldSharedDisabled)
		common.SetDiskCacheConfig(oldDiskCacheConfig)
	})

	result := json.RawMessage(`{"data":[{"b64_json":"inline-b64"}]}`)
	task := &model.Task{}

	path, err := storeImageTaskResultData(task, result, time.Now().Unix())

	require.NoError(t, err)
	require.NotEmpty(t, path)
	require.Equal(t, path, task.PrivateData.ResultBodyPath)
	require.FileExists(t, path)
	require.True(t, imageTaskDataIsStoredResultPlaceholder(task.Data))
	_ = os.Remove(path)
}

func TestImageTaskResultStorageActionRejectsOversizeInlineResult(t *testing.T) {
	oldShared := constant.ImageTaskFileCacheShared
	oldTrusted := constant.ImageTaskFileCacheSharedTrusted
	oldInlineMax := constant.ImageTaskResultInlineMaxMB
	constant.ImageTaskFileCacheShared = false
	constant.ImageTaskFileCacheSharedTrusted = false
	constant.ImageTaskResultInlineMaxMB = 1
	t.Cleanup(func() {
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskFileCacheSharedTrusted = oldTrusted
		constant.ImageTaskResultInlineMaxMB = oldInlineMax
	})

	small := []byte(`{"data":[{"b64_json":"aGVsbG8="}]}`)
	offload, err := imageTaskResultStorageAction(small)
	require.NoError(t, err)
	require.False(t, offload)

	oversize := append([]byte(`{"data":[{"b64_json":"`), bytes.Repeat([]byte("a"), (1<<20)+1)...)
	oversize = append(oversize, []byte(`"}]}`)...)
	offload, err = imageTaskResultStorageAction(oversize)
	require.False(t, offload)
	require.ErrorContains(t, err, "too large to store inline")

	// Non-b64 payloads must use the same inline size guard.
	oversizeURL := append([]byte(`{"data":[{"url":"https://example.com/`), bytes.Repeat([]byte("a"), (1<<20)+1)...)
	oversizeURL = append(oversizeURL, []byte(`"}]}`)...)
	offload, err = imageTaskResultStorageAction(oversizeURL)
	require.False(t, offload)
	require.ErrorContains(t, err, "too large to store inline")

	// 关闭上限后恢复内联行为。
	constant.ImageTaskResultInlineMaxMB = 0
	offload, err = imageTaskResultStorageAction(oversize)
	require.NoError(t, err)
	require.False(t, offload)
}

func TestStoreImageTaskResultDataMarksReviewInsteadOfInliningOversizeResult(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	oldDB := model.DB
	oldShared := constant.ImageTaskFileCacheShared
	oldTrusted := constant.ImageTaskFileCacheSharedTrusted
	oldInlineMax := constant.ImageTaskResultInlineMaxMB
	model.DB = db
	constant.ImageTaskFileCacheShared = false
	constant.ImageTaskFileCacheSharedTrusted = false
	constant.ImageTaskResultInlineMaxMB = 1
	t.Cleanup(func() {
		model.DB = oldDB
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskFileCacheSharedTrusted = oldTrusted
		constant.ImageTaskResultInlineMaxMB = oldInlineMax
		_ = sqlDB.Close()
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:     "task_result_inline_guard",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusInProgress,
		Progress:   "1%",
		SubmitTime: now,
		StartTime:  now,
	}
	require.NoError(t, db.Create(task).Error)

	oversize := append([]byte(`{"data":[{"b64_json":"`), bytes.Repeat([]byte("a"), (1<<20)+1)...)
	oversize = append(oversize, []byte(`"}]}`)...)

	path, storeErr := storeImageTaskResultData(task, oversize, now)
	require.Empty(t, path)
	require.ErrorContains(t, storeErr, "too large to store inline")

	require.NoError(t, markImageTaskUpstreamResultReview(
		context.Background(), task, model.TaskStatusInProgress,
		"store image task result failed: "+storeErr.Error(),
	))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.Contains(t, reloaded.FailReason, "too large to store inline")
	require.Empty(t, reloaded.PrivateData.ResultBodyPath)
}

func TestRunImageTasksRetiresRemovedBridgeTasksWithoutUpstreamCall(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.Task{},
		&model.TaskSettlementRecord{},
		&model.User{},
		&model.Token{},
		&model.Channel{},
		&model.Log{},
		&model.QuotaData{},
		&model.TokenUsageDaily{},
	))

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldUsingSQLite := common.UsingSQLite
	oldRedisEnabled := common.RedisEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	model.DB = db
	model.LOG_DB = db
	common.UsingSQLite = true
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	common.BatchUpdateEnabled = false
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.UsingSQLite = oldUsingSQLite
		common.RedisEnabled = oldRedisEnabled
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
		_ = sqlDB.Close()
	})

	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "removed image task bridge", http.StatusBadGateway)
	}))
	defer upstream.Close()

	const userQuota int64 = 10000
	const preConsumed = 250
	require.NoError(t, db.Create(&model.User{
		Id:       1,
		Username: "bridge-retire-user",
		Password: "password123",
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    userQuota,
	}).Error)
	require.NoError(t, db.Create(&model.Token{
		Id:          1,
		UserId:      1,
		Key:         "sk-bridge-retire",
		Name:        "bridge-retire",
		Status:      common.TokenStatusEnabled,
		RemainQuota: userQuota,
	}).Error)
	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      1,
		Type:    constant.ChannelTypeOpenAI,
		Key:     "upstream-key",
		Status:  common.ChannelStatusEnabled,
		Name:    "removed-bridge",
		Group:   "default",
		Models:  "gpt-image-1",
		BaseURL: &baseURL,
	}).Error)

	now := time.Now().Unix()
	queued := &model.Task{
		TaskID:     "task_removed_bridge_queued",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Quota:      preConsumed,
		Action:     constant.TaskActionImageGeneration,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		Properties: model.Properties{OriginModelName: "gpt-image-1"},
		PrivateData: model.TaskPrivateData{
			ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
			TokenId:       1,
			BillingSource: service.BillingSourceWallet,
		},
	}
	submitted := &model.Task{
		TaskID:     "task_removed_bridge_submitted",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Quota:      preConsumed,
		Action:     constant.TaskActionImageGeneration,
		Status:     model.TaskStatusSubmitted,
		Progress:   "10%",
		SubmitTime: now,
		Properties: model.Properties{OriginModelName: "gpt-image-1"},
		PrivateData: model.TaskPrivateData{
			ImageTaskMode: "gpt_image2api_async",
			TokenId:       1,
			BillingSource: service.BillingSourceWallet,
		},
	}
	inProgress := &model.Task{
		TaskID:     "task_removed_bridge_in_progress",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Quota:      preConsumed,
		Action:     constant.TaskActionImageGeneration,
		Status:     model.TaskStatusInProgress,
		Progress:   "40%",
		SubmitTime: now,
		Properties: model.Properties{OriginModelName: "gpt-image-1"},
		PrivateData: model.TaskPrivateData{
			ImageTaskMode:  dto.ImageTaskModeAsyncTaskBridge,
			UpstreamTaskID: "upstream_still_running",
			TokenId:        1,
			BillingSource:  service.BillingSourceWallet,
		},
	}
	applied := &model.Task{
		TaskID:           "task_removed_bridge_applied",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Quota:            preConsumed,
		Action:           constant.TaskActionImageGeneration,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusApplied,
		Properties:       model.Properties{OriginModelName: "gpt-image-1"},
		PrivateData: model.TaskPrivateData{
			ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
			TokenId:       1,
		},
	}
	missingResult := &model.Task{
		TaskID:           "task_removed_bridge_missing_result",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Quota:            preConsumed,
		Action:           constant.TaskActionImageGeneration,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusPending,
		Properties:       model.Properties{OriginModelName: "gpt-image-1"},
		PrivateData: model.TaskPrivateData{
			ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
			TokenId:       1,
			BillingSource: service.BillingSourceWallet,
		},
	}
	require.NoError(t, db.Create(queued).Error)
	require.NoError(t, db.Create(submitted).Error)
	require.NoError(t, db.Create(inProgress).Error)
	require.NoError(t, db.Create(applied).Error)
	require.NoError(t, db.Create(missingResult).Error)

	require.NoError(t, RunImageTasks(context.Background(), []*model.Task{queued, submitted, inProgress, applied, missingResult}))
	require.Zero(t, calls.Load())

	var failed, review, running, settled, missing model.Task
	require.NoError(t, db.First(&failed, queued.ID).Error)
	require.NoError(t, db.First(&review, submitted.ID).Error)
	require.NoError(t, db.First(&running, inProgress.ID).Error)
	require.NoError(t, db.First(&settled, applied.ID).Error)
	require.NoError(t, db.First(&missing, missingResult.ID).Error)

	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), failed.Status)
	require.Equal(t, service.RemovedImageTaskBridgeFailReason, failed.FailReason)
	require.NotEqual(t, model.TaskSettlementStatusReview, failed.SettlementStatus)
	require.False(t, failed.RefundPending)
	require.Zero(t, failed.Quota)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), review.Status)
	require.Equal(t, model.TaskSettlementStatusReview, review.SettlementStatus)
	require.Equal(t, service.RemovedImageTaskBridgeFailReason, review.FailReason)
	require.False(t, review.RefundPending)
	require.EqualValues(t, preConsumed, review.Quota)
	require.NotZero(t, review.PrivateData.UpstreamSubmitUncertainAt)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), running.Status)
	require.Equal(t, model.TaskSettlementStatusReview, running.SettlementStatus)
	require.False(t, running.RefundPending)
	require.EqualValues(t, preConsumed, running.Quota)
	require.Equal(t, "upstream_still_running", running.PrivateData.UpstreamTaskID)
	require.Equal(t, model.TaskSettlementStatusSettled, settled.SettlementStatus)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), missing.Status)
	require.Equal(t, service.RemovedImageTaskBridgeFailReason, missing.FailReason)
	require.NotEqual(t, model.TaskSettlementStatusReview, missing.SettlementStatus)
	require.False(t, missing.RefundPending)
	require.Zero(t, missing.Quota)

	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	require.EqualValues(t, userQuota+int64(preConsumed)*2, user.Quota)
}

func TestRunImageTasksFinalizesRemovedBridgeAppliedConsumptionWithoutResult(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskSettlementRecord{}, &model.User{}, &model.Token{}, &model.Channel{}))

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldUsingSQLite := common.UsingSQLite
	oldNode := common.NodeName
	model.DB = db
	model.LOG_DB = db
	common.UsingSQLite = true
	common.NodeName = "node-a"
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.UsingSQLite = oldUsingSQLite
		common.NodeName = oldNode
		_ = sqlDB.Close()
	})

	require.NoError(t, db.Create(&model.User{
		Id: 1, Username: "bridge-applied-user", Password: "password123",
		Status: common.UserStatusEnabled, Group: "default", Quota: 10000,
	}).Error)
	require.NoError(t, db.Create(&model.Token{
		Id: 1, UserId: 1, Key: "sk-bridge-applied", Name: "bridge-applied",
		Status: common.TokenStatusEnabled, RemainQuota: 10000,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Type: constant.ChannelTypeOpenAI, Key: "upstream-key",
		Status: common.ChannelStatusEnabled, Name: "bridge-applied", Group: "default", Models: "gpt-image-1",
	}).Error)

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_removed_bridge_applied_record",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Quota:            250,
		Action:           constant.TaskActionImageGeneration,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusReview,
		Properties:       model.Properties{OriginModelName: "gpt-image-1"},
		PrivateData: model.TaskPrivateData{
			ImageTaskMode:  dto.ImageTaskModeAsyncTaskBridge,
			TokenId:        1,
			BillingSource:  service.BillingSourceWallet,
			NodeName:       common.NodeName,
			ResultBodyPath: filepath.Join(t.TempDir(), "applied-missing.json"),
		},
		ImageTaskResultStored: true,
		Data:                  json.RawMessage(`{"_newapi_result_file":true}`),
	}
	require.NoError(t, db.Create(task).Error)
	appliedQuota := 80
	require.NoError(t, db.Create(&model.TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        model.TaskSettlementRecordStatusApplied,
		Operation:     "image_consumption",
		AppliedQuota:  &appliedQuota,
		AppliedAt:     now,
	}).Error)

	require.NoError(t, RunImageTasks(context.Background(), []*model.Task{task}))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	require.Equal(t, model.TaskSettlementStatusSettled, reloaded.SettlementStatus)
	require.Equal(t, appliedQuota, reloaded.Quota)
	require.False(t, reloaded.RefundPending)
	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	require.EqualValues(t, 10000, user.Quota)
}

func TestRunSyncWrapperImageTaskDoesNotPreConsumeFixedPriceAgain(t *testing.T) {
	withTempImageTaskCache(t)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.Task{},
		&model.TaskSettlementRecord{},
		&model.User{},
		&model.Token{},
		&model.Channel{},
		&model.Log{},
		&model.QuotaData{},
		&model.TokenUsageDaily{},
	))

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldUsingSQLite := common.UsingSQLite
	oldRedisEnabled := common.RedisEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	model.DB = db
	model.LOG_DB = db
	common.UsingSQLite = true
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	common.BatchUpdateEnabled = false
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.UsingSQLite = oldUsingSQLite
		common.RedisEnabled = oldRedisEnabled
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
		_ = sqlDB.Close()
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1710000000,"data":[{"b64_json":"already-reserved"}]}`))
	}))
	defer upstream.Close()

	const remainingQuota int64 = 100000
	const alreadyReserved = 5000
	require.NoError(t, db.Create(&model.User{
		Id: 1, Username: "priced-image-user", Password: "password123",
		Status: common.UserStatusEnabled, Group: "default", Quota: remainingQuota,
		Setting: `{"billing_preference":"wallet_only"}`,
	}).Error)
	require.NoError(t, db.Create(&model.Token{
		Id: 1, UserId: 1, Key: "sk-priced-image", Name: "priced-image",
		Status: common.TokenStatusEnabled, RemainQuota: remainingQuota,
	}).Error)
	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Type: constant.ChannelTypeOpenAI, Key: "upstream-key",
		Status: common.ChannelStatusEnabled, Name: "priced-image", Group: "default",
		Models: "gpt-image-1", BaseURL: &baseURL,
	}).Error)

	body := []byte(`{"model":"gpt-image-1","prompt":"cat","n":1}`)
	bodyPath, err := common.WriteImageTaskBodyCacheFile(body)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(bodyPath) })

	task := &model.Task{
		TaskID:     "task_priced_already_reserved",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Quota:      alreadyReserved,
		Action:     constant.TaskActionImageGeneration,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: time.Now().Unix(),
		Properties: model.Properties{OriginModelName: "gpt-image-1"},
		PrivateData: model.TaskPrivateData{
			PublicImageTask:    true,
			BillingSource:      service.BillingSourceWallet,
			TokenId:            1,
			ImageTaskMode:      dto.ImageTaskModeSyncWrapper,
			RequestPath:        "/v1/images/generations",
			RequestMethod:      http.MethodPost,
			RequestContentType: "application/json",
			RequestBodyPath:    bodyPath,
			RequestBodySize:    int64(len(body)),
			Key:                "upstream-key",
			BillingContext: &model.TaskBillingContext{
				ModelPrice:      0.04,
				GroupRatio:      1,
				OriginModelName: "gpt-image-1",
				PerCallBilling:  true,
			},
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, runSyncWrapperImageTask(context.Background(), task))

	var updated model.Task
	require.NoError(t, db.First(&updated, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), updated.Status)
	require.Equal(t, model.TaskSettlementStatusSettled, updated.SettlementStatus)
	require.NotZero(t, updated.Quota)

	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	require.EqualValues(t, remainingQuota+int64(alreadyReserved), int64(updated.Quota)+user.Quota,
		"worker execution must not open a second pre-consume; settlement may only adjust the creation reserve")
	var token model.Token
	require.NoError(t, db.First(&token, 1).Error)
	require.EqualValues(t, remainingQuota+int64(alreadyReserved), int64(updated.Quota)+token.RemainQuota)
}

func TestRunSyncWrapperImageTaskReleasesMultipartTempFiles(t *testing.T) {
	// multipart 临时文件重定向到本用例独占目录，避免跨包并行统计串扰。
	isolatedTempDir := t.TempDir()
	t.Setenv("TMPDIR", isolatedTempDir)
	t.Setenv("TMP", isolatedTempDir)
	t.Setenv("TEMP", isolatedTempDir)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.Task{},
		&model.TaskSettlementRecord{},
		&model.User{},
		&model.Token{},
		&model.Channel{},
		&model.Log{},
		&model.QuotaData{},
		&model.TokenUsageDaily{},
	))

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldUsingSQLite := common.UsingSQLite
	oldRedisEnabled := common.RedisEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldDiskCacheConfig := common.GetDiskCacheConfig()
	oldShared := constant.ImageTaskFileCacheShared
	oldTrusted := constant.ImageTaskFileCacheSharedTrusted
	oldSharedDisabled := common.ImageTaskSharedCacheDisabled()
	oldMaxFileDownloadMB := constant.MaxFileDownloadMB
	goodCache := oldDiskCacheConfig
	goodCache.Path = t.TempDir()
	model.DB = db
	model.LOG_DB = db
	common.UsingSQLite = true
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	constant.ImageTaskFileCacheShared = true
	constant.ImageTaskFileCacheSharedTrusted = true
	// 压低 multipart 内存阈值，强制文件分片落盘产生临时文件。
	constant.MaxFileDownloadMB = 1
	common.SetImageTaskSharedCacheDisabled(false)
	common.SetDiskCacheConfig(goodCache)
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.UsingSQLite = oldUsingSQLite
		common.RedisEnabled = oldRedisEnabled
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskFileCacheSharedTrusted = oldTrusted
		constant.MaxFileDownloadMB = oldMaxFileDownloadMB
		common.SetImageTaskSharedCacheDisabled(oldSharedDisabled)
		common.SetDiskCacheConfig(oldDiskCacheConfig)
		_ = sqlDB.Close()
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1710000000,"data":[{"b64_json":"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="}]}`))
	}))
	defer upstream.Close()

	require.NoError(t, db.Create(&model.User{
		Id: 1, Username: "image-user", Password: "password123",
		Status: common.UserStatusEnabled, Group: "default", Quota: 100000,
	}).Error)
	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Type: constant.ChannelTypeOpenAI, Key: "upstream-key",
		Status: common.ChannelStatusEnabled, Name: "openai-image", Group: "default",
		Models: "gpt-image-1", BaseURL: &baseURL,
	}).Error)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-1"))
	require.NoError(t, writer.WriteField("prompt", "edit this image"))
	part, err := writer.CreateFormFile("image", "input.png")
	require.NoError(t, err)
	_, err = part.Write(largeValidImageTaskTestPNG(t))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	bodyPath, err := common.WriteImageTaskBodyCacheFile(body.Bytes())
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(bodyPath) })

	task := &model.Task{
		TaskID:     "task_sync_multipart_tempfiles",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Action:     constant.TaskActionImageEdit,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: time.Now().Unix(),
		Properties: model.Properties{OriginModelName: "gpt-image-1"},
		PrivateData: model.TaskPrivateData{
			ImageTaskMode:      dto.ImageTaskModeSyncWrapper,
			RequestPath:        "/v1/images/edits",
			RequestMethod:      http.MethodPost,
			RequestContentType: writer.FormDataContentType(),
			RequestBodyPath:    bodyPath,
			RequestBodySize:    int64(body.Len()),
			Key:                "upstream-key",
		},
	}
	require.NoError(t, db.Create(task).Error)

	before, err := filepath.Glob(filepath.Join(os.TempDir(), "multipart-*"))
	require.NoError(t, err)

	require.NoError(t, runSyncWrapperImageTask(context.Background(), task))

	after, err := filepath.Glob(filepath.Join(os.TempDir(), "multipart-*"))
	require.NoError(t, err)
	require.Len(t, after, len(before), "sync wrapper execution must not leak multipart temp files")

	var updated model.Task
	require.NoError(t, db.First(&updated, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), updated.Status)
}

func TestRunSyncWrapperImageTaskDoesNotReplayMarkedSubmissionAfterLeaseRecovery(t *testing.T) {
	withTempImageTaskCache(t)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.Task{},
		&model.TaskSettlementRecord{},
		&model.User{},
		&model.Channel{},
	))

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldRedisEnabled := common.RedisEnabled
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.RedisEnabled = oldRedisEnabled
		_ = sqlDB.Close()
	})

	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1710000000,"data":[{"b64_json":"duplicate"}]}`))
	}))
	defer upstream.Close()

	require.NoError(t, db.Create(&model.User{
		Id: 1, Username: "image-user", Password: "password123",
		Status: common.UserStatusEnabled, Group: "default", Quota: 100000,
	}).Error)
	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Type: constant.ChannelTypeOpenAI, Key: "upstream-key",
		Status: common.ChannelStatusEnabled, Name: "openai-image", Group: "default",
		Models: "gpt-image-1", BaseURL: &baseURL,
	}).Error)
	body := []byte(`{"model":"gpt-image-1","prompt":"cat","stream":false}`)
	bodyPath, err := common.WriteImageTaskBodyCacheFile(body)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(bodyPath) })

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:                  "task_sync_submission_ambiguous",
		Platform:                constant.TaskPlatformImage,
		UserId:                  1,
		Group:                   "default",
		ChannelId:               1,
		Action:                  constant.TaskActionImageGeneration,
		Status:                  model.TaskStatusInProgress,
		Progress:                "1%",
		SubmitTime:              now - 30,
		StartTime:               now - 30,
		SyncSubmissionStartedAt: now - 30,
		Quota:                   600,
		Properties:              model.Properties{OriginModelName: "gpt-image-1"},
		PrivateData: model.TaskPrivateData{
			PublicImageTask:    true,
			BillingSource:      "wallet",
			ImageTaskMode:      dto.ImageTaskModeSyncWrapper,
			RequestPath:        "/v1/images/generations",
			RequestMethod:      http.MethodPost,
			RequestContentType: "application/json",
			RequestBodyPath:    bodyPath,
			RequestBodySize:    int64(len(body)),
			Key:                "upstream-key",
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, runSyncWrapperImageTask(context.Background(), task))
	require.Zero(t, upstreamCalls.Load())
	var updated model.Task
	require.NoError(t, db.First(&updated, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), updated.Status)
	require.Equal(t, model.TaskSettlementStatusReview, updated.SettlementStatus)
	require.Equal(t, 600, updated.Quota)
	require.Contains(t, updated.FailReason, "submission outcome is unknown")
}

func TestRunSyncWrapperImageTaskRefundsPrechargeWhenUpstreamWasNotSent(t *testing.T) {
	withTempImageTaskCache(t)
	db := openImageTaskRunnerDB(t)

	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	const startingQuota int64 = 99400
	const reservedQuota = 600
	mapping := `{"gpt-image-1":"alias","alias":"gpt-image-1"}`
	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.User{
		Id: 41, Username: "local-fail-image-user", Password: "password123",
		Status: common.UserStatusEnabled, Group: "default", Quota: startingQuota,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id: 41, Type: constant.ChannelTypeOpenAI, Key: "upstream-key",
		Status: common.ChannelStatusEnabled, Name: "local-fail", Group: "default",
		Models: "gpt-image-1", BaseURL: &baseURL, ModelMapping: &mapping,
	}).Error)
	task := queuedSyncImageTask(t, db, "task_sync_local_fail", 41, reservedQuota)

	require.NoError(t, runSyncWrapperImageTask(context.Background(), task))

	require.Zero(t, upstreamCalls.Load())
	var updated model.Task
	require.NoError(t, db.First(&updated, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), updated.Status)
	require.NotEqual(t, model.TaskSettlementStatusReview, updated.SettlementStatus)
	require.Zero(t, updated.Quota)
	require.Zero(t, updated.SyncSubmissionStartedAt)
	var user model.User
	require.NoError(t, db.First(&user, 41).Error)
	require.EqualValues(t, startingQuota+int64(reservedQuota), user.Quota)
}

func TestRunSyncWrapperImageTaskKeepsPrechargeWhenUpstreamRequestWasSent(t *testing.T) {
	withTempImageTaskCache(t)
	db := openImageTaskRunnerDB(t)

	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(upstream.Close)

	const startingQuota int64 = 100000
	const reservedQuota = 600
	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.User{
		Id: 42, Username: "sent-image-user", Password: "password123",
		Status: common.UserStatusEnabled, Group: "default", Quota: startingQuota,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id: 42, Type: constant.ChannelTypeOpenAI, Key: "upstream-key",
		Status: common.ChannelStatusEnabled, Name: "sent-image", Group: "default",
		Models: "gpt-image-1", BaseURL: &baseURL,
	}).Error)
	task := queuedSyncImageTask(t, db, "task_sync_upstream_sent", 42, reservedQuota)

	require.NoError(t, runSyncWrapperImageTask(context.Background(), task))

	require.EqualValues(t, 1, upstreamCalls.Load())
	var updated model.Task
	require.NoError(t, db.First(&updated, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), updated.Status)
	require.Equal(t, model.TaskSettlementStatusReview, updated.SettlementStatus)
	require.Equal(t, reservedQuota, updated.Quota)
	require.NotZero(t, updated.SyncSubmissionStartedAt)
	var user model.User
	require.NoError(t, db.First(&user, 42).Error)
	require.EqualValues(t, startingQuota, user.Quota)
}

func openImageTaskRunnerDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.Task{},
		&model.TaskSettlementRecord{},
		&model.User{},
		&model.Channel{},
	))
	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldUsingSQLite := common.UsingSQLite
	oldRedisEnabled := common.RedisEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	model.DB = db
	model.LOG_DB = db
	common.UsingSQLite = true
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	common.BatchUpdateEnabled = false
	t.Cleanup(func() { _ = sqlDB.Close() })
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.UsingSQLite = oldUsingSQLite
		common.RedisEnabled = oldRedisEnabled
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
	})
	return db
}

func queuedSyncImageTask(t *testing.T, db *gorm.DB, taskID string, ownerID int, quota int) *model.Task {
	t.Helper()
	body := []byte(`{"model":"gpt-image-1","prompt":"cat","n":1}`)
	bodyPath, err := common.WriteImageTaskBodyCacheFile(body)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(bodyPath) })
	task := &model.Task{
		TaskID:     taskID,
		Platform:   constant.TaskPlatformImage,
		UserId:     ownerID,
		Group:      "default",
		ChannelId:  ownerID,
		Quota:      quota,
		Action:     constant.TaskActionImageGeneration,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: time.Now().Unix(),
		Properties: model.Properties{OriginModelName: "gpt-image-1"},
		PrivateData: model.TaskPrivateData{
			PublicImageTask:          true,
			BillingSource:            service.BillingSourceWallet,
			PreConsumedUsageCaptured: true,
			ImageTaskMode:            dto.ImageTaskModeSyncWrapper,
			RequestPath:              "/v1/images/generations",
			RequestMethod:            http.MethodPost,
			RequestContentType:       "application/json",
			RequestBodyPath:          bodyPath,
			RequestBodySize:          int64(len(body)),
			Key:                      "upstream-key",
		},
	}
	require.NoError(t, db.Create(task).Error)
	require.NoError(t, db.Create(&model.TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        model.TaskSettlementRecordStatusPrepared,
	}).Error)
	return task
}

func TestExecuteSyncImageTaskRejectsStaleLeaseBeforeUpstreamSubmission(t *testing.T) {
	withTempImageTaskCache(t)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.Task{},
		&model.TaskSettlementRecord{},
		&model.User{},
		&model.Channel{},
	))

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldRedisEnabled := common.RedisEnabled
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.RedisEnabled = oldRedisEnabled
		_ = sqlDB.Close()
	})

	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1710000000,"data":[{"b64_json":"stale"}]}`))
	}))
	defer upstream.Close()

	require.NoError(t, db.Create(&model.User{
		Id: 1, Username: "image-user", Password: "password123",
		Status: common.UserStatusEnabled, Group: "default", Quota: 100000,
	}).Error)
	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Type: constant.ChannelTypeOpenAI, Key: "upstream-key",
		Status: common.ChannelStatusEnabled, Name: "openai-image", Group: "default",
		Models: "gpt-image-1", BaseURL: &baseURL,
	}).Error)

	body := []byte(`{"model":"gpt-image-1","prompt":"cat","stream":false}`)
	bodyPath, err := common.WriteImageTaskBodyCacheFile(body)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(bodyPath) })

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:     "task_sync_submission_stale_lease",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Action:     constant.TaskActionImageGeneration,
		Status:     model.TaskStatusInProgress,
		Progress:   "1%",
		SubmitTime: now - 30,
		StartTime:  now - 30,
		LockOwner:  "owner-b",
		LockUntil:  now + 60,
		Properties: model.Properties{OriginModelName: "gpt-image-1"},
		PrivateData: model.TaskPrivateData{
			PublicImageTask:    true,
			BillingSource:      "wallet",
			ImageTaskMode:      dto.ImageTaskModeSyncWrapper,
			RequestPath:        "/v1/images/generations",
			RequestMethod:      http.MethodPost,
			RequestContentType: "application/json",
			RequestBodyPath:    bodyPath,
			RequestBodySize:    int64(len(body)),
			Key:                "upstream-key",
		},
	}
	require.NoError(t, db.Create(task).Error)

	bodyStorage, contentType, err := openImageTaskBodyStorage(task)
	require.NoError(t, err)
	defer bodyStorage.Close()
	ctx := service.ContextWithImageTaskLeaseOwner(context.Background(), "owner-a")
	result, err := executeSyncImageTask(ctx, task, bodyStorage, contentType)

	require.Nil(t, result)
	require.ErrorContains(t, err, "lost CAS")
	require.Zero(t, upstreamCalls.Load())
	var updated model.Task
	require.NoError(t, db.First(&updated, task.ID).Error)
	require.Zero(t, updated.SyncSubmissionStartedAt)
}

func TestRunSyncWrapperImageTaskMarksReviewWhenResultStoreFails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.User{}, &model.Channel{}))

	oldDB := model.DB
	oldUsingSQLite := common.UsingSQLite
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldDiskCacheConfig := common.GetDiskCacheConfig()
	oldShared := constant.ImageTaskFileCacheShared
	oldTrusted := constant.ImageTaskFileCacheSharedTrusted
	oldSharedDisabled := common.ImageTaskSharedCacheDisabled()
	goodCache := oldDiskCacheConfig
	goodCache.Path = t.TempDir()
	model.DB = db
	common.UsingSQLite = true
	common.MemoryCacheEnabled = false
	constant.ImageTaskFileCacheShared = true
	constant.ImageTaskFileCacheSharedTrusted = true
	common.SetImageTaskSharedCacheDisabled(false)
	common.SetDiskCacheConfig(goodCache)
	t.Cleanup(func() {
		model.DB = oldDB
		common.UsingSQLite = oldUsingSQLite
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskFileCacheSharedTrusted = oldTrusted
		common.SetImageTaskSharedCacheDisabled(oldSharedDisabled)
		common.SetDiskCacheConfig(oldDiskCacheConfig)
		_ = sqlDB.Close()
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			http.Error(w, "unexpected path", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"created": 1710000000,
			"data": [{"b64_json": "test-b64-payload"}],
			"usage": {"prompt_tokens": 1, "completion_tokens": 0, "total_tokens": 1}
		}`))
	}))
	defer upstream.Close()

	require.NoError(t, db.Create(&model.User{
		Id:       1,
		Username: "image-user",
		Password: "password123",
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    100000,
	}).Error)
	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      1,
		Type:    constant.ChannelTypeOpenAI,
		Key:     "upstream-key",
		Status:  common.ChannelStatusEnabled,
		Name:    "openai-image",
		Group:   "default",
		Models:  "gpt-image-1",
		BaseURL: &baseURL,
	}).Error)

	body := []byte(`{"model":"gpt-image-1","prompt":"cat","stream":false}`)
	bodyPath, err := common.WriteImageTaskBodyCacheFile(body)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove(bodyPath)
	})
	blocker := filepath.Join(t.TempDir(), "cache-blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("block"), 0600))
	brokenCache := goodCache
	brokenCache.Path = blocker
	common.SetDiskCacheConfig(brokenCache)

	task := &model.Task{
		TaskID:     "task_sync_store_failure",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Action:     constant.TaskActionImageGeneration,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: time.Now().Unix(),
		Properties: model.Properties{
			OriginModelName: "gpt-image-1",
		},
		PrivateData: model.TaskPrivateData{
			ImageTaskMode:      dto.ImageTaskModeSyncWrapper,
			RequestPath:        "/v1/images/generations",
			RequestMethod:      http.MethodPost,
			RequestContentType: "application/json",
			RequestBodyPath:    bodyPath,
			RequestBodySize:    int64(len(body)),
			Key:                "upstream-key",
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, runSyncWrapperImageTask(context.Background(), task))

	var updated model.Task
	require.NoError(t, db.First(&updated, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), updated.Status)
	require.Equal(t, "100%", updated.Progress)
	require.Equal(t, model.TaskSettlementStatusReview, updated.SettlementStatus)
	require.Contains(t, updated.FailReason, "store image task result failed")
	require.Equal(t, bodyPath, updated.PrivateData.RequestBodyPath)
	require.FileExists(t, bodyPath)
}

func TestFailImageTaskRejectsLostLeaseOwner(t *testing.T) {
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

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:     "task_lost_owner_fail",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusInProgress,
		Progress:   "1%",
		SubmitTime: now,
		LockOwner:  "owner-b",
		LockUntil:  now + 60,
	}
	require.NoError(t, db.Create(task).Error)

	stale := *task
	ctx := service.ContextWithImageTaskLeaseOwner(context.Background(), "owner-a")
	require.ErrorContains(t, failImageTask(ctx, &stale, model.TaskStatusInProgress, "stale failure", false, false), "lost CAS")

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), reloaded.Status)
	require.Equal(t, "owner-b", reloaded.LockOwner)
	require.Empty(t, reloaded.FailReason)
}

func TestSettleImageTaskSuccessFinalizesAppliedSettlementWithoutResult(t *testing.T) {
	withTempImageTaskCache(t)
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

	bodyPath, err := common.WriteImageTaskBodyCacheFile([]byte(`{"model":"gpt-image-1"}`))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = common.RemoveDiskCacheFile(bodyPath)
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_applied_settlement_finalize",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusApplied,
		PrivateData: model.TaskPrivateData{
			RequestBodyPath: bodyPath,
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, settleImageTaskSuccess(context.Background(), task, imageTaskSettlementPayload{}))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskSettlementStatusSettled, reloaded.SettlementStatus)
	require.Empty(t, reloaded.PrivateData.RequestBodyPath)
	require.Empty(t, reloaded.PrivateData.RequestBodyBase64)
	require.Zero(t, reloaded.RetryCount)
	require.Empty(t, reloaded.LockOwner)
	require.Zero(t, reloaded.LockUntil)
	_, statErr := os.Stat(bodyPath)
	require.True(t, os.IsNotExist(statErr))
}

func TestMarkImageTaskSettlementReviewRetainsEvidenceAndBoundsRequestRetention(t *testing.T) {
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

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_review_retains_settlement_evidence",
		Platform:         constant.TaskPlatformImage,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusPending,
		PrivateData: model.TaskPrivateData{
			Key:                          "upstream-secret-key",
			RequestHeaders:               map[string]string{"X-Provider-Secret": "secret"},
			RequestBodyPath:              "/tmp/review-request.json",
			SettlementUsage:              &dto.Usage{PromptTokens: 12, TotalTokens: 12},
			SettlementEvidenceCapturedAt: now,
			TieredBillingSnapshot: &billingexpr.BillingSnapshot{
				BillingMode: "tiered_expr",
				ExprString:  `header("X-Billing-Tier") == "fast" ? tier("fast", p) : tier("normal", p)`,
			},
			BillingRequestInput: &billingexpr.RequestInput{
				Headers: map[string]string{"X-Billing-Tier": "fast", "X-Trace-Secret": "must-not-persist"},
				Params:  map[string]any{"quality": "high"},
			},
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, markImageTaskSettlementReview(context.Background(), task, "manual review"))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.NotNil(t, reloaded.PrivateData.SettlementUsage)
	require.Equal(t, 12, reloaded.PrivateData.SettlementUsage.PromptTokens)
	require.Equal(t, now, reloaded.PrivateData.SettlementEvidenceCapturedAt)
	require.Equal(t, "high", reloaded.PrivateData.BillingRequestInput.Params["quality"])
	require.Equal(t, map[string]string{"x-billing-tier": "fast"}, reloaded.PrivateData.BillingRequestInput.Headers)
	require.Empty(t, reloaded.PrivateData.Key)
	require.Empty(t, reloaded.PrivateData.RequestHeaders)
	require.True(t, reloaded.RequestCleanupPending)
	require.GreaterOrEqual(t, reloaded.RequestDeleteAfter, now+int64((12*time.Hour).Seconds())-2)
	require.LessOrEqual(t, reloaded.RequestDeleteAfter, now+int64((12*time.Hour).Seconds())+2)
}

func TestSettleImageTaskSuccessRejectsAppliedRecordForDifferentOperation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskSettlementRecord{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})

	task := &model.Task{
		TaskID:           "task_applied_wrong_operation",
		Platform:         constant.TaskPlatformImage,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusPending,
	}
	require.NoError(t, db.Create(task).Error)
	appliedQuota := 100
	require.NoError(t, db.Create(&model.TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        model.TaskSettlementRecordStatusApplied,
		Operation:     "refund",
		AppliedQuota:  &appliedQuota,
	}).Error)

	err = settleImageTaskSuccess(context.Background(), task, imageTaskSettlementPayload{})

	require.ErrorContains(t, err, "expected image_consumption")
	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
}

func TestMarkImageTaskSettlementSettledDefersRequestDeletionToOwnerNode(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	oldDB := model.DB
	oldNodeName := common.NodeName
	oldShared := constant.ImageTaskFileCacheShared
	oldTrusted := constant.ImageTaskFileCacheSharedTrusted
	model.DB = db
	common.NodeName = "worker-node-b"
	constant.ImageTaskFileCacheShared = false
	constant.ImageTaskFileCacheSharedTrusted = false
	t.Cleanup(func() {
		model.DB = oldDB
		common.NodeName = oldNodeName
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskFileCacheSharedTrusted = oldTrusted
		_ = sqlDB.Close()
	})

	bodyPath := filepath.Join(t.TempDir(), "foreign-request.json")
	require.NoError(t, os.WriteFile(bodyPath, []byte(`{"prompt":"private input"}`), 0o600))
	task := &model.Task{
		TaskID:           "task_settled_foreign_request_owner",
		Platform:         constant.TaskPlatformImage,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusApplied,
		StorageNode:      "worker-node-a",
		PrivateData: model.TaskPrivateData{
			NodeName:        "worker-node-a",
			RequestBodyPath: bodyPath,
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, markImageTaskSettlementSettled(context.Background(), task, model.TaskSettlementStatusApplied))
	_, err = os.Stat(bodyPath)
	require.NoError(t, err)

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskSettlementStatusSettled, reloaded.SettlementStatus)
	require.Equal(t, bodyPath, reloaded.PrivateData.RequestBodyPath)
	require.True(t, reloaded.RequestCleanupPending)
	require.LessOrEqual(t, reloaded.RequestDeleteAfter, time.Now().Unix())

	common.NodeName = "worker-node-a"
	require.NoError(t, service.CleanupDueImageTaskRequestFile(context.Background(), &reloaded))
	_, err = os.Stat(bodyPath)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Empty(t, reloaded.PrivateData.RequestBodyPath)
	require.False(t, reloaded.RequestCleanupPending)
}

func TestSettleImageTaskSuccessSkipsConsumptionWhenSettlementAlreadyApplying(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskSettlementRecord{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_settlement_already_applying",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusPending,
	}
	require.NoError(t, db.Create(task).Error)
	require.NoError(t, db.Create(&model.TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        model.TaskSettlementRecordStatusApplying,
	}).Error)

	err = settleImageTaskSuccess(context.Background(), task, imageTaskSettlementPayload{
		Result: json.RawMessage(`{"data":[{"url":"https://example.com/image.png"}]}`),
	})

	require.ErrorContains(t, err, "already applying")
	require.Equal(t, 1, task.RetryCount)

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskSettlementStatusPending, reloaded.SettlementStatus)
	require.Zero(t, reloaded.RetryCount)

	var record model.TaskSettlementRecord
	require.NoError(t, db.Where("task_primary_id = ?", task.ID).First(&record).Error)
	require.Equal(t, model.TaskSettlementRecordStatusApplying, record.Status)
}

func TestSettleImageTaskSuccessResumesAtomicApplyingSettlement(t *testing.T) {
	withTempImageTaskCache(t)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.Task{},
		&model.TaskSettlementRecord{},
		&model.User{},
		&model.Channel{},
		&model.Log{},
		&model.TokenUsageDaily{},
	))

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldUsingSQLite := common.UsingSQLite
	oldRedisEnabled := common.RedisEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	model.DB = db
	model.LOG_DB = db
	common.UsingSQLite = true
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	common.BatchUpdateEnabled = false
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.UsingSQLite = oldUsingSQLite
		common.RedisEnabled = oldRedisEnabled
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
		_ = sqlDB.Close()
	})

	require.NoError(t, db.Create(&model.User{
		Id:       1,
		Username: "image-atomic-resume-user",
		Password: "password123",
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:     1,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "upstream-key",
		Status: common.ChannelStatusEnabled,
		Name:   "image-atomic-resume-channel",
		Group:  "default",
		Models: "gpt-image-1",
	}).Error)

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_settlement_resume_atomic",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Action:           constant.TaskActionImageGeneration,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusPending,
		Properties: model.Properties{
			OriginModelName: "gpt-image-1",
		},
		PrivateData: model.TaskPrivateData{
			BillingSource: service.BillingSourceWallet,
			BillingContext: &model.TaskBillingContext{
				OriginModelName: "gpt-image-1",
				PerCallBilling:  true,
			},
		},
	}
	require.NoError(t, db.Create(task).Error)
	require.NoError(t, db.Create(&model.TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        model.TaskSettlementRecordStatusApplying,
		Operation:     model.TaskSettlementOperationImageAtomic,
	}).Error)

	require.NoError(t, settleImageTaskSuccess(context.Background(), task, imageTaskSettlementPayload{
		Result: json.RawMessage(`{"data":[{"url":"https://example.com/image.png"}]}`),
	}))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskSettlementStatusSettled, reloaded.SettlementStatus)
	var record model.TaskSettlementRecord
	require.NoError(t, db.Where("task_primary_id = ?", task.ID).First(&record).Error)
	require.Equal(t, model.TaskSettlementRecordStatusApplied, record.Status)
	require.NotEmpty(t, record.LogPayload)
}

func TestSettleImageTaskSuccessMarksReviewWhenSettlementAlreadyApplyingIsStale(t *testing.T) {
	withTempImageTaskCache(t)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskSettlementRecord{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})

	now := time.Now().Unix()
	bodyPath, err := common.WriteImageTaskBodyCacheFile([]byte(`{"model":"gpt-image-1"}`))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = common.RemoveDiskCacheFile(bodyPath)
	})

	task := &model.Task{
		TaskID:           "task_settlement_stale_applying",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusPending,
		PrivateData: model.TaskPrivateData{
			RequestBodyPath: bodyPath,
		},
	}
	require.NoError(t, db.Create(task).Error)
	staleAt := now - 3600
	require.NoError(t, db.Create(&model.TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        model.TaskSettlementRecordStatusApplying,
		CreatedAt:     staleAt,
		UpdatedAt:     staleAt,
	}).Error)

	require.NoError(t, settleImageTaskSuccess(context.Background(), task, imageTaskSettlementPayload{
		Result: json.RawMessage(`{"data":[{"url":"https://example.com/image.png"}]}`),
	}))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.Contains(t, reloaded.FailReason, "manual review")
	require.Greater(t, reloaded.NextPollAt, now)
	require.Equal(t, bodyPath, reloaded.PrivateData.RequestBodyPath)
	require.FileExists(t, bodyPath)

	var record model.TaskSettlementRecord
	require.NoError(t, db.Where("task_primary_id = ?", task.ID).First(&record).Error)
	require.Equal(t, model.TaskSettlementRecordStatusReview, record.Status)
	require.Contains(t, record.Error, "manual review")
}

func TestSettleImageTaskSuccessFinalizesExistingAppliedSettlementRecord(t *testing.T) {
	withTempImageTaskCache(t)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskSettlementRecord{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})

	bodyPath, err := common.WriteImageTaskBodyCacheFile([]byte(`{"model":"gpt-image-1"}`))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = common.RemoveDiskCacheFile(bodyPath)
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_settlement_record_applied",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Quota:            50,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusPending,
		PrivateData: model.TaskPrivateData{
			RequestBodyPath: bodyPath,
		},
	}
	require.NoError(t, db.Create(task).Error)
	appliedQuota := 200
	require.NoError(t, db.Create(&model.TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        model.TaskSettlementRecordStatusApplied,
		Operation:     "image_consumption",
		AppliedQuota:  &appliedQuota,
		AppliedAt:     now,
	}).Error)

	require.NoError(t, settleImageTaskSuccess(context.Background(), task, imageTaskSettlementPayload{
		Result: json.RawMessage(`{"data":[{"url":"https://example.com/image.png"}]}`),
	}))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskSettlementStatusSettled, reloaded.SettlementStatus)
	require.Equal(t, appliedQuota, reloaded.Quota)
	require.Empty(t, reloaded.PrivateData.RequestBodyPath)
	require.Empty(t, reloaded.PrivateData.RequestBodyBase64)
	require.Zero(t, reloaded.RetryCount)
	require.Empty(t, reloaded.LockOwner)
	require.Zero(t, reloaded.LockUntil)
	require.NoFileExists(t, bodyPath)
}

func TestSettleImageTaskSuccessMarksReviewWhenAppliedRecordLacksQuotaEvidence(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskSettlementRecord{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_settlement_record_applied_missing_result",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusPending,
		PrivateData: model.TaskPrivateData{
			ResultBodyPath: "missing-result.json",
		},
		Data: json.RawMessage(`{"_newapi_result_file":true}`),
	}
	require.NoError(t, db.Create(task).Error)
	require.NoError(t, db.Create(&model.TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        model.TaskSettlementRecordStatusApplied,
		AppliedAt:     now,
	}).Error)

	err = settleImageTaskSuccess(context.Background(), task, imageTaskSettlementPayload{})
	require.ErrorContains(t, err, "applied quota")

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
}

func TestFailImageTaskClearsExecutionSecretsAtTerminalState(t *testing.T) {
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

	task := &model.Task{
		TaskID:   "task_terminal_secret_cleanup",
		Platform: constant.TaskPlatformImage,
		Status:   model.TaskStatusQueued,
		PrivateData: model.TaskPrivateData{
			Key:            "upstream-secret-key",
			RequestHeaders: map[string]string{"X-Provider-Secret": "secret"},
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, failImageTask(context.Background(), task, model.TaskStatusQueued, "upstream failed", false, false))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Empty(t, reloaded.PrivateData.Key)
	require.Empty(t, reloaded.PrivateData.RequestHeaders)
}

func TestSettleImageTaskSuccessMarksReviewForMissingStoredResultBeforeCreatingSettlementRecord(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskSettlementRecord{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_missing_result_before_settlement_record",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusPending,
		PrivateData: model.TaskPrivateData{
			ResultBodyPath: "missing-result.json",
		},
		Data: json.RawMessage(`{"_newapi_result_file":true}`),
	}
	require.NoError(t, db.Create(task).Error)

	require.Error(t, settleImageTaskSuccess(context.Background(), task, imageTaskSettlementPayload{}))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.Contains(t, reloaded.FailReason, "settlement result unavailable")

	var recordCount int64
	require.NoError(t, db.Model(&model.TaskSettlementRecord{}).Where("task_primary_id = ?", task.ID).Count(&recordCount).Error)
	require.Zero(t, recordCount)
}

func TestSettleImageTaskSuccessParksChecksumMismatchWithoutRetry(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskSettlementRecord{}))

	oldDB := model.DB
	oldNode := common.NodeName
	model.DB = db
	common.NodeName = "node-a"
	t.Cleanup(func() {
		model.DB = oldDB
		common.NodeName = oldNode
		_ = sqlDB.Close()
	})

	body := []byte(`{"data":[{"b64_json":"mismatch"}]}`)
	resultPath := filepath.Join(t.TempDir(), "checksum-mismatch.json")
	require.NoError(t, os.WriteFile(resultPath, body, 0o600))
	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_checksum_mismatch_park",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Quota:            200,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusPending,
		Data:             json.RawMessage(`{"_newapi_result_file":true}`),
		PrivateData: model.TaskPrivateData{
			ImageTaskMode:    dto.ImageTaskModeAsyncTaskBridge,
			NodeName:         common.NodeName,
			ResultBodyPath:   resultPath,
			ResultBodySize:   int64(len(body)),
			ResultBodySHA256: "deadbeef",
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.Error(t, settleImageTaskSuccess(context.Background(), task, imageTaskSettlementPayload{}))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.Contains(t, reloaded.FailReason, "checksum mismatch")
	require.Zero(t, reloaded.NextPollAt)
	require.EqualValues(t, 200, reloaded.Quota)

	reloaded.PrivateData.ImageTaskMode = dto.ImageTaskModeAsyncTaskBridge
	reloaded.PrivateData.NodeName = common.NodeName
	require.NoError(t, settleImageTaskSuccess(context.Background(), &reloaded, imageTaskSettlementPayload{}))
	var parked model.Task
	require.NoError(t, db.First(&parked, task.ID).Error)
	require.Equal(t, model.TaskSettlementStatusReview, parked.SettlementStatus)
	require.Zero(t, parked.NextPollAt)
	require.EqualValues(t, 200, parked.Quota)
}

func TestSettleImageTaskSuccessUsesCapturedEvidenceAfterResultCleanup(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.Task{},
		&model.TaskSettlementRecord{},
		&model.User{},
		&model.Channel{},
		&model.Log{},
		&model.TokenUsageDaily{},
	))

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldRedisEnabled := common.RedisEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	oldLogConsumeEnabled := common.LogConsumeEnabled
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = false
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.RedisEnabled = oldRedisEnabled
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
		common.LogConsumeEnabled = oldLogConsumeEnabled
		_ = sqlDB.Close()
	})

	require.NoError(t, db.Create(&model.User{
		Id:       1,
		Username: "evidence-user",
		Password: "password123",
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}).Error)

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_settlement_evidence_without_result",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		Quota:            0,
		Action:           constant.TaskActionImageGeneration,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusPending,
		Data:             json.RawMessage(`{"_newapi_result_file":true,"removed":true}`),
		PrivateData: model.TaskPrivateData{
			SettlementEvidenceCapturedAt: now,
			BillingSource:                service.BillingSourceWallet,
			BillingContext: &model.TaskBillingContext{
				OriginModelName: "gpt-image-1",
				PerCallBilling:  true,
			},
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, settleImageTaskSuccess(context.Background(), task, imageTaskSettlementPayload{}))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskSettlementStatusSettled, reloaded.SettlementStatus)
	require.NotContains(t, reloaded.FailReason, "settlement result unavailable")
}

func TestSettleImageTaskSuccessMarksReviewForStoredResultMarkerWithoutPath(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskSettlementRecord{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_marker_without_result_path",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusPending,
		Data:             json.RawMessage(`{"_newapi_result_file":true}`),
	}
	require.NoError(t, db.Create(task).Error)

	require.Error(t, settleImageTaskSuccess(context.Background(), task, imageTaskSettlementPayload{}))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.Contains(t, reloaded.FailReason, "stored result body path is missing")

	var recordCount int64
	require.NoError(t, db.Model(&model.TaskSettlementRecord{}).Where("task_primary_id = ?", task.ID).Count(&recordCount).Error)
	require.Zero(t, recordCount)
}

func TestSettleImageTaskSuccessMarksReviewWhenTieredBillingBodyMissing(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskSettlementRecord{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})

	now := time.Now().Unix()
	expr := `param("quality") == "high" ? tier("high", p * 4) : tier("normal", p)`
	task := &model.Task{
		TaskID:           "task_missing_billing_body",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusPending,
		Data: json.RawMessage(`{
			"data": [{"url": "https://example.com/image.png"}],
			"usage": {"prompt_tokens": 100, "completion_tokens": 0, "total_tokens": 100}
		}`),
		PrivateData: model.TaskPrivateData{
			RequestContentType: "application/json",
			RequestBodyPath:    "missing-request-body.json",
			TieredBillingSnapshot: &billingexpr.BillingSnapshot{
				BillingMode:               "tiered_expr",
				ExprString:                expr,
				ExprHash:                  billingexpr.ExprHashString(expr),
				GroupRatio:                1,
				EstimatedPromptTokens:     100,
				EstimatedCompletionTokens: 0,
				EstimatedQuotaAfterGroup:  50,
				EstimatedTier:             "normal",
				QuotaPerUnit:              common.QuotaPerUnit,
				ExprVersion:               billingexpr.ExprVersion(expr),
			},
		},
	}
	require.NoError(t, db.Create(task).Error)
	require.NoError(t, db.Create(&model.TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        model.TaskSettlementRecordStatusPrepared,
	}).Error)

	require.Error(t, settleImageTaskSuccess(context.Background(), task, imageTaskSettlementPayload{}))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.Contains(t, reloaded.FailReason, "billing request body unavailable")

	var record model.TaskSettlementRecord
	require.NoError(t, db.Where("task_primary_id = ?", task.ID).First(&record).Error)
	require.Equal(t, model.TaskSettlementRecordStatusReview, record.Status)
	require.Contains(t, record.Error, "billing request body unavailable")
}

func TestSettleImageTaskSuccessKeepsSettlementWhenFixedPriceLogDeliveryFails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.Task{},
		&model.TaskSettlementRecord{},
		&model.User{},
		&model.Channel{},
		&model.TokenUsageDaily{},
	))

	brokenLogDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	brokenSQLDB, err := brokenLogDB.DB()
	require.NoError(t, err)

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldUsingSQLite := common.UsingSQLite
	oldRedisEnabled := common.RedisEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	oldLogConsumeEnabled := common.LogConsumeEnabled
	oldDataExportEnabled := common.DataExportEnabled
	model.DB = db
	model.LOG_DB = brokenLogDB
	common.UsingSQLite = true
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	common.DataExportEnabled = false
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.UsingSQLite = oldUsingSQLite
		common.RedisEnabled = oldRedisEnabled
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
		common.LogConsumeEnabled = oldLogConsumeEnabled
		common.DataExportEnabled = oldDataExportEnabled
		_ = brokenSQLDB.Close()
		_ = sqlDB.Close()
	})

	require.NoError(t, db.Create(&model.User{
		Id:       1,
		Username: "image-fixed-price-user",
		Password: "password123",
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    100000,
		Email:    "fixed-price@example.com",
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:     1,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "upstream-key",
		Status: common.ChannelStatusEnabled,
		Name:   "image-fixed-price-channel",
		Group:  "default",
		Models: "gpt-image-1",
	}).Error)

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_fixed_price_log_fails",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Quota:            200,
		Action:           constant.TaskActionImageGeneration,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusPending,
		Properties: model.Properties{
			OriginModelName: "gpt-image-1",
		},
		PrivateData: model.TaskPrivateData{
			BillingSource: service.BillingSourceWallet,
			BillingContext: &model.TaskBillingContext{
				ModelPrice:      0.02,
				ModelRatio:      1,
				CompletionRatio: 1,
				GroupRatio:      1,
				OriginModelName: "gpt-image-1",
				PerCallBilling:  true,
			},
		},
	}
	require.NoError(t, db.Create(task).Error)
	require.NoError(t, db.Create(&model.TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        model.TaskSettlementRecordStatusPrepared,
	}).Error)

	err = settleImageTaskSuccess(context.Background(), task, imageTaskSettlementPayload{
		Result: json.RawMessage(`{"data":[{"url":"https://example.com/image.png"}]}`),
	})
	require.NoError(t, err)

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	require.Equal(t, model.TaskSettlementStatusSettled, reloaded.SettlementStatus)
	require.Empty(t, reloaded.FailReason)

	var record model.TaskSettlementRecord
	require.NoError(t, db.Where("task_primary_id = ?", task.ID).First(&record).Error)
	require.Equal(t, model.TaskSettlementRecordStatusApplied, record.Status)
	require.NotEmpty(t, record.LogPayload)
	require.Zero(t, record.LogDeliveredAt)
	require.Equal(t, 1, record.LogAttemptCount)
	require.NotEmpty(t, record.LogError)
	var logPayload struct {
		Content string                 `json:"content"`
		Other   map[string]interface{} `json:"other"`
	}
	require.NoError(t, json.Unmarshal([]byte(record.LogPayload), &logPayload))
	require.Equal(t, true, logPayload.Other["is_task"])
	require.Equal(t, "/v1/images/generations", logPayload.Other["request_path"])
	require.EqualValues(t, 0.02, logPayload.Other["model_price"])
	require.Contains(t, logPayload.Content, string(constant.TaskActionImageGeneration))

	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	require.EqualValues(t, 100000-(reloaded.Quota-200), user.Quota)
	require.EqualValues(t, reloaded.Quota, user.UsedQuota)
	require.Equal(t, 1, user.RequestCount)

	var channel model.Channel
	require.NoError(t, db.First(&channel, 1).Error)
	require.Equal(t, int64(reloaded.Quota), channel.UsedQuota)
}

func TestImageTaskRelayStartTimePrefersExecutionStart(t *testing.T) {
	task := &model.Task{
		SubmitTime: time.Now().Add(-10 * time.Second).Unix(),
		StartTime:  time.Now().Add(-3 * time.Second).Unix(),
	}

	startTime := imageTaskRelayStartTime(task)

	require.Equal(t, task.StartTime, startTime.Unix())
}

func TestImageTaskRelayStartTimeFallsBackToSubmitTime(t *testing.T) {
	task := &model.Task{
		SubmitTime: time.Now().Add(-10 * time.Second).Unix(),
	}

	startTime := imageTaskRelayStartTime(task)

	require.Equal(t, task.SubmitTime, startTime.Unix())
}

func TestImageTaskNeedsSettlementIncludesReview(t *testing.T) {
	require.True(t, imageTaskNeedsSettlement(&model.Task{
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusReview,
		NextPollAt:       time.Now().Unix() + 60,
		Data:             json.RawMessage(`{"ok":true}`),
	}))
	require.True(t, imageTaskNeedsSettlement(&model.Task{
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusReview,
		PrivateData: model.TaskPrivateData{
			SettlementEvidenceCapturedAt: time.Now().Unix(),
		},
	}))
	require.False(t, imageTaskNeedsSettlement(&model.Task{
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusSettled,
	}))
	require.False(t, imageTaskNeedsSettlement(&model.Task{
		Status:           model.TaskStatusFailure,
		SettlementStatus: model.TaskSettlementStatusReview,
	}))
}

func TestImageTaskNeedsSettlementExcludesUnrecoverableReview(t *testing.T) {
	now := time.Now().Unix()
	require.False(t, imageTaskNeedsSettlement(&model.Task{
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusReview,
		ResultCleanedAt:  now,
		FailReason:       "image task result expired before settlement completed",
	}))
	require.False(t, imageTaskNeedsSettlement(&model.Task{
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusReview,
		NextPollAt:       0,
	}))
	require.False(t, imageTaskNeedsSettlement(&model.Task{
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusReview,
		NextPollAt:       now + 60,
	}))
}

func TestRunImageTasksRetriesSettlementReviewAndSettles(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.Task{},
		&model.TaskSettlementRecord{},
		&model.User{},
		&model.Channel{},
		&model.Log{},
		&model.TokenUsageDaily{},
	))

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldUsingSQLite := common.UsingSQLite
	oldRedisEnabled := common.RedisEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	oldLogConsumeEnabled := common.LogConsumeEnabled
	oldDataExportEnabled := common.DataExportEnabled
	model.DB = db
	model.LOG_DB = db
	common.UsingSQLite = true
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	common.DataExportEnabled = false
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.UsingSQLite = oldUsingSQLite
		common.RedisEnabled = oldRedisEnabled
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
		common.LogConsumeEnabled = oldLogConsumeEnabled
		common.DataExportEnabled = oldDataExportEnabled
		_ = sqlDB.Close()
	})

	require.NoError(t, db.Create(&model.User{
		Id:       1,
		Username: "image-review-retry-user",
		Password: "password123",
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    100000,
		Email:    "review-retry@example.com",
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:     1,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "upstream-key",
		Status: common.ChannelStatusEnabled,
		Name:   "image-review-retry-channel",
		Group:  "default",
		Models: "gpt-image-1",
	}).Error)

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_run_image_settlement_review",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Quota:            200,
		Action:           constant.TaskActionImageGeneration,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		FailReason:       "image task settlement requires manual review",
		SettlementStatus: model.TaskSettlementStatusReview,
		NextPollAt:       0,
		Properties: model.Properties{
			OriginModelName: "gpt-image-1",
		},
		PrivateData: model.TaskPrivateData{
			BillingSource:                service.BillingSourceWallet,
			SettlementAttemptQuota:       200,
			SettlementError:              "record consume log failed",
			SettlementEvidenceCapturedAt: now,
			BillingContext: &model.TaskBillingContext{
				ModelPrice:      0.02,
				ModelRatio:      1,
				CompletionRatio: 1,
				GroupRatio:      1,
				OriginModelName: "gpt-image-1",
				PerCallBilling:  true,
			},
		},
	}
	require.NoError(t, db.Create(task).Error)
	require.NoError(t, db.Create(&model.TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        model.TaskSettlementRecordStatusPrepared,
	}).Error)

	require.NoError(t, RunImageTasks(context.Background(), []*model.Task{task}))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	require.Equal(t, model.TaskSettlementStatusSettled, reloaded.SettlementStatus)
	require.Empty(t, reloaded.FailReason)
}

func TestRunImageTasksDoesNotRetryExpiredSettlementReview(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskSettlementRecord{}))

	oldDB := model.DB
	oldUsingSQLite := common.UsingSQLite
	model.DB = db
	common.UsingSQLite = true
	t.Cleanup(func() {
		model.DB = oldDB
		common.UsingSQLite = oldUsingSQLite
		_ = sqlDB.Close()
	})

	now := time.Now().Unix()
	expiredReason := "image task result expired before settlement completed"
	task := &model.Task{
		TaskID:           "task_expired_settlement_review",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		ChannelId:        1,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		FinishTime:       now - 60,
		FailReason:       expiredReason,
		SettlementStatus: model.TaskSettlementStatusReview,
		ResultCleanedAt:  now,
		NextPollAt:       0,
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, RunImageTasks(context.Background(), []*model.Task{task}))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.Equal(t, expiredReason, reloaded.FailReason)
	require.Zero(t, reloaded.NextPollAt)
}

func TestRunImageTasksSettlesRemovedBridgeWhenCleanupMarkedResultButFileRemains(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.Task{},
		&model.TaskSettlementRecord{},
		&model.User{},
		&model.Channel{},
		&model.Log{},
		&model.TokenUsageDaily{},
	))

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldUsingSQLite := common.UsingSQLite
	oldNode := common.NodeName
	oldRedisEnabled := common.RedisEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	oldLogConsumeEnabled := common.LogConsumeEnabled
	model.DB = db
	model.LOG_DB = db
	common.UsingSQLite = true
	common.NodeName = "node-a"
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = false
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.UsingSQLite = oldUsingSQLite
		common.NodeName = oldNode
		common.RedisEnabled = oldRedisEnabled
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
		common.LogConsumeEnabled = oldLogConsumeEnabled
		_ = sqlDB.Close()
	})

	require.NoError(t, db.Create(&model.User{
		Id: 1, Username: "bridge-cleaned-file-user", Password: "password123",
		Status: common.UserStatusEnabled, Group: "default", Quota: 0,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Type: constant.ChannelTypeOpenAI, Key: "upstream-key",
		Status: common.ChannelStatusEnabled, Name: "bridge-cleaned-file", Group: "default", Models: "gpt-image-1",
	}).Error)

	now := time.Now().Unix()
	body := []byte(`{"data":[{"b64_json":"still-billable"}]}`)
	resultPath := filepath.Join(t.TempDir(), "cleaned-still-there.json")
	require.NoError(t, os.WriteFile(resultPath, body, 0o600))
	task := &model.Task{
		TaskID:           "task_bridge_cleaned_file_still_billable",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Quota:            200,
		Action:           constant.TaskActionImageGeneration,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusReview,
		ResultCleanedAt:  now,
		NextPollAt:       0,
		Data:             json.RawMessage(`{"_newapi_result_file":true,"removed":true}`),
		Properties:       model.Properties{OriginModelName: "gpt-image-1"},
		PrivateData: model.TaskPrivateData{
			ImageTaskMode:    dto.ImageTaskModeAsyncTaskBridge,
			BillingSource:    service.BillingSourceWallet,
			NodeName:         common.NodeName,
			ResultBodyPath:   resultPath,
			ResultBodySize:   int64(len(body)),
			ResultBodySHA256: "",
			BillingContext: &model.TaskBillingContext{
				ModelPrice:      0.02,
				ModelRatio:      1,
				CompletionRatio: 1,
				GroupRatio:      1,
				OriginModelName: "gpt-image-1",
				PerCallBilling:  true,
			},
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, RunImageTasks(context.Background(), []*model.Task{task}))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	require.Equal(t, model.TaskSettlementStatusSettled, reloaded.SettlementStatus)
	require.NotZero(t, reloaded.Quota)
	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	require.Less(t, user.Quota, int64(200))
}

func TestRunImageTasksParksEmptySettlementReviewWithoutEvidence(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskSettlementRecord{}))

	oldDB := model.DB
	oldUsingSQLite := common.UsingSQLite
	model.DB = db
	common.UsingSQLite = true
	t.Cleanup(func() {
		model.DB = oldDB
		common.UsingSQLite = oldUsingSQLite
		_ = sqlDB.Close()
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_empty_settlement_review",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		ChannelId:        1,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusPending,
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, RunImageTasks(context.Background(), []*model.Task{task}))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.Equal(t, "image task success result is empty, cannot settle billing", reloaded.FailReason)
	require.Zero(t, reloaded.NextPollAt)
}
