package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

// 终态任务超出结果保留期后，预约必须放开，否则唯一索引会让这个 client_task_id
// 既拿不到结果（已清理）也永远无法重新提交。
func TestReserveImageTaskClientTaskIDReclaimsLockBoundToExpiredTerminalTask(t *testing.T) {
	truncateTables(t)

	reuseWindow := int64(common.GetImageTaskIdempotencyReuseWindow().Seconds())
	now := time.Now().Unix()
	task := &Task{
		TaskID:       "task_client_lock_expired",
		Platform:     constant.TaskPlatformImage,
		UserId:       1,
		ClientTaskID: "client_lock_expired",
		Status:       TaskStatusSuccess,
		FinishTime:   now - reuseWindow - 60,
	}
	insertTask(t, task)
	boundLock := &ImageTaskClientTaskIDLock{
		UserID:        1,
		ClientTaskID:  "client_lock_expired",
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
	}
	require.NoError(t, DB.Create(boundLock).Error)

	// 复用查询本身也必须查不到这条超窗任务。
	_, exists, err := GetImageTaskByClientTaskID(1, "client_lock_expired")
	require.NoError(t, err)
	require.False(t, exists)

	lock, reserved, err := ReserveImageTaskClientTaskID(1, "client_lock_expired", "new-fingerprint")
	require.NoError(t, err)
	require.True(t, reserved)
	require.NotNil(t, lock)
	require.Zero(t, lock.TaskPrimaryID)
	require.Equal(t, "new-fingerprint", lock.Fingerprint)
}

// 长任务可以跑到 TASK_TIMEOUT_MINUTES（默认 24 小时），远超复用窗口。
// 只要还没到终态，同键重试就必须命中原任务，否则会重复创建并重复扣费。
func TestImageTaskIdempotencyKeepsReusingLongRunningTaskBeyondWindow(t *testing.T) {
	truncateTables(t)

	reuseWindow := int64(common.GetImageTaskIdempotencyReuseWindow().Seconds())
	now := time.Now().Unix()
	task := &Task{
		TaskID:       "task_client_lock_long_running",
		Platform:     constant.TaskPlatformImage,
		UserId:       1,
		ClientTaskID: "client_lock_long_running",
		Status:       TaskStatusInProgress,
		SubmitTime:   now - reuseWindow - 3600,
	}
	insertTask(t, task)
	require.NoError(t, DB.Model(&Task{}).Where("id = ?", task.ID).
		Update("created_at", now-reuseWindow-3600).Error)
	boundLock := &ImageTaskClientTaskIDLock{
		UserID:        1,
		ClientTaskID:  "client_lock_long_running",
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
	}
	require.NoError(t, DB.Create(boundLock).Error)

	reused, exists, err := GetImageTaskByClientTaskID(1, "client_lock_long_running")
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, task.ID, reused.ID)

	lock, reserved, err := ReserveImageTaskClientTaskID(1, "client_lock_long_running")
	require.NoError(t, err)
	require.False(t, reserved, "in-flight task must keep holding its idempotency reservation")
	require.NotNil(t, lock)
	require.Equal(t, task.ID, lock.TaskPrimaryID)
}

func TestImageTaskIdempotencyStopsReusingAcknowledgedCleanedResult(t *testing.T) {
	truncateTables(t)

	now := time.Now().Unix()
	task := &Task{
		TaskID:          "task_client_lock_ack_cleaned",
		Platform:        constant.TaskPlatformImage,
		UserId:          1,
		ClientTaskID:    "client_lock_ack_cleaned",
		Status:          TaskStatusSuccess,
		FinishTime:      now,
		ResultExpiresAt: now + 3600,
		ResultCleanedAt: now,
	}
	insertTask(t, task)
	require.NoError(t, DB.Create(&ImageTaskClientTaskIDLock{
		UserID:        1,
		ClientTaskID:  task.ClientTaskID,
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
	}).Error)

	_, exists, err := GetImageTaskByClientTaskID(1, task.ClientTaskID)
	require.NoError(t, err)
	require.False(t, exists)

	lock, reserved, err := ReserveImageTaskClientTaskID(1, task.ClientTaskID, "new-fingerprint")
	require.NoError(t, err)
	require.True(t, reserved)
	require.NotNil(t, lock)
	require.Zero(t, lock.TaskPrimaryID)
}

