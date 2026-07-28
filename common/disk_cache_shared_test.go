package common

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestReserveDiskCacheBytesUsesActualSharedCacheUsage(t *testing.T) {
	oldConfig := GetDiskCacheConfig()
	oldShared := constant.ImageTaskFileCacheShared
	cachePath := t.TempDir()
	SetDiskCacheConfig(DiskCacheConfig{
		Enabled:     true,
		ThresholdMB: 1,
		MaxSizeMB:   1,
		Path:        cachePath,
	})
	constant.ImageTaskFileCacheShared = true
	t.Cleanup(func() {
		constant.ImageTaskFileCacheShared = oldShared
		SetDiskCacheConfig(oldConfig)
		ResetDiskCacheUsage()
	})

	require.NoError(t, os.MkdirAll(GetImageTaskBodyCacheDir(), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(GetImageTaskBodyCacheDir(), "other-node-body.bin"),
		make([]byte, 800<<10),
		0o600,
	))
	ResetDiskCacheUsage() // Simulate a node whose process-local counter is stale.

	reservation, err := ReserveDiskCacheBytes(300 << 10)
	if reservation != nil {
		reservation.Release()
	}
	require.True(t, errors.Is(err, ErrDiskCacheCapacityUnavailable), err)
}

func TestSharedDiskCacheReservationCountsInFlightBytesBeforeCommit(t *testing.T) {
	oldConfig := GetDiskCacheConfig()
	oldShared := constant.ImageTaskFileCacheShared
	cachePath := t.TempDir()
	SetDiskCacheConfig(DiskCacheConfig{
		Enabled:     true,
		ThresholdMB: 1,
		MaxSizeMB:   1,
		Path:        cachePath,
	})
	constant.ImageTaskFileCacheShared = true
	t.Cleanup(func() {
		constant.ImageTaskFileCacheShared = oldShared
		SetDiskCacheConfig(oldConfig)
		ResetDiskCacheUsage()
	})

	first, err := ReserveDiskCacheBytes(600 << 10)
	require.NoError(t, err)
	type reserveResult struct {
		reservation *DiskCacheReservation
		err         error
	}
	secondResult := make(chan reserveResult, 1)
	go func() {
		reservation, reserveErr := ReserveDiskCacheBytes(600 << 10)
		secondResult <- reserveResult{reservation: reservation, err: reserveErr}
	}()

	select {
	case result := <-secondResult:
		if result.reservation != nil {
			result.reservation.Release()
		}
		require.ErrorIs(t, result.err, ErrDiskCacheCapacityUnavailable)
	case <-time.After(5 * time.Second):
		t.Fatal("second shared reservation did not observe in-flight reserved bytes")
	}

	require.NoError(t, os.MkdirAll(GetImageTaskBodyCacheDir(), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(GetImageTaskBodyCacheDir(), "first-node-body.bin"),
		make([]byte, 600<<10),
		0o600,
	))
	require.NoError(t, first.Commit(600<<10))
}
