package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyRealtimeTaskResultCopiesLegacyTerminalFields(t *testing.T) {
	task := &model.Task{
		TaskID:   "task_legacy",
		Status:   model.TaskStatusInProgress,
		Progress: "50%",
	}
	result := &relaycommon.TaskInfo{
		Status:    string(model.TaskStatusSuccess),
		Progress:  "25%",
		RemoteUrl: "https://cdn.example/video.mp4",
	}

	require.True(t, applyRealtimeTaskResult(task, result, []byte(`{"done":true}`), 123))

	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	assert.Equal(t, taskcommon.ProgressComplete, task.Progress)
	assert.Equal(t, "https://cdn.example/video.mp4", task.PrivateData.ResultURL)
	assert.Equal(t, int64(123), task.FinishTime)
	assert.JSONEq(t, `{"done":true}`, string(task.Data))
}

func TestApplyRealtimeTaskResultPreservesSuccessReason(t *testing.T) {
	task := &model.Task{
		TaskID: "task_reason",
		Status: model.TaskStatusInProgress,
	}
	result := &relaycommon.TaskInfo{
		Status: "SUCCESS",
		Reason: "provider finished with a diagnostic message",
	}

	require.True(t, applyRealtimeTaskResult(task, result, nil, 123))

	assert.Equal(t, "provider finished with a diagnostic message", task.FailReason)
	assert.Equal(t, taskcommon.BuildProxyURL(task.TaskID), task.PrivateData.ResultURL)
}

func TestApplyRealtimeTaskResultCopiesFailureReasonWithoutResultURL(t *testing.T) {
	task := &model.Task{
		TaskID:   "task_failure",
		Status:   model.TaskStatusInProgress,
		Progress: "50%",
	}
	result := &relaycommon.TaskInfo{
		Status:   string(model.TaskStatusFailure),
		Reason:   "provider rejected the request",
		Progress: "25%",
	}

	require.True(t, applyRealtimeTaskResult(task, result, []byte(`{"done":false}`), 123))

	assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
	assert.Equal(t, taskcommon.ProgressComplete, task.Progress)
	assert.Equal(t, "provider rejected the request", task.FailReason)
	assert.Empty(t, task.PrivateData.ResultURL)
	assert.Equal(t, int64(123), task.FinishTime)
}

func TestApplyRealtimeTaskResultKeepsDataURIBehindTaskProxy(t *testing.T) {
	task := &model.Task{
		TaskID: "task_data_uri",
		Status: model.TaskStatusInProgress,
	}
	result := &relaycommon.TaskInfo{
		Status: model.TaskStatusSuccess,
		Url:    "data:video/mp4;base64,AAAA",
	}

	require.True(t, applyRealtimeTaskResult(task, result, []byte(`{"video":"inline"}`), 456))

	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	assert.Equal(t, taskcommon.ProgressComplete, task.Progress)
	assert.Equal(t, taskcommon.BuildProxyURL(task.TaskID), task.PrivateData.ResultURL)
	assert.Equal(t, int64(456), task.FinishTime)
}

func TestApplyRealtimeTaskResultKeepsCaseInsensitiveDataURIBehindTaskProxy(t *testing.T) {
	task := &model.Task{
		TaskID: "task_case_insensitive_data_uri",
		Status: model.TaskStatusInProgress,
	}
	result := &relaycommon.TaskInfo{
		Status: model.TaskStatusSuccess,
		Url:    "DATA:video/mp4;BASE64,AAAA",
	}

	require.True(t, applyRealtimeTaskResult(task, result, nil, 457))

	assert.Equal(t, taskcommon.BuildProxyURL(task.TaskID), task.PrivateData.ResultURL)
}

func TestApplyRealtimeTaskResultDoesNotReopenTerminalTask(t *testing.T) {
	task := &model.Task{
		TaskID:   "task_terminal",
		Status:   model.TaskStatusSuccess,
		Progress: taskcommon.ProgressComplete,
		PrivateData: model.TaskPrivateData{
			ResultURL: "https://cdn.example/already-final.mp4",
		},
		FinishTime: 321,
	}
	result := &relaycommon.TaskInfo{
		Status:   string(model.TaskStatusInProgress),
		Progress: "50%",
		Reason:   "stale provider response",
	}

	assert.False(t, applyRealtimeTaskResult(task, result, []byte(`{"done":false}`), 999))
	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	assert.Equal(t, taskcommon.ProgressComplete, task.Progress)
	assert.Equal(t, "https://cdn.example/already-final.mp4", task.PrivateData.ResultURL)
	assert.Empty(t, task.FailReason)
	assert.Equal(t, int64(321), task.FinishTime)
}
