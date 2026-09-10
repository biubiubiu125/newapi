package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func manageUserQuota(c *gin.Context, req ManageRequest) {
	action := "generic"
	params := map[string]interface{}{
		"target_user_id":  req.Id,
		"mode":            req.Mode,
		"requested_quota": req.Value,
	}
	switch req.Mode {
	case "add":
		action = "user.quota_add"
	case "subtract":
		action = "user.quota_subtract"
	case "override":
		action = "user.quota_override"
	default:
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	adjustment, err := model.AdjustUserQuota(req.Id, c.GetInt("role"), req.Mode, req.Value)
	if err != nil {
		switch {
		case errors.Is(err, model.ErrInvalidUserQuotaAdjustment):
			if (req.Mode == "add" || req.Mode == "subtract") && req.Value <= 0 {
				common.ApiErrorI18n(c, i18n.MsgUserQuotaChangeZero)
			} else {
				common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			}
		case errors.Is(err, model.ErrUserQuotaPermission):
			common.ApiErrorI18n(c, i18n.MsgUserNoPermissionHigherLevel)
		case errors.Is(err, gorm.ErrRecordNotFound):
			common.ApiErrorI18n(c, i18n.MsgUserNotExists)
		case errors.Is(err, model.ErrWalletQuotaLimitExceeded):
			common.ApiError(c, err)
		default:
			common.ApiError(c, err)
		}
		return
	}

	params["target_username"] = adjustment.Username
	params["from"] = logger.LogQuota(adjustment.Before)
	params["to"] = logger.LogQuota(adjustment.After)
	if req.Mode != "override" {
		params["quota"] = logger.LogQuota(req.Value)
	}
	recordManageAuditFor(c, adjustment.UserID, action, params)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
