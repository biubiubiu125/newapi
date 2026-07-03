package controller

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const (
	imageTaskResultExpiredMessage  = "image task result expired"
	imageTaskStoredResultMarker    = "_newapi_result_file"
	maxImageTaskClientTaskIDLength = 191
	imageTaskSyncBridgeTimeout     = 10 * time.Minute
	imageTaskSyncBridgePollEvery   = 500 * time.Millisecond
)

type imageTaskCreateInternalResult struct {
	Task     *model.Task
	Existing bool
}

func createImageTaskInternal(c *gin.Context, imageRequest *dto.ImageRequest, relayInfo *relaycommon.RelayInfo) (*imageTaskCreateInternalResult, *types.NewAPIError) {
	relayMode := imageTaskRelayMode(c)
	if relayInfo != nil {
		switch relayInfo.RelayMode {
		case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
			relayMode = relayInfo.RelayMode
		}
	}
	c.Set("relay_mode", relayMode)

	if imageRequest == nil {
		var err error
		imageRequest, err = helper.GetAndValidOpenAIImageRequest(c, relayMode)
		if err != nil {
			return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
	}
	imageRequest.Stream = false

	if relayInfo == nil {
		var err error
		relayInfo, err = relaycommon.GenRelayInfo(c, types.RelayFormatOpenAIImage, imageRequest, nil)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeGenRelayInfoFailed)
		}
	}
	relayInfo.ForcePreConsume = true
	relayInfo.InitChannelMeta(c)
	imageTaskMode := relayInfo.ChannelOtherSettings.GetImageTaskMode()
	if err := validateImageTaskModeRequest(imageRequest, imageTaskMode); err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	service.EnsureImageTaskSharedCacheReady(c)
	persistedRequest, err := persistImageTaskRequest(c, relayMode, imageRequest.Model)
	if err != nil {
		statusCode := http.StatusBadRequest
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			statusCode = http.StatusRequestEntityTooLarge
		}
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, statusCode, types.ErrOptionWithSkipRetry())
	}
	clientTaskIDProvided := persistedRequest.ClientTaskID != ""
	clientTaskIDReservationHeld := false
	clientTaskIDReservationCommitted := false
	if clientTaskIDProvided {
		existing, exist, err := model.GetImageTaskByClientTaskID(c.GetInt("id"), persistedRequest.ClientTaskID)
		if err != nil {
			_ = common.RemoveDiskCacheFile(persistedRequest.Path)
			return nil, types.NewError(err, types.ErrorCodeQueryDataError)
		}
		if exist && existing != nil {
			_ = common.RemoveDiskCacheFile(persistedRequest.Path)
			return &imageTaskCreateInternalResult{Task: existing, Existing: true}, nil
		}
		_, reserved, err := model.ReserveImageTaskClientTaskID(c.GetInt("id"), persistedRequest.ClientTaskID)
		if err != nil {
			_ = common.RemoveDiskCacheFile(persistedRequest.Path)
			return nil, types.NewError(err, types.ErrorCodeUpdateDataError)
		}
		if !reserved {
			existing, exist, err = model.GetImageTaskByClientTaskID(c.GetInt("id"), persistedRequest.ClientTaskID)
			if err != nil {
				_ = common.RemoveDiskCacheFile(persistedRequest.Path)
				return nil, types.NewError(err, types.ErrorCodeQueryDataError)
			}
			if exist && existing != nil {
				_ = common.RemoveDiskCacheFile(persistedRequest.Path)
				return &imageTaskCreateInternalResult{Task: existing, Existing: true}, nil
			}
			_ = common.RemoveDiskCacheFile(persistedRequest.Path)
			return nil, types.NewErrorWithStatusCode(
				errors.New("client_task_id is already being created, please retry later"),
				types.ErrorCodeInvalidRequest,
				http.StatusConflict,
				types.ErrOptionWithSkipRetry(),
			)
		}
		clientTaskIDReservationHeld = true
		defer func() {
			if clientTaskIDReservationHeld && !clientTaskIDReservationCommitted {
				_ = model.ReleaseImageTaskClientTaskIDLock(c.GetInt("id"), persistedRequest.ClientTaskID)
			}
		}()
	}
	relayInfo.BillingRequestInput = imageTaskBillingRequestInputFromPersistedRequest(persistedRequest, relayInfo.RequestHeaders)
	mappedImageRequest := *imageRequest
	if err := helper.ModelMappedHelper(c, relayInfo, &mappedImageRequest); err != nil {
		_ = common.RemoveDiskCacheFile(persistedRequest.Path)
		return nil, types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}

	meta := imageRequest.GetTokenCountMeta()
	if setting.ShouldCheckPromptSensitive() && meta != nil {
		contains, words := service.CheckSensitiveText(meta.CombineText)
		if contains {
			_ = common.RemoveDiskCacheFile(persistedRequest.Path)
			logger.LogWarn(c, fmt.Sprintf("user sensitive words detected: %s", strings.Join(words, ", ")))
			return nil, types.NewError(errors.New("sensitive words detected"), types.ErrorCodeSensitiveWordsDetected, types.ErrOptionWithStatusCode(http.StatusBadRequest))
		}
	}

	tokens, err := service.EstimateRequestToken(c, meta, relayInfo)
	if err != nil {
		_ = common.RemoveDiskCacheFile(persistedRequest.Path)
		return nil, types.NewError(err, types.ErrorCodeCountTokenFailed)
	}
	relayInfo.SetEstimatePromptTokens(tokens)

	priceData, err := helper.ModelPriceHelper(c, relayInfo, tokens, meta)
	if err != nil {
		_ = common.RemoveDiskCacheFile(persistedRequest.Path)
		return nil, types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
	}
	if priceData.UsePrice {
		imageN := uint(1)
		if imageRequest.N != nil && *imageRequest.N > 0 {
			imageN = *imageRequest.N
		}
		priceData.AddOtherRatio("n", float64(imageN))
		priceData.QuotaToPreConsume = int(float64(priceData.QuotaToPreConsume) * float64(imageN))
		relayInfo.PriceData = priceData
	}

	requestBodyBase64, err := imageTaskRequestBodyBase64ForStorage(persistedRequest)
	if err != nil {
		_ = common.RemoveDiskCacheFile(persistedRequest.Path)
		statusCode := http.StatusBadRequest
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			statusCode = http.StatusRequestEntityTooLarge
		}
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, statusCode, types.ErrOptionWithSkipRetry())
	}
	requestBodyPortable := requestBodyBase64 != ""
	persistedRequest.Body = nil
	if relayInfo.BillingRequestInput != nil {
		relayInfo.BillingRequestInput.Body = nil
	}

	if !priceData.FreeModel {
		if newAPIError := service.PreConsumeBilling(c, priceData.QuotaToPreConsume, relayInfo); newAPIError != nil {
			_ = common.RemoveDiskCacheFile(persistedRequest.Path)
			return nil, newAPIError
		}
	}

	task := model.InitTask(constant.TaskPlatformImage, relayInfo)
	task.ClientTaskID = persistedRequest.ClientTaskID
	if task.ClientTaskID == "" {
		task.ClientTaskID = task.TaskID
	}
	task.Status = model.TaskStatusQueued
	task.Progress = "0%"
	task.NextPollAt = time.Now().Unix()
	task.Action = imageTaskAction(relayMode)
	task.Quota = relayInfo.FinalPreConsumedQuota
	task.StorageNode = imageTaskStorageNodeForRequest(requestBodyPortable)
	task.PrivateData.BillingSource = relayInfo.BillingSource
	task.PrivateData.SubscriptionId = relayInfo.SubscriptionId
	task.PrivateData.TokenId = relayInfo.TokenId
	task.PrivateData.BillingContext = taskBillingContextFromRelayInfo(relayInfo)
	task.PrivateData.ImageTaskMode = imageTaskMode
	task.PrivateData.RequestPath = imageTaskRequestPath(relayMode)
	task.PrivateData.RequestMethod = http.MethodPost
	task.PrivateData.RequestContentType = persistedRequest.ContentType
	task.PrivateData.RequestHeaders = cloneImageTaskRequestHeaders(relayInfo.RequestHeaders)
	task.PrivateData.RequestBodyPath = persistedRequest.Path
	task.PrivateData.RequestBodyBase64 = requestBodyBase64
	task.PrivateData.RequestBodyPortable = requestBodyPortable
	task.PrivateData.RequestBodySize = persistedRequest.Size
	task.PrivateData.TieredBillingSnapshot = cloneImageTaskTieredSnapshot(relayInfo.TieredBillingSnapshot)
	task.PrivateData.BillingRequestInput = cloneImageTaskBillingRequestInputForStorage(relayInfo.BillingRequestInput)

	if err := task.Insert(); err != nil {
		if relayInfo.Billing != nil {
			relayInfo.Billing.Refund(c)
		}
		_ = common.RemoveDiskCacheFile(persistedRequest.Path)
		return nil, types.NewError(err, types.ErrorCodeUpdateDataError)
	}
	if clientTaskIDProvided {
		existing, exist, err := model.GetImageTaskByClientTaskID(c.GetInt("id"), persistedRequest.ClientTaskID)
		if err == nil && exist && existing != nil && existing.ID != task.ID {
			if deleteErr := model.DeleteTaskByID(task.ID); deleteErr != nil {
				logger.LogWarn(c, fmt.Sprintf("delete duplicate image task %s failed: %s", task.TaskID, deleteErr.Error()))
			} else {
				if relayInfo.Billing != nil {
					relayInfo.Billing.Refund(c)
				}
				_ = common.RemoveDiskCacheFile(persistedRequest.Path)
				return &imageTaskCreateInternalResult{Task: existing, Existing: true}, nil
			}
		}
	}
	if clientTaskIDProvided && clientTaskIDReservationHeld {
		if err := model.BindImageTaskClientTaskIDLock(c.GetInt("id"), persistedRequest.ClientTaskID, task); err != nil {
			logger.LogWarn(c, fmt.Sprintf("bind image task client_task_id %s failed: %s", task.TaskID, err.Error()))
		}
		clientTaskIDReservationCommitted = true
	}

	service.NotifyImageTaskQueued(c)

	return &imageTaskCreateInternalResult{Task: task}, nil
}

