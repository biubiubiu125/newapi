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

func TestGetModelRequestDefaultsModelForImageGenerationChannelSelection(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
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
