package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	ConsumptionAuditPending      = "pending"
	ConsumptionAuditUsageApplied = "usage_applied"
	ConsumptionAuditDone         = "done"
)

var (
	consumptionAuditSchemaMu      sync.Mutex
	consumptionAuditSchemaReady   sync.Map
	consumptionAuditLogIndexReady sync.Map

	// ErrConsumptionAuditConflict means this request id was already stored with different counters.
	ErrConsumptionAuditConflict = errors.New("consumption audit conflict")

	// consumptionAuditBeforeLogInsert overlaps two log inserts in tests.
	// Production leaves it nil.
	consumptionAuditBeforeLogInsert func()
)

// ConsumptionAudit remembers a delivered text, audio, or realtime charge whose
// usage counters or consume log still need to be written. The counter update and
// the status change commit together, so recovery cannot count the same request twice.
type ConsumptionAudit struct {
	ID             int64  `json:"id" gorm:"primaryKey"`
	IdempotencyKey string `json:"idempotency_key" gorm:"size:191;uniqueIndex;not null"`
	RequestID      string `json:"request_id" gorm:"size:191;index"`
	UserID         int    `json:"user_id" gorm:"index"`
	ChannelID      int    `json:"channel_id"`
	TokenID        int    `json:"token_id"`
	Quota          int    `json:"quota"`
	Status         string `json:"status" gorm:"size:32;index"`
	Payload        string `json:"payload" gorm:"type:text"`
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
}

func (ConsumptionAudit) TableName() string {
	return "consumption_audits"
}

// ConsumptionAuditPayload is the consume log captured when the result was delivered.
type ConsumptionAuditPayload struct {
	Username          string `json:"username"`
	RequestID         string `json:"request_id"`
	UpstreamRequestID string `json:"upstream_request_id"`
	Ip                string `json:"ip"`
	PromptTokens      int    `json:"prompt_tokens"`
	CompletionTokens  int    `json:"completion_tokens"`
	ModelName         string `json:"model_name"`
	TokenName         string `json:"token_name"`
	Quota             int    `json:"quota"`
	Content           string `json:"content"`
	UseTimeSeconds    int    `json:"use_time_seconds"`
	IsStream          bool   `json:"is_stream"`
	Group             string `json:"group"`
	Other             string `json:"other"`
}

// ConsumptionAuditRequest is one delivered charge that still needs usage and a log.
type ConsumptionAuditRequest struct {
	IdempotencyKey string
	RequestID      string
	UserID         int
	ChannelID      int
	TokenID        int
	Quota          int
	Payload        ConsumptionAuditPayload
}

// EnsureConsumptionAuditSchema creates the retry table when startup migration has not run.
func EnsureConsumptionAuditSchema(db *gorm.DB) error {
	if db == nil {
		return errors.New("database is required")
	}
	if _, ok := consumptionAuditSchemaReady.Load(db); ok {
		return nil
	}
	consumptionAuditSchemaMu.Lock()
	defer consumptionAuditSchemaMu.Unlock()
	if _, ok := consumptionAuditSchemaReady.Load(db); ok {
		return nil
	}
	if !db.Migrator().HasTable(&ConsumptionAudit{}) {
		if err := db.AutoMigrate(&ConsumptionAudit{}); err != nil {
			return err
		}
	}
	consumptionAuditSchemaReady.Store(db, true)
	return nil
}

// NewConsumptionAuditRequest snapshots the log fields while the request context still exists.
func NewConsumptionAuditRequest(c *gin.Context, requestID string, userID int, params RecordConsumeLogParams) ConsumptionAuditRequest {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" && c != nil {
		requestID = strings.TrimSpace(c.GetString(common.RequestIdKey))
	}
	if requestID == "" {
		requestID = common.NewRequestId()
	}
	upstreamRequestID := ""
	if c != nil {
		upstreamRequestID = c.GetString(common.UpstreamRequestIdKey)
	}
	ip := ""
	if c != nil && userID > 0 {
		if settingMap, err := GetUserSetting(userID, false); err == nil && settingMap.RecordIpLog {
			ip = common.GetClientIP(c)
		}
	}
	other := ""
	if params.Other != nil {
		other = params.Other.JSONString()
	}
	return ConsumptionAuditRequest{
		IdempotencyKey: "audit:" + requestID,
		RequestID:      requestID,
		UserID:         userID,
		ChannelID:      params.ChannelId,
		TokenID:        params.TokenId,
		Quota:          params.Quota,
		Payload: ConsumptionAuditPayload{
			Username:          resolveLogUsername(c, userID),
			RequestID:         requestID,
			UpstreamRequestID: upstreamRequestID,
			Ip:                ip,
			PromptTokens:      params.PromptTokens,
			CompletionTokens:  params.CompletionTokens,
			ModelName:         params.ModelName,
			TokenName:         params.TokenName,
			Quota:             params.Quota,
			Content:           params.Content,
			UseTimeSeconds:    params.UseTimeSeconds,
			IsStream:          params.IsStream,
			Group:             params.Group,
			Other:             other,
		},
	}
}

