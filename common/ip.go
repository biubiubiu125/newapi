package common

import (
	"net"
	"net/netip"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	trustedProxyHeadersEnv = "TRUSTED_PROXY_HEADERS"
	trustedProxyCidrsEnv   = "TRUSTED_PROXY_CIDRS"
)

var (
	trustedProxyHeaders []string
	trustedProxyCidrs   []netip.Prefix
)

var defaultTrustedProxyHeaders = []string{}

var defaultTrustedProxyCidrs = []string{}

func IsIP(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil
}

func ParseIP(s string) net.IP {
	return net.ParseIP(s)
}

func IsPrivateIP(ip net.IP) bool {
	if ip.IsPrivate() {
		return true
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}

	private := []net.IPNet{
		{IP: net.IPv4(10, 0, 0, 0), Mask: net.CIDRMask(8, 32)},
		{IP: net.IPv4(172, 16, 0, 0), Mask: net.CIDRMask(12, 32)},
		{IP: net.IPv4(192, 168, 0, 0), Mask: net.CIDRMask(16, 32)},
	}

	for _, privateNet := range private {
		if privateNet.Contains(ip) {
			return true
		}
	}
	return false
}

func IsIpInCIDRList(ip net.IP, cidrList []string) bool {
	for _, cidr := range cidrList {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			if whitelistIP := net.ParseIP(cidr); whitelistIP != nil {
				if ip.Equal(whitelistIP) {
					return true
				}
			}
			continue
		}

		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func InitTrustedProxyConfig() {
	trustedProxyHeaders = splitHeaderList(getEnvOrDefaultAllowEmpty(trustedProxyHeadersEnv, strings.Join(defaultTrustedProxyHeaders, ",")))
	trustedProxyCidrs = parseTrustedProxyCidrs(getEnvOrDefaultAllowEmpty(trustedProxyCidrsEnv, strings.Join(defaultTrustedProxyCidrs, ",")))
}

func getEnvOrDefaultAllowEmpty(env string, defaultValue string) string {
	if env == "" {
		return defaultValue
	}
	if value, ok := os.LookupEnv(env); ok {
		return value
	}
	return defaultValue
}

func GetClientIP(c *gin.Context) string {
	if c == nil {
		return ""
	}

	remoteIP := normalizeIP(remoteIPFromRequest(c))
	if IsTrustedProxyIP(remoteIP) {
		if headerIP := clientIPFromTrustedHeaders(c); headerIP != "" {
			return headerIP
		}
	}
	return remoteIP
}

func NormalizeIP(value string) string {
	return normalizeIP(value)
}

func IsTrustedProxyIP(value string) bool {
	ip, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	for _, prefix := range trustedProxyCidrs {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

func clientIPFromTrustedHeaders(c *gin.Context) string {
	for _, header := range trustedProxyHeaders {
		value := strings.TrimSpace(c.GetHeader(header))
		if value == "" {
			continue
		}
		if strings.EqualFold(header, "X-Forwarded-For") {
			if ip := clientIPFromForwardedFor(value); ip != "" {
				return ip
			}
			continue
		}
		if ip := normalizeIP(value); ip != "" {
			return ip
		}
	}
	return ""
}

func clientIPFromForwardedFor(value string) string {
	parts := strings.Split(value, ",")
	fallback := ""
	for i := len(parts) - 1; i >= 0; i-- {
		ip := normalizeIP(parts[i])
		if ip == "" {
			continue
		}
		if fallback == "" {
			fallback = ip
		}
		if !IsTrustedProxyIP(ip) {
			return ip
		}
	}
	return fallback
}

func remoteIPFromRequest(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(c.Request.RemoteAddr))
	if err == nil {
		return host
	}
	return c.Request.RemoteAddr
}

func normalizeIP(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "[") {
		if host, _, err := net.SplitHostPort(value); err == nil {
			value = host
		}
	} else if strings.Count(value, ":") == 1 {
		if host, _, err := net.SplitHostPort(value); err == nil {
			value = host
		}
	}
	ip, err := netip.ParseAddr(value)
	if err != nil {
		return ""
	}
	return ip.String()
}

func splitHeaderList(value string) []string {
	items := strings.Split(value, ",")
	headers := make([]string, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		header := strings.TrimSpace(item)
		if header == "" {
			continue
		}
		key := strings.ToLower(header)
		if seen[key] {
			continue
		}
		seen[key] = true
		headers = append(headers, header)
	}
	return headers
}

func parseTrustedProxyCidrs(value string) []netip.Prefix {
	items := strings.Split(value, ",")
	prefixes := make([]netip.Prefix, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(item); err == nil {
			prefixes = append(prefixes, prefix.Masked())
			continue
		}
		if ip, err := netip.ParseAddr(item); err == nil {
			prefixes = append(prefixes, netip.PrefixFrom(ip, ip.BitLen()))
		}
	}
	return prefixes
}
