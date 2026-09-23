package passkey

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestCreateSessionDataFlowRejectsEmptySessionWithLocalizedError(t *testing.T) {
	_, _, err := CreateSessionDataFlow("passkey_register", 1, "session", "scope", nil)
	loc, ok := common.AsLocalizedError(err)
	require.True(t, ok, "empty session should be LocalizedError, got %v", err)
	require.Equal(t, "passkey.session_empty", loc.Key)
}
