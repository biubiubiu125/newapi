package service

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestBillingAdjustmentSettleReplayDoesNotSplitWalletAndToken(t *testing.T) {
	truncate(t)
	const userID, tokenID = 9101, 9102
	const key = "sk-ledger-wallet"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 1000)
	require.NoError(t, model.DecreaseUserQuotaAllowNegative(userID, 100, true))
	require.NoError(t, model.DecreaseTokenQuota(tokenID, key, 100))

	first := walletLedgerSession(userID, tokenID, key, "req-ledger-wallet", 100)
	require.NoError(t, first.Settle(130))
	require.EqualValues(t, 870, getUserQuota(t, userID))
	require.EqualValues(t, 870, getTokenRemainQuota(t, tokenID))

	replay := walletLedgerSession(userID, tokenID, key, "req-ledger-wallet", 100)
	require.NoError(t, replay.Settle(130))
	require.EqualValues(t, 870, getUserQuota(t, userID))
	require.EqualValues(t, 870, getTokenRemainQuota(t, tokenID))

	conflict := walletLedgerSession(userID, tokenID, key, "req-ledger-wallet", 100)
	require.ErrorIs(t, conflict.Settle(150), model.ErrBillingAdjustmentConflict)
	require.EqualValues(t, 870, getUserQuota(t, userID))
	require.EqualValues(t, 870, getTokenRemainQuota(t, tokenID))

	require.NoError(t, first.Rollback(130))
	require.NoError(t, first.Rollback(130))
	require.EqualValues(t, 1000, getUserQuota(t, userID))
	require.EqualValues(t, 1000, getTokenRemainQuota(t, tokenID))
}

func TestRecalculateDoesNotChargeAppliedBillingAdjustmentAgain(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 9201, 9202, 9203
	const key = "sk-ledger-recalc"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 1000)
	seedChannel(t, channelID)
	require.NoError(t, model.DecreaseUserQuotaAllowNegative(userID, 100, true))
	require.NoError(t, model.DecreaseTokenQuota(tokenID, key, 100))

	session := walletLedgerSession(userID, tokenID, key, "req-ledger-recalc", 100)
	require.NoError(t, session.Settle(130))

	task := makeTask(userID, channelID, 100, tokenID, BillingSourceWallet, 0)
	task.TaskID = "task-ledger-recalc"
	task.Status = model.TaskStatusSuccess
	task.Progress = "100%"
	task.SettlementStatus = model.TaskSettlementStatusPending
	AttachBillingAdjustmentIdentity(task, session.relayInfo, 130)
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RecalculateTaskQuota(context.Background(), task, 130, "completion"))
	require.EqualValues(t, 870, getUserQuota(t, userID))
	require.EqualValues(t, 870, getTokenRemainQuota(t, tokenID))
	require.Equal(t, 130, task.Quota)
	require.False(t, task.PrivateData.BillingAdjustmentUnresolved)
}

func TestRecalculateChargesFullPriceWhenUnflaggedReservationWasRefunded(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 9241, 9242, 9243
	const key = "sk-ledger-unflagged-rollback"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 1000)
	seedChannel(t, channelID)
	require.NoError(t, model.DB.Create(&model.BillingAdjustment{
		IdempotencyKey:   "billing:req-ledger-unflagged-rollback",
		RequestID:        "req-ledger-unflagged-rollback",
		UserID:           userID,
		TokenID:          tokenID,
		Source:           model.BillingAdjustmentSourceWallet,
		FundingDelta:     30,
		TokenDelta:       30,
		ChargeQuota:      130,
		ReservationQuota: 100,
		RefundedQuota:    100,
		TokenIncluded:    true,
		Status:           model.BillingAdjustmentRolledBack,
		CreatedAt:        time.Now().Unix(),
		UpdatedAt:        time.Now().Unix(),
	}).Error)

	task := makeTask(userID, channelID, 100, tokenID, BillingSourceWallet, 0)
	task.TaskID = "task-ledger-unflagged-rollback"
	task.Status = model.TaskStatusSuccess
	task.Progress = "100%"
	task.SettlementStatus = ""
	task.PrivateData.Execution = &model.TaskExecutionSnapshot{RequestID: "req-ledger-unflagged-rollback"}
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RecalculateTaskQuota(context.Background(), task, 130, "completion"))
	require.EqualValues(t, 870, getUserQuota(t, userID))
	require.EqualValues(t, 870, getTokenRemainQuota(t, tokenID))
	require.Equal(t, 130, task.Quota)
}

func TestRefundReturnsCompletionChargeAfterRolledBackReservationWasRecalculated(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 9281, 9282, 9283
	const key = "sk-ledger-rollback-refund"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 1000)
	seedChannel(t, channelID)
	require.NoError(t, model.DB.Create(&model.BillingAdjustment{
		IdempotencyKey:   "billing:req-ledger-rollback-refund",
		RequestID:        "req-ledger-rollback-refund",
		UserID:           userID,
		TokenID:          tokenID,
		Source:           model.BillingAdjustmentSourceWallet,
		FundingDelta:     30,
		TokenDelta:       30,
		ChargeQuota:      130,
		ReservationQuota: 100,
		RefundedQuota:    100,
		TokenIncluded:    true,
		Status:           model.BillingAdjustmentRolledBack,
		CreatedAt:        time.Now().Unix(),
		UpdatedAt:        time.Now().Unix(),
	}).Error)

	task := makeTask(userID, channelID, 100, tokenID, BillingSourceWallet, 0)
	task.TaskID = "task-ledger-rollback-refund"
	task.Status = model.TaskStatusSuccess
	task.Progress = "100%"
	task.SettlementStatus = ""
	task.PrivateData.Execution = &model.TaskExecutionSnapshot{RequestID: "req-ledger-rollback-refund"}
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RecalculateTaskQuota(context.Background(), task, 130, "completion"))
	require.EqualValues(t, 870, getUserQuota(t, userID))
	task.Status = model.TaskStatusFailure

	require.NoError(t, RefundTaskQuota(context.Background(), task, "completion failed"))
	require.NoError(t, RefundTaskQuota(context.Background(), task, "completion failed"))

	require.EqualValues(t, 1000, getUserQuota(t, userID))
	require.EqualValues(t, 1000, getTokenRemainQuota(t, tokenID))
	require.Equal(t, 0, task.Quota)
	row, found, err := model.GetBillingAdjustment("billing:req-ledger-rollback-refund")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, model.BillingAdjustmentRolledBack, row.Status)
	require.Equal(t, 100, row.RefundedQuota)
}

func TestRefundReturnsCompletionChargeWhenItEqualsTheReturnedReservation(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 9284, 9285, 9286
	const key = "sk-ledger-rollback-equal"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 1000)
	seedChannel(t, channelID)
	require.NoError(t, model.DB.Create(&model.BillingAdjustment{
		IdempotencyKey:   "billing:req-ledger-rollback-equal",
		RequestID:        "req-ledger-rollback-equal",
		UserID:           userID,
		TokenID:          tokenID,
		Source:           model.BillingAdjustmentSourceWallet,
		FundingDelta:     0,
		TokenDelta:       0,
		ChargeQuota:      100,
		ReservationQuota: 100,
		RefundedQuota:    100,
		TokenIncluded:    true,
		Status:           model.BillingAdjustmentRolledBack,
		CreatedAt:        time.Now().Unix(),
		UpdatedAt:        time.Now().Unix(),
	}).Error)

	task := makeTask(userID, channelID, 100, tokenID, BillingSourceWallet, 0)
	task.TaskID = "task-ledger-rollback-equal"
	task.Status = model.TaskStatusSuccess
	task.Progress = "100%"
	task.SettlementStatus = ""
	task.PrivateData.Execution = &model.TaskExecutionSnapshot{RequestID: "req-ledger-rollback-equal"}
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RecalculateTaskQuota(context.Background(), task, 100, "completion"))
	require.EqualValues(t, 900, getUserQuota(t, userID))
	task.Status = model.TaskStatusFailure

	require.NoError(t, RefundTaskQuota(context.Background(), task, "completion failed"))

	require.EqualValues(t, 1000, getUserQuota(t, userID))
	require.EqualValues(t, 1000, getTokenRemainQuota(t, tokenID))
	require.Equal(t, 0, task.Quota)
}

