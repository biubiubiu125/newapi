package model

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	PaymentOrphanStatusPendingReview = "pending_review"
	PaymentOrphanStatusCredited      = "credited"
	PaymentOrphanStatusRefunded      = "refunded"
	PaymentOrphanStatusDismissed     = "dismissed"
)

const (
	PaymentOrphanStripeEventCheckoutSessionCompleted             = "checkout.session.completed"
	PaymentOrphanStripeEventCheckoutSessionAsyncPaymentSucceeded = "checkout.session.async_payment_succeeded"

	PaymentOrphanReasonStripeLocalOrderMissingAfterPaymentSucceeded         = "local order not found after stripe payment succeeded"
	PaymentOrphanReasonStripeSubscriptionPurchaseLimitAfterPaymentSucceeded = "subscription purchase limit reached after payment succeeded"
)

var (
	ErrPaymentOrphanNotFound       = errors.New("payment orphan event not found")
	ErrPaymentOrphanNotCredit      = errors.New("payment orphan event cannot be credited")
	ErrPaymentOrphanPayloadInvalid = errors.New("payment orphan event payload is invalid")
)

type paymentOrphanCreditResult struct {
	Kind         string
	UserID       int
	PaidAmount   float64
	PaidCurrency string
	CreditQuota  int64
	ProductName  string
	UpgradeGroup string
}

type PaymentOrphanEvent struct {
	ID             int64  `json:"id"`
	Provider       string `json:"provider" gorm:"type:varchar(32);not null;index:idx_payment_orphan_provider_reference,priority:1"`
	EventID        string `json:"event_id" gorm:"type:varchar(128);not null;uniqueIndex"`
	EventType      string `json:"event_type" gorm:"type:varchar(128);not null;index"`
	ReferenceID    string `json:"reference_id" gorm:"type:varchar(255);index:idx_payment_orphan_provider_reference,priority:2"`
	SessionID      string `json:"session_id" gorm:"type:varchar(128);index"`
	Status         string `json:"status" gorm:"type:varchar(32);not null;index"`
	Reason         string `json:"reason" gorm:"type:varchar(512)"`
	Error          string `json:"error" gorm:"type:text"`
	Payload        string `json:"-" gorm:"type:text"`
	CreateTime     int64  `json:"create_time" gorm:"index"`
	UpdateTime     int64  `json:"update_time"`
	ResolvedBy     int    `json:"resolved_by" gorm:"index"`
	ResolvedAt     int64  `json:"resolved_at" gorm:"index"`
	Resolution     string `json:"resolution" gorm:"type:varchar(32)"`
	ResolutionNote string `json:"resolution_note" gorm:"type:text"`
	CanCredit      bool   `json:"can_credit" gorm:"-"`
}

func RecordPaymentOrphanEvent(event *PaymentOrphanEvent) error {
	if event == nil {
		return nil
	}
	event.Provider = strings.TrimSpace(event.Provider)
	event.EventType = strings.TrimSpace(event.EventType)
	event.ReferenceID = strings.TrimSpace(event.ReferenceID)
	event.SessionID = strings.TrimSpace(event.SessionID)
	event.Status = strings.TrimSpace(event.Status)
	if event.Status == "" {
		event.Status = PaymentOrphanStatusPendingReview
	}
	event.EventID = paymentOrphanEventID(event)
	now := common.GetTimestamp()
	if event.CreateTime <= 0 {
		event.CreateTime = now
	}
	event.UpdateTime = now
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "event_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"provider",
			"event_type",
			"reference_id",
			"session_id",
			"reason",
			"error",
			"payload",
			"update_time",
		}),
	}).Create(event).Error
}

func ListPaymentOrphanEvents(status string, pageInfo *common.PageInfo) (events []*PaymentOrphanEvent, total int64, err error) {
	query := DB.Model(&PaymentOrphanEvent{})
	status = strings.TrimSpace(status)
	if status != "" && status != "all" {
		query = query.Where("status = ?", status)
	}
	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = query.Order("id desc").
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Find(&events).Error
	for _, event := range events {
		event.CanCredit = CanCreditStripePaymentOrphanEvent(event)
	}
	return events, total, err
}

// CreditStripePaymentOrphan recreates a missing Stripe top-up order and credits
// the matched Stripe customer atomically. It is deliberately limited to
// top-up references; subscription payments require a plan-specific decision.
func CanCreditStripePaymentOrphanEvent(orphan *PaymentOrphanEvent) bool {
	referenceID, ok := canCreditStripePaymentOrphanBase(orphan)
	if !ok {
		return false
	}
	payload, err := common.StrToMap(orphan.Payload)
	if err != nil {
		return false
	}
	if strings.HasPrefix(referenceID, "ref_") {
		return canCreditStripeTopUpPaymentOrphanPayload(payload)
	}
	if strings.HasPrefix(referenceID, "sub_ref_") {
		return canCreditStripeSubscriptionPaymentOrphanPayload(payload)
	}
	return false
}

