package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
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
		&model.Midjourney{},
		&model.MidjourneySettlementRecord{},
		&model.TaskSettlementRecord{},
		&model.User{},
		&model.UserLoginIdentifier{},
		&model.Token{},
		&model.Log{},
		&model.QuotaData{},
		&model.TokenUsageDaily{},
		&model.Channel{},
		&model.TopUp{},
		&model.SubscriptionPlan{},
		&model.UserSubscription{},
		&model.SubscriptionPreConsumeRecord{},
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
		model.DB.Exec("DELETE FROM midjourneys")
		model.DB.Exec("DELETE FROM midjourney_settlement_records")
		model.DB.Exec("DELETE FROM task_settlement_records")
		model.DB.Exec("DELETE FROM users")
		model.DB.Exec("DELETE FROM tokens")
		model.DB.Exec("DELETE FROM logs")
		model.DB.Exec("DELETE FROM quota_data")
		model.DB.Exec("DELETE FROM token_usage_dailies")
		model.DB.Exec("DELETE FROM channels")
		model.DB.Exec("DELETE FROM top_ups")
		model.DB.Exec("DELETE FROM subscription_plans")
		model.DB.Exec("DELETE FROM user_subscriptions")
		model.DB.Exec("DELETE FROM subscription_pre_consume_records")
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

func TestRollbackRefundedTaskTokenQuotaPreservesConcurrentTokenConsumption(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	require.NoError(t, model.DB.Create(&model.Token{
		Id:          91001,
		UserId:      91001,
		Key:         "task-refund-token",
		RemainQuota: 100,
		UsedQuota:   50,
	}).Error)

	task := &model.Task{
		TaskID: "task-refund-token-rollback",
		UserId: 91001,
		PrivateData: model.TaskPrivateData{
			TokenId: 91001,
		},
	}
	tokenAdjusted, tokenDelta, err := taskAdjustRefundTokenQuota(ctx, task, -30)
	require.NoError(t, err)
	require.True(t, tokenAdjusted)
	require.NoError(t, model.DecreaseTokenQuota(91001, "task-refund-token", 20))
	require.NoError(t, rollbackRefundedTaskTokenQuota(ctx, task, tokenDelta, 30))

	var token model.Token
	require.NoError(t, model.DB.First(&token, 91001).Error)
	require.Equal(t, 80, token.RemainQuota)
	require.Equal(t, 70, token.UsedQuota)
}

func TestMidjourneyUsageRollbackPreservesConcurrentUsage(t *testing.T) {
	truncate(t)

	require.NoError(t, model.DB.Create(&model.User{Id: 91002, Username: "mj-user", UsedQuota: 100}).Error)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 91002, Name: "mj-channel", UsedQuota: 100}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: 91002, UserId: 91002, Key: "mj-token", RemainQuota: 1000, UsedQuota: 100}).Error)

	task := &model.Midjourney{
		UserId:     91002,
		ChannelId:  91002,
		TokenId:    91002,
		SubmitTime: 1710000000,
	}
	tokenAdjusted, tokenDelta, err := refundMidjourneyTokenQuota(context.Background(), task, 30)
	require.NoError(t, err)
	require.True(t, tokenAdjusted)
	require.NoError(t, updateMidjourneyUsageCounters(task, -30, true, true))
	require.NoError(t, model.UpdateTaskUsageAdjustmentWithTokenAtSync(91002, 91002, 91002, 20, 1710000001))
	require.NoError(t, model.DecreaseTokenQuota(91002, "mj-token", 20))
	require.NoError(t, updateMidjourneyUsageCounters(task, 30, true))
	require.NoError(t, rollbackMidjourneyTokenRefund(context.Background(), task, tokenDelta, 30))

	var user model.User
	require.NoError(t, model.DB.First(&user, 91002).Error)
	require.Equal(t, 120, user.UsedQuota)

	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, 91002).Error)
	require.Equal(t, int64(120), channel.UsedQuota)

	var usage model.TokenUsageDaily
	require.NoError(t, model.DB.First(&usage, "token_id = ? AND date = ?", 91002, midjourneyTokenUsageDate(1710000000)).Error)
	require.Equal(t, 20, usage.Quota)

	var token model.Token
	require.NoError(t, model.DB.First(&token, 91002).Error)
	require.Equal(t, 980, token.RemainQuota)
	require.Equal(t, 120, token.UsedQuota)
}

func TestRefundMidjourneyTaskQuotaFinalizesAppliedRecordWithoutDoubleRefund(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	require.NoError(t, model.DB.Create(&model.User{
		Id:        91003,
		Username:  "mj-refund-owner",
		Quota:     75,
		UsedQuota: 25,
		Status:    common.UserStatusEnabled,
	}).Error)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 91003, Name: "mj-channel", UsedQuota: 25}).Error)
	require.NoError(t, model.DB.Create(&model.Token{
		Id:          91003,
		UserId:      91003,
		Key:         "mj-refund-token",
		Name:        "mj-refund-token",
		RemainQuota: 75,
		UsedQuota:   25,
		Status:      common.TokenStatusEnabled,
	}).Error)
	model.RecordTokenUsage(91003, 91003, 25, 1710000000)
	task := &model.Midjourney{
		UserId:     91003,
		Action:     constant.MjActionImagine,
		MjId:       "mj-refund-applied-retry",
		Status:     "FAILURE",
		Progress:   "100%",
		ChannelId:  91003,
		Quota:      25,
		TokenId:    91003,
		Group:      "default",
		SubmitTime: 1710000000,
	}
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RefundMidjourneyTaskQuota(ctx, task, "upstream failed"))
	require.NoError(t, model.DB.Model(&model.Midjourney{}).Where("id = ?", task.Id).Update("quota", 25).Error)
	task.Quota = 25
	require.NoError(t, RefundMidjourneyTaskQuota(ctx, task, "retry after applied record"))

	var user model.User
	require.NoError(t, model.DB.Select("quota", "used_quota").First(&user, 91003).Error)
	require.Equal(t, 100, user.Quota)
	require.Zero(t, user.UsedQuota)
	var token model.Token
	require.NoError(t, model.DB.Select("remain_quota", "used_quota").First(&token, 91003).Error)
	require.Equal(t, 100, token.RemainQuota)
	require.Zero(t, token.UsedQuota)
	var channel model.Channel
	require.NoError(t, model.DB.Select("used_quota").First(&channel, 91003).Error)
	require.Zero(t, channel.UsedQuota)
	var logs int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).
		Where("user_id = ? AND type = ?", 91003, model.LogTypeRefund).
		Count(&logs).Error)
	require.EqualValues(t, 1, logs)
	var reloaded model.Midjourney
	require.NoError(t, model.DB.First(&reloaded, task.Id).Error)
	require.Zero(t, reloaded.Quota)
	require.Empty(t, reloaded.SettlementStatus)
	var record model.MidjourneySettlementRecord
	require.NoError(t, model.DB.Where("midjourney_id = ?", task.Id).First(&record).Error)
	require.Equal(t, model.TaskSettlementRecordStatusApplied, record.Status)
	require.NotNil(t, record.PreConsumedQuota)
	require.Equal(t, 25, *record.PreConsumedQuota)
}

