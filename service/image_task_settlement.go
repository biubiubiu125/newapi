package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	imageTaskSettlementLogLeaseSeconds              int64 = 30
	publicImageTaskRefundApplyingReviewAfterSeconds int64 = 10 * 60
)

var errPublicImageTaskRefundInProgress = errors.New("public image task refund is already in progress")

func IsPublicImageTaskRefundInProgress(err error) bool {
	return errors.Is(err, errPublicImageTaskRefundInProgress)
}

type ImageTaskAtomicSettlement struct {
	ActualQuota      int
	PromptTokens     int
	CompletionTokens int
	UseTimeSeconds   int
	ModelName        string
	TokenName        string
	Content          string
	Other            map[string]interface{}
}

func PrepareImageTaskAtomicSettlement(ctx *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage, extraContent []string) (ImageTaskAtomicSettlement, error) {
	if ctx == nil || info == nil {
		return ImageTaskAtomicSettlement{}, errors.New("image task settlement context is required")
	}
	originUsage := usage
	billingUsage := effectiveBillingUsage(usage)
	summary := calculateTextQuotaSummary(ctx, info, billingUsage)
	var tieredResult *billingexpr.TieredResult
	if originUsage != nil {
		var tieredUsedVars map[string]bool
		if snapshot := info.TieredBillingSnapshot; snapshot != nil {
			tieredUsedVars = billingexpr.UsedVars(snapshot.ExprString)
		}
		if ok, tieredQuota, result := TryTieredSettle(info, BuildTieredTokenParams(billingUsage, summary.IsClaudeUsageSemantic, tieredUsedVars)); ok {
			tieredResult = result
			summary.Quota = composeTieredTextQuota(info, summary, tieredQuota, result)
		}
	}

	var other map[string]interface{}
	if summary.IsClaudeUsageSemantic {
		other = GenerateClaudeOtherInfo(
			ctx,
			info,
			summary.ModelRatio,
			summary.GroupRatio,
			summary.CompletionRatio,
			summary.CacheTokens,
			summary.CacheRatio,
			summary.CacheCreationTokens,
			summary.CacheCreationRatio,
			summary.CacheCreationTokens5m,
			summary.CacheCreationRatio5m,
			summary.CacheCreationTokens1h,
			summary.CacheCreationRatio1h,
			summary.ModelPrice,
			info.PriceData.GroupRatioInfo.GroupSpecialRatio,
		)
		other["usage_semantic"] = "anthropic"
	} else {
		other = GenerateTextOtherInfo(
			ctx,
			info,
			summary.ModelRatio,
			summary.GroupRatio,
			summary.CompletionRatio,
			summary.CacheTokens,
			summary.CacheRatio,
			summary.ModelPrice,
			info.PriceData.GroupRatioInfo.GroupSpecialRatio,
		)
	}
	if firstResponseTime, ok := other["frt"].(float64); ok && firstResponseTime < 0 {
		other["frt"] = float64(0)
	}
	appendUsageBillingPathForLog(other, common.GetContextKeyBool(ctx, constant.ContextKeyLocalCountTokens), originUsage)
	appendImageTaskUsageLogDetails(other, summary)
	attachQuotaSaturation(ctx, info, other)
	if tieredResult != nil {
		InjectTieredBillingInfo(other, info, tieredResult)
	}
	return ImageTaskAtomicSettlement{
		ActualQuota:      summary.Quota,
		PromptTokens:     summary.PromptTokens,
		CompletionTokens: summary.CompletionTokens,
		UseTimeSeconds:   max(0, int(summary.UseTimeSeconds)),
		ModelName:        summary.ModelName,
		TokenName:        summary.TokenName,
		Content:          strings.Join(extraContent, ", "),
		Other:            other,
	}, nil
}

