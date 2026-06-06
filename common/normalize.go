package common

import "strings"

func NormalizeNameList(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		normalized = append(normalized, name)
	}
	return normalized
}

func SplitCommaSeparated(value string) []string {
	if value == "" {
		return []string{}
	}
	return NormalizeNameList(strings.Split(value, ","))
}

func NormalizeCommaSeparated(value string) string {
	return strings.Join(SplitCommaSeparated(value), ",")
}
