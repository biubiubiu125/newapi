package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestDatabaseSelectionSkipsWeightZeroWhenPositiveWeightExists(t *testing.T) {
	previous := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previous })

	priority := int64(3)
	lightWeight := uint(0)
	heavyWeight := uint(100)
	light := &Channel{
		Type:     1,
		Key:      "db-light-key",
		Status:   common.ChannelStatusEnabled,
		Name:     "db-weight-zero",
		Models:   "db-weight-model",
		Group:    "db-weight-group",
		Weight:   &lightWeight,
		Priority: &priority,
	}
	heavy := &Channel{
		Type:     1,
		Key:      "db-heavy-key",
		Status:   common.ChannelStatusEnabled,
		Name:     "db-weight-heavy",
		Models:   "db-weight-model",
		Group:    "db-weight-group",
		Weight:   &heavyWeight,
		Priority: &priority,
	}
	require.NoError(t, DB.Create(light).Error)
	require.NoError(t, DB.Create(heavy).Error)
	require.NoError(t, DB.Create(&Ability{
		Group: "db-weight-group", Model: "db-weight-model", ChannelId: light.Id,
		Enabled: true, Priority: &priority, Weight: 0,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group: "db-weight-group", Model: "db-weight-model", ChannelId: heavy.Id,
		Enabled: true, Priority: &priority, Weight: 100,
	}).Error)

	for i := 0; i < 50; i++ {
		selected, err := GetRandomSatisfiedChannel("db-weight-group", "db-weight-model", 0)
		require.NoError(t, err)
		require.NotNil(t, selected)
		require.Equal(t, heavy.Id, selected.Id)
	}
}

func TestDatabaseSelectionSharesTrafficWhenAllWeightsAreZero(t *testing.T) {
	previous := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previous })

	priority := int64(1)
	zero := uint(0)
	left := &Channel{
		Type: 1, Key: "db-zero-a", Status: common.ChannelStatusEnabled,
		Name: "db-zero-a", Models: "db-zero-model", Group: "db-zero-group",
		Weight: &zero, Priority: &priority,
	}
	right := &Channel{
		Type: 1, Key: "db-zero-b", Status: common.ChannelStatusEnabled,
		Name: "db-zero-b", Models: "db-zero-model", Group: "db-zero-group",
		Weight: &zero, Priority: &priority,
	}
	require.NoError(t, DB.Create(left).Error)
	require.NoError(t, DB.Create(right).Error)
	require.NoError(t, DB.Create(&Ability{
		Group: "db-zero-group", Model: "db-zero-model", ChannelId: left.Id,
		Enabled: true, Priority: &priority, Weight: 0,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group: "db-zero-group", Model: "db-zero-model", ChannelId: right.Id,
		Enabled: true, Priority: &priority, Weight: 0,
	}).Error)

	seen := map[int]int{}
	for i := 0; i < 40; i++ {
		selected, err := GetRandomSatisfiedChannel("db-zero-group", "db-zero-model", 0)
		require.NoError(t, err)
		require.NotNil(t, selected)
		seen[selected.Id]++
	}
	require.Greater(t, seen[left.Id], 0)
	require.Greater(t, seen[right.Id], 0)
}

func TestSelectAbilityByWeightMatchesMemoryCache(t *testing.T) {
	heavy := Ability{ChannelId: 2, Weight: 100}
	light := Ability{ChannelId: 1, Weight: 0}
	for roll := 0; roll < 100; roll++ {
		chosen, ok := selectAbilityByWeight([]Ability{light, heavy}, roll)
		require.True(t, ok)
		require.Equal(t, 2, chosen.ChannelId)
	}

	first, ok := selectAbilityByWeight([]Ability{{ChannelId: 1, Weight: 0}, {ChannelId: 2, Weight: 0}}, 0)
	require.True(t, ok)
	require.Equal(t, 1, first.ChannelId)
	second, ok := selectAbilityByWeight([]Ability{{ChannelId: 1, Weight: 0}, {ChannelId: 2, Weight: 0}}, 1)
	require.True(t, ok)
	require.Equal(t, 2, second.ChannelId)
}
