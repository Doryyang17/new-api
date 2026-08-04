package model

import (
	"github.com/QuantumNous/new-api/setting/user_level_setting"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	taskLevelProgressPendingCondition = "status IN ?"
	mjLevelProgressPendingCondition   = "((status = ? AND progress = ? AND billing_status = ?) OR status = ? OR billing_status IN ?)"
)

func levelConsumedQuotaUpdateExpr(delta int64) clause.Expr {
	current := "COALESCE(level_consumed_quota, 0)"
	return gorm.Expr(
		"CASE WHEN "+current+" + ? < 0 THEN 0 WHEN "+current+" + ? > ? THEN ? ELSE "+current+" + ? END",
		delta,
		delta,
		user_level_setting.MaxThresholdQuota,
		user_level_setting.MaxThresholdQuota,
		delta,
	)
}

func adjustUserLevelConsumedQuotaTx(tx *gorm.DB, userId int, delta int64) error {
	if delta == 0 {
		return nil
	}
	return tx.Model(&User{}).
		Where("id = ?", userId).
		Update("level_consumed_quota", levelConsumedQuotaUpdateExpr(delta)).Error
}

// ReconcileTaskLevelConsumedQuota makes the amount represented by an async
// task match its terminal state. SUCCESS contributes the final task quota;
// FAILURE contributes zero. The task row and user progress change atomically.
func ReconcileTaskLevelConsumedQuota(taskId int64) (bool, error) {
	if taskId <= 0 {
		return false, nil
	}
	changed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var task Task
		if err := lockForUpdate(tx).
			Select("id", "user_id", "quota", "status", "level_progress_eligible", "level_progress_pending", "level_progress_quota").
			First(&task, taskId).Error; err != nil {
			return err
		}
		if !task.LevelProgressEligible {
			return nil
		}

		var desired int64
		switch task.Status {
		case TaskStatusSuccess:
			desired = int64(max(task.Quota, 0))
		case TaskStatusFailure:
			desired = 0
		default:
			return nil
		}
		if desired == task.LevelProgressQuota && !task.LevelProgressPending {
			return nil
		}
		if err := adjustUserLevelConsumedQuotaTx(tx, task.UserId, desired-task.LevelProgressQuota); err != nil {
			return err
		}
		update := tx.Model(&Task{}).
			Where("id = ?", task.ID).
			Updates(map[string]interface{}{
				"level_progress_quota":   desired,
				"level_progress_pending": false,
			})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		changed = true
		return nil
	})
	return changed, err
}

func ReconcilePendingTaskLevelProgress(limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	var ids []int64
	err := DB.Model(&Task{}).
		Where("level_progress_eligible = ? AND level_progress_pending = ?", true, true).
		Where(taskLevelProgressPendingCondition, []TaskStatus{TaskStatusSuccess, TaskStatusFailure}).
		Order("id").
		Limit(limit).
		Pluck("id", &ids).Error
	if err != nil {
		return 0, err
	}
	reconciled := 0
	for _, id := range ids {
		changed, reconcileErr := ReconcileTaskLevelConsumedQuota(id)
		if reconcileErr != nil {
			return reconciled, reconcileErr
		}
		if changed {
			reconciled++
		}
	}
	return reconciled, nil
}

func HasPendingTaskLevelProgress() bool {
	var id int64
	err := DB.Model(&Task{}).
		Where("level_progress_eligible = ? AND level_progress_pending = ?", true, true).
		Where(taskLevelProgressPendingCondition, []TaskStatus{TaskStatusSuccess, TaskStatusFailure}).
		Limit(1).
		Pluck("id", &id).Error
	return err == nil && id != 0
}

func ReconcileMidjourneyLevelConsumedQuota(taskId int) (bool, error) {
	if taskId <= 0 {
		return false, nil
	}
	changed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var task Midjourney
		if err := lockForUpdate(tx).
			Select("id", "user_id", "quota", "status", "progress", "billing_status", "level_progress_eligible", "level_progress_pending", "level_progress_quota").
			First(&task, taskId).Error; err != nil {
			return err
		}
		if !task.LevelProgressEligible {
			return nil
		}

		terminal := false
		desired := int64(0)
		switch {
		case task.Status == string(TaskStatusSuccess) && task.Progress == "100%" && task.BillingStatus == MidjourneyBillingStatusCharged:
			terminal = true
			desired = int64(max(task.Quota, 0))
		case task.Status == string(TaskStatusFailure) ||
			task.BillingStatus == MidjourneyBillingStatusRefunded ||
			task.BillingStatus == MidjourneyBillingStatusFailed:
			terminal = true
		}
		if !terminal || (desired == task.LevelProgressQuota && !task.LevelProgressPending) {
			return nil
		}
		if err := adjustUserLevelConsumedQuotaTx(tx, task.UserId, desired-task.LevelProgressQuota); err != nil {
			return err
		}
		update := tx.Model(&Midjourney{}).
			Where("id = ?", task.Id).
			Updates(map[string]interface{}{
				"level_progress_quota":   desired,
				"level_progress_pending": false,
			})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		changed = true
		return nil
	})
	return changed, err
}

func ReconcilePendingMidjourneyLevelProgress(limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	var ids []int
	err := DB.Model(&Midjourney{}).
		Where("level_progress_eligible = ? AND level_progress_pending = ?", true, true).
		Where(
			mjLevelProgressPendingCondition,
			string(TaskStatusSuccess), "100%", MidjourneyBillingStatusCharged,
			string(TaskStatusFailure), []string{MidjourneyBillingStatusRefunded, MidjourneyBillingStatusFailed},
		).
		Order("id").
		Limit(limit).
		Pluck("id", &ids).Error
	if err != nil {
		return 0, err
	}
	reconciled := 0
	for _, id := range ids {
		changed, reconcileErr := ReconcileMidjourneyLevelConsumedQuota(id)
		if reconcileErr != nil {
			return reconciled, reconcileErr
		}
		if changed {
			reconciled++
		}
	}
	return reconciled, nil
}

func HasPendingMidjourneyLevelProgress() bool {
	var id int
	err := DB.Model(&Midjourney{}).
		Where("level_progress_eligible = ? AND level_progress_pending = ?", true, true).
		Where(
			mjLevelProgressPendingCondition,
			string(TaskStatusSuccess), "100%", MidjourneyBillingStatusCharged,
			string(TaskStatusFailure), []string{MidjourneyBillingStatusRefunded, MidjourneyBillingStatusFailed},
		).
		Limit(1).
		Pluck("id", &id).Error
	return err == nil && id != 0
}
