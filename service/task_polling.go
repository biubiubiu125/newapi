package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/samber/lo"
)

// TaskPollingAdaptor 定义轮询所需的最小适配器接口，避免 service -> relay 的循环依赖
type TaskPollingAdaptor interface {
	Init(info *relaycommon.RelayInfo)
	FetchTask(baseURL string, key string, body map[string]any, proxy string) (*http.Response, error)
	ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error)
	// AdjustBillingOnComplete 在任务到达终态（成功/失败）时由轮询循环调用。
	// 返回正数触发差额结算（补扣/退还），返回 0 保持预扣费金额不变。
	AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int
}

// GetTaskAdaptorFunc 由 main 包注入，用于获取指定平台的任务适配器。
// 打破 service -> relay -> relay/channel -> service 的循环依赖。
var GetTaskAdaptorFunc func(platform constant.TaskPlatform) TaskPollingAdaptor
var RunImageTasksFunc func(ctx context.Context, tasks []*model.Task) error
var imageTaskResultCacheCleanupUnix int64
var imageTaskPollWakeupMu sync.Mutex
var imageTaskPollWakeupTimer *time.Timer
var imageTaskPollWakeupAt int64
var imageTaskWorkerRunnerOnce sync.Once
var imageTaskWorkerWakeup = make(chan struct{}, 1)
var imageTaskSharedCacheValidationUnix int64
var imageTaskSharedCacheValidationMu sync.Mutex

const (
	defaultImageTaskWorkerIdleInterval = 5 * time.Second
	imageTaskWorkerRestartDelay        = 5 * time.Second
	imageTaskBatchPollMaxSize          = 100
)

type imageTaskLeaseOwnerContextKey struct{}
type imageTaskLeaseOwnersContextKey struct{}

func ContextWithImageTaskLeaseOwner(ctx context.Context, owner string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return ctx
	}
	return context.WithValue(ctx, imageTaskLeaseOwnerContextKey{}, owner)
}

func ContextWithImageTaskLeaseOwners(ctx context.Context, owners map[int64]string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(owners) == 0 {
		return ctx
	}
	copied := make(map[int64]string, len(owners))
	for taskID, owner := range owners {
		owner = strings.TrimSpace(owner)
		if taskID <= 0 || owner == "" {
			continue
		}
		copied[taskID] = owner
	}
	if len(copied) == 0 {
		return ctx
	}
	return context.WithValue(ctx, imageTaskLeaseOwnersContextKey{}, copied)
}

func ImageTaskLeaseOwnerFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	owner, _ := ctx.Value(imageTaskLeaseOwnerContextKey{}).(string)
	return strings.TrimSpace(owner)
}

func ImageTaskLeaseOwnerForTaskFromContext(ctx context.Context, taskPrimaryID int64) string {
	if ctx == nil {
		return ""
	}
	if taskPrimaryID > 0 {
		if owners, ok := ctx.Value(imageTaskLeaseOwnersContextKey{}).(map[int64]string); ok {
			if owner := strings.TrimSpace(owners[taskPrimaryID]); owner != "" {
				return owner
			}
		}
	}
	return ImageTaskLeaseOwnerFromContext(ctx)
}

func cleanupExpiredImageTaskResultCache(ctx context.Context) {
	now := time.Now().Unix()
	last := atomic.LoadInt64(&imageTaskResultCacheCleanupUnix)
	if now-last < int64((10 * time.Minute).Seconds()) {
		return
	}
	if !atomic.CompareAndSwapInt64(&imageTaskResultCacheCleanupUnix, last, now) {
		return
	}
	bodyCandidates, err := common.GetExpiredImageTaskBodyCachePaths(common.GetImageTaskBodyCacheRetention())
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("image task cache cleanup skipped: scan body cache failed: %v", err))
		return
	}
	resultCandidates, err := common.GetExpiredImageTaskResultCachePaths(common.GetImageTaskResultCacheRetention())
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("image task cache cleanup skipped: scan result cache failed: %v", err))
		return
	}
	bodyKeepPaths, resultKeepPaths, err := model.GetOpenImageTaskCachePathsForCandidates(bodyCandidates, resultCandidates, 1000)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("image task cache cleanup skipped: load referenced paths failed: %v", err))
		return
	}
	if err := common.CleanupExpiredImageTaskBodyCacheFiles(bodyKeepPaths); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("image task body cache cleanup failed: %v", err))
	}
	if err := common.CleanupExpiredImageTaskResultCacheFilesWithKeep(resultKeepPaths); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("image task result cache cleanup failed: %v", err))
	}
}

func StartImageTaskWorkerRunner() {
	imageTaskWorkerRunnerOnce.Do(func() {
		if !constant.ImageTaskWorkerEnabled {
			return
		}
		runnerID := fmt.Sprintf("%s-image-worker-%s", common.NodeName, common.GetRandomString(8))
		go func() {
			for {
				runImageTaskWorkerLoop(runnerID, imageTaskWorkerIdleInterval())
				time.Sleep(imageTaskWorkerRestartDelay)
			}
		}()
	})
}

func runImageTaskWorkerLoop(runnerID string, idleInterval time.Duration) {
	defer func() {
		if r := recover(); r != nil {
			logger.LogError(context.Background(), fmt.Sprintf("image task worker runner panic: runner=%s panic=%v\n%s", runnerID, r, string(debug.Stack())))
		}
	}()
	logger.LogInfo(context.Background(), fmt.Sprintf("image task worker started: runner=%s idle_interval=%s", runnerID, idleInterval))
	ticker := time.NewTicker(idleInterval)
	defer ticker.Stop()
	runImageTaskWorkerPass(context.Background())
	for {
		select {
		case <-ticker.C:
		case <-imageTaskWorkerWakeup:
		}
		runImageTaskWorkerPass(context.Background())
	}
}

func ImageTaskWorkerEnabled() bool {
	return constant.ImageTaskWorkerEnabled && RunImageTasksFunc != nil
}

func NotifyImageTaskWorker() {
	select {
	case imageTaskWorkerWakeup <- struct{}{}:
	default:
	}
}

func NotifyImageTaskQueued(ctx context.Context) {
	NotifyImageTaskWorker()
	if ImageTaskWorkerEnabled() {
		return
	}
	enqueueImageTaskPoll(ctx)
}

func EnsureImageTaskSharedCacheReady(ctx context.Context) bool {
	if !constant.ImageTaskFileCacheShared {
		return true
	}
	imageTaskSharedCacheValidationMu.Lock()
	defer imageTaskSharedCacheValidationMu.Unlock()

	now := time.Now().Unix()
	last := atomic.LoadInt64(&imageTaskSharedCacheValidationUnix)
	if now-last < 60 {
		return !common.ImageTaskSharedCacheDisabled()
	}
	atomic.StoreInt64(&imageTaskSharedCacheValidationUnix, now)
	if err := common.ValidateImageTaskSharedCache(); err != nil {
		common.SetImageTaskSharedCacheDisabled(true)
		logger.LogWarn(ctx, fmt.Sprintf("image task shared file cache disabled: %v", err))
		return false
	}
	common.SetImageTaskSharedCacheDisabled(false)
	return true
}

