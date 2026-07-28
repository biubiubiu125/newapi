package common

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/constant"
)

// DiskCacheConfig 磁盘缓存配置（由 performance_setting 包更新）
type DiskCacheConfig struct {
	// Enabled 是否启用磁盘缓存
	Enabled bool
	// ThresholdMB 触发磁盘缓存的请求体大小阈值（MB）
	ThresholdMB int
	// MaxSizeMB 磁盘缓存最大总大小（MB）
	MaxSizeMB int
	// Path 磁盘缓存目录
	Path string
}

// 全局磁盘缓存配置
var diskCacheConfig = DiskCacheConfig{
	Enabled:     false,
	ThresholdMB: 10,
	MaxSizeMB:   1024,
	Path:        "",
}
var diskCacheConfigMu sync.RWMutex

// GetDiskCacheConfig 获取磁盘缓存配置
func GetDiskCacheConfig() DiskCacheConfig {
	diskCacheConfigMu.RLock()
	defer diskCacheConfigMu.RUnlock()
	return diskCacheConfig
}

// SetDiskCacheConfig 设置磁盘缓存配置
func SetDiskCacheConfig(config DiskCacheConfig) {
	diskCacheConfigMu.Lock()
	diskCacheConfig = config
	diskCacheConfigMu.Unlock()
	SyncDiskCacheStats()
}

// IsDiskCacheEnabled 是否启用磁盘缓存
func IsDiskCacheEnabled() bool {
	diskCacheConfigMu.RLock()
	defer diskCacheConfigMu.RUnlock()
	return diskCacheConfig.Enabled
}

// GetDiskCacheThresholdBytes 获取磁盘缓存阈值（字节）
func GetDiskCacheThresholdBytes() int64 {
	diskCacheConfigMu.RLock()
	defer diskCacheConfigMu.RUnlock()
	return int64(diskCacheConfig.ThresholdMB) << 20
}

// GetDiskCacheMaxSizeBytes 获取磁盘缓存最大大小（字节）
func GetDiskCacheMaxSizeBytes() int64 {
	diskCacheConfigMu.RLock()
	defer diskCacheConfigMu.RUnlock()
	return int64(diskCacheConfig.MaxSizeMB) << 20
}

// GetDiskCachePath 获取磁盘缓存目录
func GetDiskCachePath() string {
	diskCacheConfigMu.RLock()
	defer diskCacheConfigMu.RUnlock()
	return diskCacheConfig.Path
}

// DiskCacheStats 磁盘缓存统计信息
type DiskCacheStats struct {
	// 当前活跃的磁盘缓存文件数
	ActiveDiskFiles int64 `json:"active_disk_files"`
	// 当前磁盘缓存总大小（字节）
	CurrentDiskUsageBytes int64 `json:"current_disk_usage_bytes"`
	// 当前内存缓存数量
	ActiveMemoryBuffers int64 `json:"active_memory_buffers"`
	// 当前内存缓存总大小（字节）
	CurrentMemoryUsageBytes int64 `json:"current_memory_usage_bytes"`
	// 磁盘缓存命中次数
	DiskCacheHits int64 `json:"disk_cache_hits"`
	// 内存缓存命中次数
	MemoryCacheHits int64 `json:"memory_cache_hits"`
	// 磁盘缓存最大限制（字节）
	DiskCacheMaxBytes int64 `json:"disk_cache_max_bytes"`
	// 磁盘缓存阈值（字节）
	DiskCacheThresholdBytes int64 `json:"disk_cache_threshold_bytes"`
}

var diskCacheStats DiskCacheStats

var ErrDiskCacheCapacityUnavailable = fmt.Errorf("%w: disk cache capacity unavailable", ErrRequestBodyTooLarge)

type DiskCacheReservation struct {
	size         int64
	done         int32
	sharedMarker string
}

type diskCacheReservationFileLock struct {
	path     string
	token    string
	released int32
}

const (
	diskCacheReservationLockWait     = 30 * time.Second
	diskCacheReservationLockStale    = 10 * time.Minute
	diskCacheReservationMarkerStale  = 15 * time.Minute
	diskCacheReservationMarkerPrefix = ".newapi-disk-cache-reservation-"
)

// GetDiskCacheStats 获取缓存统计信息
func GetDiskCacheStats() DiskCacheStats {
	stats := DiskCacheStats{
		ActiveDiskFiles:         atomic.LoadInt64(&diskCacheStats.ActiveDiskFiles),
		CurrentDiskUsageBytes:   atomic.LoadInt64(&diskCacheStats.CurrentDiskUsageBytes),
		ActiveMemoryBuffers:     atomic.LoadInt64(&diskCacheStats.ActiveMemoryBuffers),
		CurrentMemoryUsageBytes: atomic.LoadInt64(&diskCacheStats.CurrentMemoryUsageBytes),
		DiskCacheHits:           atomic.LoadInt64(&diskCacheStats.DiskCacheHits),
		MemoryCacheHits:         atomic.LoadInt64(&diskCacheStats.MemoryCacheHits),
		DiskCacheMaxBytes:       GetDiskCacheMaxSizeBytes(),
		DiskCacheThresholdBytes: GetDiskCacheThresholdBytes(),
	}
	return stats
}

