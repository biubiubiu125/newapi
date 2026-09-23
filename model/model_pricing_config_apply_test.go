package model

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/stretchr/testify/require"
)

func TestCanonicalBillingModeAcceptsFrontendAliases(t *testing.T) {
	ratio, ok := CanonicalBillingMode("per-request")
	require.True(t, ok)
	require.Equal(t, "ratio", ratio)

	ratio, ok = CanonicalBillingMode("per-token")
	require.True(t, ok)
	require.Equal(t, "ratio", ratio)

	ratio, ok = CanonicalBillingMode("ratio")
	require.True(t, ok)
	require.Equal(t, "ratio", ratio)

	expr, ok := CanonicalBillingMode("tiered_expr")
	require.True(t, ok)
	require.Equal(t, "tiered_expr", expr)

	_, ok = CanonicalBillingMode("unknown")
	require.False(t, ok)
}

func TestApplyJSONOptionMapsContinuesAfterError(t *testing.T) {
	var keys []string
	err := applyJSONOptionMaps(
		[]string{"CacheRatio", "ModelPrice", "ModelRatio"},
		map[string]map[string]any{
			"CacheRatio": {"keep": 1.0},
			"ModelPrice": {"keep": 0.1},
		},
		func(key, _ string) error {
			keys = append(keys, key)
			if key == "CacheRatio" {
				return errors.New("boom")
			}
			return nil
		},
	)
	require.EqualError(t, err, "boom")
	require.Equal(t, []string{"CacheRatio", "ModelPrice", "CacheRatio"}, keys)
}

func TestApplyJSONOptionMapsRetriesFailedKey(t *testing.T) {
	var keys []string
	attempts := map[string]int{}
	err := applyJSONOptionMaps(
		[]string{"CacheRatio", "ModelPrice"},
		map[string]map[string]any{
			"CacheRatio": {"keep": 1.0},
			"ModelPrice": {"keep": 0.1},
		},
		func(key, _ string) error {
			keys = append(keys, key)
			attempts[key]++
			if key == "CacheRatio" && attempts[key] == 1 {
				return errors.New("boom")
			}
			return nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, []string{"CacheRatio", "ModelPrice", "CacheRatio"}, keys)
}

func TestApplyJSONOptionMapsDoesNotDropMarshalErrorWhenOtherKeysRetry(t *testing.T) {
	var keys []string
	attempts := map[string]int{}
	err := applyJSONOptionMaps(
		[]string{"CacheRatio", "ModelPrice"},
		map[string]map[string]any{
			"CacheRatio": {"keep": make(chan int)},
			"ModelPrice": {"keep": 0.1},
		},
		func(key, _ string) error {
			keys = append(keys, key)
			attempts[key]++
			if key == "ModelPrice" && attempts[key] == 1 {
				return errors.New("boom")
			}
			return nil
		},
	)
	require.Error(t, err)
	require.NotEqual(t, "boom", err.Error())
	require.Equal(t, []string{"ModelPrice", "ModelPrice"}, keys)
}

func TestUpdateOptionMapRestoresOptionMapWhenRatioJSONIsInvalid(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	previous, hadPrevious := common.OptionMap["ModelPrice"]
	common.OptionMap["ModelPrice"] = `{"keep":0.1}`
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if hadPrevious {
			common.OptionMap["ModelPrice"] = previous
		} else {
			delete(common.OptionMap, "ModelPrice")
		}
	})

	err := updateOptionMap("ModelPrice", `{`)
	require.Error(t, err)

	common.OptionMapRWMutex.RLock()
	got := common.OptionMap["ModelPrice"]
	common.OptionMapRWMutex.RUnlock()
	require.Equal(t, `{"keep":0.1}`, got)
}

func TestMutateModelPricingOptionsDoesNotRefreshPricingWhenApplyFails(t *testing.T) {
	useModelPricingSQLite(t)
	previous := applyCommittedModelPricing
	applyCommittedModelPricing = func(map[string]map[string]any) error {
		return errors.New("boom")
	}
	t.Cleanup(func() { applyCommittedModelPricing = previous })

	sentinel := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	lastGetPricingTime = sentinel

	err := UpdateModelPricingOptions(map[string]string{
		"ModelPrice": `{"keep":0.25}`,
	})
	require.EqualError(t, err, "boom")
	require.Equal(t, sentinel, lastGetPricingTime)
}

