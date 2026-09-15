package service

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
)

func TestRequirePublicCallbackAddressRejectsEmptyAndLocal(t *testing.T) {
	previousCustom := operation_setting.CustomCallbackAddress
	previousServer := system_setting.ServerAddress
	t.Cleanup(func() {
		operation_setting.CustomCallbackAddress = previousCustom
		system_setting.ServerAddress = previousServer
	})

	operation_setting.CustomCallbackAddress = ""
	system_setting.ServerAddress = ""
	_, err := RequirePublicCallbackAddress()
	require.ErrorIs(t, err, ErrPublicCallbackAddressNotConfigured)

	system_setting.ServerAddress = "http://localhost:3000"
	_, err = RequirePublicCallbackAddress()
	require.ErrorIs(t, err, ErrPublicCallbackAddressNotConfigured)

	system_setting.ServerAddress = "https://pay.example.com"
	addr, err := RequirePublicCallbackAddress()
	require.NoError(t, err)
	require.Equal(t, "https://pay.example.com", addr)

	system_setting.ServerAddress = "http://192.168.1.10"
	_, err = RequirePublicCallbackAddress()
	require.ErrorIs(t, err, ErrPublicCallbackAddressNotConfigured)

	system_setting.ServerAddress = "http://10.0.0.8"
	_, err = RequirePublicCallbackAddress()
	require.ErrorIs(t, err, ErrPublicCallbackAddressNotConfigured)

	system_setting.ServerAddress = "http://0.0.0.0:3000"
	_, err = RequirePublicCallbackAddress()
	require.ErrorIs(t, err, ErrPublicCallbackAddressNotConfigured)

	system_setting.ServerAddress = "http://[::]:3000"
	_, err = RequirePublicCallbackAddress()
	require.ErrorIs(t, err, ErrPublicCallbackAddressNotConfigured)
}
