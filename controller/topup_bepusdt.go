package controller

import (
	"errors"
	"fmt"
	"io"
	"net/http"
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

type BEpusdtPayRequest struct {
	Amount        int64  `json:"amount"`
	PaymentMethod string `json:"payment_method"`
}

func GetBEpusdtAssets(c *gin.Context) {
	if !service.IsUSDTGatewayConfigured() {
		common.ApiSuccess(c, []service.BEpusdtAsset{})
		return
	}
	assets, err := service.GetBEpusdtAssets()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, assets)
}

func RequestBEpusdtPay(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}
	if !service.IsUSDTGatewayConfigured() {
		common.ApiErrorMsg(c, "USDT 网关未启用或配置不完整")
		return
	}

	var req BEpusdtPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	minTopup := int64(setting.BEpusdtMinTopUp)
	if minTopup <= 0 {
		minTopup = getMinTopup()
	}
	if req.Amount < minTopup {
		common.ApiErrorMsg(c, fmt.Sprintf("充值数量不能小于 %d", minTopup))
		return
	}
	token, network, ok := service.ParseBEpusdtPaymentMethod(req.PaymentMethod)
	if !ok || token != "usdt" || network != "" {
		common.ApiErrorMsg(c, "支付链不存在或未启用")
		return
	}

	id := c.GetInt("id")
	if rejectInvalidTopUpQuota(c, id, req.Amount) {
		return
	}
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
	currency := strings.ToUpper(strings.TrimSpace(setting.BEpusdtCurrency))
	if currency == "" {
		currency = "CNY"
	}
	if currency != "CNY" {
		common.ApiErrorMsg(c, "USDT 网关订单计价币种必须为 CNY")
		return
	}
	snapshot, _ := referralService.BuildOrderSnapshot(id, payMoney, currency)
	tradeNo := fmt.Sprintf("EPU%d%s%d", id, common.GetRandomString(6), time.Now().Unix())
	method := service.USDTPaymentMethod
	provider := service.ActiveUSDTGatewayProvider()
	callbackAddress := paymentPublicBaseURLForRequest(c)
	if callbackAddress == "" {
		common.ApiErrorMsg(c, "USDT 网关回调地址必须配置为公网地址，不能使用 localhost")
		return
	}
	notifyURL := callbackAddress + "/api/user/bepusdt/notify"
	returnURL := paymentWalletReturnPathForRequest(c, "pending", provider, "topup", tradeNo)
	if returnURL == "" {
		common.ApiErrorMsg(c, "USDT 网关返回地址必须配置为公网地址，不能使用 localhost")
		return
	}

	amount := req.Amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		dAmount := decimal.NewFromInt(amount)
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		amount = dAmount.Div(dQuotaPerUnit).IntPart()
	}
	topUp := &model.TopUp{
		UserId:          id,
		Amount:          amount,
		Money:           payMoney,
		PaidAmount:      payMoney,
		PaidCurrency:    currency,
		TradeNo:         tradeNo,
		PaymentMethod:   method,
		PaymentProvider: provider,
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
		logger.LogError(c.Request.Context(), fmt.Sprintf("BEpusdt 创建充值订单失败 user_id=%d trade_no=%s payment_method=%s amount=%d error=%q", id, tradeNo, method, req.Amount, err.Error()))
		common.ApiErrorMsg(c, "创建订单失败")
		return
	}

	paymentOrder, err := service.CreateUSDTGatewayOrder(service.USDTGatewayOrderRequest{
		OrderID:     tradeNo,
		Amount:      payMoney,
		Currency:    currency,
		NotifyURL:   notifyURL,
		RedirectURL: returnURL,
		Name:        fmt.Sprintf("Topup %d", req.Amount),
		PaymentType: req.PaymentMethod,
	})
	if err != nil {
		_ = model.UpdatePendingTopUpStatus(tradeNo, provider, common.TopUpStatusExpired)
		var gatewayErr service.BEpusdtGatewayError
		if errors.As(err, &gatewayErr) {
			logger.LogError(c.Request.Context(), fmt.Sprintf("BEpusdt gateway rejected topup order user_id=%d trade_no=%s payment_method=%s amount=%d error=%q", id, tradeNo, method, req.Amount, err.Error()))
			if message := gatewayErr.PublicMessage(); message != "" {
				common.ApiErrorMsg(c, message)
				return
			}
		}
		logger.LogError(c.Request.Context(), fmt.Sprintf("BEpusdt topup payment create failed user_id=%d trade_no=%s payment_method=%s amount=%d error=%q", id, tradeNo, method, req.Amount, err.Error()))
		common.ApiErrorMsg(c, "BEpusdt 网关连接失败，请检查 BEpusdt 端点和密钥配置")
		return
	}
	topUp.ProviderPayload = common.GetJsonString(paymentOrder.Raw)
	_ = topUp.Update()
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

