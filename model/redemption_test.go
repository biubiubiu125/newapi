package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestRedeemCreatesPendingReferralCommissionJob(t *testing.T) {
	truncateTables(t)

	previousQuotaPerUnit := common.QuotaPerUnit
	previousReferralEnabled := common.ReferralEnabled
	previousReferralDefaultRate := common.ReferralDefaultRate
	previousRedemptionRate := common.ReferralRedemptionUSDToCNYRate
	common.QuotaPerUnit = 100
	common.ReferralEnabled = true
	common.ReferralDefaultRate = 10
	common.ReferralRedemptionUSDToCNYRate = 2
	t.Cleanup(func() {
		common.QuotaPerUnit = previousQuotaPerUnit
		common.ReferralEnabled = previousReferralEnabled
		common.ReferralDefaultRate = previousReferralDefaultRate
		common.ReferralRedemptionUSDToCNYRate = previousRedemptionRate
	})

	user := &User{Username: "redeem-referral-user", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(user).Error)
	inviter := &User{Username: "redeem-referral-inviter", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(inviter).Error)
	affiliate := &ReferralAffiliate{
		UserId:             inviter.Id,
		InviteCode:         "RDMTEST1",
		Status:             ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, DB.Create(affiliate).Error)
	require.NoError(t, DB.Create(&ReferralBinding{
		InviteeUserId: user.Id,
		InviterUserId: inviter.Id,
		AffiliateId:   affiliate.Id,
		BindSource:    "code",
		BindCode:      affiliate.InviteCode,
		BoundAt:       common.GetTimestamp(),
	}).Error)
	redemption := &Redemption{
		Key:         "redeem-referral-pending-job-key",
		Name:        "pending referral job",
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       300,
		CreatedTime: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(redemption).Error)

	result, err := Redeem(redemption.Key, user.Id)
	require.NoError(t, err)
	require.Equal(t, redemption.Id, result.RedemptionId)
	require.Equal(t, 300, result.Quota)

	var reloaded Redemption
	require.NoError(t, DB.First(&reloaded, redemption.Id).Error)
	require.Equal(t, common.RedemptionCodeStatusUsed, reloaded.Status)
	require.Equal(t, user.Id, reloaded.UsedUserId)
	require.Equal(t, 100.0, reloaded.QuotaPerUnitSnapshot)
	require.Equal(t, ReferralCommissionJobStatusPending, reloaded.ReferralCommissionStatus)
	require.Equal(t, affiliate.Id, reloaded.ReferralAffiliateId)
	require.Equal(t, 10.0, reloaded.ReferralRate)
	require.Equal(t, 6.0, reloaded.ReferralBaseAmount)
	require.Equal(t, "CNY", reloaded.ReferralBaseCurrency)

	var job ReferralCommissionJob
	require.NoError(t, DB.Where("source_type = ? AND source_trade_no = ?", "redemption", redemptionCommissionTradeNo(redemption.Id)).First(&job).Error)
	require.Equal(t, ReferralCommissionJobStatusPending, job.Status)
	require.Equal(t, affiliate.Id, job.AffiliateId)

	rows, err := DeleteInvalidRedemptions()
	require.NoError(t, err)
	require.Zero(t, rows)
	require.NoError(t, DB.First(&Redemption{}, redemption.Id).Error)
	require.ErrorContains(t, DeleteRedemptionById(redemption.Id), "unresolved")
}

func TestRedeemRejectsEnabledButAlreadyUsedCode(t *testing.T) {
	truncateTables(t)

	firstUser := &User{Username: "redeem-used-original-user", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(firstUser).Error)
	secondUser := &User{Username: "redeem-used-second-user", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(secondUser).Error)
	redemption := &Redemption{
		Key:          "redeem-enabled-but-used-key",
		Name:         "enabled but already used",
		Status:       common.RedemptionCodeStatusEnabled,
		Quota:        100,
		UsedUserId:   firstUser.Id,
		RedeemedTime: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(redemption).Error)

	result, err := Redeem(redemption.Key, secondUser.Id)
	require.Nil(t, result)
	require.ErrorIs(t, err, ErrRedeemFailed)

	var reloaded Redemption
	require.NoError(t, DB.First(&reloaded, redemption.Id).Error)
	require.Equal(t, firstUser.Id, reloaded.UsedUserId)
	require.Equal(t, common.RedemptionCodeStatusEnabled, reloaded.Status)
}

