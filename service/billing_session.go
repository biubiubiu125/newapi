package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

func isSubscriptionPreConsumeInsufficientError(err error) bool {
	return errors.Is(err, model.ErrNoActiveSubscription) ||
		errors.Is(err, model.ErrNoActiveSubscriptionGrantsGroup) ||
		errors.Is(err, model.ErrSubscriptionQuotaInsufficient)
}

func insufficientUserQuotaProtocolError(remain int64) *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		errors.New(i18n.ProtocolMessage(i18n.MsgProtocolInsufficientUserQuota, map[string]any{"Quota": logger.FormatQuota(remain)})),
		types.ErrorCodeInsufficientUserQuota,
		http.StatusForbidden,
		types.ErrOptionWithSkipRetry(),
		types.ErrOptionWithNoRecordErrorLog(),
	)
}

func insufficientSubscriptionQuotaProtocolError() *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		errors.New(i18n.ProtocolMessage(i18n.MsgProtocolInsufficientSubscriptionQuota)),
		types.ErrorCodeInsufficientUserQuota,
		http.StatusForbidden,
		types.ErrOptionWithSkipRetry(),
		types.ErrOptionWithNoRecordErrorLog(),
	)
}

func preConsumeQuotaProtocolError(remain int64, required int) *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		errors.New(i18n.ProtocolMessage(i18n.MsgProtocolPreConsumeFailed, map[string]any{
			"Quota":    logger.FormatQuota(remain),
			"Required": logger.FormatQuota(required),
		})),
		types.ErrorCodeInsufficientUserQuota,
		http.StatusForbidden,
		types.ErrOptionWithSkipRetry(),
		types.ErrOptionWithNoRecordErrorLog(),
	)
}

// ---------------------------------------------------------------------------
// BillingSession — 统一计费会话
// ---------------------------------------------------------------------------

// BillingSession 封装单次请求的预扣费/结算/退款生命周期。
// 实现 relaycommon.BillingSettler 接口。
type BillingSession struct {
	relayInfo        *relaycommon.RelayInfo
	funding          FundingSource
	preConsumedQuota int  // 实际预扣额度（信任用户可能为 0）
	tokenConsumed    int  // 令牌额度实际扣减量
	extraReserved    int  // 发送前补充预扣的额度（订阅退款时需要单独回滚）
	trusted          bool // 是否命中信任额度旁路
	fundingSettled   bool // funding.Settle 已成功，资金来源已提交
	settled          bool // Settle 全部完成（资金 + 令牌）
	deliveredQuota   int  // 已经记下的实际扣费，冲突时以账本为准
	refunded         bool // Refund 已调用
	refundInProgress bool
	settleErr        error // 非幂等结算：令牌或资金写入可能已提交但仍返回错误，禁止再次加减
	ledgerActive     bool  // 本次结算走请求级幂等账本，失败后可以按同一键重试
	mu               sync.Mutex
}

// Settle 根据实际消耗额度进行结算。
// 幂等账本把资金和令牌放进同一事务。旧路径仍先调整令牌，资金失败时再回滚令牌。
func (s *BillingSession) Settle(actualQuota int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settled {
		return nil
	}
	if s.settleErr != nil {
		return s.settleErr
	}
	if s.idempotentEnabled() {
		return s.settleIdempotentLocked(actualQuota)
	}
	delta := actualQuota - s.preConsumedQuota
	s.logBillingSettlement("attempt", actualQuota, delta, nil)
	if delta == 0 {
		s.fundingSettled = true
		s.deliveredQuota = actualQuota
		s.settled = true
		s.stopWalletLease()
		s.logBillingSettlement("success", actualQuota, delta, nil)
		return nil
	}

	tokenAdjusted, tokenDelta, tokenErr := adjustTokenQuotaForSettlementTracked(s.relayInfo, delta)
	if tokenErr != nil {
		s.settleErr = tokenErr
		s.logBillingSettlement("error", actualQuota, delta, tokenErr)
		return tokenErr
	}

	if !s.fundingSettled {
		if err := s.funding.Settle(delta); err != nil {
			if tokenAdjusted {
				if rollbackErr := rollbackTrackedTokenQuotaAdjustment(s.relayInfo, delta, tokenDelta); rollbackErr != nil {
					common.SysLog(fmt.Sprintf("error rolling back token quota after funding settle failed (userId=%d, tokenId=%d, delta=%d): %s",
						s.relayInfo.UserId, s.relayInfo.TokenId, delta, rollbackErr.Error()))
				}
			}
			s.settleErr = err
			s.logBillingSettlement("error", actualQuota, delta, err)
			return err
		}
		s.fundingSettled = true
	}

	if s.funding.Source() == BillingSourceSubscription {
		s.relayInfo.SubscriptionPostDelta += int64(delta)
	}
	s.deliveredQuota = actualQuota
	s.settled = true
	s.stopWalletLease()
	s.logBillingSettlement("success", actualQuota, delta, nil)
	return nil
}

func (s *BillingSession) idempotentEnabled() bool {
	if s == nil || s.relayInfo == nil || s.funding == nil || model.DB == nil {
		return false
	}
	if strings.TrimSpace(s.relayInfo.RequestId) == "" {
		return false
	}
	switch s.funding.(type) {
	case *WalletFunding, *SubscriptionFunding:
		return true
	default:
		return false
	}
}

