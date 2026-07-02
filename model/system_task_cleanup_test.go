package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCleanupFinishedSystemTasksKeepsActiveAndRecentRows(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&SystemTask{}))
	require.NoError(t, DB.Exec("DELETE FROM system_tasks").Error)
	t.Cleanup(func() {
		DB.Exec("DELETE FROM system_tasks")
	})

	now := time.Now().Unix()
	oldSucceeded := &SystemTask{
		TaskID:    "systask_old_succeeded",
		Type:      SystemTaskTypeAsyncTaskPoll,
		Status:    SystemTaskStatusSucceeded,
		CreatedAt: now - 7200,
		UpdatedAt: now - 7200,
	}
	oldFailed := &SystemTask{
		TaskID:    "systask_old_failed",
		Type:      SystemTaskTypeAsyncTaskPoll,
		Status:    SystemTaskStatusFailed,
		CreatedAt: now - 7200,
		UpdatedAt: now - 7200,
	}
	recentFinished := &SystemTask{
		TaskID:    "systask_recent",
		Type:      SystemTaskTypeAsyncTaskPoll,
		Status:    SystemTaskStatusSucceeded,
		CreatedAt: now,
		UpdatedAt: now,
	}
	pending := &SystemTask{
		TaskID:    "systask_pending",
		Type:      SystemTaskTypeAsyncTaskPoll,
		Status:    SystemTaskStatusPending,
		CreatedAt: now - 7200,
		UpdatedAt: now - 7200,
	}
	running := &SystemTask{
		TaskID:    "systask_running",
		Type:      SystemTaskTypeAsyncTaskPoll,
		Status:    SystemTaskStatusRunning,
		CreatedAt: now - 7200,
		UpdatedAt: now - 7200,
	}
	require.NoError(t, DB.Create([]*SystemTask{oldSucceeded, oldFailed, recentFinished, pending, running}).Error)

	deleted, err := CleanupFinishedSystemTasks(now-3600, 100)
	require.NoError(t, err)
	require.EqualValues(t, 2, deleted)

	var remaining []SystemTask
	require.NoError(t, DB.Order("task_id").Find(&remaining).Error)
	require.Len(t, remaining, 3)
	taskIDs := make([]string, 0, len(remaining))
	for _, task := range remaining {
		taskIDs = append(taskIDs, task.TaskID)
	}
	require.ElementsMatch(t, []string{"systask_pending", "systask_recent", "systask_running"}, taskIDs)
}
