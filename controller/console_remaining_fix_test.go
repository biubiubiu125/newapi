package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

var hanRegexp = regexp.MustCompile(`\p{Han}`)

func serveConsoleLang(
	t *testing.T,
	method, route, requestPath, rawBody, accept string,
	setup gin.HandlerFunc,
	handler gin.HandlerFunc,
) (int, map[string]any) {
	t.Helper()
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.I18n())
	if setup != nil {
		engine.Use(setup)
	}
	engine.Handle(method, route, handler)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, requestPath, strings.NewReader(rawBody))
	if rawBody != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept-Language", accept)
	engine.ServeHTTP(rec, req)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "body=%s", rec.Body.String())
	return rec.Code, body
}

func requireConsoleMessage(t *testing.T, body map[string]any, want string) {
	t.Helper()
	require.Equal(t, false, body["success"])
	require.Equal(t, want, body["message"])
}

func TestRequestAmountFollowsAcceptLanguage(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
	})

	maxAmount := decimal.NewFromInt(common.MaxWalletQuota - 1).
		Div(decimal.NewFromFloat(common.QuotaPerUnit)).
		Floor().IntPart()
	payload := fmt.Sprintf(`{"amount":%d}`, maxAmount+1)

	_, en := serveConsoleI18nJSON(t, http.MethodPost, "/api/user/amount", payload, "en-US", RequestAmount)
	require.Equal(t, "error", en["message"])
	require.Equal(t, fmt.Sprintf("Single top-up amount cannot exceed %d", maxAmount), en["data"])
	require.False(t, hanRegexp.MatchString(fmt.Sprint(en["data"])))

	_, zh := serveConsoleI18nJSON(t, http.MethodPost, "/api/user/amount", payload, "zh-CN", RequestAmount)
	require.Equal(t, "error", zh["message"])
	require.Equal(t, fmt.Sprintf("单笔充值数量不能大于 %d", maxAmount), zh["data"])
}

func TestRequestAmountOverflowFollowsAcceptLanguage(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	oldDB := model.DB
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD

	db, err := gorm.Open(sqlite.Open("file:request-amount-overflow?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	model.DB = db
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
		model.DB = oldDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})

	maxAmount := decimal.NewFromInt(common.MaxWalletQuota - 1).
		Div(decimal.NewFromFloat(common.QuotaPerUnit)).
		Floor().IntPart()
	creditedQuota, err := validateTopUpQuota(maxAmount)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.User{
		Id:       42,
		Username: "topup_capacity_user",
		Quota:    common.MaxWalletQuota - creditedQuota + 1,
		Status:   common.UserStatusEnabled,
	}).Error)

	setup := func(c *gin.Context) {
		c.Set("id", 42)
		c.Next()
	}
	payload := fmt.Sprintf(`{"amount":%d}`, maxAmount)
	_, en := serveConsoleLang(t, http.MethodPost, "/api/user/amount", "/api/user/amount", payload, "en-US", setup, RequestAmount)
	require.Equal(t, "error", en["message"])
	require.Equal(t, "Top-up quota limit exceeded", en["data"])
	require.False(t, hanRegexp.MatchString(fmt.Sprint(en["data"])))

	_, zh := serveConsoleLang(t, http.MethodPost, "/api/user/amount", "/api/user/amount", payload, "zh-CN", setup, RequestAmount)
	require.Equal(t, "error", zh["message"])
	require.Equal(t, "充值额度超出上限", zh["data"])
}

func TestUpdateOptionInvalidJSONFollowsAcceptLanguage(t *testing.T) {
	code, en := serveConsoleI18nJSON(t, http.MethodPut, "/api/option/", `{`, "en-US", UpdateOption)
	require.Equal(t, http.StatusBadRequest, code)
	requireConsoleMessage(t, en, "Invalid parameters")

	zhCode, zh := serveConsoleI18nJSON(t, http.MethodPut, "/api/option/", `{`, "zh-CN", UpdateOption)
	require.Equal(t, http.StatusBadRequest, zhCode)
	requireConsoleMessage(t, zh, "无效的参数")
}

