package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func ListPaymentOrphans(c *gin.Context) {
	status := strings.TrimSpace(c.Query("status"))
	switch status {
	case "", "all",
		model.PaymentOrphanStatusPendingReview,
		model.PaymentOrphanStatusCredited,
		model.PaymentOrphanStatusRefunded,
		model.PaymentOrphanStatusDismissed:
	default:
		common.ApiErrorMsg(c, "支付悬单状态无效")
		return
	}

	pageInfo := common.GetPageQuery(c)
	events, total, err := model.ListPaymentOrphanEvents(status, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(events)
	common.ApiSuccess(c, pageInfo)
}

func CreditPaymentOrphan(c *gin.Context) {
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "支付悬单 ID 无效")
		return
	}
	if err := model.CreditPaymentOrphan(id, c.GetInt("id"), common.GetClientIP(c)); err != nil {
		common.ApiError(c, err)
		return
	}
	processCreditPaymentOrphanCommission(c, id)
	common.ApiSuccess(c, nil)
}

type resolvePaymentOrphanRequest struct {
	Status string `json:"status"`
	Note   string `json:"note"`
}

func ResolvePaymentOrphan(c *gin.Context) {
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "支付悬单 ID 无效")
		return
	}
	var req resolvePaymentOrphanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "请求无效",
		})
		return
	}
	if err := model.MarkPaymentOrphanResolved(id, strings.TrimSpace(req.Status), c.GetInt("id"), req.Note); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func processCreditPaymentOrphanCommission(c *gin.Context, orphanID int64) {
	orphan := &model.PaymentOrphanEvent{}
	if err := model.DB.Select("provider", "reference_id", "status").First(orphan, orphanID).Error; err != nil {
		return
	}
	if orphan.Status != model.PaymentOrphanStatusCredited {
		return
	}
	referenceID := strings.TrimSpace(orphan.ReferenceID)
	if model.GetSubscriptionOrderByTradeNo(referenceID) != nil {
		_ = processPaidSubscriptionCommission(c.Request.Context(), referenceID)
		return
	}
	if model.GetTopUpByTradeNo(referenceID) != nil {
		_ = processPaidTopUpCommission(c.Request.Context(), referenceID)
	}
}
