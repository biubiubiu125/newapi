package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

func RecordConsumeAccountingError(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, phase string, err error) {
	if ctx == nil || relayInfo == nil || err == nil {
		return
	}
	channelId := 0
	if relayInfo.ChannelMeta != nil {
		channelId = relayInfo.ChannelMeta.ChannelId
	}
	useTimeSeconds := 0
	if !relayInfo.StartTime.IsZero() {
		useTimeSeconds = int(time.Since(relayInfo.StartTime).Seconds())
		if useTimeSeconds < 0 {
			useTimeSeconds = 0
		}
	}
	phase = strings.TrimSpace(phase)
	if phase == "" {
		phase = "consume accounting"
	}
	errMsg := strings.ReplaceAll(err.Error(), "\n", " ")
	other := map[string]interface{}{
		"accounting_error": true,
		"accounting_phase": phase,
		"error":            errMsg,
	}
	appendRequestPath(ctx, relayInfo, other)
	appendBillingInfo(relayInfo, other)
	model.RecordErrorLog(
		ctx,
		relayInfo.UserId,
		channelId,
		relayInfo.OriginModelName,
		ctx.GetString("token_name"),
		fmt.Sprintf("%s failed: %s", phase, errMsg),
		relayInfo.TokenId,
		useTimeSeconds,
		relayInfo.IsStream,
		relayInfo.UsingGroup,
		other,
	)
}
