package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

type midjourneyPollSummary struct {
	UnfinishedTasks int `json:"unfinished_tasks"`
	ChannelsScanned int `json:"channels_scanned"`
	NullTasksFailed int `json:"null_tasks_failed"`
}

func recoverTerminalFailedMidjourneyRefunds(ctx context.Context) {
	tasks := model.GetFailedMidjourneyTasksNeedingRefundSettlement()
	for _, task := range tasks {
		if ctx.Err() != nil {
			return
		}
		if task == nil || task.Quota == 0 {
			continue
		}
		if err := service.RefundMidjourneyTaskQuota(ctx, task, "终态失败任务退款恢复"); err != nil {
			logger.LogError(ctx, fmt.Sprintf("midjourney terminal failed task %s refund recovery failed: %s", task.MjId, err.Error()))
		}
	}
}

func runMidjourneyTaskUpdateOnce(ctx context.Context, report func(processed, total int)) midjourneyPollSummary {
	summary := midjourneyPollSummary{}
	if ctx == nil {
		ctx = context.Background()
	}

	recoverTerminalFailedMidjourneyRefunds(ctx)

	tasks := model.GetAllUnFinishTasks()
	if len(tasks) == 0 {
		return summary
	}
	summary.UnfinishedTasks = len(tasks)

	logger.LogInfo(ctx, fmt.Sprintf("检测到未完成的任务数有: %v", len(tasks)))
	taskChannelM := make(map[int][]string)
	taskM := make(map[string]*model.Midjourney)
	nullTasks := make([]*model.Midjourney, 0)
	for _, task := range tasks {
		if task.MjId == "" {
			nullTasks = append(nullTasks, task)
			continue
		}
		taskM[task.MjId] = task
		taskChannelM[task.ChannelId] = append(taskChannelM[task.ChannelId], task.MjId)
	}
	if len(nullTasks) > 0 {
		summary.NullTasksFailed = len(nullTasks)
		for _, task := range nullTasks {
			if failMidjourneyTaskAndRefund(ctx, task, "上游任务ID为空") {
				logger.LogInfo(ctx, fmt.Sprintf("Fix null mj_id task success: %d", task.Id))
			}
		}
	}
	if len(taskChannelM) == 0 {
		return summary
	}

	totalChannels := len(taskChannelM)
	processedChannels := 0
	for channelId, taskIds := range taskChannelM {
		if ctx.Err() != nil {
			break
		}
		if report != nil {
			report(processedChannels, totalChannels)
		}
		processedChannels++
		summary.ChannelsScanned++
		logger.LogInfo(ctx, fmt.Sprintf("渠道 #%d 未完成的任务有: %d", channelId, len(taskIds)))
		if len(taskIds) == 0 {
			continue
		}
		midjourneyChannel, err := model.CacheGetChannel(channelId)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("CacheGetChannel: %v", err))
			failReason := fmt.Sprintf("获取渠道信息失败，请联系管理员，渠道ID：%d", channelId)
			for _, taskId := range taskIds {
				task := taskM[taskId]
				if task == nil {
					continue
				}
				failMidjourneyTaskAndRefund(ctx, task, failReason)
			}
			continue
		}
		requestUrl := fmt.Sprintf("%s/mj/task/list-by-condition", *midjourneyChannel.BaseURL)

		body, err := common.Marshal(map[string]any{
			"ids": taskIds,
		})
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("Get Task marshal body error: %v", err))
			continue
		}
		requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, requestUrl, bytes.NewBuffer(body))
		if err != nil {
			cancel()
			logger.LogError(ctx, fmt.Sprintf("Get Task error: %v", err))
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("mj-api-secret", midjourneyChannel.Key)
		resp, err := service.GetHttpClient().Do(req)
		if err != nil {
			cancel()
			logger.LogError(ctx, fmt.Sprintf("Get Task Do req error: %v", err))
			continue
		}
		if resp.StatusCode != http.StatusOK {
			logger.LogError(ctx, fmt.Sprintf("Get Task status code: %d", resp.StatusCode))
			resp.Body.Close()
			cancel()
			continue
		}
		responseBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("Get Mjp Task parse body error: %v", err))
			continue
		}
		var responseItems []dto.MidjourneyDto
		err = common.Unmarshal(responseBody, &responseItems)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("Get Mjp Task parse body error2: %v, body: %s", err, string(responseBody)))
			continue
		}

		for _, responseItem := range responseItems {
			if ctx.Err() != nil {
				return summary
			}
			task := taskM[responseItem.MjId]
			if task == nil {
				logger.LogWarn(ctx, fmt.Sprintf("Midjourney task response ignored: unknown mj_id=%s", responseItem.MjId))
				continue
			}

			useTime := (time.Now().UnixNano() / int64(time.Millisecond)) - task.SubmitTime
			if useTime > 3600000 && task.Progress != "100%" {
				responseItem.FailReason = "上游任务超时（超过1小时）"
				responseItem.Status = "FAILURE"
			}
			if !checkMjTaskNeedUpdate(task, responseItem) {
				continue
			}
			preStatus := task.Status
			task.Code = 1
			task.Progress = responseItem.Progress
			task.PromptEn = responseItem.PromptEn
			task.State = responseItem.State
			if responseItem.SubmitTime > 0 {
				task.SubmitTime = responseItem.SubmitTime
			}
			if responseItem.StartTime > 0 {
				task.StartTime = responseItem.StartTime
			}
			if responseItem.FinishTime > 0 {
				task.FinishTime = responseItem.FinishTime
			}
			task.ImageUrl = responseItem.ImageUrl
			task.Status = responseItem.Status
			task.FailReason = responseItem.FailReason
			if responseItem.Properties != nil {
				propertiesStr, _ := common.Marshal(responseItem.Properties)
				task.Properties = string(propertiesStr)
			}
			if responseItem.Buttons != nil {
				buttonStr, _ := common.Marshal(responseItem.Buttons)
				task.Buttons = string(buttonStr)
			}
			task.VideoUrl = responseItem.VideoUrl

			if responseItem.VideoUrls != nil && len(responseItem.VideoUrls) > 0 {
				videoUrlsStr, err := common.Marshal(responseItem.VideoUrls)
				if err != nil {
					logger.LogError(ctx, fmt.Sprintf("序列化 VideoUrls 失败: %v", err))
					task.VideoUrls = "[]"
				} else {
					task.VideoUrls = string(videoUrlsStr)
				}
			} else {
				task.VideoUrls = ""
			}

			shouldReturnQuota := false
			if (task.Progress != "100%" && responseItem.FailReason != "") || (task.Progress == "100%" && task.Status == "FAILURE") {
				logger.LogInfo(ctx, task.MjId+" 构建失败，"+task.FailReason)
				task.Progress = "100%"
				if task.Quota != 0 {
					shouldReturnQuota = true
				}
			}
			won, err := task.UpdateWithStatus(preStatus)
			if err != nil {
				logger.LogError(ctx, "UpdateMidjourneyTask task error: "+err.Error())
			} else if won && shouldReturnQuota {
				if err := service.RefundMidjourneyTaskQuota(ctx, task, "构图失败"); err != nil {
					logger.LogError(ctx, fmt.Sprintf("midjourney task %s refund failed: %s", task.MjId, err.Error()))
				}
			}
		}
	}
	if report != nil && ctx.Err() == nil {
		report(totalChannels, totalChannels)
	}
	return summary
}

