package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type asyncTaskAccountingApply struct {
	operation           string
	actualQuota         int
	quotaDelta          int
	preConsumedQuota    int
	logType             int
	logQuota            int
	logContent          string
	allowMissingChannel bool
	skipQuotaData       bool
	adjustUsage         bool
	extraOther          map[string]interface{}
}

func isRetryableAsyncTaskAccountingError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "mark async task accounting applied")
}

func DispatchPendingTaskSettlementLogs(ctx context.Context, limit int) error {
	return DispatchPendingImageTaskSettlementLogs(ctx, limit)
}

func applyAsyncTaskAccountingAtomic(ctx context.Context, task *model.Task, input asyncTaskAccountingApply) error {
	if task == nil || task.ID <= 0 {
		return errors.New("task is required")
	}

	var persistedAfter model.Task
	err := model.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var persisted model.Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", task.ID).
			First(&persisted).Error; err != nil {
			return err
		}
		var record model.TaskSettlementRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("task_primary_id = ?", persisted.ID).
			First(&record).Error; err != nil {
			return err
		}

		switch record.Status {
		case model.TaskSettlementRecordStatusApplied:
			if err := finalizeLockedAsyncTaskAccountingTx(tx, &persisted, &record, input); err != nil {
				return err
			}
			persistedAfter = persisted
			return nil
		case model.TaskSettlementRecordStatusReview:
			return taskAccountingReviewError(input.operation, &record)
		case model.TaskSettlementRecordStatusPrepared, model.TaskSettlementRecordStatusApplying:
		default:
			return fmt.Errorf("async task accounting has unexpected record status %s", record.Status)
		}

		if err := applyImageTaskFundingSettlementTx(tx, &persisted, input.quotaDelta); err != nil {
			if input.operation == taskSettlementOperationRefund {
				return fmt.Errorf("refund funding failed: %w", err)
			}
			return fmt.Errorf("task quota settlement funding adjustment failed: %w", err)
		}
		tokenID, err := applyImageTaskTokenSettlementTx(tx, &persisted, input.quotaDelta)
		if err != nil {
			if input.operation == taskSettlementOperationRefund {
				return fmt.Errorf("refund token quota failed: %w", err)
			}
			return fmt.Errorf("task quota settlement token adjustment failed: %w", err)
		}
		if input.adjustUsage && input.quotaDelta != 0 {
			usageTokenID := 0
			if tokenID > 0 {
				usageTokenID = persisted.PrivateData.TokenId
			}
			if err := model.UpdateTaskUsageAdjustmentWithTokenTx(
				tx,
				persisted.UserId,
				persisted.ChannelId,
				usageTokenID,
				input.quotaDelta,
				input.allowMissingChannel && input.quotaDelta < 0,
			); err != nil {
				return fmt.Errorf("update task usage counters failed: %w", err)
			}
		}

		other := taskBillingOther(&persisted)
		other["task_id"] = persisted.TaskID
		for key, value := range input.extraOther {
			other[key] = value
		}
		payload, err := buildAsyncTaskAccountingLogPayload(&persisted, input, other)
		if err != nil {
			return err
		}
		if err := persistAsyncTaskAccountingResultTx(tx, &persisted, input); err != nil {
			return err
		}
		if err := model.MarkTaskSettlementApplicationAppliedTx(
			tx,
			persisted.ID,
			payload,
			taskSettlementAppliedDetails(input.operation, input.actualQuota, input.preConsumedQuota, input.quotaDelta, input.logType),
		); err != nil {
			return fmt.Errorf("mark async task accounting applied: %w", err)
		}
		persistedAfter = persisted
		return nil
	})
	if err != nil {
		return err
	}

	*task = persistedAfter
	model.RefreshTokenQuotaCache(task.PrivateData.TokenId, "")
	applyTaskWalletQuotaCacheDelta(task, input.quotaDelta)
	if err := DispatchPendingTaskSettlementLogs(ctx, 10); err != nil {
		common.SysLog(fmt.Sprintf("dispatch async task accounting log failed, taskId=%s: %s", task.TaskID, err.Error()))
	}
	return nil
}

