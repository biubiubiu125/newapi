package model

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	TicketCategoryCustomer = "客服部门"
	TicketCategoryFinance  = "财务部门"

	TicketStatusPending       = "待处理"
	TicketStatusProcessing    = "处理中"
	TicketStatusWaitingUser   = "等待用户回复"
	TicketStatusAdminReplied  = "管理员已回复"
	TicketStatusResolved      = "已解决"
	TicketStatusClosed        = "已关闭"
	TicketPriorityLow         = "低"
	TicketPriorityNormal      = "普通"
	TicketPriorityHigh        = "高"
	TicketPriorityUrgent      = "紧急"
	TicketSenderUser          = "user"
	TicketSenderAdmin         = "admin"
	TicketMaxAttachmentSize   = 5 * 1024 * 1024
	TicketMaxReplyAttachments = 5
	TicketMaxAttachments      = 30
	TicketMaxTotalImageBytes  = 100 * 1024 * 1024
	TicketMaxImageWidth       = 8192
	TicketMaxImageHeight      = 8192
	TicketMaxImagePixels      = 25 * 1000 * 1000
)

type Ticket struct {
	Id             int            `json:"id"`
	Number         string         `json:"number" gorm:"type:varchar(64);uniqueIndex"`
	SequenceYear   int            `json:"sequence_year" gorm:"index"`
	SequenceNumber int            `json:"sequence_number" gorm:"index"`
	UserId         int            `json:"user_id" gorm:"index"`
	Username       string         `json:"username" gorm:"index;default:''"`
	Title          string         `json:"title" gorm:"type:varchar(200)"`
	Category       string         `json:"category" gorm:"type:varchar(32);index"`
	Priority       string         `json:"priority" gorm:"type:varchar(16);index"`
	Status         string         `json:"status" gorm:"type:varchar(32);index"`
	AssigneeId     int            `json:"assignee_id" gorm:"index;default:0"`
	AssigneeName   string         `json:"assignee_name" gorm:"default:''"`
	LastReplyAt    int64          `json:"last_reply_at" gorm:"bigint;index"`
	LastReplyBy    string         `json:"last_reply_by" gorm:"type:varchar(16);default:''"`
	CreatedAt      int64          `json:"created_at" gorm:"bigint;index"`
	UpdatedAt      int64          `json:"updated_at" gorm:"bigint;index"`
	ClosedAt       int64          `json:"closed_at" gorm:"bigint;default:0"`
	DeletedAt      gorm.DeletedAt `json:"-" gorm:"index"`
}

type TicketMessage struct {
	Id        int    `json:"id"`
	TicketId  int    `json:"ticket_id" gorm:"index"`
	UserId    int    `json:"user_id" gorm:"index"`
	Username  string `json:"username" gorm:"default:''"`
	Sender    string `json:"sender" gorm:"type:varchar(16);index"`
	Content   string `json:"content" gorm:"type:text"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index"`
}

type TicketAttachment struct {
	Id          int    `json:"id"`
	TicketId    int    `json:"ticket_id" gorm:"index"`
	MessageId   int    `json:"message_id" gorm:"index"`
	UserId      int    `json:"user_id" gorm:"index"`
	FileName    string `json:"file_name" gorm:"type:varchar(255)"`
	StorageName string `json:"-" gorm:"type:varchar(255);uniqueIndex"`
	MimeType    string `json:"mime_type" gorm:"type:varchar(64)"`
	Size        int64  `json:"size"`
	Width       int    `json:"width" gorm:"default:0"`
	Height      int    `json:"height" gorm:"default:0"`
	CreatedAt   int64  `json:"created_at" gorm:"bigint;index"`
}

type TicketSequence struct {
	Year      int `gorm:"primaryKey"`
	NextSeq   int `gorm:"default:1000097"`
	UpdatedAt int64
}

func ValidTicketCategory(category string) bool {
	return category == TicketCategoryCustomer || category == TicketCategoryFinance
}

func ValidTicketStatus(status string) bool {
	switch status {
	case TicketStatusPending, TicketStatusProcessing, TicketStatusWaitingUser, TicketStatusAdminReplied, TicketStatusResolved, TicketStatusClosed:
		return true
	default:
		return false
	}
}

func ValidTicketPriority(priority string) bool {
	switch priority {
	case TicketPriorityLow, TicketPriorityNormal, TicketPriorityHigh, TicketPriorityUrgent:
		return true
	default:
		return false
	}
}

