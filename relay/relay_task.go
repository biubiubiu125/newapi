package relay

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	plugindto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type TaskSubmitResult struct {
	UpstreamTaskID           string
	TaskData                 []byte
	Platform                 constant.TaskPlatform
	Quota                    int
	ClientResponse           any
	Immediate                *relaycommon.TaskInfo
	PluginState              []byte
	CompletionBillingAdaptor service.TaskCompletionBillingAdaptor
	//PerCallPrice   types.PriceData
}

func isSuccessfulTaskSubmissionStatus(status int) bool {
	return status >= http.StatusOK && status < http.StatusMultipleChoices
}

// ResolveOriginTask 处理基于已有任务的提交（remix / continuation）：
// 查找原始任务、从中提取模型名称，并优先把请求绑定到原始任务的渠道。
// 如果原始渠道已禁用或没有可用 key，则退回到普通选路/故障转移。
// 以及提取 OtherRatios（时长、分辨率）。
// 该函数在控制器的重试循环之前调用一次，其结果通过 info 字段和上下文持久化。
func ResolveOriginTask(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	// 检测 remix action
	path := c.Request.URL.Path
	if strings.Contains(path, "/v1/videos/") && strings.HasSuffix(path, "/remix") {
		info.Action = constant.TaskActionRemix
	}

	// 提取 remix 任务的 video_id
	if info.Action == constant.TaskActionRemix {
		videoID := c.Param("video_id")
		if strings.TrimSpace(videoID) == "" {
			return service.TaskErrorWrapperLocal(fmt.Errorf("video_id is required"), "invalid_request", http.StatusBadRequest)
		}
		info.OriginTaskID = videoID
	}

	if info.OriginTaskID == "" {
		return nil
	}

	// 查找原始任务
	originTask, exist, err := model.GetByTaskId(info.UserId, info.OriginTaskID)
	if err != nil {
		return service.TaskErrorWrapper(err, "get_origin_task_failed", http.StatusInternalServerError)
	}
	if !exist {
		return service.TaskErrorWrapperLocal(errors.New("task_origin_not_exist"), "task_not_exist", http.StatusBadRequest)
	}

	// 从原始任务推导模型名称
	if info.OriginModelName == "" {
		if originTask.Properties.OriginModelName != "" {
			info.OriginModelName = originTask.Properties.OriginModelName
		} else if originTask.Properties.UpstreamModelName != "" {
			info.OriginModelName = originTask.Properties.UpstreamModelName
		} else {
			var taskData map[string]interface{}
			_ = common.Unmarshal(originTask.Data, &taskData)
			if m, ok := taskData["model"].(string); ok && m != "" {
				info.OriginModelName = m
			}
		}
	}

	// 优先绑定到原始任务的渠道；如果原始渠道不可用，则退回通用选路。
	// RelayInfo 的 ChannelMeta 在 submit 尝试中才会初始化，这里只安全地
	// 更新 Gin 上下文；如果 ChannelMeta 已存在，再同步嵌入字段。
	currentChannelID := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
	ch, err := model.GetChannelById(originTask.ChannelId, true)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "channel_not_found", http.StatusBadRequest)
	}
	if ch.Status != common.ChannelStatusEnabled {
		info.LockedChannel = nil
	} else {
		info.LockedChannel = ch
		key, _, newAPIError := ch.GetNextEnabledKey()
		if newAPIError != nil {
			info.LockedChannel = nil
		} else if originTask.ChannelId != currentChannelID {
			common.SetContextKey(c, constant.ContextKeyChannelKey, key)
			common.SetContextKey(c, constant.ContextKeyChannelType, ch.Type)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, ch.GetBaseURL())
			common.SetContextKey(c, constant.ContextKeyChannelId, originTask.ChannelId)

			if info.ChannelMeta != nil {
				info.ChannelBaseUrl = ch.GetBaseURL()
				info.ChannelId = originTask.ChannelId
				info.ChannelType = ch.Type
				info.ApiKey = key
			}
		}
	}

	// 提取 remix 参数（时长、分辨率 → OtherRatios）
	if info.Action == constant.TaskActionRemix {
		if originTask.PrivateData.BillingContext != nil {
			// 新的 remix 逻辑：直接从原始任务的 BillingContext 中提取 OtherRatios（如果存在）
			for s, f := range originTask.PrivateData.BillingContext.OtherRatios {
				info.PriceData.AddOtherRatio(s, f)
			}
		} else {
			// 旧的 remix 逻辑：直接从 task data 解析 seconds 和 size（如果存在）
			var taskData map[string]interface{}
			_ = common.Unmarshal(originTask.Data, &taskData)
			secondsStr, _ := taskData["seconds"].(string)
			seconds, _ := strconv.Atoi(secondsStr)
			if seconds <= 0 {
				seconds = 4
			}
			// 历史任务数据可能包含未经校验的时长，作为计费乘数前必须钳制
			if seconds > relaycommon.MaxTaskDurationSeconds {
				seconds = relaycommon.MaxTaskDurationSeconds
			}
			sizeStr, _ := taskData["size"].(string)
			info.PriceData.AddOtherRatio("seconds", float64(seconds))
			info.PriceData.AddOtherRatio("size", 1)
			if sizeStr == "1792x1024" || sizeStr == "1024x1792" {
				info.PriceData.AddOtherRatio("size", 1.666667)
			}
		}
	}

	return nil
}

