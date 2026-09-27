package relay

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const imageTaskSyncTimeout = 120 * time.Second
const imageTaskStoredResultMarker = "_newapi_result_file"
const imageTaskLargeStorageReadThreshold = 8 << 20
const imageTaskLargeStorageReadConcurrency = 4
const imageTaskReviewRequestRetention = 12 * time.Hour
const imageTaskSettlementReviewRetrySeconds int64 = 60

var imageTaskLargeStorageReadSlots = make(chan struct{}, imageTaskLargeStorageReadConcurrency)

type imageTaskStoredResultData struct {
	Stored      bool   `json:"_newapi_result_file"`
	Size        int64  `json:"size,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	StoredAt    int64  `json:"stored_at,omitempty"`
	ExpiresAt   int64  `json:"expires_at,omitempty"`
}

type imageTaskSyncResult struct {
	Response     []byte
	Usage        *dto.Usage
	ExtraContent []string
}

type imageTaskSettlementPayload struct {
	Result       json.RawMessage
	Usage        *dto.Usage
	ExtraContent []string
	BillingInput *billingexpr.RequestInput
}

type imageTaskBodyStorage struct {
	path string
	file *os.File
	size int64
}

func newImageTaskBodyStorage(path string) (*imageTaskBodyStorage, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return &imageTaskBodyStorage{
		path: path,
		file: file,
		size: stat.Size(),
	}, nil
}

func (s *imageTaskBodyStorage) Read(p []byte) (int, error) {
	return s.file.Read(p)
}

func (s *imageTaskBodyStorage) Seek(offset int64, whence int) (int64, error) {
	return s.file.Seek(offset, whence)
}

func (s *imageTaskBodyStorage) Close() error {
	if s == nil || s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	return err
}

func (s *imageTaskBodyStorage) NewReader() (io.ReadCloser, error) {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return nil, errors.New("task request body storage is missing")
	}
	return os.Open(s.path)
}

func (s *imageTaskBodyStorage) Bytes() ([]byte, error) {
	if s == nil || s.file == nil {
		return nil, errors.New("task request body storage is closed")
	}
	pos, err := s.file.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.path)
	if _, seekErr := s.file.Seek(pos, io.SeekStart); err == nil && seekErr != nil {
		err = seekErr
	}
	return data, err
}

func (s *imageTaskBodyStorage) Size() int64 {
	if s == nil {
		return 0
	}
	return s.size
}

func (s *imageTaskBodyStorage) IsDisk() bool {
	return true
}

func RunImageTasks(ctx context.Context, tasks []*model.Task) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for _, task := range tasks {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if task == nil || task.Platform != constant.TaskPlatformImage {
			continue
		}
		mode := task.PrivateData.ImageTaskMode
		if mode == "" {
			mode = dto.ImageTaskModeSyncWrapper
		}
		if imageTaskIsDone(task) && !imageTaskNeedsSettlement(task) {
			continue
		}
		if isRemovedImageTaskBridgeMode(mode) {
			if err := retireRemovedImageTaskBridgeTask(ctx, task); err != nil {
				logger.LogError(ctx, fmt.Sprintf("image task %s removed bridge retirement failed: %s", task.TaskID, err.Error()))
			}
			continue
		}
		if imageTaskShouldFailUnstarted(task) {
			reason := fmt.Sprintf("image task not started before timeout (%s)", imageTaskAsyncTimeoutText())
			if err := failImageTask(ctx, task, task.Status, reason, true, true); err != nil {
				logger.LogError(ctx, fmt.Sprintf("image task %s unstarted timeout cleanup failed: %s", task.TaskID, err.Error()))
			}
			continue
		}
		if imageTaskShouldFailStaleExecution(task, mode) {
			reason := fmt.Sprintf("image task execution timeout (%s)", imageTaskAsyncTimeoutText())
			var err error
			if task.SyncSubmissionStartedAt > 0 {
				err = markImageTaskExecutionReview(ctx, task, model.TaskStatusInProgress, reason)
			} else {
				err = failImageTask(ctx, task, model.TaskStatusInProgress, reason, true, true)
			}
			if err != nil {
				logger.LogError(ctx, fmt.Sprintf("image task %s timeout cleanup failed: %s", task.TaskID, err.Error()))
			}
			continue
		}
		if err := runSyncWrapperImageTask(ctx, task); err != nil {
			logger.LogError(ctx, fmt.Sprintf("image task %s run failed: %s", task.TaskID, err.Error()))
		}
	}
	return nil
}

func isRemovedImageTaskBridgeMode(mode string) bool {
	return mode == dto.ImageTaskModeAsyncTaskBridge || mode == "gpt_image2api_async"
}

func retireRemovedImageTaskBridgeTask(ctx context.Context, task *model.Task) error {
	if imageTaskNeedsSettlement(task) {
		return settleImageTaskSuccess(ctx, task, imageTaskSettlementPayload{})
	}
	if removedImageTaskBridgeNeedsReview(task) {
		return markImageTaskExecutionReview(ctx, task, task.Status, "async task bridge mode has been removed")
	}
	return failImageTask(ctx, task, task.Status, "async task bridge mode has been removed", true, true)
}

func removedImageTaskBridgeNeedsReview(task *model.Task) bool {
	if task == nil {
		return false
	}
	if strings.TrimSpace(task.PrivateData.UpstreamTaskID) != "" {
		return true
	}
	switch task.Status {
	case model.TaskStatusSubmitted, model.TaskStatusInProgress:
		return true
	default:
		return false
	}
}

func imageTaskNeedsSettlement(task *model.Task) bool {
	if task == nil || task.Status != model.TaskStatusSuccess {
		return false
	}
	switch task.SettlementStatus {
	case model.TaskSettlementStatusPending, model.TaskSettlementStatusApplied:
		return true
	case model.TaskSettlementStatusReview:
		return model.ImageTaskSettlementReviewIsRetryable(task)
	default:
		return false
	}
}

func imageTaskIsDone(task *model.Task) bool {
	return task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure
}

func imageTaskShouldFailStaleExecution(task *model.Task, mode string) bool {
	if task == nil || task.Status != model.TaskStatusInProgress || task.StartTime == 0 {
		return false
	}
	if mode == dto.ImageTaskModeAsyncTaskBridge {
		return false
	}
	return imageTaskShouldFailLongRunningUpstreamStatus(task)
}

func imageTaskShouldFailUnstarted(task *model.Task) bool {
	if task == nil || task.PrivateData.UpstreamTaskID != "" || task.SubmitTime == 0 {
		return false
	}
	timeout := imageTaskAsyncTimeout()
	if timeout <= 0 {
		return false
	}
	switch task.Status {
	case model.TaskStatusNotStart, model.TaskStatusQueued:
		return time.Now().Unix()-task.SubmitTime > int64(timeout.Seconds())
	default:
		return false
	}
}

func imageTaskResultRetention() time.Duration {
	return common.GetImageTaskResultCacheRetention()
}

func imageTaskExecutionTimedOut(task *model.Task) bool {
	if task == nil || task.Status != model.TaskStatusInProgress || task.StartTime == 0 {
		return false
	}
	return time.Now().Unix()-task.StartTime > int64(imageTaskSyncTimeout.Seconds())
}

func imageTaskCanStart(task *model.Task) bool {
	switch task.Status {
	case model.TaskStatusNotStart, model.TaskStatusQueued, model.TaskStatusSubmitted, model.TaskStatusInProgress:
		return true
	default:
		return false
	}
}

func runSyncWrapperImageTask(ctx context.Context, task *model.Task) error {
	if imageTaskNeedsSettlement(task) {
		return settleImageTaskSuccess(ctx, task, imageTaskSettlementPayload{})
	}
	if !imageTaskCanStart(task) {
		return nil
	}
	oldStatus := task.Status
	if oldStatus == model.TaskStatusInProgress && task.SyncSubmissionStartedAt > 0 {
		reason := "image task sync submission outcome is unknown after worker lease recovery; refusing to replay upstream request"
		return markImageTaskExecutionReview(ctx, task, model.TaskStatusInProgress, reason)
	}
	now := time.Now().Unix()
	task.Status = model.TaskStatusInProgress
	task.Progress = "1%"
	if task.StartTime == 0 {
		task.StartTime = now
	}
	won, err := updateImageTaskWithStatus(ctx, task, oldStatus)
	if err != nil || !won {
		return err
	}

	execCtx, cancel := context.WithTimeout(ctx, imageTaskSyncTimeout)
	defer cancel()

	bodyStorage, contentType, err := openImageTaskBodyStorage(task)
	if err != nil {
		return failImageTask(ctx, task, model.TaskStatusInProgress, err.Error(), true, true)
	}
	defer bodyStorage.Close()
	result, err := executeSyncImageTask(execCtx, task, bodyStorage, contentType)
	if err != nil {
		_ = bodyStorage.Close()
		if task.SyncSubmissionStartedAt > 0 {
			return markImageTaskExecutionReview(ctx, task, model.TaskStatusInProgress, err.Error())
		}
		return failImageTask(ctx, task, model.TaskStatusInProgress, err.Error(), true, true)
	}

	task.Status = model.TaskStatusSuccess
	task.Progress = "100%"
	task.FinishTime = time.Now().Unix()
	task.NextPollAt = 0
	task.RetryCount = 0
	task.SettlementStatus = model.TaskSettlementStatusPending
	task.PrivateData.SettlementUsage = cloneImageTaskUsage(result.Usage)
	task.PrivateData.SettlementExtraContent = append([]string(nil), result.ExtraContent...)
	resultPath, err := storeImageTaskResultData(task, json.RawMessage(result.Response), task.FinishTime)
	if err != nil {
		return markImageTaskUpstreamResultReview(ctx, task, model.TaskStatusInProgress, fmt.Sprintf("store image task result failed: %s", err.Error()))
	}
	settlementBillingInput, billingInputErr := imageTaskBillingRequestInputFromStoredBody(task)
	if billingInputErr != nil {
		return markImageTaskUpstreamResultReview(ctx, task, model.TaskStatusInProgress, fmt.Sprintf("load settlement billing evidence failed: %s", billingInputErr.Error()))
	}
	settlementBillingInput, billingInputErr = captureImageTaskSettlementBillingEvidence(task, settlementBillingInput)
	if billingInputErr != nil {
		return markImageTaskUpstreamResultReview(ctx, task, model.TaskStatusInProgress, fmt.Sprintf("capture settlement billing evidence failed: %s", billingInputErr.Error()))
	}
	task.PrivateData.BillingRequestInput = settlementBillingInput
	task.PrivateData.BillingRequestInputCaptured = true
	task.PrivateData.SettlementEvidenceCapturedAt = task.FinishTime
	task.ClearImageTaskExecutionSecrets()
	won, err = updateImageTaskWithStatus(ctx, task, model.TaskStatusInProgress)
	if err != nil {
		removeImageTaskResultPath(resultPath)
		return err
	}
	if !won {
		removeImageTaskResultPath(resultPath)
		return nil
	}
	return settleImageTaskSuccess(ctx, task, imageTaskSettlementPayload{
		Result:       json.RawMessage(result.Response),
		Usage:        result.Usage,
		ExtraContent: result.ExtraContent,
		BillingInput: settlementBillingInput,
	})
}

func updateImageTaskWithStatus(ctx context.Context, task *model.Task, fromStatus model.TaskStatus) (bool, error) {
	if task == nil {
		return false, nil
	}
	owner := service.ImageTaskLeaseOwnerForTaskFromContext(ctx, task.ID)
	if owner == "" {
		return task.UpdateWithStatus(fromStatus)
	}
	return task.UpdateWithStatusAndLease(fromStatus, owner, time.Now().Unix())
}

func executeSyncImageTask(ctx context.Context, task *model.Task, bodyStorage common.BodyStorage, contentType string) (*imageTaskSyncResult, error) {
	relayMode := imageTaskRelayModeFromTask(task)
	path := task.PrivateData.RequestPath
	if path == "" {
		path = imageTaskRequestPathFromTask(task)
	}
	if _, err := bodyStorage.Seek(0, io.SeekStart); err != nil {
		_ = bodyStorage.Close()
		return nil, err
	}

	recorder := httptest.NewRecorder()
	fakeCtx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, path, bodyStorage).WithContext(ctx)
	applyImageTaskRequestHeaders(req.Header, task.PrivateData.RequestHeaders)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	if task.TaskID != "" {
		req.Header.Set("Idempotency-Key", task.TaskID)
		req.Header.Set("X-NewAPI-Task-ID", task.TaskID)
	}
	fakeCtx.Request = req
	fakeCtx.Set("relay_mode", relayMode)
	fakeCtx.Set(common.RequestIdKey, task.TaskID)
	fakeCtx.Set(contextKeyImageTaskDeferBilling, true)
	fakeCtx.Set(common.KeyBodyStorage, bodyStorage)
	defer common.CleanupBodyStorage(fakeCtx)
	// 这里没有 net/http 的请求收尾流程，multipart 解析出的临时文件必须自行释放。
	defer func() {
		if fakeCtx.Request != nil && fakeCtx.Request.MultipartForm != nil {
			_ = fakeCtx.Request.MultipartForm.RemoveAll()
		}
	}()

	if err := setupImageTaskGinContext(fakeCtx, task); err != nil {
		return nil, err
	}

	imageRequest, err := helper.GetAndValidOpenAIImageRequest(fakeCtx, relayMode)
	if err != nil {
		return nil, err
	}
	imageRequest.Stream = common.GetPointer(false)

	relayInfo, err := buildImageTaskRelayInfo(fakeCtx, task, imageRequest, relayMode, bodyStorage, contentType)
	if err != nil {
		return nil, err
	}
	startedAt := time.Now().Unix()
	owner := service.ImageTaskLeaseOwnerForTaskFromContext(ctx, task.ID)
	marked, err := model.MarkImageTaskSyncSubmissionStarted(task.ID, owner, startedAt, startedAt)
	if err != nil {
		return nil, err
	}
	if !marked {
		return nil, errors.New("image task sync submission marker lost CAS")
	}
	task.SyncSubmissionStartedAt = startedAt
	if newAPIError := ImageHelper(fakeCtx, relayInfo); newAPIError != nil {
		return nil, newAPIError
	}
	if recorder.Code >= http.StatusBadRequest {
		return nil, fmt.Errorf("image generation failed with status %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.Len() == 0 {
		return nil, errors.New("empty image response")
	}

	result := &imageTaskSyncResult{
		Response: append([]byte(nil), recorder.Body.Bytes()...),
	}
	if deferred, ok := getImageTaskDeferredBilling(fakeCtx); ok && deferred != nil {
		usageCopy := deferred.Usage
		extraContent := append([]string(nil), deferred.ExtraContent...)
		result.Usage = &usageCopy
		result.ExtraContent = extraContent
	}
	return result, nil
}

func setupImageTaskGinContext(c *gin.Context, task *model.Task) error {
	if err := setupImageTaskBaseGinContext(c, task); err != nil {
		return err
	}
	channel, err := model.CacheGetChannel(task.ChannelId)
	if err != nil {
		return err
	}
	return setupImageTaskSelectedChannelContext(c, task, channel, imageTaskFixedUpstreamKey(task, ""))
}

func setupImageTaskBaseGinContext(c *gin.Context, task *model.Task) error {
	common.SetContextKey(c, constant.ContextKeyUserId, task.UserId)
	c.Set("id", task.UserId)
	common.SetContextKey(c, constant.ContextKeyUsingGroup, task.Group)
	common.SetContextKey(c, constant.ContextKeyTokenGroup, task.Group)
	common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now())

	if user, err := model.GetUserById(task.UserId, false); err == nil && user != nil {
		common.SetContextKey(c, constant.ContextKeyUserGroup, user.Group)
		common.SetContextKey(c, constant.ContextKeyUserQuota, user.Quota)
		common.SetContextKey(c, constant.ContextKeyUserEmail, user.Email)
		common.SetContextKey(c, constant.ContextKeyUserName, user.Username)
		common.SetContextKey(c, constant.ContextKeyUserSetting, user.GetSetting())
	} else {
		common.SetContextKey(c, constant.ContextKeyUserGroup, task.Group)
	}

	if tokenID := task.PrivateData.TokenId; tokenID > 0 {
		common.SetContextKey(c, constant.ContextKeyTokenId, tokenID)
		if token, err := model.GetTokenById(tokenID); err == nil && token != nil {
			common.SetContextKey(c, constant.ContextKeyTokenKey, token.Key)
			common.SetContextKey(c, constant.ContextKeyTokenUnlimited, token.UnlimitedQuota)
			c.Set("token_name", token.Name)
		}
	}

	modelName := imageTaskModelName(task)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, modelName)
	return nil
}

func setupImageTaskSelectedChannelContext(c *gin.Context, task *model.Task, channel *model.Channel, selectedKey string) error {
	modelName := imageTaskModelName(task)
	if apiErr := middleware.SetupContextForSelectedChannel(c, channel, modelName); apiErr != nil {
		return apiErr
	}
	if fixedKey := imageTaskFixedUpstreamKey(task, selectedKey); fixedKey != "" {
		common.SetContextKey(c, constant.ContextKeyChannelKey, fixedKey)
	}
	return nil
}

func buildImageTaskRelayInfo(c *gin.Context, task *model.Task, request *dto.ImageRequest, relayMode int, bodyStorage common.BodyStorage, contentType string) (*relaycommon.RelayInfo, error) {
	billingInput, err := imageTaskBillingRequestInputFromTask(task, bodyStorage, contentType)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	info := &relaycommon.RelayInfo{
		Request:               request,
		RelayFormat:           types.RelayFormatOpenAIImage,
		RelayMode:             relayMode,
		RequestURLPath:        imageTaskRequestPathFromTask(task),
		UserId:                task.UserId,
		UsingGroup:            task.Group,
		UserGroup:             common.GetContextKeyString(c, constant.ContextKeyUserGroup),
		UserQuota:             common.GetContextKeyInt64(c, constant.ContextKeyUserQuota),
		UserEmail:             common.GetContextKeyString(c, constant.ContextKeyUserEmail),
		OriginModelName:       imageTaskModelName(task),
		TokenId:               task.PrivateData.TokenId,
		TokenKey:              common.GetContextKeyString(c, constant.ContextKeyTokenKey),
		TokenUnlimited:        common.GetContextKeyBool(c, constant.ContextKeyTokenUnlimited),
		TokenGroup:            task.Group,
		StartTime:             now,
		FirstResponseTime:     now.Add(-time.Second),
		FinalPreConsumedQuota: task.Quota,
		ForcePreConsume:       true,
		BillingSource:         task.PrivateData.BillingSource,
		SubscriptionId:        task.PrivateData.SubscriptionId,
		PriceData:             priceDataFromTask(task),
		RequestHeaders:        cloneImageTaskStringMap(task.PrivateData.RequestHeaders),
		TieredBillingSnapshot: cloneImageTaskTieredSnapshot(task.PrivateData.TieredBillingSnapshot),
		BillingRequestInput:   billingInput,
	}
	if setting, ok := common.GetContextKeyType[dto.UserSetting](c, constant.ContextKeyUserSetting); ok {
		info.UserSetting = setting
	}
	info.InitRequestConversionChain()
	return info, nil
}

func applyImageTaskRequestHeaders(header http.Header, src map[string]string) {
	for key, value := range src {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" || imageTaskSensitiveHeader(key) {
			continue
		}
		header.Set(key, value)
	}
}

func imageTaskBillingRequestInputFromTask(task *model.Task, bodyStorage common.BodyStorage, contentType string) (*billingexpr.RequestInput, error) {
	if task == nil {
		return nil, nil
	}
	if task.PrivateData.TieredBillingSnapshot == nil && task.PrivateData.BillingRequestInput == nil {
		return nil, nil
	}
	input := cloneImageTaskBillingRequestInput(task.PrivateData.BillingRequestInput)
	if input == nil {
		input = &billingexpr.RequestInput{}
	}
	headers := cloneImageTaskStringMap(task.PrivateData.RequestHeaders)
	if len(headers) > 0 {
		for key, value := range input.Headers {
			headers[key] = value
		}
		input.Headers = headers
	}
	if len(input.Body) == 0 && imageTaskIsJSONContentType(contentType) && bodyStorage != nil && bodyStorage.Size() > 0 {
		maxBytes := imageTaskBillingRequestBodyMaxBytes()
		if maxBytes > 0 && bodyStorage.Size() > maxBytes {
			return input, fmt.Errorf("%w: image task billing request body exceeds %d MB", common.ErrRequestBodyTooLarge, maxBytes>>20)
		}
		body, err := readImageTaskBodyStorageBytes(bodyStorage)
		if err != nil {
			return nil, err
		}
		input.Body = append([]byte(nil), body...)
		if _, err := bodyStorage.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
	}
	if len(input.Headers) == 0 && len(input.Body) == 0 {
		return nil, nil
	}
	return input, nil
}

func imageTaskBillingRequestBodyMaxBytes() int64 {
	maxMB := constant.ImageTaskRequestBodyBase64MaxMB
	if maxMB <= 0 {
		maxMB = 16
	}
	return int64(maxMB) << 20
}

func imageTaskBillingRequestInputFromStoredBody(task *model.Task) (*billingexpr.RequestInput, error) {
	if task == nil {
		return nil, nil
	}
	if task.PrivateData.TieredBillingSnapshot == nil && task.PrivateData.BillingRequestInput == nil {
		return nil, nil
	}
	if task.PrivateData.BillingRequestInputCaptured || task.PrivateData.SettlementEvidenceCapturedAt > 0 {
		return cloneImageTaskBillingRequestInput(task.PrivateData.BillingRequestInput), nil
	}
	bodyStorage, contentType, err := openImageTaskBodyStorage(task)
	if err != nil {
		return cloneImageTaskBillingRequestInput(task.PrivateData.BillingRequestInput), err
	}
	defer bodyStorage.Close()
	return imageTaskBillingRequestInputFromTask(task, bodyStorage, contentType)
}

func cloneImageTaskBillingRequestInput(src *billingexpr.RequestInput) *billingexpr.RequestInput {
	if src == nil {
		return nil
	}
	dst := billingexpr.CloneRequestInput(*src)
	return &dst
}

func captureImageTaskSettlementBillingEvidence(task *model.Task, input *billingexpr.RequestInput) (*billingexpr.RequestInput, error) {
	if input == nil {
		return nil, nil
	}
	evidence := cloneImageTaskBillingRequestInput(input)
	evidence.Body = nil
	if task != nil && task.PrivateData.TieredBillingSnapshot != nil && task.PrivateData.TieredBillingSnapshot.BillingMode == "tiered_expr" {
		expression := task.PrivateData.TieredBillingSnapshot.ExprString
		if len(evidence.Params) == 0 {
			params, err := billingexpr.CaptureRequestParams(expression, input.Body)
			if err != nil {
				return nil, err
			}
			evidence.Params = params
		}
		headers, err := billingexpr.CaptureRequestHeaders(expression, input.Headers)
		if err != nil {
			return nil, err
		}
		evidence.Headers = headers
	} else {
		evidence.Headers = nil
	}
	if len(evidence.Headers) == 0 && len(evidence.Params) == 0 {
		return nil, nil
	}
	return evidence, nil
}

func minimizeImageTaskSettlementBillingEvidence(task *model.Task) {
	if task == nil || task.PrivateData.BillingRequestInput == nil {
		return
	}
	evidence, err := captureImageTaskSettlementBillingEvidence(task, task.PrivateData.BillingRequestInput)
	if err != nil {
		evidence = cloneImageTaskBillingRequestInput(task.PrivateData.BillingRequestInput)
		if evidence != nil {
			evidence.Body = nil
			evidence.Headers = nil
		}
	}
	task.PrivateData.BillingRequestInput = evidence
	task.PrivateData.BillingRequestInputCaptured = true
}

func cloneImageTaskTieredSnapshot(src *billingexpr.BillingSnapshot) *billingexpr.BillingSnapshot {
	if src == nil {
		return nil
	}
	dst := *src
	return &dst
}

func cloneImageTaskStringMap(src map[string]string) map[string]string {
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

func imageTaskIsJSONContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	return contentType == "" || strings.HasPrefix(contentType, "application/json")
}

func priceDataFromTask(task *model.Task) types.PriceData {
	priceData := types.PriceData{
		Quota:             task.Quota,
		QuotaToPreConsume: task.Quota,
	}
	if bc := task.PrivateData.BillingContext; bc != nil {
		priceData.ModelPrice = bc.ModelPrice
		priceData.ModelRatio = bc.ModelRatio
		priceData.CompletionRatio = bc.CompletionRatio
		priceData.CacheRatio = bc.CacheRatio
		priceData.CacheCreationRatio = bc.CacheCreationRatio
		priceData.CacheCreation5mRatio = bc.CacheCreation5mRatio
		priceData.CacheCreation1hRatio = bc.CacheCreation1hRatio
		priceData.ImageRatio = bc.ImageRatio
		priceData.AudioRatio = bc.AudioRatio
		priceData.AudioCompletionRatio = bc.AudioCompletionRatio
		priceData.GroupRatioInfo = types.GroupRatioInfo{
			GroupRatio:        bc.GroupRatio,
			GroupSpecialRatio: bc.GroupSpecialRatio,
			HasSpecialRatio:   bc.GroupHasSpecialRatio,
		}
		priceData.ReplaceOtherRatios(cloneImageTaskFloatMap(bc.OtherRatios))
		priceData.UsePrice = bc.PerCallBilling
	}
	return priceData
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

func imageTaskShouldRecoverPendingAsyncSubmission(task *model.Task) bool {
	if task == nil || task.PrivateData.UpstreamTaskID != "" || task.StartTime == 0 {
		return false
	}
	switch task.Status {
	case model.TaskStatusInProgress, model.TaskStatusSubmitted:
		return true
	default:
		return false
	}
}

// markImageTaskStorageNodePortableAfterSubmission 在上游已接单后解除任务与创建节点的绑定。
// 提交成功后轮询与结算都不再需要本地请求体文件，放开 storage_node 可以让任意节点接管，
// 避免创建节点下线或改名后任务永远无人调度。
// 例外：tiered_expr 计费且尚未捕获请求参数证据的任务仍需读取原始请求体，保持节点绑定。
func markImageTaskStorageNodePortableAfterSubmission(task *model.Task) {
	if task == nil || task.StorageNode == model.ImageTaskPortableStorageNode {
		return
	}
	snapshot := task.PrivateData.TieredBillingSnapshot
	needsOriginalBody := snapshot != nil &&
		snapshot.BillingMode == "tiered_expr" &&
		!task.PrivateData.BillingRequestInputCaptured
	if needsOriginalBody {
		return
	}
	task.StorageNode = model.ImageTaskPortableStorageNode
}

func clearImageTaskUpstreamSubmissionUncertainty(task *model.Task) {
	if task == nil {
		return
	}
	task.PrivateData.UpstreamSubmitUncertainAt = 0
	task.PrivateData.UpstreamSubmitUncertainCount = 0
}

func settleImageTaskSuccess(ctx context.Context, task *model.Task, payload imageTaskSettlementPayload) error {
	if task == nil || task.Status != model.TaskStatusSuccess {
		return nil
	}
	if task.SettlementStatus == model.TaskSettlementStatusSettled {
		return nil
	}
	if task.SettlementStatus == model.TaskSettlementStatusReview {
		if !model.ImageTaskSettlementReviewIsRetryable(task) {
			return nil
		}
		if err := retryImageTaskSettlementReview(ctx, task); err != nil {
			return err
		}
	}
	if task.SettlementStatus == model.TaskSettlementStatusApplied {
		if err := markImageTaskSettlementSettled(ctx, task, model.TaskSettlementStatusApplied); err != nil {
			markImageTaskTransientRetry(task)
			return err
		}
		return nil
	}

	settlementRecord, settlementRecordExists, err := model.GetTaskSettlementRecord(task.ID)
	if err != nil {
		markImageTaskTransientRetry(task)
		return err
	}
	if settlementRecordExists {
		handled, err := handleExistingImageTaskSettlementRecord(ctx, task, settlementRecord)
		if handled {
			return err
		}
	}

	usage := cloneImageTaskUsage(payload.Usage)
	if usage == nil {
		usage = cloneImageTaskUsage(task.PrivateData.SettlementUsage)
	}
	extraContent := append([]string(nil), payload.ExtraContent...)
	if len(extraContent) == 0 && len(task.PrivateData.SettlementExtraContent) > 0 {
		extraContent = append(extraContent, task.PrivateData.SettlementExtraContent...)
	}
	result := payload.Result
	evidenceCaptured := task.PrivateData.SettlementEvidenceCapturedAt > 0
	if len(result) == 0 && !evidenceCaptured {
		var err error
		result, err = loadImageTaskStoredResultData(task)
		if err != nil {
			reason := fmt.Sprintf("image task settlement result unavailable: %s", err.Error())
			if reviewErr := markImageTaskSettlementReview(ctx, task, reason); reviewErr != nil {
				return reviewErr
			}
			return errors.New(reason)
		}
	}
	if len(result) == 0 && !evidenceCaptured {
		reason := "image task success result is empty, cannot settle billing"
		if reviewErr := markImageTaskSettlementReview(ctx, task, reason); reviewErr != nil {
			return reviewErr
		}
		return errors.New(reason)
	}
	billingInput := payload.BillingInput
	if billingInput == nil {
		var billingInputErr error
		if evidenceCaptured {
			billingInput = cloneImageTaskBillingRequestInput(task.PrivateData.BillingRequestInput)
		} else {
			billingInput, billingInputErr = imageTaskBillingRequestInputFromStoredBody(task)
		}
		if billingInputErr != nil {
			logger.LogWarn(ctx, fmt.Sprintf("load image task %s billing request body failed: %s", task.TaskID, billingInputErr.Error()))
			if task.PrivateData.TieredBillingSnapshot != nil {
				reason := fmt.Sprintf("image task settlement billing request body unavailable: %s", billingInputErr.Error())
				if settlementRecordExists {
					if reviewErr := model.MarkTaskSettlementApplicationReview(task.ID, reason); reviewErr != nil {
						markImageTaskTransientRetry(task)
						return fmt.Errorf("%s; mark review failed: %w", reason, reviewErr)
					}
				}
				if reviewErr := markImageTaskSettlementReview(ctx, task, reason); reviewErr != nil {
					return reviewErr
				}
				return errors.New(reason)
			}
		}
	}

	settlementRecord, shouldApplySettlement, err := model.BeginTaskSettlementApplication(task)
	if err != nil {
		markImageTaskTransientRetry(task)
		return err
	}
	if !shouldApplySettlement {
		handled, err := handleExistingImageTaskSettlementRecord(ctx, task, settlementRecord)
		if handled {
			return err
		}
		markImageTaskTransientRetry(task)
		return fmt.Errorf("image task settlement could not start for task %s", task.TaskID)
	}

	if err := markImageTaskSettlementApplicationApplying(ctx, task); err != nil {
		markImageTaskTransientRetry(task)
		return err
	}
	preConsumedQuota := task.Quota
	actualQuota, err := settleImageTaskConsumption(ctx, task, result, usage, extraContent, billingInput)
	if err != nil {
		reason := fmt.Sprintf("image task settlement requires manual review: %s", err.Error())
		if reviewErr := model.MarkTaskSettlementApplicationReview(task.ID, reason); reviewErr != nil {
			markImageTaskTransientRetry(task)
			return fmt.Errorf("%s; mark review failed: %w", reason, reviewErr)
		}
		if reviewErr := markImageTaskSettlementReview(ctx, task, reason); reviewErr != nil {
			return reviewErr
		}
		return errors.New(reason)
	}

	settlementDetails := model.TaskSettlementApplicationAppliedDetails{
		Operation:        "image_consumption",
		AppliedQuota:     common.GetPointer(actualQuota),
		PreConsumedQuota: common.GetPointer(preConsumedQuota),
		QuotaDelta:       common.GetPointer(actualQuota - preConsumedQuota),
		LogType:          common.GetPointer(model.LogTypeConsume),
	}
	return finalizeAppliedImageTaskSettlement(ctx, task, settlementDetails)
}

func handleExistingImageTaskSettlementRecord(ctx context.Context, task *model.Task, settlementRecord *model.TaskSettlementRecord) (bool, error) {
	if task == nil || settlementRecord == nil {
		return false, nil
	}
	switch settlementRecord.Status {
	case model.TaskSettlementRecordStatusApplied:
		if settlementRecord.Operation != "" && settlementRecord.Operation != "image_consumption" {
			reason := fmt.Sprintf("applied image task settlement operation is %s, expected image_consumption", settlementRecord.Operation)
			if err := markImageTaskSettlementReview(ctx, task, reason); err != nil {
				return true, err
			}
			return true, errors.New(reason)
		}
		if settlementRecord.AppliedQuota == nil {
			reason := "applied image task settlement has no applied quota evidence"
			if err := markImageTaskSettlementReview(ctx, task, reason); err != nil {
				return true, err
			}
			return true, errors.New(reason)
		}
		return true, finalizeAppliedImageTaskSettlement(ctx, task, model.TaskSettlementApplicationAppliedDetails{
			AppliedQuota: settlementRecord.AppliedQuota,
		})
	case model.TaskSettlementRecordStatusReview:
		return true, markImageTaskSettlementReview(ctx, task, settlementRecord.Error)
	case model.TaskSettlementRecordStatusPrepared:
		return false, nil
	case model.TaskSettlementRecordStatusApplying:
		if settlementRecord.Operation == model.TaskSettlementOperationImageAtomic {
			return false, nil
		}
		refreshedRecord, _, err := model.BeginTaskSettlementApplication(task)
		if err != nil {
			markImageTaskTransientRetry(task)
			return true, err
		}
		if refreshedRecord != nil && refreshedRecord.Status != settlementRecord.Status {
			return handleExistingImageTaskSettlementRecord(ctx, task, refreshedRecord)
		}
		markImageTaskTransientRetry(task)
		return true, fmt.Errorf("image task settlement is already applying for task %s", task.TaskID)
	default:
		markImageTaskTransientRetry(task)
		return true, fmt.Errorf("image task settlement has unknown record status %s for task %s", settlementRecord.Status, task.TaskID)
	}
}

func finalizeAppliedImageTaskSettlement(ctx context.Context, task *model.Task, details ...model.TaskSettlementApplicationAppliedDetails) error {
	if task == nil {
		return nil
	}
	if len(details) > 0 && details[0].AppliedQuota != nil && task.Quota != *details[0].AppliedQuota {
		task.Quota = *details[0].AppliedQuota
		if err := task.UpdateQuota(); err != nil {
			return fmt.Errorf("finalize image task applied quota: %w", err)
		}
	}
	if task.SettlementStatus == model.TaskSettlementStatusSettled {
		return nil
	}
	if task.SettlementStatus == model.TaskSettlementStatusPending {
		if err := markImageTaskSettlementApplied(ctx, task); err != nil {
			markImageTaskTransientRetry(task)
			return err
		}
		if task.SettlementStatus == model.TaskSettlementStatusSettled {
			return nil
		}
	}
	if task.SettlementStatus != model.TaskSettlementStatusApplied {
		return fmt.Errorf("image task settlement cannot finalize from status %s", task.SettlementStatus)
	}
	if err := markImageTaskSettlementSettled(ctx, task, model.TaskSettlementStatusApplied); err != nil {
		markImageTaskTransientRetry(task)
		return err
	}
	return nil
}

func markImageTaskSettlementApplicationApplying(ctx context.Context, task *model.Task) error {
	if task == nil {
		return nil
	}
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = model.MarkTaskSettlementApplicationApplyingAtomic(task.ID)
		if err == nil {
			return nil
		}
		if !sleepImageTaskSettlementUpdateRetry(ctx, attempt) {
			break
		}
	}
	return fmt.Errorf("mark image task settlement application applying: %w", err)
}

func markImageTaskSettlementApplicationApplied(ctx context.Context, task *model.Task, details model.TaskSettlementApplicationAppliedDetails) error {
	if task == nil {
		return nil
	}
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = model.MarkTaskSettlementApplicationApplied(task.ID, details)
		if err == nil {
			return nil
		}
		if !sleepImageTaskSettlementUpdateRetry(ctx, attempt) {
			break
		}
	}
	return fmt.Errorf("mark image task settlement application applied: %w", err)
}

func markImageTaskSettlementApplied(ctx context.Context, task *model.Task) error {
	if task == nil {
		return nil
	}
	task.SettlementStatus = model.TaskSettlementStatusApplied
	task.NextPollAt = 0
	task.RetryCount = 0
	var won bool
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		won, err = task.UpdateSettlementStatus(model.TaskStatusSuccess, model.TaskSettlementStatusPending)
		if err == nil {
			break
		}
		if !sleepImageTaskSettlementUpdateRetry(ctx, attempt) {
			break
		}
	}
	if err != nil {
		return fmt.Errorf("mark image task settlement applied: %w", err)
	}
	if !won {
		latest, exists, loadErr := model.GetTaskByID(task.ID)
		if loadErr != nil {
			return loadErr
		}
		if !exists || latest.Status != model.TaskStatusSuccess ||
			(latest.SettlementStatus != model.TaskSettlementStatusApplied && latest.SettlementStatus != model.TaskSettlementStatusSettled) {
			return errors.New("image task settlement applied update lost CAS")
		}
		task.SettlementStatus = latest.SettlementStatus
	}
	return nil
}

func retryImageTaskSettlementReview(ctx context.Context, task *model.Task) error {
	if task == nil || task.SettlementStatus != model.TaskSettlementStatusReview {
		return nil
	}
	if err := model.ResetTaskSettlementApplicationFromReview(task.ID); err != nil {
		markImageTaskTransientRetry(task)
		return err
	}
	fromSettlementStatus := model.TaskSettlementStatusReview
	task.SettlementStatus = model.TaskSettlementStatusPending
	task.FailReason = ""
	task.PrivateData.SettlementAttemptQuota = 0
	task.PrivateData.SettlementError = ""
	task.NextPollAt = 0
	task.LockOwner = ""
	task.LockUntil = 0
	won, err := task.UpdateSettlementStatus(model.TaskStatusSuccess, fromSettlementStatus)
	if err != nil {
		markImageTaskTransientRetry(task)
		return err
	}
	if !won {
		latest, exists, loadErr := model.GetTaskByID(task.ID)
		if loadErr != nil {
			return loadErr
		}
		if exists && latest.Status == model.TaskStatusSuccess && latest.SettlementStatus == model.TaskSettlementStatusPending {
			*task = *latest
			return nil
		}
		return errors.New("image task settlement review retry lost CAS")
	}
	return nil
}

func imageTaskSettlementReviewShouldPark(task *model.Task, reason string) bool {
	if task == nil {
		return true
	}
	if model.ImageTaskHasSettlementEvidence(task) {
		return false
	}
	reason = strings.ToLower(strings.TrimSpace(reason))
	return strings.Contains(reason, "result is empty") ||
		strings.Contains(reason, "expired before settlement")
}

func imageTaskSettlementReviewNextPollAt(task *model.Task, reason string) int64 {
	if imageTaskSettlementReviewShouldPark(task, reason) {
		return 0
	}
	now := time.Now().Unix()
	if task != nil && task.NextPollAt > now {
		return task.NextPollAt
	}
	return now + imageTaskSettlementReviewRetrySeconds
}

func markImageTaskSettlementReview(ctx context.Context, task *model.Task, reason string) error {
	if task == nil {
		return nil
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "image task settlement requires manual review"
	}
	minimizeImageTaskSettlementBillingEvidence(task)
	task.ClearImageTaskExecutionSecrets()
	task.NextPollAt = imageTaskSettlementReviewNextPollAt(task, reason)
	if task.SettlementStatus == model.TaskSettlementStatusReview {
		if !task.RequestCleanupPending || task.RequestDeleteAfter <= 0 {
			service.ScheduleImageTaskRequestFileCleanup(task, imageTaskReviewRequestDeleteAfter(task))
		}
		won, err := task.UpdateSettlementStatus(model.TaskStatusSuccess, model.TaskSettlementStatusReview)
		if err != nil {
			return err
		}
		if !won {
			return errors.New("image task settlement review maintenance lost CAS")
		}
		return nil
	}
	fromSettlementStatus := task.SettlementStatus
	if fromSettlementStatus == "" || fromSettlementStatus == model.TaskSettlementStatusSettled {
		return fmt.Errorf("image task settlement cannot mark review from status %s", fromSettlementStatus)
	}
	task.SettlementStatus = model.TaskSettlementStatusReview
	task.FailReason = reason
	task.LockOwner = ""
	task.LockUntil = 0
	task.RetryCount = 0
	clearImageTaskUpstreamSubmissionUncertainty(task)
	service.ScheduleImageTaskRequestFileCleanup(task, imageTaskReviewRequestDeleteAfter(task))
	won, err := task.UpdateSettlementStatus(model.TaskStatusSuccess, fromSettlementStatus)
	if err != nil {
		return err
	}
	if !won {
		latest, exists, loadErr := model.GetTaskByID(task.ID)
		if loadErr != nil {
			return loadErr
		}
		if !exists || latest.Status != model.TaskStatusSuccess || latest.SettlementStatus != model.TaskSettlementStatusReview {
			return errors.New("image task settlement review status update lost CAS")
		}
	}
	touchImageTaskReviewCachePaths(task)
	return nil
}

func imageTaskReviewRequestDeleteAfter(task *model.Task) int64 {
	now := time.Now().Unix()
	base := now
	if task != nil && task.FinishTime > 0 && task.FinishTime <= now {
		base = task.FinishTime
	}
	return base + int64(imageTaskReviewRequestRetention.Seconds())
}

func markImageTaskUpstreamResultReview(ctx context.Context, task *model.Task, fromStatus model.TaskStatus, reason string) error {
	if task == nil {
		return nil
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "image task upstream result requires manual review"
	}
	now := time.Now().Unix()
	task.Status = model.TaskStatusSuccess
	task.Progress = "100%"
	if task.FinishTime == 0 {
		task.FinishTime = now
	}
	task.NextPollAt = 0
	task.LockOwner = ""
	task.LockUntil = 0
	task.RetryCount = 0
	task.SettlementStatus = model.TaskSettlementStatusReview
	task.FailReason = reason
	clearImageTaskUpstreamSubmissionUncertainty(task)
	minimizeImageTaskSettlementBillingEvidence(task)
	task.ClearImageTaskExecutionSecrets()
	service.ScheduleImageTaskRequestFileCleanup(task, imageTaskReviewRequestDeleteAfter(task))
	won, err := updateImageTaskWithStatus(ctx, task, fromStatus)
	if err != nil {
		return err
	}
	if !won {
		return errors.New("image task upstream result review status update lost CAS")
	}
	touchImageTaskReviewCachePaths(task)
	return nil
}

func touchImageTaskReviewCachePaths(task *model.Task) {
	if task == nil {
		return
	}
	now := time.Now()
	touchImageTaskCachePath(task.PrivateData.RequestBodyPath, now)
	touchImageTaskCachePath(task.PrivateData.ResultBodyPath, now)
}

func touchImageTaskCachePath(path string, now time.Time) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	_ = os.Chtimes(path, now, now)
}

func sleepImageTaskSettlementUpdateRetry(ctx context.Context, attempt int) bool {
	delay := time.Duration(attempt+1) * 100 * time.Millisecond
	if ctx == nil {
		time.Sleep(delay)
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func markImageTaskSettlementSettled(ctx context.Context, task *model.Task, fromSettlementStatus string) error {
	if task == nil {
		return nil
	}
	settledAt := time.Now().Unix()
	task.SettlementStatus = model.TaskSettlementStatusSettled
	task.FailReason = ""
	task.NextPollAt = 0
	task.LockOwner = ""
	task.LockUntil = 0
	task.RetryCount = 0
	clearImageTaskUpstreamSubmissionUncertainty(task)
	task.PrivateData.SettlementUsage = nil
	task.PrivateData.SettlementExtraContent = nil
	task.PrivateData.BillingRequestInput = nil
	task.PrivateData.BillingRequestInputCaptured = false
	task.PrivateData.SettlementEvidenceCapturedAt = 0
	task.PrivateData.SettlementAttemptQuota = 0
	task.PrivateData.SettlementError = ""
	task.ClearImageTaskExecutionSecrets()
	resultStoredAt := task.PrivateData.ResultStoredAt
	if resultStoredAt <= 0 {
		resultStoredAt = task.FinishTime
	}
	setImageTaskResultLifecycle(task, resultStoredAt)
	service.ScheduleImageTaskRequestFileCleanup(task, settledAt)
	won, err := task.UpdateSettlementStatus(model.TaskStatusSuccess, fromSettlementStatus)
	if err != nil {
		return err
	}
	if !won {
		latest, exists, loadErr := model.GetTaskByID(task.ID)
		if loadErr != nil {
			return loadErr
		}
		if !exists || latest.Status != model.TaskStatusSuccess || latest.SettlementStatus != model.TaskSettlementStatusSettled {
			return errors.New("image task settlement status update lost CAS")
		}
	}
	if cleanupErr := service.CleanupDueImageTaskRequestFile(ctx, task); cleanupErr != nil {
		logger.LogWarn(ctx, fmt.Sprintf("image task %s request file cleanup failed: %s", task.TaskID, cleanupErr.Error()))
	}
	return nil
}

func loadImageTaskStoredResultData(task *model.Task) (json.RawMessage, error) {
	if task == nil {
		return nil, nil
	}
	path := strings.TrimSpace(task.PrivateData.ResultBodyPath)
	if path == "" {
		if imageTaskDataIsStoredResultPlaceholder(task.Data) {
			return nil, errors.New("image task stored result body path is missing")
		}
		return append(json.RawMessage(nil), task.Data...), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read image task result body: %w", err)
	}
	if task.PrivateData.ResultBodySize > 0 && int64(len(data)) != task.PrivateData.ResultBodySize {
		return nil, fmt.Errorf("image task result body size mismatch")
	}
	if task.PrivateData.ResultBodySHA256 != "" {
		sum := sha256.Sum256(data)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), task.PrivateData.ResultBodySHA256) {
			return nil, fmt.Errorf("image task result body checksum mismatch")
		}
	}
	return json.RawMessage(data), nil
}

func imageTaskDataIsStoredResultPlaceholder(data json.RawMessage) bool {
	if len(data) == 0 {
		return false
	}
	var placeholder imageTaskStoredResultData
	if err := common.Unmarshal(data, &placeholder); err != nil {
		return false
	}
	return placeholder.Stored
}

func markImageTaskTransientRetry(task *model.Task) {
	if task == nil {
		return
	}
	task.RetryCount++
}

func imageTaskShouldFailLongRunningUpstreamStatus(task *model.Task) bool {
	if task == nil || task.StartTime == 0 {
		return false
	}
	timeout := imageTaskAsyncTimeout()
	if timeout <= 0 {
		return false
	}
	switch task.Status {
	case model.TaskStatusQueued, model.TaskStatusSubmitted, model.TaskStatusInProgress:
	default:
		return false
	}
	return time.Now().Unix()-task.StartTime > int64(timeout.Seconds())
}

func imageTaskAsyncTimeout() time.Duration {
	if constant.TaskTimeoutMinutes <= 0 {
		return 0
	}
	return time.Duration(constant.TaskTimeoutMinutes) * time.Minute
}

func imageTaskAsyncTimeoutText() string {
	timeout := imageTaskAsyncTimeout()
	if timeout <= 0 {
		return "disabled"
	}
	if timeout%time.Minute == 0 {
		return fmt.Sprintf("%d minutes", int(timeout/time.Minute))
	}
	return fmt.Sprintf("%d seconds", int(timeout/time.Second))
}

func taskItemMatchesJSON(item gjson.Result, upstreamID string) bool {
	if strings.TrimSpace(upstreamID) == "" || !item.IsObject() {
		return false
	}
	for _, key := range []string{"task_id", "id", "upstream_task_id", "client_task_id"} {
		if stringValueFromJSON(item, key) == upstreamID {
			return true
		}
	}
	return false
}

func taskItemHasIdentityJSON(item gjson.Result) bool {
	if !item.IsObject() {
		return false
	}
	for _, key := range []string{"task_id", "id", "upstream_task_id", "client_task_id"} {
		if stringValueFromJSON(item, key) != "" {
			return true
		}
	}
	return false
}

func stringValueFromJSON(item gjson.Result, keys ...string) string {
	for _, key := range keys {
		value := item.Get(key)
		if !value.Exists() {
			continue
		}
		switch value.Type {
		case gjson.String:
			if s := strings.TrimSpace(value.String()); s != "" {
				return s
			}
		case gjson.Number:
			if value.Num == float64(int64(value.Num)) {
				return fmt.Sprintf("%d", int64(value.Num))
			}
			return fmt.Sprintf("%g", value.Num)
		}
	}
	return ""
}

func progressValueFromJSON(item gjson.Result) string {
	for _, key := range []string{"progress", "percent"} {
		value := item.Get(key)
		if !value.Exists() {
			continue
		}
		switch value.Type {
		case gjson.String:
			return strings.TrimSpace(value.String())
		case gjson.Number:
			v := value.Num
			if v <= 1 {
				v *= 100
			}
			return fmt.Sprintf("%.0f%%", v)
		}
	}
	return ""
}

func errorValueToStringJSON(value gjson.Result) string {
	if value.Type == gjson.String || value.Type == gjson.Number {
		return strings.TrimSpace(value.String())
	}
	for _, key := range []string{"message", "error", "reason"} {
		if msg := stringValueFromJSON(value, key); msg != "" {
			return msg
		}
	}
	return strings.TrimSpace(value.Raw)
}

func joinJSONObjectRaw(firstKey string, firstValue gjson.Result, secondKey string, secondValue gjson.Result) json.RawMessage {
	out := make([]byte, 0, len(firstValue.Raw)+len(secondValue.Raw)+32)
	out = append(out, '{')
	out = append(out, '"')
	out = append(out, firstKey...)
	out = append(out, `":`...)
	out = append(out, firstValue.Raw...)
	if secondKey != "" {
		out = append(out, ',')
		out = append(out, '"')
		out = append(out, secondKey...)
		out = append(out, `":`...)
		out = append(out, secondValue.Raw...)
	}
	out = append(out, '}')
	return json.RawMessage(out)
}

