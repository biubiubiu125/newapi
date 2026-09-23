package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

func serveConsoleI18n(t *testing.T, method, path string, handler gin.HandlerFunc, accept string) map[string]any {
	t.Helper()
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.I18n())
	engine.Handle(method, path, handler)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Accept-Language", accept)
	engine.ServeHTTP(rec, req)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "body=%s", rec.Body.String())
	return body
}

func TestPasskeyRegisterBeginDisabledFollowsAcceptLanguage(t *testing.T) {
	restore := system_setting.OverridePasskeySettingsForTest(system_setting.PasskeySettings{Enabled: false})
	t.Cleanup(restore)

	zh := serveConsoleI18n(t, http.MethodPost, "/api/user/passkey/register/begin", PasskeyRegisterBegin, "zh-CN")
	require.Equal(t, false, zh["success"])
	require.Equal(t, "管理员未启用 Passkey 登录", zh["message"])

	en := serveConsoleI18n(t, http.MethodPost, "/api/user/passkey/register/begin", PasskeyRegisterBegin, "en-US")
	require.Equal(t, false, en["success"])
	require.Equal(t, "Passkey login has not been enabled by administrator", en["message"])
}

func TestPasskeyStatusRequiresLoginFollowsAcceptLanguage(t *testing.T) {
	restore := system_setting.OverridePasskeySettingsForTest(system_setting.PasskeySettings{Enabled: true})
	t.Cleanup(restore)

	en := serveConsoleI18n(t, http.MethodGet, "/api/user/passkey", PasskeyStatus, "en-US")
	require.Equal(t, false, en["success"])
	require.Equal(t, "Please sign in first", en["message"])
}

func TestGetCustomOAuthProviderInvalidIdFollowsAcceptLanguage(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	run := func(accept string) map[string]any {
		engine := gin.New()
		engine.Use(middleware.I18n())
		engine.GET("/api/custom-oauth-provider/:id", GetCustomOAuthProvider)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/custom-oauth-provider/abc", nil)
		req.Header.Set("Accept-Language", accept)
		engine.ServeHTTP(rec, req)
		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "body=%s", rec.Body.String())
		return body
	}

	zh := run("zh-CN")
	require.Equal(t, false, zh["success"])
	require.Equal(t, "无效的ID", zh["message"])

	en := run("en-US")
	require.Equal(t, false, en["success"])
	require.Equal(t, "Invalid ID", en["message"])
}

func TestGetUserOAuthBindingsRequiresLoginFollowsAcceptLanguage(t *testing.T) {
	en := serveConsoleI18n(t, http.MethodGet, "/api/user/oauth/bindings", GetUserOAuthBindings, "en-US")
	require.Equal(t, false, en["success"])
	require.Equal(t, "Please sign in first", en["message"])
}

func TestFetchCustomOAuthDiscoveryRequiresURLFollowsAcceptLanguage(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.I18n())
	engine.POST("/api/custom-oauth-provider/discovery", FetchCustomOAuthDiscovery)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/custom-oauth-provider/discovery", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", "en-US")
	engine.ServeHTTP(rec, req)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "body=%s", rec.Body.String())
	require.Equal(t, false, body["success"])
	require.Equal(t, "Please enter a Discovery URL or Issuer URL first", body["message"])
}

func TestGetCheckinStatusDisabledFollowsAcceptLanguage(t *testing.T) {
	zh := serveConsoleI18n(t, http.MethodGet, "/api/user/checkin", GetCheckinStatus, "zh-CN")
	require.Equal(t, false, zh["success"])
	require.Equal(t, "签到功能未启用", zh["message"])

	en := serveConsoleI18n(t, http.MethodGet, "/api/user/checkin", GetCheckinStatus, "en-US")
	require.Equal(t, false, en["success"])
	require.Equal(t, "Check-in feature is not enabled", en["message"])
}

