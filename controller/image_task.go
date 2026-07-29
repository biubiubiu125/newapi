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
	"sort"
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
	imageTaskResultExpiredMessage    = "image task result expired"
	imageTaskResultUnreadableMessage = "image task result is temporarily unavailable"
	imageTaskStoredResultMarker      = "_newapi_result_file"
	maxImageTaskClientTaskIDLength   = 191
	imageTaskSyncBridgeTimeout       = 10 * time.Minute
	imageTaskSyncBridgePollEvery     = 500 * time.Millisecond
	imageTaskIdempotencyWait         = 5 * time.Second
	imageTaskIdempotencyPollEvery    = 25 * time.Millisecond
)

// imageTaskResultAvailability 区分「结果确实没了」和「结果暂时读不到」。
//
// 这两种情况对外语义完全不同：前者是 410 Gone（永久，客户端应放弃），后者是 503
// （暂时，客户端应重试）。把它们混成同一个 message 会导致共享缓存被运行时禁用
// （见 service.EnsureImageTaskSharedCacheReady）或挂载抖动时，客户端对一条已经生成
// 成功的图收到永久性放弃信号，并且因此永远不会调用 ACK。
type imageTaskResultAvailability int

const (
	// imageTaskResultReady 表示结果可用，或任务本就没有结果内容（由调用方判空）。
	imageTaskResultReady imageTaskResultAvailability = iota
	// imageTaskResultGone 表示结果已被清理或已过期，不会再出现。
	imageTaskResultGone
	// imageTaskResultUnreadable 表示结果记录仍在保留期内，但本次读取失败。
	imageTaskResultUnreadable
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
	persistedRequest, err := acquireImageTaskPersistedRequest(c, relayMode, imageRequest.Model)
	if err != nil {
		return nil, newImageTaskRequestStorageError(err)
	}
	clientTaskIDProvided := persistedRequest.ClientTaskID != ""
	strictIdempotency := isPublicImageTaskCreateRequest(c)
	clientTaskIDReservationHeld := false
	clientTaskIDReservationCommitted := false
	var clientTaskIDReservation *model.ImageTaskClientTaskIDLock
	if clientTaskIDProvided {
		existing, exist, err := model.GetImageTaskByClientTaskID(c.GetInt("id"), persistedRequest.ClientTaskID)
		if err != nil {
			_ = common.RemoveDiskCacheFile(persistedRequest.Path)
			return nil, types.NewError(err, types.ErrorCodeQueryDataError)
		}
		if exist && existing != nil {
			_ = common.RemoveDiskCacheFile(persistedRequest.Path)
			if strictIdempotency {
				if !publicImageTaskAuthorized(c, existing) {
					return nil, imageTaskIdempotencyOwnershipConflictError()
				}
				if imageTaskIdempotencyFingerprintConflicts(existing, persistedRequest.Fingerprint) {
					return nil, imageTaskIdempotencyConflictError()
				}
			} else {
				logImageTaskLooseIdempotencyReuse(c, existing, persistedRequest.Fingerprint)
			}
			return &imageTaskCreateInternalResult{Task: existing, Existing: true}, nil
		}
		if reservation, ok := acquireImageTaskClientTaskIDReservation(c, c.GetInt("id"), persistedRequest.ClientTaskID, persistedRequest.Fingerprint); ok {
			clientTaskIDReservation = reservation
			clientTaskIDReservationHeld = true
		} else {
			reservation, reserved, err := model.ReserveImageTaskClientTaskID(c.GetInt("id"), persistedRequest.ClientTaskID, persistedRequest.Fingerprint)
			if err != nil {
				_ = common.RemoveDiskCacheFile(persistedRequest.Path)
				return nil, types.NewError(err, types.ErrorCodeUpdateDataError)
			}
			if !reserved {
				deadline := time.Now().Add(imageTaskIdempotencyWait)
				for !reserved {
					if strictIdempotency && imageTaskReservationFingerprintConflicts(reservation, persistedRequest.Fingerprint) {
						_ = common.RemoveDiskCacheFile(persistedRequest.Path)
						return nil, imageTaskIdempotencyConflictError()
					}
					existing, exist, released, waitErr := waitForImageTaskIdempotencyReservation(c, persistedRequest.ClientTaskID, deadline)
					if waitErr != nil {
						_ = common.RemoveDiskCacheFile(persistedRequest.Path)
						return nil, types.NewError(waitErr, types.ErrorCodeQueryDataError)
					}
					if exist && existing != nil {
						_ = common.RemoveDiskCacheFile(persistedRequest.Path)
						if strictIdempotency {
							if !publicImageTaskAuthorized(c, existing) {
								return nil, imageTaskIdempotencyOwnershipConflictError()
							}
							if imageTaskIdempotencyFingerprintConflicts(existing, persistedRequest.Fingerprint) {
								return nil, imageTaskIdempotencyConflictError()
							}
						} else {
							logImageTaskLooseIdempotencyReuse(c, existing, persistedRequest.Fingerprint)
						}
						return &imageTaskCreateInternalResult{Task: existing, Existing: true}, nil
					}
					if !released || !time.Now().Before(deadline) {
						_ = common.RemoveDiskCacheFile(persistedRequest.Path)
						return nil, imageTaskIdempotencyInProgressError()
					}
					reservation, reserved, err = model.ReserveImageTaskClientTaskID(c.GetInt("id"), persistedRequest.ClientTaskID, persistedRequest.Fingerprint)
					if err != nil {
						_ = common.RemoveDiskCacheFile(persistedRequest.Path)
						return nil, types.NewError(err, types.ErrorCodeUpdateDataError)
					}
				}
			}
			clientTaskIDReservation = reservation
			clientTaskIDReservationHeld = true
		}
		defer func() {
			if clientTaskIDReservationHeld && !clientTaskIDReservationCommitted {
				_ = model.ReleaseImageTaskClientTaskIDReservation(clientTaskIDReservation)
			}
		}()
	}
	relayInfo.BillingRequestInput = imageTaskBillingRequestInputFromPersistedRequest(persistedRequest, relayInfo.RequestHeaders, imageRequest)
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

	requestBodyBase64, err := imageTaskRequestBodyBase64ForStorage(persistedRequest)
	if err != nil {
		_ = common.RemoveDiskCacheFile(persistedRequest.Path)
		return nil, newImageTaskRequestStorageError(err)
	}
	requestBodyPortable := requestBodyBase64 != ""
	// 纯 API 节点（本机不执行）且没有受信共享缓存时，请求体必须可被其他节点读取。
	// 便携 base64 失败又没有共享路径时，创建会变成永远没人执行的僵尸任务。
	if imageTaskCreateBodyNotExecutable(requestBodyPortable) {
		_ = common.RemoveDiskCacheFile(persistedRequest.Path)
		return nil, types.NewErrorWithStatusCode(
			errors.New("image task request body is not portable and this node cannot execute image tasks"),
			types.ErrorCodeDoRequestFailed,
			http.StatusServiceUnavailable,
			types.ErrOptionWithSkipRetry(),
		)
	}
	// 心跳证据可用且集群内没有声明可执行图片任务的节点时，拒绝创建，
	// 避免 202 后任务永远排队。心跳不可用时不猜，保持创建开放。
	if executor := service.GetImageTaskClusterExecutorAvailability(); executor.Known && !executor.Has {
		_ = common.RemoveDiskCacheFile(persistedRequest.Path)
		return nil, types.NewErrorWithStatusCode(
			errors.New("no live image task executor is available in the cluster"),
			types.ErrorCodeDoRequestFailed,
			http.StatusServiceUnavailable,
			types.ErrOptionWithSkipRetry(),
		)
	}
	settlementBillingRequestInput, billingRequestInputCaptured := cloneImageTaskBillingRequestInputForStorage(
		relayInfo.BillingRequestInput,
		relayInfo.TieredBillingSnapshot,
	)
	persistedRequest.Body = nil
	if relayInfo.BillingRequestInput != nil {
		relayInfo.BillingRequestInput.Body = nil
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
	task.StorageNode = imageTaskStorageNodeForRequest(requestBodyPortable)
	task.PrivateData.PublicImageTask = strictIdempotency
	task.PrivateData.TokenId = relayInfo.TokenId
	task.PublicImageTask = strictIdempotency
	task.PublicImageTaskTokenID = relayInfo.TokenId
	task.PrivateData.NodeName = common.NodeName
	task.PrivateData.BillingContext = taskBillingContextFromRelayInfo(relayInfo)
	task.PrivateData.ImageTaskMode = imageTaskMode
	task.PrivateData.RequestPath = imageTaskRequestPath(relayMode)
	task.PrivateData.RequestMethod = http.MethodPost
	task.PrivateData.RequestContentType = persistedRequest.ContentType
	task.PrivateData.RequestHeaders = cloneImageTaskRequestHeaders(relayInfo.RequestHeaders)
	task.PrivateData.RequestBodyPath = persistedRequest.Path
	task.PrivateData.RequestBodyBase64 = requestBodyBase64
	task.PrivateData.RequestBodyPortable = requestBodyPortable
	task.PrivateData.RequestBodyShared = persistedRequest.Path != "" && service.ImageTaskFileCacheSharedTrusted()
	task.PrivateData.RequestBodySize = persistedRequest.Size
	task.PrivateData.RequestFingerprint = persistedRequest.Fingerprint
	task.PrivateData.TieredBillingSnapshot = cloneImageTaskTieredSnapshot(relayInfo.TieredBillingSnapshot)
	task.PrivateData.BillingRequestInput = settlementBillingRequestInput
	task.PrivateData.BillingRequestInputCaptured = billingRequestInputCaptured

	if newAPIError := service.CommitImageTaskCreation(
		c,
		task,
		relayInfo,
		priceData.QuotaToPreConsume,
		!priceData.FreeModel,
		clientTaskIDReservation,
	); newAPIError != nil {
		_ = common.RemoveDiskCacheFile(persistedRequest.Path)
		return nil, newAPIError
	}
	if clientTaskIDProvided && clientTaskIDReservationHeld {
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
	if !ok || channelOtherSettings.GetImageTaskMode() != dto.ImageTaskModeAsyncTaskBridge {
		return false, nil
	}
	relayInfo.InitChannelMeta(c)
	return true, relayImageTaskSyncBridge(c, imageRequest, relayInfo)
}

func relayImageTaskSyncBridge(c *gin.Context, imageRequest *dto.ImageRequest, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	if !service.ImageTaskExecutionAvailable() {
		return types.NewErrorWithStatusCode(errors.New("image task system is disabled"), types.ErrorCodeDoRequestFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
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
			responseBody, _, resultErr := imageTaskResponseResult(current)
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
		responseBody, _, resultErr := imageTaskResponseResult(current)
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
	now := time.Now().Unix()
	if !imageTaskSyncBridgeCanFailBeforeExecutionAt(current, now) {
		setImageTaskSyncBridgeRetryHeaders(c, current)
		logger.LogWarn(c, fmt.Sprintf("image task %s sync bridge wait stopped after execution started: %s", current.TaskID, reason))
		return nil, nil
	}
	clearImageTaskSyncBridgeHeaders(c)
	fromStatus := current.Status
	resultPath := strings.TrimSpace(current.PrivateData.ResultBodyPath)
	current.Status = model.TaskStatusFailure
	current.Progress = "100%"
	current.FailReason = reason
	current.FinishTime = now
	current.NextPollAt = 0
	current.LockOwner = ""
	current.LockUntil = 0
	current.RetryCount = 0
	current.SettlementStatus = ""
	service.ScheduleImageTaskRequestFileCleanup(current, current.FinishTime)
	current.PrivateData.ResultBodyPath = ""
	current.ImageTaskResultStored = false
	current.ImageTaskResultStoredAt = 0
	current.PrivateData.ResultBodySize = 0
	current.PrivateData.ResultBodySHA256 = ""
	current.PrivateData.ResultContentType = ""
	current.PrivateData.ResultStoredAt = 0
	current.PrivateData.ResultExpiresAt = 0
	current.PrivateData.UpstreamSubmitUncertainAt = 0
	current.PrivateData.UpstreamSubmitUncertainCount = 0
	current.PrivateData.SettlementUsage = nil
	current.PrivateData.SettlementExtraContent = nil
	current.PrivateData.BillingRequestInput = nil
	current.PrivateData.BillingRequestInputCaptured = false
	current.PrivateData.SettlementEvidenceCapturedAt = 0
	current.RefundPending = current.Quota != 0
	current.ClearImageTaskExecutionSecrets()
	won, err := updateImageTaskSyncBridgeCancelledBeforeExecution(current, fromStatus, now)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeUpdateDataError)
	}
	if !won {
		return nil, nil
	}
	if current.Quota != 0 {
		if err := service.RefundTaskQuota(c.Request.Context(), current, reason); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("refund task quota failed task %s: %s", current.TaskID, err.Error()))
		}
	}
	if cleanupErr := service.CleanupDueImageTaskRequestFile(c.Request.Context(), current); cleanupErr != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("image task %s request file cleanup failed: %s", current.TaskID, cleanupErr.Error()))
	}
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
	return imageTaskSyncBridgeCanFailBeforeExecutionAt(task, time.Now().Unix())
}

