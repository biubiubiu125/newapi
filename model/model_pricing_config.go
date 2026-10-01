package model

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PricingValues is one model's configuration, keyed by the existing option
// names. A missing key inherits the engine's default; an explicit zero is free.
type PricingValues map[string]any

type ModelPricingChange struct {
	ModelName       string        `json:"model_name"`
	ExpectedVersion string        `json:"expected_version"`
	Pricing         PricingValues `json:"pricing"`
	Reset           bool          `json:"reset,omitempty"`
}

type ModelPricingPluginVariant struct {
	PluginKey     string                               `json:"plugin_key"`
	PluginName    string                               `json:"plugin_name"`
	Icon          string                               `json:"icon,omitempty"`
	UsageSchema   map[string]jsplugin.UsageFieldSchema `json:"usage_schema"`
	UsageExamples []jsplugin.UsageExample              `json:"usage_examples,omitempty"`
	Configured    string                               `json:"configured"`
	Effective     string                               `json:"effective"`
	Compatible    bool                                 `json:"compatible"`
	Stale         bool                                 `json:"stale,omitempty"`
}

type ModelPricingEntry struct {
	ModelName      string                               `json:"model_name"`
	Version        string                               `json:"version"`
	Configured     PricingValues                        `json:"configured"`
	Effective      PricingValues                        `json:"effective"`
	UsageSchema    map[string]jsplugin.UsageFieldSchema `json:"usage_schema,omitempty"`
	PluginVariants []ModelPricingPluginVariant          `json:"plugin_variants,omitempty"`
}

type ModelPricingSnapshot struct {
	Entries      []ModelPricingEntry `json:"entries"`
	Options      map[string]string   `json:"options"`
	EmptyVersion string              `json:"empty_version"`
}

var ErrModelPricingConflict = errors.New("model pricing changed; reload before saving")

// Lock order is stable across instances. Creating missing option rows inside
// the transaction also serializes the first write to an unconfigured database.
var modelPricingOptionKeys = []string{
	"AudioCompletionRatio", "AudioRatio", "CacheRatio", "CompletionRatio",
	"CreateCacheRatio", "ImageRatio", "ModelPrice", "ModelRatio",
	"billing_setting.billing_expr", "billing_setting.billing_mode", billing_setting.PluginBillingExprOption,
}

var modelPricingMutationMu sync.Mutex

func IsModelPricingOption(key string) bool {
	for _, candidate := range modelPricingOptionKeys {
		if key == candidate {
			return true
		}
	}
	return false
}

func CanonicalBillingMode(value any) (string, bool) {
	mode, ok := value.(string)
	if !ok {
		return "", false
	}
	switch mode {
	case billing_setting.BillingModeRatio, "per-request", "per-token":
		return billing_setting.BillingModeRatio, true
	case billing_setting.BillingModeTieredExpr:
		return billing_setting.BillingModeTieredExpr, true
	default:
		return mode, false
	}
}

func canonicalizeBillingModeEntries(entries map[string]any) {
	for name, mode := range entries {
		if canonical, ok := CanonicalBillingMode(mode); ok {
			entries[name] = canonical
		}
	}
}

func canonicalizePricingValues(values PricingValues) {
	if values == nil {
		return
	}
	if mode, exists := values["billing_setting.billing_mode"]; exists {
		if canonical, ok := CanonicalBillingMode(mode); ok {
			values["billing_setting.billing_mode"] = canonical
		}
	}
}

func applyJSONOptionMaps(
	keys []string,
	values map[string]map[string]any,
	apply func(string, string) error,
) error {
	var first error
	failedKeys := make([]string, 0)
	for _, key := range keys {
		entries, ok := values[key]
		if !ok {
			continue
		}
		encoded, err := common.Marshal(entries)
		if err != nil {
			if first == nil {
				first = err
			}
			failedKeys = append(failedKeys, key)
			continue
		}
		if err := apply(key, string(encoded)); err != nil {
			if first == nil {
				first = err
			}
			failedKeys = append(failedKeys, key)
		}
	}
	if len(failedKeys) == 0 {
		return first
	}
	var retryErr error
	for _, key := range failedKeys {
		encoded, err := common.Marshal(values[key])
		if err != nil {
			if retryErr == nil {
				retryErr = err
			}
			continue
		}
		if err := apply(key, string(encoded)); err != nil {
			if retryErr == nil {
				retryErr = err
			}
		}
	}
	return retryErr
}

