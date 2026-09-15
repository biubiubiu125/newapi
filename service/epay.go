package service

import (
	"errors"
	"net"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

var ErrPublicCallbackAddressNotConfigured = errors.New("public callback address is not configured")

func GetCallbackAddress() string {
	if callback := strings.TrimSpace(operation_setting.CustomCallbackAddress); callback != "" && !IsLocalCallbackAddress(callback) {
		return strings.TrimRight(callback, "/")
	}
	if server := strings.TrimSpace(system_setting.ServerAddress); server != "" && !IsLocalCallbackAddress(server) {
		return strings.TrimRight(server, "/")
	}
	return ""
}

func RequirePublicCallbackAddress() (string, error) {
	addr := GetCallbackAddress()
	if addr == "" {
		return "", ErrPublicCallbackAddressNotConfigured
	}
	parsed, err := url.Parse(addr)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", ErrPublicCallbackAddressNotConfigured
	}
	return addr, nil
}

func IsLocalCallbackAddress(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return true
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" || host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}
