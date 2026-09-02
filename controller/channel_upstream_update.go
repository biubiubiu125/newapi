package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/advancedcustom"
	"github.com/QuantumNous/new-api/relay/channel/gemini"
	"github.com/QuantumNous/new-api/relay/channel/ollama"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"gorm.io/gorm"
)

const (
	channelUpstreamModelUpdateTaskDefaultIntervalMinutes  = 30
	channelUpstreamModelUpdateTaskBatchSize               = 100
	channelUpstreamModelUpdateMinCheckIntervalSeconds     = 300
	channelUpstreamModelUpdateNotifySuppressWindowSeconds = 86400
	channelUpstreamModelUpdateNotifyMaxChannelDetails     = 8
	channelUpstreamModelUpdateNotifyMaxModelDetails       = 12
	channelUpstreamModelUpdateNotifyMaxFailedChannelIDs   = 10
	channelUpstreamModelFetchDefaultTimeout               = 30 * time.Second
	channelUpstreamModelFetchMaxResponseBytes             = 10 << 20
)

var channelUpstreamModelUpdateSelectFields = []string{
	"id",
	"name",
	"type",
	"key",
	"status",
	"base_url",
	"models",
	"model_mapping",
	"settings",
	"setting",
	"other",
	"group",
	"priority",
	"weight",
	"tag",
	"channel_info",
	"header_override",
}

var (
	ErrChannelUpstreamSourceChanged = errors.New("channel upstream source changed during fetch")
	ErrChannelUpstreamStateChanged  = errors.New("channel upstream update state changed during fetch")
)

var channelUpstreamModelFetchTimeout = channelUpstreamModelFetchDefaultTimeout
var notifyUpstreamModelUpdateWatchers = service.NotifyUpstreamModelUpdateWatchers
var fetchCodexChannelModelsWithOptions = service.FetchCodexChannelModelsWithOptions
var countEnabledChannelsForUpstreamModelUpdate = func(ctx context.Context) (int64, error) {
	var totalChannels int64
	err := model.DB.WithContext(ctx).Model(&model.Channel{}).
		Where("status = ?", common.ChannelStatusEnabled).
		Count(&totalChannels).Error
	return totalChannels, err
}
var applyChannelUpstreamModelUpdatesForApplyAll = applyChannelUpstreamModelUpdatesWithContext
var applyChannelUpstreamModelUpdatesForApplyAllWithTask = applyChannelUpstreamModelUpdatesWithContextAndTask
var findEnabledChannelsAfterIDForApplyAll = findEnabledChannelsAfterIDWithContext

var channelUpstreamModelUpdateNotifyState = struct {
	sync.Mutex
	lastNotifiedAt      int64
	lastChangedChannels int
	lastFailedChannels  int
	lastSignature       string
}{}

type applyChannelUpstreamModelUpdatesRequest struct {
	ID           int      `json:"id"`
	AddModels    []string `json:"add_models"`
	RemoveModels []string `json:"remove_models"`
	IgnoreModels []string `json:"ignore_models"`
}

type applyAllChannelUpstreamModelUpdatesResult struct {
	ChannelID             int      `json:"channel_id"`
	ChannelName           string   `json:"channel_name"`
	AddedModels           []string `json:"added_models"`
	RemovedModels         []string `json:"removed_models"`
	RemainingModels       []string `json:"remaining_models"`
	RemainingRemoveModels []string `json:"remaining_remove_models"`
}

type applyAllChannelUpstreamModelUpdatesSummary struct {
	ProcessedChannels          int                                         `json:"processed_channels"`
	AddedModels                int                                         `json:"added_models"`
	RemovedModels              int                                         `json:"removed_models"`
	RemainingRemoveModelsCount int                                         `json:"remaining_remove_models_count"`
	FailedChannelIDs           []int                                       `json:"failed_channel_ids"`
	Results                    []applyAllChannelUpstreamModelUpdatesResult `json:"results"`
	RuntimeCacheRefreshError   string                                      `json:"runtime_cache_refresh_error,omitempty"`
}

type detectChannelUpstreamModelUpdatesResult struct {
	ChannelID       int      `json:"channel_id"`
	ChannelName     string   `json:"channel_name"`
	AddModels       []string `json:"add_models"`
	RemoveModels    []string `json:"remove_models"`
	LastCheckTime   int64    `json:"last_check_time"`
	AutoAddedModels int      `json:"auto_added_models"`
}

type cancelChannelUpstreamModelUpdateTaskRequest struct {
	TaskID string `json:"task_id"`
}

type upstreamModelUpdateChannelSummary struct {
	ChannelID   int
	ChannelName string
	AddCount    int
	RemoveCount int
}

func channelTypeSupportsUpstreamModelUpdate(channelType int) bool {
	switch channelType {
	case constant.ChannelTypeOpenAI,
		constant.ChannelTypeOllama,
		constant.ChannelTypeAnthropic,
		constant.ChannelTypeAli,
		constant.ChannelTypeOpenRouter,
		constant.ChannelTypeTencent,
		constant.ChannelTypeVolcEngine,
		constant.ChannelTypeGemini,
		constant.ChannelTypeMoonshot,
		constant.ChannelTypeZhipu_v4,
		constant.ChannelTypePerplexity,
		constant.ChannelTypeLingYiWanWu,
		constant.ChannelTypeCohere,
		constant.ChannelTypeMiniMax,
		constant.ChannelTypeSiliconFlow,
		constant.ChannelTypeMistral,
		constant.ChannelTypeDeepSeek,
		constant.ChannelTypeXinference,
		constant.ChannelTypeXai,
		constant.ChannelTypeCodex,
		constant.ChannelTypeAdvancedCustom,
		constant.ChannelTypeSub2API,
		constant.ChannelTypeNewAPI:
		return true
	default:
		return false
	}
}

func channelSupportsUpstreamModelUpdate(channel *model.Channel) bool {
	if channel == nil || !channelTypeSupportsUpstreamModelUpdate(channel.Type) {
		return false
	}
	if channel.ChannelInfo.IsMultiKey {
		return false
	}
	return true
}

func normalizeModelNames(models []string) []string {
	return lo.Uniq(lo.FilterMap(models, func(model string, _ int) (string, bool) {
		trimmed := strings.TrimSpace(model)
		return trimmed, trimmed != ""
	}))
}

func requireNonEmptyUpstreamModelIDs(models []string) ([]string, error) {
	normalized := normalizeModelNames(models)
	if len(normalized) == 0 {
		return nil, errors.New("upstream model response contains no valid model IDs")
	}
	return normalized, nil
}

func mergeModelNames(base []string, appended []string) []string {
	merged := normalizeModelNames(base)
	seen := make(map[string]struct{}, len(merged))
	for _, model := range merged {
		seen[model] = struct{}{}
	}
	for _, model := range normalizeModelNames(appended) {
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		merged = append(merged, model)
	}
	return merged
}

func subtractModelNames(base []string, removed []string) []string {
	removeSet := make(map[string]struct{}, len(removed))
	for _, model := range normalizeModelNames(removed) {
		removeSet[model] = struct{}{}
	}
	return lo.Filter(normalizeModelNames(base), func(model string, _ int) bool {
		_, ok := removeSet[model]
		return !ok
	})
}

func intersectModelNames(base []string, allowed []string) []string {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, model := range normalizeModelNames(allowed) {
		allowedSet[model] = struct{}{}
	}
	return lo.Filter(normalizeModelNames(base), func(model string, _ int) bool {
		_, ok := allowedSet[model]
		return ok
	})
}

func applySelectedModelChanges(originModels []string, addModels []string, removeModels []string) []string {
	// Add wins when the same model appears in both selected lists.
	normalizedAdd := normalizeModelNames(addModels)
	normalizedRemove := subtractModelNames(normalizeModelNames(removeModels), normalizedAdd)
	return subtractModelNames(mergeModelNames(originModels, normalizedAdd), normalizedRemove)
}

func normalizeChannelModelMapping(channel *model.Channel) map[string]string {
	if channel == nil || channel.ModelMapping == nil {
		return nil
	}
	rawMapping := strings.TrimSpace(*channel.ModelMapping)
	if rawMapping == "" || rawMapping == "{}" {
		return nil
	}
	parsed := make(map[string]string)
	if err := common.UnmarshalJsonStr(rawMapping, &parsed); err != nil {
		return nil
	}
	normalized := make(map[string]string, len(parsed))
	for source, target := range parsed {
		normalizedSource := strings.TrimSpace(source)
		normalizedTarget := strings.TrimSpace(target)
		if normalizedSource == "" || normalizedTarget == "" {
			continue
		}
		normalized[normalizedSource] = normalizedTarget
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func upstreamModelIsIgnored(modelName string, normalizedIgnoredModels []string) bool {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return false
	}
	return lo.ContainsBy(normalizedIgnoredModels, func(ignoredModel string) bool {
		if regexBody, ok := strings.CutPrefix(ignoredModel, "regex:"); ok {
			pattern := strings.TrimSpace(regexBody)
			if pattern == "" {
				return false
			}
			matched, err := regexp.MatchString(pattern, modelName)
			return err == nil && matched
		}
		return ignoredModel == modelName
	})
}

func collectPendingUpstreamModelChangesFromModels(
	localModels []string,
	upstreamModels []string,
	ignoredModels []string,
	modelMapping map[string]string,
) (pendingAddModels []string, pendingRemoveModels []string) {
	localSet := make(map[string]struct{})
	localModels = normalizeModelNames(localModels)
	upstreamModels = normalizeModelNames(upstreamModels)
	for _, modelName := range localModels {
		localSet[modelName] = struct{}{}
	}
	upstreamSet := make(map[string]struct{}, len(upstreamModels))
	for _, modelName := range upstreamModels {
		upstreamSet[modelName] = struct{}{}
	}

	normalizedIgnoredModels := normalizeModelNames(ignoredModels)

	redirectSourceSet := make(map[string]struct{}, len(modelMapping))
	redirectTargetSet := make(map[string]struct{}, len(modelMapping))
	for source, target := range modelMapping {
		redirectSourceSet[source] = struct{}{}
		redirectTargetSet[target] = struct{}{}
	}

	coveredUpstreamSet := make(map[string]struct{}, len(localSet)+len(redirectTargetSet))
	for modelName := range localSet {
		coveredUpstreamSet[modelName] = struct{}{}
	}
	for modelName := range redirectTargetSet {
		coveredUpstreamSet[modelName] = struct{}{}
	}

	pendingAdd := lo.Filter(upstreamModels, func(modelName string, _ int) bool {
		if _, ok := coveredUpstreamSet[modelName]; ok {
			return false
		}
		if upstreamModelIsIgnored(modelName, normalizedIgnoredModels) {
			return false
		}
		return true
	})
	pendingRemove := lo.Filter(localModels, func(modelName string, _ int) bool {
		// Redirect source models are virtual aliases and should not be removed
		// only because they are absent from upstream model list.
		if _, ok := redirectSourceSet[modelName]; ok {
			return false
		}
		if upstreamModelIsIgnored(modelName, normalizedIgnoredModels) {
			return false
		}
		_, ok := upstreamSet[modelName]
		return !ok
	})
	return normalizeModelNames(pendingAdd), normalizeModelNames(pendingRemove)
}

func getUpstreamModelUpdateMinCheckIntervalSeconds() int64 {
	interval := int64(common.GetEnvOrDefault(
		"CHANNEL_UPSTREAM_MODEL_UPDATE_MIN_CHECK_INTERVAL_SECONDS",
		channelUpstreamModelUpdateMinCheckIntervalSeconds,
	))
	if interval < 0 {
		return channelUpstreamModelUpdateMinCheckIntervalSeconds
	}
	return interval
}

func parseOpenAIModelIDs(body []byte) ([]string, error) {
	var result struct {
		Data *[]OpenAIModel `json:"data"`
	}
	if err := common.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("invalid OpenAI Models response: %w", err)
	}
	if result.Data == nil {
		return nil, fmt.Errorf("invalid OpenAI Models response: data is required")
	}
	ids := normalizeModelNames(lo.Map(*result.Data, func(item OpenAIModel, _ int) string {
		return item.ID
	}))
	if len(ids) == 0 {
		return nil, fmt.Errorf("OpenAI Models response contains no valid model IDs")
	}
	return ids, nil
}

