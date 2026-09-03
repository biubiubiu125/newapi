package oairesponses

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesRequestToGeminiChatMapsReasoningBudget(t *testing.T) {
	meta := &convmeta.Values{
		ChannelMetaAttached: true,
		UpstreamModelName:   "gemini-2.5-flash",
		Options: &convmeta.Options{
			Gemini: convmeta.GeminiOptions{
				ThinkingAdapterEnabled: true,
			},
		},
	}
	budget := uint(4096)

	got, err := OpenAIResponsesRequestToGeminiChat(context.Background(), &dto.OpenAIResponsesRequest{
		Model:           "gemini-2.5-flash",
		MaxOutputTokens: &budget,
		Reasoning:       &dto.Reasoning{Effort: "high"},
		Input:           []byte(`"Think carefully."`),
	}, meta)

	require.NoError(t, err)
	require.NotNil(t, got.GenerationConfig.ThinkingConfig)
	assert.Equal(t, "high", got.GenerationConfig.ThinkingConfig.ThinkingLevel)
}
