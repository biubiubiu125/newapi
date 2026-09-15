package helper

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

func EnsureStreamStatus(c *gin.Context, info *relaycommon.RelayInfo) {
	if info == nil && c != nil {
		if raw, exists := c.Get("relay_info"); exists {
			info, _ = raw.(*relaycommon.RelayInfo)
		}
	}
	if info == nil {
		return
	}
	if info.StreamStatus == nil {
		info.StreamStatus = relaycommon.NewStreamStatus()
	}
	if c != nil {
		if _, exists := c.Get("relay_info"); !exists {
			c.Set("relay_info", info)
		}
	}
}

func MarkStreamEnd(info *relaycommon.RelayInfo, reason relaycommon.StreamEndReason, err error) {
	if info == nil {
		return
	}
	if info.StreamStatus == nil {
		info.StreamStatus = relaycommon.NewStreamStatus()
	}
	info.StreamStatus.SetEndReason(reason, err)
}

func markClientStreamWriteFromContext(c *gin.Context) {
	if c == nil {
		return
	}
	raw, exists := c.Get("relay_info")
	if !exists {
		return
	}
	info, ok := raw.(*relaycommon.RelayInfo)
	if !ok || info == nil {
		return
	}
	info.MarkClientStreamWrite()
}

func StreamData(c *gin.Context, info *relaycommon.RelayInfo, data string) error {
	if err := StringData(c, data); err != nil {
		return err
	}
	if info != nil {
		info.MarkClientStreamWrite()
	}
	return nil
}

func StreamDone(c *gin.Context, info *relaycommon.RelayInfo) error {
	MarkStreamEnd(info, relaycommon.StreamEndReasonDone, nil)
	return StringData(c, "[DONE]")
}

func ShouldFailoverBeforeStreamDone(info *relaycommon.RelayInfo) *types.NewAPIError {
	return ErrorBeforeFirstStreamResponse(info)
}
