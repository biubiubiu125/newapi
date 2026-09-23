package model

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	commonRelay "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TaskStatus string

var ErrTaskConcurrentUpdate = errors.New("task was concurrently updated")

func (t TaskStatus) ToVideoStatus() string {
	var status string
	switch t {
	case TaskStatusQueued, TaskStatusSubmitted:
		status = dto.VideoStatusQueued
	case TaskStatusInProgress:
		status = dto.VideoStatusInProgress
	case TaskStatusSuccess:
		status = dto.VideoStatusCompleted
	case TaskStatusFailure:
		status = dto.VideoStatusFailed
	default:
		status = dto.VideoStatusUnknown // Default fallback
	}
	return status
}

const (
	TaskStatusNotStart   TaskStatus = "NOT_START"
	TaskStatusSubmitted             = "SUBMITTED"
	TaskStatusQueued                = "QUEUED"
	TaskStatusInProgress            = "IN_PROGRESS"
	TaskStatusFailure               = "FAILURE"
	TaskStatusSuccess               = "SUCCESS"
	TaskStatusUnknown               = "UNKNOWN"

	TaskSettlementStatusPending = "PENDING"
	TaskSettlementStatusApplied = "APPLIED"
	TaskSettlementStatusSettled = "SETTLED"
	TaskSettlementStatusReview  = "REVIEW"

	ImageTaskPortableStorageNode = "__portable__"
)

const TaskRefundLegacyCutoff int64 = 1771718400 // 2026-02-22 00:00:00 UTC

type Task struct {
	ID                        int64                 `json:"id" gorm:"primary_key;AUTO_INCREMENT;index:idx_task_image_dispatch,priority:6;index:idx_task_image_settlement_dispatch,priority:7;index:idx_task_image_node_dispatch,priority:7;index:idx_task_image_node_settlement,priority:8"`
	CreatedAt                 int64                 `json:"created_at" gorm:"index"`
	UpdatedAt                 int64                 `json:"updated_at"`
	TaskID                    string                `json:"task_id" gorm:"type:varchar(191);index"`                                                                                                                                                                                                                               // 第三方id，不一定有/ song id\ Task id
	Platform                  constant.TaskPlatform `json:"platform" gorm:"type:varchar(30);index;index:idx_task_dispatch,priority:1;index:idx_task_image_dispatch,priority:1;index:idx_task_image_settlement_dispatch,priority:1;index:idx_task_image_node_dispatch,priority:1;index:idx_task_image_node_settlement,priority:1"` // 平台
	UserId                    int                   `json:"user_id" gorm:"index"`
	ClientTaskID              string                `json:"client_task_id,omitempty" gorm:"type:varchar(191);index"`
	Group                     string                `json:"group" gorm:"type:varchar(50)"` // 修正计费用
	ChannelId                 int                   `json:"channel_id" gorm:"index;index:idx_task_image_dispatch,priority:4;index:idx_task_image_settlement_dispatch,priority:6;index:idx_task_image_node_dispatch,priority:6;index:idx_task_image_node_settlement,priority:7"`
	Quota                     int                   `json:"quota" gorm:"type:bigint"`
	Action                    string                `json:"action" gorm:"type:varchar(40);index"`                                                                                                                                                                                                                               // 任务类型, song, lyrics, description-mode
	Status                    TaskStatus            `json:"status" gorm:"type:varchar(20);index;index:idx_task_dispatch,priority:2;index:idx_task_image_dispatch,priority:2;index:idx_task_image_settlement_dispatch,priority:2;index:idx_task_image_node_dispatch,priority:2;index:idx_task_image_node_settlement,priority:2"` // 任务状态
	FailReason                string                `json:"fail_reason"`
	SubmitTime                int64                 `json:"submit_time" gorm:"index"`
	StartTime                 int64                 `json:"start_time" gorm:"index"`
	FinishTime                int64                 `json:"finish_time" gorm:"index"`
	Progress                  string                `json:"progress" gorm:"type:varchar(20);index"`
	NextPollAt                int64                 `json:"next_poll_at" gorm:"index;index:idx_task_dispatch,priority:3;index:idx_task_image_dispatch,priority:3;index:idx_task_image_settlement_dispatch,priority:4;index:idx_task_image_node_dispatch,priority:4;index:idx_task_image_node_settlement,priority:5"`
	LockUntil                 int64                 `json:"lock_until" gorm:"index;index:idx_task_dispatch,priority:4;index:idx_task_image_dispatch,priority:5;index:idx_task_image_settlement_dispatch,priority:5;index:idx_task_image_node_dispatch,priority:5;index:idx_task_image_node_settlement,priority:6"`
	LockOwner                 string                `json:"lock_owner" gorm:"type:varchar(128);index"`
	StorageNode               string                `json:"storage_node,omitempty" gorm:"type:varchar(128);index;index:idx_task_image_node_dispatch,priority:3;index:idx_task_image_node_settlement,priority:4"`
	RetryCount                int                   `json:"retry_count"`
	SettlementStatus          string                `json:"-" gorm:"type:varchar(20);index;index:idx_task_image_settlement_dispatch,priority:3;index:idx_task_image_node_settlement,priority:3"`
	ResultExpiresAt           int64                 `json:"-" gorm:"index;not null;default:0"`
	ResultAcknowledgedAt      int64                 `json:"-" gorm:"not null;default:0"`
	ResultDeleteAfter         int64                 `json:"-" gorm:"index;not null;default:0"`
	ResultCleanedAt           int64                 `json:"-" gorm:"index;not null;default:0"`
	ResultCleanupPending      bool                  `json:"-" gorm:"index;not null;default:false"`
	RequestCleanupPending     bool                  `json:"-" gorm:"index;not null;default:false"`
	RequestDeleteAfter        int64                 `json:"-" gorm:"index;not null;default:0"`
	RefundPending             bool                  `json:"-" gorm:"index;not null;default:false"`
	ExecutionSecretsCleanedAt int64                 `json:"-" gorm:"index;not null;default:0"`
	SyncSubmissionStartedAt   int64                 `json:"-" gorm:"index;not null;default:0"`
	PublicImageTask           bool                  `json:"-" gorm:"index;not null;default:false"`
	PublicImageTaskTokenID    int                   `json:"-" gorm:"index;not null;default:0"`
	ImageTaskCancelledAt      int64                 `json:"-" gorm:"index;not null;default:0"`
	ImageTaskResultStored     bool                  `json:"-" gorm:"index;not null;default:false"`
	// ImageTaskResultStoredAt is the denormalized result-ready timestamp used by
	// public status metadata queries that omit private_data.
	ImageTaskResultStoredAt int64      `json:"-" gorm:"not null;default:0"`
	Properties              Properties `json:"properties" gorm:"type:json"`
	Username                string     `json:"username,omitempty" gorm:"-"`
	InlineResultAvailable   bool       `json:"-" gorm:"-"`
	StoredResultAvailable   bool       `json:"-" gorm:"-"`
	// 禁止返回给用户，内部可能包含key等隐私信息
	PrivateData TaskPrivateData `json:"-" gorm:"column:private_data;type:json"`
	Data        json.RawMessage `json:"data" gorm:"type:json"`

	updateBase *Task `json:"-" gorm:"-"`
}

func (t *Task) SetData(data any) {
	b, _ := common.Marshal(data)
	t.Data = json.RawMessage(b)
}

func (t *Task) GetData(v any) error {
	return common.Unmarshal(t.Data, &v)
}

// AfterFind captures the row as the baseline for patch-style task updates.
// Task lifecycle callers commonly load a task, mutate one or two fields, and
// then persist it later. Keeping that baseline prevents a stale private_data
// snapshot from replacing values written by another lifecycle worker.
func (t *Task) AfterFind(_ *gorm.DB) error {
	t.updateBase = t.CloneForUpdate()
	return nil
}

// AfterCreate gives newly inserted task objects the same baseline semantics as
// tasks loaded from the database.
func (t *Task) AfterCreate(_ *gorm.DB) error {
	t.updateBase = t.CloneForUpdate()
	return nil
}

type Properties struct {
	Input             string `json:"input"`
	UpstreamModelName string `json:"upstream_model_name,omitempty"`
	OriginModelName   string `json:"origin_model_name,omitempty"`
}

func (m *Properties) Scan(val interface{}) error {
	bytesValue := jsonScanBytes(val)
	if len(bytesValue) == 0 {
		*m = Properties{}
		return nil
	}
	return common.Unmarshal(bytesValue, m)
}

