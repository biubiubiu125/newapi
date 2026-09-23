package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var (
	relayTaskSubmitFunc                          = relay.RelayTaskSubmit
	settleBillingFunc                            = service.SettleBilling
	logTaskConsumptionFunc                       = service.LogTaskConsumption
	refundTaskQuotaFunc                          = service.RefundTaskQuota
	persistRefundableSubmitAccountingFailureFunc = persistRefundableSubmitAccountingFailure
	persistTaskSubmitSettlementErrorFunc         = persistTaskSubmitSettlementError
	persistImmediateTaskQuotaFunc                = persistImmediateTaskQuota
	updateTaskAfterSubmitAccountingFailureFunc   = model.UpdateTaskAfterSubmitAccountingFailure
)

func persistImmediateTaskQuota(task *model.Task) error {
	if task == nil {
		return nil
	}
	return task.UpdateQuota()
}

func newSensitiveWordsError() *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		errors.New("sensitive words detected"),
		types.ErrorCodeSensitiveWordsDetected,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	)
}

type taskSubmissionOutcome struct {
	Result    *relay.TaskSubmitResult
	Task      *model.Task
	RelayInfo *relaycommon.RelayInfo
}

func relayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	switch info.RelayMode {
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		err = relay.ImageHelper(c, info)
	case relayconstant.RelayModeAudioSpeech:
		fallthrough
	case relayconstant.RelayModeAudioTranslation:
		fallthrough
	case relayconstant.RelayModeAudioTranscription:
		err = relay.AudioHelper(c, info)
	case relayconstant.RelayModeRerank:
		err = relay.RerankHelper(c, info)
	case relayconstant.RelayModeEmbeddings:
		err = relay.EmbeddingHelper(c, info)
	case relayconstant.RelayModeResponses, relayconstant.RelayModeResponsesCompact:
		err = relay.ResponsesHelper(c, info)
	case relayconstant.RelayModeAlphaSearch:
		err = relay.AlphaSearchHelper(c, info)
	default:
		err = relay.TextHelper(c, info)
	}
	return err
}

func geminiRelayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	if strings.Contains(c.Request.URL.Path, "embed") {
		err = relay.GeminiEmbeddingHandler(c, info)
	} else {
		err = relay.GeminiHelper(c, info)
	}
	return err
}

func Relay(c *gin.Context, relayFormat types.RelayFormat) {

	requestId := c.GetString(common.RequestIdKey)
	//group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	//originalModel := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)

	var (
		newAPIError *types.NewAPIError
		ws          *websocket.Conn
	)

	writeRelayErrorResponse := func() {
		if newAPIError == nil {
			return
		}
		logger.LogError(c, fmt.Sprintf("relay error: %s", common.LocalLogPreview(newAPIError.Error())))
		if shouldSuppressRelayErrorResponse(c) {
			return
		}
		newAPIError.SetMessage(common.MessageWithRequestId(newAPIError.Error(), requestId))
		switch relayFormat {
		case types.RelayFormatOpenAIRealtime:
			helper.WssError(c, ws, newAPIError.ToOpenAIError())
		case types.RelayFormatClaude:
			c.JSON(newAPIError.StatusCode, gin.H{
				"type":  "error",
				"error": newAPIError.ToClaudeError(),
			})
		default:
			c.JSON(newAPIError.StatusCode, gin.H{
				"error": newAPIError.ToOpenAIError(),
			})
		}
	}

	if relayFormat == types.RelayFormatOpenAIRealtime {
		var err error
		ws, err = upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			helper.WssError(c, ws, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry()).ToOpenAIError())
			return
		}
		defer ws.Close()
	}

	defer func() {
		writeRelayErrorResponse()
	}()

	request, err := helper.GetAndValidateRequest(c, relayFormat)
	if err != nil {
		// Map "request body too large" to 413 so clients can handle it correctly
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
		} else {
			newAPIError = types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithStatusCode(http.StatusBadRequest), types.ErrOptionWithSkipRetry())
		}
		return
	}

	relayInfo, err := relaycommon.GenRelayInfo(c, relayFormat, request, ws)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeGenRelayInfoFailed)
		return
	}
	c.Set("relay_info", relayInfo)
	defer func() {
		writeRelayErrorResponse()
		newAPIError = nil
	}()

	if relayFormat == types.RelayFormatOpenAIImage {
		if handled, bridgeErr := tryRelayImageTaskSyncBridge(c, request, relayInfo); handled {
			newAPIError = bridgeErr
			return
		}
	}

	needSensitiveCheck := setting.ShouldCheckPromptSensitive()
	needCountToken := constant.CountToken
	// Avoid building huge CombineText (strings.Join) when token counting and sensitive check are both disabled.
	var meta *types.TokenCountMeta
	if needSensitiveCheck || needCountToken {
		meta = request.GetTokenCountMeta()
	} else {
		meta = fastTokenCountMetaForPricing(request)
	}

	if needSensitiveCheck && meta != nil {
		contains, words := service.CheckSensitiveText(meta.CombineText)
		if contains {
			logger.LogWarn(c, fmt.Sprintf("user sensitive words detected: %s", strings.Join(words, ", ")))
			newAPIError = newSensitiveWordsError()
			return
		}
	}

	tokens, err := service.EstimateRequestToken(c, meta, relayInfo)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeCountTokenFailed)
		return
	}

	relayInfo.SetEstimatePromptTokens(tokens)

	priceData, err := helper.ModelPriceHelper(c, relayInfo, tokens, meta)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
		return
	}

	// common.SetContextKey(c, constant.ContextKeyTokenCountMeta, meta)

	preConsumed := false
	if priceData.FreeModel {
		logger.LogInfo(c, fmt.Sprintf("模型 %s 免费，跳过预扣费", relayInfo.OriginModelName))
	} else if relayInfo.TokenGroup != "auto" {
		newAPIError = service.PreConsumeBilling(c, priceData.QuotaToPreConsume, relayInfo)
		if newAPIError != nil {
			return
		}
		preConsumed = true
	}

	defer func() {
		// Only return quota if downstream failed and quota was actually pre-consumed
		if newAPIError != nil {
			newAPIError = service.NormalizeViolationFeeError(newAPIError)
			if relayInfo.Billing != nil {
				if refundErr := relayInfo.Billing.Refund(c); refundErr != nil {
					common.SysError("refund billing after relay error failed: " + refundErr.Error())
					service.RecordConsumeAccountingError(c, relayInfo, "refund billing after relay error", refundErr)
				}
			}
			service.ChargeViolationFeeIfNeeded(c, relayInfo, newAPIError)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:        c,
		TokenGroup: relayInfo.TokenGroup,
		ModelName:  relayInfo.OriginModelName,
		Retry:      common.GetPointer(0),
	}
	relayInfo.RetryIndex = 0
	relayInfo.LastError = nil

	for {
		retryParam.ExcludeChannelIds = getFailedChannelIds(c)
		relayInfo.RetryIndex = retryParam.GetRetry()
		channel, channelErr := getChannel(c, relayInfo, retryParam)
		if channelErr != nil {
			logger.LogError(c, channelErr.Error())
			if channel != nil {
				addUsedChannel(c, channel.Id)
				addFailedChannel(c, channel.Id)
				retryParam.ExcludeChannelIds = getFailedChannelIds(c)
				processChannelError(c, *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()), channelErr, true)
				relayInfo.LastError = channelErr
				if shouldRetry(c, channelErr, common.RetryTimes-retryParam.GetRetry()) {
					retryParam.IncreaseRetry()
					continue
				}
			}
			if relayInfo.LastError != nil {
				newAPIError = relayInfo.LastError
			} else {
				newAPIError = channelErr
			}
			break
		}

		addUsedChannel(c, channel.Id)
		priceData, err = helper.ModelPriceHelper(c, relayInfo, tokens, meta)
		if err != nil {
			newAPIError = types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
			break
		}
		if billingErr := service.PrepareTieredBillingForSelectedGroup(c, relayInfo); billingErr != nil {
			newAPIError = billingErr
			break
		}
		preConsumed = preConsumed || relayInfo.Billing != nil

		if !preConsumed && !priceData.FreeModel {
			newAPIError = service.PreConsumeBilling(c, priceData.QuotaToPreConsume, relayInfo)
			if newAPIError != nil {
				break
			}
			preConsumed = true
		}

		retryParam.ExcludeChannelIds = getFailedChannelIds(c)
		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			// Ensure consistent 413 for oversized bodies even when error occurs later (e.g., retry path)
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
			} else {
				newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			}
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		switch relayFormat {
		case types.RelayFormatOpenAIRealtime:
			newAPIError = relay.WssHelper(c, relayInfo)
		case types.RelayFormatClaude:
			newAPIError = relay.ClaudeHelper(c, relayInfo)
		case types.RelayFormatGemini:
			newAPIError = geminiRelayHandler(c, relayInfo)
		default:
			newAPIError = relayHandler(c, relayInfo)
		}

		if newAPIError == nil {
			relayInfo.LastError = nil
			return
		}

		newAPIError = service.NormalizeViolationFeeError(newAPIError)
		relayInfo.LastError = newAPIError
		addFailedChannel(c, channel.Id)
		retryParam.ExcludeChannelIds = getFailedChannelIds(c)

		processChannelError(c, *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()), newAPIError, true)

		if !shouldRetry(c, newAPIError, common.RetryTimes-retryParam.GetRetry()) {
			break
		}
		retryParam.IncreaseRetry()
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}
	if newAPIError != nil {
		perfmetrics.RecordRelaySampleAsync(relayInfo, false, 0)
	}
}