func canCreditStripePaymentOrphanBase(orphan *PaymentOrphanEvent) (string, bool) {
	if orphan == nil {
		return "", false
	}
	if orphan.Status != PaymentOrphanStatusPendingReview ||
		!strings.EqualFold(strings.TrimSpace(orphan.Provider), PaymentProviderStripe) {
		return "", false
	}
	switch strings.TrimSpace(orphan.EventType) {
	case PaymentOrphanStripeEventCheckoutSessionCompleted,
		PaymentOrphanStripeEventCheckoutSessionAsyncPaymentSucceeded:
	default:
		return "", false
	}
	referenceID := strings.TrimSpace(orphan.ReferenceID)
	if !strings.HasPrefix(referenceID, "ref_") &&
		!strings.HasPrefix(referenceID, "sub_ref_") {
		return "", false
	}
	switch strings.TrimSpace(orphan.Reason) {
	case PaymentOrphanReasonStripeLocalOrderMissingAfterPaymentSucceeded,
		PaymentOrphanReasonStripeSubscriptionPurchaseLimitAfterPaymentSucceeded:
	default:
		return "", false
	}
	return referenceID, true
}

// CreditStripePaymentOrphan recreates missing Stripe top-up/subscription payment
// state and credits the matched user atomically. It is deliberately limited to
// Stripe checkout success events that are pending manual review.
func CreditStripePaymentOrphan(id int64, resolvedBy int, callerIP string) error {
	var (
		userID                     int
		quota                      int64
		money                      float64
		currency                   string
		requestAmount              int64
		quotaPerUnit               float64
		priceSnapshot              float64
		usdExchangeRateSnapshot    float64
		customExchangeRateSnapshot float64
		quotaDisplayTypeSnapshot   string
		displayCurrencySnapshot    string
		topupGroupRatioSnapshot    float64
		amountDiscountSnapshot     float64
		creditResult               paymentOrphanCreditResult
	)
	err := DB.Transaction(func(tx *gorm.DB) error {
		orphan := &PaymentOrphanEvent{}
		if err := lockForUpdate(tx).Where("id = ?", id).First(orphan).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrPaymentOrphanNotFound
			}
			return err
		}
		if orphan.Status == PaymentOrphanStatusCredited {
			return nil
		}
		if _, ok := canCreditStripePaymentOrphanBase(orphan); !ok {
			return ErrPaymentOrphanNotCredit
		}

		payload, err := common.StrToMap(orphan.Payload)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrPaymentOrphanPayloadInvalid, err)
		}
		if strings.HasPrefix(orphan.ReferenceID, "sub_ref_") {
			creditResult, err = creditStripeSubscriptionPaymentOrphanTx(tx, orphan, payload, resolvedBy)
			userID = creditResult.UserID
			return err
		}
		if !strings.HasPrefix(orphan.ReferenceID, "ref_") {
			return ErrPaymentOrphanNotCredit
		}
		customerID := paymentOrphanPayloadString(payload, "customer")
		amountTotal := paymentOrphanPayloadString(payload, "amount_total")
		currency = strings.ToUpper(paymentOrphanPayloadString(payload, "currency"))
		if customerID == "" || amountTotal == "" || currency == "" {
			return ErrPaymentOrphanPayloadInvalid
		}
		if err := lockStripeCustomerForOrphanTx(tx, customerID); err != nil {
			return err
		}
		money, err = StripeAmountFromMinorUnit(amountTotal, currency)
		if err != nil {
			return ErrPaymentOrphanPayloadInvalid
		}
		expectedPaidAmount, expectedPaidCurrency, err := validateTopUpPaymentOrphanPaidFacts(payload, money, currency)
		if err != nil {
			return err
		}
		quota, err = paymentOrphanPayloadInt64(payload, "credit_quota")
		if err != nil || quota <= 0 {
			return ErrPaymentOrphanPayloadInvalid
		}
		quotaPerUnit, _, err = paymentOrphanPayloadOptionalFloat64(payload, "quota_per_unit")
		if err != nil {
			return ErrPaymentOrphanPayloadInvalid
		}
		requestAmount, _, err = paymentOrphanPayloadOptionalInt64(payload, "request_amount")
		if err != nil {
			return ErrPaymentOrphanPayloadInvalid
		}
		priceSnapshot, _, err = paymentOrphanPayloadOptionalFloat64(payload, "price_snapshot")
		if err != nil {
			return ErrPaymentOrphanPayloadInvalid
		}
		usdExchangeRateSnapshot, _, err = paymentOrphanPayloadOptionalFloat64(payload, "usd_exchange_rate_snapshot")
		if err != nil {
			return ErrPaymentOrphanPayloadInvalid
		}
		customExchangeRateSnapshot, _, err = paymentOrphanPayloadOptionalFloat64(payload, "custom_exchange_rate_snapshot")
		if err != nil {
			return ErrPaymentOrphanPayloadInvalid
		}
		quotaDisplayTypeSnapshot = paymentOrphanPayloadOptionalString(payload, "quota_display_type_snapshot")
		displayCurrencySnapshot = paymentOrphanPayloadOptionalString(payload, "display_currency_snapshot")
		topupGroupRatioSnapshot, _, err = paymentOrphanPayloadOptionalFloat64(payload, "topup_group_ratio_snapshot")
		if err != nil {
			return ErrPaymentOrphanPayloadInvalid
		}
		amountDiscountSnapshot, _, err = paymentOrphanPayloadOptionalFloat64(payload, "amount_discount_snapshot")
		if err != nil {
			return ErrPaymentOrphanPayloadInvalid
		}
		if topupGroupRatioSnapshot <= 0 {
			topupGroupRatioSnapshot = 1
		}
		if amountDiscountSnapshot <= 0 {
			amountDiscountSnapshot = 1
		}

		user := &User{}
		metadataUserID, hasMetadataUserID, err := paymentOrphanPayloadOptionalInt64(payload, "user_id")
		if err != nil || (hasMetadataUserID && metadataUserID <= 0) {
			return ErrPaymentOrphanPayloadInvalid
		}
		if hasMetadataUserID {
			if err := tx.First(user, int(metadataUserID)).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return fmt.Errorf("%w: user %d", ErrPaymentOrphanNotCredit, metadataUserID)
				}
				return err
			}
			if strings.TrimSpace(user.StripeCustomer) != "" && user.StripeCustomer != customerID {
				return fmt.Errorf("%w: stripe customer %s", ErrPaymentOrphanNotCredit, customerID)
			}
			if err := ensureStripeCustomerNotBoundToOtherUserTx(tx, customerID, user.Id); err != nil {
				return err
			}
		} else {
			user, err = findUniqueStripeCustomerUserTx(tx, customerID)
			if err != nil {
				return err
			}
		}
		topUp := &TopUp{}
		if err := tx.Where("trade_no = ?", orphan.ReferenceID).First(topUp).Error; err == nil {
			if topUp.Status == common.TopUpStatusSuccess {
				if topUp.UserId != user.Id ||
					topUp.PaymentProvider != PaymentProviderStripe ||
					!paymentOrphanMatchesExistingTopUp(topUp, money, currency) {
					return ErrPaymentOrphanNotCredit
				}
				orphan.Status = PaymentOrphanStatusCredited
				orphan.ResolvedBy = resolvedBy
				orphan.ResolvedAt = common.GetTimestamp()
				orphan.Resolution = PaymentOrphanStatusCredited
				orphan.ResolutionNote = paymentOrphanTopUpResolutionNote(topUp.UserId, topUp.PaidAmount, topUp.PaidCurrency, int64(topUp.CreditQuotaAmount()))
				return tx.Save(orphan).Error
			}
			return ErrPaymentOrphanNotCredit
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		if quotaPerUnit <= 0 {
			if expectedPaidAmount <= 0 {
				return ErrPaymentOrphanPayloadInvalid
			}
			quotaPerUnit = decimal.NewFromInt(quota).
				Div(decimal.NewFromFloat(expectedPaidAmount)).
				InexactFloat64()
		}
		if requestAmount <= 0 && quotaPerUnit > 0 {
			requestAmount = decimal.NewFromInt(quota).
				Div(decimal.NewFromFloat(quotaPerUnit)).
				IntPart()
		}
		if requestAmount <= 0 {
			return ErrPaymentOrphanPayloadInvalid
		}
		now := common.GetTimestamp()
		topUp = &TopUp{
			UserId:                     user.Id,
			Amount:                     requestAmount,
			Money:                      expectedPaidAmount,
			PaidAmount:                 money,
			PaidCurrency:               expectedPaidCurrency,
			TradeNo:                    orphan.ReferenceID,
			PaymentMethod:              PaymentMethodStripe,
			PaymentProvider:            PaymentProviderStripe,
			ProviderPayload:            orphan.Payload,
			OrderSnapshotVersion:       1,
			RequestAmountSnapshot:      requestAmount,
			QuotaPerUnitSnapshot:       quotaPerUnit,
			CreditQuotaSnapshot:        quota,
			PriceSnapshot:              priceSnapshot,
			USDExchangeRateSnapshot:    usdExchangeRateSnapshot,
			CustomExchangeRateSnapshot: customExchangeRateSnapshot,
			QuotaDisplayTypeSnapshot:   quotaDisplayTypeSnapshot,
			DisplayCurrencySnapshot:    displayCurrencySnapshot,
			TopupGroupRatioSnapshot:    topupGroupRatioSnapshot,
			AmountDiscountSnapshot:     amountDiscountSnapshot,
			CreateTime:                 orphan.CreateTime,
			CompleteTime:               now,
			Status:                     common.TopUpStatusSuccess,
		}
		applyOrphanReferralMetadataToTopUp(topUp, payload)
		if topUp.CreateTime <= 0 {
			topUp.CreateTime = now
		}
		if err := tx.Create(topUp).Error; err != nil {
			return err
		}
		updates := map[string]interface{}{
			"quota": gorm.Expr("quota + ?", quota),
		}
		if strings.TrimSpace(user.StripeCustomer) == "" {
			updates["stripe_customer"] = customerID
		}
		if err := tx.Model(&User{}).
			Where("id = ?", user.Id).
			Updates(updates).Error; err != nil {
			return err
		}
		userID = user.Id
		creditResult = paymentOrphanCreditResult{
			Kind:         "topup",
			UserID:       user.Id,
			PaidAmount:   money,
			PaidCurrency: currency,
			CreditQuota:  quota,
		}

		orphan.Status = PaymentOrphanStatusCredited
		orphan.ResolvedBy = resolvedBy
		orphan.ResolvedAt = now
		orphan.Resolution = PaymentOrphanStatusCredited
		orphan.ResolutionNote = paymentOrphanTopUpResolutionNote(user.Id, money, expectedPaidCurrency, quota)
		return tx.Save(orphan).Error
	})
	if err != nil {
		return err
	}
	if userID != 0 && creditResult.Kind == "topup" {
		_ = cacheUpdateUserQuota(userID)
		RecordTopupLog(userID, fmt.Sprintf("Stripe 孤儿支付补单成功，充值金额：%.2f %s，额度：%d", creditResult.PaidAmount, creditResult.PaidCurrency, creditResult.CreditQuota), callerIP, PaymentMethodStripe, "admin")
	}
	if userID != 0 && creditResult.Kind == "subscription" {
		if strings.TrimSpace(creditResult.UpgradeGroup) != "" {
			_ = UpdateUserGroupCache(userID, creditResult.UpgradeGroup)
		}
		RecordPaymentAuditLog(userID, fmt.Sprintf("Stripe 孤儿订阅补发成功，套餐: %s，支付金额: %.2f %s", creditResult.ProductName, creditResult.PaidAmount, creditResult.PaidCurrency), PaymentAuditLogInfo{
			CallerIP:        callerIP,
			PaymentMethod:   PaymentMethodStripe,
			PaymentProvider: PaymentProviderStripe,
			OrderType:       "subscription",
			ProductName:     creditResult.ProductName,
			PaidAmount:      creditResult.PaidAmount,
			PaidCurrency:    creditResult.PaidCurrency,
		})
	}
	return nil
}