func (r ConsumptionAuditRequest) validate() error {
	if strings.TrimSpace(r.IdempotencyKey) == "" {
		return errors.New("consumption audit key is empty")
	}
	if r.UserID <= 0 {
		return errors.New("consumption audit user is empty")
	}
	if r.Quota < 0 {
		return errors.New("consumption audit quota cannot be negative")
	}
	return nil
}

// EnsureConsumptionAudit stores the delivered log payload without moving counters.
func EnsureConsumptionAudit(req ConsumptionAuditRequest) error {
	if err := req.validate(); err != nil {
		return err
	}
	if err := EnsureConsumptionAuditSchema(DB); err != nil {
		return err
	}
	payload, err := json.Marshal(req.Payload)
	if err != nil {
		return err
	}
	var saveErr error
	for attempt := 0; attempt < 2; attempt++ {
		saveErr = DB.Transaction(func(tx *gorm.DB) error {
			row, err := lockOrCreateConsumptionAudit(tx, req, string(payload))
			if err != nil {
				return err
			}
			if row.UserID != req.UserID || row.ChannelID != req.ChannelID || row.TokenID != req.TokenID || row.Quota != req.Quota {
				return ErrConsumptionAuditConflict
			}
			switch row.Status {
			case ConsumptionAuditPending, ConsumptionAuditUsageApplied, ConsumptionAuditDone:
				return nil
			default:
				return fmt.Errorf("unknown consumption audit status %q", row.Status)
			}
		})
		if saveErr == nil || !errors.Is(saveErr, gorm.ErrDuplicatedKey) {
			return saveErr
		}
	}
	return saveErr
}

// StoreConsumptionAuditUsageApplied records a delivered charge whose usage counters were already written.
// Recovery then only finishes the consume log and does not count the request again.
func StoreConsumptionAuditUsageApplied(req ConsumptionAuditRequest) error {
	if err := req.validate(); err != nil {
		return err
	}
	if err := EnsureConsumptionAuditSchema(DB); err != nil {
		return err
	}
	payload, err := json.Marshal(req.Payload)
	if err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		row, err := lockOrCreateConsumptionAudit(tx, req, string(payload))
		if err != nil {
			return err
		}
		if row.UserID != req.UserID || row.ChannelID != req.ChannelID || row.TokenID != req.TokenID || row.Quota != req.Quota {
			return ErrConsumptionAuditConflict
		}
		switch row.Status {
		case ConsumptionAuditUsageApplied, ConsumptionAuditDone:
			return nil
		case ConsumptionAuditPending:
			row.Status = ConsumptionAuditUsageApplied
			row.UpdatedAt = common.GetTimestamp()
			return tx.Save(row).Error
		default:
			return fmt.Errorf("unknown consumption audit status %q", row.Status)
		}
	})
}

// ApplyConsumptionAuditUsage adds the usage counters once and marks the row.
func ApplyConsumptionAuditUsage(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("consumption audit key is empty")
	}
	if err := EnsureConsumptionAuditSchema(DB); err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		row, err := lockConsumptionAudit(tx, key)
		if err != nil {
			return err
		}
		switch row.Status {
		case ConsumptionAuditUsageApplied, ConsumptionAuditDone:
			return nil
		case ConsumptionAuditPending:
		default:
			return fmt.Errorf("unknown consumption audit status %q", row.Status)
		}
		if err := UpdateTaskConsumptionUsageWithTokenTx(tx, row.UserID, row.ChannelID, row.TokenID, row.Quota); err != nil {
			return err
		}
		row.Status = ConsumptionAuditUsageApplied
		row.UpdatedAt = common.GetTimestamp()
		return tx.Save(row).Error
	})
}

