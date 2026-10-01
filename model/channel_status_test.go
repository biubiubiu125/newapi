package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelStatusTest(t *testing.T) {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)

	memoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = memoryCacheEnabled
	})
}

func TestUpdateChannelStatusMatchesTrimmedMultiKey(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:   "crlf-keys",
		Key:    "key-a\r\nkey-b\r",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
			MultiKeyMode: constant.MultiKeyModePolling,
		},
	}
	require.NoError(t, DB.Create(&channel).Error)
	assert.Equal(t, []string{"key-a", "key-b"}, channel.GetKeys())

	key, index, apiErr := channel.ResolveReusableKey("key-b")
	require.Nil(t, apiErr)
	assert.Equal(t, "key-b", key)
	assert.Equal(t, 1, index)

	changed := UpdateChannelStatus(channel.Id, "key-b", common.ChannelStatusAutoDisabled, "provider rejected key")
	require.True(t, changed)
	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[1])
	assert.NotContains(t, stored.ChannelInfo.MultiKeyStatusList, 0)
}

func TestUpdateChannelStatusPersistsMultiKeyState(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:   "multi-key-status",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: 1,
		},
	}
	require.NoError(t, DB.Create(&channel).Error)

	changed := UpdateChannelStatus(channel.Id, "key-a", common.ChannelStatusAutoDisabled, "provider rejected key")
	require.True(t, changed)

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[0])
	assert.Equal(t, "provider rejected key", stored.ChannelInfo.MultiKeyDisabledReason[0])
	assert.NotZero(t, stored.ChannelInfo.MultiKeyDisabledTime[0])
	assert.Equal(t, 1, stored.ChannelInfo.MultiKeyPollingIndex)
}

func TestUpdateChannelStatusKeepsManualDisable(t *testing.T) {
	setupChannelStatusTest(t)

	single := Channel{
		Name:   "manual-single",
		Key:    "only-key",
		Status: common.ChannelStatusManuallyDisabled,
	}
	require.NoError(t, DB.Create(&single).Error)
	require.False(t, UpdateChannelStatus(single.Id, "", common.ChannelStatusAutoDisabled, "upstream rejected"))
	var storedSingle Channel
	require.NoError(t, DB.First(&storedSingle, single.Id).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, storedSingle.Status)
	assert.Empty(t, storedSingle.GetOtherInfo()["status_reason"])

	previousCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	InitChannelCache()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousCache
		InitChannelCache()
	})
	cached, err := CacheGetChannel(single.Id)
	require.NoError(t, err)
	require.NotNil(t, cached)
	require.False(t, UpdateChannelStatus(single.Id, "", common.ChannelStatusAutoDisabled, "upstream rejected"))
	assert.Equal(t, common.ChannelStatusManuallyDisabled, cached.Status)
	require.NoError(t, DB.First(&storedSingle, single.Id).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, storedSingle.Status)

	multi := Channel{
		Name:   "manual-multi",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusManuallyDisabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
			MultiKeyMode: constant.MultiKeyModePolling,
			MultiKeyStatusList: map[int]int{
				1: common.ChannelStatusAutoDisabled,
			},
		},
	}
	require.NoError(t, DB.Create(&multi).Error)
	require.True(t, UpdateChannelStatus(multi.Id, "key-a", common.ChannelStatusAutoDisabled, "last key rejected"))
	var storedMulti Channel
	require.NoError(t, DB.First(&storedMulti, multi.Id).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, storedMulti.Status)
	assert.Equal(t, common.ChannelStatusAutoDisabled, storedMulti.ChannelInfo.MultiKeyStatusList[0])
	assert.Empty(t, storedMulti.GetOtherInfo()["status_reason"])
}