func rawMessageFromJSONResult(value gjson.Result) json.RawMessage {
	raw := strings.TrimSpace(value.Raw)
	if raw == "" || raw == "null" {
		return nil
	}
	return json.RawMessage(append([]byte(nil), raw...))
}

func readImageTaskBodyStorageBytes(storage common.BodyStorage) ([]byte, error) {
	if storage == nil {
		return nil, errors.New("task request body storage is missing")
	}
	release := acquireImageTaskLargeStorageReadSlot(storage.Size())
	defer release()
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

func acquireImageTaskLargeStorageReadSlot(size int64) func() {
	if size < imageTaskLargeStorageReadThreshold {
		return func() {}
	}
	imageTaskLargeStorageReadSlots <- struct{}{}
	return func() {
		<-imageTaskLargeStorageReadSlots
	}
}

func openImageTaskBodyStorage(task *model.Task) (common.BodyStorage, string, error) {
	if task == nil {
		return nil, "", errors.New("task request body is missing")
	}
	contentType := task.PrivateData.RequestContentType
	if contentType == "" {
		contentType = "application/json"
	}
	if task.PrivateData.RequestBodyPortable && strings.TrimSpace(task.PrivateData.RequestBodyBase64) != "" {
		body, err := decodeImageTaskRequestBodyBase64(task.PrivateData.RequestBodyBase64)
		if err != nil {
			return nil, "", err
		}
		if task.PrivateData.RequestBodySize > 0 && int64(len(body)) != task.PrivateData.RequestBodySize {
			return nil, "", fmt.Errorf("task request body fallback size mismatch")
		}
		storage, err := common.CreateBodyStorage(body)
		if err != nil {
			return nil, "", err
		}
		return storage, contentType, nil
	}
	bodyPath := strings.TrimSpace(task.PrivateData.RequestBodyPath)
	if bodyPath != "" {
		storage, err := newImageTaskBodyStorage(bodyPath)
		if err == nil {
			if task.PrivateData.RequestBodySize > 0 && storage.Size() != task.PrivateData.RequestBodySize {
				_ = storage.Close()
				err = fmt.Errorf("task request body size mismatch")
			} else {
				return storage, contentType, nil
			}
		}
		if err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("open image task %s request body cache failed: %s", task.TaskID, err.Error()))
		}
		if strings.TrimSpace(task.PrivateData.RequestBodyBase64) == "" {
			if err == nil {
				err = errors.New("task request body is missing")
			}
			return nil, "", err
		}
	}
	body, err := decodeImageTaskRequestBodyBase64(task.PrivateData.RequestBodyBase64)
	if err != nil {
		return nil, "", err
	}
	if task.PrivateData.RequestBodySize > 0 && int64(len(body)) != task.PrivateData.RequestBodySize {
		return nil, "", fmt.Errorf("task request body fallback size mismatch")
	}
	storage, err := common.CreateBodyStorage(body)
	if err != nil {
		return nil, "", err
	}
	return storage, contentType, nil
}

