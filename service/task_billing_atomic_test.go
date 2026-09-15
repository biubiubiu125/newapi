package service

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func failAsyncTaskSettlementAppliedUpdates(t *testing.T, times int32) {
	t.Helper()
	remaining := times
	callbackName := fmt.Sprintf("test:fail_async_settlement_applied:%s", t.Name())
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Table != "task_settlement_records" {
			return
		}
		status, ok := settlementUpdateStatus(tx.Statement.Dest)
		if !ok || status != model.TaskSettlementRecordStatusApplied {
			return
		}
		if atomic.AddInt32(&remaining, -1) >= 0 {
			tx.AddError(errors.New("forced settlement record failure"))
		}
	}))
	t.Cleanup(func() {
		_ = model.DB.Callback().Update().Remove(callbackName)
	})
}

func failLogCreates(t *testing.T) func() {
	t.Helper()
	callbackName := fmt.Sprintf("test:fail_log_creates:%s", t.Name())
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "logs" {
			tx.AddError(errors.New("forced billing log failure"))
		}
	}))
	stop := func() {
		_ = model.DB.Callback().Create().Remove(callbackName)
	}
	t.Cleanup(stop)
	return stop
}

func settlementUpdateStatus(dest any) (string, bool) {
	value, ok := dest.(map[string]any)
	if !ok {
		return "", false
	}
	status, ok := value["status"].(string)
	return status, ok
}

func TestRecalculateTaskQuotaRollsBackWhenSettlementRecordUpdateFails(t *testing.T) {
	truncate(t)
	failAsyncTaskSettlementAppliedUpdates(t, 1)
	ctx := context.Background()

	const userID, tokenID, channelID = 12101, 12102, 12103
	const initQuota, preConsumed, actualQuota, tokenRemain = 10000, 2000, 3000, 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-atomic-record-fail", tokenRemain)
	seedChannel(t, channelID)
	setUserUsageCounters(t, userID, preConsumed, 1)
	setChannelUsedQuota(t, channelID, int64(preConsumed))

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	err := RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment")
	require.Error(t, err)
	require.Contains(t, err.Error(), "forced settlement record failure")
	assert.EqualValues(t, initQuota, getUserQuota(t, userID))
	assert.EqualValues(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	assert.EqualValues(t, preConsumed, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.EqualValues(t, int64(preConsumed), getChannelUsedQuota(t, channelID))
	assert.Equal(t, int64(0), countLogs(t))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, preConsumed, reloaded.Quota)
	assert.NotEqual(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)

	record, exists, err := model.GetTaskSettlementRecord(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	assert.Equal(t, model.TaskSettlementRecordStatusPrepared, record.Status)
	assert.False(t, record.HasAppliedQuotaEvidence())
}

func TestRecalculateTaskQuotaRetriesOnceAfterSettlementRecordFailure(t *testing.T) {
	truncate(t)
	failAsyncTaskSettlementAppliedUpdates(t, 1)
	ctx := context.Background()

	const userID, tokenID, channelID = 12111, 12112, 12113
	const initQuota, preConsumed, actualQuota, tokenRemain = 10000, 2000, 3000, 5000
	delta := actualQuota - preConsumed

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-atomic-record-retry", tokenRemain)
	seedChannel(t, channelID)
	setUserUsageCounters(t, userID, preConsumed, 1)
	setChannelUsedQuota(t, channelID, int64(preConsumed))

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	err := RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment")
	require.Error(t, err)
	assert.EqualValues(t, initQuota, getUserQuota(t, userID))

	require.NoError(t, RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment retry"))
	assert.EqualValues(t, initQuota-delta, getUserQuota(t, userID))
	assert.EqualValues(t, tokenRemain-delta, getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	assert.EqualValues(t, actualQuota, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(1), countLogs(t))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, actualQuota, reloaded.Quota)
	assert.Equal(t, model.TaskSettlementStatusSettled, reloaded.SettlementStatus)

	record, exists, err := model.GetTaskSettlementRecord(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	assert.Equal(t, model.TaskSettlementRecordStatusApplied, record.Status)
	require.NotNil(t, record.AppliedQuota)
	assert.Equal(t, actualQuota, *record.AppliedQuota)
}

func TestRefundTaskQuotaRollsBackWhenSettlementRecordUpdateFails(t *testing.T) {
	truncate(t)
	failAsyncTaskSettlementAppliedUpdates(t, 1)
	ctx := context.Background()

	const userID, tokenID, channelID = 12121, 12122, 12123
	const initQuota, preConsumed, tokenRemain = 10000, 3000, 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-refund-atomic-record-fail", tokenRemain)
	seedChannel(t, channelID)
	setUserUsageCounters(t, userID, preConsumed, 1)
	setChannelUsedQuota(t, channelID, int64(preConsumed))

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.RefundPending = true
	require.NoError(t, model.DB.Create(task).Error)

	err := RefundTaskQuota(ctx, task, "task failed after submit")
	require.Error(t, err)
	require.Contains(t, err.Error(), "forced settlement record failure")
	assert.EqualValues(t, initQuota, getUserQuota(t, userID))
	assert.EqualValues(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	assert.EqualValues(t, preConsumed, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(0), countLogs(t))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, preConsumed, reloaded.Quota)
	assert.True(t, reloaded.RefundPending)
	assert.NotEqual(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)

	record, exists, err := model.GetTaskSettlementRecord(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	assert.Equal(t, model.TaskSettlementRecordStatusPrepared, record.Status)
}

func TestRefundTaskQuotaRetriesOnceAfterSettlementRecordFailure(t *testing.T) {
	truncate(t)
	failAsyncTaskSettlementAppliedUpdates(t, 1)
	ctx := context.Background()

	const userID, tokenID, channelID = 12131, 12132, 12133
	const initQuota, preConsumed, tokenRemain = 10000, 3000, 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-refund-atomic-record-retry", tokenRemain)
	seedChannel(t, channelID)
	setUserUsageCounters(t, userID, preConsumed, 1)
	setChannelUsedQuota(t, channelID, int64(preConsumed))

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.RefundPending = true
	require.NoError(t, model.DB.Create(task).Error)

	err := RefundTaskQuota(ctx, task, "task failed after submit")
	require.Error(t, err)
	assert.EqualValues(t, initQuota, getUserQuota(t, userID))

	require.NoError(t, RefundTaskQuota(ctx, task, "task failed after submit retry"))
	assert.EqualValues(t, initQuota+preConsumed, getUserQuota(t, userID))
	assert.EqualValues(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	assert.EqualValues(t, 0, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(1), countLogs(t))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, 0, reloaded.Quota)
	assert.False(t, reloaded.RefundPending)

	record, exists, err := model.GetTaskSettlementRecord(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	assert.Equal(t, model.TaskSettlementRecordStatusApplied, record.Status)
}
