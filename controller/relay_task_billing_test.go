package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type relayTaskTestBilling struct {
	preConsumed   int
	settled       bool
	refunded      bool
	refundCalls   int
	rollbackCalls int
	refundApplied int
	rollbackErr   error
	refundErr     error
}

type relayTaskCompletionBillingAdaptor struct {
	calls int
	quota int
}

func (b *relayTaskTestBilling) Settle(actualQuota int) error {
	b.settled = true
	return nil
}

func (b *relayTaskTestBilling) Refund(c *gin.Context) error {
	b.refundCalls++
	if b.refundErr != nil {
		return b.refundErr
	}
	if b.settled || b.refunded {
		return nil
	}
	b.refunded = true
	b.refundApplied++
	return nil
}

func (b *relayTaskTestBilling) NeedsRefund() bool {
	return !b.refunded
}

func (b *relayTaskTestBilling) GetPreConsumedQuota() int {
	return b.preConsumed
}

func (b *relayTaskTestBilling) Reserve(targetQuota int) error {
	return nil
}

func (b *relayTaskTestBilling) Rollback(actualQuota int) error {
	b.rollbackCalls++
	if b.rollbackErr != nil {
		return b.rollbackErr
	}
	if b.refunded {
		return nil
	}
	b.refunded = true
	b.refundApplied++
	return nil
}

func (a *relayTaskCompletionBillingAdaptor) AdjustBillingOnComplete(*model.Task, *relaycommon.TaskInfo) int {
	a.calls++
	return a.quota
}

func installRelayTaskTestHooks(t *testing.T, billing *relayTaskTestBilling, publicTaskID string, quota int) {
	t.Helper()
	oldSubmit := relayTaskSubmitFunc
	oldSettle := settleBillingFunc
	oldLog := logTaskConsumptionFunc
	oldRefund := refundTaskQuotaFunc
	t.Cleanup(func() {
		relayTaskSubmitFunc = oldSubmit
		settleBillingFunc = oldSettle
		logTaskConsumptionFunc = oldLog
		refundTaskQuotaFunc = oldRefund
	})
	relayTaskSubmitFunc = func(c *gin.Context, info *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		info.InitChannelMeta(c)
		info.Billing = billing
		info.FinalPreConsumedQuota = billing.preConsumed
		info.BillingSource = service.BillingSourceWallet
		info.Action = "generate"
		info.PriceData.Quota = quota
		if info.TaskRelayInfo == nil {
			info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
		}
		info.TaskRelayInfo.PublicTaskID = publicTaskID
		return &relay.TaskSubmitResult{
			UpstreamTaskID: "upstream-" + publicTaskID,
			TaskData:       []byte(`{"id":"upstream-task"}`),
			Platform:       constant.TaskPlatformSuno,
			Quota:          quota,
		}, nil
	}
}