func ModelPricingVersion(values PricingValues) string {
	encoded, _ := common.Marshal(values)
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}

func defaultPricingMaps() map[string]map[string]any {
	result := make(map[string]map[string]any, len(modelPricingOptionKeys))
	for _, key := range modelPricingOptionKeys {
		result[key] = make(map[string]any)
	}
	for key, values := range ratio_setting.GetDefaultPricingMaps() {
		for name, value := range values {
			result[key][name] = value
		}
	}
	return result
}

func readModelPricingMaps(db *gorm.DB) (map[string]map[string]any, map[string]bool, []string, error) {
	var rows []Option
	if err := db.Where(commonKeyCol+" IN ?", modelPricingOptionKeys).Find(&rows).Error; err != nil {
		return nil, nil, nil, err
	}
	values := defaultPricingMaps()
	existing := make(map[string]bool)
	counts := make(map[string]int)
	for _, row := range rows {
		var entries map[string]any
		if err := common.UnmarshalJsonStr(row.Value, &entries); err != nil {
			return nil, nil, nil, fmt.Errorf("%s: %w", row.Key, err)
		}
		if entries == nil {
			return nil, nil, nil, fmt.Errorf("%s must be a JSON object", row.Key)
		}
		values[row.Key] = entries
		existing[row.Key] = true
		counts[row.Key]++
	}
	var duplicated []string
	for _, key := range modelPricingOptionKeys {
		if counts[key] > 1 {
			duplicated = append(duplicated, key)
		}
	}
	return values, existing, duplicated, nil
}

func modelPricingValues(values map[string]map[string]any, name string) PricingValues {
	result := make(PricingValues)
	for _, key := range modelPricingOptionKeys {
		if key == billing_setting.PluginBillingExprOption {
			variants := make(map[string]any)
			for variant, expression := range values[key] {
				if plugin, modelName, ok := billing_setting.SplitPluginBillingExprKey(variant); ok && modelName == name {
					variants[plugin] = expression
				}
			}
			if len(variants) > 0 {
				result[key] = variants
			}
			continue
		}
		if value, exists := values[key][name]; exists {
			result[key] = value
		}
	}
	return result
}

func effectiveModelPricing(values map[string]map[string]any, name string) PricingValues {
	result := modelPricingValues(values, name)
	// Legacy wildcard aliases are resolved by the same normalization as relay.
	alias := ratio_setting.FormatMatchingModelName(name)
	for _, key := range modelPricingOptionKeys[:8] {
		if value, exists := values[key][alias]; exists {
			result[key] = value
		}
	}
	mode, _ := result["billing_setting.billing_mode"].(string)
	if mode == "" {
		_, hasPrice := result["ModelPrice"]
		_, hasRatio := result["ModelRatio"]
		if _, builtin := billing_setting.GetBuiltinBillingExpr(name); builtin && !hasPrice && !hasRatio {
			mode = "tiered_expr"
		}
	}
	if mode == "tiered_expr" {
		result["billing_setting.billing_mode"] = mode
		if _, exists := result["billing_setting.billing_expr"]; !exists {
			if expression, ok := billing_setting.GetBuiltinBillingExpr(name); ok {
				result["billing_setting.billing_expr"] = expression
			}
		}
		return result
	}
	if _, exists := result["ModelPrice"]; exists {
		return result
	}
	if _, exists := result["ModelRatio"]; !exists && operation_setting.SelfUseModeEnabled {
		result["ModelRatio"] = float64(37.5)
	}
	// Completion ratios include engine-enforced model defaults. The draft is
	// complete: omitted fields use that default, not a discarded saved ratio.
	var configuredCompletion *float64
	if ratio, exists := result["CompletionRatio"].(float64); exists {
		configuredCompletion = &ratio
	}
	result["CompletionRatio"] = ratio_setting.ResolveCompletionRatio(name, configuredCompletion).Ratio
	for key, fallback := range map[string]float64{
		"CacheRatio":       ratio_setting.DefaultCacheRatio,
		"CreateCacheRatio": ratio_setting.DefaultCreateCacheRatio,
		"ImageRatio":       ratio_setting.DefaultImageRatio,
	} {
		if _, exists := result[key]; !exists {
			result[key] = fallback
		}
	}
	return result
}

