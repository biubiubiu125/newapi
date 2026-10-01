package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestClientDeliveredWriterMarksSuccessfulBodyOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	info := &relaycommon.RelayInfo{}
	calls := 0
	info.SetClientDeliveredHook(func() error {
		calls++
		return nil
	})
	ctx.Set("relay_info", info)
	ctx.Writer = &clientDeliveredWriter{ResponseWriter: ctx.Writer, c: ctx}

	_, err := ctx.Writer.Write([]byte("ok"))
	require.NoError(t, err)
	_, err = ctx.Writer.Write([]byte("more"))
	require.NoError(t, err)

	require.Equal(t, 1, calls)
	require.True(t, info.HasClientStreamWrite())
}

func TestClientDeliveredWriterIgnoresErrorBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	info := &relaycommon.RelayInfo{}
	calls := 0
	info.SetClientDeliveredHook(func() error {
		calls++
		return nil
	})
	ctx.Set("relay_info", info)
	ctx.Writer = &clientDeliveredWriter{ResponseWriter: ctx.Writer, c: ctx}

	ctx.Writer.WriteHeader(http.StatusBadGateway)
	_, err := ctx.Writer.Write([]byte("bad"))
	require.NoError(t, err)
	require.Zero(t, calls)
	require.False(t, info.HasClientStreamWrite())
}

func TestStreamKeepaliveDoesNotMarkClientDelivered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	info := &relaycommon.RelayInfo{}
	calls := 0
	info.SetClientDeliveredHook(func() error {
		calls++
		return nil
	})
	ctx.Set("relay_info", info)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Writer = &clientDeliveredWriter{ResponseWriter: ctx.Writer, c: ctx}

	require.NoError(t, helper.PingData(ctx))
	require.NoError(t, writeTaskPluginProtocolHeartbeat(ctx))
	require.Contains(t, recorder.Body.String(), ": PING")
	require.Zero(t, calls)
	require.False(t, info.HasClientStreamWrite())

	info.StreamStatus = relaycommon.NewStreamStatus()
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, errors.New("upstream closed"))
	require.NotNil(t, helper.ErrorBeforeFirstStreamResponse(info))

	_, err := ctx.Writer.Write([]byte("data: {\"ok\":true}\n\n"))
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.True(t, info.HasClientStreamWrite())
	require.Nil(t, helper.ErrorBeforeFirstStreamResponse(info))
}
