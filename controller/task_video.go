package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
)

func UpdateVideoTaskAll(ctx context.Context, platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*model.Task) error {
	for channelId, taskIds := range taskChannelM {
		if err := updateVideoTaskAll(ctx, platform, channelId, taskIds, taskM); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Channel #%d failed to update video async tasks: %s", channelId, err.Error()))
		}
	}
	return nil
}

func updateVideoTaskAll(ctx context.Context, platform constant.TaskPlatform, channelId int, taskIds []string, taskM map[string]*model.Task) error {
	logger.LogInfo(ctx, fmt.Sprintf("Channel #%d pending video tasks: %d", channelId, len(taskIds)))
	if len(taskIds) == 0 {
		return nil
	}
	cacheGetChannel, err := model.CacheGetChannel(channelId)
	if err != nil {
		reason := fmt.Sprintf("Failed to get channel info, channel ID: %d", channelId)
		now := common.GetTimestamp()
		for _, taskID := range taskIds {
			task, ok := taskM[taskID]
			if !ok || task == nil {
				continue
			}
			oldStatus := task.Status
			task.Status = model.TaskStatusFailure
			task.Progress = "100%"
			task.FailReason = reason
			if task.FinishTime == 0 {
				task.FinishTime = now
			}
			won, updateErr := task.UpdateWithStatus(oldStatus)
			if updateErr != nil {
				common.SysLog(fmt.Sprintf("UpdateVideoTask error: %v", updateErr))
				continue
			}
			if !won {
				logger.LogWarn(ctx, fmt.Sprintf("Task %s status changed from %s before channel failure update, skip refund", task.TaskID, oldStatus))
				continue
			}
			if task.Quota != 0 {
				if refundErr := service.RefundTaskQuota(ctx, task, reason); refundErr != nil {
					logger.LogError(ctx, fmt.Sprintf("video task %s refund failed after channel lookup failure: %s", task.TaskID, refundErr.Error()))
				}
			}
		}
		return fmt.Errorf("CacheGetChannel failed: %w", err)
	}
	adaptor := relay.GetTaskAdaptor(platform)
	if adaptor == nil {
		return fmt.Errorf("video adaptor not found")
	}
	info := &relaycommon.RelayInfo{}
	info.ChannelMeta = &relaycommon.ChannelMeta{
		ChannelBaseUrl: cacheGetChannel.GetBaseURL(),
	}
	info.ApiKey = cacheGetChannel.Key
	adaptor.Init(info)
	for _, taskId := range taskIds {
		if err := updateVideoSingleTask(ctx, adaptor, cacheGetChannel, taskId, taskM); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to update video task %s: %s", taskId, err.Error()))
		}
	}
	return nil
}

