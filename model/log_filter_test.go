package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func seedLogFilterRows(t *testing.T) {
	t.Helper()
	truncateTables(t)
	require.NoError(t, LOG_DB.Exec("DELETE FROM logs").Error)
	rows := []*Log{
		{
			UserId:    1,
			Username:  "alice",
			Type:      LogTypeConsume,
			ModelName: "gpt-4o",
			TokenName: "prod-key",
			Quota:     10,
		},
		{
			UserId:    2,
			Username:  "alice-smith",
			Type:      LogTypeConsume,
			ModelName: "gpt-4o-mini",
			TokenName: "prod-key-v2",
			Quota:     20,
		},
		{
			UserId:    3,
			Username:  "bob",
			Type:      LogTypeConsume,
			ModelName: "claude-3-5",
			TokenName: "prod-key",
			Quota:     30,
		},
	}
	require.NoError(t, LOG_DB.Create(&rows).Error)
}

func TestGetAllLogsUsesExactTextFiltersUnlessWildcardIsExplicit(t *testing.T) {
	seedLogFilterRows(t)

	logs, total, err := GetAllLogs(LogTypeConsume, 0, 0, "gpt-4o", "alice", "", 0, 10, 0, "", "", "")

	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, logs, 1)
	require.Equal(t, "gpt-4o", logs[0].ModelName)
	require.Equal(t, "alice", logs[0].Username)
}

func TestGetAllLogsAllowsExplicitWildcardTextFilters(t *testing.T) {
	seedLogFilterRows(t)

	logs, total, err := GetAllLogs(LogTypeConsume, 0, 0, "gpt-4o%", "alice%", "", 0, 10, 0, "", "", "")

	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, logs, 2)
}

func TestGetAllLogsKeepsTokenNameExact(t *testing.T) {
	seedLogFilterRows(t)

	logs, total, err := GetAllLogs(LogTypeConsume, 0, 0, "", "", "prod-key", 0, 10, 0, "", "", "")

	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, logs, 2)
	for _, log := range logs {
		require.Equal(t, "prod-key", log.TokenName)
	}
}