func TestUpdateChannelStatusEnableKeyDoesNotReopenManualDisable(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:   "manual-enable-key",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusManuallyDisabled,
		Models: "manual-enable-key",
		Group:  "default",
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
			MultiKeyMode: constant.MultiKeyModePolling,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusAutoDisabled,
			},
		},
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, channel.AddAbilities(nil))

	require.False(t, UpdateChannelStatus(channel.Id, "key-a", common.ChannelStatusEnabled, ""))
	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, stored.Status)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[0])
	var ability Ability
	require.NoError(t, DB.Where("channel_id = ? AND model = ?", channel.Id, "manual-enable-key").First(&ability).Error)
	assert.False(t, ability.Enabled)

	single := Channel{
		Name:   "manual-enable-single",
		Key:    "only-key",
		Status: common.ChannelStatusManuallyDisabled,
		Models: "manual-enable-single",
		Group:  "default",
	}
	require.NoError(t, DB.Create(&single).Error)
	require.NoError(t, single.AddAbilities(nil))
	require.False(t, UpdateChannelStatus(single.Id, "only-key", common.ChannelStatusEnabled, ""))
	var storedSingle Channel
	require.NoError(t, DB.First(&storedSingle, single.Id).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, storedSingle.Status)

	require.True(t, UpdateChannelStatus(single.Id, "", common.ChannelStatusEnabled, "manual operation"))
	require.NoError(t, DB.First(&storedSingle, single.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, storedSingle.Status)

	previousCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	InitChannelCache()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousCache
		InitChannelCache()
	})
	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.NotNil(t, cached)
	require.False(t, UpdateChannelStatus(channel.Id, "key-a", common.ChannelStatusEnabled, ""))
	assert.Equal(t, common.ChannelStatusManuallyDisabled, cached.Status)
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, stored.Status)
	require.NoError(t, DB.Where("channel_id = ? AND model = ?", channel.Id, "manual-enable-key").First(&ability).Error)
	assert.False(t, ability.Enabled)
}

func TestUpdateChannelStatusEnableKeyReopensAutoDisabledChannel(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:   "auto-enable-key",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusAutoDisabled,
		Models: "auto-enable-key",
		Group:  "default",
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
			MultiKeyMode: constant.MultiKeyModeRandom,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusAutoDisabled,
				1: common.ChannelStatusAutoDisabled,
			},
		},
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, channel.AddAbilities(nil))

	require.True(t, UpdateChannelStatus(channel.Id, "key-a", common.ChannelStatusEnabled, ""))
	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	assert.NotContains(t, stored.ChannelInfo.MultiKeyStatusList, 0)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[1])
	var ability Ability
	require.NoError(t, DB.Where("channel_id = ? AND model = ?", channel.Id, "auto-enable-key").First(&ability).Error)
	assert.True(t, ability.Enabled)
}

func TestUpdateChannelStatusPersistsKeyDisableWhenChannelAlreadyAutoDisabled(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:   "already-auto",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusAutoDisabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
			MultiKeyMode: constant.MultiKeyModePolling,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusAutoDisabled,
			},
		},
	}
	require.NoError(t, DB.Create(&channel).Error)

	require.True(t, UpdateChannelStatus(channel.Id, "key-b", common.ChannelStatusAutoDisabled, "second key rejected"))
	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.Status)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[0])
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[1])
	assert.Equal(t, "second key rejected", stored.ChannelInfo.MultiKeyDisabledReason[1])
}

func TestUpdateChannelStatusRefreshesReasonWithoutOverwritingOwnedColumns(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:   "refresh-status-reason",
		Key:    "original-key",
		Status: common.ChannelStatusAutoDisabled,
		Models: "original-model",
	}
	require.NoError(t, DB.Create(&channel).Error)

	changed := UpdateChannelStatus(channel.Id, "", common.ChannelStatusAutoDisabled, "still unavailable")
	require.True(t, changed)

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.Status)
	assert.Equal(t, "original-key", stored.Key)
	assert.Equal(t, "original-model", stored.Models)
	otherInfo := stored.GetOtherInfo()
	assert.Equal(t, "still unavailable", otherInfo["status_reason"])
	assert.NotZero(t, otherInfo["status_time"])
}

