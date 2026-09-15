package types

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestToOpenAIErrorHidesHostDatabaseInternals(t *testing.T) {
	apiErr := NewError(errors.New("sql: database is closed"), ErrorCodeQueryDataError)

	message := apiErr.ToOpenAIError().Message
	require.Equal(t, "task failed", message)
	require.NotContains(t, message, "sql")
	require.NotContains(t, message, "database")
	require.Equal(t, "sql: database is closed", apiErr.Error())
}

func TestToOpenAIErrorHidesBillingPersistInternals(t *testing.T) {
	apiErr := NewError(errors.New("ERROR: password authentication failed (SQLSTATE 28P01)"), ErrorCodeUpdateDataError)

	message := apiErr.ToOpenAIError().Message
	require.Equal(t, "task failed", message)
	require.NotContains(t, message, "password")
	require.NotContains(t, message, "SQLSTATE")
}

func TestToOpenAIErrorKeepsExactTimeoutMessage(t *testing.T) {
	apiErr := NewErrorWithStatusCode(errors.New("image generation timed out"), ErrorCodeDoRequestFailed, http.StatusGatewayTimeout)

	require.Equal(t, "image generation timed out", apiErr.ToOpenAIError().Message)
	require.Equal(t, http.StatusGatewayTimeout, apiErr.StatusCode)
}

func TestToOpenAIErrorKeepsUpstreamProviderMessage(t *testing.T) {
	apiErr := NewOpenAIError(errors.New("Your request was rejected by the safety system"), ErrorCodeBadResponseStatusCode, http.StatusBadRequest)

	require.Equal(t, "Your request was rejected by the safety system", apiErr.ToOpenAIError().Message)
}
