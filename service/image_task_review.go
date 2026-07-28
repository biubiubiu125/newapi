package service

import (
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// PrepareImageTaskExecutionReview closes an image task whose upstream outcome
// is unknown without refunding quota that the upstream may already have spent.
func PrepareImageTaskExecutionReview(task *model.Task, now int64, reason string) {
	if task == nil {
		return
	}
	if now <= 0 {
		now = time.Now().Unix()
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "image task upstream execution outcome requires manual review"
	}

	task.Status = model.TaskStatusFailure
	task.Progress = "100%"
	task.FinishTime = now
	task.FailReason = reason
	task.NextPollAt = 0
	task.LockOwner = ""
	task.LockUntil = 0
	task.RetryCount = 0
	task.SettlementStatus = model.TaskSettlementStatusReview
	task.RefundPending = false
	task.PrivateData.SettlementAttemptQuota = 0
	task.PrivateData.SettlementError = reason
	task.ClearImageTaskExecutionSecrets()

	deleteAfter := now + int64(common.GetImageTaskResultCacheRetention().Seconds())
	ScheduleImageTaskRequestFileCleanup(task, deleteAfter)
}
