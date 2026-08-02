package service

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

func normalizeGroupDescriptionMap(groups map[string]string) map[string]string {
	cleaned := make(map[string]string, len(groups))
	for groupName, desc := range groups {
		groupName = strings.TrimSpace(groupName)
		if groupName == "" {
			continue
		}
		cleaned[groupName] = strings.TrimSpace(desc)
	}
	return cleaned
}

func GetUserUsableGroups(userGroup string) map[string]string {
	userGroup = strings.TrimSpace(userGroup)
	groupsCopy := normalizeGroupDescriptionMap(setting.GetUserUsableGroupsCopy())
	if userGroup != "" {
		specialSettings, ok := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.Get(userGroup)
		if ok {
			for specialGroup, desc := range specialSettings {
				specialGroup = strings.TrimSpace(specialGroup)
				if strings.HasPrefix(specialGroup, "-:") {
					groupToRemove := strings.TrimSpace(strings.TrimPrefix(specialGroup, "-:"))
					if groupToRemove != "" {
						delete(groupsCopy, groupToRemove)
					}
				} else if strings.HasPrefix(specialGroup, "+:") {
					groupToAdd := strings.TrimSpace(strings.TrimPrefix(specialGroup, "+:"))
					if groupToAdd != "" {
						groupsCopy[groupToAdd] = strings.TrimSpace(desc)
					}
				} else if specialGroup != "" {
					groupsCopy[specialGroup] = strings.TrimSpace(desc)
				}
			}
		}
		if _, ok := groupsCopy[userGroup]; !ok {
			groupsCopy[userGroup] = "用户分组"
		}
	}
	return groupsCopy
}

func GetUserUsableGroupsByUser(userId int, userGroup string) map[string]string {
	groups := GetUserUsableGroups(userGroup)
	grantGroups, err := model.GetActiveSubscriptionGrantGroups(userId)
	if err != nil {
		return groups
	}
	for _, group := range grantGroups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		if _, ok := ratio_setting.GetGroupRatioCopy()[group]; !ok {
			continue
		}
		if _, ok := groups[group]; !ok {
			groups[group] = setting.GetUsableGroupDescription(group)
		}
	}
	return groups
}

func GroupInUserUsableGroups(userGroup, groupName string) bool {
	_, ok := GetUserUsableGroups(strings.TrimSpace(userGroup))[strings.TrimSpace(groupName)]
	return ok
}

func GroupInUserUsableGroupsByUser(userId int, userGroup, groupName string) bool {
	_, ok := GetUserUsableGroupsByUser(userId, strings.TrimSpace(userGroup))[strings.TrimSpace(groupName)]
	return ok
}

func IsUserSelectableGroup(userGroup, groupName string) bool {
	groupName = strings.TrimSpace(groupName)
	if groupName == "" || groupName == "auto" {
		return false
	}
	return GroupInUserUsableGroups(userGroup, groupName) && ratio_setting.ContainsGroupRatio(groupName)
}

func GetUserAutoGroup(userGroup string) []string {
	autoGroups := make([]string, 0)
	seen := make(map[string]struct{})
	for _, group := range setting.GetAutoGroups() {
		if !IsUserSelectableGroup(userGroup, group) {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		autoGroups = append(autoGroups, group)
	}
	return autoGroups
}

func GetUserAutoGroupByUser(userId int, userGroup string) []string {
	groups := GetUserUsableGroupsByUser(userId, userGroup)
	autoGroups := make([]string, 0)
	for _, group := range setting.GetAutoGroups() {
		if _, ok := groups[group]; ok {
			autoGroups = append(autoGroups, group)
		}
	}
	return autoGroups
}

func FilterUserTokenAutoGroups(userGroup string, groups []string) []string {
	maxCount := setting.GetMaxTokenAutoGroups()
	filtered := make([]string, 0, min(len(groups), maxCount))
	seen := make(map[string]struct{})
	for _, group := range groups {
		if !IsUserSelectableGroup(userGroup, group) {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		filtered = append(filtered, group)
		if len(filtered) == maxCount {
			break
		}
	}
	return filtered
}

func GetRequestAutoGroups(c *gin.Context, userGroup string) []string {
	value, ok := common.GetContextKey(c, constant.ContextKeyTokenAutoGroups)
	if !ok {
		return GetUserAutoGroup(userGroup)
	}
	groups, ok := value.([]string)
	if !ok {
		return []string{}
	}
	return FilterUserTokenAutoGroups(userGroup, groups)
}

func GetGroupsEnabledModels(groups []string) []string {
	seen := make(map[string]struct{})
	models := make([]string, 0)
	for _, group := range groups {
		for _, modelName := range model.GetGroupEnabledModels(group) {
			if _, ok := seen[modelName]; ok {
				continue
			}
			seen[modelName] = struct{}{}
			models = append(models, modelName)
		}
	}
	return models
}

func GetUserGroupRatio(userGroup, group string) float64 {
	ratio, ok := ratio_setting.GetGroupGroupRatio(
		strings.TrimSpace(userGroup),
		strings.TrimSpace(group),
	)
	if ok {
		return ratio
	}
	return ratio_setting.GetGroupRatio(group)
}
