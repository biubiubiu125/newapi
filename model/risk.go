package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	RiskEventStatusOpen     = "open"
	RiskEventStatusViewed   = "viewed"
	RiskEventStatusResolved = "resolved"
	RiskEventStatusIgnored  = "ignored"

	RiskSeverityHigh    = "high"
	RiskSeverityWarning = "warning"
	RiskSeverityInfo    = "info"

	RiskTargetUser     = "user"
	RiskTargetIP       = "ip"
	RiskTargetToken    = "token"
	RiskTargetOrder    = "order"
	RiskTargetReferral = "referral"
	RiskTargetRule     = "rule"

	RiskWhitelistUser     = "user"
	RiskWhitelistIP       = "ip"
	RiskWhitelistToken    = "token"
	RiskWhitelistReferral = "referral"

	RiskActionViewed          = "viewed"
	RiskActionResolved        = "resolved"
	RiskActionIgnored         = "ignored"
	RiskActionBanUser         = "ban_user"
	RiskActionUnbanUser       = "unban_user"
	RiskActionDisableToken    = "disable_token"
	RiskActionWhitelist       = "whitelist"
	RiskActionRemoveWhitelist = "remove_whitelist"
	RiskActionNote            = "note"
)

type RiskEvent struct {
	Id             int     `json:"id"`
	EventKey       string  `json:"event_key" gorm:"type:varchar(191);uniqueIndex"`
	Type           string  `json:"type" gorm:"type:varchar(64);index"`
	TargetType     string  `json:"target_type" gorm:"type:varchar(32);index"`
	TargetId       string  `json:"target_id" gorm:"type:varchar(191);index"`
	UserId         int     `json:"user_id" gorm:"index"`
	Username       string  `json:"username" gorm:"type:varchar(191);index"`
	Ip             string  `json:"ip" gorm:"type:varchar(64);index"`
	TokenId        int     `json:"token_id" gorm:"index"`
	TokenName      string  `json:"token_name" gorm:"type:varchar(191)"`
	OrderType      string  `json:"order_type" gorm:"type:varchar(32);index"`
	TradeNo        string  `json:"trade_no" gorm:"type:varchar(255);index"`
	ReferralUserId int     `json:"referral_user_id" gorm:"index"`
	Severity       string  `json:"severity" gorm:"type:varchar(32);index"`
	Status         string  `json:"status" gorm:"type:varchar(32);index"`
	Title          string  `json:"title" gorm:"type:varchar(255)"`
	Summary        string  `json:"summary" gorm:"type:text"`
	Evidence       string  `json:"evidence" gorm:"type:text"`
	HitCount       int64   `json:"hit_count" gorm:"default:0"`
	Amount         float64 `json:"amount" gorm:"type:decimal(20,8);default:0"`
	WindowHours    int     `json:"window_hours" gorm:"default:24"`
	FirstSeenAt    int64   `json:"first_seen_at" gorm:"default:0;index"`
	LastSeenAt     int64   `json:"last_seen_at" gorm:"default:0;index"`
	ReviewedAt     int64   `json:"reviewed_at" gorm:"default:0"`
	ReviewedBy     int     `json:"reviewed_by" gorm:"index"`
	ResolvedAt     int64   `json:"resolved_at" gorm:"default:0"`
	ResolvedBy     int     `json:"resolved_by" gorm:"index"`
	ResolveNote    string  `json:"resolve_note" gorm:"type:text"`
	CreatedAt      int64   `json:"created_at" gorm:"autoCreateTime;index"`
	UpdatedAt      int64   `json:"updated_at" gorm:"autoUpdateTime"`
}

type RiskAction struct {
	Id             int    `json:"id"`
	EventId        int    `json:"event_id" gorm:"index"`
	Action         string `json:"action" gorm:"type:varchar(64);index"`
	TargetType     string `json:"target_type" gorm:"type:varchar(32);index"`
	TargetId       string `json:"target_id" gorm:"type:varchar(191);index"`
	UserId         int    `json:"user_id" gorm:"index"`
	TokenId        int    `json:"token_id" gorm:"index"`
	Ip             string `json:"ip" gorm:"type:varchar(64);index"`
	OperatorUserId int    `json:"operator_user_id" gorm:"index"`
	OperatorName   string `json:"operator_name" gorm:"type:varchar(191)"`
	Reason         string `json:"reason" gorm:"type:text"`
	OldValue       string `json:"old_value" gorm:"type:text"`
	NewValue       string `json:"new_value" gorm:"type:text"`
	Evidence       string `json:"evidence" gorm:"type:text"`
	ClientIP       string `json:"client_ip" gorm:"type:varchar(64)"`
	UserAgent      string `json:"user_agent" gorm:"type:text"`
	CreatedAt      int64  `json:"created_at" gorm:"autoCreateTime;index"`
}

