package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to open test db: " + err.Error())
	}
	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get sql.DB: " + err.Error())
	}
	sqlDB.SetMaxOpenConns(1)

	model.DB = db
	model.LOG_DB = db

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true

	if err := db.AutoMigrate(
		&model.Task{},
		&model.TaskDispatchState{},
		&model.ImageTaskChannelLease{},
		&model.TaskSettlementRecord{},
		&model.User{},
		&model.UserLoginIdentifier{},
		&model.Token{},
		&model.Log{},
		&model.Channel{},
		&model.TopUp{},
		&model.UserSubscription{},
		&model.SystemTask{},
		&model.SystemTaskLock{},
	); err != nil {
		panic("failed to migrate: " + err.Error())
	}

	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// Seed helpers
// ---------------------------------------------------------------------------

func truncate(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM tasks")
		model.DB.Exec("DELETE FROM task_dispatch_states")
		model.DB.Exec("DELETE FROM image_task_channel_leases")
		model.DB.Exec("DELETE FROM task_settlement_records")
		model.DB.Exec("DELETE FROM users")
		model.DB.Exec("DELETE FROM tokens")
		model.DB.Exec("DELETE FROM logs")
		model.DB.Exec("DELETE FROM channels")
		model.DB.Exec("DELETE FROM top_ups")
		model.DB.Exec("DELETE FROM user_subscriptions")
		model.DB.Exec("DELETE FROM system_task_locks")
		model.DB.Exec("DELETE FROM system_tasks")
	})
}

func TestImageTaskBatchPollSizeClampsToProtocolLimit(t *testing.T) {
	oldBatchSize := constant.ImageTaskBatchPollSize
	constant.ImageTaskBatchPollSize = 250
	t.Cleanup(func() {
		constant.ImageTaskBatchPollSize = oldBatchSize
	})

	require.Equal(t, 100, imageTaskBatchPollSize())
}

func TestTaskPollingPlatformOrderPrioritizesImage(t *testing.T) {
	order := taskPollingPlatformOrder(map[constant.TaskPlatform][]*model.Task{
		constant.TaskPlatformSuno:      {},
		constant.TaskPlatformImage:     {},
		constant.TaskPlatform("video"): {},
	})

	require.Len(t, order, 3)
	require.Equal(t, string(constant.TaskPlatformImage), string(order[0]))
	require.Equal(t, string(constant.TaskPlatformSuno), string(order[1]))
	require.Equal(t, "video", string(order[2]))
}

func TestRunTaskPollingOnceRunsImageTasksWithoutGenericAdaptor(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Exec("DELETE FROM tasks").Error)
	oldAdaptor := GetTaskAdaptorFunc
	oldRunImageTasks := RunImageTasksFunc
	oldTaskQueryLimit := constant.TaskQueryLimit
	oldWorkerConcurrency := constant.ImageTaskWorkerConcurrency
	oldChannelConcurrency := constant.ImageTaskChannelConcurrency
	oldLeaseSeconds := constant.ImageTaskLeaseSeconds
	GetTaskAdaptorFunc = nil
	constant.TaskQueryLimit = 10
	constant.ImageTaskWorkerConcurrency = 1
	constant.ImageTaskChannelConcurrency = 1
	constant.ImageTaskLeaseSeconds = 60
	var called int32
	RunImageTasksFunc = func(ctx context.Context, tasks []*model.Task) error {
		atomic.AddInt32(&called, int32(len(tasks)))
		require.Len(t, tasks, 1)
		require.Equal(t, "task_image_without_generic_adaptor", tasks[0].TaskID)
		return nil
	}
	t.Cleanup(func() {
		GetTaskAdaptorFunc = oldAdaptor
		RunImageTasksFunc = oldRunImageTasks
		constant.TaskQueryLimit = oldTaskQueryLimit
		constant.ImageTaskWorkerConcurrency = oldWorkerConcurrency
		constant.ImageTaskChannelConcurrency = oldChannelConcurrency
		constant.ImageTaskLeaseSeconds = oldLeaseSeconds
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:     "task_image_without_generic_adaptor",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		NextPollAt: now - 1,
		PrivateData: model.TaskPrivateData{
			ImageTaskMode: dto.ImageTaskModeGPTImage2APIAsync,
		},
	}
	require.NoError(t, model.DB.Create(task).Error)

	summary := RunTaskPollingOnce(context.Background(), nil)

	require.Equal(t, 1, summary.UnfinishedTasks)
	require.Equal(t, 1, int(atomic.LoadInt32(&called)))
}

