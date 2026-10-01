package model

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

var (
	billingAdjustmentSchemaMu    sync.Mutex
	billingAdjustmentSchemaReady sync.Map

	// BillingAdjustmentBeforeEnsurePending is nil in production.
	// Tests return a retryable error to force another pending-row attempt.
	BillingAdjustmentBeforeEnsurePending func() error

	// BillingAdjustmentAfterEnsurePending is nil in production.
	// Tests return a retryable error after the pending row has committed.
	BillingAdjustmentAfterEnsurePending func() error

	// BillingAdjustmentSpoolDir stores settle requests when the pending row cannot be written.
	// Empty uses the log directory. Tests set a temporary directory.
	BillingAdjustmentSpoolDir string

	// BillingAdjustmentFallbackSpoolDir is the second disk location used when the primary spool cannot be written.
	// Empty uses the process temp directory. Tests set this to force the in-memory fallback.
	BillingAdjustmentFallbackSpoolDir string
)

// EnsureBillingAdjustmentSchema creates the ledger table when a process is
// using a database that has not run the startup migration yet.
func EnsureBillingAdjustmentSchema(db *gorm.DB) error {
	if db == nil {
		return errors.New("database is required")
	}
	if _, ok := billingAdjustmentSchemaReady.Load(db); ok {
		return nil
	}
	billingAdjustmentSchemaMu.Lock()
	defer billingAdjustmentSchemaMu.Unlock()
	if _, ok := billingAdjustmentSchemaReady.Load(db); ok {
		return nil
	}
	if !db.Migrator().HasTable(&BillingAdjustment{}) {
		if err := db.AutoMigrate(&BillingAdjustment{}); err != nil {
			return err
		}
	} else if !db.Migrator().HasColumn(&BillingAdjustment{}, "SubscriptionConsumedAt") {
		if err := db.Migrator().AddColumn(&BillingAdjustment{}, "SubscriptionConsumedAt"); err != nil {
			return err
		}
	}
	billingAdjustmentSchemaReady.Store(db, true)
	return nil
}

const (
	BillingAdjustmentPending    = "pending"
	BillingAdjustmentApplied    = "applied"
	BillingAdjustmentRolledBack = "rolled_back"

	BillingAdjustmentSourceWallet       = "wallet"
	BillingAdjustmentSourceSubscription = "subscription"
)

var (
	// ErrBillingAdjustmentConflict means the stored row does not match this request.
	ErrBillingAdjustmentConflict = errors.New("billing adjustment conflict")
	// ErrBillingAdjustmentClosed means the request was already rolled back.
	ErrBillingAdjustmentClosed = errors.New("billing adjustment closed")
	// ErrBillingAdjustmentAbsent means settle never recorded a row.
	ErrBillingAdjustmentAbsent = errors.New("billing adjustment absent")
	// ErrBillingAdjustmentDeferred means the charge is stored and will be applied later.
	ErrBillingAdjustmentDeferred = errors.New("billing adjustment deferred")
)

// BillingAdjustment is one request's wallet/subscription and token settlement.
// The quota deltas and the row commit in the same transaction, so a retry cannot
// charge or refund twice.
type BillingAdjustment struct {
	ID                     int64  `json:"id" gorm:"primaryKey"`
	IdempotencyKey         string `json:"idempotency_key" gorm:"size:191;uniqueIndex;not null"`
	RequestID              string `json:"request_id" gorm:"size:191;index"`
	UserID                 int    `json:"user_id" gorm:"index"`
	TokenID                int    `json:"token_id"`
	Source                 string `json:"source" gorm:"size:32"`
	SubscriptionID         int    `json:"subscription_id"`
	FundingDelta           int    `json:"funding_delta"`
	TokenDelta             int    `json:"token_delta"`
	ChargeQuota            int    `json:"charge_quota"`
	ReservationQuota       int    `json:"reservation_quota"`
	ExtraReserved          int    `json:"extra_reserved"`
	SubscriptionConsumedAt int64  `json:"subscription_consumed_at"`
	RefundedQuota          int    `json:"refunded_quota"`
	TokenIncluded          bool   `json:"token_included"`
	Status                 string `json:"status" gorm:"size:32;index"`
	CreatedAt              int64  `json:"created_at"`
	UpdatedAt              int64  `json:"updated_at"`
	// WalletCacheInvalidate is set when a refunded pre-consume must be charged
	// in full. It is not a column. The cache is dropped after the transaction
	// commits, because the stored funding delta is no longer the wallet change.
	WalletCacheInvalidate bool `json:"-" gorm:"-"`
}

