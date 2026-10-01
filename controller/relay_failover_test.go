package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	appdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	apptypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetChannelSelectsWhenNoPreselectedChannel(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	weight := uint(100)
	priority := int64(0)
	channel := &model.Channel{
		Type:     constant.ChannelTypeOpenAI,
		Key:      "fallback-key",
		Status:   common.ChannelStatusEnabled,
		Name:     "fallback-channel",
		Weight:   &weight,
		Models:   "remix-model",
		Group:    "default",
		Priority: &priority,
	}
	require.NoError(t, db.Create(channel).Error)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/abc/remix", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "remix-model",
		TokenGroup:      "default",
	}
	selected, err := getChannel(ctx, relayInfo, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "remix-model",
		Retry:      common.GetPointer(0),
	})
	require.Nil(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, channel.Id, selected.Id)
}

func TestTaskSubmissionRotatesMultiKeyBeforeLowerPriority(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	previousMemoryCache := common.MemoryCacheEnabled
	previousRetryTimes := common.RetryTimes
	common.MemoryCacheEnabled = true
	common.RetryTimes = 1
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCache
		common.RetryTimes = previousRetryTimes
		model.InitChannelCache()
	})

	highPriority := int64(10)
	lowPriority := int64(0)
	weight := uint(100)
	high := &model.Channel{
		Type:     constant.ChannelTypeOpenAI,
		Key:      "key-a\nkey-b",
		Status:   common.ChannelStatusEnabled,
		Name:     "multi-key-high",
		Weight:   &weight,
		Models:   "multi-key-model",
		Group:    "default",
		Priority: &highPriority,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
			MultiKeyMode: constant.MultiKeyModePolling,
		},
	}
	low := &model.Channel{
		Type:     constant.ChannelTypeOpenAI,
		Key:      "low-key",
		Status:   common.ChannelStatusEnabled,
		Name:     "single-key-low",
		Weight:   &weight,
		Models:   "multi-key-model",
		Group:    "default",
		Priority: &lowPriority,
	}
	require.NoError(t, db.Create(high).Error)
	require.NoError(t, db.Create(low).Error)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader("{}"))
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyChannelId, high.Id)
	common.SetContextKey(ctx, constant.ContextKeyChannelType, high.Type)
	common.SetContextKey(ctx, constant.ContextKeyChannelName, high.Name)
	common.SetContextKey(ctx, constant.ContextKeyChannelIsMultiKey, true)
	common.SetContextKey(ctx, constant.ContextKeyChannelKey, "key-a")

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "multi-key-model",
		TokenGroup:      "default",
		UserGroup:       "default",
		UsingGroup:      "default",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}
	var keys []string
	_, taskErr := executeTaskSubmissionWith(ctx, relayInfo, func(c *gin.Context, _ *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		keys = append(keys, common.GetContextKeyString(c, constant.ContextKeyChannelKey))
		return nil, service.TaskErrorWrapper(errors.New("upstream"), "upstream_error", http.StatusInternalServerError)
	})
	require.NotNil(t, taskErr)
	// Both keys are tried, then the one cross-channel retry still reaches the
	// lower-priority channel. Key rotation must not spend RetryTimes.
	assert.Equal(t, []string{"key-a", "key-b", "low-key"}, keys)
}

func TestTaskSubmissionRotatesKeysWhenRetryBudgetIsZero(t *testing.T) {
	keys := runMultiKeySubmission(t, 0, nil, http.StatusInternalServerError, false)
	assert.Equal(t, []string{"key-a", "key-b"}, keys)
}

func TestTaskSubmissionRotatesKeysWhenChannelIsPinned(t *testing.T) {
	keys := runMultiKeySubmission(t, 1, func(ctx *gin.Context, highID int) {
		ctx.Set("specific_channel_id", highID)
	}, http.StatusInternalServerError, false)
	assert.Equal(t, []string{"key-a", "key-b"}, keys)
}

