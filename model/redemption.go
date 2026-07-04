package model

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/shopspring/decimal"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Redemption struct {
	Id                       int            `json:"id"`
	UserId                   int            `json:"user_id"`
	Key                      string         `json:"key" gorm:"type:char(32);uniqueIndex"`
	Status                   int            `json:"status" gorm:"default:1"`
	Name                     string         `json:"name" gorm:"index"`
	Quota                    int            `json:"quota" gorm:"default:100"`
	QuotaPerUnitSnapshot     float64        `json:"quota_per_unit_snapshot" gorm:"type:decimal(20,8);default:0"`
	CreatedTime              int64          `json:"created_time" gorm:"bigint"`
	RedeemedTime             int64          `json:"redeemed_time" gorm:"bigint"`
	Count                    int            `json:"count" gorm:"-:all"` // only for api request
	UsedUserId               int            `json:"used_user_id"`
	UsedUsername             string         `json:"used_username" gorm:"->;-:migration;column:used_username"`
	ReferralAffiliateId      int            `json:"referral_affiliate_id" gorm:"index"`
	ReferralRate             float64        `json:"referral_rate" gorm:"type:decimal(10,4);default:0"`
	ReferralBaseAmount       float64        `json:"referral_base_amount" gorm:"type:decimal(20,8);default:0"`
	ReferralBaseCurrency     string         `json:"referral_base_currency" gorm:"type:varchar(16);default:''"`
	ReferralCommissionStatus string         `json:"referral_commission_status" gorm:"type:varchar(32);default:'';index"`
	ReferralCommissionError  string         `json:"referral_commission_error" gorm:"type:text"`
	ReferralCommissionAt     int64          `json:"referral_commission_at" gorm:"default:0"`
	DeletedAt                gorm.DeletedAt `gorm:"index"`
	ExpiredTime              int64          `json:"expired_time" gorm:"bigint"` // 过期时间，0 表示不过期
}

type RedeemResult struct {
	RedemptionId int `json:"redemption_id"`
	Quota        int `json:"quota"`
}

func GetAllRedemptions(startIdx int, num int) (redemptions []*Redemption, total int64, err error) {
	// 开始事务
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 获取总数
	err = tx.Model(&Redemption{}).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// 获取分页数据
	err = tx.Model(&Redemption{}).
		Select("redemptions.*, users.username AS used_username").
		Joins("LEFT JOIN users ON users.id = redemptions.used_user_id").
		Order("redemptions.id desc").
		Limit(num).
		Offset(startIdx).
		Find(&redemptions).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// 提交事务
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return redemptions, total, nil
}

func SearchRedemptions(keyword string, startIdx int, num int) (redemptions []*Redemption, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Build query based on keyword type
	query := tx.Model(&Redemption{}).
		Joins("LEFT JOIN users ON users.id = redemptions.used_user_id")

	// Only try to convert to ID if the string represents a valid integer
	if id, err := strconv.Atoi(keyword); err == nil {
		query = query.Where("redemptions.id = ? OR redemptions.name LIKE ? OR users.username LIKE ?", id, keyword+"%", keyword+"%")
	} else {
		query = query.Where("redemptions.name LIKE ? OR users.username LIKE ?", keyword+"%", keyword+"%")
	}

	// Get total count
	err = query.Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Get paginated data
	err = query.
		Select("redemptions.*, users.username AS used_username").
		Order("redemptions.id desc").
		Limit(num).
		Offset(startIdx).
		Find(&redemptions).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return redemptions, total, nil
}

func GetRedemptionById(id int) (*Redemption, error) {
	if id == 0 {
		return nil, errors.New("id 为空！")
	}
	redemption := Redemption{Id: id}
	var err error = nil
	err = DB.First(&redemption, "id = ?", id).Error
	return &redemption, err
}

