package controller

import (
	"bytes"
	"fmt"
	"html"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"golang.org/x/image/webp"
)

type createTicketRequest struct {
	Title    string `form:"title" json:"title"`
	Category string `form:"category" json:"category"`
	Priority string `form:"priority" json:"priority"`
	Content  string `form:"content" json:"content"`
}

type replyTicketRequest struct {
	Content string `form:"content" json:"content"`
}

type updateTicketRequest struct {
	Category     *string `json:"category"`
	Priority     *string `json:"priority"`
	Status       *string `json:"status"`
	AssigneeId   *int    `json:"assignee_id"`
	AssigneeName *string `json:"assignee_name"`
}

const ticketMultipartFormOverheadBytes = 2 * 1024 * 1024

type ticketBadgeCursor struct {
	LastReplyAt int64 `gorm:"column:last_reply_at"`
	ID          int   `gorm:"column:id"`
}

func ListTickets(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	isAdmin := isTicketAdminRequest(c)
	tx := model.DB.Model(&model.Ticket{})
	if !isAdmin {
		tx = tx.Where("user_id = ?", c.GetInt("id"))
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		tx = tx.Where("status = ?", status)
	}
	if category := strings.TrimSpace(c.Query("category")); category != "" {
		tx = tx.Where("category = ?", category)
	}
	if priority := strings.TrimSpace(c.Query("priority")); priority != "" {
		tx = tx.Where("priority = ?", priority)
	}
	if assigneeId := strings.TrimSpace(c.Query("assignee_id")); assigneeId != "" {
		if id, err := strconv.Atoi(assigneeId); err == nil && id >= 0 {
			tx = tx.Where("assignee_id = ?", id)
		}
	}
	if startTime := strings.TrimSpace(c.Query("start_time")); startTime != "" {
		if ts, err := strconv.ParseInt(startTime, 10, 64); err == nil && ts > 0 {
			tx = tx.Where("created_at >= ?", ts)
		}
	}
	if endTime := strings.TrimSpace(c.Query("end_time")); endTime != "" {
		if ts, err := strconv.ParseInt(endTime, 10, 64); err == nil && ts > 0 {
			tx = tx.Where("created_at <= ?", ts)
		}
	}
	if keyword := strings.TrimSpace(c.Query("keyword")); keyword != "" {
		like := "%" + keyword + "%"
		tx = tx.Where(
			"number LIKE ? OR title LIKE ? OR username LIKE ?",
			like,
			like,
			like,
		)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var tickets []*model.Ticket
	if err := tx.Order("updated_at desc, id desc").Offset(pageInfo.GetStartIdx()).Limit(pageInfo.GetPageSize()).Find(&tickets).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(tickets)
	common.ApiSuccess(c, pageInfo)
}

func GetTicket(c *gin.Context) {
	ticket, ok := loadTicketForRequest(c, false)
	if !ok {
		return
	}
	var messages []*model.TicketMessage
	if err := model.DB.Where("ticket_id = ?", ticket.Id).Order("id asc").Find(&messages).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var attachments []*model.TicketAttachment
	if err := model.DB.Where("ticket_id = ?", ticket.Id).Order("id asc").Find(&attachments).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"ticket":      ticket,
		"messages":    messages,
		"attachments": attachments,
	})
}