func TestRefundMidjourneyTaskQuotaSkipsFreshApplyingRecord(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	now := time.Now().Unix()

	task := &model.Midjourney{
		UserId:           91004,
		Action:           constant.MjActionImagine,
		MjId:             "mj-refund-applying-retry",
		Status:           "FAILURE",
		Progress:         "100%",
		ChannelId:        91004,
		Quota:            25,
		TokenId:          91004,
		Group:            "default",
		SubmitTime:       now * 1000,
		SettlementStatus: "",
	}
	require.NoError(t, model.DB.Create(task).Error)
	require.NoError(t, model.DB.Create(&model.MidjourneySettlementRecord{
		MidjourneyID: task.Id,
		PublicTaskID: task.MjId,
		Status:       model.TaskSettlementRecordStatusApplying,
		Operation:    "refund",
		CreatedAt:    now,
		UpdatedAt:    now,
	}).Error)

	require.NoError(t, RefundMidjourneyTaskQuota(ctx, task, "concurrent refund"))

	var reloaded model.Midjourney
	require.NoError(t, model.DB.First(&reloaded, task.Id).Error)
	require.Equal(t, 25, reloaded.Quota)
	require.Empty(t, reloaded.SettlementStatus)
	var record model.MidjourneySettlementRecord
	require.NoError(t, model.DB.Where("midjourney_id = ?", task.Id).First(&record).Error)
	require.Equal(t, model.TaskSettlementRecordStatusApplying, record.Status)
}

func TestRefundMidjourneyTaskQuotaRejectsAppliedNonRefundRecordWhenQuotaIsZero(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	now := time.Now().Unix()

	task := &model.Midjourney{
		UserId:           91005,
		Action:           constant.MjActionImagine,
		MjId:             "mj-refund-wrong-operation",
		Status:           "FAILURE",
		Progress:         "100%",
		ChannelId:        91005,
		Quota:            0,
		TokenId:          91005,
		Group:            "default",
		SubmitTime:       now * 1000,
		SettlementStatus: "",
	}
	require.NoError(t, model.DB.Create(task).Error)
	require.NoError(t, model.DB.Create(&model.MidjourneySettlementRecord{
		MidjourneyID: task.Id,
		PublicTaskID: task.MjId,
		Status:       model.TaskSettlementRecordStatusApplied,
		Operation:    "recalculation",
		CreatedAt:    now,
		UpdatedAt:    now,
		AppliedAt:    now,
	}).Error)

	err := RefundMidjourneyTaskQuota(ctx, task, "retry after applied record")
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot finalize refund")

	var reloaded model.Midjourney
	require.NoError(t, model.DB.First(&reloaded, task.Id).Error)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
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
			ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
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
			ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
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

func useBrokenLogDB(t *testing.T) {
	t.Helper()
	oldLogDB := model.LOG_DB
	brokenLogDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.LOG_DB = brokenLogDB
	t.Cleanup(func() {
		model.LOG_DB = oldLogDB
	})
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

func setUserUsageCounters(t *testing.T, id int, usedQuota int, requestCount int) {
	t.Helper()
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", id).Updates(map[string]interface{}{
		"used_quota":    usedQuota,
		"request_count": requestCount,
	}).Error)
}

func getUserUsageCounters(t *testing.T, id int) (int, int) {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("used_quota", "request_count").Where("id = ?", id).First(&user).Error)
	return user.UsedQuota, user.RequestCount
}

func setChannelUsedQuota(t *testing.T, id int, usedQuota int64) {
	t.Helper()
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", id).Update("used_quota", usedQuota).Error)
}

func getChannelUsedQuota(t *testing.T, id int) int64 {
	t.Helper()
	var channel model.Channel
	require.NoError(t, model.DB.Select("used_quota").Where("id = ?", id).First(&channel).Error)
	return channel.UsedQuota
}

func getTokenRemainQuota(t *testing.T, id int) int {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.Select("remain_quota").Where("id = ?", id).First(&token).Error)
	return token.RemainQuota
}

func getTokenUsageDaily(t *testing.T, tokenID int) model.TokenUsageDaily {
	t.Helper()
	var usage model.TokenUsageDaily
	require.NoError(t, model.DB.Where("token_id = ?", tokenID).First(&usage).Error)
	return usage
}

func tokenUsageDateForTest(t *testing.T) string {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	return time.Now().In(loc).Format("2006-01-02")
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

func TestLogTaskConsumptionReturnsErrorAndSkipsLogWhenUsageCounterUpdateFails(t *testing.T) {
	truncate(t)

	const userID = 9101
	const tokenID = 9102
	seedUser(t, userID, 10000)
	seedToken(t, tokenID, userID, "task-token-key", 1000)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/tasks/log", nil)
	ctx.Set("token_name", "task-token")

	info := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		OriginModelName: "gpt-4o",
		UsingGroup:      "default",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 9103},
		PriceData: types.PriceData{
			Quota:      10,
			ModelPrice: 0.1,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			Action:       constant.TaskActionImageGeneration,
			PublicTaskID: "task_9101",
		},
	}

	err := LogTaskConsumption(ctx, info)

	require.Error(t, err)
	require.Contains(t, err.Error(), "usage counter update failed")
	assert.Equal(t, int64(0), countLogs(t))
}

func TestLogTaskConsumptionCountsZeroQuotaRequests(t *testing.T) {
	truncate(t)

	const userID = 9107
	const tokenID = 9108
	const channelID = 9109
	seedUser(t, userID, 10000)
	seedToken(t, tokenID, userID, "task-zero-quota-token", 1000)
	seedChannel(t, channelID)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/tasks/log", nil)
	ctx.Set("token_name", "task-zero-quota")

	info := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		OriginModelName: "gpt-4o",
		UsingGroup:      "default",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
		PriceData: types.PriceData{
			Quota:      0,
			ModelPrice: 0.1,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			Action:       constant.TaskActionImageGeneration,
			PublicTaskID: "task_zero_quota",
		},
	}

	require.NoError(t, LogTaskConsumption(ctx, info))

	var user model.User
	require.NoError(t, model.DB.Select("used_quota", "request_count").First(&user, userID).Error)
	require.Equal(t, 0, user.UsedQuota)
	require.Equal(t, 1, user.RequestCount)
	var token model.Token
	require.NoError(t, model.DB.Select("used_quota").First(&token, tokenID).Error)
	require.Equal(t, 0, token.UsedQuota)
	var usage model.TokenUsageDaily
	require.NoError(t, model.DB.First(&usage, "token_id = ? AND date = ?", tokenID, tokenUsageDateForTest(t)).Error)
	require.Equal(t, 0, usage.Quota)
	require.Equal(t, 1, usage.RequestCount)
	assert.Equal(t, int64(1), countLogs(t))
}

func TestLogTaskConsumptionRollsBackZeroQuotaRequestCountWhenConsumeLogFails(t *testing.T) {
	truncate(t)
	useBrokenLogDB(t)

	const userID = 9110
	const tokenID = 9111
	const channelID = 9112
	seedUser(t, userID, 10000)
	seedToken(t, tokenID, userID, "task-zero-quota-rollback-token", 1000)
	seedChannel(t, channelID)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/tasks/log", nil)
	ctx.Set("token_name", "task-zero-quota-rollback")

	info := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		OriginModelName: "gpt-4o",
		UsingGroup:      "default",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
		PriceData: types.PriceData{
			Quota:      0,
			ModelPrice: 0.1,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			Action:       constant.TaskActionImageGeneration,
			PublicTaskID: "task_zero_quota_rollback",
		},
	}

	err := LogTaskConsumption(ctx, info)

	require.Error(t, err)
	require.Contains(t, err.Error(), "record consume log failed")
	var user model.User
	require.NoError(t, model.DB.Select("used_quota", "request_count").First(&user, userID).Error)
	require.Equal(t, 0, user.UsedQuota)
	require.Equal(t, 0, user.RequestCount)
	var token model.Token
	require.NoError(t, model.DB.Select("used_quota").First(&token, tokenID).Error)
	require.Equal(t, 0, token.UsedQuota)
	var usage model.TokenUsageDaily
	require.NoError(t, model.DB.First(&usage, "token_id = ? AND date = ?", tokenID, tokenUsageDateForTest(t)).Error)
	require.Equal(t, 0, usage.Quota)
	require.Equal(t, 0, usage.RequestCount)
}

