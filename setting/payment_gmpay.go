package setting

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
)

var (
	GMPayEnabled           bool
	GMPayBaseURL           string
	GMPayPID               string
	GMPaySecretKey         string
	GMPayCurrency          string = "cny"
	GMPayDisplayName       string = "USDT"
	GMPayAssetDisplayNames string = `{"usdt":"USDT","usdt:tron":"USDT-TRC20","usdt:bsc":"USDT-BEP20","usdt:polygon":"USDT-Polygon"}`
	GMPayMinTopUp          int    = 1
)

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
