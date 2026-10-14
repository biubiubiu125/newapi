package reasoning

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestThinkingSuffixToggleLeavesEffortSuffixesUnchangedWhenDisabled(t *testing.T) {
	t.Parallel()

	base, intent, found, err := ParseClaudeModelSuffix("claude-opus-4-6-high", false)
	assert.NoError(t, err)
	assert.False(t, found)
	assert.Empty(t, intent.Effort)
	assert.Equal(t, "claude-opus-4-6-high", base)

	base, intent, found, err = ParseClaudeModelSuffix("claude-opus-4-6-high", true)
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "claude-opus-4-6", base)
	assert.NotEmpty(t, intent.Effort)

	base, intent, found, err = ParseGeminiModelSuffix("gemini-2.5-flash-high", false)
	assert.NoError(t, err)
	assert.False(t, found)
	assert.Empty(t, intent.Effort)
	assert.Equal(t, "gemini-2.5-flash-high", base)

	base, _, found, err = ParseGeminiModelSuffix("gemini-2.5-flash-high", true)
	assert.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "gemini-2.5-flash", base)
}

func TestParseOpenAIReasoningEffortFromModelSuffixSupportsMaxAndPreservesRealModelIDs(t *testing.T) {
	t.Parallel()

	effort, base := ParseOpenAIReasoningEffortFromModelSuffix("gpt-5.6-sol-max")
	assert.Equal(t, "max", effort)
	assert.Equal(t, "gpt-5.6-sol", base)

	preserve := func(model string) bool {
		return model == "qwen-max" || model == "vendor/qwen-max"
	}
	effort, base = ParseOpenAIReasoningEffortFromModelSuffix("vendor/qwen-max", preserve)
	assert.Empty(t, effort)
	assert.Equal(t, "vendor/qwen-max", base)
}
