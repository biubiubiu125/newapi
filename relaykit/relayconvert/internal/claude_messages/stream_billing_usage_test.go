package claudemessages

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatClaudeResponseInfoPreservesNativeBillingAcrossStreamEvents(t *testing.T) {
	info := &ClaudeResponseInfo{}
	startUsage := &dto.ClaudeUsage{
		InputTokens: 10,
		BillingUsage: &dto.BillingUsage{
			Source:   dto.BillingUsageSourceClaudeMessages,
			Semantic: dto.BillingUsageSemanticAnthropic,
			ClaudeUsage: &dto.ClaudeUsage{
				InputTokens: 10,
				ServerToolUse: &dto.ClaudeServerToolUse{
					WebSearchRequests: 2,
				},
			},
		},
	}

	require.True(t, FormatClaudeResponseInfo(&dto.ClaudeResponse{
		Type: "message_start",
		Message: &dto.ClaudeMediaMessage{
			Id:    "msg_1",
			Model: "claude-test",
			Usage: startUsage,
		},
	}, nil, info))
	require.NotNil(t, info.Usage.BillingUsage)
	require.NotNil(t, info.Usage.BillingUsage.ClaudeUsage)
	require.NotNil(t, info.Usage.BillingUsage.ClaudeUsage.ServerToolUse)

	require.True(t, FormatClaudeResponseInfo(&dto.ClaudeResponse{
		Type: "message_delta",
		Usage: &dto.ClaudeUsage{
			OutputTokens: 42,
		},
	}, nil, info))

	require.NotNil(t, info.Usage)
	assert.Equal(t, 10, info.Usage.PromptTokens)
	assert.Equal(t, 42, info.Usage.CompletionTokens)
	require.NotNil(t, info.Usage.BillingUsage)
	require.NotNil(t, info.Usage.BillingUsage.ClaudeUsage)
	assert.Equal(t, 10, info.Usage.BillingUsage.ClaudeUsage.InputTokens)
	assert.Equal(t, 42, info.Usage.BillingUsage.ClaudeUsage.OutputTokens)
	require.NotNil(t, info.Usage.BillingUsage.ClaudeUsage.ServerToolUse)
	assert.Equal(t, 2, info.Usage.BillingUsage.ClaudeUsage.ServerToolUse.WebSearchRequests)

	FinalizeClaudeStreamBillingUsage(info)
	require.NotNil(t, info.Usage.BillingUsage.ClaudeUsage.ServerToolUse)
	assert.Equal(t, 2, info.Usage.BillingUsage.ClaudeUsage.ServerToolUse.WebSearchRequests)
}
