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
