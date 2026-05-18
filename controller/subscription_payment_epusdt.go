package controller

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

type SubscriptionEpusdtPayRequest struct {
	PlanId        int    `json:"plan_id"`
	PaymentMethod string `json:"payment_method"`
}

func SubscriptionRequestEpusdt(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}
	if !service.IsEpusdtConfigured() {
		common.ApiErrorMsg(c, "Epusdt 未启用或配置不完整")
		return
	}

	var req SubscriptionEpusdtPayRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	token, network, ok := service.ParseEpusdtPaymentMethod(req.PaymentMethod)
	if !ok || !service.IsValidEpusdtPaymentMethod(req.PaymentMethod) {
		common.ApiErrorMsg(c, "支付链不存在或未启用")
		return
	}
	if network == "" {
		network = "tron"
	}

	plan, err := model.GetSubscriptionPlanById(req.PlanId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !plan.Enabled {
		common.ApiErrorMsg(c, "套餐未启用")
		return
	}
	if plan.PriceAmount < 0.01 {
		common.ApiErrorMsg(c, "套餐金额过低")
		return
	}

	userId := c.GetInt("id")
	if plan.MaxPurchasePerUser > 0 {
		count, err := model.CountUserSubscriptionsByPlan(userId, plan.Id)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if count >= int64(plan.MaxPurchasePerUser) {
			common.ApiErrorMsg(c, "已达到该套餐购买上限")
			return
		}
	}

	currency := strings.ToUpper(strings.TrimSpace(setting.EpusdtCurrency))
	if currency == "" {
		currency = "CNY"
	}
	if currency != "CNY" {
		common.ApiErrorMsg(c, "Epusdt 订单计价币种必须为 CNY")
		return
	}
	snapshot, _ := referralService.BuildOrderSnapshot(userId, plan.PriceAmount, currency)
	tradeNo := fmt.Sprintf("SEPU%d%s%d", userId, common.GetRandomString(6), time.Now().Unix())
	method := service.BuildEpusdtPaymentMethod(token, network)
	order := &model.SubscriptionOrder{
		UserId:          userId,
		PlanId:          plan.Id,
		Money:           plan.PriceAmount,
		PaidAmount:      plan.PriceAmount,
		PaidCurrency:    currency,
		TradeNo:         tradeNo,
		PaymentMethod:   method,
		PaymentProvider: model.PaymentProviderEpusdt,
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
		common.ApiErrorMsg(c, "创建订单失败")
		return
	}

	callbackAddress := service.GetCallbackAddress()
	paymentOrder, err := service.CreateEpusdtOrder(service.EpusdtCreateOrderRequest{
		OrderID:     tradeNo,
		Amount:      plan.PriceAmount,
		Currency:    currency,
		Token:       token,
		Network:     network,
		NotifyURL:   callbackAddress + "/api/subscription/epusdt/notify",
		RedirectURL: paymentReturnPath("/console/topup?show_history=true"),
		Name:        fmt.Sprintf("Subscription %s", plan.Title),
		PaymentType: method,
	})
	if err != nil {
		_ = model.ExpireSubscriptionOrder(tradeNo, model.PaymentProviderEpusdt)
		var gatewayErr service.EpusdtGatewayError
		if errors.As(err, &gatewayErr) {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Epusdt gateway rejected subscription order user_id=%d trade_no=%s payment_method=%s plan_id=%d error=%q", userId, tradeNo, method, plan.Id, err.Error()))
			if message := gatewayErr.PublicMessage(); message != "" {
				common.ApiErrorMsg(c, message)
				return
			}
		}
		logger.LogError(c.Request.Context(), fmt.Sprintf("Epusdt subscription payment create failed user_id=%d trade_no=%s payment_method=%s plan_id=%d error=%q", userId, tradeNo, method, plan.Id, err.Error()))
		common.ApiErrorMsg(c, "拉起支付失败")
		return
	}
	order.ProviderPayload = common.GetJsonString(paymentOrder.Raw)
	_ = order.Update()
	common.ApiSuccess(c, gin.H{
		"payment_url":      paymentOrder.PaymentURL,
		"order_id":         paymentOrder.OrderID,
		"transaction_id":   paymentOrder.TransactionID,
		"payment_address":  paymentOrder.PaymentAddress,
		"payment_amount":   paymentOrder.PaymentAmount,
		"payment_currency": paymentOrder.PaymentCurrency,
	})
}

func SubscriptionEpusdtNotify(c *gin.Context) {
	params, err := readEpusdtCallback(c)
	if err != nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	if !service.VerifyEpusdtSignature(params) {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	tradeNo := service.EpusdtCallbackTradeNo(params)
	if tradeNo == "" {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	status := service.EpusdtCallbackStatus(params)
	if !service.IsEpusdtPaidStatus(status) {
		_, _ = c.Writer.Write([]byte("ok"))
		return
	}
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)
	merchantID := service.EpusdtCallbackMerchantID(params)
	if !epusdtCallbackMerchantMatches(merchantID) {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	if err := model.CompleteSubscriptionOrderWithValidation(tradeNo, common.GetJsonString(params), model.PaymentCallbackValidation{
		ExpectedPaymentProvider: model.PaymentProviderEpusdt,
		ActualPaymentMethod:     service.EpusdtCallbackMethod(params),
		ActualPaymentToken:      service.EpusdtCallbackToken(params),
		PaidAmount:              service.EpusdtCallbackPaidAmount(params),
		PaidCurrency:            epusdtCallbackPaidCurrency(params),
		RequirePaymentFacts:     true,
	}); err != nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	_ = referralService.ProcessSubscriptionCommission(tradeNo)
	_, _ = c.Writer.Write([]byte("ok"))
}
