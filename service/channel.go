package service

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

const BalanceInsufficientDisableReasonPrefix = "[balance_insufficient]"

var strictBalanceInsufficientPhrases = []string{
	"your credit balance is too low",
	"credit balance is too low",
	"you exceeded your current quota",
	"exceeded your current quota",
	"insufficient quota",
	"insufficient_quota",
	"quota exceeded",
	"quota_exceeded",
	"insufficient balance",
	"balance insufficient",
	"balance_insufficient",
	"insufficient credit",
	"insufficient credits",
	"not enough credit",
	"not enough credits",
	"out of credits",
	"credit exhausted",
	"credits exhausted",
	"billing hard limit",
	"billing_hard_limit",
	"billing quota",
	"余额不足",
	"额度不足",
	"余额不够",
	"额度不够",
}

var broadBalanceInsufficientKeywords = map[string]struct{}{
	"balance":      {},
	"quota":        {},
	"insufficient": {},
	"billing":      {},
	"credit":       {},
}

var balanceInsufficientErrorFields = map[string]struct{}{
	"code":           {},
	"detail":         {},
	"details":        {},
	"error":          {},
	"error_code":     {},
	"message":        {},
	"msg":            {},
	"reason":         {},
	"status_message": {},
	"type":           {},
}

func formatNotifyType(channelId int, status int) string {
	return fmt.Sprintf("%s_%d_%d", dto.NotifyTypeChannelUpdate, channelId, status)
}

// disable & notify
func DisableChannel(channelError types.ChannelError, reason string) {
	isBalanceInsufficient := IsBalanceInsufficientMessage(reason)
	if isBalanceInsufficient && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(reason)), BalanceInsufficientDisableReasonPrefix) {
		reason = BalanceInsufficientDisableReasonPrefix + " " + reason
	}
	common.SysLog(fmt.Sprintf("通道「%s」（#%d）发生错误，准备禁用，原因：%s", channelError.ChannelName, channelError.ChannelId, common.LocalLogPreview(reason)))

	if !channelError.AutoBan && !isBalanceInsufficient {
		common.SysLog(fmt.Sprintf("通道「%s」（#%d）未启用自动禁用功能，跳过禁用操作", channelError.ChannelName, channelError.ChannelId))
		return
	}

	success := model.UpdateChannelStatus(channelError.ChannelId, channelError.UsingKey, common.ChannelStatusAutoDisabled, reason)
	if success {
		subject := fmt.Sprintf("通道「%s」（#%d）已被禁用", channelError.ChannelName, channelError.ChannelId)
		content := fmt.Sprintf("通道「%s」（#%d）已被禁用，原因：%s", channelError.ChannelName, channelError.ChannelId, reason)
		NotifyRootUser(formatNotifyType(channelError.ChannelId, common.ChannelStatusAutoDisabled), subject, content)
	}
}

func EnableChannel(channelId int, usingKey string, channelName string) {
	success := model.UpdateChannelStatus(channelId, usingKey, common.ChannelStatusEnabled, "")
	if success {
		subject := fmt.Sprintf("通道「%s」（#%d）已被启用", channelName, channelId)
		content := fmt.Sprintf("通道「%s」（#%d）已被启用", channelName, channelId)
		NotifyRootUser(formatNotifyType(channelId, common.ChannelStatusEnabled), subject, content)
	}
}

func ShouldDisableChannel(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	if IsBalanceInsufficientError(err) {
		return true
	}
	if !common.AutomaticDisableChannelEnabled {
		return false
	}
	if types.IsChannelError(err) {
		return true
	}
	if types.IsSkipRetryError(err) {
		return false
	}
	if operation_setting.ShouldDisableByStatusCode(err.StatusCode) {
		return true
	}

	return shouldDisableByMessageKeywords(err.Error())
}

func ShouldEnableChannel(newAPIError *types.NewAPIError, status int) bool {
	if !common.AutomaticEnableChannelEnabled {
		return false
	}
	if newAPIError != nil {
		return false
	}
	if status != common.ChannelStatusAutoDisabled {
		return false
	}
	return true
}

func ShouldEnableChannelForChannel(newAPIError *types.NewAPIError, channel *model.Channel) bool {
	if channel == nil {
		return false
	}
	if !ShouldEnableChannel(newAPIError, channel.Status) {
		return false
	}
	if ChannelHasBalanceInsufficientDisableReason(channel) {
		return false
	}
	return true
}

