package model

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestExpiredUndeliveredSubscriptionPreConsumeRefunds(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPreConsumeRecord{}, &UserSubscription{}, &BillingAdjustment{}))
	user, sub := seedLeaseSubscription(t, "sub-lease-open", 100)
	_, err := PreConsumeUserSubscription("sub-lease-open", user.Id, "gpt-4o", 0, 40, "default")
	require.NoError(t, err)
	require.NoError(t, ArmSubscriptionPreConsumeLease("sub-lease-open"))
	require.NoError(t, DB.Model(&SubscriptionPreConsumeRecord{}).Where("request_id = ?", "sub-lease-open").Update("lease_until", 1).Error)

	require.NoError(t, RecoverExpiredSubscriptionPreConsumes(10))
	require.EqualValues(t, 0, mustSubscriptionUsed(t, sub.Id))
	var record SubscriptionPreConsumeRecord
	require.NoError(t, DB.Where("request_id = ?", "sub-lease-open").First(&record).Error)
	require.Equal(t, "refunded", record.Status)
}

func TestExpiredDeliveredSubscriptionPreConsumeKeepsCharge(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPreConsumeRecord{}, &UserSubscription{}, &BillingAdjustment{}))
	user, sub := seedLeaseSubscription(t, "sub-lease-kept", 100)
	_, err := PreConsumeUserSubscription("sub-lease-kept", user.Id, "gpt-4o", 0, 40, "default")
	require.NoError(t, err)
	require.NoError(t, ArmSubscriptionPreConsumeLease("sub-lease-kept"))
	require.NoError(t, MarkSubscriptionPreConsumeClientDelivered("sub-lease-kept"))
	require.NoError(t, DB.Model(&SubscriptionPreConsumeRecord{}).Where("request_id = ?", "sub-lease-kept").Update("lease_until", 1).Error)

	require.NoError(t, RecoverExpiredSubscriptionPreConsumes(10))
	require.EqualValues(t, 40, mustSubscriptionUsed(t, sub.Id))
	var record SubscriptionPreConsumeRecord
	require.NoError(t, DB.Where("request_id = ?", "sub-lease-kept").First(&record).Error)
	require.Equal(t, "consumed", record.Status)
}

func TestLiveSubscriptionLeaseIsNotRefunded(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPreConsumeRecord{}, &UserSubscription{}, &BillingAdjustment{}))
	user, sub := seedLeaseSubscription(t, "sub-lease-live", 100)
	_, err := PreConsumeUserSubscription("sub-lease-live", user.Id, "gpt-4o", 0, 40, "default")
	require.NoError(t, err)
	require.NoError(t, ArmSubscriptionPreConsumeLease("sub-lease-live"))

	require.NoError(t, RecoverExpiredSubscriptionPreConsumes(10))
	require.EqualValues(t, 40, mustSubscriptionUsed(t, sub.Id))
}

func TestExpiredSubscriptionPreConsumeWaitsForBillingAdjustment(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPreConsumeRecord{}, &UserSubscription{}, &BillingAdjustment{}))
	user, sub := seedLeaseSubscription(t, "sub-lease-ledger", 100)
	_, err := PreConsumeUserSubscription("sub-lease-ledger", user.Id, "gpt-4o", 0, 40, "default")
	require.NoError(t, err)
	require.NoError(t, ArmSubscriptionPreConsumeLease("sub-lease-ledger"))
	require.NoError(t, DB.Model(&SubscriptionPreConsumeRecord{}).Where("request_id = ?", "sub-lease-ledger").Update("lease_until", 1).Error)
	require.NoError(t, EnsurePendingBillingAdjustment(BillingAdjustmentRequest{
		IdempotencyKey:   "billing:sub-lease-ledger",
		RequestID:        "sub-lease-ledger",
		UserID:           user.Id,
		Source:           BillingAdjustmentSourceSubscription,
		SubscriptionID:   sub.Id,
		FundingDelta:     10,
		ChargeQuota:      50,
		ReservationQuota: 40,
	}))

	require.NoError(t, RecoverExpiredSubscriptionPreConsumes(10))
	require.EqualValues(t, 40, mustSubscriptionUsed(t, sub.Id))
}

func seedLeaseSubscription(t *testing.T, name string, total int64) (User, UserSubscription) {
	t.Helper()
	now := time.Now().Unix()
	user := User{
		Username: name,
		Password: "password123",
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, DB.Create(&user).Error)
	plan := SubscriptionPlan{
		Title:            name,
		Enabled:          true,
		TotalAmount:      total,
		QuotaResetPeriod: SubscriptionResetNever,
	}
	require.NoError(t, DB.Create(&plan).Error)
	sub := UserSubscription{
		UserId:      user.Id,
		PlanId:      plan.Id,
		AmountTotal: total,
		AmountUsed:  0,
		StartTime:   now - 10,
		EndTime:     now + 3600,
		Status:      "active",
	}
	require.NoError(t, DB.Create(&sub).Error)
	return user, sub
}

