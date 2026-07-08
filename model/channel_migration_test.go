package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelAutoMigrateUsesOpenAIOrganizationColumnName(t *testing.T) {
	setupRiskCleanupTestDB(t)

	require.NoError(t, DB.AutoMigrate(&Channel{}))
	require.True(t, DB.Migrator().HasColumn(&Channel{}, "openai_organization"))
	require.False(t, DB.Migrator().HasColumn(&Channel{}, "open_ai_organization"))
}

func TestEnsureChannelOpenAIOrganizationColumnMigratesLegacyColumn(t *testing.T) {
	setupRiskCleanupTestDB(t)

	require.NoError(t, DB.Exec(`
CREATE TABLE channels (
	id integer primary key,
	open_ai_organization text
)`).Error)
	require.NoError(t, DB.Exec(
		"INSERT INTO channels (id, open_ai_organization) VALUES (?, ?)",
		1,
		"org-legacy",
	).Error)
	require.False(t, DB.Migrator().HasColumn(&Channel{}, "openai_organization"))

	require.NoError(t, ensureChannelOpenAIOrganizationColumn())
	require.True(t, DB.Migrator().HasColumn(&Channel{}, "openai_organization"))

	var organization string
	require.NoError(t, DB.Raw(
		"SELECT openai_organization FROM channels WHERE id = ?",
		1,
	).Scan(&organization).Error)
	require.Equal(t, "org-legacy", organization)

	require.NoError(t, ensureChannelOpenAIOrganizationColumn())
}
