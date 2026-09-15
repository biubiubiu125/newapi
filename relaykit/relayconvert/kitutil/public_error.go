package kitutil

import (
	"regexp"
	"strings"
)

const (
	PublicAccountingFailReason = "billing accounting failed after task submission"
	PublicSettlementFailReason = "billing settlement failed before consumption log"
	PublicInternalFailReason   = "task failed"
)

var publicClientLocatorPattern = regexp.MustCompile(`(?i)(?:https?://[^\s]+|data:[^\s]+)`)
var publicClientInternalPattern = regexp.MustCompile(`(?i)(?:\b(?:pq|gorm|sqlstate|sql|redis|postgres|mysql|mongodb|sqlite|dsn|password|authorization|api[_-]?key|access[_-]?token|secret|private[_-]?key)\b|database is closed|no such table|dial tcp|connection refused|unix socket|127\.0\.0\.1|0\.0\.0\.0|localhost|::1|(?:10|127)\.\d{1,3}\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2\d|3[0-1])\.\d{1,3}\.\d{1,3}|[a-z0-9.-]+\.(?:internal|local|lan)\b)`)

// SanitizePublicClientError strips result locators and internal billing/DB
// details from a persisted or host error before it is returned to clients.
func SanitizePublicClientError(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ""
	}
	reason = strings.TrimSpace(publicClientLocatorPattern.ReplaceAllString(reason, ""))
	reason = strings.Join(strings.Fields(reason), " ")
	if reason == "" {
		return ""
	}
	lower := strings.ToLower(reason)
	switch {
	case strings.Contains(lower, PublicAccountingFailReason),
		strings.Contains(lower, "rollback billing after task error"):
		return PublicAccountingFailReason
	case strings.Contains(lower, PublicSettlementFailReason),
		strings.Contains(lower, "settlement review update failed"),
		strings.Contains(lower, "record consume log failed"),
		strings.Contains(lower, "review update failed"),
		strings.Contains(lower, "settlement failed"):
		return PublicSettlementFailReason
	}
	if publicClientInternalPattern.MatchString(reason) {
		return PublicInternalFailReason
	}
	return reason
}
