package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestTransferAffQuotaToQuotaStaysWithinWalletCeiling(t *testing.T) {
	truncateTables(t)
	amount := int64(common.QuotaPerUnit)
	user := &User{
		Username: "aff-transfer-ok",
		Password: "password",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    1000,
		AffQuota: amount * 2,
		AffCode:  "aff-transfer-ok",
	}
	require.NoError(t, DB.Create(user).Error)

	require.NoError(t, user.TransferAffQuotaToQuota(amount))

	var got User
	require.NoError(t, DB.Select("quota", "aff_quota").First(&got, user.Id).Error)
	require.Equal(t, int64(1000)+amount, got.Quota)
	require.Equal(t, amount, got.AffQuota)
}

func TestTransferAffQuotaToQuotaRejectsWalletCeiling(t *testing.T) {
	truncateTables(t)
	amount := int64(common.QuotaPerUnit)
	startQuota := common.MaxWalletQuota - amount + 1
	startAff := amount * 2
	user := &User{
		Username: "aff-transfer-cap",
		Password: "password",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    startQuota,
		AffQuota: startAff,
		AffCode:  "aff-transfer-cap",
	}
	require.NoError(t, DB.Create(user).Error)

	err := user.TransferAffQuotaToQuota(amount)
	require.ErrorIs(t, err, ErrWalletQuotaLimitExceeded)

	var got User
	require.NoError(t, DB.Select("quota", "aff_quota").First(&got, user.Id).Error)
	require.Equal(t, startQuota, got.Quota)
	require.Equal(t, startAff, got.AffQuota)
}

func TestTransferAffQuotaToQuotaAllowsExactWalletCeiling(t *testing.T) {
	truncateTables(t)
	amount := int64(common.QuotaPerUnit)
	user := &User{
		Username: "aff-transfer-exact",
		Password: "password",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    common.MaxWalletQuota - amount,
		AffQuota: amount,
		AffCode:  "aff-transfer-exact",
	}
	require.NoError(t, DB.Create(user).Error)

	require.NoError(t, user.TransferAffQuotaToQuota(amount))

	var got User
	require.NoError(t, DB.Select("quota", "aff_quota").First(&got, user.Id).Error)
	require.Equal(t, common.MaxWalletQuota, got.Quota)
	require.Equal(t, int64(0), got.AffQuota)
}
