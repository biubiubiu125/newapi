package relay

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRelayTaskFetchRejectsUnknownModeWithoutPanicking(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())

	require.NotPanics(t, func() {
		taskErr := RelayTaskFetch(context, -1)
		require.NotNil(t, taskErr)
		require.Equal(t, "invalid_relay_mode", taskErr.Code)
		require.Equal(t, 400, taskErr.StatusCode)
	})
}
