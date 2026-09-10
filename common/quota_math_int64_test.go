package common

import (
	"math"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestWalletQuotaMathPreservesValuesAboveInt32(t *testing.T) {
	value := float64(math.MaxInt32) + 12345
	quota, err := WalletQuotaFromFloatStrict(value)
	require.NoError(t, err)
	require.Equal(t, int64(value), quota)
}

func TestWalletQuotaDecimalMathPreservesValuesAboveInt32(t *testing.T) {
	value := decimal.NewFromInt(int64(math.MaxInt32) + 12345)
	quota, err := WalletQuotaFromDecimalStrict(value)
	require.NoError(t, err)
	require.Equal(t, int64(math.MaxInt32)+12345, quota)
}
