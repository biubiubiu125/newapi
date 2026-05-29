package service

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
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

func TestGetPublicProviderPriceExportMatchesHvoyShape(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	oldRaw := common.OptionMap[ProviderPriceOverridesOptionKey]
	oldSystemName := common.SystemName
	common.SystemName = "Test API"
	common.OptionMap[ProviderPriceOverridesOptionKey] = `[
		{
			"id":"row-1",
			"group_name":"stable-group",
			"model_name":"gpt-test",
			"input_price":7.5,
			"cache_create_price":-1,
			"enabled":true
		},
		{
			"id":"row-2",
			"group_name":"cache-only",
			"model_name":"ignored",
			"cache_input_price":1,
			"enabled":true
		},
		{
			"id":"row-3",
			"group_name":"disabled",
			"model_name":"gpt-disabled",
			"input_price":1,
			"enabled":false
		},
		{
			"id":"row-4",
			"group_name":"stable-group",
			"model_name":"gpt-test",
			"input_price":9,
			"enabled":true
		},
		{
			"id":"row-5",
			"group_name":"free-group",
			"model_name":"gpt-free",
			"input_price":0,
			"enabled":true
		}
	]`
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap[ProviderPriceOverridesOptionKey] = oldRaw
		common.SystemName = oldSystemName
		common.OptionMapRWMutex.Unlock()
	})

	data, err := GetPublicProviderPriceExport()
	require.NoError(t, err)
	require.Len(t, data.Models, 3)

	var activeModel ProviderPriceExportModel
	var disabledModel ProviderPriceExportModel
	var freeModel ProviderPriceExportModel
	for _, model := range data.Models {
		if model.ModelName == "gpt-test" {
			activeModel = model
		}
		if model.ModelName == "gpt-disabled" {
			disabledModel = model
		}
		if model.ModelName == "gpt-free" {
			freeModel = model
		}
	}
	require.Equal(t, "stable-group", activeModel.GroupName)
	require.True(t, activeModel.Enabled)
	require.Equal(t, "disabled", disabledModel.GroupName)
	require.False(t, disabledModel.Enabled)
	require.NotNil(t, activeModel.InputPrice)
	require.NotNil(t, activeModel.OutputPrice)
	require.Equal(t, *activeModel.InputPrice, *activeModel.OutputPrice)
	require.Equal(t, "free-group", freeModel.GroupName)
	require.NotNil(t, freeModel.InputPrice)
	require.NotNil(t, freeModel.OutputPrice)
	require.Equal(t, float64(0), *freeModel.InputPrice)
	require.Equal(t, float64(0), *freeModel.OutputPrice)

	modelJSON, err := common.Marshal(activeModel)
	require.NoError(t, err)
	modelPayload := string(modelJSON)
	require.Contains(t, modelPayload, `"output_price":7.5`)
	require.Contains(t, modelPayload, `"cache_input_price":null`)
	require.Contains(t, modelPayload, `"cache_create_price":null`)
	require.Contains(t, modelPayload, `"note":""`)
	require.NotContains(t, modelPayload, "image_output_price")

	dataJSON, err := common.Marshal(data)
	require.NoError(t, err)
	dataPayload := string(dataJSON)
	require.NotContains(t, dataPayload, "display_only")
	require.NotContains(t, dataPayload, "affects_billing")
	require.False(t, strings.Contains(dataPayload, "cache-only"))
	require.False(t, strings.Contains(dataPayload, `"input_price":9`))
}