func TestUpdateOptionGitHubOAuthMissingFollowsAcceptLanguage(t *testing.T) {
	originalID := common.GitHubClientId
	originalSecret := common.GitHubClientSecret
	common.GitHubClientId = ""
	common.GitHubClientSecret = ""
	t.Cleanup(func() {
		common.GitHubClientId = originalID
		common.GitHubClientSecret = originalSecret
	})

	body := `{"key":"GitHubOAuthEnabled","value":"true"}`
	_, en := serveConsoleI18nJSON(t, http.MethodPut, "/api/option/", body, "en-US", UpdateOption)
	requireConsoleMessage(t, en, "Cannot enable GitHub OAuth. Please fill in GitHub Client Id and GitHub Client Secret first!")
	require.False(t, hanRegexp.MatchString(fmt.Sprint(en["message"])))

	_, zh := serveConsoleI18nJSON(t, http.MethodPut, "/api/option/", body, "zh-CN", UpdateOption)
	requireConsoleMessage(t, zh, "无法启用 GitHub OAuth，请先填入 GitHub Client Id 以及 GitHub Client Secret！")
}

func TestUploadLogoMissingFileFollowsAcceptLanguage(t *testing.T) {
	code, en := serveConsoleI18nJSON(t, http.MethodPost, "/api/option/logo", "", "en-US", UploadSystemLogo)
	require.Equal(t, http.StatusOK, code)
	requireConsoleMessage(t, en, "Please select an image file")

	zhCode, zh := serveConsoleI18nJSON(t, http.MethodPost, "/api/option/logo", "", "zh-CN", UploadSystemLogo)
	require.Equal(t, http.StatusOK, zhCode)
	requireConsoleMessage(t, zh, "请选择图片文件")
}

func TestGetReferralSummaryDisabledFollowsAcceptLanguage(t *testing.T) {
	original := common.ReferralEnabled
	common.ReferralEnabled = false
	t.Cleanup(func() { common.ReferralEnabled = original })

	_, en := serveConsoleI18nJSON(t, http.MethodGet, "/api/referral/summary", "", "en-US", GetReferralSummary)
	requireConsoleMessage(t, en, "Referral program is not enabled")
	require.False(t, hanRegexp.MatchString(fmt.Sprint(en["message"])))

	_, zh := serveConsoleI18nJSON(t, http.MethodGet, "/api/referral/summary", "", "zh-CN", GetReferralSummary)
	requireConsoleMessage(t, zh, "邀请返利未启用")
}

func TestUploadTaskPluginSha256MismatchFollowsAcceptLanguage(t *testing.T) {
	body := `{"source":"export default {}","sourceSha256":"deadbeef"}`
	_, en := serveConsoleI18nJSON(t, http.MethodPost, "/api/task/plugins", body, "en-US", UploadTaskPlugin)
	requireConsoleMessage(t, en, "Plugin source sha256 mismatch")
	require.False(t, hanRegexp.MatchString(fmt.Sprint(en["message"])))

	_, zh := serveConsoleI18nJSON(t, http.MethodPost, "/api/task/plugins", body, "zh-CN", UploadTaskPlugin)
	requireConsoleMessage(t, zh, "插件源码 SHA256 不匹配")
}

func TestGetAllDeploymentsDisabledFollowsAcceptLanguage(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = map[string]string{}
	}
	oldEnabled := common.OptionMap["model_deployment.ionet.enabled"]
	oldKey := common.OptionMap["model_deployment.ionet.api_key"]
	common.OptionMap["model_deployment.ionet.enabled"] = "false"
	common.OptionMap["model_deployment.ionet.api_key"] = ""
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap["model_deployment.ionet.enabled"] = oldEnabled
		common.OptionMap["model_deployment.ionet.api_key"] = oldKey
		common.OptionMapRWMutex.Unlock()
	})

	_, en := serveConsoleI18nJSON(t, http.MethodGet, "/api/deployments", "", "en-US", GetAllDeployments)
	requireConsoleMessage(t, en, "io.net model deployment is not enabled or API key is missing")

	_, zh := serveConsoleI18nJSON(t, http.MethodGet, "/api/deployments", "", "zh-CN", GetAllDeployments)
	requireConsoleMessage(t, zh, "io.net 模型部署功能未启用或 API 密钥缺失")
}

