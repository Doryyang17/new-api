package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGrantUserQuotaBatchIsAtomicVisibleAndIdempotent(t *testing.T) {
	truncateTables(t)
	users := []*User{
		{Id: 1, Username: "quota-grant-root", Password: "password123", AffCode: "grant-root", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Quota: 100},
		{Id: 2, Username: "quota-grant-enabled", Password: "password123", AffCode: "grant-enabled", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 100},
		{Id: 3, Username: "quota-grant-disabled", Password: "password123", AffCode: "grant-disabled", Role: common.RoleCommonUser, Status: common.UserStatusDisabled, Quota: 200},
		{Id: 4, Username: "quota-grant-admin", Password: "password123", AffCode: "grant-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, Quota: 300},
	}
	for _, user := range users {
		require.NoError(t, DB.Create(user).Error)
	}

	requestId := uuid.NewString()
	params := QuotaGrantBatchParams{
		RequestId:      requestId,
		OperatorUserId: 1,
		OperatorName:   users[0].Username,
		TargetUserIds:  []int{4, 2, 3, 2},
		Quota:          5000,
		AmountUsd:      "0.01",
		Reason:         "迁移补偿",
		Ip:             "192.0.2.1",
		AdminInfo:      map[string]interface{}{"admin_id": 1},
	}

	result, err := GrantUserQuotaBatch(params)
	require.NoError(t, err)
	assert.False(t, result.AlreadyProcessed)
	assert.Equal(t, 3, result.Batch.TargetCount)

	for userId, expectedQuota := range map[int]int{2: 5100, 3: 5200, 4: 5300} {
		var user User
		require.NoError(t, DB.First(&user, userId).Error)
		assert.Equal(t, expectedQuota, user.Quota)

		var log Log
		require.NoError(t, LOG_DB.Where("request_id = ? AND user_id = ?", requestId, userId).First(&log).Error)
		assert.Equal(t, LogTypeManage, log.Type)
		assert.Equal(t, user.Username, log.Username)
		assert.Empty(t, log.Ip)
		other, mapErr := common.StrToMap(log.Other)
		require.NoError(t, mapErr)
		op, ok := other["op"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "user.quota_grant", op["action"])
		paramsMap, ok := op["params"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "迁移补偿", paramsMap["reason"])
		assert.Equal(t, "0.01", paramsMap["amount_usd"])
	}

	var logCount int64
	require.NoError(t, LOG_DB.Model(&Log{}).Where("request_id = ?", requestId).Count(&logCount).Error)
	assert.EqualValues(t, 4, logCount)
	var operatorLog Log
	require.NoError(t, LOG_DB.Where("request_id = ? AND user_id = ?", requestId, params.OperatorUserId).First(&operatorLog).Error)
	assert.Equal(t, params.Ip, operatorLog.Ip)

	duplicate, err := GrantUserQuotaBatch(params)
	require.NoError(t, err)
	assert.True(t, duplicate.AlreadyProcessed)
	require.NoError(t, LOG_DB.Model(&Log{}).Where("request_id = ?", requestId).Count(&logCount).Error)
	assert.EqualValues(t, 4, logCount)

	var enabledUser User
	require.NoError(t, DB.First(&enabledUser, 2).Error)
	assert.Equal(t, 5100, enabledUser.Quota)

	conflicting := params
	conflicting.Reason = "不同原因"
	_, err = GrantUserQuotaBatch(conflicting)
	assert.ErrorIs(t, err, ErrQuotaGrantIdempotencyConflict)
}

func TestGrantUserQuotaBatchWritesLogsAcrossBatches(t *testing.T) {
	truncateTables(t)
	operator := &User{Id: 10001, Username: "quota-batch-root", Password: "password123", AffCode: "batch-root", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(operator).Error)

	users := make([]*User, 0, quotaGrantLogBatchSize+25)
	userIds := make([]int, 0, quotaGrantLogBatchSize+25)
	for index := 0; index < quotaGrantLogBatchSize+25; index++ {
		id := 11000 + index
		users = append(users, &User{
			Id:       id,
			Username: fmt.Sprintf("quota-batch-user-%d", index),
			Password: "password123",
			AffCode:  fmt.Sprintf("batch-user-%d", index),
			Role:     common.RoleCommonUser,
			Status:   common.UserStatusEnabled,
		})
		userIds = append(userIds, id)
	}
	require.NoError(t, DB.CreateInBatches(&users, 100).Error)

	requestId := uuid.NewString()
	result, err := GrantUserQuotaBatch(QuotaGrantBatchParams{
		RequestId:      requestId,
		OperatorUserId: operator.Id,
		OperatorName:   operator.Username,
		TargetUserIds:  userIds,
		Quota:          100,
		AmountUsd:      "0.01",
		Reason:         "分批写入测试",
	})
	require.NoError(t, err)
	assert.Equal(t, len(userIds), result.Batch.TargetCount)

	var logCount int64
	require.NoError(t, LOG_DB.Model(&Log{}).Where("request_id = ?", requestId).Count(&logCount).Error)
	assert.EqualValues(t, len(userIds)+1, logCount)
}

func TestGrantUserQuotaBatchReturnsCommittedResultWhenCacheSyncIsPending(t *testing.T) {
	truncateTables(t)
	operator := &User{Id: 12001, Username: "quota-retry-root", Password: "password123", AffCode: "retry-root", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	target := &User{Id: 12002, Username: "quota-retry-target", Password: "password123", AffCode: "retry-target", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 100}
	require.NoError(t, DB.Create(operator).Error)
	require.NoError(t, DB.Create(target).Error)

	params := QuotaGrantBatchParams{
		RequestId:      uuid.NewString(),
		OperatorUserId: operator.Id,
		OperatorName:   operator.Username,
		TargetUserIds:  []int{target.Id},
		Quota:          5000,
		AmountUsd:      "0.01",
		Reason:         "缓存重试测试",
	}

	previousRedisEnabled := common.RedisEnabled
	previousRedisClient := common.RDB
	failingRedisClient := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		MaxRetries:   -1,
		DialTimeout:  50 * time.Millisecond,
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 50 * time.Millisecond,
	})
	common.RedisEnabled = true
	common.RDB = failingRedisClient
	t.Cleanup(func() {
		_ = failingRedisClient.Close()
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRedisClient
	})

	result, err := GrantUserQuotaBatch(params)
	require.NoError(t, err)
	assert.False(t, result.AlreadyProcessed)
	assert.True(t, result.CacheSyncPending)

	retryResult, err := GrantUserQuotaBatch(params)
	require.NoError(t, err)
	assert.True(t, retryResult.AlreadyProcessed)
	assert.True(t, retryResult.CacheSyncPending)

	var reloaded User
	require.NoError(t, DB.First(&reloaded, target.Id).Error)
	assert.Equal(t, 5100, reloaded.Quota)
	var logCount int64
	require.NoError(t, LOG_DB.Model(&Log{}).Where("request_id = ?", params.RequestId).Count(&logCount).Error)
	assert.EqualValues(t, 2, logCount)
}

