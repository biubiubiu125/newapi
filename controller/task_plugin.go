package controller

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/plugins"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const maxTaskPluginSourceBytes = 8 << 20

type taskPluginUploadRequest struct {
	Source       string `json:"source" binding:"required"`
	Enabled      *bool  `json:"enabled"`
	Remark       string `json:"remark"`
	Force        bool   `json:"force"`
	SourceSha256 string `json:"sourceSha256"`
	Icon         string `json:"icon"`
}

func UploadTaskPlugin(c *gin.Context) {
	var request taskPluginUploadRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if len(request.Source) > maxTaskPluginSourceBytes {
		writeTaskPluginLocalizedError(c, i18n.MsgPluginSourceTooLarge, nil, "plugin source exceeds 8 MiB")
		return
	}
	if expected := strings.TrimSpace(request.SourceSha256); expected != "" {
		actual := fmt.Sprintf("%x", sha256.Sum256([]byte(request.Source)))
		if !strings.EqualFold(actual, expected) {
			writeTaskPluginLocalizedError(c, i18n.MsgPluginSHA256Mismatch, nil, "plugin source sha256 mismatch")
			return
		}
	}
	temporary := jsplugin.NewRegistry()
	loaded, err := temporary.Register(request.Source, jsplugin.Options{})
	if err != nil {
		respondTaskPluginCompileError(c, err)
		return
	}
	if err = jsplugin.ValidateV1Meta(loaded.Meta); err != nil {
		respondTaskPluginCompileError(c, err)
		return
	}
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	if enabled && !request.Force {
		if err = jsplugin.PreflightRoutingConflict(jsplugin.DefaultRegistry.Generation(), loaded); err != nil {
			respondTaskPluginCompileError(c, err)
			return
		}
	}
	plugin := model.TaskPlugin{
		Key: loaded.Meta.Key, APIVersion: loaded.Meta.APIVersion, Version: loaded.Meta.Version,
		Source: request.Source, SourceHash: fmt.Sprintf("%x", sha256.Sum256([]byte(request.Source))),
		Enabled: enabled, Remark: request.Remark,
	}
	if icon := strings.TrimSpace(request.Icon); icon != "" {
		mediaType, data, iconErr := jsplugin.DecodeIconDataURI(icon)
		if iconErr != nil {
			respondTaskPluginCompileError(c, iconErr)
			return
		}
		plugin.IconMediaType = mediaType
		plugin.IconData = data
	}
	if err = model.WithTaskPluginKeyLock(plugin.Key, func() error {
		previousVersions, err := model.ListTaskPluginVersions(plugin.Key)
		if err != nil {
			return err
		}
		if err = model.SaveTaskPluginLocked(&plugin); err != nil {
			return err
		}
		return syncTaskPluginsOnceWithRollbackLocked(c.Request.Context(), plugin.Key, previousVersions)
	}); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, taskPluginDetail{Plugin: &plugin, Meta: loaded.Meta, Source: plugin.Source, Layer: "override", HasIcon: plugin.HasIcon()})
}

