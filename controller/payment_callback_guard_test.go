package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupPaymentCallbackGuardDB(t *testing.T) {
	t.Helper()

	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL
	previousRedisEnabled := common.RedisEnabled
	previousServerAddress := system_setting.ServerAddress
	previousPayAddress := operation_setting.PayAddress
	previousEpayID := operation_setting.EpayId
	previousEpayKey := operation_setting.EpayKey
	previousPayMethods := operation_setting.PayMethods
	previousBEpusdtPID := setting.BEpusdtPID
	previousBEpusdtSecretKey := setting.BEpusdtSecretKey
	previousBEpusdtCurrency := setting.BEpusdtCurrency
	paymentSetting := operation_setting.GetPaymentSetting()
	previousComplianceConfirmed := paymentSetting.ComplianceConfirmed
	previousComplianceTermsVersion := paymentSetting.ComplianceTermsVersion

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	system_setting.ServerAddress = "http://newapi.test"
	operation_setting.PayAddress = "https://pay.example.com"
	operation_setting.EpayId = "pid-test"
	operation_setting.EpayKey = "key-test"
	operation_setting.PayMethods = []map[string]string{{"type": "alipay"}}
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	setting.BEpusdtPID = "bepusdt-pid-test"
	setting.BEpusdtSecretKey = "bepusdt-key-test"
	setting.BEpusdtCurrency = "cny"

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.UserLoginIdentifier{},
		&model.Token{},
		&model.Log{},
		&model.TopUp{},
		&model.SubscriptionPlan{},
		&model.SubscriptionOrder{},
		&model.UserSubscription{},
		&model.ReferralAffiliate{},
		&model.ReferralBinding{},
		&model.ReferralCommissionAccount{},
		&model.ReferralCommission{},
		&model.ReferralCommissionLedger{},
		&model.ReferralCommissionJob{},
		&model.ReferralWithdrawal{},
		&model.ReferralWithdrawalItem{},
		&model.RiskEvent{},
		&model.RiskAction{},
		&model.RiskWhitelist{},
	))

	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		common.RedisEnabled = previousRedisEnabled
		system_setting.ServerAddress = previousServerAddress
		operation_setting.PayAddress = previousPayAddress
		operation_setting.EpayId = previousEpayID
		operation_setting.EpayKey = previousEpayKey
		operation_setting.PayMethods = previousPayMethods
		paymentSetting.ComplianceConfirmed = previousComplianceConfirmed
		paymentSetting.ComplianceTermsVersion = previousComplianceTermsVersion
		setting.BEpusdtPID = previousBEpusdtPID
		setting.BEpusdtSecretKey = previousBEpusdtSecretKey
		setting.BEpusdtCurrency = previousBEpusdtCurrency
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
}

func signedEpayCallback(values map[string]string) url.Values {
	params := map[string]string{}
	for key, value := range values {
		params[key] = value
	}
	signed := epay.GenerateParams(params, operation_setting.EpayKey)
	out := url.Values{}
	for key, value := range signed {
		out.Set(key, value)
	}
	return out
}

func signedBEpusdtCallback(values map[string]interface{}) string {
	params := map[string]interface{}{}
	for key, value := range values {
		params[key] = value
	}
	params["signature"] = service.BEpusdtSign(params, setting.BEpusdtSecretKey)
	return common.GetJsonString(params)
}

