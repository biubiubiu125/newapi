package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type TopUp struct {
	Id                         int     `json:"id"`
	UserId                     int     `json:"user_id" gorm:"index"`
	Amount                     int64   `json:"amount"`
	Money                      float64 `json:"money"`
	PaidAmount                 float64 `json:"paid_amount" gorm:"type:decimal(20,8);default:0"`
	PaidCurrency               string  `json:"paid_currency" gorm:"type:varchar(16);default:''"`
	TradeNo                    string  `json:"trade_no" gorm:"unique;type:varchar(255);index"`
	PaymentMethod              string  `json:"payment_method" gorm:"type:varchar(50)"`
	PaymentProvider            string  `json:"payment_provider" gorm:"type:varchar(50);default:''"`
	ProviderPayload            string  `json:"provider_payload" gorm:"type:text"`
	OrderSnapshotVersion       int     `json:"order_snapshot_version" gorm:"type:int;default:0"`
	RequestAmountSnapshot      int64   `json:"request_amount_snapshot" gorm:"type:bigint;default:0"`
	CreditQuotaSnapshot        int64   `json:"credit_quota_snapshot" gorm:"type:bigint;default:0"`
	QuotaPerUnitSnapshot       float64 `json:"quota_per_unit_snapshot" gorm:"type:decimal(20,8);default:0"`
	PriceSnapshot              float64 `json:"price_snapshot" gorm:"type:decimal(20,8);default:0"`
	USDExchangeRateSnapshot    float64 `json:"usd_exchange_rate_snapshot" gorm:"type:decimal(20,8);default:0"`
	CustomExchangeRateSnapshot float64 `json:"custom_exchange_rate_snapshot" gorm:"type:decimal(20,8);default:0"`
	QuotaDisplayTypeSnapshot   string  `json:"quota_display_type_snapshot" gorm:"type:varchar(32);default:''"`
	DisplayCurrencySnapshot    string  `json:"display_currency_snapshot" gorm:"type:varchar(16);default:''"`
	TopupGroupRatioSnapshot    float64 `json:"topup_group_ratio_snapshot" gorm:"type:decimal(20,8);default:0"`
	AmountDiscountSnapshot     float64 `json:"amount_discount_snapshot" gorm:"type:decimal(20,8);default:0"`
	CreateTime                 int64   `json:"create_time"`
	CompleteTime               int64   `json:"complete_time"`
	Status                     string  `json:"status"`
	ReferralAffiliateId        int     `json:"referral_affiliate_id" gorm:"index"`
	ReferralRate               float64 `json:"referral_rate" gorm:"type:decimal(10,4);default:0"`
	ReferralBaseAmount         float64 `json:"referral_base_amount" gorm:"type:decimal(20,8);default:0"`
	ReferralBaseCurrency       string  `json:"referral_base_currency" gorm:"type:varchar(16);default:''"`
	ReferralCommissionStatus   string  `json:"referral_commission_status" gorm:"type:varchar(32);default:'';index"`
	ReferralCommissionError    string  `json:"referral_commission_error" gorm:"type:text"`
	ReferralCommissionAt       int64   `json:"referral_commission_at" gorm:"default:0"`
}

const (
	PaymentMethodStripe       = "stripe"
	PaymentMethodCreem        = "creem"
	PaymentMethodWaffo        = "waffo"
	PaymentMethodWaffoPancake = "waffo_pancake"
	PaymentMethodEpusdtPrefix = "epusdt:"
)

const (
	PaymentProviderEpay         = "epay"
	PaymentProviderStripe       = "stripe"
	PaymentProviderCreem        = "creem"
	PaymentProviderWaffo        = "waffo"
	PaymentProviderWaffoPancake = "waffo_pancake"
	PaymentProviderEpusdt       = "epusdt"
)

var (
	ErrPaymentMethodMismatch   = errors.New("payment method mismatch")
	ErrTopUpNotFound           = errors.New("topup not found")
	ErrTopUpStatusInvalid      = errors.New("topup status invalid")
	ErrPaymentAmountMismatch   = errors.New("payment amount mismatch")
	ErrPaymentCurrencyMismatch = errors.New("payment currency mismatch")
)