func TestRunTaskPollingOnceStartsOtherPlatformsWhileImageRunning(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Exec("DELETE FROM tasks").Error)
	oldAdaptor := GetTaskAdaptorFunc
	oldRunImageTasks := RunImageTasksFunc
	oldTaskQueryLimit := constant.TaskQueryLimit
	oldWorkerConcurrency := constant.ImageTaskWorkerConcurrency
	oldChannelConcurrency := constant.ImageTaskChannelConcurrency
	oldLeaseSeconds := constant.ImageTaskLeaseSeconds
	constant.TaskQueryLimit = 10
	constant.ImageTaskWorkerConcurrency = 1
	constant.ImageTaskChannelConcurrency = 1
	constant.ImageTaskLeaseSeconds = 60
	imageStarted := make(chan struct{})
	releaseImage := make(chan struct{})
	sunoStarted := make(chan struct{})
	done := make(chan struct{})
	var imageStartedClosed int32
	var releaseClosed int32
	closeRelease := func() {
		if atomic.CompareAndSwapInt32(&releaseClosed, 0, 1) {
			close(releaseImage)
		}
	}
	RunImageTasksFunc = func(ctx context.Context, tasks []*model.Task) error {
		require.Len(t, tasks, 1)
		if atomic.CompareAndSwapInt32(&imageStartedClosed, 0, 1) {
			close(imageStarted)
			select {
			case <-releaseImage:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
	GetTaskAdaptorFunc = func(platform constant.TaskPlatform) TaskPollingAdaptor {
		if platform == constant.TaskPlatformSuno {
			return &signalTaskPollingAdaptor{started: sunoStarted}
		}
		return nil
	}
	t.Cleanup(func() {
		closeRelease()
		GetTaskAdaptorFunc = oldAdaptor
		RunImageTasksFunc = oldRunImageTasks
		constant.TaskQueryLimit = oldTaskQueryLimit
		constant.ImageTaskWorkerConcurrency = oldWorkerConcurrency
		constant.ImageTaskChannelConcurrency = oldChannelConcurrency
		constant.ImageTaskLeaseSeconds = oldLeaseSeconds
	})

	now := time.Now().Unix()
	imageTask := &model.Task{
		TaskID:     "task_image_parallel_platform",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		NextPollAt: now - 1,
		PrivateData: model.TaskPrivateData{
			ImageTaskMode: dto.ImageTaskModeGPTImage2APIAsync,
		},
	}
	baseURL := "http://suno.local"
	sunoChannel := &model.Channel{
		Id:      2,
		Name:    "suno",
		Key:     "suno-key",
		Status:  common.ChannelStatusEnabled,
		BaseURL: &baseURL,
	}
	sunoTask := &model.Task{
		TaskID:     "suno_task_parallel_platform",
		Platform:   constant.TaskPlatformSuno,
		UserId:     1,
		Group:      "default",
		ChannelId:  2,
		Status:     model.TaskStatusSubmitted,
		Progress:   "0%",
		SubmitTime: now,
	}
	require.NoError(t, model.DB.Create(imageTask).Error)
	require.NoError(t, model.DB.Create(sunoChannel).Error)
	require.NoError(t, model.DB.Create(sunoTask).Error)

	go func() {
		RunTaskPollingOnce(context.Background(), nil)
		close(done)
	}()

	select {
	case <-imageStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("image task did not start")
	}
	select {
	case <-sunoStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("suno task waited for image dispatch to finish")
	}
	closeRelease()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("polling pass did not finish")
	}
}

func seedUser(t *testing.T, id int, quota int) {
	t.Helper()
	user := &model.User{Id: id, Username: "test_user", Quota: quota, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
}

func seedToken(t *testing.T, id int, userId int, key string, remainQuota int) {
	t.Helper()
	token := &model.Token{
		Id:          id,
		UserId:      userId,
		Key:         key,
		Name:        "test_token",
		Status:      common.TokenStatusEnabled,
		RemainQuota: remainQuota,
		UsedQuota:   0,
	}
	require.NoError(t, model.DB.Create(token).Error)
}

func seedSubscription(t *testing.T, id int, userId int, amountTotal int64, amountUsed int64) {
	t.Helper()
	sub := &model.UserSubscription{
		Id:          id,
		UserId:      userId,
		AmountTotal: amountTotal,
		AmountUsed:  amountUsed,
		Status:      "active",
		StartTime:   time.Now().Unix(),
		EndTime:     time.Now().Add(30 * 24 * time.Hour).Unix(),
	}
	require.NoError(t, model.DB.Create(sub).Error)
}

func seedChannel(t *testing.T, id int) {
	t.Helper()
	ch := &model.Channel{Id: id, Name: "test_channel", Key: "sk-test", Status: common.ChannelStatusEnabled}
	require.NoError(t, model.DB.Create(ch).Error)
}

func makeTask(userId, channelId, quota, tokenId int, billingSource string, subscriptionId int) *model.Task {
	return &model.Task{
		TaskID:    "task_" + time.Now().Format("150405.000"),
		UserId:    userId,
		ChannelId: channelId,
		Quota:     quota,
		Status:    model.TaskStatus(model.TaskStatusInProgress),
		Group:     "default",
		Data:      json.RawMessage(`{}`),
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
		Properties: model.Properties{
			OriginModelName: "test-model",
		},
		PrivateData: model.TaskPrivateData{
			BillingSource:  billingSource,
			SubscriptionId: subscriptionId,
			TokenId:        tokenId,
			BillingContext: &model.TaskBillingContext{
				ModelPrice:      0.02,
				GroupRatio:      1.0,
				OriginModelName: "test-model",
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Read-back helpers
// ---------------------------------------------------------------------------

func getUserQuota(t *testing.T, id int) int {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", id).First(&user).Error)
	return user.Quota
}

func getTokenRemainQuota(t *testing.T, id int) int {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.Select("remain_quota").Where("id = ?", id).First(&token).Error)
	return token.RemainQuota
}

func getTokenUsedQuota(t *testing.T, id int) int {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.Select("used_quota").Where("id = ?", id).First(&token).Error)
	return token.UsedQuota
}

func getSubscriptionUsed(t *testing.T, id int) int64 {
	t.Helper()
	var sub model.UserSubscription
	require.NoError(t, model.DB.Select("amount_used").Where("id = ?", id).First(&sub).Error)
	return sub.AmountUsed
}

func getLastLog(t *testing.T) *model.Log {
	t.Helper()
	var log model.Log
	err := model.LOG_DB.Order("id desc").First(&log).Error
	if err != nil {
		return nil
	}
	return &log
}

func countLogs(t *testing.T) int64 {
	t.Helper()
	var count int64
	model.LOG_DB.Model(&model.Log{}).Count(&count)
	return count
}

func TestRunTaskPollingOnceCleansExpiredImageTaskResultCacheWithoutAdaptor(t *testing.T) {
	oldConfig := common.GetDiskCacheConfig()
	oldCleanupUnix := atomic.LoadInt64(&imageTaskResultCacheCleanupUnix)
	oldAdaptor := GetTaskAdaptorFunc
	oldTaskTimeoutMinutes := constant.TaskTimeoutMinutes
	oldResultRetentionMinutes := constant.ImageTaskResultRetentionMinutes
	cacheRoot := t.TempDir()
	common.SetDiskCacheConfig(common.DiskCacheConfig{Path: cacheRoot})
	atomic.StoreInt64(&imageTaskResultCacheCleanupUnix, 0)
	GetTaskAdaptorFunc = nil
	constant.TaskTimeoutMinutes = 60
	constant.ImageTaskResultRetentionMinutes = 60
	t.Cleanup(func() {
		common.SetDiskCacheConfig(oldConfig)
		atomic.StoreInt64(&imageTaskResultCacheCleanupUnix, oldCleanupUnix)
		GetTaskAdaptorFunc = oldAdaptor
		constant.TaskTimeoutMinutes = oldTaskTimeoutMinutes
		constant.ImageTaskResultRetentionMinutes = oldResultRetentionMinutes
	})

	path, err := common.WriteImageTaskResultCacheFile([]byte(`{"data":[{"b64_json":"expired"}]}`))
	require.NoError(t, err)
	oldTime := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(path, oldTime, oldTime))

	RunTaskPollingOnce(context.Background(), nil)

	require.NoFileExists(t, path)
}

func TestSweepTimedOutTasksSkipsImageTaskForRunner(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Exec("DELETE FROM tasks").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM logs").Error)
	oldConfig := common.GetDiskCacheConfig()
	oldTaskTimeoutMinutes := constant.TaskTimeoutMinutes
	cacheRoot := t.TempDir()
	common.SetDiskCacheConfig(common.DiskCacheConfig{Path: cacheRoot})
	constant.TaskTimeoutMinutes = 1
	t.Cleanup(func() {
		common.SetDiskCacheConfig(oldConfig)
		constant.TaskTimeoutMinutes = oldTaskTimeoutMinutes
	})

	bodyPath, err := common.WriteImageTaskBodyCacheFile([]byte(`{"model":"gpt-image-1","prompt":"hello","stream":false}`))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove(bodyPath)
	})
	now := time.Now().Unix()
	task := &model.Task{
		TaskID:     "task_image_async_timeout_guard",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Action:     constant.TaskActionImageGeneration,
		Status:     model.TaskStatusSubmitted,
		Progress:   "0%",
		SubmitTime: now - 120,
		StartTime:  now,
		PrivateData: model.TaskPrivateData{
			ImageTaskMode:   dto.ImageTaskModeGPTImage2APIAsync,
			RequestBodyPath: bodyPath,
			UpstreamTaskID:  "upstream_123",
		},
	}
	require.NoError(t, model.DB.Create(task).Error)

	sweepTimedOutTasks(context.Background())

	var reloaded model.Task
	require.NoError(t, model.DB.Where("task_id = ?", task.TaskID).First(&reloaded).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSubmitted), reloaded.Status)
	require.Equal(t, "0%", reloaded.Progress)
	require.Equal(t, bodyPath, reloaded.PrivateData.RequestBodyPath)
	require.FileExists(t, bodyPath)
}