func TestGetRechargeAuditInvalidStatusFollowsAcceptLanguage(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	run := func(accept string) map[string]any {
		engine := gin.New()
		engine.Use(middleware.I18n())
		engine.GET("/api/recharge-audit", GetRechargeAudit)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/recharge-audit?status=bogus", nil)
		req.Header.Set("Accept-Language", accept)
		engine.ServeHTTP(rec, req)
		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "body=%s", rec.Body.String())
		return body
	}

	zh := run("zh-CN")
	require.Equal(t, false, zh["success"])
	require.Equal(t, "无效的参数", zh["message"])

	en := run("en-US")
	require.Equal(t, false, en["success"])
	require.Equal(t, "Invalid parameters", en["message"])
}

func TestSendEmailVerificationInvalidParamsFollowsAcceptLanguage(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.I18n())
	engine.GET("/api/verification", SendEmailVerification)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/verification?email=not-an-email", nil)
	req.Header.Set("Accept-Language", "en-US")
	engine.ServeHTTP(rec, req)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "body=%s", rec.Body.String())
	require.Equal(t, false, body["success"])
	require.Equal(t, "Invalid parameters", body["message"])
}

func TestResetPasswordInvalidParamsFollowsAcceptLanguage(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.I18n())
	engine.POST("/api/user/reset", ResetPassword)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/user/reset", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", "en-US")
	engine.ServeHTTP(rec, req)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "body=%s", rec.Body.String())
	require.Equal(t, false, body["success"])
	require.Equal(t, "Invalid parameters", body["message"])
}

func serveConsoleI18nJSON(t *testing.T, method, path, rawBody, accept string, handler gin.HandlerFunc) (int, map[string]any) {
	t.Helper()
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.I18n())
	route := path
	if idx := strings.Index(path, "?"); idx >= 0 {
		route = path[:idx]
	}
	engine.Handle(method, route, handler)
	rec := httptest.NewRecorder()
	var bodyReader *strings.Reader
	if rawBody == "" {
		bodyReader = strings.NewReader("")
	} else {
		bodyReader = strings.NewReader(rawBody)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	if rawBody != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept-Language", accept)
	engine.ServeHTTP(rec, req)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "body=%s", rec.Body.String())
	return rec.Code, body
}

func TestFetchModelsInvalidJSONFollowsAcceptLanguage(t *testing.T) {
	code, en := serveConsoleI18nJSON(t, http.MethodPost, "/api/channel/fetch_models", "{", "en-US", FetchModels)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, false, en["success"])
	require.Equal(t, "Invalid parameters", en["message"])

	zhCode, zh := serveConsoleI18nJSON(t, http.MethodPost, "/api/channel/fetch_models", "{", "zh-CN", FetchModels)
	require.Equal(t, http.StatusOK, zhCode)
	require.Equal(t, false, zh["success"])
	require.Equal(t, "无效的参数", zh["message"])
}

func TestAddChannelEmptyFollowsAcceptLanguage(t *testing.T) {
	_, en := serveConsoleI18nJSON(t, http.MethodPost, "/api/channel/", `{"mode":"single"}`, "en-US", AddChannel)
	require.Equal(t, false, en["success"])
	require.Equal(t, "Channel cannot be empty", en["message"])

	_, zh := serveConsoleI18nJSON(t, http.MethodPost, "/api/channel/", `{"mode":"single"}`, "zh-CN", AddChannel)
	require.Equal(t, false, zh["success"])
	require.Equal(t, "渠道不能为空", zh["message"])
}

func TestFetchModelsMissingChannelFollowsAcceptLanguage(t *testing.T) {
	_ = setupModelListControllerTestDB(t)

	_, en := serveConsoleI18nJSON(t, http.MethodPost, "/api/channel/fetch_models", `{"id":999}`, "en-US", FetchModels)
	require.Equal(t, false, en["success"])
	require.Equal(t, "Failed to get channel information, please try again later", en["message"])

	_, zh := serveConsoleI18nJSON(t, http.MethodPost, "/api/channel/fetch_models", `{"id":999}`, "zh-CN", FetchModels)
	require.Equal(t, false, zh["success"])
	require.Equal(t, "获取渠道信息失败，请稍后重试", zh["message"])
}

