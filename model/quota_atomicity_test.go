package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/require"
)

func TestDecreaseUserQuotaReturnsErrorWhenNoRowsUpdated(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{
		Id:       9301,
		Username: "wallet-owner",
		Password: "password123",
		Quota:    10,
		Status:   common.UserStatusEnabled,
	}).Error)

	err := DecreaseUserQuota(9301, 20, false)

	require.Error(t, err)
	var user User
	require.NoError(t, DB.Select("quota").First(&user, 9301).Error)
	require.Equal(t, 10, user.Quota)
}

func TestIncreaseUserQuotaIgnoresCacheRefreshFailureAfterDatabaseUpdate(t *testing.T) {
	truncateTables(t)
	oldRedisEnabled := common.RedisEnabled
	oldCacheUpdateUserQuotaField := cacheUpdateUserQuotaField
	common.RedisEnabled = true
	cacheUpdateUserQuotaField = func(userId int, quota int) error {
		return errors.New("redis unavailable")
	}
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		cacheUpdateUserQuotaField = oldCacheUpdateUserQuotaField
	})
	require.NoError(t, DB.Create(&User{
		Id:       9306,
		Username: "wallet-cache-refund-owner",
		Password: "password123",
		Quota:    10,
		Status:   common.UserStatusEnabled,
	}).Error)

	err := IncreaseUserQuota(9306, 5, false)

	require.NoError(t, err)
	var user User
	require.NoError(t, DB.Select("quota").First(&user, 9306).Error)
	require.Equal(t, 15, user.Quota)
}

func TestDecreaseUserQuotaIgnoresCacheRefreshFailureAfterDatabaseUpdate(t *testing.T) {
	truncateTables(t)
	oldRedisEnabled := common.RedisEnabled
	oldCacheUpdateUserQuotaField := cacheUpdateUserQuotaField
	common.RedisEnabled = true
	cacheUpdateUserQuotaField = func(userId int, quota int) error {
		return errors.New("redis unavailable")
	}
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		cacheUpdateUserQuotaField = oldCacheUpdateUserQuotaField
	})
	require.NoError(t, DB.Create(&User{
		Id:       9307,
		Username: "wallet-cache-charge-owner",
		Password: "password123",
		Quota:    10,
		Status:   common.UserStatusEnabled,
	}).Error)

	err := DecreaseUserQuota(9307, 4, false)

	require.NoError(t, err)
	var user User
	require.NoError(t, DB.Select("quota").First(&user, 9307).Error)
	require.Equal(t, 6, user.Quota)
}

func TestDecreaseTokenQuotaReturnsErrorAndKeepsBalanceWhenInsufficient(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&Token{
		Id:          9401,
		UserId:      9302,
		Key:         "quota-token",
		Name:        "quota-token",
		RemainQuota: 10,
		UsedQuota:   0,
		Status:      common.TokenStatusEnabled,
	}).Error)

	err := DecreaseTokenQuota(9401, "quota-token", 20)

	require.Error(t, err)
	var token Token
	require.NoError(t, DB.Select("remain_quota", "used_quota").First(&token, 9401).Error)
	require.Equal(t, 10, token.RemainQuota)
	require.Equal(t, 0, token.UsedQuota)
}

func TestIncreaseUserQuotaUpdatesDatabaseImmediatelyWhenBatchEnabled(t *testing.T) {
	truncateTables(t)
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = true
	t.Cleanup(func() {
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
	})
	require.NoError(t, DB.Create(&User{
		Id:       9303,
		Username: "wallet-refund-owner",
		Password: "password123",
		Quota:    10,
		Status:   common.UserStatusEnabled,
	}).Error)

	err := IncreaseUserQuota(9303, 5, false)

	require.NoError(t, err)
	var user User
	require.NoError(t, DB.Select("quota").First(&user, 9303).Error)
	require.Equal(t, 15, user.Quota)
}