func finalizeLockedAsyncTaskAccountingTx(tx *gorm.DB, task *model.Task, record *model.TaskSettlementRecord, input asyncTaskAccountingApply) error {
	if input.operation == taskSettlementOperationRefund {
		if err := validateAppliedTaskRefundDetails(task, record); err != nil {
			return err
		}
		refundInput := input
		refundInput.actualQuota = 0
		return persistAsyncTaskAccountingResultTx(tx, task, refundInput)
	}
	if record.Operation != "" && record.Operation != taskSettlementOperationRecalculation {
		return fmt.Errorf("applied task settlement operation is %s, cannot finalize recalculation", record.Operation)
	}
	if record.AppliedQuota == nil {
		taskID := ""
		if task != nil {
			taskID = task.TaskID
		}
		return fmt.Errorf("applied task recalculation record for task %s has no applied quota evidence", taskID)
	}
	settledInput := input
	settledInput.actualQuota = *record.AppliedQuota
	return persistAsyncTaskAccountingResultTx(tx, task, settledInput)
}

func persistAsyncTaskAccountingResultTx(tx *gorm.DB, task *model.Task, input asyncTaskAccountingApply) error {
	if tx == nil || task == nil || task.ID <= 0 {
		return errors.New("async task accounting persist requires a locked task")
	}
	privateData := task.PrivateData
	privateData.SettlementAttemptQuota = 0
	privateData.SettlementError = ""
	failReason := clearSettlementReviewFailReason(task.FailReason)
	settlementStatus := task.SettlementStatus
	switch input.operation {
	case taskSettlementOperationRefund:
		if settlementStatus == model.TaskSettlementStatusReview {
			settlementStatus = ""
		}
	default:
		settlementStatus = model.TaskSettlementStatusSettled
	}
	updatedAt := time.Now().Unix()
	if updatedAt <= task.UpdatedAt {
		updatedAt = task.UpdatedAt + 1
	}
	result := tx.Model(&model.Task{}).Where("id = ?", task.ID).Updates(map[string]any{
		"quota":             input.actualQuota,
		"fail_reason":       failReason,
		"private_data":      privateData,
		"settlement_status": settlementStatus,
		"refund_pending":    false,
		"next_poll_at":      int64(0),
		"updated_at":        updatedAt,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("async task accounting task update lost CAS, taskId=%s", task.TaskID)
	}
	task.Quota = input.actualQuota
	task.FailReason = failReason
	task.PrivateData = privateData
	task.SettlementStatus = settlementStatus
	task.RefundPending = false
	task.NextPollAt = 0
	task.UpdatedAt = updatedAt
	return nil
}

func buildAsyncTaskAccountingLogPayload(task *model.Task, input asyncTaskAccountingApply, other map[string]interface{}) (string, error) {
	if task == nil {
		return "", errors.New("task is required")
	}
	content := strings.TrimSpace(input.logContent)
	if content == "" && input.logType == model.LogTypeRefund {
		content = fmt.Sprintf("异步任务执行失败，退还额度 %s", logger.LogQuota(input.logQuota))
	}
	if content == "" {
		content = fmt.Sprintf("异步任务计费调整，额度 %s", logger.LogQuota(input.logQuota))
	}
	payload := imageTaskSettlementLogPayload{
		UserID:            task.UserId,
		LogType:           input.logType,
		Content:           content,
		ChannelID:         task.ChannelId,
		ModelName:         taskModelName(task),
		Quota:             input.logQuota,
		TokenID:           task.PrivateData.TokenId,
		Group:             task.Group,
		RequestID:         task.TaskID,
		Other:             other,
		CreatedAt:         time.Now().Unix(),
		NodeName:          task.PrivateData.NodeName,
		QuotaDataCount:    0,
		QuotaDataCaptured: true,
		SkipQuotaData:     input.skipQuotaData,
	}
	encoded, err := common.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
