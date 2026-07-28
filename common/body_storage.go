package common

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/constant"
)

// BodyStorage 请求体存储接口
type BodyStorage interface {
	io.ReadSeeker
	io.Closer
	// Bytes 获取全部内容
	Bytes() ([]byte, error)
	// Size 获取数据大小
	Size() int64
	// IsDisk 是否是磁盘存储
	IsDisk() bool
}

// ErrStorageClosed 存储已关闭错误
var ErrStorageClosed = fmt.Errorf("body storage is closed")

// memoryStorage 内存存储实现
type memoryStorage struct {
	data   []byte
	reader *bytes.Reader
	size   int64
	closed int32
	mu     sync.Mutex
}

func newMemoryStorage(data []byte) *memoryStorage {
	size := int64(len(data))
	IncrementMemoryBuffers(size)
	return &memoryStorage{
		data:   data,
		reader: bytes.NewReader(data),
		size:   size,
	}
}

func (m *memoryStorage) Read(p []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if atomic.LoadInt32(&m.closed) == 1 {
		return 0, ErrStorageClosed
	}
	return m.reader.Read(p)
}

func (m *memoryStorage) Seek(offset int64, whence int) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if atomic.LoadInt32(&m.closed) == 1 {
		return 0, ErrStorageClosed
	}
	return m.reader.Seek(offset, whence)
}

func (m *memoryStorage) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if atomic.CompareAndSwapInt32(&m.closed, 0, 1) {
		DecrementMemoryBuffers(m.size)
	}
	return nil
}

func (m *memoryStorage) Bytes() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if atomic.LoadInt32(&m.closed) == 1 {
		return nil, ErrStorageClosed
	}
	return m.data, nil
}

func (m *memoryStorage) Size() int64 {
	return m.size
}

func (m *memoryStorage) IsDisk() bool {
	return false
}

// diskStorage 磁盘存储实现
type diskStorage struct {
	file     *os.File
	filePath string
	size     int64
	closed   int32
	mu       sync.Mutex
}

func newDiskStorage(data []byte, cachePath string) (*diskStorage, error) {
	reservation, err := ReserveDiskCacheBytes(int64(len(data)))
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			reservation.Release()
		}
	}()
	// 使用统一的缓存目录管理
	filePath, file, err := CreateDiskCacheFile(DiskCacheTypeBody)
	if err != nil {
		return nil, err
	}

	// 写入数据
	n, err := file.Write(data)
	if err != nil {
		file.Close()
		os.Remove(filePath)
		return nil, fmt.Errorf("failed to write to temp file: %w", err)
	}
	if n != len(data) {
		file.Close()
		os.Remove(filePath)
		return nil, io.ErrShortWrite
	}

	// 重置文件指针
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		os.Remove(filePath)
		return nil, fmt.Errorf("failed to seek temp file: %w", err)
	}

	size := int64(n)
	if err := reservation.Commit(size); err != nil {
		file.Close()
		os.Remove(filePath)
		return nil, err
	}
	committed = true

	return &diskStorage{
		file:     file,
		filePath: filePath,
		size:     size,
	}, nil
}

func newDiskStorageFromReader(reader io.Reader, maxBytes int64, cachePath string, diskMaxBytes int64) (*diskStorage, error) {
	if maxBytes <= 0 {
		return nil, ErrRequestBodyTooLarge
	}
	reserveBytes := diskMaxBytes
	if reserveBytes <= 0 || reserveBytes > maxBytes {
		reserveBytes = maxBytes
	}
	reservation, err := ReserveDiskCacheBytes(reserveBytes)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			reservation.Release()
		}
	}()
	// 使用统一的缓存目录管理
	filePath, file, err := CreateDiskCacheFile(DiskCacheTypeBody)
	if err != nil {
		return nil, err
	}

	written, err := copyReaderToReservedDiskCache(file, reader, maxBytes, reservation, reserveBytes)
	if err != nil {
		file.Close()
		os.Remove(filePath)
		return nil, err
	}

	// 重置文件指针
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		os.Remove(filePath)
		return nil, fmt.Errorf("failed to seek temp file: %w", err)
	}

	if err := reservation.Commit(written); err != nil {
		file.Close()
		os.Remove(filePath)
		return nil, err
	}
	committed = true

	return &diskStorage{
		file:     file,
		filePath: filePath,
		size:     written,
	}, nil
}

