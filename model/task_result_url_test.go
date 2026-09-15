package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskMatchesRequestToken(t *testing.T) {
	legacy := &Task{PrivateData: TaskPrivateData{}}
	assert.True(t, legacy.MatchesRequestToken(0))
	assert.True(t, legacy.MatchesRequestToken(80))

	bound := &Task{PrivateData: TaskPrivateData{TokenId: 80}}
	assert.True(t, bound.MatchesRequestToken(0))
	assert.True(t, bound.MatchesRequestToken(80))
	assert.False(t, bound.MatchesRequestToken(81))
	assert.False(t, (*Task)(nil).MatchesRequestToken(80))
}

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

func TestGetResultURLRewritesDataURLToTaskProxy(t *testing.T) {
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://api.example.com"
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	task := &Task{
		TaskID: "task_data_result",
		Status: TaskStatusSuccess,
		PrivateData: TaskPrivateData{
			ResultURL: "data:video/mp4;base64,AAAA",
		},
	}

	assert.Equal(t, "https://api.example.com/v1/videos/task_data_result/content", task.GetResultURL())
}

func TestGetResultURLRewritesGCSURIToTaskProxy(t *testing.T) {
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://api.example.com"
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	task := &Task{
		TaskID: "task_gcs_result",
		Status: TaskStatusSuccess,
		PrivateData: TaskPrivateData{
			ResultURL: "gs://bucket/video.mp4",
		},
	}

	assert.Equal(t, "https://api.example.com/v1/videos/task_gcs_result/content", task.GetResultURL())
}

func TestGetResultURLIgnoresNonURLSuccessReason(t *testing.T) {
	task := &Task{
		Status:     TaskStatusSuccess,
		FailReason: "provider completed with a diagnostic message",
	}

	assert.Empty(t, task.GetResultURL())
}

func TestGetResultURLKeepsPublicHTTPS(t *testing.T) {
	task := &Task{
		TaskID: "task_public_https",
		Status: TaskStatusSuccess,
		PrivateData: TaskPrivateData{
			ResultURL: "https://cdn.example/video.mp4",
		},
	}

	assert.Equal(t, "https://cdn.example/video.mp4", task.GetResultURL())
}

func TestGetResultURLRewritesGeminiFilesHTTPS(t *testing.T) {
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://api.example.com"
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	task := &Task{
		TaskID: "task_gemini_files",
		Status: TaskStatusSuccess,
		PrivateData: TaskPrivateData{
			ResultURL: "https://generativelanguage.googleapis.com/v1beta/files/abc:download?alt=media",
		},
	}

	assert.Equal(t, "https://api.example.com/v1/videos/task_gemini_files/content", task.GetResultURL())
	assert.Equal(t, "https://generativelanguage.googleapis.com/v1beta/files/abc:download?alt=media", task.PrivateData.ResultURL)
}

func TestGetResultURLRewritesFilesGoogleapisHTTPS(t *testing.T) {
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://api.example.com"
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	task := &Task{
		TaskID: "task_files_googleapis",
		Status: TaskStatusSuccess,
		PrivateData: TaskPrivateData{
			ResultURL: "https://files.googleapis.com/v1beta/files/abc:download?alt=media",
		},
	}

	assert.Equal(t, "https://api.example.com/v1/videos/task_files_googleapis/content", task.GetResultURL())
	assert.Equal(t, "https://files.googleapis.com/v1beta/files/abc:download?alt=media", task.PrivateData.ResultURL)
}

func TestGetResultURLRewritesChannelMediaHostHTTPS(t *testing.T) {
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://api.example.com"
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	task := &Task{
		TaskID: "task_channel_media_host",
		Status: TaskStatusSuccess,
		PrivateData: TaskPrivateData{
			ResultURL:        "https://gemini.internal/legacy/video.mp4",
			ResultProxyHosts: []string{"gemini.internal"},
		},
	}

	assert.Equal(t, "https://api.example.com/v1/videos/task_channel_media_host/content", task.GetResultURL())
	assert.Equal(t, "https://gemini.internal/legacy/video.mp4", task.PrivateData.ResultURL)
}

