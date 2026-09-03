package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestServeRevalidatedJSONReturnsNotModifiedForMatchingETag(t *testing.T) {
	gin.SetMode(gin.TestMode)

	firstRecorder := httptest.NewRecorder()
	firstContext, _ := gin.CreateTestContext(firstRecorder)
	firstContext.Request = httptest.NewRequest(http.MethodGet, "/api/notice", nil)
	serveRevalidatedJSON(firstContext, "public-content:notice:v1", "notice", gin.H{
		"success": true,
		"message": "",
		"data":    "notice",
	})

	etag := firstRecorder.Header().Get("ETag")
	require.NotEmpty(t, etag)
	require.Equal(t, http.StatusOK, firstRecorder.Code)

	secondRecorder := httptest.NewRecorder()
	secondContext, _ := gin.CreateTestContext(secondRecorder)
	secondContext.Request = httptest.NewRequest(http.MethodGet, "/api/notice", nil)
	secondContext.Request.Header.Set("If-None-Match", etag)
	serveRevalidatedJSON(secondContext, "public-content:notice:v1", "notice", gin.H{
		"success": true,
		"message": "",
		"data":    "notice",
	})

	require.Equal(t, http.StatusNotModified, secondRecorder.Code)
	require.Empty(t, secondRecorder.Body.Bytes())
	require.Equal(t, etag, secondRecorder.Header().Get("ETag"))
}
