package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUserEmailTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := DB
	previousLogDB := LOG_DB
	previousUsingSQLite := common.UsingSQLite
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	LOG_DB = db
	common.UsingSQLite = true
	require.NoError(t, db.AutoMigrate(&User{}))
	t.Cleanup(func() {
		DB = previousDB
		LOG_DB = previousLogDB
		common.UsingSQLite = previousUsingSQLite
	})
	return db
}

func TestEnsureUserEmailCanonicalUniqueIndexBackfillsAndEnforcesUniqueness(t *testing.T) {
	db := setupUserEmailTestDB(t)

	user := &User{
		Username:    "email-owner",
		Password:    "12345678",
		DisplayName: "email-owner",
		Email:       "Owner@Example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, user.Insert(0))
	require.NoError(t, db.Model(&User{}).Where("id = ?", user.Id).Update("email_canonical", nil).Error)

	require.NoError(t, ensureUserEmailCanonicalUniqueIndex())

	var canonical string
	require.NoError(t, db.Model(&User{}).Select("email_canonical").Where("id = ?", user.Id).Scan(&canonical).Error)
	require.Equal(t, "owner@example.com", canonical)

	duplicate := &User{
		Username:    "email-dupe",
		Password:    "12345678",
		DisplayName: "email-dupe",
		Email:       "OWNER@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.Error(t, duplicate.Insert(0))
}

func TestEnsureUserEmailCanonicalUniqueIndexRejectsExistingDuplicates(t *testing.T) {
	db := setupUserEmailTestDB(t)

	first := &User{Username: "first", Password: "12345678", DisplayName: "first", Email: "same@example.com", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	second := &User{Username: "second", Password: "12345678", DisplayName: "second", Email: "SAME@example.com", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, first.Insert(0))
	require.NoError(t, second.Insert(0))
	require.NoError(t, db.Model(&User{}).Where("id IN ?", []int{first.Id, second.Id}).Update("email_canonical", nil).Error)

	err := ensureUserEmailCanonicalUniqueIndex()
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate user email exists")
}

func TestClearEmailBindingAlsoClearsCanonicalEmail(t *testing.T) {
	db := setupUserEmailTestDB(t)
	require.NoError(t, ensureUserEmailCanonicalUniqueIndex())

	user := &User{
		Username:    "clear-email",
		Password:    "12345678",
		DisplayName: "clear-email",
		Email:       "clear@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, user.Insert(0))
	require.NoError(t, user.ClearBinding("email"))

	var stored User
	require.NoError(t, db.First(&stored, user.Id).Error)
	require.Empty(t, stored.Email)
	require.Nil(t, stored.EmailCanonical)

	reused := &User{
		Username:    "reuse-email",
		Password:    "12345678",
		DisplayName: "reuse-email",
		Email:       "clear@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, reused.Insert(0))
}