func imageTaskSyncBridgeCanFailBeforeExecutionAt(task *model.Task, now int64) bool {
	return model.ImageTaskCanCancelBeforeExecution(task, now)
}

func updateImageTaskSyncBridgeCancelledBeforeExecution(task *model.Task, fromStatus model.TaskStatus, now int64) (bool, error) {
	// Authoritative cancel CAS lives in the model layer: it re-reads under a row
	// lock and re-checks lease + upstream submission markers so the WHERE clause
	// cannot race past imageTaskSyncBridgeCanFailBeforeExecutionAt.
	return model.ApplyImageTaskCancelBeforeExecution(task, fromStatus, now)
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
	if mode != dto.ImageTaskModeAsyncTaskBridge || imageRequest == nil || imageRequest.N == nil {
		return nil
	}
	if *imageRequest.N > 1 {
		return errors.New("异步任务桥接模式暂不支持 n 大于 1，请拆分为多个图片任务")
	}
	return nil
}

func imageTaskResponseResult(task *model.Task) (json.RawMessage, imageTaskResultAvailability, string) {
	if task == nil {
		return nil, imageTaskResultReady, ""
	}
	if strings.TrimSpace(task.PrivateData.ResultBodyPath) == "" {
		if len(task.Data) == 0 {
			return nil, imageTaskResultReady, ""
		}
		if imageTaskDataIsStoredResultPlaceholder(task.Data) {
			return nil, imageTaskResultGone, imageTaskResultExpiredMessage
		}
		return append(json.RawMessage(nil), task.Data...), imageTaskResultReady, ""
	}
	file, availability, resultErr := openValidatedImageTaskResultFile(task)
	if file == nil {
		return nil, availability, resultErr
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, imageTaskResultUnreadable, imageTaskResultUnreadableMessage
	}
	return json.RawMessage(data), imageTaskResultReady, ""
}