func TestGetAllFlowQuotaDatesInvalidStartFollowsAcceptLanguage(t *testing.T) {
	_, en := serveConsoleI18nJSON(t, http.MethodGet, "/api/data/flow?start_timestamp=abc&end_timestamp=2", "", "en-US", GetAllFlowQuotaDates)
	requireConsoleMessage(t, en, "Invalid start timestamp")
	require.False(t, hanRegexp.MatchString(fmt.Sprint(en["message"])))

	_, zh := serveConsoleI18nJSON(t, http.MethodGet, "/api/data/flow?start_timestamp=abc&end_timestamp=2", "", "zh-CN", GetAllFlowQuotaDates)
	requireConsoleMessage(t, zh, "无效的起始时间")
}

func TestCleanupLogFilesInvalidModeFollowsAcceptLanguage(t *testing.T) {
	_, en := serveConsoleI18nJSON(t, http.MethodPost, "/api/performance/log-files/cleanup?mode=bad&value=1", "", "en-US", CleanupLogFiles)
	requireConsoleMessage(t, en, "Invalid mode, must be by_count or by_days")
	require.False(t, hanRegexp.MatchString(fmt.Sprint(en["message"])))

	_, zh := serveConsoleI18nJSON(t, http.MethodPost, "/api/performance/log-files/cleanup?mode=bad&value=1", "", "zh-CN", CleanupLogFiles)
	requireConsoleMessage(t, zh, "无效的模式，必须是 by_count 或 by_days")
}

func TestConfirmPaymentComplianceAccessTokenFollowsAcceptLanguage(t *testing.T) {
	setup := func(c *gin.Context) {
		c.Set("use_access_token", true)
		c.Next()
	}
	code, en := serveConsoleLang(t, http.MethodPost, "/api/payment/compliance/confirm", "/api/payment/compliance/confirm", `{"confirmed":true}`, "en-US", setup, ConfirmPaymentCompliance)
	require.Equal(t, http.StatusForbidden, code)
	requireConsoleMessage(t, en, "This operation requires dashboard session authentication. API access token is not allowed.")

	code, zh := serveConsoleLang(t, http.MethodPost, "/api/payment/compliance/confirm", "/api/payment/compliance/confirm", `{"confirmed":true}`, "zh-CN", setup, ConfirmPaymentCompliance)
	require.Equal(t, http.StatusForbidden, code)
	requireConsoleMessage(t, zh, "此操作需要控制台会话登录，不允许使用 API 访问令牌")
}

func TestRequestCreemPayReadBodyFollowsAcceptLanguage(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalKey := setting.CreemApiKey
	originalProducts := setting.CreemProducts
	originalSecret := setting.CreemWebhookSecret
	setting.CreemApiKey = "ck_test"
	setting.CreemProducts = `[{"productId":"prod"}]`
	setting.CreemWebhookSecret = "whsec"
	t.Cleanup(func() {
		setting.CreemApiKey = originalKey
		setting.CreemProducts = originalProducts
		setting.CreemWebhookSecret = originalSecret
	})

	run := func(accept string) map[string]any {
		require.NoError(t, i18n.Init())
		gin.SetMode(gin.TestMode)
		engine := gin.New()
		engine.Use(middleware.I18n(), func(c *gin.Context) {
			c.Set("id", 1)
			c.Next()
		})
		engine.POST("/api/user/pay_creem", func(c *gin.Context) {
			c.Request.Body = io.NopCloser(errReader{})
			RequestCreemPay(c)
		})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/user/pay_creem", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept-Language", accept)
		engine.ServeHTTP(rec, req)
		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "body=%s", rec.Body.String())
		return body
	}

	en := run("en-US")
	require.Equal(t, "error", en["message"])
	require.Equal(t, "Failed to read request", en["data"])
	require.False(t, hanRegexp.MatchString(fmt.Sprint(en["data"])))

	zh := run("zh-CN")
	require.Equal(t, "error", zh["message"])
	require.Equal(t, "读取请求失败", zh["data"])
}

