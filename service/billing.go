package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

const (
	BillingSourceWallet             = "wallet"
	BillingSourceSubscription       = "subscription"
	contextKeySettlementApplied     = "settlement_applied"
	contextKeySettlementError       = "settlement_error"
	contextKeyUsageCountersRecorded = "usage_counters_recorded"
	contextKeySubmittedTask         = "submitted_task"
)

func ContextKeySettlementError() string {
	return contextKeySettlementError
}

func ContextKeySettlementApplied() string {
	return contextKeySettlementApplied
}

func ContextKeyUsageCountersRecorded() string {
	return contextKeyUsageCountersRecorded
}

func ContextKeySubmittedTask() string {
	return contextKeySubmittedTask
}

func setUsageCountersRecorded(ctx context.Context, recorded bool) {
	if ginCtx, ok := ctx.(*gin.Context); ok && ginCtx != nil {
		ginCtx.Set(contextKeyUsageCountersRecorded, recorded)
	}
}

func attachSettlementError(other *model.LogOther, settleErr error) {
	if settleErr == nil {
		return
	}
	attachSettlementErrorMessage(other, settleErr.Error())
}

func attachSettlementErrorMessage(other *model.LogOther, errMsg string) {
	if other == nil || errMsg == "" {
		return
	}
	other.SetPublic("settlement_status", "error")
	other.SetPublic("settlement_error", strings.ReplaceAll(errMsg, "\n", " "))
}

func AttachSettlementError(other *model.LogOther, settleErr error) {
	attachSettlementError(other, settleErr)
}

func AttachSettlementLogFields(other *model.LogOther, relayInfo *relaycommon.RelayInfo, attemptedQuota int, settleErr error) int {
	return attachSettlementLogFields(other, relayInfo, attemptedQuota, settleErr)
}

func settlementFallbackQuota(relayInfo *relaycommon.RelayInfo) int {
	if relayInfo == nil {
		return 0
	}
	if relayInfo.FinalPreConsumedQuota != 0 {
		return relayInfo.FinalPreConsumedQuota
	}
	if relayInfo.Billing != nil {
		return relayInfo.Billing.GetPreConsumedQuota()
	}
	return 0
}

func logQuotaAfterSettlement(relayInfo *relaycommon.RelayInfo, attemptedQuota int, settlementFailed bool) int {
	if !settlementFailed {
		return attemptedQuota
	}
	return settlementFallbackQuota(relayInfo)
}

func LogQuotaAfterSettlement(relayInfo *relaycommon.RelayInfo, attemptedQuota int, settleErr error) int {
	return logQuotaAfterSettlement(relayInfo, attemptedQuota, settleErr != nil)
}

func attachSettlementAccountingFields(other *model.LogOther, attemptedQuota int, settledQuota int) {
	if other == nil {
		return
	}
	other.SetPublic("attempted_quota", attemptedQuota)
	other.SetPublic("settled_quota", settledQuota)
}

func attachSettlementLogFields(other *model.LogOther, relayInfo *relaycommon.RelayInfo, attemptedQuota int, settleErr error) int {
	logQuota := LogQuotaAfterSettlement(relayInfo, attemptedQuota, settleErr)
	if settleErr != nil {
		attachSettlementError(other, settleErr)
		attachSettlementAccountingFields(other, attemptedQuota, logQuota)
	}
	return logQuota
}

func isDeferredBillingSettlement(err error) bool {
	return errors.Is(err, model.ErrBillingAdjustmentDeferred)
}

// consumeSettlementLog records a deferred charge at the attempted quota.
// settlementApplied 表示钱已经落账，用量失败时可以回滚。
// chargeAccepted 表示结果已经交付，日志失败不能把这笔钱退掉。
func consumeSettlementLog(other *model.LogOther, relayInfo *relaycommon.RelayInfo, attemptedQuota int, settleErr error) (logQuota int, settlementApplied bool, chargeAccepted bool, clientErr error) {
	if isDeferredBillingSettlement(settleErr) {
		if other != nil {
			other.SetAdmin("billing_adjustment_pending", true)
		}
		return attemptedQuota, false, true, nil
	}
	logQuota = attachSettlementLogFields(other, relayInfo, attemptedQuota, settleErr)
	applied := settleErr == nil
	return logQuota, applied, applied, settleErr
}

