package model

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAcknowledgeImageTaskResultReportsFirstWriter(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:           "task_result_ack_first_writer",
		Platform:         constant.TaskPlatformImage,
		Status:           TaskStatusSuccess,
		SettlementStatus: TaskSettlementStatusSettled,
		ResultExpiresAt:  now + 3600,
	}
	task.SetData(map[string]any{"data": []any{map[string]any{"url": "https://example.com/ack.png"}}})
	require.NoError(t, task.Insert())

	first, err := AcknowledgeImageTaskResult(task.ID, now, now+120)
	require.NoError(t, err)
	require.True(t, first)
	second, err := AcknowledgeImageTaskResult(task.ID, now+1, now+121)
	require.NoError(t, err)
	require.False(t, second)
}

func TestCleanupExpiredImageTaskResultsClearsContentButKeepsTaskState(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	due := &Task{
		TaskID:                "task_result_cleanup_due",
		Platform:              constant.TaskPlatformImage,
		Status:                TaskStatusSuccess,
		SettlementStatus:      TaskSettlementStatusSettled,
		FinishTime:            now - 60,
		ResultExpiresAt:       now + 3600,
		ResultAcknowledgedAt:  now - 180,
		ResultDeleteAfter:     now - 1,
		ImageTaskResultStored: true,
		PrivateData: TaskPrivateData{
			ResultBodyPath:    "/tmp/newapi-result-due.json",
			ResultBodySize:    42,
			ResultBodySHA256:  "sha",
			ResultContentType: "application/json",
			ResultStoredAt:    now - 60,
			ResultExpiresAt:   now + 3600,
		},
	}
	due.SetData(map[string]any{"data": []any{map[string]any{"b64_json": "secret-result"}}})
	require.NoError(t, due.Insert())

	future := &Task{
		TaskID:           "task_result_cleanup_future",
		Platform:         constant.TaskPlatformImage,
		Status:           TaskStatusSuccess,
		SettlementStatus: TaskSettlementStatusSettled,
		FinishTime:       now,
		ResultExpiresAt:  now + 3600,
	}
	future.SetData(map[string]any{"data": []any{map[string]any{"url": "https://example.com/future.png"}}})
	require.NoError(t, future.Insert())

	cleanups, err := CleanupExpiredImageTaskResults(now, 12*time.Hour, 100)

	require.NoError(t, err)
	require.Equal(t, []ImageTaskResultCleanup{{TaskPrimaryID: due.ID, Path: "/tmp/newapi-result-due.json"}}, cleanups)

	reloaded, exists, err := GetTaskByID(due.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, TaskStatus(TaskStatusSuccess), reloaded.Status)
	require.Equal(t, TaskSettlementStatusSettled, reloaded.SettlementStatus)
	require.Equal(t, due.ResultAcknowledgedAt, reloaded.ResultAcknowledgedAt)
	require.Equal(t, now, reloaded.ResultCleanedAt)
	require.Zero(t, reloaded.ResultDeleteAfter)
	require.Equal(t, "/tmp/newapi-result-due.json", reloaded.PrivateData.ResultBodyPath)
	require.True(t, reloaded.ResultCleanupPending)
	require.False(t, reloaded.ImageTaskResultStored)
	require.NotContains(t, string(reloaded.Data), "secret-result")
	require.Contains(t, string(reloaded.Data), "_newapi_result_file")

	reloadedFuture, exists, err := GetTaskByID(future.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Zero(t, reloadedFuture.ResultCleanedAt)
	require.Contains(t, string(reloadedFuture.Data), "future.png")
}

func TestCleanupExpiredImageTaskResultsMarksOpenSettlementAsReview(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:                "task_result_cleanup_open_settlement",
		Platform:              constant.TaskPlatformImage,
		Status:                TaskStatusSuccess,
		SettlementStatus:      TaskSettlementStatusPending,
		FinishTime:            now - 60,
		ResultExpiresAt:       now - 1,
		ImageTaskResultStored: true,
		PrivateData: TaskPrivateData{
			ResultBodyPath:   "/tmp/newapi-result-open-settlement.json",
			ResultBodySize:   12,
			ResultBodySHA256: "sha",
			ResultStoredAt:   now - 60,
			ResultExpiresAt:  now - 1,
			// 无结算证据：结果到期后必须 REVIEW，避免永久 finalizing。
		},
	}
	task.SetData(map[string]any{"data": []any{map[string]any{"b64_json": "pending-result"}}})
	require.NoError(t, task.Insert())

	cleanups, err := CleanupExpiredImageTaskResults(now, 12*time.Hour, 100)
	require.NoError(t, err)
	require.Equal(t, []ImageTaskResultCleanup{{TaskPrimaryID: task.ID, Path: "/tmp/newapi-result-open-settlement.json"}}, cleanups)

	reloaded, exists, err := GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, TaskStatus(TaskStatusSuccess), reloaded.Status)
	require.Equal(t, TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.Equal(t, imageTaskResultExpiredBeforeSettlementReason, reloaded.FailReason)
	require.Equal(t, now, reloaded.ResultCleanedAt)
	require.Zero(t, reloaded.NextPollAt)
	require.NotContains(t, string(reloaded.Data), "pending-result")
}

