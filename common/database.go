package common

type DatabaseType string

const (
	DatabaseTypeMySQL      DatabaseType = "mysql"
	DatabaseTypeSQLite     DatabaseType = "sqlite"
	DatabaseTypePostgreSQL DatabaseType = "postgres"
	DatabaseTypeClickHouse DatabaseType = "clickhouse"
)

var UsingSQLite = false
var UsingPostgreSQL = false
var LogSqlType = DatabaseTypeSQLite // Default to SQLite for logging SQL queries
var UsingMySQL = false
var UsingClickHouse = false

var mainDatabaseType = DatabaseTypeSQLite
var logDatabaseType = DatabaseTypeSQLite

func MainDatabaseType() DatabaseType {
	switch {
	case UsingMySQL:
		return DatabaseTypeMySQL
	case UsingPostgreSQL:
		return DatabaseTypePostgreSQL
	case UsingSQLite:
		return DatabaseTypeSQLite
	default:
		return mainDatabaseType
	}
}

func LogDatabaseType() DatabaseType {
	if UsingClickHouse {
		return DatabaseTypeClickHouse
	}
	if LogSqlType != "" {
		return LogSqlType
	}
	return logDatabaseType
}

func SetMainDatabaseType(databaseType DatabaseType) {
	mainDatabaseType = databaseType
	UsingMySQL = databaseType == DatabaseTypeMySQL
	UsingPostgreSQL = databaseType == DatabaseTypePostgreSQL
	UsingSQLite = databaseType == DatabaseTypeSQLite
}

func SetLogDatabaseType(databaseType DatabaseType) {
	logDatabaseType = databaseType
	LogSqlType = databaseType
	UsingClickHouse = databaseType == DatabaseTypeClickHouse
}

func SetDatabaseTypes(mainType DatabaseType, logType DatabaseType) {
	SetMainDatabaseType(mainType)
	SetLogDatabaseType(logType)
}

func UsingMainDatabase(databaseType DatabaseType) bool {
	return MainDatabaseType() == databaseType
}

func UsingLogDatabase(databaseType DatabaseType) bool {
	return LogDatabaseType() == databaseType
}

// SQLitePath is the DSN for the default SQLite database. WAL keeps readers
// concurrent with the single writer; immediate transactions avoid stale
// read-then-write snapshots; the busy timeout lets writers queue briefly.
var SQLitePath = "one-api.db?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate"