func decodeImageTaskRequestBodyBase64(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("task request body is missing")
	}
	body, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode task request body: %w", err)
	}
	return body, nil
}

func storeImageTaskResultData(task *model.Task, result json.RawMessage, storedAt int64) (string, error) {
	if task == nil {
		return "", nil
	}
	task.PrivateData.ResultBodyPath = ""
	task.PrivateData.ResultBodySize = 0
	task.PrivateData.ResultBodySHA256 = ""
	task.PrivateData.ResultContentType = ""
	task.PrivateData.ResultStoredAt = 0
	task.PrivateData.ResultExpiresAt = 0
	task.ImageTaskResultStored = false
	task.ImageTaskResultStoredAt = 0
	task.ResultExpiresAt = 0
	task.ResultAcknowledgedAt = 0
	task.ResultDeleteAfter = 0
	task.ResultCleanedAt = 0
	task.ResultCleanupPending = false

	result, err := cacheImageTaskResultURLs(result)
	if err != nil {
		return "", err
	}
	data := []byte(result)
	offload, err := imageTaskResultStorageAction(data)
	if err != nil {
		return "", err
	}
	if !offload {
		task.PrivateData.ResultStoredAt = storedAt
		task.ImageTaskResultStoredAt = storedAt
		setImageTaskResultLifecycle(task, storedAt)
		task.Data = append(json.RawMessage(nil), result...)
		return "", nil
	}

	path, err := common.WriteImageTaskResultCacheFile(data)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	task.PrivateData.ResultBodyPath = path
	task.PrivateData.ResultBodySize = int64(len(data))
	task.PrivateData.ResultBodySHA256 = sha
	task.PrivateData.ResultContentType = "application/json"
	task.PrivateData.ResultStoredAt = storedAt
	task.ImageTaskResultStored = true
	task.ImageTaskResultStoredAt = storedAt
	setImageTaskResultLifecycle(task, storedAt)

	placeholder, err := common.Marshal(imageTaskStoredResultData{
		Stored:      true,
		Size:        int64(len(data)),
		SHA256:      sha,
		ContentType: "application/json",
		StoredAt:    storedAt,
		ExpiresAt:   task.ResultExpiresAt,
	})
	if err != nil {
		_ = common.RemoveDiskCacheFile(path)
		task.PrivateData.ResultBodyPath = ""
		task.PrivateData.ResultBodySize = 0
		task.PrivateData.ResultBodySHA256 = ""
		task.PrivateData.ResultContentType = ""
		task.PrivateData.ResultStoredAt = 0
		task.PrivateData.ResultExpiresAt = 0
		task.ImageTaskResultStored = false
		task.ImageTaskResultStoredAt = 0
		task.ResultExpiresAt = 0
		return "", err
	}
	task.Data = json.RawMessage(placeholder)
	return path, nil
}

