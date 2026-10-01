package model

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func openPostgreSQLChannelTest(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		dsn = "host=/var/run/postgresql user=biubiubiu dbname=newapi_image_task_sql sslmode=disable"
	}
	pg, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := pg.DB()
	require.NoError(t, err)
	require.NoError(t, pg.AutoMigrate(&Channel{}))

	previousDB := DB
	previousLogDB := LOG_DB
	previousMain := common.MainDatabaseType()
	previousLog := common.LogDatabaseType()
	DB = pg
	LOG_DB = pg
	common.SetDatabaseTypes(common.DatabaseTypePostgreSQL, common.DatabaseTypePostgreSQL)
	t.Cleanup(func() {
		DB = previousDB
		LOG_DB = previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		_ = sqlDB.Close()
	})
	return pg
}

func TestPostgreSQLMultiKeyChannelInfoUpdatesDoNotClobber(t *testing.T) {
	pg := openPostgreSQLChannelTest(t)

	channel := &Channel{
		Type:   1,
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		Name:   "pg-polling-cursor",
		Group:  "default",
		Models: "pg-polling-cursor",
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: 1,
		},
	}
	require.NoError(t, pg.Create(channel).Error)
	t.Cleanup(func() {
		_ = pg.Delete(&Channel{}, channel.Id).Error
	})

	stale := *channel
	stale.ChannelInfo.MultiKeyPollingIndex = 0
	stale.ChannelInfo.MultiKeyStatusList = map[int]int{0: common.ChannelStatusAutoDisabled}
	stale.ChannelInfo.MultiKeyDisabledReason = map[int]string{0: "provider rejected key"}
	stale.ChannelInfo.MultiKeyDisabledTime = map[int]int64{0: 111}
	require.NoError(t, stale.saveStatusState())

	var stored Channel
	require.NoError(t, pg.First(&stored, channel.Id).Error)
	require.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[0])
	require.Equal(t, "provider rejected key", stored.ChannelInfo.MultiKeyDisabledReason[0])
	require.Equal(t, int64(111), stored.ChannelInfo.MultiKeyDisabledTime[0])
	require.Equal(t, 1, stored.ChannelInfo.MultiKeyPollingIndex)

	require.NoError(t, persistMultiKeyPollingIndex(channel.Id, 2))
	require.NoError(t, pg.First(&stored, channel.Id).Error)
	require.Equal(t, 2, stored.ChannelInfo.MultiKeyPollingIndex)
	require.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[0])
	require.Equal(t, "provider rejected key", stored.ChannelInfo.MultiKeyDisabledReason[0])

	const rounds = 20
	for round := 0; round < rounds; round++ {
		enabled := ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: 0,
		}
		require.NoError(t, pg.Model(&Channel{}).Where("id = ?", channel.Id).Update("channel_info", enabled).Error)

		var wg sync.WaitGroup
		errors := make(chan error, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			writer := *channel
			writer.ChannelInfo = enabled
			writer.ChannelInfo.MultiKeyStatusList = map[int]int{0: common.ChannelStatusAutoDisabled}
			writer.ChannelInfo.MultiKeyDisabledReason = map[int]string{0: "provider rejected key"}
			errors <- writer.saveStatusState()
		}()
		go func() {
			defer wg.Done()
			errors <- persistMultiKeyPollingIndex(channel.Id, 1)
		}()
		wg.Wait()
		close(errors)
		for saveErr := range errors {
			require.NoError(t, saveErr)
		}

		require.NoError(t, pg.First(&stored, channel.Id).Error)
		require.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[0], "round %d restored a disabled key", round)
		require.Equal(t, "provider rejected key", stored.ChannelInfo.MultiKeyDisabledReason[0], "round %d", round)
		require.Equal(t, 1, stored.ChannelInfo.MultiKeyPollingIndex, "round %d rewound the polling cursor", round)
	}
}

