package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
)

func withBEpusdtSettings(t *testing.T, baseURL string) {
	t.Helper()
	previousEnabled := setting.GMPayEnabled
	previousGatewayType := setting.USDTGatewayType
	previousBaseURL := setting.GMPayBaseURL
	previousPID := setting.GMPayPID
	previousSecretKey := setting.GMPaySecretKey
	previousCurrency := setting.GMPayCurrency
	setting.GMPayEnabled = true
	setting.USDTGatewayType = setting.USDTGatewayTypeBEpusdt
	setting.GMPayBaseURL = baseURL
	setting.GMPayPID = ""
	setting.GMPaySecretKey = "secret"
	setting.GMPayCurrency = "cny"
	t.Cleanup(func() {
		setting.GMPayEnabled = previousEnabled
		setting.USDTGatewayType = previousGatewayType
		setting.GMPayBaseURL = previousBaseURL
		setting.GMPayPID = previousPID
		setting.GMPaySecretKey = previousSecretKey
		setting.GMPayCurrency = previousCurrency
	})
}

func TestUSDTGatewayAssetsOnlyExposeBEpusdtUSDT(t *testing.T) {
	withBEpusdtSettings(t, "https://pay.example.com")

	assets, err := GetGMPayAssets()
	require.NoError(t, err)
	require.Equal(t, []GMPayAsset{{
		Token:       "usdt",
		PaymentType: "usdt",
		DisplayName: "USDT",
	}}, assets)

	methods := GMPayAssetsForTopupMethods()
	require.Equal(t, []map[string]string{{
		"name":      "USDT",
		"type":      "usdt",
		"color":     "rgba(var(--semi-teal-5), 1)",
		"min_topup": "1",
		"provider":  "bepusdt",
	}}, methods)
	require.True(t, IsValidGMPayPaymentMethod("usdt"))
	require.False(t, IsValidGMPayPaymentMethod("gmpay:usdt:tron"))
}

func TestBEpusdtConfigurationDoesNotRequireMerchantID(t *testing.T) {
	withBEpusdtSettings(t, "https://pay.example.com")
	setting.GMPayPID = ""

	require.True(t, IsUSDTGatewayConfigured())
	require.Equal(t, "bepusdt", ActiveUSDTGatewayProvider())
}

func TestCreateUSDTGatewayOrderUsesBEpusdtCreateOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/order/create-order", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.Equal(t, "application/json", r.Header.Get("Accept"))
		require.NotEmpty(t, r.Header.Get("User-Agent"))

		var body map[string]interface{}
		require.NoError(t, common.DecodeJson(r.Body, &body))
		require.Equal(t, "order-bepusdt-1", body["order_id"])
		require.Equal(t, 12.5, body["amount"])
		require.Equal(t, "CNY", body["fiat"])
		require.Equal(t, "USDT", body["currencies"])
		require.Equal(t, "https://merchant.example.com/notify", body["notify_url"])
		require.Equal(t, "https://merchant.example.com/wallet?show_history=true", body["redirect_url"])
		require.NotContains(t, body, "pid")
		require.NotContains(t, body, "merchant_id")
		require.NotContains(t, body, "network")
		require.NotContains(t, body, "trade_type")
		require.NotContains(t, body, "payment_type")
		require.True(t, VerifyGMPaySignature(body))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status_code":200,"message":"success","data":{"trade_id":"BEPAY-1","order_id":"order-bepusdt-1","amount":"12.5","status":1,"payment_url":"https://pay.example.com/pay/cashier/BEPAY-1"}}`))
	}))
	defer server.Close()
	withBEpusdtSettings(t, server.URL)

	order, err := CreateUSDTGatewayOrder(USDTGatewayOrderRequest{
		OrderID:     "order-bepusdt-1",
		Amount:      12.5,
		Currency:    "CNY",
		NotifyURL:   "https://merchant.example.com/notify",
		RedirectURL: "https://merchant.example.com/wallet?show_history=true",
		Name:        "Topup 10",
		PaymentType: "usdt",
	})
	require.NoError(t, err)
	require.Equal(t, "BEPAY-1", order.TransactionID)
	require.Equal(t, "https://pay.example.com/pay/cashier/BEPAY-1", order.PaymentURL)
}

func TestBEpusdtSignAndNotifyHelpers(t *testing.T) {
	values := map[string]interface{}{
		"trade_id":      "BEPAY202605220001",
		"order_id":      "merchant-order-1",
		"amount":        "100",
		"actual_amount": "14.2857",
		"token":         "TReceiveAddressFromBEpusdt",
		"fiat":          "CNY",
		"status":        2,
	}
	values["signature"] = GMPaySign(values, "secret")

	withBEpusdtSettings(t, "https://pay.example.com")
	require.True(t, VerifyGMPaySignature(values))
	values["signature"] = "invalid"
	require.False(t, VerifyGMPaySignature(values))
	values["signature"] = GMPaySign(values, "secret")
	require.Equal(t, "merchant-order-1", GMPayCallbackTradeNo(values))
	require.Equal(t, "2", GMPayCallbackStatus(values))
	require.True(t, IsGMPayPaidStatus(GMPayCallbackStatus(values)))
	require.Empty(t, GMPayCallbackToken(values))
	require.Equal(t, 100.0, GMPayCallbackPaidAmount(values))
	require.Equal(t, "CNY", GMPayCallbackPaidCurrency(values))

	values["currencies"] = "USDT"
	values["signature"] = GMPaySign(values, "secret")
	require.Equal(t, "usdt", GMPayCallbackToken(values))
}