func TestSaveStatusStateFromSingleKeySnapshotPreservesUnownedColumns(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:        "single-key-status",
		Key:         "original-key",
		Status:      common.ChannelStatusEnabled,
		Models:      "original-model",
		Group:       "default",
		UsedQuota:   100,
		ChannelInfo: ChannelInfo{},
	}
	require.NoError(t, DB.Create(&channel).Error)

	stale, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)

	concurrentChannelInfo := ChannelInfo{
		IsMultiKey:           true,
		MultiKeySize:         2,
		MultiKeyMode:         constant.MultiKeyModePolling,
		MultiKeyPollingIndex: 1,
	}
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Updates(map[string]any{
		"key":          "rotated-key",
		"used_quota":   gorm.Expr("used_quota + ?", 250),
		"models":       "concurrent-model",
		"channel_info": concurrentChannelInfo,
	}).Error)

	stale.Status = common.ChannelStatusManuallyDisabled
	stale.SetOtherInfo(map[string]interface{}{
		"status_reason": "manual operation",
		"status_time":   int64(1234),
	})
	require.NoError(t, stale.saveStatusState())

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, stored.Status)
	assert.Equal(t, "rotated-key", stored.Key)
	assert.Equal(t, int64(350), stored.UsedQuota)
	assert.Equal(t, "concurrent-model", stored.Models)
	assert.Equal(t, concurrentChannelInfo, stored.ChannelInfo)

	otherInfo := stored.GetOtherInfo()
	assert.Equal(t, "manual operation", otherInfo["status_reason"])
	assert.Equal(t, float64(1234), otherInfo["status_time"])
}

func TestSaveMultiKeyStatusKeepsPollingIndexAdvancedByAnotherWriter(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:   "polling-cursor-race",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: 1,
		},
	}
	require.NoError(t, DB.Create(&channel).Error)

	stale := channel
	stale.ChannelInfo.MultiKeyPollingIndex = 0
	stale.ChannelInfo.MultiKeyStatusList = map[int]int{
		0: common.ChannelStatusAutoDisabled,
	}
	stale.ChannelInfo.MultiKeyDisabledReason = map[int]string{
		0: "provider rejected key",
	}
	stale.ChannelInfo.MultiKeyDisabledTime = map[int]int64{
		0: 111,
	}
	require.NoError(t, stale.saveStatusState())

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[0])
	assert.Equal(t, "provider rejected key", stored.ChannelInfo.MultiKeyDisabledReason[0])
	assert.Equal(t, int64(111), stored.ChannelInfo.MultiKeyDisabledTime[0])
	assert.Equal(t, 1, stored.ChannelInfo.MultiKeyPollingIndex)
}

func TestSaveMultiKeyStatusKeepsAnotherWritersKeyDisable(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:   "two-key-status-race",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: 1,
		},
	}
	require.NoError(t, DB.Create(&channel).Error)

	first := channel
	first.ChannelInfo.MultiKeyStatusList = map[int]int{1: common.ChannelStatusAutoDisabled}
	first.ChannelInfo.MultiKeyDisabledReason = map[int]string{1: "provider rejected key"}
	first.ChannelInfo.MultiKeyDisabledTime = map[int]int64{1: 222}
	require.NoError(t, first.saveStatusState())

	stale := channel
	stale.ChannelInfo.MultiKeyPollingIndex = 0
	stale.ChannelInfo.MultiKeyStatusList = map[int]int{0: common.ChannelStatusAutoDisabled}
	stale.ChannelInfo.MultiKeyDisabledReason = map[int]string{0: "provider rejected key"}
	stale.ChannelInfo.MultiKeyDisabledTime = map[int]int64{0: 111}
	require.NoError(t, stale.saveStatusState())

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[0])
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[1])
	assert.Equal(t, "provider rejected key", stored.ChannelInfo.MultiKeyDisabledReason[0])
	assert.Equal(t, "provider rejected key", stored.ChannelInfo.MultiKeyDisabledReason[1])
	assert.Equal(t, int64(111), stored.ChannelInfo.MultiKeyDisabledTime[0])
	assert.Equal(t, int64(222), stored.ChannelInfo.MultiKeyDisabledTime[1])
	assert.Equal(t, 1, stored.ChannelInfo.MultiKeyPollingIndex)
}