func mustSubscriptionUsed(t *testing.T, id int) int64 {
	t.Helper()
	var sub UserSubscription
	require.NoError(t, DB.Select("amount_used").First(&sub, id).Error)
	return sub.AmountUsed
}

func TestPreConsumeUserSubscriptionReturnsNoActiveSentinel(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPreConsumeRecord{}, &UserSubscription{}))
	user := User{
		Username: "sub-none",
		Password: "password123",
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, DB.Create(&user).Error)

	_, err := PreConsumeUserSubscription("req-no-sub", user.Id, "gpt-4o", 0, 10, "default")
	require.ErrorIs(t, err, ErrNoActiveSubscription)
}

func TestPreConsumeUserSubscriptionReturnsQuotaInsufficientSentinel(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPreConsumeRecord{}, &UserSubscription{}))
	now := time.Now().Unix()
	user := User{
		Username: "sub-empty",
		Password: "password123",
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, DB.Create(&user).Error)
	plan := SubscriptionPlan{
		Title:            "empty-plan",
		Enabled:          true,
		TotalAmount:      10,
		QuotaResetPeriod: SubscriptionResetNever,
	}
	require.NoError(t, DB.Create(&plan).Error)
	require.NoError(t, DB.Create(&UserSubscription{
		UserId:      user.Id,
		PlanId:      plan.Id,
		AmountTotal: 10,
		AmountUsed:  10,
		StartTime:   now - 10,
		EndTime:     now + 3600,
		Status:      "active",
	}).Error)

	_, err := PreConsumeUserSubscription("req-empty-sub", user.Id, "gpt-4o", 0, 5, "default")
	require.ErrorIs(t, err, ErrSubscriptionQuotaInsufficient)
}

func TestPreConsumeUserSubscriptionReturnsGroupGrantSentinel(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPreConsumeRecord{}, &UserSubscription{}))
	now := time.Now().Unix()
	user := User{
		Username: "sub-group",
		Password: "password123",
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&UserSubscription{
		UserId:      user.Id,
		AmountTotal: 100,
		AmountUsed:  0,
		StartTime:   now - 10,
		EndTime:     now + 3600,
		Status:      "active",
		GrantGroups: "vip",
	}).Error)

	_, err := PreConsumeUserSubscription("req-group-sub", user.Id, "gpt-4o", 0, 5, "default")
	require.ErrorIs(t, err, ErrNoActiveSubscriptionGrantsGroup)
}

func TestPreConsumeUserSubscriptionTxDoesNotMaskQueryError(t *testing.T) {
	previousUsingSQLite := common.UsingSQLite
	previousUsingPostgreSQL := common.UsingPostgreSQL
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	common.UsingSQLite = true
	common.UsingPostgreSQL = false
	t.Cleanup(func() {
		common.UsingSQLite = previousUsingSQLite
		common.UsingPostgreSQL = previousUsingPostgreSQL
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})
	require.NoError(t, db.AutoMigrate(&UserSubscription{}, &SubscriptionPreConsumeRecord{}))
	require.NoError(t, db.Migrator().DropTable(&UserSubscription{}))

	_, err = PreConsumeUserSubscriptionTx(db, "req-db-error", 1, "gpt-4o", 0, 10, "default")
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrNoActiveSubscription))
	require.NotContains(t, strings.ToLower(err.Error()), "no active subscription")
}

func TestCleanupSubscriptionPreConsumeRecordsKeepsConsumedRows(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPreConsumeRecord{}))
	t.Cleanup(func() {
		DB.Where("request_id IN ?", []string{"keep-consumed", "drop-refunded"}).Delete(&SubscriptionPreConsumeRecord{})
	})
	old := time.Now().Unix() - 10*24*3600
	require.NoError(t, DB.Create(&SubscriptionPreConsumeRecord{
		RequestId:          "keep-consumed",
		UserId:             1,
		UserSubscriptionId: 1,
		PreConsumed:        10,
		Status:             "consumed",
	}).Error)
	require.NoError(t, DB.Create(&SubscriptionPreConsumeRecord{
		RequestId:          "drop-refunded",
		UserId:             1,
		UserSubscriptionId: 1,
		PreConsumed:        10,
		Status:             "refunded",
	}).Error)
	require.NoError(t, DB.Model(&SubscriptionPreConsumeRecord{}).Where("request_id IN ?", []string{"keep-consumed", "drop-refunded"}).UpdateColumns(map[string]any{
		"created_at": old,
		"updated_at": old,
	}).Error)

	n, err := CleanupSubscriptionPreConsumeRecords(7 * 24 * 3600)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	var kept SubscriptionPreConsumeRecord
	require.NoError(t, DB.Where("request_id = ?", "keep-consumed").First(&kept).Error)
	require.Equal(t, "consumed", kept.Status)
	err = DB.Where("request_id = ?", "drop-refunded").First(&SubscriptionPreConsumeRecord{}).Error
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}