func CreateTicket(c *gin.Context) {
	var req createTicketRequest
	if err := bindTicketForm(c, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Category = strings.TrimSpace(req.Category)
	req.Priority = strings.TrimSpace(req.Priority)
	req.Content = strings.TrimSpace(req.Content)
	if req.Title == "" || len(req.Title) > 200 {
		common.ApiErrorMsg(c, "工单标题不能为空且不能超过 200 字")
		return
	}
	if req.Content == "" {
		common.ApiErrorMsg(c, "工单内容不能为空")
		return
	}
	if req.Priority == "" {
		req.Priority = model.TicketPriorityNormal
	}
	if !model.ValidTicketCategory(req.Category) {
		common.ApiErrorMsg(c, "工单分类不正确")
		return
	}
	if !model.ValidTicketPriority(req.Priority) {
		common.ApiErrorMsg(c, "工单优先级不正确")
		return
	}
	attachments, err := parseTicketAttachments(c, 0, c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	now := common.GetTimestamp()
	year := time.Now().In(time.FixedZone("Asia/Shanghai", 8*60*60)).Year()
	number, seq, err := model.NextTicketNumber(year)
	if err != nil {
		cleanupTicketAttachmentFiles(attachments)
		common.SysError(fmt.Sprintf("failed to generate ticket number: user_id=%d, year=%d, err=%v", c.GetInt("id"), year, err))
		common.ApiErrorMsg(c, "工单编号生成失败，请稍后重试")
		return
	}
	username := c.GetString("username")
	ticket := &model.Ticket{
		Number:         number,
		SequenceYear:   year,
		SequenceNumber: seq,
		UserId:         c.GetInt("id"),
		Username:       username,
		Title:          req.Title,
		Category:       req.Category,
		Priority:       req.Priority,
		Status:         model.TicketStatusPending,
		LastReplyAt:    now,
		LastReplyBy:    model.TicketSenderUser,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	message := &model.TicketMessage{
		UserId:    c.GetInt("id"),
		Username:  username,
		Sender:    model.TicketSenderUser,
		Content:   req.Content,
		CreatedAt: now,
	}
	if err := model.CreateTicketWithMessage(ticket, message, attachments); err != nil {
		cleanupTicketAttachmentFiles(attachments)
		common.ApiError(c, err)
		return
	}
	moveTicketAttachments(ticket.Id, message.Id, attachments)
	notifyTicketCreated(ticket)
	common.ApiSuccess(c, ticket)
}

func ReplyTicket(c *gin.Context) {
	ticket, ok := loadTicketForRequest(c, false)
	if !ok {
		return
	}
	if ticket.Status == model.TicketStatusClosed {
		common.ApiErrorMsg(c, "工单已关闭，重新打开后才能回复")
		return
	}
	var req replyTicketRequest
	if err := bindTicketForm(c, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	req.Content = strings.TrimSpace(req.Content)
	if req.Content == "" {
		common.ApiErrorMsg(c, "回复内容不能为空")
		return
	}
	isAdmin := isTicketAdminRequest(c)
	attachments, err := parseTicketAttachments(c, ticket.Id, c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	now := common.GetTimestamp()
	sender := model.TicketSenderUser
	nextStatus := model.TicketStatusPending
	if isAdmin {
		sender = model.TicketSenderAdmin
		nextStatus = model.TicketStatusAdminReplied
	}
	message := &model.TicketMessage{
		TicketId:  ticket.Id,
		UserId:    c.GetInt("id"),
		Username:  c.GetString("username"),
		Sender:    sender,
		Content:   req.Content,
		CreatedAt: now,
	}
	if err := model.AddTicketMessage(ticket, message, attachments, nextStatus); err != nil {
		cleanupTicketAttachmentFiles(attachments)
		common.ApiError(c, err)
		return
	}
	moveTicketAttachments(ticket.Id, message.Id, attachments)
	if isAdmin {
		notifyTicketAdminReply(ticket)
	}
	common.ApiSuccess(c, message)
}

func CloseTicket(c *gin.Context) {
	ticket, ok := loadTicketForRequest(c, false)
	if !ok {
		return
	}
	now := common.GetTimestamp()
	if err := model.DB.Model(ticket).Updates(map[string]interface{}{
		"status":     model.TicketStatusClosed,
		"closed_at":  now,
		"updated_at": now,
	}).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"closed": true})
}

func ReopenTicket(c *gin.Context) {
	ticket, ok := loadTicketForRequest(c, false)
	if !ok {
		return
	}
	now := common.GetTimestamp()
	if err := model.DB.Model(ticket).Updates(map[string]interface{}{
		"status":     model.TicketStatusPending,
		"closed_at":  int64(0),
		"updated_at": now,
	}).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"reopened": true})
}

func UpdateTicket(c *gin.Context) {
	ticket, ok := loadTicketForRequest(c, true)
	if !ok {
		return
	}
	var req updateTicketRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "无效的工单更新参数")
		return
	}
	now := common.GetTimestamp()
	updates := map[string]interface{}{"updated_at": now}
	if req.Category != nil {
		category := strings.TrimSpace(*req.Category)
		if !model.ValidTicketCategory(category) {
			common.ApiErrorMsg(c, "工单分类不正确")
			return
		}
		updates["category"] = category
	}
	if req.Priority != nil {
		priority := strings.TrimSpace(*req.Priority)
		if !model.ValidTicketPriority(priority) {
			common.ApiErrorMsg(c, "工单优先级不正确")
			return
		}
		updates["priority"] = priority
	}
	if req.Status != nil {
		status := strings.TrimSpace(*req.Status)
		if !model.ValidTicketStatus(status) {
			common.ApiErrorMsg(c, "工单状态不正确")
			return
		}
		updates["status"] = status
		if status == model.TicketStatusClosed {
			updates["closed_at"] = now
		} else {
			updates["closed_at"] = int64(0)
		}
	}
	if req.AssigneeId != nil {
		assigneeId := *req.AssigneeId
		if assigneeId < 0 {
			common.ApiErrorMsg(c, "指派处理人不正确")
			return
		}
		if assigneeId == 0 {
			updates["assignee_id"] = 0
			updates["assignee_name"] = ""
		} else {
			user, err := model.GetUserById(assigneeId, false)
			if err != nil {
				common.ApiErrorMsg(c, "指派处理人不存在")
				return
			}
			if user.Status != common.UserStatusEnabled || user.Role < common.RoleAdminUser {
				common.ApiErrorMsg(c, "只能指派给启用的管理员")
				return
			}
			updates["assignee_id"] = user.Id
			updates["assignee_name"] = user.Username
		}
	} else if req.AssigneeName != nil {
		common.ApiErrorMsg(c, "请通过处理人 ID 指派工单")
		return
	}
	if err := model.DB.Model(ticket).Updates(updates).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"updated": true})
}

