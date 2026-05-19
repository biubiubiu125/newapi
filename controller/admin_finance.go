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

type auditAnomaly struct {
	Type      string `json:"type"`
	Severity  string `json:"severity"`
	TradeNo   string `json:"trade_no,omitempty"`
	UserID    int    `json:"user_id,omitempty"`
	Username  string `json:"username,omitempty"`
	Message   string `json:"message"`
	CreatedAt int64  `json:"created_at,omitempty"`
}

func GetRechargeAudit(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	keyword := strings.TrimSpace(c.Query("keyword"))
	status := strings.TrimSpace(c.Query("status"))
	provider := strings.TrimSpace(c.Query("provider"))
	orderType := strings.TrimSpace(c.Query("order_type"))
	startTime, _ := strconv.ParseInt(c.Query("start_time"), 10, 64)
	endTime, _ := strconv.ParseInt(c.Query("end_time"), 10, 64)

	baseSQL, baseArgs := orderManagementBaseSQL()
	whereSQL, whereArgs := orderManagementWhereSQL(keyword, status, provider, orderType, startTime, endTime)

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
	status := strings.TrimSpace(c.Query("status"))
	provider := strings.TrimSpace(c.Query("provider"))
	orderType := strings.TrimSpace(c.Query("order_type"))
	startTime, _ := strconv.ParseInt(c.Query("start_time"), 10, 64)
	endTime, _ := strconv.ParseInt(c.Query("end_time"), 10, 64)

	baseSQL, baseArgs := orderManagementBaseSQL()
	whereSQL, whereArgs := orderManagementWhereSQL(keyword, status, provider, orderType, startTime, endTime)
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

	anomalies, err := buildRechargeAnomalies(keyword, status, provider, orderType, startTime, endTime)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, gin.H{
		"totals":      totals,
		"by_currency": byCurrency,
		"by_provider": byProvider,
		"by_status":   byStatus,
		"anomalies":   anomalies,
	})
}

func orderManagementBaseSQL() (string, []interface{}) {
	creditExpr, creditArgs := rechargeAuditCreditAmountExpr()
	sql := fmt.Sprintf(`
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
WHERE NOT EXISTS (
	SELECT 1 FROM subscription_orders AS so WHERE so.trade_no = t.trade_no
)
UNION ALL
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
LEFT JOIN subscription_plans p ON p.id = s.plan_id`, creditExpr)
	return sql, creditArgs
}

func orderManagementWhereSQL(keyword string, status string, provider string, orderType string, startTime int64, endTime int64) (string, []interface{}) {
	clauses := make([]string, 0)
	args := make([]interface{}, 0)
	if keyword != "" {
		like := "%" + keyword + "%"
		if parsedUserID, err := strconv.Atoi(keyword); err == nil {
			clauses = append(clauses, "(o.trade_no LIKE ? OR o.username LIKE ? OR o.user_id = ?)")
			args = append(args, like, like, parsedUserID)
		} else {
			clauses = append(clauses, "(o.trade_no LIKE ? OR o.username LIKE ? OR o.product_name LIKE ?)")
			args = append(args, like, like, like)
		}
	}
	if status != "" {
		clauses = append(clauses, "o.status = ?")
		args = append(args, status)
	}
	if provider != "" {
		clauses = append(clauses, "o.payment_provider = ?")
		args = append(args, provider)
	}
	if orderType != "" && orderType != "all" {
		clauses = append(clauses, "o.order_type = ?")
		args = append(args, orderType)
	}
	if startTime > 0 {
		clauses = append(clauses, "o.create_time >= ?")
		args = append(args, startTime)
	}
	if endTime > 0 {
		clauses = append(clauses, "o.create_time <= ?")
		args = append(args, endTime)
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

func buildRechargeAnomalies(keyword string, status string, provider string, orderType string, startTime int64, endTime int64) ([]auditAnomaly, error) {
	now := common.GetTimestamp()
	baseSQL, baseArgs := orderManagementBaseSQL()
	whereSQL, whereArgs := orderManagementWhereSQL(keyword, status, provider, orderType, startTime, endTime)

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
