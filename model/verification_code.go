package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// VerificationCode is the shared store for registration and password-reset
// codes. Only the hash is persisted.
type VerificationCode struct {
	Id        int    `json:"id" gorm:"primaryKey"`
	Purpose   string `json:"purpose" gorm:"type:varchar(16);not null;uniqueIndex:idx_verification_purpose_key,priority:1"`
	LookupKey string `json:"lookup_key" gorm:"type:varchar(255);not null;uniqueIndex:idx_verification_purpose_key,priority:2"`
	CodeHash  string `json:"-" gorm:"type:varchar(64);not null"`
	ExpiresAt int64  `json:"expires_at" gorm:"not null;index"`
}

func (VerificationCode) TableName() string {
	return "verification_codes"
}

type databaseVerificationBackend struct{}

var verificationTables sync.Map

func init() {
	common.SetVerificationBackend(databaseVerificationBackend{})
}

func (databaseVerificationBackend) Save(key, code, purpose string, expiresAt time.Time) error {
	db, err := verificationDB()
	if err != nil {
		return err
	}
	lookupKey := normalizeVerificationLookup(key)
	record := VerificationCode{
		Purpose:   purpose,
		LookupKey: lookupKey,
		CodeHash:  hashVerificationCode(purpose, lookupKey, code),
		ExpiresAt: expiresAt.Unix(),
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("expires_at <= ?", time.Now().Unix()).Delete(&VerificationCode{}).Error; err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "purpose"},
				{Name: "lookup_key"},
			},
			DoUpdates: clause.AssignmentColumns([]string{"code_hash", "expires_at"}),
		}).Create(&record).Error
	})
}

func (databaseVerificationBackend) Match(key, code, purpose string, now time.Time) (bool, error) {
	db, err := verificationDB()
	if err != nil {
		return false, err
	}
	lookupKey := normalizeVerificationLookup(key)
	var count int64
	err = db.Model(&VerificationCode{}).
		Where("purpose = ? AND lookup_key = ? AND code_hash = ? AND expires_at > ?", purpose, lookupKey, hashVerificationCode(purpose, lookupKey, code), now.Unix()).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (databaseVerificationBackend) Consume(key, code, purpose string, now time.Time) (bool, error) {
	db, err := verificationDB()
	if err != nil {
		return false, err
	}
	lookupKey := normalizeVerificationLookup(key)
	result := db.Where(
		"purpose = ? AND lookup_key = ? AND code_hash = ? AND expires_at > ?",
		purpose,
		lookupKey,
		hashVerificationCode(purpose, lookupKey, code),
		now.Unix(),
	).Delete(&VerificationCode{})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func consumeVerificationCodeTx(tx *gorm.DB, key, code, purpose string, now time.Time) (bool, error) {
	if tx == nil {
		return false, errors.New("verification storage is unavailable")
	}
	lookupKey := normalizeVerificationLookup(key)
	result := tx.Where(
		"purpose = ? AND lookup_key = ? AND code_hash = ? AND expires_at > ?",
		purpose,
		lookupKey,
		hashVerificationCode(purpose, lookupKey, code),
		now.Unix(),
	).Delete(&VerificationCode{})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (databaseVerificationBackend) Delete(key, purpose string) error {
	db, err := verificationDB()
	if err != nil {
		return err
	}
	return db.Where("purpose = ? AND lookup_key = ?", purpose, normalizeVerificationLookup(key)).Delete(&VerificationCode{}).Error
}

func verificationDB() (*gorm.DB, error) {
	if DB == nil {
		return nil, errors.New("verification storage is unavailable")
	}
	if err := ensureVerificationTable(DB); err != nil {
		return nil, err
	}
	return DB, nil
}

func ensureVerificationTable(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	if _, ready := verificationTables.Load(sqlDB); ready {
		return nil
	}
	if err := db.AutoMigrate(&VerificationCode{}); err != nil {
		return err
	}
	verificationTables.Store(sqlDB, struct{}{})
	return nil
}

func normalizeVerificationLookup(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}

func hashVerificationCode(purpose, key, code string) string {
	sum := sha256.Sum256([]byte(purpose + "\n" + key + "\n" + code))
	return hex.EncodeToString(sum[:])
}
