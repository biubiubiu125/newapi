package model

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	commonRelay "github.com/QuantumNous/new-api/relay/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TaskStatus string

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

type Task struct {
	ID               int64                 `json:"id" gorm:"primary_key;AUTO_INCREMENT;index:idx_task_image_dispatch,priority:6;index:idx_task_image_settlement_dispatch,priority:7;index:idx_task_image_node_dispatch,priority:7;index:idx_task_image_node_settlement,priority:8"`
	CreatedAt        int64                 `json:"created_at" gorm:"index"`
	UpdatedAt        int64                 `json:"updated_at"`
	TaskID           string                `json:"task_id" gorm:"type:varchar(191);index"`                                                                                                                                                                                                                               // 第三方id，不一定有/ song id\ Task id
	Platform         constant.TaskPlatform `json:"platform" gorm:"type:varchar(30);index;index:idx_task_dispatch,priority:1;index:idx_task_image_dispatch,priority:1;index:idx_task_image_settlement_dispatch,priority:1;index:idx_task_image_node_dispatch,priority:1;index:idx_task_image_node_settlement,priority:1"` // 平台
	UserId           int                   `json:"user_id" gorm:"index"`
	ClientTaskID     string                `json:"client_task_id,omitempty" gorm:"type:varchar(191);index"`
	Group            string                `json:"group" gorm:"type:varchar(50)"` // 修正计费用
	ChannelId        int                   `json:"channel_id" gorm:"index;index:idx_task_image_dispatch,priority:4;index:idx_task_image_settlement_dispatch,priority:6;index:idx_task_image_node_dispatch,priority:6;index:idx_task_image_node_settlement,priority:7"`
	Quota            int                   `json:"quota"`
	Action           string                `json:"action" gorm:"type:varchar(40);index"`                                                                                                                                                                                                                               // 任务类型, song, lyrics, description-mode
	Status           TaskStatus            `json:"status" gorm:"type:varchar(20);index;index:idx_task_dispatch,priority:2;index:idx_task_image_dispatch,priority:2;index:idx_task_image_settlement_dispatch,priority:2;index:idx_task_image_node_dispatch,priority:2;index:idx_task_image_node_settlement,priority:2"` // 任务状态
	FailReason       string                `json:"fail_reason"`
	SubmitTime       int64                 `json:"submit_time" gorm:"index"`
	StartTime        int64                 `json:"start_time" gorm:"index"`
	FinishTime       int64                 `json:"finish_time" gorm:"index"`
	Progress         string                `json:"progress" gorm:"type:varchar(20);index"`
	NextPollAt       int64                 `json:"next_poll_at" gorm:"index;index:idx_task_dispatch,priority:3;index:idx_task_image_dispatch,priority:3;index:idx_task_image_settlement_dispatch,priority:4;index:idx_task_image_node_dispatch,priority:4;index:idx_task_image_node_settlement,priority:5"`
	LockUntil        int64                 `json:"lock_until" gorm:"index;index:idx_task_dispatch,priority:4;index:idx_task_image_dispatch,priority:5;index:idx_task_image_settlement_dispatch,priority:5;index:idx_task_image_node_dispatch,priority:5;index:idx_task_image_node_settlement,priority:6"`
	LockOwner        string                `json:"lock_owner" gorm:"type:varchar(128);index"`
	StorageNode      string                `json:"storage_node,omitempty" gorm:"type:varchar(128);index;index:idx_task_image_node_dispatch,priority:3;index:idx_task_image_node_settlement,priority:4"`
	RetryCount       int                   `json:"retry_count"`
	SettlementStatus string                `json:"-" gorm:"type:varchar(20);index;index:idx_task_image_settlement_dispatch,priority:3;index:idx_task_image_node_settlement,priority:3"`
	Properties       Properties            `json:"properties" gorm:"type:json"`
	Username         string                `json:"username,omitempty" gorm:"-"`
	// 禁止返回给用户，内部可能包含key等隐私信息
	PrivateData TaskPrivateData `json:"-" gorm:"column:private_data;type:json"`
	Data        json.RawMessage `json:"data" gorm:"type:json"`
}