func TestLedgerRollbackDoesNotRecordRefundWhenWalletCannotAcceptCredit(t *testing.T) {
	truncate(t)
	const userID, tokenID = 9287, 9288
	const key = "sk-ledger-cap"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 1000)
	session := walletLedgerSession(userID, tokenID, key, "req-ledger-cap", 0)
	require.NoError(t, session.Settle(100))
	require.EqualValues(t, 900, getUserQuota(t, userID))
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Update("quota", common.MaxWalletQuota).Error)

	err := session.Rollback(100)

	require.ErrorIs(t, err, model.ErrUserQuotaCap)
	require.Equal(t, common.MaxWalletQuota, getUserQuota(t, userID))
	require.EqualValues(t, 900, getTokenRemainQuota(t, tokenID))
	row, found, loadErr := model.GetBillingAdjustment("billing:req-ledger-cap")
	require.NoError(t, loadErr)
	require.True(t, found)
	require.Equal(t, model.BillingAdjustmentApplied, row.Status)
	require.False(t, session.refunded)
}

func TestWalletRefundDoesNotClaimCreditBeyondWalletCap(t *testing.T) {
	truncate(t)
	const userID = 9289
	seedUser(t, userID, int(common.MaxWalletQuota))
	funding := &WalletFunding{userId: userID, consumed: 25}

	err := funding.Refund()

	require.ErrorIs(t, err, model.ErrUserQuotaCap)
	require.Equal(t, common.MaxWalletQuota, getUserQuota(t, userID))
	require.Equal(t, 25, funding.consumed)
}

func TestRecalculateDoesNotStackUnflaggedPendingLedger(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 9251, 9252, 9253
	const key = "sk-ledger-unflagged-pending"
	seedUser(t, userID, 900)
	seedToken(t, tokenID, userID, key, 900)
	seedChannel(t, channelID)
	require.NoError(t, model.DB.Create(&model.BillingAdjustment{
		IdempotencyKey:   "billing:req-ledger-unflagged-pending",
		RequestID:        "req-ledger-unflagged-pending",
		UserID:           userID,
		TokenID:          tokenID,
		Source:           model.BillingAdjustmentSourceWallet,
		FundingDelta:     30,
		TokenDelta:       30,
		ChargeQuota:      130,
		ReservationQuota: 100,
		TokenIncluded:    true,
		Status:           model.BillingAdjustmentPending,
		CreatedAt:        time.Now().Unix(),
		UpdatedAt:        time.Now().Unix(),
	}).Error)

	task := makeTask(userID, channelID, 100, tokenID, BillingSourceWallet, 0)
	task.TaskID = "task-ledger-unflagged-pending"
	task.Status = model.TaskStatusSuccess
	task.Progress = "100%"
	task.SettlementStatus = ""
	task.PrivateData.Execution = &model.TaskExecutionSnapshot{RequestID: "req-ledger-unflagged-pending"}
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RecalculateTaskQuota(context.Background(), task, 130, "completion"))
	require.NoError(t, model.RecoverPendingBillingAdjustments(10))
	require.EqualValues(t, 870, getUserQuota(t, userID))
	require.EqualValues(t, 870, getTokenRemainQuota(t, tokenID))
	require.Equal(t, 130, task.Quota)
}

func TestRefundDoesNotCreditUnflaggedRolledBackReservationAgain(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 9261, 9262, 9263
	const key = "sk-ledger-unflagged-refund"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 1000)
	seedChannel(t, channelID)
	require.NoError(t, model.DB.Create(&model.BillingAdjustment{
		IdempotencyKey:   "billing:req-ledger-unflagged-refund",
		RequestID:        "req-ledger-unflagged-refund",
		UserID:           userID,
		TokenID:          tokenID,
		Source:           model.BillingAdjustmentSourceWallet,
		FundingDelta:     30,
		TokenDelta:       30,
		ChargeQuota:      130,
		ReservationQuota: 100,
		RefundedQuota:    100,
		TokenIncluded:    true,
		Status:           model.BillingAdjustmentRolledBack,
		CreatedAt:        time.Now().Unix(),
		UpdatedAt:        time.Now().Unix(),
	}).Error)

	task := makeTask(userID, channelID, 100, tokenID, BillingSourceWallet, 0)
	task.TaskID = "task-ledger-unflagged-refund"
	task.Status = model.TaskStatusFailure
	task.Progress = "100%"
	task.SettlementStatus = ""
	task.PrivateData.Execution = &model.TaskExecutionSnapshot{RequestID: "req-ledger-unflagged-refund"}
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RefundTaskQuota(context.Background(), task, "upstream failed"))
	require.EqualValues(t, 1000, getUserQuota(t, userID))
	require.EqualValues(t, 1000, getTokenRemainQuota(t, tokenID))
	require.Equal(t, 0, task.Quota)
}

func TestRecalculateWaitsUntilSharedBillingAdjustmentExists(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 9211, 9212, 9213
	const key = "sk-ledger-missing"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 1000)
	seedChannel(t, channelID)
	require.NoError(t, model.DecreaseUserQuotaAllowNegative(userID, 100, true))
	require.NoError(t, model.DecreaseTokenQuota(tokenID, key, 100))

	session := walletLedgerSession(userID, tokenID, key, "req-ledger-missing", 100)
	task := makeTask(userID, channelID, 100, tokenID, BillingSourceWallet, 0)
	task.TaskID = "task-ledger-missing"
	task.Status = model.TaskStatusSuccess
	task.Progress = "100%"
	task.SettlementStatus = model.TaskSettlementStatusPending
	AttachBillingAdjustmentIdentity(task, session.relayInfo, 130)
	require.NoError(t, model.DB.Create(task).Error)

	err := RecalculateTaskQuota(context.Background(), task, 130, "completion")
	require.ErrorIs(t, err, model.ErrBillingAdjustmentDeferred)
	require.EqualValues(t, 900, getUserQuota(t, userID))
	require.EqualValues(t, 900, getTokenRemainQuota(t, tokenID))
	require.True(t, task.PrivateData.BillingAdjustmentUnresolved)
	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	require.True(t, stored.PrivateData.BillingAdjustmentUnresolved)

	require.NoError(t, session.Settle(130))
	require.EqualValues(t, 870, getUserQuota(t, userID))
	require.EqualValues(t, 870, getTokenRemainQuota(t, tokenID))
	require.NoError(t, RecalculateTaskQuota(context.Background(), task, 130, "completion"))
	require.EqualValues(t, 870, getUserQuota(t, userID))
	require.EqualValues(t, 870, getTokenRemainQuota(t, tokenID))
	require.Equal(t, 130, task.Quota)
	require.False(t, task.PrivateData.BillingAdjustmentUnresolved)
}

func TestRecalculateImportsLocalBillingAdjustmentSpoolOnce(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 9221, 9222, 9223
	const key = "sk-ledger-local-spool"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 1000)
	seedChannel(t, channelID)
	require.NoError(t, model.DecreaseUserQuotaAllowNegative(userID, 100, true))
	require.NoError(t, model.DecreaseTokenQuota(tokenID, key, 100))
	model.BillingAdjustmentSpoolDir = t.TempDir()
	t.Cleanup(func() {
		model.BillingAdjustmentSpoolDir = ""
		model.ClearUnpersistedBillingAdjustments()
	})
	model.ClearUnpersistedBillingAdjustments()
	require.NoError(t, model.RememberUnpersistedBillingAdjustment(model.BillingAdjustmentRequest{
		IdempotencyKey:   "billing:req-ledger-local-spool",
		RequestID:        "req-ledger-local-spool",
		UserID:           userID,
		TokenID:          tokenID,
		TokenKey:         key,
		Source:           model.BillingAdjustmentSourceWallet,
		FundingDelta:     30,
		TokenDelta:       30,
		ChargeQuota:      130,
		ReservationQuota: 100,
		TokenIncluded:    true,
	}))

	session := walletLedgerSession(userID, tokenID, key, "req-ledger-local-spool", 100)
	task := makeTask(userID, channelID, 100, tokenID, BillingSourceWallet, 0)
	task.TaskID = "task-ledger-local-spool"
	task.Status = model.TaskStatusSuccess
	task.Progress = "100%"
	task.SettlementStatus = model.TaskSettlementStatusPending
	AttachBillingAdjustmentIdentity(task, session.relayInfo, 130)
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RecalculateTaskQuota(context.Background(), task, 130, "completion"))
	require.EqualValues(t, 870, getUserQuota(t, userID))
	require.EqualValues(t, 870, getTokenRemainQuota(t, tokenID))
	require.Equal(t, 130, task.Quota)
	require.False(t, task.PrivateData.BillingAdjustmentUnresolved)
	require.NoError(t, RecalculateTaskQuota(context.Background(), task, 130, "completion"))
	require.EqualValues(t, 870, getUserQuota(t, userID))
	require.EqualValues(t, 870, getTokenRemainQuota(t, tokenID))
}

