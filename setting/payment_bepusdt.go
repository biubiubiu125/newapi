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

var allowedBEpusdtTradeTypes = map[string]struct{}{
	"usdt.trc20":    {},
	"usdt.erc20":    {},
	"usdt.polygon":  {},
	"usdt.bep20":    {},
	"usdt.aptos":    {},
	"usdt.solana":   {},
	"usdt.xlayer":   {},
	"usdt.arbitrum": {},
	"usdt.plasma":   {},
	"usdt.ton":      {},
}

func NormalizeUSDTGatewayType(value string) string {
	return USDTGatewayTypeBEpusdt
}

func GetUSDTGatewayType() string {
	return NormalizeUSDTGatewayType(USDTGatewayType)
}

func IsAllowedBEpusdtTradeType(value string) bool {
	_, ok := allowedBEpusdtTradeTypes[strings.ToLower(strings.TrimSpace(value))]
	return ok
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

func ValidateBEpusdtTradeType(value string) error {
	if _, ok := NormalizeBEpusdtTradeType(value); !ok {
		return errors.New("invalid bepusdt trade type")
	}
	return nil
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