func TestPostgreSQLMultiKeyStatusMergeClearAndReplace(t *testing.T) {
	pg := openPostgreSQLChannelTest(t)

	channel := &Channel{
		Type:   1,
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusAutoDisabled,
		Name:   "pg-key-merge",
		Group:  "default",
		Models: "pg-key-merge",
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: 1,
			MultiKeyStatusList: map[int]int{
				1: common.ChannelStatusAutoDisabled,
			},
			MultiKeyDisabledReason: map[int]string{1: "provider rejected key"},
			MultiKeyDisabledTime:   map[int]int64{1: 222},
		},
	}
	require.NoError(t, pg.Create(channel).Error)
	t.Cleanup(func() {
		_ = pg.Delete(&Channel{}, channel.Id).Error
	})

	stale := *channel
	stale.ChannelInfo.MultiKeyPollingIndex = 0
	stale.ChannelInfo.MultiKeyStatusList = map[int]int{0: common.ChannelStatusManuallyDisabled}
	stale.ChannelInfo.MultiKeyDisabledReason = map[int]string{0: "manual disable"}
	stale.ChannelInfo.MultiKeyDisabledTime = map[int]int64{0: 111}
	require.NoError(t, stale.saveStatusState())

	var stored Channel
	require.NoError(t, pg.First(&stored, channel.Id).Error)
	require.Equal(t, common.ChannelStatusManuallyDisabled, stored.ChannelInfo.MultiKeyStatusList[0])
	require.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[1])
	require.Equal(t, "manual disable", stored.ChannelInfo.MultiKeyDisabledReason[0])
	require.Equal(t, "provider rejected key", stored.ChannelInfo.MultiKeyDisabledReason[1])
	require.Equal(t, int64(222), stored.ChannelInfo.MultiKeyDisabledTime[1])
	require.Equal(t, 1, stored.ChannelInfo.MultiKeyPollingIndex)

	cleared := stored
	cleared.ChannelInfo.MultiKeyStatusList = nil
	cleared.ChannelInfo.MultiKeyDisabledReason = nil
	cleared.ChannelInfo.MultiKeyDisabledTime = nil
	cleared.ChannelInfo.MultiKeyPollingIndex = 0
	cleared.MarkMultiKeyStatusCleared(0)
	require.NoError(t, cleared.saveStatusState())
	require.NoError(t, pg.First(&stored, channel.Id).Error)
	require.NotContains(t, stored.ChannelInfo.MultiKeyStatusList, 0)
	require.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[1])
	require.Equal(t, "provider rejected key", stored.ChannelInfo.MultiKeyDisabledReason[1])
	require.Equal(t, 1, stored.ChannelInfo.MultiKeyPollingIndex)

	replaced := stored
	replaced.Status = common.ChannelStatusEnabled
	replaced.ChannelInfo.MultiKeyStatusList = map[int]int{}
	replaced.ChannelInfo.MultiKeyDisabledReason = map[int]string{}
	replaced.ChannelInfo.MultiKeyDisabledTime = map[int]int64{}
	replaced.ChannelInfo.MultiKeyPollingIndex = 0
	replaced.MarkMultiKeyStatusReplaced()
	require.NoError(t, replaced.saveStatusState())
	require.NoError(t, pg.First(&stored, channel.Id).Error)
	require.Empty(t, stored.ChannelInfo.MultiKeyStatusList)
	require.Empty(t, stored.ChannelInfo.MultiKeyDisabledReason)
	require.Empty(t, stored.ChannelInfo.MultiKeyDisabledTime)
	require.Equal(t, 1, stored.ChannelInfo.MultiKeyPollingIndex)
	require.Equal(t, 2, stored.ChannelInfo.MultiKeySize)
}

func TestPostgreSQLPerKeyDisableKeepsManualStatusCommittedWhileWaiting(t *testing.T) {
	pg := openPostgreSQLChannelTest(t)
	require.NoError(t, pg.AutoMigrate(&Ability{}))

	channel := newPostgreSQLLockChannel(t, pg, "pg-manual-while-locked", "key-a\nkey-b")
	insertPostgreSQLAbility(t, pg, channel)

	writer := startWhileChannelLocked(t, pg, channel.Id, func() error {
		if !UpdateChannelStatus(channel.Id, "key-a", common.ChannelStatusAutoDisabled, "provider rejected key") {
			return errPostgreSQLStatusNotWritten
		}
		return nil
	}, func(tx *gorm.DB, locked *Channel) error {
		if err := tx.Model(locked).Update("status", common.ChannelStatusManuallyDisabled).Error; err != nil {
			return err
		}
		return tx.Model(&Ability{}).Where("channel_id = ?", locked.Id).Update("enabled", false).Error
	})
	require.NoError(t, writer)

	var stored Channel
	require.NoError(t, pg.First(&stored, channel.Id).Error)
	require.Equal(t, common.ChannelStatusManuallyDisabled, stored.Status)
	require.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[0])
	var ability Ability
	require.NoError(t, pg.Where("channel_id = ?", channel.Id).First(&ability).Error)
	require.False(t, ability.Enabled)
}

func TestPostgreSQLSecondLastKeyDisableTurnsChannelOff(t *testing.T) {
	pg := openPostgreSQLChannelTest(t)
	require.NoError(t, pg.AutoMigrate(&Ability{}))

	channel := newPostgreSQLLockChannel(t, pg, "pg-last-key", "key-a\nkey-b")
	insertPostgreSQLAbility(t, pg, channel)

	writer := startWhileChannelLocked(t, pg, channel.Id, func() error {
		if !UpdateChannelStatus(channel.Id, "key-b", common.ChannelStatusAutoDisabled, "provider rejected key") {
			return errPostgreSQLStatusNotWritten
		}
		return nil
	}, func(tx *gorm.DB, locked *Channel) error {
		locked.ChannelInfo.MultiKeyStatusList = map[int]int{0: common.ChannelStatusAutoDisabled}
		locked.ChannelInfo.MultiKeyDisabledReason = map[int]string{0: "holder"}
		locked.ChannelInfo.MultiKeyDisabledTime = map[int]int64{0: 111}
		return locked.saveStatusStateWithTx(tx)
	})
	require.NoError(t, writer)

	var stored Channel
	require.NoError(t, pg.First(&stored, channel.Id).Error)
	require.Equal(t, common.ChannelStatusAutoDisabled, stored.Status)
	require.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[0])
	require.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[1])
	var ability Ability
	require.NoError(t, pg.Where("channel_id = ?", channel.Id).First(&ability).Error)
	require.False(t, ability.Enabled)
}

