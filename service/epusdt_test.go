package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
)

func withEpusdtSettings(t *testing.T, baseURL string) {
	t.Helper()
	previousEnabled := setting.EpusdtEnabled
	previousBaseURL := setting.EpusdtBaseURL
	previousPID := setting.EpusdtPID
	previousSecretKey := setting.EpusdtSecretKey
	previousCurrency := setting.EpusdtCurrency
	setting.EpusdtEnabled = true
	setting.EpusdtBaseURL = baseURL
	setting.EpusdtPID = "1000"
	setting.EpusdtSecretKey = "secret"
	setting.EpusdtCurrency = "cny"
	t.Cleanup(func() {
		setting.EpusdtEnabled = previousEnabled
		setting.EpusdtBaseURL = previousBaseURL
		setting.EpusdtPID = previousPID
		setting.EpusdtSecretKey = previousSecretKey
		setting.EpusdtCurrency = previousCurrency
	})
}

func TestEpusdtSignMatchesGMwalletRule(t *testing.T) {
	values := map[string]interface{}{
		"pid":        "1000",
		"order_id":   "ORD20260416001",
		"currency":   "cny",
		"token":      "usdt",
		"network":    "tron",
		"amount":     "100",
		"notify_url": "https://example.com/notify",
	}

	got := EpusdtSign(values, "secret")
	require.Equal(t, "2bc0709369e36e990c6bde5ada816323", got)
}

func TestGetEpusdtAssetsParsesGMwalletConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/payments/gmpay/v1/config", r.URL.Path)
		require.NotEmpty(t, r.Header.Get("User-Agent"))
		require.Equal(t, "application/json", r.Header.Get("Accept"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"supported_assets":[{"network":"tron","tokens":["USDT","TRX"]},{"network":"polygon","tokens":["USDT"]}]}}`))
	}))
	defer server.Close()
	withEpusdtSettings(t, server.URL)

	assets, err := GetEpusdtAssets()
	require.NoError(t, err)
	require.Len(t, assets, 3)
	require.Equal(t, "epusdt:usdt:tron", assets[0].PaymentType)
	require.Equal(t, "epusdt:trx:tron", assets[1].PaymentType)
	require.Equal(t, "epusdt:usdt:polygon", assets[2].PaymentType)
}

func TestGetEpusdtAssetsParsesGMwalletSupportedAssets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/payments/gmpay/v1/config" {
			http.NotFound(w, r)
			return
		}
		require.Equal(t, "/payments/gmpay/v1/supported-assets", r.URL.Path)
		require.NotEmpty(t, r.Header.Get("User-Agent"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":[{"network":"tron","tokens":["USDT"]},{"network":"bsc","tokens":["USDT"]}]}`))
	}))
	defer server.Close()
	withEpusdtSettings(t, server.URL)

	assets, err := GetEpusdtAssets()
	require.NoError(t, err)
	require.Len(t, assets, 2)
	require.Equal(t, "epusdt:usdt:tron", assets[0].PaymentType)
	require.Equal(t, "epusdt:usdt:bsc", assets[1].PaymentType)
}

func TestGetEpusdtAssetsFallsBackToUsdtWhenConfigUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	}))
	defer server.Close()
	withEpusdtSettings(t, server.URL)

	assets, err := GetEpusdtAssets()
	require.NoError(t, err)
	require.Len(t, assets, 3)
	require.Equal(t, "epusdt:usdt:tron", assets[0].PaymentType)
	require.Equal(t, "epusdt:usdt:bsc", assets[1].PaymentType)
	require.Equal(t, "epusdt:usdt:polygon", assets[2].PaymentType)
	require.True(t, IsValidEpusdtPaymentMethod("epusdt:usdt"))
	require.True(t, IsValidEpusdtPaymentMethod("epusdt:usdt:tron"))
}

func TestEpusdtAssetsForTopupMethodsCollapsesNetworksToUsdt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"supported_assets":[{"network":"tron","tokens":["USDT","TRX"]},{"network":"bsc","tokens":["USDT"]},{"network":"polygon","tokens":["USDT"]}]}}`))
	}))
	defer server.Close()
	withEpusdtSettings(t, server.URL)

	methods := EpusdtAssetsForTopupMethods()
	require.Len(t, methods, 1)
	require.Equal(t, "USDT", methods[0]["name"])
	require.Equal(t, "epusdt:usdt", methods[0]["type"])
	require.Equal(t, "epusdt", methods[0]["provider"])
}

func TestCreateEpusdtOrderParsesGMwalletResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/payments/gmpay/v1/order/create-transaction", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.Equal(t, "application/json", r.Header.Get("Accept"))
		require.NotEmpty(t, r.Header.Get("User-Agent"))
		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "1000", body["pid"])
		require.Equal(t, "order-1", body["order_id"])
		require.Equal(t, 0.01, body["amount"])
		require.True(t, VerifyEpusdtSignature(body))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"trade_id":"T202605180001","order_id":"order-1","amount":0.01,"currency":"CNY","actual_amount":0.0014,"receive_address":"TTestAddress","token":"USDT","expiration_time":1779039999,"payment_url":"https://pay.example.com/pay/checkout-counter/T202605180001"}}`))
	}))
	defer server.Close()
	withEpusdtSettings(t, server.URL)

	order, err := CreateEpusdtOrder(EpusdtCreateOrderRequest{
		OrderID:   "order-1",
		Amount:    0.01,
		Currency:  "CNY",
		Token:     "usdt",
		Network:   "tron",
		NotifyURL: "https://merchant.example.com/notify",
	})
	require.NoError(t, err)
	require.Equal(t, "order-1", order.OrderID)
	require.Equal(t, "T202605180001", order.TransactionID)
	require.Equal(t, "TTestAddress", order.PaymentAddress)
	require.Equal(t, "0.0014", order.PaymentAmount)
	require.Equal(t, "USDT", order.PaymentCurrency)
	require.True(t, strings.Contains(order.PaymentURL, "T202605180001"))
}

func TestGMwalletNotifyHelpers(t *testing.T) {
	values := map[string]interface{}{
		"pid":                  "1000",
		"trade_id":             "T202605180001",
		"order_id":             "merchant-order-1",
		"amount":               100.0,
		"actual_amount":        14.2857,
		"receive_address":      "TTestAddress",
		"token":                "USDT",
		"block_transaction_id": "0xtx",
		"status":               2,
	}
	values["signature"] = EpusdtSign(values, "secret")

	withEpusdtSettings(t, "https://pay.example.com")
	require.True(t, VerifyEpusdtSignature(values))
	require.Equal(t, "merchant-order-1", EpusdtCallbackTradeNo(values))
	require.Equal(t, "2", EpusdtCallbackStatus(values))
	require.True(t, IsEpusdtPaidStatus(EpusdtCallbackStatus(values)))
	require.Equal(t, "usdt", EpusdtCallbackToken(values))
	require.Equal(t, 100.0, EpusdtCallbackPaidAmount(values))
	require.Empty(t, EpusdtCallbackPaidCurrency(values))
	require.Equal(t, "1000", EpusdtCallbackMerchantID(values))
}
