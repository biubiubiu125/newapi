package service

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestNilTextUsageKeepsPreConsumeWhenPromptWasEstimated(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID int = 9710, 9711, 9712
	const preConsumed = 40
	seedUser(t, userID, 10000-preConsumed)
	seedChannel(t, channelID)
	seedToken(t, tokenID, userID, "text-nil-usage", 1000-preConsumed)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("username", "text-nil-usage-owner")
	ctx.Set("token_name", "text-nil-usage")
	relayInfo := &relaycommon.RelayInfo{
		UserId:          userID,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
		TokenId:         tokenID,
		TokenKey:        "text-nil-usage",
		OriginModelName: "gpt-4o",
		UsingGroup:      "default",
		PriceData:       typesPriceRatio(),
	}
	relayInfo.SetEstimatePromptTokens(1000)
	relayInfo.Billing = &BillingSession{
		relayInfo:        relayInfo,
		funding:          &WalletFunding{userId: userID, consumed: preConsumed},
		preConsumedQuota: preConsumed,
		tokenConsumed:    preConsumed,
	}

	err := PostTextConsumeQuotaChecked(ctx, relayInfo, nil, nil)
	require.NoError(t, err)
	require.EqualValues(t, 10000-preConsumed, getUserQuota(t, userID))
	require.EqualValues(t, 1000-preConsumed, getTokenRemainQuota(t, tokenID))
}

func TestNilTextUsageStillChargesModelPrice(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID int = 9713, 9714, 9715
	const preConsumed = 40
	seedUser(t, userID, 10000-preConsumed)
	seedChannel(t, channelID)
	seedToken(t, tokenID, userID, "text-nil-price", 1000-preConsumed)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("username", "text-nil-price-owner")
	ctx.Set("token_name", "text-nil-price")
	relayInfo := &relaycommon.RelayInfo{
		UserId:          userID,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
		TokenId:         tokenID,
		TokenKey:        "text-nil-price",
		OriginModelName: "gpt-4o",
		UsingGroup:      "default",
		PriceData: types.PriceData{
			UsePrice:       true,
			ModelPrice:     0.00008,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	relayInfo.SetEstimatePromptTokens(1000)
	relayInfo.Billing = &BillingSession{
		relayInfo:        relayInfo,
		funding:          &WalletFunding{userId: userID, consumed: preConsumed},
		preConsumedQuota: preConsumed,
		tokenConsumed:    preConsumed,
	}

	err := PostTextConsumeQuotaChecked(ctx, relayInfo, nil, nil)
	require.NoError(t, err)
	require.EqualValues(t, 10000-preConsumed, getUserQuota(t, userID))
	require.EqualValues(t, 1000-preConsumed, getTokenRemainQuota(t, tokenID))
}

func TestNilAudioUsageKeepsPreConsumeWhenPromptWasEstimated(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID int = 9716, 9717, 9718
	const preConsumed = 40
	seedUser(t, userID, 10000-preConsumed)
	seedChannel(t, channelID)
	seedToken(t, tokenID, userID, "audio-nil-usage", 1000-preConsumed)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("username", "audio-nil-usage-owner")
	ctx.Set("token_name", "audio-nil-usage")
	relayInfo := &relaycommon.RelayInfo{
		UserId:          userID,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
		TokenId:         tokenID,
		TokenKey:        "audio-nil-usage",
		OriginModelName: "gpt-4o-audio-preview",
		UsingGroup:      "default",
		PriceData:       typesPriceRatio(),
	}
	relayInfo.SetEstimatePromptTokens(1000)
	relayInfo.Billing = &BillingSession{
		relayInfo:        relayInfo,
		funding:          &WalletFunding{userId: userID, consumed: preConsumed},
		preConsumedQuota: preConsumed,
		tokenConsumed:    preConsumed,
	}

	err := PostAudioConsumeQuota(ctx, relayInfo, nil, "")
	require.NoError(t, err)
	require.EqualValues(t, 10000-preConsumed, getUserQuota(t, userID))
	require.EqualValues(t, 1000-preConsumed, getTokenRemainQuota(t, tokenID))
}

func TestWalletFirstUsesDatabaseWhenCacheIsStaleLow(t *testing.T) {
	truncate(t)
	useServiceMiniRedis(t)
	const userID, tokenID, subID = 9720, 9721, 9722
	seedUser(t, userID, 500)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Update("auth_version", 1).Error)
	seedToken(t, tokenID, userID, "sk-stale-low", 1000)
	seedSubscription(t, subID, userID, 1000, 0)
	_, err := model.GetUserCache(userID)
	require.NoError(t, err)
	require.NoError(t, common.RDB.HSet(t.Context(), fmt.Sprintf("user:%d", userID), "Quota", "10").Err())

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		TokenKey:        "sk-stale-low",
		RequestId:       "wallet-stale-low",
		ForcePreConsume: true,
		UserSetting: dto.UserSetting{
			BillingPreference: "wallet_first",
		},
	}
	session, apiErr := NewBillingSession(ctx, info, 100)
	require.Nil(t, apiErr)
	require.NotNil(t, session)
	require.Equal(t, BillingSourceWallet, session.funding.Source())
	require.EqualValues(t, 400, getUserQuota(t, userID))
	require.EqualValues(t, 0, subscriptionAmountUsed(t, subID))
	require.Equal(t, model.WalletPreConsumeReserved, walletPreConsumeStatus(t, "wallet-stale-low"))
}