// ApplyChannelPin copies plugin-declared origin-task facts into RelayInfo and
// pins retries to the origin channel when the request explicitly requires it.
func ApplyChannelPin(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if info == nil {
		return nil
	}
	if info.TaskRelayInfo == nil {
		info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
	}
	if tasks, ok := common.GetContextKeyType[[]*model.Task](c, constant.ContextKeyOriginTasks); ok {
		refs := make([]relaycommon.OriginTaskRef, 0, len(tasks))
		for _, task := range tasks {
			if task == nil {
				continue
			}
			refs = append(refs, relaycommon.OriginTaskRef{
				TaskID: task.TaskID, UpstreamTaskID: task.GetUpstreamTaskID(),
				Action: task.Action, Status: string(task.Status),
				Data: append([]byte(nil), task.Data...),
			})
		}
		info.OriginTasks = refs
	}
	pin, found, _ := service.GetChannelConstraints(c).ResolvedPin()
	if !found || pin.RetryMode != plugindto.PinRetrySameChannel {
		return nil
	}
	ch, err := model.CacheGetChannel(pin.ChannelId)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "origin_task_channel_disabled", http.StatusBadRequest)
	}
	if ch.Status != common.ChannelStatusEnabled {
		return service.TaskErrorWrapperLocal(errors.New("the channel of the origin task is disabled"), "origin_task_channel_disabled", http.StatusBadRequest)
	}
	info.LockedChannel = ch
	return nil
}

func ApplyOriginTaskAffinity(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	return ApplyChannelPin(c, info)
}

