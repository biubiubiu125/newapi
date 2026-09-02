package system_setting

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

type ThemeSettings struct {
	Frontend string `json:"frontend"`
}

var themeSettings = ThemeSettings{
	Frontend: "default",
}

func init() {
	config.GlobalConfig.Register("theme", &themeSettings)
	syncThemeToCommon()
}

func syncThemeToCommon() {
	// Keep loading the legacy setting without allowing it to select a removed
	// frontend; common.SetTheme is intentionally a compatibility no-op.
	common.SetTheme(themeSettings.Frontend)
}

func GetThemeSettings() *ThemeSettings {
	return &themeSettings
}

// UpdateAndSyncTheme preserves the legacy config load hook without changing
// the active frontend.
func UpdateAndSyncTheme() {
	syncThemeToCommon()
}
