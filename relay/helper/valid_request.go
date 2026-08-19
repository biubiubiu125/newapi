package helper

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"mime/multipart"
	"net/url"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/samber/lo"
	"golang.org/x/image/webp"

	"github.com/gin-gonic/gin"
)

func GetAndValidateRequest(c *gin.Context, format types.RelayFormat) (request dto.Request, err error) {
	relayMode := relayconstant.Path2RelayMode(c.Request.URL.Path)

	switch format {
	case types.RelayFormatOpenAI:
		request, err = GetAndValidateTextRequest(c, relayMode)
	case types.RelayFormatGemini:
		if strings.Contains(c.Request.URL.Path, ":embedContent") {
			request, err = GetAndValidateGeminiEmbeddingRequest(c)
		} else if strings.Contains(c.Request.URL.Path, ":batchEmbedContents") {
			request, err = GetAndValidateGeminiBatchEmbeddingRequest(c)
		} else {
			request, err = GetAndValidateGeminiRequest(c)
		}
	case types.RelayFormatClaude:
		request, err = GetAndValidateClaudeRequest(c)
	case types.RelayFormatOpenAIResponses:
		request, err = GetAndValidateResponsesRequest(c)
	case types.RelayFormatOpenAIResponsesCompaction:
		request, err = GetAndValidateResponsesCompactionRequest(c)
	case types.RelayFormatOpenAIAlphaSearch:
		request, err = GetAndValidateAlphaSearchRequest(c)

	case types.RelayFormatOpenAIImage:
		request, err = GetAndValidOpenAIImageRequest(c, relayMode)
	case types.RelayFormatEmbedding:
		request, err = GetAndValidateEmbeddingRequest(c, relayMode)
	case types.RelayFormatRerank:
		request, err = GetAndValidateRerankRequest(c)
	case types.RelayFormatOpenAIAudio:
		request, err = GetAndValidAudioRequest(c, relayMode)
	case types.RelayFormatOpenAIRealtime:
		request = &dto.BaseRequest{}
	default:
		return nil, fmt.Errorf("unsupported relay format: %s", format)
	}
	return request, err
}

func GetAndValidAudioRequest(c *gin.Context, relayMode int) (*dto.AudioRequest, error) {
	audioRequest := &dto.AudioRequest{}
	err := common.UnmarshalBodyReusable(c, audioRequest)
	if err != nil {
		return nil, err
	}
	switch relayMode {
	case relayconstant.RelayModeAudioSpeech:
		if audioRequest.Model == "" {
			return nil, errors.New("model is required")
		}
	default:
		if audioRequest.Model == "" {
			return nil, errors.New("model is required")
		}
		if audioRequest.ResponseFormat == "" {
			audioRequest.ResponseFormat = "json"
		}
	}
	return audioRequest, nil
}