func TestGetResultURLKeepsCDNWhenChannelHostDiffers(t *testing.T) {
	task := &Task{
		TaskID: "task_channel_cdn",
		Status: TaskStatusSuccess,
		PrivateData: TaskPrivateData{
			ResultURL:        "https://cdn.example/video.mp4",
			ResultProxyHosts: []string{"gemini.internal"},
		},
	}

	assert.Equal(t, "https://cdn.example/video.mp4", task.GetResultURL())
}

func TestGetResultURLRewritesHistoricalChannelMediaHostWithoutResultProxyHosts(t *testing.T) {
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://api.example.com"
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	baseURL := "https://gemini.internal/v1beta"
	channel := &Channel{
		Id:      912001,
		Type:    constant.ChannelTypeGemini,
		Name:    "historical-result-proxy-host",
		Key:     "sk-historical",
		Status:  common.ChannelStatusEnabled,
		BaseURL: &baseURL,
	}
	require.NoError(t, DB.Create(channel).Error)
	t.Cleanup(func() {
		_ = DB.Delete(&Channel{}, channel.Id).Error
	})

	task := &Task{
		TaskID:    "task_historical_channel_media_host",
		ChannelId: channel.Id,
		Status:    TaskStatusSuccess,
		PrivateData: TaskPrivateData{
			ResultURL: "https://gemini.internal/legacy/video.mp4",
		},
	}

	assert.Equal(t, "https://api.example.com/v1/videos/task_historical_channel_media_host/content", task.GetResultURL())
	assert.Equal(t, "https://gemini.internal/legacy/video.mp4", task.PrivateData.ResultURL)
	assert.Empty(t, task.PrivateData.ResultProxyHosts)
}

func TestGetResultURLRewritesHistoricalChannelMediaHostWhenMemoryCacheMisses(t *testing.T) {
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://api.example.com"
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	oldCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = oldCache })

	baseURL := "https://gemini.cachemiss/v1beta"
	channel := &Channel{
		Id:      912002,
		Type:    constant.ChannelTypeGemini,
		Name:    "historical-result-proxy-host-cache-miss",
		Key:     "sk-historical-cache-miss",
		Status:  common.ChannelStatusEnabled,
		BaseURL: &baseURL,
	}
	require.NoError(t, DB.Create(channel).Error)
	t.Cleanup(func() {
		_ = DB.Delete(&Channel{}, channel.Id).Error
	})

	task := &Task{
		TaskID:    "task_historical_channel_cache_miss",
		ChannelId: channel.Id,
		Status:    TaskStatusSuccess,
		PrivateData: TaskPrivateData{
			ResultURL: "https://gemini.cachemiss/legacy/video.mp4",
		},
	}

	assert.Equal(t, "https://api.example.com/v1/videos/task_historical_channel_cache_miss/content", task.GetResultURL())
	assert.Equal(t, "https://gemini.cachemiss/legacy/video.mp4", task.PrivateData.ResultURL)
	assert.Empty(t, task.PrivateData.ResultProxyHosts)
}

func TestGetResultURLRewritesHistoricalKeyedChannelStoredHostWhenLiveBaseURLChanged(t *testing.T) {
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://api.example.com"
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	baseURL := "https://gemini.current/v1beta"
	channel := &Channel{
		Id:      912003,
		Type:    constant.ChannelTypeGemini,
		Name:    "historical-keyed-host-changed",
		Key:     "sk-historical-keyed",
		Status:  common.ChannelStatusEnabled,
		BaseURL: &baseURL,
	}
	require.NoError(t, DB.Create(channel).Error)
	t.Cleanup(func() {
		_ = DB.Delete(&Channel{}, channel.Id).Error
	})

	task := &Task{
		TaskID:    "task_historical_keyed_host_changed",
		ChannelId: channel.Id,
		Status:    TaskStatusSuccess,
		PrivateData: TaskPrivateData{
			ResultURL: "https://gemini.submit-time/legacy/video.mp4",
		},
	}

	assert.Equal(t, "https://api.example.com/v1/videos/task_historical_keyed_host_changed/content", task.GetResultURL())
	assert.Equal(t, "https://gemini.submit-time/legacy/video.mp4", task.PrivateData.ResultURL)
	assert.Empty(t, task.PrivateData.ResultProxyHosts)
}