func GetModelPricingSnapshot(names []string) (*ModelPricingSnapshot, error) {
	values, _, _, err := readModelPricingMaps(DB)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		nameSet := make(map[string]bool)
		for key, entries := range values {
			for name := range entries {
				if key == billing_setting.PluginBillingExprOption {
					_, modelName, ok := billing_setting.SplitPluginBillingExprKey(name)
					if !ok {
						continue
					}
					name = modelName
				}
				nameSet[name] = true
			}
		}
		for name := range billing_setting.GetBuiltinBillingExprCopy() {
			nameSet[name] = true
		}
		for name := range nameSet {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	result := &ModelPricingSnapshot{Entries: make([]ModelPricingEntry, 0, len(names)), Options: make(map[string]string), EmptyVersion: ModelPricingVersion(PricingValues{})}
	generation := jsplugin.DefaultRegistry.Generation()
	for _, name := range names {
		configured := modelPricingValues(values, name)
		entry := ModelPricingEntry{ModelName: name, Version: ModelPricingVersion(configured), Configured: configured, Effective: effectiveModelPricing(values, name)}
		if plugin, ok := generation.GetByModel(name); ok {
			entry.UsageSchema, _ = plugin.Meta.UsageForModel(name)
		} else if target, ok := ResolveTaskModelAlias(generation, name); ok {
			if plugin, ok := generation.Get(target.PluginKey); ok {
				entry.UsageSchema, _ = plugin.Meta.UsageForModel(target.Declared)
			}
		}
		plugins := generation.PluginsByModel(name)
		configuredVariants, _ := configured[billing_setting.PluginBillingExprOption].(map[string]any)
		if len(plugins) >= 2 || len(configuredVariants) > 0 {
			keys := make(map[string]bool, len(plugins)+len(configuredVariants))
			for _, plugin := range plugins {
				keys[plugin.Meta.Key] = true
			}
			for key := range configuredVariants {
				keys[key] = true
			}
			for _, key := range slices.Sorted(maps.Keys(keys)) {
				configuredValue, overridden := configuredVariants[key]
				configuredExpr, _ := configuredValue.(string)
				plugin, exists := generation.Get(key)
				if !exists || !slices.Contains(plugin.Meta.Models, name) {
					variant := ModelPricingPluginVariant{
						PluginKey: key, PluginName: key, Configured: configuredExpr,
						UsageSchema: map[string]jsplugin.UsageFieldSchema{}, Stale: true,
					}
					if exists {
						variant.PluginName, variant.Icon = plugin.Meta.Name, plugin.Meta.Icon
					}
					entry.PluginVariants = append(entry.PluginVariants, variant)
					continue
				}
				schema, examples := plugin.Meta.UsageForModel(name)
				if schema == nil {
					schema = map[string]jsplugin.UsageFieldSchema{}
				}
				expression := configuredExpr
				if !overridden && entry.Effective["billing_setting.billing_mode"] == billing_setting.BillingModeTieredExpr {
					expression, _ = entry.Effective["billing_setting.billing_expr"].(string)
				}
				entry.PluginVariants = append(entry.PluginVariants, ModelPricingPluginVariant{
					PluginKey: plugin.Meta.Key, PluginName: plugin.Meta.Name, Icon: plugin.Meta.Icon,
					UsageSchema: schema, UsageExamples: examples, Configured: configuredExpr, Effective: expression,
					Compatible: billing_setting.TaskExprCompatible(expression, schema),
				})
			}
		}
		result.Entries = append(result.Entries, entry)
	}
	// Preserve the existing settings editor's full-map interface. Built-in
	// expressions are display defaults only; per-model writes do not persist them.
	for name, expression := range billing_setting.GetBuiltinBillingExprCopy() {
		effective := effectiveModelPricing(values, name)
		if effective["billing_setting.billing_mode"] != "tiered_expr" {
			continue
		}
		if _, ok := values["billing_setting.billing_mode"][name]; !ok {
			values["billing_setting.billing_mode"][name] = "tiered_expr"
		}
		if _, ok := values["billing_setting.billing_expr"][name]; !ok {
			values["billing_setting.billing_expr"][name] = expression
		}
	}
	for key, entries := range values {
		encoded, err := common.Marshal(entries)
		if err != nil {
			return nil, err
		}
		result.Options[key] = string(encoded)
	}
	return result, nil
}

func replaceModelPricing(values map[string]map[string]any, name string, draft PricingValues) {
	for _, key := range modelPricingOptionKeys {
		if key == billing_setting.PluginBillingExprOption {
			if values[key] == nil {
				values[key] = map[string]any{}
			}
			for variant := range values[key] {
				if _, model, ok := billing_setting.SplitPluginBillingExprKey(variant); ok && model == name {
					delete(values[key], variant)
				}
			}
			variants, _ := draft[key].(map[string]any)
			for plugin, expr := range variants {
				values[key][billing_setting.PluginBillingExprKey(plugin, name)] = expr
			}
			continue
		}
		delete(values[key], name)
		if value, exists := draft[key]; exists {
			values[key][name] = value
		}
	}
}

func PreviewModelPricing(name string, draft PricingValues) (PricingValues, error) {
	if draft == nil {
		return nil, errors.New("pricing draft is required")
	}
	values, _, _, err := readModelPricingMaps(DB)
	if err != nil {
		return nil, err
	}
	if err := ValidateModelPricing(name, draft); err != nil {
		return nil, err
	}
	replaceModelPricing(values, name, draft)
	return effectiveModelPricing(values, name), nil
}

func ValidateModelPricing(name string, values PricingValues) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("model name is required")
	}
	for key, value := range values {
		if !IsModelPricingOption(key) {
			return fmt.Errorf("unsupported pricing field: %s", key)
		}
		if key == "billing_setting.billing_mode" {
			if _, ok := CanonicalBillingMode(value); !ok {
				return errors.New("invalid billing mode")
			}
			continue
		}
		if key == "billing_setting.billing_expr" {
			expression, ok := value.(string)
			if !ok || strings.TrimSpace(expression) == "" {
				return errors.New("billing expression is required")
			}
			if err := validateSharedModelBillingExpr(name, expression, values); err != nil {
				return fmt.Errorf("model %s: %w", name, err)
			}
			continue
		}
		if key == billing_setting.PluginBillingExprOption {
			if err := validatePluginBillingExprs(name, value); err != nil {
				return err
			}
			continue
		}
		number, ok := value.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
			return fmt.Errorf("%s must be a finite, non-negative number", key)
		}
	}
	if values["billing_setting.billing_mode"] == "tiered_expr" {
		if _, exists := values["billing_setting.billing_expr"]; !exists {
			if _, builtin := billing_setting.GetBuiltinBillingExpr(name); !builtin {
				return errors.New("billing expression is required")
			}
		}
	}
	return nil
}

