package controller

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
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

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	system_setting.ServerAddress = "http://newapi.test"
	operation_setting.PayAddress = "https://pay.example.com"
	operation_setting.EpayId = "pid-test"
	operation_setting.EpayKey = "key-test"

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