func TestSaveKeyManagementStateKeepsConcurrentDisableAndCursor(t *testing.T) {
	setupChannelStatusTest(t)

	channel := &Channel{
		Name:   "admin-key-race",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		Models: "admin-key-race",
		Group:  "default",
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: 1,
			MultiKeyStatusList:   map[int]int{1: common.ChannelStatusAutoDisabled},
			MultiKeyDisabledReason: map[int]string{
				1: "provider rejected key",
			},
			MultiKeyDisabledTime: map[int]int64{1: 222},
		},
	}
	require.NoError(t, DB.Create(channel).Error)

	stale := *channel
	stale.ChannelInfo.MultiKeyPollingIndex = 0
	stale.ChannelInfo.MultiKeyStatusList = map[int]int{0: common.ChannelStatusManuallyDisabled}
	stale.ChannelInfo.MultiKeyDisabledReason = map[int]string{0: "manual disable"}
	stale.ChannelInfo.MultiKeyDisabledTime = map[int]int64{0: 111}
	require.NoError(t, stale.SaveKeyManagementState(false))

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, stored.ChannelInfo.MultiKeyStatusList[0])
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[1])
	assert.Equal(t, "manual disable", stored.ChannelInfo.MultiKeyDisabledReason[0])
	assert.Equal(t, "provider rejected key", stored.ChannelInfo.MultiKeyDisabledReason[1])
	assert.Equal(t, 1, stored.ChannelInfo.MultiKeyPollingIndex)
}

func TestUpdateChannelStatusWritesDatabaseWhenCacheAlreadyHasStatus(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:   "cache-ahead",
		Key:    "only-key",
		Status: common.ChannelStatusAutoDisabled,
		Models: "cache-ahead",
		Group:  "default",
	}
	require.NoError(t, channel.Insert())
	require.NoError(t, DB.Model(&Ability{}).Where("channel_id = ?", channel.Id).Update("enabled", false).Error)

	common.MemoryCacheEnabled = true
	InitChannelCache()
	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.NotNil(t, cached)
	cached.Status = common.ChannelStatusEnabled

	require.True(t, UpdateChannelStatus(channel.Id, "", common.ChannelStatusEnabled, ""))

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	var ability Ability
	require.NoError(t, DB.Where("channel_id = ? AND model = ?", channel.Id, "cache-ahead").First(&ability).Error)
	assert.True(t, ability.Enabled)
}

func TestGetNextEnabledKeyPollingReadsDatabaseStatusUnderMemoryCache(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:   "stale-poll-cache",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		Models: "stale-poll-cache",
		Group:  "default",
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: 0,
		},
	}
	require.NoError(t, DB.Create(&channel).Error)

	common.MemoryCacheEnabled = true
	InitChannelCache()
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Update("channel_info", ChannelInfo{
		IsMultiKey:           true,
		MultiKeySize:         2,
		MultiKeyMode:         constant.MultiKeyModePolling,
		MultiKeyPollingIndex: 0,
		MultiKeyStatusList:   map[int]int{0: common.ChannelStatusAutoDisabled},
	}).Error)

	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.NotContains(t, cached.ChannelInfo.MultiKeyStatusList, 0)

	key, idx, apiErr := cached.GetNextEnabledKey()
	require.Nil(t, apiErr)
	assert.Equal(t, "key-b", key)
	assert.Equal(t, 1, idx)
}

func TestGetNextEnabledKeyRandomReadsDatabaseStatusUnderMemoryCache(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:   "stale-random-cache",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		Models: "stale-random-cache",
		Group:  "default",
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
			MultiKeyMode: constant.MultiKeyModeRandom,
		},
	}
	require.NoError(t, DB.Create(&channel).Error)

	common.MemoryCacheEnabled = true
	InitChannelCache()
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Update("channel_info", ChannelInfo{
		IsMultiKey:         true,
		MultiKeySize:       2,
		MultiKeyMode:       constant.MultiKeyModeRandom,
		MultiKeyStatusList: map[int]int{0: common.ChannelStatusAutoDisabled},
	}).Error)

	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.NotContains(t, cached.ChannelInfo.MultiKeyStatusList, 0)

	for range 20 {
		key, idx, apiErr := cached.GetNextEnabledKey()
		require.Nil(t, apiErr)
		assert.Equal(t, "key-b", key)
		assert.Equal(t, 1, idx)
	}
}