// IncrementDiskFiles 增加磁盘文件计数
func IncrementDiskFiles(size int64) {
	atomic.AddInt64(&diskCacheStats.ActiveDiskFiles, 1)
	atomic.AddInt64(&diskCacheStats.CurrentDiskUsageBytes, size)
}

// DecrementDiskFiles 减少磁盘文件计数
func DecrementDiskFiles(size int64) {
	if atomic.AddInt64(&diskCacheStats.ActiveDiskFiles, -1) < 0 {
		atomic.StoreInt64(&diskCacheStats.ActiveDiskFiles, 0)
	}
	releaseDiskCacheBytes(size)
}

// IncrementMemoryBuffers 增加内存缓存计数
func IncrementMemoryBuffers(size int64) {
	atomic.AddInt64(&diskCacheStats.ActiveMemoryBuffers, 1)
	atomic.AddInt64(&diskCacheStats.CurrentMemoryUsageBytes, size)
}

// DecrementMemoryBuffers 减少内存缓存计数
func DecrementMemoryBuffers(size int64) {
	atomic.AddInt64(&diskCacheStats.ActiveMemoryBuffers, -1)
	atomic.AddInt64(&diskCacheStats.CurrentMemoryUsageBytes, -size)
}

// IncrementDiskCacheHits 增加磁盘缓存命中次数
func IncrementDiskCacheHits() {
	atomic.AddInt64(&diskCacheStats.DiskCacheHits, 1)
}

// IncrementMemoryCacheHits 增加内存缓存命中次数
func IncrementMemoryCacheHits() {
	atomic.AddInt64(&diskCacheStats.MemoryCacheHits, 1)
}

// ResetDiskCacheStats 重置命中统计信息（不重置当前使用量）
func ResetDiskCacheStats() {
	atomic.StoreInt64(&diskCacheStats.DiskCacheHits, 0)
	atomic.StoreInt64(&diskCacheStats.MemoryCacheHits, 0)
}

// ResetDiskCacheUsage 重置磁盘缓存使用量统计（用于清理缓存后）
func ResetDiskCacheUsage() {
	atomic.StoreInt64(&diskCacheStats.ActiveDiskFiles, 0)
	atomic.StoreInt64(&diskCacheStats.CurrentDiskUsageBytes, 0)
}

// SyncDiskCacheStats 从实际磁盘状态同步统计信息
// 用于修正统计与实际不符的情况
func SyncDiskCacheStats() {
	fileCount, totalSize, err := GetDiskCacheInfo()
	if err != nil {
		return
	}
	atomic.StoreInt64(&diskCacheStats.ActiveDiskFiles, int64(fileCount))
	atomic.StoreInt64(&diskCacheStats.CurrentDiskUsageBytes, totalSize)
}

// IsDiskCacheAvailable 检查是否可以创建新的磁盘缓存
func IsDiskCacheAvailable(requestSize int64) bool {
	if !IsDiskCacheEnabled() {
		return false
	}
	return IsDiskCacheCapacityAvailable(requestSize)
}

func IsDiskCacheCapacityAvailable(requestSize int64) bool {
	if requestSize < 0 {
		return false
	}
	maxBytes := GetDiskCacheMaxSizeBytes()
	if maxBytes <= 0 {
		return true
	}
	currentUsage := atomic.LoadInt64(&diskCacheStats.CurrentDiskUsageBytes)
	return currentUsage <= maxBytes && requestSize <= maxBytes-currentUsage
}

