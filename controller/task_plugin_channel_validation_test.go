package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestValidateChannelRequiresTaskPluginBinding(t *testing.T) {
	emptySetting := `{"task_plugin_key":"  "}`
	channel := &model.Channel{
		Type:    constant.ChannelTypeTaskPlugin,
		Setting: &emptySetting,
	}

	err := validateChannel(channel, false)

	loc, ok := common.AsLocalizedError(err)
	require.True(t, ok, "want LocalizedError, got %v", err)
	require.Equal(t, "channel.task_plugin_key_required", loc.Key)
}

func TestValidateChannelRejectsUnknownTaskPluginBinding(t *testing.T) {
	setting := `{"task_plugin_key":"missing-task-plugin"}`
	channel := &model.Channel{
		Type:    constant.ChannelTypeTaskPlugin,
		Setting: &setting,
	}

	err := validateChannel(channel, false)

	loc, ok := common.AsLocalizedError(err)
	require.True(t, ok, "want LocalizedError, got %v", err)
	require.Equal(t, "channel.task_plugin_not_registered", loc.Key)
	require.NotEmpty(t, loc.Args)
	require.Equal(t, "missing-task-plugin", loc.Args[0]["Plugin"])
}
