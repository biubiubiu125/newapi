package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestGetPayMoneyUsesConfiguredDisplayCurrencyRate(t *testing.T) {
	originalPrice := operation_setting.Price
	originalUSDExchangeRate := operation_setting.USDExchangeRate
	originalCustomExchangeRate := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
	originalQuotaDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalDiscounts := make(map[int]float64, len(operation_setting.GetPaymentSetting().AmountDiscount))
	for k, v := range operation_setting.GetPaymentSetting().AmountDiscount {
		originalDiscounts[k] = v
	}
	originalTopupGroupRatio := common.TopupGroupRatio2JSONString()

	t.Cleanup(func() {
		operation_setting.Price = originalPrice
		operation_setting.USDExchangeRate = originalUSDExchangeRate
		operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = originalCustomExchangeRate
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalQuotaDisplayType
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscounts
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(originalTopupGroupRatio))
	})

	operation_setting.Price = 1
	operation_setting.USDExchangeRate = 7.3
	operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 8.2
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{3: 0.9}
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1,"vip":1.2}`))

	testCases := []struct {
		name             string
		amount           int64
		group            string
		quotaDisplayType string
		expected         float64
	}{
		{
			name:             "CNY display uses USD exchange rate",
			amount:           3,
			group:            "default",
			quotaDisplayType: operation_setting.QuotaDisplayTypeCNY,
			expected:         19.71,
		},
		{
			name:             "USD display uses payment gateway price",
			amount:           3,
			group:            "default",
			quotaDisplayType: operation_setting.QuotaDisplayTypeUSD,
			expected:         2.7,
		},
		{
			name:             "custom display uses custom exchange rate",
			amount:           3,
			group:            "default",
			quotaDisplayType: operation_setting.QuotaDisplayTypeCustom,
			expected:         22.14,
		},
		{
			name:             "group ratio still applies",
			amount:           3,
			group:            "vip",
			quotaDisplayType: operation_setting.QuotaDisplayTypeCNY,
			expected:         23.652,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			operation_setting.GetGeneralSetting().QuotaDisplayType = tc.quotaDisplayType
			actual := getPayMoney(tc.amount, tc.group)
			require.InDelta(t, tc.expected, actual, 0.000001)
		})
	}
}
