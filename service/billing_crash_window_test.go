package service

import (
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestSettleStagesActualChargeBeforeMoneyMoves(t *testing.T) {
	truncate(t)
	const userID, tokenID = 9801, 9802
	const key = "sk-stage-before-apply"
	const requestID = "req-stage-before-apply"
	seedUser(t, userID, 500)
	seedToken(t, tokenID, userID, key, 500)
	require.NoError(t, model.ReserveWalletPreConsume(requestID, userID, 100))
	reserved, err := model.TryReserveTokenQuota(tokenID, key, 100, false)
	require.NoError(t, err)
	require.True(t, reserved)
	require.EqualValues(t, 400, getUserQuota(t, userID))
	require.EqualValues(t, 400, getTokenRemainQuota(t, tokenID))

	info := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		TokenKey:        key,
		RequestId:       requestID,
		OriginModelName: "test-model",
		UsingGroup:      "default",
	}
	staged := false
	model.BillingAdjustmentAfterEnsurePending = func() error {
		staged = true
		require.EqualValues(t, 400, getUserQuota(t, userID))
		return errors.New("stop after the charge is durable")
	}
	t.Cleanup(func() {
		model.BillingAdjustmentAfterEnsurePending = nil
	})
	session := &BillingSession{
		relayInfo: info,
		funding: &WalletFunding{
			userId:    userID,
			requestId: requestID,
			consumed:  100,
		},
		preConsumedQuota: 100,
		tokenConsumed:    100,
	}
	info.Billing = session

	require.ErrorIs(t, session.Settle(180), model.ErrBillingAdjustmentDeferred)
	require.True(t, staged)
	require.EqualValues(t, 400, getUserQuota(t, userID))
	require.EqualValues(t, 400, getTokenRemainQuota(t, tokenID))
	row, found, err := model.GetBillingAdjustment(BillingAdjustmentKey(info))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, model.BillingAdjustmentPending, row.Status)
	require.EqualValues(t, 180, row.ChargeQuota)
	require.EqualValues(t, 80, row.FundingDelta)

	model.BillingAdjustmentAfterEnsurePending = nil
	require.NoError(t, model.RecoverPendingBillingAdjustments(10))
	require.EqualValues(t, 320, getUserQuota(t, userID))
	require.EqualValues(t, 320, getTokenRemainQuota(t, tokenID))
}

func TestSubscriptionFundingPreConsumeArmsLease(t *testing.T) {
	truncate(t)
	user, sub := seedServiceLeaseSubscription(t, "sub-arm-live")
	funding := &SubscriptionFunding{
		requestId:  "sub-arm-live",
		userId:     user.Id,
		modelName:  "gpt-4o",
		usingGroup: "default",
		amount:     40,
	}
	t.Cleanup(funding.stopLease)

	require.NoError(t, funding.PreConsume(40))
	var record model.SubscriptionPreConsumeRecord
	require.NoError(t, model.DB.Where("request_id = ?", "sub-arm-live").First(&record).Error)
	require.Greater(t, record.LeaseUntil, int64(0))
	require.EqualValues(t, 40, mustServiceSubscriptionUsed(t, sub.Id))
}

func TestNodeLocalRecoveryRefundsExpiredSubscriptionPreConsume(t *testing.T) {
	truncate(t)
	_, sub := seedServiceLeaseSubscription(t, "sub-sweep-live")
	_, err := model.PreConsumeUserSubscription("sub-sweep-live", sub.UserId, "gpt-4o", 0, 40, "default")
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&model.SubscriptionPreConsumeRecord{}).Where("request_id = ?", "sub-sweep-live").Update("lease_until", 1).Error)

	recoverNodeLocalBillingAdjustments()

	require.EqualValues(t, 0, mustServiceSubscriptionUsed(t, sub.Id))
	var record model.SubscriptionPreConsumeRecord
	require.NoError(t, model.DB.Where("request_id = ?", "sub-sweep-live").First(&record).Error)
	require.Equal(t, "refunded", record.Status)
}

func seedServiceLeaseSubscription(t *testing.T, name string) (model.User, model.UserSubscription) {
	t.Helper()
	now := time.Now().Unix()
	user := model.User{
		Username: name,
		Password: "password123",
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, model.DB.Create(&user).Error)
	plan := model.SubscriptionPlan{
		Title:            name,
		Enabled:          true,
		TotalAmount:      100,
		QuotaResetPeriod: model.SubscriptionResetNever,
	}
	require.NoError(t, model.DB.Create(&plan).Error)
	sub := model.UserSubscription{
		UserId:      user.Id,
		PlanId:      plan.Id,
		AmountTotal: 100,
		AmountUsed:  0,
		StartTime:   now - 10,
		EndTime:     now + 3600,
		Status:      "active",
	}
	require.NoError(t, model.DB.Create(&sub).Error)
	return user, sub
}

func mustServiceSubscriptionUsed(t *testing.T, id int) int64 {
	t.Helper()
	var sub model.UserSubscription
	require.NoError(t, model.DB.Select("amount_used").First(&sub, id).Error)
	return sub.AmountUsed
}
