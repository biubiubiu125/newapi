package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestFixAbilityRollsBackWhenRebuildFails(t *testing.T) {
	if !common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		t.Skip("the failure injection uses a SQLite trigger")
	}
	setupChannelStatusTest(t)
	require.NoError(t, DB.Exec("DROP TRIGGER IF EXISTS abilities_reject_boom").Error)
	t.Cleanup(func() {
		_ = DB.Exec("DROP TRIGGER IF EXISTS abilities_reject_boom").Error
	})

	orphan := Ability{Group: "default", Model: "orphan-model", ChannelId: 990001, Enabled: true}
	require.NoError(t, DB.Create(&orphan).Error)
	good := Channel{Name: "ability-good", Key: "good-key", Status: common.ChannelStatusEnabled, Models: "keep-model", Group: "default"}
	bad := Channel{Name: "ability-bad", Key: "bad-key", Status: common.ChannelStatusEnabled, Models: "boom-ability", Group: "default"}
	require.NoError(t, DB.Create(&good).Error)
	require.NoError(t, DB.Create(&bad).Error)
	require.NoError(t, good.AddAbilities(nil))

	require.NoError(t, DB.Exec(`
CREATE TRIGGER abilities_reject_boom
BEFORE INSERT ON abilities
WHEN NEW.model = 'boom-ability'
BEGIN
  SELECT RAISE(ABORT, 'boom ability');
END`).Error)

	_, _, err := FixAbility()
	require.Error(t, err)

	var orphanCount int64
	require.NoError(t, DB.Model(&Ability{}).Where("channel_id = ? AND model = ?", orphan.ChannelId, orphan.Model).Count(&orphanCount).Error)
	require.Equal(t, int64(1), orphanCount)
	var kept int64
	require.NoError(t, DB.Model(&Ability{}).Where("channel_id = ? AND model = ?", good.Id, "keep-model").Count(&kept).Error)
	require.Equal(t, int64(1), kept)
	var boom int64
	require.NoError(t, DB.Model(&Ability{}).Where("model = ?", "boom-ability").Count(&boom).Error)
	require.Zero(t, boom)
}
