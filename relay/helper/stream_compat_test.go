package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamDataMarksClientPayloadWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/stream", nil)
	info := &relaycommon.RelayInfo{}
	EnsureStreamStatus(c, info)

	require.NoError(t, StreamData(c, info, `{"ok":true}`))

	assert.True(t, info.HasClientStreamWrite())
	assert.Contains(t, w.Body.String(), `data: {"ok":true}`)
}

func TestShouldFailoverBeforeStreamDoneUsesFirstPayloadGuard(t *testing.T) {
	info := &relaycommon.RelayInfo{StreamStatus: relaycommon.NewStreamStatus()}
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonEOF, nil)

	err := ShouldFailoverBeforeStreamDone(info)

	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "stream ended before first response")
}
