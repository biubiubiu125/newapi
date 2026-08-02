package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExternalIdentityAlreadyTakenWhenMultipleRowsMatch(t *testing.T) {
	db := setupUserEmailTestDB(t)

	for _, user := range []*User{
		{
			Username:   "external-owner-one",
			Email:      "external-owner-one@example.com",
			Password:   "password123",
			Role:       common.RoleCommonUser,
			Status:     common.UserStatusEnabled,
			GitHubId:   "github-duplicate",
			DiscordId:  "discord-duplicate",
			OidcId:     "oidc-duplicate",
			WeChatId:   "wechat-duplicate",
			TelegramId: "telegram-duplicate",
		},
		{
			Username:   "external-owner-two",
			Email:      "external-owner-two@example.com",
			Password:   "password123",
			Role:       common.RoleCommonUser,
			Status:     common.UserStatusEnabled,
			GitHubId:   "github-duplicate",
			DiscordId:  "discord-duplicate",
			OidcId:     "oidc-duplicate",
			WeChatId:   "wechat-duplicate",
			TelegramId: "telegram-duplicate",
		},
	} {
		require.NoError(t, db.Create(user).Error)
	}

	assert.True(t, IsGitHubIdAlreadyTaken("github-duplicate"))
	assert.True(t, IsDiscordIdAlreadyTaken("discord-duplicate"))
	assert.True(t, IsOidcIdAlreadyTaken("oidc-duplicate"))
	assert.True(t, IsWeChatIdAlreadyTaken("wechat-duplicate"))
	assert.True(t, IsTelegramIdAlreadyTaken("telegram-duplicate"))
}

func TestOidcIdentityRemainsReservedAfterSoftDelete(t *testing.T) {
	db := setupUserEmailTestDB(t)
	user := &User{
		Username: "oidc-deleted-owner",
		Email:    "oidc-deleted-owner@example.com",
		Password: "password123",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		OidcId:   "oidc-deleted",
	}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Delete(user).Error)

	assert.True(t, IsOidcIdAlreadyTaken("oidc-deleted"))
}
