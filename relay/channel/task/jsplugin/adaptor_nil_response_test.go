package jsplugin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTaskAdaptorParseResponseRejectsNilUpstreamResponse(t *testing.T) {
	plugin, err := pluginruntime.NewRegistry().Register(mockPlugin, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)

	parsed, taskErr := adaptor.ParseResponse(c, nil, info)

	require.Nil(t, parsed)
	require.NotNil(t, taskErr)
	require.Equal(t, "plugin_submit_response_invalid", taskErr.Code)
}

func TestTaskAdaptorParseResponseRejectsResponseWithoutBody(t *testing.T) {
	plugin, err := pluginruntime.NewRegistry().Register(mockPlugin, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)

	parsed, taskErr := adaptor.ParseResponse(c, &http.Response{StatusCode: http.StatusOK}, info)

	require.Nil(t, parsed)
	require.NotNil(t, taskErr)
	require.Equal(t, "plugin_submit_response_invalid", taskErr.Code)
}
