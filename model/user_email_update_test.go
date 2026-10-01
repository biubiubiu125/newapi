package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUpdateUserEmailDoesNotRestoreStaleRoleStatusGroupOrBinding(t *testing.T) {
	setupUserUpdateTestState(t)
	user := User{
		Username: "email-snapshot-user",
		Password: "password",
		Email:    "old@example.com",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		GitHubId: "gh-old",
		Quota:    12345,
		AffCode:  "email-snapshot-aff",
	}
	require.NoError(t, DB.Create(&user).Error)

	sqlDB, err := DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(2)
	t.Cleanup(func() {
		sqlDB.SetMaxOpenConns(1)
	})

	fired := false
	callbackName := "test_update_user_email_snapshot"
	require.NoError(t, DB.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if fired || tx.Statement.Table != "users" {
			return
		}
		fired = true
		require.NoError(t, tx.Session(&gorm.Session{NewDB: true, SkipHooks: true}).Exec(
			`UPDATE users SET role = ?, status = ?, "group" = ?, github_id = ? WHERE id = ?`,
			common.RoleAdminUser,
			common.UserStatusDisabled,
			"vip",
			"",
			user.Id,
		).Error)
	}))
	t.Cleanup(func() {
		DB.Callback().Query().Remove(callbackName)
	})

	require.NoError(t, UpdateUserEmail(user.Id, "new@example.com"))
	require.True(t, fired)

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	require.Equal(t, "new@example.com", got.Email)
	require.Equal(t, common.RoleAdminUser, got.Role)
	require.Equal(t, common.UserStatusDisabled, got.Status)
	require.Equal(t, "vip", got.Group)
	require.Empty(t, got.GitHubId)
	require.Equal(t, int64(12345), got.Quota)
}

func TestClaimExternalIdentityKeepsRoleStatusGroupAndOtherBindings(t *testing.T) {
	setupUserUpdateTestState(t)
	user := User{
		Username:  "claim-snapshot-user",
		Password:  "password",
		Role:      common.RoleAdminUser,
		Status:    common.UserStatusDisabled,
		Group:     "vip",
		DiscordId: "discord-keep",
		Quota:     42,
		AffCode:   "claim-snapshot-aff",
	}
	require.NoError(t, DB.Create(&user).Error)

	require.NoError(t, (&User{Id: user.Id}).ClaimExternalIdentity(ExternalIdentityProviderGitHub, "gh-new"))

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	require.Equal(t, common.RoleAdminUser, got.Role)
	require.Equal(t, common.UserStatusDisabled, got.Status)
	require.Equal(t, "vip", got.Group)
	require.Equal(t, "discord-keep", got.DiscordId)
	require.Equal(t, "gh-new", got.GitHubId)
	require.Equal(t, int64(42), got.Quota)
}
