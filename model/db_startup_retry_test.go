package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestWithDatabaseStartupRetryRetriesTransientFailure(t *testing.T) {
	t.Setenv("DB_STARTUP_CONNECT_TIMEOUT_SECONDS", "1")
	t.Setenv("DB_STARTUP_CONNECT_RETRY_INTERVAL_MS", "1")

	attempts := 0
	err := withDatabaseStartupRetry("test database", func() error {
		attempts++
		if attempts < 3 {
			return errors.New("database is not ready")
		}
		return nil
	})

	require.NoError(t, err)
	require.Equal(t, 3, attempts)
}

func TestInitLogDBSkipsMigrationOnSlaveWhenUsingMainDatabase(t *testing.T) {
	db := useSubscriptionPlanMigrationDB(t)
	previousLogDB := LOG_DB
	previousLogDatabaseType := common.LogDatabaseType()
	previousIsMasterNode := common.IsMasterNode
	t.Cleanup(func() {
		LOG_DB = previousLogDB
		common.SetLogDatabaseType(previousLogDatabaseType)
		common.IsMasterNode = previousIsMasterNode
	})

	t.Setenv("LOG_SQL_DSN", "")
	LOG_DB = nil
	common.IsMasterNode = false

	require.NoError(t, InitLogDB())
	require.False(t, db.Migrator().HasTable(&Log{}))
}
