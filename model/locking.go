package model

import (
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func sqliteDatabaseLocked(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") || strings.Contains(message, "sqlite_busy")
}

var userDeleteLocks [64]sync.Mutex

func userDeleteLock(userID int) *sync.Mutex {
	if userID < 0 {
		userID = -userID
	}
	return &userDeleteLocks[userID%len(userDeleteLocks)]
}

// lockForUpdate makes the next query emit SELECT ... FOR UPDATE so the matched
// rows stay locked until the surrounding transaction ends.
//
// GORM v2 silently ignores the legacy `Set("gorm:query_option", "FOR UPDATE")`
// from GORM v1, so that form does not lock anything. Always use this helper
// instead.
//
// SQLite has no FOR UPDATE syntax (the clause would be a syntax error), so it
// is skipped there; SQLite's single-writer model makes one of two conflicting
// transactions fail instead of both committing.
func LockForUpdate(tx *gorm.DB) *gorm.DB {
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		return tx
	}
	return tx.Clauses(clause.Locking{Strength: "UPDATE"})
}

func lockForUpdate(tx *gorm.DB) *gorm.DB {
	return LockForUpdate(tx)
}
