package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

func midjourneyIsSubscription(task *model.Midjourney) bool {
	return task != nil && task.BillingSource == BillingSourceSubscription && task.SubscriptionId > 0
}

func adjustMidjourneyFunding(task *model.Midjourney, delta int) error {
	if task == nil || delta == 0 {
		return nil
	}
	if midjourneyIsSubscription(task) {
		return model.PostConsumeUserSubscriptionDelta(task.SubscriptionId, int64(delta))
	}
	if delta > 0 {
		return model.DecreaseUserQuota(task.UserId, delta, false)
	}
	return model.IncreaseUserQuota(task.UserId, -delta, false)
}

func refundMidjourneyTokenQuota(ctx context.Context, task *model.Midjourney, quota int) (bool, taskTokenQuotaSnapshot, error) {
	if task == nil || task.TokenId <= 0 || quota == 0 {
		return false, taskTokenQuotaSnapshot{}, nil
	}
	token, err := model.GetTokenById(task.TokenId)
	key := ""
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("midjourney token key unavailable, continue refund by token id tokenId=%d task=%s: %s", task.TokenId, task.MjId, err.Error()))
	} else {
		key = token.Key
	}
	tokenDelta, err := model.IncreaseTokenQuotaTracked(task.TokenId, key, quota)
	if err != nil {
		if model.IsTokenQuotaNoRowsError(err) {
			logger.LogWarn(ctx, fmt.Sprintf("skip midjourney token quota refund because token no longer exists userId=%d tokenId=%d quota=%d: %s", task.UserId, task.TokenId, quota, err.Error()))
			return false, taskTokenQuotaSnapshot{}, nil
		}
		return false, taskTokenQuotaSnapshot{}, err
	}
	return true, taskTokenQuotaSnapshot{
		valid:       true,
		tokenId:     tokenDelta.TokenId,
		key:         tokenDelta.Key,
		remainQuota: tokenDelta.RemainDelta,
		usedQuota:   tokenDelta.UsedDelta,
	}, nil
}

func rollbackMidjourneyTokenRefund(ctx context.Context, task *model.Midjourney, snapshot taskTokenQuotaSnapshot, quota int) error {
	if task == nil || task.TokenId <= 0 || quota == 0 {
		return nil
	}
	if snapshot.valid {
		return model.ApplyTokenQuotaDelta(model.TokenQuotaDelta{
			TokenId:     snapshot.tokenId,
			Key:         snapshot.key,
			RemainDelta: -snapshot.remainQuota,
			UsedDelta:   -snapshot.usedQuota,
		})
	}
	if err := model.DecreaseTokenQuota(task.TokenId, "", quota); err != nil {
		if model.IsTokenQuotaNoRowsError(err) {
			logger.LogWarn(ctx, fmt.Sprintf("skip midjourney token refund rollback because token no longer exists userId=%d tokenId=%d quota=%d: %s", task.UserId, task.TokenId, quota, err.Error()))
			return nil
		}
		return err
	}
	return nil
}

func updateMidjourneyUsageCounters(task *model.Midjourney, quotaDelta int, includeTokenUsage bool, allowMissingChannelRefund ...bool) error {
	if task == nil || quotaDelta == 0 {
		return nil
	}
	allowMissingChannel := len(allowMissingChannelRefund) > 0 && allowMissingChannelRefund[0] && quotaDelta < 0
	if includeTokenUsage {
		var err error
		if allowMissingChannel {
			err = model.UpdateTaskUsageAdjustmentWithTokenAtSyncAllowMissingChannelRefund(task.UserId, task.ChannelId, task.TokenId, quotaDelta, midjourneyUsageTimestamp(task))
		} else {
			err = model.UpdateTaskUsageAdjustmentWithTokenAtSync(task.UserId, task.ChannelId, task.TokenId, quotaDelta, midjourneyUsageTimestamp(task))
		}
		if err != nil {
			return fmt.Errorf("update midjourney usage counters failed: %w", err)
		}
		return nil
	}
	var err error
	if allowMissingChannel {
		err = model.UpdateUserAndChannelUsedQuotaAllowMissingChannelRefundSync(task.UserId, task.ChannelId, quotaDelta)
	} else {
		err = model.UpdateUserAndChannelUsedQuotaSync(task.UserId, task.ChannelId, quotaDelta)
	}
	if err != nil {
		return fmt.Errorf("update midjourney usage counters failed: %w", err)
	}
	return nil
}

