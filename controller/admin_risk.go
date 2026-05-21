package controller

import (
	"fmt"
	"sort"
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

type riskLogRow struct {
	ID        int    `json:"id"`
	UserID    int    `json:"user_id"`
	Username  string `json:"username"`
	Type      int    `json:"type"`
	Content   string `json:"content"`
	Quota     int    `json:"quota"`
	IP        string `json:"ip"`
	CreatedAt int64  `json:"created_at"`
}

type riskOrderRow struct {
	OrderType                string  `json:"order_type"`
	TradeNo                  string  `json:"trade_no"`
	UserID                   int     `json:"user_id"`
	Username                 string  `json:"username"`
	Status                   string  `json:"status"`
	PaidAmount               float64 `json:"paid_amount"`
	PaidCurrency             string  `json:"paid_currency"`
	PaymentProvider          string  `json:"payment_provider"`
	PaymentMethod            string  `json:"payment_method"`
	ReferralCommissionStatus string  `json:"referral_commission_status"`
	ReferralCommissionError  string  `json:"referral_commission_error"`
	CreatedAt                int64   `json:"created_at"`
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
	var newUsers int64
	_ = model.DB.Model(&model.User{}).Where("created_at >= ?", since).Count(&newUsers).Error

	common.ApiSuccess(c, gin.H{
		"window_hours":   windowHours,
		"signal_count":   len(signals),
		"disabled_users": disabledUsers,
		"new_user_count": newUsers,
		"signals":        signals,
	})
}

func GetRiskDetail(c *gin.Context) {
	windowHours := parseRiskWindowHours(c.Query("window_hours"))
	since := common.GetTimestamp() - int64(windowHours*3600)
	riskType := strings.TrimSpace(c.Query("type"))
	ip := strings.TrimSpace(c.Query("ip"))
	userID, _ := strconv.Atoi(strings.TrimSpace(c.Query("user_id")))

	userIDs := make([]int, 0, 8)
	if userID > 0 {
		userIDs = append(userIDs, userID)
	}
	switch riskType {
	case "shared_ip":
		if ip != "" {
			ids, err := riskUserIDsByIP(ip, since)
			if err != nil {
				common.ApiError(c, err)
				return
			}
			userIDs = append(userIDs, ids...)
		}
	case "new_users":
		ids, err := riskNewUserIDs(since)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		userIDs = append(userIDs, ids...)
	case "disabled_users":
		ids, err := riskUserIDsByStatus(common.UserStatusDisabled)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		userIDs = append(userIDs, ids...)
	}
	userIDs = uniquePositiveInts(userIDs)

	users, err := riskUsersByIDs(userIDs, since)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	logs, err := riskLogsForDetail(riskType, ip, userIDs, since)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	orders, err := riskOrdersByUserIDs(userIDs, since)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, gin.H{
		"type":         riskType,
		"window_hours": windowHours,
		"ip":           ip,
		"user_id":      userID,
		"users":        users,
		"logs":         logs,
		"orders":       orders,
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

func riskUserIDsByIP(ip string, since int64) ([]int, error) {
	var rows []struct {
		UserID int
	}
	if err := model.LOG_DB.Table("logs").
		Select("distinct user_id").
		Where("created_at >= ? AND ip = ? AND user_id > 0", since, ip).
		Limit(200).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UserID)
	}
	return ids, nil
}

func riskUserIDsByStatus(status int) ([]int, error) {
	var rows []struct {
		UserID int
	}
	if err := model.DB.Table("users").
		Select("id AS user_id").
		Where("status = ?", status).
		Order("id desc").
		Limit(200).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UserID)
	}
	return ids, nil
}

func riskNewUserIDs(since int64) ([]int, error) {
	var rows []struct {
		UserID int
	}
	if err := model.DB.Table("users").
		Select("id AS user_id").
		Where("created_at >= ?", since).
		Order("id desc").
		Limit(200).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UserID)
	}
	return ids, nil
}

