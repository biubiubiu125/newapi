package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type referralSettingsRequest struct {
	Enabled            bool    `json:"enabled"`
	CookieTTLDays      int     `json:"cookie_ttl_days"`
	DefaultRate        float64 `json:"default_rate"`
	SettleFreezeDays   int     `json:"settle_freeze_days"`
	MinWithdrawAmount  float64 `json:"min_withdraw_amount"`
	WithdrawFee        float64 `json:"withdraw_fee"`
	RedirectPath       string  `json:"redirect_path"`
	RequireApproval    bool    `json:"require_approval"`
	SettlementCurrency string  `json:"settlement_currency"`
	SettlementFxRates  string  `json:"settlement_fx_rates"`
}

type referralApproveRequest struct {
	RateOverride *float64 `json:"rate_override,omitempty"`
	Reason       string   `json:"reason"`
}

type referralStatusRequest struct {
	Reason string `json:"reason"`
}

type referralAdjustRequest struct {
	Amount         float64 `json:"amount"`
	Remark         string  `json:"remark"`
	IdempotencyKey string  `json:"idempotency_key"`
}

type referralWithdrawalReviewRequest struct {
	AdminNote      string `json:"admin_note"`
	RejectReason   string `json:"reject_reason"`
	RejectProofURL string `json:"reject_proof_url"`
}

type referralWithdrawalPayRequest struct {
	AdminNote       string `json:"admin_note"`
	PaymentProofURL string `json:"payment_proof_url"`
	PaymentTxnNo    string `json:"payment_txn_no"`
}

type referralCommissionRetryRequest struct {
	SourceType string `json:"source_type"`
	TradeNo    string `json:"trade_no"`
}

func GetReferralOverview(c *gin.Context) {
	item, err := referralService.GetOverview()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, item)
}

