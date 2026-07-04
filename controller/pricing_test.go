package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type pricingAPIResponse struct {
	Success     bool               `json:"success"`
	Data        []model.Pricing    `json:"data"`
	GroupRatio  map[string]float64 `json:"group_ratio"`
	UsableGroup map[string]string  `json:"usable_group"`
	AutoGroups  []string           `json:"auto_groups"`
}

func withPricingGroupOptions(t *testing.T, groupRatio string, userUsableGroups string) {
	t.Helper()

	originalGroupRatio := ratio_setting.GroupRatio2JSONString()
	originalUserUsableGroups := setting.UserUsableGroups2JSONString()

	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(groupRatio))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(userUsableGroups))
	model.InvalidatePricingCache()

	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatio))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUserUsableGroups))
		model.InvalidatePricingCache()
	})
}

func TestFilterPricingByUsableGroupsTrimsUnconfiguredGroups(t *testing.T) {
	pricing := []model.Pricing{
		{ModelName: "mixed", EnableGroup: []string{"default", "legacy", "vip", "default"}},
		{ModelName: "legacy-only", EnableGroup: []string{"legacy"}},
		{ModelName: "all-groups", EnableGroup: []string{" all ", "legacy"}},
		{ModelName: "trimmed", EnableGroup: []string{" default ", "vip"}},
	}
	usableGroup := map[string]string{
		"default": "Default",
		"vip":     "VIP",
	}

	filtered := filterPricingByUsableGroups(pricing, usableGroup)
	byModel := make(map[string]model.Pricing, len(filtered))
	for _, item := range filtered {
		byModel[item.ModelName] = item
	}

	require.Len(t, filtered, 3)
	require.Equal(t, []string{"default", "vip"}, byModel["mixed"].EnableGroup)
	require.Equal(t, []string{"default", "vip"}, byModel["all-groups"].EnableGroup)
	require.Equal(t, []string{"default", "vip"}, byModel["trimmed"].EnableGroup)
	_, exists := byModel["legacy-only"]
	require.False(t, exists)
}

func TestFilterPricingByUsableGroupsKeepsUsableGroupWithoutRatio(t *testing.T) {
	filtered := filterPricingByUsableGroups(
		[]model.Pricing{
			{ModelName: "legacy-only", EnableGroup: []string{"legacy"}},
		},
		map[string]string{"legacy": "Legacy"},
	)

	require.Len(t, filtered, 1)
	require.Equal(t, "legacy-only", filtered[0].ModelName)
	require.Equal(t, []string{"legacy"}, filtered[0].EnableGroup)
}

func TestPricingResponseGroupRatioDoesNotFilterUsableGroups(t *testing.T) {
	usableGroup := map[string]string{
		"default": "Default",
		"legacy":  "Legacy",
	}
	groupRatio := map[string]float64{
		"default": 1,
		"vip":     2,
	}

	filtered := pricingResponseGroupRatio(groupRatio, usableGroup)

	require.Equal(t, map[string]float64{"default": 1}, filtered)
	require.Equal(t, map[string]string{
		"default": "Default",
		"legacy":  "Legacy",
	}, usableGroup)
}

func TestPricingConfiguredAutoGroupsKeepsOnlyUsableGroups(t *testing.T) {
	filtered := pricingConfiguredAutoGroups(
		[]string{"default", "legacy", "default", ""},
		map[string]string{"default": "Default"},
	)

	require.Equal(t, []string{"default"}, filtered)
}

func TestGetPricingKeepsModelsForUsableGroupWithoutGroupRatio(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	withPricingGroupOptions(t, `{"default":1}`, `{"legacy":"Legacy"}`)

	require.NoError(t, db.Create(&model.Channel{
		Id:     1,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "test-key",
		Status: common.ChannelStatusEnabled,
		Name:   "pricing-test-channel",
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "legacy",
		Model:     "legacy-model",
		ChannelId: 1,
		Enabled:   true,
	}).Error)
	model.InvalidatePricingCache()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/pricing", nil)

	GetPricing(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload pricingAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Equal(t, map[string]string{"legacy": "Legacy"}, payload.UsableGroup)
	require.Empty(t, payload.GroupRatio)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "legacy-model", payload.Data[0].ModelName)
	require.Equal(t, []string{"legacy"}, payload.Data[0].EnableGroup)
}

