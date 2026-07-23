package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const (
	taskSettlementOperationRecalculation = "recalculation"
	taskSettlementOperationRefund        = "refund"
)

// LogTaskConsumption records task usage logs and usage counters.
// Actual billing is handled by BillingSession before this point.
func LogTaskConsumption(c *gin.Context, info *relaycommon.RelayInfo) error {
	tokenName := c.GetString("token_name")
	logContent := fmt.Sprintf("操作 %s", info.Action)
	// 支持任务仅按次计费
	if common.StringsContains(constant.TaskPricePatches, info.OriginModelName) {
		logContent = fmt.Sprintf("%s，按次计费", logContent)
	} else {
		if otherRatios := info.PriceData.OtherRatios(); len(otherRatios) > 0 {
			var contents []string
			for key, ra := range otherRatios {
				if 1.0 != ra {
					contents = append(contents, fmt.Sprintf("%s: %.2f", key, ra))
				}
			}
			if len(contents) > 0 {
				logContent = fmt.Sprintf("%s, 计算参数：%s", logContent, strings.Join(contents, ", "))
			}
		}
	}
	other := make(map[string]interface{})
	other["is_task"] = true
	other["request_path"] = c.Request.URL.Path
	other["model_price"] = info.PriceData.ModelPrice
	if info.PriceData.ModelRatio > 0 {
		other["model_ratio"] = info.PriceData.ModelRatio
	}
	other["group_ratio"] = info.PriceData.GroupRatioInfo.GroupRatio
	if info.PriceData.GroupRatioInfo.HasSpecialRatio {
		other["user_group_ratio"] = info.PriceData.GroupRatioInfo.GroupSpecialRatio
	}
	if info.IsModelMapped {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = info.UpstreamModelName
	}
	if info.TaskRelayInfo != nil && info.TaskRelayInfo.PublicTaskID != "" {
		other["task_id"] = info.TaskRelayInfo.PublicTaskID
	}
	settlementErr := c.GetString(contextKeySettlementError)
	logQuota := attachSettlementLogFieldsMessage(other, info, info.PriceData.Quota, settlementErr)
	attachQuotaSaturation(c, info, other)
	settlementSucceeded := c.GetBool(contextKeySettlementApplied)
	if logQuota > 0 {
		if err := model.UpdateTaskConsumptionUsageWithTokenSync(info.UserId, info.ChannelId, info.TokenId, logQuota); err != nil {
			if settlementSucceeded {
				if rollbackErr := RollbackBillingSettlement(c, info, logQuota); rollbackErr != nil {
					return fmt.Errorf("log task consumption usage counter update failed: %w; rollback billing failed: %v", err, rollbackErr)
				}
			}
			return fmt.Errorf("log task consumption usage counter update failed: %w", err)
		}
	}
	if err := model.RecordConsumeLog(c, info.UserId, model.RecordConsumeLogParams{
		ChannelId: info.ChannelId,
		ModelName: info.OriginModelName,
		TokenName: tokenName,
		Quota:     logQuota,
		Content:   logContent,
		TokenId:   info.TokenId,
		Group:     info.UsingGroup,
		Other:     other,
	}); err != nil {
		rollbackErrs := []string{}
		if logQuota > 0 {
			if rollbackErr := RollbackTaskConsumptionUsage(info.UserId, info.ChannelId, info.TokenId, logQuota); rollbackErr != nil {
				rollbackErrs = append(rollbackErrs, rollbackErr.Error())
			}
		}
		if settlementSucceeded {
			if rollbackErr := RollbackBillingSettlement(c, info, logQuota); rollbackErr != nil {
				rollbackErrs = append(rollbackErrs, rollbackErr.Error())
			}
		}
		if len(rollbackErrs) > 0 {
			return fmt.Errorf("record consume log failed: %w; rollback errors: %s", err, strings.Join(rollbackErrs, "; "))
		}
		return fmt.Errorf("record consume log failed: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// 异步任务计费辅助函数
// ---------------------------------------------------------------------------

// resolveTokenKey gets the token key by id for cache refresh operations.
func resolveTokenKey(ctx context.Context, tokenId int, taskID string) string {
	token, err := model.GetTokenById(tokenId)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("获取令牌 key 失败 (tokenId=%d, task=%s): %s", tokenId, taskID, err.Error()))
		return ""
	}
	return token.Key
}

// taskIsSubscription reports whether the task is billed through subscription quota.
func taskIsSubscription(task *model.Task) bool {
	return task.PrivateData.BillingSource == BillingSourceSubscription && task.PrivateData.SubscriptionId > 0
}

// taskAdjustFunding adjusts the task funding source. Positive delta charges, negative delta refunds.
func taskAdjustFunding(task *model.Task, delta int) error {
	if taskIsSubscription(task) {
		return model.PostConsumeUserSubscriptionDelta(task.PrivateData.SubscriptionId, int64(delta))
	}
	if delta > 0 {
		return model.DecreaseUserQuota(task.UserId, delta, false)
	}
	return model.IncreaseUserQuota(task.UserId, -delta, false)
}

// taskAdjustTokenQuota adjusts token quota. Positive delta charges, negative delta refunds.
func taskAdjustTokenQuota(ctx context.Context, task *model.Task, delta int) (bool, error) {
	if task.PrivateData.TokenId <= 0 || delta == 0 {
		return false, nil
	}
	tokenKey := resolveTokenKey(ctx, task.PrivateData.TokenId, task.TaskID)
	if tokenKey == "" {
		logger.LogWarn(ctx, fmt.Sprintf("token key unavailable, continue quota adjustment by token id (tokenId=%d, task=%s)", task.PrivateData.TokenId, task.TaskID))
	}
	var err error
	if delta > 0 {
		err = model.DecreaseTokenQuota(task.PrivateData.TokenId, tokenKey, delta)
	} else {
		err = model.IncreaseTokenQuota(task.PrivateData.TokenId, tokenKey, -delta)
	}
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("adjust token quota failed (delta=%d, task=%s): %s", delta, task.TaskID, err.Error()))
		return false, err
	}
	return true, nil
}

