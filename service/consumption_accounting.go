package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
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
