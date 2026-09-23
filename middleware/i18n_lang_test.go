package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

func TestDetectLanguageCanonicalizesFrontendZhTW(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserSetting, dto.UserSetting{
		Language: "zhTW",
	})
	if got := detectLanguage(ctx); got != i18n.LangZhTW {
		t.Fatalf("detectLanguage(zhTW)=%q want %q", got, i18n.LangZhTW)
	}
}

func TestDetectLanguageCanonicalizesFrontendZhCN(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserSetting, dto.UserSetting{
		Language: "zhCN",
	})
	if got := detectLanguage(ctx); got != i18n.LangZhCN {
		t.Fatalf("detectLanguage(zhCN)=%q want %q", got, i18n.LangZhCN)
	}
}

func TestDetectLanguageDefaultsToSimplifiedChinese(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	if got := detectLanguage(ctx); got != i18n.LangZhCN {
		t.Fatalf("detectLanguage default=%q want %q", got, i18n.LangZhCN)
	}
}

func TestDetectLanguageEnglishHeaderStaysEnglish(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	ctx.Request.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if got := detectLanguage(ctx); got != i18n.LangEn {
		t.Fatalf("detectLanguage en-US=%q want %q", got, i18n.LangEn)
	}
}

func TestI18nLoginErrorFollowsAcceptLanguage(t *testing.T) {
	if err := i18n.Init(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(I18n())
	engine.POST("/api/user/login", func(c *gin.Context) {
		common.ApiErrorI18n(c, i18n.MsgUserUsernameOrPasswordError)
	})

	cases := []struct {
		header string
		want   string
	}{
		{"zh-CN,zh;q=0.9", "用户名或密码错误，或用户已被封禁"},
		{"en-US,en;q=0.9", "Username or password is incorrect, or user has been banned"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/user/login", nil)
		req.Header.Set("Accept-Language", tc.header)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		var body struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("Accept-Language %q: decode: %v body=%s", tc.header, err, rec.Body.String())
		}
		if body.Success {
			t.Fatalf("Accept-Language %q: success=true, want login error", tc.header)
		}
		if body.Message != tc.want {
			t.Fatalf("Accept-Language %q: message=%q want %q", tc.header, body.Message, tc.want)
		}
	}
}

func TestI18nUserSettingOverridesAcceptLanguageAfterAuth(t *testing.T) {
	if err := i18n.Init(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(I18n())
	engine.Use(func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyUserSetting, dto.UserSetting{
			Language: "zhCN",
		})
		c.Set("id", 1)
		c.Next()
	})
	engine.GET("/api/user/self", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": i18n.T(c, i18n.MsgUserUsernameOrPasswordError),
			"lang":    GetLanguage(c),
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/user/self", nil)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	var body struct {
		Message string `json:"message"`
		Lang    string `json:"lang"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	want := "用户名或密码错误，或用户已被封禁"
	if body.Message != want {
		t.Fatalf("message=%q want %q", body.Message, want)
	}
	if body.Lang != i18n.LangZhCN {
		t.Fatalf("GetLanguage=%q want %q", body.Lang, i18n.LangZhCN)
	}
}