func appendImageTaskUsageLogDetails(other map[string]interface{}, summary textQuotaSummary) {
	if summary.ImageTokens != 0 {
		other["image"] = true
		other["image_ratio"] = summary.ImageRatio
		other["image_output"] = summary.ImageTokens
	}
	if summary.CacheCreationTokens > 0 {
		other["cache_creation_tokens"] = summary.CacheCreationTokens
		other["cache_creation_ratio"] = summary.CacheCreationRatio
	}
	if summary.CacheCreationTokens5m > 0 {
		other["cache_creation_tokens_5m"] = summary.CacheCreationTokens5m
		other["cache_creation_ratio_5m"] = summary.CacheCreationRatio5m
	}
	if summary.CacheCreationTokens1h > 0 {
		other["cache_creation_tokens_1h"] = summary.CacheCreationTokens1h
		other["cache_creation_ratio_1h"] = summary.CacheCreationRatio1h
	}
	if cacheWriteTokens := cacheWriteTokensTotal(summary); cacheWriteTokens > 0 {
		other["cache_write_tokens"] = cacheWriteTokens
	}
}

func ApplyImageTaskSettlementAtomic(ctx context.Context, task *model.Task, input ImageTaskAtomicSettlement) (bool, error) {
	if task == nil || task.ID <= 0 {
		return false, errors.New("image task is required")
	}
	if input.ActualQuota < 0 {
		return false, fmt.Errorf("actual quota cannot be negative: %d", input.ActualQuota)
	}

	applied := false
	preConsumedQuota := 0
	err := model.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var persistedTask model.Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND platform = ?", task.ID, constant.TaskPlatformImage).
			First(&persistedTask).Error; err != nil {
			return err
		}
		if persistedTask.Status != model.TaskStatusSuccess {
			return fmt.Errorf("image task settlement requires SUCCESS status, got %s", persistedTask.Status)
		}

		var record model.TaskSettlementRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("task_primary_id = ?", task.ID).
			First(&record).Error; err != nil {
			return err
		}
		if record.Status == model.TaskSettlementRecordStatusApplied {
			return nil
		}
		if persistedTask.SettlementStatus != model.TaskSettlementStatusPending {
			return fmt.Errorf("image task settlement requires PENDING settlement status, got %s", persistedTask.SettlementStatus)
		}
		if record.Status != model.TaskSettlementRecordStatusApplying || record.Operation != model.TaskSettlementOperationImageAtomic {
			return fmt.Errorf("image task atomic settlement record is not applicable: status=%s operation=%s", record.Status, record.Operation)
		}

		preConsumedQuota = persistedTask.Quota
		delta := input.ActualQuota - preConsumedQuota
		if err := applyImageTaskFundingSettlementTx(tx, &persistedTask, delta); err != nil {
			return err
		}
		settlementTokenID, err := applyImageTaskTokenSettlementTx(tx, &persistedTask, delta)
		if err != nil {
			return err
		}
		if err := model.UpdateTaskConsumptionUsageWithTokenAllowMissingChannelTx(
			tx,
			persistedTask.UserId,
			persistedTask.ChannelId,
			settlementTokenID,
			input.ActualQuota,
		); err != nil {
			return err
		}

		payload, err := buildImageTaskSettlementLogPayload(&persistedTask, input)
		if err != nil {
			return err
		}
		taskUpdate := tx.Model(&model.Task{}).
			Where("id = ? AND status = ? AND settlement_status IN ?", persistedTask.ID, model.TaskStatusSuccess, []string{model.TaskSettlementStatusPending, model.TaskSettlementStatusApplied}).
			Updates(map[string]interface{}{
				"quota":             input.ActualQuota,
				"settlement_status": model.TaskSettlementStatusApplied,
				"updated_at":        common.GetTimestamp(),
			})
		if taskUpdate.Error != nil {
			return taskUpdate.Error
		}
		if taskUpdate.RowsAffected != 1 {
			return errors.New("image task atomic settlement task update lost CAS")
		}
		details := model.TaskSettlementApplicationAppliedDetails{
			Operation:        "image_consumption",
			AppliedQuota:     common.GetPointer(input.ActualQuota),
			PreConsumedQuota: common.GetPointer(preConsumedQuota),
			QuotaDelta:       common.GetPointer(delta),
			LogType:          common.GetPointer(model.LogTypeConsume),
		}
		if err := model.MarkTaskSettlementApplicationAppliedTx(tx, persistedTask.ID, payload, details); err != nil {
			return err
		}
		applied = true
		return nil
	})
	if err != nil {
		return false, err
	}
	if !applied {
		return false, nil
	}

	task.Quota = input.ActualQuota
	task.SettlementStatus = model.TaskSettlementStatusApplied
	model.RefreshTokenQuotaCache(task.PrivateData.TokenId, "")
	if err := model.CacheUpdateUserQuota(task.UserId); err != nil {
		common.SysLog(fmt.Sprintf("refresh image task settlement user cache failed, userId=%d: %s", task.UserId, err.Error()))
	}
	return true, nil
}

