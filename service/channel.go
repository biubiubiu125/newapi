package service

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
)

const BalanceInsufficientDisableReasonPrefix = "[balance_insufficient]"

func formatNotifyType(channelId int, status int) string {
	return fmt.Sprintf("%s_%d_%d", dto.NotifyTypeChannelUpdate, channelId, status)
}

// disable & notify
func DisableChannel(channelError types.ChannelError, reason string) {
	isBalanceInsufficient := IsBalanceInsufficientMessage(reason)
	if isBalanceInsufficient && !strings.HasPrefix(reason, BalanceInsufficientDisableReasonPrefix) {
		reason = BalanceInsufficientDisableReasonPrefix + " " + reason
	}
	common.SysLog(fmt.Sprintf("通道「%s」（#%d）发生错误，准备禁用，原因：%s", channelError.ChannelName, channelError.ChannelId, common.LocalLogPreview(reason)))

	// 检查是否启用自动禁用功能
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

	lowerMessage := strings.ToLower(err.Error())
	search, _ := AcSearch(lowerMessage, operation_setting.AutomaticDisableKeywords, true)
	return search
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
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" {
		return false
	}
	if strings.Contains(message, BalanceInsufficientDisableReasonPrefix) {
		return true
	}
	search, _ := AcSearch(message, operation_setting.BalanceInsufficientKeywords, true)
	return search
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