func TestTaskSubmissionRotatesKeysWhenAffinitySkipsChannelSwitch(t *testing.T) {
	keys := runMultiKeySubmission(t, 1, func(ctx *gin.Context, _ int) {
		ctx.Set("channel_affinity_skip_retry_on_failure", true)
	}, http.StatusInternalServerError, false)
	assert.Equal(t, []string{"key-a", "key-b"}, keys)
}

func TestTaskSubmissionDoesNotRotateKeysOnSingleAttemptPin(t *testing.T) {
	keys := runMultiKeySubmission(t, 1, func(ctx *gin.Context, highID int) {
		service.GetChannelConstraints(ctx).AddPin(appdto.ChannelPin{
			ChannelId: highID,
			Source:    appdto.PinSourceToken,
			Rank:      appdto.PinRankToken,
			RetryMode: appdto.PinRetrySingleAttempt,
		})
	}, http.StatusInternalServerError, false)
	assert.Equal(t, []string{"key-a"}, keys)
}

func TestTaskSubmissionDoesNotRotateKeysOnLocalRejection(t *testing.T) {
	keys := runMultiKeySubmission(t, 1, nil, http.StatusBadRequest, true)
	assert.Equal(t, []string{"key-a"}, keys)
}

func runMultiKeySubmission(t *testing.T, retryTimes int, configure func(*gin.Context, int), statusCode int, local bool) []string {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	previousMemoryCache := common.MemoryCacheEnabled
	previousRetryTimes := common.RetryTimes
	common.MemoryCacheEnabled = true
	common.RetryTimes = retryTimes
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCache
		common.RetryTimes = previousRetryTimes
		model.InitChannelCache()
	})

	highPriority := int64(10)
	lowPriority := int64(0)
	weight := uint(100)
	high := &model.Channel{
		Type:     constant.ChannelTypeOpenAI,
		Key:      "key-a\nkey-b",
		Status:   common.ChannelStatusEnabled,
		Name:     "multi-key-rotation",
		Weight:   &weight,
		Models:   "multi-key-rotation-model",
		Group:    "default",
		Priority: &highPriority,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
			MultiKeyMode: constant.MultiKeyModePolling,
		},
	}
	low := &model.Channel{
		Type:     constant.ChannelTypeOpenAI,
		Key:      "low-key",
		Status:   common.ChannelStatusEnabled,
		Name:     "single-key-rotation-peer",
		Weight:   &weight,
		Models:   "multi-key-rotation-model",
		Group:    "default",
		Priority: &lowPriority,
	}
	require.NoError(t, db.Create(high).Error)
	require.NoError(t, db.Create(low).Error)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader("{}"))
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyChannelId, high.Id)
	common.SetContextKey(ctx, constant.ContextKeyChannelType, high.Type)
	common.SetContextKey(ctx, constant.ContextKeyChannelName, high.Name)
	common.SetContextKey(ctx, constant.ContextKeyChannelIsMultiKey, true)
	common.SetContextKey(ctx, constant.ContextKeyChannelKey, "key-a")
	if configure != nil {
		configure(ctx, high.Id)
	}

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "multi-key-rotation-model",
		TokenGroup:      "default",
		UserGroup:       "default",
		UsingGroup:      "default",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}
	var keys []string
	_, taskErr := executeTaskSubmissionWith(ctx, relayInfo, func(c *gin.Context, _ *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		keys = append(keys, common.GetContextKeyString(c, constant.ContextKeyChannelKey))
		if local {
			return nil, service.TaskErrorWrapperLocal(errors.New("bad request"), "invalid_request", statusCode)
		}
		return nil, service.TaskErrorWrapper(errors.New("upstream"), "upstream_error", statusCode)
	})
	require.NotNil(t, taskErr)
	return keys
}