// RelayTaskSubmit 完成 task 提交的全部流程（每次尝试调用一次）：
// 刷新渠道元数据 → 确定 platform/adaptor → 验证请求 →
// 估算计费(EstimateBilling) → 计算价格 → 预扣费（仅首次）→
// 构建/发送/解析上游请求 → 提交后计费调整(AdjustBillingOnSubmit)。
// 控制器负责 defer Refund 和成功后 Settle。
func RelayTaskSubmit(c *gin.Context, info *relaycommon.RelayInfo) (*TaskSubmitResult, *dto.TaskError) {
	info.InitChannelMeta(c)

	// 1. 确定 platform → 创建适配器 → 验证请求。
	platform := constant.TaskPlatform(c.GetString("platform"))
	if platform == "" {
		platform = GetTaskPlatform(c)
	}
	platform, adaptor := getTaskAdaptorForRequest(c, platform)
	if adaptor == nil {
		code, message := TaskPlatformUnavailableError(platform)
		return nil, service.TaskErrorWrapperLocal(errors.New(message), code, http.StatusBadRequest)
	}
	// 插件 submit hooks 在验证阶段就能读取 public task id。
	if info.PublicTaskID == "" {
		info.PublicTaskID = model.GenerateTaskID()
	}
	adaptor.Init(info)

	// 插件端可能在 ValidateRequestAndSetAction 里构建上游请求；若进入前已有
	// 原始模型名，先完成渠道模型映射，避免验证阶段使用未映射模型。
	mappedBeforeValidate := info.OriginModelName != ""
	if mappedBeforeValidate {
		info.UpstreamModelName = info.OriginModelName
		if err := helper.ModelMappedHelper(c, info, nil); err != nil {
			return nil, service.TaskErrorWrapperLocal(err, "model_mapping_failed", http.StatusBadRequest)
		}
	}
	if taskErr := adaptor.ValidateRequestAndSetAction(c, info); taskErr != nil {
		return nil, taskErr
	}

	// 2. 确定模型名称。
	modelName := info.OriginModelName
	if modelName == "" {
		modelName = service.CoverTaskActionToModelName(platform, info.Action)
	}
	if !mappedBeforeValidate {
		info.OriginModelName = modelName
		info.UpstreamModelName = modelName
		if err := helper.ModelMappedHelper(c, info, nil); err != nil {
			return nil, service.TaskErrorWrapperLocal(err, "model_mapping_failed", http.StatusBadRequest)
		}
	}

	// 3. 价格计算：普通按次价格或 task-usage 表达式价格。
	info.OriginModelName = modelName
	var priceData types.PriceData
	var err error
	useTiered := billing_setting.GetBillingMode(modelName) == billing_setting.BillingModeTieredExpr
	var exprStr string
	var exists bool
	if useTiered {
		exprStr, exists = billing_setting.GetBillingExpr(modelName)
	} else if info.IsModelMapped && billing_setting.GetBillingMode(info.UpstreamModelName) == billing_setting.BillingModeTieredExpr {
		if tailExpr, tailOK := billing_setting.GetBillingExpr(info.UpstreamModelName); tailOK && strings.TrimSpace(tailExpr) != "" {
			exprStr = tailExpr
			exists = true
			useTiered = true
		}
	}
	if useTiered {
		provider, supported := adaptor.(channel.TaskUsageFactsProvider)
		if billingexpr.UsesFixedPricing(exprStr) {
			return nil, service.TaskErrorWrapper(fmt.Errorf("fixed pricing is not supported for task usage expressions"), "model_price_error", http.StatusBadRequest)
		}
		if !exists || !supported {
			return nil, service.TaskErrorWrapper(fmt.Errorf("task model %s has no usage expression or meter", modelName), "model_price_error", http.StatusBadRequest)
		}
		var facts map[string]any
		if validatedProvider, ok := adaptor.(channel.TaskValidatedUsageFactsProvider); ok {
			facts, err = validatedProvider.ExtractUsageFactsValidated(c, info)
			if err != nil {
				return nil, service.TaskErrorWrapperLocal(err, "plugin_usage_invalid", http.StatusBadRequest)
			}
		} else {
			facts = provider.ExtractUsageFacts(c, info)
		}
		cost, trace, runErr := billingexpr.RunExprWithRequest(exprStr, billingexpr.TokenParams{}, billingexpr.RequestInput{Usage: facts})
		if runErr != nil || cost < 0 {
			if runErr == nil {
				runErr = fmt.Errorf("negative task expression result")
			}
			return nil, service.TaskErrorWrapper(runErr, "model_price_error", http.StatusBadRequest)
		}
		groupRatioInfo := helper.HandleGroupRatio(c, info)
		quota, clamp := common.QuotaRoundChecked(cost * common.QuotaPerUnit * groupRatioInfo.GroupRatio)
		noteTaskQuotaClamp(info, clamp)
		priceData = types.PriceData{Quota: quota, QuotaToPreConsume: quota, GroupRatioInfo: groupRatioInfo}
		info.TieredBillingSnapshot = &billingexpr.BillingSnapshot{
			BillingMode:               billing_setting.BillingModeTieredExpr,
			ModelName:                 modelName,
			ExprString:                exprStr,
			ExprHash:                  billingexpr.ExprHashString(exprStr),
			GroupRatio:                groupRatioInfo.GroupRatio,
			EstimatedQuotaBeforeGroup: cost * common.QuotaPerUnit,
			EstimatedQuotaAfterGroup:  quota,
			EstimatedTier:             trace.MatchedTier,
			QuotaPerUnit:              common.QuotaPerUnit,
			ExprVersion:               billingexpr.ExprVersion(exprStr),
			TaskUsageBilling:          true,
			UsageFacts:                facts,
		}
	} else {
		priceData, err = helper.ModelPriceHelperPerCall(c, info)
		if err != nil {
			return nil, service.TaskErrorWrapper(err, "model_price_error", http.StatusBadRequest)
		}
	}
	info.PriceData = priceData
	baseQuotaForOtherRatios := float64(info.PriceData.Quota)

	// 4. 计费估算：普通倍率只在非表达式计费下应用。
	if info.TieredBillingSnapshot == nil {
		var estimatedRatios map[string]float64
		if validatedProvider, ok := adaptor.(channel.TaskValidatedBillingProvider); ok {
			estimatedRatios, err = validatedProvider.EstimateBillingValidated(c, info)
			if err != nil {
				return nil, service.TaskErrorWrapperLocal(err, "plugin_usage_invalid", http.StatusBadRequest)
			}
		} else {
			estimatedRatios = adaptor.EstimateBilling(c, info)
		}
		if len(estimatedRatios) > 0 {
			for k, v := range estimatedRatios {
				info.PriceData.AddOtherRatio(k, v)
			}
		}
	}

	// 5. 将 OtherRatios 应用到基础额度（饱和转换，防止溢出成负数）。
	if info.TieredBillingSnapshot == nil && !common.StringsContains(constant.TaskPricePatches, modelName) {
		quotaWithRatios := info.PriceData.ApplyOtherRatiosToFloat(float64(info.PriceData.Quota))
		quota, clamp := common.QuotaFromPositiveFloatChecked(quotaWithRatios)
		info.PriceData.Quota = quota
		noteTaskQuotaClamp(info, clamp)
	}

	// 6. 预扣费（仅首次 — 重试时 info.Billing 已存在，跳过）。
	if info.Billing == nil && !info.PriceData.FreeModel {
		info.ForcePreConsume = true
		if apiErr := service.PreConsumeBilling(c, info.PriceData.Quota, info); apiErr != nil {
			return nil, service.TaskErrorFromAPIError(apiErr)
		}
	}

	// 7. 构建和发送请求。
	requestBody, err := adaptor.BuildRequestBody(c, info)
	if err != nil {
		return nil, service.TaskErrorWrapper(err, "build_request_failed", http.StatusInternalServerError)
	}
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return nil, service.TaskErrorWrapper(err, "do_request_failed", http.StatusInternalServerError)
	}
	if resp == nil || resp.Body == nil {
		return nil, service.TaskErrorWrapperLocal(errors.New("upstream returned an empty response"), "fail_to_fetch_task", http.StatusBadGateway)
	}
	defer resp.Body.Close()
	if !isSuccessfulTaskSubmissionStatus(resp.StatusCode) {
		responseBody, readErr := service.ReadResponseBodyLimited(resp, service.MaxResponseBodyBytes)
		if readErr != nil && len(responseBody) == 0 {
			responseBody = []byte(readErr.Error())
		}
		return nil, service.TaskErrorWrapper(fmt.Errorf("%s", string(responseBody)), "fail_to_fetch_task", resp.StatusCode)
	}

	// 8. 返回 OtherRatios 给下游（header 必须在写 body 之前设置）。
	otherRatios := info.PriceData.OtherRatios()
	if otherRatios == nil {
		otherRatios = map[string]float64{}
	}
	ratiosJSON, _ := common.Marshal(otherRatios)
	c.Header("X-New-Api-Other-Ratios", string(ratiosJSON))

	// 9. 解析响应；插件返回 client response，旧适配器经 bridge 复用 DoResponse。
	parsed, taskErr := adaptor.ParseResponse(c, resp, info)
	if taskErr != nil {
		return nil, taskErr
	}
	if parsed == nil {
		return nil, service.TaskErrorWrapperLocal(errors.New("task adaptor returned an empty response"), "plugin_submit_response_invalid", http.StatusBadGateway)
	}

	// 10. 提交后计费调整。
	finalQuota := info.PriceData.Quota
	if info.TieredBillingSnapshot == nil {
		var adjustedRatios map[string]float64
		if contextAdaptor, ok := adaptor.(interface {
			AdjustBillingOnSubmitContext(context.Context, *relaycommon.RelayInfo, []byte) map[string]float64
		}); ok {
			adjustedRatios = contextAdaptor.AdjustBillingOnSubmitContext(c.Request.Context(), info, parsed.TaskData)
		} else {
			adjustedRatios = adaptor.AdjustBillingOnSubmit(info, parsed.TaskData)
		}
		if len(adjustedRatios) > 0 {
			if adjustedQuota, ok := recalcQuotaFromRatios(info, adjustedRatios, baseQuotaForOtherRatios); ok {
				finalQuota = adjustedQuota
				info.PriceData.ReplaceOtherRatios(adjustedRatios)
				info.PriceData.Quota = finalQuota
			}
		}
	}

	return &TaskSubmitResult{
		UpstreamTaskID:           parsed.UpstreamTaskID,
		TaskData:                 parsed.TaskData,
		ClientResponse:           parsed.ClientResponse,
		Platform:                 platform,
		Quota:                    finalQuota,
		Immediate:                parsed.Immediate,
		PluginState:              parsed.PluginState,
		CompletionBillingAdaptor: adaptor,
	}, nil
}

