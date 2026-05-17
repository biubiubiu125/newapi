package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type riskSignal struct {
	Type        string  `json:"type"`
	Severity    string  `json:"severity"`
	UserID      int     `json:"user_id,omitempty"`
	Username    string  `json:"username,omitempty"`
	IP          string  `json:"ip,omitempty"`
	Count       int64   `json:"count"`
	Amount      float64 `json:"amount,omitempty"`
	Message     string  `json:"message"`
	FirstSeenAt int64   `json:"first_seen_at,omitempty"`
	LastSeenAt  int64   `json:"last_seen_at,omitempty"`
}

type riskUserRow struct {
	UserID          int     `json:"user_id"`
	Username        string  `json:"username"`
	Status          int     `json:"status"`
	Role            int     `json:"role"`
	CreatedAt       int64   `json:"created_at"`
	LastLoginAt     int64   `json:"last_login_at"`
	Quota           int     `json:"quota"`
	UsedQuota       int     `json:"used_quota"`
	RequestCount    int     `json:"request_count"`
	TopupCount      int64   `json:"topup_count"`
	TopupPaidAmount float64 `json:"topup_paid_amount"`
	ErrorCount      int64   `json:"error_count"`
	ConsumeCount    int64   `json:"consume_count"`
	ConsumeQuota    int64   `json:"consume_quota"`
	SignalCount     int     `json:"signal_count"`
	Severity        string  `json:"severity"`
}

type riskLogMetric struct {
	UserID       int   `json:"user_id"`
	ErrorCount   int64 `json:"error_count"`
	ConsumeCount int64 `json:"consume_count"`
	ConsumeQuota int64 `json:"consume_quota"`
}

func GetRiskOverview(c *gin.Context) {
	windowHours := parseRiskWindowHours(c.Query("window_hours"))
	since := common.GetTimestamp() - int64(windowHours*3600)
	signals, err := collectRiskSignals(since)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	var disabledUsers int64
	_ = model.DB.Model(&model.User{}).Where("status = ?", common.UserStatusDisabled).Count(&disabledUsers).Error

	common.ApiSuccess(c, gin.H{
		"window_hours":   windowHours,
		"signal_count":   len(signals),
		"disabled_users": disabledUsers,
		"signals":        signals,
	})
}

func GetRiskUsers(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	windowHours := parseRiskWindowHours(c.Query("window_hours"))
	keyword := strings.TrimSpace(c.Query("keyword"))
	since := common.GetTimestamp() - int64(windowHours*3600)

	query := model.DB.Table("users AS u").
		Select(`
			u.id AS user_id,
			u.username,
			u.status,
			u.role,
			u.created_at,
			u.last_login_at,
			u.quota,
			u.used_quota,
			u.request_count,
			coalesce(topup.topup_count, 0) AS topup_count,
			coalesce(topup.topup_paid_amount, 0) AS topup_paid_amount
		`).
		Joins(`LEFT JOIN (
			SELECT user_id, count(*) AS topup_count, coalesce(sum(paid_amount), 0) AS topup_paid_amount
			FROM top_ups
			WHERE create_time >= ? AND status = ?
			GROUP BY user_id
		) topup ON topup.user_id = u.id`, since, common.TopUpStatusSuccess)
	if keyword != "" {
		like := "%" + keyword + "%"
		if _, err := strconv.Atoi(keyword); err == nil {
			query = query.Where("u.id = ? OR u.username LIKE ? OR u.email LIKE ?", keyword, like, like)
		} else {
			query = query.Where("u.username LIKE ? OR u.email LIKE ?", like, like)
		}
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	var rows []riskUserRow
	if err := query.Order("coalesce(topup.topup_paid_amount,0) desc, coalesce(topup.topup_count,0) desc, u.id desc").
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Scan(&rows).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if err := fillRiskUserLogMetrics(rows, since); err != nil {
		common.ApiError(c, err)
		return
	}

	for i := range rows {
		rows[i].SignalCount, rows[i].Severity = scoreRiskUser(rows[i])
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(rows)
	common.ApiSuccess(c, pageInfo)
}

func fillRiskUserLogMetrics(rows []riskUserRow, since int64) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UserID)
	}
	var metrics []riskLogMetric
	if err := model.LOG_DB.Table("logs").
		Select(`
			user_id,
			coalesce(sum(case when type = ? then 1 else 0 end), 0) AS error_count,
			coalesce(sum(case when type = ? then 1 else 0 end), 0) AS consume_count,
			coalesce(sum(case when type = ? then quota else 0 end), 0) AS consume_quota
		`, model.LogTypeError, model.LogTypeConsume, model.LogTypeConsume).
		Where("created_at >= ? AND user_id IN ?", since, ids).
		Group("user_id").
		Scan(&metrics).Error; err != nil {
		return err
	}
	byUser := make(map[int]riskLogMetric, len(metrics))
	for _, metric := range metrics {
		byUser[metric.UserID] = metric
	}
	for i := range rows {
		metric := byUser[rows[i].UserID]
		rows[i].ErrorCount = metric.ErrorCount
		rows[i].ConsumeCount = metric.ConsumeCount
		rows[i].ConsumeQuota = metric.ConsumeQuota
	}
	return nil
}

