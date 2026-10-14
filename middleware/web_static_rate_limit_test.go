package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGlobalWebRateLimitLeavesBuildFilesOutOfThePageBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	restore := setWebRateLimitForTest(t, true, 2, 60, false, 1, 60)
	defer restore()

	frontendDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(frontendDir, "static", "js"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(frontendDir, "static", "js", "app.js"), []byte("export const ready = true\n"), 0o644))
	frontendFS := static.LocalFile(frontendDir, false)

	router := gin.New()
	router.NoRoute(GlobalWebRateLimit(frontendFS), ServeFrontendFiles(frontendFS), func(c *gin.Context) {
		c.String(http.StatusOK, "page")
	})

	for i := 0; i < 5; i++ {
		response := performRateLimitedRequest(router, "203.0.113.10:1001", "/static/js/app.js")
		require.Equal(t, http.StatusOK, response.Code, "existing build files must not spend the page limit while the static limiter is off")
	}
	require.Equal(t, http.StatusOK, performRateLimitedRequest(router, "203.0.113.10:1001", "/console").Code)
	require.Equal(t, http.StatusOK, performRateLimitedRequest(router, "203.0.113.10:1001", "/missing.js").Code)
	limited := performRateLimitedRequest(router, "203.0.113.10:1001", "/console")
	require.Equal(t, http.StatusTooManyRequests, limited.Code)
	require.Contains(t, limited.Body.String(), "Too many requests")
}

func TestGlobalWebRateLimitUsesSeparateStaticBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	restore := setWebRateLimitForTest(t, true, 5, 60, true, 2, 60)
	defer restore()

	frontendDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(frontendDir, "static", "js"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(frontendDir, "static", "js", "app.js"), []byte("export const ready = true\n"), 0o644))
	frontendFS := static.LocalFile(frontendDir, false)

	router := gin.New()
	router.NoRoute(GlobalWebRateLimit(frontendFS), ServeFrontendFiles(frontendFS), func(c *gin.Context) {
		c.String(http.StatusOK, "page")
	})

	require.Equal(t, http.StatusOK, performRateLimitedRequest(router, "203.0.113.11:1001", "/static/js/app.js").Code)
	require.Equal(t, http.StatusOK, performRateLimitedRequest(router, "203.0.113.11:1001", "/static/js/app.js").Code)
	limited := performRateLimitedRequest(router, "203.0.113.11:1001", "/static/js/app.js")
	require.Equal(t, http.StatusTooManyRequests, limited.Code)
	require.Equal(t, http.StatusOK, performRateLimitedRequest(router, "203.0.113.11:1001", "/console").Code)
}

func setWebRateLimitForTest(t *testing.T, webEnabled bool, webNum int, webDuration int64, staticEnabled bool, staticNum int, staticDuration int64) func() {
	t.Helper()
	oldRedis := common.RedisEnabled
	oldWebEnabled := common.GlobalWebRateLimitEnable
	oldWebNum := common.GlobalWebRateLimitNum
	oldWebDuration := common.GlobalWebRateLimitDuration
	oldStaticEnabled := common.GlobalStaticRateLimitEnable
	oldStaticNum := common.GlobalStaticRateLimitNum
	oldStaticDuration := common.GlobalStaticRateLimitDuration
	common.RedisEnabled = false
	common.GlobalWebRateLimitEnable = webEnabled
	common.GlobalWebRateLimitNum = webNum
	common.GlobalWebRateLimitDuration = webDuration
	common.GlobalStaticRateLimitEnable = staticEnabled
	common.GlobalStaticRateLimitNum = staticNum
	common.GlobalStaticRateLimitDuration = staticDuration
	return func() {
		common.RedisEnabled = oldRedis
		common.GlobalWebRateLimitEnable = oldWebEnabled
		common.GlobalWebRateLimitNum = oldWebNum
		common.GlobalWebRateLimitDuration = oldWebDuration
		common.GlobalStaticRateLimitEnable = oldStaticEnabled
		common.GlobalStaticRateLimitNum = oldStaticNum
		common.GlobalStaticRateLimitDuration = oldStaticDuration
	}
}

func performRateLimitedRequest(router http.Handler, remoteAddr, target string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.RemoteAddr = remoteAddr
	router.ServeHTTP(recorder, request)
	return recorder
}
