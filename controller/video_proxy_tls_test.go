package controller

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/require"
)

func TestLegacyTaskFetchHonorsChannelTLSSkipVerify(t *testing.T) {
	service.InitHttpClient()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	newAdaptor := func() interface {
		Init(*relaycommon.RelayInfo)
		FetchTask(string, string, *model.Task, string) (*http.Response, error)
	} {
		t.Helper()
		adaptor := relay.GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeSora)))
		require.NotNil(t, adaptor)
		return adaptor
	}

	setting := `{"tls_insecure_skip_verify":true}`
	channel := &model.Channel{Type: constant.ChannelTypeSora, Setting: &setting}
	task := &model.Task{TaskID: "task-1"}

	_, err := newAdaptor().FetchTask(server.URL, "key", task, "")
	require.Error(t, err)

	adaptor := newAdaptor()
	initLegacyTaskFetch(adaptor, channel, server.URL, "key")
	resp, err := adaptor.FetchTask(server.URL, "key", task, "")
	require.NoError(t, err)
	require.NotNil(t, resp)
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
}
