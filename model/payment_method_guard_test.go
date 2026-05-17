package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func insertUserForPaymentGuardTest(t *testing.T, id int, quota int) {
	t.Helper()
	user := &User{
		Id:       id,
		Username: "payment_guard_user",
		Status:   common.UserStatusEnabled,
		Quota:    quota,
	}
	require.NoError(t, DB.Create(user).Error)
}

func insertSubscriptionPlanForPaymentGuardTest(t *testing.T, id int) *SubscriptionPlan {
	t.Helper()
	plan := &SubscriptionPlan{
		Id:            id,
		Title:         "Guard Plan",
		PriceAmount:   9.99,
		Currency:      "USD",
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}
	require.NoError(t, DB.Create(plan).Error)
	return plan
}

func insertSubscriptionOrderForPaymentGuardTest(t *testing.T, tradeNo string, userID int, planID int, paymentProvider string) {
	t.Helper()
	order := &SubscriptionOrder{
		UserId:          userID,
		PlanId:          planID,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "CNY",
		TradeNo:         tradeNo,
		PaymentMethod:   paymentProvider,
		PaymentProvider: paymentProvider,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, order.Insert())
}

func insertTopUpForPaymentGuardTest(t *testing.T, tradeNo string, userID int, paymentProvider string) {
	t.Helper()
	topUp := &TopUp{
		UserId:          userID,
		Amount:          2,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "CNY",
		TradeNo:         tradeNo,
		PaymentMethod:   paymentProvider,
		PaymentProvider: paymentProvider,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())
}

func getTopUpStatusForPaymentGuardTest(t *testing.T, tradeNo string) string {
	t.Helper()
	topUp := GetTopUpByTradeNo(tradeNo)
	require.NotNil(t, topUp)
	return topUp.Status
}

func countUserSubscriptionsForPaymentGuardTest(t *testing.T, userID int) int64 {
	t.Helper()
	var count int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", userID).Count(&count).Error)
	return count
}

func getUserQuotaForPaymentGuardTest(t *testing.T, userID int) int {
	t.Helper()
	var user User
	require.NoError(t, DB.Select("quota").Where("id = ?", userID).First(&user).Error)
	return user.Quota
}

func TestRechargeWaffoPancake_RejectsMismatchedPaymentMethod(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 101, 0)
	insertTopUpForPaymentGuardTest(t, "waffo-pancake-guard", 101, PaymentProviderStripe)

	err := RechargeWaffoPancake("waffo-pancake-guard")
	require.Error(t, err)

	topUp := GetTopUpByTradeNo("waffo-pancake-guard")
	require.NotNil(t, topUp)
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
	assert.Equal(t, 0, getUserQuotaForPaymentGuardTest(t, 101))
}

func TestUpdatePendingTopUpStatus_RejectsMismatchedPaymentProvider(t *testing.T) {
	testCases := []struct {
		name                    string
		tradeNo                 string
		storedPaymentProvider   string
		expectedPaymentProvider string
		targetStatus            string
	}{
		{
			name:                    "stripe expire",
			tradeNo:                 "stripe-expire-guard",
			storedPaymentProvider:   PaymentProviderCreem,
			expectedPaymentProvider: PaymentProviderStripe,
			targetStatus:            common.TopUpStatusExpired,
		},
		{
			name:                    "waffo failed",
			tradeNo:                 "waffo-failed-guard",
			storedPaymentProvider:   PaymentProviderStripe,
			expectedPaymentProvider: PaymentProviderWaffo,
			targetStatus:            common.TopUpStatusFailed,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			insertUserForPaymentGuardTest(t, 150, 0)
			insertTopUpForPaymentGuardTest(t, tc.tradeNo, 150, tc.storedPaymentProvider)

			err := UpdatePendingTopUpStatus(tc.tradeNo, tc.expectedPaymentProvider, tc.targetStatus)
			require.ErrorIs(t, err, ErrPaymentMethodMismatch)
			assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, tc.tradeNo))
		})
	}
}

func TestCompleteSubscriptionOrder_RejectsMismatchedPaymentProvider(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 202, 0)
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 301)
	insertSubscriptionOrderForPaymentGuardTest(t, "sub-guard-order", 202, plan.Id, PaymentProviderStripe)

	err := CompleteSubscriptionOrder("sub-guard-order", `{"provider":"epay"}`, PaymentProviderEpay, "alipay")
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)

	order := GetSubscriptionOrderByTradeNo("sub-guard-order")
	require.NotNil(t, order)
	assert.Equal(t, common.TopUpStatusPending, order.Status)
	assert.Zero(t, countUserSubscriptionsForPaymentGuardTest(t, 202))

	topUp := GetTopUpByTradeNo("sub-guard-order")
	assert.Nil(t, topUp)
}