func GetReferralAdminBadges(c *gin.Context) {
	item, err := referralService.GetAdminBadgeCounts(service.ReferralAdminBadgeQuery{
		AfterPendingAffiliateCursor:  c.Query("after_pending_affiliate_cursor"),
		AfterPendingWithdrawalCursor: c.Query("after_pending_withdrawal_cursor"),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, item)
}

func GetReferralSettings(c *gin.Context) {
	common.ApiSuccess(c, referralService.GetSettings())
}

func UpdateReferralSettings(c *gin.Context) {
	adminId := c.GetInt("id")
	var req referralSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	item, err := referralService.UpdateSettings(service.ReferralSettings{
		Enabled:            req.Enabled,
		CookieTTLDays:      req.CookieTTLDays,
		DefaultRate:        req.DefaultRate,
		SettleFreezeDays:   req.SettleFreezeDays,
		MinWithdrawAmount:  req.MinWithdrawAmount,
		WithdrawFee:        req.WithdrawFee,
		RedirectPath:       strings.TrimSpace(req.RedirectPath),
		RequireApproval:    req.RequireApproval,
		SettlementCurrency: strings.TrimSpace(req.SettlementCurrency),
		SettlementFxRates:  strings.TrimSpace(req.SettlementFxRates),
	}, adminId, common.GetClientIP(c), c.GetHeader("User-Agent"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, item)
}

func GetReferralAffiliates(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	items, total, err := referralService.ListAffiliates(service.ReferralListParams{
		Page:     pageInfo.Page,
		PageSize: pageInfo.PageSize,
		Status:   strings.TrimSpace(c.Query("status")),
		Keyword:  strings.TrimSpace(c.Query("keyword")),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func GetPendingReferralAffiliates(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	items, total, err := referralService.ListAffiliates(service.ReferralListParams{
		Page:     pageInfo.Page,
		PageSize: pageInfo.PageSize,
		Status:   "pending",
		Keyword:  strings.TrimSpace(c.Query("keyword")),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func GetReferralBindings(c *gin.Context) {
	userId, ok := parseReferralUserID(c, "user_id")
	if !ok {
		return
	}
	pageInfo := common.GetPageQuery(c)
	items, total, err := referralService.ListAffiliateBindings(userId, service.ReferralListParams{
		Page:     pageInfo.Page,
		PageSize: pageInfo.PageSize,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func GetAdminReferralCommissions(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	if raw := strings.TrimSpace(c.Query("affiliate_user_id")); raw != "" {
		userId, err := strconv.Atoi(raw)
		if err != nil || userId <= 0 {
			common.ApiErrorMsg(c, "invalid affiliate_user_id")
			return
		}
		items, total, err := referralService.ListAffiliateCommissions(userId, service.ReferralListParams{
			Page:     pageInfo.Page,
			PageSize: pageInfo.PageSize,
			Status:   strings.TrimSpace(c.Query("status")),
		})
		if err != nil {
			common.ApiError(c, err)
			return
		}
		pageInfo.SetTotal(int(total))
		pageInfo.SetItems(items)
		common.ApiSuccess(c, pageInfo)
		return
	}
	items, total, err := referralService.ListCommissions(service.ReferralListParams{
		Page:     pageInfo.Page,
		PageSize: pageInfo.PageSize,
		Status:   strings.TrimSpace(c.Query("status")),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func GetReferralCommissionJobs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	items, total, err := referralService.ListCommissionJobs(service.ReferralListParams{
		Page:     pageInfo.Page,
		PageSize: pageInfo.PageSize,
		Status:   strings.TrimSpace(c.Query("status")),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func GetReferralLedgers(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	items, total, err := referralService.ListLedgers(service.ReferralListParams{
		Page:     pageInfo.Page,
		PageSize: pageInfo.PageSize,
		Keyword:  strings.TrimSpace(c.Query("keyword")),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func GetReferralAdminAuditLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	items, total, err := referralService.ListAdminAuditLogs(service.ReferralListParams{
		Page:     pageInfo.Page,
		PageSize: pageInfo.PageSize,
		Keyword:  strings.TrimSpace(c.Query("keyword")),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func ApproveReferralAffiliate(c *gin.Context) {
	adminId, userId, ok := parseAdminReferralTarget(c)
	if !ok {
		return
	}
	var req referralApproveRequest
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		common.ApiError(c, err)
		return
	}
	item, err := referralService.ApproveAffiliate(userId, adminId, req.RateOverride, strings.TrimSpace(req.Reason), common.GetClientIP(c), c.GetHeader("User-Agent"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, item)
}

func SetReferralAffiliateRate(c *gin.Context) {
	adminId, userId, ok := parseAdminReferralTarget(c)
	if !ok {
		return
	}
	var req referralApproveRequest
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		common.ApiError(c, err)
		return
	}
	item, err := referralService.SetAffiliateRateOverride(userId, adminId, req.RateOverride, strings.TrimSpace(req.Reason), common.GetClientIP(c), c.GetHeader("User-Agent"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, item)
}

func RejectReferralAffiliate(c *gin.Context) {
	adminId, userId, ok := parseAdminReferralTarget(c)
	if !ok {
		return
	}
	var req referralStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		common.ApiError(c, err)
		return
	}
	item, err := referralService.RejectAffiliate(userId, adminId, strings.TrimSpace(req.Reason), common.GetClientIP(c), c.GetHeader("User-Agent"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, item)
}

func DisableReferralAffiliate(c *gin.Context) {
	adminId, userId, ok := parseAdminReferralTarget(c)
	if !ok {
		return
	}
	var req referralStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		common.ApiError(c, err)
		return
	}
	item, err := referralService.DisableAffiliate(userId, adminId, strings.TrimSpace(req.Reason), common.GetClientIP(c), c.GetHeader("User-Agent"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, item)
}

func RestoreReferralAffiliate(c *gin.Context) {
	adminId, userId, ok := parseAdminReferralTarget(c)
	if !ok {
		return
	}
	item, err := referralService.RestoreAffiliate(userId, adminId, common.GetClientIP(c), c.GetHeader("User-Agent"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, item)
}

func AdjustReferralAffiliate(c *gin.Context) {
	adminId, userId, ok := parseAdminReferralTarget(c)
	if !ok {
		return
	}
	var req referralAdjustRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if idempotencyKey == "" {
		idempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	}
	item, err := referralService.AdjustAffiliateCommission(service.ReferralAdjustInput{
		UserId:         userId,
		AdminUserId:    adminId,
		Delta:          req.Amount,
		Remark:         strings.TrimSpace(req.Remark),
		IdempotencyKey: idempotencyKey,
		IP:             common.GetClientIP(c),
		UserAgent:      c.GetHeader("User-Agent"),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, item)
}

func FreezeReferralSettlement(c *gin.Context) {
	adminId, userId, ok := parseAdminReferralTarget(c)
	if !ok {
		return
	}
	var req referralStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		common.ApiError(c, err)
		return
	}
	item, err := referralService.FreezeSettlement(userId, adminId, strings.TrimSpace(req.Reason), common.GetClientIP(c), c.GetHeader("User-Agent"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, item)
}

func RestoreReferralSettlement(c *gin.Context) {
	adminId, userId, ok := parseAdminReferralTarget(c)
	if !ok {
		return
	}
	item, err := referralService.RestoreSettlement(userId, adminId, common.GetClientIP(c), c.GetHeader("User-Agent"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, item)
}

func FreezeReferralWithdrawal(c *gin.Context) {
	adminId, userId, ok := parseAdminReferralTarget(c)
	if !ok {
		return
	}
	var req referralStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		common.ApiError(c, err)
		return
	}
	item, err := referralService.FreezeWithdrawal(userId, adminId, strings.TrimSpace(req.Reason), common.GetClientIP(c), c.GetHeader("User-Agent"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, item)
}

func RestoreReferralWithdrawal(c *gin.Context) {
	adminId, userId, ok := parseAdminReferralTarget(c)
	if !ok {
		return
	}
	item, err := referralService.RestoreWithdrawal(userId, adminId, common.GetClientIP(c), c.GetHeader("User-Agent"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, item)
}

func RunReferralSettlementBatch(c *gin.Context) {
	item, err := referralService.RunSettlementBatch()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, item)
}

func GetAdminReferralWithdrawals(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	if raw := strings.TrimSpace(c.Query("affiliate_user_id")); raw != "" {
		userId, err := strconv.Atoi(raw)
		if err != nil || userId <= 0 {
			common.ApiErrorMsg(c, "invalid affiliate_user_id")
			return
		}
		items, total, err := referralService.ListAffiliateWithdrawals(userId, service.ReferralListParams{
			Page:     pageInfo.Page,
			PageSize: pageInfo.PageSize,
			Status:   strings.TrimSpace(c.Query("status")),
		})
		if err != nil {
			common.ApiError(c, err)
			return
		}
		pageInfo.SetTotal(int(total))
		pageInfo.SetItems(items)
		common.ApiSuccess(c, pageInfo)
		return
	}
	items, total, err := referralService.ListWithdrawals(service.ReferralListParams{
		Page:     pageInfo.Page,
		PageSize: pageInfo.PageSize,
		Status:   strings.TrimSpace(c.Query("status")),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func ApproveReferralWithdrawal(c *gin.Context) {
	adminId := c.GetInt("id")
	withdrawalId, ok := parseReferralUserID(c, "id")
	if !ok {
		return
	}
	var req referralWithdrawalReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		common.ApiError(c, err)
		return
	}
	item, err := referralService.ApproveWithdrawal(service.ReferralWithdrawalReviewInput{
		WithdrawalId: withdrawalId,
		AdminUserId:  adminId,
		AdminNote:    strings.TrimSpace(req.AdminNote),
		IP:           common.GetClientIP(c),
		UserAgent:    c.GetHeader("User-Agent"),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, item)
}

func RejectReferralWithdrawal(c *gin.Context) {
	adminId := c.GetInt("id")
	withdrawalId, ok := parseReferralUserID(c, "id")
	if !ok {
		return
	}
	var req referralWithdrawalReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	item, err := referralService.RejectWithdrawal(service.ReferralWithdrawalReviewInput{
		WithdrawalId:   withdrawalId,
		AdminUserId:    adminId,
		AdminNote:      strings.TrimSpace(req.AdminNote),
		RejectReason:   strings.TrimSpace(req.RejectReason),
		RejectProofURL: strings.TrimSpace(req.RejectProofURL),
		IP:             common.GetClientIP(c),
		UserAgent:      c.GetHeader("User-Agent"),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, item)
}

func MarkReferralWithdrawalPaid(c *gin.Context) {
	adminId := c.GetInt("id")
	withdrawalId, ok := parseReferralUserID(c, "id")
	if !ok {
		return
	}
	var req referralWithdrawalPayRequest
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		common.ApiError(c, err)
		return
	}
	item, err := referralService.MarkWithdrawalPaid(service.ReferralWithdrawalPayInput{
		WithdrawalId:    withdrawalId,
		AdminUserId:     adminId,
		AdminNote:       strings.TrimSpace(req.AdminNote),
		PaymentProofURL: strings.TrimSpace(req.PaymentProofURL),
		PaymentTxnNo:    strings.TrimSpace(req.PaymentTxnNo),
		IP:              common.GetClientIP(c),
		UserAgent:       c.GetHeader("User-Agent"),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, item)
}

func RetryReferralCommissionJob(c *gin.Context) {
	var req referralCommissionRetryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := referralService.RetryCommissionJob(req.SourceType, req.TradeNo); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"source_type": strings.ToLower(strings.TrimSpace(req.SourceType)),
		"trade_no":    strings.TrimSpace(req.TradeNo),
	})
}

func parseAdminReferralTarget(c *gin.Context) (adminId int, userId int, ok bool) {
	adminId = c.GetInt("id")
	userId, ok = parseReferralUserID(c, "user_id")
	return adminId, userId, ok
}

func parseReferralUserID(c *gin.Context, key string) (int, bool) {
	value, err := strconv.Atoi(strings.TrimSpace(c.Param(key)))
	if err != nil || value <= 0 {
		common.ApiErrorMsg(c, "invalid id")
		return 0, false
	}
	return value, true
}
