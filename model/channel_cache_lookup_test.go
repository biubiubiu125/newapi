package model

import (
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func useEmptyChannelIDCache(t *testing.T) {
	t.Helper()
	previousEnabled := common.MemoryCacheEnabled
	channelSyncLock.Lock()
	previous := channelsIDM
	channelsIDM = nil
	channelSyncLock.Unlock()
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		channelSyncLock.Lock()
		channelsIDM = previous
		channelSyncLock.Unlock()
		common.MemoryCacheEnabled = previousEnabled
	})
}

func TestCacheGetChannelMissLoadsTheDatabaseRow(t *testing.T) {
	weight := uint(1)
	channel := &Channel{
		Type:   1,
		Key:    "cache-miss-key",
		Status: common.ChannelStatusEnabled,
		Name:   "cache-miss-channel",
		Group:  "default",
		Models: "cache-miss-model",
		Weight: &weight,
	}
	require.NoError(t, DB.Create(channel).Error)
	t.Cleanup(func() {
		_ = DB.Delete(&Channel{}, channel.Id).Error
	})
	useEmptyChannelIDCache(t)

	got, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, channel.Id, got.Id)
	require.Equal(t, "cache-miss-channel", got.Name)

	info, err := CacheGetChannelInfo(channel.Id)
	require.NoError(t, err)
	require.NotNil(t, info)
}

func TestCacheGetChannelMissIsDeletionOnlyWhenDatabaseHasNoRow(t *testing.T) {
	useEmptyChannelIDCache(t)

	_, err := CacheGetChannel(910000111)
	require.Error(t, err)
	require.True(t, IsChannelLookupMissing(err))
	require.False(t, IsChannelLookupTemporarilyUnavailable(err))

	_, infoErr := CacheGetChannelInfo(910000111)
	require.Error(t, infoErr)
	require.True(t, IsChannelLookupMissing(infoErr))
	require.False(t, IsChannelLookupTemporarilyUnavailable(infoErr))
}

func TestCacheGetChannelMissKeepsDatabaseOutageRetryable(t *testing.T) {
	broken, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "closed.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := broken.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	previousDB := DB
	DB = broken
	t.Cleanup(func() { DB = previousDB })
	useEmptyChannelIDCache(t)

	_, err = CacheGetChannel(42)
	require.Error(t, err)
	require.False(t, IsChannelLookupMissing(err))
	require.True(t, IsChannelLookupTemporarilyUnavailable(err))

	_, infoErr := CacheGetChannelInfo(42)
	require.Error(t, infoErr)
	require.False(t, IsChannelLookupMissing(infoErr))
	require.True(t, IsChannelLookupTemporarilyUnavailable(infoErr))
}

func TestCacheGetChannelHitDoesNotDependOnDatabase(t *testing.T) {
	cached := &Channel{Id: 880001, Name: "only-in-cache", Key: "k", Status: common.ChannelStatusEnabled}
	previousEnabled := common.MemoryCacheEnabled
	channelSyncLock.Lock()
	previous := channelsIDM
	channelsIDM = map[int]*Channel{cached.Id: cached}
	channelSyncLock.Unlock()
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		channelSyncLock.Lock()
		channelsIDM = previous
		channelSyncLock.Unlock()
		common.MemoryCacheEnabled = previousEnabled
	})

	got, err := CacheGetChannel(cached.Id)
	require.NoError(t, err)
	require.Same(t, cached, got)

	info, err := CacheGetChannelInfo(cached.Id)
	require.NoError(t, err)
	require.Equal(t, &cached.ChannelInfo, info)
}

func TestCacheGetChannelMissKeepsMultiKeyPollingIndex(t *testing.T) {
	weight := uint(1)
	channel := &Channel{
		Type:   1,
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		Name:   "polling-cache-miss",
		Group:  "default",
		Models: "polling-cache-miss",
		Weight: &weight,
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: 0,
		},
	}
	require.NoError(t, DB.Create(channel).Error)
	t.Cleanup(func() {
		_ = DB.Delete(&Channel{}, channel.Id).Error
	})
	useEmptyChannelIDCache(t)

	detached, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	key, _, keyErr := detached.GetNextEnabledKey()
	require.Nil(t, keyErr)
	require.Equal(t, "key-a", key)
	require.Equal(t, 1, detached.ChannelInfo.MultiKeyPollingIndex)

	info, err := CacheGetChannelInfo(channel.Id)
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, 1, info.MultiKeyPollingIndex)

	shared, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.Same(t, info, &shared.ChannelInfo)
	next, _, keyErr := shared.GetNextEnabledKey()
	require.Nil(t, keyErr)
	require.Equal(t, "key-b", next)
	require.Equal(t, 0, info.MultiKeyPollingIndex)
}