func ImageTaskFileCacheSharedEnabled() bool {
	return constant.ImageTaskFileCacheShared && !common.ImageTaskSharedCacheDisabled()
}

func ImageTaskFileCacheSharedTrusted() bool {
	return ImageTaskFileCacheSharedEnabled() && constant.ImageTaskFileCacheSharedTrusted
}

func ImageTaskLocalFileCacheAffinityEnabled() bool {
	return !constant.ImageTaskFileCacheShared &&
		constant.ImageTaskLocalFileCacheAffinity &&
		strings.TrimSpace(common.NodeName) != ""
}

func ImageTaskRequestBodyBase64FallbackEnabled() bool {
	if ImageTaskFileCacheSharedTrusted() {
		return false
	}
	if constant.ImageTaskFileCacheShared {
		return true
	}
	return !ImageTaskLocalFileCacheAffinityEnabled()
}

func ImageTaskRequestBodyBase64MaxBytes() int64 {
	maxMB := constant.ImageTaskRequestBodyBase64MaxMB
	if maxMB <= 0 {
		maxMB = 16
	}
	return int64(maxMB) << 20
}

func ValidateImageTaskRequestBodyBase64Size(size int64) error {
	if !ImageTaskRequestBodyBase64FallbackEnabled() || size <= 0 {
		return nil
	}
	maxBytes := ImageTaskRequestBodyBase64MaxBytes()
	if maxBytes <= 0 || size <= maxBytes {
		return nil
	}
	maxMB := maxBytes >> 20
	return fmt.Errorf("%w: image task portable request body exceeds %d MB; enable trusted shared file cache, local file affinity, or raise IMAGE_TASK_REQUEST_BODY_BASE64_MAX_MB", common.ErrRequestBodyTooLarge, maxMB)
}

func ImageTaskResultFileCacheSharedEnabled() bool {
	return ImageTaskFileCacheSharedTrusted()
}

func runImageTaskWorkerPass(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.LogError(ctx, fmt.Sprintf("image task worker panic: %v\n%s", r, string(debug.Stack())))
		}
	}()
	if ctx == nil {
		ctx = context.Background()
	}
	if !constant.UpdateTask || RunImageTasksFunc == nil {
		return
	}
	EnsureImageTaskSharedCacheReady(ctx)
	cleanupExpiredImageTaskResultCache(ctx)
	for ctx.Err() == nil {
		limit := imageTaskWorkerQueryLimit()
		tasks := model.GetRunnableImageTasks(limit, time.Now().Unix())
		if len(tasks) == 0 {
			return
		}
		DispatchImageTasks(ctx, tasks)
		if len(tasks) < limit {
			return
		}
	}
}

func imageTaskWorkerIdleInterval() time.Duration {
	seconds := constant.ImageTaskWorkerIdleSeconds
	if seconds <= 0 {
		return defaultImageTaskWorkerIdleInterval
	}
	return time.Duration(seconds) * time.Second
}

func imageTaskWorkerQueryLimit() int {
	workerLimit := imageTaskWorkerConcurrency()
	limit := constant.TaskQueryLimit
	if limit <= 0 {
		limit = workerLimit
	}
	maxBatch := workerLimit * 4
	if maxBatch < workerLimit {
		maxBatch = workerLimit
	}
	if limit > maxBatch {
		limit = maxBatch
	}
	if limit < workerLimit {
		limit = workerLimit
	}
	return limit
}

// sweepTimedOutTasks 在主轮询之前独立清理超时任务。
// 每次最多处理 100 条，剩余的下个周期继续处理。
// 使用 per-task CAS (UpdateWithStatus) 防止覆盖被正常轮询已推进的任务。
func sweepTimedOutTasks(ctx context.Context) {
	if constant.TaskTimeoutMinutes <= 0 {
		return
	}
	cutoff := time.Now().Unix() - int64(constant.TaskTimeoutMinutes)*60
	tasks := model.GetTimedOutUnfinishedTasks(cutoff, 100)
	if len(tasks) == 0 {
		return
	}

	const legacyTaskCutoff int64 = 1740182400 // 2026-02-22 00:00:00 UTC
	reason := fmt.Sprintf("任务超时（%d分钟）", constant.TaskTimeoutMinutes)
	legacyReason := "任务超时（旧系统遗留任务，不进行退款，请联系管理员）"
	now := time.Now().Unix()
	timedOutCount := 0

	for _, task := range tasks {
		if !shouldSweepTimedOutTask(task) {
			continue
		}
		isLegacy := task.SubmitTime > 0 && task.SubmitTime < legacyTaskCutoff

		oldStatus := task.Status
		oldLockOwner := task.LockOwner
		bodyPath, resultPath := prepareTimedOutTaskFailure(task, now, timeoutReason(isLegacy, reason, legacyReason))

		won, err := task.UpdateWithStatus(oldStatus)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("sweepTimedOutTasks CAS update error for task %s: %v", task.TaskID, err))
			continue
		}
		if !won {
			logger.LogInfo(ctx, fmt.Sprintf("sweepTimedOutTasks: task %s already transitioned, skip", task.TaskID))
			continue
		}
		timedOutCount++
		if !isLegacy && task.Quota != 0 {
			RefundTaskQuota(ctx, task, reason)
		}
		if task.Platform == constant.TaskPlatformImage {
			removeImageTaskCachedFile(bodyPath)
			removeImageTaskCachedFile(resultPath)
			if oldLockOwner != "" {
				_ = model.ReleaseImageTaskChannelLease(oldLockOwner)
			}
		}
	}

	if timedOutCount > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("sweepTimedOutTasks: timed out %d tasks", timedOutCount))
	}
}

func shouldSweepTimedOutTask(task *model.Task) bool {
	if task == nil {
		return false
	}
	return task.Platform != constant.TaskPlatformImage
}

func timeoutReason(isLegacy bool, reason string, legacyReason string) string {
	if isLegacy {
		return legacyReason
	}
	return reason
}

func prepareTimedOutTaskFailure(task *model.Task, now int64, reason string) (string, string) {
	if task == nil {
		return "", ""
	}
	var bodyPath, resultPath string
	if task.Platform == constant.TaskPlatformImage {
		bodyPath = strings.TrimSpace(task.PrivateData.RequestBodyPath)
		resultPath = strings.TrimSpace(task.PrivateData.ResultBodyPath)
		task.PrivateData.RequestBodyPath = ""
		task.PrivateData.RequestBodyBase64 = ""
		task.PrivateData.RequestBodyPortable = false
		task.PrivateData.ResultBodyPath = ""
		task.PrivateData.ResultBodySize = 0
		task.PrivateData.ResultBodySHA256 = ""
		task.PrivateData.ResultContentType = ""
		task.PrivateData.ResultStoredAt = 0
		task.PrivateData.ResultExpiresAt = 0
		task.PrivateData.UpstreamSubmitUncertainAt = 0
		task.PrivateData.UpstreamSubmitUncertainCount = 0
		task.SettlementStatus = ""
	}
	task.Status = model.TaskStatusFailure
	task.Progress = "100%"
	task.FinishTime = now
	task.FailReason = reason
	task.LockOwner = ""
	task.LockUntil = 0
	task.RetryCount = 0
	return bodyPath, resultPath
}