func TestFetchModelsCodexMultiKeyDraftFollowsAcceptLanguage(t *testing.T) {
	_, en := serveConsoleI18nJSON(t, http.MethodPost, "/api/channel/fetch_models", `{"type":57,"key":"a\nb"}`, "en-US", FetchModels)
	require.Equal(t, false, en["success"])
	require.Equal(t, "Codex channels do not support fetching models with multiple draft keys", en["message"])

	_, zh := serveConsoleI18nJSON(t, http.MethodPost, "/api/channel/fetch_models", `{"type":57,"key":"a\nb"}`, "zh-CN", FetchModels)
	require.Equal(t, false, zh["success"])
	require.Equal(t, "Codex 渠道拉取模型不支持多 Key 草稿", zh["message"])
}

func TestFetchModelsInvalidTypeFollowsAcceptLanguage(t *testing.T) {
	_, en := serveConsoleI18nJSON(t, http.MethodPost, "/api/channel/fetch_models", `{}`, "en-US", FetchModels)
	require.Equal(t, false, en["success"])
	require.Equal(t, "Invalid channel type", en["message"])

	_, zh := serveConsoleI18nJSON(t, http.MethodPost, "/api/channel/fetch_models", `{}`, "zh-CN", FetchModels)
	require.Equal(t, false, zh["success"])
	require.Equal(t, "无效的渠道类型", zh["message"])
}

func TestStatusRunningFollowsAcceptLanguage(t *testing.T) {
	_ = setupModelListControllerTestDB(t)

	zh := serveConsoleI18n(t, http.MethodGet, "/api/status/test", TestStatus, "zh-CN")
	require.Equal(t, true, zh["success"])
	require.Equal(t, "服务器运行中", zh["message"])

	en := serveConsoleI18n(t, http.MethodGet, "/api/status/test", TestStatus, "en-US")
	require.Equal(t, true, en["success"])
	require.Equal(t, "Server is running", en["message"])
}

func TestUpdateOptionInvalidAPIInfoFollowsAcceptLanguage(t *testing.T) {
	body := `{"key":"console_setting.api_info","value":"not-json"}`
	_, en := serveConsoleI18nJSON(t, http.MethodPut, "/api/option/", body, "en-US", UpdateOption)
	require.Equal(t, false, en["success"])
	require.Contains(t, en["message"], "API information JSON is invalid")
	require.NotContains(t, en["message"], "格式错误")

	_, zh := serveConsoleI18nJSON(t, http.MethodPut, "/api/option/", body, "zh-CN", UpdateOption)
	require.Equal(t, false, zh["success"])
	require.Equal(t, "API信息格式错误", zh["message"])
	require.NotContains(t, zh["message"], "invalid character")
}

func TestSendEmailVerificationSMTPFailureFollowsAcceptLanguage(t *testing.T) {
	_ = setupModelListControllerTestDB(t)
	originalServer := common.SMTPServer
	originalAccount := common.SMTPAccount
	originalFrom := common.SMTPFrom
	common.SMTPServer = ""
	common.SMTPAccount = ""
	common.SMTPFrom = ""
	t.Cleanup(func() {
		common.SMTPServer = originalServer
		common.SMTPAccount = originalAccount
		common.SMTPFrom = originalFrom
	})

	_, en := serveConsoleI18nJSON(t, http.MethodGet, "/api/verification?email=locale-smtp@example.com", "", "en-US", SendEmailVerification)
	require.Equal(t, false, en["success"])
	require.Equal(t, "Failed to send email, please try again later", en["message"])
	require.NotEqual(t, "SMTP 服务器未配置", en["message"])

	_, zh := serveConsoleI18nJSON(t, http.MethodGet, "/api/verification?email=locale-smtp@example.com", "", "zh-CN", SendEmailVerification)
	require.Equal(t, false, zh["success"])
	require.Equal(t, "邮件发送失败，请稍后重试", zh["message"])
}