func TestMutateModelPricingOptionsRefreshesPricingAfterSuccessfulApply(t *testing.T) {
	useModelPricingSQLite(t)
	sentinel := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	lastGetPricingTime = sentinel

	require.NoError(t, UpdateModelPricingOptions(map[string]string{
		"ModelPrice": `{"keep":0.25}`,
	}))
	require.False(t, lastGetPricingTime.Equal(sentinel))
}

func useModelPricingSQLite(t *testing.T) {
	t.Helper()
	// sqlite memory is a unit harness, not the production PostgreSQL path.
	originalMaster := common.IsMasterNode
	originalDSN := os.Getenv("SQL_DSN")
	originalDB := DB
	t.Cleanup(func() {
		common.IsMasterNode = originalMaster
		_ = os.Setenv("SQL_DSN", originalDSN)
		DB = originalDB
	})
	common.IsMasterNode = false
	t.Setenv("SQL_DSN", "local")
	require.NoError(t, InitDB())
	require.NoError(t, DB.AutoMigrate(&Option{}, &Ability{}, &Channel{}))
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	common.OptionMapRWMutex.Unlock()
}

func TestUpdateModelPricingOptionsDoesNotPersistDisplayBuiltin(t *testing.T) {
	useModelPricingSQLite(t)
	builtin, ok := billing_setting.GetBuiltinBillingExpr("gpt-6-astra")
	require.True(t, ok)

	require.NoError(t, UpdateModelPricingOptions(map[string]string{
		"billing_setting.billing_mode": `{}`,
		"billing_setting.billing_expr": `{}`,
	}))
	require.NoError(t, UpdateModelPricingOptions(map[string]string{
		"billing_setting.billing_mode": `{"keep":"ratio","gpt-6-astra":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"gpt-6-astra":` + jsonQuote(builtin) + `}`,
	}))

	var modeRow Option
	require.NoError(t, DB.Where("key = ?", "billing_setting.billing_mode").First(&modeRow).Error)
	var modes map[string]any
	require.NoError(t, json.Unmarshal([]byte(modeRow.Value), &modes))
	require.Equal(t, "ratio", modes["keep"])
	_, hasBuiltin := modes["gpt-6-astra"]
	require.False(t, hasBuiltin)

	var exprRow Option
	require.NoError(t, DB.Where("key = ?", "billing_setting.billing_expr").First(&exprRow).Error)
	var exprs map[string]any
	require.NoError(t, json.Unmarshal([]byte(exprRow.Value), &exprs))
	_, hasBuiltinExpr := exprs["gpt-6-astra"]
	require.False(t, hasBuiltinExpr)
}

func TestUpdateModelPricingDoesNotPersistDisplayBuiltin(t *testing.T) {
	useModelPricingSQLite(t)
	builtin, ok := billing_setting.GetBuiltinBillingExpr("gpt-6-astra")
	require.True(t, ok)

	require.NoError(t, UpdateModelPricingOptions(map[string]string{
		"billing_setting.billing_mode": `{}`,
		"billing_setting.billing_expr": `{}`,
	}))

	snapshot, err := GetModelPricingSnapshot([]string{"gpt-6-astra"})
	require.NoError(t, err)
	require.NotEmpty(t, snapshot.Entries)
	entry := snapshot.Entries[0]
	require.Equal(t, "gpt-6-astra", entry.ModelName)

	require.NoError(t, UpdateModelPricing([]ModelPricingChange{{
		ModelName:       "gpt-6-astra",
		ExpectedVersion: entry.Version,
		Pricing: PricingValues{
			"billing_setting.billing_mode": "tiered_expr",
			"billing_setting.billing_expr": builtin,
		},
	}}))

	var modeRow Option
	require.NoError(t, DB.Where("key = ?", "billing_setting.billing_mode").First(&modeRow).Error)
	var modes map[string]any
	require.NoError(t, json.Unmarshal([]byte(modeRow.Value), &modes))
	_, hasBuiltin := modes["gpt-6-astra"]
	require.False(t, hasBuiltin)

	var exprRow Option
	require.NoError(t, DB.Where("key = ?", "billing_setting.billing_expr").First(&exprRow).Error)
	var exprs map[string]any
	require.NoError(t, json.Unmarshal([]byte(exprRow.Value), &exprs))
	_, hasBuiltinExpr := exprs["gpt-6-astra"]
	require.False(t, hasBuiltinExpr)
}