func removeImageTaskCachedFile(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	_ = common.RemoveDiskCacheFile(path)
}

type TaskPollSummary struct {
	UnfinishedTasks  int `json:"unfinished_tasks"`
	PlatformsScanned int `json:"platforms_scanned"`
	NullTasksFailed  int `json:"null_tasks_failed"`
}

func RunTaskPollingOnce(ctx context.Context, report func(processed, total int)) TaskPollSummary {
	summary := TaskPollSummary{}
	if ctx == nil {
		ctx = context.Background()
	}
	cleanupExpiredImageTaskResultCache(ctx)
	if GetTaskAdaptorFunc == nil && RunImageTasksFunc == nil {
		return summary
	}

	EnsureImageTaskSharedCacheReady(ctx)

	common.SysLog("任务进度轮询开始")
	sweepTimedOutTasks(ctx)
	allTasks := getRunnableTasksForSystemPolling(time.Now().Unix())
	summary.UnfinishedTasks = len(allTasks)
	platformTask := make(map[constant.TaskPlatform][]*model.Task)
	for _, t := range allTasks {
		platformTask[t.Platform] = append(platformTask[t.Platform], t)
	}

	platforms := taskPollingPlatformOrder(platformTask)
	totalPlatforms := len(platforms)
	processedPlatforms := 0
	var dispatchWG sync.WaitGroup
	for _, platform := range platforms {
		tasks := platformTask[platform]
		if ctx.Err() != nil {
			break
		}
		if report != nil {
			report(processedPlatforms, totalPlatforms)
		}
		processedPlatforms++
		if len(tasks) == 0 {
			continue
		}
		summary.PlatformsScanned++
		taskChannelM := make(map[int][]string)
		taskM := make(map[string]*model.Task)
		nullTaskIds := make([]int64, 0)
		for _, task := range tasks {
			upstreamID := task.GetUpstreamTaskID()
			if upstreamID == "" {
				nullTaskIds = append(nullTaskIds, task.ID)
				continue
			}
			taskM[upstreamID] = task
			taskChannelM[task.ChannelId] = append(taskChannelM[task.ChannelId], upstreamID)
		}
		if len(nullTaskIds) > 0 {
			summary.NullTasksFailed += len(nullTaskIds)
			err := model.TaskBulkUpdateByID(nullTaskIds, map[string]any{
				"status":   "FAILURE",
				"progress": "100%",
			})
			if err != nil {
				logger.LogError(ctx, fmt.Sprintf("Fix null task_id task error: %v", err))
			} else {
				logger.LogInfo(ctx, fmt.Sprintf("Fix null task_id task success: %v", nullTaskIds))
			}
		}
		if len(taskChannelM) == 0 {
			continue
		}

		if platform == constant.TaskPlatformImage {
			dispatchWG.Add(1)
			go func(taskChannelM map[int][]string, taskM map[string]*model.Task) {
				defer dispatchWG.Done()
				DispatchPlatformUpdate(ctx, constant.TaskPlatformImage, taskChannelM, taskM)
			}(taskChannelM, taskM)
			continue
		}
		DispatchPlatformUpdate(ctx, platform, taskChannelM, taskM)
	}
	dispatchWG.Wait()
	if report != nil && ctx.Err() == nil {
		report(totalPlatforms, totalPlatforms)
	}
	common.SysLog("任务进度轮询完成")
	return summary
}

func getRunnableTasksForSystemPolling(now int64) []*model.Task {
	if ImageTaskWorkerEnabled() {
		return model.GetRunnableNonImageSyncTasks(constant.TaskQueryLimit)
	}
	return model.GetRunnableUnfinishedSyncTasks(constant.TaskQueryLimit, now)
}

func taskPollingPlatformOrder(platformTask map[constant.TaskPlatform][]*model.Task) []constant.TaskPlatform {
	if len(platformTask) == 0 {
		return nil
	}
	platforms := make([]constant.TaskPlatform, 0, len(platformTask))
	if _, ok := platformTask[constant.TaskPlatformImage]; ok {
		platforms = append(platforms, constant.TaskPlatformImage)
	}
	remaining := make([]constant.TaskPlatform, 0, len(platformTask))
	for platform := range platformTask {
		if platform == constant.TaskPlatformImage {
			continue
		}
		remaining = append(remaining, platform)
	}
	sort.Slice(remaining, func(i, j int) bool {
		return string(remaining[i]) < string(remaining[j])
	})
	platforms = append(platforms, remaining...)
	return platforms
}

// DispatchPlatformUpdate 按平台分发轮询更新
func DispatchPlatformUpdate(ctx context.Context, platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*model.Task) {
	if ctx == nil {
		ctx = context.Background()
	}
	if platform != constant.TaskPlatformImage && GetTaskAdaptorFunc == nil {
		common.SysLog("GetTaskAdaptorFunc is nil")
		return
	}
	switch platform {
	case constant.TaskPlatformImage:
		tasks := make([]*model.Task, 0, len(taskM))
		seen := make(map[int64]struct{}, len(taskM))
		for _, task := range taskM {
			if task == nil {
				continue
			}
			if _, ok := seen[task.ID]; ok {
				continue
			}
			seen[task.ID] = struct{}{}
			tasks = append(tasks, task)
		}
		DispatchImageTasks(ctx, tasks)
	case constant.TaskPlatformMidjourney:
		// MJ 轮询由其自身处理，这里预留入口
	case constant.TaskPlatformSuno:
		_ = UpdateSunoTasks(ctx, taskChannelM, taskM)
	default:
		if err := UpdateVideoTasks(ctx, platform, taskChannelM, taskM); err != nil {
			common.SysLog(fmt.Sprintf("UpdateVideoTasks fail: %s", err))
		}
	}
}

// UpdateSunoTasks 按渠道更新所有 Suno 任务
type leasedImageTask struct {
	task             *model.Task
	owner            string
	status           model.TaskStatus
	progress         string
	upstreamID       string
	retryCount       int
	settlementStatus string
	channelLease     bool
}

type imageTaskDispatchDone struct {
	channelID        int
	taskCount        int
	channelSaturated bool
}

