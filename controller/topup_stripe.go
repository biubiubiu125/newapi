package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/checkout/session"
	"github.com/stripe/stripe-go/v81/webhook"
	"github.com/thanhpk/randstr"
)

var (
	stripeAdaptor       = &StripeAdaptor{}
	stripeSessionNew    = session.New
	stripeSessionExpire = session.Expire
)

// StripePayRequest represents a payment request for Stripe checkout.
type StripePayRequest struct {
	// Amount is the quantity of units to purchase.
	Amount int64 `json:"amount"`
	// PaymentMethod specifies the payment method (e.g., "stripe").
	PaymentMethod string `json:"payment_method"`
	// SuccessURL is the optional custom URL to redirect after successful payment.
	// If empty, defaults to the server's console log page.
	SuccessURL string `json:"success_url,omitempty"`
	// CancelURL is the optional custom URL to redirect when payment is canceled.
	// If empty, defaults to the server's console topup page.
	CancelURL string `json:"cancel_url,omitempty"`
}

type StripeAdaptor struct {
}

func (*StripeAdaptor) RequestAmount(c *gin.Context, req *StripePayRequest) {
	if req.Amount < getStripeMinTopup() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", getStripeMinTopup())})
		return
	}
	id := c.GetInt("id")
	group, err := model.GetUserGroup(id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}
	payMoney := getStripePayMoney(float64(req.Amount), group)
	if payMoney <= 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": strconv.FormatFloat(payMoney, 'f', 2, 64)})
}

