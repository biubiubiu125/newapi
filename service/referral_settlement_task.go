package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	defaultReferralSettlementTickInterval = 1 * time.Minute
	minReferralSettlementTickInterval     = 10 * time.Second
)

var (
	referralSettlementOnce    sync.Once
	referralSettlementRunning atomic.Bool
)

func referralSettlementTickInterval() time.Duration {
	seconds := common.GetEnvOrDefault("REFERRAL_SETTLEMENT_INTERVAL_SECONDS", int(defaultReferralSettlementTickInterval/time.Second))
	if seconds <= 0 {
		seconds = int(defaultReferralSettlementTickInterval / time.Second)
	}
	interval := time.Duration(seconds) * time.Second
	if interval < minReferralSettlementTickInterval {
		return minReferralSettlementTickInterval
	}
	return interval
}

func StartReferralSettlementTask() {
	referralSettlementOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			interval := referralSettlementTickInterval()
			logger.LogInfo(context.Background(), fmt.Sprintf("referral settlement task started: tick=%s", interval))
			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			runReferralSettlementOnce()
			for range ticker.C {
				runReferralSettlementOnce()
			}
		})
	})
}

func runReferralSettlementOnce() {
	if !referralSettlementRunning.CompareAndSwap(false, true) {
		return
	}
	defer referralSettlementRunning.Store(false)

	ctx := context.Background()
	settled, err := ReferralRuntime.RunSettlementBatchInline()
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("referral settlement task failed: %v", err))
		return
	}
	if common.DebugEnabled && settled > 0 {
		logger.LogDebug(ctx, "referral settlement task settled=%d", settled)
	}
}
