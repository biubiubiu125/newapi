package service

import (
	"errors"
	"fmt"
	"testing"
	"unicode"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/stretchr/testify/require"
)

func TestBillingSessionSettleDoesNotReapplyFundingAfterCommitAmbiguity(t *testing.T) {
	funding := &scriptedFundingSource{settleErr: errors.New("driver lost the response after commit")}
	session := &BillingSession{
		relayInfo:        &relaycommon.RelayInfo{UserId: 7, IsPlayground: true},
		funding:          funding,
		preConsumedQuota: 100,
	}

	require.Error(t, session.Settle(80))
	require.Error(t, session.Settle(80))
	require.Equal(t, 1, funding.settleCalls)

	funding.settleErr = nil
	require.Error(t, session.Settle(40))
	require.Equal(t, 1, funding.settleCalls)
}

type scriptedFundingSource struct {
	settleErr   error
	settleCalls int
	settleDelta int
}

func (f *scriptedFundingSource) Source() string { return BillingSourceWallet }
func (f *scriptedFundingSource) PreConsume(amount int) error {
	return nil
}
func (f *scriptedFundingSource) Settle(delta int) error {
	f.settleCalls++
	f.settleDelta += delta
	return f.settleErr
}
func (f *scriptedFundingSource) Refund() error { return nil }

func TestIsSubscriptionPreConsumeInsufficientErrorUsesSentinels(t *testing.T) {
	require.True(t, isSubscriptionPreConsumeInsufficientError(model.ErrNoActiveSubscription))
	require.True(t, isSubscriptionPreConsumeInsufficientError(fmt.Errorf("wrap: %w", model.ErrSubscriptionQuotaInsufficient)))
	require.True(t, isSubscriptionPreConsumeInsufficientError(fmt.Errorf("%w: default", model.ErrNoActiveSubscriptionGrantsGroup)))
	require.False(t, isSubscriptionPreConsumeInsufficientError(errors.New("sql: database is closed")))
	require.False(t, isSubscriptionPreConsumeInsufficientError(nil))
}

func TestInsufficientQuotaProtocolErrorsStayEnglish(t *testing.T) {
	require.NoError(t, i18n.Init())

	userErr := insufficientUserQuotaProtocolError(100)
	require.NotNil(t, userErr)
	requireNoHan(t, userErr.Error())
	require.Contains(t, userErr.Error(), "Insufficient user quota")

	subErr := insufficientSubscriptionQuotaProtocolError()
	require.NotNil(t, subErr)
	requireNoHan(t, subErr.Error())
	require.Contains(t, subErr.Error(), "Insufficient subscription quota")
}

func requireNoHan(t *testing.T, s string) {
	t.Helper()
	for _, r := range s {
		if unicode.In(r, unicode.Han) {
			t.Fatalf("contains Han: %q", s)
		}
	}
}