func (*StripeAdaptor) RequestPay(c *gin.Context, req *StripePayRequest) {
	if req.PaymentMethod != model.PaymentMethodStripe {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "不支持的支付渠道"})
		return
	}
	if !isStripeTopUpEnabled() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "Stripe 支付未启用"})
		return
	}
	if req.Amount < getStripeMinTopup() {
		c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("充值数量不能小于 %d", getStripeMinTopup()), "data": 10})
		return
	}
	if req.Amount > 10000 {
		c.JSON(http.StatusOK, gin.H{"message": "充值数量不能大于 10000", "data": 10})
		return
	}

	if req.SuccessURL != "" && common.ValidateRedirectURL(req.SuccessURL) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "支付成功重定向URL不在可信任域名列表中", "data": ""})
		return
	}

	if req.CancelURL != "" && common.ValidateRedirectURL(req.CancelURL) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "支付取消重定向URL不在可信任域名列表中", "data": ""})
		return
	}

	id := c.GetInt("id")
	user, err := model.GetUserById(id, false)
	if err != nil || user == nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("Stripe 创建充值订单时用户不可用 user_id=%d error=%v", id, err))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "用户不存在或已失效"})
		return
	}
	chargedMoney := getStripePayMoney(float64(req.Amount), user.Group)
	snapshot, _ := referralService.BuildOrderSnapshot(id, chargedMoney, "USD")

	reference := fmt.Sprintf("new-api-ref-%d-%d-%s", user.Id, time.Now().UnixMilli(), randstr.String(4))
	referenceId := "ref_" + common.Sha1([]byte(reference))

	topUp := &model.TopUp{
		UserId:          id,
		Amount:          req.Amount,
		Money:           chargedMoney,
		PaidAmount:      chargedMoney,
		PaidCurrency:    "USD",
		TradeNo:         referenceId,
		PaymentMethod:   model.PaymentMethodStripe,
		PaymentProvider: model.PaymentProviderStripe,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	applyTopUpOrderSnapshot(topUp, topUpOrderSnapshotInput{
		RequestAmount: req.Amount,
		CreditAmount:  req.Amount,
		PaidAmount:    chargedMoney,
		PaidCurrency:  "USD",
		UserGroup:     user.Group,
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
		logger.LogError(c.Request.Context(), fmt.Sprintf("Stripe 创建充值订单失败 user_id=%d trade_no=%s amount=%d error=%q", id, referenceId, req.Amount, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}
	checkoutSession, err := genStripeLink(referenceId, user.StripeCustomer, user.Email, req.Amount, chargedMoney, req.SuccessURL, req.CancelURL, stripeTopUpCheckoutMetadata(topUp))
	if err != nil {
		if statusErr := model.UpdatePendingTopUpStatus(referenceId, model.PaymentProviderStripe, common.TopUpStatusFailed); statusErr != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Stripe Checkout Session 失败后标记充值订单失败失败 user_id=%d trade_no=%s error=%q", id, referenceId, statusErr.Error()))
		}
		logger.LogError(c.Request.Context(), fmt.Sprintf("Stripe 创建 Checkout Session 失败 user_id=%d trade_no=%s amount=%d error=%q", id, referenceId, req.Amount, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("Stripe 充值订单创建成功 user_id=%d trade_no=%s amount=%d money=%.2f", id, referenceId, req.Amount, chargedMoney))
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"pay_link": checkoutSession.URL,
			"order_id": referenceId,
			"trade_no": referenceId,
		},
	})
}

func RequestStripeAmount(c *gin.Context) {
	var req StripePayRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	stripeAdaptor.RequestAmount(c, &req)
}

func RequestStripePay(c *gin.Context) {
	var req StripePayRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	stripeAdaptor.RequestPay(c, &req)
}

func StripeWebhook(c *gin.Context) {
	ctx := c.Request.Context()
	if !isStripeWebhookEnabled() {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe webhook 被拒绝 reason=webhook_disabled path=%q client_ip=%s", c.Request.RequestURI, common.GetClientIP(c)))
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	payload, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Stripe webhook 读取请求体失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, common.GetClientIP(c), err.Error()))
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}

	signature := c.GetHeader("Stripe-Signature")
	logger.LogInfo(ctx, fmt.Sprintf("Stripe webhook 收到请求 path=%q client_ip=%s body_size=%d", c.Request.RequestURI, common.GetClientIP(c), len(payload)))
	event, err := webhook.ConstructEventWithOptions(payload, signature, stripeWebhookSecret(), webhook.ConstructEventOptions{
		IgnoreAPIVersionMismatch: true,
	})

	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe webhook 验签失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, common.GetClientIP(c), err.Error()))
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	callerIp := common.GetClientIP(c)
	logger.LogInfo(ctx, fmt.Sprintf("Stripe webhook 验签成功 event_type=%s client_ip=%s path=%q", string(event.Type), callerIp, c.Request.RequestURI))
	var handlerErr error
	switch event.Type {
	case stripe.EventTypeCheckoutSessionCompleted:
		handlerErr = sessionCompleted(ctx, event, callerIp)
	case stripe.EventTypeCheckoutSessionExpired:
		handlerErr = sessionExpired(ctx, event)
	case stripe.EventTypeCheckoutSessionAsyncPaymentSucceeded:
		handlerErr = sessionAsyncPaymentSucceeded(ctx, event, callerIp)
	case stripe.EventTypeCheckoutSessionAsyncPaymentFailed:
		handlerErr = sessionAsyncPaymentFailed(ctx, event, callerIp)
	default:
		logger.LogInfo(ctx, fmt.Sprintf("Stripe webhook 忽略事件 event_type=%s client_ip=%s", string(event.Type), callerIp))
	}
	if handlerErr != nil {
		logger.LogError(ctx, fmt.Sprintf("Stripe webhook processing failed event_type=%s client_ip=%s error=%q", string(event.Type), callerIp, handlerErr.Error()))
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	c.Status(http.StatusOK)
}

func sessionCompleted(ctx context.Context, event stripe.Event, callerIp string) error {
	customerId := event.GetObjectValue("customer")
	referenceId := event.GetObjectValue("client_reference_id")
	status := event.GetObjectValue("status")
	if "complete" != status {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe checkout.completed 状态异常，忽略处理 trade_no=%s status=%s client_ip=%s", referenceId, status, callerIp))
		return nil
	}

	paymentStatus := event.GetObjectValue("payment_status")
	if paymentStatus != "paid" && !stripeNoCostTopUpPaymentConfirmed(event, referenceId) {
		logger.LogInfo(ctx, fmt.Sprintf("Stripe Checkout 支付未完成，等待异步结果 trade_no=%s payment_status=%s client_ip=%s", referenceId, paymentStatus, callerIp))
		return nil
	}

	return fulfillOrder(ctx, event, referenceId, customerId, callerIp)
}

// sessionAsyncPaymentSucceeded handles delayed payment methods (bank transfer, SEPA, etc.)
// that confirm payment after the checkout session completes.
func sessionAsyncPaymentSucceeded(ctx context.Context, event stripe.Event, callerIp string) error {
	customerId := event.GetObjectValue("customer")
	referenceId := event.GetObjectValue("client_reference_id")
	logger.LogInfo(ctx, fmt.Sprintf("Stripe 异步支付成功 trade_no=%s client_ip=%s", referenceId, callerIp))

	return fulfillOrder(ctx, event, referenceId, customerId, callerIp)
}