func TestLogTaskConsumptionRollsBackUsageWhenConsumeLogFails(t *testing.T) {
	truncate(t)
	useBrokenLogDB(t)

	const userID = 9104
	const tokenID = 9105
	const channelID = 9106
	seedUser(t, userID, 10000)
	seedToken(t, tokenID, userID, "task-log-fail-token", 1000)
	seedChannel(t, channelID)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/tasks/log", nil)
	ctx.Set("token_name", "task-token")

	info := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		OriginModelName: "gpt-4o",
		UsingGroup:      "default",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
		PriceData: types.PriceData{
			Quota:      10,
			ModelPrice: 0.1,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			Action:       constant.TaskActionImageGeneration,
			PublicTaskID: "task_9104",
		},
	}

	err := LogTaskConsumption(ctx, info)

	require.Error(t, err)
	require.Contains(t, err.Error(), "record consume log failed")
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	assert.Equal(t, 0, usedQuota)
	assert.Equal(t, 0, requestCount)
	assert.Equal(t, int64(0), getChannelUsedQuota(t, channelID))
	var daily model.TokenUsageDaily
	err = model.DB.Where("token_id = ?", tokenID).First(&daily).Error
	if err == nil {
		assert.Equal(t, 0, daily.Quota)
		assert.Equal(t, 0, daily.RequestCount)
	}
}

func TestUpdateSunoTasksRefundsWhenChannelLookupFails(t *testing.T) {
	truncate(t)

	const userID = 9110
	const tokenID = 9111
	const missingChannelID = 9112
	const initQuota = 10000
	const preConsumed = 1200
	const tokenRemain = 5000
	const upstreamTaskID = "suno_missing_channel_upstream"

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "suno-missing-channel-token", tokenRemain)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).Update("used_quota", preConsumed).Error)
	setUserUsageCounters(t, userID, preConsumed, 1)
	model.RecordTokenUsage(tokenID, userID, preConsumed, common.GetTimestamp())

	task := makeTask(userID, missingChannelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.TaskID = "suno_missing_channel_public"
	task.Platform = constant.TaskPlatformSuno
	task.Status = model.TaskStatusSubmitted
	task.Progress = "0%"
	task.PrivateData.UpstreamTaskID = upstreamTaskID
	require.NoError(t, model.DB.Create(task).Error)

	err := updateSunoTasks(context.Background(), missingChannelID, []string{upstreamTaskID}, map[string]*model.Task{
		upstreamTaskID: task,
	})

	require.Error(t, err)
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, reloaded.Status)
	assert.Equal(t, "100%", reloaded.Progress)
	assert.Contains(t, reloaded.FailReason, fmt.Sprintf("渠道ID：%d", missingChannelID))
	assert.Equal(t, 0, reloaded.Quota)

	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, 0, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	assert.Equal(t, 0, usedQuota)
	assert.Equal(t, 1, requestCount)
	usage := getTokenUsageDaily(t, tokenID)
	assert.Equal(t, 0, usage.Quota)
	assert.Equal(t, 1, usage.RequestCount)

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed, log.Quota)
	assert.Equal(t, userID, log.UserId)
	assert.Equal(t, "test_user", log.Username)
	assert.Equal(t, tokenID, log.TokenId)
	assert.Equal(t, "test_token", log.TokenName)
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
			ImageTaskMode:   dto.ImageTaskModeAsyncTaskBridge,
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
			ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
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
				ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
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
				ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
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
					ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
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
			ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
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
			ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
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
				ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
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
			ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
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
			ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
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
			ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
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
			ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
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
			ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
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
			ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
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
			ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
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
			ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
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
			ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
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
			ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
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
			ImageTaskMode:   dto.ImageTaskModeAsyncTaskBridge,
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
				ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
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
			ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
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
			ImageTaskMode:   dto.ImageTaskModeAsyncTaskBridge,
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
			ImageTaskMode:   dto.ImageTaskModeAsyncTaskBridge,
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
			ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
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
			ImageTaskMode:  dto.ImageTaskModeAsyncTaskBridge,
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
			ImageTaskMode:  dto.ImageTaskModeAsyncTaskBridge,
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
	setUserUsageCounters(t, userID, preConsumed, 1)
	setChannelUsedQuota(t, channelID, int64(preConsumed))

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RefundTaskQuota(ctx, task, "task failed: upstream error"))

	// User quota should increase by preConsumed
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))

	// Token remain_quota should increase, used_quota should decrease
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, 0, getTokenUsedQuota(t, tokenID))

	require.Eventually(t, func() bool {
		usedQuota, requestCount := getUserUsageCounters(t, userID)
		return usedQuota == 0 && requestCount == 1
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, int64(0), getChannelUsedQuota(t, channelID))

	// A refund log should be created
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed, log.Quota)
	assert.Equal(t, "test-model", log.ModelName)

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, 0, reloaded.Quota)
}

func TestRefundTaskQuotaRollsBackAccountingWhenRefundLogFails(t *testing.T) {
	truncate(t)
	useBrokenLogDB(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1120, 1120, 1120
	const initQuota, preConsumed = 10000, 3000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-refund-log-fail", tokenRemain)
	seedChannel(t, channelID)
	setUserUsageCounters(t, userID, preConsumed, 1)
	setChannelUsedQuota(t, channelID, int64(preConsumed))

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	err := RefundTaskQuota(ctx, task, "task failed after submit")

	require.Error(t, err)
	require.Contains(t, err.Error(), "record task billing log failed")
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, 0, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	assert.Equal(t, preConsumed, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(preConsumed), getChannelUsedQuota(t, channelID))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, preConsumed, reloaded.Quota)
	assert.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	assert.Equal(t, preConsumed, reloaded.PrivateData.SettlementAttemptQuota)
	assert.Contains(t, reloaded.PrivateData.SettlementError, "record task billing log failed")
}

func TestRefundTaskQuota_DoesNotRefundWhenTaskQuotaPersistenceFails(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1019, 1019, 1019
	const initQuota, preConsumed = 10000, 3000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-refund-missing-task", tokenRemain)
	seedChannel(t, channelID)
	setUserUsageCounters(t, userID, preConsumed, 1)
	setChannelUsedQuota(t, channelID, int64(preConsumed))

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)
	require.NoError(t, model.DB.Delete(&model.Task{}, task.ID).Error)

	err := RefundTaskQuota(ctx, task, "missing task row")

	require.Error(t, err)
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, 0, getTokenUsedQuota(t, tokenID))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, model.TaskSettlementStatusReview, task.SettlementStatus)
	assert.Equal(t, preConsumed, task.PrivateData.SettlementAttemptQuota)
	assert.Contains(t, task.PrivateData.SettlementError, "task refund accounting target task is missing")
	require.Eventually(t, func() bool {
		usedQuota, requestCount := getUserUsageCounters(t, userID)
		return usedQuota == preConsumed && requestCount == 1
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, int64(preConsumed), getChannelUsedQuota(t, channelID))
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRefundTaskQuota_ExportsNetQuotaDataWithoutExtraCount(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 101, 101, 101
	const initQuota, preConsumed = 10000, 3000
	now := common.GetTimestamp()
	hour := now - now%3600

	oldDataExportEnabled := common.DataExportEnabled
	oldNodeName := common.NodeName
	common.DataExportEnabled = true
	common.NodeName = "refund-node"
	model.CacheQuotaDataLock.Lock()
	model.CacheQuotaData = make(map[string]*model.QuotaData)
	model.CacheQuotaDataLock.Unlock()
	t.Cleanup(func() {
		common.DataExportEnabled = oldDataExportEnabled
		common.NodeName = oldNodeName
		model.CacheQuotaDataLock.Lock()
		model.CacheQuotaData = make(map[string]*model.QuotaData)
		model.CacheQuotaDataLock.Unlock()
	})

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-refund-export", 5000)
	seedChannel(t, channelID)
	require.NoError(t, model.DB.Create(&model.QuotaData{
		UserID:    userID,
		Username:  "test_user",
		ModelName: "test-model",
		CreatedAt: hour,
		UseGroup:  "default",
		TokenID:   tokenID,
		ChannelID: channelID,
		NodeName:  "refund-node",
		Count:     1,
		Quota:     preConsumed,
	}).Error)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.PrivateData.NodeName = "refund-node"
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RefundTaskQuota(ctx, task, "task failed after submit"))
	model.SaveQuotaDataCache()

	rows, err := model.GetQuotaDataByUserId(userID, hour-1, hour+1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 1, rows[0].Count)
	assert.Equal(t, 0, rows[0].Quota)
}