func (s *BillingSession) settleIdempotentLocked(actualQuota int) error {
	delta := actualQuota - s.preConsumedQuota
	s.ledgerActive = true
	s.logBillingSettlement("attempt", actualQuota, delta, nil)
	if actualQuota < 0 {
		err := fmt.Errorf("actual quota cannot be negative: %d", actualQuota)
		s.logBillingSettlement("error", actualQuota, delta, err)
		return err
	}
	req := s.billingAdjustmentRequest(actualQuota)
	err := stageDurableBillingAdjustment(req)
	if errors.Is(err, model.ErrBillingAdjustmentDeferred) {
		s.logBillingSettlement("error", actualQuota, delta, err)
		return err
	}
	if err != nil && !model.BillingAdjustmentRetryable(err) {
		if errors.Is(err, model.ErrBillingAdjustmentConflict) || errors.Is(err, model.ErrBillingAdjustmentClosed) {
			s.settleErr = err
		}
		s.logBillingSettlement("error", actualQuota, delta, err)
		return err
	}
	if err != nil {
		err = persistRetryableBillingAdjustment(req)
	} else {
		for attempt := 0; attempt < 3; attempt++ {
			_, err = model.ApplyBillingAdjustment(req)
			if err == nil || !model.BillingAdjustmentRetryable(err) {
				break
			}
		}
		// 三次都没落上时留下 pending 行。主节点会补记，已返回的结果不能停在预扣。
		if err != nil && model.BillingAdjustmentRetryable(err) {
			err = persistRetryableBillingAdjustment(req)
		}
	}
	if err != nil {
		if errors.Is(err, model.ErrBillingAdjustmentConflict) || errors.Is(err, model.ErrBillingAdjustmentClosed) {
			s.settleErr = err
		}
		s.logBillingSettlement("error", actualQuota, delta, err)
		return err
	}
	return s.finishIdempotentSettle(actualQuota, delta)
}

func (s *BillingSession) finishIdempotentSettle(actualQuota int, delta int) error {
	if s.funding.Source() == BillingSourceSubscription {
		s.relayInfo.SubscriptionPostDelta += int64(delta)
	}
	s.fundingSettled = true
	s.deliveredQuota = actualQuota
	s.settled = true
	s.settleErr = nil
	s.stopWalletLease()
	s.logBillingSettlement("success", actualQuota, delta, nil)
	return nil
}

// stageDurableBillingAdjustment commits the actual charge before any money moves.
// The spool is removed only after the pending row is visible. A hook error after
// that commit stays deferred and must not apply the charge in this call.
func stageDurableBillingAdjustment(req model.BillingAdjustmentRequest) error {
	_ = model.RememberUnpersistedBillingAdjustment(req)
	err := model.EnsurePendingBillingAdjustment(req)
	row, found, getErr := model.GetBillingAdjustment(req.IdempotencyKey)
	if getErr != nil && err == nil {
		return getErr
	}
	if found {
		model.ForgetUnpersistedBillingAdjustment(req.IdempotencyKey)
	}
	if err == nil {
		return nil
	}
	if errors.Is(err, model.ErrBillingAdjustmentConflict) || errors.Is(err, model.ErrBillingAdjustmentClosed) {
		return err
	}
	if found && row != nil && row.Status == model.BillingAdjustmentPending {
		return model.ErrBillingAdjustmentDeferred
	}
	return err
}