func taskAdjustRefundTokenQuota(ctx context.Context, task *model.Task, delta int) (bool, taskTokenQuotaSnapshot, error) {
	if task.PrivateData.TokenId <= 0 || delta == 0 {
		return false, taskTokenQuotaSnapshot{}, nil
	}
	if delta >= 0 {
		tokenAdjusted, err := taskAdjustTokenQuota(ctx, task, delta)
		return tokenAdjusted, taskTokenQuotaSnapshot{}, err
	}
	tokenKey := resolveTokenKey(ctx, task.PrivateData.TokenId, task.TaskID)
	if tokenKey == "" {
		logger.LogWarn(ctx, fmt.Sprintf("token key unavailable, continue quota adjustment by token id (tokenId=%d, task=%s)", task.PrivateData.TokenId, task.TaskID))
	}
	tokenDelta, err := model.IncreaseTokenQuotaTracked(task.PrivateData.TokenId, tokenKey, -delta)
	if err != nil && delta < 0 && model.IsTokenQuotaNoRowsError(err) {
		logger.LogWarn(ctx, fmt.Sprintf("skip token quota refund because token no longer exists (task=%s, tokenId=%d): %s", task.TaskID, task.PrivateData.TokenId, err.Error()))
		return false, taskTokenQuotaSnapshot{}, nil
	}
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("adjust token quota failed (delta=%d, task=%s): %s", delta, task.TaskID, err.Error()))
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

type taskTokenQuotaSnapshot struct {
	valid       bool
	tokenId     int
	key         string
	remainQuota int
	usedQuota   int
}

func rollbackRefundedTaskTokenQuota(ctx context.Context, task *model.Task, snapshot taskTokenQuotaSnapshot, fallbackQuota int) error {
	if snapshot.valid {
		return model.ApplyTokenQuotaDelta(model.TokenQuotaDelta{
			TokenId:     snapshot.tokenId,
			Key:         snapshot.key,
			RemainDelta: -snapshot.remainQuota,
			UsedDelta:   -snapshot.usedQuota,
		})
	}
	_, err := taskAdjustTokenQuota(ctx, task, fallbackQuota)
	return err
}

// taskBillingOther builds the usage log Other field from task billing context.
func taskBillingOther(task *model.Task) map[string]interface{} {
	other := make(map[string]interface{})
	if bc := task.PrivateData.BillingContext; bc != nil {
		other["model_price"] = bc.ModelPrice
		if bc.ModelRatio > 0 {
			other["model_ratio"] = bc.ModelRatio
		}
		other["group_ratio"] = bc.GroupRatio
		if bc.GroupHasSpecialRatio {
			other["user_group_ratio"] = bc.GroupSpecialRatio
		}
		if priceData := taskBillingContextPriceData(bc); priceData != nil {
			for k, v := range priceData.OtherRatios() {
				other[k] = v
			}
		}
	}
	props := task.Properties
	if props.UpstreamModelName != "" && props.UpstreamModelName != props.OriginModelName {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = props.UpstreamModelName
	}
	return other
}

func taskBillingContextPriceData(bc *model.TaskBillingContext) *types.PriceData {
	if bc == nil || len(bc.OtherRatios) == 0 {
		return nil
	}
	priceData := &types.PriceData{}
	if !priceData.ReplaceOtherRatios(bc.OtherRatios) {
		return nil
	}
	return priceData
}

// taskModelName gets the billing model name from BillingContext or Properties.
func taskModelName(task *model.Task) string {
	if bc := task.PrivateData.BillingContext; bc != nil && bc.OriginModelName != "" {
		return bc.OriginModelName
	}
	return task.Properties.OriginModelName
}

// TaskBillingModelRatio returns the model ratio captured at submission time
// when available. Older tasks without a billing snapshot fall back to current
// model settings.
func TaskBillingModelRatio(task *model.Task, fallbackModelNames ...string) (float64, bool) {
	if task == nil {
		return 0, false
	}
	if bc := task.PrivateData.BillingContext; bc != nil {
		if bc.PerCallBilling {
			return 0, true
		}
		if bc.GroupRatioCaptured || bc.ModelRatio > 0 {
			return bc.ModelRatio, true
		}
	}

	modelName := strings.TrimSpace(taskModelName(task))
	if modelName == "" {
		for _, fallback := range fallbackModelNames {
			modelName = strings.TrimSpace(fallback)
			if modelName != "" {
				break
			}
		}
	}
	if modelName == "" {
		return 0, false
	}
	modelRatio, hasRatioSetting, _ := ratio_setting.GetModelRatio(modelName)
	return modelRatio, hasRatioSetting
}

