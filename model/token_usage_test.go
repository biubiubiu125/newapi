package model

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestTokenUsageUpdateAssignmentsUseQualifiedColumnsForPostgres(t *testing.T) {
	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  "host=localhost user=test dbname=test password=test sslmode=disable",
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		DryRun:                true,
		DisableAutomaticPing:  true,
		SkipDefaultTransaction: true,
	})
	require.NoError(t, err)

	usage := &TokenUsageDaily{
		TokenId:      1,
		Date:         "2026-06-17",
		UserId:       2,
		Quota:        100,
		RequestCount: 1,
		LastUsedAt:   1781719200,
		CreatedAt:    1781719200,
		UpdatedAt:    1781719200,
	}
	updates := tokenUsageUpdateAssignments(usage.UserId, usage.Quota, usage.RequestCount, usage.UpdatedAt, true)

	tx := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "token_id"}, {Name: "date"}},
		DoUpdates: clause.Assignments(updates),
	}).Create(usage)

	require.NoError(t, tx.Error)
	sql := tx.Statement.SQL.String()
	require.Contains(t, sql, `"token_usage_dailies"."last_used_at"`)
	require.Contains(t, sql, `"token_usage_dailies"."quota"`)
	require.Contains(t, sql, `"token_usage_dailies"."request_count"`)
	require.NotContains(t, sql, "CASE WHEN last_used_at <")
	require.NotContains(t, sql, "quota +")
	require.NotContains(t, sql, "request_count +")
}
