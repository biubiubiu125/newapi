package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupAdminFinanceTestDB(t *testing.T) {
	t.Helper()

	previousDB := model.DB
	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL
	previousQuotaPerUnit := common.QuotaPerUnit

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.QuotaPerUnit = 500000

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.TopUp{},
		&model.SubscriptionPlan{},
		&model.SubscriptionOrder{},
	))

	t.Cleanup(func() {
		model.DB = previousDB
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		common.QuotaPerUnit = previousQuotaPerUnit
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
}

func TestGetRechargeAuditReturnsTopupAndSubscriptionOrders(t *testing.T) {
	setupAdminFinanceTestDB(t)
	gin.SetMode(gin.TestMode)

	require.NoError(t, model.DB.Create(&model.User{Id: 1, Username: "alice", Password: "password123"}).Error)
	require.NoError(t, model.DB.Create(&model.User{Id: 2, Username: "bob", Password: "password123"}).Error)
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id:          10,
		Title:       "Pro Plan",
		PriceAmount: 29.9,
		Currency:    "CNY",
		TotalAmount: 9000000,
	}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:          1,
		Amount:          3,
		Money:           21.9,
		PaidAmount:      21.9,
		PaidCurrency:    "CNY",
		TradeNo:         "TOPUP-1",
		PaymentMethod:   "alipay",
		PaymentProvider: model.PaymentProviderEpay,
		CreateTime:      100,
		Status:          common.TopUpStatusSuccess,
	}).Error)
	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		UserId:                  2,
		PlanId:                  10,
		Money:                   29.9,
		PaidAmount:              29.9,
		PaidCurrency:            "CNY",
		TradeNo:                 "SUB-1",
		PaymentMethod:           "epusdt:usdt:tron",
		PaymentProvider:         model.PaymentProviderEpusdt,
		PlanTitleSnapshot:       "Pro Snapshot",
		PlanPriceSnapshot:       29.9,
		PlanTotalAmountSnapshot: 9000000,
		CreateTime:              200,
		Status:                  common.TopUpStatusPending,
	}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:          2,
		Amount:          0,
		Money:           29.9,
		PaidAmount:      29.9,
		PaidCurrency:    "CNY",
		TradeNo:         "SUB-1",
		PaymentMethod:   "epusdt:usdt:tron",
		PaymentProvider: model.PaymentProviderEpusdt,
		CreateTime:      200,
		Status:          common.TopUpStatusSuccess,
	}).Error)

	router := gin.New()
	router.GET("/audit", GetRechargeAudit)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/audit?p=1&page_size=10", nil)
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code)

	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Total int                  `json:"total"`
			Items []rechargeAuditOrder `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Equal(t, 2, payload.Data.Total)
	require.Len(t, payload.Data.Items, 2)
	require.Equal(t, "subscription", payload.Data.Items[0].OrderType)
	require.Equal(t, "SUB-1", payload.Data.Items[0].TradeNo)
	require.Equal(t, "Pro Snapshot", payload.Data.Items[0].ProductName)
	require.Equal(t, "topup", payload.Data.Items[1].OrderType)
	require.Equal(t, "TOPUP-1", payload.Data.Items[1].TradeNo)
	require.InDelta(t, 21.9, payload.Data.Items[1].PaidAmountCNY, 0.000001)
}

func TestGetRechargeAuditSummaryCountsUnifiedOrders(t *testing.T) {
	setupAdminFinanceTestDB(t)
	gin.SetMode(gin.TestMode)

	require.NoError(t, model.DB.Create(&model.User{Id: 1, Username: "alice", Password: "password123"}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:          1,
		Amount:          3,
		Money:           21.9,
		PaidAmount:      21.9,
		PaidCurrency:    "CNY",
		TradeNo:         "TOPUP-SUMMARY",
		PaymentMethod:   "alipay",
		PaymentProvider: model.PaymentProviderEpay,
		CreateTime:      100,
		Status:          common.TopUpStatusSuccess,
	}).Error)
	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		UserId:          1,
		PlanId:          10,
		Money:           29.9,
		PaidAmount:      29.9,
		PaidCurrency:    "CNY",
		TradeNo:         "SUB-SUMMARY",
		PaymentMethod:   "epusdt:usdt:tron",
		PaymentProvider: model.PaymentProviderEpusdt,
		CreateTime:      200,
		Status:          common.TopUpStatusPending,
	}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:          1,
		Amount:          0,
		Money:           29.9,
		PaidAmount:      29.9,
		PaidCurrency:    "CNY",
		TradeNo:         "SUB-SUMMARY",
		PaymentMethod:   "epusdt:usdt:tron",
		PaymentProvider: model.PaymentProviderEpusdt,
		CreateTime:      200,
		Status:          common.TopUpStatusSuccess,
	}).Error)

	router := gin.New()
	router.GET("/audit/summary", GetRechargeAuditSummary)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/audit/summary", nil)
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code)

	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Totals rechargeAuditTotals `json:"totals"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.EqualValues(t, 2, payload.Data.Totals.TotalCount)
	require.EqualValues(t, 1, payload.Data.Totals.SuccessCount)
	require.EqualValues(t, 1, payload.Data.Totals.PendingCount)
	require.InDelta(t, 21.9, payload.Data.Totals.PaidAmountCNY, 0.000001)
	require.InDelta(t, 3, payload.Data.Totals.CreditAmount, 0.000001)
}