// recalcQuotaFromRatios 根据 adjustedRatios 重新计算 quota。
// 公式: baseQuota × ∏(ratio) — 其中 baseQuota 是不含 OtherRatios 的基础额度。
func recalcQuotaFromRatios(info *relaycommon.RelayInfo, ratios map[string]float64, baseQuota float64) (int, bool) {
	// 从 PriceData 获取不含 OtherRatios 的基础价格
	if baseQuota <= 0 {
		baseQuota = info.PriceData.RemoveOtherRatiosFromFloat(float64(info.PriceData.Quota))
	}
	priceData := info.PriceData
	if !priceData.ReplaceOtherRatios(ratios) {
		return 0, false
	}
	// 应用新的 ratios
	result := priceData.ApplyOtherRatiosToFloat(baseQuota)
	quota, clamp := common.QuotaFromPositiveFloatChecked(result)
	noteTaskQuotaClamp(info, clamp)
	return quota, true
}

// noteTaskQuotaClamp records the first quota saturation event onto the task's
// RelayInfo so LogTaskConsumption can surface it on the submit log's
// admin_info. First non-nil clamp wins.
func noteTaskQuotaClamp(info *relaycommon.RelayInfo, clamp *common.QuotaClamp) {
	if clamp == nil || info == nil {
		return
	}
	if info.QuotaClamp == nil {
		info.QuotaClamp = clamp
	}
}

