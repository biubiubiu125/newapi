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

func TestOpenAIResponsesRequestToClaudeMessagesOmitsTrailingAssistantText(t *testing.T) {
	maxTokens := uint(128)
	got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, &dto.OpenAIResponsesRequest{
		Model:           "claude-test",
		MaxOutputTokens: &maxTokens,
		Input:           []byte(`[{"role":"user","content":"question"},{"role":"assistant","content":"old answer"}]`),
	})
	require.NoError(t, err)
	require.Len(t, got.Messages, 1)
	assert.Equal(t, "user", got.Messages[0].Role)

	kept, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, &dto.OpenAIResponsesRequest{
		Model:           "claude-test",
		MaxOutputTokens: &maxTokens,
		Input:           []byte(`[{"role":"assistant","content":"prefill"}]`),
	})
	require.NoError(t, err)
	require.NotEmpty(t, kept.Messages)
	assert.Equal(t, "assistant", kept.Messages[len(kept.Messages)-1].Role)
}
