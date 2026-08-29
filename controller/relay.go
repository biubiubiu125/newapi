package controller

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var (
	relayTaskSubmitFunc    = relay.RelayTaskSubmit
	settleBillingFunc      = service.SettleBilling
	logTaskConsumptionFunc = service.LogTaskConsumption
)

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
			newAPIError = types.NewError(err, types.ErrorCodeInvalidRequest)
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
			newAPIError = types.NewError(err, types.ErrorCodeSensitiveWordsDetected)
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
				processChannelError(c, *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()), channelErr, false)
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

		processChannelError(c, *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()), newAPIError, false)

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
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许跨域
	},
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
		autoBan := c.GetBool("auto_ban")
		autoBanInt := 1
		if !autoBan {
			autoBanInt = 0
		}
		return &model.Channel{
			Id:      c.GetInt("channel_id"),
			Type:    c.GetInt("channel_type"),
			Name:    c.GetString("channel_name"),
			AutoBan: &autoBanInt,
		}, nil
	}
	channel, selectGroup, err := service.CacheGetRandomSatisfiedChannel(retryParam)
	if err != nil {
		return nil, types.NewError(fmt.Errorf("获取分组 %s 下模型 %s 的可用渠道失败（retry）: %s", selectGroup, info.OriginModelName, err.Error()), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if channel == nil {
		return nil, types.NewError(fmt.Errorf("分组 %s 下模型 %s 的可用渠道不存在（retry）", selectGroup, info.OriginModelName), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}

	info.PriceData.GroupRatioInfo = helper.HandleGroupRatio(c, info)

	newAPIError := middleware.SetupContextForSelectedChannel(c, channel, info.OriginModelName)
	if newAPIError != nil {
		return channel, newAPIError
	}
	return channel, nil
}

func shouldRetry(c *gin.Context, openaiErr *types.NewAPIError, retryTimes int) bool {
	if openaiErr == nil {
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
	if types.IsChannelError(openaiErr) {
		return true
	}
	if types.IsSkipRetryError(openaiErr) {
		return false
	}
	if service.IsBalanceInsufficientError(openaiErr) {
		return true
	}
	code := openaiErr.StatusCode
	if code >= 200 && code < 300 {
		return false
	}
	if code < 100 || code > 599 {
		return true
	}
	return true
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
		c.JSON(http.StatusInternalServerError, gin.H{
			"description": fmt.Sprintf("failed to generate relay info: %s", err.Error()),
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
	//err = relayMidjourneySubmit(c, relayMode)
	log.Println(mjErr)
	if mjErr != nil {
		statusCode := http.StatusBadRequest
		if mjErr.Code == 30 {
			mjErr.Result = "当前分组负载已饱和，请稍后再试，或升级账户以提升服务质量。"
			statusCode = http.StatusTooManyRequests
		}
		c.JSON(statusCode, gin.H{
			"description": fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result),
			"type":        "upstream_error",
			"code":        mjErr.Code,
		})
		channelId := c.GetInt("channel_id")
		logger.LogError(c, fmt.Sprintf("relay error (channel #%d, status code %d): %s", channelId, statusCode, fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result)))
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

func RelayTaskFetch(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &dto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}
	if taskErr := relay.RelayTaskFetch(c, relayInfo.RelayMode); taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

func RelayTask(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &dto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}
	relayInfo.InitChannelMeta(c)

	if taskErr := relay.ResolveOriginTask(c, relayInfo); taskErr != nil {
		respondTaskError(c, taskErr)
		return
	}

	var result *relay.TaskSubmitResult
	var taskErr *dto.TaskError
	var lastUpstreamTaskErr *dto.TaskError
	billingLogged := false
	defer func() {
		if taskErr != nil && relayInfo.Billing != nil && !billingLogged {
			if refundErr := relayInfo.Billing.Refund(c); refundErr != nil {
				common.SysError("refund billing after task error failed: " + refundErr.Error())
				service.RecordConsumeAccountingError(c, relayInfo, "refund billing after task error", refundErr)
			}
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:        c,
		TokenGroup: relayInfo.TokenGroup,
		ModelName:  relayInfo.OriginModelName,
		Retry:      common.GetPointer(0),
	}

	for {
		retryParam.ExcludeChannelIds = getFailedChannelIds(c)
		var channel *model.Channel

		if lockedCh, ok := relayInfo.LockedChannel.(*model.Channel); ok && lockedCh != nil && retryParam.GetRetry() == 0 {
			channel = lockedCh
		} else {
			var channelErr *types.NewAPIError
			channel, channelErr = getChannel(c, relayInfo, retryParam)
			if channelErr != nil {
				logger.LogError(c, channelErr.Error())
				if channel != nil {
					addUsedChannel(c, channel.Id)
					addFailedChannel(c, channel.Id)
					retryParam.ExcludeChannelIds = getFailedChannelIds(c)
					processChannelError(c,
						*types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey,
							common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()),
						channelErr, false)
					if shouldRetry(c, channelErr, common.RetryTimes-retryParam.GetRetry()) {
						retryParam.IncreaseRetry()
						continue
					}
				}
				if lastUpstreamTaskErr != nil {
					taskErr = lastUpstreamTaskErr
				} else {
					taskErr = service.TaskErrorWrapperLocal(channelErr.Err, "get_channel_failed", http.StatusInternalServerError)
				}
				break
			}
		}

		addUsedChannel(c, channel.Id)
		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusRequestEntityTooLarge)
			} else {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusBadRequest)
			}
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		result, taskErr = relayTaskSubmitFunc(c, relayInfo)
		if taskErr == nil {
			break
		}

		if !taskErr.LocalError {
			lastUpstreamTaskErr = taskErr
			addFailedChannel(c, channel.Id)
			retryParam.ExcludeChannelIds = getFailedChannelIds(c)
			processChannelError(c,
				*types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey,
					common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()),
				types.NewOpenAIError(taskErr.Error, types.ErrorCodeBadResponseStatusCode, taskErr.StatusCode), false)
		}

		if !shouldRetryTaskRelay(c, channel.Id, taskErr, common.RetryTimes-retryParam.GetRetry()) {
			break
		}
		retryParam.IncreaseRetry()
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}

	// ── 成功：先插入任务，再结算 + 日志 ──
	if taskErr == nil {
		task := model.InitTask(result.Platform, relayInfo)
		task.PrivateData.UpstreamTaskID = result.UpstreamTaskID
		task.PrivateData.BillingSource = relayInfo.BillingSource
		task.PrivateData.SubscriptionId = relayInfo.SubscriptionId
		task.PrivateData.TokenId = relayInfo.TokenId
		task.PrivateData.NodeName = common.NodeName
		task.PrivateData.BillingContext = taskBillingContextFromRelayInfo(relayInfo)
		task.Quota = result.Quota
		task.Data = result.TaskData
		task.Action = relayInfo.Action
		if insertErr := task.Insert(); insertErr != nil {
			common.SysError("insert task error: " + insertErr.Error())
			taskErr = service.TaskErrorWrapperLocal(insertErr, "insert_task_failed", http.StatusInternalServerError)
		} else {
			var settleErr error
			if settleErr = settleBillingFunc(c, relayInfo, result.Quota); settleErr != nil {
				common.SysError("settle task billing error: " + settleErr.Error())
				service.RecordConsumeAccountingError(c, relayInfo, "settle task billing", settleErr)
				c.Set(service.ContextKeySettlementError(), settleErr.Error())
				if updateErr := persistTaskSubmitSettlementError(task, relayInfo, result.Quota, settleErr); updateErr != nil {
					common.SysError("update task settlement error: " + updateErr.Error())
					service.RecordConsumeAccountingError(c, relayInfo, "persist task settlement review", updateErr)
					if failErr := failPersistedTaskAfterSubmitSettlementError(task, relayInfo, result.Quota, settleErr, updateErr); failErr != nil {
						common.SysError("fail persisted task after settlement error: " + failErr.Error())
						service.RecordConsumeAccountingError(c, relayInfo, "fail persisted task after settlement error", failErr)
						if deleteErr := model.DeleteTaskByID(task.ID); deleteErr != nil {
							common.SysError("delete task after settlement failure error: " + deleteErr.Error())
							service.RecordConsumeAccountingError(c, relayInfo, "delete task after settlement failure", deleteErr)
						}
					}
					taskErr = service.TaskErrorWrapperLocal(updateErr, "update_task_settlement_failed", http.StatusInternalServerError)
				}
			} else {
				c.Set(service.ContextKeySettlementApplied(), true)
			}
			if taskErr == nil {
				if err := logTaskConsumptionFunc(c, relayInfo); err != nil {
					common.SysError("log task consumption error: " + err.Error())
					service.RecordConsumeAccountingError(c, relayInfo, "log task consumption", err)
					if updateErr := persistTaskSubmitSettlementError(task, relayInfo, result.Quota, err); updateErr != nil {
						common.SysError("update task accounting error: " + updateErr.Error())
						service.RecordConsumeAccountingError(c, relayInfo, "persist task accounting review", updateErr)
					}
					if failErr := failPersistedTaskAfterSubmitAccountingError(task, relayInfo, result.Quota, err); failErr != nil {
						common.SysError("fail persisted task after accounting error: " + failErr.Error())
						service.RecordConsumeAccountingError(c, relayInfo, "fail persisted task after accounting error", failErr)
						if deleteErr := model.DeleteTaskByID(task.ID); deleteErr != nil {
							common.SysError("delete task after accounting failure error: " + deleteErr.Error())
							service.RecordConsumeAccountingError(c, relayInfo, "delete task after accounting failure", deleteErr)
						}
					}
					taskErr = service.TaskErrorWrapperLocal(err, "log_task_consumption_failed", http.StatusInternalServerError)
				} else {
					billingLogged = true
				}
			}
		}
	}

	if taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

// respondTaskError 统一输出 Task 错误响应（含 429 限流提示改写）
func respondTaskError(c *gin.Context, taskErr *dto.TaskError) {
	if taskErr.StatusCode == http.StatusTooManyRequests {
		taskErr.Message = "当前分组上游负载已饱和，请稍后再试"
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
	return task.UpdateSubmitSettlementError()
}

func failPersistedTaskAfterSubmitSettlementError(task *model.Task, relayInfo *relaycommon.RelayInfo, attemptedQuota int, settleErr error, persistErr error) error {
	return failPersistedTaskAfterSubmitAccountingFailure(
		task,
		attemptedQuota,
		settleErr,
		persistErr,
		"billing settlement failed before consumption log",
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
	failReason := "billing accounting failed after task submission"
	if accountingErr != nil {
		failReason += ": " + sanitizeTaskAccountingError(accountingErr)
	}
	currentFailReason := strings.TrimSpace(task.FailReason)
	if currentFailReason == "" {
		task.FailReason = failReason
	} else if !strings.Contains(currentFailReason, failReason) {
		task.FailReason = currentFailReason + "; " + failReason
	}
	task.Quota = 0
	task.PrivateData.SettlementAttemptQuota = attemptedQuota
	if accountingErr != nil {
		task.PrivateData.SettlementError = appendTaskSettlementError(
			task.PrivateData.SettlementError,
			sanitizeTaskAccountingError(accountingErr),
		)
	}
	task.SettlementStatus = model.TaskSettlementStatusReview
	return task.UpdateSubmitSettlementError()
}

func failPersistedTaskAfterSubmitAccountingFailure(task *model.Task, attemptedQuota int, primaryErr error, secondaryErr error, primaryReason string, secondaryReason string) error {
	if task == nil {
		return nil
	}
	if task.ID <= 0 {
		return fmt.Errorf("fail task after submit settlement error failed, taskId=%s, id=%d", task.TaskID, task.ID)
	}
	failReason := primaryReason
	if primaryErr != nil {
		failReason += ": " + strings.ReplaceAll(primaryErr.Error(), "\n", " ")
	}
	if secondaryErr != nil {
		if secondaryReason == "" {
			secondaryReason = "secondary update failed"
		}
		failReason += "; " + secondaryReason + ": " + strings.ReplaceAll(secondaryErr.Error(), "\n", " ")
	}
	task.Quota = 0
	task.FailReason = failReason
	task.PrivateData.SettlementAttemptQuota = attemptedQuota
	if primaryErr != nil {
		task.PrivateData.SettlementError = appendTaskSettlementError(
			task.PrivateData.SettlementError,
			sanitizeTaskAccountingError(primaryErr),
		)
	}
	task.SettlementStatus = model.TaskSettlementStatusReview
	task.Status = model.TaskStatusFailure
	task.Progress = "100%"
	task.FinishTime = common.GetTimestamp()
	result := model.DB.Model(&model.Task{}).
		Where("id = ?", task.ID).
		Updates(map[string]any{
			"quota":             task.Quota,
			"status":            task.Status,
			"progress":          task.Progress,
			"finish_time":       task.FinishTime,
			"fail_reason":       task.FailReason,
			"private_data":      task.PrivateData,
			"settlement_status": task.SettlementStatus,
			"updated_at":        common.GetTimestamp(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("fail task after submit settlement error failed, taskId=%s, id=%d", task.TaskID, task.ID)
	}
	return nil
}

func shouldRetryTaskRelay(c *gin.Context, channelId int, taskErr *dto.TaskError, retryTimes int) bool {
	if taskErr == nil {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	if taskErr.LocalError {
		return false
	}
	if taskErr.StatusCode/100 == 2 {
		return false
	}
	return true
}