// TaskBillingGroupRatio returns the final group ratio captured at submission
// time when available. Older tasks without a snapshot fall back to current
// user-group and using-group settings.
func TaskBillingGroupRatio(task *model.Task) (float64, bool) {
	if task == nil {
		return 0, false
	}
	if bc := task.PrivateData.BillingContext; bc != nil {
		if bc.GroupHasSpecialRatio {
			return bc.GroupSpecialRatio, true
		}
		if bc.GroupRatioCaptured || bc.GroupRatio > 0 {
			return bc.GroupRatio, true
		}
	}

	usingGroup := strings.TrimSpace(task.Group)
	userGroup := ""
	if task.UserId > 0 {
		if user, err := model.GetUserById(task.UserId, false); err == nil && user != nil {
			userGroup = strings.TrimSpace(user.Group)
			if usingGroup == "" {
				usingGroup = userGroup
			}
		}
	}
	if usingGroup == "" {
		return 0, false
	}
	if userGroup != "" {
		if ratio, ok := ratio_setting.GetGroupGroupRatio(userGroup, usingGroup); ok {
			return ratio, true
		}
	}
	if ratio, ok := ratio_setting.GetGroupGroupRatio(usingGroup, usingGroup); ok {
		return ratio, true
	}
	return ratio_setting.GetGroupRatio(usingGroup), true
}

const TaskSettlementReviewFailReason = "billing settlement requires manual review"

func clearSettlementReviewFailReason(failReason string) string {
	failReason = strings.TrimSpace(failReason)
	if failReason == TaskSettlementReviewFailReason {
		return ""
	}
	marker := "; " + TaskSettlementReviewFailReason
	if strings.HasSuffix(failReason, marker) {
		return strings.TrimSpace(strings.TrimSuffix(failReason, marker))
	}
	return failReason
}

func clearTaskSettlementReview(task *model.Task) bool {
	if task == nil || task.SettlementStatus != model.TaskSettlementStatusReview {
		return false
	}
	task.SettlementStatus = ""
	task.FailReason = clearSettlementReviewFailReason(task.FailReason)
	task.PrivateData.SettlementAttemptQuota = 0
	task.PrivateData.SettlementError = ""
	return true
}

func MarkTaskSettlementReview(ctx context.Context, task *model.Task, attemptedQuota int, settleErr error) {
	if task == nil || settleErr == nil {
		return
	}
	task.PrivateData.SettlementAttemptQuota = attemptedQuota
	task.PrivateData.SettlementError = strings.ReplaceAll(settleErr.Error(), "\n", " ")
	task.FailReason = TaskSettlementReviewFailReason
	task.SettlementStatus = model.TaskSettlementStatusReview
	if err := task.UpdateSubmitSettlementError(); err != nil {
		logger.LogError(ctx, fmt.Sprintf("mark task settlement review failed task %s: %s", task.TaskID, err.Error()))
	}
}

func markTaskRefundReview(ctx context.Context, task *model.Task, attemptedQuota int, refundErr error) {
	if task == nil || refundErr == nil {
		return
	}
	task.PrivateData.SettlementAttemptQuota = attemptedQuota
	task.PrivateData.SettlementError = strings.ReplaceAll(refundErr.Error(), "\n", " ")
	if task.FailReason == "" {
		task.FailReason = TaskSettlementReviewFailReason
	} else if !strings.Contains(task.FailReason, TaskSettlementReviewFailReason) {
		task.FailReason += "; " + TaskSettlementReviewFailReason
	}
	task.SettlementStatus = model.TaskSettlementStatusReview
	if task.ID > 0 {
		if err := task.UpdateSubmitSettlementError(); err != nil {
			logger.LogError(ctx, fmt.Sprintf("mark task refund review failed task %s: %s", task.TaskID, err.Error()))
		}
	}
}

func taskRefundAllowsMissingChannel(reason string) bool {
	return strings.Contains(reason, "获取渠道信息失败") ||
		strings.Contains(reason, "Failed to get channel info")
}

func updateTaskUsageCounters(task *model.Task, quotaDelta int, includeTokenUsage bool, allowMissingChannelRefund ...bool) error {
	if task == nil || quotaDelta == 0 {
		return nil
	}
	allowMissingChannel := len(allowMissingChannelRefund) > 0 && allowMissingChannelRefund[0]
	if includeTokenUsage {
		if quotaDelta < 0 && allowMissingChannel {
			if err := model.UpdateTaskUsageAdjustmentWithTokenSyncAllowMissingChannelRefund(task.UserId, task.ChannelId, task.PrivateData.TokenId, quotaDelta); err != nil {
				return fmt.Errorf("update task usage counters failed: %w", err)
			}
			return nil
		}
		if err := model.UpdateTaskUsageAdjustmentWithTokenSync(task.UserId, task.ChannelId, task.PrivateData.TokenId, quotaDelta); err != nil {
			return fmt.Errorf("update task usage counters failed: %w", err)
		}
		return nil
	}
	if quotaDelta < 0 && allowMissingChannel {
		if err := model.UpdateUserAndChannelUsedQuotaAllowMissingChannelRefundSync(task.UserId, task.ChannelId, quotaDelta); err != nil {
			return fmt.Errorf("update task usage counters failed: %w", err)
		}
		return nil
	}
	if err := model.UpdateUserAndChannelUsedQuotaSync(task.UserId, task.ChannelId, quotaDelta); err != nil {
		return fmt.Errorf("update task usage counters failed: %w", err)
	}
	return nil
}

func taskAccountingRollbackError(primary error, rollbackErrs []string) error {
	if len(rollbackErrs) == 0 {
		return primary
	}
	return fmt.Errorf("%w; rollback errors: %s", primary, strings.Join(rollbackErrs, "; "))
}

func taskAccountingRecordError(operation string, task *model.Task, err error) error {
	if err == nil {
		return nil
	}
	taskID := ""
	if task != nil {
		taskID = task.TaskID
	}
	if taskID == "" {
		return fmt.Errorf("%s accounting failed: %w", operation, err)
	}
	return fmt.Errorf("%s accounting failed for task %s: %w", operation, taskID, err)
}

