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
		&model.UserLoginIdentifier{},
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
		PaymentMethod:           model.PaymentMethodUSDT,
		PaymentProvider:         model.PaymentProviderBEpusdt,
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
		PaymentMethod:   model.PaymentMethodUSDT,
		PaymentProvider: model.PaymentProviderBEpusdt,
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

func TestGetRechargeAuditFiltersByUserID(t *testing.T) {
	setupAdminFinanceTestDB(t)
	gin.SetMode(gin.TestMode)

	require.NoError(t, model.DB.Create(&model.User{Id: 1, Username: "alice", Password: "password123"}).Error)
	require.NoError(t, model.DB.Create(&model.User{Id: 2, Username: "bob", Password: "password123"}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:          1,
		Amount:          3,
		Money:           21.9,
		PaidAmount:      21.9,
		PaidCurrency:    "CNY",
		TradeNo:         "ALICE-TOPUP",
		PaymentMethod:   "alipay",
		PaymentProvider: model.PaymentProviderEpay,
		CreateTime:      100,
		Status:          common.TopUpStatusSuccess,
	}).Error)
	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		UserId:                  1,
		PlanId:                  10,
		Money:                   29.9,
		PaidAmount:              29.9,
		PaidCurrency:            "CNY",
		TradeNo:                 "ALICE-SUB",
		PaymentMethod:           model.PaymentMethodUSDT,
		PaymentProvider:         model.PaymentProviderBEpusdt,
		PlanTitleSnapshot:       "Alice Pro",
		PlanPriceSnapshot:       29.9,
		PlanTotalAmountSnapshot: 9000000,
		CreateTime:              200,
		Status:                  common.TopUpStatusSuccess,
	}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:          2,
		Amount:          9,
		Money:           59.9,
		PaidAmount:      59.9,
		PaidCurrency:    "CNY",
		TradeNo:         "BOB-TOPUP",
		PaymentMethod:   "alipay",
		PaymentProvider: model.PaymentProviderEpay,
		CreateTime:      300,
		Status:          common.TopUpStatusSuccess,
	}).Error)

	router := gin.New()
	router.GET("/audit", GetRechargeAudit)
	router.GET("/audit/summary", GetRechargeAuditSummary)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/audit?p=1&page_size=10&user_id=1", nil)
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code)

	var listPayload struct {
		Success bool `json:"success"`
		Data    struct {
			Total int                  `json:"total"`
			Items []rechargeAuditOrder `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &listPayload))
	require.True(t, listPayload.Success)
	require.Equal(t, 2, listPayload.Data.Total)
	require.Len(t, listPayload.Data.Items, 2)
	for _, item := range listPayload.Data.Items {
		require.Equal(t, 1, item.UserID)
		require.NotEqual(t, "BOB-TOPUP", item.TradeNo)
	}

	recorder = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/audit/summary?user_id=1", nil)
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code)

	var summaryPayload struct {
		Success bool `json:"success"`
		Data    struct {
			Totals rechargeAuditTotals `json:"totals"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &summaryPayload))
	require.True(t, summaryPayload.Success)
	require.EqualValues(t, 2, summaryPayload.Data.Totals.TotalCount)
	require.EqualValues(t, 2, summaryPayload.Data.Totals.SuccessCount)
	require.InDelta(t, 51.8, summaryPayload.Data.Totals.PaidAmountCNY, 0.000001)
	require.InDelta(t, 3, summaryPayload.Data.Totals.CreditAmount, 0.000001)
}

