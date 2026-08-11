package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

type SubscriptionBEpusdtPayRequest struct {
	PlanId        int    `json:"plan_id"`
	PaymentMethod string `json:"payment_method"`
}

func SubscriptionRequestBEpusdt(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}
	if !service.IsUSDTGatewayConfigured() {
		common.ApiErrorMsg(c, "USDT 网关未启用或配置不完整")
		return
	}

	var req SubscriptionBEpusdtPayRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	token, network, ok := service.ParseBEpusdtPaymentMethod(req.PaymentMethod)
	if !ok || token != "usdt" || network != "" {
		common.ApiErrorMsg(c, "支付链不存在或未启用")
		return
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

	currency := strings.ToUpper(strings.TrimSpace(setting.BEpusdtCurrency))
	if currency == "" {
		currency = "CNY"
	}
	if currency != "CNY" {
		common.ApiErrorMsg(c, "USDT 网关订单计价币种必须为 CNY")
		return
	}
	callbackAddress := paymentPublicBaseURLForRequest(c)
	if callbackAddress == "" {
		common.ApiErrorMsg(c, "USDT 网关回调地址必须配置为公网地址，不能使用 localhost")
		return
	}
	notifyURL := callbackAddress + "/api/subscription/bepusdt/notify"
	paidAmount, err := normalizeSubscriptionPaymentAmount(plan, currency)
	if err != nil {
		common.ApiErrorMsg(c, "套餐金额无效")
		return
	}
	snapshot, _ := referralService.BuildOrderSnapshot(userId, paidAmount, currency)
	tradeNo := fmt.Sprintf("SEPU%d%s%d", userId, common.GetRandomString(6), time.Now().Unix())
	method := service.USDTPaymentMethod
	provider := service.ActiveUSDTGatewayProvider()
	returnURL := paymentWalletReturnPathForRequest(c, "pending", provider, "subscription", tradeNo)
	if returnURL == "" {
		common.ApiErrorMsg(c, "USDT 网关返回地址必须配置为公网地址，不能使用 localhost")
		return
	}
	order := &model.SubscriptionOrder{
		UserId:          userId,
		PlanId:          plan.Id,
		Money:           paidAmount,
		PaidAmount:      paidAmount,
		PaidCurrency:    currency,
		TradeNo:         tradeNo,
		PaymentMethod:   method,
		PaymentProvider: provider,
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

	paymentOrder, err := service.CreateUSDTGatewayOrder(service.USDTGatewayOrderRequest{
		OrderID:     tradeNo,
		Amount:      paidAmount,
		Currency:    currency,
		NotifyURL:   notifyURL,
		RedirectURL: returnURL,
		Name:        fmt.Sprintf("Subscription %s", plan.Title),
		PaymentType: req.PaymentMethod,
	})
	if err != nil {
		_ = model.ExpireSubscriptionOrder(tradeNo, provider)
		var gatewayErr service.BEpusdtGatewayError
		if errors.As(err, &gatewayErr) {
			logger.LogError(c.Request.Context(), fmt.Sprintf("BEpusdt gateway rejected subscription order user_id=%d trade_no=%s payment_method=%s plan_id=%d error=%q", userId, tradeNo, method, plan.Id, err.Error()))
			if message := gatewayErr.PublicMessage(); message != "" {
				common.ApiErrorMsg(c, message)
				return
			}
		}
		logger.LogError(c.Request.Context(), fmt.Sprintf("BEpusdt subscription payment create failed user_id=%d trade_no=%s payment_method=%s plan_id=%d error=%q", userId, tradeNo, method, plan.Id, err.Error()))
		common.ApiErrorMsg(c, "BEpusdt 网关连接失败，请检查 BEpusdt 端点和密钥配置")
		return
	}
	order.ProviderPayload = common.GetJsonString(paymentOrder.Raw)
	_ = order.Update()
	common.ApiSuccess(c, gin.H{
		"payment_url":      paymentOrder.PaymentURL,
		"order_id":         paymentOrder.OrderID,
		"trade_no":         tradeNo,
		"transaction_id":   paymentOrder.TransactionID,
		"payment_address":  paymentOrder.PaymentAddress,
		"payment_amount":   paymentOrder.PaymentAmount,
		"payment_currency": paymentOrder.PaymentCurrency,
	})
}

func SubscriptionBEpusdtNotify(c *gin.Context) {
	params, err := readBEpusdtCallback(c)
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("BEpusdt subscription webhook 参数解析失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, common.GetClientIP(c), err.Error()))
		c.String(http.StatusBadRequest, "fail")
		return
	}
	tradeNo := service.BEpusdtCallbackTradeNo(params)
	if tradeNo == "" {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("BEpusdt subscription webhook rejected reason=missing_order_id path=%q client_ip=%s", c.Request.RequestURI, common.GetClientIP(c)))
		c.String(http.StatusBadRequest, "fail")
		return
	}
	status := service.BEpusdtCallbackStatus(params)
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("BEpusdt subscription webhook received trade_no=%s status=%s amount=%s fiat=%s client_ip=%s", tradeNo, status, bepusdtCallbackString(params, "amount"), bepusdtCallbackString(params, "fiat"), common.GetClientIP(c)))
	facts, err := validateUSDTGatewayCallback(params)
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("BEpusdt subscription webhook rejected trade_no=%s path=%q client_ip=%s error=%q", tradeNo, c.Request.RequestURI, common.GetClientIP(c), err.Error()))
		c.String(http.StatusBadRequest, "fail")
		return
	}
	if !service.IsBEpusdtPaidStatus(status) {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("BEpusdt subscription webhook 忽略非成功事件 trade_no=%s status=%s client_ip=%s", tradeNo, status, common.GetClientIP(c)))
		_, _ = c.Writer.Write([]byte("ok"))
		return
	}
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)
	if err := model.CompleteSubscriptionOrderWithValidation(tradeNo, common.GetJsonString(params), model.PaymentCallbackValidation{
		ExpectedPaymentProvider: facts.Provider,
		ActualPaymentMethod:     facts.PaymentMethod,
		ActualPaymentToken:      facts.Token,
		PaidAmount:              facts.PaidAmount,
		PaidCurrency:            facts.PaidCurrency,
		RequirePaymentFacts:     true,
		CallerIP:                common.GetClientIP(c),
	}); err != nil {
		if isPermanentPaymentReviewError(err) {
			if recordErr := recordPaymentReview(c.Request.Context(), model.PaymentProviderBEpusdt, "", "subscription.notify", tradeNo, "", "BEpusdt subscription payment requires manual review after payment succeeded", err, common.GetJsonString(params)); recordErr != nil {
				logger.LogError(c.Request.Context(), fmt.Sprintf("BEpusdt subscription review record failed trade_no=%s client_ip=%s error=%q", tradeNo, common.GetClientIP(c), recordErr.Error()))
				c.String(http.StatusInternalServerError, "fail")
				return
			}
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("BEpusdt subscription payment queued for manual review trade_no=%s client_ip=%s error=%q", tradeNo, common.GetClientIP(c), err.Error()))
			c.String(http.StatusOK, "ok")
			return
		}
		if isPaymentCallbackRejection(err) {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("BEpusdt subscription webhook rejected trade_no=%s client_ip=%s error=%q", tradeNo, common.GetClientIP(c), err.Error()))
			c.String(http.StatusBadRequest, "fail")
			return
		}
		logger.LogError(c.Request.Context(), fmt.Sprintf("BEpusdt subscription processing failed trade_no=%s client_ip=%s error=%q", tradeNo, common.GetClientIP(c), err.Error()))
		c.String(http.StatusInternalServerError, "fail")
		return
	}
	if err := processPaidSubscriptionCommission(c.Request.Context(), tradeNo); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("BEpusdt subscription commission failed trade_no=%s client_ip=%s error=%q", tradeNo, common.GetClientIP(c), err.Error()))
		c.String(http.StatusInternalServerError, "fail")
		return
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("BEpusdt subscription webhook processed trade_no=%s client_ip=%s", tradeNo, common.GetClientIP(c)))
	_, _ = c.Writer.Write([]byte("ok"))
}