func shouldSuppressRelayErrorResponse(c *gin.Context) bool {
	if c == nil || !common.GetContextKeyBool(c, constant.ContextKeyIsStream) {
		return false
	}
	relayInfoValue, ok := c.Get("relay_info")
	if !ok {
		return false
	}
	info, ok := relayInfoValue.(*relaycommon.RelayInfo)
	return ok && info.HasClientStreamWrite()
}

var upgrader = websocket.Upgrader{
	Subprotocols: []string{"realtime"}, // WS 握手支持的协议，如果有使用 Sec-WebSocket-Protocol，则必须在此声明对应的 Protocol TODO add other protocol
	CheckOrigin:  checkRelayWebSocketOrigin,
}

func checkRelayWebSocketOrigin(request *http.Request) bool {
	if request == nil {
		return false
	}
	origins := request.Header.Values("Origin")
	if len(origins) == 0 {
		return true
	}
	if len(origins) != 1 {
		return false
	}
	origin, err := common.NormalizeOrigin(origins[0])
	if err != nil {
		return false
	}
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	}
	requestOrigin, err := common.NormalizeOrigin(scheme + "://" + request.Host)
	if err == nil && origin == requestOrigin {
		return true
	}
	for _, trusted := range common.SessionCookieTrustedURLs {
		if origin == trusted {
			return true
		}
	}
	for _, configured := range strings.Split(os.Getenv("CORS_ALLOWED_ORIGINS"), ",") {
		normalized, normalizeErr := common.NormalizeOrigin(configured)
		if normalizeErr == nil && origin == normalized {
			return true
		}
	}
	return false
}

func addUsedChannel(c *gin.Context, channelId int) {
	useChannel := c.GetStringSlice("use_channel")
	useChannel = append(useChannel, fmt.Sprintf("%d", channelId))
	c.Set("use_channel", useChannel)
}

func addFailedChannel(c *gin.Context, channelId int) {
	if channelId <= 0 {
		return
	}
	failed := getFailedChannelIds(c)
	for _, id := range failed {
		if id == channelId {
			return
		}
	}
	failed = append(failed, channelId)
	c.Set("failed_channel_ids", failed)
}

func getFailedChannelIds(c *gin.Context) []int {
	raw, exists := c.Get("failed_channel_ids")
	if !exists {
		return nil
	}
	ids, ok := raw.([]int)
	if !ok {
		return nil
	}
	return ids
}

func fastTokenCountMetaForPricing(request dto.Request) *types.TokenCountMeta {
	if request == nil {
		return &types.TokenCountMeta{}
	}
	meta := &types.TokenCountMeta{
		TokenType: types.TokenTypeTokenizer,
	}
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		maxCompletionTokens := lo.FromPtrOr(r.MaxCompletionTokens, uint(0))
		maxTokens := lo.FromPtrOr(r.MaxTokens, uint(0))
		if maxCompletionTokens > maxTokens {
			meta.MaxTokens = int(maxCompletionTokens)
		} else {
			meta.MaxTokens = int(maxTokens)
		}
	case *dto.OpenAIResponsesRequest:
		meta.MaxTokens = int(lo.FromPtrOr(r.MaxOutputTokens, uint(0)))
	case *dto.ClaudeRequest:
		meta.MaxTokens = int(lo.FromPtr(r.MaxTokens))
	case *dto.ImageRequest:
		// Pricing for image requests depends on ImagePriceRatio; safe to compute even when CountToken is disabled.
		return r.GetTokenCountMeta()
	default:
		// Best-effort: leave CombineText empty to avoid large allocations.
	}
	return meta
}

