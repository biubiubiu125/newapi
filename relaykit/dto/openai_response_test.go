package dto

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOpenAIResponsesResponseImageGenerationMetadata(t *testing.T) {
	resp := &OpenAIResponsesResponse{
		Output: []ResponsesOutput{
			{Type: "message", Quality: "ignored", Size: "ignored"},
			{Type: ResponsesOutputTypeImageGenerationCall, Quality: "high", Size: "1024x1024"},
		},
	}

	assert.True(t, resp.HasImageGenerationCall())
	assert.Equal(t, "high", resp.GetQuality())
	assert.Equal(t, "1024x1024", resp.GetSize())
}

func TestOpenAIResponsesResponseImageGenerationMetadataEmpty(t *testing.T) {
	resp := &OpenAIResponsesResponse{Output: []ResponsesOutput{{Type: "message"}}}

	assert.False(t, resp.HasImageGenerationCall())
	assert.Empty(t, resp.GetQuality())
	assert.Empty(t, resp.GetSize())
}
