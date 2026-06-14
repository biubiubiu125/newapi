package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	TelegramPushMaxAttempts = 3
	telegramPushRetryDelay  = time.Minute
	telegramPushRetryTick   = time.Minute
	telegramPushRetryLimit  = 50
)

var (
	telegramRetryOnce       sync.Once
	telegramRetryRunning    atomic.Bool
	startTelegramPushRecord = func(recordId int) {
		go RunTelegramPushRecord(recordId)
	}
)

func NormalizeTelegramPushDisplayName(displayName string) string {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return "RKAPI"
	}
	return displayName
}

func NormalizeTelegramPushSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case model.TelegramPushSourceAuto:
		return model.TelegramPushSourceAuto
	default:
		return model.TelegramPushSourceManual
	}
}

func BuildTelegramPushText(displayName string, title string, content string) string {
	displayName = NormalizeTelegramPushDisplayName(displayName)
	title = strings.TrimSpace(title)
	content = strings.TrimSpace(content)
	prefix := "[" + displayName + "]"
	if title != "" {
		header := "<b>" + html.EscapeString(prefix+title) + "</b>"
		if content != "" {
			return header + "\n" + html.EscapeString(content)
		}
		return header
	}
	if content != "" {
		return "<b>" + html.EscapeString(prefix) + "</b>" + html.EscapeString(content)
	}
	return ""
}

func SendTelegramPush(botToken string, chatId string, displayName string, title string, content string) error {
	botToken = strings.TrimSpace(botToken)
	chatId = strings.TrimSpace(chatId)
	if botToken == "" || chatId == "" {
		return fmt.Errorf("Telegram Bot Token 和 Chat ID 不能为空")
	}
	text := BuildTelegramPushText(displayName, title, content)
	if text == "" {
		return fmt.Errorf("推送内容不能为空")
	}
	form := url.Values{}
	form.Set("chat_id", chatId)
	form.Set("text", text)
	form.Set("parse_mode", "HTML")
	endpoint := "https://api.telegram.org/bot" + botToken + "/sendMessage"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := GetHttpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		reason := strings.TrimSpace(string(body))
		if reason != "" {
			return fmt.Errorf("Telegram 推送失败，HTTP 状态码 %d: %s", resp.StatusCode, reason)
		}
		return fmt.Errorf("Telegram 推送失败，HTTP 状态码 %d", resp.StatusCode)
	}
	return nil
}

func CreateTelegramPushRecord(announcementId string, title string, content string, source string) (*model.TelegramPushRecord, error) {
	now := common.GetTimestamp()
	record := &model.TelegramPushRecord{
		AnnouncementId: strings.TrimSpace(announcementId),
		Title:          title,
		Content:        content,
		ChatId:         common.TelegramPushChatId,
		DisplayName:    NormalizeTelegramPushDisplayName(common.TelegramPushDisplayName),
		Source:         NormalizeTelegramPushSource(source),
		Status:         model.TelegramPushStatusPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := model.DB.Create(record).Error; err != nil {
		return nil, err
	}
	startTelegramPushRecord(record.Id)
	return record, nil
}

type announcementForTelegramPush struct {
	Id      json.RawMessage `json:"id"`
	Title   string          `json:"title"`
	Content string          `json:"content"`
}

func (announcement announcementForTelegramPush) idString(index int) string {
	raw := strings.TrimSpace(string(announcement.Id))
	if raw == "" || raw == "null" {
		return fmt.Sprintf("index:%d", index)
	}
	var text string
	if err := json.Unmarshal(announcement.Id, &text); err == nil {
		text = strings.TrimSpace(text)
		if text != "" {
			return text
		}
	}
	return raw
}

func parseAnnouncementsForTelegramPush(raw string) ([]announcementForTelegramPush, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, err
	}
	announcements := make([]announcementForTelegramPush, 0, len(entries))
	for index, entry := range entries {
		var legacyContent string
		if err := json.Unmarshal(entry, &legacyContent); err == nil {
			announcements = append(announcements, announcementForTelegramPush{
				Id:      json.RawMessage(fmt.Sprintf("%d", index+1)),
				Content: legacyContent,
			})
			continue
		}
		var announcement announcementForTelegramPush
		if err := json.Unmarshal(entry, &announcement); err != nil {
			return nil, err
		}
		announcements = append(announcements, announcement)
	}
	return announcements, nil
}

func announcementTelegramFingerprint(announcement announcementForTelegramPush) string {
	return strings.TrimSpace(announcement.Title) + "\n" + strings.TrimSpace(announcement.Content)
}

