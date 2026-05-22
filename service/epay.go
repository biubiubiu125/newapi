package service

import (
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

func GetCallbackAddress() string {
	if callback := strings.TrimSpace(operation_setting.CustomCallbackAddress); callback != "" && !isLocalCallbackAddress(callback) {
		return strings.TrimRight(callback, "/")
	}
	if server := strings.TrimSpace(system_setting.ServerAddress); server != "" && !isLocalCallbackAddress(server) {
		return strings.TrimRight(server, "/")
	}
	return ""
}

func isLocalCallbackAddress(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return true
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "" || host == "localhost" || host == "127.0.0.1" || host == "::1"
}
