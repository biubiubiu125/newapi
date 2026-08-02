package dto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelOtherSettingsGetImageTaskModeUsesAsyncTaskBridge(t *testing.T) {
	settings := &ChannelOtherSettings{ImageTaskMode: ImageTaskModeAsyncTaskBridge}

	require.Equal(t, ImageTaskModeAsyncTaskBridge, settings.GetImageTaskMode())
}

func TestChannelOtherSettingsGetImageTaskModeRejectsLegacyAsyncTaskBridgeValue(t *testing.T) {
	settings := &ChannelOtherSettings{ImageTaskMode: "gpt_image2api_async"}

	require.Equal(t, ImageTaskModeSyncWrapper, settings.GetImageTaskMode())
}