func tryRelayImageTaskSyncBridge(c *gin.Context, request dto.Request, relayInfo *relaycommon.RelayInfo) (bool, *types.NewAPIError) {
	imageRequest, ok := request.(*dto.ImageRequest)
	if !ok || relayInfo == nil {
		return false, nil
	}
	switch relayInfo.RelayMode {
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
	default:
		return false, nil
	}
	channelOtherSettings, ok := common.GetContextKeyType[dto.ChannelOtherSettings](c, constant.ContextKeyChannelOtherSetting)
	if !ok || channelOtherSettings.GetImageTaskMode() != dto.ImageTaskModeGPTImage2APIAsync {
		return false, nil
	}
	relayInfo.InitChannelMeta(c)
	return true, relayImageTaskSyncBridge(c, imageRequest, relayInfo)
}

func relayImageTaskSyncBridge(c *gin.Context, imageRequest *dto.ImageRequest, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	if !service.ImageTaskExecutionAvailable() {
		return types.NewErrorWithStatusCode(errors.New("image task execution is disabled"), types.ErrorCodeDoRequestFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
	}
	result, newAPIError := createImageTaskInternal(c, imageRequest, relayInfo)
	if newAPIError != nil {
		return newAPIError
	}
	if result == nil || result.Task == nil {
		return types.NewError(errors.New("image task create result is empty"), types.ErrorCodeUpdateDataError)
	}
	setImageTaskSyncBridgeTaskHeaders(c, result.Task)
	if result.Existing {
		service.NotifyImageTaskQueued(c)
	}
	responseBody, newAPIError := waitImageTaskSyncBridgeResult(c, result.Task)
	if newAPIError != nil {
		return newAPIError
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", responseBody)
	return nil
}

func waitImageTaskSyncBridgeResult(c *gin.Context, task *model.Task) (json.RawMessage, *types.NewAPIError) {
	if task == nil {
		return nil, types.NewError(errors.New("image task is nil"), types.ErrorCodeQueryDataError)
	}
	timer := time.NewTimer(imageTaskSyncBridgeTimeout)
	defer timer.Stop()
	ticker := time.NewTicker(imageTaskSyncBridgePollEvery)
	defer ticker.Stop()

	for {
		current, exist, err := model.GetByTaskId(task.UserId, task.TaskID)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeQueryDataError)
		}
		if !exist || current == nil || current.Platform != constant.TaskPlatformImage {
			return nil, types.NewErrorWithStatusCode(errors.New("image task not found"), types.ErrorCodeQueryDataError, http.StatusInternalServerError)
		}
		if current.Status == model.TaskStatusFailure {
			return nil, imageTaskSyncBridgeFailureError(current)
		}
		if current.Status == model.TaskStatusSuccess && current.SettlementStatus == model.TaskSettlementStatusReview {
			reason := strings.TrimSpace(current.FailReason)
			if reason == "" {
				reason = "image task failed"
			}
			return nil, types.NewErrorWithStatusCode(errors.New(reason), types.ErrorCodeDoRequestFailed, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
		}
		if imageTaskResponseResultVisible(current) {
			responseBody, resultErr := imageTaskResponseResult(current)
			if resultErr != "" {
				return nil, types.NewErrorWithStatusCode(errors.New(resultErr), types.ErrorCodeBadResponseBody, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
			}
			if len(responseBody) == 0 {
				return nil, types.NewErrorWithStatusCode(errors.New("empty image task result"), types.ErrorCodeEmptyResponse, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
			}
			return responseBody, nil
		}

		select {
		case <-ticker.C:
		case <-timer.C:
			if responseBody, cancelErr := cancelImageTaskSyncBridgeWait(c, task, "image generation timed out"); len(responseBody) > 0 || cancelErr != nil {
				return responseBody, cancelErr
			}
			return nil, imageTaskSyncBridgeWaitStoppedError(c, task, "image generation timed out", http.StatusGatewayTimeout)
		case <-c.Request.Context().Done():
			if responseBody, cancelErr := cancelImageTaskSyncBridgeWait(c, task, "client closed request"); len(responseBody) > 0 || cancelErr != nil {
				return responseBody, cancelErr
			}
			return nil, imageTaskSyncBridgeWaitStoppedError(c, task, "client closed request", 499)
		}
	}
}

func cancelImageTaskSyncBridgeWait(c *gin.Context, task *model.Task, reason string) (json.RawMessage, *types.NewAPIError) {
	if task == nil {
		return nil, nil
	}
	current, exist, err := model.GetByTaskId(task.UserId, task.TaskID)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeQueryDataError)
	}
	if !exist || current == nil || current.Platform != constant.TaskPlatformImage {
		return nil, types.NewErrorWithStatusCode(errors.New("image task not found"), types.ErrorCodeQueryDataError, http.StatusInternalServerError)
	}
	if current.Status == model.TaskStatusFailure ||
		(current.Status == model.TaskStatusSuccess && current.SettlementStatus == model.TaskSettlementStatusReview) {
		return nil, imageTaskSyncBridgeFailureError(current)
	}
	if imageTaskResponseResultVisible(current) {
		responseBody, resultErr := imageTaskResponseResult(current)
		if resultErr != "" {
			return nil, types.NewErrorWithStatusCode(errors.New(resultErr), types.ErrorCodeBadResponseBody, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
		}
		if len(responseBody) > 0 {
			return responseBody, nil
		}
	}
	if current.Status == model.TaskStatusSuccess {
		return nil, nil
	}
	if reason == "" {
		reason = "image task sync bridge cancelled"
	}
	if !imageTaskSyncBridgeCanFailBeforeExecution(current) {
		setImageTaskSyncBridgeRetryHeaders(c, current)
		logger.LogWarn(c, fmt.Sprintf("image task %s sync bridge wait stopped after execution started: %s", current.TaskID, reason))
		return nil, nil
	}
	clearImageTaskSyncBridgeHeaders(c)
	fromStatus := current.Status
	bodyPath := strings.TrimSpace(current.PrivateData.RequestBodyPath)
	resultPath := strings.TrimSpace(current.PrivateData.ResultBodyPath)
	current.Status = model.TaskStatusFailure
	current.Progress = "100%"
	current.FailReason = reason
	current.FinishTime = time.Now().Unix()
	current.NextPollAt = 0
	current.LockOwner = ""
	current.LockUntil = 0
	current.RetryCount = 0
	current.SettlementStatus = ""
	current.PrivateData.RequestBodyPath = ""
	current.PrivateData.RequestBodyBase64 = ""
	current.PrivateData.RequestBodyPortable = false
	current.PrivateData.ResultBodyPath = ""
	current.PrivateData.ResultBodySize = 0
	current.PrivateData.ResultBodySHA256 = ""
	current.PrivateData.ResultContentType = ""
	current.PrivateData.ResultStoredAt = 0
	current.PrivateData.ResultExpiresAt = 0
	current.PrivateData.UpstreamSubmitUncertainAt = 0
	current.PrivateData.UpstreamSubmitUncertainCount = 0
	current.PrivateData.SettlementUsage = nil
	current.PrivateData.SettlementExtraContent = nil
	won, err := updateImageTaskSyncBridgeCancelledBeforeExecution(current, fromStatus)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeUpdateDataError)
	}
	if !won {
		return nil, nil
	}
	if current.Quota != 0 {
		service.RefundTaskQuota(c, current, reason)
	}
	_ = common.RemoveDiskCacheFile(bodyPath)
	_ = common.RemoveDiskCacheFile(resultPath)
	return nil, imageTaskSyncBridgeFailureError(current)
}

