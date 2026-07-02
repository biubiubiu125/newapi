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
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const (
	imageTaskResultExpiredMessage  = "image task result expired"
	imageTaskStoredResultMarker    = "_newapi_result_file"
	maxImageTaskBatchQueryIDs      = 100
	maxImageTaskBatchQueryLength   = 8192
	maxImageTaskClientTaskIDLength = 191
)

func CreateImageTask(c *gin.Context) {
	relayMode := imageTaskRelayMode(c)
	c.Set("relay_mode", relayMode)

	imageRequest, err := helper.GetAndValidOpenAIImageRequest(c, relayMode)
	if err != nil {
		respondImageTaskError(c, types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry()))
		return
	}
	imageRequest.Stream = false

	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatOpenAIImage, imageRequest, nil)
	if err != nil {
		respondImageTaskError(c, types.NewError(err, types.ErrorCodeGenRelayInfoFailed))
		return
	}
	relayInfo.ForcePreConsume = true
	relayInfo.InitChannelMeta(c)
	imageTaskMode := relayInfo.ChannelOtherSettings.GetImageTaskMode()
	if err := validateImageTaskModeRequest(imageRequest, imageTaskMode); err != nil {
		respondImageTaskError(c, types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry()))
		return
	}
	service.EnsureImageTaskSharedCacheReady(c)
	persistedRequest, err := persistImageTaskRequest(c, relayMode, imageRequest.Model)
	if err != nil {
		statusCode := http.StatusBadRequest
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			statusCode = http.StatusRequestEntityTooLarge
		}
		respondImageTaskError(c, types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, statusCode, types.ErrOptionWithSkipRetry()))
		return
	}
	clientTaskIDProvided := persistedRequest.ClientTaskID != ""
	clientTaskIDReservationHeld := false
	clientTaskIDReservationCommitted := false
	if clientTaskIDProvided {
		existing, exist, err := model.GetImageTaskByClientTaskID(c.GetInt("id"), persistedRequest.ClientTaskID)
		if err != nil {
			_ = common.RemoveDiskCacheFile(persistedRequest.Path)
			respondImageTaskError(c, types.NewError(err, types.ErrorCodeQueryDataError))
			return
		}
		if exist && existing != nil {
			_ = common.RemoveDiskCacheFile(persistedRequest.Path)
			c.JSON(http.StatusOK, dto.ImageTaskCreateResponse{
				TaskID:       existing.TaskID,
				ClientTaskID: strings.TrimSpace(existing.ClientTaskID),
				Status:       imageTaskPublicStatusFromTask(existing),
				CreatedAt:    existing.CreatedAt,
			})
			return
		}
		_, reserved, err := model.ReserveImageTaskClientTaskID(c.GetInt("id"), persistedRequest.ClientTaskID)
		if err != nil {
			_ = common.RemoveDiskCacheFile(persistedRequest.Path)
			respondImageTaskError(c, types.NewError(err, types.ErrorCodeUpdateDataError))
			return
		}
		if !reserved {
			existing, exist, err = model.GetImageTaskByClientTaskID(c.GetInt("id"), persistedRequest.ClientTaskID)
			if err != nil {
				_ = common.RemoveDiskCacheFile(persistedRequest.Path)
				respondImageTaskError(c, types.NewError(err, types.ErrorCodeQueryDataError))
				return
			}
			if exist && existing != nil {
				_ = common.RemoveDiskCacheFile(persistedRequest.Path)
				c.JSON(http.StatusOK, dto.ImageTaskCreateResponse{
					TaskID:       existing.TaskID,
					ClientTaskID: strings.TrimSpace(existing.ClientTaskID),
					Status:       imageTaskPublicStatusFromTask(existing),
					CreatedAt:    existing.CreatedAt,
				})
				return
			}
			_ = common.RemoveDiskCacheFile(persistedRequest.Path)
			respondImageTaskError(c, types.NewErrorWithStatusCode(
				errors.New("client_task_id is already being created, please retry later"),
				types.ErrorCodeInvalidRequest,
				http.StatusConflict,
				types.ErrOptionWithSkipRetry(),
			))
			return
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
		respondImageTaskError(c, types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry()))
		return
	}

	meta := imageRequest.GetTokenCountMeta()
	if setting.ShouldCheckPromptSensitive() && meta != nil {
		contains, words := service.CheckSensitiveText(meta.CombineText)
		if contains {
			_ = common.RemoveDiskCacheFile(persistedRequest.Path)
			logger.LogWarn(c, fmt.Sprintf("user sensitive words detected: %s", strings.Join(words, ", ")))
			respondImageTaskError(c, types.NewError(errors.New("sensitive words detected"), types.ErrorCodeSensitiveWordsDetected, types.ErrOptionWithStatusCode(http.StatusBadRequest)))
			return
		}
	}

	tokens, err := service.EstimateRequestToken(c, meta, relayInfo)
	if err != nil {
		_ = common.RemoveDiskCacheFile(persistedRequest.Path)
		respondImageTaskError(c, types.NewError(err, types.ErrorCodeCountTokenFailed))
		return
	}
	relayInfo.SetEstimatePromptTokens(tokens)

	priceData, err := helper.ModelPriceHelper(c, relayInfo, tokens, meta)
	if err != nil {
		_ = common.RemoveDiskCacheFile(persistedRequest.Path)
		respondImageTaskError(c, types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest)))
		return
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
		respondImageTaskError(c, types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, statusCode, types.ErrOptionWithSkipRetry()))
		return
	}
	requestBodyPortable := requestBodyBase64 != ""
	persistedRequest.Body = nil
	if relayInfo.BillingRequestInput != nil {
		relayInfo.BillingRequestInput.Body = nil
	}

	if !priceData.FreeModel {
		if newAPIError := service.PreConsumeBilling(c, priceData.QuotaToPreConsume, relayInfo); newAPIError != nil {
			_ = common.RemoveDiskCacheFile(persistedRequest.Path)
			respondImageTaskError(c, newAPIError)
			return
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
		respondImageTaskError(c, types.NewError(err, types.ErrorCodeUpdateDataError))
		return
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
				c.JSON(http.StatusOK, dto.ImageTaskCreateResponse{
					TaskID:       existing.TaskID,
					ClientTaskID: strings.TrimSpace(existing.ClientTaskID),
					Status:       imageTaskPublicStatusFromTask(existing),
					CreatedAt:    existing.CreatedAt,
				})
				return
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

	c.JSON(http.StatusOK, dto.ImageTaskCreateResponse{
		TaskID:       task.TaskID,
		ClientTaskID: task.ClientTaskID,
		Status:       dto.ImageTaskStatusQueued,
		CreatedAt:    task.CreatedAt,
	})
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

func GetImageTask(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("task_id"))
	userID := c.GetInt("id")
	task, exist, err := model.GetByTaskId(userID, taskID)
	if err != nil {
		respondImageTaskError(c, types.NewError(err, types.ErrorCodeQueryDataError))
		return
	}
	if !exist || task == nil || task.Platform != constant.TaskPlatformImage {
		respondImageTaskError(c, types.NewErrorWithStatusCode(fmt.Errorf("task not found"), types.ErrorCodeInvalidRequest, http.StatusNotFound, types.ErrOptionWithSkipRetry()))
		return
	}
	if !imageTaskAllowedByTokenModelLimit(c, task) {
		respondImageTaskModelForbidden(c, task)
		return
	}
	c.JSON(http.StatusOK, imageTaskToResponseWithResult(task, imageTaskIncludeImageData(c)))
}

func GetImageTasks(c *gin.Context) {
	rawIDs := strings.TrimSpace(c.Query("ids"))
	clientTaskID, err := normalizeImageTaskClientTaskID(c.Query("client_task_id"))
	if err != nil {
		respondImageTaskError(c, types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry()))
		return
	}
	if rawIDs == "" && clientTaskID == "" {
		respondImageTaskError(c, types.NewErrorWithStatusCode(fmt.Errorf("ids is required"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry()))
		return
	}
	if len(rawIDs) > maxImageTaskBatchQueryLength {
		respondImageTaskError(c, types.NewErrorWithStatusCode(fmt.Errorf("ids is too long"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry()))
		return
	}
	taskIDs := make([]any, 0)
	requestedTaskIDs := make([]string, 0)
	seen := make(map[string]struct{})
	for _, id := range strings.Split(rawIDs, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		taskIDs = append(taskIDs, id)
		requestedTaskIDs = append(requestedTaskIDs, id)
		if len(taskIDs) > maxImageTaskBatchQueryIDs {
			respondImageTaskError(c, types.NewErrorWithStatusCode(fmt.Errorf("ids exceeds limit %d", maxImageTaskBatchQueryIDs), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry()))
			return
		}
	}
	if len(taskIDs) == 0 && clientTaskID == "" {
		respondImageTaskError(c, types.NewErrorWithStatusCode(fmt.Errorf("ids is required"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry()))
		return
	}
	tasks, err := model.GetImageTasksByTaskIDsOrClientTaskID(c.GetInt("id"), taskIDs, clientTaskID)
	if err != nil {
		respondImageTaskError(c, types.NewError(err, types.ErrorCodeQueryDataError))
		return
	}
	includeResult := imageTaskIncludeImageData(c)
	responses := make([]dto.ImageTaskResponse, 0, len(tasks))
	items := make([]dto.ImageTaskGPTImage2APIItem, 0, len(tasks))
	foundTaskIDs := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		if task == nil || task.Platform != constant.TaskPlatformImage {
			continue
		}
		if !imageTaskAllowedByTokenModelLimit(c, task) {
			respondImageTaskModelForbidden(c, task)
			return
		}
		foundTaskIDs[task.TaskID] = struct{}{}
		response := imageTaskToResponseWithResult(task, includeResult)
		responses = append(responses, response)
		items = append(items, imageTaskToGPTImage2APIItem(response))
	}
	missingIDs := make([]string, 0)
	for _, id := range requestedTaskIDs {
		if _, ok := foundTaskIDs[id]; !ok {
			missingIDs = append(missingIDs, id)
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"data":        responses,
		"items":       items,
		"missing_ids": missingIDs,
	})
}

func imageTaskAllowedByTokenModelLimit(c *gin.Context, task *model.Task) bool {
	if !common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled) {
		return true
	}
	if task == nil {
		return false
	}
	modelName := strings.TrimSpace(task.Properties.OriginModelName)
	if modelName == "" {
		return false
	}
	value, ok := common.GetContextKey(c, constant.ContextKeyTokenModelLimit)
	if !ok {
		return false
	}
	tokenModelLimit, ok := value.(map[string]bool)
	if !ok {
		return false
	}
	_, ok = tokenModelLimit[ratio_setting.FormatMatchingModelName(modelName)]
	return ok
}

func respondImageTaskModelForbidden(c *gin.Context, task *model.Task) {
	modelName := "unknown"
	if task != nil && strings.TrimSpace(task.Properties.OriginModelName) != "" {
		modelName = strings.TrimSpace(task.Properties.OriginModelName)
	}
	respondImageTaskError(c, types.NewErrorWithStatusCode(
		fmt.Errorf("model %s is not allowed for this token", modelName),
		types.ErrorCodeAccessDenied,
		http.StatusForbidden,
		types.ErrOptionWithSkipRetry(),
	))
}

func imageTaskToResponse(task *model.Task) dto.ImageTaskResponse {
	return imageTaskToResponseWithResult(task, true)
}

func imageTaskToResponseWithResult(task *model.Task, includeResult bool) dto.ImageTaskResponse {
	resp := dto.ImageTaskResponse{
		TaskID:       task.TaskID,
		ClientTaskID: strings.TrimSpace(task.ClientTaskID),
		Status:       imageTaskPublicStatusFromTask(task),
		Progress:     task.Progress,
		CreatedAt:    task.CreatedAt,
		UpdatedAt:    task.UpdatedAt,
		Error:        task.FailReason,
	}
	if includeResult && imageTaskResponseResultVisible(task) {
		result, resultErr := imageTaskResponseResult(task)
		if len(result) > 0 {
			resp.Result = result
		}
		if resultErr != "" && resp.Error == "" {
			resp.Error = resultErr
		}
	}
	return resp
}

func imageTaskToGPTImage2APIItem(resp dto.ImageTaskResponse) dto.ImageTaskGPTImage2APIItem {
	item := dto.ImageTaskGPTImage2APIItem{
		ID:           resp.TaskID,
		TaskID:       resp.TaskID,
		ClientTaskID: resp.ClientTaskID,
		Status:       resp.Status,
		Progress:     resp.Progress,
		CreatedAt:    resp.CreatedAt,
		UpdatedAt:    resp.UpdatedAt,
		Error:        resp.Error,
	}
	if len(resp.Result) == 0 {
		return item
	}
	if data, usage, ok := imageTaskGPTImage2APIDataFromResult(resp.Result); ok {
		item.Data = data
		item.Usage = usage
		return item
	}
	item.Result = append(json.RawMessage(nil), resp.Result...)
	return item
}

func imageTaskGPTImage2APIDataFromResult(result json.RawMessage) (json.RawMessage, json.RawMessage, bool) {
	result = bytes.TrimSpace(result)
	if len(result) == 0 || bytes.Equal(result, []byte("null")) {
		return nil, nil, false
	}
	var envelope map[string]json.RawMessage
	if err := common.Unmarshal(result, &envelope); err != nil {
		return nil, nil, false
	}
	data := bytes.TrimSpace(envelope["data"])
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil, nil, false
	}
	itemData := append(json.RawMessage(nil), data...)
	var itemUsage json.RawMessage
	if usage := bytes.TrimSpace(envelope["usage"]); len(usage) > 0 && !bytes.Equal(usage, []byte("null")) {
		itemUsage = append(json.RawMessage(nil), usage...)
	}
	return itemData, itemUsage, true
}