func (BillingAdjustment) TableName() string {
	return "billing_adjustments"
}

// BillingAdjustmentRequest is the settle payload for one request.
type BillingAdjustmentRequest struct {
	IdempotencyKey   string
	RequestID        string
	UserID           int
	TokenID          int
	TokenKey         string
	Source           string
	SubscriptionID   int
	FundingDelta     int
	TokenDelta       int
	ChargeQuota      int
	ReservationQuota int
	ExtraReserved    int
	TokenIncluded    bool
}

func (r BillingAdjustmentRequest) validate() error {
	if strings.TrimSpace(r.IdempotencyKey) == "" {
		return errors.New("billing adjustment key is empty")
	}
	if r.UserID <= 0 {
		return errors.New("billing adjustment user is empty")
	}
	if r.ChargeQuota < 0 || r.ReservationQuota < 0 || r.ExtraReserved < 0 {
		return errors.New("billing adjustment quota cannot be negative")
	}
	if r.ReservationQuota+r.FundingDelta != r.ChargeQuota {
		return fmt.Errorf("%w: reservation %d delta %d charge %d", ErrBillingAdjustmentConflict, r.ReservationQuota, r.FundingDelta, r.ChargeQuota)
	}
	switch r.Source {
	case BillingAdjustmentSourceWallet:
		if r.SubscriptionID != 0 {
			return fmt.Errorf("%w: wallet adjustment cannot carry a subscription", ErrBillingAdjustmentConflict)
		}
	case BillingAdjustmentSourceSubscription:
		if r.SubscriptionID <= 0 {
			return errors.New("billing adjustment subscription is empty")
		}
	default:
		return fmt.Errorf("unsupported billing adjustment source %q", r.Source)
	}
	if r.TokenIncluded {
		if r.TokenID <= 0 {
			return errors.New("billing adjustment token is empty")
		}
		if r.TokenDelta != r.FundingDelta {
			return fmt.Errorf("%w: token delta %d funding delta %d", ErrBillingAdjustmentConflict, r.TokenDelta, r.FundingDelta)
		}
	} else if r.TokenDelta != 0 {
		return fmt.Errorf("%w: token delta must be zero when token is excluded", ErrBillingAdjustmentConflict)
	}
	return nil
}

func (r BillingAdjustmentRequest) same(row *BillingAdjustment) bool {
	if row == nil {
		return false
	}
	return row.UserID == r.UserID &&
		row.TokenID == r.TokenID &&
		row.Source == r.Source &&
		row.SubscriptionID == r.SubscriptionID &&
		row.FundingDelta == r.FundingDelta &&
		row.TokenDelta == r.TokenDelta &&
		row.ChargeQuota == r.ChargeQuota &&
		row.ReservationQuota == r.ReservationQuota &&
		row.ExtraReserved == r.ExtraReserved &&
		row.TokenIncluded == r.TokenIncluded
}

// ApplyBillingAdjustment commits the settle delta once. A second call with the
// same payload does not move quota. appliedNow is false on that replay.
func ApplyBillingAdjustment(req BillingAdjustmentRequest) (bool, error) {
	if err := req.validate(); err != nil {
		return false, err
	}
	if err := EnsureBillingAdjustmentSchema(DB); err != nil {
		return false, err
	}
	if err := EnsureWalletPreConsumeSchema(DB); err != nil {
		return false, err
	}
	appliedNow := false
	invalidateWalletUser := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		row, err := lockOrCreateBillingAdjustment(tx, req)
		if err != nil {
			return err
		}
		if !req.same(row) {
			return ErrBillingAdjustmentConflict
		}
		switch row.Status {
		case BillingAdjustmentApplied:
			return nil
		case BillingAdjustmentRolledBack:
			return ErrBillingAdjustmentClosed
		case BillingAdjustmentPending:
			if err := applyBillingAdjustmentDeltas(tx, row); err != nil {
				return err
			}
			if row.WalletCacheInvalidate {
				invalidateWalletUser = row.UserID
			}
			row.Status = BillingAdjustmentApplied
			row.UpdatedAt = common.GetTimestamp()
			if err := tx.Save(row).Error; err != nil {
				return err
			}
			appliedNow = true
			return nil
		default:
			return fmt.Errorf("unknown billing adjustment status %q", row.Status)
		}
	})
	if err != nil {
		return false, err
	}
	if appliedNow {
		cacheReq := req
		if invalidateWalletUser > 0 {
			_ = InvalidateUserCache(invalidateWalletUser)
			cacheReq.FundingDelta = 0
		}
		applyBillingAdjustmentCache(cacheReq)
	}
	return appliedNow, nil
}

