package controller

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPasswordTransportPrefersEncryptedPassword(t *testing.T) {
	previousEnabled := common.PasswordLoginEncryptionEnabled
	common.PasswordLoginEncryptionEnabled = true
	t.Cleanup(func() {
		common.PasswordLoginEncryptionEnabled = previousEnabled
	})

	privateKeyPEM, err := common.GeneratePasswordEncryptionPrivateKey()
	require.NoError(t, err)
	require.NoError(t, common.LoadPasswordEncryptionPrivateKey(privateKeyPEM))
	keyID, publicKeyPEM := common.PasswordEncryptionPublicKey()
	require.NotEmpty(t, keyID)

	ciphertext := encryptTestPassword(t, publicKeyPEM, "encrypted-password")
	password, err := resolvePasswordTransport(
		"plaintext-password",
		ciphertext,
		keyID,
		false,
	)
	require.NoError(t, err)
	assert.Equal(t, "encrypted-password", password)
}

func TestPasswordTransportRequiresEncryptedPasswordWhenEnabled(t *testing.T) {
	previousEnabled := common.PasswordLoginEncryptionEnabled
	common.PasswordLoginEncryptionEnabled = true
	t.Cleanup(func() {
		common.PasswordLoginEncryptionEnabled = previousEnabled
	})

	_, err := resolvePasswordTransport("legacy-password", "", "", true)
	require.ErrorIs(t, err, common.ErrPasswordEncryptionInvalid)
}

func TestPasswordTransportMapDecryptsPasswordFields(t *testing.T) {
	privateKeyPEM, err := common.GeneratePasswordEncryptionPrivateKey()
	require.NoError(t, err)
	require.NoError(t, common.LoadPasswordEncryptionPrivateKey(privateKeyPEM))
	keyID, publicKeyPEM := common.PasswordEncryptionPublicKey()

	values := map[string]interface{}{
		"password_encrypted":          encryptTestPassword(t, publicKeyPEM, "new-password"),
		"original_password_encrypted": encryptTestPassword(t, publicKeyPEM, "old-password"),
		"encryption_key_id":           keyID,
	}
	require.NoError(t, normalizePasswordTransportMap(values))
	assert.Equal(t, "new-password", values["password"])
	assert.Equal(t, "old-password", values["original_password"])
	assert.NotContains(t, values, "password_encrypted")
	assert.NotContains(t, values, "original_password_encrypted")
	assert.NotContains(t, values, "encryption_key_id")
}

func encryptTestPassword(t *testing.T, publicKeyPEM string, password string) string {
	t.Helper()
	block, _ := pem.Decode([]byte(publicKeyPEM))
	require.NotNil(t, block)
	publicKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	require.NoError(t, err)
	rsaPublicKey, ok := publicKey.(*rsa.PublicKey)
	require.True(t, ok)
	ciphertext, err := rsa.EncryptOAEP(
		sha256.New(),
		rand.Reader,
		rsaPublicKey,
		[]byte(password),
		nil,
	)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(ciphertext)
}
