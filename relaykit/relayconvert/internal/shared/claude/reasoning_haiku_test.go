package claude

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/stretchr/testify/require"
)

func TestHaiku5StaysKnownAndKeepsAdaptiveDefault(t *testing.T) {
	require.True(t, reasoning.IsKnownClaudeModel("claude-haiku-5"))
	require.True(t, reasoning.IsKnownClaudeModel("claude-haiku-5-20251001"))
	require.True(t, reasoning.IsKnownClaudeModel("claude-haiku-5-thinking"))
	require.True(t, reasoning.IsKnownClaudeModel("claude-fable-5"))
	require.True(t, reasoning.IsKnownClaudeModel("claude-mythos-5"))
	require.True(t, reasoning.IsKnownClaudeModel("claude-mythos-preview"))
	require.False(t, reasoning.IsKnownClaudeModel("claude-haiku-3"))

	req := &dto.ClaudeRequest{Model: "claude-haiku-5"}
	err := ApplyReasoning(context.Background(), req, nil, reasoning.Intent{Mode: reasoning.ModeAdaptive}, true)
	require.NoError(t, err)
	require.NotNil(t, req.Thinking)
	require.Equal(t, "adaptive", req.Thinking.Type)
	if len(req.OutputConfig) > 0 {
		var cfg map[string]any
		require.NoError(t, json.Unmarshal(req.OutputConfig, &cfg))
		require.NotEqual(t, "high", cfg["effort"])
	}
}
