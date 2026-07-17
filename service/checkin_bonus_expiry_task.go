package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	checkinBonusExpiryTickInterval = 15 * time.Second
	checkinBonusExpiryBatchSize    = 500
	checkinBonusRecoveryBatchSize  = 200
	checkinBonusLeaseStaleAfter    = 45 * time.Second
)

var (
	checkinBonusExpiryOnce    sync.Once
	checkinBonusExpiryRunning atomic.Bool
	checkinBonusProcessId     = common.NewRequestId()
)

// StartCheckinBonusExpiryTask recovers reservations left by an earlier process
// on every node. The master also marks due bonuses as expired; consumption
// paths check expire_at transactionally, so expiry remains cleanup rather than
// a correctness dependency.
func StartCheckinBonusExpiryTask() {
	checkinBonusExpiryOnce.Do(func() {
		if err := model.UpsertCheckinBonusProcessLease(
			checkinBonusProcessId,
			common.NodeName,
			common.StartTime,
			time.Now().Unix(),
		); err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("check-in bonus process lease registration failed: %v", err))
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("check-in bonus maintenance task started: tick=%s", checkinBonusExpiryTickInterval))
			ticker := time.NewTicker(checkinBonusExpiryTickInterval)
			defer ticker.Stop()
			runCheckinBonusExpiryOnce(time.Now())
			for now := range ticker.C {
				runCheckinBonusExpiryOnce(now)
			}
		})
	})
}

func runCheckinBonusExpiryOnce(now time.Time) {
	if !checkinBonusExpiryRunning.CompareAndSwap(false, true) {
		return
	}
	defer checkinBonusExpiryRunning.Store(false)

	if err := model.UpsertCheckinBonusProcessLease(
		checkinBonusProcessId,
		common.NodeName,
		common.StartTime,
		now.Unix(),
	); err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("check-in bonus process lease heartbeat failed: %v", err))
		return
	}

	recovered, err := model.RecoverOrphanedCheckinBonusUsages(
		checkinBonusProcessId,
		now,
		checkinBonusLeaseStaleAfter,
		checkinBonusRecoveryBatchSize,
	)
	if err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("check-in bonus orphan recovery failed: %v", err))
		return
	}
	if recovered > 0 {
		logger.LogInfo(context.Background(), fmt.Sprintf("check-in bonus orphan recovery refunded %d reservation(s)", recovered))
	}
	if !common.IsMasterNode {
		return
	}

	total := int64(0)
	for {
		expired, err := model.ExpireCheckinBonuses(now.Unix(), checkinBonusExpiryBatchSize)
		if err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("check-in bonus expiry task failed: %v", err))
			return
		}
		total += expired
		if expired < checkinBonusExpiryBatchSize {
			break
		}
	}
	if common.DebugEnabled && total > 0 {
		logger.LogInfo(context.Background(), fmt.Sprintf("check-in bonus expiry task marked %d records expired", total))
	}
}
