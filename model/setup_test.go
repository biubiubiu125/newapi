package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestCheckSetupDoesNotAutoCompleteWhenRootExistsWithoutSetupRow(t *testing.T) {
	db := setupUserEmailTestDB(t)
	require.NoError(t, db.AutoMigrate(&Setup{}))

	previous := constant.Setup
	t.Cleanup(func() { constant.Setup = previous })

	hashed, err := common.Password2Hash("oldpassword")
	require.NoError(t, err)
	require.NoError(t, db.Create(&User{
		Username: "root",
		Password: hashed,
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
	}).Error)

	constant.Setup = true
	CheckSetup()
	require.False(t, constant.Setup)
	require.Nil(t, GetSetup())
}

func TestCheckSetupMarksInitializedWhenSetupRowExists(t *testing.T) {
	db := setupUserEmailTestDB(t)
	require.NoError(t, db.AutoMigrate(&Setup{}))

	previous := constant.Setup
	t.Cleanup(func() { constant.Setup = previous })

	require.NoError(t, db.Create(&Setup{
		Version:       "test",
		InitializedAt: 1,
	}).Error)

	constant.Setup = false
	CheckSetup()
	require.True(t, constant.Setup)
}
