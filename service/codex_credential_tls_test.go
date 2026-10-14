package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

func TestCodexOAuthClientVerifiesCertificatesWhenChannelSkipsTLS(t *testing.T) {
	InitHttpClient()
	previous := common.TLSInsecureSkipVerify
	common.TLSInsecureSkipVerify = false
	t.Cleanup(func() { common.TLSInsecureSkipVerify = previous })

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	client, err := getCodexOAuthHTTPClientWithSettings("", dto.ChannelSettings{TLSInsecureSkipVerify: true})
	require.NoError(t, err)
	resp, err := client.Get(server.URL)
	require.Error(t, err)
	if resp != nil {
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
	}
}

func TestCodexReleaseLookupVerifiesCertificatesWhenChannelSkipsTLS(t *testing.T) {
	InitHttpClient()
	previous := common.TLSInsecureSkipVerify
	common.TLSInsecureSkipVerify = false
	t.Cleanup(func() { common.TLSInsecureSkipVerify = previous })

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	setting := `{"tls_insecure_skip_verify":true}`
	client, err := newCodexReleaseHTTPClient(&model.Channel{Setting: &setting})
	require.NoError(t, err)
	resp, err := client.Get(server.URL)
	require.Error(t, err)
	if resp != nil {
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
	}
}

func TestCodexProviderModelFetchStillSkipsChannelTLS(t *testing.T) {
	InitHttpClient()
	previous := common.TLSInsecureSkipVerify
	common.TLSInsecureSkipVerify = false
	t.Cleanup(func() { common.TLSInsecureSkipVerify = previous })

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	setting := `{"tls_insecure_skip_verify":true}`
	client, err := newCodexChannelHTTPClient(&model.Channel{Setting: &setting})
	require.NoError(t, err)
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	require.NotNil(t, resp)
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
}
