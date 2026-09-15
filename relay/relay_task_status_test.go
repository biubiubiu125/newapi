package relay

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/model"
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

func TestTaskSubmitFetchErrorHidesRawUpstreamBody(t *testing.T) {
	taskErr := taskSubmitFetchError(http.StatusBadGateway, []byte(`<!DOCTYPE html><html><body>pq: password authentication failed</body></html>`))

	require.NotNil(t, taskErr)
	require.Equal(t, "fail_to_fetch_task", taskErr.Code)
	require.Equal(t, http.StatusBadGateway, taskErr.StatusCode)
	require.Equal(t, "upstream request failed", taskErr.Message)
	require.NotContains(t, taskErr.Message, "password")
	require.NotContains(t, taskErr.Message, "DOCTYPE")
}

func TestTaskSubmitFetchErrorSanitizesStructuredUpstreamMessage(t *testing.T) {
	taskErr := taskSubmitFetchError(http.StatusBadGateway, []byte(`{"error":{"message":"pq: password authentication failed for user newapi"}}`))

	require.NotNil(t, taskErr)
	require.Equal(t, model.TaskPublicInternalFailReason, taskErr.Message)
	require.NotContains(t, taskErr.Message, "password")
}

func TestTaskSubmitFetchErrorKeepsShortPublicUpstreamMessage(t *testing.T) {
	taskErr := taskSubmitFetchError(http.StatusBadRequest, []byte(`{"error":{"message":"content policy violation"}}`))

	require.NotNil(t, taskErr)
	require.Equal(t, "content policy violation", taskErr.Message)
}