func taskAccountingReviewError(operation string, record *model.TaskSettlementRecord) error {
	msg := ""
	if record != nil {
		msg = strings.TrimSpace(record.Error)
	}
	if msg == "" {
		msg = "manual review is required"
	}
	return fmt.Errorf("%s accounting requires manual review: %s", operation, msg)
}

func ensureTaskAccountingTarget(task *model.Task, operation string) error {
	if task == nil {
		return nil
	}
	if task.ID <= 0 {
		return fmt.Errorf("%s accounting requires persisted task, taskId=%s, id=%d", operation, task.TaskID, task.ID)
	}
	latest, exists, err := model.GetTaskByID(task.ID)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%s accounting target task is missing, taskId=%s, id=%d", operation, task.TaskID, task.ID)
	}
	if task.TaskID == "" {
		task.TaskID = latest.TaskID
	}
	return nil
}

func beginTaskAccountingApplication(task *model.Task, operation string) (*model.TaskSettlementRecord, bool, error) {
	if err := ensureTaskAccountingTarget(task, operation); err != nil {
		return nil, false, err
	}
	record, shouldApply, err := model.BeginTaskSettlementApplication(task)
	if err != nil {
		return nil, false, err
	}
	if !shouldApply {
		return record, false, nil
	}
	if err := model.MarkTaskSettlementApplicationApplying(task.ID); err != nil {
		return record, false, fmt.Errorf("mark %s accounting applying: %w", operation, err)
	}
	return record, true, nil
}

func markTaskAccountingRecordReview(ctx context.Context, task *model.Task, operation string, accountingErr error) {
	if task == nil || task.ID <= 0 || accountingErr == nil {
		return
	}
	message := fmt.Sprintf("%s accounting requires manual review: %s", operation, strings.ReplaceAll(accountingErr.Error(), "\n", " "))
	if err := model.MarkTaskSettlementApplicationReview(task.ID, message); err != nil {
		logger.LogError(ctx, fmt.Sprintf("mark task accounting record review failed task %s: %s", task.TaskID, err.Error()))
	}
}

func taskSettlementAppliedDetails(operation string, appliedQuota int, preConsumedQuota int, quotaDelta int, logType int) model.TaskSettlementApplicationAppliedDetails {
	return model.TaskSettlementApplicationAppliedDetails{
		Operation:        operation,
		AppliedQuota:     common.GetPointer(appliedQuota),
		PreConsumedQuota: common.GetPointer(preConsumedQuota),
		QuotaDelta:       common.GetPointer(quotaDelta),
		LogType:          common.GetPointer(logType),
	}
}

func finalizeAppliedTaskRefund(task *model.Task) error {
	if task == nil {
		return nil
	}
	task.Quota = 0
	clearTaskSettlementReview(task)
	return task.UpdateQuota()
}

func finalizeAppliedTaskRecalculation(task *model.Task, actualQuota int) error {
	if task == nil {
		return nil
	}
	task.Quota = actualQuota
	clearTaskSettlementReview(task)
	return task.UpdateQuota()
}

func appliedTaskRecalculationQuota(ctx context.Context, task *model.Task, record *model.TaskSettlementRecord) (int, error) {
	if record != nil {
		if record.Operation != "" && record.Operation != taskSettlementOperationRecalculation {
			return 0, fmt.Errorf("applied task settlement operation is %s, cannot finalize recalculation", record.Operation)
		}
		if record.AppliedQuota != nil {
			return *record.AppliedQuota, nil
		}
	}

	details, found, err := legacyAppliedTaskRecalculationDetailsFromBillingLog(task)
	if err != nil {
		return 0, err
	}
	if !found || details.AppliedQuota == nil {
		taskID := ""
		if task != nil {
			taskID = task.TaskID
		}
		return 0, fmt.Errorf("applied task recalculation record for task %s has no applied quota evidence", taskID)
	}
	if task != nil && task.ID > 0 {
		if err := model.BackfillTaskSettlementApplicationAppliedDetails(task.ID, details); err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("backfill applied task recalculation details failed task %s: %s", task.TaskID, err.Error()))
		}
	}
	return *details.AppliedQuota, nil
}

func legacyAppliedTaskRecalculationDetailsFromBillingLog(task *model.Task) (model.TaskSettlementApplicationAppliedDetails, bool, error) {
	if task == nil || strings.TrimSpace(task.TaskID) == "" || model.LOG_DB == nil {
		return model.TaskSettlementApplicationAppliedDetails{}, false, nil
	}
	quotedTaskID, err := json.Marshal(task.TaskID)
	if err != nil {
		return model.TaskSettlementApplicationAppliedDetails{}, false, err
	}
	pattern := fmt.Sprintf("%%\"task_id\":%s%%", string(quotedTaskID))
	var logs []model.Log
	if err := model.LOG_DB.Model(&model.Log{}).
		Where("type IN ?", []int{model.LogTypeConsume, model.LogTypeRefund}).
		Where("other LIKE ?", pattern).
		Order("created_at desc, id desc").
		Limit(20).
		Find(&logs).Error; err != nil {
		return model.TaskSettlementApplicationAppliedDetails{}, false, err
	}
	for _, log := range logs {
		other, err := common.StrToMap(log.Other)
		if err != nil {
			continue
		}
		if taskID, _ := other["task_id"].(string); taskID != task.TaskID {
			continue
		}
		actualQuota, ok, err := intFromTaskBillingLogOther(other, "actual_quota")
		if err != nil {
			return model.TaskSettlementApplicationAppliedDetails{}, false, err
		}
		if !ok {
			continue
		}
		quotaDelta := log.Quota
		if log.Type == model.LogTypeRefund {
			quotaDelta = -log.Quota
		}
		preConsumedQuota, ok, err := intFromTaskBillingLogOther(other, "pre_consumed_quota")
		if err != nil {
			return model.TaskSettlementApplicationAppliedDetails{}, false, err
		}
		if !ok {
			preConsumedQuota = actualQuota - quotaDelta
		}
		return taskSettlementAppliedDetails(taskSettlementOperationRecalculation, actualQuota, preConsumedQuota, quotaDelta, log.Type), true, nil
	}
	return model.TaskSettlementApplicationAppliedDetails{}, false, nil
}

