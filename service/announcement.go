package service

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/announcementkey"
	"github.com/QuantumNous/new-api/setting/console_setting"
)

var ErrAnnouncementNotFound = errors.New("公告不存在或尚未发布")

const publicAnnouncementPageSize = 20

type announcementConfig struct {
	Id          any    `json:"id"`
	Title       string `json:"title"`
	Content     string `json:"content"`
	PublishDate string `json:"publishDate"`
	Type        string `json:"type"`
	Level       string `json:"level"`
	ForceRead   bool   `json:"forceRead"`
	Immediate   *bool  `json:"immediate"`
	Extra       string `json:"extra"`
	Category    string `json:"category"`
	Pinned      bool   `json:"pinned"`
	OfflineAt   string `json:"offlineAt"`
}

type Announcement struct {
	Id          any    `json:"id,omitempty"`
	Key         string `json:"key"`
	Title       string `json:"title"`
	Content     string `json:"content"`
	Summary     string `json:"summary,omitempty"`
	PublishDate string `json:"publishDate"`
	Type        string `json:"type,omitempty"`
	Level       string `json:"level"`
	ForceRead   bool   `json:"forceRead"`
	Immediate   bool   `json:"immediate"`
	Extra       string `json:"extra,omitempty"`
	Category    string `json:"category"`
	Pinned      bool   `json:"pinned"`
	OfflineAt   string `json:"offlineAt,omitempty"`
	Published   bool   `json:"published"`
	Read        bool   `json:"read"`
	ReadAt      int64  `json:"readAt,omitempty"`
}

type AnnouncementPage struct {
	Page        int            `json:"page"`
	PageSize    int            `json:"page_size"`
	Total       int            `json:"total"`
	UnreadCount int            `json:"unread_count"`
	Items       []Announcement `json:"items"`
}

type AnnouncementStat struct {
	Announcement
	ReadCount   int64   `json:"read_count"`
	UnreadCount int64   `json:"unread_count"`
	ReadRate    float64 `json:"read_rate"`
}

type AnnouncementStatsPage struct {
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
	Total    int                `json:"total"`
	Items    []AnnouncementStat `json:"items"`
}

func normalizeAnnouncementPage(page int, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = common.ItemsPerPage
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func announcementIdSignature(id any) string {
	switch value := id.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(value)
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	default:
		encoded, err := common.Marshal(value)
		if err != nil {
			return fmt.Sprint(value)
		}
		return string(encoded)
	}
}

func buildAnnouncementKey(config announcementConfig) string {
	signature := announcementIdSignature(config.Id)
	if signature != "" {
		return announcementkey.FromID(signature)
	}
	return announcementkey.Legacy(config.PublishDate, config.Content)
}

func truncateRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit])) + "..."
}

func announcementTitle(config announcementConfig) string {
	if title := strings.TrimSpace(config.Title); title != "" {
		return title
	}
	firstLine := strings.TrimSpace(strings.SplitN(config.Content, "\n", 2)[0])
	return truncateRunes(firstLine, 80)
}

func announcementSummary(config announcementConfig) string {
	if summary := strings.TrimSpace(config.Extra); summary != "" {
		return summary
	}
	return truncateRunes(strings.ReplaceAll(config.Content, "\n", " "), 120)
}

func announcementLevel(config announcementConfig) string {
	switch config.Level {
	case "normal", "important", "urgent":
		return config.Level
	}
	switch config.Type {
	case "error":
		return "urgent"
	case "ongoing", "warning":
		return "important"
	default:
		return "normal"
	}
}

func isAnnouncementPublished(config announcementConfig, now time.Time) bool {
	publishAt, err := time.Parse(time.RFC3339, config.PublishDate)
	if err != nil || publishAt.After(now) {
		return false
	}
	if strings.TrimSpace(config.OfflineAt) == "" {
		return true
	}
	offlineAt, err := time.Parse(time.RFC3339, config.OfflineAt)
	return err == nil && offlineAt.After(now)
}

