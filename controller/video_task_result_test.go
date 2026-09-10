package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
)

func TestApplyImmediateTaskResultKeepsRemoteURLAndTerminalFields(t *testing.T) {
	task := &model.Task{
		TaskID: "task_immediate",
		Status: model.TaskStatusInProgress,
	}
	result := &relaycommon.TaskInfo{
		Status:    string(model.TaskStatusSuccess),
		Progress:  "40%",
		RemoteUrl: "https://cdn.example/immediate.mp4",
	}

	applyImmediateTaskResult(task, result, 789)

	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	assert.Equal(t, taskcommon.ProgressComplete, task.Progress)
	assert.Equal(t, "https://cdn.example/immediate.mp4", task.PrivateData.ResultURL)
	assert.Equal(t, int64(789), task.FinishTime)
}

func TestApplyImmediateTaskResultPreservesSuccessReason(t *testing.T) {
	task := &model.Task{
		TaskID: "task_immediate_reason",
		Status: model.TaskStatusInProgress,
	}
	result := &relaycommon.TaskInfo{
		Status: string(model.TaskStatusSuccess),
		Reason: "provider finished with notes",
	}

	applyImmediateTaskResult(task, result, 123)

	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	assert.Equal(t, "provider finished with notes", task.FailReason)
	assert.Equal(t, taskcommon.BuildProxyURL(task.TaskID), task.PrivateData.ResultURL)
	assert.Equal(t, int64(123), task.FinishTime)
}
