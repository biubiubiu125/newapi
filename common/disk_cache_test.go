package common

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestImageTaskBodyCacheDirUsesConfiguredDiskCachePath(t *testing.T) {
	oldConfig := GetDiskCacheConfig()
	t.Cleanup(func() {
		SetDiskCacheConfig(oldConfig)
	})
	cacheRoot := t.TempDir()
	SetDiskCacheConfig(DiskCacheConfig{Path: cacheRoot})

	require.Equal(t, filepath.Join(cacheRoot, imageTaskBodyCacheDir), GetImageTaskBodyCacheDir())
}

func TestImageTaskResultCacheDirUsesConfiguredDiskCachePath(t *testing.T) {
	oldConfig := GetDiskCacheConfig()
	t.Cleanup(func() {
		SetDiskCacheConfig(oldConfig)
	})
	cacheRoot := t.TempDir()
	SetDiskCacheConfig(DiskCacheConfig{Path: cacheRoot})

	require.Equal(t, filepath.Join(cacheRoot, imageTaskResultCacheDir), GetImageTaskResultCacheDir())
}

func TestCleanupOldImageTaskBodyCacheFilesKeepsReferencedPaths(t *testing.T) {
	oldConfig := GetDiskCacheConfig()
	t.Cleanup(func() {
		SetDiskCacheConfig(oldConfig)
	})
	cacheRoot := t.TempDir()
	SetDiskCacheConfig(DiskCacheConfig{Path: cacheRoot})

	keptPath, err := WriteImageTaskBodyCacheFile([]byte("keep"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = RemoveDiskCacheFile(keptPath)
	})
	removedPath, err := WriteImageTaskBodyCacheFile([]byte("remove"))
	require.NoError(t, err)
	oldTime := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(keptPath, oldTime, oldTime))
	require.NoError(t, os.Chtimes(removedPath, oldTime, oldTime))

	err = CleanupOldImageTaskBodyCacheFiles(time.Hour, map[string]struct{}{
		filepath.Clean(keptPath): {},
	})

	require.NoError(t, err)
	require.FileExists(t, keptPath)
	require.NoFileExists(t, removedPath)
}

func TestImageTaskCacheWritesUseDiskQuota(t *testing.T) {
	oldConfig := GetDiskCacheConfig()
	ResetDiskCacheUsage()
	ResetDiskCacheStats()
	t.Cleanup(func() {
		ResetDiskCacheUsage()
		ResetDiskCacheStats()
		SetDiskCacheConfig(oldConfig)
	})
	SetDiskCacheConfig(DiskCacheConfig{
		Enabled:     false,
		ThresholdMB: 1,
		MaxSizeMB:   1,
		Path:        t.TempDir(),
	})

	first := bytes.Repeat([]byte("a"), 700<<10)
	second := bytes.Repeat([]byte("b"), 400<<10)
	path, err := WriteImageTaskBodyCacheFile(first)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = RemoveDiskCacheFile(path)
	})

	_, err = WriteImageTaskResultCacheFile(second)
	require.ErrorIs(t, err, ErrRequestBodyTooLarge)
	stats := GetDiskCacheStats()
	require.Equal(t, int64(1), stats.ActiveDiskFiles)
	require.Equal(t, int64(len(first)), stats.CurrentDiskUsageBytes)
}

func TestSetDiskCacheConfigSyncsExistingImageTaskCacheUsage(t *testing.T) {
	oldConfig := GetDiskCacheConfig()
	ResetDiskCacheUsage()
	ResetDiskCacheStats()
	t.Cleanup(func() {
		ResetDiskCacheUsage()
		ResetDiskCacheStats()
		SetDiskCacheConfig(oldConfig)
	})

	cacheRoot := t.TempDir()
	bodyDir := filepath.Join(cacheRoot, imageTaskBodyCacheDir)
	resultDir := filepath.Join(cacheRoot, imageTaskResultCacheDir)
	require.NoError(t, os.MkdirAll(bodyDir, 0755))
	require.NoError(t, os.MkdirAll(resultDir, 0755))
	bodyPath := filepath.Join(bodyDir, "body-existing.tmp")
	resultPath := filepath.Join(resultDir, "result-existing.json")
	require.NoError(t, os.WriteFile(bodyPath, bytes.Repeat([]byte("b"), 7), 0600))
	require.NoError(t, os.WriteFile(resultPath, bytes.Repeat([]byte("r"), 11), 0600))

	SetDiskCacheConfig(DiskCacheConfig{
		MaxSizeMB: 1,
		Path:      cacheRoot,
	})

	stats := GetDiskCacheStats()
	require.Equal(t, int64(2), stats.ActiveDiskFiles)
	require.Equal(t, int64(18), stats.CurrentDiskUsageBytes)
}

func TestCleanupOldImageTaskCacheFilesReleasesDiskQuota(t *testing.T) {
	oldConfig := GetDiskCacheConfig()
	ResetDiskCacheUsage()
	ResetDiskCacheStats()
	t.Cleanup(func() {
		ResetDiskCacheUsage()
		ResetDiskCacheStats()
		SetDiskCacheConfig(oldConfig)
	})
	SetDiskCacheConfig(DiskCacheConfig{
		MaxSizeMB: 1,
		Path:      t.TempDir(),
	})

	path, err := WriteImageTaskResultCacheFile(bytes.Repeat([]byte("x"), 16<<10))
	require.NoError(t, err)
	oldTime := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(path, oldTime, oldTime))

	require.NoError(t, CleanupOldImageTaskResultCacheFiles(time.Hour))

	require.NoFileExists(t, path)
	require.Equal(t, int64(0), GetDiskCacheStats().CurrentDiskUsageBytes)
}