func AutoPushChangedAnnouncements(previousRaw string, nextRaw string) (int, error) {
	previous, err := parseAnnouncementsForTelegramPush(previousRaw)
	if err != nil {
		return 0, err
	}
	next, err := parseAnnouncementsForTelegramPush(nextRaw)
	if err != nil {
		return 0, err
	}
	previousById := make(map[string]string, len(previous))
	for index, announcement := range previous {
		previousById[announcement.idString(index)] = announcementTelegramFingerprint(announcement)
	}
	created := 0
	for index, announcement := range next {
		title := strings.TrimSpace(announcement.Title)
		content := strings.TrimSpace(announcement.Content)
		if title == "" && content == "" {
			continue
		}
		id := announcement.idString(index)
		if previousById[id] == announcementTelegramFingerprint(announcement) {
			continue
		}
		if _, err := CreateTelegramPushRecord(id, announcement.Title, announcement.Content, model.TelegramPushSourceAuto); err != nil {
			return created, err
		}
		created++
	}
	return created, nil
}

func RunTelegramPushRecord(recordId int) {
	var record model.TelegramPushRecord
	if err := model.DB.First(&record, "id = ?", recordId).Error; err != nil {
		common.SysLog("failed to get telegram push record: " + err.Error())
		return
	}
	if record.AttemptCount >= TelegramPushMaxAttempts {
		return
	}
	now := common.GetTimestamp()
	tx := model.DB.Model(&model.TelegramPushRecord{}).
		Where("id = ? AND status IN ?", record.Id, []string{model.TelegramPushStatusPending, model.TelegramPushStatusFailed}).
		Updates(map[string]interface{}{
			"status":        model.TelegramPushStatusRunning,
			"attempt_count": record.AttemptCount + 1,
			"updated_at":    now,
		})
	if tx.Error != nil {
		common.SysLog("failed to mark telegram push record running: " + tx.Error.Error())
		return
	}
	if tx.RowsAffected == 0 {
		return
	}
	record.AttemptCount++
	record.Status = model.TelegramPushStatusRunning
	record.UpdatedAt = now
	chatId := strings.TrimSpace(record.ChatId)
	if chatId == "" {
		chatId = common.TelegramPushChatId
	}
	displayName := record.DisplayName
	if strings.TrimSpace(displayName) == "" {
		displayName = common.TelegramPushDisplayName
	}
	err := SendTelegramPush(common.TelegramPushBotToken, chatId, displayName, record.Title, record.Content)
	if err != nil {
		_ = model.DB.Model(&model.TelegramPushRecord{}).Where("id = ?", record.Id).Updates(map[string]interface{}{
			"status":         model.TelegramPushStatusFailed,
			"failure_reason": err.Error(),
			"updated_at":     common.GetTimestamp(),
		}).Error
		if record.AttemptCount < TelegramPushMaxAttempts {
			nextAttempt := record.AttemptCount
			time.AfterFunc(time.Duration(nextAttempt)*telegramPushRetryDelay, func() {
				RunTelegramPushRecord(record.Id)
			})
		}
		return
	}
	_ = model.DB.Model(&model.TelegramPushRecord{}).Where("id = ?", record.Id).Updates(map[string]interface{}{
		"status":         model.TelegramPushStatusSucceeded,
		"failure_reason": "",
		"sent_at":        common.GetTimestamp(),
		"updated_at":     common.GetTimestamp(),
	}).Error
}

func StartTelegramPushRetryTask() {
	telegramRetryOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			runTelegramPushRetryOnce()
			ticker := time.NewTicker(telegramPushRetryTick)
			defer ticker.Stop()
			for range ticker.C {
				runTelegramPushRetryOnce()
			}
		})
	})
}

func runTelegramPushRetryOnce() {
	if !telegramRetryRunning.CompareAndSwap(false, true) {
		return
	}
	defer telegramRetryRunning.Store(false)

	records, err := model.ListRetryableTelegramPushRecords(TelegramPushMaxAttempts, telegramPushRetryLimit)
	if err != nil {
		common.SysLog("telegram push retry scan failed: " + err.Error())
		return
	}
	now := common.GetTimestamp()
	for _, record := range records {
		if record == nil || record.Id <= 0 {
			continue
		}
		if record.Status == model.TelegramPushStatusRunning {
			if record.UpdatedAt+int64(telegramPushRetryDelay/time.Second) > now {
				continue
			}
			_ = model.DB.Model(&model.TelegramPushRecord{}).
				Where("id = ? AND status = ?", record.Id, model.TelegramPushStatusRunning).
				Updates(map[string]interface{}{
					"status":         model.TelegramPushStatusFailed,
					"failure_reason": "推送任务中断，等待自动重试",
					"updated_at":     now,
				}).Error
			record.Status = model.TelegramPushStatusFailed
			record.UpdatedAt = now
		}
		if record.Status == model.TelegramPushStatusFailed {
			delaySeconds := int64(record.AttemptCount) * int64(telegramPushRetryDelay/time.Second)
			if delaySeconds > 0 && record.UpdatedAt+delaySeconds > now {
				continue
			}
		}
		recordId := record.Id
		gopool.Go(func() {
			defer func() {
				if r := recover(); r != nil {
					common.SysLog(fmt.Sprintf("telegram push retry panic: %v", r))
				}
			}()
			RunTelegramPushRecord(recordId)
		})
	}
}