func GetTaskPluginVersions(c *gin.Context) {
	plugins, err := model.ListTaskPluginVersions(c.Param("key"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, plugins)
}

type taskPluginListItem struct {
	Meta          jsplugin.Meta  `json:"meta"`
	Source        string         `json:"source"`
	Enabled       bool           `json:"enabled"`
	Active        bool           `json:"active"`
	SourceHash    string         `json:"source_hash"`
	HasIcon       bool           `json:"has_icon"`
	Remark        string         `json:"remark"`
	RuntimeStatus string         `json:"runtime_status"`
	RuntimeError  string         `json:"runtime_error,omitempty"`
	FactoryMeta   *jsplugin.Meta `json:"factory_meta,omitempty"`
	ChannelCount  int            `json:"channel_count"`
	InFlightCount int64          `json:"in_flight_count"`
}

type taskPluginRebuildOutcome struct {
	Status           string    `json:"status"`
	AttemptedAt      time.Time `json:"attempted_at"`
	Generation       uint64    `json:"generation"`
	DatabaseRevision string    `json:"database_revision,omitempty"`
	PluginErrorCount int       `json:"plugin_error_count"`
	Error            string    `json:"error,omitempty"`
}

type taskPluginRuntimeStatus struct {
	CurrentGeneration     uint64                   `json:"current_generation"`
	GenerationPublishedAt time.Time                `json:"generation_published_at"`
	DatabaseRevision      string                   `json:"database_revision"`
	DatabaseError         string                   `json:"database_error,omitempty"`
	LastRebuild           taskPluginRebuildOutcome `json:"last_rebuild"`
	PluginErrors          map[string]string        `json:"plugin_errors"`
}

func ListTaskPlugins(c *gin.Context) {
	databasePlugins, err := model.ListTaskPlugins()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	snapshot := jsplugin.DefaultRegistry.Snapshot()
	factory := make(map[string]jsplugin.Meta, len(snapshot.Factory))
	override := make(map[string]jsplugin.Meta, len(snapshot.Override))
	for _, meta := range snapshot.Factory {
		factory[meta.Key] = meta
	}
	for _, meta := range snapshot.Override {
		override[meta.Key] = meta
	}
	activeRows := make(map[string]model.TaskPlugin)
	keys := make(map[string]struct{}, len(factory)+len(databasePlugins))
	for key := range factory {
		keys[key] = struct{}{}
	}
	for _, plugin := range databasePlugins {
		keys[plugin.Key] = struct{}{}
		if plugin.Active {
			activeRows[plugin.Key] = plugin
		}
	}

	runtimeErrors := jsplugin.DefaultRegistry.RoutingErrors()
	taskPluginSyncState.Lock()
	for key, message := range taskPluginSyncState.errors {
		runtimeErrors[key] = message
	}
	taskPluginSyncState.Unlock()

	items := make([]taskPluginListItem, 0, len(keys))
	for key := range keys {
		factoryMeta, hasFactory := factory[key]
		row, hasOverride := activeRows[key]
		item := taskPluginListItem{Enabled: true, Active: true, RuntimeStatus: "registered"}
		if hasOverride {
			item.Source = "override"
			if hasFactory {
				item.Source = "override_over_factory"
				factoryCopy := factoryMeta
				item.FactoryMeta = &factoryCopy
			}
			item.Meta = jsplugin.Meta{Key: row.Key, Version: row.Version, APIVersion: row.APIVersion}
			if compiled, compileErr := jsplugin.NewRegistry().Register(row.Source, jsplugin.Options{Key: row.Key, Version: row.Version}); compileErr == nil {
				item.Meta = compiled.Meta
			}
			item.Enabled = row.Enabled
			item.Active = row.Active
			item.SourceHash = row.SourceHash
			item.HasIcon = row.HasIcon() || factoryPluginHasIcon(key)
			item.Remark = row.Remark
			if !constant.TaskPluginOverrideEnabled {
				item.RuntimeStatus = "disabled_fallback"
			} else if message := runtimeErrors[key]; message != "" {
				item.RuntimeStatus = "compile_failed"
				item.RuntimeError = message
			} else if runtimeMeta, ok := override[key]; ok {
				item.Meta = runtimeMeta
			} else if !row.Enabled {
				item.RuntimeStatus = "disabled_fallback"
			} else {
				item.RuntimeStatus = "not_registered"
			}
			// "disabled_fallback" promises that the built-in still serves. When the
			// factory layer is suppressed as well, nothing serves this key.
			if item.RuntimeStatus == "disabled_fallback" && hasFactory && setting.IsTaskPluginFactoryDisabled(key) {
				item.RuntimeStatus = "disabled"
			}
		} else {
			item.Source = "factory"
			item.Meta = factoryMeta
			item.HasIcon = factoryPluginHasIcon(key)
			item.Enabled = !setting.IsTaskPluginFactoryDisabled(key)
			source, sourceErr := plugins.Source(key)
			if sourceErr == nil {
				item.SourceHash = fmt.Sprintf("%x", sha256.Sum256([]byte(source)))
			}
			if !item.Enabled {
				item.RuntimeStatus = "disabled"
			} else if message := runtimeErrors[key]; message != "" {
				item.RuntimeStatus = "compile_failed"
				item.RuntimeError = message
			}
		}
		if !hasFactory {
			channels, inFlight, usageErr := model.GetTaskPluginUsage(key)
			if usageErr != nil {
				common.ApiError(c, usageErr)
				return
			}
			item.ChannelCount = len(channels)
			item.InFlightCount = inFlight
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Meta.SortPriority != items[j].Meta.SortPriority {
			return items[i].Meta.SortPriority > items[j].Meta.SortPriority
		}
		return items[i].Meta.Key < items[j].Meta.Key
	})
	common.ApiSuccess(c, items)
}

func GetTaskPluginRuntime(c *gin.Context) {
	routingStatus := jsplugin.DefaultRegistry.RoutingStatus()
	pluginErrors := routingStatus.Errors

	taskPluginSyncState.Lock()
	for key, message := range taskPluginSyncState.errors {
		pluginErrors[key] = message
	}
	lastRebuild := taskPluginSyncState.lastRebuild
	lastDatabaseRevision := lastRebuild.DatabaseRevision
	taskPluginSyncState.Unlock()

	registryRebuild := routingStatus.LastRebuild
	if lastRebuild.AttemptedAt.Before(registryRebuild.AttemptedAt) {
		lastRebuild = taskPluginRebuildOutcome{
			Status:      registryRebuild.Status,
			AttemptedAt: registryRebuild.AttemptedAt,
			Generation:  registryRebuild.Generation,
			Error:       registryRebuild.Error,
		}
	}
	if lastRebuild.Status == "" {
		lastRebuild.Status = "never"
	}
	lastRebuild.PluginErrorCount = len(pluginErrors)
	if lastRebuild.Status == "success" && len(pluginErrors) > 0 {
		lastRebuild.Status = "partial"
	}

	status := taskPluginRuntimeStatus{
		DatabaseRevision: lastDatabaseRevision,
		LastRebuild:      lastRebuild,
		PluginErrors:     pluginErrors,
	}
	databaseSnapshot, err := model.GetTaskPluginSyncSnapshot()
	if err != nil {
		status.DatabaseError = "database snapshot unavailable"
	} else {
		status.DatabaseRevision = databaseSnapshot.Revision
	}
	if routingStatus.Generation != nil {
		status.CurrentGeneration = routingStatus.Generation.Number
		status.GenerationPublishedAt = routingStatus.Generation.PublishedAt
	}
	common.ApiSuccess(c, status)
}

type taskPluginDetail struct {
	Plugin  *model.TaskPlugin `json:"plugin,omitempty"`
	Meta    jsplugin.Meta     `json:"meta"`
	Source  string            `json:"source"`
	Layer   string            `json:"layer"`
	HasIcon bool              `json:"has_icon"`
}

func GetTaskPlugin(c *gin.Context) {
	key := c.Param("key")
	version := c.Query("version")
	plugin, err := model.GetTaskPluginVersion(key, version)
	if err == nil {
		loaded, compileErr := jsplugin.NewRegistry().Register(plugin.Source, jsplugin.Options{Key: plugin.Key, Version: plugin.Version})
		if compileErr != nil {
			respondTaskPluginCompileError(c, compileErr)
			return
		}
		common.ApiSuccess(c, taskPluginDetail{Plugin: plugin, Meta: loaded.Meta, Source: plugin.Source, Layer: "override", HasIcon: plugin.HasIcon()})
		return
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) || version != "" {
		common.ApiError(c, err)
		return
	}
	source, err := plugins.Source(key)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgPluginNotFound)
		return
	}
	loaded, err := jsplugin.NewRegistry().RegisterFactory(source, jsplugin.Options{Key: key})
	if err != nil {
		respondTaskPluginCompileError(c, err)
		return
	}
	common.ApiSuccess(c, taskPluginDetail{Meta: loaded.Meta, Source: source, Layer: "factory", HasIcon: factoryPluginHasIcon(key)})
}

