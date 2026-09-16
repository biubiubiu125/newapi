package common

import "strings"

// NormalizeSystemName rewrites leftover New API / RKAPI branding to RK API.
// Custom site names are preserved.
func NormalizeSystemName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "RK API"
	}
	compact := strings.ToLower(strings.Join(strings.Fields(trimmed), ""))
	switch compact {
	case "newapi", "rkapi":
		return "RK API"
	default:
		return trimmed
	}
}
