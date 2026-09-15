package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	TaskSettlementRecordStatusPrepared = "PREPARED"
	TaskSettlementRecordStatusApplying = "APPLYING"
	TaskSettlementRecordStatusApplied  = "APPLIED"
	TaskSettlementRecordStatusReview   = "REVIEW"
	TaskSettlementOperationImageAtomic = "image_consumption_atomic_v1"
)

const taskSettlementApplyingReviewSeconds int64 = 10 * 60

const TaskSettlementApplicationInterruptedReviewReason = "task settlement application was interrupted before completion; manual review is required"

type TaskSettlementRecord struct {
	ID               int64  `json:"id" gorm:"primary_key;AUTO_INCREMENT"`
	TaskPrimaryID    int64  `json:"task_primary_id" gorm:"uniqueIndex;not null"`
	PublicTaskID     string `json:"public_task_id" gorm:"type:varchar(191);index"`
	Status           string `json:"status" gorm:"type:varchar(20);index"`
	Operation        string `json:"operation" gorm:"type:varchar(32);index"`
	AppliedQuota     *int   `json:"applied_quota,omitempty"`
	PreConsumedQuota *int   `json:"pre_consumed_quota,omitempty"`
	QuotaDelta       *int   `json:"quota_delta,omitempty"`
	LogType          *int   `json:"log_type,omitempty"`
	Error            string `json:"error" gorm:"type:text"`
	CreatedAt        int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt        int64  `json:"updated_at" gorm:"bigint;index"`
	AppliedAt        int64  `json:"applied_at" gorm:"bigint;index"`
	LogPayload       string `json:"-" gorm:"type:text"`
	LogDeliveredAt   int64  `json:"-" gorm:"bigint;index;not null;default:0"`
	LogAttemptCount  int    `json:"-" gorm:"not null;default:0"`
	LogNextAttemptAt int64  `json:"-" gorm:"bigint;index;not null;default:0"`
	LogLockUntil     int64  `json:"-" gorm:"bigint;index;not null;default:0"`
	LogLockOwner     string `json:"-" gorm:"type:varchar(128);index"`
	LogError         string `json:"-" gorm:"type:text"`
}

func (r *TaskSettlementRecord) HasAppliedQuotaEvidence() bool {
	return r != nil && r.AppliedQuota != nil
}

func (r *TaskSettlementRecord) WasInterrupted() bool {
	return r != nil && strings.TrimSpace(r.Error) == TaskSettlementApplicationInterruptedReviewReason
}

