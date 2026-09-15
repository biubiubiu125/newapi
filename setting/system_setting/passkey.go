package system_setting

import (
	"net"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

type PasskeySettings struct {
	Enabled              bool   `json:"enabled"`
	RPDisplayName        string `json:"rp_display_name"`
	RPID                 string `json:"rp_id"`
	Origins              string `json:"origins"`
	AllowInsecureOrigin  bool   `json:"allow_insecure_origin"`
	UserVerification     string `json:"user_verification"`
	AttachmentPreference string `json:"attachment_preference"`
}

var defaultPasskeySettings = PasskeySettings{
	Enabled:              false,
	RPDisplayName:        common.SystemName,
	RPID:                 "",
	Origins:              "",
	AllowInsecureOrigin:  false,
	UserVerification:     "preferred",
	AttachmentPreference: "",
}

func init() {
	config.GlobalConfig.Register("passkey", &defaultPasskeySettings)
}

func PasskeySettingsSnapshot() PasskeySettings {
	common.OptionMapRWMutex.RLock()
	settings := defaultPasskeySettings
	serverAddress := ServerAddress
	common.OptionMapRWMutex.RUnlock()
	return settings.withDefaults(serverAddress)
}

func GetPasskeySettings() *PasskeySettings {
	snapshot := PasskeySettingsSnapshot()
	return &snapshot
}

func OverridePasskeySettingsForTest(settings PasskeySettings) func() {
	common.OptionMapRWMutex.Lock()
	previous := defaultPasskeySettings
	defaultPasskeySettings = settings
	common.OptionMapRWMutex.Unlock()
	return func() {
		common.OptionMapRWMutex.Lock()
		defaultPasskeySettings = previous
		common.OptionMapRWMutex.Unlock()
	}
}

func (s PasskeySettings) withDefaults(serverAddress string) PasskeySettings {
	serverAddress = strings.TrimSpace(serverAddress)
	if strings.TrimSpace(s.RPID) == "" && serverAddress != "" {
		if parsed, err := url.Parse(serverAddress); err == nil && parsed.Host != "" {
			s.RPID = hostWithoutPort(parsed.Host)
		} else {
			s.RPID = hostWithoutPort(serverAddress)
		}
	} else {
		s.RPID = hostWithoutPort(s.RPID)
	}
	if s.Origins == "" || s.Origins == "[]" {
		s.Origins = serverAddress
	}
	return s
}

func hostWithoutPort(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if hostname, _, err := net.SplitHostPort(host); err == nil {
		return hostname
	}
	return host
}
