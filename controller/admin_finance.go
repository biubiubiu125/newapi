package controller

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type rechargeAuditOrder struct {
	ID                       int     `json:"id"`
	UserID                   int     `json:"user_id"`
	Username                 string  `json:"username"`
	Amount                   int64   `json:"amount"`
	Money                    float64 `json:"money"`
	PaidAmount               float64 `json:"paid_amount"`
	PaidCurrency             string  `json:"paid_currency"`
	TradeNo                  string  `json:"trade_no"`
	PaymentMethod            string  `json:"payment_method"`
	PaymentProvider          string  `json:"payment_provider"`
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
}

type statusSummary struct {
	Status     string  `json:"status"`
	Currency   string  `json:"currency"`
	Count      int64   `json:"count"`
	PaidAmount float64 `json:"paid_amount"`
}

type currencySummary struct {
	Currency   string  `json:"currency"`
	Count      int64   `json:"count"`
	PaidAmount float64 `json:"paid_amount"`
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
	startTime, _ := strconv.ParseInt(c.Query("start_time"), 10, 64)
	endTime, _ := strconv.ParseInt(c.Query("end_time"), 10, 64)

	query := model.DB.Table("top_ups AS t").
		Select("t.id, t.user_id, u.username, t.amount, t.money, t.paid_amount, t.paid_currency, t.trade_no, t.payment_method, t.payment_provider, t.create_time, t.complete_time, t.status, t.referral_commission_status, t.referral_commission_error").
		Joins("LEFT JOIN users u ON u.id = t.user_id")
	query = applyRechargeAuditFilters(query, keyword, status, provider, startTime, endTime)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	var orders []rechargeAuditOrder
	if err := query.Order("t.id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Scan(&orders).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(orders)

	common.ApiSuccess(c, pageInfo)
}

func GetRechargeAuditSummary(c *gin.Context) {
	keyword := strings.TrimSpace(c.Query("keyword"))
	status := strings.TrimSpace(c.Query("status"))
	provider := strings.TrimSpace(c.Query("provider"))
	startTime, _ := strconv.ParseInt(c.Query("start_time"), 10, 64)
	endTime, _ := strconv.ParseInt(c.Query("end_time"), 10, 64)

	base := model.DB.Table("top_ups AS t").Joins("LEFT JOIN users u ON u.id = t.user_id")
	base = applyRechargeAuditFilters(base, keyword, status, provider, startTime, endTime)

	var totals struct {
		TotalCount   int64   `json:"total_count"`
		SuccessCount int64   `json:"success_count"`
		PendingCount int64   `json:"pending_count"`
		FailedCount  int64   `json:"failed_count"`
		PaidAmount   float64 `json:"paid_amount"`
		CreditAmount int64   `json:"credit_amount"`
	}
	if err := base.Select(`
		count(*) AS total_count,
		coalesce(sum(case when t.status = ? then 1 else 0 end), 0) AS success_count,
		coalesce(sum(case when t.status = ? then 1 else 0 end), 0) AS pending_count,
		coalesce(sum(case when t.status in (?, ?) then 1 else 0 end), 0) AS failed_count,
		coalesce(sum(case when t.status = ? then t.paid_amount else 0 end), 0) AS paid_amount,
		coalesce(sum(case when t.status = ? then t.amount else 0 end), 0) AS credit_amount
	`, common.TopUpStatusSuccess, common.TopUpStatusPending, common.TopUpStatusFailed, common.TopUpStatusExpired, common.TopUpStatusSuccess, common.TopUpStatusSuccess).Scan(&totals).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	var byCurrency []currencySummary
	if err := applyRechargeAuditFilters(model.DB.Table("top_ups AS t").Joins("LEFT JOIN users u ON u.id = t.user_id"), keyword, status, provider, startTime, endTime).
		Select("upper(coalesce(nullif(t.paid_currency, ''), 'CNY')) AS currency, count(*) AS count, coalesce(sum(t.paid_amount), 0) AS paid_amount").
		Where("t.status = ?", common.TopUpStatusSuccess).
		Group("upper(coalesce(nullif(t.paid_currency, ''), 'CNY'))").
		Order("paid_amount desc").
		Scan(&byCurrency).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	var byProvider []providerSummary
	if err := applyRechargeAuditFilters(model.DB.Table("top_ups AS t").Joins("LEFT JOIN users u ON u.id = t.user_id"), keyword, status, provider, startTime, endTime).
		Select("coalesce(nullif(t.payment_provider, ''), 'unknown') AS payment_provider, upper(coalesce(nullif(t.paid_currency, ''), 'CNY')) AS paid_currency, count(*) AS count, coalesce(sum(t.paid_amount), 0) AS paid_amount").
		Group("coalesce(nullif(t.payment_provider, ''), 'unknown'), upper(coalesce(nullif(t.paid_currency, ''), 'CNY'))").
		Order("paid_amount desc").
		Scan(&byProvider).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	var byStatus []statusSummary
	if err := applyRechargeAuditFilters(model.DB.Table("top_ups AS t").Joins("LEFT JOIN users u ON u.id = t.user_id"), keyword, status, provider, startTime, endTime).
		Select("t.status, upper(coalesce(nullif(t.paid_currency, ''), 'CNY')) AS currency, count(*) AS count, coalesce(sum(t.paid_amount), 0) AS paid_amount").
		Group("t.status, upper(coalesce(nullif(t.paid_currency, ''), 'CNY'))").
		Order("count desc").
		Scan(&byStatus).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	anomalies, err := buildRechargeAnomalies(keyword, status, provider, startTime, endTime)
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

func buildRechargeAnomalies(keyword string, status string, provider string, startTime int64, endTime int64) ([]auditAnomaly, error) {
	now := common.GetTimestamp()
	query := model.DB.Table("top_ups AS t").
		Select("t.trade_no, t.user_id, u.username, t.create_time, t.status, t.paid_amount, t.money, t.payment_provider, t.referral_commission_status, t.referral_commission_error").
		Joins("LEFT JOIN users u ON u.id = t.user_id")
	query = applyRechargeAuditFilters(query, keyword, status, provider, startTime, endTime)

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
	if err := query.Order("t.id desc").Limit(300).Scan(&rows).Error; err != nil {
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
