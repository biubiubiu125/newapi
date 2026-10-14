package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

type recordingSunoPollAdaptor struct {
	inited  bool
	setting dto.ChannelSettings
	proxy   string
}

func (a *recordingSunoPollAdaptor) Init(info *relaycommon.RelayInfo) {
	a.inited = true
	if info != nil && info.ChannelMeta != nil {
		a.setting = info.ChannelSetting
	}
}

func (a *recordingSunoPollAdaptor) FetchTask(_, _ string, _ map[string]any, proxy string) (*http.Response, error) {
	a.proxy = proxy
	if !a.inited || !a.setting.TLSInsecureSkipVerify {
		return nil, http.ErrNotSupported
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"code":"success","data":[]}`)),
	}, nil
}

func (a *recordingSunoPollAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return nil, nil
}

func (a *recordingSunoPollAdaptor) AdjustBillingOnComplete(*model.Task, *relaycommon.TaskInfo) int {
	return 0
}

func TestUpdateSunoTaskBatchStoresTLSSkipBeforeFetch(t *testing.T) {
	baseURL := "https://suno.example"
	setting := `{"proxy":"http://127.0.0.1:8080","tls_insecure_skip_verify":true}`
	channel := &model.Channel{
		Id:      7,
		Type:    1,
		BaseURL: &baseURL,
		Setting: &setting,
	}
	adaptor := &recordingSunoPollAdaptor{}

	err := updateSunoTaskBatch(context.Background(), channel.Id, nil, nil, channel, adaptor, sunoTaskBatch{key: "batch-key"})
	require.NoError(t, err)
	require.True(t, adaptor.inited)
	require.True(t, adaptor.setting.TLSInsecureSkipVerify)
	require.Equal(t, "http://127.0.0.1:8080", adaptor.setting.Proxy)
	require.Equal(t, "http://127.0.0.1:8080", adaptor.proxy)
}
