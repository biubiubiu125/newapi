package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestPositiveQuotaCacheCreditCannotExceedDatabaseBalance(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t)

	user := User{
		Username:    "quota-credit-clamp",
		Password:    "password",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		Quota:       110,
		AuthVersion: 1,
	}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, populateUserCache(user))

	require.NoError(t, cacheIncrUserQuota(user.Id, 10))

	cached, err := common.RDB.HGet(t.Context(), getUserCacheKey(user.Id), "Quota").Result()
	require.NoError(t, err)
	require.Equal(t, "110", cached)
	require.True(t, server.Exists(getUserCacheKey(user.Id)))
}

func TestPositiveQuotaCacheCreditDoesNotCreatePartialHash(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)

	user := User{
		Username:    "quota-credit-miss",
		Password:    "password",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		Quota:       40,
		AuthVersion: 1,
	}
	require.NoError(t, DB.Create(&user).Error)

	require.NoError(t, cacheIncrUserQuota(user.Id, 10))
	exists, err := common.RDB.Exists(t.Context(), getUserCacheKey(user.Id)).Result()
	require.NoError(t, err)
	require.EqualValues(t, 0, exists)
}

func TestStaleQuotaCreditCeilingDoesNotRaiseCache(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)

	user := User{
		Username:    "quota-credit-stale-ceiling",
		Password:    "password",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		Quota:       95,
		AuthVersion: 1,
	}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, populateUserCache(user))

	// The credit read the balance as 100, then a debit committed at 95
	// before this script ran with the stale ceiling.
	require.NoError(t, cacheCreditUserQuota(user.Id, 10, 100))

	cached, err := common.RDB.HGet(t.Context(), getUserCacheKey(user.Id), "Quota").Result()
	require.NoError(t, err)
	require.Equal(t, "95", cached)
}

func TestQuotaCacheCreditAppliesWhenCacheMatchesPreCreditBalance(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)

	user := User{
		Username:    "quota-credit-exact",
		Password:    "password",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		Quota:       100,
		AuthVersion: 1,
	}
	require.NoError(t, DB.Create(&user).Error)
	cachedUser := user
	cachedUser.Quota = 90
	require.NoError(t, populateUserCache(cachedUser))

	require.NoError(t, cacheIncrUserQuota(user.Id, 10))

	cached, err := common.RDB.HGet(t.Context(), getUserCacheKey(user.Id), "Quota").Result()
	require.NoError(t, err)
	require.Equal(t, "100", cached)
}

func TestNegativeQuotaCacheDeltaStaysUnclamped(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)

	user := User{
		Username:    "quota-debit-unclamped",
		Password:    "password",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		Quota:       100,
		AuthVersion: 1,
	}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, populateUserCache(user))

	require.NoError(t, cacheIncrUserQuota(user.Id, -30))

	cached, err := common.RDB.HGet(t.Context(), getUserCacheKey(user.Id), "Quota").Result()
	require.NoError(t, err)
	require.Equal(t, "70", cached)
}
