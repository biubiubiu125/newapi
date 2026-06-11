package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	rechargeOrderMaintenanceDefaultInterval = 30 * time.Minute
	rechargeOrderMaintenanceMinInterval     = time.Minute
	rechargeOrderMaintenanceBatchSize       = 300
)

var (
	rechargeOrderMaintenanceOnce    sync.Once
	rechargeOrderMaintenanceRunning atomic.Bool
)

func rechargeOrderMaintenanceInterval() time.Duration {
	seconds := common.GetEnvOrDefault("RECHARGE_ORDER_MAINTENANCE_INTERVAL_SECONDS", int(rechargeOrderMaintenanceDefaultInterval/time.Second))
	if seconds <= 0 {
		seconds = int(rechargeOrderMaintenanceDefaultInterval / time.Second)
	}
	interval := time.Duration(seconds) * time.Second
	if interval < rechargeOrderMaintenanceMinInterval {
		return rechargeOrderMaintenanceMinInterval
	}
	return interval
}

func StartRechargeOrderMaintenanceTask() {
	rechargeOrderMaintenanceOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			interval := rechargeOrderMaintenanceInterval()
			logger.LogInfo(context.Background(), fmt.Sprintf("recharge order maintenance task started: tick=%s", interval))
			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			runRechargeOrderMaintenanceOnce()
			for range ticker.C {
				runRechargeOrderMaintenanceOnce()
			}
		})
	})
}

func runRechargeOrderMaintenanceOnce() {
	if !rechargeOrderMaintenanceRunning.CompareAndSwap(false, true) {
		return
	}
	defer rechargeOrderMaintenanceRunning.Store(false)

	ctx := context.Background()
	totalExpiredTopUps := 0
	totalExpiredSubscriptionOrders := 0
	for {
		topUps, subscriptionOrders, err := model.ExpireStalePendingRechargeOrders(model.DefaultPendingOrderExpireSeconds, rechargeOrderMaintenanceBatchSize)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("recharge order expire task failed: %v", err))
			return
		}
		totalExpiredTopUps += topUps
		totalExpiredSubscriptionOrders += subscriptionOrders
		if topUps < rechargeOrderMaintenanceBatchSize && subscriptionOrders < rechargeOrderMaintenanceBatchSize {
			break
		}
	}

	totalDeletedTopUps := 0
	totalDeletedSubscriptionOrders := 0
	for {
		topUps, subscriptionOrders, err := model.CleanupExpiredRechargeOrders(model.DefaultExpiredOrderRetentionSeconds, rechargeOrderMaintenanceBatchSize)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("recharge order cleanup task failed: %v", err))
			return
		}
		totalDeletedTopUps += topUps
		totalDeletedSubscriptionOrders += subscriptionOrders
		if topUps < rechargeOrderMaintenanceBatchSize && subscriptionOrders < rechargeOrderMaintenanceBatchSize {
			break
		}
	}

	if common.DebugEnabled && (totalExpiredTopUps > 0 || totalExpiredSubscriptionOrders > 0 || totalDeletedTopUps > 0 || totalDeletedSubscriptionOrders > 0) {
		logger.LogDebug(ctx, "recharge order maintenance: expired_topups=%d, expired_subscription_orders=%d, deleted_topups=%d, deleted_subscription_orders=%d",
			totalExpiredTopUps, totalExpiredSubscriptionOrders, totalDeletedTopUps, totalDeletedSubscriptionOrders)
	}
}
