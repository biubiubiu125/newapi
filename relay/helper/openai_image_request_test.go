package helper

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestGetAndValidOpenAIImageRequestMultipartStream verifies multipart image
// edit parsing: the stream field is parsed and validated, and the request body
// stays replayable for the upstream request.
func TestGetAndValidOpenAIImageRequestMultipartStream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newContext := func(t *testing.T, streamValue string, withImage bool) (*gin.Context, string) {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		require.NoError(t, writer.WriteField("model", "gpt-image-1"))
		require.NoError(t, writer.WriteField("prompt", "edit this image"))
		require.NoError(t, writer.WriteField("stream", streamValue))
		if withImage {
			part, err := writer.CreateFormFile("image", "input.png")
			require.NoError(t, err)
			_, err = part.Write([]byte("fake image"))
			require.NoError(t, err)
		}
		require.NoError(t, writer.Close())
		originalBody := body.String()

		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		return c, originalBody
	}

	t.Run("valid stream value keeps body replayable", func(t *testing.T) {
		c, originalBody := newContext(t, "true", true)

		req, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)
		require.NoError(t, err)
		require.True(t, req.Stream)
		require.True(t, req.IsStream(c))

		bodyAfterValidation, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.Equal(t, originalBody, string(bodyAfterValidation))

		form, err := common.ParseMultipartFormReusable(c)
		require.NoError(t, err)
		require.Equal(t, "true", url.Values(form.Value).Get("stream"))
		require.Len(t, form.File["image"], 1)
	})

	t.Run("invalid stream value is rejected", func(t *testing.T) {
		c, _ := newContext(t, "notabool", false)

		_, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid stream value")
	})
}

func TestGetAndValidOpenAIImageRequestReusesPreparsedMultipartForm(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/image-tasks/edits", nil)
	c.Request.Header.Set("Content-Type", "multipart/form-data; boundary=already-parsed")
	c.Request.MultipartForm = &multipart.Form{
		Value: map[string][]string{
			"model":  {"gpt-image-1"},
			"prompt": {"reuse this form"},
		},
		File: map[string][]*multipart.FileHeader{
			"image": {{Filename: "input.png"}},
		},
	}

	req, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)

	require.NoError(t, err)
	require.Equal(t, "gpt-image-1", req.Model)
	require.Equal(t, "reuse this form", req.Prompt)
}

