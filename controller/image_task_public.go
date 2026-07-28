package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const publicImageTaskAckGrace = 2 * time.Minute

func RequirePublicImageTaskContentType(relayMode int) gin.HandlerFunc {
	return func(c *gin.Context) {
		contentType := strings.TrimSpace(c.GetHeader("Content-Type"))
		mediaType, params, err := mime.ParseMediaType(contentType)
		if err != nil {
			publicImageTaskError(c, http.StatusUnsupportedMediaType, "unsupported_media_type", "unsupported image task request content type")
			c.Abort()
			return
		}
		switch relayMode {
		case relayconstant.RelayModeImagesGenerations:
			if mediaType != "application/json" {
				publicImageTaskError(c, http.StatusUnsupportedMediaType, "unsupported_media_type", "image generation tasks require application/json")
				c.Abort()
				return
			}
		case relayconstant.RelayModeImagesEdits:
			if mediaType != "multipart/form-data" {
				publicImageTaskError(c, http.StatusUnsupportedMediaType, "unsupported_media_type", "image edit tasks require multipart/form-data")
				c.Abort()
				return
			}
			if strings.TrimSpace(params["boundary"]) == "" {
				publicImageTaskError(c, http.StatusBadRequest, "invalid_request", "multipart boundary is required")
				c.Abort()
				return
			}
		}
		c.Next()
	}
}

func ReusePublicImageTaskGenerationIfExists(c *gin.Context) {
	ReusePublicImageTaskIfExists(c, relayconstant.RelayModeImagesGenerations)
}

func ReusePublicImageTaskEditIfExists(c *gin.Context) {
	ReusePublicImageTaskIfExists(c, relayconstant.RelayModeImagesEdits)
}

func ReusePublicImageTaskIfExists(c *gin.Context, relayMode int) {
	defer cleanupPublicImageTaskMultipartForm(c)
	clientTaskID, err := publicImageTaskIdempotencyCandidate(c, relayMode)
	if err != nil {
		if isImageTaskRequestStorageError(err) {
			respondPublicImageTaskRequestStorageError(c, err)
			c.Abort()
			return
		}
		publicImageTaskError(c, http.StatusBadRequest, "invalid_request", err.Error())
		c.Abort()
		return
	}
	if clientTaskID == "" {
		c.Next()
		return
	}

	c.Set("relay_mode", relayMode)
	imageRequest, err := helper.GetAndValidOpenAIImageRequest(c, relayMode)
	if err != nil {
		publicImageTaskError(c, http.StatusBadRequest, "invalid_request", err.Error())
		c.Abort()
		return
	}
	persisted, err := persistImageTaskRequest(c, relayMode, imageRequest.Model)
	if err != nil {
		respondPublicImageTaskRequestStorageError(c, err)
		c.Abort()
		return
	}
	handoff := stageImageTaskPersistedRequest(c, persisted)
	defer func() {
		if !handoff.claimed {
			_ = common.RemoveDiskCacheFile(persisted.Path)
		}
	}()

	task, exists, err := model.GetImageTaskByClientTaskID(c.GetInt("id"), persisted.ClientTaskID)
	if err != nil {
		publicImageTaskError(c, http.StatusInternalServerError, "task_query_failed", "failed to query image task")
		c.Abort()
		return
	}
	proceedWithReservation := func(reservation *model.ImageTaskClientTaskIDLock) {
		reservationHandoff := stageImageTaskClientTaskIDReservation(c, reservation)
		defer func() {
			if !reservationHandoff.claimed {
				_ = model.ReleaseImageTaskClientTaskIDReservation(reservationHandoff.reservation)
			}
		}()
		c.Next()
	}
	if !exists {
		reservation, reserved, reserveErr := model.ReserveImageTaskClientTaskID(c.GetInt("id"), persisted.ClientTaskID, persisted.Fingerprint)
		if reserveErr != nil {
			publicImageTaskError(c, http.StatusInternalServerError, "task_reservation_failed", "failed to reserve image task idempotency key")
			c.Abort()
			return
		}
		deadline := time.Now().Add(imageTaskIdempotencyWait)
		for !reserved {
			if imageTaskReservationFingerprintConflicts(reservation, persisted.Fingerprint) {
				publicImageTaskError(c, http.StatusConflict, "idempotency_conflict", "idempotency key was already used with a different image request")
				c.Abort()
				return
			}
			var released bool
			task, exists, released, err = waitForImageTaskIdempotencyReservation(c, persisted.ClientTaskID, deadline)
			if err != nil {
				publicImageTaskError(c, http.StatusInternalServerError, "task_query_failed", "failed to query image task")
				c.Abort()
				return
			}
			if exists {
				break
			}
			if !released || !time.Now().Before(deadline) {
				publicImageTaskError(c, http.StatusConflict, "idempotency_in_progress", "idempotency key is already being processed")
				c.Abort()
				return
			}
			reservation, reserved, reserveErr = model.ReserveImageTaskClientTaskID(c.GetInt("id"), persisted.ClientTaskID, persisted.Fingerprint)
			if reserveErr != nil {
				publicImageTaskError(c, http.StatusInternalServerError, "task_reservation_failed", "failed to reserve image task idempotency key")
				c.Abort()
				return
			}
		}
		if reserved {
			proceedWithReservation(reservation)
			return
		}
	}
	if !exists || task == nil {
		c.Next()
		return
	}
	if !publicImageTaskAuthorized(c, task) {
		publicImageTaskError(c, http.StatusConflict, "idempotency_conflict", "idempotency key is already in use")
		c.Abort()
		return
	}
	if imageTaskIdempotencyFingerprintConflicts(task, persisted.Fingerprint) {
		publicImageTaskError(c, http.StatusConflict, "idempotency_conflict", "idempotency key was already used with a different image request")
		c.Abort()
		return
	}
	c.JSON(http.StatusAccepted, publicImageTaskResponse(task, time.Now().Unix()))
	c.Abort()
}

