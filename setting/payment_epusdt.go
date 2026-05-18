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
	EpusdtAssetDisplayNames string = `{"usdt":"USDT","usdt:tron":"USDT-TRC20","usdt:bsc":"USDT-BEP20","usdt:polygon":"USDT-Polygon"}`
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
