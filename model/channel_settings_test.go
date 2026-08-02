package model

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/stretchr/testify/require"
)

func TestValidateSettingsRejectsInvalidImageTaskMode(t *testing.T) {
	channel := &Channel{
		OtherSettings: `{"image_task_mode":"invalid"}`,
	}

	require.ErrorContains(t, channel.ValidateSettings(), "image_task_mode is invalid")
}

func TestValidateSettingsRejectsLegacyImageTaskMode(t *testing.T) {
	channel := &Channel{
		OtherSettings: `{"image_task_mode":"gpt_image2api_async"}`,
	}

	require.ErrorContains(t, channel.ValidateSettings(), "image_task_mode is invalid")
}

func TestValidateSettingsRequiresBaseURLForAsyncTaskBridgeMode(t *testing.T) {
	channel := &Channel{
		OtherSettings: `{"image_task_mode":"` + dto.ImageTaskModeAsyncTaskBridge + `"}`,
	}

	require.ErrorContains(t, channel.ValidateSettings(), "异步任务桥接模式必须配置")

	baseURL := "https://async-task-bridge.example.com"
	channel.BaseURL = &baseURL
	require.NoError(t, channel.ValidateSettings())
}