// AcceptDeliveredSettlement records the charge for a response that was already sent.
// Conflict keeps the stored charge. A rolled-back reservation is charged by the
// current delta once. The error is not returned to the relay, because that would
// refund a response the client already has.
func (s *BillingSession) AcceptDeliveredSettlement(actualQuota int) (charged int, applied bool, err error) {
	if s == nil {
		return 0, false, errors.New("billing session is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settled {
		return s.deliveredQuota, false, nil
	}
	if !s.idempotentEnabled() {
		if s.settleErr != nil {
			return 0, false, s.settleErr
		}
		return 0, false, errors.New("billing session is not ledger backed")
	}
	if actualQuota < 0 {
		return 0, false, fmt.Errorf("actual quota cannot be negative: %d", actualQuota)
	}
	req := s.billingAdjustmentRequest(actualQuota)
	if stageErr := stageDurableBillingAdjustment(req); stageErr != nil &&
		!errors.Is(stageErr, model.ErrBillingAdjustmentDeferred) &&
		!errors.Is(stageErr, model.ErrBillingAdjustmentConflict) &&
		!errors.Is(stageErr, model.ErrBillingAdjustmentClosed) &&
		!model.BillingAdjustmentRetryable(stageErr) {
		return 0, false, stageErr
	}
	charged, applied, err = model.CollectDeliveredBillingCharge(req)
	if err != nil {
		return 0, false, err
	}
	if applied && s.funding != nil && s.funding.Source() == BillingSourceSubscription && s.relayInfo != nil {
		s.relayInfo.SubscriptionPostDelta += int64(charged - s.preConsumedQuota)
	}
	s.ledgerActive = true
	s.fundingSettled = true
	s.deliveredQuota = charged
	s.settled = true
	s.settleErr = nil
	s.refunded = false
	s.stopWalletLease()
	s.logBillingSettlement("delivered", charged, charged-s.preConsumedQuota, nil)
	return charged, applied, nil
}

// DeliveredQuota returns the quota already recorded for a settled session.
func (s *BillingSession) DeliveredQuota() (int, bool) {
	if s == nil {
		return 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.settled {
		return 0, false
	}
	return s.deliveredQuota, true
}

func (s *BillingSession) billingAdjustmentRequest(actualQuota int) model.BillingAdjustmentRequest {
	delta := actualQuota - s.preConsumedQuota
	tokenDelta := 0
	tokenIncluded := s.relayInfo != nil && s.relayInfo.TokenId > 0 && !s.relayInfo.IsPlayground
	if tokenIncluded {
		tokenDelta = delta
	}
	return model.BillingAdjustmentRequest{
		IdempotencyKey:   BillingAdjustmentKey(s.relayInfo),
		RequestID:        strings.TrimSpace(s.relayInfo.RequestId),
		UserID:           s.relayInfo.UserId,
		TokenID:          s.relayInfo.TokenId,
		TokenKey:         s.relayInfo.TokenKey,
		Source:           s.funding.Source(),
		SubscriptionID:   s.subscriptionID(),
		FundingDelta:     delta,
		TokenDelta:       tokenDelta,
		ChargeQuota:      actualQuota,
		ReservationQuota: s.preConsumedQuota,
		ExtraReserved:    s.extraReserved,
		TokenIncluded:    tokenIncluded,
	}
}

// persistRetryableBillingAdjustment stores a pending row after apply could not commit.
// A retryable save error is deferred only when that row is actually present.
func persistRetryableBillingAdjustment(req model.BillingAdjustmentRequest) error {
	var saveErr error
	for attempt := 0; attempt < 3; attempt++ {
		saveErr = model.EnsurePendingBillingAdjustment(req)
		if saveErr == nil || !model.BillingAdjustmentRetryable(saveErr) {
			break
		}
		if resolved, ok := storedBillingAdjustmentResult(req.IdempotencyKey); ok {
			return resolved
		}
	}
	if saveErr != nil {
		if !model.BillingAdjustmentRetryable(saveErr) {
			return saveErr
		}
		if resolved, ok := storedBillingAdjustmentResult(req.IdempotencyKey); ok {
			return resolved
		}
		// 数据库暂时写不进行时，先把同一份请求落到本地，恢复任务再补 pending 行。
		if err := model.RememberUnpersistedBillingAdjustment(req); err != nil {
			return saveErr
		}
		return model.ErrBillingAdjustmentDeferred
	}
	if _, applyErr := model.ApplyBillingAdjustment(req); applyErr == nil {
		return nil
	} else if model.BillingAdjustmentRetryable(applyErr) {
		return model.ErrBillingAdjustmentDeferred
	} else {
		return applyErr
	}
}

func storedBillingAdjustmentResult(key string) (error, bool) {
	row, found, err := model.GetBillingAdjustment(key)
	if err != nil || !found || row == nil {
		return nil, false
	}
	switch row.Status {
	case model.BillingAdjustmentApplied:
		return nil, true
	case model.BillingAdjustmentPending:
		return model.ErrBillingAdjustmentDeferred, true
	case model.BillingAdjustmentRolledBack:
		return model.ErrBillingAdjustmentClosed, true
	default:
		return fmt.Errorf("unknown billing adjustment status %q", row.Status), true
	}
}

func (s *BillingSession) rollbackIdempotent(actualQuota int) error {
	key := BillingAdjustmentKey(s.relayInfo)
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = model.RollbackBillingAdjustment(key, actualQuota)
		if err == nil || errors.Is(err, model.ErrBillingAdjustmentAbsent) || errors.Is(err, model.ErrBillingAdjustmentConflict) {
			break
		}
	}
	if errors.Is(err, model.ErrBillingAdjustmentAbsent) {
		err = model.RefundBillingReservation(s.reservationRefund())
	}
	return err
}

func (s *BillingSession) reservationRefund() model.BillingReservationRefund {
	tokenIncluded := s != nil && s.relayInfo != nil && s.relayInfo.TokenId > 0 && !s.relayInfo.IsPlayground
	requestID := ""
	tokenID := 0
	tokenKey := ""
	userID := 0
	if s != nil && s.relayInfo != nil {
		requestID = strings.TrimSpace(s.relayInfo.RequestId)
		tokenID = s.relayInfo.TokenId
		tokenKey = s.relayInfo.TokenKey
		userID = s.relayInfo.UserId
	}
	source := ""
	if s != nil && s.funding != nil {
		source = s.funding.Source()
	}
	return model.BillingReservationRefund{
		IdempotencyKey:   BillingAdjustmentKey(s.relayInfo),
		RequestID:        requestID,
		UserID:           userID,
		TokenID:          tokenID,
		TokenKey:         tokenKey,
		Source:           source,
		SubscriptionID:   s.subscriptionID(),
		ReservationQuota: s.preConsumedQuota,
		ExtraReserved:    s.extraReserved,
		TokenIncluded:    tokenIncluded,
	}
}

func (s *BillingSession) subscriptionID() int {
	if s == nil {
		return 0
	}
	if sub, ok := s.funding.(*SubscriptionFunding); ok {
		return sub.subscriptionId
	}
	return 0
}

// LedgerBacked reports that this session settled through the idempotent ledger.
func (s *BillingSession) LedgerBacked() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ledgerActive
}

// BillingAdjustmentKey is the ledger key for one relay request.
func BillingAdjustmentKey(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	requestID := strings.TrimSpace(info.RequestId)
	if requestID == "" {
		return ""
	}
	return "billing:" + requestID
}