func TestRelayTaskImmediateFailureRefundsWithoutConsumptionSettlement(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	insertRelayTaskTestChannel(t, 303)

	billing := &relayTaskTestBilling{preConsumed: 100}
	oldSubmit := relayTaskSubmitFunc
	oldSettle := settleBillingFunc
	oldLog := logTaskConsumptionFunc
	oldRefund := refundTaskQuotaFunc
	t.Cleanup(func() {
		relayTaskSubmitFunc = oldSubmit
		settleBillingFunc = oldSettle
		logTaskConsumptionFunc = oldLog
		refundTaskQuotaFunc = oldRefund
	})
	relayTaskSubmitFunc = func(c *gin.Context, info *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		info.InitChannelMeta(c)
		info.Billing = billing
		info.FinalPreConsumedQuota = billing.preConsumed
		info.BillingSource = service.BillingSourceWallet
		info.Action = "generate"
		info.PriceData.Quota = 150
		if info.TaskRelayInfo == nil {
			info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
		}
		info.TaskRelayInfo.PublicTaskID = "task-immediate-failure"
		return &relay.TaskSubmitResult{
			UpstreamTaskID: "upstream-immediate-failure",
			TaskData:       []byte(`{"id":"upstream-task"}`),
			Platform:       constant.TaskPlatformSuno,
			Quota:          150,
			Immediate: &relaycommon.TaskInfo{
				Status:   string(model.TaskStatusFailure),
				Progress: "100%",
				Reason:   "upstream rejected the task",
			},
		}, nil
	}
	settleBillingFunc = func(*gin.Context, *relaycommon.RelayInfo, int) error {
		t.Fatal("immediate failure must not settle successful consumption")
		return nil
	}
	logTaskConsumptionFunc = func(*gin.Context, *relaycommon.RelayInfo) error {
		t.Fatal("immediate failure must not write a consumption log")
		return nil
	}
	refundTaskQuotaFunc = func(ctx context.Context, task *model.Task, reason string) error {
		require.Equal(t, 100, task.Quota)
		require.Equal(t, "upstream rejected the task", reason)
		require.True(t, task.PrivateData.PreConsumedUsageCaptured)
		require.False(t, task.PrivateData.PreConsumedUsageRecorded)
		task.Quota = 0
		return task.UpdateQuota()
	}
	ctx, recorder := newRelayTaskTestContext(303, 303, 303)

	RelayTask(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"status":"failed"`)
	var task model.Task
	require.NoError(t, db.First(&task, "task_id = ?", "task-immediate-failure").Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
	require.Zero(t, task.Quota)
	require.Equal(t, "upstream rejected the task", task.FailReason)
}

func TestTaskSubmissionStatusReflectsImmediateTerminalState(t *testing.T) {
	require.Equal(t, "queued", taskSubmissionStatus(&model.Task{Status: model.TaskStatusNotStart}))
	require.Equal(t, "in_progress", taskSubmissionStatus(&model.Task{Status: model.TaskStatusInProgress}))
	require.Equal(t, "completed", taskSubmissionStatus(&model.Task{Status: model.TaskStatusSuccess}))
	require.Equal(t, "failed", taskSubmissionStatus(&model.Task{Status: model.TaskStatusFailure}))
}

func TestTaskSubmissionStatusMapsRetryableSettlementReviewToInProgress(t *testing.T) {
	require.Equal(t, "in_progress", taskSubmissionStatus(&model.Task{
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusReview,
		NextPollAt:       time.Now().Unix() + 60,
	}))
	require.Equal(t, "failed", taskSubmissionStatus(&model.Task{
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusReview,
	}))
}

func TestTaskSubmissionStatusMapsPendingSettlementToInProgress(t *testing.T) {
	require.Equal(t, "in_progress", taskSubmissionStatus(&model.Task{
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusPending,
	}))
	require.Equal(t, "in_progress", taskSubmissionStatus(&model.Task{
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusApplied,
	}))
	require.Equal(t, "completed", taskSubmissionStatus(&model.Task{
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusSettled,
	}))
}

func TestTaskPluginSubmissionPersistsSelectedChannelKey(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	oldSettle := settleBillingFunc
	oldLog := logTaskConsumptionFunc
	t.Cleanup(func() {
		settleBillingFunc = oldSettle
		logTaskConsumptionFunc = oldLog
	})
	settleBillingFunc = func(*gin.Context, *relaycommon.RelayInfo, int) error {
		return nil
	}
	logTaskConsumptionFunc = func(*gin.Context, *relaycommon.RelayInfo) error {
		return nil
	}

	ctx, _ := newRelayTaskTestContext(305, 0, 305)
	ctx.Set(pluginruntime.ContextKeyPinnedPlugin, pluginruntime.PinnedPlugin{
		Plugin: &pluginruntime.LoadedPlugin{
			Meta: pluginruntime.Meta{Key: "task-plugin", Version: "1.0.0"},
		},
	})
	info := &relaycommon.RelayInfo{
		UserId:          305,
		TokenGroup:      "default",
		OriginModelName: "task-plugin-model",
	}
	info.TaskRelayInfo = &relaycommon.TaskRelayInfo{PublicTaskID: "task-plugin-key"}

	outcome, taskErr := executeTaskSubmissionWith(ctx, info, func(c *gin.Context, info *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		info.InitChannelMeta(c)
		return &relay.TaskSubmitResult{
			UpstreamTaskID: "upstream-task-plugin-key",
			TaskData:       []byte(`{"status":"queued"}`),
			Platform:       constant.TaskPlatform("task-plugin"),
		}, nil
	})
	require.Nil(t, taskErr)
	require.NotNil(t, outcome)
	require.Equal(t, "sk-upstream", outcome.Task.PrivateData.Key)

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, "task_id = ?", "task-plugin-key").Error)
	require.Equal(t, "sk-upstream", reloaded.PrivateData.Key)
}

func TestRelayTaskImmediateSuccessInvokesCompletionBilling(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	insertRelayTaskTestChannel(t, 304)

	billing := &relayTaskTestBilling{preConsumed: 100}
	completion := &relayTaskCompletionBillingAdaptor{}
	oldSubmit := relayTaskSubmitFunc
	oldSettle := settleBillingFunc
	oldLog := logTaskConsumptionFunc
	oldRefund := refundTaskQuotaFunc
	t.Cleanup(func() {
		relayTaskSubmitFunc = oldSubmit
		settleBillingFunc = oldSettle
		logTaskConsumptionFunc = oldLog
		refundTaskQuotaFunc = oldRefund
	})
	relayTaskSubmitFunc = func(c *gin.Context, info *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		info.InitChannelMeta(c)
		info.Billing = billing
		info.FinalPreConsumedQuota = billing.preConsumed
		info.BillingSource = service.BillingSourceWallet
		info.Action = "generate"
		info.PriceData.Quota = 100
		if info.TaskRelayInfo == nil {
			info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
		}
		info.TaskRelayInfo.PublicTaskID = "task-immediate-success"
		return &relay.TaskSubmitResult{
			UpstreamTaskID: "upstream-immediate-success",
			TaskData:       []byte(`{"id":"upstream-task"}`),
			Platform:       constant.TaskPlatformSuno,
			Quota:          100,
			Immediate: &relaycommon.TaskInfo{
				Status:    string(model.TaskStatusSuccess),
				Progress:  "80%",
				RemoteUrl: "https://cdn.example/immediate.mp4",
			},
			CompletionBillingAdaptor: completion,
		}, nil
	}
	settleBillingFunc = func(*gin.Context, *relaycommon.RelayInfo, int) error { return nil }
	logTaskConsumptionFunc = func(*gin.Context, *relaycommon.RelayInfo) error { return nil }
	refundTaskQuotaFunc = func(context.Context, *model.Task, string) error {
		t.Fatal("immediate success must not refund")
		return nil
	}
	ctx, recorder := newRelayTaskTestContext(304, 304, 304)

	RelayTask(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"status":"completed"`)
	require.Equal(t, 1, completion.calls)
	var task model.Task
	require.NoError(t, db.First(&task, "task_id = ?", "task-immediate-success").Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	require.Equal(t, model.TaskSettlementStatusSettled, task.SettlementStatus)
	require.Equal(t, "https://cdn.example/immediate.mp4", task.PrivateData.ResultURL)
	require.NotZero(t, task.FinishTime)
}

func TestRelayTaskImmediateSuccessDoesNotSettleBillingTwice(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskSettlementRecord{}))
	insertRelayTaskTestChannel(t, 305)

	billing := &relayTaskTestBilling{preConsumed: 100}
	completion := &relayTaskCompletionBillingAdaptor{quota: 150}
	oldSubmit := relayTaskSubmitFunc
	oldSettle := settleBillingFunc
	oldLog := logTaskConsumptionFunc
	oldRefund := refundTaskQuotaFunc
	t.Cleanup(func() {
		relayTaskSubmitFunc = oldSubmit
		settleBillingFunc = oldSettle
		logTaskConsumptionFunc = oldLog
		refundTaskQuotaFunc = oldRefund
	})
	relayTaskSubmitFunc = func(c *gin.Context, info *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		info.InitChannelMeta(c)
		info.Billing = billing
		info.FinalPreConsumedQuota = billing.preConsumed
		info.BillingSource = service.BillingSourceWallet
		info.Action = "generate"
		info.PriceData.Quota = 100
		if info.TaskRelayInfo == nil {
			info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
		}
		info.TaskRelayInfo.PublicTaskID = "task-immediate-no-double-settle"
		return &relay.TaskSubmitResult{
			UpstreamTaskID: "upstream-immediate-no-double-settle",
			TaskData:       []byte(`{"id":"upstream-task"}`),
			Platform:       constant.TaskPlatformSuno,
			Quota:          100,
			Immediate: &relaycommon.TaskInfo{
				Status:   string(model.TaskStatusSuccess),
				Progress: "100%",
			},
			CompletionBillingAdaptor: completion,
		}, nil
	}
	settleCalls := 0
	settledQuota := 0
	settleBillingFunc = func(_ *gin.Context, _ *relaycommon.RelayInfo, quota int) error {
		settleCalls++
		settledQuota = quota
		return nil
	}
	logTaskConsumptionFunc = func(*gin.Context, *relaycommon.RelayInfo) error { return nil }
	refundTaskQuotaFunc = func(context.Context, *model.Task, string) error { return nil }
	ctx, recorder := newRelayTaskTestContext(305, 305, 305)

	RelayTask(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, completion.calls)
	require.Equal(t, 1, settleCalls)
	require.Equal(t, 150, settledQuota)
}

func insertRelayTaskTestChannel(t *testing.T, channelID int) {
	t.Helper()

	channel := &model.Channel{
		Id:     channelID,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "sk-relay-task",
		Status: common.ChannelStatusEnabled,
		Name:   "relay-task-test",
		Models: "suno_music",
		Group:  "default",
	}
	require.NoError(t, channel.Insert())
}

func newRelayTaskTestContext(userID int, tokenID int, channelID int) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/generations", strings.NewReader(`{"model":"suno_music","prompt":"test"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("platform", string(constant.TaskPlatformSuno))
	ctx.Set("token_name", "test_token")
	common.SetContextKey(ctx, constant.ContextKeyUserId, userID)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUserQuota, 10000)
	common.SetContextKey(ctx, constant.ContextKeyUserName, "test_user")
	common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "suno_music")
	common.SetContextKey(ctx, constant.ContextKeyTokenId, tokenID)
	common.SetContextKey(ctx, constant.ContextKeyTokenKey, "sk-relay-task")
	common.SetContextKey(ctx, constant.ContextKeyChannelId, channelID)
	common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(ctx, constant.ContextKeyChannelName, "relay-task-test")
	common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, "https://example.invalid")
	common.SetContextKey(ctx, constant.ContextKeyChannelKey, "sk-upstream")
	return ctx, recorder
}

func TestTaskQuotaAfterSubmitSettlementUsesPreConsumedQuotaWhenSettlementFails(t *testing.T) {
	relayInfo := &relaycommon.RelayInfo{
		FinalPreConsumedQuota: 100,
	}
	relayInfo.Billing = &service.BillingSession{}

	quota := taskQuotaAfterSubmitSettlement(relayInfo, 150, errors.New("settlement failed"))

	require.Equal(t, 100, quota)
}

func TestTaskQuotaAfterSubmitSettlementUsesAttemptedQuotaWhenSettlementSucceeds(t *testing.T) {
	quota := taskQuotaAfterSubmitSettlement(&relaycommon.RelayInfo{
		FinalPreConsumedQuota: 100,
	}, 150, nil)

	require.Equal(t, 150, quota)
}

