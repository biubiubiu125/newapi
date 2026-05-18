package controller

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

func GetTopUpInfo(c *gin.Context) {
	complianceConfirmed := operation_setting.IsPaymentComplianceConfirmed()

	// 获取支付方式
	payMethods := clonePayMethods(operation_setting.PayMethods)
	if !complianceConfirmed {
		payMethods = []map[string]string{}
	}

	// 如果启用了 Stripe 支付，添加到支付方法列表
	if isStripeTopUpEnabled() {
		// 检查是否已经包含 Stripe
		hasStripe := false
		for _, method := range payMethods {
			if method["type"] == "stripe" {
				hasStripe = true
				break
			}
		}

		if !hasStripe {
			stripeMethod := map[string]string{
				"name":      "Stripe",
				"type":      "stripe",
				"color":     "rgba(var(--semi-purple-5), 1)",
				"min_topup": strconv.Itoa(setting.StripeMinTopUp),
			}
			payMethods = appendUniquePayMethod(payMethods, stripeMethod)
		}
	}

	// 如果启用了 Waffo 支付，添加到支付方法列表
	enableWaffo := isWaffoTopUpEnabled()
	if enableWaffo {
		hasWaffo := false
		for _, method := range payMethods {
			if method["type"] == model.PaymentMethodWaffo {
				hasWaffo = true
				break
			}
		}

		if !hasWaffo {
			waffoMethod := map[string]string{
				"name":      "Waffo (Global Payment)",
				"type":      model.PaymentMethodWaffo,
				"color":     "rgba(var(--semi-blue-5), 1)",
				"min_topup": strconv.Itoa(setting.WaffoMinTopUp),
			}
			payMethods = appendUniquePayMethod(payMethods, waffoMethod)
		}
	}

	enableWaffoPancake := isWaffoPancakeTopUpEnabled()
	if enableWaffoPancake {
		hasWaffoPancake := false
		for _, method := range payMethods {
			if method["type"] == model.PaymentMethodWaffoPancake {
				hasWaffoPancake = true
				break
			}
		}

		if !hasWaffoPancake {
			payMethods = appendUniquePayMethod(payMethods, map[string]string{
				"name":      "Waffo Pancake",
				"type":      model.PaymentMethodWaffoPancake,
				"color":     "rgba(var(--semi-orange-5), 1)",
				"min_topup": strconv.Itoa(setting.WaffoPancakeMinTopUp),
			})
		}
	}

	enableEpusdt := service.IsEpusdtConfigured()
	epusdtPayMethods := []map[string]string{}
	if enableEpusdt && complianceConfirmed {
		epusdtPayMethods = service.EpusdtAssetsForTopupMethods()
	}

	data := gin.H{
		"enable_online_topup":              isEpayTopUpEnabled(),
		"enable_stripe_topup":              isStripeTopUpEnabled(),
		"enable_creem_topup":               isCreemTopUpEnabled(),
		"enable_waffo_topup":               enableWaffo,
		"enable_waffo_pancake_topup":       enableWaffoPancake,
		"enable_epusdt_topup":              enableEpusdt,
		"enable_redemption":                complianceConfirmed,
		"payment_compliance_confirmed":     complianceConfirmed,
		"payment_compliance_terms_version": operation_setting.CurrentComplianceTermsVersion,
		"waffo_pay_methods": func() interface{} {
			if enableWaffo {
				return setting.GetWaffoPayMethods()
			}
			return nil
		}(),
		"creem_products":          setting.CreemProducts,
		"pay_methods":             payMethods,
		"epusdt_pay_methods":      epusdtPayMethods,
		"min_topup":               operation_setting.MinTopUp,
		"stripe_min_topup":        setting.StripeMinTopUp,
		"waffo_min_topup":         setting.WaffoMinTopUp,
		"waffo_pancake_min_topup": setting.WaffoPancakeMinTopUp,
		"epusdt_min_topup":        setting.EpusdtMinTopUp,
		"amount_options":          operation_setting.GetPaymentSetting().AmountOptions,
		"discount":                operation_setting.GetPaymentSetting().AmountDiscount,
		"topup_link":              common.TopUpLink,
	}
	common.ApiSuccess(c, data)
}

