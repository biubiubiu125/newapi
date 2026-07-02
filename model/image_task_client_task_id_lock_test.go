package model

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestReserveImageTaskClientTaskIDKeepsBoundLockWhenTaskExists(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID:       "task_client_lock_exists",
		Platform:     constant.TaskPlatformImage,
		UserId:       1,
		ClientTaskID: "client_lock_exists",
		Status:       TaskStatusQueued,
	}
	insertTask(t, task)
	existingLock := &ImageTaskClientTaskIDLock{
		UserID:        1,
		ClientTaskID:  "client_lock_exists",
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
	}
	require.NoError(t, DB.Create(existingLock).Error)

	lock, reserved, err := ReserveImageTaskClientTaskID(1, "client_lock_exists")

	require.NoError(t, err)
	require.False(t, reserved)
	require.NotNil(t, lock)
	require.Equal(t, existingLock.ID, lock.ID)
	require.Equal(t, task.ID, lock.TaskPrimaryID)
}

func TestReserveImageTaskClientTaskIDReclaimsOrphanedBoundLock(t *testing.T) {
	truncateTables(t)

	orphanedLock := &ImageTaskClientTaskIDLock{
		UserID:        1,
		ClientTaskID:  "client_lock_orphaned",
		TaskPrimaryID: 999999,
		PublicTaskID:  "task_missing",
	}
	require.NoError(t, DB.Create(orphanedLock).Error)

	lock, reserved, err := ReserveImageTaskClientTaskID(1, "client_lock_orphaned")

	require.NoError(t, err)
	require.True(t, reserved)
	require.NotNil(t, lock)
	require.Zero(t, lock.TaskPrimaryID)
	require.Empty(t, lock.PublicTaskID)

	var count int64
	require.NoError(t, DB.Model(&ImageTaskClientTaskIDLock{}).
		Where("user_id = ? AND client_task_id = ?", 1, "client_lock_orphaned").
		Count(&count).Error)
	require.EqualValues(t, 1, count)
}