func TestSweepTimedOutTasksStillFailsNonImageTask(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Exec("DELETE FROM tasks").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM logs").Error)
	oldTaskTimeoutMinutes := constant.TaskTimeoutMinutes
	constant.TaskTimeoutMinutes = 1
	t.Cleanup(func() {
		constant.TaskTimeoutMinutes = oldTaskTimeoutMinutes
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:     "task_video_timeout_guard",
		Platform:   constant.TaskPlatform("video"),
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusInProgress,
		Progress:   "50%",
		SubmitTime: now - 120,
		StartTime:  now - 120,
	}
	require.NoError(t, model.DB.Create(task).Error)

	sweepTimedOutTasks(context.Background())

	var reloaded model.Task
	require.NoError(t, model.DB.Where("task_id = ?", task.TaskID).First(&reloaded).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status)
	require.Equal(t, "100%", reloaded.Progress)
	require.NotEmpty(t, reloaded.FailReason)
}

func TestRunTaskPollingOnceSkipsFutureImageTask(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Exec("DELETE FROM tasks").Error)
	oldAdaptor := GetTaskAdaptorFunc
	oldRunImageTasks := RunImageTasksFunc
	GetTaskAdaptorFunc = func(platform constant.TaskPlatform) TaskPollingAdaptor {
		return nil
	}
	var called int32
	RunImageTasksFunc = func(ctx context.Context, tasks []*model.Task) error {
		atomic.AddInt32(&called, 1)
		return nil
	}
	t.Cleanup(func() {
		GetTaskAdaptorFunc = oldAdaptor
		RunImageTasksFunc = oldRunImageTasks
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:     "task_image_future_poll",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		NextPollAt: now + 3600,
		PrivateData: model.TaskPrivateData{
			ImageTaskMode: dto.ImageTaskModeGPTImage2APIAsync,
		},
	}
	require.NoError(t, model.DB.Create(task).Error)

	RunTaskPollingOnce(context.Background(), nil)

	require.Equal(t, int32(0), atomic.LoadInt32(&called))
}

func TestRunTaskPollingOnceFairlyIncludesLaterChannelsBeforeLimit(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Exec("DELETE FROM tasks").Error)
	oldAdaptor := GetTaskAdaptorFunc
	oldRunImageTasks := RunImageTasksFunc
	oldTaskQueryLimit := constant.TaskQueryLimit
	oldWorkerConcurrency := constant.ImageTaskWorkerConcurrency
	oldChannelConcurrency := constant.ImageTaskChannelConcurrency
	oldLeaseSeconds := constant.ImageTaskLeaseSeconds
	GetTaskAdaptorFunc = func(platform constant.TaskPlatform) TaskPollingAdaptor {
		return nil
	}
	constant.TaskQueryLimit = 3
	constant.ImageTaskWorkerConcurrency = 3
	constant.ImageTaskChannelConcurrency = 1
	constant.ImageTaskLeaseSeconds = 60
	calls := make(chan int, 3)
	RunImageTasksFunc = func(ctx context.Context, tasks []*model.Task) error {
		require.Len(t, tasks, 1)
		select {
		case calls <- tasks[0].ChannelId:
		default:
		}
		return nil
	}
	t.Cleanup(func() {
		GetTaskAdaptorFunc = oldAdaptor
		RunImageTasksFunc = oldRunImageTasks
		constant.TaskQueryLimit = oldTaskQueryLimit
		constant.ImageTaskWorkerConcurrency = oldWorkerConcurrency
		constant.ImageTaskChannelConcurrency = oldChannelConcurrency
		constant.ImageTaskLeaseSeconds = oldLeaseSeconds
	})

	now := time.Now().Unix()
	for i := 0; i < 5; i++ {
		task := &model.Task{
			TaskID:     fmt.Sprintf("task_image_fair_hot_%d", i),
			Platform:   constant.TaskPlatformImage,
			UserId:     1,
			Group:      "default",
			ChannelId:  1,
			Status:     model.TaskStatusQueued,
			Progress:   "0%",
			SubmitTime: now,
			NextPollAt: now - 1,
			PrivateData: model.TaskPrivateData{
				ImageTaskMode: dto.ImageTaskModeGPTImage2APIAsync,
			},
		}
		require.NoError(t, model.DB.Create(task).Error)
	}
	for _, channelID := range []int{2, 3} {
		task := &model.Task{
			TaskID:     fmt.Sprintf("task_image_fair_channel_%d", channelID),
			Platform:   constant.TaskPlatformImage,
			UserId:     1,
			Group:      "default",
			ChannelId:  channelID,
			Status:     model.TaskStatusQueued,
			Progress:   "0%",
			SubmitTime: now,
			NextPollAt: now - 1,
			PrivateData: model.TaskPrivateData{
				ImageTaskMode: dto.ImageTaskModeGPTImage2APIAsync,
			},
		}
		require.NoError(t, model.DB.Create(task).Error)
	}

	RunTaskPollingOnce(context.Background(), nil)

	close(calls)
	executedChannels := make(map[int]bool)
	for channelID := range calls {
		executedChannels[channelID] = true
	}
	require.True(t, executedChannels[1])
	require.True(t, executedChannels[2])
	require.True(t, executedChannels[3])
	require.Len(t, executedChannels, 3)
}

func TestRunTaskPollingOnceRotatesImageChannelsAcrossPasses(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Exec("DELETE FROM tasks").Error)
	oldAdaptor := GetTaskAdaptorFunc
	oldRunImageTasks := RunImageTasksFunc
	oldTaskQueryLimit := constant.TaskQueryLimit
	oldWorkerConcurrency := constant.ImageTaskWorkerConcurrency
	oldChannelConcurrency := constant.ImageTaskChannelConcurrency
	oldLeaseSeconds := constant.ImageTaskLeaseSeconds
	GetTaskAdaptorFunc = func(platform constant.TaskPlatform) TaskPollingAdaptor {
		return nil
	}
	constant.TaskQueryLimit = 3
	constant.ImageTaskWorkerConcurrency = 3
	constant.ImageTaskChannelConcurrency = 1
	constant.ImageTaskLeaseSeconds = 60
	calls := make(chan int, 20)
	RunImageTasksFunc = func(ctx context.Context, tasks []*model.Task) error {
		require.Len(t, tasks, 1)
		calls <- tasks[0].ChannelId
		return nil
	}
	t.Cleanup(func() {
		GetTaskAdaptorFunc = oldAdaptor
		RunImageTasksFunc = oldRunImageTasks
		constant.TaskQueryLimit = oldTaskQueryLimit
		constant.ImageTaskWorkerConcurrency = oldWorkerConcurrency
		constant.ImageTaskChannelConcurrency = oldChannelConcurrency
		constant.ImageTaskLeaseSeconds = oldLeaseSeconds
	})

	now := time.Now().Unix()
	for channelID := 1; channelID <= 6; channelID++ {
		for i := 0; i < 2; i++ {
			task := &model.Task{
				TaskID:     fmt.Sprintf("task_image_rotate_%d_%d", channelID, i),
				Platform:   constant.TaskPlatformImage,
				UserId:     1,
				Group:      "default",
				ChannelId:  channelID,
				Status:     model.TaskStatusQueued,
				Progress:   "0%",
				SubmitTime: now,
				NextPollAt: now - 1,
				PrivateData: model.TaskPrivateData{
					ImageTaskMode: dto.ImageTaskModeGPTImage2APIAsync,
				},
			}
			require.NoError(t, model.DB.Create(task).Error)
		}
	}

	RunTaskPollingOnce(context.Background(), nil)
	firstPass := drainImageTaskChannelCalls(calls)
	RunTaskPollingOnce(context.Background(), nil)
	secondPass := drainImageTaskChannelCalls(calls)

	require.Len(t, firstPass, 3)
	require.Len(t, secondPass, 3)
	coveredChannels := make(map[int]bool)
	for _, channelID := range append(firstPass, secondPass...) {
		coveredChannels[channelID] = true
	}
	for channelID := 1; channelID <= 6; channelID++ {
		require.True(t, coveredChannels[channelID], "channel %d should be dispatched across two passes", channelID)
	}
}

func drainImageTaskChannelCalls(calls <-chan int) []int {
	channels := make([]int, 0)
	for {
		select {
		case channelID := <-calls:
			channels = append(channels, channelID)
		default:
			return channels
		}
	}
}

func TestRunTaskPollingOnceReservesImageSlotWhenNonImageBacklogFillsLimit(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Exec("DELETE FROM tasks").Error)
	oldAdaptor := GetTaskAdaptorFunc
	oldRunImageTasks := RunImageTasksFunc
	oldTaskQueryLimit := constant.TaskQueryLimit
	oldWorkerConcurrency := constant.ImageTaskWorkerConcurrency
	oldChannelConcurrency := constant.ImageTaskChannelConcurrency
	oldLeaseSeconds := constant.ImageTaskLeaseSeconds
	GetTaskAdaptorFunc = func(platform constant.TaskPlatform) TaskPollingAdaptor {
		return nil
	}
	constant.TaskQueryLimit = 3
	constant.ImageTaskWorkerConcurrency = 2
	constant.ImageTaskChannelConcurrency = 1
	constant.ImageTaskLeaseSeconds = 60
	calls := make(chan string, 1)
	RunImageTasksFunc = func(ctx context.Context, tasks []*model.Task) error {
		require.Len(t, tasks, 1)
		select {
		case calls <- tasks[0].TaskID:
		default:
		}
		return nil
	}
	t.Cleanup(func() {
		GetTaskAdaptorFunc = oldAdaptor
		RunImageTasksFunc = oldRunImageTasks
		constant.TaskQueryLimit = oldTaskQueryLimit
		constant.ImageTaskWorkerConcurrency = oldWorkerConcurrency
		constant.ImageTaskChannelConcurrency = oldChannelConcurrency
		constant.ImageTaskLeaseSeconds = oldLeaseSeconds
	})

	now := time.Now().Unix()
	for i := 0; i < 6; i++ {
		task := &model.Task{
			TaskID:     fmt.Sprintf("task_suno_backlog_%d", i),
			Platform:   constant.TaskPlatformSuno,
			UserId:     1,
			Group:      "default",
			ChannelId:  999,
			Status:     model.TaskStatusSubmitted,
			Progress:   "0%",
			SubmitTime: now,
		}
		require.NoError(t, model.DB.Create(task).Error)
	}
	imageTask := &model.Task{
		TaskID:     "task_image_reserved_slot",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		NextPollAt: now - 1,
		PrivateData: model.TaskPrivateData{
			ImageTaskMode: dto.ImageTaskModeGPTImage2APIAsync,
		},
	}
	require.NoError(t, model.DB.Create(imageTask).Error)

	RunTaskPollingOnce(context.Background(), nil)

	select {
	case taskID := <-calls:
		require.Equal(t, imageTask.TaskID, taskID)
	default:
		t.Fatal("image task was starved by non-image backlog")
	}
}