func getChannel(c *gin.Context, info *relaycommon.RelayInfo, retryParam *service.RetryParam) (*model.Channel, *types.NewAPIError) {
	if info.ChannelMeta == nil {
		channelId := c.GetInt("channel_id")
		preselectedUsable := channelId > 0
		if preselectedUsable {
			for _, failedId := range retryParam.ExcludeChannelIds {
				if failedId == channelId {
					preselectedUsable = false
					break
				}
			}
		}
		if preselectedUsable {
			autoBan := c.GetBool("auto_ban")
			autoBanInt := 1
			if !autoBan {
				autoBanInt = 0
			}
			return &model.Channel{
				Id:      channelId,
				Type:    c.GetInt("channel_type"),
				Name:    c.GetString("channel_name"),
				AutoBan: &autoBanInt,
			}, nil
		}
	}
	channel, selectGroup, err := service.CacheGetRandomSatisfiedChannel(retryParam)
	if err != nil {
		return nil, types.NewError(errors.New(i18n.ProtocolMessage(i18n.MsgProtocolChannelRetryFailed, map[string]any{"Group": selectGroup, "Model": info.OriginModelName, "Error": err.Error()})), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if channel == nil {
		return nil, types.NewError(errors.New(i18n.ProtocolMessage(i18n.MsgProtocolChannelRetryMissing, map[string]any{"Group": selectGroup, "Model": info.OriginModelName})), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}

	info.PriceData.GroupRatioInfo = helper.HandleGroupRatio(c, info)

	newAPIError := middleware.SetupContextForSelectedChannel(c, channel, info.OriginModelName)
	if newAPIError != nil {
		return channel, newAPIError
	}
	return channel, nil
}

func shouldRetry(c *gin.Context, openaiErr *types.NewAPIError, retryTimes int) bool {
	if openaiErr == nil || retryTimes <= 0 {
		return false
	}
	if openaiErr != nil && common.GetContextKeyBool(c, constant.ContextKeyIsStream) {
		if relayInfo, ok := c.Get("relay_info"); ok {
			if info, ok := relayInfo.(*relaycommon.RelayInfo); ok && info.HasClientStreamWrite() {
				return false
			}
		}
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if types.IsSkipRetryError(openaiErr) {
		return false
	}
	if types.IsChannelError(openaiErr) || service.IsBalanceInsufficientError(openaiErr) {
		return true
	}
	code := openaiErr.StatusCode
	if code >= 200 && code < 300 {
		return false
	}
	if code < 100 || code > 599 {
		return true
	}
	return operation_setting.ShouldRetryByStatusCode(code)
}

func processChannelError(c *gin.Context, channelError types.ChannelError, err *types.NewAPIError, disableChannel bool) {
	logger.LogError(c, fmt.Sprintf("channel error (channel #%d, status code: %d): %s", channelError.ChannelId, err.StatusCode, common.LocalLogPreview(err.Error())))
	// 不要使用context获取渠道信息，异步处理时可能会出现渠道信息不一致的情况
	// do not use context to get channel info, there may be inconsistent channel info when processing asynchronously
	if disableChannel && service.ShouldDisableChannel(err) && (channelError.AutoBan || service.IsBalanceInsufficientError(err)) {
		reason := err.ErrorWithStatusCode()
		if service.IsBalanceInsufficientError(err) {
			service.DisableChannel(channelError, reason)
		} else {
			gopool.Go(func() {
				service.DisableChannel(channelError, reason)
			})
		}
	}

	if constant.ErrorLogEnabled && types.IsRecordErrorLog(err) {
		// 保存错误日志到mysql中
		userId := c.GetInt("id")
		tokenName := c.GetString("token_name")
		modelName := c.GetString("original_model")
		tokenId := c.GetInt("token_id")
		userGroup := c.GetString("group")
		channelId := c.GetInt("channel_id")
		other := make(map[string]interface{})
		if c.Request != nil && c.Request.URL != nil {
			other["request_path"] = c.Request.URL.Path
		}
		other["error_type"] = err.GetErrorType()
		other["error_code"] = err.GetErrorCode()
		other["status_code"] = err.StatusCode
		other["channel_id"] = channelId
		other["channel_name"] = c.GetString("channel_name")
		other["channel_type"] = c.GetInt("channel_type")
		adminInfo := make(map[string]interface{})
		adminInfo["use_channel"] = c.GetStringSlice("use_channel")
		isMultiKey := common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey)
		if isMultiKey {
			adminInfo["is_multi_key"] = true
			adminInfo["multi_key_index"] = common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
		}
		service.AppendChannelAffinityAdminInfo(c, adminInfo)
		other["admin_info"] = adminInfo
		startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
		if startTime.IsZero() {
			startTime = time.Now()
		}
		useTimeSeconds := int(time.Since(startTime).Seconds())
		model.RecordErrorLog(c, userId, channelId, modelName, tokenName, err.MaskSensitiveErrorWithStatusCode(), tokenId, useTimeSeconds, common.GetContextKeyBool(c, constant.ContextKeyIsStream), userGroup, other)
	}

}

func RelayMidjourney(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatMjProxy, nil, nil)

	if err != nil {
		original := fmt.Sprintf("failed to generate relay info: %s", err.Error())
		logger.LogError(c, original)
		c.JSON(http.StatusInternalServerError, gin.H{
			"description": common.PublicRequestErrorMessage(original),
			"type":        "upstream_error",
			"code":        4,
		})
		return
	}

	var mjErr *dto.MidjourneyResponse
	switch relayInfo.RelayMode {
	case relayconstant.RelayModeMidjourneyNotify:
		mjErr = relay.RelayMidjourneyNotify(c)
	case relayconstant.RelayModeMidjourneyTaskFetch, relayconstant.RelayModeMidjourneyTaskFetchByCondition:
		mjErr = relay.RelayMidjourneyTask(c, relayInfo.RelayMode)
	case relayconstant.RelayModeMidjourneyTaskImageSeed:
		mjErr = relay.RelayMidjourneyTaskImageSeed(c)
	case relayconstant.RelayModeSwapFace:
		mjErr = relay.RelaySwapFace(c, relayInfo)
	default:
		mjErr = relay.RelayMidjourneySubmit(c, relayInfo)
	}
	if mjErr != nil {
		statusCode := http.StatusBadRequest
		if mjErr.Code == 30 {
			mjErr.Result = i18n.ProtocolMessage(i18n.MsgProtocolGroupSaturatedUpgrade)
			statusCode = http.StatusTooManyRequests
		}
		original := strings.TrimSpace(fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result))
		c.JSON(statusCode, gin.H{
			"description": common.PublicRequestErrorMessage(original),
			"type":        "upstream_error",
			"code":        mjErr.Code,
		})
		channelId := c.GetInt("channel_id")
		logger.LogError(c, fmt.Sprintf("relay error (channel #%d, status code %d): %s", channelId, statusCode, original))
	}
}

func RelayNotImplemented(c *gin.Context) {
	err := types.OpenAIError{
		Message: "API not implemented",
		Type:    "new_api_error",
		Param:   "",
		Code:    "api_not_implemented",
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": err,
	})
}