func TestRefundTaskQuota_ImageTaskExportsNetQuotaDataWithoutExtraCount(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1020, 1020, 1020
	const initQuota, preConsumed = 10000, 3000
	now := common.GetTimestamp()
	hour := now - now%3600

	oldDataExportEnabled := common.DataExportEnabled
	oldNodeName := common.NodeName
	common.DataExportEnabled = true
	common.NodeName = "image-refund-node"
	model.CacheQuotaDataLock.Lock()
	model.CacheQuotaData = make(map[string]*model.QuotaData)
	model.CacheQuotaDataLock.Unlock()
	t.Cleanup(func() {
		common.DataExportEnabled = oldDataExportEnabled
		common.NodeName = oldNodeName
		model.CacheQuotaDataLock.Lock()
		model.CacheQuotaData = make(map[string]*model.QuotaData)
		model.CacheQuotaDataLock.Unlock()
	})

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-image-refund-export", 5000)
	seedChannel(t, channelID)
	require.NoError(t, model.DB.Create(&model.QuotaData{
		UserID:    userID,
		Username:  "test_user",
		ModelName: "test-model",
		CreatedAt: hour,
		UseGroup:  "default",
		TokenID:   tokenID,
		ChannelID: channelID,
		NodeName:  "image-refund-node",
		Count:     1,
		Quota:     preConsumed,
	}).Error)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Platform = constant.TaskPlatformImage
	task.PrivateData.UpstreamTaskID = "upstream-image-refund"
	task.PrivateData.NodeName = "image-refund-node"
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RefundTaskQuota(ctx, task, "image task failed after submit"))
	model.SaveQuotaDataCache()

	rows, err := model.GetQuotaDataByUserId(userID, hour-1, hour+1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 1, rows[0].Count)
	assert.Equal(t, 0, rows[0].Quota)
}

func TestRefundTaskQuota_ClearsSettlementReviewFields(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1015, 1015, 1015
	const initQuota, preConsumed = 10000, 3000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-refund-review-clear", 5000)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatusFailure
	task.FailReason = "upstream failed; " + TaskSettlementReviewFailReason
	task.SettlementStatus = model.TaskSettlementStatusReview
	task.PrivateData.SettlementAttemptQuota = 4500
	task.PrivateData.SettlementError = "settlement update failed"
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RefundTaskQuota(ctx, task, "upstream failed"))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Empty(t, reloaded.SettlementStatus)
	assert.Equal(t, "upstream failed", reloaded.FailReason)
	assert.Zero(t, reloaded.PrivateData.SettlementAttemptQuota)
	assert.Empty(t, reloaded.PrivateData.SettlementError)
}

func TestRefundTaskQuotaFinalizesAppliedSettlementRecordWithoutDoubleRefund(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1022, 1022, 1022
	const initQuota, preConsumed = 10000, 3000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota+preConsumed)
	seedToken(t, tokenID, userID, "sk-refund-applied-retry", tokenRemain+preConsumed)
	seedChannel(t, channelID)
	setUserUsageCounters(t, userID, 0, 1)
	setChannelUsedQuota(t, channelID, 0)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)
	require.NoError(t, model.DB.Create(&model.Log{
		UserId:    userID,
		Username:  "test_user",
		CreatedAt: common.GetTimestamp(),
		Type:      model.LogTypeRefund,
		ModelName: "test-model",
		Quota:     preConsumed,
		ChannelId: channelID,
		TokenId:   tokenID,
		Group:     "default",
	}).Error)
	require.NoError(t, model.DB.Create(&model.TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        model.TaskSettlementRecordStatusApplied,
		AppliedAt:     common.GetTimestamp(),
	}).Error)

	require.NoError(t, RefundTaskQuota(ctx, task, "task failed after applied record"))

	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, 0, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	assert.Equal(t, 0, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(0), getChannelUsedQuota(t, channelID))
	assert.Equal(t, int64(1), countLogs(t))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, 0, reloaded.Quota)
	assert.Empty(t, reloaded.SettlementStatus)
}

func TestRefundTaskQuotaZeroQuotaFinalizesAppliedRefundRecord(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1023, 1023, 1023

	seedUser(t, userID, 10000)
	seedToken(t, tokenID, userID, "sk-refund-zero-applied-retry", 5000)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, 0, tokenID, BillingSourceWallet, 0)
	task.SettlementStatus = model.TaskSettlementStatusReview
	task.FailReason = TaskSettlementReviewFailReason
	task.PrivateData.SettlementAttemptQuota = 3000
	task.PrivateData.SettlementError = "finalize applied task refund failed"
	require.NoError(t, model.DB.Create(task).Error)
	require.NoError(t, model.DB.Create(&model.TaskSettlementRecord{
		TaskPrimaryID:    task.ID,
		PublicTaskID:     task.TaskID,
		Status:           model.TaskSettlementRecordStatusApplied,
		Operation:        taskSettlementOperationRefund,
		AppliedQuota:     common.GetPointer(0),
		PreConsumedQuota: common.GetPointer(3000),
		QuotaDelta:       common.GetPointer(-3000),
		LogType:          common.GetPointer(model.LogTypeRefund),
		AppliedAt:        common.GetTimestamp(),
	}).Error)

	require.NoError(t, RefundTaskQuota(ctx, task, "retry after applied refund record"))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, 0, reloaded.Quota)
	assert.Empty(t, reloaded.SettlementStatus)
	assert.Empty(t, reloaded.FailReason)
	assert.Zero(t, reloaded.PrivateData.SettlementAttemptQuota)
	assert.Empty(t, reloaded.PrivateData.SettlementError)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRefundTaskQuotaRejectsAppliedRecalculationRecord(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1026, 1026, 1026
	const initQuota, preConsumed = 10000, 3000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-refund-applied-recalc", tokenRemain)
	seedChannel(t, channelID)
	setUserUsageCounters(t, userID, preConsumed, 1)
	setChannelUsedQuota(t, channelID, int64(preConsumed))

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatusFailure
	task.SettlementStatus = model.TaskSettlementStatusPending
	require.NoError(t, model.DB.Create(task).Error)
	require.NoError(t, model.DB.Create(&model.TaskSettlementRecord{
		TaskPrimaryID:    task.ID,
		PublicTaskID:     task.TaskID,
		Status:           model.TaskSettlementRecordStatusApplied,
		Operation:        taskSettlementOperationRecalculation,
		AppliedQuota:     common.GetPointer(4500),
		PreConsumedQuota: common.GetPointer(preConsumed),
		QuotaDelta:       common.GetPointer(1500),
		LogType:          common.GetPointer(model.LogTypeConsume),
		AppliedAt:        common.GetTimestamp(),
	}).Error)

	err := RefundTaskQuota(ctx, task, "task failed after recalculation applied")

	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot finalize refund")
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, preConsumed, reloaded.Quota)
	assert.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	assert.Equal(t, TaskSettlementReviewFailReason, reloaded.FailReason)
	assert.Equal(t, preConsumed, reloaded.PrivateData.SettlementAttemptQuota)
	assert.Contains(t, reloaded.PrivateData.SettlementError, "cannot finalize refund")
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	assert.Equal(t, preConsumed, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(preConsumed), getChannelUsedQuota(t, channelID))
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRefundTaskQuotaSkipsFreshApplyingSettlementRecord(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1024, 1024, 1024
	const initQuota, preConsumed = 10000, 3000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-refund-applying", tokenRemain)
	seedChannel(t, channelID)
	setUserUsageCounters(t, userID, preConsumed, 1)
	setChannelUsedQuota(t, channelID, int64(preConsumed))

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatusFailure
	task.SettlementStatus = model.TaskSettlementStatusPending
	require.NoError(t, model.DB.Create(task).Error)
	require.NoError(t, model.DB.Create(&model.TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        model.TaskSettlementRecordStatusApplying,
		Operation:     taskSettlementOperationRefund,
		UpdatedAt:     common.GetTimestamp(),
	}).Error)

	require.NoError(t, RefundTaskQuota(ctx, task, "concurrent refund already applying"))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, preConsumed, reloaded.Quota)
	assert.Equal(t, model.TaskSettlementStatusPending, reloaded.SettlementStatus)
	assert.Empty(t, reloaded.PrivateData.SettlementError)
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	assert.Equal(t, preConsumed, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(preConsumed), getChannelUsedQuota(t, channelID))
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRefundTaskQuota_MarksReviewWhenUsageCounterUpdateFails(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1021, 1021, 1021
	const initQuota, preConsumed = 10000, 3000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-refund-counter-fail", tokenRemain)
	seedChannel(t, channelID)
	setUserUsageCounters(t, userID, preConsumed, 1)
	setChannelUsedQuota(t, channelID, int64(preConsumed))

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)
	require.NoError(t, model.DB.Delete(&model.Channel{}, channelID).Error)

	err := RefundTaskQuota(ctx, task, "task failed after submit")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "update task usage counters failed")
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, model.TaskSettlementStatusReview, task.SettlementStatus)
	assert.Equal(t, preConsumed, task.PrivateData.SettlementAttemptQuota)
	assert.Contains(t, task.PrivateData.SettlementError, "update task usage counters failed")
	assert.Contains(t, task.FailReason, TaskSettlementReviewFailReason)
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	assert.Equal(t, preConsumed, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(0), countLogs(t))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, preConsumed, reloaded.Quota)
	assert.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	assert.Equal(t, preConsumed, reloaded.PrivateData.SettlementAttemptQuota)
	assert.Contains(t, reloaded.PrivateData.SettlementError, "update task usage counters failed")
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
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RefundTaskQuota(ctx, task, "subscription task failed"))

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

	require.NoError(t, RefundTaskQuota(ctx, task, "zero quota task"))

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
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RefundTaskQuota(ctx, task, "no token task failed"))

	// User quota refunded
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))

	// Log created
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