func sanitizeFetchModelsError(err error, key string) error {
	if err == nil {
		return nil
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		err = urlErr.Err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	message := err.Error()
	if bodyOffset := strings.Index(message, ", body:"); bodyOffset >= 0 {
		message = message[:bodyOffset]
	}
	key = strings.TrimSpace(key)
	if key != "" {
		message = strings.ReplaceAll(message, key, "[REDACTED]")
		message = strings.ReplaceAll(message, url.QueryEscape(key), "[REDACTED]")
		message = strings.ReplaceAll(message, url.PathEscape(key), "[REDACTED]")
	}
	return errors.New(message)
}

func sanitizeAdvancedCustomRequestError(err error, key string, requestURL string) error {
	err = sanitizeFetchModelsError(err, key)
	if err == nil {
		return nil
	}
	parsedURL, parseErr := url.Parse(requestURL)
	if parseErr != nil {
		return err
	}
	message := err.Error()
	for _, value := range parsedURL.Query() {
		for _, secret := range value {
			if secret == "" {
				continue
			}
			message = strings.ReplaceAll(message, secret, "[REDACTED]")
			message = strings.ReplaceAll(message, url.QueryEscape(secret), "[REDACTED]")
			message = strings.ReplaceAll(message, url.PathEscape(secret), "[REDACTED]")
		}
	}
	if key != "" {
		message = strings.ReplaceAll(message, key, "[REDACTED]")
		message = strings.ReplaceAll(message, url.QueryEscape(key), "[REDACTED]")
		message = strings.ReplaceAll(message, url.PathEscape(key), "[REDACTED]")
	}
	return errors.New(message)
}

func getFetchModelsResponseBodyWithContext(ctx context.Context, method string, requestURL string, channel *model.Channel, headers http.Header) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	request, err := http.NewRequestWithContext(ctx, method, requestURL, nil)
	if err != nil {
		return nil, err
	}
	for name, values := range headers {
		for _, value := range values {
			if strings.EqualFold(name, "Host") {
				request.Host = value
				continue
			}
			request.Header.Add(name, value)
		}
	}
	client, err := newChannelUpstreamModelFetchHTTPClient(channel)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status code: %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, channelUpstreamModelFetchMaxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > channelUpstreamModelFetchMaxResponseBytes {
		return nil, fmt.Errorf("response body exceeds %d bytes", channelUpstreamModelFetchMaxResponseBytes)
	}
	return body, nil
}

func newChannelUpstreamModelFetchHTTPClient(channel *model.Channel) (*http.Client, error) {
	if channel == nil {
		return nil, errors.New("channel is nil")
	}
	settings := channel.GetSetting()
	client, err := service.GetHttpClientWithProxySettings(settings.Proxy, settings)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("upstream model HTTP client is nil")
	}
	return service.CloneHTTPClientWithoutRedirects(client), nil
}

type channelUpstreamModelFetchOptions struct {
	AllowCodexCredentialRefresh bool
}

type channelUpstreamModelUpdateCheckOptions struct {
	AllowCodexCredentialRefresh bool
}

var defaultChannelUpstreamModelUpdateCheckOptions = channelUpstreamModelUpdateCheckOptions{
	AllowCodexCredentialRefresh: true,
}

func fetchChannelUpstreamModelIDs(ctx context.Context, channel *model.Channel) ([]string, error) {
	return fetchChannelUpstreamModelIDsWithOptions(ctx, channel, channelUpstreamModelFetchOptions{
		AllowCodexCredentialRefresh: true,
	})
}

func fetchChannelUpstreamModelIDsWithOptions(ctx context.Context, channel *model.Channel, options channelUpstreamModelFetchOptions) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if channelUpstreamModelFetchTimeout > 0 {
		fetchCtx, cancel := context.WithTimeout(ctx, channelUpstreamModelFetchTimeout)
		defer cancel()
		ctx = fetchCtx
	}
	if channel == nil {
		return nil, errors.New("channel is nil")
	}
	if channel.Type <= constant.ChannelTypeUnknown || channel.Type >= len(constant.ChannelBaseURLs) {
		return nil, fmt.Errorf("unsupported channel type: %d", channel.Type)
	}
	if !channelTypeSupportsUpstreamModelUpdate(channel.Type) {
		return nil, fmt.Errorf("channel type %d does not support upstream model updates", channel.Type)
	}
	if channel.ChannelInfo.IsMultiKey {
		return nil, errors.New("multi-key channels do not support upstream model updates")
	}
	baseURL := constant.ChannelBaseURLs[channel.Type]
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if (channel.Type == constant.ChannelTypeSub2API || channel.Type == constant.ChannelTypeNewAPI) && baseURL == "" {
		return nil, fmt.Errorf("%s channel base URL cannot be empty", constant.GetChannelTypeName(channel.Type))
	}

	if channel.Type == constant.ChannelTypeOllama {
		key, _, apiErr := channel.GetNextEnabledKey()
		if apiErr != nil {
			return nil, fmt.Errorf("获取渠道密钥失败: %w", apiErr)
		}
		key = strings.TrimSpace(key)
		client, err := newChannelUpstreamModelFetchHTTPClient(channel)
		if err != nil {
			return nil, sanitizeFetchModelsError(err, key)
		}
		headers, err := buildFetchModelsHeaderOverrides(channel, key)
		if err != nil {
			return nil, sanitizeFetchModelsError(err, key)
		}
		models, err := ollama.FetchOllamaModelsWithContextAndClientAndHeaders(
			ctx,
			baseURL,
			key,
			client,
			headers,
		)
		if err != nil {
			return nil, sanitizeFetchModelsError(err, key)
		}
		return requireNonEmptyUpstreamModelIDs(lo.Map(models, func(item ollama.OllamaModel, _ int) string {
			return item.Name
		}))
	}

	if channel.Type == constant.ChannelTypeGemini {
		key, _, apiErr := channel.GetNextEnabledKey()
		if apiErr != nil {
			return nil, fmt.Errorf("获取渠道密钥失败: %w", apiErr)
		}
		key = strings.TrimSpace(key)
		client, err := newChannelUpstreamModelFetchHTTPClient(channel)
		if err != nil {
			return nil, sanitizeFetchModelsError(err, key)
		}
		headers, err := buildFetchModelsHeaderOverrides(channel, key)
		if err != nil {
			return nil, sanitizeFetchModelsError(err, key)
		}
		models, err := gemini.FetchGeminiModelsWithContextAndClientAndHeaders(
			ctx,
			baseURL,
			key,
			client,
			headers,
		)
		if err != nil {
			return nil, sanitizeFetchModelsError(err, key)
		}
		return requireNonEmptyUpstreamModelIDs(models)
	}

	if channel.Type == constant.ChannelTypeAdvancedCustom {
		return fetchAdvancedCustomUpstreamModelIDs(ctx, channel, baseURL)
	}

	if channel.Type == constant.ChannelTypeCodex {
		key, _, apiErr := channel.GetNextEnabledKey()
		if apiErr != nil {
			return nil, fmt.Errorf("获取渠道密钥失败: %w", apiErr)
		}
		key = strings.TrimSpace(key)
		headers, err := buildFetchModelsHeaderOverrides(channel, key)
		if err != nil {
			return nil, sanitizeFetchModelsError(err, key)
		}
		models, err := fetchCodexChannelModelsWithOptions(ctx, channel, service.CodexChannelModelFetchOptions{
			AllowCredentialRefresh: options.AllowCodexCredentialRefresh,
			Headers:                headers,
			BuildHeaders:           buildFetchModelsHeaderOverrides,
		})
		if err != nil {
			var cacheFetchErr *service.CodexChannelModelFetchError
			if errors.As(err, &cacheFetchErr) {
				models, modelErr := requireNonEmptyUpstreamModelIDs(cacheFetchErr.Models)
				if modelErr != nil {
					return nil, modelErr
				}
				return models, err
			}
			return nil, err
		}
		return requireNonEmptyUpstreamModelIDs(models)
	}

	var url string
	switch channel.Type {
	case constant.ChannelTypeAli:
		url = fmt.Sprintf("%s/compatible-mode/v1/models", baseURL)
	case constant.ChannelTypeZhipu_v4:
		if plan, ok := constant.ChannelSpecialBases[baseURL]; ok && plan.OpenAIBaseURL != "" {
			url = fmt.Sprintf("%s/models", plan.OpenAIBaseURL)
		} else {
			url = fmt.Sprintf("%s/api/paas/v4/models", baseURL)
		}
	case constant.ChannelTypeVolcEngine:
		if plan, ok := constant.ChannelSpecialBases[baseURL]; ok && plan.OpenAIBaseURL != "" {
			url = fmt.Sprintf("%s/v1/models", plan.OpenAIBaseURL)
		} else {
			url = fmt.Sprintf("%s/v1/models", baseURL)
		}
	case constant.ChannelTypeMoonshot:
		if plan, ok := constant.ChannelSpecialBases[baseURL]; ok && plan.OpenAIBaseURL != "" {
			url = fmt.Sprintf("%s/models", plan.OpenAIBaseURL)
		} else {
			url = fmt.Sprintf("%s/v1/models", baseURL)
		}
	default:
		url = fmt.Sprintf("%s/v1/models", baseURL)
	}

	key, _, apiErr := channel.GetNextEnabledKey()
	if apiErr != nil {
		return nil, fmt.Errorf("获取渠道密钥失败: %w", apiErr)
	}
	key = strings.TrimSpace(key)

	headers, err := buildFetchModelsHeaders(channel, key)
	if err != nil {
		return nil, err
	}

	body, err := getFetchModelsResponseBodyWithContext(ctx, http.MethodGet, url, channel, headers)
	if err != nil {
		return nil, sanitizeAdvancedCustomRequestError(err, key, url)
	}

	return parseOpenAIModelIDs(body)
}