func GetTaskPluginIcon(c *gin.Context) {
	mediaType, data, ok := resolveTaskPluginIcon(c.Param("key"), c.Query("version"))
	if !ok {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "private, max-age=300")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, mediaType, data)
}

func resolveTaskPluginIcon(key, version string) (string, []byte, bool) {
	if version != "" {
		plugin, err := model.GetTaskPluginVersion(key, version)
		if err != nil || !plugin.HasIcon() {
			return "", nil, false
		}
		return plugin.IconMediaType, plugin.IconData, true
	}
	plugin, err := model.GetTaskPluginVersion(key, "")
	if err == nil && plugin.HasIcon() {
		return plugin.IconMediaType, plugin.IconData, true
	}
	mediaType, data, iconErr := plugins.Icon(key)
	if iconErr != nil || len(data) == 0 {
		return "", nil, false
	}
	return mediaType, data, true
}

func factoryPluginHasIcon(key string) bool {
	_, data, err := plugins.Icon(key)
	return err == nil && len(data) > 0
}

type taskPluginDryRunRequest struct {
	Hook   string            `json:"hook" binding:"required"`
	Member string            `json:"member"`
	Args   []json.RawMessage `json:"args"`
}

func DryRunTaskPlugin(c *gin.Context) {
	var request taskPluginDryRunRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	detailSource := ""
	plugin, err := model.GetTaskPluginVersion(c.Param("key"), "")
	if err == nil {
		detailSource = plugin.Source
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		detailSource, err = plugins.Source(c.Param("key"))
	}
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgPluginNotFound)
		return
	}
	loaded, err := jsplugin.NewRegistry().Register(detailSource, jsplugin.Options{Key: c.Param("key")})
	if err != nil {
		respondTaskPluginCompileError(c, err)
		return
	}
	args := make([]any, len(request.Args))
	for index, raw := range request.Args {
		if err = common.Unmarshal(raw, &args[index]); err != nil {
			writeTaskPluginLocalizedError(c, i18n.MsgPluginInvalidArgument, map[string]any{"Index": index + 1, "Error": err.Error()}, err.Error())
			return
		}
	}
	var output any
	if request.Member == "" {
		output, err = loaded.Engine.Call(context.Background(), request.Hook, args...)
	} else {
		output, err = loaded.Engine.CallMember(context.Background(), request.Hook, request.Member, args...)
	}
	if err != nil {
		respondTaskPluginRuntimeError(c, err)
		return
	}
	common.ApiSuccess(c, output)
}