func creditStripeSubscriptionPaymentOrphanTx(tx *gorm.DB, orphan *PaymentOrphanEvent, payload map[string]interface{}, resolvedBy int) (paymentOrphanCreditResult, error) {
	result := paymentOrphanCreditResult{Kind: "subscription"}
	if tx == nil || orphan == nil {
		return result, ErrPaymentOrphanNotCredit
	}
	customerID := paymentOrphanPayloadString(payload, "customer")
	amountTotal := paymentOrphanPayloadString(payload, "amount_total")
	currency := strings.ToUpper(paymentOrphanPayloadString(payload, "currency"))
	if customerID == "" || amountTotal == "" || currency == "" {
		return result, ErrPaymentOrphanPayloadInvalid
	}
	if err := lockStripeCustomerForOrphanTx(tx, customerID); err != nil {
		return result, err
	}
	paidAmount, err := StripeAmountFromMinorUnit(amountTotal, currency)
	if err != nil {
		return result, ErrPaymentOrphanPayloadInvalid
	}
	userID, hasUserID, err := paymentOrphanPayloadOptionalInt64(payload, "user_id")
	if err != nil || !hasUserID || userID <= 0 {
		return result, ErrPaymentOrphanPayloadInvalid
	}
	planID, hasPlanID, err := paymentOrphanPayloadOptionalInt64(payload, "plan_id")
	if err != nil || !hasPlanID || planID <= 0 {
		return result, ErrPaymentOrphanPayloadInvalid
	}

	user := &User{}
	if err := tx.First(user, int(userID)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return result, fmt.Errorf("%w: user %d", ErrPaymentOrphanNotCredit, userID)
		}
		return result, err
	}
	if strings.TrimSpace(user.StripeCustomer) != "" && user.StripeCustomer != customerID {
		return result, fmt.Errorf("%w: stripe customer %s", ErrPaymentOrphanNotCredit, customerID)
	}
	if err := ensureStripeCustomerNotBoundToOtherUserTx(tx, customerID, user.Id); err != nil {
		return result, err
	}
	plan, err := getSubscriptionPlanByIdTx(tx, int(planID))
	if err != nil {
		return result, err
	}
	expectedAmount := plan.PriceAmount
	if metadataAmount, hasAmount, err := paymentOrphanPayloadOptionalFloat64(payload, "paid_amount"); err != nil {
		return result, ErrPaymentOrphanPayloadInvalid
	} else if hasAmount {
		expectedAmount = metadataAmount
	}
	expectedCurrency := strings.ToUpper(strings.TrimSpace(plan.Currency))
	if expectedCurrency == "" {
		expectedCurrency = "USD"
	}
	if metadataCurrency := strings.ToUpper(paymentOrphanPayloadOptionalString(payload, "paid_currency")); metadataCurrency != "" {
		expectedCurrency = metadataCurrency
	}
	if !samePaymentCurrency(expectedCurrency, currency) || !samePaymentAmount(expectedAmount, paidAmount) {
		return result, ErrPaymentOrphanNotCredit
	}

	order := &SubscriptionOrder{}
	if err := tx.Where("trade_no = ?", orphan.ReferenceID).First(order).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return result, err
		}
		order = &SubscriptionOrder{
			UserId:          int(userID),
			PlanId:          int(planID),
			Money:           expectedAmount,
			PaidAmount:      paidAmount,
			PaidCurrency:    currency,
			TradeNo:         orphan.ReferenceID,
			PaymentMethod:   PaymentMethodStripe,
			PaymentProvider: PaymentProviderStripe,
			ProviderPayload: orphan.Payload,
			CreateTime:      orphan.CreateTime,
			Status:          common.TopUpStatusPending,
		}
		if order.CreateTime <= 0 {
			order.CreateTime = common.GetTimestamp()
		}
		order.ApplyPlanSnapshotFields(plan, currency)
		applySubscriptionOrderOrphanMetadata(order, payload)
		if err := tx.Create(order).Error; err != nil {
			return result, err
		}
	} else if order.Status == common.TopUpStatusSuccess {
		if order.UserId != int(userID) ||
			order.PlanId != int(planID) ||
			order.PaymentProvider != PaymentProviderStripe ||
			!paymentOrphanMatchesExistingSubscriptionOrder(order, paidAmount, currency, expectedCurrency) {
			return result, ErrPaymentOrphanNotCredit
		}
		orphan.Status = PaymentOrphanStatusCredited
		orphan.ResolvedBy = resolvedBy
		orphan.ResolvedAt = common.GetTimestamp()
		orphan.Resolution = PaymentOrphanStatusCredited
		orphan.ResolutionNote = paymentOrphanSubscriptionResolutionNote(order.UserId, order.PaidAmount, order.PaidCurrency, order.PlanTitleSnapshot)
		if err := tx.Save(orphan).Error; err != nil {
			return result, err
		}
		result.UserID = order.UserId
		result.PaidAmount = order.PaidAmount
		result.PaidCurrency = order.PaidCurrency
		result.ProductName = order.PlanTitleSnapshot
		result.UpgradeGroup = order.PlanUpgradeGroupSnapshot
		return result, nil
	} else if order.Status != common.TopUpStatusPending ||
		order.UserId != int(userID) ||
		order.PlanId != int(planID) ||
		order.PaymentProvider != PaymentProviderStripe ||
		!samePaymentCurrency(order.PaidCurrency, currency) ||
		!samePaymentAmount(order.PaidAmount, paidAmount) {
		return result, ErrPaymentOrphanNotCredit
	}

	plan = order.ApplyPlanSnapshot(plan)
	if plan == nil {
		return result, ErrPaymentOrphanNotCredit
	}
	if _, err := createUserSubscriptionFromPlanTx(tx, order.UserId, plan, "order", createUserSubscriptionOptions{
		SkipPurchaseLimit: true,
	}); err != nil {
		return result, err
	}
	order.ProviderPayload = orphan.Payload
	if err := upsertSubscriptionTopUpTx(tx, order); err != nil {
		return result, err
	}
	now := common.GetTimestamp()
	order.Status = common.TopUpStatusSuccess
	order.CompleteTime = now
	if err := tx.Save(order).Error; err != nil {
		return result, err
	}
	if strings.TrimSpace(user.StripeCustomer) == "" {
		if err := tx.Model(&User{}).Where("id = ?", user.Id).Update("stripe_customer", customerID).Error; err != nil {
			return result, err
		}
	}
	orphan.Status = PaymentOrphanStatusCredited
	orphan.ResolvedBy = resolvedBy
	orphan.ResolvedAt = now
	orphan.Resolution = PaymentOrphanStatusCredited
	orphan.ResolutionNote = paymentOrphanSubscriptionResolutionNote(order.UserId, order.PaidAmount, order.PaidCurrency, plan.Title)
	if err := tx.Save(orphan).Error; err != nil {
		return result, err
	}

	result.UserID = order.UserId
	result.PaidAmount = order.PaidAmount
	result.PaidCurrency = order.PaidCurrency
	result.ProductName = plan.Title
	result.UpgradeGroup = strings.TrimSpace(plan.UpgradeGroup)
	result.CreditQuota = plan.TotalAmount
	return result, nil
}

