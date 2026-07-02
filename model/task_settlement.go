package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

const (
	TaskSettlementRecordStatusPrepared = "PREPARED"
	TaskSettlementRecordStatusApplying = "APPLYING"
	TaskSettlementRecordStatusApplied  = "APPLIED"
	TaskSettlementRecordStatusReview   = "REVIEW"
)

const taskSettlementApplyingReviewSeconds int64 = 10 * 60

type TaskSettlementRecord struct {
	ID            int64  `json:"id" gorm:"primary_key;AUTO_INCREMENT"`
	TaskPrimaryID int64  `json:"task_primary_id" gorm:"uniqueIndex;not null"`
	PublicTaskID  string `json:"public_task_id" gorm:"type:varchar(191);index"`
	Status        string `json:"status" gorm:"type:varchar(20);index"`
	Error         string `json:"error" gorm:"type:text"`
	CreatedAt     int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt     int64  `json:"updated_at" gorm:"bigint;index"`
	AppliedAt     int64  `json:"applied_at" gorm:"bigint;index"`
}

func (r *TaskSettlementRecord) BeforeCreate(_ *gorm.DB) error {
	now := time.Now().Unix()
	if r.CreatedAt == 0 {
		r.CreatedAt = now
	}
	if r.UpdatedAt == 0 {
		r.UpdatedAt = now
	}
	return nil
}

func (r *TaskSettlementRecord) BeforeUpdate(_ *gorm.DB) error {
	r.UpdatedAt = time.Now().Unix()
	return nil
}

func BeginTaskSettlementApplication(task *Task) (*TaskSettlementRecord, bool, error) {
	if task == nil || task.ID <= 0 {
		return nil, false, errors.New("task is required")
	}
	now := time.Now().Unix()
	record := &TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        TaskSettlementRecordStatusPrepared,
	}
	createErr := DB.Create(record).Error
	if createErr == nil {
		return record, true, nil
	}

	existing, exists, loadErr := GetTaskSettlementRecord(task.ID)
	if loadErr != nil {
		return nil, false, loadErr
	}
	if !exists {
		return nil, false, createErr
	}
	if existing.Status == TaskSettlementRecordStatusPrepared {
		return existing, true, nil
	}
	if existing.Status == TaskSettlementRecordStatusApplying &&
		existing.UpdatedAt > 0 &&
		now-existing.UpdatedAt >= taskSettlementApplyingReviewSeconds {
		message := "task settlement application was interrupted before completion; manual review is required"
		if err := MarkTaskSettlementApplicationReview(task.ID, message); err != nil {
			return nil, false, err
		}
		existing, exists, loadErr = GetTaskSettlementRecord(task.ID)
		if loadErr != nil {
			return nil, false, loadErr
		}
		if !exists {
			return nil, false, errors.New("task settlement record disappeared after review transition")
		}
	}
	return existing, false, nil
}

func MarkTaskSettlementApplicationApplying(taskPrimaryID int64) error {
	if taskPrimaryID <= 0 {
		return errors.New("task primary id is required")
	}
	result := DB.Model(&TaskSettlementRecord{}).
		Where("task_primary_id = ? AND status = ?", taskPrimaryID, TaskSettlementRecordStatusPrepared).
		Updates(map[string]any{
			"status":     TaskSettlementRecordStatusApplying,
			"error":      "",
			"updated_at": time.Now().Unix(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	record, exists, err := GetTaskSettlementRecord(taskPrimaryID)
	if err != nil {
		return err
	}
	if exists && record.Status == TaskSettlementRecordStatusReview {
		return errors.New("task settlement application already requires review")
	}
	return errors.New("task settlement application mark applying lost CAS")
}

func GetTaskSettlementRecord(taskPrimaryID int64) (*TaskSettlementRecord, bool, error) {
	if taskPrimaryID <= 0 {
		return nil, false, nil
	}
	var record TaskSettlementRecord
	err := DB.Where("task_primary_id = ?", taskPrimaryID).First(&record).Error
	exists, err := RecordExist(err)
	if err != nil {
		return nil, false, err
	}
	return &record, exists, nil
}

func MarkTaskSettlementApplicationApplied(taskPrimaryID int64) error {
	if taskPrimaryID <= 0 {
		return errors.New("task primary id is required")
	}
	now := time.Now().Unix()
	result := DB.Model(&TaskSettlementRecord{}).
		Where("task_primary_id = ? AND status = ?", taskPrimaryID, TaskSettlementRecordStatusApplying).
		Updates(map[string]any{
			"status":     TaskSettlementRecordStatusApplied,
			"error":      "",
			"applied_at": now,
			"updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	record, exists, err := GetTaskSettlementRecord(taskPrimaryID)
	if err != nil {
		return err
	}
	if exists && record.Status == TaskSettlementRecordStatusApplied {
		return nil
	}
	return errors.New("task settlement application mark applied lost CAS")
}

func MarkTaskSettlementApplicationReview(taskPrimaryID int64, message string) error {
	if taskPrimaryID <= 0 || message == "" {
		return nil
	}
	result := DB.Model(&TaskSettlementRecord{}).
		Where("task_primary_id = ? AND status IN ?", taskPrimaryID, []string{
			TaskSettlementRecordStatusPrepared,
			TaskSettlementRecordStatusApplying,
		}).
		Updates(map[string]any{
			"status":     TaskSettlementRecordStatusReview,
			"error":      message,
			"updated_at": time.Now().Unix(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	record, exists, err := GetTaskSettlementRecord(taskPrimaryID)
	if err != nil {
		return err
	}
	if exists && (record.Status == TaskSettlementRecordStatusReview || record.Status == TaskSettlementRecordStatusApplied) {
		return nil
	}
	return errors.New("task settlement application mark review lost CAS")
}

func MarkTaskSettlementApplicationError(taskPrimaryID int64, message string) {
	_ = MarkTaskSettlementApplicationReview(taskPrimaryID, message)
}

func CleanupTerminalTaskSettlementRecords(cutoff int64, limit int) (int64, error) {
	if cutoff <= 0 {
		return 0, nil
	}
	if limit <= 0 {
		limit = 1000
	}
	var ids []int64
	err := DB.Table("task_settlement_records").
		Select("task_settlement_records.id").
		Joins("LEFT JOIN tasks ON tasks.id = task_settlement_records.task_primary_id").
		Where("task_settlement_records.updated_at < ?", cutoff).
		Where(`
(
  task_settlement_records.status = ? AND tasks.status = ? AND tasks.settlement_status = ?
) OR (
  task_settlement_records.status = ? AND tasks.status = ? AND tasks.settlement_status = ?
) OR (
  tasks.id IS NULL AND task_settlement_records.status IN (?, ?)
)`,
			TaskSettlementRecordStatusApplied, TaskStatusSuccess, TaskSettlementStatusSettled,
			TaskSettlementRecordStatusReview, TaskStatusSuccess, TaskSettlementStatusReview,
			TaskSettlementRecordStatusApplied, TaskSettlementRecordStatusReview,
		).
		Order("task_settlement_records.id ASC").
		Limit(limit).
		Pluck("task_settlement_records.id", &ids).Error
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	result := DB.Where("id IN ?", ids).Delete(&TaskSettlementRecord{})
	return result.RowsAffected, result.Error
}
