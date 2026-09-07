package relay

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"testing"
)

func originTaskPinContext(t *testing.T, taskID string) (*gin.Context, *model.Channel) {
	t.Helper()
	previousDB := model.DB
	previousType := common.MainDatabaseType()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}))
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetMainDatabaseType(previousType)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	channel := &model.Channel{Name: "origin-channel", Status: common.ChannelStatusEnabled, Type: constant.ChannelTypeDoubaoVideo}
	require.NoError(t, db.Create(channel).Error)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/vendor/jobs", nil)
	common.SetContextKey(c, constant.ContextKeyUserId, 7)
	common.SetContextKey(c, constant.ContextKeyOriginTasks, []*model.Task{{TaskID: taskID, Action: "text_to_video", Status: model.TaskStatusSuccess, PrivateData: model.TaskPrivateData{UpstreamTaskID: "upstream-" + taskID}}})
	service.GetChannelConstraints(c).AddPin(dto.ChannelPin{ChannelId: channel.Id, Source: dto.PinSourceOriginTask, Rank: dto.PinRankOriginTask, RetryMode: dto.PinRetrySameChannel})
	return c, channel
}

func TestApplyOriginTaskAffinitySetsLockedChannel(t *testing.T) {
	c, channel := originTaskPinContext(t, "task-lock")

	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	taskErr := ApplyOriginTaskAffinity(c, info)
	require.Nil(t, taskErr)
	locked, ok := info.LockedChannel.(*model.Channel)
	require.True(t, ok)
	require.NotNil(t, locked)
	assert.Equal(t, channel.Id, locked.Id)
	require.Len(t, info.OriginTasks, 1)
	assert.Equal(t, "task-lock", info.OriginTasks[0].TaskID)
	assert.Equal(t, "upstream-task-lock", info.OriginTasks[0].UpstreamTaskID)
	assert.Equal(t, "text_to_video", info.OriginTasks[0].Action)
	assert.Equal(t, string(model.TaskStatusSuccess), info.OriginTasks[0].Status)
}

func TestApplyChannelPinLocksOnlySameChannelRetry(t *testing.T) {
	c, channel := originTaskPinContext(t, "task-lock-mode")

	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	require.Nil(t, ApplyChannelPin(c, info))
	locked, ok := info.LockedChannel.(*model.Channel)
	require.True(t, ok)
	assert.Equal(t, channel.Id, locked.Id)

	tokenOnly, _ := gin.CreateTestContext(httptest.NewRecorder())
	service.GetChannelConstraints(tokenOnly).AddPin(dto.ChannelPin{
		ChannelId: channel.Id,
		Source:    dto.PinSourceToken,
		Rank:      dto.PinRankToken,
		RetryMode: dto.PinRetrySingleAttempt,
	})
	tokenInfo := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	require.Nil(t, ApplyChannelPin(tokenOnly, tokenInfo))
	assert.Nil(t, tokenInfo.LockedChannel)
}