// sessionAsyncPaymentFailed marks orders as failed when delayed payment methods
// ultimately fail (e.g. bank transfer not received, SEPA rejected).
func sessionAsyncPaymentFailed(ctx context.Context, event stripe.Event, callerIp string) error {
	referenceId := event.GetObjectValue("client_reference_id")
	logger.LogWarn(ctx, fmt.Sprintf("Stripe 异步支付失败 trade_no=%s client_ip=%s", referenceId, callerIp))

	if len(referenceId) == 0 {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe 异步支付失败事件缺少订单号 client_ip=%s", callerIp))
		return nil
	}

	LockOrder(referenceId)
	defer UnlockOrder(referenceId)

	if err := model.FailSubscriptionOrder(referenceId, model.PaymentProviderStripe); err == nil {
		logger.LogInfo(ctx, fmt.Sprintf("Stripe async payment failed: subscription order marked failed trade_no=%s client_ip=%s", referenceId, callerIp))
		return nil
	} else if errors.Is(err, model.ErrPaymentMethodMismatch) {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe async payment failed: subscription order payment provider mismatch, acknowledging permanent event trade_no=%s client_ip=%s", referenceId, callerIp))
		return nil
	} else if err != nil &&
		!errors.Is(err, model.ErrSubscriptionOrderNotFound) &&
		!errors.Is(err, model.ErrPaymentAmountMismatch) &&
		!errors.Is(err, model.ErrPaymentCurrencyMismatch) {
		logger.LogError(ctx, fmt.Sprintf("Stripe async payment failed: subscription order status update failed trade_no=%s client_ip=%s error=%q", referenceId, callerIp, err.Error()))
		return err
	}

	if err := model.UpdatePendingTopUpStatus(referenceId, model.PaymentProviderStripe, common.TopUpStatusFailed); err != nil {
		if errors.Is(err, model.ErrTopUpNotFound) {
			logger.LogWarn(ctx, fmt.Sprintf("Stripe 异步支付失败但本地订单不存在 trade_no=%s client_ip=%s", referenceId, callerIp))
			return nil
		}
		if errors.Is(err, model.ErrPaymentMethodMismatch) {
			logger.LogWarn(ctx, fmt.Sprintf("Stripe 异步支付失败但订单支付网关不匹配 trade_no=%s client_ip=%s", referenceId, callerIp))
			return nil
		}
		if errors.Is(err, model.ErrTopUpStatusInvalid) {
			logger.LogInfo(ctx, fmt.Sprintf("Stripe 异步支付失败但订单状态非 pending，忽略处理 trade_no=%s client_ip=%s", referenceId, callerIp))
			return nil
		}
		logger.LogError(ctx, fmt.Sprintf("Stripe 标记充值订单失败状态失败 trade_no=%s client_ip=%s error=%q", referenceId, callerIp, err.Error()))
		return err
	}
	logger.LogInfo(ctx, fmt.Sprintf("Stripe 充值订单已标记为失败 trade_no=%s client_ip=%s", referenceId, callerIp))
	return nil
}