func TestRunTaskPollingOnceSkipsImageWhenDedicatedWorkerEnabled(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Exec("DELETE FROM tasks").Error)
	oldRunImageTasks := RunImageTasksFunc
	oldWorkerEnabled := constant.ImageTaskWorkerEnabled
	constant.ImageTaskWorkerEnabled = true
	var calls int32
	RunImageTasksFunc = func(ctx context.Context, tasks []*model.Task) error {
		atomic.AddInt32(&calls, 1)
		return nil
	}
	t.Cleanup(func() {
		RunImageTasksFunc = oldRunImageTasks
		constant.ImageTaskWorkerEnabled = oldWorkerEnabled
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:     "task_image_dedicated_worker_skip",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		NextPollAt: now - 1,
		PrivateData: model.TaskPrivateData{
			ImageTaskMode: dto.ImageTaskModeGPTImage2APIAsync,
		},
	}
	require.NoError(t, model.DB.Create(task).Error)

	RunTaskPollingOnce(context.Background(), nil)

	require.Equal(t, int32(0), atomic.LoadInt32(&calls))
}

func TestGetRunnableUnfinishedSyncTasksCapsImageBatchByWorkerLimit(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Exec("DELETE FROM tasks").Error)
	oldTaskQueryLimit := constant.TaskQueryLimit
	oldWorkerConcurrency := constant.ImageTaskWorkerConcurrency
	constant.TaskQueryLimit = 1000
	constant.ImageTaskWorkerConcurrency = 2
	t.Cleanup(func() {
		constant.TaskQueryLimit = oldTaskQueryLimit
		constant.ImageTaskWorkerConcurrency = oldWorkerConcurrency
	})

	now := time.Now().Unix()
	for i := 0; i < 20; i++ {
		task := &model.Task{
			TaskID:     fmt.Sprintf("task_image_capped_%d", i),
			Platform:   constant.TaskPlatformImage,
			UserId:     1,
			Group:      "default",
			ChannelId:  i + 1,
			Status:     model.TaskStatusQueued,
			Progress:   "0%",
			SubmitTime: now,
			NextPollAt: now - 1,
			PrivateData: model.TaskPrivateData{
				ImageTaskMode: dto.ImageTaskModeGPTImage2APIAsync,
			},
		}
		require.NoError(t, model.DB.Create(task).Error)
	}

	tasks := model.GetRunnableUnfinishedSyncTasks(constant.TaskQueryLimit, now)

	require.Len(t, tasks, 8)
}

func TestDispatchImageTasksReleasesLeaseAndSchedulesNextPoll(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Exec("DELETE FROM tasks").Error)
	oldRunImageTasks := RunImageTasksFunc
	oldWorkerConcurrency := constant.ImageTaskWorkerConcurrency
	oldChannelConcurrency := constant.ImageTaskChannelConcurrency
	oldLeaseSeconds := constant.ImageTaskLeaseSeconds
	constant.ImageTaskWorkerConcurrency = 2
	constant.ImageTaskChannelConcurrency = 1
	constant.ImageTaskLeaseSeconds = 60
	RunImageTasksFunc = func(ctx context.Context, tasks []*model.Task) error {
		require.Len(t, tasks, 1)
		task := tasks[0]
		owner := ImageTaskLeaseOwnerFromContext(ctx)
		require.NotEmpty(t, owner)
		require.Equal(t, task.LockOwner, owner)
		oldStatus := task.Status
		task.Status = model.TaskStatusSubmitted
		task.Progress = "0%"
		task.RetryCount = 0
		_, err := task.UpdateWithStatusAndLease(oldStatus, owner, time.Now().Unix())
		return err
	}
	t.Cleanup(func() {
		RunImageTasksFunc = oldRunImageTasks
		constant.ImageTaskWorkerConcurrency = oldWorkerConcurrency
		constant.ImageTaskChannelConcurrency = oldChannelConcurrency
		constant.ImageTaskLeaseSeconds = oldLeaseSeconds
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:     "task_image_dispatch_schedule",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		NextPollAt: now - 1,
		PrivateData: model.TaskPrivateData{
			ImageTaskMode: dto.ImageTaskModeGPTImage2APIAsync,
		},
	}
	require.NoError(t, model.DB.Create(task).Error)

	DispatchImageTasks(context.Background(), []*model.Task{task})

	var reloaded model.Task
	require.NoError(t, model.DB.Where("task_id = ?", task.TaskID).First(&reloaded).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSubmitted), reloaded.Status)
	require.Empty(t, reloaded.LockOwner)
	require.Zero(t, reloaded.LockUntil)
	require.Zero(t, reloaded.RetryCount)
	require.GreaterOrEqual(t, reloaded.NextPollAt, now+2)
}

func TestDispatchImageTasksTreatsZeroConcurrencyAsUnlimited(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Exec("DELETE FROM tasks").Error)
	oldRunImageTasks := RunImageTasksFunc
	oldWorkerConcurrency := constant.ImageTaskWorkerConcurrency
	oldChannelConcurrency := constant.ImageTaskChannelConcurrency
	oldTaskQueryLimit := constant.TaskQueryLimit
	oldLeaseSeconds := constant.ImageTaskLeaseSeconds
	constant.ImageTaskWorkerConcurrency = 0
	constant.ImageTaskChannelConcurrency = 0
	constant.TaskQueryLimit = 10
	constant.ImageTaskLeaseSeconds = 60
	var calls int32
	RunImageTasksFunc = func(ctx context.Context, tasks []*model.Task) error {
		require.Len(t, tasks, 1)
		atomic.AddInt32(&calls, 1)
		return nil
	}
	t.Cleanup(func() {
		RunImageTasksFunc = oldRunImageTasks
		constant.ImageTaskWorkerConcurrency = oldWorkerConcurrency
		constant.ImageTaskChannelConcurrency = oldChannelConcurrency
		constant.TaskQueryLimit = oldTaskQueryLimit
		constant.ImageTaskLeaseSeconds = oldLeaseSeconds
	})

	require.Equal(t, 10, imageTaskWorkerQueryLimit())
	now := time.Now().Unix()
	first := &model.Task{
		TaskID:     "task_image_unlimited_first",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		NextPollAt: now - 1,
		PrivateData: model.TaskPrivateData{
			ImageTaskMode: dto.ImageTaskModeGPTImage2APIAsync,
		},
	}
	second := &model.Task{
		TaskID:     "task_image_unlimited_second",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		NextPollAt: now - 1,
		PrivateData: model.TaskPrivateData{
			ImageTaskMode: dto.ImageTaskModeGPTImage2APIAsync,
		},
	}
	require.NoError(t, model.DB.Create(first).Error)
	require.NoError(t, model.DB.Create(second).Error)

	DispatchImageTasks(context.Background(), []*model.Task{first, second})

	require.Equal(t, int32(2), atomic.LoadInt32(&calls))
	var leasedCount int64
	require.NoError(t, model.DB.Model(&model.ImageTaskChannelLease{}).Where("channel_id = ?", 1).Count(&leasedCount).Error)
	require.Zero(t, leasedCount)
}

func TestDispatchImageTasksDoesNotLeaseQueuedWorkBeforeWorkerStarts(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Exec("DELETE FROM tasks").Error)
	oldRunImageTasks := RunImageTasksFunc
	oldWorkerConcurrency := constant.ImageTaskWorkerConcurrency
	oldChannelConcurrency := constant.ImageTaskChannelConcurrency
	oldLeaseSeconds := constant.ImageTaskLeaseSeconds
	constant.ImageTaskWorkerConcurrency = 1
	constant.ImageTaskChannelConcurrency = 1
	constant.ImageTaskLeaseSeconds = 60
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	var calls int32
	var releaseClosed int32
	closeRelease := func() {
		if atomic.CompareAndSwapInt32(&releaseClosed, 0, 1) {
			close(release)
		}
	}
	RunImageTasksFunc = func(ctx context.Context, tasks []*model.Task) error {
		require.Len(t, tasks, 1)
		if atomic.AddInt32(&calls, 1) == 1 {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
	t.Cleanup(func() {
		closeRelease()
		RunImageTasksFunc = oldRunImageTasks
		constant.ImageTaskWorkerConcurrency = oldWorkerConcurrency
		constant.ImageTaskChannelConcurrency = oldChannelConcurrency
		constant.ImageTaskLeaseSeconds = oldLeaseSeconds
	})

	now := time.Now().Unix()
	first := &model.Task{
		TaskID:     "task_image_dispatch_first",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		NextPollAt: now - 1,
		PrivateData: model.TaskPrivateData{
			ImageTaskMode: dto.ImageTaskModeGPTImage2APIAsync,
		},
	}
	second := &model.Task{
		TaskID:     "task_image_dispatch_second",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		NextPollAt: now - 1,
		PrivateData: model.TaskPrivateData{
			ImageTaskMode: dto.ImageTaskModeGPTImage2APIAsync,
		},
	}
	require.NoError(t, model.DB.Create(first).Error)
	require.NoError(t, model.DB.Create(second).Error)

	go func() {
		DispatchImageTasks(context.Background(), []*model.Task{first, second})
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first task did not start")
	}

	var queued model.Task
	require.NoError(t, model.DB.Where("task_id = ?", second.TaskID).First(&queued).Error)
	require.Empty(t, queued.LockOwner)
	require.Zero(t, queued.LockUntil)

	closeRelease()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("dispatch did not finish")
	}
	require.Equal(t, int32(2), atomic.LoadInt32(&calls))
}