func clonePayMethods(methods []map[string]string) []map[string]string {
	cloned := make([]map[string]string, 0, len(methods))
	for _, method := range methods {
		item := make(map[string]string, len(method))
		for key, value := range method {
			item[key] = value
		}
		cloned = append(cloned, item)
	}
	return cloned
}

func appendUniquePayMethods(methods []map[string]string, additions ...map[string]string) []map[string]string {
	for _, addition := range additions {
		methods = appendUniquePayMethod(methods, addition)
	}
	return methods
}

func appendUniquePayMethod(methods []map[string]string, addition map[string]string) []map[string]string {
	paymentType := strings.TrimSpace(addition["type"])
	if paymentType == "" {
		return methods
	}
	for _, method := range methods {
		if strings.TrimSpace(method["type"]) == paymentType {
			return methods
		}
	}
	return append(methods, addition)
}

type EpayRequest struct {
	Amount        int64  `json:"amount"`
	PaymentMethod string `json:"payment_method"`
}

type AmountRequest struct {
	Amount int64 `json:"amount"`
}

type topUpOrderSnapshotInput struct {
	RequestAmount int64
	CreditAmount  int64
	PaidAmount    float64
	PaidCurrency  string
	UserGroup     string
}

func paymentDisplayCurrencySnapshot() string {
	switch operation_setting.GetQuotaDisplayType() {
	case operation_setting.QuotaDisplayTypeCNY:
		return "CNY"
	case operation_setting.QuotaDisplayTypeUSD:
		return "USD"
	case operation_setting.QuotaDisplayTypeCustom:
		return strings.TrimSpace(operation_setting.GetGeneralSetting().CustomCurrencySymbol)
	case operation_setting.QuotaDisplayTypeTokens:
		return "TOKENS"
	default:
		return strings.TrimSpace(operation_setting.GetQuotaDisplayType())
	}
}

func paymentAmountDiscountSnapshot(amount int64) float64 {
	if discount, ok := operation_setting.GetPaymentSetting().AmountDiscount[int(amount)]; ok && discount > 0 {
		return discount
	}
	return 1
}

func applyTopUpOrderSnapshot(topUp *model.TopUp, input topUpOrderSnapshotInput) {
	if topUp == nil {
		return
	}
	quotaPerUnit := common.QuotaPerUnit
	if quotaPerUnit <= 0 {
		quotaPerUnit = 1
	}
	groupRatio := common.GetTopupGroupRatio(input.UserGroup)
	if groupRatio <= 0 {
		groupRatio = 1
	}
	creditAmount := input.CreditAmount
	if creditAmount <= 0 {
		creditAmount = topUp.Amount
	}
	topUp.OrderSnapshotVersion = 1
	topUp.RequestAmountSnapshot = input.RequestAmount
	switch topUp.PaymentProvider {
	case model.PaymentProviderCreem:
		topUp.CreditQuotaSnapshot = creditAmount
	case model.PaymentProviderStripe:
		topUp.CreditQuotaSnapshot = decimal.NewFromFloat(topUp.Money).Mul(decimal.NewFromFloat(quotaPerUnit)).IntPart()
	default:
		topUp.CreditQuotaSnapshot = decimal.NewFromInt(creditAmount).Mul(decimal.NewFromFloat(quotaPerUnit)).IntPart()
	}
	topUp.QuotaPerUnitSnapshot = quotaPerUnit
	topUp.PriceSnapshot = operation_setting.Price
	topUp.USDExchangeRateSnapshot = operation_setting.USDExchangeRate
	topUp.CustomExchangeRateSnapshot = operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
	topUp.QuotaDisplayTypeSnapshot = operation_setting.GetQuotaDisplayType()
	topUp.DisplayCurrencySnapshot = paymentDisplayCurrencySnapshot()
	topUp.TopupGroupRatioSnapshot = groupRatio
	topUp.AmountDiscountSnapshot = paymentAmountDiscountSnapshot(input.RequestAmount)
	if topUp.PaidAmount <= 0 {
		topUp.PaidAmount = input.PaidAmount
	}
	if strings.TrimSpace(topUp.PaidCurrency) == "" {
		topUp.PaidCurrency = strings.ToUpper(strings.TrimSpace(input.PaidCurrency))
	}
	if strings.TrimSpace(topUp.ReferralBaseCurrency) == "" {
		topUp.ReferralBaseCurrency = strings.ToUpper(strings.TrimSpace(topUp.PaidCurrency))
	}
}