// CollectDeliveredBillingCharge settles a response that was already sent.
// An applied row is kept as-is, even when this request's amount differs.
// A rolled-back row has already returned its reservation, so this request's
// delta is applied once and the row becomes applied. A missing or pending
// row uses the normal one-time settle.
func CollectDeliveredBillingCharge(req BillingAdjustmentRequest) (charged int, applied bool, err error) {
	if err := req.validate(); err != nil {
		return 0, false, err
	}
	if err := EnsureBillingAdjustmentSchema(DB); err != nil {
		return 0, false, err
	}
	if err := EnsureWalletPreConsumeSchema(DB); err != nil {
		return 0, false, err
	}
	var cache *BillingAdjustmentRequest
	invalidateWalletUser := 0
	err = DB.Transaction(func(tx *gorm.DB) error {
		var row BillingAdjustment
		err := lockForUpdate(tx).Where("idempotency_key = ?", req.IdempotencyKey).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			created, err := lockOrCreateBillingAdjustment(tx, req)
			if err != nil {
				return err
			}
			row = *created
		} else if err != nil {
			return err
		}
		switch row.Status {
		case BillingAdjustmentApplied:
			charged = row.ChargeQuota
			return nil
		case BillingAdjustmentPending:
			if !req.same(&row) {
				return ErrBillingAdjustmentConflict
			}
			if err := applyBillingAdjustmentDeltas(tx, &row); err != nil {
				return err
			}
			if row.WalletCacheInvalidate {
				invalidateWalletUser = row.UserID
			}
			row.Status = BillingAdjustmentApplied
			row.UpdatedAt = common.GetTimestamp()
			if err := tx.Save(&row).Error; err != nil {
				return err
			}
			charged = row.ChargeQuota
			applied = true
			copied := req
			cache = &copied
			return nil
		case BillingAdjustmentRolledBack:
			if err := applyDeliveredChargeToRolledBackTx(tx, &row, req); err != nil {
				return err
			}
			if row.WalletCacheInvalidate {
				invalidateWalletUser = row.UserID
			}
			charged = row.ChargeQuota
			applied = true
			copied := req
			cache = &copied
			return nil
		default:
			return fmt.Errorf("unknown billing adjustment status %q", row.Status)
		}
	})
	if err != nil {
		return 0, false, err
	}
	if cache != nil {
		if invalidateWalletUser > 0 {
			_ = InvalidateUserCache(invalidateWalletUser)
			cache.FundingDelta = 0
		}
		applyBillingAdjustmentCache(*cache)
	}
	return charged, applied, nil
}

// applyDeliveredChargeToRolledBackTx applies the current request delta.
// The earlier rollback already returned the old reservation. Charging the
// full amount again would stack on a pre-consume that this request still holds.
func applyDeliveredChargeToRolledBackTx(tx *gorm.DB, row *BillingAdjustment, req BillingAdjustmentRequest) error {
	if row == nil {
		return errors.New("billing adjustment is nil")
	}
	row.RequestID = req.RequestID
	row.UserID = req.UserID
	row.TokenID = req.TokenID
	row.Source = req.Source
	row.SubscriptionID = req.SubscriptionID
	row.FundingDelta = req.FundingDelta
	row.TokenDelta = req.TokenDelta
	row.ChargeQuota = req.ChargeQuota
	row.ReservationQuota = req.ReservationQuota
	row.ExtraReserved = req.ExtraReserved
	row.TokenIncluded = req.TokenIncluded
	row.RefundedQuota = 0
	if row.Source == BillingAdjustmentSourceSubscription {
		if err := reopenSubscriptionPreConsumeForDeliveredChargeTx(tx, row.RequestID, int64(row.ChargeQuota)); err != nil {
			return err
		}
	}
	if err := applyBillingAdjustmentDeltas(tx, row); err != nil {
		return err
	}
	row.Status = BillingAdjustmentApplied
	row.UpdatedAt = common.GetTimestamp()
	return tx.Save(row).Error
}

