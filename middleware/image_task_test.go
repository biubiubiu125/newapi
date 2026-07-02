package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestShouldSkipMiddlewareModelLimitOnlyForImageTaskGETWithoutChannelSelection(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/image-tasks?ids=task_1", nil)
	require.True(t, shouldSkipMiddlewareModelLimit(ctx, false))

	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/image-tasks", nil)
	require.False(t, shouldSkipMiddlewareModelLimit(ctx, false))

	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/image-tasks/task_1", nil)
	require.False(t, shouldSkipMiddlewareModelLimit(ctx, true))
}

func TestShouldSkipModelRequestRateLimitForImageTaskGET(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/image-tasks/task_1", nil)
	require.True(t, shouldSkipModelRequestRateLimit(ctx))

	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/images/generations", nil)
	require.False(t, shouldSkipModelRequestRateLimit(ctx))

	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/image-tasks", nil)
	require.False(t, shouldSkipModelRequestRateLimit(ctx))
}

func TestIsImageTaskReadOnlyRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/image-tasks?ids=task_1", nil)
	require.True(t, isImageTaskReadOnlyRequest(ctx))

	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/image-tasks/task_1", nil)
	require.True(t, isImageTaskReadOnlyRequest(ctx))

	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/image-tasks", nil)
	require.False(t, isImageTaskReadOnlyRequest(ctx))
}

func TestGetModelRequestUsesDefaultModelForImageTaskGeneration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/image-tasks/generations", nil)
	ctx.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.CreateBodyStorage([]byte(`{}`))
	require.NoError(t, err)
	defer storage.Close()
	ctx.Set(common.KeyBodyStorage, storage)

	modelRequest, shouldSelectChannel, err := getModelRequest(ctx)

	require.NoError(t, err)
	require.True(t, shouldSelectChannel)
	require.Equal(t, "dall-e", modelRequest.Model)
}

func TestImageTaskReadOnlyTokenAllowsExhaustedOnly(t *testing.T) {
	now := int64(1000)

	require.True(t, isImageTaskReadOnlyTokenUsable(&model.Token{
		Status:      common.TokenStatusEnabled,
		ExpiredTime: -1,
	}, now))
	require.True(t, isImageTaskReadOnlyTokenUsable(&model.Token{
		Status:      common.TokenStatusExhausted,
		ExpiredTime: -1,
	}, now))
	require.False(t, isImageTaskReadOnlyTokenUsable(&model.Token{
		Status:      common.TokenStatusDisabled,
		ExpiredTime: -1,
	}, now))
	require.False(t, isImageTaskReadOnlyTokenUsable(&model.Token{
		Status:      common.TokenStatusExpired,
		ExpiredTime: -1,
	}, now))
	require.False(t, isImageTaskReadOnlyTokenUsable(&model.Token{
		Status:      common.TokenStatusExhausted,
		ExpiredTime: now - 1,
	}, now))
}

func TestSetupContextForTokenKeepsModelLimitsForImageTaskPolling(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	token := &model.Token{
		Id:                 7,
		UserId:             11,
		Key:                "test-key",
		ModelLimitsEnabled: true,
		ModelLimits:        "gpt-image-1,gpt-4o",
	}

	require.NoError(t, SetupContextForToken(ctx, token))
	require.True(t, common.GetContextKeyBool(ctx, constant.ContextKeyTokenModelLimitEnabled))
	value, ok := common.GetContextKey(ctx, constant.ContextKeyTokenModelLimit)
	require.True(t, ok)
	limits, ok := value.(map[string]bool)
	require.True(t, ok)
	require.True(t, limits["gpt-image-1"])
}