func TestTaskSubmissionReachesSamePriorityPeerAfterMultiKeyCap(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	previousMemoryCache := common.MemoryCacheEnabled
	previousRetryTimes := common.RetryTimes
	common.MemoryCacheEnabled = true
	common.RetryTimes = 1
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCache
		common.RetryTimes = previousRetryTimes
		model.InitChannelCache()
	})

	priority := int64(10)
	weight := uint(100)
	multi := &model.Channel{
		Type:     constant.ChannelTypeOpenAI,
		Key:      "k1\nk2\nk3\nk4",
		Status:   common.ChannelStatusEnabled,
		Name:     "multi-key-peer",
		Weight:   &weight,
		Models:   "multi-key-peer-model",
		Group:    "default",
		Priority: &priority,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 4,
			MultiKeyMode: constant.MultiKeyModePolling,
		},
	}
	peer := &model.Channel{
		Type:     constant.ChannelTypeOpenAI,
		Key:      "peer-key",
		Status:   common.ChannelStatusEnabled,
		Name:     "same-priority-peer",
		Weight:   &weight,
		Models:   "multi-key-peer-model",
		Group:    "default",
		Priority: &priority,
	}
	require.NoError(t, db.Create(multi).Error)
	require.NoError(t, db.Create(peer).Error)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader("{}"))
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyChannelId, multi.Id)
	common.SetContextKey(ctx, constant.ContextKeyChannelType, multi.Type)
	common.SetContextKey(ctx, constant.ContextKeyChannelName, multi.Name)
	common.SetContextKey(ctx, constant.ContextKeyChannelIsMultiKey, true)
	common.SetContextKey(ctx, constant.ContextKeyChannelKey, "k1")

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "multi-key-peer-model",
		TokenGroup:      "default",
		UserGroup:       "default",
		UsingGroup:      "default",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}
	var keys []string
	_, taskErr := executeTaskSubmissionWith(ctx, relayInfo, func(c *gin.Context, _ *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		keys = append(keys, common.GetContextKeyString(c, constant.ContextKeyChannelKey))
		return nil, service.TaskErrorWrapper(errors.New("upstream"), "upstream_error", http.StatusInternalServerError)
	})
	require.NotNil(t, taskErr)
	assert.Equal(t, []string{"k1", "k2", "k3", "peer-key"}, keys)
	assert.NotContains(t, keys, "k4")
}

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

	retryable := apptypes.NewOpenAIError(errors.New("unauthorized"), apptypes.ErrorCodeBadResponseStatusCode, http.StatusUnauthorized)
	assert.True(t, shouldRetry(ctx, retryable, 1))
	assert.False(t, shouldRetry(ctx, retryable, 0))

	timeout := apptypes.NewOpenAIError(errors.New("timeout"), apptypes.ErrorCodeBadResponseStatusCode, http.StatusRequestTimeout)
	assert.False(t, shouldRetry(ctx, timeout, 1))

	serverErr := apptypes.NewOpenAIError(errors.New("server error"), apptypes.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError)
	assert.True(t, shouldRetry(ctx, serverErr, 1))
}

func TestShouldRetryRelayFailureHonorsChannelAffinitySkip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("channel_affinity_skip_retry_on_failure", true)

	retryable := apptypes.NewOpenAIError(errors.New("unauthorized"), apptypes.ErrorCodeBadResponseStatusCode, http.StatusUnauthorized)
	assert.False(t, shouldRetry(ctx, retryable, 1))
}

func TestTaskPluginSubmissionRetryKeepsChannelIdentityFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	service.AppendTaskPluginIdentityFilter(ctx, "google")

	retryParam := newTaskPluginRetryParam(ctx, &relaycommon.RelayInfo{
		TokenGroup:      "default",
		OriginModelName: "veo",
	})

	require.NotNil(t, retryParam.ChannelFilter)
	pluginChannel := &model.Channel{Type: constant.ChannelTypeTaskPlugin}
	pluginChannel.SetSetting(dto.ChannelSettings{TaskPluginKey: "google"})
	otherPluginChannel := &model.Channel{Type: constant.ChannelTypeTaskPlugin}
	otherPluginChannel.SetSetting(dto.ChannelSettings{TaskPluginKey: "other"})
	require.True(t, retryParam.ChannelFilter(pluginChannel))
	require.False(t, retryParam.ChannelFilter(otherPluginChannel))
}

