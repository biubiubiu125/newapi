package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
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

func seedOrphanSweepBilling(t *testing.T, userID int, tokenID int, channelID int, quota int) {
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
	require.Equal(t, quota, reloaded.Quota)
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
		require.Equal(t, quota, reloaded.Quota)
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
	require.Equal(t, quota, reloaded.Quota)
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
	require.Equal(t, quota, user.Quota)
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
	require.Equal(t, quota, user.Quota)
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
		require.Equal(t, quota, reloaded.Quota)
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
	require.Equal(t, quota, reloaded.Quota)
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
	require.Equal(t, quota, reloaded.Quota)
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
	require.Equal(t, quota, token.UsedQuota)
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
	require.Equal(t, quota, reloaded.Quota)
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
	require.Equal(t, quota, reloaded.Quota)
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