func TestRedeemRejectsNonPositiveQuotaCode(t *testing.T) {
	truncateTables(t)

	user := &User{Username: "redeem-invalid-quota-user", Status: common.UserStatusEnabled, Quota: 100}
	require.NoError(t, DB.Create(user).Error)

	for _, item := range []struct {
		redemption *Redemption
		quota      int
	}{
		{
			redemption: &Redemption{
				Key:         "redeem-zero-quota-key",
				Name:        "zero quota",
				Status:      common.RedemptionCodeStatusEnabled,
				Quota:       100,
				CreatedTime: common.GetTimestamp(),
			},
			quota: 0,
		},
		{
			redemption: &Redemption{
				Key:         "redeem-negative-quota-key",
				Name:        "negative quota",
				Status:      common.RedemptionCodeStatusEnabled,
				Quota:       100,
				CreatedTime: common.GetTimestamp(),
			},
			quota: -100,
		},
	} {
		redemption := item.redemption
		require.NoError(t, DB.Create(redemption).Error)
		require.NoError(t, DB.Model(redemption).Update("quota", item.quota).Error)
		result, err := Redeem(redemption.Key, user.Id)
		require.Nil(t, result)
		require.ErrorIs(t, err, ErrRedeemFailed)

		var reloaded Redemption
		require.NoError(t, DB.First(&reloaded, redemption.Id).Error)
		require.Equal(t, common.RedemptionCodeStatusEnabled, reloaded.Status)
		require.Zero(t, reloaded.UsedUserId)
		require.Zero(t, reloaded.RedeemedTime)
	}

	var reloadedUser User
	require.NoError(t, DB.First(&reloadedUser, user.Id).Error)
	require.Equal(t, 100, reloadedUser.Quota)

	var jobs int64
	require.NoError(t, DB.Model(&ReferralCommissionJob{}).Where("source_type = ?", "redemption").Count(&jobs).Error)
	require.Zero(t, jobs)
}

func TestRedeemSkippedReferralWritesTerminalTime(t *testing.T) {
	truncateTables(t)

	previousQuotaPerUnit := common.QuotaPerUnit
	previousReferralEnabled := common.ReferralEnabled
	previousRedemptionRate := common.ReferralRedemptionUSDToCNYRate
	common.QuotaPerUnit = 100
	common.ReferralEnabled = true
	common.ReferralRedemptionUSDToCNYRate = 1
	t.Cleanup(func() {
		common.QuotaPerUnit = previousQuotaPerUnit
		common.ReferralEnabled = previousReferralEnabled
		common.ReferralRedemptionUSDToCNYRate = previousRedemptionRate
	})

	user := &User{Username: "redeem-skipped-terminal-user", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(user).Error)
	redemption := &Redemption{
		Key:         "redeem-skipped-terminal-key",
		Name:        "skipped terminal time",
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       100,
		CreatedTime: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(redemption).Error)

	result, err := Redeem(redemption.Key, user.Id)
	require.NoError(t, err)
	require.Equal(t, redemption.Id, result.RedemptionId)

	var reloaded Redemption
	require.NoError(t, DB.First(&reloaded, redemption.Id).Error)
	require.Equal(t, ReferralCommissionJobStatusSkipped, reloaded.ReferralCommissionStatus)
	require.Equal(t, "no_binding", reloaded.ReferralCommissionError)
	require.NotZero(t, reloaded.ReferralCommissionAt)

	var job ReferralCommissionJob
	require.NoError(t, DB.Where("source_type = ? AND source_trade_no = ?", "redemption", redemptionCommissionTradeNo(redemption.Id)).First(&job).Error)
	require.Equal(t, ReferralCommissionJobStatusSkipped, job.Status)
	require.Equal(t, "no_binding", job.LastError)
	require.NotZero(t, job.SucceededAt)
}

