package controller

import (
	"testing"

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

	require.ErrorContains(t, err, "task plugin key")
}

func TestValidateChannelRejectsUnknownTaskPluginBinding(t *testing.T) {
	setting := `{"task_plugin_key":"missing-task-plugin"}`
	channel := &model.Channel{
		Type:    constant.ChannelTypeTaskPlugin,
		Setting: &setting,
	}

	err := validateChannel(channel, false)

	require.ErrorContains(t, err, "task plugin")
}
