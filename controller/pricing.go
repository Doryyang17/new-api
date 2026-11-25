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
		// The pricing cache is shared by all viewers.
		groups := make([]string, 0, len(item.EnableGroup))
		for _, group := range item.EnableGroup {
			if _, ok := usableGroup[group]; ok || group == "all" {
				groups = append(groups, group)
			}
		}
		if len(groups) > 0 {
			item.EnableGroup = groups
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func GetPricing(c *gin.Context) {
	group := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	levelKey := common.GetContextKeyString(c, constant.ContextKeyUserLevel)
	if userID := c.GetInt("id"); group == "" && userID > 0 {
		if user, err := model.GetUserCache(userID); err == nil {
			group, levelKey = user.Group, user.LevelKey
		}
	}

	usableGroup := service.GetUserUsableGroups(group)
	pricing := filterPricingByUsableGroups(model.GetPricing(), usableGroup)
	groupRatio := ratio_setting.GetGroupRatioCopy()
	baseGroupRatio := make(map[string]float64, len(groupRatio))
	for g, ratio := range groupRatio {
		if _, ok := usableGroup[g]; !ok {
			delete(groupRatio, g)
			continue
		}
		baseGroupRatio[g] = ratio
		if group != "" {
			info := service.ResolveUserGroupRatio(group, g, levelKey)
			baseGroupRatio[g], groupRatio[g] = info.BaseGroupRatio, info.GroupRatio
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
		common.ApiError(c, err)
		return
	}
	err = ratio_setting.UpdateModelRatioByJSONString(defaultStr)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccessT(c, "Model ratios reset", nil)
}
