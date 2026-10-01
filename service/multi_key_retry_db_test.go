package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNextUnusedEnabledKeySkipsDatabaseDisabledKeyWhenCacheIsStale(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	channel := &model.Channel{
		Name:   t.Name(),
		Type:   1,
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		Models: "retry-stale-cache",
		Group:  "default",
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
			MultiKeyMode: constant.MultiKeyModePolling,
		},
	}
	require.NoError(t, channel.Insert())
	model.InitChannelCache()
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", channel.Id).Update("channel_info", model.ChannelInfo{
		IsMultiKey:   true,
		MultiKeySize: 2,
		MultiKeyMode: constant.MultiKeyModePolling,
		MultiKeyStatusList: map[int]int{
			0: common.ChannelStatusAutoDisabled,
		},
	}).Error)

	cached, err := model.CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.NotContains(t, cached.ChannelInfo.MultiKeyStatusList, 0)

	key, ok := NextUnusedEnabledKey(cached, nil)
	require.True(t, ok)
	require.Equal(t, "key-b", key)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelIsMultiKey, true)
	require.False(t, RememberFailedMultiKey(ctx, channel.Id, "key-b"))
}
