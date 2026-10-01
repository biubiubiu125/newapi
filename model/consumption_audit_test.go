package model

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestCompleteConsumptionAuditOverlappedInsertsKeepOneLog(t *testing.T) {
	truncateTables(t)
	oldExport := common.DataExportEnabled
	common.DataExportEnabled = false
	t.Cleanup(func() {
		common.DataExportEnabled = oldExport
		consumptionAuditBeforeLogInsert = nil
		DB.Exec("DELETE FROM consumption_audits")
		DB.Exec("DELETE FROM logs")
	})

	const key = "audit:req-overlap"
	payload, err := json.Marshal(ConsumptionAuditPayload{
		Username:         "overlap-owner",
		RequestID:        "req-overlap",
		PromptTokens:     20,
		CompletionTokens: 10,
		ModelName:        "gpt-4o",
		TokenName:        "overlap-token",
		Quota:            30,
		Content:          "overlap",
		Group:            "default",
	})
	require.NoError(t, err)
	require.NoError(t, EnsureConsumptionAuditSchema(DB))
	require.NoError(t, DB.Create(&ConsumptionAudit{
		IdempotencyKey: key,
		RequestID:      "req-overlap",
		UserID:         9801,
		ChannelID:      9802,
		TokenID:        9803,
		Quota:          30,
		Status:         ConsumptionAuditUsageApplied,
		Payload:        string(payload),
		CreatedAt:      1,
		UpdatedAt:      1,
	}).Error)

	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	consumptionAuditBeforeLogInsert = func() {
		entered <- struct{}{}
		<-release
	}

	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- CompleteConsumptionAudit(key)
		}()
	}
	<-entered
	<-entered
	close(release)
	wg.Wait()
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)

	var count int64
	require.NoError(t, LOG_DB.Model(&Log{}).Where("settlement_key = ?", key).Count(&count).Error)
	require.Equal(t, int64(1), count)
	var row ConsumptionAudit
	require.NoError(t, DB.Where("idempotency_key = ?", key).First(&row).Error)
	require.Equal(t, ConsumptionAuditDone, row.Status)
}

func TestAuditSettlementKeyUniqueLeavesOtherLogKeysReusable(t *testing.T) {
	truncateTables(t)
	t.Cleanup(func() {
		DB.Exec("DELETE FROM logs")
	})
	indexed, err := ensureConsumptionAuditLogIndex(LOG_DB)
	require.NoError(t, err)
	require.True(t, indexed)

	for range 2 {
		require.NoError(t, createLog(&Log{UserId: 9804, Type: LogTypeConsume, SettlementKey: ""}))
		require.NoError(t, createLog(&Log{UserId: 9804, Type: LogTypeConsume, SettlementKey: "task_settlement:9804"}))
	}
	require.NoError(t, createLog(&Log{UserId: 9804, Type: LogTypeConsume, SettlementKey: "audit:req-once"}))
	err = createLog(&Log{UserId: 9804, Type: LogTypeConsume, SettlementKey: "audit:req-once"})
	require.Error(t, err)
	require.True(t, isLogSettlementKeyConflict(err))
}

func TestConsumptionAuditLogIndexUsesPrefixedTable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{TablePrefix: "auditpref_"},
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Log{}))

	indexed, err := ensureConsumptionAuditLogIndex(db)
	require.NoError(t, err)
	require.True(t, indexed)

	var count int
	require.NoError(t, db.Raw(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ? AND tbl_name = ?`,
		"idx_auditpref_logs_audit_settlement_key",
		"auditpref_logs",
	).Scan(&count).Error)
	require.Equal(t, 1, count)

	require.NoError(t, db.Create(&Log{UserId: 1, Type: LogTypeConsume, SettlementKey: "audit:prefixed"}).Error)
	err = db.Create(&Log{UserId: 1, Type: LogTypeConsume, SettlementKey: "audit:prefixed"}).Error
	require.Error(t, err)
	require.True(t, isLogSettlementKeyConflict(err))
	require.NoError(t, db.Create(&Log{UserId: 1, Type: LogTypeConsume, SettlementKey: "task:reusable"}).Error)
	require.NoError(t, db.Create(&Log{UserId: 1, Type: LogTypeConsume, SettlementKey: "task:reusable"}).Error)
}
