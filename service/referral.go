package service

import (
	"crypto/hmac"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ReferralCookieName      = "newapi_referral"
	referralAssetURLPrefix  = "/referral-assets/"
	referralDefaultRedirect = "/sign-up"
	ReferralAdminUploadPath = "/api/user/admin/referral/upload"
	ReferralUserUploadPath  = "/api/user/referral/upload"
)

var errReferralFxRateMissing = errors.New(model.ReferralCommissionErrorFxRateMissing)

type ReferralLanding struct {
	Code          string `json:"code"`
	RedirectPath  string `json:"redirect_path"`
	CookieTTLDays int    `json:"cookie_ttl_days"`
}

type ReferralAssetInput struct {
	OwnerUserId int
	Purpose     string
	CreatedBy   string
}

type ReferralProfile struct {
	Id                 int      `json:"id"`
	UserId             int      `json:"user_id"`
	InviteCode         string   `json:"invite_code"`
	Status             string   `json:"status"`
	SourceType         string   `json:"source_type"`
	ApplicantNote      string   `json:"applicant_note"`
	RateOverride       *float64 `json:"rate_override,omitempty"`
	AcquisitionEnabled bool     `json:"acquisition_enabled"`
	SettlementEnabled  bool     `json:"settlement_enabled"`
	WithdrawalEnabled  bool     `json:"withdrawal_enabled"`
	RiskReason         string   `json:"risk_reason"`
	RiskNote           string   `json:"risk_note"`
	ApprovedAt         int64    `json:"approved_at"`
	DisabledAt         int64    `json:"disabled_at"`
}

type ReferralSummary struct {
	Status             string   `json:"status"`
	InviteCode         string   `json:"invite_code"`
	Rate               *float64 `json:"rate,omitempty"`
	AcquisitionEnabled bool     `json:"acquisition_enabled"`
	SettlementEnabled  bool     `json:"settlement_enabled"`
	WithdrawalEnabled  bool     `json:"withdrawal_enabled"`
	ClickCount         int64    `json:"click_count"`
	BoundUserCount     int64    `json:"bound_user_count"`
	PaidUserCount      int64    `json:"paid_user_count"`
	PendingAmount      float64  `json:"pending_amount"`
	AvailableAmount    float64  `json:"available_amount"`
	FrozenAmount       float64  `json:"frozen_amount"`
	WithdrawnAmount    float64  `json:"withdrawn_amount"`
	SettlementCurrency string   `json:"settlement_currency"`
	MinWithdrawAmount  float64  `json:"min_withdraw_amount"`
}

type ReferralCommissionView struct {
	Id                   int     `json:"id"`
	AffiliateId          int     `json:"affiliate_id"`
	AffiliateUserId      int     `json:"affiliate_user_id"`
	AffiliateUsername    string  `json:"affiliate_username,omitempty"`
	AffiliateEmail       string  `json:"affiliate_email,omitempty"`
	SourceType           string  `json:"source_type"`
	SourceOrderId        int     `json:"source_order_id"`
	SourceTradeNo        string  `json:"source_trade_no"`
	InviteeUserId        int     `json:"invitee_user_id"`
	InviteeUsername      string  `json:"invitee_username,omitempty"`
	InviteeEmail         string  `json:"invitee_email,omitempty"`
	OrderType            string  `json:"order_type"`
	BaseAmount           float64 `json:"base_amount"`
	PaidAmount           float64 `json:"paid_amount"`
	PaidCurrency         string  `json:"paid_currency"`
	PaidAmountCNY        float64 `json:"paid_amount_cny"`
	PaidCNYFxRate        float64 `json:"paid_cny_fx_rate"`
	PaidCNYFxMissing     bool    `json:"paid_cny_fx_missing"`
	SettlementCurrency   string  `json:"settlement_currency"`
	SettlementFxRate     float64 `json:"settlement_fx_rate"`
	SettlementBaseAmount float64 `json:"settlement_base_amount"`
	Rate                 float64 `json:"rate"`
	CommissionAmount     float64 `json:"commission_amount"`
	Status               string  `json:"status"`
	SettleAt             int64   `json:"settle_at"`
	AvailableAt          int64   `json:"available_at"`
	FrozenAt             int64   `json:"frozen_at"`
	CreatedAt            int64   `json:"created_at"`
}

type ReferralCommissionJobView struct {
	Id            int    `json:"id"`
	SourceType    string `json:"source_type"`
	SourceTradeNo string `json:"source_trade_no"`
	AffiliateId   int    `json:"affiliate_id"`
	Status        string `json:"status"`
	AttemptCount  int    `json:"attempt_count"`
	LastError     string `json:"last_error"`
	LockedAt      int64  `json:"locked_at"`
	SucceededAt   int64  `json:"succeeded_at"`
	FailedAt      int64  `json:"failed_at"`
	UpdatedAt     int64  `json:"updated_at"`
}

type ReferralBindingView struct {
	Id              int    `json:"id"`
	InviteeUserId   int    `json:"invitee_user_id"`
	InviteeUsername string `json:"invitee_username"`
	InviteeEmail    string `json:"invitee_email"`
	BoundAt         int64  `json:"bound_at"`
}

type ReferralAffiliateView struct {
	Id                 int      `json:"id"`
	UserId             int      `json:"user_id"`
	Username           string   `json:"username"`
	Email              string   `json:"email"`
	InviteCode         string   `json:"invite_code"`
	Status             string   `json:"status"`
	SourceType         string   `json:"source_type"`
	ApplicantNote      string   `json:"applicant_note"`
	RateOverride       *float64 `json:"rate_override,omitempty"`
	Rate               *float64 `json:"rate,omitempty"`
	AcquisitionEnabled bool     `json:"acquisition_enabled"`
	SettlementEnabled  bool     `json:"settlement_enabled"`
	WithdrawalEnabled  bool     `json:"withdrawal_enabled"`
	RiskReason         string   `json:"risk_reason"`
	RiskNote           string   `json:"risk_note"`
	ClickCount         int64    `json:"click_count"`
	BoundUserCount     int64    `json:"bound_user_count"`
	PaidUserCount      int64    `json:"paid_user_count"`
	PendingAmount      float64  `json:"pending_amount"`
	AvailableAmount    float64  `json:"available_amount"`
	FrozenAmount       float64  `json:"frozen_amount"`
	WithdrawnAmount    float64  `json:"withdrawn_amount"`
	SettlementCurrency string   `json:"settlement_currency"`
	ApprovedAt         int64    `json:"approved_at"`
	DisabledAt         int64    `json:"disabled_at"`
	CreatedAt          int64    `json:"created_at"`
	UpdatedAt          int64    `json:"updated_at"`
}

type ReferralWithdrawalView struct {
	Id                 int     `json:"id"`
	AffiliateId        int     `json:"affiliate_id"`
	UserId             int     `json:"user_id"`
	Username           string  `json:"username,omitempty"`
	Email              string  `json:"email,omitempty"`
	SettlementCurrency string  `json:"settlement_currency"`
	Amount             float64 `json:"amount"`
	FeeAmount          float64 `json:"fee_amount"`
	NetAmount          float64 `json:"net_amount"`
	AccountType        string  `json:"account_type"`
	AccountName        string  `json:"account_name"`
	AccountNo          string  `json:"account_no"`
	AccountNoMasked    string  `json:"account_no_masked"`
	AccountNetwork     string  `json:"account_network"`
	QRImageURL         string  `json:"qr_image_url"`
	ApplicantNote      string  `json:"applicant_note"`
	AdminNote          string  `json:"admin_note"`
	PaymentProofURL    string  `json:"payment_proof_url"`
	PaymentTxnNo       string  `json:"payment_txn_no"`
	Status             string  `json:"status"`
	RejectReason       string  `json:"reject_reason"`
	SubmittedAt        int64   `json:"submitted_at"`
	ApprovedAt         int64   `json:"approved_at"`
	PayoutDeadlineAt   int64   `json:"payout_deadline_at"`
	PaidAt             int64   `json:"paid_at"`
	RejectedAt         int64   `json:"rejected_at"`
	CanceledAt         int64   `json:"canceled_at"`
}

type ReferralLedgerView struct {
	Id                 int     `json:"id"`
	AffiliateId        int     `json:"affiliate_id"`
	UserId             int     `json:"user_id"`
	Username           string  `json:"username,omitempty"`
	Email              string  `json:"email,omitempty"`
	CommissionId       int     `json:"commission_id"`
	WithdrawalId       int     `json:"withdrawal_id"`
	Type               string  `json:"type"`
	RefType            string  `json:"ref_type"`
	RefId              string  `json:"ref_id"`
	ExternalRefId      string  `json:"external_ref_id"`
	SettlementCurrency string  `json:"settlement_currency"`
	DeltaPending       float64 `json:"delta_pending"`
	DeltaAvailable     float64 `json:"delta_available"`
	DeltaFrozen        float64 `json:"delta_frozen"`
	DeltaWithdrawn     float64 `json:"delta_withdrawn"`
	Remark             string  `json:"remark"`
	Operator           string  `json:"operator"`
	CreatedAt          int64   `json:"created_at"`
}

type ReferralAdminAuditLogView struct {
	Id             int    `json:"id"`
	Action         string `json:"action"`
	TargetUserId   int    `json:"target_user_id"`
	TargetUsername string `json:"target_username,omitempty"`
	AffiliateId    int    `json:"affiliate_id"`
	AdminUserId    int    `json:"admin_user_id"`
	Reason         string `json:"reason"`
	Ip             string `json:"ip"`
	UserAgent      string `json:"user_agent"`
	OldValue       string `json:"old_value"`
	NewValue       string `json:"new_value"`
	CreatedAt      int64  `json:"created_at"`
}

type ReferralOverview struct {
	TotalAffiliates          int64   `json:"total_affiliates"`
	PendingAffiliates        int64   `json:"pending_affiliates"`
	ApprovedAffiliates       int64   `json:"approved_affiliates"`
	DisabledAffiliates       int64   `json:"disabled_affiliates"`
	ReferralClickCount       int64   `json:"referral_click_count"`
	BoundUserCount           int64   `json:"bound_user_count"`
	EffectivePaidUserCount   int64   `json:"effective_paid_user_count"`
	PendingAmount            float64 `json:"pending_amount"`
	AvailableAmount          float64 `json:"available_amount"`
	FrozenAmount             float64 `json:"frozen_amount"`
	WithdrawnAmount          float64 `json:"withdrawn_amount"`
	SettlementCurrency       string  `json:"settlement_currency"`
	FailedCommissionJobCount int64   `json:"failed_commission_job_count"`
}

type ReferralSettings struct {
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

type ReferralSnapshot struct {
	AffiliateId int
	Rate        float64
	BaseAmount  float64
	PaidAmount  float64
	Currency    string
	Status      string
	Error       string
}

type ReferralApplyInput struct {
	UserId        int
	ApplicantNote string
}

type ReferralWithdrawalCreateInput struct {
	UserId         int
	Amount         float64
	AccountType    string
	AccountName    string
	AccountNo      string
	AccountNetwork string
	QRImageURL     string
	ApplicantNote  string
	IdempotencyKey string
}

type ReferralWithdrawalReviewInput struct {
	WithdrawalId int
	AdminUserId  int
	AdminNote    string
	RejectReason string
	IP           string
	UserAgent    string
}

type ReferralWithdrawalPayInput struct {
	WithdrawalId    int
	AdminUserId     int
	AdminNote       string
	PaymentProofURL string
	PaymentTxnNo    string
	IP              string
	UserAgent       string
}

type ReferralAdjustInput struct {
	UserId         int
	AdminUserId    int
	Delta          float64
	Remark         string
	IdempotencyKey string
	IP             string
	UserAgent      string
}

type ReferralListParams struct {
	Page     int
	PageSize int
	Status   string
	Keyword  string
}

type ReferralService struct{}

func NewReferralService() *ReferralService {
	return &ReferralService{}
}

var ReferralRuntime = NewReferralService()

func (s *ReferralService) IsEnabled() bool {
	return common.ReferralEnabled
}

func (s *ReferralService) GetSettings() ReferralSettings {
	redirectPath := sanitizeReferralRedirectPath(strings.TrimSpace(common.ReferralRedirectPath))
	if redirectPath == "" {
		redirectPath = referralDefaultRedirect
	}
	return ReferralSettings{
		Enabled:            common.ReferralEnabled,
		CookieTTLDays:      common.ReferralCookieTTLDays,
		DefaultRate:        common.ReferralDefaultRate,
		SettleFreezeDays:   common.ReferralSettleFreezeDays,
		MinWithdrawAmount:  common.ReferralMinWithdrawAmount,
		WithdrawFee:        common.ReferralWithdrawFee,
		RedirectPath:       redirectPath,
		RequireApproval:    common.ReferralRequireApproval,
		SettlementCurrency: common.NormalizeReferralSettlementCurrency(common.ReferralSettlementCurrency),
		SettlementFxRates:  common.ReferralSettlementFxRatesToJSONString(),
	}
}

func (s *ReferralService) UpdateSettings(input ReferralSettings, adminUserId int, ip, userAgent string) (ReferralSettings, error) {
	if input.CookieTTLDays < 1 || input.CookieTTLDays > 365 {
		return s.GetSettings(), errors.New("cookie_ttl_days must be between 1 and 365")
	}
	if input.SettleFreezeDays < 0 || input.SettleFreezeDays > 365 {
		return s.GetSettings(), errors.New("settle_freeze_days must be between 0 and 365")
	}
	if err := validateReferralRate(input.DefaultRate); err != nil {
		return s.GetSettings(), err
	}
	if input.MinWithdrawAmount < 0 || math.IsNaN(input.MinWithdrawAmount) || math.IsInf(input.MinWithdrawAmount, 0) {
		return s.GetSettings(), errors.New("min_withdraw_amount must be a finite non-negative number")
	}
	if input.WithdrawFee < 0 || math.IsNaN(input.WithdrawFee) || math.IsInf(input.WithdrawFee, 0) {
		return s.GetSettings(), errors.New("withdraw_fee must be a finite non-negative number")
	}
	rawSettlementCurrency := strings.ToUpper(strings.TrimSpace(input.SettlementCurrency))
	if rawSettlementCurrency != "" && rawSettlementCurrency != "CNY" {
		return s.GetSettings(), errors.New("settlement_currency currently only supports CNY")
	}
	input.SettlementCurrency = "CNY"
	if strings.TrimSpace(input.SettlementFxRates) == "" {
		input.SettlementFxRates = common.ReferralSettlementFxRatesToJSONString()
	} else {
		fxRates, err := common.ParseReferralSettlementFxRatesJSONString(input.SettlementFxRates)
		if err != nil {
			return s.GetSettings(), err
		}
		fxRatesJSONBytes, err := common.Marshal(fxRates)
		if err != nil {
			return s.GetSettings(), err
		}
		input.SettlementFxRates = string(fxRatesJSONBytes)
	}
	rawRedirectPath := strings.TrimSpace(input.RedirectPath)
	input.RedirectPath = sanitizeReferralRedirectPath(rawRedirectPath)
	if rawRedirectPath != "" && input.RedirectPath == "" {
		return s.GetSettings(), errors.New("redirect_path must be an allowed internal path")
	}
	if input.RedirectPath == "" {
		input.RedirectPath = referralDefaultRedirect
	}

	settingsBefore := s.GetSettings()
	updates := []struct {
		key   string
		value string
	}{
		{"ReferralEnabled", fmt.Sprintf("%t", input.Enabled)},
		{"ReferralCookieTTLDays", fmt.Sprintf("%d", input.CookieTTLDays)},
		{"ReferralDefaultRate", fmt.Sprintf("%g", input.DefaultRate)},
		{"ReferralSettleFreezeDays", fmt.Sprintf("%d", input.SettleFreezeDays)},
		{"ReferralMinWithdrawAmount", fmt.Sprintf("%g", input.MinWithdrawAmount)},
		{"ReferralWithdrawFee", fmt.Sprintf("%g", input.WithdrawFee)},
		{"ReferralRedirectPath", input.RedirectPath},
		{"ReferralRequireApproval", fmt.Sprintf("%t", input.RequireApproval)},
		{"ReferralSettlementCurrency", input.SettlementCurrency},
		{"ReferralSettlementFxRates", input.SettlementFxRates},
	}
	for _, update := range updates {
		if err := model.UpdateOption(update.key, update.value); err != nil {
			return settingsBefore, err
		}
	}
	_ = s.recordAdminAudit("referral_settings_update", 0, 0, adminUserId, "settings updated", ip, userAgent, map[string]any{
		"before": settingsBefore,
	}, map[string]any{
		"after": input,
	})
	return s.GetSettings(), nil
}

func (s *ReferralService) cookieSecret() string {
	if secret := strings.TrimSpace(common.ReferralSigningSecret); secret != "" {
		return secret
	}
	if secret := strings.TrimSpace(common.CryptoSecret); secret != "" {
		return secret
	}
	return strings.TrimSpace(common.SessionSecret)
}

func (s *ReferralService) assetSigningSecret() string {
	secret := strings.TrimSpace(common.ReferralAssetSigningSecret)
	if secret != "" {
		return secret
	}
	return s.cookieSecret()
}

func (s *ReferralService) BuildSignedCookieValue(code string, issuedAt time.Time) (string, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return "", errors.New("empty invite code")
	}
	secret := s.cookieSecret()
	if secret == "" {
		return "", errors.New("missing cookie signing secret")
	}
	payload := fmt.Sprintf("%s.%d", code, issuedAt.Unix())
	signature := common.HmacSha256(payload, secret)
	return common.EncodeBase64(payload + "." + signature), nil
}