func TestImageSettlementWaitsUntilSharedBillingAdjustmentExists(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 9231, 9232, 9233
	const key = "sk-ledger-image-missing"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 1000)
	seedChannel(t, channelID)
	require.NoError(t, model.DecreaseUserQuotaAllowNegative(userID, 100, true))
	require.NoError(t, model.DecreaseTokenQuota(tokenID, key, 100))

	session := walletLedgerSession(userID, tokenID, key, "req-ledger-image-missing", 100)
	task := imageLedgerTask(t, userID, channelID, tokenID, 100, session, 130, "task-ledger-image-missing")

	applied, err := ApplyImageTaskSettlementAtomic(context.Background(), task, ImageTaskAtomicSettlement{ActualQuota: 130})
	require.ErrorIs(t, err, model.ErrBillingAdjustmentDeferred)
	require.False(t, applied)
	require.EqualValues(t, 900, getUserQuota(t, userID))
	require.EqualValues(t, 900, getTokenRemainQuota(t, tokenID))
	require.True(t, task.PrivateData.BillingAdjustmentUnresolved)
	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	require.True(t, stored.PrivateData.BillingAdjustmentUnresolved)
	require.Equal(t, 100, stored.Quota)

	require.NoError(t, session.Settle(130))
	require.EqualValues(t, 870, getUserQuota(t, userID))
	require.EqualValues(t, 870, getTokenRemainQuota(t, tokenID))
	applied, err = ApplyImageTaskSettlementAtomic(context.Background(), task, ImageTaskAtomicSettlement{ActualQuota: 130})
	require.NoError(t, err)
	require.True(t, applied)
	require.EqualValues(t, 870, getUserQuota(t, userID))
	require.EqualValues(t, 870, getTokenRemainQuota(t, tokenID))
	require.Equal(t, 130, task.Quota)
	require.False(t, task.PrivateData.BillingAdjustmentUnresolved)
}

func TestImageSettlementImportsLocalBillingAdjustmentSpoolOnce(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 9241, 9242, 9243
	const key = "sk-ledger-image-spool"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 1000)
	seedChannel(t, channelID)
	require.NoError(t, model.DecreaseUserQuotaAllowNegative(userID, 100, true))
	require.NoError(t, model.DecreaseTokenQuota(tokenID, key, 100))
	model.BillingAdjustmentSpoolDir = t.TempDir()
	t.Cleanup(func() {
		model.BillingAdjustmentSpoolDir = ""
		model.ClearUnpersistedBillingAdjustments()
	})
	model.ClearUnpersistedBillingAdjustments()
	require.NoError(t, model.RememberUnpersistedBillingAdjustment(model.BillingAdjustmentRequest{
		IdempotencyKey:   "billing:req-ledger-image-spool",
		RequestID:        "req-ledger-image-spool",
		UserID:           userID,
		TokenID:          tokenID,
		TokenKey:         key,
		Source:           model.BillingAdjustmentSourceWallet,
		FundingDelta:     30,
		TokenDelta:       30,
		ChargeQuota:      130,
		ReservationQuota: 100,
		TokenIncluded:    true,
	}))

	session := walletLedgerSession(userID, tokenID, key, "req-ledger-image-spool", 100)
	task := imageLedgerTask(t, userID, channelID, tokenID, 100, session, 130, "task-ledger-image-spool")

	applied, err := ApplyImageTaskSettlementAtomic(context.Background(), task, ImageTaskAtomicSettlement{ActualQuota: 130})
	require.NoError(t, err)
	require.True(t, applied)
	require.EqualValues(t, 870, getUserQuota(t, userID))
	require.EqualValues(t, 870, getTokenRemainQuota(t, tokenID))
	require.Equal(t, 130, task.Quota)
	require.False(t, task.PrivateData.BillingAdjustmentUnresolved)

	require.NoError(t, model.RecoverUnpersistedBillingAdjustments(100))
	require.NoError(t, model.RecoverPendingBillingAdjustments(100))
	require.EqualValues(t, 870, getUserQuota(t, userID))
	require.EqualValues(t, 870, getTokenRemainQuota(t, tokenID))
}

func imageLedgerTask(t *testing.T, userID int, channelID int, tokenID int, quota int, session *BillingSession, actualQuota int, taskID string) *model.Task {
	t.Helper()
	task := makeTask(userID, channelID, quota, tokenID, BillingSourceWallet, 0)
	task.TaskID = taskID
	task.Platform = constant.TaskPlatformImage
	task.Status = model.TaskStatusSuccess
	task.Progress = "100%"
	task.SettlementStatus = model.TaskSettlementStatusPending
	AttachBillingAdjustmentIdentity(task, session.relayInfo, actualQuota)
	require.NoError(t, model.DB.Create(task).Error)
	require.NoError(t, model.DB.Create(&model.TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        model.TaskSettlementRecordStatusApplying,
		Operation:     model.TaskSettlementOperationImageAtomic,
	}).Error)
	return task
}

func TestRefundTaskQuotaReleasesRolledBackAdjustmentOnce(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 9301, 9302, 9303
	const key = "sk-ledger-refund"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 1000)
	seedChannel(t, channelID)
	require.NoError(t, model.DecreaseUserQuotaAllowNegative(userID, 100, true))
	require.NoError(t, model.DecreaseTokenQuota(tokenID, key, 100))

	session := walletLedgerSession(userID, tokenID, key, "req-ledger-refund", 100)
	require.NoError(t, session.Settle(130))

	task := makeTask(userID, channelID, 130, tokenID, BillingSourceWallet, 0)
	task.TaskID = "task-ledger-refund"
	task.Status = model.TaskStatusFailure
	task.Progress = "100%"
	task.RefundPending = true
	AttachBillingAdjustmentIdentity(task, session.relayInfo, 130)
	task.PrivateData.BillingAdjustmentRollback = true
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RefundTaskQuota(context.Background(), task, "upstream failed"))
	require.NoError(t, RefundTaskQuota(context.Background(), task, "upstream failed"))
	require.EqualValues(t, 1000, getUserQuota(t, userID))
	require.EqualValues(t, 1000, getTokenRemainQuota(t, tokenID))
	require.Equal(t, 0, task.Quota)
	require.False(t, task.RefundPending)
}

func TestSubscriptionBillingAdjustmentRollbackOnce(t *testing.T) {
	truncate(t)
	const userID, tokenID, subscriptionID = 9401, 9402, 9403
	const key = "sk-ledger-subscription"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 1000)
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id:          9404,
		Title:       "ledger",
		PriceAmount: 1,
	}).Error)
	seedSubscription(t, subscriptionID, userID, 1000, 100)
	require.NoError(t, model.DB.Create(&model.SubscriptionPreConsumeRecord{
		RequestId:          "req-ledger-subscription",
		UserId:             userID,
		UserSubscriptionId: subscriptionID,
		PreConsumed:        100,
		Status:             "consumed",
	}).Error)
	require.NoError(t, model.DecreaseTokenQuota(tokenID, key, 100))

	info := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		TokenKey:        key,
		RequestId:       "req-ledger-subscription",
		OriginModelName: "test-model",
		UsingGroup:      "default",
	}
	session := &BillingSession{
		relayInfo: info,
		funding: &SubscriptionFunding{
			requestId:      info.RequestId,
			subscriptionId: subscriptionID,
			preConsumed:    100,
		},
		preConsumedQuota: 100,
		tokenConsumed:    100,
	}
	info.Billing = session
	require.NoError(t, session.Settle(130))
	require.EqualValues(t, 130, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 870, getTokenRemainQuota(t, tokenID))

	require.NoError(t, session.Rollback(130))
	require.NoError(t, session.Rollback(130))
	require.EqualValues(t, 0, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 1000, getTokenRemainQuota(t, tokenID))
}