func fetchAdvancedCustomUpstreamModelIDs(ctx context.Context, channel *model.Channel, baseURL string) ([]string, error) {
	key, _, apiErr := channel.GetNextEnabledKey()
	if apiErr != nil {
		return nil, fmt.Errorf("获取渠道密钥失败: %w", apiErr)
	}
	key = strings.TrimSpace(key)

	info := &relaycommon.RelayInfo{
		RelayFormat:    types.RelayFormatOpenAI,
		RelayMode:      relayconstant.RelayModeUnknown,
		RequestURLPath: dto.AdvancedCustomModelListPath,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:          constant.ChannelTypeAdvancedCustom,
			ChannelBaseUrl:       baseURL,
			ApiKey:               key,
			ChannelOtherSettings: channel.GetOtherSettings(),
		},
	}

	adaptor := &advancedcustom.Adaptor{}
	requestURL, headers, err := adaptor.BuildModelListRequest(info)
	if err != nil {
		return nil, sanitizeFetchModelsError(err, key)
	}
	if err := applyFetchModelsHeaderOverrides(channel, key, headers); err != nil {
		return nil, sanitizeFetchModelsError(err, key)
	}

	body, err := getFetchModelsResponseBodyWithContext(ctx, http.MethodGet, requestURL, channel, headers)
	if err != nil {
		return nil, sanitizeAdvancedCustomRequestError(err, key, requestURL)
	}
	return parseOpenAIModelIDs(body)
}

func updateChannelUpstreamModelSettingsWithTx(tx *gorm.DB, channel *model.Channel, settings dto.ChannelOtherSettings, updateModels bool) error {
	if tx == nil {
		tx = model.DB
	}
	settingsRaw, err := mergeChannelUpstreamModelUpdateSettings(channel.OtherSettings, settings)
	if err != nil {
		return err
	}
	channel.OtherSettings = settingsRaw
	updates := map[string]interface{}{
		"settings": channel.OtherSettings,
	}
	if updateModels {
		updates["models"] = channel.Models
	}
	return tx.Model(&model.Channel{}).Where("id = ?", channel.Id).Updates(updates).Error
}

func mergeChannelUpstreamModelUpdateSettings(raw string, settings dto.ChannelOtherSettings) (string, error) {
	settingsMap, ok := channelSettingsMapForUpdate(raw)
	if !ok {
		return "", fmt.Errorf("channel settings is not valid JSON")
	}
	applyChannelUpstreamModelUpdateSettingsToMap(settingsMap, settings)
	settingsBytes, err := common.Marshal(settingsMap)
	if err != nil {
		return "", err
	}
	return string(settingsBytes), nil
}

func applyChannelUpstreamModelUpdateSettingsToMap(settingsMap map[string]any, settings dto.ChannelOtherSettings) {
	if !channelUpstreamModelUpdateSettingsHasValues(settings) {
		removeChannelUpstreamModelUpdateFields(settingsMap)
		return
	}

	if settings.UpstreamModelUpdateCheckEnabled {
		settingsMap["upstream_model_update_check_enabled"] = true
		if settings.UpstreamModelUpdateAutoSyncEnabled {
			settingsMap["upstream_model_update_auto_sync_enabled"] = true
		} else {
			delete(settingsMap, "upstream_model_update_auto_sync_enabled")
		}
	} else {
		delete(settingsMap, "upstream_model_update_check_enabled")
		delete(settingsMap, "upstream_model_update_auto_sync_enabled")
	}

	if ignoredModels := normalizeModelNames(settings.UpstreamModelUpdateIgnoredModels); len(ignoredModels) > 0 {
		settingsMap["upstream_model_update_ignored_models"] = ignoredModels
	} else {
		delete(settingsMap, "upstream_model_update_ignored_models")
	}
	if settings.UpstreamModelUpdateLastCheckTime > 0 {
		settingsMap["upstream_model_update_last_check_time"] = settings.UpstreamModelUpdateLastCheckTime
	} else {
		delete(settingsMap, "upstream_model_update_last_check_time")
	}
	if detectedModels := normalizeModelNames(settings.UpstreamModelUpdateLastDetectedModels); len(detectedModels) > 0 {
		settingsMap["upstream_model_update_last_detected_models"] = detectedModels
	} else {
		delete(settingsMap, "upstream_model_update_last_detected_models")
	}
	if removedModels := normalizeModelNames(settings.UpstreamModelUpdateLastRemovedModels); len(removedModels) > 0 {
		settingsMap["upstream_model_update_last_removed_models"] = removedModels
	} else {
		delete(settingsMap, "upstream_model_update_last_removed_models")
	}
}

func channelUpstreamModelUpdateSettingsHasValues(settings dto.ChannelOtherSettings) bool {
	return settings.UpstreamModelUpdateCheckEnabled ||
		settings.UpstreamModelUpdateAutoSyncEnabled ||
		len(settings.UpstreamModelUpdateIgnoredModels) > 0 ||
		settings.UpstreamModelUpdateLastCheckTime > 0 ||
		len(settings.UpstreamModelUpdateLastDetectedModels) > 0 ||
		len(settings.UpstreamModelUpdateLastRemovedModels) > 0
}

func withLockedChannelUpstreamModelUpdate(
	channelID int,
	handler func(tx *gorm.DB, channel *model.Channel) error,
) error {
	return withLockedChannelUpstreamModelUpdateContext(
		context.Background(),
		channelID,
		"",
		"",
		handler,
	)
}

func withLockedChannelUpstreamModelUpdateContext(
	ctx context.Context,
	channelID int,
	taskID string,
	runnerID string,
	handler func(tx *gorm.DB, channel *model.Channel) error,
) error {
	if channelID <= 0 {
		return errors.New("渠道 ID 无效")
	}
	if handler == nil {
		return errors.New("channel update handler is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return model.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := model.LockSystemTaskForExecution(tx, taskID, runnerID); err != nil {
			return err
		}
		channel := &model.Channel{}
		if err := model.LockForUpdate(tx).Where("id = ?", channelID).First(channel).Error; err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return handler(tx, channel)
	})
}

func sameChannelUpstreamModelSource(left *model.Channel, right *model.Channel) bool {
	if left == nil || right == nil {
		return false
	}
	return left.Id == right.Id &&
		left.Type == right.Type &&
		sameChannelUpstreamModelSourceKey(left, right) &&
		left.GetBaseURL() == right.GetBaseURL() &&
		left.Other == right.Other &&
		sameChannelUpstreamModelSourceSettings(left.OtherSettings, right.OtherSettings) &&
		stringPointerValue(left.ModelMapping) == stringPointerValue(right.ModelMapping) &&
		stringPointerValue(left.Setting) == stringPointerValue(right.Setting) &&
		stringPointerValue(left.HeaderOverride) == stringPointerValue(right.HeaderOverride) &&
		left.ChannelInfo.IsMultiKey == right.ChannelInfo.IsMultiKey &&
		left.ChannelInfo.MultiKeySize == right.ChannelInfo.MultiKeySize &&
		left.ChannelInfo.MultiKeyMode == right.ChannelInfo.MultiKeyMode &&
		reflect.DeepEqual(left.ChannelInfo.MultiKeyStatusList, right.ChannelInfo.MultiKeyStatusList)
}

func sameChannelUpstreamModelSourceKey(left *model.Channel, right *model.Channel) bool {
	if left.Type != constant.ChannelTypeCodex || right.Type != constant.ChannelTypeCodex {
		return left.Key == right.Key
	}
	return reflect.DeepEqual(
		codexStableCredentialSource(left.Key),
		codexStableCredentialSource(right.Key),
	)
}

func codexStableCredentialSource(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parsed := map[string]any{}
	if err := common.UnmarshalJsonStr(raw, &parsed); err != nil {
		return map[string]any{"__raw": raw}
	}
	delete(parsed, "access_token")
	delete(parsed, "refresh_token")
	delete(parsed, "expired")
	delete(parsed, "last_refresh")
	if len(parsed) == 0 {
		return nil
	}
	return parsed
}

func sameChannelUpstreamModelSourceSettings(left string, right string) bool {
	return reflect.DeepEqual(
		channelUpstreamModelSourceSettings(left),
		channelUpstreamModelSourceSettings(right),
	)
}

func channelUpstreamModelSourceSettings(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parsed := map[string]any{}
	if err := common.UnmarshalJsonStr(raw, &parsed); err != nil {
		return map[string]any{"__raw": raw}
	}
	delete(parsed, "upstream_model_update_last_check_time")
	delete(parsed, "upstream_model_update_last_detected_models")
	delete(parsed, "upstream_model_update_last_removed_models")
	if len(parsed) == 0 {
		return nil
	}
	return parsed
}

func clearUnsupportedChannelUpstreamModelUpdateSettings(settings *dto.ChannelOtherSettings) bool {
	if settings == nil {
		return false
	}
	changed := settings.UpstreamModelUpdateCheckEnabled ||
		settings.UpstreamModelUpdateAutoSyncEnabled ||
		len(settings.UpstreamModelUpdateIgnoredModels) > 0 ||
		settings.UpstreamModelUpdateLastCheckTime != 0 ||
		len(settings.UpstreamModelUpdateLastDetectedModels) > 0 ||
		len(settings.UpstreamModelUpdateLastRemovedModels) > 0
	if !changed {
		return false
	}
	settings.UpstreamModelUpdateCheckEnabled = false
	settings.UpstreamModelUpdateAutoSyncEnabled = false
	settings.UpstreamModelUpdateIgnoredModels = nil
	settings.UpstreamModelUpdateLastCheckTime = 0
	settings.UpstreamModelUpdateLastDetectedModels = nil
	settings.UpstreamModelUpdateLastRemovedModels = nil
	return true
}

