package service

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/announcementkey"
	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupAnnouncementServiceTest(t *testing.T) {
	t.Helper()
	previousDB := model.DB
	settings := console_setting.GetConsoleSetting()
	previousAnnouncements := settings.Announcements
	previousEnabled := settings.AnnouncementsEnabled

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.AnnouncementRead{}))
	model.DB = db
	settings.AnnouncementsEnabled = true
	t.Cleanup(func() {
		model.DB = previousDB
		settings.Announcements = previousAnnouncements
		settings.AnnouncementsEnabled = previousEnabled
	})
}

func TestAnnouncementKeyKeepsExplicitIdStable(t *testing.T) {
	first := buildAnnouncementKey(announcementConfig{Id: "notice-1", Content: "旧内容"})
	second := buildAnnouncementKey(announcementConfig{Id: "notice-1", Content: "更新后的内容"})
	assert.Equal(t, first, second)
}

func TestAnnouncementKeyKeepsLegacyReceiptsWhenIDIsMigrated(t *testing.T) {
	publishDate := "2026-07-25T08:00:00Z"
	content := "旧公告正文"
	legacyKey := buildAnnouncementKey(announcementConfig{
		Content:     content,
		PublishDate: publishDate,
	})
	migratedKey := buildAnnouncementKey(announcementConfig{
		Id:          announcementkey.LegacyID(publishDate, content),
		Content:     "编辑后的公告正文",
		PublishDate: "2026-07-26T08:00:00Z",
	})

	assert.Equal(t, legacyKey, migratedKey)
}

func TestMandatoryAnnouncementReadFlowAndStats(t *testing.T) {
	setupAnnouncementServiceTest(t)
	require.NoError(t, model.DB.Create(&[]model.User{
		{Id: 1, Username: "reader", Password: "password", AffCode: "service-1", Status: common.UserStatusEnabled},
		{Id: 2, Username: "pending", Password: "password", AffCode: "service-2", Status: common.UserStatusEnabled},
	}).Error)

	now := time.Now()
	configs := []announcementConfig{
		{
			Id:          "mandatory",
			Content:     "维护通知",
			PublishDate: now.Add(-time.Hour).Format(time.RFC3339),
			Type:        "warning",
			ForceRead:   true,
		},
		{
			Id:          "future",
			Title:       "未来公告",
			Content:     "尚未发布",
			PublishDate: now.Add(time.Hour).Format(time.RFC3339),
			Level:       "urgent",
			ForceRead:   true,
		},
	}
	encoded, err := common.Marshal(configs)
	require.NoError(t, err)
	console_setting.GetConsoleSetting().Announcements = string(encoded)

	public := PublicAnnouncements()
	require.Len(t, public, 1)
	assert.Equal(t, "维护通知", public[0].Title)
	assert.Equal(t, "important", public[0].Level)

	mandatory, err := ListUnreadRequiredAnnouncements(1)
	require.NoError(t, err)
	require.Len(t, mandatory, 1)
	unreadCount, err := CountUnreadAnnouncements(1)
	require.NoError(t, err)
	assert.Equal(t, 1, unreadCount)

	readAt, newlyRead, err := MarkAnnouncementRead(1, mandatory[0].Key)
	require.NoError(t, err)
	assert.Positive(t, readAt)
	assert.True(t, newlyRead)
	mandatory, err = ListUnreadRequiredAnnouncements(1)
	require.NoError(t, err)
	assert.Empty(t, mandatory)
	unreadCount, err = CountUnreadAnnouncements(1)
	require.NoError(t, err)
	assert.Zero(t, unreadCount)

	page, err := ListAnnouncements(1, 1, 20)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.True(t, page.Items[0].Read)
	assert.Zero(t, page.UnreadCount)

	stats, err := ListAnnouncementStats(1, 20)
	require.NoError(t, err)
	require.Len(t, stats.Items, 2)
	statsByKey := make(map[string]AnnouncementStat, len(stats.Items))
	for _, item := range stats.Items {
		statsByKey[item.Key] = item
	}
	mandatoryStat := statsByKey[public[0].Key]
	assert.EqualValues(t, 1, mandatoryStat.ReadCount)
	assert.EqualValues(t, 1, mandatoryStat.UnreadCount)
	assert.InDelta(t, 50, mandatoryStat.ReadRate, 0.001)
	for _, item := range stats.Items {
		if item.Key != public[0].Key {
			assert.False(t, item.Published)
		}
	}
}

func TestUnreadUserPaginationNormalizesInvalidValues(t *testing.T) {
	setupAnnouncementServiceTest(t)
	users := make([]model.User, common.ItemsPerPage+5)
	for i := range users {
		users[i] = model.User{
			Id:       i + 1,
			Username: fmt.Sprintf("user-%d", i+1),
			Password: "password",
			AffCode:  fmt.Sprintf("pagination-%d", i+1),
			Status:   common.UserStatusEnabled,
		}
	}
	require.NoError(t, model.DB.Create(&users).Error)

	configs := []announcementConfig{{
		Id:          "pagination",
		Content:     "分页公告",
		PublishDate: time.Now().Add(-time.Hour).Format(time.RFC3339),
	}}
	encoded, err := common.Marshal(configs)
	require.NoError(t, err)
	console_setting.GetConsoleSetting().Announcements = string(encoded)
	announcements := PublicAnnouncements()
	require.Len(t, announcements, 1)

	unreadUsers, total, err := ListAnnouncementUnreadUsers(
		announcements[0].Key,
		-1,
		-1,
	)
	require.NoError(t, err)
	assert.EqualValues(t, len(users), total)
	assert.Len(t, unreadUsers, common.ItemsPerPage)
}

func TestAnnouncementPaginationRejectsOverflowingPage(t *testing.T) {
	setupAnnouncementServiceTest(t)
	require.NoError(t, model.DB.Create(&model.User{
		Id:       1,
		Username: "overflow-user",
		Password: "password",
		AffCode:  "overflow-user",
		Status:   common.UserStatusEnabled,
	}).Error)

	content := "完整公告正文"
	configs := []announcementConfig{{
		Id:          "overflow-announcement",
		Content:     content,
		Extra:       "公告摘要",
		PublishDate: time.Now().Add(-time.Hour).Format(time.RFC3339),
	}}
	encoded, err := common.Marshal(configs)
	require.NoError(t, err)
	console_setting.GetConsoleSetting().Announcements = string(encoded)

	page, err := ListAnnouncements(1, math.MaxInt, 100)
	require.NoError(t, err)
	assert.Empty(t, page.Items)
	assert.Equal(t, 1, page.Total)

	stats, err := ListAnnouncementStats(math.MaxInt, 100)
	require.NoError(t, err)
	assert.Empty(t, stats.Items)
	assert.Equal(t, 1, stats.Total)

	unreadUsers, total, err := ListAnnouncementUnreadUsers(
		PublicAnnouncements()[0].Key,
		math.MaxInt,
		100,
	)
	require.NoError(t, err)
	assert.Empty(t, unreadUsers)
	assert.EqualValues(t, 1, total)

	previews := PublicAnnouncementPreviews(1)
	require.Len(t, previews, 1)
	assert.Equal(t, "公告摘要", previews[0].Content)
	publicPage := ListPublicAnnouncements(1, 1)
	require.Len(t, publicPage.Items, 1)
	assert.Equal(t, content, publicPage.Items[0].Content)
}