func DispatchImageTasks(ctx context.Context, tasks []*model.Task) {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(tasks) == 0 {
		return
	}
	if RunImageTasksFunc == nil {
		common.SysLog("RunImageTasksFunc is nil")
		return
	}

	workerCount := imageTaskWorkerConcurrency()
	if workerCount > len(tasks) {
		workerCount = len(tasks)
	}
	channelLimit := imageTaskChannelConcurrency()
	pending := append([]*model.Task(nil), tasks...)
	activeByChannel := make(map[int]int)
	saturatedChannels := make(map[int]struct{})
	activeTotal := 0
	doneCh := make(chan imageTaskDispatchDone, len(tasks))

	startReady := func() {
		if ctx.Err() != nil {
			return
		}
		for activeTotal < workerCount && len(pending) > 0 {
			progressed := false
			for i := 0; i < len(pending) && activeTotal < workerCount; i++ {
				task := pending[i]
				if task == nil || task.Platform != constant.TaskPlatformImage {
					pending = append(pending[:i], pending[i+1:]...)
					i--
					progressed = true
					continue
				}
				if _, saturated := saturatedChannels[task.ChannelId]; saturated {
					continue
				}
				if channelLimit > 0 && activeByChannel[task.ChannelId] >= channelLimit {
					continue
				}
				maxBatch := workerCount - activeTotal
				if channelLimit > 0 {
					channelRemaining := channelLimit - activeByChannel[task.ChannelId]
					if channelRemaining < maxBatch {
						maxBatch = channelRemaining
					}
				}
				batch := takeImageTaskDispatchBatch(&pending, i, maxBatch)
				if len(batch) == 0 {
					continue
				}
				i--
				activeByChannel[task.ChannelId] += len(batch)
				activeTotal += len(batch)
				progressed = true
				go func(batch []*model.Task) {
					channelSaturated := false
					defer func() {
						channelID := 0
						taskCount := len(batch)
						if r := recover(); r != nil {
							taskID := ""
							if len(batch) > 0 && batch[0] != nil {
								taskID = batch[0].TaskID
								channelID = batch[0].ChannelId
							}
							logger.LogError(ctx, fmt.Sprintf("image task dispatch panic: task=%s channel=%d panic=%v\n%s", taskID, channelID, r, string(debug.Stack())))
						}
						if len(batch) > 0 && batch[0] != nil {
							channelID = batch[0].ChannelId
						}
						doneCh <- imageTaskDispatchDone{channelID: channelID, taskCount: taskCount, channelSaturated: channelSaturated}
					}()
					channelSaturated = processImageTaskDispatchBatch(ctx, batch)
				}(batch)
			}
			if !progressed {
				break
			}
		}
	}

	startReady()
	for activeTotal > 0 {
		done := <-doneCh
		if done.taskCount <= 0 {
			done.taskCount = 1
		}
		if activeByChannel[done.channelID] > 0 {
			activeByChannel[done.channelID] -= done.taskCount
			if activeByChannel[done.channelID] < 0 {
				activeByChannel[done.channelID] = 0
			}
		}
		if done.channelSaturated && done.channelID > 0 {
			saturatedChannels[done.channelID] = struct{}{}
			backoffPendingImageTasksForSaturatedChannel(ctx, pending, done.channelID)
		}
		activeTotal -= done.taskCount
		if activeTotal < 0 {
			activeTotal = 0
		}
		startReady()
	}
}

func takeImageTaskDispatchBatch(pending *[]*model.Task, index int, maxBatch int) []*model.Task {
	if pending == nil || index < 0 || index >= len(*pending) || maxBatch <= 0 {
		return nil
	}
	task := (*pending)[index]
	*pending = append((*pending)[:index], (*pending)[index+1:]...)
	batch := []*model.Task{task}
	if maxBatch <= 1 || !imageTaskDispatchBatchable(task) {
		return batch
	}
	if configuredBatch := imageTaskBatchPollSize(); configuredBatch > 0 && maxBatch > configuredBatch {
		maxBatch = configuredBatch
	}
	key := imageTaskDispatchBatchKey(task)
	for i := 0; i < len(*pending) && len(batch) < maxBatch; i++ {
		candidate := (*pending)[i]
		if !imageTaskDispatchBatchable(candidate) || imageTaskDispatchBatchKey(candidate) != key {
			continue
		}
		batch = append(batch, candidate)
		*pending = append((*pending)[:i], (*pending)[i+1:]...)
		i--
	}
	return batch
}

func imageTaskDispatchBatchable(task *model.Task) bool {
	if task == nil || task.Platform != constant.TaskPlatformImage {
		return false
	}
	if task.PrivateData.ImageTaskMode != dto.ImageTaskModeGPTImage2APIAsync {
		return false
	}
	if strings.TrimSpace(task.PrivateData.UpstreamTaskID) == "" {
		return false
	}
	switch task.Status {
	case model.TaskStatusQueued, model.TaskStatusSubmitted, model.TaskStatusInProgress:
		return imageTaskBatchPollSize() > 1
	default:
		return false
	}
}

func imageTaskDispatchBatchKey(task *model.Task) string {
	if task == nil {
		return ""
	}
	key := strings.TrimSpace(task.PrivateData.Key)
	return fmt.Sprintf("%d:%s", task.ChannelId, key)
}

func backoffPendingImageTasksForSaturatedChannel(ctx context.Context, pending []*model.Task, channelID int) {
	if channelID <= 0 || len(pending) == 0 {
		return
	}
	nextPollAt := time.Now().Unix() + imageTaskChannelSaturationBackoffSeconds()
	ids := make([]int64, 0, len(pending))
	for _, task := range pending {
		if task == nil || task.Platform != constant.TaskPlatformImage || task.ChannelId != channelID || task.ID <= 0 {
			continue
		}
		task.NextPollAt = nextPollAt
		ids = append(ids, task.ID)
	}
	if len(ids) == 0 {
		return
	}
	if err := model.BackoffUnlockedImageTasksByIDs(ids, nextPollAt); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("backoff saturated image task channel %d failed: %v", channelID, err))
	}
}

func processImageTaskDispatch(ctx context.Context, task *model.Task) bool {
	return processImageTaskDispatchBatch(ctx, []*model.Task{task})
}

func processImageTaskDispatchBatch(ctx context.Context, tasks []*model.Task) bool {
	if ctx.Err() != nil || len(tasks) == 0 {
		return false
	}
	leasedItems := make([]leasedImageTask, 0, len(tasks))
	channelSaturated := false
	for i, task := range tasks {
		if ctx.Err() != nil || task == nil || task.Platform != constant.TaskPlatformImage {
			continue
		}
		item, ok, saturated := claimImageTask(ctx, task)
		if saturated {
			channelSaturated = true
			backoffUnclaimedImageTaskBatch(ctx, tasks[i:])
			break
		}
		if !ok {
			continue
		}
		leasedItems = append(leasedItems, item)
	}
	if len(leasedItems) == 0 {
		return channelSaturated
	}
	owners := make(map[int64]string, len(leasedItems))
	runnableTasks := make([]*model.Task, 0, len(leasedItems))
	for _, item := range leasedItems {
		if item.task == nil {
			continue
		}
		owners[item.task.ID] = item.owner
		runnableTasks = append(runnableTasks, item.task)
	}
	baseCtx := ContextWithImageTaskLeaseOwners(ctx, owners)
	if len(leasedItems) == 1 {
		baseCtx = ContextWithImageTaskLeaseOwner(baseCtx, leasedItems[0].owner)
	}
	runCtx, cancel := context.WithCancel(baseCtx)
	defer cancel()
	stops := make([]func(), 0, len(leasedItems))
	for _, item := range leasedItems {
		stops = append(stops, startImageTaskLeaseHeartbeat(runCtx, cancel, item))
	}
	defer func() {
		for _, stop := range stops {
			if stop != nil {
				stop()
			}
		}
		for _, item := range leasedItems {
			releaseLeasedImageTask(ctx, item)
		}
	}()
	if err := RunImageTasksFunc(runCtx, runnableTasks); err != nil {
		logger.LogError(ctx, fmt.Sprintf("RunImageTasks fail: %s", err))
	}
	return channelSaturated
}