func publicImageTaskIdempotencyCandidate(c *gin.Context, relayMode int) (string, error) {
	if c == nil || c.Request == nil {
		return "", nil
	}
	contentType := c.GetHeader("Content-Type")
	bodyTaskID := ""
	var err error
	switch {
	case imageTaskMultipartContentTypeHasBoundary(contentType):
		form := c.Request.MultipartForm
		if form == nil {
			form, err = common.ParseMultipartFormReusable(c)
			if err != nil {
				return "", err
			}
			c.Request.MultipartForm = form
		}
		if form != nil {
			bodyTaskID, err = normalizeImageTaskClientTaskID(firstImageTaskFormValue(form.Value, "client_task_id"))
		}
	case strings.Contains(contentType, "application/x-www-form-urlencoded"):
		storage, storageErr := common.GetBodyStorage(c)
		if storageErr != nil {
			return "", storageErr
		}
		body, readErr := readImageTaskStorageBytes(storage)
		if readErr != nil {
			return "", readErr
		}
		values, parseErr := url.ParseQuery(string(body))
		if parseErr != nil {
			return "", parseErr
		}
		bodyTaskID, err = normalizeImageTaskClientTaskID(values.Get("client_task_id"))
	case strings.HasPrefix(contentType, "application/json"), contentType == "", relayMode == relayconstant.RelayModeImagesGenerations:
		storage, storageErr := common.GetBodyStorage(c)
		if storageErr != nil {
			return "", storageErr
		}
		if _, seekErr := storage.Seek(0, io.SeekStart); seekErr != nil {
			return "", seekErr
		}
		body, readErr := io.ReadAll(io.LimitReader(storage, storage.Size()+1))
		if readErr != nil {
			return "", readErr
		}
		if _, seekErr := storage.Seek(0, io.SeekStart); seekErr != nil {
			return "", seekErr
		}
		bodyTaskID, err = imageTaskClientTaskIDFromJSONBody(body)
	}
	if err != nil {
		return "", err
	}
	return reconcileImageTaskIdempotencyIdentifiers(c, bodyTaskID)
}