func imageTaskSyncBridgeWaitStoppedError(c *gin.Context, task *model.Task, reason string, statusCode int) *types.NewAPIError {
	setImageTaskSyncBridgeRetryHeaders(c, task)
	retryID := imageTaskSyncBridgeRetryID(task)
	if retryID != "" {
		reason = fmt.Sprintf("%s; image task is still running, retry with Idempotency-Key: %s", reason, retryID)
	}
	return types.NewErrorWithStatusCode(errors.New(reason), types.ErrorCodeDoRequestFailed, statusCode, types.ErrOptionWithSkipRetry())
}

func setImageTaskSyncBridgeRetryHeaders(c *gin.Context, task *model.Task) {
	setImageTaskSyncBridgeTaskHeaders(c, task)
	if c == nil || task == nil {
		return
	}
	if retryID := imageTaskSyncBridgeRetryID(task); retryID != "" {
		c.Header("X-NewAPI-Retry-Idempotency-Key", retryID)
	}
}

func setImageTaskSyncBridgeTaskHeaders(c *gin.Context, task *model.Task) {
	if c == nil || task == nil {
		return
	}
	if task.TaskID != "" {
		c.Header("X-NewAPI-Image-Task-ID", task.TaskID)
	}
	if retryID := imageTaskSyncBridgeRetryID(task); retryID != "" {
		c.Header("X-NewAPI-Image-Client-Task-ID", retryID)
	}
}