func announcementPublishTime(announcement Announcement) time.Time {
	publishAt, err := time.Parse(time.RFC3339, announcement.PublishDate)
	if err != nil {
		return time.Time{}
	}
	return publishAt
}

func configuredAnnouncements(now time.Time) []Announcement {
	settings := console_setting.GetConsoleSetting()
	if strings.TrimSpace(settings.Announcements) == "" {
		return []Announcement{}
	}

	var configs []announcementConfig
	if err := common.Unmarshal([]byte(settings.Announcements), &configs); err != nil {
		common.SysError("解析系统公告失败：" + err.Error())
		return []Announcement{}
	}

	announcements := make([]Announcement, 0, len(configs))
	for _, config := range configs {
		immediate := true
		if config.Immediate != nil {
			immediate = *config.Immediate
		}
		category := strings.TrimSpace(config.Category)
		if category == "" {
			category = "system"
		}
		announcements = append(announcements, Announcement{
			Id:          config.Id,
			Key:         buildAnnouncementKey(config),
			Title:       announcementTitle(config),
			Content:     config.Content,
			Summary:     announcementSummary(config),
			PublishDate: config.PublishDate,
			Type:        config.Type,
			Level:       announcementLevel(config),
			ForceRead:   config.ForceRead,
			Immediate:   immediate,
			Extra:       config.Extra,
			Category:    category,
			Pinned:      config.Pinned,
			OfflineAt:   config.OfflineAt,
			Published:   isAnnouncementPublished(config, now),
		})
	}

	sort.SliceStable(announcements, func(i, j int) bool {
		if announcements[i].Pinned != announcements[j].Pinned {
			return announcements[i].Pinned
		}
		return announcementPublishTime(announcements[i]).After(
			announcementPublishTime(announcements[j]),
		)
	})
	return announcements
}

func PublicAnnouncements() []Announcement {
	if !console_setting.GetConsoleSetting().AnnouncementsEnabled {
		return []Announcement{}
	}
	all := configuredAnnouncements(time.Now())
	published := make([]Announcement, 0, len(all))
	for _, announcement := range all {
		if announcement.Published {
			published = append(published, announcement)
		}
	}
	return published
}

func PublicAnnouncementPreviews(limit int) []Announcement {
	announcements := PublicAnnouncements()
	if limit < 0 {
		limit = 0
	}
	if limit < len(announcements) {
		announcements = announcements[:limit]
	}
	previews := make([]Announcement, len(announcements))
	for i, announcement := range announcements {
		announcement.Content = announcement.Summary
		announcement.Read = false
		announcement.ReadAt = 0
		previews[i] = announcement
	}
	return previews
}

func ListPublicAnnouncements(page int, pageSize int) AnnouncementPage {
	page, pageSize = normalizeAnnouncementPage(page, pageSize)
	if pageSize > publicAnnouncementPageSize {
		pageSize = publicAnnouncementPageSize
	}
	announcements := PublicAnnouncements()
	start, end := common.GetPageBounds(int64(len(announcements)), page, pageSize)
	return AnnouncementPage{
		Page:     page,
		PageSize: pageSize,
		Total:    len(announcements),
		Items:    announcements[start:end],
	}
}

func ListAnnouncements(userId int, page int, pageSize int) (AnnouncementPage, error) {
	page, pageSize = normalizeAnnouncementPage(page, pageSize)
	announcements := PublicAnnouncements()
	keys := make([]string, len(announcements))
	for i := range announcements {
		keys[i] = announcements[i].Key
	}
	readTimes, err := model.GetAnnouncementReadTimes(userId, keys)
	if err != nil {
		return AnnouncementPage{}, err
	}

	unreadCount := 0
	for i := range announcements {
		readAt, read := readTimes[announcements[i].Key]
		announcements[i].Read = read
		announcements[i].ReadAt = readAt
		if !read {
			unreadCount++
		}
	}

	start, end := common.GetPageBounds(int64(len(announcements)), page, pageSize)
	return AnnouncementPage{
		Page:        page,
		PageSize:    pageSize,
		Total:       len(announcements),
		UnreadCount: unreadCount,
		Items:       announcements[start:end],
	}, nil
}

