package common

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetTrustQuotaUsesConfiguredTrustQuota(t *testing.T) {
	oldTrustQuota := TrustQuota
	t.Cleanup(func() {
		TrustQuota = oldTrustQuota
	})

	TrustQuota = 12345
	require.Equal(t, 12345, GetTrustQuota())

	TrustQuota = 0
	require.Equal(t, 0, GetTrustQuota())

	TrustQuota = -1
	require.Equal(t, 0, GetTrustQuota())
}

func TestInitEnvReadsTrustQuota(t *testing.T) {
	oldTrustQuota := TrustQuota
	oldArgs := os.Args
	oldEnv, hadEnv := os.LookupEnv("TRUST_QUOTA")
	t.Cleanup(func() {
		TrustQuota = oldTrustQuota
		os.Args = oldArgs
		if hadEnv {
			require.NoError(t, os.Setenv("TRUST_QUOTA", oldEnv))
		} else {
			require.NoError(t, os.Unsetenv("TRUST_QUOTA"))
		}
	})

	os.Args = []string{"new-api-test"}
	require.NoError(t, os.Setenv("TRUST_QUOTA", "12345"))

	InitEnv()

	require.Equal(t, 12345, TrustQuota)
}