func RelayNotFound(c *gin.Context) {
	err := types.OpenAIError{
		Message: fmt.Sprintf("Invalid URL (%s %s)", c.Request.Method, c.Request.URL.Path),
		Type:    "invalid_request_error",
		Param:   "",
		Code:    "",
	}
	c.JSON(http.StatusNotFound, gin.H{
		"error": err,
	})
}

// RelayTaskPluginEndpoint keeps unclaimed shared-endpoint traffic on its
// existing handler while claimed requests enter the generation-pinned
// protocol bridge.
func RelayTaskPluginEndpoint(c *gin.Context, fallback gin.HandlerFunc) {
	pinnedValue, exists := c.Get(pluginruntime.ContextKeyPinnedEndpoint)
	if !exists {
		fallback(c)
		return
	}
	pinned, ok := pinnedValue.(pluginruntime.PinnedEndpoint)
	if !ok || pinned.Plugin == nil || pinned.Generation == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"message": "Task protocol request failed",
				"type":    "new_api_error",
				"code":    "task_protocol_error",
			},
		})
		return
	}
	if pinned.Protocol != "openai_responses" {
		fallback(c)
		return
	}
	serveTaskPluginProtocol(c, pinned, defaultPluginProtocolBridgeDeps())
}

func RelayTaskOrFetch(c *gin.Context) {
	if c.GetInt("relay_mode") == relayconstant.RelayModeVideoFetchByID {
		RelayTaskFetch(c)
		return
	}
	RelayTask(c)
}

func RelayTaskFetch(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		respondTaskError(c, service.TaskErrorWrapper(err, "gen_relay_info_failed", http.StatusInternalServerError))
		return
	}
	if taskErr := relay.RelayTaskFetch(c, relayInfo.RelayMode); taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

func RelayTask(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		respondTaskSubmissionError(c, service.TaskErrorWrapper(err, "gen_relay_info_failed", http.StatusInternalServerError))
		return
	}
	if action := c.GetString("task_action"); action != "" {
		relayInfo.Action = action
	}

	if taskErr := relay.ResolveOriginTask(c, relayInfo); taskErr != nil {
		respondTaskSubmissionError(c, taskErr)
		return
	}
	if taskErr := relay.ApplyOriginTaskAffinity(c, relayInfo); taskErr != nil {
		respondTaskSubmissionError(c, taskErr)
		return
	}

	outcome, taskErr := executeTaskSubmission(c, relayInfo)
	if taskErr != nil {
		respondTaskSubmissionError(c, taskErr)
		return
	}
	presentTaskSubmission(c, outcome)
}

// executeTaskSubmission owns the retry, billing, and persistence lifecycle.
// It deliberately performs no client response writes so JSON and protocol
// presenters share the same durable task barrier. Its cancellation semantics
// come from c.Request.Context: native task endpoints use the client context,
// while the Responses bridge supplies an independently bounded context.
func executeTaskSubmission(c *gin.Context, relayInfo *relaycommon.RelayInfo) (*taskSubmissionOutcome, *dto.TaskError) {
	return executeTaskSubmissionWith(c, relayInfo, relayTaskSubmitFunc)
}

type taskSubmitAttempt func(*gin.Context, *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError)

