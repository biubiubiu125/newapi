package controller

import (
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
	previousEpusdtPID := setting.EpusdtPID
	previousEpusdtSecretKey := setting.EpusdtSecretKey

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	system_setting.ServerAddress = "http://newapi.test"
	operation_setting.PayAddress = "https://pay.example.com"
	operation_setting.EpayId = "pid-test"
	operation_setting.EpayKey = "key-test"
	setting.EpusdtPID = "epusdt-pid-test"
	setting.EpusdtSecretKey = "epusdt-key-test"

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Log{},
		&model.TopUp{},
		&model.SubscriptionPlan{},
		&model.SubscriptionOrder{},
		&model.UserSubscription{},
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
		setting.EpusdtPID = previousEpusdtPID
		setting.EpusdtSecretKey = previousEpusdtSecretKey
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

func signedEpusdtCallback(values map[string]interface{}) string {
	params := map[string]interface{}{}
	for key, value := range values {
		params[key] = value
	}
	params["signature"] = service.EpusdtSign(params, setting.EpusdtSecretKey)
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

func TestEpusdtTopupNotifyRejectsMissingMerchant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	user := &model.User{Id: 912, Username: "epusdt_merchant_guard_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          2,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "CNY",
		TradeNo:         "epusdt-missing-merchant-guard",
		PaymentMethod:   service.BuildEpusdtPaymentMethod("usdt", "tron"),
		PaymentProvider: model.PaymentProviderEpusdt,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())

	body := signedEpusdtCallback(map[string]interface{}{
		"order_id":       topUp.TradeNo,
		"transaction_id": "epusdt-gateway-merchant-guard",
		"amount":         "9.99",
		"order_currency": "CNY",
		"token":          "usdt",
		"network":        "tron",
		"status":         "paid",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/user/epusdt/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	EpusdtTopUpNotify(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "fail", w.Body.String())
	reloaded := model.GetTopUpByTradeNo(topUp.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusPending, reloaded.Status)
	var updatedUser model.User
	require.NoError(t, model.DB.Where("id = ?", user.Id).First(&updatedUser).Error)
	require.Zero(t, updatedUser.Quota)
}

func TestEpusdtTopupNotifyAcceptsGMwalletCallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	user := &model.User{Id: 914, Username: "epusdt_gmpay_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          2,
		Money:           9.99,
		PaidAmount:      9.99,
		PaidCurrency:    "CNY",
		TradeNo:         "epusdt-gmpay-success",
		PaymentMethod:   service.BuildEpusdtPaymentMethod("usdt", "tron"),
		PaymentProvider: model.PaymentProviderEpusdt,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())

	body := signedEpusdtCallback(map[string]interface{}{
		"pid":                  setting.EpusdtPID,
		"trade_id":             "T202605180001",
		"order_id":             topUp.TradeNo,
		"amount":               "9.99",
		"actual_amount":        "1.4285",
		"receive_address":      "TTestAddress",
		"token":                "USDT",
		"network":              "tron",
		"block_transaction_id": "0xtesttx",
		"status":               2,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/user/epusdt/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	EpusdtTopUpNotify(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "ok", w.Body.String())
	reloaded := model.GetTopUpByTradeNo(topUp.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusSuccess, reloaded.Status)
	var updatedUser model.User
	require.NoError(t, model.DB.Where("id = ?", user.Id).First(&updatedUser).Error)
	require.Positive(t, updatedUser.Quota)
}

func TestSubscriptionEpusdtNotifyRejectsMissingMerchant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	user := &model.User{Id: 913, Username: "sub_epusdt_merchant_guard_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	plan := &model.SubscriptionPlan{
		Id:            810,
		Title:         "Epusdt Merchant Guard Plan",
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
		TradeNo:         "sub-epusdt-missing-merchant-guard",
		PaymentMethod:   service.BuildEpusdtPaymentMethod("usdt", "tron"),
		PaymentProvider: model.PaymentProviderEpusdt,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, order.Insert())

	body := signedEpusdtCallback(map[string]interface{}{
		"order_id":       order.TradeNo,
		"transaction_id": "epusdt-gateway-sub-merchant-guard",
		"amount":         "9.99",
		"order_currency": "CNY",
		"token":          "usdt",
		"network":        "tron",
		"status":         "paid",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/subscription/epusdt/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	SubscriptionEpusdtNotify(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "fail", w.Body.String())
	reloaded := model.GetSubscriptionOrderByTradeNo(order.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusPending, reloaded.Status)
	var subscriptionCount int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", user.Id).Count(&subscriptionCount).Error)
	require.Zero(t, subscriptionCount)
}

func TestSubscriptionEpusdtNotifyAcceptsGMwalletCallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	user := &model.User{Id: 915, Username: "sub_epusdt_gmpay_user", Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
	plan := &model.SubscriptionPlan{
		Id:            811,
		Title:         "Epusdt GMPay Plan",
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
		TradeNo:         "sub-epusdt-gmpay-success",
		PaymentMethod:   service.BuildEpusdtPaymentMethod("usdt", "tron"),
		PaymentProvider: model.PaymentProviderEpusdt,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, order.Insert())

	body := signedEpusdtCallback(map[string]interface{}{
		"pid":                  setting.EpusdtPID,
		"trade_id":             "T202605180002",
		"order_id":             order.TradeNo,
		"amount":               "9.99",
		"actual_amount":        "1.4285",
		"receive_address":      "TTestAddress",
		"token":                "USDT",
		"network":              "tron",
		"block_transaction_id": "0xtesttxsub",
		"status":               2,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/subscription/epusdt/notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	SubscriptionEpusdtNotify(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "ok", w.Body.String())
	reloaded := model.GetSubscriptionOrderByTradeNo(order.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusSuccess, reloaded.Status)
	var subscriptionCount int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", user.Id).Count(&subscriptionCount).Error)
	require.Equal(t, int64(1), subscriptionCount)
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
