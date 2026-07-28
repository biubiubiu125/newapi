package middleware

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAbortWithOpenAIMessageUsesPublicImageTaskEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		status int
		code   types.ErrorCode
		want   string
	}{
		{status: http.StatusBadRequest, want: "invalid_request"},
		{status: http.StatusUnauthorized, want: "unauthorized"},
		{status: http.StatusForbidden, code: types.ErrorCodeAccessDenied, want: "access_denied"},
		{status: http.StatusTooManyRequests, want: "rate_limit_exceeded"},
		{status: http.StatusInternalServerError, want: "internal_error"},
		{status: http.StatusServiceUnavailable, code: types.ErrorCodeModelNotFound, want: "image_task_unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			engine := gin.New()
			engine.GET("/v1/image-tasks/task_x", func(c *gin.Context) {
				if tt.code == "" {
					abortWithOpenAiMessage(c, tt.status, "test error")
					return
				}
				abortWithOpenAiMessage(c, tt.status, "test error", tt.code)
			})
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/image-tasks/task_x", nil))

			require.Equal(t, tt.status, recorder.Code)
			var body struct {
				Error struct {
					Code string `json:"code"`
					Type string `json:"type"`
				} `json:"error"`
			}
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			require.Equal(t, tt.want, body.Error.Code)
			require.Equal(t, "image_task_error", body.Error.Type)
		})
	}
}

func TestAbortWithOpenAIMessageKeepsExistingEnvelopeOutsideImageTasks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/v1/models", func(c *gin.Context) {
		abortWithOpenAiMessage(c, http.StatusUnauthorized, "test error")
	})
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/models", nil))

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"type":"new_api_error"`)
}

func TestMemoryModelRequestRateLimitUsesPublicImageTaskEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("id", 912345)
		c.Set("token_id", 812345)
	})
	engine.GET("/v1/image-tasks/test", memoryRateLimitHandler(60, 1, 100), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	first := httptest.NewRecorder()
	engine.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/v1/image-tasks/test", nil))
	require.Equal(t, http.StatusOK, first.Code)
	second := httptest.NewRecorder()
	engine.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/v1/image-tasks/test", nil))
	require.Equal(t, http.StatusTooManyRequests, second.Code)
	require.Contains(t, second.Body.String(), `"code":"rate_limit_exceeded"`)
	require.Contains(t, second.Body.String(), `"type":"image_task_error"`)
}

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

func TestGetModelRequestDefaultsModelForPublicImageTaskGeneration(t *testing.T) {
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

func TestGetModelRequestDefaultsModelForPublicImageTaskEdit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("prompt", "edit this image"))
	part, err := writer.CreateFormFile("image", "input.png")
	require.NoError(t, err)
	_, err = part.Write([]byte("fake image"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/image-tasks/edits", nil)
	ctx.Request.Header.Set("Content-Type", writer.FormDataContentType())
	storage, err := common.CreateBodyStorage(body.Bytes())
	require.NoError(t, err)
	defer storage.Close()
	ctx.Set(common.KeyBodyStorage, storage)

	modelRequest, shouldSelectChannel, err := getModelRequest(ctx)

	require.NoError(t, err)
	require.True(t, shouldSelectChannel)
	require.Equal(t, "gpt-image-1", modelRequest.Model)
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