func (m Properties) Value() (driver.Value, error) {
	if m == (Properties{}) {
		return nil, nil
	}
	b, err := common.Marshal(m)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

type TaskPrivateData struct {
	PublicImageTask              bool              `json:"public_image_task,omitempty"`
	ImageTaskMode                string            `json:"image_task_mode,omitempty"`
	RequestPath                  string            `json:"request_path,omitempty"`
	RequestMethod                string            `json:"request_method,omitempty"`
	RequestContentType           string            `json:"request_content_type,omitempty"`
	RequestHeaders               map[string]string `json:"request_headers,omitempty"`
	RequestBodyPath              string            `json:"request_body_path,omitempty"`
	RequestBodyBase64            string            `json:"request_body_base64,omitempty"`
	RequestBodyPortable          bool              `json:"request_body_portable,omitempty"`
	RequestBodyShared            bool              `json:"request_body_shared,omitempty"`
	RequestBodySize              int64             `json:"request_body_size,omitempty"`
	RequestFingerprint           string            `json:"request_fingerprint,omitempty"`
	ResultBodyPath               string            `json:"result_body_path,omitempty"`
	ResultBodySize               int64             `json:"result_body_size,omitempty"`
	ResultBodySHA256             string            `json:"result_body_sha256,omitempty"`
	ResultContentType            string            `json:"result_content_type,omitempty"`
	ResultStoredAt               int64             `json:"result_stored_at,omitempty"`
	ResultExpiresAt              int64             `json:"result_expires_at,omitempty"`
	Key                          string            `json:"key,omitempty"`
	UpstreamTaskID               string            `json:"upstream_task_id,omitempty"` // 上游真实 task ID
	UpstreamSubmitUncertainAt    int64             `json:"upstream_submit_uncertain_at,omitempty"`
	UpstreamSubmitUncertainCount int               `json:"upstream_submit_uncertain_count,omitempty"`
	ResultURL                    string            `json:"result_url,omitempty"` // 任务成功后的结果 URL（视频地址等）
	// ResultProxyHosts are channel media hosts captured at submit. GetResultURL
	// rewrites matching https URLs to /v1/videos/{id}/content so OpenAI clients
	// do not fetch credentialed Gemini-compatible hosts directly.
	ResultProxyHosts []string `json:"result_proxy_hosts,omitempty"`
	// Execution records safe, immutable request provenance. It stays in
	// private_data so public task DTOs cannot expose it accidentally.
	Execution *TaskExecutionSnapshot `json:"execution,omitempty"`
	// PluginState is plugin-owned cross-round data. It is only replaced when
	// a plugin hook explicitly returns state.
	PluginState json.RawMessage `json:"plugin_state,omitempty"`
	// ArtifactRefs records objects durably written by the configured artifact
	// backend. It remains private task data and is never exposed in task DTOs.
	ArtifactRefs map[string]TaskArtifactStorageRef `json:"artifact_refs,omitempty"`
	// ResponsesBackground records whether the create request requested a
	// background response protocol.
	ResponsesBackground bool `json:"responses_background,omitempty"`
	// 计费上下文：用于异步退款/差额结算（轮询阶段读取）
	BillingSource                string                       `json:"billing_source,omitempty"`  // "wallet" 或 "subscription"
	SubscriptionId               int                          `json:"subscription_id,omitempty"` // 订阅 ID，用于订阅退款
	TokenId                      int                          `json:"token_id,omitempty"`        // 令牌 ID，用于令牌额度退款
	NodeName                     string                       `json:"node_name,omitempty"`       // 发起任务的节点名，轮询结算阶段据此归属日志
	BillingContext               *TaskBillingContext          `json:"billing_context,omitempty"` // 计费参数快照（用于轮询阶段重新计算）
	PreConsumedUsageRecorded     bool                         `json:"pre_consumed_usage_recorded,omitempty"`
	PreConsumedUsageCaptured     bool                         `json:"pre_consumed_usage_captured,omitempty"`
	TieredBillingSnapshot        *billingexpr.BillingSnapshot `json:"tiered_billing_snapshot,omitempty"`
	BillingRequestInput          *billingexpr.RequestInput    `json:"billing_request_input,omitempty"`
	BillingRequestInputCaptured  bool                         `json:"billing_request_input_captured,omitempty"`
	SettlementUsage              *dto.Usage                   `json:"settlement_usage,omitempty"`
	SettlementExtraContent       []string                     `json:"settlement_extra_content,omitempty"`
	SettlementEvidenceCapturedAt int64                        `json:"settlement_evidence_captured_at,omitempty"`
	SettlementAttemptQuota       int                          `json:"settlement_attempt_quota,omitempty"`
	SettlementError              string                       `json:"settlement_error,omitempty"`
	CancelledAt                  int64                        `json:"cancelled_at,omitempty"`
	CancelledReason              string                       `json:"cancelled_reason,omitempty"`
	PollFailures                 int                          `json:"poll_failures,omitempty"`
}

type TaskArtifactStorageRef struct {
	Backend   string `json:"backend"`
	Bucket    string `json:"bucket"`
	ObjectKey string `json:"object_key"`
	Type      string `json:"type,omitempty"`
	MimeType  string `json:"mime_type,omitempty"`
	Size      int64  `json:"size,omitempty"`
}

type TaskExecutionSnapshot struct {
	RequestID   string              `json:"request_id,omitempty"`
	RequestPath string              `json:"request_path,omitempty"`
	TaskPlugin  *TaskPluginSnapshot `json:"task_plugin,omitempty"`
}

// TaskPluginSnapshot contains credential-free plugin identity and the exact
// source snapshot needed to replay a historical factory plugin after its
// current runtime registration is removed.
type TaskPluginSnapshot struct {
	Key        string                    `json:"key"`
	Name       string                    `json:"name"`
	Version    string                    `json:"version"`
	Source     string                    `json:"source,omitempty"`
	Author     *TaskPluginAuthorSnapshot `json:"author,omitempty"`
	APIVersion int                       `json:"api_version"`
	Generation uint64                    `json:"generation"`
}

type TaskPluginAuthorSnapshot struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

func (t *Task) ClearImageTaskExecutionSecrets() {
	if t == nil {
		return
	}
	t.PrivateData.Key = ""
	t.PrivateData.RequestHeaders = nil
	if t.PrivateData.BillingRequestInput != nil {
		t.PrivateData.BillingRequestInput.Body = nil
	}
	if t.ExecutionSecretsCleanedAt == 0 {
		t.ExecutionSecretsCleanedAt = common.GetTimestamp()
	}
}

func minimizeImageTaskTerminalExecutionSecrets(task *Task) {
	if task == nil {
		return
	}
	if task.Status == TaskStatusFailure || task.SettlementStatus == TaskSettlementStatusSettled {
		task.PrivateData.BillingRequestInput = nil
		task.PrivateData.BillingRequestInputCaptured = false
		task.PrivateData.SettlementUsage = nil
		task.PrivateData.SettlementExtraContent = nil
		task.PrivateData.SettlementEvidenceCapturedAt = 0
		task.ClearImageTaskExecutionSecrets()
		return
	}
	if task.Status != TaskStatusSuccess || task.PrivateData.TieredBillingSnapshot == nil || task.PrivateData.TieredBillingSnapshot.BillingMode != "tiered_expr" {
		task.PrivateData.BillingRequestInput = nil
		task.PrivateData.BillingRequestInputCaptured = false
		task.ClearImageTaskExecutionSecrets()
		return
	}

	input := billingexpr.RequestInput{}
	if task.PrivateData.BillingRequestInput != nil {
		input = billingexpr.CloneRequestInput(*task.PrivateData.BillingRequestInput)
	}
	var evidenceErr error
	if len(input.Params) == 0 {
		if len(input.Body) > 0 {
			input.Params, evidenceErr = billingexpr.CaptureRequestParams(task.PrivateData.TieredBillingSnapshot.ExprString, input.Body)
		} else if referencedParams, err := billingexpr.ReferencedRequestParams(task.PrivateData.TieredBillingSnapshot.ExprString); err != nil {
			evidenceErr = err
		} else if len(referencedParams) > 0 {
			evidenceErr = errors.New("referenced request parameter evidence is unavailable")
		}
	}
	headers := make(map[string]string, len(input.Headers)+len(task.PrivateData.RequestHeaders))
	for key, value := range input.Headers {
		headers[key] = value
	}
	for key, value := range task.PrivateData.RequestHeaders {
		headers[key] = value
	}
	capturedHeaders, headerErr := billingexpr.CaptureRequestHeaders(task.PrivateData.TieredBillingSnapshot.ExprString, headers)
	if headerErr != nil {
		capturedHeaders = nil
		evidenceErr = errors.Join(evidenceErr, headerErr)
	}
	input.Headers = capturedHeaders
	input.Body = nil
	task.PrivateData.BillingRequestInput = &input
	task.PrivateData.BillingRequestInputCaptured = true
	if evidenceErr != nil {
		task.SettlementStatus = TaskSettlementStatusReview
		reason := "image task billing evidence migration requires manual review: " + strings.ReplaceAll(evidenceErr.Error(), "\n", " ")
		if strings.TrimSpace(task.FailReason) == "" {
			task.FailReason = reason
		} else if !strings.Contains(task.FailReason, reason) {
			task.FailReason += "; " + reason
		}
	}
	task.ClearImageTaskExecutionSecrets()
}

const imageTaskFairChannelCursorKey = "image_task_fair_channel_cursor"

type TaskDispatchState struct {
	Key       string `json:"key" gorm:"type:varchar(64);primaryKey"`
	Value     int64  `json:"value" gorm:"bigint"`
	UpdatedAt int64  `json:"updated_at" gorm:"bigint;index"`
}

func (state *TaskDispatchState) BeforeCreate(_ *gorm.DB) error {
	if state.UpdatedAt == 0 {
		state.UpdatedAt = common.GetTimestamp()
	}
	return nil
}

// TaskBillingContext 记录任务提交时的计费参数，以便轮询阶段可以重新计算额度。
type TaskBillingContext struct {
	ModelPrice           float64            `json:"model_price,omitempty"`             // 模型单价
	GroupRatio           float64            `json:"group_ratio,omitempty"`             // 分组倍率
	GroupRatioCaptured   bool               `json:"group_ratio_captured,omitempty"`    // 是否已捕获分组倍率
	GroupSpecialRatio    float64            `json:"group_special_ratio,omitempty"`     // 用户分组特殊倍率
	GroupHasSpecialRatio bool               `json:"group_has_special_ratio,omitempty"` // 是否命中特殊倍率
	ModelRatio           float64            `json:"model_ratio,omitempty"`             // 模型倍率
	CompletionRatio      float64            `json:"completion_ratio,omitempty"`        // 输出倍率
	CacheRatio           float64            `json:"cache_ratio,omitempty"`             // 缓存读取倍率
	CacheCreationRatio   float64            `json:"cache_creation_ratio,omitempty"`    // 缓存写入倍率
	CacheCreation5mRatio float64            `json:"cache_creation_5m_ratio,omitempty"` // 5 分钟缓存写入倍率
	CacheCreation1hRatio float64            `json:"cache_creation_1h_ratio,omitempty"` // 1 小时缓存写入倍率
	ImageRatio           float64            `json:"image_ratio,omitempty"`             // 图片 token 倍率
	AudioRatio           float64            `json:"audio_ratio,omitempty"`             // 音频输入倍率
	AudioCompletionRatio float64            `json:"audio_completion_ratio,omitempty"`  // 音频输出倍率
	OtherRatios          map[string]float64 `json:"other_ratios,omitempty"`            // 附加倍率（时长、分辨率等）
	OriginModelName      string             `json:"origin_model_name,omitempty"`       // 模型名称，必须为OriginModelName
	PerCallBilling       bool               `json:"per_call_billing,omitempty"`        // 按次计费：跳过轮询阶段的差额结算
}

// GetUpstreamTaskID 获取上游真实 task ID（用于与 provider 通信）
// 旧数据没有 UpstreamTaskID 时，TaskID 本身就是上游 ID
func (t *Task) GetUpstreamTaskID() string {
	if t.PrivateData.UpstreamTaskID != "" {
		return t.PrivateData.UpstreamTaskID
	}
	return t.TaskID
}

// GetResultURL 获取任务结果 URL（视频地址等）
// 仅成功任务允许暴露结果 URL；旧数据在成功时可回退到 FailReason 中的 legacy URL。
func (t *Task) GetResultURL() string {
	if t == nil || t.Status != TaskStatusSuccess {
		return ""
	}
	extraHosts := t.resultProxyHosts()
	if resultURL := strings.TrimSpace(t.PrivateData.ResultURL); resultURL != "" {
		return publicTaskResultURL(t.TaskID, resultURL, extraHosts...)
	}
	if legacyURL := strings.TrimSpace(t.FailReason); legacyTaskResultURL(legacyURL) {
		return publicTaskResultURL(t.TaskID, legacyURL, extraHosts...)
	}
	return ""
}

func channelNeedsKeyedMediaProxy(channel *Channel) bool {
	if channel == nil {
		return false
	}
	switch channel.Type {
	case constant.ChannelTypeGemini, constant.ChannelTypeVertexAi:
		return true
	default:
		return false
	}
}

func appendUniqueMediaHost(hosts []string, host string) []string {
	host = strings.TrimSpace(strings.ToLower(strings.TrimSuffix(host, ".")))
	if host == "" {
		return hosts
	}
	for _, existing := range hosts {
		if existing == host {
			return hosts
		}
	}
	return append(hosts, host)
}

func (t *Task) resultProxyHosts() []string {
	if t == nil {
		return nil
	}
	hosts := append([]string(nil), t.PrivateData.ResultProxyHosts...)
	var channel *Channel
	if len(hosts) == 0 && t.ChannelId > 0 && DB != nil {
		loaded, err := CacheGetChannel(t.ChannelId)
		if err != nil || loaded == nil {
			loaded, err = GetChannelById(t.ChannelId, true)
		}
		if err == nil && loaded != nil {
			channel = loaded
			if host := MediaHostFromBaseURL(channel.GetBaseURL()); host != "" {
				hosts = appendUniqueMediaHost(hosts, host)
			}
		}
	}
	if channelNeedsKeyedMediaProxy(channel) && channel.BaseURL != nil {
		if liveHost := MediaHostFromBaseURL(*channel.BaseURL); liveHost != "" {
			if stored := MediaHostFromBaseURL(t.PrivateData.ResultURL); stored != "" {
				hosts = appendUniqueMediaHost(hosts, stored)
			}
		}
	}
	return hosts
}

func publicTaskResultURL(taskID, resultURL string, extraHosts ...string) string {
	resultURL = strings.TrimSpace(resultURL)
	if resultURL == "" {
		return ""
	}
	if resultURLRequiresContentProxy(resultURL, extraHosts...) {
		return fmt.Sprintf("%s/v1/videos/%s/content", system_setting.ServerAddress, taskID)
	}
	return resultURL
}

func resultURLRequiresContentProxy(value string, extraHosts ...string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if isGCSObjectURI(value) || isInlineDataURL(value) {
		return true
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed == nil {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	host := MediaHostFromBaseURL(value)
	switch {
	case host == "generativelanguage.googleapis.com", host == "ai.google.dev", host == "files.googleapis.com":
		return true
	case host == "aiplatform.googleapis.com", strings.HasSuffix(host, ".aiplatform.googleapis.com"):
		return true
	case host == "storage.googleapis.com", host == "storage.cloud.google.com":
		return !gcsHTTPSIsSigned(parsed)
	default:
		for _, extra := range extraHosts {
			if extraHost := MediaHostFromBaseURL(extra); extraHost != "" && extraHost == host {
				return true
			}
		}
		return false
	}
}

// MediaHostFromBaseURL returns the lowercase hostname of a channel base URL or
// media locator. Scheme-less values are treated as https hosts so stored proxy
// hosts and custom Gemini-compatible BaseURLs compare the same way.
func MediaHostFromBaseURL(baseURL string) string {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		return ""
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(parsed.Hostname()), "."))
}

func gcsHTTPSIsSigned(parsed *url.URL) bool {
	if parsed == nil {
		return false
	}
	query := parsed.Query()
	return strings.TrimSpace(query.Get("X-Goog-Signature")) != "" ||
		strings.TrimSpace(query.Get("X-Goog-Algorithm")) != "" ||
		strings.TrimSpace(query.Get("GoogleAccessId")) != "" ||
		strings.TrimSpace(query.Get("X-Goog-Credential")) != ""
}

const publicTaskCappedProgress = "99%"

func TaskSettlementReviewIsRetryable(task *Task) bool {
	if task == nil || task.Status != TaskStatusSuccess || task.SettlementStatus != TaskSettlementStatusReview {
		return false
	}
	if task.Platform == constant.TaskPlatformImage {
		return ImageTaskSettlementReviewIsRetryable(task)
	}
	return task.ResultCleanedAt <= 0 && task.NextPollAt > 0
}

func (t *Task) PublicStatus() TaskStatus {
	if t == nil {
		return ""
	}
	if t.Status != TaskStatusSuccess {
		return t.Status
	}
	switch t.SettlementStatus {
	case TaskSettlementStatusSettled:
		return TaskStatusSuccess
	case TaskSettlementStatusPending, TaskSettlementStatusApplied:
		return TaskStatusInProgress
	case TaskSettlementStatusReview:
		if TaskSettlementReviewIsRetryable(t) {
			return TaskStatusInProgress
		}
		return TaskStatusFailure
	default:
		return TaskStatusSuccess
	}
}

func (t *Task) PublicMediaReady() bool {
	return t != nil && t.PublicStatus() == TaskStatusSuccess && t.ResultCleanedAt <= 0
}

func (t *Task) PublicResultURL() string {
	if !t.PublicMediaReady() {
		return ""
	}
	return t.GetResultURL()
}

func (t *Task) PublicProgress() string {
	if t == nil {
		return ""
	}
	switch t.PublicStatus() {
	case TaskStatusSuccess, TaskStatusFailure:
		return t.Progress
	}
	if strings.TrimSpace(t.Progress) == "100%" {
		return publicTaskCappedProgress
	}
	return t.Progress
}

func (t *Task) PublicFinishTime() int64 {
	if t == nil {
		return 0
	}
	switch t.PublicStatus() {
	case TaskStatusSuccess, TaskStatusFailure:
		return t.FinishTime
	default:
		return 0
	}
}

const (
	TaskPublicAccountingFailReason = kitutil.PublicAccountingFailReason
	TaskPublicSettlementFailReason = kitutil.PublicSettlementFailReason
	TaskPublicInternalFailReason   = kitutil.PublicInternalFailReason
)

func (t *Task) PublicFailReason() string {
	if t == nil {
		return ""
	}
	switch t.PublicStatus() {
	case TaskStatusSuccess, TaskStatusInProgress, TaskStatusQueued, TaskStatusSubmitted, TaskStatusNotStart:
		return ""
	}
	return SanitizePublicTaskFailReason(t.FailReason)
}

// Public protocol fail reasons. These stay English on the public envelope.
const (
	PublicFailReasonEmptyUpstreamTaskID = "upstream task id is empty"
	PublicFailReasonUpstreamTaskTimeout = "upstream task timed out (over 1 hour)"
	PublicFailReasonLegacyTaskTimeout   = "task timed out (legacy task, no refund, please contact the administrator)"
	publicFailReasonChannelInfoFmt      = "failed to get channel information, please contact the administrator, channel id: %d"
	publicFailReasonTaskTimeoutFmt      = "task timed out (%d minutes)"
	legacyFailReasonEmptyUpstreamTaskID = "上游任务ID为空"
	legacyFailReasonUpstreamTaskTimeout = "上游任务超时（超过1小时）"
	legacyFailReasonLegacyTaskTimeout   = "任务超时（旧系统遗留任务，不进行退款，请联系管理员）"
)

var (
	legacyPublicChannelInfoFailReasonRE = regexp.MustCompile(`^获取渠道信息失败，请联系管理员，渠道ID[:：]\s*(\d+)$`)
	publicChannelInfoFailReasonRE       = regexp.MustCompile(`(?i)^failed to get channel information, please contact the administrator, channel id:\s*(\d+)$`)
	legacySweepTimeoutFailReasonRE      = regexp.MustCompile(`^任务超时（(\d+)分钟）$`)
	publicSweepTimeoutFailReasonRE      = regexp.MustCompile(`(?i)^task timed out \((\d+) minutes\)$`)
)

func FormatPublicChannelInfoFailReason(channelID int) string {
	return fmt.Sprintf(publicFailReasonChannelInfoFmt, channelID)
}

func FormatPublicTaskTimeoutFailReason(minutes int) string {
	return fmt.Sprintf(publicFailReasonTaskTimeoutFmt, minutes)
}

func normalizeLegacyPublicTaskFailReason(reason string) string {
	trimmed := strings.TrimSpace(reason)
	switch trimmed {
	case legacyFailReasonEmptyUpstreamTaskID, PublicFailReasonEmptyUpstreamTaskID:
		return PublicFailReasonEmptyUpstreamTaskID
	case legacyFailReasonUpstreamTaskTimeout, PublicFailReasonUpstreamTaskTimeout:
		return PublicFailReasonUpstreamTaskTimeout
	case legacyFailReasonLegacyTaskTimeout, PublicFailReasonLegacyTaskTimeout:
		return PublicFailReasonLegacyTaskTimeout
	}
	if id, ok := parsePublicChannelInfoFailReasonID(trimmed); ok {
		return FormatPublicChannelInfoFailReason(id)
	}
	if minutes, ok := parsePublicTaskTimeoutMinutes(trimmed); ok {
		return FormatPublicTaskTimeoutFailReason(minutes)
	}
	return reason
}

func parsePublicTaskTimeoutMinutes(reason string) (int, bool) {
	for _, re := range []*regexp.Regexp{legacySweepTimeoutFailReasonRE, publicSweepTimeoutFailReasonRE} {
		match := re.FindStringSubmatch(reason)
		if len(match) != 2 {
			continue
		}
		minutes, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		return minutes, true
	}
	return 0, false
}

func parsePublicChannelInfoFailReasonID(reason string) (int, bool) {
	for _, re := range []*regexp.Regexp{legacyPublicChannelInfoFailReasonRE, publicChannelInfoFailReasonRE} {
		match := re.FindStringSubmatch(reason)
		if len(match) != 2 {
			continue
		}
		id, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		return id, true
	}
	return 0, false
}

// SanitizePublicTaskFailReason strips result locators and internal billing/DB
// details from a persisted FailReason before it is returned to clients.
func SanitizePublicTaskFailReason(reason string) string {
	return kitutil.SanitizePublicClientError(normalizeLegacyPublicTaskFailReason(reason))
}

// MatchesRequestToken reports whether an API token may access this task.
// Session requests (tokenID == 0) and legacy tasks without TokenId stay user-scoped.
func (t *Task) MatchesRequestToken(tokenID int) bool {
	if t == nil {
		return false
	}
	if tokenID <= 0 || t.PrivateData.TokenId <= 0 {
		return true
	}
	return t.PrivateData.TokenId == tokenID
}

// GenerateTaskID 生成对外暴露的 task_xxxx 格式 ID
func GenerateTaskID() string {
	key, _ := common.GenerateRandomCharsKey(32)
	return "task_" + key
}

func (p *TaskPrivateData) Scan(val interface{}) error {
	bytesValue := jsonScanBytes(val)
	if len(bytesValue) == 0 {
		return nil
	}
	return common.Unmarshal(bytesValue, p)
}

func (p TaskPrivateData) Value() (driver.Value, error) {
	b, err := common.Marshal(p)
	if err != nil {
		return nil, err
	}
	if string(b) == "{}" {
		return nil, nil
	}
	return string(b), nil
}

// SyncTaskQueryParams 用于包含所有搜索条件的结构体，可以根据需求添加更多字段
type SyncTaskQueryParams struct {
	Platform       constant.TaskPlatform
	ChannelID      string
	TaskID         string
	UserID         string
	Action         string
	Status         string
	StartTimestamp int64
	EndTimestamp   int64
	UserIDs        []int
}

func InitTask(platform constant.TaskPlatform, relayInfo *commonRelay.RelayInfo) *Task {
	properties := Properties{}
	privateData := TaskPrivateData{}
	if relayInfo != nil && relayInfo.ChannelMeta != nil {
		// Persist the key selected for this attempt. Multi-key channels may
		// expose a list in Channel.Key; async polling and content delivery must
		// reuse the exact credential that submitted the task.
		if relayInfo.ChannelMeta.ApiKey != "" {
			privateData.Key = relayInfo.ChannelMeta.ApiKey
		}
		if host := MediaHostFromBaseURL(relayInfo.ChannelMeta.ChannelBaseUrl); host != "" {
			privateData.ResultProxyHosts = []string{host}
		}
		if relayInfo.UpstreamModelName != "" {
			properties.UpstreamModelName = relayInfo.UpstreamModelName
		}
		if relayInfo.OriginModelName != "" {
			properties.OriginModelName = relayInfo.OriginModelName
		}
	}

	// 使用预生成的公开 ID（如果有），否则新生成
	taskID := ""
	if relayInfo.TaskRelayInfo != nil && relayInfo.TaskRelayInfo.PublicTaskID != "" {
		taskID = relayInfo.TaskRelayInfo.PublicTaskID
	} else {
		taskID = GenerateTaskID()
	}

	t := &Task{
		TaskID:      taskID,
		UserId:      relayInfo.UserId,
		Group:       relayInfo.UsingGroup,
		SubmitTime:  time.Now().Unix(),
		Status:      TaskStatusNotStart,
		Progress:    "0%",
		ChannelId:   relayInfo.ChannelId,
		Platform:    platform,
		Properties:  properties,
		PrivateData: privateData,
	}
	return t
}

func TaskGetAllUserTask(userId int, startIdx int, num int, queryParams SyncTaskQueryParams) []*Task {
	var tasks []*Task
	var err error

	// 初始化查询构建器
	query := DB.Where("user_id = ?", userId)

	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = applyTaskStatusQuery(query, queryParams.Status)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.StartTimestamp != 0 {
		// 假设您已将前端传来的时间戳转换为数据库所需的时间格式，并处理了时间戳的验证和解析
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err = query.Omit("channel_id").Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}

	return tasks
}

func TaskGetAllTasks(startIdx int, num int, queryParams SyncTaskQueryParams) []*Task {
	var tasks []*Task
	var err error

	// 初始化查询构建器
	query := DB

	// 添加过滤条件
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.UserID != "" {
		query = query.Where("user_id = ?", queryParams.UserID)
	}
	if len(queryParams.UserIDs) != 0 {
		query = query.Where("user_id in (?)", queryParams.UserIDs)
	}
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = applyTaskStatusQuery(query, queryParams.Status)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}

	return tasks
}

