package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

func TestApplyInterfaceLanguageToNewUserUsesAcceptLanguage(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/register", nil)
	ctx.Request.Header.Set("Accept-Language", "en-US,en;q=0.9")

	user := model.User{Username: "new-lang-user"}
	applyInterfaceLanguageToNewUser(ctx, &user)
	assert.Equal(t, "en", user.GetSetting().Language)

	user.SetSetting(dto.UserSetting{Language: "zhCN"})
	applyInterfaceLanguageToNewUser(ctx, &user)
	assert.Equal(t, "zhCN", user.GetSetting().Language)
}

func TestUpdateSelfCanonicalizesRecognizedLanguageAndRejectsUnknown(t *testing.T) {
	db := setupManageUserTestDB(t)
	require.NoError(t, i18n.Init())

	user := model.User{
		Username: "self-language-user",
		Password: "password",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, db.Create(&user).Error)

	gin.SetMode(gin.TestMode)
	run := func(body, accept string) map[string]any {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPut, "/api/user/self", strings.NewReader(body))
		ctx.Request.Header.Set("Content-Type", "application/json")
		ctx.Request.Header.Set("Accept-Language", accept)
		ctx.Set("id", user.Id)
		UpdateSelf(ctx)
		var payload map[string]any
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
		return payload
	}

	reject := run(`{"language":"de"}`, "en-US")
	require.Equal(t, false, reject["success"])
	require.Equal(t, "Invalid parameters", reject["message"])

	got, err := model.GetUserById(user.Id, true)
	require.NoError(t, err)
	require.Empty(t, got.GetSetting().Language)

	ok := run(`{"language":"zhCN"}`, "en-US")
	require.Equal(t, true, ok["success"], "body=%v", ok)
	got, err = model.GetUserById(user.Id, true)
	require.NoError(t, err)
	require.Equal(t, i18n.LangZhCN, got.GetSetting().Language)

	ok = run(`{"language":"en-US"}`, "zh-CN")
	require.Equal(t, true, ok["success"], "body=%v", ok)
	got, err = model.GetUserById(user.Id, true)
	require.NoError(t, err)
	require.Equal(t, i18n.LangEn, got.GetSetting().Language)
}

func TestUpdateSelfLanguageDoesNotRestoreStaleRoleStatusGroupOrBinding(t *testing.T) {
	db := setupManageUserTestDB(t)
	require.NoError(t, i18n.Init())

	user := model.User{
		Username: "self-language-race",
		Password: "password",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		GitHubId: "gh-user",
		Quota:    12345,
	}
	require.NoError(t, db.Create(&user).Error)

	fired := false
	callbackName := "test_update_self_language_snapshot"
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if fired || tx.Statement.Table != "users" {
			return
		}
		fired = true
		require.NoError(t, tx.Session(&gorm.Session{NewDB: true, SkipHooks: true}).Exec(
			`UPDATE users SET role = ?, status = ?, "group" = ?, github_id = ? WHERE id = ?`,
			common.RoleAdminUser,
			common.UserStatusDisabled,
			"vip",
			"",
			user.Id,
		).Error)
	}))
	t.Cleanup(func() {
		db.Callback().Query().Remove(callbackName)
	})

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/user/self", strings.NewReader(`{"language":"zhCN"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("id", user.Id)
	UpdateSelf(ctx)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, true, payload["success"], "body=%v", payload)
	require.True(t, fired)

	var got model.User
	require.NoError(t, db.First(&got, user.Id).Error)
	require.Equal(t, i18n.LangZhCN, got.GetSetting().Language)
	require.Equal(t, common.RoleAdminUser, got.Role)
	require.Equal(t, common.UserStatusDisabled, got.Status)
	require.Equal(t, "vip", got.Group)
	require.Empty(t, got.GitHubId)
	require.Equal(t, int64(12345), got.Quota)
}

func TestApplyStoredInterfaceLanguageToNewUserIgnoresUnknownAndCallbackHeader(t *testing.T) {
	require.NoError(t, i18n.Init())

	user := model.User{Username: "oauth-lang-user"}
	applyStoredInterfaceLanguageToNewUser(&user, "zhCN")
	assert.Equal(t, i18n.LangZhCN, user.GetSetting().Language)

	applyStoredInterfaceLanguageToNewUser(&user, "en")
	assert.Equal(t, i18n.LangZhCN, user.GetSetting().Language)

	blank := model.User{Username: "oauth-lang-blank"}
	applyStoredInterfaceLanguageToNewUser(&blank, "de")
	assert.Empty(t, blank.GetSetting().Language)

	applyStoredInterfaceLanguageToNewUser(&blank, "fr-FR")
	assert.Equal(t, i18n.LangFr, blank.GetSetting().Language)
}

