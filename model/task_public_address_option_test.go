package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func useTaskPublicAddressOptionDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := DB
	previousType := common.MainDatabaseType()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Option{}))
	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		DB = previousDB
		common.SetMainDatabaseType(previousType)
	})
	return db
}

func TestUpdateOptionSyncsTaskPublicAddressToSystemSettings(t *testing.T) {
	useTaskPublicAddressOptionDB(t)
	previousPublicAddress := system_setting.TaskPublicAddress
	previousMap := common.OptionMap
	t.Cleanup(func() {
		system_setting.TaskPublicAddress = previousPublicAddress
		common.OptionMap = previousMap
	})
	common.OptionMap = map[string]string{}

	require.NoError(t, UpdateOption("TaskPublicAddress", "https://media.example.com/prefix/"))
	assert.Equal(t, "https://media.example.com/prefix/", system_setting.TaskPublicAddress)

	require.NoError(t, UpdateOption("TaskPublicAddress", ""))
	assert.Empty(t, system_setting.TaskPublicAddress)
}
