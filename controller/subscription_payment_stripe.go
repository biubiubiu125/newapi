package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stripe/stripe-go/v81"
	"github.com/thanhpk/randstr"
)

type SubscriptionStripePayRequest struct {
	PlanId int `json:"plan_id"`
}

func SubscriptionRequestStripePay(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}

	var req SubscriptionStripePayRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
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
	if plan.StripePriceId == "" {
		common.ApiErrorMsg(c, "该套餐未配置 StripePriceId")
		return
	}
	if !strings.HasPrefix(setting.StripeApiSecret, "sk_") && !strings.HasPrefix(setting.StripeApiSecret, "rk_") {
		common.ApiErrorMsg(c, "Stripe 未配置或密钥无效")
		return
	}
	if setting.StripeWebhookSecret == "" {
		common.ApiErrorMsg(c, "Stripe Webhook 未配置")
		return
	}

	userId := c.GetInt("id")
	user, err := model.GetUserById(userId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if user == nil {
		common.ApiErrorMsg(c, "用户不存在")
		return
	}

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

	reference := fmt.Sprintf("sub-stripe-ref-%d-%d-%s", user.Id, time.Now().UnixMilli(), randstr.String(4))
	referenceId := "sub_ref_" + common.Sha1([]byte(reference))
	paidCurrency := strings.ToUpper(strings.TrimSpace(plan.Currency))
	if paidCurrency == "" {
		paidCurrency = "USD"
	}
	snapshot, _ := referralService.BuildOrderSnapshot(userId, plan.PriceAmount, paidCurrency)

	order := &model.SubscriptionOrder{
		UserId:          userId,
		PlanId:          plan.Id,
		Money:           plan.PriceAmount,
		PaidAmount:      plan.PriceAmount,
		PaidCurrency:    paidCurrency,
		TradeNo:         referenceId,
		PaymentMethod:   model.PaymentMethodStripe,
		PaymentProvider: model.PaymentProviderStripe,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
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

	checkoutSession, err := genStripeSubscriptionLink(referenceId, user.StripeCustomer, user.Email, plan.StripePriceId, stripeSubscriptionCheckoutMetadata(order))
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Stripe 订阅支付链接创建失败 trade_no=%s plan_id=%d error=%q", referenceId, plan.Id, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}
	if err := persistSubscriptionOrderAfterStripeCheckout(c.Request.Context(), order, checkoutSession); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Stripe 创建订阅订单失败 trade_no=%s plan_id=%d error=%q", referenceId, plan.Id, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"pay_link": checkoutSession.URL,
			"order_id": referenceId,
			"trade_no": referenceId,
		},
	})
}

func genStripeSubscriptionLink(referenceId string, customerId string, email string, priceId string, metadata map[string]string) (*stripe.CheckoutSession, error) {
	stripe.Key = setting.StripeApiSecret

	params := &stripe.CheckoutSessionParams{
		ClientReferenceID: stripe.String(referenceId),
		SuccessURL:        stripe.String(paymentReturnPath("/wallet")),
		CancelURL:         stripe.String(paymentReturnPath("/wallet")),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(priceId),
				Quantity: stripe.Int64(1),
			},
		},
		Mode: stripe.String(string(stripe.CheckoutSessionModeSubscription)),
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

func persistSubscriptionOrderAfterStripeCheckout(ctx context.Context, order *model.SubscriptionOrder, checkoutSession *stripe.CheckoutSession) error {
	if order == nil {
		return errors.New("subscription order is required")
	}
	return expireStripeCheckoutSessionOnLocalOrderError(ctx, checkoutSession, order.TradeNo, order.Insert())
}

