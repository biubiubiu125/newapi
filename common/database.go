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
// SQLitePath is the DSN for the default SQLite database. It uses WAL journal
// mode so readers are never blocked by the single writer, plus a 30s busy
// timeout for writers to queue.
//
// Two details are non-obvious and both are required for concurrent correctness:
//
//  1. The busy timeout must be passed as a `_pragma=busy_timeout(30000)` DSN
//     parameter. The pure-Go driver (modernc.org/sqlite, used through
//     github.com/glebarez/sqlite) silently ignores the plain `_busy_timeout=`
//     form, so without this the effective timeout stays at SQLite's 5s default
//     and concurrent writes surface as "database is locked" (see #6805).
//
//  2. `_txlock=immediate` (BEGIN IMMEDIATE) must be enabled. Without it, a
//     transaction that first SELECTs (establishing a read snapshot) and then
//     writes can hit SQLITE_BUSY_SNAPSHOT when another connection commits in
//     between; the busy handler does not cover that case, so the write fails
//     instantly no matter the timeout. BEGIN IMMEDIATE takes the write lock up
//     front, so writers serialize through the busy timeout instead of dying on
//     a stale snapshot. Autocommit SELECTs stay concurrent because WAL keeps
//     readers unlocked.
