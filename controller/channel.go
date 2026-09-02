package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/ollama"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type OpenAIModel struct {
	ID         string         `json:"id"`
	Object     string         `json:"object"`
	Created    int64          `json:"created"`
	OwnedBy    string         `json:"owned_by"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	Permission []struct {
		ID                 string `json:"id"`
		Object             string `json:"object"`
		Created            int64  `json:"created"`
		AllowCreateEngine  bool   `json:"allow_create_engine"`
		AllowSampling      bool   `json:"allow_sampling"`
		AllowLogprobs      bool   `json:"allow_logprobs"`
		AllowSearchIndices bool   `json:"allow_search_indices"`
		AllowView          bool   `json:"allow_view"`
		AllowFineTuning    bool   `json:"allow_fine_tuning"`
		Organization       string `json:"organization"`
		Group              string `json:"group"`
		IsBlocking         bool   `json:"is_blocking"`
	} `json:"permission"`
	Root   string `json:"root"`
	Parent string `json:"parent"`
}

type OpenAIModelsResponse struct {
	Data    []OpenAIModel `json:"data"`
	Success bool          `json:"success"`
}

func parseStatusFilter(statusParam string) int {
	switch strings.ToLower(statusParam) {
	case "enabled", "1":
		return common.ChannelStatusEnabled
	case "disabled", "0":
		return 0
	default:
		return -1
	}
}

func clearChannelInfo(channel *model.Channel) {
	if channel.ChannelInfo.IsMultiKey {
		channel.ChannelInfo.MultiKeyDisabledReason = nil
		channel.ChannelInfo.MultiKeyDisabledTime = nil
	}
}

func applyChannelStatusFilter(query *gorm.DB, statusFilter int) *gorm.DB {
	if statusFilter == common.ChannelStatusEnabled {
		return query.Where("status = ?", common.ChannelStatusEnabled)
	}
	if statusFilter == 0 {
		return query.Where("status != ?", common.ChannelStatusEnabled)
	}
	return query
}

func buildChannelListQuery(group string, statusFilter int, typeFilter int) *gorm.DB {
	query := model.DB.Model(&model.Channel{})
	query = model.ApplyChannelGroupFilter(query, group)
	query = applyChannelStatusFilter(query, statusFilter)
	if typeFilter >= 0 {
		query = query.Where("type = ?", typeFilter)
	}
	return query
}

func GetChannelOps(c *gin.Context) {
	common.ApiSuccess(c, gin.H{
		"retry_times": common.RetryTimes,
	})
}

func GetAllChannels(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	channelData := make([]*model.Channel, 0)
	idSort, _ := strconv.ParseBool(c.Query("id_sort"))
	sortOptions := model.NewChannelSortOptions(c.Query("sort_by"), c.Query("sort_order"), idSort)
	enableTagMode, _ := strconv.ParseBool(c.Query("tag_mode"))
	groupFilter := model.NormalizeChannelGroupFilter(c.Query("group"))
	statusParam := c.Query("status")
	// statusFilter: -1 all, 1 enabled, 0 disabled (include auto & manual)
	statusFilter := parseStatusFilter(statusParam)
	// type filter
	typeStr := c.Query("type")
	typeFilter := -1
	if typeStr != "" {
		if t, err := strconv.Atoi(typeStr); err == nil {
			typeFilter = t
		}
	}

	var total int64

	if enableTagMode {
		tags, err := model.GetPaginatedChannelTags(buildChannelListQuery(groupFilter, statusFilter, typeFilter), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
		if err != nil {
			common.SysError("failed to get paginated tags: " + err.Error())
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "获取标签失败，请稍后重试"})
			return
		}
		total, err = model.CountChannelTags(buildChannelListQuery(groupFilter, statusFilter, typeFilter))
		if err != nil {
			common.SysError("failed to count tags: " + err.Error())
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "获取标签数量失败，请稍后重试"})
			return
		}
		for _, tag := range tags {
			if tag == nil || *tag == "" {
				continue
			}
			var tagChannels []*model.Channel
			err := sortOptions.Apply(buildChannelListQuery(groupFilter, statusFilter, typeFilter).Where("tag = ?", *tag)).
				Omit("key").
				Find(&tagChannels).Error
			if err != nil {
				common.SysError("failed to get channels by tag: " + err.Error())
				c.JSON(http.StatusOK, gin.H{"success": false, "message": "获取标签渠道失败，请稍后重试"})
				return
			}
			channelData = append(channelData, tagChannels...)
		}
	} else {
		if err := buildChannelListQuery(groupFilter, statusFilter, typeFilter).Count(&total).Error; err != nil {
			common.SysError("failed to count channels: " + err.Error())
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "获取渠道数量失败，请稍后重试"})
			return
		}

		err := sortOptions.Apply(buildChannelListQuery(groupFilter, statusFilter, typeFilter)).
			Limit(pageInfo.GetPageSize()).
			Offset(pageInfo.GetStartIdx()).
			Omit("key").
			Find(&channelData).Error
		if err != nil {
			common.SysError("failed to get channels: " + err.Error())
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "获取渠道列表失败，请稍后重试"})
			return
		}
	}

	for _, datum := range channelData {
		clearChannelInfo(datum)
	}

	countQuery := buildChannelListQuery(groupFilter, statusFilter, -1)
	var results []struct {
		Type  int64
		Count int64
	}
	if err := countQuery.Select("type, count(*) as count").Group("type").Find(&results).Error; err != nil {
		common.SysError("failed to count channel types: " + err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "获取渠道类型统计失败，请稍后重试"})
		return
	}
	typeCounts := make(map[int64]int64)
	for _, r := range results {
		typeCounts[r.Type] = r.Count
	}
	common.ApiSuccess(c, gin.H{
		"items":       channelData,
		"total":       total,
		"page":        pageInfo.GetPage(),
		"page_size":   pageInfo.GetPageSize(),
		"type_counts": typeCounts,
	})
	return
}

func buildFetchModelsHeaders(channel *model.Channel, key string) (http.Header, error) {
	var headers http.Header
	switch channel.Type {
	case constant.ChannelTypeAnthropic:
		headers = GetClaudeAuthHeader(key)
	default:
		headers = GetAuthHeader(key)
	}

	if err := applyFetchModelsHeaderOverrides(channel, key, headers); err != nil {
		return nil, err
	}

	return headers, nil
}

func buildFetchModelsHeaderOverrides(channel *model.Channel, key string) (http.Header, error) {
	headers := make(http.Header)
	if err := applyFetchModelsHeaderOverrides(channel, key, headers); err != nil {
		return nil, err
	}
	return headers, nil
}

func applyFetchModelsHeaderOverrides(channel *model.Channel, key string, headers http.Header) error {
	if channel != nil && channel.Type == constant.ChannelTypeCodex {
		var oauthKey service.CodexOAuthKey
		if err := common.Unmarshal([]byte(strings.TrimSpace(key)), &oauthKey); err == nil {
			key = strings.TrimSpace(oauthKey.AccessToken)
		}
	}
	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:          key,
			HeadersOverride: channel.GetHeaderOverride(),
		},
	}
	overrides, err := relaychannel.ResolveHeaderOverride(info, nil)
	if err != nil {
		return err
	}
	for name, value := range overrides {
		headers.Set(name, value)
	}

	return nil
}

func FetchUpstreamModels(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	channel, err := model.GetChannelById(id, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	originalCodexKey := ""
	if channel.Type == constant.ChannelTypeCodex {
		originalCodexKey = channel.Key
	}
	ids, err := fetchChannelUpstreamModelIDs(c.Request.Context(), channel)
	err = refreshRuntimeCacheAfterCodexCredentialChange(channel, originalCodexKey, err)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("获取模型列表失败: %s", err.Error()),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    ids,
	})
}

func FixChannelsAbilities(c *gin.Context) {
	success, fails, err := model.FixAbility()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"success": success,
			"fails":   fails,
		},
	})
}

func SearchChannels(c *gin.Context) {
	keyword := c.Query("keyword")
	group := c.Query("group")
	modelKeyword := c.Query("model")
	statusParam := c.Query("status")
	statusFilter := parseStatusFilter(statusParam)
	idSort, _ := strconv.ParseBool(c.Query("id_sort"))
	sortOptions := model.NewChannelSortOptions(c.Query("sort_by"), c.Query("sort_order"), idSort)
	enableTagMode, _ := strconv.ParseBool(c.Query("tag_mode"))
	channelData := make([]*model.Channel, 0)
	if enableTagMode {
		tags, err := model.SearchTags(keyword, group, modelKeyword, idSort)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
		for _, tag := range tags {
			if tag != nil && *tag != "" {
				var tagChannels []*model.Channel
				err := sortOptions.Apply(buildChannelListQuery(group, -1, -1).Where("tag = ?", *tag)).
					Omit("key").
					Find(&tagChannels).Error
				if err != nil {
					c.JSON(http.StatusOK, gin.H{
						"success": false,
						"message": err.Error(),
					})
					return
				}
				channelData = append(channelData, tagChannels...)
			}
		}
	} else {
		channels, err := model.SearchChannels(keyword, group, modelKeyword, idSort, sortOptions)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
		channelData = channels
	}

	if statusFilter == common.ChannelStatusEnabled || statusFilter == 0 {
		filtered := make([]*model.Channel, 0, len(channelData))
		for _, ch := range channelData {
			if statusFilter == common.ChannelStatusEnabled && ch.Status != common.ChannelStatusEnabled {
				continue
			}
			if statusFilter == 0 && ch.Status == common.ChannelStatusEnabled {
				continue
			}
			filtered = append(filtered, ch)
		}
		channelData = filtered
	}

	// calculate type counts for search results
	typeCounts := make(map[int64]int64)
	for _, channel := range channelData {
		typeCounts[int64(channel.Type)]++
	}

	typeParam := c.Query("type")
	typeFilter := -1
	if typeParam != "" {
		if tp, err := strconv.Atoi(typeParam); err == nil {
			typeFilter = tp
		}
	}

	if typeFilter >= 0 {
		filtered := make([]*model.Channel, 0, len(channelData))
		for _, ch := range channelData {
			if ch.Type == typeFilter {
				filtered = append(filtered, ch)
			}
		}
		channelData = filtered
	}

	page, _ := strconv.Atoi(c.DefaultQuery("p", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	total := len(channelData)
	startIdx := (page - 1) * pageSize
	if startIdx > total {
		startIdx = total
	}
	endIdx := startIdx + pageSize
	if endIdx > total {
		endIdx = total
	}

	pagedData := channelData[startIdx:endIdx]

	for _, datum := range pagedData {
		clearChannelInfo(datum)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"items":       pagedData,
			"total":       total,
			"type_counts": typeCounts,
		},
	})
	return
}

func GetChannel(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	channel, err := model.GetChannelById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if channel != nil {
		clearChannelInfo(channel)
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    channel,
	})
	return
}

// GetChannelKey 获取渠道密钥（需要通过安全验证中间件）
// 此函数依赖 SecureVerificationRequired 中间件，确保用户已通过安全验证
func GetChannelKey(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, fmt.Errorf("渠道ID格式错误: %v", err))
		return
	}

	// 获取渠道信息（包含密钥）
	channel, err := model.GetChannelById(channelId, true)
	if err != nil {
		common.ApiError(c, fmt.Errorf("获取渠道信息失败: %v", err))
		return
	}

	if channel == nil {
		common.ApiError(c, fmt.Errorf("渠道不存在"))
		return
	}

	// 记录操作审计日志（高危：查看渠道密钥）
	recordManageAudit(c, "channel.key_view", map[string]interface{}{
		"id":   channelId,
		"name": channel.Name,
	})

	// 返回渠道密钥
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "获取成功",
		"data": map[string]interface{}{
			"key": channel.Key,
		},
	})
}

// validateTwoFactorAuth 统一的2FA验证函数
func validateTwoFactorAuth(twoFA *model.TwoFA, code string) bool {
	// 尝试验证TOTP
	if cleanCode, err := common.ValidateNumericCode(code); err == nil {
		if isValid, _ := twoFA.ValidateTOTPAndUpdateUsage(cleanCode); isValid {
			return true
		}
	}

	// 尝试验证备用码
	if isValid, err := twoFA.ValidateBackupCodeAndUpdateUsage(code); err == nil && isValid {
		return true
	}

	return false
}

// validateChannel 通用的渠道校验函数
func validateChannel(channel *model.Channel, isAdd bool) error {
	// 校验 channel settings
	if channel == nil {
		return fmt.Errorf("渠道不能为空")
	}
	if err := channel.ValidateSettings(); err != nil {
		return fmt.Errorf("渠道额外设置[channel setting] 格式错误：%s", err.Error())
	}

	// 如果是添加操作，检查 channel 和 key 是否为空
	if isAdd {
		if channel.Key == "" {
			return fmt.Errorf("渠道不能为空")
		}

		// 检查模型名称长度是否超过 255
		for _, m := range channel.GetModels() {
			if len(m) > 255 {
				return fmt.Errorf("模型名称过长: %s", m)
			}
		}
	}

	// 自定义上游网关必须提供基础地址。
	if channel.Type == constant.ChannelTypeSub2API || channel.Type == constant.ChannelTypeNewAPI {
		if channel.BaseURL == nil || strings.TrimSpace(*channel.BaseURL) == "" {
			if channel.Type == constant.ChannelTypeSub2API {
				return fmt.Errorf("Sub2API 渠道基础地址不能为空")
			}
			return fmt.Errorf("New API 渠道基础地址不能为空")
		}
	}

	if channel.Type == constant.ChannelTypeVertexAi {
		if channel.Other == "" {
			return fmt.Errorf("部署地区不能为空")
		}

		regionMap, err := common.StrToMap(channel.Other)
		if err != nil {
			return fmt.Errorf("部署地区必须是标准的Json格式，例如{\"default\": \"us-central1\", \"region2\": \"us-east1\"}")
		}

		if regionMap["default"] == nil {
			return fmt.Errorf("部署地区必须包含default字段")
		}
	}

	// Codex OAuth key validation (optional, only when JSON object is provided)
	if channel.Type == constant.ChannelTypeCodex {
			trimmedKey := strings.TrimSpace(channel.Key)
			if isAdd || trimmedKey != "" {
				if !strings.HasPrefix(trimmedKey, "{") {
					return fmt.Errorf("Codex key 必须是有效的 JSON 对象")
				}
				var keyMap map[string]any
				if err := common.Unmarshal([]byte(trimmedKey), &keyMap); err != nil {
					return fmt.Errorf("Codex key 必须是有效的 JSON 对象")
				}
				if v, ok := keyMap["access_token"]; !ok || v == nil || strings.TrimSpace(fmt.Sprintf("%v", v)) == "" {
					return fmt.Errorf("Codex key JSON 必须包含 access_token")
				}
				if v, ok := keyMap["account_id"]; !ok || v == nil || strings.TrimSpace(fmt.Sprintf("%v", v)) == "" {
					return fmt.Errorf("Codex key JSON 必须包含 account_id")
				}
			}
		}

	return nil
}

func RefreshCodexChannelCredential(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, fmt.Errorf("渠道 ID 无效: %w", err))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	oauthKey, ch, err := service.RefreshCodexChannelCredential(ctx, channelId, service.CodexCredentialRefreshOptions{ResetCaches: true})
	if err != nil && errors.Is(err, service.ErrCodexCredentialCacheRefresh) && oauthKey != nil && ch != nil {
		// The credential write is durable even when the first runtime-cache
		// refresh failed. Retry the cache refresh before reporting failure so
		// the manual path does not leave an old token in memory unnecessarily.
		if cacheErr := refreshChannelRuntimeCache(); cacheErr == nil {
			err = nil
		} else {
			err = fmt.Errorf("%w; compensating runtime cache refresh failed: %v", err, cacheErr)
		}
	}
	if err != nil {
		common.SysError("failed to refresh codex channel credential: " + err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "刷新凭证失败，请稍后重试"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "已刷新",
		"data": gin.H{
			"expires_at":   oauthKey.Expired,
			"last_refresh": oauthKey.LastRefresh,
			"account_id":   oauthKey.AccountID,
			"email":        oauthKey.Email,
			"channel_id":   ch.Id,
			"channel_type": ch.Type,
			"channel_name": ch.Name,
		},
	})
}

type AddChannelRequest struct {
	Mode                      string                `json:"mode"`
	MultiKeyMode              constant.MultiKeyMode `json:"multi_key_mode"`
	BatchAddSetKeyPrefix2Name bool                  `json:"batch_add_set_key_prefix_2_name"`
	Channel                   *model.Channel        `json:"channel"`
}

func getVertexArrayKeys(keys string) ([]string, error) {
	if keys == "" {
		return nil, nil
	}
	var keyArray []interface{}
	err := common.Unmarshal([]byte(keys), &keyArray)
	if err != nil {
		return nil, fmt.Errorf("批量添加 Vertex AI 必须使用标准的JsonArray格式，例如[{key1}, {key2}...]，请检查输入: %w", err)
	}
	cleanKeys := make([]string, 0, len(keyArray))
	for _, key := range keyArray {
		var keyStr string
		switch v := key.(type) {
		case string:
			keyStr = strings.TrimSpace(v)
		default:
			bytes, err := common.Marshal(v)
			if err != nil {
				return nil, fmt.Errorf("Vertex AI key JSON 编码失败: %w", err)
			}
			keyStr = string(bytes)
		}
		if keyStr != "" {
			cleanKeys = append(cleanKeys, keyStr)
		}
	}
	if len(cleanKeys) == 0 {
		return nil, fmt.Errorf("批量添加 Vertex AI 的 keys 不能为空")
	}
	return cleanKeys, nil
}

func isValidJSONText(text string) bool {
	var payload interface{}
	return common.Unmarshal([]byte(text), &payload) == nil
}

func normalizeChannelUpstreamModelUpdateSettingsForCreate(channel *model.Channel) error {
	if channel == nil {
		return nil
	}
	settingsMap, ok := channelSettingsMapForUpdate(channel.OtherSettings)
	if !ok {
		return fmt.Errorf("渠道额外设置不是有效的 JSON")
	}
	changed := false
	if !channelSupportsUpstreamModelUpdate(channel) {
		changed = removeChannelUpstreamModelUpdateFields(settingsMap)
	} else {
		for _, key := range []string{
			"upstream_model_update_last_check_time",
			"upstream_model_update_last_detected_models",
			"upstream_model_update_last_removed_models",
		} {
			if _, ok := settingsMap[key]; ok {
				delete(settingsMap, key)
				changed = true
			}
		}
		if checkEnabled, _ := settingsMap["upstream_model_update_check_enabled"].(bool); !checkEnabled {
			if _, ok := settingsMap["upstream_model_update_auto_sync_enabled"]; ok {
				delete(settingsMap, "upstream_model_update_auto_sync_enabled")
				changed = true
			}
		}
	}
	if !changed {
		return nil
	}
	settingsBytes, err := common.Marshal(settingsMap)
	if err != nil {
		return err
	}
	channel.OtherSettings = string(settingsBytes)
	return nil
}

func AddChannel(c *gin.Context) {
	addChannelRequest := AddChannelRequest{}
	err := c.ShouldBindJSON(&addChannelRequest)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	if addChannelRequest.Channel == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "渠道不能为空",
		})
		return
	}
	addChannelRequest.Channel.CreatedTime = common.GetTimestamp()
	keys := make([]string, 0)
	switch addChannelRequest.Mode {
	case "multi_to_single":
		addChannelRequest.Channel.ChannelInfo.IsMultiKey = true
		addChannelRequest.Channel.ChannelInfo.MultiKeyMode = addChannelRequest.MultiKeyMode
		if addChannelRequest.Channel.Type == constant.ChannelTypeVertexAi && addChannelRequest.Channel.GetOtherSettings().VertexKeyType != dto.VertexKeyTypeAPIKey {
			array, err := getVertexArrayKeys(addChannelRequest.Channel.Key)
			if err != nil {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": err.Error(),
				})
				return
			}
			addChannelRequest.Channel.ChannelInfo.MultiKeySize = len(array)
			addChannelRequest.Channel.Key = strings.Join(array, "\n")
		} else {
			cleanKeys := make([]string, 0)
			for _, key := range strings.Split(addChannelRequest.Channel.Key, "\n") {
				if key == "" {
					continue
				}
				key = strings.TrimSpace(key)
				cleanKeys = append(cleanKeys, key)
			}
			addChannelRequest.Channel.ChannelInfo.MultiKeySize = len(cleanKeys)
			addChannelRequest.Channel.Key = strings.Join(cleanKeys, "\n")
		}
		keys = []string{addChannelRequest.Channel.Key}
	case "batch":
		if addChannelRequest.Channel.Type == constant.ChannelTypeVertexAi && addChannelRequest.Channel.GetOtherSettings().VertexKeyType != dto.VertexKeyTypeAPIKey {
			// multi json
			keys, err = getVertexArrayKeys(addChannelRequest.Channel.Key)
			if err != nil {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": err.Error(),
				})
				return
			}
		} else {
			keys = strings.Split(addChannelRequest.Channel.Key, "\n")
		}
	case "single":
		keys = []string{addChannelRequest.Channel.Key}
	default:
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "不支持的添加模式",
		})
		return
	}
	if err := normalizeChannelUpstreamModelUpdateSettingsForCreate(addChannelRequest.Channel); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	// 使用统一的校验函数。multi_to_single 会在上面先标记 ChannelInfo，
	// 因此不支持上游模型更新的聚合密钥渠道会先清理残留设置再校验。
	if err := validateChannel(addChannelRequest.Channel, true); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	channels := make([]model.Channel, 0, len(keys))
	for _, key := range keys {
		if key == "" {
			continue
		}
		localChannel := addChannelRequest.Channel
		localChannel.Key = key
		if addChannelRequest.BatchAddSetKeyPrefix2Name && len(keys) > 1 {
			keyPrefix := localChannel.Key
			if len(localChannel.Key) > 8 {
				keyPrefix = localChannel.Key[:8]
			}
			localChannel.Name = fmt.Sprintf("%s %s", localChannel.Name, keyPrefix)
		}
		channels = append(channels, *localChannel)
	}
	err = model.BatchInsertChannels(channels)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	service.ResetProxyClientCache()
	recordManageAudit(c, "channel.create", map[string]interface{}{
		"name":  addChannelRequest.Channel.Name,
		"type":  addChannelRequest.Channel.Type,
		"count": len(channels),
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func DeleteChannel(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	channelName := ""
	if existing, err := model.GetChannelById(id, false); err == nil && existing != nil {
		channelName = existing.Name
	}
	channel := model.Channel{Id: id}
	err := channel.Delete()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	service.ResetProxyClientCache()
	recordManageAudit(c, "channel.delete", map[string]interface{}{
		"id":   id,
		"name": channelName,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func DeleteDisabledChannel(c *gin.Context) {
	rows, err := model.DeleteDisabledChannel()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	recordManageAudit(c, "channel.delete_disabled", map[string]interface{}{
		"count": rows,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    rows,
	})
	return
}

type ChannelTag struct {
	Tag            string  `json:"tag"`
	NewTag         *string `json:"new_tag"`
	Priority       *int64  `json:"priority"`
	Weight         *uint   `json:"weight"`
	ModelMapping   *string `json:"model_mapping"`
	Models         *string `json:"models"`
	Groups         *string `json:"groups"`
	ParamOverride  *string `json:"param_override"`
	HeaderOverride *string `json:"header_override"`
}

func DisableTagChannels(c *gin.Context) {
	channelTag := ChannelTag{}
	err := c.ShouldBindJSON(&channelTag)
	if err != nil || channelTag.Tag == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "参数错误",
		})
		return
	}
	err = model.DisableChannelByTag(channelTag.Tag)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	recordManageAudit(c, "channel.tag_disable", map[string]interface{}{
		"tag": channelTag.Tag,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func EnableTagChannels(c *gin.Context) {
	channelTag := ChannelTag{}
	err := c.ShouldBindJSON(&channelTag)
	if err != nil || channelTag.Tag == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "参数错误",
		})
		return
	}
	err = model.EnableChannelByTag(channelTag.Tag)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	recordManageAudit(c, "channel.tag_enable", map[string]interface{}{
		"tag": channelTag.Tag,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func EditTagChannels(c *gin.Context) {
	channelTag := ChannelTag{}
	err := c.ShouldBindJSON(&channelTag)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "参数错误",
		})
		return
	}
	if channelTag.Tag == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "标签不能为空",
		})
		return
	}
	if (channelTag.ParamOverride != nil || channelTag.HeaderOverride != nil) &&
		!authz.Can(c.GetInt("id"), c.GetInt("role"), authz.ChannelSensitiveWrite) {
		common.ApiErrorI18n(c, i18n.MsgAuthInsufficientPrivilege)
		return
	}
	if channelTag.ParamOverride != nil {
		trimmed := strings.TrimSpace(*channelTag.ParamOverride)
		if trimmed != "" && !isValidJSONText(trimmed) {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "参数覆盖必须是合法的 JSON 格式",
			})
			return
		}
		channelTag.ParamOverride = common.GetPointer[string](trimmed)
	}
	if channelTag.HeaderOverride != nil {
		trimmed := strings.TrimSpace(*channelTag.HeaderOverride)
		if trimmed != "" && !isValidJSONText(trimmed) {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "请求头覆盖必须是合法的 JSON 格式",
			})
			return
		}
		channelTag.HeaderOverride = common.GetPointer[string](trimmed)
	}
	err = model.EditChannelByTag(channelTag.Tag, channelTag.NewTag, channelTag.ModelMapping, channelTag.Models, channelTag.Groups, channelTag.Priority, channelTag.Weight, channelTag.ParamOverride, channelTag.HeaderOverride)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	recordManageAudit(c, "channel.tag_edit", map[string]interface{}{
		"tag": channelTag.Tag,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

type ChannelBatch struct {
	Ids []int   `json:"ids"`
	Tag *string `json:"tag"`
}

func DeleteChannelBatch(c *gin.Context) {
	channelBatch := ChannelBatch{}
	err := c.ShouldBindJSON(&channelBatch)
	if err != nil || len(channelBatch.Ids) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "参数错误",
		})
		return
	}
	deletedCount, err := model.BatchDeleteChannels(channelBatch.Ids)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	service.ResetProxyClientCache()
	recordManageAudit(c, "channel.delete_batch", map[string]interface{}{
		"count": deletedCount,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    deletedCount,
	})
	return
}

type PatchChannel struct {
	model.Channel
	MultiKeyMode *string `json:"multi_key_mode"`
	KeyMode      *string `json:"key_mode"` // 多key模式下密钥覆盖或者追加
}

var errChannelSensitivePermissionDenied = errors.New("渠道敏感写入权限不足")

type channelUpdateValidationError struct {
	message string
}

func (e channelUpdateValidationError) Error() string {
	return e.message
}

func pointerUpdateValue[T any](value *T) any {
	if value == nil {
		return nil
	}
	return *value
}

func channelUpdateColumns(channel *PatchChannel, requestData map[string]any) map[string]any {
	updates := make(map[string]any)
	for field := range requestData {
		if _, ok := channelReadOnlyFields[field]; ok {
			continue
		}
		if _, ok := channelOperationalFields[field]; ok {
			continue
		}
		switch field {
		case "type":
			updates["type"] = channel.Type
		case "key":
			updates["key"] = channel.Key
		case "openai_organization":
			updates["openai_organization"] = pointerUpdateValue(channel.OpenAIOrganization)
		case "test_model":
			updates["test_model"] = pointerUpdateValue(channel.TestModel)
		case "name":
			updates["name"] = channel.Name
		case "weight":
			updates["weight"] = pointerUpdateValue(channel.Weight)
		case "base_url":
			updates["base_url"] = pointerUpdateValue(channel.BaseURL)
		case "other":
			updates["other"] = channel.Other
		case "models":
			updates["models"] = channel.Models
		case "group":
			updates["group"] = channel.Group
		case "model_mapping":
			updates["model_mapping"] = pointerUpdateValue(channel.ModelMapping)
		case "status_code_mapping":
			updates["status_code_mapping"] = pointerUpdateValue(channel.StatusCodeMapping)
		case "priority":
			updates["priority"] = pointerUpdateValue(channel.Priority)
		case "auto_ban":
			updates["auto_ban"] = pointerUpdateValue(channel.AutoBan)
		case "other_info":
			updates["other_info"] = channel.OtherInfo
		case "tag":
			updates["tag"] = pointerUpdateValue(channel.Tag)
		case "setting":
			updates["setting"] = pointerUpdateValue(channel.Setting)
		case "param_override":
			updates["param_override"] = pointerUpdateValue(channel.ParamOverride)
		case "header_override":
			updates["header_override"] = pointerUpdateValue(channel.HeaderOverride)
		case "remark":
			updates["remark"] = pointerUpdateValue(channel.Remark)
		case "settings":
			updates["settings"] = channel.OtherSettings
		}
	}
	updates["channel_info"] = channel.ChannelInfo
	return updates
}

func mergeChannelUpstreamModelUpdateRuntimeSettingsForUpdate(requestedSettings string, currentSettings string, sourceChanged bool) string {
	requested, ok := channelSettingsMapForUpdate(requestedSettings)
	if !ok {
		return requestedSettings
	}
	current, _ := channelSettingsMapForUpdate(currentSettings)

	if checkEnabled, _ := requested["upstream_model_update_check_enabled"].(bool); checkEnabled {
		if sourceChanged {
			clearChannelUpstreamModelUpdateRuntimeFields(requested)
		} else {
			preserveChannelSettingRuntimeField(requested, current, "upstream_model_update_last_check_time")
			preserveChannelSettingRuntimeField(requested, current, "upstream_model_update_last_detected_models")
			preserveChannelSettingRuntimeField(requested, current, "upstream_model_update_last_removed_models")
		}
	} else {
		delete(requested, "upstream_model_update_auto_sync_enabled")
		clearChannelUpstreamModelUpdateRuntimeFields(requested)
	}

	settingsBytes, err := common.Marshal(requested)
	if err != nil {
		return requestedSettings
	}
	return string(settingsBytes)
}

var channelUpstreamModelUpdateSettingKeys = []string{
	"upstream_model_update_check_enabled",
	"upstream_model_update_auto_sync_enabled",
	"upstream_model_update_ignored_models",
	"upstream_model_update_last_check_time",
	"upstream_model_update_last_detected_models",
	"upstream_model_update_last_removed_models",
}

func clearChannelUpstreamModelUpdateRuntimeFields(settings map[string]any) {
	settings["upstream_model_update_last_detected_models"] = []string{}
	settings["upstream_model_update_last_removed_models"] = []string{}
	settings["upstream_model_update_last_check_time"] = 0
}

func removeChannelUpstreamModelUpdateFields(settings map[string]any) bool {
	changed := false
	for _, key := range channelUpstreamModelUpdateSettingKeys {
		if _, ok := settings[key]; ok {
			delete(settings, key)
			changed = true
		}
	}
	return changed
}

func channelSettingsContainUpstreamModelUpdateFields(raw string) bool {
	settings, ok := channelSettingsMapForUpdate(raw)
	if !ok {
		return false
	}
	for _, key := range channelUpstreamModelUpdateSettingKeys {
		if _, ok := settings[key]; ok {
			return true
		}
	}
	return false
}

func clearChannelUpstreamModelUpdateRuntimeSettingsForSourceChange(raw string) (string, bool) {
	settings, ok := channelSettingsMapForUpdate(raw)
	if !ok {
		return raw, false
	}
	hasUpstreamModelUpdateFields := false
	for _, key := range channelUpstreamModelUpdateSettingKeys {
		if _, ok := settings[key]; ok {
			hasUpstreamModelUpdateFields = true
			break
		}
	}
	if !hasUpstreamModelUpdateFields {
		return raw, false
	}
	clearChannelUpstreamModelUpdateRuntimeFields(settings)
	settingsBytes, err := common.Marshal(settings)
	if err != nil {
		return raw, false
	}
	return string(settingsBytes), true
}

func channelSettingsWithoutUpstreamModelUpdateFields(raw string) (string, bool, error) {
	settings, ok := channelSettingsMapForUpdate(raw)
	if !ok {
		return "", false, fmt.Errorf("渠道额外设置不是有效的 JSON")
	}
	if !removeChannelUpstreamModelUpdateFields(settings) {
		return raw, false, nil
	}
	settingsBytes, err := common.Marshal(settings)
	if err != nil {
		return "", false, err
	}
	return string(settingsBytes), true, nil
}

func sameChannelSettingsWithoutUpstreamModelUpdateFields(left string, right string) bool {
	leftSettings, leftOK := channelSettingsMapForUpdate(left)
	rightSettings, rightOK := channelSettingsMapForUpdate(right)
	if !leftOK || !rightOK {
		return strings.TrimSpace(left) == strings.TrimSpace(right)
	}
	removeChannelUpstreamModelUpdateFields(leftSettings)
	removeChannelUpstreamModelUpdateFields(rightSettings)
	if len(leftSettings) == 0 {
		leftSettings = nil
	}
	if len(rightSettings) == 0 {
		rightSettings = nil
	}
	return reflect.DeepEqual(leftSettings, rightSettings)
}

func channelSettingsMapForUpdate(raw string) (map[string]any, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]any{}, true
	}
	parsed := map[string]any{}
	if err := common.UnmarshalJsonStr(raw, &parsed); err != nil {
		return nil, false
	}
	if parsed == nil {
		return map[string]any{}, true
	}
	return parsed, true
}

func preparePatchChannelForValidation(channel *PatchChannel, origin *model.Channel, requestData map[string]any) (model.Channel, error) {
	if channel == nil {
		return model.Channel{}, fmt.Errorf("渠道不能为空")
	}
	validationChannel := channel.Channel
	if origin != nil {
		validationChannel.ChannelInfo = origin.ChannelInfo
		if _, ok := requestData["type"]; !ok {
			validationChannel.Type = origin.Type
		}
		if _, ok := requestData["base_url"]; !ok {
			validationChannel.BaseURL = origin.BaseURL
		}
		if _, ok := requestData["other"]; !ok {
			validationChannel.Other = origin.Other
		}
		if _, ok := requestData["setting"]; !ok {
			validationChannel.Setting = origin.Setting
		}
	}
	if channel.MultiKeyMode != nil && strings.TrimSpace(*channel.MultiKeyMode) != "" {
		validationChannel.ChannelInfo.MultiKeyMode = constant.MultiKeyMode(strings.TrimSpace(*channel.MultiKeyMode))
	}
	if !channelSupportsUpstreamModelUpdate(&validationChannel) {
		cleanedSettings, changed, err := channelSettingsWithoutUpstreamModelUpdateFields(validationChannel.OtherSettings)
		if err != nil {
			return validationChannel, err
		}
		if changed {
			validationChannel.OtherSettings = cleanedSettings
			if _, ok := requestData["settings"]; ok {
				channel.OtherSettings = cleanedSettings
			}
		}
	}
	return validationChannel, nil
}

func preserveChannelSettingRuntimeField(requested map[string]any, current map[string]any, key string) {
	if value, ok := current[key]; ok {
		requested[key] = value
		return
	}
	delete(requested, key)
}

func channelUpstreamModelSourceChangedForUpdate(origin *model.Channel, channel *PatchChannel, requestData map[string]any) bool {
	if origin == nil || channel == nil {
		return true
	}
	if _, ok := requestData["type"]; ok && origin.Type != channel.Type {
		return true
	}
	if _, ok := requestData["key"]; ok && origin.Key != channel.Key {
		return true
	}
	if _, ok := requestData["base_url"]; ok && !equalStringPtr(origin.BaseURL, channel.BaseURL) {
		return true
	}
	if _, ok := requestData["other"]; ok && origin.Other != channel.Other {
		return true
	}
	if _, ok := requestData["models"]; ok && !reflect.DeepEqual(normalizeModelNames(origin.GetModels()), normalizeModelNames(channel.GetModels())) {
		return true
	}
	if _, ok := requestData["model_mapping"]; ok && !equalStringPtr(origin.ModelMapping, channel.ModelMapping) {
		return true
	}
	if _, ok := requestData["setting"]; ok && !equalStringPtr(origin.Setting, channel.Setting) {
		return true
	}
	if _, ok := requestData["header_override"]; ok && !equalStringPtr(origin.HeaderOverride, channel.HeaderOverride) {
		return true
	}
	if _, ok := requestData["multi_key_mode"]; ok &&
		channel.MultiKeyMode != nil &&
		strings.TrimSpace(*channel.MultiKeyMode) != "" &&
		origin.ChannelInfo.MultiKeyMode != constant.MultiKeyMode(strings.TrimSpace(*channel.MultiKeyMode)) {
		return true
	}
	if _, ok := requestData["settings"]; ok && channelUpstreamModelUpdateSourceSettingsChanged(origin.OtherSettings, channel.OtherSettings) {
		return true
	}
	return false
}

func channelUpstreamModelUpdateSourceSettingsChanged(originSettings string, requestedSettings string) bool {
	origin, originOK := channelSettingsMapForUpdate(originSettings)
	requested, requestedOK := channelSettingsMapForUpdate(requestedSettings)
	if !originOK || !requestedOK {
		return strings.TrimSpace(originSettings) != strings.TrimSpace(requestedSettings)
	}
	for _, key := range []string{
		"advanced_custom",
		"proxy",
		"upstream_model_update_check_enabled",
		"upstream_model_update_ignored_models",
	} {
		if !reflect.DeepEqual(origin[key], requested[key]) {
			return true
		}
	}
	return false
}

func copyChannelUpdateRequestData(requestData map[string]any) map[string]any {
	copied := make(map[string]any, len(requestData))
	for key, value := range requestData {
		copied[key] = value
	}
	return copied
}

func clonePatchChannelPointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func useLockedChannelFieldForStaleNoop(
	requestData map[string]any,
	field string,
	requested any,
	stale any,
	locked any,
	useLocked func(),
) {
	if _, ok := requestData[field]; !ok {
		useLocked()
		return
	}
	if reflect.DeepEqual(requested, stale) && !reflect.DeepEqual(locked, stale) {
		delete(requestData, field)
		useLocked()
	}
}

func mergeLockedChannelStateForUpdate(
	channel *PatchChannel,
	staleOrigin *model.Channel,
	lockedOrigin *model.Channel,
	requestData map[string]any,
) map[string]any {
	effectiveRequestData := copyChannelUpdateRequestData(requestData)
	if channel == nil || staleOrigin == nil || lockedOrigin == nil {
		return effectiveRequestData
	}

	channel.ChannelInfo = lockedOrigin.ChannelInfo
	useLockedChannelFieldForStaleNoop(effectiveRequestData, "type", channel.Type, staleOrigin.Type, lockedOrigin.Type, func() {
		channel.Type = lockedOrigin.Type
	})
	useLockedChannelFieldForStaleNoop(effectiveRequestData, "key", channel.Key, staleOrigin.Key, lockedOrigin.Key, func() {
		channel.Key = lockedOrigin.Key
	})
	useLockedChannelFieldForStaleNoop(effectiveRequestData, "openai_organization", pointerUpdateValue(channel.OpenAIOrganization), pointerUpdateValue(staleOrigin.OpenAIOrganization), pointerUpdateValue(lockedOrigin.OpenAIOrganization), func() {
		channel.OpenAIOrganization = clonePatchChannelPointer(lockedOrigin.OpenAIOrganization)
	})
	useLockedChannelFieldForStaleNoop(effectiveRequestData, "test_model", pointerUpdateValue(channel.TestModel), pointerUpdateValue(staleOrigin.TestModel), pointerUpdateValue(lockedOrigin.TestModel), func() {
		channel.TestModel = clonePatchChannelPointer(lockedOrigin.TestModel)
	})
	useLockedChannelFieldForStaleNoop(effectiveRequestData, "name", channel.Name, staleOrigin.Name, lockedOrigin.Name, func() {
		channel.Name = lockedOrigin.Name
	})
	useLockedChannelFieldForStaleNoop(effectiveRequestData, "weight", pointerUpdateValue(channel.Weight), pointerUpdateValue(staleOrigin.Weight), pointerUpdateValue(lockedOrigin.Weight), func() {
		channel.Weight = clonePatchChannelPointer(lockedOrigin.Weight)
	})
	useLockedChannelFieldForStaleNoop(effectiveRequestData, "base_url", pointerUpdateValue(channel.BaseURL), pointerUpdateValue(staleOrigin.BaseURL), pointerUpdateValue(lockedOrigin.BaseURL), func() {
		channel.BaseURL = clonePatchChannelPointer(lockedOrigin.BaseURL)
	})
	useLockedChannelFieldForStaleNoop(effectiveRequestData, "other", channel.Other, staleOrigin.Other, lockedOrigin.Other, func() {
		channel.Other = lockedOrigin.Other
	})
	useLockedChannelFieldForStaleNoop(effectiveRequestData, "models", channel.Models, staleOrigin.Models, lockedOrigin.Models, func() {
		channel.Models = lockedOrigin.Models
	})
	useLockedChannelFieldForStaleNoop(effectiveRequestData, "group", channel.Group, staleOrigin.Group, lockedOrigin.Group, func() {
		channel.Group = lockedOrigin.Group
	})
	useLockedChannelFieldForStaleNoop(effectiveRequestData, "model_mapping", pointerUpdateValue(channel.ModelMapping), pointerUpdateValue(staleOrigin.ModelMapping), pointerUpdateValue(lockedOrigin.ModelMapping), func() {
		channel.ModelMapping = clonePatchChannelPointer(lockedOrigin.ModelMapping)
	})
	useLockedChannelFieldForStaleNoop(effectiveRequestData, "status_code_mapping", pointerUpdateValue(channel.StatusCodeMapping), pointerUpdateValue(staleOrigin.StatusCodeMapping), pointerUpdateValue(lockedOrigin.StatusCodeMapping), func() {
		channel.StatusCodeMapping = clonePatchChannelPointer(lockedOrigin.StatusCodeMapping)
	})
	useLockedChannelFieldForStaleNoop(effectiveRequestData, "priority", pointerUpdateValue(channel.Priority), pointerUpdateValue(staleOrigin.Priority), pointerUpdateValue(lockedOrigin.Priority), func() {
		channel.Priority = clonePatchChannelPointer(lockedOrigin.Priority)
	})
	useLockedChannelFieldForStaleNoop(effectiveRequestData, "auto_ban", pointerUpdateValue(channel.AutoBan), pointerUpdateValue(staleOrigin.AutoBan), pointerUpdateValue(lockedOrigin.AutoBan), func() {
		channel.AutoBan = clonePatchChannelPointer(lockedOrigin.AutoBan)
	})
	useLockedChannelFieldForStaleNoop(effectiveRequestData, "other_info", channel.OtherInfo, staleOrigin.OtherInfo, lockedOrigin.OtherInfo, func() {
		channel.OtherInfo = lockedOrigin.OtherInfo
	})
	useLockedChannelFieldForStaleNoop(effectiveRequestData, "tag", pointerUpdateValue(channel.Tag), pointerUpdateValue(staleOrigin.Tag), pointerUpdateValue(lockedOrigin.Tag), func() {
		channel.Tag = clonePatchChannelPointer(lockedOrigin.Tag)
	})
	useLockedChannelFieldForStaleNoop(effectiveRequestData, "setting", pointerUpdateValue(channel.Setting), pointerUpdateValue(staleOrigin.Setting), pointerUpdateValue(lockedOrigin.Setting), func() {
		channel.Setting = clonePatchChannelPointer(lockedOrigin.Setting)
	})
	useLockedChannelFieldForStaleNoop(effectiveRequestData, "param_override", pointerUpdateValue(channel.ParamOverride), pointerUpdateValue(staleOrigin.ParamOverride), pointerUpdateValue(lockedOrigin.ParamOverride), func() {
		channel.ParamOverride = clonePatchChannelPointer(lockedOrigin.ParamOverride)
	})
	useLockedChannelFieldForStaleNoop(effectiveRequestData, "header_override", pointerUpdateValue(channel.HeaderOverride), pointerUpdateValue(staleOrigin.HeaderOverride), pointerUpdateValue(lockedOrigin.HeaderOverride), func() {
		channel.HeaderOverride = clonePatchChannelPointer(lockedOrigin.HeaderOverride)
	})
	useLockedChannelFieldForStaleNoop(effectiveRequestData, "remark", pointerUpdateValue(channel.Remark), pointerUpdateValue(staleOrigin.Remark), pointerUpdateValue(lockedOrigin.Remark), func() {
		channel.Remark = clonePatchChannelPointer(lockedOrigin.Remark)
	})
	useLockedChannelFieldForStaleNoop(effectiveRequestData, "settings", strings.TrimSpace(channel.OtherSettings), strings.TrimSpace(staleOrigin.OtherSettings), strings.TrimSpace(lockedOrigin.OtherSettings), func() {
		channel.OtherSettings = lockedOrigin.OtherSettings
	})
	if channel.MultiKeyMode != nil && *channel.MultiKeyMode != "" {
		channel.ChannelInfo.MultiKeyMode = constant.MultiKeyMode(*channel.MultiKeyMode)
	}
	return effectiveRequestData
}

func applyPatchChannelMultiKeyUpdate(channel *PatchChannel, originChannel *model.Channel) error {
	if channel == nil || originChannel == nil {
		return nil
	}
	if channel.MultiKeyMode != nil && *channel.MultiKeyMode != "" {
		mode := constant.MultiKeyMode(strings.TrimSpace(*channel.MultiKeyMode))
		if mode != constant.MultiKeyModeRandom && mode != constant.MultiKeyModePolling {
			return channelUpdateValidationError{message: "多密钥模式无效"}
		}
		channel.ChannelInfo.MultiKeyMode = mode
	}
	if channel.KeyMode == nil || !channel.ChannelInfo.IsMultiKey {
		return nil
	}
	switch *channel.KeyMode {
	case "append":
		if originChannel.Key == "" {
			return nil
		}
		var newKeys []string
		var existingKeys []string

		existingKeys = originChannel.GetKeys()

		if channel.Type == constant.ChannelTypeVertexAi && channel.GetOtherSettings().VertexKeyType != dto.VertexKeyTypeAPIKey {
			if strings.HasPrefix(strings.TrimSpace(channel.Key), "[") {
				array, err := getVertexArrayKeys(channel.Key)
				if err != nil {
					return channelUpdateValidationError{message: "追加密钥解析失败: " + err.Error()}
				}
				newKeys = array
			} else {
				newKeys = []string{channel.Key}
			}
		} else {
			inputKeys := strings.Split(channel.Key, "\n")
			for _, key := range inputKeys {
				key = strings.TrimSpace(key)
				if key != "" {
					newKeys = append(newKeys, key)
				}
			}
		}

		seen := make(map[string]struct{}, len(existingKeys)+len(newKeys))
		for _, key := range existingKeys {
			normalized := strings.TrimSpace(key)
			if normalized == "" {
				continue
			}
			seen[normalized] = struct{}{}
		}
		dedupedNewKeys := make([]string, 0, len(newKeys))
		for _, key := range newKeys {
			normalized := strings.TrimSpace(key)
			if normalized == "" {
				continue
			}
			if _, ok := seen[normalized]; ok {
				continue
			}
			seen[normalized] = struct{}{}
			dedupedNewKeys = append(dedupedNewKeys, normalized)
		}

		allKeys := append(existingKeys, dedupedNewKeys...)
		channel.Key = strings.Join(allKeys, "\n")
	case "replace":
	}
	return nil
}

func updateChannelColumnsWithLockedState(
	channel *PatchChannel,
	staleOrigin *model.Channel,
	requestData map[string]any,
	canSensitiveWrite bool,
) (*model.Channel, error) {
	var lockedBefore model.Channel
	_, clientRequestedSettings := requestData["settings"]
	clientRequestedSettingsRaw := channel.OtherSettings
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var locked model.Channel
		if err := model.LockForUpdate(tx).Where("id = ?", channel.Id).First(&locked).Error; err != nil {
			return err
		}
		lockedBefore = locked
		effectiveRequestData := mergeLockedChannelStateForUpdate(channel, staleOrigin, &locked, requestData)
		_, settingsRequested := effectiveRequestData["settings"]
		effectiveClientRequestedSettings := clientRequestedSettings && settingsRequested
		if settingsRequested {
			clientRequestedSettingsRaw = channel.OtherSettings
		}
		supportsUpstreamModelUpdate := channelSupportsUpstreamModelUpdate(&channel.Channel)
		sourceChanged := false
		if supportsUpstreamModelUpdate {
			sourceChanged = channelUpstreamModelSourceChangedForUpdate(&locked, channel, effectiveRequestData)
		}
		if !supportsUpstreamModelUpdate {
			settingsToClean := channel.OtherSettings
			if !settingsRequested {
				settingsToClean = locked.OtherSettings
			}
			cleanedSettings, changed, err := channelSettingsWithoutUpstreamModelUpdateFields(settingsToClean)
			if err != nil {
				if settingsRequested {
					return err
				}
			} else if changed {
				channel.OtherSettings = cleanedSettings
				effectiveRequestData["settings"] = cleanedSettings
			}
		} else if settingsRequested {
			channel.OtherSettings = mergeChannelUpstreamModelUpdateRuntimeSettingsForUpdate(channel.OtherSettings, locked.OtherSettings, sourceChanged)
		} else if supportsUpstreamModelUpdate && sourceChanged {
			if cleanedSettings, changed := clearChannelUpstreamModelUpdateRuntimeSettingsForSourceChange(locked.OtherSettings); changed {
				channel.OtherSettings = cleanedSettings
				effectiveRequestData["settings"] = cleanedSettings
			}
		}
		permissionRequestData := effectiveRequestData
		settingsOnlyChangedByServer := !effectiveClientRequestedSettings ||
			sameChannelUpstreamModelSourceSettings(locked.OtherSettings, clientRequestedSettingsRaw)
		if settingsOnlyChangedByServer {
			permissionRequestData = copyChannelUpdateRequestData(effectiveRequestData)
			delete(permissionRequestData, "settings")
		}
		if channelHasSensitiveChanges(channel, &locked, permissionRequestData) && !canSensitiveWrite {
			return errChannelSensitivePermissionDenied
		}
		if err := applyPatchChannelMultiKeyUpdate(channel, &locked); err != nil {
			return err
		}
		return channel.Channel.UpdateColumnsWithTx(tx, channelUpdateColumns(channel, effectiveRequestData))
	})
	if err != nil {
		return nil, err
	}
	return &lockedBefore, nil
}

type ChannelStatusRequest struct {
	Status int `json:"status"`
}

type ChannelStatusBatchRequest struct {
	Ids    []int `json:"ids"`
	Status int   `json:"status"`
}

func UpdateChannel(c *gin.Context) {
	channel := PatchChannel{}
	rawBody, err := c.GetRawData()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := common.Unmarshal(rawBody, &channel); err != nil {
		common.ApiError(c, err)
		return
	}
	var requestData map[string]any
	if err := common.Unmarshal(rawBody, &requestData); err != nil {
		common.ApiError(c, err)
		return
	}
	if _, ok := requestData["status"]; ok {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if _, ok := requestData["type"]; ok && (channel.Type <= constant.ChannelTypeUnknown || channel.Type >= len(constant.ChannelBaseURLs)) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	clearChannelReadOnlyFields(&channel, requestData)

	// Preserve existing ChannelInfo to ensure multi-key channels keep correct state even if the client does not send ChannelInfo in the request.
	originChannel, err := model.GetChannelById(channel.Id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	// Always copy the original ChannelInfo so that fields like IsMultiKey and MultiKeySize are retained.
	channel.ChannelInfo = originChannel.ChannelInfo

	validationChannel, err := preparePatchChannelForValidation(&channel, originChannel, requestData)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	// 使用统一的校验函数。先用原始 ChannelInfo 补齐聚合密钥状态，
	// 再清理不支持渠道的上游模型更新残留，避免旧设置阻断正常更新。
	if err := validateChannel(&validationChannel, false); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	if channelHasSensitiveChanges(&channel, originChannel, requestData) &&
		!authz.Can(c.GetInt("id"), c.GetInt("role"), authz.ChannelSensitiveWrite) {
		common.ApiErrorI18n(c, i18n.MsgAuthInsufficientPrivilege)
		return
	}

	auditOrigin, err := updateChannelColumnsWithLockedState(
		&channel,
		originChannel,
		requestData,
		authz.Can(c.GetInt("id"), c.GetInt("role"), authz.ChannelSensitiveWrite),
	)
	if err != nil {
		if errors.Is(err, errChannelSensitivePermissionDenied) {
			common.ApiErrorI18n(c, i18n.MsgAuthInsufficientPrivilege)
			return
		}
		var validationErr channelUpdateValidationError
		if errors.As(err, &validationErr) {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": validationErr.Error(),
			})
			return
		}
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	service.ResetProxyClientCache()
	// 记录变更的字段名（语言无关的字段标识），密钥仅记录"已更换"绝不记录内容。
	changedFields := make([]string, 0)
	if auditOrigin == nil {
		auditOrigin = originChannel
	}
	if channel.Models != auditOrigin.Models {
		changedFields = append(changedFields, "models")
	}
	if channel.Group != auditOrigin.Group {
		changedFields = append(changedFields, "group")
	}
	if channel.Type != auditOrigin.Type {
		changedFields = append(changedFields, "type")
	}
	if !equalStringPtr(channel.BaseURL, auditOrigin.BaseURL) {
		changedFields = append(changedFields, "base_url")
	}
	if channel.Key != "" && channel.Key != auditOrigin.Key {
		changedFields = append(changedFields, "key")
	}
	recordManageAudit(c, "channel.update", map[string]interface{}{
		"id":             channel.Id,
		"name":           channel.Name,
		"changed_fields": changedFields,
	})
	channel.Key = ""
	clearChannelInfo(&channel.Channel)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    channel,
	})
	return
}

func UpdateChannelStatus(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	req := ChannelStatusRequest{}
	if err := c.ShouldBindJSON(&req); err != nil || !isManageableChannelStatus(req.Status) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	changed := model.UpdateChannelStatus(id, "", req.Status, "manual operation")
	if changed {
		model.InitChannelCache()
		service.ResetProxyClientCache()
	}
	recordManageAudit(c, "channel.status_update", map[string]interface{}{
		"id":      id,
		"status":  req.Status,
		"changed": changed,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    changed,
	})
}

func BatchUpdateChannelStatus(c *gin.Context) {
	req := ChannelStatusBatchRequest{}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Ids) == 0 || !isManageableChannelStatus(req.Status) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	changedCount := 0
	for _, id := range req.Ids {
		if model.UpdateChannelStatus(id, "", req.Status, "manual batch operation") {
			changedCount++
		}
	}
	if changedCount > 0 {
		model.InitChannelCache()
		service.ResetProxyClientCache()
	}
	recordManageAudit(c, "channel.status_update_batch", map[string]interface{}{
		"count":  changedCount,
		"total":  len(req.Ids),
		"status": req.Status,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    changedCount,
	})
}

func isManageableChannelStatus(status int) bool {
	return status == common.ChannelStatusEnabled || status == common.ChannelStatusManuallyDisabled
}

// equalStringPtr 比较两个 *string 是否相等（均为 nil 视为相等）。
func equalStringPtr(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

type fetchModelsRequest struct {
	Id              int    `json:"id"`
	BaseURL         string `json:"base_url"`
	BaseURLOverride bool   `json:"base_url_override"`
	DraftOverride   bool   `json:"draft_override"`
	Type            int    `json:"type"`
	Key             string `json:"key"`
	Setting         string `json:"setting"`
	Settings        string `json:"settings"`
	HeaderOverride  string `json:"header_override"`
	Other           string `json:"other"`
}

func normalizeCodexOAuthDraftFetchKey(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "{") {
		return "", false
	}
	var key map[string]any
	if err := common.Unmarshal([]byte(raw), &key); err != nil || key == nil {
		return "", false
	}
	return raw, true
}

func prepareDraftFetchModelKey(channel *model.Channel) {
	channel.Keys = nil
	if channel.Type == constant.ChannelTypeCodex {
		if key, ok := normalizeCodexOAuthDraftFetchKey(channel.Key); ok {
			channel.Key = key
			channel.ChannelInfo.IsMultiKey = false
			channel.ChannelInfo.MultiKeySize = 0
			channel.ChannelInfo.MultiKeyStatusList = nil
			channel.ChannelInfo.MultiKeyDisabledReason = nil
			channel.ChannelInfo.MultiKeyDisabledTime = nil
			channel.ChannelInfo.MultiKeyPollingIndex = 0
			channel.ChannelInfo.MultiKeyMode = constant.MultiKeyModeRandom
			return
		}
	}
	keys := channel.GetKeys()

	for _, key := range keys {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			channel.Key = trimmed
			break
		}
	}

	channel.Keys = nil
	channel.ChannelInfo.IsMultiKey = false
	channel.ChannelInfo.MultiKeySize = 0
	channel.ChannelInfo.MultiKeyStatusList = nil
	channel.ChannelInfo.MultiKeyDisabledReason = nil
	channel.ChannelInfo.MultiKeyDisabledTime = nil
	channel.ChannelInfo.MultiKeyPollingIndex = 0
	channel.ChannelInfo.MultiKeyMode = constant.MultiKeyModeRandom
}

func fetchModelsRequestHasMultipleDraftKeys(req fetchModelsRequest, channelType int) bool {
	if channelType == constant.ChannelTypeCodex {
		if _, ok := normalizeCodexOAuthDraftFetchKey(req.Key); ok {
			return false
		}
	}
	keyCount := 0
	for _, key := range strings.Split(req.Key, "\n") {
		if strings.TrimSpace(key) == "" {
			continue
		}
		keyCount++
		if keyCount > 1 {
			return true
		}
	}
	return false
}

func applyFetchModelsRequest(channel *model.Channel, req fetchModelsRequest) {
	if req.Type != 0 {
		channel.Type = req.Type
	}
	if strings.TrimSpace(req.Key) != "" {
		channel.Key = req.Key
		prepareDraftFetchModelKey(channel)
	}
	if req.Id == 0 || req.BaseURLOverride || strings.TrimSpace(req.BaseURL) != "" {
		baseURL := strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
		channel.BaseURL = common.GetPointer(baseURL)
	}
	if req.DraftOverride || req.Setting != "" {
		channel.Setting = common.GetPointer(req.Setting)
	}
	if req.DraftOverride || req.Settings != "" {
		channel.OtherSettings = req.Settings
	}
	if req.DraftOverride || req.HeaderOverride != "" {
		channel.HeaderOverride = common.GetPointer(req.HeaderOverride)
	}
	if req.DraftOverride || req.Other != "" {
		channel.Other = req.Other
	}
}

func fetchModelsRequestUsesDraftOverrides(req fetchModelsRequest) bool {
	if req.Id == 0 || req.DraftOverride || req.Type != 0 || req.BaseURLOverride {
		return true
	}
	return strings.TrimSpace(req.Key) != "" ||
		strings.TrimSpace(req.BaseURL) != "" ||
		req.Setting != "" ||
		req.Settings != "" ||
		req.HeaderOverride != "" ||
		req.Other != ""
}

func fetchModelsRequestAllowsCodexCredentialRefresh(req fetchModelsRequest) bool {
	return !fetchModelsRequestUsesDraftOverrides(req)
}

func FetchModels(c *gin.Context) {
	var req fetchModelsRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "请求无效",
		})
		return
	}

	channel := &model.Channel{}
	if req.Id > 0 {
		existing, err := model.GetChannelById(req.Id, true)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": fmt.Sprintf("获取渠道失败: %s", err.Error()),
			})
			return
		}
		channel = existing
	}
	requestedType := req.Type
	if requestedType == 0 {
		requestedType = channel.Type
	}
	if requestedType == constant.ChannelTypeCodex && fetchModelsRequestHasMultipleDraftKeys(req, requestedType) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Codex 渠道拉取模型不支持多 Key 草稿",
		})
		return
	}
	applyFetchModelsRequest(channel, req)
	if channel.Type <= constant.ChannelTypeUnknown || channel.Type >= len(constant.ChannelBaseURLs) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的渠道类型",
		})
		return
	}

	originalCodexKey := ""
	if channel.Type == constant.ChannelTypeCodex {
		originalCodexKey = channel.Key
	}
	ids, err := fetchChannelUpstreamModelIDsWithOptions(c.Request.Context(), channel, channelUpstreamModelFetchOptions{
		AllowCodexCredentialRefresh: fetchModelsRequestAllowsCodexCredentialRefresh(req),
	})
	err = refreshRuntimeCacheAfterCodexCredentialChange(channel, originalCodexKey, err)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("获取模型列表失败: %s", err.Error()),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    ids,
	})
}
func BatchSetChannelTag(c *gin.Context) {
	channelBatch := ChannelBatch{}
	err := c.ShouldBindJSON(&channelBatch)
	if err != nil || len(channelBatch.Ids) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "参数错误",
		})
		return
	}
	err = model.BatchSetChannelTag(channelBatch.Ids, channelBatch.Tag)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	recordManageAudit(c, "channel.tag_batch_set", map[string]interface{}{
		"count": len(channelBatch.Ids),
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    len(channelBatch.Ids),
	})
	return
}

func GetTagModels(c *gin.Context) {
	tag := c.Query("tag")
	if tag == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "标签不能为空",
		})
		return
	}

	channels, err := model.GetChannelsByTag(tag, false, false) // idSort=false, selectAll=false
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	var longestModels string
	maxLength := 0

	// Find the longest models string among all channels with the given tag
	for _, channel := range channels {
		if channel.Models != "" {
			currentModels := strings.Split(channel.Models, ",")
			if len(currentModels) > maxLength {
				maxLength = len(currentModels)
				longestModels = channel.Models
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    longestModels,
	})
	return
}

// CopyChannel handles cloning an existing channel with its key.
// POST /api/channel/copy/:id
// Optional query params:
//
//	suffix         - string appended to the original name (default "_复制")
//	reset_balance  - bool, when true will reset balance & used_quota to 0 (default true)
func CopyChannel(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "ID 无效"})
		return
	}

	suffix := c.DefaultQuery("suffix", "_复制")
	resetBalance := true
	if rbStr := c.DefaultQuery("reset_balance", "true"); rbStr != "" {
		if v, err := strconv.ParseBool(rbStr); err == nil {
			resetBalance = v
		}
	}

	// fetch original channel with key
	origin, err := model.GetChannelById(id, true)
	if err != nil {
		common.SysError("failed to get channel by id: " + err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "获取渠道信息失败，请稍后重试"})
		return
	}

	// clone channel
	clone := *origin // shallow copy is sufficient as we will overwrite primitives
	clone.Id = 0     // let DB auto-generate
	clone.CreatedTime = common.GetTimestamp()
	clone.Name = origin.Name + suffix
	clone.TestTime = 0
	clone.ResponseTime = 0
	if resetBalance {
		clone.Balance = 0
		clone.UsedQuota = 0
	}

	if err := normalizeChannelUpstreamModelUpdateSettingsForCreate(&clone); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "渠道设置无效: " + err.Error()})
		return
	}
	if err := validateChannel(&clone, false); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "渠道设置无效: " + err.Error()})
		return
	}

	// insert
	if err := clone.Insert(); err != nil {
		common.SysError("failed to clone channel: " + err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "复制渠道失败，请稍后重试"})
		return
	}
	model.InitChannelCache()
	recordManageAudit(c, "channel.copy", map[string]interface{}{
		"sourceId": id,
		"id":       clone.Id,
		"name":     clone.Name,
	})
	// success
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"id": clone.Id}})
}

// MultiKeyManageRequest represents the request for multi-key management operations
type MultiKeyManageRequest struct {
	ChannelId int    `json:"channel_id"`
	Action    string `json:"action"`              // "disable_key", "enable_key", "delete_key", "delete_disabled_keys", "get_key_status"
	KeyIndex  *int   `json:"key_index,omitempty"` // for disable_key, enable_key, and delete_key actions
	Page      int    `json:"page,omitempty"`      // for get_key_status pagination
	PageSize  int    `json:"page_size,omitempty"` // for get_key_status pagination
	Status    *int   `json:"status,omitempty"`    // for get_key_status filtering: 1=enabled, 2=manual_disabled, 3=auto_disabled, nil=all
}

// MultiKeyStatusResponse represents the response for key status query
type MultiKeyStatusResponse struct {
	Keys       []KeyStatus `json:"keys"`
	Total      int         `json:"total"`
	Page       int         `json:"page"`
	PageSize   int         `json:"page_size"`
	TotalPages int         `json:"total_pages"`
	// Statistics
	EnabledCount        int `json:"enabled_count"`
	ManualDisabledCount int `json:"manual_disabled_count"`
	AutoDisabledCount   int `json:"auto_disabled_count"`
}

type KeyStatus struct {
	Index        int    `json:"index"`
	Status       int    `json:"status"` // 1: enabled, 2: disabled
	DisabledTime int64  `json:"disabled_time,omitempty"`
	Reason       string `json:"reason,omitempty"`
	KeyPreview   string `json:"key_preview"` // first 10 chars of key for identification
}

// ManageMultiKeys handles multi-key management operations
func ManageMultiKeys(c *gin.Context) {
	request := MultiKeyManageRequest{}
	err := c.ShouldBindJSON(&request)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	channel, err := model.GetChannelById(request.ChannelId, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "渠道不存在",
		})
		return
	}

	if !channel.ChannelInfo.IsMultiKey {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "该渠道不是多密钥模式",
		})
		return
	}
	if multiKeyActionRequiresSensitiveWrite(request.Action) &&
		!authz.Can(c.GetInt("id"), c.GetInt("role"), authz.ChannelSensitiveWrite) {
		common.ApiErrorI18n(c, i18n.MsgAuthInsufficientPrivilege)
		return
	}

	// get_key_status 为只读查询，不记录审计；其余为修改操作，记录审计并跳过中间件兜底。
	if request.Action == "get_key_status" {
		markAuditLogged(c)
	} else {
		recordManageAudit(c, "channel.multi_key_manage", map[string]interface{}{
			"action": request.Action,
			"id":     channel.Id,
		})
	}

	lock := model.GetChannelPollingLock(channel.Id)
	lock.Lock()
	defer lock.Unlock()

	switch request.Action {
	case "get_key_status":
		keys := channel.GetKeys()

		// Default pagination parameters
		page := request.Page
		pageSize := request.PageSize
		if page <= 0 {
			page = 1
		}
		if pageSize <= 0 {
			pageSize = 50 // Default page size
		}

		// Statistics for all keys (unchanged by filtering)
		var enabledCount, manualDisabledCount, autoDisabledCount int

		// Build all key status data first
		var allKeyStatusList []KeyStatus
		for i, key := range keys {
			status := 1 // default enabled
			var disabledTime int64
			var reason string

			if channel.ChannelInfo.MultiKeyStatusList != nil {
				if s, exists := channel.ChannelInfo.MultiKeyStatusList[i]; exists {
					status = s
				}
			}

			// Count for statistics (all keys)
			switch status {
			case 1:
				enabledCount++
			case 2:
				manualDisabledCount++
			case 3:
				autoDisabledCount++
			}

			if status != 1 {
				if channel.ChannelInfo.MultiKeyDisabledTime != nil {
					disabledTime = channel.ChannelInfo.MultiKeyDisabledTime[i]
				}
				if channel.ChannelInfo.MultiKeyDisabledReason != nil {
					reason = channel.ChannelInfo.MultiKeyDisabledReason[i]
				}
			}

			// Create key preview (first 10 chars)
			keyPreview := key
			if len(key) > 10 {
				keyPreview = key[:10] + "..."
			}

			allKeyStatusList = append(allKeyStatusList, KeyStatus{
				Index:        i,
				Status:       status,
				DisabledTime: disabledTime,
				Reason:       reason,
				KeyPreview:   keyPreview,
			})
		}

		// Apply status filter if specified
		var filteredKeyStatusList []KeyStatus
		if request.Status != nil {
			for _, keyStatus := range allKeyStatusList {
				if keyStatus.Status == *request.Status {
					filteredKeyStatusList = append(filteredKeyStatusList, keyStatus)
				}
			}
		} else {
			filteredKeyStatusList = allKeyStatusList
		}

		// Calculate pagination based on filtered results
		filteredTotal := len(filteredKeyStatusList)
		totalPages := (filteredTotal + pageSize - 1) / pageSize
		if totalPages == 0 {
			totalPages = 1
		}
		if page > totalPages {
			page = totalPages
		}

		// Calculate range for current page
		start := (page - 1) * pageSize
		end := start + pageSize
		if end > filteredTotal {
			end = filteredTotal
		}

		// Get the page data
		var pageKeyStatusList []KeyStatus
		if start < filteredTotal {
			pageKeyStatusList = filteredKeyStatusList[start:end]
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data": MultiKeyStatusResponse{
				Keys:                pageKeyStatusList,
				Total:               filteredTotal, // Total of filtered results
				Page:                page,
				PageSize:            pageSize,
				TotalPages:          totalPages,
				EnabledCount:        enabledCount,        // Overall statistics
				ManualDisabledCount: manualDisabledCount, // Overall statistics
				AutoDisabledCount:   autoDisabledCount,   // Overall statistics
			},
		})
		return

	case "disable_key":
		if request.KeyIndex == nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "未指定要禁用的密钥索引",
			})
			return
		}

		keyIndex := *request.KeyIndex
		if keyIndex < 0 || keyIndex >= channel.ChannelInfo.MultiKeySize {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "密钥索引超出范围",
			})
			return
		}

		if channel.ChannelInfo.MultiKeyStatusList == nil {
			channel.ChannelInfo.MultiKeyStatusList = make(map[int]int)
		}
		if channel.ChannelInfo.MultiKeyDisabledTime == nil {
			channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
		}
		if channel.ChannelInfo.MultiKeyDisabledReason == nil {
			channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)
		}

		channel.ChannelInfo.MultiKeyStatusList[keyIndex] = 2 // disabled

		err = channel.Update()
		if err != nil {
			common.ApiError(c, err)
			return
		}

		model.InitChannelCache()
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "密钥已禁用",
		})
		return

	case "enable_key":
		if request.KeyIndex == nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "未指定要启用的密钥索引",
			})
			return
		}

		keyIndex := *request.KeyIndex
		if keyIndex < 0 || keyIndex >= channel.ChannelInfo.MultiKeySize {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "密钥索引超出范围",
			})
			return
		}

		// 从状态列表中删除该密钥的记录，使其回到默认启用状态
		if channel.ChannelInfo.MultiKeyStatusList != nil {
			delete(channel.ChannelInfo.MultiKeyStatusList, keyIndex)
		}
		if channel.ChannelInfo.MultiKeyDisabledTime != nil {
			delete(channel.ChannelInfo.MultiKeyDisabledTime, keyIndex)
		}
		if channel.ChannelInfo.MultiKeyDisabledReason != nil {
			delete(channel.ChannelInfo.MultiKeyDisabledReason, keyIndex)
		}

		err = channel.Update()
		if err != nil {
			common.ApiError(c, err)
			return
		}

		model.InitChannelCache()
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "密钥已启用",
		})
		return

	case "enable_all_keys":
		// 清空所有禁用状态，使所有密钥回到默认启用状态
		var enabledCount int
		if channel.ChannelInfo.MultiKeyStatusList != nil {
			enabledCount = len(channel.ChannelInfo.MultiKeyStatusList)
		}

		channel.ChannelInfo.MultiKeyStatusList = make(map[int]int)
		channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
		channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)

		err = channel.Update()
		if err != nil {
			common.ApiError(c, err)
			return
		}

		model.InitChannelCache()
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": fmt.Sprintf("已启用 %d 个密钥", enabledCount),
		})
		return

	case "disable_all_keys":
		// 禁用所有启用的密钥
		if channel.ChannelInfo.MultiKeyStatusList == nil {
			channel.ChannelInfo.MultiKeyStatusList = make(map[int]int)
		}
		if channel.ChannelInfo.MultiKeyDisabledTime == nil {
			channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
		}
		if channel.ChannelInfo.MultiKeyDisabledReason == nil {
			channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)
		}

		var disabledCount int
		for i := 0; i < channel.ChannelInfo.MultiKeySize; i++ {
			status := 1 // default enabled
			if s, exists := channel.ChannelInfo.MultiKeyStatusList[i]; exists {
				status = s
			}

			// 只禁用当前启用的密钥
			if status == 1 {
				channel.ChannelInfo.MultiKeyStatusList[i] = 2 // disabled
				disabledCount++
			}
		}

		if disabledCount == 0 {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "没有可禁用的密钥",
			})
			return
		}

		err = channel.Update()
		if err != nil {
			common.ApiError(c, err)
			return
		}

		model.InitChannelCache()
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": fmt.Sprintf("已禁用 %d 个密钥", disabledCount),
		})
		return

	case "delete_key":
		if request.KeyIndex == nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "未指定要删除的密钥索引",
			})
			return
		}

		keyIndex := *request.KeyIndex
		if keyIndex < 0 || keyIndex >= channel.ChannelInfo.MultiKeySize {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "密钥索引超出范围",
			})
			return
		}

		keys := channel.GetKeys()
		var remainingKeys []string
		var newStatusList = make(map[int]int)
		var newDisabledTime = make(map[int]int64)
		var newDisabledReason = make(map[int]string)

		newIndex := 0
		for i, key := range keys {
			// 跳过要删除的密钥
			if i == keyIndex {
				continue
			}

			remainingKeys = append(remainingKeys, key)

			// 保留其他密钥的状态信息，重新索引
			if channel.ChannelInfo.MultiKeyStatusList != nil {
				if status, exists := channel.ChannelInfo.MultiKeyStatusList[i]; exists && status != 1 {
					newStatusList[newIndex] = status
				}
			}
			if channel.ChannelInfo.MultiKeyDisabledTime != nil {
				if t, exists := channel.ChannelInfo.MultiKeyDisabledTime[i]; exists {
					newDisabledTime[newIndex] = t
				}
			}
			if channel.ChannelInfo.MultiKeyDisabledReason != nil {
				if r, exists := channel.ChannelInfo.MultiKeyDisabledReason[i]; exists {
					newDisabledReason[newIndex] = r
				}
			}
			newIndex++
		}

		if len(remainingKeys) == 0 {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "不能删除最后一个密钥",
			})
			return
		}

		// Update channel with remaining keys
		channel.Key = strings.Join(remainingKeys, "\n")
		channel.ChannelInfo.MultiKeySize = len(remainingKeys)
		channel.ChannelInfo.MultiKeyStatusList = newStatusList
		channel.ChannelInfo.MultiKeyDisabledTime = newDisabledTime
		channel.ChannelInfo.MultiKeyDisabledReason = newDisabledReason

		err = channel.Update()
		if err != nil {
			common.ApiError(c, err)
			return
		}

		model.InitChannelCache()
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "密钥已删除",
		})
		return

	case "delete_disabled_keys":
		keys := channel.GetKeys()
		var remainingKeys []string
		var deletedCount int
		var newStatusList = make(map[int]int)
		var newDisabledTime = make(map[int]int64)
		var newDisabledReason = make(map[int]string)

		newIndex := 0
		for i, key := range keys {
			status := 1 // default enabled
			if channel.ChannelInfo.MultiKeyStatusList != nil {
				if s, exists := channel.ChannelInfo.MultiKeyStatusList[i]; exists {
					status = s
				}
			}

			// 只删除自动禁用（status == 3）的密钥，保留启用（status == 1）和手动禁用（status == 2）的密钥
			if status == 3 {
				deletedCount++
			} else {
				remainingKeys = append(remainingKeys, key)
				// 保留非自动禁用密钥的状态信息，重新索引
				if status != 1 {
					newStatusList[newIndex] = status
					if channel.ChannelInfo.MultiKeyDisabledTime != nil {
						if t, exists := channel.ChannelInfo.MultiKeyDisabledTime[i]; exists {
							newDisabledTime[newIndex] = t
						}
					}
					if channel.ChannelInfo.MultiKeyDisabledReason != nil {
						if r, exists := channel.ChannelInfo.MultiKeyDisabledReason[i]; exists {
							newDisabledReason[newIndex] = r
						}
					}
				}
				newIndex++
			}
		}

		if deletedCount == 0 {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "没有需要删除的自动禁用密钥",
			})
			return
		}

		// Update channel with remaining keys
		channel.Key = strings.Join(remainingKeys, "\n")
		channel.ChannelInfo.MultiKeySize = len(remainingKeys)
		channel.ChannelInfo.MultiKeyStatusList = newStatusList
		channel.ChannelInfo.MultiKeyDisabledTime = newDisabledTime
		channel.ChannelInfo.MultiKeyDisabledReason = newDisabledReason

		err = channel.Update()
		if err != nil {
			common.ApiError(c, err)
			return
		}

		model.InitChannelCache()
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": fmt.Sprintf("已删除 %d 个自动禁用的密钥", deletedCount),
			"data":    deletedCount,
		})
		return

	default:
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "不支持的操作",
		})
		return
	}
}

func multiKeyActionRequiresSensitiveWrite(action string) bool {
	return action == "delete_key" || action == "delete_disabled_keys"
}

// OllamaPullModel 拉取 Ollama 模型
func OllamaPullModel(c *gin.Context) {
	var req struct {
		ChannelID int    `json:"channel_id"`
		ModelName string `json:"model_name"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "请求参数无效",
		})
		return
	}

	if req.ChannelID == 0 || req.ModelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "必须提供渠道 ID 和模型名",
		})
		return
	}

	// 获取渠道信息
	channel, err := model.GetChannelById(req.ChannelID, true)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "未找到渠道",
		})
		return
	}

	// 检查是否是 Ollama 渠道
	if channel.Type != constant.ChannelTypeOllama {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "此操作仅支持 Ollama 渠道",
		})
		return
	}

	baseURL := constant.ChannelBaseURLs[channel.Type]
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}

	key := strings.Split(channel.Key, "\n")[0]
	err = ollama.PullOllamaModel(baseURL, key, req.ModelName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": fmt.Sprintf("Failed to pull model: %s", err.Error()),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("Model %s pulled successfully", req.ModelName),
	})
}

