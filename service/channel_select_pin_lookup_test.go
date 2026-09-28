package service

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSelectChannelForRequestPinLookupBlipIsRetryable(t *testing.T) {
	broken, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "closed.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := broken.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	previousDB := model.DB
	model.DB = broken
	t.Cleanup(func() { model.DB = previousDB })

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	GetChannelConstraints(ctx).AddPin(dto.ChannelPin{
		ChannelId: 77001,
		Source:    dto.PinSourceOriginTask,
		Rank:      dto.PinRankOriginTask,
		RetryMode: dto.PinRetrySameChannel,
	})

	channel, group, selectErr := SelectChannelForRequest(ctx, "kling-video", &RetryParam{TokenGroup: "default"})
	require.Nil(t, channel)
	require.Empty(t, group)
	require.NotNil(t, selectErr)
	require.Equal(t, http.StatusServiceUnavailable, selectErr.StatusCode)
	require.NotEqual(t, "origin_task_channel_disabled", string(selectErr.Code))
}

func TestSelectChannelForRequestTokenPinLookupBlipIsRetryable(t *testing.T) {
	broken, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "closed.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := broken.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	previousDB := model.DB
	model.DB = broken
	t.Cleanup(func() { model.DB = previousDB })

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	GetChannelConstraints(ctx).AddPin(dto.ChannelPin{
		ChannelId: 77002,
		Source:    dto.PinSourceToken,
		Rank:      dto.PinRankToken,
		RetryMode: dto.PinRetrySingleAttempt,
	})

	channel, _, selectErr := SelectChannelForRequest(ctx, "gpt-4o", &RetryParam{TokenGroup: "default"})
	require.Nil(t, channel)
	require.NotNil(t, selectErr)
	require.Equal(t, http.StatusServiceUnavailable, selectErr.StatusCode)
}