func TestDeleteInvalidRedemptionsKeepsUnprocessedUsedCode(t *testing.T) {
	truncateTables(t)

	redemption := &Redemption{
		Key:          "redeem-unprocessed-empty-status",
		Name:         "unprocessed used redemption",
		Status:       common.RedemptionCodeStatusUsed,
		Quota:        100,
		UsedUserId:   123,
		RedeemedTime: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(redemption).Error)

	rows, err := DeleteInvalidRedemptions()
	require.NoError(t, err)
	require.Zero(t, rows)
	require.NoError(t, DB.First(&Redemption{}, redemption.Id).Error)
	require.ErrorContains(t, DeleteRedemptionById(redemption.Id), "unresolved")
}

func TestDeleteInvalidRedemptionsKeepsDisabledUsedUnresolvedCode(t *testing.T) {
	truncateTables(t)

	protected := &Redemption{
		Key:                      "redeem-disabled-unresolved-key",
		Name:                     "disabled unresolved redemption",
		Status:                   common.RedemptionCodeStatusDisabled,
		Quota:                    100,
		UsedUserId:               123,
		RedeemedTime:             common.GetTimestamp(),
		ReferralCommissionStatus: ReferralCommissionJobStatusFailed,
		ReferralCommissionError:  "temporary error",
	}
	require.NoError(t, DB.Create(protected).Error)
	deletable := &Redemption{
		Key:    "redeem-disabled-unused-key",
		Name:   "disabled unused redemption",
		Status: common.RedemptionCodeStatusDisabled,
		Quota:  100,
	}
	require.NoError(t, DB.Create(deletable).Error)

	rows, err := DeleteInvalidRedemptions()
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
	require.NoError(t, DB.First(&Redemption{}, protected.Id).Error)
	require.Error(t, DB.First(&Redemption{}, deletable.Id).Error)
	require.ErrorContains(t, DeleteRedemptionById(protected.Id), "unresolved")
}

func TestDeleteInvalidRedemptionsAllowsSucceededUsedCode(t *testing.T) {
	truncateTables(t)

	invitee := &User{Username: "redeem-succeeded-cleanup-invitee", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(invitee).Error)
	inviter := &User{Username: "redeem-succeeded-cleanup-inviter", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(inviter).Error)
	affiliate := &ReferralAffiliate{
		UserId:             inviter.Id,
		InviteCode:         "RDMOK001",
		Status:             ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, DB.Create(affiliate).Error)
	redemption := &Redemption{
		Key:                      "redeem-succeeded-cleanup",
		Name:                     "succeeded redemption",
		Status:                   common.RedemptionCodeStatusUsed,
		Quota:                    100,
		UsedUserId:               invitee.Id,
		RedeemedTime:             common.GetTimestamp(),
		ReferralAffiliateId:      affiliate.Id,
		ReferralCommissionStatus: ReferralCommissionJobStatusSucceeded,
	}
	require.NoError(t, DB.Create(redemption).Error)
	tradeNo := redemptionCommissionTradeNo(redemption.Id)
	require.NoError(t, DB.Create(&ReferralCommissionAccount{
		AffiliateId:   affiliate.Id,
		UserId:        inviter.Id,
		PendingAmount: 0.1,
	}).Error)
	commission := &ReferralCommission{
		AffiliateId:          affiliate.Id,
		AffiliateUserId:      inviter.Id,
		InviteeUserId:        invitee.Id,
		SourceType:           "redemption",
		SourceOrderId:        redemption.Id,
		SourceTradeNo:        tradeNo,
		OrderType:            "redemption",
		BaseAmount:           1,
		PaidAmount:           1,
		PaidCurrency:         "CNY",
		SettlementCurrency:   "CNY",
		SettlementFxRate:     1,
		SettlementBaseAmount: 1,
		Rate:                 10,
		CommissionAmount:     0.1,
		Status:               ReferralCommissionStatusPending,
	}
	require.NoError(t, DB.Create(commission).Error)
	require.NoError(t, DB.Create(&ReferralCommissionLedger{
		AffiliateId:        affiliate.Id,
		UserId:             inviter.Id,
		CommissionId:       commission.Id,
		Type:               "commission_accrue",
		RefType:            "redemption",
		RefId:              tradeNo,
		ExternalRefId:      "accrue:redemption:" + tradeNo,
		SettlementCurrency: "CNY",
		DeltaPending:       0.1,
		Operator:           "system",
		CreatedAt:          common.GetTimestamp(),
	}).Error)

	rows, err := DeleteInvalidRedemptions()
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
	require.Error(t, DB.First(&Redemption{}, redemption.Id).Error)
}