func collectRiskSignals(since int64) ([]riskSignal, error) {
	signals := make([]riskSignal, 0)

	var sharedIPs []riskSignal
	if err := model.LOG_DB.Table("logs").
		Select("ip, count(distinct user_id) AS count, min(created_at) AS first_seen_at, max(created_at) AS last_seen_at").
		Where("created_at >= ? AND ip <> ''", since).
		Group("ip").
		Having("count(distinct user_id) >= ?", 5).
		Order("count desc").
		Limit(20).
		Scan(&sharedIPs).Error; err != nil {
		return nil, err
	}
	for _, item := range sharedIPs {
		item.Type = "shared_ip"
		item.Severity = "warning"
		item.Message = "同一 IP 关联多个账号"
		signals = append(signals, item)
	}

	var highErrors []riskSignal
	if err := model.LOG_DB.Table("logs").
		Select("user_id, username, count(*) AS count, min(created_at) AS first_seen_at, max(created_at) AS last_seen_at").
		Where("created_at >= ? AND type = ?", since, model.LogTypeError).
		Group("user_id, username").
		Having("count(*) >= ?", 20).
		Order("count desc").
		Limit(20).
		Scan(&highErrors).Error; err != nil {
		return nil, err
	}
	for _, item := range highErrors {
		item.Type = "high_error_count"
		item.Severity = "warning"
		item.Message = "短时间错误日志较多"
		signals = append(signals, item)
	}

	var highTopups []riskSignal
	if err := model.DB.Table("top_ups AS t").
		Select("t.user_id, u.username, count(*) AS count, coalesce(sum(t.paid_amount), 0) AS amount, min(t.create_time) AS first_seen_at, max(t.create_time) AS last_seen_at").
		Joins("LEFT JOIN users u ON u.id = t.user_id").
		Where("t.create_time >= ? AND t.status = ?", since, common.TopUpStatusSuccess).
		Group("t.user_id, u.username").
		Having("count(*) >= ? OR coalesce(sum(t.paid_amount), 0) >= ?", 5, 1000).
		Order("amount desc").
		Limit(20).
		Scan(&highTopups).Error; err != nil {
		return nil, err
	}
	for _, item := range highTopups {
		item.Type = "high_topup_activity"
		item.Severity = "info"
		item.Message = "短时间充值频次或金额较高"
		signals = append(signals, item)
	}

	var newHighSpend []riskSignal
	var consumeCandidates []riskSignal
	if err := model.LOG_DB.Table("logs").
		Select("user_id, coalesce(sum(quota), 0) AS amount, count(*) AS count, min(created_at) AS first_seen_at, max(created_at) AS last_seen_at").
		Where("created_at >= ? AND type = ?", since, model.LogTypeConsume).
		Group("user_id").
		Having("coalesce(sum(quota), 0) >= ?", 1000000).
		Order("amount desc").
		Scan(&consumeCandidates).Error; err != nil {
		return nil, err
	}
	if len(consumeCandidates) > 0 {
		ids := make([]int, 0, len(consumeCandidates))
		for _, item := range consumeCandidates {
			ids = append(ids, item.UserID)
		}
		var newUsers []struct {
			ID       int
			Username string
		}
		if err := model.DB.Model(&model.User{}).
			Select("id, username").
			Where("id IN ? AND created_at >= ?", ids, since).
			Scan(&newUsers).Error; err != nil {
			return nil, err
		}
		usernames := make(map[int]string, len(newUsers))
		for _, user := range newUsers {
			usernames[user.ID] = user.Username
		}
		for _, item := range consumeCandidates {
			username, ok := usernames[item.UserID]
			if !ok {
				continue
			}
			item.Username = username
			newHighSpend = append(newHighSpend, item)
			if len(newHighSpend) >= 20 {
				break
			}
		}
	}
	for _, item := range newHighSpend {
		item.Type = "new_user_high_consume"
		item.Severity = "warning"
		item.Message = "新账号短时间消耗较高"
		signals = append(signals, item)
	}

	return signals, nil
}

func parseRiskWindowHours(value string) int {
	window, _ := strconv.Atoi(value)
	switch {
	case window <= 0:
		return 24
	case window > 24*30:
		return 24 * 30
	default:
		return window
	}
}

func scoreRiskUser(row riskUserRow) (int, string) {
	score := 0
	if row.Status == common.UserStatusDisabled {
		score++
	}
	if row.ErrorCount >= 20 {
		score += 2
	}
	if row.TopupCount >= 5 || row.TopupPaidAmount >= 1000 {
		score++
	}
	if row.ConsumeQuota >= 1000000 {
		score++
	}
	if score >= 3 {
		return score, "high"
	}
	if score > 0 {
		return score, "warning"
	}
	return 0, "normal"
}