func TestIncreaseTokenQuotaUpdatesDatabaseAfterBatchFlush(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	common.BatchUpdateEnabled = true
	require.NoError(t, DB.Create(&Token{
		Id:          9402,
		UserId:      9304,
		Key:         "quota-refund-token",
		Name:        "quota-refund-token",
		RemainQuota: 10,
		UsedQuota:   20,
		Status:      common.TokenStatusEnabled,
	}).Error)

	err := IncreaseTokenQuota(9402, "quota-refund-token", 5)

	require.NoError(t, err)
	var token Token
	require.NoError(t, DB.Select("remain_quota", "used_quota").First(&token, 9402).Error)
	require.Equal(t, 10, token.RemainQuota)
	require.Equal(t, 20, token.UsedQuota)

	batchUpdate()

	require.NoError(t, DB.Select("remain_quota", "used_quota").First(&token, 9402).Error)
	require.Equal(t, 15, token.RemainQuota)
	require.Equal(t, 15, token.UsedQuota)
}

func TestIncreaseTokenQuotaClampsUsedQuotaAtZero(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&Token{
		Id:          9404,
		UserId:      9308,
		Key:         "quota-refund-token-clamp",
		Name:        "quota-refund-token-clamp",
		RemainQuota: 10,
		UsedQuota:   3,
		Status:      common.TokenStatusEnabled,
	}).Error)

	err := IncreaseTokenQuota(9404, "quota-refund-token-clamp", 5)

	require.NoError(t, err)
	var token Token
	require.NoError(t, DB.Select("remain_quota", "used_quota").First(&token, 9404).Error)
	require.Equal(t, 15, token.RemainQuota)
	require.Equal(t, 0, token.UsedQuota)
}

func TestIncreaseTokenQuotaKeepsUnlimitedRemainQuota(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&Token{
		Id:             9403,
		UserId:         9305,
		Key:            "quota-unlimited-token",
		Name:           "quota-unlimited-token",
		RemainQuota:    10,
		UsedQuota:      20,
		UnlimitedQuota: true,
		Status:         common.TokenStatusEnabled,
	}).Error)

	err := IncreaseTokenQuota(9403, "quota-unlimited-token", 5)

	require.NoError(t, err)
	var token Token
	require.NoError(t, DB.Select("remain_quota", "used_quota").First(&token, 9403).Error)
	require.Equal(t, 10, token.RemainQuota)
	require.Equal(t, 15, token.UsedQuota)
}

func TestIncreaseTokenQuotaTrackedRollbackOnlyReversesItsOwnDelta(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&Token{
		Id:          9405,
		UserId:      9309,
		Key:         "quota-tracked-refund-token",
		Name:        "quota-tracked-refund-token",
		RemainQuota: 100,
		UsedQuota:   50,
		Status:      common.TokenStatusEnabled,
	}).Error)

	delta, err := IncreaseTokenQuotaTracked(9405, "quota-tracked-refund-token", 30)
	require.NoError(t, err)
	require.NoError(t, DecreaseTokenQuota(9405, "quota-tracked-refund-token", 20))
	require.NoError(t, ApplyTokenQuotaDelta(TokenQuotaDelta{
		TokenId:     delta.TokenId,
		Key:         delta.Key,
		RemainDelta: -delta.RemainDelta,
		UsedDelta:   -delta.UsedDelta,
	}))

	var token Token
	require.NoError(t, DB.Select("remain_quota", "used_quota").First(&token, 9405).Error)
	require.Equal(t, 80, token.RemainQuota)
	require.Equal(t, 70, token.UsedQuota)
}

func TestIncreaseTokenQuotaTrackedReportsClampedUsedQuotaDelta(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&Token{
		Id:          9406,
		UserId:      9309,
		Key:         "quota-tracked-clamp-token",
		Name:        "quota-tracked-clamp-token",
		RemainQuota: 100,
		UsedQuota:   10,
		Status:      common.TokenStatusEnabled,
	}).Error)

	delta, err := IncreaseTokenQuotaTracked(9406, "quota-tracked-clamp-token", 30)
	require.NoError(t, err)

	require.Equal(t, 30, delta.RemainDelta)
	require.Equal(t, -10, delta.UsedDelta)
	var token Token
	require.NoError(t, DB.Select("remain_quota", "used_quota").First(&token, 9406).Error)
	require.Equal(t, 130, token.RemainQuota)
	require.Equal(t, 0, token.UsedQuota)
}