func TestImageTaskIdempotencyStopsReusingResultAfterAcknowledgementGrace(t *testing.T) {
	truncateTables(t)

	now := time.Now().Unix()
	task := &Task{
		TaskID:               "task_client_lock_ack_grace_elapsed",
		Platform:             constant.TaskPlatformImage,
		UserId:               1,
		ClientTaskID:         "client_lock_ack_grace_elapsed",
		Status:               TaskStatusSuccess,
		FinishTime:           now,
		ResultExpiresAt:      now + 3600,
		ResultAcknowledgedAt: now - 121,
		ResultDeleteAfter:    now - 1,
	}
	insertTask(t, task)
	require.NoError(t, DB.Create(&ImageTaskClientTaskIDLock{
		UserID:        1,
		ClientTaskID:  task.ClientTaskID,
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
	}).Error)

	_, exists, err := GetImageTaskByClientTaskID(1, task.ClientTaskID)
	require.NoError(t, err)
	require.False(t, exists)
	_, reserved, err := ReserveImageTaskClientTaskID(1, task.ClientTaskID, "new-fingerprint")
	require.NoError(t, err)
	require.True(t, reserved)
}

func TestImageTaskIdempotencyKeepsReusingResultDuringAcknowledgementGrace(t *testing.T) {
	truncateTables(t)

	now := time.Now().Unix()
	task := &Task{
		TaskID:               "task_client_lock_ack_grace_active",
		Platform:             constant.TaskPlatformImage,
		UserId:               1,
		ClientTaskID:         "client_lock_ack_grace_active",
		Status:               TaskStatusSuccess,
		SettlementStatus:     TaskSettlementStatusSettled,
		FinishTime:           now,
		ResultExpiresAt:      now + 3600,
		ResultAcknowledgedAt: now,
		ResultDeleteAfter:    now + 120,
	}
	task.SetData(map[string]any{"data": []any{map[string]any{"url": "https://example.com/ack-grace.png"}}})
	insertTask(t, task)
	require.NoError(t, DB.Create(&ImageTaskClientTaskIDLock{
		UserID:        1,
		ClientTaskID:  task.ClientTaskID,
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
	}).Error)

	reused, exists, err := GetImageTaskByClientTaskID(1, task.ClientTaskID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, task.ID, reused.ID)
	_, reserved, err := ReserveImageTaskClientTaskID(1, task.ClientTaskID, "new-fingerprint")
	require.NoError(t, err)
	require.False(t, reserved)
}

func TestImageTaskIdempotencyStopsReusingFailedTaskImmediately(t *testing.T) {
	truncateTables(t)

	now := time.Now().Unix()
	cases := []struct {
		name         string
		clientTaskID string
		finishTime   int64
	}{
		{name: "finished failure", clientTaskID: "client_lock_failed_finished", finishTime: now},
		{name: "legacy failure without finish time", clientTaskID: "client_lock_failed_legacy", finishTime: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := &Task{
				TaskID:       "task_" + tc.clientTaskID,
				Platform:     constant.TaskPlatformImage,
				UserId:       1,
				ClientTaskID: tc.clientTaskID,
				Status:       TaskStatusFailure,
				FinishTime:   tc.finishTime,
			}
			insertTask(t, task)
			require.NoError(t, DB.Create(&ImageTaskClientTaskIDLock{
				UserID:        1,
				ClientTaskID:  tc.clientTaskID,
				TaskPrimaryID: task.ID,
				PublicTaskID:  task.TaskID,
			}).Error)

			_, exists, err := GetImageTaskByClientTaskID(1, tc.clientTaskID)
			require.NoError(t, err)
			require.False(t, exists)

			lock, reserved, err := ReserveImageTaskClientTaskID(1, tc.clientTaskID, "new-fingerprint")
			require.NoError(t, err)
			require.True(t, reserved)
			require.NotNil(t, lock)
			require.Zero(t, lock.TaskPrimaryID)
		})
	}
}