func cleanupUnsupportedChannelUpstreamModelUpdateSettings(channelID int) (bool, error) {
	return cleanupUnsupportedChannelUpstreamModelUpdateSettingsWithContext(
		context.Background(),
		channelID,
		"",
		"",
	)
}

func cleanupUnsupportedChannelUpstreamModelUpdateSettingsWithContext(
	ctx context.Context,
	channelID int,
	taskID string,
	runnerID string,
) (bool, error) {
	cleaned := false
	err := withLockedChannelUpstreamModelUpdateContext(ctx, channelID, taskID, runnerID, func(tx *gorm.DB, lockedChannel *model.Channel) error {
		if channelSupportsUpstreamModelUpdate(lockedChannel) {
			return nil
		}
		hasResidualKeys := channelSettingsContainUpstreamModelUpdateFields(lockedChannel.OtherSettings)
		lockedSettings := lockedChannel.GetOtherSettings()
		if !clearUnsupportedChannelUpstreamModelUpdateSettings(&lockedSettings) && !hasResidualKeys {
			return nil
		}
		if err := updateChannelUpstreamModelSettingsWithTx(tx, lockedChannel, lockedSettings, false); err != nil {
			return err
		}
		cleaned = true
		return nil
	})
	return cleaned, err
}

func stringPointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func persistChannelUpstreamModelSettingsAndAbilitiesWithTx(
	tx *gorm.DB,
	channel *model.Channel,
	settings dto.ChannelOtherSettings,
	modelsChanged bool,
	removeModels []string,
) error {
	if err := updateChannelUpstreamModelSettingsWithTx(tx, channel, settings, modelsChanged); err != nil {
		return err
	}
	if modelsChanged {
		if err := syncChannelUpstreamModelAbilitiesWithTx(tx, channel, removeModels); err != nil {
			return err
		}
	}
	return nil
}

func syncChannelUpstreamModelAbilitiesWithTx(tx *gorm.DB, channel *model.Channel, removeModels []string) error {
	if tx == nil {
		tx = model.DB
	}
	normalizedRemoveModels := normalizeModelNames(removeModels)
	if len(normalizedRemoveModels) > 0 {
		query := tx.Where("channel_id = ? AND model IN ?", channel.Id, normalizedRemoveModels)
		if groups := common.SplitCommaSeparated(channel.Group); len(groups) > 0 {
			query = query.Where(map[string]interface{}{"group": groups})
		}
		if err := query.Delete(&model.Ability{}).Error; err != nil {
			return err
		}
	}
	return channel.AddAbilities(tx)
}

func recheckChannelUpstreamModelUpdateBeforeFetch(
	ctx context.Context,
	channel *model.Channel,
	settings *dto.ChannelOtherSettings,
	force bool,
	requireEnabled bool,
	now int64,
	taskID string,
	runnerID string,
) (skipFetch bool, err error) {
	err = withLockedChannelUpstreamModelUpdateContext(ctx, channel.Id, taskID, runnerID, func(tx *gorm.DB, lockedChannel *model.Channel) error {
		if requireEnabled && lockedChannel.Status != common.ChannelStatusEnabled {
			*settings = lockedChannel.GetOtherSettings()
			*channel = *lockedChannel
			skipFetch = true
			return nil
		}
		if !channelSupportsUpstreamModelUpdate(lockedChannel) {
			hasResidualKeys := channelSettingsContainUpstreamModelUpdateFields(lockedChannel.OtherSettings)
			lockedSettings := lockedChannel.GetOtherSettings()
			if clearUnsupportedChannelUpstreamModelUpdateSettings(&lockedSettings) || hasResidualKeys {
				if err := updateChannelUpstreamModelSettingsWithTx(tx, lockedChannel, lockedSettings, false); err != nil {
					return err
				}
			}
			*settings = lockedSettings
			*channel = *lockedChannel
			skipFetch = true
			return nil
		}
		if !sameChannelUpstreamModelSource(channel, lockedChannel) {
			return ErrChannelUpstreamSourceChanged
		}
		lockedSettings := lockedChannel.GetOtherSettings()
		if !lockedSettings.UpstreamModelUpdateCheckEnabled {
			*settings = lockedSettings
			*channel = *lockedChannel
			skipFetch = true
			return errors.New("upstream model update check is not enabled")
		}
		if !force {
			minInterval := getUpstreamModelUpdateMinCheckIntervalSeconds()
			if lockedSettings.UpstreamModelUpdateLastCheckTime > 0 &&
				now-lockedSettings.UpstreamModelUpdateLastCheckTime < minInterval {
				*settings = lockedSettings
				*channel = *lockedChannel
				skipFetch = true
				return nil
			}
		}
		*settings = lockedSettings
		*channel = *lockedChannel
		return nil
	})
	return skipFetch, err
}

func checkAndPersistChannelUpstreamModelUpdates(
	ctx context.Context,
	channel *model.Channel,
	settings *dto.ChannelOtherSettings,
	force bool,
	allowAutoApply bool,
	requireEnabled bool,
) (modelsChanged bool, autoAddedModels []string, err error) {
	return checkAndPersistChannelUpstreamModelUpdatesWithOptions(
		ctx,
		channel,
		settings,
		force,
		allowAutoApply,
		requireEnabled,
		defaultChannelUpstreamModelUpdateCheckOptions,
		"",
		"",
	)
}

