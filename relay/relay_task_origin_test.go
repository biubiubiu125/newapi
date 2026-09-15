package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	plugindto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupOriginTaskDB(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}))
	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
	})
	return db
}

func TestResolveOriginTaskRejectsOtherAPIToken(t *testing.T) {
	db := setupOriginTaskDB(t)

	task := &model.Task{
		TaskID: "origin-video",
		UserId: 11,
		Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			TokenId:        80,
			UpstreamTaskID: "up-1",
		},
	}
	require.NoError(t, db.Create(task).Error)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/origin-video/remix", nil)
	ctx.Params = gin.Params{{Key: "video_id", Value: "origin-video"}}
	ctx.Set("id", 11)
	ctx.Set("token_id", 81)
	ctx.Set("role", common.RoleCommonUser)

	info := &relaycommon.RelayInfo{
		UserId:        11,
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	taskErr := ResolveOriginTask(ctx, info)
	require.NotNil(t, taskErr)
	require.Equal(t, "task_not_exist", taskErr.Code)
}

func TestResolveOriginTaskReusesOriginPrivateKeyOnSameChannel(t *testing.T) {
	db := setupOriginTaskDB(t)

	originChannel := &model.Channel{
		Type:   constant.ChannelTypeOpenAI,
		Name:   "origin-same-channel",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: 0,
			MultiKeyStatusList: map[int]int{
				1: common.ChannelStatusAutoDisabled,
			},
		},
	}
	require.NoError(t, db.Create(originChannel).Error)
	require.NoError(t, db.Create(&model.Task{
		TaskID:    "origin-same-key",
		UserId:    21,
		ChannelId: originChannel.Id,
		Status:    model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "up-same",
			Key:            "key-b",
		},
	}).Error)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/origin-same-key/remix", nil)
	ctx.Params = gin.Params{{Key: "video_id", Value: "origin-same-key"}}
	common.SetContextKey(ctx, constant.ContextKeyChannelId, originChannel.Id)
	common.SetContextKey(ctx, constant.ContextKeyChannelKey, "key-a")
	common.SetContextKey(ctx, constant.ContextKeyChannelType, originChannel.Type)

	info := &relaycommon.RelayInfo{
		UserId:        21,
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   originChannel.Id,
			ChannelType: originChannel.Type,
			ApiKey:      "key-a",
		},
	}

	taskErr := ResolveOriginTask(ctx, info)
	require.Nil(t, taskErr)
	require.NotNil(t, info.LockedChannel)
	require.Equal(t, originChannel.Id, info.LockedChannel.(*model.Channel).Id)
	require.Equal(t, "key-b", common.GetContextKeyString(ctx, constant.ContextKeyChannelKey))
	require.Equal(t, "key-b", info.ApiKey)
}

