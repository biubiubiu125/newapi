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
