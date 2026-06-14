package model

import (
	"context"
	"time"

	"gorm.io/gorm"
)

const (
	ConversationExportStatusPending   = "pending"
	ConversationExportStatusRunning   = "running"
	ConversationExportStatusSucceeded = "succeeded"
	ConversationExportStatusFailed    = "failed"
	ConversationExportStatusExpired   = "expired"

	ConversationExportModePlain  = "plain"
	ConversationExportModeStrict = "strict"
)

type ConversationSnapshot struct {
	Id               int    `json:"id" gorm:"index:idx_conversation_snapshots_created_id,priority:1;index:idx_conversation_snapshots_created_at_id,priority:2"`
	RequestId        string `json:"request_id" gorm:"type:varchar(64);index;default:''"`
	UserId           int    `json:"user_id" gorm:"index"`
	Username         string `json:"username" gorm:"index;default:''"`
	TokenId          int    `json:"token_id" gorm:"index"`
	TokenName        string `json:"token_name" gorm:"index;default:''"`
	TokenKey         string `json:"token_key" gorm:"default:''"`
	ModelName        string `json:"model_name" gorm:"index;default:''"`
	Group            string `json:"group" gorm:"column:group_name;index;default:''"`
	RequestText      string `json:"request_text" gorm:"type:text"`
	ResponseText     string `json:"response_text" gorm:"type:text"`
	PromptTokens     int    `json:"prompt_tokens" gorm:"default:0"`
	CompletionTokens int    `json:"completion_tokens" gorm:"default:0"`
	TotalTokens      int    `json:"total_tokens" gorm:"default:0"`
	CacheTokens      int    `json:"cache_tokens" gorm:"default:0"`
	Quota            int    `json:"quota" gorm:"default:0"`
	ChannelId        int    `json:"channel_id" gorm:"index;default:0"`
	ChannelName      string `json:"channel_name" gorm:"default:''"`
	StatusCode       int    `json:"status_code" gorm:"default:0"`
	ErrorSummary     string `json:"error_summary" gorm:"type:text"`
	CreatedAt        int64  `json:"created_at" gorm:"bigint;index:idx_conversation_snapshots_created_id,priority:2;index:idx_conversation_snapshots_created_at_id,priority:1"`
}

type ConversationExportTask struct {
	Id            int    `json:"id"`
	Status        string `json:"status" gorm:"type:varchar(32);index;default:'pending'"`
	Mode          string `json:"mode" gorm:"type:varchar(16);default:'plain'"`
	Filters       string `json:"filters" gorm:"type:text"`
	Fields        string `json:"fields" gorm:"type:text"`
	FileName      string `json:"file_name" gorm:"default:''"`
	FilePath      string `json:"-" gorm:"type:text"`
	FileSize      int64  `json:"file_size" gorm:"default:0"`
	FailureReason string `json:"failure_reason" gorm:"type:text"`
	CreatedBy     int    `json:"created_by" gorm:"index"`
	CreatedAt     int64  `json:"created_at" gorm:"bigint;index"`
	StartedAt     int64  `json:"started_at" gorm:"bigint;default:0"`
	FinishedAt    int64  `json:"finished_at" gorm:"bigint;default:0"`
	ExpiresAt     int64  `json:"expires_at" gorm:"bigint;index;default:0"`
	TotalRows     int64  `json:"total_rows" gorm:"default:0"`
}

func InsertConversationSnapshot(snapshot *ConversationSnapshot) error {
	if snapshot == nil {
		return nil
	}
	return LOG_DB.Create(snapshot).Error
}

func DeleteConversationSnapshotsByTimeRange(ctx context.Context, startTimestamp int64, endTimestamp int64, limit int) (int64, error) {
	if limit <= 0 {
		limit = 1000
	}
	var total int64
	for {
		if ctx != nil && ctx.Err() != nil {
			return total, ctx.Err()
		}
		var ids []int
		if err := LOG_DB.Model(&ConversationSnapshot{}).
			Where("created_at >= ? AND created_at <= ?", startTimestamp, endTimestamp).
			Order("created_at asc, id asc").
			Limit(limit).
			Pluck("id", &ids).Error; err != nil {
			return total, err
		}
		if len(ids) == 0 {
			break
		}
		tx := LOG_DB.Where("id IN ?", ids).Delete(&ConversationSnapshot{})
		if tx.Error != nil {
			return total, tx.Error
		}
		total += tx.RowsAffected
		if tx.RowsAffected < int64(limit) {
			break
		}
	}
	return total, nil
}

func DeleteOldConversationSnapshots(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	if limit <= 0 {
		limit = 1000
	}
	var total int64
	for {
		if ctx != nil && ctx.Err() != nil {
			return total, ctx.Err()
		}
		var ids []int
		if err := LOG_DB.Model(&ConversationSnapshot{}).
			Where("created_at < ?", targetTimestamp).
			Order("created_at asc, id asc").
			Limit(limit).
			Pluck("id", &ids).Error; err != nil {
			return total, err
		}
		if len(ids) == 0 {
			break
		}
		tx := LOG_DB.Where("id IN ?", ids).Delete(&ConversationSnapshot{})
		if tx.Error != nil {
			return total, tx.Error
		}
		total += tx.RowsAffected
		if tx.RowsAffected < int64(limit) {
			break
		}
	}
	return total, nil
}

func CountConversationSnapshots(startTimestamp int64, endTimestamp int64) (int64, error) {
	var count int64
	err := LOG_DB.Model(&ConversationSnapshot{}).
		Where("created_at >= ? AND created_at <= ?", startTimestamp, endTimestamp).
		Count(&count).Error
	return count, err
}

func InsertConversationExportTask(task *ConversationExportTask) error {
	return LOG_DB.Create(task).Error
}

func GetConversationExportTask(id int) (*ConversationExportTask, error) {
	var task ConversationExportTask
	err := LOG_DB.First(&task, "id = ?", id).Error
	return &task, err
}

func UpdateConversationExportTask(id int, values map[string]interface{}) error {
	return LOG_DB.Model(&ConversationExportTask{}).Where("id = ?", id).Updates(values).Error
}

func ListConversationExportTasks(offset int, limit int) ([]*ConversationExportTask, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var total int64
	if err := LOG_DB.Model(&ConversationExportTask{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var tasks []*ConversationExportTask
	err := LOG_DB.Order("id desc").Offset(offset).Limit(limit).Find(&tasks).Error
	return tasks, total, err
}

func MarkExpiredConversationExportTasks(now time.Time) error {
	ts := now.Unix()
	return LOG_DB.Model(&ConversationExportTask{}).
		Where("status = ? AND expires_at > 0 AND expires_at < ?", ConversationExportStatusSucceeded, ts).
		Update("status", ConversationExportStatusExpired).Error
}

func DeleteConversationExportTask(task *ConversationExportTask) error {
	if task == nil {
		return nil
	}
	return LOG_DB.Delete(task).Error
}

func ConversationSnapshotQuery() *gorm.DB {
	return LOG_DB.Model(&ConversationSnapshot{})
}