func TestOrderManagementBaseSQLPushesFiltersIntoOrderBranches(t *testing.T) {
	sql, args := orderManagementBaseSQL(rechargeAuditFilters{userID: 42})
	require.Contains(t, sql, "FROM top_ups AS t")
	require.Contains(t, sql, "FROM subscription_orders AS s")
	require.Contains(t, sql, "t.user_id = ?")
	require.Contains(t, sql, "s.user_id = ?")
	require.Len(t, args, 5)
	require.Equal(t, 42, args[3])
	require.Equal(t, 42, args[4])

	sql, args = orderManagementBaseSQL(rechargeAuditFilters{
		orderType: "topup",
		userID:    42,
	})
	require.Contains(t, sql, "FROM top_ups AS t")
	require.NotContains(t, sql, "FROM subscription_orders AS s")
	require.Contains(t, sql, "t.user_id = ?")
	require.Len(t, args, 4)
	require.Equal(t, 42, args[3])
}

func TestGetRechargeAuditPreservesStoredPaidCurrencyForFutureGateways(t *testing.T) {
	setupAdminFinanceTestDB(t)
	gin.SetMode(gin.TestMode)

	require.NoError(t, model.DB.Create(&model.User{Id: 1, Username: "alice", Password: "password123"}).Error)
	require.NoError(t, model.DB.Create(&model.User{Id: 2, Username: "bob", Password: "password123"}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:          1,
		Amount:          3,
		Money:           12,
		PaidAmount:      12,
		PaidCurrency:    "USD",
		TradeNo:         "STRIPE-CURRENCY-TOPUP",
		PaymentMethod:   "stripe",
		PaymentProvider: model.PaymentProviderStripe,
		CreateTime:      100,
		Status:          common.TopUpStatusSuccess,
	}).Error)
	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		UserId:          2,
		PlanId:          10,
		Money:           9.9,
		PaidAmount:      9.9,
		PaidCurrency:    "USD",
		TradeNo:         "STRIPE-CURRENCY-SUBSCRIPTION",
		PaymentMethod:   "stripe",
		PaymentProvider: model.PaymentProviderStripe,
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
			Items []rechargeAuditOrder `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Len(t, payload.Data.Items, 2)
	for _, item := range payload.Data.Items {
		require.Equal(t, "USD", item.PaidCurrency)
	}
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
		PaymentMethod:   model.PaymentMethodUSDT,
		PaymentProvider: model.PaymentProviderBEpusdt,
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
		PaymentMethod:   model.PaymentMethodUSDT,
		PaymentProvider: model.PaymentProviderBEpusdt,
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

func TestGetRechargeAuditSummaryRespectsWindowHours(t *testing.T) {
	setupAdminFinanceTestDB(t)
	gin.SetMode(gin.TestMode)

	now := common.GetTimestamp()
	require.NoError(t, model.DB.Create(&model.User{Id: 1, Username: "alice", Password: "password123"}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:          1,
		Amount:          3,
		Money:           21.9,
		PaidAmount:      21.9,
		PaidCurrency:    "CNY",
		TradeNo:         "RECENT-TOPUP",
		PaymentMethod:   "alipay",
		PaymentProvider: model.PaymentProviderEpay,
		CreateTime:      now - 60,
		Status:          common.TopUpStatusSuccess,
	}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:          1,
		Amount:          9,
		Money:           59.9,
		PaidAmount:      59.9,
		PaidCurrency:    "CNY",
		TradeNo:         "OLD-TOPUP",
		PaymentMethod:   "alipay",
		PaymentProvider: model.PaymentProviderEpay,
		CreateTime:      now - 25*60*60,
		Status:          common.TopUpStatusSuccess,
	}).Error)

	router := gin.New()
	router.GET("/audit/summary", GetRechargeAuditSummary)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/audit/summary?window_hours=24", nil)
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
	require.EqualValues(t, 1, payload.Data.Totals.TotalCount)
	require.EqualValues(t, 1, payload.Data.Totals.SuccessCount)
	require.InDelta(t, 21.9, payload.Data.Totals.PaidAmountCNY, 0.000001)
}

func TestGetRechargeAuditSummaryCountsNewOrdersAfterCursor(t *testing.T) {
	setupAdminFinanceTestDB(t)
	gin.SetMode(gin.TestMode)

	require.NoError(t, model.DB.Create(&model.User{Id: 1, Username: "alice", Password: "password123"}).Error)
	ackedTopUp := model.TopUp{
		UserId:       1,
		TradeNo:      "ACKED-TOPUP",
		CreateTime:   100,
		CompleteTime: 500,
		Status:       common.TopUpStatusSuccess,
	}
	require.NoError(t, model.DB.Create(&ackedTopUp).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:       1,
		TradeNo:      "NEW-TOPUP",
		CreateTime:   200,
		CompleteTime: 600,
		Status:       common.TopUpStatusSuccess,
	}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:       1,
		TradeNo:      "PENDING-TOPUP",
		CreateTime:   300,
		CompleteTime: 700,
		Status:       common.TopUpStatusPending,
	}).Error)
	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		UserId:       1,
		TradeNo:      "NEW-SUB",
		CreateTime:   400,
		CompleteTime: 700,
		Status:       common.TopUpStatusFailed,
	}).Error)

	router := gin.New()
	router.GET("/audit/summary", GetRechargeAuditSummary)
	recorder := httptest.NewRecorder()
	afterCursor := formatRechargeAuditOrderCursor(rechargeAuditOrderCursor{
		CompleteTime: 500,
		OrderRank:    1,
		ID:           ackedTopUp.Id,
	})
	req := httptest.NewRequest(http.MethodGet, "/audit/summary?after_order_cursor="+afterCursor, nil)
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code)

	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			NewOrderCount     int64  `json:"new_order_count"`
			LatestOrderCursor string `json:"latest_order_cursor"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.EqualValues(t, 2, payload.Data.NewOrderCount)
	require.NotEmpty(t, payload.Data.LatestOrderCursor)
}