// fulfillOrder is the shared logic for crediting quota after payment is confirmed.
func fulfillOrder(ctx context.Context, event stripe.Event, referenceId string, customerId string, callerIp string) error {
	if len(referenceId) == 0 {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe 完成订单时缺少订单号 client_ip=%s", callerIp))
		return nil
	}

	LockOrder(referenceId)
	defer UnlockOrder(referenceId)
	paidAmount, paidCurrency, err := stripePaymentFacts(event)
	if err != nil {
		if recordErr := recordStripePaymentOrphanEvent(ctx, event, referenceId, "payment facts invalid after stripe payment succeeded", err); recordErr != nil {
			return recordErr
		}
		logger.LogWarn(ctx, fmt.Sprintf("Stripe payment succeeded but payment facts are invalid trade_no=%s event_type=%s client_ip=%s error=%q", referenceId, string(event.Type), callerIp, err.Error()))
		return nil
	}
	payload := map[string]any{
		"customer":       customerId,
		"amount_total":   event.GetObjectValue("amount_total"),
		"currency":       strings.ToUpper(event.GetObjectValue("currency")),
		"credit_quota":   stripeEventMetadataValue(event, "credit_quota"),
		"quota_per_unit": stripeEventMetadataValue(event, "quota_per_unit"),
		"event_type":     string(event.Type),
	}
	subscriptionValidation := model.PaymentCallbackValidation{
		ExpectedPaymentProvider: model.PaymentProviderStripe,
		ActualPaymentMethod:     model.PaymentMethodStripe,
		StripeCustomer:          customerId,
		PaidAmount:              paidAmount,
		PaidCurrency:            paidCurrency,
		RequirePaymentFacts:     true,
		CallerIP:                callerIp,
	}
	if err = model.CompleteSubscriptionOrderWithValidation(referenceId, common.GetJsonString(payload), subscriptionValidation); err == nil {
		if err := processPaidSubscriptionCommission(ctx, referenceId); err != nil {
			return err
		}
		logger.LogInfo(ctx, fmt.Sprintf("Stripe 订阅订单处理成功 trade_no=%s event_type=%s client_ip=%s", referenceId, string(event.Type), callerIp))
		return nil
	} else if errors.Is(err, model.ErrPaymentMethodMismatch) || errors.Is(err, model.ErrSubscriptionOrderStatusInvalid) {
		if recordErr := recordStripePaymentOrphanEvent(ctx, event, referenceId, "subscription payment requires manual review after stripe payment succeeded", err); recordErr != nil {
			return recordErr
		}
		logger.LogWarn(ctx, fmt.Sprintf("Stripe subscription payment queued for manual review trade_no=%s event_type=%s client_ip=%s error=%q", referenceId, string(event.Type), callerIp, err.Error()))
		return nil
	} else if errors.Is(err, model.ErrSubscriptionPurchaseLimit) {
		if recordErr := recordStripePaymentOrphanEvent(ctx, event, referenceId, model.PaymentOrphanReasonStripeSubscriptionPurchaseLimitAfterPaymentSucceeded, err); recordErr != nil {
			return recordErr
		}
		logger.LogWarn(ctx, fmt.Sprintf("Stripe subscription payment exceeded the purchase limit; queued for manual review trade_no=%s event_type=%s client_ip=%s error=%q", referenceId, string(event.Type), callerIp, err.Error()))
		return nil
	} else if err != nil &&
		!errors.Is(err, model.ErrSubscriptionOrderNotFound) &&
		!errors.Is(err, model.ErrPaymentAmountMismatch) &&
		!errors.Is(err, model.ErrPaymentCurrencyMismatch) {
		logger.LogError(ctx, fmt.Sprintf("Stripe 订阅订单处理失败 trade_no=%s event_type=%s client_ip=%s error=%q", referenceId, string(event.Type), callerIp, err.Error()))
		return err
	}

	if errors.Is(err, model.ErrPaymentAmountMismatch) || errors.Is(err, model.ErrPaymentCurrencyMismatch) {
		if recordErr := recordStripePaymentOrphanEvent(ctx, event, referenceId, "subscription payment facts mismatch after stripe payment succeeded", err); recordErr != nil {
			return recordErr
		}
		logger.LogWarn(ctx, fmt.Sprintf("Stripe subscription payment facts mismatch trade_no=%s event_type=%s client_ip=%s error=%q", referenceId, string(event.Type), callerIp, err.Error()))
		return nil
	}

	topUpValidation := model.PaymentCallbackValidation{
		ExpectedPaymentProvider: model.PaymentProviderStripe,
		ActualPaymentMethod:     model.PaymentMethodStripe,
		PaidAmount:              paidAmount,
		PaidCurrency:            paidCurrency,
		RequirePaymentFacts:     true,
		AllowPaymentDiscount:    stripeEventMetadataBool(event, "promotion_codes_enabled") && stripeEventHasDiscount(event),
		CallerIP:                callerIp,
	}
	err = model.RechargeStripeWithValidation(referenceId, customerId, common.GetJsonString(payload), topUpValidation)
	if errors.Is(err, model.ErrTopUpNotFound) {
		if recordErr := recordStripePaymentOrphanEvent(ctx, event, referenceId, model.PaymentOrphanReasonStripeLocalOrderMissingAfterPaymentSucceeded, nil); recordErr != nil {
			return recordErr
		}
		logger.LogWarn(ctx, fmt.Sprintf("Stripe payment succeeded but local order is missing; recorded orphan payment for manual review trade_no=%s event_type=%s client_ip=%s", referenceId, string(event.Type), callerIp))
		return nil
	}
	if errors.Is(err, model.ErrPaymentAmountMismatch) || errors.Is(err, model.ErrPaymentCurrencyMismatch) {
		if recordErr := recordStripePaymentOrphanEvent(ctx, event, referenceId, "topup payment facts mismatch after stripe payment succeeded", err); recordErr != nil {
			return recordErr
		}
		logger.LogWarn(ctx, fmt.Sprintf("Stripe topup payment facts mismatch trade_no=%s event_type=%s client_ip=%s error=%q", referenceId, string(event.Type), callerIp, err.Error()))
		return nil
	}
	if errors.Is(err, model.ErrPaymentMethodMismatch) || errors.Is(err, model.ErrTopUpStatusInvalid) {
		if recordErr := recordStripePaymentOrphanEvent(ctx, event, referenceId, "top-up payment requires manual review after stripe payment succeeded", err); recordErr != nil {
			return recordErr
		}
		logger.LogWarn(ctx, fmt.Sprintf("Stripe top-up payment queued for manual review trade_no=%s event_type=%s client_ip=%s error=%q", referenceId, string(event.Type), callerIp, err.Error()))
		return nil
	}
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Stripe 充值处理失败 trade_no=%s event_type=%s client_ip=%s error=%q", referenceId, string(event.Type), callerIp, err.Error()))
		return err
	}
	if err := processPaidTopUpCommission(ctx, referenceId); err != nil {
		return err
	}

	total, _ := strconv.ParseFloat(event.GetObjectValue("amount_total"), 64)
	currency := strings.ToUpper(event.GetObjectValue("currency"))
	logger.LogInfo(ctx, fmt.Sprintf("Stripe 充值成功 trade_no=%s amount_total=%.2f currency=%s event_type=%s client_ip=%s", referenceId, total/100, currency, string(event.Type), callerIp))
	return nil
}

