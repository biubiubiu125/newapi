package console_setting

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
)

var (
	maxAnnouncementContentCharacters = 2000
	maxAnnouncementTitleCharacters   = 100
	urlRegex                         = regexp.MustCompile(`^https?://(?:(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)*[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?|(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?))(?:\:[0-9]{1,5})?(?:/.*)?$`)
	dangerousChars                   = []string{"<script", "<iframe", "javascript:", "onload=", "onerror=", "onclick="}
	validColors                      = map[string]bool{
		"blue": true, "green": true, "cyan": true, "purple": true, "pink": true,
		"red": true, "orange": true, "amber": true, "yellow": true, "lime": true,
		"light-green": true, "teal": true, "light-blue": true, "indigo": true,
		"violet": true, "grey": true, "slate": true,
	}
	slugRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

func localized(key string, args ...map[string]any) error {
	return common.Localized(key, args...)
}

func localizedIndex(key string, index int) error {
	return localized(key, map[string]any{"Index": index})
}

func localizedIndexMax(key string, index, max int) error {
	return localized(key, map[string]any{"Index": index, "Max": max})
}

func parseJSONArray(jsonStr string, invalidKey string) ([]map[string]interface{}, error) {
	var list []map[string]interface{}
	if err := common.UnmarshalJsonStr(jsonStr, &list); err != nil {
		return nil, localized(invalidKey, map[string]any{"Error": err.Error()})
	}
	return list, nil
}

func exceedsMaxCharacters(s string, max int) bool {
	return len(utf16.Encode([]rune(s))) > max
}

func validateURL(urlStr string, index int, invalidKey, parseKey string) error {
	if !urlRegex.MatchString(urlStr) {
		return localizedIndex(invalidKey, index)
	}
	if _, err := url.Parse(urlStr); err != nil {
		return localized(parseKey, map[string]any{"Index": index, "Error": err.Error()})
	}
	return nil
}

func checkDangerousContent(content string, index int, key string) error {
	lower := strings.ToLower(content)
	for _, d := range dangerousChars {
		if strings.Contains(lower, d) {
			return localizedIndex(key, index)
		}
	}
	return nil
}

func getJSONList(jsonStr string) []map[string]interface{} {
	if jsonStr == "" {
		return []map[string]interface{}{}
	}
	var list []map[string]interface{}
	_ = common.UnmarshalJsonStr(jsonStr, &list)
	return list
}

func ValidateConsoleSettings(settingsStr string, settingType string) error {
	if settingsStr == "" {
		return nil
	}

	switch settingType {
	case "ApiInfo":
		return validateApiInfo(settingsStr)
	case "Announcements":
		return validateAnnouncements(settingsStr)
	case "FAQ":
		return validateFAQ(settingsStr)
	case "UptimeKumaGroups":
		return validateUptimeKumaGroups(settingsStr)
	default:
		return localized(i18n.MsgConsoleUnknownSettingType, map[string]any{"Type": settingType})
	}
}

func validateApiInfo(apiInfoStr string) error {
	apiInfoList, err := parseJSONArray(apiInfoStr, i18n.MsgConsoleAPIInfoJSONInvalid)
	if err != nil {
		return err
	}

	if len(apiInfoList) > 50 {
		return localized(i18n.MsgConsoleAPIInfoMax)
	}

	for i, apiInfo := range apiInfoList {
		urlStr, ok := apiInfo["url"].(string)
		if !ok || urlStr == "" {
			return localizedIndex(i18n.MsgConsoleAPIInfoMissingURL, i+1)
		}
		route, ok := apiInfo["route"].(string)
		if !ok || route == "" {
			return localizedIndex(i18n.MsgConsoleAPIInfoMissingRoute, i+1)
		}
		description, ok := apiInfo["description"].(string)
		if !ok || description == "" {
			return localizedIndex(i18n.MsgConsoleAPIInfoMissingDescription, i+1)
		}
		color, ok := apiInfo["color"].(string)
		if !ok || color == "" {
			return localizedIndex(i18n.MsgConsoleAPIInfoMissingColor, i+1)
		}

		if err := validateURL(urlStr, i+1, i18n.MsgConsoleAPIInfoURLInvalid, i18n.MsgConsoleAPIInfoURLParseFailed); err != nil {
			return err
		}

		if exceedsMaxCharacters(urlStr, 500) {
			return localizedIndex(i18n.MsgConsoleAPIInfoURLTooLong, i+1)
		}
		if exceedsMaxCharacters(route, 100) {
			return localizedIndex(i18n.MsgConsoleAPIInfoRouteTooLong, i+1)
		}
		if exceedsMaxCharacters(description, 200) {
			return localizedIndex(i18n.MsgConsoleAPIInfoDescriptionTooLong, i+1)
		}

		if !validColors[color] {
			return localizedIndex(i18n.MsgConsoleAPIInfoColorInvalid, i+1)
		}

		if err := checkDangerousContent(description, i+1, i18n.MsgConsoleAPIInfoDangerous); err != nil {
			return err
		}
		if err := checkDangerousContent(route, i+1, i18n.MsgConsoleAPIInfoDangerous); err != nil {
			return err
		}
	}
	return nil
}

func GetApiInfo() []map[string]interface{} {
	return getJSONList(GetConsoleSetting().ApiInfo)
}

func validateAnnouncements(announcementsStr string) error {
	list, err := parseJSONArray(announcementsStr, i18n.MsgConsoleAnnouncementJSONInvalid)
	if err != nil {
		return err
	}
	if len(list) > 100 {
		return localized(i18n.MsgConsoleAnnouncementMax)
	}
	validTypes := map[string]bool{
		"default": true, "ongoing": true, "success": true, "warning": true, "error": true,
	}
	for i, ann := range list {
		content, ok := ann["content"].(string)
		if !ok || content == "" {
			return localizedIndex(i18n.MsgConsoleAnnouncementMissingContent, i+1)
		}
		publishDateAny, exists := ann["publishDate"]
		if !exists {
			return localizedIndex(i18n.MsgConsoleAnnouncementMissingPublishDate, i+1)
		}
		publishDateStr, ok := publishDateAny.(string)
		if !ok || publishDateStr == "" {
			return localizedIndex(i18n.MsgConsoleAnnouncementPublishDateEmpty, i+1)
		}
		if _, err := time.Parse(time.RFC3339, publishDateStr); err != nil {
			return localizedIndex(i18n.MsgConsoleAnnouncementPublishDateInvalid, i+1)
		}
		if t, exists := ann["type"]; exists {
			if typeStr, ok := t.(string); ok {
				if !validTypes[typeStr] {
					return localizedIndex(i18n.MsgConsoleAnnouncementTypeInvalid, i+1)
				}
			}
		}
		if title, exists := ann["title"]; exists {
			if titleStr, ok := title.(string); ok && exceedsMaxCharacters(titleStr, maxAnnouncementTitleCharacters) {
				return localizedIndexMax(i18n.MsgConsoleAnnouncementTitleTooLong, i+1, maxAnnouncementTitleCharacters)
			}
		}
		if exceedsMaxCharacters(content, maxAnnouncementContentCharacters) {
			return localizedIndexMax(i18n.MsgConsoleAnnouncementContentTooLong, i+1, maxAnnouncementContentCharacters)
		}
		if extra, exists := ann["extra"]; exists {
			if extraStr, ok := extra.(string); ok && exceedsMaxCharacters(extraStr, 100) {
				return localizedIndex(i18n.MsgConsoleAnnouncementExtraTooLong, i+1)
			}
		}
	}
	return nil
}

func validateFAQ(faqStr string) error {
	list, err := parseJSONArray(faqStr, i18n.MsgConsoleFAQJSONInvalid)
	if err != nil {
		return err
	}
	if len(list) > 100 {
		return localized(i18n.MsgConsoleFAQMax)
	}
	for i, faq := range list {
		question, ok := faq["question"].(string)
		if !ok || question == "" {
			return localizedIndex(i18n.MsgConsoleFAQMissingQuestion, i+1)
		}
		answer, ok := faq["answer"].(string)
		if !ok || answer == "" {
			return localizedIndex(i18n.MsgConsoleFAQMissingAnswer, i+1)
		}
		if exceedsMaxCharacters(question, 200) {
			return localizedIndex(i18n.MsgConsoleFAQQuestionTooLong, i+1)
		}
		if exceedsMaxCharacters(answer, 1000) {
			return localizedIndex(i18n.MsgConsoleFAQAnswerTooLong, i+1)
		}
	}
	return nil
}

func getPublishTime(item map[string]interface{}) time.Time {
	if v, ok := item["publishDate"]; ok {
		if s, ok2 := v.(string); ok2 {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

func GetAnnouncements() []map[string]interface{} {
	list := getJSONList(GetConsoleSetting().Announcements)
	sort.SliceStable(list, func(i, j int) bool {
		return getPublishTime(list[i]).After(getPublishTime(list[j]))
	})
	return list
}

func GetFAQ() []map[string]interface{} {
	return getJSONList(GetConsoleSetting().FAQ)
}

func validateUptimeKumaGroups(groupsStr string) error {
	groups, err := parseJSONArray(groupsStr, i18n.MsgConsoleUptimeGroupJSONInvalid)
	if err != nil {
		return err
	}

	if len(groups) > 20 {
		return localized(i18n.MsgConsoleUptimeGroupMax)
	}

	nameSet := make(map[string]bool)

	for i, group := range groups {
		categoryName, ok := group["categoryName"].(string)
		if !ok || categoryName == "" {
			return localizedIndex(i18n.MsgConsoleUptimeGroupMissingName, i+1)
		}
		if nameSet[categoryName] {
			return localizedIndex(i18n.MsgConsoleUptimeGroupDuplicateName, i+1)
		}
		nameSet[categoryName] = true
		urlStr, ok := group["url"].(string)
		if !ok || urlStr == "" {
			return localizedIndex(i18n.MsgConsoleUptimeGroupMissingURL, i+1)
		}
		slug, ok := group["slug"].(string)
		if !ok || slug == "" {
			return localizedIndex(i18n.MsgConsoleUptimeGroupMissingSlug, i+1)
		}
		description, ok := group["description"].(string)
		if !ok {
			description = ""
		}

		if err := validateURL(urlStr, i+1, i18n.MsgConsoleUptimeGroupURLInvalid, i18n.MsgConsoleUptimeGroupURLParseFailed); err != nil {
			return err
		}

		if exceedsMaxCharacters(categoryName, 50) {
			return localizedIndex(i18n.MsgConsoleUptimeGroupNameTooLong, i+1)
		}
		if exceedsMaxCharacters(urlStr, 500) {
			return localizedIndex(i18n.MsgConsoleUptimeGroupURLTooLong, i+1)
		}
		if exceedsMaxCharacters(slug, 100) {
			return localizedIndex(i18n.MsgConsoleUptimeGroupSlugTooLong, i+1)
		}
		if exceedsMaxCharacters(description, 200) {
			return localizedIndex(i18n.MsgConsoleUptimeGroupDescriptionTooLong, i+1)
		}

		if !slugRegex.MatchString(slug) {
			return localizedIndex(i18n.MsgConsoleUptimeGroupSlugCharset, i+1)
		}

		if err := checkDangerousContent(description, i+1, i18n.MsgConsoleUptimeGroupDangerous); err != nil {
			return err
		}
		if err := checkDangerousContent(categoryName, i+1, i18n.MsgConsoleUptimeGroupDangerous); err != nil {
			return err
		}
	}
	return nil
}

func GetUptimeKumaGroups() []map[string]interface{} {
	return getJSONList(GetConsoleSetting().UptimeKumaGroups)
}