func GetTicketBadge(c *gin.Context) {
	isAdmin := isTicketAdminRequest(c)
	todoStatuses := []string{model.TicketStatusPending}
	sender := model.TicketSenderAdmin
	if !isAdmin {
		todoStatuses = []string{model.TicketStatusWaitingUser, model.TicketStatusAdminReplied}
	} else {
		sender = model.TicketSenderUser
	}
	tx := model.DB.Model(&model.Ticket{}).Where("status IN ?", todoStatuses)
	if !isAdmin {
		tx = tx.Where("user_id = ?", c.GetInt("id"))
	}
	var count int64
	if err := tx.Count(&count).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	latestCursor, err := latestTicketBadgeCursor(isAdmin, c.GetInt("id"), todoStatuses, sender)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	newCount, err := countTicketBadgeAfterCursor(c.Query("after_cursor"), isAdmin, c.GetInt("id"), todoStatuses, sender)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"count":         count,
		"new_count":     newCount,
		"latest_cursor": formatTicketBadgeCursor(latestCursor),
	})
}

func latestTicketBadgeCursor(isAdmin bool, userID int, statuses []string, sender string) (ticketBadgeCursor, error) {
	tx := model.DB.Model(&model.Ticket{}).
		Select("last_reply_at, id").
		Where("status IN ? AND last_reply_by = ? AND last_reply_at > 0", statuses, sender)
	if !isAdmin {
		tx = tx.Where("user_id = ?", userID)
	}
	var cursor ticketBadgeCursor
	if err := tx.Order("last_reply_at desc, id desc").Limit(1).Scan(&cursor).Error; err != nil {
		return ticketBadgeCursor{}, err
	}
	return cursor, nil
}

func countTicketBadgeAfterCursor(rawCursor string, isAdmin bool, userID int, statuses []string, sender string) (int64, error) {
	cursor, ok := parseTicketBadgeCursor(rawCursor)
	if !ok {
		return 0, nil
	}
	tx := model.DB.Model(&model.Ticket{}).
		Where("status IN ? AND last_reply_by = ? AND last_reply_at > 0", statuses, sender).
		Where("last_reply_at > ? OR (last_reply_at = ? AND id > ?)", cursor.LastReplyAt, cursor.LastReplyAt, cursor.ID)
	if !isAdmin {
		tx = tx.Where("user_id = ?", userID)
	}
	var count int64
	if err := tx.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func parseTicketBadgeCursor(raw string) (ticketBadgeCursor, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ticketBadgeCursor{}, false
	}
	parts := strings.Split(raw, ":")
	if len(parts) != 2 {
		return ticketBadgeCursor{}, false
	}
	lastReplyAt, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || lastReplyAt < 0 {
		return ticketBadgeCursor{}, false
	}
	id, err := strconv.Atoi(parts[1])
	if err != nil || id < 0 {
		return ticketBadgeCursor{}, false
	}
	return ticketBadgeCursor{LastReplyAt: lastReplyAt, ID: id}, true
}

