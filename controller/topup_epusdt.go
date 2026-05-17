package controller

import (
	"encoding/json"
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

type EpusdtPayRequest struct {
	Amount        int64  `json:"amount"`
	PaymentMethod string `json:"payment_method"`
}

func GetEpusdtAssets(c *gin.Context) {
	if !service.IsEpusdtConfigured() {
		common.ApiSuccess(c, []service.EpusdtAsset{})
		return
	}
	assets, err := service.GetEpusdtAssets()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, assets)
}

func RequestEpusdtPay(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}
	if !service.IsEpusdtConfigured() {
		common.ApiErrorMsg(c, "Epusdt 未启用或配置不完整")
		return
	}

	var req EpusdtPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	minTopup := int64(setting.EpusdtMinTopUp)
	if minTopup <= 0 {
		minTopup = getMinTopup()
	}
	if req.Amount < minTopup {
		common.ApiErrorMsg(c, fmt.Sprintf("充值数量不能小于 %d", minTopup))
		return
	}
	token, network, ok := service.ParseEpusdtPaymentMethod(req.PaymentMethod)
	if !ok || !service.IsValidEpusdtPaymentMethod(req.PaymentMethod) {
		common.ApiErrorMsg(c, "支付链不存在或未启用")
		return
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
	currency := strings.ToUpper(strings.TrimSpace(setting.EpusdtCurrency))
	if currency == "" {
		currency = "CNY"
	}
	if currency != "CNY" {
		common.ApiErrorMsg(c, "Epusdt 订单计价币种必须为 CNY")
		return
	}
	snapshot, _ := referralService.BuildOrderSnapshot(id, payMoney, currency)
	tradeNo := fmt.Sprintf("EPU%d%s%d", id, common.GetRandomString(6), time.Now().Unix())
	callbackAddress := service.GetCallbackAddress()
	notifyURL := callbackAddress + "/api/user/epusdt/notify"
	returnURL := paymentReturnPath("/console/topup?show_history=true")

	amount := req.Amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		dAmount := decimal.NewFromInt(amount)
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		amount = dAmount.Div(dQuotaPerUnit).IntPart()
	}
	method := service.BuildEpusdtPaymentMethod(token, network)
	topUp := &model.TopUp{
		UserId:          id,
		Amount:          amount,
		Money:           payMoney,
		PaidAmount:      payMoney,
		PaidCurrency:    currency,
		TradeNo:         tradeNo,
		PaymentMethod:   method,
		PaymentProvider: model.PaymentProviderEpusdt,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if snapshot != nil {
		topUp.ReferralAffiliateId = snapshot.AffiliateId
		topUp.ReferralRate = snapshot.Rate
		topUp.ReferralBaseAmount = snapshot.BaseAmount
		topUp.ReferralCommissionStatus = snapshot.Status
		topUp.ReferralCommissionError = snapshot.Error
	}
	if err := topUp.Insert(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Epusdt 创建充值订单失败 user_id=%d trade_no=%s payment_method=%s amount=%d error=%q", id, tradeNo, method, req.Amount, err.Error()))
		common.ApiErrorMsg(c, "创建订单失败")
		return
	}

	paymentOrder, err := service.CreateEpusdtOrder(service.EpusdtCreateOrderRequest{
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
		_ = model.UpdatePendingTopUpStatus(tradeNo, model.PaymentProviderEpusdt, common.TopUpStatusExpired)
		logger.LogError(c.Request.Context(), fmt.Sprintf("Epusdt 拉起支付失败 user_id=%d trade_no=%s payment_method=%s amount=%d error=%q", id, tradeNo, method, req.Amount, err.Error()))
		common.ApiErrorMsg(c, "拉起支付失败")
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

func EpusdtTopUpNotify(c *gin.Context) {
	params, err := readEpusdtCallback(c)
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("Epusdt webhook 参数解析失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	if !service.VerifyEpusdtSignature(params) {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("Epusdt webhook 验签失败 path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
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
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("Epusdt webhook 忽略非成功事件 trade_no=%s status=%s client_ip=%s", tradeNo, status, c.ClientIP()))
		_, _ = c.Writer.Write([]byte("ok"))
		return
	}
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)
	method := service.EpusdtCallbackMethod(params)
	merchantID := service.EpusdtCallbackMerchantID(params)
	if merchantID != "" && merchantID != setting.EpusdtPID {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("Epusdt webhook merchant mismatch trade_no=%s callback_pid=%s client_ip=%s", tradeNo, merchantID, c.ClientIP()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	if err := model.RechargeEpusdtWithValidation(tradeNo, common.GetJsonString(params), model.PaymentCallbackValidation{
		ExpectedPaymentProvider: model.PaymentProviderEpusdt,
		ActualPaymentMethod:     method,
		PaidAmount:              service.EpusdtCallbackPaidAmount(params),
		PaidCurrency:            service.EpusdtCallbackPaidCurrency(params),
		RequirePaymentFacts:     true,
	}, c.ClientIP()); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Epusdt 充值处理失败 trade_no=%s client_ip=%s error=%q", tradeNo, c.ClientIP(), err.Error()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	_ = referralService.ProcessTopUpCommission(tradeNo)
	_, _ = c.Writer.Write([]byte("ok"))
}

func readEpusdtCallback(c *gin.Context) (map[string]interface{}, error) {
	params := map[string]interface{}{}
	contentType := strings.ToLower(c.GetHeader("Content-Type"))
	if strings.Contains(contentType, "application/json") {
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, 2<<20))
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(body, &params); err != nil {
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
