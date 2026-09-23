package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConsoleStoredDetailDropsUntranslatedEnglish(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)

	zh := consoleLangContext(t, "zh-CN")
	checks := consoleStoredTaskError(zh, "failed channel checks: 3", false)
	require.Equal(t, "渠道检查失败：3", checks)
	require.NotContains(t, checks, "failed channel")

	cache := consoleStoredTaskError(zh, "runtime channel cache refresh failed: dial tcp 127.0.0.1:5432: connect: connection refused", false)
	require.Equal(t, "运行时渠道缓存刷新失败", cache)
	require.NotContains(t, cache, "dial tcp")

	keptCache := consoleStoredTaskError(zh, "runtime channel cache refresh failed: 数据库出错", false)
	require.Equal(t, "运行时渠道缓存刷新失败：数据库出错", keptCache)

	require.Equal(t, "任务已取消", consoleStoredTaskError(zh, "task cancelled by user", false))
	require.Equal(t, "任务超时", consoleStoredTaskError(zh, "context deadline exceeded", false))
	require.Equal(t, "", consoleStoredTaskError(zh, "dial tcp 127.0.0.1:5432", false))
	require.Equal(t, "请求失败", consoleStoredTaskError(zh, "dial tcp 127.0.0.1:5432", true))
	require.Equal(t, "上游返回额度不足", consoleStoredTaskError(zh, "上游返回额度不足", true))

	zhTW := consoleLangContext(t, "zh-TW")
	twChecks := consoleStoredTaskError(zhTW, "failed channel updates: 2", false)
	require.Equal(t, "渠道更新失敗：2", twChecks)
	require.NotContains(t, twChecks, "failed channel")

	reason := consolePaymentOrphanReason(zh, "local order insert failed: dial tcp 127.0.0.1:5432: connect: connection refused")
	require.Equal(t, "本地订单写入失败", reason)
	require.NotContains(t, reason, "dial tcp")

	hanReason := consolePaymentOrphanReason(zh, "local order insert failed: 金额过小")
	require.Equal(t, "本地订单写入失败：金额过小", hanReason)

	review := consolePaymentOrphanReason(zh, "BEpusdt top-up payment requires manual review after payment succeeded")
	require.Equal(t, "BEpusdt 充值成功后需要人工核对", review)
	require.NotContains(t, review, "manual review")

	require.Equal(t, "这笔支付需要人工核对", consolePaymentOrphanReason(zh, "some gateway exploded"))
	require.Equal(t, "金额过小", consolePaymentOrphanReason(zh, "金额过小"))
	require.Equal(t, "", consolePaymentOrphanReason(zh, "  "))
	require.Equal(t, "", consolePaymentOrphanError(zh, "pq: password authentication failed for user newapi"))
	require.Equal(t, "金额过小", consolePaymentOrphanError(zh, "金额过小"))

	en := consoleLangContext(t, "en-US")
	raw := "local order insert failed: dial tcp 127.0.0.1:5432: connect: connection refused"
	require.Equal(t, raw, consolePaymentOrphanReason(en, raw))
	require.Equal(t, "failed channel checks: 3", consoleStoredTaskError(en, "failed channel checks: 3", true))
	require.Equal(t, "pq: password authentication failed", consolePaymentOrphanError(en, "pq: password authentication failed"))
}
