package oairesponses

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesRequestToGeminiPreservesExplicitZeroParameters(t *testing.T) {
	topP := 0.0
	maxTokens := uint(0)
	req := &dto.OpenAIResponsesRequest{
		Model:           "gemini-2.5-flash",
		TopP:            &topP,
		MaxOutputTokens: &maxTokens,
		Input:           json.RawMessage(`"hello"`),
	}

	got, err := OpenAIResponsesRequestToGeminiChat(nil, req, nil)
	require.NoError(t, err)
	require.NotNil(t, got.GenerationConfig.TopP)
	require.NotNil(t, got.GenerationConfig.MaxOutputTokens)
	assert.Equal(t, float64(0), *got.GenerationConfig.TopP)
	assert.Equal(t, uint(0), *got.GenerationConfig.MaxOutputTokens)
}

func TestOpenAIResponsesRequestToGeminiPreservesFunctionCallIDs(t *testing.T) {
	req := &dto.OpenAIResponsesRequest{
		Model: "gemini-2.5-flash",
		Input: json.RawMessage(`[
			{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"q\":\"x\"}"},
			{"type":"function_call_output","call_id":"call_1","output":"ok"}
		]`),
	}

	got, err := OpenAIResponsesRequestToGeminiChat(nil, req, nil)
	require.NoError(t, err)
	require.Len(t, got.Contents, 2)
	require.NotNil(t, got.Contents[0].Parts[0].FunctionCall)
	assert.Equal(t, "call_1", got.Contents[0].Parts[0].FunctionCall.ID)
	require.NotNil(t, got.Contents[1].Parts[0].FunctionResponse)
	assert.JSONEq(t, `"call_1"`, string(got.Contents[1].Parts[0].FunctionResponse.ID))
}