func cleanupPublicImageTaskMultipartForm(c *gin.Context) {
	if c == nil || c.Request == nil || c.Request.MultipartForm == nil {
		return
	}
	_ = c.Request.MultipartForm.RemoveAll()
	c.Request.MultipartForm = nil
}

func CreatePublicImageTask(c *gin.Context, relayMode int) {
	if !service.ImageTaskExecutionAvailable() {
		publicImageTaskError(c, http.StatusServiceUnavailable, "image_task_unavailable", "image task execution is disabled")
		return
	}
	c.Set("relay_mode", relayMode)
	imageRequest, err := helper.GetAndValidOpenAIImageRequest(c, relayMode)
	if err != nil {
		publicImageTaskError(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatOpenAIImage, imageRequest, nil)
	if err != nil {
		publicImageTaskError(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	relayInfo.RelayMode = relayMode
	result, apiErr := createImageTaskInternal(c, imageRequest, relayInfo)
	if apiErr != nil {
		respondPublicImageTaskAPIError(c, apiErr)
		return
	}
	if result == nil || result.Task == nil {
		publicImageTaskError(c, http.StatusInternalServerError, "image_task_create_failed", "image task create result is empty")
		return
	}
	if result.Existing && !publicImageTaskAuthorized(c, result.Task) {
		publicImageTaskError(c, http.StatusConflict, "idempotency_conflict", "idempotency key is already in use")
		return
	}
	c.JSON(http.StatusAccepted, publicImageTaskResponse(result.Task, time.Now().Unix()))
}

func GetPublicImageTask(c *gin.Context) {
	task, ok := loadAuthorizedPublicImageTaskMetadata(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, publicImageTaskResponse(task, time.Now().Unix()))
}

func ListPublicImageTasks(c *gin.Context) {
	rawIDs := strings.Split(c.Query("ids"), ",")
	ids := make([]any, 0, len(rawIDs))
	order := make([]string, 0, len(rawIDs))
	seen := make(map[string]struct{}, len(rawIDs))
	for _, rawID := range rawIDs {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		if len(order) >= 100 {
			publicImageTaskError(c, http.StatusBadRequest, "too_many_task_ids", "at most 100 task ids are allowed")
			return
		}
		seen[id] = struct{}{}
		order = append(order, id)
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		publicImageTaskError(c, http.StatusBadRequest, "task_ids_required", "ids is required")
		return
	}
	tasks, err := model.GetPublicImageTasksByTaskIDs(c.GetInt("id"), ids)
	if err != nil {
		publicImageTaskError(c, http.StatusInternalServerError, "task_query_failed", "failed to query image tasks")
		return
	}
	byID := make(map[string]*model.Task, len(tasks))
	for _, task := range tasks {
		if publicImageTaskAuthorized(c, task) {
			byID[task.TaskID] = task
		}
	}
	now := time.Now().Unix()
	items := make([]*dto.PublicImageTask, 0, len(byID))
	for _, id := range order {
		if task := byID[id]; task != nil {
			items = append(items, publicImageTaskResponse(task, now))
		}
	}
	c.JSON(http.StatusOK, dto.PublicImageTaskList{Data: items})
}

func GetPublicImageTaskResult(c *gin.Context) {
	task, ok := loadAuthorizedPublicImageTask(c)
	if !ok {
		return
	}
	if task.Status != model.TaskStatusSuccess || task.SettlementStatus != model.TaskSettlementStatusSettled {
		publicImageTaskError(c, http.StatusConflict, "result_not_ready", "image task result is not ready")
		return
	}
	now := time.Now().Unix()
	if !publicImageTaskResultAvailable(task, now) {
		publicImageTaskError(c, http.StatusGone, "result_expired", "image task result is no longer available")
		return
	}
	result, availability, resultErr := imageTaskResponseResult(task)
	if availability == imageTaskResultUnreadable {
		// 结果记录仍在保留期内却读不出来，几乎总是共享缓存或本地磁盘的暂时性故障。
		// 报 410 会让客户端永久放弃一张已经生成成功的图，所以这里必须报可重试的 503。
		logger.LogWarn(c, fmt.Sprintf(
			"image task %s result unreadable on node %q (shared result cache enabled=%t, stored path set=%t): %s",
			task.TaskID,
			common.NodeName,
			service.ImageTaskResultFileCacheSharedEnabled(),
			strings.TrimSpace(task.PrivateData.ResultBodyPath) != "",
			resultErr,
		))
		c.Header("Retry-After", "5")
		publicImageTaskError(c, http.StatusServiceUnavailable, "result_temporarily_unavailable", "image task result is temporarily unavailable, please retry")
		return
	}
	if resultErr != "" || len(result) == 0 {
		publicImageTaskError(c, http.StatusGone, "result_expired", "image task result is no longer available")
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", result)
}

func AcknowledgePublicImageTaskResult(c *gin.Context) {
	task, ok := loadAuthorizedPublicImageTask(c)
	if !ok {
		return
	}
	now := time.Now().Unix()
	if task.Status != model.TaskStatusSuccess || task.SettlementStatus != model.TaskSettlementStatusSettled {
		publicImageTaskError(c, http.StatusConflict, "result_not_ready", "image task result is not ready")
		return
	}
	if task.ResultAcknowledgedAt == 0 && !publicImageTaskResultAvailable(task, now) {
		publicImageTaskError(c, http.StatusGone, "result_expired", "image task result is no longer available")
		return
	}
	acknowledged, err := model.AcknowledgeImageTaskResult(task.ID, now, now+int64(publicImageTaskAckGrace.Seconds()))
	if err != nil {
		publicImageTaskError(c, http.StatusInternalServerError, "ack_failed", "failed to acknowledge image task result")
		return
	}
	reloaded, exists, err := model.GetTaskByID(task.ID)
	if err != nil || !exists {
		publicImageTaskError(c, http.StatusInternalServerError, "task_query_failed", "failed to reload image task")
		return
	}
	if !acknowledged && reloaded.ResultAcknowledgedAt == 0 {
		if !publicImageTaskResultAvailable(reloaded, now) {
			publicImageTaskError(c, http.StatusGone, "result_expired", "image task result is no longer available")
			return
		}
		publicImageTaskError(c, http.StatusConflict, "ack_conflict", "image task result acknowledgement conflicted with another update")
		return
	}
	c.JSON(http.StatusOK, publicImageTaskResponse(reloaded, now))
}

func CancelPublicImageTask(c *gin.Context) {
	task, ok := loadAuthorizedPublicImageTask(c)
	if !ok {
		return
	}
	if task.PrivateData.CancelledAt > 0 {
		cleanupCancelledPublicImageTaskFiles(c, task)
		if !refundCancelledPublicImageTask(c, task) {
			return
		}
		c.JSON(http.StatusOK, publicImageTaskResponse(task, time.Now().Unix()))
		return
	}
	if task.Status == model.TaskStatusFailure || task.Status == model.TaskStatusSuccess {
		publicImageTaskError(c, http.StatusConflict, "not_cancellable", "image task is already finished")
		return
	}
	now := time.Now().Unix()
	if !imageTaskSyncBridgeCanFailBeforeExecutionAt(task, now) {
		publicImageTaskError(c, http.StatusConflict, "not_cancellable", "image task has already started")
		return
	}
	fromStatus := task.Status
	task.Status = model.TaskStatusFailure
	task.Progress = "100%"
	task.FailReason = "image task cancelled by client"
	task.FinishTime = now
	task.NextPollAt = 0
	task.LockOwner = ""
	task.LockUntil = 0
	task.RetryCount = 0
	task.SettlementStatus = ""
	task.PrivateData.CancelledAt = now
	task.ImageTaskCancelledAt = now
	task.PrivateData.CancelledReason = task.FailReason
	service.ScheduleImageTaskRequestFileCleanup(task, now)
	if strings.TrimSpace(task.PrivateData.ResultBodyPath) == "" {
		task.PrivateData.ResultBodySize = 0
		task.PrivateData.ResultBodySHA256 = ""
		task.PrivateData.ResultContentType = ""
		task.PrivateData.ResultStoredAt = 0
		task.PrivateData.ResultExpiresAt = 0
	}
	task.ResultExpiresAt = 0
	task.ResultAcknowledgedAt = 0
	task.ResultDeleteAfter = 0
	task.ResultCleanedAt = 0
	task.ResultCleanupPending = false
	task.Data = nil
	task.PrivateData.SettlementUsage = nil
	task.PrivateData.SettlementExtraContent = nil
	task.PrivateData.BillingRequestInput = nil
	task.PrivateData.BillingRequestInputCaptured = false
	task.PrivateData.SettlementEvidenceCapturedAt = 0
	task.RefundPending = task.Quota != 0
	task.ClearImageTaskExecutionSecrets()
	won, err := updateImageTaskSyncBridgeCancelledBeforeExecution(task, fromStatus, now)
	if err != nil {
		publicImageTaskError(c, http.StatusInternalServerError, "cancel_failed", "failed to cancel image task")
		return
	}
	if !won {
		reloaded, exists, reloadErr := model.GetTaskByID(task.ID)
		if reloadErr != nil {
			publicImageTaskError(c, http.StatusInternalServerError, "task_query_failed", "failed to reload image task")
			return
		}
		if exists && publicImageTaskAuthorized(c, reloaded) && reloaded.PrivateData.CancelledAt > 0 {
			if !refundCancelledPublicImageTask(c, reloaded) {
				return
			}
			c.JSON(http.StatusOK, publicImageTaskResponse(reloaded, time.Now().Unix()))
			return
		}
		publicImageTaskError(c, http.StatusConflict, "not_cancellable", "image task has already started")
		return
	}
	cleanupCancelledPublicImageTaskFiles(c, task)
	if !refundCancelledPublicImageTask(c, task) {
		return
	}
	c.JSON(http.StatusOK, publicImageTaskResponse(task, now))
}

func cleanupCancelledPublicImageTaskFiles(c *gin.Context, task *model.Task) {
	if task == nil || task.PrivateData.CancelledAt == 0 {
		return
	}
	service.ScheduleImageTaskRequestFileCleanup(task, time.Now().Unix())
	// 只写本函数负责的两个标量列。整体覆盖 private_data 会和后台退款恢复
	// （service.recoverPendingImageTaskRefunds 每 10s 在每个节点扫 refund_pending）
	// 并发打架，把它刚写入的 settlement_error / review 标注冲掉。
	// PrivateData 里的 RequestBodyBase64/Portable 由 FinalizeImageTaskRequestFileCleanup
	// 在事务加行锁后统一收口，不需要在这里落库。
	if err := model.DB.Model(&model.Task{}).
		Where("id = ? AND status = ?", task.ID, model.TaskStatusFailure).
		Updates(map[string]any{
			"request_cleanup_pending": task.RequestCleanupPending,
			"request_delete_after":    task.RequestDeleteAfter,
			"updated_at":              time.Now().Unix(),
		}).Error; err != nil {
		logger.LogWarn(c, "persist cancelled image task request cleanup failed: "+err.Error())
		return
	}
	cleanupCtx := context.Background()
	if c != nil && c.Request != nil {
		cleanupCtx = c.Request.Context()
	}
	if err := service.CleanupDueImageTaskRequestFile(cleanupCtx, task); err != nil {
		logger.LogWarn(c, "cancelled image task request file cleanup failed: "+err.Error())
	}
	resultPath := strings.TrimSpace(task.PrivateData.ResultBodyPath)
	if resultPath == "" {
		return
	}
	if err := common.RemoveDiskCacheFile(resultPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		logger.LogWarn(c, "cancelled image task result file cleanup failed: "+err.Error())
		return
	}
	// 同上：结果文件元数据的清理走事务 + 行锁重读，只覆盖结果相关字段。
	if err := model.ClearImageTaskResultFileMetadata(task.ID, resultPath); err != nil {
		logger.LogWarn(c, "persist cancelled image task file cleanup failed: "+err.Error())
		return
	}
	task.PrivateData.ResultBodyPath = ""
	task.ImageTaskResultStored = false
	task.PrivateData.ResultBodySize = 0
	task.PrivateData.ResultBodySHA256 = ""
	task.PrivateData.ResultContentType = ""
	task.PrivateData.ResultStoredAt = 0
	task.PrivateData.ResultExpiresAt = 0
}

func refundCancelledPublicImageTask(c *gin.Context, task *model.Task) bool {
	if task == nil {
		return true
	}
	if err := service.RefundTaskQuota(c.Request.Context(), task, task.FailReason); err != nil {
		if service.IsPublicImageTaskRefundInProgress(err) {
			c.Header("Retry-After", "1")
			publicImageTaskError(c, http.StatusConflict, "cancel_refund_in_progress", "image task was cancelled and the refund is still being processed")
			return false
		}
		publicImageTaskError(c, http.StatusInternalServerError, "cancel_refund_failed", "image task was cancelled but refund requires review")
		return false
	}
	reloaded, exists, err := model.GetTaskByID(task.ID)
	if err != nil || !exists {
		publicImageTaskError(c, http.StatusInternalServerError, "task_query_failed", "failed to verify image task refund")
		return false
	}
	*task = *reloaded
	if task.Quota != 0 || task.RefundPending {
		c.Header("Retry-After", "1")
		publicImageTaskError(c, http.StatusConflict, "cancel_refund_in_progress", "image task was cancelled and the refund is still being processed")
		return false
	}
	return true
}

func loadAuthorizedPublicImageTask(c *gin.Context) (*model.Task, bool) {
	task, exists, err := model.GetByTaskId(c.GetInt("id"), c.Param("task_id"))
	if err != nil {
		publicImageTaskError(c, http.StatusInternalServerError, "task_query_failed", "failed to query image task")
		return nil, false
	}
	if !exists || !publicImageTaskAuthorized(c, task) {
		publicImageTaskError(c, http.StatusNotFound, "task_not_found", "image task not found")
		return nil, false
	}
	return task, true
}

func loadAuthorizedPublicImageTaskMetadata(c *gin.Context) (*model.Task, bool) {
	task, exists, err := model.GetPublicImageTaskByTaskID(c.GetInt("id"), c.Param("task_id"))
	if err != nil {
		publicImageTaskError(c, http.StatusInternalServerError, "task_query_failed", "failed to query image task")
		return nil, false
	}
	if !exists || !publicImageTaskAuthorized(c, task) {
		publicImageTaskError(c, http.StatusNotFound, "task_not_found", "image task not found")
		return nil, false
	}
	return task, true
}

func publicImageTaskAuthorized(c *gin.Context, task *model.Task) bool {
	if c == nil || task == nil || !task.PrivateData.PublicImageTask || task.Platform != constant.TaskPlatformImage || task.UserId != c.GetInt("id") {
		return false
	}
	tokenID := c.GetInt("token_id")
	return tokenID > 0 && tokenID == task.PrivateData.TokenId
}

func publicImageTaskResponse(task *model.Task, now int64) *dto.PublicImageTask {
	response := &dto.PublicImageTask{}
	if task == nil {
		return response
	}
	response.TaskID = task.TaskID
	response.ClientTaskID = task.ClientTaskID
	response.Progress = task.Progress
	response.CreatedAt = task.CreatedAt
	response.UpdatedAt = task.UpdatedAt
	response.StartedAt = task.StartTime
	response.CompletedAt = task.FinishTime
	response.ResultAcknowledgedAt = task.ResultAcknowledgedAt
	switch {
	case task.PrivateData.CancelledAt > 0 && task.SettlementStatus == model.TaskSettlementStatusReview:
		response.Status = "failed"
		response.Error = &dto.PublicImageTaskError{Code: "refund_review", Message: "image task refund requires review"}
	case task.PrivateData.CancelledAt > 0 && (task.RefundPending || task.Quota != 0):
		response.Status = "cancelling"
	case task.PrivateData.CancelledAt > 0:
		response.Status = "cancelled"
	case task.Status == model.TaskStatusFailure && task.SettlementStatus == model.TaskSettlementStatusReview && task.RefundPending:
		response.Status = "failed"
		response.Error = &dto.PublicImageTaskError{Code: "refund_review", Message: "image task refund requires review"}
	case task.Status == model.TaskStatusFailure && task.SettlementStatus == model.TaskSettlementStatusReview:
		response.Status = "failed"
		response.Error = &dto.PublicImageTaskError{Code: "settlement_review", Message: "image task execution or settlement requires review"}
	case task.Status == model.TaskStatusFailure && task.RefundPending:
		response.Status = "failed"
		response.Error = &dto.PublicImageTaskError{Code: "refund_pending", Message: "image task refund is still being processed"}
	case task.Status == model.TaskStatusFailure:
		response.Status = "failed"
		response.Error = &dto.PublicImageTaskError{Code: "image_task_failed", Message: "image task failed"}
	case task.Status == model.TaskStatusSuccess && task.SettlementStatus == model.TaskSettlementStatusReview:
		response.Status = "failed"
		response.Error = &dto.PublicImageTaskError{Code: "settlement_review", Message: "image task settlement requires review"}
	case task.Status == model.TaskStatusSuccess && task.SettlementStatus != model.TaskSettlementStatusSettled:
		response.Status = "finalizing"
	case task.Status == model.TaskStatusSuccess:
		response.Status = "completed"
		response.ResultAvailable = publicImageTaskResultAvailable(task, now)
		response.ResultExpiresAt = publicImageTaskResultExpiry(task)
	case task.Status == model.TaskStatusInProgress:
		response.Status = "running"
	default:
		response.Status = "queued"
	}
	return response
}

func publicImageTaskResultAvailable(task *model.Task, now int64) bool {
	if task == nil || task.Status != model.TaskStatusSuccess || task.SettlementStatus != model.TaskSettlementStatusSettled {
		return false
	}
	if task.ResultCleanedAt > 0 || (task.ResultDeleteAfter > 0 && now >= task.ResultDeleteAfter) {
		return false
	}
	expiresAt := publicImageTaskResultExpiry(task)
	if expiresAt > 0 && now >= expiresAt {
		return false
	}
	return task.InlineResultAvailable || task.StoredResultAvailable || len(task.Data) > 0 || strings.TrimSpace(task.PrivateData.ResultBodyPath) != ""
}

func publicImageTaskResultExpiry(task *model.Task) int64 {
	if task == nil {
		return 0
	}
	expiresAt := int64(0)
	if task.ResultExpiresAt > 0 {
		expiresAt = task.ResultExpiresAt
	}
	if task.PrivateData.ResultExpiresAt > 0 && (expiresAt == 0 || task.PrivateData.ResultExpiresAt < expiresAt) {
		expiresAt = task.PrivateData.ResultExpiresAt
	}
	resultStoredAt := task.PrivateData.ResultStoredAt
	if resultStoredAt <= 0 {
		resultStoredAt = task.FinishTime
	}
	if resultStoredAt > 0 {
		completionExpiry := resultStoredAt + int64(common.GetImageTaskResultCacheRetention().Seconds())
		if expiresAt == 0 || completionExpiry < expiresAt {
			expiresAt = completionExpiry
		}
	}
	// ACK 之后真正的删除时间是 ResultDeleteAfter（ACK 时间 + 2 分钟缓冲），必须收敛到它，
	// 否则客户端会拿到一个最多 12 小时后的过期时间，然后在两分钟后就吃到 410。
	if task.ResultDeleteAfter > 0 && (expiresAt == 0 || task.ResultDeleteAfter < expiresAt) {
		expiresAt = task.ResultDeleteAfter
	}
	return expiresAt
}

func publicImageTaskError(c *gin.Context, status int, code string, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message, "type": "image_task_error"}})
}

// respondPublicImageTaskAPIError 把内部 *types.NewAPIError 翻译成 /v1/image-tasks/* 的
// 统一错误信封。createImageTaskInternal 与同步桥共用，直接透出 NewAPIError 会让同一个
// 幂等冲突在预检 handler 和创建流程里给出两套 error.code（预检给 idempotency_conflict，
// 创建流程给 invalid_request），而这两条路径的先后完全取决于并发竞态。
// 同步桥 /v1/images/* 继续使用 respondImageTaskError，信封不变。
func respondPublicImageTaskAPIError(c *gin.Context, apiErr *types.NewAPIError) {
	if apiErr == nil {
		return
	}
	status := apiErr.StatusCode
	if status == 0 {
		status = http.StatusInternalServerError
	}
	code := "image_task_create_failed"
	switch apiErr.GetErrorCode() {
	case types.ErrorCodeIdempotencyConflict:
		code = "idempotency_conflict"
	case types.ErrorCodeIdempotencyInProgress:
		code = "idempotency_in_progress"
	case types.ErrorCodeInsufficientUserQuota:
		code = "insufficient_quota"
	case types.ErrorCodePreConsumeTokenQuotaFailed:
		code = "insufficient_token_quota"
	case types.ErrorCodeModelPriceError:
		code = "model_price_error"
	case types.ErrorCodeReadRequestBodyFailed:
		switch {
		case errors.Is(apiErr, common.ErrDiskCacheCapacityUnavailable):
			code = "image_task_unavailable"
			c.Header("Retry-After", "1")
		case status >= http.StatusInternalServerError:
			code = "internal_error"
		default:
			code = "invalid_request"
		}
	case types.ErrorCodeInvalidRequest, types.ErrorCodeConvertRequestFailed:
		code = "invalid_request"
	case types.ErrorCodeQueryDataError:
		code = "task_query_failed"
	}
	message := apiErr.ToOpenAIError().Message
	if status >= http.StatusInternalServerError {
		logger.LogError(c, fmt.Sprintf("public image task request failed: %s", apiErr.MaskSensitiveError()))
		message = "image task request failed"
	}
	publicImageTaskError(c, status, code, message)
}

func isImageTaskRequestStorageError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, common.ErrDiskCacheCapacityUnavailable) || common.IsRequestBodyTooLargeError(err) {
		return true
	}
	var pathErr *os.PathError
	return errors.As(err, &pathErr)
}

func newImageTaskRequestStorageError(err error) *types.NewAPIError {
	switch {
	case errors.Is(err, common.ErrDiskCacheCapacityUnavailable):
		return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusServiceUnavailable)
	case common.IsRequestBodyTooLargeError(err):
		return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
	default:
		return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusInternalServerError)
	}
}

func respondPublicImageTaskRequestStorageError(c *gin.Context, err error) {
	respondPublicImageTaskAPIError(c, newImageTaskRequestStorageError(err))
}
