package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetNextEnabledKeySkipsBlankLines(t *testing.T) {
	channel := &Channel{
		Status: common.ChannelStatusEnabled,
		Key:    "key-a\n\nkey-b",
		ChannelInfo: ChannelInfo{
			IsMultiKey:         true,
			MultiKeySize:       3,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusManuallyDisabled},
		},
	}
	key, idx, err := channel.GetNextEnabledKey()
	require.Nil(t, err)
	require.Equal(t, "key-b", key)
	require.Equal(t, 2, idx)

	blanksDisabled := &Channel{
		Status: common.ChannelStatusEnabled,
		Key:    "key-a\n\nkey-b",
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 3,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusManuallyDisabled,
				2: common.ChannelStatusManuallyDisabled,
			},
		},
	}
	_, _, err = blanksDisabled.GetNextEnabledKey()
	require.NotNil(t, err)
}

func TestPollingKeySaveDoesNotReviveDisabledKey(t *testing.T) {
	setupChannelStatusTest(t)

	channel := &Channel{
		Name:   "poll-stale",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		Models: "poll-model",
		Group:  "default",
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: 0,
		},
	}
	require.NoError(t, channel.Insert())

	stale, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	require.True(t, UpdateChannelStatus(channel.Id, "key-a", common.ChannelStatusAutoDisabled, "provider rejected key"))

	key, idx, apiErr := stale.GetNextEnabledKey()
	require.Nil(t, apiErr)
	require.Equal(t, "key-b", key)
	require.Equal(t, 1, idx)

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	require.Equal(t, common.ChannelStatusEnabled, stored.Status)
	require.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[0])
	require.NotContains(t, stored.ChannelInfo.MultiKeyStatusList, 1)
	require.Equal(t, 0, stored.ChannelInfo.MultiKeyPollingIndex)
}

func TestGetNextEnabledKeyPollingSkipsBlankLines(t *testing.T) {
	setupChannelStatusTest(t)

	channel := &Channel{
		Name:   "blank-poll",
		Key:    "key-a\n\nkey-b",
		Status: common.ChannelStatusEnabled,
		Models: "test-model",
		Group:  "default",
		ChannelInfo: ChannelInfo{
			IsMultiKey:         true,
			MultiKeySize:       3,
			MultiKeyMode:       constant.MultiKeyModePolling,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusManuallyDisabled},
		},
	}
	require.NoError(t, channel.Insert())
	key, idx, err := channel.GetNextEnabledKey()
	require.Nil(t, err)
	require.Equal(t, "key-b", key)
	require.Equal(t, 2, idx)
}

func TestSyncChannelStatusIgnoresBlankKeys(t *testing.T) {
	channel := &Channel{
		Status: common.ChannelStatusEnabled,
		Key:    "\n\n",
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
		},
	}
	SyncChannelStatusWithEnabledKeys(channel, "all keys disabled")
	require.Equal(t, common.ChannelStatusAutoDisabled, channel.Status)
}

func TestSaveKeyManagementStateDoesNotClobberQuota(t *testing.T) {
	setupChannelStatusTest(t)

	channel := &Channel{
		Name:      "quota",
		Key:       "key-one\nkey-two",
		Status:    common.ChannelStatusEnabled,
		Models:    "test-model",
		Group:     "default",
		UsedQuota: 100,
		Balance:   3.5,
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
		},
	}
	require.NoError(t, channel.Insert())
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Updates(map[string]any{
		"used_quota": int64(180),
		"balance":    9.25,
	}).Error)

	channel.UsedQuota = 100
	channel.Balance = 3.5
	channel.ChannelInfo.MultiKeyStatusList = map[int]int{0: common.ChannelStatusManuallyDisabled}
	channel.ChannelInfo.MultiKeyDisabledReason = map[int]string{0: "manual disable"}
	require.NoError(t, channel.SaveKeyManagementState(false))

	var saved Channel
	require.NoError(t, DB.First(&saved, channel.Id).Error)
	require.Equal(t, int64(180), saved.UsedQuota)
	require.Equal(t, 9.25, saved.Balance)
	require.Equal(t, common.ChannelStatusManuallyDisabled, saved.ChannelInfo.MultiKeyStatusList[0])
}

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

