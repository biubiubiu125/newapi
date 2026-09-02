package model

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestSanitizeDBErrorAddsPostgreSQLPoolerHint(t *testing.T) {
	err := sanitizeDBError(&pgconn.PgError{
		Severity: "FATAL",
		Code:     "08P01",
		Message:  "prepared statement name is already in use: stmt_secret",
	})
	require.Equal(t, "postgres error SQLSTATE 08P01: prepared statement conflict with a transaction-pooling proxy (PgBouncer/Neon/Supabase); other clients sharing this database must disable prepared statements, or upgrade PgBouncer to >=1.21 with max_prepared_statements enabled", err.Error())
	require.NotContains(t, err.Error(), "stmt_secret")
}
