package service

import (
	"testing"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

func TestQuotaNotifyCopyFollowsUserLanguage(t *testing.T) {
	require.NoError(t, i18n.Init())

	zhTitle, zhHTML := quotaNotifyCopy("zh-CN", dto.NotifyTypeEmail, "quota", "$1", "https://example/wallet")
	require.Equal(t, "额度预警通知", zhTitle)
	require.Contains(t, zhHTML, "当前剩余额度为 $1")
	require.Contains(t, zhHTML, "https://example/wallet")

	enTitle, enHTML := quotaNotifyCopy("en", dto.NotifyTypeEmail, "quota", "$1", "https://example/wallet")
	require.Equal(t, "Quota warning", enTitle)
	require.Contains(t, enHTML, "Remaining quota is $1")
	require.NotContains(t, enHTML, "额度")
	require.Contains(t, enHTML, "https://example/wallet")

	_, bark := quotaNotifyCopy("en", dto.NotifyTypeBark, "subscription", "$2", "https://example/wallet")
	require.NotContains(t, bark, "订阅")
	require.Contains(t, bark, "$2")
}
