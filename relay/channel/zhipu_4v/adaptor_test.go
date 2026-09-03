package zhipu_4v

import (
	"encoding/json"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdaptorPassthroughOpenAIResponses(t *testing.T) {
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "http://zhipu.test",
		},
		RelayMode: relayconstant.RelayModeResponses,
	}
	request := dto.OpenAIResponsesRequest{
		Model: "glm-5",
		Input: json.RawMessage(`[{"role":"user","content":"hello"}]`),
	}

	result, err := adaptor.ConvertOpenAIResponsesRequest(nil, info, request)
	require.NoError(t, err)
	require.IsType(t, dto.OpenAIResponsesRequest{}, result)
	assert.Equal(t, request, result)

	url, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "http://zhipu.test/api/v1/responses", url)
}