func TestCleanupExpiredImageTaskResultsKeepsPendingWhenSettlementEvidenceExists(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:                "task_result_cleanup_pending_with_evidence",
		Platform:              constant.TaskPlatformImage,
		Status:                TaskStatusSuccess,
		SettlementStatus:      TaskSettlementStatusPending,
		FinishTime:            now - 60,
		ResultExpiresAt:       now - 1,
		ImageTaskResultStored: true,
		PrivateData: TaskPrivateData{
			ResultBodyPath:               "/tmp/newapi-result-pending-evidence.json",
			ResultBodySize:               12,
			ResultBodySHA256:             "sha",
			ResultStoredAt:               now - 60,
			ResultExpiresAt:              now - 1,
			SettlementEvidenceCapturedAt: now - 60,
			BillingRequestInputCaptured:  true,
		},
	}
	task.SetData(map[string]any{"data": []any{map[string]any{"b64_json": "pending-with-evidence"}}})
	require.NoError(t, task.Insert())

	cleanups, err := CleanupExpiredImageTaskResults(now, 12*time.Hour, 100)
	require.NoError(t, err)
	require.Equal(t, []ImageTaskResultCleanup{{TaskPrimaryID: task.ID, Path: "/tmp/newapi-result-pending-evidence.json"}}, cleanups)

	reloaded, exists, err := GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, TaskSettlementStatusPending, reloaded.SettlementStatus)
	require.Equal(t, now, reloaded.ResultCleanedAt)
	require.Empty(t, reloaded.FailReason)
	require.NotContains(t, string(reloaded.Data), "pending-with-evidence")
}

func TestMarkExpiredOpenImageTaskSettlementReviewIsIdempotent(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:           "task_open_settlement_review_mark",
		Platform:         constant.TaskPlatformImage,
		Status:           TaskStatusSuccess,
		SettlementStatus: TaskSettlementStatusPending,
		ResultExpiresAt:  now - 5,
		FailReason:       "",
	}
	require.NoError(t, task.Insert())

	first, err := MarkExpiredOpenImageTaskSettlementReview(task.ID, now)
	require.NoError(t, err)
	require.True(t, first)
	second, err := MarkExpiredOpenImageTaskSettlementReview(task.ID, now)
	require.NoError(t, err)
	require.False(t, second)

	reloaded, exists, err := GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.Equal(t, imageTaskResultExpiredBeforeSettlementReason, reloaded.FailReason)
}

func TestMarkExpiredOpenImageTaskSettlementReviewSkipsApplied(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:           "task_open_settlement_review_skip_applied",
		Platform:         constant.TaskPlatformImage,
		Status:           TaskStatusSuccess,
		SettlementStatus: TaskSettlementStatusApplied,
		ResultExpiresAt:  now - 5,
	}
	require.NoError(t, task.Insert())

	updated, err := MarkExpiredOpenImageTaskSettlementReview(task.ID, now)
	require.NoError(t, err)
	require.False(t, updated)

	reloaded, exists, err := GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, TaskSettlementStatusApplied, reloaded.SettlementStatus)
}

