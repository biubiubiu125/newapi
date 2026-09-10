package vertex

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTaskAdaptorFetchTaskContextRejectsCancelledContextBeforeProviderCall(t *testing.T) {
	adaptor := &TaskAdaptor{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := adaptor.FetchTaskContext(ctx, "https://provider.example", `{}`, map[string]any{
		"task_id": "not-a-valid-task-id",
	}, "")
	require.Error(t, err)
}