func IsBalanceInsufficientError(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	if err.StatusCode >= http.StatusBadRequest && err.StatusCode <= http.StatusNetworkAuthenticationRequired {
		return IsBalanceInsufficientMessage(err.Error())
	}
	return IsBalanceInsufficientMessage(err.Error())
}

func IsBalanceInsufficientMessage(message string) bool {
	message = strings.TrimSpace(message)
	if message == "" {
		return false
	}
	if strings.HasPrefix(strings.ToLower(message), BalanceInsufficientDisableReasonPrefix) {
		message = strings.TrimSpace(message[len(BalanceInsufficientDisableReasonPrefix):])
		if message == "" {
			return false
		}
	}
	// 请求体里可能包含 prompt 等用户文本，余额不足只从错误文本或结构化错误字段里判断。
	for _, candidate := range balanceInsufficientMessageCandidates(message) {
		if containsBalanceInsufficientPhrase(candidate) {
			return true
		}
	}
	return false
}

func shouldDisableByMessageKeywords(message string) bool {
	for _, candidate := range balanceInsufficientMessageCandidates(message) {
		search, _ := AcSearch(strings.ToLower(candidate), operation_setting.AutomaticDisableKeywords, true)
		if search {
			return true
		}
	}
	return false
}

func balanceInsufficientMessageCandidates(message string) []string {
	candidates := make([]string, 0, 4)
	if plainText := strings.TrimSpace(stripJSONPayload(message)); plainText != "" {
		candidates = append(candidates, plainText)
	}
	if jsonCandidates := balanceInsufficientJSONCandidates(message); len(jsonCandidates) > 0 {
		candidates = append(candidates, jsonCandidates...)
	}
	if len(candidates) == 0 {
		candidates = append(candidates, message)
	}
	return candidates
}

func stripJSONPayload(message string) string {
	if idx := strings.Index(message, "{"); idx >= 0 {
		return message[:idx]
	}
	return message
}

func balanceInsufficientJSONCandidates(message string) []string {
	idx := strings.Index(message, "{")
	if idx < 0 {
		return nil
	}
	var payload any
	if err := common.DecodeJsonUseNumber(strings.NewReader(message[idx:]), &payload); err != nil {
		return nil
	}
	candidates := make([]string, 0, 4)
	collectBalanceInsufficientJSONCandidates(payload, "", &candidates)
	return candidates
}

func collectBalanceInsufficientJSONCandidates(value any, key string, candidates *[]string) {
	switch typedValue := value.(type) {
	case map[string]any:
		for childKey, childValue := range typedValue {
			normalizedKey := strings.ToLower(strings.TrimSpace(childKey))
			if !isBalanceInsufficientErrorField(normalizedKey) {
				continue
			}
			collectBalanceInsufficientJSONCandidates(childValue, normalizedKey, candidates)
		}
	case []any:
		if key != "" && !isBalanceInsufficientErrorField(key) {
			return
		}
		for _, childValue := range typedValue {
			collectBalanceInsufficientJSONCandidates(childValue, key, candidates)
		}
	case string:
		if isBalanceInsufficientErrorField(key) {
			*candidates = append(*candidates, typedValue)
		}
	}
}

func isBalanceInsufficientErrorField(key string) bool {
	_, ok := balanceInsufficientErrorFields[key]
	return ok
}

func containsBalanceInsufficientPhrase(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" {
		return false
	}
	for _, phrase := range strictBalanceInsufficientPhrases {
		if strings.Contains(message, strings.ToLower(phrase)) {
			return true
		}
	}
	for _, phrase := range operation_setting.BalanceInsufficientKeywords {
		phrase = strings.ToLower(strings.TrimSpace(phrase))
		if phrase == "" {
			continue
		}
		if _, broad := broadBalanceInsufficientKeywords[phrase]; broad {
			continue
		}
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}

func ChannelHasBalanceInsufficientDisableReason(channel *model.Channel) bool {
	if channel == nil {
		return false
	}
	info := channel.GetOtherInfo()
	if reason, ok := info["status_reason"].(string); ok && IsBalanceInsufficientMessage(reason) {
		return true
	}
	for _, reason := range channel.ChannelInfo.MultiKeyDisabledReason {
		if IsBalanceInsufficientMessage(reason) {
			return true
		}
	}
	return false
}
