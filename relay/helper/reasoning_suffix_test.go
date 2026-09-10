package helper

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyReasoningModelSuffixAppliesExplicitModelControls(t *testing.T) {
	temperature := 0.9
	request := &dto.GeneralOpenAIRequest{
		Model:       "qwen3-max@thinking:on@effort:high@temperature:0.2@topp:0.8",
		Temperature: &temperature,
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: request.Model,
		Request:         request,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: request.Model,
		},
	}

	require.NoError(t, ApplyReasoningModelSuffix(nil, info, request))
	assert.Equal(t, "qwen3-max", info.UpstreamModelName)
	assert.Equal(t, "qwen3-max", request.Model)
	assert.Equal(t, 0.2, *request.Temperature)
	assert.Equal(t, 0.8, *request.TopP)
	assert.Equal(t, "high", request.ReasoningEffort)
	require.NotNil(t, info.ReasoningConversion)
	assert.Equal(t, "enabled", info.ReasoningConversion.Mode)
	assert.Equal(t, "high", info.ReasoningConversion.Effort)
}