func copyReaderToReservedDiskCache(file *os.File, reader io.Reader, maxBytes int64, reservation *DiskCacheReservation, reservedBytes int64) (int64, error) {
	if maxBytes <= 0 {
		return 0, ErrRequestBodyTooLarge
	}
	if reservedBytes <= 0 {
		return 0, ErrDiskCacheCapacityUnavailable
	}
	buf := make([]byte, 32*1024)
	written := int64(0)
	for {
		if written >= maxBytes {
			n, err := reader.Read(buf[:1])
			if n > 0 {
				return written, ErrRequestBodyTooLarge
			}
			if err == nil {
				return written, io.ErrNoProgress
			}
			if err == io.EOF {
				return written, nil
			}
			if err != nil {
				return written, fmt.Errorf("failed to read from source: %w", err)
			}
			continue
		}
		if written >= reservedBytes {
			extendBytes := int64(1 << 20)
			if remaining := maxBytes - reservedBytes; remaining < extendBytes {
				extendBytes = remaining
			}
			if extendBytes <= 0 {
				continue
			}
			if err := reservation.Extend(extendBytes); err != nil {
				return written, err
			}
			reservedBytes += extendBytes
		}
		readBytes := reservedBytes - written
		if remaining := maxBytes - written; remaining < readBytes {
			readBytes = remaining
		}
		if readBytes > int64(len(buf)) {
			readBytes = int64(len(buf))
		}
		n, readErr := reader.Read(buf[:int(readBytes)])
		if n > 0 {
			wn, writeErr := file.Write(buf[:n])
			written += int64(wn)
			if writeErr != nil {
				return written, fmt.Errorf("failed to write to temp file: %w", writeErr)
			}
			if wn != n {
				return written, io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			return written, nil
		}
		if readErr != nil {
			return written, fmt.Errorf("failed to read from source: %w", readErr)
		}
		if n == 0 {
			return written, io.ErrNoProgress
		}
	}
}

func (d *diskStorage) Read(p []byte) (n int, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if atomic.LoadInt32(&d.closed) == 1 {
		return 0, ErrStorageClosed
	}
	return d.file.Read(p)
}

func (d *diskStorage) Seek(offset int64, whence int) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if atomic.LoadInt32(&d.closed) == 1 {
		return 0, ErrStorageClosed
	}
	return d.file.Seek(offset, whence)
}

func (d *diskStorage) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if atomic.CompareAndSwapInt32(&d.closed, 0, 1) {
		d.file.Close()
		os.Remove(d.filePath)
		DecrementDiskFiles(d.size)
	}
	return nil
}

func (d *diskStorage) Bytes() ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if atomic.LoadInt32(&d.closed) == 1 {
		return nil, ErrStorageClosed
	}

	// 保存当前位置
	currentPos, err := d.file.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}

	// 移动到开头
	if _, err := d.file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	// 读取全部内容
	data := make([]byte, d.size)
	_, err = io.ReadFull(d.file, data)
	if err != nil {
		return nil, err
	}

	// 恢复位置
	if _, err := d.file.Seek(currentPos, io.SeekStart); err != nil {
		return nil, err
	}

	return data, nil
}

func (d *diskStorage) Size() int64 {
	return d.size
}

func (d *diskStorage) IsDisk() bool {
	return true
}

// CreateBodyStorage 根据数据大小创建合适的存储
func CreateBodyStorage(data []byte) (BodyStorage, error) {
	size := int64(len(data))
	threshold := GetDiskCacheThresholdBytes()

	// 检查是否应该使用磁盘缓存
	if IsDiskCacheEnabled() &&
		size >= threshold &&
		IsDiskCacheAvailable(size) {
		storage, err := newDiskStorage(data, GetDiskCachePath())
		if err != nil {
			// 如果磁盘存储失败，回退到内存存储
			SysError(fmt.Sprintf("failed to create disk storage, falling back to memory: %v", err))
			return newMemoryStorage(data), nil
		}
		return storage, nil
	}

	return newMemoryStorage(data), nil
}

