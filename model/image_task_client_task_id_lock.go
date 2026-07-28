package model

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"gorm.io/gorm"
)

const imageTaskClientTaskIDReservationStaleSeconds = 600

type ImageTaskClientTaskIDLock struct {
	ID            int64  `json:"id" gorm:"primary_key;AUTO_INCREMENT"`
	UserID        int    `json:"user_id" gorm:"column:user_id;uniqueIndex:idx_image_task_client_task_id_lock;index;not null"`
	ClientTaskID  string `json:"client_task_id" gorm:"type:varchar(191);uniqueIndex:idx_image_task_client_task_id_lock;not null"`
	TaskPrimaryID int64  `json:"task_primary_id" gorm:"index"`
	PublicTaskID  string `json:"public_task_id" gorm:"type:varchar(191);index"`
	Fingerprint   string `json:"fingerprint" gorm:"type:varchar(64)"`
	CreatedAt     int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt     int64  `json:"updated_at" gorm:"bigint;index"`
}

func (l *ImageTaskClientTaskIDLock) BeforeCreate(_ *gorm.DB) error {
	now := time.Now().Unix()
	if l.CreatedAt == 0 {
		l.CreatedAt = now
	}
	if l.UpdatedAt == 0 {
		l.UpdatedAt = now
	}
	return nil
}

func (l *ImageTaskClientTaskIDLock) BeforeUpdate(_ *gorm.DB) error {
	l.UpdatedAt = time.Now().Unix()
	return nil
}