// AttachBillingAdjustmentIdentity records the ledger key on a durable task so a
// later poll can finish or reverse the same charge.
func AttachBillingAdjustmentIdentity(task *model.Task, info *relaycommon.RelayInfo, actualQuota int) {
	if task == nil || info == nil {
		return
	}
	session, ok := info.Billing.(*BillingSession)
	if !ok || !session.IdempotentFunding() {
		return
	}
	task.PrivateData.BillingAdjustmentKey = BillingAdjustmentKey(info)
	task.PrivateData.BillingAdjustmentUnresolved = true
	task.PrivateData.BillingAdjustmentActual = actualQuota
	task.PrivateData.BillingAdjustmentReservation = 0
	task.PrivateData.BillingAdjustmentExtra = 0
	if info.Billing != nil {
		task.PrivateData.BillingAdjustmentReservation = info.Billing.GetPreConsumedQuota()
	}
	session.mu.Lock()
	task.PrivateData.BillingAdjustmentExtra = session.extraReserved
	session.mu.Unlock()
}

// IdempotentFunding reports whether this session can use the request ledger.
func (s *BillingSession) IdempotentFunding() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.idempotentEnabled()
}

func adjustTokenQuotaForSettlement(relayInfo *relaycommon.RelayInfo, delta int) (bool, error) {
	tokenAdjusted, _, err := adjustTokenQuotaForSettlementTracked(relayInfo, delta)
	return tokenAdjusted, err
}

func adjustTokenQuotaForSettlementTracked(relayInfo *relaycommon.RelayInfo, delta int) (bool, model.TokenQuotaDelta, error) {
	if relayInfo == nil || relayInfo.IsPlayground || delta == 0 {
		return false, model.TokenQuotaDelta{}, nil
	}

	var err error
	if delta > 0 {
		err = model.DecreaseTokenQuotaAllowNegative(relayInfo.TokenId, relayInfo.TokenKey, int64(delta))
		if err != nil {
			if model.IsTokenQuotaNoRowsError(err) {
				common.SysLog(fmt.Sprintf("skip token quota charge because token no longer exists (userId=%d, tokenId=%d, delta=%d): %s",
					relayInfo.UserId, relayInfo.TokenId, delta, err.Error()))
				return false, model.TokenQuotaDelta{}, nil
			}
			return false, model.TokenQuotaDelta{}, err
		}
		return true, model.TokenQuotaDelta{}, nil
	}

	tokenDelta, err := model.IncreaseTokenQuotaTracked(relayInfo.TokenId, relayInfo.TokenKey, int64(-delta))
	if err != nil {
		if model.IsTokenQuotaNoRowsError(err) {
			common.SysLog(fmt.Sprintf("skip token quota refund because token no longer exists (userId=%d, tokenId=%d, delta=%d): %s",
				relayInfo.UserId, relayInfo.TokenId, delta, err.Error()))
			return false, model.TokenQuotaDelta{}, nil
		}
		return false, model.TokenQuotaDelta{}, err
	}
	return true, tokenDelta, nil
}

func rollbackTrackedTokenQuotaAdjustment(relayInfo *relaycommon.RelayInfo, delta int, tokenDelta model.TokenQuotaDelta) error {
	if relayInfo == nil || relayInfo.IsPlayground || delta == 0 {
		return nil
	}
	if delta < 0 {
		return model.ApplyTokenQuotaDelta(model.TokenQuotaDelta{
			TokenId:     tokenDelta.TokenId,
			Key:         tokenDelta.Key,
			RemainDelta: -tokenDelta.RemainDelta,
			UsedDelta:   -tokenDelta.UsedDelta,
		})
	} else {
		return model.IncreaseTokenQuota(relayInfo.TokenId, relayInfo.TokenKey, int64(delta))
	}
}

// Refund 退还所有预扣费，幂等安全，异步执行。
func (s *BillingSession) Refund(c *gin.Context) error {
	s.mu.Lock()
	if s.settled || s.refunded || s.refundInProgress || !s.needsRefundLocked() {
		s.mu.Unlock()
		return nil
	}
	s.refundInProgress = true
	ledgerActive := s.ledgerActive
	s.mu.Unlock()

	logger.LogInfo(c, fmt.Sprintf("用户 %d 请求失败, 返还预扣费（token_quota=%s, funding=%s）",
		s.relayInfo.UserId,
		logger.FormatQuota(s.tokenConsumed),
		s.funding.Source(),
	))

	// 复制需要的值到本地，避免后续状态变更影响退款过程
	tokenId := s.relayInfo.TokenId
	tokenKey := s.relayInfo.TokenKey
	isPlayground := s.relayInfo.IsPlayground
	tokenConsumed := s.tokenConsumed
	extraReserved := s.extraReserved
	subscriptionId := s.relayInfo.SubscriptionId
	funding := s.funding
	refundRelayInfo := &relaycommon.RelayInfo{
		UserId:       s.relayInfo.UserId,
		TokenId:      tokenId,
		TokenKey:     tokenKey,
		IsPlayground: isPlayground,
	}

	refundSucceeded := false
	defer func() {
		s.mu.Lock()
		s.refunded = refundSucceeded
		s.refundInProgress = false
		if refundSucceeded {
			s.stopWalletLease()
		}
		if refundSucceeded && ledgerActive {
			s.preConsumedQuota = 0
			s.tokenConsumed = 0
			s.extraReserved = 0
			s.settled = false
			s.fundingSettled = false
		}
		s.mu.Unlock()
	}()

	if ledgerActive {
		if err := s.rollbackIdempotent(0); err != nil {
			common.SysLog("error refunding idempotent billing reservation: " + err.Error())
			return err
		}
		refundSucceeded = true
		return nil
	}

	extraRequestID := ""
	if subFunding, ok := funding.(*SubscriptionFunding); ok {
		extraRequestID = subFunding.requestId
		if subscriptionId <= 0 {
			subscriptionId = subFunding.subscriptionId
		}
	}
	extraRefunded := int64(0)
	if extraReserved > 0 && funding.Source() == BillingSourceSubscription && subscriptionId > 0 {
		refunded, err := model.RefundSubscriptionReservedExtra(extraRequestID, subscriptionId, int64(extraReserved))
		if err != nil {
			common.SysLog("error refunding subscription extra reserved quota: " + err.Error())
			return err
		}
		extraRefunded = refunded
	}
	restoreExtra := func() {
		if extraRefunded <= 0 {
			return
		}
		if rollbackErr := model.ReserveUserSubscriptionDelta(extraRequestID, subscriptionId, extraRefunded); rollbackErr != nil {
			common.SysLog("error rolling back refunded subscription extra reserved quota: " + rollbackErr.Error())
		}
	}
	tokenAdjusted := false
	tokenDelta := model.TokenQuotaDelta{}
	if tokenConsumed > 0 && !isPlayground {
		var err error
		tokenAdjusted, tokenDelta, err = adjustTokenQuotaForSettlementTracked(refundRelayInfo, -tokenConsumed)
		if err != nil {
			restoreExtra()
			common.SysLog("error refunding token quota: " + err.Error())
			return err
		}
	}
	if err := funding.Refund(); err != nil {
		if tokenAdjusted {
			if rollbackErr := rollbackTrackedTokenQuotaAdjustment(refundRelayInfo, -tokenConsumed, tokenDelta); rollbackErr != nil {
				common.SysLog("error rolling back refunded token quota: " + rollbackErr.Error())
			}
		}
		restoreExtra()
		common.SysLog("error refunding billing source: " + err.Error())
		return err
	}
	refundSucceeded = true
	return nil
}