type PaymentCallbackValidation struct {
	ExpectedPaymentProvider string
	ActualPaymentMethod     string
	ActualPaymentToken      string
	PaidAmount              float64
	PaidCurrency            string
	RequirePaymentFacts     bool
}

func (topUp *TopUp) Insert() error {
	var err error
	err = DB.Create(topUp).Error
	return err
}

func (topUp *TopUp) Update() error {
	var err error
	err = DB.Save(topUp).Error
	return err
}

func (topUp *TopUp) CreditQuotaAmount() int {
	if topUp == nil {
		return 0
	}
	if topUp.CreditQuotaSnapshot > 0 {
		return int(topUp.CreditQuotaSnapshot)
	}
	quotaPerUnit := topUp.QuotaPerUnitSnapshot
	if quotaPerUnit <= 0 {
		quotaPerUnit = common.QuotaPerUnit
	}
	if quotaPerUnit <= 0 {
		return 0
	}
	if topUp.PaymentProvider == PaymentProviderCreem {
		return int(topUp.Amount)
	}
	if topUp.PaymentProvider == PaymentProviderStripe {
		return int(decimal.NewFromFloat(topUp.Money).Mul(decimal.NewFromFloat(quotaPerUnit)).IntPart())
	}
	return int(decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(quotaPerUnit)).IntPart())
}

func GetTopUpById(id int) *TopUp {
	var topUp *TopUp
	var err error
	err = DB.Where("id = ?", id).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}

func GetTopUpByTradeNo(tradeNo string) *TopUp {
	var topUp *TopUp
	var err error
	err = DB.Where("trade_no = ?", tradeNo).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}

func UpdatePendingTopUpStatus(tradeNo string, expectedPaymentProvider string, targetStatus string) error {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return ErrTopUpNotFound
		}
		if expectedPaymentProvider != "" && topUp.PaymentProvider != expectedPaymentProvider {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}

		topUp.Status = targetStatus
		return tx.Save(topUp).Error
	})
}

func Recharge(referenceId string, customerId string, callerIp string) (err error) {
	if referenceId == "" {
		return errors.New("未提供支付单号")
	}

	var quota float64
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", referenceId).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderStripe {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		err = tx.Save(topUp).Error
		if err != nil {
			return err
		}

		quota = float64(topUp.CreditQuotaAmount())
		if quota <= 0 {
			return errors.New("invalid topup quota")
		}
		err = tx.Model(&User{}).Where("id = ?", topUp.UserId).Updates(map[string]interface{}{"stripe_customer": customerId, "quota": gorm.Expr("quota + ?", quota)}).Error
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}

	RecordTopupLog(topUp.UserId, fmt.Sprintf("使用在线充值成功，充值金额: %v，支付金额：%d", logger.FormatQuota(int(quota)), topUp.Amount), callerIp, topUp.PaymentMethod, PaymentMethodStripe)

	return nil
}

// topUpQueryWindowSeconds 限制充值记录查询的时间窗口（秒）。
const topUpQueryWindowSeconds int64 = 30 * 24 * 60 * 60

// topUpQueryCutoff 返回允许查询的最早 create_time（秒级 Unix 时间戳）。
func topUpQueryCutoff() int64 {
	return common.GetTimestamp() - topUpQueryWindowSeconds
}