func validateTopUpPaymentOrphanPaidFacts(payload map[string]interface{}, actualAmount float64, actualCurrency string) (float64, string, error) {
	expectedAmount, hasExpectedAmount, err := paymentOrphanPayloadOptionalFloat64(payload, "paid_amount")
	if err != nil || !hasExpectedAmount || expectedAmount <= 0 {
		return 0, "", ErrPaymentOrphanPayloadInvalid
	}
	expectedCurrency := strings.ToUpper(paymentOrphanPayloadOptionalString(payload, "paid_currency"))
	if strings.TrimSpace(expectedCurrency) == "" {
		return 0, "", ErrPaymentOrphanPayloadInvalid
	}
	if _, _, err := validatedPaymentFacts(expectedAmount, expectedCurrency, PaymentCallbackValidation{
		PaidAmount:           actualAmount,
		PaidCurrency:         actualCurrency,
		RequirePaymentFacts:  true,
		AllowPaymentDiscount: paymentOrphanPayloadAllowsStripeDiscount(payload),
	}); err != nil {
		return 0, "", ErrPaymentOrphanNotCredit
	}
	return expectedAmount, expectedCurrency, nil
}

func canCreditStripeTopUpPaymentOrphanPayload(payload map[string]interface{}) bool {
	customerID := paymentOrphanPayloadString(payload, "customer")
	amountTotal := paymentOrphanPayloadString(payload, "amount_total")
	currency := strings.ToUpper(paymentOrphanPayloadString(payload, "currency"))
	if customerID == "" || amountTotal == "" || currency == "" {
		return false
	}
	paidAmount, err := StripeAmountFromMinorUnit(amountTotal, currency)
	if err != nil {
		return false
	}
	if _, _, err = validateTopUpPaymentOrphanPaidFacts(payload, paidAmount, currency); err != nil {
		return false
	}
	quota, err := paymentOrphanPayloadInt64(payload, "credit_quota")
	return err == nil && quota > 0
}

