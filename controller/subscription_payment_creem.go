package controller

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/thanhpk/randstr"
)

type SubscriptionCreemPayRequest struct {
	PlanId int `json:"plan_id"`
}

func SubscriptionRequestCreemPay(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}

	var req SubscriptionCreemPayRequest

	// Keep body for debugging consistency (like RequestCreemPay)
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem 订阅支付请求读取失败 error=%q", err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": i18n.T(c, i18n.MsgCommonReadFailed)})
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": i18n.T(c, i18n.MsgInvalidParams)})
		return
	}

	plan, err := model.GetSubscriptionPlanById(req.PlanId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !plan.Enabled {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionNotEnabled)
		return
	}
	if plan.PriceAmount < 0.01 {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionAmountTooLow)
		return
	}
	productID := strings.TrimSpace(plan.CreemProductId)
	if productID == "" {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionCreemProductMissing)
		return
	}
	if !isCreemSubscriptionEnabled() {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionCreemNotConfigured)
		return
	}

	userId := c.GetInt("id")
	user, err := model.GetUserById(userId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if user == nil {
		common.ApiErrorI18n(c, i18n.MsgUserNotExists)
		return
	}

	if plan.MaxPurchasePerUser > 0 {
		count, err := model.CountUserSubscriptionsByPlan(userId, plan.Id)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if count >= int64(plan.MaxPurchasePerUser) {
			common.ApiErrorI18n(c, i18n.MsgSubscriptionPurchaseMax)
			return
		}
	}

	reference := "sub-creem-ref-" + randstr.String(6)
	referenceId := "sub_ref_" + common.Sha1([]byte(reference+time.Now().String()+user.Username))
	currency := strings.ToUpper(strings.TrimSpace(plan.Currency))
	if currency == "" {
		currency = "USD"
		switch operation_setting.GetGeneralSetting().QuotaDisplayType {
		case operation_setting.QuotaDisplayTypeCNY:
			currency = "CNY"
		case operation_setting.QuotaDisplayTypeUSD:
			currency = "USD"
		}
	}
	paidAmount, err := normalizeSubscriptionPaymentAmount(plan, currency)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionAmountInvalid)
		return
	}
	if err := validateCreemSubscriptionProduct(c.Request.Context(), productID, paidAmount, currency); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf(
			"Creem 订阅产品校验失败 plan_id=%d product_id=%q amount=%.8f currency=%s error=%q",
			plan.Id, productID, paidAmount, currency, err.Error(),
		))
		common.ApiErrorI18n(c, i18n.MsgSubscriptionCreemPriceInvalid)
		return
	}
	snapshot, _ := referralService.BuildOrderSnapshot(userId, paidAmount, currency)

	// create pending order first
	order := &model.SubscriptionOrder{
		UserId:          userId,
		PlanId:          plan.Id,
		Money:           paidAmount,
		PaidAmount:      paidAmount,
		PaidCurrency:    currency,
		TradeNo:         referenceId,
		PaymentMethod:   model.PaymentMethodCreem,
		PaymentProvider: model.PaymentProviderCreem,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	applySubscriptionOrderSnapshot(order, plan, currency)
	if snapshot != nil {
		order.ReferralAffiliateId = snapshot.AffiliateId
		order.ReferralRate = snapshot.Rate
		order.ReferralBaseAmount = snapshot.BaseAmount
		order.ReferralBaseCurrency = snapshot.Currency
		order.ReferralCommissionStatus = snapshot.Status
		order.ReferralCommissionError = snapshot.Error
	}
	if err := order.Insert(); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": i18n.T(c, i18n.MsgPaymentCreateFailed)})
		return
	}

	// Reuse Creem checkout generator by building a lightweight product reference.
	product := &CreemProduct{
		ProductId: productID,
		Name:      plan.Title,
		Price:     paidAmount,
		Currency:  currency,
		Quota:     0,
	}

	checkoutUrl, err := genCreemLink(c.Request.Context(), referenceId, product, user.Email, user.Username)
	if err != nil {
		if expireErr := model.ExpireSubscriptionOrder(referenceId, model.PaymentProviderCreem); expireErr != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Creem 订阅支付链接创建失败后关闭订单失败 trade_no=%s error=%q", referenceId, expireErr.Error()))
		}
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem 订阅支付链接创建失败 trade_no=%s product_id=%s error=%q", referenceId, product.ProductId, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": i18n.T(c, i18n.MsgPaymentStartFailed)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"checkout_url": checkoutUrl,
			"order_id":     referenceId,
			"trade_no":     referenceId,
		},
	})
}
