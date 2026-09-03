package gemini

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/stretchr/testify/require"
)

func TestApplyThinkingConfigPreservesRealEffortTailModelID(t *testing.T) {
	request := &dto.GeminiChatRequest{}
	info := &convmeta.Values{
		UpstreamModelName:   "qwen-max",
		ChannelMetaAttached: true,
		Options: &convmeta.Options{
			PreserveEffortTail: func(modelName string) bool {
				return modelName == "qwen-max"
			},
			Gemini: convmeta.GeminiOptions{
				ThinkingAdapterEnabled: true,
			},
		},
	}

	ApplyThinkingConfig(request, info)

	require.Nil(t, request.GenerationConfig.ThinkingConfig)
}