// openValidatedImageTaskResultFile opens a stored result file, validates size and
// SHA-256 by streaming (no full-file allocation beyond the hash buffer), then seeks
// back to the start so callers can stream or read the body.
func openValidatedImageTaskResultFile(task *model.Task) (*os.File, imageTaskResultAvailability, string) {
	if task == nil {
		return nil, imageTaskResultReady, ""
	}
	path := strings.TrimSpace(task.PrivateData.ResultBodyPath)
	if path == "" {
		return nil, imageTaskResultReady, ""
	}
	if task.PrivateData.ResultExpiresAt > 0 && time.Now().Unix() > task.PrivateData.ResultExpiresAt {
		return nil, imageTaskResultGone, imageTaskResultExpiredMessage
	}
	file, err := os.Open(path)
	if err != nil {
		// 只有已经登记过清理的任务才能断定结果永久消失。其余读失败（共享缓存被运行时
		// 禁用、挂载抖动、IO 错误）都必须当作暂时性的，否则会把可恢复故障报成过期。
		if task.ResultCleanedAt > 0 {
			return nil, imageTaskResultGone, imageTaskResultExpiredMessage
		}
		return nil, imageTaskResultUnreadable, imageTaskResultUnreadableMessage
	}
	hasher := sha256.New()
	written, err := io.Copy(hasher, file)
	if err != nil {
		_ = file.Close()
		return nil, imageTaskResultUnreadable, imageTaskResultUnreadableMessage
	}
	if task.PrivateData.ResultBodySize > 0 && written != task.PrivateData.ResultBodySize {
		_ = file.Close()
		return nil, imageTaskResultUnreadable, imageTaskResultUnreadableMessage
	}
	if task.PrivateData.ResultBodySHA256 != "" {
		if !strings.EqualFold(hex.EncodeToString(hasher.Sum(nil)), task.PrivateData.ResultBodySHA256) {
			_ = file.Close()
			return nil, imageTaskResultUnreadable, imageTaskResultUnreadableMessage
		}
	} else {
		// 历史数据没有摘要时，退回完整 JSON 校验（仍只发生一次，并 seek 回文件头）。
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			_ = file.Close()
			return nil, imageTaskResultUnreadable, imageTaskResultUnreadableMessage
		}
		data, readErr := io.ReadAll(file)
		if readErr != nil || !common.JsonValid(data) {
			_ = file.Close()
			return nil, imageTaskResultUnreadable, imageTaskResultUnreadableMessage
		}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, imageTaskResultUnreadable, imageTaskResultUnreadableMessage
	}
	return file, imageTaskResultReady, ""
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
	Path            string
	ContentType     string
	Size            int64
	Body            []byte
	MultipartValues url.Values
	ClientTaskID    string
	Fingerprint     string
}

