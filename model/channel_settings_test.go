package model

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"

	"github.com/stretchr/testify/require"
)

func TestValidateSettingsRejectsInvalidImageTaskMode(t *testing.T) {
	channel := &Channel{
		OtherSettings: `{"image_task_mode":"invalid"}`,
	}

	require.ErrorContains(t, channel.ValidateSettings(), "image_task_mode is invalid")
}

func TestValidateSettingsRequiresBaseURLForGPTImage2APIAsyncMode(t *testing.T) {
	channel := &Channel{
		OtherSettings: `{"image_task_mode":"` + dto.ImageTaskModeGPTImage2APIAsync + `"}`,
	}

	require.ErrorContains(t, channel.ValidateSettings(), "gpt_image2api 异步模式必须配置")

	baseURL := "https://gpt-image2api.example.com"
	channel.BaseURL = &baseURL
	require.NoError(t, channel.ValidateSettings())
}
