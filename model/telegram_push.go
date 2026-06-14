package model

const (
	TelegramPushStatusPending   = "pending"
	TelegramPushStatusRunning   = "running"
	TelegramPushStatusSucceeded = "succeeded"
	TelegramPushStatusFailed    = "failed"
	TelegramPushSourceManual    = "manual"
	TelegramPushSourceAuto      = "auto"
)

type TelegramPushRecord struct {
	Id             int    `json:"id"`
	AnnouncementId string `json:"announcement_id" gorm:"type:varchar(64);index;default:''"`
	Title          string `json:"title" gorm:"type:text"`
	Content        string `json:"content" gorm:"type:text"`
	ChatId         string `json:"chat_id" gorm:"type:varchar(128);index"`
	DisplayName    string `json:"display_name" gorm:"type:varchar(64);default:''"`
	Source         string `json:"source" gorm:"type:varchar(32);index;default:'manual'"`
	Status         string `json:"status" gorm:"type:varchar(32);index"`
	AttemptCount   int    `json:"attempt_count" gorm:"default:0"`
	FailureReason  string `json:"failure_reason" gorm:"type:text"`
	CreatedAt      int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt      int64  `json:"updated_at" gorm:"bigint;index"`
	SentAt         int64  `json:"sent_at" gorm:"bigint;default:0"`
}

func ListRetryableTelegramPushRecords(maxAttempts int, limit int) ([]*TelegramPushRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	var records []*TelegramPushRecord
	err := DB.Where(
		"(status IN ? AND attempt_count < ?) OR status = ?",
		[]string{TelegramPushStatusPending, TelegramPushStatusFailed},
		maxAttempts,
		TelegramPushStatusRunning,
	).
		Order("updated_at asc").
		Limit(limit).
		Find(&records).Error
	return records, err
}