func backoffUnclaimedImageTaskBatch(ctx context.Context, tasks []*model.Task) {
	if len(tasks) == 0 {
		return
	}
	nextPollAt := time.Now().Unix() + imageTaskChannelSaturationBackoffSeconds()
	ids := make([]int64, 0, len(tasks))
	for _, task := range tasks {
		if task == nil || task.ID <= 0 {
			continue
		}
		task.NextPollAt = nextPollAt
		ids = append(ids, task.ID)
	}
	if len(ids) == 0 {
		return
	}
	if err := model.BackoffUnlockedImageTasksByIDs(ids, nextPollAt); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("backoff saturated image task batch failed: %v", err))
	}
}

func startImageTaskLeaseHeartbeat(ctx context.Context, cancel context.CancelFunc, item leasedImageTask) func() {
	done := make(chan struct{})
	var stopOnce sync.Once
	leaseSeconds := imageTaskLeaseSeconds()
	interval := time.Duration(leaseSeconds) * time.Second / 3
	if interval < time.Second {
		interval = time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				now := time.Now().Unix()
				renewed, err := model.RenewTaskLease(item.task.ID, item.owner, now, leaseSeconds)
				if err != nil {
					logger.LogWarn(ctx, fmt.Sprintf("renew image task %s lease failed: %v", item.task.TaskID, err))
					cancel()
					return
				}
				if !renewed {
					logger.LogWarn(ctx, fmt.Sprintf("image task %s lease lost", item.task.TaskID))
					cancel()
					return
				}
				if item.channelLease {
					renewed, err = model.RenewImageTaskChannelLease(item.owner, now, leaseSeconds)
					if err != nil {
						logger.LogWarn(ctx, fmt.Sprintf("renew image task %s channel lease failed: %v", item.task.TaskID, err))
						cancel()
						return
					}
					if !renewed {
						logger.LogWarn(ctx, fmt.Sprintf("image task %s channel lease lost", item.task.TaskID))
						cancel()
						return
					}
				}
			}
		}
	}()
	return func() {
		stopOnce.Do(func() {
			close(done)
		})
	}
}

func ScheduleNextImageTaskPollWakeup(ctx context.Context) {
	if !constant.UpdateTask {
		return
	}
	now := time.Now().Unix()
	if model.HasRunnableImageTasks(now) {
		NotifyImageTaskWorker()
		if !ImageTaskWorkerEnabled() {
			enqueueImageTaskPoll(ctx)
		}
		return
	}
	nextAt, ok := model.GetNextRunnableImageTaskAt(now)
	if !ok {
		return
	}
	scheduleImageTaskPollWakeupAt(nextAt)
}

func scheduleImageTaskPollWakeupAt(nextAt int64) {
	if nextAt <= 0 {
		return
	}
	imageTaskPollWakeupMu.Lock()
	defer imageTaskPollWakeupMu.Unlock()
	if imageTaskPollWakeupTimer != nil && imageTaskPollWakeupAt > 0 && imageTaskPollWakeupAt <= nextAt {
		return
	}
	if imageTaskPollWakeupTimer != nil {
		imageTaskPollWakeupTimer.Stop()
	}
	delay := time.Until(time.Unix(nextAt, 0))
	if delay < 0 {
		delay = 0
	}
	imageTaskPollWakeupAt = nextAt
	imageTaskPollWakeupTimer = time.AfterFunc(delay, func() {
		runImageTaskPollWakeup(nextAt)
	})
}

func runImageTaskPollWakeup(expectedAt int64) {
	imageTaskPollWakeupMu.Lock()
	if imageTaskPollWakeupAt != expectedAt {
		imageTaskPollWakeupMu.Unlock()
		return
	}
	imageTaskPollWakeupAt = 0
	imageTaskPollWakeupTimer = nil
	imageTaskPollWakeupMu.Unlock()

	ScheduleNextImageTaskPollWakeup(context.Background())
}

func enqueueImageTaskPoll(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, _, err := EnqueueSystemTask(model.SystemTaskTypeAsyncTaskPoll, nil); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("enqueue image task polling wakeup failed: %s", err.Error()))
	}
	NotifyImageTaskWorker()
}

func claimImageTask(ctx context.Context, task *model.Task) (leasedImageTask, bool, bool) {
	var empty leasedImageTask
	if ctx.Err() != nil || task == nil || task.Platform != constant.TaskPlatformImage {
		return empty, false, false
	}
	now := time.Now().Unix()
	owner := imageTaskLeaseOwner()
	leaseSeconds := imageTaskLeaseSeconds()
	claimed, ok, err := model.ClaimTaskLease(task.ID, owner, now, leaseSeconds)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("claim image task %s failed: %v", task.TaskID, err))
		return empty, false, false
	}
	if !ok || claimed == nil {
		return empty, false, false
	}
	channelLease := false
	channelLimit := imageTaskChannelConcurrency()
	if channelLimit > 0 && claimed.ChannelId > 0 {
		acquired, err := model.TryAcquireImageTaskChannelLease(claimed.ChannelId, claimed.ID, owner, now, leaseSeconds, channelLimit)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("claim image task %s channel lease failed: %v", task.TaskID, err))
			_ = model.ReleaseTaskLease(claimed.ID, owner, now+imageTaskChannelSaturationBackoffSeconds(), claimed.RetryCount)
			return empty, false, true
		}
		if !acquired {
			_ = model.ReleaseTaskLease(claimed.ID, owner, now+imageTaskChannelSaturationBackoffSeconds(), claimed.RetryCount)
			return empty, false, true
		}
		channelLease = true
	}
	return leasedImageTask{
		task:             claimed,
		owner:            owner,
		status:           claimed.Status,
		progress:         claimed.Progress,
		upstreamID:       claimed.PrivateData.UpstreamTaskID,
		retryCount:       claimed.RetryCount,
		settlementStatus: claimed.SettlementStatus,
		channelLease:     channelLease,
	}, true, false
}

func releaseLeasedImageTask(ctx context.Context, item leasedImageTask) {
	if item.task == nil || item.owner == "" {
		return
	}
	if item.channelLease {
		defer func() {
			if err := model.ReleaseImageTaskChannelLease(item.owner); err != nil {
				logger.LogWarn(ctx, fmt.Sprintf("release image task %s channel lease failed: %v", item.task.TaskID, err))
			}
		}()
	}
	current, exist, err := model.GetTaskByID(item.task.ID)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("reload image task %s before lease release failed: %v", item.task.TaskID, err))
		return
	}
	if !exist || current == nil {
		return
	}
	retryCount := current.RetryCount
	if item.task.RetryCount > retryCount {
		retryCount = item.task.RetryCount
	}
	if imageTaskProgressed(item, current) && !imageTaskProgressedIntoRetryableSettlement(item, current) {
		retryCount = 0
	}
	nextPollAt := nextImageTaskPollAt(current, retryCount)
	if imageTaskIsTerminal(current) {
		nextPollAt = 0
		retryCount = 0
	}
	if err := model.ReleaseTaskLease(current.ID, item.owner, nextPollAt, retryCount); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("release image task %s lease failed: %v", current.TaskID, err))
	}
}