var fetchRespBuilders = map[int]func(c *gin.Context) (respBody []byte, taskResp *dto.TaskError){
	relayconstant.RelayModeSunoFetchByID:  sunoFetchByIDRespBodyBuilder,
	relayconstant.RelayModeSunoFetch:      sunoFetchRespBodyBuilder,
	relayconstant.RelayModeVideoFetchByID: videoFetchByIDRespBodyBuilder,
}

func RelayTaskFetch(c *gin.Context, relayMode int) (taskResp *dto.TaskError) {
	respBuilder, ok := fetchRespBuilders[relayMode]
	if !ok {
		return service.TaskErrorWrapperLocal(errors.New("invalid_relay_mode"), "invalid_relay_mode", http.StatusBadRequest)
	}

	respBody, taskErr := respBuilder(c)
	if taskErr != nil {
		return taskErr
	}
	if len(respBody) == 0 {
		respBody = []byte("{\"code\":\"success\",\"data\":null}")
	}

	c.Writer.Header().Set("Content-Type", "application/json")
	_, err := io.Copy(c.Writer, bytes.NewBuffer(respBody))
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "copy_response_body_failed", http.StatusInternalServerError)
		return
	}
	return
}

func sunoFetchRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	userId := c.GetInt("id")
	var condition = struct {
		IDs    []any  `json:"ids"`
		Action string `json:"action"`
	}{}
	err := c.BindJSON(&condition)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "invalid_request", http.StatusBadRequest)
		return
	}
	var tasks []any
	if len(condition.IDs) > 0 {
		taskModels, err := model.GetByTaskIds(userId, condition.IDs)
		if err != nil {
			taskResp = service.TaskErrorWrapper(err, "get_tasks_failed", http.StatusInternalServerError)
			return
		}
		for _, task := range taskModels {
			tasks = append(tasks, TaskModel2Dto(task))
		}
	} else {
		tasks = make([]any, 0)
	}
	respBody, err = common.Marshal(dto.TaskResponse[[]any]{
		Code: "success",
		Data: tasks,
	})
	return
}

func sunoFetchByIDRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	taskId := c.Param("id")
	userId := c.GetInt("id")

	originTask, exist, err := model.GetByTaskId(userId, taskId)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "get_task_failed", http.StatusInternalServerError)
		return
	}
	if !exist {
		taskResp = service.TaskErrorWrapperLocal(errors.New("task_not_exist"), "task_not_exist", http.StatusBadRequest)
		return
	}

	respBody, err = common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: TaskModel2Dto(originTask),
	})
	return
}

func videoFetchByIDRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	taskId := c.Param("task_id")
	if taskId == "" {
		taskId = c.GetString("task_id")
	}
	userId := c.GetInt("id")

	originTask, exist, err := model.GetByTaskId(userId, taskId)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "get_task_failed", http.StatusInternalServerError)
		return
	}
	if !exist {
		taskResp = service.TaskErrorWrapperLocal(errors.New("task_not_exist"), "task_not_exist", http.StatusBadRequest)
		return
	}

	isOpenAIVideoAPI := strings.HasPrefix(c.Request.RequestURI, "/v1/videos/")

	// Gemini/Vertex 支持实时查询：用户 fetch 时直接从上游拉取最新状态
	if realtimeResp := tryRealtimeFetch(c.Request.Context(), originTask, isOpenAIVideoAPI); len(realtimeResp) > 0 {
		respBody = realtimeResp
		return
	}

	// OpenAI Video API 格式: 走各 adaptor 的 ConvertToOpenAIVideo
	if isOpenAIVideoAPI {
		var adaptor any
		if originTask.PrivateData.Execution != nil && originTask.PrivateData.Execution.TaskPlugin != nil {
			pluginAdaptor, resolveErr := GetTaskPluginAdaptorForTask(originTask)
			if resolveErr != nil {
				taskResp = service.TaskErrorWrapper(resolveErr, "task_plugin_unavailable", http.StatusServiceUnavailable)
				return
			}
			adaptor = pluginAdaptor
		} else {
			adaptor = GetTaskAdaptor(originTask.Platform)
			if adaptor == nil {
				taskResp = service.TaskErrorWrapperLocal(fmt.Errorf("invalid channel id: %d", originTask.ChannelId), "invalid_channel_id", http.StatusBadRequest)
				return
			}
		}
		if converter, ok := adaptor.(channel.OpenAIVideoConverter); ok {
			var openAIVideoData []byte
			if contextConverter, contextOK := adaptor.(channel.OpenAIVideoConverterContext); contextOK {
				openAIVideoData, err = contextConverter.ConvertToOpenAIVideoContext(c.Request.Context(), originTask)
			} else {
				openAIVideoData, err = converter.ConvertToOpenAIVideo(originTask)
			}
			if err != nil {
				taskResp = service.TaskErrorWrapper(err, "convert_to_openai_video_failed", http.StatusInternalServerError)
				return
			}
			respBody = openAIVideoData
			return
		}
		taskResp = service.TaskErrorWrapperLocal(fmt.Errorf("not_implemented:%s", originTask.Platform), "not_implemented", http.StatusNotImplemented)
		return
	}

	// 通用 TaskDto 格式
	respBody, err = common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: TaskModel2Dto(originTask),
	})
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "marshal_response_failed", http.StatusInternalServerError)
	}
	return
}

