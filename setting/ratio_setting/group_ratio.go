package ratio_setting

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/types"
)

var defaultGroupRatio = map[string]float64{
	"default": 1,
	"vip":     1,
	"svip":    1,
}

var groupRatioMap = types.NewRWMap[string, float64]()

var defaultGroupGroupRatio = map[string]map[string]float64{
	"vip": {
		"edit_this": 0.9,
	},
}

var groupGroupRatioMap = types.NewRWMap[string, map[string]float64]()

var defaultGroupSpecialUsableGroup = map[string]map[string]string{}

type GroupRatioSetting struct {
	GroupRatio              *types.RWMap[string, float64]            `json:"group_ratio"`
	GroupGroupRatio         *types.RWMap[string, map[string]float64] `json:"group_group_ratio"`
	GroupSpecialUsableGroup *types.RWMap[string, map[string]string]  `json:"group_special_usable_group"`
}

var groupRatioSetting GroupRatioSetting

func init() {
	groupSpecialUsableGroup := types.NewRWMap[string, map[string]string]()
	groupSpecialUsableGroup.AddAll(defaultGroupSpecialUsableGroup)

	groupRatioMap.AddAll(defaultGroupRatio)
	groupGroupRatioMap.AddAll(defaultGroupGroupRatio)

	groupRatioSetting = GroupRatioSetting{
		GroupSpecialUsableGroup: groupSpecialUsableGroup,
		GroupRatio:              groupRatioMap,
		GroupGroupRatio:         groupGroupRatioMap,
	}

	config.GlobalConfig.Register("group_ratio_setting", &groupRatioSetting)
}

func GetGroupRatioSetting() *GroupRatioSetting {
	if groupRatioSetting.GroupSpecialUsableGroup == nil {
		groupRatioSetting.GroupSpecialUsableGroup = types.NewRWMap[string, map[string]string]()
		groupRatioSetting.GroupSpecialUsableGroup.AddAll(defaultGroupSpecialUsableGroup)
	}
	return &groupRatioSetting
}

func NormalizeGroupSpecialUsableGroup() {
	setting := GetGroupRatioSetting()
	cleaned := types.NewRWMap[string, map[string]string]()
	for userGroup, rules := range setting.GroupSpecialUsableGroup.ReadAll() {
		userGroup = strings.TrimSpace(userGroup)
		if userGroup == "" {
			continue
		}
		cleanedRules := make(map[string]string, len(rules))
		for rawGroup, desc := range rules {
			rawGroup = strings.TrimSpace(rawGroup)
			if rawGroup == "" {
				continue
			}
			prefix := ""
			groupName := rawGroup
			if strings.HasPrefix(rawGroup, "-:") || strings.HasPrefix(rawGroup, "+:") {
				prefix = rawGroup[:2]
				groupName = strings.TrimSpace(rawGroup[2:])
			}
			if groupName == "" {
				continue
			}
			cleanedRules[prefix+groupName] = strings.TrimSpace(desc)
		}
		if len(cleanedRules) > 0 {
			cleaned.Set(userGroup, cleanedRules)
		}
	}
	setting.GroupSpecialUsableGroup = cleaned
}

func GetGroupRatioCopy() map[string]float64 {
	return groupRatioMap.ReadAll()
}

func ContainsGroupRatio(name string) bool {
	name = strings.TrimSpace(name)
	_, ok := groupRatioMap.Get(name)
	return ok
}

func GroupRatio2JSONString() string {
	return groupRatioMap.MarshalJSONString()
}

func UpdateGroupRatioByJSONString(jsonStr string) error {
	next := make(map[string]float64)
	if err := common.Unmarshal([]byte(jsonStr), &next); err != nil {
		return err
	}
	cleaned := make(map[string]float64, len(next))
	for name, ratio := range next {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		cleaned[name] = ratio
	}
	groupRatioMap.ReplaceAll(cleaned)
	return nil
}

func GetGroupRatio(name string) float64 {
	name = strings.TrimSpace(name)
	ratio, ok := groupRatioMap.Get(name)
	if !ok {
		common.SysLog("group ratio not found: " + name)
		return 1
	}
	return ratio
}

func GetGroupGroupRatio(userGroup, usingGroup string) (float64, bool) {
	userGroup = strings.TrimSpace(userGroup)
	usingGroup = strings.TrimSpace(usingGroup)
	gp, ok := groupGroupRatioMap.Get(userGroup)
	if !ok {
		return -1, false
	}
	ratio, ok := gp[usingGroup]
	if !ok {
		return -1, false
	}
	return ratio, true
}

func GroupGroupRatio2JSONString() string {
	return groupGroupRatioMap.MarshalJSONString()
}

func UpdateGroupGroupRatioByJSONString(jsonStr string) error {
	next := make(map[string]map[string]float64)
	if err := common.Unmarshal([]byte(jsonStr), &next); err != nil {
		return err
	}
	cleaned := make(map[string]map[string]float64, len(next))
	for userGroup, ratios := range next {
		userGroup = strings.TrimSpace(userGroup)
		if userGroup == "" {
			continue
		}
		cleanedRatios := make(map[string]float64, len(ratios))
		for targetGroup, ratio := range ratios {
			targetGroup = strings.TrimSpace(targetGroup)
			if targetGroup == "" {
				continue
			}
			cleanedRatios[targetGroup] = ratio
		}
		if len(cleanedRatios) > 0 {
			cleaned[userGroup] = cleanedRatios
		}
	}
	groupGroupRatioMap.ReplaceAll(cleaned)
	return nil
}

func CheckGroupRatio(jsonStr string) error {
	checkGroupRatio := make(map[string]float64)
	err := common.Unmarshal([]byte(jsonStr), &checkGroupRatio)
	if err != nil {
		return err
	}
	for name, ratio := range checkGroupRatio {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if ratio < 0 {
			return errors.New("group ratio must be not less than 0: " + name)
		}
	}
	return nil
}