func unchangedBillingExpr(name, expression string) bool {
	previous, ok := billing_setting.GetBillingExpr(name)
	return ok && previous == expression
}

func validateSharedModelBillingExpr(name, expression string, values PricingValues) error {
	generation := jsplugin.DefaultRegistry.Generation()
	overrides, _ := values[billing_setting.PluginBillingExprOption].(map[string]any)
	plugins := generation.PluginsByModel(name)
	if len(plugins) == 0 {
		if target, resolved := ResolveTaskModelAlias(generation, name); resolved {
			shared := len(generation.PluginsByModel(target.Declared)) >= 2
			if !shared && unchangedBillingExpr(name, expression) {
				return nil
			}
			if plugin, ok := generation.Get(target.PluginKey); ok {
				schema, _ := plugin.Meta.UsageForModel(target.Declared)
				return billing_setting.SmokeTestTaskExpr(expression, schema)
			}
		}
		if unchangedBillingExpr(name, expression) {
			return nil
		}
		return billing_setting.SmokeTestExpr(expression)
	}
	if len(plugins) < 2 && unchangedBillingExpr(name, expression) {
		return nil
	}
	checked := false
	for _, plugin := range plugins {
		if _, skip := overrides[plugin.Meta.Key]; skip {
			continue
		}
		schema, _ := plugin.Meta.UsageForModel(name)
		if err := billing_setting.SmokeTestTaskExpr(expression, schema); err != nil {
			return fmt.Errorf("plugin %s: %w", plugin.Meta.Key, err)
		}
		checked = true
	}
	if checked {
		return nil
	}
	return billing_setting.SmokeTestExpr(expression)
}

