package model

import (
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

const imageTaskChannelLeaseCleanupThrottleSeconds int64 = 5

var imageTaskChannelLeaseCleanupUnix sync.Map

type ImageTaskChannelLease struct {
	ID            int64  `json:"id" gorm:"primary_key;AUTO_INCREMENT"`
	ChannelID     int    `json:"channel_id" gorm:"uniqueIndex:idx_image_task_channel_slot;index;not null"`
	Slot          int    `json:"slot" gorm:"uniqueIndex:idx_image_task_channel_slot;not null"`
	TaskPrimaryID int64  `json:"task_primary_id" gorm:"index"`
	Owner         string `json:"owner" gorm:"type:varchar(128);uniqueIndex;index;not null"`
	ExpiresAt     int64  `json:"expires_at" gorm:"bigint;index"`
	CreatedAt     int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt     int64  `json:"updated_at" gorm:"bigint;index"`
}

func (l *ImageTaskChannelLease) BeforeCreate(_ *gorm.DB) error {
	now := time.Now().Unix()
	if l.CreatedAt == 0 {
		l.CreatedAt = now
	}
	if l.UpdatedAt == 0 {
		l.UpdatedAt = now
	}
	return nil
}

func (l *ImageTaskChannelLease) BeforeUpdate(_ *gorm.DB) error {
	l.UpdatedAt = time.Now().Unix()
	return nil
}

func TryAcquireImageTaskChannelLease(channelID int, taskPrimaryID int64, owner string, now int64, leaseSeconds int64, limit int) (bool, error) {
	if channelID <= 0 || taskPrimaryID <= 0 || owner == "" || leaseSeconds <= 0 {
		return false, nil
	}
	if limit <= 0 {
		return true, nil
	}
	_ = cleanupExpiredImageTaskChannelLeasesForAcquire(channelID, now, limit*2)
	expiresAt := now + leaseSeconds
	for slot := 0; slot < limit; slot++ {
		lease := &ImageTaskChannelLease{
			ChannelID:     channelID,
			Slot:          slot,
			TaskPrimaryID: taskPrimaryID,
			Owner:         owner,
			ExpiresAt:     expiresAt,
		}
		if err := DB.Create(lease).Error; err != nil {
			if isImageTaskLeaseConflict(err) {
				continue
			}
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func cleanupExpiredImageTaskChannelLeasesForAcquire(channelID int, now int64, limit int) error {
	if channelID <= 0 {
		return CleanupExpiredImageTaskChannelLeases(now, limit)
	}
	if now <= 0 {
		now = time.Now().Unix()
	}
	if lastValue, ok := imageTaskChannelLeaseCleanupUnix.Load(channelID); ok {
		if last, ok := lastValue.(int64); ok && now-last < imageTaskChannelLeaseCleanupThrottleSeconds {
			return nil
		}
	}
	imageTaskChannelLeaseCleanupUnix.Store(channelID, now)
	return cleanupExpiredImageTaskChannelLeases(channelID, now, limit)
}

func RenewImageTaskChannelLease(owner string, now int64, leaseSeconds int64) (bool, error) {
	if owner == "" || leaseSeconds <= 0 {
		return false, nil
	}
	result := DB.Model(&ImageTaskChannelLease{}).
		Where("owner = ? AND expires_at > ?", owner, now).
		Updates(map[string]any{
			"expires_at": now + leaseSeconds,
			"updated_at": now,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func ReleaseImageTaskChannelLease(owner string) error {
	if owner == "" {
		return nil
	}
	return DB.Where("owner = ?", owner).Delete(&ImageTaskChannelLease{}).Error
}

func CleanupExpiredImageTaskChannelLeases(now int64, limit int) error {
	return cleanupExpiredImageTaskChannelLeases(0, now, limit)
}

func cleanupExpiredImageTaskChannelLeases(channelID int, now int64, limit int) error {
	if now <= 0 {
		now = time.Now().Unix()
	}
	if limit <= 0 {
		limit = 1000
	}
	var ids []int64
	query := DB.Model(&ImageTaskChannelLease{}).Where("expires_at <= ?", now)
	if channelID > 0 {
		query = query.Where("channel_id = ?", channelID)
	}
	if err := query.Order("id ASC").
		Limit(limit).
		Pluck("id", &ids).Error; err != nil || len(ids) == 0 {
		return err
	}
	return DB.Where("id IN ?", ids).Delete(&ImageTaskChannelLease{}).Error
}

func CountActiveImageTaskChannelLeases(channelID int, now int64) (int64, error) {
	var count int64
	err := DB.Model(&ImageTaskChannelLease{}).
		Where("channel_id = ? AND expires_at > ?", channelID, now).
		Count(&count).Error
	return count, err
}

func isImageTaskLeaseConflict(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate") ||
		strings.Contains(message, "unique") ||
		strings.Contains(message, "constraint")
}
