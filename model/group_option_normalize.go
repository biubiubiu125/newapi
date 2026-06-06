package model

import (
	"encoding/json"
	"strings"
)

func normalizeJSONMapFloat(value string) (string, error) {
	next := make(map[string]float64)
	if err := json.Unmarshal([]byte(value), &next); err != nil {
		return "", err
	}
	cleaned := make(map[string]float64, len(next))
	for name, ratio := range next {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		cleaned[name] = ratio
	}
	bytes, err := json.Marshal(cleaned)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func normalizeJSONMapString(value string) (string, error) {
	next := make(map[string]string)
	if err := json.Unmarshal([]byte(value), &next); err != nil {
		return "", err
	}
	cleaned := make(map[string]string, len(next))
	for name, desc := range next {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		cleaned[name] = strings.TrimSpace(desc)
	}
	bytes, err := json.Marshal(cleaned)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func normalizeJSONGroupGroupRatio(value string) (string, error) {
	next := make(map[string]map[string]float64)
	if err := json.Unmarshal([]byte(value), &next); err != nil {
		return "", err
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
	bytes, err := json.Marshal(cleaned)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func normalizeJSONArrayStrings(value string) (string, error) {
	next := make([]string, 0)
	if err := json.Unmarshal([]byte(value), &next); err != nil {
		return "", err
	}
	seen := make(map[string]struct{}, len(next))
	cleaned := make([]string, 0, len(next))
	for _, name := range next {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		cleaned = append(cleaned, name)
	}
	bytes, err := json.Marshal(cleaned)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func normalizeJSONGroupSpecialUsable(value string) (string, error) {
	next := make(map[string]map[string]string)
	if err := json.Unmarshal([]byte(value), &next); err != nil {
		return "", err
	}
	cleaned := make(map[string]map[string]string, len(next))
	for userGroup, rules := range next {
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
			cleaned[userGroup] = cleanedRules
		}
	}
	bytes, err := json.Marshal(cleaned)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func normalizeOptionValueForStorage(key string, value string) (string, error) {
	switch key {
	case "GroupRatio", "TopupGroupRatio":
		return normalizeJSONMapFloat(value)
	case "UserUsableGroups":
		return normalizeJSONMapString(value)
	case "GroupGroupRatio":
		return normalizeJSONGroupGroupRatio(value)
	case "AutoGroups":
		return normalizeJSONArrayStrings(value)
	case "group_ratio_setting.group_special_usable_group":
		return normalizeJSONGroupSpecialUsable(value)
	default:
		return value, nil
	}
}