func imageTaskProgressed(before leasedImageTask, current *model.Task) bool {
	if current == nil {
		return false
	}
	return current.Status != before.status ||
		current.Progress != before.progress ||
		current.PrivateData.UpstreamTaskID != before.upstreamID ||
		current.SettlementStatus != before.settlementStatus
}

func imageTaskProgressedIntoRetryableSettlement(before leasedImageTask, current *model.Task) bool {
	if current == nil {
		return false
	}
	if current.Status != model.TaskStatusSuccess {
		return false
	}
	switch current.SettlementStatus {
	case model.TaskSettlementStatusPending, model.TaskSettlementStatusApplied:
		return before.settlementStatus != current.SettlementStatus
	default:
		return false
	}
}

func imageTaskIsTerminal(task *model.Task) bool {
	if task == nil {
		return false
	}
	if task.Status == model.TaskStatusSuccess && (task.SettlementStatus == model.TaskSettlementStatusPending || task.SettlementStatus == model.TaskSettlementStatusApplied) {
		return false
	}
	return task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure
}

func nextImageTaskPollAt(task *model.Task, retryCount int) int64 {
	if task == nil || imageTaskIsTerminal(task) {
		return 0
	}
	delay := imageTaskPollDelay(task, retryCount)
	return time.Now().Add(delay).Unix()
}

func imageTaskPollDelay(task *model.Task, retryCount int) time.Duration {
	if retryCount > 0 {
		delay := time.Duration(1<<minImageTaskRetryExponent(retryCount)) * time.Second
		if delay < 5*time.Second {
			delay = 5 * time.Second
		}
		if delay > 120*time.Second {
			delay = 120 * time.Second
		}
		return imageTaskPollDelayWithJitter(delay, 15*time.Second)
	}
	if task != nil && task.PrivateData.UpstreamTaskID == "" {
		return imageTaskPollDelayWithJitter(3*time.Second, 2*time.Second)
	}
	age := imageTaskPollAge(task)
	var delay time.Duration
	switch task.Status {
	case model.TaskStatusInProgress:
		delay = 5 * time.Second
		if age >= 30*time.Minute {
			delay = 30 * time.Second
		} else if age >= 15*time.Minute {
			delay = 20 * time.Second
		} else if age >= 5*time.Minute {
			delay = 10 * time.Second
		}
	case model.TaskStatusQueued, model.TaskStatusSubmitted:
		delay = 10 * time.Second
		if age >= 30*time.Minute {
			delay = 60 * time.Second
		} else if age >= 10*time.Minute {
			delay = 30 * time.Second
		}
	default:
		delay = 10 * time.Second
	}
	return imageTaskPollDelayWithJitter(delay, 10*time.Second)
}

func imageTaskPollAge(task *model.Task) time.Duration {
	if task == nil {
		return 0
	}
	now := time.Now().Unix()
	for _, ts := range []int64{task.StartTime, task.SubmitTime, task.CreatedAt} {
		if ts > 0 && ts <= now {
			return time.Duration(now-ts) * time.Second
		}
	}
	return 0
}

func imageTaskPollDelayWithJitter(base time.Duration, maxJitter time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	jitterRange := base / 5
	if jitterRange < time.Second {
		jitterRange = time.Second
	}
	if maxJitter > 0 && jitterRange > maxJitter {
		jitterRange = maxJitter
	}
	jitter := time.Duration(rand.Int63n(int64(jitterRange) + 1))
	return base + jitter
}

func minImageTaskRetryExponent(retryCount int) int {
	if retryCount < 0 {
		return 0
	}
	if retryCount > 7 {
		return 7
	}
	return retryCount
}

func imageTaskLeaseOwner() string {
	node := strings.TrimSpace(common.NodeName)
	if node == "" {
		node = "newapi"
	}
	return fmt.Sprintf("%s-image-%d-%s", node, time.Now().UnixNano(), common.GetRandomString(6))
}

func imageTaskLeaseSeconds() int64 {
	if constant.ImageTaskLeaseSeconds > 0 {
		return int64(constant.ImageTaskLeaseSeconds)
	}
	return 120
}

func imageTaskWorkerConcurrency() int {
	if constant.ImageTaskWorkerConcurrency > 0 {
		return constant.ImageTaskWorkerConcurrency
	}
	return 20
}

func imageTaskChannelConcurrency() int {
	if constant.ImageTaskChannelConcurrency > 0 {
		return constant.ImageTaskChannelConcurrency
	}
	return 10
}

func imageTaskBatchPollSize() int {
	size := constant.ImageTaskBatchPollSize
	if size <= 0 {
		return 20
	}
	if size > imageTaskBatchPollMaxSize {
		return imageTaskBatchPollMaxSize
	}
	return size
}

func imageTaskChannelSaturationBackoffSeconds() int64 {
	return 2
}

func UpdateSunoTasks(ctx context.Context, taskChannelM map[int][]string, taskM map[string]*model.Task) error {
	for channelId, taskIds := range taskChannelM {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := updateSunoTasks(ctx, channelId, taskIds, taskM)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("渠道 #%d 更新异步任务失败: %s", channelId, err.Error()))
		}
	}
	return nil
}

