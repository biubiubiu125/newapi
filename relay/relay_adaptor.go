package relay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	_ "github.com/QuantumNous/new-api/plugins"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/advancedcustom"
	"github.com/QuantumNous/new-api/relay/channel/ali"
	"github.com/QuantumNous/new-api/relay/channel/aws"
	"github.com/QuantumNous/new-api/relay/channel/baidu"
	"github.com/QuantumNous/new-api/relay/channel/baidu_v2"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	"github.com/QuantumNous/new-api/relay/channel/cloudflare"
	"github.com/QuantumNous/new-api/relay/channel/codex"
	"github.com/QuantumNous/new-api/relay/channel/cohere"
	"github.com/QuantumNous/new-api/relay/channel/coze"
	"github.com/QuantumNous/new-api/relay/channel/deepseek"
	"github.com/QuantumNous/new-api/relay/channel/dify"
	"github.com/QuantumNous/new-api/relay/channel/gemini"
	"github.com/QuantumNous/new-api/relay/channel/jimeng"
	"github.com/QuantumNous/new-api/relay/channel/jina"
	"github.com/QuantumNous/new-api/relay/channel/minimax"
	"github.com/QuantumNous/new-api/relay/channel/mistral"
	"github.com/QuantumNous/new-api/relay/channel/mokaai"
	"github.com/QuantumNous/new-api/relay/channel/moonshot"
	"github.com/QuantumNous/new-api/relay/channel/newapi"
	"github.com/QuantumNous/new-api/relay/channel/ollama"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	"github.com/QuantumNous/new-api/relay/channel/palm"
	"github.com/QuantumNous/new-api/relay/channel/perplexity"
	"github.com/QuantumNous/new-api/relay/channel/replicate"
	"github.com/QuantumNous/new-api/relay/channel/siliconflow"
	"github.com/QuantumNous/new-api/relay/channel/sub2api"
	"github.com/QuantumNous/new-api/relay/channel/submodel"
	taskali "github.com/QuantumNous/new-api/relay/channel/task/ali"
	taskdoubao "github.com/QuantumNous/new-api/relay/channel/task/doubao"
	taskGemini "github.com/QuantumNous/new-api/relay/channel/task/gemini"
	"github.com/QuantumNous/new-api/relay/channel/task/hailuo"
	taskjimeng "github.com/QuantumNous/new-api/relay/channel/task/jimeng"
	jspluginadaptor "github.com/QuantumNous/new-api/relay/channel/task/jsplugin"
	"github.com/QuantumNous/new-api/relay/channel/task/kling"
	tasksora "github.com/QuantumNous/new-api/relay/channel/task/sora"
	"github.com/QuantumNous/new-api/relay/channel/task/suno"
	taskvertex "github.com/QuantumNous/new-api/relay/channel/task/vertex"
	taskVidu "github.com/QuantumNous/new-api/relay/channel/task/vidu"
	"github.com/QuantumNous/new-api/relay/channel/tencent"
	"github.com/QuantumNous/new-api/relay/channel/vertex"
	"github.com/QuantumNous/new-api/relay/channel/volcengine"
	"github.com/QuantumNous/new-api/relay/channel/xai"
	"github.com/QuantumNous/new-api/relay/channel/xunfei"
	"github.com/QuantumNous/new-api/relay/channel/zhipu"
	"github.com/QuantumNous/new-api/relay/channel/zhipu_4v"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	taskdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GetAdaptor(apiType int) channel.Adaptor {
	switch apiType {
	case constant.APITypeAli:
		return &ali.Adaptor{}
	case constant.APITypeAnthropic:
		return &claude.Adaptor{}
	case constant.APITypeBaidu:
		return &baidu.Adaptor{}
	case constant.APITypeGemini:
		return &gemini.Adaptor{}
	case constant.APITypeOpenAI:
		return &openai.Adaptor{}
	case constant.APITypePaLM:
		return &palm.Adaptor{}
	case constant.APITypeTencent:
		return &tencent.DispatchAdaptor{}
	case constant.APITypeXunfei:
		return &xunfei.Adaptor{}
	case constant.APITypeZhipu:
		return &zhipu.Adaptor{}
	case constant.APITypeZhipuV4:
		return &zhipu_4v.Adaptor{}
	case constant.APITypeOllama:
		return &ollama.Adaptor{}
	case constant.APITypePerplexity:
		return &perplexity.Adaptor{}
	case constant.APITypeAws:
		return &aws.Adaptor{}
	case constant.APITypeCohere:
		return &cohere.Adaptor{}
	case constant.APITypeDify:
		return &dify.Adaptor{}
	case constant.APITypeJina:
		return &jina.Adaptor{}
	case constant.APITypeCloudflare:
		return &cloudflare.Adaptor{}
	case constant.APITypeSiliconFlow:
		return &siliconflow.Adaptor{}
	case constant.APITypeVertexAi:
		return &vertex.Adaptor{}
	case constant.APITypeMistral:
		return &mistral.Adaptor{}
	case constant.APITypeDeepSeek:
		return &deepseek.Adaptor{}
	case constant.APITypeMokaAI:
		return &mokaai.Adaptor{}
	case constant.APITypeVolcEngine:
		return &volcengine.Adaptor{}
	case constant.APITypeBaiduV2:
		return &baidu_v2.Adaptor{}
	case constant.APITypeOpenRouter:
		return &openai.Adaptor{}
	case constant.APITypeXinference:
		return &openai.Adaptor{}
	case constant.APITypeXai:
		return &xai.Adaptor{}
	case constant.APITypeCoze:
		return &coze.Adaptor{}
	case constant.APITypeJimeng:
		return &jimeng.Adaptor{}
	case constant.APITypeMoonshot:
		return &moonshot.Adaptor{} // Moonshot uses Claude API
	case constant.APITypeSubmodel:
		return &submodel.Adaptor{}
	case constant.APITypeMiniMax:
		return &minimax.Adaptor{}
	case constant.APITypeReplicate:
		return &replicate.Adaptor{}
	case constant.APITypeCodex:
		return &codex.Adaptor{}
	case constant.APITypeAdvancedCustom:
		return &advancedcustom.Adaptor{}
	case constant.APITypeSub2API:
		return &sub2api.Adaptor{}
	case constant.APITypeNewAPI:
		return &newapi.Adaptor{}
	}
	return nil
}