// 对外不可领取结果的成功态不能继续占用幂等键。
func TestImageTaskIdempotencyStopsReusingSuccessWhenPublicResultIsNotRetrievable(t *testing.T) {
	truncateTables(t)

	now := time.Now().Unix()
	cases := []struct {
		name             string
		clientTaskID     string
		settlementStatus string
		withResult       bool
	}{
		{
			name:             "settlement review is public failed",
			clientTaskID:     "client_lock_success_review",
			settlementStatus: TaskSettlementStatusReview,
			withResult:       true,
		},
		{
			name:             "settled success without result body",
			clientTaskID:     "client_lock_success_settled_no_result",
			settlementStatus: TaskSettlementStatusSettled,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := &Task{
				TaskID:           "task_" + tc.clientTaskID,
				Platform:         constant.TaskPlatformImage,
				UserId:           1,
				ClientTaskID:     tc.clientTaskID,
				Status:           TaskStatusSuccess,
				SettlementStatus: tc.settlementStatus,
				FinishTime:       now,
				ResultExpiresAt:  now + 3600,
			}
			if tc.withResult {
				task.SetData(map[string]any{"data": []any{map[string]any{"url": "https://example.com/review.png"}}})
			}
			insertTask(t, task)
			require.NoError(t, DB.Create(&ImageTaskClientTaskIDLock{
				UserID:        1,
				ClientTaskID:  tc.clientTaskID,
				TaskPrimaryID: task.ID,
				PublicTaskID:  task.TaskID,
			}).Error)

			_, exists, err := GetImageTaskByClientTaskID(1, tc.clientTaskID)
			require.NoError(t, err)
			require.False(t, exists)

			lock, reserved, err := ReserveImageTaskClientTaskID(1, tc.clientTaskID, "new-fingerprint")
			require.NoError(t, err)
			require.True(t, reserved)
			require.NotNil(t, lock)
			require.Zero(t, lock.TaskPrimaryID)
		})
	}
}

// 陈旧预约被回收后同一个 client_task_id 可能对应多条任务，复用必须返回最新那条。
func TestGetImageTaskByClientTaskIDReturnsNewestDuplicate(t *testing.T) {
	truncateTables(t)

	older := &Task{
		TaskID:       "task_client_lock_dup_old",
		Platform:     constant.TaskPlatformImage,
		UserId:       1,
		ClientTaskID: "client_lock_duplicate",
		Status:       TaskStatusFailure,
		FinishTime:   time.Now().Unix() - 60,
	}
	insertTask(t, older)
	newer := &Task{
		TaskID:       "task_client_lock_dup_new",
		Platform:     constant.TaskPlatformImage,
		UserId:       1,
		ClientTaskID: "client_lock_duplicate",
		Status:       TaskStatusInProgress,
	}
	insertTask(t, newer)

	reused, exists, err := GetImageTaskByClientTaskID(1, "client_lock_duplicate")
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, newer.ID, reused.ID)
}