func TestResolveOriginTaskAppliesOriginChannelContextAndPrivateKey(t *testing.T) {
	db := setupOriginTaskDB(t)

	mapping := `{"sora-2":"sora-2-origin"}`
	org := "org-origin"
	baseURL := "https://origin.example"
	headerOverride := `{"X-Origin":"remix"}`
	originChannel := &model.Channel{
		Type:               constant.ChannelTypeOpenAI,
		Name:               "origin-other-channel",
		Key:                "key-a\nkey-b",
		Status:             common.ChannelStatusEnabled,
		ModelMapping:       &mapping,
		OpenAIOrganization: &org,
		BaseURL:            &baseURL,
		HeaderOverride:     &headerOverride,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: 0,
			MultiKeyStatusList: map[int]int{
				1: common.ChannelStatusAutoDisabled,
			},
		},
	}
	require.NoError(t, db.Create(originChannel).Error)
	require.NoError(t, db.Create(&model.Task{
		TaskID:    "origin-other-key",
		UserId:    22,
		ChannelId: originChannel.Id,
		Status:    model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "up-other",
			Key:            "key-b",
		},
	}).Error)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/origin-other-key/remix", nil)
	ctx.Params = gin.Params{{Key: "video_id", Value: "origin-other-key"}}
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 9999)
	common.SetContextKey(ctx, constant.ContextKeyChannelKey, "distributed-key")
	common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, "https://distributed.example")
	common.SetContextKey(ctx, constant.ContextKeyChannelModelMapping, `{"sora-2":"sora-2-distributed"}`)
	common.SetContextKey(ctx, constant.ContextKeyChannelOrganization, "org-distributed")
	common.SetContextKey(ctx, constant.ContextKeyChannelHeaderOverride, map[string]interface{}{"X-Distributed": "1"})

	info := &relaycommon.RelayInfo{
		UserId:          22,
		OriginModelName: "sora-2",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:      9999,
			ChannelType:    constant.ChannelTypeOpenAI,
			ChannelBaseUrl: "https://distributed.example",
			ApiKey:         "distributed-key",
			Organization:   "org-distributed",
		},
	}

	taskErr := ResolveOriginTask(ctx, info)
	require.Nil(t, taskErr)
	require.NotNil(t, info.LockedChannel)
	require.Equal(t, originChannel.Id, info.LockedChannel.(*model.Channel).Id)
	require.Equal(t, originChannel.Id, common.GetContextKeyInt(ctx, constant.ContextKeyChannelId))
	require.Equal(t, "key-b", common.GetContextKeyString(ctx, constant.ContextKeyChannelKey))
	require.Equal(t, "key-b", info.ApiKey)
	require.Equal(t, originChannel.Id, info.ChannelId)
	require.Equal(t, "https://origin.example", common.GetContextKeyString(ctx, constant.ContextKeyChannelBaseUrl))
	require.Equal(t, mapping, common.GetContextKeyString(ctx, constant.ContextKeyChannelModelMapping))
	require.Equal(t, org, common.GetContextKeyString(ctx, constant.ContextKeyChannelOrganization))
	headers, ok := common.GetContextKeyType[map[string]interface{}](ctx, constant.ContextKeyChannelHeaderOverride)
	require.True(t, ok)
	require.Equal(t, "remix", headers["X-Origin"])
}

func TestApplyChannelPinSetsOriginChannelContextAndPrivateKey(t *testing.T) {
	db := setupOriginTaskDB(t)

	mapping := `{"suno_music":"suno-origin"}`
	originChannel := &model.Channel{
		Type:         constant.ChannelTypeOpenAI,
		Name:         "pinned-origin-channel",
		Key:          "key-a\nkey-b",
		Status:       common.ChannelStatusEnabled,
		ModelMapping: &mapping,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: 0,
			MultiKeyStatusList: map[int]int{
				1: common.ChannelStatusAutoDisabled,
			},
		},
	}
	require.NoError(t, db.Create(originChannel).Error)
	originTask := &model.Task{
		TaskID:    "pinned-origin-task",
		UserId:    23,
		ChannelId: originChannel.Id,
		Status:    model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "up-pinned",
			Key:            "key-b",
		},
	}
	require.NoError(t, db.Create(originTask).Error)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/generations", nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 8888)
	common.SetContextKey(ctx, constant.ContextKeyChannelKey, "distributed-key")
	common.SetContextKey(ctx, constant.ContextKeyChannelModelMapping, `{"suno_music":"suno-distributed"}`)
	common.SetContextKey(ctx, constant.ContextKeyOriginTasks, []*model.Task{originTask})
	service.GetChannelConstraints(ctx).AddPin(plugindto.ChannelPin{
		ChannelId: originChannel.Id,
		Source:    plugindto.PinSourceOriginTask,
		Rank:      plugindto.PinRankOriginTask,
		RetryMode: plugindto.PinRetrySameChannel,
	})

	info := &relaycommon.RelayInfo{
		UserId:          23,
		OriginModelName: "suno_music",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: 8888,
			ApiKey:    "distributed-key",
		},
	}

	taskErr := ApplyChannelPin(ctx, info)
	require.Nil(t, taskErr)
	require.NotNil(t, info.LockedChannel)
	require.Equal(t, originChannel.Id, info.LockedChannel.(*model.Channel).Id)
	require.Equal(t, "key-b", common.GetContextKeyString(ctx, constant.ContextKeyChannelKey))
	require.Equal(t, "key-b", info.ApiKey)
	require.Equal(t, mapping, common.GetContextKeyString(ctx, constant.ContextKeyChannelModelMapping))
	require.Equal(t, originChannel.Id, common.GetContextKeyInt(ctx, constant.ContextKeyChannelId))
}