func walletLedgerSession(userID, tokenID int, key, requestID string, preConsumed int) *BillingSession {
	info := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		TokenKey:        key,
		RequestId:       requestID,
		OriginModelName: "test-model",
		UsingGroup:      "default",
	}
	session := &BillingSession{
		relayInfo:        info,
		funding:          &WalletFunding{userId: userID},
		preConsumedQuota: preConsumed,
		tokenConsumed:    preConsumed,
	}
	info.Billing = session
	return session
}

func TestBillingAdjustmentSettleChargesWalletWhenTokenWasDeleted(t *testing.T) {
	truncate(t)
	const userID, tokenID = 9801, 9802
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, "sk-deleted-token", 1000)
	require.NoError(t, model.DecreaseUserQuotaAllowNegative(userID, 100, true))
	require.NoError(t, model.DB.Delete(&model.Token{}, tokenID).Error)

	session := walletLedgerSession(userID, tokenID, "sk-deleted-token", "req-deleted-token", 100)
	require.NoError(t, session.Settle(130))
	require.EqualValues(t, 870, getUserQuota(t, userID))
}

func TestPendingBillingAdjustmentRecoveryAppliesOnce(t *testing.T) {
	truncate(t)
	const userID, tokenID = 9501, 9502
	seedUser(t, userID, 900)
	seedToken(t, tokenID, userID, "sk-ledger-pending", 900)
	require.NoError(t, model.DB.Create(&model.BillingAdjustment{
		IdempotencyKey:   "billing:req-ledger-pending",
		RequestID:        "req-ledger-pending",
		UserID:           userID,
		TokenID:          tokenID,
		Source:           model.BillingAdjustmentSourceWallet,
		FundingDelta:     30,
		TokenDelta:       30,
		ChargeQuota:      130,
		ReservationQuota: 100,
		TokenIncluded:    true,
		Status:           model.BillingAdjustmentPending,
		CreatedAt:        time.Now().Unix(),
		UpdatedAt:        time.Now().Unix(),
	}).Error)

	require.NoError(t, model.RecoverPendingBillingAdjustments(10))
	require.NoError(t, model.RecoverPendingBillingAdjustments(10))
	require.EqualValues(t, 870, getUserQuota(t, userID))
	require.EqualValues(t, 870, getTokenRemainQuota(t, tokenID))
}

func TestBillingAdjustmentSettleAllowsSubscriptionOverdraft(t *testing.T) {
	truncate(t)
	const userID, tokenID, subscriptionID = 9641, 9642, 9643
	const key = "sk-ledger-sub-overdraft"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 0)
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id:          9644,
		Title:       "overdraft",
		PriceAmount: 1,
	}).Error)
	seedSubscription(t, subscriptionID, userID, 100, 100)
	require.NoError(t, model.DB.Create(&model.SubscriptionPreConsumeRecord{
		RequestId:          "req-ledger-sub-overdraft",
		UserId:             userID,
		UserSubscriptionId: subscriptionID,
		PreConsumed:        100,
		Status:             "consumed",
	}).Error)
	info := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		TokenKey:        key,
		RequestId:       "req-ledger-sub-overdraft",
		OriginModelName: "test-model",
		UsingGroup:      "default",
	}
	session := &BillingSession{
		relayInfo: info,
		funding: &SubscriptionFunding{
			requestId:      info.RequestId,
			subscriptionId: subscriptionID,
			preConsumed:    100,
		},
		preConsumedQuota: 100,
		tokenConsumed:    100,
	}
	info.Billing = session
	require.NoError(t, session.Settle(120))
	require.EqualValues(t, 120, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, -20, getTokenRemainQuota(t, tokenID))
}

func TestBillingAdjustmentSettleAllowsDeliveredOverdraft(t *testing.T) {
	truncate(t)
	const userID, tokenID = 9601, 9602
	const key = "sk-ledger-overdraft"
	seedUser(t, userID, 90)
	seedToken(t, tokenID, userID, key, 0)

	session := walletLedgerSession(userID, tokenID, key, "req-ledger-overdraft", 10)
	require.NoError(t, session.Settle(20))
	require.EqualValues(t, 80, getUserQuota(t, userID))
	require.EqualValues(t, -10, getTokenRemainQuota(t, tokenID))
}

func TestBillingAdjustmentSettleDefersMissingSubscriptionUntilRecovery(t *testing.T) {
	truncate(t)
	const userID, tokenID, subscriptionID = 9611, 9612, 9613
	const key = "sk-ledger-deferred"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 990)
	info := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		TokenKey:        key,
		RequestId:       "req-ledger-deferred",
		OriginModelName: "test-model",
		UsingGroup:      "default",
	}
	session := &BillingSession{
		relayInfo: info,
		funding: &SubscriptionFunding{
			requestId:      info.RequestId,
			subscriptionId: subscriptionID,
			preConsumed:    10,
		},
		preConsumedQuota: 10,
		tokenConsumed:    10,
	}
	info.Billing = session

	require.ErrorIs(t, session.Settle(30), model.ErrBillingAdjustmentDeferred)
	require.EqualValues(t, 1000, getUserQuota(t, userID))
	require.EqualValues(t, 990, getTokenRemainQuota(t, tokenID))

	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id:          9614,
		Title:       "deferred",
		PriceAmount: 1,
	}).Error)
	seedSubscription(t, subscriptionID, userID, 1000, 10)
	require.NoError(t, model.RecoverPendingBillingAdjustments(10))
	require.NoError(t, model.RecoverPendingBillingAdjustments(10))
	require.EqualValues(t, 30, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 970, getTokenRemainQuota(t, tokenID))
}

func TestBillingAdjustmentSettleLeavesPendingRowWhenEnsureFailsRetryably(t *testing.T) {
	truncate(t)
	const userID, tokenID, subscriptionID = 9651, 9652, 9653
	const key = "sk-ledger-ensure-retry"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 990)
	info := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		TokenKey:        key,
		RequestId:       "req-ledger-ensure-retry",
		OriginModelName: "test-model",
		UsingGroup:      "default",
	}
	failures := 0
	model.BillingAdjustmentBeforeEnsurePending = func() error {
		failures++
		if failures < 3 {
			return errors.New("database is locked")
		}
		return nil
	}
	t.Cleanup(func() {
		model.BillingAdjustmentBeforeEnsurePending = nil
	})
	session := &BillingSession{
		relayInfo: info,
		funding: &SubscriptionFunding{
			requestId:      info.RequestId,
			subscriptionId: subscriptionID,
			preConsumed:    10,
		},
		preConsumedQuota: 10,
		tokenConsumed:    10,
	}
	info.Billing = session

	require.ErrorIs(t, session.Settle(30), model.ErrBillingAdjustmentDeferred)
	require.EqualValues(t, 1000, getUserQuota(t, userID))
	require.EqualValues(t, 990, getTokenRemainQuota(t, tokenID))
	row, found, err := model.GetBillingAdjustment(BillingAdjustmentKey(info))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, model.BillingAdjustmentPending, row.Status)
	require.EqualValues(t, 30, row.ChargeQuota)
}

func TestBillingAdjustmentSettleDefersWhenPendingCommitIsAmbiguous(t *testing.T) {
	truncate(t)
	const userID, tokenID, subscriptionID = 9661, 9662, 9663
	const key = "sk-ledger-ensure-ambiguous"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 990)
	info := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		TokenKey:        key,
		RequestId:       "req-ledger-ensure-ambiguous",
		OriginModelName: "test-model",
		UsingGroup:      "default",
	}
	model.BillingAdjustmentAfterEnsurePending = func() error {
		return errors.New("driver returned an error after commit")
	}
	t.Cleanup(func() {
		model.BillingAdjustmentAfterEnsurePending = nil
	})
	session := &BillingSession{
		relayInfo: info,
		funding: &SubscriptionFunding{
			requestId:      info.RequestId,
			subscriptionId: subscriptionID,
			preConsumed:    10,
		},
		preConsumedQuota: 10,
		tokenConsumed:    10,
	}
	info.Billing = session

	require.ErrorIs(t, session.Settle(30), model.ErrBillingAdjustmentDeferred)
	require.EqualValues(t, 1000, getUserQuota(t, userID))
	row, found, err := model.GetBillingAdjustment(BillingAdjustmentKey(info))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, model.BillingAdjustmentPending, row.Status)
}