func Redeem(key string, userId int) (*RedeemResult, error) {
	if key == "" {
		return nil, errors.New("未提供兑换码")
	}
	if userId == 0 {
		return nil, errors.New("无效的 user id")
	}
	redemption := &Redemption{}

	keyCol := "`key`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		keyCol = `"key"`
	}
	common.RandomSleep()
	err := DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(keyCol+" = ?", key).First(redemption).Error
		if err != nil {
			return errors.New("无效的兑换码")
		}
		if redemption.Status != common.RedemptionCodeStatusEnabled {
			return errors.New("该兑换码已被使用")
		}
		if redemption.UsedUserId > 0 || redemption.RedeemedTime > 0 {
			return errors.New("该兑换码已被使用")
		}
		if redemption.ExpiredTime != 0 && redemption.ExpiredTime < common.GetTimestamp() {
			return errors.New("该兑换码已过期")
		}
		if redemption.Quota <= 0 {
			return errors.New("兑换码额度必须大于0")
		}
		err = tx.Model(&User{}).Where("id = ?", userId).Update("quota", gorm.Expr("quota + ?", redemption.Quota)).Error
		if err != nil {
			return err
		}
		redemption.RedeemedTime = common.GetTimestamp()
		redemption.Status = common.RedemptionCodeStatusUsed
		redemption.UsedUserId = userId
		if redemption.QuotaPerUnitSnapshot <= 0 {
			redemption.QuotaPerUnitSnapshot = common.QuotaPerUnit
		}
		snapshotRedemptionReferralStateTx(tx, redemption)
		claim := tx.Model(&Redemption{}).
			Where("id = ? AND status = ? AND used_user_id = 0 AND redeemed_time = 0", redemption.Id, common.RedemptionCodeStatusEnabled).
			Updates(map[string]interface{}{
				"redeemed_time":              redemption.RedeemedTime,
				"status":                     redemption.Status,
				"used_user_id":               redemption.UsedUserId,
				"referral_affiliate_id":      redemption.ReferralAffiliateId,
				"referral_rate":              redemption.ReferralRate,
				"referral_base_amount":       redemption.ReferralBaseAmount,
				"referral_base_currency":     redemption.ReferralBaseCurrency,
				"referral_commission_status": redemption.ReferralCommissionStatus,
				"referral_commission_error":  redemption.ReferralCommissionError,
				"referral_commission_at":     redemption.ReferralCommissionAt,
				"quota_per_unit_snapshot":    redemption.QuotaPerUnitSnapshot,
			})
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected != 1 {
			return errors.New("该兑换码已被使用")
		}
		job := &ReferralCommissionJob{
			SourceType:    "redemption",
			SourceTradeNo: redemptionCommissionTradeNo(redemption.Id),
			AffiliateId:   redemption.ReferralAffiliateId,
			Status:        redemption.ReferralCommissionStatus,
		}
		now := common.GetTimestamp()
		if job.Status == ReferralCommissionJobStatusFailed {
			job.LastError = redemption.ReferralCommissionError
			job.FailedAt = now
		}
		if job.Status == ReferralCommissionJobStatusSkipped {
			job.LastError = strings.TrimSpace(redemption.ReferralCommissionError)
			job.SucceededAt = now
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(job).Error
	})
	if err != nil {
		common.SysError("redemption failed: " + err.Error())
		return nil, ErrRedeemFailed
	}
	RecordLog(userId, LogTypeTopup, fmt.Sprintf("通过兑换码充值 %s，兑换码ID %d", logger.LogQuota(redemption.Quota), redemption.Id))
	return &RedeemResult{
		RedemptionId: redemption.Id,
		Quota:        redemption.Quota,
	}, nil
}

func (redemption *Redemption) Insert() error {
	var err error
	if redemption.QuotaPerUnitSnapshot <= 0 {
		redemption.QuotaPerUnitSnapshot = common.QuotaPerUnit
	}
	err = DB.Create(redemption).Error
	return err
}

func (redemption *Redemption) SelectUpdate() error {
	// This can update zero values
	return DB.Model(redemption).Select("redeemed_time", "status").Updates(redemption).Error
}

// Update Make sure your token's fields is completed, because this will update non-zero values
func (redemption *Redemption) Update() error {
	var err error
	err = DB.Model(redemption).Select("name", "status", "quota", "quota_per_unit_snapshot", "redeemed_time", "expired_time").Updates(redemption).Error
	return err
}

func (redemption *Redemption) Delete() error {
	var err error
	if redemptionReferralCommissionDeletionBlocked(redemption) {
		return errors.New("redemption referral commission is unresolved")
	}
	err = DB.Delete(redemption).Error
	return err
}

func DeleteRedemptionById(id int) (err error) {
	if id == 0 {
		return errors.New("id 为空！")
	}
	redemption := Redemption{Id: id}
	err = DB.Where(redemption).First(&redemption).Error
	if err != nil {
		return err
	}
	return redemption.Delete()
}

