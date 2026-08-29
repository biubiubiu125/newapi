package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	apptypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetChannelReturnsSelectedChannelWhenKeySetupFails(t *testing.T) {
	db := setupModelListControllerTestDB(t)

	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
	})

	badPriority := int64(10)
	goodPriority := int64(0)
	weight := uint(100)

	badChannel := &model.Channel{
		Type:        constant.ChannelTypeOpenAI,
		Key:         "",
		Status:      common.ChannelStatusEnabled,
		Name:        "bad-channel",
		Weight:      &weight,
		Models:      "failover-model",
		Group:       "default",
		Priority:    &badPriority,
		ChannelInfo: model.ChannelInfo{IsMultiKey: true, MultiKeySize: 1},
	}
	goodChannel := &model.Channel{
		Type:     constant.ChannelTypeOpenAI,
		Key:      "good-key",
		Status:   common.ChannelStatusEnabled,
		Name:     "good-channel",
		Weight:   &weight,
		Models:   "failover-model",
		Group:    "default",
		Priority: &goodPriority,
	}

	require.NoError(t, db.Create(badChannel).Error)
	require.NoError(t, db.Create(goodChannel).Error)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "failover-model",
		TokenGroup:      "default",
		ChannelMeta:     &relaycommon.ChannelMeta{},
	}
	retryParam := &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "failover-model",
		Retry:      common.GetPointer(0),
	}

	channel, err := getChannel(ctx, relayInfo, retryParam)

	require.Error(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, badChannel.Id, channel.Id)
	assert.Equal(t, apptypes.ErrorCodeChannelNoAvailableKey, err.GetErrorCode())
}

func TestShouldRetryRelayFailureRetriesUpstreamStatusErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("channel_affinity_skip_retry_on_failure", true)

	tests := []struct {
		name string
		err  *apptypes.NewAPIError
		want bool
	}{
		{
			name: "401",
			err:  apptypes.NewOpenAIError(errors.New("unauthorized"), apptypes.ErrorCodeBadResponseStatusCode, http.StatusUnauthorized),
			want: true,
		},
		{
			name: "408",
			err:  apptypes.NewOpenAIError(errors.New("timeout"), apptypes.ErrorCodeBadResponseStatusCode, http.StatusRequestTimeout),
			want: true,
		},
		{
			name: "500",
			err:  apptypes.NewOpenAIError(errors.New("server error"), apptypes.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError),
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, shouldRetry(ctx, test.err, 0))
		})
	}
}

func TestShouldRetryRelayFailureSkipsLocalAndSpecificChannelErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	localErr := apptypes.NewError(errors.New("invalid request"), apptypes.ErrorCodeInvalidRequest, apptypes.ErrOptionWithSkipRetry())
	assert.False(t, shouldRetry(ctx, localErr, 0))

	ctx.Set("specific_channel_id", 123)
	retryable := apptypes.NewOpenAIError(errors.New("server error"), apptypes.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError)
	assert.False(t, shouldRetry(ctx, retryable, 0))
}

func TestShouldRetryTaskRelayFailureRetriesUpstreamStatusErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("channel_affinity_skip_retry_on_failure", true)

	tests := []struct {
		name string
		err  *dto.TaskError
		want bool
	}{
		{
			name: "401",
			err:  &dto.TaskError{StatusCode: http.StatusUnauthorized},
			want: true,
		},
		{
			name: "500",
			err:  &dto.TaskError{StatusCode: http.StatusInternalServerError},
			want: true,
		},
		{
			name: "timeout",
			err:  &dto.TaskError{StatusCode: http.StatusRequestTimeout},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, shouldRetryTaskRelay(ctx, 1, test.err, 0))
		})
	}
}

func TestShouldRetryTaskRelayFailureSkipsLocalAndSpecificChannelErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	localErr := &dto.TaskError{StatusCode: http.StatusBadRequest, LocalError: true}
	assert.False(t, shouldRetryTaskRelay(ctx, 1, localErr, 0))

	ctx.Set("specific_channel_id", 123)
	retryable := &dto.TaskError{StatusCode: http.StatusInternalServerError}
	assert.False(t, shouldRetryTaskRelay(ctx, 1, retryable, 0))
}

func TestProcessChannelErrorCanSkipDisableWhenRequested(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))

	channel := &model.Channel{
		Type:  constant.ChannelTypeOpenAI,
		Key:   "test-key",
		Name:  "relay-failover-channel",
		Group: "default",
	}
	require.NoError(t, db.Create(channel).Error)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set("channel_name", channel.Name)
	ctx.Set("channel_type", channel.Type)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, channel.Id)
	common.SetContextKey(ctx, constant.ContextKeyChannelKey, channel.Key)

	err := apptypes.NewOpenAIError(errors.New("余额不足"), apptypes.ErrorCodeBadResponseStatusCode, http.StatusForbidden)
	processChannelError(ctx, *apptypes.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, channel.Key, true), err, false)

	var reloaded model.Channel
	require.NoError(t, db.First(&reloaded, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, reloaded.Status)
}

