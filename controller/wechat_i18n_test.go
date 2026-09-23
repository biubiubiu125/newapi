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
)

func withWeChatServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	previous := common.WeChatServerAddress
	common.WeChatServerAddress = server.URL
	t.Cleanup(func() { common.WeChatServerAddress = previous })
}

func TestGetWeChatIdByCodeHidesUpstreamEnglish(t *testing.T) {
	if err := i18n.Init(); err != nil {
		t.Fatal(err)
	}

	withWeChatServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":false,"message":"invalid wechat code from upstream"}`))
	})

	_, err := getWeChatIdByCode("wx-code")
	loc, ok := common.AsLocalizedError(err)
	if !ok {
		t.Fatalf("err=%v (%T) want LocalizedError", err, err)
	}
	if loc.Key != i18n.MsgUserVerificationCodeError {
		t.Fatalf("key=%q want %q", loc.Key, i18n.MsgUserVerificationCodeError)
	}
	if strings.Contains(err.Error(), "invalid wechat code") {
		t.Fatalf("leaked upstream English: %v", err)
	}

	gin.SetMode(gin.TestMode)
	cases := []struct {
		header string
		want   string
	}{
		{"zh-CN", "验证码错误或已过期"},
		{"en-US", "Verification code is incorrect or has expired"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		req := httptest.NewRequest(http.MethodGet, "/api/oauth/wechat", nil)
		req.Header.Set("Accept-Language", tc.header)
		c.Request = req
		middleware.I18n()(c)
		common.ApiError(c, err)
		var body struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
		}
		if decodeErr := json.Unmarshal(rec.Body.Bytes(), &body); decodeErr != nil {
			t.Fatalf("%s decode: %v body=%s", tc.header, decodeErr, rec.Body.String())
		}
		if body.Success {
			t.Fatalf("%s success=true body=%s", tc.header, rec.Body.String())
		}
		if body.Message != tc.want {
			t.Fatalf("%s message=%q want %q", tc.header, body.Message, tc.want)
		}
		if strings.Contains(body.Message, "invalid wechat code") {
			t.Fatalf("%s leaked upstream English: %q", tc.header, body.Message)
		}
	}
}

func TestGetWeChatIdByCodeConnectFailureIsLocalized(t *testing.T) {
	if err := i18n.Init(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	addr := server.URL
	server.Close()
	previous := common.WeChatServerAddress
	common.WeChatServerAddress = addr
	t.Cleanup(func() { common.WeChatServerAddress = previous })

	_, err := getWeChatIdByCode("wx-code")
	loc, ok := common.AsLocalizedError(err)
	if !ok {
		t.Fatalf("err=%v (%T) want LocalizedError", err, err)
	}
	if loc.Key != i18n.MsgOAuthConnectFailed {
		t.Fatalf("key=%q want %q", loc.Key, i18n.MsgOAuthConnectFailed)
	}
	if strings.Contains(strings.ToLower(err.Error()), "connection refused") {
		t.Fatalf("leaked dial English: %v", err)
	}
}

func TestGetWeChatIdByCodeInvalidJSONIsLocalized(t *testing.T) {
	if err := i18n.Init(); err != nil {
		t.Fatal(err)
	}
	withWeChatServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	})
	_, err := getWeChatIdByCode("wx-code")
	loc, ok := common.AsLocalizedError(err)
	if !ok {
		t.Fatalf("err=%v (%T) want LocalizedError", err, err)
	}
	if loc.Key != i18n.MsgOAuthGetUserErr {
		t.Fatalf("key=%q want %q", loc.Key, i18n.MsgOAuthGetUserErr)
	}
}