func sessionExpired(ctx context.Context, event stripe.Event) error {
	referenceId := event.GetObjectValue("client_reference_id")
	status := event.GetObjectValue("status")
	if "expired" != status {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe checkout.expired 状态异常，忽略处理 trade_no=%s status=%s", referenceId, status))
		return nil
	}

	if len(referenceId) == 0 {
		logger.LogWarn(ctx, "Stripe checkout.expired 缺少订单号")
		return nil
	}

	// Subscription order expiration
	LockOrder(referenceId)
	defer UnlockOrder(referenceId)
	if err := model.ExpireSubscriptionOrder(referenceId, model.PaymentProviderStripe); err == nil {
		logger.LogInfo(ctx, fmt.Sprintf("Stripe 订阅订单已过期 trade_no=%s", referenceId))
		return nil
	} else if errors.Is(err, model.ErrPaymentMethodMismatch) {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe checkout.expired 订阅订单支付网关不匹配，确认永久不可处理事件 trade_no=%s", referenceId))
		return nil
	} else if err != nil &&
		!errors.Is(err, model.ErrSubscriptionOrderNotFound) &&
		!errors.Is(err, model.ErrPaymentAmountMismatch) &&
		!errors.Is(err, model.ErrPaymentCurrencyMismatch) {
		logger.LogError(ctx, fmt.Sprintf("Stripe 订阅订单过期处理失败 trade_no=%s error=%q", referenceId, err.Error()))
		return err
	}

	err := model.UpdatePendingTopUpStatus(referenceId, model.PaymentProviderStripe, common.TopUpStatusExpired)
	if errors.Is(err, model.ErrTopUpNotFound) {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe 充值订单不存在，无法标记过期 trade_no=%s", referenceId))
		return nil
	}
	if errors.Is(err, model.ErrPaymentMethodMismatch) {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe checkout.expired 订单支付网关不匹配，忽略处理 trade_no=%s", referenceId))
		return nil
	}
	if errors.Is(err, model.ErrTopUpStatusInvalid) {
		logger.LogInfo(ctx, fmt.Sprintf("Stripe checkout.expired 订单状态非 pending，按幂等事件忽略 trade_no=%s", referenceId))
		return nil
	}
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Stripe 充值订单过期处理失败 trade_no=%s error=%q", referenceId, err.Error()))
		return err
	}

	logger.LogInfo(ctx, fmt.Sprintf("Stripe 充值订单已过期 trade_no=%s", referenceId))
	return nil
}

