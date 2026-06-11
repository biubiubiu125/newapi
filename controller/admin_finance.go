package controller

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	referralservice "github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type rechargeAuditOrder struct {
	OrderType                string  `json:"order_type"`
	ID                       int     `json:"id"`
	UserID                   int     `json:"user_id"`
	Username                 string  `json:"username"`
	Amount                   int64   `json:"amount"`
	CreditAmount             float64 `json:"credit_amount"`
	CreditQuota              int64   `json:"credit_quota"`
	ProductName              string  `json:"product_name"`
	Money                    float64 `json:"money"`
	PaidAmount               float64 `json:"paid_amount"`
	PaidCurrency             string  `json:"paid_currency"`
	PaidAmountCNY            float64 `json:"paid_amount_cny"`
	PaidCNYFxRate            float64 `json:"paid_cny_fx_rate"`
	PaidCNYFxMissing         bool    `json:"paid_cny_fx_missing"`
	TradeNo                  string  `json:"trade_no"`
	PaymentMethod            string  `json:"payment_method"`
	PaymentProvider          string  `json:"payment_provider"`
	PriceSnapshot            float64 `json:"price_snapshot"`
	USDExchangeRateSnapshot  float64 `json:"usd_exchange_rate_snapshot"`
	QuotaDisplayTypeSnapshot string  `json:"quota_display_type_snapshot"`
	CreateTime               int64   `json:"create_time"`
	CompleteTime             int64   `json:"complete_time"`
	Status                   string  `json:"status"`
	ReferralCommissionStatus string  `json:"referral_commission_status"`
	ReferralCommissionError  string  `json:"referral_commission_error"`
}

type providerSummary struct {
	PaymentProvider string  `json:"payment_provider"`
	PaidCurrency    string  `json:"paid_currency"`
	Count           int64   `json:"count"`
	PaidAmount      float64 `json:"paid_amount"`
	PaidAmountCNY   float64 `json:"paid_amount_cny"`
	PaidCNYFxRate   float64 `json:"paid_cny_fx_rate"`
	FxMissingCount  int64   `json:"fx_missing_count"`
}

type statusSummary struct {
	Status         string  `json:"status"`
	Currency       string  `json:"currency"`
	Count          int64   `json:"count"`
	PaidAmount     float64 `json:"paid_amount"`
	PaidAmountCNY  float64 `json:"paid_amount_cny"`
	PaidCNYFxRate  float64 `json:"paid_cny_fx_rate"`
	FxMissingCount int64   `json:"fx_missing_count"`
}

type currencySummary struct {
	Currency       string  `json:"currency"`
	Count          int64   `json:"count"`
	PaidAmount     float64 `json:"paid_amount"`
	PaidAmountCNY  float64 `json:"paid_amount_cny"`
	PaidCNYFxRate  float64 `json:"paid_cny_fx_rate"`
	FxMissingCount int64   `json:"fx_missing_count"`
}

type rechargeAuditTotals struct {
	TotalCount     int64   `json:"total_count"`
	SuccessCount   int64   `json:"success_count"`
	PendingCount   int64   `json:"pending_count"`
	FailedCount    int64   `json:"failed_count"`
	PaidAmount     float64 `json:"paid_amount"`
	PaidAmountCNY  float64 `json:"paid_amount_cny"`
	FxMissingCount int64   `json:"fx_missing_count"`
	CreditAmount   float64 `json:"credit_amount"`
}

type rechargeAuditOrderCursor struct {
	CompleteTime int64 `gorm:"column:complete_time"`
	OrderRank    int   `gorm:"column:order_rank"`
	ID           int   `gorm:"column:id"`
}

const (
	rechargeAuditTopupOrderRank        = 1
	rechargeAuditSubscriptionOrderRank = 2
)

type auditAnomaly struct {
	Type      string `json:"type"`
	Severity  string `json:"severity"`
	TradeNo   string `json:"trade_no,omitempty"`
	UserID    int    `json:"user_id,omitempty"`
	Username  string `json:"username,omitempty"`
	Message   string `json:"message"`
	CreatedAt int64  `json:"created_at,omitempty"`
}

type rechargeAuditFilters struct {
	keyword   string
	status    string
	provider  string
	orderType string
	userID    int
	startTime int64
	endTime   int64
}

