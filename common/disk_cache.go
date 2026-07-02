package common

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// DiskCacheType 磁盘缓存类型
type DiskCacheType string

const (
	DiskCacheTypeBody DiskCacheType = "body" // 请求体缓存
	DiskCacheTypeFile DiskCacheType = "file" // 文件数据缓存
)

// 统一的缓存目录名
const diskCacheDir = "new-api-body-cache"
const imageTaskBodyCacheDir = "image-task-body-cache"
const imageTaskResultCacheDir = "image-task-result-cache"

var imageTaskSharedCacheDisabled int32

func SetImageTaskSharedCacheDisabled(disabled bool) {
	if disabled {
		atomic.StoreInt32(&imageTaskSharedCacheDisabled, 1)
		return
	}
	atomic.StoreInt32(&imageTaskSharedCacheDisabled, 0)
}

func ImageTaskSharedCacheDisabled() bool {
	return atomic.LoadInt32(&imageTaskSharedCacheDisabled) != 0
}

// GetDiskCacheDir 获取统一的磁盘缓存目录
// 注意：每次调用都会重新计算，以响应配置变化
func GetDiskCacheDir() string {
	cachePath := GetDiskCachePath()
	if cachePath == "" {
		cachePath = os.TempDir()
	}
	return filepath.Join(cachePath, diskCacheDir)
}

// EnsureDiskCacheDir 确保缓存目录存在
func EnsureDiskCacheDir() error {
	dir := GetDiskCacheDir()
	return os.MkdirAll(dir, 0755)
}

// CreateDiskCacheFile 创建磁盘缓存文件
// cacheType: 缓存类型（body/file）
// 返回文件路径和文件句柄
func CreateDiskCacheFile(cacheType DiskCacheType) (string, *os.File, error) {
	if err := EnsureDiskCacheDir(); err != nil {
		return "", nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	dir := GetDiskCacheDir()
	filename := fmt.Sprintf("%s-%s-%d.tmp", cacheType, uuid.New().String()[:8], time.Now().UnixNano())
	filePath := filepath.Join(dir, filename)

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR|os.O_EXCL, 0600)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create cache file: %w", err)
	}

	return filePath, file, nil
}

func CreateDiskCacheFileWithReservation(cacheType DiskCacheType, reserveBytes int64) (string, *os.File, *DiskCacheReservation, error) {
	reservation, err := ReserveDiskCacheBytes(reserveBytes)
	if err != nil {
		return "", nil, nil, err
	}
	filePath, file, err := CreateDiskCacheFile(cacheType)
	if err != nil {
		reservation.Release()
		return "", nil, nil, err
	}
	return filePath, file, reservation, nil
}

// WriteDiskCacheFile 写入数据到磁盘缓存文件
// 返回文件路径
func WriteDiskCacheFile(cacheType DiskCacheType, data []byte) (string, error) {
	reservation, err := ReserveDiskCacheBytes(int64(len(data)))
	if err != nil {
		return "", err
	}
	committed := false
	defer func() {
		if !committed {
			reservation.Release()
		}
	}()
	filePath, file, err := CreateDiskCacheFile(cacheType)
	if err != nil {
		return "", err
	}

	n, err := file.Write(data)
	if err != nil {
		file.Close()
		os.Remove(filePath)
		return "", fmt.Errorf("failed to write cache file: %w", err)
	}
	if n != len(data) {
		file.Close()
		os.Remove(filePath)
		return "", fmt.Errorf("failed to write cache file: %w", io.ErrShortWrite)
	}

	if err := file.Close(); err != nil {
		os.Remove(filePath)
		return "", fmt.Errorf("failed to close cache file: %w", err)
	}
	if err := reservation.Commit(int64(n)); err != nil {
		os.Remove(filePath)
		return "", err
	}
	committed = true

	return filePath, nil
}

func GetImageTaskBodyCacheDir() string {
	return filepath.Join(getImageTaskCacheBaseDir(), imageTaskBodyCacheDir)
}

func GetImageTaskResultCacheDir() string {
	return filepath.Join(getImageTaskCacheBaseDir(), imageTaskResultCacheDir)
}

func getImageTaskCacheBaseDir() string {
	cachePath := strings.TrimSpace(GetDiskCachePath())
	if cachePath == "" {
		cachePath = defaultImageTaskBodyCacheBaseDir()
	}
	return cachePath
}

func defaultImageTaskBodyCacheBaseDir() string {
	if runtime.GOOS != "windows" {
		if stat, err := os.Stat("/data"); err == nil && stat.IsDir() {
			return "/data"
		}
	}
	return os.TempDir()
}

