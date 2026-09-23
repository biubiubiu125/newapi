package service

import (
	"testing"

	"github.com/QuantumNous/new-api/i18n"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestPreConsumeTokenQuotaRejectsNegativeInEnglish(t *testing.T) {
	require.NoError(t, i18n.Init())
	err := PreConsumeTokenQuota(&relaycommon.RelayInfo{}, -1)
	require.Error(t, err)
	requireNoHan(t, err.Error())
	require.Contains(t, err.Error(), "cannot be negative")
}