func applySubscriptionOrderSnapshot(order *model.SubscriptionOrder, plan *model.SubscriptionPlan, paidCurrency string) {
	if order == nil || plan == nil {
		return
	}
	order.OrderSnapshotVersion = 1
	order.PlanTitleSnapshot = plan.Title
	order.PlanPriceSnapshot = plan.PriceAmount
	order.PlanCurrencySnapshot = strings.ToUpper(strings.TrimSpace(plan.Currency))
	if strings.TrimSpace(order.PlanCurrencySnapshot) == "" {
		order.PlanCurrencySnapshot = strings.ToUpper(strings.TrimSpace(paidCurrency))
	}
	order.PlanDurationUnitSnapshot = plan.DurationUnit
	order.PlanDurationValueSnapshot = plan.DurationValue
	order.PlanCustomSecondsSnapshot = plan.CustomSeconds
	order.PlanTotalAmountSnapshot = plan.TotalAmount
	order.PlanQuotaResetPeriodSnapshot = plan.QuotaResetPeriod
	order.PlanQuotaResetCustomSecondsSnapshot = plan.QuotaResetCustomSeconds
	order.PlanUpgradeGroupSnapshot = strings.TrimSpace(plan.UpgradeGroup)
	order.USDExchangeRateSnapshot = operation_setting.USDExchangeRate
	order.CustomExchangeRateSnapshot = operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
	order.QuotaDisplayTypeSnapshot = operation_setting.GetQuotaDisplayType()
	order.DisplayCurrencySnapshot = paymentDisplayCurrencySnapshot()
	if strings.TrimSpace(order.ReferralBaseCurrency) == "" {
		order.ReferralBaseCurrency = strings.ToUpper(strings.TrimSpace(order.PaidCurrency))
	}
}

func GetEpayClient() *epay.Client {
	if operation_setting.PayAddress == "" || operation_setting.EpayId == "" || operation_setting.EpayKey == "" {
		return nil
	}
	withUrl, err := epay.NewClient(&epay.Config{
		PartnerID: operation_setting.EpayId,
		Key:       operation_setting.EpayKey,
	}, operation_setting.PayAddress)
	if err != nil {
		return nil
	}
	return withUrl
}

func getPayMoney(amount int64, group string) float64 {
	dAmount := decimal.NewFromInt(amount)
	// 充值金额以“展示类型”为准：
	// - USD/CNY: 前端传 amount 为金额单位；TOKENS: 前端传 tokens，需要换成 USD 金额
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		dAmount = dAmount.Div(dQuotaPerUnit)
	}

	topupGroupRatio := common.GetTopupGroupRatio(group)
	if topupGroupRatio == 0 {
		topupGroupRatio = 1
	}

	dTopupGroupRatio := decimal.NewFromFloat(topupGroupRatio)
	dPrice := decimal.NewFromFloat(operation_setting.Price)
	// apply optional preset discount by the original request amount (if configured), default 1.0
	discount := 1.0
	if ds, ok := operation_setting.GetPaymentSetting().AmountDiscount[int(amount)]; ok {
		if ds > 0 {
			discount = ds
		}
	}
	dDiscount := decimal.NewFromFloat(discount)

	payMoney := dAmount.Mul(dPrice).Mul(dTopupGroupRatio).Mul(dDiscount)

	return payMoney.InexactFloat64()
}

func getMinTopup() int64 {
	minTopup := operation_setting.MinTopUp
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		dMinTopup := decimal.NewFromInt(int64(minTopup))
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		minTopup = int(dMinTopup.Mul(dQuotaPerUnit).IntPart())
	}
	return int64(minTopup)
}

