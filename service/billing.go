package service

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

const (
	BillingSourceWallet         = "wallet"
	BillingSourceSubscription   = "subscription"
	contextKeySettlementApplied = "settlement_applied"
	contextKeySettlementError   = "settlement_error"
)

func ContextKeySettlementError() string {
	return contextKeySettlementError
}

func ContextKeySettlementApplied() string {
	return contextKeySettlementApplied
}

func attachSettlementError(other map[string]interface{}, settleErr error) {
	if settleErr == nil {
		return
	}
	attachSettlementErrorMessage(other, settleErr.Error())
}

func attachSettlementErrorMessage(other map[string]interface{}, errMsg string) {
	if other == nil || errMsg == "" {
		return
	}
	other["settlement_status"] = "error"
	other["settlement_error"] = strings.ReplaceAll(errMsg, "\n", " ")
}

func AttachSettlementError(other map[string]interface{}, settleErr error) {
	attachSettlementError(other, settleErr)
}

func AttachSettlementLogFields(other map[string]interface{}, relayInfo *relaycommon.RelayInfo, attemptedQuota int, settleErr error) int {
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

func attachSettlementAccountingFields(other map[string]interface{}, attemptedQuota int, settledQuota int) {
	if other == nil {
		return
	}
	other["attempted_quota"] = attemptedQuota
	other["settled_quota"] = settledQuota
}

func attachSettlementLogFields(other map[string]interface{}, relayInfo *relaycommon.RelayInfo, attemptedQuota int, settleErr error) int {
	logQuota := LogQuotaAfterSettlement(relayInfo, attemptedQuota, settleErr)
	if settleErr != nil {
		attachSettlementError(other, settleErr)
		attachSettlementAccountingFields(other, attemptedQuota, logQuota)
	}
	return logQuota
}

func attachSettlementLogFieldsMessage(other map[string]interface{}, relayInfo *relaycommon.RelayInfo, attemptedQuota int, errMsg string) int {
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