func clearImageTaskSyncBridgeHeaders(c *gin.Context) {
	if c == nil || c.Writer == nil {
		return
	}
	headers := c.Writer.Header()
	headers.Del("X-NewAPI-Image-Task-ID")
	headers.Del("X-NewAPI-Image-Client-Task-ID")
	headers.Del("X-NewAPI-Retry-Idempotency-Key")
}

func imageTaskSyncBridgeRetryID(task *model.Task) string {
	if task == nil {
		return ""
	}
	retryID := strings.TrimSpace(task.ClientTaskID)
	if retryID == "" {
		retryID = strings.TrimSpace(task.TaskID)
	}
	return retryID
}

func imageTaskSyncBridgeCanFailBeforeExecution(task *model.Task) bool {
	if task == nil {
		return false
	}
	if strings.TrimSpace(task.LockOwner) != "" ||
		strings.TrimSpace(task.PrivateData.UpstreamTaskID) != "" ||
		task.PrivateData.UpstreamSubmitUncertainAt > 0 ||
		task.PrivateData.UpstreamSubmitUncertainCount > 0 {
		return false
	}
	switch task.Status {
	case model.TaskStatusNotStart, model.TaskStatusQueued:
		return true
	default:
		return false
	}
}

func updateImageTaskSyncBridgeCancelledBeforeExecution(task *model.Task, fromStatus model.TaskStatus) (bool, error) {
	if task == nil {
		return false, nil
	}
	result := model.DB.Model(task).
		Where("status = ?", fromStatus).
		Where("(lock_owner = '' OR lock_owner IS NULL)").
		Select("*").
		Updates(task)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func imageTaskSyncBridgeFailureError(task *model.Task) *types.NewAPIError {
	reason := "image task failed"
	if task != nil && strings.TrimSpace(task.FailReason) != "" {
		reason = strings.TrimSpace(task.FailReason)
	}
	statusCode := http.StatusBadGateway
	if reason == "image generation timed out" {
		statusCode = http.StatusGatewayTimeout
	} else if reason == "client closed request" {
		statusCode = 499
	}
	return types.NewErrorWithStatusCode(errors.New(reason), types.ErrorCodeDoRequestFailed, statusCode, types.ErrOptionWithSkipRetry())
}

func validateImageTaskModeRequest(imageRequest *dto.ImageRequest, mode string) error {
	if mode != dto.ImageTaskModeGPTImage2APIAsync || imageRequest == nil || imageRequest.N == nil {
		return nil
	}
	if *imageRequest.N > 1 {
		return errors.New("gpt_image2api 异步模式暂不支持 n 大于 1，请拆分为多个图片任务")
	}
	return nil
}

func imageTaskResponseResult(task *model.Task) (json.RawMessage, string) {
	if task == nil {
		return nil, ""
	}
	if task.PrivateData.ResultBodyPath == "" {
		if len(task.Data) == 0 {
			return nil, ""
		}
		if imageTaskDataIsStoredResultPlaceholder(task.Data) {
			return nil, imageTaskResultExpiredMessage
		}
		return append(json.RawMessage(nil), task.Data...), ""
	}
	if task.PrivateData.ResultExpiresAt > 0 && time.Now().Unix() > task.PrivateData.ResultExpiresAt {
		return nil, imageTaskResultExpiredMessage
	}
	data, err := os.ReadFile(task.PrivateData.ResultBodyPath)
	if err != nil {
		return nil, imageTaskResultExpiredMessage
	}
	if task.PrivateData.ResultBodySize > 0 && int64(len(data)) != task.PrivateData.ResultBodySize {
		return nil, imageTaskResultExpiredMessage
	}
	if task.PrivateData.ResultBodySHA256 != "" {
		sum := sha256.Sum256(data)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), task.PrivateData.ResultBodySHA256) {
			return nil, imageTaskResultExpiredMessage
		}
	}
	if !json.Valid(data) {
		return nil, imageTaskResultExpiredMessage
	}
	return json.RawMessage(data), ""
}

