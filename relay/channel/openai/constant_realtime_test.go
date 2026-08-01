package openai

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModelListIncludesRealtimeGAModels(t *testing.T) {
	for _, model := range []string{
		"gpt-realtime-2",
		"gpt-realtime-2.1",
		"gpt-realtime-2.1-mini",
		"gpt-realtime-whisper",
		"gpt-realtime-translate",
	} {
		assert.Contains(t, ModelList, model)
	}
}
