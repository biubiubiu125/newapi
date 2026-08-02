package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func useSubscriptionPlanMigrationDB(t *testing.T) *gorm.DB {
	t.Helper()

	previousDB := DB
	previousMainDatabaseType := common.MainDatabaseType()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		DB = previousDB
		common.SetMainDatabaseType(previousMainDatabaseType)
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestEnsureSubscriptionPlanTableSQLiteCreatesGrantGroupsColumn(t *testing.T) {
	db := useSubscriptionPlanMigrationDB(t)

	require.NoError(t, ensureSubscriptionPlanTableSQLite())
	require.True(t, db.Migrator().HasColumn(&SubscriptionPlan{}, "GrantGroups"))
}