func applyTaskStatusQuery(query *gorm.DB, status string) *gorm.DB {
	switch TaskStatus(status) {
	case TaskStatusSuccess:
		return query.Where(
			"status = ? AND COALESCE(settlement_status, '') <> ?",
			TaskStatusSuccess,
			TaskSettlementStatusReview,
		)
	case TaskStatusFailure:
		return query.Where(
			"(status = ? OR (status = ? AND settlement_status = ?))",
			TaskStatusFailure,
			TaskStatusSuccess,
			TaskSettlementStatusReview,
		)
	default:
		return query.Where("status = ?", status)
	}
}

func GetTimedOutUnfinishedTasks(cutoffUnix int64, limit int) []*Task {
	var tasks []*Task
	err := DB.Where(taskUnfinishedProgressWhere, "100%").
		Where("status NOT IN ?", []string{TaskStatusFailure, TaskStatusSuccess}).
		Where("submit_time < ?", cutoffUnix).
		Order("submit_time").
		Limit(limit).
		Find(&tasks).Error
	if err != nil {
		return nil
	}
	return tasks
}

func GetAllUnFinishSyncTasks(limit int) []*Task {
	var tasks []*Task
	var err error
	// get all tasks progress is not 100%
	err = DB.Where(taskUnfinishedProgressWhere, "100%").Where("status != ?", TaskStatusFailure).Where("status != ?", TaskStatusSuccess).Limit(limit).Order("id").Find(&tasks).Error
	if err != nil {
		return nil
	}
	return tasks
}

func GetRunnableUnfinishedSyncTasks(limit int, now int64) []*Task {
	if limit <= 0 {
		return nil
	}

	imageReserve := runnableImageTaskQueryReserve(limit)
	nonImageTasks := getRunnableNonImageTasks(limit-imageReserve, nil, now)
	imageLimit := runnableImageTaskQueryLimit(limit)
	remainingSlots := limit - len(nonImageTasks)
	if imageLimit > remainingSlots {
		imageLimit = remainingSlots
	}
	imageTasks := getRunnableImageTasksFair(imageLimit, now)
	total := len(nonImageTasks) + len(imageTasks)
	if total < limit {
		nonImageTasks = append(nonImageTasks, getRunnableNonImageTasks(limit-total, taskPrimaryIDs(nonImageTasks), now)...)
	}

	tasks := make([]*Task, 0, len(nonImageTasks)+len(imageTasks))
	tasks = append(tasks, nonImageTasks...)
	tasks = append(tasks, imageTasks...)
	return tasks
}

func GetRunnableNonImageSyncTasks(limit int) []*Task {
	return getRunnableNonImageTasks(limit, nil, time.Now().Unix())
}

func runnableImageTaskQueryReserve(limit int) int {
	if limit <= 1 {
		return limit
	}
	workerLimit := constant.ImageTaskWorkerConcurrency
	if workerLimit <= 0 {
		workerLimit = 20
	}
	reserve := workerLimit
	if half := limit / 2; reserve > half {
		reserve = half
	}
	if reserve < 1 {
		reserve = 1
	}
	return reserve
}

func runnableImageTaskQueryLimit(limit int) int {
	if limit <= 0 {
		return 0
	}
	workerLimit := constant.ImageTaskWorkerConcurrency
	if workerLimit <= 0 {
		workerLimit = 20
	}
	maxBatch := workerLimit * 4
	if maxBatch < workerLimit {
		maxBatch = workerLimit
	}
	if limit > maxBatch {
		limit = maxBatch
	}
	if limit < 1 {
		return 1
	}
	return limit
}

func getRunnableNonImageTasks(limit int, excludeIDs []int64, now int64) []*Task {
	if limit <= 0 {
		return nil
	}
	if now <= 0 {
		now = time.Now().Unix()
	}
	query := DB.Where("platform <> ?", constant.TaskPlatformImage).
		Where(
			"(("+taskUnfinishedProgressWhere+" AND status NOT IN ?) OR (status = ? AND settlement_status = ? AND (next_poll_at IS NULL OR (next_poll_at > 0 AND next_poll_at <= ?))))",
			"100%",
			[]TaskStatus{TaskStatusFailure, TaskStatusSuccess},
			TaskStatusSuccess,
			TaskSettlementStatusReview,
			now,
		)
	if len(excludeIDs) > 0 {
		query = query.Where("id NOT IN ?", excludeIDs)
	}
	var tasks []*Task
	err := query.Limit(limit).Order("id").Find(&tasks).Error
	if err != nil {
		return nil
	}
	return tasks
}

func taskPrimaryIDs(tasks []*Task) []int64 {
	ids := make([]int64, 0, len(tasks))
	for _, task := range tasks {
		if task != nil && task.ID > 0 {
			ids = append(ids, task.ID)
		}
	}
	return ids
}

func getRunnableImageTasksFair(limit int, now int64) []*Task {
	if limit <= 0 {
		return nil
	}
	return getRunnableImageTasksFairByChannel(limit, now)
}

func GetRunnableImageTasks(limit int, now int64) []*Task {
	return getRunnableImageTasksFair(limit, now)
}

const taskUnfinishedProgressWhere = "(progress != ? OR progress = '' OR progress IS NULL)"

func runnableImageSettlementStatuses() []string {
	return []string{TaskSettlementStatusPending, TaskSettlementStatusApplied, TaskSettlementStatusReview}
}

func runnableImageTaskStatusQuery(query *gorm.DB) *gorm.DB {
	return query.Where(
		"(status NOT IN ? OR (status = ? AND settlement_status IN ?))",
		[]TaskStatus{TaskStatusFailure, TaskStatusSuccess},
		TaskStatusSuccess,
		runnableImageSettlementStatuses(),
	)
}

type runnableImageTaskChannel struct {
	ChannelId int `gorm:"column:channel_id"`
}

const runnableImageTaskDueWhere = "(next_poll_at <= ? OR next_poll_at IS NULL) AND (lock_until <= ? OR lock_until IS NULL)"

func runnableImageTaskOpenSettlementDueWhere() string {
	return "settlement_status IN (?, ?) AND " + runnableImageTaskDueWhere
}

func runnableImageTaskRetryableReviewDueWhere() string {
	payloadWhere := imageTaskRetryableReviewPayloadWhere()
	evidenceWhere := imageTaskPrivateDataTextColumn() + ` LIKE '%"settlement_evidence_captured_at":%'`
	return "settlement_status = ? AND (lock_until <= ? OR lock_until IS NULL) AND (" +
		"(COALESCE(next_poll_at, 0) > 0 AND next_poll_at <= ? AND " + payloadWhere + ") OR " +
		"(COALESCE(next_poll_at, 0) = 0 AND " + evidenceWhere + "))"
}

func imageTaskRetryableReviewPayloadWhere() string {
	privateText := imageTaskPrivateDataTextColumn()
	return "(" +
		"COALESCE(image_task_result_stored, 0) != 0 OR " +
		"(COALESCE(result_cleaned_at, 0) = 0 AND data IS NOT NULL AND LENGTH(CAST(data AS TEXT)) > 0) OR " +
		privateText + ` LIKE '%"settlement_evidence_captured_at":%' OR ` +
		privateText + ` LIKE '%"result_body_path":"%' OR ` +
		privateText + ` LIKE '%"request_body_path":"%' OR ` +
		privateText + ` LIKE '%"request_body_base64":"%'` +
		")"
}

func runnableImageTaskNodeWhere() (string, []any) {
	where, args := runnableImageTaskStorageNodeFilter()
	if where == "" {
		return "", nil
	}
	return " AND " + where, args
}

func imageTaskRunnableNodeQuery(query *gorm.DB) *gorm.DB {
	where, args := runnableImageTaskStorageNodeFilter()
	if where == "" {
		return query
	}
	return query.Where(where, args...)
}

func runnableImageTaskStorageNodeFilter() (string, []any) {
	node := strings.TrimSpace(common.NodeName)
	portableWhere, portableArgs := runnableImageTaskPortableStorageNodeFilter()
	if constant.ImageTaskFileCacheShared {
		if !common.ImageTaskSharedCacheDisabled() {
			return "", nil
		}
		if node == "" {
			return portableWhere, portableArgs
		}
		return "(storage_node = ? OR " + portableWhere + ")", append([]any{node}, portableArgs...)
	}
	if !constant.ImageTaskLocalFileCacheAffinity || node == "" {
		return "", nil
	}
	return "(storage_node = ? OR " + portableWhere + ")", append([]any{node}, portableArgs...)
}

func runnableImageTaskPortableStorageNodeFilter() (string, []any) {
	return "storage_node = ?", []any{ImageTaskPortableStorageNode}
}

func getRunnableImageTasksFairByChannel(limit int, now int64) []*Task {
	channels := getRunnableImageTaskChannels(limit, now)
	if len(channels) == 0 {
		return nil
	}

	perChannelLimit := limit / len(channels)
	if limit%len(channels) != 0 {
		perChannelLimit++
	}
	if perChannelLimit < 1 {
		perChannelLimit = 1
	}

	if tasks, ok := getRunnableImageTasksForChannels(channels, perChannelLimit, limit, now); ok {
		advanceImageTaskFairChannelCursorFromTasks(tasks)
		return tasks
	}

	buckets := make([][]*Task, 0, len(channels))
	for _, channel := range channels {
		channelTasks := getRunnableImageTasksForChannel(channel.ChannelId, perChannelLimit, now)
		if len(channelTasks) > 0 {
			buckets = append(buckets, channelTasks)
		}
	}

	tasks := interleaveImageTaskBuckets(buckets, limit)
	advanceImageTaskFairChannelCursorFromTasks(tasks)
	return tasks
}

func interleaveImageTaskBuckets(buckets [][]*Task, limit int) []*Task {
	tasks := make([]*Task, 0, limit)
	for len(tasks) < limit {
		progressed := false
		for i := range buckets {
			if len(buckets[i]) == 0 {
				continue
			}
			tasks = append(tasks, buckets[i][0])
			buckets[i] = buckets[i][1:]
			progressed = true
			if len(tasks) >= limit {
				break
			}
		}
		if !progressed {
			break
		}
	}
	return tasks
}

type runnableImageTaskRow struct {
	ID        int64 `gorm:"column:id"`
	ChannelID int   `gorm:"column:channel_id"`
}

func getRunnableImageTasksForChannels(channels []runnableImageTaskChannel, perChannelLimit int, limit int, now int64) ([]*Task, bool) {
	if len(channels) == 0 || perChannelLimit <= 0 || limit <= 0 {
		return nil, true
	}
	channelIDs := make([]int, 0, len(channels))
	for _, channel := range channels {
		channelIDs = append(channelIDs, channel.ChannelId)
	}
	nodeWhere, nodeArgs := runnableImageTaskNodeWhere()
	args := []any{
		constant.TaskPlatformImage, channelIDs, TaskStatusFailure, TaskStatusSuccess, now, now,
	}
	args = append(args, nodeArgs...)
	args = append(args,
		constant.TaskPlatformImage, channelIDs, TaskStatusSuccess, TaskSettlementStatusPending, TaskSettlementStatusApplied, now, now,
		constant.TaskPlatformImage, channelIDs, TaskStatusSuccess, TaskSettlementStatusReview, now, now,
	)
	args = append(args, perChannelLimit)

	// 结算分支（SUCCESS + PENDING/APPLIED/可重试 REVIEW）不加 storage_node 过滤：
	// 成功路径在置 SUCCESS 前已固化计费证据，结算不依赖创建节点的本地请求体文件，
	// 任意节点接管可避免节点消失后待结算任务永久搁浅。
	// next_poll_at=0 且无结算证据的 REVIEW 是人工/过期收口，不能再当到期任务捞起来。
	// 有结算证据但窗口被写成 0 的 REVIEW 仍要入队，避免可恢复结算永远搁浅。
	var rows []runnableImageTaskRow
	err := DB.Raw(`
SELECT id, channel_id FROM (
  SELECT id, channel_id, ROW_NUMBER() OVER (PARTITION BY channel_id ORDER BY id ASC) AS rn
  FROM (
    SELECT id, channel_id FROM tasks
    WHERE platform = ? AND channel_id IN ? AND status NOT IN (?, ?) AND `+runnableImageTaskDueWhere+nodeWhere+`
    UNION
    SELECT id, channel_id FROM tasks
    WHERE platform = ? AND channel_id IN ? AND status = ? AND `+runnableImageTaskOpenSettlementDueWhere()+`
    UNION
    SELECT id, channel_id FROM tasks
    WHERE platform = ? AND channel_id IN ? AND status = ? AND `+runnableImageTaskRetryableReviewDueWhere()+`
  ) AS runnable_tasks
) AS ranked_tasks
WHERE rn <= ?
ORDER BY channel_id ASC, id ASC`,
		args...,
	).Scan(&rows).Error
	if err != nil {
		return nil, false
	}
	if len(rows) == 0 {
		return nil, true
	}

	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	loaded := getTasksByIDsPreserveOrder(ids)
	if len(loaded) == 0 {
		return nil, true
	}
	loadedByID := make(map[int64]*Task, len(loaded))
	for _, task := range loaded {
		if task != nil {
			loadedByID[task.ID] = task
		}
	}
	bucketIDs := make(map[int][]int64, len(channels))
	for _, row := range rows {
		bucketIDs[row.ChannelID] = append(bucketIDs[row.ChannelID], row.ID)
	}
	buckets := make([][]*Task, 0, len(channels))
	for _, channel := range channels {
		ids := bucketIDs[channel.ChannelId]
		if len(ids) == 0 {
			continue
		}
		bucket := make([]*Task, 0, len(ids))
		for _, id := range ids {
			if task := loadedByID[id]; imageTaskShouldDispatch(task) {
				bucket = append(bucket, task)
			}
		}
		if len(bucket) > 0 {
			buckets = append(buckets, bucket)
		}
	}
	return interleaveImageTaskBuckets(buckets, limit), true
}

func getRunnableImageTaskChannels(limit int, now int64) []runnableImageTaskChannel {
	if limit <= 0 {
		return nil
	}
	cursor := getImageTaskFairChannelCursor()
	channels := make([]runnableImageTaskChannel, 0, limit)
	nodeWhere, nodeArgs := runnableImageTaskNodeWhere()
	args := []any{
		constant.TaskPlatformImage, TaskStatusFailure, TaskStatusSuccess, now, now,
	}
	args = append(args, nodeArgs...)
	args = append(args,
		constant.TaskPlatformImage, TaskStatusSuccess, TaskSettlementStatusPending, TaskSettlementStatusApplied, now, now,
		constant.TaskPlatformImage, TaskStatusSuccess, TaskSettlementStatusReview, now, now,
	)
	args = append(args, cursor, limit)
	// 结算分支不加 storage_node 过滤，允许任意节点接管结算，见 getRunnableImageTasksForChannels。
	if err := DB.Raw(`
SELECT channel_id FROM (
  SELECT DISTINCT channel_id FROM tasks
  WHERE platform = ? AND status NOT IN (?, ?) AND `+runnableImageTaskDueWhere+nodeWhere+`
  UNION
  SELECT DISTINCT channel_id FROM tasks
  WHERE platform = ? AND status = ? AND `+runnableImageTaskOpenSettlementDueWhere()+`
  UNION
  SELECT DISTINCT channel_id FROM tasks
  WHERE platform = ? AND status = ? AND `+runnableImageTaskRetryableReviewDueWhere()+`
) AS runnable_channels
ORDER BY CASE WHEN channel_id > ? THEN 0 ELSE 1 END, channel_id ASC
LIMIT ?`,
		args...,
	).Scan(&channels).Error; err != nil {
		return nil
	}
	return channels
}

