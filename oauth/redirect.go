package oauth

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

func oauthCallbackURI(c *gin.Context, path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if site := oauthPublicSiteURL(); site != "" {
		return site + path
	}
	return ""
}

func oauthCallbackURIOrError(c *gin.Context, path string) (string, error) {
	uri := oauthCallbackURI(c, path)
	if uri == "" {
		return "", fmt.Errorf("oauth server address is not configured")
	}
	return uri, nil
}

func oauthPublicSiteURL() string {
	addr := strings.TrimRight(strings.TrimSpace(system_setting.ServerAddress), "/")
	if addr == "" || isLocalCallbackAddress(addr) {
		return ""
	}
	parsed, err := url.Parse(addr)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return addr
}

func isLocalCallbackAddress(raw string) bool {
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
