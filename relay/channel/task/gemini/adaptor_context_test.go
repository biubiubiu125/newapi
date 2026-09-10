package gemini

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/stretchr/testify/require"
)

func TestTaskAdaptorFetchTaskContextCancelsRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	adaptor := &TaskAdaptor{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := adaptor.FetchTaskContext(ctx, server.URL, "key", map[string]any{
		"task_id": taskcommon.EncodeLocalTaskID("operations/task"),
	}, "")
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}