func cacheImageTaskResultURLs(result json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(result)) == 0 || !bytes.Contains(result, []byte(`"url"`)) {
		return result, nil
	}

	var payload map[string]any
	if err := common.Unmarshal(result, &payload); err != nil {
		return nil, fmt.Errorf("parse image task result for url caching: %w", err)
	}
	items, ok := payload["data"].([]any)
	if !ok || len(items) == 0 {
		return result, nil
	}

	changed := false
	for index, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		if existingB64, ok := item["b64_json"].(string); ok && strings.TrimSpace(existingB64) != "" {
			if _, hasURL := item["url"]; hasURL {
				delete(item, "url")
				changed = true
			}
			continue
		}
		resultURL, ok := item["url"].(string)
		if !ok || strings.TrimSpace(resultURL) == "" {
			continue
		}
		b64JSON, err := imageTaskResultURLToB64JSON(resultURL)
		if err != nil {
			return nil, fmt.Errorf("cache image task result url at data[%d]: %w", index, err)
		}
		item["b64_json"] = b64JSON
		delete(item, "url")
		changed = true
	}
	if !changed {
		return result, nil
	}
	normalized, err := common.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal cached image task result: %w", err)
	}
	return json.RawMessage(normalized), nil
}

func imageTaskResultURLToB64JSON(resultURL string) (string, error) {
	resultURL = strings.TrimSpace(resultURL)
	if resultURL == "" {
		return "", errors.New("image result url is empty")
	}
	if strings.HasPrefix(strings.ToLower(resultURL), "data:") {
		contentType, data, err := service.DecodeBase64FileData(resultURL)
		if err != nil {
			return "", err
		}
		return validateImageTaskResultB64JSON(contentType, data)
	}
	contentType, data, err := service.GetImageFromUrl(resultURL)
	if err != nil {
		return "", err
	}
	return validateImageTaskResultB64JSON(contentType, data)
}