func isPublicImageTaskAccounting(task *model.Task) bool {
	return task != nil && task.Platform == constant.TaskPlatformImage && (task.PrivateData.PublicImageTask || task.PublicImageTask)
}

func ApplyPublicImageTaskRefundAtomic(ctx context.Context, task *model.Task, reason string) error {
	if task == nil || task.ID <= 0 {
		return errors.New("public image task is required")
	}
	var persistedAfter model.Task
	err := model.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var persistedTask model.Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND platform = ?", task.ID, constant.TaskPlatformImage).
			First(&persistedTask).Error; err != nil {
			return err
		}
		if (!persistedTask.PrivateData.PublicImageTask && !persistedTask.PublicImageTask) || persistedTask.Status != model.TaskStatusFailure {
			return fmt.Errorf("public image task refund requires a failed public task")
		}
		if persistedTask.SettlementStatus == model.TaskSettlementStatusReview {
			return errors.New("public image task refund already requires review")
		}

		quota := persistedTask.Quota
		var record model.TaskSettlementRecord
		recordExists := true
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("task_primary_id = ?", persistedTask.ID).
			First(&record).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			recordExists = false
		}

		applyRefund := false
		switch {
		case !recordExists && quota == 0 && !persistedTask.RefundPending:
			// Free tasks have no accounting record and need no refund work.
		case !recordExists && quota == 0:
			return errors.New("public image task refund record is missing while refund is pending")
		case !recordExists:
			record = model.TaskSettlementRecord{
				TaskPrimaryID: persistedTask.ID,
				PublicTaskID:  persistedTask.TaskID,
				Status:        model.TaskSettlementRecordStatusApplying,
				Operation:     taskSettlementOperationRefund,
			}
			if err := tx.Create(&record).Error; err != nil {
				return err
			}
			applyRefund = true
		case record.Status == model.TaskSettlementRecordStatusApplied:
			if err := validateAppliedTaskRefundDetails(&persistedTask, &record); err != nil {
				return err
			}
			quota = 0
		case record.Status == model.TaskSettlementRecordStatusPrepared && quota > 0:
			result := tx.Model(&model.TaskSettlementRecord{}).
				Where("id = ? AND status = ?", record.ID, model.TaskSettlementRecordStatusPrepared).
				Updates(map[string]any{
					"status":     model.TaskSettlementRecordStatusApplying,
					"operation":  taskSettlementOperationRefund,
					"error":      "",
					"updated_at": time.Now().Unix(),
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errors.New("public image task refund record lost CAS")
			}
			record.Status = model.TaskSettlementRecordStatusApplying
			record.Operation = taskSettlementOperationRefund
			applyRefund = true
		case record.Status == model.TaskSettlementRecordStatusReview:
			return errors.New("public image task refund record requires review")
		case record.Status == model.TaskSettlementRecordStatusApplying &&
			record.Operation == taskSettlementOperationRefund &&
			record.UpdatedAt > 0 &&
			time.Now().Unix()-record.UpdatedAt < publicImageTaskRefundApplyingReviewAfterSeconds:
			return errPublicImageTaskRefundInProgress
		default:
			return fmt.Errorf("public image task refund record is ambiguous: status=%s operation=%s", record.Status, record.Operation)
		}

		if applyRefund {
			if err := applyImageTaskFundingSettlementTx(tx, &persistedTask, -quota); err != nil {
				return err
			}
			tokenAdjusted := false
			if persistedTask.PrivateData.TokenId > 0 {
				if err := model.IncreaseTokenQuotaTx(tx, persistedTask.PrivateData.TokenId, quota); err != nil {
					if !model.IsTokenQuotaNoRowsError(err) {
						return err
					}
				} else {
					tokenAdjusted = true
				}
			}
			usageCountersAdjusted := taskRefundHasPreConsumedUsage(&persistedTask)
			if usageCountersAdjusted {
				tokenID := 0
				if tokenAdjusted {
					tokenID = persistedTask.PrivateData.TokenId
				}
				if err := model.UpdateTaskUsageAdjustmentWithTokenTx(
					tx,
					persistedTask.UserId,
					persistedTask.ChannelId,
					tokenID,
					-quota,
					taskRefundAllowsMissingChannel(reason),
				); err != nil {
					return err
				}
			}
			payload, err := buildPublicImageTaskRefundLogPayload(&persistedTask, quota, reason, usageCountersAdjusted)
			if err != nil {
				return err
			}
			if err := model.MarkTaskSettlementApplicationAppliedTx(
				tx,
				persistedTask.ID,
				payload,
				taskSettlementAppliedDetails(taskSettlementOperationRefund, 0, quota, -quota, model.LogTypeRefund),
			); err != nil {
				return err
			}
		}

		privateData := persistedTask.PrivateData
		privateData.SettlementAttemptQuota = 0
		privateData.SettlementError = ""
		now := time.Now().Unix()
		result := tx.Model(&model.Task{}).
			Where("id = ? AND status = ?", persistedTask.ID, model.TaskStatusFailure).
			Updates(map[string]any{
				"quota":             0,
				"refund_pending":    false,
				"settlement_status": "",
				"fail_reason":       clearSettlementReviewFailReason(persistedTask.FailReason),
				"private_data":      privateData,
				"updated_at":        now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("public image task refund task update lost CAS")
		}
		persistedTask.Quota = 0
		persistedTask.RefundPending = false
		persistedTask.SettlementStatus = ""
		persistedTask.FailReason = clearSettlementReviewFailReason(persistedTask.FailReason)
		persistedTask.PrivateData = privateData
		persistedTask.UpdatedAt = now
		persistedAfter = persistedTask
		return nil
	})
	if err != nil {
		return err
	}

	*task = persistedAfter
	model.RefreshTokenQuotaCache(task.PrivateData.TokenId, "")
	if err := model.CacheUpdateUserQuota(task.UserId); err != nil {
		common.SysLog(fmt.Sprintf("refresh public image refund user cache failed, userId=%d: %s", task.UserId, err.Error()))
	}
	if err := DispatchPendingImageTaskSettlementLogs(ctx, 10); err != nil {
		common.SysLog(fmt.Sprintf("dispatch public image refund log failed, taskId=%s: %s", task.TaskID, err.Error()))
	}
	return nil
}

