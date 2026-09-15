package oauth

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/service"
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
	if addr == "" || service.IsLocalCallbackAddress(addr) {
		return ""
	}
	parsed, err := url.Parse(addr)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return addr
}
