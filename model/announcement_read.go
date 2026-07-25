package model

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm/clause"
)

// AnnouncementRead stores one durable read receipt per user and announcement.
type AnnouncementRead struct {
	Id              int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId          int    `json:"user_id" gorm:"column:user_id;not null;uniqueIndex:idx_announcement_reads_user_key,priority:1;index"`
	AnnouncementKey string `json:"announcement_key" gorm:"column:announcement_key;type:char(64);not null;uniqueIndex:idx_announcement_reads_user_key,priority:2;index"`
	ReadAt          int64  `json:"read_at" gorm:"column:read_at;type:bigint;not null;index"`
}

func (AnnouncementRead) TableName() string {
	return "announcement_reads"
}

func GetAnnouncementReadTimes(userId int, announcementKeys []string) (map[string]int64, error) {
	readTimes := make(map[string]int64, len(announcementKeys))
	if len(announcementKeys) == 0 {
		return readTimes, nil
	}

	var reads []AnnouncementRead
	if err := DB.Select("announcement_key", "read_at").
		Where("user_id = ? AND announcement_key IN ?", userId, announcementKeys).
		Find(&reads).Error; err != nil {
		return nil, err
	}
	for _, read := range reads {
		readTimes[read.AnnouncementKey] = read.ReadAt
	}
	return readTimes, nil
}

func MarkAnnouncementRead(userId int, announcementKey string) (int64, bool, error) {
	read := AnnouncementRead{
		UserId:          userId,
		AnnouncementKey: announcementKey,
		ReadAt:          time.Now().Unix(),
	}
	result := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "announcement_key"}},
		DoNothing: true,
	}).Create(&read)
	if result.Error != nil {
		return 0, false, result.Error
	}
	if result.RowsAffected > 0 {
		return read.ReadAt, true, nil
	}

	var existing AnnouncementRead
	if err := DB.Select("read_at").
		Where("user_id = ? AND announcement_key = ?", userId, announcementKey).
		First(&existing).Error; err != nil {
		return 0, false, err
	}
	return existing.ReadAt, false, nil
}

func GetAnnouncementReadCounts(announcementKeys []string) (map[string]int64, error) {
	counts := make(map[string]int64, len(announcementKeys))
	if len(announcementKeys) == 0 {
		return counts, nil
	}

	type countRow struct {
		AnnouncementKey string
		ReadCount       int64
	}
	var rows []countRow
	err := DB.Table("announcement_reads").
		Select("announcement_reads.announcement_key, COUNT(*) AS read_count").
		Joins("JOIN users ON users.id = announcement_reads.user_id").
		Where("announcement_reads.announcement_key IN ?", announcementKeys).
		Where("users.status = ? AND users.deleted_at IS NULL", common.UserStatusEnabled).
		Group("announcement_reads.announcement_key").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		counts[row.AnnouncementKey] = row.ReadCount
	}
	return counts, nil
}

func CountAnnouncementEligibleUsers() (int64, error) {
	var total int64
	err := DB.Model(&User{}).
		Where("status = ?", common.UserStatusEnabled).
		Count(&total).Error
	return total, err
}

type AnnouncementUnreadUser struct {
	Id          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	CreatedAt   int64  `json:"created_at"`
	LastLoginAt int64  `json:"last_login_at"`
}

func ListAnnouncementUnreadUsers(announcementKey string, page int, pageSize int) ([]AnnouncementUnreadUser, int64, error) {
	subquery := DB.Table("announcement_reads").
		Select("1").
		Where("announcement_reads.user_id = users.id").
		Where("announcement_reads.announcement_key = ?", announcementKey)
	query := DB.Model(&User{}).
		Where("status = ?", common.UserStatusEnabled).
		Where("NOT EXISTS (?)", subquery)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	startIdx, endIdx := common.GetPageBounds(total, page, pageSize)
	if startIdx == endIdx {
		return []AnnouncementUnreadUser{}, total, nil
	}

	var users []AnnouncementUnreadUser
	err := query.Select("id", "username", "display_name", "email", "created_at", "last_login_at").
		Order("id DESC").
		Limit(pageSize).
		Offset(startIdx).
		Find(&users).Error
	return users, total, err
}
