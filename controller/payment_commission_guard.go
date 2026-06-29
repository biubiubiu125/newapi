package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

func processPaidTopUpCommission(ctx context.Context, tradeNo string) error {
	// Payment fulfillment already succeeded before this hook runs; referral
	// issues are audited manually and must not make gateways retry paid orders.
	if err := referralService.ProcessTopUpCommission(tradeNo); err != nil {
		logger.LogError(ctx, fmt.Sprintf("referral topup commission processing failed trade_no=%s error=%q", tradeNo, err.Error()))
		markPaidTopUpCommissionFailed(ctx, tradeNo, err)
		return nil
	}

	status, detail, err := topUpReferralCommissionStatus(tradeNo)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("referral topup commission status check failed trade_no=%s error=%q", tradeNo, err.Error()))
		return nil
	}
	if status == model.ReferralCommissionJobStatusFailed {
		logger.LogError(ctx, fmt.Sprintf("referral topup commission failed trade_no=%s error=%q", tradeNo, detail))
		return nil
	}
	if status != model.ReferralCommissionJobStatusSucceeded && status != model.ReferralCommissionJobStatusSkipped {
		logger.LogError(ctx, fmt.Sprintf("referral topup commission incomplete trade_no=%s status=%s", tradeNo, status))
		return nil
	}
	return nil
}

func processPaidSubscriptionCommission(ctx context.Context, tradeNo string) error {
	// Payment fulfillment already succeeded before this hook runs; referral
	// issues are audited manually and must not make gateways retry paid orders.
	if err := referralService.ProcessSubscriptionCommission(tradeNo); err != nil {
		logger.LogError(ctx, fmt.Sprintf("referral subscription commission processing failed trade_no=%s error=%q", tradeNo, err.Error()))
		markPaidSubscriptionCommissionFailed(ctx, tradeNo, err)
		return nil
	}

	status, detail, err := subscriptionReferralCommissionStatus(tradeNo)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("referral subscription commission status check failed trade_no=%s error=%q", tradeNo, err.Error()))
		return nil
	}
	if status == model.ReferralCommissionJobStatusFailed {
		logger.LogError(ctx, fmt.Sprintf("referral subscription commission failed trade_no=%s error=%q", tradeNo, detail))
		return nil
	}
	if status != model.ReferralCommissionJobStatusSucceeded && status != model.ReferralCommissionJobStatusSkipped {
		logger.LogError(ctx, fmt.Sprintf("referral subscription commission incomplete trade_no=%s status=%s", tradeNo, status))
		return nil
	}
	return nil
}

func processRedeemedCodeCommission(ctx context.Context, redemptionId int) error {
	if redemptionId <= 0 {
		return nil
	}
	if err := referralService.ProcessRedemptionCommission(redemptionId); err != nil {
		logger.LogError(ctx, fmt.Sprintf("referral redemption commission processing failed redemption_id=%d error=%q", redemptionId, err.Error()))
		markRedeemedCodeCommissionFailed(ctx, redemptionId, err)
	}
	return nil
}

func topUpReferralCommissionStatus(tradeNo string) (string, string, error) {
	var topUp model.TopUp
	if err := model.DB.Select("referral_commission_status", "referral_commission_error").
		Where("trade_no = ?", tradeNo).
		First(&topUp).Error; err != nil {
		return "", "", err
	}
	return topUp.ReferralCommissionStatus, topUp.ReferralCommissionError, nil
}

func markPaidTopUpCommissionFailed(ctx context.Context, tradeNo string, cause error) {
	var topUp model.TopUp
	if err := model.DB.Select("referral_affiliate_id").Where("trade_no = ?", tradeNo).First(&topUp).Error; err != nil {
		logger.LogError(ctx, fmt.Sprintf("referral topup commission failed-state lookup failed trade_no=%s error=%q", tradeNo, err.Error()))
		return
	}
	if err := referralService.MarkCommissionJobFailed("topup", tradeNo, topUp.ReferralAffiliateId, cause); err != nil {
		logger.LogError(ctx, fmt.Sprintf("referral topup commission failed-state job update failed trade_no=%s error=%q", tradeNo, err.Error()))
	}
	update := map[string]interface{}{
		"referral_commission_status": model.ReferralCommissionJobStatusFailed,
		"referral_commission_error":  referralFailureMessage(cause),
		"referral_commission_at":     time.Now().Unix(),
	}
	if err := model.DB.Model(&model.TopUp{}).
		Where("trade_no = ? AND referral_commission_status NOT IN ?", tradeNo, []string{
			model.ReferralCommissionJobStatusSucceeded,
			model.ReferralCommissionJobStatusSkipped,
		}).
		Updates(update).Error; err != nil {
		logger.LogError(ctx, fmt.Sprintf("referral topup commission failed-state order update failed trade_no=%s error=%q", tradeNo, err.Error()))
	}
}

