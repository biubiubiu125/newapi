package model

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestPostgresSchemaMigratorImplementsMigrateColumnUnique(t *testing.T) {
	var migrator interface {
		MigrateColumnUnique(value any, field *schema.Field, column gorm.ColumnType) error
	} = postgresSchemaMigrator{}
	require.NotNil(t, migrator)

	body, err := os.ReadFile("migration_dialector.go")
	require.NoError(t, err)
	source := string(body)
	require.Contains(t, source, "func (m postgresSchemaMigrator) MigrateColumnUnique")
	require.Contains(t, source, "cardinality(constraint_meta.conkey) = 1")
}
