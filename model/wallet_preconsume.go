package model

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"gorm.io/gorm"
)

const (
	WalletPreConsumeReserved = "reserved"
	WalletPreConsumeTrusted  = "trusted"
	WalletPreConsumeSettled  = "settled"
	WalletPreConsumeRefunded = "refunded"

	walletPreConsumeLeaseSeconds int64 = 120
)

var (
	// ErrWalletPreConsumeInsufficient means the wallet could not fund this reserve.
	ErrWalletPreConsumeInsufficient = errors.New("wallet pre-consume quota insufficient")
	// ErrWalletPreConsumeNotFound means this request never wrote a reserve row.
	ErrWalletPreConsumeNotFound = errors.New("wallet pre-consume record not found")
	// ErrWalletPreConsumeClosed means the reserve was already settled or refunded.
	ErrWalletPreConsumeClosed = errors.New("wallet pre-consume is closed")
	// ErrWalletPreConsumeConflict means the same request id was reused for a different reserve.
	ErrWalletPreConsumeConflict = errors.New("wallet pre-consume conflict")
	// ErrWalletPreConsumeLeaseActive means expiry recovery lost the race to a live request.
	ErrWalletPreConsumeLeaseActive = errors.New("wallet pre-consume lease is active")

	walletPreConsumeSchemaMu    sync.Mutex
	walletPreConsumeSchemaReady sync.Map
)

