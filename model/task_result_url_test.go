package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetResultURLDoesNotExposeFailureReason(t *testing.T) {
	task := &Task{
		Status:     TaskStatusFailure,
		FailReason: "https://provider.example/error-details",
	}

	assert.Empty(t, task.GetResultURL())
}

func TestGetResultURLKeepsSuccessfulLegacyURLCompatibility(t *testing.T) {
	task := &Task{
		Status:     TaskStatusSuccess,
		FailReason: "https://provider.example/video.mp4",
	}

	assert.Equal(t, "https://provider.example/video.mp4", task.GetResultURL())
}

func TestGetResultURLIgnoresNonURLSuccessReason(t *testing.T) {
	task := &Task{
		Status:     TaskStatusSuccess,
		FailReason: "provider completed with a diagnostic message",
	}

	assert.Empty(t, task.GetResultURL())
}