func TestPostgreSQLDeleteReindexesDisableCommittedWhileWaiting(t *testing.T) {
	pg := openPostgreSQLChannelTest(t)
	require.NoError(t, pg.AutoMigrate(&Ability{}))

	channel := newPostgreSQLLockChannel(t, pg, "pg-delete-reindex", "key-a\nkey-b\nkey-c")
	channel.ChannelInfo.MultiKeySize = 3
	require.NoError(t, pg.Model(channel).Update("channel_info", channel.ChannelInfo).Error)

	writer := startWhileChannelLocked(t, pg, channel.Id, func() error {
		return DeleteMultiKeyAtIndex(channel.Id, 0)
	}, func(tx *gorm.DB, locked *Channel) error {
		locked.ChannelInfo.MultiKeyStatusList = map[int]int{2: common.ChannelStatusAutoDisabled}
		locked.ChannelInfo.MultiKeyDisabledReason = map[int]string{2: "provider rejected key"}
		locked.ChannelInfo.MultiKeyDisabledTime = map[int]int64{2: 333}
		return locked.saveStatusStateWithTx(tx)
	})
	require.NoError(t, writer)

	var stored Channel
	require.NoError(t, pg.First(&stored, channel.Id).Error)
	require.Equal(t, "key-b\nkey-c", stored.Key)
	require.Equal(t, 2, stored.ChannelInfo.MultiKeySize)
	require.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[1])
	require.Equal(t, "provider rejected key", stored.ChannelInfo.MultiKeyDisabledReason[1])
	require.NotContains(t, stored.ChannelInfo.MultiKeyStatusList, 2)
}

var errPostgreSQLStatusNotWritten = errors.New("channel status was not written")

func newPostgreSQLLockChannel(t *testing.T, pg *gorm.DB, name string, key string) *Channel {
	t.Helper()
	channel := &Channel{
		Type:   1,
		Key:    key,
		Status: common.ChannelStatusEnabled,
		Name:   name,
		Group:  "default",
		Models: name,
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
			MultiKeyMode: constant.MultiKeyModePolling,
		},
	}
	require.NoError(t, pg.Create(channel).Error)
	t.Cleanup(func() {
		_ = pg.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error
		_ = pg.Delete(&Channel{}, channel.Id).Error
	})
	return channel
}

func insertPostgreSQLAbility(t *testing.T, pg *gorm.DB, channel *Channel) {
	t.Helper()
	priority := int64(0)
	tag := channel.Name
	require.NoError(t, pg.Create(&Ability{
		Group:     "default",
		Model:     channel.Models,
		ChannelId: channel.Id,
		Enabled:   true,
		Priority:  &priority,
		Tag:       &tag,
	}).Error)
}

// startWhileChannelLocked holds the row, waits until fn is blocked on it, then
// applies the overlapping write and commits. fn must lock the same row before
// it reads channel status or key indexes.
func startWhileChannelLocked(t *testing.T, pg *gorm.DB, channelID int, fn func() error, overlap func(tx *gorm.DB, locked *Channel) error) error {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		dsn = "host=/var/run/postgresql user=biubiubiu dbname=newapi_image_task_sql sslmode=disable"
	}
	holder, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := holder.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	tx := holder.Begin()
	require.NoError(t, tx.Error)
	committed := false
	t.Cleanup(func() {
		if !committed {
			_ = tx.Rollback().Error
		}
	})
	var locked Channel
	require.NoError(t, tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, "id = ?", channelID).Error)

	done := make(chan error, 1)
	go func() {
		done <- fn()
	}()
	waitForChannelLockWaiter(t, pg)
	require.NoError(t, overlap(tx, &locked))
	require.NoError(t, tx.Commit().Error)
	committed = true

	select {
	case err := <-done:
		return err
	case <-time.After(15 * time.Second):
		t.Fatal("channel update did not finish after the row lock was released")
		return nil
	}
}

func waitForChannelLockWaiter(t *testing.T, pg *gorm.DB) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		err := pg.Raw(`
			SELECT COUNT(*) FROM pg_stat_activity
			WHERE pid <> pg_backend_pid()
			  AND wait_event_type = 'Lock'
			  AND query LIKE '%channels%'
		`).Scan(&waiting).Error
		require.NoError(t, err)
		if waiting > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for the channel row lock")
}
