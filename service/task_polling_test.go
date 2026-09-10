package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type taskPollingFetchAdaptor struct {
	mu           sync.Mutex
	taskIDs      []string
	fetched      chan string
	blockTaskID  string
	blockStarted chan struct{}
	releaseBlock chan struct{}
	blockOnce    sync.Once
}

type channelAwarePollingAdaptor struct {
	mu       sync.Mutex
	baseURLs []string
}

type sunoResponsePollingAdaptor struct {
	mu           sync.Mutex
	responseBody []byte
	response     dto.TaskResponse[[]dto.SunoDataResponse]
	statusCode   int
	fetchErr     error
	keys         []string
	bodies       []map[string]any
}

type pollOutcomeAdaptor struct {
	statusCode   int
	responseBody []byte
	fetchErr     error
	parseResult  *relaycommon.TaskInfo
	parseErr     error
	adjustReturn int
}

type pluginPollingFallbackAdaptor struct{}

type legacyPollingFallbackAdaptor struct{}

func (pluginPollingFallbackAdaptor) Init(_ *relaycommon.RelayInfo) {}

func (pluginPollingFallbackAdaptor) FetchTask(_ string, _ string, _ *model.Task, _ string) (*http.Response, error) {
	return nil, nil
}

func (pluginPollingFallbackAdaptor) ParseTaskResult(_ *model.Task, _ *http.Response, _ []byte) (*relaycommon.TaskInfo, error) {
	return nil, nil
}

func (pluginPollingFallbackAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return 0
}

func (legacyPollingFallbackAdaptor) Init(_ *relaycommon.RelayInfo) {}

func (legacyPollingFallbackAdaptor) FetchTask(_ string, _ string, _ map[string]any, _ string) (*http.Response, error) {
	return nil, nil
}

func (legacyPollingFallbackAdaptor) ParseTaskResult(_ []byte) (*relaycommon.TaskInfo, error) {
	return nil, nil
}

func (legacyPollingFallbackAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return 0
}

type contextAwarePollingAdaptor struct {
	fetchContext  context.Context
	parseContext  context.Context
	adjustContext context.Context
}

type oversizedTaskPollingAdaptor struct {
	responseBody []byte
}

func TestRedactVideoResponseBodyKeepsGeminiPayloadForStorage(t *testing.T) {
	body := []byte(`{"response":{"bytesBase64Encoded":"aGVsbG8=","video":"aGVsbG8=","videos":[{"bytesBase64Encoded":"aGVsbG8="}]}}`)

	require.JSONEq(t, string(body), string(RedactVideoResponseBody(body)))
}

func TestUpdateVideoSingleTaskPersistsRemoteURLForSuccessfulTask(t *testing.T) {
	truncate(t)

	channel := &model.Channel{
		Id:     3601,
		Type:   constant.ChannelTypeGemini,
		Key:    "channel-secret",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, model.DB.Create(channel).Error)

	task := makeTask(3602, channel.Id, 0, 0, BillingSourceWallet, 0)
	task.TaskID = "task_remote_url_persist"
	task.Platform = constant.TaskPlatform("gemini")
	task.PrivateData.UpstreamTaskID = "upstream-remote-url"
	require.NoError(t, model.DB.Create(task).Error)

	adaptor := &pollOutcomeAdaptor{
		statusCode:   http.StatusOK,
		responseBody: []byte(`{"status":"SUCCESS"}`),
		parseResult: &relaycommon.TaskInfo{
			Status:    model.TaskStatusSuccess,
			RemoteUrl: "https://example.com/video.mp4",
			Progress:  "100%",
		},
	}

	require.NoError(t, updateVideoSingleTask(context.Background(), legacyTaskPollingAdaptorBridge{TaskPollingAdaptor: adaptor}, channel, task.GetUpstreamTaskID(), map[string]*model.Task{
		task.GetUpstreamTaskID(): task,
	}))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	require.Equal(t, string(model.TaskStatusSuccess), string(reloaded.Status))
	require.Equal(t, "https://example.com/video.mp4", reloaded.PrivateData.ResultURL)
	require.Equal(t, "https://example.com/video.mp4", reloaded.GetResultURL())
}

func TestUpdateVideoSingleTaskPersistsCrossInstanceResultURL(t *testing.T) {
	truncate(t)

	const channelID = 3609
	channel := &model.Channel{
		Id:     channelID,
		Type:   constant.ChannelTypeKling,
		Key:    "channel-secret",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, model.DB.Create(channel).Error)

	task := makeTask(3610, channelID, 0, 0, BillingSourceWallet, 0)
	task.TaskID = "task_cross_instance_result_url"
	task.Platform = constant.TaskPlatform("kling")
	task.PrivateData.UpstreamTaskID = "upstream-cross-instance-result-url"
	require.NoError(t, model.DB.Create(task).Error)

	responseBody, err := common.Marshal(dto.TaskResponse[dto.TaskDto]{
		Code: dto.TaskSuccessCode,
		Data: dto.TaskDto{
			TaskID:    task.GetUpstreamTaskID(),
			Status:    string(model.TaskStatusSuccess),
			Progress:  "100%",
			ResultURL: "https://remote-newapi.example/video.mp4",
		},
	})
	require.NoError(t, err)

	adaptor := &pollOutcomeAdaptor{
		statusCode:   http.StatusOK,
		responseBody: responseBody,
		parseErr:     errors.New("new api response must not fall back to provider parser"),
	}

	require.NoError(t, updateVideoSingleTask(context.Background(), legacyTaskPollingAdaptorBridge{TaskPollingAdaptor: adaptor}, channel, task.GetUpstreamTaskID(), map[string]*model.Task{
		task.GetUpstreamTaskID(): task,
	}))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	require.Equal(t, "https://remote-newapi.example/video.mp4", reloaded.PrivateData.ResultURL)
	require.Equal(t, "https://remote-newapi.example/video.mp4", reloaded.GetResultURL())
}

func TestUpdateVideoSingleTaskPreservesSuccessfulReasonWithoutResultURL(t *testing.T) {
	truncate(t)

	channel := &model.Channel{
		Id:     3603,
		Type:   constant.ChannelTypeGemini,
		Key:    "channel-secret",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, model.DB.Create(channel).Error)

	task := makeTask(3604, channel.Id, 0, 0, BillingSourceWallet, 0)
	task.TaskID = "task_success_reason_only"
	task.Platform = constant.TaskPlatform("gemini")
	task.PrivateData.UpstreamTaskID = "upstream-success-reason"
	require.NoError(t, model.DB.Create(task).Error)

	adaptor := &pollOutcomeAdaptor{
		statusCode:   http.StatusOK,
		responseBody: []byte(`{"status":"SUCCESS"}`),
		parseResult: &relaycommon.TaskInfo{
			Status:   model.TaskStatusSuccess,
			Reason:   "provider completed with a diagnostic message",
			Progress: "100%",
		},
	}

	require.NoError(t, updateVideoSingleTask(context.Background(), legacyTaskPollingAdaptorBridge{TaskPollingAdaptor: adaptor}, channel, task.GetUpstreamTaskID(), map[string]*model.Task{
		task.GetUpstreamTaskID(): task,
	}))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	require.Equal(t, string(model.TaskStatusSuccess), string(reloaded.Status))
	require.Equal(t, "provider completed with a diagnostic message", reloaded.FailReason)
	require.Equal(t, taskcommon.BuildProxyURL(task.TaskID), reloaded.PrivateData.ResultURL)
	require.Equal(t, taskcommon.BuildProxyURL(task.TaskID), reloaded.GetResultURL())
}

func TestUpdateVideoSingleTaskClearsLegacySuccessResultURLFromFailReason(t *testing.T) {
	truncate(t)

	channel := &model.Channel{
		Id:     3605,
		Type:   constant.ChannelTypeGemini,
		Key:    "channel-secret",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, model.DB.Create(channel).Error)

	task := makeTask(3606, channel.Id, 0, 0, BillingSourceWallet, 0)
	task.TaskID = "task_legacy_result_reason"
	task.Platform = constant.TaskPlatform("gemini")
	task.PrivateData.UpstreamTaskID = "upstream-legacy-result-reason"
	require.NoError(t, model.DB.Create(task).Error)

	responseBody := []byte(`{"code":"success","data":{"status":"SUCCESS","fail_reason":"https://provider.example/video.mp4"}}`)
	adaptor := &pollOutcomeAdaptor{
		statusCode:   http.StatusOK,
		responseBody: responseBody,
		fetchErr:     nil,
		parseErr:     errors.New("parseTaskResult should not be called for new api response"),
	}

	require.NoError(t, updateVideoSingleTask(context.Background(), legacyTaskPollingAdaptorBridge{TaskPollingAdaptor: adaptor}, channel, task.GetUpstreamTaskID(), map[string]*model.Task{
		task.GetUpstreamTaskID(): task,
	}))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	require.Equal(t, string(model.TaskStatusSuccess), string(reloaded.Status))
	require.Empty(t, reloaded.FailReason)
	require.Equal(t, "https://provider.example/video.mp4", reloaded.PrivateData.ResultURL)
	require.Equal(t, "https://provider.example/video.mp4", reloaded.GetResultURL())
}

func TestRunTaskPollingOnceFailsNullUpstreamTaskWithBillingClosure(t *testing.T) {
	truncate(t)

	oldTaskQueryLimit := constant.TaskQueryLimit
	constant.TaskQueryLimit = 10
	t.Cleanup(func() {
		constant.TaskQueryLimit = oldTaskQueryLimit
	})

	oldAdaptor := GetTaskAdaptorFunc
	oldPluginAdaptor := GetTaskPluginAdaptorFunc
	oldPluginAdaptorForTask := GetTaskPluginAdaptorForTaskFunc
	oldImageRunner := RunImageTasksFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor {
		return legacyPollingFallbackAdaptor{}
	}
	GetTaskPluginAdaptorFunc = nil
	GetTaskPluginAdaptorForTaskFunc = nil
	RunImageTasksFunc = nil
	t.Cleanup(func() {
		GetTaskAdaptorFunc = oldAdaptor
		GetTaskPluginAdaptorFunc = oldPluginAdaptor
		GetTaskPluginAdaptorForTaskFunc = oldPluginAdaptorForTask
		RunImageTasksFunc = oldImageRunner
	})

	const userID, tokenID, channelID = 3611, 3612, 3613
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, "null-upstream-token", 1000)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, 100, tokenID, BillingSourceWallet, 0)
	task.TaskID = ""
	task.Platform = constant.TaskPlatform("kling")
	task.Progress = "30%"
	task.PrivateData.UpstreamTaskID = ""
	require.NoError(t, model.DB.Create(task).Error)

	summary := RunTaskPollingOnce(context.Background(), nil)

	require.Equal(t, 1, summary.NullTasksFailed)
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	require.Equal(t, string(model.TaskStatusFailure), string(reloaded.Status))
	require.Equal(t, "100%", reloaded.Progress)
	require.NotZero(t, reloaded.FinishTime)
	require.Contains(t, reloaded.FailReason, "task_id")
	require.Zero(t, reloaded.Quota)
	require.EqualValues(t, 1100, getUserQuota(t, userID))
	require.EqualValues(t, 1100, getTokenRemainQuota(t, tokenID))
}

func (a *contextAwarePollingAdaptor) Init(_ *relaycommon.RelayInfo) {}

func (a *contextAwarePollingAdaptor) FetchTask(_ string, _ string, _ *model.Task, _ string) (*http.Response, error) {
	return nil, errors.New("legacy polling method should not be called")
}