func TestVerificationEmailRendersRequestLanguage(t *testing.T) {
	require.NoError(t, i18n.Init())
	subject, content := renderVerificationEmail("en", "NewAPI", "123456", 10)
	require.Contains(t, subject, "email verification")
	require.NotContains(t, subject, "邮箱")
	require.Contains(t, content, "123456")
	require.Contains(t, content, "NewAPI")
	require.NotContains(t, content, "您好")

	zhSubject, zhContent := renderVerificationEmail("zh-CN", "NewAPI", "123456", 10)
	require.Contains(t, zhSubject, "邮箱验证")
	require.Contains(t, zhContent, "验证码")
}

func TestPostSetupAlreadyInitializedFollowsAcceptLanguage(t *testing.T) {
	original := constant.Setup
	constant.Setup = true
	t.Cleanup(func() { constant.Setup = original })

	_, en := serveConsoleI18nJSON(t, http.MethodPost, "/api/setup", `{}`, "en-US", PostSetup)
	require.Equal(t, false, en["success"])
	require.Equal(t, "System has already been initialized", en["message"])

	_, zh := serveConsoleI18nJSON(t, http.MethodPost, "/api/setup", `{}`, "zh-CN", PostSetup)
	require.Equal(t, false, zh["success"])
	require.Equal(t, "系统已经初始化完成", zh["message"])
}

func TestAdminCreateSubscriptionPlanEmptyTitleFollowsAcceptLanguage(t *testing.T) {
	payment := operation_setting.GetPaymentSetting()
	original := *payment
	payment.ComplianceConfirmed = true
	payment.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	t.Cleanup(func() { *payment = original })

	body := `{"plan":{"title":"","price_amount":1}}`
	_, en := serveConsoleI18nJSON(t, http.MethodPost, "/api/subscription/admin/plan", body, "en-US", AdminCreateSubscriptionPlan)
	require.Equal(t, false, en["success"])
	require.Equal(t, "Subscription plan title cannot be empty", en["message"])

	_, zh := serveConsoleI18nJSON(t, http.MethodPost, "/api/subscription/admin/plan", body, "zh-CN", AdminCreateSubscriptionPlan)
	require.Equal(t, false, zh["success"])
	require.Equal(t, "套餐标题不能为空", zh["message"])
}

func TestCreateVendorMetaEmptyNameFollowsAcceptLanguage(t *testing.T) {
	_, en := serveConsoleI18nJSON(t, http.MethodPost, "/api/vendors", `{"name":""}`, "en-US", CreateVendorMeta)
	require.Equal(t, false, en["success"])
	require.Equal(t, "Vendor name cannot be empty", en["message"])

	_, zh := serveConsoleI18nJSON(t, http.MethodPost, "/api/vendors", `{"name":""}`, "zh-CN", CreateVendorMeta)
	require.Equal(t, false, zh["success"])
	require.Equal(t, "供应商名称不能为空", zh["message"])
}

func TestDeleteLoginSessionRequiresConsoleSessionFollowsAcceptLanguage(t *testing.T) {
	_, en := serveConsoleI18nJSON(t, http.MethodDelete, "/api/user/sessions/abc", "", "en-US", DeleteLoginSession)
	require.Equal(t, false, en["success"])
	require.Equal(t, "Please sign in from the console session first", en["message"])

	_, zh := serveConsoleI18nJSON(t, http.MethodDelete, "/api/user/sessions/abc", "", "zh-CN", DeleteLoginSession)
	require.Equal(t, false, zh["success"])
	require.Equal(t, "需要先登录控制台会话", zh["message"])
}

