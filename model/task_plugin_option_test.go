package model

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	_ "github.com/QuantumNous/new-api/plugins"
	"github.com/QuantumNous/new-api/setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func useTaskPluginOptionDB(t *testing.T) {
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
}

func registerGoogleOverridePlugin(t *testing.T) *pluginruntime.LoadedPlugin {
	t.Helper()
	const source = `
export const meta = {apiVersion:1,key:"google",name:"Google Override",version:"9.9.9-override",author:{name:"Test"},models:["google-model"],fetchMode:"per_task"};
export function buildSubmitRequest() { return {}; }
export function parseSubmitResponse() { return {}; }
export function buildQueryRequest() { return {}; }
export function parseTaskResult() { return {}; }
`
	plugin, err := pluginruntime.DefaultRegistry.Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	return plugin
}

func TestInitOptionMapPublishesTaskPluginRuntimeControls(t *testing.T) {
	useTaskPluginOptionDB(t)

	previousMap := common.OptionMap
	previousTaskPluginEnabled := constant.TaskPluginEnabled
	previousTaskPluginOverrideEnabled := constant.TaskPluginOverrideEnabled
	previousDisabled := append([]string(nil), pluginruntime.DefaultRegistry.Snapshot().DisabledFactory...)
	previousEnabled := pluginruntime.DefaultRegistry.Enabled()
	t.Cleanup(func() {
		pluginruntime.DefaultRegistry.Unregister("google")
		_ = pluginruntime.DefaultRegistry.SetDisabledFactoryKeys(previousDisabled)
		constant.TaskPluginEnabled = previousTaskPluginEnabled
		constant.TaskPluginOverrideEnabled = previousTaskPluginOverrideEnabled
		pluginruntime.DefaultRegistry.SetOverrideEnabled(previousTaskPluginOverrideEnabled)
		pluginruntime.DefaultRegistry.SetEnabled(previousEnabled)
		common.OptionMap = previousMap
	})

	constant.TaskPluginEnabled = true
	constant.TaskPluginOverrideEnabled = true
	pluginruntime.DefaultRegistry.SetEnabled(true)
	pluginruntime.DefaultRegistry.SetOverrideEnabled(true)
	require.NoError(t, pluginruntime.DefaultRegistry.SetDisabledFactoryKeys(nil))
	common.OptionMap = map[string]string{}
	require.NoError(t, pluginruntime.DefaultRegistry.Unregister("google"))
	factoryPlugin, ok := pluginruntime.DefaultRegistry.Get("google")
	require.True(t, ok)
	overridePlugin := registerGoogleOverridePlugin(t)

	pluginruntime.DefaultRegistry.SetEnabled(false)
	pluginruntime.DefaultRegistry.SetOverrideEnabled(false)
	require.NoError(t, pluginruntime.DefaultRegistry.SetDisabledFactoryKeys([]string{"google"}))

	InitOptionMap()

	assert.Equal(t, "true", common.OptionMap["TaskPluginEnabled"])
	assert.Equal(t, "true", common.OptionMap["TaskPluginOverrideEnabled"])
	assert.Equal(t, setting.TaskPluginMarketplaceSources2JsonString(), common.OptionMap[setting.TaskPluginMarketplaceSourcesKey])
	assert.Equal(t, "[]", common.OptionMap[setting.TaskPluginDisabledFactoryKeysKey])
	assert.True(t, pluginruntime.DefaultRegistry.Enabled())
	assert.Empty(t, pluginruntime.DefaultRegistry.Snapshot().DisabledFactory)

	plugin, ok := pluginruntime.DefaultRegistry.Get("google")
	require.True(t, ok)
	assert.NotEqual(t, factoryPlugin.Meta.Version, plugin.Meta.Version)
	assert.Equal(t, overridePlugin.Meta.Version, plugin.Meta.Version)
}