func intFromTaskBillingLogOther(other map[string]interface{}, key string) (int, bool, error) {
	value, ok := other[key]
	if !ok || value == nil {
		return 0, false, nil
	}
	switch v := value.(type) {
	case int:
		return v, true, nil
	case int64:
		converted, err := checkedIntFromInt64(v)
		return converted, err == nil, err
	case int32:
		return int(v), true, nil
	case float64:
		if math.Trunc(v) != v {
			return 0, false, fmt.Errorf("%s is not an integer: %v", key, v)
		}
		if v > float64(maxIntValue()) || v < float64(minIntValue()) {
			return 0, false, fmt.Errorf("%s is out of int range: %v", key, v)
		}
		return int(v), true, nil
	case json.Number:
		parsed, err := v.Int64()
		if err != nil {
			return 0, false, err
		}
		converted, err := checkedIntFromInt64(parsed)
		return converted, err == nil, err
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return 0, false, nil
		}
		parsed, err := strconv.ParseInt(text, 10, 0)
		if err != nil {
			return 0, false, err
		}
		converted, err := checkedIntFromInt64(parsed)
		return converted, err == nil, err
	default:
		return 0, false, fmt.Errorf("%s has unsupported type %T", key, value)
	}
}

func checkedIntFromInt64(value int64) (int, error) {
	if value > maxIntValue() || value < minIntValue() {
		return 0, fmt.Errorf("value is out of int range: %d", value)
	}
	return int(value), nil
}

func maxIntValue() int64 {
	return int64(^uint(0) >> 1)
}

func minIntValue() int64 {
	return -maxIntValue() - 1
}

func rollbackTaskSettlementAfterBillingLogFailure(
	ctx context.Context,
	task *model.Task,
	preConsumedQuota int,
	preSettlementStatus string,
	preFailReason string,
	prePrivateData model.TaskPrivateData,
	quotaDelta int,
	tokenAdjusted bool,
	tokenRefundDelta taskTokenQuotaSnapshot,
	attemptedQuota int,
	logErr error,
) error {
	rollbackErrs := []string{}
	if err := updateTaskUsageCounters(task, -quotaDelta, tokenAdjusted); err != nil {
		logger.LogError(ctx, fmt.Sprintf("rollback task usage counters after billing log failure failed task %s: %s", task.TaskID, err.Error()))
		rollbackErrs = append(rollbackErrs, err.Error())
	}
	if err := taskAdjustFunding(task, -quotaDelta); err != nil {
		logger.LogError(ctx, fmt.Sprintf("rollback funding after billing log failure failed task %s: %s", task.TaskID, err.Error()))
		rollbackErrs = append(rollbackErrs, err.Error())
	}
	if tokenAdjusted {
		var err error
		if quotaDelta < 0 {
			err = rollbackRefundedTaskTokenQuota(ctx, task, tokenRefundDelta, -quotaDelta)
		} else {
			_, err = taskAdjustTokenQuota(ctx, task, -quotaDelta)
		}
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("rollback token quota after billing log failure failed task %s: %s", task.TaskID, err.Error()))
			rollbackErrs = append(rollbackErrs, err.Error())
		}
	}

	task.Quota = preConsumedQuota
	task.SettlementStatus = preSettlementStatus
	task.FailReason = preFailReason
	task.PrivateData = prePrivateData

	reviewErr := taskAccountingRollbackError(fmt.Errorf("record task billing log failed: %w", logErr), rollbackErrs)
	MarkTaskSettlementReview(ctx, task, attemptedQuota, reviewErr)
	return reviewErr
}

func rollbackTaskRefundAfterBillingLogFailure(
	ctx context.Context,
	task *model.Task,
	originalQuota int,
	originalFailReason string,
	originalSettlementStatus string,
	originalPrivateData model.TaskPrivateData,
	tokenAdjusted bool,
	tokenSnapshot taskTokenQuotaSnapshot,
	logErr error,
) error {
	rollbackErrs := []string{}
	if err := updateTaskUsageCounters(task, originalQuota, tokenAdjusted); err != nil {
		logger.LogError(ctx, fmt.Sprintf("rollback refund usage counters after billing log failure failed task %s: %s", task.TaskID, err.Error()))
		rollbackErrs = append(rollbackErrs, err.Error())
	}
	if err := taskAdjustFunding(task, originalQuota); err != nil {
		logger.LogError(ctx, fmt.Sprintf("rollback refund funding after billing log failure failed task %s: %s", task.TaskID, err.Error()))
		rollbackErrs = append(rollbackErrs, err.Error())
	}
	if tokenAdjusted {
		if err := rollbackRefundedTaskTokenQuota(ctx, task, tokenSnapshot, originalQuota); err != nil {
			logger.LogError(ctx, fmt.Sprintf("rollback refund token quota after billing log failure failed task %s: %s", task.TaskID, err.Error()))
			rollbackErrs = append(rollbackErrs, err.Error())
		}
	}

	task.Quota = originalQuota
	task.FailReason = originalFailReason
	task.SettlementStatus = originalSettlementStatus
	task.PrivateData = originalPrivateData

	reviewErr := taskAccountingRollbackError(fmt.Errorf("record task billing log failed: %w", logErr), rollbackErrs)
	markTaskRefundReview(ctx, task, originalQuota, reviewErr)
	return reviewErr
}

