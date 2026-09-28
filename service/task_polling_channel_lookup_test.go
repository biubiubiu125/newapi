package service

import (
	"context"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestUpdateVideoTasksTransientChannelLookupDoesNotFailOrRefund(t *testing.T) {
	truncate(t)

	const userID, channelID = 13601, 13602
	const preConsumed = 900
	seedUser(t, userID, 4000)
	task := makeTask(userID, channelID, preConsumed, 0, BillingSourceWallet, 0)
	task.TaskID = "video-channel-blip"
	task.Platform = constant.TaskPlatform("kling")
	task.Status = model.TaskStatusInProgress
	task.PrivateData.UpstreamTaskID = "video-channel-blip-upstream"
	require.NoError(t, model.DB.Create(task).Error)

	previous := lookupPollingChannel
	lookupPollingChannel = func(int) (*model.Channel, error) {
		return nil, errors.New("SSL connection has been closed unexpectedly")
	}
	t.Cleanup(func() { lookupPollingChannel = previous })

	err := updateVideoTasks(context.Background(), constant.TaskPlatform("kling"), channelID, []string{task.TaskID}, map[string]*model.Task{
		task.TaskID: task,
	})
	require.Error(t, err)

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), reloaded.Status)
	require.Equal(t, preConsumed, reloaded.Quota)
	require.Empty(t, reloaded.FailReason)
	require.False(t, reloaded.RefundPending)
	require.EqualValues(t, 4000, getUserQuota(t, userID))
}

func TestUpdateVideoTasksMissingChannelStillFailsAndRefunds(t *testing.T) {
	truncate(t)

	const userID, channelID = 13611, 13612
	const preConsumed = 900
	seedUser(t, userID, 4000)
	task := makeTask(userID, channelID, preConsumed, 0, BillingSourceWallet, 0)
	task.TaskID = "video-channel-gone"
	task.Platform = constant.TaskPlatform("kling")
	task.Status = model.TaskStatusInProgress
	task.PrivateData.UpstreamTaskID = "video-channel-gone-upstream"
	require.NoError(t, model.DB.Create(task).Error)

	previous := lookupPollingChannel
	lookupPollingChannel = func(int) (*model.Channel, error) {
		return nil, errors.New("channel #13612 no longer exists")
	}
	t.Cleanup(func() { lookupPollingChannel = previous })

	err := updateVideoTasks(context.Background(), constant.TaskPlatform("kling"), channelID, []string{task.TaskID}, map[string]*model.Task{
		task.TaskID: task,
	})
	require.Error(t, err)

	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status)
	require.Zero(t, reloaded.Quota)
	require.EqualValues(t, 4900, getUserQuota(t, userID))
}

func TestUpdateBatchAndSunoTasksTransientChannelLookupDoesNotRefund(t *testing.T) {
	truncate(t)

	const userID, channelID = 13621, 13622
	const preConsumed = 400
	seedUser(t, userID, 2000)
	batchTask := makeTask(userID, channelID, preConsumed, 0, BillingSourceWallet, 0)
	batchTask.TaskID = "batch-channel-blip"
	batchTask.Status = model.TaskStatusSubmitted
	require.NoError(t, model.DB.Create(batchTask).Error)
	sunoTask := makeTask(userID, channelID, preConsumed, 0, BillingSourceWallet, 0)
	sunoTask.TaskID = "suno-channel-blip"
	sunoTask.Platform = constant.TaskPlatformSuno
	sunoTask.Status = model.TaskStatusSubmitted
	sunoTask.PrivateData.UpstreamTaskID = "suno-channel-blip-upstream"
	require.NoError(t, model.DB.Create(sunoTask).Error)

	previous := lookupPollingChannel
	lookupPollingChannel = func(int) (*model.Channel, error) {
		return nil, errors.New("remaining connection slots are reserved")
	}
	t.Cleanup(func() { lookupPollingChannel = previous })

	require.Error(t, updateBatchTasks(context.Background(), nil, channelID, []string{batchTask.TaskID}, map[string]*model.Task{
		batchTask.TaskID: batchTask,
	}))
	require.Error(t, updateSunoTasks(context.Background(), channelID, []string{sunoTask.TaskID}, map[string]*model.Task{
		sunoTask.TaskID: sunoTask,
	}))

	var reloadedBatch model.Task
	require.NoError(t, model.DB.First(&reloadedBatch, batchTask.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSubmitted), reloadedBatch.Status)
	require.Equal(t, preConsumed, reloadedBatch.Quota)
	var reloadedSuno model.Task
	require.NoError(t, model.DB.First(&reloadedSuno, sunoTask.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSubmitted), reloadedSuno.Status)
	require.Equal(t, preConsumed, reloadedSuno.Quota)
	require.EqualValues(t, 2000, getUserQuota(t, userID))
}
