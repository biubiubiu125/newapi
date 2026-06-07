package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const contextKeyImageStreamAllowed = "image_stream_allowed"

func OpenaiImageResponseHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if !isImageStreamAllowed(c, info) {
		return OpenaiHandlerWithUsage(c, info, resp)
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return OpenaiImageStreamHandler(c, info, resp)
	}
	return OpenaiImageJSONAsStreamHandler(c, info, resp)
}

func isImageStreamAllowed(c *gin.Context, info *relaycommon.RelayInfo) bool {
	if c != nil {
		if allowed, exists := c.Get(contextKeyImageStreamAllowed); exists {
			return allowed == true
		}
	}
	return info != nil && info.IsStream
}

func OpenaiImageStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid image stream response or response body")
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	defer service.CloseResponseBodyGracefully(resp)

	usage := &dto.Usage{}
	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		if err := helper.StringData(c, data); err != nil {
			logger.LogError(c, "failed to write image stream data: "+err.Error())
			sr.Stop(err)
			return
		}
		updateImageStreamUsage(usage, data)
	})

	if info.StreamStatus == nil || (info.StreamStatus.IsNormalEnd() && !info.StreamStatus.HasErrors()) {
		helper.Done(c)
	}
	return usage, nil
}

func OpenaiImageJSONAsStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	var usageResp dto.SimpleResponse
	if err := common.Unmarshal(responseBody, &usageResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	streamData := compactJSONForSSE(responseBody)
	if err := helper.StringData(c, string(streamData)); err != nil {
		logger.LogError(c, "failed to write image json stream data: "+err.Error())
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	helper.Done(c)

	if usageResp.InputTokens > 0 {
		usageResp.PromptTokens += usageResp.InputTokens
	}
	if usageResp.OutputTokens > 0 {
		usageResp.CompletionTokens += usageResp.OutputTokens
	}
	if usageResp.InputTokensDetails != nil {
		usageResp.PromptTokensDetails.ImageTokens += usageResp.InputTokensDetails.ImageTokens
		usageResp.PromptTokensDetails.TextTokens += usageResp.InputTokensDetails.TextTokens
	}
	applyUsagePostProcessing(info, &usageResp.Usage, responseBody)
	return &usageResp.Usage, nil
}

func compactJSONForSSE(data []byte) []byte {
	var buf bytes.Buffer
	if err := json.Compact(&buf, data); err != nil {
		return data
	}
	return buf.Bytes()
}

func updateImageStreamUsage(usage *dto.Usage, data string) {
	if usage == nil || data == "" || !gjson.Valid(data) {
		return
	}

	for _, path := range []string{"usage", "response.usage", "data.usage"} {
		if rawUsage := gjson.Get(data, path); rawUsage.Exists() && rawUsage.IsObject() {
			mergeImageStreamUsage(usage, rawUsage.Raw)
		}
	}
}

func mergeImageStreamUsage(usage *dto.Usage, raw string) {
	if usage == nil || raw == "" {
		return
	}

	var streamUsage dto.Usage
	if err := common.Unmarshal([]byte(raw), &streamUsage); err != nil {
		return
	}

	if streamUsage.InputTokens > 0 {
		usage.InputTokens = streamUsage.InputTokens
		usage.PromptTokens = streamUsage.InputTokens
	}
	if streamUsage.OutputTokens > 0 {
		usage.OutputTokens = streamUsage.OutputTokens
		usage.CompletionTokens = streamUsage.OutputTokens
	}
	if streamUsage.PromptTokens > 0 {
		usage.PromptTokens = streamUsage.PromptTokens
	}
	if streamUsage.CompletionTokens > 0 {
		usage.CompletionTokens = streamUsage.CompletionTokens
	}
	if streamUsage.TotalTokens > 0 {
		usage.TotalTokens = streamUsage.TotalTokens
	}
	if streamUsage.PromptTokensDetails.CachedTokens > 0 {
		usage.PromptTokensDetails.CachedTokens = streamUsage.PromptTokensDetails.CachedTokens
	}
	if streamUsage.PromptTokensDetails.TextTokens > 0 {
		usage.PromptTokensDetails.TextTokens = streamUsage.PromptTokensDetails.TextTokens
	}
	if streamUsage.PromptTokensDetails.ImageTokens > 0 {
		usage.PromptTokensDetails.ImageTokens = streamUsage.PromptTokensDetails.ImageTokens
	}
	if streamUsage.InputTokensDetails != nil {
		if usage.InputTokensDetails == nil {
			usage.InputTokensDetails = &dto.InputTokenDetails{}
		}
		if streamUsage.InputTokensDetails.CachedTokens > 0 {
			usage.InputTokensDetails.CachedTokens = streamUsage.InputTokensDetails.CachedTokens
			usage.PromptTokensDetails.CachedTokens = streamUsage.InputTokensDetails.CachedTokens
		}
		if streamUsage.InputTokensDetails.TextTokens > 0 {
			usage.InputTokensDetails.TextTokens = streamUsage.InputTokensDetails.TextTokens
			usage.PromptTokensDetails.TextTokens = streamUsage.InputTokensDetails.TextTokens
		}
		if streamUsage.InputTokensDetails.ImageTokens > 0 {
			usage.InputTokensDetails.ImageTokens = streamUsage.InputTokensDetails.ImageTokens
			usage.PromptTokensDetails.ImageTokens = streamUsage.InputTokensDetails.ImageTokens
		}
	}

	if usage.TotalTokens == 0 && (usage.PromptTokens > 0 || usage.CompletionTokens > 0) {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
}
