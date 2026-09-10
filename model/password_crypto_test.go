package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestInitPasswordEncryptionPersistsAndReloadsActiveKey(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&LoginEncryptionKey{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&LoginEncryptionKey{}).Error)

	previousKeyID, previousPublicKey := common.PasswordEncryptionPublicKey()
	t.Cleanup(func() {
		_ = DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&LoginEncryptionKey{}).Error
		if previousKeyID == "" || previousPublicKey == "" {
			return
		}
	})

	require.NoError(t, InitPasswordEncryption())
	keyID, publicKey := common.PasswordEncryptionPublicKey()
	require.NotEmpty(t, keyID)
	require.NotEmpty(t, publicKey)

	var stored LoginEncryptionKey
	require.NoError(t, DB.Where("slot = ?", activeLoginEncryptionKeySlot).First(&stored).Error)
	require.Equal(t, activeLoginEncryptionKeySlot, stored.Slot)
	require.NotEmpty(t, stored.PrivateKeyPEM)

	require.NoError(t, InitPasswordEncryption())
	reloadedKeyID, reloadedPublicKey := common.PasswordEncryptionPublicKey()
	require.Equal(t, keyID, reloadedKeyID)
	require.Equal(t, publicKey, reloadedPublicKey)
}

func TestClaimPasswordEncryptionReplayIsDurablyUnique(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&AuthFlow{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&AuthFlow{}).Error)

	expiresAt := time.Now().Add(5 * time.Minute)
	require.NoError(t, ClaimPasswordEncryptionReplay("key-1", "digest-1", expiresAt))
	require.ErrorIs(t, ClaimPasswordEncryptionReplay("key-1", "digest-1", expiresAt), ErrAuthFlowConsumed)
}
