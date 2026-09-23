package controller

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

type SubscriptionEpayPayRequest struct {
	PlanId        int    `json:"plan_id"`
	PaymentMethod string `json:"payment_method"`
}

func SubscriptionRequestEpay(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}

	var req SubscriptionEpayPayRequest
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
	if !operation_setting.ContainsPayMethod(req.PaymentMethod) {
		common.ApiErrorI18n(c, i18n.MsgPaymentMethodNotExists)
		return
	}

	userId := c.GetInt("id")
	paidAmount, err := normalizeSubscriptionPaymentAmount(plan, "CNY")
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionAmountInvalid)
		return
	}
	snapshot, _ := referralService.BuildOrderSnapshot(userId, paidAmount, "CNY")
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

	callBackAddress, err := service.RequirePublicCallbackAddress()
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgTopupCallbackMissing)
		return
	}
	returnUrl, err := url.Parse(callBackAddress + "/api/subscription/epay/return")
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgPaymentCallbackError)
		return
	}
	notifyUrl, err := url.Parse(callBackAddress + "/api/subscription/epay/notify")
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgPaymentCallbackError)
		return
	}

	tradeNo := fmt.Sprintf("%s%d", common.GetRandomString(6), time.Now().Unix())
	tradeNo = fmt.Sprintf("SUBUSR%dNO%s", userId, tradeNo)

	client := GetEpayClient()
	if client == nil {
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}

	order := &model.SubscriptionOrder{
		UserId:          userId,
		PlanId:          plan.Id,
		Money:           paidAmount,
		PaidAmount:      paidAmount,
		PaidCurrency:    "CNY",
		TradeNo:         tradeNo,
		PaymentMethod:   req.PaymentMethod,
		PaymentProvider: model.PaymentProviderEpay,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	applySubscriptionOrderSnapshot(order, plan, "CNY")
	if snapshot != nil {
		order.ReferralAffiliateId = snapshot.AffiliateId
		order.ReferralRate = snapshot.Rate
		order.ReferralBaseAmount = snapshot.BaseAmount
		order.ReferralBaseCurrency = snapshot.Currency
		order.ReferralCommissionStatus = snapshot.Status
		order.ReferralCommissionError = snapshot.Error
	}
	if err := order.Insert(); err != nil {
		common.ApiErrorI18n(c, i18n.MsgPaymentCreateFailed)
		return
	}
	uri, params, err := client.Purchase(&epay.PurchaseArgs{
		Type:           req.PaymentMethod,
		ServiceTradeNo: tradeNo,
		Name:           fmt.Sprintf("SUB:%s", plan.Title),
		Money:          strconv.FormatFloat(paidAmount, 'f', 2, 64),
		Device:         epay.PC,
		NotifyUrl:      notifyUrl,
		ReturnUrl:      returnUrl,
	})
	if err != nil {
		_ = model.ExpireSubscriptionOrder(tradeNo, model.PaymentProviderEpay)
		common.ApiErrorI18n(c, i18n.MsgPaymentStartFailed)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": params, "url": uri, "order_id": tradeNo, "trade_no": tradeNo})
}

func SubscriptionEpayNotify(c *gin.Context) {
	if !isEpayWebhookEnabled() {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	var params map[string]string

	if c.Request.Method == "POST" {
		// POST 请求：从 POST body 解析参数
		if err := c.Request.ParseForm(); err != nil {
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

	if len(params) == 0 {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	client := GetEpayClient()
	if client == nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	verifyInfo, err := client.Verify(params)
	if err != nil || !verifyInfo.VerifyStatus {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	if !epayCallbackMerchantMatches(params) {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	if verifyInfo.TradeStatus != epay.StatusTradeSuccess {
		_, _ = c.Writer.Write([]byte("success"))
		return
	}

	LockOrder(verifyInfo.ServiceTradeNo)
	defer UnlockOrder(verifyInfo.ServiceTradeNo)

	if err := model.CompleteSubscriptionOrderWithValidation(verifyInfo.ServiceTradeNo, common.GetJsonString(verifyInfo), model.PaymentCallbackValidation{
		ExpectedPaymentProvider: model.PaymentProviderEpay,
		ActualPaymentMethod:     verifyInfo.Type,
		PaidAmount:              parseCallbackAmount(verifyInfo.Money),
		PaidCurrency:            "CNY",
		RequirePaymentFacts:     true,
		CallerIP:                common.GetClientIP(c),
	}); err != nil {
		if isPermanentPaymentReviewError(err) {
			if recordErr := recordPaymentReview(c.Request.Context(), model.PaymentProviderEpay, "", "subscription.notify", verifyInfo.ServiceTradeNo, "", "Epay subscription payment requires manual review after payment succeeded", err, common.GetJsonString(verifyInfo)); recordErr != nil {
				c.String(http.StatusInternalServerError, "fail")
				return
			}
			_, _ = c.Writer.Write([]byte("success"))
			return
		}
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	if err := processPaidSubscriptionCommission(c.Request.Context(), verifyInfo.ServiceTradeNo); err != nil {
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	_, _ = c.Writer.Write([]byte("success"))
}

// SubscriptionEpayReturn handles browser return after payment.
// Browser return is not trusted for fulfillment; only notify_url can complete orders.
func SubscriptionEpayReturn(c *gin.Context) {
	var params map[string]string

	if c.Request.Method == "POST" {
		// POST 请求：从 POST body 解析参数
		if err := c.Request.ParseForm(); err != nil {
			c.Redirect(http.StatusFound, paymentWalletReturnPath("fail", model.PaymentProviderEpay, "subscription", ""))
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

	if len(params) == 0 {
		c.Redirect(http.StatusFound, paymentWalletReturnPath("fail", model.PaymentProviderEpay, "subscription", ""))
		return
	}

	client := GetEpayClient()
	if client == nil {
		c.Redirect(http.StatusFound, paymentWalletReturnPath("fail", model.PaymentProviderEpay, "subscription", params["out_trade_no"]))
		return
	}
	verifyInfo, err := client.Verify(params)
	if err != nil || !verifyInfo.VerifyStatus {
		c.Redirect(http.StatusFound, paymentWalletReturnPath("fail", model.PaymentProviderEpay, "subscription", params["out_trade_no"]))
		return
	}
	if !epayCallbackMerchantMatches(params) {
		c.Redirect(http.StatusFound, paymentWalletReturnPath("fail", model.PaymentProviderEpay, "subscription", params["out_trade_no"]))
		return
	}
	if verifyInfo.TradeStatus == epay.StatusTradeSuccess {
		c.Redirect(http.StatusFound, paymentWalletReturnPath("success", model.PaymentProviderEpay, "subscription", verifyInfo.ServiceTradeNo))
		return
	}
	c.Redirect(http.StatusFound, paymentWalletReturnPath("pending", model.PaymentProviderEpay, "subscription", verifyInfo.ServiceTradeNo))
}