func executeTaskSubmissionWith(
	c *gin.Context,
	relayInfo *relaycommon.RelayInfo,
	submit taskSubmitAttempt,
) (*taskSubmissionOutcome, *dto.TaskError) {
	diagnostics := newTaskPluginSubmitDiagnostics(c)
	diagnostics.start(relayInfo)
	var result *relay.TaskSubmitResult
	var taskErr *dto.TaskError
	durable := false
	stage := "start"
	defer func() {
		if !durable && relayInfo.Billing != nil {
			diagnostics.refund(stage)
			relayInfo.Billing.Refund(c)
		}
	}()
	stage = "before_attempt"
	if requestErr := c.Request.Context().Err(); requestErr != nil {
		diagnostics.cancelled("before_attempt", 0)
		return nil, service.TaskErrorWrapperLocal(requestErr, "request_cancelled", http.StatusRequestTimeout)
	}

	retryParam := newTaskPluginRetryParam(c, relayInfo)

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		retryParam.ExcludeChannelIds = getFailedChannelIds(c)
		stage = "select_channel"
		if requestErr := c.Request.Context().Err(); requestErr != nil {
			diagnostics.cancelled("before_attempt", retryParam.GetRetry()+1)
			taskErr = service.TaskErrorWrapperLocal(requestErr, "request_cancelled", http.StatusRequestTimeout)
			break
		}
		var channel *model.Channel

		if lockedCh, ok := relayInfo.LockedChannel.(*model.Channel); ok && lockedCh != nil {
			channel = lockedCh
			preferredKey := ""
			if common.GetContextKeyInt(c, constant.ContextKeyChannelId) == channel.Id {
				preferredKey = common.GetContextKeyString(c, constant.ContextKeyChannelKey)
			}
			if setupErr := middleware.SetupContextForSelectedChannelWithKey(c, channel, relayInfo.OriginModelName, preferredKey); setupErr != nil {
				taskErr = service.TaskErrorWrapperLocal(setupErr.Err, "setup_locked_channel_failed", http.StatusInternalServerError)
				break
			}
		} else {
			var channelErr *types.NewAPIError
			channel, channelErr = getChannel(c, relayInfo, retryParam)
			if channelErr != nil {
				logger.LogError(c, channelErr.Error())
				if channel != nil {
					addUsedChannel(c, channel.Id)
					addFailedChannel(c, channel.Id)
					retryParam.ExcludeChannelIds = getFailedChannelIds(c)
					processChannelError(c, *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()), channelErr, true)
					if shouldRetry(c, channelErr, common.RetryTimes-retryParam.GetRetry()) {
						continue
					}
				}
				taskErr = service.TaskErrorWrapperLocal(channelErr.Err, "get_channel_failed", http.StatusInternalServerError)
				break
			}
		}
		diagnostics.attempt(retryParam.GetRetry()+1, channel, relayInfo.LockedChannel != nil)

		addUsedChannel(c, channel.Id)
		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			stage = "read_body"
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusRequestEntityTooLarge)
			} else {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusBadRequest)
			}
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		stage = "submit"
		result, taskErr = submit(c, relayInfo)
		if taskErr == nil {
			diagnostics.attemptSucceeded(retryParam.GetRetry()+1, result)
			break
		}
		if requestErr := c.Request.Context().Err(); requestErr != nil {
			diagnostics.cancelled("after_submit", retryParam.GetRetry()+1)
			taskErr = service.TaskErrorWrapperLocal(requestErr, "request_cancelled", http.StatusRequestTimeout)
			break
		}

		addFailedChannel(c, channel.Id)
		retryParam.ExcludeChannelIds = getFailedChannelIds(c)

		if !taskErr.LocalError {
			processChannelError(c,
				*types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey,
					common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()),
				types.NewOpenAIError(taskErr.Error, types.ErrorCodeBadResponseStatusCode, taskErr.StatusCode),
				true)
		}

		willRetry := shouldRetryTaskRelay(c, channel.Id, taskErr, common.RetryTimes-retryParam.GetRetry())
		diagnostics.attemptFailed(retryParam.GetRetry()+1, channel, taskErr, willRetry)
		if !willRetry {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}

	if taskErr != nil {
		diagnostics.failed(stage, "task_error", taskErr, false)
		return nil, taskErr
	}
	if result == nil {
		taskErr = service.TaskErrorWrapperLocal(errors.New("task submission returned no result"), "task_submit_failed", http.StatusInternalServerError)
		diagnostics.failed("submit", "missing_result", taskErr, false)
		return nil, taskErr
	}
	persistCtx := context.WithoutCancel(c.Request.Context())

	// Reserve any submit-time upward billing adjustment before persistence.
	// This keeps insertion failures fully refundable while ensuring settlement
	// after the barrier normally has a zero positive delta.
	if relayInfo.Billing != nil {
		stage = "reserve"
		diagnostics.reserve("reserve_start", result.Quota)
		if reserveErr := relayInfo.Billing.Reserve(result.Quota); reserveErr != nil {
			common.SysError("reserve adjusted task billing error: " + reserveErr.Error())
			taskErr = service.TaskErrorWrapperLocal(errors.New("insufficient quota for adjusted task cost"), string(types.ErrorCodeInsufficientUserQuota), http.StatusForbidden)
			diagnostics.failed("reserve", "insufficient_quota", taskErr, false)
			return nil, taskErr
		}
		diagnostics.reserve("reserve_complete", result.Quota)
	}

	stage = "insert"
	task := model.InitTask(result.Platform, relayInfo)
	task.PrivateData.Execution = service.TaskExecutionSnapshotFromContext(c)
	captureTaskPluginKey(task, relayInfo)
	task.PrivateData.UpstreamTaskID = result.UpstreamTaskID
	task.PrivateData.BillingSource = relayInfo.BillingSource
	task.PrivateData.SubscriptionId = relayInfo.SubscriptionId
	task.PrivateData.TokenId = relayInfo.TokenId
	task.PrivateData.NodeName = common.NodeName
	task.PrivateData.BillingContext = taskBillingContextFromRelayInfo(relayInfo)
	if relayInfo.TieredBillingSnapshot != nil {
		snapshot := *relayInfo.TieredBillingSnapshot
		task.PrivateData.TieredBillingSnapshot = &snapshot
	}
	if relayInfo.BillingRequestInput != nil {
		requestInput := billingexpr.CloneRequestInput(*relayInfo.BillingRequestInput)
		task.PrivateData.BillingRequestInput = &requestInput
		task.PrivateData.BillingRequestInputCaptured = true
	}
	task.Quota = result.Quota
	task.Data = result.TaskData
	if len(result.PluginState) > 0 {
		task.PrivateData.PluginState = result.PluginState
	}
	task.Action = relayInfo.Action
	if immediate := result.Immediate; immediate != nil {
		applyImmediateTaskResult(task, immediate, time.Now().Unix(), relayInfo.ChannelType)
	}
	diagnostics.insertStart(task)
	if insertErr := task.InsertWithContext(persistCtx); insertErr != nil {
		common.SysError("insert task error: " + insertErr.Error())
		taskErr = service.TaskErrorWrapperLocal(errors.New("failed to persist task"), "task_insert_failed", http.StatusInternalServerError)
		diagnostics.failed("insert", "database_error", taskErr, false)
		return nil, taskErr
	}
	durable = true
	stage = "settle"
	diagnostics.durable(task)
	prepaidQuota := result.Quota
	settlementQuota := result.Quota
	if result.Immediate != nil && task.Status == model.TaskStatusSuccess {
		if adjusted := service.AdjustImmediateTaskQuota(persistCtx, result.CompletionBillingAdaptor, task, result.Immediate); adjusted {
			adjustedQuota := task.Quota
			settlementQuota = adjustedQuota
			relayInfo.PriceData.Quota = settlementQuota
			if err := persistImmediateTaskQuotaFunc(task); err != nil {
				common.SysError("persist immediate task quota error: " + err.Error())
				service.RecordConsumeAccountingError(c, relayInfo, "persist immediate task quota", err)
				task.Quota = prepaidQuota
				settlementQuota = prepaidQuota
				relayInfo.PriceData.Quota = prepaidQuota
				service.MarkTaskSettlementReview(persistCtx, task, adjustedQuota, err)
			}
		}
	}
	diagnostics.settleStart(task, settlementQuota)

	if result.Immediate != nil && task.Status == model.TaskStatusFailure {
		// An immediate failure has not gone through the normal consumption-log
		// path. Refund the exact amount that was pre-consumed, and tell the task
		// refund path not to decrement usage counters that were never recorded.
		preConsumedQuota := relayInfo.FinalPreConsumedQuota
		if relayInfo.Billing != nil {
			preConsumedQuota = relayInfo.Billing.GetPreConsumedQuota()
		}
		if preConsumedQuota < 0 {
			preConsumedQuota = 0
		}
		task.Quota = preConsumedQuota
		task.PrivateData.PreConsumedUsageCaptured = true
		task.PrivateData.PreConsumedUsageRecorded = false
		if task.Quota > 0 {
			if refundErr := refundTaskQuotaFunc(persistCtx, task, task.FailReason); refundErr != nil {
				common.SysError("refund immediate task billing error: " + refundErr.Error())
				service.RecordConsumeAccountingError(c, relayInfo, "refund immediate task billing", refundErr)
			}
		} else if err := task.UpdateQuota(); err != nil {
			common.SysError("persist immediate failed task quota error: " + err.Error())
			service.RecordConsumeAccountingError(c, relayInfo, "persist immediate failed task", err)
		}
		diagnostics.complete(task, task.Quota)
		return &taskSubmissionOutcome{Result: result, Task: task, RelayInfo: relayInfo}, nil
	}

	billingLogged := false
	settleErr := settleBillingFunc(c, relayInfo, settlementQuota)
	if settleErr != nil {
		common.SysError("settle task billing error: " + settleErr.Error())
		service.RecordConsumeAccountingError(c, relayInfo, "settle task billing", settleErr)
		c.Set(service.ContextKeySettlementError(), settleErr.Error())
		if updateErr := persistTaskSubmitSettlementErrorFunc(task, relayInfo, settlementQuota, settleErr); updateErr != nil {
			common.SysError("update task settlement error: " + updateErr.Error())
			service.RecordConsumeAccountingError(c, relayInfo, "persist task settlement review", updateErr)
			prepaid := relayInfo.FinalPreConsumedQuota
			if prepaid <= 0 {
				prepaid = task.Quota
			}
			applyTaskSubmitAccountingUsageFlags(c, task)
			if persistErr := persistRefundableSubmitAccountingFailureFunc(task, prepaid, settleErr, updateErr); persistErr != nil {
				common.SysError("update task accounting error: " + persistErr.Error())
				service.RecordConsumeAccountingError(c, relayInfo, "persist task accounting review", persistErr)
				if persistErr = persistRefundableSubmitAccountingFailure(task, prepaid, settleErr, updateErr); persistErr != nil {
					common.SysError("update task accounting error: " + persistErr.Error())
					service.RecordConsumeAccountingError(c, relayInfo, "persist task accounting review", persistErr)
				}
			}
			taskErr = service.TaskErrorWrapperLocal(updateErr, "update_task_settlement_failed", http.StatusInternalServerError)
		}
	} else {
		c.Set(service.ContextKeySettlementApplied(), true)
	}

	if taskErr == nil && c.GetBool(service.ContextKeySettlementApplied()) {
		if err := logTaskConsumptionFunc(c, relayInfo); err != nil {
			common.SysError("log task consumption error: " + err.Error())
			service.RecordConsumeAccountingError(c, relayInfo, "log task consumption", err)
			c.Set(service.ContextKeySettlementError(), err.Error())
			var rollbackErr error
			if relayInfo.Billing != nil {
				rollbackErr = relayInfo.Billing.Rollback(settlementQuota)
				if rollbackErr != nil {
					common.SysError("rollback billing after task error failed: " + rollbackErr.Error())
					service.RecordConsumeAccountingError(c, relayInfo, "rollback billing after task error", rollbackErr)
				}
			}
			if relayInfo.Billing == nil || rollbackErr != nil {
				applyTaskSubmitAccountingUsageFlags(c, task)
				if persistErr := persistRefundableSubmitAccountingFailureFunc(task, settlementQuota, err, rollbackErr); persistErr != nil {
					common.SysError("update task accounting error: " + persistErr.Error())
					service.RecordConsumeAccountingError(c, relayInfo, "persist task accounting review", persistErr)
					if persistErr = persistRefundableSubmitAccountingFailure(task, settlementQuota, err, rollbackErr); persistErr != nil {
						common.SysError("update task accounting error: " + persistErr.Error())
						service.RecordConsumeAccountingError(c, relayInfo, "persist task accounting review", persistErr)
					}
				}
			} else {
				applyTaskSubmitAccountingUsageFlags(c, task)
				if failErr := failPersistedTaskAfterSubmitAccountingError(task, relayInfo, settlementQuota, err); failErr != nil {
					common.SysError("fail persisted task after accounting error: " + failErr.Error())
					service.RecordConsumeAccountingError(c, relayInfo, "fail persisted task after accounting error", failErr)
					if forceErr := model.ForceTaskRefundableAfterSubmitAccountingFailure(task); forceErr != nil {
						common.SysError("force refundable persist after accounting failure error: " + forceErr.Error())
						service.RecordConsumeAccountingError(c, relayInfo, "persist task accounting review", forceErr)
					}
				}
			}
			taskErr = service.TaskErrorWrapperLocal(err, "log_task_consumption_failed", http.StatusInternalServerError)
		} else {
			billingLogged = true
			if persistErr := service.PersistSuccessfulTaskSubmitSettlement(c.Request.Context(), task); persistErr != nil {
				common.SysError("persist task billing settled error: " + persistErr.Error())
				service.RecordConsumeAccountingError(c, relayInfo, "persist task billing settled", persistErr)
			}
		}
	}
	if taskErr != nil && !billingLogged && relayInfo.Billing != nil && !c.GetBool(service.ContextKeySettlementApplied()) && (task == nil || !task.RefundPending) {
		if refundErr := relayInfo.Billing.Refund(c); refundErr != nil {
			common.SysError("refund billing after task error failed: " + refundErr.Error())
			service.RecordConsumeAccountingError(c, relayInfo, "refund billing after task error", refundErr)
		}
	}
	if taskErr != nil {
		diagnostics.failed(stage, "accounting_error", taskErr, true)
		return nil, taskErr
	}
	finalQuota := result.Quota
	if result.Immediate != nil && task.Status == model.TaskStatusSuccess {
		finalQuota = task.Quota
	}
	diagnostics.complete(task, finalQuota)

	return &taskSubmissionOutcome{Result: result, Task: task, RelayInfo: relayInfo}, nil
}

