package service

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

// ---------------------------------------------------------------------------
// FundingSource — 资金来源接口（钱包 or 订阅）
// ---------------------------------------------------------------------------

// FundingSource 抽象了预扣费的资金来源。
type FundingSource interface {
	// Source 返回资金来源标识："wallet" 或 "subscription"
	Source() string
	// PreConsume 从该资金来源预扣 amount 额度
	PreConsume(amount int) error
	// Settle 根据差额调整资金来源（正数补扣，负数退还）
	Settle(delta int) error
	// Refund 退还所有预扣费
	Refund() error
}

// ---------------------------------------------------------------------------
// WalletFunding — 钱包资金来源实现
// ---------------------------------------------------------------------------

// ErrInsufficientWalletQuota 钱包原子预扣失败（余额不足），未发生任何扣减。
// BillingSession 据此映射为 ErrorCodeInsufficientUserQuota，
// 使 wallet_first 等计费偏好可以回退到订阅。
var ErrInsufficientWalletQuota = errors.New("wallet quota insufficient")

type WalletFunding struct {
	userId    int
	consumed  int // 实际预扣的用户额度
	requestId string
	leaseStop chan struct{}
	leaseOnce sync.Once
}

func (w *WalletFunding) Source() string { return BillingSourceWallet }

func (w *WalletFunding) PreConsume(amount int) error {
	if amount <= 0 {
		return nil
	}
	if strings.TrimSpace(w.requestId) != "" {
		err := model.ReserveWalletPreConsume(w.requestId, w.userId, int64(amount))
		if err != nil {
			if errors.Is(err, model.ErrWalletPreConsumeInsufficient) {
				return ErrInsufficientWalletQuota
			}
			return err
		}
		w.consumed = amount
		w.startLease()
		return nil
	}
	reserved, err := model.TryReserveUserQuota(w.userId, amount)
	if err != nil {
		return err
	}
	if !reserved {
		return ErrInsufficientWalletQuota
	}
	w.consumed = amount
	return nil
}

func (w *WalletFunding) Settle(delta int) error {
	if delta == 0 {
		return nil
	}
	if delta > 0 {
		return model.DecreaseUserQuotaAllowNegative(w.userId, int64(delta), false)
	}
	// 负差额是退款。顶格时整笔回滚，不能把被截掉的部分记成已退。
	return model.CreditUserQuotaStrict(w.userId, int64(-delta))
}

func (w *WalletFunding) Refund() error {
	if strings.TrimSpace(w.requestId) != "" {
		err := model.RefundWalletPreConsume(w.requestId)
		if err == nil {
			w.consumed = 0
			w.stopLease()
			return nil
		}
		if !errors.Is(err, model.ErrWalletPreConsumeNotFound) {
			return err
		}
	}
	if w.consumed <= 0 {
		w.stopLease()
		return nil
	}
	// 全额入账才算退回。钱包已经顶格时交易回滚，余额不变，调用方不能把 consumed 当成已退。
	if err := model.CreditUserQuotaStrict(w.userId, int64(w.consumed)); err != nil {
		return err
	}
	w.consumed = 0
	w.stopLease()
	return nil
}

func (w *WalletFunding) startLease() {
	if w == nil || strings.TrimSpace(w.requestId) == "" || w.leaseStop != nil {
		return
	}
	w.leaseStop = make(chan struct{})
	requestID := w.requestId
	stop := w.leaseStop
	gopool.Go(func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				alive, err := model.RefreshWalletPreConsumeLease(requestID)
				if err != nil {
					common.SysLog("wallet pre-consume lease refresh failed: " + err.Error())
					continue
				}
				if !alive {
					return
				}
			}
		}
	})
}

func (w *WalletFunding) stopLease() {
	if w == nil || w.leaseStop == nil {
		return
	}
	w.leaseOnce.Do(func() {
		close(w.leaseStop)
	})
}

// ---------------------------------------------------------------------------
// SubscriptionFunding — 订阅资金来源实现
// ---------------------------------------------------------------------------

type SubscriptionFunding struct {
	requestId      string
	userId         int
	modelName      string
	usingGroup     string
	amount         int64 // 预扣的订阅额度（subConsume）
	subscriptionId int
	preConsumed    int64
	leaseStop      chan struct{}
	leaseOnce      sync.Once
	// 以下字段在 PreConsume 成功后填充，供 RelayInfo 同步使用
	AmountTotal     int64
	AmountUsedAfter int64
	PlanId          int
	PlanTitle       string
}

func (s *SubscriptionFunding) Source() string { return BillingSourceSubscription }

func (s *SubscriptionFunding) PreConsume(_ int) error {
	// amount 参数被忽略，使用内部 s.amount（已在构造时根据 preConsumedQuota 计算）
	res, err := model.PreConsumeUserSubscription(s.requestId, s.userId, s.modelName, 0, s.amount, s.usingGroup)
	if err != nil {
		return err
	}
	s.subscriptionId = res.UserSubscriptionId
	s.preConsumed = res.PreConsumed
	s.AmountTotal = res.AmountTotal
	s.AmountUsedAfter = res.AmountUsedAfter
	// 获取订阅计划信息
	if planInfo, err := model.GetSubscriptionPlanInfoByUserSubscriptionId(res.UserSubscriptionId); err == nil && planInfo != nil {
		s.PlanId = planInfo.PlanId
		s.PlanTitle = planInfo.PlanTitle
	}
	if err := model.ArmSubscriptionPreConsumeLease(s.requestId); err != nil {
		// 预扣已经成功。租约失败只留下不能被清扫的占用，不能把这次请求打回去。
		common.SysLog("arm subscription pre-consume lease: " + err.Error())
		return nil
	}
	s.startLease()
	return nil
}

func (s *SubscriptionFunding) Settle(delta int) error {
	if delta == 0 {
		return nil
	}
	if delta > 0 {
		return model.PostConsumeUserSubscriptionDeltaAllowOverdraft(s.subscriptionId, int64(delta))
	}
	return model.PostConsumeUserSubscriptionDelta(s.subscriptionId, int64(delta))
}

func (s *SubscriptionFunding) Refund() error {
	if s.preConsumed <= 0 {
		return nil
	}
	if err := refundWithRetry(func() error {
		return model.RefundSubscriptionPreConsume(s.requestId)
	}); err != nil {
		return err
	}
	s.stopLease()
	return nil
}

func (s *SubscriptionFunding) startLease() {
	if s == nil || strings.TrimSpace(s.requestId) == "" || s.leaseStop != nil {
		return
	}
	s.leaseStop = make(chan struct{})
	requestID := s.requestId
	stop := s.leaseStop
	gopool.Go(func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				alive, err := model.RefreshSubscriptionPreConsumeLease(requestID)
				if err != nil {
					common.SysLog("subscription pre-consume lease refresh failed: " + err.Error())
					continue
				}
				if !alive {
					return
				}
			}
		}
	})
}

func (s *SubscriptionFunding) stopLease() {
	if s == nil || s.leaseStop == nil {
		return
	}
	s.leaseOnce.Do(func() {
		close(s.leaseStop)
	})
}

// refundWithRetry 尝试多次执行退款操作以提高成功率，只能用于基于事务的退款函数！！！！！！
// try to refund with retries, only for refund functions based on transactions!!!
func refundWithRetry(fn func() error) error {
	if fn == nil {
		return nil
	}
	const maxAttempts = 3
	var lastErr error
	for i := range maxAttempts {
		if err := fn(); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if i < maxAttempts-1 {
			time.Sleep(time.Duration(200*(i+1)) * time.Millisecond)
		}
	}
	return lastErr
}