func DeleteTaskPluginVersion(c *gin.Context) {
	key := c.Param("key")
	version := c.Param("version")
	plugin, lookupErr := model.GetTaskPluginVersion(key, version)
	if lookupErr != nil {
		if errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			writeTaskPluginLocalizedError(c, i18n.MsgPluginOverrideNotFound, nil, "factory plugins cannot be deleted")
			return
		}
		common.ApiError(c, lookupErr)
		return
	}
	inUse, usageErr := model.TaskPluginVersionInUse(key, version)
	if usageErr != nil {
		common.ApiError(c, usageErr)
		return
	}
	if inUse {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgPluginVersionInUse),
		})
		return
	}
	if plugin.Active && !taskPluginHasFactory(key) {
		channels, inFlight, activeUsageErr := model.GetTaskPluginUsage(key)
		if activeUsageErr != nil {
			common.ApiError(c, activeUsageErr)
			return
		}
		if (len(channels) > 0 || inFlight > 0) && c.Query("force") != "true" {
			writeTaskPluginStillInUse(c, channels, inFlight)
			return
		}
	}
	var deleteErr error
	if err := model.WithTaskPluginKeyLock(key, func() error {
		previousVersions, err := model.ListTaskPluginVersions(key)
		if err != nil {
			return err
		}
		_, deleteErr = model.DeleteTaskPluginVersionLocked(key, version)
		if deleteErr != nil {
			return deleteErr
		}
		return syncTaskPluginsOnceWithRollbackLocked(c.Request.Context(), key, previousVersions)
	}); err != nil {
		if errors.Is(deleteErr, model.ErrTaskPluginVersionInUse) {
			c.JSON(http.StatusConflict, gin.H{
				"success": false,
				"message": i18n.T(c, i18n.MsgPluginVersionInUse),
			})
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeTaskPluginLocalizedError(c, i18n.MsgPluginOverrideNotFound, nil, "factory plugins cannot be deleted")
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

type taskPluginActivateRequest struct {
	Version string `json:"version" binding:"required"`
}

func ActivateTaskPlugin(c *gin.Context) {
	var request taskPluginActivateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	versions, err := model.ListTaskPluginVersions(c.Param("key"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var target *model.TaskPlugin
	for i := range versions {
		if versions[i].Version == request.Version {
			target = &versions[i]
			break
		}
	}
	if target == nil {
		common.ApiErrorI18n(c, i18n.MsgPluginVersionNotFound)
		return
	}
	if _, err = jsplugin.NewRegistry().Register(target.Source, jsplugin.Options{Key: target.Key, Version: target.Version}); err != nil {
		respondTaskPluginCompileError(c, err)
		return
	}
	if err = model.WithTaskPluginKeyLock(target.Key, func() error {
		previousVersions, err := model.ListTaskPluginVersions(target.Key)
		if err != nil {
			return err
		}
		if err = model.ActivateTaskPluginLocked(target.Key, target.Version); err != nil {
			return err
		}
		return syncTaskPluginsOnceWithRollbackLocked(c.Request.Context(), target.Key, previousVersions)
	}); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func writeTaskPluginStillInUse(c *gin.Context, channels []model.TaskPluginChannelRef, inFlight int64) {
	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"message": taskPluginLocalizedMessage(c, i18n.MsgPluginStillInUse, nil, "task plugin is still in use"),
		"data": gin.H{
			"channels":        channels,
			"in_flight_count": inFlight,
		},
	})
}

type taskPluginStatusRequest struct {
	Enabled *bool `json:"enabled" binding:"required"`
}

func SetTaskPluginStatus(c *gin.Context) {
	var request taskPluginStatusRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.Enabled == nil {
		common.ApiErrorI18n(c, i18n.MsgPluginEnabledRequired)
		return
	}
	key := c.Param("key")
	cascade := false
	var channels []model.TaskPluginChannelRef
	if !*request.Enabled {
		var inFlight int64
		var usageErr error
		channels, inFlight, usageErr = model.GetTaskPluginUsage(key)
		if usageErr != nil {
			common.ApiError(c, usageErr)
			return
		}
		cascade = c.Query("cascade") == "true"
		force := c.Query("force") == "true"
		if (len(channels) > 0 && !cascade) || (inFlight > 0 && !force) {
			writeTaskPluginStillInUse(c, channels, inFlight)
			return
		}
	}
	_, lookupErr := model.GetTaskPluginVersion(key, "")
	hasActiveOverride := lookupErr == nil
	if lookupErr != nil && !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		common.ApiError(c, lookupErr)
		return
	}
	// Switching a key off must silence every layer that can serve it. The
	// factory built-in goes into the disabled set even when an override row
	// exists; otherwise the built-in would keep routing the same models after
	// the administrator disabled the plugin. Switching on reverses both layers.
	if taskPluginHasFactory(key) {
		keys := setting.GetTaskPluginDisabledFactoryKeys()
		if *request.Enabled {
			next := make([]string, 0, len(keys))
			for _, item := range keys {
				if item != key {
					next = append(next, item)
				}
			}
			keys = next
		} else if !slices.Contains(keys, key) {
			keys = append(append([]string{}, keys...), key)
		}
		encoded, err := setting.EncodeTaskPluginDisabledFactoryKeys(keys)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if err = model.UpdateOption(setting.TaskPluginDisabledFactoryKeysKey, string(encoded)); err != nil {
			common.ApiError(c, err)
			return
		}
		if !hasActiveOverride {
			disabledChannels, unboundChannels, failedChannels := cascadeTaskPluginChannels(key, channels, cascade)
			response := gin.H{"plugin_enabled": *request.Enabled, "disabled_channels": disabledChannels, "unbound_channels": unboundChannels}
			if len(failedChannels) > 0 {
				response["cascade_failed_channels"] = failedChannels
			}
			common.ApiSuccess(c, response)
			return
		}
	}
	if err := model.WithTaskPluginKeyLock(key, func() error {
		previousVersions, err := model.ListTaskPluginVersions(key)
		if err != nil {
			return err
		}
		if err := model.SetTaskPluginEnabledLocked(key, *request.Enabled); err != nil {
			return err
		}
		return syncTaskPluginsOnceWithRollbackLocked(c.Request.Context(), key, previousVersions)
	}); err != nil {
		common.ApiError(c, err)
		return
	}
	disabledChannels, unboundChannels, failedChannels := cascadeTaskPluginChannels(key, channels, cascade)
	response := gin.H{"plugin_enabled": *request.Enabled, "disabled_channels": disabledChannels, "unbound_channels": unboundChannels}
	if len(failedChannels) > 0 {
		response["cascade_failed_channels"] = failedChannels
	}
	common.ApiSuccess(c, response)
}

func taskPluginHasFactory(key string) bool {
	for _, meta := range jsplugin.DefaultRegistry.Snapshot().Factory {
		if meta.Key == key {
			return true
		}
	}
	return false
}

var updateTaskPluginChannelStatus = model.UpdateChannelStatus

func cascadeTaskPluginChannels(key string, channels []model.TaskPluginChannelRef, cascade bool) (int, int, []int) {
	if !cascade {
		return 0, 0, nil
	}
	disabledChannels := 0
	unboundChannels := 0
	failedChannels := make([]int, 0)
	for _, channel := range channels {
		if channel.Type == constant.ChannelTypeNewAPI {
			changed, err := model.UnbindTaskPlugin(channel.Id, key)
			if err != nil || !changed {
				failedChannels = append(failedChannels, channel.Id)
				continue
			}
			unboundChannels++
			continue
		}
		if updateTaskPluginChannelStatus(channel.Id, "", common.ChannelStatusManuallyDisabled, "task plugin disabled") {
			disabledChannels++
		} else {
			failedChannels = append(failedChannels, channel.Id)
		}
	}
	if unboundChannels > 0 {
		model.InitChannelCache()
	}
	return disabledChannels, unboundChannels, failedChannels
}

func GetTaskPluginMarketplaceSources(c *gin.Context) {
	common.ApiSuccess(c, setting.GetTaskPluginMarketplaceSources())
}

func UpdateTaskPluginMarketplaceSources(c *gin.Context) {
	var sources []setting.TaskPluginMarketplaceSource
	if err := c.ShouldBindJSON(&sources); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if sources == nil {
		sources = []setting.TaskPluginMarketplaceSource{}
	}
	for i := range sources {
		name := strings.TrimSpace(sources[i].Name)
		indexURL := strings.TrimSpace(sources[i].IndexURL)
		if name == "" {
			writeTaskPluginLocalizedError(c, i18n.MsgPluginMarketplaceNameRequired, nil, "marketplace source name is required")
			return
		}
		parsed, err := url.Parse(indexURL)
		if err != nil || !parsed.IsAbs() || parsed.Host == "" || (!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) {
			writeTaskPluginLocalizedError(c, i18n.MsgPluginMarketplaceURLInvalid, nil, "marketplace source index_url must be an absolute http(s) URL")
			return
		}
		sources[i].Name = name
		sources[i].IndexURL = indexURL
	}
	encoded, err := common.Marshal(sources)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err = model.UpdateOption(setting.TaskPluginMarketplaceSourcesKey, string(encoded)); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, sources)
}

func GetTaskPluginOptions(c *gin.Context) {
	snapshot := jsplugin.DefaultRegistry.Snapshot()
	seen := make(map[string]bool)
	listed := make([]jsplugin.Meta, 0, len(snapshot.Factory)+len(snapshot.Override))
	for layer, metas := range [][]jsplugin.Meta{snapshot.Override, snapshot.Factory} {
		for _, meta := range metas {
			if seen[meta.Key] {
				continue
			}
			// Disabled factory keys are omitted from bind options. The disabled
			// set suppresses only the factory fallback; an enabled override for
			// the same key is listed in the override pass and still appears.
			if layer == 1 && setting.IsTaskPluginFactoryDisabled(meta.Key) {
				continue
			}
			if _, ok := jsplugin.DefaultRegistry.Get(meta.Key); !ok {
				continue
			}
			seen[meta.Key] = true
			listed = append(listed, meta)
		}
	}
	sortTaskPluginBindOptions(listed)
	options := make([]gin.H, 0, len(listed))
	for _, meta := range listed {
		hasIcon := false
		if plugin, err := model.GetTaskPluginVersion(meta.Key, ""); err == nil {
			hasIcon = plugin.HasIcon()
		}
		if !hasIcon {
			hasIcon = factoryPluginHasIcon(meta.Key)
		}
		options = append(options, gin.H{
			"key":           meta.Key,
			"name":          meta.Name,
			"description":   meta.Description,
			"icon":          meta.Icon,
			"hasIcon":       hasIcon,
			"baseUrl":       meta.BaseURL,
			"website":       meta.Website,
			"sortPriority":  meta.SortPriority,
			"channelTypes":  meta.ChannelTypes,
			"upstreams":     meta.Upstreams,
			"models":        meta.Models,
			"usageSchema":   meta.UsageSchema,
			"usageProfiles": meta.UsageProfiles,
		})
	}
	common.ApiSuccess(c, options)
}

func sortTaskPluginBindOptions(metas []jsplugin.Meta) {
	sort.SliceStable(metas, func(i, j int) bool {
		if metas[i].SortPriority != metas[j].SortPriority {
			return metas[i].SortPriority > metas[j].SortPriority
		}
		return metas[i].Key < metas[j].Key
	})
}

var taskPluginSyncState = struct {
	sync.Mutex
	hashes      map[string]string
	errors      map[string]string
	lastRebuild taskPluginRebuildOutcome
}{hashes: map[string]string{}, errors: map[string]string{}}

func syncTaskPluginsOnce() error {
	return syncTaskPluginsOnceContext(context.Background())
}

func syncTaskPluginsOnceWithRollback(ctx context.Context, key string, previousVersions []model.TaskPlugin) error {
	if err := syncTaskPluginsOnceContext(ctx); err != nil {
		if rollbackErr := model.RestoreTaskPluginVersions(key, previousVersions); rollbackErr != nil {
			return fmt.Errorf("%w; rollback task plugin versions failed: %v", err, rollbackErr)
		}
		return err
	}
	return nil
}

func syncTaskPluginsOnceWithRollbackLocked(ctx context.Context, key string, previousVersions []model.TaskPlugin) error {
	if err := syncTaskPluginsOnceContext(ctx); err != nil {
		if rollbackErr := model.RestoreTaskPluginVersionsLocked(key, previousVersions); rollbackErr != nil {
			return fmt.Errorf("%w; rollback task plugin versions failed: %v", err, rollbackErr)
		}
		return err
	}
	return nil
}

func syncTaskPluginsOnceContext(ctx context.Context) error {
	started := time.Now()
	taskPluginSyncState.Lock()
	defer taskPluginSyncState.Unlock()
	databaseSnapshot, err := model.GetTaskPluginSyncSnapshot()
	if err != nil {
		syncErr := fmt.Errorf("sync task plugins: %w", err)
		taskPluginSyncState.lastRebuild = taskPluginRebuildOutcome{
			Status:           "failed",
			AttemptedAt:      time.Now(),
			Generation:       jsplugin.DefaultRegistry.Generation().Number,
			DatabaseRevision: taskPluginSyncState.lastRebuild.DatabaseRevision,
			Error:            syncErr.Error(),
		}
		logger.LogDebug(
			ctx,
			"task_plugin subsystem=sync event=failed stage=database_snapshot retained_generation=%d elapsed_ms=%d",
			jsplugin.DefaultRegistry.Generation().Number,
			time.Since(started).Milliseconds(),
		)
		return syncErr
	}
	databasePlugins := databaseSnapshot.Plugins
	sort.Slice(databasePlugins, func(i, j int) bool { return databasePlugins[i].Key < databasePlugins[j].Key })
	currentOverrides := jsplugin.DefaultRegistry.OverridePlugins()
	generationBefore := jsplugin.DefaultRegistry.Generation().Number
	logger.LogDebug(
		ctx,
		"task_plugin subsystem=sync event=start database_revision=%q generation=%d desired_plugins=%d current_overrides=%d",
		databaseSnapshot.Revision,
		generationBefore,
		len(databasePlugins),
		len(currentOverrides),
	)
	nextOverrides := make([]*jsplugin.LoadedPlugin, 0, len(databasePlugins))
	nextHashes := make(map[string]string, len(databasePlugins))
	seen := make(map[string]bool, len(databasePlugins))
	for _, plugin := range databasePlugins {
		seen[plugin.Key] = true
		if current := currentOverrides[plugin.Key]; current != nil && taskPluginSyncState.hashes[plugin.Key] == plugin.SourceHash {
			nextOverrides = append(nextOverrides, current)
			nextHashes[plugin.Key] = plugin.SourceHash
			delete(taskPluginSyncState.errors, plugin.Key)
			logger.LogDebug(
				ctx,
				"task_plugin subsystem=sync event=plugin plugin=%q version=%q action=reuse",
				plugin.Key,
				plugin.Version,
			)
			continue
		}
		logger.LogDebug(
			ctx,
			"task_plugin subsystem=sync event=plugin plugin=%q version=%q action=compile_start",
			plugin.Key,
			plugin.Version,
		)
		compiled, compileErr := jsplugin.CompilePlugin(plugin.Source, jsplugin.Options{Key: plugin.Key, Version: plugin.Version})
		if compileErr != nil {
			retainedIncumbent := false
			if current := currentOverrides[plugin.Key]; current != nil {
				nextOverrides = append(nextOverrides, current)
				retainedIncumbent = true
				if currentHash := taskPluginSyncState.hashes[plugin.Key]; currentHash != "" {
					nextHashes[plugin.Key] = currentHash
				}
			}
			taskPluginSyncState.errors[plugin.Key] = compileErr.Error()
			common.SysError(fmt.Sprintf("compile task plugin %s@%s: %v", plugin.Key, plugin.Version, compileErr))
			logger.LogDebug(
				ctx,
				"task_plugin subsystem=sync event=plugin plugin=%q version=%q action=compile_failed retained_incumbent=%t",
				plugin.Key,
				plugin.Version,
				retainedIncumbent,
			)
			continue
		}
		nextOverrides = append(nextOverrides, compiled)
		nextHashes[plugin.Key] = plugin.SourceHash
		delete(taskPluginSyncState.errors, plugin.Key)
		logger.LogDebug(
			ctx,
			"task_plugin subsystem=sync event=plugin plugin=%q version=%q action=compile_success",
			plugin.Key,
			plugin.Version,
		)
	}
	if err = jsplugin.DefaultRegistry.ReplaceOverrides(nextOverrides); err != nil {
		syncErr := fmt.Errorf("publish task plugin generation: %w", err)
		taskPluginSyncState.lastRebuild = taskPluginRebuildOutcome{
			Status:           "failed",
			AttemptedAt:      time.Now(),
			Generation:       jsplugin.DefaultRegistry.Generation().Number,
			DatabaseRevision: databaseSnapshot.Revision,
			Error:            syncErr.Error(),
		}
		logger.LogDebug(
			ctx,
			"task_plugin subsystem=sync event=failed stage=publish retained_generation=%d retained_generation_active=true database_revision=%q elapsed_ms=%d",
			jsplugin.DefaultRegistry.Generation().Number,
			databaseSnapshot.Revision,
			time.Since(started).Milliseconds(),
		)
		return syncErr
	}
	taskPluginSyncState.hashes = nextHashes
	for key := range taskPluginSyncState.errors {
		if !seen[key] {
			delete(taskPluginSyncState.errors, key)
		}
	}
	pluginErrors := jsplugin.DefaultRegistry.RoutingErrors()
	for key, message := range taskPluginSyncState.errors {
		pluginErrors[key] = message
	}
	pluginErrorCount := len(pluginErrors)
	status := "success"
	if pluginErrorCount > 0 {
		status = "partial"
	}
	taskPluginSyncState.lastRebuild = taskPluginRebuildOutcome{
		Status:           status,
		AttemptedAt:      time.Now(),
		Generation:       jsplugin.DefaultRegistry.Generation().Number,
		DatabaseRevision: databaseSnapshot.Revision,
		PluginErrorCount: pluginErrorCount,
	}
	logger.LogDebug(
		ctx,
		"task_plugin subsystem=sync event=complete database_revision=%q previous_generation=%d generation=%d status=%q active_overrides=%d plugin_errors=%d elapsed_ms=%d",
		databaseSnapshot.Revision,
		generationBefore,
		jsplugin.DefaultRegistry.Generation().Number,
		status,
		len(jsplugin.DefaultRegistry.ActiveOverridePlugins()),
		pluginErrorCount,
		time.Since(started).Milliseconds(),
	)
	return nil
}

func SyncTaskPluginsOnce() {
	if err := syncTaskPluginsOnce(); err != nil {
		common.SysError(err.Error())
	}
}

func taskPluginLocalizedMessage(c *gin.Context, key string, data map[string]any, diagnostic string) string {
	message := i18n.T(c, key, data)
	diagnostic = strings.TrimSpace(diagnostic)
	if diagnostic == "" {
		return message
	}
	if message == "" {
		return diagnostic
	}
	if strings.Contains(strings.ToLower(message), strings.ToLower(diagnostic)) {
		return message
	}
	// An explicit language already has a catalog sentence. Do not append the
	// English diagnostic. Requests without a language still need it so older
	// callers can match the English error text.
	if taskPluginLanguageExplicit(c) && message != key {
		return message
	}
	return message + ": " + diagnostic
}

func taskPluginLanguageExplicit(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	return strings.TrimSpace(c.GetHeader("Accept-Language")) != "" || strings.TrimSpace(c.Query("lang")) != ""
}

func writeTaskPluginLocalizedError(c *gin.Context, key string, data map[string]any, diagnostic string) {
	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"message": taskPluginLocalizedMessage(c, key, data, diagnostic),
	})
}

func respondTaskPluginCompileError(c *gin.Context, err error) {
	var unknown *jsplugin.UnknownMetaFieldError
	if errors.As(err, &unknown) {
		common.ApiErrorI18n(c, i18n.MsgTaskPluginUnknownMetaField, map[string]any{"Field": unknown.Field})
		return
	}
	if key, data, ok := taskPluginConflictMessage(err); ok {
		writeTaskPluginLocalizedError(c, key, data, err.Error())
		return
	}
	writeTaskPluginLocalizedError(c, i18n.MsgPluginCompileFailed, map[string]any{"Error": err.Error()}, err.Error())
}

func respondTaskPluginRuntimeError(c *gin.Context, err error) {
	writeTaskPluginLocalizedError(c, i18n.MsgPluginRuntimeFailed, map[string]any{"Error": err.Error()}, err.Error())
}

func SyncTaskPlugins() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		SyncTaskPluginsOnce()
	}
}
