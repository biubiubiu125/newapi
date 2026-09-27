package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	appdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	relaydto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestChannelSatisfiesTaskPluginIdentity(t *testing.T) {
	pluginKey := "demo-plugin"
	constraints := &appdto.ChannelConstraints{}
	constraints.AddFilter(appdto.ChannelFilter{
		Kind:                   appdto.FilterTaskPluginIdentity,
		TaskPluginKey:          pluginKey,
		TaskPluginChannelTypes: []int{constant.ChannelTypeOpenAI},
	})

	taskPluginSetting := relaydto.ChannelSettings{TaskPluginKey: pluginKey}
	taskPluginChannel := &model.Channel{Type: constant.ChannelTypeTaskPlugin}
	taskPluginChannel.SetSetting(taskPluginSetting)

	otherTaskPluginSetting := relaydto.ChannelSettings{TaskPluginKey: "other-plugin"}
	otherTaskPluginChannel := &model.Channel{Type: constant.ChannelTypeTaskPlugin}
	otherTaskPluginChannel.SetSetting(otherTaskPluginSetting)

	sharedChannel := &model.Channel{Type: constant.ChannelTypeOpenAI}
	unrelatedChannel := &model.Channel{Type: constant.ChannelTypeAnthropic}

	require.True(t, ChannelSatisfiesConstraints(taskPluginChannel, "", constraints))
	require.False(t, ChannelSatisfiesConstraints(otherTaskPluginChannel, "", constraints))
	require.True(t, ChannelSatisfiesConstraints(sharedChannel, "", constraints))
	require.False(t, ChannelSatisfiesConstraints(unrelatedChannel, "", constraints))
}

func TestTaskPluginIdentityIncludesSharedEndpointTaskPluginKeys(t *testing.T) {
	first := &pluginruntime.LoadedPlugin{
		Meta: pluginruntime.Meta{Key: "first-plugin"},
	}
	second := &pluginruntime.LoadedPlugin{
		Meta: pluginruntime.Meta{Key: "second-plugin"},
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(pluginruntime.ContextKeyPinnedEndpoint, pluginruntime.PinnedEndpoint{
		Generation: &pluginruntime.RoutingGeneration{Number: 1},
		Plugin:     first,
		Candidates: []pluginruntime.ProtocolBinding{
			{Plugin: first},
			{Plugin: second},
		},
	})

	AppendTaskPluginIdentityFilter(c, first.Meta.Key)

	secondChannel := &model.Channel{Type: constant.ChannelTypeTaskPlugin}
	secondChannel.SetSetting(relaydto.ChannelSettings{TaskPluginKey: second.Meta.Key})
	otherChannel := &model.Channel{Type: constant.ChannelTypeTaskPlugin}
	otherChannel.SetSetting(relaydto.ChannelSettings{TaskPluginKey: "unrelated-plugin"})
	constraints := GetChannelConstraints(c)

	require.True(t, ChannelSatisfiesConstraints(secondChannel, "", constraints))
	require.False(t, ChannelSatisfiesConstraints(otherChannel, "", constraints))
}

func TestChannelSatisfiesRequestPathAndResponsesWebSocket(t *testing.T) {
	imagePath := &appdto.ChannelConstraints{}
	imagePath.AddFilter(appdto.ChannelFilter{Kind: appdto.FilterRequestPath, RequestPath: "/v1/image-tasks/generations"})
	require.True(t, ChannelSatisfiesConstraints(&model.Channel{Type: constant.ChannelTypeOpenAI}, "gpt-image-1", imagePath))

	advanced := &model.Channel{Type: constant.ChannelTypeAdvancedCustom}
	advanced.SetOtherSettings(relaydto.ChannelOtherSettings{AdvancedCustom: &relaydto.AdvancedCustomConfig{Routes: []relaydto.AdvancedCustomRoute{{
		IncomingPath: "/v1/images/generations",
		UpstreamPath: "/v1/images/generations",
		Models:       []string{"gpt-image-1"},
	}}}})
	selectionPath := &appdto.ChannelConstraints{}
	selectionPath.AddFilter(appdto.ChannelFilter{Kind: appdto.FilterRequestPath, RequestPath: "/v1/images/generations"})
	require.True(t, ChannelSatisfiesConstraints(advanced, "gpt-image-1", selectionPath))
	require.False(t, ChannelSatisfiesConstraints(advanced, "other-model", selectionPath))
	require.False(t, ChannelSatisfiesConstraints(advanced, "gpt-image-1", imagePath))

	websocket := &appdto.ChannelConstraints{}
	websocket.AddFilter(appdto.ChannelFilter{Kind: appdto.FilterResponsesWebSocket})
	enabled := &model.Channel{Type: constant.ChannelTypeOpenAI}
	enabled.SetSetting(relaydto.ChannelSettings{ResponsesWebSocketEnabled: true})
	require.True(t, ChannelSatisfiesConstraints(enabled, "gpt-4o", websocket))
	require.False(t, ChannelSatisfiesConstraints(&model.Channel{Type: constant.ChannelTypeOpenAI}, "gpt-4o", websocket))

	unknown := &appdto.ChannelConstraints{}
	unknown.AddFilter(appdto.ChannelFilter{Kind: "not-a-real-filter"})
	require.False(t, ChannelSatisfiesConstraints(&model.Channel{Type: constant.ChannelTypeOpenAI}, "gpt-image-1", unknown))
}