func applyImageTaskTokenSettlementTx(tx *gorm.DB, task *model.Task, delta int) (int, error) {
	if task == nil || task.PrivateData.TokenId <= 0 {
		return 0, nil
	}
	tokenID := task.PrivateData.TokenId
	var err error
	switch {
	case delta > 0:
		err = model.DecreaseTokenQuotaTx(tx, tokenID, delta)
	case delta < 0:
		err = model.IncreaseTokenQuotaTx(tx, tokenID, -delta)
	default:
		var count int64
		if err := tx.Model(&model.Token{}).Where("id = ?", tokenID).Count(&count).Error; err != nil {
			return 0, err
		}
		if count == 0 {
			return 0, nil
		}
		return tokenID, nil
	}
	if err == nil {
		return tokenID, nil
	}
	if !model.IsTokenQuotaNoRowsError(err) {
		return 0, err
	}
	var count int64
	if countErr := tx.Model(&model.Token{}).Where("id = ?", tokenID).Count(&count).Error; countErr != nil {
		return 0, countErr
	}
	if count == 0 {
		return 0, nil
	}
	return 0, err
}

func applyImageTaskFundingSettlementTx(tx *gorm.DB, task *model.Task, delta int) error {
	if task == nil || delta == 0 {
		return nil
	}
	switch task.PrivateData.BillingSource {
	case BillingSourceWallet, "":
		if delta > 0 {
			return model.DecreaseUserQuotaTx(tx, task.UserId, delta)
		}
		return model.IncreaseUserQuotaTx(tx, task.UserId, -delta)
	case BillingSourceSubscription:
		return model.PostConsumeUserSubscriptionDeltaTx(tx, task.PrivateData.SubscriptionId, int64(delta))
	default:
		return fmt.Errorf("unsupported image task billing source: %s", task.PrivateData.BillingSource)
	}
}

