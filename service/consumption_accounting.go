package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

func RollbackTaskConsumptionUsage(userId int, channelId int, tokenId int, quota int) error {
	if err := model.UpdateTaskConsumptionUsageRollbackWithTokenSync(userId, channelId, tokenId, quota); err != nil {
		return fmt.Errorf("rollback task consumption usage failed: %w", err)
	}
	return nil
}

func RollbackBillingSettlement(ctx context.Context, relayInfo *relaycommon.RelayInfo, quota int) error {
	if relayInfo == nil || quota == 0 {
		return nil
	}
	if relayInfo.Billing != nil {
		if rollbacker, ok := relayInfo.Billing.(interface{ Rollback(int) error }); ok {
			return rollbacker.Rollback(quota)
		}
	}
	if err := PostConsumeQuota(relayInfo, -quota, quota, false); err != nil {
		return fmt.Errorf("rollback billing settlement failed: %w", err)
	}
	return nil
}

func RollbackDirectPostConsumeQuota(ctx context.Context, relayInfo *relaycommon.RelayInfo, quota int) error {
	if relayInfo == nil || quota == 0 {
		return nil
	}
	if err := PostConsumeQuota(relayInfo, -quota, quota, false); err != nil {
		return fmt.Errorf("rollback direct post consume quota failed: %w", err)
	}
	return nil
}

func wrapUsageCounterUpdateError(ctx context.Context, relayInfo *relaycommon.RelayInfo, quota int, settlementSucceeded bool, err error, prefix string) error {
	if err == nil {
		return nil
	}
	if settlementSucceeded {
		if rollbackErr := RollbackBillingSettlement(ctx, relayInfo, quota); rollbackErr != nil {
			return fmt.Errorf("%s: %w; rollback billing failed: %v", prefix, err, rollbackErr)
		}
	}
	return fmt.Errorf("%s: %w", prefix, err)
}

// keepDeliveredUsageCounterError leaves a delivered text, audio, or realtime charge in place when only the usage counters failed.
func keepDeliveredUsageCounterError(err error) error {
	if err == nil {
		return nil
	}
	// 用量计数失败和消费日志失败一样，都不能把已经交付的扣费退掉。
	return fmt.Errorf("usage counter update failed: %w", errors.Join(ErrDeliveredConsumeLogKept, err))
}

// ErrDeliveredConsumeLogKept marks a consume-log failure that must not refund a delivered charge.
var ErrDeliveredConsumeLogKept = errors.New("delivered consume log kept")

// DeliveredConsumeLogKept reports a consume-log failure that left the charge in place.
func DeliveredConsumeLogKept(err error) bool {
	return errors.Is(err, ErrDeliveredConsumeLogKept)
}

// keepDeliveredConsumeLogError leaves a delivered charge in place when only the consume log failed.
func keepDeliveredConsumeLogError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("record consume log failed: %w", errors.Join(ErrDeliveredConsumeLogKept, err))
}

// recordDeliveredConsumption writes usage counters and the consume log for a charge that was already accepted.
// 失败时留下审计行，主节点稍后补记，不能把已经交付的钱退掉。
func recordDeliveredConsumption(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, params model.RecordConsumeLogParams) error {
	if relayInfo == nil {
		return keepDeliveredUsageCounterError(errors.New("relay info is required"))
	}
	req := model.NewConsumptionAuditRequest(ctx, relayInfo.RequestId, relayInfo.UserId, params)
	if err := model.EnsureConsumptionAudit(req); err != nil {
		return keepDeliveredUsageCounterError(err)
	}
	if err := model.ApplyConsumptionAuditUsage(req.IdempotencyKey); err != nil {
		return keepDeliveredUsageCounterError(err)
	}
	if err := model.CompleteConsumptionAudit(req.IdempotencyKey); err != nil {
		return keepDeliveredConsumeLogError(err)
	}
	return nil
}

func wrapRecordConsumeLogError(ctx context.Context, relayInfo *relaycommon.RelayInfo, userId int, channelId int, tokenId int, quota int, settlementSucceeded bool, err error) error {
	if err == nil {
		return nil
	}
	rollbackErrs := []string{}
	if rollbackErr := RollbackTaskConsumptionUsage(userId, channelId, tokenId, quota); rollbackErr != nil {
		rollbackErrs = append(rollbackErrs, rollbackErr.Error())
	} else {
		setUsageCountersRecorded(ctx, false)
	}
	if settlementSucceeded {
		if rollbackErr := RollbackBillingSettlement(ctx, relayInfo, quota); rollbackErr != nil {
			rollbackErrs = append(rollbackErrs, rollbackErr.Error())
		}
	}
	if len(rollbackErrs) > 0 {
		return fmt.Errorf("record consume log failed: %w; rollback errors: %s", err, strings.Join(rollbackErrs, "; "))
	}
	return fmt.Errorf("record consume log failed: %w", err)
}
