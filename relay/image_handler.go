package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/gin-gonic/gin"
)

const (
	contextKeyImageStreamAllowed       = "image_stream_allowed"
	contextKeyImageTaskDeferBilling    = "image_task_defer_billing"
	contextKeyImageTaskDeferredBilling = "image_task_deferred_billing"
)

type imageTaskDeferredBilling struct {
	Usage        dto.Usage
	ExtraContent []string
}

func ImageHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	info.InitChannelMeta(c)

	imageReq, ok := info.Request.(*dto.ImageRequest)
	if !ok {
		return types.NewErrorWithStatusCode(fmt.Errorf("invalid request type, expected dto.ImageRequest, got %T", info.Request), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	request, err := common.DeepCopy(imageReq)
	if err != nil {
		return types.NewError(fmt.Errorf("failed to copy request to ImageRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	imageStreamDisabled := applyImageStreamSupportForChannel(c, info, request)

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)

	var requestBody io.Reader

	if model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled {
		body, cleanup, err := buildImagePassThroughBody(c, info, imageStreamDisabled)
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		if cleanup != nil {
			defer cleanup()
		}
		requestBody = body
	} else {
		convertedRequest, err := adaptor.ConvertImageRequest(c, info, *request)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed)
		}
		relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)

		switch convertedRequest.(type) {
		case *bytes.Buffer:
			requestBody = convertedRequest.(io.Reader)
		default:
			jsonData, err := common.Marshal(convertedRequest)
			if err != nil {
				return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}

			// apply param override
			if len(info.ParamOverride) > 0 {
				jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
				if err != nil {
					return newAPIErrorFromParamOverride(err)
				}
			}

			logger.LogDebug(c, "image request body: %s", jsonData)
			body, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
			if err != nil {
				return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}
			defer closer.Close()
			jsonData = nil
			requestBody = body
		}
	}

	statusCodeMappingStr := c.GetString("status_code_mapping")

	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	var httpResp *http.Response
	if resp != nil {
		httpResp = resp.(*http.Response)
		info.IsStream = info.IsStream || strings.HasPrefix(httpResp.Header.Get("Content-Type"), "text/event-stream")
		if httpResp.StatusCode != http.StatusOK {
			if httpResp.StatusCode == http.StatusCreated && info.ApiType == constant.APITypeReplicate {
				// replicate channel returns 201 Created when using Prefer: wait, treat it as success.
				httpResp.StatusCode = http.StatusOK
			} else {
				newAPIError = service.RelayErrorHandler(c.Request.Context(), httpResp, false)
				// reset status code 重置状态码
				service.ResetStatusCode(newAPIError, statusCodeMappingStr)
				return newAPIError
			}
		}
	}

	usage, newAPIError := adaptor.DoResponse(c, httpResp, info)
	if newAPIError != nil {
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}

	imageN := uint(1)
	if request.N != nil {
		imageN = *request.N
	}

	if usage.(*dto.Usage).TotalTokens == 0 {
		usage.(*dto.Usage).TotalTokens = 1
	}
	if usage.(*dto.Usage).PromptTokens == 0 {
		usage.(*dto.Usage).PromptTokens = 1
	}

	quality := request.Quality
	if quality == "" {
		quality = "standard"
	}

	var logContent []string

	if len(request.Size) > 0 {
		logContent = append(logContent, fmt.Sprintf("大小 %s", request.Size))
	}
	if len(quality) > 0 {
		logContent = append(logContent, fmt.Sprintf("品质 %s", quality))
	}
	if imageN > 0 {
		logContent = append(logContent, fmt.Sprintf("生成数量 %d", imageN))
	}

	if c.GetBool(contextKeyImageTaskDeferBilling) {
		usageCopy := *usage.(*dto.Usage)
		c.Set(contextKeyImageTaskDeferredBilling, &imageTaskDeferredBilling{
			Usage:        usageCopy,
			ExtraContent: append([]string(nil), logContent...),
		})
		return nil
	}

	service.PostTextConsumeQuota(c, info, usage.(*dto.Usage), logContent)
	return nil
}

func getImageTaskDeferredBilling(c *gin.Context) (*imageTaskDeferredBilling, bool) {
	if c == nil {
		return nil, false
	}
	value, exists := c.Get(contextKeyImageTaskDeferredBilling)
	if !exists {
		return nil, false
	}
	deferred, ok := value.(*imageTaskDeferredBilling)
	return deferred, ok
}

