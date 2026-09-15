package hailuo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

func TestInitStoresChannelProxy(t *testing.T) {
	adaptor := &TaskAdaptor{}
	adaptor.Init(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeMiniMax,
			ChannelBaseUrl: "https://api.minimax.chat",
			ApiKey:         "hailuo-key",
			ChannelSetting: dto.ChannelSettings{Proxy: "socks5://127.0.0.1:1080"},
		},
	})
	require.Equal(t, "socks5://127.0.0.1:1080", adaptor.proxy)
}

func TestParseTaskResultUsesProxyWhenResolvingFileURL(t *testing.T) {
	var gotProxy string
	previous := hailuoHTTPClient
	t.Cleanup(func() { hailuoHTTPClient = previous })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/files/retrieve", r.URL.Path)
		require.Equal(t, "file-1", r.URL.Query().Get("file_id"))
		_ = json.NewEncoder(w).Encode(RetrieveFileResponse{
			File:     FileObject{DownloadURL: "https://cdn.minimax.example/video.mp4"},
			BaseResp: BaseResp{StatusCode: StatusSuccess},
		})
	}))
	t.Cleanup(server.Close)

	hailuoHTTPClient = func(proxy string) (*http.Client, error) {
		gotProxy = proxy
		return server.Client(), nil
	}

	adaptor := &TaskAdaptor{
		baseURL: server.URL,
		apiKey:  "hailuo-key",
		proxy:   "socks5://127.0.0.1:1080",
	}
	ti, err := adaptor.ParseTaskResult([]byte(`{"task_id":"t1","status":"Success","file_id":"file-1","base_resp":{"status_code":0}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, ti.Status)
	require.Equal(t, "https://cdn.minimax.example/video.mp4", ti.Url)
	require.Equal(t, "socks5://127.0.0.1:1080", gotProxy)
}

func TestParseTaskResultDoesNotOverrideAPIFailureWithSuccessStatus(t *testing.T) {
	previous := hailuoHTTPClient
	t.Cleanup(func() { hailuoHTTPClient = previous })
	hailuoHTTPClient = func(string) (*http.Client, error) {
		t.Fatal("api failure must not retrieve file url")
		return nil, nil
	}

	adaptor := &TaskAdaptor{baseURL: "https://api.minimax.chat", apiKey: "hailuo-key"}
	ti, err := adaptor.ParseTaskResult([]byte(`{"task_id":"t1","status":"Success","file_id":"file-1","base_resp":{"status_code":1008,"status_msg":"no balance"}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusFailure, ti.Status)
	require.Equal(t, 1008, ti.Code)
	require.Equal(t, "no balance", ti.Reason)
	require.Equal(t, "100%", ti.Progress)
	require.Empty(t, ti.Url)
}

func TestParseTaskResultDoesNotOverrideAPIFailureWithProcessingStatus(t *testing.T) {
	adaptor := &TaskAdaptor{}
	ti, err := adaptor.ParseTaskResult([]byte(`{"task_id":"t1","status":"Processing","base_resp":{"status_code":1002,"status_msg":"rate limited"}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusFailure, ti.Status)
	require.Equal(t, 1002, ti.Code)
	require.Equal(t, "rate limited", ti.Reason)
	require.Equal(t, "100%", ti.Progress)
}

func TestConvertToOpenAIVideoSanitizesProviderFailure(t *testing.T) {
	adaptor := &TaskAdaptor{}
	data, err := json.Marshal(QueryTaskResponse{
		TaskID: "upstream-hailuo",
		Status: TaskStatusFailed,
		BaseResp: BaseResp{
			StatusCode: 1008,
			StatusMsg:  "no balance dsn=postgres://user:secret@10.0.0.8:5432/newapi",
		},
	})
	require.NoError(t, err)

	rendered, err := adaptor.ConvertToOpenAIVideo(&model.Task{
		TaskID:     "task_hailuo_fail",
		Status:     model.TaskStatusFailure,
		Progress:   "100%",
		FailReason: "pq: password authentication failed for user newapi",
		Data:       data,
		Properties: model.Properties{OriginModelName: "MiniMax-Hailuo-02"},
	})
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, json.Unmarshal(rendered, &video))
	require.Equal(t, "task_hailuo_fail", video.ID)
	require.Equal(t, dto.VideoStatusFailed, video.Status)
	require.NotNil(t, video.Error)
	require.Equal(t, model.TaskPublicInternalFailReason, video.Error.Message)
	require.Equal(t, "task_failed", video.Error.Code)
	require.Nil(t, video.Metadata)
	require.NotContains(t, string(rendered), "password")
	require.NotContains(t, string(rendered), "no balance")
	require.NotContains(t, string(rendered), "10.0.0.8")
	require.NotContains(t, string(rendered), "secret")
}