type imageTaskSettlementLogPayload struct {
	UserID             int                    `json:"user_id"`
	LogType            int                    `json:"log_type"`
	Content            string                 `json:"content"`
	ChannelID          int                    `json:"channel_id"`
	ModelName          string                 `json:"model_name"`
	TokenName          string                 `json:"token_name"`
	Quota              int                    `json:"quota"`
	PromptTokens       int                    `json:"prompt_tokens"`
	CompletionTokens   int                    `json:"completion_tokens"`
	UseTimeSeconds     int                    `json:"use_time_seconds"`
	TokenID            int                    `json:"token_id"`
	Group              string                 `json:"group"`
	RequestID          string                 `json:"request_id"`
	Other              map[string]interface{} `json:"other"`
	CreatedAt          int64                  `json:"created_at"`
	NodeName           string                 `json:"node_name,omitempty"`
	QuotaDataCount     int                    `json:"quota_data_count"`
	QuotaDataTokenUsed int                    `json:"quota_data_token_used"`
	QuotaDataCaptured  bool                   `json:"quota_data_captured,omitempty"`
	SkipQuotaData      bool                   `json:"skip_quota_data,omitempty"`
}

func buildImageTaskSettlementLogPayload(task *model.Task, input ImageTaskAtomicSettlement) (string, error) {
	modelName := input.ModelName
	if modelName == "" && task != nil && task.PrivateData.BillingContext != nil {
		modelName = task.PrivateData.BillingContext.OriginModelName
	}
	payload := imageTaskSettlementLogPayload{
		UserID:             task.UserId,
		LogType:            model.LogTypeConsume,
		Content:            input.Content,
		ChannelID:          task.ChannelId,
		ModelName:          modelName,
		TokenName:          input.TokenName,
		Quota:              input.ActualQuota,
		PromptTokens:       input.PromptTokens,
		CompletionTokens:   input.CompletionTokens,
		UseTimeSeconds:     input.UseTimeSeconds,
		TokenID:            task.PrivateData.TokenId,
		Group:              task.Group,
		RequestID:          task.TaskID,
		Other:              input.Other,
		CreatedAt:          time.Now().Unix(),
		NodeName:           task.PrivateData.NodeName,
		QuotaDataCount:     1,
		QuotaDataTokenUsed: input.PromptTokens + input.CompletionTokens,
		QuotaDataCaptured:  true,
	}
	encoded, err := common.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func buildPublicImageTaskRefundLogPayload(task *model.Task, quota int, reason string, usageCountersAdjusted bool) (string, error) {
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["reason"] = reason
	other["pre_consumed_usage_recorded"] = usageCountersAdjusted
	payload := imageTaskSettlementLogPayload{
		UserID:            task.UserId,
		LogType:           model.LogTypeRefund,
		ChannelID:         task.ChannelId,
		ModelName:         taskModelName(task),
		Quota:             quota,
		TokenID:           task.PrivateData.TokenId,
		Group:             task.Group,
		RequestID:         task.TaskID,
		Other:             other,
		CreatedAt:         time.Now().Unix(),
		NodeName:          task.PrivateData.NodeName,
		QuotaDataCaptured: true,
		SkipQuotaData: !usageCountersAdjusted ||
			(task.Platform == constant.TaskPlatformImage && strings.TrimSpace(task.PrivateData.UpstreamTaskID) == ""),
	}
	encoded, err := common.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func DispatchPendingImageTaskSettlementLogs(ctx context.Context, limit int) error {
	now := time.Now().Unix()
	records, err := model.GetPendingTaskSettlementLogs(limit, now)
	if err != nil {
		return err
	}
	owner := fmt.Sprintf("%s-image-log-%s", common.NodeName, common.GetRandomString(8))
	var firstErr error
	for _, record := range records {
		if record == nil {
			continue
		}
		claimed, claimErr := model.ClaimTaskSettlementLog(record.ID, owner, now, imageTaskSettlementLogLeaseSeconds)
		if claimErr != nil {
			if firstErr == nil {
				firstErr = claimErr
			}
			continue
		}
		if !claimed {
			continue
		}
		if dispatchErr := deliverClaimedImageTaskSettlementLog(ctx, record, owner); dispatchErr != nil {
			_ = model.RetryTaskSettlementLog(record.ID, owner, dispatchErr.Error(), time.Now().Unix())
			if firstErr == nil {
				firstErr = dispatchErr
			}
			continue
		}
	}
	return firstErr
}

type imageTaskSettlementLogIdentity struct {
	Username  string
	TokenName string
}

func parseImageTaskSettlementLog(record *model.TaskSettlementRecord) (imageTaskSettlementLogPayload, error) {
	if record == nil || record.LogPayload == "" {
		return imageTaskSettlementLogPayload{}, errors.New("image task settlement log payload is missing")
	}
	var payload imageTaskSettlementLogPayload
	if err := common.Unmarshal([]byte(record.LogPayload), &payload); err != nil {
		return imageTaskSettlementLogPayload{}, err
	}
	return payload, nil
}

func resolveImageTaskSettlementLogIdentity(payload imageTaskSettlementLogPayload) imageTaskSettlementLogIdentity {
	identity := imageTaskSettlementLogIdentity{TokenName: payload.TokenName}
	identity.Username, _ = model.GetUsernameById(payload.UserID, false)
	if identity.TokenName == "" && payload.TokenID > 0 {
		if token, err := model.GetTokenById(payload.TokenID); err == nil {
			identity.TokenName = token.Name
		}
	}
	return identity
}

func imageTaskSettlementDeliveryKey(recordID int64) string {
	return fmt.Sprintf("task_settlement:%d", recordID)
}

func ensureImageTaskSettlementLog(
	ctx context.Context,
	logDB *gorm.DB,
	record *model.TaskSettlementRecord,
	payload imageTaskSettlementLogPayload,
	identity imageTaskSettlementLogIdentity,
) (bool, error) {
	if payload.LogType == model.LogTypeConsume && !common.LogConsumeEnabled {
		return false, nil
	}
	if logDB == nil {
		return false, errors.New("log database is required")
	}
	deliveryKey := imageTaskSettlementDeliveryKey(record.ID)
	var existing int64
	if err := logDB.WithContext(ctx).Model(&model.Log{}).
		Where("request_id = ? AND type = ?", payload.RequestID, payload.LogType).
		Where("settlement_key = ? OR COALESCE(settlement_key, '') = ''", deliveryKey).
		Count(&existing).Error; err != nil {
		return false, err
	}
	if existing > 0 {
		return true, nil
	}
	err := model.RecordTaskBillingLogWithDB(logDB.WithContext(ctx), model.RecordTaskBillingLogParams{
		UserId:           payload.UserID,
		Username:         identity.Username,
		LogType:          payload.LogType,
		Content:          payload.Content,
		ChannelId:        payload.ChannelID,
		ModelName:        payload.ModelName,
		Quota:            payload.Quota,
		TokenId:          payload.TokenID,
		Group:            payload.Group,
		Other:            payload.Other,
		PromptTokens:     payload.PromptTokens,
		CompletionTokens: payload.CompletionTokens,
		UseTimeSeconds:   payload.UseTimeSeconds,
		TokenName:        identity.TokenName,
		RequestId:        payload.RequestID,
		SettlementKey:    deliveryKey,
		CreatedAt:        payload.CreatedAt,
		NodeName:         payload.NodeName,
		SkipQuotaData:    true,
	})
	return err == nil, err
}

func dispatchImageTaskSettlementLog(ctx context.Context, record *model.TaskSettlementRecord) error {
	payload, err := parseImageTaskSettlementLog(record)
	if err != nil {
		return err
	}
	identity := resolveImageTaskSettlementLogIdentity(payload)
	_, err = ensureImageTaskSettlementLog(ctx, model.LOG_DB, record, payload, identity)
	return err
}

func deliverClaimedImageTaskSettlementLog(ctx context.Context, record *model.TaskSettlementRecord, owner string) error {
	payload, err := parseImageTaskSettlementLog(record)
	if err != nil {
		return err
	}
	identity := resolveImageTaskSettlementLogIdentity(payload)
	return model.DeliverClaimedTaskSettlementLog(ctx, record.ID, owner, time.Now().Unix(), func(tx *gorm.DB, lockedRecord *model.TaskSettlementRecord) error {
		lockedPayload, err := parseImageTaskSettlementLog(lockedRecord)
		if err != nil {
			return err
		}
		logDB := model.LOG_DB
		if model.LOG_DB == model.DB {
			logDB = tx
		}
		logged, err := ensureImageTaskSettlementLog(ctx, logDB, lockedRecord, lockedPayload, identity)
		if err != nil || !logged || !common.DataExportEnabled || lockedPayload.SkipQuotaData {
			return err
		}
		quotaDataCount := lockedPayload.QuotaDataCount
		quotaDataTokens := lockedPayload.QuotaDataTokenUsed
		if !lockedPayload.QuotaDataCaptured {
			if quotaDataCount <= 0 {
				quotaDataCount = 1
			}
			if quotaDataTokens <= 0 {
				quotaDataTokens = lockedPayload.PromptTokens + lockedPayload.CompletionTokens
			}
		}
		nodeName := lockedPayload.NodeName
		if nodeName == "" {
			nodeName = common.NodeName
		}
		quota := lockedPayload.Quota
		if lockedPayload.LogType == model.LogTypeRefund {
			quota = -quota
		}
		return model.RecordTaskSettlementQuotaDataTx(tx, model.QuotaDataLogParams{
			UserID:        lockedPayload.UserID,
			Username:      identity.Username,
			ModelName:     lockedPayload.ModelName,
			Quota:         quota,
			CreatedAt:     lockedPayload.CreatedAt,
			TokenUsed:     quotaDataTokens,
			UseGroup:      lockedPayload.Group,
			TokenID:       lockedPayload.TokenID,
			ChannelID:     lockedPayload.ChannelID,
			NodeName:      nodeName,
			Count:         quotaDataCount,
			ExplicitCount: true,
		})
	})
}
