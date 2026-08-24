package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

func refreshUserQuotaCacheBestEffort(id int) {
	if err := CacheUpdateUserQuota(id); err != nil {
		common.SysLog(fmt.Sprintf("failed to refresh user quota cache after quota update, userId=%d: %s", id, err.Error()))
	}
}

func updateUserUsedQuotaWithDB(db *gorm.DB, id int, quota int) error {
	if quota == 0 {
		return nil
	}
	if id <= 0 {
		return fmt.Errorf("user used quota update failed, user_id=%d, delta_quota=%d", id, quota)
	}
	updateExpr := gorm.Expr("used_quota + ?", quota)
	if quota < 0 {
		updateExpr = gorm.Expr("CASE WHEN used_quota + ? < 0 THEN 0 ELSE used_quota + ? END", quota, quota)
	}
	result := db.Model(&User{}).Where("id = ?", id).Update("used_quota", updateExpr)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var count int64
		if err := db.Model(&User{}).Where("id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("user used quota update failed, user_id=%d, delta_quota=%d", id, quota)
		}
	}
	return nil
}

func updateUserUsedQuotaAndRequestCountWithDB(db *gorm.DB, id int, quota int, count int) error {
	if quota == 0 && count == 0 {
		return nil
	}
	if id <= 0 {
		return fmt.Errorf("user used quota update failed, user_id=%d, delta_quota=%d", id, quota)
	}
	updates := map[string]interface{}{}
	if quota != 0 {
		updates["used_quota"] = gorm.Expr("used_quota + ?", quota)
	}
	if count != 0 {
		updates["request_count"] = gorm.Expr("request_count + ?", count)
	}
	result := db.Model(&User{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var count int64
		if err := db.Model(&User{}).Where("id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("user used quota update failed, user_id=%d, delta_quota=%d", id, quota)
		}
	}
	return nil
}

func UpdateUserUsedQuotaSync(id int, quota int) error {
	return updateUserUsedQuota(id, quota)
}

func UpdateUserUsedQuotaAndRequestCountSync(id int, quota int) error {
	err := updateUserUsedQuotaAndRequestCountWithDB(DB, id, quota, 1)
	if err == nil {
		refreshUserQuotaCacheBestEffort(id)
	}
	return err
}