// genStripeLink generates a Stripe Checkout session URL for payment.
// It creates a new checkout session with the specified parameters and returns the payment URL.
//
// Parameters:
//   - referenceId: unique reference identifier for the transaction
//   - customerId: existing Stripe customer ID (empty string if new customer)
//   - email: customer email address for new customer creation
//   - amount: quantity of units to purchase
//   - successURL: custom URL to redirect after successful payment (empty for default)
//   - cancelURL: custom URL to redirect when payment is canceled (empty for default)
//
// Returns the checkout session or an error if the session creation fails.
func stripeTopUpCheckoutMetadata(topUp *model.TopUp) map[string]string {
	metadata := make(map[string]string)
	if topUp == nil {
		return metadata
	}
	addStripeCheckoutMetadata(metadata, "user_id", strconv.Itoa(topUp.UserId))
	requestAmount := topUp.RequestAmountSnapshot
	if requestAmount <= 0 {
		requestAmount = topUp.Amount
	}
	addStripeCheckoutMetadata(metadata, "request_amount", strconv.FormatInt(requestAmount, 10))
	addStripeCheckoutMetadata(metadata, "credit_quota", strconv.FormatInt(topUp.CreditQuotaSnapshot, 10))
	addStripeCheckoutMetadata(metadata, "quota_per_unit", strconv.FormatFloat(topUp.QuotaPerUnitSnapshot, 'f', -1, 64))
	addStripeCheckoutMetadata(metadata, "paid_amount", strconv.FormatFloat(topUp.PaidAmount, 'f', -1, 64))
	addStripeCheckoutMetadata(metadata, "paid_currency", topUp.PaidCurrency)
	addStripeCheckoutMetadata(metadata, "promotion_codes_enabled", strconv.FormatBool(setting.StripePromotionCodesEnabled))
	addStripeCheckoutMetadata(metadata, "price_snapshot", strconv.FormatFloat(topUp.PriceSnapshot, 'f', -1, 64))
	addStripeCheckoutMetadata(metadata, "usd_exchange_rate_snapshot", strconv.FormatFloat(topUp.USDExchangeRateSnapshot, 'f', -1, 64))
	addStripeCheckoutMetadata(metadata, "custom_exchange_rate_snapshot", strconv.FormatFloat(topUp.CustomExchangeRateSnapshot, 'f', -1, 64))
	addStripeCheckoutMetadata(metadata, "quota_display_type_snapshot", topUp.QuotaDisplayTypeSnapshot)
	addStripeCheckoutMetadata(metadata, "display_currency_snapshot", topUp.DisplayCurrencySnapshot)
	addStripeCheckoutMetadata(metadata, "topup_group_ratio_snapshot", strconv.FormatFloat(topUp.TopupGroupRatioSnapshot, 'f', -1, 64))
	addStripeCheckoutMetadata(metadata, "amount_discount_snapshot", strconv.FormatFloat(topUp.AmountDiscountSnapshot, 'f', -1, 64))
	addStripeCheckoutMetadata(metadata, "referral_affiliate_id", strconv.Itoa(topUp.ReferralAffiliateId))
	addStripeCheckoutMetadata(metadata, "referral_rate", strconv.FormatFloat(topUp.ReferralRate, 'f', -1, 64))
	addStripeCheckoutMetadata(metadata, "referral_base_amount", strconv.FormatFloat(topUp.ReferralBaseAmount, 'f', -1, 64))
	addStripeCheckoutMetadata(metadata, "referral_base_currency", topUp.ReferralBaseCurrency)
	addStripeCheckoutMetadata(metadata, "referral_commission_status", topUp.ReferralCommissionStatus)
	addStripeCheckoutMetadata(metadata, "referral_commission_error", topUp.ReferralCommissionError)
	return metadata
}

func addStripeCheckoutMetadata(metadata map[string]string, key string, value string) {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" || value == "" || value == "0" {
		return
	}
	metadata[key] = value
}

