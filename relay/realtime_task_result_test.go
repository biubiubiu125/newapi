package relay

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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

func TestApplyRealtimeTaskResultFailsWhenSuccessHasNoResultURL(t *testing.T) {
	task := &model.Task{
		TaskID: "task_reason",
		Status: model.TaskStatusInProgress,
	}
	result := &relaycommon.TaskInfo{
		Status: "SUCCESS",
		Reason: "provider finished with a diagnostic message",
	}

	require.True(t, applyRealtimeTaskResult(task, result, nil, 123))

	assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
	assert.Equal(t, "provider finished with a diagnostic message", task.FailReason)
	assert.Empty(t, task.PrivateData.ResultURL)
	assert.Empty(t, task.SettlementStatus)
}

func TestApplyRealtimeTaskResultFailsWhenSuccessHasEmptyResultURL(t *testing.T) {
	task := &model.Task{
		TaskID: "task_empty_url",
		Status: model.TaskStatusInProgress,
	}
	result := &relaycommon.TaskInfo{
		Status: string(model.TaskStatusSuccess),
	}

	require.True(t, applyRealtimeTaskResult(task, result, nil, 123))

	assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
	assert.Equal(t, taskcommon.MissingResultURLReason, task.FailReason)
	assert.Empty(t, task.PrivateData.ResultURL)
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

	require.True(t, applyRealtimeTaskResult(task, result, []byte(`{"video":"data:video/mp4;base64,AAAA"}`), 456))

	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	assert.Equal(t, taskcommon.ProgressComplete, task.Progress)
	assert.Equal(t, "data:video/mp4;base64,AAAA", task.PrivateData.ResultURL)
	assert.Equal(t, taskcommon.BuildProxyURL(task.TaskID), task.GetResultURL())
	assert.Equal(t, int64(456), task.FinishTime)
	assert.NotContains(t, string(task.Data), "data:video/mp4;base64,AAAA")
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

	assert.Equal(t, "DATA:video/mp4;BASE64,AAAA", task.PrivateData.ResultURL)
	assert.Equal(t, taskcommon.BuildProxyURL(task.TaskID), task.GetResultURL())
}

func TestApplyRealtimeTaskResultAllowsEmptyProxyForSoraChannelType(t *testing.T) {
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://api.example.com"
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	task := &model.Task{
		TaskID:   "task_realtime_sora",
		Status:   model.TaskStatusInProgress,
		Platform: constant.TaskPlatform("custom-sora-plugin"),
	}
	result := &relaycommon.TaskInfo{
		Status: string(model.TaskStatusSuccess),
	}

	require.True(t, applyRealtimeTaskResult(task, result, nil, 123, constant.ChannelTypeSora))

	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	assert.Equal(t, taskcommon.BuildProxyURL(task.TaskID), task.PrivateData.ResultURL)
	assert.Equal(t, int64(123), task.FinishTime)
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

func TestRealtimeTaskPublicBodyHidesRetryableSettlementReview(t *testing.T) {
	task := &model.Task{
		TaskID:           "task_rt_review",
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusReview,
		Progress:         "100%",
		FinishTime:       123,
		NextPollAt:       456,
		PrivateData: model.TaskPrivateData{
			ResultURL: "https://cdn.example/video.mp4",
		},
	}

	body := realtimeTaskPublicBody(task, "mp4")
	require.NotEmpty(t, body)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(body, &parsed))
	require.Equal(t, "success", parsed["code"])
	data, ok := parsed["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "processing", data["status"])
	assert.Equal(t, "task_rt_review", data["task_id"])
	assert.Equal(t, "mp4", data["format"])
	assert.Empty(t, data["url"])
}

func TestRestoreRealtimeTaskAfterLostUpdateReloadsPersistedWinner(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = oldDB })

	task := &model.Task{
		TaskID:   "task_rt_cas",
		Status:   model.TaskStatusInProgress,
		Progress: "50%",
		Data:     []byte(`{"local":"stale"}`),
		PrivateData: model.TaskPrivateData{
			ResultURL: "",
		},
	}
	require.NoError(t, db.Create(task).Error)
	original := task.CloneForUpdate()

	winner, exists, err := model.GetTaskByID(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	winner.Status = model.TaskStatusSuccess
	winner.Progress = "100%"
	winner.PrivateData.ResultURL = "https://cdn.example/winner.mp4"
	winner.Data = []byte(`{"winner":true}`)
	won, err := winner.UpdateWithStatus(model.TaskStatusInProgress)
	require.NoError(t, err)
	require.True(t, won)

	task.Status = model.TaskStatusSuccess
	task.Progress = "100%"
	task.PrivateData.ResultURL = "https://cdn.example/local.mp4"
	task.Data = []byte(`{"local":"applied"}`)
	restoreRealtimeTaskAfterLostUpdate(task, original)

	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	require.Equal(t, "100%", task.Progress)
	require.Equal(t, "https://cdn.example/winner.mp4", task.PrivateData.ResultURL)
	require.JSONEq(t, `{"winner":true}`, string(task.Data))
}