func validatePluginBillingExprs(modelName string, value any) error {
	variants, ok := value.(map[string]any)
	if !ok || variants == nil {
		return errors.New("plugin billing expressions must be an object")
	}
	generation := jsplugin.DefaultRegistry.Generation()
	known := make(map[string]*jsplugin.LoadedPlugin)
	for _, plugin := range generation.PluginsByModel(modelName) {
		known[plugin.Meta.Key] = plugin
	}
	for pluginKey, raw := range variants {
		plugin, exists := known[pluginKey]
		if !exists {
			// A removed provider stays in the stored map until an editor replaces it.
			// Preview and unrelated price saves must keep working.
			continue
		}
		expression, ok := raw.(string)
		if !ok || strings.TrimSpace(expression) == "" {
			return fmt.Errorf("plugin %s billing expression must be a string", pluginKey)
		}
		if len(known) < 2 {
			if previous, stored := billing_setting.GetPluginBillingExpr(pluginKey, modelName); stored && previous == expression {
				continue
			}
		}
		schema, _ := plugin.Meta.UsageForModel(modelName)
		if err := billing_setting.SmokeTestTaskExpr(expression, schema); err != nil {
			return fmt.Errorf("plugin %s: %w", pluginKey, err)
		}
	}
	return nil
}

func UpdateModelPricing(changes []ModelPricingChange) error {
	if len(changes) == 0 {
		return errors.New("select model pricing changes before saving")
	}
	seen := make(map[string]bool)
	for _, change := range changes {
		if seen[change.ModelName] {
			return errors.New("duplicate model pricing change")
		}
		seen[change.ModelName] = true
		if change.ExpectedVersion == "" {
			return ErrModelPricingConflict
		}
		canonicalizePricingValues(change.Pricing)
		if err := ValidateModelPricing(change.ModelName, change.Pricing); err != nil {
			return err
		}
	}
	return mutateModelPricingOptions(func(_ *gorm.DB, values map[string]map[string]any) error {
		previous := map[string]map[string]any{
			"billing_setting.billing_mode": cloneStringAnyMap(values["billing_setting.billing_mode"]),
			"billing_setting.billing_expr": cloneStringAnyMap(values["billing_setting.billing_expr"]),
		}
		defaults := defaultPricingMaps()
		for _, change := range changes {
			if ModelPricingVersion(modelPricingValues(values, change.ModelName)) != change.ExpectedVersion {
				return fmt.Errorf("%w: %s", ErrModelPricingConflict, change.ModelName)
			}
			pricing := change.Pricing
			if change.Reset {
				pricing = modelPricingValues(defaults, change.ModelName)
			}
			if err := rejectChangedUndeclaredPluginPricing(change.ModelName, values, pricing); err != nil {
				return err
			}
			replaceModelPricing(values, change.ModelName, pricing)
		}
		stripDisplayOnlyBuiltinPricing(previous, values)
		return nil
	})
}

// UpdateModelPricingOptions keeps legacy single-option callers on the same
// locking, validation and transaction path as the model-level API.
func UpdateModelPricingOptions(updates map[string]string) error {
	return updateModelPricingOptions(updates, nil)
}

// ValidatePluginBillingExprReplacement checks a full legacy plugin-expression
// map against the locked pricing rows. modelName names the first affected model.
func ValidatePluginBillingExprReplacement(expressions map[string]string) (string, error) {
	values, _, _, err := readModelPricingMaps(DB)
	if err != nil {
		return "", err
	}
	raw, err := common.Marshal(expressions)
	if err != nil {
		return "", err
	}
	before := cloneStringAnyMap(values[billing_setting.PluginBillingExprOption])
	err = applyModelPricingOptionUpdates(map[string]string{
		billing_setting.PluginBillingExprOption: string(raw),
	}, values)
	if err != nil {
		return changedPluginBillingModel(before, expressions), err
	}
	return "", nil
}