func TestFetchModelsInnerErrorHasNoChinese(t *testing.T) {
	oldDB := model.DB
	db, err := gorm.Open(sqlite.Open("file:fetch-models-inner?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}))
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})

	channel := model.Channel{
		Type:        constant.ChannelTypeOllama,
		Name:        "ollama-empty-keys",
		Key:         "",
		Status:      common.ChannelStatusEnabled,
		ChannelInfo: model.ChannelInfo{IsMultiKey: true},
	}
	require.NoError(t, db.Create(&channel).Error)

	body := fmt.Sprintf(`{"id":%d}`, channel.Id)
	_, en := serveConsoleI18nJSON(t, http.MethodPost, "/api/channel/fetch_models", body, "en-US", FetchModels)
	require.Equal(t, false, en["success"])
	message := fmt.Sprint(en["message"])
	require.Contains(t, message, "Failed to get model list")
	require.NotContains(t, message, "获取渠道密钥失败")
	require.False(t, hanRegexp.MatchString(message))

	_, zh := serveConsoleI18nJSON(t, http.MethodPost, "/api/channel/fetch_models", body, "zh-CN", FetchModels)
	require.Equal(t, false, zh["success"])
	zhMessage := fmt.Sprint(zh["message"])
	require.Contains(t, zhMessage, "获取模型列表失败")
	require.NotContains(t, zhMessage, "获取渠道密钥失败")
}

func TestLockedChannelUpstreamInvalidIDIsEnglish(t *testing.T) {
	err := withLockedChannelUpstreamModelUpdateContext(context.Background(), 0, "task", "runner", func(*gorm.DB, *model.Channel) error {
		return nil
	})
	require.EqualError(t, err, "invalid channel id")
	require.False(t, hanRegexp.MatchString(err.Error()))
}

func TestChannelResponseTimeExceededErrorIsEnglish(t *testing.T) {
	err := channelResponseTimeExceededError(1500, 1000)
	require.EqualError(t, err, "response time 1.50s exceeds threshold 1.00s")
	require.False(t, hanRegexp.MatchString(err.Error()))
}

func TestAdminCreateUserSubscriptionGroupMessageFollowsAcceptLanguage(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedisEnabled })
	oldDB := model.DB
	oldLogDB := model.LOG_DB
	db, err := gorm.Open(sqlite.Open("file:admin-bind-group?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.UserSubscription{}))
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})

	user := model.User{
		Username: "sub-group-user",
		Password: "unused-password-hash",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, db.Create(&user).Error)
	plan := &model.SubscriptionPlan{
		Title:         "Pro plan",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		TotalAmount:   100,
		UpgradeGroup:  "pro",
		Enabled:       true,
	}
	require.NoError(t, db.Create(plan).Error)

	setup := func(c *gin.Context) { c.Next() }
	path := fmt.Sprintf("/api/user/%d/subscription", user.Id)
	payload := fmt.Sprintf(`{"plan_id":%d}`, plan.Id)
	_, en := serveConsoleLang(t, http.MethodPost, "/api/user/:id/subscription", path, payload, "en-US", setup, AdminCreateUserSubscription)
	require.Equal(t, true, en["success"])
	enData, ok := en["data"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "User group will be upgraded to pro", enData["message"])
	require.False(t, hanRegexp.MatchString(fmt.Sprint(enData["message"])))

	user2 := model.User{
		Username: "sub-group-user-zh",
		Password: "unused-password-hash",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, db.Create(&user2).Error)
	zhPath := fmt.Sprintf("/api/user/%d/subscription", user2.Id)
	_, zh := serveConsoleLang(t, http.MethodPost, "/api/user/:id/subscription", zhPath, payload, "zh-CN", setup, AdminCreateUserSubscription)
	require.Equal(t, true, zh["success"])
	zhData, ok := zh["data"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "用户分组将升级到 pro", zhData["message"])
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }
func (errReader) Close() error             { return nil }

func TestTransferAffQuotaFollowsAcceptLanguage(t *testing.T) {
	_, en := serveConsoleI18nJSON(t, http.MethodPost, "/api/user/self/aff_transfer", "", "en-US", TransferAffQuota)
	requireConsoleMessage(t, en, "Legacy affiliate quota transfer is deprecated")
	require.False(t, hanRegexp.MatchString(fmt.Sprint(en["message"])))

	_, zh := serveConsoleI18nJSON(t, http.MethodPost, "/api/user/self/aff_transfer", "", "zh-CN", TransferAffQuota)
	requireConsoleMessage(t, zh, "旧版邀请额度划转接口已停用")
}