func TestDispatchImageTasksSaturatedChannelDoesNotBlockOtherChannels(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Exec("DELETE FROM tasks").Error)
	oldRunImageTasks := RunImageTasksFunc
	oldWorkerConcurrency := constant.ImageTaskWorkerConcurrency
	oldChannelConcurrency := constant.ImageTaskChannelConcurrency
	oldLeaseSeconds := constant.ImageTaskLeaseSeconds
	constant.ImageTaskWorkerConcurrency = 2
	constant.ImageTaskChannelConcurrency = 1
	constant.ImageTaskLeaseSeconds = 60
	channelOneStarted := make(chan struct{})
	releaseChannelOne := make(chan struct{})
	channelTwoStarted := make(chan struct{})
	done := make(chan struct{})
	var channelOneStartedClosed int32
	var channelTwoStartedClosed int32
	var releaseClosed int32
	var calls int32
	closeRelease := func() {
		if atomic.CompareAndSwapInt32(&releaseClosed, 0, 1) {
			close(releaseChannelOne)
		}
	}
	RunImageTasksFunc = func(ctx context.Context, tasks []*model.Task) error {
		require.Len(t, tasks, 1)
		task := tasks[0]
		atomic.AddInt32(&calls, 1)
		switch task.ChannelId {
		case 1:
			if atomic.CompareAndSwapInt32(&channelOneStartedClosed, 0, 1) {
				close(channelOneStarted)
				select {
				case <-releaseChannelOne:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		case 2:
			if atomic.CompareAndSwapInt32(&channelTwoStartedClosed, 0, 1) {
				close(channelTwoStarted)
			}
		}
		return nil
	}
	t.Cleanup(func() {
		closeRelease()
		RunImageTasksFunc = oldRunImageTasks
		constant.ImageTaskWorkerConcurrency = oldWorkerConcurrency
		constant.ImageTaskChannelConcurrency = oldChannelConcurrency
		constant.ImageTaskLeaseSeconds = oldLeaseSeconds
	})

	now := time.Now().Unix()
	firstSameChannel := &model.Task{
		TaskID:     "task_image_channel_block_first",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		NextPollAt: now - 1,
		PrivateData: model.TaskPrivateData{
			ImageTaskMode: dto.ImageTaskModeGPTImage2APIAsync,
		},
	}
	secondSameChannel := &model.Task{
		TaskID:     "task_image_channel_block_second",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		NextPollAt: now - 1,
		PrivateData: model.TaskPrivateData{
			ImageTaskMode: dto.ImageTaskModeGPTImage2APIAsync,
		},
	}
	otherChannel := &model.Task{
		TaskID:     "task_image_channel_block_other",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  2,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		NextPollAt: now - 1,
		PrivateData: model.TaskPrivateData{
			ImageTaskMode: dto.ImageTaskModeGPTImage2APIAsync,
		},
	}
	require.NoError(t, model.DB.Create(firstSameChannel).Error)
	require.NoError(t, model.DB.Create(secondSameChannel).Error)
	require.NoError(t, model.DB.Create(otherChannel).Error)

	go func() {
		DispatchImageTasks(context.Background(), []*model.Task{firstSameChannel, secondSameChannel, otherChannel})
		close(done)
	}()

	select {
	case <-channelOneStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("channel one task did not start")
	}

	select {
	case <-channelTwoStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("other channel task was blocked by saturated channel")
	}

	var channelOneTasks []model.Task
	require.NoError(t, model.DB.Where("channel_id = ?", 1).Order("id").Find(&channelOneTasks).Error)
	require.Len(t, channelOneTasks, 2)
	leasedCount := 0
	for _, task := range channelOneTasks {
		if task.LockOwner != "" || task.LockUntil != 0 {
			leasedCount++
		}
	}
	require.Equal(t, 1, leasedCount)

	closeRelease()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("dispatch did not finish")
	}
	require.Equal(t, int32(3), atomic.LoadInt32(&calls))
}

func TestDispatchImageTasksHonorsGlobalChannelLease(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Exec("DELETE FROM tasks").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM image_task_channel_leases").Error)
	oldRunImageTasks := RunImageTasksFunc
	oldWorkerConcurrency := constant.ImageTaskWorkerConcurrency
	oldChannelConcurrency := constant.ImageTaskChannelConcurrency
	oldLeaseSeconds := constant.ImageTaskLeaseSeconds
	constant.ImageTaskWorkerConcurrency = 2
	constant.ImageTaskChannelConcurrency = 1
	constant.ImageTaskLeaseSeconds = 60
	var calls int32
	RunImageTasksFunc = func(ctx context.Context, tasks []*model.Task) error {
		atomic.AddInt32(&calls, 1)
		return nil
	}
	t.Cleanup(func() {
		RunImageTasksFunc = oldRunImageTasks
		constant.ImageTaskWorkerConcurrency = oldWorkerConcurrency
		constant.ImageTaskChannelConcurrency = oldChannelConcurrency
		constant.ImageTaskLeaseSeconds = oldLeaseSeconds
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:     "task_image_global_channel_blocked",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		NextPollAt: now - 1,
		PrivateData: model.TaskPrivateData{
			ImageTaskMode: dto.ImageTaskModeGPTImage2APIAsync,
		},
	}
	sameChannelSkipped := &model.Task{
		TaskID:     "task_image_global_channel_skipped",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		NextPollAt: now - 1,
		PrivateData: model.TaskPrivateData{
			ImageTaskMode: dto.ImageTaskModeGPTImage2APIAsync,
		},
	}
	require.NoError(t, model.DB.Create(task).Error)
	require.NoError(t, model.DB.Create(sameChannelSkipped).Error)
	acquired, err := model.TryAcquireImageTaskChannelLease(1, task.ID+1000, "remote-owner", now, 60, 1)
	require.NoError(t, err)
	require.True(t, acquired)

	DispatchImageTasks(context.Background(), []*model.Task{task, sameChannelSkipped})

	require.Zero(t, atomic.LoadInt32(&calls))
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	require.Empty(t, reloaded.LockOwner)
	require.Zero(t, reloaded.LockUntil)
	require.GreaterOrEqual(t, reloaded.NextPollAt, now+imageTaskChannelSaturationBackoffSeconds())
	var skipped model.Task
	require.NoError(t, model.DB.First(&skipped, sameChannelSkipped.ID).Error)
	require.Empty(t, skipped.LockOwner)
	require.Zero(t, skipped.LockUntil)
	require.GreaterOrEqual(t, skipped.NextPollAt, now+imageTaskChannelSaturationBackoffSeconds())
	count, err := model.CountActiveImageTaskChannelLeases(1, now)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
}

func TestDispatchImageTasksBacksOffTransientRetry(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Exec("DELETE FROM tasks").Error)
	oldRunImageTasks := RunImageTasksFunc
	oldWorkerConcurrency := constant.ImageTaskWorkerConcurrency
	oldChannelConcurrency := constant.ImageTaskChannelConcurrency
	oldLeaseSeconds := constant.ImageTaskLeaseSeconds
	constant.ImageTaskWorkerConcurrency = 1
	constant.ImageTaskChannelConcurrency = 1
	constant.ImageTaskLeaseSeconds = 60
	RunImageTasksFunc = func(ctx context.Context, tasks []*model.Task) error {
		require.Len(t, tasks, 1)
		tasks[0].RetryCount++
		return nil
	}
	t.Cleanup(func() {
		RunImageTasksFunc = oldRunImageTasks
		constant.ImageTaskWorkerConcurrency = oldWorkerConcurrency
		constant.ImageTaskChannelConcurrency = oldChannelConcurrency
		constant.ImageTaskLeaseSeconds = oldLeaseSeconds
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:     "task_image_dispatch_backoff",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusSubmitted,
		Progress:   "0%",
		SubmitTime: now,
		NextPollAt: now - 1,
		PrivateData: model.TaskPrivateData{
			ImageTaskMode:   dto.ImageTaskModeGPTImage2APIAsync,
			UpstreamTaskID:  "upstream_123",
			RequestBodyPath: "kept-for-settlement.json",
		},
	}
	require.NoError(t, model.DB.Create(task).Error)

	DispatchImageTasks(context.Background(), []*model.Task{task})

	var reloaded model.Task
	require.NoError(t, model.DB.Where("task_id = ?", task.TaskID).First(&reloaded).Error)
	require.Equal(t, 1, reloaded.RetryCount)
	require.Empty(t, reloaded.LockOwner)
	require.Zero(t, reloaded.LockUntil)
	require.GreaterOrEqual(t, reloaded.NextPollAt, now+4)
}

func TestDispatchImageTasksRecoversWorkerPanic(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Exec("DELETE FROM tasks").Error)
	oldRunImageTasks := RunImageTasksFunc
	oldWorkerConcurrency := constant.ImageTaskWorkerConcurrency
	oldChannelConcurrency := constant.ImageTaskChannelConcurrency
	oldLeaseSeconds := constant.ImageTaskLeaseSeconds
	constant.ImageTaskWorkerConcurrency = 1
	constant.ImageTaskChannelConcurrency = 1
	constant.ImageTaskLeaseSeconds = 60
	var calls int32
	RunImageTasksFunc = func(ctx context.Context, tasks []*model.Task) error {
		require.Len(t, tasks, 1)
		if atomic.AddInt32(&calls, 1) == 1 {
			panic("simulated image worker panic")
		}
		return nil
	}
	t.Cleanup(func() {
		RunImageTasksFunc = oldRunImageTasks
		constant.ImageTaskWorkerConcurrency = oldWorkerConcurrency
		constant.ImageTaskChannelConcurrency = oldChannelConcurrency
		constant.ImageTaskLeaseSeconds = oldLeaseSeconds
	})

	now := time.Now().Unix()
	for i := 0; i < 2; i++ {
		task := &model.Task{
			TaskID:     fmt.Sprintf("task_image_panic_recover_%d", i),
			Platform:   constant.TaskPlatformImage,
			UserId:     1,
			Group:      "default",
			ChannelId:  1,
			Status:     model.TaskStatusQueued,
			Progress:   "0%",
			SubmitTime: now,
			NextPollAt: now - 1,
			PrivateData: model.TaskPrivateData{
				ImageTaskMode: dto.ImageTaskModeGPTImage2APIAsync,
			},
		}
		require.NoError(t, model.DB.Create(task).Error)
	}

	require.NotPanics(t, func() {
		DispatchImageTasks(context.Background(), model.GetRunnableImageTasks(2, now))
	})
	require.Equal(t, int32(2), atomic.LoadInt32(&calls))
}