func checkAndPersistChannelUpstreamModelUpdatesWithOptions(
	ctx context.Context,
	channel *model.Channel,
	settings *dto.ChannelOtherSettings,
	force bool,
	allowAutoApply bool,
	requireEnabled bool,
	options channelUpstreamModelUpdateCheckOptions,
	taskID string,
	runnerID string,
) (modelsChanged bool, autoAddedModels []string, err error) {
	if channel == nil || channel.Id <= 0 {
		return false, nil, errors.New("invalid channel")
	}
	if settings == nil {
		return false, nil, errors.New("channel update settings are required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return false, nil, err
	}
	if !channelSupportsUpstreamModelUpdate(channel) {
		err = withLockedChannelUpstreamModelUpdateContext(ctx, channel.Id, taskID, runnerID, func(tx *gorm.DB, lockedChannel *model.Channel) error {
			if err := ctx.Err(); err != nil {
				*settings = lockedChannel.GetOtherSettings()
				*channel = *lockedChannel
				return err
			}
			if channelSupportsUpstreamModelUpdate(lockedChannel) {
				*settings = lockedChannel.GetOtherSettings()
				*channel = *lockedChannel
				return nil
			}
			hasResidualKeys := channelSettingsContainUpstreamModelUpdateFields(lockedChannel.OtherSettings)
			lockedSettings := lockedChannel.GetOtherSettings()
			changed := clearUnsupportedChannelUpstreamModelUpdateSettings(&lockedSettings)
			if changed || hasResidualKeys {
				if err := updateChannelUpstreamModelSettingsWithTx(tx, lockedChannel, lockedSettings, false); err != nil {
					return err
				}
			}
			*settings = lockedSettings
			*channel = *lockedChannel
			return nil
		})
		return false, nil, err
	}
	if !settings.UpstreamModelUpdateCheckEnabled {
		return false, nil, errors.New("upstream model update check is not enabled")
	}
	now := common.GetTimestamp()
	if !force {
		minInterval := getUpstreamModelUpdateMinCheckIntervalSeconds()
		if settings.UpstreamModelUpdateLastCheckTime > 0 &&
			now-settings.UpstreamModelUpdateLastCheckTime < minInterval {
			return false, nil, nil
		}
	}

	skipFetch, err := recheckChannelUpstreamModelUpdateBeforeFetch(ctx, channel, settings, force, requireEnabled, now, taskID, runnerID)
	if err != nil || skipFetch {
		return false, nil, err
	}
	originalCodexKey := ""
	if channel.Type == constant.ChannelTypeCodex {
		originalCodexKey = channel.Key
	}
	fetchCtx := ctx
	var cancelFetch context.CancelFunc
	if channelUpstreamModelFetchTimeout > 0 {
		fetchCtx, cancelFetch = context.WithTimeout(ctx, channelUpstreamModelFetchTimeout)
		defer cancelFetch()
	}
	upstreamModels, fetchErr := fetchChannelUpstreamModelIDsWithOptions(fetchCtx, channel, channelUpstreamModelFetchOptions{
		AllowCodexCredentialRefresh: options.AllowCodexCredentialRefresh,
	})
	fetchErr = refreshRuntimeCacheAfterCodexCredentialChange(channel, originalCodexKey, fetchErr)
	if ctx != nil && ctx.Err() != nil {
		return false, nil, ctx.Err()
	}

	var fetchResultErr error
	err = withLockedChannelUpstreamModelUpdateContext(ctx, channel.Id, taskID, runnerID, func(tx *gorm.DB, lockedChannel *model.Channel) error {
		if err := ctx.Err(); err != nil {
			*settings = lockedChannel.GetOtherSettings()
			*channel = *lockedChannel
			return err
		}
		if requireEnabled && lockedChannel.Status != common.ChannelStatusEnabled {
			*settings = lockedChannel.GetOtherSettings()
			*channel = *lockedChannel
			return nil
		}
		if !channelSupportsUpstreamModelUpdate(lockedChannel) {
			hasResidualKeys := channelSettingsContainUpstreamModelUpdateFields(lockedChannel.OtherSettings)
			lockedSettings := lockedChannel.GetOtherSettings()
			if clearUnsupportedChannelUpstreamModelUpdateSettings(&lockedSettings) || hasResidualKeys {
				if err := updateChannelUpstreamModelSettingsWithTx(tx, lockedChannel, lockedSettings, false); err != nil {
					return err
				}
			}
			*settings = lockedSettings
			*channel = *lockedChannel
			return nil
		}
		if !sameChannelUpstreamModelSource(channel, lockedChannel) {
			return ErrChannelUpstreamSourceChanged
		}
		lockedSettings := lockedChannel.GetOtherSettings()
		if !lockedSettings.UpstreamModelUpdateCheckEnabled {
			*settings = lockedSettings
			*channel = *lockedChannel
			return errors.New("upstream model update check is not enabled")
		}
		if !force {
			minInterval := getUpstreamModelUpdateMinCheckIntervalSeconds()
			if lockedSettings.UpstreamModelUpdateLastCheckTime > 0 &&
				now-lockedSettings.UpstreamModelUpdateLastCheckTime < minInterval {
				*settings = lockedSettings
				*channel = *lockedChannel
				return nil
			}
		}

		if fetchErr != nil {
			*settings = lockedSettings
			*channel = *lockedChannel
			fetchResultErr = fetchErr
			return nil
		}
		if !sameChannelUpstreamModelUpdateRuntimeState(*settings, lockedSettings) {
			*settings = lockedSettings
			*channel = *lockedChannel
			return ErrChannelUpstreamStateChanged
		}

		lockedSettings.UpstreamModelUpdateLastCheckTime = now
		pendingAddModels, pendingRemoveModels := collectPendingUpstreamModelChangesFromModels(
			lockedChannel.GetModels(),
			upstreamModels,
			lockedSettings.UpstreamModelUpdateIgnoredModels,
			normalizeChannelModelMapping(lockedChannel),
		)
		if allowAutoApply && lockedSettings.UpstreamModelUpdateAutoSyncEnabled && len(pendingAddModels) > 0 {
			originModels := normalizeModelNames(lockedChannel.GetModels())
			autoAddedModels = subtractModelNames(pendingAddModels, originModels)
			mergedModels := mergeModelNames(originModels, autoAddedModels)
			if len(mergedModels) > len(originModels) {
				lockedChannel.Models = strings.Join(mergedModels, ",")
				modelsChanged = true
			}
			lockedSettings.UpstreamModelUpdateLastDetectedModels = []string{}
		} else {
			lockedSettings.UpstreamModelUpdateLastDetectedModels = pendingAddModels
		}
		lockedSettings.UpstreamModelUpdateLastRemovedModels = pendingRemoveModels

		if err := persistChannelUpstreamModelSettingsAndAbilitiesWithTx(tx, lockedChannel, lockedSettings, modelsChanged, nil); err != nil {
			return err
		}
		*settings = lockedSettings
		*channel = *lockedChannel
		return nil
	})
	if err != nil {
		return false, autoAddedModels, err
	}
	if fetchResultErr != nil {
		return false, nil, fetchResultErr
	}
	return modelsChanged, autoAddedModels, nil
}

func sameChannelUpstreamModelUpdateRuntimeState(
	left dto.ChannelOtherSettings,
	right dto.ChannelOtherSettings,
) bool {
	return left.UpstreamModelUpdateLastCheckTime == right.UpstreamModelUpdateLastCheckTime &&
		slices.Equal(
			normalizeModelNames(left.UpstreamModelUpdateLastDetectedModels),
			normalizeModelNames(right.UpstreamModelUpdateLastDetectedModels),
		) &&
		slices.Equal(
			normalizeModelNames(left.UpstreamModelUpdateLastRemovedModels),
			normalizeModelNames(right.UpstreamModelUpdateLastRemovedModels),
		)
}

func refreshChannelRuntimeCache() error {
	var cacheErr error
	if common.MemoryCacheEnabled {
		for attempt := 0; attempt < 3; attempt++ {
			func() {
				defer func() {
					if r := recover(); r != nil {
						cacheErr = fmt.Errorf("InitChannelCache panic: %v", r)
					}
				}()
				cacheErr = model.InitChannelCacheWithError()
			}()
			if cacheErr == nil {
				break
			}
			if attempt < 2 {
				time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
			}
		}
		if cacheErr != nil {
			common.SysLog(fmt.Sprintf("InitChannelCache failed: %v", cacheErr))
		}
	}
	service.ResetProxyClientCache()
	return cacheErr
}

func refreshRuntimeCacheAfterCodexCredentialChange(
	channel *model.Channel,
	originalKey string,
	fetchErr error,
) error {
	if channel == nil || channel.Type != constant.ChannelTypeCodex {
		return fetchErr
	}
	var cacheFetchErr *service.CodexChannelModelFetchError
	if errors.As(fetchErr, &cacheFetchErr) {
		if cacheErr := refreshChannelRuntimeCache(); cacheErr == nil {
			return nil
		} else {
			return fmt.Errorf("%w; runtime channel cache refresh failed: %v", fetchErr, cacheErr)
		}
	}
	if channel.Key == originalKey {
		return fetchErr
	}
	cacheErr := refreshChannelRuntimeCache()
	if cacheErr == nil {
		return fetchErr
	}
	if fetchErr == nil {
		return fmt.Errorf("models fetched but runtime channel cache refresh failed: %w", cacheErr)
	}
	return fmt.Errorf("%w; runtime channel cache refresh failed: %v", fetchErr, cacheErr)
}

func upstreamModelUpdateNotificationSignature(
	changedChannels int,
	failedChannels int,
	failedChannelIDs []int,
	channelSummaries []upstreamModelUpdateChannelSummary,
	addModelSamples []string,
	removeModelSamples []string,
) string {
	normalizedFailedIDs := append([]int(nil), failedChannelIDs...)
	slices.Sort(normalizedFailedIDs)

	summaryParts := make([]string, 0, len(channelSummaries))
	for _, summary := range channelSummaries {
		summaryParts = append(summaryParts, fmt.Sprintf("%d:%s:%d:%d", summary.ChannelID, summary.ChannelName, summary.AddCount, summary.RemoveCount))
	}
	slices.Sort(summaryParts)

	normalizedAddSamples := normalizeModelNames(addModelSamples)
	slices.Sort(normalizedAddSamples)
	normalizedRemoveSamples := normalizeModelNames(removeModelSamples)
	slices.Sort(normalizedRemoveSamples)

	return fmt.Sprintf(
		"changed=%d;failed=%d;failed_ids=%v;summaries=%v;add=%v;remove=%v",
		changedChannels,
		failedChannels,
		normalizedFailedIDs,
		summaryParts,
		normalizedAddSamples,
		normalizedRemoveSamples,
	)
}

func shouldSendUpstreamModelUpdateNotification(
	now int64,
	changedChannels int,
	failedChannels int,
	failedChannelIDs []int,
	channelSummaries []upstreamModelUpdateChannelSummary,
	addModelSamples []string,
	removeModelSamples []string,
) bool {
	if changedChannels <= 0 && failedChannels <= 0 {
		return true
	}
	signature := upstreamModelUpdateNotificationSignature(
		changedChannels,
		failedChannels,
		failedChannelIDs,
		channelSummaries,
		addModelSamples,
		removeModelSamples,
	)

	channelUpstreamModelUpdateNotifyState.Lock()
	defer channelUpstreamModelUpdateNotifyState.Unlock()

	if channelUpstreamModelUpdateNotifyState.lastNotifiedAt > 0 &&
		now-channelUpstreamModelUpdateNotifyState.lastNotifiedAt < channelUpstreamModelUpdateNotifySuppressWindowSeconds &&
		channelUpstreamModelUpdateNotifyState.lastChangedChannels == changedChannels &&
		channelUpstreamModelUpdateNotifyState.lastFailedChannels == failedChannels &&
		channelUpstreamModelUpdateNotifyState.lastSignature == signature {
		return false
	}

	return true
}

func recordUpstreamModelUpdateNotificationSent(
	now int64,
	changedChannels int,
	failedChannels int,
	failedChannelIDs []int,
	channelSummaries []upstreamModelUpdateChannelSummary,
	addModelSamples []string,
	removeModelSamples []string,
) {
	signature := upstreamModelUpdateNotificationSignature(
		changedChannels,
		failedChannels,
		failedChannelIDs,
		channelSummaries,
		addModelSamples,
		removeModelSamples,
	)

	channelUpstreamModelUpdateNotifyState.Lock()
	defer channelUpstreamModelUpdateNotifyState.Unlock()

	channelUpstreamModelUpdateNotifyState.lastNotifiedAt = now
	channelUpstreamModelUpdateNotifyState.lastChangedChannels = changedChannels
	channelUpstreamModelUpdateNotifyState.lastFailedChannels = failedChannels
	channelUpstreamModelUpdateNotifyState.lastSignature = signature
}

func buildUpstreamModelUpdateTaskNotificationContent(
	checkedChannels int,
	changedChannels int,
	detectedAddModels int,
	detectedRemoveModels int,
	autoAddedModels int,
	failedChannelIDs []int,
	channelSummaries []upstreamModelUpdateChannelSummary,
	addModelSamples []string,
	removeModelSamples []string,
) string {
	var builder strings.Builder
	failedChannels := len(failedChannelIDs)
	builder.WriteString(fmt.Sprintf(
		"上游模型巡检摘要：检测渠道 %d 个，发现变更 %d 个，新增 %d 个，删除 %d 个，自动同步新增 %d 个，失败 %d 个。",
		checkedChannels,
		changedChannels,
		detectedAddModels,
		detectedRemoveModels,
		autoAddedModels,
		failedChannels,
	))

	if len(channelSummaries) > 0 {
		displayCount := min(len(channelSummaries), channelUpstreamModelUpdateNotifyMaxChannelDetails)
		builder.WriteString(fmt.Sprintf("\n\n变更渠道明细（展示 %d/%d）：", displayCount, len(channelSummaries)))
		for _, summary := range channelSummaries[:displayCount] {
			builder.WriteString(fmt.Sprintf("\n- %s (+%d / -%d)", summary.ChannelName, summary.AddCount, summary.RemoveCount))
		}
		if len(channelSummaries) > displayCount {
			builder.WriteString(fmt.Sprintf("\n- 其余 %d 个渠道已省略", len(channelSummaries)-displayCount))
		}
	}

	normalizedAddModelSamples := normalizeModelNames(addModelSamples)
	if len(normalizedAddModelSamples) > 0 {
		displayCount := min(len(normalizedAddModelSamples), channelUpstreamModelUpdateNotifyMaxModelDetails)
		builder.WriteString(fmt.Sprintf("\n\n新增模型示例（展示 %d/%d）：%s",
			displayCount,
			len(normalizedAddModelSamples),
			strings.Join(normalizedAddModelSamples[:displayCount], ", "),
		))
		if len(normalizedAddModelSamples) > displayCount {
			builder.WriteString(fmt.Sprintf("（其余 %d 个已省略）", len(normalizedAddModelSamples)-displayCount))
		}
	}

	normalizedRemoveModelSamples := normalizeModelNames(removeModelSamples)
	if len(normalizedRemoveModelSamples) > 0 {
		displayCount := min(len(normalizedRemoveModelSamples), channelUpstreamModelUpdateNotifyMaxModelDetails)
		builder.WriteString(fmt.Sprintf("\n\n删除模型示例（展示 %d/%d）：%s",
			displayCount,
			len(normalizedRemoveModelSamples),
			strings.Join(normalizedRemoveModelSamples[:displayCount], ", "),
		))
		if len(normalizedRemoveModelSamples) > displayCount {
			builder.WriteString(fmt.Sprintf("（其余 %d 个已省略）", len(normalizedRemoveModelSamples)-displayCount))
		}
	}

	if failedChannels > 0 {
		displayCount := min(failedChannels, channelUpstreamModelUpdateNotifyMaxFailedChannelIDs)
		displayIDs := lo.Map(failedChannelIDs[:displayCount], func(channelID int, _ int) string {
			return fmt.Sprintf("%d", channelID)
		})
		builder.WriteString(fmt.Sprintf(
			"\n\n失败渠道 ID（展示 %d/%d）：%s",
			displayCount,
			failedChannels,
			strings.Join(displayIDs, ", "),
		))
		if failedChannels > displayCount {
			builder.WriteString(fmt.Sprintf("（其余 %d 个已省略）", failedChannels-displayCount))
		}
	}
	return builder.String()
}

type upstreamModelUpdateSummary struct {
	CheckedChannels          int    `json:"checked_channels"`
	ChangedChannels          int    `json:"changed_channels"`
	DetectedAddModels        int    `json:"detected_add_models"`
	DetectedRemoveModels     int    `json:"detected_remove_models"`
	FailedChannels           int    `json:"failed_channels"`
	AutoAddedModels          int    `json:"auto_added_models"`
	RuntimeCacheRefreshError string `json:"runtime_cache_refresh_error,omitempty"`
}

func runChannelUpstreamModelUpdateTaskOnce(ctx context.Context, force bool, allowAutoApply bool, allowCodexCredentialRefresh bool, report func(processed, total int)) (upstreamModelUpdateSummary, error) {
	return runChannelUpstreamModelUpdateTaskOnceWithTask(
		ctx,
		"",
		"",
		force,
		allowAutoApply,
		allowCodexCredentialRefresh,
		report,
	)
}

func runChannelUpstreamModelUpdateTaskOnceWithTask(
	ctx context.Context,
	taskID string,
	runnerID string,
	force bool,
	allowAutoApply bool,
	allowCodexCredentialRefresh bool,
	report func(processed, total int),
) (upstreamModelUpdateSummary, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	checkedChannels := 0
	failedChannels := 0
	failedChannelIDs := make([]int, 0)
	changedChannels := 0
	detectedAddModels := 0
	detectedRemoveModels := 0
	autoAddedModelCount := 0
	channelSummaries := make([]upstreamModelUpdateChannelSummary, 0)
	addModelSamples := make([]string, 0)
	removeModelSamples := make([]string, 0)
	refreshNeeded := false
	var runErr error

	totalChannels, err := countEnabledChannelsForUpstreamModelUpdate(ctx)
	totalChannelsKnown := true
	if err != nil {
		common.SysLog(fmt.Sprintf("upstream model update count enabled channels failed: %v", err))
		totalChannels = 0
		totalChannelsKnown = false
	}
	processed := 0
	reportTotal := func() int {
		if totalChannelsKnown {
			return int(totalChannels)
		}
		return processed
	}
	lastID := 0
scanLoop:
	for {
		if ctx != nil && ctx.Err() != nil {
			runErr = ctx.Err()
			break
		}
		var channels []*model.Channel
		query := model.DB.WithContext(ctx).
			Select(channelUpstreamModelUpdateSelectFields).
			Where("status = ?", common.ChannelStatusEnabled).
			Order("id asc").
			Limit(channelUpstreamModelUpdateTaskBatchSize)
		if lastID > 0 {
			query = query.Where("id > ?", lastID)
		}
		err := query.Find(&channels).Error
		if err != nil {
			common.SysLog(fmt.Sprintf("upstream model update task query failed: %v", err))
			runErr = err
			break
		}
		if len(channels) == 0 {
			break
		}
		lastID = channels[len(channels)-1].Id

		for _, channel := range channels {
			if channel == nil {
				continue
			}
			if ctx != nil && ctx.Err() != nil {
				runErr = ctx.Err()
				break scanLoop
			}
			processed++
			if report != nil && totalChannelsKnown {
				report(processed, reportTotal())
			}

			if !channelSupportsUpstreamModelUpdate(channel) {
				if channelSettingsContainUpstreamModelUpdateFields(channel.OtherSettings) {
					if cleaned, err := cleanupUnsupportedChannelUpstreamModelUpdateSettingsWithContext(
						ctx,
						channel.Id,
						taskID,
						runnerID,
					); err != nil {
						if errors.Is(err, model.ErrSystemTaskLockLost) {
							runErr = err
							break scanLoop
						}
						failedChannels++
						failedChannelIDs = append(failedChannelIDs, channel.Id)
						common.SysLog(fmt.Sprintf("upstream model update cleanup failed: channel_id=%d channel_name=%s err=%v", channel.Id, channel.Name, err))
					} else if cleaned {
						refreshNeeded = true
					}
				}
				continue
			}
			settings := channel.GetOtherSettings()
			if !settings.UpstreamModelUpdateCheckEnabled {
				continue
			}

			checkedChannels++
			originalSettingsRaw := channel.OtherSettings
			originalCodexKey := ""
			if channel.Type == constant.ChannelTypeCodex {
				originalCodexKey = channel.Key
			}
			modelsChanged, autoAddedModels, err := checkAndPersistChannelUpstreamModelUpdatesWithOptions(
				ctx,
				channel,
				&settings,
				force,
				allowAutoApply,
				true,
				channelUpstreamModelUpdateCheckOptions{AllowCodexCredentialRefresh: allowCodexCredentialRefresh},
				taskID,
				runnerID,
			)
			if channel.Type == constant.ChannelTypeCodex && channel.Key != originalCodexKey {
				// Credential refresh persists the new key before model update
				// persistence. Even when the provider retry fails, the runtime
				// cache must be rebuilt so it cannot keep the old token.
				refreshNeeded = true
			}
			if err != nil {
				if errors.Is(err, model.ErrSystemTaskLockLost) {
					runErr = err
					break scanLoop
				}
				if ctx != nil && ctx.Err() != nil {
					runErr = ctx.Err()
					break scanLoop
				}
				failedChannels++
				failedChannelIDs = append(failedChannelIDs, channel.Id)
				common.SysLog(fmt.Sprintf("upstream model update check failed: channel_id=%d channel_name=%s err=%v", channel.Id, channel.Name, err))
				continue
			}
			currentAddModels := normalizeModelNames(settings.UpstreamModelUpdateLastDetectedModels)
			currentRemoveModels := normalizeModelNames(settings.UpstreamModelUpdateLastRemovedModels)
			currentAddCount := len(currentAddModels) + len(autoAddedModels)
			currentRemoveCount := len(currentRemoveModels)
			detectedAddModels += currentAddCount
			detectedRemoveModels += currentRemoveCount
			if currentAddCount > 0 || currentRemoveCount > 0 {
				changedChannels++
				channelSummaries = append(channelSummaries, upstreamModelUpdateChannelSummary{
					ChannelID:   channel.Id,
					ChannelName: channel.Name,
					AddCount:    currentAddCount,
					RemoveCount: currentRemoveCount,
				})
			}
			addModelSamples = mergeModelNames(addModelSamples, currentAddModels)
			addModelSamples = mergeModelNames(addModelSamples, autoAddedModels)
			removeModelSamples = mergeModelNames(removeModelSamples, currentRemoveModels)
			if modelsChanged {
				refreshNeeded = true
			}
			if channel.OtherSettings != originalSettingsRaw {
				refreshNeeded = true
			}
			autoAddedModelCount += len(autoAddedModels)

			if common.RequestInterval > 0 {
				if ctx == nil {
					time.Sleep(common.RequestInterval)
				} else {
					select {
					case <-ctx.Done():
						runErr = ctx.Err()
						break scanLoop
					case <-time.After(common.RequestInterval):
					}
				}
			}
		}

		if len(channels) < channelUpstreamModelUpdateTaskBatchSize {
			break
		}
	}

	if runErr == nil && ctx != nil && ctx.Err() != nil {
		runErr = ctx.Err()
	}

	if report != nil && runErr == nil {
		total := int(totalChannels)
		if !totalChannelsKnown {
			total = processed
		}
		report(total, total)
	}

	summary := upstreamModelUpdateSummary{
		CheckedChannels:      checkedChannels,
		ChangedChannels:      changedChannels,
		DetectedAddModels:    detectedAddModels,
		DetectedRemoveModels: detectedRemoveModels,
		FailedChannels:       failedChannels,
		AutoAddedModels:      autoAddedModelCount,
	}
	var cacheRefreshErr error
	if refreshNeeded {
		cacheRefreshErr = refreshChannelRuntimeCache()
		if cacheRefreshErr != nil {
			summary.RuntimeCacheRefreshError = cacheRefreshErr.Error()
			if runErr == nil {
				runErr = fmt.Errorf("runtime channel cache refresh failed: %w", cacheRefreshErr)
			}
		}
	}

	finalErr := runErr
	if finalErr == nil && failedChannels > 0 {
		finalErr = fmt.Errorf("failed channel checks: %d", failedChannels)
	}

	if checkedChannels > 0 || common.DebugEnabled {
		common.SysLog(fmt.Sprintf(
			"upstream model update task done: checked_channels=%d changed_channels=%d detected_add_models=%d detected_remove_models=%d failed_channels=%d auto_added_models=%d",
			checkedChannels,
			changedChannels,
			detectedAddModels,
			detectedRemoveModels,
			failedChannels,
			autoAddedModelCount,
		))
	}
	if runErr == nil && (changedChannels > 0 || failedChannels > 0) {
		now := common.GetTimestamp()
		if !shouldSendUpstreamModelUpdateNotification(
			now,
			changedChannels,
			failedChannels,
			failedChannelIDs,
			channelSummaries,
			addModelSamples,
			removeModelSamples,
		) {
			common.SysLog(fmt.Sprintf(
				"upstream model update notification skipped in 24h window: changed_channels=%d failed_channels=%d",
				changedChannels,
				failedChannels,
			))
			return summary, finalErr
		}
		sentCount := notifyUpstreamModelUpdateWatchers(
			"上游模型巡检通知",
			buildUpstreamModelUpdateTaskNotificationContent(
				checkedChannels,
				changedChannels,
				detectedAddModels,
				detectedRemoveModels,
				autoAddedModelCount,
				failedChannelIDs,
				channelSummaries,
				addModelSamples,
				removeModelSamples,
			),
		)
		if sentCount > 0 {
			recordUpstreamModelUpdateNotificationSent(
				now,
				changedChannels,
				failedChannels,
				failedChannelIDs,
				channelSummaries,
				addModelSamples,
				removeModelSamples,
			)
		}
	}
	return summary, finalErr
}

func ApplyChannelUpstreamModelUpdates(c *gin.Context) {
	var req applyChannelUpstreamModelUpdatesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.ID <= 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "渠道 ID 无效",
		})
		return
	}

	channel, err := model.GetChannelById(req.ID, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if channel.Status != common.ChannelStatusEnabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "渠道已禁用",
		})
		return
	}

	originalSettingsRaw := channel.OtherSettings
	requestCtx := context.Background()
	if c.Request != nil {
		requestCtx = c.Request.Context()
	}
	addedModels, removedModels, ignoredModels, remainingModels, remainingRemoveModels, modelsChanged, err := applyChannelUpstreamModelUpdatesWithContext(
		requestCtx,
		channel,
		req.AddModels,
		req.IgnoreModels,
		req.RemoveModels,
		true,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if channel.Status != common.ChannelStatusEnabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "渠道已禁用",
		})
		return
	}
	needsCacheRefresh := modelsChanged || channel.OtherSettings != originalSettingsRaw
	if needsCacheRefresh {
		if err := refreshChannelRuntimeCache(); err != nil {
			common.ApiError(c, fmt.Errorf("channel update persisted but runtime cache refresh failed: %w", err))
			return
		}
	}
	if !channelSupportsUpstreamModelUpdate(channel) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "该渠道不支持上游模型更新",
		})
		return
	}

	recordManageAudit(c, "channel.upstream_apply", map[string]interface{}{
		"id": channel.Id,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"id":                      channel.Id,
			"added_models":            addedModels,
			"removed_models":          removedModels,
			"ignored_models":          ignoredModels,
			"remaining_models":        remainingModels,
			"remaining_remove_models": remainingRemoveModels,
			"models":                  channel.Models,
			"settings":                channel.OtherSettings,
		},
	})
}