func reopenSubscriptionPreConsumeForDeliveredChargeTx(tx *gorm.DB, requestID string, amount int64) error {
	if tx == nil {
		return errors.New("database transaction is required")
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil
	}
	var record SubscriptionPreConsumeRecord
	err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	record.Status = "consumed"
	record.PreConsumed = amount
	record.PostResetReserved = 0
	return tx.Save(&record).Error
}

// RollbackBillingAdjustment reverses an applied charge once, or cancels a
// pending reservation. actualQuota must match the stored charge when non-zero.
// A missing row returns ErrBillingAdjustmentAbsent and moves no money.
func RollbackBillingAdjustment(key string, actualQuota int) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("billing adjustment key is empty")
	}
	if actualQuota < 0 {
		return fmt.Errorf("actual quota cannot be negative: %d", actualQuota)
	}
	if err := EnsureBillingAdjustmentSchema(DB); err != nil {
		return err
	}
	if err := EnsureWalletPreConsumeSchema(DB); err != nil {
		return err
	}
	var cache billingAdjustmentCache
	err := DB.Transaction(func(tx *gorm.DB) error {
		var row BillingAdjustment
		err := lockForUpdate(tx).Where("idempotency_key = ?", key).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrBillingAdjustmentAbsent
		}
		if err != nil {
			return err
		}
		if row.Status == BillingAdjustmentRolledBack {
			return nil
		}
		refundQuota := 0
		walletUserCredit := -1
		switch row.Status {
		case BillingAdjustmentPending:
			refundQuota = row.ReservationQuota
			credited, err := creditBillingReservation(tx, &row)
			if err != nil {
				return err
			}
			if row.Source == BillingAdjustmentSourceWallet {
				walletUserCredit = credited
			}
		case BillingAdjustmentApplied:
			if actualQuota > 0 && actualQuota != row.ChargeQuota {
				return fmt.Errorf("%w: actual %d charge %d", ErrBillingAdjustmentConflict, actualQuota, row.ChargeQuota)
			}
			refundQuota = row.ChargeQuota
			if err := creditBillingCharge(tx, &row); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown billing adjustment status %q", row.Status)
		}
		row.Status = BillingAdjustmentRolledBack
		row.RefundedQuota = refundQuota
		row.UpdatedAt = common.GetTimestamp()
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		cache = billingAdjustmentCacheFrom(&row, refundQuota, true)
		if walletUserCredit >= 0 {
			cache.userDelta = int64(walletUserCredit)
		}
		return nil
	})
	if err != nil {
		return err
	}
	applyBillingRollbackCache(cache)
	return nil
}

// BillingReservationRefund credits a pre-settle reservation once.
type BillingReservationRefund struct {
	IdempotencyKey   string
	RequestID        string
	UserID           int
	TokenID          int
	TokenKey         string
	Source           string
	SubscriptionID   int
	ReservationQuota int
	ExtraReserved    int
	TokenIncluded    bool
}

