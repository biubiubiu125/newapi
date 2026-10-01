package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestCalcSubscriptionBalanceQuotaConvertsPlanCurrencyToWalletUSD(t *testing.T) {
	previousQuotaPerUnit := common.QuotaPerUnit
	previousRate := operation_setting.USDExchangeRate
	common.QuotaPerUnit = 500000
	operation_setting.USDExchangeRate = 7
	t.Cleanup(func() {
		common.QuotaPerUnit = previousQuotaPerUnit
		operation_setting.USDExchangeRate = previousRate
	})

	usd, err := calcSubscriptionBalanceQuota(10, "USD")
	require.NoError(t, err)
	require.Equal(t, int64(5000000), usd)

	cny, err := calcSubscriptionBalanceQuota(70, "CNY")
	require.NoError(t, err)
	require.Equal(t, int64(5000000), cny)

	emptyCurrency, err := calcSubscriptionBalanceQuota(70, "")
	require.NoError(t, err)
	require.Equal(t, usd, emptyCurrency)

	_, err = calcSubscriptionBalanceQuota(70, "EUR")
	require.Error(t, err)
	_, err = calcSubscriptionBalanceQuota(0, "EUR")
	require.Error(t, err)

	operation_setting.USDExchangeRate = 0
	_, err = calcSubscriptionBalanceQuota(70, "CNY")
	require.Error(t, err)
	usdWithoutRate, err := calcSubscriptionBalanceQuota(10, "USD")
	require.NoError(t, err)
	require.Equal(t, int64(5000000), usdWithoutRate)
}

func TestPurchaseSubscriptionWithBalanceChargesConvertedQuota(t *testing.T) {
	truncateTables(t)
	previousQuotaPerUnit := common.QuotaPerUnit
	previousRate := operation_setting.USDExchangeRate
	common.QuotaPerUnit = 500000
	operation_setting.USDExchangeRate = 7
	t.Cleanup(func() {
		common.QuotaPerUnit = previousQuotaPerUnit
		operation_setting.USDExchangeRate = previousRate
	})

	user := &User{
		Username: "balance-sub-user",
		Password: "password",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    5000000,
		AffCode:  "balance-sub-aff",
	}
	require.NoError(t, DB.Create(user).Error)
	plan := &SubscriptionPlan{
		Title:           "CNY Pro",
		PriceAmount:     70,
		Currency:        "CNY",
		DurationUnit:    SubscriptionDurationMonth,
		DurationValue:   1,
		TotalAmount:     1000,
		Enabled:         true,
		AllowBalancePay: boolPointer(true),
	}
	require.NoError(t, DB.Create(plan).Error)

	require.NoError(t, PurchaseSubscriptionWithBalance(user.Id, plan.Id))

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	require.Equal(t, int64(0), got.Quota)
	var order SubscriptionOrder
	require.NoError(t, DB.Where("user_id = ?", user.Id).First(&order).Error)
	require.Equal(t, "CNY", order.PlanCurrencySnapshot)
	require.Equal(t, "CNY", order.PaidCurrency)
	require.Equal(t, 70.0, order.Money)
	var subs int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ? AND plan_id = ?", user.Id, plan.Id).Count(&subs).Error)
	require.Equal(t, int64(1), subs)

	shortUser := &User{
		Username: "balance-sub-short",
		Password: "password",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    4999999,
		AffCode:  "balance-sub-short-aff",
	}
	require.NoError(t, DB.Create(shortUser).Error)
	require.Error(t, PurchaseSubscriptionWithBalance(shortUser.Id, plan.Id))
	var shortGot User
	require.NoError(t, DB.First(&shortGot, shortUser.Id).Error)
	require.Equal(t, int64(4999999), shortGot.Quota)
}

func TestPurchaseSubscriptionWithBalanceRejectsUnlimitedFreePlan(t *testing.T) {
	truncateTables(t)
	user := &User{
		Username: "free-plan-user",
		Password: "password",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    100,
		AffCode:  "free-plan-aff",
	}
	require.NoError(t, DB.Create(user).Error)
	unlimited := &SubscriptionPlan{
		Title:           "Free Forever",
		PriceAmount:     0,
		Currency:        "USD",
		DurationUnit:    SubscriptionDurationMonth,
		DurationValue:   1,
		TotalAmount:     1000,
		UpgradeGroup:    "vip",
		Enabled:         true,
		AllowBalancePay: boolPointer(true),
	}
	require.NoError(t, DB.Create(unlimited).Error)

	err := PurchaseSubscriptionWithBalance(user.Id, unlimited.Id)
	require.Error(t, err)
	require.ErrorContains(t, err, "subscription.purchase_max")
	var subs int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ? AND plan_id = ?", user.Id, unlimited.Id).Count(&subs).Error)
	require.Zero(t, subs)
	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	require.Equal(t, int64(100), got.Quota)
	require.Equal(t, "default", got.Group)

	capped := &SubscriptionPlan{
		Title:              "Free Once",
		PriceAmount:        0,
		Currency:           "USD",
		DurationUnit:       SubscriptionDurationMonth,
		DurationValue:      1,
		TotalAmount:        50,
		MaxPurchasePerUser: 1,
		Enabled:            true,
		AllowBalancePay:    boolPointer(true),
	}
	require.NoError(t, DB.Create(capped).Error)
	require.NoError(t, PurchaseSubscriptionWithBalance(user.Id, capped.Id))
	require.ErrorContains(t, PurchaseSubscriptionWithBalance(user.Id, capped.Id), "subscription.purchase_max")
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ? AND plan_id = ?", user.Id, capped.Id).Count(&subs).Error)
	require.EqualValues(t, 1, subs)
	require.NoError(t, DB.First(&got, user.Id).Error)
	require.Equal(t, int64(100), got.Quota)
}

func boolPointer(value bool) *bool {
	return &value
}
