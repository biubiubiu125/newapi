package common

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"testing"

	"github.com/stretchr/testify/require"
)

func parseTestPasswordPrivateKey(privateKeyPEM string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(privateKeyPEM))
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	return parsed.(*rsa.PrivateKey), nil
}

func TestPasswordEncryptionRoundTripAndKeyBinding(t *testing.T) {
	privateKeyPEM, err := GeneratePasswordEncryptionPrivateKey()
	require.NoError(t, err)
	require.NoError(t, LoadPasswordEncryptionPrivateKey(privateKeyPEM))

	kid, publicKeyPEM := PasswordEncryptionPublicKey()
	require.NotEmpty(t, kid)
	require.NotEmpty(t, publicKeyPEM)

	privateKey, err := parseTestPasswordPrivateKey(privateKeyPEM)
	require.NoError(t, err)
	ciphertext, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, &privateKey.PublicKey, []byte("correct horse battery staple"), nil)
	require.NoError(t, err)

	plaintext, err := DecryptPassword(base64.StdEncoding.EncodeToString(ciphertext), kid)
	require.NoError(t, err)
	require.Equal(t, "correct horse battery staple", plaintext)
	_, err = DecryptPassword(base64.StdEncoding.EncodeToString(ciphertext), kid)
	require.ErrorIs(t, err, ErrPasswordEncryptionInvalid)

	_, err = DecryptPassword(base64.StdEncoding.EncodeToString(ciphertext), "wrong-key-id")
	require.ErrorIs(t, err, ErrPasswordEncryptionInvalid)
}

func TestDecryptPasswordRejectsMalformedPayload(t *testing.T) {
	privateKeyPEM, err := GeneratePasswordEncryptionPrivateKey()
	require.NoError(t, err)
	require.NoError(t, LoadPasswordEncryptionPrivateKey(privateKeyPEM))
	kid, _ := PasswordEncryptionPublicKey()

	for _, payload := range []string{"", "not-base64", base64.StdEncoding.EncodeToString([]byte("short"))} {
		_, err := DecryptPassword(payload, kid)
		require.ErrorIs(t, err, ErrPasswordEncryptionInvalid, payload)
	}
}