func getRunnableImageTasksForChannel(channelID int, limit int, now int64) []*Task {
	if limit <= 0 {
		return nil
	}
	var ids []int64
	nodeWhere, nodeArgs := runnableImageTaskNodeWhere()
	args := []any{
		constant.TaskPlatformImage, channelID, TaskStatusFailure, TaskStatusSuccess, now, now,
	}
	args = append(args, nodeArgs...)
	args = append(args,
		constant.TaskPlatformImage, channelID, TaskStatusSuccess, TaskSettlementStatusPending, TaskSettlementStatusApplied, now, now,
		constant.TaskPlatformImage, channelID, TaskStatusSuccess, TaskSettlementStatusReview, now, now,
	)
	args = append(args, limit)
	// 结算分支不加 storage_node 过滤，允许任意节点接管结算，见 getRunnableImageTasksForChannels。
	err := DB.Raw(`
  SELECT id FROM (
    SELECT id FROM tasks
  WHERE platform = ? AND channel_id = ? AND status NOT IN (?, ?) AND `+runnableImageTaskDueWhere+nodeWhere+`
  UNION
  SELECT id FROM tasks
  WHERE platform = ? AND channel_id = ? AND status = ? AND `+runnableImageTaskOpenSettlementDueWhere()+`
  UNION
  SELECT id FROM tasks
  WHERE platform = ? AND channel_id = ? AND status = ? AND `+runnableImageTaskRetryableReviewDueWhere()+`
) AS runnable_tasks
ORDER BY id ASC
LIMIT ?`,
		args...,
	).Scan(&ids).Error
	if err != nil || len(ids) == 0 {
		return nil
	}
	return filterDispatchableImageTasks(getTasksByIDsPreserveOrder(ids))
}

func imageTaskShouldDispatch(task *Task) bool {
	if task == nil {
		return false
	}
	if task.Status == TaskStatusSuccess && task.SettlementStatus == TaskSettlementStatusReview {
		return ImageTaskSettlementReviewIsRetryable(task)
	}
	return true
}

func filterDispatchableImageTasks(tasks []*Task) []*Task {
	if len(tasks) == 0 {
		return tasks
	}
	filtered := make([]*Task, 0, len(tasks))
	for _, task := range tasks {
		if imageTaskShouldDispatch(task) {
			filtered = append(filtered, task)
		}
	}
	return filtered
}

func getTasksByIDsPreserveOrder(ids []int64) []*Task {
	if len(ids) == 0 {
		return nil
	}
	var loaded []*Task
	if err := DB.Where("id IN ?", ids).Find(&loaded).Error; err != nil {
		return nil
	}
	byID := make(map[int64]*Task, len(loaded))
	for _, task := range loaded {
		if task != nil {
			byID[task.ID] = task
		}
	}
	tasks := make([]*Task, 0, len(ids))
	for _, id := range ids {
		if task := byID[id]; task != nil {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

func advanceImageTaskFairChannelCursorFromTasks(tasks []*Task) {
	var lastChannelID int64
	found := false
	for _, task := range tasks {
		if task == nil {
			continue
		}
		lastChannelID = int64(task.ChannelId)
		found = true
	}
	if found {
		setImageTaskFairChannelCursor(lastChannelID)
	}
}

func getImageTaskFairChannelCursor() int64 {
	if DB == nil {
		return 0
	}
	var state TaskDispatchState
	if err := DB.Select("value").
		Where(taskDispatchStateKeyEq(imageTaskFairChannelCursorKey)).
		First(&state).Error; err != nil {
		return 0
	}
	return state.Value
}

func taskDispatchStateKeyEq(key string) clause.Expression {
	return clause.Eq{
		Column: clause.Column{Name: "key"},
		Value:  key,
	}
}

func setImageTaskFairChannelCursor(cursor int64) {
	if DB == nil {
		return
	}
	if cursor < 0 {
		cursor = 0
	}
	now := common.GetTimestamp()
	state := &TaskDispatchState{
		Key:       imageTaskFairChannelCursorKey,
		Value:     cursor,
		UpdatedAt: now,
	}
	_ = DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"value",
			"updated_at",
		}),
	}).Create(state).Error
}

func HasUnfinishedSyncTasks() bool {
	var id int64
	err := DB.Model(&Task{}).
		Where(taskUnfinishedProgressWhere, "100%").
		Where("status != ?", TaskStatusFailure).
		Where("status != ?", TaskStatusSuccess).
		Limit(1).
		Pluck("id", &id).Error
	return err == nil && id != 0
}

func HasRunnableSyncTasks(now int64) bool {
	if HasRunnableNonImageSyncTasks() {
		return true
	}
	return HasRunnableImageTasks(now)
}

func HasRunnableNonImageSyncTasks() bool {
	var id int64
	err := DB.Model(&Task{}).
		Where("platform <> ?", constant.TaskPlatformImage).
		Where(taskUnfinishedProgressWhere, "100%").
		Where("status NOT IN ?", []TaskStatus{TaskStatusFailure, TaskStatusSuccess}).
		Limit(1).
		Pluck("id", &id).Error
	return err == nil && id != 0
}

func HasRunnableImageTasks(now int64) bool {
	return hasRunnableImageUnfinishedTasks(now) || hasRunnableImageSettlementTasks(now)
}

func hasRunnableImageUnfinishedTasks(now int64) bool {
	var id int64
	err := imageTaskRunnableNodeQuery(imageTaskDueQuery(DB.Model(&Task{}), now)).
		Where("platform = ?", constant.TaskPlatformImage).
		Where("status NOT IN ?", []TaskStatus{TaskStatusFailure, TaskStatusSuccess}).
		Limit(1).
		Pluck("id", &id).Error
	return err == nil && id != 0
}

func hasRunnableImageSettlementTasks(now int64) bool {
	return hasRunnableImageOpenSettlementTasks(now) || hasRunnableImageRetryableReviewTasks(now)
}

func hasRunnableImageOpenSettlementTasks(now int64) bool {
	var id int64
	// 结算任务不做 storage_node 过滤：任意节点均可接管结算，避免节点消失后待结算任务搁浅。
	err := imageTaskDueQuery(DB.Model(&Task{}), now).
		Where("platform = ?", constant.TaskPlatformImage).
		Where("status = ?", TaskStatusSuccess).
		Where("settlement_status IN ?", []string{TaskSettlementStatusPending, TaskSettlementStatusApplied}).
		Limit(1).
		Pluck("id", &id).Error
	return err == nil && id != 0
}

func hasRunnableImageRetryableReviewTasks(now int64) bool {
	var id int64
	err := DB.Model(&Task{}).
		Where("platform = ?", constant.TaskPlatformImage).
		Where("status = ?", TaskStatusSuccess).
		Where(runnableImageTaskRetryableReviewDueWhere(), TaskSettlementStatusReview, now, now).
		Limit(1).
		Pluck("id", &id).Error
	return err == nil && id != 0
}

func GetNextRunnableImageTaskAt(now int64) (int64, bool) {
	nextAt, ok := getNextRunnableImageTaskAtForQuery(imageTaskRunnableNodeQuery(imageTaskUnfinishedBaseQuery(DB.Model(&Task{}))), now)
	settlementNextAt, settlementOK := getNextRunnableImageTaskAtForQuery(imageTaskSettlementBaseQuery(DB.Model(&Task{})), now)
	if !ok || (settlementOK && settlementNextAt < nextAt) {
		nextAt = settlementNextAt
		ok = settlementOK
	}
	return nextAt, ok
}

func getNextRunnableImageTaskAtForQuery(query *gorm.DB, now int64) (int64, bool) {
	var row struct {
		NextAt int64 `gorm:"column:next_at"`
	}
	err := query.
		Select("COALESCE(MIN(CASE WHEN COALESCE(next_poll_at, 0) > COALESCE(lock_until, 0) THEN COALESCE(next_poll_at, 0) ELSE COALESCE(lock_until, 0) END), 0) AS next_at").
		Where("COALESCE(next_poll_at, 0) > ? OR COALESCE(lock_until, 0) > ?", now, now).
		Scan(&row).Error
	if err != nil || row.NextAt <= now {
		return 0, false
	}
	return row.NextAt, true
}

func imageTaskUnfinishedBaseQuery(query *gorm.DB) *gorm.DB {
	return query.Where("platform = ?", constant.TaskPlatformImage).
		Where("status NOT IN ?", []TaskStatus{TaskStatusFailure, TaskStatusSuccess})
}

func imageTaskSettlementBaseQuery(query *gorm.DB) *gorm.DB {
	return query.Where("platform = ?", constant.TaskPlatformImage).
		Where("status = ?", TaskStatusSuccess).
		Where("settlement_status IN ?", runnableImageSettlementStatuses())
}

func imageTaskDueQuery(query *gorm.DB, now int64) *gorm.DB {
	return query.Where("(next_poll_at <= ? OR next_poll_at IS NULL)", now).
		Where("(lock_until <= ? OR lock_until IS NULL)", now)
}

// GetOrphanedImageTaskCandidates 返回租约已过期、到期后仍无人认领的图片任务。
// 只用索引列筛选，PrivateData 相关的安全条件由调用方在 Go 侧判定。
func GetOrphanedImageTaskCandidates(now int64, staleBefore int64, limit int) ([]*Task, error) {
	if limit <= 0 {
		limit = 100
	}
	var tasks []*Task
	err := DB.Omit("data").Where(
		"platform = ? AND status IN ? AND (lock_until <= ? OR lock_until IS NULL) AND COALESCE(next_poll_at, 0) > 0 AND next_poll_at <= ?",
		constant.TaskPlatformImage,
		[]TaskStatus{TaskStatusNotStart, TaskStatusQueued, TaskStatusSubmitted, TaskStatusInProgress},
		now,
		staleBefore,
	).Order("id ASC").Limit(limit).Find(&tasks).Error
	if err != nil {
		return nil, err
	}
	return tasks, nil
}

// UpdateWithStatusIfUnlocked performs a CAS update guarded by fromStatus and by
// the absence of a live lease. A stale lease left behind by a crashed node must
// not block recovery, so an expired lock_until is treated as unlocked — the same
// rule ClaimTaskLease uses when it takes over a task.
//
// The data column is omitted on purpose: callers load orphan candidates without
// it, so writing the struct back with Select("*") would blank whatever the row
// already holds.
func (t *Task) UpdateWithStatusIfUnlocked(fromStatus TaskStatus, now int64) (bool, error) {
	return t.UpdateWithStatusIfUnlockedFrom(t.updateBase, fromStatus, now)
}

func (t *Task) UpdateWithStatusIfUnlockedFrom(base *Task, fromStatus TaskStatus, now int64) (bool, error) {
	if t == nil || t.ID <= 0 {
		return false, nil
	}
	won := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var current Task
		if err := lockForUpdate(tx).Where("id = ?", t.ID).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if current.Status != fromStatus ||
			(strings.TrimSpace(current.LockOwner) != "" && current.LockUntil > now) {
			return nil
		}
		updates, applyPatch, err := buildTaskUpdatePatch(base, &current, t)
		if err != nil {
			return err
		}
		if !applyPatch {
			// The row still satisfies the CAS/lease predicates. A no-op
			// mutation is still a successful ownership check; callers use the
			// result to decide whether it is safe to continue the external
			// operation (for example, the first async submission attempt).
			*t = current
			won = true
			return nil
		}
		updates["updated_at"] = nextTaskUpdatedAt(current.UpdatedAt)
		result := tx.Model(&Task{}).
			Where("id = ? AND status = ?", t.ID, fromStatus).
			Where("(lock_owner = '' OR lock_owner IS NULL OR COALESCE(lock_until, 0) <= ?)", now).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		var merged Task
		if err := tx.Where("id = ?", t.ID).First(&merged).Error; err != nil {
			return err
		}
		*t = merged
		won = true
		return nil
	})
	return won, err
}

// ImageTaskCanCancelBeforeExecution reports whether a task is still safe to
// cancel and refund: not started/queued only, no live lease, and no evidence of
// upstream submission. Matches the public cancel pre-check.
func ImageTaskCanCancelBeforeExecution(task *Task, now int64) bool {
	if task == nil {
		return false
	}
	if (strings.TrimSpace(task.LockOwner) != "" && task.LockUntil > now) ||
		strings.TrimSpace(task.PrivateData.UpstreamTaskID) != "" ||
		task.PrivateData.UpstreamSubmitUncertainAt > 0 ||
		task.PrivateData.UpstreamSubmitUncertainCount > 0 ||
		// Sync-wrapper submissions mark this before the status flip is durable;
		// refuse cancel so we never refund after an upstream request may exist.
		task.SyncSubmissionStartedAt > 0 {
		return false
	}
	switch task.Status {
	case TaskStatusNotStart, TaskStatusQueued:
		return true
	default:
		return false
	}
}

// ApplyImageTaskCancelBeforeExecution applies a prepared cancel update only when
// the row still passes ImageTaskCanCancelBeforeExecution under a row lock.
// This closes the TOCTOU gap between the HTTP pre-check and a plain status+lock CAS.
func ApplyImageTaskCancelBeforeExecution(task *Task, fromStatus TaskStatus, now int64) (bool, error) {
	return ApplyImageTaskCancelBeforeExecutionFrom(task, task.updateBase, fromStatus, now)
}

// ApplyImageTaskCancelBeforeExecutionFrom is the baseline-aware form used by
// callers that prepared a task mutation from a known database snapshot.
func ApplyImageTaskCancelBeforeExecutionFrom(task, base *Task, fromStatus TaskStatus, now int64) (bool, error) {
	if task == nil || task.ID <= 0 {
		return false, nil
	}
	switch fromStatus {
	case TaskStatusNotStart, TaskStatusQueued:
	default:
		return false, nil
	}
	won := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var current Task
		if err := lockForUpdate(tx).Where("id = ?", task.ID).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if current.Status != fromStatus {
			return nil
		}
		if !ImageTaskCanCancelBeforeExecution(&current, now) {
			return nil
		}
		updates, applyPatch, err := buildTaskUpdatePatch(base, &current, task)
		if err != nil {
			return err
		}
		if !applyPatch {
			return nil
		}
		updates["updated_at"] = nextTaskUpdatedAt(current.UpdatedAt)
		result := tx.Model(&Task{}).
			Where("id = ? AND status = ?", task.ID, fromStatus).
			Where("(lock_owner = '' OR lock_owner IS NULL OR COALESCE(lock_until, 0) <= ?)", now).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		var merged Task
		if err := tx.Where("id = ?", task.ID).First(&merged).Error; err != nil {
			return err
		}
		*task = merged
		won = true
		return nil
	})
	return won, err
}

func GetOpenImageTaskCachePaths(batchSize int) (map[string]struct{}, map[string]struct{}, error) {
	return GetOpenImageTaskCachePathsForCandidates(nil, nil, batchSize)
}

func GetOpenImageTaskCachePathsForCandidates(bodyCandidates map[string]struct{}, resultCandidates map[string]struct{}, batchSize int) (map[string]struct{}, map[string]struct{}, error) {
	if batchSize <= 0 {
		batchSize = 1000
	}
	bodyPaths := make(map[string]struct{})
	resultPaths := make(map[string]struct{})
	if bodyCandidates != nil && resultCandidates != nil && len(bodyCandidates) == 0 && len(resultCandidates) == 0 {
		return bodyPaths, resultPaths, nil
	}
	candidateNames := imageTaskCacheCandidateNames(bodyCandidates, resultCandidates)
	now := time.Now().Unix()
	if (bodyCandidates != nil || resultCandidates != nil) && len(candidateNames) == 0 {
		return bodyPaths, resultPaths, nil
	}
	collect := func(query *gorm.DB) error {
		var tasks []Task
		return query.FindInBatches(&tasks, batchSize, func(tx *gorm.DB, batch int) error {
			for i := range tasks {
				keepBody, keepResult := imageTaskCachePathRetention(&tasks[i], now)
				if keepBody {
					addImageTaskCachePathForCandidates(bodyPaths, tasks[i].PrivateData.RequestBodyPath, bodyCandidates)
				}
				if keepResult {
					addImageTaskCachePathForCandidates(resultPaths, tasks[i].PrivateData.ResultBodyPath, resultCandidates)
				}
			}
			return nil
		}).Error
	}
	if len(candidateNames) == 0 {
		if err := collect(openImageTaskCachePathQuery(DB.Model(&Task{}), now)); err != nil {
			return nil, nil, err
		}
		return bodyPaths, resultPaths, nil
	}
	for _, names := range chunkImageTaskCacheCandidateNames(candidateNames, 50) {
		if err := collect(applyImageTaskPrivateDataCandidateFilter(openImageTaskCachePathQuery(DB.Model(&Task{}), now), names)); err != nil {
			return nil, nil, err
		}
	}
	return bodyPaths, resultPaths, nil
}

