package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

func TestTokenAuthProtocolErrorStaysEnglish(t *testing.T) {
	if err := i18n.Init(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(I18n())
	engine.POST("/v1/chat/completions", TokenAuth(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want %d body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	msg := openaiErrorMessage(t, rec)
	assertEnglishProtocolMessage(t, msg, "Invalid token")
}

func TestPublicImageTaskAuthErrorStaysEnglish(t *testing.T) {
	if err := i18n.Init(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(I18n())
	engine.GET("/v1/image-tasks", TokenAuthForTaskAccess(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/image-tasks?ids=task-1", nil)
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want %d body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	msg := openaiErrorMessage(t, rec)
	assertEnglishProtocolMessage(t, msg, "Invalid token")
}

func TestDistributorProtocolErrorStaysEnglish(t *testing.T) {
	if err := i18n.Init(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(I18n())
	engine.POST("/v1/chat/completions", Distribute())

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	msg := openaiErrorMessage(t, rec)
	assertEnglishProtocolMessage(t, msg, "Invalid request")
	if strings.Contains(msg, "Invalid request: Invalid request") {
		t.Fatalf("protocol message was wrapped twice: %q", msg)
	}
}

func TestDistributorMidjourneyProtocolErrorIsNotDoubleWrapped(t *testing.T) {
	if err := i18n.Init(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(I18n())
	engine.POST("/mj/submit/imagine", Distribute())

	req := httptest.NewRequest(http.MethodPost, "/mj/submit/imagine", strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	msg := openaiErrorMessage(t, rec)
	assertEnglishProtocolMessage(t, msg, "Invalid request")
	if strings.Contains(msg, "Invalid Midjourney request") {
		t.Fatalf("midjourney protocol message was wrapped twice: %q", msg)
	}
}

func TestNoAvailableChannelMessageStaysEnglish(t *testing.T) {
	if err := i18n.Init(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	I18n()(ctx)

	msg := noAvailableChannelMessage(ctx, "default", "gpt-4")
	assertEnglishProtocolMessage(t, msg, "No available channel")
}

func TestTokenAuthReadOnlyUsesUserSettingLanguage(t *testing.T) {
	if err := i18n.Init(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	setupDashboardAuthMiddlewareTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Token{}))
	model.InitColumnNames()
	i18n.SetUserLangLoader(model.GetUserLanguage)
	t.Cleanup(func() { i18n.SetUserLangLoader(nil) })

	user := &model.User{
		Username: "token-lang-user",
		Password: "password-placeholder",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "token-lang-aff",
	}
	user.SetSetting(dto.UserSetting{Language: "en"})
	require.NoError(t, model.DB.Create(user).Error)
	require.NoError(t, model.DB.Create(&model.Token{
		UserId: user.Id,
		Key:    "tokenlangkey",
		Status: common.TokenStatusEnabled,
		Name:   "readonly-lang",
	}).Error)

	engine := gin.New()
	engine.Use(I18n())
	engine.GET("/api/log/self", TokenAuthReadOnly(), func(c *gin.Context) {
		common.ApiErrorI18n(c, i18n.MsgCheckinDisabled)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/log/self", nil)
	req.Header.Set("Authorization", "Bearer sk-tokenlangkey")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	var body struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "body=%s", rec.Body.String())
	if body.Success {
		t.Fatal("success=true, want translated checkin disabled error")
	}
	if body.Message != "Check-in feature is not enabled" {
		t.Fatalf("message=%q want English from user setting, not Accept-Language", body.Message)
	}
}

func TestTokenAuthReadOnlyKeepsDashboardLanguage(t *testing.T) {
	if err := i18n.Init(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(I18n())
	engine.GET("/api/log/self", TokenAuthReadOnly(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/log/self", nil)
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want %d body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	var body struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if body.Success {
		t.Fatal("success=true, want dashboard auth error")
	}
	if body.Message != "未提供令牌" {
		t.Fatalf("message=%q want dashboard Chinese copy", body.Message)
	}
}

func openaiErrorMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	return body.Error.Message
}

func assertEnglishProtocolMessage(t *testing.T, msg, mustContain string) {
	t.Helper()
	if !strings.Contains(msg, mustContain) {
		t.Fatalf("message=%q want to contain %q", msg, mustContain)
	}
	for _, r := range msg {
		if unicode.In(r, unicode.Han) {
			t.Fatalf("protocol message contains Chinese: %q", msg)
		}
	}
}
