package controller

import (
	"bytes"
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
	require.NoError(t, db.AutoMigrate(&model.SubscriptionPlan{}, &model.UserSubscription{}))

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

func TestAdminUpdateSubscriptionPlanSyncsActiveSubscriptionBenefitsWithoutQuota(t *testing.T) {
	setupSubscriptionControllerTestDB(t)
	gin.SetMode(gin.TestMode)

	trueValue := true
	falseValue := false
	now := common.GetTimestamp()

	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id:                  603,
		Title:               "Original Plan",
		PriceAmount:         9.99,
		Currency:            "CNY",
		DurationUnit:        model.SubscriptionDurationMonth,
		DurationValue:       1,
		Enabled:             true,
		TotalAmount:         1000,
		GrantGroups:         "default",
		DowngradeGroup:      "default",
		AllowBalancePay:     &trueValue,
		AllowWalletOverflow: &trueValue,
	}).Error)

	require.NoError(t, model.DB.Create(&model.UserSubscription{
		Id:                  701,
		UserId:              1001,
		PlanId:              603,
		AmountTotal:         12345,
		AmountUsed:          2345,
		StartTime:           now - 60,
		EndTime:             now + 3600,
		Status:              "active",
		Source:              "order",
		UpgradeGroup:        "default",
		GrantGroups:         "default",
		DowngradeGroup:      "default",
		AllowWalletOverflow: true,
	}).Error)
	require.NoError(t, model.DB.Create(&model.UserSubscription{
		Id:                  702,
		UserId:              1002,
		PlanId:              603,
		AmountTotal:         8888,
		AmountUsed:          888,
		StartTime:           now - 7200,
		EndTime:             now - 3600,
		Status:              "active",
		Source:              "order",
		GrantGroups:         "default",
		DowngradeGroup:      "default",
		AllowWalletOverflow: true,
	}).Error)

	body, err := json.Marshal(AdminUpsertSubscriptionPlanRequest{
		Plan: model.SubscriptionPlan{
			Title:               "Updated Plan",
			Subtitle:            "Updated Subtitle",
			PriceAmount:         18.88,
			Currency:            "CNY",
			DurationUnit:        model.SubscriptionDurationMonth,
			DurationValue:       1,
			Enabled:             true,
			SortOrder:           2,
			MaxPurchasePerUser:  3,
			TotalAmount:         999999,
			UpgradeGroup:        "vip",
			GrantGroups:         "vip,svip",
			DowngradeGroup:      "svip",
			QuotaResetPeriod:    model.SubscriptionResetNever,
			AllowBalancePay:     &trueValue,
			AllowWalletOverflow: &falseValue,
		},
		SyncActiveUserSubscriptions: true,
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "603"}}
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/subscription/admin/plans/603", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	AdminUpdateSubscriptionPlan(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)

	var activeSub model.UserSubscription
	require.NoError(t, model.DB.Where("id = ?", 701).First(&activeSub).Error)
	require.Equal(t, "vip,svip", activeSub.GrantGroups)
	require.Equal(t, "svip", activeSub.DowngradeGroup)
	require.False(t, activeSub.AllowWalletOverflow)
	require.Equal(t, "default", activeSub.UpgradeGroup)
	require.Equal(t, int64(12345), activeSub.AmountTotal)
	require.Equal(t, int64(2345), activeSub.AmountUsed)

	var expiredSub model.UserSubscription
	require.NoError(t, model.DB.Where("id = ?", 702).First(&expiredSub).Error)
	require.Equal(t, "default", expiredSub.GrantGroups)
	require.Equal(t, "default", expiredSub.DowngradeGroup)
	require.True(t, expiredSub.AllowWalletOverflow)
	require.Equal(t, int64(8888), expiredSub.AmountTotal)
	require.Equal(t, int64(888), expiredSub.AmountUsed)
}

