package controller

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeUpstreamErrorBodyRedactsSensitiveFields(t *testing.T) {
	body := []byte(`{
		"error": {
			"message": "invalid upstream request",
			"api_key": "sk-test",
			"nested": {
				"access_token": "token-test"
			}
		},
		"authorization": "Bearer secret-test"
	}`)

	result := sanitizeUpstreamErrorBody(body)

	require.Contains(t, result, "invalid upstream request")
	require.Contains(t, result, `"api_key":"[redacted]"`)
	require.Contains(t, result, `"access_token":"[redacted]"`)
	require.Contains(t, result, `"authorization":"[redacted]"`)
	require.NotContains(t, result, "sk-test")
	require.NotContains(t, result, "token-test")
	require.NotContains(t, result, "secret-test")
}

func TestSanitizeUpstreamErrorBodyTruncatesLongText(t *testing.T) {
	result := sanitizeUpstreamErrorBody([]byte(strings.Repeat("a", upstreamErrorBodyLimit+20)))

	require.Len(t, []rune(result), upstreamErrorBodyLimit+3)
	require.True(t, strings.HasSuffix(result, "..."))
}

func TestSanitizeUpstreamErrorBodyRedactsSensitivePlainText(t *testing.T) {
	body := []byte(`upstream rejected Authorization: Bearer sk-plain-secret-123456 with api_key=literal-secret-123`)

	result := sanitizeUpstreamErrorBody(body, "literal-secret-123")

	require.Contains(t, result, "upstream rejected")
	require.NotContains(t, result, "sk-plain-secret-123456")
	require.NotContains(t, result, "literal-secret-123")
	require.Contains(t, result, "[redacted]")
}

func TestSanitizeUpstreamErrorBodyRedactsSensitiveStringValues(t *testing.T) {
	body := []byte(`{
		"error": {
			"message": "upstream echoed Bearer literal-secret-456",
			"detail": "token=sk-json-secret-123456"
		}
	}`)

	result := sanitizeUpstreamErrorBody(body, "literal-secret-456")

	require.Contains(t, result, "upstream echoed")
	require.NotContains(t, result, "literal-secret-456")
	require.NotContains(t, result, "sk-json-secret-123456")
	require.Contains(t, result, "[redacted]")
}