func DetectChannelUpstreamModelUpdates(c *gin.Context) {
	var req applyChannelUpstreamModelUpdatesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.ID <= 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "渠道 ID 无效",
		})
		return
	}

	channel, err := model.GetChannelById(req.ID, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if channel.Status != common.ChannelStatusEnabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "渠道已禁用",
		})
		return
	}

	originalSettingsRaw := channel.OtherSettings
	settings := channel.GetOtherSettings()
	originalCodexKey := ""
	if channel.Type == constant.ChannelTypeCodex {
		originalCodexKey = channel.Key
	}
	modelsChanged, autoAddedModels, err := checkAndPersistChannelUpstreamModelUpdatesWithOptions(
		c.Request.Context(),
		channel,
		&settings,
		true,
		false,
		true,
		channelUpstreamModelUpdateCheckOptions{AllowCodexCredentialRefresh: false},
		"",
		"",
	)
	credentialChanged := channel.Type == constant.ChannelTypeCodex && channel.Key != originalCodexKey
	if err != nil {
		if credentialChanged {
			if cacheErr := refreshChannelRuntimeCache(); cacheErr != nil {
				err = fmt.Errorf("%w; runtime channel cache refresh failed: %v", err, cacheErr)
			}
		}
		common.ApiError(c, err)
		return
	}
	if channel.Status != common.ChannelStatusEnabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "渠道已禁用",
		})
		return
	}
	needsCacheRefresh := credentialChanged ||
		modelsChanged ||
		channel.OtherSettings != originalSettingsRaw
	if needsCacheRefresh {
		if err := refreshChannelRuntimeCache(); err != nil {
			common.ApiError(c, fmt.Errorf("channel detection persisted but runtime cache refresh failed: %w", err))
			return
		}
	}
	if !channelSupportsUpstreamModelUpdate(channel) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "该渠道不支持上游模型更新",
		})
		return
	}
	addModels := normalizeModelNames(settings.UpstreamModelUpdateLastDetectedModels)
	removeModels := normalizeModelNames(settings.UpstreamModelUpdateLastRemovedModels)
	recordManageAudit(c, "channel.upstream_detect", map[string]interface{}{
		"id":               channel.Id,
		"add_count":        len(addModels),
		"remove_count":     len(removeModels),
		"auto_added_count": len(autoAddedModels),
	})

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": detectChannelUpstreamModelUpdatesResult{
			ChannelID:       channel.Id,
			ChannelName:     channel.Name,
			AddModels:       addModels,
			RemoveModels:    removeModels,
			LastCheckTime:   settings.UpstreamModelUpdateLastCheckTime,
			AutoAddedModels: len(autoAddedModels),
		},
	})
}

