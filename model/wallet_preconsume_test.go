package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestWalletPreConsumeIsIdempotentUntilSettledOrRefunded(t *testing.T) {
	truncateTables(t)
	user := createReserveTestUser(t, 500)

	require.NoError(t, ReserveWalletPreConsume("wallet-pre-same", user.Id, 100))
	require.NoError(t, ReserveWalletPreConsume("wallet-pre-same", user.Id, 100))
	require.EqualValues(t, 400, mustUserQuota(t, user.Id))

	err := ReserveWalletPreConsume("wallet-pre-same", user.Id, 80)
	require.ErrorIs(t, err, ErrWalletPreConsumeConflict)
	require.EqualValues(t, 400, mustUserQuota(t, user.Id))

	_, err = ApplyBillingAdjustment(BillingAdjustmentRequest{
		IdempotencyKey:   "billing:wallet-pre-same",
		RequestID:        "wallet-pre-same",
		UserID:           user.Id,
		Source:           BillingAdjustmentSourceWallet,
		FundingDelta:     0,
		ChargeQuota:      100,
		ReservationQuota: 100,
	})
	require.NoError(t, err)
	require.EqualValues(t, 400, mustUserQuota(t, user.Id))
	require.Equal(t, WalletPreConsumeSettled, mustWalletPreConsumeStatus(t, "wallet-pre-same"))

	err = ReserveWalletPreConsume("wallet-pre-same", user.Id, 100)
	require.ErrorIs(t, err, ErrWalletPreConsumeClosed)
	require.EqualValues(t, 400, mustUserQuota(t, user.Id))
}

func TestWalletPreConsumeRefundsExpiredReservationOnce(t *testing.T) {
	truncateTables(t)
	user := createReserveTestUser(t, 500)
	require.NoError(t, ReserveWalletPreConsume("wallet-pre-expire", user.Id, 100))
	require.NoError(t, IncreaseWalletPreConsume("wallet-pre-expire", user.Id, 20))
	require.EqualValues(t, 380, mustUserQuota(t, user.Id))
	require.NoError(t, DB.Model(&WalletPreConsumeRecord{}).Where("request_id = ?", "wallet-pre-expire").Update("lease_until", 1).Error)

	require.NoError(t, RecoverExpiredWalletPreConsumes(10))
	require.EqualValues(t, 500, mustUserQuota(t, user.Id))
	require.Equal(t, WalletPreConsumeRefunded, mustWalletPreConsumeStatus(t, "wallet-pre-expire"))

	require.NoError(t, RecoverExpiredWalletPreConsumes(10))
	require.EqualValues(t, 500, mustUserQuota(t, user.Id))
}

func TestExpiredDeliveredWalletPreConsumeStaysCharged(t *testing.T) {
	truncateTables(t)
	user := createReserveTestUser(t, 500)
	require.NoError(t, ReserveWalletPreConsume("wallet-pre-delivered", user.Id, 100))
	require.NoError(t, MarkWalletPreConsumeClientDelivered("wallet-pre-delivered"))
	require.NoError(t, DB.Model(&WalletPreConsumeRecord{}).Where("request_id = ?", "wallet-pre-delivered").Update("lease_until", 1).Error)

	require.NoError(t, RecoverExpiredWalletPreConsumes(10))
	require.EqualValues(t, 400, mustUserQuota(t, user.Id))
	require.Equal(t, WalletPreConsumeSettled, mustWalletPreConsumeStatus(t, "wallet-pre-delivered"))

	require.NoError(t, RecoverExpiredWalletPreConsumes(10))
	require.EqualValues(t, 400, mustUserQuota(t, user.Id))
}

func TestDeliveredWalletPreConsumeStillRefundsWhenRequestFails(t *testing.T) {
	truncateTables(t)
	user := createReserveTestUser(t, 500)
	require.NoError(t, ReserveWalletPreConsume("wallet-pre-delivered-fail", user.Id, 100))
	require.NoError(t, MarkWalletPreConsumeClientDelivered("wallet-pre-delivered-fail"))

	require.NoError(t, RefundWalletPreConsume("wallet-pre-delivered-fail"))
	require.EqualValues(t, 500, mustUserQuota(t, user.Id))
	require.Equal(t, WalletPreConsumeRefunded, mustWalletPreConsumeStatus(t, "wallet-pre-delivered-fail"))
}