func TestAttachTaskSubmitSettlementErrorRecordsAttemptedQuota(t *testing.T) {
	task := &model.Task{}

	attachTaskSubmitSettlementError(task, 150, errors.New("settlement failed\nwith detail"))

	require.Equal(t, 150, task.PrivateData.SettlementAttemptQuota)
	require.Equal(t, "settlement failed with detail", task.PrivateData.SettlementError)
}

func TestAttachTaskSubmitSettlementErrorAppendsExistingError(t *testing.T) {
	task := &model.Task{}
	task.PrivateData.SettlementError = "settlement failed"

	attachTaskSubmitSettlementError(task, 150, errors.New("record consume log failed"))

	require.Equal(t, "settlement failed; record consume log failed", task.PrivateData.SettlementError)
}

func TestPersistTaskSubmitSettlementErrorReturnsUpdateFailure(t *testing.T) {
	task := &model.Task{TaskID: "missing-task-row"}
	relayInfo := &relaycommon.RelayInfo{FinalPreConsumedQuota: 100}

	err := persistTaskSubmitSettlementError(task, relayInfo, 150, errors.New("settlement failed"))

	require.Error(t, err)
	require.Equal(t, 100, task.Quota)
	require.Equal(t, 150, task.PrivateData.SettlementAttemptQuota)
	require.Equal(t, "settlement failed", task.PrivateData.SettlementError)
}

func TestPersistTaskSubmitSettlementErrorWritesFallbackQuotaAndReviewStatus(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	task := &model.Task{
		TaskID: "task-settlement-review",
		Quota:  150,
	}
	require.NoError(t, db.Create(task).Error)

	err := persistTaskSubmitSettlementError(
		task,
		&relaycommon.RelayInfo{FinalPreConsumedQuota: 100},
		150,
		errors.New("settlement failed\nwith detail"),
	)

	require.NoError(t, err)
	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, "id = ?", task.ID).Error)
	require.Equal(t, 100, reloaded.Quota)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.Equal(t, 150, reloaded.PrivateData.SettlementAttemptQuota)
	require.Equal(t, "settlement failed with detail", reloaded.PrivateData.SettlementError)
	require.Greater(t, reloaded.NextPollAt, time.Now().Unix())
	require.LessOrEqual(t, reloaded.NextPollAt, time.Now().Unix()+int64(service.TaskSettlementReviewRetrySeconds)+1)
}

func TestPersistTaskSubmitSettlementErrorDoesNotReinsertMissingTask(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	task := &model.Task{
		TaskID: "task-settlement-missing",
		Quota:  150,
	}
	require.NoError(t, db.Create(task).Error)
	require.NoError(t, db.Delete(&model.Task{}, task.ID).Error)

	err := persistTaskSubmitSettlementError(
		task,
		&relaycommon.RelayInfo{FinalPreConsumedQuota: 100},
		150,
		errors.New("settlement failed"),
	)

	require.Error(t, err)
	var count int64
	require.NoError(t, db.Model(&model.Task{}).Where("id = ?", task.ID).Count(&count).Error)
	require.Zero(t, count)
}

func TestFailPersistedTaskAfterSubmitSettlementErrorMarksRefundedFailure(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	task := &model.Task{
		TaskID:   "task-submit-review-fallback",
		Quota:    150,
		Status:   model.TaskStatusNotStart,
		Progress: "0%",
	}
	require.NoError(t, db.Create(task).Error)

	err := failPersistedTaskAfterSubmitSettlementError(
		task,
		&relaycommon.RelayInfo{FinalPreConsumedQuota: 100},
		150,
		errors.New("settlement failed"),
		errors.New("review update failed"),
	)

	require.NoError(t, err)
	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, 0, reloaded.Quota)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status)
	require.Equal(t, "100%", reloaded.Progress)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.Equal(t, 150, reloaded.PrivateData.SettlementAttemptQuota)
	require.Contains(t, reloaded.PrivateData.SettlementError, "settlement failed")
	require.Contains(t, reloaded.PrivateData.SettlementError, "review update failed")
	require.Equal(t, model.TaskPublicSettlementFailReason, reloaded.FailReason)
	require.NotZero(t, reloaded.FinishTime)
	require.GreaterOrEqual(t, reloaded.FinishTime, common.GetTimestamp()-5)
}

func TestFailPersistedTaskAfterSubmitAccountingErrorMarksReviewAndEndsTask(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	task := &model.Task{
		TaskID:   "task-submit-accounting-fallback",
		Quota:    100,
		Status:   model.TaskStatusNotStart,
		Progress: "0%",
	}
	require.NoError(t, db.Create(task).Error)

	err := failPersistedTaskAfterSubmitAccountingError(
		task,
		&relaycommon.RelayInfo{FinalPreConsumedQuota: 100},
		150,
		errors.New("record consume log failed"),
	)

	require.NoError(t, err)
	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, 0, reloaded.Quota)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status)
	require.Equal(t, "100%", reloaded.Progress)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.Equal(t, 150, reloaded.PrivateData.SettlementAttemptQuota)
	require.Equal(t, "record consume log failed", reloaded.PrivateData.SettlementError)
	require.Equal(t, model.TaskPublicAccountingFailReason, reloaded.FailReason)
	require.NotZero(t, reloaded.FinishTime)
}

func TestFailPersistedTaskAfterSubmitAccountingErrorStopsPolling(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	task := &model.Task{
		TaskID:   "task-submit-accounting-pollable",
		Quota:    100,
		Status:   model.TaskStatusNotStart,
		Progress: "0%",
	}
	require.NoError(t, db.Create(task).Error)

	err := failPersistedTaskAfterSubmitAccountingError(
		task,
		&relaycommon.RelayInfo{FinalPreConsumedQuota: 100},
		150,
		errors.New("record consume log failed"),
	)

	require.NoError(t, err)
	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, 0, reloaded.Quota)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status)
	require.Equal(t, "100%", reloaded.Progress)
	require.NotZero(t, reloaded.FinishTime)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.Equal(t, 150, reloaded.PrivateData.SettlementAttemptQuota)
	require.Contains(t, reloaded.PrivateData.SettlementError, "record consume log failed")
	require.Equal(t, model.TaskPublicAccountingFailReason, reloaded.FailReason)
}

func TestRelayTaskSettleFailurePersistsReviewAndDoesNotRefundSubmittedTask(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Log{}))
	insertRelayTaskTestChannel(t, 301)

	billing := &relayTaskTestBilling{preConsumed: 100}
	installRelayTaskTestHooks(t, billing, "task-controller-settle-review", 150)
	settleBillingFunc = func(c *gin.Context, info *relaycommon.RelayInfo, actualQuota int) error {
		require.Equal(t, 150, actualQuota)
		return errors.New("settlement failed")
	}
	logCalled := false
	logTaskConsumptionFunc = func(c *gin.Context, info *relaycommon.RelayInfo) error {
		logCalled = true
		return nil
	}
	ctx, _ := newRelayTaskTestContext(301, 301, 301)

	RelayTask(ctx)

	require.False(t, logCalled)
	require.Zero(t, billing.refundApplied)
	require.Zero(t, billing.refundCalls)
	var task model.Task
	require.NoError(t, db.First(&task, "task_id = ?", "task-controller-settle-review").Error)
	require.Equal(t, 100, task.Quota)
	require.Equal(t, model.TaskSettlementStatusReview, task.SettlementStatus)
	require.Equal(t, 150, task.PrivateData.SettlementAttemptQuota)
	require.Equal(t, "settlement failed", task.PrivateData.SettlementError)
	require.Equal(t, service.TaskSettlementReviewFailReason, task.FailReason)
	require.Greater(t, task.NextPollAt, time.Now().Unix())
}

