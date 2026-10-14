package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
)

func TestMidjourneyImageHTTPClientHonorsChannelTLSSkipVerify(t *testing.T) {
	service.InitHttpClient()
	fetch := system_setting.GetFetchSetting()
	previous := *fetch
	fetch.EnableSSRFProtection = false
	t.Cleanup(func() { *fetch = previous })

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	setting := `{"tls_insecure_skip_verify":true}`
	client, proxy, err := midjourneyImageHTTPClient(&model.Channel{Setting: &setting})
	require.NoError(t, err)
	require.Empty(t, proxy)
	require.NotNil(t, client)
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	fallback, proxy, err := midjourneyImageHTTPClient(&model.Channel{})
	require.NoError(t, err)
	require.Empty(t, proxy)
	require.Nil(t, fallback)
	_, err = service.GetSSRFProtectedHTTPClient().Get(server.URL)
	require.Error(t, err)
}