func failMidjourneyTaskAndRefund(ctx context.Context, task *model.Midjourney, failReason string) bool {
	if task == nil {
		return false
	}
	preStatus := task.Status
	task.FailReason = failReason
	task.Status = "FAILURE"
	task.Progress = "100%"
	if task.FinishTime == 0 {
		task.FinishTime = time.Now().UnixMilli()
	}
	won, err := task.UpdateWithStatus(preStatus)
	if err != nil {
		logger.LogError(ctx, "UpdateMidjourneyTask task error: "+err.Error())
		return false
	}
	if !won {
		return false
	}
	if task.Quota != 0 {
		if err := service.RefundMidjourneyTaskQuota(ctx, task, failReason); err != nil {
			logger.LogError(ctx, fmt.Sprintf("midjourney task %s refund failed: %s", task.MjId, err.Error()))
		}
	}
	return true
}

func checkMjTaskNeedUpdate(oldTask *model.Midjourney, newTask dto.MidjourneyDto) bool {
	if oldTask.Code != 1 {
		return true
	}
	if oldTask.Progress != newTask.Progress {
		return true
	}
	if oldTask.PromptEn != newTask.PromptEn {
		return true
	}
	if oldTask.State != newTask.State {
		return true
	}
	if oldTask.SubmitTime != newTask.SubmitTime {
		return true
	}
	if oldTask.StartTime != newTask.StartTime {
		return true
	}
	if oldTask.FinishTime != newTask.FinishTime {
		return true
	}
	if oldTask.ImageUrl != newTask.ImageUrl {
		return true
	}
	if oldTask.Status != newTask.Status {
		return true
	}
	if oldTask.FailReason != newTask.FailReason {
		return true
	}
	if oldTask.FinishTime != newTask.FinishTime {
		return true
	}
	if oldTask.Progress != "100%" && newTask.FailReason != "" {
		return true
	}
	// 检查 VideoUrl 是否需要更新
	if oldTask.VideoUrl != newTask.VideoUrl {
		return true
	}
	// 检查 VideoUrls 是否需要更新
	if newTask.VideoUrls != nil && len(newTask.VideoUrls) > 0 {
		newVideoUrlsStr, _ := common.Marshal(newTask.VideoUrls)
		if oldTask.VideoUrls != string(newVideoUrlsStr) {
			return true
		}
	} else if oldTask.VideoUrls != "" {
		// 如果新数据没有 VideoUrls 但旧数据有，需要更新（清空）
		return true
	}

	return false
}