func imageTaskDataIsStoredResultPlaceholder(data json.RawMessage) bool {
	if len(data) == 0 {
		return false
	}
	var placeholder struct {
		Stored bool `json:"_newapi_result_file"`
	}
	if err := common.Unmarshal(data, &placeholder); err != nil {
		return false
	}
	return placeholder.Stored
}

func imageTaskResponseResultVisible(task *model.Task) bool {
	if task == nil {
		return false
	}
	return task.Status == model.TaskStatusSuccess &&
		task.SettlementStatus != model.TaskSettlementStatusReview &&
		!imageTaskSettlementOpen(task.SettlementStatus)
}

func imageTaskSettlementOpen(settlementStatus string) bool {
	switch settlementStatus {
	case model.TaskSettlementStatusPending, model.TaskSettlementStatusApplied:
		return true
	default:
		return false
	}
}

func imageTaskRelayMode(c *gin.Context) int {
	if mode := c.GetInt("relay_mode"); mode != relayconstant.RelayModeUnknown {
		return mode
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/images/edits") {
		return relayconstant.RelayModeImagesEdits
	}
	return relayconstant.RelayModeImagesGenerations
}

func imageTaskAction(relayMode int) string {
	if relayMode == relayconstant.RelayModeImagesEdits {
		return constant.TaskActionImageEdit
	}
	return constant.TaskActionImageGeneration
}

func imageTaskRequestPath(relayMode int) string {
	if relayMode == relayconstant.RelayModeImagesEdits {
		return "/v1/images/edits"
	}
	return "/v1/images/generations"
}

type imageTaskPersistedRequest struct {
	Path         string
	ContentType  string
	Size         int64
	Body         []byte
	ClientTaskID string
}

