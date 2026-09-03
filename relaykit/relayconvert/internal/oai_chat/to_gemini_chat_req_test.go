package oaichat

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
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

func TestOpenAIChatRequestToGeminiKeepsHostedNamesAsFunctionsWhenDisabled(t *testing.T) {
	request := dto.GeneralOpenAIRequest{
		Model: "gemini-2.5-flash",
		Tools: []dto.ToolCallRequest{
			{Type: "function", Function: dto.FunctionRequest{Name: "googleSearch"}},
			{Type: "function", Function: dto.FunctionRequest{Name: "codeExecution"}},
			{Type: "function", Function: dto.FunctionRequest{Name: "urlContext"}},
		},
	}

	got, err := OpenAIChatRequestToGeminiGenerateContent(context.Background(), request, &convmeta.Values{
		Options: &convmeta.Options{},
	})
	require.NoError(t, err)
	require.Len(t, got.GetTools(), 1)
	assert.Nil(t, got.GetTools()[0].GoogleSearch)
	assert.Nil(t, got.GetTools()[0].CodeExecution)
	assert.Nil(t, got.GetTools()[0].URLContext)
	functions, err := kitutil.Any2Type[[]dto.FunctionRequest](got.GetTools()[0].FunctionDeclarations)
	require.NoError(t, err)
	require.Len(t, functions, 3)
	assert.Equal(t, "googleSearch", functions[0].Name)
	assert.Equal(t, "codeExecution", functions[1].Name)
	assert.Equal(t, "urlContext", functions[2].Name)
}

func TestOpenAIChatRequestToGeminiMapsOnlyEnabledHostedTools(t *testing.T) {
	request := dto.GeneralOpenAIRequest{
		Model: "gemini-2.5-flash",
		Tools: []dto.ToolCallRequest{
			{Type: "function", Function: dto.FunctionRequest{Name: "googleSearch"}},
			{Type: "function", Function: dto.FunctionRequest{Name: "codeExecution"}},
			{Type: "function", Function: dto.FunctionRequest{Name: "urlContext"}},
		},
	}

	got, err := OpenAIChatRequestToGeminiGenerateContent(context.Background(), request, &convmeta.Values{
		Options: &convmeta.Options{
			HostedTools: convmeta.HostedToolCapabilities{
				GoogleSearch:  true,
				CodeExecution: true,
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, got.GetTools(), 3)
	assert.NotNil(t, got.GetTools()[0].CodeExecution)
	assert.NotNil(t, got.GetTools()[1].GoogleSearch)
	functions, err := kitutil.Any2Type[[]dto.FunctionRequest](got.GetTools()[2].FunctionDeclarations)
	require.NoError(t, err)
	require.Len(t, functions, 1)
	assert.Equal(t, "urlContext", functions[0].Name)
}
