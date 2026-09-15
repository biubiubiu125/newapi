package service

import (
	"errors"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/require"
)

func TestIsSubscriptionPreConsumeInsufficientErrorUsesSentinels(t *testing.T) {
	require.True(t, isSubscriptionPreConsumeInsufficientError(model.ErrNoActiveSubscription))
	require.True(t, isSubscriptionPreConsumeInsufficientError(fmt.Errorf("wrap: %w", model.ErrSubscriptionQuotaInsufficient)))
	require.True(t, isSubscriptionPreConsumeInsufficientError(fmt.Errorf("%w: default", model.ErrNoActiveSubscriptionGrantsGroup)))
	require.False(t, isSubscriptionPreConsumeInsufficientError(errors.New("sql: database is closed")))
	require.False(t, isSubscriptionPreConsumeInsufficientError(nil))
}