// RefundTaskQuota refunds pre-consumed quota after an async task fails.
func RefundTaskQuota(ctx context.Context, task *model.Task, reason string) error {
	if task == nil {
		return nil
	}
	quota := task.Quota
	if quota == 0 {
		return nil
	}

	originalQuota := task.Quota
	originalFailReason := task.FailReason
	originalSettlementStatus := task.SettlementStatus
	originalPrivateData := task.PrivateData

	restoreTaskState := func(persist bool) {
		task.Quota = originalQuota
		task.FailReason = originalFailReason
		task.SettlementStatus = originalSettlementStatus
		task.PrivateData = originalPrivateData
		if persist && task.ID > 0 {
			if restoreErr := task.UpdateQuota(); restoreErr != nil {
				logger.LogError(ctx, fmt.Sprintf("restore task quota after refund failure failed task %s: %s", task.TaskID, restoreErr.Error()))
			}
		}
	}

	record, shouldApply, err := beginTaskAccountingApplication(task, "task refund")
	if err != nil {
		err = taskAccountingRecordError("task refund", task, err)
		markTaskRefundReview(ctx, task, originalQuota, err)
		return err
	}
	if !shouldApply {
		switch record.Status {
		case model.TaskSettlementRecordStatusApplied:
			if err := finalizeAppliedTaskRefund(task); err != nil {
				err = taskAccountingRecordError("finalize applied task refund", task, err)
				markTaskRefundReview(ctx, task, originalQuota, err)
				return err
			}
			return nil
		case model.TaskSettlementRecordStatusReview:
			err := taskAccountingReviewError("task refund", record)
			markTaskRefundReview(ctx, task, originalQuota, err)
			return err
		case model.TaskSettlementRecordStatusApplying:
			return fmt.Errorf("task refund accounting is already applying for task %s", task.TaskID)
		default:
			return fmt.Errorf("task refund accounting has unexpected record status %s for task %s", record.Status, task.TaskID)
		}
	}

	taskStatePersisted := false
	tokenAdjusted, tokenSnapshot, err := taskAdjustRefundTokenQuota(ctx, task, -quota)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("refund token quota failed task %s: %s", task.TaskID, err.Error()))
		restoreTaskState(taskStatePersisted)
		reviewErr := fmt.Errorf("refund token quota failed: %w", err)
		markTaskAccountingRecordReview(ctx, task, "task refund", reviewErr)
		markTaskRefundReview(ctx, task, originalQuota, reviewErr)
		return reviewErr
	}
	if err := taskAdjustFunding(task, -quota); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("refund funding failed task %s: %s", task.TaskID, err.Error()))
		if tokenAdjusted {
			if rollbackErr := rollbackRefundedTaskTokenQuota(ctx, task, tokenSnapshot, quota); rollbackErr != nil {
				logger.LogError(ctx, fmt.Sprintf("rollback refunded token quota failed task %s: %s", task.TaskID, rollbackErr.Error()))
			}
		}
		restoreTaskState(taskStatePersisted)
		reviewErr := fmt.Errorf("refund funding failed: %w", err)
		markTaskAccountingRecordReview(ctx, task, "task refund", reviewErr)
		markTaskRefundReview(ctx, task, originalQuota, reviewErr)
		return reviewErr
	}
	if err := updateTaskUsageCounters(task, -quota, tokenAdjusted, taskRefundAllowsMissingChannel(reason)); err != nil {
		logger.LogError(ctx, fmt.Sprintf("refund usage counter update failed task %s: %s", task.TaskID, err.Error()))
		rollbackErr := err
		if fundingRollbackErr := taskAdjustFunding(task, quota); fundingRollbackErr != nil {
			logger.LogError(ctx, fmt.Sprintf("rollback funding after refund usage counter failure failed task %s: %s", task.TaskID, fundingRollbackErr.Error()))
			rollbackErr = fmt.Errorf("%w; rollback funding failed: %v", rollbackErr, fundingRollbackErr)
		}
		if tokenAdjusted {
			if tokenRollbackErr := rollbackRefundedTaskTokenQuota(ctx, task, tokenSnapshot, quota); tokenRollbackErr != nil {
				logger.LogError(ctx, fmt.Sprintf("rollback token quota after refund usage counter failure failed task %s: %s", task.TaskID, tokenRollbackErr.Error()))
				rollbackErr = fmt.Errorf("%w; rollback token quota failed: %v", rollbackErr, tokenRollbackErr)
			}
		}
		restoreTaskState(taskStatePersisted)
		markTaskAccountingRecordReview(ctx, task, "task refund", rollbackErr)
		markTaskRefundReview(ctx, task, originalQuota, rollbackErr)
		return rollbackErr
	}
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["reason"] = reason
	if err := model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:        task.UserId,
		LogType:       model.LogTypeRefund,
		Content:       "",
		ChannelId:     task.ChannelId,
		ModelName:     taskModelName(task),
		Quota:         quota,
		TokenId:       task.PrivateData.TokenId,
		Group:         task.Group,
		Other:         other,
		SkipQuotaData: task.Platform == constant.TaskPlatformImage && strings.TrimSpace(task.PrivateData.UpstreamTaskID) == "",
	}); err != nil {
		logger.LogError(ctx, fmt.Sprintf("record task billing log failed task %s: %s", task.TaskID, err.Error()))
		reviewErr := rollbackTaskRefundAfterBillingLogFailure(ctx, task, originalQuota, originalFailReason, originalSettlementStatus, originalPrivateData, tokenAdjusted, tokenSnapshot, err)
		markTaskAccountingRecordReview(ctx, task, "task refund", reviewErr)
		return reviewErr
	}
	if err := model.MarkTaskSettlementApplicationApplied(
		task.ID,
		taskSettlementAppliedDetails(taskSettlementOperationRefund, 0, originalQuota, -quota, model.LogTypeRefund),
	); err != nil {
		reviewErr := fmt.Errorf("mark task refund accounting applied failed: %w", err)
		markTaskAccountingRecordReview(ctx, task, "task refund", reviewErr)
		markTaskRefundReview(ctx, task, originalQuota, reviewErr)
		return reviewErr
	}
	if err := finalizeAppliedTaskRefund(task); err != nil {
		err = taskAccountingRecordError("finalize applied task refund", task, err)
		markTaskRefundReview(ctx, task, originalQuota, err)
		return err
	}
	return nil
}