func canCreditStripeSubscriptionPaymentOrphanPayload(payload map[string]interface{}) bool {
	customerID := paymentOrphanPayloadString(payload, "customer")
	amountTotal := paymentOrphanPayloadString(payload, "amount_total")
	currency := strings.ToUpper(paymentOrphanPayloadString(payload, "currency"))
	if customerID == "" || amountTotal == "" || currency == "" {
		return false
	}
	if _, err := StripeAmountFromMinorUnit(amountTotal, currency); err != nil {
		return false
	}
	userID, hasUserID, err := paymentOrphanPayloadOptionalInt64(payload, "user_id")
	if err != nil || !hasUserID || userID <= 0 {
		return false
	}
	planID, hasPlanID, err := paymentOrphanPayloadOptionalInt64(payload, "plan_id")
	if err != nil || !hasPlanID || planID <= 0 {
		return false
	}
	if amount, hasAmount, err := paymentOrphanPayloadOptionalFloat64(payload, "paid_amount"); err != nil || (hasAmount && amount <= 0) {
		return false
	}
	return true
}

func paymentOrphanPayloadAllowsStripeDiscount(payload map[string]interface{}) bool {
	enabled, hasEnabled, err := paymentOrphanPayloadOptionalBool(payload, "promotion_codes_enabled")
	if err != nil || !hasEnabled || !enabled {
		return false
	}
	totalDetails, ok := payload["total_details"].(map[string]interface{})
	if !ok || totalDetails == nil {
		return false
	}
	amountDiscount, hasDiscount, err := paymentOrphanPayloadOptionalFloat64(totalDetails, "amount_discount")
	return err == nil && hasDiscount && amountDiscount > 0
}