func TestGetAffCodeFollowsAcceptLanguage(t *testing.T) {
	_, en := serveConsoleI18nJSON(t, http.MethodGet, "/api/user/aff", "", "en-US", GetAffCode)
	requireConsoleMessage(t, en, "Legacy affiliate code endpoint is deprecated")

	_, zh := serveConsoleI18nJSON(t, http.MethodGet, "/api/user/aff", "", "zh-CN", GetAffCode)
	requireConsoleMessage(t, zh, "旧版邀请码接口已停用")
}

func TestGetAdminReferralCommissionsInvalidAffiliateIDFollowsAcceptLanguage(t *testing.T) {
	_, en := serveConsoleLang(t, http.MethodGet, "/api/user/admin/referral/commissions", "/api/user/admin/referral/commissions?affiliate_user_id=abc", "", "en-US", nil, GetAdminReferralCommissions)
	requireConsoleMessage(t, en, "Invalid affiliate user ID")
	require.False(t, hanRegexp.MatchString(fmt.Sprint(en["message"])))

	_, zh := serveConsoleLang(t, http.MethodGet, "/api/user/admin/referral/commissions", "/api/user/admin/referral/commissions?affiliate_user_id=abc", "", "zh-CN", nil, GetAdminReferralCommissions)
	requireConsoleMessage(t, zh, "无效的推广用户 ID")
}

func TestApproveReferralAffiliateInvalidIDFollowsAcceptLanguage(t *testing.T) {
	_, en := serveConsoleLang(t, http.MethodPost, "/api/user/admin/referral/affiliates/:user_id/approve", "/api/user/admin/referral/affiliates/abc/approve", "{}", "en-US", nil, ApproveReferralAffiliate)
	requireConsoleMessage(t, en, "Invalid ID")

	_, zh := serveConsoleLang(t, http.MethodPost, "/api/user/admin/referral/affiliates/:user_id/approve", "/api/user/admin/referral/affiliates/abc/approve", "{}", "zh-CN", nil, ApproveReferralAffiliate)
	requireConsoleMessage(t, zh, "无效的ID")
}

func TestDeleteStaleSystemInstanceRequiresNodeName(t *testing.T) {
	_, en := serveConsoleI18nJSON(t, http.MethodDelete, "/api/system/instances", "", "en-US", DeleteStaleSystemInstance)
	requireConsoleMessage(t, en, "Node name is required")
	require.False(t, hanRegexp.MatchString(fmt.Sprint(en["message"])))

	_, zh := serveConsoleI18nJSON(t, http.MethodDelete, "/api/system/instances", "", "zh-CN", DeleteStaleSystemInstance)
	requireConsoleMessage(t, zh, "节点名称不能为空")
}

func TestFetchUpstreamModelsInvalidIDFollowsAcceptLanguage(t *testing.T) {
	_, en := serveConsoleLang(t, http.MethodGet, "/api/channel/fetch_models/:id", "/api/channel/fetch_models/abc", "", "en-US", nil, FetchUpstreamModels)
	requireConsoleMessage(t, en, "Channel ID format error")
	require.False(t, hanRegexp.MatchString(fmt.Sprint(en["message"])))

	_, zh := serveConsoleLang(t, http.MethodGet, "/api/channel/fetch_models/:id", "/api/channel/fetch_models/abc", "", "zh-CN", nil, FetchUpstreamModels)
	requireConsoleMessage(t, zh, "渠道ID格式错误")
}

func TestUpdateOptionTaskPublicAddressFollowsAcceptLanguage(t *testing.T) {
	payload := `{"key":"TaskPublicAddress","value":"ftp://media.example.com/tasks"}`
	_, en := serveConsoleI18nJSON(t, http.MethodPut, "/api/option/", payload, "en-US", UpdateOption)
	requireConsoleMessage(t, en, "Task public address must use http or https")
	require.False(t, hanRegexp.MatchString(fmt.Sprint(en["message"])))

	_, zh := serveConsoleI18nJSON(t, http.MethodPut, "/api/option/", payload, "zh-CN", UpdateOption)
	requireConsoleMessage(t, zh, "任务公开地址必须使用 http 或 https")
}