func TestMarkExpiredOpenImageTaskSettlementReviewSkipsPendingWithEvidence(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:           "task_open_settlement_review_skip_evidence",
		Platform:         constant.TaskPlatformImage,
		Status:           TaskStatusSuccess,
		SettlementStatus: TaskSettlementStatusPending,
		ResultExpiresAt:  now - 5,
		PrivateData: TaskPrivateData{
			SettlementEvidenceCapturedAt: now - 10,
		},
	}
	require.NoError(t, task.Insert())

	updated, err := MarkExpiredOpenImageTaskSettlementReview(task.ID, now)
	require.NoError(t, err)
	require.False(t, updated)

	reloaded, exists, err := GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, TaskSettlementStatusPending, reloaded.SettlementStatus)
}

func TestFinalizeImageTaskResultFileCleanupClearsPendingPath(t *testing.T) {
	truncateTables(t)
	task := &Task{
		TaskID:                "task_result_cleanup_finalize",
		Platform:              constant.TaskPlatformImage,
		Status:                TaskStatusSuccess,
		ResultCleanedAt:       time.Now().Unix(),
		ResultCleanupPending:  true,
		ImageTaskResultStored: true,
		PrivateData: TaskPrivateData{
			ResultBodyPath:   "/tmp/newapi-result-finalize.json",
			ResultBodySHA256: "sha",
		},
	}
	require.NoError(t, task.Insert())

	require.NoError(t, FinalizeImageTaskResultFileCleanup(task.ID, task.PrivateData.ResultBodyPath))

	reloaded, exists, err := GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.False(t, reloaded.ResultCleanupPending)
	require.Empty(t, reloaded.PrivateData.ResultBodyPath)
	require.Empty(t, reloaded.PrivateData.ResultBodySHA256)
	require.False(t, reloaded.ImageTaskResultStored)
}

func TestCleanupExpiredImageTaskResultsUsesLegacyFinishTimeFallback(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:           "task_result_cleanup_legacy",
		Platform:         constant.TaskPlatformImage,
		Status:           TaskStatusSuccess,
		SettlementStatus: TaskSettlementStatusSettled,
		FinishTime:       now - int64((13 * time.Hour).Seconds()),
	}
	task.SetData(map[string]any{"data": []any{map[string]any{"url": "https://example.com/legacy.png"}}})
	require.NoError(t, task.Insert())

	_, err := CleanupExpiredImageTaskResults(now, 12*time.Hour, 100)

	require.NoError(t, err)
	reloaded, exists, err := GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, task.FinishTime+int64((12*time.Hour).Seconds()), reloaded.ResultExpiresAt)
	require.Equal(t, now, reloaded.ResultCleanedAt)
}

func TestCleanupExpiredImageTaskResultsAlsoClearsSettlementReviewResult(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:           "task_result_cleanup_review",
		Platform:         constant.TaskPlatformImage,
		Status:           TaskStatusSuccess,
		SettlementStatus: TaskSettlementStatusReview,
		FinishTime:       now - 60,
		ResultExpiresAt:  now - 1,
	}
	task.SetData(map[string]any{"data": []any{map[string]any{"url": "https://example.com/review.png"}}})
	require.NoError(t, task.Insert())

	_, err := CleanupExpiredImageTaskResults(now, 12*time.Hour, 100)

	require.NoError(t, err)
	reloaded, exists, err := GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, now, reloaded.ResultCleanedAt)
	require.NotContains(t, string(reloaded.Data), "review.png")
}