func TestDeleteInvalidRedemptionsKeepsSucceededUsedCodeWithoutCommissionChain(t *testing.T) {
	truncateTables(t)

	redemption := &Redemption{
		Key:                      "redeem-succeeded-missing-chain",
		Name:                     "succeeded redemption without commission chain",
		Status:                   common.RedemptionCodeStatusUsed,
		Quota:                    100,
		UsedUserId:               123,
		RedeemedTime:             common.GetTimestamp(),
		ReferralCommissionStatus: ReferralCommissionJobStatusSucceeded,
	}
	require.NoError(t, DB.Create(redemption).Error)

	rows, err := DeleteInvalidRedemptions()
	require.NoError(t, err)
	require.Zero(t, rows)
	require.NoError(t, DB.First(&Redemption{}, redemption.Id).Error)
	require.ErrorContains(t, DeleteRedemptionById(redemption.Id), "unresolved")
}

func TestDeleteInvalidRedemptionsKeepsSkippedUsedCodeWithoutTerminalJob(t *testing.T) {
	truncateTables(t)

	redemption := &Redemption{
		Key:                      "redeem-skipped-missing-job",
		Name:                     "skipped redemption without job",
		Status:                   common.RedemptionCodeStatusUsed,
		Quota:                    100,
		UsedUserId:               123,
		RedeemedTime:             common.GetTimestamp(),
		ReferralCommissionStatus: ReferralCommissionJobStatusSkipped,
		ReferralCommissionError:  "no_binding",
		ReferralCommissionAt:     common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(redemption).Error)

	rows, err := DeleteInvalidRedemptions()
	require.NoError(t, err)
	require.Zero(t, rows)
	require.NoError(t, DB.First(&Redemption{}, redemption.Id).Error)
	require.ErrorContains(t, DeleteRedemptionById(redemption.Id), "unresolved")

	require.NoError(t, DB.Create(&ReferralCommissionJob{
		SourceType:    "redemption",
		SourceTradeNo: redemptionCommissionTradeNo(redemption.Id),
		Status:        ReferralCommissionJobStatusSkipped,
		LastError:     "no_binding",
		SucceededAt:   common.GetTimestamp(),
	}).Error)

	rows, err = DeleteInvalidRedemptions()
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
	require.Error(t, DB.First(&Redemption{}, redemption.Id).Error)
}

func TestDeleteInvalidRedemptionsKeepsSkippedUsedCodeWithResidualCommission(t *testing.T) {
	truncateTables(t)

	invitee := &User{Username: "redeem-skipped-residual-invitee", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(invitee).Error)
	inviter := &User{Username: "redeem-skipped-residual-inviter", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(inviter).Error)
	affiliate := &ReferralAffiliate{
		UserId:             inviter.Id,
		InviteCode:         "RDMRS001",
		Status:             ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, DB.Create(affiliate).Error)
	redemption := &Redemption{
		Key:                      "redeem-skipped-residual-commission",
		Name:                     "skipped redemption with residual commission",
		Status:                   common.RedemptionCodeStatusUsed,
		Quota:                    100,
		UsedUserId:               invitee.Id,
		RedeemedTime:             common.GetTimestamp(),
		ReferralCommissionStatus: ReferralCommissionJobStatusSkipped,
		ReferralCommissionError:  "no_binding",
		ReferralCommissionAt:     common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(redemption).Error)
	tradeNo := redemptionCommissionTradeNo(redemption.Id)
	require.NoError(t, DB.Create(&ReferralCommissionJob{
		SourceType:    "redemption",
		SourceTradeNo: tradeNo,
		Status:        ReferralCommissionJobStatusSkipped,
		LastError:     "no_binding",
		SucceededAt:   common.GetTimestamp(),
	}).Error)
	require.NoError(t, DB.Create(&ReferralCommission{
		AffiliateId:          affiliate.Id,
		AffiliateUserId:      inviter.Id,
		InviteeUserId:        invitee.Id,
		SourceType:           "redemption",
		SourceOrderId:        redemption.Id,
		SourceTradeNo:        tradeNo,
		OrderType:            "redemption",
		BaseAmount:           1,
		PaidAmount:           1,
		PaidCurrency:         "CNY",
		SettlementCurrency:   "CNY",
		SettlementFxRate:     1,
		SettlementBaseAmount: 1,
		Rate:                 10,
		CommissionAmount:     0.1,
		Status:               ReferralCommissionStatusPending,
	}).Error)

	rows, err := DeleteInvalidRedemptions()
	require.NoError(t, err)
	require.Zero(t, rows)
	require.NoError(t, DB.First(&Redemption{}, redemption.Id).Error)
	require.ErrorContains(t, DeleteRedemptionById(redemption.Id), "unresolved")
}