func persistImageTaskRequest(c *gin.Context, relayMode int, modelName string) (*imageTaskPersistedRequest, error) {
	if c != nil && c.Request != nil {
		if err := service.ValidateImageTaskRequestBodyBase64Size(c.Request.ContentLength); err != nil {
			return nil, err
		}
	}
	contentType := c.Request.Header.Get("Content-Type")
	if imageTaskMultipartContentTypeHasBoundary(contentType) {
		return persistImageTaskMultipartRequest(c, modelName)
	}

	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	if err := service.ValidateImageTaskRequestBodyBase64Size(storage.Size()); err != nil {
		return nil, err
	}
	var body []byte
	clientTaskID := ""
	if strings.HasPrefix(contentType, "application/json") || contentType == "" {
		if _, err := storage.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		body, err = imageTaskJSONBodyWithoutStreamFromReader(storage, modelName)
		if err != nil {
			return nil, err
		}
		clientTaskID, err = imageTaskClientTaskIDFromJSONBody(body)
		if err != nil {
			return nil, err
		}
		contentType = "application/json"
	} else if strings.Contains(contentType, "application/x-www-form-urlencoded") {
		body, err = readImageTaskStorageBytes(storage)
		if err != nil {
			return nil, err
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, err
		}
		if modelName != "" {
			values.Set("model", modelName)
		}
		clientTaskID, err = normalizeImageTaskClientTaskID(values.Get("client_task_id"))
		if err != nil {
			return nil, err
		}
		values.Set("stream", "false")
		body = []byte(values.Encode())
		contentType = "application/x-www-form-urlencoded"
	} else if relayMode == relayconstant.RelayModeImagesGenerations {
		if _, err := storage.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		body, err = imageTaskJSONBodyWithoutStreamFromReader(storage, modelName)
		if err != nil {
			return nil, err
		}
		clientTaskID, err = imageTaskClientTaskIDFromJSONBody(body)
		if err != nil {
			return nil, err
		}
		contentType = "application/json"
	} else {
		body, err = readImageTaskStorageBytes(storage)
		if err != nil {
			return nil, err
		}
	}
	if clientTaskID == "" {
		clientTaskID, err = imageTaskClientTaskIDFromIdempotencyKey(c)
		if err != nil {
			return nil, err
		}
	}
	if err := service.ValidateImageTaskRequestBodyBase64Size(int64(len(body))); err != nil {
		return nil, err
	}
	path, err := common.WriteImageTaskBodyCacheFile(body)
	if err != nil {
		if !service.ImageTaskRequestBodyBase64FallbackEnabled() || len(body) == 0 {
			return nil, err
		}
		return &imageTaskPersistedRequest{
			ContentType:  contentType,
			Size:         int64(len(body)),
			Body:         body,
			ClientTaskID: clientTaskID,
		}, nil
	}
	return &imageTaskPersistedRequest{
		Path:         path,
		ContentType:  contentType,
		Size:         int64(len(body)),
		Body:         body,
		ClientTaskID: clientTaskID,
	}, nil
}

func imageTaskJSONBodyWithoutStream(body []byte, modelName string) ([]byte, error) {
	return imageTaskJSONBodyWithoutStreamFromReader(bytes.NewReader(body), modelName)
}

func imageTaskJSONBodyWithoutStreamFromReader(reader io.Reader, modelName string) ([]byte, error) {
	bodyMap := make(map[string]json.RawMessage)
	if err := common.DecodeJson(reader, &bodyMap); err != nil {
		return nil, err
	}
	if modelName != "" {
		modelJSON, err := common.Marshal(modelName)
		if err != nil {
			return nil, err
		}
		bodyMap["model"] = json.RawMessage(modelJSON)
	}
	bodyMap["stream"] = json.RawMessage("false")
	return common.Marshal(bodyMap)
}

func imageTaskClientTaskIDFromJSONBody(body []byte) (string, error) {
	if len(body) == 0 {
		return "", nil
	}
	bodyMap := make(map[string]json.RawMessage)
	if err := common.Unmarshal(body, &bodyMap); err != nil {
		return "", err
	}
	raw, ok := bodyMap["client_task_id"]
	if !ok || len(raw) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return "", nil
	}
	var value string
	if err := common.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("client_task_id must be a string")
	}
	return normalizeImageTaskClientTaskID(value)
}

func imageTaskClientTaskIDFromIdempotencyKey(c *gin.Context) (string, error) {
	if c == nil || c.Request == nil {
		return "", nil
	}
	return normalizeImageTaskClientTaskID(c.GetHeader("Idempotency-Key"))
}

func normalizeImageTaskClientTaskID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if len(value) > maxImageTaskClientTaskIDLength {
		return "", fmt.Errorf("client_task_id is too long")
	}
	if strings.ContainsAny(value, ",\r\n\t ") {
		return "", fmt.Errorf("client_task_id contains invalid characters")
	}
	return value, nil
}

