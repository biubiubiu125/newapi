package coze

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type errAfterReader struct {
	data []byte
	err  error
}

func (r *errAfterReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func (r *errAfterReader) Close() error { return nil }

func TestCozeChatStreamHandlerKeepsUsageAfterClientWriteWhenScannerFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "coze-bot"},
	}
	ctx.Set("relay_info", info)

	body := "event: conversation.message.delta\ndata: {\"content\":\"\\\"hello\\\"\"}\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       &errAfterReader{data: []byte(body), err: errors.New("stream truncated")},
		Header:     make(http.Header),
	}

	usage, apiErr := cozeChatStreamHandler(ctx, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.True(t, info.HasClientStreamWrite())
	require.True(t, strings.Contains(recorder.Body.String(), "hello"))
}
