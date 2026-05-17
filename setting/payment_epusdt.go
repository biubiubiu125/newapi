package setting

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
)

var (
	EpusdtEnabled           bool
	EpusdtBaseURL           string
	EpusdtPID               string
	EpusdtSecretKey         string
	EpusdtCurrency          string = "cny"
	EpusdtDisplayName       string = "USDT"
	EpusdtAssetDisplayNames string = `{"usdt:tron":"USDT-TRC20","usdt:polygon":"USDT-Polygon","usdt:bsc":"USDT-BEP20","usdt:bep20":"USDT-BEP20"}`
	EpusdtMinTopUp          int    = 1
)

func GetEpusdtAssetDisplayNames() map[string]string {
	names := map[string]string{}
	if strings.TrimSpace(EpusdtAssetDisplayNames) == "" {
		return names
	}
	if err := common.UnmarshalJsonStr(EpusdtAssetDisplayNames, &names); err != nil {
		return map[string]string{}
	}
	return names
}