func RequestEpay(c *gin.Context) {
	var req EpayRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	if req.Amount < getMinTopup() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", getMinTopup())})
		return
	}

	id := c.GetInt("id")
	group, err := model.GetUserGroup(id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}
	payMoney := getPayMoney(req.Amount, group)
	snapshot, _ := referralService.BuildOrderSnapshot(id, payMoney, "CNY")
	if payMoney < 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}

	if !operation_setting.ContainsPayMethod(req.PaymentMethod) {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "支付方式不存在"})
		return
	}

	callBackAddress := service.GetCallbackAddress()
	returnUrl, _ := url.Parse(paymentReturnPath("/console/log"))
	notifyUrl, _ := url.Parse(callBackAddress + "/api/user/epay/notify")
	tradeNo := fmt.Sprintf("%s%d", common.GetRandomString(6), time.Now().Unix())
	tradeNo = fmt.Sprintf("USR%dNO%s", id, tradeNo)
	client := GetEpayClient()
	if client == nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "当前管理员未配置支付信息"})
		return
	}
	uri, params, err := client.Purchase(&epay.PurchaseArgs{
		Type:           req.PaymentMethod,
		ServiceTradeNo: tradeNo,
		Name:           fmt.Sprintf("TUC%d", req.Amount),
		Money:          strconv.FormatFloat(payMoney, 'f', 2, 64),
		Device:         epay.PC,
		NotifyUrl:      notifyUrl,
		ReturnUrl:      returnUrl,
	})
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 拉起支付失败 user_id=%d trade_no=%s payment_method=%s amount=%d error=%q", id, tradeNo, req.PaymentMethod, req.Amount, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}
	amount := req.Amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		dAmount := decimal.NewFromInt(int64(amount))
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		amount = dAmount.Div(dQuotaPerUnit).IntPart()
	}
	topUp := &model.TopUp{
		UserId:          id,
		Amount:          amount,
		Money:           payMoney,
		PaidAmount:      payMoney,
		PaidCurrency:    "CNY",
		TradeNo:         tradeNo,
		PaymentMethod:   req.PaymentMethod,
		PaymentProvider: model.PaymentProviderEpay,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	applyTopUpOrderSnapshot(topUp, topUpOrderSnapshotInput{
		RequestAmount: req.Amount,
		CreditAmount:  amount,
		PaidAmount:    payMoney,
		PaidCurrency:  "CNY",
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
	err = topUp.Insert()
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 创建充值订单失败 user_id=%d trade_no=%s payment_method=%s amount=%d error=%q", id, tradeNo, req.PaymentMethod, req.Amount, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("易支付 充值订单创建成功 user_id=%d trade_no=%s payment_method=%s amount=%d money=%.2f", id, tradeNo, req.PaymentMethod, req.Amount, payMoney))
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": params, "url": uri})
}

// tradeNo lock
var orderLocks sync.Map
var createLock sync.Mutex

// refCountedMutex 带引用计数的互斥锁，确保最后一个使用者才从 map 中删除
type refCountedMutex struct {
	mu       sync.Mutex
	refCount int
}

// LockOrder 尝试对给定订单号加锁
func LockOrder(tradeNo string) {
	createLock.Lock()
	var rcm *refCountedMutex
	if v, ok := orderLocks.Load(tradeNo); ok {
		rcm = v.(*refCountedMutex)
	} else {
		rcm = &refCountedMutex{}
		orderLocks.Store(tradeNo, rcm)
	}
	rcm.refCount++
	createLock.Unlock()
	rcm.mu.Lock()
}

// UnlockOrder 释放给定订单号的锁
func UnlockOrder(tradeNo string) {
	v, ok := orderLocks.Load(tradeNo)
	if !ok {
		return
	}
	rcm := v.(*refCountedMutex)
	rcm.mu.Unlock()

	createLock.Lock()
	rcm.refCount--
	if rcm.refCount == 0 {
		orderLocks.Delete(tradeNo)
	}
	createLock.Unlock()
}

func EpayNotify(c *gin.Context) {
	if !isEpayWebhookEnabled() {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("epay webhook rejected reason=webhook_disabled path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	var params map[string]string
	if c.Request.Method == "POST" {
		if err := c.Request.ParseForm(); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("epay webhook form parse failed path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
			_, _ = c.Writer.Write([]byte("fail"))
			return
		}
		params = lo.Reduce(lo.Keys(c.Request.PostForm), func(r map[string]string, t string, i int) map[string]string {
			r[t] = c.Request.PostForm.Get(t)
			return r
		}, map[string]string{})
	} else {
		params = lo.Reduce(lo.Keys(c.Request.URL.Query()), func(r map[string]string, t string, i int) map[string]string {
			r[t] = c.Request.URL.Query().Get(t)
			return r
		}, map[string]string{})
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("epay webhook received path=%q client_ip=%s method=%s param_count=%d", c.Request.RequestURI, c.ClientIP(), c.Request.Method, len(params)))

	if len(params) == 0 {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("epay webhook rejected reason=empty_params path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	client := GetEpayClient()
	if client == nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("epay webhook rejected reason=client_not_configured path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	verifyInfo, err := client.Verify(params)
	if err != nil || !verifyInfo.VerifyStatus {
		if err != nil {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("epay webhook signature verification failed path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		} else {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("epay webhook signature verification failed path=%q client_ip=%s verify_status=false", c.Request.RequestURI, c.ClientIP()))
		}
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	if !epayCallbackMerchantMatches(params) {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("epay webhook merchant mismatch trade_no=%s callback_pid=%s client_ip=%s", verifyInfo.ServiceTradeNo, strings.TrimSpace(params["pid"]), c.ClientIP()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("epay webhook signature verified trade_no=%s callback_type=%s trade_status=%s client_ip=%s", verifyInfo.ServiceTradeNo, verifyInfo.Type, verifyInfo.TradeStatus, c.ClientIP()))
	if verifyInfo.TradeStatus != epay.StatusTradeSuccess {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("epay webhook ignored non-success event trade_no=%s callback_type=%s trade_status=%s client_ip=%s", verifyInfo.ServiceTradeNo, verifyInfo.Type, verifyInfo.TradeStatus, c.ClientIP()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	LockOrder(verifyInfo.ServiceTradeNo)
	defer UnlockOrder(verifyInfo.ServiceTradeNo)

	if err := model.RechargeEpayWithValidation(verifyInfo.ServiceTradeNo, common.GetJsonString(verifyInfo), model.PaymentCallbackValidation{
		ExpectedPaymentProvider: model.PaymentProviderEpay,
		ActualPaymentMethod:     verifyInfo.Type,
		PaidAmount:              parseCallbackAmount(verifyInfo.Money),
		PaidCurrency:            "CNY",
		RequirePaymentFacts:     true,
	}, c.ClientIP()); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("epay topup processing failed trade_no=%s callback_type=%s client_ip=%s error=%q", verifyInfo.ServiceTradeNo, verifyInfo.Type, c.ClientIP(), err.Error()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	_ = referralService.ProcessTopUpCommission(verifyInfo.ServiceTradeNo)
	_, _ = c.Writer.Write([]byte("success"))
}

func epayCallbackMerchantMatches(params map[string]string) bool {
	return strings.TrimSpace(params["pid"]) != "" && strings.TrimSpace(params["pid"]) == strings.TrimSpace(operation_setting.EpayId)
}

/*
func epayNotifyLegacy(c *gin.Context) {
	if !isEpayWebhookEnabled() {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("易支付 webhook 被拒绝 reason=webhook_disabled path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	var params map[string]string

	if c.Request.Method == "POST" {
		// POST 请求：从 POST body 解析参数
		if err := c.Request.ParseForm(); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 webhook POST 表单解析失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
			_, _ = c.Writer.Write([]byte("fail"))
			return
		}
		params = lo.Reduce(lo.Keys(c.Request.PostForm), func(r map[string]string, t string, i int) map[string]string {
			r[t] = c.Request.PostForm.Get(t)
			return r
		}, map[string]string{})
	} else {
		// GET 请求：从 URL Query 解析参数
		params = lo.Reduce(lo.Keys(c.Request.URL.Query()), func(r map[string]string, t string, i int) map[string]string {
			r[t] = c.Request.URL.Query().Get(t)
			return r
		}, map[string]string{})
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("易支付 webhook 收到请求 path=%q client_ip=%s method=%s param_count=%d", c.Request.RequestURI, c.ClientIP(), c.Request.Method, len(params)))

	if len(params) == 0 {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("易支付 webhook 参数为空 path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	client := GetEpayClient()
	if client == nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 client 未初始化 path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		_, err := c.Writer.Write([]byte("fail"))
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 webhook 响应写入失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		}
		return
	}
	verifyInfo, err := client.Verify(params)
	if err == nil && verifyInfo.VerifyStatus {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("易支付 webhook 验签成功 trade_no=%s callback_type=%s trade_status=%s client_ip=%s", verifyInfo.ServiceTradeNo, verifyInfo.Type, verifyInfo.TradeStatus, c.ClientIP()))
		_, err := c.Writer.Write([]byte("success"))
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 webhook 响应写入失败 trade_no=%s client_ip=%s error=%q", verifyInfo.ServiceTradeNo, c.ClientIP(), err.Error()))
		}
	} else {
		_, err := c.Writer.Write([]byte("fail"))
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 webhook 响应写入失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		}
		if err != nil {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("易支付 webhook 验签失败 path=%q client_ip=%s verify_error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		} else {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("易支付 webhook 验签失败 path=%q client_ip=%s verify_status=false", c.Request.RequestURI, c.ClientIP()))
		}
		return
	}

	if verifyInfo.TradeStatus == epay.StatusTradeSuccess {
		LockOrder(verifyInfo.ServiceTradeNo)
		defer UnlockOrder(verifyInfo.ServiceTradeNo)
		topUp := model.GetTopUpByTradeNo(verifyInfo.ServiceTradeNo)
		if topUp == nil {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("易支付 回调订单不存在 trade_no=%s callback_type=%s client_ip=%s", verifyInfo.ServiceTradeNo, verifyInfo.Type, c.ClientIP()))
			return
		}
		if topUp.PaymentProvider != model.PaymentProviderEpay {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("易支付 订单支付网关不匹配 trade_no=%s order_provider=%s callback_type=%s client_ip=%s", verifyInfo.ServiceTradeNo, topUp.PaymentProvider, verifyInfo.Type, c.ClientIP()))
			return
		}
		if topUp.PaymentMethod != verifyInfo.Type {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("epay callback payment method mismatch trade_no=%s order_payment_method=%s actual_type=%s client_ip=%s", verifyInfo.ServiceTradeNo, topUp.PaymentMethod, verifyInfo.Type, c.ClientIP()))
			return
		}
		if !callbackAmountMatches(topUp.PaidAmount, verifyInfo.Money) {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("epay callback amount mismatch trade_no=%s order_amount=%.8f callback_amount=%s client_ip=%s", verifyInfo.ServiceTradeNo, topUp.PaidAmount, verifyInfo.Money, c.ClientIP()))
			return
		}
		if !callbackCurrencyMatches(topUp.PaidCurrency, "CNY") {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("epay callback currency mismatch trade_no=%s order_currency=%s callback_currency=CNY client_ip=%s", verifyInfo.ServiceTradeNo, topUp.PaidCurrency, c.ClientIP()))
			return
		}
		if topUp.Status == common.TopUpStatusPending {
			if topUp.PaymentMethod != verifyInfo.Type {
				logger.LogInfo(c.Request.Context(), fmt.Sprintf("易支付 实际支付方式与订单不同 trade_no=%s order_payment_method=%s actual_type=%s client_ip=%s", verifyInfo.ServiceTradeNo, topUp.PaymentMethod, verifyInfo.Type, c.ClientIP()))
				topUp.PaymentMethod = verifyInfo.Type
			}
			topUp.Status = common.TopUpStatusSuccess
			err := topUp.Update()
			if err != nil {
				logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 更新充值订单失败 trade_no=%s user_id=%d client_ip=%s error=%q topup=%q", topUp.TradeNo, topUp.UserId, c.ClientIP(), err.Error(), common.GetJsonString(topUp)))
				return
			}
			//user, _ := model.GetUserById(topUp.UserId, false)
			//user.Quota += topUp.Amount * 500000
			dAmount := decimal.NewFromInt(int64(topUp.Amount))
			dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
			quotaToAdd := int(dAmount.Mul(dQuotaPerUnit).IntPart())
			err = model.IncreaseUserQuota(topUp.UserId, quotaToAdd, true)
			if err != nil {
				logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 更新用户额度失败 trade_no=%s user_id=%d client_ip=%s quota_to_add=%d error=%q topup=%q", topUp.TradeNo, topUp.UserId, c.ClientIP(), quotaToAdd, err.Error(), common.GetJsonString(topUp)))
				return
			}
			logger.LogInfo(c.Request.Context(), fmt.Sprintf("易支付 充值成功 trade_no=%s user_id=%d client_ip=%s quota_to_add=%d money=%.2f topup=%q", topUp.TradeNo, topUp.UserId, c.ClientIP(), quotaToAdd, topUp.Money, common.GetJsonString(topUp)))
			model.RecordTopupLog(topUp.UserId, fmt.Sprintf("使用在线充值成功，充值金额: %v，支付金额：%f", logger.LogQuota(quotaToAdd), topUp.Money), c.ClientIP(), topUp.PaymentMethod, "epay")
			_ = referralService.ProcessTopUpCommission(topUp.TradeNo)
		}
	} else {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("易支付 webhook 忽略事件 trade_no=%s callback_type=%s trade_status=%s client_ip=%s", verifyInfo.ServiceTradeNo, verifyInfo.Type, verifyInfo.TradeStatus, c.ClientIP()))
	}
}
*/

func RequestAmount(c *gin.Context) {
	var req AmountRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}

	if req.Amount < getMinTopup() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", getMinTopup())})
		return
	}
	id := c.GetInt("id")
	group, err := model.GetUserGroup(id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}
	payMoney := getPayMoney(req.Amount, group)
	if payMoney <= 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": strconv.FormatFloat(payMoney, 'f', 2, 64)})
}

func parseCallbackAmount(value string) float64 {
	amount, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return -1
	}
	return amount
}

func callbackAmountMatches(expected float64, actual string) bool {
	actualAmount := parseCallbackAmount(actual)
	if actualAmount <= 0 {
		return false
	}
	return decimal.NewFromFloat(expected).Round(8).Equal(decimal.NewFromFloat(actualAmount).Round(8))
}

func callbackCurrencyMatches(expected string, actual string) bool {
	expected = strings.ToUpper(strings.TrimSpace(expected))
	actual = strings.ToUpper(strings.TrimSpace(actual))
	if expected == "" || actual == "" {
		return expected == actual
	}
	return expected == actual
}

func GetUserTopUps(c *gin.Context) {
	userId := c.GetInt("id")
	pageInfo := common.GetPageQuery(c)
	keyword := c.Query("keyword")

	var (
		topups []*model.TopUp
		total  int64
		err    error
	)
	if keyword != "" {
		topups, total, err = model.SearchUserTopUps(userId, keyword, pageInfo)
	} else {
		topups, total, err = model.GetUserTopUps(userId, pageInfo)
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(topups)
	common.ApiSuccess(c, pageInfo)
}

// GetAllTopUps 管理员获取全平台充值记录
func GetAllTopUps(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	keyword := c.Query("keyword")

	var (
		topups []*model.TopUp
		total  int64
		err    error
	)
	if keyword != "" {
		topups, total, err = model.SearchAllTopUps(keyword, pageInfo)
	} else {
		topups, total, err = model.GetAllTopUps(pageInfo)
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(topups)
	common.ApiSuccess(c, pageInfo)
}

type AdminCompleteTopupRequest struct {
	TradeNo string `json:"trade_no"`
}

// AdminCompleteTopUp 管理员补单接口
func AdminCompleteTopUp(c *gin.Context) {
	var req AdminCompleteTopupRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.TradeNo == "" {
		common.ApiErrorMsg(c, "参数错误")
		return
	}

	// 订单级互斥，防止并发补单
	LockOrder(req.TradeNo)
	defer UnlockOrder(req.TradeNo)

	if err := model.ManualCompleteTopUp(req.TradeNo, c.ClientIP()); err != nil {
		common.ApiError(c, err)
		return
	}
	_ = referralService.ProcessTopUpCommission(req.TradeNo)
	common.ApiSuccess(c, nil)
}