// CompleteConsumptionAudit writes one consume log and marks the row done.
func CompleteConsumptionAudit(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("consumption audit key is empty")
	}
	if err := EnsureConsumptionAuditSchema(DB); err != nil {
		return err
	}
	var row ConsumptionAudit
	if err := DB.Where("idempotency_key = ?", key).First(&row).Error; err != nil {
		return err
	}
	if row.Status == ConsumptionAuditDone {
		return nil
	}
	if row.Status != ConsumptionAuditUsageApplied {
		return errors.New("consumption audit usage is not applied")
	}
	payload, err := decodeConsumptionAuditPayload(row.Payload)
	if err != nil {
		return err
	}
	inserted := false
	if common.LogConsumeEnabled {
		indexed, err := ensureConsumptionAuditLogIndex(LOG_DB)
		if err != nil {
			return err
		}
		if indexed {
			// 唯一索引兜住并发插入。冲突表示另一边已经写下这条日志。
			inserted, err = insertConsumptionAuditLog(&row, payload, key)
			if err != nil {
				return err
			}
		} else {
			exists, err := consumeLogExists(key)
			if err != nil {
				return err
			}
			if !exists {
				inserted, err = insertConsumptionAuditLog(&row, payload, key)
				if err != nil {
					return err
				}
			}
		}
	}
	if err := markConsumptionAuditDone(key); err != nil {
		return err
	}
	if inserted && common.DataExportEnabled {
		exportConsumptionAuditQuotaData(row, payload)
	}
	return nil
}

