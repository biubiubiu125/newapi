package relay

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPluginResponsesFinalMetadataDropsSignedLocators(t *testing.T) {
	machine := NewPluginResponsesMachine("task_public", "demo-model", 10, DefaultPluginProtocolLimits())
	response, err := machine.FinalResponse(map[string]any{
		"output": []any{
			map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type":        "output_text",
						"text":        "ok",
						"annotations": []any{},
						"logprobs":    []any{},
					},
				},
			},
		},
		"metadata": map[string]any{
			"label":      "safe",
			"result_url": "https://cdn.example/signed?token=abc",
			"href":       "https://evil.example/file",
			"note":       "http://10.0.0.8/secret",
			"preview":    "data:video/mp4;base64,AAAA",
		},
	}, "SUCCESS")
	require.NoError(t, err)
	require.Equal(t, "resp_public", response["id"])
	require.Equal(t, "response", response["object"])
	require.Equal(t, int64(10), response["created_at"])
	require.Equal(t, "completed", response["status"])
	require.Nil(t, response["error"])

	metadata, ok := response["metadata"].(map[string]string)
	require.True(t, ok)
	require.Equal(t, "safe", metadata["label"])
	require.Equal(t, "task_public", metadata["task_id"])
	require.NotContains(t, metadata, "result_url")
	require.NotContains(t, metadata, "href")
	require.NotContains(t, metadata, "note")
	require.NotContains(t, metadata, "preview")
	for _, value := range metadata {
		require.NotContains(t, value, "cdn.example")
		require.NotContains(t, value, "evil.example")
		require.NotContains(t, value, "10.0.0.8")
		require.NotContains(t, value, "data:video")
		require.NotContains(t, value, "token=abc")
	}
}