func ensureStripeCustomerNotBoundToOtherUserTx(tx *gorm.DB, customerID string, userID int) error {
	customerID = strings.TrimSpace(customerID)
	if tx == nil || customerID == "" || userID <= 0 {
		return ErrPaymentOrphanPayloadInvalid
	}
	owner := &User{}
	err := lockForUpdate(tx).Select("id").Where("stripe_customer = ? AND id <> ?", customerID, userID).First(owner).Error
	if err == nil {
		return fmt.Errorf("%w: stripe customer %s already belongs to user %d", ErrPaymentOrphanNotCredit, customerID, owner.Id)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	return err
}

// lockStripeCustomerForOrphanTx serializes orphan credits that target the same
// Stripe customer, including the first bind when no user row currently owns
// the customer. Checking existing rows alone cannot close that race because
// two transactions can both observe an unbound customer and then bind it.
func lockStripeCustomerForOrphanTx(tx *gorm.DB, customerID string) error {
	customerID = strings.TrimSpace(customerID)
	if tx == nil || customerID == "" {
		return ErrPaymentOrphanPayloadInvalid
	}
	switch {
	case common.UsingMainDatabase(common.DatabaseTypePostgreSQL):
		return tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", customerID).Error
	case common.UsingMainDatabase(common.DatabaseTypeMySQL):
		var ids []int
		return lockForUpdate(tx).
			Model(&User{}).
			Select("id").
			Where("stripe_customer = ?", customerID).
			Find(&ids).Error
	default:
		// SQLite has a single-writer transaction model; the conflicting update
		// is serialized by the database and the second transaction cannot
		// commit a duplicate binding.
		return nil
	}
}

func paymentOrphanTopUpResolutionNote(userID int, paidAmount float64, paidCurrency string, quota int64) string {
	return fmt.Sprintf("credited topup user_id=%d amount=%.2f %s quota=%d", userID, paidAmount, strings.ToUpper(strings.TrimSpace(paidCurrency)), quota)
}

func paymentOrphanSubscriptionResolutionNote(userID int, paidAmount float64, paidCurrency string, productName string) string {
	return fmt.Sprintf("credited subscription user_id=%d amount=%.2f %s product=%s", userID, paidAmount, strings.ToUpper(strings.TrimSpace(paidCurrency)), strings.TrimSpace(productName))
}

func paymentOrphanMatchesExistingTopUp(topUp *TopUp, paidAmount float64, paidCurrency string) bool {
	if topUp == nil {
		return false
	}
	expectedAmount := topUp.PaidAmount
	if expectedAmount <= 0 {
		expectedAmount = topUp.Money
	}
	expectedCurrency := topUp.PaidCurrency
	if strings.TrimSpace(expectedCurrency) == "" {
		expectedCurrency = defaultPaymentFactsCurrency(topUp.PaymentProvider)
	}
	_, _, err := validatedPaymentFacts(expectedAmount, expectedCurrency, PaymentCallbackValidation{
		PaidAmount:          paidAmount,
		PaidCurrency:        paidCurrency,
		RequirePaymentFacts: true,
	})
	return err == nil
}

func paymentOrphanMatchesExistingSubscriptionOrder(order *SubscriptionOrder, paidAmount float64, paidCurrency string, fallbackCurrency string) bool {
	if order == nil {
		return false
	}
	expectedAmount := order.PaidAmount
	if expectedAmount <= 0 {
		expectedAmount = order.Money
	}
	expectedCurrency := order.PaidCurrency
	if strings.TrimSpace(expectedCurrency) == "" {
		expectedCurrency = fallbackCurrency
	}
	_, _, err := validatedPaymentFacts(expectedAmount, expectedCurrency, PaymentCallbackValidation{
		PaidAmount:          paidAmount,
		PaidCurrency:        paidCurrency,
		RequirePaymentFacts: true,
	})
	return err == nil
}

func applySubscriptionOrderOrphanMetadata(order *SubscriptionOrder, payload map[string]interface{}) {
	if order == nil {
		return
	}
	if value := paymentOrphanPayloadOptionalString(payload, "plan_title_snapshot"); value != "" {
		order.PlanTitleSnapshot = value
	}
	if value, ok, err := paymentOrphanPayloadOptionalFloat64(payload, "plan_price_snapshot"); err == nil && ok {
		order.PlanPriceSnapshot = value
	}
	if value := strings.ToUpper(paymentOrphanPayloadOptionalString(payload, "plan_currency_snapshot")); value != "" {
		order.PlanCurrencySnapshot = value
	}
	if value := paymentOrphanPayloadOptionalString(payload, "plan_duration_unit_snapshot"); value != "" {
		order.PlanDurationUnitSnapshot = value
	}
	if value, ok, err := paymentOrphanPayloadOptionalInt64(payload, "plan_duration_value_snapshot"); err == nil && ok {
		order.PlanDurationValueSnapshot = int(value)
	}
	if value, ok, err := paymentOrphanPayloadOptionalInt64(payload, "plan_custom_seconds_snapshot"); err == nil && ok {
		order.PlanCustomSecondsSnapshot = value
	}
	if value, ok, err := paymentOrphanPayloadOptionalInt64(payload, "plan_total_amount_snapshot"); err == nil && ok {
		order.PlanTotalAmountSnapshot = value
	}
	if value := paymentOrphanPayloadOptionalString(payload, "plan_quota_reset_period_snapshot"); value != "" {
		order.PlanQuotaResetPeriodSnapshot = value
	}
	if value, ok, err := paymentOrphanPayloadOptionalInt64(payload, "plan_quota_reset_custom_seconds_snapshot"); err == nil && ok {
		order.PlanQuotaResetCustomSecondsSnapshot = value
	}
	if value := paymentOrphanPayloadOptionalString(payload, "plan_upgrade_group_snapshot"); value != "" {
		order.PlanUpgradeGroupSnapshot = value
	}
	if value := paymentOrphanPayloadOptionalString(payload, "plan_grant_groups_snapshot"); value != "" {
		order.PlanGrantGroupsSnapshot = value
	}
	if value := paymentOrphanPayloadOptionalString(payload, "plan_downgrade_group_snapshot"); value != "" {
		order.PlanDowngradeGroupSnapshot = value
	}
	if value, ok, err := paymentOrphanPayloadOptionalBool(payload, "plan_allow_balance_pay_snapshot"); err == nil && ok {
		order.PlanAllowBalancePaySnapshot = &value
	}
	if value, ok, err := paymentOrphanPayloadOptionalBool(payload, "plan_allow_wallet_overflow_snapshot"); err == nil && ok {
		order.PlanAllowWalletOverflowSnapshot = &value
	}
	if value, ok, err := paymentOrphanPayloadOptionalFloat64(payload, "usd_exchange_rate_snapshot"); err == nil && ok {
		order.USDExchangeRateSnapshot = value
	}
	if value, ok, err := paymentOrphanPayloadOptionalFloat64(payload, "custom_exchange_rate_snapshot"); err == nil && ok {
		order.CustomExchangeRateSnapshot = value
	}
	if value := paymentOrphanPayloadOptionalString(payload, "quota_display_type_snapshot"); value != "" {
		order.QuotaDisplayTypeSnapshot = value
	}
	if value := paymentOrphanPayloadOptionalString(payload, "display_currency_snapshot"); value != "" {
		order.DisplayCurrencySnapshot = value
	}
	applyOrphanReferralMetadataToSubscriptionOrder(order, payload)
}

func applyOrphanReferralMetadataToTopUp(topUp *TopUp, payload map[string]interface{}) {
	if topUp == nil {
		return
	}
	if value, ok, err := paymentOrphanPayloadOptionalInt64(payload, "referral_affiliate_id"); err == nil && ok {
		topUp.ReferralAffiliateId = int(value)
	}
	if value, ok, err := paymentOrphanPayloadOptionalFloat64(payload, "referral_rate"); err == nil && ok {
		topUp.ReferralRate = value
	}
	if value, ok, err := paymentOrphanPayloadOptionalFloat64(payload, "referral_base_amount"); err == nil && ok {
		topUp.ReferralBaseAmount = value
	}
	if value := strings.ToUpper(paymentOrphanPayloadOptionalString(payload, "referral_base_currency")); value != "" {
		topUp.ReferralBaseCurrency = value
	}
	if value := paymentOrphanPayloadOptionalString(payload, "referral_commission_status"); value != "" {
		topUp.ReferralCommissionStatus = value
	}
	if value := paymentOrphanPayloadOptionalString(payload, "referral_commission_error"); value != "" {
		topUp.ReferralCommissionError = value
	}
	if value, ok, err := paymentOrphanPayloadOptionalInt64(payload, "referral_commission_at"); err == nil && ok {
		topUp.ReferralCommissionAt = value
	}
}

func applyOrphanReferralMetadataToSubscriptionOrder(order *SubscriptionOrder, payload map[string]interface{}) {
	if order == nil {
		return
	}
	if value, ok, err := paymentOrphanPayloadOptionalInt64(payload, "referral_affiliate_id"); err == nil && ok {
		order.ReferralAffiliateId = int(value)
	}
	if value, ok, err := paymentOrphanPayloadOptionalFloat64(payload, "referral_rate"); err == nil && ok {
		order.ReferralRate = value
	}
	if value, ok, err := paymentOrphanPayloadOptionalFloat64(payload, "referral_base_amount"); err == nil && ok {
		order.ReferralBaseAmount = value
	}
	if value := strings.ToUpper(paymentOrphanPayloadOptionalString(payload, "referral_base_currency")); value != "" {
		order.ReferralBaseCurrency = value
	}
	if value := paymentOrphanPayloadOptionalString(payload, "referral_commission_status"); value != "" {
		order.ReferralCommissionStatus = value
	}
	if value := paymentOrphanPayloadOptionalString(payload, "referral_commission_error"); value != "" {
		order.ReferralCommissionError = value
	}
	if value, ok, err := paymentOrphanPayloadOptionalInt64(payload, "referral_commission_at"); err == nil && ok {
		order.ReferralCommissionAt = value
	}
}

func findUniqueStripeCustomerUserTx(tx *gorm.DB, customerID string) (*User, error) {
	customerID = strings.TrimSpace(customerID)
	if customerID == "" {
		return nil, ErrPaymentOrphanPayloadInvalid
	}
	var users []User
	if err := tx.Where("stripe_customer = ?", customerID).Limit(2).Find(&users).Error; err != nil {
		return nil, err
	}
	if len(users) != 1 {
		return nil, fmt.Errorf("%w: stripe customer %s", ErrPaymentOrphanNotCredit, customerID)
	}
	return &users[0], nil
}

func MarkPaymentOrphanResolved(id int64, status string, resolvedBy int, note string) error {
	if status != PaymentOrphanStatusRefunded && status != PaymentOrphanStatusDismissed {
		return ErrPaymentOrphanNotCredit
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		orphan := &PaymentOrphanEvent{}
		if err := lockForUpdate(tx).Where("id = ?", id).First(orphan).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrPaymentOrphanNotFound
			}
			return err
		}
		if orphan.Status != PaymentOrphanStatusPendingReview {
			return nil
		}
		now := common.GetTimestamp()
		orphan.Status = status
		orphan.ResolvedBy = resolvedBy
		orphan.ResolvedAt = now
		orphan.Resolution = status
		orphan.ResolutionNote = strings.TrimSpace(note)
		return tx.Save(orphan).Error
	})
}

