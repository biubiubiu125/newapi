package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRebindTaskPluginEndpointToSelectedLegacyChannel(t *testing.T) {
	first := &pluginruntime.LoadedPlugin{
		Meta: pluginruntime.Meta{Key: "first-provider", ChannelTypes: []int{1001}},
	}
	second := &pluginruntime.LoadedPlugin{
		Meta: pluginruntime.Meta{Key: "second-provider", ChannelTypes: []int{1002}},
	}
	generation := &pluginruntime.RoutingGeneration{Number: 7}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(pluginruntime.ContextKeyPinnedEndpoint, pluginruntime.PinnedEndpoint{
		Generation: generation,
		Plugin:     first,
		Candidates: []pluginruntime.ProtocolBinding{
			{Plugin: first},
			{Plugin: second},
		},
	})
	c.Set(pluginruntime.ContextKeyPinnedPlugin, pluginruntime.PinnedPlugin{
		Generation: generation,
		Plugin:     first,
	})
	c.Set("expected_task_plugin_key", first.Meta.Key)
	c.Set("task_plugin_key", first.Meta.Key)
	c.Set("platform", first.Meta.Key)

	rebindTaskPluginEndpointForSelectedChannel(c, &model.Channel{Type: 1002})

	pinnedValue, exists := c.Get(pluginruntime.ContextKeyPinnedEndpoint)
	require.True(t, exists)
	pinned, ok := pinnedValue.(pluginruntime.PinnedEndpoint)
	require.True(t, ok)
	require.NotNil(t, pinned.Plugin)
	assert.Equal(t, second.Meta.Key, pinned.Plugin.Meta.Key)

	pluginValue, exists := c.Get(pluginruntime.ContextKeyPinnedPlugin)
	require.True(t, exists)
	pinnedPlugin, ok := pluginValue.(pluginruntime.PinnedPlugin)
	require.True(t, ok)
	require.NotNil(t, pinnedPlugin.Plugin)
	assert.Equal(t, second.Meta.Key, pinnedPlugin.Plugin.Meta.Key)
	assert.Equal(t, second.Meta.Key, c.GetString("expected_task_plugin_key"))
	assert.Equal(t, second.Meta.Key, c.GetString("task_plugin_key"))
	assert.Equal(t, second.Meta.Key, c.GetString("platform"))
}