// CreateBodyStorageFromReader 从 Reader 创建存储（用于大请求的流式处理）
func CreateBodyStorageFromReader(reader io.Reader, contentLength int64, maxBytes int64) (BodyStorage, error) {
	threshold := GetDiskCacheThresholdBytes()
	expectedSize := contentLength
	if expectedSize <= 0 {
		expectedSize = maxBytes
	}

	// 如果启用了磁盘缓存且内容可能超过阈值，直接使用磁盘存储。
	// Content-Length 缺失时按 maxBytes 评估，避免 chunked 大请求先堆到内存。
	if IsDiskCacheEnabled() &&
		expectedSize >= threshold {
		reserveBytes := expectedSize
		if contentLength <= 0 {
			reserveBytes = threshold
			if reserveBytes <= 0 {
				reserveBytes = 1 << 20
			}
		}
		if reserveBytes <= 0 || reserveBytes > maxBytes {
			reserveBytes = maxBytes
		}
		storage, err := newDiskStorageFromReader(reader, maxBytes, GetDiskCachePath(), reserveBytes)
		if err != nil {
			if IsRequestBodyTooLargeError(err) {
				return nil, err
			}
			// 磁盘存储失败，reader 已被消费，无法安全回退
			// 直接返回错误而非尝试回退（因为 reader 数据已丢失）
			return nil, fmt.Errorf("disk storage creation failed: %w", err)
		}
		IncrementDiskCacheHits()
		return storage, nil
	}

	// 使用内存读取
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, ErrRequestBodyTooLarge
	}

	storage, err := CreateBodyStorage(data)
	if err != nil {
		return nil, err
	}
	// 如果最终使用内存存储，记录内存缓存命中
	if !storage.IsDisk() {
		IncrementMemoryCacheHits()
	} else {
		IncrementDiskCacheHits()
	}
	return storage, nil
}

func CreateDiskBodyStorageFromReader(reader io.Reader, maxBytes int64) (BodyStorage, error) {
	return CreateDiskBodyStorageFromReaderWithReservation(reader, maxBytes, maxBytes)
}

func CreateDiskBodyStorageFromReaderWithReservation(reader io.Reader, maxBytes int64, reserveBytes int64) (BodyStorage, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("%w: disk body storage requires a positive limit", ErrRequestBodyTooLarge)
	}
	if reserveBytes <= 0 || reserveBytes > maxBytes {
		reserveBytes = maxBytes
	}
	return newDiskStorageFromReader(reader, maxBytes, GetDiskCachePath(), reserveBytes)
}

func diskCacheAvailableBytes() int64 {
	maxBytes := GetDiskCacheMaxSizeBytes()
	currentUsage := atomic.LoadInt64(&diskCacheStats.CurrentDiskUsageBytes)
	if maxBytes <= currentUsage {
		return 0
	}
	return maxBytes - currentUsage
}

// ReaderOnly wraps an io.Reader to hide io.Closer, preventing http.NewRequest
// from type-asserting io.ReadCloser and closing the underlying BodyStorage.
func ReaderOnly(r io.Reader) io.Reader {
	return struct{ io.Reader }{r}
}

func GetImageTaskResultCacheRetention() time.Duration {
	const maxRetention = 12 * time.Hour
	if constant.ImageTaskResultRetentionMinutes > 0 {
		retention := time.Duration(constant.ImageTaskResultRetentionMinutes) * time.Minute
		if retention > 0 && retention < maxRetention {
			return retention
		}
	}
	return maxRetention
}

// GetImageTaskIdempotencyReuseWindow 返回终态图片任务被 client_task_id / Idempotency-Key
// 复用的时间窗口，与结果保留期对齐。
//
// 窗口外的旧任务结果早已被清理，如果继续把它当作幂等命中返回，这个键就永远无法再生成
// 新图（GET /result 恒返回 410）。注意窗口只对终态任务生效：执行中的任务无论多久都必须
// 命中复用，否则同键重试会在长任务上重复创建并重复扣费。
func GetImageTaskIdempotencyReuseWindow() time.Duration {
	return GetImageTaskResultCacheRetention()
}

func GetImageTaskBodyCacheRetention() time.Duration {
	retention := GetImageTaskResultCacheRetention()
	if constant.TaskTimeoutMinutes > 0 {
		taskTimeout := time.Duration(constant.TaskTimeoutMinutes) * time.Minute
		if taskTimeout > retention {
			retention = taskTimeout
		}
	}
	return retention + time.Hour
}

func CleanupExpiredImageTaskBodyCacheFiles(keepPaths map[string]struct{}) error {
	return CleanupOldImageTaskBodyCacheFiles(GetImageTaskBodyCacheRetention(), keepPaths)
}

func CleanupExpiredImageTaskResultCacheFiles() error {
	return CleanupOldImageTaskResultCacheFiles(GetImageTaskResultCacheRetention())
}

func CleanupExpiredImageTaskResultCacheFilesWithKeep(keepPaths map[string]struct{}) error {
	return CleanupOldImageTaskResultCacheFiles(GetImageTaskResultCacheRetention(), keepPaths)
}

// CleanupOldCacheFiles 清理旧的缓存文件（用于启动时清理残留）
func CleanupOldCacheFiles() {
	// 使用统一的缓存管理
	CleanupOldDiskCacheFiles(5 * time.Minute)
}