func ReserveDiskCacheBytes(size int64) (*DiskCacheReservation, error) {
	if size < 0 {
		return nil, fmt.Errorf("%w: invalid disk cache reservation size", ErrRequestBodyTooLarge)
	}
	if size == 0 {
		return &DiskCacheReservation{}, nil
	}
	maxBytes := GetDiskCacheMaxSizeBytes()
	if maxBytes <= 0 {
		atomic.AddInt64(&diskCacheStats.CurrentDiskUsageBytes, size)
		return &DiskCacheReservation{size: size}, nil
	}
	sharedLock, err := acquireSharedDiskCacheReservationLock()
	if err != nil {
		return nil, err
	}
	if sharedLock != nil {
		defer sharedLock.release()
		fileCount, totalSize, err := sharedDiskCacheReservationUsage()
		if err != nil {
			return nil, fmt.Errorf("%w: inspect shared disk cache: %v", ErrDiskCacheCapacityUnavailable, err)
		}
		if totalSize > maxBytes || size > maxBytes-totalSize {
			return nil, ErrDiskCacheCapacityUnavailable
		}
		markerPath, err := createSharedDiskCacheReservationMarker(size)
		if err != nil {
			return nil, err
		}
		atomic.StoreInt64(&diskCacheStats.ActiveDiskFiles, int64(fileCount))
		atomic.StoreInt64(&diskCacheStats.CurrentDiskUsageBytes, totalSize+size)
		return &DiskCacheReservation{size: size, sharedMarker: markerPath}, nil
	}
	for {
		current := atomic.LoadInt64(&diskCacheStats.CurrentDiskUsageBytes)
		if current > maxBytes || size > maxBytes-current {
			return nil, ErrDiskCacheCapacityUnavailable
		}
		if atomic.CompareAndSwapInt64(&diskCacheStats.CurrentDiskUsageBytes, current, current+size) {
			return &DiskCacheReservation{size: size}, nil
		}
	}
}

func sharedDiskCacheBasePath() string {
	basePath := strings.TrimSpace(GetDiskCachePath())
	if basePath == "" {
		basePath = os.TempDir()
	}
	return basePath
}

func sharedDiskCacheReservationUsage() (fileCount int, totalSize int64, err error) {
	fileCount, totalSize, err = GetDiskCacheInfo()
	if err != nil {
		return 0, 0, err
	}
	entries, err := os.ReadDir(sharedDiskCacheBasePath())
	if err != nil {
		if os.IsNotExist(err) {
			return fileCount, totalSize, nil
		}
		return 0, 0, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), diskCacheReservationMarkerPrefix) {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		markerPath := filepath.Join(sharedDiskCacheBasePath(), entry.Name())
		if time.Since(info.ModTime()) > diskCacheReservationMarkerStale {
			_ = os.Remove(markerPath)
			continue
		}
		totalSize += info.Size()
	}
	return fileCount, totalSize, nil
}

