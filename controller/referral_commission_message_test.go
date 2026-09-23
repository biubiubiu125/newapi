package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestReferralCommissionAnomalyDropsUntranslatedEnglish(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)

	zh := referralCommissionAnomalyMessage(consoleLangContext(t, "zh-CN"), "affiliate_not_found")
	require.Contains(t, zh, "未找到推广员")
	require.NotContains(t, zh, "affiliate_not_found")

	unknown := referralCommissionAnomalyMessage(consoleLangContext(t, "zh-CN"), "dial tcp 127.0.0.1:5432")
	require.Equal(t, "返佣生成失败", unknown)
	require.NotContains(t, unknown, "dial tcp")

	kept := referralCommissionAnomalyMessage(consoleLangContext(t, "zh-CN"), "金额过小")
	require.Contains(t, kept, "金额过小")

	mixed := referralCommissionAnomalyMessage(consoleLangContext(t, "zh-CN"), "连接失败: dial tcp 127.0.0.1:5432: connect: connection refused")
	require.Contains(t, mixed, "连接失败")
	require.NotContains(t, mixed, "dial tcp")
	require.NotContains(t, mixed, "connection refused")

	en := referralCommissionAnomalyMessage(consoleLangContext(t, "en-US"), "affiliate_not_found")
	require.Contains(t, en, "Affiliate not found")
	require.NotContains(t, en, "affiliate_not_found")
}

func consoleLangContext(t *testing.T, accept string) *gin.Context {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	ctx.Request.Header.Set("Accept-Language", accept)
	return ctx
}
