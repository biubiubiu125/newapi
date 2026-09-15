package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAbortWithOpenAiMessageHidesDatabaseInternals(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/v1/chat/completions", func(c *gin.Context) {
		abortWithOpenAiMessage(c, http.StatusServiceUnavailable, `Failed to get available channel for model gpt-4o under group default (distributor): sql: database is closed`)
	})

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	body := recorder.Body.String()
	require.NotContains(t, strings.ToLower(body), "sql:")
	require.NotContains(t, strings.ToLower(body), "database is closed")
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, "new_api_error", payload.Error.Type)
	require.NotEqual(t, "", payload.Error.Message)
	require.NotContains(t, strings.ToLower(payload.Error.Message), "sql")
}

func TestAbortWithOpenAiMessageKeepsBusinessAndValidationMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{name: "business", message: "无可用渠道", want: "无可用渠道"},
		{name: "json validation", message: "invalid character 'x' looking for beginning of value", want: "invalid character 'x' looking for beginning of value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := gin.New()
			engine.POST("/v1/chat/completions", func(c *gin.Context) {
				abortWithOpenAiMessage(c, http.StatusBadRequest, tt.message)
			})
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Contains(t, recorder.Body.String(), tt.want)
		})
	}
}

func TestAbortWithOpenAiMessageHidesDatabaseInternalsOnImageTasks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/v1/image-tasks/task_x", func(c *gin.Context) {
		abortWithOpenAiMessage(c, http.StatusServiceUnavailable, `pq: password authentication failed for user "newapi" host=10.0.0.8`)
	})
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/image-tasks/task_x", nil))

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	body := recorder.Body.String()
	require.NotContains(t, strings.ToLower(body), "pq:")
	require.NotContains(t, strings.ToLower(body), "password")
	require.NotContains(t, body, "10.0.0.8")
	require.Contains(t, body, `"type":"image_task_error"`)
}

func TestAbortWithMidjourneyMessageHidesDatabaseInternals(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/mj/submit/imagine", func(c *gin.Context) {
		abortWithMidjourneyMessage(c, http.StatusInternalServerError, 1, `sql: database is closed`)
	})

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/mj/submit/imagine", nil))

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	body := recorder.Body.String()
	require.NotContains(t, strings.ToLower(body), "sql:")
	require.NotContains(t, strings.ToLower(body), "database is closed")
	var payload struct {
		Description string `json:"description"`
		Type        string `json:"type"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, "new_api_error", payload.Type)
	require.NotEqual(t, "", payload.Description)
	require.NotContains(t, strings.ToLower(payload.Description), "sql")
}

func TestAbortWithMidjourneyMessageKeepsBusinessMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/mj/submit/imagine", func(c *gin.Context) {
		abortWithMidjourneyMessage(c, http.StatusBadRequest, 23, "无可用渠道")
	})
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/mj/submit/imagine", nil))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "无可用渠道")
}
