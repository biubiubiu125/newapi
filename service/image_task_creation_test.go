package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func imageTaskCreationContext(tokenQuota int) *gin.Context {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("token_quota", tokenQuota)
	return ctx
}

func seedImageTaskCreationWallet(t *testing.T, userID, tokenID, quota int) *relaycommon.RelayInfo {
	t.Helper()
	user := &model.User{
		Id:       userID,
		Username: "image-task-atomic-user",
		Password: "password123",
		Quota:    quota,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, model.DB.Create(user).Error)
	token := &model.Token{
		Id:          tokenID,
		UserId:      userID,
		Key:         "image-task-atomic-token",
		Name:        "image-task-atomic-token",
		Status:      common.TokenStatusEnabled,
		RemainQuota: quota,
	}
	require.NoError(t, model.DB.Create(token).Error)
	return &relaycommon.RelayInfo{
		UserId:          userID,
		UsingGroup:      "default",
		TokenId:         tokenID,
		TokenKey:        token.Key,
		RequestId:       "image-task-atomic-request",
		ForcePreConsume: true,
		UserSetting: dto.UserSetting{
			BillingPreference: "wallet_only",
		},
	}
}

func seedImageTaskCreationReservation(t *testing.T, userID int, clientTaskID, fingerprint string) *model.ImageTaskClientTaskIDLock {
	t.Helper()
	reservation, reserved, err := model.ReserveImageTaskClientTaskID(userID, clientTaskID, fingerprint)
	require.NoError(t, err)
	require.True(t, reserved)
	require.NotNil(t, reservation)
	return reservation
}

func newAtomicImageTask(userID int, clientTaskID string) *model.Task {
	return &model.Task{
		TaskID:       "image-task-atomic-public-id",
		Platform:     constant.TaskPlatformImage,
		UserId:       userID,
		ClientTaskID: clientTaskID,
		Group:        "default",
		Status:       model.TaskStatusQueued,
		Progress:     "0%",
	}
}

func TestCommitImageTaskCreationAtomicallyCommitsWalletBillingTaskAndReservation(t *testing.T) {
	truncate(t)
	const userID, tokenID, initialQuota, preConsumed = 11001, 11002, 1000, 120
	relayInfo := seedImageTaskCreationWallet(t, userID, tokenID, initialQuota)
	reservation := seedImageTaskCreationReservation(t, userID, "client-atomic-success", "fingerprint-success")
	task := newAtomicImageTask(userID, reservation.ClientTaskID)

	apiErr := CommitImageTaskCreation(
		imageTaskCreationContext(initialQuota), task, relayInfo, preConsumed, true, reservation,
	)

	require.Nil(t, apiErr)
	require.NotZero(t, task.ID)
	require.Equal(t, preConsumed, task.Quota)
	require.Equal(t, BillingSourceWallet, task.PrivateData.BillingSource)
	require.NotNil(t, relayInfo.Billing)

	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	require.Equal(t, initialQuota-preConsumed, user.Quota)
	var token model.Token
	require.NoError(t, model.DB.First(&token, tokenID).Error)
	require.Equal(t, initialQuota-preConsumed, token.RemainQuota)
	require.Equal(t, preConsumed, token.UsedQuota)

	lock, exists, err := model.GetImageTaskClientTaskIDLock(userID, reservation.ClientTaskID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, task.ID, lock.TaskPrimaryID)
	require.Equal(t, task.TaskID, lock.PublicTaskID)
}

