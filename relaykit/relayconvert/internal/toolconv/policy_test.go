package toolconv

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func geminiCodeExecutionRequest(t *testing.T) *dto.GeminiChatRequest {
	t.Helper()
	tools, err := kitutil.Marshal([]map[string]any{{"codeExecution": map[string]any{}}})
	require.NoError(t, err)
	return &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{
			{Role: "user", Parts: []dto.GeminiPart{{Text: "run this"}}},
		},
		Tools: tools,
	}
}

func hasDiagnosticCode(diagnostics []types.ConversionDiagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func TestDefaultPolicyAllowsGeminiCodeExecutionToOpenAI(t *testing.T) {
	t.Parallel()

	_, set, err := ExtractRequest(types.RelayFormatGemini, geminiCodeExecutionRequest(t))
	require.NoError(t, err)
	target := &dto.GeneralOpenAIRequest{
		Model:    "gpt-4o",
		Messages: []dto.Message{{Role: "user", Content: "run this"}},
	}

	out, diagnostics, err := AttachRequest(types.RelayFormatOpenAI, target, set, &convmeta.Options{})
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.True(t, hasDiagnosticCode(diagnostics, "unsupported_hosted_tool"))
	assert.Equal(t, types.ConversionLossPolicyAllow, (&convmeta.Options{}).EffectiveToolLossPolicy())
}

func TestResponsePhaseNeverRejectsEvenUnderStrictPolicy(t *testing.T) {
	t.Parallel()

	text := "hello"
	resp := &dto.ClaudeResponse{
		Type:       "message",
		Role:       "assistant",
		StopReason: "pause_turn",
		Content: []dto.ClaudeMediaMessage{
			{Type: "redacted_thinking", Data: "secret"},
			{Type: "text", Text: &text},
		},
	}
	diagnostics := InspectResponse(types.RelayFormatClaude, types.RelayFormatOpenAI, resp)
	require.True(t, hasDiagnosticCode(diagnostics, "continuation_state_lost"))
	require.Error(t, types.RejectConversionLoss(types.ConversionLossPolicyStrict, diagnostics))

	_, hosted, err := ExtractHostedResponse(types.RelayFormatClaude, resp)
	require.NoError(t, err)
	out, _, err := AttachHostedResponse(
		types.RelayFormatOpenAI,
		&dto.OpenAITextResponse{},
		hosted,
		&convmeta.Options{ToolLossPolicy: types.ConversionLossPolicyStrict},
	)
	require.NoError(t, err)
	require.NotNil(t, out)
}

func TestSafePolicyRejectsRequestPhaseHostedToolLoss(t *testing.T) {
	t.Parallel()

	_, set, err := ExtractRequest(types.RelayFormatGemini, geminiCodeExecutionRequest(t))
	require.NoError(t, err)
	target := &dto.GeneralOpenAIRequest{
		Model:    "gpt-4o",
		Messages: []dto.Message{{Role: "user", Content: "run this"}},
	}

	_, diagnostics, err := AttachRequest(
		types.RelayFormatOpenAI,
		target,
		set,
		&convmeta.Options{ToolLossPolicy: types.ConversionLossPolicySafe},
	)
	require.Error(t, err)
	var loss *types.ConversionLossError
	require.ErrorAs(t, err, &loss)
	require.NotEmpty(t, loss.Diagnostics)
	assert.True(t, hasDiagnosticCode(loss.Diagnostics, "unsupported_hosted_tool"))
	assert.True(t, hasDiagnosticCode(diagnostics, "unsupported_hosted_tool"))
}

func TestResponsesCustomToolReachesEachTargetAsStringInputFunction(t *testing.T) {
	t.Parallel()

	req := responsesCustomToolRequest(t)
	_, set, err := ExtractRequest(types.RelayFormatOpenAIResponses, req)
	require.NoError(t, err)

	chat, diagnostics, err := AttachRequest(types.RelayFormatOpenAI, &dto.GeneralOpenAIRequest{Model: "gpt-test"}, set, &convmeta.Options{})
	require.NoError(t, err)
	assert.True(t, hasDiagnosticCode(diagnostics, "custom_tool_as_function"))
	chatReq := chat.(*dto.GeneralOpenAIRequest)
	require.Len(t, chatReq.Tools, 1)
	assert.Equal(t, "apply_patch", chatReq.Tools[0].Function.Name)
	assert.Contains(t, chatReq.Tools[0].Function.Description, "Lark grammar")
	assert.Equal(t, "string", chatReq.Tools[0].Function.Parameters.(map[string]any)["properties"].(map[string]any)["input"].(map[string]any)["type"])
	assert.Equal(t, map[string]any{"type": "function", "function": map[string]any{"name": "apply_patch"}}, chatReq.ToolChoice)

	claude, _, err := AttachRequest(types.RelayFormatClaude, &dto.ClaudeRequest{Model: "claude-test"}, set, &convmeta.Options{})
	require.NoError(t, err)
	claudeReq := claude.(*dto.ClaudeRequest)
	tools := claudeReq.Tools.([]any)
	require.Len(t, tools, 1)
	assert.Equal(t, "apply_patch", tools[0].(*dto.Tool).Name)

	gemini, _, err := AttachRequest(types.RelayFormatGemini, &dto.GeminiChatRequest{}, set, &convmeta.Options{})
	require.NoError(t, err)
	geminiReq := gemini.(*dto.GeminiChatRequest)
	declarations := geminiFunctionDeclarations(t, geminiReq.GetTools())
	require.Len(t, declarations, 1)
	assert.Equal(t, "apply_patch", declarations[0]["name"])
	assert.Equal(t, "STRING", declarations[0]["parameters"].(map[string]any)["properties"].(map[string]any)["input"].(map[string]any)["type"])
	require.NotNil(t, geminiReq.ToolConfig)
	assert.Equal(t, []string{"apply_patch"}, geminiReq.ToolConfig.FunctionCallingConfig.AllowedFunctionNames)

	_, strictDiagnostics, err := AttachRequest(
		types.RelayFormatOpenAI,
		&dto.GeneralOpenAIRequest{Model: "gpt-test"},
		set,
		&convmeta.Options{ToolLossPolicy: types.ConversionLossPolicyStrict},
	)
	require.Error(t, err)
	var loss *types.ConversionLossError
	require.ErrorAs(t, err, &loss)
	assert.True(t, hasDiagnosticCode(strictDiagnostics, "custom_tool_as_function"))
}

func TestResponsesCustomToolNameConflictIsDropped(t *testing.T) {
	t.Parallel()

	req := &dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Tools: mustPolicyRaw(t, []map[string]any{
			{"type": "custom", "name": "exec"},
			{"type": "function", "name": "wait", "parameters": map[string]any{"type": "object"}},
			{"type": "function", "name": "exec", "parameters": map[string]any{"type": "object"}},
			{"type": "custom", "name": "apply_patch"},
			{"type": "custom", "name": "apply_patch"},
		}),
		ToolChoice: mustPolicyRaw(t, map[string]any{"type": "custom", "name": "exec"}),
	}
	_, set, err := ExtractRequest(types.RelayFormatOpenAIResponses, req)
	require.NoError(t, err)

	out, diagnostics, err := AttachRequest(types.RelayFormatOpenAI, &dto.GeneralOpenAIRequest{Model: "gpt-test"}, set, &convmeta.Options{})
	require.NoError(t, err)
	tools := out.(*dto.GeneralOpenAIRequest).Tools
	require.Len(t, tools, 3)
	assert.Equal(t, "wait", tools[0].Function.Name)
	assert.Equal(t, "exec", tools[1].Function.Name)
	assert.Equal(t, "object", tools[1].Function.Parameters.(map[string]any)["type"])
	assert.Equal(t, "apply_patch", tools[2].Function.Name)
	assert.Nil(t, out.(*dto.GeneralOpenAIRequest).ToolChoice)
	assert.True(t, hasDiagnosticCode(diagnostics, "custom_tool_name_conflict"))
	names := ResponsesCustomToolNames(set)
	_, hasExec := names["exec"]
	_, hasPatch := names["apply_patch"]
	assert.False(t, hasExec)
	assert.True(t, hasPatch)
}

func responsesCustomToolRequest(t *testing.T) *dto.OpenAIResponsesRequest {
	t.Helper()
	return &dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Tools: mustPolicyRaw(t, []map[string]any{
			{
				"type":        "custom",
				"name":        "apply_patch",
				"description": "Apply a patch",
				"format": map[string]any{
					"type":       "grammar",
					"syntax":     "lark",
					"definition": "start: /.+/\n",
				},
			},
		}),
		ToolChoice: mustPolicyRaw(t, map[string]any{"type": "custom", "name": "apply_patch"}),
	}
}

func mustPolicyRaw(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := kitutil.Marshal(value)
	require.NoError(t, err)
	return raw
}

func geminiFunctionDeclarations(t *testing.T, tools []dto.GeminiChatTool) []map[string]any {
	t.Helper()
	require.Len(t, tools, 1)
	raw, err := kitutil.Marshal(tools[0].FunctionDeclarations)
	require.NoError(t, err)
	var declarations []map[string]any
	require.NoError(t, kitutil.Unmarshal(raw, &declarations))
	return declarations
}
