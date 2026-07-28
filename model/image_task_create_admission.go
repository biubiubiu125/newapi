package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrImageTaskCreateRateLimitExceeded = errors.New("image task create rate limit exceeded")
var ErrImageTaskCreateCapacityExceeded = errors.New("image task create capacity exceeded")

const imageTaskCreateGuardKey = "global"

type ImageTaskCreateGuard struct {
	Key       string `gorm:"primaryKey;type:varchar(32)"`
	UpdatedAt int64  `gorm:"not null;default:0"`
}

type ImageTaskCreateRateBucket struct {
	Key           string `gorm:"primaryKey;type:varchar(191)"`
	WindowStartAt int64  `gorm:"index;not null;default:0"`
	RequestCount  int    `gorm:"not null;default:0"`
	UpdatedAt     int64  `gorm:"not null;default:0"`
}

type ImageTaskCreateReservation struct {
	ID            string `gorm:"primaryKey;type:varchar(64)"`
	UserID        int    `gorm:"index;not null"`
	TokenID       int    `gorm:"index;not null"`
	ReservedBytes int64  `gorm:"not null;default:0"`
	ExpiresAt     int64  `gorm:"index;not null"`
	CreatedAt     int64  `gorm:"not null"`
}

type ImageTaskCreateAdmissionLimits struct {
	RequestLimit          int
	WindowSeconds         int64
	MaxInFlight           int
	MaxReservedBytes      int64
	ReservationTTLSeconds int64
}

// AcquireImageTaskCreateAdmission serializes admission on one database row so
// every API node observes the same rate bucket and in-flight reservations.
func AcquireImageTaskCreateAdmission(
	userID int,
	tokenID int,
	reservedBytes int64,
	now int64,
	limits ImageTaskCreateAdmissionLimits,
) (string, error) {
	if userID <= 0 || tokenID <= 0 {
		return "", fmt.Errorf("invalid image task admission identity")
	}
	if now <= 0 {
		now = GetDBTimestamp()
	}
	if limits.ReservationTTLSeconds <= 0 {
		limits.ReservationTTLSeconds = 600
	}
	if reservedBytes <= 0 {
		reservedBytes = 1
	}
	rateEnabled := limits.RequestLimit > 0 && limits.WindowSeconds > 0
	capacityEnabled := limits.MaxInFlight > 0 || limits.MaxReservedBytes > 0
	if !rateEnabled && !capacityEnabled {
		return "", nil
	}
	if limits.MaxReservedBytes > 0 && reservedBytes > limits.MaxReservedBytes {
		return "", ErrImageTaskCreateCapacityExceeded
	}

	guard := ImageTaskCreateGuard{Key: imageTaskCreateGuardKey, UpdatedAt: now}
	if err := DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&guard).Error; err != nil {
		return "", err
	}

	reservationID := ""
	err := DB.Transaction(func(tx *gorm.DB) error {
		var lockedGuard ImageTaskCreateGuard
		if err := lockForUpdate(tx).Where("key = ?", imageTaskCreateGuardKey).First(&lockedGuard).Error; err != nil {
			return err
		}
		if err := tx.Where("expires_at <= ?", now).Delete(&ImageTaskCreateReservation{}).Error; err != nil {
			return err
		}

		var bucket ImageTaskCreateRateBucket
		if rateEnabled {
			bucketKey := fmt.Sprintf("user:%d", userID)
			windowStart := now - now%limits.WindowSeconds
			result := lockForUpdate(tx).Where("key = ?", bucketKey).Limit(1).Find(&bucket)
			switch {
			case result.Error != nil:
				return result.Error
			case result.RowsAffected == 0:
				bucket = ImageTaskCreateRateBucket{Key: bucketKey, WindowStartAt: windowStart}
			case bucket.WindowStartAt != windowStart:
				bucket.WindowStartAt = windowStart
				bucket.RequestCount = 0
			}
			if bucket.RequestCount >= limits.RequestLimit {
				return ErrImageTaskCreateRateLimitExceeded
			}
		}

		if capacityEnabled {
			var activeCount int64
			if err := tx.Model(&ImageTaskCreateReservation{}).Where("expires_at > ?", now).Count(&activeCount).Error; err != nil {
				return err
			}
			if limits.MaxInFlight > 0 && activeCount >= int64(limits.MaxInFlight) {
				return ErrImageTaskCreateCapacityExceeded
			}
			var activeBytes struct {
				Total int64
			}
			if err := tx.Model(&ImageTaskCreateReservation{}).
				Select("COALESCE(SUM(reserved_bytes), 0) AS total").
				Where("expires_at > ?", now).
				Scan(&activeBytes).Error; err != nil {
				return err
			}
			if limits.MaxReservedBytes > 0 && activeBytes.Total+reservedBytes > limits.MaxReservedBytes {
				return ErrImageTaskCreateCapacityExceeded
			}
		}

		if rateEnabled {
			bucket.RequestCount++
			bucket.UpdatedAt = now
			if err := tx.Save(&bucket).Error; err != nil {
				return err
			}
		}
		if capacityEnabled {
			reservationID = common.GetUUID()
			reservation := ImageTaskCreateReservation{
				ID:            reservationID,
				UserID:        userID,
				TokenID:       tokenID,
				ReservedBytes: reservedBytes,
				ExpiresAt:     now + limits.ReservationTTLSeconds,
				CreatedAt:     now,
			}
			if err := tx.Create(&reservation).Error; err != nil {
				return err
			}
		}
		return tx.Model(&ImageTaskCreateGuard{}).
			Where("key = ?", imageTaskCreateGuardKey).
			Update("updated_at", now).Error
	})
	if err != nil {
		return "", err
	}
	return reservationID, nil
}

func ReleaseImageTaskCreateAdmission(reservationID string) error {
	if reservationID == "" {
		return nil
	}
	return DB.Where("id = ?", reservationID).Delete(&ImageTaskCreateReservation{}).Error
}

func RenewImageTaskCreateAdmission(reservationID string, now int64, ttlSeconds int64) (bool, error) {
	if reservationID == "" {
		return false, nil
	}
	if now <= 0 {
		now = GetDBTimestamp()
	}
	if ttlSeconds <= 0 {
		ttlSeconds = 600
	}
	result := DB.Model(&ImageTaskCreateReservation{}).
		Where("id = ? AND expires_at > ?", reservationID, now).
		Update("expires_at", now+ttlSeconds)
	return result.RowsAffected > 0, result.Error
}