func TestGetNextEnabledKeyPollingReadsDatabaseWhenCacheShowsNoEnabledKey(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:   "cache-shows-none",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		Models: "cache-shows-none",
		Group:  "default",
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: 0,
		},
	}
	require.NoError(t, DB.Create(&channel).Error)

	common.MemoryCacheEnabled = true
	InitChannelCache()
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Update("channel_info", ChannelInfo{
		IsMultiKey:           true,
		MultiKeySize:         2,
		MultiKeyMode:         constant.MultiKeyModePolling,
		MultiKeyPollingIndex: 0,
		MultiKeyStatusList:   map[int]int{0: common.ChannelStatusAutoDisabled},
	}).Error)

	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	cached.ChannelInfo.MultiKeyStatusList = map[int]int{
		0: common.ChannelStatusAutoDisabled,
		1: common.ChannelStatusAutoDisabled,
	}

	key, idx, apiErr := cached.GetNextEnabledKey()
	require.Nil(t, apiErr)
	assert.Equal(t, "key-b", key)
	assert.Equal(t, 1, idx)
}

func TestSaveKeyManagementStateClearIndexKeepsOtherDisable(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:   "clear-one-key",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusAutoDisabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
			MultiKeyMode: constant.MultiKeyModePolling,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusManuallyDisabled,
				1: common.ChannelStatusAutoDisabled,
			},
			MultiKeyDisabledReason: map[int]string{
				0: "manual disable",
				1: "provider rejected key",
			},
			MultiKeyDisabledTime: map[int]int64{0: 111, 1: 222},
			MultiKeyPollingIndex: 1,
		},
	}
	require.NoError(t, DB.Create(&channel).Error)

	stale := channel
	stale.Status = common.ChannelStatusEnabled
	stale.ChannelInfo.MultiKeyStatusList = nil
	stale.ChannelInfo.MultiKeyDisabledReason = nil
	stale.ChannelInfo.MultiKeyDisabledTime = nil
	stale.ChannelInfo.MultiKeyPollingIndex = 0
	stale.MarkMultiKeyStatusCleared(0)
	require.NoError(t, stale.SaveKeyManagementState(false))

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.NotContains(t, stored.ChannelInfo.MultiKeyStatusList, 0)
	assert.NotContains(t, stored.ChannelInfo.MultiKeyDisabledReason, 0)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[1])
	assert.Equal(t, "provider rejected key", stored.ChannelInfo.MultiKeyDisabledReason[1])
	assert.Equal(t, int64(222), stored.ChannelInfo.MultiKeyDisabledTime[1])
	assert.Equal(t, 1, stored.ChannelInfo.MultiKeyPollingIndex)
}

func TestSaveKeyManagementStateReplaceDropsStatusAndKeepsCursor(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:   "replace-key-status",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusAutoDisabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: 1,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusAutoDisabled,
				1: common.ChannelStatusManuallyDisabled,
			},
			MultiKeyDisabledReason: map[int]string{1: "manual disable"},
			MultiKeyDisabledTime:   map[int]int64{1: 222},
		},
	}
	require.NoError(t, DB.Create(&channel).Error)

	stale := channel
	stale.Status = common.ChannelStatusEnabled
	stale.ChannelInfo.MultiKeyStatusList = map[int]int{}
	stale.ChannelInfo.MultiKeyDisabledReason = map[int]string{}
	stale.ChannelInfo.MultiKeyDisabledTime = map[int]int64{}
	stale.ChannelInfo.MultiKeyPollingIndex = 0
	stale.MarkMultiKeyStatusReplaced()
	require.NoError(t, stale.SaveKeyManagementState(false))

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Empty(t, stored.ChannelInfo.MultiKeyStatusList)
	assert.Empty(t, stored.ChannelInfo.MultiKeyDisabledReason)
	assert.Empty(t, stored.ChannelInfo.MultiKeyDisabledTime)
	assert.Equal(t, 1, stored.ChannelInfo.MultiKeyPollingIndex)
	assert.Equal(t, 2, stored.ChannelInfo.MultiKeySize)
	assert.Equal(t, constant.MultiKeyModePolling, stored.ChannelInfo.MultiKeyMode)
}

