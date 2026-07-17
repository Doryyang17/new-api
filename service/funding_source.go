package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
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
	// Reserve 为流式/异步请求追加预扣。
	Reserve(amount int) error
	// RollbackReserve 回滚最近一次追加预扣。
	RollbackReserve(amount int) error
	// CurrentConsumed 返回该来源当前承担的额度。
	CurrentConsumed() int
}

// ---------------------------------------------------------------------------
// WalletFunding — 钱包资金来源实现
// ---------------------------------------------------------------------------

type WalletFunding struct {
	userId   int
	consumed int // 实际预扣的用户额度
}

func (w *WalletFunding) Source() string { return BillingSourceWallet }

func (w *WalletFunding) PreConsume(amount int) error {
	if amount <= 0 {
		return nil
	}
	if err := model.DecreaseUserQuota(w.userId, amount, false); err != nil {
		return err
	}
	w.consumed += amount
	return nil
}

func (w *WalletFunding) Settle(delta int) error {
	if delta == 0 {
		return nil
	}
	if delta > 0 {
		if err := model.DecreaseUserQuota(w.userId, delta, false); err != nil {
			return err
		}
		w.consumed += delta
		return nil
	}
	if err := model.IncreaseUserQuota(w.userId, -delta, false); err != nil {
		return err
	}
	w.consumed += delta
	return nil
}

func (w *WalletFunding) Refund() error {
	if w.consumed <= 0 {
		return nil
	}
	// IncreaseUserQuota 是 quota += N 的非幂等操作，不能重试，否则会多退额度。
	// 订阅的 RefundSubscriptionPreConsume 有 requestId 幂等保护所以可以重试。
	if err := model.IncreaseUserQuota(w.userId, w.consumed, false); err != nil {
		return err
	}
	w.consumed = 0
	return nil
}

func (w *WalletFunding) Reserve(amount int) error {
	if amount <= 0 {
		return nil
	}
	quota, err := model.GetUserQuota(w.userId, false)
	if err != nil {
		return err
	}
	if quota < amount {
		return fmt.Errorf("insufficient user quota for wallet reservation: have=%d need=%d", quota, amount)
	}
	return w.PreConsume(amount)
}

func (w *WalletFunding) RollbackReserve(amount int) error {
	if amount <= 0 {
		return nil
	}
	if amount > w.consumed {
		return fmt.Errorf("wallet reserve rollback exceeds consumed amount")
	}
	if err := model.IncreaseUserQuota(w.userId, amount, false); err != nil {
		return err
	}
	w.consumed -= amount
	return nil
}

func (w *WalletFunding) CurrentConsumed() int { return w.consumed }

// ---------------------------------------------------------------------------
// SubscriptionFunding — 订阅资金来源实现
// ---------------------------------------------------------------------------

type SubscriptionFunding struct {
	requestId      string
	userId         int
	modelName      string
	subscriptionId int
	preConsumed    int64
	reserved       int64
	postDelta      int64
	reserveInitial int64
	// 以下字段在 PreConsume 成功后填充，供 RelayInfo 同步使用
	AmountTotal     int64
	AmountUsedAfter int64
	PlanId          int
	PlanTitle       string
}

func (s *SubscriptionFunding) Source() string { return BillingSourceSubscription }

func (s *SubscriptionFunding) PreConsume(amount int) error {
	if amount <= 0 {
		return nil
	}
	res, err := model.PreConsumeUserSubscription(s.requestId, s.userId, s.modelName, 0, int64(amount))
	if err != nil {
		return err
	}
	return s.applyPreConsumeResult(res)
}

