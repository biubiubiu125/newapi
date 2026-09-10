package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupTaskPluginBindPermissionTest(t *testing.T) *gorm.DB {
	t.Helper()

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}, &model.Log{}))

	wasMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() {
		common.IsMasterNode = wasMaster
	})

	require.NoError(t, authz.Init(db))
	t.Cleanup(func() {
		require.NoError(t, authz.Init(db))
	})

	require.NoError(t, authz.SetUserPermissions(42, authz.PermissionsMap{
		authz.ResourceChannel: {
			authz.ActionSensitiveWrite: true,
		},
		authz.ResourceTaskPlugin: {
			authz.ActionBind: false,
		},
	}))
	require.False(t, authz.Can(42, common.RoleAdminUser, authz.TaskPluginBind))
	require.True(t, authz.Can(42, common.RoleAdminUser, authz.ChannelSensitiveWrite))

	return db
}

func TestAddChannelRejectsTaskPluginBindWithoutPermission(t *testing.T) {
	db := setupTaskPluginBindPermissionTest(t)
	taskPluginSetting := `{"task_plugin_key":"kling"}`

	request := AddChannelRequest{
		Mode: "single",
		Channel: &model.Channel{
			Name:    "task-plugin-channel",
			Type:    constant.ChannelTypeTaskPlugin,
			Key:     "secret-key",
			Models:  "gpt-4o",
			Group:   "default",
			Setting: &taskPluginSetting,
		},
	}
	body, err := common.Marshal(request)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 42)
	ctx.Set("role", common.RoleAdminUser)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	AddChannel(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":false`)

	var count int64
	require.NoError(t, db.Model(&model.Channel{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestUpdateChannelRejectsTaskPluginBindWithoutPermission(t *testing.T) {
	db := setupTaskPluginBindPermissionTest(t)
	require.NoError(t, db.Create(&model.Channel{
		Type:   constant.ChannelTypeOpenAI,
		Key:    "sk-old",
		Status: common.ChannelStatusEnabled,
		Name:   "openai-channel",
		Models: "gpt-4o",
		Group:  "default",
	}).Error)

	var channel model.Channel
	require.NoError(t, db.First(&channel).Error)
	taskPluginSetting := `{"task_plugin_key":"kling"}`
	request := PatchChannel{
		Channel: model.Channel{
			Id:      channel.Id,
			Type:    constant.ChannelTypeTaskPlugin,
			Setting: &taskPluginSetting,
		},
	}
	body, err := common.Marshal(request)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 42)
	ctx.Set("role", common.RoleAdminUser)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/channel/", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	UpdateChannel(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":false`)

	var updated model.Channel
	require.NoError(t, db.First(&updated, channel.Id).Error)
	assert.Equal(t, constant.ChannelTypeOpenAI, updated.Type)
	assert.Equal(t, "sk-old", updated.Key)
}

func TestCopyChannelRejectsTaskPluginBindWithoutPermission(t *testing.T) {
	db := setupTaskPluginBindPermissionTest(t)
	taskPluginSetting := `{"task_plugin_key":"kling"}`
	require.NoError(t, db.Create(&model.Channel{
		Type:    constant.ChannelTypeTaskPlugin,
		Key:     "secret-key",
		Status:  common.ChannelStatusEnabled,
		Name:    "task-plugin-channel",
		Models:  "gpt-4o",
		Group:   "default",
		Setting: &taskPluginSetting,
	}).Error)

	var origin model.Channel
	require.NoError(t, db.First(&origin).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 42)
	ctx.Set("role", common.RoleAdminUser)
	ctx.Params = gin.Params{{Key: "id", Value: "1"}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/copy/1", nil)

	CopyChannel(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":false`)

	var count int64
	require.NoError(t, db.Model(&model.Channel{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}
