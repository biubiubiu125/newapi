package openaicompat

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/dto"
)

func ResponsesResponseToChatCompletionsResponse(resp *dto.OpenAIResponsesResponse, id string) (*dto.OpenAITextResponse, *dto.Usage, error) {
	if resp == nil {
		return nil, nil, errors.New("response is nil")
	}

	text := ExtractOutputTextFromResponses(resp)

	usage := &dto.Usage{}
	if resp.Usage != nil {
		if resp.Usage.InputTokens != 0 {
			usage.PromptTokens = resp.Usage.InputTokens
			usage.InputTokens = resp.Usage.InputTokens
		}
		if resp.Usage.OutputTokens != 0 {
			usage.CompletionTokens = resp.Usage.OutputTokens
			usage.OutputTokens = resp.Usage.OutputTokens
		}
		if resp.Usage.TotalTokens != 0 {
			usage.TotalTokens = resp.Usage.TotalTokens
		} else {
			usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
		}
		if resp.Usage.InputTokensDetails != nil {
			usage.PromptTokensDetails.CachedTokens = resp.Usage.InputTokensDetails.CachedTokens
			usage.PromptTokensDetails.ImageTokens = resp.Usage.InputTokensDetails.ImageTokens
			usage.PromptTokensDetails.AudioTokens = resp.Usage.InputTokensDetails.AudioTokens
		}
		if resp.Usage.CompletionTokenDetails.ReasoningTokens != 0 {
			usage.CompletionTokenDetails.ReasoningTokens = resp.Usage.CompletionTokenDetails.ReasoningTokens
		}
	}

	created := resp.CreatedAt

	var toolCalls []dto.ToolCallResponse
	if text == "" && len(resp.Output) > 0 {
		for _, out := range resp.Output {
			if out.Type != "function_call" {
				continue
			}
			name := strings.TrimSpace(out.Name)
			if name == "" {
				continue
			}
			callId := strings.TrimSpace(out.CallId)
			if callId == "" {
				callId = strings.TrimSpace(out.ID)
			}
			toolCalls = append(toolCalls, dto.ToolCallResponse{
				ID:   callId,
				Type: "function",
				Function: dto.FunctionResponse{
					Name:      name,
					Arguments: out.ArgumentsString(),
				},
			})
		}
	}

	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	msg := dto.Message{
		Role:    "assistant",
		Content: text,
	}
	if len(toolCalls) > 0 {
		msg.SetToolCalls(toolCalls)
		msg.Content = ""
	}

	out := &dto.OpenAITextResponse{
		Id:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   resp.Model,
		Choices: []dto.OpenAITextResponseChoice{
			{
				Index:        0,
				Message:      msg,
				FinishReason: finishReason,
			},
		},
		Usage: *usage,
	}

	return out, usage, nil
}

func ExtractOutputTextFromResponses(resp *dto.OpenAIResponsesResponse) string {
	if resp == nil || len(resp.Output) == 0 {
		return ""
	}

	var sb strings.Builder

	// Prefer assistant message outputs.
	for _, out := range resp.Output {
		if out.Type != "message" {
			continue
		}
		if out.Role != "" && out.Role != "assistant" {
			continue
		}
		for _, c := range out.Content {
			if c.Type == "output_text" && c.Text != "" {
				sb.WriteString(c.Text)
			}
		}
	}
	for _, out := range resp.Output {
		if out.Type == dto.ResponsesOutputTypeImageGenerationCall {
			appendResponsesImageGenerationOutput(&sb, out)
		}
	}
	if sb.Len() > 0 {
		return sb.String()
	}
	for _, out := range resp.Output {
		if out.Type == dto.ResponsesOutputTypeImageGenerationCall {
			appendResponsesImageGenerationOutput(&sb, out)
			continue
		}
		for _, c := range out.Content {
			if c.Text != "" {
				sb.WriteString(c.Text)
			}
		}
	}
	return sb.String()
}

func ExtractImageGenerationTextFromResponses(resp *dto.OpenAIResponsesResponse) string {
	if resp == nil || len(resp.Output) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, out := range resp.Output {
		if out.Type == dto.ResponsesOutputTypeImageGenerationCall {
			appendResponsesImageGenerationOutput(&sb, out)
		}
	}
	return sb.String()
}

func appendResponsesImageGenerationOutput(sb *strings.Builder, output dto.ResponsesOutput) {
	parts := responsesImageGenerationMarkdown(output)
	for _, part := range parts {
		appendResponsesOutputSeparator(sb)
		sb.WriteString(part)
	}
}

func appendResponsesOutputSeparator(sb *strings.Builder) {
	if sb.Len() > 0 {
		sb.WriteByte('\n')
	}
}

func responsesImageGenerationMarkdown(output dto.ResponsesOutput) []string {
	items := make([]string, 0)
	items = appendResponsesImageValue(items, output.Result)
	for _, result := range output.Results {
		items = appendResponsesImageValue(items, result)
	}
	items = appendResponsesImageValue(items, output.Url)
	items = appendResponsesImageRawValue(items, output.ImageUrl)
	items = appendResponsesImageValue(items, output.B64Json)
	for _, content := range output.Content {
		switch content.Type {
		case "output_text", "text":
			if content.Text != "" {
				items = append(items, content.Text)
			}
		default:
			items = appendResponsesImageValue(items, content.Text)
		}
		items = appendResponsesImageValue(items, content.Result)
		items = appendResponsesImageValue(items, content.Url)
		items = appendResponsesImageRawValue(items, content.ImageUrl)
		items = appendResponsesImageValue(items, content.B64Json)
	}
	return items
}

func appendResponsesImageValue(items []string, value string) []string {
	for _, imageValue := range responsesImageValues(value) {
		items = append(items, markdownForResponsesImageValue(imageValue))
	}
	return items
}

func appendResponsesImageRawValue(items []string, raw json.RawMessage) []string {
	rawValue := strings.TrimSpace(string(raw))
	if rawValue == "" || rawValue == "null" {
		return items
	}

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return appendResponsesImageValue(items, rawValue)
	}
	for _, imageValue := range collectResponsesImageValues(value) {
		items = append(items, markdownForResponsesImageValue(imageValue))
	}
	return items
}

func responsesImageValues(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if !strings.HasPrefix(value, "{") && !strings.HasPrefix(value, "[") {
		return []string{value}
	}

	var parsed any
	if err := json.Unmarshal([]byte(value), &parsed); err != nil {
		return []string{value}
	}
	return collectResponsesImageValues(parsed)
}

func collectResponsesImageValues(value any) []string {
	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []string{strings.TrimSpace(v)}
	case []any:
		items := make([]string, 0, len(v))
		for _, item := range v {
			items = append(items, collectResponsesImageValues(item)...)
		}
		return items
	case map[string]any:
		items := make([]string, 0)
		for _, key := range []string{"url", "image_url", "b64_json", "result", "results", "data"} {
			if item, ok := v[key]; ok {
				items = append(items, collectResponsesImageValues(item)...)
			}
		}
		return items
	default:
		return nil
	}
}

func markdownForResponsesImageValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "data:image/") {
		return fmt.Sprintf("![image](%s)", value)
	}
	return fmt.Sprintf("![image](data:image/png;base64,%s)", value)
}