func TestRelayTaskImmediateSuccessSettleFailureSchedulesReviewRetry(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Log{}))
	insertRelayTaskTestChannel(t, 306)

	billing := &relayTaskTestBilling{preConsumed: 100}
	oldSubmit := relayTaskSubmitFunc
	oldSettle := settleBillingFunc
	oldLog := logTaskConsumptionFunc
	oldRefund := refundTaskQuotaFunc
	t.Cleanup(func() {
		relayTaskSubmitFunc = oldSubmit
		settleBillingFunc = oldSettle
		logTaskConsumptionFunc = oldLog
		refundTaskQuotaFunc = oldRefund
	})
	relayTaskSubmitFunc = func(c *gin.Context, info *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		info.InitChannelMeta(c)
		info.Billing = billing
		info.FinalPreConsumedQuota = billing.preConsumed
		info.BillingSource = service.BillingSourceWallet
		info.Action = "generate"
		info.PriceData.Quota = 100
		if info.TaskRelayInfo == nil {
			info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
		}
		info.TaskRelayInfo.PublicTaskID = "task-immediate-settle-review"
		return &relay.TaskSubmitResult{
			UpstreamTaskID: "upstream-immediate-settle-review",
			TaskData:       []byte(`{"id":"upstream-task"}`),
			Platform:       constant.TaskPlatformSuno,
			Quota:          100,
			Immediate: &relaycommon.TaskInfo{
				Status:    string(model.TaskStatusSuccess),
				Progress:  "100%",
				RemoteUrl: "https://cdn.example/immediate.mp4",
			},
		}, nil
	}
	settleBillingFunc = func(*gin.Context, *relaycommon.RelayInfo, int) error {
		return errors.New("settlement failed")
	}
	logCalls := 0
	logTaskConsumptionFunc = func(*gin.Context, *relaycommon.RelayInfo) error {
		logCalls++
		return nil
	}
	refundTaskQuotaFunc = func(context.Context, *model.Task, string) error {
		t.Fatal("immediate success settlement review must not refund")
		return nil
	}
	ctx, recorder := newRelayTaskTestContext(306, 306, 306)

	RelayTask(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"status":"in_progress"`)
	require.NotContains(t, recorder.Body.String(), `"status":"completed"`)
	require.Zero(t, billing.refundCalls)
	require.Zero(t, logCalls)
	var task model.Task
	require.NoError(t, db.First(&task, "task_id = ?", "task-immediate-settle-review").Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	require.Equal(t, model.TaskSettlementStatusReview, task.SettlementStatus)
	require.Equal(t, "https://cdn.example/immediate.mp4", task.PrivateData.ResultURL)
	require.Greater(t, task.NextPollAt, time.Now().Unix())
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), task.PublicStatus())
}

func TestRelayTaskLogFailureAfterSubmitMarksReviewAndRefunds(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Log{}))
	insertRelayTaskTestChannel(t, 302)

	billing := &relayTaskTestBilling{preConsumed: 100}
	installRelayTaskTestHooks(t, billing, "task-controller-log-review", 150)
	settleBillingFunc = func(c *gin.Context, info *relaycommon.RelayInfo, actualQuota int) error {
		require.Equal(t, 150, actualQuota)
		require.NotNil(t, info.Billing)
		return info.Billing.Settle(actualQuota)
	}
	logTaskConsumptionFunc = func(c *gin.Context, info *relaycommon.RelayInfo) error {
		return errors.New("record consume log failed")
	}
	ctx, recorder := newRelayTaskTestContext(302, 302, 302)

	RelayTask(ctx)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Contains(t, recorder.Body.String(), "log_task_consumption_failed")
	require.Equal(t, 1, billing.rollbackCalls)
	require.Equal(t, 1, billing.refundApplied)
	require.Zero(t, billing.refundCalls)
	var task model.Task
	require.NoError(t, db.First(&task, "task_id = ?", "task-controller-log-review").Error)
	require.Equal(t, 0, task.Quota)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
	require.Equal(t, "100%", task.Progress)
	require.Equal(t, model.TaskSettlementStatusReview, task.SettlementStatus)
	require.Equal(t, 150, task.PrivateData.SettlementAttemptQuota)
	require.Contains(t, task.PrivateData.SettlementError, "record consume log failed")
	require.Equal(t, model.TaskPublicAccountingFailReason, task.FailReason)
}

func TestRelayTaskLogFailureAfterSettleRollsBackWalletQuota(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Log{}, &model.Token{}))
	insertRelayTaskTestChannel(t, 312)

	const userID, tokenID, channelID = 312, 312, 312
	const initQuota, preConsumed = 10000, 150
	require.NoError(t, db.Create(&model.User{
		Id:       userID,
		Username: "relay-task-log-rollback",
		Quota:    initQuota,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Token{
		Id:          tokenID,
		UserId:      userID,
		Key:         "sk-relay-task-log-rollback",
		Name:        "relay-task-log-rollback",
		Status:      common.TokenStatusEnabled,
		RemainQuota: initQuota,
	}).Error)

	oldSubmit := relayTaskSubmitFunc
	oldSettle := settleBillingFunc
	oldLog := logTaskConsumptionFunc
	t.Cleanup(func() {
		relayTaskSubmitFunc = oldSubmit
		settleBillingFunc = oldSettle
		logTaskConsumptionFunc = oldLog
	})
	relayTaskSubmitFunc = func(c *gin.Context, info *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		info.InitChannelMeta(c)
		info.UserId = userID
		info.TokenId = tokenID
		info.TokenKey = "sk-relay-task-log-rollback"
		info.ForcePreConsume = true
		info.UserSetting.BillingPreference = "wallet_only"
		info.BillingSource = service.BillingSourceWallet
		info.Action = "generate"
		info.PriceData.Quota = preConsumed
		session, apiErr := service.NewBillingSession(c, info, preConsumed)
		require.Nil(t, apiErr)
		require.NotNil(t, session)
		info.Billing = session
		info.FinalPreConsumedQuota = preConsumed
		if info.TaskRelayInfo == nil {
			info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
		}
		info.TaskRelayInfo.PublicTaskID = "task-controller-log-rollback-wallet"
		return &relay.TaskSubmitResult{
			UpstreamTaskID: "upstream-log-rollback-wallet",
			TaskData:       []byte(`{"id":"upstream-task"}`),
			Platform:       constant.TaskPlatformSuno,
			Quota:          preConsumed,
		}, nil
	}
	settleBillingFunc = service.SettleBilling
	logTaskConsumptionFunc = func(*gin.Context, *relaycommon.RelayInfo) error {
		return errors.New("record consume log failed")
	}
	ctx, recorder := newRelayTaskTestContext(userID, tokenID, channelID)

	RelayTask(ctx)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Contains(t, recorder.Body.String(), "log_task_consumption_failed")
	userQuota, err := model.GetUserQuota(userID, false)
	require.NoError(t, err)
	require.EqualValues(t, initQuota, userQuota)
	var token model.Token
	require.NoError(t, db.First(&token, tokenID).Error)
	require.EqualValues(t, initQuota, token.RemainQuota)
	var task model.Task
	require.NoError(t, db.First(&task, "task_id = ?", "task-controller-log-rollback-wallet").Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
	require.Equal(t, model.TaskSettlementStatusReview, task.SettlementStatus)
}

func TestRelayTaskLogFailureWhenRollbackFailsKeepsRefundPending(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Log{}))
	insertRelayTaskTestChannel(t, 322)

	billing := &relayTaskTestBilling{
		preConsumed: 100,
		rollbackErr: errors.New("wallet credit failed"),
	}
	installRelayTaskTestHooks(t, billing, "task-controller-log-rollback-fail", 150)
	settleBillingFunc = func(c *gin.Context, info *relaycommon.RelayInfo, actualQuota int) error {
		require.Equal(t, 150, actualQuota)
		require.NotNil(t, info.Billing)
		return info.Billing.Settle(actualQuota)
	}
	logTaskConsumptionFunc = func(*gin.Context, *relaycommon.RelayInfo) error {
		return errors.New("record consume log failed")
	}
	ctx, recorder := newRelayTaskTestContext(322, 322, 322)

	RelayTask(ctx)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Contains(t, recorder.Body.String(), "log_task_consumption_failed")
	require.Equal(t, 1, billing.rollbackCalls)
	require.Zero(t, billing.refundCalls)
	require.Zero(t, billing.refundApplied)
	var task model.Task
	require.NoError(t, db.First(&task, "task_id = ?", "task-controller-log-rollback-fail").Error)
	require.Equal(t, 150, task.Quota)
	require.True(t, task.RefundPending)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
	require.NotEqual(t, model.TaskSettlementStatusReview, task.SettlementStatus)
	require.Equal(t, 150, task.PrivateData.SettlementAttemptQuota)
	require.Equal(t, model.TaskPublicAccountingFailReason, task.FailReason)
	require.Contains(t, task.PrivateData.SettlementError, "record consume log failed")

	pending, err := model.GetPendingTaskRefundsAfter(0, 100)
	require.NoError(t, err)
	found := false
	for _, item := range pending {
		if item.TaskID == "task-controller-log-rollback-fail" {
			found = true
			require.Equal(t, 150, item.Quota)
			require.True(t, item.RefundPending)
			break
		}
	}
	require.True(t, found, "rollback-failed submit must remain refundable for the pending-refund sweeper")
}

func TestRelayTaskLogFailureWhenRollbackFailsSweeperRestoresWallet(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Log{}, &model.Token{}, &model.TaskSettlementRecord{}, &model.QuotaData{}))
	insertRelayTaskTestChannel(t, 323)

	const userID, tokenID, channelID = 323, 323, 323
	const initQuota, preConsumed = 10000, 150
	require.NoError(t, db.Create(&model.User{
		Id:       userID,
		Username: "relay-task-log-rollback-fail",
		Quota:    initQuota,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Token{
		Id:          tokenID,
		UserId:      userID,
		Key:         "sk-relay-task-log-rollback-fail",
		Name:        "relay-task-log-rollback-fail",
		Status:      common.TokenStatusEnabled,
		RemainQuota: initQuota,
	}).Error)

	wrapped := &rollbackFailBilling{failWith: errors.New("wallet credit failed")}
	oldSubmit := relayTaskSubmitFunc
	oldSettle := settleBillingFunc
	oldLog := logTaskConsumptionFunc
	t.Cleanup(func() {
		relayTaskSubmitFunc = oldSubmit
		settleBillingFunc = oldSettle
		logTaskConsumptionFunc = oldLog
	})
	relayTaskSubmitFunc = func(c *gin.Context, info *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		info.InitChannelMeta(c)
		info.UserId = userID
		info.TokenId = tokenID
		info.TokenKey = "sk-relay-task-log-rollback-fail"
		info.ForcePreConsume = true
		info.UserSetting.BillingPreference = "wallet_only"
		info.BillingSource = service.BillingSourceWallet
		info.Action = "generate"
		info.PriceData.Quota = preConsumed
		session, apiErr := service.NewBillingSession(c, info, preConsumed)
		require.Nil(t, apiErr)
		require.NotNil(t, session)
		wrapped.inner = session
		info.Billing = wrapped
		info.FinalPreConsumedQuota = preConsumed
		if info.TaskRelayInfo == nil {
			info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
		}
		info.TaskRelayInfo.PublicTaskID = "task-controller-log-rollback-fail-wallet"
		return &relay.TaskSubmitResult{
			UpstreamTaskID: "upstream-log-rollback-fail-wallet",
			TaskData:       []byte(`{"id":"upstream-task"}`),
			Platform:       constant.TaskPlatformSuno,
			Quota:          preConsumed,
		}, nil
	}
	settleBillingFunc = service.SettleBilling
	logTaskConsumptionFunc = func(*gin.Context, *relaycommon.RelayInfo) error {
		return errors.New("record consume log failed")
	}
	ctx, recorder := newRelayTaskTestContext(userID, tokenID, channelID)

	RelayTask(ctx)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Contains(t, recorder.Body.String(), "log_task_consumption_failed")
	require.Equal(t, 1, wrapped.rollbacks)
	userQuota, err := model.GetUserQuota(userID, false)
	require.NoError(t, err)
	require.EqualValues(t, initQuota-preConsumed, userQuota)
	var token model.Token
	require.NoError(t, db.First(&token, tokenID).Error)
	require.EqualValues(t, initQuota-preConsumed, token.RemainQuota)
	var task model.Task
	require.NoError(t, db.First(&task, "task_id = ?", "task-controller-log-rollback-fail-wallet").Error)
	require.Equal(t, preConsumed, task.Quota)
	require.True(t, task.RefundPending)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
	require.NotEqual(t, model.TaskSettlementStatusReview, task.SettlementStatus)

	require.NoError(t, service.RefundTaskQuota(context.Background(), &task, task.FailReason))
	userQuota, err = model.GetUserQuota(userID, false)
	require.NoError(t, err)
	require.EqualValues(t, initQuota, userQuota)
	require.NoError(t, db.First(&token, tokenID).Error)
	require.EqualValues(t, initQuota, token.RemainQuota)
	require.NoError(t, db.First(&task, "task_id = ?", "task-controller-log-rollback-fail-wallet").Error)
	require.Zero(t, task.Quota)
	require.False(t, task.RefundPending)
}

type rollbackFailBilling struct {
	inner     relaycommon.BillingSettler
	failWith  error
	rollbacks int
}

func (b *rollbackFailBilling) Settle(actualQuota int) error {
	return b.inner.Settle(actualQuota)
}

func (b *rollbackFailBilling) Refund(c *gin.Context) error {
	return b.inner.Refund(c)
}

func (b *rollbackFailBilling) Rollback(actualQuota int) error {
	b.rollbacks++
	return b.failWith
}

func (b *rollbackFailBilling) NeedsRefund() bool {
	return b.inner.NeedsRefund()
}

func (b *rollbackFailBilling) GetPreConsumedQuota() int {
	return b.inner.GetPreConsumedQuota()
}

func (b *rollbackFailBilling) Reserve(targetQuota int) error {
	return b.inner.Reserve(targetQuota)
}

func TestRelayTaskKeepsUpstreamSuccessWhenClientCancelsAfterSubmit(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	insertRelayTaskTestChannel(t, 306)

	billing := &relayTaskTestBilling{preConsumed: 100}
	reqCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	oldSubmit := relayTaskSubmitFunc
	oldSettle := settleBillingFunc
	oldLog := logTaskConsumptionFunc
	t.Cleanup(func() {
		relayTaskSubmitFunc = oldSubmit
		settleBillingFunc = oldSettle
		logTaskConsumptionFunc = oldLog
	})
	relayTaskSubmitFunc = func(c *gin.Context, info *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		info.InitChannelMeta(c)
		info.Billing = billing
		info.FinalPreConsumedQuota = billing.preConsumed
		info.BillingSource = service.BillingSourceWallet
		info.Action = "generate"
		info.PriceData.Quota = 100
		if info.TaskRelayInfo == nil {
			info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
		}
		info.TaskRelayInfo.PublicTaskID = "task-client-cancel-after-submit"
		cancel()
		return &relay.TaskSubmitResult{
			UpstreamTaskID: "upstream-client-cancel-after-submit",
			TaskData:       []byte(`{"id":"upstream-task"}`),
			Platform:       constant.TaskPlatformSuno,
			Quota:          100,
		}, nil
	}
	settleBillingFunc = func(*gin.Context, *relaycommon.RelayInfo, int) error { return nil }
	logTaskConsumptionFunc = func(*gin.Context, *relaycommon.RelayInfo) error { return nil }

	ctx, recorder := newRelayTaskTestContext(306, 306, 306)
	ctx.Request = ctx.Request.WithContext(reqCtx)

	RelayTask(ctx)

	require.Zero(t, billing.refundCalls)
	var task model.Task
	require.NoError(t, db.First(&task, "task_id = ?", "task-client-cancel-after-submit").Error)
	require.Equal(t, "upstream-client-cancel-after-submit", task.PrivateData.UpstreamTaskID)
	require.Equal(t, 100, task.Quota)
	_ = recorder
}

func TestTaskRelayAccountingFailuresPersistAuditRecords(t *testing.T) {
	body, err := os.ReadFile("relay.go")
	require.NoError(t, err)
	source := string(body)

	for _, want := range []string{
		`RecordConsumeAccountingError(c, relayInfo, "refund billing after relay error"`,
		`RecordConsumeAccountingError(c, relayInfo, "refund billing after task error"`,
		`RecordConsumeAccountingError(c, relayInfo, "rollback billing after task error"`,
		`RecordConsumeAccountingError(c, relayInfo, "settle task billing"`,
		`RecordConsumeAccountingError(c, relayInfo, "persist task settlement review"`,
		`RecordConsumeAccountingError(c, relayInfo, "log task consumption"`,
		`RecordConsumeAccountingError(c, relayInfo, "persist task accounting review"`,
	} {
		require.Contains(t, source, want)
	}
	require.NotContains(t, source, "DeleteTaskByID")
	require.Contains(t, source, "ForceTaskRefundableAfterSubmitAccountingFailure")
}

func TestImageTaskCreationUsesAtomicBillingCommit(t *testing.T) {
	body, err := os.ReadFile("image_task.go")
	require.NoError(t, err)
	source := string(body)

	require.Contains(t, source, "service.CommitImageTaskCreation(")
	for _, removedCompensation := range []string{
		"refund image task billing after insert failure",
		"refund duplicate image task billing",
	} {
		require.NotContains(t, source, removedCompensation)
	}
}

func TestChannelTestHandlesConsumeLogError(t *testing.T) {
	body, err := os.ReadFile("channel-test.go")
	require.NoError(t, err)
	source := string(body)

	require.Contains(t, source, "if err := model.RecordConsumeLog")
	require.Contains(t, source, "record channel test consume log")
}

func TestRelayTaskLogFailureWhenRefundablePersistFailsStillMarksRefundPending(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	insertRelayTaskTestChannel(t, 324)

	billing := &relayTaskTestBilling{
		preConsumed: 100,
		rollbackErr: errors.New("wallet credit failed"),
	}
	oldPersist := persistRefundableSubmitAccountingFailureFunc
	installRelayTaskTestHooks(t, billing, "task-controller-log-persist-fail", 150)
	t.Cleanup(func() {
		persistRefundableSubmitAccountingFailureFunc = oldPersist
	})
	relayTaskSubmitFunc = func(c *gin.Context, info *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		info.InitChannelMeta(c)
		info.Billing = billing
		info.FinalPreConsumedQuota = billing.preConsumed
		info.BillingSource = service.BillingSourceWallet
		info.Action = "generate"
		info.PriceData.Quota = 150
		if info.TaskRelayInfo == nil {
			info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
		}
		info.TaskRelayInfo.PublicTaskID = "task-controller-log-persist-fail"
		return &relay.TaskSubmitResult{
			UpstreamTaskID: "upstream-log-persist-fail",
			TaskData:       []byte(`{"id":"upstream-task"}`),
			Platform:       constant.TaskPlatformSuno,
			Quota:          150,
			Immediate: &relaycommon.TaskInfo{
				Status:    string(model.TaskStatusSuccess),
				Progress:  "100%",
				RemoteUrl: "https://cdn.example/persist-fail.mp4",
			},
		}, nil
	}
	settleBillingFunc = func(c *gin.Context, info *relaycommon.RelayInfo, actualQuota int) error {
		require.NotNil(t, info.Billing)
		return info.Billing.Settle(actualQuota)
	}
	logTaskConsumptionFunc = func(*gin.Context, *relaycommon.RelayInfo) error {
		return errors.New("record consume log failed")
	}
	persistRefundableSubmitAccountingFailureFunc = func(*model.Task, int, error, error) error {
		return errors.New("persist refundable accounting failure")
	}
	ctx, recorder := newRelayTaskTestContext(324, 324, 324)

	RelayTask(ctx)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Contains(t, recorder.Body.String(), "log_task_consumption_failed")
	var task model.Task
	require.NoError(t, db.First(&task, "task_id = ?", "task-controller-log-persist-fail").Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
	require.True(t, task.RefundPending)
	require.Equal(t, 150, task.Quota)
	require.NotEqual(t, model.TaskSettlementStatusReview, task.SettlementStatus)

	pendingSettlements, err := model.GetPendingTaskSettlementsAfter(0, 100)
	require.NoError(t, err)
	for _, item := range pendingSettlements {
		require.NotEqual(t, "task-controller-log-persist-fail", item.TaskID)
	}
	pendingRefunds, err := model.GetPendingTaskRefundsAfter(0, 100)
	require.NoError(t, err)
	found := false
	for _, item := range pendingRefunds {
		if item.TaskID == "task-controller-log-persist-fail" {
			found = true
			require.True(t, item.RefundPending)
			require.Equal(t, 150, item.Quota)
			break
		}
	}
	require.True(t, found, "persist-fail after rollback-fail must remain refundable")
}

func TestRelayTaskLogFailureWhenRollbackFailsDoesNotDoubleDecrementUsage(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Log{}, &model.Token{}, &model.TaskSettlementRecord{}, &model.QuotaData{}))
	insertRelayTaskTestChannel(t, 325)

	const userID, tokenID, channelID = 325, 325, 325
	const initQuota, preConsumed = 10000, 150
	require.NoError(t, db.Create(&model.User{
		Id:           userID,
		Username:     "relay-task-log-rollback-usage",
		Quota:        initQuota,
		UsedQuota:    0,
		RequestCount: 0,
		Status:       common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Token{
		Id:          tokenID,
		UserId:      userID,
		Key:         "sk-relay-task-log-rollback-usage",
		Name:        "relay-task-log-rollback-usage",
		Status:      common.TokenStatusEnabled,
		RemainQuota: initQuota,
	}).Error)

	wrapped := &rollbackFailBilling{failWith: errors.New("wallet credit failed")}
	oldSubmit := relayTaskSubmitFunc
	oldSettle := settleBillingFunc
	oldLog := logTaskConsumptionFunc
	t.Cleanup(func() {
		relayTaskSubmitFunc = oldSubmit
		settleBillingFunc = oldSettle
		logTaskConsumptionFunc = oldLog
	})
	relayTaskSubmitFunc = func(c *gin.Context, info *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		info.InitChannelMeta(c)
		info.UserId = userID
		info.TokenId = tokenID
		info.TokenKey = "sk-relay-task-log-rollback-usage"
		info.ForcePreConsume = true
		info.UserSetting.BillingPreference = "wallet_only"
		info.BillingSource = service.BillingSourceWallet
		info.Action = "generate"
		info.PriceData.Quota = preConsumed
		session, apiErr := service.NewBillingSession(c, info, preConsumed)
		require.Nil(t, apiErr)
		require.NotNil(t, session)
		wrapped.inner = session
		info.Billing = wrapped
		info.FinalPreConsumedQuota = preConsumed
		if info.TaskRelayInfo == nil {
			info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
		}
		info.TaskRelayInfo.PublicTaskID = "task-controller-log-rollback-usage"
		return &relay.TaskSubmitResult{
			UpstreamTaskID: "upstream-log-rollback-usage",
			TaskData:       []byte(`{"id":"upstream-task"}`),
			Platform:       constant.TaskPlatformSuno,
			Quota:          preConsumed,
		}, nil
	}
	settleBillingFunc = service.SettleBilling
	logTaskConsumptionFunc = func(*gin.Context, *relaycommon.RelayInfo) error {
		return errors.New("record consume log failed")
	}
	ctx, recorder := newRelayTaskTestContext(userID, tokenID, channelID)

	RelayTask(ctx)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	var task model.Task
	require.NoError(t, db.First(&task, "task_id = ?", "task-controller-log-rollback-usage").Error)
	require.True(t, task.PrivateData.PreConsumedUsageCaptured)
	require.False(t, task.PrivateData.PreConsumedUsageRecorded)
	require.True(t, task.RefundPending)

	var user model.User
	require.NoError(t, db.Select("used_quota", "request_count", "quota").First(&user, userID).Error)
	require.EqualValues(t, 0, user.UsedQuota)
	require.Equal(t, 0, user.RequestCount)
	require.EqualValues(t, initQuota-preConsumed, user.Quota)

	require.NoError(t, service.RefundTaskQuota(context.Background(), &task, task.FailReason))
	require.NoError(t, db.First(&user, userID).Error)
	require.EqualValues(t, 0, user.UsedQuota)
	require.Equal(t, 0, user.RequestCount)
	require.EqualValues(t, initQuota, user.Quota)
}

func TestRelayTaskImmediateSuccessQuotaPersistFailureSettlesPrepaidAndMarksReview(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskSettlementRecord{}))
	insertRelayTaskTestChannel(t, 327)

	billing := &relayTaskTestBilling{preConsumed: 100}
	completion := &relayTaskCompletionBillingAdaptor{quota: 150}
	oldSubmit := relayTaskSubmitFunc
	oldSettle := settleBillingFunc
	oldLog := logTaskConsumptionFunc
	oldRefund := refundTaskQuotaFunc
	oldPersistQuota := persistImmediateTaskQuotaFunc
	t.Cleanup(func() {
		relayTaskSubmitFunc = oldSubmit
		settleBillingFunc = oldSettle
		logTaskConsumptionFunc = oldLog
		refundTaskQuotaFunc = oldRefund
		persistImmediateTaskQuotaFunc = oldPersistQuota
	})
	relayTaskSubmitFunc = func(c *gin.Context, info *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		info.InitChannelMeta(c)
		info.Billing = billing
		info.FinalPreConsumedQuota = billing.preConsumed
		info.BillingSource = service.BillingSourceWallet
		info.Action = "generate"
		info.PriceData.Quota = 100
		if info.TaskRelayInfo == nil {
			info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
		}
		info.TaskRelayInfo.PublicTaskID = "task-immediate-quota-persist-fail"
		return &relay.TaskSubmitResult{
			UpstreamTaskID: "upstream-immediate-quota-persist-fail",
			TaskData:       []byte(`{"id":"upstream-task"}`),
			Platform:       constant.TaskPlatformSuno,
			Quota:          100,
			Immediate: &relaycommon.TaskInfo{
				Status:   string(model.TaskStatusSuccess),
				Progress: "100%",
			},
			CompletionBillingAdaptor: completion,
		}, nil
	}
	persistImmediateTaskQuotaFunc = func(*model.Task) error {
		return errors.New("quota persist failed")
	}
	settleCalls := 0
	settledQuota := 0
	settleBillingFunc = func(_ *gin.Context, _ *relaycommon.RelayInfo, quota int) error {
		settleCalls++
		settledQuota = quota
		return nil
	}
	logTaskConsumptionFunc = func(*gin.Context, *relaycommon.RelayInfo) error { return nil }
	refundTaskQuotaFunc = func(context.Context, *model.Task, string) error {
		t.Fatal("quota persist failure must not refund a successful task")
		return nil
	}
	ctx, recorder := newRelayTaskTestContext(327, 327, 327)

	RelayTask(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, completion.calls)
	require.Equal(t, 1, settleCalls)
	require.Equal(t, 100, settledQuota)
	var task model.Task
	require.NoError(t, db.First(&task, "task_id = ?", "task-immediate-quota-persist-fail").Error)
	require.Equal(t, 100, task.Quota)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	require.Equal(t, model.TaskSettlementStatusReview, task.SettlementStatus)
	require.Equal(t, 150, task.PrivateData.SettlementAttemptQuota)
	require.Greater(t, task.NextPollAt, time.Now().Unix())

	pendingSettlements, err := model.GetPendingTaskSettlementsAfter(0, 100)
	require.NoError(t, err)
	for _, item := range pendingSettlements {
		require.NotEqual(t, "task-immediate-quota-persist-fail", item.TaskID)
	}
}

func TestRelayTaskSettleFailureWhenReviewPersistFailsKeepsRefundPending(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Log{}))
	insertRelayTaskTestChannel(t, 328)

	billing := &relayTaskTestBilling{
		preConsumed: 100,
		refundErr:   errors.New("wallet credit failed"),
	}
	oldPersistReview := persistTaskSubmitSettlementErrorFunc
	installRelayTaskTestHooks(t, billing, "task-controller-settle-review-persist-fail", 150)
	t.Cleanup(func() {
		persistTaskSubmitSettlementErrorFunc = oldPersistReview
	})
	settleBillingFunc = func(c *gin.Context, info *relaycommon.RelayInfo, actualQuota int) error {
		require.Equal(t, 150, actualQuota)
		return errors.New("settlement failed")
	}
	logCalled := false
	logTaskConsumptionFunc = func(c *gin.Context, info *relaycommon.RelayInfo) error {
		logCalled = true
		return nil
	}
	persistTaskSubmitSettlementErrorFunc = func(*model.Task, *relaycommon.RelayInfo, int, error) error {
		return errors.New("review persist failed")
	}
	ctx, recorder := newRelayTaskTestContext(328, 328, 328)

	RelayTask(ctx)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.False(t, logCalled)
	var task model.Task
	require.NoError(t, db.First(&task, "task_id = ?", "task-controller-settle-review-persist-fail").Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
	require.True(t, task.RefundPending)
	require.Equal(t, 100, task.Quota)
	require.NotEqual(t, model.TaskSettlementStatusReview, task.SettlementStatus)

	pendingSettlements, err := model.GetPendingTaskSettlementsAfter(0, 100)
	require.NoError(t, err)
	for _, item := range pendingSettlements {
		require.NotEqual(t, "task-controller-settle-review-persist-fail", item.TaskID)
	}
	pendingRefunds, err := model.GetPendingTaskRefundsAfter(0, 100)
	require.NoError(t, err)
	found := false
	for _, item := range pendingRefunds {
		if item.TaskID == "task-controller-settle-review-persist-fail" {
			found = true
			require.True(t, item.RefundPending)
			require.Equal(t, 100, item.Quota)
			break
		}
	}
	require.True(t, found, "settle-fail review persist fail must remain refundable at prepaid quota")
}

func TestRelayTaskLogFailureWhenBothRefundablePersistsFailStillMarksRefundPending(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	insertRelayTaskTestChannel(t, 329)

	billing := &relayTaskTestBilling{
		preConsumed: 100,
		rollbackErr: errors.New("wallet credit failed"),
	}
	oldPersist := persistRefundableSubmitAccountingFailureFunc
	oldUpdate := updateTaskAfterSubmitAccountingFailureFunc
	installRelayTaskTestHooks(t, billing, "task-controller-log-both-persist-fail", 150)
	t.Cleanup(func() {
		persistRefundableSubmitAccountingFailureFunc = oldPersist
		updateTaskAfterSubmitAccountingFailureFunc = oldUpdate
	})
	relayTaskSubmitFunc = func(c *gin.Context, info *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		info.InitChannelMeta(c)
		info.Billing = billing
		info.FinalPreConsumedQuota = billing.preConsumed
		info.BillingSource = service.BillingSourceWallet
		info.Action = "generate"
		info.PriceData.Quota = 150
		if info.TaskRelayInfo == nil {
			info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
		}
		info.TaskRelayInfo.PublicTaskID = "task-controller-log-both-persist-fail"
		return &relay.TaskSubmitResult{
			UpstreamTaskID: "upstream-log-both-persist-fail",
			TaskData:       []byte(`{"id":"upstream-task"}`),
			Platform:       constant.TaskPlatformSuno,
			Quota:          150,
			Immediate: &relaycommon.TaskInfo{
				Status:    string(model.TaskStatusSuccess),
				Progress:  "100%",
				RemoteUrl: "https://cdn.example/both-persist-fail.mp4",
			},
		}, nil
	}
	settleBillingFunc = func(c *gin.Context, info *relaycommon.RelayInfo, actualQuota int) error {
		require.NotNil(t, info.Billing)
		return info.Billing.Settle(actualQuota)
	}
	logTaskConsumptionFunc = func(*gin.Context, *relaycommon.RelayInfo) error {
		return errors.New("record consume log failed")
	}
	persistRefundableSubmitAccountingFailureFunc = func(*model.Task, int, error, error) error {
		return errors.New("persist refundable accounting failure")
	}
	updateTaskAfterSubmitAccountingFailureFunc = func(*model.Task) error {
		return errors.New("private_data persist failed")
	}
	ctx, recorder := newRelayTaskTestContext(329, 329, 329)

	RelayTask(ctx)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Contains(t, recorder.Body.String(), "log_task_consumption_failed")
	var task model.Task
	require.NoError(t, db.First(&task, "task_id = ?", "task-controller-log-both-persist-fail").Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
	require.True(t, task.RefundPending)
	require.Equal(t, 150, task.Quota)
	require.NotEqual(t, model.TaskSettlementStatusReview, task.SettlementStatus)

	pendingSettlements, err := model.GetPendingTaskSettlementsAfter(0, 100)
	require.NoError(t, err)
	for _, item := range pendingSettlements {
		require.NotEqual(t, "task-controller-log-both-persist-fail", item.TaskID)
	}
	pendingRefunds, err := model.GetPendingTaskRefundsAfter(0, 100)
	require.NoError(t, err)
	found := false
	for _, item := range pendingRefunds {
		if item.TaskID == "task-controller-log-both-persist-fail" {
			found = true
			require.True(t, item.RefundPending)
			require.Equal(t, 150, item.Quota)
			break
		}
	}
	require.True(t, found, "both persistRefundable attempts failing must still leave a refundable row")
	require.True(t, task.PrivateData.PreConsumedUsageCaptured, "Force last-resort persist must keep usage-captured so the sweeper does not decrement used_quota again")
	require.False(t, task.PrivateData.PreConsumedUsageRecorded)
}

func TestRelayTaskLogFailureWhenUsageRollbackFailsKeepsRefundPendingForUsage(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Log{}, &model.Token{}, &model.TaskSettlementRecord{}, &model.QuotaData{}, &model.TokenUsageDaily{}))
	insertRelayTaskTestChannel(t, 330)

	const userID, tokenID, channelID = 330, 330, 330
	const initQuota, preConsumed = 10000, 150
	require.NoError(t, db.Create(&model.User{
		Id:           userID,
		Username:     "relay-task-log-usage-rollback-fail",
		Quota:        initQuota,
		UsedQuota:    0,
		RequestCount: 0,
		Status:       common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Token{
		Id:          tokenID,
		UserId:      userID,
		Key:         "sk-relay-task-log-usage-rollback-fail",
		Name:        "relay-task-log-usage-rollback-fail",
		Status:      common.TokenStatusEnabled,
		RemainQuota: initQuota,
	}).Error)

	oldSubmit := relayTaskSubmitFunc
	oldSettle := settleBillingFunc
	oldLog := logTaskConsumptionFunc
	t.Cleanup(func() {
		relayTaskSubmitFunc = oldSubmit
		settleBillingFunc = oldSettle
		logTaskConsumptionFunc = oldLog
	})
	relayTaskSubmitFunc = func(c *gin.Context, info *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		info.InitChannelMeta(c)
		info.UserId = userID
		info.TokenId = tokenID
		info.TokenKey = "sk-relay-task-log-usage-rollback-fail"
		info.ForcePreConsume = true
		info.UserSetting.BillingPreference = "wallet_only"
		info.BillingSource = service.BillingSourceWallet
		info.Action = "generate"
		info.PriceData.Quota = preConsumed
		session, apiErr := service.NewBillingSession(c, info, preConsumed)
		require.Nil(t, apiErr)
		require.NotNil(t, session)
		info.Billing = session
		info.FinalPreConsumedQuota = preConsumed
		if info.TaskRelayInfo == nil {
			info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
		}
		info.TaskRelayInfo.PublicTaskID = "task-controller-log-usage-rollback-fail"
		return &relay.TaskSubmitResult{
			UpstreamTaskID: "upstream-log-usage-rollback-fail",
			TaskData:       []byte(`{"id":"upstream-task"}`),
			Platform:       constant.TaskPlatformSuno,
			Quota:          preConsumed,
		}, nil
	}
	settleBillingFunc = service.SettleBilling
	logTaskConsumptionFunc = func(c *gin.Context, info *relaycommon.RelayInfo) error {
		require.NoError(t, model.UpdateTaskConsumptionUsageWithTokenSync(userID, channelID, tokenID, preConsumed))
		c.Set(service.ContextKeyUsageCountersRecorded(), true)
		return errors.New("record consume log failed")
	}
	ctx, recorder := newRelayTaskTestContext(userID, tokenID, channelID)

	RelayTask(ctx)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	var task model.Task
	require.NoError(t, db.First(&task, "task_id = ?", "task-controller-log-usage-rollback-fail").Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
	require.Zero(t, task.Quota)
	require.True(t, task.PrivateData.PreConsumedUsageCaptured)
	require.True(t, task.PrivateData.PreConsumedUsageRecorded)
	require.True(t, task.RefundPending)

	var user model.User
	require.NoError(t, db.Select("used_quota", "request_count", "quota").First(&user, userID).Error)
	require.EqualValues(t, preConsumed, user.UsedQuota)
	require.Equal(t, 1, user.RequestCount)
	require.EqualValues(t, initQuota, user.Quota)

	pendingRefunds, err := model.GetPendingTaskRefundsAfter(0, 100)
	require.NoError(t, err)
	found := false
	for _, item := range pendingRefunds {
		if item.TaskID == "task-controller-log-usage-rollback-fail" {
			found = true
			break
		}
	}
	require.True(t, found, "usage-rollback-fail after wallet restore must remain visible to the refund sweeper")

	require.NoError(t, service.RefundTaskQuota(context.Background(), &task, task.FailReason))
	require.NoError(t, db.First(&user, userID).Error)
	require.EqualValues(t, 0, user.UsedQuota)
	require.Equal(t, 0, user.RequestCount)
	require.EqualValues(t, initQuota, user.Quota)
	require.NoError(t, db.First(&task, task.ID).Error)
	require.False(t, task.RefundPending)
}