func GetAllMidjourney(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)

	// 解析其他查询参数
	queryParams := model.TaskQueryParams{
		ChannelID:      c.Query("channel_id"),
		MjID:           c.Query("mj_id"),
		StartTimestamp: c.Query("start_timestamp"),
		EndTimestamp:   c.Query("end_timestamp"),
	}

	items := model.GetAllTasks(pageInfo.GetStartIdx(), pageInfo.GetPageSize(), queryParams)
	total := model.CountAllTasks(queryParams)
	fillMidjourneyUsernames(items)

	if setting.MjForwardUrlEnabled {
		for i, midjourney := range items {
			midjourney.ImageUrl = system_setting.ServerAddress + "/mj/image/" + midjourney.MjId
			items[i] = midjourney
		}
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func GetUserMidjourney(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)

	userId := c.GetInt("id")

	queryParams := model.TaskQueryParams{
		MjID:           c.Query("mj_id"),
		StartTimestamp: c.Query("start_timestamp"),
		EndTimestamp:   c.Query("end_timestamp"),
	}

	items := model.GetAllUserTask(userId, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), queryParams)
	total := model.CountAllUserTask(userId, queryParams)
	fillMidjourneyUsernames(items)

	if setting.MjForwardUrlEnabled {
		for i, midjourney := range items {
			midjourney.ImageUrl = system_setting.ServerAddress + "/mj/image/" + midjourney.MjId
			items[i] = midjourney
		}
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func fillMidjourneyUsernames(items []*model.Midjourney) {
	if len(items) == 0 {
		return
	}
	usernames := make(map[int]string)
	for _, item := range items {
		if item == nil || item.UserId <= 0 {
			continue
		}
		if _, ok := usernames[item.UserId]; ok {
			continue
		}
		if user, err := model.GetUserCache(item.UserId); err == nil && user != nil {
			usernames[item.UserId] = user.Username
		}
	}
	for _, item := range items {
		if item == nil {
			continue
		}
		item.Username = usernames[item.UserId]
	}
}