func (a *contextAwarePollingAdaptor) FetchTaskContext(ctx context.Context, _ string, _ string, _ *model.Task, _ string) (*http.Response, error) {
	a.fetchContext = ctx
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{}`)),
	}, nil
}

func (a *contextAwarePollingAdaptor) ParseTaskResult(_ *model.Task, _ *http.Response, _ []byte) (*relaycommon.TaskInfo, error) {
	return nil, errors.New("legacy parse method should not be called")
}

func (a *contextAwarePollingAdaptor) ParseTaskResultContext(ctx context.Context, _ *model.Task, _ *http.Response, _ []byte) (*relaycommon.TaskInfo, error) {
	a.parseContext = ctx
	return &relaycommon.TaskInfo{Status: model.TaskStatusInProgress}, nil
}

func (a *contextAwarePollingAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return 0
}

func (a *contextAwarePollingAdaptor) AdjustBillingOnCompleteContext(ctx context.Context, _ *model.Task, _ *relaycommon.TaskInfo) int {
	a.adjustContext = ctx
	return 0
}

func (a *oversizedTaskPollingAdaptor) Init(_ *relaycommon.RelayInfo) {}

func (a *oversizedTaskPollingAdaptor) FetchTask(_ string, _ string, _ *model.Task, _ string) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(a.responseBody)),
	}, nil
}

func (a *oversizedTaskPollingAdaptor) ParseTaskResult(_ *model.Task, _ *http.Response, _ []byte) (*relaycommon.TaskInfo, error) {
	return &relaycommon.TaskInfo{Status: model.TaskStatusSuccess}, nil
}

func (a *oversizedTaskPollingAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return 0
}

func TestTaskPollingAdaptorForTaskDoesNotFallbackAcrossPluginSnapshot(t *testing.T) {
	task := &model.Task{
		Platform: constant.TaskPlatform("kling"),
		PrivateData: model.TaskPrivateData{
			Execution: &model.TaskExecutionSnapshot{
				TaskPlugin: &model.TaskPluginSnapshot{
					Key:     "kling",
					Version: "1.0.0",
				},
			},
		},
	}
	fallback := pluginPollingFallbackAdaptor{}
	previousTaskFactory := GetTaskPluginAdaptorForTaskFunc
	previousPlatformFactory := GetTaskPluginAdaptorFunc
	previousLegacyFactory := GetTaskAdaptorFunc
	GetTaskPluginAdaptorForTaskFunc = func(*model.Task) TaskPluginPollingAdaptor {
		return nil
	}
	GetTaskPluginAdaptorFunc = func(constant.TaskPlatform) TaskPluginPollingAdaptor {
		return fallback
	}
	GetTaskAdaptorFunc = nil
	t.Cleanup(func() {
		GetTaskPluginAdaptorForTaskFunc = previousTaskFactory
		GetTaskPluginAdaptorFunc = previousPlatformFactory
		GetTaskAdaptorFunc = previousLegacyFactory
	})

	assert.Nil(t, taskPollingAdaptorForTask(task, task.Platform))
}

func TestTaskPollingAdaptorForLegacyTaskDoesNotSilentlyUseCurrentPlugin(t *testing.T) {
	previousPlatformFactory := GetTaskPluginAdaptorFunc
	previousLegacyFactory := GetTaskAdaptorFunc
	fallback := pluginPollingFallbackAdaptor{}
	GetTaskPluginAdaptorFunc = func(constant.TaskPlatform) TaskPluginPollingAdaptor {
		return fallback
	}
	GetTaskAdaptorFunc = nil
	t.Cleanup(func() {
		GetTaskPluginAdaptorFunc = previousPlatformFactory
		GetTaskAdaptorFunc = previousLegacyFactory
	})

	task := &model.Task{Platform: constant.TaskPlatform("google")}
	assert.Nil(t, taskPollingAdaptorForTask(task, task.Platform))
}

func TestTaskPollingAdaptorForLegacyTaskStillUsesLegacyAdaptor(t *testing.T) {
	called := false
	previousPlatformFactory := GetTaskPluginAdaptorFunc
	previousLegacyFactory := GetTaskAdaptorFunc
	GetTaskPluginAdaptorFunc = func(constant.TaskPlatform) TaskPluginPollingAdaptor {
		called = true
		return nil
	}
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor {
		return legacyPollingFallbackAdaptor{}
	}
	t.Cleanup(func() {
		GetTaskPluginAdaptorFunc = previousPlatformFactory
		GetTaskAdaptorFunc = previousLegacyFactory
	})

	task := &model.Task{Platform: constant.TaskPlatform("suno")}
	adaptor := taskPollingAdaptorForTask(task, task.Platform)
	assert.NotNil(t, adaptor)
	assert.False(t, called)
}

func TestTaskPollingUsesContextAwarePluginMethods(t *testing.T) {
	truncate(t)
	adaptor := &contextAwarePollingAdaptor{}
	ctx := context.WithValue(context.Background(), struct{}{}, "poll")
	const channelID = 3503
	channel := &model.Channel{
		Id:     channelID,
		Type:   constant.ChannelTypeKling,
		Name:   "context-aware-poll-channel",
		Key:    "sk-context-aware-poll",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, model.DB.Create(channel).Error)
	task := makeTask(3501, channelID, 0, 0, BillingSourceWallet, 0)
	task.TaskID = "task_context_aware_poll"
	task.Platform = constant.TaskPlatform("kling")
	task.PrivateData.UpstreamTaskID = "upstream-1"
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, updateVideoSingleTask(ctx, adaptor, channel, task.GetUpstreamTaskID(), map[string]*model.Task{
		task.GetUpstreamTaskID(): task,
	}))
	assert.Same(t, ctx, adaptor.fetchContext)
	assert.Same(t, ctx, adaptor.parseContext)

	result := &relaycommon.TaskInfo{Status: model.TaskStatusInProgress}
	assert.Zero(t, adjustBillingOnCompleteWithContext(ctx, adaptor, task, result))
	assert.Same(t, ctx, adaptor.adjustContext)
}

func TestUpdateVideoSingleTaskRejectsOversizeResponseBody(t *testing.T) {
	truncate(t)
	const channelID = 3504
	channel := &model.Channel{
		Id:     channelID,
		Type:   constant.ChannelTypeKling,
		Name:   "oversize-poll-channel",
		Key:    "sk-oversize-poll",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, model.DB.Create(channel).Error)
	task := makeTask(3502, channelID, 0, 0, BillingSourceWallet, 0)
	task.TaskID = "task_oversize_poll"
	task.Platform = constant.TaskPlatform("kling")
	task.PrivateData.UpstreamTaskID = "upstream-oversize"
	require.NoError(t, model.DB.Create(task).Error)

	responseBody := []byte(`{"code":"success","data":{"task_id":"upstream-oversize","status":"SUCCESS","progress":"100%","fail_reason":"","result_url":"https://example.com/result.mp4","data":{}},"padding":"` + strings.Repeat("x", int(MaxResponseBodyBytes)+1) + `"}`)
	adaptor := &oversizedTaskPollingAdaptor{responseBody: responseBody}

	require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, channel, task.GetUpstreamTaskID(), map[string]*model.Task{
		task.GetUpstreamTaskID(): task,
	}))
	assert.NotEqual(t, model.TaskStatusSuccess, task.Status)
	assert.Greater(t, task.PrivateData.PollFailures, 0)
}

func (a *pollOutcomeAdaptor) Init(_ *relaycommon.RelayInfo) {}

func (a *pollOutcomeAdaptor) FetchTask(_ string, _ string, _ map[string]any, _ string) (*http.Response, error) {
	if a.fetchErr != nil {
		return nil, a.fetchErr
	}
	return &http.Response{
		StatusCode: a.statusCode,
		Body:       io.NopCloser(bytes.NewReader(a.responseBody)),
	}, nil
}

func (a *pollOutcomeAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	if a.parseErr != nil {
		return nil, a.parseErr
	}
	return a.parseResult, nil
}

func (a *pollOutcomeAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return a.adjustReturn
}

func (a *sunoResponsePollingAdaptor) Init(_ *relaycommon.RelayInfo) {}

func (a *sunoResponsePollingAdaptor) FetchTask(_ string, key string, body map[string]any, _ string) (*http.Response, error) {
	if a.fetchErr != nil {
		return nil, a.fetchErr
	}
	a.mu.Lock()
	a.keys = append(a.keys, key)
	a.bodies = append(a.bodies, body)
	a.mu.Unlock()
	if a.statusCode == 0 {
		a.statusCode = http.StatusOK
	}
	if a.responseBody != nil {
		return &http.Response{
			StatusCode: a.statusCode,
			Body:       io.NopCloser(bytes.NewReader(a.responseBody)),
		}, nil
	}
	responseBody, err := common.Marshal(a.response)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: a.statusCode,
		Body:       io.NopCloser(bytes.NewReader(responseBody)),
	}, nil
}

func (a *sunoResponsePollingAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return nil, nil
}

func (a *sunoResponsePollingAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return 0
}

func (a *sunoResponsePollingAdaptor) fetchedKeys() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.keys...)
}

func (a *sunoResponsePollingAdaptor) fetchedBodies() []map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	bodies := make([]map[string]any, 0, len(a.bodies))
	for _, body := range a.bodies {
		copied := make(map[string]any, len(body))
		for key, value := range body {
			copied[key] = value
		}
		bodies = append(bodies, copied)
	}
	return bodies
}

func TestRecoverPendingImageTaskRefundsClearsCompletedIntent(t *testing.T) {
	truncate(t)
	task := &model.Task{
		TaskID:        "task_recover_zero_refund_intent",
		Platform:      constant.TaskPlatformImage,
		Status:        model.TaskStatusFailure,
		RefundPending: true,
	}
	require.NoError(t, model.DB.Create(task).Error)

	recoverPendingImageTaskRefunds(context.Background(), 100)

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	require.False(t, reloaded.RefundPending)
}

func TestRecoverPendingTaskRefundsIncludesVideoTasks(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 3603, 3604, 3605
	const initialUserQuota, preConsumedQuota = 10000, 3000
	const initialTokenQuota = 5000

	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, "sk-video-refund-recovery", initialTokenQuota)
	seedChannel(t, channelID)
	setUserUsageCounters(t, userID, preConsumedQuota, 1)
	setChannelUsedQuota(t, channelID, int64(preConsumedQuota))

	task := makeTask(userID, channelID, preConsumedQuota, tokenID, BillingSourceWallet, 0)
	task.TaskID = "task_recover_video_refund"
	task.Platform = constant.TaskPlatform("kling")
	task.Status = model.TaskStatusFailure
	task.RefundPending = true
	task.SettlementStatus = model.TaskSettlementStatusPending
	require.NoError(t, model.DB.Create(task).Error)

	recoverPendingTaskRefunds(ctx, 100)

	require.EqualValues(t, initialUserQuota+preConsumedQuota, getUserQuota(t, userID))
	require.EqualValues(t, initialTokenQuota+preConsumedQuota, getTokenRemainQuota(t, tokenID))
	require.EqualValues(t, 0, getTokenUsedQuota(t, tokenID))
	require.Eventually(t, func() bool {
		usedQuota, requestCount := getUserUsageCounters(t, userID)
		return usedQuota == 0 && requestCount == 1
	}, time.Second, 10*time.Millisecond)
	require.EqualValues(t, 0, getChannelUsedQuota(t, channelID))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	require.Zero(t, reloaded.Quota)
	require.False(t, reloaded.RefundPending)
	require.Equal(t, model.TaskSettlementStatusPending, reloaded.SettlementStatus)
}

func TestRunTaskPollingOnceRecoversPendingTaskRefundsBeforeAdaptorCheck(t *testing.T) {
	truncate(t)
	oldAdaptor := GetTaskAdaptorFunc
	oldPluginAdaptor := GetTaskPluginAdaptorFunc
	oldPluginAdaptorForTask := GetTaskPluginAdaptorForTaskFunc
	oldImageRunner := RunImageTasksFunc
	GetTaskAdaptorFunc = nil
	GetTaskPluginAdaptorFunc = nil
	GetTaskPluginAdaptorForTaskFunc = nil
	RunImageTasksFunc = nil
	t.Cleanup(func() {
		GetTaskAdaptorFunc = oldAdaptor
		GetTaskPluginAdaptorFunc = oldPluginAdaptor
		GetTaskPluginAdaptorForTaskFunc = oldPluginAdaptorForTask
		RunImageTasksFunc = oldImageRunner
	})

	const userID, tokenID, channelID = 3606, 3607, 3608
	const initialUserQuota, preConsumedQuota = 10000, 3000
	const initialTokenQuota = 5000

	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, "sk-video-refund-runner", initialTokenQuota)
	seedChannel(t, channelID)
	setUserUsageCounters(t, userID, preConsumedQuota, 1)
	setChannelUsedQuota(t, channelID, int64(preConsumedQuota))

	task := makeTask(userID, channelID, preConsumedQuota, tokenID, BillingSourceWallet, 0)
	task.TaskID = "task_recover_video_refund_runner"
	task.Platform = constant.TaskPlatform("kling")
	task.Status = model.TaskStatusFailure
	task.RefundPending = true
	require.NoError(t, model.DB.Create(task).Error)

	summary := RunTaskPollingOnce(context.Background(), nil)

	require.Equal(t, 0, summary.UnfinishedTasks)
	require.EqualValues(t, initialUserQuota+preConsumedQuota, getUserQuota(t, userID))
	require.EqualValues(t, 0, getTokenUsedQuota(t, tokenID))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	require.Zero(t, reloaded.Quota)
	require.False(t, reloaded.RefundPending)
}

func (a *taskPollingFetchAdaptor) Init(_ *relaycommon.RelayInfo) {}

func (a *taskPollingFetchAdaptor) FetchTask(_ string, _ string, body map[string]any, _ string) (*http.Response, error) {
	taskID, _ := body["task_id"].(string)
	if taskID == a.blockTaskID && a.releaseBlock != nil {
		a.blockOnce.Do(func() {
			if a.blockStarted != nil {
				close(a.blockStarted)
			}
		})
		<-a.releaseBlock
	}

	a.mu.Lock()
	a.taskIDs = append(a.taskIDs, taskID)
	a.mu.Unlock()
	if a.fetched != nil {
		select {
		case a.fetched <- taskID:
		default:
		}
	}

	response := dto.TaskResponse[model.Task]{
		Code: dto.TaskSuccessCode,
		Data: model.Task{
			TaskID:   taskID,
			Status:   model.TaskStatusInProgress,
			Progress: "30%",
		},
	}
	responseBody, err := common.Marshal(response)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(responseBody)),
	}, nil
}

func (a *taskPollingFetchAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return &relaycommon.TaskInfo{Status: model.TaskStatusInProgress}, nil
}

func (a *taskPollingFetchAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return 0
}

func (a *taskPollingFetchAdaptor) fetchCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.taskIDs)
}

func (a *taskPollingFetchAdaptor) fetchedTaskIDs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.taskIDs...)
}

func (a *channelAwarePollingAdaptor) Init(_ *relaycommon.RelayInfo) {}

func (a *channelAwarePollingAdaptor) FetchTask(baseURL string, _ string, body map[string]any, _ string) (*http.Response, error) {
	a.mu.Lock()
	a.baseURLs = append(a.baseURLs, baseURL)
	a.mu.Unlock()
	taskID, _ := body["task_id"].(string)
	response := dto.TaskResponse[model.Task]{
		Code: dto.TaskSuccessCode,
		Data: model.Task{
			TaskID:   taskID,
			Status:   model.TaskStatusSuccess,
			Progress: "100%",
		},
	}
	responseBody, err := common.Marshal(response)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(responseBody)),
	}, nil
}

func (a *channelAwarePollingAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, Progress: "100%"}, nil
}

func (a *channelAwarePollingAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return 0
}

func (a *channelAwarePollingAdaptor) fetchedBaseURLs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.baseURLs...)
}

func seedTaskPollingChannel(t *testing.T, id int, disableSleep bool) {
	t.Helper()
	ch := &model.Channel{
		Id:     id,
		Type:   constant.ChannelTypeKling,
		Name:   "polling_channel",
		Key:    "sk-test",
		Status: common.ChannelStatusEnabled,
	}
	if disableSleep {
		ch.SetOtherSettings(dto.ChannelOtherSettings{DisableTaskPollingSleep: true})
	}
	require.NoError(t, model.DB.Create(ch).Error)
}

func seedPollingTask(t *testing.T, channelID int, publicID string, upstreamID string) *model.Task {
	t.Helper()
	task := &model.Task{
		TaskID:    publicID,
		Platform:  constant.TaskPlatform("kling"),
		UserId:    1,
		ChannelId: channelID,
		Action:    constant.TaskActionGenerate,
		Status:    model.TaskStatusInProgress,
		Progress:  "30%",
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: upstreamID,
		},
	}
	require.NoError(t, model.DB.Create(task).Error)
	return task
}

func TestRunTaskPollingOnceKeepsSameUpstreamIDAcrossChannels(t *testing.T) {
	truncate(t)
	previousTaskQueryLimit := constant.TaskQueryLimit
	constant.TaskQueryLimit = 10
	t.Cleanup(func() { constant.TaskQueryLimit = previousTaskQueryLimit })

	const (
		firstChannelID  = 4101
		secondChannelID = 4102
		upstreamTaskID  = "same-provider-task-id"
	)
	firstBaseURL := "https://provider-a.example"
	secondBaseURL := "https://provider-b.example"
	require.NoError(t, model.DB.Create(&model.Channel{
		Id: firstChannelID, Type: constant.ChannelTypeKling, Key: "key-a",
		Status: common.ChannelStatusEnabled, BaseURL: &firstBaseURL,
	}).Error)
	require.NoError(t, model.DB.Create(&model.Channel{
		Id: secondChannelID, Type: constant.ChannelTypeKling, Key: "key-b",
		Status: common.ChannelStatusEnabled, BaseURL: &secondBaseURL,
	}).Error)

	first := seedPollingTask(t, firstChannelID, "task_public_same_upstream_a", upstreamTaskID)
	second := seedPollingTask(t, secondChannelID, "task_public_same_upstream_b", upstreamTaskID)

	adaptor := &channelAwarePollingAdaptor{}
	previousFactory := GetTaskAdaptorFunc
	previousPluginFactory := GetTaskPluginAdaptorFunc
	previousTaskPluginFactory := GetTaskPluginAdaptorForTaskFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	GetTaskPluginAdaptorFunc = nil
	GetTaskPluginAdaptorForTaskFunc = nil
	t.Cleanup(func() {
		GetTaskAdaptorFunc = previousFactory
		GetTaskPluginAdaptorFunc = previousPluginFactory
		GetTaskPluginAdaptorForTaskFunc = previousTaskPluginFactory
	})

	summary := RunTaskPollingOnce(context.Background(), nil)
	require.Equal(t, 2, summary.UnfinishedTasks)
	assert.ElementsMatch(t, []string{firstBaseURL, secondBaseURL}, adaptor.fetchedBaseURLs())

	var firstReloaded, secondReloaded model.Task
	require.NoError(t, model.DB.First(&firstReloaded, first.ID).Error)
	require.NoError(t, model.DB.First(&secondReloaded, second.ID).Error)
	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), firstReloaded.Status)
	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), secondReloaded.Status)
}

func TestUpdateSunoTasksDoesNotRefundWhenStatusCASIsLost(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 9701, 9702, 9703
	const preConsumed = 1200
	const upstreamTaskID = "suno-cas-lost-upstream"
	baseURL := "https://suno.example"
	seedUser(t, userID, 8800)
	seedToken(t, tokenID, userID, "suno-cas-lost-token", 3800)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).Update("used_quota", preConsumed).Error)
	setUserUsageCounters(t, userID, preConsumed, 1)
	model.RecordTokenUsage(tokenID, userID, preConsumed, common.GetTimestamp())
	require.NoError(t, model.DB.Create(&model.Channel{
		Id:      channelID,
		Name:    "suno-cas-lost-channel",
		Key:     "suno-key",
		Status:  common.ChannelStatusEnabled,
		BaseURL: &baseURL,
	}).Error)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.TaskID = "suno-cas-lost-public"
	task.Platform = constant.TaskPlatformSuno
	task.Status = model.TaskStatusSuccess
	task.Progress = "100%"
	task.PrivateData.UpstreamTaskID = upstreamTaskID
	require.NoError(t, model.DB.Create(task).Error)

	staleTask := *task
	staleTask.Status = model.TaskStatusSubmitted
	staleTask.Progress = "50%"
	adaptor := &sunoResponsePollingAdaptor{
		response: dto.TaskResponse[[]dto.SunoDataResponse]{
			Code: dto.TaskSuccessCode,
			Data: []dto.SunoDataResponse{{
				TaskID:     upstreamTaskID,
				Status:     string(model.TaskStatusFailure),
				FailReason: "upstream failed",
			}},
		},
	}
	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })

	require.NoError(t, updateSunoTasks(context.Background(), channelID, []string{upstreamTaskID}, map[string]*model.Task{
		upstreamTaskID: &staleTask,
	}))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	require.EqualValues(t, 8800, getUserQuota(t, userID))
	require.EqualValues(t, 3800, getTokenRemainQuota(t, tokenID))
	require.EqualValues(t, preConsumed, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	require.EqualValues(t, preConsumed, usedQuota)
	require.Equal(t, 1, requestCount)
}

func TestUpdateSunoTasksUsesPersistedKeyPerTaskBatch(t *testing.T) {
	truncate(t)

	const channelID = 9813
	baseURL := "https://suno-multi-key.example"
	require.NoError(t, model.DB.Create(&model.Channel{
		Id:      channelID,
		Name:    "suno-multi-key-channel",
		Key:     "channel-key-a\nchannel-key-b",
		Status:  common.ChannelStatusEnabled,
		BaseURL: &baseURL,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: true,
		},
	}).Error)

	first := makeTask(1, channelID, 0, 0, BillingSourceWallet, 0)
	first.TaskID = "suno-multi-key-first"
	first.Platform = constant.TaskPlatformSuno
	first.PrivateData.UpstreamTaskID = "suno-upstream-a"
	first.PrivateData.Key = "selected-key-a"
	require.NoError(t, model.DB.Create(first).Error)

	second := makeTask(1, channelID, 0, 0, BillingSourceWallet, 0)
	second.TaskID = "suno-multi-key-second"
	second.Platform = constant.TaskPlatformSuno
	second.PrivateData.UpstreamTaskID = "suno-upstream-b"
	second.PrivateData.Key = "selected-key-b"
	require.NoError(t, model.DB.Create(second).Error)

	adaptor := &sunoResponsePollingAdaptor{
		response: dto.TaskResponse[[]dto.SunoDataResponse]{
			Code: dto.TaskSuccessCode,
			Data: []dto.SunoDataResponse{
				{TaskID: "suno-upstream-a", Status: string(model.TaskStatusInProgress)},
				{TaskID: "suno-upstream-b", Status: string(model.TaskStatusInProgress)},
			},
		},
	}
	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })

	taskM := map[string]*model.Task{
		pollingTaskReference(first):  first,
		pollingTaskReference(second): second,
	}
	require.NoError(t, updateSunoTasks(context.Background(), channelID, []string{
		pollingTaskReference(first),
		pollingTaskReference(second),
	}, taskM))

	assert.ElementsMatch(t, []string{"selected-key-a", "selected-key-b"}, adaptor.fetchedKeys())
	bodies := adaptor.fetchedBodies()
	require.Len(t, bodies, 2)
	var fetchedIDs [][]string
	for _, body := range bodies {
		ids, ok := body["ids"].([]string)
		require.True(t, ok)
		fetchedIDs = append(fetchedIDs, ids)
	}
	assert.ElementsMatch(t, [][]string{{"suno-upstream-a"}, {"suno-upstream-b"}}, fetchedIDs)
}

func TestUpdateSunoTasksFailsAndRefundsWhenUpstreamTaskGone(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 9801, 9802, 9803
	const upstreamTaskID = "suno-gone-upstream"
	baseURL := "https://suno.example"
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, "suno-gone-token", 1000)
	require.NoError(t, model.DB.Create(&model.Channel{
		Id:      channelID,
		Type:    constant.ChannelTypeSunoAPI,
		Name:    "suno-gone-channel",
		Key:     "suno-key",
		Status:  common.ChannelStatusEnabled,
		BaseURL: &baseURL,
	}).Error)

	task := makeTask(userID, channelID, 100, tokenID, BillingSourceWallet, 0)
	task.TaskID = "suno-gone-public"
	task.Platform = constant.TaskPlatformSuno
	task.PrivateData.UpstreamTaskID = upstreamTaskID
	require.NoError(t, model.DB.Create(task).Error)

	adaptor := &sunoResponsePollingAdaptor{
		statusCode:   http.StatusNotFound,
		responseBody: []byte(`{"error":"not found"}`),
	}
	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })

	require.NoError(t, updateSunoTasks(context.Background(), channelID, []string{upstreamTaskID}, map[string]*model.Task{
		upstreamTaskID: task,
	}))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status)
	require.Contains(t, reloaded.FailReason, "not found")
	require.Zero(t, reloaded.Quota)
	require.EqualValues(t, 1100, getUserQuota(t, userID))
}

func TestCleanupExpiredImageTaskResultsDeletesCancelledRequestFileOnOwnerNode(t *testing.T) {
	truncate(t)
	oldNodeName := common.NodeName
	oldFileCacheShared := constant.ImageTaskFileCacheShared
	oldFileCacheSharedTrusted := constant.ImageTaskFileCacheSharedTrusted
	common.NodeName = "api-node-a"
	constant.ImageTaskFileCacheShared = false
	constant.ImageTaskFileCacheSharedTrusted = false
	t.Cleanup(func() {
		common.NodeName = oldNodeName
		constant.ImageTaskFileCacheShared = oldFileCacheShared
		constant.ImageTaskFileCacheSharedTrusted = oldFileCacheSharedTrusted
	})

	bodyPath := filepath.Join(t.TempDir(), "cancelled-request.json")
	require.NoError(t, os.WriteFile(bodyPath, []byte(`{"prompt":"private input"}`), 0o600))
	now := time.Now().Unix()
	task := &model.Task{
		TaskID:                "task_cancelled_request_cleanup",
		Platform:              constant.TaskPlatformImage,
		Status:                model.TaskStatusFailure,
		StorageNode:           "api-node-a",
		RequestCleanupPending: true,
		PrivateData: model.TaskPrivateData{
			CancelledAt:     now,
			RequestBodyPath: bodyPath,
			NodeName:        "api-node-a",
		},
	}
	require.NoError(t, task.Insert())

	runImageTaskRequestCleanupPass(context.Background())

	_, err := os.Stat(bodyPath)
	require.ErrorIs(t, err, os.ErrNotExist)
	reloaded, exists, err := model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Empty(t, reloaded.PrivateData.RequestBodyPath)
	require.False(t, reloaded.RequestCleanupPending)
}

func TestImageTaskExecutionAvailableIsClusterScoped(t *testing.T) {
	oldUpdateTask := constant.UpdateTask
	oldWorkerEnabled := constant.ImageTaskWorkerEnabled
	oldMaster := common.IsMasterNode
	oldRunner := RunImageTasksFunc
	t.Cleanup(func() {
		constant.UpdateTask = oldUpdateTask
		constant.ImageTaskWorkerEnabled = oldWorkerEnabled
		common.IsMasterNode = oldMaster
		RunImageTasksFunc = oldRunner
	})

	RunImageTasksFunc = func(context.Context, []*model.Task) error { return nil }
	constant.UpdateTask = true

	// Pure API node (no local worker, not master) can still accept creates; other
	// nodes with workers/master poll pick the task up from the shared DB.
	common.IsMasterNode = false
	constant.ImageTaskWorkerEnabled = false
	require.True(t, ImageTaskExecutionAvailable())
	require.False(t, ImageTaskLocalExecutionAvailable())

	common.IsMasterNode = true
	constant.ImageTaskWorkerEnabled = false
	require.True(t, ImageTaskExecutionAvailable())
	require.True(t, ImageTaskLocalExecutionAvailable())

	common.IsMasterNode = false
	constant.ImageTaskWorkerEnabled = true
	require.True(t, ImageTaskExecutionAvailable())
	require.True(t, ImageTaskLocalExecutionAvailable())

	constant.UpdateTask = false
	require.False(t, ImageTaskExecutionAvailable())

	constant.UpdateTask = true
	RunImageTasksFunc = nil
	require.False(t, ImageTaskExecutionAvailable())
}

func TestGetImageTaskClusterExecutorAvailabilityUsesHeartbeatAdvertisement(t *testing.T) {
	truncate(t)
	oldUpdateTask := constant.UpdateTask
	oldWorkerEnabled := constant.ImageTaskWorkerEnabled
	oldMaster := common.IsMasterNode
	oldNode := common.NodeName
	oldRunner := RunImageTasksFunc
	t.Cleanup(func() {
		constant.UpdateTask = oldUpdateTask
		constant.ImageTaskWorkerEnabled = oldWorkerEnabled
		common.IsMasterNode = oldMaster
		common.NodeName = oldNode
		RunImageTasksFunc = oldRunner
	})

	RunImageTasksFunc = func(context.Context, []*model.Task) error { return nil }
	constant.UpdateTask = true
	common.IsMasterNode = false
	constant.ImageTaskWorkerEnabled = false
	common.NodeName = "api-node"

	// No heartbeat rows: evidence unusable, do not claim "no executor".
	avail := GetImageTaskClusterExecutorAvailability()
	require.False(t, avail.Known)
	require.False(t, avail.Has)

	now := time.Now().Unix()
	require.NoError(t, model.UpsertSystemInstance("api-node", map[string]any{
		"role": map[string]any{"is_master": false, "image_task_executor": false},
	}, now, now))
	require.NoError(t, model.UpsertSystemInstance("worker-node", map[string]any{
		"role": map[string]any{"is_master": false, "image_task_executor": true},
	}, now, now))

	avail = GetImageTaskClusterExecutorAvailability()
	require.True(t, avail.Known)
	require.True(t, avail.Has)

	require.NoError(t, model.DB.Where("node_name = ?", "worker-node").Delete(&model.SystemInstance{}).Error)
	avail = GetImageTaskClusterExecutorAvailability()
	require.True(t, avail.Known)
	require.False(t, avail.Has, "pure API cluster with only non-executors must report no executor")

	// Legacy worker heartbeat without image_task_executor must not hard-fail creates.
	require.NoError(t, model.UpsertSystemInstance("legacy-worker", map[string]any{
		"role": map[string]any{"is_master": true},
	}, now, now))
	avail = GetImageTaskClusterExecutorAvailability()
	require.False(t, avail.Known, "legacy heartbeat without capability field is incomplete evidence")
	require.False(t, avail.Has)

	// Local execution short-circuits heartbeat inspection.
	constant.ImageTaskWorkerEnabled = true
	avail = GetImageTaskClusterExecutorAvailability()
	require.True(t, avail.Known)
	require.True(t, avail.Has)
}

func TestGetImageTaskClusterExecutorAvailabilityRequiresExplicitFalseFromAllNodes(t *testing.T) {
	truncate(t)
	oldUpdateTask := constant.UpdateTask
	oldWorkerEnabled := constant.ImageTaskWorkerEnabled
	oldMaster := common.IsMasterNode
	oldNode := common.NodeName
	oldRunner := RunImageTasksFunc
	t.Cleanup(func() {
		constant.UpdateTask = oldUpdateTask
		constant.ImageTaskWorkerEnabled = oldWorkerEnabled
		common.IsMasterNode = oldMaster
		common.NodeName = oldNode
		RunImageTasksFunc = oldRunner
	})

	RunImageTasksFunc = func(context.Context, []*model.Task) error { return nil }
	constant.UpdateTask = true
	common.IsMasterNode = false
	constant.ImageTaskWorkerEnabled = false
	common.NodeName = "api-node"
	now := time.Now().Unix()

	require.NoError(t, model.UpsertSystemInstance("api-node", map[string]any{
		"role": map[string]any{"image_task_executor": false},
	}, now, now))
	require.NoError(t, model.UpsertSystemInstance("api-node-2", map[string]any{
		"role": map[string]any{"image_task_executor": false},
	}, now, now))

	avail := GetImageTaskClusterExecutorAvailability()
	require.True(t, avail.Known)
	require.False(t, avail.Has)
}

func TestImageTaskRequestBodyBase64FallbackForcedOnAPIOnlyNode(t *testing.T) {
	oldShared := constant.ImageTaskFileCacheShared
	oldTrusted := constant.ImageTaskFileCacheSharedTrusted
	oldAffinity := constant.ImageTaskLocalFileCacheAffinity
	oldWorkerEnabled := constant.ImageTaskWorkerEnabled
	oldMaster := common.IsMasterNode
	oldNode := common.NodeName
	oldRunner := RunImageTasksFunc
	t.Cleanup(func() {
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskFileCacheSharedTrusted = oldTrusted
		constant.ImageTaskLocalFileCacheAffinity = oldAffinity
		constant.ImageTaskWorkerEnabled = oldWorkerEnabled
		common.IsMasterNode = oldMaster
		common.NodeName = oldNode
		RunImageTasksFunc = oldRunner
	})

	RunImageTasksFunc = func(context.Context, []*model.Task) error { return nil }
	constant.ImageTaskFileCacheShared = false
	constant.ImageTaskFileCacheSharedTrusted = false
	constant.ImageTaskLocalFileCacheAffinity = true
	common.NodeName = "api-only"
	common.IsMasterNode = false
	constant.ImageTaskWorkerEnabled = false

	require.True(t, ImageTaskRequestBodyBase64FallbackEnabled())

	// Trusted shared cache still wins: bodies stay on the shared volume.
	constant.ImageTaskFileCacheShared = true
	constant.ImageTaskFileCacheSharedTrusted = true
	require.False(t, ImageTaskRequestBodyBase64FallbackEnabled())
}

func TestLogImageTaskDeploymentWarningsCoversAPIOnlyAndDisabledSystem(t *testing.T) {
	// Smoke-call the warning helper under the two residual deployment shapes
	// from the closed-loop review: disabled system, and create-capable API-only.
	// The helper must not panic and must remain safe to call at process start.
	oldUpdateTask := constant.UpdateTask
	oldWorkerEnabled := constant.ImageTaskWorkerEnabled
	oldMaster := common.IsMasterNode
	oldRunner := RunImageTasksFunc
	oldRetention := constant.ImageTaskResultRetentionMinutes
	t.Cleanup(func() {
		constant.UpdateTask = oldUpdateTask
		constant.ImageTaskWorkerEnabled = oldWorkerEnabled
		common.IsMasterNode = oldMaster
		RunImageTasksFunc = oldRunner
		constant.ImageTaskResultRetentionMinutes = oldRetention
	})

	constant.ImageTaskResultRetentionMinutes = 720
	constant.UpdateTask = false
	RunImageTasksFunc = nil
	require.NotPanics(t, logImageTaskDeploymentWarnings)

	RunImageTasksFunc = func(context.Context, []*model.Task) error { return nil }
	constant.UpdateTask = true
	common.IsMasterNode = false
	constant.ImageTaskWorkerEnabled = false
	require.True(t, ImageTaskExecutionAvailable())
	require.False(t, ImageTaskLocalExecutionAvailable())
	require.NotPanics(t, logImageTaskDeploymentWarnings)

	common.IsMasterNode = true
	require.True(t, ImageTaskLocalExecutionAvailable())
	require.NotPanics(t, logImageTaskDeploymentWarnings)
}

func TestRunImageTaskResultCleanupPassIsSafeOnNonMasterNode(t *testing.T) {
	// Result lifecycle cleanup is no longer master-only; any node may run the
	// pass. An empty DB must be a no-op rather than panic or require master.
	truncate(t)
	oldMaster := common.IsMasterNode
	oldCleanupUnix := atomic.LoadInt64(&imageTaskResultRecordCleanupUnix)
	common.IsMasterNode = false
	atomic.StoreInt64(&imageTaskResultRecordCleanupUnix, 0)
	t.Cleanup(func() {
		common.IsMasterNode = oldMaster
		atomic.StoreInt64(&imageTaskResultRecordCleanupUnix, oldCleanupUnix)
	})

	require.NotPanics(t, func() {
		runImageTaskResultCleanupPass(context.Background())
	})
}

func TestProcessImageTaskResultFileCleanupsSkipsForeignLocalResultPath(t *testing.T) {
	truncate(t)
	oldNodeName := common.NodeName
	oldFileCacheShared := constant.ImageTaskFileCacheShared
	oldFileCacheSharedTrusted := constant.ImageTaskFileCacheSharedTrusted
	common.NodeName = "api-node-b"
	constant.ImageTaskFileCacheShared = false
	constant.ImageTaskFileCacheSharedTrusted = false
	t.Cleanup(func() {
		common.NodeName = oldNodeName
		constant.ImageTaskFileCacheShared = oldFileCacheShared
		constant.ImageTaskFileCacheSharedTrusted = oldFileCacheSharedTrusted
	})

	missingForeignPath := filepath.Join(t.TempDir(), "foreign-result.json")
	now := time.Now().Unix()
	task := &model.Task{
		TaskID:                  "task_foreign_result_cleanup",
		Platform:                constant.TaskPlatformImage,
		Status:                  model.TaskStatusSuccess,
		SettlementStatus:        model.TaskSettlementStatusSettled,
		StorageNode:             "worker-node-a",
		FinishTime:              now - 60,
		ResultCleanedAt:         now,
		ResultCleanupPending:    true,
		ImageTaskResultStored:   true,
		ImageTaskResultStoredAt: now - 60,
		PrivateData: model.TaskPrivateData{
			NodeName:          "worker-node-a",
			ResultBodyPath:    missingForeignPath,
			ResultBodySize:    123,
			ResultBodySHA256:  "sha",
			ResultContentType: "application/json",
			ResultStoredAt:    now - 60,
			ResultExpiresAt:   now + 3600,
		},
	}
	task.SetData(map[string]any{"_newapi_result_file": true})
	require.NoError(t, task.Insert())

	processImageTaskResultFileCleanups(context.Background(), []model.ImageTaskResultCleanup{{
		TaskPrimaryID: task.ID,
		Path:          missingForeignPath,
	}})

	reloaded, exists, err := model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.True(t, reloaded.ResultCleanupPending)
	require.Equal(t, missingForeignPath, reloaded.PrivateData.ResultBodyPath)
	require.True(t, reloaded.ImageTaskResultStored)
	require.EqualValues(t, 123, reloaded.PrivateData.ResultBodySize)
}

func TestProcessImageTaskResultFileCleanupsKeepsUnknownOwnerMissingResultPath(t *testing.T) {
	truncate(t)
	oldNodeName := common.NodeName
	oldFileCacheShared := constant.ImageTaskFileCacheShared
	oldFileCacheSharedTrusted := constant.ImageTaskFileCacheSharedTrusted
	common.NodeName = "api-node-b"
	constant.ImageTaskFileCacheShared = false
	constant.ImageTaskFileCacheSharedTrusted = false
	t.Cleanup(func() {
		common.NodeName = oldNodeName
		constant.ImageTaskFileCacheShared = oldFileCacheShared
		constant.ImageTaskFileCacheSharedTrusted = oldFileCacheSharedTrusted
	})

	missingPath := filepath.Join(t.TempDir(), "unknown-owner-result.json")
	now := time.Now().Unix()
	task := &model.Task{
		TaskID:                  "task_unknown_owner_result_cleanup",
		Platform:                constant.TaskPlatformImage,
		Status:                  model.TaskStatusSuccess,
		SettlementStatus:        model.TaskSettlementStatusSettled,
		StorageNode:             model.ImageTaskPortableStorageNode,
		FinishTime:              now - 60,
		ResultCleanedAt:         now,
		ResultCleanupPending:    true,
		ImageTaskResultStored:   true,
		ImageTaskResultStoredAt: now - 60,
		PrivateData: model.TaskPrivateData{
			ResultBodyPath:    missingPath,
			ResultBodySize:    456,
			ResultBodySHA256:  "sha",
			ResultContentType: "application/json",
			ResultStoredAt:    now - 60,
			ResultExpiresAt:   now + 3600,
		},
	}
	task.SetData(map[string]any{"_newapi_result_file": true})
	require.NoError(t, task.Insert())

	processImageTaskResultFileCleanups(context.Background(), []model.ImageTaskResultCleanup{{
		TaskPrimaryID: task.ID,
		Path:          missingPath,
	}})

	reloaded, exists, err := model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.True(t, reloaded.ResultCleanupPending)
	require.Equal(t, missingPath, reloaded.PrivateData.ResultBodyPath)
	require.True(t, reloaded.ImageTaskResultStored)
	require.EqualValues(t, 456, reloaded.PrivateData.ResultBodySize)
}

func TestScheduleImageTaskRequestFileCleanupRetainsOwnerPathUntilDeletion(t *testing.T) {
	now := time.Now().Unix()
	task := &model.Task{
		Platform:    constant.TaskPlatformImage,
		StorageNode: model.ImageTaskPortableStorageNode,
		PrivateData: model.TaskPrivateData{
			NodeName:            "api-node-a",
			RequestBodyPath:     "/tmp/image-task-request.json",
			RequestBodyBase64:   "c2VjcmV0",
			RequestBodyPortable: true,
		},
	}

	ScheduleImageTaskRequestFileCleanup(task, now+int64((12*time.Hour).Seconds()))

	require.True(t, task.RequestCleanupPending)
	require.Equal(t, now+int64((12*time.Hour).Seconds()), task.RequestDeleteAfter)
	require.Equal(t, "/tmp/image-task-request.json", task.PrivateData.RequestBodyPath)
	require.Empty(t, task.PrivateData.RequestBodyBase64)
	require.False(t, task.PrivateData.RequestBodyPortable)
}

func TestCleanupDueImageTaskRequestFileKeepsHistoricalLocalFileOnForeignNode(t *testing.T) {
	truncate(t)
	oldNodeName := common.NodeName
	oldFileCacheShared := constant.ImageTaskFileCacheShared
	oldFileCacheSharedTrusted := constant.ImageTaskFileCacheSharedTrusted
	common.NodeName = "api-node-b"
	constant.ImageTaskFileCacheShared = true
	constant.ImageTaskFileCacheSharedTrusted = true
	t.Cleanup(func() {
		common.NodeName = oldNodeName
		constant.ImageTaskFileCacheShared = oldFileCacheShared
		constant.ImageTaskFileCacheSharedTrusted = oldFileCacheSharedTrusted
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:                "task_historical_local_request_cleanup",
		Platform:              constant.TaskPlatformImage,
		Status:                model.TaskStatusFailure,
		StorageNode:           "api-node-a",
		RequestCleanupPending: true,
		RequestDeleteAfter:    now,
		PrivateData: model.TaskPrivateData{
			RequestBodyPath: "/cache-owned-by-api-node-a/request.json",
			NodeName:        "api-node-a",
		},
	}
	require.NoError(t, task.Insert())

	cleanupPendingImageTaskRequestFiles(context.Background(), 100)

	reloaded, exists, err := model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, task.PrivateData.RequestBodyPath, reloaded.PrivateData.RequestBodyPath)
	require.True(t, reloaded.RequestCleanupPending)
	require.Equal(t, now, reloaded.RequestDeleteAfter)
}

func TestCleanupDueImageTaskRequestFileAllowsRecordedSharedFileOnForeignNode(t *testing.T) {
	truncate(t)
	oldNodeName := common.NodeName
	oldFileCacheShared := constant.ImageTaskFileCacheShared
	oldFileCacheSharedTrusted := constant.ImageTaskFileCacheSharedTrusted
	common.NodeName = "api-node-b"
	constant.ImageTaskFileCacheShared = true
	constant.ImageTaskFileCacheSharedTrusted = true
	t.Cleanup(func() {
		common.NodeName = oldNodeName
		constant.ImageTaskFileCacheShared = oldFileCacheShared
		constant.ImageTaskFileCacheSharedTrusted = oldFileCacheSharedTrusted
	})

	task := &model.Task{
		TaskID:                "task_recorded_shared_request_cleanup",
		Platform:              constant.TaskPlatformImage,
		Status:                model.TaskStatusFailure,
		StorageNode:           "api-node-a",
		RequestCleanupPending: true,
		RequestDeleteAfter:    time.Now().Unix(),
		PrivateData: model.TaskPrivateData{
			RequestBodyPath:   "/shared-cache/request.json",
			RequestBodyShared: true,
			NodeName:          "api-node-a",
		},
	}
	require.NoError(t, task.Insert())

	cleanupPendingImageTaskRequestFiles(context.Background(), 100)

	reloaded, exists, err := model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Empty(t, reloaded.PrivateData.RequestBodyPath)
	require.False(t, reloaded.PrivateData.RequestBodyShared)
	require.False(t, reloaded.RequestCleanupPending)
	require.Zero(t, reloaded.RequestDeleteAfter)
}

func resetImageTaskOrphanSweepThrottle(t *testing.T) {
	t.Helper()
	atomic.StoreInt64(&imageTaskOrphanSweepUnix, 0)
	t.Cleanup(func() { atomic.StoreInt64(&imageTaskOrphanSweepUnix, 0) })
}

// D-5：同一请求内多次估算 token（渠道重试）不得反复解析 multipart 并留下临时文件。
func TestEstimateRequestTokenReusesParsedMultipartFormAcrossRetries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// multipart 临时文件重定向到本用例独占目录，避免跨包并行统计串扰。
	isolatedTempDir := t.TempDir()
	t.Setenv("TMPDIR", isolatedTempDir)
	t.Setenv("TMP", isolatedTempDir)
	t.Setenv("TEMP", isolatedTempDir)
	oldCountToken := constant.CountToken
	oldMaxFileDownloadMB := constant.MaxFileDownloadMB
	constant.CountToken = true
	constant.MaxFileDownloadMB = 1
	t.Cleanup(func() {
		constant.CountToken = oldCountToken
		constant.MaxFileDownloadMB = oldMaxFileDownloadMB
	})

	countTempFiles := func() int {
		matches, err := filepath.Glob(filepath.Join(os.TempDir(), "multipart-*"))
		require.NoError(t, err)
		return len(matches)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "whisper-1"))
	part, err := writer.CreateFormFile("file", "audio.bin")
	require.NoError(t, err)
	_, err = part.Write(bytes.Repeat([]byte("a"), (2<<20)+1))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", nil)
	ctx.Request.Header.Set("Content-Type", writer.FormDataContentType())
	storage, err := common.CreateBodyStorage(body.Bytes())
	require.NoError(t, err)
	t.Cleanup(func() { storage.Close() })
	ctx.Set(common.KeyBodyStorage, storage)
	t.Cleanup(func() {
		if ctx.Request.MultipartForm != nil {
			_ = ctx.Request.MultipartForm.RemoveAll()
		}
	})

	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeAudioTranscription}
	meta := &types.TokenCountMeta{}

	baseline := countTempFiles()
	_, _ = EstimateRequestToken(ctx, meta, info)
	afterFirst := countTempFiles()
	require.Greater(t, afterFirst, baseline, "test setup must spill the multipart file to disk")

	_, _ = EstimateRequestToken(ctx, meta, info)
	require.Equal(t, afterFirst, countTempFiles(),
		"retrying token estimation must reuse the parsed form instead of leaking a new one")
}

func seedOrphanSweepBilling(t *testing.T, userID int, tokenID int, channelID int, quota int64) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.User{
		Id: userID, Username: "orphan_user", Quota: 0, Status: common.UserStatusEnabled,
	}).Error)
	require.NoError(t, model.DB.Create(&model.Token{
		Id: tokenID, UserId: userID, Key: "sk-orphan-" + strconv.Itoa(tokenID), Name: "orphan_token",
		Status: common.TokenStatusEnabled, RemainQuota: 0, UsedQuota: quota,
	}).Error)
	require.NoError(t, model.DB.Create(&model.Channel{
		Id: channelID, Type: constant.ChannelTypeOpenAI, Name: "orphan_channel",
		Key: "sk-test", Status: common.ChannelStatusEnabled,
	}).Error)
}

// seedLiveInstances 写入节点心跳。孤儿判据依赖它区分"节点消失"和"节点健在但积压"。
func seedLiveInstances(t *testing.T, nodeNames ...string) {
	t.Helper()
	now := time.Now().Unix()
	for _, name := range nodeNames {
		require.NoError(t, model.DB.Create(&model.SystemInstance{
			NodeName:   name,
			StartedAt:  now - 600,
			LastSeenAt: now,
			CreatedAt:  now - 600,
			UpdatedAt:  now,
		}).Error)
	}
}

func newOrphanImageTask(taskID string, userID, tokenID, channelID, quota int) *model.Task {
	return &model.Task{
		TaskID:      taskID,
		Platform:    constant.TaskPlatformImage,
		UserId:      userID,
		ChannelId:   channelID,
		Group:       "default",
		Quota:       quota,
		Status:      model.TaskStatusQueued,
		Progress:    "0%",
		StorageNode: "dead-node",
		Properties:  model.Properties{OriginModelName: "test-model"},
		PrivateData: model.TaskPrivateData{
			BillingSource: BillingSourceWallet,
			TokenId:       tokenID,
			NodeName:      "dead-node",
			BillingContext: &model.TaskBillingContext{
				ModelPrice:      0.02,
				GroupRatio:      1.0,
				OriginModelName: "test-model",
			},
		},
	}
}

// D-1：归属节点仍在心跳的任务只是排队积压，绝不能被孤儿清扫失败退款。
func TestSweepOrphanedImageTasksKeepsBacklogOnLiveNode(t *testing.T) {
	truncate(t)
	resetImageTaskOrphanSweepThrottle(t)
	oldOrphanSeconds := constant.ImageTaskOrphanFailSeconds
	oldTimeout := constant.TaskTimeoutMinutes
	oldNodeName := common.NodeName
	constant.ImageTaskOrphanFailSeconds = 1800
	constant.TaskTimeoutMinutes = 1440
	common.NodeName = "live-node"
	t.Cleanup(func() {
		constant.ImageTaskOrphanFailSeconds = oldOrphanSeconds
		constant.TaskTimeoutMinutes = oldTimeout
		common.NodeName = oldNodeName
	})

	const userID, tokenID, channelID, quota = 4201, 4201, 4201, 3000
	seedOrphanSweepBilling(t, userID, tokenID, channelID, quota)
	seedLiveInstances(t, "live-node", "busy-node")

	now := time.Now().Unix()
	task := newOrphanImageTask("task_backlog_live_node", userID, tokenID, channelID, quota)
	task.StorageNode = "busy-node"
	task.PrivateData.NodeName = "busy-node"
	task.SubmitTime = now - 2400
	task.NextPollAt = now - 2400
	require.NoError(t, model.DB.Create(task).Error)

	sweepOrphanedImageTasks(context.Background(), 100)

	reloaded, exists, err := model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, model.TaskStatus(model.TaskStatusQueued), reloaded.Status,
		"backlogged task on a live node must not be failed by the orphan sweep")
	require.EqualValues(t, quota, reloaded.Quota)
}

// D-1：storage_node 为空或便携的任务任何节点都能调度，未被调度不等于孤儿。
func TestSweepOrphanedImageTasksKeepsPortableAndUnboundTasks(t *testing.T) {
	truncate(t)
	resetImageTaskOrphanSweepThrottle(t)
	oldOrphanSeconds := constant.ImageTaskOrphanFailSeconds
	oldTimeout := constant.TaskTimeoutMinutes
	oldNodeName := common.NodeName
	constant.ImageTaskOrphanFailSeconds = 1800
	constant.TaskTimeoutMinutes = 1440
	common.NodeName = "live-node"
	t.Cleanup(func() {
		constant.ImageTaskOrphanFailSeconds = oldOrphanSeconds
		constant.TaskTimeoutMinutes = oldTimeout
		common.NodeName = oldNodeName
	})

	const userID, tokenID, channelID, quota = 4202, 4202, 4202, 1000
	seedOrphanSweepBilling(t, userID, tokenID, channelID, quota)
	seedLiveInstances(t, "live-node")

	now := time.Now().Unix()
	portable := newOrphanImageTask("task_orphan_portable", userID, tokenID, channelID, quota)
	portable.StorageNode = model.ImageTaskPortableStorageNode
	portable.SubmitTime = now - 2400
	portable.NextPollAt = now - 2400
	require.NoError(t, model.DB.Create(portable).Error)

	unbound := newOrphanImageTask("task_orphan_unbound", userID, tokenID, channelID, quota)
	unbound.StorageNode = ""
	unbound.PrivateData.NodeName = ""
	unbound.SubmitTime = now - 2400
	unbound.NextPollAt = now - 2400
	require.NoError(t, model.DB.Create(unbound).Error)

	sweepOrphanedImageTasks(context.Background(), 100)

	for _, id := range []int64{portable.ID, unbound.ID} {
		reloaded, exists, err := model.GetTaskByID(id)
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, model.TaskStatus(model.TaskStatusQueued), reloaded.Status)
		require.EqualValues(t, quota, reloaded.Quota)
	}
}

// D-1：心跳数据不可用时（本节点都不在实例表里）不得凭时间猜测孤儿。
func TestSweepOrphanedImageTasksSkipsGraceWhenHeartbeatUnavailable(t *testing.T) {
	truncate(t)
	resetImageTaskOrphanSweepThrottle(t)
	oldOrphanSeconds := constant.ImageTaskOrphanFailSeconds
	oldTimeout := constant.TaskTimeoutMinutes
	oldNodeName := common.NodeName
	constant.ImageTaskOrphanFailSeconds = 1800
	constant.TaskTimeoutMinutes = 1440
	common.NodeName = "live-node"
	t.Cleanup(func() {
		constant.ImageTaskOrphanFailSeconds = oldOrphanSeconds
		constant.TaskTimeoutMinutes = oldTimeout
		common.NodeName = oldNodeName
	})

	const userID, tokenID, channelID, quota = 4203, 4203, 4203, 1500
	seedOrphanSweepBilling(t, userID, tokenID, channelID, quota)
	// 故意不写入任何心跳记录

	now := time.Now().Unix()
	task := newOrphanImageTask("task_orphan_no_heartbeat", userID, tokenID, channelID, quota)
	task.SubmitTime = now - 2400
	task.NextPollAt = now - 2400
	require.NoError(t, model.DB.Create(task).Error)

	sweepOrphanedImageTasks(context.Background(), 100)

	reloaded, exists, err := model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, model.TaskStatus(model.TaskStatusQueued), reloaded.Status)
	require.EqualValues(t, quota, reloaded.Quota)
}

// D-2：节点持有租约时崩溃，lock_owner 残留且租约过期，必须能被接管并退款。
func TestSweepOrphanedImageTasksTakesOverStaleLeaseFromCrashedNode(t *testing.T) {
	truncate(t)
	resetImageTaskOrphanSweepThrottle(t)
	oldOrphanSeconds := constant.ImageTaskOrphanFailSeconds
	oldNodeName := common.NodeName
	constant.ImageTaskOrphanFailSeconds = 1800
	common.NodeName = "live-node"
	t.Cleanup(func() {
		constant.ImageTaskOrphanFailSeconds = oldOrphanSeconds
		common.NodeName = oldNodeName
	})

	const userID, tokenID, channelID, quota = 4204, 4204, 4204, 4000
	seedOrphanSweepBilling(t, userID, tokenID, channelID, quota)
	seedLiveInstances(t, "live-node")

	now := time.Now().Unix()
	task := newOrphanImageTask("task_crashed_stale_lease", userID, tokenID, channelID, quota)
	task.SubmitTime = now - 7200
	task.NextPollAt = now - 7200
	task.LockOwner = "dead-node-image-123-abc"
	task.LockUntil = now - 3600
	require.NoError(t, model.DB.Create(task).Error)

	sweepOrphanedImageTasks(context.Background(), 100)

	reloaded, exists, err := model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status,
		"a stale lease from a crashed node must not block the orphan sweep")
	require.Zero(t, reloaded.Quota)
	require.Empty(t, reloaded.LockOwner)

	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	require.EqualValues(t, quota, user.Quota)
}

// D-3：归属节点已确认消失时，清扫必须收口请求体元数据，避免永久重复扫描。
func TestSweepOrphanedImageTasksFinalizesForeignRequestMetadataWhenOwnerVanished(t *testing.T) {
	truncate(t)
	resetImageTaskOrphanSweepThrottle(t)
	oldOrphanSeconds := constant.ImageTaskOrphanFailSeconds
	oldNodeName := common.NodeName
	oldShared := constant.ImageTaskFileCacheShared
	oldTrusted := constant.ImageTaskFileCacheSharedTrusted
	constant.ImageTaskOrphanFailSeconds = 1800
	constant.ImageTaskFileCacheShared = false
	constant.ImageTaskFileCacheSharedTrusted = false
	common.NodeName = "live-node"
	t.Cleanup(func() {
		constant.ImageTaskOrphanFailSeconds = oldOrphanSeconds
		common.NodeName = oldNodeName
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskFileCacheSharedTrusted = oldTrusted
	})

	const userID, tokenID, channelID, quota = 4205, 4205, 4205, 2000
	seedOrphanSweepBilling(t, userID, tokenID, channelID, quota)
	seedLiveInstances(t, "live-node")

	now := time.Now().Unix()
	bodyPath := filepath.Join(t.TempDir(), "foreign-node-body.json")
	task := newOrphanImageTask("task_foreign_body_file", userID, tokenID, channelID, quota)
	task.PrivateData.RequestBodyPath = bodyPath
	task.SubmitTime = now - 7200
	task.NextPollAt = now - 7200
	require.NoError(t, model.DB.Create(task).Error)

	sweepOrphanedImageTasks(context.Background(), 100)

	reloaded, exists, err := model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status)
	require.Empty(t, reloaded.PrivateData.RequestBodyPath)
	require.False(t, reloaded.RequestCleanupPending)
	pending, err := model.GetPendingImageTaskRequestFileCleanupsAfter(time.Now().Unix(), 0, 100)
	require.NoError(t, err)
	require.Empty(t, pending)
}

// D-4：清扫不得因为查询时省略 data 列而把已有 data 抹掉。
func TestSweepOrphanedImageTasksDoesNotWipeTaskData(t *testing.T) {
	truncate(t)
	resetImageTaskOrphanSweepThrottle(t)
	oldOrphanSeconds := constant.ImageTaskOrphanFailSeconds
	oldNodeName := common.NodeName
	constant.ImageTaskOrphanFailSeconds = 1800
	common.NodeName = "live-node"
	t.Cleanup(func() {
		constant.ImageTaskOrphanFailSeconds = oldOrphanSeconds
		common.NodeName = oldNodeName
	})

	const userID, tokenID, channelID, quota = 4206, 4206, 4206, 1200
	seedOrphanSweepBilling(t, userID, tokenID, channelID, quota)
	seedLiveInstances(t, "live-node")

	now := time.Now().Unix()
	task := newOrphanImageTask("task_orphan_keeps_data", userID, tokenID, channelID, quota)
	task.SubmitTime = now - 7200
	task.NextPollAt = now - 7200
	task.Data = json.RawMessage(`{"progress_marker":"keep-me"}`)
	require.NoError(t, model.DB.Create(task).Error)

	sweepOrphanedImageTasks(context.Background(), 100)

	reloaded, exists, err := model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status)
	require.JSONEq(t, `{"progress_marker":"keep-me"}`, string(reloaded.Data),
		"orphan sweep must not blank the data column it never loaded")
}

func TestSweepOrphanedImageTasksFailsAndRefundsUnclaimedQueuedTask(t *testing.T) {
	truncate(t)
	resetImageTaskOrphanSweepThrottle(t)
	oldOrphanSeconds := constant.ImageTaskOrphanFailSeconds
	oldNodeName := common.NodeName
	constant.ImageTaskOrphanFailSeconds = 1800
	common.NodeName = "live-node"
	t.Cleanup(func() {
		constant.ImageTaskOrphanFailSeconds = oldOrphanSeconds
		common.NodeName = oldNodeName
	})
	seedLiveInstances(t, "live-node")

	const userID, tokenID, channelID, quota = 4101, 4101, 4101, 3000
	seedOrphanSweepBilling(t, userID, tokenID, channelID, quota)

	now := time.Now().Unix()
	task := newOrphanImageTask("task_orphan_queued", userID, tokenID, channelID, quota)
	task.StorageNode = "dead-node"
	task.SubmitTime = now - 3600
	task.NextPollAt = now - 3600
	require.NoError(t, model.DB.Create(task).Error)

	sweepOrphanedImageTasks(context.Background(), 100)

	reloaded, exists, err := model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status)
	require.Contains(t, reloaded.FailReason, "not picked up by any worker")
	require.Zero(t, reloaded.Quota)
	require.False(t, reloaded.RefundPending)

	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	require.EqualValues(t, quota, user.Quota)
}

func TestSweepOrphanedImageTasksSkipsLeasedAndFreshTasks(t *testing.T) {
	truncate(t)
	resetImageTaskOrphanSweepThrottle(t)
	oldOrphanSeconds := constant.ImageTaskOrphanFailSeconds
	oldTimeout := constant.TaskTimeoutMinutes
	constant.ImageTaskOrphanFailSeconds = 1800
	constant.TaskTimeoutMinutes = 1440
	t.Cleanup(func() {
		constant.ImageTaskOrphanFailSeconds = oldOrphanSeconds
		constant.TaskTimeoutMinutes = oldTimeout
	})

	const userID, tokenID, channelID, quota = 4102, 4102, 4102, 1500
	seedOrphanSweepBilling(t, userID, tokenID, channelID, quota)
	seedLiveInstances(t, common.NodeName)

	now := time.Now().Unix()
	leased := newOrphanImageTask("task_orphan_leased", userID, tokenID, channelID, quota)
	leased.SubmitTime = now - 3600
	leased.NextPollAt = now - 3600
	leased.LockOwner = "live-node-owner"
	leased.LockUntil = now + 120
	require.NoError(t, model.DB.Create(leased).Error)

	fresh := newOrphanImageTask("task_orphan_fresh", userID, tokenID, channelID, quota)
	fresh.SubmitTime = now - 30
	fresh.NextPollAt = now - 30
	require.NoError(t, model.DB.Create(fresh).Error)

	submitted := newOrphanImageTask("task_orphan_submitted", userID, tokenID, channelID, quota)
	submitted.Status = model.TaskStatusInProgress
	submitted.SubmitTime = now - 3600
	submitted.NextPollAt = now - 3600
	submitted.StartTime = now - 3600
	submitted.PrivateData.UpstreamTaskID = "upstream_alive"
	require.NoError(t, model.DB.Create(submitted).Error)

	sweepOrphanedImageTasks(context.Background(), 100)

	for _, id := range []int64{leased.ID, fresh.ID, submitted.ID} {
		reloaded, exists, err := model.GetTaskByID(id)
		require.NoError(t, err)
		require.True(t, exists)
		require.NotEqual(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status)
		require.EqualValues(t, quota, reloaded.Quota)
	}
}

func TestSweepOrphanedImageTasksFailsSubmittedTaskAfterExecutionTimeout(t *testing.T) {
	truncate(t)
	resetImageTaskOrphanSweepThrottle(t)
	oldOrphanSeconds := constant.ImageTaskOrphanFailSeconds
	oldTimeout := constant.TaskTimeoutMinutes
	constant.ImageTaskOrphanFailSeconds = 1800
	constant.TaskTimeoutMinutes = 60
	t.Cleanup(func() {
		constant.ImageTaskOrphanFailSeconds = oldOrphanSeconds
		constant.TaskTimeoutMinutes = oldTimeout
	})

	const userID, tokenID, channelID, quota = 4103, 4103, 4103, 2200
	seedOrphanSweepBilling(t, userID, tokenID, channelID, quota)

	now := time.Now().Unix()
	task := newOrphanImageTask("task_orphan_timeout", userID, tokenID, channelID, quota)
	task.Status = model.TaskStatusInProgress
	task.StartTime = now - 7200
	task.SubmitTime = now - 7200
	task.NextPollAt = now - 7200
	task.PrivateData.UpstreamTaskID = "upstream_stuck"
	require.NoError(t, model.DB.Create(task).Error)

	sweepOrphanedImageTasks(context.Background(), 100)

	reloaded, exists, err := model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status)
	require.Contains(t, reloaded.FailReason, "execution timeout")
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.EqualValues(t, quota, reloaded.Quota)
	require.Equal(t, "upstream_stuck", reloaded.PrivateData.UpstreamTaskID)
	require.False(t, reloaded.RefundPending)

	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	require.Zero(t, user.Quota)
}

func TestOrphanedImageTaskFailureDoesNotRefundMarkedSyncSubmission(t *testing.T) {
	truncate(t)
	resetImageTaskOrphanSweepThrottle(t)
	oldOrphanSeconds := constant.ImageTaskOrphanFailSeconds
	oldTimeout := constant.TaskTimeoutMinutes
	constant.ImageTaskOrphanFailSeconds = 1800
	constant.TaskTimeoutMinutes = 60
	t.Cleanup(func() {
		constant.ImageTaskOrphanFailSeconds = oldOrphanSeconds
		constant.TaskTimeoutMinutes = oldTimeout
	})

	const userID, tokenID, channelID, quota = 4199, 4199, 4199, 1700
	seedOrphanSweepBilling(t, userID, tokenID, channelID, quota)
	bodyPath := filepath.Join(t.TempDir(), "sync-submission-body.json")
	require.NoError(t, os.WriteFile(bodyPath, []byte(`{"model":"gpt-image-1"}`), 0o600))

	now := time.Now().Unix()
	task := newOrphanImageTask("task_orphan_sync_submission", userID, tokenID, channelID, quota)
	task.Status = model.TaskStatusInProgress
	task.SubmitTime = now - 7200
	task.StartTime = now - 7200
	task.NextPollAt = now - 7200
	task.SyncSubmissionStartedAt = now - 7100
	task.PrivateData.RequestBodyPath = bodyPath
	task.PrivateData.RequestBodySize = int64(len(`{"model":"gpt-image-1"}`))
	require.NoError(t, model.DB.Create(task).Error)

	sweepOrphanedImageTasks(context.Background(), 100)

	reloaded, exists, err := model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.Contains(t, reloaded.FailReason, "execution timeout")
	require.EqualValues(t, quota, reloaded.Quota)
	require.False(t, reloaded.RefundPending)
	require.Equal(t, bodyPath, reloaded.PrivateData.RequestBodyPath)
	require.True(t, reloaded.RequestCleanupPending)
	require.Greater(t, reloaded.RequestDeleteAfter, now)
	require.FileExists(t, bodyPath)

	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	require.Zero(t, user.Quota, "a persisted sync submission marker makes the upstream outcome unknown")
	var token model.Token
	require.NoError(t, model.DB.First(&token, tokenID).Error)
	require.Zero(t, token.RemainQuota)
	require.EqualValues(t, quota, token.UsedQuota)
}

func TestSweepOrphanedImageTasksIsDisabledWhenGraceAndTimeoutOff(t *testing.T) {
	truncate(t)
	resetImageTaskOrphanSweepThrottle(t)
	oldOrphanSeconds := constant.ImageTaskOrphanFailSeconds
	oldTimeout := constant.TaskTimeoutMinutes
	constant.ImageTaskOrphanFailSeconds = 0
	constant.TaskTimeoutMinutes = 0
	t.Cleanup(func() {
		constant.ImageTaskOrphanFailSeconds = oldOrphanSeconds
		constant.TaskTimeoutMinutes = oldTimeout
	})

	const userID, tokenID, channelID, quota = 4104, 4104, 4104, 900
	seedOrphanSweepBilling(t, userID, tokenID, channelID, quota)

	now := time.Now().Unix()
	task := newOrphanImageTask("task_orphan_disabled", userID, tokenID, channelID, quota)
	task.SubmitTime = now - 86400
	task.NextPollAt = now - 86400
	require.NoError(t, model.DB.Create(task).Error)

	sweepOrphanedImageTasks(context.Background(), 100)

	reloaded, exists, err := model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, model.TaskStatus(model.TaskStatusQueued), reloaded.Status)
	require.EqualValues(t, quota, reloaded.Quota)
}

func TestSweepOrphanedImageTasksLosesCASToConcurrentLease(t *testing.T) {
	truncate(t)
	resetImageTaskOrphanSweepThrottle(t)
	oldOrphanSeconds := constant.ImageTaskOrphanFailSeconds
	constant.ImageTaskOrphanFailSeconds = 1800
	t.Cleanup(func() { constant.ImageTaskOrphanFailSeconds = oldOrphanSeconds })

	const userID, tokenID, channelID, quota = 4105, 4105, 4105, 1200
	seedOrphanSweepBilling(t, userID, tokenID, channelID, quota)
	seedLiveInstances(t, common.NodeName)

	now := time.Now().Unix()
	task := newOrphanImageTask("task_orphan_race", userID, tokenID, channelID, quota)
	task.SubmitTime = now - 3600
	task.NextPollAt = now - 3600
	require.NoError(t, model.DB.Create(task).Error)

	// 另一个节点先拿到租约，清扫必须让出，不能把执行中的任务改成失败并退款。
	claimed, ok, err := model.ClaimTaskLease(task.ID, "other-node-owner", now, 120)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, claimed)

	sweepOrphanedImageTasks(context.Background(), 100)

	reloaded, exists, err := model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, model.TaskStatus(model.TaskStatusQueued), reloaded.Status)
	require.EqualValues(t, quota, reloaded.Quota)
	require.Equal(t, "other-node-owner", reloaded.LockOwner)
}

func TestUpdateVideoTasksDefaultSleepWaitsBetweenTasks(t *testing.T) {
	truncate(t)

	const channelID = 101
	seedTaskPollingChannel(t, channelID, false)
	first := seedPollingTask(t, channelID, "task_public_1", "upstream_1")
	second := seedPollingTask(t, channelID, "task_public_2", "upstream_2")

	adaptor := &taskPollingFetchAdaptor{}
	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := UpdateVideoTasks(ctx, constant.TaskPlatform("kling"), map[int][]string{
		channelID: {
			first.GetUpstreamTaskID(),
			second.GetUpstreamTaskID(),
		},
	}, map[string]*model.Task{
		first.GetUpstreamTaskID():  first,
		second.GetUpstreamTaskID(): second,
	})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, 1, adaptor.fetchCount())
}

func TestUpdateVideoTasksCanSkipPollingSleepPerChannel(t *testing.T) {
	truncate(t)

	const channelID = 102
	seedTaskPollingChannel(t, channelID, true)
	first := seedPollingTask(t, channelID, "task_public_3", "upstream_3")
	second := seedPollingTask(t, channelID, "task_public_4", "upstream_4")

	adaptor := &taskPollingFetchAdaptor{}
	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := UpdateVideoTasks(ctx, constant.TaskPlatform("kling"), map[int][]string{
		channelID: {
			first.GetUpstreamTaskID(),
			second.GetUpstreamTaskID(),
		},
	}, map[string]*model.Task{
		first.GetUpstreamTaskID():  first,
		second.GetUpstreamTaskID(): second,
	})

	require.NoError(t, err)
	assert.Equal(t, 2, adaptor.fetchCount())
}

func TestUpdateVideoTasksDefaultSleepDoesNotBlockOtherChannels(t *testing.T) {
	truncate(t)

	const firstChannelID = 201
	const secondChannelID = 202
	seedTaskPollingChannel(t, firstChannelID, false)
	seedTaskPollingChannel(t, secondChannelID, false)
	firstChannelFirst := seedPollingTask(t, firstChannelID, "task_public_5", "upstream_a_1")
	firstChannelSecond := seedPollingTask(t, firstChannelID, "task_public_6", "upstream_a_2")
	secondChannelFirst := seedPollingTask(t, secondChannelID, "task_public_7", "upstream_b_1")
	secondChannelSecond := seedPollingTask(t, secondChannelID, "task_public_8", "upstream_b_2")

	adaptor := &taskPollingFetchAdaptor{}
	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := UpdateVideoTasks(ctx, constant.TaskPlatform("kling"), map[int][]string{
		firstChannelID: {
			firstChannelFirst.GetUpstreamTaskID(),
			firstChannelSecond.GetUpstreamTaskID(),
		},
		secondChannelID: {
			secondChannelFirst.GetUpstreamTaskID(),
			secondChannelSecond.GetUpstreamTaskID(),
		},
	}, map[string]*model.Task{
		firstChannelFirst.GetUpstreamTaskID():   firstChannelFirst,
		firstChannelSecond.GetUpstreamTaskID():  firstChannelSecond,
		secondChannelFirst.GetUpstreamTaskID():  secondChannelFirst,
		secondChannelSecond.GetUpstreamTaskID(): secondChannelSecond,
	})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.ElementsMatch(t, []string{"upstream_a_1", "upstream_b_1"}, adaptor.fetchedTaskIDs())
}

func TestUpdateVideoTasksSlowChannelDoesNotBlockOtherChannels(t *testing.T) {
	truncate(t)

	const slowChannelID = 251
	const fastChannelID = 252
	seedTaskPollingChannel(t, slowChannelID, false)
	seedTaskPollingChannel(t, fastChannelID, true)
	slowTask := seedPollingTask(t, slowChannelID, "task_public_slow", "upstream_slow_1")
	fastFirst := seedPollingTask(t, fastChannelID, "task_public_fast_1", "upstream_fast_parallel_1")
	fastSecond := seedPollingTask(t, fastChannelID, "task_public_fast_2", "upstream_fast_parallel_2")
	slowTaskID := slowTask.GetUpstreamTaskID()
	fastFirstID := fastFirst.GetUpstreamTaskID()
	fastSecondID := fastSecond.GetUpstreamTaskID()

	adaptor := &taskPollingFetchAdaptor{
		fetched:      make(chan string, 4),
		blockTaskID:  slowTaskID,
		blockStarted: make(chan struct{}),
		releaseBlock: make(chan struct{}),
	}
	var releaseOnce sync.Once
	releaseBlockedTask := func() {
		releaseOnce.Do(func() {
			close(adaptor.releaseBlock)
		})
	}
	t.Cleanup(releaseBlockedTask)
	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })

	errCh := make(chan error, 1)
	gopool.Go(func() {
		errCh <- UpdateVideoTasks(context.Background(), constant.TaskPlatform("kling"), map[int][]string{
			slowChannelID: {
				slowTaskID,
			},
			fastChannelID: {
				fastFirstID,
				fastSecondID,
			},
		}, map[string]*model.Task{
			slowTaskID:   slowTask,
			fastFirstID:  fastFirst,
			fastSecondID: fastSecond,
		})
	})

	select {
	case <-adaptor.blockStarted:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("slow channel did not start blocking")
	}

	require.Eventually(t, func() bool {
		fetchedTaskIDs := adaptor.fetchedTaskIDs()
		return len(fetchedTaskIDs) == 2 &&
			fetchedTaskIDs[0] == fastFirstID &&
			fetchedTaskIDs[1] == fastSecondID
	}, 500*time.Millisecond, 10*time.Millisecond)

	releaseBlockedTask()
	require.NoError(t, <-errCh)
	assert.ElementsMatch(t, []string{
		slowTaskID,
		fastFirstID,
		fastSecondID,
	}, adaptor.fetchedTaskIDs())
}

func TestUpdateVideoTasksMixedChannelSleepSettings(t *testing.T) {
	truncate(t)

	const sleepyChannelID = 301
	const fastChannelID = 302
	seedTaskPollingChannel(t, sleepyChannelID, false)
	seedTaskPollingChannel(t, fastChannelID, true)
	sleepyFirst := seedPollingTask(t, sleepyChannelID, "task_public_9", "upstream_sleepy_1")
	sleepySecond := seedPollingTask(t, sleepyChannelID, "task_public_10", "upstream_sleepy_2")
	fastFirst := seedPollingTask(t, fastChannelID, "task_public_11", "upstream_fast_1")
	fastSecond := seedPollingTask(t, fastChannelID, "task_public_12", "upstream_fast_2")

	adaptor := &taskPollingFetchAdaptor{}
	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := UpdateVideoTasks(ctx, constant.TaskPlatform("kling"), map[int][]string{
		sleepyChannelID: {
			sleepyFirst.GetUpstreamTaskID(),
			sleepySecond.GetUpstreamTaskID(),
		},
		fastChannelID: {
			fastFirst.GetUpstreamTaskID(),
			fastSecond.GetUpstreamTaskID(),
		},
	}, map[string]*model.Task{
		sleepyFirst.GetUpstreamTaskID():  sleepyFirst,
		sleepySecond.GetUpstreamTaskID(): sleepySecond,
		fastFirst.GetUpstreamTaskID():    fastFirst,
		fastSecond.GetUpstreamTaskID():   fastSecond,
	})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.ElementsMatch(t, []string{"upstream_sleepy_1", "upstream_fast_1", "upstream_fast_2"}, adaptor.fetchedTaskIDs())
}

func TestUpdateVideoSingleTaskBoundsConsecutivePollFailuresAndRefunds(t *testing.T) {
	truncate(t)
	oldMaxFailures := constant.TaskPollMaxFailures
	constant.TaskPollMaxFailures = 2
	t.Cleanup(func() {
		constant.TaskPollMaxFailures = oldMaxFailures
	})

	const userID, tokenID, channelID = 3101, 3102, 3103
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, "poll-failure-token", 1000)
	channel := &model.Channel{
		Id:     channelID,
		Type:   constant.ChannelTypeKling,
		Name:   "poll-failure-channel",
		Key:    "sk-poll-failure",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, model.DB.Create(channel).Error)

	task := makeTask(userID, channelID, 100, tokenID, BillingSourceWallet, 0)
	task.TaskID = "task_poll_failure_bounded"
	task.Platform = constant.TaskPlatform("kling")
	task.PrivateData.UpstreamTaskID = "upstream_poll_failure_bounded"
	require.NoError(t, model.DB.Create(task).Error)

	adaptor := &pollOutcomeAdaptor{
		statusCode:   http.StatusBadGateway,
		responseBody: []byte(`{"error":"upstream unavailable"}`),
	}
	taskM := map[string]*model.Task{task.GetUpstreamTaskID(): task}

	require.NoError(t, updateVideoSingleTask(context.Background(), legacyTaskPollingAdaptorBridge{TaskPollingAdaptor: adaptor}, channel, task.GetUpstreamTaskID(), taskM))
	var first model.Task
	require.NoError(t, model.DB.First(&first, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), first.Status)
	require.Equal(t, 1, first.PrivateData.PollFailures)
	require.EqualValues(t, 100, first.Quota)

	require.NoError(t, updateVideoSingleTask(context.Background(), legacyTaskPollingAdaptorBridge{TaskPollingAdaptor: adaptor}, channel, task.GetUpstreamTaskID(), map[string]*model.Task{
		task.GetUpstreamTaskID(): &first,
	}))
	var final model.Task
	require.NoError(t, model.DB.First(&final, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), final.Status)
	require.Equal(t, 2, final.PrivateData.PollFailures)
	require.Zero(t, final.Quota)
	require.EqualValues(t, 1100, getUserQuota(t, userID))
}

func TestUpdateVideoSingleTaskResetsPollFailuresAfterValidStatus(t *testing.T) {
	truncate(t)

	const channelID = 3201
	seedUser(t, 3200, 1000)
	channel := &model.Channel{
		Id:     channelID,
		Type:   constant.ChannelTypeKling,
		Name:   "poll-reset-channel",
		Key:    "sk-poll-reset",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, model.DB.Create(channel).Error)
	task := makeTask(3200, channelID, 0, 0, BillingSourceWallet, 0)
	task.TaskID = "task_poll_failure_reset"
	task.Platform = constant.TaskPlatform("kling")
	task.PrivateData.UpstreamTaskID = "upstream_poll_failure_reset"
	task.PrivateData.PollFailures = 1
	require.NoError(t, model.DB.Create(task).Error)

	responseBody, err := common.Marshal(dto.TaskResponse[model.Task]{
		Code: dto.TaskSuccessCode,
		Data: model.Task{
			TaskID:   task.GetUpstreamTaskID(),
			Status:   model.TaskStatusInProgress,
			Progress: "50%",
		},
	})
	require.NoError(t, err)
	adaptor := &pollOutcomeAdaptor{
		statusCode:   http.StatusOK,
		responseBody: responseBody,
	}

	require.NoError(t, updateVideoSingleTask(context.Background(), legacyTaskPollingAdaptorBridge{TaskPollingAdaptor: adaptor}, channel, task.GetUpstreamTaskID(), map[string]*model.Task{
		task.GetUpstreamTaskID(): task,
	}))
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), reloaded.Status)
	require.Equal(t, "50%", reloaded.Progress)
	require.Zero(t, reloaded.PrivateData.PollFailures)
}

func TestUpdateVideoSingleTaskFailsImmediatelyWhenUpstreamTaskIsGone(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 3301, 3302, 3303
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, "poll-not-found-token", 1000)
	channel := &model.Channel{
		Id:     channelID,
		Type:   constant.ChannelTypeKling,
		Name:   "poll-not-found-channel",
		Key:    "sk-poll-not-found",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, model.DB.Create(channel).Error)
	task := makeTask(userID, channelID, 100, tokenID, BillingSourceWallet, 0)
	task.TaskID = "task_poll_not_found"
	task.Platform = constant.TaskPlatform("kling")
	task.PrivateData.UpstreamTaskID = "upstream_poll_not_found"
	require.NoError(t, model.DB.Create(task).Error)

	adaptor := &pollOutcomeAdaptor{
		statusCode:   http.StatusNotFound,
		responseBody: []byte(`{"error":"not found"}`),
	}
	require.NoError(t, updateVideoSingleTask(context.Background(), legacyTaskPollingAdaptorBridge{TaskPollingAdaptor: adaptor}, channel, task.GetUpstreamTaskID(), map[string]*model.Task{
		task.GetUpstreamTaskID(): task,
	}))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status)
	require.Contains(t, reloaded.FailReason, "not found")
	require.Zero(t, reloaded.Quota)
	require.EqualValues(t, 1100, getUserQuota(t, userID))
}

func TestUpdateVideoSingleTaskFailureWithUsageRefundsInsteadOfSettling(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 3401, 3402, 3403
	seedUser(t, userID, 1000)
	seedToken(t, tokenID, userID, "poll-failure-usage-token", 1000)
	channel := &model.Channel{
		Id:     channelID,
		Type:   constant.ChannelTypeKling,
		Name:   "poll-failure-usage-channel",
		Key:    "sk-poll-failure-usage",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, model.DB.Create(channel).Error)

	task := makeTask(userID, channelID, 100, tokenID, BillingSourceWallet, 0)
	task.TaskID = "task_poll_failure_usage"
	task.Platform = constant.TaskPlatform("kling")
	task.PrivateData.UpstreamTaskID = "upstream_poll_failure_usage"
	require.NoError(t, model.DB.Create(task).Error)

	adaptor := &pollOutcomeAdaptor{
		statusCode:   http.StatusOK,
		responseBody: []byte(`{"status":"FAILURE"}`),
		parseResult: &relaycommon.TaskInfo{
			Status:           model.TaskStatusFailure,
			Reason:           "provider rejected task",
			TotalTokens:      100,
			CompletionTokens: 50,
		},
		adjustReturn: 10,
	}
	require.NoError(t, updateVideoSingleTask(context.Background(), legacyTaskPollingAdaptorBridge{TaskPollingAdaptor: adaptor}, channel, task.GetUpstreamTaskID(), map[string]*model.Task{
		task.GetUpstreamTaskID(): task,
	}))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status)
	require.Zero(t, reloaded.Quota)
	require.EqualValues(t, 1100, getUserQuota(t, userID))
	require.EqualValues(t, 1100, getTokenRemainQuota(t, tokenID))
}