const imageTaskPersistedRequestHandoffKey = "image_task_persisted_request_handoff"

type imageTaskPersistedRequestHandoff struct {
	request *imageTaskPersistedRequest
	claimed bool
}

const imageTaskClientTaskIDReservationHandoffKey = "image_task_client_task_id_reservation_handoff"

type imageTaskClientTaskIDReservationHandoff struct {
	reservation *model.ImageTaskClientTaskIDLock
	claimed     bool
}

func stageImageTaskPersistedRequest(c *gin.Context, request *imageTaskPersistedRequest) *imageTaskPersistedRequestHandoff {
	handoff := &imageTaskPersistedRequestHandoff{request: request}
	if c != nil {
		c.Set(imageTaskPersistedRequestHandoffKey, handoff)
	}
	return handoff
}

func acquireImageTaskPersistedRequest(c *gin.Context, relayMode int, modelName string) (*imageTaskPersistedRequest, error) {
	if c != nil {
		if value, exists := c.Get(imageTaskPersistedRequestHandoffKey); exists {
			if handoff, ok := value.(*imageTaskPersistedRequestHandoff); ok && handoff != nil && handoff.request != nil && !handoff.claimed {
				handoff.claimed = true
				return handoff.request, nil
			}
		}
	}
	return persistImageTaskRequest(c, relayMode, modelName)
}

