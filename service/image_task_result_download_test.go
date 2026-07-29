package service

import (
	"errors"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestAcquireImageTaskResultDownloadSlotEnforcesGlobalAndTokenLimits(t *testing.T) {
	oldGlobal := constant.ImageTaskResultDownloadConcurrency
	oldToken := constant.ImageTaskResultDownloadTokenConcurrency
	constant.ImageTaskResultDownloadConcurrency = 2
	constant.ImageTaskResultDownloadTokenConcurrency = 1
	t.Cleanup(func() {
		constant.ImageTaskResultDownloadConcurrency = oldGlobal
		constant.ImageTaskResultDownloadTokenConcurrency = oldToken
		ResetImageTaskResultDownloadLimiterForTest()
	})
	ResetImageTaskResultDownloadLimiterForTest()

	releaseA, err := AcquireImageTaskResultDownloadSlot(11)
	require.NoError(t, err)
	_, err = AcquireImageTaskResultDownloadSlot(11)
	require.ErrorIs(t, err, ErrImageTaskResultDownloadBusy)

	releaseB, err := AcquireImageTaskResultDownloadSlot(12)
	require.NoError(t, err)
	_, err = AcquireImageTaskResultDownloadSlot(13)
	require.ErrorIs(t, err, ErrImageTaskResultDownloadBusy)

	releaseA()
	releaseC, err := AcquireImageTaskResultDownloadSlot(11)
	require.NoError(t, err)
	releaseB()
	releaseC()
}

func TestAcquireImageTaskResultDownloadSlotReleaseIsIdempotent(t *testing.T) {
	oldGlobal := constant.ImageTaskResultDownloadConcurrency
	constant.ImageTaskResultDownloadConcurrency = 1
	constant.ImageTaskResultDownloadTokenConcurrency = 0
	t.Cleanup(func() {
		constant.ImageTaskResultDownloadConcurrency = oldGlobal
		ResetImageTaskResultDownloadLimiterForTest()
	})
	ResetImageTaskResultDownloadLimiterForTest()

	release, err := AcquireImageTaskResultDownloadSlot(1)
	require.NoError(t, err)
	release()
	release()

	release2, err := AcquireImageTaskResultDownloadSlot(1)
	require.NoError(t, err)
	release2()
}

func TestAcquireImageTaskResultDownloadSlotZeroDisablesLimits(t *testing.T) {
	oldGlobal := constant.ImageTaskResultDownloadConcurrency
	oldToken := constant.ImageTaskResultDownloadTokenConcurrency
	constant.ImageTaskResultDownloadConcurrency = 0
	constant.ImageTaskResultDownloadTokenConcurrency = 0
	t.Cleanup(func() {
		constant.ImageTaskResultDownloadConcurrency = oldGlobal
		constant.ImageTaskResultDownloadTokenConcurrency = oldToken
		ResetImageTaskResultDownloadLimiterForTest()
	})
	ResetImageTaskResultDownloadLimiterForTest()

	var releases []func()
	for i := 0; i < 8; i++ {
		release, err := AcquireImageTaskResultDownloadSlot(99)
		require.NoError(t, err)
		releases = append(releases, release)
	}
	for _, release := range releases {
		release()
	}
}

func TestAcquireImageTaskResultDownloadSlotConcurrentTokenIsolation(t *testing.T) {
	oldGlobal := constant.ImageTaskResultDownloadConcurrency
	oldToken := constant.ImageTaskResultDownloadTokenConcurrency
	constant.ImageTaskResultDownloadConcurrency = 4
	constant.ImageTaskResultDownloadTokenConcurrency = 1
	t.Cleanup(func() {
		constant.ImageTaskResultDownloadConcurrency = oldGlobal
		constant.ImageTaskResultDownloadTokenConcurrency = oldToken
		ResetImageTaskResultDownloadLimiterForTest()
	})
	ResetImageTaskResultDownloadLimiterForTest()

	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for tokenID := 1; tokenID <= 4; tokenID++ {
		wg.Add(1)
		go func(tokenID int) {
			defer wg.Done()
			release, err := AcquireImageTaskResultDownloadSlot(tokenID)
			if err != nil {
				errs <- err
				return
			}
			release()
		}(tokenID)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
		require.False(t, errors.Is(err, ErrImageTaskResultDownloadBusy))
	}
}
