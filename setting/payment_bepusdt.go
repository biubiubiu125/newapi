package setting

import (
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
	BEpusdtDisplayName       string = "USDT"
	BEpusdtAssetDisplayNames string = `{"usdt":"USDT"}`
	BEpusdtMinTopUp          int    = 1
)

const (
	USDTGatewayTypeBEpusdt = "bepusdt"
)

func NormalizeUSDTGatewayType(value string) string {
	return USDTGatewayTypeBEpusdt
}

func GetUSDTGatewayType() string {
	return NormalizeUSDTGatewayType(USDTGatewayType)
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