func captureTaskPluginKey(task *model.Task, relayInfo *relaycommon.RelayInfo) {
	if task == nil || relayInfo == nil ||
		task.PrivateData.Execution == nil ||
		task.PrivateData.Execution.TaskPlugin == nil ||
		relayInfo.ChannelMeta == nil ||
		strings.TrimSpace(task.PrivateData.Key) != "" {
		return
	}
	if strings.TrimSpace(relayInfo.ApiKey) != "" {
		task.PrivateData.Key = relayInfo.ApiKey
	}
}

func newTaskPluginRetryParam(c *gin.Context, relayInfo *relaycommon.RelayInfo) *service.RetryParam {
	return &service.RetryParam{
		Ctx:           c,
		TokenGroup:    relayInfo.TokenGroup,
		ModelName:     relayInfo.OriginModelName,
		RequestPath:   c.Request.URL.Path,
		Retry:         common.GetPointer(0),
		ChannelFilter: service.TaskPluginChannelFilter(c),
	}
}

func presentTaskSubmission(c *gin.Context, outcome *taskSubmissionOutcome) {
	diagnostics := newTaskPluginSubmitDiagnostics(c)
	otherRatios := outcome.RelayInfo.PriceData.OtherRatios()
	if otherRatios == nil {
		otherRatios = map[string]float64{}
	}
	if ratiosJSON, err := common.Marshal(otherRatios); err == nil {
		c.Header("X-New-Api-Other-Ratios", string(ratiosJSON))
	}
	if pinnedValue, exists := c.Get(pluginruntime.ContextKeyPinnedRoute); exists {
		if pinned, ok := pinnedValue.(pluginruntime.PinnedRoute); ok && pinned.Plugin != nil && pinned.Route.Render != "" {
			view, err := service.BuildTaskPluginView(outcome.Task)
			requestValue, _ := c.Get(pluginruntime.ContextKeyRouteRequest)
			requestContext, _ := requestValue.(pluginruntime.RouteRequestContext)
			if err == nil {
				viewValue, valueErr := taskPluginProtocolJSONValue(view)
				if valueErr == nil {
					if body, callErr := pinned.Plugin.Engine.CallPath(c.Request.Context(), "native", []string{pinned.Route.Render}, requestContext.JSValue(), viewValue); callErr == nil {
						diagnostics.present(outcome.Task, "native_presenter")
						c.JSON(http.StatusOK, body)
						return
					} else {
						logger.LogError(c, "task plugin native submit presenter failed: "+callErr.Error())
					}
				} else {
					logger.LogError(c, "encode task plugin native submit view failed: "+valueErr.Error())
				}
			} else {
				logger.LogError(c, "build task plugin native submit view failed: "+err.Error())
			}
		}
	}
	if pinnedValue, exists := c.Get(pluginruntime.ContextKeyPinnedEndpoint); exists {
		if pinned, ok := pinnedValue.(pluginruntime.PinnedEndpoint); ok && pinned.Protocol == "openai_video" && pinned.Operation.Name == "create" {
			diagnostics.present(outcome.Task, "openai_video_create")
			c.JSON(http.StatusOK, outcome.Task.ToOpenAIVideo())
			return
		}
	}
	createdAt := outcome.Task.CreatedAt
	if createdAt == 0 {
		createdAt = outcome.Task.SubmitTime
	}
	diagnostics.present(outcome.Task, "host_fallback")
	c.JSON(http.StatusOK, map[string]any{
		"id":         outcome.Task.TaskID,
		"task_id":    outcome.Task.TaskID,
		"status":     taskSubmissionStatus(outcome.Task),
		"model":      outcome.RelayInfo.OriginModelName,
		"created_at": createdAt,
	})
}