func (s *SubscriptionFunding) applyPreConsumeResult(res *model.SubscriptionPreConsumeResult) error {
	if res == nil {
		return errors.New("subscription pre-consume result is nil")
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
	return nil
}

func (s *SubscriptionFunding) Settle(delta int) error {
	if delta == 0 {
		return nil
	}
	if s.subscriptionId == 0 {
		if delta < 0 {
			return fmt.Errorf("subscription is not initialized")
		}
		return s.PreConsume(delta)
	}
	if err := model.PostConsumeUserSubscriptionDelta(s.subscriptionId, int64(delta)); err != nil {
		return err
	}
	s.postDelta += int64(delta)
	return nil
}

func (s *SubscriptionFunding) Refund() error {
	if s.preConsumed <= 0 && s.reserved <= 0 {
		return nil
	}
	if s.preConsumed > 0 {
		if err := refundWithRetry(func() error {
			return model.RefundSubscriptionPreConsume(s.requestId)
		}); err != nil {
			return err
		}
	}
	if s.reserved > 0 {
		if err := model.PostConsumeUserSubscriptionDelta(s.subscriptionId, -s.reserved); err != nil {
			return err
		}
	}
	s.preConsumed = 0
	s.reserved = 0
	s.reserveInitial = 0
	return nil
}

func (s *SubscriptionFunding) Reserve(amount int) error {
	if amount <= 0 {
		return nil
	}
	if s.subscriptionId == 0 {
		if err := s.PreConsume(amount); err != nil {
			return err
		}
		s.reserveInitial += int64(amount)
		return nil
	}
	if err := model.PostConsumeUserSubscriptionDelta(s.subscriptionId, int64(amount)); err != nil {
		return err
	}
	s.reserved += int64(amount)
	return nil
}

func (s *SubscriptionFunding) RollbackReserve(amount int) error {
	if amount <= 0 {
		return nil
	}
	amount64 := int64(amount)
	if s.reserveInitial >= amount64 && s.preConsumed == s.reserveInitial && s.reserved == 0 {
		if err := model.RefundSubscriptionPreConsume(s.requestId); err != nil {
			return err
		}
		s.subscriptionId = 0
		s.preConsumed = 0
		s.reserveInitial = 0
		return nil
	}
	if amount64 > s.reserved {
		return fmt.Errorf("subscription reserve rollback exceeds reserved amount")
	}
	if err := model.PostConsumeUserSubscriptionDelta(s.subscriptionId, -amount64); err != nil {
		return err
	}
	s.reserved -= amount64
	return nil
}

func (s *SubscriptionFunding) CurrentConsumed() int {
	return int(s.preConsumed + s.reserved + s.postDelta)
}

// CheckinBonusFunding composes the independent check-in bonus in front of an
// existing wallet/subscription source without changing that source's rules.
type CheckinBonusFunding struct {
	requestId string
	userId    int
	tokenId   int
	base      FundingSource

	bonusPreConsumed int
	bonusConsumed    int
	baseConsumed     int

	lastReserveBonus               int
	lastReserveBase                int
	combinedTracked                bool
	baseOnlyLocked                 bool
	lastReserveInitialSubscription bool
	tokenHandled                   bool
}

func (f *CheckinBonusFunding) Source() string { return f.base.Source() }

func (f *CheckinBonusFunding) PreConsume(amount int) error {
	if wallet, ok := f.base.(*WalletFunding); ok {
		bonus, baseAmount, tracked, err := model.ReserveCheckinBonusWallet(
			f.userId,
			f.requestId,
			amount,
			time.Now(),
			common.NodeName,
			common.StartTime,
			checkinBonusProcessId,
			f.tokenId,
			true,
		)
		if err != nil {
			return err
		}
		if !tracked {
			if err := wallet.PreConsume(baseAmount); err != nil {
				return err
			}
			f.baseOnlyLocked = amount > 0
		} else {
			wallet.consumed += baseAmount
			f.combinedTracked = true
		}
		f.bonusPreConsumed = bonus
		f.bonusConsumed = bonus
		f.baseConsumed = baseAmount
		return nil
	}
	if subscription, ok := f.base.(*SubscriptionFunding); ok {
		bonus, baseAmount, tracked, result, err := model.ReserveCheckinBonusSubscription(
			f.userId,
			f.requestId,
			subscription.modelName,
			amount,
			time.Now(),
			common.NodeName,
			common.StartTime,
			checkinBonusProcessId,
			f.tokenId,
		)
		if err != nil {
			return err
		}
		if !tracked {
			if err := subscription.PreConsume(baseAmount); err != nil {
				return err
			}
			f.baseOnlyLocked = amount > 0
		} else {
			f.combinedTracked = true
			if result != nil {
				if err := subscription.applyPreConsumeResult(result); err != nil {
					return err
				}
			}
		}
		f.bonusPreConsumed = bonus
		f.bonusConsumed = bonus
		f.baseConsumed = baseAmount
		return nil
	}

	bonus, err := model.ReserveCheckinBonus(f.userId, f.requestId, amount, time.Now())
	if err != nil {
		return err
	}
	baseAmount := amount - bonus
	if err := f.base.PreConsume(baseAmount); err != nil {
		if rollbackErr := model.ReleaseCheckinBonusReservation(f.requestId, bonus, time.Now()); rollbackErr != nil {
			return fmt.Errorf("%w; check-in bonus rollback failed: %v", err, rollbackErr)
		}
		return err
	}
	if bonus == 0 && amount > 0 {
		f.baseOnlyLocked = true
	}
	f.bonusPreConsumed = bonus
	f.bonusConsumed = bonus
	f.baseConsumed = baseAmount
	return nil
}

func (f *CheckinBonusFunding) Reserve(amount int) error {
	if amount <= 0 {
		return nil
	}
	// A request whose first positive pre-consume found no bonus has already
	// charged its original source outside the combined ledger. Switching that
	// request to a newly-created bonus later would leave the earlier reservation
	// absent from the ledger and make final settlement charge it again.
	if f.baseOnlyLocked {
		if err := f.base.Reserve(amount); err != nil {
			return err
		}
		f.baseConsumed += amount
		f.lastReserveBonus = 0
		f.lastReserveBase = amount
		f.lastReserveInitialSubscription = false
		return nil
	}
	if wallet, ok := f.base.(*WalletFunding); ok {
		bonus, baseAmount, tracked, err := model.ReserveCheckinBonusWallet(
			f.userId,
			f.requestId,
			amount,
			time.Now(),
			common.NodeName,
			common.StartTime,
			checkinBonusProcessId,
			f.tokenId,
			true,
		)
		if err != nil {
			return err
		}
		if !tracked {
			if f.combinedTracked {
				return errors.New("tracked check-in bonus wallet reservation disappeared")
			}
			if err := wallet.Reserve(baseAmount); err != nil {
				return err
			}
		} else {
			wallet.consumed += baseAmount
			f.combinedTracked = true
		}
		f.bonusPreConsumed += bonus
		f.bonusConsumed += bonus
		f.baseConsumed += baseAmount
		f.lastReserveBonus = bonus
		f.lastReserveBase = baseAmount
		return nil
	}
	if subscription, ok := f.base.(*SubscriptionFunding); ok {
		wasInitialized := subscription.subscriptionId > 0
		bonus, baseAmount, tracked, result, err := model.ReserveCheckinBonusSubscription(
			f.userId,
			f.requestId,
			subscription.modelName,
			amount,
			time.Now(),
			common.NodeName,
			common.StartTime,
			checkinBonusProcessId,
			f.tokenId,
		)
		if err != nil {
			return err
		}
		if !tracked {
			if f.combinedTracked {
				return errors.New("tracked check-in bonus subscription reservation disappeared")
			}
			if err := subscription.Reserve(baseAmount); err != nil {
				return err
			}
		} else {
			f.combinedTracked = true
			if result != nil && !wasInitialized {
				if err := subscription.applyPreConsumeResult(result); err != nil {
					return err
				}
				f.lastReserveInitialSubscription = true
			} else if baseAmount > 0 {
				subscription.reserved += int64(baseAmount)
			}
		}
		f.bonusPreConsumed += bonus
		f.bonusConsumed += bonus
		f.baseConsumed += baseAmount
		f.lastReserveBonus = bonus
		f.lastReserveBase = baseAmount
		return nil
	}

	bonus, err := model.ReserveCheckinBonus(f.userId, f.requestId, amount, time.Now())
	if err != nil {
		return err
	}
	baseAmount := amount - bonus
	if err := f.base.Reserve(baseAmount); err != nil {
		if rollbackErr := model.ReleaseCheckinBonusReservation(f.requestId, bonus, time.Now()); rollbackErr != nil {
			return fmt.Errorf("%w; check-in bonus rollback failed: %v", err, rollbackErr)
		}
		return err
	}
	f.bonusPreConsumed += bonus
	f.bonusConsumed += bonus
	f.baseConsumed += baseAmount
	f.lastReserveBonus = bonus
	f.lastReserveBase = baseAmount
	return nil
}

func (f *CheckinBonusFunding) RollbackReserve(amount int) error {
	if amount <= 0 {
		return nil
	}
	if f.lastReserveBonus+f.lastReserveBase != amount {
		return fmt.Errorf("check-in bonus reserve rollback does not match latest reserve")
	}
	if f.combinedTracked {
		if err := model.ReleaseCheckinBonusFundingReservation(
			f.requestId,
			f.lastReserveBonus,
			f.lastReserveBase,
			time.Now(),
		); err != nil {
			return err
		}
		switch base := f.base.(type) {
		case *WalletFunding:
			base.consumed -= f.lastReserveBase
		case *SubscriptionFunding:
			if f.lastReserveInitialSubscription {
				base.preConsumed = 0
				base.AmountUsedAfter -= int64(f.lastReserveBase)
				if base.AmountUsedAfter < 0 {
					base.AmountUsedAfter = 0
				}
			} else {
				base.reserved -= int64(f.lastReserveBase)
			}
		}
		f.bonusPreConsumed -= f.lastReserveBonus
		f.bonusConsumed -= f.lastReserveBonus
		f.baseConsumed -= f.lastReserveBase
		f.lastReserveBonus = 0
		f.lastReserveBase = 0
		f.lastReserveInitialSubscription = false
		return nil
	}
	var baseErr error
	if f.lastReserveBase > 0 {
		baseErr = f.base.RollbackReserve(f.lastReserveBase)
	}
	bonusErr := model.ReleaseCheckinBonusReservation(f.requestId, f.lastReserveBonus, time.Now())
	if baseErr != nil || bonusErr != nil {
		return fmt.Errorf("funding reserve rollback failed: base=%v bonus=%v", baseErr, bonusErr)
	}
	f.bonusPreConsumed -= f.lastReserveBonus
	f.bonusConsumed -= f.lastReserveBonus
	f.baseConsumed -= f.lastReserveBase
	f.lastReserveBonus = 0
	f.lastReserveBase = 0
	return nil
}

func (f *CheckinBonusFunding) Settle(delta int) error {
	if f.baseOnlyLocked {
		if err := f.base.Settle(delta); err != nil {
			return err
		}
		f.baseConsumed += delta
		return nil
	}
	if f.combinedTracked {
		if delta > 0 {
			var bonus, baseDelta int
			var tracked bool
			var err error
			switch base := f.base.(type) {
			case *WalletFunding:
				bonus, baseDelta, tracked, err = model.ReserveCheckinBonusWallet(
					f.userId, f.requestId, delta, time.Now(), common.NodeName,
					common.StartTime, checkinBonusProcessId, f.tokenId, false,
				)
			case *SubscriptionFunding:
				var result *model.SubscriptionPreConsumeResult
				bonus, baseDelta, tracked, result, err = model.ReserveCheckinBonusSubscription(
					f.userId, f.requestId, base.modelName, delta, time.Now(), common.NodeName,
					common.StartTime, checkinBonusProcessId, f.tokenId,
				)
				if err == nil && result != nil {
					err = base.applyPreConsumeResult(result)
				}
			default:
				return errors.New("unsupported tracked check-in bonus funding source")
			}
			if err != nil {
				return err
			}
			if !tracked {
				return errors.New("tracked check-in bonus funding settlement disappeared")
			}
			f.bonusConsumed += bonus
			f.baseConsumed += baseDelta
		} else if delta < 0 {
			refund := -delta
			baseRefund := min(refund, f.baseConsumed)
			f.baseConsumed -= baseRefund
			refund -= baseRefund
			bonusRefund := min(refund, f.bonusConsumed)
			f.bonusConsumed -= bonusRefund
		}
		handled, err := model.SettleCheckinBonusFundingUsage(
			f.requestId,
			f.bonusConsumed,
			f.baseConsumed,
			time.Now(),
		)
		if err != nil {
			return err
		}
		if !handled {
			return errors.New("tracked check-in bonus funding settlement is missing")
		}
		switch base := f.base.(type) {
		case *WalletFunding:
			base.consumed = f.baseConsumed
		case *SubscriptionFunding:
			preSettled := base.preConsumed + base.reserved
			base.postDelta = int64(f.baseConsumed) - preSettled
		}
		return nil
	}

	// Trusted wallet requests skip pre-consumption entirely. If a bonus is
	// available at settlement time, commit the complete bonus/wallet split and
	// its audit ledger in one transaction instead of leaving an unowned bonus
	// reservation between separate database operations.
	if delta > 0 && f.bonusConsumed == 0 && f.baseConsumed == 0 && f.base.CurrentConsumed() == 0 {
		if wallet, ok := f.base.(*WalletFunding); ok {
			bonus, baseDelta, tracked, err := model.SettleCheckinBonusWalletCharge(
				f.userId,
				f.requestId,
				delta,
				time.Now(),
				common.NodeName,
				common.StartTime,
				checkinBonusProcessId,
			)
			if err != nil {
				return err
			}
			if tracked {
				f.combinedTracked = true
				f.bonusConsumed = bonus
				f.baseConsumed = baseDelta
				wallet.consumed = baseDelta
				return nil
			}
			// The atomic check found no active bonus. Commit the postpaid wallet
			// charge directly instead of performing a second bonus lookup that
			// could reintroduce a split transaction if a bonus appears meanwhile.
			if err := wallet.Settle(delta); err != nil {
				return err
			}
			f.baseConsumed = delta
			return nil
		}
	}

	if delta > 0 {
		bonus, err := model.ReserveCheckinBonus(f.userId, f.requestId, delta, time.Now())
		if err != nil {
			return err
		}
		baseDelta := delta - bonus
		if err := f.base.Settle(baseDelta); err != nil {
			if rollbackErr := model.ReleaseCheckinBonusReservation(f.requestId, bonus, time.Now()); rollbackErr != nil {
				return fmt.Errorf("%w; check-in bonus settlement rollback failed: %v", err, rollbackErr)
			}
			return err
		}
		f.bonusConsumed += bonus
		f.baseConsumed += baseDelta
	} else if delta < 0 {
		refund := -delta
		baseRefund := min(refund, f.baseConsumed)
		if baseRefund > 0 {
			if err := f.base.Settle(-baseRefund); err != nil {
				return err
			}
			f.baseConsumed -= baseRefund
			refund -= baseRefund
		}
		bonusRefund := min(refund, f.bonusConsumed)
		f.bonusConsumed -= bonusRefund
	}
	return model.SettleCheckinBonusUsage(f.requestId, f.bonusConsumed, time.Now())
}

func (f *CheckinBonusFunding) Refund() error {
	if f.combinedTracked {
		_, _, _, tokenHandled, handled, err := model.RefundCheckinBonusFundingUsage(f.requestId, time.Now())
		if err != nil {
			return err
		}
		if !handled {
			return errors.New("tracked check-in bonus funding refund is missing")
		}
		switch base := f.base.(type) {
		case *WalletFunding:
			base.consumed = 0
		case *SubscriptionFunding:
			base.preConsumed = 0
			base.reserved = 0
			base.postDelta = 0
			base.reserveInitial = 0
		}
		f.baseConsumed = 0
		f.bonusConsumed = 0
		f.bonusPreConsumed = 0
		f.tokenHandled = tokenHandled
		return nil
	}
	baseErr := f.base.Refund()
	_, bonusErr := model.RefundCheckinBonusUsage(f.requestId, time.Now())
	if baseErr != nil || bonusErr != nil {
		return fmt.Errorf("funding refund failed: base=%v bonus=%v", baseErr, bonusErr)
	}
	f.baseConsumed = 0
	f.bonusConsumed = 0
	return nil
}

func (f *CheckinBonusFunding) CurrentConsumed() int {
	return f.bonusConsumed + f.baseConsumed
}

// refundWithRetry 尝试多次执行退款操作以提高成功率，只能用于基于事务的退款函数！！！！！！
// try to refund with retries, only for refund functions based on transactions!!!
func refundWithRetry(fn func() error) error {
	if fn == nil {
		return nil
	}
	const maxAttempts = 3
	var lastErr error
	for i := 0; i < maxAttempts; i++ {
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