// RefundBillingReservation records a rolled-back reservation when settle never
// started. An applied row is left untouched and returns ErrBillingAdjustmentClosed.
func RefundBillingReservation(req BillingReservationRefund) error {
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	if req.IdempotencyKey == "" {
		return errors.New("billing adjustment key is empty")
	}
	if req.ReservationQuota < 0 || req.ExtraReserved < 0 {
		return errors.New("billing adjustment quota cannot be negative")
	}
	if err := EnsureBillingAdjustmentSchema(DB); err != nil {
		return err
	}
	if err := EnsureWalletPreConsumeSchema(DB); err != nil {
		return err
	}
	var cache billingAdjustmentCache
	err := DB.Transaction(func(tx *gorm.DB) error {
		var row BillingAdjustment
		err := lockForUpdate(tx).Where("idempotency_key = ?", req.IdempotencyKey).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			now := common.GetTimestamp()
			row = BillingAdjustment{
				IdempotencyKey:         req.IdempotencyKey,
				RequestID:              strings.TrimSpace(req.RequestID),
				UserID:                 req.UserID,
				TokenID:                req.TokenID,
				Source:                 req.Source,
				SubscriptionID:         req.SubscriptionID,
				ReservationQuota:       req.ReservationQuota,
				ExtraReserved:          req.ExtraReserved,
				SubscriptionConsumedAt: subscriptionPreConsumeCreatedAtTx(tx, req.RequestID),
				TokenIncluded:          req.TokenIncluded,
				Status:                 BillingAdjustmentPending,
				CreatedAt:              now,
				UpdatedAt:              now,
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		switch row.Status {
		case BillingAdjustmentRolledBack:
			return nil
		case BillingAdjustmentApplied:
			return ErrBillingAdjustmentClosed
		case BillingAdjustmentPending:
			walletCredited, err := creditBillingReservation(tx, &row)
			if err != nil {
				return err
			}
			row.Status = BillingAdjustmentRolledBack
			row.RefundedQuota = row.ReservationQuota
			row.UpdatedAt = common.GetTimestamp()
			if err := tx.Save(&row).Error; err != nil {
				return err
			}
			cache = billingAdjustmentCacheFrom(&row, row.RefundedQuota, true)
			if row.Source == BillingAdjustmentSourceWallet {
				cache.userDelta = int64(walletCredited)
			}
			return nil
		default:
			return fmt.Errorf("unknown billing adjustment status %q", row.Status)
		}
	})
	if err != nil {
		return err
	}
	applyBillingRollbackCache(cache)
	return nil
}

// GetBillingAdjustment loads one ledger row.
func GetBillingAdjustment(key string) (*BillingAdjustment, bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, false, errors.New("billing adjustment key is empty")
	}
	if err := EnsureBillingAdjustmentSchema(DB); err != nil {
		return nil, false, err
	}
	var row BillingAdjustment
	err := DB.Where("idempotency_key = ?", key).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &row, true, nil
}

// BillingAdjustmentRetryable reports a failure that can be stored and applied again.
// 余额不足和账本冲突不能重试；数据库抖动可以留下 pending 行以后补记。
func BillingAdjustmentRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrBillingAdjustmentConflict) ||
		errors.Is(err, ErrBillingAdjustmentClosed) ||
		errors.Is(err, ErrBillingAdjustmentAbsent) ||
		errors.Is(err, ErrBillingAdjustmentDeferred) ||
		IsTokenQuotaInsufficientError(err) ||
		IsTokenQuotaNoRowsError(err) {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "cannot be negative"),
		strings.Contains(msg, "unsupported billing"),
		strings.Contains(msg, "quota cannot be negative"),
		strings.Contains(msg, "subscription used exceeds total"),
		strings.Contains(msg, "invalid user"),
		strings.Contains(msg, "database is required"),
		strings.Contains(msg, "database transaction is required"):
		return false
	default:
		return true
	}
}

// EnsurePendingBillingAdjustment stores the charge without moving money.
// 同一次请求的相同内容重复写入是空操作。
func EnsurePendingBillingAdjustment(req BillingAdjustmentRequest) error {
	if BillingAdjustmentBeforeEnsurePending != nil {
		if hookErr := BillingAdjustmentBeforeEnsurePending(); hookErr != nil {
			return hookErr
		}
	}
	if err := req.validate(); err != nil {
		return err
	}
	if err := EnsureBillingAdjustmentSchema(DB); err != nil {
		return err
	}
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		err = DB.Transaction(func(tx *gorm.DB) error {
			row, err := lockOrCreateBillingAdjustment(tx, req)
			if err != nil {
				return err
			}
			if !req.same(row) {
				return ErrBillingAdjustmentConflict
			}
			switch row.Status {
			case BillingAdjustmentPending, BillingAdjustmentApplied:
				return nil
			case BillingAdjustmentRolledBack:
				return ErrBillingAdjustmentClosed
			default:
				return fmt.Errorf("unknown billing adjustment status %q", row.Status)
			}
		})
		if err == nil || !errors.Is(err, gorm.ErrDuplicatedKey) {
			break
		}
	}
	if err == nil && BillingAdjustmentAfterEnsurePending != nil {
		if hookErr := BillingAdjustmentAfterEnsurePending(); hookErr != nil {
			return hookErr
		}
	}
	return err
}

