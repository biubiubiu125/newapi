package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	vertexcore "github.com/QuantumNous/new-api/relay/channel/vertex"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
)

func TestExtractVertexVideoURLFromPayloadPrefersHTTPSThenGCS(t *testing.T) {
	httpsURL := extractVertexVideoURLFromPayload([]byte(`{"response":{"videos":[{"uri":"https://cdn.example/video.mp4","gcsUri":"gs://bucket/video.mp4"}]}}`))
	require.Equal(t, "https://cdn.example/video.mp4", httpsURL)

	gcsURL := extractVertexVideoURLFromPayload([]byte(`{"response":{"videos":[{"gcsUri":"gs://bucket/video.mp4"}]}}`))
	require.Equal(t, "gs://bucket/video.mp4", gcsURL)

	generated := extractVertexVideoURLFromPayload([]byte(`{"response":{"generateVideoResponse":{"generatedVideos":[{"video":{"uri":"gs://bucket/generated.mp4"}}]}}}`))
	require.Equal(t, "gs://bucket/generated.mp4", generated)
}

func TestGetGeminiVideoURLUsesStoredDataResultURL(t *testing.T) {
	task := &model.Task{
		TaskID: "task_gemini_data",
		Status: model.TaskStatusSuccess,
		Data:   []byte(`{"response":{}}`),
	}
	task.PrivateData.ResultURL = "data:video/mp4;base64,AAAA"

	url, err := getGeminiVideoURL(&model.Channel{}, task, "unused-key")
	require.NoError(t, err)
	require.Equal(t, "data:video/mp4;base64,AAAA", url)
}

func TestGetVertexVideoURLUsesStoredGCSResultURL(t *testing.T) {
	task := &model.Task{
		TaskID: "task_vertex_legacy_gcs",
		Status: model.TaskStatusSuccess,
	}
	task.PrivateData.ResultURL = "gs://bucket/legacy.mp4"

	url, err := getVertexVideoURL(&model.Channel{}, task)
	require.NoError(t, err)
	require.Equal(t, "gs://bucket/legacy.mp4", url)
}

func TestGetVertexVideoURLUsesGCSURIWhenResultURLIsProxy(t *testing.T) {
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://api.example.com"
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	task := &model.Task{
		TaskID: "task_vertex_gcs",
		Status: model.TaskStatusSuccess,
		Data:   []byte(`{"response":{"videos":[{"gcsUri":"gs://bucket/video.mp4"}]}}`),
	}
	task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)

	url, err := getVertexVideoURL(&model.Channel{}, task)
	require.NoError(t, err)
	require.Equal(t, "gs://bucket/video.mp4", url)
}

func TestDecorateVertexMediaURLRewritesGCSWithAccessToken(t *testing.T) {
	previous := acquireVertexAccessToken
	acquireVertexAccessToken = func(creds vertexcore.Credentials, proxy string) (string, error) {
		require.Equal(t, "sa@example.com", creds.ClientEmail)
		require.Empty(t, proxy)
		return "stub-token", nil
	}
	t.Cleanup(func() { acquireVertexAccessToken = previous })

	channel := &model.Channel{Key: `{"client_email":"sa@example.com","private_key":"dummy"}`}
	url, headers, err := decorateVertexMediaURL(channel, &model.Task{}, "gs://bucket/path/video.mp4")
	require.NoError(t, err)
	require.Equal(t, "https://storage.googleapis.com/bucket/path/video.mp4", url)
	require.Equal(t, "Bearer stub-token", headers["Authorization"])
}

func TestDecorateVertexMediaURLKeepsDirectHTTPS(t *testing.T) {
	url, headers, err := decorateVertexMediaURL(&model.Channel{}, &model.Task{}, "https://cdn.example/video.mp4")
	require.NoError(t, err)
	require.Equal(t, "https://cdn.example/video.mp4", url)
	require.Empty(t, headers)
}

