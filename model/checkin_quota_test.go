package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestCheckinRejectsAwardAboveWalletQuota(t *testing.T) {
	setupUserUpdateTestState(t)
	require.NoError(t, DB.AutoMigrate(&Checkin{}))

	overCap := User{
		Username:    "checkin-over-cap",
		Password:    "unused",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		Quota:       common.MaxWalletQuota - 10,
		AuthVersion: 1,
	}
	require.NoError(t, DB.Create(&overCap).Error)
	overCheckin := &Checkin{
		UserId:       overCap.Id,
		CheckinDate:  "2099-01-01",
		QuotaAwarded: 40,
		CreatedAt:    1,
	}
	_, err := userCheckinWithTransaction(overCheckin, overCap.Id, 40)
	require.Error(t, err)
	var overCapGot User
	require.NoError(t, DB.Select("quota").First(&overCapGot, overCap.Id).Error)
	require.Equal(t, common.MaxWalletQuota-10, overCapGot.Quota)
	var overCapRows int64
	require.NoError(t, DB.Model(&Checkin{}).Where("user_id = ?", overCap.Id).Count(&overCapRows).Error)
	require.Zero(t, overCapRows)

	sqliteOver := User{
		Username:    "checkin-sqlite-over",
		Password:    "unused",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		Quota:       common.MaxWalletQuota - 10,
		AuthVersion: 1,
	}
	require.NoError(t, DB.Create(&sqliteOver).Error)
	sqliteCheckin := &Checkin{
		UserId:       sqliteOver.Id,
		CheckinDate:  "2099-01-02",
		QuotaAwarded: 40,
		CreatedAt:    1,
	}
	_, err = userCheckinWithoutTransaction(sqliteCheckin, sqliteOver.Id, 40)
	require.Error(t, err)
	var sqliteOverGot User
	require.NoError(t, DB.Select("quota").First(&sqliteOverGot, sqliteOver.Id).Error)
	require.Equal(t, common.MaxWalletQuota-10, sqliteOverGot.Quota)
	var sqliteOverRows int64
	require.NoError(t, DB.Model(&Checkin{}).Where("user_id = ?", sqliteOver.Id).Count(&sqliteOverRows).Error)
	require.Zero(t, sqliteOverRows)

	withinCap := User{
		Username:    "checkin-within-cap",
		Password:    "unused",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		Quota:       1000,
		AuthVersion: 1,
	}
	require.NoError(t, DB.Create(&withinCap).Error)
	withinCheckin := &Checkin{
		UserId:       withinCap.Id,
		CheckinDate:  "2099-01-03",
		QuotaAwarded: 40,
		CreatedAt:    1,
	}
	created, err := userCheckinWithTransaction(withinCheckin, withinCap.Id, 40)
	require.NoError(t, err)
	require.Equal(t, 40, created.QuotaAwarded)
	var withinGot User
	require.NoError(t, DB.Select("quota").First(&withinGot, withinCap.Id).Error)
	require.Equal(t, int64(1040), withinGot.Quota)
}

func TestCheckinDateUsesShanghaiBoundary(t *testing.T) {
	utcEvening := time.Date(2026, 1, 1, 16, 30, 0, 0, time.UTC)
	require.Equal(t, "2026-01-02", CheckinDateString(utcEvening))
	require.Equal(t, "2026-01", CheckinMonthString(utcEvening))

	shanghaiMorning := time.Date(2026, 1, 2, 0, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	require.Equal(t, "2026-01-02", CheckinDateString(shanghaiMorning))
}