func applyChannelUpstreamModelUpdates(
	channel *model.Channel,
	addModelsInput []string,
	ignoreModelsInput []string,
	removeModelsInput []string,
	requireEnabled bool,
) (
	addedModels []string,
	removedModels []string,
	ignoredModels []string,
	remainingModels []string,
	remainingRemoveModels []string,
	modelsChanged bool,
	err error,
) {
	return applyChannelUpstreamModelUpdatesWithContext(
		context.Background(),
		channel,
		addModelsInput,
		ignoreModelsInput,
		removeModelsInput,
		requireEnabled,
	)
}

func applyChannelUpstreamModelUpdatesWithContext(
	ctx context.Context,
	channel *model.Channel,
	addModelsInput []string,
	ignoreModelsInput []string,
	removeModelsInput []string,
	requireEnabled bool,
) (
	addedModels []string,
	removedModels []string,
	ignoredModels []string,
	remainingModels []string,
	remainingRemoveModels []string,
	modelsChanged bool,
	err error,
) {
	return applyChannelUpstreamModelUpdatesWithContextAndTask(
		ctx,
		channel,
		addModelsInput,
		ignoreModelsInput,
		removeModelsInput,
		requireEnabled,
		"",
		"",
	)
}

func applyChannelUpstreamModelUpdatesWithContextAndTask(
	ctx context.Context,
	channel *model.Channel,
	addModelsInput []string,
	ignoreModelsInput []string,
	removeModelsInput []string,
	requireEnabled bool,
	taskID string,
	runnerID string,
) (
	addedModels []string,
	removedModels []string,
	ignoredModels []string,
	remainingModels []string,
	remainingRemoveModels []string,
	modelsChanged bool,
	err error,
) {
	if channel == nil || channel.Id <= 0 {
		return nil, nil, nil, nil, nil, false, errors.New("invalid channel")
	}
	err = withLockedChannelUpstreamModelUpdateContext(ctx, channel.Id, taskID, runnerID, func(tx *gorm.DB, lockedChannel *model.Channel) error {
		if requireEnabled && lockedChannel.Status != common.ChannelStatusEnabled {
			*channel = *lockedChannel
			return nil
		}
		if !channelSupportsUpstreamModelUpdate(lockedChannel) {
			hasResidualKeys := channelSettingsContainUpstreamModelUpdateFields(lockedChannel.OtherSettings)
			settings := lockedChannel.GetOtherSettings()
			if clearUnsupportedChannelUpstreamModelUpdateSettings(&settings) || hasResidualKeys {
				if err := updateChannelUpstreamModelSettingsWithTx(tx, lockedChannel, settings, false); err != nil {
					return err
				}
			}
			*channel = *lockedChannel
			return nil
		}
		settings := lockedChannel.GetOtherSettings()
		if !settings.UpstreamModelUpdateCheckEnabled {
			return errors.New("upstream model update check is not enabled")
		}
		addedModels, removedModels, ignoredModels, remainingModels, remainingRemoveModels, modelsChanged, err = applyChannelUpstreamModelUpdatesWithTx(
			tx,
			lockedChannel,
			addModelsInput,
			ignoreModelsInput,
			removeModelsInput,
		)
		if err != nil {
			return err
		}
		*channel = *lockedChannel
		return nil
	})
	return
}

func applyChannelUpstreamModelUpdatesWithTx(
	tx *gorm.DB,
	channel *model.Channel,
	addModelsInput []string,
	ignoreModelsInput []string,
	removeModelsInput []string,
) (
	addedModels []string,
	removedModels []string,
	ignoredModels []string,
	remainingModels []string,
	remainingRemoveModels []string,
	modelsChanged bool,
	err error,
) {
	settings := channel.GetOtherSettings()
	pendingAddModels := normalizeModelNames(settings.UpstreamModelUpdateLastDetectedModels)
	pendingRemoveModels := normalizeModelNames(settings.UpstreamModelUpdateLastRemovedModels)
	addModels := intersectModelNames(addModelsInput, pendingAddModels)
	ignoredModels = intersectModelNames(ignoreModelsInput, pendingAddModels)
	removeModels := intersectModelNames(removeModelsInput, pendingRemoveModels)
	removeModels = subtractModelNames(removeModels, addModels)

	originModels := normalizeModelNames(channel.GetModels())
	nextModels := applySelectedModelChanges(originModels, addModels, removeModels)
	modelsChanged = !slices.Equal(originModels, nextModels)
	if modelsChanged {
		channel.Models = strings.Join(nextModels, ",")
	}

	settings.UpstreamModelUpdateIgnoredModels = mergeModelNames(settings.UpstreamModelUpdateIgnoredModels, ignoredModels)
	if len(addModels) > 0 {
		settings.UpstreamModelUpdateIgnoredModels = subtractModelNames(settings.UpstreamModelUpdateIgnoredModels, addModels)
	}
	remainingModels = subtractModelNames(pendingAddModels, append(addModels, ignoredModels...))
	remainingRemoveModels = subtractModelNames(pendingRemoveModels, removeModels)
	settings.UpstreamModelUpdateLastDetectedModels = remainingModels
	settings.UpstreamModelUpdateLastRemovedModels = remainingRemoveModels
	settings.UpstreamModelUpdateLastCheckTime = common.GetTimestamp()

	if err := persistChannelUpstreamModelSettingsAndAbilitiesWithTx(tx, channel, settings, modelsChanged, removeModels); err != nil {
		return nil, nil, nil, nil, nil, false, err
	}
	return addModels, removeModels, ignoredModels, remainingModels, remainingRemoveModels, modelsChanged, nil
}

func collectPendingApplyUpstreamModelChanges(settings dto.ChannelOtherSettings) (pendingAddModels []string, pendingRemoveModels []string) {
	return normalizeModelNames(settings.UpstreamModelUpdateLastDetectedModels), normalizeModelNames(settings.UpstreamModelUpdateLastRemovedModels)
}