func TestExpiredWalletPreConsumeThenSettleChargesFullAmount(t *testing.T) {
	truncate(t)
	const userID, tokenID = 9730, 9731
	seedUser(t, userID, 500)
	seedToken(t, tokenID, userID, "sk-wallet-full", 1000)
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		TokenKey:        "sk-wallet-full",
		RequestId:       "wallet-session-full",
		ForcePreConsume: true,
		UserSetting:     dto.UserSetting{BillingPreference: "wallet_only"},
	}
	session, apiErr := NewBillingSession(ctx, info, 100)
	require.Nil(t, apiErr)
	require.EqualValues(t, 400, getUserQuota(t, userID))
	require.NoError(t, model.DB.Model(&model.WalletPreConsumeRecord{}).Where("request_id = ?", "wallet-session-full").Update("lease_until", 1).Error)
	require.NoError(t, model.RecoverExpiredWalletPreConsumes(10))
	require.EqualValues(t, 500, getUserQuota(t, userID))

	require.NoError(t, session.Settle(130))
	require.EqualValues(t, 370, getUserQuota(t, userID))
	require.EqualValues(t, 870, getTokenRemainQuota(t, tokenID))
}

func TestWalletPreConsumeSettleZeroDoesNotRefundLater(t *testing.T) {
	truncate(t)
	const userID, tokenID = 9732, 9733
	seedUser(t, userID, 500)
	seedToken(t, tokenID, userID, "sk-wallet-zero", 1000)
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		TokenKey:        "sk-wallet-zero",
		RequestId:       "wallet-session-zero",
		ForcePreConsume: true,
		UserSetting:     dto.UserSetting{BillingPreference: "wallet_only"},
	}
	session, apiErr := NewBillingSession(ctx, info, 100)
	require.Nil(t, apiErr)
	require.NoError(t, session.Settle(100))
	require.EqualValues(t, 400, getUserQuota(t, userID))
	require.Equal(t, model.WalletPreConsumeSettled, walletPreConsumeStatus(t, "wallet-session-zero"))
	require.NoError(t, model.DB.Model(&model.WalletPreConsumeRecord{}).Where("request_id = ?", "wallet-session-zero").Update("lease_until", 1).Error)
	require.NoError(t, model.RecoverExpiredWalletPreConsumes(10))
	require.EqualValues(t, 400, getUserQuota(t, userID))
}

func TestCappedWalletRefundDoesNotReportSuccess(t *testing.T) {
	truncate(t)
	const userID = 9740
	seedUser(t, userID, 0)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Update("quota", common.MaxWalletQuota).Error)

	relayInfo := &relaycommon.RelayInfo{UserId: userID, IsPlayground: true}
	err := PostConsumeQuota(relayInfo, -10, 0, false)
	require.ErrorIs(t, err, model.ErrUserQuotaCap)
	require.EqualValues(t, common.MaxWalletQuota, getUserQuota(t, userID))

	err = taskAdjustFunding(&model.Task{UserId: userID}, -10)
	require.ErrorIs(t, err, model.ErrUserQuotaCap)
	require.EqualValues(t, common.MaxWalletQuota, getUserQuota(t, userID))

	ok := RefundMidjourneyQuota(context.Background(), &model.Midjourney{UserId: userID, Quota: 10, MjId: "mj-cap"}, "cap")
	require.False(t, ok)
	require.EqualValues(t, common.MaxWalletQuota, getUserQuota(t, userID))

	err = adjustMidjourneyFunding(&model.Midjourney{UserId: userID, Quota: 10}, -10)
	require.ErrorIs(t, err, model.ErrUserQuotaCap)
	require.EqualValues(t, common.MaxWalletQuota, getUserQuota(t, userID))
}

func typesPriceRatio() types.PriceData {
	return types.PriceData{
		ModelRatio:     1,
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}
}

func useServiceMiniRedis(t *testing.T) {
	t.Helper()
	server := miniredis.RunT(t)
	oldEnabled := common.RedisEnabled
	oldRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RedisEnabled = oldEnabled
		common.RDB = oldRDB
	})
}

func subscriptionAmountUsed(t *testing.T, id int) int64 {
	t.Helper()
	var used int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("id = ?", id).Select("amount_used").Scan(&used).Error)
	return used
}

func walletPreConsumeStatus(t *testing.T, requestID string) string {
	t.Helper()
	var record model.WalletPreConsumeRecord
	require.NoError(t, model.DB.Where("request_id = ?", requestID).First(&record).Error)
	return record.Status
}