func TestSaveKeyManagementStateReplaceStructureKeepsCursor(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:   "replace-key-structure",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: 1,
			MultiKeyStatusList: map[int]int{
				1: common.ChannelStatusAutoDisabled,
			},
			MultiKeyDisabledReason: map[int]string{1: "provider rejected key"},
			MultiKeyDisabledTime:   map[int]int64{1: 222},
		},
	}
	require.NoError(t, DB.Create(&channel).Error)

	stale := channel
	stale.Key = "key-a"
	stale.ChannelInfo.MultiKeySize = 1
	stale.ChannelInfo.MultiKeyStatusList = map[int]int{}
	stale.ChannelInfo.MultiKeyDisabledReason = map[int]string{}
	stale.ChannelInfo.MultiKeyDisabledTime = map[int]int64{}
	stale.ChannelInfo.MultiKeyPollingIndex = 0
	stale.MarkMultiKeyStructureReplaced()
	require.NoError(t, stale.SaveKeyManagementState(true))

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, "key-a", stored.Key)
	assert.Equal(t, 1, stored.ChannelInfo.MultiKeySize)
	assert.True(t, stored.ChannelInfo.IsMultiKey)
	assert.Equal(t, constant.MultiKeyModePolling, stored.ChannelInfo.MultiKeyMode)
	assert.Empty(t, stored.ChannelInfo.MultiKeyStatusList)
	assert.Equal(t, 1, stored.ChannelInfo.MultiKeyPollingIndex)
}

func TestDeleteMultiKeyBeforeCursorRewindsPollingIndex(t *testing.T) {
	setupChannelStatusTest(t)

	channel := pollingChannelWithKeys(t, "key-a\nkey-b\nkey-c", 1)
	require.NoError(t, DeleteMultiKeyAtIndex(channel.Id, 0))

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, "key-b\nkey-c", stored.Key)
	assert.Equal(t, 0, stored.ChannelInfo.MultiKeyPollingIndex)
}

func TestDeleteMultiKeyAtCursorKeepsNextKeyInPlace(t *testing.T) {
	setupChannelStatusTest(t)

	channel := pollingChannelWithKeys(t, "key-a\nkey-b\nkey-c", 1)
	require.NoError(t, DeleteMultiKeyAtIndex(channel.Id, 1))

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, "key-a\nkey-c", stored.Key)
	assert.Equal(t, 1, stored.ChannelInfo.MultiKeyPollingIndex)
}

func TestDeleteMultiKeyAfterCursorLeavesPollingIndex(t *testing.T) {
	setupChannelStatusTest(t)

	channel := pollingChannelWithKeys(t, "key-a\nkey-b\nkey-c", 0)
	require.NoError(t, DeleteMultiKeyAtIndex(channel.Id, 2))

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, "key-a\nkey-b", stored.Key)
	assert.Equal(t, 0, stored.ChannelInfo.MultiKeyPollingIndex)
}

func TestDeleteAutoDisabledKeysBeforeCursorRewindsPollingIndex(t *testing.T) {
	setupChannelStatusTest(t)

	channel := pollingChannelWithKeys(t, "key-a\nkey-b\nkey-c", 2)
	channel.ChannelInfo.MultiKeyStatusList = map[int]int{
		0: common.ChannelStatusAutoDisabled,
		1: common.ChannelStatusAutoDisabled,
	}
	require.NoError(t, DB.Model(channel).Update("channel_info", channel.ChannelInfo).Error)

	deleted, err := DeleteAutoDisabledMultiKeys(channel.Id)
	require.NoError(t, err)
	assert.Equal(t, 2, deleted)

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, "key-c", stored.Key)
	assert.Equal(t, 0, stored.ChannelInfo.MultiKeyPollingIndex)
}

func pollingChannelWithKeys(t *testing.T, keys string, cursor int) *Channel {
	t.Helper()
	channel := &Channel{
		Name:   "cursor-keys",
		Key:    keys,
		Status: common.ChannelStatusEnabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         len(strings.Split(keys, "\n")),
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: cursor,
		},
	}
	require.NoError(t, DB.Create(channel).Error)
	return channel
}
