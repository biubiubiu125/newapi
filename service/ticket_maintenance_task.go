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
	ticketAutoCloseAfter                 = 24 * time.Hour
	ticketMaintenanceDefaultInterval     = time.Hour
	ticketMaintenanceMinInterval         = time.Minute
	ticketMaintenanceBatchSize           = 300
	ticketMaintenanceIntervalEnvVariable = "TICKET_MAINTENANCE_INTERVAL_SECONDS"
)

var (
	ticketMaintenanceOnce    sync.Once
	ticketMaintenanceRunning atomic.Bool
)

func ticketMaintenanceInterval() time.Duration {
	seconds := common.GetEnvOrDefault(ticketMaintenanceIntervalEnvVariable, int(ticketMaintenanceDefaultInterval/time.Second))
	if seconds <= 0 {
		seconds = int(ticketMaintenanceDefaultInterval / time.Second)
	}
	interval := time.Duration(seconds) * time.Second
	if interval < ticketMaintenanceMinInterval {
		return ticketMaintenanceMinInterval
	}
	return interval
}

func StartTicketMaintenanceTask() {
	ticketMaintenanceOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			interval := ticketMaintenanceInterval()
			logger.LogInfo(context.Background(), fmt.Sprintf("ticket maintenance task started: tick=%s", interval))
			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			runTicketMaintenanceOnce()
			for range ticker.C {
				runTicketMaintenanceOnce()
			}
		})
	})
}

func runTicketMaintenanceOnce() {
	if !ticketMaintenanceRunning.CompareAndSwap(false, true) {
		return
	}
	defer ticketMaintenanceRunning.Store(false)

	ctx := context.Background()
	now := common.GetTimestamp()
	before := now - int64(ticketAutoCloseAfter/time.Second)
	var totalClosed int64
	for {
		closed, err := model.AutoCloseTicketsWithoutUserReply(before, now, ticketMaintenanceBatchSize)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("ticket auto-close task failed: %v", err))
			return
		}
		totalClosed += closed
		if closed < ticketMaintenanceBatchSize {
			break
		}
	}

	if common.DebugEnabled && totalClosed > 0 {
		logger.LogDebug(ctx, "ticket maintenance: auto_closed=%d", totalClosed)
	}
}
