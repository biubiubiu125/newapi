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
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const imageTaskSyncTimeout = 120 * time.Second
const imageTaskStoredResultMarker = "_newapi_result_file"
const imageTaskUncertainSubmissionRetryCooldown = 30 * time.Second
const imageTaskUncertainSubmissionMaxAttempts = 3
const imageTaskLargeStorageReadThreshold = 8 << 20
const imageTaskLargeStorageReadConcurrency = 4
const imageTaskInlineB64ResultMaxBytes = 1 << 20
const imageTaskBatchPollMaxIDs = 100

var errImageTaskHTTPResponseTooLarge = errors.New("image task upstream response too large")
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
	batch := make([]*model.Task, 0, imageTaskBatchPollSize())
	flushBatch := func() {
		if len(batch) == 0 {
			return
		}
		if err := runGPTImage2APIAsyncImageTaskBatch(ctx, batch); err != nil {
			logger.LogError(ctx, fmt.Sprintf("image task batch run failed: %s", err.Error()))
		}
		batch = batch[:0]
	}
	for _, task := range tasks {
		if ctx.Err() != nil {
			flushBatch()
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
		if imageTaskShouldFailUnstarted(task) {
			flushBatch()
			reason := fmt.Sprintf("image task not started before timeout (%s)", imageTaskAsyncTimeoutText())
			if err := failImageTask(ctx, task, task.Status, reason, true, true); err != nil {
				logger.LogError(ctx, fmt.Sprintf("image task %s unstarted timeout cleanup failed: %s", task.TaskID, err.Error()))
			}
			continue
		}
		if imageTaskShouldFailStaleExecution(task, mode) {
			flushBatch()
			reason := fmt.Sprintf("image task execution timeout (%s)", imageTaskAsyncTimeoutText())
			if err := failImageTask(ctx, task, model.TaskStatusInProgress, reason, true, true); err != nil {
				logger.LogError(ctx, fmt.Sprintf("image task %s timeout cleanup failed: %s", task.TaskID, err.Error()))
			}
			continue
		}
		if imageTaskCanBatchPollGPTImage2API(task, mode) {
			batch = append(batch, task)
			if len(batch) >= imageTaskBatchPollSize() {
				flushBatch()
			}
			continue
		}
		flushBatch()
		var err error
		switch mode {
		case dto.ImageTaskModeGPTImage2APIAsync:
			err = runGPTImage2APIAsyncImageTask(ctx, task)
		default:
			err = runSyncWrapperImageTask(ctx, task)
		}
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("image task %s run failed: %s", task.TaskID, err.Error()))
		}
	}
	flushBatch()
	return nil
}

func imageTaskNeedsSettlement(task *model.Task) bool {
	return task != nil &&
		task.Status == model.TaskStatusSuccess &&
		(task.SettlementStatus == model.TaskSettlementStatusPending ||
			task.SettlementStatus == model.TaskSettlementStatusApplied)
}

func imageTaskIsDone(task *model.Task) bool {
	return task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure
}

func imageTaskShouldFailStaleExecution(task *model.Task, mode string) bool {
	if task == nil || task.Status != model.TaskStatusInProgress || task.StartTime == 0 {
		return false
	}
	if mode == dto.ImageTaskModeGPTImage2APIAsync {
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

	if err := setupImageTaskGinContext(fakeCtx, task); err != nil {
		return nil, err
	}

	imageRequest, err := helper.GetAndValidOpenAIImageRequest(fakeCtx, relayMode)
	if err != nil {
		return nil, err
	}
	imageRequest.Stream = false

	relayInfo, err := buildImageTaskRelayInfo(fakeCtx, task, imageRequest, relayMode, bodyStorage, contentType)
	if err != nil {
		return nil, err
	}
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

func setupImageTaskFixedChannelContext(c *gin.Context, task *model.Task, channel *model.Channel, selectedKey string) error {
	if channel == nil {
		return errors.New("channel is nil")
	}
	modelName := imageTaskModelName(task)
	c.Set("original_model", modelName)
	common.SetContextKey(c, constant.ContextKeyChannelId, channel.Id)
	common.SetContextKey(c, constant.ContextKeyChannelName, channel.Name)
	common.SetContextKey(c, constant.ContextKeyChannelType, channel.Type)
	common.SetContextKey(c, constant.ContextKeyChannelCreateTime, channel.CreatedTime)
	common.SetContextKey(c, constant.ContextKeyChannelSetting, channel.GetSetting())
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, channel.GetOtherSettings())
	paramOverride := channel.GetParamOverride()
	headerOverride := channel.GetHeaderOverride()
	if mergedParam, applied := service.ApplyChannelAffinityOverrideTemplate(c, paramOverride); applied {
		paramOverride = mergedParam
	}
	common.SetContextKey(c, constant.ContextKeyChannelParamOverride, paramOverride)
	common.SetContextKey(c, constant.ContextKeyChannelHeaderOverride, headerOverride)
	if channel.OpenAIOrganization != nil && *channel.OpenAIOrganization != "" {
		common.SetContextKey(c, constant.ContextKeyChannelOrganization, *channel.OpenAIOrganization)
	}
	common.SetContextKey(c, constant.ContextKeyChannelAutoBan, channel.GetAutoBan())
	common.SetContextKey(c, constant.ContextKeyChannelModelMapping, channel.GetModelMapping())
	common.SetContextKey(c, constant.ContextKeyChannelStatusCodeMapping, channel.GetStatusCodeMapping())
	common.SetContextKey(c, constant.ContextKeyChannelIsMultiKey, channel.ChannelInfo.IsMultiKey)
	common.SetContextKey(c, constant.ContextKeyChannelKey, imageTaskFixedUpstreamKey(task, selectedKey))
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, channel.GetBaseURL())
	common.SetContextKey(c, constant.ContextKeySystemPromptOverride, false)

	switch channel.Type {
	case constant.ChannelTypeAzure:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeVertexAi:
		c.Set("region", channel.Other)
	case constant.ChannelTypeXunfei:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeGemini:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeAli:
		c.Set("plugin", channel.Other)
	case constant.ChannelCloudflare:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeMokaAI:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeCoze:
		c.Set("bot_id", channel.Other)
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
		UserQuota:             common.GetContextKeyInt(c, constant.ContextKeyUserQuota),
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
	dst := &billingexpr.RequestInput{
		Headers: cloneImageTaskStringMap(src.Headers),
	}
	if len(src.Body) > 0 {
		dst.Body = append([]byte(nil), src.Body...)
	}
	return dst
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
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "authorization", "proxy-authorization", "x-api-key", "api-key", "cookie", "set-cookie":
		return true
	default:
		return false
	}
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

func runGPTImage2APIAsyncImageTask(ctx context.Context, task *model.Task) error {
	if imageTaskNeedsSettlement(task) {
		return settleGPTImage2APIAsyncImageTask(ctx, task, nil, nil)
	}
	if task.PrivateData.UpstreamTaskID == "" {
		if imageTaskShouldRecoverPendingAsyncSubmission(task) {
			return recoverGPTImage2APIAsyncSubmission(ctx, task)
		}
		return submitGPTImage2APIAsyncImageTask(ctx, task)
	}
	return pollGPTImage2APIAsyncImageTask(ctx, task)
}

type gptImage2APIBatchPollItem struct {
	task           *model.Task
	channel        *model.Channel
	key            string
	headerOverride map[string]string
}

type gptImage2APIBatchPollGroup struct {
	channel        *model.Channel
	key            string
	headerOverride map[string]string
	items          []gptImage2APIBatchPollItem
}

func imageTaskCanBatchPollGPTImage2API(task *model.Task, mode string) bool {
	if imageTaskBatchPollSize() <= 1 {
		return false
	}
	if task == nil || mode != dto.ImageTaskModeGPTImage2APIAsync {
		return false
	}
	if strings.TrimSpace(task.PrivateData.UpstreamTaskID) == "" || imageTaskNeedsSettlement(task) {
		return false
	}
	switch task.Status {
	case model.TaskStatusQueued, model.TaskStatusSubmitted, model.TaskStatusInProgress:
		return true
	default:
		return false
	}
}

func imageTaskBatchPollSize() int {
	size := constant.ImageTaskBatchPollSize
	if size <= 0 {
		size = 20
	}
	if size > imageTaskBatchPollMaxIDs {
		return imageTaskBatchPollMaxIDs
	}
	return size
}

func runGPTImage2APIAsyncImageTaskBatch(ctx context.Context, tasks []*model.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	groups := make(map[string]*gptImage2APIBatchPollGroup)
	for _, task := range tasks {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if task == nil {
			continue
		}
		item, key, err := prepareGPTImage2APIBatchPollItem(ctx, task)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("image task %s batch poll setup failed: %s", task.TaskID, err.Error()))
			continue
		}
		group := groups[key]
		if group == nil {
			group = &gptImage2APIBatchPollGroup{
				channel:        item.channel,
				key:            item.key,
				headerOverride: item.headerOverride,
			}
			groups[key] = group
		}
		group.items = append(group.items, item)
	}
	for _, group := range groups {
		for _, items := range chunkGPTImage2APIBatchPollItems(group.items, imageTaskBatchPollSize()) {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			groupCopy := *group
			groupCopy.items = items
			if err := pollGPTImage2APIAsyncImageTaskStatusBatch(ctx, &groupCopy); err != nil {
				logger.LogError(ctx, fmt.Sprintf("image task batch status poll failed: %s", err.Error()))
			}
		}
	}
	return nil
}