func openImageTaskCachePathQuery(query *gorm.DB, now int64) *gorm.DB {
	return query.
		Select("id, status, settlement_status, private_data, result_cleaned_at, result_delete_after, result_expires_at, request_cleanup_pending, request_delete_after").
		Where("platform = ?", constant.TaskPlatformImage).
		Where(
			`(
				status NOT IN ?
				OR (status = ? AND settlement_status IN ?)
				OR (
					COALESCE(result_cleaned_at, 0) = 0
					AND (
						COALESCE(result_delete_after, 0) > ?
						OR (COALESCE(result_delete_after, 0) = 0 AND COALESCE(result_expires_at, 0) > ?)
					)
				)
				OR (request_cleanup_pending = ? AND COALESCE(request_delete_after, 0) > ?)
			)`,
			[]TaskStatus{TaskStatusFailure, TaskStatusSuccess},
			TaskStatusSuccess,
			[]string{TaskSettlementStatusPending, TaskSettlementStatusApplied, TaskSettlementStatusReview},
			now,
			now,
			true,
			now,
		)
}

func imageTaskCachePathRetention(task *Task, now int64) (keepBody bool, keepResult bool) {
	if task == nil {
		return false, false
	}
	unfinished := task.Status != TaskStatusFailure && task.Status != TaskStatusSuccess
	settlementActive := task.Status == TaskStatusSuccess &&
		(task.SettlementStatus == TaskSettlementStatusPending ||
			task.SettlementStatus == TaskSettlementStatusApplied ||
			task.SettlementStatus == TaskSettlementStatusReview)
	keepBody = unfinished || settlementActive || (task.RequestCleanupPending && task.RequestDeleteAfter > now)
	keepResult = unfinished || (task.ResultCleanedAt == 0 &&
		(task.ResultDeleteAfter > now ||
			(task.ResultDeleteAfter == 0 && (task.ResultExpiresAt > now || (settlementActive && task.ResultExpiresAt == 0)))))
	return keepBody, keepResult
}

func imageTaskCacheCandidateNames(candidateSets ...map[string]struct{}) []string {
	seen := make(map[string]struct{})
	for _, candidates := range candidateSets {
		for path := range candidates {
			name := strings.TrimSpace(filepath.Base(path))
			if name == "" || name == "." || name == string(filepath.Separator) {
				continue
			}
			seen[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	return names
}

func chunkImageTaskCacheCandidateNames(names []string, size int) [][]string {
	if size <= 0 || len(names) <= size {
		return [][]string{names}
	}
	chunks := make([][]string, 0, (len(names)+size-1)/size)
	for len(names) > 0 {
		end := size
		if end > len(names) {
			end = len(names)
		}
		chunks = append(chunks, names[:end])
		names = names[end:]
	}
	return chunks
}

func applyImageTaskPrivateDataCandidateFilter(query *gorm.DB, names []string) *gorm.DB {
	if len(names) == 0 {
		return query
	}
	column := imageTaskPrivateDataTextColumn()
	clauses := make([]string, 0, len(names))
	args := make([]any, 0, len(names))
	for _, name := range names {
		clauses = append(clauses, column+" LIKE ?")
		args = append(args, "%"+name+"%")
	}
	return query.Where("("+strings.Join(clauses, " OR ")+")", args...)
}

func imageTaskPrivateDataTextColumn() string {
	column := "CAST(private_data AS TEXT)"
	switch common.MainDatabaseType() {
	case common.DatabaseTypeMySQL:
		column = "CAST(private_data AS CHAR)"
	case common.DatabaseTypePostgreSQL:
		column = "private_data::text"
	}
	return column
}

func addImageTaskCachePathForCandidates(paths map[string]struct{}, path string, candidates map[string]struct{}) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	path = filepath.Clean(path)
	if candidates != nil {
		if _, ok := candidates[path]; !ok {
			return
		}
	}
	paths[path] = struct{}{}
}

func addImageTaskCachePath(paths map[string]struct{}, path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	paths[filepath.Clean(path)] = struct{}{}
}

func GetByOnlyTaskId(taskId string) (*Task, bool, error) {
	if taskId == "" {
		return nil, false, nil
	}
	return getUniqueTaskByQuery(DB.Where("task_id = ?", taskId))
}

// GetUniqueByOnlyTaskId resolves a public task identifier only when exactly
// one row owns it. Capability-based reads must fail closed on collisions.
func GetUniqueByOnlyTaskId(taskId string) (*Task, bool, error) {
	if taskId == "" {
		return nil, false, nil
	}
	return getUniqueTaskByQuery(DB.Where("task_id = ?", taskId))
}

func GetTaskByID(id int64) (*Task, bool, error) {
	if id <= 0 {
		return nil, false, nil
	}
	var task *Task
	err := DB.Where("id = ?", id).First(&task).Error
	exist, err := RecordExist(err)
	if err != nil {
		return nil, false, err
	}
	return task, exist, nil
}

func GetByTaskId(userId int, taskId string) (*Task, bool, error) {
	if taskId == "" {
		return nil, false, nil
	}
	return getUniqueTaskByQuery(DB.Where("user_id = ? and task_id = ?", userId, taskId))
}

func GetByTaskIds(userId int, taskIds []any) ([]*Task, error) {
	if len(taskIds) == 0 {
		return nil, nil
	}
	var task []*Task
	var err error
	err = DB.Where("user_id = ? and task_id in (?)", userId, taskIds).
		Find(&task).Error
	if err != nil {
		return nil, err
	}
	return task, nil
}

func GetByTaskIdsForPlatforms(userID int, platforms []constant.TaskPlatform, taskIDs []string) ([]*Task, error) {
	if len(platforms) == 0 || len(taskIDs) == 0 {
		return nil, nil
	}
	var tasks []*Task
	if err := DB.
		Where("user_id = ? AND platform IN ? AND task_id IN ?", userID, platforms, taskIDs).
		Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

// GetTaskForProtocolObservation reloads a task through the ownership and
// plugin-platform boundary used by long-lived protocol observers.
func GetTaskForProtocolObservation(ctx context.Context, userID int, platform constant.TaskPlatform, taskID string) (*Task, bool, error) {
	if taskID == "" {
		return nil, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return getUniqueTaskByQuery(DB.WithContext(ctx).
		Where("user_id = ? AND platform = ? AND task_id = ?", userID, platform, taskID))
}

func getUniqueTaskByQuery(query *gorm.DB) (*Task, bool, error) {
	if query == nil {
		return nil, false, nil
	}
	var tasks []*Task
	if err := query.Order("id").Limit(2).Find(&tasks).Error; err != nil {
		return nil, false, err
	}
	if len(tasks) != 1 || tasks[0] == nil {
		return nil, false, nil
	}
	return tasks[0], true, nil
}

func legacyTaskResultURL(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	lower := strings.ToLower(value)
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "data:")
}

func isGCSObjectURI(value string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "gs://")
}

func isInlineDataURL(value string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "data:")
}

func markInlineImageTaskResultsAvailable(query *gorm.DB, tasks []*Task) error {
	if len(tasks) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(tasks))
	byID := make(map[int64]*Task, len(tasks))
	for _, task := range tasks {
		if task == nil || task.ID <= 0 {
			continue
		}
		ids = append(ids, task.ID)
		byID[task.ID] = task
	}
	if len(ids) == 0 {
		return nil
	}
	var availableIDs []int64
	if err := query.Model(&Task{}).
		Where("id IN ? AND data IS NOT NULL AND COALESCE(result_cleaned_at, 0) = 0", ids).
		Pluck("id", &availableIDs).Error; err != nil {
		return err
	}
	for _, id := range availableIDs {
		if task := byID[id]; task != nil {
			task.InlineResultAvailable = true
		}
	}
	return nil
}

func hydratePublicImageTaskScalarMetadata(tasks []*Task) {
	for _, task := range tasks {
		if task == nil {
			continue
		}
		resultStoredAt := task.ImageTaskResultStoredAt
		if resultStoredAt <= 0 {
			resultStoredAt = task.FinishTime
		}
		task.PrivateData = TaskPrivateData{
			PublicImageTask: task.PublicImageTask,
			TokenId:         task.PublicImageTaskTokenID,
			CancelledAt:     task.ImageTaskCancelledAt,
			ResultExpiresAt: task.ResultExpiresAt,
			ResultStoredAt:  resultStoredAt,
		}
		task.StoredResultAvailable = task.ImageTaskResultStored
	}
}

func publicImageTaskTokenWhere(userID int, tokenID int, taskID string) (string, []any, bool) {
	if userID <= 0 || tokenID <= 0 || strings.TrimSpace(taskID) == "" {
		return "", nil, false
	}
	return "user_id = ? AND platform = ? AND task_id = ? AND public_image_task = ? AND public_image_task_token_id = ?",
		[]any{userID, constant.TaskPlatformImage, taskID, true, tokenID},
		true
}

func GetPublicImageTaskByTaskID(userID int, tokenID int, taskID string) (*Task, bool, error) {
	where, args, ok := publicImageTaskTokenWhere(userID, tokenID, taskID)
	if !ok {
		return nil, false, nil
	}
	var task Task
	err := DB.Omit("data", "private_data").Where(where, args...).First(&task).Error
	exists, err := RecordExist(err)
	if err != nil || !exists {
		return nil, exists, err
	}
	hydratePublicImageTaskScalarMetadata([]*Task{&task})
	if err := markInlineImageTaskResultsAvailable(DB, []*Task{&task}); err != nil {
		return nil, false, err
	}
	return &task, true, nil
}

func GetPublicImageTaskFullByTaskID(userID int, tokenID int, taskID string) (*Task, bool, error) {
	where, args, ok := publicImageTaskTokenWhere(userID, tokenID, taskID)
	if !ok {
		return nil, false, nil
	}
	var task Task
	err := DB.Where(where, args...).First(&task).Error
	exists, err := RecordExist(err)
	if err != nil || !exists {
		return nil, exists, err
	}
	task.PrivateData.PublicImageTask = true
	task.PrivateData.TokenId = tokenID
	return &task, true, nil
}

// GetPublicImageTaskFullByTaskIDForArtifact loads a task for a signed public
// artifact capability. The capability itself is verified by the controller;
// this query still enforces the task's public image classification.
func GetPublicImageTaskFullByTaskIDForArtifact(taskID string) (*Task, bool, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, false, nil
	}
	var task Task
	err := DB.Where(
		"task_id = ? AND platform = ? AND public_image_task = ?",
		taskID,
		constant.TaskPlatformImage,
		true,
	).First(&task).Error
	exists, err := RecordExist(err)
	if err != nil || !exists {
		return nil, exists, err
	}
	task.PrivateData.PublicImageTask = true
	task.PrivateData.TokenId = task.PublicImageTaskTokenID
	return &task, true, nil
}

