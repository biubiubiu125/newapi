package setting

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

var (
	BEpusdtEnabled           bool
	USDTGatewayType          string = "bepusdt"
	BEpusdtBaseURL           string
	BEpusdtPID               string
	BEpusdtSecretKey         string
	BEpusdtCurrency          string = "cny"
	BEpusdtTradeType         string = DefaultBEpusdtTradeType
	BEpusdtDisplayName       string = "USDT"
	BEpusdtAssetDisplayNames string = `{"usdt":"USDT"}`
	BEpusdtMinTopUp          int    = 1
)

const (
	USDTGatewayTypeBEpusdt  = "bepusdt"
	DefaultBEpusdtTradeType = "usdt.trc20"
)

var bepusdtTradeTypeOrder = []string{
	"usdt.trc20",
	"usdt.bep20",
	"usdt.polygon",
	"usdt.xlayer",
}

func NormalizeUSDTGatewayType(value string) string {
	return USDTGatewayTypeBEpusdt
}

func GetUSDTGatewayType() string {
	return NormalizeUSDTGatewayType(USDTGatewayType)
}

func IsAllowedBEpusdtTradeType(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	for _, item := range bepusdtTradeTypeOrder {
		if item == normalized {
			return true
		}
	}
	return false
}

func NormalizeBEpusdtTradeType(value string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return DefaultBEpusdtTradeType, true
	}
	if !IsAllowedBEpusdtTradeType(normalized) {
		return "", false
	}
	return normalized, true
}

func NormalizeBEpusdtTradeTypes(value string) (string, bool) {
	if strings.TrimSpace(value) == "" {
		return DefaultBEpusdtTradeType, true
	}
	seen := make(map[string]struct{}, len(bepusdtTradeTypeOrder))
	for _, part := range strings.Split(value, ",") {
		token := strings.ToLower(strings.TrimSpace(part))
		if token == "" {
			continue
		}
		if !IsAllowedBEpusdtTradeType(token) {
			return "", false
		}
		seen[token] = struct{}{}
	}
	if len(seen) == 0 {
		return "", false
	}
	ordered := make([]string, 0, len(seen))
	for _, item := range bepusdtTradeTypeOrder {
		if _, ok := seen[item]; ok {
			ordered = append(ordered, item)
		}
	}
	return strings.Join(ordered, ","), true
}

func ValidateBEpusdtTradeType(value string) error {
	if _, ok := NormalizeBEpusdtTradeTypes(value); !ok {
		return errors.New("invalid bepusdt trade type")
	}
	return nil
}

func EnabledBEpusdtTradeTypes() ([]string, bool) {
	normalized, ok := NormalizeBEpusdtTradeTypes(BEpusdtTradeType)
	if !ok {
		return nil, false
	}
	return strings.Split(normalized, ","), true
}

func ResolveBEpusdtCheckoutTradeType(paymentMethod string) (string, error) {
	enabled, ok := EnabledBEpusdtTradeTypes()
	if !ok || len(enabled) == 0 {
		return "", errors.New("invalid bepusdt trade type")
	}
	method := strings.ToLower(strings.TrimSpace(paymentMethod))
	if method == "usdt" {
		if len(enabled) == 1 {
			return enabled[0], nil
		}
		return "", errors.New("invalid bepusdt trade type")
	}
	if method == "" {
		return "", errors.New("invalid bepusdt trade type")
	}
	for _, item := range enabled {
		if item == method {
			return item, nil
		}
	}
	return "", errors.New("invalid bepusdt trade type")
}

func GetBEpusdtAssetDisplayNames() map[string]string {
	names := map[string]string{}
	if strings.TrimSpace(BEpusdtAssetDisplayNames) == "" {
		return names
	}
	if err := common.UnmarshalJsonStr(BEpusdtAssetDisplayNames, &names); err != nil {
		return map[string]string{}
	}
	return names
}