func TestWriteTopUpClientErrorMapsInvalidQuota(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)

	en := writeTopUpClientErrorJSON(t, "en-US", model.ErrInvalidTopUpQuota)
	require.Equal(t, "error", en["message"])
	require.Equal(t, "Invalid top-up quota", en["data"])
	require.False(t, hanRegexp.MatchString(fmt.Sprint(en["data"])))

	zh := writeTopUpClientErrorJSON(t, "zh-CN", model.ErrInvalidTopUpQuota)
	require.Equal(t, "error", zh["message"])
	require.Equal(t, "无效的充值额度", zh["data"])
}

func TestUploadReferralAssetInvalidPurposeFollowsAcceptLanguage(t *testing.T) {
	previous := common.ReferralEnabled
	common.ReferralEnabled = true
	t.Cleanup(func() { common.ReferralEnabled = previous })

	en := serveReferralAssetUpload(t, "/api/user/referral/upload", "payment_proof", "en-US")
	requireConsoleMessage(t, en, "Invalid referral asset purpose")
	require.False(t, hanRegexp.MatchString(fmt.Sprint(en["message"])))

	zh := serveReferralAssetUpload(t, "/api/user/referral/upload", "payment_proof", "zh-CN")
	requireConsoleMessage(t, zh, "无效的邀请素材用途")
}

func writeTopUpClientErrorJSON(t *testing.T, accept string, err error) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/api/user/pay", nil)
	req.Header.Set("Accept-Language", accept)
	c.Request = req
	middleware.I18n()(c)
	writeTopUpClientError(c, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "body=%s", rec.Body.String())
	return body
}

func serveReferralAssetUpload(t *testing.T, path, purpose, accept string) map[string]any {
	t.Helper()
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	require.NoError(t, writer.WriteField("purpose", purpose))
	require.NoError(t, writer.Close())

	engine := gin.New()
	engine.Use(middleware.I18n())
	engine.POST(path, UploadReferralAsset)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept-Language", accept)
	engine.ServeHTTP(rec, req)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload), "body=%s", rec.Body.String())
	return payload
}

func TestSearchModelsMetaInvalidSquareStateFollowsAcceptLanguage(t *testing.T) {
	code, en := serveConsoleI18nJSON(t, http.MethodGet, "/api/models/search?square_state=unknown", "", "en-US", SearchModelsMeta)
	require.Equal(t, http.StatusBadRequest, code)
	requireConsoleMessage(t, en, "Invalid model square state")
	require.False(t, hanRegexp.MatchString(fmt.Sprint(en["message"])))

	zhCode, zh := serveConsoleI18nJSON(t, http.MethodGet, "/api/models/search?square_state=unknown", "", "zh-CN", SearchModelsMeta)
	require.Equal(t, http.StatusBadRequest, zhCode)
	requireConsoleMessage(t, zh, "无效的模型广场状态")
}

func TestDeleteModelMetaPricingRootOnlyFollowsAcceptLanguage(t *testing.T) {
	setup := func(c *gin.Context) {
		c.Set("role", common.RoleAdminUser)
		c.Next()
	}
	code, en := serveConsoleLang(t, http.MethodDelete, "/api/models/:id", "/api/models/1?remove_pricing=true", "", "en-US", setup, DeleteModelMeta)
	require.Equal(t, http.StatusForbidden, code)
	requireConsoleMessage(t, en, "Model pricing is managed by a super administrator.")
	require.False(t, hanRegexp.MatchString(fmt.Sprint(en["message"])))

	zhCode, zh := serveConsoleLang(t, http.MethodDelete, "/api/models/:id", "/api/models/1?remove_pricing=true", "", "zh-CN", setup, DeleteModelMeta)
	require.Equal(t, http.StatusForbidden, zhCode)
	requireConsoleMessage(t, zh, "模型定价由超级管理员管理")
}

