package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestExpireStalePendingRechargeOrders(t *testing.T) {
	truncateTables(t)

	now := common.GetTimestamp()
	oldCreateTime := now - DefaultPendingOrderExpireSeconds - 60
	freshCreateTime := now - DefaultPendingOrderExpireSeconds + 60

	require.NoError(t, DB.Create(&TopUp{UserId: 1, TradeNo: "topup-old-pending", Status: common.TopUpStatusPending, CreateTime: oldCreateTime}).Error)
	require.NoError(t, DB.Create(&TopUp{UserId: 1, TradeNo: "topup-fresh-pending", Status: common.TopUpStatusPending, CreateTime: freshCreateTime}).Error)
	require.NoError(t, DB.Create(&TopUp{UserId: 1, TradeNo: "topup-old-success", Status: common.TopUpStatusSuccess, CreateTime: oldCreateTime}).Error)
	require.NoError(t, DB.Create(&SubscriptionOrder{UserId: 1, TradeNo: "sub-old-pending", Status: common.TopUpStatusPending, CreateTime: oldCreateTime}).Error)
	require.NoError(t, DB.Create(&SubscriptionOrder{UserId: 1, TradeNo: "sub-fresh-pending", Status: common.TopUpStatusPending, CreateTime: freshCreateTime}).Error)

	expiredTopUps, expiredSubscriptionOrders, err := ExpireStalePendingRechargeOrders(DefaultPendingOrderExpireSeconds, 100)
	require.NoError(t, err)
	require.Equal(t, 1, expiredTopUps)
	require.Equal(t, 1, expiredSubscriptionOrders)

	var oldTopUp TopUp
	require.NoError(t, DB.Where("trade_no = ?", "topup-old-pending").First(&oldTopUp).Error)
	require.Equal(t, common.TopUpStatusExpired, oldTopUp.Status)
	require.Greater(t, oldTopUp.CompleteTime, int64(0))

	var freshTopUp TopUp
	require.NoError(t, DB.Where("trade_no = ?", "topup-fresh-pending").First(&freshTopUp).Error)
	require.Equal(t, common.TopUpStatusPending, freshTopUp.Status)

	var successTopUp TopUp
	require.NoError(t, DB.Where("trade_no = ?", "topup-old-success").First(&successTopUp).Error)
	require.Equal(t, common.TopUpStatusSuccess, successTopUp.Status)

	var oldSubscriptionOrder SubscriptionOrder
	require.NoError(t, DB.Where("trade_no = ?", "sub-old-pending").First(&oldSubscriptionOrder).Error)
	require.Equal(t, common.TopUpStatusExpired, oldSubscriptionOrder.Status)
	require.Greater(t, oldSubscriptionOrder.CompleteTime, int64(0))

	var freshSubscriptionOrder SubscriptionOrder
	require.NoError(t, DB.Where("trade_no = ?", "sub-fresh-pending").First(&freshSubscriptionOrder).Error)
	require.Equal(t, common.TopUpStatusPending, freshSubscriptionOrder.Status)
}

func TestUpdatePendingTopUpStatusSetsCompleteTimeForExpiredOrder(t *testing.T) {
	truncateTables(t)

	require.NoError(t, DB.Create(&TopUp{
		UserId:          1,
		TradeNo:         "topup-manual-expired",
		PaymentProvider: PaymentProviderEpay,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}).Error)

	require.NoError(t, UpdatePendingTopUpStatus("topup-manual-expired", PaymentProviderEpay, common.TopUpStatusExpired))

	var topUp TopUp
	require.NoError(t, DB.Where("trade_no = ?", "topup-manual-expired").First(&topUp).Error)
	require.Equal(t, common.TopUpStatusExpired, topUp.Status)
	require.Greater(t, topUp.CompleteTime, int64(0))
}

func TestCleanupExpiredRechargeOrders(t *testing.T) {
	truncateTables(t)

	now := common.GetTimestamp()
	oldCreateTime := now - DefaultExpiredOrderRetentionSeconds - 60
	oldCompleteTime := now - DefaultExpiredOrderRetentionSeconds - 60
	freshCompleteTime := now - DefaultExpiredOrderRetentionSeconds + 60

	require.NoError(t, DB.Create(&TopUp{UserId: 1, TradeNo: "topup-old-complete-expired", Status: common.TopUpStatusExpired, CreateTime: oldCreateTime, CompleteTime: oldCompleteTime}).Error)
	require.NoError(t, DB.Create(&TopUp{UserId: 1, TradeNo: "topup-old-create-expired", Status: common.TopUpStatusExpired, CreateTime: oldCreateTime}).Error)
	require.NoError(t, DB.Create(&TopUp{UserId: 1, TradeNo: "topup-fresh-expired", Status: common.TopUpStatusExpired, CreateTime: oldCreateTime, CompleteTime: freshCompleteTime}).Error)
	require.NoError(t, DB.Create(&TopUp{UserId: 1, TradeNo: "topup-old-pending", Status: common.TopUpStatusPending, CreateTime: oldCreateTime}).Error)
	require.NoError(t, DB.Create(&SubscriptionOrder{UserId: 1, TradeNo: "sub-old-complete-expired", Status: common.TopUpStatusExpired, CreateTime: oldCreateTime, CompleteTime: oldCompleteTime}).Error)
	require.NoError(t, DB.Create(&SubscriptionOrder{UserId: 1, TradeNo: "sub-fresh-expired", Status: common.TopUpStatusExpired, CreateTime: oldCreateTime, CompleteTime: freshCompleteTime}).Error)

	deletedTopUps, deletedSubscriptionOrders, err := CleanupExpiredRechargeOrders(DefaultExpiredOrderRetentionSeconds, 100)
	require.NoError(t, err)
	require.Equal(t, 2, deletedTopUps)
	require.Equal(t, 1, deletedSubscriptionOrders)

	require.NoError(t, DB.Where("trade_no = ?", "topup-fresh-expired").First(&TopUp{}).Error)
	require.NoError(t, DB.Where("trade_no = ?", "topup-old-pending").First(&TopUp{}).Error)
	require.NoError(t, DB.Where("trade_no = ?", "sub-fresh-expired").First(&SubscriptionOrder{}).Error)

	var count int64
	require.NoError(t, DB.Model(&TopUp{}).Where("trade_no IN ?", []string{"topup-old-complete-expired", "topup-old-create-expired"}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("trade_no = ?", "sub-old-complete-expired").Count(&count).Error)
	require.Zero(t, count)
}