func midjourneyUsageTimestamp(task *model.Midjourney) int64 {
	if task != nil && task.SubmitTime > 0 {
		if task.SubmitTime > 9999999999 {
			return task.SubmitTime / 1000
		}
		return task.SubmitTime
	}
	return common.GetTimestamp()
}

func midjourneyTokenUsageDate(timestamp int64) string {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	return time.Unix(timestamp, 0).In(loc).Format("2006-01-02")
}

func markMidjourneyBillingReview(ctx context.Context, task *model.Midjourney, accountingErr error) {
	if task == nil || accountingErr == nil {
		return
	}
	msg := strings.ReplaceAll(accountingErr.Error(), "\n", " ")
	failReason := strings.TrimSpace(task.FailReason)
	if failReason == "" {
		failReason = TaskSettlementReviewFailReason
	} else if !strings.Contains(failReason, TaskSettlementReviewFailReason) {
		failReason += "; " + TaskSettlementReviewFailReason
	}
	failReason += ": " + msg
	task.FailReason = failReason
	task.SettlementStatus = model.TaskSettlementStatusReview
	if task.Id > 0 {
		if err := updateMidjourneyBillingState(task); err != nil {
			logger.LogError(ctx, fmt.Sprintf("mark midjourney billing review failed task %s: %s", task.MjId, err.Error()))
		}
	}
}

func MarkMidjourneyBillingReview(ctx context.Context, task *model.Midjourney, accountingErr error) {
	markMidjourneyBillingReview(ctx, task, accountingErr)
}

func ClearMidjourneyQuotaAfterBillingRollback(ctx context.Context, task *model.Midjourney, accountingErr error) {
	if task == nil {
		return
	}
	if accountingErr != nil {
		msg := strings.ReplaceAll(accountingErr.Error(), "\n", " ")
		failReason := strings.TrimSpace(task.FailReason)
		if failReason == "" {
			failReason = TaskSettlementReviewFailReason
		} else if !strings.Contains(failReason, TaskSettlementReviewFailReason) {
			failReason += "; " + TaskSettlementReviewFailReason
		}
		if msg != "" && !strings.Contains(failReason, msg) {
			failReason += ": " + msg
		}
		task.FailReason = failReason
	}
	task.Quota = 0
	task.SettlementStatus = model.TaskSettlementStatusReview
	if err := updateMidjourneyBillingState(task); err != nil {
		logger.LogError(ctx, fmt.Sprintf("clear midjourney quota after billing rollback failed task %s: %s", task.MjId, err.Error()))
	}
}

func clearMidjourneySettlementReview(task *model.Midjourney) bool {
	if task == nil || task.SettlementStatus != model.TaskSettlementStatusReview {
		return false
	}
	task.SettlementStatus = ""
	task.FailReason = clearSettlementReviewFailReason(task.FailReason)
	return true
}

func updateMidjourneyBillingState(task *model.Midjourney) error {
	if task == nil || task.Id <= 0 {
		return nil
	}
	result := model.DB.Model(&model.Midjourney{}).
		Where("id = ?", task.Id).
		Updates(map[string]any{
			"quota":             task.Quota,
			"fail_reason":       task.FailReason,
			"settlement_status": task.SettlementStatus,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var count int64
		if err := model.DB.Model(&model.Midjourney{}).Where("id = ?", task.Id).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("update midjourney billing state failed task %s id=%d", task.MjId, task.Id)
		}
	}
	return nil
}

const midjourneySettlementOperationRefund = "refund"

func midjourneySettlementAppliedDetails(operation string, appliedQuota int, preConsumedQuota int, quotaDelta int, logType int) model.TaskSettlementApplicationAppliedDetails {
	return model.TaskSettlementApplicationAppliedDetails{
		Operation:        operation,
		AppliedQuota:     common.GetPointer(appliedQuota),
		PreConsumedQuota: common.GetPointer(preConsumedQuota),
		QuotaDelta:       common.GetPointer(quotaDelta),
		LogType:          common.GetPointer(logType),
	}
}

func validateAppliedMidjourneyRefundRecord(record *model.MidjourneySettlementRecord) error {
	if record != nil && record.Operation != "" && record.Operation != midjourneySettlementOperationRefund {
		return fmt.Errorf("applied midjourney settlement operation is %s, cannot finalize refund", record.Operation)
	}
	return nil
}