func markRedeemedCodeCommissionFailed(ctx context.Context, redemptionId int, cause error) {
	tradeNo := redemptionCommissionTradeNo(redemptionId)
	affiliateId := 0
	var redemption model.Redemption
	if err := model.DB.Unscoped().Select("referral_affiliate_id").Where("id = ?", redemptionId).First(&redemption).Error; err != nil {
		logger.LogError(ctx, fmt.Sprintf("referral redemption commission failed-state lookup failed redemption_id=%d error=%q", redemptionId, err.Error()))
	} else {
		affiliateId = redemption.ReferralAffiliateId
	}
	if err := referralService.MarkCommissionJobFailed("redemption", tradeNo, affiliateId, cause); err != nil {
		logger.LogError(ctx, fmt.Sprintf("referral redemption commission failed-state job update failed redemption_id=%d error=%q", redemptionId, err.Error()))
	}
	update := map[string]interface{}{
		"referral_commission_status": model.ReferralCommissionJobStatusFailed,
		"referral_commission_error":  referralFailureMessage(cause),
		"referral_commission_at":     time.Now().Unix(),
	}
	if err := model.DB.Unscoped().Model(&model.Redemption{}).
		Where("id = ? AND COALESCE(referral_commission_status, '') NOT IN ?", redemptionId, []string{
			model.ReferralCommissionJobStatusSucceeded,
			model.ReferralCommissionJobStatusSkipped,
		}).
		Updates(update).Error; err != nil {
		logger.LogError(ctx, fmt.Sprintf("referral redemption commission failed-state source update failed redemption_id=%d error=%q", redemptionId, err.Error()))
	}
}

func redemptionCommissionTradeNo(redemptionId int) string {
	return fmt.Sprintf("redemption:%d", redemptionId)
}

func markPaidSubscriptionCommissionFailed(ctx context.Context, tradeNo string, cause error) {
	var order model.SubscriptionOrder
	if err := model.DB.Select("referral_affiliate_id").Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		logger.LogError(ctx, fmt.Sprintf("referral subscription commission failed-state lookup failed trade_no=%s error=%q", tradeNo, err.Error()))
		return
	}
	if err := referralService.MarkCommissionJobFailed("subscription", tradeNo, order.ReferralAffiliateId, cause); err != nil {
		logger.LogError(ctx, fmt.Sprintf("referral subscription commission failed-state job update failed trade_no=%s error=%q", tradeNo, err.Error()))
	}
	update := map[string]interface{}{
		"referral_commission_status": model.ReferralCommissionJobStatusFailed,
		"referral_commission_error":  referralFailureMessage(cause),
		"referral_commission_at":     time.Now().Unix(),
	}
	if err := model.DB.Model(&model.SubscriptionOrder{}).
		Where("trade_no = ? AND referral_commission_status NOT IN ?", tradeNo, []string{
			model.ReferralCommissionJobStatusSucceeded,
			model.ReferralCommissionJobStatusSkipped,
		}).
		Updates(update).Error; err != nil {
		logger.LogError(ctx, fmt.Sprintf("referral subscription commission failed-state order update failed trade_no=%s error=%q", tradeNo, err.Error()))
	}
	if err := model.DB.Model(&model.TopUp{}).
		Where("trade_no = ? AND referral_commission_status NOT IN ?", tradeNo, []string{
			model.ReferralCommissionJobStatusSucceeded,
			model.ReferralCommissionJobStatusSkipped,
		}).
		Updates(update).Error; err != nil {
		logger.LogError(ctx, fmt.Sprintf("referral subscription commission failed-state synthetic topup update failed trade_no=%s error=%q", tradeNo, err.Error()))
	}
}

func referralFailureMessage(cause error) string {
	message := strings.TrimSpace(fmt.Sprint(cause))
	if message == "" {
		return "referral commission processing failed"
	}
	return message
}

func subscriptionReferralCommissionStatus(tradeNo string) (string, string, error) {
	var order model.SubscriptionOrder
	if err := model.DB.Select("referral_commission_status", "referral_commission_error").
		Where("trade_no = ?", tradeNo).
		First(&order).Error; err != nil {
		return "", "", err
	}
	return order.ReferralCommissionStatus, order.ReferralCommissionError, nil
}