type TaskSettlementApplicationAppliedDetails struct {
	Operation        string
	AppliedQuota     *int
	PreConsumedQuota *int
	QuotaDelta       *int
	LogType          *int
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
	if existing.Status == TaskSettlementRecordStatusApplying && existing.Operation == TaskSettlementOperationImageAtomic {
		return existing, true, nil
	}
	if existing.Status == TaskSettlementRecordStatusApplying &&
		existing.UpdatedAt > 0 &&
		now-existing.UpdatedAt >= taskSettlementApplyingReviewSeconds {
		if existing.HasAppliedQuotaEvidence() {
			if err := MarkTaskSettlementApplicationAppliedFromEvidence(task.ID); err != nil {
				return nil, false, err
			}
			existing, exists, loadErr = GetTaskSettlementRecord(task.ID)
			if loadErr != nil {
				return nil, false, loadErr
			}
			if !exists {
				return nil, false, errors.New("task settlement record disappeared after applied evidence transition")
			}
			return existing, false, nil
		}
		if err := MarkTaskSettlementApplicationReview(task.ID, TaskSettlementApplicationInterruptedReviewReason); err != nil {
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

func resetTaskSettlementApplicationPrepared(taskPrimaryID int64) error {
	result := DB.Model(&TaskSettlementRecord{}).
		Where("task_primary_id = ? AND status = ?", taskPrimaryID, TaskSettlementRecordStatusApplying).
		Updates(map[string]any{
			"status":     TaskSettlementRecordStatusPrepared,
			"error":      "",
			"updated_at": time.Now().Unix(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("task settlement application reset prepared lost CAS")
	}
	return nil
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

func MarkTaskSettlementApplicationApplyingAtomic(taskPrimaryID int64) error {
	if taskPrimaryID <= 0 {
		return errors.New("task primary id is required")
	}
	result := DB.Model(&TaskSettlementRecord{}).
		Where("task_primary_id = ? AND status = ?", taskPrimaryID, TaskSettlementRecordStatusPrepared).
		Updates(map[string]any{
			"status":     TaskSettlementRecordStatusApplying,
			"operation":  TaskSettlementOperationImageAtomic,
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
	if exists && record.Status == TaskSettlementRecordStatusApplying && record.Operation == TaskSettlementOperationImageAtomic {
		return nil
	}
	return errors.New("task atomic settlement application mark applying lost CAS")
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

func taskSettlementApplicationAppliedUpdates(details ...TaskSettlementApplicationAppliedDetails) map[string]any {
	updates := map[string]any{}
	if len(details) == 0 {
		return updates
	}
	detail := details[0]
	if detail.Operation != "" {
		updates["operation"] = detail.Operation
	}
	if detail.AppliedQuota != nil {
		updates["applied_quota"] = *detail.AppliedQuota
	}
	if detail.PreConsumedQuota != nil {
		updates["pre_consumed_quota"] = *detail.PreConsumedQuota
	}
	if detail.QuotaDelta != nil {
		updates["quota_delta"] = *detail.QuotaDelta
	}
	if detail.LogType != nil {
		updates["log_type"] = *detail.LogType
	}
	return updates
}

func MarkTaskSettlementApplicationApplied(taskPrimaryID int64, details ...TaskSettlementApplicationAppliedDetails) error {
	if taskPrimaryID <= 0 {
		return errors.New("task primary id is required")
	}
	now := time.Now().Unix()
	updates := taskSettlementApplicationAppliedUpdates(details...)
	updates["status"] = TaskSettlementRecordStatusApplied
	updates["error"] = ""
	updates["applied_at"] = now
	updates["updated_at"] = now
	result := DB.Model(&TaskSettlementRecord{}).
		Where("task_primary_id = ? AND status = ?", taskPrimaryID, TaskSettlementRecordStatusApplying).
		Updates(updates)
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

func MarkTaskSettlementApplicationAppliedFromEvidence(taskPrimaryID int64) error {
	if taskPrimaryID <= 0 {
		return errors.New("task primary id is required")
	}
	now := time.Now().Unix()
	result := DB.Model(&TaskSettlementRecord{}).
		Where("task_primary_id = ? AND status IN ? AND applied_quota IS NOT NULL", taskPrimaryID, []string{
			TaskSettlementRecordStatusApplying,
			TaskSettlementRecordStatusReview,
		}).
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
	if exists && record.Status == TaskSettlementRecordStatusApplied && record.HasAppliedQuotaEvidence() {
		return nil
	}
	return errors.New("task settlement application mark applied from evidence lost CAS")
}

func MarkTaskSettlementApplicationAppliedTx(tx *gorm.DB, taskPrimaryID int64, logPayload string, details TaskSettlementApplicationAppliedDetails) error {
	if tx == nil {
		return errors.New("database transaction is required")
	}
	if taskPrimaryID <= 0 {
		return errors.New("task primary id is required")
	}
	now := time.Now().Unix()
	updates := taskSettlementApplicationAppliedUpdates(details)
	updates["status"] = TaskSettlementRecordStatusApplied
	updates["error"] = ""
	updates["applied_at"] = now
	updates["updated_at"] = now
	updates["log_payload"] = logPayload
	updates["log_delivered_at"] = 0
	updates["log_attempt_count"] = 0
	updates["log_next_attempt_at"] = now
	updates["log_lock_until"] = 0
	updates["log_lock_owner"] = ""
	updates["log_error"] = ""
	result := tx.Model(&TaskSettlementRecord{}).
		Where("task_primary_id = ? AND status IN ?", taskPrimaryID, []string{TaskSettlementRecordStatusPrepared, TaskSettlementRecordStatusApplying}).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("task settlement application mark applied lost CAS")
	}
	return nil
}

func GetPendingTaskSettlementLogs(limit int, now int64) ([]*TaskSettlementRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	var records []*TaskSettlementRecord
	err := DB.Where("status = ? AND log_payload <> '' AND COALESCE(log_delivered_at, 0) = 0", TaskSettlementRecordStatusApplied).
		Where("COALESCE(log_next_attempt_at, 0) <= ?", now).
		Where("COALESCE(log_lock_until, 0) <= ?", now).
		Order("id ASC").
		Limit(limit).
		Find(&records).Error
	return records, err
}

func ClaimTaskSettlementLog(recordID int64, owner string, now int64, leaseSeconds int64) (bool, error) {
	if recordID <= 0 || owner == "" || leaseSeconds <= 0 {
		return false, nil
	}
	result := DB.Model(&TaskSettlementRecord{}).
		Where("id = ? AND status = ? AND log_payload <> '' AND COALESCE(log_delivered_at, 0) = 0", recordID, TaskSettlementRecordStatusApplied).
		Where("COALESCE(log_next_attempt_at, 0) <= ?", now).
		Where("COALESCE(log_lock_until, 0) <= ?", now).
		Updates(map[string]any{
			"log_lock_owner": owner,
			"log_lock_until": now + leaseSeconds,
		})
	return result.RowsAffected > 0, result.Error
}

func CompleteTaskSettlementLog(recordID int64, owner string, deliveredAt int64) error {
	result := DB.Model(&TaskSettlementRecord{}).
		Where("id = ? AND log_lock_owner = ? AND COALESCE(log_delivered_at, 0) = 0", recordID, owner).
		Updates(map[string]any{
			"log_delivered_at":    deliveredAt,
			"log_lock_owner":      "",
			"log_lock_until":      0,
			"log_next_attempt_at": 0,
			"log_error":           "",
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("complete task settlement log lost lease")
	}
	return nil
}

func DeliverClaimedTaskSettlementLog(
	ctx context.Context,
	recordID int64,
	owner string,
	deliveredAt int64,
	deliver func(tx *gorm.DB, record *TaskSettlementRecord) error,
) error {
	if recordID <= 0 || owner == "" || deliver == nil {
		return errors.New("claimed task settlement log delivery is invalid")
	}
	if deliveredAt <= 0 {
		deliveredAt = time.Now().Unix()
	}
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record TaskSettlementRecord
		if err := lockForUpdate(tx).
			Where("id = ? AND log_lock_owner = ? AND COALESCE(log_delivered_at, 0) = 0", recordID, owner).
			First(&record).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("deliver task settlement log lost lease")
			}
			return err
		}
		if err := deliver(tx, &record); err != nil {
			return err
		}
		result := tx.Model(&TaskSettlementRecord{}).
			Where("id = ? AND log_lock_owner = ? AND COALESCE(log_delivered_at, 0) = 0", recordID, owner).
			Updates(map[string]any{
				"log_delivered_at":    deliveredAt,
				"log_lock_owner":      "",
				"log_lock_until":      0,
				"log_next_attempt_at": 0,
				"log_error":           "",
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("deliver task settlement log lost lease")
		}
		return nil
	})
}

func RetryTaskSettlementLog(recordID int64, owner string, errMessage string, nextAttemptAt int64) error {
	result := DB.Model(&TaskSettlementRecord{}).
		Where("id = ? AND log_lock_owner = ? AND COALESCE(log_delivered_at, 0) = 0", recordID, owner).
		Updates(map[string]any{
			"log_attempt_count":   gorm.Expr("log_attempt_count + 1"),
			"log_next_attempt_at": nextAttemptAt,
			"log_lock_owner":      "",
			"log_lock_until":      0,
			"log_error":           errMessage,
		})
	return result.Error
}

func BackfillTaskSettlementApplicationAppliedDetails(taskPrimaryID int64, details TaskSettlementApplicationAppliedDetails) error {
	if taskPrimaryID <= 0 {
		return errors.New("task primary id is required")
	}
	updates := taskSettlementApplicationAppliedUpdates(details)
	if len(updates) == 0 {
		return nil
	}
	updates["updated_at"] = time.Now().Unix()
	return DB.Model(&TaskSettlementRecord{}).
		Where("task_primary_id = ? AND status = ?", taskPrimaryID, TaskSettlementRecordStatusApplied).
		Updates(updates).Error
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

func ResetTaskSettlementApplicationFromReview(taskPrimaryID int64) error {
	if taskPrimaryID <= 0 {
		return errors.New("task primary id is required")
	}
	existing, exists, err := GetTaskSettlementRecord(taskPrimaryID)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	switch existing.Status {
	case TaskSettlementRecordStatusPrepared:
		return nil
	case TaskSettlementRecordStatusApplied:
		return errors.New("task settlement application already applied")
	case TaskSettlementRecordStatusApplying:
		return errors.New("task settlement application is still applying")
	case TaskSettlementRecordStatusReview:
		if existing.HasAppliedQuotaEvidence() {
			return errors.New("task settlement review has applied quota evidence")
		}
		if existing.WasInterrupted() {
			return errors.New("task settlement application was interrupted before completion")
		}
	default:
		return fmt.Errorf("task settlement application has unexpected status %s", existing.Status)
	}
	now := time.Now().Unix()
	result := DB.Model(&TaskSettlementRecord{}).
		Where("task_primary_id = ? AND status = ? AND applied_quota IS NULL", taskPrimaryID, TaskSettlementRecordStatusReview).
		Updates(map[string]any{
			"status":     TaskSettlementRecordStatusPrepared,
			"error":      "",
			"operation":  "",
			"updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("task settlement application reset from review lost CAS")
	}
	return nil
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
  task_settlement_records.status = ?
  AND (COALESCE(task_settlement_records.log_payload, '') = '' OR COALESCE(task_settlement_records.log_delivered_at, 0) > 0)
  AND tasks.status IN (?, ?) AND COALESCE(tasks.settlement_status, '') NOT IN (?, ?, ?)
) OR (
  task_settlement_records.status = ? AND tasks.status = ? AND tasks.settlement_status = ?
) OR (
  tasks.id IS NULL AND (
    task_settlement_records.status = ? OR (
      task_settlement_records.status = ?
      AND (COALESCE(task_settlement_records.log_payload, '') = '' OR COALESCE(task_settlement_records.log_delivered_at, 0) > 0)
    )
  )
)`,
			TaskSettlementRecordStatusApplied, TaskStatusSuccess, TaskStatusFailure, TaskSettlementStatusPending, TaskSettlementStatusApplied, TaskSettlementStatusReview,
			TaskSettlementRecordStatusReview, TaskStatusSuccess, TaskSettlementStatusReview,
			TaskSettlementRecordStatusReview, TaskSettlementRecordStatusApplied,
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