func TestUpdateChannelStatusRollsBackWhenAbilityUpdateFails(t *testing.T) {
	setupChannelStatusTest(t)

	channel := &Channel{
		Name:   "status-ability",
		Key:    "sk-test",
		Status: common.ChannelStatusEnabled,
		Models: "status-model",
		Group:  "default",
	}
	require.NoError(t, channel.Insert())

	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register("fail-ability-status", failAbilityStatusWrite))
	t.Cleanup(func() {
		_ = DB.Callback().Update().Remove("fail-ability-status")
	})

	require.False(t, UpdateChannelStatus(channel.Id, "", common.ChannelStatusManuallyDisabled, "manual operation"))

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	require.Equal(t, common.ChannelStatusEnabled, stored.Status)

	var ability Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
	require.True(t, ability.Enabled)
}

func TestDisableChannelByTagRollsBackWhenAbilityUpdateFails(t *testing.T) {
	setupChannelStatusTest(t)

	tag := "disable-tag"
	channel := &Channel{
		Name:   "tag-ability",
		Key:    "sk-test",
		Status: common.ChannelStatusEnabled,
		Models: "tag-model",
		Group:  "default",
		Tag:    &tag,
	}
	require.NoError(t, channel.Insert())

	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register("fail-ability-tag", failAbilityStatusWrite))
	t.Cleanup(func() {
		_ = DB.Callback().Update().Remove("fail-ability-tag")
	})

	require.Error(t, DisableChannelByTag(tag))

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	require.Equal(t, common.ChannelStatusEnabled, stored.Status)

	var ability Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
	require.True(t, ability.Enabled)
}

func failAbilityStatusWrite(tx *gorm.DB) {
	if tx == nil || tx.Statement == nil {
		return
	}
	table := tx.Statement.Table
	if table == "" && tx.Statement.Schema != nil {
		table = tx.Statement.Schema.Table
	}
	if table == "abilities" {
		tx.AddError(errors.New("ability write failed"))
	}
}

func TestDisableChannelByTagRollsBackAllKeysReason(t *testing.T) {
	setupChannelStatusTest(t)

	tag := "all-keys-tag"
	channel := &Channel{
		Name:   "all-keys",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusAutoDisabled,
		Models: "tag-model",
		Group:  "default",
		Tag:    &tag,
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
			MultiKeyStatusList: map[int]int{
				0: common.ChannelStatusAutoDisabled,
				1: common.ChannelStatusAutoDisabled,
			},
		},
	}
	channel.SetOtherInfo(map[string]interface{}{"status_reason": ChannelStatusReasonAllKeysDisabled})
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group: "default", Model: "tag-model", ChannelId: channel.Id, Enabled: true, Tag: &tag,
	}).Error)

	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register("fail-ability-all-keys", failAbilityStatusWrite))
	t.Cleanup(func() {
		_ = DB.Callback().Update().Remove("fail-ability-all-keys")
	})

	require.Error(t, DisableChannelByTag(tag))

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	require.Equal(t, common.ChannelStatusAutoDisabled, stored.Status)
	require.Equal(t, ChannelStatusReasonAllKeysDisabled, stored.GetOtherInfo()["status_reason"])

	var ability Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
	require.True(t, ability.Enabled)
}

func TestEnableChannelByTagRollsBackWhenAbilityUpdateFails(t *testing.T) {
	setupChannelStatusTest(t)

	tag := "enable-tag"
	channel := &Channel{
		Name:   "enable-ability",
		Key:    "sk-test",
		Status: common.ChannelStatusAutoDisabled,
		Models: "enable-model",
		Group:  "default",
		Tag:    &tag,
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "enable-model",
		ChannelId: channel.Id,
		Enabled:   false,
		Tag:       &tag,
	}).Error)

	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register("fail-ability-enable", failAbilityStatusWrite))
	t.Cleanup(func() {
		_ = DB.Callback().Update().Remove("fail-ability-enable")
	})

	require.Error(t, EnableChannelByTag(tag))

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	require.Equal(t, common.ChannelStatusAutoDisabled, stored.Status)

	var ability Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
	require.False(t, ability.Enabled)
}

func TestUpdateChannelStatusDisablesAbilityWithChannel(t *testing.T) {
	setupChannelStatusTest(t)

	channel := &Channel{
		Name:   "status-ability-ok",
		Key:    "sk-test",
		Status: common.ChannelStatusEnabled,
		Models: "status-model",
		Group:  "default",
	}
	require.NoError(t, channel.Insert())

	require.True(t, UpdateChannelStatus(channel.Id, "", common.ChannelStatusManuallyDisabled, "manual operation"))

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	require.Equal(t, common.ChannelStatusManuallyDisabled, stored.Status)

	var ability Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
	require.False(t, ability.Enabled)
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
