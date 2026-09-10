package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildTaskPluginViewHidesLegacyResultURLFromSuccessfulFailureReason(t *testing.T) {
	tests := []struct {
		name       string
		failReason string
		want       string
	}{
		{
			name:       "http url",
			failReason: "https://provider.example/video.mp4",
		},
		{
			name:       "data url",
			failReason: "data:video/mp4;base64,AAAA",
		},
		{
			name:       "diagnostic reason",
			failReason: "provider returned an empty video",
			want:       "provider returned an empty video",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view, err := BuildTaskPluginView(&model.Task{
				TaskID:     "task_plugin_view",
				Status:     model.TaskStatusSuccess,
				FailReason: tt.failReason,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.want, view.FailReason)
		})
	}
}