func TestMutateModelPricingOptionsDoesNotInvalidateCacheWhenBillingApplyFollowedByFailure(t *testing.T) {
	useModelPricingSQLite(t)
	previous := applyCommittedModelPricing
	applyCommittedModelPricing = func(committed map[string]map[string]any) error {
		return applyJSONOptionMaps(modelPricingOptionKeys, committed, func(key, value string) error {
			if key == "billing_setting.billing_expr" {
				return errors.New("boom")
			}
			return updateOptionMap(key, value)
		})
	}
	t.Cleanup(func() { applyCommittedModelPricing = previous })

	sentinel := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	lastGetPricingTime = sentinel

	err := UpdateModelPricingOptions(map[string]string{
		"billing_setting.billing_mode": `{"keep":"ratio"}`,
		"billing_setting.billing_expr": `{}`,
	})
	require.EqualError(t, err, "boom")
	require.Equal(t, sentinel, lastGetPricingTime)
}

func TestUpdateOptionMapRestoresWhenBillingJSONIsInvalid(t *testing.T) {
	useModelPricingSQLite(t)
	t.Cleanup(func() {
		_ = updateOptionMap("billing_setting.billing_mode", `{}`)
		_ = updateOptionMap("billing_setting.billing_expr", `{}`)
	})
	require.NoError(t, updateOptionMap("billing_setting.billing_mode", `{"keep":"ratio"}`))
	err := updateOptionMap("billing_setting.billing_mode", `{`)
	require.Error(t, err)
	require.Equal(t, "ratio", billing_setting.GetBillingMode("keep"))

	common.OptionMapRWMutex.RLock()
	got := common.OptionMap["billing_setting.billing_mode"]
	common.OptionMapRWMutex.RUnlock()
	require.Equal(t, `{"keep":"ratio"}`, got)
}

func TestUpdateModelPricingOptionsCheckedAcceptsDisplayOnlyBuiltinInExpected(t *testing.T) {
	useModelPricingSQLite(t)
	builtin, ok := billing_setting.GetBuiltinBillingExpr("gpt-6-astra")
	require.True(t, ok)

	require.NoError(t, UpdateModelPricingOptions(map[string]string{
		"billing_setting.billing_mode": `{}`,
		"billing_setting.billing_expr": `{}`,
		"ModelPrice":                   `{"keep":0.1}`,
	}))

	expected := map[string]string{
		"billing_setting.billing_mode": `{"gpt-6-astra":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"gpt-6-astra":` + jsonQuote(builtin) + `}`,
		"ModelPrice":                   `{"keep":0.1}`,
	}
	updates := map[string]string{
		"billing_setting.billing_mode": `{"keep":"ratio","gpt-6-astra":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"gpt-6-astra":` + jsonQuote(builtin) + `}`,
		"ModelPrice":                   `{"keep":0.25}`,
	}
	require.NoError(t, UpdateModelPricingOptionsChecked(updates, expected))

	var modeRow Option
	require.NoError(t, DB.Where("key = ?", "billing_setting.billing_mode").First(&modeRow).Error)
	var modes map[string]any
	require.NoError(t, json.Unmarshal([]byte(modeRow.Value), &modes))
	require.Equal(t, "ratio", modes["keep"])
	_, hasBuiltin := modes["gpt-6-astra"]
	require.False(t, hasBuiltin)

	var exprRow Option
	require.NoError(t, DB.Where("key = ?", "billing_setting.billing_expr").First(&exprRow).Error)
	var exprs map[string]any
	require.NoError(t, json.Unmarshal([]byte(exprRow.Value), &exprs))
	_, hasBuiltinExpr := exprs["gpt-6-astra"]
	require.False(t, hasBuiltinExpr)

	var priceRow Option
	require.NoError(t, DB.Where("key = ?", "ModelPrice").First(&priceRow).Error)
	var prices map[string]any
	require.NoError(t, json.Unmarshal([]byte(priceRow.Value), &prices))
	require.Equal(t, 0.25, prices["keep"])
}

func jsonQuote(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
