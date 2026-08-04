package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/user_level_setting"

	"github.com/gin-gonic/gin"
)

func filterPricingByUsableGroups(pricing []model.Pricing, usableGroup map[string]string) []model.Pricing {
	if len(pricing) == 0 {
		return pricing
	}
	if len(usableGroup) == 0 {
		return []model.Pricing{}
	}

	filtered := make([]model.Pricing, 0, len(pricing))
	for _, item := range pricing {
		if common.StringsContains(item.EnableGroup, "all") {
			filtered = append(filtered, item)
			continue
		}
		for _, group := range item.EnableGroup {
			if _, ok := usableGroup[group]; ok {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered
}

func GetPricing(c *gin.Context) {
	pricing := model.GetPricing()
	userId, exists := c.Get("id")
	usableGroup := map[string]string{}
	groupRatio := map[string]float64{}
	baseGroupRatio := map[string]float64{}
	for s, f := range ratio_setting.GetGroupRatioCopy() {
		groupRatio[s] = f
		baseGroupRatio[s] = f
	}
	var group string
	var levelKey string
	if exists {
		group = common.GetContextKeyString(c, constant.ContextKeyUserGroup)
		levelKey = common.GetContextKeyString(c, constant.ContextKeyUserLevel)
		if group == "" {
			user, err := model.GetUserCache(userId.(int))
			if err == nil {
				group = user.Group
				levelKey = user.LevelKey
			}
		}
		if group != "" {
			for g := range groupRatio {
				ratioInfo := service.ResolveUserGroupRatio(group, g, levelKey)
				baseGroupRatio[g] = ratioInfo.BaseGroupRatio
				groupRatio[g] = ratioInfo.GroupRatio
			}
		}
	}

	usableGroup = service.GetUserUsableGroups(group)
	pricing = filterPricingByUsableGroups(pricing, usableGroup)
	// check groupRatio contains usableGroup
	for group := range ratio_setting.GetGroupRatioCopy() {
		if _, ok := usableGroup[group]; !ok {
			delete(groupRatio, group)
			delete(baseGroupRatio, group)
		}
	}
	level := user_level_setting.ResolveBillingLevel(levelKey)

	c.JSON(200, gin.H{
		"success":          true,
		"data":             pricing,
		"vendors":          model.GetVendors(),
		"group_ratio":      groupRatio,
		"base_group_ratio": baseGroupRatio,
		"user_level": gin.H{
			"enabled": level.Enabled,
			"id":      level.ID,
			"name":    level.Name,
			"ratio":   level.Ratio,
		},
		"usable_group":       usableGroup,
		"supported_endpoint": model.GetSupportedEndpointMap(),
		"auto_groups":        service.GetUserAutoGroup(group),
		"pricing_version":    "a42d372ccf0b5dd13ecf71203521f9d2",
	})
}

func ResetModelRatio(c *gin.Context) {
	defaultStr := ratio_setting.DefaultModelRatio2JSONString()
	err := model.UpdateOption("ModelRatio", defaultStr)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	err = ratio_setting.UpdateModelRatioByJSONString(defaultStr)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(200, gin.H{
		"success": true,
		"message": "重置模型倍率成功",
	})
}
