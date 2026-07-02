package model

import (
	"errors"
	"strings"
	"time"

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

func ReserveImageTaskClientTaskID(userID int, clientTaskID string) (*ImageTaskClientTaskIDLock, bool, error) {
	clientTaskID = strings.TrimSpace(clientTaskID)
	if userID <= 0 || clientTaskID == "" {
		return nil, false, nil
	}
	now := GetDBTimestamp()
	lock := &ImageTaskClientTaskIDLock{
		UserID:       userID,
		ClientTaskID: clientTaskID,
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
		return DeleteStaleImageTaskClientTaskIDLock(lock.ID, lock.CreatedAt)
	}
	exists, err := imageTaskClientTaskIDLockBoundTaskExists(lock)
	if err != nil || exists {
		return false, err
	}
	return DeleteOrphanedImageTaskClientTaskIDLock(lock.ID, lock.TaskPrimaryID, lock.UpdatedAt)
}

func imageTaskClientTaskIDLockBoundTaskExists(lock *ImageTaskClientTaskIDLock) (bool, error) {
	if lock == nil || lock.TaskPrimaryID <= 0 {
		return false, nil
	}
	var count int64
	err := DB.Model(&Task{}).
		Where("id = ? AND user_id = ? AND platform = ? AND client_task_id = ?",
			lock.TaskPrimaryID, lock.UserID, constant.TaskPlatformImage, lock.ClientTaskID).
		Count(&count).Error
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

func ReleaseImageTaskClientTaskIDLock(userID int, clientTaskID string) error {
	clientTaskID = strings.TrimSpace(clientTaskID)
	if userID <= 0 || clientTaskID == "" {
		return nil
	}
	return DB.Where("user_id = ? AND client_task_id = ? AND task_primary_id = 0", userID, clientTaskID).
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