func GetUserTopUps(userId int, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	// Start transaction
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	cutoff := topUpQueryCutoff()

	// Get total count within transaction
	err = tx.Model(&TopUp{}).Where("user_id = ? AND create_time >= ?", userId, cutoff).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Get paginated topups within same transaction
	err = tx.Where("user_id = ? AND create_time >= ?", userId, cutoff).Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Commit transaction
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

// GetAllTopUps 获取全平台的充值记录（管理员使用，不限制时间窗口）
func GetAllTopUps(pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	if err = tx.Model(&TopUp{}).Count(&total).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

// searchTopUpCountHardLimit 搜索充值记录时 COUNT 的安全上限，
// 防止对超大表执行无界 COUNT 触发 DoS。
const searchTopUpCountHardLimit = 10000

// SearchUserTopUps 按订单号搜索某用户的充值记录
func SearchUserTopUps(userId int, keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{}).Where("user_id = ? AND create_time >= ?", userId, topUpQueryCutoff())
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to count search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// SearchAllTopUps 按订单号搜索全平台充值记录（管理员使用，不限制时间窗口）
func SearchAllTopUps(keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{})
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to count search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// ManualCompleteTopUp 管理员手动完成订单并给用户充值
func ManualCompleteTopUp(tradeNo string, callerIp string) error {
	if tradeNo == "" {
		return errors.New("未提供订单号")
	}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	var userId int
	var quotaToAdd int
	var payMoney float64
	var paymentMethod string

	err := DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		// 行级锁，避免并发补单
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return errors.New("充值订单不存在")
		}

		// 幂等处理：已成功直接返回
		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("订单状态不是待支付，无法补单")
		}

		// 计算应充值额度：
		// - Stripe 订单：Money 代表经分组倍率换算后的美元数量，直接 * QuotaPerUnit
		// - 其他订单（如易支付）：Amount 为美元数量，* QuotaPerUnit
		quotaToAdd = topUp.CreditQuotaAmount()
		if quotaToAdd <= 0 {
			return errors.New("无效的充值额度")
		}

		// 标记完成
		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		// 增加用户额度（立即写库，保持一致性）
		if err := tx.Model(&User{}).Where("id = ?", topUp.UserId).Update("quota", gorm.Expr("quota + ?", quotaToAdd)).Error; err != nil {
			return err
		}

		userId = topUp.UserId
		payMoney = topUp.Money
		paymentMethod = topUp.PaymentMethod
		return nil
	})

	if err != nil {
		return err
	}

	// 事务外记录日志，避免阻塞
	RecordTopupLog(userId, fmt.Sprintf("管理员补单成功，充值金额: %v，支付金额：%f", logger.FormatQuota(quotaToAdd), payMoney), callerIp, paymentMethod, "admin")
	return nil
}
func RechargeCreem(referenceId string, customerEmail string, customerName string, callerIp string) (err error) {
	if referenceId == "" {
		return errors.New("未提供支付单号")
	}

	var quota int64
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", referenceId).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderCreem {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		err = tx.Save(topUp).Error
		if err != nil {
			return err
		}

		// Creem 直接使用 Amount 作为充值额度（整数）
		quota = topUp.Amount

		// 构建更新字段，优先使用邮箱，如果邮箱为空则使用用户名
		updateFields := map[string]interface{}{
			"quota": gorm.Expr("quota + ?", quota),
		}

		// 如果有客户邮箱，尝试更新用户邮箱（仅当用户邮箱为空时）
		if customerEmail != "" {
			// 先检查用户当前邮箱是否为空
			var user User
			err = tx.Where("id = ?", topUp.UserId).First(&user).Error
			if err != nil {
				return err
			}

			// 如果用户邮箱为空，则更新为支付时使用的邮箱
			if user.Email == "" {
				updateFields["email"] = customerEmail
			}
		}

		err = tx.Model(&User{}).Where("id = ?", topUp.UserId).Updates(updateFields).Error
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("creem topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}

	RecordTopupLog(topUp.UserId, fmt.Sprintf("使用Creem充值成功，充值额度: %v，支付金额：%.2f", quota, topUp.Money), callerIp, topUp.PaymentMethod, PaymentMethodCreem)

	return nil
}

func RechargeWaffo(tradeNo string, callerIp string) (err error) {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}
	var quotaToAdd int
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderWaffo {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			return nil // 幂等：已成功直接返回
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}

		quotaToAdd = topUp.CreditQuotaAmount()
		if quotaToAdd <= 0 {
			return errors.New("无效的充值额度")
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		if err := tx.Model(&User{}).Where("id = ?", topUp.UserId).Update("quota", gorm.Expr("quota + ?", quotaToAdd)).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("waffo topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}

	if quotaToAdd > 0 {
		RecordTopupLog(topUp.UserId, fmt.Sprintf("Waffo充值成功，充值额度: %v，支付金额: %.2f", logger.FormatQuota(quotaToAdd), topUp.Money), callerIp, topUp.PaymentMethod, PaymentMethodWaffo)
	}

	return nil
}

func RechargeWaffoPancake(tradeNo string) (err error) {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}

	var quotaToAdd int
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderWaffoPancake {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}

		quotaToAdd = topUp.CreditQuotaAmount()
		if quotaToAdd <= 0 {
			return errors.New("无效的充值额度")
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		if err := tx.Model(&User{}).Where("id = ?", topUp.UserId).Update("quota", gorm.Expr("quota + ?", quotaToAdd)).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("waffo pancake topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}

	if quotaToAdd > 0 {
		RecordLog(topUp.UserId, LogTypeTopup, fmt.Sprintf("Waffo Pancake充值成功，充值额度: %v，支付金额: %.2f", logger.FormatQuota(quotaToAdd), topUp.Money))
	}

	return nil
}

func RechargeEpayWithValidation(tradeNo string, providerPayload string, validation PaymentCallbackValidation, callerIp string) (err error) {
	if tradeNo == "" {
		return errors.New("missing payment order number")
	}
	if validation.ExpectedPaymentProvider == "" {
		validation.ExpectedPaymentProvider = PaymentProviderEpay
	}

	var quotaToAdd int
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error
		if err != nil {
			return ErrTopUpNotFound
		}

		if topUp.PaymentProvider != validation.ExpectedPaymentProvider {
			return ErrPaymentMethodMismatch
		}
		if validation.ActualPaymentMethod != "" && !callbackPaymentMethodMatches(topUp.PaymentMethod, validation.ActualPaymentMethod, validation.ExpectedPaymentProvider) {
			return ErrPaymentMethodMismatch
		}
		if validation.ActualPaymentToken != "" && !paymentMethodMatchesEpusdtToken(topUp.PaymentMethod, validation.ActualPaymentToken) {
			return ErrPaymentMethodMismatch
		}
		if validation.RequirePaymentFacts && !samePaymentCurrency(topUp.PaidCurrency, validation.PaidCurrency) {
			return ErrPaymentCurrencyMismatch
		}
		if validation.RequirePaymentFacts && !samePaymentAmount(topUp.PaidAmount, validation.PaidAmount) {
			return ErrPaymentAmountMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}

		quotaToAdd = topUp.CreditQuotaAmount()
		if quotaToAdd <= 0 {
			return errors.New("invalid topup quota")
		}

		if providerPayload != "" {
			topUp.ProviderPayload = providerPayload
		}
		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		return tx.Model(&User{}).Where("id = ?", topUp.UserId).Update("quota", gorm.Expr("quota + ?", quotaToAdd)).Error
	})

	if err != nil {
		common.SysError("epay topup failed: " + err.Error())
		return err
	}

	if quotaToAdd > 0 {
		_ = CacheUpdateUserQuota(topUp.UserId)
		RecordTopupLog(topUp.UserId, fmt.Sprintf("Epay topup succeeded, quota: %v, paid amount: %.2f %s", logger.FormatQuota(quotaToAdd), topUp.PaidAmount, topUp.PaidCurrency), callerIp, topUp.PaymentMethod, PaymentProviderEpay)
	}

	return nil
}

func RechargeEpusdt(tradeNo string, providerPayload string, actualPaymentMethod string, callerIp string) (err error) {
	return RechargeEpusdtWithValidation(tradeNo, providerPayload, PaymentCallbackValidation{
		ExpectedPaymentProvider: PaymentProviderEpusdt,
		ActualPaymentMethod:     actualPaymentMethod,
	}, callerIp)
}

func RechargeEpusdtWithValidation(tradeNo string, providerPayload string, validation PaymentCallbackValidation, callerIp string) (err error) {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}

	var quotaToAdd int
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != validation.ExpectedPaymentProvider {
			return ErrPaymentMethodMismatch
		}
		if validation.ActualPaymentMethod != "" && !callbackPaymentMethodMatches(topUp.PaymentMethod, validation.ActualPaymentMethod, validation.ExpectedPaymentProvider) {
			return ErrPaymentMethodMismatch
		}
		if validation.ActualPaymentToken != "" && !paymentMethodMatchesEpusdtToken(topUp.PaymentMethod, validation.ActualPaymentToken) {
			return ErrPaymentMethodMismatch
		}
		if validation.RequirePaymentFacts && !samePaymentCurrency(topUp.PaidCurrency, validation.PaidCurrency) {
			return ErrPaymentCurrencyMismatch
		}
		if validation.RequirePaymentFacts && !samePaymentAmount(topUp.PaidAmount, validation.PaidAmount) {
			return ErrPaymentAmountMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}

		quotaToAdd = topUp.CreditQuotaAmount()
		if quotaToAdd <= 0 {
			return errors.New("无效的充值额度")
		}

		if providerPayload != "" {
			topUp.ProviderPayload = providerPayload
		}
		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		if err := tx.Model(&User{}).Where("id = ?", topUp.UserId).Update("quota", gorm.Expr("quota + ?", quotaToAdd)).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("epusdt topup failed: " + err.Error())
		if errors.Is(err, ErrPaymentMethodMismatch) ||
			errors.Is(err, ErrPaymentAmountMismatch) ||
			errors.Is(err, ErrPaymentCurrencyMismatch) ||
			errors.Is(err, ErrTopUpStatusInvalid) {
			return err
		}
		return errors.New("充值失败，请稍后重试")
	}

	if quotaToAdd > 0 {
		RecordTopupLog(topUp.UserId, fmt.Sprintf("Epusdt充值成功，充值额度: %v，支付金额：%.2f %s", logger.FormatQuota(quotaToAdd), topUp.PaidAmount, topUp.PaidCurrency), callerIp, topUp.PaymentMethod, PaymentProviderEpusdt)
	}

	return nil
}

