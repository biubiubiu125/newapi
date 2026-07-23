package service

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

func RollbackTaskConsumptionUsage(userId int, channelId int, tokenId int, quota int) error {
	if quota == 0 {
		return nil
	}
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
