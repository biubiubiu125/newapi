package model

import (
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

func UpdateUserAndChannelUsedQuotaSync(userId int, channelId int, quota int) error {
	if quota == 0 {
		return nil
	}
	if err := DB.Transaction(func(tx *gorm.DB) error {
		if err := updateUserUsedQuotaWithDB(tx, userId, quota); err != nil {
			return err
		}
		if err := updateChannelUsedQuotaWithDB(tx, channelId, quota); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	refreshUserQuotaCacheBestEffort(userId)
	return nil
}

func UpdateUserAndChannelUsedQuotaAllowMissingChannelRefundSync(userId int, channelId int, quota int) error {
	if quota == 0 {
		return nil
	}
	if err := DB.Transaction(func(tx *gorm.DB) error {
		if err := updateUserUsedQuotaWithDB(tx, userId, quota); err != nil {
			return err
		}
		if err := updateChannelUsedQuotaWithDB(tx, channelId, quota); err != nil {
			if quota < 0 && IsChannelUsedQuotaNoRowsError(err) {
				common.SysLog(err.Error())
				return nil
			}
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	refreshUserQuotaCacheBestEffort(userId)
	return nil
}

func UpdateTaskConsumptionUsageSync(userId int, channelId int, quota int) error {
	if quota == 0 {
		return nil
	}
	if err := DB.Transaction(func(tx *gorm.DB) error {
		if err := updateUserUsedQuotaAndRequestCountWithDB(tx, userId, quota, 1); err != nil {
			return err
		}
		if err := updateChannelUsedQuotaWithDB(tx, channelId, quota); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	refreshUserQuotaCacheBestEffort(userId)
	return nil
}

func UpdateTaskConsumptionUsageWithTokenSync(userId int, channelId int, tokenId int, quota int) error {
	return updateTaskUsageWithTokenSync(DB, userId, channelId, tokenId, quota, 1)
}

func UpdateTaskUsageAdjustmentWithTokenSync(userId int, channelId int, tokenId int, quota int) error {
	return updateTaskUsageWithTokenSync(DB, userId, channelId, tokenId, quota, 0)
}

func UpdateTaskUsageAdjustmentWithTokenAtSync(userId int, channelId int, tokenId int, quota int, usedAt int64) error {
	return updateTaskUsageWithTokenAtSyncOptions(DB, userId, channelId, tokenId, quota, 0, false, usedAt)
}

func UpdateTaskUsageAdjustmentWithTokenAtSyncAllowMissingChannelRefund(userId int, channelId int, tokenId int, quota int, usedAt int64) error {
	return updateTaskUsageWithTokenAtSyncOptions(DB, userId, channelId, tokenId, quota, 0, true, usedAt)
}

func UpdateTaskUsageAdjustmentWithTokenSyncAllowMissingChannelRefund(userId int, channelId int, tokenId int, quota int) error {
	return updateTaskUsageWithTokenSyncOptions(DB, userId, channelId, tokenId, quota, 0, true)
}

func UpdateTaskConsumptionUsageRollbackWithTokenSync(userId int, channelId int, tokenId int, quota int) error {
	if quota < 0 {
		quota = -quota
	}
	return updateTaskUsageWithTokenSync(DB, userId, channelId, tokenId, -quota, -1)
}

func updateTaskUsageWithTokenSync(db *gorm.DB, userId int, channelId int, tokenId int, quota int, requestCount int) error {
	return updateTaskUsageWithTokenSyncOptions(db, userId, channelId, tokenId, quota, requestCount, false)
}

func updateTaskUsageWithTokenSyncOptions(db *gorm.DB, userId int, channelId int, tokenId int, quota int, requestCount int, allowMissingChannelRefund bool) error {
	return updateTaskUsageWithTokenAtSyncOptions(db, userId, channelId, tokenId, quota, requestCount, allowMissingChannelRefund, common.GetTimestamp())
}

func updateTaskUsageWithTokenAtSyncOptions(db *gorm.DB, userId int, channelId int, tokenId int, quota int, requestCount int, allowMissingChannelRefund bool, usedAt int64) error {
	if quota == 0 && requestCount == 0 {
		return nil
	}
	if usedAt <= 0 {
		usedAt = common.GetTimestamp()
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		if requestCount != 0 {
			err = updateUserUsedQuotaAndRequestCountWithDB(tx, userId, quota, requestCount)
		} else {
			err = updateUserUsedQuotaWithDB(tx, userId, quota)
		}
		if err != nil {
			return err
		}
		if err := updateChannelUsedQuotaWithDB(tx, channelId, quota); err != nil {
			if allowMissingChannelRefund && quota < 0 && IsChannelUsedQuotaNoRowsError(err) {
				common.SysLog(err.Error())
			} else {
				return err
			}
		}
		if tokenId > 0 {
			updateLastUsedAt := requestCount > 0
			if err := recordTokenUsageWithDB(tx, tokenId, userId, quota, usedAt, requestCount, updateLastUsedAt); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	refreshUserQuotaCacheBestEffort(userId)
	return nil
}