func TestUpdateUserAndChannelUsedQuotaSyncUpdatesBothCounters(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{
		Id:        9310,
		Username:  "usage-counter-owner",
		Password:  "password123",
		UsedQuota: 100,
		Status:    common.UserStatusEnabled,
	}).Error)
	require.NoError(t, DB.Create(&Channel{
		Id:        9410,
		Name:      "usage-counter-channel",
		Key:       "sk-channel",
		UsedQuota: 100,
		Status:    common.ChannelStatusEnabled,
	}).Error)

	err := UpdateUserAndChannelUsedQuotaSync(9310, 9410, -40)

	require.NoError(t, err)
	var user User
	require.NoError(t, DB.Select("used_quota").First(&user, 9310).Error)
	require.Equal(t, 60, user.UsedQuota)
	var channel Channel
	require.NoError(t, DB.Select("used_quota").First(&channel, 9410).Error)
	require.EqualValues(t, 60, channel.UsedQuota)
}

func TestUpdateUserAndChannelUsedQuotaSyncRollsBackWhenChannelMissing(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{
		Id:        9311,
		Username:  "usage-counter-rollback-owner",
		Password:  "password123",
		UsedQuota: 100,
		Status:    common.UserStatusEnabled,
	}).Error)

	err := UpdateUserAndChannelUsedQuotaSync(9311, 9411, -40)

	require.Error(t, err)
	var user User
	require.NoError(t, DB.Select("used_quota").First(&user, 9311).Error)
	require.Equal(t, 100, user.UsedQuota)
}

func TestUpdateTaskConsumptionUsageSyncUpdatesUsageAndRequestCount(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{
		Id:           9312,
		Username:     "task-consumption-owner",
		Password:     "password123",
		UsedQuota:    50,
		RequestCount: 2,
		Status:       common.UserStatusEnabled,
	}).Error)
	require.NoError(t, DB.Create(&Channel{
		Id:        9412,
		Name:      "task-consumption-channel",
		Key:       "sk-channel",
		UsedQuota: 70,
		Status:    common.ChannelStatusEnabled,
	}).Error)

	err := UpdateTaskConsumptionUsageSync(9312, 9412, 30)

	require.NoError(t, err)
	var user User
	require.NoError(t, DB.Select("used_quota", "request_count").First(&user, 9312).Error)
	require.Equal(t, 80, user.UsedQuota)
	require.Equal(t, 3, user.RequestCount)
	var channel Channel
	require.NoError(t, DB.Select("used_quota").First(&channel, 9412).Error)
	require.EqualValues(t, 100, channel.UsedQuota)
}

func TestUpdateTaskConsumptionUsageSyncRollsBackWhenChannelMissing(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{
		Id:           9313,
		Username:     "task-consumption-rollback-owner",
		Password:     "password123",
		UsedQuota:    50,
		RequestCount: 2,
		Status:       common.UserStatusEnabled,
	}).Error)

	err := UpdateTaskConsumptionUsageSync(9313, 9413, 30)

	require.Error(t, err)
	var user User
	require.NoError(t, DB.Select("used_quota", "request_count").First(&user, 9313).Error)
	require.Equal(t, 50, user.UsedQuota)
	require.Equal(t, 2, user.RequestCount)
}