func stripeSubscriptionCheckoutMetadata(order *model.SubscriptionOrder) map[string]string {
	metadata := make(map[string]string)
	if order == nil {
		return metadata
	}
	addStripeCheckoutMetadata(metadata, "user_id", strconv.Itoa(order.UserId))
	addStripeCheckoutMetadata(metadata, "plan_id", strconv.Itoa(order.PlanId))
	addStripeCheckoutMetadata(metadata, "paid_amount", strconv.FormatFloat(order.PaidAmount, 'f', -1, 64))
	addStripeCheckoutMetadata(metadata, "paid_currency", order.PaidCurrency)
	addStripeCheckoutMetadata(metadata, "plan_title_snapshot", order.PlanTitleSnapshot)
	addStripeCheckoutMetadata(metadata, "plan_price_snapshot", strconv.FormatFloat(order.PlanPriceSnapshot, 'f', -1, 64))
	addStripeCheckoutMetadata(metadata, "plan_currency_snapshot", order.PlanCurrencySnapshot)
	addStripeCheckoutMetadata(metadata, "plan_duration_unit_snapshot", order.PlanDurationUnitSnapshot)
	addStripeCheckoutMetadata(metadata, "plan_duration_value_snapshot", strconv.Itoa(order.PlanDurationValueSnapshot))
	addStripeCheckoutMetadata(metadata, "plan_custom_seconds_snapshot", strconv.FormatInt(order.PlanCustomSecondsSnapshot, 10))
	addStripeCheckoutMetadata(metadata, "plan_total_amount_snapshot", strconv.FormatInt(order.PlanTotalAmountSnapshot, 10))
	addStripeCheckoutMetadata(metadata, "plan_quota_reset_period_snapshot", order.PlanQuotaResetPeriodSnapshot)
	addStripeCheckoutMetadata(metadata, "plan_quota_reset_custom_seconds_snapshot", strconv.FormatInt(order.PlanQuotaResetCustomSecondsSnapshot, 10))
	addStripeCheckoutMetadata(metadata, "plan_upgrade_group_snapshot", order.PlanUpgradeGroupSnapshot)
	addStripeCheckoutMetadata(metadata, "plan_grant_groups_snapshot", order.PlanGrantGroupsSnapshot)
	addStripeCheckoutMetadata(metadata, "plan_downgrade_group_snapshot", order.PlanDowngradeGroupSnapshot)
	if order.PlanAllowBalancePaySnapshot != nil {
		addStripeCheckoutMetadata(metadata, "plan_allow_balance_pay_snapshot", strconv.FormatBool(*order.PlanAllowBalancePaySnapshot))
	}
	if order.PlanAllowWalletOverflowSnapshot != nil {
		addStripeCheckoutMetadata(metadata, "plan_allow_wallet_overflow_snapshot", strconv.FormatBool(*order.PlanAllowWalletOverflowSnapshot))
	}
	addStripeCheckoutMetadata(metadata, "usd_exchange_rate_snapshot", strconv.FormatFloat(order.USDExchangeRateSnapshot, 'f', -1, 64))
	addStripeCheckoutMetadata(metadata, "custom_exchange_rate_snapshot", strconv.FormatFloat(order.CustomExchangeRateSnapshot, 'f', -1, 64))
	addStripeCheckoutMetadata(metadata, "quota_display_type_snapshot", order.QuotaDisplayTypeSnapshot)
	addStripeCheckoutMetadata(metadata, "display_currency_snapshot", order.DisplayCurrencySnapshot)
	addStripeCheckoutMetadata(metadata, "referral_affiliate_id", strconv.Itoa(order.ReferralAffiliateId))
	addStripeCheckoutMetadata(metadata, "referral_rate", strconv.FormatFloat(order.ReferralRate, 'f', -1, 64))
	addStripeCheckoutMetadata(metadata, "referral_base_amount", strconv.FormatFloat(order.ReferralBaseAmount, 'f', -1, 64))
	addStripeCheckoutMetadata(metadata, "referral_base_currency", order.ReferralBaseCurrency)
	addStripeCheckoutMetadata(metadata, "referral_commission_status", order.ReferralCommissionStatus)
	addStripeCheckoutMetadata(metadata, "referral_commission_error", order.ReferralCommissionError)
	return metadata
}
