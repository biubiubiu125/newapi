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

func TestTaskArtifactAccessRejectsUnsafePathSegments(t *testing.T) {
	oldSecret := common.CryptoSecret
	oldPublicAddress := system_setting.TaskPublicAddress
	oldServerAddress := system_setting.ServerAddress
	common.CryptoSecret = "task-artifact-path-secret"
	system_setting.TaskPublicAddress = "https://media.example/tasks"
	system_setting.ServerAddress = "https://server.example"
	t.Cleanup(func() {
		common.CryptoSecret = oldSecret
		system_setting.TaskPublicAddress = oldPublicAddress
		system_setting.ServerAddress = oldServerAddress
	})

	for _, testCase := range []struct {
		name       string
		taskID     string
		artifact   string
		resultPath bool
	}{
		{name: "task slash", taskID: "task/escape", artifact: "video"},
		{name: "task backslash", taskID: `task\escape`, artifact: "video"},
		{name: "task dot", taskID: "..", artifact: "video"},
		{name: "artifact slash", taskID: "task-safe", artifact: "video/other"},
		{name: "artifact control", taskID: "task-safe", artifact: "video\x00"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := IssueTaskArtifactAccess(testCase.taskID, testCase.artifact)
			assert.ErrorIs(t, err, ErrTaskArtifactAccessInvalid)

			_, err = BuildTaskArtifactContentURL(testCase.taskID, testCase.artifact)
			assert.ErrorIs(t, err, ErrTaskArtifactAccessInvalid)

			if testCase.resultPath {
				_, err = BuildTaskArtifactResultURL(testCase.taskID)
				assert.ErrorIs(t, err, ErrTaskArtifactAccessInvalid)
			}
		})
	}

	_, err := BuildTaskArtifactResultURL("task/escape")
	assert.ErrorIs(t, err, ErrTaskArtifactAccessInvalid)
}