// RecalculateTaskQuota settles the async task quota delta against the pre-consumed task quota.
func RecalculateTaskQuota(ctx context.Context, task *model.Task, actualQuota int, reason string, clamps ...*common.QuotaClamp) error {
	if task == nil {
		return nil
	}
	if actualQuota < 0 {
		return fmt.Errorf("actual quota cannot be negative: %d", actualQuota)
	}
	preConsumedQuota := task.Quota
	quotaDelta := actualQuota - preConsumedQuota

	if quotaDelta == 0 {
		logger.LogInfo(ctx, fmt.Sprintf("task %s pre-consumed quota matched actual quota (%s, %s)",
			task.TaskID, logger.LogQuota(actualQuota), reason))
		if clearTaskSettlementReview(task) {
			if err := task.UpdateQuota(); err != nil {
				logger.LogError(ctx, fmt.Sprintf("task quota settlement clear review failed task %s: %s", task.TaskID, err.Error()))
				return err
			}
		}
		return nil
	}

	logger.LogInfo(ctx, fmt.Sprintf("task %s quota settlement delta=%s (actual=%s, pre-consumed=%s, %s)",
		task.TaskID,
		logger.LogQuota(quotaDelta),
		logger.LogQuota(actualQuota),
		logger.LogQuota(preConsumedQuota),
		reason,
	))

	record, shouldApply, err := beginTaskAccountingApplication(task, "task recalculation")
	if err != nil {
		return taskAccountingRecordError("task recalculation", task, err)
	}
	if !shouldApply {
		switch record.Status {
		case model.TaskSettlementRecordStatusApplied:
			appliedQuota, quotaErr := appliedTaskRecalculationQuota(ctx, task, record)
			if quotaErr != nil {
				MarkTaskSettlementReview(ctx, task, actualQuota, quotaErr)
				return taskAccountingRecordError("finalize applied task recalculation", task, quotaErr)
			}
			if err := finalizeAppliedTaskRecalculation(task, appliedQuota); err != nil {
				return taskAccountingRecordError("finalize applied task recalculation", task, err)
			}
			return nil
		case model.TaskSettlementRecordStatusReview:
			return taskAccountingReviewError("task recalculation", record)
		case model.TaskSettlementRecordStatusApplying:
			return fmt.Errorf("task recalculation accounting is already applying for task %s", task.TaskID)
		default:
			return fmt.Errorf("task recalculation accounting has unexpected record status %s for task %s", record.Status, task.TaskID)
		}
	}

	tokenAdjusted := false
	tokenRefundDelta := taskTokenQuotaSnapshot{}
	if quotaDelta < 0 {
		tokenAdjusted, tokenRefundDelta, err = taskAdjustRefundTokenQuota(ctx, task, quotaDelta)
	} else {
		tokenAdjusted, err = taskAdjustTokenQuota(ctx, task, quotaDelta)
	}
	if err != nil {
		reviewErr := fmt.Errorf("task quota settlement token adjustment failed: %w", err)
		logger.LogError(ctx, fmt.Sprintf("task quota settlement token adjustment failed task %s: %s", task.TaskID, err.Error()))
		markTaskAccountingRecordReview(ctx, task, "task recalculation", reviewErr)
		MarkTaskSettlementReview(ctx, task, actualQuota, reviewErr)
		return reviewErr
	}

	if err := taskAdjustFunding(task, quotaDelta); err != nil {
		if tokenAdjusted {
			var rollbackErr error
			if quotaDelta < 0 {
				rollbackErr = rollbackRefundedTaskTokenQuota(ctx, task, tokenRefundDelta, -quotaDelta)
			} else {
				_, rollbackErr = taskAdjustTokenQuota(ctx, task, -quotaDelta)
			}
			if rollbackErr != nil {
				logger.LogError(ctx, fmt.Sprintf("rollback token quota after task settlement failed task %s: %s", task.TaskID, rollbackErr.Error()))
			}
		}
		reviewErr := fmt.Errorf("task quota settlement funding adjustment failed: %w", err)
		logger.LogError(ctx, fmt.Sprintf("task quota settlement funding adjustment failed task %s: %s", task.TaskID, err.Error()))
		markTaskAccountingRecordReview(ctx, task, "task recalculation", reviewErr)
		MarkTaskSettlementReview(ctx, task, actualQuota, reviewErr)
		return reviewErr
	}

	preSettlementStatus := task.SettlementStatus
	preFailReason := task.FailReason
	prePrivateData := task.PrivateData
	if err := updateTaskUsageCounters(task, quotaDelta, tokenAdjusted); err != nil {
		logger.LogError(ctx, fmt.Sprintf("task quota settlement usage counter update failed task %s: %s", task.TaskID, err.Error()))
		rollbackErr := err
		task.Quota = preConsumedQuota
		task.SettlementStatus = preSettlementStatus
		task.FailReason = preFailReason
		task.PrivateData = prePrivateData
		if restoreErr := task.UpdateQuota(); restoreErr != nil {
			logger.LogError(ctx, fmt.Sprintf("rollback task quota after usage counter failure failed task %s: %s", task.TaskID, restoreErr.Error()))
			rollbackErr = fmt.Errorf("%w; rollback task quota failed: %v", rollbackErr, restoreErr)
		}
		if fundingRollbackErr := taskAdjustFunding(task, -quotaDelta); fundingRollbackErr != nil {
			logger.LogError(ctx, fmt.Sprintf("rollback funding after usage counter failure failed task %s: %s", task.TaskID, fundingRollbackErr.Error()))
			rollbackErr = fmt.Errorf("%w; rollback funding failed: %v", rollbackErr, fundingRollbackErr)
		}
		if tokenAdjusted {
			var tokenRollbackErr error
			if quotaDelta < 0 {
				tokenRollbackErr = rollbackRefundedTaskTokenQuota(ctx, task, tokenRefundDelta, -quotaDelta)
			} else {
				_, tokenRollbackErr = taskAdjustTokenQuota(ctx, task, -quotaDelta)
			}
			if tokenRollbackErr != nil {
				logger.LogError(ctx, fmt.Sprintf("rollback token quota after usage counter failure failed task %s: %s", task.TaskID, tokenRollbackErr.Error()))
				rollbackErr = fmt.Errorf("%w; rollback token quota failed: %v", rollbackErr, tokenRollbackErr)
			}
		}
		markTaskAccountingRecordReview(ctx, task, "task recalculation", rollbackErr)
		MarkTaskSettlementReview(ctx, task, actualQuota, rollbackErr)
		return rollbackErr
	}

	var logType int
	var logQuota int
	if quotaDelta > 0 {
		logType = model.LogTypeConsume
		logQuota = quotaDelta
	} else {
		logType = model.LogTypeRefund
		logQuota = -quotaDelta
	}
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["pre_consumed_quota"] = preConsumedQuota
	other["actual_quota"] = actualQuota
	for _, clamp := range clamps {
		attachQuotaSaturationToOther(other, clamp)
	}
	if err := model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:    task.UserId,
		LogType:   logType,
		Content:   reason,
		ChannelId: task.ChannelId,
		ModelName: taskModelName(task),
		Quota:     logQuota,
		TokenId:   task.PrivateData.TokenId,
		Group:     task.Group,
		Other:     other,
		NodeName:  task.PrivateData.NodeName,
	}); err != nil {
		logger.LogError(ctx, fmt.Sprintf("record task billing log failed task %s: %s", task.TaskID, err.Error()))
		reviewErr := rollbackTaskSettlementAfterBillingLogFailure(ctx, task, preConsumedQuota, preSettlementStatus, preFailReason, prePrivateData, quotaDelta, tokenAdjusted, tokenRefundDelta, actualQuota, err)
		markTaskAccountingRecordReview(ctx, task, "task recalculation", reviewErr)
		return reviewErr
	}
	if err := model.MarkTaskSettlementApplicationApplied(
		task.ID,
		taskSettlementAppliedDetails(taskSettlementOperationRecalculation, actualQuota, preConsumedQuota, quotaDelta, logType),
	); err != nil {
		reviewErr := fmt.Errorf("mark task recalculation accounting applied failed: %w", err)
		markTaskAccountingRecordReview(ctx, task, "task recalculation", reviewErr)
		MarkTaskSettlementReview(ctx, task, actualQuota, reviewErr)
		return reviewErr
	}
	if err := finalizeAppliedTaskRecalculation(task, actualQuota); err != nil {
		err = taskAccountingRecordError("finalize applied task recalculation", task, err)
		MarkTaskSettlementReview(ctx, task, actualQuota, err)
		return err
	}
	return nil
}