func readImageTaskStorageBytes(storage common.BodyStorage) ([]byte, error) {
	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(storage, storage.Size()+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > storage.Size() {
		return nil, common.ErrRequestBodyTooLarge
	}
	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return body, nil
}

func imageTaskCacheReservationBytes(contentLength int64) int64 {
	maxMB := constant.MaxRequestBodyMB
	if maxMB <= 0 {
		maxMB = 128
	}
	maxBytes := int64(maxMB) << 20
	if contentLength <= 0 {
		return maxBytes
	}
	reserveBytes := contentLength + (1 << 20)
	if reserveBytes <= 0 || reserveBytes > maxBytes {
		return maxBytes
	}
	return reserveBytes
}

func persistImageTaskMultipartRequest(c *gin.Context, modelName string) (*imageTaskPersistedRequest, error) {
	form := c.Request.MultipartForm
	if form == nil {
		parsed, err := common.ParseMultipartFormReusable(c)
		if err != nil {
			return nil, err
		}
		form = parsed
	}
	if form != nil {
		defer form.RemoveAll()
	}
	clientTaskID := ""
	if form != nil {
		var err error
		clientTaskID, err = normalizeImageTaskClientTaskID(firstImageTaskFormValue(form.Value, "client_task_id"))
		if err != nil {
			return nil, err
		}
	}
	if clientTaskID == "" {
		var err error
		clientTaskID, err = imageTaskClientTaskIDFromIdempotencyKey(c)
		if err != nil {
			return nil, err
		}
	}

	path, file, reservation, err := common.CreateImageTaskBodyCacheFileWithReservation(imageTaskCacheReservationBytes(c.Request.ContentLength))
	if err != nil {
		if service.ImageTaskRequestBodyBase64FallbackEnabled() {
			return persistImageTaskMultipartRequestInMemory(form, modelName, clientTaskID)
		}
		return nil, err
	}
	cleanup := true
	committed := false
	defer func() {
		_ = file.Close()
		if cleanup {
			_ = os.Remove(path)
		}
		if !committed {
			reservation.Release()
		}
	}()

	writer := multipart.NewWriter(file)
	if err := writeImageTaskMultipartRequest(writer, form, modelName); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	var body []byte
	if service.ImageTaskRequestBodyBase64FallbackEnabled() {
		if err := service.ValidateImageTaskRequestBodyBase64Size(stat.Size()); err != nil {
			return nil, err
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		body, err = io.ReadAll(file)
		if err != nil {
			return nil, err
		}
	}
	if err := reservation.Commit(stat.Size()); err != nil {
		return nil, err
	}
	committed = true
	cleanup = false
	return &imageTaskPersistedRequest{
		Path:         path,
		ContentType:  writer.FormDataContentType(),
		Size:         stat.Size(),
		Body:         body,
		ClientTaskID: clientTaskID,
	}, nil
}

func persistImageTaskMultipartRequestInMemory(form *multipart.Form, modelName string, clientTaskID string) (*imageTaskPersistedRequest, error) {
	if form == nil {
		return nil, errors.New("multipart form is missing")
	}
	buffer := &imageTaskLimitedBuffer{limit: service.ImageTaskRequestBodyBase64MaxBytes()}
	writer := multipart.NewWriter(buffer)
	if err := writeImageTaskMultipartRequest(writer, form, modelName); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	body := append([]byte(nil), buffer.Bytes()...)
	if err := service.ValidateImageTaskRequestBodyBase64Size(int64(len(body))); err != nil {
		return nil, err
	}
	return &imageTaskPersistedRequest{
		ContentType:  writer.FormDataContentType(),
		Size:         int64(len(body)),
		Body:         body,
		ClientTaskID: clientTaskID,
	}, nil
}

type imageTaskLimitedBuffer struct {
	bytes.Buffer
	limit int64
}

func (b *imageTaskLimitedBuffer) Write(p []byte) (int, error) {
	if b != nil && b.limit > 0 && int64(b.Len()+len(p)) > b.limit {
		return 0, common.ErrRequestBodyTooLarge
	}
	return b.Buffer.Write(p)
}

func writeImageTaskMultipartRequest(writer *multipart.Writer, form *multipart.Form, modelName string) error {
	for key, values := range form.Value {
		if key == "stream" {
			continue
		}
		if key == "model" && modelName != "" {
			continue
		}
		for _, value := range values {
			if err := writer.WriteField(key, value); err != nil {
				return err
			}
		}
	}
	if modelName != "" {
		if err := writer.WriteField("model", modelName); err != nil {
			return err
		}
	}
	if err := writer.WriteField("stream", "false"); err != nil {
		return err
	}
	for fieldName, fileHeaders := range form.File {
		for _, fileHeader := range fileHeaders {
			if err := copyImageTaskMultipartFile(writer, fieldName, fileHeader); err != nil {
				return err
			}
		}
	}
	return nil
}

func firstImageTaskFormValue(values map[string][]string, key string) string {
	if len(values) == 0 {
		return ""
	}
	items := values[key]
	if len(items) == 0 {
		return ""
	}
	return items[0]
}

func imageTaskRequestBodyBase64ForStorage(persisted *imageTaskPersistedRequest) (string, error) {
	if persisted == nil || !service.ImageTaskRequestBodyBase64FallbackEnabled() || len(persisted.Body) == 0 {
		return "", nil
	}
	if err := service.ValidateImageTaskRequestBodyBase64Size(int64(len(persisted.Body))); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(persisted.Body), nil
}

func imageTaskStorageNodeForRequest(requestBodyPortable bool) string {
	if requestBodyPortable {
		return model.ImageTaskPortableStorageNode
	}
	return common.NodeName
}

func imageTaskBillingRequestInputFromPersistedRequest(persisted *imageTaskPersistedRequest, headers map[string]string) *billingexpr.RequestInput {
	if persisted == nil {
		return nil
	}
	input := &billingexpr.RequestInput{
		Headers: cloneImageTaskRequestHeaders(headers),
	}
	if imageTaskIsJSONContentType(persisted.ContentType) && len(persisted.Body) > 0 {
		input.Body = persisted.Body
	}
	if len(input.Headers) == 0 && len(input.Body) == 0 {
		return nil
	}
	return input
}

func imageTaskIsJSONContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	return contentType == "" || strings.HasPrefix(contentType, "application/json")
}

func copyImageTaskMultipartFile(writer *multipart.Writer, fieldName string, fileHeader *multipart.FileHeader) error {
	file, err := fileHeader.Open()
	if err != nil {
		return err
	}
	defer file.Close()

	partHeader := make(textproto.MIMEHeader)
	partHeader.Set("Content-Disposition", fmt.Sprintf(
		`form-data; name="%s"; filename="%s"`,
		escapeImageTaskMultipartHeader(fieldName),
		escapeImageTaskMultipartHeader(fileHeader.Filename),
	))
	if fileContentType := fileHeader.Header.Get("Content-Type"); fileContentType != "" {
		partHeader.Set("Content-Type", fileContentType)
	}
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		return err
	}
	_, err = io.Copy(part, file)
	return err
}

