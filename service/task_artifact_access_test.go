package service

import (
	"net/url"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateTaskArtifactBaseURLRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{
		"",
		" ftp://media.example/tasks",
		"ftp://media.example/tasks",
		"https://user:secret@media.example/tasks",
		"https://media.example/tasks?token=secret",
		"https://media.example/tasks#preview",
	} {
		assert.Error(t, ValidateTaskArtifactBaseURL(value), value)
	}
}

func TestBuildTaskArtifactResultURLFallsBackToServerAddress(t *testing.T) {
	oldSecret := common.CryptoSecret
	oldPublicAddress := system_setting.TaskPublicAddress
	oldServerAddress := system_setting.ServerAddress
	common.CryptoSecret = "task-artifact-fallback-secret"
	system_setting.TaskPublicAddress = ""
	system_setting.ServerAddress = "https://server.example/gateway/"
	t.Cleanup(func() {
		common.CryptoSecret = oldSecret
		system_setting.TaskPublicAddress = oldPublicAddress
		system_setting.ServerAddress = oldServerAddress
	})

	resultURL, err := BuildTaskArtifactResultURL("task-fallback")
	require.NoError(t, err)
	parsed, err := url.Parse(resultURL)
	require.NoError(t, err)
	assert.Equal(t, "https", parsed.Scheme)
	assert.Equal(t, "server.example", parsed.Host)
	assert.Equal(t, "/gateway/v1/image-tasks/task-fallback/result", parsed.Path)
	assert.True(t, VerifyTaskArtifactAccess(
		parsed.Query().Get(TaskArtifactAccessQueryParameter),
		"task-fallback",
		TaskArtifactResultArtifactKey,
	))
}