func DeleteInvalidRedemptions() (int64, error) {
	now := common.GetTimestamp()
	var redemptions []Redemption
	if err := DB.Where(
		"(status = ? AND (used_user_id <= 0 OR (COALESCE(referral_commission_status, '') <> '' AND COALESCE(referral_commission_status, '') NOT IN ?))) OR status = ? OR (status = ? AND expired_time != 0 AND expired_time < ?)",
		common.RedemptionCodeStatusUsed,
		[]string{ReferralCommissionJobStatusPending, ReferralCommissionJobStatusProcessing, ReferralCommissionJobStatusFailed},
		common.RedemptionCodeStatusDisabled,
		common.RedemptionCodeStatusEnabled,
		now,
	).Find(&redemptions).Error; err != nil {
		return 0, err
	}
	var rows int64
	for i := range redemptions {
		if redemptionReferralCommissionDeletionBlocked(&redemptions[i]) {
			continue
		}
		result := DB.Delete(&redemptions[i])
		if result.Error != nil {
			return rows, result.Error
		}
		rows += result.RowsAffected
	}
	return rows, nil
}

func redemptionCommissionTradeNo(redemptionId int) string {
	return fmt.Sprintf("redemption:%d", redemptionId)
}

func snapshotRedemptionReferralStateTx(tx *gorm.DB, redemption *Redemption) {
	if redemption == nil {
		return
	}
	redemption.ReferralAffiliateId = 0
	redemption.ReferralRate = 0
	redemption.ReferralBaseAmount = 0
	redemption.ReferralBaseCurrency = ""
	redemption.ReferralCommissionStatus = ReferralCommissionJobStatusPending
	redemption.ReferralCommissionError = ""
	redemption.ReferralCommissionAt = 0
	setTerminal := func(status string, message string) {
		redemption.ReferralCommissionStatus = status
		redemption.ReferralCommissionError = message
		redemption.ReferralCommissionAt = common.GetTimestamp()
	}

	baseAmount, baseErr := redemptionReferralBaseAmountCNY(redemption)
	if baseErr == nil {
		redemption.ReferralBaseAmount = baseAmount
		redemption.ReferralBaseCurrency = "CNY"
	}
	finishWithBaseError := func() bool {
		if baseErr == nil {
			return false
		}
		setTerminal(ReferralCommissionJobStatusFailed, baseErr.Error())
		return true
	}

	if !common.ReferralEnabled {
		if finishWithBaseError() {
			return
		}
		setTerminal(ReferralCommissionJobStatusSkipped, "referral_disabled")
		return
	}

	binding := &ReferralBinding{}
	if err := tx.Where("invitee_user_id = ?", redemption.UsedUserId).First(binding).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if finishWithBaseError() {
				return
			}
			setTerminal(ReferralCommissionJobStatusSkipped, "no_binding")
			return
		}
		setTerminal(ReferralCommissionJobStatusFailed, err.Error())
		return
	}

	affiliate := &ReferralAffiliate{}
	if err := tx.Where("id = ?", binding.AffiliateId).First(affiliate).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if finishWithBaseError() {
				return
			}
			setTerminal(ReferralCommissionJobStatusSkipped, "affiliate_not_found")
			return
		}
		setTerminal(ReferralCommissionJobStatusFailed, err.Error())
		return
	}
	if affiliate.Status != ReferralAffiliateStatusApproved {
		if finishWithBaseError() {
			return
		}
		setTerminal(ReferralCommissionJobStatusSkipped, "affiliate_not_approved")
		return
	}
	if !affiliate.AcquisitionEnabled {
		if finishWithBaseError() {
			return
		}
		setTerminal(ReferralCommissionJobStatusSkipped, "affiliate_acquisition_disabled")
		return
	}
	if !affiliate.SettlementEnabled {
		if finishWithBaseError() {
			return
		}
		setTerminal(ReferralCommissionJobStatusSkipped, "affiliate_settlement_disabled")
		return
	}
	rate := redemptionReferralRate(affiliate.RateOverride)
	if rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		if finishWithBaseError() {
			return
		}
		setTerminal(ReferralCommissionJobStatusSkipped, "invalid_rate")
		return
	}

	redemption.ReferralAffiliateId = affiliate.Id
	redemption.ReferralRate = rate
	if finishWithBaseError() {
		return
	}
	redemption.ReferralCommissionStatus = ReferralCommissionJobStatusPending
	redemption.ReferralCommissionError = ""
}