func TestRunImageTaskWorkerPassClaimsRunnableTasks(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Exec("DELETE FROM tasks").Error)
	oldRunImageTasks := RunImageTasksFunc
	oldUpdateTask := constant.UpdateTask
	oldTaskQueryLimit := constant.TaskQueryLimit
	oldWorkerConcurrency := constant.ImageTaskWorkerConcurrency
	oldChannelConcurrency := constant.ImageTaskChannelConcurrency
	oldLeaseSeconds := constant.ImageTaskLeaseSeconds
	constant.UpdateTask = true
	constant.TaskQueryLimit = 10
	constant.ImageTaskWorkerConcurrency = 2
	constant.ImageTaskChannelConcurrency = 1
	constant.ImageTaskLeaseSeconds = 60
	var calls int32
	RunImageTasksFunc = func(ctx context.Context, tasks []*model.Task) error {
		require.Len(t, tasks, 1)
		atomic.AddInt32(&calls, 1)
		return nil
	}
	t.Cleanup(func() {
		RunImageTasksFunc = oldRunImageTasks
		constant.UpdateTask = oldUpdateTask
		constant.TaskQueryLimit = oldTaskQueryLimit
		constant.ImageTaskWorkerConcurrency = oldWorkerConcurrency
		constant.ImageTaskChannelConcurrency = oldChannelConcurrency
		constant.ImageTaskLeaseSeconds = oldLeaseSeconds
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:     "task_image_worker_pass",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		NextPollAt: now - 1,
		PrivateData: model.TaskPrivateData{
			ImageTaskMode: dto.ImageTaskModeGPTImage2APIAsync,
		},
	}
	require.NoError(t, model.DB.Create(task).Error)

	runImageTaskWorkerPass(context.Background())

	require.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestDispatchImageTasksPreservesRetryWhenProgressingIntoPendingSettlement(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Exec("DELETE FROM tasks").Error)
	oldRunImageTasks := RunImageTasksFunc
	oldWorkerConcurrency := constant.ImageTaskWorkerConcurrency
	oldChannelConcurrency := constant.ImageTaskChannelConcurrency
	oldLeaseSeconds := constant.ImageTaskLeaseSeconds
	constant.ImageTaskWorkerConcurrency = 1
	constant.ImageTaskChannelConcurrency = 1
	constant.ImageTaskLeaseSeconds = 60
	RunImageTasksFunc = func(ctx context.Context, tasks []*model.Task) error {
		require.Len(t, tasks, 1)
		task := tasks[0]
		task.Status = model.TaskStatusSuccess
		task.Progress = "100%"
		task.FinishTime = time.Now().Unix()
		task.NextPollAt = 0
		task.SettlementStatus = model.TaskSettlementStatusPending
		task.RetryCount++
		owner := ImageTaskLeaseOwnerFromContext(ctx)
		won, err := task.UpdateWithStatusAndLease(model.TaskStatusSubmitted, owner, time.Now().Unix())
		require.NoError(t, err)
		require.True(t, won)
		return nil
	}
	t.Cleanup(func() {
		RunImageTasksFunc = oldRunImageTasks
		constant.ImageTaskWorkerConcurrency = oldWorkerConcurrency
		constant.ImageTaskChannelConcurrency = oldChannelConcurrency
		constant.ImageTaskLeaseSeconds = oldLeaseSeconds
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:     "task_image_pending_settlement_backoff",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusSubmitted,
		Progress:   "0%",
		SubmitTime: now,
		NextPollAt: now - 1,
		PrivateData: model.TaskPrivateData{
			ImageTaskMode:   dto.ImageTaskModeGPTImage2APIAsync,
			UpstreamTaskID:  "upstream_pending_settlement",
			RequestBodyPath: "kept-for-settlement.json",
		},
	}
	require.NoError(t, model.DB.Create(task).Error)

	DispatchImageTasks(context.Background(), []*model.Task{task})

	var reloaded model.Task
	require.NoError(t, model.DB.Where("task_id = ?", task.TaskID).First(&reloaded).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	require.Equal(t, model.TaskSettlementStatusPending, reloaded.SettlementStatus)
	require.Equal(t, 1, reloaded.RetryCount)
	require.Empty(t, reloaded.LockOwner)
	require.Zero(t, reloaded.LockUntil)
	require.GreaterOrEqual(t, reloaded.NextPollAt, now+4)
}

func TestDispatchImageTasksPreservesRetryWhenProgressingIntoAppliedSettlement(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Exec("DELETE FROM tasks").Error)
	oldRunImageTasks := RunImageTasksFunc
	oldWorkerConcurrency := constant.ImageTaskWorkerConcurrency
	oldChannelConcurrency := constant.ImageTaskChannelConcurrency
	oldLeaseSeconds := constant.ImageTaskLeaseSeconds
	constant.ImageTaskWorkerConcurrency = 1
	constant.ImageTaskChannelConcurrency = 1
	constant.ImageTaskLeaseSeconds = 60
	RunImageTasksFunc = func(ctx context.Context, tasks []*model.Task) error {
		require.Len(t, tasks, 1)
		task := tasks[0]
		task.Status = model.TaskStatusSuccess
		task.Progress = "100%"
		task.FinishTime = time.Now().Unix()
		task.NextPollAt = 0
		task.SettlementStatus = model.TaskSettlementStatusApplied
		task.RetryCount++
		owner := ImageTaskLeaseOwnerFromContext(ctx)
		won, err := task.UpdateWithStatusAndLease(model.TaskStatusSubmitted, owner, time.Now().Unix())
		require.NoError(t, err)
		require.True(t, won)
		return nil
	}
	t.Cleanup(func() {
		RunImageTasksFunc = oldRunImageTasks
		constant.ImageTaskWorkerConcurrency = oldWorkerConcurrency
		constant.ImageTaskChannelConcurrency = oldChannelConcurrency
		constant.ImageTaskLeaseSeconds = oldLeaseSeconds
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:     "task_image_applied_settlement_backoff",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusSubmitted,
		Progress:   "0%",
		SubmitTime: now,
		NextPollAt: now - 1,
		PrivateData: model.TaskPrivateData{
			ImageTaskMode:   dto.ImageTaskModeGPTImage2APIAsync,
			UpstreamTaskID:  "upstream_applied_settlement",
			RequestBodyPath: "kept-for-finalize.json",
		},
	}
	require.NoError(t, model.DB.Create(task).Error)

	DispatchImageTasks(context.Background(), []*model.Task{task})

	var reloaded model.Task
	require.NoError(t, model.DB.Where("task_id = ?", task.TaskID).First(&reloaded).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	require.Equal(t, model.TaskSettlementStatusApplied, reloaded.SettlementStatus)
	require.Equal(t, 1, reloaded.RetryCount)
	require.Empty(t, reloaded.LockOwner)
	require.Zero(t, reloaded.LockUntil)
	require.GreaterOrEqual(t, reloaded.NextPollAt, now+4)
}

func TestImageTaskRunnableQueriesTreatNullSchedulingFieldsAsReady(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Exec("DELETE FROM tasks").Error)

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:     "task_image_null_schedule",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		PrivateData: model.TaskPrivateData{
			ImageTaskMode: dto.ImageTaskModeGPTImage2APIAsync,
		},
	}
	require.NoError(t, model.DB.Create(task).Error)
	require.NoError(t, model.DB.Exec("UPDATE tasks SET next_poll_at = NULL, lock_until = NULL WHERE id = ?", task.ID).Error)

	require.True(t, model.HasRunnableImageTasks(now))
	tasks := model.GetRunnableUnfinishedSyncTasks(10, now)
	require.Len(t, tasks, 1)
	require.Equal(t, task.TaskID, tasks[0].TaskID)

	claimed, ok, err := model.ClaimTaskLease(task.ID, "test-owner", now, 60)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, claimed)
	require.Equal(t, "test-owner", claimed.LockOwner)
}