// tryRealtimeFetch 尝试从上游实时拉取 Gemini/Vertex 任务状态。
// 仅当渠道类型为 Gemini 或 Vertex 时触发；其他渠道或出错时返回 nil。
// 当非 OpenAI Video API 时，还会构建自定义格式的响应体。
func tryRealtimeFetch(ctx context.Context, task *model.Task, isOpenAIVideoAPI bool) []byte {
	if task == nil {
		return nil
	}
	if task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure {
		return nil
	}
	if !shouldUseLegacyRealtimeFetch(task) {
		return tryPluginRealtimeFetch(ctx, task, isOpenAIVideoAPI)
	}
	channelModel, err := model.GetChannelById(task.ChannelId, true)
	if err != nil {
		return nil
	}
	if channelModel.Type != constant.ChannelTypeVertexAi && channelModel.Type != constant.ChannelTypeGemini {
		return nil
	}

	baseURL := constant.ChannelBaseURLs[channelModel.Type]
	if channelModel.GetBaseURL() != "" {
		baseURL = channelModel.GetBaseURL()
	}
	proxy := channelModel.GetSetting().Proxy
	adaptor := GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(channelModel.Type)))
	if adaptor == nil {
		return nil
	}
	key := getLegacyRealtimeTaskKey(channelModel, task)

	var resp *http.Response
	if contextAdaptor, ok := adaptor.(interface {
		FetchTaskContext(context.Context, string, string, *model.Task, string) (*http.Response, error)
	}); ok {
		resp, err = contextAdaptor.FetchTaskContext(ctx, baseURL, key, task, proxy)
	} else {
		resp, err = adaptor.FetchTask(baseURL, key, map[string]any{
			"task_id": task.GetUpstreamTaskID(),
			"action":  task.Action,
		}, proxy)
	}
	if err != nil || resp == nil || resp.Body == nil {
		return nil
	}
	defer resp.Body.Close()
	body, err := service.ReadResponseBodyLimited(resp, service.MaxResponseBodyBytes)
	if err != nil {
		return nil
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil
	}
	ti, err := adaptor.ParseTaskResult(body)
	if err != nil || ti == nil {
		return nil
	}

	snap := task.Snapshot()
	originalPrivateData := task.PrivateData
	originalData := append([]byte(nil), task.Data...)
	if !applyRealtimeTaskResult(task, ti, body, time.Now().Unix()) {
		return nil
	}

	if !snap.Equal(task.Snapshot()) {
		won, updateErr := task.UpdateWithStatus(snap.Status)
		if updateErr != nil || !won {
			task.Status = snap.Status
			task.Progress = snap.Progress
			task.StartTime = snap.StartTime
			task.FinishTime = snap.FinishTime
			task.FailReason = snap.FailReason
			task.PrivateData = originalPrivateData
			task.Data = originalData
			return nil
		}
	}
	if snap.Status != task.Status {
		settleRealtimeTaskBilling(ctx, adaptor, task, ti)
	}

	// OpenAI Video API 由调用者的 ConvertToOpenAIVideo 分支处理
	if isOpenAIVideoAPI {
		return nil
	}

	// 非 OpenAI Video API: 构建自定义格式响应
	format := detectVideoFormat(body)
	out := map[string]any{
		"error":    nil,
		"format":   format,
		"metadata": nil,
		"status":   mapTaskStatusToSimple(task.Status),
		"task_id":  task.TaskID,
		"url":      task.GetResultURL(),
	}
	respBody, _ := common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: out,
	})
	return respBody
}