func GetTaskPlatform(c *gin.Context) constant.TaskPlatform {
	if pluginKey := c.GetString("task_plugin_key"); pluginKey != "" {
		return constant.TaskPlatform(pluginKey)
	}
	channelType := c.GetInt("channel_type")
	if channelType > 0 {
		return constant.TaskPlatform(strconv.Itoa(channelType))
	}
	return constant.TaskPlatform(c.GetString("platform"))
}

var taskPluginKeys = map[constant.TaskPlatform]string{
	constant.TaskPlatformSuno:                                            "sunoapi",
	constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeAli)):         "alibaba",
	constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeKling)):       "kling",
	constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeJimeng)):      "jimeng",
	constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeVidu)):        "vidu",
	constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeDoubaoVideo)): "doubao",
	constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeVolcEngine)):  "doubao",
	constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeGemini)):      "google",
	constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeMiniMax)):     "hailuo",
	constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeSora)):        "sora",
	constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeOpenAI)):      "sora",
	constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeNewAPI)):      "sora",
	constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeVertexAi)):    "vertex-ai",
}

func ResolveTaskPluginForPlatform(generation *pluginruntime.RoutingGeneration, platform constant.TaskPlatform) (*pluginruntime.LoadedPlugin, bool) {
	if generation == nil {
		return nil, false
	}
	if key, ok := taskPluginKeys[platform]; ok {
		if plugin, found := generation.Get(key); found {
			return plugin, true
		}
	}
	return generation.Get(string(platform))
}