func TestCompleteSubscriptionOrder_RejectsMismatchedCallbackFacts(t *testing.T) {
	testCases := []struct {
		name          string
		validation    PaymentCallbackValidation
		expectedError error
	}{
		{
			name: "payment method mismatch",
			validation: PaymentCallbackValidation{
				ExpectedPaymentProvider: PaymentProviderEpay,
				ActualPaymentMethod:     "wxpay",
				PaidAmount:              9.99,
				PaidCurrency:            "CNY",
				RequirePaymentFacts:     true,
			},
			expectedError: ErrPaymentMethodMismatch,
		},
		{
			name: "amount mismatch",
			validation: PaymentCallbackValidation{
				ExpectedPaymentProvider: PaymentProviderEpay,
				ActualPaymentMethod:     PaymentProviderEpay,
				PaidAmount:              9.98,
				PaidCurrency:            "CNY",
				RequirePaymentFacts:     true,
			},
			expectedError: ErrPaymentAmountMismatch,
		},
		{
			name: "currency mismatch",
			validation: PaymentCallbackValidation{
				ExpectedPaymentProvider: PaymentProviderEpay,
				ActualPaymentMethod:     PaymentProviderEpay,
				PaidAmount:              9.99,
				PaidCurrency:            "USDT",
				RequirePaymentFacts:     true,
			},
			expectedError: ErrPaymentCurrencyMismatch,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			insertUserForPaymentGuardTest(t, 212, 0)
			plan := insertSubscriptionPlanForPaymentGuardTest(t, 312)
			insertSubscriptionOrderForPaymentGuardTest(t, "sub-callback-facts", 212, plan.Id, PaymentProviderEpay)

			err := CompleteSubscriptionOrderWithValidation("sub-callback-facts", `{"provider":"epay"}`, tc.validation)
			require.ErrorIs(t, err, tc.expectedError)

			order := GetSubscriptionOrderByTradeNo("sub-callback-facts")
			require.NotNil(t, order)
			assert.Equal(t, common.TopUpStatusPending, order.Status)
			assert.Zero(t, countUserSubscriptionsForPaymentGuardTest(t, 212))
		})
	}
}

func TestRechargeEpusdtWithValidation_RejectsMismatchedCallbackFacts(t *testing.T) {
	testCases := []struct {
		name          string
		validation    PaymentCallbackValidation
		expectedError error
	}{
		{
			name: "payment method mismatch",
			validation: PaymentCallbackValidation{
				ExpectedPaymentProvider: PaymentProviderEpusdt,
				ActualPaymentMethod:     "epusdt:usdt:polygon",
				PaidAmount:              9.99,
				PaidCurrency:            "CNY",
				RequirePaymentFacts:     true,
			},
			expectedError: ErrPaymentMethodMismatch,
		},
		{
			name: "amount mismatch",
			validation: PaymentCallbackValidation{
				ExpectedPaymentProvider: PaymentProviderEpusdt,
				ActualPaymentMethod:     PaymentProviderEpusdt,
				PaidAmount:              10.01,
				PaidCurrency:            "CNY",
				RequirePaymentFacts:     true,
			},
			expectedError: ErrPaymentAmountMismatch,
		},
		{
			name: "currency mismatch",
			validation: PaymentCallbackValidation{
				ExpectedPaymentProvider: PaymentProviderEpusdt,
				ActualPaymentMethod:     PaymentProviderEpusdt,
				PaidAmount:              9.99,
				PaidCurrency:            "USDT",
				RequirePaymentFacts:     true,
			},
			expectedError: ErrPaymentCurrencyMismatch,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			insertUserForPaymentGuardTest(t, 404, 0)
			insertTopUpForPaymentGuardTest(t, "epusdt-callback-facts", 404, PaymentProviderEpusdt)

			err := RechargeEpusdtWithValidation("epusdt-callback-facts", `{"provider":"epusdt"}`, tc.validation, "127.0.0.1")
			require.ErrorIs(t, err, tc.expectedError)

			assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, "epusdt-callback-facts"))
			assert.Equal(t, 0, getUserQuotaForPaymentGuardTest(t, 404))
		})
	}
}

