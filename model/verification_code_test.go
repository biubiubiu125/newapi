package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestVerificationCodesPersistAcrossMemoryResetAndConsumeOnce(t *testing.T) {
	truncateTables(t)
	previousMinutes := common.VerificationValidMinutes
	common.VerificationValidMinutes = 10
	t.Cleanup(func() {
		common.VerificationValidMinutes = previousMinutes
		common.DeleteKey("reset@example.com", common.PasswordResetPurpose)
		common.DeleteKey("verify@example.com", common.EmailVerificationPurpose)
		common.DeleteKey("expired@example.com", common.PasswordResetPurpose)
	})

	require.NoError(t, common.RegisterVerificationCodeWithKey("reset@example.com", "reset-token", common.PasswordResetPurpose))
	require.NoError(t, common.RegisterVerificationCodeWithKey("verify@example.com", "654321", common.EmailVerificationPurpose))

	var records []VerificationCode
	require.NoError(t, DB.Find(&records).Error)
	require.Len(t, records, 2)
	for _, record := range records {
		require.NotEqual(t, "reset-token", record.CodeHash)
		require.NotEqual(t, "654321", record.CodeHash)
		require.NotContains(t, record.CodeHash, "reset-token")
		require.Greater(t, record.ExpiresAt, time.Now().Unix())
	}

	require.True(t, common.VerifyCodeWithKey("VERIFY@example.com", "654321", common.EmailVerificationPurpose))
	require.True(t, common.ConsumeCodeWithKey(" RESET@example.com ", "reset-token", common.PasswordResetPurpose))
	require.False(t, common.ConsumeCodeWithKey("reset@example.com", "reset-token", common.PasswordResetPurpose))
	require.True(t, common.VerifyCodeWithKey("verify@example.com", "654321", common.EmailVerificationPurpose))

	common.VerificationValidMinutes = 0
	require.NoError(t, common.RegisterVerificationCodeWithKey("expired@example.com", "expired-token", common.PasswordResetPurpose))
	require.False(t, common.ConsumeCodeWithKey("expired@example.com", "expired-token", common.PasswordResetPurpose))
}
