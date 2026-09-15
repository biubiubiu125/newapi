package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestSyncChannelStatusWithEnabledKeysDisablesWhenNoKeyRemains(t *testing.T) {
	channel := &Channel{
		Status: common.ChannelStatusEnabled,
		Key:    "k1",
		ChannelInfo: ChannelInfo{
			IsMultiKey:         true,
			MultiKeySize:       1,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusAutoDisabled},
		},
	}
	SyncChannelStatusWithEnabledKeys(channel, "all keys disabled")
	require.Equal(t, common.ChannelStatusAutoDisabled, channel.Status)
}

func TestSyncChannelStatusWithEnabledKeysEnablesWhenAKeyIsRestored(t *testing.T) {
	channel := &Channel{
		Status: common.ChannelStatusAutoDisabled,
		Key:    "k1\nk2",
		ChannelInfo: ChannelInfo{
			IsMultiKey:         true,
			MultiKeySize:       2,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusAutoDisabled},
		},
	}
	delete(channel.ChannelInfo.MultiKeyStatusList, 1)
	SyncChannelStatusWithEnabledKeys(channel, "")
	require.Equal(t, common.ChannelStatusEnabled, channel.Status)
}

func TestHandlerMultiKeyUpdateEnablingChannelRestoresDisabledKeys(t *testing.T) {
	channel := &Channel{
		Status: common.ChannelStatusAutoDisabled,
		Key:    "k1\nk2",
		ChannelInfo: ChannelInfo{
			IsMultiKey:         true,
			MultiKeySize:       2,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusAutoDisabled, 1: common.ChannelStatusAutoDisabled},
		},
	}
	handlerMultiKeyUpdate(channel, "", common.ChannelStatusEnabled, "manual operation")
	require.Equal(t, common.ChannelStatusEnabled, channel.Status)
	require.Empty(t, channel.ChannelInfo.MultiKeyStatusList)
	key, _, err := channel.GetNextEnabledKey()
	require.Nil(t, err)
	require.NotEmpty(t, key)
}

func TestEnableChannelByTagRestoresDisabledMultiKeys(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Channel{}, &Ability{}))
	require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)

	weight := uint(100)
	channel := &Channel{
		Type:   1,
		Key:    "k1\nk2",
		Status: common.ChannelStatusAutoDisabled,
		Name:   "tag-channel",
		Group:  "default",
		Models: "tag-model",
		Weight: &weight,
		ChannelInfo: ChannelInfo{
			IsMultiKey:         true,
			MultiKeySize:       2,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusAutoDisabled, 1: common.ChannelStatusAutoDisabled},
		},
	}
	channel.SetTag("restore-tag")
	require.NoError(t, DB.Create(channel).Error)

	require.NoError(t, EnableChannelByTag("restore-tag"))

	reloaded, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	require.Equal(t, common.ChannelStatusEnabled, reloaded.Status)
	require.Empty(t, reloaded.ChannelInfo.MultiKeyStatusList)
	key, _, apiErr := reloaded.GetNextEnabledKey()
	require.Nil(t, apiErr)
	require.NotEmpty(t, key)
}

func TestCacheUpdateChannelStatusReindexesEnabledChannel(t *testing.T) {
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
	})
	require.NoError(t, DB.AutoMigrate(&Channel{}, &Ability{}))
	require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)

	weight := uint(100)
	channel := &Channel{
		Type:   1,
		Key:    "cache-key",
		Status: common.ChannelStatusEnabled,
		Name:   "cache-channel",
		Group:  "default",
		Models: "cache-model",
		Weight: &weight,
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "cache-model",
		ChannelId: channel.Id,
		Enabled:   true,
		Weight:    100,
	}).Error)
	InitChannelCache()

	CacheUpdateChannelStatus(channel.Id, common.ChannelStatusAutoDisabled)
	disabled, err := GetRandomSatisfiedChannel("default", "cache-model", 0)
	require.NoError(t, err)
	require.Nil(t, disabled)

	CacheUpdateChannelStatus(channel.Id, common.ChannelStatusEnabled)
	enabled, err := GetRandomSatisfiedChannel("default", "cache-model", 0)
	require.NoError(t, err)
	require.NotNil(t, enabled)
	require.Equal(t, channel.Id, enabled.Id)
}