func TestResolveOriginTaskFallsBackWhenOriginChannelHasNoAvailableKey(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	originChannel := &model.Channel{
		Type:   constant.ChannelTypeOpenAI,
		Name:   "origin-task-channel",
		Key:    "",
		Group:  "default",
		Models: "suno_music",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 1,
		},
	}
	require.NoError(t, db.Create(originChannel).Error)
	require.NoError(t, db.Create(&model.Task{
		TaskID:    "origin-task",
		UserId:    12345,
		ChannelId: originChannel.Id,
	}).Error)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/origin-task/remix", nil)
	ctx.Params = gin.Params{{Key: "video_id", Value: "origin-task"}}

	info := &relaycommon.RelayInfo{
		UserId:          12345,
		OriginModelName: "suno_music",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   9999,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}

	taskErr := relay.ResolveOriginTask(ctx, info)
	require.Nil(t, taskErr)
	assert.Nil(t, info.LockedChannel)
}

func TestRelayTaskFallsBackFromLockedChannelAfterFirstFailure(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
	})

	weight := uint(100)
	highPriority := int64(10)
	lowPriority := int64(0)
	modelName := "suno_music"
	originChannel := &model.Channel{
		Type:     constant.ChannelTypeOpenAI,
		Name:     "origin-task-channel",
		Key:      "origin-key",
		Group:    "default",
		Models:   modelName,
		Status:   common.ChannelStatusEnabled,
		Weight:   &weight,
		Priority: &highPriority,
	}
	fallbackChannel := &model.Channel{
		Type:     constant.ChannelTypeOpenAI,
		Name:     "fallback-task-channel",
		Key:      "fallback-key",
		Group:    "default",
		Models:   modelName,
		Status:   common.ChannelStatusEnabled,
		Weight:   &weight,
		Priority: &lowPriority,
	}
	require.NoError(t, db.Create(originChannel).Error)
	require.NoError(t, db.Create(fallbackChannel).Error)
	require.NoError(t, db.Create(&model.Task{
		TaskID:    "origin-task",
		UserId:    24680,
		ChannelId: originChannel.Id,
	}).Error)
	model.InitChannelCache()

	oldSubmit := relayTaskSubmitFunc
	oldSettle := settleBillingFunc
	oldLog := logTaskConsumptionFunc
	t.Cleanup(func() {
		relayTaskSubmitFunc = oldSubmit
		settleBillingFunc = oldSettle
		logTaskConsumptionFunc = oldLog
	})

	var selectedChannelIDs []int
	relayTaskSubmitFunc = func(c *gin.Context, info *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		info.InitChannelMeta(c)
		selectedChannelIDs = append(selectedChannelIDs, info.ChannelMeta.ChannelId)
		if len(selectedChannelIDs) == 1 {
			return nil, &dto.TaskError{
				Message:    "upstream task failed",
				StatusCode: http.StatusInternalServerError,
				Error:      errors.New("upstream task failed"),
			}
		}
		return &relay.TaskSubmitResult{
			UpstreamTaskID: "upstream-task",
			TaskData:       []byte(`{"id":"upstream-task"}`),
			Platform:       constant.TaskPlatformSuno,
			Quota:          1,
		}, nil
	}
	settleBillingFunc = func(c *gin.Context, relayInfo *relaycommon.RelayInfo, quota int) error { return nil }
	logTaskConsumptionFunc = func(c *gin.Context, relayInfo *relaycommon.RelayInfo) error { return nil }

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/origin-task/remix", strings.NewReader(`{"model":"suno_music","prompt":"test"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = gin.Params{{Key: "video_id", Value: "origin-task"}}
	ctx.Set("platform", string(constant.TaskPlatformSuno))
	ctx.Set("token_name", "relay-task-token")
	common.SetContextKey(ctx, constant.ContextKeyUserId, 24680)
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyOriginalModel, modelName)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, fallbackChannel.Id)
	common.SetContextKey(ctx, constant.ContextKeyChannelType, fallbackChannel.Type)
	common.SetContextKey(ctx, constant.ContextKeyChannelName, fallbackChannel.Name)
	common.SetContextKey(ctx, constant.ContextKeyChannelKey, fallbackChannel.Key)
	common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, fallbackChannel.GetBaseURL())

	RelayTask(ctx)

	require.Len(t, selectedChannelIDs, 2)
	assert.Equal(t, originChannel.Id, selectedChannelIDs[0])
	assert.Equal(t, fallbackChannel.Id, selectedChannelIDs[1])
	assert.Equal(t, http.StatusOK, recorder.Code)
}