func TestUpdateTaskConsumptionUsageWithTokenSyncUpdatesUsageRequestCountAndTokenUsageDaily(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{
		Id:           9314,
		Username:     "task-token-consumption-owner",
		Password:     "password123",
		UsedQuota:    50,
		RequestCount: 2,
		Status:       common.UserStatusEnabled,
	}).Error)
	require.NoError(t, DB.Create(&Channel{
		Id:        9414,
		Name:      "task-token-consumption-channel",
		Key:       "sk-channel",
		UsedQuota: 70,
		Status:    common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, DB.Create(&Token{
		Id:          9514,
		UserId:      9314,
		Key:         "task-token-consumption-key",
		Name:        "task-token-consumption-key",
		RemainQuota: 100,
		UsedQuota:   5,
		Status:      common.TokenStatusEnabled,
	}).Error)

	err := UpdateTaskConsumptionUsageWithTokenSync(9314, 9414, 9514, 30)

	require.NoError(t, err)
	var user User
	require.NoError(t, DB.Select("used_quota", "request_count").First(&user, 9314).Error)
	require.Equal(t, 80, user.UsedQuota)
	require.Equal(t, 3, user.RequestCount)
	var channel Channel
	require.NoError(t, DB.Select("used_quota").First(&channel, 9414).Error)
	require.EqualValues(t, 100, channel.UsedQuota)
	var usage TokenUsageDaily
	require.NoError(t, DB.Where("token_id = ?", 9514).First(&usage).Error)
	require.Equal(t, 30, usage.Quota)
	require.Equal(t, 1, usage.RequestCount)
	require.Equal(t, 9314, usage.UserId)
}

func TestUpdateTaskUsageAdjustmentWithTokenSyncUpdatesUsageAndTokenUsageDailyWithoutRequestCount(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{
		Id:           9315,
		Username:     "task-token-adjust-owner",
		Password:     "password123",
		UsedQuota:    80,
		RequestCount: 3,
		Status:       common.UserStatusEnabled,
	}).Error)
	require.NoError(t, DB.Create(&Channel{
		Id:        9415,
		Name:      "task-token-adjust-channel",
		Key:       "sk-channel",
		UsedQuota: 100,
		Status:    common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, DB.Create(&Token{
		Id:          9515,
		UserId:      9315,
		Key:         "task-token-adjust-key",
		Name:        "task-token-adjust-key",
		RemainQuota: 100,
		UsedQuota:   5,
		Status:      common.TokenStatusEnabled,
	}).Error)

	err := UpdateTaskUsageAdjustmentWithTokenSync(9315, 9415, 9515, -40)

	require.NoError(t, err)
	var user User
	require.NoError(t, DB.Select("used_quota", "request_count").First(&user, 9315).Error)
	require.Equal(t, 40, user.UsedQuota)
	require.Equal(t, 3, user.RequestCount)
	var channel Channel
	require.NoError(t, DB.Select("used_quota").First(&channel, 9415).Error)
	require.EqualValues(t, 60, channel.UsedQuota)
	var usage TokenUsageDaily
	require.NoError(t, DB.Where("token_id = ?", 9515).First(&usage).Error)
	require.Equal(t, -40, usage.Quota)
	require.Equal(t, 0, usage.RequestCount)
	require.Equal(t, 9315, usage.UserId)
}

func TestUpdateTaskConsumptionUsageWithTokenSyncRollsBackWhenChannelMissing(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{
		Id:           9316,
		Username:     "task-token-rollback-owner",
		Password:     "password123",
		UsedQuota:    50,
		RequestCount: 2,
		Status:       common.UserStatusEnabled,
	}).Error)
	require.NoError(t, DB.Create(&Token{
		Id:          9516,
		UserId:      9316,
		Key:         "task-token-rollback-key",
		Name:        "task-token-rollback-key",
		RemainQuota: 100,
		UsedQuota:   5,
		Status:      common.TokenStatusEnabled,
	}).Error)

	err := UpdateTaskConsumptionUsageWithTokenSync(9316, 9416, 9516, 30)

	require.Error(t, err)
	var user User
	require.NoError(t, DB.Select("used_quota", "request_count").First(&user, 9316).Error)
	require.Equal(t, 50, user.UsedQuota)
	require.Equal(t, 2, user.RequestCount)
	var usage TokenUsageDaily
	require.Error(t, DB.Where("token_id = ?", 9516).First(&usage).Error)
}
