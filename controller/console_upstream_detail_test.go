package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConsoleUpstreamDetailDropsUntranslatedEnglish(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)

	zhChannel := consoleUpstreamDetail(consoleLangContext(t, "zh-CN"), "dial tcp 127.0.0.1:5432: connect: connection refused", i18n.MsgChannelTestFailed)
	require.Equal(t, "测试渠道失败", zhChannel)
	require.NotContains(t, zhChannel, "dial tcp")

	zhRatio := consoleUpstreamDetail(consoleLangContext(t, "zh-CN"), "404 Not Found", i18n.MsgRatioSyncUpstreamFailed)
	require.Equal(t, "请求失败", zhRatio)
	require.NotContains(t, zhRatio, "Not Found")

	zhTW := consoleUpstreamDetail(consoleLangContext(t, "zh-TW"), "response time 1.50s exceeds threshold 1.00s", i18n.MsgChannelTestFailed)
	require.Equal(t, "測試渠道失敗", zhTW)
	require.NotContains(t, zhTW, "threshold")

	kept := consoleUpstreamDetail(consoleLangContext(t, "zh-CN"), "上游返回额度不足", i18n.MsgRatioSyncUpstreamFailed)
	require.Equal(t, "上游返回额度不足", kept)

	parsed := consoleUpstreamDetail(consoleLangContext(t, "zh-CN"), "无法解析上游返回数据", i18n.MsgRatioSyncUpstreamFailed)
	require.Equal(t, "无法解析上游返回数据", parsed)

	en := consoleUpstreamDetail(consoleLangContext(t, "en-US"), "dial tcp 127.0.0.1:5432: connect: connection refused", i18n.MsgChannelTestFailed)
	require.Equal(t, "dial tcp 127.0.0.1:5432: connect: connection refused", en)

	enEmpty := consoleUpstreamDetail(consoleLangContext(t, "en-US"), "  ", i18n.MsgRatioSyncUpstreamFailed)
	require.Equal(t, "Request failed", enEmpty)
}
