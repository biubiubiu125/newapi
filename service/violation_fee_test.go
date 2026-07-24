package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestViolationFeeRollsBackDirectChargeWhenConsumeLogFailsAfterRequestRefund(t *testing.T) {
	truncate(t)
	useBrokenLogDB(t)

	const userID = 9781
	const tokenID = 9782
	const channelID = 9783
	const preConsumed = 100
	const initialQuota = 100000

	seedUser(t, userID, initialQuota-preConsumed)
	seedToken(t, tokenID, userID, "violation-token", initialQuota-preConsumed)
	seedChannel(t, channelID)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).Update("used_quota", preConsumed).Error)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("token_name", "violation-token")
	relayInfo := &relaycommon.RelayInfo{
		UserId:          userID,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
		TokenId:         tokenID,
		TokenKey:        "violation-token",
		OriginModelName: "grok-4",
		UsingGroup:      "default",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	relayInfo.Billing = &BillingSession{
		relayInfo:        relayInfo,
		funding:          &WalletFunding{userId: userID, consumed: preConsumed},
		preConsumedQuota: preConsumed,
		tokenConsumed:    preConsumed,
	}
	require.NoError(t, relayInfo.Billing.Refund(ctx))

	apiErr := types.NewErrorWithStatusCode(
		http.ErrAbortHandler,
		types.ErrorCodeViolationFeeGrokCSAM,
		http.StatusBadRequest,
	)
	charged := ChargeViolationFeeIfNeeded(ctx, relayInfo, apiErr)

	require.False(t, charged)
	var user model.User
	require.NoError(t, model.DB.Select("quota", "used_quota").First(&user, userID).Error)
	require.Equal(t, initialQuota, user.Quota)
	require.Zero(t, user.UsedQuota)
	var token model.Token
	require.NoError(t, model.DB.Select("remain_quota", "used_quota").First(&token, tokenID).Error)
	require.Equal(t, initialQuota, token.RemainQuota)
	require.Zero(t, token.UsedQuota)
}