func createSharedDiskCacheReservationMarker(size int64) (string, error) {
	markerPath := filepath.Join(sharedDiskCacheBasePath(), diskCacheReservationMarkerPrefix+GetUUID())
	file, err := os.OpenFile(markerPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("%w: create shared disk cache reservation: %v", ErrDiskCacheCapacityUnavailable, err)
	}
	if err := file.Truncate(size); err != nil {
		_ = file.Close()
		_ = os.Remove(markerPath)
		return "", fmt.Errorf("%w: size shared disk cache reservation: %v", ErrDiskCacheCapacityUnavailable, err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(markerPath)
		return "", fmt.Errorf("%w: close shared disk cache reservation: %v", ErrDiskCacheCapacityUnavailable, err)
	}
	return markerPath, nil
}

func acquireSharedDiskCacheReservationLock() (*diskCacheReservationFileLock, error) {
	if !constant.ImageTaskFileCacheShared {
		return nil, nil
	}
	basePath := sharedDiskCacheBasePath()
	if err := os.MkdirAll(basePath, 0o755); err != nil {
		return nil, fmt.Errorf("%w: create shared disk cache lock directory: %v", ErrDiskCacheCapacityUnavailable, err)
	}
	lockPath := filepath.Join(basePath, ".newapi-disk-cache-reservation.lock")
	token := fmt.Sprintf("%d:%s", os.Getpid(), GetUUID())
	deadline := time.Now().Add(diskCacheReservationLockWait)
	for {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			if _, writeErr := file.WriteString(token); writeErr != nil {
				_ = file.Close()
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("%w: write shared disk cache lock: %v", ErrDiskCacheCapacityUnavailable, writeErr)
			}
			if closeErr := file.Close(); closeErr != nil {
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("%w: close shared disk cache lock: %v", ErrDiskCacheCapacityUnavailable, closeErr)
			}
			return &diskCacheReservationFileLock{path: lockPath, token: token}, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("%w: acquire shared disk cache lock: %v", ErrDiskCacheCapacityUnavailable, err)
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > diskCacheReservationLockStale {
			stalePath := lockPath + ".stale." + GetUUID()
			if renameErr := os.Rename(lockPath, stalePath); renameErr == nil {
				_ = os.Remove(stalePath)
				continue
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%w: timed out acquiring shared disk cache lock", ErrDiskCacheCapacityUnavailable)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func (l *diskCacheReservationFileLock) release() {
	if l == nil || !atomic.CompareAndSwapInt32(&l.released, 0, 1) {
		return
	}
	content, err := os.ReadFile(l.path)
	if err != nil || strings.TrimSpace(string(content)) != l.token {
		return
	}
	_ = os.Remove(l.path)
}

func (r *DiskCacheReservation) Extend(additionalSize int64) error {
	if r == nil || additionalSize <= 0 {
		return nil
	}
	if atomic.LoadInt32(&r.done) != 0 {
		return nil
	}
	maxBytes := GetDiskCacheMaxSizeBytes()
	if maxBytes <= 0 {
		atomic.AddInt64(&diskCacheStats.CurrentDiskUsageBytes, additionalSize)
		atomic.AddInt64(&r.size, additionalSize)
		return nil
	}
	if r.sharedMarker != "" {
		sharedLock, err := acquireSharedDiskCacheReservationLock()
		if err != nil {
			return err
		}
		defer sharedLock.release()
		fileCount, totalSize, err := sharedDiskCacheReservationUsage()
		if err != nil {
			return fmt.Errorf("%w: inspect shared disk cache: %v", ErrDiskCacheCapacityUnavailable, err)
		}
		if totalSize > maxBytes || additionalSize > maxBytes-totalSize {
			return ErrDiskCacheCapacityUnavailable
		}
		newSize := atomic.LoadInt64(&r.size) + additionalSize
		if err := os.Truncate(r.sharedMarker, newSize); err != nil {
			return fmt.Errorf("%w: extend shared disk cache reservation: %v", ErrDiskCacheCapacityUnavailable, err)
		}
		atomic.StoreInt64(&r.size, newSize)
		atomic.StoreInt64(&diskCacheStats.ActiveDiskFiles, int64(fileCount))
		atomic.StoreInt64(&diskCacheStats.CurrentDiskUsageBytes, totalSize+additionalSize)
		return nil
	}
	for {
		current := atomic.LoadInt64(&diskCacheStats.CurrentDiskUsageBytes)
		if current > maxBytes || additionalSize > maxBytes-current {
			return ErrDiskCacheCapacityUnavailable
		}
		if atomic.CompareAndSwapInt64(&diskCacheStats.CurrentDiskUsageBytes, current, current+additionalSize) {
			atomic.AddInt64(&r.size, additionalSize)
			return nil
		}
	}
}

func finishSharedDiskCacheReservation(markerPath string) error {
	if markerPath == "" {
		return nil
	}
	sharedLock, err := acquireSharedDiskCacheReservationLock()
	if err != nil {
		return err
	}
	defer sharedLock.release()
	if err := os.Remove(markerPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%w: release shared disk cache reservation: %v", ErrDiskCacheCapacityUnavailable, err)
	}
	fileCount, totalSize, err := sharedDiskCacheReservationUsage()
	if err != nil {
		return fmt.Errorf("%w: refresh shared disk cache usage: %v", ErrDiskCacheCapacityUnavailable, err)
	}
	atomic.StoreInt64(&diskCacheStats.ActiveDiskFiles, int64(fileCount))
	atomic.StoreInt64(&diskCacheStats.CurrentDiskUsageBytes, totalSize)
	return nil
}

func (r *DiskCacheReservation) Commit(actualSize int64) error {
	if r == nil {
		IncrementDiskFiles(actualSize)
		return nil
	}
	if actualSize < 0 {
		return fmt.Errorf("%w: invalid disk cache file size", ErrRequestBodyTooLarge)
	}
	if !atomic.CompareAndSwapInt32(&r.done, 0, 1) {
		return nil
	}
	reservedSize := atomic.LoadInt64(&r.size)
	if r.sharedMarker != "" {
		if err := finishSharedDiskCacheReservation(r.sharedMarker); err != nil {
			atomic.StoreInt32(&r.done, 0)
			return err
		}
		if actualSize > reservedSize {
			return fmt.Errorf("%w: disk cache capacity exceeded", ErrRequestBodyTooLarge)
		}
		return nil
	}
	if actualSize > reservedSize {
		releaseDiskCacheBytes(reservedSize)
		return fmt.Errorf("%w: disk cache capacity exceeded", ErrRequestBodyTooLarge)
	}
	if unused := reservedSize - actualSize; unused > 0 {
		releaseDiskCacheBytes(unused)
	}
	atomic.AddInt64(&diskCacheStats.ActiveDiskFiles, 1)
	return nil
}

func (r *DiskCacheReservation) Release() {
	if r == nil {
		return
	}
	if atomic.CompareAndSwapInt32(&r.done, 0, 1) {
		if r.sharedMarker != "" {
			_ = finishSharedDiskCacheReservation(r.sharedMarker)
			return
		}
		releaseDiskCacheBytes(atomic.LoadInt64(&r.size))
	}
}

func releaseDiskCacheBytes(size int64) {
	if size <= 0 {
		return
	}
	if atomic.AddInt64(&diskCacheStats.CurrentDiskUsageBytes, -size) < 0 {
		atomic.StoreInt64(&diskCacheStats.CurrentDiskUsageBytes, 0)
	}
}
