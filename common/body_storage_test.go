package common

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateBodyStorageFromReaderSpoolsUnknownLengthToDisk(t *testing.T) {
	oldConfig := GetDiskCacheConfig()
	ResetDiskCacheUsage()
	ResetDiskCacheStats()
	SetDiskCacheConfig(DiskCacheConfig{
		Enabled:     true,
		ThresholdMB: 1,
		MaxSizeMB:   16,
		Path:        t.TempDir(),
	})
	t.Cleanup(func() {
		ResetDiskCacheUsage()
		ResetDiskCacheStats()
		SetDiskCacheConfig(oldConfig)
	})

	payload := bytes.Repeat([]byte("a"), 2<<20)
	storage, err := CreateBodyStorageFromReader(bytes.NewReader(payload), -1, 4<<20)
	require.NoError(t, err)
	defer storage.Close()

	require.True(t, storage.IsDisk())
	require.Equal(t, int64(len(payload)), storage.Size())
	got, err := storage.Bytes()
	require.NoError(t, err)
	require.Equal(t, payload, got)
}

func TestCreateBodyStorageFromReaderUnknownLengthExtendsReservation(t *testing.T) {
	oldConfig := GetDiskCacheConfig()
	ResetDiskCacheUsage()
	ResetDiskCacheStats()
	SetDiskCacheConfig(DiskCacheConfig{
		Enabled:     true,
		ThresholdMB: 1,
		MaxSizeMB:   2,
		Path:        t.TempDir(),
	})
	t.Cleanup(func() {
		ResetDiskCacheUsage()
		ResetDiskCacheStats()
		SetDiskCacheConfig(oldConfig)
	})

	payload := bytes.Repeat([]byte("a"), 1536<<10)
	storage, err := CreateBodyStorageFromReader(bytes.NewReader(payload), -1, 4<<20)
	require.NoError(t, err)
	defer storage.Close()

	require.True(t, storage.IsDisk())
	require.Equal(t, int64(len(payload)), storage.Size())
	require.Equal(t, int64(1), GetDiskCacheStats().ActiveDiskFiles)
	require.Equal(t, int64(len(payload)), GetDiskCacheStats().CurrentDiskUsageBytes)
}

func TestCreateBodyStorageFromReaderUnknownLengthDoesNotFallbackToMemoryWhenDiskFull(t *testing.T) {
	oldConfig := GetDiskCacheConfig()
	ResetDiskCacheUsage()
	ResetDiskCacheStats()
	SetDiskCacheConfig(DiskCacheConfig{
		Enabled:     true,
		ThresholdMB: 1,
		MaxSizeMB:   1,
		Path:        t.TempDir(),
	})
	t.Cleanup(func() {
		ResetDiskCacheUsage()
		ResetDiskCacheStats()
		SetDiskCacheConfig(oldConfig)
	})

	payload := bytes.Repeat([]byte("a"), 2<<20)
	beforeMemoryBuffers := GetDiskCacheStats().ActiveMemoryBuffers
	storage, err := CreateBodyStorageFromReader(bytes.NewReader(payload), -1, 4<<20)

	require.Error(t, err)
	require.Nil(t, storage)
	require.True(t, IsRequestBodyTooLargeError(err))
	require.Equal(t, beforeMemoryBuffers, GetDiskCacheStats().ActiveMemoryBuffers)
}
