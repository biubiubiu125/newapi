package service

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestSlaveSystemTaskRunnerStartsLocalBillingRecovery(t *testing.T) {
	oldMaster := common.IsMasterNode
	oldOnce := systemTaskRunnerOnce
	oldStart := startNodeLocalBillingRecoveryFn
	common.IsMasterNode = false
	systemTaskRunnerOnce = sync.Once{}
	called := make(chan struct{}, 1)
	startNodeLocalBillingRecoveryFn = func() {
		select {
		case called <- struct{}{}:
		default:
		}
	}
	t.Cleanup(func() {
		common.IsMasterNode = oldMaster
		systemTaskRunnerOnce = oldOnce
		startNodeLocalBillingRecoveryFn = oldStart
	})

	StartSystemTaskRunner()

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("slave runner did not start local billing recovery")
	}
}

func TestNodeLocalBillingRecoveryAppliesMemorySettle(t *testing.T) {
	truncate(t)
	const userID, tokenID, subscriptionID = 9981, 9982, 9983
	const key = "sk-ledger-slave-memory"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, key, 990)
	session := subscriptionLedgerSession(userID, tokenID, subscriptionID, key, "req-ledger-slave-memory", 10)

	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(blocked, []byte("x"), 0o600))
	model.BillingAdjustmentSpoolDir = filepath.Join(blocked, "billing-adjustments")
	model.BillingAdjustmentFallbackSpoolDir = filepath.Join(blocked, "fallback-billing-adjustments")
	model.ClearUnpersistedBillingAdjustments()
	t.Cleanup(func() {
		model.BillingAdjustmentSpoolDir = ""
		model.BillingAdjustmentFallbackSpoolDir = ""
		model.ClearUnpersistedBillingAdjustments()
	})
	model.BillingAdjustmentBeforeEnsurePending = func() error {
		return errors.New("database is locked")
	}
	t.Cleanup(func() {
		model.BillingAdjustmentBeforeEnsurePending = nil
	})

	require.ErrorIs(t, session.Settle(30), model.ErrBillingAdjustmentDeferred)
	_, found, err := model.GetBillingAdjustment(BillingAdjustmentKey(session.relayInfo))
	require.NoError(t, err)
	require.False(t, found)

	model.BillingAdjustmentBeforeEnsurePending = nil
	model.BillingAdjustmentSpoolDir = t.TempDir()
	model.BillingAdjustmentFallbackSpoolDir = t.TempDir()
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id:          9984,
		Title:       "slave-memory",
		PriceAmount: 1,
	}).Error)
	seedSubscription(t, subscriptionID, userID, 1000, 10)

	recoverNodeLocalBillingAdjustments()
	require.EqualValues(t, 30, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 970, getTokenRemainQuota(t, tokenID))

	recoverNodeLocalBillingAdjustments()
	require.EqualValues(t, 30, getSubscriptionUsed(t, subscriptionID))
	require.EqualValues(t, 970, getTokenRemainQuota(t, tokenID))
}