func finalizeAppliedMidjourneyRefund(task *model.Midjourney) error {
	if task == nil {
		return nil
	}
	task.Quota = 0
	if !clearMidjourneySettlementReview(task) {
		task.SettlementStatus = ""
	}
	return updateMidjourneyBillingState(task)
}

func markMidjourneyAccountingRecordReview(ctx context.Context, task *model.Midjourney, operation string, accountingErr error) {
	if task == nil || task.Id <= 0 || accountingErr == nil {
		return
	}
	message := fmt.Sprintf("%s accounting requires manual review: %s", operation, strings.ReplaceAll(accountingErr.Error(), "\n", " "))
	if err := model.MarkMidjourneySettlementApplicationReview(task.Id, message); err != nil {
		logger.LogError(ctx, fmt.Sprintf("mark midjourney accounting record review failed task %s: %s", task.MjId, err.Error()))
	}
}

func midjourneyAccountingReviewError(operation string, record *model.MidjourneySettlementRecord) error {
	msg := ""
	if record != nil {
		msg = strings.TrimSpace(record.Error)
	}
	if msg == "" {
		msg = "manual review is required"
	}
	return fmt.Errorf("%s accounting requires manual review: %s", operation, msg)
}

func beginMidjourneyAccountingApplication(task *model.Midjourney, operation string) (*model.MidjourneySettlementRecord, bool, error) {
	record, shouldApply, err := model.BeginMidjourneySettlementApplication(task, operation)
	if err != nil {
		return nil, false, err
	}
	if !shouldApply {
		return record, false, nil
	}
	if err := model.MarkMidjourneySettlementApplicationApplying(task.Id); err != nil {
		latestRecord, exists, loadErr := model.GetMidjourneySettlementRecord(task.Id)
		if loadErr == nil && exists && latestRecord.Status != model.TaskSettlementRecordStatusPrepared {
			return latestRecord, false, nil
		}
		if loadErr != nil {
			return record, false, fmt.Errorf("load %s accounting after applying CAS failure: %w", operation, loadErr)
		}
		return record, false, fmt.Errorf("mark %s accounting applying: %w", operation, err)
	}
	return record, true, nil
}