func redemptionReferralBaseAmountCNY(redemption *Redemption) (float64, error) {
	if redemption == nil || redemption.Quota <= 0 {
		return 0, errors.New("redemption quota must be positive")
	}
	quotaPerUnit := redemption.QuotaPerUnitSnapshot
	if quotaPerUnit <= 0 {
		quotaPerUnit = common.QuotaPerUnit
	}
	if quotaPerUnit <= 0 || math.IsNaN(quotaPerUnit) || math.IsInf(quotaPerUnit, 0) {
		return 0, errors.New("quota per unit must be a positive finite number")
	}
	rate := common.ReferralRedemptionUSDToCNYRate
	if rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return 0, errors.New("redemption_usd_to_cny_rate must be a positive finite number")
	}
	amount := decimal.NewFromInt(int64(redemption.Quota)).
		Div(decimal.NewFromFloat(quotaPerUnit)).
		Mul(decimal.NewFromFloat(rate)).
		Round(8)
	out, _ := amount.Float64()
	return out, nil
}

func redemptionReferralRate(rateOverride *float64) float64 {
	if rateOverride != nil {
		return redemptionReferralRound(*rateOverride)
	}
	return redemptionReferralRound(common.ReferralDefaultRate)
}

func redemptionReferralRound(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return value
	}
	out, _ := decimal.NewFromFloat(value).Round(8).Float64()
	return out
}

func redemptionReferralCommissionDeletionBlocked(redemption *Redemption) bool {
	if redemption == nil || redemption.Id <= 0 {
		return false
	}
	if redemptionRedeemedForReferral(redemption) && redemptionReferralCommissionUnresolved(redemption.ReferralCommissionStatus) {
		return true
	}
	if redemptionRedeemedForReferral(redemption) && strings.TrimSpace(redemption.ReferralCommissionStatus) == ReferralCommissionJobStatusSucceeded {
		complete, err := RedemptionReferralSucceededCommissionCompleteTx(DB, redemption)
		if err != nil || !complete {
			return true
		}
	}
	if redemptionRedeemedForReferral(redemption) && strings.TrimSpace(redemption.ReferralCommissionStatus) == ReferralCommissionJobStatusSkipped {
		complete, err := RedemptionReferralSkippedCommissionCompleteTx(DB, redemption)
		if err != nil || !complete {
			return true
		}
	}
	var count int64
	if err := DB.Model(&ReferralCommissionJob{}).
		Where("source_type = ? AND source_trade_no = ? AND status IN ?",
			"redemption",
			redemptionCommissionTradeNo(redemption.Id),
			[]string{ReferralCommissionJobStatusPending, ReferralCommissionJobStatusProcessing, ReferralCommissionJobStatusFailed},
		).Count(&count).Error; err != nil {
		return true
	}
	return count > 0
}

func redemptionReferralCommissionUnresolved(status string) bool {
	switch strings.TrimSpace(status) {
	case "", ReferralCommissionJobStatusPending, ReferralCommissionJobStatusProcessing, ReferralCommissionJobStatusFailed:
		return true
	default:
		return false
	}
}

func RedemptionReferralSucceededCommissionCompleteTx(tx *gorm.DB, redemption *Redemption) (bool, error) {
	commission, complete, err := RedemptionReferralSucceededCommissionRecordCompleteTx(tx, redemption)
	if err != nil || !complete {
		return complete, err
	}
	if redemption.ReferralAffiliateId > 0 && commission.AffiliateId != redemption.ReferralAffiliateId {
		return false, nil
	}
	return true, nil
}

func RedemptionReferralSucceededCommissionRecordCompleteTx(tx *gorm.DB, redemption *Redemption) (*ReferralCommission, bool, error) {
	if redemption == nil || redemption.Id <= 0 {
		return nil, false, nil
	}
	if tx == nil {
		tx = DB
	}
	tradeNo := redemptionCommissionTradeNo(redemption.Id)
	commission := &ReferralCommission{}
	if err := tx.Where("source_type = ? AND source_trade_no = ?", "redemption", tradeNo).First(commission).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if commission.Id <= 0 || commission.AffiliateId <= 0 || commission.AffiliateUserId <= 0 || commission.CommissionAmount <= 0 {
		return commission, false, nil
	}
	if commission.SourceOrderId > 0 && commission.SourceOrderId != redemption.Id {
		return commission, false, nil
	}
	if redemption.UsedUserId > 0 && commission.InviteeUserId != redemption.UsedUserId {
		return commission, false, nil
	}
	ledgerComplete, err := ReferralCommissionAccrueLedgerCompleteTx(tx, commission, "redemption", tradeNo)
	if err != nil {
		return commission, false, err
	}
	if !ledgerComplete {
		return commission, false, nil
	}
	accountComplete, err := ReferralAccountLedgerBalanceCompleteTx(tx, commission.AffiliateId, commission.AffiliateUserId)
	if err != nil {
		return commission, false, err
	}
	return commission, accountComplete, nil
}

