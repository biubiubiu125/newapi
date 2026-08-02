package controller

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v81"
	"gorm.io/gorm"
)

func TestStripeAsyncPaymentFailedMarksSubscriptionOrderFailed(t *testing.T) {
	setupPaymentCallbackGuardDB(t)

	user := &model.User{
		Id:       6101,
		Username: "stripe_async_subscription_user",
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, model.DB.Create(user).Error)
	plan := &model.SubscriptionPlan{
		Id:            6102,
		Title:         "Stripe Async Plan",
		PriceAmount:   9.99,
		Currency:      "USD",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	order := &model.SubscriptionOrder{
		UserId:          user.Id,
		PlanId:          plan.Id,
		Money:           plan.PriceAmount,
		PaidAmount:      plan.PriceAmount,
		PaidCurrency:    "USD",
		TradeNo:         "stripe-async-failed-subscription",
		PaymentMethod:   model.PaymentMethodStripe,
		PaymentProvider: model.PaymentProviderStripe,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, order.Insert())

	sessionAsyncPaymentFailed(context.Background(), stripe.Event{
		Data: &stripe.EventData{
			Object: map[string]interface{}{
				"client_reference_id": order.TradeNo,
			},
		},
	}, "127.0.0.1")

	reloaded := model.GetSubscriptionOrderByTradeNo(order.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusFailed, reloaded.Status)
	require.Greater(t, reloaded.CompleteTime, int64(0))
}

func TestStripeAsyncPaymentFailedWebhookReturnsServerErrorWhenOrderStateCannotBePersisted(t *testing.T) {
	setupPaymentCallbackGuardDB(t)
	setupStripeWebhookTestSettings(t)

	user := &model.User{
		Id:       6111,
		Username: "stripe_async_failed_db_user",
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, model.DB.Create(user).Error)
	plan := &model.SubscriptionPlan{
		Id:            6112,
		Title:         "Stripe Async Failed DB Plan",
		PriceAmount:   9.99,
		Currency:      "USD",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	order := &model.SubscriptionOrder{
		UserId:          user.Id,
		PlanId:          plan.Id,
		Money:           plan.PriceAmount,
		PaidAmount:      plan.PriceAmount,
		PaidCurrency:    "USD",
		TradeNo:         "stripe-async-failed-db-error",
		PaymentMethod:   model.PaymentMethodStripe,
		PaymentProvider: model.PaymentProviderStripe,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, order.Insert())
	sqlDB, err := model.DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	response := performStripeWebhookRequest(stripeCheckoutSessionEventBody("evt_async_failed_db", "checkout.session.async_payment_failed", order.TradeNo, ""))

	require.Equal(t, http.StatusInternalServerError, response.Code)
}

func TestStripeAsyncPaymentFailedWebhookReturnsServerErrorWhenTopUpLookupFails(t *testing.T) {
	setupPaymentCallbackGuardDB(t)
	setupStripeWebhookTestSettings(t)

	user := &model.User{
		Id:       6121,
		Username: "stripe_async_failed_topup_db_user",
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          2,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "USD",
		TradeNo:         "stripe-async-failed-topup-db-error",
		PaymentMethod:   model.PaymentMethodStripe,
		PaymentProvider: model.PaymentProviderStripe,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())

	closeDBAfterSubscriptionOrderLookup(t)

	response := performStripeWebhookRequest(stripeCheckoutSessionEventBody("evt_async_failed_topup_db", "checkout.session.async_payment_failed", topUp.TradeNo, ""))

	require.Equal(t, http.StatusInternalServerError, response.Code)
}

func TestStripeCompletedWebhookAcksWhenNoLocalOrderExists(t *testing.T) {
	setupPaymentCallbackGuardDB(t)
	setupStripeWebhookTestSettings(t)

	response := performStripeWebhookRequest(stripeCheckoutSessionEventBody("evt_completed_missing_order", "checkout.session.completed", "stripe-completed-missing-order", `"status":"complete","payment_status":"paid","customer":"cus_missing","amount_total":"999","currency":"usd"`))

	require.Equal(t, http.StatusOK, response.Code)
}

func TestStripeWebhookAcksPermanentSubscriptionProviderMismatch(t *testing.T) {
	tests := []struct {
		name        string
		eventType   string
		tradeNo     string
		extraFields string
	}{
		{
			name:        "completed subscription provider mismatch",
			eventType:   "checkout.session.completed",
			tradeNo:     "stripe-completed-subscription-provider-mismatch",
			extraFields: `"status":"complete","payment_status":"paid","customer":"cus_subscription_mismatch","amount_total":"999","currency":"usd"`,
		},
		{
			name:        "expired subscription provider mismatch",
			eventType:   "checkout.session.expired",
			tradeNo:     "stripe-expired-subscription-provider-mismatch",
			extraFields: `"status":"expired"`,
		},
		{
			name:      "async failed subscription provider mismatch",
			eventType: "checkout.session.async_payment_failed",
			tradeNo:   "stripe-async-failed-subscription-provider-mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupPaymentCallbackGuardDB(t)
			setupStripeWebhookTestSettings(t)

			user := &model.User{
				Id:       6141,
				Username: "stripe_subscription_mismatch_user",
				Status:   common.UserStatusEnabled,
			}
			require.NoError(t, model.DB.Create(user).Error)
			plan := &model.SubscriptionPlan{
				Id:            6142,
				Title:         "Stripe Subscription Mismatch Plan",
				PriceAmount:   9.99,
				Currency:      "USD",
				DurationUnit:  model.SubscriptionDurationMonth,
				DurationValue: 1,
				Enabled:       true,
				TotalAmount:   1000,
			}
			require.NoError(t, model.DB.Create(plan).Error)
			order := &model.SubscriptionOrder{
				UserId:          user.Id,
				PlanId:          plan.Id,
				Money:           plan.PriceAmount,
				PaidAmount:      plan.PriceAmount,
				PaidCurrency:    "USD",
				TradeNo:         tt.tradeNo,
				PaymentMethod:   model.PaymentMethodStripe,
				PaymentProvider: model.PaymentProviderEpay,
				Status:          common.TopUpStatusPending,
				CreateTime:      time.Now().Unix(),
			}
			require.NoError(t, order.Insert())

			response := performStripeWebhookRequest(stripeCheckoutSessionEventBody("evt_"+tt.tradeNo, tt.eventType, order.TradeNo, tt.extraFields))

			require.Equal(t, http.StatusOK, response.Code)
			reloaded := model.GetSubscriptionOrderByTradeNo(order.TradeNo)
			require.NotNil(t, reloaded)
			require.Equal(t, common.TopUpStatusPending, reloaded.Status)
			require.Equal(t, int64(0), reloaded.CompleteTime)
		})
	}
}

func TestStripeSuccessWebhookAcksPermanentTopUpStates(t *testing.T) {
	tests := []struct {
		name            string
		eventType       string
		tradeNo         string
		paymentProvider string
		status          string
		extraFields     string
	}{
		{
			name:            "completed already successful topup",
			eventType:       "checkout.session.completed",
			tradeNo:         "stripe-completed-success-topup",
			paymentProvider: model.PaymentProviderStripe,
			status:          common.TopUpStatusSuccess,
			extraFields:     `"status":"complete","payment_status":"paid","customer":"cus_success","amount_total":"999","currency":"usd"`,
		},
		{
			name:            "async succeeded provider mismatch",
			eventType:       "checkout.session.async_payment_succeeded",
			tradeNo:         "stripe-async-succeeded-provider-mismatch",
			paymentProvider: model.PaymentProviderEpay,
			status:          common.TopUpStatusPending,
			extraFields:     `"customer":"cus_mismatch","amount_total":"999","currency":"usd"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupPaymentCallbackGuardDB(t)
			setupStripeWebhookTestSettings(t)

			user := &model.User{
				Id:       6141,
				Username: "stripe_success_permanent_user",
				Status:   common.UserStatusEnabled,
			}
			require.NoError(t, model.DB.Create(user).Error)
			topUp := &model.TopUp{
				UserId:          user.Id,
				Amount:          2,
				Money:           9.99,
				PaidAmount:      9.99,
				PaidCurrency:    "USD",
				TradeNo:         tt.tradeNo,
				PaymentMethod:   model.PaymentMethodStripe,
				PaymentProvider: tt.paymentProvider,
				Status:          tt.status,
				CreateTime:      time.Now().Unix(),
				CompleteTime:    time.Now().Unix(),
			}
			require.NoError(t, topUp.Insert())

			response := performStripeWebhookRequest(stripeCheckoutSessionEventBody("evt_"+tt.tradeNo, tt.eventType, topUp.TradeNo, tt.extraFields))

			require.Equal(t, http.StatusOK, response.Code)
		})
	}
}

func TestStripeExpiredWebhookAcksAlreadyTerminalTopUpOrder(t *testing.T) {
	setupPaymentCallbackGuardDB(t)
	setupStripeWebhookTestSettings(t)

	user := &model.User{
		Id:       6131,
		Username: "stripe_expired_terminal_user",
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          2,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "USD",
		TradeNo:         "stripe-expired-terminal-topup",
		PaymentMethod:   model.PaymentMethodStripe,
		PaymentProvider: model.PaymentProviderStripe,
		Status:          common.TopUpStatusExpired,
		CreateTime:      time.Now().Unix(),
		CompleteTime:    time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())

	response := performStripeWebhookRequest(stripeCheckoutSessionEventBody("evt_expired_terminal_topup", "checkout.session.expired", topUp.TradeNo, `"status":"expired"`))

	require.Equal(t, http.StatusOK, response.Code)
}

func stripeTestSignatureHeader(payload []byte, secret string) string {
	timestamp := time.Now().Unix()
	signedPayload := fmt.Sprintf("%d.%s", timestamp, payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signedPayload))
	return fmt.Sprintf("t=%d,v1=%s", timestamp, hex.EncodeToString(mac.Sum(nil)))
}

func setupStripeWebhookTestSettings(t *testing.T) {
	t.Helper()
	previousAPISecret := setting.StripeApiSecret
	previousWebhookSecret := setting.StripeWebhookSecret
	previousPriceID := setting.StripePriceId
	setting.StripeApiSecret = "sk_test_async_failed"
	setting.StripeWebhookSecret = "whsec_async_failed"
	setting.StripePriceId = "price_async_failed"
	t.Cleanup(func() {
		setting.StripeApiSecret = previousAPISecret
		setting.StripeWebhookSecret = previousWebhookSecret
		setting.StripePriceId = previousPriceID
	})
}

func stripeCheckoutSessionEventBody(eventID string, eventType string, referenceID string, extraObjectFields string) string {
	if extraObjectFields != "" {
		extraObjectFields = "," + extraObjectFields
	}
	return fmt.Sprintf(`{"id":%q,"object":"event","type":%q,"data":{"object":{"object":"checkout.session","client_reference_id":%q%s}}}`, eventID, eventType, referenceID, extraObjectFields)
}

func performStripeWebhookRequest(body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/stripe/webhook", strings.NewReader(body))
	request.Header.Set("Stripe-Signature", stripeTestSignatureHeader([]byte(body), setting.StripeWebhookSecret))
	response := httptest.NewRecorder()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(response)
	c.Request = request

	StripeWebhook(c)

	return response
}

func closeDBAfterSubscriptionOrderLookup(t *testing.T) {
	t.Helper()
	closed := false
	require.NoError(t, model.DB.Callback().Query().After("gorm:query").Register("stripe_close_after_subscription_order_lookup", func(db *gorm.DB) {
		if closed || db.Statement == nil {
			return
		}
		table := db.Statement.Table
		if table == "" && db.Statement.Schema != nil {
			table = db.Statement.Schema.Table
		}
		if table != "subscription_orders" {
			return
		}
		closed = true
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	}))
}