func prepareGPTImage2APIBatchPollItem(ctx context.Context, task *model.Task) (gptImage2APIBatchPollItem, string, error) {
	var empty gptImage2APIBatchPollItem
	channel, key, err := imageTaskChannelAndKey(task)
	if err != nil {
		return empty, "", handleGPTImage2APITransientError(ctx, task, "upstream poll task setup failed", err.Error())
	}
	task.PrivateData.Key = key
	headerOverride, err := buildGPTImage2APIAsyncStatusHeaderOverride(ctx, task, channel, key)
	if err != nil {
		return empty, "", handleGPTImage2APITransientError(ctx, task, "upstream poll task header override failed", err.Error())
	}
	item := gptImage2APIBatchPollItem{
		task:           task,
		channel:        channel,
		key:            key,
		headerOverride: headerOverride,
	}
	return item, gptImage2APIBatchPollKey(channel, key, headerOverride), nil
}

func gptImage2APIBatchPollKey(channel *model.Channel, key string, headerOverride map[string]string) string {
	if channel == nil {
		return ""
	}
	headerSignature, _ := json.Marshal(headerOverride)
	return strings.Join([]string{
		fmt.Sprintf("%d", channel.Id),
		channel.GetBaseURL(),
		channel.GetSetting().Proxy,
		strings.TrimSpace(key),
		string(headerSignature),
	}, "\x00")
}

func chunkGPTImage2APIBatchPollItems(items []gptImage2APIBatchPollItem, size int) [][]gptImage2APIBatchPollItem {
	if size <= 0 || len(items) <= size {
		return [][]gptImage2APIBatchPollItem{items}
	}
	chunks := make([][]gptImage2APIBatchPollItem, 0, (len(items)+size-1)/size)
	for len(items) > 0 {
		end := size
		if end > len(items) {
			end = len(items)
		}
		chunks = append(chunks, items[:end])
		items = items[end:]
	}
	return chunks
}