func changedPluginBillingModel(before map[string]any, expressions map[string]string) string {
	seen := make(map[string]bool)
	for key := range before {
		if _, modelName, ok := billing_setting.SplitPluginBillingExprKey(key); ok {
			seen[modelName] = true
		}
	}
	for key := range expressions {
		if _, modelName, ok := billing_setting.SplitPluginBillingExprKey(key); ok {
			seen[modelName] = true
		}
	}
	names := slices.Sorted(maps.Keys(seen))
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func rejectChangedUndeclaredPluginPricing(name string, stored map[string]map[string]any, next PricingValues) error {
	generation := jsplugin.DefaultRegistry.Generation()
	known := make(map[string]bool)
	for _, plugin := range generation.PluginsByModel(name) {
		known[plugin.Meta.Key] = true
	}
	current := modelPricingValues(stored, name)
	currentVariants, _ := current[billing_setting.PluginBillingExprOption].(map[string]any)
	nextVariants, _ := next[billing_setting.PluginBillingExprOption].(map[string]any)
	for plugin, raw := range nextVariants {
		if known[plugin] {
			continue
		}
		if fmt.Sprint(currentVariants[plugin]) == fmt.Sprint(raw) {
			continue
		}
		return fmt.Errorf("plugin %s does not declare this model", plugin)
	}
	return nil
}

func UpdateModelPricingOptionsChecked(updates, expected map[string]string) error {
	if expected == nil {
		return fmt.Errorf("%w", ErrModelPricingConflict)
	}
	return updateModelPricingOptions(updates, expected)
}

func updateModelPricingOptions(updates, expected map[string]string) error {
	return mutateModelPricingOptions(func(_ *gorm.DB, values map[string]map[string]any) error {
		if expected != nil {
			if err := checkExpectedModelPricingOptions(expected, updates, values); err != nil {
				return err
			}
		}
		return applyModelPricingOptionUpdates(updates, values)
	})
}

func modelPricingValidationName(key, name string) string {
	if key != billing_setting.PluginBillingExprOption {
		return name
	}
	if _, modelName, ok := billing_setting.SplitPluginBillingExprKey(name); ok {
		return modelName
	}
	return name
}

func applyModelPricingOptionUpdates(updates map[string]string, values map[string]map[string]any) error {
	previous := map[string]map[string]any{
		"billing_setting.billing_mode": cloneStringAnyMap(values["billing_setting.billing_mode"]),
		"billing_setting.billing_expr": cloneStringAnyMap(values["billing_setting.billing_expr"]),
	}
	names := make(map[string]bool)
	for key, raw := range updates {
		if !IsModelPricingOption(key) {
			return fmt.Errorf("unsupported pricing field: %s", key)
		}
		var entries map[string]any
		if err := common.UnmarshalJsonStr(raw, &entries); err != nil {
			return err
		}
		if entries == nil {
			return fmt.Errorf("%s must be a JSON object", key)
		}
		for name := range values[key] {
			names[modelPricingValidationName(key, name)] = true
		}
		values[key] = entries
		if key == "billing_setting.billing_mode" {
			canonicalizeBillingModeEntries(entries)
		}
		for name := range entries {
			if key == billing_setting.PluginBillingExprOption {
				if _, _, ok := billing_setting.SplitPluginBillingExprKey(name); !ok {
					return fmt.Errorf("invalid plugin billing expression key: %s", name)
				}
			}
			names[modelPricingValidationName(key, name)] = true
		}
	}
	stripDisplayOnlyBuiltinPricing(previous, values)
	for name := range names {
		err := ValidateModelPricing(name, modelPricingValues(values, name))
		if err == nil {
			continue
		}
		// A provider can disappear while its expression is still stored. Saving an
		// unrelated price, or deleting that stale override, must not re-litigate it.
		if billingExprUnchanged(previous, values, name) && strings.Contains(err.Error(), "no task plugin usage schema") {
			continue
		}
		return err
	}
	return nil
}

func billingExprUnchanged(previous, values map[string]map[string]any, name string) bool {
	before, _ := previous["billing_setting.billing_expr"][name].(string)
	after, _ := values["billing_setting.billing_expr"][name].(string)
	return before == after
}

func checkExpectedModelPricingOptions(expected, updates map[string]string, current map[string]map[string]any) error {
	for key := range updates {
		raw, ok := expected[key]
		if !ok {
			return fmt.Errorf("%w", ErrModelPricingConflict)
		}
		var want map[string]any
		if err := common.UnmarshalJsonStr(raw, &want); err != nil {
			return fmt.Errorf("%w", ErrModelPricingConflict)
		}
		if want == nil {
			want = map[string]any{}
		}
		got := cloneStringAnyMap(current[key])
		stripDisplayOnlyBuiltinFromExpected(key, want, got)
		if !modelPricingJSONEqual(want, got) {
			return fmt.Errorf("%w", ErrModelPricingConflict)
		}
	}
	return nil
}

func stripDisplayOnlyBuiltinPricing(previous, values map[string]map[string]any) {
	prevMode := previous["billing_setting.billing_mode"]
	prevExpr := previous["billing_setting.billing_expr"]
	inMode := values["billing_setting.billing_mode"]
	inExpr := values["billing_setting.billing_expr"]
	if inMode == nil {
		inMode = map[string]any{}
		values["billing_setting.billing_mode"] = inMode
	}
	if inExpr == nil {
		inExpr = map[string]any{}
		values["billing_setting.billing_expr"] = inExpr
	}
	for name, builtinExpr := range billing_setting.GetBuiltinBillingExprCopy() {
		if _, ok := prevMode[name]; ok {
			continue
		}
		if _, ok := prevExpr[name]; ok {
			continue
		}
		mode, _ := inMode[name].(string)
		expr, _ := inExpr[name].(string)
		if mode != "" && mode != billing_setting.BillingModeTieredExpr {
			continue
		}
		if expr != "" && expr != builtinExpr {
			continue
		}
		delete(inMode, name)
		delete(inExpr, name)
	}
}

func stripDisplayOnlyBuiltinFromExpected(key string, want, current map[string]any) {
	if key != "billing_setting.billing_mode" && key != "billing_setting.billing_expr" {
		return
	}
	for name, builtinExpr := range billing_setting.GetBuiltinBillingExprCopy() {
		if _, exists := current[name]; exists {
			continue
		}
		if key == "billing_setting.billing_mode" {
			mode, _ := want[name].(string)
			if mode == "" || mode == billing_setting.BillingModeTieredExpr {
				delete(want, name)
			}
			continue
		}
		expr, _ := want[name].(string)
		if expr == "" || expr == builtinExpr {
			delete(want, name)
		}
	}
}

func cloneStringAnyMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func modelPricingJSONEqual(left, right map[string]any) bool {
	if left == nil {
		left = map[string]any{}
	}
	if right == nil {
		right = map[string]any{}
	}
	encodedLeft, errLeft := common.Marshal(left)
	encodedRight, errRight := common.Marshal(right)
	return errLeft == nil && errRight == nil && string(encodedLeft) == string(encodedRight)
}

func mutateModelPricingOptions(mutate func(*gorm.DB, map[string]map[string]any) error) error {
	modelPricingMutationMu.Lock()
	defer modelPricingMutationMu.Unlock()
	var committed map[string]map[string]any
	err := DB.Transaction(func(tx *gorm.DB) error {
		values, existing, duplicated, err := readModelPricingMaps(lockForUpdate(tx))
		if err != nil {
			return err
		}
		if len(duplicated) > 0 {
			common.SysError("options table has duplicate pricing keys [" + strings.Join(duplicated, ", ") + "]; the table is missing a primary key")
		}
		defaults := defaultPricingMaps()
		for _, key := range modelPricingOptionKeys {
			if existing[key] {
				continue
			}
			encoded, err := common.Marshal(defaults[key])
			if err != nil {
				return err
			}
			row := Option{Key: key, Value: string(encoded)}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
				return err
			}
			values[key] = defaults[key]
		}
		if err := mutate(tx, values); err != nil {
			return err
		}
		for _, key := range modelPricingOptionKeys {
			encoded, err := common.Marshal(values[key])
			if err != nil {
				return err
			}
			if err := tx.Model(&Option{}).Where(commonKeyCol+" = ?", key).Update("value", string(encoded)).Error; err != nil {
				return err
			}
		}
		committed = values
		return nil
	})
	if err != nil {
		return err
	}
	applyErr := applyCommittedModelPricing(committed)
	if applyErr != nil {
		return applyErr
	}
	RefreshPricing()
	ratio_setting.InvalidateExposedDataCache()
	return nil
}

var applyCommittedModelPricing = func(committed map[string]map[string]any) error {
	return applyJSONOptionMaps(modelPricingOptionKeys, committed, updateOptionMap)
}