func TestGetChannelRebindsTaskPluginEndpointOnRetry(t *testing.T) {
	setupModelListControllerTestDB(t)
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousMemoryCacheEnabled
	})

	channel := &model.Channel{
		Type:   1002,
		Key:    "selected-key",
		Status: common.ChannelStatusEnabled,
		Name:   "selected-task-plugin-channel",
		Models: "shared-task-model",
		Group:  "default",
	}
	require.NoError(t, channel.Insert())

	first := &pluginruntime.LoadedPlugin{
		Meta: pluginruntime.Meta{Key: "first-provider", ChannelTypes: []int{1001}},
	}
	second := &pluginruntime.LoadedPlugin{
		Meta: pluginruntime.Meta{Key: "second-provider", ChannelTypes: []int{1002}},
	}
	generation := &pluginruntime.RoutingGeneration{Number: 7}

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	ctx.Set(pluginruntime.ContextKeyPinnedEndpoint, pluginruntime.PinnedEndpoint{
		Generation: generation,
		Plugin:     first,
		Candidates: []pluginruntime.ProtocolBinding{
			{Plugin: first},
			{Plugin: second},
		},
	})
	ctx.Set(pluginruntime.ContextKeyPinnedPlugin, pluginruntime.PinnedPlugin{
		Generation: generation,
		Plugin:     first,
	})

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "shared-task-model",
		TokenGroup:      "default",
		ChannelMeta:     &relaycommon.ChannelMeta{},
	}
	channel, taskErr := getChannel(ctx, relayInfo, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "shared-task-model",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, taskErr)
	require.NotNil(t, channel)
	assert.Equal(t, 1002, channel.Type)

	pinnedValue, exists := ctx.Get(pluginruntime.ContextKeyPinnedPlugin)
	require.True(t, exists)
	pinned, ok := pinnedValue.(pluginruntime.PinnedPlugin)
	require.True(t, ok)
	require.NotNil(t, pinned.Plugin)
	assert.Equal(t, second.Meta.Key, pinned.Plugin.Meta.Key)
}

func TestShouldRetryRelayFailureSkipsLocalAndSpecificChannelErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	localErr := apptypes.NewError(errors.New("invalid request"), apptypes.ErrorCodeInvalidRequest, apptypes.ErrOptionWithSkipRetry())
	assert.False(t, shouldRetry(ctx, localErr, 0))

	ctx.Set("specific_channel_id", 123)
	retryable := apptypes.NewOpenAIError(errors.New("server error"), apptypes.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError)
	assert.False(t, shouldRetry(ctx, retryable, 0))
	assert.True(t, sameChannelKeyRotationAllowed(ctx, retryable))

	service.GetChannelConstraints(ctx).AddPin(appdto.ChannelPin{
		ChannelId: 123,
		Source:    appdto.PinSourceToken,
		Rank:      appdto.PinRankToken,
		RetryMode: appdto.PinRetrySingleAttempt,
	})
	assert.False(t, sameChannelKeyRotationAllowed(ctx, retryable))
	assert.False(t, sameChannelKeyRotationAllowed(ctx, localErr))
}

func TestShouldRetryTaskRelayFailureRetriesUpstreamStatusErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	assert.True(t, shouldRetryTaskRelay(ctx, 1, &dto.TaskError{StatusCode: http.StatusUnauthorized}, 1))
	assert.False(t, shouldRetryTaskRelay(ctx, 1, &dto.TaskError{StatusCode: http.StatusUnauthorized}, 0))
	assert.True(t, shouldRetryTaskRelay(ctx, 1, &dto.TaskError{StatusCode: http.StatusInternalServerError}, 1))
	assert.False(t, shouldRetryTaskRelay(ctx, 1, &dto.TaskError{StatusCode: http.StatusRequestTimeout}, 1))
}

func TestShouldRetryTaskRelayFailureHonorsChannelAffinitySkip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("channel_affinity_skip_retry_on_failure", true)
	assert.False(t, shouldRetryTaskRelay(ctx, 1, &dto.TaskError{StatusCode: http.StatusInternalServerError}, 1))
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

