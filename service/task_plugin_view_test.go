package service

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
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

func TestBuildTaskPluginViewUsesPublicStatusDuringRetryableSettlementReview(t *testing.T) {
	view, err := BuildTaskPluginView(&model.Task{
		TaskID:           "task_plugin_view_review",
		Platform:         constant.TaskPlatform("gemini"),
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusReview,
		Progress:         "100%",
		FinishTime:       123,
		NextPollAt:       456,
		FailReason:       "billing settlement requires manual review",
		Data:             []byte(`{"url":"https://cdn.example/video.mp4"}`),
	})
	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusInProgress), view.Status)
	assert.Equal(t, "99%", view.Progress)
	assert.Zero(t, view.FinishedAt)
	assert.Empty(t, view.FailReason)
}
