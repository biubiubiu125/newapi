package model

import (
	"path/filepath"
	"sync"
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

func TestInitChannelCacheKeepsDatabasePollingIndex(t *testing.T) {
	weight := uint(1)
	channel := &Channel{
		Type:   1,
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		Name:   "polling-index-db-wins",
		Group:  "default",
		Models: "polling-index-db-wins",
		Weight: &weight,
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: 1,
		},
	}
	require.NoError(t, DB.Create(channel).Error)
	t.Cleanup(func() {
		_ = DB.Delete(&Channel{}, channel.Id).Error
	})

	previousEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousEnabled
		InitChannelCache()
	})

	stale := *channel
	stale.ChannelInfo.MultiKeyPollingIndex = 0
	channelSyncLock.Lock()
	if channelsIDM == nil {
		channelsIDM = map[int]*Channel{}
	}
	channelsIDM[channel.Id] = &stale
	channelSyncLock.Unlock()

	require.NoError(t, InitChannelCacheWithError())

	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.NotNil(t, cached)
	require.Equal(t, 1, cached.ChannelInfo.MultiKeyPollingIndex)

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	require.Equal(t, 1, stored.ChannelInfo.MultiKeyPollingIndex)
}

func TestGetNextEnabledKeyPersistsPollingIndexWithMemoryCache(t *testing.T) {
	weight := uint(1)
	channel := &Channel{
		Type:   1,
		Key:    "key-a\nkey-b\nkey-c",
		Status: common.ChannelStatusEnabled,
		Name:   "polling-index-persist",
		Group:  "default",
		Models: "polling-index-persist",
		Weight: &weight,
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 3,
			MultiKeyMode: constant.MultiKeyModePolling,
			MultiKeyStatusList: map[int]int{
				2: common.ChannelStatusAutoDisabled,
			},
		},
	}
	require.NoError(t, DB.Create(channel).Error)
	t.Cleanup(func() {
		_ = DB.Delete(&Channel{}, channel.Id).Error
	})
	previousEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	InitChannelCache()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = previousEnabled
		InitChannelCache()
	})

	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.NotNil(t, cached)
	cached.ChannelInfo.MultiKeyStatusList = nil

	key, _, keyErr := cached.GetNextEnabledKey()
	require.Nil(t, keyErr)
	require.Equal(t, "key-a", key)

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	require.Equal(t, 1, stored.ChannelInfo.MultiKeyPollingIndex)
	require.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[2])
}

func TestPersistMultiKeyPollingIndexKeepsConcurrentKeyDisable(t *testing.T) {
	weight := uint(1)
	channel := &Channel{
		Type:   1,
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		Name:   "polling-index-concurrent",
		Group:  "default",
		Models: "polling-index-concurrent",
		Weight: &weight,
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
			MultiKeyMode: constant.MultiKeyModePolling,
		},
	}
	require.NoError(t, DB.Create(channel).Error)
	t.Cleanup(func() {
		_ = DB.Delete(&Channel{}, channel.Id).Error
	})

	const rounds = 40
	for round := 0; round < rounds; round++ {
		enabled := ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: 0,
		}
		require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Update("channel_info", enabled).Error)

		var wg sync.WaitGroup
		errors := make(chan error, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			stale := *channel
			stale.ChannelInfo = enabled
			stale.ChannelInfo.MultiKeyStatusList = map[int]int{0: common.ChannelStatusAutoDisabled}
			stale.ChannelInfo.MultiKeyDisabledReason = map[int]string{0: "provider rejected key"}
			errors <- stale.saveStatusState()
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

		var stored Channel
		require.NoError(t, DB.First(&stored, channel.Id).Error)
		require.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[0], "round %d restored a disabled key", round)
		require.Equal(t, "provider rejected key", stored.ChannelInfo.MultiKeyDisabledReason[0], "round %d", round)
		require.Equal(t, 1, stored.ChannelInfo.MultiKeyPollingIndex, "round %d rewound the polling cursor", round)
	}
}