func TestDecorateVertexMediaURLAddsBearerForStorageHTTPS(t *testing.T) {
	previous := acquireVertexAccessToken
	acquireVertexAccessToken = func(creds vertexcore.Credentials, proxy string) (string, error) {
		require.Equal(t, "sa@example.com", creds.ClientEmail)
		require.Empty(t, proxy)
		return "stub-token", nil
	}
	t.Cleanup(func() { acquireVertexAccessToken = previous })

	channel := &model.Channel{Key: `{"client_email":"sa@example.com","private_key":"dummy"}`}
	url, headers, err := decorateVertexMediaURL(channel, &model.Task{}, "https://storage.googleapis.com/bucket/path/video.mp4")
	require.NoError(t, err)
	require.Equal(t, "https://storage.googleapis.com/bucket/path/video.mp4", url)
	require.Equal(t, "Bearer stub-token", headers["Authorization"])
}

func TestDecorateGeminiMediaURLRejectsGCS(t *testing.T) {
	url, headers, err := decorateGeminiMediaURL("gs://bucket/path/video.mp4", "gemini-key")
	require.Error(t, err)
	require.Contains(t, err.Error(), "gemini gcs uri not retrievable")
	require.Empty(t, url)
	require.Empty(t, headers)
}

func TestDecorateGeminiMediaURLKeepsDataURL(t *testing.T) {
	url, headers, err := decorateGeminiMediaURL("data:video/mp4;base64,AAAA", "gemini-key")
	require.NoError(t, err)
	require.Equal(t, "data:video/mp4;base64,AAAA", url)
	require.Empty(t, headers)
}

func TestDecorateGeminiMediaURLRejectsUnsignedStorageHTTPS(t *testing.T) {
	url, headers, err := decorateGeminiMediaURL("https://storage.googleapis.com/bucket/path/video.mp4", "gemini-key")
	require.Error(t, err)
	require.Contains(t, err.Error(), "gemini gcs uri not retrievable")
	require.Empty(t, url)
	require.Empty(t, headers)
}

func TestDecorateGeminiMediaURLKeepsSignedStorageHTTPSWithoutAPIKey(t *testing.T) {
	signed := "https://storage.googleapis.com/bucket/path/video.mp4?X-Goog-Algorithm=GOOG4-RSA-SHA256&X-Goog-Signature=deadbeef"
	url, headers, err := decorateGeminiMediaURL(signed, "gemini-key")
	require.NoError(t, err)
	require.Equal(t, signed, url)
	require.Empty(t, headers)
	require.NotContains(t, url, "key=")
}

func TestDecorateGeminiMediaURLKeepsCDNWithoutAPIKey(t *testing.T) {
	url, headers, err := decorateGeminiMediaURL("https://cdn.example/video.mp4", "gemini-key")
	require.NoError(t, err)
	require.Equal(t, "https://cdn.example/video.mp4", url)
	require.Empty(t, headers)
}

func TestDecorateGeminiMediaURLAddsAPIKeyForGeminiFiles(t *testing.T) {
	url, headers, err := decorateGeminiMediaURL("https://generativelanguage.googleapis.com/v1beta/files/abc:download", "gemini-key")
	require.NoError(t, err)
	require.Contains(t, url, "key=gemini-key")
	require.Equal(t, "gemini-key", headers["x-goog-api-key"])
}

func TestDecorateGeminiMediaURLAddsAPIKeyForChannelHost(t *testing.T) {
	url, headers, err := decorateGeminiMediaURL("https://gemini.internal/legacy/video.mp4", "channel-secret", "gemini.internal")
	require.NoError(t, err)
	require.Contains(t, url, "key=channel-secret")
	require.Equal(t, "channel-secret", headers["x-goog-api-key"])
}

func TestGetGeminiVideoURLDoesNotAttachKeyToCDN(t *testing.T) {
	task := &model.Task{
		TaskID: "task_gemini_cdn",
		Status: model.TaskStatusSuccess,
	}
	task.PrivateData.ResultURL = "https://cdn.example/video.mp4"

	url, err := getGeminiVideoURL(&model.Channel{}, task, "gemini-key")
	require.NoError(t, err)
	require.Equal(t, "https://cdn.example/video.mp4", url)
}