func taskSubmissionStatus(task *model.Task) string {
	if task == nil {
		return "queued"
	}
	switch publicStatus := task.PublicStatus(); publicStatus {
	case model.TaskStatusSuccess, model.TaskStatusFailure, model.TaskStatusInProgress:
		return publicStatus.ToVideoStatus()
	default:
		return "queued"
	}
}

func applyImmediateTaskResult(task *model.Task, immediate *relaycommon.TaskInfo, now int64, channelType int) {
	if task == nil || immediate == nil {
		return
	}
	task.Status = model.TaskStatus(immediate.Status)
	if immediate.Progress != "" {
		task.Progress = immediate.Progress
	}
	if len(immediate.PluginState) > 0 {
		task.PrivateData.PluginState = immediate.PluginState
	}
	switch task.Status {
	case model.TaskStatusSuccess:
		// Sora-like platforms persist a proxy URL and let VideoProxy fetch by
		// public task ID. Other platforms must already have a direct or data: URL.
		taskcommon.ApplyTaskSuccessResult(task, immediate.Url, immediate.RemoteUrl, immediate.Reason, now, taskcommon.AllowsEmptyProxyResult(task.Platform, channelType))
	case model.TaskStatusFailure:
		task.Progress = taskcommon.ProgressComplete
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		task.FailReason = immediate.Reason
		task.PrivateData.ResultURL = ""
	}
}

func respondTaskSubmissionError(c *gin.Context, taskErr *dto.TaskError) {
	newTaskPluginSubmitDiagnostics(c).presentError(taskErr)
	if middleware.RespondTaskPluginError(c, taskErr) {
		return
	}
	respondTaskError(c, taskErr)
}

// respondTaskError 统一输出 Task 错误响应（含 429 限流提示改写）
func respondTaskError(c *gin.Context, taskErr *dto.TaskError) {
	if taskErr.StatusCode == http.StatusTooManyRequests {
		taskErr.Message = i18n.ProtocolMessage(i18n.MsgChannelUpstreamSaturated)
	}
	c.JSON(taskErr.StatusCode, taskErr)
}

func taskQuotaAfterSubmitSettlement(relayInfo *relaycommon.RelayInfo, attemptedQuota int, settleErr error) int {
	return service.LogQuotaAfterSettlement(relayInfo, attemptedQuota, settleErr)
}

func attachTaskSubmitSettlementError(task *model.Task, attemptedQuota int, settleErr error) {
	if task == nil || settleErr == nil {
		return
	}
	task.PrivateData.SettlementAttemptQuota = attemptedQuota
	task.PrivateData.SettlementError = appendTaskSettlementError(
		task.PrivateData.SettlementError,
		sanitizeTaskAccountingError(settleErr),
	)
	task.FailReason = service.TaskSettlementReviewFailReason
}

func sanitizeTaskAccountingError(err error) string {
	if err == nil {
		return ""
	}
	return strings.ReplaceAll(err.Error(), "\n", " ")
}

func appendTaskSettlementError(existing string, next string) string {
	existing = strings.TrimSpace(existing)
	next = strings.TrimSpace(next)
	if existing == "" {
		return next
	}
	if next == "" || existing == next {
		return existing
	}
	return existing + "; " + next
}