func TestGetAndValidPublicImageTaskEditNormalizesMultipartBillingFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fields := map[string]string{
		"prompt":             "edit this image",
		"response_format":    "b64_json",
		"style":              "vivid",
		"user":               "billing-user",
		"background":         "transparent",
		"moderation":         "low",
		"output_format":      "webp",
		"output_compression": "80",
		"partial_images":     "2",
		"input_fidelity":     "high",
		"stream":             "true",
		"watermark":          "true",
	}
	for key, value := range fields {
		require.NoError(t, writer.WriteField(key, value))
	}
	part, err := writer.CreateFormFile("image", "input.png")
	require.NoError(t, err)
	_, err = part.Write([]byte("fake image"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/image-tasks/edits", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	req, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)

	require.NoError(t, err)
	require.Equal(t, "gpt-image-1", req.Model)
	require.Equal(t, "auto", req.Quality)
	require.Equal(t, "b64_json", req.ResponseFormat)
	require.JSONEq(t, `"vivid"`, string(req.Style))
	require.JSONEq(t, `"billing-user"`, string(req.User))
	require.JSONEq(t, `"transparent"`, string(req.Background))
	require.JSONEq(t, `"low"`, string(req.Moderation))
	require.JSONEq(t, `"webp"`, string(req.OutputFormat))
	require.JSONEq(t, `80`, string(req.OutputCompression))
	require.JSONEq(t, `2`, string(req.PartialImages))
	require.JSONEq(t, `"high"`, string(req.InputFidelity))
	require.True(t, req.Stream)
	require.NotNil(t, req.Watermark)
	require.True(t, *req.Watermark)

	billingInput, err := BuildBillingExprRequestInputFromRequest(req, nil)
	require.NoError(t, err)
	var billingBody map[string]any
	require.NoError(t, common.Unmarshal(billingInput.Body, &billingBody))
	require.Equal(t, "b64_json", billingBody["response_format"])
	require.Equal(t, "vivid", billingBody["style"])
	require.Equal(t, "transparent", billingBody["background"])
	require.Equal(t, "webp", billingBody["output_format"])
	require.EqualValues(t, 80, billingBody["output_compression"])
	require.EqualValues(t, 2, billingBody["partial_images"])
	require.Equal(t, "high", billingBody["input_fidelity"])
	require.Equal(t, true, billingBody["watermark"])
}

func TestGetAndValidOpenAIImageRequestKeepsSyncMultipartCompatibility(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-1"))
	require.NoError(t, writer.WriteField("prompt", "edit this image"))
	require.NoError(t, writer.WriteField("output_compression", "not-an-integer"))
	require.NoError(t, writer.WriteField("watermark", "not-a-boolean"))
	part, err := writer.CreateFormFile("image", "input.png")
	require.NoError(t, err)
	_, err = part.Write([]byte("fake image"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	req, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)

	require.NoError(t, err)
	require.Equal(t, "standard", req.Quality)
	require.Empty(t, req.OutputCompression)
	require.NotNil(t, req.Watermark)
	require.False(t, *req.Watermark)
}

func TestGetAndValidOpenAIImageRequestRequiresModelForImageGeneration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.CreateBodyStorage([]byte(`{"prompt":"draw a cat"}`))
	require.NoError(t, err)
	defer storage.Close()
	c.Set(common.KeyBodyStorage, storage)

	req, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesGenerations)

	require.Error(t, err)
	require.Nil(t, req)
	require.Contains(t, err.Error(), "model is required")
}

func TestGetAndValidPublicImageTaskGenerationDefaultsModelAndRequiresPrompt(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newContext := func(t *testing.T, body string) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/image-tasks/generations", nil)
		c.Request.Header.Set("Content-Type", "application/json")
		storage, err := common.CreateBodyStorage([]byte(body))
		require.NoError(t, err)
		t.Cleanup(func() { _ = storage.Close() })
		c.Set(common.KeyBodyStorage, storage)
		return c
	}

	req, err := GetAndValidOpenAIImageRequest(
		newContext(t, `{"prompt":"draw a cat"}`),
		relayconstant.RelayModeImagesGenerations,
	)
	require.NoError(t, err)
	require.Equal(t, "dall-e", req.Model)

	req, err = GetAndValidOpenAIImageRequest(
		newContext(t, `{"model":"gpt-image-1"}`),
		relayconstant.RelayModeImagesGenerations,
	)
	require.Error(t, err)
	require.Nil(t, req)
	require.Contains(t, err.Error(), "prompt is required")
}

func TestGetAndValidPublicImageTaskRejectsExplicitZeroN(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("json generation", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/image-tasks/generations", nil)
		c.Request.Header.Set("Content-Type", "application/json")
		storage, err := common.CreateBodyStorage([]byte(`{"model":"gpt-image-1","prompt":"draw a cat","n":0}`))
		require.NoError(t, err)
		t.Cleanup(func() { _ = storage.Close() })
		c.Set(common.KeyBodyStorage, storage)

		req, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesGenerations)

		require.Nil(t, req)
		require.ErrorContains(t, err, "n must be an integer between 1 and")
	})

	t.Run("multipart edit", func(t *testing.T) {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		require.NoError(t, writer.WriteField("model", "gpt-image-1"))
		require.NoError(t, writer.WriteField("prompt", "edit this image"))
		require.NoError(t, writer.WriteField("n", "0"))
		part, err := writer.CreateFormFile("image", "input.png")
		require.NoError(t, err)
		_, err = part.Write([]byte("fake image"))
		require.NoError(t, err)
		require.NoError(t, writer.Close())

		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/image-tasks/edits", &body)
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())

		req, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)

		require.Nil(t, req)
		require.ErrorContains(t, err, "n must be an integer between 1 and")
	})
}

