package controller

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetAnnouncements(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	result, err := service.ListAnnouncements(c.GetInt("id"), pageInfo.GetPage(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func GetAnnouncementUnreadCount(c *gin.Context) {
	unreadCount, err := service.CountUnreadAnnouncements(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"unread_count": unreadCount})
}

func GetPublicAnnouncements(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	common.ApiSuccess(c, service.ListPublicAnnouncements(pageInfo.GetPage(), pageInfo.GetPageSize()))
}

func GetUnreadRequiredAnnouncements(c *gin.Context) {
	announcements, err := service.ListUnreadRequiredAnnouncements(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, announcements)
}

func MarkAnnouncementRead(c *gin.Context) {
	readAt, newlyRead, err := service.MarkAnnouncementRead(c.GetInt("id"), c.Param("key"))
	if err != nil {
		if errors.Is(err, service.ErrAnnouncementNotFound) {
			common.ApiErrorMsg(c, err.Error())
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"read_at": readAt, "newly_read": newlyRead})
}

func GetAnnouncementStats(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	result, err := service.ListAnnouncementStats(pageInfo.GetPage(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func GetAnnouncementUnreadUsers(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	if pageInfo.Page < 1 {
		pageInfo.Page = 1
	}
	if pageInfo.PageSize < 1 {
		pageInfo.PageSize = common.ItemsPerPage
	}
	users, total, err := service.ListAnnouncementUnreadUsers(c.Param("key"), pageInfo.GetPage(), pageInfo.GetPageSize())
	if err != nil {
		if errors.Is(err, service.ErrAnnouncementNotFound) {
			common.ApiErrorMsg(c, err.Error())
			return
		}
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(users)
	common.ApiSuccess(c, pageInfo)
}