func TestCommitImageTaskCreationThenRefundPreservesUnbookedUsageCounters(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID, initialQuota, preConsumed = 11003, 11004, 11005, 1000, 120
	const historicalUserUsage, historicalRequests, historicalChannelUsage = 700, 4, 900
	const historicalTokenUsage, historicalTokenRequests = 500, 3
	relayInfo := seedImageTaskCreationWallet(t, userID, tokenID, initialQuota)
	relayInfo.ChannelMeta = &relaycommon.ChannelMeta{ChannelId: channelID}
	seedChannel(t, channelID)
	setUserUsageCounters(t, userID, historicalUserUsage, historicalRequests)
	setChannelUsedQuota(t, channelID, historicalChannelUsage)
	require.NoError(t, model.DB.Create(&model.TokenUsageDaily{
		TokenId:      tokenID,
		Date:         tokenUsageDateForTest(t),
		UserId:       userID,
		Quota:        historicalTokenUsage,
		RequestCount: historicalTokenRequests,
	}).Error)
	reservation := seedImageTaskCreationReservation(t, userID, "client-create-refund-counters", "fingerprint-create-refund-counters")
	task := newAtomicImageTask(userID, reservation.ClientTaskID)
	task.ChannelId = channelID
	task.PrivateData.TokenId = tokenID

	apiErr := CommitImageTaskCreation(
		imageTaskCreationContext(initialQuota), task, relayInfo, preConsumed, true, reservation,
	)
	require.Nil(t, apiErr)
	require.NoError(t, RefundTaskQuota(t.Context(), task, "upstream image task failed"))

	userUsage, requestCount := getUserUsageCounters(t, userID)
	require.Equal(t, historicalUserUsage, userUsage)
	require.Equal(t, historicalRequests, requestCount)
	require.Equal(t, int64(historicalChannelUsage), getChannelUsedQuota(t, channelID))
	daily := getTokenUsageDaily(t, tokenID)
	require.Equal(t, historicalTokenUsage, daily.Quota)
	require.Equal(t, historicalTokenRequests, daily.RequestCount)
	require.Equal(t, initialQuota, getUserQuota(t, userID))
	require.Equal(t, initialQuota, getTokenRemainQuota(t, tokenID))
	require.Zero(t, getTokenUsedQuota(t, tokenID))
}

func TestCommitImageTaskCreationAtomicallyRollsBackWhenReservationBindingIsLost(t *testing.T) {
	truncate(t)
	const userID, tokenID, initialQuota, preConsumed = 11011, 11012, 1000, 120
	relayInfo := seedImageTaskCreationWallet(t, userID, tokenID, initialQuota)
	reservation := seedImageTaskCreationReservation(t, userID, "client-atomic-rollback", "fingerprint-rollback")
	require.NoError(t, model.DB.Delete(&model.ImageTaskClientTaskIDLock{}, reservation.ID).Error)
	task := newAtomicImageTask(userID, reservation.ClientTaskID)

	apiErr := CommitImageTaskCreation(
		imageTaskCreationContext(initialQuota), task, relayInfo, preConsumed, true, reservation,
	)

	require.NotNil(t, apiErr)
	var taskCount int64
	require.NoError(t, model.DB.Model(&model.Task{}).Where("task_id = ?", task.TaskID).Count(&taskCount).Error)
	require.Zero(t, taskCount)
	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	require.Equal(t, initialQuota, user.Quota)
	var token model.Token
	require.NoError(t, model.DB.First(&token, tokenID).Error)
	require.Equal(t, initialQuota, token.RemainQuota)
	require.Zero(t, token.UsedQuota)
	require.Nil(t, relayInfo.Billing)
}

// CommitImageTaskCreation 把 tx.Create(task) 提到扣费之前（避免大 blob 写入落在
// users/tokens 行锁窗口内）。这条用例锁住由此带来的新风险：扣费失败时任务行必须
// 随事务一起回滚，不能留下一条没有付费、却会被 worker 捞去执行的任务。
func TestCommitImageTaskCreationRollsBackTaskRowWhenBillingFails(t *testing.T) {
	truncate(t)
	const userID, tokenID, initialQuota, preConsumed = 11051, 11052, 50, 120
	relayInfo := seedImageTaskCreationWallet(t, userID, tokenID, initialQuota)
	reservation := seedImageTaskCreationReservation(t, userID, "client-atomic-insufficient", "fingerprint-insufficient")
	task := newAtomicImageTask(userID, reservation.ClientTaskID)

	apiErr := CommitImageTaskCreation(
		imageTaskCreationContext(initialQuota), task, relayInfo, preConsumed, true, reservation,
	)

	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())

	var taskCount int64
	require.NoError(t, model.DB.Model(&model.Task{}).Where("task_id = ?", task.TaskID).Count(&taskCount).Error)
	require.Zero(t, taskCount, "billing failure must not leave an unpaid task row behind")
	require.Zero(t, task.ID)
	require.Zero(t, task.Quota)

	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	require.Equal(t, initialQuota, user.Quota)
	var token model.Token
	require.NoError(t, model.DB.First(&token, tokenID).Error)
	require.Equal(t, initialQuota, token.RemainQuota)
	require.Zero(t, token.UsedQuota)
	require.Nil(t, relayInfo.Billing)

	// 预约未绑定，客户端可以用同一个键重新提交。
	lock, exists, err := model.GetImageTaskClientTaskIDLock(userID, reservation.ClientTaskID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Zero(t, lock.TaskPrimaryID)
}

