package controller

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

func processPaidTopUpCommission(ctx context.Context, tradeNo string) error {
	// Payment fulfillment already succeeded before this hook runs; referral
	// issues are audited manually and must not make gateways retry paid orders.
	if err := referralService.ProcessTopUpCommission(tradeNo); err != nil {
		logger.LogError(ctx, fmt.Sprintf("referral topup commission processing failed trade_no=%s error=%q", tradeNo, err.Error()))
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

func topUpReferralCommissionStatus(tradeNo string) (string, string, error) {
	var topUp model.TopUp
	if err := model.DB.Select("referral_commission_status", "referral_commission_error").
		Where("trade_no = ?", tradeNo).
		First(&topUp).Error; err != nil {
		return "", "", err
	}
	return topUp.ReferralCommissionStatus, topUp.ReferralCommissionError, nil
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
