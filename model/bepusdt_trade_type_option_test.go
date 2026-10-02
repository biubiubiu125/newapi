package model

import (
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
)

func TestBEpusdtTradeTypeOptionValidationAndNormalization(t *testing.T) {
	require.NoError(t, validateOptionValue("BEpusdtTradeType", ""))
	require.NoError(t, validateOptionValue("BEpusdtTradeType", " USDT.TRC20 "))
	require.NoError(t, validateOptionValue("BEpusdtTradeType", "usdt.xlayer, USDT.TRC20, usdt.trc20"))
	require.Error(t, validateOptionValue("BEpusdtTradeType", "usdt"))
	require.Error(t, validateOptionValue("BEpusdtTradeType", "usdc.trc20"))
	require.Error(t, validateOptionValue("BEpusdtTradeType", "usdt.erc20"))
	require.Error(t, validateOptionValue("BEpusdtTradeType", "usdt.trc20,usdt.erc20"))

	got, err := normalizeOptionValueForStorage("BEpusdtTradeType", "  ")
	require.NoError(t, err)
	require.Equal(t, setting.DefaultBEpusdtTradeType, got)

	got, err = normalizeOptionValueForStorage("BEpusdtTradeType", "USDT.Polygon")
	require.NoError(t, err)
	require.Equal(t, "usdt.polygon", got)

	got, err = normalizeOptionValueForStorage("BEpusdtTradeType", "usdt.xlayer, USDT.BEP20, usdt.trc20")
	require.NoError(t, err)
	require.Equal(t, "usdt.trc20,usdt.bep20,usdt.xlayer", got)

	_, err = normalizeOptionValueForStorage("BEpusdtTradeType", "usdc.trc20")
	require.Error(t, err)
	_, err = normalizeOptionValueForStorage("BEpusdtTradeType", ",")
	require.Error(t, err)
}
