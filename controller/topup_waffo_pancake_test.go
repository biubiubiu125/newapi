package controller

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestFormatWaffoPancakeAmount_UsesDisplayPriceString(t *testing.T) {
	testCases := []struct {
		name     string
		amount   float64
		expected string
	}{
		{name: "whole amount", amount: 29, expected: "29.00"},
		{name: "decimal amount", amount: 29.9, expected: "29.90"},
		{name: "round half up to cents", amount: 29.999, expected: "30.00"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := formatWaffoPancakeAmount(tc.amount, "USD")
			require.NoError(t, err)
			require.Equal(t, tc.expected, actual)
		})
	}
}

func TestGetWaffoPancakePayMoney(t *testing.T) {
	originalUnitPrice := setting.WaffoPancakeUnitPrice
	originalQuotaDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalDiscounts := make(map[int]float64, len(operation_setting.GetPaymentSetting().AmountDiscount))
	for k, v := range operation_setting.GetPaymentSetting().AmountDiscount {
		originalDiscounts[k] = v
	}
	originalTopupGroupRatio := common.TopupGroupRatio2JSONString()

	t.Cleanup(func() {
		setting.WaffoPancakeUnitPrice = originalUnitPrice
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalQuotaDisplayType
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscounts
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(originalTopupGroupRatio))
	})

	setting.WaffoPancakeUnitPrice = 2.5
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{
		10:                           0.8,
		int(common.QuotaPerUnit * 3): 0.5,
		20:                           0,
	}
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1,"vip":1.2}`))

	testCases := []struct {
		name             string
		amount           int64
		group            string
		quotaDisplayType string
		expected         float64
	}{
		{
			name:             "currency display applies unit price group ratio and discount",
			amount:           10,
			group:            "vip",
			quotaDisplayType: operation_setting.QuotaDisplayTypeUSD,
			expected:         24,
		},
		{
			name:             "tokens display converts quota to display units before pricing",
			amount:           int64(common.QuotaPerUnit * 3),
			group:            "vip",
			quotaDisplayType: operation_setting.QuotaDisplayTypeTokens,
			expected:         4.5,
		},
		{
			name:             "non-positive discount falls back to no discount",
			amount:           20,
			group:            "default",
			quotaDisplayType: operation_setting.QuotaDisplayTypeUSD,
			expected:         50,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			operation_setting.GetGeneralSetting().QuotaDisplayType = tc.quotaDisplayType
			actual := getWaffoPancakePayMoney(tc.amount, tc.group)
			require.InDelta(t, tc.expected, actual, 0.000001)
		})
	}
}

func TestWaffoPancakeSubscriptionCompletionProcessesCommission(t *testing.T) {
	setupPaymentCallbackGuardDB(t)
	db := model.DB
	previousReferralEnabled := common.ReferralEnabled
	previousReferralSettlementCurrency := common.ReferralSettlementCurrency
	previousReferralSettlementFxRates := common.ReferralSettlementFxRates
	t.Cleanup(func() {
		common.ReferralEnabled = previousReferralEnabled
		common.ReferralSettlementCurrency = previousReferralSettlementCurrency
		common.ReferralSettlementFxRates = previousReferralSettlementFxRates
	})
	common.ReferralEnabled = true
	common.ReferralSettlementCurrency = "CNY"
	common.ReferralSettlementFxRates = map[string]float64{"CNY": 1, "USD": 7}

	affiliateUser := &model.User{Id: 501, Username: "waffo_sub_affiliate", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(affiliateUser).Error)
	invitee := &model.User{Id: 502, Username: "waffo_sub_invitee", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(invitee).Error)
	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "WAFFOSUB",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, db.Create(affiliate).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionAccount{
		AffiliateId: affiliate.Id,
		UserId:      affiliateUser.Id,
	}).Error)
	plan := &model.SubscriptionPlan{
		Id:            901,
		Title:         "Waffo Sub Plan",
		PriceAmount:   9.99,
		Currency:      "USD",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}
	require.NoError(t, db.Create(plan).Error)
	order := &model.SubscriptionOrder{
		UserId:               invitee.Id,
		PlanId:               plan.Id,
		Money:                plan.PriceAmount,
		PaidAmount:           plan.PriceAmount,
		PaidCurrency:         "USD",
		TradeNo:              "WAFFO_PANCAKE_SUB-502-1-test",
		PaymentMethod:        model.PaymentMethodWaffoPancake,
		PaymentProvider:      model.PaymentProviderWaffoPancake,
		Status:               common.TopUpStatusPending,
		CreateTime:           time.Now().Unix(),
		ReferralAffiliateId:  affiliate.Id,
		ReferralRate:         20,
		ReferralBaseAmount:   plan.PriceAmount,
		ReferralBaseCurrency: "USD",
	}
	applySubscriptionOrderSnapshot(order, plan, "USD")
	require.NoError(t, order.Insert())
	event := &service.WaffoPancakeWebhookEvent{
		Data: service.WaffoPancakeWebhookData{
			Amount:   "9.99",
			Currency: "USD",
		},
	}

	require.NoError(t, model.CompleteSubscriptionOrderWithValidation(order.TradeNo, `{"provider":"waffo_pancake"}`, model.PaymentCallbackValidation{
		ExpectedPaymentProvider: model.PaymentProviderWaffoPancake,
		ActualPaymentMethod:     model.PaymentMethodWaffoPancake,
		PaidAmount:              waffoPancakeEventPaidAmount(event),
		PaidCurrency:            waffoPancakeEventPaidCurrency(event),
		RequirePaymentFacts:     true,
		CallerIP:                "127.0.0.1",
	}))
	require.NoError(t, processPaidSubscriptionCommission(context.Background(), order.TradeNo))

	reloaded := model.GetSubscriptionOrderByTradeNo(order.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusSuccess, reloaded.Status)
	require.Equal(t, model.ReferralCommissionJobStatusSucceeded, reloaded.ReferralCommissionStatus)
	var commission model.ReferralCommission
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "subscription", order.TradeNo).First(&commission).Error)
	require.Equal(t, "USD", commission.PaidCurrency)
	require.InDelta(t, 9.99, commission.PaidAmount, 0.000001)
	require.InDelta(t, 14.59, commission.CommissionAmount, 0.01)
}
