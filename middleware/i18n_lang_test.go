package middleware

import (
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