// TaskPlatformUnavailableError explains why no adaptor serves the platform:
// the task-plugin system is switched off, the resolved plugin is disabled,
// or the platform simply names nothing. The distinction is user-actionable,
// so it must survive into the client-facing message.
func TaskPlatformUnavailableError(platform constant.TaskPlatform) (string, string) {
	if !pluginruntime.DefaultRegistry.Enabled() {
		return "task_plugin_system_disabled", "the task plugin system is disabled on this gateway"
	}
	key := string(platform)
	if mapped, ok := taskPluginKeys[platform]; ok {
		key = mapped
	}
	for _, meta := range pluginruntime.DefaultRegistry.Snapshot().Factory {
		if meta.Key == key {
			return "task_plugin_disabled", fmt.Sprintf("task plugin %q is disabled on this gateway", key)
		}
	}
	return "invalid_api_platform", fmt.Sprintf("invalid api platform: %s", platform)
}

func GetTaskAdaptor(platform constant.TaskPlatform) channel.TaskAdaptor {
	// Factory plugins own usage facts and provider validation. Keep the Go
	// adaptors only when that plugin is not loaded.
	if plugin, ok := ResolveTaskPluginForPlatform(pluginruntime.DefaultRegistry.Generation(), platform); ok {
		return jspluginadaptor.New(plugin)
	}
	return legacyTaskAdaptor(platform)
}

func legacyTaskAdaptor(platform constant.TaskPlatform) channel.TaskAdaptor {
	switch platform {
	case constant.TaskPlatformSuno:
		return newLegacyTaskAdaptor(&suno.TaskAdaptor{})
	}
	if channelType, err := strconv.ParseInt(string(platform), 10, 64); err == nil {
		switch channelType {
		case constant.ChannelTypeAli:
			return newLegacyTaskAdaptor(&taskali.TaskAdaptor{})
		case constant.ChannelTypeKling:
			return newLegacyTaskAdaptor(&kling.TaskAdaptor{})
		case constant.ChannelTypeJimeng:
			return newLegacyTaskAdaptor(&taskjimeng.TaskAdaptor{})
		case constant.ChannelTypeVertexAi:
			return newLegacyTaskAdaptor(&taskvertex.TaskAdaptor{})
		case constant.ChannelTypeVidu:
			return newLegacyTaskAdaptor(&taskVidu.TaskAdaptor{})
		case constant.ChannelTypeDoubaoVideo, constant.ChannelTypeVolcEngine:
			return newLegacyTaskAdaptor(&taskdoubao.TaskAdaptor{})
		case constant.ChannelTypeSora, constant.ChannelTypeOpenAI, constant.ChannelTypeNewAPI:
			return newLegacyTaskAdaptor(&tasksora.TaskAdaptor{})
		case constant.ChannelTypeGemini:
			return newLegacyTaskAdaptor(&taskGemini.TaskAdaptor{})
		case constant.ChannelTypeMiniMax:
			return newLegacyTaskAdaptor(&hailuo.TaskAdaptor{})
		}
	}
	return nil
}

// legacyBuiltinTaskAdaptor is the provider contract still implemented by the
// built-in video adaptors. The host task interface now uses ParseResponse and
// a task object; this bridge keeps those providers working.
type legacyBuiltinTaskAdaptor interface {
	Init(info *relaycommon.RelayInfo)
	ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *taskdto.TaskError
	BuildRequestURL(info *relaycommon.RelayInfo) (string, error)
	BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error
	BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error)
	DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error)
	DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, err *taskdto.TaskError)
	GetModelList() []string
	GetChannelName() string
	FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error)
	ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error)
}

type legacyTaskAdaptorBridge struct {
	inner legacyBuiltinTaskAdaptor
}

func newLegacyTaskAdaptor(inner legacyBuiltinTaskAdaptor) channel.TaskAdaptor {
	return legacyTaskAdaptorBridge{inner: inner}
}

func (a legacyTaskAdaptorBridge) Init(info *relaycommon.RelayInfo) {
	a.inner.Init(info)
}

func (a legacyTaskAdaptorBridge) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *taskdto.TaskError {
	return a.inner.ValidateRequestAndSetAction(c, info)
}