func BEpusdtTopUpNotify(c *gin.Context) {
	params, err := readBEpusdtCallback(c)
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("BEpusdt webhook 参数解析失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, common.GetClientIP(c), err.Error()))
		c.String(http.StatusBadRequest, "fail")
		return
	}
	tradeNo := service.BEpusdtCallbackTradeNo(params)
	if tradeNo == "" {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("BEpusdt webhook rejected reason=missing_order_id path=%q client_ip=%s", c.Request.RequestURI, common.GetClientIP(c)))
		c.String(http.StatusBadRequest, "fail")
		return
	}
	status := service.BEpusdtCallbackStatus(params)
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("BEpusdt topup webhook received trade_no=%s status=%s amount=%s fiat=%s client_ip=%s", tradeNo, status, bepusdtCallbackString(params, "amount"), bepusdtCallbackString(params, "fiat"), common.GetClientIP(c)))
	facts, err := validateUSDTGatewayCallback(params)
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("USDT gateway webhook rejected trade_no=%s path=%q client_ip=%s error=%q", tradeNo, c.Request.RequestURI, common.GetClientIP(c), err.Error()))
		c.String(http.StatusBadRequest, "fail")
		return
	}
	if !service.IsBEpusdtPaidStatus(status) {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("BEpusdt webhook 忽略非成功事件 trade_no=%s status=%s client_ip=%s", tradeNo, status, common.GetClientIP(c)))
		_, _ = c.Writer.Write([]byte("ok"))
		return
	}
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)
	if err := model.RechargeBEpusdtWithValidation(tradeNo, common.GetJsonString(params), model.PaymentCallbackValidation{
		ExpectedPaymentProvider: facts.Provider,
		ActualPaymentMethod:     facts.PaymentMethod,
		ActualPaymentToken:      facts.Token,
		PaidAmount:              facts.PaidAmount,
		PaidCurrency:            facts.PaidCurrency,
		RequirePaymentFacts:     true,
	}, common.GetClientIP(c)); err != nil {
		if isPermanentPaymentReviewError(err) {
			if recordErr := recordPaymentReview(c.Request.Context(), model.PaymentProviderBEpusdt, "", "topup.notify", tradeNo, "", "BEpusdt top-up payment requires manual review after payment succeeded", err, common.GetJsonString(params)); recordErr != nil {
				logger.LogError(c.Request.Context(), fmt.Sprintf("BEpusdt topup review record failed trade_no=%s client_ip=%s error=%q", tradeNo, common.GetClientIP(c), recordErr.Error()))
				c.String(http.StatusInternalServerError, "fail")
				return
			}
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("BEpusdt topup payment queued for manual review trade_no=%s client_ip=%s error=%q", tradeNo, common.GetClientIP(c), err.Error()))
			c.String(http.StatusOK, "ok")
			return
		}
		if isPaymentCallbackRejection(err) {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("BEpusdt topup webhook rejected trade_no=%s client_ip=%s error=%q", tradeNo, common.GetClientIP(c), err.Error()))
			c.String(http.StatusBadRequest, "fail")
			return
		}
		logger.LogError(c.Request.Context(), fmt.Sprintf("BEpusdt 充值处理失败 trade_no=%s client_ip=%s error=%q", tradeNo, common.GetClientIP(c), err.Error()))
		c.String(http.StatusInternalServerError, "fail")
		return
	}
	if err := processPaidTopUpCommission(c.Request.Context(), tradeNo); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("BEpusdt topup commission failed trade_no=%s client_ip=%s error=%q", tradeNo, common.GetClientIP(c), err.Error()))
		c.String(http.StatusInternalServerError, "fail")
		return
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("BEpusdt topup webhook processed trade_no=%s client_ip=%s", tradeNo, common.GetClientIP(c)))
	_, _ = c.Writer.Write([]byte("ok"))
}

func isPaymentCallbackRejection(err error) bool {
	return errors.Is(err, model.ErrPaymentMethodMismatch) ||
		errors.Is(err, model.ErrPaymentAmountMismatch) ||
		errors.Is(err, model.ErrPaymentCurrencyMismatch) ||
		errors.Is(err, model.ErrTopUpNotFound) ||
		errors.Is(err, model.ErrTopUpStatusInvalid) ||
		errors.Is(err, model.ErrSubscriptionOrderNotFound) ||
		errors.Is(err, model.ErrSubscriptionOrderStatusInvalid)
}

func validateUSDTGatewayCallback(params map[string]interface{}) (service.USDTGatewayCallbackFacts, error) {
	if !service.VerifyBEpusdtSignature(params) {
		return service.USDTGatewayCallbackFacts{}, errors.New("invalid signature")
	}
	method := service.BEpusdtCallbackMethod(params)
	if method == "" {
		method = service.USDTPaymentMethod
	}
	return service.USDTGatewayCallbackFacts{
		Provider:      model.PaymentProviderBEpusdt,
		PaymentMethod: method,
		Token:         service.BEpusdtCallbackToken(params),
		PaidAmount:    service.BEpusdtCallbackPaidAmount(params),
		PaidCurrency:  service.BEpusdtCallbackPaidCurrency(params),
	}, nil
}

func bepusdtCallbackString(params map[string]interface{}, key string) string {
	value, ok := params[key]
	if !ok || value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func readBEpusdtCallback(c *gin.Context) (map[string]interface{}, error) {
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
