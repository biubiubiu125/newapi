package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestAuthVersionBumpRevokesDashboardAccessToken(t *testing.T) {
	setupUserUpdateTestState(t)

	const token = "dashboard-token-auth-version-01"
	user := User{
		Username:    "auth-version-token-user",
		Password:    "password",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AuthVersion: 1,
		AffCode:     "auth-version-token-aff",
	}
	user.SetAccessToken(token)
	createdAt := int64(1_700_000_000)
	user.AccessTokenCreatedAt = &createdAt
	require.NoError(t, DB.Create(&user).Error)

	next, err := BumpUserAuthVersion(user.Id)
	require.NoError(t, err)
	require.EqualValues(t, 2, next)

	authenticated, err := ValidateAccessToken(token)
	require.NoError(t, err)
	require.Nil(t, authenticated)

	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	require.Empty(t, stored.GetAccessToken())
	require.Nil(t, stored.AccessTokenCreatedAt)
	require.EqualValues(t, 2, stored.AuthVersion)
}

func TestAuthVersionBumpRollbackKeepsDashboardAccessToken(t *testing.T) {
	setupUserUpdateTestState(t)

	const token = "dashboard-token-auth-rollback-01"
	user := User{
		Username:    "auth-version-rollback-user",
		Password:    "password",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AuthVersion: 1,
		AffCode:     "auth-version-rollback-aff",
	}
	user.SetAccessToken(token)
	require.NoError(t, DB.Create(&user).Error)

	tx := DB.Begin()
	require.NoError(t, tx.Error)
	_, err := IncrementUserAuthVersionWithTx(tx, user.Id)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback().Error)

	authenticated, err := ValidateAccessToken(token)
	require.NoError(t, err)
	require.NotNil(t, authenticated)
	require.Equal(t, user.Id, authenticated.Id)

	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	require.Equal(t, token, stored.GetAccessToken())
	require.EqualValues(t, 1, stored.AuthVersion)
}
