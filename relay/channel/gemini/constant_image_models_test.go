package gemini

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModelListIncludesGeminiGAImageModels(t *testing.T) {
	for _, model := range []string{
		"gemini-3-pro-image",
		"gemini-3.1-flash-image",
	} {
		assert.Contains(t, ModelList, model)
	}
}