func RedemptionReferralSkippedCommissionCompleteTx(tx *gorm.DB, redemption *Redemption) (bool, error) {
	if redemption == nil || redemption.Id <= 0 {
		return false, nil
	}
	if tx == nil {
		tx = DB
	}
	commission, succeededComplete, err := RedemptionReferralSucceededCommissionRecordCompleteTx(tx, redemption)
	if err != nil {
		return false, err
	}
	if succeededComplete || commission != nil {
		return false, nil
	}
	job := &ReferralCommissionJob{}
	if err := tx.Where("source_type = ? AND source_trade_no = ?", "redemption", redemptionCommissionTradeNo(redemption.Id)).First(job).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if strings.TrimSpace(job.Status) != ReferralCommissionJobStatusSkipped || job.SucceededAt <= 0 {
		return false, nil
	}
	if reason := strings.TrimSpace(redemption.ReferralCommissionError); reason != "" && strings.TrimSpace(job.LastError) != reason {
		return false, nil
	}
	return true, nil
}

func ReferralCommissionAccrueLedgerCompleteTx(tx *gorm.DB, commission *ReferralCommission, sourceType string, tradeNo string) (bool, error) {
	if commission == nil || commission.Id <= 0 || commission.CommissionAmount <= 0 {
		return false, nil
	}
	if tx == nil {
		tx = DB
	}
	sourceType = strings.ToLower(strings.TrimSpace(sourceType))
	tradeNo = strings.TrimSpace(tradeNo)
	if sourceType == "" || tradeNo == "" {
		return false, nil
	}
	var totals struct {
		Count        int64
		DeltaPending float64
	}
	if err := tx.Model(&ReferralCommissionLedger{}).
		Select("COUNT(*) AS count, COALESCE(SUM(delta_pending), 0) AS delta_pending").
		Where(
			"commission_id = ? AND type = ? AND ref_type = ? AND ref_id = ? AND external_ref_id = ?",
			commission.Id,
			"commission_accrue",
			sourceType,
			tradeNo,
			fmt.Sprintf("accrue:%s:%s", sourceType, tradeNo),
		).
		Scan(&totals).Error; err != nil {
		return false, err
	}
	return totals.Count > 0 && referralMoneyEqual(totals.DeltaPending, commission.CommissionAmount), nil
}

func ReferralAccountLedgerBalanceCompleteTx(tx *gorm.DB, affiliateId int, userId int) (bool, error) {
	if affiliateId <= 0 || userId <= 0 {
		return false, nil
	}
	if tx == nil {
		tx = DB
	}
	account := &ReferralCommissionAccount{}
	if err := tx.Where("affiliate_id = ? AND user_id = ?", affiliateId, userId).First(account).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	var totals struct {
		PendingAmount   float64
		AvailableAmount float64
		FrozenAmount    float64
		WithdrawnAmount float64
	}
	if err := tx.Model(&ReferralCommissionLedger{}).
		Select("COALESCE(SUM(delta_pending), 0) AS pending_amount, COALESCE(SUM(delta_available), 0) AS available_amount, COALESCE(SUM(delta_frozen), 0) AS frozen_amount, COALESCE(SUM(delta_withdrawn), 0) AS withdrawn_amount").
		Where("affiliate_id = ? AND user_id = ?", affiliateId, userId).
		Scan(&totals).Error; err != nil {
		return false, err
	}
	return referralMoneyEqual(account.PendingAmount, totals.PendingAmount) &&
		referralMoneyEqual(account.AvailableAmount, totals.AvailableAmount) &&
		referralMoneyEqual(account.FrozenAmount, totals.FrozenAmount) &&
		referralMoneyEqual(account.WithdrawnAmount, totals.WithdrawnAmount), nil
}

func referralMoneyEqual(left, right float64) bool {
	if math.IsNaN(left) || math.IsNaN(right) || math.IsInf(left, 0) || math.IsInf(right, 0) {
		return false
	}
	return math.Abs(redemptionReferralRound(left)-redemptionReferralRound(right)) <= 0.00000001
}

func redemptionRedeemedForReferral(redemption *Redemption) bool {
	if redemption == nil {
		return false
	}
	return redemption.UsedUserId > 0 && (redemption.RedeemedTime > 0 || redemption.Status == common.RedemptionCodeStatusUsed)
}