func TestRefundTaskQuota_RefundsFundingWhenTokenDeleted(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, channelID = 404, 404
	const deletedTokenID = 404
	const initQuota, preConsumed = 10000, 1500

	seedUser(t, userID, initQuota)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, deletedTokenID, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RefundTaskQuota(ctx, task, "token deleted task failed"))

	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed, log.Quota)
}

func TestRefundTaskQuota_RollsBackTokenWhenFundingRefundFails(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 19, 19, 19
	const quota = 10

	seedUser(t, userID, 10000)
	seedToken(t, tokenID, userID, "sk-refund-rollback", 0)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).Update("used_quota", quota).Error)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, quota, tokenID, BillingSourceSubscription, 9999)
	require.NoError(t, model.DB.Create(task).Error)

	err := RefundTaskQuota(ctx, task, "subscription refund failed")

	require.Error(t, err)
	assert.Equal(t, 0, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, quota, getTokenUsedQuota(t, tokenID))
	assert.Equal(t, 10000, getUserQuota(t, userID))
	assert.Equal(t, int64(0), countLogs(t))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, quota, reloaded.Quota)
	assert.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	assert.Equal(t, quota, reloaded.PrivateData.SettlementAttemptQuota)
	assert.Contains(t, reloaded.PrivateData.SettlementError, "refund funding failed")
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
	setUserUsageCounters(t, userID, preConsumed, 1)
	setChannelUsedQuota(t, channelID, int64(preConsumed))

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment"))

	// User quota should decrease by the delta (1000 additional charge)
	assert.Equal(t, initQuota-(actualQuota-preConsumed), getUserQuota(t, userID))

	// Token should also be charged the delta
	assert.Equal(t, tokenRemain-(actualQuota-preConsumed), getTokenRemainQuota(t, tokenID))

	// task.Quota should be updated to actualQuota
	assert.Equal(t, actualQuota, task.Quota)

	require.Eventually(t, func() bool {
		usedQuota, requestCount := getUserUsageCounters(t, userID)
		return usedQuota == actualQuota && requestCount == 1
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, int64(actualQuota), getChannelUsedQuota(t, channelID))

	// Log type should be Consume (additional charge)
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeConsume, log.Type)
	assert.Equal(t, actualQuota-preConsumed, log.Quota)
}

func TestTaskBillingGroupRatioUsesZeroSpecialRatioSnapshot(t *testing.T) {
	task := &model.Task{}
	task.PrivateData.BillingContext = &model.TaskBillingContext{
		GroupRatio:           2,
		GroupSpecialRatio:    0,
		GroupHasSpecialRatio: true,
	}

	ratio, ok := TaskBillingGroupRatio(task)

	require.True(t, ok)
	assert.EqualValues(t, 0, ratio)
}

func TestTaskBillingGroupRatioUsesCapturedZeroGroupRatioSnapshot(t *testing.T) {
	task := &model.Task{}
	task.PrivateData.BillingContext = &model.TaskBillingContext{
		GroupRatio:         0,
		GroupRatioCaptured: true,
	}

	ratio, ok := TaskBillingGroupRatio(task)

	require.True(t, ok)
	assert.EqualValues(t, 0, ratio)
}

