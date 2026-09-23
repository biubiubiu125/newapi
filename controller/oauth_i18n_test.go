package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

func TestOAuthDisabledMessagesFollowAcceptLanguage(t *testing.T) {
	if err := i18n.Init(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)

	origTelegram := common.TelegramOAuthEnabled
	origGitHub := common.GitHubOAuthEnabled
	origLinuxDO := common.LinuxDOOAuthEnabled
	origDiscord := system_setting.GetDiscordSettings().Enabled
	origOIDC := system_setting.GetOIDCSettings().Enabled
	origWeChat := common.WeChatAuthEnabled
	t.Cleanup(func() {
		common.TelegramOAuthEnabled = origTelegram
		common.GitHubOAuthEnabled = origGitHub
		common.LinuxDOOAuthEnabled = origLinuxDO
		system_setting.GetDiscordSettings().Enabled = origDiscord
		system_setting.GetOIDCSettings().Enabled = origOIDC
		common.WeChatAuthEnabled = origWeChat
	})
	common.TelegramOAuthEnabled = false
	common.GitHubOAuthEnabled = false
	common.LinuxDOOAuthEnabled = false
	system_setting.GetDiscordSettings().Enabled = false
	system_setting.GetOIDCSettings().Enabled = false
	common.WeChatAuthEnabled = false

	engine := gin.New()
	engine.Use(middleware.I18n())
	engine.POST("/telegram", TelegramLogin)
	engine.POST("/github-bind", GitHubBind)
	engine.POST("/linuxdo-bind", LinuxDoBind)
	engine.GET("/discord-bind", DiscordBind)
	engine.GET("/oidc-bind", OidcBind)
	engine.GET("/wechat", WeChatAuth)

	cases := []struct {
		method string
		path   string
		header string
		want   string
	}{
		{http.MethodPost, "/telegram", "zh-CN", "管理员未开启通过 Telegram 登录以及注册"},
		{http.MethodPost, "/telegram", "en-US", "Telegram login and registration has not been enabled by administrator"},
		{http.MethodPost, "/github-bind", "zh-CN", "管理员未开启通过 GitHub 登录以及注册"},
		{http.MethodPost, "/github-bind", "en-US", "GitHub login and registration has not been enabled by administrator"},
		{http.MethodPost, "/linuxdo-bind", "zh-CN", "管理员未开启通过 Linux DO 登录以及注册"},
		{http.MethodPost, "/linuxdo-bind", "en-US", "Linux DO login and registration has not been enabled by administrator"},
		{http.MethodGet, "/discord-bind", "zh-CN", "管理员未开启通过 Discord 登录以及注册"},
		{http.MethodGet, "/discord-bind", "en-US", "Discord login and registration has not been enabled by administrator"},
		{http.MethodGet, "/oidc-bind", "zh-CN", "管理员未开启通过 OIDC 登录以及注册"},
		{http.MethodGet, "/oidc-bind", "en-US", "OIDC login and registration has not been enabled by administrator"},
		{http.MethodGet, "/wechat", "zh-CN", "管理员未开启通过 WeChat 登录以及注册"},
		{http.MethodGet, "/wechat", "en-US", "WeChat login and registration has not been enabled by administrator"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("Accept-Language", tc.header)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		var body struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s %s %s: decode: %v body=%s", tc.method, tc.path, tc.header, err, rec.Body.String())
		}
		if body.Success {
			t.Fatalf("%s %s %s: success=true, want oauth disabled error", tc.method, tc.path, tc.header)
		}
		if body.Message != tc.want {
			t.Fatalf("%s %s %s: message=%q want %q", tc.method, tc.path, tc.header, body.Message, tc.want)
		}
	}
}

func oauthErrorContext(t *testing.T, lang string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodGet, "/api/oauth/custom", nil)
	req.Header.Set("Accept-Language", lang)
	c.Request = req
	middleware.I18n()(c)
	return c, rec
}

func decodeAPIErrorMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if body.Success {
		t.Fatalf("success=true, want oauth error body=%s", rec.Body.String())
	}
	return body.Message
}

func TestHandleOAuthAccessDeniedDefaultFollowsAcceptLanguage(t *testing.T) {
	if err := i18n.Init(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)

	cases := []struct {
		header string
		want   string
	}{
		{"zh-CN", "访问被拒绝：当前账号不满足该登录方式的访问要求。"},
		{"en-US", "Access denied: your account does not meet this provider's access requirements."},
	}
	defaultEnglish := "Access denied: your account does not meet this provider's access requirements."
	for _, tc := range cases {
		for _, denied := range []*oauth.AccessDeniedError{
			{},
			{Message: defaultEnglish},
		} {
			c, rec := oauthErrorContext(t, tc.header)
			handleOAuthError(c, denied)
			got := decodeAPIErrorMessage(t, rec)
			if got != tc.want {
				t.Fatalf("%s message=%q want %q err=%q", tc.header, got, tc.want, denied.Message)
			}
		}
	}
}

func TestHandleOAuthAccessDeniedCustomMessagePassthrough(t *testing.T) {
	if err := i18n.Init(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)

	const custom = "你的账号未加入白名单"
	c, rec := oauthErrorContext(t, "zh-CN")
	handleOAuthError(c, &oauth.AccessDeniedError{Message: custom})
	if got := decodeAPIErrorMessage(t, rec); got != custom {
		t.Fatalf("message=%q want custom %q", got, custom)
	}
}

func TestOAuthProviderQueryErrorHidesErrorDescription(t *testing.T) {
	if err := i18n.Init(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)

	cases := []struct {
		query  string
		header string
		want   string
	}{
		{
			query:  "error=access_denied&error_description=The+user+denied+your+request",
			header: "zh-CN",
			want:   "授权已取消，或该登录方式拒绝了访问。",
		},
		{
			query:  "error=access_denied&error_description=The+user+denied+your+request",
			header: "en-US",
			want:   "Authorization was cancelled or access was denied.",
		},
		{
			query:  "error=server_error&error_description=temporarily+unavailable",
			header: "zh-CN",
			want:   "授权失败，请重试。",
		},
		{
			query:  "error=server_error&error_description=temporarily+unavailable",
			header: "en-US",
			want:   "Authorization failed. Please try again.",
		},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		req := httptest.NewRequest(http.MethodGet, "/api/oauth/github?"+tc.query, nil)
		req.Header.Set("Accept-Language", tc.header)
		c.Request = req
		middleware.I18n()(c)
		respondOAuthProviderQueryError(c)
		got := decodeAPIErrorMessage(t, rec)
		if got != tc.want {
			t.Fatalf("%s %s: message=%q want %q", tc.header, tc.query, got, tc.want)
		}
		if strings.Contains(strings.ToLower(got), "user denied") || strings.Contains(strings.ToLower(got), "temporarily unavailable") {
			t.Fatalf("%s leaked provider description: %q", tc.header, got)
		}
	}
}