func genStripeLink(referenceId string, customerId string, email string, amount int64, chargedMoney float64, successURL string, cancelURL string, metadata map[string]string) (*stripe.CheckoutSession, error) {
	if !isStripeAPISecretConfigured() {
		return nil, fmt.Errorf("无效的Stripe API密钥")
	}

	stripe.Key = stripeAPISecret()

	priceID := stripePriceId()
	if priceID == "" {
		return nil, fmt.Errorf("无效的Stripe price id")
	}

	// Use custom URLs if provided, otherwise use defaults
	if successURL == "" {
		successURL = paymentReturnPath("/usage-logs")
	}
	if cancelURL == "" {
		cancelURL = paymentReturnPath("/wallet")
	}

	lineItem := &stripe.CheckoutSessionLineItemParams{
		Quantity: stripe.Int64(1),
	}
	if stripeTopUpUsesConfiguredPrice(amount, chargedMoney) {
		lineItem.Price = stripe.String(priceID)
		lineItem.Quantity = stripe.Int64(amount)
	} else {
		unitAmount, err := stripeTopUpUnitAmount(chargedMoney)
		if err != nil {
			return nil, err
		}
		lineItem.PriceData = &stripe.CheckoutSessionLineItemPriceDataParams{
			Currency:   stripe.String("usd"),
			UnitAmount: stripe.Int64(unitAmount),
			ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
				Name: stripe.String("New API top-up"),
			},
		}
	}

	params := &stripe.CheckoutSessionParams{
		ClientReferenceID:   stripe.String(referenceId),
		SuccessURL:          stripe.String(successURL),
		CancelURL:           stripe.String(cancelURL),
		LineItems:           []*stripe.CheckoutSessionLineItemParams{lineItem},
		Mode:                stripe.String(string(stripe.CheckoutSessionModePayment)),
		AllowPromotionCodes: stripe.Bool(setting.StripePromotionCodesEnabled),
	}
	for key, value := range metadata {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		params.AddMetadata(key, value)
	}

	if "" == customerId {
		if "" != email {
			params.CustomerEmail = stripe.String(email)
		}

		params.CustomerCreation = stripe.String(string(stripe.CheckoutSessionCustomerCreationAlways))
	} else {
		params.Customer = stripe.String(customerId)
	}

	result, err := stripeSessionNew(params)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func stripeTopUpUsesConfiguredPrice(amount int64, chargedMoney float64) bool {
	baseAmount := float64(amount)
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens &&
		common.QuotaPerUnit > 0 {
		baseAmount /= common.QuotaPerUnit
	}
	baseAmount *= setting.StripeUnitPrice
	return decimal.NewFromFloat(baseAmount).Round(8).Equal(decimal.NewFromFloat(chargedMoney).Round(8))
}

func stripeTopUpUnitAmount(chargedMoney float64) (int64, error) {
	normalized, err := model.NormalizePaymentAmount(chargedMoney, "USD")
	if err != nil {
		return 0, fmt.Errorf("invalid Stripe top-up amount: %w", err)
	}
	unitAmount := decimal.NewFromFloat(normalized).Mul(decimal.NewFromInt(100)).Round(0)
	if !unitAmount.IsPositive() {
		return 0, fmt.Errorf("invalid Stripe top-up amount")
	}
	return unitAmount.IntPart(), nil
}

func expireStripeCheckoutSessionOnLocalOrderError(ctx context.Context, checkoutSession *stripe.CheckoutSession, tradeNo string, localErr error) error {
	if localErr == nil {
		return nil
	}
	if checkoutSession == nil || checkoutSession.ID == "" {
		return localErr
	}
	if _, expireErr := stripeSessionExpire(checkoutSession.ID, nil); expireErr != nil {
		logger.LogWarn(ctx, fmt.Sprintf("Stripe Checkout Session 补偿过期失败 trade_no=%s session_id=%s error=%q", tradeNo, checkoutSession.ID, expireErr.Error()))
		recordErr := model.RecordPaymentOrphanEvent(&model.PaymentOrphanEvent{
			Provider:    model.PaymentProviderStripe,
			EventID:     "stripe_checkout_session_create:" + checkoutSession.ID,
			EventType:   "checkout.session.create",
			ReferenceID: tradeNo,
			SessionID:   checkoutSession.ID,
			Status:      model.PaymentOrphanStatusPendingReview,
			Reason:      "local order insert failed: " + localErr.Error(),
			Error:       expireErr.Error(),
			Payload: common.GetJsonString(map[string]any{
				"checkout_session_id": checkoutSession.ID,
				"trade_no":            tradeNo,
			}),
		})
		if recordErr != nil {
			logger.LogError(ctx, fmt.Sprintf("Stripe Checkout Session compensation record failed trade_no=%s session_id=%s error=%q", tradeNo, checkoutSession.ID, recordErr.Error()))
			return fmt.Errorf("%w; stripe compensation record failed: %v", localErr, recordErr)
		}
	}
	return localErr
}