func updateSunoTasks(ctx context.Context, channelId int, taskIds []string, taskM map[string]*model.Task) error {
	logger.LogInfo(ctx, fmt.Sprintf("渠道 #%d 未完成的任务有: %d", channelId, len(taskIds)))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(taskIds) == 0 {
		return nil
	}
	ch, err := model.CacheGetChannel(channelId)
	if err != nil {
		common.SysLog(fmt.Sprintf("CacheGetChannel: %v", err))
		// Collect DB primary key IDs for bulk update (taskIds are upstream IDs, not task_id column values)
		var failedIDs []int64
		for _, upstreamID := range taskIds {
			if t, ok := taskM[upstreamID]; ok {
				failedIDs = append(failedIDs, t.ID)
			}
		}
		err = model.TaskBulkUpdateByID(failedIDs, map[string]any{
			"fail_reason": fmt.Sprintf("获取渠道信息失败，请联系管理员，渠道ID：%d", channelId),
			"status":      "FAILURE",
			"progress":    "100%",
		})
		if err != nil {
			common.SysLog(fmt.Sprintf("UpdateSunoTask error: %v", err))
		}
		return err
	}
	adaptor := GetTaskAdaptorFunc(constant.TaskPlatformSuno)
	if adaptor == nil {
		return errors.New("adaptor not found")
	}
	proxy := ch.GetSetting().Proxy
	resp, err := adaptor.FetchTask(*ch.BaseURL, ch.Key, map[string]any{
		"ids": taskIds,
	}, proxy)
	if err != nil {
		common.SysLog(fmt.Sprintf("Get Task Do req error: %v", err))
		return err
	}
	if resp.StatusCode != http.StatusOK {
		logger.LogError(ctx, fmt.Sprintf("Get Task status code: %d", resp.StatusCode))
		return fmt.Errorf("Get Task status code: %d", resp.StatusCode)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		common.SysLog(fmt.Sprintf("Get Suno Task parse body error: %v", err))
		return err
	}
	var responseItems dto.TaskResponse[[]dto.SunoDataResponse]
	err = common.Unmarshal(responseBody, &responseItems)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Get Suno Task parse body error2: %v, body: %s", err, string(responseBody)))
		return err
	}
	if !responseItems.IsSuccess() {
		common.SysLog(fmt.Sprintf("渠道 #%d 未完成的任务有: %d, 成功获取到任务数: %s", channelId, len(taskIds), string(responseBody)))
		return err
	}

	for _, responseItem := range responseItems.Data {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		task := taskM[responseItem.TaskID]
		if task == nil {
			logger.LogWarn(ctx, fmt.Sprintf("Suno task response ignored: unknown task_id=%s", responseItem.TaskID))
			continue
		}
		if !taskNeedsUpdate(task, responseItem) {
			continue
		}

		task.Status = lo.If(model.TaskStatus(responseItem.Status) != "", model.TaskStatus(responseItem.Status)).Else(task.Status)
		task.FailReason = lo.If(responseItem.FailReason != "", responseItem.FailReason).Else(task.FailReason)
		task.SubmitTime = lo.If(responseItem.SubmitTime != 0, responseItem.SubmitTime).Else(task.SubmitTime)
		task.StartTime = lo.If(responseItem.StartTime != 0, responseItem.StartTime).Else(task.StartTime)
		task.FinishTime = lo.If(responseItem.FinishTime != 0, responseItem.FinishTime).Else(task.FinishTime)
		if responseItem.FailReason != "" || task.Status == model.TaskStatusFailure {
			logger.LogInfo(ctx, task.TaskID+" 构建失败，"+task.FailReason)
			task.Progress = "100%"
			RefundTaskQuota(ctx, task, task.FailReason)
		}
		if responseItem.Status == model.TaskStatusSuccess {
			task.Progress = "100%"
		}
		task.Data = responseItem.Data

		err = task.Update()
		if err != nil {
			common.SysLog("UpdateSunoTask task error: " + err.Error())
		}
	}
	return nil
}

// taskNeedsUpdate 检查 Suno 任务是否需要更新
func taskNeedsUpdate(oldTask *model.Task, newTask dto.SunoDataResponse) bool {
	if oldTask.SubmitTime != newTask.SubmitTime {
		return true
	}
	if oldTask.StartTime != newTask.StartTime {
		return true
	}
	if oldTask.FinishTime != newTask.FinishTime {
		return true
	}
	if string(oldTask.Status) != newTask.Status {
		return true
	}
	if oldTask.FailReason != newTask.FailReason {
		return true
	}

	if (oldTask.Status == model.TaskStatusFailure || oldTask.Status == model.TaskStatusSuccess) && oldTask.Progress != "100%" {
		return true
	}

	oldData, _ := common.Marshal(oldTask.Data)
	newData, _ := common.Marshal(newTask.Data)

	sort.Slice(oldData, func(i, j int) bool {
		return oldData[i] < oldData[j]
	})
	sort.Slice(newData, func(i, j int) bool {
		return newData[i] < newData[j]
	})

	if string(oldData) != string(newData) {
		return true
	}
	return false
}

// UpdateVideoTasks 按渠道更新所有视频任务
func UpdateVideoTasks(ctx context.Context, platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*model.Task) error {
	for channelId, taskIds := range taskChannelM {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := updateVideoTasks(ctx, platform, channelId, taskIds, taskM); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Channel #%d failed to update video async tasks: %s", channelId, err.Error()))
		}
	}
	return nil
}

func updateVideoTasks(ctx context.Context, platform constant.TaskPlatform, channelId int, taskIds []string, taskM map[string]*model.Task) error {
	logger.LogInfo(ctx, fmt.Sprintf("Channel #%d pending video tasks: %d", channelId, len(taskIds)))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(taskIds) == 0 {
		return nil
	}
	cacheGetChannel, err := model.CacheGetChannel(channelId)
	if err != nil {
		// Collect DB primary key IDs for bulk update (taskIds are upstream IDs, not task_id column values)
		var failedIDs []int64
		for _, upstreamID := range taskIds {
			if t, ok := taskM[upstreamID]; ok {
				failedIDs = append(failedIDs, t.ID)
			}
		}
		errUpdate := model.TaskBulkUpdateByID(failedIDs, map[string]any{
			"fail_reason": fmt.Sprintf("Failed to get channel info, channel ID: %d", channelId),
			"status":      "FAILURE",
			"progress":    "100%",
		})
		if errUpdate != nil {
			common.SysLog(fmt.Sprintf("UpdateVideoTask error: %v", errUpdate))
		}
		return fmt.Errorf("CacheGetChannel failed: %w", err)
	}
	adaptor := GetTaskAdaptorFunc(platform)
	if adaptor == nil {
		return fmt.Errorf("video adaptor not found")
	}
	info := &relaycommon.RelayInfo{}
	info.ChannelMeta = &relaycommon.ChannelMeta{
		ChannelBaseUrl: cacheGetChannel.GetBaseURL(),
	}
	info.ApiKey = cacheGetChannel.Key
	adaptor.Init(info)
	disablePollingSleep := cacheGetChannel.GetOtherSettings().DisableTaskPollingSleep
	for i, taskId := range taskIds {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := updateVideoSingleTask(ctx, adaptor, cacheGetChannel, taskId, taskM); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to update video task %s: %s", taskId, err.Error()))
		}
		if disablePollingSleep || i == len(taskIds)-1 {
			continue
		}
		// sleep 1 second between each task to avoid hitting rate limits of upstream platforms
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
	return nil
}