func (s *ReferralService) ParseSignedCookieValue(raw string) (string, error) {
	secret := s.cookieSecret()
	if secret == "" {
		return "", errors.New("missing cookie signing secret")
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	parts := strings.Split(string(decoded), ".")
	if len(parts) != 3 {
		return "", errors.New("invalid cookie payload")
	}
	code := strings.ToUpper(strings.TrimSpace(parts[0]))
	payload := parts[0] + "." + parts[1]
	expectedSignature := common.HmacSha256(payload, secret)
	if !hmac.Equal([]byte(expectedSignature), []byte(parts[2])) {
		return "", errors.New("invalid cookie signature")
	}
	issuedAt, err := parseInt64(parts[1])
	if err != nil {
		return "", errors.New("invalid cookie timestamp")
	}
	issuedTime := time.Unix(issuedAt, 0)
	if issuedTime.After(time.Now().Add(5 * time.Minute)) {
		return "", errors.New("invalid cookie issued time")
	}
	ttlDays := common.ReferralCookieTTLDays
	if ttlDays <= 0 {
		ttlDays = 30
	}
	if issuedTime.Add(time.Duration(ttlDays) * 24 * time.Hour).Before(time.Now()) {
		return "", errors.New("cookie expired")
	}
	return code, nil
}

func HashReferralRiskValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	secret := strings.TrimSpace(common.CryptoSecret)
	if secret == "" {
		secret = strings.TrimSpace(common.SessionSecret)
	}
	if secret == "" {
		return ""
	}
	return common.HmacSha256(trimmed, secret)
}

func (s *ReferralService) HandleLanding(code string, click model.ReferralClick) (*ReferralLanding, error) {
	if !s.IsEnabled() {
		return nil, errors.New("referral disabled")
	}
	affiliate, err := s.getApprovedAffiliateByCode(code)
	if err != nil {
		return nil, err
	}
	if affiliate == nil || !affiliate.AcquisitionEnabled {
		return nil, errors.New("referral link is invalid or disabled")
	}
	click.AffiliateId = affiliate.Id
	click.InviteCode = affiliate.InviteCode
	click.CreatedAt = time.Now().Unix()
	go func(item model.ReferralClick) {
		_ = model.DB.Create(&item).Error
	}(click)
	redirectPath := strings.TrimSpace(common.ReferralRedirectPath)
	redirectPath = sanitizeReferralRedirectPath(redirectPath)
	if redirectPath == "" {
		redirectPath = referralDefaultRedirect
	}
	return &ReferralLanding{
		Code:          affiliate.InviteCode,
		RedirectPath:  redirectPath,
		CookieTTLDays: maxInt(common.ReferralCookieTTLDays, 30),
	}, nil
}

func (s *ReferralService) ResolveAffiliateCode(codes ...string) string {
	for _, raw := range codes {
		code := strings.TrimSpace(raw)
		if code == "" {
			continue
		}
		if parsed, err := s.ParseSignedCookieValue(code); err == nil && parsed != "" {
			return parsed
		}
		return strings.ToUpper(code)
	}
	return ""
}

func (s *ReferralService) BuildOrderSnapshot(userId int, paidAmount float64, paidCurrency string) (*ReferralSnapshot, error) {
	if !s.IsEnabled() || userId <= 0 || paidAmount <= 0 {
		return nil, nil
	}
	baseAmount := roundMoney(paidAmount)
	status := model.ReferralCommissionJobStatusSkipped
	snapshotError := "no_binding"
	if _, _, _, err := resolveReferralSettlementAmount(paidAmount, paidCurrency); err != nil {
		status = model.ReferralCommissionJobStatusFailed
		snapshotError = err.Error()
	}
	snapshot := &ReferralSnapshot{
		BaseAmount: baseAmount,
		PaidAmount: roundMoney(paidAmount),
		Currency:   strings.ToUpper(strings.TrimSpace(paidCurrency)),
		Status:     status,
		Error:      snapshotError,
	}
	binding := &model.ReferralBinding{}
	if err := model.DB.Where("invitee_user_id = ?", userId).First(binding).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return snapshot, nil
		}
		return nil, err
	}
	affiliate := &model.ReferralAffiliate{}
	if err := model.DB.Where("id = ?", binding.AffiliateId).First(affiliate).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			snapshot.Error = "affiliate_not_found"
			return snapshot, nil
		}
		return nil, err
	}
	if affiliate.Status != model.ReferralAffiliateStatusApproved {
		snapshot.Error = "affiliate_not_approved"
		return snapshot, nil
	}
	if !affiliate.AcquisitionEnabled {
		snapshot.Error = "affiliate_acquisition_disabled"
		return snapshot, nil
	}
	if !affiliate.SettlementEnabled {
		snapshot.Error = "affiliate_settlement_disabled"
		return snapshot, nil
	}
	if snapshotError != "" && snapshotError != "no_binding" {
		return snapshot, nil
	}
	rate := effectiveReferralRate(affiliate.RateOverride)
	if rate <= 0 {
		snapshot.Error = "invalid_rate"
		return snapshot, nil
	}
	snapshot.AffiliateId = affiliate.Id
	snapshot.Rate = rate
	if snapshotError == "" || snapshotError == "no_binding" {
		snapshot.Status = model.ReferralCommissionJobStatusPending
		snapshot.Error = ""
	}
	return snapshot, nil
}

func (s *ReferralService) BindInviteeByCodeWithTx(tx *gorm.DB, inviteeUserId int, code string, bindSource string) error {
	if tx == nil || inviteeUserId <= 0 || !s.IsEnabled() {
		return nil
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return nil
	}
	affiliate := &model.ReferralAffiliate{}
	if err := tx.Where("invite_code = ?", code).First(affiliate).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if affiliate.Status != model.ReferralAffiliateStatusApproved || !affiliate.AcquisitionEnabled {
		return nil
	}
	if affiliate.UserId == inviteeUserId {
		return errors.New("self invite is not allowed")
	}
	if err := s.lockReferralBindingUsersTx(tx, inviteeUserId, affiliate.UserId); err != nil {
		return err
	}
	existing := &model.ReferralBinding{}
	if err := tx.Where("invitee_user_id = ?", inviteeUserId).First(existing).Error; err == nil {
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	cyclic, err := s.hasBindingCycle(tx, inviteeUserId, affiliate.UserId)
	if err != nil {
		return err
	}
	if cyclic {
		return errors.New("referral cycle is not allowed")
	}
	binding := &model.ReferralBinding{
		InviteeUserId: inviteeUserId,
		InviterUserId: affiliate.UserId,
		AffiliateId:   affiliate.Id,
		BindSource:    normalizeBindSource(bindSource),
		BindCode:      affiliate.InviteCode,
		BoundAt:       time.Now().Unix(),
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(binding).Error; err != nil {
		return err
	}
	return nil
}

func (s *ReferralService) BindInviteeByCode(inviteeUserId int, code string, bindSource string) error {
	return model.DB.Transaction(func(tx *gorm.DB) error {
		return s.BindInviteeByCodeWithTx(tx, inviteeUserId, code, bindSource)
	})
}

func (s *ReferralService) ApplyAffiliate(input ReferralApplyInput) (*ReferralProfile, error) {
	if input.UserId <= 0 {
		return nil, errors.New("invalid user")
	}
	if !s.IsEnabled() {
		return nil, errors.New("referral disabled")
	}
	now := time.Now().Unix()
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		item := &model.ReferralAffiliate{}
		err := tx.Where("user_id = ?", input.UserId).First(item).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			inviteCode, err := s.generateInviteCodeTx(tx)
			if err != nil {
				return err
			}
			profile := &model.ReferralAffiliate{
				UserId:             input.UserId,
				InviteCode:         inviteCode,
				Status:             model.ReferralAffiliateStatusPending,
				SourceType:         "user_apply",
				ApplicantNote:      strings.TrimSpace(input.ApplicantNote),
				AcquisitionEnabled: false,
				SettlementEnabled:  false,
				WithdrawalEnabled:  false,
			}
			if !common.ReferralRequireApproval {
				profile.Status = model.ReferralAffiliateStatusApproved
				profile.AcquisitionEnabled = true
				profile.SettlementEnabled = true
				profile.WithdrawalEnabled = true
				profile.ApprovedAt = now
			}
			if err := tx.Create(profile).Error; err != nil {
				return err
			}
			if profile.Status == model.ReferralAffiliateStatusApproved {
				return s.ensureCommissionAccountTx(tx, profile.Id, profile.UserId)
			}
			return nil
		case err != nil:
			return err
		default:
			if item.Status == model.ReferralAffiliateStatusDisabled {
				return errors.New("referral affiliate is disabled")
			}
			if item.Status == model.ReferralAffiliateStatusApproved {
				if item.InviteCode == "" {
					inviteCode, err := s.generateInviteCodeTx(tx)
					if err != nil {
						return err
					}
					item.InviteCode = inviteCode
					if err := tx.Save(item).Error; err != nil {
						return err
					}
				}
				return s.ensureCommissionAccountTx(tx, item.Id, item.UserId)
			}
			item.SourceType = "user_apply"
			item.ApplicantNote = strings.TrimSpace(input.ApplicantNote)
			item.RiskReason = ""
			item.RiskNote = ""
			item.DisabledAt = 0
			item.DisabledBy = 0
			if common.ReferralRequireApproval {
				item.Status = model.ReferralAffiliateStatusPending
				item.AcquisitionEnabled = false
				item.SettlementEnabled = false
				item.WithdrawalEnabled = false
				item.ApprovedAt = 0
				item.ApprovedBy = 0
			} else {
				item.Status = model.ReferralAffiliateStatusApproved
				item.AcquisitionEnabled = true
				item.SettlementEnabled = true
				item.WithdrawalEnabled = true
				item.ApprovedAt = now
			}
			if item.InviteCode == "" {
				inviteCode, err := s.generateInviteCodeTx(tx)
				if err != nil {
					return err
				}
				item.InviteCode = inviteCode
			}
			if err := tx.Save(item).Error; err != nil {
				return err
			}
			if item.Status == model.ReferralAffiliateStatusApproved {
				return s.ensureCommissionAccountTx(tx, item.Id, item.UserId)
			}
			return nil
		}
	})
	if err != nil {
		return nil, err
	}
	return s.GetProfile(input.UserId)
}