func stageImageTaskClientTaskIDReservation(c *gin.Context, reservation *model.ImageTaskClientTaskIDLock) *imageTaskClientTaskIDReservationHandoff {
	handoff := &imageTaskClientTaskIDReservationHandoff{reservation: reservation}
	if c != nil {
		c.Set(imageTaskClientTaskIDReservationHandoffKey, handoff)
	}
	return handoff
}

func acquireImageTaskClientTaskIDReservation(c *gin.Context, userID int, clientTaskID string, fingerprint string) (*model.ImageTaskClientTaskIDLock, bool) {
	if c == nil {
		return nil, false
	}
	value, exists := c.Get(imageTaskClientTaskIDReservationHandoffKey)
	if !exists {
		return nil, false
	}
	handoff, ok := value.(*imageTaskClientTaskIDReservationHandoff)
	if !ok || handoff == nil || handoff.reservation == nil || handoff.claimed {
		return nil, false
	}
	if !imageTaskClientTaskIDReservationMatches(handoff.reservation, userID, clientTaskID, fingerprint) {
		return nil, false
	}
	handoff.claimed = true
	return handoff.reservation, true
}

func imageTaskClientTaskIDReservationMatches(reservation *model.ImageTaskClientTaskIDLock, userID int, clientTaskID string, fingerprint string) bool {
	if reservation == nil || reservation.ID <= 0 || reservation.UserID != userID {
		return false
	}
	return strings.TrimSpace(reservation.ClientTaskID) == strings.TrimSpace(clientTaskID) &&
		strings.TrimSpace(reservation.Fingerprint) == strings.TrimSpace(fingerprint)
}

func persistImageTaskRequest(c *gin.Context, relayMode int, modelName string) (*imageTaskPersistedRequest, error) {
	if c != nil && c.Request != nil {
		if err := service.ValidateImageTaskRequestBodyBase64Size(c.Request.ContentLength); err != nil {
			return nil, err
		}
	}
	contentType := c.Request.Header.Get("Content-Type")
	if imageTaskMultipartContentTypeHasBoundary(contentType) {
		return persistImageTaskMultipartRequest(c, relayMode, modelName)
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
	clientTaskID, err = imageTaskIdempotencyIdentifier(c, clientTaskID)
	if err != nil {
		return nil, err
	}
	if isPublicImageTaskCreateRequest(c) {
		body, err = imageTaskBodyWithoutClientTaskID(contentType, body)
		if err != nil {
			return nil, err
		}
	}
	if err := service.ValidateImageTaskRequestBodyBase64Size(int64(len(body))); err != nil {
		return nil, err
	}
	fingerprint, err := imageTaskRequestFingerprint(relayMode, contentType, body)
	if err != nil {
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
			Fingerprint:  fingerprint,
		}, nil
	}
	return &imageTaskPersistedRequest{
		Path:         path,
		ContentType:  contentType,
		Size:         int64(len(body)),
		Body:         body,
		ClientTaskID: clientTaskID,
		Fingerprint:  fingerprint,
	}, nil
}