func (a legacyTaskAdaptorBridge) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	estimator, ok := a.inner.(interface {
		EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64
	})
	if !ok {
		return nil
	}
	return estimator.EstimateBilling(c, info)
}

func (a legacyTaskAdaptorBridge) AdjustBillingOnSubmit(info *relaycommon.RelayInfo, taskData []byte) map[string]float64 {
	adjuster, ok := a.inner.(interface {
		AdjustBillingOnSubmit(info *relaycommon.RelayInfo, taskData []byte) map[string]float64
	})
	if !ok {
		return nil
	}
	return adjuster.AdjustBillingOnSubmit(info, taskData)
}

func (a legacyTaskAdaptorBridge) AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int {
	adjuster, ok := a.inner.(interface {
		AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int
	})
	if !ok {
		return 0
	}
	return adjuster.AdjustBillingOnComplete(task, taskResult)
}

func (a legacyTaskAdaptorBridge) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return a.inner.BuildRequestURL(info)
}

func (a legacyTaskAdaptorBridge) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	return a.inner.BuildRequestHeader(c, req, info)
}

func (a legacyTaskAdaptorBridge) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	return a.inner.BuildRequestBody(c, info)
}

func (a legacyTaskAdaptorBridge) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return a.inner.DoRequest(c, info, requestBody)
}

func (a legacyTaskAdaptorBridge) ParseResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*channel.TaskSubmitResponse, *taskdto.TaskError) {
	upstreamTaskID, taskData, taskErr := a.inner.DoResponse(c, resp, info)
	if taskErr != nil {
		return nil, taskErr
	}
	return &channel.TaskSubmitResponse{UpstreamTaskID: upstreamTaskID, TaskData: taskData}, nil
}

func (a legacyTaskAdaptorBridge) GetModelList() []string {
	return a.inner.GetModelList()
}

func (a legacyTaskAdaptorBridge) GetChannelName() string {
	return a.inner.GetChannelName()
}

func (a legacyTaskAdaptorBridge) FetchTask(baseURL, key string, task *model.Task, proxy string) (*http.Response, error) {
	body := map[string]any{}
	if task != nil {
		body["task_id"] = task.GetUpstreamTaskID()
		body["action"] = task.Action
	}
	return a.inner.FetchTask(baseURL, key, body, proxy)
}

func (a legacyTaskAdaptorBridge) FetchTaskContext(ctx context.Context, baseURL, key string, task *model.Task, proxy string) (*http.Response, error) {
	contextAdaptor, ok := a.inner.(interface {
		FetchTaskContext(context.Context, string, string, map[string]any, string) (*http.Response, error)
	})
	if !ok {
		return a.FetchTask(baseURL, key, task, proxy)
	}
	body := map[string]any{}
	if task != nil {
		body["task_id"] = task.GetUpstreamTaskID()
		body["action"] = task.Action
	}
	return contextAdaptor.FetchTaskContext(ctx, baseURL, key, body, proxy)
}

func (a legacyTaskAdaptorBridge) ParseTaskResult(_ *model.Task, _ *http.Response, respBody []byte) (*relaycommon.TaskInfo, error) {
	return a.inner.ParseTaskResult(respBody)
}

// GetLegacyTaskPollingAdaptor returns the built-in provider adaptor for tasks
// that never recorded a plugin execution snapshot. A loaded JS plugin must not
// hide that adaptor, or an in-flight Kling/Jimeng/Sora/Suno task stops being
// polled and is refunded after repeated empty polls. Snapshot tasks use the
// plugin factory and do not come through here.
func GetLegacyTaskPollingAdaptor(platform constant.TaskPlatform) service.TaskPollingAdaptor {
	adaptor := legacyTaskAdaptor(platform)
	bridge, ok := adaptor.(legacyTaskAdaptorBridge)
	if !ok || bridge.inner == nil {
		return nil
	}
	return legacyPollingAdaptor{inner: bridge.inner}
}

type legacyPollingAdaptor struct {
	inner legacyBuiltinTaskAdaptor
}

