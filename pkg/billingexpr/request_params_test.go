package billingexpr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCaptureRequestHeadersOnlyKeepsExpressionReferences(t *testing.T) {
	headers, err := CaptureRequestHeaders(
		`has(header("X-Billing-Tier"), "fast") ? param("quality") : header("x-region")`,
		map[string]string{
			"X-Billing-Tier": "fast",
			"X-Region":       "us-east",
			"X-Trace-Secret": "must-not-persist",
		},
	)

	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"x-billing-tier": "fast",
		"x-region":       "us-east",
	}, headers)
}

func TestReferencedRequestHeadersRejectsDynamicName(t *testing.T) {
	_, err := ReferencedRequestHeaders(`header(param("header_name"))`)

	require.ErrorContains(t, err, "string literal")
}
