package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
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
		common.ApiErrorI18n(c, i18n.MsgTelegramPushInvalidConfig)
		return
	}
	displayName := service.NormalizeTelegramPushDisplayName(req.DisplayName)
	if strings.ContainsAny(displayName, "\r\n") {
		common.ApiErrorI18n(c, i18n.MsgTelegramPushNameNewline)
		return
	}
	if len([]rune(displayName)) > 32 {
		common.ApiErrorI18n(c, i18n.MsgTelegramPushNameTooLong)
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
		common.ApiErrorI18n(c, i18n.MsgTelegramPushInvalidTest)
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		text = i18n.T(c, i18n.MsgTelegramPushTestSuccess)
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
		common.ApiErrorI18n(c, i18n.MsgTelegramPushInvalidAnnouncement)
		return
	}
	if strings.TrimSpace(req.Title) == "" && strings.TrimSpace(req.Content) == "" {
		common.ApiErrorI18n(c, i18n.MsgTelegramPushAnnouncementEmpty)
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
	for _, record := range records {
		if record == nil {
			continue
		}
		record.FailureReason = localizeTelegramFailureReason(c, record.FailureReason)
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(records)
	common.ApiSuccess(c, pageInfo)
}

func localizeTelegramFailureReason(c *gin.Context, reason string) string {
	reason = strings.TrimSpace(reason)
	switch reason {
	case "":
		return ""
	case i18n.MsgTelegramPushInvalidConfig, "Telegram Bot Token 和 Chat ID 不能为空":
		return i18n.T(c, i18n.MsgTelegramPushInvalidConfig)
	case i18n.MsgTelegramPushContentEmpty, "推送内容不能为空":
		return i18n.T(c, i18n.MsgTelegramPushContentEmpty)
	case i18n.MsgTelegramPushInterrupted, "推送任务中断，等待自动重试":
		return i18n.T(c, i18n.MsgTelegramPushInterrupted)
	case i18n.MsgTelegramPushSendFailed:
		return i18n.T(c, i18n.MsgTelegramPushSendFailed)
	default:
		if strings.HasPrefix(reason, "Telegram 推送失败") {
			return i18n.T(c, i18n.MsgTelegramPushSendFailed)
		}
		return reason
	}
}

func RetryTelegramPushRecord(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgTelegramPushRecordIdInvalid)
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
		common.ApiErrorI18n(c, i18n.MsgTelegramPushRetryFailedOnly)
		return
	}
	startTelegramPushRecord(id)
	common.ApiSuccess(c, gin.H{"retrying": true})
}