func escapeImageTaskMultipartHeader(value string) string {
	return strings.NewReplacer("\\", "\\\\", `"`, "\\\"").Replace(value)
}

func cloneImageTaskRequestHeaders(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" || imageTaskSensitiveHeader(key) {
			continue
		}
		dst[key] = value
	}
	if len(dst) == 0 {
		return nil
	}
	return dst
}

func imageTaskSensitiveHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "authorization", "proxy-authorization", "x-api-key", "api-key", "cookie", "set-cookie":
		return true
	default:
		return false
	}
}

func imageTaskMultipartContentTypeHasBoundary(contentType string) bool {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	if !strings.EqualFold(mediaType, "multipart/form-data") {
		return false
	}
	return strings.TrimSpace(params["boundary"]) != ""
}

func cloneImageTaskTieredSnapshot(src *billingexpr.BillingSnapshot) *billingexpr.BillingSnapshot {
	if src == nil {
		return nil
	}
	dst := *src
	return &dst
}

func taskBillingContextFromRelayInfo(relayInfo *relaycommon.RelayInfo) *model.TaskBillingContext {
	if relayInfo == nil {
		return nil
	}
	priceData := relayInfo.PriceData
	return &model.TaskBillingContext{
		ModelPrice:           priceData.ModelPrice,
		GroupRatio:           priceData.GroupRatioInfo.GroupRatio,
		GroupSpecialRatio:    priceData.GroupRatioInfo.GroupSpecialRatio,
		GroupHasSpecialRatio: priceData.GroupRatioInfo.HasSpecialRatio,
		ModelRatio:           priceData.ModelRatio,
		CompletionRatio:      priceData.CompletionRatio,
		CacheRatio:           priceData.CacheRatio,
		CacheCreationRatio:   priceData.CacheCreationRatio,
		CacheCreation5mRatio: priceData.CacheCreation5mRatio,
		CacheCreation1hRatio: priceData.CacheCreation1hRatio,
		ImageRatio:           priceData.ImageRatio,
		AudioRatio:           priceData.AudioRatio,
		AudioCompletionRatio: priceData.AudioCompletionRatio,
		OtherRatios:          cloneImageTaskFloatMap(priceData.OtherRatios),
		OriginModelName:      relayInfo.OriginModelName,
		PerCallBilling:       common.StringsContains(constant.TaskPricePatches, relayInfo.OriginModelName) || priceData.UsePrice,
	}
}

func cloneImageTaskFloatMap(src map[string]float64) map[string]float64 {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]float64, len(src))
	for key, value := range src {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		dst[key] = value
	}
	if len(dst) == 0 {
		return nil
	}
	return dst
}

func cloneImageTaskBillingRequestInputForStorage(src *billingexpr.RequestInput) *billingexpr.RequestInput {
	if src == nil {
		return nil
	}
	dst := &billingexpr.RequestInput{
		Headers: cloneImageTaskRequestHeaders(src.Headers),
	}
	if len(dst.Headers) == 0 {
		return nil
	}
	return dst
}

func respondImageTaskError(c *gin.Context, newAPIError *types.NewAPIError) {
	if newAPIError == nil {
		return
	}
	statusCode := newAPIError.StatusCode
	if statusCode == 0 {
		statusCode = http.StatusInternalServerError
	}
	c.JSON(statusCode, gin.H{"error": newAPIError.ToOpenAIError()})
}
