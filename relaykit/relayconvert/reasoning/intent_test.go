package reasoning

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReasoningIntentCarriesExplicitStateAcrossOpenAIFormats(t *testing.T) {
	t.Parallel()

	budget := 4096
	include := true
	chat := &dto.GeneralOpenAIRequest{
		Model:           "gpt-5.6-sol",
		ReasoningEffort: "high",
		Reasoning:       []byte(`{"max_tokens":4096,"exclude":false}`),
	}

	intent, err := FromOpenAIChat(chat)
	require.NoError(t, err)
	assert.Equal(t, ModeEnabled, intent.Mode)
	assert.Equal(t, EffortHigh, intent.Effort)
	require.NotNil(t, intent.BudgetTokens)
	assert.Equal(t, budget, *intent.BudgetTokens)
	require.NotNil(t, intent.IncludeThoughts)
	assert.Equal(t, include, *intent.IncludeThoughts)

	responses := &dto.OpenAIResponsesRequest{Model: chat.Model}
	require.NoError(t, ApplyToOpenAIResponses(responses, intent))
	require.NotNil(t, responses.Reasoning)
	assert.Equal(t, "high", responses.Reasoning.Effort)
	require.NotNil(t, responses.ReasoningConversion)
	assert.Equal(t, budget, *responses.ReasoningConversion.BudgetTokens)
}

func TestReasoningIntentRejectsConflictingExplicitControls(t *testing.T) {
	t.Parallel()

	_, err := FromOpenAIChat(&dto.GeneralOpenAIRequest{
		Model:           "gpt-5.6-sol",
		ReasoningEffort: "low",
		Reasoning:       []byte(`{"effort":"high"}`),
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEffortConflict)
}