func imageTaskRequestFingerprint(relayMode int, contentType string, body []byte) (string, error) {
	normalized := body
	mediaType, _, _ := mime.ParseMediaType(contentType)
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "", "application/json":
		var value any
		if err := common.DecodeJsonUseNumber(bytes.NewReader(body), &value); err != nil {
			return "", err
		}
		if object, ok := value.(map[string]any); ok {
			delete(object, "client_task_id")
		}
		var err error
		normalized, err = common.Marshal(value)
		if err != nil {
			return "", err
		}
	case "application/x-www-form-urlencoded":
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return "", err
		}
		values.Del("client_task_id")
		normalized = []byte(values.Encode())
	}
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "newapi-image-task-v1\n%d\n%s\n", relayMode, strings.ToLower(strings.TrimSpace(mediaType)))
	_, _ = hash.Write(normalized)
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func imageTaskBodyWithoutClientTaskID(contentType string, body []byte) ([]byte, error) {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "", "application/json":
		bodyMap := make(map[string]json.RawMessage)
		if err := common.Unmarshal(body, &bodyMap); err != nil {
			return nil, err
		}
		delete(bodyMap, "client_task_id")
		return common.Marshal(bodyMap)
	case "application/x-www-form-urlencoded":
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, err
		}
		values.Del("client_task_id")
		return []byte(values.Encode()), nil
	default:
		return body, nil
	}
}

func imageTaskIdempotencyFingerprintConflicts(existing *model.Task, fingerprint string) bool {
	if existing == nil || strings.TrimSpace(existing.PrivateData.RequestFingerprint) == "" || strings.TrimSpace(fingerprint) == "" {
		return false
	}
	return existing.PrivateData.RequestFingerprint != fingerprint
}

// logImageTaskLooseIdempotencyReuse 记录内部同步桥复用旧任务但请求内容已变化的情况。
// 同步接口为兼容存量客户端不做指纹门禁（只有 /v1/image-tasks/* 返回 409），
// 因此这里只告警，不改变行为。
func logImageTaskLooseIdempotencyReuse(c *gin.Context, existing *model.Task, fingerprint string) {
	if !imageTaskIdempotencyFingerprintConflicts(existing, fingerprint) {
		return
	}
	logger.LogWarn(c, fmt.Sprintf(
		"image task %s reused for client_task_id %s with a different request fingerprint; "+
			"use /v1/image-tasks/* for strict idempotency conflict detection",
		existing.TaskID, existing.ClientTaskID))
}

func imageTaskIdempotencyConflictError() *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		errors.New("idempotency key was already used with a different image request"),
		types.ErrorCodeIdempotencyConflict,
		http.StatusConflict,
		types.ErrOptionWithSkipRetry(),
	)
}

func imageTaskIdempotencyOwnershipConflictError() *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		errors.New("idempotency key is already in use"),
		types.ErrorCodeIdempotencyConflict,
		http.StatusConflict,
		types.ErrOptionWithSkipRetry(),
	)
}

func imageTaskIdempotencyInProgressError() *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		errors.New("client_task_id is already being created, please retry later"),
		types.ErrorCodeIdempotencyInProgress,
		http.StatusConflict,
		types.ErrOptionWithSkipRetry(),
	)
}

func imageTaskReservationFingerprintConflicts(lock *model.ImageTaskClientTaskIDLock, fingerprint string) bool {
	if lock == nil || strings.TrimSpace(lock.Fingerprint) == "" || strings.TrimSpace(fingerprint) == "" {
		return false
	}
	return lock.Fingerprint != fingerprint
}