func TestGetChannelAffinityUsageMissingParamsFollowsAcceptLanguage(t *testing.T) {
	_, en := serveConsoleI18nJSON(t, http.MethodGet, "/api/channel/affinity/usage", "", "en-US", GetChannelAffinityUsageCacheStats)
	requireConsoleMessage(t, en, "Missing param: rule_name")
	require.False(t, hanRegexp.MatchString(fmt.Sprint(en["message"])))

	_, zh := serveConsoleI18nJSON(t, http.MethodGet, "/api/channel/affinity/usage", "", "zh-CN", GetChannelAffinityUsageCacheStats)
	requireConsoleMessage(t, zh, "缺少参数：rule_name")

	_, enKey := serveConsoleI18nJSON(t, http.MethodGet, "/api/channel/affinity/usage?rule_name=demo", "", "en-US", GetChannelAffinityUsageCacheStats)
	requireConsoleMessage(t, enKey, "Missing param: key_fp")

	_, zhKey := serveConsoleI18nJSON(t, http.MethodGet, "/api/channel/affinity/usage?rule_name=demo", "", "zh-CN", GetChannelAffinityUsageCacheStats)
	requireConsoleMessage(t, zhKey, "缺少参数：key_fp")
}

func TestUploadTaskPluginInvalidJSONFollowsAcceptLanguage(t *testing.T) {
	_, en := serveConsoleI18nJSON(t, http.MethodPost, "/api/task/plugins", `{`, "en-US", UploadTaskPlugin)
	requireConsoleMessage(t, en, "Invalid parameters")
	require.False(t, hanRegexp.MatchString(fmt.Sprint(en["message"])))

	_, zh := serveConsoleI18nJSON(t, http.MethodPost, "/api/task/plugins", `{`, "zh-CN", UploadTaskPlugin)
	requireConsoleMessage(t, zh, "无效的参数")
}

func TestSetTaskPluginStatusStillInUseFollowsAcceptLanguage(t *testing.T) {
	oldDB := model.DB
	db, err := gorm.Open(sqlite.Open("file:task-plugin-still-in-use?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Task{}))
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})

	setting := `{"task_plugin_key":"still-in-use"}`
	require.NoError(t, db.Create(&model.Channel{
		Type:    constant.ChannelTypeTaskPlugin,
		Name:    "plugin-channel",
		Key:     "plugin-key",
		Status:  common.ChannelStatusEnabled,
		Setting: &setting,
	}).Error)

	_, en := serveConsoleLang(t, http.MethodPut, "/api/task/plugins/:key/status", "/api/task/plugins/still-in-use/status", `{"enabled":false}`, "en-US", nil, SetTaskPluginStatus)
	requireConsoleMessage(t, en, "Task plugin is still in use")
	require.False(t, hanRegexp.MatchString(fmt.Sprint(en["message"])))

	_, zh := serveConsoleLang(t, http.MethodPut, "/api/task/plugins/:key/status", "/api/task/plugins/still-in-use/status", `{"enabled":false}`, "zh-CN", nil, SetTaskPluginStatus)
	requireConsoleMessage(t, zh, "任务插件仍在使用中")
}

func TestFetchUpstreamRatiosParseFailedFollowsAcceptLanguage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":123}`))
	}))
	t.Cleanup(server.Close)

	body := fmt.Sprintf(`{"upstreams":[{"id":1,"name":"Broken","base_url":%q,"endpoint":"/pricing"}]}`, server.URL)
	_, en := serveConsoleI18nJSON(t, http.MethodPost, "/api/ratio_sync/fetch", body, "en-US", FetchUpstreamRatios)
	require.Equal(t, true, en["success"])
	require.Equal(t, "Unable to parse upstream response", firstRatioSyncTestError(t, en))
	require.False(t, hanRegexp.MatchString(firstRatioSyncTestError(t, en)))

	_, zh := serveConsoleI18nJSON(t, http.MethodPost, "/api/ratio_sync/fetch", body, "zh-CN", FetchUpstreamRatios)
	require.Equal(t, true, zh["success"])
	require.Equal(t, "无法解析上游返回数据", firstRatioSyncTestError(t, zh))
}

func firstRatioSyncTestError(t *testing.T, body map[string]any) string {
	t.Helper()
	data, ok := body["data"].(map[string]any)
	require.True(t, ok, "data=%v", body["data"])
	results, ok := data["test_results"].([]any)
	require.NotEmpty(t, results)
	first, ok := results[0].(map[string]any)
	require.True(t, ok)
	return fmt.Sprint(first["error"])
}

