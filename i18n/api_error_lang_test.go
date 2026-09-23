package i18n

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
)

func TestApiErrorTranslatesRecordNotFound(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)

	cases := []struct {
		header string
		want   string
	}{
		{"zh-CN", "未找到"},
		{"en-US", "Not found"},
	}
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/api/user/self", nil)
		ctx.Request.Header.Set("Accept-Language", tc.header)

		common.ApiError(ctx, errors.New("record not found"))

		var payload map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatalf("%s: decode: %v body=%s", tc.header, err, recorder.Body.String())
		}
		if payload["success"] != false {
			t.Fatalf("%s: success=%v", tc.header, payload["success"])
		}
		if payload["message"] != tc.want {
			t.Fatalf("%s: message=%q want %q", tc.header, payload["message"], tc.want)
		}
	}
}

func TestApiErrorTranslatesLocalizedError(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/oauth/github", nil)
	ctx.Request.Header.Set("Accept-Language", "en-US")

	common.ApiError(ctx, common.Localized(MsgOAuthNotEnabled, map[string]any{"Provider": "GitHub"}))

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	want := "GitHub login and registration has not been enabled by administrator"
	if payload["message"] != want {
		t.Fatalf("message=%q want %q", payload["message"], want)
	}
}

func TestApiErrorTranslatesAuthFlowAndConsoleSentinels(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)

	cases := []struct {
		header string
		err    error
		want   string
	}{
		{"en-US", errors.New("auth flow is invalid"), "Authentication flow is invalid"},
		{"zh-CN", errors.New("auth flow is invalid"), "认证流程无效"},
		{"en-US", errors.New("auth flow has expired"), "Authentication flow has expired"},
		{"zh-CN", errors.New("auth flow has expired"), "认证流程已过期"},
		{"en-US", errors.New("auth flow has already been consumed"), "Authentication flow has already been used"},
		{"zh-CN", errors.New("auth flow has already been consumed"), "认证流程已使用"},
		{"en-US", errors.New("channel update persisted but runtime cache refresh failed: boom"), "Channel was saved, but refreshing the runtime cache failed"},
		{"zh-CN", errors.New("channel update persisted but runtime cache refresh failed: boom"), "渠道已保存，但运行时缓存刷新失败"},
		{"en-US", errors.New("failed to check name availability: boom"), "Failed to check whether the name is available"},
		{"zh-CN", errors.New("failed to check name availability: boom"), "检查名称是否可用失败"},
	}
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/api/user/self", nil)
		ctx.Request.Header.Set("Accept-Language", tc.header)

		common.ApiError(ctx, tc.err)

		var payload map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatalf("%s %v: decode: %v body=%s", tc.header, tc.err, err, recorder.Body.String())
		}
		if payload["message"] != tc.want {
			t.Fatalf("%s %v: message=%q want %q", tc.header, tc.err, payload["message"], tc.want)
		}
	}
}

func TestApiErrorTranslatesTwoFANumericCode(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)

	_, err := common.ValidateNumericCode("12")
	if err == nil {
		t.Fatal("expected invalid 2FA code")
	}

	cases := []struct {
		header string
		want   string
	}{
		{"zh-CN", "验证码必须是6位数字"},
		{"en-US", "Verification code must be 6 digits"},
	}
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/2fa/enable", nil)
		ctx.Request.Header.Set("Accept-Language", tc.header)

		common.ApiError(ctx, err)

		var payload map[string]any
		if decodeErr := json.Unmarshal(recorder.Body.Bytes(), &payload); decodeErr != nil {
			t.Fatalf("%s: decode: %v body=%s", tc.header, decodeErr, recorder.Body.String())
		}
		if payload["message"] != tc.want {
			t.Fatalf("%s: message=%q want %q", tc.header, payload["message"], tc.want)
		}
	}
}
