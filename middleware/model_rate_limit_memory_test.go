package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMemoryModelRateLimitUsesProtocolMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldMinutes := setting.ModelRequestRateLimitDurationMinutes
	setting.ModelRequestRateLimitDurationMinutes = 3
	t.Cleanup(func() {
		setting.ModelRequestRateLimitDurationMinutes = oldMinutes
	})

	totalRouter := gin.New()
	totalRouter.Use(func(c *gin.Context) {
		c.Set("id", 424201)
		c.Next()
	})
	totalRouter.POST("/v1/chat/completions", memoryRateLimitHandler(60, 1, 0), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	require.Equal(t, http.StatusOK, performModelRateLimit(totalRouter, "/v1/chat/completions").Code)
	totalLimited := performModelRateLimit(totalRouter, "/v1/chat/completions")
	require.Equal(t, http.StatusTooManyRequests, totalLimited.Code)
	require.Contains(t, totalLimited.Body.String(), "total request limit")
	require.Contains(t, totalLimited.Body.String(), "new_api_error")

	successRouter := gin.New()
	successRouter.Use(func(c *gin.Context) {
		c.Set("id", 424202)
		c.Next()
	})
	successRouter.POST("/v1/chat/completions", memoryRateLimitHandler(60, 0, 1), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	require.Equal(t, http.StatusOK, performModelRateLimit(successRouter, "/v1/chat/completions").Code)
	successLimited := performModelRateLimit(successRouter, "/v1/chat/completions")
	require.Equal(t, http.StatusTooManyRequests, successLimited.Code)
	require.Contains(t, successLimited.Body.String(), "request limit")
	require.NotContains(t, successLimited.Body.String(), "total request limit")

	imageRouter := gin.New()
	imageRouter.Use(func(c *gin.Context) {
		c.Set("id", 424203)
		c.Next()
	})
	imageRouter.POST("/v1/image-tasks", memoryRateLimitHandler(60, 1, 0), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	require.Equal(t, http.StatusOK, performModelRateLimit(imageRouter, "/v1/image-tasks").Code)
	imageLimited := performModelRateLimit(imageRouter, "/v1/image-tasks")
	require.Equal(t, http.StatusTooManyRequests, imageLimited.Code)
	require.Contains(t, imageLimited.Body.String(), "too many image task requests")
	require.Contains(t, imageLimited.Body.String(), "image_task_error")
}

func performModelRateLimit(router http.Handler, target string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, target, nil)
	router.ServeHTTP(recorder, request)
	return recorder
}