func getLegacyRealtimeTaskKey(channel *model.Channel, task *model.Task) string {
	if task != nil {
		if key := strings.TrimSpace(task.PrivateData.Key); key != "" {
			return key
		}
	}
	if channel == nil {
		return ""
	}
	for _, key := range channel.GetKeys() {
		if key = strings.TrimSpace(key); key != "" {
			return key
		}
	}
	return strings.TrimSpace(channel.Key)
}

func tryPluginRealtimeFetch(ctx context.Context, task *model.Task, isOpenAIVideoAPI bool) []byte {
	if task == nil {
		return nil
	}
	if task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure {
		return nil
	}
	channelModel, err := model.GetChannelById(task.ChannelId, true)
	if err != nil || channelModel == nil {
		return nil
	}
	adaptor, err := GetTaskPluginAdaptorForTask(task)
	if err != nil || adaptor == nil {
		return nil
	}
	baseURL := constant.ChannelBaseURLs[channelModel.Type]
	if channelModel.GetBaseURL() != "" {
		baseURL = channelModel.GetBaseURL()
	}
	key := task.PrivateData.Key
	if key == "" {
		key = channelModel.Key
	}
	adaptor.Init(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: channelModel.Type, ChannelBaseUrl: baseURL,
			ApiKey: key, ChannelSetting: channelModel.GetSetting(),
		},
	})

	var resp *http.Response
	if contextAdaptor, contextOK := adaptor.(interface {
		FetchTaskContext(context.Context, string, string, *model.Task, string) (*http.Response, error)
	}); contextOK {
		resp, err = contextAdaptor.FetchTaskContext(ctx, baseURL, key, task, channelModel.GetSetting().Proxy)
	} else {
		resp, err = adaptor.FetchTask(baseURL, key, task, channelModel.GetSetting().Proxy)
	}
	if err != nil || resp == nil || resp.Body == nil {
		return nil
	}
	defer resp.Body.Close()
	body, err := service.ReadResponseBodyLimited(resp, service.MaxResponseBodyBytes)
	if err != nil {
		return nil
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil
	}

	var result *relaycommon.TaskInfo
	if contextAdaptor, contextOK := adaptor.(interface {
		ParseTaskResultContext(context.Context, *model.Task, *http.Response, []byte) (*relaycommon.TaskInfo, error)
	}); contextOK {
		result, err = contextAdaptor.ParseTaskResultContext(ctx, task, resp, body)
	} else {
		result, err = adaptor.ParseTaskResult(task, resp, body)
	}
	if err != nil || result == nil {
		return nil
	}

	snap := task.Snapshot()
	originalPrivateData := task.PrivateData
	originalData := append([]byte(nil), task.Data...)
	if !applyRealtimeTaskResult(task, result, body, time.Now().Unix()) {
		return nil
	}
	if !snap.Equal(task.Snapshot()) {
		won, updateErr := task.UpdateWithStatus(snap.Status)
		if updateErr != nil || !won {
			task.Status = snap.Status
			task.Progress = snap.Progress
			task.StartTime = snap.StartTime
			task.FinishTime = snap.FinishTime
			task.FailReason = snap.FailReason
			task.PrivateData = originalPrivateData
			task.Data = originalData
			return nil
		}
	}
	if snap.Status != task.Status {
		settleRealtimeTaskBilling(ctx, adaptor, task, result)
	}
	if isOpenAIVideoAPI {
		return nil
	}

	out := map[string]any{
		"error": nil, "format": detectVideoFormat(body), "metadata": nil,
		"status": mapTaskStatusToSimple(task.Status), "task_id": task.TaskID,
		"url": task.GetResultURL(),
	}
	respBody, _ := common.Marshal(dto.TaskResponse[any]{Code: "success", Data: out})
	return respBody
}

func applyRealtimeTaskResult(task *model.Task, result *relaycommon.TaskInfo, body []byte, now int64) bool {
	if task == nil || result == nil {
		return false
	}
	if task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure {
		return false
	}
	status := model.TaskStatus(strings.TrimSpace(result.Status))
	if !knownRealtimeTaskStatus(status) {
		return false
	}
	if len(body) > 0 {
		task.Data = service.RedactVideoResponseBody(body)
	}
	if len(result.PluginState) > 0 {
		task.PrivateData.PluginState = result.PluginState
	}
	if result.Status != "" {
		task.Status = status
	}
	if result.Progress != "" {
		task.Progress = result.Progress
	}
	switch task.Status {
	case model.TaskStatusSuccess:
		task.Progress = taskcommon.ProgressComplete
		task.FailReason = strings.TrimSpace(result.Reason)
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		resultURL := strings.TrimSpace(result.Url)
		if resultURL == "" {
			resultURL = strings.TrimSpace(result.RemoteUrl)
		}
		switch {
		case taskcommon.IsDataURL(resultURL):
			task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
		case resultURL != "":
			task.PrivateData.ResultURL = resultURL
		default:
			task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
		}
	case model.TaskStatusFailure:
		task.Progress = taskcommon.ProgressComplete
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		if strings.TrimSpace(result.Reason) != "" {
			task.FailReason = result.Reason
		}
		task.PrivateData.ResultURL = ""
	}
	return true
}