// RecoverPendingConsumptionAudits replays usage counters and consume logs left unfinished.
func RecoverPendingConsumptionAudits(limit int) error {
	if DB == nil {
		return errors.New("database is required")
	}
	if err := EnsureConsumptionAuditSchema(DB); err != nil {
		return err
	}
	if limit <= 0 {
		limit = 100
	}
	var rows []ConsumptionAudit
	if err := DB.Where("status IN ?", []string{ConsumptionAuditPending, ConsumptionAuditUsageApplied}).Order("id asc").Limit(limit).Find(&rows).Error; err != nil {
		return err
	}
	var first error
	for i := range rows {
		row := &rows[i]
		if row.Status == ConsumptionAuditPending {
			if err := ApplyConsumptionAuditUsage(row.IdempotencyKey); err != nil {
				if first == nil {
					first = err
				}
				continue
			}
		}
		if err := CompleteConsumptionAudit(row.IdempotencyKey); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func lockOrCreateConsumptionAudit(tx *gorm.DB, req ConsumptionAuditRequest, payload string) (*ConsumptionAudit, error) {
	row, err := lockConsumptionAudit(tx, req.IdempotencyKey)
	if err == nil {
		return row, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	now := common.GetTimestamp()
	created := &ConsumptionAudit{
		IdempotencyKey: req.IdempotencyKey,
		RequestID:      req.RequestID,
		UserID:         req.UserID,
		ChannelID:      req.ChannelID,
		TokenID:        req.TokenID,
		Quota:          req.Quota,
		Status:         ConsumptionAuditPending,
		Payload:        payload,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := tx.Create(created).Error; err != nil {
		return nil, err
	}
	return created, nil
}

func lockConsumptionAudit(tx *gorm.DB, key string) (*ConsumptionAudit, error) {
	var row ConsumptionAudit
	err := lockForUpdate(tx).Where("idempotency_key = ?", key).First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func markConsumptionAuditDone(key string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		row, err := lockConsumptionAudit(tx, key)
		if err != nil {
			return err
		}
		if row.Status == ConsumptionAuditDone {
			return nil
		}
		if row.Status != ConsumptionAuditUsageApplied {
			return errors.New("consumption audit usage is not applied")
		}
		row.Status = ConsumptionAuditDone
		row.UpdatedAt = common.GetTimestamp()
		return tx.Save(row).Error
	})
}

func decodeConsumptionAuditPayload(raw string) (ConsumptionAuditPayload, error) {
	var payload ConsumptionAuditPayload
	if strings.TrimSpace(raw) == "" {
		return payload, errors.New("consumption audit payload is empty")
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return payload, err
	}
	return payload, nil
}

func ensureConsumptionAuditLogIndex(db *gorm.DB) (bool, error) {
	if db == nil {
		return false, errors.New("log database is not initialized")
	}
	switch db.Dialector.Name() {
	case "postgres", "sqlite":
	default:
		return false, nil
	}
	if _, ok := consumptionAuditLogIndexReady.Load(db); ok {
		return true, nil
	}
	consumptionAuditSchemaMu.Lock()
	defer consumptionAuditSchemaMu.Unlock()
	if _, ok := consumptionAuditLogIndexReady.Load(db); ok {
		return true, nil
	}
	if !db.Migrator().HasTable(&Log{}) {
		return false, errors.New("log table is not initialized")
	}
	table, err := logTableName(db)
	if err != nil {
		return false, err
	}
	dialect := db.Dialector.Name()
	quotedTable := quoteSQLIdentifier(dialect, table)
	quotedColumn := quoteSQLIdentifier(dialect, "settlement_key")
	quotedIndex := quoteSQLIdentifier(dialect, consumptionAuditLogIndexName(table))
	err = db.Exec(fmt.Sprintf(
		`CREATE UNIQUE INDEX IF NOT EXISTS %s ON %s (%s) WHERE %s LIKE 'audit:%%'`,
		quotedIndex, quotedTable, quotedColumn, quotedColumn,
	)).Error
	if err != nil {
		return false, err
	}
	consumptionAuditLogIndexReady.Store(db, true)
	return true, nil
}

func insertConsumptionAuditLog(row *ConsumptionAudit, payload ConsumptionAuditPayload, key string) (bool, error) {
	if consumptionAuditBeforeLogInsert != nil {
		consumptionAuditBeforeLogInsert()
	}
	log := &Log{
		UserId:            row.UserID,
		Username:          payload.Username,
		CreatedAt:         common.GetTimestamp(),
		Type:              LogTypeConsume,
		Content:           payload.Content,
		PromptTokens:      payload.PromptTokens,
		CompletionTokens:  payload.CompletionTokens,
		TokenName:         payload.TokenName,
		ModelName:         payload.ModelName,
		Quota:             payload.Quota,
		ChannelId:         row.ChannelID,
		TokenId:           row.TokenID,
		UseTime:           payload.UseTimeSeconds,
		IsStream:          payload.IsStream,
		Group:             payload.Group,
		Ip:                payload.Ip,
		RequestId:         payload.RequestID,
		UpstreamRequestId: payload.UpstreamRequestID,
		SettlementKey:     key,
		Other:             payload.Other,
	}
	if err := createLog(log); err != nil {
		if isLogSettlementKeyConflict(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func isLogSettlementKeyConflict(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint failed") ||
		strings.Contains(message, "duplicate key value") ||
		strings.Contains(message, "audit_settlement_key")
}

func logTableName(db *gorm.DB) (string, error) {
	if db == nil {
		return "", errors.New("log database is not initialized")
	}
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(&Log{}); err != nil {
		return "", err
	}
	table := strings.TrimSpace(stmt.Table)
	if table == "" {
		return "", errors.New("log table name is empty")
	}
	return table, nil
}

func consumptionAuditLogIndexName(table string) string {
	const suffix = "_audit_settlement_key"
	name := "idx_" + table + suffix
	if len(name) <= 63 {
		return name
	}
	head := 63 - len(suffix)
	if head < len("idx_") {
		head = len("idx_")
	}
	return name[:head] + suffix
}

func quoteSQLIdentifier(dialect string, identifier string) string {
	switch dialect {
	case "postgres", "sqlite":
		return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
	default:
		return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
	}
}

func consumeLogExists(settlementKey string) (bool, error) {
	if LOG_DB == nil {
		return false, errors.New("log database is not initialized")
	}
	var count int64
	err := LOG_DB.Model(&Log{}).Where("settlement_key = ? AND type = ?", settlementKey, LogTypeConsume).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func exportConsumptionAuditQuotaData(row ConsumptionAudit, payload ConsumptionAuditPayload) {
	username := payload.Username
	userID := row.UserID
	tokenID := row.TokenID
	channelID := row.ChannelID
	gopool.Go(func() {
		LogQuotaData(QuotaDataLogParams{
			UserID:    userID,
			Username:  username,
			ModelName: payload.ModelName,
			Quota:     payload.Quota,
			CreatedAt: common.GetTimestamp(),
			TokenUsed: payload.PromptTokens + payload.CompletionTokens,
			UseGroup:  payload.Group,
			TokenID:   tokenID,
			ChannelID: channelID,
			NodeName:  common.NodeName,
		})
	})
}
