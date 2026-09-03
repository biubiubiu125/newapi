package reasoning

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