func TestWalletPreConsumeKeepsActiveLease(t *testing.T) {
	truncateTables(t)
	user := createReserveTestUser(t, 500)
	require.NoError(t, ReserveWalletPreConsume("wallet-pre-live", user.Id, 100))

	require.NoError(t, RecoverExpiredWalletPreConsumes(10))
	require.EqualValues(t, 400, mustUserQuota(t, user.Id))
	require.Equal(t, WalletPreConsumeReserved, mustWalletPreConsumeStatus(t, "wallet-pre-live"))
}

func TestRefundedWalletPreConsumeThenSettleChargesFullAmount(t *testing.T) {
	truncateTables(t)
	user := createReserveTestUser(t, 500)
	require.NoError(t, ReserveWalletPreConsume("wallet-pre-full", user.Id, 100))
	require.NoError(t, DB.Model(&WalletPreConsumeRecord{}).Where("request_id = ?", "wallet-pre-full").Update("lease_until", 1).Error)
	require.NoError(t, RecoverExpiredWalletPreConsumes(10))
	require.EqualValues(t, 500, mustUserQuota(t, user.Id))

	applied, err := ApplyBillingAdjustment(BillingAdjustmentRequest{
		IdempotencyKey:   "billing:wallet-pre-full",
		RequestID:        "wallet-pre-full",
		UserID:           user.Id,
		Source:           BillingAdjustmentSourceWallet,
		FundingDelta:     30,
		ChargeQuota:      130,
		ReservationQuota: 100,
	})
	require.NoError(t, err)
	require.True(t, applied)
	require.EqualValues(t, 370, mustUserQuota(t, user.Id))
}

func TestTryReserveUserQuotaUsesDatabaseWhenCacheIsStaleLow(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 200)
	require.NoError(t, populateUserCache(user))
	require.NoError(t, common.RDB.HSet(t.Context(), getUserCacheKey(user.Id), "Quota", "10").Err())

	quota, err := WalletQuotaForPreConsume(user.Id, 100)
	require.NoError(t, err)
	require.EqualValues(t, 200, quota)

	reserved, err := TryReserveUserQuota(user.Id, 100)
	require.NoError(t, err)
	require.True(t, reserved)
	require.EqualValues(t, 100, mustUserQuota(t, user.Id))
	visible, err := GetUserQuota(user.Id, false)
	require.NoError(t, err)
	require.EqualValues(t, 100, visible)
}

func TestTryReserveUserQuotaStaysShortWhenDatabaseIsAlsoShort(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 30)
	require.NoError(t, populateUserCache(user))
	require.NoError(t, common.RDB.HSet(t.Context(), getUserCacheKey(user.Id), "Quota", "10").Err())

	reserved, err := TryReserveUserQuota(user.Id, 100)
	require.NoError(t, err)
	require.False(t, reserved)
	require.EqualValues(t, 30, mustUserQuota(t, user.Id))
}

func TestWalletPreConsumeReleaseReturnsOnlyTheExtraAmount(t *testing.T) {
	truncateTables(t)
	user := createReserveTestUser(t, 500)
	require.NoError(t, ReserveWalletPreConsume("wallet-pre-release", user.Id, 100))
	require.NoError(t, IncreaseWalletPreConsume("wallet-pre-release", user.Id, 20))
	require.NoError(t, ReleaseWalletPreConsume("wallet-pre-release", user.Id, 20))

	require.EqualValues(t, 400, mustUserQuota(t, user.Id))
	var record WalletPreConsumeRecord
	require.NoError(t, DB.Where("request_id = ?", "wallet-pre-release").First(&record).Error)
	require.Equal(t, WalletPreConsumeReserved, record.Status)
	require.EqualValues(t, 100, record.Amount)
}