func GetPublicImageTasksByTaskIDs(userID int, tokenID int, taskIDs []any) ([]*Task, error) {
	if userID <= 0 || tokenID <= 0 || len(taskIDs) == 0 {
		return nil, nil
	}
	var tasks []*Task
	where := "user_id = ? AND platform = ? AND task_id IN ? AND public_image_task = ? AND public_image_task_token_id = ?"
	args := []any{userID, constant.TaskPlatformImage, taskIDs, true, tokenID}
	if err := DB.Omit("data", "private_data").Where(where, args...).Find(&tasks).Error; err != nil {
		return nil, err
	}
	hydratePublicImageTaskScalarMetadata(tasks)
	if err := markInlineImageTaskResultsAvailable(DB, tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

func GetPendingImageTaskRefundsAfter(afterTaskPrimaryID int64, limit int) ([]*Task, error) {
	if limit <= 0 {
		limit = 100
	}
	var tasks []*Task
	err := DB.Omit("data").Where(
		"platform = ? AND status = ? AND refund_pending = ? AND id > ?",
		constant.TaskPlatformImage,
		TaskStatusFailure,
		true,
		afterTaskPrimaryID,
	).Order("id ASC").Limit(limit).Find(&tasks).Error
	return tasks, err
}

func GetPendingTaskRefundsAfter(afterTaskPrimaryID int64, limit int) ([]*Task, error) {
	if limit <= 0 {
		limit = 100
	}
	var tasks []*Task
	err := DB.Omit("data").Where(
		"platform <> ? AND status = ? AND refund_pending = ? AND id > ?",
		constant.TaskPlatformImage,
		TaskStatusFailure,
		true,
		afterTaskPrimaryID,
	).Order("id ASC").Limit(limit).Find(&tasks).Error
	return tasks, err
}

func GetPendingTaskSettlementsAfter(afterTaskPrimaryID int64, limit int) ([]*Task, error) {
	if limit <= 0 {
		limit = 100
	}
	var tasks []*Task
	err := DB.Omit("data").Where(
		"platform <> ? AND status = ? AND COALESCE(settlement_status, '') IN ? AND id > ?",
		constant.TaskPlatformImage,
		TaskStatusSuccess,
		[]string{"", TaskSettlementStatusPending},
		afterTaskPrimaryID,
	).Order("id ASC").Limit(limit).Find(&tasks).Error
	return tasks, err
}

// imageTaskIdempotencyReusableWhere 限定哪些历史任务还能被同一个 client_task_id 命中。
//
// 规则：
//   - 非终态任务永远可复用。任务最长可以跑到 TASK_TIMEOUT_MINUTES（默认 24 小时），
//     如果按创建时间掐窗口，长任务在窗口外被同键重试会重复创建并重复扣费。
//   - SUCCESS + 结算中（PENDING/APPLIED）只在结果仍可领取时复用。结果过期或清理后
//     必须让出幂等键；清理路径会把未完成结算收口为 REVIEW，避免重复扣费窗口。
//   - SUCCESS + SETTLED 只在结果保留期内且结果仍在时复用。
//   - FAILURE / SUCCESS+REVIEW 立即不可复用。
func imageTaskIdempotencyReusableWhere(query *gorm.DB, now int64) *gorm.DB {
	reuseWindow := int64(common.GetImageTaskIdempotencyReuseWindow().Seconds())
	if reuseWindow <= 0 {
		return query
	}
	terminalCutoff := now - reuseWindow
	resultStillAvailable := `
		COALESCE(result_cleaned_at, 0) = 0 AND
		(COALESCE(result_delete_after, 0) = 0 OR result_delete_after > ?) AND
		(COALESCE(result_expires_at, 0) = 0 OR result_expires_at > ?) AND
		(image_task_result_stored = ? OR data IS NOT NULL)
	`
	return query.Where(
		`(
			status NOT IN ? OR
			(
				status = ? AND settlement_status IN ? AND
				`+resultStillAvailable+`
			) OR
			(
				status = ? AND settlement_status = ? AND
				(COALESCE(finish_time, 0) = 0 OR finish_time >= ?) AND
				`+resultStillAvailable+`
			)
		)`,
		[]TaskStatus{TaskStatusSuccess, TaskStatusFailure},
		TaskStatusSuccess,
		[]string{TaskSettlementStatusPending, TaskSettlementStatusApplied},
		now,
		now,
		true,
		TaskStatusSuccess,
		TaskSettlementStatusSettled,
		terminalCutoff,
		now,
		now,
		true,
	)
}

func GetImageTaskByClientTaskID(userId int, clientTaskID string) (*Task, bool, error) {
	clientTaskID = strings.TrimSpace(clientTaskID)
	if userId <= 0 || clientTaskID == "" {
		return nil, false, nil
	}
	var task *Task
	query := DB.Where("user_id = ? AND platform = ? AND client_task_id = ?", userId, constant.TaskPlatformImage, clientTaskID)
	// 必须取最新一条：陈旧预约被回收后同一个 client_task_id 可能对应多条任务
	// （见 reclaimImageTaskClientTaskIDLockIfStale 的说明），取最老那条会一直返回
	// 那个已经被放弃的任务，而不是真正在跑的新任务。
	err := imageTaskIdempotencyReusableWhere(query, common.GetTimestamp()).
		Order("id DESC").
		First(&task).Error
	exist, err := RecordExist(err)
	if err != nil {
		return nil, false, err
	}
	return task, exist, nil
}

func GetImageTasksByTaskIDsOrClientTaskID(userId int, taskIds []any, clientTaskID string) ([]*Task, error) {
	clientTaskID = strings.TrimSpace(clientTaskID)
	if len(taskIds) == 0 && clientTaskID == "" {
		return nil, nil
	}
	query := DB.Where("user_id = ? AND platform = ?", userId, constant.TaskPlatformImage)
	switch {
	case len(taskIds) > 0 && clientTaskID != "":
		query = query.Where("(task_id IN ? OR client_task_id = ?)", taskIds, clientTaskID)
	case len(taskIds) > 0:
		query = query.Where("task_id IN ?", taskIds)
	default:
		query = query.Where("client_task_id = ?", clientTaskID)
	}
	var tasks []*Task
	if err := query.Order("id ASC").Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

func AcknowledgeImageTaskResult(taskPrimaryID int64, acknowledgedAt int64, deleteAfter int64) (bool, error) {
	if taskPrimaryID <= 0 || acknowledgedAt <= 0 || deleteAfter < acknowledgedAt {
		return false, fmt.Errorf("invalid image task result acknowledgement")
	}
	result := DB.Model(&Task{}).
		Where("id = ? AND platform = ? AND status = ? AND settlement_status = ? AND COALESCE(result_cleaned_at, 0) = 0", taskPrimaryID, constant.TaskPlatformImage, TaskStatusSuccess, TaskSettlementStatusSettled).
		Where("COALESCE(result_acknowledged_at, 0) = 0").
		Updates(map[string]any{
			"result_acknowledged_at": acknowledgedAt,
			"result_delete_after":    deleteAfter,
			"updated_at":             GetDBTimestamp(),
		})
	return result.RowsAffected > 0, result.Error
}

type ImageTaskResultCleanup struct {
	TaskPrimaryID int64
	Path          string
}

const imageTaskResultExpiredBeforeSettlementReason = "image task result expired before settlement completed"

// imageTaskPendingNeedsReviewWhenResultUnavailable reports whether a SUCCESS+PENDING
// task must be forced into REVIEW once its public result is gone.
//
// If settlement evidence was already captured, the worker can still finish billing
// without the response body; forcing REVIEW would block that automatic close-out.
func imageTaskPendingNeedsReviewWhenResultUnavailable(task *Task) bool {
	if task == nil || task.Status != TaskStatusSuccess || task.SettlementStatus != TaskSettlementStatusPending {
		return false
	}
	return !ImageTaskHasSettlementEvidence(task)
}

func ImageTaskHasSettlementEvidence(task *Task) bool {
	return task != nil && task.PrivateData.SettlementEvidenceCapturedAt > 0
}

func ImageTaskHasRetrievableSettlementResult(task *Task) bool {
	if task == nil || task.ResultCleanedAt > 0 {
		return false
	}
	if task.ImageTaskResultStored {
		return true
	}
	if strings.TrimSpace(task.PrivateData.ResultBodyPath) != "" {
		return true
	}
	return len(bytes.TrimSpace(task.Data)) > 0
}

func ImageTaskHasSettlementRequestBody(task *Task) bool {
	if task == nil {
		return false
	}
	return strings.TrimSpace(task.PrivateData.RequestBodyPath) != "" ||
		strings.TrimSpace(task.PrivateData.RequestBodyBase64) != ""
}

func parkUnrecoverableImageTaskSettlementReviewUpdates(updates map[string]any, task *Task) {
	updates["next_poll_at"] = 0
	updates["lock_owner"] = ""
	updates["lock_until"] = 0
	updates["retry_count"] = 0
	if task != nil && strings.TrimSpace(task.FailReason) == "" {
		updates["fail_reason"] = imageTaskResultExpiredBeforeSettlementReason
	}
}

// ImageTaskSettlementReviewIsRetryable reports whether SUCCESS+REVIEW should still
// be picked up by the worker. Manual/unrecoverable review stays parked:
// no billing evidence, and either the result is already gone, never stored, or
// no retry window was scheduled (next_poll_at=0).
func ImageTaskSettlementReviewIsRetryable(task *Task) bool {
	if task == nil || task.Status != TaskStatusSuccess || task.SettlementStatus != TaskSettlementStatusReview {
		return false
	}
	if ImageTaskHasSettlementEvidence(task) {
		return true
	}
	if task.ResultCleanedAt > 0 {
		return false
	}
	if !ImageTaskHasRetrievableSettlementResult(task) && !ImageTaskHasSettlementRequestBody(task) {
		return false
	}
	return task.NextPollAt > 0
}

func CleanupExpiredImageTaskResults(now int64, legacyRetention time.Duration, limit int) ([]ImageTaskResultCleanup, error) {
	if now <= 0 {
		return nil, fmt.Errorf("invalid image task result cleanup time")
	}
	if legacyRetention <= 0 {
		legacyRetention = 72 * time.Hour
	}
	if limit <= 0 {
		limit = 100
	}
	legacyCutoff := now - int64(legacyRetention.Seconds())
	marker, err := common.Marshal(map[string]any{
		"_newapi_result_file": true,
		"removed":             true,
	})
	if err != nil {
		return nil, err
	}
	dueWhere := "((COALESCE(result_delete_after, 0) > 0 AND result_delete_after <= ?) OR (COALESCE(result_expires_at, 0) > 0 AND result_expires_at <= ?) OR (status = ? AND finish_time > 0 AND finish_time <= ?))"
	cleanups := make([]ImageTaskResultCleanup, 0)
	err = DB.Transaction(func(tx *gorm.DB) error {
		var taskIDs []int64
		if err := lockForUpdate(tx.Model(&Task{})).
			Select("id").
			Where("platform = ? AND COALESCE(result_cleaned_at, 0) = 0", constant.TaskPlatformImage).
			Where(dueWhere, now, now, TaskStatusSuccess, legacyCutoff).
			Order("id ASC").
			Limit(limit).
			Pluck("id", &taskIDs).Error; err != nil {
			return err
		}
		for _, taskID := range taskIDs {
			var task Task
			if err := lockForUpdate(tx).
				Select("id", "status", "finish_time", "result_expires_at", "settlement_status", "fail_reason", "private_data").
				Where("id = ? AND platform = ? AND COALESCE(result_cleaned_at, 0) = 0", taskID, constant.TaskPlatformImage).
				Where(dueWhere, now, now, TaskStatusSuccess, legacyCutoff).
				First(&task).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue
				}
				return err
			}
			path := strings.TrimSpace(task.PrivateData.ResultBodyPath)
			if path == "" {
				clearImageTaskResultFileMetadata(&task.PrivateData)
			}
			resultExpiresAt := task.ResultExpiresAt
			if task.FinishTime > 0 {
				completionExpiry := task.FinishTime + int64(legacyRetention.Seconds())
				if resultExpiresAt == 0 || completionExpiry < resultExpiresAt {
					resultExpiresAt = completionExpiry
				}
				if task.PrivateData.ResultExpiresAt == 0 || completionExpiry < task.PrivateData.ResultExpiresAt {
					task.PrivateData.ResultExpiresAt = completionExpiry
				}
			}
			updates := map[string]any{
				"private_data":                task.PrivateData,
				"data":                        json.RawMessage(marker),
				"image_task_result_stored":    false,
				"image_task_result_stored_at": 0,
				"result_expires_at":           resultExpiresAt,
				"result_delete_after":         0,
				"result_cleaned_at":           now,
				"result_cleanup_pending":      path != "",
				"updated_at":                  now,
			}
			// PENDING 且结果已到期、又没有结算证据：收口为 REVIEW，避免永久 finalizing。
			// 已有结算证据时保留 PENDING，让 worker 继续自动结算。
			// APPLIED 只差标 SETTLED，不得改成 REVIEW。
			// 已是 REVIEW 且没有结算证据：结果清掉后必须停掉自动重试窗口。
			if imageTaskPendingNeedsReviewWhenResultUnavailable(&task) {
				updates["settlement_status"] = TaskSettlementStatusReview
				parkUnrecoverableImageTaskSettlementReviewUpdates(updates, &task)
			} else if task.Status == TaskStatusSuccess &&
				task.SettlementStatus == TaskSettlementStatusReview &&
				!ImageTaskHasSettlementEvidence(&task) {
				parkUnrecoverableImageTaskSettlementReviewUpdates(updates, &task)
			}
			result := tx.Model(&Task{}).
				Where("id = ? AND COALESCE(result_cleaned_at, 0) = 0", task.ID).
				Where(dueWhere, now, now, TaskStatusSuccess, legacyCutoff).
				Updates(updates)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected > 0 {
				cleanups = append(cleanups, ImageTaskResultCleanup{TaskPrimaryID: task.ID, Path: path})
			}
		}
		return nil
	})
	return cleanups, err
}

// MarkExpiredOpenImageTaskSettlementReview closes SUCCESS+PENDING tasks whose result is
// already gone/expired and that lack settlement evidence. Tasks that already captured
// billing evidence stay PENDING so settlement can still finish. APPLIED is left alone
// so the final SETTLED CAS can complete after result cleanup.
func MarkExpiredOpenImageTaskSettlementReview(taskPrimaryID int64, now int64) (bool, error) {
	if taskPrimaryID <= 0 {
		return false, nil
	}
	if now <= 0 {
		now = GetDBTimestamp()
	}
	var updated bool
	err := DB.Transaction(func(tx *gorm.DB) error {
		var task Task
		if err := lockForUpdate(tx).
			Select("id", "status", "settlement_status", "fail_reason", "result_cleaned_at", "result_expires_at", "result_delete_after", "private_data").
			Where("id = ? AND platform = ?", taskPrimaryID, constant.TaskPlatformImage).
			First(&task).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if !imageTaskPendingNeedsReviewWhenResultUnavailable(&task) {
			return nil
		}
		resultUnavailable := task.ResultCleanedAt > 0 ||
			(task.ResultDeleteAfter > 0 && now >= task.ResultDeleteAfter) ||
			(task.ResultExpiresAt > 0 && now >= task.ResultExpiresAt)
		if !resultUnavailable {
			return nil
		}
		updates := map[string]any{
			"settlement_status": TaskSettlementStatusReview,
			"next_poll_at":      0,
			"lock_owner":        "",
			"lock_until":        0,
			"retry_count":       0,
			"updated_at":        now,
		}
		if strings.TrimSpace(task.FailReason) == "" {
			updates["fail_reason"] = imageTaskResultExpiredBeforeSettlementReason
		}
		result := tx.Model(&Task{}).
			Where("id = ? AND status = ? AND settlement_status = ?", task.ID, TaskStatusSuccess, TaskSettlementStatusPending).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		updated = result.RowsAffected > 0
		return nil
	})
	return updated, err
}

func GetPendingImageTaskResultFileCleanups(limit int) ([]ImageTaskResultCleanup, error) {
	return GetPendingImageTaskResultFileCleanupsAfter(0, limit)
}

func GetPendingImageTaskResultFileCleanupsAfter(afterTaskPrimaryID int64, limit int) ([]ImageTaskResultCleanup, error) {
	if limit <= 0 {
		limit = 100
	}
	var taskIDs []int64
	if err := DB.Model(&Task{}).
		Select("id").
		Where("platform = ? AND result_cleanup_pending = ? AND id > ?", constant.TaskPlatformImage, true, afterTaskPrimaryID).
		Order("id ASC").Limit(limit).Pluck("id", &taskIDs).Error; err != nil {
		return nil, err
	}
	cleanups := make([]ImageTaskResultCleanup, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		var task Task
		if err := DB.Select("id", "private_data").
			Where("id = ? AND platform = ? AND result_cleanup_pending = ?", taskID, constant.TaskPlatformImage, true).
			First(&task).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, err
		}
		cleanups = append(cleanups, ImageTaskResultCleanup{
			TaskPrimaryID: task.ID,
			Path:          strings.TrimSpace(task.PrivateData.ResultBodyPath),
		})
	}
	return cleanups, nil
}

func FinalizeImageTaskResultFileCleanup(taskPrimaryID int64, path string) error {
	if taskPrimaryID <= 0 {
		return nil
	}
	path = strings.TrimSpace(path)
	return DB.Transaction(func(tx *gorm.DB) error {
		var task Task
		if err := lockForUpdate(tx).Where("id = ? AND result_cleanup_pending = ?", taskPrimaryID, true).First(&task).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if strings.TrimSpace(task.PrivateData.ResultBodyPath) != path {
			return nil
		}
		clearImageTaskResultFileMetadata(&task.PrivateData)
		return tx.Model(&Task{}).
			Where("id = ? AND result_cleanup_pending = ?", taskPrimaryID, true).
			Updates(map[string]any{
				"private_data":                task.PrivateData,
				"image_task_result_stored":    false,
				"image_task_result_stored_at": 0,
				"result_cleanup_pending":      false,
				"updated_at":                  getDBTimestampTx(tx),
			}).Error
	})
}

func GetPendingImageTaskRequestFileCleanupsAfter(now int64, afterTaskPrimaryID int64, limit int) ([]*Task, error) {
	if now <= 0 {
		return nil, fmt.Errorf("invalid image task request cleanup time")
	}
	if limit <= 0 {
		limit = 100
	}
	var taskIDs []int64
	err := DB.Model(&Task{}).Select("id").Where(
		"platform = ? AND request_cleanup_pending = ? AND id > ? AND (COALESCE(request_delete_after, 0) = 0 OR request_delete_after <= ?)",
		constant.TaskPlatformImage,
		true,
		afterTaskPrimaryID,
		now,
	).
		Order("id ASC").Limit(limit).Pluck("id", &taskIDs).Error
	if err != nil {
		return nil, err
	}
	tasks := make([]*Task, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		var task Task
		if err := DB.Select("id", "storage_node", "private_data", "request_cleanup_pending", "request_delete_after").Where(
			"id = ? AND platform = ? AND request_cleanup_pending = ? AND (COALESCE(request_delete_after, 0) = 0 OR request_delete_after <= ?)",
			taskID,
			constant.TaskPlatformImage,
			true,
			now,
		).First(&task).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, err
		}
		task.PrivateData = TaskPrivateData{
			RequestBodyPath:   task.PrivateData.RequestBodyPath,
			RequestBodyShared: task.PrivateData.RequestBodyShared,
			NodeName:          task.PrivateData.NodeName,
		}
		tasks = append(tasks, &task)
	}
	return tasks, nil
}

func FinalizeImageTaskRequestFileCleanup(taskPrimaryID int64, path string) error {
	if taskPrimaryID <= 0 {
		return nil
	}
	path = strings.TrimSpace(path)
	return DB.Transaction(func(tx *gorm.DB) error {
		var task Task
		if err := lockForUpdate(tx).Where("id = ? AND request_cleanup_pending = ?", taskPrimaryID, true).First(&task).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if strings.TrimSpace(task.PrivateData.RequestBodyPath) != path {
			return nil
		}
		task.PrivateData.RequestBodyPath = ""
		task.PrivateData.RequestBodyBase64 = ""
		task.PrivateData.RequestBodyPortable = false
		task.PrivateData.RequestBodyShared = false
		task.PrivateData.RequestBodySize = 0
		return tx.Model(&Task{}).
			Where("id = ? AND request_cleanup_pending = ?", taskPrimaryID, true).
			Updates(map[string]any{
				"private_data":            task.PrivateData,
				"request_cleanup_pending": false,
				"request_delete_after":    0,
				"updated_at":              getDBTimestampTx(tx),
			}).Error
	})
}

func clearImageTaskResultFileMetadata(privateData *TaskPrivateData) {
	if privateData == nil {
		return
	}
	privateData.ResultBodyPath = ""
	privateData.ResultBodySize = 0
	privateData.ResultBodySHA256 = ""
	privateData.ResultContentType = ""
	privateData.ResultStoredAt = 0
	privateData.ResultExpiresAt = 0
}

// ClearImageTaskResultFileMetadata 在结果文件已经被删除后清掉指向它的元数据。
//
// 必须在事务里加行锁重读 private_data 再回写：调用方（取消清理）与后台退款恢复、
// 结算复核并发运行，整体覆盖 private_data 会把对方刚写入的 settlement_error 之类
// 审计信息冲掉。path 不匹配说明已经有别人处理过，直接放行不动。
func ClearImageTaskResultFileMetadata(taskPrimaryID int64, path string) error {
	if taskPrimaryID <= 0 {
		return nil
	}
	path = strings.TrimSpace(path)
	return DB.Transaction(func(tx *gorm.DB) error {
		var task Task
		if err := lockForUpdate(tx).
			Select("id", "private_data").
			Where("id = ?", taskPrimaryID).
			First(&task).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if strings.TrimSpace(task.PrivateData.ResultBodyPath) != path {
			return nil
		}
		clearImageTaskResultFileMetadata(&task.PrivateData)
		return tx.Model(&Task{}).
			Where("id = ?", taskPrimaryID).
			Updates(map[string]any{
				"private_data":                task.PrivateData,
				"image_task_result_stored":    false,
				"image_task_result_stored_at": 0,
				"updated_at":                  getDBTimestampTx(tx),
			}).Error
	})
}