func formatTicketBadgeCursor(cursor ticketBadgeCursor) string {
	if cursor.LastReplyAt < 0 || cursor.ID < 0 {
		return ""
	}
	return fmt.Sprintf("%d:%d", cursor.LastReplyAt, cursor.ID)
}

func GetTicketAttachment(c *gin.Context) {
	ticketId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "工单 ID 不正确")
		return
	}
	attachmentId, err := strconv.Atoi(c.Param("attachment_id"))
	if err != nil {
		common.ApiErrorMsg(c, "附件 ID 不正确")
		return
	}
	var attachment model.TicketAttachment
	if err := model.DB.First(&attachment, "id = ?", attachmentId).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var ticket model.Ticket
	if err := model.DB.First(&ticket, "id = ?", attachment.TicketId).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if ticket.Id != ticketId {
		common.ApiErrorMsg(c, "附件不属于该工单")
		return
	}
	if !isTicketAdminRequest(c) && ticket.UserId != c.GetInt("id") {
		common.ApiErrorMsg(c, "无权访问该附件")
		return
	}
	path := filepath.Join(ticketAttachmentDir(), filepath.Base(attachment.StorageName))
	c.Header("Content-Type", attachment.MimeType)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "private, max-age=3600")
	c.Header("Content-Disposition", "inline; filename="+strconv.Quote(attachment.FileName))
	c.File(path)
}

func bindTicketForm(c *gin.Context, target any) error {
	contentType := c.GetHeader("Content-Type")
	if strings.Contains(strings.ToLower(contentType), "multipart/form-data") {
		maxBytes := ticketMultipartMaxRequestBytes()
		if c.Request.ContentLength > maxBytes {
			return ticketMultipartTooLargeError()
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		if err := c.ShouldBind(target); err != nil {
			if common.IsRequestBodyTooLargeError(err) {
				return ticketMultipartTooLargeError()
			}
			return err
		}
		return nil
	}
	return common.DecodeJson(c.Request.Body, target)
}

func ticketMultipartMaxRequestBytes() int64 {
	return int64(model.TicketMaxAttachmentSize*model.TicketMaxReplyAttachments + ticketMultipartFormOverheadBytes)
}

func ticketMultipartTooLargeError() error {
	return fmt.Errorf("工单附件请求不能超过 %dMB", ticketMultipartMaxRequestBytes()/(1024*1024))
}

func loadTicketForRequest(c *gin.Context, adminOnly bool) (*model.Ticket, bool) {
	adminRequest := isTicketAdminRequest(c)
	if adminOnly && !adminRequest {
		common.ApiErrorMsg(c, "无权操作该工单")
		return nil, false
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "工单 ID 不正确")
		return nil, false
	}
	var ticket model.Ticket
	if err := model.DB.First(&ticket, "id = ?", id).Error; err != nil {
		common.ApiError(c, err)
		return nil, false
	}
	if !adminRequest && ticket.UserId != c.GetInt("id") {
		common.ApiErrorMsg(c, "无权访问该工单")
		return nil, false
	}
	return &ticket, true
}

func isTicketAdminRequest(c *gin.Context) bool {
	if c == nil || c.GetInt("role") < common.RoleAdminUser {
		return false
	}
	return strings.Contains(c.FullPath(), "/admin/tickets")
}

