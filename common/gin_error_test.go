package common

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestApiErrorHidesDatabaseInternals(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/user/self", nil)

	ApiError(ctx, errors.New(`pq: password authentication failed for user "newapi" host=10.0.0.8`))

	require.Equal(t, http.StatusOK, recorder.Code)
	body := recorder.Body.String()
	require.NotContains(t, strings.ToLower(body), "pq:")
	require.NotContains(t, strings.ToLower(body), "password")
	require.NotContains(t, body, "10.0.0.8")
	require.NotContains(t, body, "newapi")
	var payload map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, false, payload["success"])
	message := requireString(t, payload["message"])
	require.NotEqual(t, "", message)
	require.NotEqual(t, `pq: password authentication failed for user "newapi" host=10.0.0.8`, message)
}

func TestApiErrorTranslatesPostgresDriverError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := TranslateMessage
	TranslateMessage = func(_ *gin.Context, key string, _ ...map[string]any) string {
		if key == "common.database_error" {
			return "\u6570\u636e\u5e93\u51fa\u9519\uff0c\u8bf7\u8054\u7cfb\u7ba1\u7406\u5458"
		}
		return key
	}
	t.Cleanup(func() {
		TranslateMessage = previous
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/login", nil)

	ApiError(ctx, errors.New(`ERROR: duplicate key value violates unique constraint "auth_flows_pkey" (SQLSTATE 23505)`))

	var payload map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, false, payload["success"])
	require.Equal(t, "\u6570\u636e\u5e93\u51fa\u9519\uff0c\u8bf7\u8054\u7cfb\u7ba1\u7406\u5458", payload["message"])
	require.NotContains(t, recorder.Body.String(), "SQLSTATE")
	require.NotContains(t, recorder.Body.String(), "auth_flows_pkey")
}

func TestApiErrorKeepsChineseBusinessMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/checkin", nil)

	ApiError(ctx, errors.New("今日已签到"))

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, false, payload["success"])
	require.Equal(t, "今日已签到", payload["message"])
}

func TestApiErrorMsgHidesDatabaseInternals(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel/", nil)

	ApiErrorMsg(ctx, `ERROR: relation "channels" does not exist (SQLSTATE 42P01)`)

	require.Equal(t, http.StatusOK, recorder.Code)
	body := recorder.Body.String()
	require.NotContains(t, body, "SQLSTATE")
	require.NotContains(t, body, "channels")
	require.NotContains(t, strings.ToLower(body), "relation")
	var payload map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, false, payload["success"])
	require.NotEqual(t, `ERROR: relation "channels" does not exist (SQLSTATE 42P01)`, payload["message"])
}

func TestApiErrorWithStatusHidesDatabaseInternals(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/passkey/register/begin", nil)

	ApiErrorWithStatus(ctx, http.StatusUnauthorized, errors.New("sql: database is closed"))

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	body := recorder.Body.String()
	require.NotContains(t, strings.ToLower(body), "sql:")
	require.NotContains(t, strings.ToLower(body), "database is closed")
	var payload map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, false, payload["success"])
	require.NotEqual(t, "sql: database is closed", payload["message"])
}

func TestSanitizeChineseConsoleTextDropsEnglishClauses(t *testing.T) {
	require.Equal(t, "连接失败", SanitizeChineseConsoleText("连接失败: dial tcp 127.0.0.1:11434: connect: connection refused"))
	require.Equal(t, "失败", SanitizeChineseConsoleText("失败 timeout"))
	require.Equal(t, "不是合法 JSON", SanitizeChineseConsoleText("不是合法 JSON"))
	require.Equal(t, "今日已签到", SanitizeChineseConsoleText("今日已签到"))
	require.Equal(t, "BEpusdt 网关拒绝订单：金额过小", SanitizeChineseConsoleText("BEpusdt 网关拒绝订单：金额过小"))
	require.Equal(t, "", SanitizeChineseConsoleText("connection refused"))
}

func TestApiErrorStripsEnglishClausesOnChineseDashboard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousLanguage := DashboardLanguage
	previousTranslate := TranslateMessage
	DashboardLanguage = func(*gin.Context) string { return "zh-CN" }
	TranslateMessage = func(_ *gin.Context, key string, _ ...map[string]any) string {
		if key == "common.operation_failed" {
			return "操作失败"
		}
		return key
	}
	t.Cleanup(func() {
		DashboardLanguage = previousLanguage
		TranslateMessage = previousTranslate
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/test", nil)
	ApiError(ctx, errors.New("连接失败: upstream timeout"))

	var payload map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, "连接失败", payload["message"])
	require.NotContains(t, recorder.Body.String(), "upstream")
	require.NotContains(t, recorder.Body.String(), "timeout")
}

func TestPublicRequestErrorMessageKeepsRecordNotFoundEnglish(t *testing.T) {
	require.Equal(t, "record not found", PublicRequestErrorMessage("record not found"))
}

func TestPublicRequestErrorMessageHidesDatabaseInternals(t *testing.T) {
	message := PublicRequestErrorMessage(`pq: password authentication failed for user "newapi" host=10.0.0.8`)
	require.Equal(t, "invalid request", message)
	require.Equal(t, "invalid character 'x' looking for beginning of value", PublicRequestErrorMessage("invalid character 'x' looking for beginning of value"))
}

func requireString(t *testing.T, value any) string {
	t.Helper()
	text, ok := value.(string)
	require.True(t, ok, "message must be a string, got %T", value)
	return text
}

func TestDefaultTranslateMessageDoesNotSetTranslateID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/user/self", nil)

	got := TranslateMessage(ctx, "common.invalid_params")
	require.Equal(t, "common.invalid_params", got)
	require.Empty(t, recorder.Header().Get("X-Translate-id"))
}
