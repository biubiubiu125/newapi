package relayconvert

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestExtractImageGenerationTextFromResponsesIncludesImageGenerationResult(t *testing.T) {
	resp := &dto.OpenAIResponsesResponse{
		Output: []dto.ResponsesOutput{
			{
				Type:   dto.ResponsesOutputTypeImageGenerationCall,
				Result: "https://example.com/image.png",
			},
		},
	}

	text := ExtractImageGenerationTextFromResponses(resp)

	require.Equal(t, "![image](https://example.com/image.png)", text)
}

func TestExtractImageGenerationTextFromResponsesReadsImageResults(t *testing.T) {
	resp := &dto.OpenAIResponsesResponse{
		Output: []dto.ResponsesOutput{
			{
				Type: "message",
				Role: "assistant",
				Content: []dto.ResponsesOutputContent{
					{Type: "output_text", Text: "done"},
				},
			},
			{
				Type:    dto.ResponsesOutputTypeImageGenerationCall,
				Results: []string{"abc123"},
			},
		},
	}

	text := ExtractImageGenerationTextFromResponses(resp)

	require.Equal(t, "![image](data:image/png;base64,abc123)", text)
}

func TestExtractImageGenerationTextFromResponsesReadsCommonImageFields(t *testing.T) {
	resp := &dto.OpenAIResponsesResponse{
		Output: []dto.ResponsesOutput{
			{
				Type:     dto.ResponsesOutputTypeImageGenerationCall,
				ImageUrl: []byte(`{"url":"https://example.com/from-image-url.png"}`),
				Content: []dto.ResponsesOutputContent{
					{
						Type:    "image",
						B64Json: "abc123",
					},
				},
			},
		},
	}

	text := ExtractImageGenerationTextFromResponses(resp)

	require.Equal(t, "![image](https://example.com/from-image-url.png)\n![image](data:image/png;base64,abc123)", text)
}

func TestExtractImageGenerationTextFromResponsesSkipsAssistantText(t *testing.T) {
	resp := &dto.OpenAIResponsesResponse{
		Output: []dto.ResponsesOutput{
			{
				Type: "message",
				Role: "assistant",
				Content: []dto.ResponsesOutputContent{
					{Type: "output_text", Text: "done"},
				},
			},
			{
				Type:   dto.ResponsesOutputTypeImageGenerationCall,
				Result: "https://example.com/image.png",
			},
		},
	}

	text := ExtractImageGenerationTextFromResponses(resp)

	require.Equal(t, "![image](https://example.com/image.png)", text)
}
