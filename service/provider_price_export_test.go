package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeProviderPriceOverridesDropsBlankRowsAndSorts(t *testing.T) {
	input := []ProviderPriceOverride{
		{
			GroupName: "default",
			ModelName: "gpt-4.1",
			Enabled:   true,
			SortOrder: 2,
		},
		{
			GroupName: "vip",
			ModelName: "gpt-5",
			Enabled:   true,
			SortOrder: 1,
		},
		{
			GroupName: "",
			ModelName: "blank-row",
			Enabled:   true,
		},
	}

	result := normalizeProviderPriceOverrides(input)
	require.Len(t, result, 2)
	require.Equal(t, "vip", result[0].GroupName)
	require.Equal(t, "gpt-5", result[0].ModelName)
	require.NotEmpty(t, result[0].ID)
	require.Equal(t, "default", result[1].GroupName)
}

func TestHasAnyProviderPriceValue(t *testing.T) {
	zero := ProviderPriceOverride{
		GroupName: "default",
		ModelName: "gpt-4.1",
		Enabled:   true,
	}
	require.False(t, hasAnyProviderPriceValue(zero))

	price := 12.5
	nonZero := zero
	nonZero.InputPrice = &price
	require.True(t, hasAnyProviderPriceValue(nonZero))
}
