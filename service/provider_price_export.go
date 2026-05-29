package service

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

const ProviderPriceOverridesOptionKey = "ProviderPriceOverrides"
const ProviderPriceExportSchemaVersion = "1.0"

type ProviderPriceOverride struct {
	ID                 string   `json:"id"`
	GroupName          string   `json:"group_name"`
	ModelName          string   `json:"model_name"`
	InputPrice         *float64 `json:"input_price,omitempty"`
	OutputPrice        *float64 `json:"output_price,omitempty"`
	CacheInputPrice    *float64 `json:"cache_input_price,omitempty"`
	CacheCreatePrice   *float64 `json:"cache_create_price,omitempty"`
	CacheCreatePrice1h *float64 `json:"cache_create_price_1h,omitempty"`
	ImageOutputPrice   *float64 `json:"image_output_price,omitempty"`
	Enabled            bool     `json:"enabled"`
	Note               string   `json:"note,omitempty"`
	SortOrder          int      `json:"sort_order"`
}

type ProviderPriceExportData struct {
	Currency   string                     `json:"currency"`
	PriceUnit  string                     `json:"price_unit"`
	SiteName   string                     `json:"site_name,omitempty"`
	SiteDomain string                     `json:"site_domain,omitempty"`
	UpdatedAt  string                     `json:"updated_at"`
	Models     []ProviderPriceExportModel `json:"models"`
}

type ProviderPriceExportModel struct {
	GroupName          string   `json:"group_name"`
	ModelName          string   `json:"model_name"`
	InputPrice         *float64 `json:"input_price"`
	OutputPrice        *float64 `json:"output_price"`
	CacheInputPrice    *float64 `json:"cache_input_price"`
	CacheCreatePrice   *float64 `json:"cache_create_price"`
	CacheCreatePrice1h *float64 `json:"cache_create_price_1h"`
	Enabled            bool     `json:"enabled"`
	Note               string   `json:"note"`
}

func normalizeProviderPriceOverrides(items []ProviderPriceOverride) []ProviderPriceOverride {
	if len(items) == 0 {
		return nil
	}

	normalized := make([]ProviderPriceOverride, 0, len(items))
	seenIDs := make(map[string]int, len(items))
	for idx, item := range items {
		item.ID = strings.TrimSpace(item.ID)
		if item.ID == "" {
			item.ID = fmt.Sprintf("provider-price-%d", idx+1)
		}
		item.GroupName = strings.TrimSpace(item.GroupName)
		item.ModelName = strings.TrimSpace(item.ModelName)
		item.Note = strings.TrimSpace(item.Note)
		if item.GroupName == "" || item.ModelName == "" {
			continue
		}
		baseID := item.ID
		for suffix := 2; seenIDs[strings.ToLower(item.ID)] > 0; suffix++ {
			item.ID = fmt.Sprintf("%s-%d", baseID, suffix)
		}
		seenIDs[strings.ToLower(item.ID)]++
		normalized = append(normalized, item)
	}

	sort.SliceStable(normalized, func(i, j int) bool {
		if normalized[i].SortOrder == normalized[j].SortOrder {
			if strings.EqualFold(normalized[i].GroupName, normalized[j].GroupName) {
				return strings.ToLower(normalized[i].ModelName) < strings.ToLower(normalized[j].ModelName)
			}
			return strings.ToLower(normalized[i].GroupName) < strings.ToLower(normalized[j].GroupName)
		}
		return normalized[i].SortOrder < normalized[j].SortOrder
	})

	return normalized
}

func hasAnyProviderPriceValue(item ProviderPriceOverride) bool {
	for _, value := range []*float64{
		item.InputPrice,
		item.OutputPrice,
		item.CacheInputPrice,
		item.CacheCreatePrice,
		item.CacheCreatePrice1h,
		item.ImageOutputPrice,
	} {
		if value != nil && *value > 0 {
			return true
		}
	}
	return false
}

func hasRequiredProviderInputPrice(item ProviderPriceOverride) bool {
	return item.InputPrice != nil && *item.InputPrice > 0
}

func providerPriceExportOutputPrice(item ProviderPriceOverride) *float64 {
	if item.OutputPrice != nil && *item.OutputPrice >= 0 {
		return item.OutputPrice
	}
	if item.InputPrice == nil {
		return nil
	}
	value := *item.InputPrice
	return &value
}

func providerPriceExportOptionalPrice(value *float64) *float64 {
	if value == nil || *value < 0 {
		return nil
	}
	return value
}

func parseProviderPriceOverrides(raw string) []ProviderPriceOverride {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}
	var items []ProviderPriceOverride
	if err := common.UnmarshalJsonStr(raw, &items); err != nil {
		return nil
	}
	return normalizeProviderPriceOverrides(items)
}

func marshalProviderPriceOverrides(items []ProviderPriceOverride) (string, error) {
	normalized := normalizeProviderPriceOverrides(items)
	if len(normalized) == 0 {
		return "[]", nil
	}
	data, err := common.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("marshal provider price overrides: %w", err)
	}
	return string(data), nil
}

func GetProviderPriceOverrides() ([]ProviderPriceOverride, error) {
	common.OptionMapRWMutex.RLock()
	raw := strings.TrimSpace(common.OptionMap[ProviderPriceOverridesOptionKey])
	common.OptionMapRWMutex.RUnlock()
	if raw == "" {
		return nil, nil
	}
	return parseProviderPriceOverrides(raw), nil
}

func UpdateProviderPriceOverrides(items []ProviderPriceOverride) ([]ProviderPriceOverride, error) {
	payload, err := marshalProviderPriceOverrides(items)
	if err != nil {
		return nil, err
	}
	if err := model.UpdateOption(ProviderPriceOverridesOptionKey, payload); err != nil {
		return nil, err
	}
	return parseProviderPriceOverrides(payload), nil
}

func GetPublicProviderPriceExport() (*ProviderPriceExportData, error) {
	items, err := GetProviderPriceOverrides()
	if err != nil {
		return nil, err
	}
	models := make([]ProviderPriceExportModel, 0, len(items))
	seenModelGroups := make(map[string]struct{}, len(items))
	for _, item := range items {
		if !hasRequiredProviderInputPrice(item) {
			continue
		}
		modelGroupKey := strings.ToLower(item.ModelName) + "\x00" + strings.ToLower(item.GroupName)
		if _, exists := seenModelGroups[modelGroupKey]; exists {
			continue
		}
		seenModelGroups[modelGroupKey] = struct{}{}
		models = append(models, ProviderPriceExportModel{
			GroupName:          item.GroupName,
			ModelName:          item.ModelName,
			InputPrice:         item.InputPrice,
			OutputPrice:        providerPriceExportOutputPrice(item),
			CacheInputPrice:    providerPriceExportOptionalPrice(item.CacheInputPrice),
			CacheCreatePrice:   providerPriceExportOptionalPrice(item.CacheCreatePrice),
			CacheCreatePrice1h: providerPriceExportOptionalPrice(item.CacheCreatePrice1h),
			Enabled:            item.Enabled,
			Note:               item.Note,
		})
	}
	return &ProviderPriceExportData{
		Currency:   "CNY",
		PriceUnit:  "per_1m_tokens",
		SiteName:   strings.TrimSpace(common.SystemName),
		SiteDomain: providerPriceExportSiteDomain(),
		UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
		Models:     models,
	}, nil
}

func providerPriceExportSiteDomain() string {
	raw := strings.TrimSpace(system_setting.ServerAddress)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if parsed.Host != "" {
		return parsed.Host
	}
	return raw
}
