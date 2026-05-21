package controller

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type GMPayPayRequest struct {
	Amount        int64  `json:"amount"`
	PaymentMethod string `json:"payment_method"`
}

func GetGMPayAssets(c *gin.Context) {
	if !service.IsGMPayConfigured() {
		common.ApiSuccess(c, []service.GMPayAsset{})
		return
	}
	assets, err := service.GetGMPayAssets()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, assets)
}

func RequestGMPayPay(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}
	if !service.IsGMPayConfigured() {
		common.ApiErrorMsg(c, "GMPay 未启用或配置不完整")
		return
	}

	var req GMPayPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	minTopup := int64(setting.GMPayMinTopUp)
	if minTopup <= 0 {
		minTopup = getMinTopup()
	}
	if req.Amount < minTopup {
		common.ApiErrorMsg(c, fmt.Sprintf("充值数量不能小于 %d", minTopup))
		return
	}
	token, network, ok := service.ParseGMPayPaymentMethod(req.PaymentMethod)
	if !ok || !service.IsValidGMPayPaymentMethod(req.PaymentMethod) {
		common.ApiErrorMsg(c, "支付链不存在或未启用")
		return
	}
	if network == "" {
		network = "tron"
	}

	id := c.GetInt("id")
	group, err := model.GetUserGroup(id, true)
	if err != nil {
		common.ApiErrorMsg(c, "获取用户分组失败")
		return
	}
	payMoney := getPayMoney(req.Amount, group)
	if payMoney < 0.01 {
		common.ApiErrorMsg(c, "充值金额过低")
		return
	}
	currency := strings.ToUpper(strings.TrimSpace(setting.GMPayCurrency))
	if currency == "" {
		currency = "CNY"
	}
	if currency != "CNY" {
		common.ApiErrorMsg(c, "GMPay 订单计价币种必须为 CNY")
		return
	}
	snapshot, _ := referralService.BuildOrderSnapshot(id, payMoney, currency)
	tradeNo := fmt.Sprintf("EPU%d%s%d", id, common.GetRandomString(6), time.Now().Unix())
	callbackAddress := service.GetCallbackAddress()
	notifyURL := callbackAddress + "/api/user/gmpay/notify"
	returnURL := paymentReturnPath("/console/topup?show_history=true")

	amount := req.Amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		dAmount := decimal.NewFromInt(amount)
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		amount = dAmount.Div(dQuotaPerUnit).IntPart()
	}
	method := service.BuildGMPayPaymentMethod(token, network)
	topUp := &model.TopUp{
		UserId:          id,
		Amount:          amount,
		Money:           payMoney,
		PaidAmount:      payMoney,
		PaidCurrency:    currency,
		TradeNo:         tradeNo,
		PaymentMethod:   method,
		PaymentProvider: model.PaymentProviderGMPay,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	applyTopUpOrderSnapshot(topUp, topUpOrderSnapshotInput{
		RequestAmount: req.Amount,
		CreditAmount:  amount,
		PaidAmount:    payMoney,
		PaidCurrency:  currency,
		UserGroup:     group,
	})
	if snapshot != nil {
		topUp.ReferralAffiliateId = snapshot.AffiliateId
		topUp.ReferralRate = snapshot.Rate
		topUp.ReferralBaseAmount = snapshot.BaseAmount
		topUp.ReferralBaseCurrency = snapshot.Currency
		topUp.ReferralCommissionStatus = snapshot.Status
		topUp.ReferralCommissionError = snapshot.Error
	}
	if err := topUp.Insert(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("GMPay 创建充值订单失败 user_id=%d trade_no=%s payment_method=%s amount=%d error=%q", id, tradeNo, method, req.Amount, err.Error()))
		common.ApiErrorMsg(c, "创建订单失败")
		return
	}

	paymentOrder, err := service.CreateGMPayOrder(service.GMPayCreateOrderRequest{
		OrderID:     tradeNo,
		Amount:      payMoney,
		Currency:    currency,
		Token:       token,
		Network:     network,
		NotifyURL:   notifyURL,
		RedirectURL: returnURL,
		Name:        fmt.Sprintf("Topup %d", req.Amount),
		PaymentType: method,
	})
	if err != nil {
		_ = model.UpdatePendingTopUpStatus(tradeNo, model.PaymentProviderGMPay, common.TopUpStatusExpired)
		var gatewayErr service.GMPayGatewayError
		if errors.As(err, &gatewayErr) {
			logger.LogError(c.Request.Context(), fmt.Sprintf("GMPay gateway rejected topup order user_id=%d trade_no=%s payment_method=%s amount=%d error=%q", id, tradeNo, method, req.Amount, err.Error()))
			if message := gatewayErr.PublicMessage(); message != "" {
				common.ApiErrorMsg(c, message)
				return
			}
		}
		logger.LogError(c.Request.Context(), fmt.Sprintf("GMPay topup payment create failed user_id=%d trade_no=%s payment_method=%s amount=%d error=%q", id, tradeNo, method, req.Amount, err.Error()))
		common.ApiErrorMsg(c, "GMPay 网关连接失败，请检查 GMPay 端点、商户号和密钥配置")
		return
	}
	topUp.ProviderPayload = common.GetJsonString(paymentOrder.Raw)
	_ = topUp.Update()
	common.ApiSuccess(c, gin.H{
		"payment_url":      paymentOrder.PaymentURL,
		"order_id":         paymentOrder.OrderID,
		"transaction_id":   paymentOrder.TransactionID,
		"payment_address":  paymentOrder.PaymentAddress,
		"payment_amount":   paymentOrder.PaymentAmount,
		"payment_currency": paymentOrder.PaymentCurrency,
	})
}