func riskUsersByIDs(userIDs []int, since int64) ([]riskUserRow, error) {
	userIDs = uniquePositiveInts(userIDs)
	if len(userIDs) == 0 {
		return []riskUserRow{}, nil
	}
	var rows []riskUserRow
	if err := model.DB.Table("users AS u").
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
		) topup ON topup.user_id = u.id`, since, common.TopUpStatusSuccess).
		Where("u.id IN ?", userIDs).
		Order("u.id desc").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	if err := fillRiskUserLogMetrics(rows, since); err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].SignalCount, rows[i].Severity = scoreRiskUser(rows[i])
	}
	return rows, nil
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

func riskLogsForDetail(riskType string, ip string, userIDs []int, since int64) ([]riskLogRow, error) {
	query := model.LOG_DB.Table("logs").
		Select("id, user_id, username, type, content, quota, ip, created_at").
		Where("created_at >= ?", since)
	switch riskType {
	case "shared_ip":
		if ip == "" {
			return []riskLogRow{}, nil
		}
		query = query.Where("ip = ?", ip)
	case "high_error_count", "errors":
		query = query.Where("type = ?", model.LogTypeError)
		if len(userIDs) > 0 {
			query = query.Where("user_id IN ?", userIDs)
		}
	case "new_user_high_consume", "consume":
		query = query.Where("type = ?", model.LogTypeConsume)
		if len(userIDs) > 0 {
			query = query.Where("user_id IN ?", userIDs)
		}
	default:
		if len(userIDs) > 0 {
			query = query.Where("user_id IN ?", userIDs)
		} else if ip != "" {
			query = query.Where("ip = ?", ip)
		} else {
			return []riskLogRow{}, nil
		}
	}

	var rows []riskLogRow
	if err := query.Order("id desc").Limit(80).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func riskOrdersByUserIDs(userIDs []int, since int64) ([]riskOrderRow, error) {
	userIDs = uniquePositiveInts(userIDs)
	if len(userIDs) == 0 {
		return []riskOrderRow{}, nil
	}
	rows := make([]riskOrderRow, 0, 80)
	var topups []riskOrderRow
	if err := model.DB.Table("top_ups AS t").
		Select(`
			'topup' AS order_type,
			t.trade_no,
			t.user_id,
			u.username,
			t.status,
			t.paid_amount,
			t.paid_currency,
			t.payment_provider,
			t.payment_method,
			t.referral_commission_status,
			t.referral_commission_error,
			t.create_time AS created_at
		`).
		Joins("LEFT JOIN users u ON u.id = t.user_id").
		Where("t.create_time >= ? AND t.user_id IN ?", since, userIDs).
		Order("t.create_time desc").
		Limit(80).
		Scan(&topups).Error; err != nil {
		return nil, err
	}
	rows = append(rows, topups...)

	var subscriptions []riskOrderRow
	if err := model.DB.Table("subscription_orders AS s").
		Select(`
			'subscription' AS order_type,
			s.trade_no,
			s.user_id,
			u.username,
			s.status,
			s.paid_amount,
			s.paid_currency,
			s.payment_provider,
			s.payment_method,
			s.referral_commission_status,
			s.referral_commission_error,
			s.create_time AS created_at
		`).
		Joins("LEFT JOIN users u ON u.id = s.user_id").
		Where("s.create_time >= ? AND s.user_id IN ?", since, userIDs).
		Order("s.create_time desc").
		Limit(80).
		Scan(&subscriptions).Error; err != nil {
		return nil, err
	}
	rows = append(rows, subscriptions...)
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].CreatedAt > rows[j].CreatedAt
	})
	if len(rows) > 80 {
		rows = rows[:80]
	}
	return rows, nil
}

func uniquePositiveInts(values []int) []int {
	seen := make(map[int]struct{}, len(values))
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func collectRiskSignals(since int64) ([]riskSignal, error) {
	signals := make([]riskSignal, 0)

	var sharedIPs []riskSignal
	if err := model.LOG_DB.Table("logs").
		Select("ip, count(distinct user_id) AS count, min(created_at) AS first_seen_at, max(created_at) AS last_seen_at").
		Where("created_at >= ? AND ip <> '' AND user_id > 0", since).
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
		Where("created_at >= ? AND type = ? AND user_id > 0", since, model.LogTypeError).
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
		item.Message = fmt.Sprintf("短时间错误日志较多：%d 次", item.Count)
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
		item.Message = fmt.Sprintf("短时间充值频繁或金额较高：%d 笔", item.Count)
		signals = append(signals, item)
	}

	var newHighSpend []riskSignal
	var consumeCandidates []riskSignal
	if err := model.LOG_DB.Table("logs").
		Select("user_id, coalesce(sum(quota), 0) AS amount, count(*) AS count, min(created_at) AS first_seen_at, max(created_at) AS last_seen_at").
		Where("created_at >= ? AND type = ? AND user_id > 0", since, model.LogTypeConsume).
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
		item.Message = fmt.Sprintf("新账号短时间消耗较高：%.0f 额度", item.Amount)
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