func TestConfirmPaymentComplianceFollowsAcceptLanguage(t *testing.T) {
	_, en := serveConsoleI18nJSON(t, http.MethodPost, "/api/payment/compliance/confirm", `{`, "en-US", ConfirmPaymentCompliance)
	require.Equal(t, false, en["success"])
	require.Equal(t, "Invalid parameters", en["message"])

	_, zh := serveConsoleI18nJSON(t, http.MethodPost, "/api/payment/compliance/confirm", `{`, "zh-CN", ConfirmPaymentCompliance)
	require.Equal(t, false, zh["success"])
	require.Equal(t, "无效的参数", zh["message"])

	_, enConfirm := serveConsoleI18nJSON(t, http.MethodPost, "/api/payment/compliance/confirm", `{}`, "en-US", ConfirmPaymentCompliance)
	require.Equal(t, false, enConfirm["success"])
	require.Equal(t, "Please confirm the compliance statement", enConfirm["message"])

	_, zhConfirm := serveConsoleI18nJSON(t, http.MethodPost, "/api/payment/compliance/confirm", `{}`, "zh-CN", ConfirmPaymentCompliance)
	require.Equal(t, false, zhConfirm["success"])
	require.Equal(t, "请确认合规声明", zhConfirm["message"])
}

func TestEnable2FAInvalidJSONFollowsAcceptLanguage(t *testing.T) {
	_, en := serveConsoleI18nJSON(t, http.MethodPost, "/api/user/2fa/enable", `{`, "en-US", Enable2FA)
	require.Equal(t, false, en["success"])
	require.Equal(t, "Invalid parameters", en["message"])

	_, zh := serveConsoleI18nJSON(t, http.MethodPost, "/api/user/2fa/enable", `{`, "zh-CN", Enable2FA)
	require.Equal(t, false, zh["success"])
	require.Equal(t, "无效的参数", zh["message"])
}

func TestSearchAllLogsDeprecatedFollowsAcceptLanguage(t *testing.T) {
	_, en := serveConsoleI18nJSON(t, http.MethodGet, "/api/log/self/search", "", "en-US", SearchAllLogs)
	require.Equal(t, false, en["success"])
	require.Equal(t, "This API has been deprecated", en["message"])

	_, zh := serveConsoleI18nJSON(t, http.MethodGet, "/api/log/self/search", "", "zh-CN", SearchAllLogs)
	require.Equal(t, false, zh["success"])
	require.Equal(t, "该接口已废弃", zh["message"])
}

func TestGetUserQuotaDatesRangeFollowsAcceptLanguage(t *testing.T) {
	_, en := serveConsoleI18nJSON(t, http.MethodGet, "/api/data/self?start_timestamp=1&end_timestamp=2592002", "", "en-US", GetUserQuotaDates)
	require.Equal(t, false, en["success"])
	require.Equal(t, "The time range cannot exceed 1 month", en["message"])

	_, zh := serveConsoleI18nJSON(t, http.MethodGet, "/api/data/self?start_timestamp=1&end_timestamp=2592002", "", "zh-CN", GetUserQuotaDates)
	require.Equal(t, false, zh["success"])
	require.Equal(t, "时间跨度不能超过 1 个月", zh["message"])
}

func TestCreateTicketEmptyTitleFollowsAcceptLanguage(t *testing.T) {
	_, en := serveConsoleI18nJSON(t, http.MethodPost, "/api/user/ticket", `{"title":"","content":"hello"}`, "en-US", CreateTicket)
	require.Equal(t, false, en["success"])
	require.Equal(t, "Ticket title cannot be empty and cannot exceed 200 characters", en["message"])

	_, zh := serveConsoleI18nJSON(t, http.MethodPost, "/api/user/ticket", `{"title":"","content":"hello"}`, "zh-CN", CreateTicket)
	require.Equal(t, false, zh["success"])
	require.Equal(t, "工单标题不能为空且不能超过 200 字", zh["message"])
}

