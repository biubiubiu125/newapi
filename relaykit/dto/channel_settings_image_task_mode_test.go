package dto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelOtherSettingsGetImageTaskModeIgnoresRemovedAsyncTaskBridge(t *testing.T) {
	for _, mode := range []string{"", ImageTaskModeSyncWrapper, ImageTaskModeAsyncTaskBridge, "gpt_image2api_async"} {
		settings := &ChannelOtherSettings{ImageTaskMode: mode}
		require.Equal(t, ImageTaskModeSyncWrapper, settings.GetImageTaskMode())
	}
	require.Equal(t, ImageTaskModeSyncWrapper, (*ChannelOtherSettings)(nil).GetImageTaskMode())
}

func TestChannelOtherSettingsGetImageTaskModeRejectsLegacyAsyncTaskBridgeValue(t *testing.T) {
	settings := &ChannelOtherSettings{ImageTaskMode: "gpt_image2api_async"}

	require.Equal(t, ImageTaskModeSyncWrapper, settings.GetImageTaskMode())
}
