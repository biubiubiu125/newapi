package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSetWebRouterServesIndexPageForWebAndWorkbenchRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	indexPage := []byte("<!doctype html><html><body>web index</body></html>")

	require.NotPanics(t, func() {
		SetWebRouter(engine, WebAssets{IndexPage: indexPage})
	})

	cases := []string{"/missing", "/image-tasks", "/image-tasks/task-1"}
	for _, path := range cases {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		engine.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusOK, recorder.Code, path)
		require.Equal(t, string(indexPage), recorder.Body.String(), path)
	}
}
