package service

import (
	"errors"
	"fmt"
	"sync"

	"github.com/QuantumNous/new-api/constant"
)

var (
	// ErrImageTaskResultDownloadBusy means the process or token already holds the
	// maximum number of concurrent result downloads.
	ErrImageTaskResultDownloadBusy = errors.New("image task result download capacity is full")
)

type imageTaskResultDownloadLimiter struct {
	mu           sync.Mutex
	globalActive int64
	tokenActive  map[int]int64
}

var imageTaskResultDownloads = &imageTaskResultDownloadLimiter{
	tokenActive: make(map[int]int64),
}

// AcquireImageTaskResultDownloadSlot reserves one global slot and one per-token
// slot for serving a public image-task result. release must be called exactly once.
// A limit of 0 disables that dimension.
func AcquireImageTaskResultDownloadSlot(tokenID int) (release func(), err error) {
	return imageTaskResultDownloads.acquire(tokenID, imageTaskResultDownloadGlobalLimit(), imageTaskResultDownloadTokenLimit())
}

func imageTaskResultDownloadGlobalLimit() int64 {
	if constant.ImageTaskResultDownloadConcurrency <= 0 {
		return 0
	}
	return int64(constant.ImageTaskResultDownloadConcurrency)
}

func imageTaskResultDownloadTokenLimit() int64 {
	if constant.ImageTaskResultDownloadTokenConcurrency <= 0 {
		return 0
	}
	return int64(constant.ImageTaskResultDownloadTokenConcurrency)
}

func (l *imageTaskResultDownloadLimiter) acquire(tokenID int, globalLimit, tokenLimit int64) (func(), error) {
	if l == nil {
		return func() {}, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if globalLimit > 0 && l.globalActive >= globalLimit {
		return nil, fmt.Errorf("%w: global limit %d", ErrImageTaskResultDownloadBusy, globalLimit)
	}
	if tokenLimit > 0 && tokenID > 0 && l.tokenActive[tokenID] >= tokenLimit {
		return nil, fmt.Errorf("%w: token limit %d", ErrImageTaskResultDownloadBusy, tokenLimit)
	}

	l.globalActive++
	if tokenID > 0 {
		l.tokenActive[tokenID]++
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			if l.globalActive > 0 {
				l.globalActive--
			}
			if tokenID > 0 {
				if current := l.tokenActive[tokenID]; current <= 1 {
					delete(l.tokenActive, tokenID)
				} else {
					l.tokenActive[tokenID] = current - 1
				}
			}
		})
	}, nil
}

// ResetImageTaskResultDownloadLimiterForTest clears download concurrency state.
// Tests only.
func ResetImageTaskResultDownloadLimiterForTest() {
	imageTaskResultDownloads.mu.Lock()
	defer imageTaskResultDownloads.mu.Unlock()
	imageTaskResultDownloads.globalActive = 0
	imageTaskResultDownloads.tokenActive = make(map[int]int64)
}