func (s *BillingSession) Rollback(actualQuota int) error {
	if s == nil || s.relayInfo == nil || s.funding == nil || actualQuota <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refunded {
		return nil
	}
	if s.ledgerActive {
		if err := s.rollbackIdempotent(actualQuota); err != nil {
			return err
		}
		s.settled = false
		s.fundingSettled = false
		s.refunded = true
		s.preConsumedQuota = 0
		s.tokenConsumed = 0
		s.extraReserved = 0
		s.syncRelayInfo()
		return nil
	}

	tokenAdjusted, tokenDelta, err := adjustTokenQuotaForSettlementTracked(s.relayInfo, -actualQuota)
	if err != nil {
		return err
	}

	if s.fundingSettled {
		if err := s.rollbackFundingSettlement(actualQuota); err != nil {
			if tokenAdjusted {
				if rollbackErr := rollbackTrackedTokenQuotaAdjustment(s.relayInfo, -actualQuota, tokenDelta); rollbackErr != nil {
					return fmt.Errorf("rollback funding settlement failed: %w; rollback token quota failed: %v", err, rollbackErr)
				}
			}
			return err
		}
	}

	if tokenAdjusted {
		common.SysLog(fmt.Sprintf("rollback billing settlement adjusted token quota (userId=%d, tokenId=%d, quota=%d)",
			s.relayInfo.UserId, s.relayInfo.TokenId, actualQuota))
	}
	s.settled = false
	s.fundingSettled = false
	s.refunded = true
	s.preConsumedQuota = 0
	s.tokenConsumed = 0
	s.extraReserved = 0
	s.syncRelayInfo()
	return nil
}

func (s *BillingSession) rollbackFundingSettlement(actualQuota int) error {
	if s == nil || s.funding == nil || actualQuota <= 0 {
		return nil
	}
	if sub, ok := s.funding.(*SubscriptionFunding); ok && strings.TrimSpace(sub.requestId) != "" {
		return model.RollbackSubscriptionPreConsumeSettlement(sub.requestId, int64(actualQuota))
	}
	return s.funding.Settle(-actualQuota)
}

// NeedsRefund 返回是否存在需要退还的预扣状态。
func (s *BillingSession) NeedsRefund() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.needsRefundLocked()
}

func (s *BillingSession) stopWalletLease() {
	if s == nil {
		return
	}
	if wallet, ok := s.funding.(*WalletFunding); ok {
		wallet.stopLease()
	}
	if subscription, ok := s.funding.(*SubscriptionFunding); ok {
		subscription.stopLease()
	}
}

func (s *BillingSession) needsRefundLocked() bool {
	if s.settled || s.refunded || s.fundingSettled {
		// fundingSettled 时资金来源已提交结算，不能再退预扣费
		return false
	}
	if s.tokenConsumed > 0 {
		return true
	}
	// 订阅可能在 tokenConsumed=0 时仍预扣了额度
	if sub, ok := s.funding.(*SubscriptionFunding); ok && sub.preConsumed > 0 {
		return true
	}
	// 信任旁路没有扣钱，但请求号上的义务还在，失败时必须关掉。
	if s.trusted && s.relayInfo != nil && strings.TrimSpace(s.relayInfo.RequestId) != "" {
		return true
	}
	return false
}

// GetPreConsumedQuota 返回实际预扣的额度。
func (s *BillingSession) GetPreConsumedQuota() int {
	return s.preConsumedQuota
}