func validateImageTaskResultB64JSON(contentType string, data string) (string, error) {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if contentType == "" || !strings.HasPrefix(contentType, "image/") {
		return "", fmt.Errorf("invalid image result content type: %s", contentType)
	}
	_, _, cleanBase64, err := service.DecodeBase64ImageData(data)
	if err != nil {
		return "", err
	}
	return cleanBase64, nil
}

func setImageTaskResultLifecycle(task *model.Task, availableAt int64) {
	if task == nil || availableAt <= 0 {
		return
	}
	expiresAt := availableAt + int64(imageTaskResultRetention().Seconds())
	if task.ResultExpiresAt > 0 && task.ResultExpiresAt < expiresAt {
		expiresAt = task.ResultExpiresAt
	}
	if task.PrivateData.ResultExpiresAt > 0 && task.PrivateData.ResultExpiresAt < expiresAt {
		expiresAt = task.PrivateData.ResultExpiresAt
	}
	task.ResultExpiresAt = expiresAt
	task.PrivateData.ResultExpiresAt = expiresAt
}

func imageTaskResultStorageAction(data []byte) (bool, error) {
	if len(data) == 0 {
		return false, nil
	}
	// Large b64_json payloads prefer trusted shared file cache when available.
	// URL-only and other inline JSON still go through the same size guard so a
	// huge non-b64 response cannot blow past IMAGE_TASK_RESULT_INLINE_MAX_MB.
	if imageTaskResultHasB64JSON(data) && service.ImageTaskFileCacheSharedTrusted() {
		return true, nil
	}
	// 无法外置时结果只能内联进数据库。超过上限时不再尝试写库：
	// 反复写失败会让任务停在执行中直到全量超时退款，而上游其实已经生成了图片。
	// 这里直接返回错误，由调用方转为结算人工审查，避免误退款。
	if maxBytes := imageTaskResultInlineMaxBytes(); maxBytes > 0 && int64(len(data)) > maxBytes {
		return false, fmt.Errorf(
			"image task result is too large to store inline (%d bytes > %d MB); enable IMAGE_TASK_FILE_CACHE_SHARED_TRUSTED or raise IMAGE_TASK_RESULT_INLINE_MAX_MB",
			len(data), maxBytes>>20,
		)
	}
	return false, nil
}

