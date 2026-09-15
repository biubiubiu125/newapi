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