func (s *ReferralService) GetProfile(userId int) (*ReferralProfile, error) {
	item := &model.ReferralAffiliate{}
	if err := model.DB.Where("user_id = ?", userId).First(item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	sanitizeAffiliateState(item)
	return toReferralProfile(item), nil
}

func (s *ReferralService) GetSummary(userId int) (*ReferralSummary, error) {
	affiliate := &model.ReferralAffiliate{}
	if err := model.DB.Where("user_id = ?", userId).First(affiliate).Error; err != nil {
		return nil, err
	}
	sanitizeAffiliateState(affiliate)
	account, err := s.getOrCreateAccount(affiliate.Id, affiliate.UserId)
	if err != nil {
		return nil, err
	}
	clickCount := int64(0)
	_ = model.DB.Model(&model.ReferralClick{}).Where("affiliate_id = ?", affiliate.Id).Count(&clickCount).Error
	boundCount := int64(0)
	_ = model.DB.Model(&model.ReferralBinding{}).Where("affiliate_id = ?", affiliate.Id).Count(&boundCount).Error
	paidCount := int64(0)
	_ = model.DB.Model(&model.ReferralCommission{}).Where("affiliate_id = ? AND commission_amount > 0", affiliate.Id).Distinct("invitee_user_id").Count(&paidCount).Error
	rate := effectiveReferralRate(affiliate.RateOverride)
	return &ReferralSummary{
		Status:             affiliate.Status,
		InviteCode:         affiliate.InviteCode,
		Rate:               &rate,
		AcquisitionEnabled: affiliate.AcquisitionEnabled,
		SettlementEnabled:  affiliate.SettlementEnabled,
		WithdrawalEnabled:  affiliate.WithdrawalEnabled,
		ClickCount:         clickCount,
		BoundUserCount:     boundCount,
		PaidUserCount:      paidCount,
		PendingAmount:      account.PendingAmount,
		AvailableAmount:    account.AvailableAmount,
		FrozenAmount:       account.FrozenAmount,
		WithdrawnAmount:    account.WithdrawnAmount,
		MinWithdrawAmount:  common.ReferralMinWithdrawAmount,
		SettlementCurrency: accountSettlementCurrency(account.SettlementCurrency),
	}, nil
}

func (s *ReferralService) ListUserCommissions(userId int, params ReferralListParams) ([]ReferralCommissionView, int64, error) {
	affiliate := &model.ReferralAffiliate{}
	if err := model.DB.Where("user_id = ?", userId).First(affiliate).Error; err != nil {
		return nil, 0, err
	}
	return s.listCommissions(params, affiliate.Id, false)
}

func (s *ReferralService) ListCommissions(params ReferralListParams) ([]ReferralCommissionView, int64, error) {
	return s.listCommissions(params, 0, true)
}

func (s *ReferralService) ListAffiliateCommissions(affiliateUserId int, params ReferralListParams) ([]ReferralCommissionView, int64, error) {
	affiliate := &model.ReferralAffiliate{}
	if err := model.DB.Where("user_id = ?", affiliateUserId).First(affiliate).Error; err != nil {
		return nil, 0, err
	}
	return s.listCommissions(params, affiliate.Id, false)
}

func (s *ReferralService) ListCommissionJobs(params ReferralListParams) ([]ReferralCommissionJobView, int64, error) {
	page, pageSize := normalizePage(params.Page, params.PageSize)
	query := model.DB.Model(&model.ReferralCommissionJob{})
	if status := strings.TrimSpace(params.Status); status != "" {
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.ReferralCommissionJob
	if err := query.Order("updated_at desc, id desc").Limit(pageSize).Offset((page - 1) * pageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	items := make([]ReferralCommissionJobView, 0, len(rows))
	for _, row := range rows {
		items = append(items, ReferralCommissionJobView{
			Id:            row.Id,
			SourceType:    row.SourceType,
			SourceTradeNo: row.SourceTradeNo,
			AffiliateId:   row.AffiliateId,
			Status:        row.Status,
			AttemptCount:  row.AttemptCount,
			LastError:     row.LastError,
			LockedAt:      row.LockedAt,
			SucceededAt:   row.SucceededAt,
			FailedAt:      row.FailedAt,
			UpdatedAt:     row.UpdatedAt,
		})
	}
	return items, total, nil
}

func sanitizeAffiliateState(item *model.ReferralAffiliate) {
	if item == nil {
		return
	}
	switch item.Status {
	case model.ReferralAffiliateStatusApproved:
		// Keep stored flags as the source of truth for approved affiliates.
	case model.ReferralAffiliateStatusPending, model.ReferralAffiliateStatusRejected, model.ReferralAffiliateStatusDisabled:
		item.AcquisitionEnabled = false
		item.SettlementEnabled = false
		item.WithdrawalEnabled = false
	}
}

func (s *ReferralService) ListLedgers(params ReferralListParams) ([]ReferralLedgerView, int64, error) {
	page, pageSize := normalizePage(params.Page, params.PageSize)
	query := model.DB.Model(&model.ReferralCommissionLedger{})
	if keyword := strings.TrimSpace(params.Keyword); keyword != "" {
		query = query.Where(
			"type LIKE ? OR ref_type LIKE ? OR external_ref_id LIKE ? OR remark LIKE ?",
			"%"+keyword+"%",
			"%"+keyword+"%",
			"%"+keyword+"%",
			"%"+keyword+"%",
		)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.ReferralCommissionLedger
	if err := query.Order("created_at desc, id desc").Limit(pageSize).Offset((page - 1) * pageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	items := make([]ReferralLedgerView, 0, len(rows))
	for _, row := range rows {
		user := &model.User{}
		_ = model.DB.Select("username,email").Where("id = ?", row.UserId).First(user).Error
		items = append(items, ReferralLedgerView{
			Id:                 row.Id,
			AffiliateId:        row.AffiliateId,
			UserId:             row.UserId,
			Username:           user.Username,
			Email:              user.Email,
			CommissionId:       row.CommissionId,
			WithdrawalId:       row.WithdrawalId,
			Type:               row.Type,
			RefType:            row.RefType,
			RefId:              row.RefId,
			ExternalRefId:      row.ExternalRefId,
			SettlementCurrency: accountSettlementCurrency(row.SettlementCurrency),
			DeltaPending:       row.DeltaPending,
			DeltaAvailable:     row.DeltaAvailable,
			DeltaFrozen:        row.DeltaFrozen,
			DeltaWithdrawn:     row.DeltaWithdrawn,
			Remark:             row.Remark,
			Operator:           row.Operator,
			CreatedAt:          row.CreatedAt,
		})
	}
	return items, total, nil
}

func (s *ReferralService) ListAdminAuditLogs(params ReferralListParams) ([]ReferralAdminAuditLogView, int64, error) {
	page, pageSize := normalizePage(params.Page, params.PageSize)
	query := model.DB.Model(&model.ReferralAdminAuditLog{})
	if keyword := strings.TrimSpace(params.Keyword); keyword != "" {
		query = query.Joins("LEFT JOIN users AS target_users ON target_users.id = referral_admin_audit_logs.target_user_id")
		query = query.Where(
			"referral_admin_audit_logs.action LIKE ? OR referral_admin_audit_logs.reason LIKE ? OR referral_admin_audit_logs.old_value LIKE ? OR referral_admin_audit_logs.new_value LIKE ? OR target_users.username LIKE ?",
			"%"+keyword+"%",
			"%"+keyword+"%",
			"%"+keyword+"%",
			"%"+keyword+"%",
			"%"+keyword+"%",
		)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.ReferralAdminAuditLog
	if err := query.Order("referral_admin_audit_logs.created_at desc, referral_admin_audit_logs.id desc").Limit(pageSize).Offset((page - 1) * pageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	targetUsernames := map[int]string{}
	targetUserIds := make([]int, 0, len(rows))
	for _, row := range rows {
		if row.TargetUserId > 0 {
			targetUserIds = append(targetUserIds, row.TargetUserId)
		}
	}
	if len(targetUserIds) > 0 {
		var users []struct {
			Id       int
			Username string
		}
		if err := model.DB.Model(&model.User{}).Select("id, username").Where("id IN ?", targetUserIds).Scan(&users).Error; err != nil {
			return nil, 0, err
		}
		for _, user := range users {
			targetUsernames[user.Id] = user.Username
		}
	}
	items := make([]ReferralAdminAuditLogView, 0, len(rows))
	for _, row := range rows {
		items = append(items, ReferralAdminAuditLogView{
			Id:             row.Id,
			Action:         row.Action,
			TargetUserId:   row.TargetUserId,
			TargetUsername: targetUsernames[row.TargetUserId],
			AffiliateId:    row.AffiliateId,
			AdminUserId:    row.AdminUserId,
			Reason:         row.Reason,
			Ip:             row.Ip,
			UserAgent:      row.UserAgent,
			OldValue:       row.OldValue,
			NewValue:       row.NewValue,
			CreatedAt:      row.CreatedAt,
		})
	}
	return items, total, nil
}

func (s *ReferralService) ListUserWithdrawals(userId int, params ReferralListParams) ([]ReferralWithdrawalView, int64, error) {
	return s.listWithdrawals(params, userId, false)
}

func (s *ReferralService) ListWithdrawals(params ReferralListParams) ([]ReferralWithdrawalView, int64, error) {
	return s.listWithdrawals(params, 0, true)
}

func (s *ReferralService) ListAffiliateWithdrawals(affiliateUserId int, params ReferralListParams) ([]ReferralWithdrawalView, int64, error) {
	return s.listWithdrawals(params, affiliateUserId, false)
}

func (s *ReferralService) CreateWithdrawal(input ReferralWithdrawalCreateInput) (*ReferralWithdrawalView, error) {
	if !s.IsEnabled() {
		return nil, errors.New("referral is disabled")
	}
	if input.UserId <= 0 {
		return nil, errors.New("invalid user")
	}
	if math.IsNaN(input.Amount) || math.IsInf(input.Amount, 0) || input.Amount <= 0 {
		return nil, errors.New("withdraw amount must be greater than 0")
	}
	if common.ReferralMinWithdrawAmount > 0 && input.Amount < common.ReferralMinWithdrawAmount {
		return nil, errors.New("withdraw amount is below minimum threshold")
	}
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		return nil, errors.New("idempotency key is required")
	}
	var withdrawalId int
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		affiliate := &model.ReferralAffiliate{}
		if err := tx.Where("user_id = ?", input.UserId).First(affiliate).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("referral profile not found")
			}
			return err
		}
		if affiliate.Status != model.ReferralAffiliateStatusApproved || !affiliate.WithdrawalEnabled {
			return errors.New("withdrawal is disabled for current affiliate")
		}
		account := &model.ReferralCommissionAccount{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("affiliate_id = ?", affiliate.Id).First(account).Error; err != nil {
			return err
		}
		existing := &model.ReferralWithdrawal{}
		if err := tx.Where("user_id = ? AND idempotency_key = ?", input.UserId, strings.TrimSpace(input.IdempotencyKey)).First(existing).Error; err == nil {
			if sameWithdrawalRequest(existing, input) {
				withdrawalId = existing.Id
				return nil
			}
			return errors.New("idempotency key conflicts with different payload")
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		fee := roundMoney(common.ReferralWithdrawFee)
		if fee < 0 {
			return errors.New("withdraw fee must be non-negative")
		}
		if fee > input.Amount {
			return errors.New("withdraw fee exceeds withdrawal amount")
		}
		qrImageURL := strings.TrimSpace(input.QRImageURL)
		if err := s.validateWithdrawalAssetTx(tx, input.UserId, qrImageURL, model.ReferralAssetPurposeWithdrawalQR); err != nil {
			return err
		}
		qrImagePath := stripAssetSignature(qrImageURL)
		withdrawal := &model.ReferralWithdrawal{
			AffiliateId:        affiliate.Id,
			UserId:             input.UserId,
			Amount:             roundMoney(input.Amount),
			FeeAmount:          fee,
			NetAmount:          roundMoney(input.Amount - fee),
			SettlementCurrency: accountSettlementCurrency(account.SettlementCurrency),
			AccountType:        strings.ToLower(strings.TrimSpace(input.AccountType)),
			AccountName:        strings.TrimSpace(input.AccountName),
			AccountNo:          strings.TrimSpace(input.AccountNo),
			AccountNetwork:     strings.TrimSpace(input.AccountNetwork),
			QRImageURL:         qrImagePath,
			ApplicantNote:      strings.TrimSpace(input.ApplicantNote),
			Status:             model.ReferralWithdrawalStatusPending,
			IdempotencyKey:     strings.TrimSpace(input.IdempotencyKey),
			SubmittedAt:        time.Now().Unix(),
		}
		if withdrawal.AccountNo == "" {
			return errors.New("withdrawal account is required")
		}
		switch withdrawal.AccountType {
		case "alipay", "wechat":
			withdrawal.AccountNetwork = ""
		case "usdt":
			withdrawal.AccountName = ""
			withdrawal.AccountNetwork = strings.ToUpper(withdrawal.AccountNetwork)
			if withdrawal.AccountNetwork != "TRC20" && withdrawal.AccountNetwork != "BEP20" && withdrawal.AccountNetwork != "POLYGON" {
				return errors.New("invalid withdraw network")
			}
		default:
			return errors.New("invalid withdraw type")
		}
		res := tx.Model(&model.ReferralCommissionAccount{}).
			Where("id = ? AND available_amount >= ?", account.Id, withdrawal.Amount).
			Updates(map[string]any{
				"available_amount": gorm.Expr("available_amount - ?", withdrawal.Amount),
				"frozen_amount":    gorm.Expr("frozen_amount + ?", withdrawal.Amount),
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errors.New("available referral balance is insufficient")
		}
		if err := tx.Create(withdrawal).Error; err != nil {
			return err
		}
		withdrawalId = withdrawal.Id
		if err := s.allocateWithdrawalItemsTx(tx, affiliate.Id, withdrawal.Id, withdrawal.Amount); err != nil {
			return err
		}
		if err := s.createLedgerTx(tx, &model.ReferralCommissionLedger{
			AffiliateId:        affiliate.Id,
			UserId:             affiliate.UserId,
			WithdrawalId:       withdrawal.Id,
			Type:               "withdrawal_freeze",
			RefType:            "withdrawal",
			RefId:              fmt.Sprintf("%d", withdrawal.Id),
			ExternalRefId:      "withdrawal_freeze:" + withdrawal.IdempotencyKey,
			SettlementCurrency: withdrawal.SettlementCurrency,
			DeltaAvailable:     roundMoney(-withdrawal.Amount),
			DeltaFrozen:        roundMoney(withdrawal.Amount),
			Operator:           "user",
			Remark:             "user created referral withdrawal",
			CreatedAt:          time.Now().Unix(),
		}); err != nil {
			return err
		}
		return s.recordAdminAuditTx(tx, "referral_withdrawal_create", input.UserId, affiliate.Id, input.UserId, strings.TrimSpace(input.ApplicantNote), "", "", nil, map[string]any{
			"withdrawal_id":   withdrawal.Id,
			"amount":          withdrawal.Amount,
			"idempotency_key": withdrawal.IdempotencyKey,
			"account_type":    withdrawal.AccountType,
			"account_network": withdrawal.AccountNetwork,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.GetWithdrawalById(withdrawalId, false)
}

func (s *ReferralService) CancelWithdrawal(withdrawalId int, userId int) (*ReferralWithdrawalView, error) {
	if withdrawalId <= 0 || userId <= 0 {
		return nil, errors.New("invalid withdrawal")
	}
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		withdrawal := &model.ReferralWithdrawal{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", withdrawalId, userId).First(withdrawal).Error; err != nil {
			return err
		}
		if withdrawal.Status != model.ReferralWithdrawalStatusPending {
			return errors.New("only pending withdrawals can be canceled")
		}
		account := &model.ReferralCommissionAccount{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("affiliate_id = ?", withdrawal.AffiliateId).First(account).Error; err != nil {
			return err
		}
		res := tx.Model(&model.ReferralWithdrawal{}).
			Where("id = ? AND status = ?", withdrawal.Id, model.ReferralWithdrawalStatusPending).
			Updates(map[string]any{
				"status":      model.ReferralWithdrawalStatusCanceled,
				"canceled_at": time.Now().Unix(),
				"canceled_by": userId,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errors.New("only pending withdrawals can be canceled")
		}
		res = tx.Model(&model.ReferralCommissionAccount{}).
			Where("id = ? AND frozen_amount >= ?", account.Id, withdrawal.Amount).
			Updates(map[string]any{
				"frozen_amount":    gorm.Expr("frozen_amount - ?", withdrawal.Amount),
				"available_amount": gorm.Expr("available_amount + ?", withdrawal.Amount),
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errors.New("withdrawal frozen balance is invalid")
		}
		if err := s.releaseWithdrawalItemsTx(tx, withdrawal.Id); err != nil {
			return err
		}
		if err := s.createLedgerTx(tx, &model.ReferralCommissionLedger{
			AffiliateId:        withdrawal.AffiliateId,
			UserId:             withdrawal.UserId,
			WithdrawalId:       withdrawal.Id,
			Type:               "withdrawal_cancel_release",
			RefType:            "withdrawal",
			RefId:              fmt.Sprintf("%d", withdrawal.Id),
			ExternalRefId:      fmt.Sprintf("withdrawal_cancel_release:%d", withdrawal.Id),
			SettlementCurrency: withdrawal.SettlementCurrency,
			DeltaAvailable:     roundMoney(withdrawal.Amount),
			DeltaFrozen:        roundMoney(-withdrawal.Amount),
			Operator:           "user",
			Remark:             "user canceled referral withdrawal",
			CreatedAt:          time.Now().Unix(),
		}); err != nil {
			return err
		}
		return s.recordAdminAuditTx(tx, "referral_withdrawal_cancel", userId, withdrawal.AffiliateId, userId, "", "", "", nil, map[string]any{
			"withdrawal_id": withdrawal.Id,
			"amount":        withdrawal.Amount,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.GetWithdrawalById(withdrawalId, false)
}

func (s *ReferralService) ApproveWithdrawal(input ReferralWithdrawalReviewInput) (*ReferralWithdrawalView, error) {
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		withdrawal := &model.ReferralWithdrawal{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", input.WithdrawalId).First(withdrawal).Error; err != nil {
			return err
		}
		res := tx.Model(&model.ReferralWithdrawal{}).
			Where("id = ? AND status = ?", withdrawal.Id, model.ReferralWithdrawalStatusPending).
			Updates(map[string]any{
				"status":             model.ReferralWithdrawalStatusApproved,
				"approved_at":        time.Now().Unix(),
				"payout_deadline_at": time.Now().Add(48 * time.Hour).Unix(),
				"reviewed_by":        input.AdminUserId,
				"admin_note":         strings.TrimSpace(input.AdminNote),
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errors.New("only pending withdrawals can be approved")
		}
		if err := s.createLedgerTx(tx, &model.ReferralCommissionLedger{
			AffiliateId:        withdrawal.AffiliateId,
			UserId:             withdrawal.UserId,
			WithdrawalId:       withdrawal.Id,
			Type:               "withdrawal_approve",
			RefType:            "withdrawal",
			RefId:              fmt.Sprintf("%d", withdrawal.Id),
			ExternalRefId:      fmt.Sprintf("withdrawal_approve:%d", withdrawal.Id),
			SettlementCurrency: withdrawal.SettlementCurrency,
			Operator:           "admin",
			Remark:             strings.TrimSpace(input.AdminNote),
			CreatedAt:          time.Now().Unix(),
		}); err != nil {
			return err
		}
		return s.recordAdminAuditTx(tx, "referral_withdrawal_approve", withdrawal.UserId, withdrawal.AffiliateId, input.AdminUserId, strings.TrimSpace(input.AdminNote), strings.TrimSpace(input.IP), strings.TrimSpace(input.UserAgent), nil, map[string]any{
			"withdrawal_id": withdrawal.Id,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.GetWithdrawalById(input.WithdrawalId, true)
}

func (s *ReferralService) RejectWithdrawal(input ReferralWithdrawalReviewInput) (*ReferralWithdrawalView, error) {
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		withdrawal := &model.ReferralWithdrawal{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", input.WithdrawalId).First(withdrawal).Error; err != nil {
			return err
		}
		if withdrawal.Status != model.ReferralWithdrawalStatusPending && withdrawal.Status != model.ReferralWithdrawalStatusApproved {
			return errors.New("withdrawal can not be rejected")
		}
		account := &model.ReferralCommissionAccount{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("affiliate_id = ?", withdrawal.AffiliateId).First(account).Error; err != nil {
			return err
		}
		res := tx.Model(&model.ReferralWithdrawal{}).
			Where("id = ? AND status IN ?", withdrawal.Id, []string{model.ReferralWithdrawalStatusPending, model.ReferralWithdrawalStatusApproved}).
			Updates(map[string]any{
				"status":        model.ReferralWithdrawalStatusRejected,
				"rejected_at":   time.Now().Unix(),
				"rejected_by":   input.AdminUserId,
				"reject_reason": strings.TrimSpace(input.RejectReason),
				"admin_note":    strings.TrimSpace(input.AdminNote),
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errors.New("withdrawal can not be rejected")
		}
		res = tx.Model(&model.ReferralCommissionAccount{}).
			Where("id = ? AND frozen_amount >= ?", account.Id, withdrawal.Amount).
			Updates(map[string]any{
				"frozen_amount":    gorm.Expr("frozen_amount - ?", withdrawal.Amount),
				"available_amount": gorm.Expr("available_amount + ?", withdrawal.Amount),
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errors.New("withdrawal frozen balance is invalid")
		}
		if err := s.releaseWithdrawalItemsTx(tx, withdrawal.Id); err != nil {
			return err
		}
		if err := s.createLedgerTx(tx, &model.ReferralCommissionLedger{
			AffiliateId:        withdrawal.AffiliateId,
			UserId:             withdrawal.UserId,
			WithdrawalId:       withdrawal.Id,
			Type:               "withdrawal_reject_release",
			RefType:            "withdrawal",
			RefId:              fmt.Sprintf("%d", withdrawal.Id),
			ExternalRefId:      fmt.Sprintf("withdrawal_reject_release:%d", withdrawal.Id),
			SettlementCurrency: withdrawal.SettlementCurrency,
			DeltaAvailable:     roundMoney(withdrawal.Amount),
			DeltaFrozen:        roundMoney(-withdrawal.Amount),
			Operator:           "admin",
			Remark:             strings.TrimSpace(input.RejectReason),
			CreatedAt:          time.Now().Unix(),
		}); err != nil {
			return err
		}
		return s.recordAdminAuditTx(tx, "referral_withdrawal_reject", withdrawal.UserId, withdrawal.AffiliateId, input.AdminUserId, strings.TrimSpace(input.RejectReason), strings.TrimSpace(input.IP), strings.TrimSpace(input.UserAgent), nil, map[string]any{
			"withdrawal_id": withdrawal.Id,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.GetWithdrawalById(input.WithdrawalId, true)
}

func (s *ReferralService) MarkWithdrawalPaid(input ReferralWithdrawalPayInput) (*ReferralWithdrawalView, error) {
	if strings.TrimSpace(input.PaymentTxnNo) == "" && strings.TrimSpace(input.PaymentProofURL) == "" {
		return nil, errors.New("payment_txn_no or payment_proof_url is required")
	}
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		withdrawal := &model.ReferralWithdrawal{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", input.WithdrawalId).First(withdrawal).Error; err != nil {
			return err
		}
		if withdrawal.Status != model.ReferralWithdrawalStatusApproved {
			return errors.New("only approved withdrawals can be marked paid")
		}
		paymentProofURL := strings.TrimSpace(input.PaymentProofURL)
		if err := s.validatePaymentProofAssetTx(tx, paymentProofURL); err != nil {
			return err
		}
		paymentProofPath := stripAssetSignature(paymentProofURL)
		account := &model.ReferralCommissionAccount{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("affiliate_id = ?", withdrawal.AffiliateId).First(account).Error; err != nil {
			return err
		}
		res := tx.Model(&model.ReferralWithdrawal{}).
			Where("id = ? AND status = ?", withdrawal.Id, model.ReferralWithdrawalStatusApproved).
			Updates(map[string]any{
				"status":            model.ReferralWithdrawalStatusPaid,
				"paid_at":           time.Now().Unix(),
				"admin_note":        strings.TrimSpace(input.AdminNote),
				"payment_proof_url": paymentProofPath,
				"payment_txn_no":    strings.TrimSpace(input.PaymentTxnNo),
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errors.New("only approved withdrawals can be marked paid")
		}
		res = tx.Model(&model.ReferralCommissionAccount{}).
			Where("id = ? AND frozen_amount >= ?", account.Id, withdrawal.Amount).
			Updates(map[string]any{
				"frozen_amount":    gorm.Expr("frozen_amount - ?", withdrawal.Amount),
				"withdrawn_amount": gorm.Expr("withdrawn_amount + ?", withdrawal.Amount),
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errors.New("withdrawal frozen balance is invalid")
		}
		if err := s.markWithdrawalItemsPaidTx(tx, withdrawal.Id); err != nil {
			return err
		}
		if err := s.createLedgerTx(tx, &model.ReferralCommissionLedger{
			AffiliateId:        withdrawal.AffiliateId,
			UserId:             withdrawal.UserId,
			WithdrawalId:       withdrawal.Id,
			Type:               "withdrawal_paid",
			RefType:            "withdrawal",
			RefId:              fmt.Sprintf("%d", withdrawal.Id),
			ExternalRefId:      fmt.Sprintf("withdrawal_paid:%d", withdrawal.Id),
			SettlementCurrency: withdrawal.SettlementCurrency,
			DeltaFrozen:        roundMoney(-withdrawal.Amount),
			DeltaWithdrawn:     roundMoney(withdrawal.Amount),
			Operator:           "admin",
			Remark:             strings.TrimSpace(input.PaymentTxnNo),
			CreatedAt:          time.Now().Unix(),
		}); err != nil {
			return err
		}
		return s.recordAdminAuditTx(tx, "referral_withdrawal_paid", withdrawal.UserId, withdrawal.AffiliateId, input.AdminUserId, strings.TrimSpace(input.AdminNote), strings.TrimSpace(input.IP), strings.TrimSpace(input.UserAgent), nil, map[string]any{
			"withdrawal_id":     withdrawal.Id,
			"payment_txn_no":    strings.TrimSpace(input.PaymentTxnNo),
			"payment_proof_url": paymentProofPath,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.GetWithdrawalById(input.WithdrawalId, true)
}

func (s *ReferralService) GetWithdrawalById(id int, adminView bool) (*ReferralWithdrawalView, error) {
	withdrawal := &model.ReferralWithdrawal{}
	if err := model.DB.Where("id = ?", id).First(withdrawal).Error; err != nil {
		return nil, err
	}
	return s.buildWithdrawalView(withdrawal, adminView)
}

func (s *ReferralService) AdjustAffiliateCommission(input ReferralAdjustInput) (*ReferralAffiliateView, error) {
	if input.UserId <= 0 || input.AdminUserId <= 0 {
		return nil, errors.New("invalid adjust request")
	}
	if math.IsNaN(input.Delta) || math.IsInf(input.Delta, 0) || input.Delta == 0 {
		return nil, errors.New("adjust amount must not be zero")
	}
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		return nil, errors.New("idempotency key is required")
	}
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		affiliate := &model.ReferralAffiliate{}
		if err := tx.Where("user_id = ?", input.UserId).First(affiliate).Error; err != nil {
			return err
		}
		account, err := s.getOrCreateAccountTx(tx, affiliate.Id, affiliate.UserId)
		if err != nil {
			return err
		}
		ledgerType := "commission_adjust_increase"
		if input.Delta < 0 {
			ledgerType = "commission_adjust_decrease"
		}
		ledger := &model.ReferralCommissionLedger{
			AffiliateId:        affiliate.Id,
			UserId:             affiliate.UserId,
			Type:               ledgerType,
			RefType:            "affiliate",
			RefId:              fmt.Sprintf("%d", affiliate.Id),
			ExternalRefId:      "adjust:" + strings.TrimSpace(input.IdempotencyKey),
			SettlementCurrency: accountSettlementCurrency(account.SettlementCurrency),
			DeltaAvailable:     roundMoney(input.Delta),
			Operator:           "admin",
			Remark:             strings.TrimSpace(input.Remark),
			CreatedAt:          time.Now().Unix(),
		}
		existingLedger := &model.ReferralCommissionLedger{}
		if err := tx.Where("external_ref_id = ?", ledger.ExternalRefId).First(existingLedger).Error; err == nil {
			if sameAdjustmentPayload(existingLedger, affiliate.Id, affiliate.UserId, input.Delta, input.Remark) {
				return nil
			}
			return errors.New("idempotency key conflicts with different payload")
		} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := s.createLedgerTx(tx, ledger); err != nil {
			if isDuplicateError(err) {
				if err := tx.Where("external_ref_id = ?", ledger.ExternalRefId).First(existingLedger).Error; err == nil && sameAdjustmentPayload(existingLedger, affiliate.Id, affiliate.UserId, input.Delta, input.Remark) {
					return nil
				}
				return errors.New("idempotency key conflicts with different payload")
			}
			return err
		}
		update := tx.Model(&model.ReferralCommissionAccount{}).Where("id = ?", account.Id)
		if input.Delta < 0 {
			update = update.Where("available_amount >= ?", -input.Delta)
		}
		res := update.Update("available_amount", gorm.Expr("available_amount + ?", input.Delta))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errors.New("available referral balance is insufficient")
		}
		return s.recordAdminAuditTx(tx, "referral_affiliate_adjust", input.UserId, affiliate.Id, input.AdminUserId, strings.TrimSpace(input.Remark), strings.TrimSpace(input.IP), strings.TrimSpace(input.UserAgent), nil, map[string]any{
			"amount":          roundMoney(input.Delta),
			"idempotency_key": strings.TrimSpace(input.IdempotencyKey),
		})
	})
	if err != nil {
		return nil, err
	}
	return s.GetAffiliateView(input.UserId)
}

func (s *ReferralService) GetOverview() (*ReferralOverview, error) {
	out := &ReferralOverview{}
	out.SettlementCurrency = referralSettlementCurrency()
	_ = model.DB.Model(&model.ReferralAffiliate{}).Count(&out.TotalAffiliates).Error
	_ = model.DB.Model(&model.ReferralAffiliate{}).Where("status = ?", model.ReferralAffiliateStatusPending).Count(&out.PendingAffiliates).Error
	_ = model.DB.Model(&model.ReferralAffiliate{}).Where("status = ?", model.ReferralAffiliateStatusApproved).Count(&out.ApprovedAffiliates).Error
	_ = model.DB.Model(&model.ReferralAffiliate{}).Where("status = ?", model.ReferralAffiliateStatusDisabled).Count(&out.DisabledAffiliates).Error
	_ = model.DB.Model(&model.ReferralClick{}).Count(&out.ReferralClickCount).Error
	_ = model.DB.Model(&model.ReferralBinding{}).Count(&out.BoundUserCount).Error
	_ = model.DB.Model(&model.ReferralCommission{}).Distinct("invitee_user_id").Count(&out.EffectivePaidUserCount).Error
	var accounts []model.ReferralCommissionAccount
	if err := model.DB.Find(&accounts).Error; err != nil {
		return nil, err
	}
	for _, item := range accounts {
		out.PendingAmount = roundMoney(out.PendingAmount + item.PendingAmount)
		out.AvailableAmount = roundMoney(out.AvailableAmount + item.AvailableAmount)
		out.FrozenAmount = roundMoney(out.FrozenAmount + item.FrozenAmount)
		out.WithdrawnAmount = roundMoney(out.WithdrawnAmount + item.WithdrawnAmount)
	}
	_ = model.DB.Model(&model.ReferralCommissionJob{}).Where("status = ?", model.ReferralCommissionJobStatusFailed).Count(&out.FailedCommissionJobCount).Error
	return out, nil
}

func (s *ReferralService) ListAffiliates(params ReferralListParams) ([]ReferralAffiliateView, int64, error) {
	page, pageSize := normalizePage(params.Page, params.PageSize)
	query := model.DB.Model(&model.ReferralAffiliate{})
	if status := strings.TrimSpace(params.Status); status != "" {
		query = query.Where("status = ?", status)
	}
	keyword := strings.TrimSpace(params.Keyword)
	if keyword != "" {
		query = query.Joins("LEFT JOIN users ON users.id = referral_affiliates.user_id").Where("users.username LIKE ? OR users.email LIKE ? OR referral_affiliates.invite_code LIKE ?", "%"+keyword+"%", "%"+keyword+"%", "%"+strings.ToUpper(keyword)+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var affiliates []model.ReferralAffiliate
	if err := query.Order("updated_at desc, id desc").Limit(pageSize).Offset((page - 1) * pageSize).Find(&affiliates).Error; err != nil {
		return nil, 0, err
	}
	items := make([]ReferralAffiliateView, 0, len(affiliates))
	for _, affiliate := range affiliates {
		view, err := s.buildAffiliateView(&affiliate)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *view)
	}
	return items, total, nil
}

func (s *ReferralService) ListAffiliateBindings(affiliateUserId int, params ReferralListParams) ([]ReferralBindingView, int64, error) {
	affiliate := &model.ReferralAffiliate{}
	if err := model.DB.Where("user_id = ?", affiliateUserId).First(affiliate).Error; err != nil {
		return nil, 0, err
	}
	page, pageSize := normalizePage(params.Page, params.PageSize)
	query := model.DB.Model(&model.ReferralBinding{}).Where("affiliate_id = ?", affiliate.Id)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var bindings []model.ReferralBinding
	if err := query.Order("bound_at desc, id desc").Limit(pageSize).Offset((page - 1) * pageSize).Find(&bindings).Error; err != nil {
		return nil, 0, err
	}
	items := make([]ReferralBindingView, 0, len(bindings))
	for _, binding := range bindings {
		user := &model.User{}
		_ = model.DB.Select("username,email").Where("id = ?", binding.InviteeUserId).First(user).Error
		items = append(items, ReferralBindingView{
			Id:              binding.Id,
			InviteeUserId:   binding.InviteeUserId,
			InviteeUsername: user.Username,
			InviteeEmail:    user.Email,
			BoundAt:         binding.BoundAt,
		})
	}
	return items, total, nil
}

func (s *ReferralService) ApproveAffiliate(userId int, adminUserId int, rateOverride *float64, ip, userAgent string) (*ReferralAffiliateView, error) {
	now := time.Now().Unix()
	normalizedRate, err := normalizeReferralRateOverride(rateOverride)
	if err != nil {
		return nil, err
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		item := &model.ReferralAffiliate{}
		lookupErr := tx.Where("user_id = ?", userId).First(item).Error
		switch {
		case errors.Is(lookupErr, gorm.ErrRecordNotFound):
			inviteCode, err := s.generateInviteCodeTx(tx)
			if err != nil {
				return err
			}
			item = &model.ReferralAffiliate{
				UserId:             userId,
				InviteCode:         inviteCode,
				Status:             model.ReferralAffiliateStatusApproved,
				SourceType:         "admin_created",
				RateOverride:       normalizedRate,
				AcquisitionEnabled: true,
				SettlementEnabled:  true,
				WithdrawalEnabled:  true,
				ApprovedBy:         adminUserId,
				ApprovedAt:         now,
			}
			if err := tx.Create(item).Error; err != nil {
				return err
			}
			if err := s.recordAdminAuditTx(tx, "referral_affiliate_approve", userId, item.Id, adminUserId, "", ip, userAgent, map[string]any{
				"status":        nil,
				"rate_override": nil,
			}, map[string]any{
				"status":        item.Status,
				"rate_override": item.RateOverride,
			}); err != nil {
				return err
			}
			return s.ensureCommissionAccountTx(tx, item.Id, item.UserId)
		case lookupErr != nil:
			return lookupErr
		default:
			oldValue := map[string]any{"status": item.Status, "rate_override": item.RateOverride}
			item.Status = model.ReferralAffiliateStatusApproved
			item.RateOverride = normalizedRate
			item.AcquisitionEnabled = true
			item.SettlementEnabled = true
			item.WithdrawalEnabled = true
			item.ApprovedBy = adminUserId
			item.ApprovedAt = now
			item.DisabledAt = 0
			item.DisabledBy = 0
			item.RiskReason = ""
			if item.InviteCode == "" {
				inviteCode, err := s.generateInviteCodeTx(tx)
				if err != nil {
					return err
				}
				item.InviteCode = inviteCode
			}
			if err := tx.Save(item).Error; err != nil {
				return err
			}
			if err := s.ensureCommissionAccountTx(tx, item.Id, item.UserId); err != nil {
				return err
			}
			_ = s.recordAdminAuditTx(tx, "referral_affiliate_approve", userId, item.Id, adminUserId, "", ip, userAgent, oldValue, map[string]any{"status": item.Status, "rate_override": item.RateOverride})
			return nil
		}
	})
	if err != nil {
		return nil, err
	}
	return s.GetAffiliateView(userId)
}

func (s *ReferralService) SetAffiliateRateOverride(userId int, adminUserId int, rateOverride *float64, reason, ip, userAgent string) (*ReferralAffiliateView, error) {
	normalizedRate, err := normalizeReferralRateOverride(rateOverride)
	if err != nil {
		return nil, err
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		item := &model.ReferralAffiliate{}
		if err := tx.Where("user_id = ?", userId).First(item).Error; err != nil {
			return err
		}
		oldValue := map[string]any{"rate_override": item.RateOverride}
		item.RateOverride = normalizedRate
		if err := tx.Save(item).Error; err != nil {
			return err
		}
		return s.recordAdminAuditTx(tx, "referral_affiliate_rate", userId, item.Id, adminUserId, reason, ip, userAgent, oldValue, map[string]any{"rate_override": normalizedRate})
	})
	if err != nil {
		return nil, err
	}
	return s.GetAffiliateView(userId)
}

func (s *ReferralService) RejectAffiliate(userId int, adminUserId int, reason, ip, userAgent string) (*ReferralAffiliateView, error) {
	return s.updateAffiliateStatus(userId, adminUserId, model.ReferralAffiliateStatusRejected, false, false, false, reason, "referral_affiliate_reject", ip, userAgent)
}

func (s *ReferralService) DisableAffiliate(userId int, adminUserId int, reason, ip, userAgent string) (*ReferralAffiliateView, error) {
	return s.updateAffiliateStatus(userId, adminUserId, model.ReferralAffiliateStatusDisabled, false, false, false, reason, "referral_affiliate_disable", ip, userAgent)
}

func (s *ReferralService) RestoreAffiliate(userId int, adminUserId int, ip, userAgent string) (*ReferralAffiliateView, error) {
	return s.updateAffiliateStatus(userId, adminUserId, model.ReferralAffiliateStatusApproved, true, true, true, "", "referral_affiliate_restore", ip, userAgent)
}

func (s *ReferralService) FreezeSettlement(userId int, adminUserId int, reason, ip, userAgent string) (*ReferralAffiliateView, error) {
	value := false
	return s.updateAffiliateFlags(userId, adminUserId, &value, nil, reason, "referral_settlement_freeze", ip, userAgent)
}

func (s *ReferralService) RestoreSettlement(userId int, adminUserId int, ip, userAgent string) (*ReferralAffiliateView, error) {
	value := true
	return s.updateAffiliateFlags(userId, adminUserId, &value, nil, "", "referral_settlement_restore", ip, userAgent)
}

func (s *ReferralService) FreezeWithdrawal(userId int, adminUserId int, reason, ip, userAgent string) (*ReferralAffiliateView, error) {
	value := false
	return s.updateAffiliateFlags(userId, adminUserId, nil, &value, reason, "referral_withdrawal_freeze", ip, userAgent)
}

func (s *ReferralService) RestoreWithdrawal(userId int, adminUserId int, ip, userAgent string) (*ReferralAffiliateView, error) {
	value := true
	return s.updateAffiliateFlags(userId, adminUserId, nil, &value, "", "referral_withdrawal_restore", ip, userAgent)
}

func (s *ReferralService) GetAffiliateView(userId int) (*ReferralAffiliateView, error) {
	item := &model.ReferralAffiliate{}
	if err := model.DB.Where("user_id = ?", userId).First(item).Error; err != nil {
		return nil, err
	}
	return s.buildAffiliateView(item)
}

func (s *ReferralService) RunSettlementBatch() (*model.ReferralSettlementBatch, error) {
	now := time.Now().Unix()
	batch := &model.ReferralSettlementBatch{
		BatchNo:   fmt.Sprintf("ref-%d-%s", now, strings.ToLower(common.GetRandomString(6))),
		Status:    "running",
		StartedAt: now,
	}
	if err := model.DB.Create(batch).Error; err != nil {
		return nil, err
	}
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var commissions []model.ReferralCommission
		locking := clause.Locking{Strength: "UPDATE"}
		if !common.UsingSQLite {
			locking.Options = "SKIP LOCKED"
		}
		if err := tx.Clauses(locking).Where("status = ? AND settle_at > 0 AND settle_at <= ?", model.ReferralCommissionStatusPending, now).Order("settle_at asc, id asc").Limit(200).Find(&commissions).Error; err != nil {
			return err
		}
		batch.ScannedCount = len(commissions)
		for _, commission := range commissions {
			affiliate := &model.ReferralAffiliate{}
			if err := tx.Where("id = ?", commission.AffiliateId).First(affiliate).Error; err != nil {
				batch.FailedCount++
				continue
			}
			if affiliate.Status != model.ReferralAffiliateStatusApproved || !affiliate.SettlementEnabled {
				batch.SkippedCount++
				continue
			}
			account, err := s.getOrCreateAccountTx(tx, affiliate.Id, affiliate.UserId)
			if err != nil {
				return err
			}
			res := tx.Model(&model.ReferralCommission{}).
				Where("id = ? AND status = ?", commission.Id, model.ReferralCommissionStatusPending).
				Updates(map[string]any{
					"status":       model.ReferralCommissionStatusAvailable,
					"available_at": now,
					"updated_at":   now,
				})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected != 1 {
				continue
			}
			res = tx.Model(&model.ReferralCommissionAccount{}).
				Where("id = ? AND pending_amount >= ?", account.Id, commission.CommissionAmount).
				Updates(map[string]any{
					"pending_amount":   gorm.Expr("pending_amount - ?", commission.CommissionAmount),
					"available_amount": gorm.Expr("available_amount + ?", commission.CommissionAmount),
				})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected != 1 {
				return errors.New("referral settlement account balance mismatch")
			}
			if err := s.createLedgerTx(tx, &model.ReferralCommissionLedger{
				AffiliateId:        commission.AffiliateId,
				UserId:             affiliate.UserId,
				CommissionId:       commission.Id,
				Type:               "commission_settle",
				RefType:            "commission",
				RefId:              fmt.Sprintf("%d", commission.Id),
				ExternalRefId:      fmt.Sprintf("settle:%d", commission.Id),
				SettlementCurrency: accountSettlementCurrency(commission.SettlementCurrency),
				DeltaPending:       roundMoney(-commission.CommissionAmount),
				DeltaAvailable:     roundMoney(commission.CommissionAmount),
				Operator:           "system",
				CreatedAt:          now,
			}); err != nil {
				return err
			}
			batch.SettledCount++
		}
		return nil
	})
	if err != nil {
		batch.Status = "failed"
		batch.ErrorSummary = err.Error()
	} else {
		batch.Status = "completed"
	}
	batch.FinishedAt = time.Now().Unix()
	_ = model.DB.Save(batch).Error
	return batch, err
}

func (s *ReferralService) SettleDueCommissions() error {
	_, err := s.RunSettlementBatchInline()
	return err
}

func (s *ReferralService) RunSettlementBatchInline() (int, error) {
	now := time.Now().Unix()
	settled := 0
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var commissions []model.ReferralCommission
		locking := clause.Locking{Strength: "UPDATE"}
		if !common.UsingSQLite {
			locking.Options = "SKIP LOCKED"
		}
		if err := tx.Clauses(locking).Where("status = ? AND settle_at > 0 AND settle_at <= ?", model.ReferralCommissionStatusPending, now).Order("settle_at asc, id asc").Limit(200).Find(&commissions).Error; err != nil {
			return err
		}
		for _, commission := range commissions {
			affiliate := &model.ReferralAffiliate{}
			if err := tx.Where("id = ?", commission.AffiliateId).First(affiliate).Error; err != nil {
				return err
			}
			if affiliate.Status != model.ReferralAffiliateStatusApproved || !affiliate.SettlementEnabled {
				continue
			}
			account, err := s.getOrCreateAccountTx(tx, affiliate.Id, affiliate.UserId)
			if err != nil {
				return err
			}
			res := tx.Model(&model.ReferralCommission{}).
				Where("id = ? AND status = ?", commission.Id, model.ReferralCommissionStatusPending).
				Updates(map[string]any{
					"status":       model.ReferralCommissionStatusAvailable,
					"available_at": now,
				})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected != 1 {
				continue
			}
			res = tx.Model(&model.ReferralCommissionAccount{}).
				Where("id = ? AND pending_amount >= ?", account.Id, commission.CommissionAmount).
				Updates(map[string]any{
					"pending_amount":   gorm.Expr("pending_amount - ?", commission.CommissionAmount),
					"available_amount": gorm.Expr("available_amount + ?", commission.CommissionAmount),
				})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected != 1 {
				return errors.New("referral settlement account balance mismatch")
			}
			if err := s.createLedgerTx(tx, &model.ReferralCommissionLedger{
				AffiliateId:        commission.AffiliateId,
				UserId:             affiliate.UserId,
				CommissionId:       commission.Id,
				Type:               "commission_settle",
				RefType:            "commission",
				RefId:              fmt.Sprintf("%d", commission.Id),
				ExternalRefId:      fmt.Sprintf("settle:%d", commission.Id),
				SettlementCurrency: accountSettlementCurrency(commission.SettlementCurrency),
				DeltaPending:       roundMoney(-commission.CommissionAmount),
				DeltaAvailable:     roundMoney(commission.CommissionAmount),
				Operator:           "system",
				CreatedAt:          now,
			}); err != nil {
				return err
			}
			settled++
		}
		return nil
	})
	return settled, err
}

func (s *ReferralService) ProcessTopUpCommission(tradeNo string) error {
	return model.DB.Transaction(func(tx *gorm.DB) error {
		topup := &model.TopUp{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trade_no = ?", tradeNo).First(topup).Error; err != nil {
			return err
		}
		if topup.Status != common.TopUpStatusSuccess {
			return nil
		}
		return s.processCommissionTx(tx, "topup", tradeNo, topup.UserId, topup.Id, "topup", topup.ReferralAffiliateId, topup.ReferralRate, topup.ReferralBaseAmount, topup.PaidAmount, topup.PaidCurrency, &topup.ReferralCommissionStatus, &topup.ReferralCommissionError, &topup.ReferralCommissionAt, func() error {
			return tx.Save(topup).Error
		})
	})
}

func (s *ReferralService) ProcessSubscriptionCommission(tradeNo string) error {
	return model.DB.Transaction(func(tx *gorm.DB) error {
		order := &model.SubscriptionOrder{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trade_no = ?", tradeNo).First(order).Error; err != nil {
			return err
		}
		if order.Status != common.TopUpStatusSuccess {
			return nil
		}
		return s.processCommissionTx(tx, "subscription", tradeNo, order.UserId, order.Id, "subscription", order.ReferralAffiliateId, order.ReferralRate, order.ReferralBaseAmount, order.PaidAmount, order.PaidCurrency, &order.ReferralCommissionStatus, &order.ReferralCommissionError, &order.ReferralCommissionAt, func() error {
			return tx.Save(order).Error
		})
	})
}

func (s *ReferralService) RetryCommissionJob(sourceType string, tradeNo string) error {
	sourceType = strings.ToLower(strings.TrimSpace(sourceType))
	tradeNo = strings.TrimSpace(tradeNo)
	if tradeNo == "" {
		return errors.New("trade_no is required")
	}
	switch sourceType {
	case "topup":
		return s.ProcessTopUpCommission(tradeNo)
	case "subscription":
		return s.ProcessSubscriptionCommission(tradeNo)
	default:
		return errors.New("unsupported source_type")
	}
}

func (s *ReferralService) processCommissionTx(
	tx *gorm.DB,
	sourceType string,
	tradeNo string,
	userId int,
	sourceOrderId int,
	orderType string,
	affiliateId int,
	rate float64,
	baseAmount float64,
	paidAmount float64,
	paidCurrency string,
	statusPtr *string,
	errorPtr *string,
	atPtr *int64,
	save func() error,
) error {
	now := time.Now().Unix()
	job := &model.ReferralCommissionJob{}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("source_type = ? AND source_trade_no = ?", sourceType, tradeNo).First(job).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			job = &model.ReferralCommissionJob{
				SourceType:    sourceType,
				SourceTradeNo: tradeNo,
				AffiliateId:   affiliateId,
				Status:        model.ReferralCommissionJobStatusPending,
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(job).Error; err != nil {
				return err
			}
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("source_type = ? AND source_trade_no = ?", sourceType, tradeNo).First(job).Error; err != nil {
				return err
			}
		} else {
			return err
		}
	}
	if *statusPtr == "succeeded" || *statusPtr == "skipped" {
		return nil
	}
	if job.Status == model.ReferralCommissionJobStatusSucceeded || job.Status == model.ReferralCommissionJobStatusSkipped {
		return nil
	}
	if affiliateId <= 0 || rate <= 0 {
		job.Status = model.ReferralCommissionJobStatusSkipped
		job.LastError = ""
		job.SucceededAt = now
		*statusPtr = "skipped"
		*errorPtr = "missing_referral_snapshot"
		*atPtr = now
		if err := tx.Save(job).Error; err != nil {
			return err
		}
		return save()
	}
	job.Status = model.ReferralCommissionJobStatusProcessing
	job.LockedAt = now
	job.AttemptCount++
	if err := tx.Save(job).Error; err != nil {
		return err
	}
	sourcePaidAmount := paidAmount
	if sourcePaidAmount <= 0 {
		sourcePaidAmount = baseAmount
	}
	settlementBaseAmount, settlementCurrency, settlementFxRate, err := resolveReferralSettlementAmount(sourcePaidAmount, paidCurrency)
	if err != nil {
		job.Status = model.ReferralCommissionJobStatusFailed
		job.LastError = err.Error()
		job.FailedAt = now
		*statusPtr = "failed"
		*errorPtr = err.Error()
		*atPtr = now
		if err := tx.Save(job).Error; err != nil {
			return err
		}
		return save()
	}
	commissionAmount := calculateCommissionAmount(settlementBaseAmount, rate)
	if commissionAmount <= 0 {
		job.Status = model.ReferralCommissionJobStatusSkipped
		job.LastError = ""
		job.SucceededAt = now
		*statusPtr = "skipped"
		*errorPtr = "zero_commission_amount"
		*atPtr = now
		if err := tx.Save(job).Error; err != nil {
			return err
		}
		return save()
	}
	commission := &model.ReferralCommission{
		AffiliateId:          affiliateId,
		AffiliateUserId:      0,
		InviteeUserId:        userId,
		SourceType:           sourceType,
		SourceOrderId:        sourceOrderId,
		SourceTradeNo:        tradeNo,
		OrderType:            strings.TrimSpace(orderType),
		BaseAmount:           settlementBaseAmount,
		PaidAmount:           roundMoney(sourcePaidAmount),
		PaidCurrency:         strings.ToUpper(strings.TrimSpace(paidCurrency)),
		SettlementCurrency:   settlementCurrency,
		SettlementFxRate:     settlementFxRate,
		SettlementBaseAmount: settlementBaseAmount,
		Rate:                 roundMoney(rate),
		CommissionAmount:     commissionAmount,
		Status:               model.ReferralCommissionStatusPending,
		SettleAt:             time.Now().Add(time.Duration(maxInt(common.ReferralSettleFreezeDays, 0)) * 24 * time.Hour).Unix(),
	}
	affiliate := &model.ReferralAffiliate{}
	if err := tx.Where("id = ?", affiliateId).First(affiliate).Error; err != nil {
		job.Status = model.ReferralCommissionJobStatusFailed
		job.LastError = err.Error()
		job.FailedAt = now
		*statusPtr = "failed"
		*errorPtr = err.Error()
		*atPtr = now
		_ = tx.Save(job).Error
		return save()
	}
	if affiliate.Status != model.ReferralAffiliateStatusApproved || !affiliate.SettlementEnabled {
		job.Status = model.ReferralCommissionJobStatusSkipped
		job.LastError = ""
		job.SucceededAt = now
		*statusPtr = "skipped"
		*errorPtr = "affiliate_not_eligible"
		*atPtr = now
		if err := tx.Save(job).Error; err != nil {
			return err
		}
		return save()
	}
	commission.AffiliateUserId = affiliate.UserId
	res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(commission)
	if res.Error != nil {
		job.Status = model.ReferralCommissionJobStatusFailed
		job.LastError = res.Error.Error()
		job.FailedAt = now
		*statusPtr = "failed"
		*errorPtr = res.Error.Error()
		*atPtr = now
		_ = tx.Save(job).Error
		return save()
	}
	if res.RowsAffected == 0 {
		job.Status = model.ReferralCommissionJobStatusSucceeded
		job.LastError = ""
		job.SucceededAt = now
		*statusPtr = "succeeded"
		*errorPtr = ""
		*atPtr = now
		if err := tx.Save(job).Error; err != nil {
			return err
		}
		return save()
	}
	account, err := s.getOrCreateAccountTx(tx, affiliateId, affiliate.UserId)
	if err != nil {
		return err
	}
	res = tx.Model(&model.ReferralCommissionAccount{}).
		Where("id = ?", account.Id).
		Update("pending_amount", gorm.Expr("pending_amount + ?", commissionAmount))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return errors.New("failed to update referral pending amount")
	}
	if err := s.createLedgerTx(tx, &model.ReferralCommissionLedger{
		AffiliateId:        affiliateId,
		UserId:             affiliate.UserId,
		CommissionId:       commission.Id,
		Type:               "commission_accrue",
		RefType:            sourceType,
		RefId:              tradeNo,
		ExternalRefId:      fmt.Sprintf("accrue:%s:%s", sourceType, tradeNo),
		SettlementCurrency: settlementCurrency,
		DeltaPending:       commissionAmount,
		Operator:           "system",
		CreatedAt:          now,
	}); err != nil {
		return err
	}
	job.Status = model.ReferralCommissionJobStatusSucceeded
	job.LastError = ""
	job.SucceededAt = now
	*statusPtr = "succeeded"
	*errorPtr = ""
	*atPtr = now
	if err := tx.Save(job).Error; err != nil {
		return err
	}
	return save()
}

func (s *ReferralService) SignAssetURL(publicPath string) string {
	publicPath = strings.TrimSpace(publicPath)
	if publicPath == "" {
		return ""
	}
	secret := s.assetSigningSecret()
	if secret == "" {
		return ""
	}
	expires := time.Now().Add(24 * time.Hour).Unix()
	payload := fmt.Sprintf("%s|%d", publicPath, expires)
	sig := common.HmacSha256(payload, secret)
	return fmt.Sprintf("%s?expires=%d&sig=%s", publicPath, expires, sig)
}

func (s *ReferralService) VerifyAssetURL(publicPath, expires, sig string) bool {
	secret := s.assetSigningSecret()
	if secret == "" {
		return false
	}
	exp, err := parseInt64(expires)
	if err != nil || exp <= 0 {
		return false
	}
	if time.Now().Unix() > exp {
		return false
	}
	payload := fmt.Sprintf("%s|%d", strings.TrimSpace(publicPath), exp)
	expected := common.HmacSha256(payload, secret)
	return hmac.Equal([]byte(expected), []byte(strings.TrimSpace(sig)))
}

func (s *ReferralService) SaveAsset(data []byte, contentType string, prefix string, input ReferralAssetInput) (string, error) {
	if len(data) == 0 {
		return "", errors.New("empty asset")
	}
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		return "", errors.New("only image upload is supported")
	}
	dir, err := ensureReferralAssetDir()
	if err != nil {
		return "", err
	}
	ext := assetExtensionFromContentType(contentType)
	name := fmt.Sprintf("%s-%d-%s%s", strings.TrimSpace(prefix), time.Now().UnixMilli(), strings.ToLower(common.GetRandomString(8)), ext)
	fullPath := filepath.Join(dir, name)
	if err := os.WriteFile(fullPath, data, 0o644); err != nil {
		return "", err
	}
	publicPath := referralAssetURLPrefix + name
	asset := &model.ReferralAsset{
		OwnerUserId: input.OwnerUserId,
		Purpose:     strings.TrimSpace(input.Purpose),
		StoragePath: publicPath,
		ContentType: contentType,
		Size:        int64(len(data)),
		CreatedBy:   strings.TrimSpace(input.CreatedBy),
		CreatedAt:   time.Now().Unix(),
	}
	if err := model.DB.Create(asset).Error; err != nil {
		_ = os.Remove(fullPath)
		return "", err
	}
	return publicPath, nil
}

func ReferralAssetPath(name string) (string, error) {
	cleanName := filepath.Base(strings.TrimSpace(name))
	if cleanName == "" || cleanName == "." || cleanName == string(filepath.Separator) {
		return "", errors.New("invalid asset name")
	}
	dir, err := ensureReferralAssetDir()
	if err != nil {
		return "", err
	}
	fullPath := filepath.Join(dir, cleanName)
	if _, err := os.Stat(fullPath); err != nil {
		return "", err
	}
	return fullPath, nil
}

func ensureReferralAssetDir() (string, error) {
	dir := filepath.Join(".", "uploads", "referral-assets")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func assetExtensionFromContentType(contentType string) string {
	lower := strings.ToLower(strings.TrimSpace(contentType))
	switch lower {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".bin"
	}
}

func (s *ReferralService) getApprovedAffiliateByCode(code string) (*model.ReferralAffiliate, error) {
	item := &model.ReferralAffiliate{}
	if err := model.DB.Where("invite_code = ? AND status = ?", strings.ToUpper(strings.TrimSpace(code)), model.ReferralAffiliateStatusApproved).First(item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return item, nil
}

func (s *ReferralService) generateInviteCode() string {
	return strings.ToUpper(common.GetRandomString(8))
}

func (s *ReferralService) generateInviteCodeTx(tx *gorm.DB) (string, error) {
	for range 16 {
		code := s.generateInviteCode()
		var count int64
		if err := tx.Model(&model.ReferralAffiliate{}).Where("invite_code = ?", code).Count(&count).Error; err != nil {
			return "", err
		}
		if count == 0 {
			return code, nil
		}
	}
	return "", errors.New("failed to generate unique invite code")
}

func (s *ReferralService) lockReferralBindingUsersTx(tx *gorm.DB, inviteeUserId, inviterUserId int) error {
	if tx == nil || inviteeUserId <= 0 || inviterUserId <= 0 {
		return errors.New("invalid referral binding users")
	}
	ids := []int{inviteeUserId, inviterUserId}
	if inviterUserId < inviteeUserId {
		ids[0], ids[1] = inviterUserId, inviteeUserId
	}
	var users []model.User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id").
		Where("id IN ?", ids).
		Order("id asc").
		Find(&users).Error; err != nil {
		return err
	}
	if len(users) != len(ids) {
		return errors.New("referral binding user not found")
	}
	return nil
}

func (s *ReferralService) hasBindingCycle(tx *gorm.DB, inviteeUserId, inviterUserId int) (bool, error) {
	current := inviterUserId
	visited := map[int]struct{}{inviteeUserId: {}}
	for current > 0 {
		if current == inviteeUserId {
			return true, nil
		}
		if _, ok := visited[current]; ok {
			return true, nil
		}
		visited[current] = struct{}{}
		binding := &model.ReferralBinding{}
		if err := tx.Where("invitee_user_id = ?", current).First(binding).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return false, nil
			}
			return false, err
		}
		current = binding.InviterUserId
	}
	return false, nil
}

func sanitizeReferralRedirectPath(value string) string {
	path := strings.TrimSpace(value)
	if path == "" {
		return referralDefaultRedirect
	}
	path = strings.ReplaceAll(path, "\\", "/")
	if strings.Contains(path, "://") || strings.HasPrefix(path, "//") || strings.Contains(path, "\x00") {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		return ""
	}
	allowed := map[string]struct{}{
		"/sign-up":  {},
		"/register": {},
		"/sign-in":  {},
		"/login":    {},
		"/pricing":  {},
	}
	if _, ok := allowed[path]; ok {
		return path
	}
	return ""
}

func validateReferralRate(rate float64) error {
	if math.IsNaN(rate) || math.IsInf(rate, 0) {
		return errors.New("rate must be a finite number")
	}
	if rate < 0 || rate > 100 {
		return errors.New("rate must be between 0 and 100")
	}
	return nil
}

func normalizeReferralRateOverride(rate *float64) (*float64, error) {
	if rate == nil {
		return nil, nil
	}
	if err := validateReferralRate(*rate); err != nil {
		return nil, err
	}
	normalized := roundMoney(*rate)
	return &normalized, nil
}

func effectiveReferralRate(rateOverride *float64) float64 {
	if rateOverride != nil {
		return roundMoney(*rateOverride)
	}
	return roundMoney(common.ReferralDefaultRate)
}

func referralSettlementCurrency() string {
	return common.NormalizeReferralSettlementCurrency(common.ReferralSettlementCurrency)
}

func accountSettlementCurrency(value string) string {
	currency := strings.ToUpper(strings.TrimSpace(value))
	if currency == "" {
		return referralSettlementCurrency()
	}
	return currency
}

func settlementBaseAmountForView(commission model.ReferralCommission) float64 {
	if commission.SettlementBaseAmount > 0 {
		return commission.SettlementBaseAmount
	}
	return commission.BaseAmount
}

func resolveReferralSettlementAmount(paidAmount float64, paidCurrency string) (float64, string, float64, error) {
	currency := strings.ToUpper(strings.TrimSpace(paidCurrency))
	if currency == "" {
		return 0, referralSettlementCurrency(), 0, errReferralFxRateMissing
	}
	if paidAmount <= 0 || math.IsNaN(paidAmount) || math.IsInf(paidAmount, 0) {
		return 0, referralSettlementCurrency(), 0, errors.New("paid_amount must be a positive finite number")
	}
	settlementCurrency := referralSettlementCurrency()
	if currency == settlementCurrency {
		return roundMoney(paidAmount), settlementCurrency, 1, nil
	}
	rate, ok := referralSettlementFxRate(currency)
	if !ok || rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return 0, settlementCurrency, 0, errReferralFxRateMissing
	}
	return roundMoney(paidAmount * rate), settlementCurrency, roundMoney(rate), nil
}

func referralSettlementFxRate(currency string) (float64, bool) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		return 0, false
	}
	if currency == referralSettlementCurrency() {
		return 1, true
	}
	if currency == "USD" && operation_setting.USDExchangeRate > 0 && !math.IsNaN(operation_setting.USDExchangeRate) && !math.IsInf(operation_setting.USDExchangeRate, 0) {
		return operation_setting.USDExchangeRate, true
	}
	if rate, ok := common.ReferralSettlementFxRatesSnapshot()[currency]; ok && rate > 0 && !math.IsNaN(rate) && !math.IsInf(rate, 0) {
		return rate, true
	}
	return 0, false
}

func PaidAmountCNY(paidAmount float64, paidCurrency string, paymentProvider string) (float64, float64, bool) {
	provider := strings.ToLower(strings.TrimSpace(paymentProvider))
	currency := strings.ToUpper(strings.TrimSpace(paidCurrency))
	if provider == model.PaymentProviderEpay || provider == model.PaymentProviderEpusdt {
		currency = "CNY"
	}
	amount, _, rate, err := resolveReferralSettlementAmount(paidAmount, currency)
	if err != nil {
		return 0, 0, true
	}
	return amount, rate, false
}

func (s *ReferralService) validateWithdrawalAssetTx(tx *gorm.DB, userId int, assetURL string, purpose string) error {
	assetURL = strings.TrimSpace(assetURL)
	if assetURL == "" {
		return nil
	}
	asset := &model.ReferralAsset{}
	if err := tx.Where("storage_path = ?", stripAssetSignature(assetURL)).First(asset).Error; err != nil {
		return errors.New("referral asset not found")
	}
	if asset.OwnerUserId != userId {
		return errors.New("referral asset does not belong to current user")
	}
	if asset.Purpose != purpose {
		return errors.New("referral asset purpose is invalid")
	}
	return nil
}

func (s *ReferralService) validatePaymentProofAssetTx(tx *gorm.DB, assetURL string) error {
	assetURL = strings.TrimSpace(assetURL)
	if assetURL == "" {
		return nil
	}
	asset := &model.ReferralAsset{}
	if err := tx.Where("storage_path = ?", stripAssetSignature(assetURL)).First(asset).Error; err != nil {
		return errors.New("payment proof asset not found")
	}
	if asset.CreatedBy != "admin" || asset.Purpose != model.ReferralAssetPurposePaymentProof {
		return errors.New("payment proof asset purpose is invalid")
	}
	return nil
}

func stripAssetSignature(value string) string {
	trimmed := strings.TrimSpace(value)
	if idx := strings.Index(trimmed, "?"); idx >= 0 {
		return trimmed[:idx]
	}
	return trimmed
}

func sameWithdrawalRequest(existing *model.ReferralWithdrawal, input ReferralWithdrawalCreateInput) bool {
	if existing == nil {
		return false
	}
	return roundMoney(existing.Amount) == roundMoney(input.Amount) &&
		strings.EqualFold(strings.TrimSpace(existing.AccountType), strings.TrimSpace(input.AccountType)) &&
		strings.TrimSpace(existing.AccountName) == strings.TrimSpace(input.AccountName) &&
		strings.TrimSpace(existing.AccountNo) == strings.TrimSpace(input.AccountNo) &&
		strings.EqualFold(strings.TrimSpace(existing.AccountNetwork), strings.TrimSpace(input.AccountNetwork)) &&
		stripAssetSignature(existing.QRImageURL) == stripAssetSignature(input.QRImageURL) &&
		strings.TrimSpace(existing.ApplicantNote) == strings.TrimSpace(input.ApplicantNote)
}

func sameAdjustmentPayload(existing *model.ReferralCommissionLedger, affiliateId int, userId int, delta float64, remark string) bool {
	if existing == nil {
		return false
	}
	return existing.AffiliateId == affiliateId &&
		existing.UserId == userId &&
		roundMoney(existing.DeltaAvailable) == roundMoney(delta) &&
		strings.TrimSpace(existing.Remark) == strings.TrimSpace(remark)
}

func (s *ReferralService) createLedgerTx(tx *gorm.DB, ledger *model.ReferralCommissionLedger) error {
	if tx == nil || ledger == nil {
		return errors.New("invalid ledger")
	}
	return tx.Create(ledger).Error
}

func isDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate") || strings.Contains(msg, "unique") || strings.Contains(msg, "constraint")
}

func normalizeBindSource(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "code":
		return "code"
	default:
		return "cookie"
	}
}

func (s *ReferralService) getOrCreateAccount(affiliateId int, userId int) (*model.ReferralCommissionAccount, error) {
	return s.getOrCreateAccountTx(model.DB, affiliateId, userId)
}

func (s *ReferralService) getOrCreateAccountTx(tx *gorm.DB, affiliateId int, userId int) (*model.ReferralCommissionAccount, error) {
	account := &model.ReferralCommissionAccount{}
	if err := tx.Where("affiliate_id = ?", affiliateId).First(account).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		account = &model.ReferralCommissionAccount{
			AffiliateId:        affiliateId,
			UserId:             userId,
			SettlementCurrency: referralSettlementCurrency(),
		}
		if err := tx.Create(account).Error; err != nil {
			if !isDuplicateError(err) {
				return nil, err
			}
			account = &model.ReferralCommissionAccount{}
			if err := tx.Where("affiliate_id = ?", affiliateId).First(account).Error; err != nil {
				return nil, err
			}
		}
	}
	if strings.TrimSpace(account.SettlementCurrency) == "" {
		account.SettlementCurrency = referralSettlementCurrency()
		if err := tx.Model(&model.ReferralCommissionAccount{}).Where("id = ?", account.Id).Update("settlement_currency", account.SettlementCurrency).Error; err != nil {
			return nil, err
		}
	}
	return account, nil
}

func (s *ReferralService) ensureCommissionAccountTx(tx *gorm.DB, affiliateId int, userId int) error {
	_, err := s.getOrCreateAccountTx(tx, affiliateId, userId)
	return err
}

func (s *ReferralService) buildAffiliateView(affiliate *model.ReferralAffiliate) (*ReferralAffiliateView, error) {
	sanitizeAffiliateState(affiliate)
	user := &model.User{}
	_ = model.DB.Select("username,email").Where("id = ?", affiliate.UserId).First(user).Error
	account, err := s.getOrCreateAccount(affiliate.Id, affiliate.UserId)
	if err != nil {
		return nil, err
	}
	clickCount := int64(0)
	boundCount := int64(0)
	paidCount := int64(0)
	_ = model.DB.Model(&model.ReferralClick{}).Where("affiliate_id = ?", affiliate.Id).Count(&clickCount).Error
	_ = model.DB.Model(&model.ReferralBinding{}).Where("affiliate_id = ?", affiliate.Id).Count(&boundCount).Error
	_ = model.DB.Model(&model.ReferralCommission{}).Where("affiliate_id = ? AND commission_amount > 0", affiliate.Id).Distinct("invitee_user_id").Count(&paidCount).Error
	rate := effectiveReferralRate(affiliate.RateOverride)
	return &ReferralAffiliateView{
		Id:                 affiliate.Id,
		UserId:             affiliate.UserId,
		Username:           user.Username,
		Email:              user.Email,
		InviteCode:         affiliate.InviteCode,
		Status:             affiliate.Status,
		SourceType:         affiliate.SourceType,
		ApplicantNote:      affiliate.ApplicantNote,
		RateOverride:       affiliate.RateOverride,
		Rate:               &rate,
		AcquisitionEnabled: affiliate.AcquisitionEnabled,
		SettlementEnabled:  affiliate.SettlementEnabled,
		WithdrawalEnabled:  affiliate.WithdrawalEnabled,
		RiskReason:         affiliate.RiskReason,
		RiskNote:           affiliate.RiskNote,
		ClickCount:         clickCount,
		BoundUserCount:     boundCount,
		PaidUserCount:      paidCount,
		PendingAmount:      account.PendingAmount,
		AvailableAmount:    account.AvailableAmount,
		FrozenAmount:       account.FrozenAmount,
		WithdrawnAmount:    account.WithdrawnAmount,
		SettlementCurrency: accountSettlementCurrency(account.SettlementCurrency),
		ApprovedAt:         affiliate.ApprovedAt,
		DisabledAt:         affiliate.DisabledAt,
		CreatedAt:          affiliate.CreatedAt,
		UpdatedAt:          affiliate.UpdatedAt,
	}, nil
}

func toReferralProfile(item *model.ReferralAffiliate) *ReferralProfile {
	return &ReferralProfile{
		Id:                 item.Id,
		UserId:             item.UserId,
		InviteCode:         item.InviteCode,
		Status:             item.Status,
		SourceType:         item.SourceType,
		ApplicantNote:      item.ApplicantNote,
		RateOverride:       item.RateOverride,
		AcquisitionEnabled: item.AcquisitionEnabled,
		SettlementEnabled:  item.SettlementEnabled,
		WithdrawalEnabled:  item.WithdrawalEnabled,
		RiskReason:         item.RiskReason,
		RiskNote:           item.RiskNote,
		ApprovedAt:         item.ApprovedAt,
		DisabledAt:         item.DisabledAt,
	}
}

func (s *ReferralService) listCommissions(params ReferralListParams, affiliateId int, global bool) ([]ReferralCommissionView, int64, error) {
	page, pageSize := normalizePage(params.Page, params.PageSize)
	query := model.DB.Model(&model.ReferralCommission{})
	if !global {
		query = query.Where("affiliate_id = ?", affiliateId)
	}
	if status := strings.TrimSpace(params.Status); status != "" {
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var commissions []model.ReferralCommission
	if err := query.Order("created_at desc, id desc").Limit(pageSize).Offset((page - 1) * pageSize).Find(&commissions).Error; err != nil {
		return nil, 0, err
	}
	items := make([]ReferralCommissionView, 0, len(commissions))
	for _, commission := range commissions {
		affiliateUser := &model.User{}
		_ = model.DB.Select("username,email").Where("id = ?", commission.AffiliateUserId).First(affiliateUser).Error
		inviteeUser := &model.User{}
		_ = model.DB.Select("username,email").Where("id = ?", commission.InviteeUserId).First(inviteeUser).Error
		status := commission.Status
		if status != model.ReferralCommissionStatusPending {
			derived, _ := s.deriveCommissionStatus(commission.Id, commission.CommissionAmount)
			if derived != "" {
				status = derived
			}
		}
		paidAmountCNY, paidCNYFxRate, paidCNYFxMissing := PaidAmountCNY(
			commission.PaidAmount,
			commission.PaidCurrency,
			"",
		)
		items = append(items, ReferralCommissionView{
			Id:                   commission.Id,
			AffiliateId:          commission.AffiliateId,
			AffiliateUserId:      commission.AffiliateUserId,
			AffiliateUsername:    affiliateUser.Username,
			AffiliateEmail:       affiliateUser.Email,
			SourceType:           commission.SourceType,
			SourceOrderId:        commission.SourceOrderId,
			SourceTradeNo:        commission.SourceTradeNo,
			InviteeUserId:        commission.InviteeUserId,
			InviteeUsername:      inviteeUser.Username,
			InviteeEmail:         inviteeUser.Email,
			OrderType:            commission.OrderType,
			BaseAmount:           commission.BaseAmount,
			PaidAmount:           commission.PaidAmount,
			PaidCurrency:         commission.PaidCurrency,
			PaidAmountCNY:        paidAmountCNY,
			PaidCNYFxRate:        paidCNYFxRate,
			PaidCNYFxMissing:     paidCNYFxMissing,
			SettlementCurrency:   accountSettlementCurrency(commission.SettlementCurrency),
			SettlementFxRate:     commission.SettlementFxRate,
			SettlementBaseAmount: settlementBaseAmountForView(commission),
			Rate:                 commission.Rate,
			CommissionAmount:     commission.CommissionAmount,
			Status:               status,
			SettleAt:             commission.SettleAt,
			AvailableAt:          commission.AvailableAt,
			FrozenAt:             commission.FrozenAt,
			CreatedAt:            commission.CreatedAt,
		})
	}
	return items, total, nil
}

func (s *ReferralService) listWithdrawals(params ReferralListParams, affiliateUserId int, global bool) ([]ReferralWithdrawalView, int64, error) {
	page, pageSize := normalizePage(params.Page, params.PageSize)
	query := model.DB.Model(&model.ReferralWithdrawal{})
	if !global {
		query = query.Where("user_id = ?", affiliateUserId)
	}
	if status := strings.TrimSpace(params.Status); status != "" {
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.ReferralWithdrawal
	if err := query.Order("submitted_at desc, id desc").Limit(pageSize).Offset((page - 1) * pageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	items := make([]ReferralWithdrawalView, 0, len(rows))
	for _, row := range rows {
		view, err := s.buildWithdrawalView(&row, global)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *view)
	}
	return items, total, nil
}

func (s *ReferralService) buildWithdrawalView(row *model.ReferralWithdrawal, adminView bool) (*ReferralWithdrawalView, error) {
	user := &model.User{}
	_ = model.DB.Select("username,email").Where("id = ?", row.UserId).First(user).Error
	maskedAccountNo := maskAccountNo(row.AccountNo)
	view := &ReferralWithdrawalView{
		Id:                 row.Id,
		AffiliateId:        row.AffiliateId,
		UserId:             row.UserId,
		Username:           user.Username,
		Email:              user.Email,
		SettlementCurrency: accountSettlementCurrency(row.SettlementCurrency),
		Amount:             row.Amount,
		FeeAmount:          row.FeeAmount,
		NetAmount:          row.NetAmount,
		AccountType:        row.AccountType,
		AccountName:        row.AccountName,
		AccountNo:          maskedAccountNo,
		AccountNoMasked:    maskedAccountNo,
		AccountNetwork:     row.AccountNetwork,
		QRImageURL:         row.QRImageURL,
		ApplicantNote:      row.ApplicantNote,
		AdminNote:          row.AdminNote,
		PaymentProofURL:    row.PaymentProofURL,
		PaymentTxnNo:       row.PaymentTxnNo,
		Status:             row.Status,
		RejectReason:       row.RejectReason,
		SubmittedAt:        row.SubmittedAt,
		ApprovedAt:         row.ApprovedAt,
		PayoutDeadlineAt:   row.PayoutDeadlineAt,
		PaidAt:             row.PaidAt,
		RejectedAt:         row.RejectedAt,
		CanceledAt:         row.CanceledAt,
	}
	if adminView {
		view.AccountNo = row.AccountNo
	}
	if row.QRImageURL != "" {
		view.QRImageURL = s.SignAssetURL(row.QRImageURL)
	}
	if row.PaymentProofURL != "" {
		view.PaymentProofURL = s.SignAssetURL(row.PaymentProofURL)
	}
	return view, nil
}

func (s *ReferralService) allocateWithdrawalItemsTx(tx *gorm.DB, affiliateId int, withdrawalId int, amount float64) error {
	remaining := roundMoney(amount)
	var commissions []model.ReferralCommission
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("affiliate_id = ? AND status IN ?", affiliateId, []string{model.ReferralCommissionStatusAvailable, model.ReferralCommissionStatusFrozen, model.ReferralCommissionStatusPaid}).Order("available_at asc, id asc").Find(&commissions).Error; err != nil {
		return err
	}
	for _, commission := range commissions {
		if remaining <= 0 {
			break
		}
		frozenAllocated, withdrawnAllocated, err := s.getCommissionAllocatedAmountsTx(tx, commission.Id)
		if err != nil {
			return err
		}
		available := roundMoney(commission.CommissionAmount - frozenAllocated - withdrawnAllocated)
		if available <= 0 {
			continue
		}
		useAmount := available
		if useAmount > remaining {
			useAmount = remaining
		}
		item := &model.ReferralWithdrawalItem{
			WithdrawalId:    withdrawalId,
			CommissionId:    commission.Id,
			AllocatedAmount: roundMoney(useAmount),
			Status:          model.ReferralWithdrawalItemStatusFrozen,
		}
		if err := tx.Create(item).Error; err != nil {
			return err
		}
		if err := s.syncCommissionStatusTx(tx, commission.Id); err != nil {
			return err
		}
		remaining = roundMoney(remaining - useAmount)
	}
	if remaining > 0.00000001 {
		// Admin adjustments can legitimately increase available_amount without creating
		// a backing commission row. Preserve withdrawal/account consistency by storing
		// the unmatched portion as an account-level allocation placeholder.
		item := &model.ReferralWithdrawalItem{
			WithdrawalId:    withdrawalId,
			CommissionId:    0,
			AllocatedAmount: remaining,
			Status:          model.ReferralWithdrawalItemStatusFrozen,
		}
		if err := tx.Create(item).Error; err != nil {
			return err
		}
		remaining = 0
	}
	if remaining > 0.00000001 {
		return errors.New("insufficient available commission allocations")
	}
	return nil
}

func (s *ReferralService) releaseWithdrawalItemsTx(tx *gorm.DB, withdrawalId int) error {
	var items []model.ReferralWithdrawalItem
	if err := tx.Where("withdrawal_id = ? AND status = ?", withdrawalId, model.ReferralWithdrawalItemStatusFrozen).Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		item.Status = model.ReferralWithdrawalItemStatusReleased
		item.AllocatedAmount = 0
		if err := tx.Save(&item).Error; err != nil {
			return err
		}
		if item.CommissionId <= 0 {
			continue
		}
		if err := s.syncCommissionStatusTx(tx, item.CommissionId); err != nil {
			return err
		}
	}
	return nil
}

func (s *ReferralService) markWithdrawalItemsPaidTx(tx *gorm.DB, withdrawalId int) error {
	var items []model.ReferralWithdrawalItem
	if err := tx.Where("withdrawal_id = ? AND status = ?", withdrawalId, model.ReferralWithdrawalItemStatusFrozen).Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		item.Status = model.ReferralWithdrawalItemStatusWithdrawn
		if err := tx.Save(&item).Error; err != nil {
			return err
		}
		if item.CommissionId <= 0 {
			continue
		}
		if err := s.syncCommissionStatusTx(tx, item.CommissionId); err != nil {
			return err
		}
	}
	return nil
}

func (s *ReferralService) getCommissionAllocatedAmountsTx(tx *gorm.DB, commissionId int) (float64, float64, error) {
	var items []model.ReferralWithdrawalItem
	if err := tx.Where("commission_id = ?", commissionId).Find(&items).Error; err != nil {
		return 0, 0, err
	}
	frozenAmount := 0.0
	withdrawnAmount := 0.0
	for _, item := range items {
		switch item.Status {
		case model.ReferralWithdrawalItemStatusFrozen:
			frozenAmount = roundMoney(frozenAmount + item.AllocatedAmount)
		case model.ReferralWithdrawalItemStatusWithdrawn:
			withdrawnAmount = roundMoney(withdrawnAmount + item.AllocatedAmount)
		}
	}
	return frozenAmount, withdrawnAmount, nil
}

func (s *ReferralService) deriveCommissionStatus(commissionId int, commissionAmount float64) (string, error) {
	return s.deriveCommissionStatusTx(model.DB, commissionId, commissionAmount)
}

func (s *ReferralService) deriveCommissionStatusTx(tx *gorm.DB, commissionId int, commissionAmount float64) (string, error) {
	frozenAmount, withdrawnAmount, err := s.getCommissionAllocatedAmountsTx(tx, commissionId)
	if err != nil {
		return "", err
	}
	if frozenAmount > 0 {
		return model.ReferralCommissionStatusFrozen, nil
	}
	if withdrawnAmount+0.00000001 >= commissionAmount {
		return model.ReferralCommissionStatusPaid, nil
	}
	return model.ReferralCommissionStatusAvailable, nil
}

func (s *ReferralService) syncCommissionStatusTx(tx *gorm.DB, commissionId int) error {
	commission := &model.ReferralCommission{}
	if err := tx.Where("id = ?", commissionId).First(commission).Error; err != nil {
		return err
	}
	if commission.Status == model.ReferralCommissionStatusPending {
		return nil
	}
	status, err := s.deriveCommissionStatusTx(tx, commissionId, commission.CommissionAmount)
	if err != nil {
		return err
	}
	commission.Status = status
	if status == model.ReferralCommissionStatusFrozen && commission.FrozenAt == 0 {
		commission.FrozenAt = time.Now().Unix()
	}
	if status != model.ReferralCommissionStatusFrozen {
		commission.FrozenAt = 0
	}
	return tx.Save(commission).Error
}

func (s *ReferralService) updateAffiliateStatus(userId, adminUserId int, status string, acquisitionEnabled, settlementEnabled, withdrawalEnabled bool, reason, action, ip, userAgent string) (*ReferralAffiliateView, error) {
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		item := &model.ReferralAffiliate{}
		if err := tx.Where("user_id = ?", userId).First(item).Error; err != nil {
			return err
		}
		oldValue := map[string]any{
			"status":              item.Status,
			"acquisition_enabled": item.AcquisitionEnabled,
			"settlement_enabled":  item.SettlementEnabled,
			"withdrawal_enabled":  item.WithdrawalEnabled,
		}
		item.Status = status
		item.AcquisitionEnabled = acquisitionEnabled
		item.SettlementEnabled = settlementEnabled
		item.WithdrawalEnabled = withdrawalEnabled
		item.RiskReason = strings.TrimSpace(reason)
		if status == model.ReferralAffiliateStatusDisabled {
			item.DisabledAt = time.Now().Unix()
			item.DisabledBy = adminUserId
		}
		if status == model.ReferralAffiliateStatusApproved {
			item.ApprovedAt = maxInt64(item.ApprovedAt, time.Now().Unix())
			item.ApprovedBy = adminUserId
			item.DisabledAt = 0
			item.DisabledBy = 0
		}
		if err := tx.Save(item).Error; err != nil {
			return err
		}
		return s.recordAdminAuditTx(tx, action, userId, item.Id, adminUserId, reason, ip, userAgent, oldValue, map[string]any{
			"status":              item.Status,
			"acquisition_enabled": item.AcquisitionEnabled,
			"settlement_enabled":  item.SettlementEnabled,
			"withdrawal_enabled":  item.WithdrawalEnabled,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.GetAffiliateView(userId)
}

func (s *ReferralService) updateAffiliateFlags(userId, adminUserId int, settlementEnabled *bool, withdrawalEnabled *bool, reason, action, ip, userAgent string) (*ReferralAffiliateView, error) {
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		item := &model.ReferralAffiliate{}
		if err := tx.Where("user_id = ?", userId).First(item).Error; err != nil {
			return err
		}
		oldValue := map[string]any{
			"settlement_enabled": item.SettlementEnabled,
			"withdrawal_enabled": item.WithdrawalEnabled,
		}
		if settlementEnabled != nil {
			item.SettlementEnabled = *settlementEnabled
		}
		if withdrawalEnabled != nil {
			item.WithdrawalEnabled = *withdrawalEnabled
		}
		if err := tx.Save(item).Error; err != nil {
			return err
		}
		return s.recordAdminAuditTx(tx, action, userId, item.Id, adminUserId, reason, ip, userAgent, oldValue, map[string]any{
			"settlement_enabled": item.SettlementEnabled,
			"withdrawal_enabled": item.WithdrawalEnabled,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.GetAffiliateView(userId)
}

func (s *ReferralService) recordAdminAudit(action string, targetUserId int, affiliateId int, adminUserId int, reason, ip, userAgent string, oldValue, newValue map[string]any) error {
	return s.recordAdminAuditTx(nil, action, targetUserId, affiliateId, adminUserId, reason, ip, userAgent, oldValue, newValue)
}

func (s *ReferralService) recordAdminAuditTx(tx *gorm.DB, action string, targetUserId int, affiliateId int, adminUserId int, reason, ip, userAgent string, oldValue, newValue map[string]any) error {
	log := &model.ReferralAdminAuditLog{
		Action:       strings.TrimSpace(action),
		TargetUserId: targetUserId,
		AffiliateId:  affiliateId,
		AdminUserId:  adminUserId,
		Reason:       strings.TrimSpace(reason),
		Ip:           strings.TrimSpace(ip),
		UserAgent:    strings.TrimSpace(userAgent),
		OldValue:     common.MapToJsonStr(oldValue),
		NewValue:     common.MapToJsonStr(newValue),
	}
	db := model.DB
	if tx != nil {
		db = tx
	}
	return db.Create(log).Error
}

func maskAccountNo(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) <= 8 {
		return trimmed
	}
	return string(runes[:4]) + strings.Repeat("*", len(runes)-8) + string(runes[len(runes)-4:])
}

func roundMoney(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return value
	}
	out, _ := moneyDecimal(value).Float64()
	return out
}

func moneyDecimal(value float64) decimal.Decimal {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return decimal.Zero
	}
	return decimal.NewFromFloat(value).Round(8)
}

func calculateCommissionAmount(baseAmount, rate float64) float64 {
	if baseAmount <= 0 || rate <= 0 || math.IsNaN(baseAmount) || math.IsNaN(rate) || math.IsInf(baseAmount, 0) || math.IsInf(rate, 0) {
		return 0
	}
	amount := moneyDecimal(baseAmount).Mul(moneyDecimal(rate)).Div(decimal.NewFromInt(100)).Round(8)
	out, _ := amount.Float64()
	return out
}

func normalizePage(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = common.ItemsPerPage
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func parseInt64(value string) (int64, error) {
	var out int64
	_, err := fmt.Sscan(strings.TrimSpace(value), &out)
	return out, err
}

func maxInt(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func maxInt64(value int64, other int64) int64 {
	if value > other {
		return value
	}
	return other
}