func TestProcessChannelErrorDisablesOnInsufficientBalanceWhenRequested(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))

	channel := &model.Channel{
		Type:  constant.ChannelTypeOpenAI,
		Key:   "test-key",
		Name:  "relay-disable-channel",
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
	processChannelError(ctx, *apptypes.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, channel.Key, true), err, true)

	var reloaded model.Channel
	require.NoError(t, db.First(&reloaded, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusAutoDisabled, reloaded.Status)
}

func TestResolveOriginTaskFailsWhenOriginChannelHasNoAvailableKey(t *testing.T) {
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
	require.NotNil(t, taskErr)
	assert.Equal(t, "setup_locked_channel_failed", taskErr.Code)
	assert.Nil(t, info.LockedChannel)
}

func TestResolveOriginTaskFailsWhenOriginChannelDisabled(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	originChannel := &model.Channel{
		Type:   constant.ChannelTypeOpenAI,
		Name:   "origin-disabled-channel",
		Key:    "origin-key",
		Group:  "default",
		Models: "suno_music",
		Status: common.ChannelStatusManuallyDisabled,
	}
	require.NoError(t, db.Create(originChannel).Error)
	require.NoError(t, db.Create(&model.Task{
		TaskID:    "origin-disabled-task",
		UserId:    12345,
		ChannelId: originChannel.Id,
	}).Error)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/origin-disabled-task/remix", nil)
	ctx.Params = gin.Params{{Key: "video_id", Value: "origin-disabled-task"}}

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
	require.NotNil(t, taskErr)
	assert.Equal(t, "origin_task_channel_disabled", taskErr.Code)
	assert.Nil(t, info.LockedChannel)
}

func TestResolveOriginTaskUsesUpstreamTaskIDForRemix(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	originChannel := &model.Channel{
		Type:   constant.ChannelTypeOpenAI,
		Name:   "origin-remix-channel",
		Key:    "origin-key",
		Group:  "default",
		Models: "sora-2",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, db.Create(originChannel).Error)
	require.NoError(t, db.Create(&model.Task{
		TaskID:    "task_public_remix",
		UserId:    12345,
		ChannelId: originChannel.Id,
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "video_upstream_abc",
		},
	}).Error)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/task_public_remix/remix", nil)
	ctx.Params = gin.Params{{Key: "video_id", Value: "task_public_remix"}}

	info := &relaycommon.RelayInfo{
		UserId:        12345,
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}

	taskErr := relay.ResolveOriginTask(ctx, info)
	require.Nil(t, taskErr)
	assert.Equal(t, "video_upstream_abc", info.OriginTaskID)
}

func TestRelayTaskKeepsLockedChannelAfterRetryableFailure(t *testing.T) {
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
	assert.Equal(t, originChannel.Id, selectedChannelIDs[1])
	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestRelayTaskRemixFirstAttemptReusesOriginPrivateKey(t *testing.T) {
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
	mapping := `{"suno_music":"suno-origin"}`
	originChannel := &model.Channel{
		Type:         constant.ChannelTypeOpenAI,
		Name:         "origin-multikey-channel",
		Key:          "key-a\nkey-b",
		Group:        "default",
		Models:       modelName,
		Status:       common.ChannelStatusEnabled,
		Weight:       &weight,
		Priority:     &highPriority,
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
		TaskID:    "origin-task-key",
		UserId:    24681,
		ChannelId: originChannel.Id,
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "video_upstream_key",
			Key:            "key-b",
		},
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

	var firstKey string
	var firstChannelID int
	var firstMapping string
	relayTaskSubmitFunc = func(c *gin.Context, info *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		info.InitChannelMeta(c)
		if firstKey == "" {
			firstKey = info.ApiKey
			firstChannelID = info.ChannelId
			firstMapping = common.GetContextKeyString(c, constant.ContextKeyChannelModelMapping)
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
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/origin-task-key/remix", strings.NewReader(`{"model":"suno_music","prompt":"test"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = gin.Params{{Key: "video_id", Value: "origin-task-key"}}
	ctx.Set("platform", string(constant.TaskPlatformSuno))
	ctx.Set("token_name", "relay-task-token")
	common.SetContextKey(ctx, constant.ContextKeyUserId, 24681)
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyOriginalModel, modelName)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, fallbackChannel.Id)
	common.SetContextKey(ctx, constant.ContextKeyChannelType, fallbackChannel.Type)
	common.SetContextKey(ctx, constant.ContextKeyChannelName, fallbackChannel.Name)
	common.SetContextKey(ctx, constant.ContextKeyChannelKey, fallbackChannel.Key)
	common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, fallbackChannel.GetBaseURL())
	common.SetContextKey(ctx, constant.ContextKeyChannelModelMapping, `{"suno_music":"suno-distributed"}`)

	RelayTask(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, originChannel.Id, firstChannelID)
	assert.Equal(t, "key-b", firstKey)
	assert.Equal(t, mapping, firstMapping)
}

