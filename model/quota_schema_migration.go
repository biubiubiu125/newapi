package model

import (
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type quotaSchemaColumn struct {
	table  string
	column string
	model  any
}

var walletQuotaSchemaColumns = []quotaSchemaColumn{
	{table: "users", column: "quota", model: &User{}},
	{table: "users", column: "used_quota", model: &User{}},
	{table: "users", column: "aff_quota", model: &User{}},
	{table: "users", column: "aff_history", model: &User{}},
	{table: "tokens", column: "remain_quota", model: &Token{}},
	{table: "tokens", column: "used_quota", model: &Token{}},
	{table: "redemptions", column: "quota", model: &Redemption{}},
	{table: "top_ups", column: "credit_quota_snapshot", model: &TopUp{}},
	{table: "tasks", column: "quota", model: &Task{}},
	{table: "logs", column: "quota", model: &Log{}},
}

// ValidateQuotaSchema checks the columns that can carry wallet or credited
// quota. Missing tables/columns are ignored so this can run before the normal
// AutoMigrate pass on a fresh installation.
func ValidateQuotaSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("validate quota schema: database is nil")
	}

	for _, column := range walletQuotaSchemaColumns {
		if !db.Migrator().HasTable(column.model) || !db.Migrator().HasColumn(column.model, column.column) {
			continue
		}
		columnType, err := db.Migrator().ColumnTypes(column.model)
		if err != nil {
			return fmt.Errorf("inspect %s.%s quota column: %w", column.table, column.column, err)
		}
		for _, candidate := range columnType {
			if candidate.Name() != column.column {
				continue
			}
			databaseType := strings.ToUpper(strings.TrimSpace(candidate.DatabaseTypeName()))
			if !quotaDatabaseTypeIsWideEnough(databaseType, db.Dialector.Name()) {
				return fmt.Errorf(
					"incompatible quota schema for %s.%s: database type %q cannot safely hold int64 wallet values",
					column.table,
					column.column,
					databaseType,
				)
			}
			break
		}
	}
	return nil
}

// MigrateQuotaSchema widens existing quota columns without changing values.
// It is intentionally explicit instead of relying on dialect-specific
// AutoMigrate behavior, which may leave an old INT column untouched.
func MigrateQuotaSchema(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("migrate quota schema: database is nil")
	}

	switch db.Dialector.Name() {
	case "sqlite":
		// SQLite INTEGER already uses signed 64-bit storage. AutoMigrate keeps
		// the model contract explicit for newly created tables.
		if err := db.AutoMigrate(&User{}, &Token{}, &Redemption{}, &TopUp{}, &Task{}, &Log{}); err != nil {
			return fmt.Errorf("migrate SQLite quota schema: %w", err)
		}
	case "postgres":
		if err := migratePostgresQuotaSchema(db); err != nil {
			return err
		}
	case "mysql":
		if err := migrateMySQLQuotaSchema(db); err != nil {
			return err
		}
	default:
		return fmt.Errorf("migrate quota schema: unsupported database %q", db.Dialector.Name())
	}

	return ValidateQuotaSchema(db)
}

func migratePostgresQuotaSchema(db *gorm.DB) error {
	for _, column := range walletQuotaSchemaColumns {
		if !db.Migrator().HasTable(column.model) || !db.Migrator().HasColumn(column.model, column.column) {
			continue
		}
		columnType, err := findQuotaSchemaColumnType(db, column)
		if err != nil {
			return err
		}
		if columnType == nil || !quotaColumnNeedsWidening(columnType, "postgres") {
			continue
		}
		table := clause.Table{Name: column.table}
		identifier := clause.Column{Name: column.column}
		statement := fmt.Sprintf(
			"ALTER TABLE %s ALTER COLUMN %s TYPE BIGINT USING %s::bigint",
			quotePostgresIdentifier(table.Name),
			quotePostgresIdentifier(identifier.Name),
			quotePostgresIdentifier(identifier.Name),
		)
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("migrate PostgreSQL %s.%s to BIGINT: %w", column.table, column.column, err)
		}
	}
	return nil
}

func migrateMySQLQuotaSchema(db *gorm.DB) error {
	for _, column := range walletQuotaSchemaColumns {
		if !db.Migrator().HasTable(column.model) || !db.Migrator().HasColumn(column.model, column.column) {
			continue
		}
		columnTypes, err := db.Migrator().ColumnTypes(column.model)
		if err != nil {
			return fmt.Errorf("inspect %s.%s quota column: %w", column.table, column.column, err)
		}
		var columnType gorm.ColumnType
		for _, candidate := range columnTypes {
			if candidate.Name() == column.column {
				columnType = candidate
				break
			}
		}
		if columnType == nil {
			continue
		}
		if !quotaColumnNeedsWidening(columnType, "mysql") {
			continue
		}
		statement := fmt.Sprintf(
			"ALTER TABLE %s MODIFY COLUMN %s %s",
			quoteMySQLIdentifier(column.table),
			quoteMySQLIdentifier(column.column),
			mysqlQuotaColumnDefinition(columnType),
		)
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("migrate MySQL %s.%s to BIGINT: %w", column.table, column.column, err)
		}
	}
	return nil
}

func mysqlQuotaColumnDefinition(column gorm.ColumnType) string {
	definition := "BIGINT"
	if nullable, ok := column.Nullable(); ok {
		if nullable {
			definition += " NULL"
		} else {
			definition += " NOT NULL"
		}
	}
	if defaultValue, ok := column.DefaultValue(); ok {
		definition += " DEFAULT " + mysqlDefaultExpression(defaultValue)
	}
	if autoIncrement, ok := column.AutoIncrement(); ok && autoIncrement {
		definition += " AUTO_INCREMENT"
	}
	if comment, ok := column.Comment(); ok && comment != "" {
		definition += " COMMENT '" + strings.ReplaceAll(comment, "'", "''") + "'"
	}
	return definition
}

func mysqlDefaultExpression(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "NULL"
	}
	if strings.EqualFold(value, "NULL") ||
		strings.HasPrefix(strings.ToUpper(value), "CURRENT_TIMESTAMP") {
		return value
	}
	if _, err := strconv.ParseFloat(value, 64); err == nil {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func findQuotaSchemaColumnType(db *gorm.DB, column quotaSchemaColumn) (gorm.ColumnType, error) {
	columnTypes, err := db.Migrator().ColumnTypes(column.model)
	if err != nil {
		return nil, fmt.Errorf("inspect %s.%s quota column: %w", column.table, column.column, err)
	}
	for _, candidate := range columnTypes {
		if candidate.Name() == column.column {
			return candidate, nil
		}
	}
	return nil, nil
}

func quotaColumnNeedsWidening(column gorm.ColumnType, dialect string) bool {
	if column == nil {
		return false
	}
	return !quotaDatabaseTypeIsWideEnough(
		strings.ToUpper(strings.TrimSpace(column.DatabaseTypeName())),
		dialect,
	)
}

func quotaDatabaseTypeIsWideEnough(databaseType, dialect string) bool {
	databaseType = strings.ToUpper(strings.TrimSpace(databaseType))
	switch dialect {
	case "sqlite":
		return databaseType == "" || strings.Contains(databaseType, "INT")
	case "postgres":
		// PostgreSQL stores BIGINT as INT8; GORM may report either name.
		return databaseType == "BIGINT" || databaseType == "INT8"
	default:
		return strings.HasPrefix(databaseType, "BIGINT") &&
			!strings.Contains(databaseType, "UNSIGNED")
	}
}

func quotePostgresIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func quoteMySQLIdentifier(identifier string) string {
	return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
}
