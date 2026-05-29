package model

import (
	"errors"
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
	previousQuotaForNewUser := common.QuotaForNewUser
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	LOG_DB = db
	common.UsingSQLite = true
	common.QuotaForNewUser = 0
	require.NoError(t, db.AutoMigrate(&User{}, &UserLoginIdentifier{}))
	t.Cleanup(func() {
		DB = previousDB
		LOG_DB = previousLogDB
		common.UsingSQLite = previousUsingSQLite
		common.QuotaForNewUser = previousQuotaForNewUser
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
	require.NoError(t, db.Create(first).Error)
	require.NoError(t, db.Create(second).Error)
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

func TestValidateAndFillAllowsEmailLogin(t *testing.T) {
	setupUserEmailTestDB(t)
	require.NoError(t, ensureUserEmailCanonicalUniqueIndex())

	user := &User{
		Username:    "email-login",
		Password:    "correct-password",
		DisplayName: "email-login",
		Email:       "Owner@Example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, user.Insert(0))

	login := &User{
		Username: " OWNER@example.COM ",
		Password: "correct-password",
	}
	require.NoError(t, login.ValidateAndFill())
	require.Equal(t, user.Id, login.Id)
	require.Equal(t, "email-login", login.Username)
}

func TestValidateAndFillPrefersUsernameBeforeEmail(t *testing.T) {
	db := setupUserEmailTestDB(t)
	require.NoError(t, ensureUserEmailCanonicalUniqueIndex())

	usernameOwner := &User{
		Username:    "shared@example.com",
		Password:    "username-password",
		DisplayName: "username-owner",
		Email:       "username-owner@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, usernameOwner.Insert(0))

	emailOwnerPassword, err := common.Password2Hash("email-password")
	require.NoError(t, err)
	emailOwner := &User{
		Username:    "email-owner",
		Password:    emailOwnerPassword,
		DisplayName: "email-owner",
		Email:       "shared@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, db.Create(emailOwner).Error)

	login := &User{
		Username: "shared@example.com",
		Password: "username-password",
	}
	require.NoError(t, login.ValidateAndFill())
	require.Equal(t, usernameOwner.Id, login.Id)

	ambiguousEmailLogin := &User{
		Username: "shared@example.com",
		Password: "email-password",
	}
	require.ErrorIs(t, ambiguousEmailLogin.ValidateAndFill(), ErrUserPasswordIncorrect)
}

func TestLoginIdentifiersAreGloballyUniqueAcrossUsernameAndEmail(t *testing.T) {
	setupUserEmailTestDB(t)
	require.NoError(t, ensureUserEmailCanonicalUniqueIndex())

	existing := &User{
		Username:    "existing-user",
		Password:    "12345678",
		DisplayName: "existing-user",
		Email:       "owner@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, existing.Insert(0))

	emailAsUsername := &User{
		Username:    "owner@example.com",
		Password:    "12345678",
		DisplayName: "email-as-username",
		Email:       "other@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.ErrorIs(t, emailAsUsername.Insert(0), ErrUserLoginIdentifierTaken)

	usernameAsEmail := &User{
		Username:    "other-user",
		Password:    "12345678",
		DisplayName: "username-as-email",
		Email:       "existing-user",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.ErrorIs(t, usernameAsEmail.Insert(0), ErrUserLoginIdentifierTaken)

	update := User{Id: existing.Id, Username: "renamed-user", Email: "owner@example.com"}
	require.NoError(t, update.Update(false))
}

func TestLoginIdentifiersNormalizeEmailLikeUsernameCase(t *testing.T) {
	setupUserEmailTestDB(t)
	require.NoError(t, ensureUserEmailCanonicalUniqueIndex())

	existing := &User{
		Username:    "email-owner-case",
		Password:    "12345678",
		DisplayName: "email-owner-case",
		Email:       "owner@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, existing.Insert(0))

	emailAsUsername := &User{
		Username:    "OWNER@example.com",
		Password:    "12345678",
		DisplayName: "email-as-username-case",
		Email:       "other-case@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.ErrorIs(t, emailAsUsername.Insert(0), ErrUserLoginIdentifierTaken)
}

func TestLoginIdentifierPrecheckUsesNormalizedIdentifierTable(t *testing.T) {
	db := setupUserEmailTestDB(t)
	require.NoError(t, ensureUserEmailCanonicalUniqueIndex())

	existing := &User{
		Username:    "OWNER@example.com",
		Password:    "12345678",
		DisplayName: "owner",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, db.Create(existing).Error)
	require.NoError(t, syncUserLoginIdentifiersWithTx(db, existing.Id, existing.Username, existing.Email))

	exists, err := IsLoginIdentifierTakenByOther("", "owner@example.com", 0)
	require.NoError(t, err)
	require.True(t, exists)
}

func TestSoftDeletedUserKeepsLoginIdentifiersReserved(t *testing.T) {
	setupUserEmailTestDB(t)
	require.NoError(t, ensureUserEmailCanonicalUniqueIndex())

	deleted := &User{
		Username:    "OWNER@example.com",
		Password:    "12345678",
		DisplayName: "deleted",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, deleted.Insert(0))
	require.NoError(t, deleted.Delete())

	exists, err := IsLoginIdentifierTakenByOther("", "owner@example.com", 0)
	require.NoError(t, err)
	require.True(t, exists)

	reuse := &User{
		Username:    "reuse",
		Password:    "12345678",
		DisplayName: "reuse",
		Email:       "owner@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.ErrorIs(t, reuse.Insert(0), ErrUserLoginIdentifierTaken)
}

func TestLoginIdentifiersRejectSameUserDuplicateUsernameAndEmail(t *testing.T) {
	setupUserEmailTestDB(t)
	require.NoError(t, ensureUserEmailCanonicalUniqueIndex())

	user := &User{
		Username:    "same@example.com",
		Password:    "12345678",
		DisplayName: "same",
		Email:       "SAME@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.ErrorIs(t, user.Insert(0), ErrUserLoginIdentifierTaken)
}

func TestEnsureUserLoginIdentifiersRejectsHistoricalCrossFieldConflict(t *testing.T) {
	db := setupUserEmailTestDB(t)
	require.NoError(t, ensureUserEmailCanonicalUniqueIndex())

	usernameOwner := &User{Username: "taken@example.com", Password: "12345678", DisplayName: "username-owner", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	emailOwner := &User{Username: "email-owner", Password: "12345678", DisplayName: "email-owner", Email: "taken@example.com", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(usernameOwner).Error)
	require.NoError(t, db.Create(emailOwner).Error)

	err := ensureUserLoginIdentifiers()
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate user login identifier exists")
}

func TestLoginIdentifierUniqueIndexRejectsCrossFieldConflict(t *testing.T) {
	db := setupUserEmailTestDB(t)
	require.NoError(t, ensureUserEmailCanonicalUniqueIndex())
	require.NoError(t, ensureUserLoginIdentifiers())

	existing := &User{
		Username:    "index-owner",
		Password:    "12345678",
		DisplayName: "index-owner",
		Email:       "index-owner@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, existing.Insert(0))

	bypassedPrecheck := &User{
		Username:    "index-owner@example.com",
		Password:    "12345678",
		DisplayName: "bypassed-precheck",
		Email:       "other-index@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, db.Create(bypassedPrecheck).Error)

	err := db.Transaction(func(tx *gorm.DB) error {
		return syncUserLoginIdentifiersWithTx(tx, bypassedPrecheck.Id, bypassedPrecheck.Username, bypassedPrecheck.Email)
	})
	require.Error(t, err)
	require.True(t, IsUserEmailUniqueError(err), "expected unique error, got %v", err)
}

func TestUserLoginIdentifierTableErrorIsNotUniqueConflict(t *testing.T) {
	err := errors.New("SQL logic error: no such table: user_login_identifiers (1)")

	require.False(t, IsUserEmailUniqueError(err))
}

func TestMigrateDBCreatesAndSyncsUserLoginIdentifiers(t *testing.T) {
	db := setupUserEmailTestDB(t)
	require.NoError(t, db.Migrator().DropTable(&UserLoginIdentifier{}))

	user := &User{
		Username:    "migrate-owner",
		Password:    "12345678",
		DisplayName: "migrate-owner",
		Email:       "migrate-owner@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, db.Create(user).Error)

	require.NoError(t, migrateDB())
	require.True(t, db.Migrator().HasTable(&UserLoginIdentifier{}))

	var identifiers []UserLoginIdentifier
	require.NoError(t, db.Where("user_id = ?", user.Id).Find(&identifiers).Error)
	require.Len(t, identifiers, 2)
}

func TestInsertPreserveQuotaKeepsExplicitQuota(t *testing.T) {
	setupUserEmailTestDB(t)
	common.QuotaForNewUser = 0

	root := &User{
		Username:    "root",
		Password:    "12345678",
		DisplayName: "Root User",
		Quota:       100000000,
		Role:        common.RoleRootUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, root.InsertPreserveQuota(0))

	var stored User
	require.NoError(t, DB.First(&stored, root.Id).Error)
	require.Equal(t, 100000000, stored.Quota)
}

func TestRechargeCreemBackfillsEmailLoginIdentifiers(t *testing.T) {
	db := setupUserEmailTestDB(t)
	require.NoError(t, db.AutoMigrate(&TopUp{}, &Log{}))
	require.NoError(t, ensureUserEmailCanonicalUniqueIndex())
	require.NoError(t, ensureUserLoginIdentifiers())

	user := &User{
		Username:    "creem-email-sync",
		Password:    "12345678",
		DisplayName: "creem-email-sync",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, user.Insert(0))
	topUp := &TopUp{
		UserId:          user.Id,
		Amount:          123,
		Money:           1,
		TradeNo:         "creem-email-sync-trade",
		PaymentMethod:   PaymentMethodCreem,
		PaymentProvider: PaymentProviderCreem,
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, db.Create(topUp).Error)

	require.NoError(t, RechargeCreem(topUp.TradeNo, " Buyer@Example.COM ", "Buyer", "127.0.0.1"))

	var stored User
	require.NoError(t, db.First(&stored, user.Id).Error)
	require.Equal(t, "buyer@example.com", stored.Email)
	require.NotNil(t, stored.EmailCanonical)
	require.Equal(t, "buyer@example.com", *stored.EmailCanonical)
	require.Equal(t, 123, stored.Quota)

	var identifier UserLoginIdentifier
	require.NoError(t, db.Where("user_id = ? AND identifier = ? AND kind = ?", user.Id, "buyer@example.com", "email").First(&identifier).Error)

	login := &User{
		Username: "BUYER@example.COM",
		Password: "12345678",
	}
	require.NoError(t, login.ValidateAndFill())
	require.Equal(t, user.Id, login.Id)
}

func TestRechargeCreemSkipsConflictingCustomerEmailButCreditsQuota(t *testing.T) {
	db := setupUserEmailTestDB(t)
	require.NoError(t, db.AutoMigrate(&TopUp{}, &Log{}))
	require.NoError(t, ensureUserEmailCanonicalUniqueIndex())
	require.NoError(t, ensureUserLoginIdentifiers())

	owner := &User{
		Username:    "creem-email-owner",
		Password:    "12345678",
		DisplayName: "creem-email-owner",
		Email:       "owner@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	payer := &User{
		Username:    "creem-email-payer",
		Password:    "12345678",
		DisplayName: "creem-email-payer",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, owner.Insert(0))
	require.NoError(t, payer.Insert(0))
	topUp := &TopUp{
		UserId:          payer.Id,
		Amount:          456,
		Money:           1,
		TradeNo:         "creem-email-conflict-trade",
		PaymentMethod:   PaymentMethodCreem,
		PaymentProvider: PaymentProviderCreem,
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, db.Create(topUp).Error)

	require.NoError(t, RechargeCreem(topUp.TradeNo, "OWNER@example.com", "Buyer", "127.0.0.1"))

	var stored User
	require.NoError(t, db.First(&stored, payer.Id).Error)
	require.Empty(t, stored.Email)
	require.Nil(t, stored.EmailCanonical)
	require.Equal(t, 456, stored.Quota)

	var count int64
	require.NoError(t, db.Model(&UserLoginIdentifier{}).Where("user_id = ? AND identifier = ?", payer.Id, "owner@example.com").Count(&count).Error)
	require.Equal(t, int64(0), count)

	login := &User{
		Username: "owner@example.com",
		Password: "12345678",
	}
	require.NoError(t, login.ValidateAndFill())
	require.Equal(t, owner.Id, login.Id)
}