func persistTaskSubmitSettlementError(task *model.Task, relayInfo *relaycommon.RelayInfo, attemptedQuota int, settleErr error) error {
	if task == nil || settleErr == nil {
		return nil
	}
	task.Quota = taskQuotaAfterSubmitSettlement(relayInfo, attemptedQuota, settleErr)
	attachTaskSubmitSettlementError(task, attemptedQuota, settleErr)
	task.SettlementStatus = model.TaskSettlementStatusReview
	task.NextPollAt = time.Now().Unix() + service.TaskSettlementReviewRetrySeconds
	return task.UpdateSubmitSettlementError()
}

func applyTaskSubmitAccountingUsageFlags(c *gin.Context, task *model.Task) {
	if task == nil {
		return
	}
	task.PrivateData.PreConsumedUsageCaptured = true
	task.PrivateData.PreConsumedUsageRecorded = c != nil && c.GetBool(service.ContextKeyUsageCountersRecorded())
}

func persistRefundableSubmitAccountingFailure(task *model.Task, attemptedQuota int, accountingErr error, rollbackErr error) error {
	if task == nil {
		return nil
	}
	if task.ID <= 0 {
		return fmt.Errorf("mark task submit refund pending failed, taskId=%s, id=%d", task.TaskID, task.ID)
	}
	quota := attemptedQuota
	if quota <= 0 {
		quota = task.Quota
	}
	if quota < 0 {
		quota = 0
	}
	task.Quota = quota
	task.RefundPending = quota > 0 || task.PrivateData.PreConsumedUsageRecorded
	if task.SettlementStatus == model.TaskSettlementStatusReview {
		task.SettlementStatus = ""
	}
	task.Status = model.TaskStatusFailure
	task.Progress = "100%"
	if task.FinishTime == 0 {
		task.FinishTime = common.GetTimestamp()
	}
	task.PrivateData.SettlementAttemptQuota = quota
	if accountingErr != nil {
		task.PrivateData.SettlementError = appendTaskSettlementError(
			task.PrivateData.SettlementError,
			sanitizeTaskAccountingError(accountingErr),
		)
	}
	if rollbackErr != nil {
		task.PrivateData.SettlementError = appendTaskSettlementError(
			task.PrivateData.SettlementError,
			"rollback billing after task error: "+sanitizeTaskAccountingError(rollbackErr),
		)
	}
	task.FailReason = model.TaskPublicAccountingFailReason
	if err := updateTaskAfterSubmitAccountingFailureFunc(task); err != nil {
		if forceErr := model.ForceTaskRefundableAfterSubmitAccountingFailure(task); forceErr != nil {
			return err
		}
	}
	return nil
}

func failPersistedTaskAfterSubmitSettlementError(task *model.Task, relayInfo *relaycommon.RelayInfo, attemptedQuota int, settleErr error, persistErr error) error {
	return failPersistedTaskAfterSubmitAccountingFailure(
		task,
		attemptedQuota,
		settleErr,
		persistErr,
		model.TaskPublicSettlementFailReason,
		"settlement review update failed",
	)
}

func failPersistedTaskAfterSubmitAccountingError(task *model.Task, relayInfo *relaycommon.RelayInfo, attemptedQuota int, accountingErr error) error {
	if task == nil {
		return nil
	}
	if task.ID <= 0 {
		return fmt.Errorf("mark task submit accounting review failed, taskId=%s, id=%d", task.TaskID, task.ID)
	}
	task.FailReason = model.TaskPublicAccountingFailReason
	task.Quota = 0
	task.RefundPending = task.PrivateData.PreConsumedUsageRecorded
	task.PrivateData.SettlementAttemptQuota = attemptedQuota
	if accountingErr != nil {
		task.PrivateData.SettlementError = appendTaskSettlementError(
			task.PrivateData.SettlementError,
			sanitizeTaskAccountingError(accountingErr),
		)
	}
	if !task.RefundPending {
		task.SettlementStatus = model.TaskSettlementStatusReview
	} else if task.SettlementStatus == model.TaskSettlementStatusReview {
		task.SettlementStatus = ""
	}
	task.Status = model.TaskStatusFailure
	task.Progress = "100%"
	if task.FinishTime == 0 {
		task.FinishTime = common.GetTimestamp()
	}
	if err := updateTaskAfterSubmitAccountingFailureFunc(task); err != nil {
		if forceErr := model.ForceTaskRefundableAfterSubmitAccountingFailure(task); forceErr != nil {
			return err
		}
	}
	return nil
}

func failPersistedTaskAfterSubmitAccountingFailure(task *model.Task, attemptedQuota int, primaryErr error, secondaryErr error, primaryReason string, secondaryReason string) error {
	if task == nil {
		return nil
	}
	if task.ID <= 0 {
		return fmt.Errorf("fail task after submit settlement error failed, taskId=%s, id=%d", task.TaskID, task.ID)
	}
	publicReason := strings.TrimSpace(primaryReason)
	if publicReason == "" {
		publicReason = model.TaskPublicSettlementFailReason
	}
	task.Quota = 0
	task.RefundPending = false
	task.FailReason = publicReason
	task.PrivateData.SettlementAttemptQuota = attemptedQuota
	if primaryErr != nil {
		task.PrivateData.SettlementError = appendTaskSettlementError(
			task.PrivateData.SettlementError,
			sanitizeTaskAccountingError(primaryErr),
		)
	}
	if secondaryErr != nil {
		if secondaryReason == "" {
			secondaryReason = "secondary update failed"
		}
		task.PrivateData.SettlementError = appendTaskSettlementError(
			task.PrivateData.SettlementError,
			sanitizeTaskAccountingError(fmt.Errorf("%s: %w", secondaryReason, secondaryErr)),
		)
	}
	task.SettlementStatus = model.TaskSettlementStatusReview
	task.Status = model.TaskStatusFailure
	task.Progress = "100%"
	task.FinishTime = common.GetTimestamp()
	return model.UpdateTaskAfterSubmitAccountingFailure(task)
}

func shouldRetryTaskRelay(c *gin.Context, channelId int, taskErr *dto.TaskError, retryTimes int) bool {
	if taskErr == nil || retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if taskErr.LocalError {
		return false
	}
	if taskErr.StatusCode/100 == 2 {
		return false
	}
	if taskErr.StatusCode < 100 || taskErr.StatusCode > 599 {
		return true
	}
	return operation_setting.ShouldRetryByStatusCode(taskErr.StatusCode)
}