func NextTicketNumber(year int) (string, int, error) {
	const maxAttempts = 3
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		number, seq, err := nextTicketNumberOnce(year)
		if err == nil {
			return number, seq, nil
		}
		lastErr = err
		if !isTicketSequenceCreateConflict(err) {
			break
		}
	}
	return "", 0, lastErr
}

func nextTicketNumberOnce(year int) (string, int, error) {
	var seq int
	err := DB.Transaction(func(tx *gorm.DB) error {
		var ticketSeq TicketSequence
		now := time.Now().Unix()
		if err := tx.Where("year = ?", year).FirstOrCreate(
			&ticketSeq,
			TicketSequence{Year: year, NextSeq: 1000097, UpdatedAt: now},
		).Error; err != nil {
			return err
		}
		result := tx.Model(&TicketSequence{}).
			Where("year = ?", year).
			Updates(map[string]interface{}{
				"next_seq":   gorm.Expr("next_seq + ?", 1),
				"updated_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		if err := tx.First(&ticketSeq, "year = ?", year).Error; err != nil {
			return err
		}
		seq = ticketSeq.NextSeq - 1
		return nil
	})
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("RKAPI%d-%d", year, seq), seq, nil
}

func isTicketSequenceCreateConflict(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate") ||
		strings.Contains(message, "unique constraint") ||
		strings.Contains(message, "constraint failed")
}

func CreateTicketWithMessage(ticket *Ticket, message *TicketMessage, attachments []*TicketAttachment) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if len(attachments) > TicketMaxAttachments {
			return fmt.Errorf("单个工单最多保存 %d 张图片", TicketMaxAttachments)
		}
		totalBytes := int64(0)
		for _, attachment := range attachments {
			totalBytes += attachment.Size
		}
		if totalBytes > TicketMaxTotalImageBytes {
			return fmt.Errorf("单个工单图片总大小不能超过 100MB")
		}
		if err := tx.Create(ticket).Error; err != nil {
			return err
		}
		message.TicketId = ticket.Id
		if err := tx.Create(message).Error; err != nil {
			return err
		}
		for _, attachment := range attachments {
			attachment.TicketId = ticket.Id
			attachment.MessageId = message.Id
			if err := tx.Create(attachment).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func AddTicketMessage(ticket *Ticket, message *TicketMessage, attachments []*TicketAttachment, nextStatus string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if len(attachments) > 0 {
			var locked Ticket
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, "id = ?", ticket.Id).Error; err != nil {
				return err
			}
			existingCount, existingBytes, err := CountTicketAttachmentsTx(tx, ticket.Id)
			if err != nil {
				return err
			}
			addBytes := int64(0)
			for _, attachment := range attachments {
				addBytes += attachment.Size
			}
			if existingCount+int64(len(attachments)) > TicketMaxAttachments {
				return fmt.Errorf("单个工单最多保存 %d 张图片", TicketMaxAttachments)
			}
			if existingBytes+addBytes > TicketMaxTotalImageBytes {
				return fmt.Errorf("单个工单图片总大小不能超过 100MB")
			}
		}
		if err := tx.Create(message).Error; err != nil {
			return err
		}
		for _, attachment := range attachments {
			attachment.TicketId = ticket.Id
			attachment.MessageId = message.Id
			if err := tx.Create(attachment).Error; err != nil {
				return err
			}
		}
		updates := map[string]interface{}{
			"last_reply_at": message.CreatedAt,
			"last_reply_by": message.Sender,
			"updated_at":    message.CreatedAt,
		}
		if nextStatus != "" {
			updates["status"] = nextStatus
			if nextStatus != TicketStatusClosed {
				updates["closed_at"] = int64(0)
			}
		}
		return tx.Model(&Ticket{}).Where("id = ?", ticket.Id).Updates(updates).Error
	})
}

func CountTicketAttachments(ticketId int) (int64, int64, error) {
	return CountTicketAttachmentsTx(DB, ticketId)
}

func CountTicketAttachmentsTx(tx *gorm.DB, ticketId int) (int64, int64, error) {
	var count int64
	var totalBytes int64
	if tx == nil {
		tx = DB
	}
	err := tx.Model(&TicketAttachment{}).
		Where("ticket_id = ?", ticketId).
		Count(&count).Error
	if err != nil {
		return 0, 0, err
	}
	err = tx.Model(&TicketAttachment{}).
		Where("ticket_id = ?", ticketId).
		Select("COALESCE(SUM(size), 0)").
		Scan(&totalBytes).Error
	return count, totalBytes, err
}