func TestCleanupExpiredImageTaskClientTaskIDLocksKeepsLiveReservations(t *testing.T) {
	truncateTables(t)

	now := time.Now().Unix()
	cutoff := now - 3600

	liveTask := &Task{
		TaskID:       "task_lock_cleanup_live",
		Platform:     constant.TaskPlatformImage,
		UserId:       1,
		ClientTaskID: "client_lock_cleanup_live",
		Status:       TaskStatusInProgress,
	}
	insertTask(t, liveTask)
	liveLock := &ImageTaskClientTaskIDLock{
		UserID:        1,
		ClientTaskID:  "client_lock_cleanup_live",
		TaskPrimaryID: liveTask.ID,
		PublicTaskID:  liveTask.TaskID,
		UpdatedAt:     cutoff - 60,
	}
	require.NoError(t, DB.Create(liveLock).Error)
	require.NoError(t, DB.Model(&ImageTaskClientTaskIDLock{}).Where("id = ?", liveLock.ID).
		Update("updated_at", cutoff-60).Error)

	finishedTask := &Task{
		TaskID:       "task_lock_cleanup_done",
		Platform:     constant.TaskPlatformImage,
		UserId:       1,
		ClientTaskID: "client_lock_cleanup_done",
		Status:       TaskStatusSuccess,
		FinishTime:   cutoff - 120,
	}
	insertTask(t, finishedTask)
	finishedLock := &ImageTaskClientTaskIDLock{
		UserID:        1,
		ClientTaskID:  "client_lock_cleanup_done",
		TaskPrimaryID: finishedTask.ID,
		PublicTaskID:  finishedTask.TaskID,
	}
	require.NoError(t, DB.Create(finishedLock).Error)
	require.NoError(t, DB.Model(&ImageTaskClientTaskIDLock{}).Where("id = ?", finishedLock.ID).
		Update("updated_at", cutoff-60).Error)

	pendingReservation := &ImageTaskClientTaskIDLock{
		UserID:       1,
		ClientTaskID: "client_lock_cleanup_pending",
	}
	require.NoError(t, DB.Create(pendingReservation).Error)
	require.NoError(t, DB.Model(&ImageTaskClientTaskIDLock{}).Where("id = ?", pendingReservation.ID).
		Update("updated_at", cutoff-60).Error)

	deleted, err := CleanupExpiredImageTaskClientTaskIDLocks(cutoff, 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	_, exists, err := GetImageTaskClientTaskIDLock(1, "client_lock_cleanup_done")
	require.NoError(t, err)
	require.False(t, exists)
	// 在途任务和未绑定预约都必须保留。
	_, exists, err = GetImageTaskClientTaskIDLock(1, "client_lock_cleanup_live")
	require.NoError(t, err)
	require.True(t, exists)
	_, exists, err = GetImageTaskClientTaskIDLock(1, "client_lock_cleanup_pending")
	require.NoError(t, err)
	require.True(t, exists)
}

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

func TestReserveImageTaskClientTaskIDStoresRequestFingerprint(t *testing.T) {
	truncateTables(t)

	lock, reserved, err := ReserveImageTaskClientTaskID(1, "client_lock_fingerprint", "request_fingerprint")

	require.NoError(t, err)
	require.True(t, reserved)
	require.Equal(t, "request_fingerprint", lock.Fingerprint)
	reloaded, exists, err := GetImageTaskClientTaskIDLock(1, "client_lock_fingerprint")
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, "request_fingerprint", reloaded.Fingerprint)
}

func TestReleaseImageTaskClientTaskIDReservationDoesNotDeleteReplacement(t *testing.T) {
	truncateTables(t)

	original := &ImageTaskClientTaskIDLock{
		UserID:       1,
		ClientTaskID: "client_lock_replaced",
		Fingerprint:  "original",
	}
	require.NoError(t, DB.Create(original).Error)
	require.NoError(t, DB.Delete(original).Error)

	replacement := &ImageTaskClientTaskIDLock{
		UserID:       1,
		ClientTaskID: "client_lock_replaced",
		Fingerprint:  "replacement",
	}
	require.NoError(t, DB.Create(replacement).Error)

	require.NoError(t, ReleaseImageTaskClientTaskIDReservation(original))
	reloaded, exists, err := GetImageTaskClientTaskIDLock(1, "client_lock_replaced")
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, replacement.ID, reloaded.ID)

	require.NoError(t, ReleaseImageTaskClientTaskIDReservation(replacement))
	_, exists, err = GetImageTaskClientTaskIDLock(1, "client_lock_replaced")
	require.NoError(t, err)
	require.False(t, exists)
}