func mustUserQuota(t *testing.T, id int) int64 {
	t.Helper()
	quota, err := GetUserQuota(id, true)
	require.NoError(t, err)
	return quota
}

func mustWalletPreConsumeStatus(t *testing.T, requestID string) string {
	t.Helper()
	var record WalletPreConsumeRecord
	require.NoError(t, DB.Where("request_id = ?", requestID).First(&record).Error)
	return record.Status
}

func TestTrustedWalletObligationDoesNotDebitUntilDeliveryExpires(t *testing.T) {
	truncateTables(t)
	user := createReserveTestUser(t, 150)

	require.NoError(t, ReserveTrustedWalletPreConsume("wallet-trust-open", user.Id, 200))
	require.EqualValues(t, 150, mustUserQuota(t, user.Id))
	require.Equal(t, WalletPreConsumeTrusted, mustWalletPreConsumeStatus(t, "wallet-trust-open"))

	require.NoError(t, DB.Model(&WalletPreConsumeRecord{}).Where("request_id = ?", "wallet-trust-open").Update("lease_until", 1).Error)
	_, err := refundWalletPreConsume("wallet-trust-open", true)
	require.NoError(t, err)
	require.EqualValues(t, 150, mustUserQuota(t, user.Id))
	require.Equal(t, WalletPreConsumeRefunded, mustWalletPreConsumeStatus(t, "wallet-trust-open"))

	require.NoError(t, ReserveTrustedWalletPreConsume("wallet-trust-fail", user.Id, 200))
	require.NoError(t, MarkWalletPreConsumeClientDelivered("wallet-trust-fail"))
	require.NoError(t, RefundWalletPreConsume("wallet-trust-fail"))
	require.EqualValues(t, 150, mustUserQuota(t, user.Id))
	require.Equal(t, WalletPreConsumeRefunded, mustWalletPreConsumeStatus(t, "wallet-trust-fail"))
}

func TestTrustedDeliveredWalletExpiryChargesEstimateOnce(t *testing.T) {
	truncateTables(t)
	user := createReserveTestUser(t, 150)

	require.NoError(t, ReserveTrustedWalletPreConsume("wallet-trust-delivered", user.Id, 200))
	require.NoError(t, MarkWalletPreConsumeClientDelivered("wallet-trust-delivered"))
	require.NoError(t, DB.Model(&WalletPreConsumeRecord{}).Where("request_id = ?", "wallet-trust-delivered").Update("lease_until", 1).Error)

	_, err := refundWalletPreConsume("wallet-trust-delivered", true)
	require.NoError(t, err)
	require.EqualValues(t, -50, mustUserQuota(t, user.Id))
	require.Equal(t, WalletPreConsumeSettled, mustWalletPreConsumeStatus(t, "wallet-trust-delivered"))

	_, err = refundWalletPreConsume("wallet-trust-delivered", true)
	require.ErrorIs(t, err, ErrWalletPreConsumeClosed)
	require.EqualValues(t, -50, mustUserQuota(t, user.Id))
}

func TestTrustedSettleAfterEstimateCollectionChargesOnlyTheDifference(t *testing.T) {
	truncateTables(t)
	user := createReserveTestUser(t, 150)

	require.NoError(t, ReserveTrustedWalletPreConsume("wallet-trust-delta", user.Id, 200))
	require.NoError(t, MarkWalletPreConsumeClientDelivered("wallet-trust-delta"))
	require.NoError(t, DB.Model(&WalletPreConsumeRecord{}).Where("request_id = ?", "wallet-trust-delta").Update("lease_until", 1).Error)
	_, err := refundWalletPreConsume("wallet-trust-delta", true)
	require.NoError(t, err)
	require.EqualValues(t, -50, mustUserQuota(t, user.Id))

	_, err = ApplyBillingAdjustment(BillingAdjustmentRequest{
		IdempotencyKey:   "billing:wallet-trust-delta",
		RequestID:        "wallet-trust-delta",
		UserID:           user.Id,
		Source:           BillingAdjustmentSourceWallet,
		FundingDelta:     30,
		ChargeQuota:      30,
		ReservationQuota: 0,
	})
	require.NoError(t, err)
	require.EqualValues(t, 120, mustUserQuota(t, user.Id))
}

