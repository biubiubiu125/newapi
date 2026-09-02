package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestThemeAwarePathAlwaysRewritesLegacyConsoleRoutes(t *testing.T) {
	SetTheme("classic")
	assert.Equal(t, "default", GetTheme())
	assert.Equal(t, "/tickets?ticket_id=1", ThemeAwarePath("/console/tickets?ticket_id=1"))
	assert.Equal(t, "/admin-tickets", ThemeAwarePath("/console/admin-tickets"))
	assert.Equal(t, "/wallet", ThemeAwarePath("/console/topup"))
	assert.Equal(t, "/usage-logs", ThemeAwarePath("/console/log"))
	assert.Equal(t, "/profile", ThemeAwarePath("/console/personal"))
}

func TestSQLitePathUsesConcurrentWritePragmas(t *testing.T) {
	assert.Contains(t, SQLitePath, "_pragma=busy_timeout(30000)")
	assert.Contains(t, SQLitePath, "_pragma=journal_mode(WAL)")
	assert.Contains(t, SQLitePath, "_txlock=immediate")
}