func TestRefundTaskQuotaReturnsSubscriptionExtraWhenLedgerRowMissing(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID, subscriptionID = 9621, 9622, 9623, 9624
	const key = "sk-ledger-extra"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 1000)
	seedChannel(t, channelID)
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id:          9625,
		Title:       "extra",
		PriceAmount: 1,
	}).Error)
	seedSubscription(t, subscriptionID, userID, 1000, 120)
	require.NoError(t, model.DB.Create(&model.SubscriptionPreConsumeRecord{
		RequestId:          "req-ledger-extra",
		UserId:             userID,
		UserSubscriptionId: subscriptionID,
		PreConsumed:        100,
		Status:             "consumed",
	}).Error)
	require.NoError(t, model.DecreaseTokenQuota(tokenID, key, 120))

	info := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		TokenKey:        key,
		RequestId:       "req-ledger-extra",
		OriginModelName: "test-model",
		UsingGroup:      "default",
	}
	session := &BillingSession{
		relayInfo: info,
		funding: &SubscriptionFunding{
			requestId:      info.RequestId,
			subscriptionId: subscriptionID,
			preConsumed:    100,
		},
		preConsumedQuota: 120,
		tokenConsumed:    120,
		extraReserved:    20,
	}
	info.Billing = session
	task := makeTask(userID, channelID, 120, tokenID, BillingSourceSubscription, subscriptionID)
	task.TaskID = "task-ledger-extra"
	task.Status = model.TaskStatusFailure
	task.Progress = "100%"
	task.RefundPending = true
	AttachBillingAdjustmentIdentity(task, info, 120)
	require.Equal(t, 20, task.PrivateData.BillingAdjustmentExtra)
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RefundTaskQuota(context.Background(), task, "upstream failed"))
	require.NoError(t, RefundTaskQuota(context.Background(), task, "upstream failed"))
	require.EqualValues(t, 0, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 1000, getTokenRemainQuota(t, tokenID))
	require.Equal(t, 0, task.Quota)
}

func TestSubscriptionReservationRefundRestoresPreconsumeWhenRecordMissing(t *testing.T) {
	truncate(t)
	const userID, tokenID, subscriptionID = 9701, 9702, 9703
	const key = "sk-ledger-missing-record"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 1000)
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{Id: 9704, Title: "missing", PriceAmount: 1}).Error)
	seedSubscription(t, subscriptionID, userID, 1000, 120)
	require.NoError(t, model.DecreaseTokenQuota(tokenID, key, 120))
	now := time.Now().Unix()
	require.NoError(t, model.DB.Create(&model.BillingAdjustment{
		IdempotencyKey:   "billing:req-missing-record",
		RequestID:        "req-missing-record",
		UserID:           userID,
		TokenID:          tokenID,
		Source:           model.BillingAdjustmentSourceSubscription,
		SubscriptionID:   subscriptionID,
		ChargeQuota:      120,
		ReservationQuota: 120,
		ExtraReserved:    20,
		TokenIncluded:    true,
		Status:           model.BillingAdjustmentPending,
		CreatedAt:        now,
		UpdatedAt:        now,
	}).Error)

	require.NoError(t, model.RollbackBillingAdjustment("billing:req-missing-record", 0))
	require.NoError(t, model.RollbackBillingAdjustment("billing:req-missing-record", 0))
	require.EqualValues(t, 0, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 1000, getTokenRemainQuota(t, tokenID))
}

func TestSubscriptionRollbackWithRecordAfterResetKeepsNewUsage(t *testing.T) {
	truncate(t)
	const userID, tokenID, subscriptionID = 9711, 9712, 9713
	const key = "sk-ledger-reset-record"
	const requestID = "req-ledger-reset-record"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 1000)
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{Id: 9714, Title: "reset", PriceAmount: 1}).Error)
	seedSubscription(t, subscriptionID, userID, 5000, 400)
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("id = ?", subscriptionID).UpdateColumn("last_reset_time", time.Now().Unix()).Error)
	require.NoError(t, model.DB.Create(&model.SubscriptionPreConsumeRecord{
		RequestId:          requestID,
		UserId:             userID,
		UserSubscriptionId: subscriptionID,
		PreConsumed:        1000,
		Status:             "consumed",
	}).Error)
	consumedAt := time.Now().Unix() - 86400
	require.NoError(t, model.DB.Model(&model.SubscriptionPreConsumeRecord{}).Where("request_id = ?", requestID).UpdateColumn("created_at", consumedAt).Error)
	require.NoError(t, model.DecreaseTokenQuota(tokenID, key, 1000))
	require.NoError(t, model.DB.Create(&model.BillingAdjustment{
		IdempotencyKey:         "billing:" + requestID,
		RequestID:              requestID,
		UserID:                 userID,
		TokenID:                tokenID,
		Source:                 model.BillingAdjustmentSourceSubscription,
		SubscriptionID:         subscriptionID,
		ChargeQuota:            1000,
		ReservationQuota:       1000,
		TokenIncluded:          true,
		Status:                 model.BillingAdjustmentApplied,
		SubscriptionConsumedAt: consumedAt,
		CreatedAt:              consumedAt,
		UpdatedAt:              consumedAt,
	}).Error)

	require.NoError(t, model.RollbackBillingAdjustment("billing:"+requestID, 1000))
	require.NoError(t, model.RollbackBillingAdjustment("billing:"+requestID, 1000))
	require.EqualValues(t, 400, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 1000, getTokenRemainQuota(t, tokenID))
	var record model.SubscriptionPreConsumeRecord
	require.NoError(t, model.DB.Where("request_id = ?", requestID).First(&record).Error)
	require.Equal(t, "refunded", record.Status)
}

func TestSubscriptionAppliedRollbackRefundsWhenRecordMissing(t *testing.T) {
	truncate(t)
	const userID, tokenID, subscriptionID = 9721, 9722, 9723
	const key = "sk-ledger-applied-missing"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 1000)
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{Id: 9724, Title: "applied-missing", PriceAmount: 1}).Error)
	seedSubscription(t, subscriptionID, userID, 1000, 130)
	require.NoError(t, model.DecreaseTokenQuota(tokenID, key, 130))
	now := time.Now().Unix()
	require.NoError(t, model.DB.Create(&model.BillingAdjustment{
		IdempotencyKey:   "billing:req-applied-missing",
		RequestID:        "req-applied-missing",
		UserID:           userID,
		TokenID:          tokenID,
		Source:           model.BillingAdjustmentSourceSubscription,
		SubscriptionID:   subscriptionID,
		ChargeQuota:      130,
		ReservationQuota: 100,
		FundingDelta:     30,
		TokenDelta:       30,
		TokenIncluded:    true,
		Status:           model.BillingAdjustmentApplied,
		CreatedAt:        now,
		UpdatedAt:        now,
	}).Error)

	require.NoError(t, model.RollbackBillingAdjustment("billing:req-applied-missing", 130))
	require.NoError(t, model.RollbackBillingAdjustment("billing:req-applied-missing", 130))
	require.EqualValues(t, 0, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 1000, getTokenRemainQuota(t, tokenID))
}

func TestSubscriptionReservationRefundAfterResetDoesNotWipeNewUsage(t *testing.T) {
	truncate(t)
	const userID, tokenID, subscriptionID = 9731, 9732, 9733
	const key = "sk-ledger-reset-missing"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 1000)
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{Id: 9734, Title: "reset-missing", PriceAmount: 1}).Error)
	seedSubscription(t, subscriptionID, userID, 5000, 50)
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("id = ?", subscriptionID).UpdateColumn("last_reset_time", time.Now().Unix()).Error)
	require.NoError(t, model.DecreaseTokenQuota(tokenID, key, 120))
	consumedAt := time.Now().Unix() - 86400
	require.NoError(t, model.DB.Create(&model.BillingAdjustment{
		IdempotencyKey:         "billing:req-reset-missing",
		RequestID:              "req-reset-missing",
		UserID:                 userID,
		TokenID:                tokenID,
		Source:                 model.BillingAdjustmentSourceSubscription,
		SubscriptionID:         subscriptionID,
		ChargeQuota:            120,
		ReservationQuota:       120,
		ExtraReserved:          20,
		TokenIncluded:          true,
		Status:                 model.BillingAdjustmentPending,
		SubscriptionConsumedAt: consumedAt,
		CreatedAt:              consumedAt,
		UpdatedAt:              consumedAt,
	}).Error)

	require.NoError(t, model.RollbackBillingAdjustment("billing:req-reset-missing", 0))
	require.EqualValues(t, 50, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 1000, getTokenRemainQuota(t, tokenID))
}

