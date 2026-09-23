package controller

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/thanhpk/randstr"
)

type SubscriptionWaffoPancakePayRequest struct {
	PlanId int `json:"plan_id"`
}

func SubscriptionRequestWaffoPancakePay(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}

	var req SubscriptionWaffoPancakePayRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
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
	if strings.TrimSpace(plan.WaffoPancakeProductId) == "" {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionWaffoProductMissing)
		return
	}
	// Plan targets its own Pancake product, so the checkout gate only needs
	// gateway credentials here — not the gateway-level WaffoPancakeProductID.
	if !isWaffoPancakeSubscriptionEnabled() {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionWaffoNotConfigured)
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
	if err := validateWaffoPancakeSubscriptionProduct(c.Request.Context(), plan.WaffoPancakeProductId); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf(
			"Waffo Pancake 订阅产品与店铺校验失败 plan_id=%d product_id=%q store_id=%q error=%q",
			plan.Id, plan.WaffoPancakeProductId, setting.WaffoPancakeStoreID, err.Error(),
		))
		common.ApiErrorI18n(c, i18n.MsgSubscriptionWaffoShopMismatch)
		return
	}

	// WAFFO_PANCAKE_SUB- prefix (vs. wallet's WAFFO_PANCAKE-) drives webhook
	// dispatch in WaffoPancakeWebhook.
	tradeNo := fmt.Sprintf("WAFFO_PANCAKE_SUB-%d-%d-%s", userId, time.Now().UnixMilli(), randstr.String(6))
	paidCurrency := strings.ToUpper(strings.TrimSpace(plan.Currency))
	if paidCurrency == "" {
		paidCurrency = strings.ToUpper(strings.TrimSpace(setting.WaffoPancakeCurrency))
	}
	if paidCurrency == "" {
		paidCurrency = "USD"
	}
	paidAmount, err := normalizeSubscriptionPaymentAmount(plan, paidCurrency)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionAmountInvalid)
		return
	}
	priceSnapshot, err := model.FormatPaymentAmount(paidAmount, paidCurrency)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionAmountInvalid)
		return
	}
	snapshot, _ := referralService.BuildOrderSnapshot(userId, paidAmount, paidCurrency)

	order := &model.SubscriptionOrder{
		UserId:          userId,
		PlanId:          plan.Id,
		Money:           paidAmount,
		PaidAmount:      paidAmount,
		PaidCurrency:    paidCurrency,
		TradeNo:         tradeNo,
		PaymentMethod:   model.PaymentMethodWaffoPancake,
		PaymentProvider: model.PaymentProviderWaffoPancake,
		WaffoPancakeStoreID: strings.TrimSpace(
			setting.WaffoPancakeStoreID,
		),
		CreateTime: time.Now().Unix(),
		Status:     common.TopUpStatusPending,
	}
	applySubscriptionOrderSnapshot(order, plan, paidCurrency)
	if snapshot != nil {
		order.ReferralAffiliateId = snapshot.AffiliateId
		order.ReferralRate = snapshot.Rate
		order.ReferralBaseAmount = snapshot.BaseAmount
		order.ReferralBaseCurrency = snapshot.Currency
		order.ReferralCommissionStatus = snapshot.Status
		order.ReferralCommissionError = snapshot.Error
	}
	if err := order.Insert(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Waffo Pancake 订阅订单创建失败 user_id=%d plan_id=%d trade_no=%s error=%q", userId, plan.Id, tradeNo, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": i18n.T(c, i18n.MsgPaymentCreateFailed)})
		return
	}

	expiresInSeconds := 45 * 60
	session, err := createWaffoPancakeCheckoutSession(c.Request.Context(), &service.WaffoPancakeCreateSessionParams{
		ProductID:     strings.TrimSpace(plan.WaffoPancakeProductId),
		Currency:      paidCurrency,
		BuyerIdentity: service.WaffoPancakeBuyerIdentityFromUserID(user.Id),
		PriceSnapshot: &service.WaffoPancakePriceSnapshot{
			Amount:      priceSnapshot,
			TaxCategory: "saas",
		},
		BuyerEmail:              getWaffoPancakeBuyerEmail(user),
		ExpiresInSeconds:        &expiresInSeconds,
		OrderMerchantExternalID: tradeNo,
	})
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Waffo Pancake 订阅结账会话创建失败 user_id=%d plan_id=%d trade_no=%s error=%q", userId, plan.Id, tradeNo, err.Error()))
		order.Status = common.TopUpStatusFailed
		_ = order.Update()
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": i18n.T(c, i18n.MsgPaymentStartFailed)})
		return
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("Waffo Pancake 订阅订单创建成功 user_id=%d plan_id=%d trade_no=%s session_id=%s money=%.2f", userId, plan.Id, tradeNo, session.SessionID, paidAmount))

	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"checkout_url":     session.CheckoutURL,
			"session_id":       session.SessionID,
			"expires_at":       session.ExpiresAt,
			"order_id":         tradeNo,
			"trade_no":         tradeNo,
			"token":            session.Token,
			"token_expires_at": session.TokenExpiresAt,
		},
	})
}