func TestImageTaskRunnableQueriesIncludePendingSettlementSuccess(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Exec("DELETE FROM tasks").Error)

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_image_pending_settlement",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now - 60,
		FinishTime:       now - 10,
		NextPollAt:       now - 1,
		SettlementStatus: model.TaskSettlementStatusPending,
		PrivateData: model.TaskPrivateData{
			ImageTaskMode:  dto.ImageTaskModeGPTImage2APIAsync,
			UpstreamTaskID: "upstream_pending_settlement",
		},
	}
	require.NoError(t, model.DB.Create(task).Error)

	require.True(t, model.HasRunnableImageTasks(now))
	tasks := model.GetRunnableUnfinishedSyncTasks(10, now)
	require.Len(t, tasks, 1)
	require.Equal(t, task.TaskID, tasks[0].TaskID)

	claimed, ok, err := model.ClaimTaskLease(task.ID, "settlement-owner", now, 60)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, claimed)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), claimed.Status)
	require.Equal(t, model.TaskSettlementStatusPending, claimed.SettlementStatus)

	renewed, err := model.RenewTaskLease(task.ID, "settlement-owner", now+1, 60)
	require.NoError(t, err)
	require.True(t, renewed)
}

func TestImageTaskRunnableQueriesIncludeAppliedSettlementSuccess(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Exec("DELETE FROM tasks").Error)

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_image_applied_settlement",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now - 60,
		FinishTime:       now - 10,
		NextPollAt:       now - 1,
		SettlementStatus: model.TaskSettlementStatusApplied,
		PrivateData: model.TaskPrivateData{
			ImageTaskMode:  dto.ImageTaskModeGPTImage2APIAsync,
			UpstreamTaskID: "upstream_applied_settlement",
		},
	}
	require.NoError(t, model.DB.Create(task).Error)

	require.True(t, model.HasRunnableImageTasks(now))
	tasks := model.GetRunnableUnfinishedSyncTasks(10, now)
	require.Len(t, tasks, 1)
	require.Equal(t, task.TaskID, tasks[0].TaskID)

	claimed, ok, err := model.ClaimTaskLease(task.ID, "settlement-owner", now, 60)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, claimed)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), claimed.Status)
	require.Equal(t, model.TaskSettlementStatusApplied, claimed.SettlementStatus)
}

func TestGetNextRunnableImageTaskAtUsesPollAndLeaseDueTime(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Exec("DELETE FROM tasks").Error)

	now := time.Now().Unix()
	earlier := &model.Task{
		TaskID:     "task_image_next_earlier",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusSubmitted,
		Progress:   "0%",
		SubmitTime: now,
		NextPollAt: now + 10,
		LockUntil:  0,
	}
	laterByLease := &model.Task{
		TaskID:     "task_image_next_later_by_lease",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusSubmitted,
		Progress:   "0%",
		SubmitTime: now,
		NextPollAt: now + 1,
		LockUntil:  now + 20,
	}
	require.NoError(t, model.DB.Create(earlier).Error)
	require.NoError(t, model.DB.Create(laterByLease).Error)

	nextAt, ok := model.GetNextRunnableImageTaskAt(now)
	require.True(t, ok)
	require.Equal(t, now+10, nextAt)
}

// ===========================================================================
// RefundTaskQuota tests
// ===========================================================================

func TestRefundTaskQuota_Wallet(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1, 1, 1
	const initQuota, preConsumed = 10000, 3000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-test-key", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)

	RefundTaskQuota(ctx, task, "task failed: upstream error")

	// User quota should increase by preConsumed
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))

	// Token remain_quota should increase, used_quota should decrease
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, -preConsumed, getTokenUsedQuota(t, tokenID))

	// A refund log should be created
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed, log.Quota)
	assert.Equal(t, "test-model", log.ModelName)
}

func TestRefundTaskQuota_Subscription(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID, subID = 2, 2, 2, 1
	const preConsumed = 2000
	const subTotal, subUsed int64 = 100000, 50000
	const tokenRemain = 8000

	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "sk-sub-key", tokenRemain)
	seedChannel(t, channelID)
	seedSubscription(t, subID, userID, subTotal, subUsed)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceSubscription, subID)

	RefundTaskQuota(ctx, task, "subscription task failed")

	// Subscription used should decrease by preConsumed
	assert.Equal(t, subUsed-int64(preConsumed), getSubscriptionUsed(t, subID))

	// Token should also be refunded
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

func TestRefundTaskQuota_ZeroQuota(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID = 3
	seedUser(t, userID, 5000)

	task := makeTask(userID, 0, 0, 0, BillingSourceWallet, 0)

	RefundTaskQuota(ctx, task, "zero quota task")

	// No change to user quota
	assert.Equal(t, 5000, getUserQuota(t, userID))

	// No log created
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRefundTaskQuota_NoToken(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, channelID = 4, 4
	const initQuota, preConsumed = 10000, 1500

	seedUser(t, userID, initQuota)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, 0, BillingSourceWallet, 0) // TokenId=0

	RefundTaskQuota(ctx, task, "no token task failed")

	// User quota refunded
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))

	// Log created
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

// ===========================================================================
// RecalculateTaskQuota tests
// ===========================================================================

func TestRecalculate_PositiveDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 10, 10, 10
	const initQuota, preConsumed = 10000, 2000
	const actualQuota = 3000 // under-charged by 1000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-pos", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)

	RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment")

	// User quota should decrease by the delta (1000 additional charge)
	assert.Equal(t, initQuota-(actualQuota-preConsumed), getUserQuota(t, userID))

	// Token should also be charged the delta
	assert.Equal(t, tokenRemain-(actualQuota-preConsumed), getTokenRemainQuota(t, tokenID))

	// task.Quota should be updated to actualQuota
	assert.Equal(t, actualQuota, task.Quota)

	// Log type should be Consume (additional charge)
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeConsume, log.Type)
	assert.Equal(t, actualQuota-preConsumed, log.Quota)
}

