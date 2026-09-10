package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

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
	refunded      bool
	refundCalls   int
	rollbackCalls int
	refundApplied int
}

type relayTaskCompletionBillingAdaptor struct {
	calls int
}

func (b *relayTaskTestBilling) Settle(actualQuota int) error {
	return nil
}

func (b *relayTaskTestBilling) Refund(c *gin.Context) error {
	b.refundCalls++
	if b.refunded {
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
	if b.refunded {
		return nil
	}
	b.refunded = true
	b.refundApplied++
	return nil
}

func (a *relayTaskCompletionBillingAdaptor) AdjustBillingOnComplete(*model.Task, *relaycommon.TaskInfo) int {
	a.calls++
	return 0
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
	require.Equal(t, "https://cdn.example/immediate.mp4", task.PrivateData.ResultURL)
	require.NotZero(t, task.FinishTime)
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
	require.Equal(t, "settlement failed", reloaded.PrivateData.SettlementError)
	require.True(t, strings.Contains(reloaded.FailReason, "settlement failed"))
	require.True(t, strings.Contains(reloaded.FailReason, "review update failed"))
	require.NotZero(t, reloaded.FinishTime)
	require.GreaterOrEqual(t, reloaded.FinishTime, common.GetTimestamp()-5)
}

func TestFailPersistedTaskAfterSubmitAccountingErrorMarksReviewWithoutEndingTask(t *testing.T) {
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
	require.Equal(t, model.TaskStatusNotStart, reloaded.Status)
	require.Equal(t, "0%", reloaded.Progress)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.Equal(t, 150, reloaded.PrivateData.SettlementAttemptQuota)
	require.Equal(t, "record consume log failed", reloaded.PrivateData.SettlementError)
	require.True(t, strings.Contains(reloaded.FailReason, "billing accounting failed after task submission"))
	require.True(t, strings.Contains(reloaded.FailReason, "record consume log failed"))
	require.Zero(t, reloaded.FinishTime)
}

func TestFailPersistedTaskAfterSubmitAccountingErrorKeepsSubmittedTaskPollable(t *testing.T) {
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
	require.Equal(t, model.TaskStatusNotStart, reloaded.Status)
	require.Equal(t, "0%", reloaded.Progress)
	require.Zero(t, reloaded.FinishTime)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.Equal(t, 150, reloaded.PrivateData.SettlementAttemptQuota)
	require.Contains(t, reloaded.PrivateData.SettlementError, "record consume log failed")
	require.Contains(t, reloaded.FailReason, "billing accounting failed after task submission")
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
		require.Contains(t, c.GetString(service.ContextKeySettlementError()), "settlement failed")
		return nil
	}
	ctx, _ := newRelayTaskTestContext(301, 301, 301)

	RelayTask(ctx)

	require.True(t, logCalled)
	require.Zero(t, billing.refundApplied)
	require.Zero(t, billing.refundCalls)
	var task model.Task
	require.NoError(t, db.First(&task, "task_id = ?", "task-controller-settle-review").Error)
	require.Equal(t, 100, task.Quota)
	require.Equal(t, model.TaskSettlementStatusReview, task.SettlementStatus)
	require.Equal(t, 150, task.PrivateData.SettlementAttemptQuota)
	require.Equal(t, "settlement failed", task.PrivateData.SettlementError)
	require.Equal(t, service.TaskSettlementReviewFailReason, task.FailReason)
}

func TestRelayTaskLogFailureAfterSubmitKeepsReviewTaskAndRefundsOnlyOnce(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Log{}))
	insertRelayTaskTestChannel(t, 302)

	billing := &relayTaskTestBilling{preConsumed: 100}
	installRelayTaskTestHooks(t, billing, "task-controller-log-review", 150)
	settleBillingFunc = func(c *gin.Context, info *relaycommon.RelayInfo, actualQuota int) error {
		require.Equal(t, 150, actualQuota)
		return nil
	}
	logTaskConsumptionFunc = func(c *gin.Context, info *relaycommon.RelayInfo) error {
		require.NoError(t, service.RollbackBillingSettlement(c, info, 150))
		return errors.New("record consume log failed")
	}
	ctx, recorder := newRelayTaskTestContext(302, 302, 302)

	RelayTask(ctx)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Equal(t, 1, billing.rollbackCalls)
	require.Equal(t, 1, billing.refundCalls)
	require.Equal(t, 1, billing.refundApplied)
	var task model.Task
	require.NoError(t, db.First(&task, "task_id = ?", "task-controller-log-review").Error)
	require.Equal(t, 0, task.Quota)
	require.Equal(t, model.TaskStatusNotStart, task.Status)
	require.Equal(t, "0%", task.Progress)
	require.Zero(t, task.FinishTime)
	require.Equal(t, model.TaskSettlementStatusReview, task.SettlementStatus)
	require.Equal(t, 150, task.PrivateData.SettlementAttemptQuota)
	require.Equal(t, "record consume log failed", task.PrivateData.SettlementError)
	require.Contains(t, task.FailReason, "billing accounting failed after task submission")
	require.Contains(t, task.FailReason, "record consume log failed")
}

func TestTaskRelayAccountingFailuresPersistAuditRecords(t *testing.T) {
	body, err := os.ReadFile("relay.go")
	require.NoError(t, err)
	source := string(body)

	for _, want := range []string{
		`RecordConsumeAccountingError(c, relayInfo, "refund billing after relay error"`,
		`RecordConsumeAccountingError(c, relayInfo, "refund billing after task error"`,
		`RecordConsumeAccountingError(c, relayInfo, "settle task billing"`,
		`RecordConsumeAccountingError(c, relayInfo, "persist task settlement review"`,
		`RecordConsumeAccountingError(c, relayInfo, "log task consumption"`,
		`RecordConsumeAccountingError(c, relayInfo, "persist task accounting review"`,
		`failPersistedTaskAfterSubmitAccountingError(task, relayInfo, settlementQuota, err)`,
	} {
		require.Contains(t, source, want)
	}
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