func TestUpdateUserSettingPreservesLanguageSidebarAndBilling(t *testing.T) {
	db := setupManageUserTestDB(t)
	require.NoError(t, i18n.Init())

	user := model.User{
		Username: "setting-language-user",
		Password: "password",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	user.SetSetting(dto.UserSetting{
		Language:              "zhCN",
		SidebarModules:        `{"chat":true}`,
		BillingPreference:     "wallet_only",
		NotifyType:            dto.NotifyTypeEmail,
		QuotaWarningThreshold: 500,
	})
	require.NoError(t, db.Create(&user).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/user/setting",
		strings.NewReader(`{"notify_type":"email","quota_warning_threshold":1000,"accept_unset_model_ratio_model":true,"record_ip_log":true}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("id", user.Id)

	UpdateUserSetting(ctx)

	assert.Equal(t, http.StatusOK, recorder.Code)
	var body struct {
		Success bool `json:"success"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.True(t, body.Success, "body=%s", recorder.Body.String())

	got, err := model.GetUserById(user.Id, true)
	require.NoError(t, err)
	setting := got.GetSetting()
	assert.Equal(t, "zhCN", setting.Language)
	assert.Equal(t, `{"chat":true}`, setting.SidebarModules)
	assert.Equal(t, "wallet_only", setting.BillingPreference)
	assert.Equal(t, dto.NotifyTypeEmail, setting.NotifyType)
	assert.Equal(t, float64(1000), setting.QuotaWarningThreshold)
	assert.True(t, setting.AcceptUnsetRatioModel)
	assert.True(t, setting.RecordIpLog)
}

func TestUpdateUserSettingKeepsInactiveNotificationChannels(t *testing.T) {
	db := setupManageUserTestDB(t)
	require.NoError(t, i18n.Init())

	user := model.User{
		Username: "setting-notify-user",
		Password: "password",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	user.SetSetting(dto.UserSetting{
		Language:              "zhCN",
		NotifyType:            dto.NotifyTypeWebhook,
		QuotaWarningThreshold: 500,
		WebhookUrl:            "https://example.com/hook",
		WebhookSecret:         "keep-secret",
		NotificationEmail:     "notify@example.com",
		BarkUrl:               "https://api.day.app/token",
		GotifyUrl:             "https://gotify.example.com",
		GotifyToken:           "gotify-token",
		GotifyPriority:        7,
	})
	require.NoError(t, db.Create(&user).Error)

	putSetting := func(body string) {
		t.Helper()
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPut, "/api/user/setting", strings.NewReader(body))
		ctx.Request.Header.Set("Content-Type", "application/json")
		ctx.Set("id", user.Id)
		UpdateUserSetting(ctx)
		require.Contains(t, recorder.Body.String(), `"success":true`, recorder.Body.String())
	}

	putSetting(`{
		"notify_type":"email",
		"quota_warning_threshold":800,
		"webhook_url":"https://example.com/hook",
		"webhook_secret":"keep-secret",
		"notification_email":"notify@example.com",
		"bark_url":"https://api.day.app/token",
		"gotify_url":"https://gotify.example.com",
		"gotify_token":"gotify-token",
		"gotify_priority":7,
		"accept_unset_model_ratio_model":false,
		"record_ip_log":true
	}`)

	got, err := model.GetUserById(user.Id, true)
	require.NoError(t, err)
	setting := got.GetSetting()
	assert.Equal(t, dto.NotifyTypeEmail, setting.NotifyType)
	assert.Equal(t, "https://example.com/hook", setting.WebhookUrl)
	assert.Equal(t, "keep-secret", setting.WebhookSecret)
	assert.Equal(t, "notify@example.com", setting.NotificationEmail)
	assert.Equal(t, "https://api.day.app/token", setting.BarkUrl)
	assert.Equal(t, "https://gotify.example.com", setting.GotifyUrl)
	assert.Equal(t, "gotify-token", setting.GotifyToken)
	assert.Equal(t, 7, setting.GotifyPriority)
	assert.Equal(t, "zhCN", setting.Language)

	putSetting(`{"notify_type":"email","quota_warning_threshold":900,"accept_unset_model_ratio_model":false,"record_ip_log":false}`)
	got, err = model.GetUserById(user.Id, true)
	require.NoError(t, err)
	setting = got.GetSetting()
	assert.Equal(t, float64(900), setting.QuotaWarningThreshold)
	assert.Equal(t, "https://example.com/hook", setting.WebhookUrl)
	assert.Equal(t, "keep-secret", setting.WebhookSecret)
	assert.Equal(t, "notify@example.com", setting.NotificationEmail)
	assert.Equal(t, "https://api.day.app/token", setting.BarkUrl)
	assert.Equal(t, 7, setting.GotifyPriority)
	assert.False(t, setting.RecordIpLog)

	putSetting(`{
		"notify_type":"webhook",
		"quota_warning_threshold":900,
		"webhook_url":"https://example.com/hook",
		"webhook_secret":"",
		"accept_unset_model_ratio_model":false,
		"record_ip_log":false
	}`)
	got, err = model.GetUserById(user.Id, true)
	require.NoError(t, err)
	setting = got.GetSetting()
	assert.Equal(t, dto.NotifyTypeWebhook, setting.NotifyType)
	assert.Equal(t, "https://example.com/hook", setting.WebhookUrl)
	assert.Empty(t, setting.WebhookSecret)
	assert.Equal(t, "notify@example.com", setting.NotificationEmail)
	assert.Equal(t, "https://gotify.example.com", setting.GotifyUrl)
}
