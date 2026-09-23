package controller

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
)

func referralCommissionAnomalyMessage(c *gin.Context, raw string) string {
	reason := referralCommissionReason(c, raw)
	if reason == "" {
		return i18n.T(c, i18n.MsgReferralCommissionFailed)
	}
	return i18n.T(c, i18n.MsgReferralCommissionFailed, map[string]any{"Reason": reason})
}

func referralCommissionReason(c *gin.Context, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if key, ok := referralCommissionErrorKeys[raw]; ok {
		return i18n.T(c, key)
	}
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "unique constraint failed") || strings.Contains(lower, "duplicate key value"):
		return i18n.T(c, i18n.MsgReferralErrDuplicateRecord)
	case strings.Contains(lower, "subscription order not found"):
		return i18n.T(c, i18n.MsgReferralErrSubscriptionOrderNotFound)
	case strings.Contains(lower, "topup order not found"):
		return i18n.T(c, i18n.MsgReferralErrTopupOrderNotFound)
	case strings.Contains(lower, "record not found"):
		return i18n.T(c, i18n.MsgReferralErrRecordNotFound)
	}
	lang := i18n.GetLangFromContext(c)
	if lang == i18n.LangZhCN || lang == i18n.LangZhTW {
		if cleaned := common.SanitizeChineseConsoleText(raw); cleaned != "" {
			return cleaned
		}
		return ""
	}
	return raw
}

var referralCommissionErrorKeys = map[string]string{
	"fx_rate_missing":                              i18n.MsgReferralErrFxRateMissing,
	"missing_referral_snapshot":                    i18n.MsgReferralErrMissingSnapshot,
	"zero_commission_amount":                       i18n.MsgReferralErrZeroAmount,
	"affiliate_not_eligible":                       i18n.MsgReferralErrNotEligible,
	"unsupported source_type":                      i18n.MsgReferralErrUnsupportedSource,
	"trade_no is required":                         i18n.MsgReferralErrTradeNoRequired,
	"failed to update referral pending amount":     i18n.MsgReferralErrPendingUpdateFailed,
	"record not found":                             i18n.MsgReferralErrRecordNotFound,
	"subscription order not found":                 i18n.MsgReferralErrSubscriptionOrderNotFound,
	"topup order not found":                        i18n.MsgReferralErrTopupOrderNotFound,
	"duplicate_job_superseded_by_subscription":     i18n.MsgReferralErrDuplicateJob,
	"paid_amount must be a positive finite number": i18n.MsgReferralErrPaidAmount,
	"affiliate_not_found":                          i18n.MsgReferralErrAffiliateNotFound,
	"affiliate_not_approved":                       i18n.MsgReferralErrAffiliateNotApproved,
	"affiliate_acquisition_disabled":               i18n.MsgReferralErrAcquisitionDisabled,
	"affiliate_settlement_disabled":                i18n.MsgReferralErrSettlementDisabled,
	"no_binding":                                   i18n.MsgReferralErrNoBinding,
	"invalid_rate":                                 i18n.MsgReferralErrInvalidRate,
	"redemption_commission_chain_incomplete":       i18n.MsgReferralErrChainIncomplete,
}
