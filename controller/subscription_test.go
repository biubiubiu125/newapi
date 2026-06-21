package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupSubscriptionControllerTestDB(t *testing.T) {
	t.Helper()

	previousDB := model.DB
	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL
	paymentSetting := operation_setting.GetPaymentSetting()
	previousComplianceConfirmed := paymentSetting.ComplianceConfirmed
	previousComplianceTermsVersion := paymentSetting.ComplianceTermsVersion

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.SubscriptionPlan{}))

	t.Cleanup(func() {
		model.DB = previousDB
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		paymentSetting.ComplianceConfirmed = previousComplianceConfirmed
		paymentSetting.ComplianceTermsVersion = previousComplianceTermsVersion
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
}

type subscriptionPlansAPIResponse struct {
	Success bool `json:"success"`
	Data    []struct {
		Plan model.SubscriptionPlan `json:"plan"`
	} `json:"data"`
}

func decodeSubscriptionPlansResponse(t *testing.T, recorder *httptest.ResponseRecorder) subscriptionPlansAPIResponse {
	t.Helper()

	require.Equal(t, http.StatusOK, recorder.Code)
	var response subscriptionPlansAPIResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Len(t, response.Data, 1)
	return response
}

func TestGetSubscriptionPlansNormalizesPlanDefaults(t *testing.T) {
	setupSubscriptionControllerTestDB(t)
	gin.SetMode(gin.TestMode)

	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id:            601,
		Title:         "Public Plan",
		PriceAmount:   9.99,
		Currency:      "CNY",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/subscription/plans", nil)

	GetSubscriptionPlans(ctx)

	response := decodeSubscriptionPlansResponse(t, recorder)
	require.NotNil(t, response.Data[0].Plan.AllowBalancePay)
	require.True(t, *response.Data[0].Plan.AllowBalancePay)
	require.NotNil(t, response.Data[0].Plan.AllowWalletOverflow)
	require.True(t, *response.Data[0].Plan.AllowWalletOverflow)
}

func TestAdminListSubscriptionPlansNormalizesPlanDefaults(t *testing.T) {
	setupSubscriptionControllerTestDB(t)
	gin.SetMode(gin.TestMode)

	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id:            602,
		Title:         "Admin Plan",
		PriceAmount:   9.99,
		Currency:      "CNY",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/subscription/admin/plans", nil)

	AdminListSubscriptionPlans(ctx)

	response := decodeSubscriptionPlansResponse(t, recorder)
	require.NotNil(t, response.Data[0].Plan.AllowBalancePay)
	require.True(t, *response.Data[0].Plan.AllowBalancePay)
	require.NotNil(t, response.Data[0].Plan.AllowWalletOverflow)
	require.True(t, *response.Data[0].Plan.AllowWalletOverflow)
}
