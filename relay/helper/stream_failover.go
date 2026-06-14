package helper

import (
	"fmt"
	"net/http"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
)

func ErrorBeforeFirstStreamResponse(info *relaycommon.RelayInfo) *types.NewAPIError {
	if info == nil || info.HasClientStreamWrite() || info.StreamStatus == nil {
		return nil
	}
	if info.StreamStatus.EndReason == relaycommon.StreamEndReasonNone {
		return nil
	}
	reason := info.StreamStatus.EndReason
	if reason == relaycommon.StreamEndReasonDone {
		return nil
	}
	if info.ReceivedResponseCount > 0 && info.StreamStatus.IsNormalEnd() && !info.StreamStatus.HasErrors() {
		return nil
	}
	err := info.StreamStatus.EndError
	if err == nil {
		err = fmt.Errorf("stream ended before first response: %s", info.StreamStatus.Summary())
	}
	return types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
}