func TestCleanupKeepsConsumedSubscriptionRecordForRollback(t *testing.T) {
	truncate(t)
	const userID, subscriptionID = 9741, 9743
	const requestID = "req-cleanup-consumed"
	seedUser(t, userID, 1000)
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{Id: 9744, Title: "cleanup", PriceAmount: 1}).Error)
	seedSubscription(t, subscriptionID, userID, 1000, 80)
	require.NoError(t, model.DB.Create(&model.SubscriptionPreConsumeRecord{
		RequestId:          requestID,
		UserId:             userID,
		UserSubscriptionId: subscriptionID,
		PreConsumed:        80,
		Status:             "consumed",
	}).Error)
	old := time.Now().Unix() - 10*24*3600
	require.NoError(t, model.DB.Model(&model.SubscriptionPreConsumeRecord{}).Where("request_id = ?", requestID).UpdateColumns(map[string]any{
		"created_at": old,
		"updated_at": old,
	}).Error)
	require.NoError(t, model.DB.Create(&model.BillingAdjustment{
		IdempotencyKey:         "billing:" + requestID,
		RequestID:              requestID,
		UserID:                 userID,
		Source:                 model.BillingAdjustmentSourceSubscription,
		SubscriptionID:         subscriptionID,
		ChargeQuota:            80,
		ReservationQuota:       80,
		Status:                 model.BillingAdjustmentApplied,
		SubscriptionConsumedAt: old,
		CreatedAt:              old,
		UpdatedAt:              old,
	}).Error)

	n, err := model.CleanupSubscriptionPreConsumeRecords(7 * 24 * 3600)
	require.NoError(t, err)
	require.EqualValues(t, 0, n)
	require.NoError(t, model.RollbackBillingAdjustment("billing:"+requestID, 80))
	require.EqualValues(t, 0, getSubscriptionUsed(t, subscriptionID))
}

func TestBillingAdjustmentSettleSpoolsWhenPendingRowCannotBeWritten(t *testing.T) {
	truncate(t)
	const userID, tokenID, subscriptionID = 9751, 9752, 9753
	const key = "sk-ledger-spool"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 990)
	info := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		TokenKey:        key,
		RequestId:       "req-ledger-spool",
		OriginModelName: "test-model",
		UsingGroup:      "default",
	}
	model.BillingAdjustmentSpoolDir = t.TempDir()
	t.Cleanup(func() {
		model.BillingAdjustmentSpoolDir = ""
	})
	model.BillingAdjustmentBeforeEnsurePending = func() error {
		return errors.New("database is locked")
	}
	t.Cleanup(func() {
		model.BillingAdjustmentBeforeEnsurePending = nil
	})
	session := &BillingSession{
		relayInfo: info,
		funding: &SubscriptionFunding{
			requestId:      info.RequestId,
			subscriptionId: subscriptionID,
			preConsumed:    10,
		},
		preConsumedQuota: 10,
		tokenConsumed:    10,
	}
	info.Billing = session

	require.ErrorIs(t, session.Settle(30), model.ErrBillingAdjustmentDeferred)
	_, found, err := model.GetBillingAdjustment(BillingAdjustmentKey(info))
	require.NoError(t, err)
	require.False(t, found)
	require.EqualValues(t, 1000, getUserQuota(t, userID))
	require.EqualValues(t, 990, getTokenRemainQuota(t, tokenID))

	model.BillingAdjustmentBeforeEnsurePending = nil
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id:          9754,
		Title:       "spool",
		PriceAmount: 1,
	}).Error)
	seedSubscription(t, subscriptionID, userID, 1000, 10)
	require.NoError(t, model.RecoverUnpersistedBillingAdjustments(10))
	require.NoError(t, model.RecoverPendingBillingAdjustments(10))
	require.EqualValues(t, 30, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 970, getTokenRemainQuota(t, tokenID))
}

func TestBillingAdjustmentSettleDefersWhenSpoolCannotBeWritten(t *testing.T) {
	truncate(t)
	const userID, tokenID, subscriptionID = 9991, 9992, 9993
	const key = "sk-ledger-spool-memory"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 990)
	info := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		TokenKey:        key,
		RequestId:       "req-ledger-spool-memory",
		OriginModelName: "test-model",
		UsingGroup:      "default",
	}
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(blocked, []byte("x"), 0o600))
	model.BillingAdjustmentSpoolDir = filepath.Join(blocked, "billing-adjustments")
	model.BillingAdjustmentFallbackSpoolDir = filepath.Join(blocked, "fallback-billing-adjustments")
	t.Cleanup(func() {
		model.BillingAdjustmentSpoolDir = ""
		model.BillingAdjustmentFallbackSpoolDir = ""
		model.ClearUnpersistedBillingAdjustments()
	})
	model.ClearUnpersistedBillingAdjustments()
	model.BillingAdjustmentBeforeEnsurePending = func() error {
		return errors.New("database is locked")
	}
	t.Cleanup(func() {
		model.BillingAdjustmentBeforeEnsurePending = nil
	})
	session := &BillingSession{
		relayInfo: info,
		funding: &SubscriptionFunding{
			requestId:      info.RequestId,
			subscriptionId: subscriptionID,
			preConsumed:    10,
		},
		preConsumedQuota: 10,
		tokenConsumed:    10,
	}
	info.Billing = session

	require.ErrorIs(t, session.Settle(30), model.ErrBillingAdjustmentDeferred)
	_, found, err := model.GetBillingAdjustment(BillingAdjustmentKey(info))
	require.NoError(t, err)
	require.False(t, found)
	require.EqualValues(t, 1000, getUserQuota(t, userID))
	require.EqualValues(t, 990, getTokenRemainQuota(t, tokenID))

	model.BillingAdjustmentBeforeEnsurePending = nil
	model.BillingAdjustmentSpoolDir = t.TempDir()
	model.BillingAdjustmentFallbackSpoolDir = t.TempDir()
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id:          9994,
		Title:       "spool-memory",
		PriceAmount: 1,
	}).Error)
	seedSubscription(t, subscriptionID, userID, 1000, 10)
	require.NoError(t, model.RecoverUnpersistedBillingAdjustments(10))
	require.NoError(t, model.RecoverPendingBillingAdjustments(10))
	require.EqualValues(t, 30, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 970, getTokenRemainQuota(t, tokenID))
	require.NoError(t, model.RecoverUnpersistedBillingAdjustments(10))
	require.NoError(t, model.RecoverPendingBillingAdjustments(10))
	require.EqualValues(t, 30, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 970, getTokenRemainQuota(t, tokenID))
}

func TestBillingAdjustmentSettleAfterSubscriptionResetChargesActual(t *testing.T) {
	truncate(t)
	const userID, tokenID, subscriptionID = 9761, 9762, 9763
	const key = "sk-ledger-reset-equal"
	seedResetSubscriptionLedger(t, userID, tokenID, subscriptionID, 9764, key, "req-ledger-reset-equal", 1000, 400, 5000)
	session := subscriptionLedgerSession(userID, tokenID, subscriptionID, key, "req-ledger-reset-equal", 1000)
	require.NoError(t, session.Settle(1000))
	require.NoError(t, session.Settle(1000))
	require.EqualValues(t, 1400, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 5000, getTokenRemainQuota(t, tokenID))
}

func TestBillingAdjustmentSettleAfterSubscriptionResetDownwardChargesActual(t *testing.T) {
	truncate(t)
	const userID, tokenID, subscriptionID = 9771, 9772, 9773
	const key = "sk-ledger-reset-down"
	seedResetSubscriptionLedger(t, userID, tokenID, subscriptionID, 9774, key, "req-ledger-reset-down", 1000, 400, 5000)
	session := subscriptionLedgerSession(userID, tokenID, subscriptionID, key, "req-ledger-reset-down", 1000)
	require.NoError(t, session.Settle(200))
	require.EqualValues(t, 600, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 5800, getTokenRemainQuota(t, tokenID))
}

func TestBillingAdjustmentSettleAfterSubscriptionResetZeroKeepsNewUsage(t *testing.T) {
	truncate(t)
	const userID, tokenID, subscriptionID = 9781, 9782, 9783
	const key = "sk-ledger-reset-zero"
	seedResetSubscriptionLedger(t, userID, tokenID, subscriptionID, 9784, key, "req-ledger-reset-zero", 1000, 400, 5000)
	session := subscriptionLedgerSession(userID, tokenID, subscriptionID, key, "req-ledger-reset-zero", 1000)
	require.NoError(t, session.Settle(0))
	require.EqualValues(t, 400, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 6000, getTokenRemainQuota(t, tokenID))
}