func TestGetResultURLKeepsSunoCDNWhenLiveChannelBaseURLChanged(t *testing.T) {
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://api.example.com"
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	baseURL := "https://suno.current/v1"
	channel := &Channel{
		Id:      912004,
		Type:    constant.ChannelTypeOpenAI,
		Name:    "historical-suno-cdn",
		Key:     "sk-historical-suno",
		Status:  common.ChannelStatusEnabled,
		BaseURL: &baseURL,
	}
	require.NoError(t, DB.Create(channel).Error)
	t.Cleanup(func() {
		_ = DB.Delete(&Channel{}, channel.Id).Error
	})

	cdn := "https://cdn.suno.ai/legacy/video.mp4"
	task := &Task{
		TaskID:    "task_historical_suno_cdn",
		ChannelId: channel.Id,
		Status:    TaskStatusSuccess,
		PrivateData: TaskPrivateData{
			ResultURL: cdn,
		},
	}

	assert.Equal(t, cdn, task.GetResultURL())
	assert.Empty(t, task.PrivateData.ResultProxyHosts)
}

func TestGetResultURLRewritesUnsignedGoogleStorageHTTPS(t *testing.T) {
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://api.example.com"
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	task := &Task{
		TaskID: "task_gcs_https",
		Status: TaskStatusSuccess,
		PrivateData: TaskPrivateData{
			ResultURL: "https://storage.googleapis.com/bucket/video.mp4",
		},
	}

	assert.Equal(t, "https://api.example.com/v1/videos/task_gcs_https/content", task.GetResultURL())
}

func TestGetResultURLKeepsSignedGoogleStorageHTTPS(t *testing.T) {
	signed := "https://storage.googleapis.com/bucket/video.mp4?X-Goog-Algorithm=GOOG4-RSA-SHA256&X-Goog-Credential=sa%40example.com&X-Goog-Signature=abc"
	task := &Task{
		TaskID: "task_gcs_signed",
		Status: TaskStatusSuccess,
		PrivateData: TaskPrivateData{
			ResultURL: signed,
		},
	}

	assert.Equal(t, signed, task.GetResultURL())
}

func TestPublicStatusMapsRetryableVideoSettlementReviewToInProgress(t *testing.T) {
	task := &Task{
		TaskID:           "task_video_retryable_review",
		Platform:         constant.TaskPlatform("gemini"),
		Status:           TaskStatusSuccess,
		SettlementStatus: TaskSettlementStatusReview,
		Progress:         "100%",
		FinishTime:       123,
		NextPollAt:       456,
		PrivateData: TaskPrivateData{
			ResultURL: "https://cdn.example/video.mp4",
		},
	}

	assert.Equal(t, TaskStatus(TaskStatusInProgress), task.PublicStatus())
	assert.Empty(t, task.PublicResultURL())
	assert.Equal(t, "99%", task.PublicProgress())
	assert.Zero(t, task.PublicFinishTime())
}

func TestPublicStatusMapsUnrecoverableVideoSettlementReviewToFailure(t *testing.T) {
	task := &Task{
		TaskID:           "task_video_unrecoverable_review",
		Platform:         constant.TaskPlatform("gemini"),
		Status:           TaskStatusSuccess,
		SettlementStatus: TaskSettlementStatusReview,
		Progress:         "100%",
		FinishTime:       123,
		FailReason:       "billing settlement requires manual review",
		PrivateData: TaskPrivateData{
			ResultURL: "https://cdn.example/video.mp4",
		},
	}

	assert.Equal(t, TaskStatus(TaskStatusFailure), task.PublicStatus())
	assert.Empty(t, task.PublicResultURL())
}

func TestPublicMediaReadyFalseDuringRetryableSettlementReview(t *testing.T) {
	task := &Task{
		TaskID:           "task_video_public_media_review",
		Platform:         constant.TaskPlatform("gemini"),
		Status:           TaskStatusSuccess,
		SettlementStatus: TaskSettlementStatusReview,
		NextPollAt:       456,
		PrivateData: TaskPrivateData{
			ResultURL: "https://cdn.example/video.mp4",
		},
	}

	assert.False(t, task.PublicMediaReady())
	assert.Empty(t, task.PublicResultURL())
}

func TestPublicMediaReadyFalseAfterResultCleanup(t *testing.T) {
	task := &Task{
		TaskID:          "task_video_public_media_cleaned",
		Platform:        constant.TaskPlatform("gemini"),
		Status:          TaskStatusSuccess,
		ResultCleanedAt: 123,
		PrivateData: TaskPrivateData{
			ResultURL: "https://cdn.example/video.mp4",
		},
	}

	assert.Equal(t, TaskStatus(TaskStatusSuccess), task.PublicStatus())
	assert.False(t, task.PublicMediaReady())
	assert.Empty(t, task.PublicResultURL())
}

