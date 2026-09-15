package kitutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizePublicClientErrorStripsInternals(t *testing.T) {
	assert.Equal(t, PublicInternalFailReason, SanitizePublicClientError("sql: database is closed"))
	assert.Equal(t, PublicInternalFailReason, SanitizePublicClientError("ERROR: no such table: tasks (SQLSTATE 42P01)"))
	assert.Equal(t, PublicInternalFailReason, SanitizePublicClientError("pq: password authentication failed for user newapi"))
	assert.Equal(t, PublicAccountingFailReason, SanitizePublicClientError(
		"billing accounting failed after task submission: pq: password authentication failed"))
	assert.Equal(t, "content policy", SanitizePublicClientError("content policy"))
	assert.Equal(t, "image generation timed out", SanitizePublicClientError("image generation timed out"))
	assert.Empty(t, SanitizePublicClientError("https://cdn.example/video.mp4"))
}