func imageTaskResultInlineMaxBytes() int64 {
	if constant.ImageTaskResultInlineMaxMB <= 0 {
		return 0
	}
	return int64(constant.ImageTaskResultInlineMaxMB) << 20
}

func imageTaskResultHasB64JSON(data []byte) bool {
	if !bytes.Contains(data, []byte(`"b64_json"`)) {
		return false
	}
	if !gjson.ValidBytes(data) {
		return true
	}
	return imageTaskJSONResultHasB64JSON(gjson.ParseBytes(data))
}

func imageTaskJSONResultHasB64JSON(value gjson.Result) bool {
	if value.IsObject() {
		found := false
		value.ForEach(func(key, nested gjson.Result) bool {
			if key.String() == "b64_json" {
				found = true
				return false
			}
			if imageTaskJSONResultHasB64JSON(nested) {
				found = true
				return false
			}
			return true
		})
		return found
	}
	if value.IsArray() {
		found := false
		value.ForEach(func(_, nested gjson.Result) bool {
			if imageTaskJSONResultHasB64JSON(nested) {
				found = true
				return false
			}
			return true
		})
		return found
	}
	return false
}

func takeImageTaskResultPath(task *model.Task) string {
	if task == nil || task.PrivateData.ResultBodyPath == "" {
		return ""
	}
	path := task.PrivateData.ResultBodyPath
	task.PrivateData.ResultBodyPath = ""
	task.ImageTaskResultStored = false
	task.ImageTaskResultStoredAt = 0
	task.PrivateData.ResultBodySize = 0
	task.PrivateData.ResultBodySHA256 = ""
	task.PrivateData.ResultContentType = ""
	task.PrivateData.ResultStoredAt = 0
	task.PrivateData.ResultExpiresAt = 0
	return path
}

