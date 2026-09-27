package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelDisableNoticeDropsUntranslatedReason(t *testing.T) {
	_, english := ChannelDisableNotice("openai", 7, "your credit balance is too low")
	require.Contains(t, english, "余额不足")
	require.NotContains(t, english, "credit balance")
	require.NotContains(t, english, BalanceInsufficientDisableReasonPrefix)

	_, prefixed := ChannelDisableNotice("openai", 7, BalanceInsufficientDisableReasonPrefix+" quota exceeded")
	require.Contains(t, prefixed, "余额不足")
	require.NotContains(t, prefixed, "quota exceeded")

	_, unknown := ChannelDisableNotice("openai", 7, "connection reset by peer")
	require.Equal(t, "通道「openai」（#7）已被禁用", unknown)
	require.NotContains(t, unknown, "connection reset")

	_, masked := ChannelDisableNotice("openai", 7, "status_code=401, upstream https://***.com/***?token=*** api_key:***")
	require.Contains(t, masked, "status_code=401")
	require.NotContains(t, masked, "review-token")

	_, rawStatus := ChannelDisableNotice("openai", 7, "status_code=401, upstream https://private.example.com/path?token=review-token")
	require.NotContains(t, rawStatus, "review-token")
	require.NotContains(t, rawStatus, "status_code=401")

	_, chinese := ChannelDisableNotice("openai", 7, "上游返回额度不足")
	require.Contains(t, chinese, "上游返回额度不足")
}
