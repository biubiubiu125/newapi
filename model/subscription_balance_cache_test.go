package model

import (
	"context"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

type redisCommandHook struct {
	reject func(name string) bool
}

func (h redisCommandHook) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	if h.reject != nil && h.reject(cmd.Name()) {
		return ctx, errors.New("redis command failed")
	}
	return ctx, nil
}

func (h redisCommandHook) AfterProcess(context.Context, redis.Cmder) error { return nil }

func (h redisCommandHook) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (h redisCommandHook) AfterProcessPipeline(context.Context, []redis.Cmder) error { return nil }

func rejectRedisCommands(t *testing.T, names ...string) {
	t.Helper()
	blocked := map[string]bool{}
	for _, name := range names {
		blocked[name] = true
	}
	common.RDB.AddHook(redisCommandHook{reject: func(name string) bool {
		return blocked[name]
	}})
}

func newBalanceSubscriptionFixture(t *testing.T, username string, quota int64) (*User, *SubscriptionPlan) {
	t.Helper()
	user := &User{
		Username:    username,
		Password:    "password",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		Quota:       quota,
		AffCode:     username + "-aff",
		AuthVersion: 1,
	}
	require.NoError(t, DB.Create(user).Error)
	plan := &SubscriptionPlan{
		Title:           "USD Cache",
		PriceAmount:     10,
		Currency:        "USD",
		DurationUnit:    SubscriptionDurationMonth,
		DurationValue:   1,
		TotalAmount:     1000,
		Enabled:         true,
		AllowBalancePay: boolPointer(true),
	}
	require.NoError(t, DB.Create(plan).Error)
	require.NoError(t, populateUserCache(*user))
	return user, plan
}

func useBalanceSubscriptionPricing(t *testing.T) {
	t.Helper()
	previousQuotaPerUnit := common.QuotaPerUnit
	previousRate := operation_setting.USDExchangeRate
	common.QuotaPerUnit = 500000
	operation_setting.USDExchangeRate = 7
	t.Cleanup(func() {
		common.QuotaPerUnit = previousQuotaPerUnit
		operation_setting.USDExchangeRate = previousRate
	})
}

func TestPurchaseSubscriptionWithBalanceUpdatesLiveQuotaCache(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	useBalanceSubscriptionPricing(t)
	user, plan := newBalanceSubscriptionFixture(t, "balance-cache-live", 5_000_000)

	require.NoError(t, PurchaseSubscriptionWithBalance(user.Id, plan.Id))

	cached, err := GetUserQuota(user.Id, false)
	require.NoError(t, err)
	require.Equal(t, int64(0), cached)
}

func TestPurchaseSubscriptionWithBalanceDropsStaleQuotaCacheWhenDecrementFails(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	useBalanceSubscriptionPricing(t)
	user, plan := newBalanceSubscriptionFixture(t, "balance-cache-drop", 5_000_000)
	rejectRedisCommands(t, "eval")

	require.NoError(t, PurchaseSubscriptionWithBalance(user.Id, plan.Id))

	var got User
	require.NoError(t, DB.Select("quota").First(&got, user.Id).Error)
	require.Equal(t, int64(0), got.Quota)
	cached, err := GetUserQuota(user.Id, false)
	require.NoError(t, err)
	require.Equal(t, int64(0), cached)
}

func TestPurchaseSubscriptionWithBalanceRollsBackWhenQuotaCacheCannotBeCleared(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	useBalanceSubscriptionPricing(t)
	user, plan := newBalanceSubscriptionFixture(t, "balance-cache-rollback", 5_000_000)
	rejectRedisCommands(t, "eval", "del")

	require.Error(t, PurchaseSubscriptionWithBalance(user.Id, plan.Id))

	var got User
	require.NoError(t, DB.Select("quota").First(&got, user.Id).Error)
	require.Equal(t, int64(5_000_000), got.Quota)
	var subs int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", user.Id).Count(&subs).Error)
	require.Equal(t, int64(0), subs)
	cached, err := GetUserQuota(user.Id, false)
	require.NoError(t, err)
	require.Equal(t, int64(5_000_000), cached)
}