func removeImageTaskResultPath(path string) {
	if path == "" {
		return
	}
	_ = common.RemoveDiskCacheFile(path)
}

func failImageTask(ctx context.Context, task *model.Task, fromStatus model.TaskStatus, reason string, refund bool, cleanup bool) error {
	task.Status = model.TaskStatusFailure
	task.Progress = "100%"
	task.FailReason = reason
	task.FinishTime = time.Now().Unix()
	task.NextPollAt = 0
	task.LockOwner = ""
	task.LockUntil = 0
	task.RetryCount = 0
	task.SettlementStatus = ""
	clearImageTaskUpstreamSubmissionUncertainty(task)
	task.PrivateData.SettlementUsage = nil
	task.PrivateData.SettlementExtraContent = nil
	task.PrivateData.BillingRequestInput = nil
	task.PrivateData.BillingRequestInputCaptured = false
	task.PrivateData.SettlementEvidenceCapturedAt = 0
	task.RefundPending = refund && task.Quota != 0
	task.ClearImageTaskExecutionSecrets()
	var resultPath string
	if cleanup {
		service.ScheduleImageTaskRequestFileCleanup(task, task.FinishTime)
		resultPath = takeImageTaskResultPath(task)
	}
	won, err := updateImageTaskWithStatus(ctx, task, fromStatus)
	if err != nil {
		return err
	}
	if !won {
		return errors.New("image task failure status update lost CAS")
	}
	if refund && task.Quota != 0 {
		if err := service.RefundTaskQuota(ctx, task, reason); err != nil {
			logger.LogError(ctx, fmt.Sprintf("image task %s refund failed: %s", task.TaskID, err.Error()))
		}
	}
	if cleanup {
		if cleanupErr := service.CleanupDueImageTaskRequestFile(ctx, task); cleanupErr != nil {
			logger.LogWarn(ctx, fmt.Sprintf("image task %s request file cleanup failed: %s", task.TaskID, cleanupErr.Error()))
		}
		removeImageTaskResultPath(resultPath)
	}
	return nil
}

func markImageTaskExecutionReview(ctx context.Context, task *model.Task, fromStatus model.TaskStatus, reason string) error {
	service.PrepareImageTaskExecutionReview(task, time.Now().Unix(), reason)
	won, err := updateImageTaskWithStatus(ctx, task, fromStatus)
	if err != nil {
		return err
	}
	if !won {
		return errors.New("image task execution review status update lost CAS")
	}
	return nil
}

func imageTaskFixedUpstreamKey(task *model.Task, selectedKey string) string {
	if task != nil {
		if key := strings.TrimSpace(task.PrivateData.Key); key != "" {
			return key
		}
	}
	return strings.TrimSpace(selectedKey)
}

func newImageTaskRelayFakeContext(ctx context.Context, task *model.Task, method string, path string, body io.Reader, contentType string) (*gin.Context, func()) {
	if ctx == nil {
		ctx = context.Background()
	}
	recorder := httptest.NewRecorder()
	fakeCtx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(method, path, body).WithContext(ctx)
	applyImageTaskRequestHeaders(req.Header, task.PrivateData.RequestHeaders)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	fakeCtx.Request = req
	fakeCtx.Set("relay_mode", imageTaskRelayModeFromTask(task))
	fakeCtx.Set(common.RequestIdKey, task.TaskID)
	cleanup := func() {
		if fakeCtx.Request != nil && fakeCtx.Request.MultipartForm != nil {
			_ = fakeCtx.Request.MultipartForm.RemoveAll()
		}
	}
	return fakeCtx, cleanup
}

func imageTaskOutboundCacheReservationBytes(inputSize int64) int64 {
	maxMB := constant.MaxRequestBodyMB
	if maxMB <= 0 {
		maxMB = 128
	}
	maxBytes := int64(maxMB) << 20
	if inputSize <= 0 {
		return maxBytes
	}
	reserveBytes := inputSize + (1 << 20)
	if reserveBytes <= 0 || reserveBytes > maxBytes {
		return maxBytes
	}
	return reserveBytes
}

func cloneMIMEHeader(header textproto.MIMEHeader) textproto.MIMEHeader {
	cloned := make(textproto.MIMEHeader, len(header))
	for key, values := range header {
		cloned[key] = append([]string(nil), values...)
	}
	return cloned
}