func TestEpayTopupNotifyRejectsMismatchedMerchant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	user := &model.User{Id: 910, Username: "epay_merchant_guard_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          2,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "CNY",
		TradeNo:         "topup-merchant-guard",
		PaymentMethod:   "alipay",
		PaymentProvider: model.PaymentProviderEpay,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())

	params := signedEpayCallback(map[string]string{
		"pid":          "wrong-pid",
		"type":         "alipay",
		"out_trade_no": topUp.TradeNo,
		"trade_no":     "gateway-merchant-guard",
		"name":         "Topup Merchant Guard",
		"money":        "9.99",
		"trade_status": epay.StatusTradeSuccess,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/user/epay/notify", strings.NewReader(params.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	EpayNotify(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "fail", w.Body.String())
	reloaded := model.GetTopUpByTradeNo(topUp.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusPending, reloaded.Status)
	var updatedUser model.User
	require.NoError(t, model.DB.Where("id = ?", user.Id).First(&updatedUser).Error)
	require.Zero(t, updatedUser.Quota)
}

func TestEpayTopupNotifyRejectsNonSuccessStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	user := &model.User{Id: 916, Username: "epay_status_guard_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          2,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "CNY",
		TradeNo:         "topup-status-guard",
		PaymentMethod:   "alipay",
		PaymentProvider: model.PaymentProviderEpay,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())

	params := signedEpayCallback(map[string]string{
		"pid":          operation_setting.EpayId,
		"type":         "alipay",
		"out_trade_no": topUp.TradeNo,
		"trade_no":     "gateway-status-guard",
		"name":         "Topup Status Guard",
		"money":        "9.99",
		"trade_status": "TRADE_CLOSED",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/user/epay/notify", strings.NewReader(params.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	EpayNotify(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "fail", w.Body.String())
	reloaded := model.GetTopUpByTradeNo(topUp.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusPending, reloaded.Status)
	var updatedUser model.User
	require.NoError(t, model.DB.Where("id = ?", user.Id).First(&updatedUser).Error)
	require.Zero(t, updatedUser.Quota)
}

func TestBEpusdtTopupNotifyRejectsLegacyBEpusdtOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	user := &model.User{Id: 912, Username: "bepusdt_merchant_guard_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          2,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "CNY",
		TradeNo:         "bepusdt-missing-merchant-guard",
		PaymentMethod:   service.BuildBEpusdtPaymentMethod("usdt", "tron"),
		PaymentProvider: model.PaymentProviderBEpusdt,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())

	body := signedBEpusdtCallback(map[string]interface{}{
		"order_id":       topUp.TradeNo,
		"transaction_id": "bepusdt-gateway-merchant-guard",
		"amount":         "9.99",
		"order_currency": "CNY",
		"token":          "usdt",
		"network":        "tron",
		"status":         "paid",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/user/bepusdt/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	BEpusdtTopUpNotify(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, "fail", w.Body.String())
	reloaded := model.GetTopUpByTradeNo(topUp.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusPending, reloaded.Status)
	var updatedUser model.User
	require.NoError(t, model.DB.Where("id = ?", user.Id).First(&updatedUser).Error)
	require.Zero(t, updatedUser.Quota)
}

func TestBEpusdtTopupNotifyRejectsLegacyBEpusdtPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	user := &model.User{Id: 914, Username: "bepusdt_bepusdt_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          2,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "CNY",
		TradeNo:         "bepusdt-bepusdt-success",
		PaymentMethod:   service.BuildBEpusdtPaymentMethod("usdt", "tron"),
		PaymentProvider: model.PaymentProviderBEpusdt,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())

	body := signedBEpusdtCallback(map[string]interface{}{
		"pid":                  setting.BEpusdtPID,
		"trade_id":             "T202605180001",
		"order_id":             topUp.TradeNo,
		"amount":               "9.99",
		"actual_amount":        "1.4285",
		"receive_address":      "TTestAddress",
		"token":                "USDT",
		"currency":             "USDT",
		"network":              "tron",
		"block_transaction_id": "0xtesttx",
		"status":               2,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/user/bepusdt/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	BEpusdtTopUpNotify(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, "fail", w.Body.String())
	reloaded := model.GetTopUpByTradeNo(topUp.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusPending, reloaded.Status)
	var updatedUser model.User
	require.NoError(t, model.DB.Where("id = ?", user.Id).First(&updatedUser).Error)
	require.Zero(t, updatedUser.Quota)
}

func TestBEpusdtTopupNotifyAcceptsCashierCallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	user := &model.User{Id: 916, Username: "bepusdt_cashier_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          2,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "CNY",
		TradeNo:         "bepusdt-cashier-success",
		PaymentMethod:   model.PaymentMethodUSDT,
		PaymentProvider: model.PaymentProviderBEpusdt,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())

	body := signedBEpusdtCallback(map[string]interface{}{
		"trade_id":      "BEPAY202605220001",
		"order_id":      topUp.TradeNo,
		"amount":        "9.99",
		"actual_amount": "1.4285",
		"token":         "TReceiveAddressFromBEpusdt",
		"status":        2,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/user/bepusdt/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	BEpusdtTopUpNotify(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "ok", w.Body.String())
	reloaded := model.GetTopUpByTradeNo(topUp.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusSuccess, reloaded.Status)
	var updatedUser model.User
	require.NoError(t, model.DB.Where("id = ?", user.Id).First(&updatedUser).Error)
	require.Positive(t, updatedUser.Quota)
	var topupLog model.Log
	require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", user.Id, model.LogTypeTopup).First(&topupLog).Error)
	require.Contains(t, topupLog.Content, "BEpusdt USDT")
}

func TestBEpusdtTopupNotifyRejectsTokenMismatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	user := &model.User{Id: 923, Username: "bepusdt_token_guard_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          2,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "CNY",
		TradeNo:         "bepusdt-token-mismatch",
		PaymentMethod:   model.PaymentMethodUSDT,
		PaymentProvider: model.PaymentProviderBEpusdt,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())

	body := signedBEpusdtCallback(map[string]interface{}{
		"trade_id":   "BEPAY202605220003",
		"order_id":   topUp.TradeNo,
		"amount":     "9.99",
		"fiat":       "CNY",
		"currencies": "BTC",
		"status":     2,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/user/bepusdt/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	BEpusdtTopUpNotify(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, "fail", w.Body.String())
	reloaded := model.GetTopUpByTradeNo(topUp.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusPending, reloaded.Status)
	var updatedUser model.User
	require.NoError(t, model.DB.Where("id = ?", user.Id).First(&updatedUser).Error)
	require.Zero(t, updatedUser.Quota)
}

func TestBEpusdtTopupNotifyRejectsAmountMismatchWithBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	user := &model.User{Id: 925, Username: "bepusdt_amount_guard_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          2,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "CNY",
		TradeNo:         "bepusdt-amount-mismatch",
		PaymentMethod:   model.PaymentMethodUSDT,
		PaymentProvider: model.PaymentProviderBEpusdt,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())

	body := signedBEpusdtCallback(map[string]interface{}{
		"trade_id": "BEPAY202605220005",
		"order_id": topUp.TradeNo,
		"amount":   "9.98",
		"fiat":     "CNY",
		"status":   2,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/user/bepusdt/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	BEpusdtTopUpNotify(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, "fail", w.Body.String())
	reloaded := model.GetTopUpByTradeNo(topUp.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusPending, reloaded.Status)
	var updatedUser model.User
	require.NoError(t, model.DB.Where("id = ?", user.Id).First(&updatedUser).Error)
	require.Zero(t, updatedUser.Quota)
}

func TestBEpusdtTopupNotifyRejectsMissingOrderWithBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	body := signedBEpusdtCallback(map[string]interface{}{
		"trade_id": "BEPAY202605220007",
		"order_id": "bepusdt-missing-order",
		"amount":   "9.99",
		"fiat":     "CNY",
		"status":   2,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/user/bepusdt/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	BEpusdtTopUpNotify(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, "fail", w.Body.String())
}

func TestBEpusdtTopupNotifyRejectsInvalidOrderStatusWithBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	user := &model.User{Id: 927, Username: "bepusdt_status_guard_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          2,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "CNY",
		TradeNo:         "bepusdt-invalid-status",
		PaymentMethod:   model.PaymentMethodUSDT,
		PaymentProvider: model.PaymentProviderBEpusdt,
		Status:          common.TopUpStatusFailed,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())

	body := signedBEpusdtCallback(map[string]interface{}{
		"trade_id": "BEPAY202605220008",
		"order_id": topUp.TradeNo,
		"amount":   "9.99",
		"fiat":     "CNY",
		"status":   2,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/user/bepusdt/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	BEpusdtTopUpNotify(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, "fail", w.Body.String())
	reloaded := model.GetTopUpByTradeNo(topUp.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusFailed, reloaded.Status)
	var updatedUser model.User
	require.NoError(t, model.DB.Where("id = ?", user.Id).First(&updatedUser).Error)
	require.Zero(t, updatedUser.Quota)
}

func TestBEpusdtTopupNotifyAcceptsNativeCallbackEvenWithLegacyExtraFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	user := &model.User{Id: 921, Username: "bepusdt_cross_gateway_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          2,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "CNY",
		TradeNo:         "bepusdt-cross-gateway-guard",
		PaymentMethod:   model.PaymentMethodUSDT,
		PaymentProvider: model.PaymentProviderBEpusdt,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())

	body := signedBEpusdtCallback(map[string]interface{}{
		"pid":            setting.BEpusdtPID,
		"trade_id":       "T202605220002",
		"order_id":       topUp.TradeNo,
		"amount":         "9.99",
		"order_currency": "CNY",
		"token":          "USDT",
		"network":        "tron",
		"status":         2,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/user/bepusdt/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	BEpusdtTopUpNotify(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "ok", w.Body.String())
	reloaded := model.GetTopUpByTradeNo(topUp.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusSuccess, reloaded.Status)
	var updatedUser model.User
	require.NoError(t, model.DB.Where("id = ?", user.Id).First(&updatedUser).Error)
	require.Positive(t, updatedUser.Quota)
}

func TestBEpusdtTopupNotifyRejectsLegacyBEpusdtNetworkOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	user := &model.User{Id: 917, Username: "bepusdt_network_guard_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          2,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "CNY",
		TradeNo:         "bepusdt-network-guard",
		PaymentMethod:   service.BuildBEpusdtPaymentMethod("usdt", "tron"),
		PaymentProvider: model.PaymentProviderBEpusdt,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())

	body := signedBEpusdtCallback(map[string]interface{}{
		"pid":            setting.BEpusdtPID,
		"trade_id":       "T202605180003",
		"order_id":       topUp.TradeNo,
		"amount":         "9.99",
		"order_currency": "CNY",
		"token":          "USDT",
		"network":        "polygon",
		"status":         2,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/user/bepusdt/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	BEpusdtTopUpNotify(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, "fail", w.Body.String())
	reloaded := model.GetTopUpByTradeNo(topUp.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusPending, reloaded.Status)
	var updatedUser model.User
	require.NoError(t, model.DB.Where("id = ?", user.Id).First(&updatedUser).Error)
	require.Zero(t, updatedUser.Quota)
}

func TestBEpusdtTopupNotifyRejectsLegacyBEpusdtPaymentTypeOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	user := &model.User{Id: 919, Username: "bepusdt_payment_type_guard_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          2,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "CNY",
		TradeNo:         "bepusdt-payment-type-guard",
		PaymentMethod:   service.BuildBEpusdtPaymentMethod("usdt", "tron"),
		PaymentProvider: model.PaymentProviderBEpusdt,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())

	body := signedBEpusdtCallback(map[string]interface{}{
		"pid":            setting.BEpusdtPID,
		"trade_id":       "T202605190001",
		"order_id":       topUp.TradeNo,
		"amount":         "9.99",
		"order_currency": "CNY",
		"token":          "USDT",
		"network":        "tron",
		"payment_type":   "epay:alipay",
		"status":         2,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/user/bepusdt/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	BEpusdtTopUpNotify(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, "fail", w.Body.String())
	reloaded := model.GetTopUpByTradeNo(topUp.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusPending, reloaded.Status)
	var updatedUser model.User
	require.NoError(t, model.DB.Where("id = ?", user.Id).First(&updatedUser).Error)
	require.Zero(t, updatedUser.Quota)
}

func TestSubscriptionBEpusdtNotifyRejectsLegacyBEpusdtOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	user := &model.User{Id: 913, Username: "sub_bepusdt_merchant_guard_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	plan := &model.SubscriptionPlan{
		Id:            810,
		Title:         "BEpusdt Merchant Guard Plan",
		PriceAmount:   9.99,
		Currency:      "CNY",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	order := &model.SubscriptionOrder{
		UserId:          user.Id,
		PlanId:          plan.Id,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "CNY",
		TradeNo:         "sub-bepusdt-missing-merchant-guard",
		PaymentMethod:   service.BuildBEpusdtPaymentMethod("usdt", "tron"),
		PaymentProvider: model.PaymentProviderBEpusdt,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, order.Insert())

	body := signedBEpusdtCallback(map[string]interface{}{
		"order_id":       order.TradeNo,
		"transaction_id": "bepusdt-gateway-sub-merchant-guard",
		"amount":         "9.99",
		"order_currency": "CNY",
		"token":          "usdt",
		"network":        "tron",
		"status":         "paid",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/subscription/bepusdt/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	SubscriptionBEpusdtNotify(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, "fail", w.Body.String())
	reloaded := model.GetSubscriptionOrderByTradeNo(order.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusPending, reloaded.Status)
	var subscriptionCount int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", user.Id).Count(&subscriptionCount).Error)
	require.Zero(t, subscriptionCount)
}

func TestSubscriptionBEpusdtNotifyRejectsLegacyBEpusdtPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	user := &model.User{Id: 915, Username: "sub_bepusdt_bepusdt_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	plan := &model.SubscriptionPlan{
		Id:            811,
		Title:         "BEpusdt BEpusdt Plan",
		PriceAmount:   9.99,
		Currency:      "CNY",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	order := &model.SubscriptionOrder{
		UserId:          user.Id,
		PlanId:          plan.Id,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "CNY",
		TradeNo:         "sub-bepusdt-bepusdt-success",
		PaymentMethod:   service.BuildBEpusdtPaymentMethod("usdt", "tron"),
		PaymentProvider: model.PaymentProviderBEpusdt,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, order.Insert())

	body := signedBEpusdtCallback(map[string]interface{}{
		"pid":                  setting.BEpusdtPID,
		"trade_id":             "T202605180002",
		"order_id":             order.TradeNo,
		"amount":               "9.99",
		"actual_amount":        "1.4285",
		"receive_address":      "TTestAddress",
		"token":                "USDT",
		"currency":             "USDT",
		"network":              "tron",
		"block_transaction_id": "0xtesttxsub",
		"status":               2,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/subscription/bepusdt/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	SubscriptionBEpusdtNotify(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, "fail", w.Body.String())
	reloaded := model.GetSubscriptionOrderByTradeNo(order.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusPending, reloaded.Status)
	var subscriptionCount int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", user.Id).Count(&subscriptionCount).Error)
	require.Zero(t, subscriptionCount)
}

func TestSubscriptionBEpusdtNotifyAcceptsCashierCallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	user := &model.User{Id: 922, Username: "sub_bepusdt_cashier_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	plan := &model.SubscriptionPlan{
		Id:            814,
		Title:         "BEpusdt Plan",
		PriceAmount:   9.99,
		Currency:      "CNY",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	order := &model.SubscriptionOrder{
		UserId:          user.Id,
		PlanId:          plan.Id,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "CNY",
		TradeNo:         "sub-bepusdt-cashier-success",
		PaymentMethod:   model.PaymentMethodUSDT,
		PaymentProvider: model.PaymentProviderBEpusdt,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, order.Insert())

	body := signedBEpusdtCallback(map[string]interface{}{
		"trade_id":      "BEPAY202605220002",
		"order_id":      order.TradeNo,
		"amount":        "9.99",
		"actual_amount": "1.4285",
		"token":         "TReceiveAddressFromBEpusdt",
		"status":        2,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/subscription/bepusdt/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	SubscriptionBEpusdtNotify(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "ok", w.Body.String())
	reloaded := model.GetSubscriptionOrderByTradeNo(order.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusSuccess, reloaded.Status)
	var subscriptionCount int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", user.Id).Count(&subscriptionCount).Error)
	require.Equal(t, int64(1), subscriptionCount)
	var topupLog model.Log
	require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", user.Id, model.LogTypeTopup).First(&topupLog).Error)
	require.Contains(t, topupLog.Content, "订阅")
}

func TestSubscriptionBEpusdtNotifyRejectsTokenMismatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	user := &model.User{Id: 924, Username: "sub_bepusdt_token_guard_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	plan := &model.SubscriptionPlan{
		Id:            815,
		Title:         "BEpusdt Token Guard Plan",
		PriceAmount:   9.99,
		Currency:      "CNY",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	order := &model.SubscriptionOrder{
		UserId:          user.Id,
		PlanId:          plan.Id,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "CNY",
		TradeNo:         "sub-bepusdt-token-mismatch",
		PaymentMethod:   model.PaymentMethodUSDT,
		PaymentProvider: model.PaymentProviderBEpusdt,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, order.Insert())

	body := signedBEpusdtCallback(map[string]interface{}{
		"trade_id":   "BEPAY202605220004",
		"order_id":   order.TradeNo,
		"amount":     "9.99",
		"fiat":       "CNY",
		"currencies": "ETH",
		"status":     2,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/subscription/bepusdt/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	SubscriptionBEpusdtNotify(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, "fail", w.Body.String())
	reloaded := model.GetSubscriptionOrderByTradeNo(order.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusPending, reloaded.Status)
	var subscriptionCount int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", user.Id).Count(&subscriptionCount).Error)
	require.Zero(t, subscriptionCount)
}

func TestSubscriptionBEpusdtNotifyRejectsCurrencyMismatchWithBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	user := &model.User{Id: 926, Username: "sub_bepusdt_currency_guard_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	plan := &model.SubscriptionPlan{
		Id:            816,
		Title:         "BEpusdt Currency Guard Plan",
		PriceAmount:   9.99,
		Currency:      "CNY",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	order := &model.SubscriptionOrder{
		UserId:          user.Id,
		PlanId:          plan.Id,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "CNY",
		TradeNo:         "sub-bepusdt-currency-mismatch",
		PaymentMethod:   model.PaymentMethodUSDT,
		PaymentProvider: model.PaymentProviderBEpusdt,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, order.Insert())

	body := signedBEpusdtCallback(map[string]interface{}{
		"trade_id": "BEPAY202605220006",
		"order_id": order.TradeNo,
		"amount":   "9.99",
		"fiat":     "USD",
		"status":   2,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/subscription/bepusdt/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	SubscriptionBEpusdtNotify(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, "fail", w.Body.String())
	reloaded := model.GetSubscriptionOrderByTradeNo(order.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusPending, reloaded.Status)
	var subscriptionCount int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", user.Id).Count(&subscriptionCount).Error)
	require.Zero(t, subscriptionCount)
}

func TestSubscriptionBEpusdtNotifyRejectsLegacyBEpusdtNetworkOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	user := &model.User{Id: 918, Username: "sub_bepusdt_network_guard_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	plan := &model.SubscriptionPlan{
		Id:            812,
		Title:         "BEpusdt Network Guard Plan",
		PriceAmount:   9.99,
		Currency:      "CNY",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	order := &model.SubscriptionOrder{
		UserId:          user.Id,
		PlanId:          plan.Id,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "CNY",
		TradeNo:         "sub-bepusdt-network-guard",
		PaymentMethod:   service.BuildBEpusdtPaymentMethod("usdt", "tron"),
		PaymentProvider: model.PaymentProviderBEpusdt,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, order.Insert())

	body := signedBEpusdtCallback(map[string]interface{}{
		"pid":            setting.BEpusdtPID,
		"trade_id":       "T202605180004",
		"order_id":       order.TradeNo,
		"amount":         "9.99",
		"order_currency": "CNY",
		"token":          "USDT",
		"network":        "polygon",
		"status":         2,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/subscription/bepusdt/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	SubscriptionBEpusdtNotify(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, "fail", w.Body.String())
	reloaded := model.GetSubscriptionOrderByTradeNo(order.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusPending, reloaded.Status)
	var subscriptionCount int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", user.Id).Count(&subscriptionCount).Error)
	require.Zero(t, subscriptionCount)
}

func TestSubscriptionBEpusdtNotifyRejectsLegacyBEpusdtPaymentTypeOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	user := &model.User{Id: 920, Username: "sub_bepusdt_payment_type_guard_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	plan := &model.SubscriptionPlan{
		Id:            813,
		Title:         "BEpusdt Payment Type Guard Plan",
		PriceAmount:   9.99,
		Currency:      "CNY",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	order := &model.SubscriptionOrder{
		UserId:          user.Id,
		PlanId:          plan.Id,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "CNY",
		TradeNo:         "sub-bepusdt-payment-type-guard",
		PaymentMethod:   service.BuildBEpusdtPaymentMethod("usdt", "tron"),
		PaymentProvider: model.PaymentProviderBEpusdt,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, order.Insert())

	body := signedBEpusdtCallback(map[string]interface{}{
		"pid":            setting.BEpusdtPID,
		"trade_id":       "T202605190002",
		"order_id":       order.TradeNo,
		"amount":         "9.99",
		"order_currency": "CNY",
		"token":          "USDT",
		"network":        "tron",
		"payment_type":   "epay:alipay",
		"status":         2,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/subscription/bepusdt/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	SubscriptionBEpusdtNotify(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, "fail", w.Body.String())
	reloaded := model.GetSubscriptionOrderByTradeNo(order.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusPending, reloaded.Status)
	var subscriptionCount int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", user.Id).Count(&subscriptionCount).Error)
	require.Zero(t, subscriptionCount)
}

func TestSubscriptionEpayNotifyRejectsMismatchedMerchant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	user := &model.User{Id: 911, Username: "sub_merchant_guard_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	plan := &model.SubscriptionPlan{
		Id:            809,
		Title:         "Merchant Guard Plan",
		PriceAmount:   9.99,
		Currency:      "CNY",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	order := &model.SubscriptionOrder{
		UserId:          user.Id,
		PlanId:          plan.Id,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "CNY",
		TradeNo:         "sub-merchant-guard",
		PaymentMethod:   "alipay",
		PaymentProvider: model.PaymentProviderEpay,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, order.Insert())

	params := signedEpayCallback(map[string]string{
		"pid":          "wrong-pid",
		"type":         "alipay",
		"out_trade_no": order.TradeNo,
		"trade_no":     "gateway-sub-merchant-guard",
		"name":         "SUB:Merchant Guard Plan",
		"money":        "9.99",
		"trade_status": epay.StatusTradeSuccess,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/subscription/epay/notify", strings.NewReader(params.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	SubscriptionEpayNotify(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "fail", w.Body.String())
	reloaded := model.GetSubscriptionOrderByTradeNo(order.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusPending, reloaded.Status)
	var subscriptionCount int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", user.Id).Count(&subscriptionCount).Error)
	require.Zero(t, subscriptionCount)
}

func TestSubscriptionEpayNotifyRejectsWhenWebhookDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	paymentSetting := operation_setting.GetPaymentSetting()
	paymentSetting.ComplianceConfirmed = false

	user := &model.User{Id: 919, Username: "sub_epay_disabled_guard_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	plan := &model.SubscriptionPlan{
		Id:            813,
		Title:         "Disabled Webhook Guard Plan",
		PriceAmount:   9.99,
		Currency:      "CNY",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	order := &model.SubscriptionOrder{
		UserId:          user.Id,
		PlanId:          plan.Id,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "CNY",
		TradeNo:         "sub-epay-disabled-webhook",
		PaymentMethod:   "alipay",
		PaymentProvider: model.PaymentProviderEpay,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, order.Insert())

	params := signedEpayCallback(map[string]string{
		"pid":          operation_setting.EpayId,
		"type":         "alipay",
		"out_trade_no": order.TradeNo,
		"trade_no":     "gateway-sub-disabled-webhook",
		"name":         "SUB:Disabled Webhook Guard Plan",
		"money":        "9.99",
		"trade_status": epay.StatusTradeSuccess,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/subscription/epay/notify", strings.NewReader(params.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	SubscriptionEpayNotify(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "fail", w.Body.String())
	reloaded := model.GetSubscriptionOrderByTradeNo(order.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusPending, reloaded.Status)
	var subscriptionCount int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", user.Id).Count(&subscriptionCount).Error)
	require.Zero(t, subscriptionCount)
}

func TestSubscriptionEpayReturnDoesNotCompleteOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	user := &model.User{Id: 909, Username: "return_guard_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	plan := &model.SubscriptionPlan{
		Id:            808,
		Title:         "Return Guard Plan",
		PriceAmount:   9.99,
		Currency:      "CNY",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	order := &model.SubscriptionOrder{
		UserId:          user.Id,
		PlanId:          plan.Id,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "CNY",
		TradeNo:         "sub-return-guard",
		PaymentMethod:   "alipay",
		PaymentProvider: model.PaymentProviderEpay,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, order.Insert())

	query := signedEpayCallback(map[string]string{
		"pid":          operation_setting.EpayId,
		"type":         "alipay",
		"out_trade_no": order.TradeNo,
		"trade_no":     "gateway-123",
		"name":         "SUB:Return Guard Plan",
		"money":        "9.99",
		"trade_status": epay.StatusTradeSuccess,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/subscription/epay/return?"+query.Encode(), nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	SubscriptionEpayReturn(c)

	require.Equal(t, http.StatusFound, w.Code)
	reloaded := model.GetSubscriptionOrderByTradeNo(order.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusPending, reloaded.Status)

	var subscriptionCount int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", user.Id).Count(&subscriptionCount).Error)
	require.Zero(t, subscriptionCount)
}

func TestProcessPaidTopUpCommissionDoesNotBlockPaidOrderOnReferralFailure(t *testing.T) {
	setupPaymentCallbackGuardDB(t)

	user := &model.User{Id: 920, Username: "topup_referral_failure_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:                   user.Id,
		Amount:                   2,
		Money:                    9.99,
		PaidAmount:               9.99,
		PaidCurrency:             "XXX",
		TradeNo:                  "topup-referral-failure",
		PaymentMethod:            "alipay",
		PaymentProvider:          model.PaymentProviderEpay,
		Status:                   common.TopUpStatusSuccess,
		CreateTime:               time.Now().Unix(),
		ReferralAffiliateId:      1,
		ReferralRate:             10,
		ReferralCommissionStatus: model.ReferralCommissionJobStatusPending,
	}
	require.NoError(t, topUp.Insert())

	require.NoError(t, processPaidTopUpCommission(context.Background(), topUp.TradeNo))

	reloaded := model.GetTopUpByTradeNo(topUp.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusSuccess, reloaded.Status)
	require.Equal(t, model.ReferralCommissionJobStatusFailed, reloaded.ReferralCommissionStatus)
	require.NotEmpty(t, reloaded.ReferralCommissionError)
}

func TestProcessPaidSubscriptionCommissionDoesNotBlockPaidOrderOnReferralFailure(t *testing.T) {
	setupPaymentCallbackGuardDB(t)

	user := &model.User{Id: 921, Username: "sub_referral_failure_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	order := &model.SubscriptionOrder{
		UserId:                   user.Id,
		PlanId:                   1,
		Money:                    9.99,
		PaidAmount:               9.99,
		PaidCurrency:             "XXX",
		TradeNo:                  "sub-referral-failure",
		PaymentMethod:            "alipay",
		PaymentProvider:          model.PaymentProviderEpay,
		Status:                   common.TopUpStatusSuccess,
		CreateTime:               time.Now().Unix(),
		ReferralAffiliateId:      1,
		ReferralRate:             10,
		ReferralCommissionStatus: model.ReferralCommissionJobStatusPending,
	}
	require.NoError(t, order.Insert())

	require.NoError(t, processPaidSubscriptionCommission(context.Background(), order.TradeNo))

	reloaded := model.GetSubscriptionOrderByTradeNo(order.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusSuccess, reloaded.Status)
	require.Equal(t, model.ReferralCommissionJobStatusFailed, reloaded.ReferralCommissionStatus)
	require.NotEmpty(t, reloaded.ReferralCommissionError)
}