func parseTicketAttachments(c *gin.Context, ticketId int, userId int) ([]*model.TicketAttachment, error) {
	contentType := strings.ToLower(c.GetHeader("Content-Type"))
	if !strings.Contains(contentType, "multipart/form-data") {
		return nil, nil
	}
	form, err := c.MultipartForm()
	if err != nil {
		return nil, err
	}
	if form == nil || len(form.File) == 0 {
		return nil, nil
	}
	files := form.File["attachments"]
	if len(files) == 0 {
		files = form.File["file"]
	}
	if len(files) > model.TicketMaxReplyAttachments {
		return nil, fmt.Errorf("单次最多上传 %d 张图片", model.TicketMaxReplyAttachments)
	}
	existingCount := int64(0)
	existingBytes := int64(0)
	if ticketId > 0 {
		var err error
		existingCount, existingBytes, err = model.CountTicketAttachments(ticketId)
		if err != nil {
			return nil, err
		}
		if existingCount+int64(len(files)) > model.TicketMaxAttachments {
			return nil, fmt.Errorf("单个工单最多保存 %d 张图片", model.TicketMaxAttachments)
		}
	}
	attachments := make([]*model.TicketAttachment, 0, len(files))
	fail := func(err error) ([]*model.TicketAttachment, error) {
		cleanupTicketAttachmentFiles(attachments)
		return nil, err
	}
	for _, header := range files {
		file, err := header.Open()
		if err != nil {
			return fail(err)
		}
		data, readErr := io.ReadAll(io.LimitReader(file, model.TicketMaxAttachmentSize+1))
		_ = file.Close()
		if readErr != nil {
			return fail(readErr)
		}
		if len(data) > model.TicketMaxAttachmentSize {
			return fail(fmt.Errorf("单张图片不能超过 5MB"))
		}
		if existingBytes+int64(len(data)) > model.TicketMaxTotalImageBytes {
			return fail(fmt.Errorf("单个工单图片总大小不能超过 100MB"))
		}
		mimeType, ext, width, height, err := validateTicketImage(data)
		if err != nil {
			return fail(err)
		}
		name := fmt.Sprintf("ticket-%d-%d-%s%s", userId, time.Now().UnixNano(), strings.ToLower(common.GetRandomString(8)), ext)
		if err := os.MkdirAll(ticketAttachmentDir(), 0o755); err != nil {
			return fail(err)
		}
		path := filepath.Join(ticketAttachmentDir(), name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return fail(err)
		}
		attachments = append(attachments, &model.TicketAttachment{
			UserId:      userId,
			FileName:    filepath.Base(header.Filename),
			StorageName: name,
			MimeType:    mimeType,
			Size:        int64(len(data)),
			Width:       width,
			Height:      height,
			CreatedAt:   common.GetTimestamp(),
		})
		existingBytes += int64(len(data))
	}
	return attachments, nil
}

func validateTicketImage(data []byte) (string, string, int, int, error) {
	contentType := http.DetectContentType(data)
	var cfg image.Config
	var format string
	var err error
	if contentType == "image/webp" {
		cfg, err = webp.DecodeConfig(bytes.NewReader(data))
		format = "webp"
		if err != nil {
			return "", "", 0, 0, fmt.Errorf("图片文件无效")
		}
		if err := validateTicketImageSize(cfg); err != nil {
			return "", "", 0, 0, err
		}
		_, err = webp.Decode(bytes.NewReader(data))
	} else {
		cfg, format, err = image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return "", "", 0, 0, fmt.Errorf("图片文件无效")
		}
		if !isAllowedTicketImageFormat(format) {
			return "", "", 0, 0, fmt.Errorf("只支持 png、jpg、jpeg、webp 图片")
		}
		if err := validateTicketImageSize(cfg); err != nil {
			return "", "", 0, 0, err
		}
		_, _, err = image.Decode(bytes.NewReader(data))
	}
	if err != nil {
		return "", "", 0, 0, fmt.Errorf("图片文件无效")
	}
	switch strings.ToLower(format) {
	case "png":
		return "image/png", ".png", cfg.Width, cfg.Height, nil
	case "jpeg":
		return "image/jpeg", ".jpg", cfg.Width, cfg.Height, nil
	case "webp":
		return "image/webp", ".webp", cfg.Width, cfg.Height, nil
	default:
		return "", "", 0, 0, fmt.Errorf("只支持 png、jpg、jpeg、webp 图片")
	}
}

func isAllowedTicketImageFormat(format string) bool {
	switch strings.ToLower(format) {
	case "png", "jpeg", "webp":
		return true
	default:
		return false
	}
}

