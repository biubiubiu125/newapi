package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestKlingRequestConvertRewritesFetchPathWithoutSelectingChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/kling/v1/videos/text2video/task_abc", nil)
	c.Params = gin.Params{{Key: "task_id", Value: "task_abc"}}

	KlingRequestConvert()(c)

	require.Equal(t, "/v1/video/generations/task_abc", c.Request.URL.Path)
	require.Equal(t, "task_abc", c.GetString("task_id"))
	mode, exists := c.Get("relay_mode")
	require.True(t, exists)
	require.Equal(t, relayconstant.RelayModeVideoFetchByID, mode)
}