func updateVideoSingleTask(ctx context.Context, adaptor TaskPollingAdaptor, ch *model.Channel, taskId string, taskM map[string]*model.Task) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	baseURL := constant.ChannelBaseURLs[ch.Type]
	if ch.GetBaseURL() != "" {
		baseURL = ch.GetBaseURL()
	}
	proxy := ch.GetSetting().Proxy

	task := taskM[taskId]
	if task == nil {
		logger.LogError(ctx, fmt.Sprintf("Task %s not found in taskM", taskId))
		return fmt.Errorf("task %s not found", taskId)
	}
	key := ch.Key

	privateData := task.PrivateData
	if privateData.Key != "" {
		key = privateData.Key
	}
	resp, err := adaptor.FetchTask(baseURL, key, map[string]any{
		"task_id": task.GetUpstreamTaskID(),
		"action":  task.Action,
	}, proxy)
	if err != nil {
		return fmt.Errorf("fetchTask failed for task %s: %w", taskId, err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("readAll failed for task %s: %w", taskId, err)
	}

	logger.LogDebug(ctx, "updateVideoSingleTask response: %s", responseBody)

	snap := task.Snapshot()

	taskResult := &relaycommon.TaskInfo{}
	// try parse as New API response format
	var responseItems dto.TaskResponse[model.Task]
	if err = common.Unmarshal(responseBody, &responseItems); err == nil && responseItems.IsSuccess() {
		logger.LogDebug(ctx, "updateVideoSingleTask parsed as new api response format: %+v", responseItems)
		t := responseItems.Data
		taskResult.TaskID = t.TaskID
		taskResult.Status = string(t.Status)
		taskResult.Url = t.GetResultURL()
		taskResult.Progress = t.Progress
		taskResult.Reason = t.FailReason
		task.Data = t.Data
	} else if taskResult, err = adaptor.ParseTaskResult(responseBody); err != nil {
		return fmt.Errorf("parseTaskResult failed for task %s: %w", taskId, err)
	}

	task.Data = redactVideoResponseBody(responseBody)

	logger.LogDebug(ctx, "updateVideoSingleTask taskResult: %+v", taskResult)

	now := time.Now().Unix()
	if taskResult.Status == "" {
		//taskResult = relaycommon.FailTaskInfo("upstream returned empty status")
		errorResult := &dto.GeneralErrorResponse{}
		if err = common.Unmarshal(responseBody, &errorResult); err == nil {
			openaiError := errorResult.TryToOpenAIError()
			if openaiError != nil {
				// 返回规范的 OpenAI 错误格式，提取错误信息，判断错误是否为任务失败
				if openaiError.Code == "429" {
					// 429 错误通常表示请求过多或速率限制，暂时不认为是任务失败，保持原状态等待下一轮轮询
					return nil
				}

				// 其他错误认为是任务失败，记录错误信息并更新任务状态
				taskResult = relaycommon.FailTaskInfo("upstream returned error")
			} else {
				// unknown error format, log original response
				logger.LogError(ctx, fmt.Sprintf("Task %s returned empty status with unrecognized error format, response: %s", taskId, string(responseBody)))
				taskResult = relaycommon.FailTaskInfo("upstream returned unrecognized message")
			}
		}
	}

	shouldRefund := false
	shouldSettle := false
	quota := task.Quota

	task.Status = model.TaskStatus(taskResult.Status)
	switch taskResult.Status {
	case model.TaskStatusSubmitted:
		task.Progress = taskcommon.ProgressSubmitted
	case model.TaskStatusQueued:
		task.Progress = taskcommon.ProgressQueued
	case model.TaskStatusInProgress:
		task.Progress = taskcommon.ProgressInProgress
		if task.StartTime == 0 {
			task.StartTime = now
		}
	case model.TaskStatusSuccess:
		task.Progress = taskcommon.ProgressComplete
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		if strings.HasPrefix(taskResult.Url, "data:") {
			// data: URI (e.g. Vertex base64 encoded video) — keep in Data, not in ResultURL
			task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
		} else if taskResult.Url != "" {
			// Direct upstream URL (e.g. Kling, Ali, Doubao, etc.)
			task.PrivateData.ResultURL = taskResult.Url
		} else {
			// No URL from adaptor — construct proxy URL using public task ID
			task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
		}
		shouldSettle = true
	case model.TaskStatusFailure:
		logger.LogJson(ctx, fmt.Sprintf("Task %s failed", taskId), task)
		task.Status = model.TaskStatusFailure
		task.Progress = taskcommon.ProgressComplete
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		task.FailReason = taskResult.Reason
		logger.LogInfo(ctx, fmt.Sprintf("Task %s failed: %s", task.TaskID, task.FailReason))
		taskResult.Progress = taskcommon.ProgressComplete
		if quota != 0 {
			shouldRefund = true
		}
	default:
		return fmt.Errorf("unknown task status %s for task %s", taskResult.Status, task.TaskID)
	}
	if taskResult.Progress != "" {
		task.Progress = taskResult.Progress
	}

	isDone := task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure
	if isDone && snap.Status != task.Status {
		won, err := task.UpdateWithStatus(snap.Status)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("UpdateWithStatus failed for task %s: %s", task.TaskID, err.Error()))
			shouldRefund = false
			shouldSettle = false
		} else if !won {
			logger.LogWarn(ctx, fmt.Sprintf("Task %s already transitioned by another process, skip billing", task.TaskID))
			shouldRefund = false
			shouldSettle = false
		}
	} else if !snap.Equal(task.Snapshot()) {
		if _, err := task.UpdateWithStatus(snap.Status); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to update task %s: %s", task.TaskID, err.Error()))
		}
	} else {
		// No changes, skip update
		logger.LogDebug(ctx, "No update needed for task %s", task.TaskID)
	}

	if shouldSettle {
		settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)
	}
	if shouldRefund {
		RefundTaskQuota(ctx, task, task.FailReason)
	}

	return nil
}

func redactVideoResponseBody(body []byte) []byte {
	var m map[string]any
	if err := common.Unmarshal(body, &m); err != nil {
		return body
	}
	resp, _ := m["response"].(map[string]any)
	if resp != nil {
		delete(resp, "bytesBase64Encoded")
		if v, ok := resp["video"].(string); ok {
			resp["video"] = truncateBase64(v)
		}
		if vs, ok := resp["videos"].([]any); ok {
			for i := range vs {
				if vm, ok := vs[i].(map[string]any); ok {
					delete(vm, "bytesBase64Encoded")
				}
			}
		}
	}
	b, err := common.Marshal(m)
	if err != nil {
		return body
	}
	return b
}

func truncateBase64(s string) string {
	const maxKeep = 256
	if len(s) <= maxKeep {
		return s
	}
	return s[:maxKeep] + "..."
}

// settleTaskBillingOnComplete 任务完成时的统一计费调整。
// 优先级：1. adaptor.AdjustBillingOnComplete 返回正数 → 使用 adaptor 计算的额度
//
//  2. taskResult.TotalTokens > 0 → 按 token 重算
//  3. 都不满足 → 保持预扣额度不变
func settleTaskBillingOnComplete(ctx context.Context, adaptor TaskPollingAdaptor, task *model.Task, taskResult *relaycommon.TaskInfo) {
	// 0. 按次计费的任务不做差额结算
	if bc := task.PrivateData.BillingContext; bc != nil && bc.PerCallBilling {
		logger.LogInfo(ctx, fmt.Sprintf("任务 %s 按次计费，跳过差额结算", task.TaskID))
		return
	}
	// 1. 优先让 adaptor 决定最终额度
	if actualQuota := adaptor.AdjustBillingOnComplete(task, taskResult); actualQuota > 0 {
		RecalculateTaskQuota(ctx, task, actualQuota, "adaptor计费调整")
		return
	}
	// 2. 回退到 token 重算
	if taskResult.TotalTokens > 0 {
		RecalculateTaskQuotaByTokens(ctx, task, taskResult.TotalTokens)
		return
	}
	// 3. 无调整，保持预扣额度
}
