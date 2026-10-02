package model

import (
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
)

func TestBEpusdtTradeTypeOptionValidationAndNormalization(t *testing.T) {
	require.NoError(t, validateOptionValue("BEpusdtTradeType", ""))
	require.NoError(t, validateOptionValue("BEpusdtTradeType", " USDT.TRC20 "))
	require.Error(t, validateOptionValue("BEpusdtTradeType", "usdt"))
	require.Error(t, validateOptionValue("BEpusdtTradeType", "usdc.trc20"))

	got, err := normalizeOptionValueForStorage("BEpusdtTradeType", "  ")
	require.NoError(t, err)
	require.Equal(t, setting.DefaultBEpusdtTradeType, got)

	got, err = normalizeOptionValueForStorage("BEpusdtTradeType", "USDT.Polygon")
	require.NoError(t, err)
	require.Equal(t, "usdt.polygon", got)

	_, err = normalizeOptionValueForStorage("BEpusdtTradeType", "usdc.trc20")
	require.Error(t, err)
}
