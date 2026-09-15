package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

func applyUserQuotaCacheDeltaBestEffort(id int, delta int64) {
	if err := ApplyUserQuotaCacheDelta(id, delta); err != nil {
		common.SysLog(fmt.Sprintf("failed to apply user quota cache delta, userId=%d delta=%d: %s", id, delta, err.Error()))
	}
}

func updateUserUsedQuotaWithDB(db *gorm.DB, id int, quota int64) error {
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

func updateUserUsedQuotaAndRequestCountWithDB(db *gorm.DB, id int, quota int64, count int) error {
	if quota == 0 && count == 0 {
		return nil
	}
	if id <= 0 {
		return fmt.Errorf("user used quota update failed, user_id=%d, delta_quota=%d", id, quota)
	}
	updates := map[string]interface{}{}
	if quota != 0 {
		if quota < 0 {
			updates["used_quota"] = gorm.Expr("CASE WHEN used_quota + ? < 0 THEN 0 ELSE used_quota + ? END", quota, quota)
		} else {
			updates["used_quota"] = gorm.Expr("used_quota + ?", quota)
		}
	}
	if count != 0 {
		if count < 0 {
			updates["request_count"] = gorm.Expr("CASE WHEN request_count + ? < 0 THEN 0 ELSE request_count + ? END", count, count)
		} else {
			updates["request_count"] = gorm.Expr("request_count + ?", count)
		}
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

func UpdateUserUsedQuotaSync(id int, quota int64) error {
	return updateUserUsedQuota(id, quota)
}

func UpdateUserUsedQuotaAndRequestCountSync(id int, quota int64) error {
	return updateUserUsedQuotaAndRequestCountWithDB(DB, id, quota, 1)
}