func TestIncreaseWalletPreConsumeConvertsTrustedObligationIntoReserve(t *testing.T) {
	truncateTables(t)
	user := createReserveTestUser(t, 500)

	require.NoError(t, ReserveTrustedWalletPreConsume("wallet-trust-convert", user.Id, 200))
	require.NoError(t, IncreaseWalletPreConsume("wallet-trust-convert", user.Id, 400))
	require.EqualValues(t, 100, mustUserQuota(t, user.Id))
	require.Equal(t, WalletPreConsumeReserved, mustWalletPreConsumeStatus(t, "wallet-trust-convert"))

	var record WalletPreConsumeRecord
	require.NoError(t, DB.Where("request_id = ?", "wallet-trust-convert").First(&record).Error)
	require.EqualValues(t, 400, record.Amount)
}

func TestExpiredWalletPreConsumeWaitsForBillingAdjustment(t *testing.T) {
	truncateTables(t)
	user := createReserveTestUser(t, 500)
	require.NoError(t, ReserveWalletPreConsume("wallet-ledger-expire", user.Id, 100))
	require.NoError(t, DB.Model(&WalletPreConsumeRecord{}).Where("request_id = ?", "wallet-ledger-expire").Update("lease_until", 1).Error)
	require.NoError(t, EnsurePendingBillingAdjustment(BillingAdjustmentRequest{
		IdempotencyKey:   "billing:wallet-ledger-expire",
		RequestID:        "wallet-ledger-expire",
		UserID:           user.Id,
		Source:           BillingAdjustmentSourceWallet,
		FundingDelta:     -20,
		ChargeQuota:      80,
		ReservationQuota: 100,
	}))

	require.NoError(t, RecoverExpiredWalletPreConsumes(10))
	require.EqualValues(t, 400, mustUserQuota(t, user.Id))
	require.Equal(t, WalletPreConsumeReserved, mustWalletPreConsumeStatus(t, "wallet-ledger-expire"))

	require.NoError(t, RollbackBillingAdjustment("billing:wallet-ledger-expire", 0))
	require.EqualValues(t, 500, mustUserQuota(t, user.Id))
	require.Equal(t, WalletPreConsumeRefunded, mustWalletPreConsumeStatus(t, "wallet-ledger-expire"))
	require.NoError(t, RollbackBillingAdjustment("billing:wallet-ledger-expire", 0))
	require.EqualValues(t, 500, mustUserQuota(t, user.Id))
}

func TestRollbackAfterExpiredWalletRefundDoesNotCreditTwice(t *testing.T) {
	truncateTables(t)
	user := createReserveTestUser(t, 500)
	require.NoError(t, ReserveWalletPreConsume("wallet-ledger-raced", user.Id, 100))
	require.NoError(t, DB.Model(&WalletPreConsumeRecord{}).Where("request_id = ?", "wallet-ledger-raced").Update("lease_until", 1).Error)
	require.NoError(t, RecoverExpiredWalletPreConsumes(10))
	require.EqualValues(t, 500, mustUserQuota(t, user.Id))
	require.NoError(t, EnsurePendingBillingAdjustment(BillingAdjustmentRequest{
		IdempotencyKey:   "billing:wallet-ledger-raced",
		RequestID:        "wallet-ledger-raced",
		UserID:           user.Id,
		Source:           BillingAdjustmentSourceWallet,
		FundingDelta:     0,
		ChargeQuota:      100,
		ReservationQuota: 100,
	}))

	require.NoError(t, RollbackBillingAdjustment("billing:wallet-ledger-raced", 0))
	require.EqualValues(t, 500, mustUserQuota(t, user.Id))
	require.Equal(t, WalletPreConsumeRefunded, mustWalletPreConsumeStatus(t, "wallet-ledger-raced"))
}
