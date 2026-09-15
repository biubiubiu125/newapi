package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
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

func TestStringDataMarksClientStreamWriteFromRelayInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/stream", nil)
	info := &relaycommon.RelayInfo{}
	c.Set("relay_info", info)

	require.NoError(t, StringData(c, `{"delta":"hi"}`))

	assert.True(t, info.HasClientStreamWrite())
}

func TestClaudeChunkDataMarksClientStreamWriteFromRelayInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/stream", nil)
	info := &relaycommon.RelayInfo{}
	c.Set("relay_info", info)

	ClaudeChunkData(c, dto.ClaudeResponse{Type: "content_block_delta"}, `{"type":"content_block_delta"}`)

	assert.True(t, info.HasClientStreamWrite())
}

func TestShouldFailoverBeforeStreamDoneUsesFirstPayloadGuard(t *testing.T) {
	info := &relaycommon.RelayInfo{StreamStatus: relaycommon.NewStreamStatus()}
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonEOF, nil)

	err := ShouldFailoverBeforeStreamDone(info)

	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "stream ended before first response")
}