func GetAndValidateRerankRequest(c *gin.Context) (*dto.RerankRequest, error) {
	var rerankRequest *dto.RerankRequest
	err := common.UnmarshalBodyReusable(c, &rerankRequest)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("getAndValidateTextRequest failed: %s", err.Error()))
		return nil, types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	if rerankRequest.Query == "" {
		return nil, types.NewError(fmt.Errorf("query is empty"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	if len(rerankRequest.Documents) == 0 {
		return nil, types.NewError(fmt.Errorf("documents is empty"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	return rerankRequest, nil
}

func GetAndValidateEmbeddingRequest(c *gin.Context, relayMode int) (*dto.EmbeddingRequest, error) {
	var embeddingRequest *dto.EmbeddingRequest
	err := common.UnmarshalBodyReusable(c, &embeddingRequest)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("getAndValidateTextRequest failed: %s", err.Error()))
		return nil, types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	if embeddingRequest.Input == nil {
		return nil, fmt.Errorf("input is empty")
	}
	if relayMode == relayconstant.RelayModeModerations && embeddingRequest.Model == "" {
		embeddingRequest.Model = "omni-moderation-latest"
	}
	if relayMode == relayconstant.RelayModeEmbeddings && embeddingRequest.Model == "" {
		embeddingRequest.Model = c.Param("model")
	}
	return embeddingRequest, nil
}

// maxTokensLimit bounds user-supplied max token fields. These values feed
// pre-consume quota math (preConsumedTokens * ratio); an unbounded value can
// overflow the conversion and corrupt billing.
const maxTokensLimit = math.MaxInt32 / 2

func exceedsMaxTokensLimit(values ...*uint) bool {
	for _, v := range values {
		if lo.FromPtrOr(v, uint(0)) > maxTokensLimit {
			return true
		}
	}
	return false
}

func GetAndValidateResponsesRequest(c *gin.Context) (*dto.OpenAIResponsesRequest, error) {
	request := &dto.OpenAIResponsesRequest{}
	err := common.UnmarshalBodyReusable(c, request)
	if err != nil {
		return nil, err
	}
	if request.Model == "" {
		return nil, errors.New("model is required")
	}
	if request.Input == nil {
		return nil, errors.New("input is required")
	}
	if exceedsMaxTokensLimit(request.MaxOutputTokens) {
		return nil, errors.New("max_output_tokens is invalid")
	}
	return request, nil
}

func GetAndValidateAlphaSearchRequest(c *gin.Context) (*dto.AlphaSearchRequest, error) {
	request := &dto.AlphaSearchRequest{}
	if err := common.UnmarshalBodyReusable(c, request); err != nil {
		return nil, err
	}
	if request.Model == "" {
		return nil, errors.New("model is required")
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	rawBody, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	request.RawBody = rawBody
	return request, nil
}

func GetAndValidateResponsesCompactionRequest(c *gin.Context) (*dto.OpenAIResponsesCompactionRequest, error) {
	request := &dto.OpenAIResponsesCompactionRequest{}
	if err := common.UnmarshalBodyReusable(c, request); err != nil {
		return nil, err
	}
	if request.Model == "" {
		return nil, errors.New("model is required")
	}
	return request, nil
}

func GetAndValidOpenAIImageRequest(c *gin.Context, relayMode int) (*dto.ImageRequest, error) {
	imageRequest := &dto.ImageRequest{}
	publicImageTask := c != nil && c.Request != nil && c.Request.URL != nil &&
		strings.HasPrefix(c.Request.URL.Path, "/v1/image-tasks/")

	switch relayMode {
	case relayconstant.RelayModeImagesEdits:
		if strings.Contains(c.Request.Header.Get("Content-Type"), "multipart/form-data") {
			form := c.Request.MultipartForm
			if form == nil {
				var err error
				form, err = common.ParseMultipartFormReusable(c)
				if err != nil {
					return nil, fmt.Errorf("failed to parse image edit form request: %w", err)
				}
				c.Request.MultipartForm = form
			}
			formData := url.Values(form.Value)
			if err := populateImageRequestFromMultipart(imageRequest, formData, publicImageTask); err != nil {
				return nil, err
			}
			if publicImageTask && imageRequest.Model == "" {
				imageRequest.Model = "gpt-image-2"
			}

			if imageRequest.Model == "gpt-image-1" {
				if imageRequest.Quality == "" {
					if publicImageTask {
						imageRequest.Quality = "auto"
					} else {
						imageRequest.Quality = "standard"
					}
				}
			}
			if publicImageTask && imageRequest.Model == "gpt-image-2" && imageRequest.Quality == "" {
				imageRequest.Quality = "high"
			}
			if imageRequest.N == nil || *imageRequest.N == 0 {
				imageRequest.N = common.GetPointer(uint(1))
			}
			if publicImageTask && *imageRequest.N != 1 {
				return nil, errors.New("n must be 1 for image task requests; create multiple tasks for counts greater than one")
			}

			if publicImageTask {
				if strings.TrimSpace(imageRequest.Prompt) == "" {
					return nil, errors.New("prompt is required")
				}
				if err := validatePublicImageTaskSize(imageRequest.Size); err != nil {
					return nil, err
				}
				imageCount := countOpenAIImageEditFiles(form)
				if imageCount == 0 {
					return nil, errors.New("image is required")
				}
				if imageCount > 6 {
					return nil, errors.New("image count must be between 1 and 6")
				}
				if err := ValidateOpenAIImageEditFiles(form); err != nil {
					return nil, err
				}
			}
			break
		}
		fallthrough
	default:
		err := common.UnmarshalBodyReusable(c, imageRequest)
		if err != nil {
			return nil, err
		}

		if publicImageTask && imageRequest.Model == "" {
			switch relayMode {
			case relayconstant.RelayModeImagesGenerations:
				imageRequest.Model = "gpt-image-2"
			case relayconstant.RelayModeImagesEdits:
				imageRequest.Model = "gpt-image-2"
			}
		}

		if imageRequest.Model == "" {
			//imageRequest.Model = "dall-e-3"
			return nil, errors.New("model is required")
		}

		if strings.Contains(imageRequest.Size, "×") {
			return nil, errors.New("size an unexpected error occurred in the parameter, please use 'x' instead of the multiplication sign '×'")
		}

		if imageRequest.N != nil && (*imageRequest.N > dto.MaxImageN || (publicImageTask && *imageRequest.N == 0)) {
			return nil, fmt.Errorf("n must be an integer between 1 and %d", dto.MaxImageN)
		}

		// Not "256x256", "512x512", or "1024x1024"
		if imageRequest.Model == "dall-e-2" || imageRequest.Model == "dall-e" {
			if imageRequest.Size != "" && imageRequest.Size != "256x256" && imageRequest.Size != "512x512" && imageRequest.Size != "1024x1024" {
				return nil, errors.New("size must be one of 256x256, 512x512, or 1024x1024 for dall-e-2 or dall-e")
			}
			if imageRequest.Size == "" {
				imageRequest.Size = "1024x1024"
			}
		} else if imageRequest.Model == "dall-e-3" {
			if imageRequest.Size != "" && imageRequest.Size != "1024x1024" && imageRequest.Size != "1024x1792" && imageRequest.Size != "1792x1024" {
				return nil, errors.New("size must be one of 1024x1024, 1024x1792 or 1792x1024 for dall-e-3")
			}
			if imageRequest.Quality == "" {
				imageRequest.Quality = "standard"
			}
			if imageRequest.Size == "" {
				imageRequest.Size = "1024x1024"
			}
		} else if imageRequest.Model == "gpt-image-1" {
			if imageRequest.Quality == "" {
				imageRequest.Quality = "auto"
			}
		}
		if publicImageTask && imageRequest.Model == "gpt-image-2" && imageRequest.Quality == "" {
			imageRequest.Quality = "high"
		}

		if publicImageTask && strings.TrimSpace(imageRequest.Prompt) == "" {
			return nil, errors.New("prompt is required")
		}
		if publicImageTask {
			if err := validatePublicImageTaskSize(imageRequest.Size); err != nil {
				return nil, err
			}
		}
		if publicImageTask && relayMode == relayconstant.RelayModeImagesEdits && len(imageRequest.Image) == 0 {
			return nil, errors.New("image is required")
		}

		if imageRequest.N == nil || *imageRequest.N == 0 {
			imageRequest.N = common.GetPointer(uint(1))
		}
		if publicImageTask && *imageRequest.N != 1 {
			return nil, errors.New("n must be 1 for image task requests; create multiple tasks for counts greater than one")
		}
	}

	return imageRequest, nil
}

func countOpenAIImageEditFiles(form *multipart.Form) int {
	if form == nil || len(form.File) == 0 {
		return 0
	}
	count := 0
	for fieldName, files := range form.File {
		if isOpenAIImageEditFileField(fieldName) {
			count += len(files)
		}
	}
	return count
}

func isOpenAIImageEditFileField(fieldName string) bool {
	if fieldName == "image" || fieldName == "image[]" {
		return true
	}
	if !strings.HasPrefix(fieldName, "image[") || !strings.HasSuffix(fieldName, "]") {
		return false
	}
	index := strings.TrimSuffix(strings.TrimPrefix(fieldName, "image["), "]")
	if index == "" {
		return false
	}
	for _, ch := range index {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

const openAIImageEditMaxUploadBytes = 64 << 20

func OpenValidatedOpenAIImageEditFile(fileHeader *multipart.FileHeader, fieldName string) (io.ReadCloser, string, error) {
	if fileHeader == nil {
		return nil, "", fmt.Errorf("%s is required", fieldName)
	}
	if fileHeader.Size > openAIImageEditMaxUploadBytes {
		return nil, "", fmt.Errorf("%s exceeds maximum allowed size of %d MB", fieldName, openAIImageEditMaxUploadBytes>>20)
	}

	file, err := fileHeader.Open()
	if err != nil {
		return nil, "", fmt.Errorf("failed to open %s: %w", fieldName, err)
	}

	data, readErr := io.ReadAll(io.LimitReader(file, int64(openAIImageEditMaxUploadBytes)+1))
	closeErr := file.Close()
	if readErr != nil {
		return nil, "", fmt.Errorf("failed to read %s: %w", fieldName, readErr)
	}
	if closeErr != nil {
		return nil, "", fmt.Errorf("failed to close %s: %w", fieldName, closeErr)
	}
	if len(data) > openAIImageEditMaxUploadBytes {
		return nil, "", fmt.Errorf("%s exceeds maximum allowed size of %d MB", fieldName, openAIImageEditMaxUploadBytes>>20)
	}
	if len(data) == 0 {
		return nil, "", fmt.Errorf("%s is empty", fieldName)
	}

	contentType, err := validateOpenAIImageData(data)
	if err != nil {
		return nil, "", fmt.Errorf("%s is not a valid image: %w", fieldName, err)
	}

	return io.NopCloser(bytes.NewReader(data)), contentType, nil
}

func ValidateOpenAIImageEditFiles(form *multipart.Form) error {
	if form == nil || len(form.File) == 0 {
		return nil
	}
	for fieldName, files := range form.File {
		if fieldName != "mask" && !isOpenAIImageEditFileField(fieldName) {
			continue
		}
		for index, fileHeader := range files {
			readCloser, _, err := OpenValidatedOpenAIImageEditFile(fileHeader, openAIImageEditFieldLabel(fieldName, index))
			if err != nil {
				return err
			}
			_ = readCloser.Close()
		}
	}
	return nil
}

func openAIImageEditFieldLabel(fieldName string, index int) string {
	if index <= 0 {
		return fieldName
	}
	return fmt.Sprintf("%s[%d]", fieldName, index)
}

func detectOpenAIImageContentType(data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("image data is empty")
	}

	if _, format, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		if contentType, ok := openAIImageFormatToContentType(format); ok {
			return contentType, nil
		}
		return "", fmt.Errorf("unsupported image format: %s", format)
	}

	if _, err := webp.DecodeConfig(bytes.NewReader(data)); err == nil {
		return "image/webp", nil
	}

	return "", errors.New("unsupported or corrupted image")
}

func validateOpenAIImageData(data []byte) (string, error) {
	contentType, err := detectOpenAIImageContentType(data)
	if err != nil {
		return "", err
	}

	if contentType == "image/webp" {
		if _, err := webp.Decode(bytes.NewReader(data)); err != nil {
			return "", fmt.Errorf("unsupported or corrupted image: %w", err)
		}
		return contentType, nil
	}

	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return "", fmt.Errorf("unsupported or corrupted image: %w", err)
	}
	return contentType, nil
}

func openAIImageFormatToContentType(format string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "jpeg", "jpg":
		return "image/jpeg", true
	case "png":
		return "image/png", true
	case "gif":
		return "image/gif", true
	case "webp":
		return "image/webp", true
	default:
		return "", false
	}
}

func validatePublicImageTaskSize(size string) error {
	size = strings.TrimSpace(size)
	if size == "" {
		return nil
	}
	if strings.Contains(size, "×") {
		return errors.New("size an unexpected error occurred in the parameter, please use 'x' instead of the multiplication sign '×'")
	}
	parts := strings.Split(size, "x")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return errors.New("size must use WIDTHxHEIGHT format")
	}
	width, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || width <= 0 {
		return errors.New("width must be a positive integer")
	}
	height, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || height <= 0 {
		return errors.New("height must be a positive integer")
	}
	if width%16 != 0 || height%16 != 0 {
		return errors.New("width and height must be multiples of 16")
	}
	ratio := float64(width) / float64(height)
	if ratio > 3 || ratio < 1.0/3.0 {
		return errors.New("size ratio must be between 1:3 and 3:1")
	}
	return nil
}

func populateImageRequestFromMultipart(imageRequest *dto.ImageRequest, formData url.Values, publicImageTask bool) error {
	imageRequest.Model = formData.Get("model")
	imageRequest.Prompt = formData.Get("prompt")
	imageRequest.Size = formData.Get("size")
	imageRequest.Quality = formData.Get("quality")

	if nValue := strings.TrimSpace(formData.Get("n")); nValue != "" {
		n, err := strconv.Atoi(nValue)
		if err != nil || n < 0 || n > dto.MaxImageN || (publicImageTask && n == 0) {
			return fmt.Errorf("n must be an integer between 1 and %d", dto.MaxImageN)
		}
		imageRequest.N = common.GetPointer(uint(n))
	}

	setRawString := func(target *json.RawMessage, key string) error {
		if !formData.Has(key) {
			return nil
		}
		raw, err := common.Marshal(formData.Get(key))
		if err != nil {
			return err
		}
		*target = raw
		return nil
	}
	if err := setRawString(&imageRequest.Image, "image"); err != nil {
		return fmt.Errorf("invalid image value: %w", err)
	}

	setRawInteger := func(target *json.RawMessage, key string, min int, max int) error {
		if !formData.Has(key) {
			return nil
		}
		value, err := strconv.Atoi(strings.TrimSpace(formData.Get(key)))
		if err != nil || value < min || (max >= 0 && value > max) {
			if max >= 0 {
				return fmt.Errorf("%s must be an integer between %d and %d", key, min, max)
			}
			return fmt.Errorf("%s must be an integer greater than or equal to %d", key, min)
		}
		raw, err := common.Marshal(value)
		if err != nil {
			return err
		}
		*target = raw
		return nil
	}
	if publicImageTask {
		imageRequest.ResponseFormat = formData.Get("response_format")
		for _, field := range []struct {
			key    string
			target *json.RawMessage
		}{
			{key: "style", target: &imageRequest.Style},
			{key: "user", target: &imageRequest.User},
			{key: "background", target: &imageRequest.Background},
			{key: "moderation", target: &imageRequest.Moderation},
			{key: "output_format", target: &imageRequest.OutputFormat},
			{key: "input_fidelity", target: &imageRequest.InputFidelity},
			{key: "watermark_enabled", target: &imageRequest.WatermarkEnabled},
			{key: "user_id", target: &imageRequest.UserId},
			{key: "image_url", target: &imageRequest.ImageUrl},
			{key: "mask", target: &imageRequest.Mask},
		} {
			if err := setRawString(field.target, field.key); err != nil {
				return fmt.Errorf("invalid %s value: %w", field.key, err)
			}
		}
		if err := setRawInteger(&imageRequest.OutputCompression, "output_compression", 0, 100); err != nil {
			return err
		}
		if err := setRawInteger(&imageRequest.PartialImages, "partial_images", 0, -1); err != nil {
			return err
		}
	}

	if formData.Has("stream") {
		stream, err := strconv.ParseBool(formData.Get("stream"))
		if err != nil {
			return fmt.Errorf("invalid stream value: %w", err)
		}
		imageRequest.Stream = common.GetPointer(stream)
	}
	if formData.Has("watermark") {
		watermark := formData.Get("watermark") == "true"
		if publicImageTask {
			parsed, err := strconv.ParseBool(formData.Get("watermark"))
			if err != nil {
				return fmt.Errorf("invalid watermark value: %w", err)
			}
			watermark = parsed
		}
		imageRequest.Watermark = &watermark
	}
	return nil
}

func GetAndValidateClaudeRequest(c *gin.Context) (textRequest *dto.ClaudeRequest, err error) {
	textRequest = &dto.ClaudeRequest{}
	err = common.UnmarshalBodyReusable(c, textRequest)
	if err != nil {
		return nil, err
	}
	if textRequest.Messages == nil || len(textRequest.Messages) == 0 {
		return nil, errors.New("field messages is required")
	}
	if textRequest.Model == "" {
		return nil, errors.New("field model is required")
	}
	if exceedsMaxTokensLimit(textRequest.MaxTokens, textRequest.MaxTokensToSample) {
		return nil, errors.New("max_tokens is invalid")
	}

	//if textRequest.Stream {
	//	relayInfo.IsStream = true
	//}

	return textRequest, nil
}

func GetAndValidateTextRequest(c *gin.Context, relayMode int) (*dto.GeneralOpenAIRequest, error) {
	textRequest := &dto.GeneralOpenAIRequest{}
	err := common.UnmarshalBodyReusable(c, textRequest)
	if err != nil {
		return nil, err
	}

	if relayMode == relayconstant.RelayModeModerations && textRequest.Model == "" {
		textRequest.Model = "text-moderation-latest"
	}
	if relayMode == relayconstant.RelayModeEmbeddings && textRequest.Model == "" {
		textRequest.Model = c.Param("model")
	}

	if exceedsMaxTokensLimit(textRequest.MaxTokens, textRequest.MaxCompletionTokens) {
		return nil, errors.New("max_tokens is invalid")
	}
	if textRequest.Model == "" {
		return nil, errors.New("model is required")
	}
	if textRequest.WebSearchOptions != nil {
		if textRequest.WebSearchOptions.SearchContextSize != "" {
			validSizes := map[string]bool{
				"high":   true,
				"medium": true,
				"low":    true,
			}
			if !validSizes[textRequest.WebSearchOptions.SearchContextSize] {
				return nil, errors.New("invalid search_context_size, must be one of: high, medium, low")
			}
		} else {
			textRequest.WebSearchOptions.SearchContextSize = "medium"
		}
	}
	switch relayMode {
	case relayconstant.RelayModeCompletions:
		if textRequest.Prompt == "" {
			return nil, errors.New("field prompt is required")
		}
	case relayconstant.RelayModeChatCompletions:
		// For FIM (Fill-in-the-middle) requests with prefix/suffix, messages is optional
		// It will be filled by provider-specific adaptors if needed (e.g., SiliconFlow)。Or it is allowed by model vendor(s) (e.g., DeepSeek)
		if len(textRequest.Messages) == 0 && textRequest.Prefix == nil && textRequest.Suffix == nil {
			return nil, errors.New("field messages is required")
		}
	case relayconstant.RelayModeEmbeddings:
	case relayconstant.RelayModeModerations:
		if textRequest.Input == nil || textRequest.Input == "" {
			return nil, errors.New("field input is required")
		}
	case relayconstant.RelayModeEdits:
		if textRequest.Instruction == "" {
			return nil, errors.New("field instruction is required")
		}
	}
	return textRequest, nil
}

func GetAndValidateGeminiRequest(c *gin.Context) (*dto.GeminiChatRequest, error) {
	request := &dto.GeminiChatRequest{}
	err := common.UnmarshalBodyReusable(c, request)
	if err != nil {
		return nil, err
	}
	if len(request.Contents) == 0 && len(request.Requests) == 0 {
		return nil, errors.New("contents is required")
	}
	if exceedsMaxTokensLimit(request.GenerationConfig.MaxOutputTokens) {
		return nil, errors.New("maxOutputTokens is invalid")
	}

	//if c.Query("alt") == "sse" {
	//	relayInfo.IsStream = true
	//}

	return request, nil
}

func GetAndValidateGeminiEmbeddingRequest(c *gin.Context) (*dto.GeminiEmbeddingRequest, error) {
	request := &dto.GeminiEmbeddingRequest{}
	err := common.UnmarshalBodyReusable(c, request)
	if err != nil {
		return nil, err
	}
	return request, nil
}

func GetAndValidateGeminiBatchEmbeddingRequest(c *gin.Context) (*dto.GeminiBatchEmbeddingRequest, error) {
	request := &dto.GeminiBatchEmbeddingRequest{}
	err := common.UnmarshalBodyReusable(c, request)
	if err != nil {
		return nil, err
	}
	return request, nil
}