func TestCommitImageTaskCreationAtomicallyBindsFreeTaskWithoutBilling(t *testing.T) {
	truncate(t)
	const userID = 11021
	reservation := seedImageTaskCreationReservation(t, userID, "client-atomic-free", "fingerprint-free")
	relayInfo := &relaycommon.RelayInfo{UserId: userID, UsingGroup: "default"}
	task := newAtomicImageTask(userID, reservation.ClientTaskID)

	apiErr := CommitImageTaskCreation(imageTaskCreationContext(0), task, relayInfo, 0, false, reservation)

	require.Nil(t, apiErr)
	require.NotZero(t, task.ID)
	require.Zero(t, task.Quota)
	require.Nil(t, relayInfo.Billing)
	lock, exists, err := model.GetImageTaskClientTaskIDLock(userID, reservation.ClientTaskID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, task.ID, lock.TaskPrimaryID)
}

func seedImageTaskCreationSubscription(t *testing.T, userID, planID, subscriptionID int, total, used int64, allowWalletOverflow bool) {
	t.Helper()
	plan := &model.SubscriptionPlan{
		Id:               planID,
		Title:            "image task atomic plan",
		Enabled:          true,
		DurationUnit:     model.SubscriptionDurationMonth,
		DurationValue:    1,
		TotalAmount:      total,
		QuotaResetPeriod: model.SubscriptionResetNever,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	subscription := &model.UserSubscription{
		Id:                  subscriptionID,
		UserId:              userID,
		PlanId:              planID,
		AmountTotal:         total,
		AmountUsed:          used,
		StartTime:           time.Now().Add(-time.Hour).Unix(),
		EndTime:             time.Now().Add(24 * time.Hour).Unix(),
		Status:              "active",
		AllowWalletOverflow: allowWalletOverflow,
	}
	require.NoError(t, model.DB.Create(subscription).Error)
}

func TestCommitImageTaskCreationAtomicallyCommitsSubscriptionBilling(t *testing.T) {
	truncate(t)
	const userID, tokenID, planID, subscriptionID = 11031, 11032, 11033, 11034
	const tokenQuota, preConsumed = 1000, 120
	relayInfo := seedImageTaskCreationWallet(t, userID, tokenID, tokenQuota)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Update("quota", 0).Error)
	seedImageTaskCreationSubscription(t, userID, planID, subscriptionID, 1000, 50, false)
	relayInfo.RequestId = "image-task-subscription-request"
	relayInfo.UserSetting.BillingPreference = "subscription_only"
	reservation := seedImageTaskCreationReservation(t, userID, "client-atomic-subscription", "fingerprint-subscription")
	task := newAtomicImageTask(userID, reservation.ClientTaskID)

	apiErr := CommitImageTaskCreation(
		imageTaskCreationContext(tokenQuota), task, relayInfo, preConsumed, true, reservation,
	)

	require.Nil(t, apiErr)
	require.Equal(t, BillingSourceSubscription, task.PrivateData.BillingSource)
	require.Equal(t, subscriptionID, task.PrivateData.SubscriptionId)
	var subscription model.UserSubscription
	require.NoError(t, model.DB.First(&subscription, subscriptionID).Error)
	require.Equal(t, int64(50+preConsumed), subscription.AmountUsed)
	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	require.Zero(t, user.Quota)
	var token model.Token
	require.NoError(t, model.DB.First(&token, tokenID).Error)
	require.Equal(t, tokenQuota-preConsumed, token.RemainQuota)
	var record model.SubscriptionPreConsumeRecord
	require.NoError(t, model.DB.Where("request_id = ?", relayInfo.RequestId).First(&record).Error)
	require.Equal(t, int64(preConsumed), record.PreConsumed)
}

