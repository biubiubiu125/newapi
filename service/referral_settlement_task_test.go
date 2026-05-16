package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReferralSettlementTickIntervalFallbackAndMinimum(t *testing.T) {
	t.Setenv("REFERRAL_SETTLEMENT_INTERVAL_SECONDS", "")
	require.Equal(t, defaultReferralSettlementTickInterval, referralSettlementTickInterval())

	t.Setenv("REFERRAL_SETTLEMENT_INTERVAL_SECONDS", "1")
	require.Equal(t, minReferralSettlementTickInterval, referralSettlementTickInterval())

	t.Setenv("REFERRAL_SETTLEMENT_INTERVAL_SECONDS", "90")
	require.Equal(t, 90*time.Second, referralSettlementTickInterval())
}
