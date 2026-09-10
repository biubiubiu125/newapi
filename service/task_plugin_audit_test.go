package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTaskExecutionSnapshotCapturesExactPluginSource(t *testing.T) {
	const source = `
export const meta = {apiVersion: 1, key: "snapshot-source-capture", name: "Snapshot Source Capture", version: "1.0.0", author: {name: "Test"}, models: ["snapshot-source-capture-model"], fetchMode: "per_task"};
export function buildSubmitRequest() { return {}; }
export function parseSubmitResponse() { return {}; }
export function buildQueryRequest() { return {}; }
export function parseTaskResult() { return {}; }
`
	registry := pluginruntime.NewRegistry()
	plugin, err := registry.RegisterFactory(source, pluginruntime.Options{})
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(nil)
	context.Set(common.RequestIdKey, "request-id")
	context.Set(pluginruntime.ContextKeyPinnedPlugin, pluginruntime.PinnedPlugin{
		Generation: registry.Generation(),
		Plugin:     plugin,
	})

	snapshot := TaskExecutionSnapshotFromContext(context)
	require.NotNil(t, snapshot)
	require.NotNil(t, snapshot.TaskPlugin)
	require.Equal(t, source, snapshot.TaskPlugin.Source)
}