func paymentOrphanPayloadString(payload map[string]interface{}, key string) string {
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func paymentOrphanPayloadInt64(payload map[string]interface{}, key string) (int64, error) {
	value := paymentOrphanPayloadString(payload, key)
	if value == "" {
		value = paymentOrphanPayloadMetadataString(payload, key)
	}
	if value == "" {
		return 0, ErrPaymentOrphanPayloadInvalid
	}
	return strconv.ParseInt(value, 10, 64)
}

func paymentOrphanPayloadFloat64(payload map[string]interface{}, key string) (float64, error) {
	value := paymentOrphanPayloadString(payload, key)
	if value == "" {
		value = paymentOrphanPayloadMetadataString(payload, key)
	}
	if value == "" {
		return 0, ErrPaymentOrphanPayloadInvalid
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, ErrPaymentOrphanPayloadInvalid
	}
	return parsed, nil
}

func paymentOrphanPayloadOptionalString(payload map[string]interface{}, key string) string {
	value := paymentOrphanPayloadString(payload, key)
	if value == "" {
		value = paymentOrphanPayloadMetadataString(payload, key)
	}
	return value
}

func paymentOrphanPayloadOptionalInt64(payload map[string]interface{}, key string) (int64, bool, error) {
	value := paymentOrphanPayloadOptionalString(payload, key)
	if value == "" {
		return 0, false, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	return parsed, true, err
}

func paymentOrphanPayloadOptionalFloat64(payload map[string]interface{}, key string) (float64, bool, error) {
	value := paymentOrphanPayloadOptionalString(payload, key)
	if value == "" {
		return 0, false, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err == nil && (math.IsNaN(parsed) || math.IsInf(parsed, 0)) {
		err = ErrPaymentOrphanPayloadInvalid
	}
	return parsed, true, err
}

func paymentOrphanPayloadOptionalBool(payload map[string]interface{}, key string) (bool, bool, error) {
	value := paymentOrphanPayloadOptionalString(payload, key)
	if value == "" {
		return false, false, nil
	}
	parsed, err := strconv.ParseBool(value)
	return parsed, true, err
}

func paymentOrphanPayloadMetadataString(payload map[string]interface{}, key string) string {
	metadata, ok := payload["metadata"].(map[string]interface{})
	if !ok || metadata == nil {
		return ""
	}
	return paymentOrphanPayloadString(metadata, key)
}

func paymentOrphanEventID(event *PaymentOrphanEvent) string {
	if event == nil {
		return ""
	}
	eventID := strings.TrimSpace(event.EventID)
	if eventID != "" {
		return eventID
	}
	source := strings.Join([]string{
		strings.TrimSpace(event.Provider),
		strings.TrimSpace(event.EventType),
		strings.TrimSpace(event.ReferenceID),
		strings.TrimSpace(event.SessionID),
		strings.TrimSpace(event.Payload),
	}, "|")
	return "payment_orphan_" + common.Sha1([]byte(source))
}