func imageTaskIncludeImageData(c *gin.Context) bool {
	if c == nil {
		return true
	}
	value := strings.TrimSpace(c.Query("include_image_data"))
	if value == "" {
		return true
	}
	return !strings.EqualFold(value, "false")
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

func imageTaskPublicStatus(status model.TaskStatus) string {
	switch status {
	case model.TaskStatusSuccess:
		return dto.ImageTaskStatusSuccess
	case model.TaskStatusFailure:
		return dto.ImageTaskStatusFailed
	case model.TaskStatusInProgress:
		return dto.ImageTaskStatusRunning
	default:
		return dto.ImageTaskStatusQueued
	}
}

func imageTaskPublicStatusFromTask(task *model.Task) string {
	if task == nil {
		return dto.ImageTaskStatusQueued
	}
	if task.Status == model.TaskStatusSuccess && task.SettlementStatus == model.TaskSettlementStatusReview {
		return dto.ImageTaskStatusFailed
	}
	if task.Status == model.TaskStatusSuccess && imageTaskSettlementOpen(task.SettlementStatus) {
		return dto.ImageTaskStatusRunning
	}
	return imageTaskPublicStatus(task.Status)
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
	if strings.HasPrefix(c.Request.URL.Path, "/v1/image-tasks/edits") {
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
	if strings.Contains(contentType, "multipart/form-data") {
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