func TestPublicMediaReadyTrueWhenPublicSuccess(t *testing.T) {
	task := &Task{
		TaskID:   "task_video_public_media_ready",
		Platform: constant.TaskPlatform("gemini"),
		Status:   TaskStatusSuccess,
		PrivateData: TaskPrivateData{
			ResultURL: "https://cdn.example/video.mp4",
		},
	}

	assert.True(t, task.PublicMediaReady())
	assert.Equal(t, "https://cdn.example/video.mp4", task.PublicResultURL())
}

func TestPublicStatusMapsPendingVideoSettlementToInProgress(t *testing.T) {
	task := &Task{
		TaskID:           "task_video_pending_settlement",
		Platform:         constant.TaskPlatform("gemini"),
		Status:           TaskStatusSuccess,
		SettlementStatus: TaskSettlementStatusPending,
		Progress:         "100%",
		FinishTime:       123,
		PrivateData: TaskPrivateData{
			ResultURL: "https://cdn.example/video.mp4",
		},
	}

	assert.Equal(t, TaskStatus(TaskStatusInProgress), task.PublicStatus())
	assert.False(t, task.PublicMediaReady())
	assert.Empty(t, task.PublicResultURL())
	assert.Equal(t, "99%", task.PublicProgress())
	assert.Zero(t, task.PublicFinishTime())
}

func TestPublicStatusMapsAppliedVideoSettlementToInProgress(t *testing.T) {
	task := &Task{
		TaskID:           "task_video_applied_settlement",
		Platform:         constant.TaskPlatform("gemini"),
		Status:           TaskStatusSuccess,
		SettlementStatus: TaskSettlementStatusApplied,
		Progress:         "100%",
		FinishTime:       123,
		PrivateData: TaskPrivateData{
			ResultURL: "https://cdn.example/video.mp4",
		},
	}

	assert.Equal(t, TaskStatus(TaskStatusInProgress), task.PublicStatus())
	assert.False(t, task.PublicMediaReady())
	assert.Empty(t, task.PublicResultURL())
}

func TestPublicMediaReadyTrueWhenSettled(t *testing.T) {
	task := &Task{
		TaskID:           "task_video_settled_media",
		Platform:         constant.TaskPlatform("gemini"),
		Status:           TaskStatusSuccess,
		SettlementStatus: TaskSettlementStatusSettled,
		PrivateData: TaskPrivateData{
			ResultURL: "https://cdn.example/video.mp4",
		},
	}

	assert.Equal(t, TaskStatus(TaskStatusSuccess), task.PublicStatus())
	assert.True(t, task.PublicMediaReady())
	assert.Equal(t, "https://cdn.example/video.mp4", task.PublicResultURL())
}

func TestSanitizePublicTaskFailReasonStripsAccountingInternals(t *testing.T) {
	assert.Equal(t, TaskPublicAccountingFailReason, SanitizePublicTaskFailReason(
		"billing accounting failed after task submission: pq: password authentication failed"))
	assert.Equal(t, TaskPublicAccountingFailReason, SanitizePublicTaskFailReason(
		"upstream failed; rollback billing after task error: redis: connection refused"))
	assert.Equal(t, TaskPublicSettlementFailReason, SanitizePublicTaskFailReason(
		"billing settlement failed before consumption log: record consume log failed"))
	assert.Equal(t, TaskPublicSettlementFailReason, SanitizePublicTaskFailReason(
		"settlement review update failed: gorm: sqlstate 23505"))
	assert.Equal(t, TaskPublicSettlementFailReason, SanitizePublicTaskFailReason("record consume log failed"))
	assert.Equal(t, TaskPublicSettlementFailReason, SanitizePublicTaskFailReason("settlement failed"))
	assert.Equal(t, TaskPublicSettlementFailReason, SanitizePublicTaskFailReason("review update failed"))
	assert.Empty(t, SanitizePublicTaskFailReason("https://cdn.example/video.mp4"))
	assert.Equal(t, "download failed:", SanitizePublicTaskFailReason(
		"download failed: https://cdn.example/video.mp4?X-Goog-Signature=abc"))
	assert.Equal(t, "provider rejected payload", SanitizePublicTaskFailReason(
		"provider rejected payload data:video/mp4;base64,AAAA"))
	assert.Equal(t, "content policy", SanitizePublicTaskFailReason("content policy"))
	assert.Equal(t, TaskPublicInternalFailReason, SanitizePublicTaskFailReason(
		"pq: password authentication failed for user newapi"))
	assert.Equal(t, TaskPublicInternalFailReason, SanitizePublicTaskFailReason(
		"timeout connecting to 10.0.0.8:5432"))
	assert.Equal(t, TaskPublicInternalFailReason, SanitizePublicTaskFailReason(
		"dial tcp 127.0.0.1:6379: connect: connection refused"))
	assert.Equal(t, TaskPublicInternalFailReason, SanitizePublicTaskFailReason(
		"sql: database is closed"))
	assert.Equal(t, TaskPublicInternalFailReason, SanitizePublicTaskFailReason(
		"ERROR: no such table: tasks (SQLSTATE 42P01)"))
}

