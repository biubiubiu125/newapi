package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDeleteTaskPluginVersionRetainsPersistedTaskVersion(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:task-plugin-delete-guard?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&Task{}, &TaskPlugin{}))

	previousDB := DB
	DB = database
	t.Cleanup(func() {
		DB = previousDB
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})

	plugin := &TaskPlugin{
		Key:        "delete-guard-plugin",
		APIVersion: 1,
		Version:    "1.0.0",
		Source:     "export default {}",
		SourceHash: "hash",
		Enabled:    true,
		Active:     true,
	}
	require.NoError(t, database.Create(plugin).Error)
	task := &Task{
		TaskID:   "task_delete_guard",
		Platform: "delete-guard-plugin",
		PrivateData: TaskPrivateData{
			Execution: &TaskExecutionSnapshot{
				TaskPlugin: &TaskPluginSnapshot{
					Key:     plugin.Key,
					Version: plugin.Version,
				},
			},
		},
	}
	require.NoError(t, database.Create(task).Error)

	_, err = DeleteTaskPluginVersion(plugin.Key, plugin.Version)
	require.ErrorIs(t, err, ErrTaskPluginVersionInUse)

	var remaining TaskPlugin
	require.NoError(t, database.Where("key = ? AND version = ?", plugin.Key, plugin.Version).First(&remaining).Error)
}
