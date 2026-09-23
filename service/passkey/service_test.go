package passkey

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
)

func TestBuildWebAuthnHTTPOriginReturnsLocalizedError(t *testing.T) {
	previousAddress := system_setting.ServerAddress
	system_setting.ServerAddress = ""
	restore := system_setting.OverridePasskeySettingsForTest(system_setting.PasskeySettings{
		Enabled:             true,
		AllowInsecureOrigin: false,
		Origins:             "",
	})
	t.Cleanup(func() {
		system_setting.ServerAddress = previousAddress
		restore()
	})

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api/user/passkey/register/begin", nil)
	_, err := BuildWebAuthn(req)
	loc, ok := common.AsLocalizedError(err)
	require.True(t, ok, "insecure origin should be LocalizedError, got %v", err)
	require.Equal(t, "passkey.https_required", loc.Key)
}
