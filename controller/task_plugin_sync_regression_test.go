package controller

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSyncTaskPluginsReuseClearsPreviousCompileError(t *testing.T) {
	const (
		key     = "sync-reuse-error-clear"
		version = "1.0.0"
		source  = `export const meta = {apiVersion:1,key:"sync-reuse-error-clear",name:"Sync Reuse Error Clear",version:"1.0.0",author:{name:"Test"},models:["sync-reuse-error-clear-model"],fetchMode:"per_task"};
export function buildSubmitRequest() { return {}; }
export function parseSubmitResponse() { return {}; }
export function buildQueryRequest() { return {}; }
export function parseTaskResult() { return {}; }`
	)

	database, err := gorm.Open(sqlite.Open("file:task-plugin-sync-reuse-error-clear?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.TaskPlugin{}))
	previousDB := model.DB
	model.DB = database
	t.Cleanup(func() {
		model.DB = previousDB
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})

	sum := sha256.Sum256([]byte(source))
	sourceHash := fmt.Sprintf("%x", sum[:])
	require.NoError(t, database.Create(&model.TaskPlugin{
		Key: key, APIVersion: 1, Version: version, Source: source,
		SourceHash: sourceHash, Enabled: true, Active: true,
	}).Error)
	_, err = jsplugin.DefaultRegistry.Register(source, jsplugin.Options{Key: key, Version: version})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = jsplugin.DefaultRegistry.Unregister(key)
	})

	taskPluginSyncState.Lock()
	previousHashes := taskPluginSyncState.hashes
	previousErrors := taskPluginSyncState.errors
	taskPluginSyncState.hashes = map[string]string{key: sourceHash}
	taskPluginSyncState.errors = map[string]string{key: "stale compile error"}
	taskPluginSyncState.Unlock()
	t.Cleanup(func() {
		taskPluginSyncState.Lock()
		taskPluginSyncState.hashes = previousHashes
		taskPluginSyncState.errors = previousErrors
		taskPluginSyncState.Unlock()
	})

	require.NoError(t, syncTaskPluginsOnceContext(t.Context()))

	taskPluginSyncState.Lock()
	_, exists := taskPluginSyncState.errors[key]
	taskPluginSyncState.Unlock()
	assert.False(t, exists)
}

func TestSyncTaskPluginsMarksPartialWhenDesiredPluginCannotCompile(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:task-plugin-sync-compile-failure?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.TaskPlugin{}))
	previousDB := model.DB
	model.DB = database
	t.Cleanup(func() {
		model.DB = previousDB
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})

	require.NoError(t, database.Create(&model.TaskPlugin{
		Key:        "sync-compile-failure",
		APIVersion: 1,
		Version:    "1.0.0",
		Source:     `export const meta = {`,
		SourceHash: "invalid",
		Enabled:    true,
		Active:     true,
	}).Error)

	err = syncTaskPluginsOnceContext(t.Context())
	require.NoError(t, err)

	taskPluginSyncState.Lock()
	rebuild := taskPluginSyncState.lastRebuild
	taskPluginSyncState.Unlock()
	assert.Equal(t, "partial", rebuild.Status)
	assert.Equal(t, 1, rebuild.PluginErrorCount)
}