func RefundMidjourneyTaskQuota(ctx context.Context, task *model.Midjourney, reason string) error {
	if task == nil {
		return nil
	}
	quota := task.Quota
	if quota == 0 {
		record, exists, err := model.GetMidjourneySettlementRecord(task.Id)
		if err != nil {
			return err
		}
		if exists && record.Status == model.TaskSettlementRecordStatusApplied {
			if err := validateAppliedMidjourneyRefundRecord(record); err != nil {
				reviewErr := fmt.Errorf("finalize applied midjourney refund failed: %w", err)
				markMidjourneyBillingReview(ctx, task, reviewErr)
				return reviewErr
			}
			return finalizeAppliedMidjourneyRefund(task)
		}
		return nil
	}
	record, shouldApply, err := beginMidjourneyAccountingApplication(task, midjourneySettlementOperationRefund)
	if err != nil {
		reviewErr := fmt.Errorf("begin midjourney refund accounting failed: %w", err)
		markMidjourneyBillingReview(ctx, task, reviewErr)
		return reviewErr
	}
	if !shouldApply {
		switch record.Status {
		case model.TaskSettlementRecordStatusApplied:
			if err := validateAppliedMidjourneyRefundRecord(record); err != nil {
				reviewErr := fmt.Errorf("finalize applied midjourney refund failed: %w", err)
				markMidjourneyBillingReview(ctx, task, reviewErr)
				return reviewErr
			}
			if err := finalizeAppliedMidjourneyRefund(task); err != nil {
				reviewErr := fmt.Errorf("finalize applied midjourney refund failed: %w", err)
				markMidjourneyBillingReview(ctx, task, reviewErr)
				return reviewErr
			}
			return nil
		case model.TaskSettlementRecordStatusReview:
			reviewErr := midjourneyAccountingReviewError("midjourney refund", record)
			markMidjourneyBillingReview(ctx, task, reviewErr)
			return reviewErr
		case model.TaskSettlementRecordStatusApplying:
			return nil
		default:
			return fmt.Errorf("midjourney refund accounting has unexpected record status %s for task %s", record.Status, task.MjId)
		}
	}

	tokenAdjusted, tokenSnapshot, err := refundMidjourneyTokenQuota(ctx, task, quota)
	if err != nil {
		reviewErr := fmt.Errorf("refund midjourney token quota failed: %w", err)
		markMidjourneyAccountingRecordReview(ctx, task, "midjourney refund", reviewErr)
		markMidjourneyBillingReview(ctx, task, reviewErr)
		return reviewErr
	}
	if err := adjustMidjourneyFunding(task, -quota); err != nil {
		rollbackErrs := []string{}
		if tokenAdjusted {
			if rollbackErr := rollbackMidjourneyTokenRefund(ctx, task, tokenSnapshot, quota); rollbackErr != nil {
				rollbackErrs = append(rollbackErrs, rollbackErr.Error())
			}
		}
		reviewErr := taskAccountingRollbackError(fmt.Errorf("refund midjourney funding failed: %w", err), rollbackErrs)
		markMidjourneyAccountingRecordReview(ctx, task, "midjourney refund", reviewErr)
		markMidjourneyBillingReview(ctx, task, reviewErr)
		return reviewErr
	}
	if err := updateMidjourneyUsageCounters(task, -quota, tokenAdjusted, true); err != nil {
		rollbackErrs := []string{}
		if fundingErr := adjustMidjourneyFunding(task, quota); fundingErr != nil {
			rollbackErrs = append(rollbackErrs, fundingErr.Error())
		}
		if tokenAdjusted {
			if tokenErr := rollbackMidjourneyTokenRefund(ctx, task, tokenSnapshot, quota); tokenErr != nil {
				rollbackErrs = append(rollbackErrs, tokenErr.Error())
			}
		}
		reviewErr := taskAccountingRollbackError(err, rollbackErrs)
		markMidjourneyAccountingRecordReview(ctx, task, "midjourney refund", reviewErr)
		markMidjourneyBillingReview(ctx, task, reviewErr)
		return reviewErr
	}
	other := map[string]interface{}{
		"task_id":            task.MjId,
		"reason":             reason,
		"pre_consumed_quota": quota,
		"actual_quota":       0,
	}
	if task.BillingSource != "" {
		other["billing_source"] = task.BillingSource
	}
	if task.SubscriptionId > 0 {
		other["subscription_id"] = task.SubscriptionId
	}
	if err := model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:    task.UserId,
		LogType:   model.LogTypeRefund,
		Content:   "",
		ChannelId: task.ChannelId,
		ModelName: CovertMjpActionToModelName(task.Action),
		Quota:     quota,
		TokenId:   task.TokenId,
		Group:     task.Group,
		Other:     other,
	}); err != nil {
		rollbackErrs := []string{}
		if usageErr := updateMidjourneyUsageCounters(task, quota, tokenAdjusted); usageErr != nil {
			rollbackErrs = append(rollbackErrs, usageErr.Error())
		}
		if fundingErr := adjustMidjourneyFunding(task, quota); fundingErr != nil {
			rollbackErrs = append(rollbackErrs, fundingErr.Error())
		}
		if tokenAdjusted {
			if tokenErr := rollbackMidjourneyTokenRefund(ctx, task, tokenSnapshot, quota); tokenErr != nil {
				rollbackErrs = append(rollbackErrs, tokenErr.Error())
			}
		}
		reviewErr := taskAccountingRollbackError(fmt.Errorf("record midjourney refund log failed: %w", err), rollbackErrs)
		markMidjourneyAccountingRecordReview(ctx, task, "midjourney refund", reviewErr)
		markMidjourneyBillingReview(ctx, task, reviewErr)
		return reviewErr
	}
	if err := model.MarkMidjourneySettlementApplicationApplied(
		task.Id,
		midjourneySettlementAppliedDetails(midjourneySettlementOperationRefund, 0, quota, -quota, model.LogTypeRefund),
	); err != nil {
		reviewErr := fmt.Errorf("mark midjourney refund accounting applied failed: %w", err)
		markMidjourneyAccountingRecordReview(ctx, task, "midjourney refund", reviewErr)
		markMidjourneyBillingReview(ctx, task, reviewErr)
		return reviewErr
	}
	if err := finalizeAppliedMidjourneyRefund(task); err != nil {
		reviewErr := fmt.Errorf("finalize applied midjourney refund failed: %w", err)
		markMidjourneyBillingReview(ctx, task, reviewErr)
		return reviewErr
	}
	return nil
}