func TestRechargeEpayWithValidation_RejectsMismatchedCallbackFacts(t *testing.T) {
	testCases := []struct {
		name          string
		validation    PaymentCallbackValidation
		expectedError error
	}{
		{
			name: "payment method mismatch",
			validation: PaymentCallbackValidation{
				ExpectedPaymentProvider: PaymentProviderEpay,
				ActualPaymentMethod:     "wxpay",
				PaidAmount:              9.99,
				PaidCurrency:            "CNY",
				RequirePaymentFacts:     true,
			},
			expectedError: ErrPaymentMethodMismatch,
		},
		{
			name: "amount mismatch",
			validation: PaymentCallbackValidation{
				ExpectedPaymentProvider: PaymentProviderEpay,
				ActualPaymentMethod:     PaymentProviderEpay,
				PaidAmount:              10.01,
				PaidCurrency:            "CNY",
				RequirePaymentFacts:     true,
			},
			expectedError: ErrPaymentAmountMismatch,
		},
		{
			name: "currency mismatch",
			validation: PaymentCallbackValidation{
				ExpectedPaymentProvider: PaymentProviderEpay,
				ActualPaymentMethod:     PaymentProviderEpay,
				PaidAmount:              9.99,
				PaidCurrency:            "USDT",
				RequirePaymentFacts:     true,
			},
			expectedError: ErrPaymentCurrencyMismatch,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			insertUserForPaymentGuardTest(t, 414, 0)
			insertTopUpForPaymentGuardTest(t, "epay-callback-facts", 414, PaymentProviderEpay)

			err := RechargeEpayWithValidation("epay-callback-facts", `{"provider":"epay"}`, tc.validation, "127.0.0.1")
			require.ErrorIs(t, err, tc.expectedError)

			assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, "epay-callback-facts"))
			assert.Equal(t, 0, getUserQuotaForPaymentGuardTest(t, 414))
		})
	}
}

func TestRechargeEpayWithValidation_CompletesOnce(t *testing.T) {
	truncateTables(t)
	common.QuotaPerUnit = 500000
	insertUserForPaymentGuardTest(t, 424, 0)
	insertTopUpForPaymentGuardTest(t, "epay-success-once", 424, PaymentProviderEpay)

	validation := PaymentCallbackValidation{
		ExpectedPaymentProvider: PaymentProviderEpay,
		ActualPaymentMethod:     PaymentProviderEpay,
		PaidAmount:              9.99,
		PaidCurrency:            "CNY",
		RequirePaymentFacts:     true,
	}
	require.NoError(t, RechargeEpayWithValidation("epay-success-once", `{"provider":"epay"}`, validation, "127.0.0.1"))
	require.NoError(t, RechargeEpayWithValidation("epay-success-once", `{"provider":"epay"}`, validation, "127.0.0.1"))

	assert.Equal(t, common.TopUpStatusSuccess, getTopUpStatusForPaymentGuardTest(t, "epay-success-once"))
	assert.Equal(t, 1000000, getUserQuotaForPaymentGuardTest(t, 424))
}

func TestRechargeEpayWithValidation_RejectsMissingCallbackFacts(t *testing.T) {
	truncateTables(t)
	insertUserForPaymentGuardTest(t, 434, 0)
	insertTopUpForPaymentGuardTest(t, "epay-missing-facts", 434, PaymentProviderEpay)

	err := RechargeEpayWithValidation("epay-missing-facts", `{"provider":"epay"}`, PaymentCallbackValidation{
		ExpectedPaymentProvider: PaymentProviderEpay,
		ActualPaymentMethod:     PaymentProviderEpay,
		RequirePaymentFacts:     true,
	}, "127.0.0.1")
	require.ErrorIs(t, err, ErrPaymentCurrencyMismatch)

	assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, "epay-missing-facts"))
	assert.Equal(t, 0, getUserQuotaForPaymentGuardTest(t, 434))
}

func TestExpireSubscriptionOrder_RejectsMismatchedPaymentProvider(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 303, 0)
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 401)
	insertSubscriptionOrderForPaymentGuardTest(t, "sub-expire-guard", 303, plan.Id, PaymentProviderStripe)

	err := ExpireSubscriptionOrder("sub-expire-guard", PaymentProviderCreem)
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)

	order := GetSubscriptionOrderByTradeNo("sub-expire-guard")
	require.NotNil(t, order)
	assert.Equal(t, common.TopUpStatusPending, order.Status)
}
