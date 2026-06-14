package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type telegramPushSettingsRequest struct {
	BotToken    string `json:"bot_token"`
	ChatId      string `json:"chat_id"`
	DisplayName string `json:"display_name"`
}

type telegramPushTestRequest struct {
	Text string `json:"text"`
}

type telegramPushAnnouncementRequest struct {
	AnnouncementId string `json:"announcement_id"`
	Title          string `json:"title"`
	Content        string `json:"content"`
}

var startTelegramPushRecord = func(recordId int) {
	go service.RunTelegramPushRecord(recordId)
}

func GetTelegramPushSettings(c *gin.Context) {
	common.ApiSuccess(c, gin.H{
		"bot_token":    common.TelegramPushBotToken,
		"chat_id":      common.TelegramPushChatId,
		"display_name": service.NormalizeTelegramPushDisplayName(common.TelegramPushDisplayName),
	})
}

func UpdateTelegramPushSettings(c *gin.Context) {
	var req telegramPushSettingsRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "无效的 Telegram 推送配置")
		return
	}
	displayName := service.NormalizeTelegramPushDisplayName(req.DisplayName)
	if strings.ContainsAny(displayName, "\r\n") {
		common.ApiErrorMsg(c, "项目显示名称不能包含换行")
		return
	}
	if len([]rune(displayName)) > 32 {
		common.ApiErrorMsg(c, "项目显示名称不能超过 32 个字符")
		return
	}
	if err := model.UpdateOption("TelegramPushBotToken", strings.TrimSpace(req.BotToken)); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpdateOption("TelegramPushChatId", strings.TrimSpace(req.ChatId)); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpdateOption("TelegramPushDisplayName", displayName); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"saved": true})
}

func TestTelegramPush(c *gin.Context) {
	var req telegramPushTestRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "无效的测试内容")
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		text = "Telegram 推送测试成功"
	}
	if err := service.SendTelegramPush(common.TelegramPushBotToken, common.TelegramPushChatId, common.TelegramPushDisplayName, text, ""); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"sent": true})
}

func PushAnnouncementToTelegram(c *gin.Context) {
	var req telegramPushAnnouncementRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "无效的公告推送内容")
		return
	}
	if strings.TrimSpace(req.Title) == "" && strings.TrimSpace(req.Content) == "" {
		common.ApiErrorMsg(c, "公告标题和内容不能同时为空")
		return
	}
	record, err := service.CreateTelegramPushRecord(req.AnnouncementId, req.Title, req.Content, model.TelegramPushSourceManual)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, record)
}

func ListTelegramPushRecords(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	var total int64
	if err := model.DB.Model(&model.TelegramPushRecord{}).Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var records []*model.TelegramPushRecord
	if err := model.DB.Order("id desc").Offset(pageInfo.GetStartIdx()).Limit(pageInfo.GetPageSize()).Find(&records).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(records)
	common.ApiSuccess(c, pageInfo)
}

func RetryTelegramPushRecord(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "推送记录 ID 不正确")
		return
	}
	tx := model.DB.Model(&model.TelegramPushRecord{}).
		Where("id = ? AND status = ?", id, model.TelegramPushStatusFailed).
		Updates(map[string]interface{}{
			"status":         model.TelegramPushStatusPending,
			"attempt_count":  0,
			"failure_reason": "",
			"sent_at":        0,
			"updated_at":     common.GetTimestamp(),
		})
	if tx.Error != nil {
		common.ApiError(c, tx.Error)
		return
	}
	if tx.RowsAffected == 0 {
		common.ApiErrorMsg(c, "只有失败的推送记录可以重试")
		return
	}
	startTelegramPushRecord(id)
	common.ApiSuccess(c, gin.H{"retrying": true})
}