type RiskWhitelist struct {
	Id             int    `json:"id"`
	TargetType     string `json:"target_type" gorm:"type:varchar(32);uniqueIndex:idx_risk_whitelist_target,priority:1"`
	TargetId       string `json:"target_id" gorm:"type:varchar(191);uniqueIndex:idx_risk_whitelist_target,priority:2"`
	Reason         string `json:"reason" gorm:"type:text"`
	OperatorUserId int    `json:"operator_user_id" gorm:"index"`
	OperatorName   string `json:"operator_name" gorm:"type:varchar(191)"`
	ExpiresAt      int64  `json:"expires_at" gorm:"default:0;index"`
	CreatedAt      int64  `json:"created_at" gorm:"autoCreateTime;index"`
	UpdatedAt      int64  `json:"updated_at" gorm:"autoUpdateTime"`
}

type RiskEventUpsert struct {
	EventKey       string
	Type           string
	TargetType     string
	TargetId       string
	UserId         int
	Username       string
	Ip             string
	TokenId        int
	TokenName      string
	OrderType      string
	TradeNo        string
	ReferralUserId int
	Severity       string
	Title          string
	Summary        string
	Evidence       map[string]interface{}
	HitCount       int64
	Amount         float64
	WindowHours    int
	FirstSeenAt    int64
	LastSeenAt     int64
}

func UpsertRiskEvent(input RiskEventUpsert) (*RiskEvent, error) {
	now := common.GetTimestamp()
	if input.EventKey == "" {
		input.EventKey = input.Type + ":" + input.TargetType + ":" + input.TargetId
	}
	if input.TargetId == "" {
		input.TargetId = input.EventKey
	}
	if input.Severity == "" {
		input.Severity = RiskSeverityInfo
	}
	if input.WindowHours <= 0 {
		input.WindowHours = 24
	}
	if input.FirstSeenAt <= 0 {
		input.FirstSeenAt = now
	}
	if input.LastSeenAt <= 0 {
		input.LastSeenAt = now
	}
	evidence := common.MapToJsonStr(input.Evidence)
	var existing RiskEvent
	err := DB.Where("event_key = ?", input.EventKey).First(&existing).Error
	if err == nil {
		return updateRiskEventFromInput(existing, input, evidence)
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	event := RiskEvent{
		EventKey:       input.EventKey,
		Type:           input.Type,
		TargetType:     input.TargetType,
		TargetId:       input.TargetId,
		UserId:         input.UserId,
		Username:       input.Username,
		Ip:             input.Ip,
		TokenId:        input.TokenId,
		TokenName:      input.TokenName,
		OrderType:      input.OrderType,
		TradeNo:        input.TradeNo,
		ReferralUserId: input.ReferralUserId,
		Severity:       input.Severity,
		Status:         RiskEventStatusOpen,
		Title:          input.Title,
		Summary:        input.Summary,
		Evidence:       evidence,
		HitCount:       input.HitCount,
		Amount:         input.Amount,
		WindowHours:    input.WindowHours,
		FirstSeenAt:    input.FirstSeenAt,
		LastSeenAt:     input.LastSeenAt,
	}
	if err := DB.Create(&event).Error; err != nil {
		if readErr := DB.Where("event_key = ?", input.EventKey).First(&existing).Error; readErr == nil {
			return updateRiskEventFromInput(existing, input, evidence)
		}
		return nil, err
	}
	return &event, nil
}

func updateRiskEventFromInput(existing RiskEvent, input RiskEventUpsert, evidence string) (*RiskEvent, error) {
	updates := map[string]interface{}{
		"type":             input.Type,
		"target_type":      input.TargetType,
		"target_id":        input.TargetId,
		"user_id":          input.UserId,
		"username":         input.Username,
		"ip":               input.Ip,
		"token_id":         input.TokenId,
		"token_name":       input.TokenName,
		"order_type":       input.OrderType,
		"trade_no":         input.TradeNo,
		"referral_user_id": input.ReferralUserId,
		"severity":         input.Severity,
		"title":            input.Title,
		"summary":          input.Summary,
		"evidence":         evidence,
		"hit_count":        input.HitCount,
		"amount":           input.Amount,
		"window_hours":     input.WindowHours,
		"last_seen_at":     input.LastSeenAt,
	}
	if existing.FirstSeenAt == 0 || input.FirstSeenAt < existing.FirstSeenAt {
		updates["first_seen_at"] = input.FirstSeenAt
	}
	if existing.Status == RiskEventStatusResolved || existing.Status == RiskEventStatusIgnored {
		updates["status"] = RiskEventStatusOpen
		updates["resolved_at"] = int64(0)
		updates["resolved_by"] = 0
		updates["resolve_note"] = ""
	}
	if err := DB.Model(&RiskEvent{}).Where("id = ?", existing.Id).Updates(updates).Error; err != nil {
		return nil, err
	}
	_ = DB.Where("id = ?", existing.Id).First(&existing).Error
	return &existing, nil
}