func (s *BillingSession) Reserve(targetQuota int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.settled || s.refunded || targetQuota <= s.preConsumedQuota {
		return nil
	}
	if s.trusted {
		s.trusted = false
	}

	delta := targetQuota - s.preConsumedQuota
	if delta <= 0 {
		return nil
	}

	if err := s.reserveToken(delta); err != nil {
		return err
	}
	if err := s.reserveFunding(delta, true); err != nil {
		if !s.relayInfo.IsPlayground {
			if rollbackErr := model.IncreaseTokenQuota(s.relayInfo.TokenId, s.relayInfo.TokenKey, int64(delta)); rollbackErr != nil {
				common.SysLog(fmt.Sprintf("error rolling back token quota after funding reserve failed (userId=%d, tokenId=%d, delta=%d): %s",
					s.relayInfo.UserId, s.relayInfo.TokenId, delta, rollbackErr.Error()))
			}
		}
		return err
	}

	s.preConsumedQuota += delta
	s.tokenConsumed += delta
	s.extraReserved += delta
	s.syncRelayInfo()
	return nil
}

// ---------------------------------------------------------------------------
// PreConsume — 统一预扣费入口（含信任额度旁路）
// ---------------------------------------------------------------------------

// preConsume 执行预扣费：信任检查 -> 令牌预扣 -> 资金来源预扣。
// 任一步骤失败时原子回滚已完成的步骤。
func (s *BillingSession) preConsume(c *gin.Context, quota int) *types.NewAPIError {
	effectiveQuota := quota
	trustQuotaLabel := logger.FormatQuota(configuredTrustQuotaUnits())

	// ---- 信任额度旁路 ----
	if s.shouldTrust(c, quota) {
		s.trusted = true
		effectiveQuota = 0
		logger.LogInfo(c, fmt.Sprintf("billing_preconsume user_id=%d token_id=%d request_id=%s billing_source=%s trusted=true trust_quota=%s user_quota=%s token_quota=%s pre_consumed_quota=0",
			s.relayInfo.UserId,
			s.relayInfo.TokenId,
			s.relayInfo.RequestId,
			s.funding.Source(),
			trustQuotaLabel,
			logger.FormatQuota(s.relayInfo.UserQuota),
			logger.FormatQuota(common.GetContextInt64(c, "token_quota")),
		))
	} else if effectiveQuota > 0 {
		logger.LogInfo(c, fmt.Sprintf("billing_preconsume user_id=%d token_id=%d request_id=%s billing_source=%s trusted=false trust_quota=%s user_quota=%s token_quota=%s pre_consumed_quota=%s",
			s.relayInfo.UserId,
			s.relayInfo.TokenId,
			s.relayInfo.RequestId,
			s.funding.Source(),
			trustQuotaLabel,
			logger.FormatQuota(s.relayInfo.UserQuota),
			logger.FormatQuota(common.GetContextInt64(c, "token_quota")),
			logger.FormatQuota(effectiveQuota),
		))
	}

	// ---- 1) 预扣令牌额度 ----
	if effectiveQuota > 0 {
		if err := PreConsumeTokenQuota(s.relayInfo, effectiveQuota); err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		s.tokenConsumed = effectiveQuota
	}

	// ---- 2) 预扣资金来源 ----
	if err := s.funding.PreConsume(effectiveQuota); err != nil {
		// 预扣费失败，回滚令牌额度
		if s.tokenConsumed > 0 && !s.relayInfo.IsPlayground {
			if rollbackErr := model.IncreaseTokenQuota(s.relayInfo.TokenId, s.relayInfo.TokenKey, int64(s.tokenConsumed)); rollbackErr != nil {
				common.SysLog(fmt.Sprintf("error rolling back token quota (userId=%d, tokenId=%d, amount=%d, fundingErr=%s): %s",
					s.relayInfo.UserId, s.relayInfo.TokenId, s.tokenConsumed, err.Error(), rollbackErr.Error()))
			}
			s.tokenConsumed = 0
		}
		if errors.Is(err, ErrInsufficientWalletQuota) {
			userQuota, quotaErr := model.GetUserQuota(s.relayInfo.UserId, false)
			if quotaErr != nil {
				userQuota = 0
			}
			return insufficientUserQuotaProtocolError(userQuota)
		}
		if isSubscriptionPreConsumeInsufficientError(err) {
			return insufficientSubscriptionQuotaProtocolError()
		}
		return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	requestID := ""
	if s.relayInfo != nil {
		requestID = strings.TrimSpace(s.relayInfo.RequestId)
	}
	if s.trusted && s.funding.Source() == BillingSourceWallet && requestID != "" && quota > 0 {
		if err := model.ReserveTrustedWalletPreConsume(requestID, s.relayInfo.UserId, int64(quota)); err != nil {
			return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		if wallet, ok := s.funding.(*WalletFunding); ok {
			wallet.startLease()
		}
	}
	if requestID != "" {
		switch s.funding.Source() {
		case BillingSourceWallet:
			if effectiveQuota > 0 || (s.trusted && quota > 0) {
				s.relayInfo.SetClientDeliveredHook(func() error {
					if err := model.MarkWalletPreConsumeClientDelivered(requestID); err != nil {
						logger.LogError(c, "mark wallet pre-consume delivered: "+err.Error())
						return err
					}
					return nil
				})
			}
		case BillingSourceSubscription:
			s.relayInfo.SetClientDeliveredHook(func() error {
				if err := model.MarkSubscriptionPreConsumeClientDelivered(requestID); err != nil {
					logger.LogError(c, "mark subscription pre-consume delivered: "+err.Error())
					return err
				}
				return nil
			})
		}
	}

	s.preConsumedQuota = effectiveQuota

	// ---- 同步 RelayInfo 兼容字段 ----
	s.syncRelayInfo()

	return nil
}

func (s *BillingSession) reserveFunding(delta int, requireAvailableQuota bool) error {
	switch funding := s.funding.(type) {
	case *WalletFunding:
		// 发送前补充预扣：余额不足时拒绝，不把请求做成欠费。
		// 最终结算（WalletFunding.Settle 正差额）才允许记欠费。
		if strings.TrimSpace(funding.requestId) != "" {
			err := model.IncreaseWalletPreConsume(funding.requestId, funding.userId, int64(delta))
			if err == nil {
				funding.consumed += delta
				return nil
			}
			if !errors.Is(err, model.ErrWalletPreConsumeNotFound) {
				if errors.Is(err, model.ErrWalletPreConsumeInsufficient) {
					return types.NewErrorWithStatusCode(
						ErrInsufficientWalletQuota,
						types.ErrorCodeInsufficientUserQuota,
						http.StatusForbidden,
						types.ErrOptionWithSkipRetry(),
						types.ErrOptionWithNoRecordErrorLog(),
					)
				}
				return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
			}
		}
		reserved, err := model.TryReserveUserQuota(funding.userId, delta)
		if err != nil {
			return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		if !reserved {
			return types.NewErrorWithStatusCode(
				ErrInsufficientWalletQuota,
				types.ErrorCodeInsufficientUserQuota,
				http.StatusForbidden,
				types.ErrOptionWithSkipRetry(),
				types.ErrOptionWithNoRecordErrorLog(),
			)
		}
		funding.consumed += delta
		return nil
	case *SubscriptionFunding:
		if err := model.ReserveUserSubscriptionDelta(funding.requestId, funding.subscriptionId, int64(delta)); err != nil {
			return insufficientSubscriptionQuotaProtocolError()
		}
		return nil
	default:
		return types.NewError(fmt.Errorf("unsupported funding source: %s", s.funding.Source()), types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
}

func (s *BillingSession) rollbackFundingReserve(delta int) {
	switch funding := s.funding.(type) {
	case *WalletFunding:
		var err error
		if strings.TrimSpace(funding.requestId) != "" {
			err = model.ReleaseWalletPreConsume(funding.requestId, funding.userId, int64(delta))
			if errors.Is(err, model.ErrWalletPreConsumeNotFound) {
				err = model.CreditUserQuotaStrict(funding.userId, int64(delta))
			}
		} else {
			err = model.CreditUserQuotaStrict(funding.userId, int64(delta))
		}
		if err != nil {
			common.SysLog("error rolling back wallet funding reserve: " + err.Error())
		} else {
			funding.consumed -= delta
		}
	case *SubscriptionFunding:
		if _, err := model.RefundSubscriptionReservedExtra(funding.requestId, funding.subscriptionId, int64(delta)); err != nil {
			common.SysLog("error rolling back subscription funding reserve: " + err.Error())
		}
	}
}

func (s *BillingSession) reserveToken(delta int) error {
	if delta <= 0 || s.relayInfo.IsPlayground {
		return nil
	}
	if err := PreConsumeTokenQuota(s.relayInfo, delta); err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}
	return nil
}

// shouldTrust 统一信任额度检查，适用于钱包和订阅。
func configuredTrustQuota() float64 {
	trustQuota := operation_setting.GetQuotaSetting().TrustQuotaUSD * common.QuotaPerUnit
	if trustQuota <= 0 || math.IsNaN(trustQuota) || math.IsInf(trustQuota, 0) {
		return 0
	}
	return trustQuota
}

func configuredTrustQuotaUnits() int64 {
	trustQuota := configuredTrustQuota()
	if trustQuota > float64(math.MaxInt64) {
		return math.MaxInt64
	}
	return int64(trustQuota)
}

func (s *BillingSession) shouldTrust(c *gin.Context, requiredQuota int) bool {
	// 异步任务（ForcePreConsume=true）必须预扣全额，不允许信任旁路。
	// requiredQuota 不再参与阈值比较：旁路只看账户余额是否高于配置的美元信任额度。
	_ = requiredQuota
	if s.relayInfo.ForcePreConsume {
		return false
	}

	trustQuota := configuredTrustQuota()
	if trustQuota <= 0 {
		return false
	}

	// 检查令牌是否充足
	tokenTrusted := s.relayInfo.TokenUnlimited
	if !tokenTrusted {
		tokenQuota := common.GetContextInt64(c, "token_quota")
		tokenTrusted = float64(tokenQuota) > trustQuota
	}
	if !tokenTrusted {
		return false
	}

	switch s.funding.Source() {
	case BillingSourceWallet:
		return float64(s.relayInfo.UserQuota) > trustQuota
	case BillingSourceSubscription:
		// 订阅不能启用信任旁路。原因：
		// 1. PreConsumeUserSubscription 要求 amount>0 来创建预扣记录并锁定订阅
		// 2. SubscriptionFunding.PreConsume 忽略参数，始终用 s.amount 预扣
		// 3. 若信任旁路将 effectiveQuota 设为 0，会导致 preConsumedQuota 与实际订阅预扣不一致
		return false
	default:
		return false
	}
}

func (s *BillingSession) logBillingSettlement(status string, actualQuota int, delta int, settleErr error) {
	if s == nil || s.relayInfo == nil || s.funding == nil {
		return
	}

	ctx := context.Background()
	if s.relayInfo.RequestId != "" {
		ctx = context.WithValue(ctx, common.RequestIdKey, s.relayInfo.RequestId)
	}

	errMsg := ""
	if settleErr != nil {
		errMsg = strings.ReplaceAll(settleErr.Error(), "\n", " ")
	}
	logger.LogInfo(ctx, fmt.Sprintf("billing_settle status=%s user_id=%d token_id=%d request_id=%s billing_source=%s trusted=%t pre_consumed_quota=%s actual_quota=%s delta=%s error=%s",
		status,
		s.relayInfo.UserId,
		s.relayInfo.TokenId,
		s.relayInfo.RequestId,
		s.funding.Source(),
		s.trusted,
		logger.FormatQuota(s.preConsumedQuota),
		logger.FormatQuota(actualQuota),
		logger.FormatQuota(delta),
		errMsg,
	))
}

// syncRelayInfo 将 BillingSession 的状态同步到 RelayInfo 的兼容字段上。
func (s *BillingSession) syncRelayInfo() {
	info := s.relayInfo
	info.FinalPreConsumedQuota = s.preConsumedQuota
	info.BillingSource = s.funding.Source()

	if sub, ok := s.funding.(*SubscriptionFunding); ok {
		info.SubscriptionId = sub.subscriptionId
		info.SubscriptionPreConsumed = sub.preConsumed + int64(s.extraReserved)
		info.SubscriptionPostDelta = 0
		info.SubscriptionAmountTotal = sub.AmountTotal
		info.SubscriptionAmountUsedAfterPreConsume = sub.AmountUsedAfter + int64(s.extraReserved)
		info.SubscriptionPlanId = sub.PlanId
		info.SubscriptionPlanTitle = sub.PlanTitle
	} else {
		info.SubscriptionId = 0
		info.SubscriptionPreConsumed = 0
	}
}

// ---------------------------------------------------------------------------
// NewBillingSession 工厂 — 根据计费偏好创建会话并处理回退
// ---------------------------------------------------------------------------

// NewBillingSession 根据用户计费偏好创建 BillingSession，处理 subscription_first / wallet_first 的回退。
func NewBillingSession(c *gin.Context, relayInfo *relaycommon.RelayInfo, preConsumedQuota int) (*BillingSession, *types.NewAPIError) {
	if relayInfo == nil {
		return nil, types.NewError(fmt.Errorf("relayInfo is nil"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	pref := common.NormalizeBillingPreference(relayInfo.UserSetting.BillingPreference)

	// 钱包路径需要先检查用户额度
	tryWallet := func() (*BillingSession, *types.NewAPIError) {
		userQuota, err := model.WalletQuotaForPreConsume(relayInfo.UserId, int64(preConsumedQuota))
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}
		if userQuota <= 0 {
			return nil, insufficientUserQuotaProtocolError(userQuota)
		}
		relayInfo.UserQuota = userQuota

		session := &BillingSession{
			relayInfo: relayInfo,
			funding:   &WalletFunding{userId: relayInfo.UserId, requestId: relayInfo.RequestId},
		}
		// 余额不够覆盖预扣估算时，只有信任额度可以继续。信任判断看的是余额是否高于配置阈值，
		// 不是请求金额本身。不满足信任时仍拒绝，wallet_first 还能回退到订阅。
		if int64(preConsumedQuota) > userQuota && !session.shouldTrust(c, preConsumedQuota) {
			return nil, preConsumeQuotaProtocolError(userQuota, preConsumedQuota)
		}
		if apiErr := session.preConsume(c, preConsumedQuota); apiErr != nil {
			return nil, apiErr
		}
		return session, nil
	}

	trySubscription := func() (*BillingSession, *types.NewAPIError) {
		subConsume := int64(preConsumedQuota)
		if subConsume <= 0 {
			subConsume = 1
		}
		session := &BillingSession{
			relayInfo: relayInfo,
			funding: &SubscriptionFunding{
				requestId:  relayInfo.RequestId,
				userId:     relayInfo.UserId,
				modelName:  relayInfo.GetBillingModelName(),
				usingGroup: relayInfo.UsingGroup,
				amount:     subConsume,
			},
		}
		// 必须传 subConsume 而非 preConsumedQuota，保证 SubscriptionFunding.amount、
		// preConsume 参数和 FinalPreConsumedQuota 三者一致，避免订阅多扣费。
		if apiErr := session.preConsume(c, int(subConsume)); apiErr != nil {
			return nil, apiErr
		}
		return session, nil
	}

	switch pref {
	case "subscription_only":
		return trySubscription()
	case "wallet_only":
		return tryWallet()
	case "wallet_first":
		session, err := tryWallet()
		if err != nil {
			if err.GetErrorCode() == types.ErrorCodeInsufficientUserQuota {
				return trySubscription()
			}
			return nil, err
		}
		return session, nil
	case "subscription_first":
		fallthrough
	default:
		hasSub, subCheckErr := model.HasActiveUserSubscription(relayInfo.UserId)
		if subCheckErr != nil {
			return nil, types.NewError(subCheckErr, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}
		if !hasSub {
			return tryWallet()
		}
		session, apiErr := trySubscription()
		if apiErr != nil {
			if apiErr.GetErrorCode() == types.ErrorCodeInsufficientUserQuota {
				// 仅当用户的活跃订阅允许钱包回退时才回退到钱包，否则返回订阅额度不足错误
				allowOverflow, overflowErr := model.UserActiveSubscriptionsAllowWalletOverflowForGroup(relayInfo.UserId, relayInfo.UsingGroup)
				if overflowErr != nil {
					return nil, types.NewError(overflowErr, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
				}
				if allowOverflow {
					return tryWallet()
				}
				return nil, apiErr
			}
			return nil, apiErr
		}
		return session, nil
	}
}