func TestUpdateSelfPasswordErrorsFollowAcceptLanguage(t *testing.T) {
	db := setupManageUserTestDB(t)

	hashed, err := common.Password2Hash("CurrentPassword123")
	require.NoError(t, err)
	withPassword := model.User{
		Username: "pwd-user",
		Password: hashed,
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, db.Create(&withPassword).Error)

	unset := model.User{
		Username: "pwd-unset-user",
		Password: "",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, db.Create(&unset).Error)

	run := func(userID int, body, accept string) map[string]any {
		setup := func(c *gin.Context) {
			c.Set("id", userID)
			c.Next()
		}
		_, payload := serveConsoleLang(t, http.MethodPut, "/api/user/self", "/api/user/self", body, accept, setup, UpdateSelf)
		return payload
	}

	requireConsoleMessage(t, run(withPassword.Id, `{"password":"NewPassword123","original_password":"wrong-pass"}`, "zh-CN"), "原密码错误")
	requireConsoleMessage(t, run(withPassword.Id, `{"password":"NewPassword123","original_password":"wrong-pass"}`, "en-US"), "Original password is incorrect")
	requireConsoleMessage(t, run(unset.Id, `{"password":"NewPassword123"}`, "zh-CN"), "当前账号未设置密码，请使用密码重置或联系管理员重置密码")
	requireConsoleMessage(t, run(unset.Id, `{"password":"NewPassword123"}`, "en-US"), "This account has no password set. Please use password reset or contact an administrator to reset it.")
}

func TestEmailBindInvalidJSONFollowsAcceptLanguage(t *testing.T) {
	_, en := serveConsoleI18nJSON(t, http.MethodPost, "/api/oauth/email/bind", `{`, "en-US", EmailBind)
	requireConsoleMessage(t, en, "Invalid parameters")

	_, zh := serveConsoleI18nJSON(t, http.MethodPost, "/api/oauth/email/bind", `{`, "zh-CN", EmailBind)
	requireConsoleMessage(t, zh, "无效的参数")
}

func TestSubscriptionBalancePayPurchaseLimitFollowsAcceptLanguage(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	oldRedisEnabled := common.RedisEnabled
	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL
	common.RedisEnabled = false
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
	})

	oldDB, oldLogDB := model.DB, model.LOG_DB
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.SubscriptionPlan{},
		&model.UserSubscription{},
		&model.SubscriptionOrder{},
		&model.Log{},
	))
	model.DB, model.LOG_DB = db, db
	const planID = 98017
	model.InvalidateSubscriptionPlanCache(planID)
	t.Cleanup(func() {
		model.InvalidateSubscriptionPlanCache(planID)
		model.DB, model.LOG_DB = oldDB, oldLogDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})

	user := model.User{
		Id:       98017,
		Username: "sub-limit-user",
		Password: "unused-password-hash",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    1_000_000,
	}
	require.NoError(t, db.Create(&user).Error)
	plan := &model.SubscriptionPlan{
		Id:                 planID,
		Title:              "Limited plan",
		PriceAmount:        0,
		DurationUnit:       model.SubscriptionDurationMonth,
		DurationValue:      1,
		Enabled:            true,
		MaxPurchasePerUser: 1,
		AllowBalancePay:    common.GetPointer(true),
	}
	require.NoError(t, db.Create(plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	require.NoError(t, db.Create(&model.UserSubscription{
		UserId: user.Id,
		PlanId: plan.Id,
		Status: "active",
	}).Error)

	setup := func(c *gin.Context) {
		c.Set("id", user.Id)
		c.Next()
	}
	body := fmt.Sprintf(`{"plan_id":%d}`, plan.Id)
	_, zh := serveConsoleLang(t, http.MethodPost, "/api/user/subscription/pay/balance", "/api/user/subscription/pay/balance", body, "zh-CN", setup, SubscriptionRequestBalancePay)
	requireConsoleMessage(t, zh, "已达到该套餐购买上限")
	_, en := serveConsoleLang(t, http.MethodPost, "/api/user/subscription/pay/balance", "/api/user/subscription/pay/balance", body, "en-US", setup, SubscriptionRequestBalancePay)
	requireConsoleMessage(t, en, "Purchase limit for this plan has been reached")
}
