package controller

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/user_level_setting"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUserLevelProgressHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMainDatabaseType, previousLogDatabaseType := common.MainDatabaseType(), common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB, model.LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Task{},
		&model.Midjourney{},
		&model.SystemTask{},
		&model.SystemTaskLock{},
	))

	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestUserLevelProgressHandlerRunsWhenTaskPollingIsDisabled(t *testing.T) {
	db := setupUserLevelProgressHandlerTestDB(t)
	previousConfig := user_level_setting.GetConfig()
	t.Cleanup(func() { applyUserLevelProgressHandlerConfig(t, previousConfig) })
	applyUserLevelProgressHandlerConfig(t, userLevelProgressHandlerConfig(true))
	previousUpdateTask := constant.UpdateTask
	previousTaskQueryLimit := constant.TaskQueryLimit
	constant.UpdateTask = false
	constant.TaskQueryLimit = 100
	t.Cleanup(func() {
		constant.UpdateTask = previousUpdateTask
		constant.TaskQueryLimit = previousTaskQueryLimit
	})

	user := model.User{Id: 1, Username: "level-reconcile-user", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&model.Task{
		TaskID:                "level-reconcile-task",
		UserId:                user.Id,
		Quota:                 100,
		Status:                model.TaskStatusSuccess,
		Progress:              "100%",
		LevelProgressEligible: true,
		LevelProgressPending:  true,
	}).Error)
	require.NoError(t, db.Create(&model.Midjourney{
		UserId:                user.Id,
		MjId:                  "level-reconcile-midjourney",
		Quota:                 200,
		Status:                string(model.TaskStatusSuccess),
		Progress:              "100%",
		BillingStatus:         model.MidjourneyBillingStatusCharged,
		LevelProgressEligible: true,
		LevelProgressPending:  true,
	}).Error)

	handler := userLevelProgressHandler{}
	assert.False(t, asyncTaskPollHandler{}.Enabled())
	assert.False(t, midjourneyPollHandler{}.Enabled())
	require.True(t, handler.Enabled())

	task, err := model.CreateSystemTask(handler.Type(), nil, nil)
	require.NoError(t, err)
	claimedTask, claimed, err := model.ClaimSystemTask(task.ID, handler.Type(), "level-progress-runner", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, claimed)
	handler.Run(context.Background(), claimedTask, "level-progress-runner")

	var reloadedUser model.User
	require.NoError(t, db.First(&reloadedUser, user.Id).Error)
	assert.EqualValues(t, 300, reloadedUser.LevelUsageQuota)
	assert.False(t, handler.Enabled())

	finished, err := model.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, finished)
	assert.Equal(t, model.SystemTaskStatusSucceeded, finished.Status)
}

func userLevelProgressHandlerConfig(enabled bool) user_level_setting.UserLevelConfig {
	configValue := user_level_setting.DefaultConfig()
	configValue.Enabled = enabled
	return configValue
}

func applyUserLevelProgressHandlerConfig(t *testing.T, value user_level_setting.UserLevelConfig) {
	t.Helper()
	data, err := common.Marshal(value)
	require.NoError(t, err)
	module := config.GlobalConfig.Get("user_level_setting")
	require.NotNil(t, module)
	require.NoError(t, config.UpdateConfigFromMap(module, map[string]string{"config": string(data)}))
}

func TestUserLevelProgressHandlerReconcilesPendingUsageWhileDiscountIsDisabled(t *testing.T) {
	db := setupUserLevelProgressHandlerTestDB(t)
	previousConfig := user_level_setting.GetConfig()
	t.Cleanup(func() { applyUserLevelProgressHandlerConfig(t, previousConfig) })
	applyUserLevelProgressHandlerConfig(t, userLevelProgressHandlerConfig(false))
	previousTaskQueryLimit := constant.TaskQueryLimit
	constant.TaskQueryLimit = 100
	t.Cleanup(func() { constant.TaskQueryLimit = previousTaskQueryLimit })

	user := model.User{Id: 2, Username: "level-reconcile-disabled", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&model.Task{
		TaskID:                "level-reconcile-disabled-task",
		UserId:                user.Id,
		Quota:                 100,
		Status:                model.TaskStatusSuccess,
		Progress:              "100%",
		LevelProgressEligible: true,
		LevelProgressPending:  true,
	}).Error)

	handler := userLevelProgressHandler{}
	require.True(t, handler.Enabled())
	task, err := model.CreateSystemTask(handler.Type(), nil, nil)
	require.NoError(t, err)
	claimedTask, claimed, err := model.ClaimSystemTask(
		task.ID,
		handler.Type(),
		"level-progress-disabled-runner",
		common.GetTimestamp()+60,
	)
	require.NoError(t, err)
	require.True(t, claimed)
	handler.Run(context.Background(), claimedTask, "level-progress-disabled-runner")

	var reloaded model.User
	require.NoError(t, db.First(&reloaded, user.Id).Error)
	assert.EqualValues(t, 100, reloaded.LevelUsageQuota)
	assert.False(t, handler.Enabled())
}
