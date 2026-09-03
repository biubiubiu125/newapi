package oaichat

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIChatRequestToGeminiPreservesExplicitZeroParameters(t *testing.T) {
	topP := 0.0
	seed := 0.0
	maxTokens := uint(0)

	got, err := OpenAIChatRequestToGeminiGenerateContent(context.Background(), dto.GeneralOpenAIRequest{
		Model:               "gemini-2.5-flash",
		TopP:                &topP,
		Seed:                &seed,
		MaxCompletionTokens: &maxTokens,
	}, nil)
	require.NoError(t, err)
	require.NotNil(t, got.GenerationConfig.TopP)
	require.NotNil(t, got.GenerationConfig.Seed)
	require.NotNil(t, got.GenerationConfig.MaxOutputTokens)
	assert.Equal(t, float64(0), *got.GenerationConfig.TopP)
	assert.Equal(t, int64(0), *got.GenerationConfig.Seed)
	assert.Equal(t, uint(0), *got.GenerationConfig.MaxOutputTokens)
}

func TestOpenAIChatRequestToGeminiRejectsFractionalThinkingBudget(t *testing.T) {
	_, err := OpenAIChatRequestToGeminiGenerateContent(context.Background(), dto.GeneralOpenAIRequest{
		Model: "gemini-2.5-flash",
		ExtraBody: json.RawMessage(`{
			"google": {
				"thinking_config": {
					"thinking_budget": 1.5
				}
			}
		}`),
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "thinking_budget must be an integer")
}

func TestOpenAIChatRequestToGeminiPreservesFunctionCallID(t *testing.T) {
	request := dto.GeneralOpenAIRequest{
		Model: "gemini-2.5-flash",
		Messages: []dto.Message{{
			Role:    "assistant",
			Content: "",
		}},
	}
	request.Messages[0].SetToolCalls([]dto.ToolCallRequest{{
		ID:   "call_1",
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      "lookup",
			Arguments: `{"q":"x"}`,
		},
	}})

	got, err := OpenAIChatRequestToGeminiGenerateContent(context.Background(), request, nil)
	require.NoError(t, err)
	require.NotNil(t, got.Contents[0].Parts[0].FunctionCall)
	assert.Equal(t, "call_1", got.Contents[0].Parts[0].FunctionCall.ID)
}

func TestOpenAIChatRequestToGeminiPreservesFunctionResponseID(t *testing.T) {
	request := dto.GeneralOpenAIRequest{
		Model: "gemini-2.5-flash",
		Messages: []dto.Message{{
			Role:       "tool",
			ToolCallId: "call_1",
			Content:    `{"value":"ok"}`,
		}},
	}

	got, err := OpenAIChatRequestToGeminiGenerateContent(context.Background(), request, nil)
	require.NoError(t, err)
	require.NotNil(t, got.Contents[0].Parts[0].FunctionResponse)
	assert.JSONEq(t, `"call_1"`, string(got.Contents[0].Parts[0].FunctionResponse.ID))
}