func attachSettlementLogFieldsMessage(other *model.LogOther, relayInfo *relaycommon.RelayInfo, attemptedQuota int, errMsg string) int {
	logQuota := logQuotaAfterSettlement(relayInfo, attemptedQuota, errMsg != "")
	if errMsg != "" {
		attachSettlementErrorMessage(other, errMsg)
		attachSettlementAccountingFields(other, attemptedQuota, logQuota)
	}
	return logQuota
}

// PreConsumeBilling 根据用户计费偏好创建 BillingSession 并执行预扣费。
// 会话存储在 relayInfo.Billing 上，供后续 Settle / Refund 使用。
func PreConsumeBilling(c *gin.Context, preConsumedQuota int, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	if relayInfo != nil && relayInfo.QuotaClamp != nil {
		return types.NewErrorWithStatusCode(
			relayInfo.QuotaClamp,
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if preConsumedQuota < 0 {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("pre-consume quota cannot be negative: %d", preConsumedQuota),
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	session, apiErr := NewBillingSession(c, relayInfo, preConsumedQuota)
	if apiErr != nil {
		return apiErr
	}
	relayInfo.Billing = session
	return nil
}

// ---------------------------------------------------------------------------
// SettleBilling — 后结算辅助函数
// ---------------------------------------------------------------------------

// SettleBilling 执行计费结算。如果 RelayInfo 上有 BillingSession 则通过 session 结算，
// 否则回退到旧的 PostConsumeQuota 路径（兼容按次计费等场景）。
func SettleBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int) error {
	if relayInfo.Billing != nil {
		preConsumed := relayInfo.Billing.GetPreConsumedQuota()
		delta := actualQuota - preConsumed

		if delta > 0 {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费后补扣费：%s（实际消耗：%s，预扣费：%s）",
				logger.FormatQuota(delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else if delta < 0 {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费后返还扣费：%s（实际消耗：%s，预扣费：%s）",
				logger.FormatQuota(-delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费与实际消耗一致，无需调整：%s（按次计费）",
				logger.FormatQuota(actualQuota),
			))
		}

		if err := relayInfo.Billing.Settle(actualQuota); err != nil {
			return err
		}

		// 发送额度通知（订阅计费使用订阅剩余额度）
		if actualQuota != 0 {
			if relayInfo.BillingSource == BillingSourceSubscription {
				checkAndSendSubscriptionQuotaNotify(relayInfo)
			} else {
				checkAndSendQuotaNotify(relayInfo, actualQuota-preConsumed, preConsumed)
			}
		}
		return nil
	}

	// 回退：无 BillingSession 时使用旧路径
	quotaDelta := actualQuota - relayInfo.FinalPreConsumedQuota
	if quotaDelta != 0 {
		return PostConsumeQuota(relayInfo, quotaDelta, relayInfo.FinalPreConsumedQuota, true)
	}
	return nil
}

// settleDeliveredBilling settles a response the client already has.
// A terminal ledger error is reconciled to the stored or current charge.
// Returning that error would make the relay refund a delivered response.
func settleDeliveredBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int) (int, error) {
	err := SettleBilling(ctx, relayInfo, actualQuota)
	if err == nil || relayInfo == nil || relayInfo.Billing == nil || isDeferredBillingSettlement(err) {
		if err == nil {
			if session, ok := relayInfo.Billing.(*BillingSession); ok {
				if charged, ok := session.DeliveredQuota(); ok {
					return charged, nil
				}
			}
		}
		return actualQuota, err
	}
	session, ok := relayInfo.Billing.(*BillingSession)
	if !ok {
		return actualQuota, err
	}
	charged, applied, acceptErr := session.AcceptDeliveredSettlement(actualQuota)
	if acceptErr != nil {
		return actualQuota, err
	}
	if applied && charged != 0 {
		preConsumed := relayInfo.Billing.GetPreConsumedQuota()
		if relayInfo.BillingSource == BillingSourceSubscription {
			checkAndSendSubscriptionQuotaNotify(relayInfo)
		} else {
			checkAndSendQuotaNotify(relayInfo, charged-preConsumed, preConsumed)
		}
	}
	return charged, nil
}