func TestUpdateOptionSyncsTaskPluginRuntimeControls(t *testing.T) {
	useTaskPluginOptionDB(t)

	previousMap := common.OptionMap
	previousTaskPluginEnabled := constant.TaskPluginEnabled
	previousTaskPluginOverrideEnabled := constant.TaskPluginOverrideEnabled
	previousDisabled := append([]string(nil), pluginruntime.DefaultRegistry.Snapshot().DisabledFactory...)
	previousEnabled := pluginruntime.DefaultRegistry.Enabled()
	t.Cleanup(func() {
		pluginruntime.DefaultRegistry.Unregister("google")
		_ = pluginruntime.DefaultRegistry.SetDisabledFactoryKeys(previousDisabled)
		constant.TaskPluginEnabled = previousTaskPluginEnabled
		constant.TaskPluginOverrideEnabled = previousTaskPluginOverrideEnabled
		pluginruntime.DefaultRegistry.SetOverrideEnabled(previousTaskPluginOverrideEnabled)
		pluginruntime.DefaultRegistry.SetEnabled(previousEnabled)
		common.OptionMap = previousMap
	})

	constant.TaskPluginEnabled = true
	constant.TaskPluginOverrideEnabled = true
	pluginruntime.DefaultRegistry.SetEnabled(true)
	pluginruntime.DefaultRegistry.SetOverrideEnabled(true)
	require.NoError(t, pluginruntime.DefaultRegistry.SetDisabledFactoryKeys(nil))
	common.OptionMap = map[string]string{}
	require.NoError(t, pluginruntime.DefaultRegistry.Unregister("google"))
	factoryPlugin, ok := pluginruntime.DefaultRegistry.Get("google")
	require.True(t, ok)
	overridePlugin := registerGoogleOverridePlugin(t)

	require.NoError(t, UpdateOption("TaskPluginEnabled", "false"))
	assert.False(t, pluginruntime.DefaultRegistry.Enabled())
	require.NoError(t, UpdateOption("TaskPluginEnabled", "true"))
	assert.True(t, pluginruntime.DefaultRegistry.Enabled())

	require.NoError(t, UpdateOption("TaskPluginOverrideEnabled", "false"))
	plugin, ok := pluginruntime.DefaultRegistry.Get("google")
	require.True(t, ok)
	assert.Equal(t, factoryPlugin.Meta.Version, plugin.Meta.Version)

	require.NoError(t, UpdateOption("TaskPluginOverrideEnabled", "true"))
	plugin, ok = pluginruntime.DefaultRegistry.Get("google")
	require.True(t, ok)
	assert.Equal(t, overridePlugin.Meta.Version, plugin.Meta.Version)

	require.NoError(t, UpdateOption(setting.TaskPluginDisabledFactoryKeysKey, `["google"]`))
	assert.Equal(t, []string{"google"}, pluginruntime.DefaultRegistry.Snapshot().DisabledFactory)

	require.NoError(t, pluginruntime.DefaultRegistry.Unregister("google"))
	_, ok = pluginruntime.DefaultRegistry.Get("google")
	assert.False(t, ok)
}

func TestUpdateOptionDoesNotPersistDisabledFactoryKeysWhenRuntimeRebuildFails(t *testing.T) {
	useTaskPluginOptionDB(t)
	previousMap := common.OptionMap
	previousDisabled := append([]string(nil), pluginruntime.DefaultRegistry.Snapshot().DisabledFactory...)
	t.Cleanup(func() {
		_ = pluginruntime.DefaultRegistry.SetGenerationPreparer(nil)
		_ = pluginruntime.DefaultRegistry.SetDisabledFactoryKeys(previousDisabled)
		common.OptionMap = previousMap
	})

	common.OptionMap = map[string]string{setting.TaskPluginDisabledFactoryKeysKey: "[]"}
	require.NoError(t, pluginruntime.DefaultRegistry.SetDisabledFactoryKeys(nil))
	var fail atomic.Bool
	require.NoError(t, pluginruntime.DefaultRegistry.SetGenerationPreparer(func(candidate, current *pluginruntime.RoutingGeneration) (pluginruntime.PreparedRoutingGeneration, error) {
		if fail.Load() {
			return pluginruntime.PreparedRoutingGeneration{}, fmt.Errorf("forced runtime rebuild failure")
		}
		return pluginruntime.PreparedRoutingGeneration{Generation: candidate}, nil
	}))
	fail.Store(true)

	err := UpdateOption(setting.TaskPluginDisabledFactoryKeysKey, `["google"]`)
	require.ErrorContains(t, err, "forced runtime rebuild failure")
	var stored Option
	require.ErrorIs(t, DB.First(&stored, "key = ?", setting.TaskPluginDisabledFactoryKeysKey).Error, gorm.ErrRecordNotFound)
	common.OptionMapRWMutex.RLock()
	assert.Equal(t, "[]", common.OptionMap[setting.TaskPluginDisabledFactoryKeysKey])
	common.OptionMapRWMutex.RUnlock()
	assert.Empty(t, pluginruntime.DefaultRegistry.Snapshot().DisabledFactory)
}