func TestCommitImageTaskCreationAtomicallySubscriptionFirstFallsBackToWalletWithoutSubscription(t *testing.T) {
	truncate(t)
	const userID, tokenID, initialQuota, preConsumed = 11041, 11042, 1000, 120
	relayInfo := seedImageTaskCreationWallet(t, userID, tokenID, initialQuota)
	relayInfo.UserSetting.BillingPreference = "subscription_first"
	reservation := seedImageTaskCreationReservation(t, userID, "client-sub-first-wallet", "fingerprint-sub-first-wallet")
	task := newAtomicImageTask(userID, reservation.ClientTaskID)

	apiErr := CommitImageTaskCreation(
		imageTaskCreationContext(initialQuota), task, relayInfo, preConsumed, true, reservation,
	)

	require.Nil(t, apiErr)
	require.Equal(t, BillingSourceWallet, task.PrivateData.BillingSource)
	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	require.Equal(t, initialQuota-preConsumed, user.Quota)
}

func TestCommitImageTaskCreationAtomicallyWalletFirstFallsBackToSubscription(t *testing.T) {
	truncate(t)
	const userID, tokenID, planID, subscriptionID = 11051, 11052, 11053, 11054
	const tokenQuota, preConsumed = 1000, 120
	relayInfo := seedImageTaskCreationWallet(t, userID, tokenID, tokenQuota)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Update("quota", 10).Error)
	seedImageTaskCreationSubscription(t, userID, planID, subscriptionID, 1000, 50, true)
	relayInfo.RequestId = "image-task-wallet-first-request"
	relayInfo.UserSetting.BillingPreference = "wallet_first"
	reservation := seedImageTaskCreationReservation(t, userID, "client-wallet-first-sub", "fingerprint-wallet-first-sub")
	task := newAtomicImageTask(userID, reservation.ClientTaskID)

	apiErr := CommitImageTaskCreation(
		imageTaskCreationContext(tokenQuota), task, relayInfo, preConsumed, true, reservation,
	)

	require.Nil(t, apiErr)
	require.Equal(t, BillingSourceSubscription, task.PrivateData.BillingSource)
	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	require.Equal(t, 10, user.Quota)
	var subscription model.UserSubscription
	require.NoError(t, model.DB.First(&subscription, subscriptionID).Error)
	require.Equal(t, int64(50+preConsumed), subscription.AmountUsed)
}

func TestCommitImageTaskCreationAtomicallySubscriptionFirstHonorsNoWalletOverflow(t *testing.T) {
	truncate(t)
	const userID, tokenID, planID, subscriptionID = 11061, 11062, 11063, 11064
	const initialQuota, preConsumed = 1000, 120
	relayInfo := seedImageTaskCreationWallet(t, userID, tokenID, initialQuota)
	seedImageTaskCreationSubscription(t, userID, planID, subscriptionID, 100, 100, false)
	relayInfo.RequestId = "image-task-no-overflow-request"
	relayInfo.UserSetting.BillingPreference = "subscription_first"
	reservation := seedImageTaskCreationReservation(t, userID, "client-no-overflow", "fingerprint-no-overflow")
	task := newAtomicImageTask(userID, reservation.ClientTaskID)

	apiErr := CommitImageTaskCreation(
		imageTaskCreationContext(initialQuota), task, relayInfo, preConsumed, true, reservation,
	)

	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	require.Equal(t, initialQuota, user.Quota)
	var token model.Token
	require.NoError(t, model.DB.First(&token, tokenID).Error)
	require.Equal(t, initialQuota, token.RemainQuota)
}
