package openai

import (
	"fmt"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

func TestCommitRealtimeUsageAfterDeliverySkipsFailedWrite(t *testing.T) {
	usage := &dto.RealtimeUsage{TotalTokens: 12, OutputTokens: 12}
	total := &dto.RealtimeUsage{}
	called := false
	err := commitRealtimeUsageAfterDelivery(false, usage, total, func() error {
		called = true
		return nil
	})
	require.NoError(t, err)
	require.False(t, called)
	require.Zero(t, total.TotalTokens)
}

func TestCommitRealtimeUsageAfterDeliveryKeepsDeliveredFrameWhenReserveFails(t *testing.T) {
	usage := &dto.RealtimeUsage{TotalTokens: 12, OutputTokens: 12}
	usage.OutputTokenDetails.TextTokens = 12
	total := &dto.RealtimeUsage{}
	err := commitRealtimeUsageAfterDelivery(true, usage, total, func() error {
		return fmt.Errorf("reserve failed")
	})
	require.EqualError(t, err, "reserve failed")
	require.Equal(t, 12, total.TotalTokens)
	require.Equal(t, 12, total.OutputTokens)
	require.Equal(t, 12, total.OutputTokenDetails.TextTokens)
}

func TestFinalizeRealtimeUsageKeepsCountedTokensWhenReserveFails(t *testing.T) {
	usage := &dto.RealtimeUsage{TotalTokens: 4, InputTokens: 4}
	usage.InputTokenDetails.TextTokens = 4
	local := &dto.RealtimeUsage{TotalTokens: 8, OutputTokens: 8}
	local.OutputTokenDetails.AudioTokens = 8
	sum := &dto.RealtimeUsage{}

	err := finalizeRealtimeUsage(usage, local, sum, nil, func(item *dto.RealtimeUsage) error {
		return fmt.Errorf("reserve failed")
	})
	require.EqualError(t, err, "reserve failed")
	require.Equal(t, 12, sum.TotalTokens)
	require.Equal(t, 4, sum.InputTokens)
	require.Equal(t, 4, sum.InputTokenDetails.TextTokens)
	require.Equal(t, 8, sum.OutputTokens)
	require.Equal(t, 8, sum.OutputTokenDetails.AudioTokens)
}

func TestAddRealtimeUsageLockedKeepsConcurrentTotals(t *testing.T) {
	var mu sync.Mutex
	total := &dto.RealtimeUsage{}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 100; n++ {
				addRealtimeUsageLocked(&mu, total, &dto.RealtimeUsage{TotalTokens: 1, OutputTokens: 1})
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 800, total.TotalTokens)
	require.Equal(t, 800, total.OutputTokens)
}

func TestCommitRealtimeUsageAfterDeliveryDoesNotDoubleCountSuccessfulReserve(t *testing.T) {
	usage := &dto.RealtimeUsage{TotalTokens: 7, InputTokens: 7}
	total := &dto.RealtimeUsage{}
	err := commitRealtimeUsageAfterDelivery(true, usage, total, func() error {
		addRealtimeUsage(total, usage)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 7, total.TotalTokens)
	require.Equal(t, 7, total.InputTokens)
}
