package model

import (
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TokenUsageReset struct {
	TokenId    int   `json:"token_id" gorm:"primaryKey"`
	UserId     int   `json:"user_id" gorm:"index"`
	ResetAt    int64 `json:"reset_at" gorm:"bigint;index"`
	ResetQuota int   `json:"reset_quota" gorm:"default:0"`
}

type TokenUsageDaily struct {
	TokenId      int    `json:"token_id" gorm:"primaryKey;index"`
	Date         string `json:"date" gorm:"primaryKey;type:char(10);index"`
	UserId       int    `json:"user_id" gorm:"index"`
	Quota        int    `json:"quota" gorm:"default:0"`
	RequestCount int    `json:"request_count" gorm:"default:0"`
	LastUsedAt   int64  `json:"last_used_at" gorm:"bigint;index"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt    int64  `json:"updated_at" gorm:"bigint;index"`
}

type TokenUsageStats struct {
	TodayQuota      int   `json:"today_quota"`
	MonthQuota      int   `json:"month_quota"`
	CumulativeQuota int   `json:"cumulative_quota"`
	LastUsedAt      int64 `json:"last_used_at"`
	ResetAt         int64 `json:"reset_at"`
}

func GetTokenUsageReset(tokenId int) (*TokenUsageReset, error) {
	var reset TokenUsageReset
	err := DB.First(&reset, "token_id = ?", tokenId).Error
	return &reset, err
}

func UpsertTokenUsageReset(tokenId int, userId int, resetAt int64, resetQuota int) error {
	reset := TokenUsageReset{
		TokenId:    tokenId,
		UserId:     userId,
		ResetAt:    resetAt,
		ResetQuota: resetQuota,
	}
	return DB.Save(&reset).Error
}

var tokenUsageLocation = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	return loc
}()

func tokenUsageDate(timestamp int64) string {
	if timestamp <= 0 {
		timestamp = common.GetTimestamp()
	}
	return time.Unix(timestamp, 0).In(tokenUsageLocation).Format("2006-01-02")
}

func tokenUsageCurrentColumn(name string) clause.Column {
	return clause.Column{Table: clause.CurrentTable, Name: name}
}

func tokenUsageIncrementExpr(column string, delta int) clause.Expr {
	return gorm.Expr("? + ?", tokenUsageCurrentColumn(column), delta)
}

func tokenUsageLastUsedAtUpdateExpr(usedAt int64) clause.Expr {
	return gorm.Expr(
		"CASE WHEN ? < ? THEN ? ELSE ? END",
		tokenUsageCurrentColumn("last_used_at"),
		usedAt,
		usedAt,
		tokenUsageCurrentColumn("last_used_at"),
	)
}

func tokenUsageUpdateAssignments(userId int, quota int, requestCount int, usedAt int64, updateLastUsedAt bool) map[string]interface{} {
	updates := map[string]interface{}{
		"user_id":       userId,
		"quota":         tokenUsageIncrementExpr("quota", quota),
		"request_count": tokenUsageIncrementExpr("request_count", requestCount),
		"updated_at":    usedAt,
	}
	if updateLastUsedAt {
		updates["last_used_at"] = tokenUsageLastUsedAtUpdateExpr(usedAt)
	}
	return updates
}

func RecordTokenUsage(tokenId int, userId int, quota int, usedAt int64) {
	if tokenId <= 0 {
		return
	}
	if usedAt <= 0 {
		usedAt = common.GetTimestamp()
	}
	if quota == 0 {
		if err := TouchTokenAccessedTime(tokenId, usedAt); err != nil {
			common.SysLog("failed to touch token accessed time: " + err.Error())
		}
	}
	requestCount := 0
	lastUsedAt := int64(0)
	if quota >= 0 {
		requestCount = 1
		lastUsedAt = usedAt
	}
	usage := &TokenUsageDaily{
		TokenId:      tokenId,
		Date:         tokenUsageDate(usedAt),
		UserId:       userId,
		Quota:        quota,
		RequestCount: requestCount,
		LastUsedAt:   lastUsedAt,
		CreatedAt:    usedAt,
		UpdatedAt:    usedAt,
	}
	updates := tokenUsageUpdateAssignments(userId, quota, requestCount, usedAt, quota >= 0)
	err := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "token_id"}, {Name: "date"}},
		DoUpdates: clause.Assignments(updates),
	}).Create(usage).Error
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return
		}
		common.SysLog("failed to record token usage: " + err.Error())
	}
}

func SumTokenConsumeLogQuota(tokenId int, startTimestamp int64, endTimestamp int64) (int, error) {
	var quota int
	tx := LOG_DB.Model(&Log{}).Where("type = ? AND token_id = ?", LogTypeConsume, tokenId)
	if startTimestamp > 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp > 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	err := tx.Select("COALESCE(SUM(quota), 0)").Scan(&quota).Error
	return quota, err
}

func firstTokenUsageDailyTimestamp(tokenId int) (int64, bool, error) {
	var first int64
	err := DB.Model(&TokenUsageDaily{}).
		Where("token_id = ?", tokenId).
		Select("COALESCE(MIN(created_at), 0)").
		Scan(&first).Error
	if err != nil {
		return 0, false, err
	}
	return first, first > 0, nil
}

func sumTokenUsageDailyQuota(tokenId int, startTimestamp int64, endTimestamp int64) (int, error) {
	var quota int
	tx := DB.Model(&TokenUsageDaily{}).Where("token_id = ?", tokenId)
	if startTimestamp > 0 {
		tx = tx.Where("date >= ?", tokenUsageDate(startTimestamp))
	}
	if endTimestamp > 0 {
		tx = tx.Where("date <= ?", tokenUsageDate(endTimestamp))
	}
	err := tx.Select("COALESCE(SUM(quota), 0)").Scan(&quota).Error
	return quota, err
}

func SumTokenUsageQuota(tokenId int, startTimestamp int64, endTimestamp int64) (int, error) {
	firstAggregateAt, hasAggregate, err := firstTokenUsageDailyTimestamp(tokenId)
	if err != nil {
		return 0, err
	}
	if !hasAggregate {
		return SumTokenConsumeLogQuota(tokenId, startTimestamp, endTimestamp)
	}

	total := 0
	if endTimestamp == 0 || endTimestamp >= firstAggregateAt {
		aggregateStart := startTimestamp
		if aggregateStart == 0 || aggregateStart < firstAggregateAt {
			aggregateStart = firstAggregateAt
		}
		quota, err := sumTokenUsageDailyQuota(tokenId, aggregateStart, endTimestamp)
		if err != nil {
			return 0, err
		}
		total += quota
	}

	logEnd := firstAggregateAt - 1
	if endTimestamp > 0 && endTimestamp < logEnd {
		logEnd = endTimestamp
	}
	if logEnd > 0 && (startTimestamp == 0 || startTimestamp <= logEnd) {
		quota, err := SumTokenConsumeLogQuota(tokenId, startTimestamp, logEnd)
		if err != nil {
			return 0, err
		}
		total += quota
	}

	return total, nil
}

func LastTokenUseTime(tokenId int) (int64, error) {
	var logLast int64
	err := LOG_DB.Model(&Log{}).
		Where("type = ? AND token_id = ?", LogTypeConsume, tokenId).
		Select("COALESCE(MAX(created_at), 0)").
		Scan(&logLast).Error
	if err != nil {
		return 0, err
	}
	var aggregateLast int64
	err = DB.Model(&TokenUsageDaily{}).
		Where("token_id = ?", tokenId).
		Select("COALESCE(MAX(last_used_at), 0)").
		Scan(&aggregateLast).Error
	if err != nil {
		return 0, err
	}
	if aggregateLast > logLast {
		return aggregateLast, nil
	}
	return logLast, nil
}

func nonNegativeQuota(quota int) int {
	if quota < 0 {
		return 0
	}
	return quota
}

func GetTokenUsageStats(tokenId int, resetAt int64, resetQuota int) (TokenUsageStats, error) {
	loc := tokenUsageLocation
	now := time.Now().In(loc)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).Unix()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc).Unix()

	todayQuota, err := SumTokenUsageQuota(tokenId, todayStart, 0)
	if err != nil {
		return TokenUsageStats{}, err
	}
	monthQuota, err := SumTokenUsageQuota(tokenId, monthStart, 0)
	if err != nil {
		return TokenUsageStats{}, err
	}
	totalQuota, err := SumTokenUsageQuota(tokenId, 0, 0)
	if err != nil {
		return TokenUsageStats{}, err
	}
	cumulativeQuota := totalQuota
	if resetAt > 0 {
		if resetQuota > 0 {
			cumulativeQuota = totalQuota - resetQuota
		} else {
			cumulativeQuota, err = SumTokenUsageQuota(tokenId, resetAt, 0)
			if err != nil {
				return TokenUsageStats{}, err
			}
		}
		if cumulativeQuota < 0 {
			cumulativeQuota = 0
		}
	}
	lastUsedAt, err := LastTokenUseTime(tokenId)
	if err != nil {
		return TokenUsageStats{}, err
	}
	if lastUsedAt == 0 {
		if token, tokenErr := GetTokenById(tokenId); tokenErr == nil {
			lastUsedAt = token.AccessedTime
		} else {
			common.SysLog("failed to fallback token accessed time: " + tokenErr.Error())
		}
	}
	return TokenUsageStats{
		TodayQuota:      nonNegativeQuota(todayQuota),
		MonthQuota:      nonNegativeQuota(monthQuota),
		CumulativeQuota: nonNegativeQuota(cumulativeQuota),
		LastUsedAt:      lastUsedAt,
		ResetAt:         resetAt,
	}, nil
}
