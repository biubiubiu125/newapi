package model

import (
	"errors"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelInsertAndUpdateKeepAbilitiesInOneTransaction(t *testing.T) {
	setupChannelStatusTest(t)
	const callbackName = "test:fail_ability_create"
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		table := ""
		if tx.Statement != nil {
			table = tx.Statement.Table
			if table == "" && tx.Statement.Schema != nil {
				table = tx.Statement.Schema.Table
			}
		}
		if table == "abilities" {
			_ = tx.AddError(errors.New("ability write failed"))
		}
	}))
	t.Cleanup(func() { DB.Callback().Create().Remove(callbackName) })

	inserted := &Channel{Name: "insert-rolls-back", Key: "k", Status: common.ChannelStatusEnabled, Models: "short-model", Group: "default"}
	require.Error(t, inserted.Insert())
	var channelCount int64
	require.NoError(t, DB.Model(&Channel{}).Where("name = ?", inserted.Name).Count(&channelCount).Error)
	assert.Zero(t, channelCount)

	DB.Callback().Create().Remove(callbackName)
	updated := &Channel{Name: "update-rolls-back", Key: "k", Status: common.ChannelStatusEnabled, Models: "short-model", Group: "default"}
	require.NoError(t, updated.Insert())
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		table := ""
		if tx.Statement != nil {
			table = tx.Statement.Table
			if table == "" && tx.Statement.Schema != nil {
				table = tx.Statement.Schema.Table
			}
		}
		if table == "abilities" {
			_ = tx.AddError(errors.New("ability write failed"))
		}
	}))
	updated.Models = strings.Repeat("m", 40)
	require.Error(t, updated.Update())

	var stored Channel
	require.NoError(t, DB.First(&stored, updated.Id).Error)
	assert.Equal(t, "short-model", stored.Models)
	var ability Ability
	require.NoError(t, DB.Where("channel_id = ?", updated.Id).First(&ability).Error)
	assert.Equal(t, "short-model", ability.Model)
}
