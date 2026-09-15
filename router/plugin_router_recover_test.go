package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type refundProbe struct {
	calls int
}

func (p *refundProbe) Settle(int) error          { return nil }
func (p *refundProbe) Refund(*gin.Context) error { p.calls++; return nil }
func (p *refundProbe) Rollback(int) error        { return nil }
func (p *refundProbe) NeedsRefund() bool         { return true }
func (p *refundProbe) GetPreConsumedQuota() int  { return 1 }
func (p *refundProbe) Reserve(int) error         { return nil }

func TestPluginRouteRecoveryRefundsBillingAfterPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	probe := &refundProbe{}
	engine := gin.New()
	engine.Use(pluginRouteRecovery())
	engine.GET("/v1/plugin-panic", func(c *gin.Context) {
		c.Set("relay_info", &relaycommon.RelayInfo{Billing: probe})
		panic(`pq: password authentication failed for user "newapi"`)
	})

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/plugin-panic", nil))

	require.Equal(t, 1, probe.calls)
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	body := recorder.Body.String()
	require.NotContains(t, strings.ToLower(body), "pq:")
	require.NotContains(t, strings.ToLower(body), "password")
	require.Contains(t, body, "Internal server error")
	require.Contains(t, body, `"type":"new_api_panic"`)
}

func TestPluginRouteRecoveryRefundsBillingAfterPartialWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	probe := &refundProbe{}
	engine := gin.New()
	engine.Use(pluginRouteRecovery())
	engine.GET("/v1/plugin-partial", func(c *gin.Context) {
		c.Set("relay_info", &relaycommon.RelayInfo{Billing: probe})
		c.JSON(http.StatusOK, gin.H{"ok": true})
		panic("after write")
	})

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/plugin-partial", nil))

	require.Equal(t, 1, probe.calls)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"ok":true}`, recorder.Body.String())
	require.NotContains(t, recorder.Body.String(), "plugin_route_error")
	require.NotContains(t, recorder.Body.String(), "new_api_panic")
}
