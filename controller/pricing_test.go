package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestFilterPricingByUsableGroupsTrimsUnconfiguredGroups(t *testing.T) {
	pricing := []model.Pricing{
		{ModelName: "mixed", EnableGroup: []string{"default", "legacy", "vip", "default"}},
		{ModelName: "legacy-only", EnableGroup: []string{"legacy"}},
		{ModelName: "all-groups", EnableGroup: []string{" all ", "legacy"}},
		{ModelName: "trimmed", EnableGroup: []string{" default ", "vip"}},
	}
	usableGroup := map[string]string{
		"default": "Default",
		"vip":     "VIP",
	}

	filtered := filterPricingByUsableGroups(pricing, usableGroup)
	byModel := make(map[string]model.Pricing, len(filtered))
	for _, item := range filtered {
		byModel[item.ModelName] = item
	}

	require.Len(t, filtered, 3)
	require.Equal(t, []string{"default", "vip"}, byModel["mixed"].EnableGroup)
	require.Equal(t, []string{"default", "vip"}, byModel["all-groups"].EnableGroup)
	require.Equal(t, []string{"default", "vip"}, byModel["trimmed"].EnableGroup)
	_, exists := byModel["legacy-only"]
	require.False(t, exists)
}

func TestPricingConfiguredUsableGroupsKeepsOnlyRatioGroups(t *testing.T) {
	usableGroup := map[string]string{
		"default": "Default",
		"legacy": "Legacy",
		"auto":   "Auto",
	}
	groupRatio := map[string]float64{
		"default": 1,
		"vip":     2,
	}

	filtered := pricingConfiguredUsableGroups(usableGroup, groupRatio)

	require.Equal(t, map[string]string{"default": "Default"}, filtered)
}

func TestPricingConfiguredAutoGroupsKeepsOnlyUsableGroups(t *testing.T) {
	filtered := pricingConfiguredAutoGroups(
		[]string{"default", "legacy", "default", ""},
		map[string]string{"default": "Default"},
	)

	require.Equal(t, []string{"default"}, filtered)
}
