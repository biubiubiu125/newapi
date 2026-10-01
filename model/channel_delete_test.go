package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestDeleteDisabledChannelRemovesAbilities(t *testing.T) {
	channel := &Channel{
		Type:   1,
		Key:    "disabled-key",
		Status: common.ChannelStatusManuallyDisabled,
		Name:   "delete-disabled-channel",
		Models: "delete-disabled-model",
		Group:  "delete-disabled-group",
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "delete-disabled-group",
		Model:     "delete-disabled-model",
		ChannelId: channel.Id,
		Enabled:   true,
	}).Error)

	_, err := DeleteDisabledChannel()
	require.NoError(t, err)

	var channels int64
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Count(&channels).Error)
	require.Zero(t, channels)

	var abilities int64
	require.NoError(t, DB.Model(&Ability{}).Where("channel_id = ?", channel.Id).Count(&abilities).Error)
	require.Zero(t, abilities)
}

func TestDeleteChannelByStatusRemovesAbilities(t *testing.T) {
	channel := &Channel{
		Type:   1,
		Key:    "status-key",
		Status: common.ChannelStatusAutoDisabled,
		Name:   "delete-by-status-channel",
		Models: "delete-by-status-model",
		Group:  "delete-by-status-group",
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "delete-by-status-group",
		Model:     "delete-by-status-model",
		ChannelId: channel.Id,
		Enabled:   false,
	}).Error)

	_, err := DeleteChannelByStatus(common.ChannelStatusAutoDisabled)
	require.NoError(t, err)

	var abilities int64
	require.NoError(t, DB.Model(&Ability{}).Where("channel_id = ?", channel.Id).Count(&abilities).Error)
	require.Zero(t, abilities)
}

func TestChannelDeleteRemovesAbilitiesInOneTransaction(t *testing.T) {
	channel := &Channel{
		Type:   1,
		Key:    "single-delete-key",
		Status: common.ChannelStatusEnabled,
		Name:   "single-delete-channel",
		Models: "single-delete-model",
		Group:  "single-delete-group",
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "single-delete-group",
		Model:     "single-delete-model",
		ChannelId: channel.Id,
		Enabled:   true,
	}).Error)

	require.NoError(t, channel.Delete())

	var channels int64
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Count(&channels).Error)
	require.Zero(t, channels)
	var abilities int64
	require.NoError(t, DB.Model(&Ability{}).Where("channel_id = ?", channel.Id).Count(&abilities).Error)
	require.Zero(t, abilities)
}