func ReserveImageTaskClientTaskID(userID int, clientTaskID string, fingerprints ...string) (*ImageTaskClientTaskIDLock, bool, error) {
	clientTaskID = strings.TrimSpace(clientTaskID)
	if userID <= 0 || clientTaskID == "" {
		return nil, false, nil
	}
	now := GetDBTimestamp()
	fingerprint := ""
	if len(fingerprints) > 0 {
		fingerprint = strings.TrimSpace(fingerprints[0])
	}
	lock := &ImageTaskClientTaskIDLock{
		UserID:       userID,
		ClientTaskID: clientTaskID,
		Fingerprint:  fingerprint,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	err := DB.Create(lock).Error
	if err == nil {
		return lock, true, nil
	}
	if !isImageTaskLeaseConflict(err) {
		return nil, false, err
	}
	existing, exists, loadErr := GetImageTaskClientTaskIDLock(userID, clientTaskID)
	if loadErr != nil {
		return nil, false, loadErr
	}
	if exists && existing != nil {
		deleted, deleteErr := reclaimImageTaskClientTaskIDLockIfStale(existing, now)
		if deleteErr != nil {
			return nil, false, deleteErr
		}
		if deleted {
			lock = &ImageTaskClientTaskIDLock{
				UserID:       userID,
				ClientTaskID: clientTaskID,
				Fingerprint:  fingerprint,
				CreatedAt:    now,
				UpdatedAt:    now,
			}
			err = DB.Create(lock).Error
			if err == nil {
				return lock, true, nil
			}
			if !isImageTaskLeaseConflict(err) {
				return nil, false, err
			}
			existing, exists, loadErr = GetImageTaskClientTaskIDLock(userID, clientTaskID)
			if loadErr != nil {
				return nil, false, loadErr
			}
		}
	}
	if !exists {
		return nil, false, nil
	}
	return existing, false, nil
}

func reclaimImageTaskClientTaskIDLockIfStale(lock *ImageTaskClientTaskIDLock, now int64) (bool, error) {
	if lock == nil {
		return false, nil
	}
	if lock.TaskPrimaryID == 0 {
		if lock.CreatedAt <= 0 || now-lock.CreatedAt <= imageTaskClientTaskIDReservationStaleSeconds {
			return false, nil
		}
		deleted, err := DeleteStaleImageTaskClientTaskIDLock(lock.ID, lock.CreatedAt)
		if deleted {
			// 回收未绑定任务的陈旧预约意味着上一次创建请求没有正常收尾。
			// 极端情况下同一幂等键可能因此产生两个任务，记录下来便于排查。
			common.SysLog(fmt.Sprintf(
				"reclaimed stale image task idempotency reservation: user=%d client_task_id=%s age=%ds",
				lock.UserID, lock.ClientTaskID, now-lock.CreatedAt))
		}
		return deleted, err
	}
	// 绑定任务不存在，或已经超出幂等复用窗口（结果早已清理），都必须放开这把锁。
	// 否则 (user_id, client_task_id) 唯一索引会让客户端在窗口外既拿不到旧结果、
	// 也永远无法用同一个键重新提交。
	reusable, err := imageTaskClientTaskIDLockBoundTaskReusable(lock, now)
	if err != nil || reusable {
		return false, err
	}
	return DeleteOrphanedImageTaskClientTaskIDLock(lock.ID, lock.TaskPrimaryID, lock.UpdatedAt)
}

func ReclaimImageTaskClientTaskIDLockIfStale(lock *ImageTaskClientTaskIDLock) (bool, error) {
	return reclaimImageTaskClientTaskIDLockIfStale(lock, GetDBTimestamp())
}

func imageTaskClientTaskIDLockBoundTaskReusable(lock *ImageTaskClientTaskIDLock, now int64) (bool, error) {
	if lock == nil || lock.TaskPrimaryID <= 0 {
		return false, nil
	}
	var count int64
	query := DB.Model(&Task{}).
		Where("id = ? AND user_id = ? AND platform = ? AND client_task_id = ?",
			lock.TaskPrimaryID, lock.UserID, constant.TaskPlatformImage, lock.ClientTaskID)
	err := imageTaskIdempotencyReusableWhere(query, now).Count(&count).Error
	return count > 0, err
}

func GetImageTaskClientTaskIDLock(userID int, clientTaskID string) (*ImageTaskClientTaskIDLock, bool, error) {
	clientTaskID = strings.TrimSpace(clientTaskID)
	if userID <= 0 || clientTaskID == "" {
		return nil, false, nil
	}
	var lock ImageTaskClientTaskIDLock
	err := DB.Where("user_id = ? AND client_task_id = ?", userID, clientTaskID).First(&lock).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &lock, true, nil
}

func BindImageTaskClientTaskIDLock(userID int, clientTaskID string, task *Task) error {
	clientTaskID = strings.TrimSpace(clientTaskID)
	if userID <= 0 || clientTaskID == "" || task == nil || task.ID <= 0 {
		return nil
	}
	return DB.Model(&ImageTaskClientTaskIDLock{}).
		Where("user_id = ? AND client_task_id = ?", userID, clientTaskID).
		Updates(map[string]any{
			"task_primary_id": task.ID,
			"public_task_id":  task.TaskID,
			"updated_at":      GetDBTimestamp(),
		}).Error
}

func BindImageTaskClientTaskIDLockTx(tx *gorm.DB, reservation *ImageTaskClientTaskIDLock, task *Task) error {
	if tx == nil {
		return errors.New("database transaction is required")
	}
	if reservation == nil || reservation.ID <= 0 || task == nil || task.ID <= 0 {
		return errors.New("image task idempotency reservation and task are required")
	}
	result := tx.Model(&ImageTaskClientTaskIDLock{}).
		Where(
			"id = ? AND user_id = ? AND client_task_id = ? AND fingerprint = ? AND task_primary_id = 0",
			reservation.ID,
			reservation.UserID,
			strings.TrimSpace(reservation.ClientTaskID),
			strings.TrimSpace(reservation.Fingerprint),
		).
		Updates(map[string]any{
			"task_primary_id": task.ID,
			"public_task_id":  task.TaskID,
			"updated_at":      getDBTimestampTx(tx),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("image task idempotency reservation binding lost CAS")
	}
	return nil
}

func ReleaseImageTaskClientTaskIDReservation(reservation *ImageTaskClientTaskIDLock) error {
	if reservation == nil || reservation.ID <= 0 || reservation.UserID <= 0 || strings.TrimSpace(reservation.ClientTaskID) == "" {
		return nil
	}
	return DB.Where(
		"id = ? AND user_id = ? AND client_task_id = ? AND fingerprint = ? AND task_primary_id = 0",
		reservation.ID,
		reservation.UserID,
		strings.TrimSpace(reservation.ClientTaskID),
		strings.TrimSpace(reservation.Fingerprint),
	).
		Delete(&ImageTaskClientTaskIDLock{}).Error
}

func DeleteStaleImageTaskClientTaskIDLock(id int64, createdAt int64) (bool, error) {
	if id <= 0 || createdAt <= 0 {
		return false, nil
	}
	result := DB.Where("id = ? AND task_primary_id = 0 AND created_at = ?", id, createdAt).
		Delete(&ImageTaskClientTaskIDLock{})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func DeleteOrphanedImageTaskClientTaskIDLock(id int64, taskPrimaryID int64, updatedAt int64) (bool, error) {
	if id <= 0 || taskPrimaryID <= 0 {
		return false, nil
	}
	query := DB.Where("id = ? AND task_primary_id = ?", id, taskPrimaryID)
	if updatedAt > 0 {
		query = query.Where("updated_at = ?", updatedAt)
	}
	result := query.Delete(&ImageTaskClientTaskIDLock{})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// CleanupExpiredImageTaskClientTaskIDLocks 回收已经没有作用的幂等预约行。
//
// 预约在绑定任务后不会被删除，而任务本身是长期保留的，所以这张表会随图片任务量
// 无上限增长。绑定任务已经超出复用窗口的行不再影响任何幂等判定（此时
// GetImageTaskByClientTaskID 已经查不到它，reclaim 也会放行），可以安全删除。
// 未绑定的预约由创建流程和 reclaim 负责，不在这里处理。
func CleanupExpiredImageTaskClientTaskIDLocks(cutoff int64, limit int) (int64, error) {
	if cutoff <= 0 {
		return 0, nil
	}
	if limit <= 0 {
		limit = 1000
	}
	var ids []int64
	err := DB.Table("image_task_client_task_id_locks").
		Select("image_task_client_task_id_locks.id").
		Joins("LEFT JOIN tasks ON tasks.id = image_task_client_task_id_locks.task_primary_id").
		Where("image_task_client_task_id_locks.task_primary_id > 0").
		Where("image_task_client_task_id_locks.updated_at < ?", cutoff).
		Where(
			"tasks.id IS NULL OR (tasks.status IN ? AND COALESCE(tasks.finish_time, 0) > 0 AND tasks.finish_time < ?)",
			[]TaskStatus{TaskStatusSuccess, TaskStatusFailure},
			cutoff,
		).
		Order("image_task_client_task_id_locks.id ASC").
		Limit(limit).
		Pluck("image_task_client_task_id_locks.id", &ids).Error
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	result := DB.Where("id IN ?", ids).Delete(&ImageTaskClientTaskIDLock{})
	return result.RowsAffected, result.Error
}
