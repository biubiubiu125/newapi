package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestEditChannelByTagClearsModelsAndDissolvesTag(t *testing.T) {
	setupChannelStatusTest(t)

	tag := "batch"
	channel := Channel{
		Name:   "tagged",
		Key:    "sk-test",
		Models: "gpt-4,gpt-3.5",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
		Tag:    &tag,
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-4",
		ChannelId: channel.Id,
		Enabled:   true,
		Tag:       &tag,
	}).Error)

	empty := ""
	require.NoError(t, EditChannelByTag(tag, &empty, nil, &empty, nil, nil, nil, nil, nil))

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	if assert.NotNil(t, stored.Tag) {
		assert.Equal(t, "", *stored.Tag)
	}
	assert.Equal(t, "", stored.Models)

	var abilityCount int64
	require.NoError(t, DB.Model(&Ability{}).Where("channel_id = ?", channel.Id).Count(&abilityCount).Error)
	assert.Equal(t, int64(0), abilityCount)
}

func TestEditChannelByTagDissolveKeepsModelsAndClearsAbilityTag(t *testing.T) {
	setupChannelStatusTest(t)

	tag := "batch"
	channel := Channel{
		Name:   "tagged",
		Key:    "sk-test",
		Models: "gpt-4",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
		Tag:    &tag,
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-4",
		ChannelId: channel.Id,
		Enabled:   true,
		Tag:       &tag,
	}).Error)

	empty := ""
	require.NoError(t, EditChannelByTag(tag, &empty, nil, nil, nil, nil, nil, nil, nil))

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, "gpt-4", stored.Models)
	if assert.NotNil(t, stored.Tag) {
		assert.Equal(t, "", *stored.Tag)
	}

	var ability Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
	if assert.NotNil(t, ability.Tag) {
		assert.Equal(t, "", *ability.Tag)
	}
}

func TestEditChannelByTagClearsGroups(t *testing.T) {
	setupChannelStatusTest(t)

	tag := "group-tag"
	channel := Channel{
		Name:   "grouped",
		Key:    "sk-test",
		Models: "gpt-4",
		Group:  "default,vip",
		Status: common.ChannelStatusEnabled,
		Tag:    &tag,
	}
	require.NoError(t, channel.Insert())

	empty := ""
	require.NoError(t, EditChannelByTag(tag, nil, nil, nil, &empty, nil, nil, nil, nil))

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, "", stored.Group)

	var abilityCount int64
	require.NoError(t, DB.Model(&Ability{}).Where("channel_id = ?", channel.Id).Count(&abilityCount).Error)
	assert.Equal(t, int64(0), abilityCount)
}

func TestEditChannelByTagWritesZeroWeightAndPriority(t *testing.T) {
	setupChannelStatusTest(t)

	tag := "weight-tag"
	weight := uint(10)
	priority := int64(5)
	channel := Channel{
		Name:     "weighted",
		Key:      "sk-test",
		Models:   "gpt-4",
		Group:    "default",
		Status:   common.ChannelStatusEnabled,
		Tag:      &tag,
		Weight:   &weight,
		Priority: &priority,
	}
	require.NoError(t, channel.Insert())

	zeroWeight := uint(0)
	zeroPriority := int64(0)
	require.NoError(t, EditChannelByTag(tag, nil, nil, nil, nil, &zeroPriority, &zeroWeight, nil, nil))

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	require.NotNil(t, stored.Weight)
	assert.Equal(t, uint(0), *stored.Weight)
	require.NotNil(t, stored.Priority)
	assert.Equal(t, int64(0), *stored.Priority)

	var ability Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
	assert.Equal(t, uint(0), ability.Weight)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, int64(0), *ability.Priority)
}

func TestUpdateAbilitiesUsesDatabaseStatusNotStaleSnapshot(t *testing.T) {
	setupChannelStatusTest(t)

	tag := "batch"
	channel := Channel{
		Name:   "stale-status",
		Key:    "sk-test",
		Models: "gpt-4",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
		Tag:    &tag,
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-4",
		ChannelId: channel.Id,
		Enabled:   true,
		Tag:       &tag,
	}).Error)
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Updates(map[string]any{
		"status": common.ChannelStatusManuallyDisabled,
		"models": "gpt-4,gpt-4o",
	}).Error)

	stale := channel
	stale.Status = common.ChannelStatusEnabled
	stale.Models = "gpt-4"
	require.NoError(t, stale.UpdateAbilities(nil))

	var abilities []Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).Order("model").Find(&abilities).Error)
	require.Len(t, abilities, 2)
	assert.Equal(t, []string{"gpt-4", "gpt-4o"}, []string{abilities[0].Model, abilities[1].Model})
	assert.False(t, abilities[0].Enabled)
	assert.False(t, abilities[1].Enabled)
}

func TestEditChannelByTagDoesNotRewriteChannelThatJoinsDuringUpdate(t *testing.T) {
	setupChannelStatusTest(t)

	tag := "batch"
	original := Channel{
		Name:   "original",
		Key:    "sk-a",
		Models: "old-a",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
		Tag:    &tag,
	}
	require.NoError(t, DB.Create(&original).Error)
	require.NoError(t, DB.Create(&Ability{
		Group: "default", Model: "old-a", ChannelId: original.Id, Enabled: true, Tag: &tag,
	}).Error)

	joiner := Channel{
		Name:   "joiner",
		Key:    "sk-b",
		Models: "old-b",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, DB.Create(&joiner).Error)
	require.NoError(t, DB.Create(&Ability{
		Group: "default", Model: "old-b", ChannelId: joiner.Id, Enabled: true,
	}).Error)

	callbackName := "edit-tag-join-" + t.Name()
	var joined bool
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if joined || tx.Statement == nil || tx.Statement.Table != "channels" {
			return
		}
		joined = true
		err := tx.Session(&gorm.Session{SkipHooks: true}).Model(&Channel{}).Where("id = ?", joiner.Id).Update("tag", tag).Error
		require.NoError(t, err)
	}))
	t.Cleanup(func() {
		_ = DB.Callback().Update().Remove(callbackName)
	})

	newModels := "new-model"
	require.NoError(t, EditChannelByTag(tag, nil, nil, &newModels, nil, nil, nil, nil, nil))

	var storedJoiner Channel
	require.NoError(t, DB.First(&storedJoiner, joiner.Id).Error)
	assert.Equal(t, "old-b", storedJoiner.Models)
	var joinerAbility Ability
	require.NoError(t, DB.Where("channel_id = ?", joiner.Id).First(&joinerAbility).Error)
	assert.Equal(t, "old-b", joinerAbility.Model)

	var originalAbilities []Ability
	require.NoError(t, DB.Where("channel_id = ?", original.Id).Find(&originalAbilities).Error)
	require.Len(t, originalAbilities, 1)
	assert.Equal(t, "new-model", originalAbilities[0].Model)
	assert.True(t, originalAbilities[0].Enabled)
}