func TestGetAndValidPublicImageTaskEditDefaultsModelAndRequiresFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newContext := func(t *testing.T, prompt string, modelName string, includeImage bool) *gin.Context {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if prompt != "" {
			require.NoError(t, writer.WriteField("prompt", prompt))
		}
		if modelName != "" {
			require.NoError(t, writer.WriteField("model", modelName))
		}
		if includeImage {
			part, err := writer.CreateFormFile("image", "input.png")
			require.NoError(t, err)
			_, err = part.Write([]byte("fake image"))
			require.NoError(t, err)
		}
		require.NoError(t, writer.Close())

		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/image-tasks/edits", &body)
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		return c
	}

	req, err := GetAndValidOpenAIImageRequest(
		newContext(t, "edit this image", "", true),
		relayconstant.RelayModeImagesEdits,
	)
	require.NoError(t, err)
	require.Equal(t, "gpt-image-1", req.Model)

	req, err = GetAndValidOpenAIImageRequest(
		newContext(t, "", "gpt-image-1", true),
		relayconstant.RelayModeImagesEdits,
	)
	require.Error(t, err)
	require.Nil(t, req)
	require.Contains(t, err.Error(), "prompt is required")

	req, err = GetAndValidOpenAIImageRequest(
		newContext(t, "edit this image", "gpt-image-1", false),
		relayconstant.RelayModeImagesEdits,
	)
	require.Error(t, err)
	require.Nil(t, req)
	require.Contains(t, err.Error(), "image is required")
}

// TestGetAndValidOpenAIImageRequestNBounds guards the billing invariant that
// the image generation count can never reach quota calculation with a value
// large enough to overflow int64 into a negative charge.
func TestGetAndValidOpenAIImageRequestNBounds(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newJSONContext := func(t *testing.T, body string) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewBufferString(body))
		c.Request.Header.Set("Content-Type", "application/json")
		return c
	}

	boundErr := fmt.Sprintf("n must be an integer between 1 and %d", dto.MaxImageN)

	tests := []struct {
		name    string
		body    string
		wantErr string
		wantN   uint
	}{
		{
			name:    "overflowed uint64 n is rejected",
			body:    `{"model":"gpt-image-1","prompt":"a cat","n":18446744073686646784}`,
			wantErr: boundErr,
		},
		{
			name:    "n above max is rejected",
			body:    fmt.Sprintf(`{"model":"gpt-image-1","prompt":"a cat","n":%d}`, dto.MaxImageN+1),
			wantErr: boundErr,
		},
		{
			name:  "n at max is accepted",
			body:  fmt.Sprintf(`{"model":"gpt-image-1","prompt":"a cat","n":%d}`, dto.MaxImageN),
			wantN: dto.MaxImageN,
		},
		{
			name:  "explicit n is accepted",
			body:  `{"model":"gpt-image-1","prompt":"a cat","n":3}`,
			wantN: 3,
		},
		{
			name:  "zero n defaults to 1",
			body:  `{"model":"gpt-image-1","prompt":"a cat","n":0}`,
			wantN: 1,
		},
		{
			name:  "absent n defaults to 1",
			body:  `{"model":"gpt-image-1","prompt":"a cat"}`,
			wantN: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newJSONContext(t, tt.body)
			req, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesGenerations)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, req.N)
			require.Equal(t, tt.wantN, *req.N)
			require.Equal(t, float64(tt.wantN), req.GetTokenCountMeta().BillingRatios["n"])
		})
	}

	t.Run("negative multipart n is rejected", func(t *testing.T) {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		require.NoError(t, writer.WriteField("model", "gpt-image-1"))
		require.NoError(t, writer.WriteField("prompt", "edit this image"))
		require.NoError(t, writer.WriteField("n", "-22904832"))
		require.NoError(t, writer.Close())

		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())

		_, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)
		require.Error(t, err)
		require.Contains(t, err.Error(), boundErr)
	})
}
