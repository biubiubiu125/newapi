package model

import (
	"errors"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

func normalizeJSONMapFloat(value string) (string, error) {
	next := make(map[string]float64)
	if err := common.Unmarshal([]byte(value), &next); err != nil {
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
	bytes, err := common.Marshal(cleaned)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func normalizeJSONMapString(value string) (string, error) {
	next := make(map[string]string)
	if err := common.Unmarshal([]byte(value), &next); err != nil {
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
	bytes, err := common.Marshal(cleaned)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func normalizeJSONGroupGroupRatio(value string) (string, error) {
	next := make(map[string]map[string]float64)
	if err := common.Unmarshal([]byte(value), &next); err != nil {
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
	bytes, err := common.Marshal(cleaned)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func normalizeJSONArrayStrings(value string) (string, error) {
	next := make([]string, 0)
	if err := common.Unmarshal([]byte(value), &next); err != nil {
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
	bytes, err := common.Marshal(cleaned)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func normalizeJSONGroupSpecialUsable(value string) (string, error) {
	next := make(map[string]map[string]string)
	if err := common.Unmarshal([]byte(value), &next); err != nil {
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
	bytes, err := common.Marshal(cleaned)
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
	case "PayAddress",
		"CustomCallbackAddress",
		"TaskPublicAddress",
		"EpayId",
		"EpayKey",
		"StripeApiSecret",
		"StripeWebhookSecret",
		"StripePriceId",
		"CreemApiKey",
		"CreemProducts",
		"CreemWebhookSecret",
		"BEpusdtBaseURL",
		"BEpusdtPID",
		"BEpusdtSecretKey",
		"BEpusdtDisplayName",
		"BEpusdtAssetDisplayNames",
		"WaffoApiKey",
		"WaffoPrivateKey",
		"WaffoPublicCert",
		"WaffoSandboxPublicCert",
		"WaffoSandboxApiKey",
		"WaffoSandboxPrivateKey",
		"WaffoMerchantId",
		"WaffoNotifyUrl",
		"WaffoReturnUrl",
		"WaffoSubscriptionReturnUrl",
		"WaffoCurrency",
		"WaffoPancakeMerchantID",
		"WaffoPancakePrivateKey",
		"WaffoPancakeReturnURL",
		"WaffoPancakeCurrency",
		"WaffoPancakeStoreID",
		"WaffoPancakeProductID":
		return strings.TrimSpace(value), nil
	default:
		return value, nil
	}
}

func validateTaskPublicAddressValue(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return errors.New("task artifact base URL is empty")
	}
	if raw != trimmed {
		return errors.New("task artifact base URL must not contain surrounding whitespace")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed == nil {
		return errors.New("task artifact base URL is invalid")
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return errors.New("task artifact base URL must use http or https")
	}
	if parsed.Host == "" || parsed.User != nil || parsed.Opaque != "" {
		return errors.New("task artifact base URL must contain a host and no userinfo")
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.Contains(trimmed, "#") {
		return errors.New("task artifact base URL must not contain a query or fragment")
	}
	return nil
}