func TestCleanupExpiredImageTaskResultsClearsUnsettledSuccessResult(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	for _, settlementStatus := range []string{TaskSettlementStatusPending, TaskSettlementStatusApplied} {
		task := &Task{
			TaskID:           "task_result_cleanup_unsettled_" + strings.ToLower(settlementStatus),
			Platform:         constant.TaskPlatformImage,
			Status:           TaskStatusSuccess,
			SettlementStatus: settlementStatus,
			FinishTime:       now - int64((13 * time.Hour).Seconds()),
			ResultExpiresAt:  now - 1,
		}
		task.SetData(map[string]any{"data": []any{map[string]any{"url": "https://example.com/unsettled.png"}}})
		require.NoError(t, task.Insert())
	}

	cleanups, err := CleanupExpiredImageTaskResults(now, 12*time.Hour, 100)

	require.NoError(t, err)
	require.Len(t, cleanups, 2)
	for _, cleanup := range cleanups {
		require.Empty(t, cleanup.Path)
	}
	var tasks []Task
	require.NoError(t, DB.Order("id ASC").Find(&tasks).Error)
	require.Len(t, tasks, 2)
	for _, task := range tasks {
		require.Equal(t, now, task.ResultCleanedAt)
		require.NotContains(t, string(task.Data), "unsettled.png")
	}
}

func TestUpdateSettlementStatusPreservesCompletedResultCleanup(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:                "task_settlement_after_result_cleanup",
		Platform:              constant.TaskPlatformImage,
		Status:                TaskStatusSuccess,
		SettlementStatus:      TaskSettlementStatusApplied,
		FinishTime:            now - int64((13 * time.Hour).Seconds()),
		ResultExpiresAt:       now - 1,
		ImageTaskResultStored: true,
		PrivateData: TaskPrivateData{
			ResultBodyPath:   "/tmp/result-cleaned-before-settlement.json",
			ResultBodySize:   128,
			ResultBodySHA256: "result-sha",
			ResultStoredAt:   now - int64((13 * time.Hour).Seconds()),
			ResultExpiresAt:  now - 1,
		},
	}
	task.SetData(map[string]any{"data": []any{map[string]any{"b64_json": "must-stay-deleted"}}})
	require.NoError(t, task.Insert())

	var stale Task
	require.NoError(t, DB.First(&stale, task.ID).Error)
	cleanups, err := CleanupExpiredImageTaskResults(now, 12*time.Hour, 100)
	require.NoError(t, err)
	require.Equal(t, []ImageTaskResultCleanup{{TaskPrimaryID: task.ID, Path: task.PrivateData.ResultBodyPath}}, cleanups)
	require.NoError(t, FinalizeImageTaskResultFileCleanup(task.ID, task.PrivateData.ResultBodyPath))

	stale.SettlementStatus = TaskSettlementStatusSettled
	won, err := stale.UpdateSettlementStatus(TaskStatusSuccess, TaskSettlementStatusApplied)
	require.NoError(t, err)
	require.True(t, won)

	reloaded, exists, err := GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, TaskSettlementStatusSettled, reloaded.SettlementStatus)
	require.Equal(t, now, reloaded.ResultCleanedAt)
	require.False(t, reloaded.ResultCleanupPending)
	require.False(t, reloaded.ImageTaskResultStored)
	require.Empty(t, reloaded.PrivateData.ResultBodyPath)
	require.Empty(t, reloaded.PrivateData.ResultBodySHA256)
	require.NotContains(t, string(reloaded.Data), "must-stay-deleted")
	require.Contains(t, string(reloaded.Data), "_newapi_result_file")
}

func TestGetPublicImageTaskByTaskIDRequiresOwnerTokenInQuery(t *testing.T) {
	truncateTables(t)
	task := &Task{
		TaskID:                 "task_public_token_scoped_single",
		Platform:               constant.TaskPlatformImage,
		UserId:                 77,
		Status:                 TaskStatusSuccess,
		SettlementStatus:       TaskSettlementStatusSettled,
		PublicImageTask:        true,
		PublicImageTaskTokenID: 700,
		PrivateData: TaskPrivateData{
			PublicImageTask: true,
			TokenId:         700,
		},
	}
	task.SetData(map[string]any{"data": []any{map[string]any{"url": "https://example.com/token-single.png"}}})
	require.NoError(t, task.Insert())

	loaded, exists, err := GetPublicImageTaskByTaskID(77, 701, task.TaskID)

	require.NoError(t, err)
	require.False(t, exists)
	require.Nil(t, loaded)
}

