package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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
		return nil, &model.ChannelLookupError{
			ChannelID: channelID,
			Err:       fmt.Errorf("channel #%d no longer exists: %w", channelID, gorm.ErrRecordNotFound),
		}
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
	require.False(t, reloaded.RefundPending)
	require.EqualValues(t, 4900, getUserQuota(t, userID))
}

func TestUpdateVideoTasksDeletionPhraseDoesNotRefund(t *testing.T) {
	truncate(t)

	const userID, channelID = 13631, 13632
	const preConsumed = 900
	seedUser(t, userID, 4000)
	task := makeTask(userID, channelID, preConsumed, 0, BillingSourceWallet, 0)
	task.TaskID = "video-channel-phrase"
	task.Platform = constant.TaskPlatform("kling")
	task.Status = model.TaskStatusInProgress
	task.PrivateData.UpstreamTaskID = "video-channel-phrase-upstream"
	require.NoError(t, model.DB.Create(task).Error)

	previous := lookupPollingChannel
	lookupPollingChannel = func(int) (*model.Channel, error) {
		return nil, errors.New("channel #13632 no longer exists: SSL connection has been closed unexpectedly")
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

func TestUpdateSunoTasksNilBaseURLDoesNotPanicOrRefund(t *testing.T) {
	for _, baseURL := range []*string{nil, commonStringPtr("   ")} {
		t.Run(fmt.Sprintf("base=%v", baseURL), func(t *testing.T) {
			truncate(t)

			const userID, channelID = 13641, 13642
			const preConsumed = 400
			seedUser(t, userID, 2000)
			task := makeTask(userID, channelID, preConsumed, 0, BillingSourceWallet, 0)
			task.TaskID = "suno-nil-base-url"
			task.Platform = constant.TaskPlatformSuno
			task.Status = model.TaskStatusSubmitted
			task.PrivateData.UpstreamTaskID = "suno-upstream-nil-base"
			require.NoError(t, model.DB.Create(task).Error)

			previousLookup := lookupPollingChannel
			lookupPollingChannel = func(int) (*model.Channel, error) {
				return &model.Channel{
					Id:      channelID,
					Type:    constant.ChannelTypeSunoAPI,
					Key:     "suno-secret",
					Status:  1,
					BaseURL: baseURL,
				}, nil
			}
			t.Cleanup(func() { lookupPollingChannel = previousLookup })

			fetcher := &countingPollingAdaptor{}
			previousAdaptor := GetTaskAdaptorFunc
			GetTaskAdaptorFunc = func(platform constant.TaskPlatform) TaskPollingAdaptor {
				if platform == constant.TaskPlatformSuno {
					return fetcher
				}
				return nil
			}
			t.Cleanup(func() { GetTaskAdaptorFunc = previousAdaptor })

			require.NotPanics(t, func() {
				err := updateSunoTasks(context.Background(), channelID, []string{task.TaskID}, map[string]*model.Task{
					task.TaskID: task,
				})
				require.NoError(t, err)
			})
			require.Zero(t, fetcher.fetches)

			var reloaded model.Task
			require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
			require.Equal(t, model.TaskStatus(model.TaskStatusSubmitted), reloaded.Status)
			require.Equal(t, preConsumed, reloaded.Quota)
			require.Empty(t, reloaded.FailReason)
			require.False(t, reloaded.RefundPending)
			require.Zero(t, reloaded.PrivateData.PollFailures)
			require.EqualValues(t, 2000, getUserQuota(t, userID))
		})
	}
}

func commonStringPtr(value string) *string {
	return &value
}
