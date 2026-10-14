package xai

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitAttachesResponsesVendorToolUsageOnly(t *testing.T) {
	info := &relaycommon.RelayInfo{RelayMode: constant.RelayModeResponses}
	(&Adaptor{}).Init(info)
	require.NotNil(t, info.VendorToolUsage)

	info.ApplyVendorToolUsage([]byte(`{"usage":{"server_side_tool_usage_details":{"web_search_calls":2,"x_posts_fetched":3,"x_users_fetched":1}}}`))
	require.NotNil(t, info.ResponsesUsageInfo)
	assert.Equal(t, 2, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearch].CallCount)
	assert.Equal(t, 0, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
	assert.Equal(t, 3, info.ResponsesUsageInfo.BuiltInTools["x_search_posts"].CallCount)
	assert.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools["x_search_profiles"].CallCount)

	chat := &relaycommon.RelayInfo{RelayMode: constant.RelayModeChatCompletions}
	(&Adaptor{}).Init(chat)
	assert.Nil(t, chat.VendorToolUsage)
	assert.NotPanics(t, func() { (&Adaptor{}).Init(nil) })
}
