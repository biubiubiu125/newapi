package model

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const activeLoginEncryptionKeySlot = "active"
const passwordEncryptionReplayPurpose = "password_encryption_replay"

// LoginEncryptionKey stores internal key material used by the browser login
// protocol. It is deliberately separate from administrator-facing options.
type LoginEncryptionKey struct {
	ID            uint   `json:"-" gorm:"primaryKey"`
	Slot          string `json:"-" gorm:"type:varchar(32);not null;uniqueIndex"`
	PrivateKeyPEM string `json:"-" gorm:"type:text;not null"`
}

// InitPasswordEncryption loads the shared login-encryption key from its
// dedicated store. Concurrent replicas converge through the unique slot.
func InitPasswordEncryption() error {
	var stored LoginEncryptionKey
	queryErr := DB.Where("slot = ?", activeLoginEncryptionKeySlot).First(&stored).Error
	if queryErr == nil {
		if err := common.LoadPasswordEncryptionPrivateKey(stored.PrivateKeyPEM); err != nil {
			return fmt.Errorf("load persisted password encryption key: %w", err)
		}
		common.SetPasswordReplayGuard(func(keyID, ciphertextDigest string, expiresAt time.Time) error {
			return ClaimPasswordEncryptionReplay(keyID, ciphertextDigest, expiresAt)
		})
		return nil
	}
	if !errors.Is(queryErr, gorm.ErrRecordNotFound) {
		return fmt.Errorf("read password encryption key: %w", queryErr)
	}

	privateKeyPEM, err := common.GeneratePasswordEncryptionPrivateKey()
	if err != nil {
		return err
	}
	candidate := LoginEncryptionKey{
		Slot:          activeLoginEncryptionKeySlot,
		PrivateKeyPEM: privateKeyPEM,
	}
	if err := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "slot"}},
		DoNothing: true,
	}).Create(&candidate).Error; err != nil {
		return fmt.Errorf("persist password encryption key: %w", err)
	}

	if err := DB.Where("slot = ?", activeLoginEncryptionKeySlot).First(&stored).Error; err != nil {
		return fmt.Errorf("reload password encryption key: %w", err)
	}
	if err := common.LoadPasswordEncryptionPrivateKey(stored.PrivateKeyPEM); err != nil {
		return fmt.Errorf("load persisted password encryption key: %w", err)
	}
	common.SetPasswordReplayGuard(func(keyID, ciphertextDigest string, expiresAt time.Time) error {
		return ClaimPasswordEncryptionReplay(keyID, ciphertextDigest, expiresAt)
	})
	return nil
}

// ClaimPasswordEncryptionReplay stores the ciphertext digest in the shared
// database so replay rejection works across application replicas.
func ClaimPasswordEncryptionReplay(keyID, ciphertextDigest string, expiresAt time.Time) error {
	keyID = strings.TrimSpace(keyID)
	ciphertextDigest = strings.TrimSpace(ciphertextDigest)
	now := time.Now()
	if DB == nil || keyID == "" || ciphertextDigest == "" || !expiresAt.After(now) {
		return ErrAuthFlowInvalid
	}
	token := "password-encryption:" + keyID + ":" + ciphertextDigest
	flow := AuthFlow{
		TokenHash:  authFlowTokenHash(token),
		Purpose:    passwordEncryptionReplayPurpose,
		ExpiresAt:  expiresAt,
		ConsumedAt: &now,
	}
	result := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "token_hash"}},
		DoNothing: true,
	}).Create(&flow)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrAuthFlowConsumed
	}
	return nil
}