func TestAdminUpdateSubscriptionPlanRejectsInvalidDowngradeGroup(t *testing.T) {
	setupSubscriptionControllerTestDB(t)
	gin.SetMode(gin.TestMode)

	trueValue := true
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id:                  604,
		Title:               "Original Plan",
		PriceAmount:         9.99,
		Currency:            "CNY",
		DurationUnit:        model.SubscriptionDurationMonth,
		DurationValue:       1,
		Enabled:             true,
		TotalAmount:         1000,
		AllowBalancePay:     &trueValue,
		AllowWalletOverflow: &trueValue,
	}).Error)

	body, err := json.Marshal(AdminUpsertSubscriptionPlanRequest{
		Plan: model.SubscriptionPlan{
			Title:               "Updated Plan",
			PriceAmount:         18.88,
			Currency:            "CNY",
			DurationUnit:        model.SubscriptionDurationMonth,
			DurationValue:       1,
			Enabled:             true,
			TotalAmount:         1000,
			DowngradeGroup:      "missing-group",
			QuotaResetPeriod:    model.SubscriptionResetNever,
			AllowBalancePay:     &trueValue,
			AllowWalletOverflow: &trueValue,
		},
		SyncActiveUserSubscriptions: true,
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "604"}}
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/subscription/admin/plans/604", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	AdminUpdateSubscriptionPlan(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
	require.NotEmpty(t, response.Message)

	var plan model.SubscriptionPlan
	require.NoError(t, model.DB.Where("id = ?", 604).First(&plan).Error)
	require.Equal(t, "Original Plan", plan.Title)
	require.Empty(t, plan.DowngradeGroup)
}

func TestAdminCreateSubscriptionPlanRejectsInvalidGrantGroup(t *testing.T) {
	setupSubscriptionControllerTestDB(t)
	gin.SetMode(gin.TestMode)

	trueValue := true
	body, err := json.Marshal(AdminUpsertSubscriptionPlanRequest{
		Plan: model.SubscriptionPlan{
			Title:               "Invalid Grant Plan",
			PriceAmount:         9.99,
			Currency:            "CNY",
			DurationUnit:        model.SubscriptionDurationMonth,
			DurationValue:       1,
			Enabled:             true,
			TotalAmount:         1000,
			GrantGroups:         "missing-group",
			QuotaResetPeriod:    model.SubscriptionResetNever,
			AllowBalancePay:     &trueValue,
			AllowWalletOverflow: &trueValue,
		},
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/subscription/admin/plans", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	AdminCreateSubscriptionPlan(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
	require.NotEmpty(t, response.Message)

	var count int64
	require.NoError(t, model.DB.Model(&model.SubscriptionPlan{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestAdminUpdateSubscriptionPlanRejectsInvalidGrantGroup(t *testing.T) {
	setupSubscriptionControllerTestDB(t)
	gin.SetMode(gin.TestMode)

	trueValue := true
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{
		Id:                  605,
		Title:               "Original Plan",
		PriceAmount:         9.99,
		Currency:            "CNY",
		DurationUnit:        model.SubscriptionDurationMonth,
		DurationValue:       1,
		Enabled:             true,
		TotalAmount:         1000,
		GrantGroups:         "default",
		AllowBalancePay:     &trueValue,
		AllowWalletOverflow: &trueValue,
	}).Error)

	body, err := json.Marshal(AdminUpsertSubscriptionPlanRequest{
		Plan: model.SubscriptionPlan{
			Title:               "Updated Plan",
			PriceAmount:         18.88,
			Currency:            "CNY",
			DurationUnit:        model.SubscriptionDurationMonth,
			DurationValue:       1,
			Enabled:             true,
			TotalAmount:         1000,
			GrantGroups:         "missing-group",
			QuotaResetPeriod:    model.SubscriptionResetNever,
			AllowBalancePay:     &trueValue,
			AllowWalletOverflow: &trueValue,
		},
		SyncActiveUserSubscriptions: true,
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "605"}}
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/subscription/admin/plans/605", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	AdminUpdateSubscriptionPlan(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
	require.NotEmpty(t, response.Message)

	var plan model.SubscriptionPlan
	require.NoError(t, model.DB.Where("id = ?", 605).First(&plan).Error)
	require.Equal(t, "Original Plan", plan.Title)
	require.Equal(t, "default", plan.GrantGroups)
}