func runApplyAllChannelUpstreamModelUpdates(
	ctx context.Context,
	taskID string,
	runnerID string,
	report func(processed, total int),
) (applyAllChannelUpstreamModelUpdatesSummary, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	summary := applyAllChannelUpstreamModelUpdatesSummary{
		FailedChannelIDs: make([]int, 0),
		Results:          make([]applyAllChannelUpstreamModelUpdatesResult, 0),
	}
	totalChannels, err := countEnabledChannelsForUpstreamModelUpdate(ctx)
	totalChannelsKnown := err == nil
	if err != nil {
		common.SysLog(fmt.Sprintf("apply all upstream model updates count enabled channels failed: %v", err))
		totalChannels = 0
	}
	processed := 0
	reportProgress := func() {
		if report == nil {
			return
		}
		total := processed
		if totalChannelsKnown {
			total = int(totalChannels)
		}
		report(processed, total)
	}

	var runErr error
	refreshNeeded := false
	lastID := 0
applyAllLoop:
	for {
		if err := ctx.Err(); err != nil {
			runErr = err
			break
		}
		channels, err := findEnabledChannelsAfterIDForApplyAll(
			ctx,
			lastID,
			channelUpstreamModelUpdateTaskBatchSize,
		)
		if err != nil {
			runErr = err
			break
		}
		if len(channels) == 0 {
			break
		}
		lastID = channels[len(channels)-1].Id

		for _, channel := range channels {
			if err := ctx.Err(); err != nil {
				runErr = err
				break applyAllLoop
			}
			if channel == nil {
				continue
			}
			processed++
			reportProgress()

			if !channelSupportsUpstreamModelUpdate(channel) {
				if channelSettingsContainUpstreamModelUpdateFields(channel.OtherSettings) {
					if cleaned, err := cleanupUnsupportedChannelUpstreamModelUpdateSettingsWithContext(
						ctx,
						channel.Id,
						taskID,
						runnerID,
					); err != nil {
						if errors.Is(err, model.ErrSystemTaskLockLost) || ctx.Err() != nil {
							if ctx.Err() != nil {
								runErr = ctx.Err()
							} else {
								runErr = err
							}
							break applyAllLoop
						}
						summary.FailedChannelIDs = append(summary.FailedChannelIDs, channel.Id)
					} else if cleaned {
						refreshNeeded = true
					}
				}
				continue
			}

			settings := channel.GetOtherSettings()
			if !settings.UpstreamModelUpdateCheckEnabled {
				continue
			}
			originalSettingsRaw := channel.OtherSettings
			pendingAddModels, pendingRemoveModels := collectPendingApplyUpstreamModelChanges(settings)
			if len(pendingAddModels) == 0 {
				summary.RemainingRemoveModelsCount += len(pendingRemoveModels)
				continue
			}

			var addedModels, removedModels, remainingModels, remainingRemoveModels []string
			var modelsChanged bool
			if taskID != "" {
				addedModels, removedModels, _, remainingModels, remainingRemoveModels, modelsChanged, err =
					applyChannelUpstreamModelUpdatesForApplyAllWithTask(
						ctx,
						channel,
						pendingAddModels,
						nil,
						nil,
						true,
						taskID,
						runnerID,
					)
			} else {
				addedModels, removedModels, _, remainingModels, remainingRemoveModels, modelsChanged, err =
					applyChannelUpstreamModelUpdatesForApplyAll(
						ctx,
						channel,
						pendingAddModels,
						nil,
						nil,
						true,
					)
			}
			if err != nil {
				if errors.Is(err, model.ErrSystemTaskLockLost) || ctx.Err() != nil {
					if ctx.Err() != nil {
						runErr = ctx.Err()
					} else {
						runErr = err
					}
					break applyAllLoop
				}
				summary.RemainingRemoveModelsCount += len(pendingRemoveModels)
				summary.FailedChannelIDs = append(summary.FailedChannelIDs, channel.Id)
				continue
			}
			if modelsChanged {
				refreshNeeded = true
			}
			if channel.OtherSettings != originalSettingsRaw {
				refreshNeeded = true
			}
			summary.RemainingRemoveModelsCount += len(remainingRemoveModels)
			summary.AddedModels += len(addedModels)
			summary.RemovedModels += len(removedModels)
			summary.Results = append(summary.Results, applyAllChannelUpstreamModelUpdatesResult{
				ChannelID:             channel.Id,
				ChannelName:           channel.Name,
				AddedModels:           addedModels,
				RemovedModels:         removedModels,
				RemainingModels:       remainingModels,
				RemainingRemoveModels: remainingRemoveModels,
			})
		}

		if len(channels) < channelUpstreamModelUpdateTaskBatchSize {
			break
		}
	}

	if ctx.Err() != nil && runErr == nil {
		runErr = ctx.Err()
	}
	summary.ProcessedChannels = processed
	if report != nil {
		report(processed, func() int {
			if totalChannelsKnown {
				return int(totalChannels)
			}
			return processed
		}())
	}
	if len(summary.FailedChannelIDs) > 0 && runErr == nil {
		runErr = fmt.Errorf("failed channel updates: %d", len(summary.FailedChannelIDs))
	}
	if refreshNeeded {
		if err := refreshChannelRuntimeCache(); err != nil {
			summary.RuntimeCacheRefreshError = err.Error()
			if runErr == nil {
				runErr = fmt.Errorf("batch apply persisted but runtime cache refresh failed: %w", err)
			}
		}
	}
	return summary, runErr
}

func findEnabledChannelsAfterID(lastID int, batchSize int) ([]*model.Channel, error) {
	return findEnabledChannelsAfterIDWithContext(context.Background(), lastID, batchSize)
}

func findEnabledChannelsAfterIDWithContext(ctx context.Context, lastID int, batchSize int) ([]*model.Channel, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var channels []*model.Channel
	query := model.DB.WithContext(ctx).
		Select(channelUpstreamModelUpdateSelectFields).
		Where("status = ?", common.ChannelStatusEnabled).
		Order("id asc").
		Limit(batchSize)
	if lastID > 0 {
		query = query.Where("id > ?", lastID)
	}
	return channels, query.Find(&channels).Error
}

func isChannelUpstreamModelUpdateOperationLockActive(ctx context.Context) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := model.CleanupInactiveSystemTaskLocks(
		model.SystemTaskTypeModelUpdate,
		common.GetTimestamp(),
	); err != nil {
		return false, err
	}
	var count int64
	err := model.DB.WithContext(ctx).
		Model(&model.SystemTaskLock{}).
		Where("type = ? AND locked_until >= ?", model.SystemTaskTypeModelUpdate, common.GetTimestamp()).
		Count(&count).Error
	return count > 0, err
}

func ApplyAllChannelUpstreamModelUpdates(c *gin.Context) {
	requestCtx := context.Background()
	if c != nil && c.Request != nil {
		requestCtx = c.Request.Context()
	}
	if err := requestCtx.Err(); err != nil {
		return
	}
	activeTask, err := service.GetCurrentSystemTask(model.SystemTaskTypeModelUpdate)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if activeTask != nil {
		renderModelUpdateTaskConflict(c, activeTask)
		return
	}
	lockActive, err := isChannelUpstreamModelUpdateOperationLockActive(requestCtx)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if lockActive {
		renderModelUpdateOperationConflict(c)
		return
	}
	task, created, err := service.EnqueueSystemTask(
		model.SystemTaskTypeModelUpdateApplyAll,
		nil,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !created {
		renderModelUpdateTaskConflict(c, task)
		return
	}
	recordManageAudit(c, "channel.upstream_apply_all", map[string]interface{}{
		"task_id": task.TaskID,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"task_id": task.TaskID,
			"status":  task.Status,
			"type":    task.Type,
		},
	})
}

func renderModelUpdateTaskConflict(c *gin.Context, task *model.SystemTask) {
	c.JSON(http.StatusConflict, gin.H{
		"success": false,
		"message": "已有模型更新任务正在运行或等待中，不能启动本次任务",
		"data": gin.H{
			"task_id": task.TaskID,
			"status":  task.Status,
			"type":    task.Type,
		},
	})
}

func renderModelUpdateOperationConflict(c *gin.Context) {
	c.JSON(http.StatusConflict, gin.H{
		"success": false,
		"message": "已有模型更新任务正在运行或等待中，不能启动本次任务",
		"data": gin.H{
			"status": model.SystemTaskStatusRunning,
			"type":   model.SystemTaskTypeModelUpdate,
		},
	})
}

func DetectAllChannelUpstreamModelUpdates(c *gin.Context) {
	requestCtx := context.Background()
	if c != nil && c.Request != nil {
		requestCtx = c.Request.Context()
	}
	if err := requestCtx.Err(); err != nil {
		return
	}
	activeTask, err := service.GetCurrentSystemTask(model.SystemTaskTypeModelUpdate)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if activeTask != nil {
		renderModelUpdateTaskConflict(c, activeTask)
		return
	}
	lockActive, err := isChannelUpstreamModelUpdateOperationLockActive(requestCtx)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if lockActive {
		renderModelUpdateOperationConflict(c)
		return
	}

	task, created, err := service.EnqueueSystemTask(model.SystemTaskTypeModelUpdateManual, nil)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !created {
		renderModelUpdateTaskConflict(c, task)
		return
	}

	recordManageAudit(c, "channel.upstream_detect_all", map[string]interface{}{
		"task_id": task.TaskID,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"task_id": task.TaskID,
			"status":  task.Status,
			"type":    task.Type,
		},
	})
}

func GetCurrentChannelUpstreamModelUpdateTask(c *gin.Context) {
	activeTask, err := service.GetCurrentSystemTask(model.SystemTaskTypeModelUpdate)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if activeTask == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data":    nil,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    activeTask.ToResponse(),
	})
}

func CancelChannelUpstreamModelUpdateTask(c *gin.Context) {
	var req cancelChannelUpstreamModelUpdateTaskRequest
	if c != nil && c.Request != nil && c.Request.Body != nil {
		if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
			common.ApiError(c, err)
			return
		}
	}

	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "必须提供任务 ID",
		})
		return
	}

	task, cancelled, err := service.CancelSystemTask(
		taskID,
		[]string{
			model.SystemTaskTypeModelUpdateManual,
			model.SystemTaskTypeModelUpdate,
			model.SystemTaskTypeModelUpdateApplyAll,
		},
		"task cancelled by user",
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if task == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "未找到任务",
		})
		return
	}

	message := ""
	if !cancelled {
		message = "task already finished"
	}
	recordManageAudit(c, "channel.upstream_update_cancel", map[string]interface{}{
		"task_id": task.TaskID,
		"type":    task.Type,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": message,
		"data":    task.ToResponse(),
	})
}

func GetChannelUpstreamModelUpdateTask(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("task_id"))
	if taskID == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "必须提供任务 ID",
		})
		return
	}

	task, err := model.GetSystemTaskByTaskID(taskID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if task == nil ||
		(task.Type != model.SystemTaskTypeModelUpdateManual &&
			task.Type != model.SystemTaskTypeModelUpdate &&
			task.Type != model.SystemTaskTypeModelUpdateApplyAll) {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "未找到任务",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    task.ToResponse(),
	})
}