func applyImageStreamSupportForChannel(c *gin.Context, info *relaycommon.RelayInfo, imageReq *dto.ImageRequest) bool {
	if info == nil || imageReq == nil {
		return false
	}
	stream := imageReq.Stream != nil && *imageReq.Stream
	info.IsStream = stream
	if c != nil {
		c.Set(string(constant.ContextKeyIsStream), stream)
		c.Set(contextKeyImageStreamAllowed, stream)
	}
	if !stream {
		return false
	}
	if info.ApiType == constant.APITypeOpenAI || info.ApiType == constant.APITypeXai {
		return false
	}
	imageReq.Stream = common.GetPointer(false)
	info.IsStream = false
	if c != nil {
		c.Set(string(constant.ContextKeyIsStream), false)
		c.Set(contextKeyImageStreamAllowed, false)
	}
	return true
}

func buildImagePassThroughBody(c *gin.Context, info *relaycommon.RelayInfo, imageStreamDisabled bool) (io.Reader, func(), error) {
	contentType := c.Request.Header.Get("Content-Type")
	if strings.Contains(contentType, "multipart/form-data") && c.Request.MultipartForm != nil {
		return buildImageMultipartPassThroughBody(c, info, imageStreamDisabled)
	}

	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, nil, err
	}
	if !imageStreamDisabled {
		return common.NewReplayableBodyReader(storage), nil, nil
	}

	if strings.HasPrefix(contentType, "application/json") {
		return buildImageJSONPassThroughBodyWithoutStream(info, storage)
	}
	if strings.Contains(contentType, "multipart/form-data") {
		return buildImageMultipartPassThroughBody(c, info, true)
	}
	return common.NewReplayableBodyReader(storage), nil, nil
}

func buildImageJSONPassThroughBodyWithoutStream(info *relaycommon.RelayInfo, storage common.BodyStorage) (io.Reader, func(), error) {
	requestBody, err := storage.Bytes()
	if err != nil {
		return nil, nil, err
	}

	var bodyMap map[string]json.RawMessage
	if err := common.Unmarshal(requestBody, &bodyMap); err != nil {
		return nil, nil, err
	}
	bodyMap["stream"] = json.RawMessage("false")

	jsonData, err := common.Marshal(bodyMap)
	if err != nil {
		return nil, nil, err
	}
	body, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
	if err != nil {
		return nil, nil, err
	}
	return body, func() {
		_ = closer.Close()
	}, nil
}

func buildImageMultipartPassThroughBody(c *gin.Context, info *relaycommon.RelayInfo, disableStream bool) (io.Reader, func(), error) {
	originalContentType := c.Request.Header.Get("Content-Type")
	form, removeForm, err := getImageMultipartFormForPassThrough(c)
	if err != nil {
		return nil, nil, err
	}
	if removeForm {
		defer form.RemoveAll()
	}

	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	for key, values := range form.Value {
		if disableStream && key == "stream" {
			continue
		}
		for _, value := range values {
			if err := writer.WriteField(key, value); err != nil {
				return nil, nil, err
			}
		}
	}

	for fieldName, fileHeaders := range form.File {
		for _, fileHeader := range fileHeaders {
			file, err := fileHeader.Open()
			if err != nil {
				return nil, nil, err
			}

			partHeader := make(textproto.MIMEHeader)
			partHeader.Set("Content-Disposition", fmt.Sprintf(
				`form-data; name="%s"; filename="%s"`,
				escapeMultipartHeaderValue(fieldName),
				escapeMultipartHeaderValue(fileHeader.Filename),
			))
			if fileContentType := fileHeader.Header.Get("Content-Type"); fileContentType != "" {
				partHeader.Set("Content-Type", fileContentType)
			}
			part, err := writer.CreatePart(partHeader)
			if err != nil {
				_ = file.Close()
				return nil, nil, err
			}
			if _, err := io.Copy(part, file); err != nil {
				_ = file.Close()
				return nil, nil, err
			}
			_ = file.Close()
		}
	}

	if err := writer.Close(); err != nil {
		return nil, nil, err
	}

	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	return &requestBody, func() {
		c.Request.Header.Set("Content-Type", originalContentType)
	}, nil
}

func getImageMultipartFormForPassThrough(c *gin.Context) (*multipart.Form, bool, error) {
	if c == nil || c.Request == nil {
		return nil, false, fmt.Errorf("request is nil")
	}
	if c.Request.MultipartForm != nil {
		return c.Request.MultipartForm, false, nil
	}
	form, err := common.ParseMultipartFormReusable(c)
	return form, true, err
}

func escapeMultipartHeaderValue(value string) string {
	return strings.NewReplacer("\\", "\\\\", `"`, "\\\"").Replace(value)
}
