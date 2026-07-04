package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupRiskCleanupTestDB(t *testing.T) {
	t.Helper()

	previousDB := DB
	previousLogDB := LOG_DB
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	LOG_DB = db
	t.Cleanup(func() {
		DB = previousDB
		LOG_DB = previousLogDB
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
}

func createLegacyRiskTables(t *testing.T) {
	t.Helper()

	for _, tableName := range []string{"risk_whitelists", "risk_actions", "risk_events"} {
		require.NoError(t, DB.Exec("CREATE TABLE "+tableName+" (id integer primary key)").Error)
	}
}

func requireLegacyRiskTables(t *testing.T, exists bool) {
	t.Helper()

	for _, tableName := range []string{"risk_whitelists", "risk_actions", "risk_events"} {
		require.Equal(t, exists, DB.Migrator().HasTable(tableName), tableName)
	}
}

func TestMigrateRiskCleanupSkipsByDefault(t *testing.T) {
	setupRiskCleanupTestDB(t)
	createLegacyRiskTables(t)
	t.Setenv(dropLegacyRiskTablesEnv, "")

	require.NoError(t, migrateRiskCleanup())
	requireLegacyRiskTables(t, true)
}

func TestMigrateRiskCleanupDropsOnlyWhenEnabled(t *testing.T) {
	setupRiskCleanupTestDB(t)
	createLegacyRiskTables(t)
	t.Setenv(dropLegacyRiskTablesEnv, "true")

	require.NoError(t, migrateRiskCleanup())
	requireLegacyRiskTables(t, false)
}