func pollGPTImage2APIAsyncImageTaskStatusBatch(ctx context.Context, group *gptImage2APIBatchPollGroup) error {
	if group == nil || len(group.items) == 0 || group.channel == nil {
		return nil
	}
	ids := make([]string, 0, len(group.items))
	for _, item := range group.items {
		if item.task == nil {
			continue
		}
		upstreamID := strings.TrimSpace(item.task.PrivateData.UpstreamTaskID)
		if upstreamID != "" {
			ids = append(ids, upstreamID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	query := url.Values{}
	query.Set("ids", strings.Join(ids, ","))
	query.Set("include_image_data", "false")
	endpoint := buildGPTImage2APIURL(group.channel.GetBaseURL(), "/api/image-tasks") + "?" + query.Encode()
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		for _, item := range group.items {
			if item.task != nil {
				_ = handleGPTImage2APITransientError(ctx, item.task, "upstream poll task request build failed", err.Error())
			}
		}
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+group.key)
	relaychannel.ApplyHeaderOverrideToRequest(req, group.headerOverride)

	respBody, statusCode, err := doImageTaskHTTPRequest(req, group.channel)
	if err != nil {
		for _, item := range group.items {
			if item.task != nil {
				_ = handleGPTImage2APITransientError(ctx, item.task, "upstream poll task request failed", err.Error())
			}
		}
		return err
	}
	if statusCode < 200 || statusCode >= 300 {
		reason := fmt.Sprintf("upstream poll task failed: status=%d body=%s", statusCode, common.LocalLogPreview(string(respBody)))
		for _, item := range group.items {
			if item.task == nil {
				continue
			}
			if gptImage2APIPollShouldRetry(statusCode) {
				_ = handleGPTImage2APITransientError(ctx, item.task, "upstream poll task transient status", reason)
				continue
			}
			_ = failImageTask(ctx, item.task, item.task.Status, reason, true, true)
		}
		return errors.New(reason)
	}

	for _, item := range group.items {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if item.task == nil {
			continue
		}
		result, err := parseGPTImage2APITaskResult(respBody, item.task.PrivateData.UpstreamTaskID)
		if err != nil {
			_ = handleGPTImage2APIInvalidPollResult(ctx, item.task, respBody, err)
			continue
		}
		if result == nil {
			_ = handleGPTImage2APIMissingPollResult(ctx, item.task, respBody, "upstream poll task returned no matching task")
			continue
		}
		if result.Status == "" {
			_ = handleGPTImage2APIMissingPollResult(ctx, item.task, respBody, "upstream poll task returned unknown task status")
			continue
		}
		if err := applyGPTImage2APIAsyncStatusOnly(ctx, item.task, result, item.key); err != nil {
			logger.LogError(ctx, fmt.Sprintf("image task %s batch status apply failed: %s", item.task.TaskID, err.Error()))
		}
	}
	return nil
}

func applyGPTImage2APIAsyncStatusOnly(ctx context.Context, task *model.Task, result *gptImage2APITaskResult, key string) error {
	if task == nil || result == nil {
		return nil
	}
	if result.Status == model.TaskStatusSuccess {
		return pollGPTImage2APIAsyncImageTask(ctx, task)
	}
	snap := task.Snapshot()
	now := time.Now().Unix()
	task.PrivateData.Key = key
	task.Status = result.Status
	task.RetryCount = 0
	if result.Progress != "" {
		task.Progress = result.Progress
	}
	var bodyPath string
	switch result.Status {
	case model.TaskStatusQueued, model.TaskStatusSubmitted:
		if task.Progress == "" {
			task.Progress = "0%"
		}
	case model.TaskStatusInProgress:
		if task.Progress == "" || task.Progress == "0%" {
			task.Progress = "1%"
		}
		if task.StartTime == 0 {
			task.StartTime = now
		}
	case model.TaskStatusFailure:
		task.Progress = "100%"
		task.FinishTime = now
		task.NextPollAt = 0
		task.LockOwner = ""
		task.LockUntil = 0
		task.SettlementStatus = ""
		clearImageTaskUpstreamSubmissionUncertainty(task)
		task.FailReason = result.Reason
		if task.FailReason == "" {
			task.FailReason = "upstream task failed"
		}
		bodyPath = takeImageTaskBodyPath(task)
	default:
		return nil
	}
	if imageTaskShouldFailLongRunningUpstreamStatus(task) {
		reason := fmt.Sprintf("upstream image task execution timeout (%s)", imageTaskAsyncTimeoutText())
		return failImageTask(ctx, task, snap.Status, reason, true, true)
	}
	won, err := updateImageTaskWithStatus(ctx, task, snap.Status)
	if err != nil || !won {
		return err
	}
	if task.Status == model.TaskStatusFailure {
		if task.Quota != 0 {
			service.RefundTaskQuota(ctx, task, task.FailReason)
		}
		removeImageTaskBodyPath(bodyPath)
	}
	return nil
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

func recoverGPTImage2APIAsyncSubmission(ctx context.Context, task *model.Task) error {
	recovered, err := recoverGPTImage2APIAsyncTaskByClientTaskID(ctx, task)
	if err != nil {
		return handleGPTImage2APITransientError(ctx, task, "upstream recover task failed", err.Error())
	}
	if !recovered {
		if !imageTaskShouldFailLongRunningUpstreamStatus(task) {
			if !imageTaskCanResubmitUncertainSubmission(task, time.Now().Unix()) {
				markImageTaskTransientRetry(task)
				return nil
			}
			return submitGPTImage2APIAsyncImageTask(ctx, task)
		}
		reason := fmt.Sprintf("image task execution timeout (%s)", imageTaskAsyncTimeoutText())
		return failImageTask(ctx, task, task.Status, reason, true, true)
	}
	return pollGPTImage2APIAsyncImageTask(ctx, task)
}

func submitGPTImage2APIAsyncImageTask(ctx context.Context, task *model.Task) error {
	if !gptImage2APIAsyncCanSubmit(task) {
		return nil
	}
	oldStatus := task.Status
	channel, key, err := imageTaskChannelAndKey(task)
	if err != nil {
		return failImageTask(ctx, task, oldStatus, err.Error(), true, true)
	}
	now := time.Now().Unix()
	task.Status = model.TaskStatusInProgress
	task.Progress = "1%"
	task.PrivateData.Key = key
	if task.StartTime == 0 {
		task.StartTime = now
	}
	won, err := updateImageTaskWithStatus(ctx, task, oldStatus)
	if err != nil || !won {
		return err
	}

	bodyStorage, contentType, err := openImageTaskBodyStorage(task)
	if err != nil {
		return failImageTask(ctx, task, model.TaskStatusInProgress, err.Error(), true, true)
	}
	defer bodyStorage.Close()
	outboundBody, headerOverride, err := buildGPTImage2APIAsyncCreateBody(ctx, task, channel, key, bodyStorage, contentType)
	if err != nil {
		_ = bodyStorage.Close()
		return failImageTask(ctx, task, model.TaskStatusInProgress, err.Error(), true, true)
	}
	defer outboundBody.Close()

	endpoint := buildGPTImage2APIURL(channel.GetBaseURL(), gptImage2APICreatePath(task))
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, outboundBody.Reader)
	if err != nil {
		outboundBody.Close()
		_ = bodyStorage.Close()
		return failImageTask(ctx, task, model.TaskStatusInProgress, err.Error(), true, true)
	}
	req.Header.Set("Content-Type", outboundBody.ContentType)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	relaychannel.ApplyHeaderOverrideToRequest(req, headerOverride)
	if outboundBody.ContentLength > 0 {
		req.ContentLength = outboundBody.ContentLength
	}

	respBody, statusCode, err := doImageTaskHTTPRequest(req, channel)
	if err != nil {
		return keepGPTImage2APIAsyncSubmissionInProgress(ctx, task, err.Error())
	}
	if gptImage2APISubmissionShouldRecover(statusCode) {
		return keepGPTImage2APIAsyncSubmissionInProgress(ctx, task, fmt.Sprintf("upstream create task status=%d body=%s", statusCode, common.LocalLogPreview(string(respBody))))
	}
	if statusCode < 200 || statusCode >= 300 {
		outboundBody.Close()
		_ = bodyStorage.Close()
		return failImageTask(ctx, task, model.TaskStatusInProgress, fmt.Sprintf("upstream create task failed: status=%d body=%s", statusCode, common.LocalLogPreview(string(respBody))), true, true)
	}
	upstreamID := extractGPTImage2APITaskID(respBody)
	if upstreamID == "" {
		return keepGPTImage2APIAsyncSubmissionInProgress(ctx, task, "upstream create task response missing task_id")
	}

	return saveGPTImage2APIAsyncSubmission(ctx, task, upstreamID)
}

func gptImage2APIAsyncCanSubmit(task *model.Task) bool {
	if task == nil || strings.TrimSpace(task.PrivateData.UpstreamTaskID) != "" {
		return false
	}
	switch task.Status {
	case model.TaskStatusNotStart, model.TaskStatusQueued, model.TaskStatusSubmitted:
		return true
	case model.TaskStatusInProgress:
		return task.StartTime > 0
	default:
		return false
	}
}

func saveGPTImage2APIAsyncSubmission(ctx context.Context, task *model.Task, upstreamID string) error {
	task.PrivateData.UpstreamTaskID = upstreamID
	clearImageTaskUpstreamSubmissionUncertainty(task)
	task.Status = model.TaskStatusSubmitted
	task.Progress = "0%"
	task.RetryCount = 0
	won, err := updateImageTaskWithStatus(ctx, task, model.TaskStatusInProgress)
	if err != nil || won {
		return err
	}

	logger.LogWarn(ctx, fmt.Sprintf("image task %s upstream id write lost CAS, reloading task state", task.TaskID))
	current, exist, err := model.GetByTaskId(task.UserId, task.TaskID)
	if err != nil {
		return err
	}
	if !exist || current == nil {
		logger.LogWarn(ctx, fmt.Sprintf("image task %s disappeared after upstream submission, upstream task may need manual check: %s", task.TaskID, upstreamID))
		return nil
	}
	if imageTaskIsDone(current) {
		logger.LogWarn(ctx, fmt.Sprintf("image task %s already finished after upstream submission, upstream task may need manual check: %s", task.TaskID, upstreamID))
		return nil
	}
	if current.PrivateData.UpstreamTaskID != "" {
		return nil
	}

	fromStatus := current.Status
	current.PrivateData.Key = imageTaskFixedUpstreamKey(current, task.PrivateData.Key)
	current.PrivateData.UpstreamTaskID = upstreamID
	clearImageTaskUpstreamSubmissionUncertainty(current)
	current.Status = model.TaskStatusSubmitted
	current.Progress = "0%"
	current.RetryCount = 0
	won, err = updateImageTaskWithStatus(ctx, current, fromStatus)
	if err != nil {
		return err
	}
	if !won {
		logger.LogWarn(ctx, fmt.Sprintf("image task %s upstream id write lost CAS after reload, will recover by local task_id", task.TaskID))
	}
	return nil
}

func keepGPTImage2APIAsyncSubmissionInProgress(ctx context.Context, task *model.Task, reason string) error {
	if task == nil {
		return nil
	}
	markImageTaskTransientRetry(task)
	markImageTaskUpstreamSubmissionUncertain(task)
	if _, err := updateImageTaskWithStatus(ctx, task, task.Status); err != nil {
		return err
	}
	logger.LogWarn(ctx, fmt.Sprintf("image task %s upstream submission uncertain, will recover by local task_id: %s", task.TaskID, reason))
	return nil
}

func markImageTaskUpstreamSubmissionUncertain(task *model.Task) {
	if task == nil {
		return
	}
	task.PrivateData.UpstreamSubmitUncertainAt = time.Now().Unix()
	task.PrivateData.UpstreamSubmitUncertainCount++
}

func clearImageTaskUpstreamSubmissionUncertainty(task *model.Task) {
	if task == nil {
		return
	}
	task.PrivateData.UpstreamSubmitUncertainAt = 0
	task.PrivateData.UpstreamSubmitUncertainCount = 0
}

func imageTaskCanResubmitUncertainSubmission(task *model.Task, now int64) bool {
	if task == nil {
		return false
	}
	uncertainAt := task.PrivateData.UpstreamSubmitUncertainAt
	uncertainCount := task.PrivateData.UpstreamSubmitUncertainCount
	if uncertainAt <= 0 || uncertainCount <= 0 {
		return true
	}
	if uncertainCount >= imageTaskUncertainSubmissionMaxAttempts {
		return false
	}
	return now-uncertainAt >= int64(imageTaskUncertainSubmissionRetryCooldown.Seconds())
}

func gptImage2APISubmissionShouldRecover(statusCode int) bool {
	return statusCode == http.StatusRequestTimeout || statusCode == http.StatusTooManyRequests || statusCode >= 500
}

func gptImage2APIPollShouldRetry(statusCode int) bool {
	return gptImage2APISubmissionShouldRecover(statusCode)
}

func recoverGPTImage2APIAsyncTaskByClientTaskID(ctx context.Context, task *model.Task) (bool, error) {
	channel, key, err := imageTaskChannelAndKey(task)
	if err != nil {
		return false, err
	}
	task.PrivateData.Key = key
	headerOverride, err := buildGPTImage2APIAsyncStatusHeaderOverride(ctx, task, channel, key)
	if err != nil {
		return false, err
	}
	endpoint := buildGPTImage2APIURL(channel.GetBaseURL(), "/api/image-tasks") + "?" + buildGPTImage2APIRecoverQuery(task)
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	relaychannel.ApplyHeaderOverrideToRequest(req, headerOverride)

	respBody, statusCode, err := doImageTaskHTTPRequest(req, channel)
	if err != nil {
		return false, err
	}
	if statusCode == http.StatusNotFound {
		return false, nil
	}
	if statusCode < 200 || statusCode >= 300 {
		return false, fmt.Errorf("upstream recover task failed: status=%d body=%s", statusCode, common.LocalLogPreview(string(respBody)))
	}
	result, err := parseGPTImage2APITaskResult(respBody, task.TaskID)
	if err != nil {
		return false, err
	}
	if result == nil {
		return false, nil
	}
	upstreamID := gptImage2APIRecoveredUpstreamID(result, respBody)
	if upstreamID == "" {
		return false, nil
	}

	snap := task.Snapshot()
	task.PrivateData.Key = key
	task.PrivateData.UpstreamTaskID = upstreamID
	clearImageTaskUpstreamSubmissionUncertainty(task)
	task.Status = model.TaskStatusSubmitted
	task.Progress = "0%"
	task.RetryCount = 0
	won, err := updateImageTaskWithStatus(ctx, task, snap.Status)
	if err != nil || !won {
		return false, err
	}
	return true, nil
}

func buildGPTImage2APIRecoverQuery(task *model.Task) string {
	query := url.Values{}
	if task != nil {
		taskID := strings.TrimSpace(task.TaskID)
		if taskID != "" {
			query.Set("ids", taskID)
			query.Set("client_task_id", taskID)
		}
	}
	query.Set("include_image_data", "false")
	return query.Encode()
}

func gptImage2APIRecoveredUpstreamID(result *gptImage2APITaskResult, respBody []byte) string {
	if result == nil {
		return ""
	}
	upstreamID := strings.TrimSpace(result.TaskID)
	if upstreamID == "" {
		upstreamID = extractGPTImage2APITaskID(respBody)
	}
	return strings.TrimSpace(upstreamID)
}

func pollGPTImage2APIAsyncImageTask(ctx context.Context, task *model.Task) error {
	channel, key, err := imageTaskChannelAndKey(task)
	if err != nil {
		return handleGPTImage2APITransientError(ctx, task, "upstream poll task setup failed", err.Error())
	}
	task.PrivateData.Key = key
	headerOverride, err := buildGPTImage2APIAsyncStatusHeaderOverride(ctx, task, channel, key)
	if err != nil {
		return handleGPTImage2APITransientError(ctx, task, "upstream poll task header override failed", err.Error())
	}
	query := url.Values{}
	query.Set("ids", task.PrivateData.UpstreamTaskID)
	query.Set("include_image_data", "true")
	endpoint := buildGPTImage2APIURL(channel.GetBaseURL(), "/api/image-tasks") + "?" + query.Encode()
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return handleGPTImage2APITransientError(ctx, task, "upstream poll task request build failed", err.Error())
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	relaychannel.ApplyHeaderOverrideToRequest(req, headerOverride)

	respStorage, statusCode, err := doImageTaskHTTPRequestStorage(req, channel)
	if respStorage != nil {
		defer respStorage.Close()
	}
	if err != nil {
		if errors.Is(err, errImageTaskHTTPResponseTooLarge) {
			if statusCode < 200 || statusCode >= 300 {
				reason := fmt.Sprintf("upstream poll task failed: status=%d body=%s", statusCode, err.Error())
				if gptImage2APIPollShouldRetry(statusCode) {
					return handleGPTImage2APITransientError(ctx, task, "upstream poll task transient status", reason)
				}
				return failImageTask(ctx, task, task.Status, reason, true, true)
			}
			return markImageTaskUpstreamResultReview(ctx, task, task.Status, err.Error())
		}
		return handleGPTImage2APITransientError(ctx, task, "upstream poll task request failed", err.Error())
	}
	respPreview := imageTaskHTTPResponseStoragePreview(respStorage)
	if statusCode < 200 || statusCode >= 300 {
		reason := fmt.Sprintf("upstream poll task failed: status=%d body=%s", statusCode, respPreview)
		if gptImage2APIPollShouldRetry(statusCode) {
			return handleGPTImage2APITransientError(ctx, task, "upstream poll task transient status", reason)
		}
		return failImageTask(ctx, task, task.Status, reason, true, true)
	}
	result, err := parseGPTImage2APITaskResultFromStorage(respStorage, task.PrivateData.UpstreamTaskID)
	if err != nil {
		return handleGPTImage2APIInvalidPollResult(ctx, task, []byte(respPreview), err)
	}
	if result == nil {
		return handleGPTImage2APIMissingPollResult(ctx, task, []byte(respPreview), "upstream poll task returned no matching task")
	}
	if result.Status == "" {
		return handleGPTImage2APIMissingPollResult(ctx, task, []byte(respPreview), "upstream poll task returned unknown task status")
	}

	snap := task.Snapshot()
	snapSettlementStatus := task.SettlementStatus
	snapNextPollAt := task.NextPollAt
	now := time.Now().Unix()
	var resultPath string
	task.PrivateData.Key = key
	task.Status = result.Status
	task.RetryCount = 0
	if result.Progress != "" {
		task.Progress = result.Progress
	}
	switch result.Status {
	case model.TaskStatusQueued, model.TaskStatusSubmitted:
		if task.Progress == "" {
			task.Progress = "0%"
		}
	case model.TaskStatusInProgress:
		if task.Progress == "" || task.Progress == "0%" {
			task.Progress = "1%"
		}
		if task.StartTime == 0 {
			task.StartTime = now
		}
	case model.TaskStatusSuccess:
		task.Progress = "100%"
		task.FinishTime = now
		task.NextPollAt = 0
		task.SettlementStatus = model.TaskSettlementStatusPending
		clearImageTaskUpstreamSubmissionUncertainty(task)
		resultPath, err = storeImageTaskResultData(task, result.Result, now)
		if err != nil {
			if errors.Is(err, common.ErrDiskCacheCapacityUnavailable) {
				task.Status = snap.Status
				task.Progress = snap.Progress
				task.StartTime = snap.StartTime
				task.FinishTime = snap.FinishTime
				task.FailReason = snap.FailReason
				task.PrivateData.ResultURL = snap.ResultURL
				task.Data = append(json.RawMessage(nil), snap.Data...)
				task.SettlementStatus = snapSettlementStatus
				task.NextPollAt = snapNextPollAt
				return handleGPTImage2APITransientError(ctx, task, "store image task result failed", err.Error())
			}
			return markImageTaskUpstreamResultReview(ctx, task, snap.Status, fmt.Sprintf("store image task result failed: %s", err.Error()))
		}
	case model.TaskStatusFailure:
		task.Progress = "100%"
		task.FinishTime = now
		task.NextPollAt = 0
		task.LockOwner = ""
		task.LockUntil = 0
		task.SettlementStatus = ""
		clearImageTaskUpstreamSubmissionUncertainty(task)
		task.FailReason = result.Reason
		if task.FailReason == "" {
			task.FailReason = "upstream task failed"
		}
	default:
		return nil
	}

	if imageTaskShouldFailLongRunningUpstreamStatus(task) {
		reason := fmt.Sprintf("upstream image task execution timeout (%s)", imageTaskAsyncTimeoutText())
		return failImageTask(ctx, task, snap.Status, reason, true, true)
	}

	var settlementBillingInput *billingexpr.RequestInput
	if task.Status == model.TaskStatusSuccess {
		var billingInputErr error
		settlementBillingInput, billingInputErr = imageTaskBillingRequestInputFromStoredBody(task)
		if billingInputErr != nil {
			logger.LogWarn(ctx, fmt.Sprintf("load image task %s billing request body failed: %s", task.TaskID, billingInputErr.Error()))
			if task.PrivateData.TieredBillingSnapshot != nil {
				reason := fmt.Sprintf("image task settlement billing request body unavailable: %s", billingInputErr.Error())
				return markImageTaskUpstreamResultReview(ctx, task, snap.Status, reason)
			}
		}
	}

	var bodyPath string
	if task.Status == model.TaskStatusFailure {
		bodyPath = takeImageTaskBodyPath(task)
	}
	won, err := updateImageTaskWithStatus(ctx, task, snap.Status)
	if err != nil || !won {
		removeImageTaskResultPath(resultPath)
		return err
	}
	if task.Status == model.TaskStatusSuccess {
		return settleGPTImage2APIAsyncImageTask(ctx, task, result.Result, settlementBillingInput)
	}
	if task.Status == model.TaskStatusFailure {
		if task.Quota != 0 {
			service.RefundTaskQuota(ctx, task, task.FailReason)
		}
		removeImageTaskBodyPath(bodyPath)
	}
	return nil
}

func settleGPTImage2APIAsyncImageTask(ctx context.Context, task *model.Task, result json.RawMessage, billingInput *billingexpr.RequestInput) error {
	return settleImageTaskSuccess(ctx, task, imageTaskSettlementPayload{
		Result:       result,
		BillingInput: billingInput,
	})
}

func settleImageTaskSuccess(ctx context.Context, task *model.Task, payload imageTaskSettlementPayload) error {
	if task == nil || task.Status != model.TaskStatusSuccess {
		return nil
	}
	if task.SettlementStatus == model.TaskSettlementStatusSettled {
		return nil
	}
	if task.SettlementStatus == model.TaskSettlementStatusReview {
		return nil
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
	if len(result) == 0 {
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
	if len(result) == 0 {
		reason := "image task success result is empty, cannot settle billing"
		if reviewErr := markImageTaskSettlementReview(ctx, task, reason); reviewErr != nil {
			return reviewErr
		}
		return errors.New(reason)
	}
	billingInput := payload.BillingInput
	if billingInput == nil {
		var billingInputErr error
		billingInput, billingInputErr = imageTaskBillingRequestInputFromStoredBody(task)
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
	if err := settleImageTaskConsumption(ctx, task, result, usage, extraContent, billingInput); err != nil {
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

	if err := markImageTaskSettlementApplicationApplied(ctx, task); err != nil {
		reason := fmt.Sprintf("image task settlement was applied but final marker failed, manual review is required: %s", err.Error())
		if reviewErr := model.MarkTaskSettlementApplicationReview(task.ID, reason); reviewErr != nil {
			markImageTaskTransientRetry(task)
			return fmt.Errorf("%s; mark review failed: %w", reason, reviewErr)
		}
		if reviewErr := markImageTaskSettlementReview(ctx, task, reason); reviewErr != nil {
			return reviewErr
		}
		return errors.New(reason)
	}

	return finalizeAppliedImageTaskSettlement(ctx, task)
}

func handleExistingImageTaskSettlementRecord(ctx context.Context, task *model.Task, settlementRecord *model.TaskSettlementRecord) (bool, error) {
	if task == nil || settlementRecord == nil {
		return false, nil
	}
	switch settlementRecord.Status {
	case model.TaskSettlementRecordStatusApplied:
		return true, finalizeAppliedImageTaskSettlement(ctx, task)
	case model.TaskSettlementRecordStatusReview:
		return true, markImageTaskSettlementReview(ctx, task, settlementRecord.Error)
	case model.TaskSettlementRecordStatusPrepared:
		return false, nil
	case model.TaskSettlementRecordStatusApplying:
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

func finalizeAppliedImageTaskSettlement(ctx context.Context, task *model.Task) error {
	if task == nil {
		return nil
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
		err = model.MarkTaskSettlementApplicationApplying(task.ID)
		if err == nil {
			return nil
		}
		if !sleepImageTaskSettlementUpdateRetry(ctx, attempt) {
			break
		}
	}
	return fmt.Errorf("mark image task settlement application applying: %w", err)
}

func markImageTaskSettlementApplicationApplied(ctx context.Context, task *model.Task) error {
	if task == nil {
		return nil
	}
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = model.MarkTaskSettlementApplicationApplied(task.ID)
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

func markImageTaskSettlementReview(ctx context.Context, task *model.Task, reason string) error {
	if task == nil {
		return nil
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "image task settlement requires manual review"
	}
	if task.SettlementStatus == model.TaskSettlementStatusReview {
		return nil
	}
	fromSettlementStatus := task.SettlementStatus
	if fromSettlementStatus == "" || fromSettlementStatus == model.TaskSettlementStatusSettled {
		return fmt.Errorf("image task settlement cannot mark review from status %s", fromSettlementStatus)
	}
	task.SettlementStatus = model.TaskSettlementStatusReview
	task.FailReason = reason
	task.NextPollAt = 0
	task.LockOwner = ""
	task.LockUntil = 0
	task.RetryCount = 0
	clearImageTaskUpstreamSubmissionUncertainty(task)
	task.PrivateData.SettlementUsage = nil
	task.PrivateData.SettlementExtraContent = nil
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
	task.PrivateData.SettlementUsage = nil
	task.PrivateData.SettlementExtraContent = nil
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
	task.SettlementStatus = model.TaskSettlementStatusSettled
	task.NextPollAt = 0
	task.LockOwner = ""
	task.LockUntil = 0
	task.RetryCount = 0
	clearImageTaskUpstreamSubmissionUncertainty(task)
	task.PrivateData.SettlementUsage = nil
	task.PrivateData.SettlementExtraContent = nil
	bodyPath := takeImageTaskBodyPath(task)
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
	removeImageTaskBodyPath(bodyPath)
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

func handleGPTImage2APITransientError(ctx context.Context, task *model.Task, reasonPrefix string, detail string) error {
	reason := fmt.Sprintf("%s: %s", reasonPrefix, detail)
	if !imageTaskShouldFailTransientUpstreamError(task) {
		markImageTaskTransientRetry(task)
		return errors.New(reason)
	}
	reason = fmt.Sprintf("%s after %s: %s", reasonPrefix, imageTaskAsyncTimeoutText(), detail)
	return failImageTask(ctx, task, task.Status, reason, true, true)
}

func markImageTaskTransientRetry(task *model.Task) {
	if task == nil {
		return
	}
	task.RetryCount++
}

func imageTaskShouldFailTransientUpstreamError(task *model.Task) bool {
	return imageTaskShouldFailLongRunningUpstreamStatus(task)
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

func handleGPTImage2APIMissingPollResult(ctx context.Context, task *model.Task, respBody []byte, reasonPrefix string) error {
	if !imageTaskShouldFailMissingUpstreamPollResult(task) {
		markImageTaskTransientRetry(task)
		return nil
	}
	reason := fmt.Sprintf("%s after %s: body=%s", reasonPrefix, imageTaskAsyncTimeoutText(), common.LocalLogPreview(string(respBody)))
	return failImageTask(ctx, task, task.Status, reason, true, true)
}

func imageTaskShouldFailMissingUpstreamPollResult(task *model.Task) bool {
	return imageTaskShouldFailLongRunningUpstreamStatus(task)
}

func handleGPTImage2APIInvalidPollResult(ctx context.Context, task *model.Task, respBody []byte, parseErr error) error {
	reasonPrefix := "upstream poll task returned invalid response"
	if parseErr != nil {
		reasonPrefix = fmt.Sprintf("%s: %s", reasonPrefix, parseErr.Error())
	}
	if !imageTaskShouldFailInvalidUpstreamPollResult(task) {
		markImageTaskTransientRetry(task)
		return errors.New(reasonPrefix)
	}
	reason := fmt.Sprintf("%s after %s: body=%s", reasonPrefix, imageTaskAsyncTimeoutText(), common.LocalLogPreview(string(respBody)))
	return failImageTask(ctx, task, task.Status, reason, true, true)
}

func imageTaskShouldFailInvalidUpstreamPollResult(task *model.Task) bool {
	return imageTaskShouldFailLongRunningUpstreamStatus(task)
}

type gptImage2APITaskResult struct {
	TaskID   string
	Status   model.TaskStatus
	Progress string
	Reason   string
	Result   json.RawMessage
}

func parseGPTImage2APITaskResult(body []byte, upstreamID string) (*gptImage2APITaskResult, error) {
	result, fallback, err := parseGPTImage2APITaskResultRawBytes(body, upstreamID)
	if err != nil || !fallback {
		return result, err
	}
	return parseGPTImage2APITaskResultAny(body, upstreamID)
}

func parseGPTImage2APITaskResultAny(body []byte, upstreamID string) (*gptImage2APITaskResult, error) {
	var raw any
	if err := common.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	return parseGPTImage2APITaskResultRaw(raw, upstreamID)
}

func parseGPTImage2APITaskResultFromStorage(storage common.BodyStorage, upstreamID string) (*gptImage2APITaskResult, error) {
	if storage == nil {
		return nil, errors.New("upstream task response body is missing")
	}
	body, err := readImageTaskBodyStorageBytes(storage)
	if err != nil {
		return nil, err
	}
	return parseGPTImage2APITaskResult(body, upstreamID)
}

func parseGPTImage2APITaskResultRawBytes(body []byte, upstreamID string) (*gptImage2APITaskResult, bool, error) {
	if !gjson.ValidBytes(body) {
		var raw any
		return nil, false, common.Unmarshal(body, &raw)
	}
	item := findGPTImage2APITaskItemJSON(gjson.ParseBytes(body), upstreamID)
	if !item.Exists() {
		return nil, true, nil
	}
	if !item.IsObject() {
		return nil, false, fmt.Errorf("upstream task item has invalid shape")
	}
	status := normalizeGPTImage2APIStatus(stringValueFromJSON(item, "status", "state"))
	result := &gptImage2APITaskResult{
		TaskID:   stringValueFromJSON(item, "task_id", "id", "upstream_task_id"),
		Status:   status,
		Progress: progressValueFromJSON(item),
		Reason:   stringValueFromJSON(item, "fail_reason", "reason", "message"),
	}
	if errValue := item.Get("error"); errValue.Exists() && result.Reason == "" {
		result.Reason = errorValueToStringJSON(errValue)
	}
	if status == model.TaskStatusSuccess {
		result.Result = marshalGPTImage2APISuccessResultJSON(item)
		if len(result.Result) == 0 {
			result.Result = rawMessageFromJSONResult(item)
		}
	}
	return result, false, nil
}

func findGPTImage2APITaskItemJSON(raw gjson.Result, upstreamID string) gjson.Result {
	if !raw.Exists() {
		return gjson.Result{}
	}
	if raw.IsArray() {
		var first gjson.Result
		count := 0
		var found gjson.Result
		raw.ForEach(func(_, item gjson.Result) bool {
			count++
			if count == 1 {
				first = item
			}
			if taskItemMatchesJSON(item, upstreamID) {
				found = item
				return false
			}
			return true
		})
		if found.Exists() {
			return found
		}
		if count == 1 && first.IsObject() &&
			(strings.TrimSpace(upstreamID) == "" || (stringValueFromJSON(first, "status", "state") != "" && !taskItemHasIdentityJSON(first))) {
			return first
		}
		return gjson.Result{}
	}
	if !raw.IsObject() {
		return gjson.Result{}
	}
	if taskItemMatchesJSON(raw, upstreamID) {
		return raw
	}
	for _, key := range []string{"items", "tasks", "results"} {
		if nested := raw.Get(key); nested.Exists() {
			if found := findGPTImage2APITaskItemJSON(nested, upstreamID); found.Exists() {
				return found
			}
		}
	}
	if upstreamID != "" {
		var direct gjson.Result
		raw.ForEach(func(key, value gjson.Result) bool {
			if key.String() == upstreamID {
				direct = value
				return false
			}
			return true
		})
		if direct.Exists() {
			if found := findGPTImage2APITaskItemJSON(direct, upstreamID); found.Exists() {
				return found
			}
			return direct
		}
	}
	if nested := raw.Get("data"); nested.Exists() {
		if found := findGPTImage2APITaskItemJSON(nested, upstreamID); found.Exists() {
			return found
		}
	}
	if stringValueFromJSON(raw, "status", "state") != "" {
		if strings.TrimSpace(upstreamID) != "" && taskItemHasIdentityJSON(raw) {
			return gjson.Result{}
		}
		if raw.Get("items").Exists() || raw.Get("tasks").Exists() || raw.Get("results").Exists() {
			return gjson.Result{}
		}
		return raw
	}
	var found gjson.Result
	raw.ForEach(func(_, value gjson.Result) bool {
		found = findGPTImage2APITaskItemJSON(value, upstreamID)
		return !found.Exists()
	})
	return found
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

func marshalGPTImage2APISuccessResultJSON(item gjson.Result) json.RawMessage {
	for _, key := range []string{"result", "response", "openai_response", "output"} {
		if value := item.Get(key); value.Exists() {
			return marshalGPTImage2APIJSONValueWithUsage(value, item.Get("usage"))
		}
	}
	data := item.Get("data")
	if !data.Exists() || strings.TrimSpace(data.Raw) == "null" {
		return nil
	}
	if !data.IsArray() {
		return rawMessageFromJSONResult(data)
	}
	if usage := item.Get("usage"); usage.Exists() && strings.TrimSpace(usage.Raw) != "null" {
		return joinJSONObjectRaw("data", data, "usage", usage)
	}
	return joinJSONObjectRaw("data", data, "", gjson.Result{})
}

func marshalGPTImage2APIJSONValueWithUsage(value gjson.Result, usage gjson.Result) json.RawMessage {
	if !usage.Exists() || strings.TrimSpace(usage.Raw) == "null" {
		return rawMessageFromJSONResult(value)
	}
	if value.IsObject() {
		if value.Get("usage").Exists() {
			return rawMessageFromJSONResult(value)
		}
		raw := bytes.TrimSpace([]byte(value.Raw))
		if len(raw) >= 2 && raw[0] == '{' && raw[len(raw)-1] == '}' {
			body := bytes.TrimSpace(raw[1 : len(raw)-1])
			out := make([]byte, 0, len(raw)+len(usage.Raw)+16)
			out = append(out, '{')
			if len(body) > 0 {
				out = append(out, body...)
				out = append(out, ',')
			}
			out = append(out, `"usage":`...)
			out = append(out, usage.Raw...)
			out = append(out, '}')
			return json.RawMessage(out)
		}
	}
	return joinJSONObjectRaw("data", value, "usage", usage)
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

func parseGPTImage2APITaskResultRaw(raw any, upstreamID string) (*gptImage2APITaskResult, error) {
	item := findGPTImage2APITaskItem(raw, upstreamID)
	if item == nil {
		return nil, nil
	}
	itemMap, ok := item.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("upstream task item has invalid shape")
	}
	status := normalizeGPTImage2APIStatus(stringValueFromMap(itemMap, "status", "state"))
	result := &gptImage2APITaskResult{
		TaskID:   stringValueFromMap(itemMap, "task_id", "id", "upstream_task_id"),
		Status:   status,
		Progress: progressValueFromMap(itemMap),
		Reason:   stringValueFromMap(itemMap, "fail_reason", "reason", "message"),
	}
	if errValue, ok := itemMap["error"]; ok && result.Reason == "" {
		result.Reason = errorValueToString(errValue)
	}
	if status == model.TaskStatusSuccess {
		result.Result = marshalGPTImage2APISuccessResult(itemMap)
		if len(result.Result) == 0 {
			result.Result, _ = common.Marshal(itemMap)
		}
	}
	return result, nil
}

func marshalGPTImage2APISuccessResult(itemMap map[string]any) json.RawMessage {
	if result, ok := firstExistingValue(itemMap, "result", "response", "openai_response", "output"); ok {
		return marshalGPTImage2APIValueWithUsage(result, itemMap["usage"])
	}
	data, ok := itemMap["data"]
	if !ok || data == nil {
		return nil
	}
	if _, ok := data.([]any); !ok {
		return marshalFirstExistingValue(itemMap, "data")
	}
	wrapped := map[string]any{
		"data": data,
	}
	if usage, ok := itemMap["usage"]; ok && usage != nil {
		wrapped["usage"] = usage
	}
	b, err := common.Marshal(wrapped)
	if err != nil {
		return nil
	}
	return json.RawMessage(b)
}

func marshalGPTImage2APIValueWithUsage(value any, usage any) json.RawMessage {
	if usage == nil {
		b, err := common.Marshal(value)
		if err != nil || len(b) == 0 || string(b) == "null" {
			return nil
		}
		return json.RawMessage(b)
	}
	if m, ok := value.(map[string]any); ok {
		if _, exists := m["usage"]; exists {
			return marshalFirstExistingValue(map[string]any{"value": value}, "value")
		}
		wrapped := make(map[string]any, len(m)+1)
		for key, item := range m {
			wrapped[key] = item
		}
		wrapped["usage"] = usage
		b, err := common.Marshal(wrapped)
		if err != nil {
			return nil
		}
		return json.RawMessage(b)
	}
	wrapped := map[string]any{
		"data":  value,
		"usage": usage,
	}
	b, err := common.Marshal(wrapped)
	if err != nil {
		return nil
	}
	return json.RawMessage(b)
}

func findGPTImage2APITaskItem(raw any, upstreamID string) any {
	switch v := raw.(type) {
	case []any:
		for _, item := range v {
			if taskItemMatches(item, upstreamID) {
				return item
			}
		}
		if len(v) == 1 {
			if item, ok := v[0].(map[string]any); ok &&
				(strings.TrimSpace(upstreamID) == "" || (stringValueFromMap(item, "status", "state") != "" && !taskItemHasIdentity(item))) {
				return v[0]
			}
		}
	case map[string]any:
		if taskItemMatches(v, upstreamID) {
			return v
		}
		for _, key := range []string{"items", "tasks", "results"} {
			if nested, ok := v[key]; ok {
				if found := findGPTImage2APITaskItem(nested, upstreamID); found != nil {
					return found
				}
			}
		}
		if upstreamID != "" {
			if direct, ok := v[upstreamID]; ok {
				if found := findGPTImage2APITaskItem(direct, upstreamID); found != nil {
					return found
				}
				return direct
			}
		}
		if nested, ok := v["data"]; ok {
			if found := findGPTImage2APITaskItem(nested, upstreamID); found != nil {
				return found
			}
		}
		if stringValueFromMap(v, "status", "state") != "" {
			if strings.TrimSpace(upstreamID) != "" && taskItemHasIdentity(v) {
				return nil
			}
			if _, hasItems := v["items"]; hasItems {
				return nil
			}
			if _, hasTasks := v["tasks"]; hasTasks {
				return nil
			}
			if _, hasResults := v["results"]; hasResults {
				return nil
			}
			return v
		}
		for _, nested := range v {
			if found := findGPTImage2APITaskItem(nested, upstreamID); found != nil {
				return found
			}
		}
	}
	return nil
}

func taskItemMatches(item any, upstreamID string) bool {
	if strings.TrimSpace(upstreamID) == "" {
		return false
	}
	m, ok := item.(map[string]any)
	if !ok {
		return false
	}
	for _, key := range []string{"task_id", "id", "upstream_task_id", "client_task_id"} {
		if stringValue(m[key]) == upstreamID {
			return true
		}
	}
	return false
}

func taskItemHasIdentity(m map[string]any) bool {
	for _, key := range []string{"task_id", "id", "upstream_task_id", "client_task_id"} {
		if stringValue(m[key]) != "" {
			return true
		}
	}
	return false
}

func normalizeGPTImage2APIStatus(status string) model.TaskStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "not_start", "pending", "queued", "queue":
		return model.TaskStatusQueued
	case "submitted":
		return model.TaskStatusSubmitted
	case "running", "processing", "in_progress", "progress":
		return model.TaskStatusInProgress
	case "success", "succeeded", "completed", "complete", "done":
		return model.TaskStatusSuccess
	case "failed", "failure", "error", "cancelled", "canceled":
		return model.TaskStatusFailure
	default:
		return ""
	}
}

func extractGPTImage2APITaskID(body []byte) string {
	var raw any
	if err := common.Unmarshal(body, &raw); err != nil {
		return ""
	}
	return extractTaskIDFromAny(raw)
}

func extractTaskIDFromAny(raw any) string {
	switch v := raw.(type) {
	case []any:
		for _, item := range v {
			if id := extractTaskIDFromAny(item); id != "" {
				return id
			}
		}
	case map[string]any:
		for _, key := range []string{"task_id", "id", "upstream_task_id"} {
			if id := stringValue(v[key]); id != "" {
				return id
			}
		}
		for _, key := range []string{"data", "result", "task"} {
			if nested, ok := v[key]; ok {
				if id := extractTaskIDFromAny(nested); id != "" {
					return id
				}
			}
		}
	}
	return ""
}

type imageTaskOutboundBody struct {
	Reader        io.Reader
	ContentType   string
	ContentLength int64
	cleanup       func()
}

func (b *imageTaskOutboundBody) Close() {
	if b == nil || b.cleanup == nil {
		return
	}
	b.cleanup()
	b.cleanup = nil
}

func newImageTaskBytesOutboundBody(body []byte, contentType string) *imageTaskOutboundBody {
	return &imageTaskOutboundBody{
		Reader:        bytes.NewReader(body),
		ContentType:   contentType,
		ContentLength: int64(len(body)),
	}
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

func takeImageTaskBodyPath(task *model.Task) string {
	if task == nil {
		return ""
	}
	path := task.PrivateData.RequestBodyPath
	task.PrivateData.RequestBodyPath = ""
	task.PrivateData.RequestBodyBase64 = ""
	task.PrivateData.RequestBodyPortable = false
	return path
}

func removeImageTaskBodyPath(path string) {
	if path == "" {
		return
	}
	_ = common.RemoveDiskCacheFile(path)
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

	data := []byte(result)
	offload, err := imageTaskResultStorageAction(data)
	if err != nil {
		return "", err
	}
	if !offload {
		task.Data = append(json.RawMessage(nil), result...)
		return "", nil
	}

	path, err := common.WriteImageTaskResultCacheFile(data)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	expiresAt := storedAt + int64(imageTaskResultRetention().Seconds())
	task.PrivateData.ResultBodyPath = path
	task.PrivateData.ResultBodySize = int64(len(data))
	task.PrivateData.ResultBodySHA256 = sha
	task.PrivateData.ResultContentType = "application/json"
	task.PrivateData.ResultStoredAt = storedAt
	task.PrivateData.ResultExpiresAt = expiresAt

	placeholder, err := common.Marshal(imageTaskStoredResultData{
		Stored:      true,
		Size:        int64(len(data)),
		SHA256:      sha,
		ContentType: "application/json",
		StoredAt:    storedAt,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		_ = common.RemoveDiskCacheFile(path)
		task.PrivateData.ResultBodyPath = ""
		task.PrivateData.ResultBodySize = 0
		task.PrivateData.ResultBodySHA256 = ""
		task.PrivateData.ResultContentType = ""
		task.PrivateData.ResultStoredAt = 0
		task.PrivateData.ResultExpiresAt = 0
		return "", err
	}
	task.Data = json.RawMessage(placeholder)
	return path, nil
}

func imageTaskResultShouldOffload(data []byte) bool {
	offload, _ := imageTaskResultStorageAction(data)
	return offload
}

func imageTaskResultStorageAction(data []byte) (bool, error) {
	if len(data) == 0 {
		return false, nil
	}
	if !imageTaskResultHasB64JSON(data) {
		return false, nil
	}
	if service.ImageTaskFileCacheSharedTrusted() {
		return true, nil
	}
	if len(data) <= imageTaskInlineB64ResultMaxBytes {
		return false, nil
	}
	return false, fmt.Errorf("image task b64_json result exceeds inline limit %d MB; enable trusted shared image task file cache", imageTaskInlineB64ResultMaxBytes>>20)
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

func imageTaskJSONValueHasB64JSON(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			if key == "b64_json" {
				return true
			}
			if imageTaskJSONValueHasB64JSON(nested) {
				return true
			}
		}
	case []any:
		for _, nested := range typed {
			if imageTaskJSONValueHasB64JSON(nested) {
				return true
			}
		}
	}
	return false
}

func takeImageTaskResultPath(task *model.Task) string {
	if task == nil || task.PrivateData.ResultBodyPath == "" {
		return ""
	}
	path := task.PrivateData.ResultBodyPath
	task.PrivateData.ResultBodyPath = ""
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
	var bodyPath string
	var resultPath string
	if cleanup {
		bodyPath = takeImageTaskBodyPath(task)
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
		service.RefundTaskQuota(ctx, task, reason)
	}
	if cleanup {
		removeImageTaskBodyPath(bodyPath)
		removeImageTaskResultPath(resultPath)
	}
	return nil
}

func imageTaskChannelAndKey(task *model.Task) (*model.Channel, string, error) {
	channel, err := model.CacheGetChannel(task.ChannelId)
	if err != nil {
		return nil, "", err
	}
	if key := imageTaskFixedUpstreamKey(task, ""); key != "" {
		return channel, key, nil
	}
	key, _, apiErr := channel.GetNextEnabledKey()
	if apiErr != nil {
		return nil, "", apiErr
	}
	return channel, strings.TrimSpace(key), nil
}

func imageTaskFixedUpstreamKey(task *model.Task, selectedKey string) string {
	if task != nil {
		if key := strings.TrimSpace(task.PrivateData.Key); key != "" {
			return key
		}
	}
	return strings.TrimSpace(selectedKey)
}

func doImageTaskHTTPRequest(req *http.Request, channel *model.Channel) ([]byte, int, error) {
	client, err := imageTaskHTTPClient(channel)
	if err != nil {
		return nil, 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := readImageTaskHTTPResponseBody(resp.Body)
	return body, resp.StatusCode, err
}

func doImageTaskHTTPRequestStorage(req *http.Request, channel *model.Channel) (common.BodyStorage, int, error) {
	client, err := imageTaskHTTPClient(channel)
	if err != nil {
		return nil, 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := readImageTaskHTTPResponseStorage(resp.Body, resp.ContentLength)
	return body, resp.StatusCode, err
}

func imageTaskHTTPClient(channel *model.Channel) (*http.Client, error) {
	proxy := ""
	if channel != nil {
		proxy = channel.GetSetting().Proxy
	}
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return http.DefaultClient, nil
	}
	return client, nil
}

func readImageTaskHTTPResponseBody(reader io.Reader) ([]byte, error) {
	maxBytes := imageTaskHTTPResponseMaxBytes()
	if maxBytes <= 0 {
		return io.ReadAll(reader)
	}
	body, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("%w: exceeds %d MB", errImageTaskHTTPResponseTooLarge, maxBytes>>20)
	}
	return body, nil
}

func readImageTaskHTTPResponseStorage(reader io.Reader, contentLength int64) (common.BodyStorage, error) {
	maxBytes := imageTaskHTTPResponseMaxBytes()
	if maxBytes <= 0 {
		body, err := io.ReadAll(reader)
		if err != nil {
			return nil, err
		}
		return common.CreateBodyStorage(body)
	}
	if contentLength > maxBytes {
		return nil, fmt.Errorf("%w: exceeds %d MB", errImageTaskHTTPResponseTooLarge, maxBytes>>20)
	}
	reserveBytes := imageTaskHTTPResponseInitialReserveBytes(contentLength, maxBytes)
	storage, err := common.CreateDiskBodyStorageFromReaderWithReservation(reader, maxBytes, reserveBytes)
	if err != nil {
		if errors.Is(err, common.ErrDiskCacheCapacityUnavailable) {
			return nil, err
		}
		if common.IsRequestBodyTooLargeError(err) {
			return nil, fmt.Errorf("%w: exceeds %d MB", errImageTaskHTTPResponseTooLarge, maxBytes>>20)
		}
		return nil, err
	}
	return storage, nil
}

func imageTaskHTTPResponseInitialReserveBytes(contentLength int64, maxBytes int64) int64 {
	if contentLength > 0 {
		return contentLength
	}
	reserveBytes := int64(1 << 20)
	if maxBytes > 0 && reserveBytes > maxBytes {
		return maxBytes
	}
	return reserveBytes
}

func imageTaskHTTPResponseStoragePreview(storage common.BodyStorage) string {
	if storage == nil {
		return ""
	}
	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		return ""
	}
	limit := int64(4096)
	if storage.Size() > 0 && storage.Size() < limit {
		limit = storage.Size()
	}
	body, _ := io.ReadAll(io.LimitReader(storage, limit))
	_, _ = storage.Seek(0, io.SeekStart)
	return common.LocalLogPreview(string(body))
}

func imageTaskHTTPResponseMaxBytes() int64 {
	if constant.ImageTaskHTTPResponseMaxMB > 0 {
		return int64(constant.ImageTaskHTTPResponseMaxMB) << 20
	}
	maxMB := constant.MaxFileDownloadMB
	if maxMB <= 0 {
		maxMB = 64
	}
	return int64(maxMB) << 20
}

func buildGPTImage2APIURL(baseURL string, path string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(baseURL, "/v1") {
		baseURL = strings.TrimSuffix(baseURL, "/v1")
	}
	return baseURL + path
}

func gptImage2APICreatePath(task *model.Task) string {
	if task.Action == constant.TaskActionImageEdit {
		return "/api/image-tasks/edits"
	}
	return "/api/image-tasks/generations"
}

func buildGPTImage2APIAsyncCreateBody(ctx context.Context, task *model.Task, channel *model.Channel, key string, bodyStorage common.BodyStorage, contentType string) (*imageTaskOutboundBody, map[string]string, error) {
	imageRequest, relayInfo, fakeCtx, cleanup, err := prepareGPTImage2APIAsyncRelayInfo(ctx, task, channel, key, bodyStorage, contentType)
	if err != nil {
		return nil, nil, err
	}
	defer cleanup()

	outboundBody, err := buildGPTImage2APICreateBody(bodyStorage, contentType, task.TaskID, imageRequest, relayInfo)
	if err != nil {
		return nil, nil, err
	}
	headerOverride, err := relaychannel.ResolveHeaderOverride(relayInfo, fakeCtx)
	if err != nil {
		outboundBody.Close()
		return nil, nil, err
	}
	return outboundBody, headerOverride, nil
}

func prepareGPTImage2APIAsyncRelayInfo(ctx context.Context, task *model.Task, channel *model.Channel, key string, bodyStorage common.BodyStorage, contentType string) (*dto.ImageRequest, *relaycommon.RelayInfo, *gin.Context, func(), error) {
	relayMode := imageTaskRelayModeFromTask(task)
	path := imageTaskRequestPathFromTask(task)
	if _, err := bodyStorage.Seek(0, io.SeekStart); err != nil {
		return nil, nil, nil, nil, err
	}

	fakeCtx, cleanup := newImageTaskRelayFakeContext(ctx, task, http.MethodPost, path, bodyStorage, contentType)
	fakeCtx.Set(common.KeyBodyStorage, bodyStorage)

	if err := setupImageTaskBaseGinContext(fakeCtx, task); err != nil {
		cleanup()
		return nil, nil, nil, nil, err
	}
	if err := setupImageTaskFixedChannelContext(fakeCtx, task, channel, key); err != nil {
		cleanup()
		return nil, nil, nil, nil, err
	}

	imageRequest, err := helper.GetAndValidOpenAIImageRequest(fakeCtx, relayMode)
	if err != nil {
		cleanup()
		return nil, nil, nil, nil, err
	}
	imageRequest.Stream = false

	relayInfo, err := buildImageTaskRelayInfo(fakeCtx, task, imageRequest, relayMode, bodyStorage, contentType)
	if err != nil {
		cleanup()
		return nil, nil, nil, nil, err
	}
	relayInfo.InitChannelMeta(fakeCtx)
	if err := helper.ModelMappedHelper(fakeCtx, relayInfo, imageRequest); err != nil {
		cleanup()
		return nil, nil, nil, nil, err
	}
	if _, err := bodyStorage.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, nil, nil, nil, err
	}
	return imageRequest, relayInfo, fakeCtx, cleanup, nil
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

func buildGPTImage2APIAsyncStatusHeaderOverride(ctx context.Context, task *model.Task, channel *model.Channel, key string) (map[string]string, error) {
	relayInfo, fakeCtx, cleanup, err := prepareGPTImage2APIAsyncStatusRelayInfo(ctx, task, channel, key)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if err := relaycommon.ApplyHeaderOverrideOperationsWithRelayInfo(relayInfo); err != nil {
		return nil, err
	}
	return relaychannel.ResolveHeaderOverride(relayInfo, fakeCtx)
}

func prepareGPTImage2APIAsyncStatusRelayInfo(ctx context.Context, task *model.Task, channel *model.Channel, key string) (*relaycommon.RelayInfo, *gin.Context, func(), error) {
	fakeCtx, cleanup := newImageTaskRelayFakeContext(ctx, task, http.MethodGet, "/api/image-tasks", nil, "")
	if err := setupImageTaskBaseGinContext(fakeCtx, task); err != nil {
		cleanup()
		return nil, nil, nil, err
	}
	if err := setupImageTaskFixedChannelContext(fakeCtx, task, channel, key); err != nil {
		cleanup()
		return nil, nil, nil, err
	}

	startTime := imageTaskRelayStartTime(task)
	request := &dto.ImageRequest{Model: imageTaskModelName(task)}
	relayInfo := &relaycommon.RelayInfo{
		Request:           request,
		RelayFormat:       types.RelayFormatOpenAIImage,
		RelayMode:         imageTaskRelayModeFromTask(task),
		RequestURLPath:    fakeCtx.Request.URL.String(),
		UserId:            task.UserId,
		UsingGroup:        task.Group,
		UserGroup:         common.GetContextKeyString(fakeCtx, constant.ContextKeyUserGroup),
		UserQuota:         common.GetContextKeyInt(fakeCtx, constant.ContextKeyUserQuota),
		UserEmail:         common.GetContextKeyString(fakeCtx, constant.ContextKeyUserEmail),
		OriginModelName:   imageTaskModelName(task),
		TokenId:           task.PrivateData.TokenId,
		TokenKey:          common.GetContextKeyString(fakeCtx, constant.ContextKeyTokenKey),
		TokenUnlimited:    common.GetContextKeyBool(fakeCtx, constant.ContextKeyTokenUnlimited),
		TokenGroup:        task.Group,
		StartTime:         startTime,
		FirstResponseTime: startTime.Add(-time.Second),
		RequestHeaders:    cloneImageTaskStringMap(task.PrivateData.RequestHeaders),
	}
	if setting, ok := common.GetContextKeyType[dto.UserSetting](fakeCtx, constant.ContextKeyUserSetting); ok {
		relayInfo.UserSetting = setting
	}
	relayInfo.InitRequestConversionChain()
	relayInfo.InitChannelMeta(fakeCtx)
	if err := helper.ModelMappedHelper(fakeCtx, relayInfo, request); err != nil {
		cleanup()
		return nil, nil, nil, err
	}
	return relayInfo, fakeCtx, cleanup, nil
}

func buildGPTImage2APICreateBody(bodyStorage common.BodyStorage, contentType string, clientTaskID string, imageRequest *dto.ImageRequest, relayInfo *relaycommon.RelayInfo) (*imageTaskOutboundBody, error) {
	if _, err := bodyStorage.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	switch {
	case strings.Contains(contentType, "multipart/form-data"):
		return buildGPTImage2APIMultipartBody(bodyStorage, contentType, clientTaskID, imageRequest, relayInfo)
	case strings.Contains(contentType, "application/x-www-form-urlencoded"):
		body, err := readImageTaskBodyStorageBytes(bodyStorage)
		if err != nil {
			return nil, err
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, err
		}
		applyGPTImage2APIImageRequestFieldsToValues(values, imageRequest)
		if gptImage2APIHasParamOverride(relayInfo) {
			bodyMap := gptImage2APIFormValuesToMap(values)
			bodyMap["stream"] = false
			bodyMap, err = applyGPTImage2APIParamOverrideToMap(bodyMap, relayInfo)
			if err != nil {
				return nil, err
			}
			bodyMap["client_task_id"] = clientTaskID
			bodyMap["stream"] = false
			out, err := common.Marshal(bodyMap)
			if err != nil {
				return nil, err
			}
			return newImageTaskBytesOutboundBody(out, "application/json"), nil
		}
		values.Set("client_task_id", clientTaskID)
		values.Set("stream", "false")
		return newImageTaskBytesOutboundBody([]byte(values.Encode()), "application/x-www-form-urlencoded"), nil
	default:
		bodyMap := make(map[string]any)
		if err := common.DecodeJson(bodyStorage, &bodyMap); err != nil {
			return nil, err
		}
		applyGPTImage2APIImageRequestFieldsToMap(bodyMap, imageRequest)
		bodyMap["stream"] = false
		bodyMap, err := applyGPTImage2APIParamOverrideToMap(bodyMap, relayInfo)
		if err != nil {
			return nil, err
		}
		bodyMap["client_task_id"] = clientTaskID
		bodyMap["stream"] = false
		out, err := common.Marshal(bodyMap)
		if err != nil {
			return nil, err
		}
		return newImageTaskBytesOutboundBody(out, "application/json"), nil
	}
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

func buildGPTImage2APIMultipartBody(bodyStorage common.BodyStorage, contentType string, clientTaskID string, imageRequest *dto.ImageRequest, relayInfo *relaycommon.RelayInfo) (*imageTaskOutboundBody, error) {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, err
	}
	boundary := params["boundary"]
	if boundary == "" {
		return nil, errors.New("multipart boundary is missing")
	}
	if _, err := bodyStorage.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	path, file, reservation, err := common.CreateDiskCacheFileWithReservation(common.DiskCacheTypeBody, imageTaskOutboundCacheReservationBytes(bodyStorage.Size()))
	if err != nil {
		return nil, err
	}
	cleanup := true
	committed := false
	defer func() {
		if cleanup {
			_ = file.Close()
			if committed {
				_ = common.RemoveDiskCacheFile(path)
			} else {
				_ = os.Remove(path)
			}
		}
		if !committed {
			reservation.Release()
		}
	}()

	reader := multipart.NewReader(bodyStorage, boundary)
	writer := multipart.NewWriter(file)
	fields := make(map[string][]string)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		name := part.FormName()
		if name == "client_task_id" || name == "stream" {
			continue
		}
		if part.FileName() == "" {
			value, err := io.ReadAll(part)
			if err != nil {
				return nil, err
			}
			fields[name] = append(fields[name], string(value))
			continue
		}
		partHeader := cloneMIMEHeader(part.Header)
		partWriter, err := writer.CreatePart(partHeader)
		if err != nil {
			return nil, err
		}
		if _, err := io.Copy(partWriter, part); err != nil {
			return nil, err
		}
	}
	applyGPTImage2APIImageRequestFieldsToFields(fields, imageRequest)
	fields["stream"] = []string{"false"}
	fields, err = applyGPTImage2APIParamOverrideToFields(fields, relayInfo)
	if err != nil {
		return nil, err
	}
	fields["client_task_id"] = []string{clientTaskID}
	fields["stream"] = []string{"false"}
	if err := writeGPTImage2APIFields(writer, fields); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	if err := reservation.Commit(stat.Size()); err != nil {
		return nil, err
	}
	committed = true
	cleanup = false
	return &imageTaskOutboundBody{
		Reader:        file,
		ContentType:   writer.FormDataContentType(),
		ContentLength: stat.Size(),
		cleanup: func() {
			_ = file.Close()
			_ = common.RemoveDiskCacheFile(path)
		},
	}, nil
}

func applyGPTImage2APIImageRequestFieldsToMap(bodyMap map[string]any, imageRequest *dto.ImageRequest) {
	if bodyMap == nil || imageRequest == nil {
		return
	}
	if modelName := strings.TrimSpace(imageRequest.Model); modelName != "" {
		bodyMap["model"] = modelName
	}
}

func applyGPTImage2APIImageRequestFieldsToValues(values url.Values, imageRequest *dto.ImageRequest) {
	if values == nil || imageRequest == nil {
		return
	}
	if modelName := strings.TrimSpace(imageRequest.Model); modelName != "" {
		values.Set("model", modelName)
	}
}

func applyGPTImage2APIImageRequestFieldsToFields(fields map[string][]string, imageRequest *dto.ImageRequest) {
	if fields == nil || imageRequest == nil {
		return
	}
	if modelName := strings.TrimSpace(imageRequest.Model); modelName != "" {
		fields["model"] = []string{modelName}
	}
}

func gptImage2APIHasParamOverride(relayInfo *relaycommon.RelayInfo) bool {
	return relayInfo != nil && relayInfo.ChannelMeta != nil && len(relayInfo.ParamOverride) > 0
}

func applyGPTImage2APIParamOverrideToMap(bodyMap map[string]any, relayInfo *relaycommon.RelayInfo) (map[string]any, error) {
	if !gptImage2APIHasParamOverride(relayInfo) {
		return bodyMap, nil
	}
	jsonData, err := common.Marshal(bodyMap)
	if err != nil {
		return nil, err
	}
	jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, relayInfo)
	if err != nil {
		return nil, err
	}
	overridden := make(map[string]any)
	if err := common.Unmarshal(jsonData, &overridden); err != nil {
		return nil, err
	}
	return overridden, nil
}

func applyGPTImage2APIParamOverrideToFields(fields map[string][]string, relayInfo *relaycommon.RelayInfo) (map[string][]string, error) {
	if !gptImage2APIHasParamOverride(relayInfo) {
		return fields, nil
	}
	bodyMap := gptImage2APIFieldsToMap(fields)
	bodyMap, err := applyGPTImage2APIParamOverrideToMap(bodyMap, relayInfo)
	if err != nil {
		return nil, err
	}
	return gptImage2APIMapToFields(bodyMap), nil
}

func gptImage2APIFormValuesToMap(values url.Values) map[string]any {
	bodyMap := make(map[string]any, len(values))
	for key, vals := range values {
		bodyMap[key] = gptImage2APIValuesToAny(vals)
	}
	return bodyMap
}

func gptImage2APIFieldsToMap(fields map[string][]string) map[string]any {
	bodyMap := make(map[string]any, len(fields))
	for key, vals := range fields {
		bodyMap[key] = gptImage2APIValuesToAny(vals)
	}
	return bodyMap
}

func gptImage2APIValuesToAny(values []string) any {
	switch len(values) {
	case 0:
		return ""
	case 1:
		return values[0]
	default:
		return append([]string(nil), values...)
	}
}

func gptImage2APIMapToFields(bodyMap map[string]any) map[string][]string {
	fields := make(map[string][]string, len(bodyMap))
	for key, value := range bodyMap {
		fields[key] = gptImage2APIFieldValues(value)
	}
	return fields
}

func gptImage2APIFieldValues(value any) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case []string:
		return append([]string(nil), v...)
	case []any:
		values := make([]string, 0, len(v))
		for _, item := range v {
			values = append(values, gptImage2APIFieldValueString(item))
		}
		return values
	default:
		return []string{gptImage2APIFieldValueString(v)}
	}
}

func gptImage2APIFieldValueString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case json.RawMessage:
		return string(v)
	case map[string]any, []any, map[string]string, []string:
		if data, err := common.Marshal(v); err == nil {
			return string(data)
		}
	}
	return fmt.Sprint(value)
}

func writeGPTImage2APIFields(writer *multipart.Writer, fields map[string][]string) error {
	for key, values := range fields {
		for _, value := range values {
			if err := writer.WriteField(key, value); err != nil {
				return err
			}
		}
	}
	return nil
}

func cloneMIMEHeader(header textproto.MIMEHeader) textproto.MIMEHeader {
	cloned := make(textproto.MIMEHeader, len(header))
	for key, values := range header {
		cloned[key] = append([]string(nil), values...)
	}
	return cloned
}

func settleImageTaskConsumption(ctx context.Context, task *model.Task, result json.RawMessage, usage *dto.Usage, extraContent []string, billingInput *billingexpr.RequestInput) error {
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
		UserQuota:             common.GetContextKeyInt(fakeCtx, constant.ContextKeyUserQuota),
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
			Action: task.Action,
		},
		ChannelMeta: imageTaskChannelMetaFromTask(ctx, task),
	}
	info.InitRequestConversionChain()
	if usage == nil {
		if parsedUsage, ok := imageTaskUsageFromResult(result); ok {
			usage = parsedUsage
		}
	}
	if usage != nil {
		return service.PostTextConsumeQuotaChecked(fakeCtx, info, usage, extraContent)
	}
	service.LogTaskConsumption(fakeCtx, info)
	return nil
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

func stringValueFromMap(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(m[key]); value != "" {
			return value
		}
	}
	return ""
}

func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%g", v)
	default:
		return ""
	}
}

func progressValueFromMap(m map[string]any) string {
	for _, key := range []string{"progress", "percent"} {
		value, ok := m[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case string:
			return strings.TrimSpace(v)
		case float64:
			if v <= 1 {
				v *= 100
			}
			return fmt.Sprintf("%.0f%%", v)
		}
	}
	return ""
}

func errorValueToString(value any) string {
	if msg := stringValue(value); msg != "" {
		return msg
	}
	if m, ok := value.(map[string]any); ok {
		if msg := stringValueFromMap(m, "message", "error", "reason"); msg != "" {
			return msg
		}
	}
	b, err := common.Marshal(value)
	if err != nil {
		return ""
	}
	return string(b)
}

func marshalFirstExistingValue(m map[string]any, keys ...string) json.RawMessage {
	value, ok := firstExistingValue(m, keys...)
	if !ok {
		return nil
	}
	b, err := common.Marshal(value)
	if err == nil && len(b) > 0 && string(b) != "null" {
		return json.RawMessage(b)
	}
	return nil
}

func firstExistingValue(m map[string]any, keys ...string) (any, bool) {
	for _, key := range keys {
		value, ok := m[key]
		if !ok || value == nil {
			continue
		}
		return value, true
	}
	return nil, false
}
