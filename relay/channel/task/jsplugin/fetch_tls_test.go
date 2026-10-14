package jsplugin

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/require"
)

func TestDoFetchDescriptorHonorsChannelTLSSkipVerify(t *testing.T) {
	service.InitHttpClient()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	fetch := func(skip bool) (*http.Response, error) {
		t.Helper()
		adaptor := New(&pluginruntime.LoadedPlugin{Meta: pluginruntime.Meta{Key: "tls-probe"}})
		adaptor.Init(&relaycommon.RelayInfo{
			ChannelMeta: &relaycommon.ChannelMeta{
				ChannelSetting: dto.ChannelSettings{TLSInsecureSkipVerify: skip},
			},
		})
		return adaptor.doFetchDescriptor(context.Background(), server.URL, "", map[string]any{
			"url":    server.URL + "/task",
			"method": http.MethodGet,
		})
	}

	_, err := fetch(false)
	require.Error(t, err)

	resp, err := fetch(true)
	require.NoError(t, err)
	require.NotNil(t, resp)
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
}
