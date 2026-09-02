package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/require"
)

// TestFormatUserLogsStripsQuotaSaturation verifies the admin-only quota
// saturation marker (nested under other.admin_info) is removed for non-admin
// log views, since formatUserLogs strips the whole admin_info object.
func TestFormatUserLogsStripsQuotaSaturation(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"model_price": 0.004,
		"admin_info": map[string]interface{}{
			"quota_saturation": map[string]interface{}{
				"op":      "QuotaFromDecimal",
				"kind":    "overflow",
				"clamped": common.MaxQuota,
			},
		},
	})
	logs := []*Log{{Other: other}}

	formatUserLogs(logs, 0)

	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	_, hasAdminInfo := parsed["admin_info"]
	require.False(t, hasAdminInfo, "admin_info (and nested quota_saturation) must be stripped for non-admin views")
	// Non-admin billing fields remain visible.
	require.Contains(t, parsed, "model_price")
}

func TestLogFormattersStripRootDiagnostics(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"root_info":  map[string]interface{}{"secret": "hidden"},
		"admin_info": map[string]interface{}{"operation": "visible-to-admin"},
	})

	userLogs := []*Log{{Other: other}}
	formatUserLogs(userLogs, 0)
	userMap, err := common.StrToMap(userLogs[0].Other)
	require.NoError(t, err)
	require.NotContains(t, userMap, "root_info")
	require.NotContains(t, userMap, "admin_info")

	adminLogs := []*Log{{Other: other}}
	FormatAdminLogs(adminLogs)
	adminMap, err := common.StrToMap(adminLogs[0].Other)
	require.NoError(t, err)
	require.NotContains(t, adminMap, "root_info")
	require.Contains(t, adminMap, "admin_info")
}