func TestBillingAdjustmentRecoverAfterSubscriptionResetChargesActual(t *testing.T) {
	truncate(t)
	const userID, tokenID, subscriptionID = 9791, 9792, 9793
	const key = "sk-ledger-reset-recover"
	const requestID = "req-ledger-reset-recover"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 4800)
	require.NoError(t, model.DB.Create(&model.SubscriptionPreConsumeRecord{
		RequestId:          requestID,
		UserId:             userID,
		UserSubscriptionId: subscriptionID,
		PreConsumed:        1000,
		Status:             "consumed",
	}).Error)
	var pre model.SubscriptionPreConsumeRecord
	require.NoError(t, model.DB.Where("request_id = ?", requestID).First(&pre).Error)
	session := subscriptionLedgerSession(userID, tokenID, subscriptionID, key, requestID, 1000)
	require.ErrorIs(t, session.Settle(1200), model.ErrBillingAdjustmentDeferred)
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id:          9794,
		Title:       "reset-recover",
		PriceAmount: 1,
	}).Error)
	seedSubscription(t, subscriptionID, userID, 100000, 400)
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("id = ?", subscriptionID).Updates(map[string]any{
		"amount_used":     400,
		"last_reset_time": pre.CreatedAt + 10,
	}).Error)
	require.NoError(t, model.RecoverPendingBillingAdjustments(10))
	require.NoError(t, model.RecoverPendingBillingAdjustments(10))
	require.EqualValues(t, 1600, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 4600, getTokenRemainQuota(t, tokenID))
}

func TestBillingAdjustmentSettleSubscriptionDownwardSamePeriod(t *testing.T) {
	truncate(t)
	const userID, tokenID, subscriptionID = 9755, 9756, 9757
	const key = "sk-ledger-same-period"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 5000)
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id:          9758,
		Title:       "same-period",
		PriceAmount: 1,
	}).Error)
	seedSubscription(t, subscriptionID, userID, 100000, 1000)
	require.NoError(t, model.DB.Create(&model.SubscriptionPreConsumeRecord{
		RequestId:          "req-ledger-same-period",
		UserId:             userID,
		UserSubscriptionId: subscriptionID,
		PreConsumed:        1000,
		Status:             "consumed",
	}).Error)
	session := subscriptionLedgerSession(userID, tokenID, subscriptionID, key, "req-ledger-same-period", 1000)
	require.NoError(t, session.Settle(200))
	require.EqualValues(t, 200, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 5800, getTokenRemainQuota(t, tokenID))
}

func seedResetSubscriptionLedger(t *testing.T, userID, tokenID, subscriptionID, planID int, key, requestID string, preConsumed int, newUsed int64, tokenRemain int) {
	t.Helper()
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, tokenRemain)
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id:          planID,
		Title:       "reset",
		PriceAmount: 1,
	}).Error)
	seedSubscription(t, subscriptionID, userID, 100000, int64(preConsumed))
	require.NoError(t, model.DB.Create(&model.SubscriptionPreConsumeRecord{
		RequestId:          requestID,
		UserId:             userID,
		UserSubscriptionId: subscriptionID,
		PreConsumed:        int64(preConsumed),
		Status:             "consumed",
	}).Error)
	var pre model.SubscriptionPreConsumeRecord
	require.NoError(t, model.DB.Where("request_id = ?", requestID).First(&pre).Error)
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("id = ?", subscriptionID).Updates(map[string]any{
		"amount_used":     newUsed,
		"last_reset_time": pre.CreatedAt + 10,
	}).Error)
}

func TestBillingAdjustmentSettleAfterResetDoesNotRecountReserve(t *testing.T) {
	truncate(t)
	const userID, tokenID, subscriptionID = 9931, 9932, 9933
	const key = "sk-ledger-reset-extra"
	seedResetSubscriptionLedger(t, userID, tokenID, subscriptionID, 9934, key, "req-ledger-reset-extra", 1000, 400, 5000)
	session := subscriptionLedgerSession(userID, tokenID, subscriptionID, key, "req-ledger-reset-extra", 1000)
	require.NoError(t, session.Reserve(1200))
	require.EqualValues(t, 600, getSubscriptionUsed(t, subscriptionID))
	require.NoError(t, session.Settle(1200))
	require.NoError(t, session.Settle(1200))
	require.EqualValues(t, 1600, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 4800, getTokenRemainQuota(t, tokenID))
}

func TestBillingAdjustmentRollbackAfterResetReserveReturnsNewPeriodCharge(t *testing.T) {
	truncate(t)
	const userID, tokenID, subscriptionID = 9941, 9942, 9943
	const key = "sk-ledger-reset-extra-rollback"
	const requestID = "req-ledger-reset-extra-rollback"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 5000)
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{Id: 9944, Title: "reset-extra-rollback", PriceAmount: 1}).Error)
	seedSubscription(t, subscriptionID, userID, 100000, 1000)
	require.NoError(t, model.DB.Create(&model.SubscriptionPreConsumeRecord{
		RequestId:          requestID,
		UserId:             userID,
		UserSubscriptionId: subscriptionID,
		PreConsumed:        1000,
		Status:             "consumed",
	}).Error)
	consumedAt := time.Now().Unix() - 3600
	require.NoError(t, model.DB.Model(&model.SubscriptionPreConsumeRecord{}).Where("request_id = ?", requestID).UpdateColumn("created_at", consumedAt).Error)
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("id = ?", subscriptionID).Updates(map[string]any{
		"amount_used":     400,
		"last_reset_time": consumedAt + 10,
	}).Error)
	session := subscriptionLedgerSession(userID, tokenID, subscriptionID, key, requestID, 1000)
	require.NoError(t, session.Reserve(1200))
	require.NoError(t, session.Settle(1200))
	require.NoError(t, session.Rollback(1200))
	require.NoError(t, session.Rollback(1200))
	require.EqualValues(t, 400, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 6000, getTokenRemainQuota(t, tokenID))
}

func TestBillingAdjustmentRefundBeforeResetExtraDoesNotTouchNewUsage(t *testing.T) {
	truncate(t)
	const userID, tokenID, subscriptionID = 9951, 9952, 9953
	const key = "sk-ledger-extra-before-reset"
	const requestID = "req-ledger-extra-before-reset"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 5000)
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{Id: 9954, Title: "extra-before-reset", PriceAmount: 1}).Error)
	seedSubscription(t, subscriptionID, userID, 100000, 1000)
	require.NoError(t, model.DB.Create(&model.SubscriptionPreConsumeRecord{
		RequestId:          requestID,
		UserId:             userID,
		UserSubscriptionId: subscriptionID,
		PreConsumed:        1000,
		Status:             "consumed",
	}).Error)
	session := subscriptionLedgerSession(userID, tokenID, subscriptionID, key, requestID, 1000)
	require.NoError(t, session.Reserve(1200))
	require.EqualValues(t, 1200, getSubscriptionUsed(t, subscriptionID))
	var pre model.SubscriptionPreConsumeRecord
	require.NoError(t, model.DB.Where("request_id = ?", requestID).First(&pre).Error)
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("id = ?", subscriptionID).Updates(map[string]any{
		"amount_used":     400,
		"last_reset_time": pre.CreatedAt + 10,
	}).Error)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.NoError(t, session.Refund(ctx))
	require.EqualValues(t, 400, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 6000, getTokenRemainQuota(t, tokenID))
}

