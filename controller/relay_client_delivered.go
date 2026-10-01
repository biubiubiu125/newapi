package controller

import (
	"bytes"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

// clientDeliveredWriter remembers the first successful response body so a crash
// before settlement cannot refund a wallet reserve the client already received.
type clientDeliveredWriter struct {
	gin.ResponseWriter
	c *gin.Context
}

func (w *clientDeliveredWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if n > 0 && err == nil && !isSSEKeepalive(p[:n]) {
		status := w.Status()
		if status >= 200 && status < 300 {
			markRelayClientDelivered(w.c)
		}
	}
	return n, err
}

// isSSEKeepalive reports a write that is only SSE comment lines, such as
// ": PING". Those bytes keep the connection open and are not response content.
func isSSEKeepalive(p []byte) bool {
	if len(bytes.TrimSpace(p)) == 0 {
		return false
	}
	sawComment := false
	for _, line := range bytes.Split(p, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		if len(line) == 0 {
			continue
		}
		if line[0] != ':' {
			return false
		}
		sawComment = true
	}
	return sawComment
}

func (w *clientDeliveredWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func markRelayClientDelivered(c *gin.Context) {
	if c == nil {
		return
	}
	raw, ok := c.Get("relay_info")
	if !ok {
		return
	}
	info, ok := raw.(*relaycommon.RelayInfo)
	if !ok || info == nil {
		return
	}
	info.MarkClientStreamWrite()
}
