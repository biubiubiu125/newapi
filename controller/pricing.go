package controller

import (
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

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
		enableGroups := normalizePricingEnableGroups(item.EnableGroup)
		if common.StringsContains(enableGroups, "all") {
			item.EnableGroup = pricingUsableGroupNames(usableGroup)
			if len(item.EnableGroup) == 0 {
				continue
			}
			filtered = append(filtered, item)
			continue
		}
		item.EnableGroup = filterPricingEnableGroups(enableGroups, usableGroup)
		if len(item.EnableGroup) == 0 {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func normalizePricingEnableGroups(enableGroups []string) []string {
	normalized := make([]string, 0, len(enableGroups))
	for _, group := range enableGroups {
		group = strings.TrimSpace(group)
		if group != "" {
			normalized = append(normalized, group)
		}
	}
	return normalized
}

func filterPricingEnableGroups(enableGroups []string, usableGroup map[string]string) []string {
	filtered := make([]string, 0, len(enableGroups))
	seen := make(map[string]bool, len(enableGroups))
	for _, group := range enableGroups {
		if _, ok := usableGroup[group]; !ok {
			continue
		}
		if !seen[group] {
			filtered = append(filtered, group)
			seen[group] = true
		}
	}
	return filtered
}

func pricingUsableGroupNames(usableGroup map[string]string) []string {
	groups := make([]string, 0, len(usableGroup))
	for group := range usableGroup {
		group = strings.TrimSpace(group)
		if group != "" {
			groups = append(groups, group)
		}
	}
	sort.Strings(groups)
	return groups
}

func pricingConfiguredUsableGroups(usableGroup map[string]string, groupRatio map[string]float64) map[string]string {
	filtered := make(map[string]string, len(usableGroup))
	for group, desc := range usableGroup {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		if _, ok := groupRatio[group]; ok {
			filtered[group] = desc
		}
	}
	return filtered
}

func pricingConfiguredAutoGroups(autoGroups []string, usableGroup map[string]string) []string {
	filtered := make([]string, 0, len(autoGroups))
	seen := make(map[string]bool, len(autoGroups))
	for _, group := range autoGroups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		if _, ok := usableGroup[group]; !ok {
			continue
		}
		if !seen[group] {
			filtered = append(filtered, group)
			seen[group] = true
		}
	}
	return filtered
}

func GetPricing(c *gin.Context) {
	pricing := model.GetPricing()
	userId, exists := c.Get("id")
	usableGroup := map[string]string{}
	groupRatio := map[string]float64{}
	for s, f := range ratio_setting.GetGroupRatioCopy() {
		groupRatio[s] = f
	}
	var group string
	userIdValue := 0
	if exists {
		userIdValue = userId.(int)
		user, err := model.GetUserCache(userIdValue)
		if err == nil {
			group = user.Group
			for g := range groupRatio {
				ratio, ok := ratio_setting.GetGroupGroupRatio(group, g)
				if ok {
					groupRatio[g] = ratio
				}
			}
		}
	}

	usableGroup = service.GetUserUsableGroupsByUser(userIdValue, group)
	usableGroup = pricingConfiguredUsableGroups(usableGroup, groupRatio)
	pricing = filterPricingByUsableGroups(pricing, usableGroup)
	// check groupRatio contains usableGroup
	for group := range ratio_setting.GetGroupRatioCopy() {
		if _, ok := usableGroup[group]; !ok {
			delete(groupRatio, group)
		}
	}

	c.JSON(200, gin.H{
		"success":            true,
		"data":               pricing,
		"vendors":            model.GetVendors(),
		"group_ratio":        groupRatio,
		"usable_group":       usableGroup,
		"supported_endpoint": model.GetSupportedEndpointMap(),
		"auto_groups":        pricingConfiguredAutoGroups(service.GetUserAutoGroupByUser(userIdValue, group), usableGroup),
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