func TestGetPublicImageTasksByTaskIDsRequiresOwnerTokenInQuery(t *testing.T) {
	truncateTables(t)
	task := &Task{
		TaskID:                 "task_public_token_scoped_list",
		Platform:               constant.TaskPlatformImage,
		UserId:                 77,
		Status:                 TaskStatusSuccess,
		SettlementStatus:       TaskSettlementStatusSettled,
		PublicImageTask:        true,
		PublicImageTaskTokenID: 700,
		PrivateData: TaskPrivateData{
			PublicImageTask: true,
			TokenId:         700,
		},
	}
	task.SetData(map[string]any{"data": []any{map[string]any{"url": "https://example.com/token-list.png"}}})
	require.NoError(t, task.Insert())

	tasks, err := GetPublicImageTasksByTaskIDs(77, 701, []any{task.TaskID})

	require.NoError(t, err)
	require.Empty(t, tasks)
}

func TestGetPublicImageTaskFullByTaskIDRequiresOwnerTokenInQuery(t *testing.T) {
	truncateTables(t)
	task := &Task{
		TaskID:                 "task_public_token_scoped_full",
		Platform:               constant.TaskPlatformImage,
		UserId:                 77,
		Status:                 TaskStatusSuccess,
		SettlementStatus:       TaskSettlementStatusSettled,
		PublicImageTask:        true,
		PublicImageTaskTokenID: 700,
		PrivateData: TaskPrivateData{
			PublicImageTask: true,
			TokenId:         700,
		},
	}
	task.SetData(map[string]any{"data": []any{map[string]any{"url": "https://example.com/token-full.png"}}})
	require.NoError(t, task.Insert())

	loaded, exists, err := GetPublicImageTaskFullByTaskID(77, 701, task.TaskID)

	require.NoError(t, err)
	require.False(t, exists)
	require.Nil(t, loaded)
}

func TestGetPublicImageTasksByTaskIDsDoesNotLoadInlineResultPayload(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:                 "task_public_metadata_large_result",
		Platform:               constant.TaskPlatformImage,
		UserId:                 77,
		Status:                 TaskStatusSuccess,
		SettlementStatus:       TaskSettlementStatusSettled,
		ResultExpiresAt:        now + 3600,
		PublicImageTask:        true,
		PublicImageTaskTokenID: 700,
		ImageTaskResultStored:  true,
		PrivateData: TaskPrivateData{
			PublicImageTask:   true,
			TokenId:           700,
			Key:               "must-not-load",
			RequestBodyBase64: strings.Repeat("C", 1024*1024),
			RequestHeaders:    map[string]string{"X-Secret": "must-not-load"},
			ResultBodyPath:    "/cache/large-result.json",
		},
	}
	task.SetData(map[string]any{"data": []any{map[string]any{"b64_json": strings.Repeat("A", 1024*1024)}}})
	require.NoError(t, task.Insert())
	callbackName := "test:reject_public_status_private_data_query"
	require.NoError(t, DB.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Table != "tasks" {
			return
		}
		for _, selected := range tx.Statement.Selects {
			if strings.Contains(strings.ToLower(selected), "private_data") {
				tx.AddError(errors.New("public status query selected private_data"))
				return
			}
		}
	}))
	t.Cleanup(func() { DB.Callback().Query().Remove(callbackName) })

	tasks, err := GetPublicImageTasksByTaskIDs(77, 700, []any{task.TaskID})

	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.True(t, tasks[0].InlineResultAvailable)
	require.True(t, tasks[0].StoredResultAvailable)
	require.Empty(t, tasks[0].Data)
	require.Empty(t, tasks[0].PrivateData.Key)
	require.Empty(t, tasks[0].PrivateData.RequestBodyBase64)
	require.Empty(t, tasks[0].PrivateData.RequestHeaders)
	require.Empty(t, tasks[0].PrivateData.ResultBodyPath)
	require.True(t, tasks[0].PrivateData.PublicImageTask)
	require.Equal(t, 700, tasks[0].PrivateData.TokenId)
}