func waitForImageTaskIdempotencyReservation(c *gin.Context, clientTaskID string, deadline time.Time) (*model.Task, bool, bool, error) {
	userID := c.GetInt("id")
	for {
		existing, exists, err := model.GetImageTaskByClientTaskID(userID, clientTaskID)
		if err != nil || exists {
			return existing, exists, false, err
		}
		_, lockExists, err := model.GetImageTaskClientTaskIDLock(userID, clientTaskID)
		if err != nil {
			return nil, false, false, err
		}
		if !lockExists {
			return nil, false, true, nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, false, false, nil
		}
		wait := imageTaskIdempotencyPollEvery
		if remaining < wait {
			wait = remaining
		}
		timer := time.NewTimer(wait)
		select {
		case <-c.Request.Context().Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, false, false, c.Request.Context().Err()
		case <-timer.C:
		}
	}
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

func reconcileImageTaskIdempotencyIdentifiers(c *gin.Context, clientTaskID string) (string, error) {
	headerTaskID, err := imageTaskClientTaskIDFromIdempotencyKey(c)
	if err != nil {
		return "", err
	}
	if clientTaskID != "" && headerTaskID != "" && clientTaskID != headerTaskID {
		return "", fmt.Errorf("client_task_id and Idempotency-Key must match")
	}
	if clientTaskID != "" {
		return clientTaskID, nil
	}
	return headerTaskID, nil
}

func imageTaskIdempotencyIdentifier(c *gin.Context, clientTaskID string) (string, error) {
	if isPublicImageTaskCreateRequest(c) {
		return reconcileImageTaskIdempotencyIdentifiers(c, clientTaskID)
	}
	if clientTaskID != "" {
		return clientTaskID, nil
	}
	return imageTaskClientTaskIDFromIdempotencyKey(c)
}

func isPublicImageTaskCreateRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	switch c.Request.URL.Path {
	case "/v1/image-tasks/generations", "/v1/image-tasks/edits":
		return true
	default:
		return false
	}
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

func persistImageTaskMultipartRequest(c *gin.Context, relayMode int, modelName string) (*imageTaskPersistedRequest, error) {
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
	clientTaskID, err := imageTaskIdempotencyIdentifier(c, clientTaskID)
	if err != nil {
		return nil, err
	}
	fingerprint, err := imageTaskMultipartRequestFingerprint(form, relayMode, modelName)
	if err != nil {
		return nil, err
	}

	stripClientTaskID := isPublicImageTaskCreateRequest(c)
	path, file, reservation, err := common.CreateImageTaskBodyCacheFileWithReservation(imageTaskCacheReservationBytes(c.Request.ContentLength))
	if err != nil {
		if service.ImageTaskRequestBodyBase64FallbackEnabled() {
			return persistImageTaskMultipartRequestInMemory(form, modelName, clientTaskID, fingerprint, stripClientTaskID)
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
	if err := writeImageTaskMultipartRequest(writer, form, modelName, stripClientTaskID); err != nil {
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
		Path:            path,
		ContentType:     writer.FormDataContentType(),
		Size:            stat.Size(),
		Body:            body,
		MultipartValues: imageTaskNormalizedMultipartValues(form, modelName, stripClientTaskID),
		ClientTaskID:    clientTaskID,
		Fingerprint:     fingerprint,
	}, nil
}

func persistImageTaskMultipartRequestInMemory(form *multipart.Form, modelName string, clientTaskID string, fingerprint string, stripClientTaskID bool) (*imageTaskPersistedRequest, error) {
	if form == nil {
		return nil, errors.New("multipart form is missing")
	}
	buffer := &imageTaskLimitedBuffer{limit: service.ImageTaskRequestBodyBase64MaxBytes()}
	writer := multipart.NewWriter(buffer)
	if err := writeImageTaskMultipartRequest(writer, form, modelName, stripClientTaskID); err != nil {
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
		ContentType:     writer.FormDataContentType(),
		Size:            int64(len(body)),
		Body:            body,
		MultipartValues: imageTaskNormalizedMultipartValues(form, modelName, stripClientTaskID),
		ClientTaskID:    clientTaskID,
		Fingerprint:     fingerprint,
	}, nil
}

func imageTaskMultipartRequestFingerprint(form *multipart.Form, relayMode int, modelName string) (string, error) {
	if form == nil {
		return "", errors.New("multipart form is missing")
	}
	values := imageTaskNormalizedMultipartValues(form, modelName, true)

	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "newapi-image-task-v1\n%d\nmultipart/form-data\n", relayMode)
	_, _ = io.WriteString(hash, values.Encode())
	fieldNames := make([]string, 0, len(form.File))
	for fieldName := range form.File {
		fieldNames = append(fieldNames, fieldName)
	}
	sort.Strings(fieldNames)
	for _, fieldName := range fieldNames {
		for _, fileHeader := range form.File[fieldName] {
			if err := writeImageTaskFingerprintPart(hash, fieldName); err != nil {
				return "", err
			}
			if err := writeImageTaskFingerprintPart(hash, fileHeader.Filename); err != nil {
				return "", err
			}
			if err := writeImageTaskFingerprintPart(hash, fileHeader.Header.Get("Content-Type")); err != nil {
				return "", err
			}
			file, err := fileHeader.Open()
			if err != nil {
				return "", err
			}
			fileHash := sha256.New()
			size, copyErr := io.Copy(fileHash, file)
			closeErr := file.Close()
			if copyErr != nil {
				return "", copyErr
			}
			if closeErr != nil {
				return "", closeErr
			}
			if err := writeImageTaskFingerprintPart(hash, fmt.Sprintf("%d", size)); err != nil {
				return "", err
			}
			if err := writeImageTaskFingerprintPart(hash, hex.EncodeToString(fileHash.Sum(nil))); err != nil {
				return "", err
			}
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writeImageTaskFingerprintPart(writer io.Writer, value string) error {
	if _, err := fmt.Fprintf(writer, "\n%d:", len(value)); err != nil {
		return err
	}
	_, err := io.WriteString(writer, value)
	return err
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

func writeImageTaskMultipartRequest(writer *multipart.Writer, form *multipart.Form, modelName string, stripClientTaskID bool) error {
	for key, values := range imageTaskNormalizedMultipartValues(form, modelName, stripClientTaskID) {
		for _, value := range values {
			if err := writer.WriteField(key, value); err != nil {
				return err
			}
		}
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

func imageTaskNormalizedMultipartValues(form *multipart.Form, modelName string, stripClientTaskID bool) url.Values {
	values := make(url.Values)
	if form != nil {
		values = make(url.Values, len(form.Value)+2)
		for key, items := range form.Value {
			if key == "stream" || (stripClientTaskID && key == "client_task_id") || (key == "model" && modelName != "") {
				continue
			}
			values[key] = append([]string(nil), items...)
		}
	}
	if modelName != "" {
		values.Set("model", modelName)
	}
	values.Set("stream", "false")
	return values
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

// imageTaskCreateBodyNotExecutable is true when this node cannot run the task and
// other nodes would also be unable to read the request body.
func imageTaskCreateBodyNotExecutable(requestBodyPortable bool) bool {
	return !service.ImageTaskLocalExecutionAvailable() &&
		!requestBodyPortable &&
		!service.ImageTaskFileCacheSharedTrusted()
}

func imageTaskBillingRequestInputFromPersistedRequest(persisted *imageTaskPersistedRequest, headers map[string]string, requests ...dto.Request) *billingexpr.RequestInput {
	if persisted == nil {
		return nil
	}
	if !imageTaskIsJSONContentType(persisted.ContentType) && len(requests) > 0 && requests[0] != nil {
		input, err := helper.BuildBillingExprRequestInputFromRequest(requests[0], headers)
		if err == nil && len(persisted.MultipartValues) > 0 {
			var body map[string]any
			if err = common.Unmarshal(input.Body, &body); err == nil {
				for key, values := range persisted.MultipartValues {
					if key == "client_task_id" {
						continue
					}
					if key == "stream" {
						body[key] = false
						continue
					}
					if _, exists := body[key]; !exists {
						body[key] = imageTaskMultipartBillingValue(values)
					}
				}
				input.Body, err = common.Marshal(body)
			}
		}
		if err == nil {
			return &input
		}
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

func imageTaskMultipartBillingValue(values []string) any {
	switch len(values) {
	case 0:
		return ""
	case 1:
		return values[0]
	default:
		return append([]string(nil), values...)
	}
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
	return common.IsSensitiveRequestHeader(name)
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
		GroupRatioCaptured:   true,
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
		OtherRatios:          cloneImageTaskFloatMap(priceData.OtherRatios()),
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

func cloneImageTaskBillingRequestInputForStorage(src *billingexpr.RequestInput, snapshot *billingexpr.BillingSnapshot) (*billingexpr.RequestInput, bool) {
	if src == nil {
		return nil, snapshot == nil || snapshot.BillingMode != "tiered_expr"
	}
	dst := &billingexpr.RequestInput{
		Headers: cloneImageTaskRequestHeaders(src.Headers),
	}
	captured := snapshot == nil || snapshot.BillingMode != "tiered_expr"
	if snapshot != nil && snapshot.BillingMode == "tiered_expr" && len(src.Body) > 0 {
		if params, err := billingexpr.CaptureRequestParams(snapshot.ExprString, src.Body); err == nil {
			dst.Params = params
			captured = true
		}
	}
	if len(dst.Headers) == 0 && len(dst.Params) == 0 {
		return nil, captured
	}
	return dst, captured
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
