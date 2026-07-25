package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupAnnouncementReadTestDB(t *testing.T) {
	t.Helper()
	previousDB := DB
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}, &AnnouncementRead{}))
	DB = db
	t.Cleanup(func() {
		DB = previousDB
	})
}

func TestAnnouncementReadReceiptsAndUnreadUsers(t *testing.T) {
	setupAnnouncementReadTestDB(t)
	users := []User{
		{Id: 1, Username: "read-user", Password: "password", AffCode: "read-1", Status: common.UserStatusEnabled},
		{Id: 2, Username: "unread-user", Password: "password", AffCode: "read-2", Status: common.UserStatusEnabled},
		{Id: 3, Username: "disabled-user", Password: "password", AffCode: "read-3", Status: common.UserStatusDisabled},
	}
	require.NoError(t, DB.Create(&users).Error)

	firstReadAt, newlyRead, err := MarkAnnouncementRead(1, strings.Repeat("a", 64))
	require.NoError(t, err)
	assert.True(t, newlyRead)
	secondReadAt, newlyRead, err := MarkAnnouncementRead(1, strings.Repeat("a", 64))
	require.NoError(t, err)
	assert.False(t, newlyRead)
	assert.Equal(t, firstReadAt, secondReadAt)
	require.NoError(t, DB.Create(&AnnouncementRead{
		UserId:          3,
		AnnouncementKey: strings.Repeat("a", 64),
		ReadAt:          firstReadAt,
	}).Error)

	var receiptCount int64
	require.NoError(t, DB.Model(&AnnouncementRead{}).Count(&receiptCount).Error)
	assert.EqualValues(t, 2, receiptCount)

	counts, err := GetAnnouncementReadCounts([]string{strings.Repeat("a", 64)})
	require.NoError(t, err)
	assert.EqualValues(t, 1, counts[strings.Repeat("a", 64)])

	eligibleUsers, err := CountAnnouncementEligibleUsers()
	require.NoError(t, err)
	assert.EqualValues(t, 2, eligibleUsers)

	unreadUsers, total, err := ListAnnouncementUnreadUsers(strings.Repeat("a", 64), 1, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, unreadUsers, 1)
	assert.Equal(t, "unread-user", unreadUsers[0].Username)
}
