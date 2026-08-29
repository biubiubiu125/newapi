package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// RegisterScheduledSystemTasks wires the periodic channel test, upstream model
// update, and async task polling (Midjourney / Suno / video) jobs into the
// system task framework so a DB lease dedups execution across multiple master
// instances and each run is recorded as one task row. Call this before
// service.StartSystemTaskRunner.
func RegisterScheduledSystemTasks() {
	service.RegisterSystemTaskHandler(channelTestHandler{})
	service.RegisterSystemTaskHandler(modelUpdateHandler{})
	service.RegisterSystemTaskHandler(manualModelUpdateHandler{})
	service.RegisterSystemTaskHandler(modelUpdateApplyAllHandler{})
	service.RegisterSystemTaskHandler(midjourneyPollHandler{})
	service.RegisterSystemTaskHandler(asyncTaskPollHandler{})
}

// channelTestHandler runs the scheduled "test all channels" job. Enablement and
// cadence still come from the monitor settings; only the execution path moved
// into the system task runner.
type channelTestHandler struct{}

func (channelTestHandler) Type() string { return model.SystemTaskTypeChannelTest }

func (channelTestHandler) Enabled() bool {
	return operation_setting.GetMonitorSetting().AutoTestChannelEnabled
}

func (channelTestHandler) Interval() time.Duration {
	minutes := operation_setting.GetMonitorSetting().AutoTestChannelMinutes
	if minutes <= 0 {
		minutes = 10
	}
	return time.Duration(minutes * float64(time.Minute))
}

func (channelTestHandler) NewPayload() any { return nil }

// channelTestTaskPayload controls one channel_test run. A nil/empty payload is a
// scheduled run, which uses the configured monitor ChannelTestMode and does not
// notify. A manual "test all channels" trigger sets Mode=scheduled_all and
// Notify=true to reproduce the legacy manual behavior (test every channel and
// notify root on completion).
type channelTestTaskPayload struct {
	Mode   string `json:"mode,omitempty"`
	Notify bool   `json:"notify,omitempty"`
}