func (t *Task) fillMissingPublicImageTaskScalars() {
	if t == nil || t.Platform != constant.TaskPlatformImage || !t.PrivateData.PublicImageTask {
		return
	}
	if !t.PublicImageTask {
		t.PublicImageTask = true
	}
	if t.PublicImageTaskTokenID == 0 && t.PrivateData.TokenId > 0 {
		t.PublicImageTaskTokenID = t.PrivateData.TokenId
	}
}

func (t *Task) Insert() error {
	t.fillMissingPublicImageTaskScalars()
	var err error
	err = DB.Create(t).Error
	return err
}

func (t *Task) InsertWithContext(ctx context.Context) error {
	t.fillMissingPublicImageTaskScalars()
	if ctx == nil {
		ctx = context.Background()
	}
	return DB.WithContext(ctx).Create(t).Error
}

func DeleteTaskByID(id int64) error {
	if id <= 0 {
		return nil
	}
	return DB.Where("id = ?", id).Delete(&Task{}).Error
}

type taskSnapshot struct {
	Status       TaskStatus
	Progress     string
	StartTime    int64
	FinishTime   int64
	FailReason   string
	ResultURL    string
	Data         json.RawMessage
	PluginState  json.RawMessage
	PollFailures int
}

func (s taskSnapshot) Equal(other taskSnapshot) bool {
	return s.Status == other.Status &&
		s.Progress == other.Progress &&
		s.StartTime == other.StartTime &&
		s.FinishTime == other.FinishTime &&
		s.FailReason == other.FailReason &&
		s.ResultURL == other.ResultURL &&
		bytes.Equal(s.Data, other.Data) &&
		bytes.Equal(s.PluginState, other.PluginState) &&
		s.PollFailures == other.PollFailures
}

func (t *Task) Snapshot() taskSnapshot {
	return taskSnapshot{
		Status:       t.Status,
		Progress:     t.Progress,
		StartTime:    t.StartTime,
		FinishTime:   t.FinishTime,
		FailReason:   t.FailReason,
		ResultURL:    t.PrivateData.ResultURL,
		Data:         t.Data,
		PluginState:  t.PrivateData.PluginState,
		PollFailures: t.PrivateData.PollFailures,
	}
}

// CloneForUpdate returns a deep copy suitable for calculating a task mutation
// patch. A patch is applied under a row lock so concurrent writers can update
// disjoint task fields without replacing each other's private data.
func (t *Task) CloneForUpdate() *Task {
	if t == nil {
		return nil
	}
	clone := *t
	clone.updateBase = nil
	clone.Data = append(json.RawMessage(nil), t.Data...)
	privateData, err := json.Marshal(t.PrivateData)
	if err == nil {
		_ = json.Unmarshal(privateData, &clone.PrivateData)
	}
	return &clone
}

func (t *Task) Update() error {
	if t == nil || t.ID <= 0 {
		return fmt.Errorf("update task failed, task is invalid")
	}
	expectedUpdatedAt := t.UpdatedAt
	updatedAt := nextTaskUpdatedAt(expectedUpdatedAt)
	updates := *t
	updates.UpdatedAt = updatedAt
	query := DB.Model(&Task{}).Where("id = ?", t.ID)
	if expectedUpdatedAt > 0 {
		query = query.Where("updated_at = ?", expectedUpdatedAt)
	}
	result := query.Select("*").Updates(&updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		exists, err := taskRowExists(t.ID)
		if err != nil {
			return err
		}
		if exists {
			return ErrTaskConcurrentUpdate
		}
		return fmt.Errorf("update task failed, id=%d", t.ID)
	}
	t.UpdatedAt = updatedAt
	return nil
}

func (t *Task) UpdateWithStatus(fromStatus TaskStatus) (bool, error) {
	return t.updateWithStatusFrom(t.updateBase, fromStatus, "", 0, false)
}

// UpdateWithStatusFrom applies only the fields changed between base and t.
// The status check and the patch application are performed while holding the
// task row lock, preventing stale private_data snapshots from overwriting
// provider results, plugin state, artifacts, or billing evidence.
func (t *Task) UpdateWithStatusFrom(base *Task, fromStatus TaskStatus) (bool, error) {
	return t.updateWithStatusFrom(base, fromStatus, "", 0, false)
}

func (t *Task) UpdateWithStatusAndLeaseFrom(base *Task, fromStatus TaskStatus, lockOwner string, now int64) (bool, error) {
	return t.updateWithStatusFrom(base, fromStatus, lockOwner, now, true)
}

func (t *Task) updateWithStatusFrom(base *Task, fromStatus TaskStatus, lockOwner string, now int64, requireLease bool) (bool, error) {
	if t == nil || t.ID <= 0 {
		return false, nil
	}
	won := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var current Task
		query := lockForUpdate(tx).Where("id = ?", t.ID)
		if err := query.First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if current.Status != fromStatus {
			return nil
		}
		if requireLease && lockOwner != "" &&
			(current.LockOwner != lockOwner || current.LockUntil <= now) {
			return nil
		}
		if base == nil && t.UpdatedAt > 0 && current.UpdatedAt != t.UpdatedAt {
			return nil
		}

		updates, applyPatch, err := buildTaskUpdatePatch(base, &current, t)
		if err != nil {
			return err
		}
		if !applyPatch {
			*t = current
			won = true
			return nil
		}
		if requireLease && t.Status != TaskStatusSuccess && t.Status != TaskStatusFailure {
			delete(updates, "lock_owner")
			delete(updates, "lock_until")
		}
		updatedAt := nextTaskUpdatedAt(current.UpdatedAt)
		updates["updated_at"] = updatedAt
		statusQuery := tx.Model(&Task{}).Where("id = ? AND status = ?", t.ID, fromStatus)
		if requireLease && lockOwner != "" {
			statusQuery = statusQuery.Where("lock_owner = ? AND COALESCE(lock_until, 0) > ?", lockOwner, now)
		}
		result := statusQuery.Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}

		var merged Task
		if err := tx.Where("id = ?", t.ID).First(&merged).Error; err != nil {
			return err
		}
		*t = merged
		won = true
		return nil
	})
	return won, err
}

func buildTaskUpdatePatch(base, current, candidate *Task) (map[string]any, bool, error) {
	updates := make(map[string]any)
	if base == nil {
		updates = map[string]any{
			"task_id":                      candidate.TaskID,
			"platform":                     candidate.Platform,
			"user_id":                      candidate.UserId,
			"client_task_id":               candidate.ClientTaskID,
			"group":                        candidate.Group,
			"channel_id":                   candidate.ChannelId,
			"quota":                        candidate.Quota,
			"action":                       candidate.Action,
			"status":                       candidate.Status,
			"fail_reason":                  candidate.FailReason,
			"submit_time":                  candidate.SubmitTime,
			"start_time":                   candidate.StartTime,
			"finish_time":                  candidate.FinishTime,
			"progress":                     candidate.Progress,
			"next_poll_at":                 candidate.NextPollAt,
			"lock_until":                   candidate.LockUntil,
			"lock_owner":                   candidate.LockOwner,
			"storage_node":                 candidate.StorageNode,
			"retry_count":                  candidate.RetryCount,
			"settlement_status":            candidate.SettlementStatus,
			"result_expires_at":            candidate.ResultExpiresAt,
			"result_acknowledged_at":       candidate.ResultAcknowledgedAt,
			"result_delete_after":          candidate.ResultDeleteAfter,
			"result_cleaned_at":            candidate.ResultCleanedAt,
			"result_cleanup_pending":       candidate.ResultCleanupPending,
			"request_cleanup_pending":      candidate.RequestCleanupPending,
			"request_delete_after":         candidate.RequestDeleteAfter,
			"refund_pending":               candidate.RefundPending,
			"execution_secrets_cleaned_at": candidate.ExecutionSecretsCleanedAt,
			"sync_submission_started_at":   candidate.SyncSubmissionStartedAt,
			"public_image_task":            candidate.PublicImageTask,
			"public_image_task_token_id":   candidate.PublicImageTaskTokenID,
			"image_task_cancelled_at":      candidate.ImageTaskCancelledAt,
			"image_task_result_stored":     candidate.ImageTaskResultStored,
			"image_task_result_stored_at":  candidate.ImageTaskResultStoredAt,
			"properties":                   candidate.Properties,
			"private_data":                 candidate.PrivateData,
			"data":                         candidate.Data,
		}
		return updates, true, nil
	}

	if candidate.TaskID != base.TaskID {
		updates["task_id"] = candidate.TaskID
	}
	if candidate.Platform != base.Platform {
		updates["platform"] = candidate.Platform
	}
	if candidate.UserId != base.UserId {
		updates["user_id"] = candidate.UserId
	}
	if candidate.ClientTaskID != base.ClientTaskID {
		updates["client_task_id"] = candidate.ClientTaskID
	}
	if candidate.Group != base.Group {
		updates["group"] = candidate.Group
	}
	if candidate.ChannelId != base.ChannelId {
		updates["channel_id"] = candidate.ChannelId
	}
	if candidate.Quota != base.Quota {
		updates["quota"] = candidate.Quota
	}
	if candidate.Action != base.Action {
		updates["action"] = candidate.Action
	}
	if candidate.Status != base.Status {
		updates["status"] = candidate.Status
	}
	if candidate.FailReason != base.FailReason {
		updates["fail_reason"] = candidate.FailReason
	}
	if candidate.SubmitTime != base.SubmitTime {
		updates["submit_time"] = candidate.SubmitTime
	}
	if candidate.StartTime != base.StartTime {
		updates["start_time"] = candidate.StartTime
	}
	if candidate.FinishTime != base.FinishTime {
		updates["finish_time"] = candidate.FinishTime
	}
	if candidate.Progress != base.Progress {
		updates["progress"] = candidate.Progress
	}
	if candidate.NextPollAt != base.NextPollAt {
		updates["next_poll_at"] = candidate.NextPollAt
	}
	if candidate.LockUntil != base.LockUntil {
		updates["lock_until"] = candidate.LockUntil
	}
	if candidate.LockOwner != base.LockOwner {
		updates["lock_owner"] = candidate.LockOwner
	}
	if candidate.StorageNode != base.StorageNode {
		updates["storage_node"] = candidate.StorageNode
	}
	if candidate.RetryCount != base.RetryCount {
		updates["retry_count"] = candidate.RetryCount
	}
	if candidate.SettlementStatus != base.SettlementStatus {
		updates["settlement_status"] = candidate.SettlementStatus
	}
	if candidate.ResultExpiresAt != base.ResultExpiresAt {
		updates["result_expires_at"] = candidate.ResultExpiresAt
	}
	if candidate.ResultAcknowledgedAt != base.ResultAcknowledgedAt {
		updates["result_acknowledged_at"] = candidate.ResultAcknowledgedAt
	}
	if candidate.ResultDeleteAfter != base.ResultDeleteAfter {
		updates["result_delete_after"] = candidate.ResultDeleteAfter
	}
	if candidate.ResultCleanedAt != base.ResultCleanedAt {
		updates["result_cleaned_at"] = candidate.ResultCleanedAt
	}
	if candidate.ResultCleanupPending != base.ResultCleanupPending {
		updates["result_cleanup_pending"] = candidate.ResultCleanupPending
	}
	if candidate.RequestCleanupPending != base.RequestCleanupPending {
		updates["request_cleanup_pending"] = candidate.RequestCleanupPending
	}
	if candidate.RequestDeleteAfter != base.RequestDeleteAfter {
		updates["request_delete_after"] = candidate.RequestDeleteAfter
	}
	if candidate.RefundPending != base.RefundPending {
		updates["refund_pending"] = candidate.RefundPending
	}
	if candidate.ExecutionSecretsCleanedAt != base.ExecutionSecretsCleanedAt {
		updates["execution_secrets_cleaned_at"] = candidate.ExecutionSecretsCleanedAt
	}
	if candidate.SyncSubmissionStartedAt != base.SyncSubmissionStartedAt {
		updates["sync_submission_started_at"] = candidate.SyncSubmissionStartedAt
	}
	if candidate.PublicImageTask != base.PublicImageTask {
		updates["public_image_task"] = candidate.PublicImageTask
	}
	if candidate.PublicImageTaskTokenID != base.PublicImageTaskTokenID {
		updates["public_image_task_token_id"] = candidate.PublicImageTaskTokenID
	}
	if candidate.ImageTaskCancelledAt != base.ImageTaskCancelledAt {
		updates["image_task_cancelled_at"] = candidate.ImageTaskCancelledAt
	}
	if candidate.ImageTaskResultStored != base.ImageTaskResultStored {
		updates["image_task_result_stored"] = candidate.ImageTaskResultStored
	}
	if candidate.ImageTaskResultStoredAt != base.ImageTaskResultStoredAt {
		updates["image_task_result_stored_at"] = candidate.ImageTaskResultStoredAt
	}
	if !reflect.DeepEqual(candidate.Properties, base.Properties) {
		updates["properties"] = candidate.Properties
	}
	if !reflect.DeepEqual(candidate.PrivateData, base.PrivateData) {
		updates["private_data"] = mergeTaskPrivateData(current.PrivateData, base.PrivateData, candidate.PrivateData)
	}
	if !bytes.Equal(candidate.Data, base.Data) {
		updates["data"] = append(json.RawMessage(nil), candidate.Data...)
	}
	return updates, len(updates) > 0, nil
}

func mergeTaskPrivateData(current, base, candidate TaskPrivateData) TaskPrivateData {
	merged := current
	dst := reflect.ValueOf(&merged).Elem()
	baseValue := reflect.ValueOf(base)
	candidateValue := reflect.ValueOf(candidate)
	for i := 0; i < dst.NumField(); i++ {
		fieldName := dst.Type().Field(i).Name
		if fieldName == "ArtifactRefs" {
			if !reflect.DeepEqual(base.ArtifactRefs, candidate.ArtifactRefs) {
				dst.Field(i).Set(reflect.ValueOf(mergeTaskArtifactRefs(
					current.ArtifactRefs,
					base.ArtifactRefs,
					candidate.ArtifactRefs,
				)))
			}
			continue
		}
		if fieldName == "RequestHeaders" {
			if !reflect.DeepEqual(base.RequestHeaders, candidate.RequestHeaders) {
				dst.Field(i).Set(reflect.ValueOf(mergeTaskStringMap(
					current.RequestHeaders,
					base.RequestHeaders,
					candidate.RequestHeaders,
				)))
			}
			continue
		}
		if !reflect.DeepEqual(baseValue.Field(i).Interface(), candidateValue.Field(i).Interface()) {
			dst.Field(i).Set(candidateValue.Field(i))
		}
	}
	return merged
}

