package service

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

func GetChannelConstraints(c *gin.Context) *dto.ChannelConstraints {
	if c == nil {
		return &dto.ChannelConstraints{}
	}
	if existing, ok := common.GetContextKeyType[*dto.ChannelConstraints](c, constant.ContextKeyChannelConstraints); ok && existing != nil {
		return existing
	}
	constraints := &dto.ChannelConstraints{}
	common.SetContextKey(c, constant.ContextKeyChannelConstraints, constraints)
	return constraints
}

func AppendTaskPluginIdentityFilter(c *gin.Context, pluginKey string) {
	if c == nil || pluginKey == "" {
		return
	}
	GetChannelConstraints(c).AddFilter(dto.ChannelFilter{
		Kind:                   dto.FilterTaskPluginIdentity,
		TaskPluginKey:          pluginKey,
		TaskPluginKeys:         pinnedTaskPluginKeys(c, pluginKey),
		TaskPluginChannelTypes: pinnedTaskPluginChannelTypes(c, pluginKey),
	})
}

// ChannelSatisfiesConstraints is the final channel-selection guard for
// request-scoped constraints. Unknown constraints fail closed so a newly
// introduced filter cannot silently be ignored by the distributor.
func ChannelSatisfiesConstraints(channel *model.Channel, constraints *dto.ChannelConstraints) bool {
	if channel == nil {
		return false
	}
	if constraints == nil || len(constraints.Filters) == 0 {
		return true
	}
	for _, filter := range constraints.Filters {
		switch filter.Kind {
		case dto.FilterTaskPluginIdentity:
			if !channelMatchesTaskPluginIdentity(channel, filter) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func channelMatchesTaskPluginIdentity(channel *model.Channel, filter dto.ChannelFilter) bool {
	pluginKey := strings.TrimSpace(filter.TaskPluginKey)
	if pluginKey == "" {
		return false
	}
	if channel.Type == constant.ChannelTypeTaskPlugin {
		channelPluginKey := strings.TrimSpace(channel.GetSetting().TaskPluginKey)
		if channelPluginKey == pluginKey {
			return true
		}
		for _, candidateKey := range filter.TaskPluginKeys {
			if channelPluginKey == strings.TrimSpace(candidateKey) {
				return true
			}
		}
		return false
	}
	for _, channelType := range filter.TaskPluginChannelTypes {
		if channel.Type == channelType {
			return true
		}
	}
	return false
}

// TaskPluginChannelFilter returns the request-scoped channel predicate used by
// both memory-cache and database channel selection.
func TaskPluginChannelFilter(c *gin.Context) func(*model.Channel) bool {
	constraints := GetChannelConstraints(c)
	if constraints == nil || len(constraints.Filters) == 0 {
		return nil
	}
	return func(channel *model.Channel) bool {
		return ChannelSatisfiesConstraints(channel, constraints)
	}
}

func pinnedTaskPluginChannelTypes(c *gin.Context, expected string) []int {
	if c == nil || expected == "" {
		return nil
	}
	if value, exists := c.Get(jsplugin.ContextKeyPinnedEndpoint); exists {
		pinned, ok := value.(jsplugin.PinnedEndpoint)
		if ok && pinned.Generation != nil && len(pinned.Candidates) > 1 {
			expectedFound := false
			channelTypes := make([]int, 0, len(pinned.Candidates))
			seen := make(map[int]struct{}, len(pinned.Candidates))
			for _, candidate := range pinned.Candidates {
				if candidate.Plugin == nil {
					continue
				}
				if candidate.Plugin.Meta.Key == expected {
					expectedFound = true
				}
				for _, channelType := range candidate.Plugin.Meta.ChannelTypes {
					if channelType == 0 || channelType == constant.ChannelTypeTaskPlugin {
						continue
					}
					if _, duplicate := seen[channelType]; duplicate {
						continue
					}
					if plugin, indexed := pinned.Generation.GetByChannelType(channelType); indexed && plugin == candidate.Plugin {
						seen[channelType] = struct{}{}
						channelTypes = append(channelTypes, channelType)
					}
				}
			}
			if expectedFound {
				return channelTypes
			}
		}
	}
	value, exists := c.Get(jsplugin.ContextKeyPinnedPlugin)
	pinned, ok := value.(jsplugin.PinnedPlugin)
	if !exists || !ok || pinned.Generation == nil || pinned.Plugin == nil || pinned.Plugin.Meta.Key != expected {
		return nil
	}
	channelTypes := make([]int, 0, len(pinned.Plugin.Meta.ChannelTypes))
	for _, channelType := range pinned.Plugin.Meta.ChannelTypes {
		if channelType == 0 || channelType == constant.ChannelTypeTaskPlugin {
			continue
		}
		channelTypes = append(channelTypes, channelType)
	}
	if len(channelTypes) == 0 {
		return nil
	}
	return channelTypes
}

func pinnedTaskPluginKeys(c *gin.Context, expected string) []string {
	if c == nil || expected == "" {
		return nil
	}
	value, exists := c.Get(jsplugin.ContextKeyPinnedEndpoint)
	pinned, ok := value.(jsplugin.PinnedEndpoint)
	if !exists || !ok || pinned.Generation == nil || len(pinned.Candidates) < 2 {
		return nil
	}
	keys := make([]string, 0, len(pinned.Candidates))
	seen := make(map[string]struct{}, len(pinned.Candidates))
	expectedFound := false
	for _, candidate := range pinned.Candidates {
		if candidate.Plugin == nil {
			continue
		}
		key := strings.TrimSpace(candidate.Plugin.Meta.Key)
		if key == "" {
			continue
		}
		if key == expected {
			expectedFound = true
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	if !expectedFound || len(keys) == 0 {
		return nil
	}
	return keys
}

type RetryParam struct {
	Ctx               *gin.Context
	TokenGroup        string
	ModelName         string
	RequestPath       string
	Retry             *int
	ExcludeChannelIds []int
	ChannelFilter     func(*model.Channel) bool
	resetNextTry      bool
}

func (p *RetryParam) GetRetry() int {
	if p.Retry == nil {
		return 0
	}
	return *p.Retry
}

func (p *RetryParam) SetRetry(retry int) {
	p.Retry = &retry
}

func (p *RetryParam) IncreaseRetry() {
	if p.resetNextTry {
		p.resetNextTry = false
		return
	}
	if p.Retry == nil {
		p.Retry = new(int)
	}
	*p.Retry++
}

func (p *RetryParam) ResetRetryNextTry() {
	p.resetNextTry = true
}

// CacheGetRandomSatisfiedChannel tries to get a random channel that satisfies the requirements.
// 尝试获取一个满足要求的随机渠道。
//
// For "auto" tokenGroup with cross-group Retry enabled:
// 对于启用了跨分组重试的 "auto" tokenGroup：
//
//   - Each group will exhaust all its priorities before moving to the next group.
//     每个分组会用完所有优先级后才会切换到下一个分组。
//
//   - Uses ContextKeyAutoGroupIndex to track current group index.
//     使用 ContextKeyAutoGroupIndex 跟踪当前分组索引。
//
//   - Uses ContextKeyAutoGroupRetryIndex to track the global Retry count when current group started.
//     使用 ContextKeyAutoGroupRetryIndex 跟踪当前分组开始时的全局重试次数。
//
//   - priorityRetry = Retry - startRetryIndex, represents the priority level within current group.
//     priorityRetry = Retry - startRetryIndex，表示当前分组内的优先级级别。
//
//   - When GetRandomSatisfiedChannel returns nil (priorities exhausted), moves to next group.
//     当 GetRandomSatisfiedChannel 返回 nil（优先级用完）时，切换到下一个分组。
//
// Example flow (2 groups, each with 2 priorities, RetryTimes=3):
// 示例流程（2个分组，每个有2个优先级，RetryTimes=3）：
//
//	Retry=0: GroupA, priority0 (startRetryIndex=0, priorityRetry=0)
//	         分组A, 优先级0
//
//	Retry=1: GroupA, priority1 (startRetryIndex=0, priorityRetry=1)
//	         分组A, 优先级1
//
//	Retry=2: GroupA exhausted → GroupB, priority0 (startRetryIndex=2, priorityRetry=0)
//	         分组A用完 → 分组B, 优先级0
//
//	Retry=3: GroupB, priority1 (startRetryIndex=2, priorityRetry=1)
//	         分组B, 优先级1
func CacheGetRandomSatisfiedChannel(param *RetryParam) (*model.Channel, string, error) {
	var channel *model.Channel
	var err error
	selectGroup := param.TokenGroup
	userGroup := common.GetContextKeyString(param.Ctx, constant.ContextKeyUserGroup)
	requestPath := strings.TrimSpace(param.RequestPath)
	if requestPath == "" && param.Ctx != nil && param.Ctx.Request != nil && param.Ctx.Request.URL != nil {
		requestPath = param.Ctx.Request.URL.Path
	}

	if param.TokenGroup == "auto" {
		var autoGroups []string
		if _, exists := common.GetContextKey(param.Ctx, constant.ContextKeyTokenAutoGroups); exists {
			autoGroups = GetRequestAutoGroups(param.Ctx, userGroup)
		} else {
			if len(setting.GetAutoGroups()) == 0 {
				return nil, selectGroup, errors.New("auto groups is not enabled")
			}
			userId := common.GetContextKeyInt(param.Ctx, constant.ContextKeyUserId)
			autoGroups = GetUserAutoGroupByUser(userId, userGroup)
		}
		if len(autoGroups) == 0 {
			return nil, selectGroup, errors.New("auto groups is not enabled")
		}

		// startGroupIndex: the group index to start searching from
		// startGroupIndex: 开始搜索的分组索引
		startGroupIndex := 0
		if lastGroupIndex, exists := common.GetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex); exists {
			if idx, ok := lastGroupIndex.(int); ok {
				startGroupIndex = idx
			}
		}

		for i := startGroupIndex; i < len(autoGroups); i++ {
			autoGroup := autoGroups[i]
			// Calculate priorityRetry for current group
			// 计算当前分组的 priorityRetry
			priorityRetry := param.GetRetry()
			// If moved to a new group, reset priorityRetry and update startRetryIndex
			// 如果切换到新分组，重置 priorityRetry 并更新 startRetryIndex
			if i > startGroupIndex {
				priorityRetry = 0
			}
			logger.LogDebug(param.Ctx, "Auto selecting group: %s, priorityRetry: %d", autoGroup, priorityRetry)

			channel, err = model.GetRandomSatisfiedChannelWithExcludeAndFilter(autoGroup, param.ModelName, priorityRetry, param.ExcludeChannelIds, requestPath, param.ChannelFilter)
			if err != nil {
				return nil, autoGroup, err
			}
			if channel == nil {
				// Current group has no available channel for this model, try next group
				// 当前分组没有该模型的可用渠道，尝试下一个分组
				logger.LogDebug(param.Ctx, "No available channel in group %s for model %s at priorityRetry %d, trying next group", autoGroup, param.ModelName, priorityRetry)
				// 重置状态以尝试下一个分组
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i+1)
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupRetryIndex, 0)
				// Reset retry counter so outer loop can continue for next group
				// 重置重试计数器，以便外层循环可以为下一个分组继续
				param.SetRetry(0)
				continue
			}
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroup, autoGroup)
			selectGroup = autoGroup
			logger.LogDebug(param.Ctx, "Auto selected group: %s", autoGroup)
			// Stay in current group; once the current group's remaining candidates are
			// exhausted, the next retry will naturally advance to the next group.
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i)
			break
		}
	} else {
		channel, err = model.GetRandomSatisfiedChannelWithExcludeAndFilter(param.TokenGroup, param.ModelName, param.GetRetry(), param.ExcludeChannelIds, requestPath, param.ChannelFilter)
		if err != nil {
			return nil, param.TokenGroup, err
		}
	}
	return channel, selectGroup, nil
}