func (channelTestHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := channelTestTaskPayload{}
	if err := task.DecodePayload(&payload); err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	summary, err := runChannelTestTask(ctx, payload.Mode, payload.Notify, service.NewSystemTaskProgressReporter(task, runnerID))
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

// modelUpdateHandler runs the scheduled upstream model update detection job.
type modelUpdateHandler struct{}

func (modelUpdateHandler) Type() string { return model.SystemTaskTypeModelUpdate }

func (modelUpdateHandler) LockType() string { return model.SystemTaskTypeModelUpdate }

func (modelUpdateHandler) Enabled() bool {
	return common.GetEnvOrDefaultBool("CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_ENABLED", true)
}

func (modelUpdateHandler) Interval() time.Duration {
	intervalMinutes := common.GetEnvOrDefault(
		"CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_INTERVAL_MINUTES",
		channelUpstreamModelUpdateTaskDefaultIntervalMinutes,
	)
	if intervalMinutes < 1 {
		intervalMinutes = channelUpstreamModelUpdateTaskDefaultIntervalMinutes
	}
	return time.Duration(intervalMinutes) * time.Minute
}

func (modelUpdateHandler) NewPayload() any { return nil }

// modelUpdateTaskPayload controls one model_update run. A scheduled run
// (Manual=false) respects the per-channel minimum check interval and may
// auto-apply detected models when a channel has auto-sync enabled. A manual
// "detect all" trigger sets Manual=true to reproduce the legacy detect-all
// semantics: force a re-check regardless of the interval and never auto-apply,
// so the admin reviews and applies changes explicitly.
type modelUpdateTaskPayload struct {
	Manual bool `json:"manual,omitempty"`
}

func (modelUpdateHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := modelUpdateTaskPayload{}
	if err := task.DecodePayload(&payload); err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	allowCodexCredentialRefresh := !payload.Manual
	summary, err := runChannelUpstreamModelUpdateTaskOnceWithTask(
		ctx,
		task.TaskID,
		runnerID,
		payload.Manual,
		!payload.Manual,
		allowCodexCredentialRefresh,
		service.NewSystemTaskProgressReporter(task, runnerID),
	)
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, summary, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

type manualModelUpdateHandler struct{}

func (manualModelUpdateHandler) Type() string { return model.SystemTaskTypeModelUpdateManual }

func (manualModelUpdateHandler) LockType() string { return model.SystemTaskTypeModelUpdate }

func (manualModelUpdateHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	summary, err := runChannelUpstreamModelUpdateTaskOnceWithTask(
		ctx,
		task.TaskID,
		runnerID,
		true,
		false,
		false,
		service.NewSystemTaskProgressReporter(task, runnerID),
	)
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, summary, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

type modelUpdateApplyAllHandler struct{}

func (modelUpdateApplyAllHandler) Type() string {
	return model.SystemTaskTypeModelUpdateApplyAll
}

func (modelUpdateApplyAllHandler) LockType() string {
	return model.SystemTaskTypeModelUpdate
}

func (modelUpdateApplyAllHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	summary, err := runApplyAllChannelUpstreamModelUpdates(
		ctx,
		task.TaskID,
		runnerID,
		service.NewSystemTaskProgressReporter(task, runnerID),
	)
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, summary, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

// midjourneyPollHandler runs one Midjourney polling pass per scheduled run.
// Enabled() folds the "are there unfinished tasks?" check into enablement so the
// scheduler creates no row when the system is idle; only when at least one
// Midjourney task is in progress does a row get scheduled.
type midjourneyPollHandler struct{}

func (midjourneyPollHandler) Type() string { return model.SystemTaskTypeMidjourneyPoll }

func (midjourneyPollHandler) Enabled() bool {
	return constant.UpdateTask && model.HasUnfinishedMidjourneyTasks()
}

func (midjourneyPollHandler) Interval() time.Duration { return 15 * time.Second }

func (midjourneyPollHandler) NewPayload() any { return nil }

func (midjourneyPollHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	summary := runMidjourneyTaskUpdateOnce(ctx, service.NewSystemTaskProgressReporter(task, runnerID))
	if ctx != nil && ctx.Err() != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, summary, ctx.Err())
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

// asyncTaskPollHandler runs one async-task (Suno/video) polling pass per
// scheduled run. Like midjourneyPollHandler, Enabled() folds in the unfinished
// task existence check so an idle system schedules no rows.
type asyncTaskPollHandler struct{}

func (asyncTaskPollHandler) Type() string { return model.SystemTaskTypeAsyncTaskPoll }

func (asyncTaskPollHandler) Enabled() bool {
	if service.ImageTaskWorkerEnabled() {
		return constant.UpdateTask && model.HasRunnableNonImageSyncTasks()
	}
	return constant.UpdateTask && model.HasRunnableSyncTasks(common.GetTimestamp())
}

func (asyncTaskPollHandler) Interval() time.Duration { return 15 * time.Second }

func (asyncTaskPollHandler) NewPayload() any { return nil }

func (asyncTaskPollHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	summary := service.RunTaskPollingOnce(ctx, service.NewSystemTaskProgressReporter(task, runnerID))
	if ctx != nil && ctx.Err() != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, summary, ctx.Err())
		service.ScheduleNextImageTaskPollWakeup(context.Background())
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
	service.ScheduleNextImageTaskPollWakeup(context.Background())
}

func finishSystemTaskHandler(task *model.SystemTask, runnerID string, status model.SystemTaskStatus, result any, runErr error) {
	errorMessage := ""
	if runErr != nil {
		errorMessage = runErr.Error()
	}
	var persistErr error
	for attempt := 0; attempt < 3; attempt++ {
		persistErr = model.FinishSystemTask(task.TaskID, runnerID, status, result, errorMessage)
		if persistErr == nil {
			return
		}
		if errors.Is(persistErr, model.ErrSystemTaskLockLost) {
			break
		}
		if attempt < 2 {
			time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
		}
	}
	if persistErr == nil {
		return
	}
	common.SysLog(fmt.Sprintf("system task %s failed to persist result after retries: %v", task.TaskID, persistErr))
	if errors.Is(persistErr, model.ErrSystemTaskLockLost) {
		return
	}

	// Preserve the actual persistence failure instead of allowing the runner's
	// generic deferred fallback to hide it behind "without a terminal state".
	failureMessage := fmt.Sprintf("failed to persist task terminal result: %v", persistErr)
	if errorMessage != "" {
		failureMessage = fmt.Sprintf("%s; handler error: %s", failureMessage, errorMessage)
	}
	if err := model.MarkSystemTaskFailedForRunner(task.TaskID, runnerID, failureMessage); err != nil {
		common.SysLog(fmt.Sprintf("system task %s failed to persist fallback failure state: %v", task.TaskID, err))
	}
}