func CreateImageTaskBodyCacheFile() (string, *os.File, error) {
	dir := GetImageTaskBodyCacheDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", nil, fmt.Errorf("failed to create image task body cache directory: %w", err)
	}
	filename := fmt.Sprintf("%s-%s-%d.tmp", DiskCacheTypeBody, uuid.New().String()[:8], time.Now().UnixNano())
	filePath := filepath.Join(dir, filename)
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR|os.O_EXCL, 0600)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create image task body cache file: %w", err)
	}
	return filePath, file, nil
}

func CreateImageTaskBodyCacheFileWithReservation(reserveBytes int64) (string, *os.File, *DiskCacheReservation, error) {
	reservation, err := ReserveDiskCacheBytes(reserveBytes)
	if err != nil {
		return "", nil, nil, err
	}
	path, file, err := CreateImageTaskBodyCacheFile()
	if err != nil {
		reservation.Release()
		return "", nil, nil, err
	}
	return path, file, reservation, nil
}

func WriteImageTaskBodyCacheFile(data []byte) (string, error) {
	reservation, err := ReserveDiskCacheBytes(int64(len(data)))
	if err != nil {
		return "", err
	}
	committed := false
	defer func() {
		if !committed {
			reservation.Release()
		}
	}()
	filePath, file, err := CreateImageTaskBodyCacheFile()
	if err != nil {
		return "", err
	}
	n, err := file.Write(data)
	if err != nil {
		file.Close()
		os.Remove(filePath)
		return "", fmt.Errorf("failed to write image task body cache file: %w", err)
	}
	if n != len(data) {
		file.Close()
		os.Remove(filePath)
		return "", fmt.Errorf("failed to write image task body cache file: %w", io.ErrShortWrite)
	}
	if err := file.Close(); err != nil {
		os.Remove(filePath)
		return "", fmt.Errorf("failed to close image task body cache file: %w", err)
	}
	if err := reservation.Commit(int64(n)); err != nil {
		os.Remove(filePath)
		return "", err
	}
	committed = true
	return filePath, nil
}

func CreateImageTaskResultCacheFile() (string, *os.File, error) {
	dir := GetImageTaskResultCacheDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", nil, fmt.Errorf("failed to create image task result cache directory: %w", err)
	}
	filename := fmt.Sprintf("result-%s-%d.json", uuid.New().String()[:8], time.Now().UnixNano())
	filePath := filepath.Join(dir, filename)
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR|os.O_EXCL, 0600)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create image task result cache file: %w", err)
	}
	return filePath, file, nil
}

func CreateImageTaskResultCacheFileWithReservation(reserveBytes int64) (string, *os.File, *DiskCacheReservation, error) {
	reservation, err := ReserveDiskCacheBytes(reserveBytes)
	if err != nil {
		return "", nil, nil, err
	}
	path, file, err := CreateImageTaskResultCacheFile()
	if err != nil {
		reservation.Release()
		return "", nil, nil, err
	}
	return path, file, reservation, nil
}

func WriteImageTaskResultCacheFile(data []byte) (string, error) {
	reservation, err := ReserveDiskCacheBytes(int64(len(data)))
	if err != nil {
		return "", err
	}
	committed := false
	defer func() {
		if !committed {
			reservation.Release()
		}
	}()
	filePath, file, err := CreateImageTaskResultCacheFile()
	if err != nil {
		return "", err
	}
	n, err := file.Write(data)
	if err != nil {
		file.Close()
		os.Remove(filePath)
		return "", fmt.Errorf("failed to write image task result cache file: %w", err)
	}
	if n != len(data) {
		file.Close()
		os.Remove(filePath)
		return "", fmt.Errorf("failed to write image task result cache file: %w", io.ErrShortWrite)
	}
	if err := file.Close(); err != nil {
		os.Remove(filePath)
		return "", fmt.Errorf("failed to close image task result cache file: %w", err)
	}
	if err := reservation.Commit(int64(n)); err != nil {
		os.Remove(filePath)
		return "", err
	}
	committed = true
	return filePath, nil
}

func ValidateImageTaskSharedCache() error {
	if err := validateImageTaskCacheProbe(CreateImageTaskBodyCacheFile); err != nil {
		return err
	}
	if err := validateImageTaskCacheProbe(CreateImageTaskResultCacheFile); err != nil {
		return err
	}
	return nil
}

func validateImageTaskCacheProbe(create func() (string, *os.File, error)) error {
	path, file, err := create()
	if err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(path)
	}()
	if _, err := file.Write([]byte("probe")); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return nil
}

