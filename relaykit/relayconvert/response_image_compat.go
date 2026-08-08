package relayconvert

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

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
	for _, part := range responsesImageGenerationMarkdown(output) {
		appendResponsesImageOutputSeparator(sb)
		sb.WriteString(part)
	}
}

func appendResponsesImageOutputSeparator(sb *strings.Builder) {
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
	if err := kitutil.Unmarshal(raw, &value); err != nil {
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
	if err := kitutil.Unmarshal([]byte(value), &parsed); err != nil {
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