func TestGrantUserQuotaBatchRollsBackOnQuotaOverflow(t *testing.T) {
	truncateTables(t)
	operator := &User{Id: 11, Username: "quota-overflow-root", Password: "password123", AffCode: "overflow-root", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	target := &User{Id: 12, Username: "quota-overflow-target", Password: "password123", AffCode: "overflow-target", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: common.MaxQuota - 10}
	require.NoError(t, DB.Create(operator).Error)
	require.NoError(t, DB.Create(target).Error)

	requestId := uuid.NewString()
	_, err := GrantUserQuotaBatch(QuotaGrantBatchParams{
		RequestId:      requestId,
		OperatorUserId: operator.Id,
		OperatorName:   operator.Username,
		TargetUserIds:  []int{target.Id},
		Quota:          11,
		AmountUsd:      "0.01",
		Reason:         "上限测试",
	})
	require.Error(t, err)

	var reloaded User
	require.NoError(t, DB.First(&reloaded, target.Id).Error)
	assert.Equal(t, common.MaxQuota-10, reloaded.Quota)

	var batchCount int64
	require.NoError(t, DB.Model(&QuotaGrantBatch{}).Where("request_id = ?", requestId).Count(&batchCount).Error)
	assert.Zero(t, batchCount)
	var logCount int64
	require.NoError(t, LOG_DB.Model(&Log{}).Where("request_id = ?", requestId).Count(&logCount).Error)
	assert.Zero(t, logCount)
}

func TestListQuotaGrantTargetsExcludesRootAndDeletedUsers(t *testing.T) {
	truncateTables(t)
	users := []*User{
		{Id: 21, Username: "grant-list-enabled", Password: "password123", AffCode: "list-enabled", Role: common.RoleCommonUser, Status: common.UserStatusEnabled},
		{Id: 22, Username: "grant-list-disabled", Password: "password123", AffCode: "list-disabled", Role: common.RoleCommonUser, Status: common.UserStatusDisabled},
		{Id: 23, Username: "grant-list-admin", Password: "password123", AffCode: "list-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled},
		{Id: 24, Username: "grant-list-root", Password: "password123", AffCode: "list-root", Role: common.RoleRootUser, Status: common.UserStatusEnabled},
	}
	for _, user := range users {
		require.NoError(t, DB.Create(user).Error)
	}
	require.NoError(t, DB.Delete(users[0]).Error)

	targets, total, err := ListQuotaGrantTargets(
		"",
		[]int{common.RoleCommonUser, common.RoleAdminUser},
		[]int{common.UserStatusEnabled, common.UserStatusDisabled},
		0,
		100,
	)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	require.Len(t, targets, 2)
	assert.Equal(t, []int{23, 22}, []int{targets[0].Id, targets[1].Id})
}
