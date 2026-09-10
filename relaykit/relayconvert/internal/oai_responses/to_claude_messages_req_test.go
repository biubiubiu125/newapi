package oairesponses

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesRequestToClaudeMessagesMapsMaxReasoningEffort(t *testing.T) {
	maxTokens := uint(8192)
	got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, &dto.OpenAIResponsesRequest{
		Model:           "claude-test",
		MaxOutputTokens: &maxTokens,
		Reasoning:       &dto.Reasoning{Effort: "max"},
		Input:           []byte(`"Think carefully."`),
	})

	require.NoError(t, err)
	require.NotNil(t, got.Thinking)
	assert.Equal(t, "enabled", got.Thinking.Type)
	assert.Equal(t, 7782, got.Thinking.GetBudgetTokens())
}