func (a legacyPollingAdaptor) Init(info *relaycommon.RelayInfo) {
	a.inner.Init(info)
}

func (a legacyPollingAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	return a.inner.FetchTask(baseURL, key, body, proxy)
}

func (a legacyPollingAdaptor) ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error) {
	return a.inner.ParseTaskResult(body)
}

func (a legacyPollingAdaptor) AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int {
	adjuster, ok := a.inner.(interface {
		AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int
	})
	if !ok {
		return 0
	}
	return adjuster.AdjustBillingOnComplete(task, taskResult)
}

func GetTaskPluginAdaptor(platform constant.TaskPlatform) channel.TaskPluginAdaptor {
	plugin, ok := ResolveTaskPluginForPlatform(pluginruntime.DefaultRegistry.Generation(), platform)
	if !ok {
		return nil
	}
	return jspluginadaptor.New(plugin)
}

// ResolveTaskPluginForTask resolves the exact plugin version recorded in the
// task execution snapshot. A task must not silently switch to a newer runtime
// plugin after an upstream update. The current generation is used when it
// still contains the requested version; otherwise the immutable database
// override is compiled as an isolated adaptor.
func ResolveTaskPluginForTask(task *model.Task) (*pluginruntime.LoadedPlugin, *pluginruntime.RoutingGeneration, error) {
	if task == nil {
		return nil, nil, errors.New("task is nil")
	}
	key := strings.TrimSpace(string(task.Platform))
	version := ""
	generationNumber := uint64(0)
	snapshotSource := ""
	if execution := task.PrivateData.Execution; execution != nil && execution.TaskPlugin != nil {
		key = strings.TrimSpace(execution.TaskPlugin.Key)
		version = strings.TrimSpace(execution.TaskPlugin.Version)
		generationNumber = execution.TaskPlugin.Generation
		snapshotSource = execution.TaskPlugin.Source
	}
	if key == "" {
		return nil, nil, errors.New("task plugin key is missing")
	}

	generation := pluginruntime.DefaultRegistry.Generation()
	if plugin, ok := ResolveTaskPluginForPlatform(generation, constant.TaskPlatform(key)); ok &&
		(version == "" || plugin.Meta.Version == version) &&
		(snapshotSource == "" || plugin.Source == snapshotSource) {
		// A generation mismatch is acceptable when the registry still exposes
		// the same immutable key/version and source. A task captured with a
		// different source keeps that snapshot even if the version string did
		// not change.
		_ = generationNumber
		return plugin, generation, nil
	}

	if version == "" {
		return nil, nil, fmt.Errorf("task plugin %q is not available", key)
	}
	if strings.TrimSpace(snapshotSource) != "" {
		plugin, compileErr := pluginruntime.CompilePlugin(snapshotSource, pluginruntime.Options{
			Key: key, Version: version,
		})
		if compileErr != nil {
			return nil, nil, fmt.Errorf("compile task plugin %s@%s snapshot: %w", key, version, compileErr)
		}
		if plugin.Meta.Key != key || plugin.Meta.Version != version {
			return nil, nil, fmt.Errorf("task plugin snapshot identity mismatch for %s@%s", key, version)
		}
		return plugin, nil, nil
	}
	persisted, persistedErr := model.GetTaskPluginVersion(key, version)
	if persistedErr == nil {
		plugin, compileErr := pluginruntime.CompilePlugin(persisted.Source, pluginruntime.Options{
			Key:     persisted.Key,
			Version: persisted.Version,
		})
		if compileErr == nil {
			return plugin, nil, nil
		}
		persistedErr = fmt.Errorf("compile persisted task plugin: %w", compileErr)
	}

	// A captured version that was never stored still uses the registered
	// factory plugin. Tests and older tasks pin 1.0.0 after the factory moved
	// to 1.0.1; there is no snapshot source to compile instead.
	if persistedErr != nil && strings.TrimSpace(snapshotSource) == "" && taskPluginVersionUnavailable(persistedErr) {
		if plugin, ok := ResolveTaskPluginForPlatform(generation, constant.TaskPlatform(key)); ok {
			return plugin, generation, nil
		}
	}
	if persistedErr != nil && taskPluginStoreErrorIsTransient(persistedErr) {
		return nil, nil, &model.TaskPluginTemporarilyUnavailableError{
			Err: fmt.Errorf("load task plugin %s@%s: %w", key, version, persistedErr),
		}
	}
	if persistedErr == nil {
		persistedErr = errors.New("task plugin source is unavailable")
	}
	return nil, nil, fmt.Errorf("load task plugin %s@%s: %w", key, version, persistedErr)
}

func taskPluginVersionUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no such table") || strings.Contains(message, "does not exist")
}

// taskPluginStoreErrorIsTransient reports a plugin-row read that failed for a
// reason other than "this version does not exist" or a bad stored source.
// Those failures must be retried; treating them as a missing plugin refunds
// an upstream task that may still be generating.
func taskPluginStoreErrorIsTransient(err error) bool {
	if err == nil || taskPluginVersionUnavailable(err) {
		return false
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "compile") {
		return false
	}
	for _, marker := range []string{
		"database is closed",
		"bad connection",
		"connection refused",
		"connection reset",
		"connection timed out",
		"broken pipe",
		"i/o timeout",
		"context deadline exceeded",
		"too many connections",
		"too many clients",
		"remaining connection slots",
		"server closed",
		"unexpected eof",
		"database is locked",
		"deadlock",
		"dial tcp",
		"network is unreachable",
		"database system is starting up",
		"database system is shutting down",
		"sqlstate 08",
		"sqlstate 40",
		"sqlstate 53",
		"sqlstate 57",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

// GetTaskPluginAdaptorForTask is the task-lifecycle counterpart to
// GetTaskPluginAdaptor. It preserves the plugin identity captured at
// submission time for polling, artifact projection, and response retrieval.
func GetTaskPluginAdaptorForTask(task *model.Task) (channel.TaskPluginAdaptor, error) {
	plugin, _, err := ResolveTaskPluginForTask(task)
	if err != nil {
		return nil, err
	}
	return jspluginadaptor.New(plugin), nil
}

func getTaskAdaptorForRequest(c *gin.Context, platform constant.TaskPlatform) (constant.TaskPlatform, channel.TaskPluginAdaptor) {
	if c != nil {
		if value, exists := c.Get(pluginruntime.ContextKeyPinnedPlugin); exists {
			if pinned, ok := value.(pluginruntime.PinnedPlugin); ok && pinned.Plugin != nil {
				platform = constant.TaskPlatform(pinned.Plugin.Meta.Key)
				return platform, jspluginadaptor.New(pinned.Plugin)
			}
			return platform, nil
		}
		if value, exists := c.Get(pluginruntime.ContextKeyPinnedEndpoint); exists {
			if pinned, ok := value.(pluginruntime.PinnedEndpoint); ok && pinned.Plugin != nil {
				platform = constant.TaskPlatform(pinned.Plugin.Meta.Key)
				return platform, jspluginadaptor.New(pinned.Plugin)
			}
			return platform, nil
		}
		if value, exists := c.Get(pluginruntime.ContextKeyPinnedRoute); exists {
			if pinned, ok := value.(pluginruntime.PinnedRoute); ok && pinned.Plugin != nil {
				platform = constant.TaskPlatform(pinned.Plugin.Meta.Key)
				return platform, jspluginadaptor.New(pinned.Plugin)
			}
			return platform, nil
		}
	}
	generation := pluginruntime.DefaultRegistry.Generation()
	if plugin, ok := ResolveTaskPluginForPlatform(generation, platform); ok {
		if c != nil {
			c.Set(pluginruntime.ContextKeyPinnedPlugin, pluginruntime.PinnedPlugin{
				Generation: generation,
				Plugin:     plugin,
			})
		}
		return platform, jspluginadaptor.New(plugin)
	}
	legacy := GetTaskAdaptor(platform)
	if legacy == nil {
		return platform, nil
	}
	return platform, legacy
}
