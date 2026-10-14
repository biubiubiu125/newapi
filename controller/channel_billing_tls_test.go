package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/require"
)

func TestGetResponseBodyHonorsChannelTLSSkipVerify(t *testing.T) {
	service.InitHttpClient()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)

	channelWith := func(skip bool) *model.Channel {
		raw := "{}"
		if skip {
			raw = `{"tls_insecure_skip_verify":true}`
		}
		return &model.Channel{Setting: &raw}
	}

	_, err := GetResponseBody(http.MethodGet, server.URL, channelWith(false), nil)
	require.Error(t, err)

	body, err := GetResponseBody(http.MethodGet, server.URL, channelWith(true), nil)
	require.NoError(t, err)
	require.JSONEq(t, `{"ok":true}`, string(body))
}