func RecoverPendingBillingAdjustments(limit int) error {
	if err := EnsureBillingAdjustmentSchema(DB); err != nil {
		return err
	}
	if limit <= 0 {
		limit = 100
	}
	var rows []BillingAdjustment
	if err := DB.Where("status = ?", BillingAdjustmentPending).Order("id asc").Limit(limit).Find(&rows).Error; err != nil {
		return err
	}
	var first error
	for i := range rows {
		req := billingAdjustmentRequestFromRow(&rows[i])
		if _, err := ApplyBillingAdjustment(req); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func billingAdjustmentRequestFromRow(row *BillingAdjustment) BillingAdjustmentRequest {
	if row == nil {
		return BillingAdjustmentRequest{}
	}
	return BillingAdjustmentRequest{
		IdempotencyKey:   row.IdempotencyKey,
		RequestID:        row.RequestID,
		UserID:           row.UserID,
		TokenID:          row.TokenID,
		Source:           row.Source,
		SubscriptionID:   row.SubscriptionID,
		FundingDelta:     row.FundingDelta,
		TokenDelta:       row.TokenDelta,
		ChargeQuota:      row.ChargeQuota,
		ReservationQuota: row.ReservationQuota,
		ExtraReserved:    row.ExtraReserved,
		TokenIncluded:    row.TokenIncluded,
	}
}

func lockOrCreateBillingAdjustment(tx *gorm.DB, req BillingAdjustmentRequest) (*BillingAdjustment, error) {
	var row BillingAdjustment
	err := lockForUpdate(tx).Where("idempotency_key = ?", req.IdempotencyKey).First(&row).Error
	if err == nil {
		return &row, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	now := common.GetTimestamp()
	consumedAt := int64(0)
	if req.Source == BillingAdjustmentSourceSubscription {
		consumedAt = subscriptionPreConsumeCreatedAtTx(tx, req.RequestID)
	}
	row = BillingAdjustment{
		IdempotencyKey:         req.IdempotencyKey,
		RequestID:              strings.TrimSpace(req.RequestID),
		UserID:                 req.UserID,
		TokenID:                req.TokenID,
		Source:                 req.Source,
		SubscriptionID:         req.SubscriptionID,
		FundingDelta:           req.FundingDelta,
		TokenDelta:             req.TokenDelta,
		ChargeQuota:            req.ChargeQuota,
		ReservationQuota:       req.ReservationQuota,
		ExtraReserved:          req.ExtraReserved,
		SubscriptionConsumedAt: consumedAt,
		TokenIncluded:          req.TokenIncluded,
		Status:                 BillingAdjustmentPending,
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	if err := tx.Create(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func applyBillingAdjustmentDeltas(tx *gorm.DB, row *BillingAdjustment) error {
	if row == nil {
		return errors.New("billing adjustment is nil")
	}
	if err := applyFundingDelta(tx, row, row.FundingDelta); err != nil {
		return err
	}
	if !row.TokenIncluded || row.TokenDelta == 0 {
		return nil
	}
	var err error
	if row.TokenDelta > 0 {
		// 已经交付的请求允许令牌透支，避免余额不足时停在预扣。
		err = DecreaseTokenQuotaAllowNegativeTx(tx, row.TokenID, int64(row.TokenDelta))
	} else {
		err = IncreaseTokenQuotaTx(tx, row.TokenID, int64(-row.TokenDelta))
	}
	// 令牌已经删除时仍然结算钱包或订阅，不能因为令牌行不在就把差额留在预扣。
	if IsTokenQuotaNoRowsError(err) {
		return nil
	}
	return err
}

func applyFundingDelta(tx *gorm.DB, row *BillingAdjustment, delta int) error {
	if row == nil {
		return errors.New("billing adjustment is nil")
	}
	if row.Source == BillingAdjustmentSourceWallet {
		return applyWalletFundingDelta(tx, row, delta)
	}
	// 订阅差额为 0 仍可能要在重置后的新周期补记实际用量，不能在这里直接返回。
	if row.Source != BillingAdjustmentSourceSubscription && delta == 0 {
		return nil
	}
	switch row.Source {
	case BillingAdjustmentSourceSubscription:
		return ApplySubscriptionLedgerChargeTx(tx, row.SubscriptionID, delta, row.ChargeQuota, subscriptionLedgerConsumedAtTx(tx, row), row.RequestID)
	default:
		return fmt.Errorf("unsupported billing adjustment source %q", row.Source)
	}
}

func subscriptionLedgerConsumedAtTx(tx *gorm.DB, row *BillingAdjustment) int64 {
	if row == nil {
		return 0
	}
	if row.SubscriptionConsumedAt > 0 {
		return row.SubscriptionConsumedAt
	}
	if at := subscriptionPreConsumeCreatedAtTx(tx, row.RequestID); at > 0 {
		return at
	}
	return row.CreatedAt
}

func creditBillingReservation(tx *gorm.DB, row *BillingAdjustment) (int, error) {
	if row == nil {
		return 0, errors.New("billing adjustment is nil")
	}
	walletCredited := row.ReservationQuota
	switch row.Source {
	case BillingAdjustmentSourceWallet:
		credited, err := creditWalletReservationOnceTx(tx, row)
		if err != nil {
			return 0, err
		}
		walletCredited = credited
	case BillingAdjustmentSourceSubscription:
		err := refundSubscriptionPreConsumeTx(tx, row.RequestID)
		missingRecord := errors.Is(err, gorm.ErrRecordNotFound)
		if err != nil && !missingRecord {
			return 0, err
		}
		consumedAt := subscriptionRefundConsumedAt(row)
		if !missingRecord {
			if createdAt := subscriptionPreConsumeCreatedAtTx(tx, row.RequestID); createdAt > 0 {
				consumedAt = createdAt
			}
		}
		posted, postedErr := subscriptionPostResetReservedTx(tx, row.RequestID)
		if postedErr != nil {
			return 0, postedErr
		}
		if posted > int64(row.ExtraReserved) {
			return 0, fmt.Errorf("%w: post-reset reserve %d extra %d", ErrBillingAdjustmentConflict, posted, row.ExtraReserved)
		}
		preResetExtra := int64(row.ExtraReserved) - posted
		if missingRecord {
			preConsumed := row.ReservationQuota - row.ExtraReserved
			if preConsumed < 0 {
				return 0, fmt.Errorf("%w: reservation %d extra %d", ErrBillingAdjustmentConflict, row.ReservationQuota, row.ExtraReserved)
			}
			if err := refundSubscriptionUsageIfCurrentPeriodTx(tx, row.SubscriptionID, int64(preConsumed), consumedAt); err != nil {
				return 0, err
			}
		}
		if preResetExtra > 0 {
			if err := refundSubscriptionUsageIfCurrentPeriodTx(tx, row.SubscriptionID, preResetExtra, consumedAt); err != nil {
				return 0, err
			}
		}
		if posted > 0 {
			if err := refundSubscriptionUsageIfCurrentPeriodTx(tx, row.SubscriptionID, posted, 0); err != nil {
				return 0, err
			}
			if err := clearSubscriptionPostResetReservedTx(tx, row.RequestID); err != nil {
				return 0, err
			}
		}
	default:
		return 0, fmt.Errorf("unsupported billing adjustment source %q", row.Source)
	}
	if err := creditTokenAmount(tx, row, row.ReservationQuota); err != nil {
		return 0, err
	}
	if row.Source == BillingAdjustmentSourceWallet {
		return walletCredited, nil
	}
	return row.ReservationQuota, nil
}

func creditWalletReservationOnceTx(tx *gorm.DB, row *BillingAdjustment) (int, error) {
	if row == nil {
		return 0, errors.New("billing adjustment is nil")
	}
	record, err := lockWalletPreConsumeTx(tx, row.RequestID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if err := creditWallet(tx, row.UserID, row.ReservationQuota); err != nil {
			return 0, err
		}
		return row.ReservationQuota, nil
	}
	if err != nil {
		return 0, err
	}
	switch record.Status {
	case WalletPreConsumeRefunded:
		return 0, nil
	case WalletPreConsumeReserved:
		amount := int(record.Amount)
		if amount > 0 {
			if err := creditWallet(tx, row.UserID, amount); err != nil {
				return 0, err
			}
		}
		if err := markWalletPreConsumeRefundedTx(tx, row.RequestID); err != nil {
			return 0, err
		}
		return amount, nil
	case WalletPreConsumeTrusted:
		amount := int(record.Collected)
		if amount > 0 {
			if err := creditWallet(tx, row.UserID, amount); err != nil {
				return 0, err
			}
		}
		if err := markWalletPreConsumeRefundedTx(tx, row.RequestID); err != nil {
			return 0, err
		}
		return amount, nil
	case WalletPreConsumeSettled:
		amount := int(record.Collected)
		if amount <= 0 {
			amount = int(record.Amount)
		}
		if amount > 0 {
			if err := creditWallet(tx, row.UserID, amount); err != nil {
				return 0, err
			}
		}
		record.Status = WalletPreConsumeRefunded
		record.UpdatedAt = common.GetTimestamp()
		if err := tx.Save(record).Error; err != nil {
			return 0, err
		}
		return amount, nil
	default:
		return 0, fmt.Errorf("wallet pre-consume %s has status %q", row.RequestID, record.Status)
	}
}

func creditBillingCharge(tx *gorm.DB, row *BillingAdjustment) error {
	if row == nil {
		return errors.New("billing adjustment is nil")
	}
	switch row.Source {
	case BillingAdjustmentSourceWallet:
		if err := creditWallet(tx, row.UserID, row.ChargeQuota); err != nil {
			return err
		}
	case BillingAdjustmentSourceSubscription:
		err := rollbackSubscriptionPreConsumeSettlementTx(tx, row.RequestID, int64(row.ChargeQuota))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := refundSubscriptionUsageIfCurrentPeriodTx(tx, row.SubscriptionID, int64(row.ChargeQuota), subscriptionRefundConsumedAt(row)); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if err := refundLedgerChargeAppliedAfterResetTx(tx, row); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported billing adjustment source %q", row.Source)
	}
	return creditTokenAmount(tx, row, row.ChargeQuota)
}

func creditWallet(tx *gorm.DB, userID int, amount int) error {
	if amount == 0 {
		return nil
	}
	if amount < 0 {
		return errors.New("billing adjustment credit cannot be negative")
	}
	return IncreaseUserQuotaTx(tx, userID, int64(amount))
}

func creditTokenAmount(tx *gorm.DB, row *BillingAdjustment, amount int) error {
	if row == nil || !row.TokenIncluded || amount == 0 || row.TokenID <= 0 {
		return nil
	}
	err := IncreaseTokenQuotaTx(tx, row.TokenID, int64(amount))
	if IsTokenQuotaNoRowsError(err) {
		return nil
	}
	return err
}

type billingAdjustmentCache struct {
	moved     bool
	userID    int
	userDelta int64
	tokenID   int
	tokenKey  string
	tokenAdd  int64
	source    string
}

func billingAdjustmentCacheFrom(row *BillingAdjustment, refunded int, credit bool) billingAdjustmentCache {
	if row == nil || !credit || refunded == 0 {
		return billingAdjustmentCache{}
	}
	cache := billingAdjustmentCache{moved: true, source: row.Source, userID: row.UserID}
	if row.Source == BillingAdjustmentSourceWallet {
		cache.userDelta = int64(refunded)
	}
	if row.TokenIncluded && row.TokenID > 0 {
		cache.tokenID = row.TokenID
		cache.tokenAdd = int64(refunded)
	}
	return cache
}

func applyBillingAdjustmentCache(req BillingAdjustmentRequest) {
	if req.Source == BillingAdjustmentSourceWallet && req.FundingDelta != 0 {
		applyUserQuotaCacheDeltaBestEffort(req.UserID, int64(-req.FundingDelta))
	}
	if !req.TokenIncluded || req.TokenDelta == 0 || req.TokenID <= 0 {
		return
	}
	if req.TokenDelta > 0 {
		applyTokenQuotaCacheDelta(tokenQuotaDeltaAfterDecrease(req.TokenID, req.TokenKey, int64(req.TokenDelta)))
		return
	}
	applyTokenQuotaCacheDelta(tokenQuotaDeltaAfterIncrease(req.TokenID, req.TokenKey, int64(-req.TokenDelta)))
}

func applyBillingRollbackCache(cache billingAdjustmentCache) {
	if !cache.moved {
		return
	}
	if cache.source == BillingAdjustmentSourceWallet && cache.userDelta != 0 {
		applyUserQuotaCacheDeltaBestEffort(cache.userID, cache.userDelta)
	}
	if cache.tokenID > 0 && cache.tokenAdd != 0 {
		applyTokenQuotaCacheDelta(tokenQuotaDeltaAfterIncrease(cache.tokenID, cache.tokenKey, cache.tokenAdd))
	}
}