func (t *Task) SetData(data any) {
	b, _ := common.Marshal(data)
	t.Data = json.RawMessage(b)
}

func (t *Task) GetData(v any) error {
	return common.Unmarshal(t.Data, &v)
}

type Properties struct {
	Input             string `json:"input"`
	UpstreamModelName string `json:"upstream_model_name,omitempty"`
	OriginModelName   string `json:"origin_model_name,omitempty"`
}

func (m *Properties) Scan(val interface{}) error {
	bytesValue, _ := val.([]byte)
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
	return common.Marshal(m)
}

type TaskPrivateData struct {
	ImageTaskMode                string            `json:"image_task_mode,omitempty"`
	RequestPath                  string            `json:"request_path,omitempty"`
	RequestMethod                string            `json:"request_method,omitempty"`
	RequestContentType           string            `json:"request_content_type,omitempty"`
	RequestHeaders               map[string]string `json:"request_headers,omitempty"`
	RequestBodyPath              string            `json:"request_body_path,omitempty"`
	RequestBodyBase64            string            `json:"request_body_base64,omitempty"`
	RequestBodyPortable          bool              `json:"request_body_portable,omitempty"`
	RequestBodySize              int64             `json:"request_body_size,omitempty"`
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
	// 计费上下文：用于异步退款/差额结算（轮询阶段读取）
	BillingSource          string                       `json:"billing_source,omitempty"`  // "wallet" 或 "subscription"
	SubscriptionId         int                          `json:"subscription_id,omitempty"` // 订阅 ID，用于订阅退款
	TokenId                int                          `json:"token_id,omitempty"`        // 令牌 ID，用于令牌额度退款
	NodeName               string                       `json:"node_name,omitempty"`       // 发起任务的节点名，轮询结算阶段据此归属日志
	BillingContext         *TaskBillingContext          `json:"billing_context,omitempty"` // 计费参数快照（用于轮询阶段重新计算）
	TieredBillingSnapshot  *billingexpr.BillingSnapshot `json:"tiered_billing_snapshot,omitempty"`
	BillingRequestInput    *billingexpr.RequestInput    `json:"billing_request_input,omitempty"`
	SettlementUsage        *dto.Usage                   `json:"settlement_usage,omitempty"`
	SettlementExtraContent []string                     `json:"settlement_extra_content,omitempty"`
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
// 新数据存在 PrivateData.ResultURL 中；旧数据回退到 FailReason（历史兼容）
func (t *Task) GetResultURL() string {
	if t.PrivateData.ResultURL != "" {
		return t.PrivateData.ResultURL
	}
	return t.FailReason
}

// GenerateTaskID 生成对外暴露的 task_xxxx 格式 ID
func GenerateTaskID() string {
	key, _ := common.GenerateRandomCharsKey(32)
	return "task_" + key
}

func (p *TaskPrivateData) Scan(val interface{}) error {
	bytesValue, _ := val.([]byte)
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
	return b, nil
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
		if relayInfo.ChannelMeta.ChannelType == constant.ChannelTypeGemini ||
			relayInfo.ChannelMeta.ChannelType == constant.ChannelTypeVertexAi {
			privateData.Key = relayInfo.ChannelMeta.ApiKey
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
			"status = ? AND NOT (platform = ? AND COALESCE(settlement_status, '') = ?)",
			TaskStatusSuccess,
			constant.TaskPlatformImage,
			TaskSettlementStatusReview,
		)
	case TaskStatusFailure:
		return query.Where(
			"(status = ? OR (platform = ? AND status = ? AND settlement_status = ?))",
			TaskStatusFailure,
			constant.TaskPlatformImage,
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
	nonImageTasks := getRunnableNonImageTasks(limit-imageReserve, nil)
	imageLimit := runnableImageTaskQueryLimit(limit)
	remainingSlots := limit - len(nonImageTasks)
	if imageLimit > remainingSlots {
		imageLimit = remainingSlots
	}
	imageTasks := getRunnableImageTasksFair(imageLimit, now)
	total := len(nonImageTasks) + len(imageTasks)
	if total < limit {
		nonImageTasks = append(nonImageTasks, getRunnableNonImageTasks(limit-total, taskPrimaryIDs(nonImageTasks))...)
	}

	tasks := make([]*Task, 0, len(nonImageTasks)+len(imageTasks))
	tasks = append(tasks, nonImageTasks...)
	tasks = append(tasks, imageTasks...)
	return tasks
}

func GetRunnableNonImageSyncTasks(limit int) []*Task {
	return getRunnableNonImageTasks(limit, nil)
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

func getRunnableNonImageTasks(limit int, excludeIDs []int64) []*Task {
	if limit <= 0 {
		return nil
	}
	query := DB.Where(taskUnfinishedProgressWhere, "100%").
		Where("status NOT IN ?", []TaskStatus{TaskStatusFailure, TaskStatusSuccess}).
		Where("platform <> ?", constant.TaskPlatformImage)
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

func runnableImageTaskStatusQuery(query *gorm.DB) *gorm.DB {
	return query.Where(
		"(status NOT IN ? OR (status = ? AND settlement_status IN ?))",
		[]TaskStatus{TaskStatusFailure, TaskStatusSuccess},
		TaskStatusSuccess,
		[]string{TaskSettlementStatusPending, TaskSettlementStatusApplied},
	)
}

type runnableImageTaskChannel struct {
	ChannelId int `gorm:"column:channel_id"`
}

const runnableImageTaskDueWhere = "(next_poll_at <= ? OR next_poll_at IS NULL) AND (lock_until <= ? OR lock_until IS NULL)"

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
	)
	args = append(args, nodeArgs...)
	args = append(args, perChannelLimit)

	var rows []runnableImageTaskRow
	err := DB.Raw(`
SELECT id, channel_id FROM (
  SELECT id, channel_id, ROW_NUMBER() OVER (PARTITION BY channel_id ORDER BY id ASC) AS rn
  FROM (
    SELECT id, channel_id FROM tasks
    WHERE platform = ? AND channel_id IN ? AND status NOT IN (?, ?) AND `+runnableImageTaskDueWhere+nodeWhere+`
    UNION
    SELECT id, channel_id FROM tasks
    WHERE platform = ? AND channel_id IN ? AND status = ? AND settlement_status IN (?, ?) AND `+runnableImageTaskDueWhere+nodeWhere+`
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
			if task := loadedByID[id]; task != nil {
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
	)
	args = append(args, nodeArgs...)
	args = append(args, cursor, limit)
	if err := DB.Raw(`
SELECT channel_id FROM (
  SELECT DISTINCT channel_id FROM tasks
  WHERE platform = ? AND status NOT IN (?, ?) AND `+runnableImageTaskDueWhere+nodeWhere+`
  UNION
  SELECT DISTINCT channel_id FROM tasks
  WHERE platform = ? AND status = ? AND settlement_status IN (?, ?) AND `+runnableImageTaskDueWhere+nodeWhere+`
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
	)
	args = append(args, nodeArgs...)
	args = append(args, limit)
	err := DB.Raw(`
  SELECT id FROM (
    SELECT id FROM tasks
  WHERE platform = ? AND channel_id = ? AND status NOT IN (?, ?) AND `+runnableImageTaskDueWhere+nodeWhere+`
  UNION
  SELECT id FROM tasks
  WHERE platform = ? AND channel_id = ? AND status = ? AND settlement_status IN (?, ?) AND `+runnableImageTaskDueWhere+nodeWhere+`
) AS runnable_tasks
ORDER BY id ASC
LIMIT ?`,
		args...,
	).Scan(&ids).Error
	if err != nil || len(ids) == 0 {
		return nil
	}
	return getTasksByIDsPreserveOrder(ids)
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
	var id int64
	err := imageTaskRunnableNodeQuery(imageTaskDueQuery(DB.Model(&Task{}), now)).
		Where("platform = ?", constant.TaskPlatformImage).
		Where("status = ?", TaskStatusSuccess).
		Where("settlement_status IN ?", []string{TaskSettlementStatusPending, TaskSettlementStatusApplied}).
		Limit(1).
		Pluck("id", &id).Error
	return err == nil && id != 0
}

func GetNextRunnableImageTaskAt(now int64) (int64, bool) {
	nextAt, ok := getNextRunnableImageTaskAtForQuery(imageTaskRunnableNodeQuery(imageTaskUnfinishedBaseQuery(DB.Model(&Task{}))), now)
	settlementNextAt, settlementOK := getNextRunnableImageTaskAtForQuery(imageTaskRunnableNodeQuery(imageTaskSettlementBaseQuery(DB.Model(&Task{}))), now)
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
		Where("settlement_status IN ?", []string{TaskSettlementStatusPending, TaskSettlementStatusApplied})
}

func imageTaskDueQuery(query *gorm.DB, now int64) *gorm.DB {
	return query.Where("(next_poll_at <= ? OR next_poll_at IS NULL)", now).
		Where("(lock_until <= ? OR lock_until IS NULL)", now)
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
	if (bodyCandidates != nil || resultCandidates != nil) && len(candidateNames) == 0 {
		return bodyPaths, resultPaths, nil
	}
	collect := func(query *gorm.DB) error {
		var tasks []Task
		return query.FindInBatches(&tasks, batchSize, func(tx *gorm.DB, batch int) error {
			for i := range tasks {
				addImageTaskCachePathForCandidates(bodyPaths, tasks[i].PrivateData.RequestBodyPath, bodyCandidates)
				addImageTaskCachePathForCandidates(resultPaths, tasks[i].PrivateData.ResultBodyPath, resultCandidates)
			}
			return nil
		}).Error
	}
	if len(candidateNames) == 0 {
		if err := collect(openImageTaskCachePathQuery(DB.Model(&Task{}))); err != nil {
			return nil, nil, err
		}
		return bodyPaths, resultPaths, nil
	}
	for _, names := range chunkImageTaskCacheCandidateNames(candidateNames, 50) {
		if err := collect(applyImageTaskPrivateDataCandidateFilter(openImageTaskCachePathQuery(DB.Model(&Task{})), names)); err != nil {
			return nil, nil, err
		}
	}
	return bodyPaths, resultPaths, nil
}

func openImageTaskCachePathQuery(query *gorm.DB) *gorm.DB {
	return query.
		Select("id, status, settlement_status, private_data").
		Where("platform = ?", constant.TaskPlatformImage).
		Where(
			"(status NOT IN ? OR (status = ? AND settlement_status IN ?))",
			[]TaskStatus{TaskStatusFailure, TaskStatusSuccess},
			TaskStatusSuccess,
			[]string{TaskSettlementStatusPending, TaskSettlementStatusApplied, TaskSettlementStatusReview},
		)
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
	column := "CAST(private_data AS TEXT)"
	switch common.MainDatabaseType() {
	case common.DatabaseTypeMySQL:
		column = "CAST(private_data AS CHAR)"
	case common.DatabaseTypePostgreSQL:
		column = "private_data::text"
	}
	clauses := make([]string, 0, len(names))
	args := make([]any, 0, len(names))
	for _, name := range names {
		clauses = append(clauses, column+" LIKE ?")
		args = append(args, "%"+name+"%")
	}
	return query.Where("("+strings.Join(clauses, " OR ")+")", args...)
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
	var task *Task
	var err error
	err = DB.Where("task_id = ?", taskId).First(&task).Error
	exist, err := RecordExist(err)
	if err != nil {
		return nil, false, err
	}
	return task, exist, err
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
	var task *Task
	var err error
	err = DB.Where("user_id = ? and task_id = ?", userId, taskId).
		First(&task).Error
	exist, err := RecordExist(err)
	if err != nil {
		return nil, false, err
	}
	return task, exist, err
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

func GetImageTaskByClientTaskID(userId int, clientTaskID string) (*Task, bool, error) {
	clientTaskID = strings.TrimSpace(clientTaskID)
	if userId <= 0 || clientTaskID == "" {
		return nil, false, nil
	}
	var task *Task
	err := DB.Where("user_id = ? AND platform = ? AND client_task_id = ?", userId, constant.TaskPlatformImage, clientTaskID).
		Order("id ASC").
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

func (Task *Task) Insert() error {
	var err error
	err = DB.Create(Task).Error
	return err
}

func DeleteTaskByID(id int64) error {
	if id <= 0 {
		return nil
	}
	return DB.Where("id = ?", id).Delete(&Task{}).Error
}

type taskSnapshot struct {
	Status     TaskStatus
	Progress   string
	StartTime  int64
	FinishTime int64
	FailReason string
	ResultURL  string
	Data       json.RawMessage
}

func (s taskSnapshot) Equal(other taskSnapshot) bool {
	return s.Status == other.Status &&
		s.Progress == other.Progress &&
		s.StartTime == other.StartTime &&
		s.FinishTime == other.FinishTime &&
		s.FailReason == other.FailReason &&
		s.ResultURL == other.ResultURL &&
		bytes.Equal(s.Data, other.Data)
}

func (t *Task) Snapshot() taskSnapshot {
	return taskSnapshot{
		Status:     t.Status,
		Progress:   t.Progress,
		StartTime:  t.StartTime,
		FinishTime: t.FinishTime,
		FailReason: t.FailReason,
		ResultURL:  t.PrivateData.ResultURL,
		Data:       t.Data,
	}
}

func (Task *Task) Update() error {
	var err error
	err = DB.Save(Task).Error
	return err
}

func (t *Task) UpdateQuota() error {
	return DB.Model(t).Update("quota", t.Quota).Error
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

// UpdateWithStatus performs a conditional UPDATE guarded by fromStatus (CAS).
// Returns (true, nil) if this caller won the update, (false, nil) if
// another process already moved the task out of fromStatus.
//
// Uses Model().Select("*").Updates() instead of Save() because GORM's Save
// falls back to INSERT ON CONFLICT when the WHERE-guarded UPDATE matches
// zero rows, which silently bypasses the CAS guard.
func (t *Task) UpdateWithStatus(fromStatus TaskStatus) (bool, error) {
	result := DB.Model(t).Where("status = ?", fromStatus).Select("*").Updates(t)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (t *Task) UpdateWithStatusAndLease(fromStatus TaskStatus, lockOwner string, now int64) (bool, error) {
	if lockOwner == "" {
		return t.UpdateWithStatus(fromStatus)
	}
	query := DB.Model(t).
		Where("status = ?", fromStatus).
		Where("lock_owner = ?", lockOwner).
		Where("COALESCE(lock_until, 0) > ?", now)
	if t.Status != TaskStatusSuccess && t.Status != TaskStatusFailure {
		query = query.Omit("lock_owner", "lock_until")
	}
	result := query.Select("*").Updates(t)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (t *Task) UpdateSettlementStatus(fromStatus TaskStatus, fromSettlementStatus string) (bool, error) {
	result := DB.Model(t).
		Where("status = ?", fromStatus).
		Where("settlement_status = ?", fromSettlementStatus).
		Select("*").
		Updates(t)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
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
	openAIVideo.Status = t.Status.ToVideoStatus()
	openAIVideo.Model = t.Properties.OriginModelName
	openAIVideo.SetProgressStr(t.Progress)
	openAIVideo.CreatedAt = t.CreatedAt
	openAIVideo.CompletedAt = t.UpdatedAt
	openAIVideo.SetMetadata("url", t.GetResultURL())
	return openAIVideo
}
