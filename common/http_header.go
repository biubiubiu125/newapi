package common

import "strings"

// IsSensitiveRequestHeader reports whether a request header can carry credentials or session secrets.
func IsSensitiveRequestHeader(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	switch normalized {
	case "authorization", "proxy-authorization", "x-api-key", "api-key", "x-goog-api-key", "mj-api-secret", "cookie", "set-cookie":
		return true
	}
	if strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "credential") ||
		strings.Contains(normalized, "api-key") ||
		strings.Contains(normalized, "apikey") ||
		strings.Contains(normalized, "private-key") ||
		strings.Contains(normalized, "access-key") {
		return true
	}
	return normalized == "token" || strings.HasSuffix(normalized, "-token")
}