func TestRecalculate_NegativeDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 11, 11, 11
	const initQuota, preConsumed = 10000, 5000
	const actualQuota = 3000 // over-charged by 2000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-neg", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)

	RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment")

	// User quota should increase by abs(delta) = 2000 (refund overpayment)
	assert.Equal(t, initQuota+(preConsumed-actualQuota), getUserQuota(t, userID))

	// Token should be refunded the difference
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))

	// task.Quota updated
	assert.Equal(t, actualQuota, task.Quota)

	// Log type should be Refund
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed-actualQuota, log.Quota)
}

func TestRecalculate_ZeroDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID = 12
	const initQuota, preConsumed = 10000, 3000

	seedUser(t, userID, initQuota)

	task := makeTask(userID, 0, preConsumed, 0, BillingSourceWallet, 0)

	RecalculateTaskQuota(ctx, task, preConsumed, "exact match")

	// No change to user quota
	assert.Equal(t, initQuota, getUserQuota(t, userID))

	// No log created (delta is zero)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRecalculate_ActualQuotaZero(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID = 13
	const initQuota = 10000

	seedUser(t, userID, initQuota)

	task := makeTask(userID, 0, 5000, 0, BillingSourceWallet, 0)

	RecalculateTaskQuota(ctx, task, 0, "zero actual")

	// No change (early return)
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRecalculate_Subscription_NegativeDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID, subID = 14, 14, 14, 2
	const preConsumed = 5000
	const actualQuota = 2000 // over-charged by 3000
	const subTotal, subUsed int64 = 100000, 50000
	const tokenRemain = 8000

	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "sk-sub-recalc", tokenRemain)
	seedChannel(t, channelID)
	seedSubscription(t, subID, userID, subTotal, subUsed)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceSubscription, subID)

	RecalculateTaskQuota(ctx, task, actualQuota, "subscription over-charge")

	// Subscription used should decrease by delta (refund 3000)
	assert.Equal(t, subUsed-int64(preConsumed-actualQuota), getSubscriptionUsed(t, subID))

	// Token refunded
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))

	assert.Equal(t, actualQuota, task.Quota)

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

// ===========================================================================
// CAS + Billing integration tests
// Simulates the flow in updateVideoSingleTask (service/task_polling.go)
// ===========================================================================

// simulatePollBilling reproduces the CAS + billing logic from updateVideoSingleTask.
// It takes a persisted task (already in DB), applies the new status, and performs
// the conditional update + billing exactly as the polling loop does.
func simulatePollBilling(ctx context.Context, task *model.Task, newStatus model.TaskStatus, actualQuota int) {
	snap := task.Snapshot()

	shouldRefund := false
	shouldSettle := false
	quota := task.Quota

	task.Status = newStatus
	switch string(newStatus) {
	case model.TaskStatusSuccess:
		task.Progress = "100%"
		task.FinishTime = 9999
		shouldSettle = true
	case model.TaskStatusFailure:
		task.Progress = "100%"
		task.FinishTime = 9999
		task.FailReason = "upstream error"
		if quota != 0 {
			shouldRefund = true
		}
	default:
		task.Progress = "50%"
	}

	isDone := task.Status == model.TaskStatus(model.TaskStatusSuccess) || task.Status == model.TaskStatus(model.TaskStatusFailure)
	if isDone && snap.Status != task.Status {
		won, err := task.UpdateWithStatus(snap.Status)
		if err != nil {
			shouldRefund = false
			shouldSettle = false
		} else if !won {
			shouldRefund = false
			shouldSettle = false
		}
	} else if !snap.Equal(task.Snapshot()) {
		_, _ = task.UpdateWithStatus(snap.Status)
	}

	if shouldSettle && actualQuota > 0 {
		RecalculateTaskQuota(ctx, task, actualQuota, "test settle")
	}
	if shouldRefund {
		RefundTaskQuota(ctx, task, task.FailReason)
	}
}

func TestCASGuardedRefund_Win(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 20, 20, 20
	const initQuota, preConsumed = 10000, 4000
	const tokenRemain = 6000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-cas-refund-win", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusFailure), 0)

	// CAS wins: task in DB should now be FAILURE
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, reloaded.Status)

	// Refund should have happened
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

func TestCASGuardedRefund_Lose(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 21, 21, 21
	const initQuota, preConsumed = 10000, 4000
	const tokenRemain = 6000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-cas-refund-lose", tokenRemain)
	seedChannel(t, channelID)

	// Create task with IN_PROGRESS in DB
	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	// Simulate another process already transitioning to FAILURE
	model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Update("status", model.TaskStatusFailure)

	// Our process still has the old in-memory state (IN_PROGRESS) and tries to transition
	// task.Status is still IN_PROGRESS in the snapshot
	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusFailure), 0)

	// CAS lost: user quota should NOT change (no double refund)
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))

	// No billing log should be created
	assert.Equal(t, int64(0), countLogs(t))
}

func TestCASGuardedSettle_Win(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 22, 22, 22
	const initQuota, preConsumed = 10000, 5000
	const actualQuota = 3000 // over-charged, should get partial refund
	const tokenRemain = 8000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-cas-settle-win", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusSuccess), actualQuota)

	// CAS wins: task should be SUCCESS
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, reloaded.Status)

	// Settlement should refund the over-charge (5000 - 3000 = 2000 back to user)
	assert.Equal(t, initQuota+(preConsumed-actualQuota), getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))

	// task.Quota should be updated to actualQuota
	assert.Equal(t, actualQuota, task.Quota)
}

func TestNonTerminalUpdate_NoBilling(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, channelID = 23, 23
	const initQuota, preConsumed = 10000, 3000

	seedUser(t, userID, initQuota)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, 0, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	task.Progress = "20%"
	require.NoError(t, model.DB.Create(task).Error)

	// Simulate a non-terminal poll update (still IN_PROGRESS, progress changed)
	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusInProgress), 0)

	// User quota should NOT change
	assert.Equal(t, initQuota, getUserQuota(t, userID))

	// No billing log
	assert.Equal(t, int64(0), countLogs(t))

	// Task progress should be updated in DB
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, "50%", reloaded.Progress)
}

// ===========================================================================
// Mock adaptor for settleTaskBillingOnComplete tests
// ===========================================================================

type mockAdaptor struct {
	adjustReturn int
}

func (m *mockAdaptor) Init(_ *relaycommon.RelayInfo) {}
func (m *mockAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	return nil, nil
}
func (m *mockAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) { return nil, nil }
func (m *mockAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return m.adjustReturn
}

type signalTaskPollingAdaptor struct {
	started chan struct{}
	closed  int32
}

func (m *signalTaskPollingAdaptor) Init(_ *relaycommon.RelayInfo) {}

func (m *signalTaskPollingAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	if atomic.CompareAndSwapInt32(&m.closed, 0, 1) {
		close(m.started)
	}
	body := `{"code":"success","data":[{"task_id":"suno_task_parallel_platform","status":"IN_PROGRESS","data":{}}]}`
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

func (m *signalTaskPollingAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return nil, nil
}

func (m *signalTaskPollingAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return 0
}

// ===========================================================================
// PerCallBilling tests — settleTaskBillingOnComplete
// ===========================================================================

func TestSettle_PerCallBilling_SkipsAdaptorAdjust(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 30, 30, 30
	const initQuota, preConsumed = 10000, 5000
	const tokenRemain = 8000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-percall-adaptor", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.PerCallBilling = true

	adaptor := &mockAdaptor{adjustReturn: 2000}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	// Per-call: no adjustment despite adaptor returning 2000
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestSettle_PerCallBilling_SkipsTotalTokens(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 31, 31, 31
	const initQuota, preConsumed = 10000, 4000
	const tokenRemain = 7000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-percall-tokens", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.PerCallBilling = true

	adaptor := &mockAdaptor{adjustReturn: 0}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, TotalTokens: 9999}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	// Per-call: no recalculation by tokens
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestSettle_NonPerCallBilling_AppliesAdaptorAdjustment(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 32, 32, 32
	const initQuota, preConsumed = 10000, 5000
	const adaptorQuota = 3000
	const tokenRemain = 8000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-nonpercall-adj", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	// PerCallBilling defaults to false

	adaptor := &mockAdaptor{adjustReturn: adaptorQuota}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	// Non-per-call: adaptor adjustment applies (refund 2000)
	assert.Equal(t, initQuota+(preConsumed-adaptorQuota), getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+(preConsumed-adaptorQuota), getTokenRemainQuota(t, tokenID))
	assert.Equal(t, adaptorQuota, task.Quota)

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}
