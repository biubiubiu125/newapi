package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func prepareTaskSettlementRecordTest(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&TaskSettlementRecord{}))
	require.NoError(t, DB.Exec("DELETE FROM task_settlement_records").Error)
	require.NoError(t, DB.Exec("DELETE FROM tasks").Error)
	t.Cleanup(func() {
		DB.Exec("DELETE FROM task_settlement_records")
		DB.Exec("DELETE FROM tasks")
	})
}

func TestBeginTaskSettlementApplicationCreatesPreparedRecord(t *testing.T) {
	prepareTaskSettlementRecordTest(t)

	task := &Task{
		TaskID:           "task_settlement_prepare",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           TaskStatusSuccess,
		Progress:         "100%",
		SettlementStatus: TaskSettlementStatusPending,
	}
	insertTask(t, task)

	record, shouldApply, err := BeginTaskSettlementApplication(task)
	require.NoError(t, err)
	require.True(t, shouldApply)
	require.NotNil(t, record)
	require.Equal(t, TaskSettlementRecordStatusPrepared, record.Status)

	require.NoError(t, MarkTaskSettlementApplicationApplying(task.ID))
	reloaded, exists, err := GetTaskSettlementRecord(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, TaskSettlementRecordStatusApplying, reloaded.Status)
}

func TestBeginTaskSettlementApplicationRetriesExistingPreparedRecord(t *testing.T) {
	prepareTaskSettlementRecordTest(t)

	now := time.Now().Unix()
	task := &Task{
		TaskID:           "task_settlement_retry_prepared",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           TaskStatusSuccess,
		Progress:         "100%",
		SettlementStatus: TaskSettlementStatusPending,
	}
	insertTask(t, task)
	require.NoError(t, DB.Create(&TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        TaskSettlementRecordStatusPrepared,
		CreatedAt:     now - 3600,
		UpdatedAt:     now - 3600,
	}).Error)

	record, shouldApply, err := BeginTaskSettlementApplication(task)
	require.NoError(t, err)
	require.True(t, shouldApply)
	require.NotNil(t, record)
	require.Equal(t, TaskSettlementRecordStatusPrepared, record.Status)

	require.NoError(t, MarkTaskSettlementApplicationApplying(task.ID))
	reloaded, exists, err := GetTaskSettlementRecord(task.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, TaskSettlementRecordStatusApplying, reloaded.Status)
}

func TestCleanupTerminalTaskSettlementRecordsOnlyDeletesClosedRecords(t *testing.T) {
	prepareTaskSettlementRecordTest(t)

	now := time.Now().Unix()
	settledTask := &Task{
		TaskID:           "task_cleanup_settled",
		Platform:         constant.TaskPlatformImage,
		Status:           TaskStatusSuccess,
		Progress:         "100%",
		SettlementStatus: TaskSettlementStatusSettled,
	}
	insertTask(t, settledTask)
	reviewTask := &Task{
		TaskID:           "task_cleanup_review",
		Platform:         constant.TaskPlatformImage,
		Status:           TaskStatusSuccess,
		Progress:         "100%",
		SettlementStatus: TaskSettlementStatusReview,
	}
	insertTask(t, reviewTask)
	pendingTask := &Task{
		TaskID:           "task_cleanup_pending",
		Platform:         constant.TaskPlatformImage,
		Status:           TaskStatusSuccess,
		Progress:         "100%",
		SettlementStatus: TaskSettlementStatusPending,
	}
	insertTask(t, pendingTask)
	recentTask := &Task{
		TaskID:           "task_cleanup_recent",
		Platform:         constant.TaskPlatformImage,
		Status:           TaskStatusSuccess,
		Progress:         "100%",
		SettlementStatus: TaskSettlementStatusSettled,
	}
	insertTask(t, recentTask)

	oldAt := now - 7200
	require.NoError(t, DB.Create([]*TaskSettlementRecord{
		{TaskPrimaryID: settledTask.ID, PublicTaskID: settledTask.TaskID, Status: TaskSettlementRecordStatusApplied, CreatedAt: oldAt, UpdatedAt: oldAt, AppliedAt: oldAt},
		{TaskPrimaryID: reviewTask.ID, PublicTaskID: reviewTask.TaskID, Status: TaskSettlementRecordStatusReview, CreatedAt: oldAt, UpdatedAt: oldAt},
		{TaskPrimaryID: pendingTask.ID, PublicTaskID: pendingTask.TaskID, Status: TaskSettlementRecordStatusApplied, CreatedAt: oldAt, UpdatedAt: oldAt, AppliedAt: oldAt},
		{TaskPrimaryID: recentTask.ID, PublicTaskID: recentTask.TaskID, Status: TaskSettlementRecordStatusApplied, CreatedAt: now, UpdatedAt: now, AppliedAt: now},
		{TaskPrimaryID: 999999, PublicTaskID: "missing_task", Status: TaskSettlementRecordStatusApplied, CreatedAt: oldAt, UpdatedAt: oldAt, AppliedAt: oldAt},
		{TaskPrimaryID: pendingTask.ID + 1000, PublicTaskID: "prepared_missing", Status: TaskSettlementRecordStatusPrepared, CreatedAt: oldAt, UpdatedAt: oldAt},
	}).Error)

	deleted, err := CleanupTerminalTaskSettlementRecords(now-3600, 100)
	require.NoError(t, err)
	require.EqualValues(t, 3, deleted)

	var remaining []TaskSettlementRecord
	require.NoError(t, DB.Order("public_task_id").Find(&remaining).Error)
	require.Len(t, remaining, 3)
	publicIDs := make([]string, 0, len(remaining))
	for _, record := range remaining {
		publicIDs = append(publicIDs, record.PublicTaskID)
	}
	require.ElementsMatch(t, []string{"prepared_missing", "task_cleanup_pending", "task_cleanup_recent"}, publicIDs)
}
