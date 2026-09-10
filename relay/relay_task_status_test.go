package relay

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTaskSubmissionAcceptsAllSuccessfulHTTPStatuses(t *testing.T) {
	for _, status := range []int{
		http.StatusOK,
		http.StatusCreated,
		http.StatusAccepted,
		http.StatusNoContent,
	} {
		require.Truef(t, isSuccessfulTaskSubmissionStatus(status), "status %d should be accepted", status)
	}
	require.False(t, isSuccessfulTaskSubmissionStatus(http.StatusBadGateway))
}