// WriteDiskCacheFileString 写入字符串到磁盘缓存文件
func WriteDiskCacheFileString(cacheType DiskCacheType, data string) (string, error) {
	return WriteDiskCacheFile(cacheType, []byte(data))
}

// ReadDiskCacheFile 读取磁盘缓存文件
func ReadDiskCacheFile(filePath string) ([]byte, error) {
	return os.ReadFile(filePath)
}

// ReadDiskCacheFileString 读取磁盘缓存文件为字符串
func ReadDiskCacheFileString(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// RemoveDiskCacheFile 删除磁盘缓存文件
func RemoveDiskCacheFile(filePath string) error {
	info, statErr := os.Stat(filePath)
	err := os.Remove(filePath)
	if err == nil && statErr == nil {
		DecrementDiskFiles(info.Size())
	}
	return err
}

// CleanupOldDiskCacheFiles 清理旧的缓存文件
// maxAge: 文件最大存活时间
// 注意：此函数只删除文件，不更新统计（因为无法知道每个文件的原始大小）
func CleanupOldDiskCacheFiles(maxAge time.Duration) error {
	dir := GetDiskCacheDir()

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 目录不存在，无需清理
		}
		return err
	}

	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > maxAge {
			// 注意：后台清理任务删除文件时，由于无法得知原始 base64Size，
			// 只能按磁盘文件大小扣减。这在目前 base64 存储模式下是准确的。
			if err := os.Remove(filepath.Join(dir, entry.Name())); err == nil {
				DecrementDiskFiles(info.Size())
			}
		}
	}
	return nil
}

func CleanupOldImageTaskBodyCacheFiles(maxAge time.Duration, keepPaths ...map[string]struct{}) error {
	return cleanupOldFilesInDir(GetImageTaskBodyCacheDir(), maxAge, firstKeepPathSet(keepPaths), DecrementDiskFiles)
}

func CleanupOldImageTaskResultCacheFiles(maxAge time.Duration, keepPaths ...map[string]struct{}) error {
	return cleanupOldFilesInDir(GetImageTaskResultCacheDir(), maxAge, firstKeepPathSet(keepPaths), DecrementDiskFiles)
}

func firstKeepPathSet(keepPaths []map[string]struct{}) map[string]struct{} {
	if len(keepPaths) == 0 {
		return nil
	}
	return keepPaths[0]
}

func cleanupOldFilesInDir(dir string, maxAge time.Duration, keepPaths map[string]struct{}, onRemove func(size int64)) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) <= maxAge {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if _, keep := keepPaths[filepath.Clean(path)]; keep {
			continue
		}
		if err := os.Remove(path); err == nil && onRemove != nil {
			onRemove(info.Size())
		}
	}
	return nil
}

// GetDiskCacheInfo 获取磁盘缓存目录信息
func GetExpiredImageTaskBodyCachePaths(maxAge time.Duration) (map[string]struct{}, error) {
	return collectOldFilesInDir(GetImageTaskBodyCacheDir(), maxAge)
}

func GetExpiredImageTaskResultCachePaths(maxAge time.Duration) (map[string]struct{}, error) {
	return collectOldFilesInDir(GetImageTaskResultCacheDir(), maxAge)
}

func collectOldFilesInDir(dir string, maxAge time.Duration) (map[string]struct{}, error) {
	paths := make(map[string]struct{})
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return paths, nil
		}
		return nil, err
	}
	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > maxAge {
			paths[filepath.Clean(filepath.Join(dir, entry.Name()))] = struct{}{}
		}
	}
	return paths, nil
}

func GetDiskCacheInfo() (fileCount int, totalSize int64, err error) {
	seen := make(map[string]struct{})
	for _, dir := range []string{GetDiskCacheDir(), GetImageTaskBodyCacheDir(), GetImageTaskResultCacheDir()} {
		dir = filepath.Clean(dir)
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		count, size, dirErr := getDiskCacheInfoForDir(dir)
		if dirErr != nil {
			return 0, 0, dirErr
		}
		fileCount += count
		totalSize += size
	}
	return fileCount, totalSize, nil
}

func getDiskCacheInfoForDir(dir string) (fileCount int, totalSize int64, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		fileCount++
		totalSize += info.Size()
	}
	return fileCount, totalSize, nil
}

// ShouldUseDiskCache 判断是否应该使用磁盘缓存
func ShouldUseDiskCache(dataSize int64) bool {
	if !IsDiskCacheEnabled() {
		return false
	}
	threshold := GetDiskCacheThresholdBytes()
	if dataSize < threshold {
		return false
	}
	return IsDiskCacheAvailable(dataSize)
}
