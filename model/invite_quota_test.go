package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func useInviteReward(t *testing.T, invitee int64) {
	t.Helper()
	previousInvitee := common.QuotaForInvitee
	previousInviter := common.QuotaForInviter
	previousPayment := *operation_setting.GetPaymentSetting()
	common.QuotaForInvitee = invitee
	common.QuotaForInviter = 0
	*operation_setting.GetPaymentSetting() = operation_setting.PaymentSetting{
		ComplianceConfirmed:    true,
		ComplianceTermsVersion: operation_setting.CurrentComplianceTermsVersion,
	}
	t.Cleanup(func() {
		common.QuotaForInvitee = previousInvitee
		common.QuotaForInviter = previousInviter
		*operation_setting.GetPaymentSetting() = previousPayment
	})
}

func TestFinishInsertInviteeBonusAddsWithinWalletQuota(t *testing.T) {
	truncateTables(t)
	useInviteReward(t, 40)
	user := &User{
		Username: "invite-within-cap",
		Password: "password",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    1000,
		AffCode:  "inv-within",
	}
	require.NoError(t, DB.Create(user).Error)
	inviter := &User{
		Username: "invite-within-inviter",
		Password: "password",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    0,
		AffCode:  "inv-within-from",
	}
	require.NoError(t, DB.Create(inviter).Error)

	user.finishInsert(inviter.Id)

	var got User
	require.NoError(t, DB.Select("quota").First(&got, user.Id).Error)
	require.Equal(t, int64(1040), got.Quota)
}

func TestFinishInsertInviteeBonusDoesNotExceedWalletQuota(t *testing.T) {
	truncateTables(t)
	useInviteReward(t, 40)
	user := &User{
		Username: "invite-over-cap",
		Password: "password",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    common.MaxWalletQuota - 10,
		AffCode:  "inv-over",
	}
	require.NoError(t, DB.Create(user).Error)
	inviter := &User{
		Username: "invite-over-inviter",
		Password: "password",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    0,
		AffCode:  "inv-over-from",
	}
	require.NoError(t, DB.Create(inviter).Error)

	user.finishInsert(inviter.Id)

	var got User
	require.NoError(t, DB.Select("quota").First(&got, user.Id).Error)
	require.Equal(t, common.MaxWalletQuota-10, got.Quota)
}

func TestFinalizeOAuthUserCreationInviteeBonusDoesNotExceedWalletQuota(t *testing.T) {
	truncateTables(t)
	useInviteReward(t, 40)
	user := &User{
		Username: "oauth-invite-over-cap",
		Password: "password",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    common.MaxWalletQuota - 10,
		AffCode:  "oauth-inv-over",
	}
	require.NoError(t, DB.Create(user).Error)
	inviter := &User{
		Username: "oauth-invite-over-inviter",
		Password: "password",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    0,
		AffCode:  "oauth-inv-from",
	}
	require.NoError(t, DB.Create(inviter).Error)

	user.FinalizeOAuthUserCreation(inviter.Id)

	var got User
	require.NoError(t, DB.Select("quota").First(&got, user.Id).Error)
	require.Equal(t, common.MaxWalletQuota-10, got.Quota)
}
