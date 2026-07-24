package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

type MidjourneySettlementRecord struct {
	ID               int64  `json:"id" gorm:"primary_key;AUTO_INCREMENT"`
	MidjourneyID     int    `json:"midjourney_id" gorm:"uniqueIndex;not null"`
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
}

func (r *MidjourneySettlementRecord) BeforeCreate(_ *gorm.DB) error {
	now := time.Now().Unix()
	if r.CreatedAt == 0 {
		r.CreatedAt = now
	}
	if r.UpdatedAt == 0 {
		r.UpdatedAt = now
	}
	return nil
}

func (r *MidjourneySettlementRecord) BeforeUpdate(_ *gorm.DB) error {
	r.UpdatedAt = time.Now().Unix()
	return nil
}

func BeginMidjourneySettlementApplication(task *Midjourney, operation string) (*MidjourneySettlementRecord, bool, error) {
	if task == nil || task.Id <= 0 {
		return nil, false, errors.New("midjourney task is required")
	}
	now := time.Now().Unix()
	record := &MidjourneySettlementRecord{
		MidjourneyID: task.Id,
		PublicTaskID: task.MjId,
		Status:       TaskSettlementRecordStatusPrepared,
		Operation:    operation,
	}
	createErr := DB.Create(record).Error
	if createErr == nil {
		return record, true, nil
	}

	existing, exists, loadErr := GetMidjourneySettlementRecord(task.Id)
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
		message := "midjourney settlement application was interrupted before completion; manual review is required"
		if err := MarkMidjourneySettlementApplicationReview(task.Id, message); err != nil {
			return nil, false, err
		}
		existing, exists, loadErr = GetMidjourneySettlementRecord(task.Id)
		if loadErr != nil {
			return nil, false, loadErr
		}
		if !exists {
			return nil, false, errors.New("midjourney settlement record disappeared after review transition")
		}
	}
	return existing, false, nil
}

func MarkMidjourneySettlementApplicationApplying(midjourneyID int) error {
	if midjourneyID <= 0 {
		return errors.New("midjourney id is required")
	}
	result := DB.Model(&MidjourneySettlementRecord{}).
		Where("midjourney_id = ? AND status = ?", midjourneyID, TaskSettlementRecordStatusPrepared).
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
	record, exists, err := GetMidjourneySettlementRecord(midjourneyID)
	if err != nil {
		return err
	}
	if exists && record.Status == TaskSettlementRecordStatusReview {
		return errors.New("midjourney settlement application already requires review")
	}
	return errors.New("midjourney settlement application mark applying lost CAS")
}

func GetMidjourneySettlementRecord(midjourneyID int) (*MidjourneySettlementRecord, bool, error) {
	if midjourneyID <= 0 {
		return nil, false, nil
	}
	var record MidjourneySettlementRecord
	err := DB.Where("midjourney_id = ?", midjourneyID).First(&record).Error
	exists, err := RecordExist(err)
	if err != nil {
		return nil, false, err
	}
	return &record, exists, nil
}

func MarkMidjourneySettlementApplicationApplied(midjourneyID int, details ...TaskSettlementApplicationAppliedDetails) error {
	if midjourneyID <= 0 {
		return errors.New("midjourney id is required")
	}
	now := time.Now().Unix()
	updates := taskSettlementApplicationAppliedUpdates(details...)
	updates["status"] = TaskSettlementRecordStatusApplied
	updates["error"] = ""
	updates["applied_at"] = now
	updates["updated_at"] = now
	result := DB.Model(&MidjourneySettlementRecord{}).
		Where("midjourney_id = ? AND status = ?", midjourneyID, TaskSettlementRecordStatusApplying).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	record, exists, err := GetMidjourneySettlementRecord(midjourneyID)
	if err != nil {
		return err
	}
	if exists && record.Status == TaskSettlementRecordStatusApplied {
		return nil
	}
	return errors.New("midjourney settlement application mark applied lost CAS")
}

func MarkMidjourneySettlementApplicationReview(midjourneyID int, message string) error {
	if midjourneyID <= 0 || message == "" {
		return nil
	}
	result := DB.Model(&MidjourneySettlementRecord{}).
		Where("midjourney_id = ? AND status IN ?", midjourneyID, []string{
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
	record, exists, err := GetMidjourneySettlementRecord(midjourneyID)
	if err != nil {
		return err
	}
	if exists && (record.Status == TaskSettlementRecordStatusReview || record.Status == TaskSettlementRecordStatusApplied) {
		return nil
	}
	return errors.New("midjourney settlement application mark review lost CAS")
}
