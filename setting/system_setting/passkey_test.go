package system_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestPasskeySettingsSnapshotDoesNotMutateGlobalAndStripsPort(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	previousSettings := defaultPasskeySettings
	previousAddress := ServerAddress
	defaultPasskeySettings = PasskeySettings{Enabled: false}
	ServerAddress = "https://panel.example.com:8443"
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defaultPasskeySettings = previousSettings
		ServerAddress = previousAddress
		common.OptionMapRWMutex.Unlock()
	})

	snapshot := PasskeySettingsSnapshot()
	require.Equal(t, "panel.example.com", snapshot.RPID)
	require.Equal(t, "https://panel.example.com:8443", snapshot.Origins)

	settings := GetPasskeySettings()
	require.Equal(t, "panel.example.com", settings.RPID)
	settings.Enabled = true

	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	require.Empty(t, defaultPasskeySettings.RPID)
	require.Empty(t, defaultPasskeySettings.Origins)
	require.False(t, defaultPasskeySettings.Enabled)
}