func settleRealtimeTaskBilling(
	ctx context.Context,
	adaptor service.TaskCompletionBillingAdaptor,
	task *model.Task,
	taskResult *relaycommon.TaskInfo,
) {
	if task == nil || taskResult == nil {
		return
	}
	switch task.Status {
	case model.TaskStatusSuccess:
		_ = service.SettleTaskBillingOnComplete(ctx, adaptor, task, taskResult)
	case model.TaskStatusFailure:
		if task.Quota == 0 {
			return
		}
		if err := service.RefundTaskQuota(ctx, task, task.FailReason); err != nil {
			logger.LogError(ctx, fmt.Sprintf("realtime task %s refund failed: %s", task.TaskID, err.Error()))
		}
	}
}

func knownRealtimeTaskStatus(status model.TaskStatus) bool {
	switch status {
	case model.TaskStatusNotStart, model.TaskStatusSubmitted, model.TaskStatusQueued,
		model.TaskStatusInProgress, model.TaskStatusSuccess, model.TaskStatusFailure:
		return true
	default:
		return false
	}
}

func shouldUseLegacyRealtimeFetch(task *model.Task) bool {
	return task != nil && (task.PrivateData.Execution == nil || task.PrivateData.Execution.TaskPlugin == nil)
}

// detectVideoFormat 从 Gemini/Vertex 原始响应中探测视频格式
func detectVideoFormat(rawBody []byte) string {
	var raw map[string]any
	if err := common.Unmarshal(rawBody, &raw); err != nil {
		return "mp4"
	}
	respObj, ok := raw["response"].(map[string]any)
	if !ok {
		return "mp4"
	}
	vids, ok := respObj["videos"].([]any)
	if !ok || len(vids) == 0 {
		return "mp4"
	}
	v0, ok := vids[0].(map[string]any)
	if !ok {
		return "mp4"
	}
	mt, ok := v0["mimeType"].(string)
	if !ok || mt == "" || strings.Contains(mt, "mp4") {
		return "mp4"
	}
	return mt
}

// mapTaskStatusToSimple 将内部 TaskStatus 映射为简化状态字符串
func mapTaskStatusToSimple(status model.TaskStatus) string {
	switch status {
	case model.TaskStatusSuccess:
		return "succeeded"
	case model.TaskStatusFailure:
		return "failed"
	case model.TaskStatusQueued, model.TaskStatusSubmitted:
		return "queued"
	default:
		return "processing"
	}
}

func TaskModel2Dto(task *model.Task) *dto.TaskDto {
	status := string(task.Status)
	if task.Platform == constant.TaskPlatformImage &&
		task.Status == model.TaskStatusSuccess &&
		task.SettlementStatus == model.TaskSettlementStatusReview {
		status = string(model.TaskStatusFailure)
	}
	failReason := task.FailReason
	if task.Status == model.TaskStatusSuccess && taskFailReasonIsLegacyResultURL(failReason) {
		failReason = ""
	}
	return &dto.TaskDto{
		ID:                     task.ID,
		CreatedAt:              task.CreatedAt,
		UpdatedAt:              task.UpdatedAt,
		TaskID:                 task.TaskID,
		Platform:               string(task.Platform),
		UserId:                 task.UserId,
		Group:                  task.Group,
		ChannelId:              task.ChannelId,
		Quota:                  task.Quota,
		Action:                 task.Action,
		Status:                 status,
		SettlementStatus:       task.SettlementStatus,
		SettlementError:        task.PrivateData.SettlementError,
		SettlementAttemptQuota: task.PrivateData.SettlementAttemptQuota,
		FailReason:             failReason,
		ResultURL:              task.GetResultURL(),
		SubmitTime:             task.SubmitTime,
		StartTime:              task.StartTime,
		FinishTime:             task.FinishTime,
		Progress:               task.Progress,
		Properties:             task.Properties,
		Username:               task.Username,
		Data:                   task.Data,
	}
}

func taskFailReasonIsLegacyResultURL(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	lower := strings.ToLower(value)
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "data:")
}