func CountUnreadAnnouncements(userId int) (int, error) {
	announcements := PublicAnnouncements()
	keys := make([]string, len(announcements))
	for i := range announcements {
		keys[i] = announcements[i].Key
	}
	readTimes, err := model.GetAnnouncementReadTimes(userId, keys)
	if err != nil {
		return 0, err
	}

	unreadCount := 0
	for _, announcement := range announcements {
		if _, read := readTimes[announcement.Key]; !read {
			unreadCount++
		}
	}
	return unreadCount, nil
}

func ListUnreadRequiredAnnouncements(userId int) ([]Announcement, error) {
	announcements := PublicAnnouncements()
	required := make([]Announcement, 0, len(announcements))
	keys := make([]string, 0, len(announcements))
	for _, announcement := range announcements {
		if announcement.ForceRead {
			required = append(required, announcement)
			keys = append(keys, announcement.Key)
		}
	}
	readTimes, err := model.GetAnnouncementReadTimes(userId, keys)
	if err != nil {
		return nil, err
	}

	unread := make([]Announcement, 0, len(required))
	for _, announcement := range required {
		if _, read := readTimes[announcement.Key]; !read {
			unread = append(unread, announcement)
		}
	}
	sort.SliceStable(unread, func(i, j int) bool {
		return announcementPublishTime(unread[i]).Before(
			announcementPublishTime(unread[j]),
		)
	})
	return unread, nil
}

func MarkAnnouncementRead(userId int, announcementKey string) (int64, bool, error) {
	found := false
	for _, announcement := range PublicAnnouncements() {
		if announcement.Key == announcementKey {
			found = true
			break
		}
	}
	if !found {
		return 0, false, ErrAnnouncementNotFound
	}
	return model.MarkAnnouncementRead(userId, announcementKey)
}

func ListAnnouncementStats(page int, pageSize int) (AnnouncementStatsPage, error) {
	page, pageSize = normalizeAnnouncementPage(page, pageSize)
	announcements := configuredAnnouncements(time.Now())
	keys := make([]string, len(announcements))
	for i := range announcements {
		keys[i] = announcements[i].Key
	}
	readCounts, err := model.GetAnnouncementReadCounts(keys)
	if err != nil {
		return AnnouncementStatsPage{}, err
	}
	eligibleUsers, err := model.CountAnnouncementEligibleUsers()
	if err != nil {
		return AnnouncementStatsPage{}, err
	}

	stats := make([]AnnouncementStat, len(announcements))
	for i, announcement := range announcements {
		readCount := int64(0)
		unreadCount := int64(0)
		readRate := float64(0)
		if announcement.Published {
			readCount = readCounts[announcement.Key]
			if readCount > eligibleUsers {
				readCount = eligibleUsers
			}
			unreadCount = eligibleUsers - readCount
			if eligibleUsers > 0 {
				readRate = float64(readCount) * 100 / float64(eligibleUsers)
			}
		}
		stats[i] = AnnouncementStat{
			Announcement: announcement,
			ReadCount:    readCount,
			UnreadCount:  unreadCount,
			ReadRate:     readRate,
		}
	}

	start, end := common.GetPageBounds(int64(len(stats)), page, pageSize)
	return AnnouncementStatsPage{
		Page:     page,
		PageSize: pageSize,
		Total:    len(stats),
		Items:    stats[start:end],
	}, nil
}

func ListAnnouncementUnreadUsers(announcementKey string, page int, pageSize int) ([]model.AnnouncementUnreadUser, int64, error) {
	found := false
	for _, announcement := range PublicAnnouncements() {
		if announcement.Key == announcementKey {
			found = true
			break
		}
	}
	if !found {
		return nil, 0, ErrAnnouncementNotFound
	}
	page, pageSize = normalizeAnnouncementPage(page, pageSize)
	return model.ListAnnouncementUnreadUsers(announcementKey, page, pageSize)
}