func TestGetPendingImageTaskRefundsOnlyReturnsDurableIntentsWithoutResultData(t *testing.T) {
	truncateTables(t)
	pending := &Task{
		TaskID:        "task_refund_pending",
		Platform:      constant.TaskPlatformImage,
		Status:        TaskStatusFailure,
		Quota:         120,
		RefundPending: true,
	}
	pending.SetData(map[string]any{"secret": strings.Repeat("B", 1024*1024)})
	require.NoError(t, pending.Insert())
	require.NoError(t, (&Task{
		TaskID:        "task_refund_not_pending",
		Platform:      constant.TaskPlatformImage,
		Status:        TaskStatusFailure,
		Quota:         120,
		RefundPending: false,
	}).Insert())
	require.NoError(t, (&Task{
		TaskID:           "task_refund_manual_review",
		Platform:         constant.TaskPlatformImage,
		Status:           TaskStatusFailure,
		Quota:            120,
		RefundPending:    true,
		SettlementStatus: TaskSettlementStatusReview,
	}).Insert())

	tasks, err := GetPendingImageTaskRefundsAfter(0, 100)

	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, pending.ID, tasks[0].ID)
	require.Empty(t, tasks[0].Data)
}

func TestGetPendingImageTaskRequestFileCleanupsOnlyReturnsDueRecords(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	legacy := &Task{
		TaskID:                "task_request_cleanup_legacy",
		Platform:              constant.TaskPlatformImage,
		Status:                TaskStatusFailure,
		RequestCleanupPending: true,
		PrivateData: TaskPrivateData{
			RequestBodyPath:   "/tmp/legacy-request.json",
			RequestBodyBase64: strings.Repeat("A", 1024*1024),
			Key:               "legacy-cleanup-secret",
		},
	}
	require.NoError(t, legacy.Insert())
	due := &Task{
		TaskID:                "task_request_cleanup_due",
		Platform:              constant.TaskPlatformImage,
		Status:                TaskStatusFailure,
		RequestCleanupPending: true,
		RequestDeleteAfter:    now - 1,
		PrivateData:           TaskPrivateData{RequestBodyPath: "/tmp/due-request.json"},
	}
	require.NoError(t, due.Insert())
	future := &Task{
		TaskID:                "task_request_cleanup_future",
		Platform:              constant.TaskPlatformImage,
		Status:                TaskStatusSuccess,
		SettlementStatus:      TaskSettlementStatusReview,
		RequestCleanupPending: true,
		RequestDeleteAfter:    now + 3600,
		PrivateData:           TaskPrivateData{RequestBodyPath: "/tmp/future-request.json"},
	}
	require.NoError(t, future.Insert())

	pending, err := GetPendingImageTaskRequestFileCleanupsAfter(now, 0, 100)

	require.NoError(t, err)
	require.Len(t, pending, 2)
	require.Equal(t, legacy.ID, pending[0].ID)
	require.Equal(t, due.ID, pending[1].ID)
	require.Empty(t, pending[0].PrivateData.RequestBodyBase64)
	require.Empty(t, pending[0].PrivateData.Key)

	require.NoError(t, FinalizeImageTaskRequestFileCleanup(due.ID, due.PrivateData.RequestBodyPath))
	reloaded, exists, err := GetTaskByID(due.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.False(t, reloaded.RequestCleanupPending)
	require.Zero(t, reloaded.RequestDeleteAfter)
	require.Empty(t, reloaded.PrivateData.RequestBodyPath)
}

func TestGetPendingImageTaskResultFileCleanupsReturnsOnlyCleanupMetadata(t *testing.T) {
	truncateTables(t)
	task := &Task{
		TaskID:               "task_result_cleanup_large_private_data",
		Platform:             constant.TaskPlatformImage,
		Status:               TaskStatusSuccess,
		ResultCleanupPending: true,
		PrivateData: TaskPrivateData{
			ResultBodyPath:    "/tmp/large-result-cleanup.json",
			RequestBodyBase64: strings.Repeat("B", 1024*1024),
			Key:               "result-cleanup-secret",
		},
	}
	require.NoError(t, task.Insert())

	cleanups, err := GetPendingImageTaskResultFileCleanupsAfter(0, 100)

	require.NoError(t, err)
	require.Equal(t, []ImageTaskResultCleanup{{
		TaskPrimaryID: task.ID,
		Path:          "/tmp/large-result-cleanup.json",
	}}, cleanups)
}
