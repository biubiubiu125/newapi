package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestEpayAndBEpusdtCallbacksCreditRedisQuotaOnce(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)

	seedCachedTopUpUser := func(id int, name string) {
		user := User{
			Id:          id,
			Username:    name,
			Status:      common.UserStatusEnabled,
			Role:        common.RoleCommonUser,
			Group:       "default",
			AuthVersion: 1,
			Quota:       0,
		}
		require.NoError(t, DB.Create(&user).Error)
		require.NoError(t, populateUserCache(user))
	}
	seedOrder := func(tradeNo string, userID int, provider string, method string) {
		topUp := &TopUp{
			UserId:              userID,
			Amount:              2,
			Money:               9.99,
			PaidAmount:          9.99,
			PaidCurrency:        "CNY",
			CreditQuotaSnapshot: 1000,
			TradeNo:             tradeNo,
			PaymentMethod:       method,
			PaymentProvider:     provider,
			Status:              common.TopUpStatusPending,
			CreateTime:          time.Now().Unix(),
		}
		require.NoError(t, topUp.Insert())
	}
	cachedQuota := func(userID int) string {
		value, err := common.RDB.HGet(t.Context(), getUserCacheKey(userID), "Quota").Result()
		require.NoError(t, err)
		return value
	}

	seedCachedTopUpUser(99001, "epay_cache_user")
	seedOrder("epay-cache-once", 99001, PaymentProviderEpay, "alipay")
	epayValidation := PaymentCallbackValidation{
		ExpectedPaymentProvider: PaymentProviderEpay,
		ActualPaymentMethod:     "alipay",
		PaidAmount:              9.99,
		PaidCurrency:            "CNY",
		RequirePaymentFacts:     true,
	}
	require.NoError(t, RechargeEpayWithValidation("epay-cache-once", `{"provider":"epay"}`, epayValidation, "127.0.0.1"))
	require.NoError(t, RechargeEpayWithValidation("epay-cache-once", `{"provider":"epay"}`, epayValidation, "127.0.0.1"))
	require.EqualValues(t, 1000, getUserQuotaForPaymentGuardTest(t, 99001))
	require.Equal(t, "1000", cachedQuota(99001))

	seedCachedTopUpUser(99002, "bepusdt_cache_user")
	seedOrder("bepusdt-cache-once", 99002, PaymentProviderBEpusdt, PaymentMethodUSDT)
	bepusdtValidation := PaymentCallbackValidation{
		ExpectedPaymentProvider: PaymentProviderBEpusdt,
		ActualPaymentMethod:     "usdt",
		ActualPaymentToken:      "USDT",
		PaidAmount:              9.99,
		PaidCurrency:            "CNY",
		RequirePaymentFacts:     true,
	}
	require.NoError(t, RechargeBEpusdtWithValidation("bepusdt-cache-once", `{"provider":"bepusdt"}`, bepusdtValidation, "127.0.0.1"))
	require.NoError(t, RechargeBEpusdtWithValidation("bepusdt-cache-once", `{"provider":"bepusdt"}`, bepusdtValidation, "127.0.0.1"))
	require.EqualValues(t, 1000, getUserQuotaForPaymentGuardTest(t, 99002))
	require.Equal(t, "1000", cachedQuota(99002))
}
