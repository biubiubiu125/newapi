package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
)

func TestRegisterValidationFollowsAcceptLanguage(t *testing.T) {
	oldRegister := common.RegisterEnabled
	oldPasswordRegister := common.PasswordRegisterEnabled
	oldEncryption := common.PasswordLoginEncryptionEnabled
	common.RegisterEnabled = true
	common.PasswordRegisterEnabled = true
	common.PasswordLoginEncryptionEnabled = false
	t.Cleanup(func() {
		common.RegisterEnabled = oldRegister
		common.PasswordRegisterEnabled = oldPasswordRegister
		common.PasswordLoginEncryptionEnabled = oldEncryption
	})

	usernameBody := `{"username":"bad name","password":"password1"}`
	_, zhUser := serveConsoleLang(
		t, http.MethodPost, "/api/user/register", "/api/user/register",
		usernameBody, "zh-CN", nil, Register,
	)
	require.Equal(t, false, zhUser["success"])
	require.Equal(t, "用户名只能包含字母、数字、下划线和连字符", zhUser["message"])
	require.NotContains(t, zhUser["message"], "username can only")

	_, enUser := serveConsoleLang(
		t, http.MethodPost, "/api/user/register", "/api/user/register",
		usernameBody, "en-US", nil, Register,
	)
	require.Equal(t, "Username can only contain letters, numbers, underscores, and hyphens", enUser["message"])

	_, zhEmail := serveConsoleLang(
		t, http.MethodPost, "/api/user/register", "/api/user/register",
		`{"username":"valid_name","password":"password1","email":"not-an-email"}`,
		"zh-CN", nil, Register,
	)
	require.Equal(t, "请输入有效的邮箱地址", zhEmail["message"])
	require.NotContains(t, zhEmail["message"], "Field validation")

	_, zhPassword := serveConsoleLang(
		t, http.MethodPost, "/api/user/register", "/api/user/register",
		`{"username":"valid_name","password":"short"}`,
		"zh-CN", nil, Register,
	)
	require.Equal(t, "密码长度需要在 8 到 20 个字符之间", zhPassword["message"])
	require.NotContains(t, zhPassword["message"], "Key:")
}

func TestApiErrorHidesUntranslatedEnglishForSimplifiedChinese(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)

	read := func(accept string, err error) string {
		t.Helper()
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/api/user/self", nil)
		ctx.Request.Header.Set("Accept-Language", accept)
		common.ApiError(ctx, err)
		var payload map[string]any
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload), recorder.Body.String())
		message, _ := payload["message"].(string)
		return message
	}

	require.Equal(t, "操作失败", read("zh-CN", errors.New("provider slug is required")))
	require.NotContains(t, read("zh-CN", errors.New("provider slug is required")), "provider slug")
	require.Equal(t, "操作失敗", read("zh-TW", errors.New("provider slug is required")))
	require.Equal(t, "provider slug is required", read("en-US", errors.New("provider slug is required")))
	require.Equal(t, "今日已签到", read("zh-CN", errors.New("今日已签到")))
}