func TestPublicFailReasonHidesAccountingInternals(t *testing.T) {
	task := &Task{
		Status:     TaskStatusFailure,
		FailReason: "billing accounting failed after task submission: pq: password authentication failed",
	}
	assert.Equal(t, TaskPublicAccountingFailReason, task.PublicFailReason())
}

func TestToOpenAIVideoHidesRetryableSettlementReview(t *testing.T) {
	task := &Task{
		TaskID:           "task_video_openai_review",
		Status:           TaskStatusSuccess,
		SettlementStatus: TaskSettlementStatusReview,
		Progress:         "100%",
		FinishTime:       123,
		UpdatedAt:        456,
		NextPollAt:       789,
		Properties: Properties{
			OriginModelName: "sora-2",
		},
		PrivateData: TaskPrivateData{
			ResultURL: "https://cdn.example/video.mp4",
		},
	}

	video := task.ToOpenAIVideo()
	assert.Equal(t, dto.VideoStatusInProgress, video.Status)
	assert.Equal(t, 99, video.Progress)
	assert.Zero(t, video.CompletedAt)
	assert.Empty(t, video.Metadata["url"])
}

func TestGetResultURLRewritesOfficialGeminiStoredHostWhenLiveBaseURLEmpty(t *testing.T) {
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://api.example.com"
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	channel := &Channel{
		Id:     912005,
		Type:   constant.ChannelTypeGemini,
		Name:   "historical-official-gemini-empty-base",
		Key:    "sk-historical-official-gemini",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, DB.Create(channel).Error)
	t.Cleanup(func() {
		_ = DB.Delete(&Channel{}, channel.Id).Error
	})

	task := &Task{
		TaskID:    "task_historical_official_gemini_empty_base",
		ChannelId: channel.Id,
		Status:    TaskStatusSuccess,
		PrivateData: TaskPrivateData{
			ResultURL: "https://files.googleapis.com/v1/files/abc:download",
		},
	}

	assert.Equal(t, "https://api.example.com/v1/videos/task_historical_official_gemini_empty_base/content", task.GetResultURL())
	assert.Equal(t, "https://files.googleapis.com/v1/files/abc:download", task.PrivateData.ResultURL)
	assert.Empty(t, task.PrivateData.ResultProxyHosts)
}

func TestGetResultURLKeepsExampleCDNWhenGeminiLiveBaseURLEmpty(t *testing.T) {
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://api.example.com"
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	channel := &Channel{
		Id:     912006,
		Type:   constant.ChannelTypeGemini,
		Name:   "historical-gemini-empty-cdn",
		Key:    "sk-historical-gemini-cdn",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, DB.Create(channel).Error)
	t.Cleanup(func() {
		_ = DB.Delete(&Channel{}, channel.Id).Error
	})

	cdn := "https://example.com/video.mp4"
	task := &Task{
		TaskID:    "task_historical_gemini_empty_cdn",
		ChannelId: channel.Id,
		Status:    TaskStatusSuccess,
		PrivateData: TaskPrivateData{
			ResultURL: cdn,
		},
	}

	assert.Equal(t, cdn, task.GetResultURL())
	assert.Empty(t, task.PrivateData.ResultProxyHosts)
}