func GMPayTopUpNotify(c *gin.Context) {
	params, err := readGMPayCallback(c)
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("GMPay webhook 参数解析失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	if !service.VerifyGMPaySignature(params) {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("GMPay webhook 验签失败 path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	tradeNo := service.GMPayCallbackTradeNo(params)
	if tradeNo == "" {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	status := service.GMPayCallbackStatus(params)
	if !service.IsGMPayPaidStatus(status) {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("GMPay webhook 忽略非成功事件 trade_no=%s status=%s client_ip=%s", tradeNo, status, c.ClientIP()))
		_, _ = c.Writer.Write([]byte("ok"))
		return
	}
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)
	merchantID := service.GMPayCallbackMerchantID(params)
	if !gmpayCallbackMerchantMatches(merchantID) {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("GMPay webhook merchant mismatch trade_no=%s callback_pid=%s client_ip=%s", tradeNo, merchantID, c.ClientIP()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	if err := model.RechargeGMPayWithValidation(tradeNo, common.GetJsonString(params), model.PaymentCallbackValidation{
		ExpectedPaymentProvider: model.PaymentProviderGMPay,
		ActualPaymentMethod:     service.GMPayCallbackMethod(params),
		ActualPaymentToken:      service.GMPayCallbackToken(params),
		PaidAmount:              service.GMPayCallbackPaidAmount(params),
		PaidCurrency:            gmpayCallbackPaidCurrency(params),
		RequirePaymentFacts:     true,
	}, c.ClientIP()); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("GMPay 充值处理失败 trade_no=%s client_ip=%s error=%q", tradeNo, c.ClientIP(), err.Error()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	if err := processPaidTopUpCommission(c.Request.Context(), tradeNo); err != nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	_, _ = c.Writer.Write([]byte("ok"))
}

func gmpayCallbackMerchantMatches(merchantID string) bool {
	expected := strings.TrimSpace(setting.GMPayPID)
	actual := strings.TrimSpace(merchantID)
	return expected != "" && actual != "" && actual == expected
}

func gmpayCallbackPaidCurrency(params map[string]interface{}) string {
	for _, key := range []string{"settlement_currency", "fiat_currency", "order_currency"} {
		currency := strings.ToUpper(strings.TrimSpace(gmpayCallbackString(params, key)))
		if currency != "" {
			return currency
		}
	}
	return strings.ToUpper(strings.TrimSpace(setting.GMPayCurrency))
}

func gmpayCallbackString(params map[string]interface{}, key string) string {
	value, ok := params[key]
	if !ok || value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func readGMPayCallback(c *gin.Context) (map[string]interface{}, error) {
	params := map[string]interface{}{}
	contentType := strings.ToLower(c.GetHeader("Content-Type"))
	if strings.Contains(contentType, "application/json") {
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, 2<<20))
		if err != nil {
			return nil, err
		}
		if err := common.Unmarshal(body, &params); err != nil {
			return nil, err
		}
		return params, nil
	}
	if err := c.Request.ParseForm(); err != nil {
		return nil, err
	}
	for key, values := range c.Request.Form {
		if len(values) > 0 {
			params[key] = values[0]
		}
	}
	return params, nil
}