func TestGetRechargeAuditBadgeOnlyCountsNewOrdersAfterCursor(t *testing.T) {
	setupAdminFinanceTestDB(t)
	gin.SetMode(gin.TestMode)

	require.NoError(t, model.DB.Create(&model.User{Id: 1, Username: "alice", Password: "password123"}).Error)
	ackedTopUp := model.TopUp{
		UserId:       1,
		TradeNo:      "BADGE-ACKED-TOPUP",
		CreateTime:   100,
		CompleteTime: 500,
		Status:       common.TopUpStatusSuccess,
	}
	require.NoError(t, model.DB.Create(&ackedTopUp).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:       1,
		TradeNo:      "BADGE-NEW-TOPUP",
		CreateTime:   200,
		CompleteTime: 600,
		Status:       common.TopUpStatusExpired,
	}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:       1,
		TradeNo:      "BADGE-PENDING-TOPUP",
		CreateTime:   300,
		CompleteTime: 700,
		Status:       common.TopUpStatusPending,
	}).Error)
	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		UserId:       1,
		TradeNo:      "BADGE-NEW-SUB",
		CreateTime:   400,
		CompleteTime: 700,
		Status:       common.TopUpStatusFailed,
	}).Error)

	router := gin.New()
	router.GET("/audit/summary", GetRechargeAuditSummary)
	recorder := httptest.NewRecorder()
	afterCursor := formatRechargeAuditOrderCursor(rechargeAuditOrderCursor{
		CompleteTime: 500,
		OrderRank:    rechargeAuditTopupOrderRank,
		ID:           ackedTopUp.Id,
	})
	req := httptest.NewRequest(http.MethodGet, "/audit/summary?badge_only=1&after_order_cursor="+afterCursor, nil)
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code)

	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			NewOrderCount     int64  `json:"new_order_count"`
			LatestOrderCursor string `json:"latest_order_cursor"`
			Anomalies         []any  `json:"anomalies"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.EqualValues(t, 2, payload.Data.NewOrderCount)
	require.Equal(
		t,
		formatRechargeAuditOrderCursor(rechargeAuditOrderCursor{
			CompleteTime: 700,
			OrderRank:    rechargeAuditSubscriptionOrderRank,
			ID:           1,
		}),
		payload.Data.LatestOrderCursor,
	)
	require.Empty(t, payload.Data.Anomalies)
}