func TestGetPricingHidesSubscriptionGrantGroupWithoutGrant(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	withPricingGroupOptions(t, `{"default":1,"vip":1}`, `{"default":"Default"}`)

	require.NoError(t, db.Create(&model.User{
		Id:       1001,
		Username: "pricing-user",
		Password: "password",
		Group:    "default",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:     1,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "test-key",
		Status: common.ChannelStatusEnabled,
		Name:   "pricing-test-channel",
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "vip",
		Model:     "vip-model",
		ChannelId: 1,
		Enabled:   true,
	}).Error)
	model.InvalidatePricingCache()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/pricing", nil)
	ctx.Set("id", 1001)

	GetPricing(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload pricingAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Equal(t, map[string]string{"default": "Default"}, payload.UsableGroup)
	require.Equal(t, map[string]float64{"default": 1}, payload.GroupRatio)
	require.Empty(t, payload.Data)
}

func TestGetPricingShowsSubscriptionGrantGroupWithGrantAndRatio(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.UserSubscription{}))
	withPricingGroupOptions(t, `{"default":1,"vip":1}`, `{"default":"Default"}`)

	now := common.GetTimestamp()
	require.NoError(t, db.Create(&model.User{
		Id:       1001,
		Username: "pricing-user",
		Password: "password",
		Group:    "default",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.UserSubscription{
		Id:          701,
		UserId:      1001,
		PlanId:      1,
		StartTime:   now - 60,
		EndTime:     now + 3600,
		Status:      "active",
		Source:      "order",
		GrantGroups: "vip",
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:     1,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "test-key",
		Status: common.ChannelStatusEnabled,
		Name:   "pricing-test-channel",
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "vip",
		Model:     "vip-model",
		ChannelId: 1,
		Enabled:   true,
	}).Error)
	model.InvalidatePricingCache()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/pricing", nil)
	ctx.Set("id", 1001)

	GetPricing(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload pricingAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Equal(t, map[string]string{"default": "Default", "vip": "vip"}, payload.UsableGroup)
	require.Equal(t, map[string]float64{"default": 1, "vip": 1}, payload.GroupRatio)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "vip-model", payload.Data[0].ModelName)
	require.Equal(t, []string{"vip"}, payload.Data[0].EnableGroup)
}

func TestGetPricingHidesSubscriptionGrantGroupWithoutGroupRatio(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.UserSubscription{}))
	withPricingGroupOptions(t, `{"default":1}`, `{"default":"Default"}`)

	now := common.GetTimestamp()
	require.NoError(t, db.Create(&model.User{
		Id:       1001,
		Username: "pricing-user",
		Password: "password",
		Group:    "default",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.UserSubscription{
		Id:          701,
		UserId:      1001,
		PlanId:      1,
		StartTime:   now - 60,
		EndTime:     now + 3600,
		Status:      "active",
		Source:      "order",
		GrantGroups: "legacy",
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:     1,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "test-key",
		Status: common.ChannelStatusEnabled,
		Name:   "pricing-test-channel",
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "legacy",
		Model:     "legacy-model",
		ChannelId: 1,
		Enabled:   true,
	}).Error)
	model.InvalidatePricingCache()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/pricing", nil)
	ctx.Set("id", 1001)

	GetPricing(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload pricingAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Equal(t, map[string]string{"default": "Default"}, payload.UsableGroup)
	require.Equal(t, map[string]float64{"default": 1}, payload.GroupRatio)
	require.Empty(t, payload.Data)
}
