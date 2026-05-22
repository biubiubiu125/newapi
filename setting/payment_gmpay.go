package setting

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
)

var (
	GMPayEnabled           bool
	USDTGatewayType        string = "bepusdt"
	GMPayBaseURL           string
	GMPayPID               string
	GMPaySecretKey         string
	GMPayCurrency          string = "cny"
	GMPayDisplayName       string = "USDT"
	GMPayAssetDisplayNames string = `{"usdt":"USDT"}`
	GMPayMinTopUp          int    = 1
)

const (
	USDTGatewayTypeGMPay   = "gmpay"
	USDTGatewayTypeBEpusdt = "bepusdt"
)

func NormalizeUSDTGatewayType(value string) string {
	return USDTGatewayTypeBEpusdt
}

func GetUSDTGatewayType() string {
	return NormalizeUSDTGatewayType(USDTGatewayType)
}

func GetGMPayAssetDisplayNames() map[string]string {
	names := map[string]string{}
	if strings.TrimSpace(GMPayAssetDisplayNames) == "" {
		return names
	}
	if err := common.UnmarshalJsonStr(GMPayAssetDisplayNames, &names); err != nil {
		return map[string]string{}
	}
	return names
}