func TestDeleteInvalidRedemptionsKeepsSucceededUsedCodeWithAccountMismatch(t *testing.T) {
	truncateTables(t)

	invitee := &User{Username: "redeem-succeeded-mismatch-invitee", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(invitee).Error)
	inviter := &User{Username: "redeem-succeeded-mismatch-inviter", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(inviter).Error)
	affiliate := &ReferralAffiliate{
		UserId:             inviter.Id,
		InviteCode:         "RDMBAD01",
		Status:             ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, DB.Create(affiliate).Error)
	redemption := &Redemption{
		Key:                      "redeem-succeeded-account-mismatch",
		Name:                     "succeeded redemption account mismatch",
		Status:                   common.RedemptionCodeStatusUsed,
		Quota:                    100,
		UsedUserId:               invitee.Id,
		RedeemedTime:             common.GetTimestamp(),
		ReferralAffiliateId:      affiliate.Id,
		ReferralCommissionStatus: ReferralCommissionJobStatusSucceeded,
	}
	require.NoError(t, DB.Create(redemption).Error)
	tradeNo := redemptionCommissionTradeNo(redemption.Id)
	require.NoError(t, DB.Create(&ReferralCommissionAccount{
		AffiliateId:   affiliate.Id,
		UserId:        inviter.Id,
		PendingAmount: 0,
	}).Error)
	commission := &ReferralCommission{
		AffiliateId:          affiliate.Id,
		AffiliateUserId:      inviter.Id,
		InviteeUserId:        invitee.Id,
		SourceType:           "redemption",
		SourceOrderId:        redemption.Id,
		SourceTradeNo:        tradeNo,
		OrderType:            "redemption",
		BaseAmount:           1,
		PaidAmount:           1,
		PaidCurrency:         "CNY",
		SettlementCurrency:   "CNY",
		SettlementFxRate:     1,
		SettlementBaseAmount: 1,
		Rate:                 10,
		CommissionAmount:     0.1,
		Status:               ReferralCommissionStatusPending,
	}
	require.NoError(t, DB.Create(commission).Error)
	require.NoError(t, DB.Create(&ReferralCommissionLedger{
		AffiliateId:        affiliate.Id,
		UserId:             inviter.Id,
		CommissionId:       commission.Id,
		Type:               "commission_accrue",
		RefType:            "redemption",
		RefId:              tradeNo,
		ExternalRefId:      "accrue:redemption:" + tradeNo,
		SettlementCurrency: "CNY",
		DeltaPending:       0.1,
		Operator:           "system",
		CreatedAt:          common.GetTimestamp(),
	}).Error)

	rows, err := DeleteInvalidRedemptions()
	require.NoError(t, err)
	require.Zero(t, rows)
	require.NoError(t, DB.First(&Redemption{}, redemption.Id).Error)
	require.ErrorContains(t, DeleteRedemptionById(redemption.Id), "unresolved")
}