func updateVideoSingleTask(ctx context.Context, adaptor channel.TaskAdaptor, channel *model.Channel, taskId string, taskM map[string]*model.Task) error {
	baseURL := constant.ChannelBaseURLs[channel.Type]
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}
	proxy := channel.GetSetting().Proxy

	task := taskM[taskId]
	if task == nil {
		logger.LogError(ctx, fmt.Sprintf("Task %s not found in taskM", taskId))
		return fmt.Errorf("task %s not found", taskId)
	}
	key := channel.Key

	privateData := task.PrivateData
	if privateData.Key != "" {
		key = privateData.Key
	}
	resp, err := adaptor.FetchTask(baseURL, key, map[string]any{
		"task_id": taskId,
		"action":  task.Action,
	}, proxy)
	if err != nil {
		return fmt.Errorf("fetchTask failed for task %s: %w", taskId, err)
	}
	//if resp.StatusCode != http.StatusOK {
	//return fmt.Errorf("get Video Task status code: %d", resp.StatusCode)
	//}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("readAll failed for task %s: %w", taskId, err)
	}

	logger.LogDebug(ctx, "UpdateVideoSingleTask response: %s", responseBody)

	taskResult := &relaycommon.TaskInfo{}
	// try parse as New API response format
	var responseItems dto.TaskResponse[model.Task]
	if err = common.Unmarshal(responseBody, &responseItems); err == nil && responseItems.IsSuccess() {
		logger.LogDebug(ctx, "UpdateVideoSingleTask parsed as new api response format: %+v", responseItems)
		t := responseItems.Data
		taskResult.TaskID = t.TaskID
		taskResult.Status = string(t.Status)
		taskResult.Url = t.FailReason
		taskResult.Progress = t.Progress
		taskResult.Reason = t.FailReason
		task.Data = t.Data
	} else if taskResult, err = adaptor.ParseTaskResult(responseBody); err != nil {
		return fmt.Errorf("parseTaskResult failed for task %s: %w", taskId, err)
	} else {
		task.Data = redactVideoResponseBody(responseBody)
	}

	logger.LogDebug(ctx, "UpdateVideoSingleTask taskResult: %+v", taskResult)

	now := time.Now().Unix()
	if taskResult.Status == "" {
		//return fmt.Errorf("task %s status is empty", taskId)
		taskResult = relaycommon.FailTaskInfo("upstream returned empty status")
	}

	// 记录原本的状态，防止重复退款
	shouldRefund := false
	shouldSettle := false
	settleActualQuota := 0
	settleReason := ""
	settleLogMessage := ""
	settleErrorAction := ""
	var settleClamp *common.QuotaClamp
	quota := task.Quota
	preStatus := task.Status

	task.Status = model.TaskStatus(taskResult.Status)
	switch taskResult.Status {
	case model.TaskStatusSubmitted:
		task.Progress = "10%"
	case model.TaskStatusQueued:
		task.Progress = "20%"
	case model.TaskStatusInProgress:
		task.Progress = "30%"
		if task.StartTime == 0 {
			task.StartTime = now
		}
	case model.TaskStatusSuccess:
		task.Progress = "100%"
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		if !(len(taskResult.Url) > 5 && taskResult.Url[:5] == "data:") {
			task.FailReason = taskResult.Url
		}

		// 如果返回了 total_tokens 并且配置了模型倍率(非固定价格),则重新计费
		if taskResult.TotalTokens > 0 {
			// 获取模型名称
			var taskData map[string]interface{}
			if err := json.Unmarshal(task.Data, &taskData); err == nil {
				if modelName, ok := taskData["model"].(string); ok && modelName != "" {
					// 获取模型价格和倍率
					modelRatio, hasRatioSetting := service.TaskBillingModelRatio(task, modelName)
					// 只有配置了倍率(非固定价格)时才按 token 重新计费
					if hasRatioSetting && modelRatio > 0 {
						// 获取用户和组的倍率信息
						if finalGroupRatio, ok := service.TaskBillingGroupRatio(task); ok {
							// 计算实际应扣费额度: totalTokens * modelRatio * groupRatio（饱和转换，防止溢出成负数）
							actualQuota, clamp := common.QuotaFromPositiveFloatChecked(float64(taskResult.TotalTokens) * modelRatio * finalGroupRatio)
							if clamp != nil {
								logger.LogWarn(ctx, fmt.Sprintf("quota saturation on video task %s: op=%s kind=%s original=%g clamped=%d user=%d",
									task.TaskID, clamp.Op, clamp.Kind, clamp.Original, clamp.Clamped, task.UserId))
							}

							// 计算差额
							preConsumedQuota := task.Quota
							quotaDelta := actualQuota - preConsumedQuota

							if preStatus == model.TaskStatusSuccess {
								logger.LogWarn(ctx, fmt.Sprintf("Task %s already in success status, skip settlement", task.TaskID))
							} else {
								shouldSettle = true
								settleActualQuota = actualQuota
								settleClamp = clamp
								if quotaDelta > 0 {
									settleLogMessage = fmt.Sprintf("video task %s post-consume charge: %s (actual: %s, pre-consumed: %s, tokens: %d)",
										task.TaskID,
										logger.LogQuota(quotaDelta),
										logger.LogQuota(actualQuota),
										logger.LogQuota(preConsumedQuota),
										taskResult.TotalTokens,
									)
									settleReason = fmt.Sprintf("video task post-consume charge, model ratio %.2f, group ratio %.2f, tokens %d, pre-consumed %s, actual %s, delta %s",
										modelRatio, finalGroupRatio, taskResult.TotalTokens,
										logger.LogQuota(preConsumedQuota), logger.LogQuota(actualQuota), logger.LogQuota(quotaDelta))
									settleErrorAction = "post-consume charge"
								} else if quotaDelta < 0 {
									refundQuota := -quotaDelta
									settleLogMessage = fmt.Sprintf("video task %s post-consume refund: %s (actual: %s, pre-consumed: %s, tokens: %d)",
										task.TaskID,
										logger.LogQuota(refundQuota),
										logger.LogQuota(actualQuota),
										logger.LogQuota(preConsumedQuota),
										taskResult.TotalTokens,
									)
									settleReason = fmt.Sprintf("video task post-consume refund, model ratio %.2f, group ratio %.2f, tokens %d, pre-consumed %s, actual %s, refund %s",
										modelRatio, finalGroupRatio, taskResult.TotalTokens,
										logger.LogQuota(preConsumedQuota), logger.LogQuota(actualQuota), logger.LogQuota(refundQuota))
									settleErrorAction = "post-consume refund"
								} else {
									settleLogMessage = fmt.Sprintf("video task %s pre-consumed quota matched actual quota (%s, tokens: %d)",
										task.TaskID, logger.LogQuota(actualQuota), taskResult.TotalTokens)
									settleReason = fmt.Sprintf("video task quota matched actual usage, tokens %d", taskResult.TotalTokens)
									settleErrorAction = "quota match settlement"
								}
							}
						}
					}
				}
			}
		}
	case model.TaskStatusFailure:
		logger.LogJson(ctx, fmt.Sprintf("Task %s failed", taskId), task)
		task.Status = model.TaskStatusFailure
		task.Progress = "100%"
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		task.FailReason = taskResult.Reason
		logger.LogInfo(ctx, fmt.Sprintf("Task %s failed: %s", task.TaskID, task.FailReason))
		taskResult.Progress = "100%"
		if quota != 0 {
			if preStatus != model.TaskStatusFailure {
				shouldRefund = true
			} else {
				logger.LogWarn(ctx, fmt.Sprintf("Task %s already in failure status, skip refund", task.TaskID))
			}
		}
	default:
		return fmt.Errorf("unknown task status %s for task %s", taskResult.Status, taskId)
	}
	if taskResult.Progress != "" {
		task.Progress = taskResult.Progress
	}
	updateSucceeded := true
	if task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure {
		if preStatus != task.Status {
			won, err := task.UpdateWithStatus(preStatus)
			if err != nil {
				common.SysLog("UpdateVideoTask task error: " + err.Error())
				updateSucceeded = false
			} else if !won {
				logger.LogWarn(ctx, fmt.Sprintf("Task %s status changed from %s before terminal update, skip settlement/refund", task.TaskID, preStatus))
				updateSucceeded = false
			}
		} else if err := task.Update(); err != nil {
			common.SysLog("UpdateVideoTask task error: " + err.Error())
			updateSucceeded = false
		}
	} else if err := task.Update(); err != nil {
		common.SysLog("UpdateVideoTask task error: " + err.Error())
		updateSucceeded = false
	}
	if !updateSucceeded {
		shouldRefund = false
		shouldSettle = false
	}

	if shouldSettle {
		if settleLogMessage != "" {
			logger.LogInfo(ctx, settleLogMessage)
		}
		if err := service.RecalculateTaskQuota(ctx, task, settleActualQuota, settleReason, settleClamp); err != nil {
			logger.LogError(ctx, fmt.Sprintf("video task %s %s failed: %s", task.TaskID, settleErrorAction, err.Error()))
			service.MarkTaskSettlementReview(ctx, task, settleActualQuota, err)
		}
	}

	if shouldRefund {
		if err := service.RefundTaskQuota(ctx, task, fmt.Sprintf("Video async task failed %s, refund %s", task.TaskID, logger.LogQuota(quota))); err != nil {
			logger.LogError(ctx, fmt.Sprintf("video task %s refund failed: %s", task.TaskID, err.Error()))
		}
	}

	return nil
}

func redactVideoResponseBody(body []byte) []byte {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	resp, _ := m["response"].(map[string]any)
	if resp != nil {
		delete(resp, "bytesBase64Encoded")
		if v, ok := resp["video"].(string); ok {
			resp["video"] = truncateBase64(v)
		}
		if vs, ok := resp["videos"].([]any); ok {
			for i := range vs {
				if vm, ok := vs[i].(map[string]any); ok {
					delete(vm, "bytesBase64Encoded")
				}
			}
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return b
}

func truncateBase64(s string) string {
	const maxKeep = 256
	if len(s) <= maxKeep {
		return s
	}
	return s[:maxKeep] + "..."
}
