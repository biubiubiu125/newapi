package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestSensitiveWordsErrorIsClientErrorAndSkipsRetry(t *testing.T) {
	err := newSensitiveWordsError()

	require.Equal(t, http.StatusBadRequest, err.StatusCode)
	require.Equal(t, types.ErrorCodeSensitiveWordsDetected, err.GetErrorCode())
	require.True(t, types.IsSkipRetryError(err))
}