// WalletPreConsumeRecord is the request-scoped wallet reserve. It closes the
// crash window between the wallet debit and the billing adjustment row.
type WalletPreConsumeRecord struct {
	ID              int64  `json:"id" gorm:"primaryKey"`
	RequestID       string `json:"request_id" gorm:"size:191;uniqueIndex;not null"`
	UserID          int    `json:"user_id" gorm:"index;not null"`
	Amount          int64  `json:"amount" gorm:"not null"`
	Collected       int64  `json:"collected" gorm:"not null;default:0"`
	Status          string `json:"status" gorm:"size:32;index;not null"`
	ClientDelivered bool   `json:"client_delivered" gorm:"not null;default:false"`
	LeaseUntil      int64  `json:"lease_until" gorm:"index;not null"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
}

func (WalletPreConsumeRecord) TableName() string {
	return "wallet_pre_consume_records"
}

// EnsureWalletPreConsumeSchema creates the reserve table on a database that
// has not run the startup migration yet.
func EnsureWalletPreConsumeSchema(db *gorm.DB) error {
	if db == nil {
		return errors.New("database is required")
	}
	if _, ok := walletPreConsumeSchemaReady.Load(db); ok {
		return nil
	}
	walletPreConsumeSchemaMu.Lock()
	defer walletPreConsumeSchemaMu.Unlock()
	if _, ok := walletPreConsumeSchemaReady.Load(db); ok {
		return nil
	}
	if err := db.AutoMigrate(&WalletPreConsumeRecord{}); err != nil {
		return err
	}
	walletPreConsumeSchemaReady.Store(db, true)
	return nil
}

// ReserveWalletPreConsume debits the wallet and inserts one reserved row.
// The same request, user and amount only refreshes the lease.
func ReserveWalletPreConsume(requestID string, userID int, amount int64) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return errors.New("wallet pre-consume request id is empty")
	}
	if userID <= 0 {
		return errors.New("invalid user")
	}
	if amount <= 0 {
		return errors.New("wallet pre-consume amount must be positive")
	}
	if err := EnsureWalletPreConsumeSchema(DB); err != nil {
		return err
	}
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		var cacheApplied bool
		var cacheAmount int64
		err = DB.Transaction(func(tx *gorm.DB) error {
			debited, txErr := reserveWalletPreConsumeTx(tx, requestID, userID, amount)
			if txErr != nil {
				return txErr
			}
			if debited <= 0 {
				return nil
			}
			applied, syncErr := syncDebitedUserQuotaCache(userID, debited)
			if syncErr != nil {
				return syncErr
			}
			cacheApplied = applied
			cacheAmount = debited
			return nil
		})
		if err != nil {
			restoreDebitedUserQuotaCache(userID, cacheAmount, cacheApplied)
		}
		if err == nil || !isWalletPreConsumeDuplicate(err) {
			break
		}
	}
	return err
}

func reserveWalletPreConsumeTx(tx *gorm.DB, requestID string, userID int, amount int64) (int64, error) {
	record, err := lockWalletPreConsumeTx(tx, requestID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if err := DecreaseUserQuotaTx(tx, userID, amount); err != nil {
			if walletQuotaUpdateRejected(err) {
				return 0, ErrWalletPreConsumeInsufficient
			}
			return 0, err
		}
		now := getDBTimestampTx(tx)
		record = &WalletPreConsumeRecord{
			RequestID:  requestID,
			UserID:     userID,
			Amount:     amount,
			Status:     WalletPreConsumeReserved,
			LeaseUntil: now + walletPreConsumeLeaseSeconds,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := tx.Create(record).Error; err != nil {
			return 0, err
		}
		return amount, nil
	}
	if err != nil {
		return 0, err
	}
	if record.UserID != userID {
		return 0, ErrWalletPreConsumeConflict
	}
	if record.Status != WalletPreConsumeReserved {
		return 0, ErrWalletPreConsumeClosed
	}
	if record.Amount != amount {
		return 0, fmt.Errorf("%w: stored=%d requested=%d", ErrWalletPreConsumeConflict, record.Amount, amount)
	}
	now := getDBTimestampTx(tx)
	record.LeaseUntil = now + walletPreConsumeLeaseSeconds
	record.UpdatedAt = now
	return 0, tx.Save(record).Error
}

// ReserveTrustedWalletPreConsume records the estimate without debiting the wallet.
// A delivered row whose process dies before settlement is charged this estimate.
// An undelivered or explicitly failed request is closed without a charge.
func ReserveTrustedWalletPreConsume(requestID string, userID int, amount int64) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return errors.New("wallet pre-consume request id is empty")
	}
	if userID <= 0 {
		return errors.New("invalid user")
	}
	if amount <= 0 {
		return errors.New("wallet pre-consume amount must be positive")
	}
	if err := EnsureWalletPreConsumeSchema(DB); err != nil {
		return err
	}
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		err = DB.Transaction(func(tx *gorm.DB) error {
			return reserveTrustedWalletPreConsumeTx(tx, requestID, userID, amount)
		})
		if err == nil || !isWalletPreConsumeDuplicate(err) {
			break
		}
	}
	return err
}

func reserveTrustedWalletPreConsumeTx(tx *gorm.DB, requestID string, userID int, amount int64) error {
	record, err := lockWalletPreConsumeTx(tx, requestID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		now := getDBTimestampTx(tx)
		return tx.Create(&WalletPreConsumeRecord{
			RequestID:  requestID,
			UserID:     userID,
			Amount:     amount,
			Status:     WalletPreConsumeTrusted,
			LeaseUntil: now + walletPreConsumeLeaseSeconds,
			CreatedAt:  now,
			UpdatedAt:  now,
		}).Error
	}
	if err != nil {
		return err
	}
	if record.UserID != userID || record.Amount != amount || record.Status != WalletPreConsumeTrusted {
		if record.Status != WalletPreConsumeTrusted {
			return ErrWalletPreConsumeClosed
		}
		return fmt.Errorf("%w: stored=%d requested=%d", ErrWalletPreConsumeConflict, record.Amount, amount)
	}
	now := getDBTimestampTx(tx)
	record.LeaseUntil = now + walletPreConsumeLeaseSeconds
	record.UpdatedAt = now
	return tx.Save(record).Error
}

// IncreaseWalletPreConsume adds an extra reserve onto a still-open row.
func IncreaseWalletPreConsume(requestID string, userID int, delta int64) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return ErrWalletPreConsumeNotFound
	}
	if userID <= 0 {
		return errors.New("invalid user")
	}
	if delta <= 0 {
		return errors.New("wallet pre-consume amount must be positive")
	}
	if err := EnsureWalletPreConsumeSchema(DB); err != nil {
		return err
	}
	var cacheApplied bool
	err := DB.Transaction(func(tx *gorm.DB) error {
		record, err := lockWalletPreConsumeTx(tx, requestID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrWalletPreConsumeNotFound
		}
		if err != nil {
			return err
		}
		if record.UserID != userID {
			return ErrWalletPreConsumeConflict
		}
		if record.Status == WalletPreConsumeTrusted {
			if err := DecreaseUserQuotaTx(tx, userID, delta); err != nil {
				if walletQuotaUpdateRejected(err) {
					return ErrWalletPreConsumeInsufficient
				}
				return err
			}
			now := getDBTimestampTx(tx)
			record.Amount = delta
			record.Status = WalletPreConsumeReserved
			record.LeaseUntil = now + walletPreConsumeLeaseSeconds
			record.UpdatedAt = now
			if err := tx.Save(record).Error; err != nil {
				return err
			}
			applied, syncErr := syncDebitedUserQuotaCache(userID, delta)
			if syncErr != nil {
				return syncErr
			}
			cacheApplied = applied
			return nil
		}
		if record.Status != WalletPreConsumeReserved {
			return ErrWalletPreConsumeClosed
		}
		if err := DecreaseUserQuotaTx(tx, userID, delta); err != nil {
			if walletQuotaUpdateRejected(err) {
				return ErrWalletPreConsumeInsufficient
			}
			return err
		}
		now := getDBTimestampTx(tx)
		record.Amount += delta
		record.LeaseUntil = now + walletPreConsumeLeaseSeconds
		record.UpdatedAt = now
		if err := tx.Save(record).Error; err != nil {
			return err
		}
		applied, syncErr := syncDebitedUserQuotaCache(userID, delta)
		if syncErr != nil {
			return syncErr
		}
		cacheApplied = applied
		return nil
	})
	if err != nil {
		restoreDebitedUserQuotaCache(userID, delta, cacheApplied)
		return err
	}
	return nil
}

// ReleaseWalletPreConsume returns part of an open reserve. A missing row is
// left to the caller so an older reserve can still be credited.
func ReleaseWalletPreConsume(requestID string, userID int, delta int64) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return ErrWalletPreConsumeNotFound
	}
	if delta <= 0 {
		return nil
	}
	if err := EnsureWalletPreConsumeSchema(DB); err != nil {
		return err
	}
	credited := int64(0)
	err := DB.Transaction(func(tx *gorm.DB) error {
		record, err := lockWalletPreConsumeTx(tx, requestID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrWalletPreConsumeNotFound
		}
		if err != nil {
			return err
		}
		if record.UserID != userID {
			return ErrWalletPreConsumeConflict
		}
		if record.Status != WalletPreConsumeReserved {
			return ErrWalletPreConsumeClosed
		}
		if record.Amount < delta {
			return fmt.Errorf("%w: stored=%d release=%d", ErrWalletPreConsumeConflict, record.Amount, delta)
		}
		if err := IncreaseUserQuotaTx(tx, userID, delta); err != nil {
			return err
		}
		now := getDBTimestampTx(tx)
		record.Amount -= delta
		record.UpdatedAt = now
		if record.Amount == 0 {
			record.Status = WalletPreConsumeRefunded
		} else {
			record.LeaseUntil = now + walletPreConsumeLeaseSeconds
		}
		credited = delta
		return tx.Save(record).Error
	})
	if err != nil {
		return err
	}
	if credited > 0 {
		applyUserQuotaCacheDeltaBestEffort(userID, credited)
	}
	return nil
}

// RefundWalletPreConsume returns the remaining reserve once.
// A settled row is left closed so a charge rollback cannot refund it again.
func RefundWalletPreConsume(requestID string) error {
	_, err := refundWalletPreConsume(requestID, false)
	return err
}

// RecoverExpiredWalletPreConsumes returns reserves whose lease was not refreshed.
// Pending billing adjustments must be applied before this runs.
func RecoverExpiredWalletPreConsumes(limit int) error {
	if DB == nil {
		return nil
	}
	if limit <= 0 {
		limit = 100
	}
	if err := EnsureWalletPreConsumeSchema(DB); err != nil {
		return err
	}
	now := getDBTimestampTx(DB)
	var rows []WalletPreConsumeRecord
	err := DB.Where("status IN ? AND lease_until > 0 AND lease_until <= ?", []string{WalletPreConsumeReserved, WalletPreConsumeTrusted}, now).
		Order("id asc").Limit(limit).Find(&rows).Error
	if err != nil {
		return err
	}
	var first error
	for _, row := range rows {
		if _, err := refundWalletPreConsume(row.RequestID, true); err != nil {
			if errors.Is(err, ErrWalletPreConsumeLeaseActive) || errors.Is(err, ErrWalletPreConsumeClosed) {
				continue
			}
			if first == nil {
				first = err
			}
		}
	}
	return first
}

// MarkWalletPreConsumeClientDelivered records that this request already wrote
// a successful response. Expiry recovery must not refund that reserve.
func MarkWalletPreConsumeClientDelivered(requestID string) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" || DB == nil {
		return nil
	}
	if err := EnsureWalletPreConsumeSchema(DB); err != nil {
		return err
	}
	now := getDBTimestampTx(DB)
	return DB.Model(&WalletPreConsumeRecord{}).
		Where("request_id = ? AND status IN ?", requestID, []string{WalletPreConsumeReserved, WalletPreConsumeTrusted}).
		Updates(map[string]any{
			"client_delivered": true,
			"updated_at":       now,
		}).Error
}

// RefreshWalletPreConsumeLease extends a live reserve. A closed or missing row
// tells the refresher to stop.
func RefreshWalletPreConsumeLease(requestID string) (bool, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" || DB == nil {
		return false, nil
	}
	if err := EnsureWalletPreConsumeSchema(DB); err != nil {
		return false, err
	}
	now := getDBTimestampTx(DB)
	result := DB.Model(&WalletPreConsumeRecord{}).
		Where("request_id = ? AND status IN ?", requestID, []string{WalletPreConsumeReserved, WalletPreConsumeTrusted}).
		Updates(map[string]any{
			"lease_until": now + walletPreConsumeLeaseSeconds,
			"updated_at":  now,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func refundWalletPreConsume(requestID string, requireExpired bool) (int64, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return 0, ErrWalletPreConsumeNotFound
	}
	if err := EnsureWalletPreConsumeSchema(DB); err != nil {
		return 0, err
	}
	credited := int64(0)
	userID := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		amount, owner, err := refundWalletPreConsumeTx(tx, requestID, requireExpired)
		if err != nil {
			return err
		}
		credited = amount
		userID = owner
		return nil
	})
	if err != nil {
		return 0, err
	}
	if credited > 0 && userID > 0 {
		applyUserQuotaCacheDeltaBestEffort(userID, credited)
	}
	return credited, nil
}

func walletBillingAdjustmentOwnsTx(tx *gorm.DB, requestID string) (bool, error) {
	if tx == nil || strings.TrimSpace(requestID) == "" {
		return false, nil
	}
	if !tx.Migrator().HasTable(&BillingAdjustment{}) {
		return false, nil
	}
	var count int64
	err := tx.Model(&BillingAdjustment{}).
		Where("request_id = ? AND status IN ?", requestID, []string{BillingAdjustmentPending, BillingAdjustmentApplied}).
		Count(&count).Error
	return count > 0, err
}

func refundWalletPreConsumeTx(tx *gorm.DB, requestID string, requireExpired bool) (int64, int, error) {
	record, err := lockWalletPreConsumeTx(tx, requestID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, 0, ErrWalletPreConsumeNotFound
	}
	if err != nil {
		return 0, 0, err
	}
	if record.Status == WalletPreConsumeRefunded {
		return 0, record.UserID, nil
	}
	now := getDBTimestampTx(tx)
	if requireExpired && (record.Status == WalletPreConsumeTrusted || record.Status == WalletPreConsumeReserved) {
		if record.LeaseUntil > now {
			return 0, record.UserID, ErrWalletPreConsumeLeaseActive
		}
		owns, err := walletBillingAdjustmentOwnsTx(tx, requestID)
		if err != nil {
			return 0, record.UserID, err
		}
		if owns {
			record.LeaseUntil = 0
			record.UpdatedAt = now
			if err := tx.Save(record).Error; err != nil {
				return 0, record.UserID, err
			}
			return 0, record.UserID, nil
		}
	}
	if record.Status == WalletPreConsumeTrusted {
		if requireExpired && record.LeaseUntil > now {
			return 0, record.UserID, ErrWalletPreConsumeLeaseActive
		}
		// Nothing was debited yet. Charge the estimate only when the response was
		// delivered and the process died. A later settlement subtracts this amount.
		if requireExpired && record.ClientDelivered {
			if record.Amount > 0 {
				if err := DecreaseUserQuotaAllowNegativeTx(tx, record.UserID, record.Amount); err != nil {
					return 0, record.UserID, err
				}
				if _, err := syncDebitedUserQuotaCache(record.UserID, record.Amount); err != nil {
					return 0, record.UserID, err
				}
			}
			record.Collected = record.Amount
			record.Status = WalletPreConsumeSettled
			record.UpdatedAt = now
			if err := tx.Save(record).Error; err != nil {
				return 0, record.UserID, err
			}
			return 0, record.UserID, nil
		}
		record.Status = WalletPreConsumeRefunded
		record.UpdatedAt = now
		if err := tx.Save(record).Error; err != nil {
			return 0, record.UserID, err
		}
		return 0, record.UserID, nil
	}
	if record.Status != WalletPreConsumeReserved {
		return 0, record.UserID, ErrWalletPreConsumeClosed
	}
	if requireExpired && record.LeaseUntil > now {
		return 0, record.UserID, ErrWalletPreConsumeLeaseActive
	}
	// The client already has the response. Keep the reserve instead of
	// refunding it when the process died before settlement committed.
	if requireExpired && record.ClientDelivered {
		record.Status = WalletPreConsumeSettled
		record.UpdatedAt = now
		if err := tx.Save(record).Error; err != nil {
			return 0, record.UserID, err
		}
		return 0, record.UserID, nil
	}
	credited := int64(0)
	if record.Amount > 0 {
		if err := IncreaseUserQuotaTx(tx, record.UserID, record.Amount); err != nil {
			return 0, record.UserID, err
		}
		credited = record.Amount
	}
	record.Status = WalletPreConsumeRefunded
	record.UpdatedAt = now
	if err := tx.Save(record).Error; err != nil {
		return 0, record.UserID, err
	}
	return credited, record.UserID, nil
}

// markWalletPreConsumeRefundedTx closes a reserve after the billing ledger has
// already credited the same amount. Settled rows stay settled.
func markWalletPreConsumeRefundedTx(tx *gorm.DB, requestID string) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" || tx == nil {
		return nil
	}
	record, err := lockWalletPreConsumeTx(tx, requestID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if record.Status != WalletPreConsumeReserved && record.Status != WalletPreConsumeTrusted {
		return nil
	}
	record.Status = WalletPreConsumeRefunded
	record.UpdatedAt = getDBTimestampTx(tx)
	return tx.Save(record).Error
}

// applyWalletFundingDelta closes the reserve and then applies the settle delta.
// A reserve that expiry recovery already returned is charged in full, unless
// this ledger row itself is the rolled-back redelivery path.
func applyWalletFundingDelta(tx *gorm.DB, row *BillingAdjustment, delta int) error {
	if row == nil {
		return errors.New("billing adjustment is nil")
	}
	fullCharge, collected, err := accountWalletPreConsumeTx(tx, row)
	if err != nil {
		return err
	}
	charge := delta
	if fullCharge {
		row.WalletCacheInvalidate = true
		charge = row.ChargeQuota
	} else if collected > 0 {
		// Expiry already took the trusted estimate. Charge only the difference.
		charge = row.ChargeQuota - int(collected)
	}
	if charge == 0 {
		return nil
	}
	if charge > 0 {
		return DecreaseUserQuotaAllowNegativeTx(tx, row.UserID, int64(charge))
	}
	return IncreaseUserQuotaTx(tx, row.UserID, int64(-charge))
}

func accountWalletPreConsumeTx(tx *gorm.DB, row *BillingAdjustment) (bool, int64, error) {
	requestID := ""
	if row != nil {
		requestID = strings.TrimSpace(row.RequestID)
	}
	if requestID == "" {
		return false, 0, nil
	}
	record, err := lockWalletPreConsumeTx(tx, requestID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	now := getDBTimestampTx(tx)
	switch record.Status {
	case WalletPreConsumeReserved, WalletPreConsumeTrusted:
		collected := record.Collected
		record.Status = WalletPreConsumeSettled
		record.UpdatedAt = now
		return false, collected, tx.Save(record).Error
	case WalletPreConsumeSettled:
		return false, record.Collected, nil
	case WalletPreConsumeRefunded:
		if row.Status == BillingAdjustmentRolledBack {
			return false, 0, nil
		}
		return true, 0, nil
	default:
		return false, 0, fmt.Errorf("unknown wallet pre-consume status %q", record.Status)
	}
}

func lockWalletPreConsumeTx(tx *gorm.DB, requestID string) (*WalletPreConsumeRecord, error) {
	var record WalletPreConsumeRecord
	err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&record).Error
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func isWalletPreConsumeDuplicate(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique") || strings.Contains(message, "duplicate")
}
