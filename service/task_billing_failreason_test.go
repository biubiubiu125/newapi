package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTaskRefundAllowsMissingChannelMatchesEnglishAndLegacyChinese(t *testing.T) {
	require.True(t, taskRefundAllowsMissingChannel("获取渠道信息失败，请联系管理员，渠道ID：3"))
	require.True(t, taskRefundAllowsMissingChannel("Failed to get channel info"))
	require.True(t, taskRefundAllowsMissingChannel(
		"failed to get channel information, please contact the administrator, channel id: 3",
	))
	require.False(t, taskRefundAllowsMissingChannel("upstream task id is empty"))
}