func GetRechargeAudit(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	keyword := strings.TrimSpace(c.Query("keyword"))
	status, ok := parseRechargeAuditStatus(c)
	if !ok {
		return
	}
	provider := strings.TrimSpace(c.Query("provider"))
	orderType := strings.TrimSpace(c.Query("order_type"))
	userID, ok := parseRechargeAuditUserID(c)
	if !ok {
		return
	}
	startTime, endTime := parseRechargeAuditTimeRange(c)
	filters := rechargeAuditFilters{
		keyword:   keyword,
		status:    status,
		provider:  provider,
		orderType: orderType,
		userID:    userID,
		startTime: startTime,
		endTime:   endTime,
	}

	baseSQL, baseArgs := orderManagementBaseSQL(filters)
	whereSQL, whereArgs := orderManagementWhereSQL(filters)

	var total int64
	countArgs := append([]interface{}{}, baseArgs...)
	countArgs = append(countArgs, whereArgs...)
	if err := model.DB.Raw(fmt.Sprintf("SELECT count(*) FROM (%s) AS o %s", baseSQL, whereSQL), countArgs...).Scan(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	var orders []rechargeAuditOrder
	queryArgs := append([]interface{}{}, baseArgs...)
	queryArgs = append(queryArgs, whereArgs...)
	queryArgs = append(queryArgs, pageInfo.GetPageSize(), pageInfo.GetStartIdx())
	if err := model.DB.Raw(fmt.Sprintf("SELECT * FROM (%s) AS o %s ORDER BY o.create_time DESC, o.id DESC LIMIT ? OFFSET ?", baseSQL, whereSQL), queryArgs...).Scan(&orders).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	for i := range orders {
		orders[i].PaidAmountCNY, orders[i].PaidCNYFxRate, orders[i].PaidCNYFxMissing = referralservice.PaidAmountCNY(
			firstPositiveFloat(orders[i].PaidAmount, orders[i].Money),
			orders[i].PaidCurrency,
			orders[i].PaymentProvider,
		)
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(orders)

	common.ApiSuccess(c, pageInfo)
}

func GetRechargeAuditSummary(c *gin.Context) {
	keyword := strings.TrimSpace(c.Query("keyword"))
	status, ok := parseRechargeAuditStatus(c)
	if !ok {
		return
	}
	provider := strings.TrimSpace(c.Query("provider"))
	orderType := strings.TrimSpace(c.Query("order_type"))
	userID, ok := parseRechargeAuditUserID(c)
	if !ok {
		return
	}
	startTime, endTime := parseRechargeAuditTimeRange(c)
	filters := rechargeAuditFilters{
		keyword:   keyword,
		status:    status,
		provider:  provider,
		orderType: orderType,
		userID:    userID,
		startTime: startTime,
		endTime:   endTime,
	}

	if isRechargeAuditBadgeOnly(c) {
		latestOrderCursor, newOrderCount, err := rechargeAuditBadgeSummary(filters, c.Query("after_order_cursor"))
		if err != nil {
			common.ApiError(c, err)
			return
		}
		common.ApiSuccess(c, gin.H{
			"totals":              rechargeAuditTotals{},
			"by_currency":         []currencySummary{},
			"by_provider":         []providerSummary{},
			"by_status":           []statusSummary{},
			"anomalies":           []auditAnomaly{},
			"new_order_count":     newOrderCount,
			"latest_order_cursor": formatRechargeAuditOrderCursor(latestOrderCursor),
		})
		return
	}

	baseSQL, baseArgs := orderManagementBaseSQL(filters)
	whereSQL, whereArgs := orderManagementWhereSQL(filters)
	baseAndWhereArgs := append([]interface{}{}, baseArgs...)
	baseAndWhereArgs = append(baseAndWhereArgs, whereArgs...)

	var totals rechargeAuditTotals
	selectSQL := fmt.Sprintf(`
		count(*) AS total_count,
		coalesce(sum(case when o.status = ? then 1 else 0 end), 0) AS success_count,
		coalesce(sum(case when o.status = ? then 1 else 0 end), 0) AS pending_count,
		coalesce(sum(case when o.status in (?, ?) then 1 else 0 end), 0) AS failed_count,
		coalesce(sum(case when o.status = ? then o.paid_amount else 0 end), 0) AS paid_amount,
		coalesce(sum(case when o.status = ? and o.order_type = 'topup' then o.credit_amount else 0 end), 0) AS credit_amount
	FROM (%s) AS o %s`, baseSQL, whereSQL)
	selectArgs := []interface{}{common.TopUpStatusSuccess, common.TopUpStatusPending, common.TopUpStatusFailed, common.TopUpStatusExpired, common.TopUpStatusSuccess, common.TopUpStatusSuccess}
	selectArgs = append(selectArgs, baseArgs...)
	selectArgs = append(selectArgs, whereArgs...)
	if err := model.DB.Raw("SELECT "+selectSQL, selectArgs...).Scan(&totals).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	latestOrderCursor, err := latestRechargeAuditOrderCursor(baseSQL, baseArgs, whereSQL, whereArgs)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	newOrderCount, err := countRechargeAuditOrdersAfterCursor(c.Query("after_order_cursor"), baseSQL, baseArgs, whereSQL, whereArgs)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	var byCurrency []currencySummary
	currencySQL := fmt.Sprintf("SELECT upper(coalesce(nullif(o.paid_currency, ''), 'CNY')) AS currency, count(*) AS count, coalesce(sum(o.paid_amount), 0) AS paid_amount FROM (%s) AS o %s GROUP BY upper(coalesce(nullif(o.paid_currency, ''), 'CNY')) ORDER BY paid_amount DESC", baseSQL, appendOrderManagementCondition(whereSQL, "o.status = ?"))
	currencyArgs := append([]interface{}{}, baseAndWhereArgs...)
	currencyArgs = append(currencyArgs, common.TopUpStatusSuccess)
	if err := model.DB.Raw(currencySQL, currencyArgs...).Scan(&byCurrency).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	applyRechargeAuditCNYSummary(&totals, byCurrency, nil, nil)

	var byProvider []providerSummary
	providerSQL := fmt.Sprintf("SELECT coalesce(nullif(o.payment_provider, ''), 'unknown') AS payment_provider, upper(coalesce(nullif(o.paid_currency, ''), 'CNY')) AS paid_currency, count(*) AS count, coalesce(sum(o.paid_amount), 0) AS paid_amount FROM (%s) AS o %s GROUP BY coalesce(nullif(o.payment_provider, ''), 'unknown'), upper(coalesce(nullif(o.paid_currency, ''), 'CNY')) ORDER BY paid_amount DESC", baseSQL, appendOrderManagementCondition(whereSQL, "o.status = ?"))
	providerArgs := append([]interface{}{}, baseAndWhereArgs...)
	providerArgs = append(providerArgs, common.TopUpStatusSuccess)
	if err := model.DB.Raw(providerSQL, providerArgs...).Scan(&byProvider).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	applyRechargeAuditCNYSummary(nil, nil, byProvider, nil)

	var byStatus []statusSummary
	statusSQL := fmt.Sprintf("SELECT o.status, upper(coalesce(nullif(o.paid_currency, ''), 'CNY')) AS currency, count(*) AS count, coalesce(sum(o.paid_amount), 0) AS paid_amount FROM (%s) AS o %s GROUP BY o.status, upper(coalesce(nullif(o.paid_currency, ''), 'CNY')) ORDER BY count DESC", baseSQL, whereSQL)
	if err := model.DB.Raw(statusSQL, baseAndWhereArgs...).Scan(&byStatus).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	applyRechargeAuditCNYSummary(nil, nil, nil, byStatus)

	anomalies, err := buildRechargeAnomalies(filters)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, gin.H{
		"totals":              totals,
		"by_currency":         byCurrency,
		"by_provider":         byProvider,
		"by_status":           byStatus,
		"anomalies":           anomalies,
		"new_order_count":     newOrderCount,
		"latest_order_cursor": formatRechargeAuditOrderCursor(latestOrderCursor),
	})
}

func orderManagementBaseSQL(filters rechargeAuditFilters) (string, []interface{}) {
	branches := make([]string, 0, 2)
	args := make([]interface{}, 0)
	includeTopup := filters.orderType == "" || filters.orderType == "all" || filters.orderType == "topup"
	includeSubscription := filters.orderType == "" || filters.orderType == "all" || filters.orderType == "subscription"

	if includeTopup {
		creditExpr, creditArgs := rechargeAuditCreditAmountExpr()
		topupWhereSQL, topupWhereArgs := orderManagementBranchWhereSQL("t", []string{`NOT EXISTS (
	SELECT 1 FROM subscription_orders AS so WHERE so.trade_no = t.trade_no
)`}, filters)
		branches = append(branches, fmt.Sprintf(`
SELECT
	'topup' AS order_type,
	t.id,
	t.user_id,
	u.username,
	t.amount,
	%s AS credit_amount,
	t.credit_quota_snapshot AS credit_quota,
	'' AS product_name,
	t.money,
	t.paid_amount,
	t.paid_currency,
	t.trade_no,
	t.payment_method,
	t.payment_provider,
	t.price_snapshot,
	t.usd_exchange_rate_snapshot,
	t.quota_display_type_snapshot,
	t.create_time,
	t.complete_time,
	t.status,
	t.referral_commission_status,
	t.referral_commission_error
FROM top_ups AS t
LEFT JOIN users u ON u.id = t.user_id
%s`, creditExpr, topupWhereSQL))
		args = append(args, creditArgs...)
		args = append(args, topupWhereArgs...)
	}

	if includeSubscription {
		subscriptionWhereSQL, subscriptionWhereArgs := orderManagementBranchWhereSQL("s", nil, filters)
		branches = append(branches, fmt.Sprintf(`
SELECT
	'subscription' AS order_type,
	s.id,
	s.user_id,
	u.username,
	0 AS amount,
	0 AS credit_amount,
	s.plan_total_amount_snapshot AS credit_quota,
	coalesce(nullif(s.plan_title_snapshot, ''), p.title, '') AS product_name,
	s.money,
	s.paid_amount,
	s.paid_currency,
	s.trade_no,
	s.payment_method,
	s.payment_provider,
	s.plan_price_snapshot AS price_snapshot,
	s.usd_exchange_rate_snapshot,
	s.quota_display_type_snapshot,
	s.create_time,
	s.complete_time,
	s.status,
	s.referral_commission_status,
	s.referral_commission_error
FROM subscription_orders AS s
LEFT JOIN users u ON u.id = s.user_id
LEFT JOIN subscription_plans p ON p.id = s.plan_id
%s`, subscriptionWhereSQL))
		args = append(args, subscriptionWhereArgs...)
	}

	if len(branches) == 0 {
		return emptyRechargeAuditBaseSQL(), nil
	}

	return strings.Join(branches, "\nUNION ALL\n"), args
}

func orderManagementBadgeBaseSQL(filters rechargeAuditFilters) (string, []interface{}) {
	branches := make([]string, 0, 2)
	args := make([]interface{}, 0)
	includeTopup := filters.orderType == "" || filters.orderType == "all" || filters.orderType == "topup"
	includeSubscription := filters.orderType == "" || filters.orderType == "all" || filters.orderType == "subscription"

	if includeTopup {
		topupWhereSQL, topupWhereArgs := orderManagementBranchWhereSQL("t", []string{`NOT EXISTS (
	SELECT 1 FROM subscription_orders AS so WHERE so.trade_no = t.trade_no
)`}, filters)
		branches = append(branches, fmt.Sprintf(`
SELECT
	'topup' AS order_type,
	t.id,
	t.user_id,
	u.username,
	'' AS product_name,
	t.trade_no,
	t.payment_provider,
	t.create_time,
	t.complete_time,
	t.status
FROM top_ups AS t
LEFT JOIN users u ON u.id = t.user_id
%s`, topupWhereSQL))
		args = append(args, topupWhereArgs...)
	}

	if includeSubscription {
		subscriptionWhereSQL, subscriptionWhereArgs := orderManagementBranchWhereSQL("s", nil, filters)
		branches = append(branches, fmt.Sprintf(`
SELECT
	'subscription' AS order_type,
	s.id,
	s.user_id,
	u.username,
	coalesce(nullif(s.plan_title_snapshot, ''), p.title, '') AS product_name,
	s.trade_no,
	s.payment_provider,
	s.create_time,
	s.complete_time,
	s.status
FROM subscription_orders AS s
LEFT JOIN users u ON u.id = s.user_id
LEFT JOIN subscription_plans p ON p.id = s.plan_id
%s`, subscriptionWhereSQL))
		args = append(args, subscriptionWhereArgs...)
	}

	if len(branches) == 0 {
		return emptyRechargeAuditBadgeBaseSQL(), nil
	}

	return strings.Join(branches, "\nUNION ALL\n"), args
}

func parseRechargeAuditStatus(c *gin.Context) (string, bool) {
	status := strings.TrimSpace(c.Query("status"))
	if status == "" || status == "all" {
		return "", true
	}
	switch status {
	case common.TopUpStatusPending,
		common.TopUpStatusSuccess,
		common.TopUpStatusFailed,
		common.TopUpStatusExpired:
		return status, true
	default:
		common.ApiErrorMsg(c, "invalid status")
		return "", false
	}
}

func parseRechargeAuditTimeRange(c *gin.Context) (int64, int64) {
	startTime, _ := strconv.ParseInt(strings.TrimSpace(c.Query("start_time")), 10, 64)
	endTime, _ := strconv.ParseInt(strings.TrimSpace(c.Query("end_time")), 10, 64)
	if startTime > 0 || endTime > 0 {
		return startTime, endTime
	}

	windowHoursRaw := strings.TrimSpace(c.Query("window_hours"))
	if windowHoursRaw == "" {
		return 0, 0
	}
	windowHours, err := strconv.ParseFloat(windowHoursRaw, 64)
	if err != nil || windowHours <= 0 {
		return 0, 0
	}
	const maxWindowHours = 24 * 365
	if windowHours > maxWindowHours {
		windowHours = maxWindowHours
	}
	endTime = common.GetTimestamp()
	startTime = endTime - int64(windowHours*60*60)
	if startTime < 0 {
		startTime = 0
	}
	return startTime, endTime
}

func parseRechargeAuditUserID(c *gin.Context) (int, bool) {
	raw := strings.TrimSpace(c.Query("user_id"))
	if raw == "" {
		return 0, true
	}
	userID, err := strconv.Atoi(raw)
	if err != nil || userID <= 0 {
		common.ApiErrorMsg(c, "invalid user_id")
		return 0, false
	}
	return userID, true
}

func isRechargeAuditBadgeOnly(c *gin.Context) bool {
	switch strings.ToLower(strings.TrimSpace(c.Query("badge_only"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func parseRechargeAuditOrderCursor(raw string) (rechargeAuditOrderCursor, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return rechargeAuditOrderCursor{}, false
	}
	parts := strings.Split(raw, ":")
	if len(parts) != 3 {
		return rechargeAuditOrderCursor{}, false
	}
	completeTime, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || completeTime < 0 {
		return rechargeAuditOrderCursor{}, false
	}
	orderRank, err := strconv.Atoi(parts[1])
	if err != nil || orderRank < 0 {
		return rechargeAuditOrderCursor{}, false
	}
	id, err := strconv.Atoi(parts[2])
	if err != nil || id < 0 {
		return rechargeAuditOrderCursor{}, false
	}
	return rechargeAuditOrderCursor{
		CompleteTime: completeTime,
		OrderRank:    orderRank,
		ID:           id,
	}, true
}

func formatRechargeAuditOrderCursor(cursor rechargeAuditOrderCursor) string {
	if cursor.CompleteTime < 0 || cursor.OrderRank < 0 || cursor.ID < 0 {
		return ""
	}
	return fmt.Sprintf("%d:%d:%d", cursor.CompleteTime, cursor.OrderRank, cursor.ID)
}

func rechargeAuditOrderRankSQL() string {
	return "CASE WHEN o.order_type = 'subscription' THEN 2 ELSE 1 END"
}

func rechargeAuditBadgeSummary(filters rechargeAuditFilters, rawCursor string) (rechargeAuditOrderCursor, int64, error) {
	if strings.TrimSpace(filters.keyword) != "" {
		baseSQL, baseArgs := orderManagementBadgeBaseSQL(filters)
		whereSQL, whereArgs := orderManagementWhereSQL(filters)
		latestOrderCursor, err := latestRechargeAuditOrderCursor(baseSQL, baseArgs, whereSQL, whereArgs)
		if err != nil {
			return rechargeAuditOrderCursor{}, 0, err
		}
		newOrderCount, err := countRechargeAuditOrdersAfterCursor(rawCursor, baseSQL, baseArgs, whereSQL, whereArgs)
		if err != nil {
			return rechargeAuditOrderCursor{}, 0, err
		}
		return latestOrderCursor, newOrderCount, nil
	}

	latestOrderCursor, err := latestRechargeAuditBadgeOrderCursor(filters)
	if err != nil {
		return rechargeAuditOrderCursor{}, 0, err
	}
	newOrderCount, err := countRechargeAuditBadgeOrdersAfterCursor(rawCursor, filters)
	if err != nil {
		return rechargeAuditOrderCursor{}, 0, err
	}
	return latestOrderCursor, newOrderCount, nil
}

func latestRechargeAuditBadgeOrderCursor(filters rechargeAuditFilters) (rechargeAuditOrderCursor, error) {
	latest := rechargeAuditOrderCursor{}

	if rechargeAuditIncludesTopup(filters) {
		cursor, err := latestRechargeAuditBadgeTopupCursor(filters)
		if err != nil {
			return rechargeAuditOrderCursor{}, err
		}
		if compareRechargeAuditOrderCursor(cursor, latest) > 0 {
			latest = cursor
		}
	}

	if rechargeAuditIncludesSubscription(filters) {
		cursor, err := latestRechargeAuditBadgeSubscriptionCursor(filters)
		if err != nil {
			return rechargeAuditOrderCursor{}, err
		}
		if compareRechargeAuditOrderCursor(cursor, latest) > 0 {
			latest = cursor
		}
	}

	return latest, nil
}

func latestRechargeAuditBadgeTopupCursor(filters rechargeAuditFilters) (rechargeAuditOrderCursor, error) {
	whereSQL, whereArgs := orderManagementBranchWhereSQL("t", []string{`NOT EXISTS (
	SELECT 1 FROM subscription_orders AS so WHERE so.trade_no = t.trade_no
)`}, filters)
	whereSQL = appendOrderManagementCondition(whereSQL, "t.status IN (?, ?, ?) AND t.complete_time > 0")
	args := append([]interface{}{}, whereArgs...)
	args = append(args, common.TopUpStatusSuccess, common.TopUpStatusFailed, common.TopUpStatusExpired)

	cursor := rechargeAuditOrderCursor{}
	querySQL := fmt.Sprintf(
		"SELECT t.complete_time, %d AS order_rank, t.id FROM top_ups AS t %s ORDER BY t.complete_time DESC, t.id DESC LIMIT 1",
		rechargeAuditTopupOrderRank,
		whereSQL,
	)
	if err := model.DB.Raw(querySQL, args...).Scan(&cursor).Error; err != nil {
		return rechargeAuditOrderCursor{}, err
	}
	return cursor, nil
}

func latestRechargeAuditBadgeSubscriptionCursor(filters rechargeAuditFilters) (rechargeAuditOrderCursor, error) {
	whereSQL, whereArgs := orderManagementBranchWhereSQL("s", nil, filters)
	whereSQL = appendOrderManagementCondition(whereSQL, "s.status IN (?, ?, ?) AND s.complete_time > 0")
	args := append([]interface{}{}, whereArgs...)
	args = append(args, common.TopUpStatusSuccess, common.TopUpStatusFailed, common.TopUpStatusExpired)

	cursor := rechargeAuditOrderCursor{}
	querySQL := fmt.Sprintf(
		"SELECT s.complete_time, %d AS order_rank, s.id FROM subscription_orders AS s %s ORDER BY s.complete_time DESC, s.id DESC LIMIT 1",
		rechargeAuditSubscriptionOrderRank,
		whereSQL,
	)
	if err := model.DB.Raw(querySQL, args...).Scan(&cursor).Error; err != nil {
		return rechargeAuditOrderCursor{}, err
	}
	return cursor, nil
}

func countRechargeAuditBadgeOrdersAfterCursor(rawCursor string, filters rechargeAuditFilters) (int64, error) {
	cursor, ok := parseRechargeAuditOrderCursor(rawCursor)
	if !ok {
		return 0, nil
	}

	var total int64
	if rechargeAuditIncludesTopup(filters) {
		count, err := countRechargeAuditBadgeTopupsAfterCursor(cursor, filters)
		if err != nil {
			return 0, err
		}
		total += count
	}
	if rechargeAuditIncludesSubscription(filters) {
		count, err := countRechargeAuditBadgeSubscriptionsAfterCursor(cursor, filters)
		if err != nil {
			return 0, err
		}
		total += count
	}
	return total, nil
}

func countRechargeAuditBadgeTopupsAfterCursor(cursor rechargeAuditOrderCursor, filters rechargeAuditFilters) (int64, error) {
	whereSQL, whereArgs := orderManagementBranchWhereSQL("t", []string{`NOT EXISTS (
	SELECT 1 FROM subscription_orders AS so WHERE so.trade_no = t.trade_no
)`}, filters)
	cursorSQL, cursorArgs := rechargeAuditBranchCursorCondition("t", rechargeAuditTopupOrderRank, cursor)
	whereSQL = appendOrderManagementCondition(whereSQL, "t.status IN (?, ?, ?) AND t.complete_time > 0")
	whereSQL = appendOrderManagementCondition(whereSQL, cursorSQL)
	args := append([]interface{}{}, whereArgs...)
	args = append(args, common.TopUpStatusSuccess, common.TopUpStatusFailed, common.TopUpStatusExpired)
	args = append(args, cursorArgs...)

	var count int64
	if err := model.DB.Raw(fmt.Sprintf("SELECT count(*) FROM top_ups AS t %s", whereSQL), args...).Scan(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func countRechargeAuditBadgeSubscriptionsAfterCursor(cursor rechargeAuditOrderCursor, filters rechargeAuditFilters) (int64, error) {
	whereSQL, whereArgs := orderManagementBranchWhereSQL("s", nil, filters)
	cursorSQL, cursorArgs := rechargeAuditBranchCursorCondition("s", rechargeAuditSubscriptionOrderRank, cursor)
	whereSQL = appendOrderManagementCondition(whereSQL, "s.status IN (?, ?, ?) AND s.complete_time > 0")
	whereSQL = appendOrderManagementCondition(whereSQL, cursorSQL)
	args := append([]interface{}{}, whereArgs...)
	args = append(args, common.TopUpStatusSuccess, common.TopUpStatusFailed, common.TopUpStatusExpired)
	args = append(args, cursorArgs...)

	var count int64
	if err := model.DB.Raw(fmt.Sprintf("SELECT count(*) FROM subscription_orders AS s %s", whereSQL), args...).Scan(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func rechargeAuditBranchCursorCondition(alias string, orderRank int, cursor rechargeAuditOrderCursor) (string, []interface{}) {
	completeColumn := alias + ".complete_time"
	if orderRank > cursor.OrderRank {
		return fmt.Sprintf("(%s > ? OR %s = ?)", completeColumn, completeColumn), []interface{}{
			cursor.CompleteTime,
			cursor.CompleteTime,
		}
	}
	if orderRank == cursor.OrderRank {
		return fmt.Sprintf("(%s > ? OR (%s = ? AND %s.id > ?))", completeColumn, completeColumn, alias), []interface{}{
			cursor.CompleteTime,
			cursor.CompleteTime,
			cursor.ID,
		}
	}
	return completeColumn + " > ?", []interface{}{cursor.CompleteTime}
}

func compareRechargeAuditOrderCursor(left rechargeAuditOrderCursor, right rechargeAuditOrderCursor) int {
	if left.CompleteTime != right.CompleteTime {
		if left.CompleteTime > right.CompleteTime {
			return 1
		}
		return -1
	}
	if left.OrderRank != right.OrderRank {
		if left.OrderRank > right.OrderRank {
			return 1
		}
		return -1
	}
	if left.ID != right.ID {
		if left.ID > right.ID {
			return 1
		}
		return -1
	}
	return 0
}

func rechargeAuditIncludesTopup(filters rechargeAuditFilters) bool {
	return filters.orderType == "" || filters.orderType == "all" || filters.orderType == "topup"
}

func rechargeAuditIncludesSubscription(filters rechargeAuditFilters) bool {
	return filters.orderType == "" || filters.orderType == "all" || filters.orderType == "subscription"
}

func countRechargeAuditOrdersAfterCursor(rawCursor string, baseSQL string, baseArgs []interface{}, whereSQL string, whereArgs []interface{}) (int64, error) {
	cursor, ok := parseRechargeAuditOrderCursor(rawCursor)
	if !ok {
		return 0, nil
	}
	rankSQL := rechargeAuditOrderRankSQL()
	newOrderCondition := fmt.Sprintf(`o.status IN (?, ?, ?) AND o.complete_time > 0 AND (
	o.complete_time > ?
	OR (o.complete_time = ? AND %s > ?)
	OR (o.complete_time = ? AND %s = ? AND o.id > ?)
)`, rankSQL, rankSQL)
	querySQL := fmt.Sprintf(
		"SELECT count(*) FROM (%s) AS o %s",
		baseSQL,
		appendOrderManagementCondition(whereSQL, newOrderCondition),
	)
	queryArgs := append([]interface{}{}, baseArgs...)
	queryArgs = append(queryArgs, whereArgs...)
	queryArgs = append(queryArgs,
		common.TopUpStatusSuccess,
		common.TopUpStatusFailed,
		common.TopUpStatusExpired,
		cursor.CompleteTime,
		cursor.CompleteTime,
		cursor.OrderRank,
		cursor.CompleteTime,
		cursor.OrderRank,
		cursor.ID,
	)
	var count int64
	if err := model.DB.Raw(querySQL, queryArgs...).Scan(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func latestRechargeAuditOrderCursor(baseSQL string, baseArgs []interface{}, whereSQL string, whereArgs []interface{}) (rechargeAuditOrderCursor, error) {
	rankSQL := rechargeAuditOrderRankSQL()
	querySQL := fmt.Sprintf(
		"SELECT o.complete_time, %s AS order_rank, o.id FROM (%s) AS o %s ORDER BY o.complete_time DESC, %s DESC, o.id DESC LIMIT 1",
		rankSQL,
		baseSQL,
		appendOrderManagementCondition(whereSQL, "o.status IN (?, ?, ?) AND o.complete_time > 0"),
		rankSQL,
	)
	queryArgs := append([]interface{}{}, baseArgs...)
	queryArgs = append(queryArgs, whereArgs...)
	queryArgs = append(queryArgs,
		common.TopUpStatusSuccess,
		common.TopUpStatusFailed,
		common.TopUpStatusExpired,
	)
	cursor := rechargeAuditOrderCursor{}
	if err := model.DB.Raw(querySQL, queryArgs...).Scan(&cursor).Error; err != nil {
		return rechargeAuditOrderCursor{}, err
	}
	return cursor, nil
}

func orderManagementBranchWhereSQL(alias string, baseClauses []string, filters rechargeAuditFilters) (string, []interface{}) {
	clauses := append([]string{}, baseClauses...)
	args := make([]interface{}, 0)
	if filters.userID > 0 {
		clauses = append(clauses, alias+".user_id = ?")
		args = append(args, filters.userID)
	}
	if filters.status != "" {
		clauses = append(clauses, alias+".status = ?")
		args = append(args, filters.status)
	}
	if filters.provider != "" {
		clauses = append(clauses, alias+".payment_provider = ?")
		args = append(args, filters.provider)
	}
	if filters.startTime > 0 {
		clauses = append(clauses, alias+".create_time >= ?")
		args = append(args, filters.startTime)
	}
	if filters.endTime > 0 {
		clauses = append(clauses, alias+".create_time <= ?")
		args = append(args, filters.endTime)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func emptyRechargeAuditBaseSQL() string {
	return `
SELECT
	'' AS order_type,
	0 AS id,
	0 AS user_id,
	'' AS username,
	0 AS amount,
	0 AS credit_amount,
	0 AS credit_quota,
	'' AS product_name,
	0 AS money,
	0 AS paid_amount,
	'' AS paid_currency,
	'' AS trade_no,
	'' AS payment_method,
	'' AS payment_provider,
	0 AS price_snapshot,
	0 AS usd_exchange_rate_snapshot,
	'' AS quota_display_type_snapshot,
	0 AS create_time,
	0 AS complete_time,
	'' AS status,
	'' AS referral_commission_status,
	'' AS referral_commission_error
WHERE 1 = 0`
}

func emptyRechargeAuditBadgeBaseSQL() string {
	return `
SELECT
	'' AS order_type,
	0 AS id,
	0 AS user_id,
	'' AS username,
	'' AS product_name,
	'' AS trade_no,
	'' AS payment_provider,
	0 AS create_time,
	0 AS complete_time,
	'' AS status
WHERE 1 = 0`
}

func orderManagementWhereSQL(filters rechargeAuditFilters) (string, []interface{}) {
	clauses := make([]string, 0)
	args := make([]interface{}, 0)
	if filters.keyword != "" {
		like := "%" + filters.keyword + "%"
		if parsedUserID, err := strconv.Atoi(filters.keyword); err == nil {
			clauses = append(clauses, "(o.trade_no LIKE ? OR o.username LIKE ? OR o.user_id = ?)")
			args = append(args, like, like, parsedUserID)
		} else {
			clauses = append(clauses, "(o.trade_no LIKE ? OR o.username LIKE ? OR o.product_name LIKE ?)")
			args = append(args, like, like, like)
		}
	}
	if len(clauses) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func appendOrderManagementCondition(whereSQL string, condition string) string {
	if strings.TrimSpace(whereSQL) == "" {
		return "WHERE " + condition
	}
	return whereSQL + " AND " + condition
}

func applyRechargeAuditFilters(query *gorm.DB, keyword string, status string, provider string, startTime int64, endTime int64) *gorm.DB {
	if keyword != "" {
		like := "%" + keyword + "%"
		if _, err := strconv.Atoi(keyword); err == nil {
			query = query.Where("t.trade_no LIKE ? OR u.username LIKE ? OR t.user_id = ?", like, like, keyword)
		} else {
			query = query.Where("t.trade_no LIKE ? OR u.username LIKE ?", like, like)
		}
	}
	if status != "" {
		query = query.Where("t.status = ?", status)
	}
	if provider != "" {
		query = query.Where("t.payment_provider = ?", provider)
	}
	if startTime > 0 {
		query = query.Where("t.create_time >= ?", startTime)
	}
	if endTime > 0 {
		query = query.Where("t.create_time <= ?", endTime)
	}
	return query
}

func rechargeAuditCreditAmountExpr() (string, []interface{}) {
	quotaPerUnit := common.QuotaPerUnit
	if quotaPerUnit <= 0 {
		quotaPerUnit = 1
	}
	return "case when t.payment_provider = ? then t.amount * 1.0 / ? when t.payment_provider = ? then t.money else t.amount end", []interface{}{model.PaymentProviderCreem, quotaPerUnit, model.PaymentProviderStripe}
}

func firstPositiveFloat(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func applyRechargeAuditCNYSummary(
	totals *rechargeAuditTotals,
	byCurrency []currencySummary,
	byProvider []providerSummary,
	byStatus []statusSummary,
) {
	if totals != nil && byCurrency != nil {
		for i := range byCurrency {
			cny, rate, missing := referralservice.PaidAmountCNY(
				byCurrency[i].PaidAmount,
				byCurrency[i].Currency,
				"",
			)
			byCurrency[i].PaidAmountCNY = cny
			byCurrency[i].PaidCNYFxRate = rate
			if missing {
				byCurrency[i].FxMissingCount = byCurrency[i].Count
				totals.FxMissingCount += byCurrency[i].Count
				continue
			}
			totals.PaidAmountCNY += cny
		}
	}
	for i := range byProvider {
		cny, rate, missing := referralservice.PaidAmountCNY(
			byProvider[i].PaidAmount,
			byProvider[i].PaidCurrency,
			byProvider[i].PaymentProvider,
		)
		byProvider[i].PaidAmountCNY = cny
		byProvider[i].PaidCNYFxRate = rate
		if missing {
			byProvider[i].FxMissingCount = byProvider[i].Count
		}
	}
	for i := range byStatus {
		cny, rate, missing := referralservice.PaidAmountCNY(
			byStatus[i].PaidAmount,
			byStatus[i].Currency,
			"",
		)
		byStatus[i].PaidAmountCNY = cny
		byStatus[i].PaidCNYFxRate = rate
		if missing {
			byStatus[i].FxMissingCount = byStatus[i].Count
		}
	}
}

func buildRechargeAnomalies(filters rechargeAuditFilters) ([]auditAnomaly, error) {
	now := common.GetTimestamp()
	baseSQL, baseArgs := orderManagementBaseSQL(filters)
	whereSQL, whereArgs := orderManagementWhereSQL(filters)

	var rows []struct {
		TradeNo                  string
		UserID                   int
		Username                 string
		CreateTime               int64
		Status                   string
		PaidAmount               float64
		Money                    float64
		PaymentProvider          string
		ReferralCommissionStatus string
		ReferralCommissionError  string
	}
	queryArgs := append([]interface{}{}, baseArgs...)
	queryArgs = append(queryArgs, whereArgs...)
	if err := model.DB.Raw(fmt.Sprintf("SELECT o.trade_no, o.user_id, o.username, o.create_time, o.status, o.paid_amount, o.money, o.payment_provider, o.referral_commission_status, o.referral_commission_error FROM (%s) AS o %s ORDER BY o.create_time DESC, o.id DESC LIMIT 300", baseSQL, whereSQL), queryArgs...).Scan(&rows).Error; err != nil {
		return nil, err
	}

	anomalies := make([]auditAnomaly, 0)
	for _, row := range rows {
		if row.Status == common.TopUpStatusPending && now-row.CreateTime > 30*60 {
			anomalies = append(anomalies, auditAnomaly{
				Type:      "pending_timeout",
				Severity:  "warning",
				TradeNo:   row.TradeNo,
				UserID:    row.UserID,
				Username:  row.Username,
				Message:   "订单待支付超过 30 分钟",
				CreatedAt: row.CreateTime,
			})
		}
		if row.Status == common.TopUpStatusSuccess && row.PaidAmount <= 0 && row.Money <= 0 {
			anomalies = append(anomalies, auditAnomaly{
				Type:      "zero_paid_success",
				Severity:  "high",
				TradeNo:   row.TradeNo,
				UserID:    row.UserID,
				Username:  row.Username,
				Message:   "成功订单支付金额为 0",
				CreatedAt: row.CreateTime,
			})
		}
		if row.Status == common.TopUpStatusSuccess && row.ReferralCommissionStatus == "failed" {
			anomalies = append(anomalies, auditAnomaly{
				Type:      "referral_commission_failed",
				Severity:  "warning",
				TradeNo:   row.TradeNo,
				UserID:    row.UserID,
				Username:  row.Username,
				Message:   fmt.Sprintf("返佣生成失败：%s", row.ReferralCommissionError),
				CreatedAt: row.CreateTime,
			})
		}
	}
	return anomalies, nil
}