func settleImageTaskConsumption(ctx context.Context, task *model.Task, result json.RawMessage, usage *dto.Usage, extraContent []string, billingInput *billingexpr.RequestInput) (int, error) {
	if billingInput == nil {
		billingInput = cloneImageTaskBillingRequestInput(task.PrivateData.BillingRequestInput)
	}
	startTime := imageTaskRelayStartTime(task)
	fakeCtx, cleanup := newImageTaskRelayFakeContext(ctx, task, http.MethodPost, imageTaskRequestPathFromTask(task), nil, "")
	defer cleanup()
	if err := setupImageTaskBaseGinContext(fakeCtx, task); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("setup image task %s billing context failed: %s", task.TaskID, err.Error()))
	}
	common.SetContextKey(fakeCtx, constant.ContextKeyRequestStartTime, startTime)
	common.SetContextKey(fakeCtx, constant.ContextKeyChannelId, task.ChannelId)
	tokenKey := ""
	tokenUnlimited := false
	if tokenID := task.PrivateData.TokenId; tokenID > 0 {
		fakeCtx.Set("token_id", tokenID)
		common.SetContextKey(fakeCtx, constant.ContextKeyTokenId, tokenID)
		if token, err := model.GetTokenById(tokenID); err == nil && token != nil {
			fakeCtx.Set("token_name", token.Name)
			common.SetContextKey(fakeCtx, constant.ContextKeyTokenKey, token.Key)
			tokenKey = token.Key
			tokenUnlimited = token.UnlimitedQuota
		}
	}

	info := &relaycommon.RelayInfo{
		RelayFormat:           types.RelayFormatOpenAIImage,
		RelayMode:             imageTaskRelayModeFromTask(task),
		RequestURLPath:        imageTaskRequestPathFromTask(task),
		UserId:                task.UserId,
		UsingGroup:            task.Group,
		UserGroup:             common.GetContextKeyString(fakeCtx, constant.ContextKeyUserGroup),
		UserQuota:             common.GetContextKeyInt64(fakeCtx, constant.ContextKeyUserQuota),
		UserEmail:             common.GetContextKeyString(fakeCtx, constant.ContextKeyUserEmail),
		OriginModelName:       imageTaskModelName(task),
		TokenId:               task.PrivateData.TokenId,
		TokenKey:              tokenKey,
		TokenUnlimited:        tokenUnlimited,
		TokenGroup:            task.Group,
		StartTime:             startTime,
		FirstResponseTime:     startTime.Add(-time.Second),
		FinalPreConsumedQuota: task.Quota,
		ForcePreConsume:       true,
		BillingSource:         task.PrivateData.BillingSource,
		SubscriptionId:        task.PrivateData.SubscriptionId,
		PriceData:             priceDataFromTask(task),
		RequestHeaders:        cloneImageTaskStringMap(task.PrivateData.RequestHeaders),
		TieredBillingSnapshot: cloneImageTaskTieredSnapshot(task.PrivateData.TieredBillingSnapshot),
		BillingRequestInput:   billingInput,
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			Action:       task.Action,
			PublicTaskID: task.TaskID,
		},
		ChannelMeta: imageTaskChannelMetaFromTask(ctx, task),
	}
	info.InitRequestConversionChain()
	if usage == nil {
		if parsedUsage, ok := imageTaskUsageFromResult(result); ok {
			usage = parsedUsage
		}
	}
	settlement := service.ImageTaskAtomicSettlement{
		ActualQuota:    info.PriceData.Quota,
		UseTimeSeconds: max(0, int(time.Since(info.StartTime).Seconds())),
		ModelName:      info.OriginModelName,
		TokenName:      fakeCtx.GetString("token_name"),
		Other:          map[string]interface{}{},
	}
	if usage != nil {
		prepared, err := service.PrepareImageTaskAtomicSettlement(fakeCtx, info, usage, extraContent)
		if err != nil {
			return 0, err
		}
		settlement = prepared
	} else {
		settlement.Content = fmt.Sprintf("操作 %s", task.Action)
		if common.StringsContains(constant.TaskPricePatches, info.OriginModelName) {
			settlement.Content += "，按次计费"
		}
	}
	settlement.Other["is_task"] = true
	settlement.Other["request_path"] = info.RequestURLPath
	if _, exists := settlement.Other["model_price"]; !exists {
		settlement.Other["model_price"] = info.PriceData.ModelPrice
	}
	if _, exists := settlement.Other["model_ratio"]; !exists && info.PriceData.ModelRatio > 0 {
		settlement.Other["model_ratio"] = info.PriceData.ModelRatio
	}
	if _, exists := settlement.Other["group_ratio"]; !exists {
		settlement.Other["group_ratio"] = info.PriceData.GroupRatioInfo.GroupRatio
	}
	if _, exists := settlement.Other["user_group_ratio"]; !exists && info.PriceData.GroupRatioInfo.HasSpecialRatio {
		settlement.Other["user_group_ratio"] = info.PriceData.GroupRatioInfo.GroupSpecialRatio
	}
	settlement.Other["task_id"] = task.TaskID
	applied, err := service.ApplyImageTaskSettlementAtomic(ctx, task, settlement)
	if err != nil {
		return settlement.ActualQuota, err
	}
	if !applied {
		return settlement.ActualQuota, fmt.Errorf("image task settlement was not applied")
	}
	if err := service.DispatchPendingImageTaskSettlementLogs(ctx, 10); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("image task %s billing log dispatch deferred: %s", task.TaskID, err.Error()))
	}
	return settlement.ActualQuota, nil
}

func imageTaskRelayStartTime(task *model.Task) time.Time {
	now := time.Now()
	if task == nil {
		return now
	}
	for _, ts := range []int64{task.StartTime, task.SubmitTime, task.CreatedAt} {
		if ts <= 0 {
			continue
		}
		startTime := time.Unix(ts, 0)
		if startTime.After(now.Add(time.Minute)) {
			continue
		}
		return startTime
	}
	return now
}

func imageTaskChannelMetaFromTask(ctx context.Context, task *model.Task) *relaycommon.ChannelMeta {
	originModel := imageTaskModelName(task)
	upstreamModel := strings.TrimSpace(task.Properties.UpstreamModelName)
	if upstreamModel == "" {
		upstreamModel = originModel
	}
	meta := &relaycommon.ChannelMeta{
		ChannelId:         task.ChannelId,
		ApiKey:            imageTaskFixedUpstreamKey(task, ""),
		UpstreamModelName: upstreamModel,
		IsModelMapped:     upstreamModel != "" && originModel != "" && upstreamModel != originModel,
	}
	channel, err := model.CacheGetChannel(task.ChannelId)
	if err != nil || channel == nil {
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("image task %s load channel meta failed: %s", task.TaskID, err.Error()))
		}
		return meta
	}
	apiType, _ := common.ChannelType2APIType(channel.Type)
	meta.ChannelType = channel.Type
	meta.ChannelId = channel.Id
	meta.ChannelIsMultiKey = channel.ChannelInfo.IsMultiKey
	meta.ChannelBaseUrl = channel.GetBaseURL()
	meta.ApiType = apiType
	meta.ApiVersion = imageTaskChannelAPIVersion(channel)
	meta.ApiKey = imageTaskFixedUpstreamKey(task, channel.Key)
	meta.ChannelCreateTime = channel.CreatedTime
	meta.ParamOverride = channel.GetParamOverride()
	meta.HeadersOverride = channel.GetHeaderOverride()
	meta.ChannelSetting = channel.GetSetting()
	meta.ChannelOtherSettings = channel.GetOtherSettings()
	if channel.OpenAIOrganization != nil {
		meta.Organization = strings.TrimSpace(*channel.OpenAIOrganization)
	}
	return meta
}

func imageTaskChannelAPIVersion(channel *model.Channel) string {
	if channel == nil {
		return ""
	}
	switch channel.Type {
	case constant.ChannelTypeAzure, constant.ChannelTypeVertexAi, constant.ChannelTypeXunfei, constant.ChannelTypeGemini:
		return channel.Other
	default:
		return ""
	}
}

func imageTaskUsageFromResult(result json.RawMessage) (*dto.Usage, bool) {
	if len(result) == 0 {
		return nil, false
	}
	var simple dto.SimpleResponse
	if err := common.Unmarshal(result, &simple); err == nil {
		normalizeImageTaskUsage(&simple.Usage)
		if service.ValidUsage(&simple.Usage) {
			return &simple.Usage, true
		}
	}

	var raw any
	if err := common.Unmarshal(result, &raw); err != nil {
		return nil, false
	}
	usageValue := findImageTaskUsageValue(raw)
	if usageValue == nil {
		return nil, false
	}
	usageBytes, err := common.Marshal(usageValue)
	if err != nil {
		return nil, false
	}
	var usage dto.Usage
	if err := common.Unmarshal(usageBytes, &usage); err != nil {
		return nil, false
	}
	normalizeImageTaskUsage(&usage)
	if !service.ValidUsage(&usage) {
		return nil, false
	}
	return &usage, true
}

func cloneImageTaskUsage(usage *dto.Usage) *dto.Usage {
	if usage == nil {
		return nil
	}
	cloned := *usage
	if usage.InputTokensDetails != nil {
		details := *usage.InputTokensDetails
		cloned.InputTokensDetails = &details
	}
	return &cloned
}

func findImageTaskUsageValue(raw any) any {
	switch value := raw.(type) {
	case map[string]any:
		if usage, ok := value["usage"]; ok && usage != nil {
			return usage
		}
		for _, key := range []string{"result", "response", "openai_response", "output", "data"} {
			if nested, ok := value[key]; ok {
				if usage := findImageTaskUsageValue(nested); usage != nil {
					return usage
				}
			}
		}
	case []any:
		for _, item := range value {
			if usage := findImageTaskUsageValue(item); usage != nil {
				return usage
			}
		}
	}
	return nil
}

func normalizeImageTaskUsage(usage *dto.Usage) {
	if usage == nil {
		return
	}
	if usage.InputTokens != 0 {
		usage.PromptTokens = usage.InputTokens
	}
	if usage.OutputTokens != 0 {
		usage.CompletionTokens = usage.OutputTokens
	}
	if usage.InputTokensDetails != nil {
		usage.PromptTokensDetails.CachedTokens = usage.InputTokensDetails.CachedTokens
		usage.PromptTokensDetails.CachedCreationTokens = usage.InputTokensDetails.CachedCreationTokens
		usage.PromptTokensDetails.ImageTokens = usage.InputTokensDetails.ImageTokens
		usage.PromptTokensDetails.TextTokens = usage.InputTokensDetails.TextTokens
		usage.PromptTokensDetails.AudioTokens = usage.InputTokensDetails.AudioTokens
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
}

func imageTaskRelayModeFromTask(task *model.Task) int {
	if task.Action == constant.TaskActionImageEdit {
		return relayconstant.RelayModeImagesEdits
	}
	return relayconstant.RelayModeImagesGenerations
}

func imageTaskRequestPathFromTask(task *model.Task) string {
	if task.PrivateData.RequestPath != "" {
		return task.PrivateData.RequestPath
	}
	if task.Action == constant.TaskActionImageEdit {
		return "/v1/images/edits"
	}
	return "/v1/images/generations"
}

func imageTaskModelName(task *model.Task) string {
	if task.PrivateData.BillingContext != nil && task.PrivateData.BillingContext.OriginModelName != "" {
		return task.PrivateData.BillingContext.OriginModelName
	}
	return task.Properties.OriginModelName
}