func TestApplySubscriptionTaskUsageAfterResetDoesNotRecountReserve(t *testing.T) {
	truncate(t)
	const userID, subscriptionID = 9971, 9972
	const requestID = "req-task-reset-extra"
	seedUser(t, userID, 1000)
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{Id: 9974, Title: "task-reset-extra", PriceAmount: 1}).Error)
	seedSubscription(t, subscriptionID, userID, 100000, 600)
	consumedAt := time.Now().Unix() - 3600
	require.NoError(t, model.DB.Create(&model.SubscriptionPreConsumeRecord{
		RequestId:          requestID,
		UserId:             userID,
		UserSubscriptionId: subscriptionID,
		PreConsumed:        1000,
		PostResetReserved:  200,
		Status:             "consumed",
	}).Error)
	require.NoError(t, model.DB.Model(&model.SubscriptionPreConsumeRecord{}).Where("request_id = ?", requestID).UpdateColumn("created_at", consumedAt).Error)
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("id = ?", subscriptionID).Update("last_reset_time", consumedAt+10).Error)
	task := &model.Task{
		TaskID: requestID,
		Quota:  1200,
		PrivateData: model.TaskPrivateData{
			SubscriptionId:         subscriptionID,
			SubscriptionConsumedAt: consumedAt,
			Execution:              &model.TaskExecutionSnapshot{RequestID: requestID},
		},
	}
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		return model.ApplySubscriptionTaskUsageTx(tx, task, 1200, 1200)
	}))
	require.EqualValues(t, 1600, getSubscriptionUsed(t, subscriptionID))

	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("id = ?", subscriptionID).Update("amount_used", 600).Error)
	refund := *task
	refund.Quota = 1200
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		return model.ApplySubscriptionTaskUsageTx(tx, &refund, 1200, 0)
	}))
	require.EqualValues(t, 400, getSubscriptionUsed(t, subscriptionID))
}

func TestBillingAdjustmentSettleSamePeriodReserveStillNets(t *testing.T) {
	truncate(t)
	const userID, tokenID, subscriptionID = 9961, 9962, 9963
	const key = "sk-ledger-same-extra"
	const requestID = "req-ledger-same-extra"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 5000)
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{Id: 9964, Title: "same-extra", PriceAmount: 1}).Error)
	seedSubscription(t, subscriptionID, userID, 100000, 1000)
	require.NoError(t, model.DB.Create(&model.SubscriptionPreConsumeRecord{
		RequestId:          requestID,
		UserId:             userID,
		UserSubscriptionId: subscriptionID,
		PreConsumed:        1000,
		Status:             "consumed",
	}).Error)
	session := subscriptionLedgerSession(userID, tokenID, subscriptionID, key, requestID, 1000)
	require.NoError(t, session.Reserve(1200))
	require.NoError(t, session.Settle(200))
	require.EqualValues(t, 200, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 5800, getTokenRemainQuota(t, tokenID))
}

func TestSettleDeliveredBillingChargesDeltaAfterRollback(t *testing.T) {
	truncate(t)
	const userID, tokenID = 9961, 9962
	const key = "sk-delivered-rollback"
	const requestID = "req-delivered-rollback"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 1000)
	require.NoError(t, model.DecreaseUserQuotaAllowNegative(userID, 100, true))
	require.NoError(t, model.DecreaseTokenQuota(tokenID, key, 100))

	first := walletLedgerSession(userID, tokenID, key, requestID, 100)
	require.NoError(t, first.Settle(130))
	require.NoError(t, first.Rollback(130))
	require.EqualValues(t, 1000, getUserQuota(t, userID))
	require.EqualValues(t, 1000, getTokenRemainQuota(t, tokenID))

	require.NoError(t, model.DecreaseUserQuotaAllowNegative(userID, 100, true))
	require.NoError(t, model.DecreaseTokenQuota(tokenID, key, 100))
	second := walletLedgerSession(userID, tokenID, key, requestID, 100)
	second.relayInfo.UserQuota = 100000
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	charged, err := settleDeliveredBilling(ctx, second.relayInfo, 130)
	require.NoError(t, err)
	require.Equal(t, 130, charged)
	require.EqualValues(t, 870, getUserQuota(t, userID))
	require.EqualValues(t, 870, getTokenRemainQuota(t, tokenID))

	charged, err = settleDeliveredBilling(ctx, second.relayInfo, 150)
	require.NoError(t, err)
	require.Equal(t, 130, charged)
	require.EqualValues(t, 870, getUserQuota(t, userID))
	require.EqualValues(t, 870, getTokenRemainQuota(t, tokenID))

	require.NoError(t, second.Rollback(130))
	require.EqualValues(t, 1000, getUserQuota(t, userID))
	require.EqualValues(t, 1000, getTokenRemainQuota(t, tokenID))
}

func TestSettleDeliveredBillingKeepsAppliedChargeOnConflict(t *testing.T) {
	truncate(t)
	const userID, tokenID = 9971, 9972
	const key = "sk-delivered-conflict"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 1000)
	require.NoError(t, model.DecreaseUserQuotaAllowNegative(userID, 100, true))
	require.NoError(t, model.DecreaseTokenQuota(tokenID, key, 100))

	first := walletLedgerSession(userID, tokenID, key, "req-delivered-conflict", 100)
	require.NoError(t, first.Settle(130))
	second := walletLedgerSession(userID, tokenID, key, "req-delivered-conflict", 100)
	second.relayInfo.UserQuota = 100000
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	charged, err := settleDeliveredBilling(ctx, second.relayInfo, 150)
	require.NoError(t, err)
	require.Equal(t, 130, charged)
	require.EqualValues(t, 870, getUserQuota(t, userID))
	require.EqualValues(t, 870, getTokenRemainQuota(t, tokenID))
}

func TestSubscriptionSettleDeliveredBillingAfterRollbackChargesOnce(t *testing.T) {
	truncate(t)
	const userID, tokenID, subscriptionID = 9981, 9982, 9983
	const key = "sk-delivered-subscription"
	const requestID = "req-delivered-subscription"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 1000)
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{Id: 9984, Title: "delivered", PriceAmount: 1}).Error)
	seedSubscription(t, subscriptionID, userID, 1000, 100)
	require.NoError(t, model.DB.Create(&model.SubscriptionPreConsumeRecord{
		RequestId:          requestID,
		UserId:             userID,
		UserSubscriptionId: subscriptionID,
		PreConsumed:        100,
		Status:             "consumed",
	}).Error)
	require.NoError(t, model.DecreaseTokenQuota(tokenID, key, 100))

	session := subscriptionLedgerSession(userID, tokenID, subscriptionID, key, requestID, 100)
	require.NoError(t, session.Settle(130))
	require.NoError(t, session.Rollback(130))
	require.EqualValues(t, 0, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 1000, getTokenRemainQuota(t, tokenID))
	session.relayInfo.UserQuota = 100000

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	charged, err := settleDeliveredBilling(ctx, session.relayInfo, 130)
	require.NoError(t, err)
	require.Equal(t, 130, charged)
	require.EqualValues(t, 130, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 870, getTokenRemainQuota(t, tokenID))

	charged, err = settleDeliveredBilling(ctx, session.relayInfo, 130)
	require.NoError(t, err)
	require.Equal(t, 130, charged)
	require.EqualValues(t, 130, getSubscriptionUsed(t, subscriptionID))

	require.NoError(t, session.Rollback(130))
	require.EqualValues(t, 0, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 1000, getTokenRemainQuota(t, tokenID))
}

func TestNonIdempotentSettleChargesWalletWhenTokenRemainIsShort(t *testing.T) {
	truncate(t)
	const userID, tokenID = 9951, 9952
	const key = "sk-short-token"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 100)
	require.NoError(t, model.DecreaseUserQuotaAllowNegative(userID, 100, true))
	require.NoError(t, model.DecreaseTokenQuota(tokenID, key, 100))

	info := &relaycommon.RelayInfo{
		UserId:   userID,
		TokenId:  tokenID,
		TokenKey: key,
	}
	session := &BillingSession{
		relayInfo:        info,
		funding:          &WalletFunding{userId: userID},
		preConsumedQuota: 100,
		tokenConsumed:    100,
	}
	info.Billing = session
	require.NoError(t, session.Settle(110))
	require.EqualValues(t, 890, getUserQuota(t, userID))
	require.EqualValues(t, -10, getTokenRemainQuota(t, tokenID))
}

func subscriptionLedgerSession(userID, tokenID, subscriptionID int, key, requestID string, preConsumed int) *BillingSession {
	info := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		TokenKey:        key,
		RequestId:       requestID,
		OriginModelName: "test-model",
		UsingGroup:      "default",
	}
	session := &BillingSession{
		relayInfo: info,
		funding: &SubscriptionFunding{
			requestId:      requestID,
			subscriptionId: subscriptionID,
			preConsumed:    int64(preConsumed),
		},
		preConsumedQuota: preConsumed,
		tokenConsumed:    preConsumed,
	}
	info.Billing = session
	return session
}
