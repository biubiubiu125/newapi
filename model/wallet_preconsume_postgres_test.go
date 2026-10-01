package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestPostgreSQLExpiredWalletPreConsumeDoesNotDoubleCredit(t *testing.T) {
	pg := openPostgreSQLChannelTest(t)
	require.NoError(t, pg.AutoMigrate(&User{}, &WalletPreConsumeRecord{}, &BillingAdjustment{}))

	user := User{
		Username:    "pg-wallet-" + common.GetRandomString(8),
		Password:    "unused-password-hash",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AuthVersion: 1,
		Quota:       500,
		AffCode:     "pg-wallet-" + common.GetRandomString(8),
	}
	require.NoError(t, pg.Create(&user).Error)
	t.Cleanup(func() {
		_ = pg.Where("user_id = ?", user.Id).Delete(&WalletPreConsumeRecord{}).Error
		_ = pg.Where("user_id = ?", user.Id).Delete(&BillingAdjustment{}).Error
		_ = pg.Unscoped().Delete(&User{}, user.Id).Error
	})

	owned := "pg-wallet-owned-" + common.GetRandomString(8)
	require.NoError(t, ReserveWalletPreConsume(owned, user.Id, 100))
	require.NoError(t, pg.Model(&WalletPreConsumeRecord{}).Where("request_id = ?", owned).Update("lease_until", 1).Error)
	require.NoError(t, EnsurePendingBillingAdjustment(BillingAdjustmentRequest{
		IdempotencyKey:   "billing:" + owned,
		RequestID:        owned,
		UserID:           user.Id,
		Source:           BillingAdjustmentSourceWallet,
		FundingDelta:     -20,
		ChargeQuota:      80,
		ReservationQuota: 100,
	}))
	_, err := refundWalletPreConsume(owned, true)
	require.NoError(t, err)
	require.EqualValues(t, 400, mustUserQuota(t, user.Id))
	require.NoError(t, RollbackBillingAdjustment("billing:"+owned, 0))
	require.EqualValues(t, 500, mustUserQuota(t, user.Id))
	require.NoError(t, RollbackBillingAdjustment("billing:"+owned, 0))
	require.EqualValues(t, 500, mustUserQuota(t, user.Id))

	raced := "pg-wallet-raced-" + common.GetRandomString(8)
	require.NoError(t, ReserveWalletPreConsume(raced, user.Id, 100))
	require.NoError(t, pg.Model(&WalletPreConsumeRecord{}).Where("request_id = ?", raced).Update("lease_until", 1).Error)
	_, err = refundWalletPreConsume(raced, true)
	require.NoError(t, err)
	require.EqualValues(t, 500, mustUserQuota(t, user.Id))
	require.NoError(t, EnsurePendingBillingAdjustment(BillingAdjustmentRequest{
		IdempotencyKey:   "billing:" + raced,
		RequestID:        raced,
		UserID:           user.Id,
		Source:           BillingAdjustmentSourceWallet,
		ChargeQuota:      100,
		ReservationQuota: 100,
	}))
	require.NoError(t, RollbackBillingAdjustment("billing:"+raced, 0))
	require.EqualValues(t, 500, mustUserQuota(t, user.Id))
}