func recordStripePaymentOrphanEvent(ctx context.Context, event stripe.Event, referenceId string, reason string, eventErr error) error {
	payload := ""
	if event.Data != nil && event.Data.Object != nil {
		payload = common.GetJsonString(event.Data.Object)
	}
	errText := ""
	if eventErr != nil {
		errText = eventErr.Error()
	}
	sessionID := event.GetObjectValue("id")
	if sessionID == "" {
		sessionID = event.GetObjectValue("session")
	}
	orphan := &model.PaymentOrphanEvent{
		Provider:    model.PaymentProviderStripe,
		EventID:     event.ID,
		EventType:   string(event.Type),
		ReferenceID: referenceId,
		SessionID:   sessionID,
		Status:      model.PaymentOrphanStatusPendingReview,
		Reason:      reason,
		Error:       errText,
		Payload:     payload,
	}
	if err := model.RecordPaymentOrphanEvent(orphan); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Stripe payment orphan event record failed trade_no=%s event_id=%s event_type=%s error=%q", referenceId, event.ID, string(event.Type), err.Error()))
		return err
	}
	return nil
}

func stripePaymentFacts(event stripe.Event) (float64, string, error) {
	amountTotal := strings.TrimSpace(event.GetObjectValue("amount_total"))
	currency := strings.ToUpper(strings.TrimSpace(event.GetObjectValue("currency")))
	paidAmount, err := model.StripeAmountFromMinorUnit(amountTotal, currency)
	if err != nil {
		return 0, "", err
	}
	return paidAmount, currency, nil
}

func stripeEventMetadataValue(event stripe.Event, key string) string {
	if event.Data == nil || event.Data.Object == nil {
		return ""
	}
	raw, ok := event.Data.Object["metadata"]
	if !ok || raw == nil {
		return ""
	}
	metadata, ok := raw.(map[string]interface{})
	if !ok {
		return ""
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func stripeEventMetadataBool(event stripe.Event, key string) bool {
	value := stripeEventMetadataValue(event, key)
	enabled, err := strconv.ParseBool(value)
	return err == nil && enabled
}

func stripeEventHasDiscount(event stripe.Event) bool {
	if event.Data == nil || event.Data.Object == nil {
		return false
	}
	raw, ok := event.Data.Object["total_details"]
	if !ok || raw == nil {
		return false
	}
	totalDetails, ok := raw.(map[string]interface{})
	if !ok {
		return false
	}
	amountDiscount, ok := totalDetails["amount_discount"]
	if !ok || amountDiscount == nil {
		return false
	}
	discount, err := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(amountDiscount)), 64)
	return err == nil && discount > 0
}

func stripeNoCostTopUpPaymentConfirmed(event stripe.Event, referenceId string) bool {
	if strings.HasPrefix(referenceId, "sub_ref_") ||
		!strings.EqualFold(event.GetObjectValue("mode"), string(stripe.CheckoutSessionModePayment)) ||
		event.GetObjectValue("payment_status") != "no_payment_required" ||
		strings.TrimSpace(event.GetObjectValue("amount_total")) != "0" {
		return false
	}
	return stripeEventMetadataBool(event, "promotion_codes_enabled") && stripeEventHasDiscount(event)
}

func GetChargedAmount(count float64, user model.User) float64 {
	topUpGroupRatio := common.GetTopupGroupRatio(user.Group)
	if topUpGroupRatio == 0 {
		topUpGroupRatio = 1
	}

	return count * topUpGroupRatio
}

func getStripePayMoney(amount float64, group string) float64 {
	originalAmount := amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		amount = amount / common.QuotaPerUnit
	}
	// Using float64 for monetary calculations is acceptable here due to the small amounts involved
	topupGroupRatio := common.GetTopupGroupRatio(group)
	if topupGroupRatio == 0 {
		topupGroupRatio = 1
	}
	// apply optional preset discount by the original request amount (if configured), default 1.0
	discount := 1.0
	if ds, ok := operation_setting.GetPaymentSetting().AmountDiscount[int(originalAmount)]; ok {
		if ds > 0 {
			discount = ds
		}
	}
	payMoney := amount * setting.StripeUnitPrice * topupGroupRatio * discount
	return payMoney
}

func getStripeMinTopup() int64 {
	minTopup := setting.StripeMinTopUp
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		minTopup = minTopup * int(common.QuotaPerUnit)
	}
	return int64(minTopup)
}
