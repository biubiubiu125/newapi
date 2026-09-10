package controller

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSetTaskPluginStatusDoesNotMutateFactoryOptionBeforeDatabaseCommit(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:task-plugin-status-option-rollback?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.Channel{}, &model.Task{}, &model.TaskPlugin{}, &model.Option{}))
	database.Callback().Create().Before("gorm:create").Register(
		"test_fail_task_plugin_option_create",
		func(tx *gorm.DB) {
			if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "options" {
				tx.AddError(errors.New("forced option persistence failure"))
			}
		},
	)

	previousDB := model.DB
	previousOptionMap := common.OptionMap
	model.DB = database
	common.OptionMap = map[string]string{
		setting.TaskPluginDisabledFactoryKeysKey: "[]",
	}
	t.Cleanup(func() {
		model.DB = previousDB
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})
	channelSetting := `{"task_plugin_key":"google"}`
	require.NoError(t, database.Create(&model.Channel{
		Id: 9912, Type: constant.ChannelTypeTaskPlugin, Key: "channel-key", Name: "google-channel",
		Status: common.ChannelStatusEnabled, Setting: &channelSetting,
	}).Error)
	channels, inFlight, err := model.GetTaskPluginUsage("google")
	require.NoError(t, err)
	require.Empty(t, inFlight)
	require.Len(t, channels, 1)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Params = gin.Params{{Key: "key", Value: "google"}}
	context.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/task-plugins/google/status?cascade=true",
		bytes.NewBufferString(`{"enabled":false}`),
	)
	context.Request.Header.Set("Content-Type", "application/json")

	SetTaskPluginStatus(context)

	common.OptionMapRWMutex.RLock()
	require.Equal(t, "[]", common.OptionMap[setting.TaskPluginDisabledFactoryKeysKey])
	common.OptionMapRWMutex.RUnlock()

	var channel model.Channel
	require.NoError(t, database.First(&channel, 9912).Error)
	require.Equal(t, common.ChannelStatusEnabled, channel.Status)
}

func TestCascadeTaskPluginChannelsReportsUpdateFailures(t *testing.T) {
	previous := updateTaskPluginChannelStatus
	t.Cleanup(func() { updateTaskPluginChannelStatus = previous })

	updateTaskPluginChannelStatus = func(id int, _ string, _ int, _ string) bool {
		return id == 1
	}
	disabled, failed := cascadeTaskPluginChannels([]model.TaskPluginChannelRef{
		{Id: 1},
		{Id: 2},
	}, true)

	require.Equal(t, 1, disabled)
	require.Equal(t, []int{2}, failed)
}