func TestEmailBindPersonalAccessTokenFollowsAcceptLanguage(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.I18n())
	engine.Use(func(c *gin.Context) {
		c.Set("id", 42)
		c.Set("use_access_token", true)
		c.Next()
	})
	engine.POST("/api/oauth/email/bind", EmailBind)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/oauth/email/bind", strings.NewReader(`{"email":"a@b.com","verification_code":"1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", "en-US")
	engine.ServeHTTP(rec, req)

	var en map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &en))
	require.Equal(t, false, en["success"])
	require.Equal(t, "The current authentication method does not support binding email", en["message"])

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/oauth/email/bind", strings.NewReader(`{"email":"a@b.com","verification_code":"1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", "zh-CN")
	engine.ServeHTTP(rec, req)

	var zh map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &zh))
	require.Equal(t, false, zh["success"])
	require.Equal(t, "当前认证方式不支持绑定邮箱", zh["message"])
}

func TestUniversalVerifyPersonalAccessTokenFollowsAcceptLanguage(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.I18n())
	engine.Use(func(c *gin.Context) {
		c.Set("id", 42)
		c.Set("use_access_token", true)
		c.Next()
	})
	engine.POST("/api/verify", UniversalVerify)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/verify", strings.NewReader(`{"method":"2fa","code":"123456","scope":"update"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", "en-US")
	engine.ServeHTTP(rec, req)

	var en map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &en))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, false, en["success"])
	require.Equal(t, "The current authentication method does not support security verification", en["message"])

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/verify", strings.NewReader(`{"method":"2fa","code":"123456","scope":"update"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", "zh-CN")
	engine.ServeHTTP(rec, req)

	var zh map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &zh))
	require.Equal(t, false, zh["success"])
	require.Equal(t, "当前认证方式不支持安全验证", zh["message"])
}

func TestTurnstileCheckEmptyTokenFollowsAcceptLanguage(t *testing.T) {
	original := common.TurnstileCheckEnabled
	common.TurnstileCheckEnabled = true
	t.Cleanup(func() { common.TurnstileCheckEnabled = original })

	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.I18n())
	engine.Use(middleware.TurnstileCheck())
	engine.POST("/api/user/register", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "ok"})
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/user/register", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", "en-US")
	engine.ServeHTTP(rec, req)

	var en map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &en))
	require.Equal(t, false, en["success"])
	require.Equal(t, "Turnstile token is empty", en["message"])

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/user/register", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", "zh-CN")
	engine.ServeHTTP(rec, req)

	var zh map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &zh))
	require.Equal(t, false, zh["success"])
	require.Equal(t, "Turnstile token 为空", zh["message"])
}

func TestNormalizeAndValidateTokenGroupFollowsAcceptLanguage(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/token/", nil)
	c.Request.Header.Set("Accept-Language", "en-US")
	middleware.I18n()(c)
	_, ok := normalizeAndValidateTokenGroup(c, "")
	require.False(t, ok)
	var en map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &en))
	require.Equal(t, "Please select an API key group", en["message"])

	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/token/", nil)
	c.Request.Header.Set("Accept-Language", "zh-CN")
	middleware.I18n()(c)
	_, ok = normalizeAndValidateTokenGroup(c, "")
	require.False(t, ok)
	var zh map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &zh))
	require.Equal(t, "请选择 API 密钥分组", zh["message"])
}

func TestTurnstileCheckNetworkErrorFollowsAcceptLanguage(t *testing.T) {
	originalEnabled := common.TurnstileCheckEnabled
	originalPoster := middleware.PostTurnstileForm
	common.TurnstileCheckEnabled = true
	middleware.PostTurnstileForm = func(string, url.Values) (*http.Response, error) {
		return nil, errors.New("dial tcp: i/o timeout")
	}
	t.Cleanup(func() {
		common.TurnstileCheckEnabled = originalEnabled
		middleware.PostTurnstileForm = originalPoster
	})

	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.I18n())
	engine.Use(middleware.TurnstileCheck())
	engine.POST("/api/user/register", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "ok"})
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/user/register?turnstile=token", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", "en-US")
	engine.ServeHTTP(rec, req)
	var en map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &en))
	require.Equal(t, false, en["success"])
	require.Equal(t, "Unable to verify Turnstile, please try again later", en["message"])

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/user/register?turnstile=token", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", "zh-CN")
	engine.ServeHTTP(rec, req)
	var zh map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &zh))
	require.Equal(t, "无法验证 Turnstile，请稍后重试", zh["message"])
}