// OllamaPullModelStream 流式拉取 Ollama 模型
func OllamaPullModelStream(c *gin.Context) {
	var req struct {
		ChannelID int    `json:"channel_id"`
		ModelName string `json:"model_name"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "请求参数无效",
		})
		return
	}

	if req.ChannelID == 0 || req.ModelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "必须提供渠道 ID 和模型名",
		})
		return
	}

	// 获取渠道信息
	channel, err := model.GetChannelById(req.ChannelID, true)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "未找到渠道",
		})
		return
	}

	// 检查是否是 Ollama 渠道
	if channel.Type != constant.ChannelTypeOllama {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "此操作仅支持 Ollama 渠道",
		})
		return
	}

	baseURL := constant.ChannelBaseURLs[channel.Type]
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}

	// 设置 SSE 头部
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	key := strings.Split(channel.Key, "\n")[0]

	// 创建进度回调函数
	progressCallback := func(progress ollama.OllamaPullResponse) {
		data, _ := common.Marshal(progress)
		fmt.Fprintf(c.Writer, "data: %s\n\n", string(data))
		c.Writer.Flush()
	}

	// 执行拉取
	err = ollama.PullOllamaModelStream(baseURL, key, req.ModelName, progressCallback)

	if err != nil {
		errorData, _ := common.Marshal(gin.H{
			"error": err.Error(),
		})
		fmt.Fprintf(c.Writer, "data: %s\n\n", string(errorData))
	} else {
		successData, _ := common.Marshal(gin.H{
			"message": fmt.Sprintf("Model %s pulled successfully", req.ModelName),
		})
		fmt.Fprintf(c.Writer, "data: %s\n\n", string(successData))
	}

	// 发送结束标志
	fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
	c.Writer.Flush()
}

// OllamaDeleteModel 删除 Ollama 模型
func OllamaDeleteModel(c *gin.Context) {
	var req struct {
		ChannelID int    `json:"channel_id"`
		ModelName string `json:"model_name"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "请求参数无效",
		})
		return
	}

	if req.ChannelID == 0 || req.ModelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "必须提供渠道 ID 和模型名",
		})
		return
	}

	// 获取渠道信息
	channel, err := model.GetChannelById(req.ChannelID, true)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "未找到渠道",
		})
		return
	}

	// 检查是否是 Ollama 渠道
	if channel.Type != constant.ChannelTypeOllama {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "此操作仅支持 Ollama 渠道",
		})
		return
	}

	baseURL := constant.ChannelBaseURLs[channel.Type]
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}

	key := strings.Split(channel.Key, "\n")[0]
	err = ollama.DeleteOllamaModel(baseURL, key, req.ModelName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": fmt.Sprintf("Failed to delete model: %s", err.Error()),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("模型 %s 已删除", req.ModelName),
	})
}

// OllamaVersion 获取 Ollama 服务版本信息
func OllamaVersion(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "渠道 ID 无效",
		})
		return
	}

	channel, err := model.GetChannelById(id, true)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "未找到渠道",
		})
		return
	}

	if channel.Type != constant.ChannelTypeOllama {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "此操作仅支持 Ollama 渠道",
		})
		return
	}

	baseURL := constant.ChannelBaseURLs[channel.Type]
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}

	key := strings.Split(channel.Key, "\n")[0]
	version, err := ollama.FetchOllamaVersion(baseURL, key)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("获取Ollama版本失败: %s", err.Error()),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"version": version,
		},
	})
}