func mergeTaskArtifactRefs(current, base, candidate map[string]TaskArtifactStorageRef) map[string]TaskArtifactStorageRef {
	if reflect.DeepEqual(base, candidate) {
		return current
	}
	merged := make(map[string]TaskArtifactStorageRef, len(current)+len(candidate))
	for key, value := range current {
		merged[key] = value
	}
	for key, baseValue := range base {
		candidateValue, exists := candidate[key]
		if !exists {
			delete(merged, key)
			continue
		}
		if !reflect.DeepEqual(baseValue, candidateValue) {
			merged[key] = candidateValue
		}
	}
	for key, candidateValue := range candidate {
		if _, existedInBase := base[key]; !existedInBase {
			merged[key] = candidateValue
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func mergeTaskStringMap(current, base, candidate map[string]string) map[string]string {
	if reflect.DeepEqual(base, candidate) {
		return current
	}
	merged := make(map[string]string, len(current)+len(candidate))
	for key, value := range current {
		merged[key] = value
	}
	for key, baseValue := range base {
		candidateValue, exists := candidate[key]
		if !exists {
			delete(merged, key)
			continue
		}
		if baseValue != candidateValue {
			merged[key] = candidateValue
		}
	}
	for key, candidateValue := range candidate {
		if _, existedInBase := base[key]; !existedInBase {
			merged[key] = candidateValue
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func (t *Task) UpdateSubmitSettlementError() error {
	if t == nil {
		return fmt.Errorf("update task settlement error failed, task is nil")
	}
	if t.ID <= 0 {
		return fmt.Errorf("update task settlement error failed, taskId=%s, id=%d", t.TaskID, t.ID)
	}
	return updateTaskSettlementFields(t)
}

func (t *Task) UpdateQuota() error {
	if t == nil {
		return fmt.Errorf("task quota update failed, task is nil")
	}
	if t.ID <= 0 {
		return fmt.Errorf("task quota update failed, taskId=%s, id=%d", t.TaskID, t.ID)
	}
	return updateTaskSettlementFields(t)
}

func nextTaskUpdatedAt(previous int64) int64 {
	now := common.GetTimestamp()
	if now <= previous {
		return previous + 1
	}
	return now
}

func updateTaskSettlementFields(t *Task) error {
	if t == nil || t.ID <= 0 {
		return fmt.Errorf("update task settlement fields failed, task is invalid")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var current Task
		if err := lockForUpdate(tx).Where("id = ?", t.ID).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("update task settlement fields failed, taskId=%s, id=%d", t.TaskID, t.ID)
			}
			return err
		}

		base := t.updateBase
		if base == nil || t.Quota != base.Quota {
			current.Quota = t.Quota
		}
		if base == nil || t.FailReason != base.FailReason {
			current.FailReason = t.FailReason
		}
		if base == nil || t.SettlementStatus != base.SettlementStatus {
			current.SettlementStatus = t.SettlementStatus
		}
		if base == nil || t.RefundPending != base.RefundPending {
			current.RefundPending = t.RefundPending
		}
		if base == nil || t.ExecutionSecretsCleanedAt != base.ExecutionSecretsCleanedAt {
			current.ExecutionSecretsCleanedAt = t.ExecutionSecretsCleanedAt
		}
		if base == nil || t.PrivateData.SettlementAttemptQuota != base.PrivateData.SettlementAttemptQuota {
			current.PrivateData.SettlementAttemptQuota = t.PrivateData.SettlementAttemptQuota
		}
		if base == nil || t.PrivateData.SettlementError != base.PrivateData.SettlementError {
			current.PrivateData.SettlementError = t.PrivateData.SettlementError
		}
		if base == nil || t.NextPollAt != base.NextPollAt {
			current.NextPollAt = t.NextPollAt
		}
		updatedAt := nextTaskUpdatedAt(current.UpdatedAt)

		if err := tx.Model(&Task{}).Where("id = ?", t.ID).Updates(map[string]any{
			"quota":                        current.Quota,
			"fail_reason":                  current.FailReason,
			"private_data":                 current.PrivateData,
			"settlement_status":            current.SettlementStatus,
			"refund_pending":               current.RefundPending,
			"execution_secrets_cleaned_at": current.ExecutionSecretsCleanedAt,
			"next_poll_at":                 current.NextPollAt,
			"updated_at":                   updatedAt,
		}).Error; err != nil {
			return err
		}
		t.Quota = current.Quota
		t.FailReason = current.FailReason
		t.SettlementStatus = current.SettlementStatus
		t.RefundPending = current.RefundPending
		t.ExecutionSecretsCleanedAt = current.ExecutionSecretsCleanedAt
		t.PrivateData = current.PrivateData
		t.NextPollAt = current.NextPollAt
		t.UpdatedAt = updatedAt
		return nil
	})
}

// UpdateTaskPrivateData applies a mutation to private_data while holding the
// task row lock and advancing updated_at. It is the safe boundary for small
// protocol/artifact writers that must not replace a stale whole Task row.
func UpdateTaskPrivateData(id int64, mutate func(*TaskPrivateData) error) error {
	if id <= 0 {
		return fmt.Errorf("update task private data failed, id=%d", id)
	}
	if mutate == nil {
		return errors.New("update task private data failed, mutation is nil")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var current Task
		if err := lockForUpdate(tx).
			Select("id", "private_data", "updated_at").
			Where("id = ?", id).
			First(&current).Error; err != nil {
			return err
		}
		if err := mutate(&current.PrivateData); err != nil {
			return err
		}
		updatedAt := nextTaskUpdatedAt(current.UpdatedAt)
		result := tx.Model(&Task{}).
			Where("id = ?", id).
			Updates(map[string]any{
				"private_data": current.PrivateData,
				"updated_at":   updatedAt,
			})
		if result.Error != nil {
			return result.Error
		}
		return nil
	})
}

// UpdateTaskAfterSubmitAccountingFailure persists only the terminal/billing
// fields owned by the submitter. The current private_data is re-read under a
// row lock so provider results, plugin state, and artifact references survive
// a concurrent accounting failure.
func UpdateTaskAfterSubmitAccountingFailure(t *Task) error {
	if t == nil || t.ID <= 0 {
		return fmt.Errorf("fail task after submit settlement error failed, task is invalid")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var current Task
		if err := lockForUpdate(tx).Where("id = ?", t.ID).First(&current).Error; err != nil {
			return err
		}
		current.Quota = t.Quota
		current.Status = t.Status
		current.Progress = t.Progress
		current.FinishTime = t.FinishTime
		current.FailReason = t.FailReason
		current.SettlementStatus = t.SettlementStatus
		current.RefundPending = t.RefundPending
		current.PrivateData.SettlementAttemptQuota = t.PrivateData.SettlementAttemptQuota
		current.PrivateData.SettlementError = t.PrivateData.SettlementError
		current.PrivateData.PreConsumedUsageCaptured = t.PrivateData.PreConsumedUsageCaptured
		current.PrivateData.PreConsumedUsageRecorded = t.PrivateData.PreConsumedUsageRecorded
		updatedAt := nextTaskUpdatedAt(current.UpdatedAt)
		if err := tx.Model(&Task{}).Where("id = ?", t.ID).Updates(map[string]any{
			"quota":             current.Quota,
			"status":            current.Status,
			"progress":          current.Progress,
			"finish_time":       current.FinishTime,
			"fail_reason":       current.FailReason,
			"private_data":      current.PrivateData,
			"settlement_status": current.SettlementStatus,
			"refund_pending":    current.RefundPending,
			"updated_at":        updatedAt,
		}).Error; err != nil {
			return err
		}
		*t = current
		t.UpdatedAt = updatedAt
		return nil
	})
}

// ForceTaskRefundableAfterSubmitAccountingFailure writes the refundable failure
// columns including usage flags in private_data. It is the last-resort persist
// when the locked private_data merge cannot be written, so the 15s settlement
// sweeper cannot SETTLED-at-prepaid a leftover SUCCESS row or double-decrement
// used_quota after a successful usage rollback.
func ForceTaskRefundableAfterSubmitAccountingFailure(t *Task) error {
	if t == nil || t.ID <= 0 {
		return fmt.Errorf("force refundable persist failed, task is invalid")
	}
	updatedAt := nextTaskUpdatedAt(t.UpdatedAt)
	result := DB.Model(&Task{}).Where("id = ?", t.ID).Updates(map[string]any{
		"quota":             t.Quota,
		"status":            t.Status,
		"progress":          t.Progress,
		"finish_time":       t.FinishTime,
		"fail_reason":       t.FailReason,
		"private_data":      t.PrivateData,
		"settlement_status": t.SettlementStatus,
		"refund_pending":    t.RefundPending,
		"updated_at":        updatedAt,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("force refundable persist failed, taskId=%s, id=%d", t.TaskID, t.ID)
	}
	t.UpdatedAt = updatedAt
	return nil
}

func taskRowExists(id int64) (bool, error) {
	var count int64
	if err := DB.Model(&Task{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func ClaimTaskLease(id int64, owner string, now int64, leaseSeconds int64) (*Task, bool, error) {
	if id <= 0 || owner == "" || leaseSeconds <= 0 {
		return nil, false, nil
	}
	lockUntil := now + leaseSeconds
	result := imageTaskDueQuery(runnableImageTaskStatusQuery(DB.Model(&Task{})), now).
		Where("id = ?", id).
		Where("platform = ?", constant.TaskPlatformImage).
		Updates(map[string]any{
			"lock_owner": owner,
			"lock_until": lockUntil,
			"updated_at": now,
		})
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, false, nil
	}

	var task Task
	if err := DB.Where("id = ?", id).First(&task).Error; err != nil {
		return nil, false, err
	}
	return &task, true, nil
}

func ReleaseTaskLease(id int64, owner string, nextPollAt int64, retryCount int) error {
	if id <= 0 || owner == "" {
		return nil
	}
	if retryCount < 0 {
		retryCount = 0
	}
	now := time.Now().Unix()
	return DB.Model(&Task{}).
		Where("id = ? AND lock_owner = ?", id, owner).
		Updates(map[string]any{
			"lock_owner":   "",
			"lock_until":   0,
			"next_poll_at": nextPollAt,
			"retry_count":  retryCount,
			"updated_at":   now,
		}).Error
}

func BackoffUnlockedImageTasksByIDs(ids []int64, nextPollAt int64) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now().Unix()
	return imageTaskDueQuery(runnableImageTaskStatusQuery(DB.Model(&Task{})), now).
		Where("id IN ?", ids).
		Where("platform = ?", constant.TaskPlatformImage).
		Where("(lock_owner = '' OR lock_owner IS NULL)").
		Updates(map[string]any{
			"next_poll_at": nextPollAt,
			"updated_at":   now,
		}).Error
}

func RenewTaskLease(id int64, owner string, now int64, leaseSeconds int64) (bool, error) {
	if id <= 0 || owner == "" || leaseSeconds <= 0 {
		return false, nil
	}
	result := runnableImageTaskStatusQuery(DB.Model(&Task{})).
		Where("id = ? AND lock_owner = ?", id, owner).
		Where("platform = ?", constant.TaskPlatformImage).
		Where("COALESCE(lock_until, 0) > ?", now).
		Updates(map[string]any{
			"lock_until": now + leaseSeconds,
			"updated_at": now,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func MarkImageTaskSyncSubmissionStarted(id int64, owner string, now int64, startedAt int64) (bool, error) {
	if id <= 0 || startedAt <= 0 {
		return false, nil
	}
	if now <= 0 {
		now = time.Now().Unix()
	}
	owner = strings.TrimSpace(owner)
	query := DB.Model(&Task{}).
		Where("id = ? AND platform = ? AND status = ? AND COALESCE(sync_submission_started_at, 0) = 0", id, constant.TaskPlatformImage, TaskStatusInProgress)
	if owner != "" {
		query = query.
			Where("lock_owner = ?", owner).
			Where("COALESCE(lock_until, 0) > ?", now)
	} else {
		query = query.
			Where("(lock_owner = '' OR lock_owner IS NULL)").
			Where("COALESCE(lock_until, 0) = 0")
	}
	result := query.
		Updates(map[string]any{
			"sync_submission_started_at": startedAt,
			"updated_at":                 now,
		})
	return result.RowsAffected > 0, result.Error
}

func (t *Task) UpdateWithStatusAndLease(fromStatus TaskStatus, lockOwner string, now int64) (bool, error) {
	if lockOwner == "" {
		return t.UpdateWithStatus(fromStatus)
	}
	return t.updateWithStatusFrom(t.updateBase, fromStatus, lockOwner, now, true)
}

func (t *Task) UpdateSettlementStatus(fromStatus TaskStatus, fromSettlementStatus string) (bool, error) {
	if t == nil || t.ID <= 0 {
		return false, nil
	}
	won := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var current Task
		if err := lockForUpdate(tx).
			Where("id = ? AND status = ? AND settlement_status = ?", t.ID, fromStatus, fromSettlementStatus).
			First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}

		privateData := t.PrivateData
		if t.updateBase != nil {
			privateData = mergeTaskPrivateData(current.PrivateData, t.updateBase.PrivateData, t.PrivateData)
		}
		privateData.ResultBodyPath = current.PrivateData.ResultBodyPath
		privateData.ResultBodySize = current.PrivateData.ResultBodySize
		privateData.ResultBodySHA256 = current.PrivateData.ResultBodySHA256
		privateData.ResultContentType = current.PrivateData.ResultContentType
		privateData.ResultStoredAt = current.PrivateData.ResultStoredAt
		privateData.ResultExpiresAt = current.PrivateData.ResultExpiresAt
		privateData.ResultURL = current.PrivateData.ResultURL

		resultExpiresAt := current.ResultExpiresAt
		if current.ResultCleanedAt == 0 {
			if resultExpiresAt == 0 || (t.ResultExpiresAt > 0 && t.ResultExpiresAt < resultExpiresAt) {
				resultExpiresAt = t.ResultExpiresAt
			}
			if privateData.ResultExpiresAt == 0 || (t.PrivateData.ResultExpiresAt > 0 && t.PrivateData.ResultExpiresAt < privateData.ResultExpiresAt) {
				privateData.ResultExpiresAt = t.PrivateData.ResultExpiresAt
			}
		}

		requestCleanupPending := t.RequestCleanupPending
		requestDeleteAfter := t.RequestDeleteAfter
		requestWasFinalized := strings.TrimSpace(t.PrivateData.RequestBodyPath) != "" && strings.TrimSpace(current.PrivateData.RequestBodyPath) == ""
		if current.RequestCleanupPending || requestWasFinalized {
			privateData.RequestBodyPath = current.PrivateData.RequestBodyPath
			privateData.RequestBodyBase64 = current.PrivateData.RequestBodyBase64
			privateData.RequestBodyPortable = current.PrivateData.RequestBodyPortable
			privateData.RequestBodyShared = current.PrivateData.RequestBodyShared
			privateData.RequestBodySize = current.PrivateData.RequestBodySize
			requestCleanupPending = current.RequestCleanupPending
			requestDeleteAfter = current.RequestDeleteAfter
		}

		updatedAt := common.GetTimestamp()
		result := tx.Model(&Task{}).
			Where("id = ? AND status = ? AND settlement_status = ?", t.ID, fromStatus, fromSettlementStatus).
			Updates(map[string]any{
				"settlement_status":            t.SettlementStatus,
				"fail_reason":                  t.FailReason,
				"next_poll_at":                 t.NextPollAt,
				"lock_owner":                   t.LockOwner,
				"lock_until":                   t.LockUntil,
				"retry_count":                  t.RetryCount,
				"private_data":                 privateData,
				"request_cleanup_pending":      requestCleanupPending,
				"request_delete_after":         requestDeleteAfter,
				"execution_secrets_cleaned_at": t.ExecutionSecretsCleanedAt,
				"result_expires_at":            resultExpiresAt,
				"updated_at":                   updatedAt,
			})
		if result.Error != nil {
			return result.Error
		}
		won = true
		t.PrivateData = privateData
		t.RequestCleanupPending = requestCleanupPending
		t.RequestDeleteAfter = requestDeleteAfter
		t.ResultExpiresAt = resultExpiresAt
		t.ResultAcknowledgedAt = current.ResultAcknowledgedAt
		t.ResultDeleteAfter = current.ResultDeleteAfter
		t.ResultCleanedAt = current.ResultCleanedAt
		t.ResultCleanupPending = current.ResultCleanupPending
		t.ImageTaskResultStored = current.ImageTaskResultStored
		t.Data = current.Data
		t.UpdatedAt = updatedAt
		return nil
	})
	return won, err
}

// TaskBulkUpdate performs an unconditional bulk UPDATE by upstream task_id strings.
// Same caveats as TaskBulkUpdateByID — no CAS guard.
func TaskBulkUpdate(taskIds []string, params map[string]any) error {
	if len(taskIds) == 0 {
		return nil
	}
	return DB.Model(&Task{}).
		Where("task_id in (?)", taskIds).
		Updates(params).Error
}

// TaskBulkUpdateByID performs an unconditional bulk UPDATE by primary key IDs.
// WARNING: This function has NO CAS (Compare-And-Swap) guard — it will overwrite
// any concurrent status changes. DO NOT use in billing/quota lifecycle flows
// (e.g., timeout, success, failure transitions that trigger refunds or settlements).
// For status transitions that involve billing, use Task.UpdateWithStatus() instead.
func TaskBulkUpdateByID(ids []int64, params map[string]any) error {
	if len(ids) == 0 {
		return nil
	}
	return DB.Model(&Task{}).
		Where("id in (?)", ids).
		Updates(params).Error
}

type TaskQuotaUsage struct {
	Mode  string  `json:"mode"`
	Count float64 `json:"count"`
}

// TaskCountAllTasks returns total tasks that match the given query params (admin usage)
func TaskCountAllTasks(queryParams SyncTaskQueryParams) int64 {
	var total int64
	query := DB.Model(&Task{})
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.UserID != "" {
		query = query.Where("user_id = ?", queryParams.UserID)
	}
	if len(queryParams.UserIDs) != 0 {
		query = query.Where("user_id in (?)", queryParams.UserIDs)
	}
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = applyTaskStatusQuery(query, queryParams.Status)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}

// TaskCountAllUserTask returns total tasks for given user
func TaskCountAllUserTask(userId int, queryParams SyncTaskQueryParams) int64 {
	var total int64
	query := DB.Model(&Task{}).Where("user_id = ?", userId)
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = applyTaskStatusQuery(query, queryParams.Status)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}
func (t *Task) ToOpenAIVideo() *dto.OpenAIVideo {
	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = t.TaskID
	openAIVideo.Status = t.PublicStatus().ToVideoStatus()
	openAIVideo.Model = t.Properties.OriginModelName
	openAIVideo.SetProgressStr(t.PublicProgress())
	openAIVideo.CreatedAt = t.CreatedAt
	if t.PublicStatus() == TaskStatusSuccess {
		if t.FinishTime > 0 {
			openAIVideo.CompletedAt = t.FinishTime
		} else {
			openAIVideo.CompletedAt = t.UpdatedAt
		}
		if resultURL := strings.TrimSpace(t.PublicResultURL()); resultURL != "" {
			openAIVideo.SetMetadata("url", resultURL)
		}
	}
	return openAIVideo
}