func TestTelegramPushEmptyConfigFollowsAcceptLanguage(t *testing.T) {
	origToken := common.TelegramPushBotToken
	origChat := common.TelegramPushChatId
	t.Cleanup(func() {
		common.TelegramPushBotToken = origToken
		common.TelegramPushChatId = origChat
	})
	common.TelegramPushBotToken = ""
	common.TelegramPushChatId = ""

	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	run := func(accept string) map[string]any {
		engine := gin.New()
		engine.Use(middleware.I18n())
		engine.POST("/api/telegram_push/test", TestTelegramPush)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/telegram_push/test", strings.NewReader(`{"text":"hello"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept-Language", accept)
		engine.ServeHTTP(rec, req)
		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "body=%s", rec.Body.String())
		return body
	}

	zh := run("zh-CN")
	require.Equal(t, false, zh["success"])
	require.Equal(t, "无效的 Telegram 推送配置", zh["message"])
	require.NotContains(t, zh["message"], "Bot Token")

	en := run("en-US")
	require.Equal(t, false, en["success"])
	require.Equal(t, "Invalid Telegram push configuration", en["message"])
}

func TestLocalizeTelegramFailureReasonFollowsAcceptLanguage(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodGet, "/api/telegram_push/records", nil)
	req.Header.Set("Accept-Language", "en-US")
	c.Request = req
	middleware.I18n()(c)

	require.Equal(t, "Push was interrupted and will retry automatically", localizeTelegramFailureReason(c, "推送任务中断，等待自动重试"))
	require.Equal(t, "Telegram push failed. Please check the bot token and chat ID.", localizeTelegramFailureReason(c, "Telegram 推送失败，HTTP 状态码 401: Unauthorized"))
	require.Equal(t, "Invalid Telegram push configuration", localizeTelegramFailureReason(c, i18n.MsgTelegramPushInvalidConfig))
}

func TestTaskPluginCompileErrorKeepsCompilerLogAndFollowsAcceptLanguage(t *testing.T) {
	handler := func(c *gin.Context) {
		respondTaskPluginCompileError(c, errors.New("SyntaxError: Unexpected token"))
	}
	zh := serveConsoleI18n(t, http.MethodPost, "/api/task_plugins/compile", handler, "zh-CN")
	require.Equal(t, false, zh["success"])
	require.Equal(t, "插件无法加载", zh["message"])
	require.NotContains(t, zh["message"], "SyntaxError")

	en := serveConsoleI18n(t, http.MethodPost, "/api/task_plugins/compile", handler, "en-US")
	require.Equal(t, "Failed to load plugin: SyntaxError: Unexpected token", en["message"])
}

func TestBEpusdtRejectedFollowsAcceptLanguage(t *testing.T) {
	handler := func(c *gin.Context) {
		common.ApiErrorI18n(c, i18n.MsgPaymentBepusdtRejected, map[string]any{
			"Error": "order amount too small (AMOUNT_TOO_SMALL)",
		})
	}
	zh := serveConsoleI18n(t, http.MethodPost, "/api/user/topup/bepusdt", handler, "zh-CN")
	require.Equal(t, false, zh["success"])
	require.Equal(t, "BEpusdt 网关拒绝订单", zh["message"])
	require.NotContains(t, zh["message"], "AMOUNT_TOO_SMALL")

	en := serveConsoleI18n(t, http.MethodPost, "/api/user/topup/bepusdt", handler, "en-US")
	require.Equal(t, "BEpusdt rejected the order: order amount too small (AMOUNT_TOO_SMALL)", en["message"])
}