func samePaymentCurrency(expected string, actual string) bool {
	expected = strings.ToUpper(strings.TrimSpace(expected))
	actual = strings.ToUpper(strings.TrimSpace(actual))
	if expected == "" || actual == "" {
		return expected == actual
	}
	return expected == actual
}

func samePaymentAmount(expected float64, actual float64) bool {
	expectedAmount := decimal.NewFromFloat(expected).Round(8)
	actualAmount := decimal.NewFromFloat(actual).Round(8)
	return expectedAmount.Equal(actualAmount)
}

func paymentMethodMatchesEpusdtToken(paymentMethod string, token string) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" {
		return true
	}
	paymentMethod = strings.ToLower(strings.TrimSpace(paymentMethod))
	if paymentMethod == PaymentMethodEpusdtPrefix+token {
		return true
	}
	expectedPrefix := PaymentMethodEpusdtPrefix + token + ":"
	return strings.HasPrefix(paymentMethod, expectedPrefix)
}

func callbackPaymentMethodMatches(expected string, actual string, provider string) bool {
	expected = strings.ToLower(strings.TrimSpace(expected))
	actual = strings.ToLower(strings.TrimSpace(actual))
	if expected == "" || actual == "" {
		return expected == actual
	}
	if provider != PaymentProviderEpusdt {
		return expected == actual
	}
	expectedToken, expectedNetwork, ok := epusdtPaymentMethodParts(expected)
	if !ok {
		return false
	}
	actualToken, actualNetwork, ok := epusdtPaymentMethodParts(actual)
	if !ok {
		return false
	}
	if expectedToken != actualToken {
		return false
	}
	if expectedNetwork != "" {
		return actualNetwork != "" && expectedNetwork == actualNetwork
	}
	return true
}

func epusdtPaymentMethodToken(paymentMethod string) (string, bool) {
	token, _, ok := epusdtPaymentMethodParts(paymentMethod)
	return token, ok
}

func epusdtPaymentMethodParts(paymentMethod string) (string, string, bool) {
	paymentMethod = strings.ToLower(strings.TrimSpace(paymentMethod))
	if !strings.HasPrefix(paymentMethod, PaymentMethodEpusdtPrefix) {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(paymentMethod, PaymentMethodEpusdtPrefix), ":")
	if len(parts) < 1 || len(parts) > 2 {
		return "", "", false
	}
	token := strings.TrimSpace(parts[0])
	network := ""
	if len(parts) == 2 {
		network = strings.TrimSpace(parts[1])
	}
	return token, network, token != ""
}
