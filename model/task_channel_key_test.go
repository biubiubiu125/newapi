package model

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	commonRelay "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestInitTaskPersistsSelectedChannelKeyForAsyncTaskChannels(t *testing.T) {
	for _, channelType := range []int{constant.ChannelTypeOpenAI, constant.ChannelTypeSora} {
		t.Run(channelTypeString(channelType), func(t *testing.T) {
			relayInfo := &commonRelay.RelayInfo{
				ChannelMeta: &commonRelay.ChannelMeta{
					ChannelType: channelType,
					ApiKey:      "selected-key",
				},
				TaskRelayInfo: &commonRelay.TaskRelayInfo{},
			}

			task := InitTask(constant.TaskPlatform(channelTypeString(channelType)), relayInfo)

			require.Equal(t, "selected-key", task.PrivateData.Key)
		})
	}
}

func TestInitTaskStoresResultProxyHostsFromChannelBaseURL(t *testing.T) {
	relayInfo := &commonRelay.RelayInfo{
		ChannelMeta: &commonRelay.ChannelMeta{
			ChannelType:    constant.ChannelTypeGemini,
			ChannelBaseUrl: "https://gemini.internal/v1beta",
			ApiKey:         "selected-key",
		},
		TaskRelayInfo: &commonRelay.TaskRelayInfo{},
	}

	task := InitTask(constant.TaskPlatform("gemini"), relayInfo)

	require.Equal(t, []string{"gemini.internal"}, task.PrivateData.ResultProxyHosts)
}

func channelTypeString(channelType int) string {
	switch channelType {
	case constant.ChannelTypeOpenAI:
		return "openai"
	case constant.ChannelTypeSora:
		return "sora"
	default:
		return "unknown"
	}
}
