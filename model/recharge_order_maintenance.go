package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	DefaultPendingOrderExpireSeconds    int64 = 24 * 60 * 60
	DefaultExpiredOrderRetentionSeconds int64 = 3 * 24 * 60 * 60

	defaultRechargeOrderMaintenanceBatchLimit = 300
)

// ExpireStalePendingRechargeOrders marks pending recharge-related orders as expired
// after the configured age. It returns affected top-up and subscription order counts.
func ExpireStalePendingRechargeOrders(maxAgeSeconds int64, limit int) (int, int, error) {
	if DB == nil {
		return 0, 0, errors.New("database is not initialized")
	}
	if maxAgeSeconds <= 0 {
		maxAgeSeconds = DefaultPendingOrderExpireSeconds
	}
	limit = normalizeRechargeOrderMaintenanceLimit(limit)

	now := common.GetTimestamp()
	cutoff := now - maxAgeSeconds
	topUps, err := expireStalePendingTopUps(cutoff, now, limit)
	if err != nil {
		return 0, 0, err
	}
	subscriptionOrders, err := expireStalePendingSubscriptionOrders(cutoff, now, limit)
	if err != nil {
		return topUps, 0, err
	}
	return topUps, subscriptionOrders, nil
}

// CleanupExpiredRechargeOrders deletes expired recharge-related orders after the
// retention window. It returns deleted top-up and subscription order counts.
func CleanupExpiredRechargeOrders(retentionSeconds int64, limit int) (int, int, error) {
	if DB == nil {
		return 0, 0, errors.New("database is not initialized")
	}
	if retentionSeconds <= 0 {
		retentionSeconds = DefaultExpiredOrderRetentionSeconds
	}
	limit = normalizeRechargeOrderMaintenanceLimit(limit)

	cutoff := common.GetTimestamp() - retentionSeconds
	topUps, err := cleanupExpiredTopUps(cutoff, limit)
	if err != nil {
		return 0, 0, err
	}
	subscriptionOrders, err := cleanupExpiredSubscriptionOrders(cutoff, limit)
	if err != nil {
		return topUps, 0, err
	}
	return topUps, subscriptionOrders, nil
}

func normalizeRechargeOrderMaintenanceLimit(limit int) int {
	if limit <= 0 {
		return defaultRechargeOrderMaintenanceBatchLimit
	}
	return limit
}

func expireStalePendingTopUps(cutoff int64, now int64, limit int) (int, error) {
	var affected int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		var ids []int
		if err := lockRechargeOrderRows(tx.Model(&TopUp{})).
			Where("status = ? AND create_time > 0 AND create_time <= ?", common.TopUpStatusPending, cutoff).
			Order("create_time ASC, id ASC").
			Limit(limit).
			Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		result := tx.Model(&TopUp{}).
			Where("id IN ? AND status = ?", ids, common.TopUpStatusPending).
			Updates(map[string]interface{}{
				"status":        common.TopUpStatusExpired,
				"complete_time": now,
			})
		affected = result.RowsAffected
		return result.Error
	})
	return int(affected), err
}

func expireStalePendingSubscriptionOrders(cutoff int64, now int64, limit int) (int, error) {
	var affected int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		var ids []int
		if err := lockRechargeOrderRows(tx.Model(&SubscriptionOrder{})).
			Where("status = ? AND create_time > 0 AND create_time <= ?", common.TopUpStatusPending, cutoff).
			Order("create_time ASC, id ASC").
			Limit(limit).
			Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		result := tx.Model(&SubscriptionOrder{}).
			Where("id IN ? AND status = ?", ids, common.TopUpStatusPending).
			Updates(map[string]interface{}{
				"status":        common.TopUpStatusExpired,
				"complete_time": now,
			})
		affected = result.RowsAffected
		return result.Error
	})
	return int(affected), err
}

func cleanupExpiredTopUps(cutoff int64, limit int) (int, error) {
	deleted, err := cleanupExpiredTopUpsByCompleteTime(cutoff, limit)
	if err != nil || deleted >= limit {
		return deleted, err
	}
	fallbackDeleted, err := cleanupExpiredTopUpsByCreateTime(cutoff, limit-deleted)
	return deleted + fallbackDeleted, err
}

func cleanupExpiredSubscriptionOrders(cutoff int64, limit int) (int, error) {
	deleted, err := cleanupExpiredSubscriptionOrdersByCompleteTime(cutoff, limit)
	if err != nil || deleted >= limit {
		return deleted, err
	}
	fallbackDeleted, err := cleanupExpiredSubscriptionOrdersByCreateTime(cutoff, limit-deleted)
	return deleted + fallbackDeleted, err
}

func cleanupExpiredTopUpsByCompleteTime(cutoff int64, limit int) (int, error) {
	return deleteExpiredRechargeOrders[TopUp](cutoff, limit,
		"complete_time > 0 AND complete_time <= ?",
		"complete_time ASC, id ASC",
	)
}

func cleanupExpiredTopUpsByCreateTime(cutoff int64, limit int) (int, error) {
	return deleteExpiredRechargeOrders[TopUp](cutoff, limit,
		"(complete_time IS NULL OR complete_time <= 0) AND create_time > 0 AND create_time <= ?",
		"create_time ASC, id ASC",
	)
}

func cleanupExpiredSubscriptionOrdersByCompleteTime(cutoff int64, limit int) (int, error) {
	return deleteExpiredRechargeOrders[SubscriptionOrder](cutoff, limit,
		"complete_time > 0 AND complete_time <= ?",
		"complete_time ASC, id ASC",
	)
}

func cleanupExpiredSubscriptionOrdersByCreateTime(cutoff int64, limit int) (int, error) {
	return deleteExpiredRechargeOrders[SubscriptionOrder](cutoff, limit,
		"(complete_time IS NULL OR complete_time <= 0) AND create_time > 0 AND create_time <= ?",
		"create_time ASC, id ASC",
	)
}

func deleteExpiredRechargeOrders[T any](cutoff int64, limit int, timeCondition string, order string) (int, error) {
	var affected int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		var ids []int
		if err := lockRechargeOrderRows(tx.Model(new(T))).
			Where("status = ?", common.TopUpStatusExpired).
			Where(timeCondition, cutoff).
			Order(order).
			Limit(limit).
			Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		result := tx.Where("id IN ? AND status = ?", ids, common.TopUpStatusExpired).
			Where(timeCondition, cutoff).
			Delete(new(T))
		affected = result.RowsAffected
		return result.Error
	})
	return int(affected), err
}

func lockRechargeOrderRows(tx *gorm.DB) *gorm.DB {
	if common.UsingSQLite {
		return tx
	}
	return tx.Clauses(clause.Locking{Strength: "UPDATE"})
}