func TestRelayTaskRetryExcludesFailedSamePriorityChannel(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	weightHigh := uint(1000)
	weightLow := uint(1)
	priority := int64(0)
	modelName := "suno_music"
	failingChannel := &model.Channel{
		Type:     constant.ChannelTypeOpenAI,
		Name:     "failing-task-channel",
		Key:      "failing-key",
		Group:    "default",
		Models:   modelName,
		Status:   common.ChannelStatusEnabled,
		Weight:   &weightHigh,
		Priority: &priority,
	}
	fallbackChannel := &model.Channel{
		Type:     constant.ChannelTypeOpenAI,
		Name:     "fallback-same-priority",
		Key:      "fallback-key",
		Group:    "default",
		Models:   modelName,
		Status:   common.ChannelStatusEnabled,
		Weight:   &weightLow,
		Priority: &priority,
	}
	require.NoError(t, db.Create(failingChannel).Error)
	require.NoError(t, db.Create(fallbackChannel).Error)
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
		if info.ChannelMeta.ChannelId == failingChannel.Id {
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
	settleBillingFunc = func(*gin.Context, *relaycommon.RelayInfo, int) error { return nil }
	logTaskConsumptionFunc = func(*gin.Context, *relaycommon.RelayInfo) error { return nil }

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", strings.NewReader(`{"model":"suno_music","prompt":"test"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("platform", string(constant.TaskPlatformSuno))
	ctx.Set("token_name", "relay-task-token")
	common.SetContextKey(ctx, constant.ContextKeyUserId, 24681)
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyOriginalModel, modelName)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, failingChannel.Id)
	common.SetContextKey(ctx, constant.ContextKeyChannelType, failingChannel.Type)
	common.SetContextKey(ctx, constant.ContextKeyChannelName, failingChannel.Name)
	common.SetContextKey(ctx, constant.ContextKeyChannelKey, failingChannel.Key)
	common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, failingChannel.GetBaseURL())

	RelayTask(ctx)

	require.GreaterOrEqual(t, len(selectedChannelIDs), 2)
	require.Equal(t, failingChannel.Id, selectedChannelIDs[0])
	require.Equal(t, fallbackChannel.Id, selectedChannelIDs[1])
	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestNextUnusedEnabledKeyDoesNotRaceStatusUpdates(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Ability{}))
	originalCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalCache
		model.InitChannelCache()
	})
	channel := &model.Channel{
		Name: t.Name(), Type: constant.ChannelTypeOpenAI, Key: "key-a\nkey-b", Status: common.ChannelStatusEnabled,
		Models: "race-model", Group: "default",
		ChannelInfo: model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2, MultiKeyMode: constant.MultiKeyModePolling},
	}
	require.NoError(t, channel.Insert())
	model.InitChannelCache()
	cached, err := model.CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.NotNil(t, cached)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			nextUnusedEnabledKey(cached, map[string]struct{}{"key-a": {}})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			model.UpdateChannelStatus(channel.Id, "key-b", common.ChannelStatusAutoDisabled, "race")
			model.UpdateChannelStatus(channel.Id, "key-b", common.ChannelStatusEnabled, "")
		}
	}()
	wg.Wait()
}