func TestRecalculateTaskQuotaByTokensUsesBillingContextSpecialGroupRatio(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	oldModelRatios := ratio_setting.ModelRatio2JSONString()
	oldGroupRatios := ratio_setting.GroupRatio2JSONString()
	oldGroupGroupRatios := ratio_setting.GroupGroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"special-token-model":10}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"priority":2}`))
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{"vip":{"priority":0.5}}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldModelRatios))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroupRatios))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(oldGroupGroupRatios))
	})

	const userID, tokenID, channelID = 1133, 1133, 1133
	const initQuota, preConsumed = 10000, 1000
	const tokenRemain = 5000
	const actualQuota = 500

	seedUser(t, userID, initQuota)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Update("group", "vip").Error)
	seedToken(t, tokenID, userID, "sk-recalc-special-ratio", tokenRemain)
	seedChannel(t, channelID)
	setUserUsageCounters(t, userID, preConsumed, 1)
	setChannelUsedQuota(t, channelID, int64(preConsumed))

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Group = "priority"
	task.Properties.OriginModelName = "special-token-model"
	task.PrivateData.BillingContext = &model.TaskBillingContext{
		OriginModelName:      "special-token-model",
		GroupRatio:           0.5,
		GroupSpecialRatio:    0.5,
		GroupHasSpecialRatio: true,
	}
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RecalculateTaskQuotaByTokens(ctx, task, 100))

	assert.Equal(t, initQuota+(preConsumed-actualQuota), getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	assert.Equal(t, actualQuota, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(actualQuota), getChannelUsedQuota(t, channelID))

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed-actualQuota, log.Quota)
	other, err := common.StrToMap(log.Other)
	require.NoError(t, err)
	assert.EqualValues(t, 0.5, other["group_ratio"])
	assert.EqualValues(t, 0.5, other["user_group_ratio"])
}

func TestRecalculateTaskQuotaByTokensUsesBillingContextModelRatioSnapshot(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	oldModelRatios := ratio_setting.ModelRatio2JSONString()
	oldGroupRatios := ratio_setting.GroupRatio2JSONString()
	oldGroupGroupRatios := ratio_setting.GroupGroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"snapshot-token-model":20}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldModelRatios))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroupRatios))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(oldGroupGroupRatios))
	})

	const userID, tokenID, channelID = 1134, 1134, 1134
	const initQuota, preConsumed = 10000, 1000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-model-ratio-snapshot", tokenRemain)
	seedChannel(t, channelID)
	setUserUsageCounters(t, userID, preConsumed, 1)
	setChannelUsedQuota(t, channelID, int64(preConsumed))

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Properties.OriginModelName = "snapshot-token-model"
	task.PrivateData.BillingContext = &model.TaskBillingContext{
		OriginModelName:    "snapshot-token-model",
		ModelRatio:         10,
		GroupRatio:         1,
		GroupRatioCaptured: true,
	}
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RecalculateTaskQuotaByTokens(ctx, task, 100))

	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, 0, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	assert.Equal(t, preConsumed, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(preConsumed), getChannelUsedQuota(t, channelID))
	assert.Equal(t, int64(0), countLogs(t))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, preConsumed, reloaded.Quota)
	assert.Empty(t, reloaded.SettlementStatus)
}

func TestRecalculateTaskQuotaMarksReviewWhenAppliedSettlementRecordHasNoEvidence(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1130, 1130, 1130
	const initQuota, preConsumed = 10000, 2000
	const actualQuota = 3000
	const tokenRemain = 5000
	const delta = actualQuota - preConsumed

	seedUser(t, userID, initQuota-delta)
	seedToken(t, tokenID, userID, "sk-recalc-applied-retry", tokenRemain-delta)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).Update("used_quota", delta).Error)
	seedChannel(t, channelID)
	setUserUsageCounters(t, userID, actualQuota, 1)
	setChannelUsedQuota(t, channelID, int64(actualQuota))

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)
	require.NoError(t, model.DB.Create(&model.Log{
		UserId:    userID,
		Username:  "test_user",
		CreatedAt: common.GetTimestamp(),
		Type:      model.LogTypeConsume,
		ModelName: "test-model",
		Quota:     delta,
		ChannelId: channelID,
		TokenId:   tokenID,
		Group:     "default",
	}).Error)
	require.NoError(t, model.DB.Create(&model.TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        model.TaskSettlementRecordStatusApplied,
		AppliedAt:     common.GetTimestamp(),
	}).Error)

	err := RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment after applied record")

	require.Error(t, err)
	require.Contains(t, err.Error(), "has no applied quota evidence")
	assert.Equal(t, initQuota-delta, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain-delta, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, delta, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	assert.Equal(t, actualQuota, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(actualQuota), getChannelUsedQuota(t, channelID))
	assert.Equal(t, int64(1), countLogs(t))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, preConsumed, reloaded.Quota)
	assert.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	assert.Equal(t, actualQuota, reloaded.PrivateData.SettlementAttemptQuota)
	assert.Contains(t, reloaded.PrivateData.SettlementError, "has no applied quota evidence")
	assert.Contains(t, reloaded.FailReason, TaskSettlementReviewFailReason)
}

func TestRecalculateTaskQuotaFinalizesAppliedSettlementRecordFromStoredQuotaAfterLogCleanup(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1131, 1131, 1131
	const initQuota, preConsumed = 10000, 2000
	const appliedQuota = 3000
	const driftedQuota = 4500
	const tokenRemain = 5000
	const delta = appliedQuota - preConsumed

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-applied-stored-quota", tokenRemain)
	seedChannel(t, channelID)
	setUserUsageCounters(t, userID, preConsumed, 1)
	setChannelUsedQuota(t, channelID, int64(preConsumed))

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.TaskID = "task_recalc_applied_stored_quota"
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RecalculateTaskQuota(ctx, task, appliedQuota, "initial adaptor adjustment"))
	require.Equal(t, int64(1), countLogs(t))
	require.NoError(t, model.LOG_DB.Exec("DELETE FROM logs").Error)
	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Update("quota", preConsumed).Error)
	task.Quota = preConsumed

	require.NoError(t, RecalculateTaskQuota(ctx, task, driftedQuota, "retry with drifted adaptor adjustment"))

	assert.Equal(t, initQuota-delta, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain-delta, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, delta, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	assert.Equal(t, appliedQuota, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(appliedQuota), getChannelUsedQuota(t, channelID))
	assert.Equal(t, int64(0), countLogs(t))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, appliedQuota, reloaded.Quota)
	assert.Empty(t, reloaded.SettlementStatus)
}

func TestRecalculateTaskQuotaFinalizesLegacyAppliedSettlementRecordFromBillingLogWhenActualQuotaDrifts(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1132, 1132, 1132
	const initQuota, preConsumed = 10000, 2000
	const appliedQuota = 3000
	const driftedQuota = 4500
	const tokenRemain = 5000
	const delta = appliedQuota - preConsumed

	seedUser(t, userID, initQuota-delta)
	seedToken(t, tokenID, userID, "sk-recalc-applied-legacy-log", tokenRemain-delta)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).Update("used_quota", delta).Error)
	seedChannel(t, channelID)
	setUserUsageCounters(t, userID, appliedQuota, 1)
	setChannelUsedQuota(t, channelID, int64(appliedQuota))

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.TaskID = "task_recalc_applied_legacy_log"
	require.NoError(t, model.DB.Create(task).Error)
	require.NoError(t, model.LOG_DB.Create(&model.Log{
		UserId:    userID,
		Username:  "test_user",
		CreatedAt: common.GetTimestamp(),
		Type:      model.LogTypeConsume,
		ModelName: "test-model",
		Quota:     delta,
		ChannelId: channelID,
		TokenId:   tokenID,
		Group:     "default",
		Other: common.MapToJsonStr(map[string]interface{}{
			"task_id":            task.TaskID,
			"pre_consumed_quota": preConsumed,
			"actual_quota":       appliedQuota,
		}),
	}).Error)
	require.NoError(t, model.DB.Create(&model.TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        model.TaskSettlementRecordStatusApplied,
		AppliedAt:     common.GetTimestamp(),
	}).Error)

	require.NoError(t, RecalculateTaskQuota(ctx, task, driftedQuota, "retry with drifted legacy actual quota"))

	assert.Equal(t, initQuota-delta, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain-delta, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, delta, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	assert.Equal(t, appliedQuota, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(appliedQuota), getChannelUsedQuota(t, channelID))
	assert.Equal(t, int64(1), countLogs(t))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, appliedQuota, reloaded.Quota)
	assert.Empty(t, reloaded.SettlementStatus)
}

func TestRecalculatePositiveDeltaRollsBackAccountingWhenBillingLogFails(t *testing.T) {
	truncate(t)
	useBrokenLogDB(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1121, 1121, 1121
	const initQuota, preConsumed = 10000, 2000
	const actualQuota = 3000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-log-fail", tokenRemain)
	seedChannel(t, channelID)
	setUserUsageCounters(t, userID, preConsumed, 1)
	setChannelUsedQuota(t, channelID, int64(preConsumed))

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	err := RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment")

	require.Error(t, err)
	require.Contains(t, err.Error(), "record task billing log failed")
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, 0, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	assert.Equal(t, preConsumed, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(preConsumed), getChannelUsedQuota(t, channelID))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, preConsumed, reloaded.Quota)
	assert.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	assert.Equal(t, actualQuota, reloaded.PrivateData.SettlementAttemptQuota)
	assert.Contains(t, reloaded.PrivateData.SettlementError, "record task billing log failed")
}

func TestRecalculateNegativeDeltaLogFailureRollsBackTrackedTokenDelta(t *testing.T) {
	truncate(t)
	useBrokenLogDB(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1129, 1129, 1129
	const initQuota, preConsumed = 10000, 3000
	const actualQuota = 1000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-refund-log-fail", tokenRemain)
	seedChannel(t, channelID)
	setUserUsageCounters(t, userID, preConsumed, 1)
	setChannelUsedQuota(t, channelID, int64(preConsumed))
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).Update("used_quota", 300).Error)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	err := RecalculateTaskQuota(ctx, task, actualQuota, "adaptor refund adjustment")

	require.Error(t, err)
	require.Contains(t, err.Error(), "record task billing log failed")
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, 300, getTokenUsedQuota(t, tokenID))
}

func TestRecalculate_PositiveDeltaDoesNotIncrementTokenRequestCount(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1016, 1016, 1016
	const initQuota, preConsumed = 10000, 2000
	const actualQuota = 3000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-request-count", 5000)
	seedChannel(t, channelID)
	model.RecordTokenUsage(tokenID, userID, preConsumed, common.GetTimestamp())

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment"))

	usage := getTokenUsageDaily(t, tokenID)
	assert.Equal(t, actualQuota, usage.Quota)
	assert.Equal(t, 1, usage.RequestCount)
}

func TestRecalculate_PositiveDeltaDoesNotIncrementLogRPM(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1017, 1017, 1017
	const initQuota, preConsumed = 10000, 2000
	const actualQuota = 3000
	now := common.GetTimestamp()

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-rpm", 5000)
	seedChannel(t, channelID)
	require.NoError(t, model.LOG_DB.Create(&model.Log{
		UserId:           userID,
		Username:         "test_user",
		CreatedAt:        now,
		Type:             model.LogTypeConsume,
		ModelName:        "test-model",
		TokenName:        "sk-recalc-rpm",
		Quota:            preConsumed,
		PromptTokens:     10,
		CompletionTokens: 5,
		Group:            "default",
	}).Error)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment"))

	stat, err := model.SumUsedQuotaByUserId(model.LogTypeConsume, 0, 0, "test-model", userID, "", 0, "")
	require.NoError(t, err)
	assert.Equal(t, actualQuota, stat.Quota)
	assert.Equal(t, 1, stat.Rpm)
	assert.Equal(t, 15, stat.Tpm)
}

func TestRecalculate_PositiveDeltaExportsNetQuotaDataWithoutExtraCount(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1012, 1012, 1012
	const initQuota, preConsumed = 10000, 2000
	const actualQuota = 3000
	now := common.GetTimestamp()
	hour := now - now%3600

	oldDataExportEnabled := common.DataExportEnabled
	oldNodeName := common.NodeName
	common.DataExportEnabled = true
	common.NodeName = "recalc-node"
	model.CacheQuotaDataLock.Lock()
	model.CacheQuotaData = make(map[string]*model.QuotaData)
	model.CacheQuotaDataLock.Unlock()
	t.Cleanup(func() {
		common.DataExportEnabled = oldDataExportEnabled
		common.NodeName = oldNodeName
		model.CacheQuotaDataLock.Lock()
		model.CacheQuotaData = make(map[string]*model.QuotaData)
		model.CacheQuotaDataLock.Unlock()
	})

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-export-pos", 5000)
	seedChannel(t, channelID)
	require.NoError(t, model.DB.Create(&model.QuotaData{
		UserID:    userID,
		Username:  "test_user",
		ModelName: "test-model",
		CreatedAt: hour,
		UseGroup:  "default",
		TokenID:   tokenID,
		ChannelID: channelID,
		NodeName:  "recalc-node",
		Count:     1,
		Quota:     preConsumed,
	}).Error)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.PrivateData.NodeName = "recalc-node"
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment"))
	model.SaveQuotaDataCache()

	rows, err := model.GetQuotaDataByUserId(userID, hour-1, hour+1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 1, rows[0].Count)
	assert.Equal(t, actualQuota, rows[0].Quota)
}

func TestRecalculate_PositiveDeltaRollsBackWhenTaskQuotaUpdateFails(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1011, 1011, 1011
	const initQuota, preConsumed = 10000, 2000
	const actualQuota = 3000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-update-fail", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)

	err := RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment")

	require.Error(t, err)
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, 0, getTokenUsedQuota(t, tokenID))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRecalculate_PositiveDeltaRollsBackFundingWhenTokenAdjustmentFails(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1010, 1010, 1010
	const initQuota, preConsumed = 10000, 2000
	const actualQuota = 3000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-token-fail", 0)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	require.Error(t, RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment"))

	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, 0, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, int64(0), countLogs(t))
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
	setUserUsageCounters(t, userID, preConsumed, 1)
	setChannelUsedQuota(t, channelID, int64(preConsumed))

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment"))

	// User quota should increase by abs(delta) = 2000 (refund overpayment)
	assert.Equal(t, initQuota+(preConsumed-actualQuota), getUserQuota(t, userID))

	// Token should be refunded the difference
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))

	// task.Quota updated
	assert.Equal(t, actualQuota, task.Quota)

	require.Eventually(t, func() bool {
		usedQuota, requestCount := getUserUsageCounters(t, userID)
		return usedQuota == actualQuota && requestCount == 1
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, int64(actualQuota), getChannelUsedQuota(t, channelID))

	// Log type should be Refund
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed-actualQuota, log.Quota)
}

func TestRecalculate_NegativeDeltaExportsNetQuotaDataWithoutExtraCount(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1013, 1013, 1013
	const initQuota, preConsumed = 10000, 5000
	const actualQuota = 3000
	now := common.GetTimestamp()
	hour := now - now%3600

	oldDataExportEnabled := common.DataExportEnabled
	oldNodeName := common.NodeName
	common.DataExportEnabled = true
	common.NodeName = "recalc-node"
	model.CacheQuotaDataLock.Lock()
	model.CacheQuotaData = make(map[string]*model.QuotaData)
	model.CacheQuotaDataLock.Unlock()
	t.Cleanup(func() {
		common.DataExportEnabled = oldDataExportEnabled
		common.NodeName = oldNodeName
		model.CacheQuotaDataLock.Lock()
		model.CacheQuotaData = make(map[string]*model.QuotaData)
		model.CacheQuotaDataLock.Unlock()
	})

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-export-neg", 5000)
	seedChannel(t, channelID)
	require.NoError(t, model.DB.Create(&model.QuotaData{
		UserID:    userID,
		Username:  "test_user",
		ModelName: "test-model",
		CreatedAt: hour,
		UseGroup:  "default",
		TokenID:   tokenID,
		ChannelID: channelID,
		NodeName:  "recalc-node",
		Count:     1,
		Quota:     preConsumed,
	}).Error)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.PrivateData.NodeName = "recalc-node"
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment"))
	model.SaveQuotaDataCache()

	rows, err := model.GetQuotaDataByUserId(userID, hour-1, hour+1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 1, rows[0].Count)
	assert.Equal(t, actualQuota, rows[0].Quota)
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

	const userID, tokenID, channelID = 13, 13, 13
	const initQuota, preConsumed = 10000, 5000
	const tokenRemain = 3000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-zero-actual", tokenRemain)
	seedChannel(t, channelID)
	setUserUsageCounters(t, userID, preConsumed, 1)
	setChannelUsedQuota(t, channelID, int64(preConsumed))

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RecalculateTaskQuota(ctx, task, 0, "zero actual"))

	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, int64(0), getChannelUsedQuota(t, channelID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	assert.Equal(t, 0, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, 0, task.Quota)

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed, log.Quota)
	assert.Equal(t, userID, log.UserId)
	assert.Equal(t, "test_user", log.Username)
	assert.Equal(t, tokenID, log.TokenId)
	assert.Equal(t, "test_token", log.TokenName)
}

func TestRecalculate_ActualQuotaZeroClearsSettlementReviewFields(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1018, 1018, 1018
	const initQuota, preConsumed = 10000, 5000
	const tokenRemain = 3000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-zero-review", tokenRemain)
	seedChannel(t, channelID)
	setUserUsageCounters(t, userID, preConsumed, 1)
	setChannelUsedQuota(t, channelID, int64(preConsumed))

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.SettlementStatus = model.TaskSettlementStatusReview
	task.FailReason = TaskSettlementReviewFailReason
	task.PrivateData.SettlementAttemptQuota = 0
	task.PrivateData.SettlementError = "previous zero settlement failed"
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RecalculateTaskQuota(ctx, task, 0, "zero actual retry"))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, 0, reloaded.Quota)
	assert.Empty(t, reloaded.SettlementStatus)
	assert.Empty(t, reloaded.FailReason)
	assert.Zero(t, reloaded.PrivateData.SettlementAttemptQuota)
	assert.Empty(t, reloaded.PrivateData.SettlementError)
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, int64(0), getChannelUsedQuota(t, channelID))

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed, log.Quota)
	assert.Equal(t, "test_user", log.Username)
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
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RecalculateTaskQuota(ctx, task, actualQuota, "subscription over-charge"))

	// Subscription used should decrease by delta (refund 3000)
	assert.Equal(t, subUsed-int64(preConsumed-actualQuota), getSubscriptionUsed(t, subID))

	// Token refunded
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))

	assert.Equal(t, actualQuota, task.Quota)

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

func TestRecalculate_ClearsSubmitSettlementReviewOnSuccess(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1014, 1014, 1014
	const initQuota, preConsumed = 10000, 2000
	const actualQuota = 3000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-review-clear", 5000)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.SettlementStatus = model.TaskSettlementStatusReview
	task.FailReason = "billing settlement requires manual review"
	task.PrivateData.SettlementAttemptQuota = actualQuota
	task.PrivateData.SettlementError = "previous settlement failed"
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, RecalculateTaskQuota(ctx, task, actualQuota, "manual review retry"))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Empty(t, reloaded.SettlementStatus)
	assert.Empty(t, reloaded.FailReason)
	assert.Zero(t, reloaded.PrivateData.SettlementAttemptQuota)
	assert.Empty(t, reloaded.PrivateData.SettlementError)
}

func TestRecalculateTaskQuotaSkipsFreshApplyingSettlementRecord(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1016, 1016, 1016
	const initQuota, preConsumed = 10000, 2000
	const actualQuota = 3000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-applying", tokenRemain)
	seedChannel(t, channelID)
	setUserUsageCounters(t, userID, preConsumed, 1)
	setChannelUsedQuota(t, channelID, int64(preConsumed))

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatusSuccess
	task.SettlementStatus = model.TaskSettlementStatusPending
	require.NoError(t, model.DB.Create(task).Error)
	require.NoError(t, model.DB.Create(&model.TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        model.TaskSettlementRecordStatusApplying,
		Operation:     taskSettlementOperationRecalculation,
		UpdatedAt:     common.GetTimestamp(),
	}).Error)

	require.NoError(t, RecalculateTaskQuota(ctx, task, actualQuota, "concurrent settlement already applying"))

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, preConsumed, reloaded.Quota)
	assert.Equal(t, model.TaskSettlementStatusPending, reloaded.SettlementStatus)
	assert.Empty(t, reloaded.PrivateData.SettlementError)
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageCounters(t, userID)
	assert.Equal(t, preConsumed, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(preConsumed), getChannelUsedQuota(t, channelID))
	assert.Equal(t, int64(0), countLogs(t))
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
	require.NoError(t, model.DB.Create(task).Error)
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

func TestSettle_NonPerCallBilling_MarksReviewWhenAdaptorSettlementFails(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 33, 33, 33
	const initQuota, preConsumed = 0, 1000
	const adaptorQuota = 4000
	const tokenRemain = 10000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-nonpercall-adj-fail", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatusSuccess
	require.NoError(t, model.DB.Create(task).Error)

	adaptor := &mockAdaptor{adjustReturn: adaptorQuota}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	require.Equal(t, preConsumed, reloaded.Quota)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.Equal(t, adaptorQuota, reloaded.PrivateData.SettlementAttemptQuota)
	require.NotEmpty(t, reloaded.PrivateData.SettlementError)
	require.NotEmpty(t, reloaded.FailReason)
	require.Contains(t, reloaded.FailReason, "billing settlement requires manual review")
	record, exists, err := model.GetTaskSettlementRecord(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, model.TaskSettlementRecordStatusReview, record.Status)
	require.Contains(t, record.Error, "task quota settlement funding adjustment failed")
	require.Equal(t, initQuota, getUserQuota(t, userID))
	require.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
}

func TestSettle_NonPerCallBilling_MarksReviewWhenTokenSettlementFails(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	ratio_setting.InitRatioSettings()

	const userID, tokenID, channelID = 34, 34, 34
	const initQuota, preConsumed = 10000, 1000
	const totalTokens = 100
	const tokenRemain = 0
	const expectedActualQuota = 1500

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-nonpercall-token-fail", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatusSuccess
	task.Properties.OriginModelName = "gpt-4"
	task.PrivateData.BillingContext.OriginModelName = "gpt-4"
	require.NoError(t, model.DB.Create(task).Error)

	adaptor := &mockAdaptor{adjustReturn: 0}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, TotalTokens: totalTokens}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	require.Equal(t, preConsumed, reloaded.Quota)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.Equal(t, expectedActualQuota, reloaded.PrivateData.SettlementAttemptQuota)
	require.NotEmpty(t, reloaded.PrivateData.SettlementError)
	require.NotEmpty(t, reloaded.FailReason)
	require.Contains(t, reloaded.FailReason, "billing settlement requires manual review")
	record, exists, err := model.GetTaskSettlementRecord(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, model.TaskSettlementRecordStatusReview, record.Status)
	require.Contains(t, record.Error, "task quota settlement token adjustment failed")
	require.Equal(t, initQuota, getUserQuota(t, userID))
	require.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
}

func TestSettle_NonPerCallBilling_FloorsPositiveTokenSettlementToOne(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	oldModelRatios := ratio_setting.ModelRatio2JSONString()
	oldGroupRatios := ratio_setting.GroupRatio2JSONString()
	oldGroupGroupRatios := ratio_setting.GroupGroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"tiny-token-model":0.4}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldModelRatios))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroupRatios))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(oldGroupGroupRatios))
	})

	const userID, tokenID, channelID = 35, 35, 35
	const initQuota, preConsumed = 10000, 10
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-token-floor", tokenRemain)
	seedChannel(t, channelID)
	setUserUsageCounters(t, userID, preConsumed, 1)
	setChannelUsedQuota(t, channelID, int64(preConsumed))

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Properties.OriginModelName = "tiny-token-model"
	task.PrivateData.BillingContext.OriginModelName = "tiny-token-model"
	require.NoError(t, model.DB.Create(task).Error)

	adaptor := &mockAdaptor{adjustReturn: 0}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, TotalTokens: 1}

	settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	require.Equal(t, 1, reloaded.Quota)
	require.Empty(t, reloaded.SettlementStatus)
	require.Equal(t, initQuota+preConsumed-1, getUserQuota(t, userID))
	require.Equal(t, tokenRemain+preConsumed-1, getTokenRemainQuota(t, tokenID))
	require.EqualValues(t, 1, getChannelUsedQuota(t, channelID))

	log := getLastLog(t)
	require.NotNil(t, log)
	require.Equal(t, model.LogTypeRefund, log.Type)
	require.Equal(t, preConsumed-1, log.Quota)
	require.Equal(t, "test_user", log.Username)
	require.Equal(t, "test_token", log.TokenName)
}