// RecalculateTaskQuotaByTokens recalculates async task billing by actual token usage.
func RecalculateTaskQuotaByTokens(ctx context.Context, task *model.Task, totalTokens int) error {
	if totalTokens <= 0 {
		return nil
	}

	// 获取模型价格和倍率
	modelRatio, hasRatioSetting := TaskBillingModelRatio(task)
	// 只有配置了倍率（非固定价格）时才按 token 重新计费
	if !hasRatioSetting || modelRatio <= 0 {
		return nil
	}

	// 获取用户和组的倍率信息
	finalGroupRatio, ok := TaskBillingGroupRatio(task)
	if !ok {
		return nil
	}

	// Calculate the product of OtherRatios, such as video discount and duration.
	otherMultiplier := 1.0
	if priceData := taskBillingContextPriceData(task.PrivateData.BillingContext); priceData != nil {
		otherMultiplier = priceData.OtherRatioMultiplier()
	}

	// 计算实际应扣费额度: totalTokens * modelRatio * groupRatio * otherMultiplier（饱和转换，防止溢出成负数）
	actualQuota, clamp := common.QuotaFromPositiveFloatChecked(float64(totalTokens) * modelRatio * finalGroupRatio * otherMultiplier)

	reason := fmt.Sprintf("token重算：tokens=%d, modelRatio=%.2f, groupRatio=%.2f, otherMultiplier=%.4f", totalTokens, modelRatio, finalGroupRatio, otherMultiplier)
	err := RecalculateTaskQuota(ctx, task, actualQuota, reason, clamp)
	if err != nil {
		MarkTaskSettlementReview(ctx, task, actualQuota, err)
	}
	return err
}
