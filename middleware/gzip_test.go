package middleware

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDecompressRequestMiddlewareClearsCompressedContentLength(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err := writer.Write([]byte(`{"prompt":"large after decompression"}`))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	engine := gin.New()
	engine.Use(DecompressRequestMiddleware())
	engine.POST("/v1/image-tasks/generations", func(c *gin.Context) {
		require.Equal(t, int64(-1), c.Request.ContentLength)
		body, readErr := io.ReadAll(c.Request.Body)
		require.NoError(t, readErr)
		require.JSONEq(t, `{"prompt":"large after decompression"}`, string(body))
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/image-tasks/generations", bytes.NewReader(compressed.Bytes()))
	req.Header.Set("Content-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestDecompressRequestMiddlewareUsesPublicImageTaskEnvelopeForMalformedGzip(t *testing.T) {
	engine := gin.New()
	engine.Use(DecompressRequestMiddleware())
	engine.POST("/v1/image-tasks/generations", func(c *gin.Context) {
		t.Fatal("malformed gzip request reached the route handler")
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/image-tasks/generations", bytes.NewReader([]byte("not-gzip")))
	req.Header.Set("Content-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, "invalid_request", payload.Error.Code)
	require.Equal(t, "image_task_error", payload.Error.Type)
	require.Contains(t, payload.Error.Message, "invalid gzip request body")
}
