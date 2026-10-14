package hailuo

import (
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/require"
)

func TestBuildVideoURLHonorsChannelTLSSkipVerify(t *testing.T) {
	service.InitHttpClient()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"file":{"download_url":"https://cdn.example/video.mp4"},"base_resp":{"status_code":0}}`))
	}))
	t.Cleanup(server.Close)

	fetch := func(skip bool) string {
		t.Helper()
		adaptor := &TaskAdaptor{}
		adaptor.Init(&relaycommon.RelayInfo{
			ChannelMeta: &relaycommon.ChannelMeta{
				ApiKey:         "hailuo-key",
				ChannelBaseUrl: server.URL,
				ChannelSetting: dto.ChannelSettings{TLSInsecureSkipVerify: skip},
			},
		})
		return adaptor.buildVideoURL("", "file-1")
	}

	require.Empty(t, fetch(false))
	require.Equal(t, "https://cdn.example/video.mp4", fetch(true))
}