func validateTicketImageSize(cfg image.Config) error {
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return fmt.Errorf("图片文件无效")
	}
	pixels := int64(cfg.Width) * int64(cfg.Height)
	if cfg.Width > model.TicketMaxImageWidth ||
		cfg.Height > model.TicketMaxImageHeight ||
		pixels > model.TicketMaxImagePixels {
		return fmt.Errorf("图片尺寸不能超过 %dx%d，且总像素不能超过 %d 万",
			model.TicketMaxImageWidth,
			model.TicketMaxImageHeight,
			model.TicketMaxImagePixels/10000,
		)
	}
	return nil
}

func ticketAttachmentDir() string {
	return filepath.Join(".", "uploads", "ticket-attachments")
}

func moveTicketAttachments(ticketId int, messageId int, attachments []*model.TicketAttachment) {
	if len(attachments) == 0 {
		return
	}
	for _, attachment := range attachments {
		_ = model.DB.Model(attachment).Updates(map[string]interface{}{
			"ticket_id":  ticketId,
			"message_id": messageId,
		}).Error
	}
}

func cleanupTicketAttachmentFiles(attachments []*model.TicketAttachment) {
	for _, attachment := range attachments {
		if attachment == nil || strings.TrimSpace(attachment.StorageName) == "" {
			continue
		}
		path := filepath.Join(ticketAttachmentDir(), filepath.Base(attachment.StorageName))
		_ = os.Remove(path)
	}
}

func notifyTicketCreated(ticket *model.Ticket) {
	if ticket == nil || !common.TicketEmailNotificationEnabled {
		return
	}
	subject := fmt.Sprintf("%s 工单提醒", common.SystemName)
	content, ok := ticketNotificationEmailHTML("有新的工单需要处理，请进入站内查看。", ticket, true)
	if !ok {
		common.SysLog("工单邮件通知跳过：站点地址未配置，无法生成站内链接")
		return
	}
	gopool.Go(func() {
		var admins []model.User
		if err := model.DB.Select("email").Where("status = ? AND role >= ?", common.UserStatusEnabled, common.RoleAdminUser).Find(&admins).Error; err != nil {
			common.SysLog("failed to query ticket notification admins: " + err.Error())
			return
		}
		for _, admin := range admins {
			if strings.TrimSpace(admin.Email) != "" {
				_ = common.SendEmail(subject, admin.Email, content)
			}
		}
	})
}

func notifyTicketAdminReply(ticket *model.Ticket) {
	if ticket == nil || !common.TicketEmailNotificationEnabled {
		return
	}
	subject := fmt.Sprintf("%s 工单提醒", common.SystemName)
	content, ok := ticketNotificationEmailHTML("管理员已回复你的工单，请进入站内查看。", ticket, false)
	if !ok {
		common.SysLog("工单邮件通知跳过：站点地址未配置，无法生成站内链接")
		return
	}
	gopool.Go(func() {
		if email, err := model.GetUserEmail(ticket.UserId); err == nil && strings.TrimSpace(email) != "" {
			_ = common.SendEmail(subject, email, content)
		}
	})
}

func ticketNotificationEmailHTML(message string, ticket *model.Ticket, admin bool) (string, bool) {
	link, ok := ticketLink(ticket, admin)
	if !ok {
		return "", false
	}
	targetName := "工单中心"
	if admin {
		targetName = "工单管理"
	}
	return fmt.Sprintf(
		"<p>%s</p><p><a href=\"%s\">点击进入%s查看</a></p>",
		html.EscapeString(message),
		html.EscapeString(link),
		html.EscapeString(targetName),
	), true
}

func ticketLink(ticket *model.Ticket, admin bool) (string, bool) {
	base := strings.TrimRight(strings.TrimSpace(system_setting.ServerAddress), "/")
	if base == "" {
		return "", false
	}
	if ticket != nil && ticket.Id > 0 {
		if admin {
			return base + common.ThemeAwarePath(fmt.Sprintf("/console/admin-tickets?ticket_id=%d", ticket.Id)), true
		}
		return base + common.ThemeAwarePath(fmt.Sprintf("/console/tickets?ticket_id=%d", ticket.Id)), true
	}
	if admin {
		return base + common.ThemeAwarePath("/console/admin-tickets"), true
	}
	return base + common.ThemeAwarePath("/console/tickets"), true
}
