package relay

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func withImageTaskAsyncTimeoutMinutes(t *testing.T, minutes int) {
	t.Helper()
	old := constant.TaskTimeoutMinutes
	constant.TaskTimeoutMinutes = minutes
	t.Cleanup(func() {
		constant.TaskTimeoutMinutes = old
	})
}

func TestCloneImageTaskStringMapDropsCredentialHeaders(t *testing.T) {
	headers := cloneImageTaskStringMap(map[string]string{
		"Authorization":      "Bearer auth-secret",
		"Mj-Api-Secret":      "sk-mj-secret",
		"X-Provider-Secret":  "provider-secret",
		"X-Auth-Token":       "auth-token",
		"X-Goog-Api-Key":     "goog-secret",
		"X-Request-Trace-Id": "trace-123",
	})

	require.Equal(t, map[string]string{"X-Request-Trace-Id": "trace-123"}, headers)
}

func TestTaskModel2DtoMarksImageSettlementReviewAsFailure(t *testing.T) {
	task := &model.Task{
		TaskID:           "task_review_dto",
		Platform:         constant.TaskPlatformImage,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusReview,
	}

	resp := TaskModel2Dto(task)

	require.Equal(t, string(model.TaskStatusFailure), resp.Status)
	require.Equal(t, model.TaskSettlementStatusReview, resp.SettlementStatus)
}

func TestTaskModel2DtoIncludesSettlementReviewDetails(t *testing.T) {
	task := &model.Task{
		TaskID:           "task_review_details_dto",
		Platform:         constant.TaskPlatformSuno,
		Status:           model.TaskStatusNotStart,
		SettlementStatus: model.TaskSettlementStatusReview,
	}
	task.PrivateData.SettlementAttemptQuota = 321
	task.PrivateData.SettlementError = "record consume log failed"

	resp := TaskModel2Dto(task)

	require.Equal(t, model.TaskSettlementStatusReview, resp.SettlementStatus)
	require.Equal(t, 321, resp.SettlementAttemptQuota)
	require.Equal(t, "record consume log failed", resp.SettlementError)
}

func TestParseAsyncTaskBridgeTaskResultSupportsKeyedDataMap(t *testing.T) {
	body := []byte(`{
		"data": {
			"upstream_123": {
				"task_id": "upstream_123",
				"status": "completed",
				"progress": 1,
				"result": {"data":[{"url":"https://example.com/a.png"}]}
			}
		}
	}`)

	result, err := parseAsyncTaskBridgeTaskResult(body, "upstream_123")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "upstream_123", result.TaskID)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), result.Status)
	require.Equal(t, "100%", result.Progress)
	require.JSONEq(t, `{"data":[{"url":"https://example.com/a.png"}]}`, string(result.Result))
}

func TestParseAsyncTaskBridgeTaskResultMatchesClientTaskID(t *testing.T) {
	body := []byte(`{
		"data": [
			{
				"task_id": "upstream_456",
				"client_task_id": "task_local_456",
				"status": "running",
				"progress": "42%"
			}
		]
	}`)

	result, err := parseAsyncTaskBridgeTaskResult(body, "task_local_456")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "upstream_456", result.TaskID)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), result.Status)
	require.Equal(t, "42%", result.Progress)
}

func TestParseAsyncTaskBridgeTaskResultTreatsDataObjectWithStatusAsTaskItem(t *testing.T) {
	body := []byte(`{
		"data": {
			"task_id": "upstream_data",
			"status": "success",
			"progress": "100%",
			"data": [{"url":"https://example.com/data.png"}]
		}
	}`)

	result, err := parseAsyncTaskBridgeTaskResult(body, "upstream_data")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "upstream_data", result.TaskID)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), result.Status)
	require.JSONEq(t, `{"data":[{"url":"https://example.com/data.png"}]}`, string(result.Result))
}

func TestParseAsyncTaskBridgeBatchTaskResultRejectsAnonymousItemForMultipleTasks(t *testing.T) {
	body := []byte(`{"items":[{"status":"failed","reason":"generation failed"}]}`)

	result, err := parseAsyncTaskBridgeBatchTaskResult(body, "upstream_a", 2)

	require.NoError(t, err)
	require.Nil(t, result)

	result, err = parseAsyncTaskBridgeBatchTaskResult(body, "upstream_a", 1)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), result.Status)
}

func TestParseAsyncTaskBridgeTaskResultFromStorageRejectsSuccessWithoutResult(t *testing.T) {
	storage, err := common.CreateBodyStorage([]byte(`{"items":[{"task_id":"upstream_empty","status":"success"}]}`))
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })

	result, err := parseAsyncTaskBridgeTaskResultFromStorage(storage, "upstream_empty")

	require.ErrorContains(t, err, "success result is missing")
	require.Nil(t, result)
}

func TestRunAsyncTaskBridgeImageTaskBatchPollsStatusWithoutImageData(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}))

	oldDB := model.DB
	oldUsingSQLite := common.UsingSQLite
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldBatchSize := constant.ImageTaskBatchPollSize
	model.DB = db
	common.UsingSQLite = true
	common.MemoryCacheEnabled = false
	constant.ImageTaskBatchPollSize = 20
	t.Cleanup(func() {
		model.DB = oldDB
		common.UsingSQLite = oldUsingSQLite
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		constant.ImageTaskBatchPollSize = oldBatchSize
		_ = sqlDB.Close()
	})

	var mu sync.Mutex
	requests := make([]url.Values, 0)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/image-tasks", r.URL.Path)
		mu.Lock()
		requests = append(requests, r.URL.Query())
		mu.Unlock()
		require.Equal(t, "false", r.URL.Query().Get("include_image_data"))
		require.ElementsMatch(t, []string{"upstream_a", "upstream_b"}, strings.Split(r.URL.Query().Get("ids"), ","))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"items": [
				{"task_id": "upstream_a", "status": "running", "progress": "25%"},
				{"task_id": "upstream_b", "status": "queued", "progress": "0%"}
			]
		}`))
	}))
	defer upstream.Close()

	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      1,
		Type:    constant.ChannelTypeOpenAI,
		Key:     "upstream-key",
		Status:  common.ChannelStatusEnabled,
		Name:    "async-task-bridge",
		Group:   "default",
		Models:  "gpt-image-1",
		BaseURL: &baseURL,
	}).Error)
	now := time.Now().Unix()
	tasks := []*model.Task{
		{
			TaskID:     "task_batch_status_a",
			Platform:   constant.TaskPlatformImage,
			UserId:     1,
			Group:      "default",
			ChannelId:  1,
			Action:     constant.TaskActionImageGeneration,
			Status:     model.TaskStatusSubmitted,
			Progress:   "0%",
			SubmitTime: now,
			StartTime:  now,
			Properties: model.Properties{OriginModelName: "gpt-image-1"},
			PrivateData: model.TaskPrivateData{
				ImageTaskMode:  dto.ImageTaskModeAsyncTaskBridge,
				UpstreamTaskID: "upstream_a",
				Key:            "upstream-key",
			},
		},
		{
			TaskID:     "task_batch_status_b",
			Platform:   constant.TaskPlatformImage,
			UserId:     1,
			Group:      "default",
			ChannelId:  1,
			Action:     constant.TaskActionImageGeneration,
			Status:     model.TaskStatusSubmitted,
			Progress:   "0%",
			SubmitTime: now,
			StartTime:  now,
			Properties: model.Properties{OriginModelName: "gpt-image-1"},
			PrivateData: model.TaskPrivateData{
				ImageTaskMode:  dto.ImageTaskModeAsyncTaskBridge,
				UpstreamTaskID: "upstream_b",
				Key:            "upstream-key",
			},
		},
	}
	require.NoError(t, db.Create(tasks[0]).Error)
	require.NoError(t, db.Create(tasks[1]).Error)

	err = RunImageTasks(context.Background(), tasks)

	require.NoError(t, err)
	mu.Lock()
	require.Len(t, requests, 1)
	mu.Unlock()
	var reloadedA model.Task
	var reloadedB model.Task
	require.NoError(t, db.Where("task_id = ?", "task_batch_status_a").First(&reloadedA).Error)
	require.NoError(t, db.Where("task_id = ?", "task_batch_status_b").First(&reloadedB).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), reloadedA.Status)
	require.Equal(t, "25%", reloadedA.Progress)
	require.Equal(t, model.TaskStatus(model.TaskStatusQueued), reloadedB.Status)
	require.Equal(t, "0%", reloadedB.Progress)
}

func TestRunAsyncTaskBridgeImageTaskBatchHTTPFailureKeepsQuotaForReview(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.Task{},
		&model.TaskSettlementRecord{},
		&model.User{},
		&model.Token{},
		&model.Channel{},
		&model.Log{},
		&model.QuotaData{},
		&model.TokenUsageDaily{},
	))

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldUsingSQLite := common.UsingSQLite
	oldRedisEnabled := common.RedisEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	model.DB = db
	model.LOG_DB = db
	common.UsingSQLite = true
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.UsingSQLite = oldUsingSQLite
		common.RedisEnabled = oldRedisEnabled
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		_ = sqlDB.Close()
	})

	var returnBusinessFailure atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if returnBusinessFailure.Load() {
			ids := strings.Split(r.URL.Query().Get("ids"), ",")
			items := make([]map[string]any, 0, len(ids))
			for _, id := range ids {
				items = append(items, map[string]any{
					"task_id": id,
					"status":  "failed",
					"error":   map[string]any{"message": "generation failed"},
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"poll forbidden"}}`))
	}))
	defer upstream.Close()

	require.NoError(t, db.Create(&model.User{
		Id:       1,
		Username: "batch-review-user",
		Password: "password123",
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    0,
	}).Error)
	require.NoError(t, db.Create(&model.Token{
		Id:          1,
		UserId:      1,
		Key:         "batch-review-token",
		Name:        "batch-review-token",
		Status:      common.TokenStatusEnabled,
		RemainQuota: 0,
		UsedQuota:   3600,
	}).Error)
	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      1,
		Type:    constant.ChannelTypeOpenAI,
		Key:     "upstream-key",
		Status:  common.ChannelStatusEnabled,
		Name:    "async-task-bridge",
		Group:   "default",
		Models:  "gpt-image-1",
		BaseURL: &baseURL,
	}).Error)

	now := time.Now().Unix()
	tasks := make([]*model.Task, 0, 2)
	for index, ids := range [][2]string{{"task_batch_review_a", "upstream_review_a"}, {"task_batch_review_b", "upstream_review_b"}} {
		task := &model.Task{
			TaskID:                 ids[0],
			Platform:               constant.TaskPlatformImage,
			UserId:                 1,
			Group:                  "default",
			ChannelId:              1,
			Action:                 constant.TaskActionImageGeneration,
			Status:                 model.TaskStatusSubmitted,
			Progress:               "0%",
			SubmitTime:             now - int64(index+1),
			StartTime:              now - int64(index+1),
			Quota:                  900,
			PublicImageTask:        true,
			PublicImageTaskTokenID: 1,
			Properties:             model.Properties{OriginModelName: "gpt-image-1"},
			PrivateData: model.TaskPrivateData{
				PublicImageTask: true,
				ImageTaskMode:   dto.ImageTaskModeAsyncTaskBridge,
				UpstreamTaskID:  ids[1],
				Key:             "upstream-key",
				BillingSource:   service.BillingSourceWallet,
				TokenId:         1,
			},
		}
		require.NoError(t, db.Create(task).Error)
		tasks = append(tasks, task)
	}

	err = runAsyncTaskBridgeImageTaskBatch(context.Background(), tasks)

	require.NoError(t, err)
	for _, task := range tasks {
		var reloaded model.Task
		require.NoError(t, db.First(&reloaded, task.ID).Error)
		require.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status)
		require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
		require.Equal(t, 900, reloaded.Quota)
		require.False(t, reloaded.RefundPending)
		require.NotEmpty(t, reloaded.PrivateData.UpstreamTaskID)
	}
	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	require.Zero(t, user.Quota, "poll failure must not refund wallet quota")
	var token model.Token
	require.NoError(t, db.First(&token, 1).Error)
	require.Zero(t, token.RemainQuota, "poll failure must not refund token quota")

	returnBusinessFailure.Store(true)
	newFailureTask := func(taskID string, upstreamID string) (*model.Task, string) {
		body := []byte(`{"model":"gpt-image-1","stream":false}`)
		bodyPath, writeErr := common.WriteImageTaskBodyCacheFile(body)
		require.NoError(t, writeErr)
		t.Cleanup(func() { _ = common.RemoveDiskCacheFile(bodyPath) })
		task := &model.Task{
			TaskID:     taskID,
			Platform:   constant.TaskPlatformImage,
			UserId:     1,
			Group:      "default",
			ChannelId:  1,
			Action:     constant.TaskActionImageGeneration,
			Status:     model.TaskStatusSubmitted,
			Progress:   "0%",
			SubmitTime: now,
			StartTime:  now,
			Quota:      900,
			Properties: model.Properties{OriginModelName: "gpt-image-1"},
			PrivateData: model.TaskPrivateData{
				ImageTaskMode:            dto.ImageTaskModeAsyncTaskBridge,
				RequestBodyPath:          bodyPath,
				RequestBodySize:          int64(len(body)),
				UpstreamTaskID:           upstreamID,
				Key:                      "upstream-key",
				BillingSource:            service.BillingSourceWallet,
				TokenId:                  1,
				PreConsumedUsageCaptured: true,
				PreConsumedUsageRecorded: false,
			},
		}
		require.NoError(t, db.Create(task).Error)
		return task, bodyPath
	}

	batchFailureTask, batchFailureBodyPath := newFailureTask("task_batch_business_failure", "upstream_batch_business_failure")
	require.NoError(t, runAsyncTaskBridgeImageTaskBatch(context.Background(), []*model.Task{batchFailureTask}))
	singleFailureTask, singleFailureBodyPath := newFailureTask("task_single_business_failure", "upstream_single_business_failure")
	require.NoError(t, pollAsyncTaskBridgeImageTask(context.Background(), singleFailureTask))

	for _, expected := range []struct {
		task     *model.Task
		bodyPath string
	}{{batchFailureTask, batchFailureBodyPath}, {singleFailureTask, singleFailureBodyPath}} {
		var reloaded model.Task
		require.NoError(t, db.First(&reloaded, expected.task.ID).Error)
		require.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status)
		require.Empty(t, reloaded.SettlementStatus)
		require.Zero(t, reloaded.Quota)
		require.False(t, reloaded.RefundPending)
		require.Contains(t, reloaded.FailReason, "generation failed")
		require.Empty(t, reloaded.PrivateData.RequestBodyPath)
		require.NoFileExists(t, expected.bodyPath)
	}
	require.NoError(t, db.First(&user, 1).Error)
	require.Equal(t, 1800, user.Quota, "explicit upstream business failures must refund wallet quota")
	require.NoError(t, db.First(&token, 1).Error)
	require.Equal(t, 1800, token.RemainQuota)
	require.Equal(t, 1800, token.UsedQuota)
}

func TestRunAsyncTaskBridgeImageTaskBatchSuccessFetchesFullResultAndSettles(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.Task{},
		&model.TaskSettlementRecord{},
		&model.User{},
		&model.Token{},
		&model.Channel{},
		&model.Log{},
		&model.Option{},
		&model.TokenUsageDaily{},
	))

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldUsingSQLite := common.UsingSQLite
	oldRedisEnabled := common.RedisEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	oldLogConsumeEnabled := common.LogConsumeEnabled
	oldDataExportEnabled := common.DataExportEnabled
	oldQuotaRemindThreshold := common.QuotaRemindThreshold
	oldBatchSize := constant.ImageTaskBatchPollSize
	oldImageTaskFileCacheShared := constant.ImageTaskFileCacheShared
	oldImageTaskFileCacheSharedTrusted := constant.ImageTaskFileCacheSharedTrusted
	oldImageTaskSharedCacheDisabled := common.ImageTaskSharedCacheDisabled()
	oldDiskCacheConfig := common.GetDiskCacheConfig()
	diskCacheConfig := oldDiskCacheConfig
	diskCacheConfig.Path = t.TempDir()
	model.DB = db
	model.LOG_DB = db
	common.UsingSQLite = true
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	common.DataExportEnabled = false
	common.QuotaRemindThreshold = 0
	constant.ImageTaskBatchPollSize = 20
	constant.ImageTaskFileCacheShared = true
	constant.ImageTaskFileCacheSharedTrusted = true
	common.SetImageTaskSharedCacheDisabled(false)
	common.ResetDiskCacheUsage()
	common.ResetDiskCacheStats()
	common.SetDiskCacheConfig(diskCacheConfig)
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.UsingSQLite = oldUsingSQLite
		common.RedisEnabled = oldRedisEnabled
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
		common.LogConsumeEnabled = oldLogConsumeEnabled
		common.DataExportEnabled = oldDataExportEnabled
		common.QuotaRemindThreshold = oldQuotaRemindThreshold
		constant.ImageTaskBatchPollSize = oldBatchSize
		constant.ImageTaskFileCacheShared = oldImageTaskFileCacheShared
		constant.ImageTaskFileCacheSharedTrusted = oldImageTaskFileCacheSharedTrusted
		common.SetImageTaskSharedCacheDisabled(oldImageTaskSharedCacheDisabled)
		common.ResetDiskCacheUsage()
		common.ResetDiskCacheStats()
		common.SetDiskCacheConfig(oldDiskCacheConfig)
		_ = sqlDB.Close()
	})

	type pollRequest struct {
		includeImageData string
		ids              []string
	}
	var mu sync.Mutex
	requests := make([]pollRequest, 0, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/image-tasks", r.URL.Path)
		ids := strings.Split(r.URL.Query().Get("ids"), ",")
		includeImageData := r.URL.Query().Get("include_image_data")
		mu.Lock()
		requests = append(requests, pollRequest{
			includeImageData: includeImageData,
			ids:              append([]string(nil), ids...),
		})
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch includeImageData {
		case "false":
			require.ElementsMatch(t, []string{"upstream_success", "upstream_running"}, ids)
			_, _ = w.Write([]byte(`{
				"items": [
					{"task_id": "upstream_success", "status": "completed", "progress": "100%"},
					{"task_id": "upstream_running", "status": "running", "progress": "42%"}
				]
			}`))
		case "true":
			require.Equal(t, []string{"upstream_success"}, ids)
			_, _ = w.Write([]byte(`{
				"items": [{
					"task_id": "upstream_success",
					"status": "completed",
					"progress": "100%",
					"result": {
						"data": [{"b64_json": "batch-success-b64"}],
						"usage": {
							"prompt_tokens": 100,
							"completion_tokens": 0,
							"total_tokens": 100
						}
					}
				}]
			}`))
		default:
			http.Error(w, "missing include_image_data", http.StatusBadRequest)
		}
	}))
	defer upstream.Close()

	require.NoError(t, db.Create(&model.User{
		Id:       1,
		Username: "image-user",
		Password: "password123",
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    100000,
		Email:    "image@example.com",
	}).Error)
	require.NoError(t, db.Create(&model.Token{
		Id:             1,
		UserId:         1,
		Key:            "token-key",
		Status:         common.TokenStatusEnabled,
		Name:           "image-token",
		ExpiredTime:    -1,
		RemainQuota:    100000,
		UnlimitedQuota: false,
		Group:          "default",
	}).Error)
	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      1,
		Type:    constant.ChannelTypeOpenAI,
		Key:     "upstream-key",
		Status:  common.ChannelStatusEnabled,
		Name:    "async-task-bridge",
		Group:   "default",
		Models:  "gpt-image-1",
		BaseURL: &baseURL,
	}).Error)

	body := []byte(`{"model":"gpt-image-1","quality":"high","stream":false}`)
	bodyPath, err := common.WriteImageTaskBodyCacheFile(body)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove(bodyPath)
	})
	expr := `param("quality") == "high" ? tier("high", p * 4) : tier("normal", p)`
	now := time.Now().Add(-time.Minute).Unix()
	successTask := &model.Task{
		TaskID:     "task_batch_success",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Quota:      50,
		Action:     constant.TaskActionImageGeneration,
		Status:     model.TaskStatusSubmitted,
		Progress:   "0%",
		SubmitTime: now,
		StartTime:  now,
		Properties: model.Properties{
			OriginModelName: "gpt-image-1",
		},
		PrivateData: model.TaskPrivateData{
			ImageTaskMode:      dto.ImageTaskModeAsyncTaskBridge,
			RequestPath:        "/v1/images/generations",
			RequestMethod:      http.MethodPost,
			RequestContentType: "application/json",
			RequestBodyPath:    bodyPath,
			RequestBodySize:    int64(len(body)),
			UpstreamTaskID:     "upstream_success",
			Key:                "upstream-key",
			BillingSource:      service.BillingSourceWallet,
			TokenId:            1,
			BillingContext: &model.TaskBillingContext{
				ModelRatio:      1,
				CompletionRatio: 1,
				GroupRatio:      1,
				OriginModelName: "gpt-image-1",
			},
			TieredBillingSnapshot: &billingexpr.BillingSnapshot{
				BillingMode:               "tiered_expr",
				ExprString:                expr,
				ExprHash:                  billingexpr.ExprHashString(expr),
				GroupRatio:                1,
				EstimatedPromptTokens:     100,
				EstimatedCompletionTokens: 0,
				EstimatedQuotaAfterGroup:  50,
				EstimatedTier:             "normal",
				QuotaPerUnit:              common.QuotaPerUnit,
				ExprVersion:               billingexpr.ExprVersion(expr),
			},
		},
	}
	runningTask := &model.Task{
		TaskID:     "task_batch_running",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Action:     constant.TaskActionImageGeneration,
		Status:     model.TaskStatusSubmitted,
		Progress:   "0%",
		SubmitTime: now,
		StartTime:  now,
		Properties: model.Properties{
			OriginModelName: "gpt-image-1",
		},
		PrivateData: model.TaskPrivateData{
			ImageTaskMode:  dto.ImageTaskModeAsyncTaskBridge,
			UpstreamTaskID: "upstream_running",
			Key:            "upstream-key",
		},
	}
	require.NoError(t, db.Create(successTask).Error)
	require.NoError(t, db.Create(runningTask).Error)

	require.NoError(t, RunImageTasks(context.Background(), []*model.Task{successTask, runningTask}))

	mu.Lock()
	requestSnapshot := append([]pollRequest(nil), requests...)
	mu.Unlock()
	require.Len(t, requestSnapshot, 2)
	require.Equal(t, "false", requestSnapshot[0].includeImageData)
	require.ElementsMatch(t, []string{"upstream_success", "upstream_running"}, requestSnapshot[0].ids)
	require.Equal(t, "true", requestSnapshot[1].includeImageData)
	require.Equal(t, []string{"upstream_success"}, requestSnapshot[1].ids)

	var updatedSuccess model.Task
	require.NoError(t, db.First(&updatedSuccess, successTask.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), updatedSuccess.Status)
	require.Equal(t, model.TaskSettlementStatusSettled, updatedSuccess.SettlementStatus)
	require.Equal(t, 200, updatedSuccess.Quota)
	require.Empty(t, updatedSuccess.PrivateData.RequestBodyPath)
	require.NoFileExists(t, bodyPath)
	require.Contains(t, string(updatedSuccess.Data), imageTaskStoredResultMarker)
	require.NotContains(t, string(updatedSuccess.Data), "batch-success-b64")
	require.NotEmpty(t, updatedSuccess.PrivateData.ResultBodyPath)
	require.FileExists(t, updatedSuccess.PrivateData.ResultBodyPath)
	t.Cleanup(func() {
		_ = os.Remove(updatedSuccess.PrivateData.ResultBodyPath)
	})
	storedResult, err := os.ReadFile(updatedSuccess.PrivateData.ResultBodyPath)
	require.NoError(t, err)
	require.Contains(t, string(storedResult), "batch-success-b64")

	var updatedRunning model.Task
	require.NoError(t, db.First(&updatedRunning, runningTask.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), updatedRunning.Status)
	require.Equal(t, "42%", updatedRunning.Progress)
	require.Empty(t, updatedRunning.SettlementStatus)

	var log model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeConsume).First(&log).Error)
	require.Equal(t, 200, log.Quota)

	settlementRecord, exists, err := model.GetTaskSettlementRecord(successTask.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, model.TaskSettlementRecordStatusApplied, settlementRecord.Status)
	require.Equal(t, "image_consumption", settlementRecord.Operation)
	require.NotNil(t, settlementRecord.AppliedQuota)
	require.NotNil(t, settlementRecord.PreConsumedQuota)
	require.NotNil(t, settlementRecord.QuotaDelta)
	require.NotNil(t, settlementRecord.LogType)
	require.Equal(t, 200, *settlementRecord.AppliedQuota)
	require.Equal(t, 50, *settlementRecord.PreConsumedQuota)
	require.Equal(t, 150, *settlementRecord.QuotaDelta)
	require.Equal(t, model.LogTypeConsume, *settlementRecord.LogType)
	require.NotZero(t, settlementRecord.LogDeliveredAt)
	require.NotEmpty(t, settlementRecord.LogPayload)
}

func TestParseAsyncTaskBridgeTaskResultSupportsRealItemsShape(t *testing.T) {
	body := []byte(`{
		"items": [{
			"id": "task_local_123",
			"task_id": "task_local_123",
			"status": "success",
			"data": [{"b64_json":"abc"}],
			"usage": {
				"prompt_tokens": 3,
				"completion_tokens": 4,
				"total_tokens": 7
			}
		}],
		"missing_ids": []
	}`)

	result, err := parseAsyncTaskBridgeTaskResult(body, "task_local_123")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), result.Status)
	require.JSONEq(t, `{
		"data": [{"b64_json":"abc"}],
		"usage": {
			"prompt_tokens": 3,
			"completion_tokens": 4,
			"total_tokens": 7
		}
	}`, string(result.Result))
}

func TestParseAsyncTaskBridgeTaskResultMergesSiblingUsageIntoResult(t *testing.T) {
	body := []byte(`{
		"items": [{
			"id": "task_result_usage",
			"status": "success",
			"result": {"data": [{"b64_json":"abc"}]},
			"usage": {
				"input_tokens": 5,
				"output_tokens": 6,
				"total_tokens": 11
			}
		}]
	}`)

	result, err := parseAsyncTaskBridgeTaskResult(body, "task_result_usage")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), result.Status)
	require.JSONEq(t, `{
		"data": [{"b64_json":"abc"}],
		"usage": {
			"input_tokens": 5,
			"output_tokens": 6,
			"total_tokens": 11
		}
	}`, string(result.Result))
	usage, ok := imageTaskUsageFromResult(result.Result)
	require.True(t, ok)
	require.Equal(t, 5, usage.PromptTokens)
	require.Equal(t, 6, usage.CompletionTokens)
	require.Equal(t, 11, usage.TotalTokens)
}

func TestParseAsyncTaskBridgeTaskResultPrefersItemsOverTopLevelData(t *testing.T) {
	body := []byte(`{
		"items": [{
			"id": "task_local_123",
			"status": "success",
			"data": [{"b64_json":"abc"}]
		}],
		"data": [{"url":"https://example.com/not-a-task.png"}]
	}`)

	result, err := parseAsyncTaskBridgeTaskResult(body, "task_local_123")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), result.Status)
	require.JSONEq(t, `{"data":[{"b64_json":"abc"}]}`, string(result.Result))
}

func TestParseAsyncTaskBridgeTaskResultDoesNotUseTopLevelStatusEnvelope(t *testing.T) {
	body := []byte(`{
		"status": "success",
		"items": [{
			"id": "task_local_123",
			"status": "success",
			"data": [{"b64_json":"abc"}]
		}]
	}`)

	result, err := parseAsyncTaskBridgeTaskResult(body, "task_local_123")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "task_local_123", result.TaskID)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), result.Status)
	require.JSONEq(t, `{"data":[{"b64_json":"abc"}]}`, string(result.Result))
}

func TestParseAsyncTaskBridgeTaskResultIgnoresUnmatchedTopLevelStatusEnvelope(t *testing.T) {
	body := []byte(`{
		"status": "success",
		"items": [{
			"id": "other_task",
			"status": "success",
			"data": [{"b64_json":"abc"}]
		}]
	}`)

	result, err := parseAsyncTaskBridgeTaskResult(body, "task_missing")

	require.NoError(t, err)
	require.Nil(t, result)
}

func TestParseAsyncTaskBridgeTaskResultIgnoresMissingIDs(t *testing.T) {
	body := []byte(`{"items":[],"missing_ids":["task_missing"]}`)

	result, err := parseAsyncTaskBridgeTaskResult(body, "task_missing")

	require.NoError(t, err)
	require.Nil(t, result)
}

func TestAsyncTaskBridgeRecoveredUpstreamIDAllowsLocalTaskID(t *testing.T) {
	body := []byte(`{
		"data": [
			{
				"task_id": "task_local_same",
				"client_task_id": "task_local_same",
				"status": "running",
				"progress": "10%"
			}
		]
	}`)

	result, err := parseAsyncTaskBridgeTaskResult(body, "task_local_same")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "task_local_same", asyncTaskBridgeRecoveredUpstreamID(result, body))
}

func TestBuildAsyncTaskBridgeRecoverQueryIncludesLocalTaskID(t *testing.T) {
	values, err := url.ParseQuery(buildAsyncTaskBridgeRecoverQuery(&model.Task{TaskID: " task_local_123 "}))

	require.NoError(t, err)
	require.Equal(t, "task_local_123", values.Get("ids"))
	require.Equal(t, "task_local_123", values.Get("client_task_id"))
	require.Equal(t, "false", values.Get("include_image_data"))
}

func TestImageTaskShouldRetryStaleSyncExecutionBeforeTaskTimeout(t *testing.T) {
	withImageTaskAsyncTimeoutMinutes(t, 30)
	task := &model.Task{
		Status:    model.TaskStatusInProgress,
		StartTime: time.Now().Add(-imageTaskSyncTimeout - time.Second).Unix(),
	}

	require.False(t, imageTaskShouldFailStaleExecution(task, dto.ImageTaskModeSyncWrapper))
	require.True(t, imageTaskCanStart(task))
}

func TestImageTaskShouldFailStaleSyncExecutionAfterTaskTimeout(t *testing.T) {
	withImageTaskAsyncTimeoutMinutes(t, 1)
	task := &model.Task{
		Status:    model.TaskStatusInProgress,
		StartTime: time.Now().Add(-2 * time.Minute).Unix(),
	}

	require.True(t, imageTaskShouldFailStaleExecution(task, dto.ImageTaskModeSyncWrapper))
}

func TestImageTaskShouldNotFailSubmittedUpstreamAsyncPoll(t *testing.T) {
	task := &model.Task{
		Status:    model.TaskStatusInProgress,
		StartTime: time.Now().Add(-imageTaskSyncTimeout - time.Second).Unix(),
	}
	task.PrivateData.UpstreamTaskID = "upstream_123"

	require.False(t, imageTaskShouldFailStaleExecution(task, dto.ImageTaskModeAsyncTaskBridge))
}

func TestImageTaskShouldNotGenericFailStaleUpstreamAsyncSubmission(t *testing.T) {
	task := &model.Task{
		Status:    model.TaskStatusInProgress,
		StartTime: time.Now().Add(-imageTaskSyncTimeout - time.Second).Unix(),
	}

	require.False(t, imageTaskShouldFailStaleExecution(task, dto.ImageTaskModeAsyncTaskBridge))
	require.True(t, imageTaskExecutionTimedOut(task))
}

func TestImageTaskShouldFailUnrecoveredSubmittedAsyncSubmissionAfterTimeout(t *testing.T) {
	withImageTaskAsyncTimeoutMinutes(t, 30)
	task := &model.Task{
		Status:    model.TaskStatusSubmitted,
		StartTime: time.Now().Add(-imageTaskAsyncTimeout() - time.Second).Unix(),
	}

	require.False(t, imageTaskExecutionTimedOut(task))
	require.True(t, imageTaskShouldFailLongRunningUpstreamStatus(task))
}

func TestImageTaskShouldRecoverPendingAsyncSubmission(t *testing.T) {
	pending := &model.Task{
		Status:    model.TaskStatusInProgress,
		StartTime: time.Now().Unix(),
	}
	require.True(t, imageTaskShouldRecoverPendingAsyncSubmission(pending))

	submittedWithoutUpstreamID := &model.Task{
		Status:    model.TaskStatusSubmitted,
		StartTime: time.Now().Unix(),
	}
	require.True(t, imageTaskShouldRecoverPendingAsyncSubmission(submittedWithoutUpstreamID))

	queued := &model.Task{
		Status:    model.TaskStatusQueued,
		StartTime: time.Now().Unix(),
	}
	require.False(t, imageTaskShouldRecoverPendingAsyncSubmission(queued))

	withUpstreamID := &model.Task{
		Status:    model.TaskStatusInProgress,
		StartTime: time.Now().Unix(),
	}
	withUpstreamID.PrivateData.UpstreamTaskID = "upstream_123"
	require.False(t, imageTaskShouldRecoverPendingAsyncSubmission(withUpstreamID))
}

func TestAsyncTaskBridgeCanSubmitPendingRetry(t *testing.T) {
	task := &model.Task{
		Status:    model.TaskStatusInProgress,
		StartTime: time.Now().Unix(),
	}
	require.True(t, asyncTaskBridgeCanSubmit(task))

	task.PrivateData.UpstreamTaskID = "upstream_123"
	require.False(t, asyncTaskBridgeCanSubmit(task))
}

func TestImageTaskShouldFailMissingUpstreamPollResultAfterTimeout(t *testing.T) {
	withImageTaskAsyncTimeoutMinutes(t, 30)
	staleSubmitted := &model.Task{
		Status:    model.TaskStatusSubmitted,
		StartTime: time.Now().Add(-imageTaskAsyncTimeout() - time.Second).Unix(),
	}
	require.True(t, imageTaskShouldFailMissingUpstreamPollResult(staleSubmitted))

	recentSubmitted := &model.Task{
		Status:    model.TaskStatusSubmitted,
		StartTime: time.Now().Add(-imageTaskAsyncTimeout() + time.Second).Unix(),
	}
	require.False(t, imageTaskShouldFailMissingUpstreamPollResult(recentSubmitted))

	staleQueued := &model.Task{
		Status:    model.TaskStatusQueued,
		StartTime: time.Now().Add(-imageTaskAsyncTimeout() - time.Second).Unix(),
	}
	require.True(t, imageTaskShouldFailMissingUpstreamPollResult(staleQueued))
}

func TestImageTaskShouldFailInvalidUpstreamPollResultAfterTimeout(t *testing.T) {
	withImageTaskAsyncTimeoutMinutes(t, 30)
	staleSubmitted := &model.Task{
		Status:    model.TaskStatusSubmitted,
		StartTime: time.Now().Add(-imageTaskAsyncTimeout() - time.Second).Unix(),
	}
	require.True(t, imageTaskShouldFailInvalidUpstreamPollResult(staleSubmitted))

	recentSubmitted := &model.Task{
		Status:    model.TaskStatusSubmitted,
		StartTime: time.Now().Add(-imageTaskAsyncTimeout() + time.Second).Unix(),
	}
	require.False(t, imageTaskShouldFailInvalidUpstreamPollResult(recentSubmitted))
}

func TestImageTaskShouldFailLongRunningUpstreamStatusAfterTimeout(t *testing.T) {
	withImageTaskAsyncTimeoutMinutes(t, 30)
	for _, status := range []model.TaskStatus{
		model.TaskStatusQueued,
		model.TaskStatusSubmitted,
		model.TaskStatusInProgress,
	} {
		task := &model.Task{
			Status:    status,
			StartTime: time.Now().Add(-imageTaskAsyncTimeout() - time.Second).Unix(),
		}
		require.True(t, imageTaskShouldFailLongRunningUpstreamStatus(task))
	}

	recentTask := &model.Task{
		Status:    model.TaskStatusInProgress,
		StartTime: time.Now().Add(-imageTaskAsyncTimeout() + time.Second).Unix(),
	}
	require.False(t, imageTaskShouldFailLongRunningUpstreamStatus(recentTask))

	doneTask := &model.Task{
		Status:    model.TaskStatusSuccess,
		StartTime: time.Now().Add(-imageTaskAsyncTimeout() - time.Second).Unix(),
	}
	require.False(t, imageTaskShouldFailLongRunningUpstreamStatus(doneTask))
}

func TestApplyAsyncTaskBridgeStatusOnlyMarksTimedOutSubmittedTaskForReview(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})
	withImageTaskAsyncTimeoutMinutes(t, 1)

	task := &model.Task{
		TaskID:     "task_async_timeout_review",
		Platform:   constant.TaskPlatformImage,
		Status:     model.TaskStatusSubmitted,
		StartTime:  time.Now().Add(-2 * time.Minute).Unix(),
		SubmitTime: time.Now().Add(-2 * time.Minute).Unix(),
		Quota:      0,
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "upstream_still_running",
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, applyAsyncTaskBridgeStatusOnly(context.Background(), task, &asyncTaskBridgeTaskResult{
		Status:   model.TaskStatusInProgress,
		Progress: "50%",
	}, "upstream-key"))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.Zero(t, reloaded.Quota)
	require.Equal(t, "upstream_still_running", reloaded.PrivateData.UpstreamTaskID)
	require.False(t, reloaded.RefundPending)
}

func TestImageTaskShouldFailTransientUpstreamErrorAfterTimeout(t *testing.T) {
	withImageTaskAsyncTimeoutMinutes(t, 30)
	for _, status := range []model.TaskStatus{
		model.TaskStatusQueued,
		model.TaskStatusSubmitted,
		model.TaskStatusInProgress,
	} {
		task := &model.Task{
			Status:    status,
			StartTime: time.Now().Add(-imageTaskAsyncTimeout() - time.Second).Unix(),
		}
		require.True(t, imageTaskShouldFailTransientUpstreamError(task))
	}

	recentTask := &model.Task{
		Status:    model.TaskStatusSubmitted,
		StartTime: time.Now().Add(-imageTaskAsyncTimeout() + time.Second).Unix(),
	}
	require.False(t, imageTaskShouldFailTransientUpstreamError(recentTask))

	doneTask := &model.Task{
		Status:    model.TaskStatusSuccess,
		StartTime: time.Now().Add(-imageTaskAsyncTimeout() - time.Second).Unix(),
	}
	require.False(t, imageTaskShouldFailTransientUpstreamError(doneTask))
}

func TestImageTaskAsyncStatusDoesNotUseSyncWrapperTimeout(t *testing.T) {
	withImageTaskAsyncTimeoutMinutes(t, 30)
	task := &model.Task{
		Status:    model.TaskStatusSubmitted,
		StartTime: time.Now().Add(-imageTaskSyncTimeout - time.Second).Unix(),
	}

	require.False(t, imageTaskShouldFailLongRunningUpstreamStatus(task))
}

func TestAsyncTaskBridgeSubmissionShouldRecover(t *testing.T) {
	require.True(t, asyncTaskBridgeSubmissionShouldRecover(408))
	require.True(t, asyncTaskBridgeSubmissionShouldRecover(429))
	require.True(t, asyncTaskBridgeSubmissionShouldRecover(500))
	require.True(t, asyncTaskBridgeSubmissionShouldRecover(524))

	require.False(t, asyncTaskBridgeSubmissionShouldRecover(400))
	require.False(t, asyncTaskBridgeSubmissionShouldRecover(401))
	require.False(t, asyncTaskBridgeSubmissionShouldRecover(404))
}

func TestAsyncTaskBridgePollShouldRetryOnlyTransientStatus(t *testing.T) {
	require.True(t, asyncTaskBridgePollShouldRetry(408))
	require.True(t, asyncTaskBridgePollShouldRetry(429))
	require.True(t, asyncTaskBridgePollShouldRetry(500))
	require.True(t, asyncTaskBridgePollShouldRetry(524))

	require.False(t, asyncTaskBridgePollShouldRetry(400))
	require.False(t, asyncTaskBridgePollShouldRetry(401))
	require.False(t, asyncTaskBridgePollShouldRetry(403))
	require.False(t, asyncTaskBridgePollShouldRetry(404))
	require.False(t, asyncTaskBridgePollShouldRetry(422))
}

func TestImageTaskFixedUpstreamKeyPrefersStoredKey(t *testing.T) {
	task := &model.Task{}
	task.PrivateData.Key = " fixed-key "

	require.Equal(t, "fixed-key", imageTaskFixedUpstreamKey(task, "next-key"))
	require.Equal(t, "next-key", imageTaskFixedUpstreamKey(&model.Task{}, " next-key "))
	require.Empty(t, imageTaskFixedUpstreamKey(nil, " "))
}

func TestPriceDataFromTaskRestoresFullBillingSnapshot(t *testing.T) {
	task := &model.Task{
		Quota: 123,
		PrivateData: model.TaskPrivateData{
			BillingContext: &model.TaskBillingContext{
				ModelPrice:           0.01,
				GroupRatio:           1.2,
				GroupSpecialRatio:    1.1,
				GroupHasSpecialRatio: true,
				ModelRatio:           2,
				CompletionRatio:      3,
				CacheRatio:           0.5,
				CacheCreationRatio:   1.4,
				CacheCreation5mRatio: 1.5,
				CacheCreation1hRatio: 2.4,
				ImageRatio:           4,
				AudioRatio:           5,
				AudioCompletionRatio: 6,
				OtherRatios: map[string]float64{
					"n": 2,
				},
				PerCallBilling: true,
			},
		},
	}

	priceData := priceDataFromTask(task)

	require.Equal(t, 123, priceData.Quota)
	require.Equal(t, 123, priceData.QuotaToPreConsume)
	require.Equal(t, 0.01, priceData.ModelPrice)
	require.Equal(t, 1.2, priceData.GroupRatioInfo.GroupRatio)
	require.Equal(t, 1.1, priceData.GroupRatioInfo.GroupSpecialRatio)
	require.True(t, priceData.GroupRatioInfo.HasSpecialRatio)
	require.Equal(t, 2.0, priceData.ModelRatio)
	require.Equal(t, 3.0, priceData.CompletionRatio)
	require.Equal(t, 0.5, priceData.CacheRatio)
	require.Equal(t, 1.4, priceData.CacheCreationRatio)
	require.Equal(t, 1.5, priceData.CacheCreation5mRatio)
	require.Equal(t, 2.4, priceData.CacheCreation1hRatio)
	require.Equal(t, 4.0, priceData.ImageRatio)
	require.Equal(t, 5.0, priceData.AudioRatio)
	require.Equal(t, 6.0, priceData.AudioCompletionRatio)
	require.Equal(t, 2.0, priceData.OtherRatios()["n"])
	require.True(t, priceData.UsePrice)
}

func TestImageTaskUsageFromResultNormalizesOpenAIImageUsage(t *testing.T) {
	result := json.RawMessage(`{
		"created": 1710000000,
		"data": [{"url": "https://example.com/a.png"}],
		"usage": {
			"input_tokens": 3,
			"output_tokens": 4,
			"total_tokens": 7,
			"input_tokens_details": {
				"image_tokens": 2,
				"text_tokens": 1
			}
		}
	}`)

	usage, ok := imageTaskUsageFromResult(result)

	require.True(t, ok)
	require.Equal(t, 3, usage.PromptTokens)
	require.Equal(t, 4, usage.CompletionTokens)
	require.Equal(t, 7, usage.TotalTokens)
	require.Equal(t, 2, usage.PromptTokensDetails.ImageTokens)
	require.Equal(t, 1, usage.PromptTokensDetails.TextTokens)
}

func TestImageTaskUsageFromResultFindsNestedUsage(t *testing.T) {
	result := json.RawMessage(`{
		"openai_response": {
			"data": [{"b64_json": "abc"}],
			"usage": {
				"input_tokens": 8,
				"output_tokens": 9,
				"input_tokens_details": {
					"image_tokens": 5
				}
			}
		}
	}`)

	usage, ok := imageTaskUsageFromResult(result)

	require.True(t, ok)
	require.Equal(t, 8, usage.PromptTokens)
	require.Equal(t, 9, usage.CompletionTokens)
	require.Equal(t, 17, usage.TotalTokens)
	require.Equal(t, 5, usage.PromptTokensDetails.ImageTokens)
}

func TestParseAsyncTaskBridgeTaskResultKeepsStageUsageShape(t *testing.T) {
	body := []byte(`{
		"items": [{
			"id": "task_stage_usage",
			"task_id": "task_stage_usage",
			"status": "success",
			"data": [{"_b64_path": "task_stage_usage/0.bin", "width": 1024, "height": 1024}],
			"usage": {
				"input_tokens": 11,
				"output_tokens": 22,
				"total_tokens": 33,
				"input_tokens_details": {
					"image_tokens": 7,
					"text_tokens": 4
				}
			}
		}],
		"missing_ids": []
	}`)

	result, err := parseAsyncTaskBridgeTaskResult(body, "task_stage_usage")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), result.Status)

	usage, ok := imageTaskUsageFromResult(result.Result)
	require.True(t, ok)
	require.Equal(t, 11, usage.PromptTokens)
	require.Equal(t, 22, usage.CompletionTokens)
	require.Equal(t, 33, usage.TotalTokens)
	require.Equal(t, 7, usage.PromptTokensDetails.ImageTokens)
	require.Equal(t, 4, usage.PromptTokensDetails.TextTokens)
}

func TestImageTaskUsageFromResultWithoutUsageFallsBack(t *testing.T) {
	usage, ok := imageTaskUsageFromResult(json.RawMessage(`{"data":[{"url":"https://example.com/a.png"}]}`))

	require.False(t, ok)
	require.Nil(t, usage)
}

func TestImageTaskBillingRequestInputFromStoredBodyUsesDiskBodyWithSnapshotOnly(t *testing.T) {
	path, err := common.WriteImageTaskBodyCacheFile([]byte(`{"model":"gpt-image-1","quality":"high","stream":false}`))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = common.RemoveDiskCacheFile(path)
	})

	task := &model.Task{}
	task.PrivateData.RequestBodyPath = path
	task.PrivateData.RequestContentType = "application/json"
	task.PrivateData.RequestHeaders = map[string]string{
		"X-Trace": " trace-123 ",
	}
	task.PrivateData.TieredBillingSnapshot = &billingexpr.BillingSnapshot{}

	input, err := imageTaskBillingRequestInputFromStoredBody(task)

	require.NoError(t, err)
	require.NotNil(t, input)
	require.JSONEq(t, `{"model":"gpt-image-1","quality":"high","stream":false}`, string(input.Body))
	require.Equal(t, "trace-123", input.Headers["X-Trace"])
}

func TestCloneImageTaskBillingRequestInputPreservesCapturedParams(t *testing.T) {
	source := &billingexpr.RequestInput{
		Headers: map[string]string{"X-Test": "trace"},
		Params:  map[string]any{"quality": "high"},
	}

	cloned := cloneImageTaskBillingRequestInput(source)

	require.NotNil(t, cloned)
	require.Equal(t, "high", cloned.Params["quality"])
	cloned.Params["quality"] = "low"
	require.Equal(t, "high", source.Params["quality"])
}

func TestCaptureImageTaskSettlementBillingEvidenceDropsRawBody(t *testing.T) {
	expr := `param("quality") == "high" ? tier("high", p * 2) : tier("normal", p)`
	task := &model.Task{PrivateData: model.TaskPrivateData{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr",
			ExprString:  expr,
		},
	}}
	input := &billingexpr.RequestInput{
		Headers: map[string]string{"X-Trace": "trace-123"},
		Body:    []byte(`{"quality":"high","prompt":"private prompt"}`),
	}

	evidence, err := captureImageTaskSettlementBillingEvidence(task, input)

	require.NoError(t, err)
	require.NotNil(t, evidence)
	require.Empty(t, evidence.Body)
	require.Empty(t, evidence.Headers)
	require.Equal(t, "high", evidence.Params["quality"])
	encoded, err := json.Marshal(evidence)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private prompt")
}

func TestCaptureImageTaskSettlementBillingEvidenceKeepsReferencedHeader(t *testing.T) {
	expr := `header("X-Billing-Tier") == "fast" ? tier("fast", p * 2) : tier("normal", p)`
	task := &model.Task{PrivateData: model.TaskPrivateData{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{BillingMode: "tiered_expr", ExprString: expr},
	}}
	input := &billingexpr.RequestInput{Headers: map[string]string{
		"X-Billing-Tier": "fast",
		"X-Trace-Secret": "must-not-persist",
	}}

	evidence, err := captureImageTaskSettlementBillingEvidence(task, input)

	require.NoError(t, err)
	require.Equal(t, map[string]string{"x-billing-tier": "fast"}, evidence.Headers)
}

func TestImageTaskBillingRequestInputFromStoredBodyRejectsOversizeBeforeRead(t *testing.T) {
	oldMaxMB := constant.ImageTaskRequestBodyBase64MaxMB
	oldDiskCacheConfig := common.GetDiskCacheConfig()
	common.ResetDiskCacheUsage()
	common.ResetDiskCacheStats()
	constant.ImageTaskRequestBodyBase64MaxMB = 1
	common.SetDiskCacheConfig(common.DiskCacheConfig{
		MaxSizeMB: 8,
		Path:      t.TempDir(),
	})
	t.Cleanup(func() {
		constant.ImageTaskRequestBodyBase64MaxMB = oldMaxMB
		common.ResetDiskCacheUsage()
		common.ResetDiskCacheStats()
		common.SetDiskCacheConfig(oldDiskCacheConfig)
	})

	path, err := common.WriteImageTaskBodyCacheFile(bytes.Repeat([]byte("a"), (1<<20)+1))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = common.RemoveDiskCacheFile(path)
	})

	task := &model.Task{}
	task.PrivateData.RequestBodyPath = path
	task.PrivateData.RequestBodySize = (1 << 20) + 1
	task.PrivateData.RequestContentType = "application/json"
	task.PrivateData.TieredBillingSnapshot = &billingexpr.BillingSnapshot{}

	input, err := imageTaskBillingRequestInputFromStoredBody(task)

	require.ErrorIs(t, err, common.ErrRequestBodyTooLarge)
	require.NotNil(t, input)
	require.Empty(t, input.Body)
}

func TestOpenImageTaskBodyStorageFallsBackToStoredBase64(t *testing.T) {
	body := []byte(`{"model":"gpt-image-1","stream":false}`)
	task := &model.Task{
		PrivateData: model.TaskPrivateData{
			RequestContentType: "application/json",
			RequestBodyPath:    "missing-request-body.json",
			RequestBodyBase64:  base64.StdEncoding.EncodeToString(body),
		},
	}

	storage, contentType, err := openImageTaskBodyStorage(task)
	require.NoError(t, err)
	defer storage.Close()

	got, err := storage.Bytes()
	require.NoError(t, err)
	require.Equal(t, "application/json", contentType)
	require.JSONEq(t, string(body), string(got))
}

func TestOpenImageTaskBodyStorageFallsBackWhenDiskBodySizeMismatches(t *testing.T) {
	body := []byte(`{"model":"gpt-image-1","stream":false}`)
	path, err := common.WriteImageTaskBodyCacheFile([]byte(`{"bad":true}`))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = common.RemoveDiskCacheFile(path)
	})
	task := &model.Task{
		TaskID: "task_body_size_mismatch",
		PrivateData: model.TaskPrivateData{
			RequestContentType: "application/json",
			RequestBodyPath:    path,
			RequestBodyBase64:  base64.StdEncoding.EncodeToString(body),
			RequestBodySize:    int64(len(body)),
		},
	}

	storage, contentType, err := openImageTaskBodyStorage(task)
	require.NoError(t, err)
	defer storage.Close()

	got, err := storage.Bytes()
	require.NoError(t, err)
	require.Equal(t, "application/json", contentType)
	require.JSONEq(t, string(body), string(got))
}

func TestOpenImageTaskBodyStoragePrefersBase64ForPortableBody(t *testing.T) {
	body := []byte(`{"b":2}`)
	path, err := common.WriteImageTaskBodyCacheFile([]byte(`{"a":1}`))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = common.RemoveDiskCacheFile(path)
	})
	task := &model.Task{
		TaskID: "task_body_portable",
		PrivateData: model.TaskPrivateData{
			RequestContentType:  "application/json",
			RequestBodyPath:     path,
			RequestBodyBase64:   base64.StdEncoding.EncodeToString(body),
			RequestBodyPortable: true,
			RequestBodySize:     int64(len(body)),
		},
	}

	storage, contentType, err := openImageTaskBodyStorage(task)
	require.NoError(t, err)
	defer storage.Close()

	got, err := storage.Bytes()
	require.NoError(t, err)
	require.Equal(t, "application/json", contentType)
	require.JSONEq(t, string(body), string(got))
}

func TestStoreImageTaskResultDataKeepsB64InlineWhenFileCacheNotShared(t *testing.T) {
	oldShared := constant.ImageTaskFileCacheShared
	oldTrusted := constant.ImageTaskFileCacheSharedTrusted
	oldDiskCacheConfig := common.GetDiskCacheConfig()
	constant.ImageTaskFileCacheShared = false
	constant.ImageTaskFileCacheSharedTrusted = false
	diskCacheConfig := oldDiskCacheConfig
	diskCacheConfig.Path = t.TempDir()
	common.SetDiskCacheConfig(diskCacheConfig)
	t.Cleanup(func() {
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskFileCacheSharedTrusted = oldTrusted
		common.SetDiskCacheConfig(oldDiskCacheConfig)
	})

	result := json.RawMessage(`{"data":[{"b64_json":"inline-b64"}]}`)
	task := &model.Task{}

	path, err := storeImageTaskResultData(task, result, time.Now().Unix())

	require.NoError(t, err)
	require.Empty(t, path)
	require.Empty(t, task.PrivateData.ResultBodyPath)
	require.JSONEq(t, string(result), string(task.Data))
}

func TestStoreImageTaskResultDataStartsExpiryAtResultPersistence(t *testing.T) {
	oldRetention := constant.ImageTaskResultRetentionMinutes
	constant.ImageTaskResultRetentionMinutes = 720
	t.Cleanup(func() { constant.ImageTaskResultRetentionMinutes = oldRetention })

	storedAt := time.Now().Unix()
	task := &model.Task{
		ResultAcknowledgedAt: storedAt - 10,
		ResultDeleteAfter:    storedAt - 5,
		ResultCleanedAt:      storedAt - 1,
	}
	result := json.RawMessage(`{"data":[{"url":"https://example.com/result.png"}]}`)

	path, err := storeImageTaskResultData(task, result, storedAt)

	require.NoError(t, err)
	require.Empty(t, path)
	require.Equal(t, storedAt+12*60*60, task.ResultExpiresAt)
	require.Equal(t, storedAt, task.PrivateData.ResultStoredAt)
	require.Equal(t, storedAt+12*60*60, task.PrivateData.ResultExpiresAt)
	require.Zero(t, task.ResultAcknowledgedAt)
	require.Zero(t, task.ResultDeleteAfter)
	require.Zero(t, task.ResultCleanedAt)
	require.JSONEq(t, string(result), string(task.Data))
}

func TestMarkImageTaskSettlementSettledDoesNotExtendResultRetention(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	oldDB := model.DB
	oldRetention := constant.ImageTaskResultRetentionMinutes
	model.DB = db
	constant.ImageTaskResultRetentionMinutes = 720
	t.Cleanup(func() {
		model.DB = oldDB
		constant.ImageTaskResultRetentionMinutes = oldRetention
		_ = sqlDB.Close()
	})

	storedAt := time.Now().Add(-13 * time.Hour).Unix()
	task := &model.Task{
		TaskID:           "task_settled_keeps_result_retention",
		Platform:         constant.TaskPlatformImage,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusApplied,
		FinishTime:       storedAt,
		ResultExpiresAt:  storedAt + 12*60*60,
		PrivateData: model.TaskPrivateData{
			ResultStoredAt:  storedAt,
			ResultExpiresAt: storedAt + 12*60*60,
		},
	}
	task.SetData(map[string]any{"data": []any{map[string]any{"url": "https://example.com/settled.png"}}})
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, markImageTaskSettlementSettled(context.Background(), task, model.TaskSettlementStatusApplied))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskSettlementStatusSettled, reloaded.SettlementStatus)
	require.Equal(t, storedAt+12*60*60, reloaded.ResultExpiresAt)
	require.Equal(t, reloaded.ResultExpiresAt, reloaded.PrivateData.ResultExpiresAt)
}

func TestStoreImageTaskResultDataKeepsB64InlineWhenSharedCacheIsNotTrusted(t *testing.T) {
	oldShared := constant.ImageTaskFileCacheShared
	oldTrusted := constant.ImageTaskFileCacheSharedTrusted
	oldDiskCacheConfig := common.GetDiskCacheConfig()
	constant.ImageTaskFileCacheShared = true
	constant.ImageTaskFileCacheSharedTrusted = false
	diskCacheConfig := oldDiskCacheConfig
	diskCacheConfig.Path = t.TempDir()
	common.SetDiskCacheConfig(diskCacheConfig)
	t.Cleanup(func() {
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskFileCacheSharedTrusted = oldTrusted
		common.SetDiskCacheConfig(oldDiskCacheConfig)
	})

	result := json.RawMessage(`{"data":[{"b64_json":"inline-b64"}]}`)
	task := &model.Task{}

	path, err := storeImageTaskResultData(task, result, time.Now().Unix())

	require.NoError(t, err)
	require.Empty(t, path)
	require.Empty(t, task.PrivateData.ResultBodyPath)
	require.JSONEq(t, string(result), string(task.Data))
}

func TestStoreImageTaskResultDataKeepsLargeB64InlineWithoutTrustedCache(t *testing.T) {
	oldShared := constant.ImageTaskFileCacheShared
	oldTrusted := constant.ImageTaskFileCacheSharedTrusted
	oldDiskCacheConfig := common.GetDiskCacheConfig()
	constant.ImageTaskFileCacheShared = true
	constant.ImageTaskFileCacheSharedTrusted = false
	diskCacheConfig := oldDiskCacheConfig
	diskCacheConfig.Path = t.TempDir()
	common.SetDiskCacheConfig(diskCacheConfig)
	t.Cleanup(func() {
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskFileCacheSharedTrusted = oldTrusted
		common.SetDiskCacheConfig(oldDiskCacheConfig)
	})

	result := json.RawMessage(`{"data":[{"b64_json":"` + strings.Repeat("x", 2<<20) + `"}]}`)
	task := &model.Task{}

	path, err := storeImageTaskResultData(task, result, time.Now().Unix())

	require.NoError(t, err)
	require.Empty(t, path)
	require.JSONEq(t, string(result), string(task.Data))
	require.Empty(t, task.PrivateData.ResultBodyPath)
}

func TestImageTaskResultStorageActionIgnoresB64JSONTextValue(t *testing.T) {
	result := []byte(`{"data":[{"revised_prompt":"b64_json"}]}`)

	offload, err := imageTaskResultStorageAction(result)

	require.NoError(t, err)
	require.False(t, offload)
}

func TestStoreImageTaskResultDataOffloadsB64WhenSharedCacheIsTrusted(t *testing.T) {
	oldShared := constant.ImageTaskFileCacheShared
	oldTrusted := constant.ImageTaskFileCacheSharedTrusted
	oldSharedDisabled := common.ImageTaskSharedCacheDisabled()
	oldDiskCacheConfig := common.GetDiskCacheConfig()
	constant.ImageTaskFileCacheShared = true
	constant.ImageTaskFileCacheSharedTrusted = true
	common.SetImageTaskSharedCacheDisabled(false)
	diskCacheConfig := oldDiskCacheConfig
	diskCacheConfig.Path = t.TempDir()
	common.SetDiskCacheConfig(diskCacheConfig)
	t.Cleanup(func() {
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskFileCacheSharedTrusted = oldTrusted
		common.SetImageTaskSharedCacheDisabled(oldSharedDisabled)
		common.SetDiskCacheConfig(oldDiskCacheConfig)
	})

	result := json.RawMessage(`{"data":[{"b64_json":"inline-b64"}]}`)
	task := &model.Task{}

	path, err := storeImageTaskResultData(task, result, time.Now().Unix())

	require.NoError(t, err)
	require.NotEmpty(t, path)
	require.Equal(t, path, task.PrivateData.ResultBodyPath)
	require.FileExists(t, path)
	require.True(t, imageTaskDataIsStoredResultPlaceholder(task.Data))
	_ = os.Remove(path)
}

func TestImageTaskResultStorageActionRejectsOversizeInlineResult(t *testing.T) {
	oldShared := constant.ImageTaskFileCacheShared
	oldTrusted := constant.ImageTaskFileCacheSharedTrusted
	oldInlineMax := constant.ImageTaskResultInlineMaxMB
	constant.ImageTaskFileCacheShared = false
	constant.ImageTaskFileCacheSharedTrusted = false
	constant.ImageTaskResultInlineMaxMB = 1
	t.Cleanup(func() {
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskFileCacheSharedTrusted = oldTrusted
		constant.ImageTaskResultInlineMaxMB = oldInlineMax
	})

	small := []byte(`{"data":[{"b64_json":"aGVsbG8="}]}`)
	offload, err := imageTaskResultStorageAction(small)
	require.NoError(t, err)
	require.False(t, offload)

	oversize := append([]byte(`{"data":[{"b64_json":"`), bytes.Repeat([]byte("a"), (1<<20)+1)...)
	oversize = append(oversize, []byte(`"}]}`)...)
	offload, err = imageTaskResultStorageAction(oversize)
	require.False(t, offload)
	require.ErrorContains(t, err, "too large to store inline")

	// Non-b64 payloads must use the same inline size guard.
	oversizeURL := append([]byte(`{"data":[{"url":"https://example.com/`), bytes.Repeat([]byte("a"), (1<<20)+1)...)
	oversizeURL = append(oversizeURL, []byte(`"}]}`)...)
	offload, err = imageTaskResultStorageAction(oversizeURL)
	require.False(t, offload)
	require.ErrorContains(t, err, "too large to store inline")

	// 关闭上限后恢复内联行为。
	constant.ImageTaskResultInlineMaxMB = 0
	offload, err = imageTaskResultStorageAction(oversize)
	require.NoError(t, err)
	require.False(t, offload)
}

func TestStoreImageTaskResultDataMarksReviewInsteadOfInliningOversizeResult(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	oldDB := model.DB
	oldShared := constant.ImageTaskFileCacheShared
	oldTrusted := constant.ImageTaskFileCacheSharedTrusted
	oldInlineMax := constant.ImageTaskResultInlineMaxMB
	model.DB = db
	constant.ImageTaskFileCacheShared = false
	constant.ImageTaskFileCacheSharedTrusted = false
	constant.ImageTaskResultInlineMaxMB = 1
	t.Cleanup(func() {
		model.DB = oldDB
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskFileCacheSharedTrusted = oldTrusted
		constant.ImageTaskResultInlineMaxMB = oldInlineMax
		_ = sqlDB.Close()
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:     "task_result_inline_guard",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusInProgress,
		Progress:   "1%",
		SubmitTime: now,
		StartTime:  now,
	}
	require.NoError(t, db.Create(task).Error)

	oversize := append([]byte(`{"data":[{"b64_json":"`), bytes.Repeat([]byte("a"), (1<<20)+1)...)
	oversize = append(oversize, []byte(`"}]}`)...)

	path, storeErr := storeImageTaskResultData(task, oversize, now)
	require.Empty(t, path)
	require.ErrorContains(t, storeErr, "too large to store inline")

	require.NoError(t, markImageTaskUpstreamResultReview(
		context.Background(), task, model.TaskStatusInProgress,
		"store image task result failed: "+storeErr.Error(),
	))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.Contains(t, reloaded.FailReason, "too large to store inline")
	require.Empty(t, reloaded.PrivateData.ResultBodyPath)
}

func TestRunSyncWrapperImageTaskReleasesMultipartTempFiles(t *testing.T) {
	// multipart 临时文件重定向到本用例独占目录，避免跨包并行统计串扰。
	isolatedTempDir := t.TempDir()
	t.Setenv("TMPDIR", isolatedTempDir)
	t.Setenv("TMP", isolatedTempDir)
	t.Setenv("TEMP", isolatedTempDir)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.Task{},
		&model.TaskSettlementRecord{},
		&model.User{},
		&model.Token{},
		&model.Channel{},
		&model.Log{},
		&model.QuotaData{},
		&model.TokenUsageDaily{},
	))

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldUsingSQLite := common.UsingSQLite
	oldRedisEnabled := common.RedisEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldDiskCacheConfig := common.GetDiskCacheConfig()
	oldShared := constant.ImageTaskFileCacheShared
	oldTrusted := constant.ImageTaskFileCacheSharedTrusted
	oldSharedDisabled := common.ImageTaskSharedCacheDisabled()
	oldMaxFileDownloadMB := constant.MaxFileDownloadMB
	goodCache := oldDiskCacheConfig
	goodCache.Path = t.TempDir()
	model.DB = db
	model.LOG_DB = db
	common.UsingSQLite = true
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	constant.ImageTaskFileCacheShared = true
	constant.ImageTaskFileCacheSharedTrusted = true
	// 压低 multipart 内存阈值，强制文件分片落盘产生临时文件。
	constant.MaxFileDownloadMB = 1
	common.SetImageTaskSharedCacheDisabled(false)
	common.SetDiskCacheConfig(goodCache)
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.UsingSQLite = oldUsingSQLite
		common.RedisEnabled = oldRedisEnabled
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskFileCacheSharedTrusted = oldTrusted
		constant.MaxFileDownloadMB = oldMaxFileDownloadMB
		common.SetImageTaskSharedCacheDisabled(oldSharedDisabled)
		common.SetDiskCacheConfig(oldDiskCacheConfig)
		_ = sqlDB.Close()
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1710000000,"data":[{"b64_json":"test-b64-payload"}]}`))
	}))
	defer upstream.Close()

	require.NoError(t, db.Create(&model.User{
		Id: 1, Username: "image-user", Password: "password123",
		Status: common.UserStatusEnabled, Group: "default", Quota: 100000,
	}).Error)
	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Type: constant.ChannelTypeOpenAI, Key: "upstream-key",
		Status: common.ChannelStatusEnabled, Name: "openai-image", Group: "default",
		Models: "gpt-image-1", BaseURL: &baseURL,
	}).Error)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-1"))
	require.NoError(t, writer.WriteField("prompt", "edit this image"))
	part, err := writer.CreateFormFile("image", "input.png")
	require.NoError(t, err)
	_, err = part.Write(bytes.Repeat([]byte("a"), (2<<20)+1))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	bodyPath, err := common.WriteImageTaskBodyCacheFile(body.Bytes())
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(bodyPath) })

	task := &model.Task{
		TaskID:     "task_sync_multipart_tempfiles",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Action:     constant.TaskActionImageEdit,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: time.Now().Unix(),
		Properties: model.Properties{OriginModelName: "gpt-image-1"},
		PrivateData: model.TaskPrivateData{
			ImageTaskMode:      dto.ImageTaskModeSyncWrapper,
			RequestPath:        "/v1/images/edits",
			RequestMethod:      http.MethodPost,
			RequestContentType: writer.FormDataContentType(),
			RequestBodyPath:    bodyPath,
			RequestBodySize:    int64(body.Len()),
			Key:                "upstream-key",
		},
	}
	require.NoError(t, db.Create(task).Error)

	before, err := filepath.Glob(filepath.Join(os.TempDir(), "multipart-*"))
	require.NoError(t, err)

	_ = runSyncWrapperImageTask(context.Background(), task)

	after, err := filepath.Glob(filepath.Join(os.TempDir(), "multipart-*"))
	require.NoError(t, err)
	require.Len(t, after, len(before), "sync wrapper execution must not leak multipart temp files")

	var updated model.Task
	require.NoError(t, db.First(&updated, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), updated.Status)
}

func TestRunSyncWrapperImageTaskDoesNotReplayMarkedSubmissionAfterLeaseRecovery(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.Task{},
		&model.TaskSettlementRecord{},
		&model.User{},
		&model.Channel{},
	))

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldRedisEnabled := common.RedisEnabled
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.RedisEnabled = oldRedisEnabled
		_ = sqlDB.Close()
	})

	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1710000000,"data":[{"b64_json":"duplicate"}]}`))
	}))
	defer upstream.Close()

	require.NoError(t, db.Create(&model.User{
		Id: 1, Username: "image-user", Password: "password123",
		Status: common.UserStatusEnabled, Group: "default", Quota: 100000,
	}).Error)
	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Type: constant.ChannelTypeOpenAI, Key: "upstream-key",
		Status: common.ChannelStatusEnabled, Name: "openai-image", Group: "default",
		Models: "gpt-image-1", BaseURL: &baseURL,
	}).Error)
	body := []byte(`{"model":"gpt-image-1","prompt":"cat","stream":false}`)
	bodyPath, err := common.WriteImageTaskBodyCacheFile(body)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(bodyPath) })

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:                  "task_sync_submission_ambiguous",
		Platform:                constant.TaskPlatformImage,
		UserId:                  1,
		Group:                   "default",
		ChannelId:               1,
		Action:                  constant.TaskActionImageGeneration,
		Status:                  model.TaskStatusInProgress,
		Progress:                "1%",
		SubmitTime:              now - 30,
		StartTime:               now - 30,
		SyncSubmissionStartedAt: now - 30,
		Quota:                   600,
		Properties:              model.Properties{OriginModelName: "gpt-image-1"},
		PrivateData: model.TaskPrivateData{
			PublicImageTask:    true,
			BillingSource:      "wallet",
			ImageTaskMode:      dto.ImageTaskModeSyncWrapper,
			RequestPath:        "/v1/images/generations",
			RequestMethod:      http.MethodPost,
			RequestContentType: "application/json",
			RequestBodyPath:    bodyPath,
			RequestBodySize:    int64(len(body)),
			Key:                "upstream-key",
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, runSyncWrapperImageTask(context.Background(), task))
	require.Zero(t, upstreamCalls.Load())
	var updated model.Task
	require.NoError(t, db.First(&updated, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), updated.Status)
	require.Equal(t, model.TaskSettlementStatusReview, updated.SettlementStatus)
	require.Equal(t, 600, updated.Quota)
	require.Contains(t, updated.FailReason, "submission outcome is unknown")
}

func TestExecuteSyncImageTaskRejectsStaleLeaseBeforeUpstreamSubmission(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.Task{},
		&model.TaskSettlementRecord{},
		&model.User{},
		&model.Channel{},
	))

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldRedisEnabled := common.RedisEnabled
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.RedisEnabled = oldRedisEnabled
		_ = sqlDB.Close()
	})

	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1710000000,"data":[{"b64_json":"stale"}]}`))
	}))
	defer upstream.Close()

	require.NoError(t, db.Create(&model.User{
		Id: 1, Username: "image-user", Password: "password123",
		Status: common.UserStatusEnabled, Group: "default", Quota: 100000,
	}).Error)
	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Type: constant.ChannelTypeOpenAI, Key: "upstream-key",
		Status: common.ChannelStatusEnabled, Name: "openai-image", Group: "default",
		Models: "gpt-image-1", BaseURL: &baseURL,
	}).Error)

	body := []byte(`{"model":"gpt-image-1","prompt":"cat","stream":false}`)
	bodyPath, err := common.WriteImageTaskBodyCacheFile(body)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(bodyPath) })

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:     "task_sync_submission_stale_lease",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Action:     constant.TaskActionImageGeneration,
		Status:     model.TaskStatusInProgress,
		Progress:   "1%",
		SubmitTime: now - 30,
		StartTime:  now - 30,
		LockOwner:  "owner-b",
		LockUntil:  now + 60,
		Properties: model.Properties{OriginModelName: "gpt-image-1"},
		PrivateData: model.TaskPrivateData{
			PublicImageTask:    true,
			BillingSource:      "wallet",
			ImageTaskMode:      dto.ImageTaskModeSyncWrapper,
			RequestPath:        "/v1/images/generations",
			RequestMethod:      http.MethodPost,
			RequestContentType: "application/json",
			RequestBodyPath:    bodyPath,
			RequestBodySize:    int64(len(body)),
			Key:                "upstream-key",
		},
	}
	require.NoError(t, db.Create(task).Error)

	bodyStorage, contentType, err := openImageTaskBodyStorage(task)
	require.NoError(t, err)
	defer bodyStorage.Close()
	ctx := service.ContextWithImageTaskLeaseOwner(context.Background(), "owner-a")
	result, err := executeSyncImageTask(ctx, task, bodyStorage, contentType)

	require.Nil(t, result)
	require.ErrorContains(t, err, "lost CAS")
	require.Zero(t, upstreamCalls.Load())
	var updated model.Task
	require.NoError(t, db.First(&updated, task.ID).Error)
	require.Zero(t, updated.SyncSubmissionStartedAt)
}

func TestRunSyncWrapperImageTaskMarksReviewWhenResultStoreFails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.User{}, &model.Channel{}))

	oldDB := model.DB
	oldUsingSQLite := common.UsingSQLite
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldDiskCacheConfig := common.GetDiskCacheConfig()
	oldShared := constant.ImageTaskFileCacheShared
	oldTrusted := constant.ImageTaskFileCacheSharedTrusted
	oldSharedDisabled := common.ImageTaskSharedCacheDisabled()
	goodCache := oldDiskCacheConfig
	goodCache.Path = t.TempDir()
	model.DB = db
	common.UsingSQLite = true
	common.MemoryCacheEnabled = false
	constant.ImageTaskFileCacheShared = true
	constant.ImageTaskFileCacheSharedTrusted = true
	common.SetImageTaskSharedCacheDisabled(false)
	common.SetDiskCacheConfig(goodCache)
	t.Cleanup(func() {
		model.DB = oldDB
		common.UsingSQLite = oldUsingSQLite
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskFileCacheSharedTrusted = oldTrusted
		common.SetImageTaskSharedCacheDisabled(oldSharedDisabled)
		common.SetDiskCacheConfig(oldDiskCacheConfig)
		_ = sqlDB.Close()
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			http.Error(w, "unexpected path", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"created": 1710000000,
			"data": [{"b64_json": "test-b64-payload"}],
			"usage": {"prompt_tokens": 1, "completion_tokens": 0, "total_tokens": 1}
		}`))
	}))
	defer upstream.Close()

	require.NoError(t, db.Create(&model.User{
		Id:       1,
		Username: "image-user",
		Password: "password123",
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    100000,
	}).Error)
	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      1,
		Type:    constant.ChannelTypeOpenAI,
		Key:     "upstream-key",
		Status:  common.ChannelStatusEnabled,
		Name:    "openai-image",
		Group:   "default",
		Models:  "gpt-image-1",
		BaseURL: &baseURL,
	}).Error)

	body := []byte(`{"model":"gpt-image-1","prompt":"cat","stream":false}`)
	bodyPath, err := common.WriteImageTaskBodyCacheFile(body)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove(bodyPath)
	})
	blocker := filepath.Join(t.TempDir(), "cache-blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("block"), 0600))
	brokenCache := goodCache
	brokenCache.Path = blocker
	common.SetDiskCacheConfig(brokenCache)

	task := &model.Task{
		TaskID:     "task_sync_store_failure",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Action:     constant.TaskActionImageGeneration,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: time.Now().Unix(),
		Properties: model.Properties{
			OriginModelName: "gpt-image-1",
		},
		PrivateData: model.TaskPrivateData{
			ImageTaskMode:      dto.ImageTaskModeSyncWrapper,
			RequestPath:        "/v1/images/generations",
			RequestMethod:      http.MethodPost,
			RequestContentType: "application/json",
			RequestBodyPath:    bodyPath,
			RequestBodySize:    int64(len(body)),
			Key:                "upstream-key",
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, runSyncWrapperImageTask(context.Background(), task))

	var updated model.Task
	require.NoError(t, db.First(&updated, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), updated.Status)
	require.Equal(t, "100%", updated.Progress)
	require.Equal(t, model.TaskSettlementStatusReview, updated.SettlementStatus)
	require.Contains(t, updated.FailReason, "store image task result failed")
	require.Equal(t, bodyPath, updated.PrivateData.RequestBodyPath)
	require.FileExists(t, bodyPath)
}

func TestReadImageTaskHTTPResponseBodyRejectsOversize(t *testing.T) {
	oldMaxFileDownloadMB := constant.MaxFileDownloadMB
	constant.MaxFileDownloadMB = 1
	t.Cleanup(func() {
		constant.MaxFileDownloadMB = oldMaxFileDownloadMB
	})

	_, err := readImageTaskHTTPResponseBody(bytes.NewReader(bytes.Repeat([]byte("a"), (1<<20)+1)))

	require.ErrorIs(t, err, errImageTaskHTTPResponseTooLarge)
}

func TestPollAsyncTaskBridgeOversizeResponseMarksReview(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}))

	oldDB := model.DB
	oldUsingSQLite := common.UsingSQLite
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldMaxFileDownloadMB := constant.MaxFileDownloadMB
	oldDiskCacheConfig := common.GetDiskCacheConfig()
	diskCacheConfig := oldDiskCacheConfig
	diskCacheConfig.Path = t.TempDir()
	model.DB = db
	common.UsingSQLite = true
	common.MemoryCacheEnabled = false
	constant.MaxFileDownloadMB = 1
	common.SetDiskCacheConfig(diskCacheConfig)
	t.Cleanup(func() {
		model.DB = oldDB
		common.UsingSQLite = oldUsingSQLite
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		constant.MaxFileDownloadMB = oldMaxFileDownloadMB
		common.SetDiskCacheConfig(oldDiskCacheConfig)
		_ = sqlDB.Close()
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(bytes.Repeat([]byte("a"), (1<<20)+1))
	}))
	defer upstream.Close()

	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      1,
		Type:    constant.ChannelTypeOpenAI,
		Key:     "upstream-key",
		Status:  common.ChannelStatusEnabled,
		Name:    "async-task-bridge",
		Group:   "default",
		Models:  "gpt-image-1",
		BaseURL: &baseURL,
	}).Error)
	bodyPath, err := common.WriteImageTaskBodyCacheFile([]byte(`{"model":"gpt-image-1","stream":false}`))
	require.NoError(t, err)
	task := &model.Task{
		TaskID:     "task_oversize_poll",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusInProgress,
		Progress:   "1%",
		SubmitTime: time.Now().Add(-time.Minute).Unix(),
		StartTime:  time.Now().Add(-time.Minute).Unix(),
		PrivateData: model.TaskPrivateData{
			ImageTaskMode:      dto.ImageTaskModeAsyncTaskBridge,
			RequestContentType: "application/json",
			RequestBodyPath:    bodyPath,
			RequestBodySize:    int64(len(`{"model":"gpt-image-1","stream":false}`)),
			UpstreamTaskID:     "upstream_oversize",
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, pollAsyncTaskBridgeImageTask(context.Background(), task))

	var updated model.Task
	require.NoError(t, db.First(&updated, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), updated.Status)
	require.Equal(t, "100%", updated.Progress)
	require.Equal(t, model.TaskSettlementStatusReview, updated.SettlementStatus)
	require.Contains(t, updated.FailReason, "image task upstream response too large")
	require.Equal(t, bodyPath, updated.PrivateData.RequestBodyPath)
	require.FileExists(t, bodyPath)
	_ = os.Remove(bodyPath)
}

func TestPollAsyncTaskBridgeHTTPFailuresMarkReview(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}))

	oldDB := model.DB
	oldUsingSQLite := common.UsingSQLite
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldMaxFileDownloadMB := constant.MaxFileDownloadMB
	oldDiskCacheConfig := common.GetDiskCacheConfig()
	model.DB = db
	common.UsingSQLite = true
	common.MemoryCacheEnabled = false
	constant.MaxFileDownloadMB = 1
	common.ResetDiskCacheUsage()
	common.ResetDiskCacheStats()
	common.SetDiskCacheConfig(common.DiskCacheConfig{
		MaxSizeMB: 8,
		Path:      t.TempDir(),
	})
	t.Cleanup(func() {
		model.DB = oldDB
		common.UsingSQLite = oldUsingSQLite
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		constant.MaxFileDownloadMB = oldMaxFileDownloadMB
		common.ResetDiskCacheUsage()
		common.ResetDiskCacheStats()
		common.SetDiskCacheConfig(oldDiskCacheConfig)
		_ = sqlDB.Close()
	})

	var upstreamStatus atomic.Int64
	upstreamStatus.Store(http.StatusBadRequest)
	var oversizedResponse atomic.Bool
	oversizedResponse.Store(true)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(int(upstreamStatus.Load()))
		if oversizedResponse.Load() {
			_, _ = w.Write(bytes.Repeat([]byte("a"), (1<<20)+1))
			return
		}
		_, _ = w.Write([]byte(`{"error":{"message":"poll authorization failed"}}`))
	}))
	defer upstream.Close()

	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      1,
		Type:    constant.ChannelTypeOpenAI,
		Key:     "upstream-key",
		Status:  common.ChannelStatusEnabled,
		Name:    "async-task-bridge",
		Group:   "default",
		Models:  "gpt-image-1",
		BaseURL: &baseURL,
	}).Error)
	bodyPath, err := common.WriteImageTaskBodyCacheFile([]byte(`{"model":"gpt-image-1","stream":false}`))
	require.NoError(t, err)
	t.Cleanup(func() { _ = common.RemoveDiskCacheFile(bodyPath) })
	task := &model.Task{
		TaskID:     "task_oversize_error_poll",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusInProgress,
		Progress:   "1%",
		SubmitTime: time.Now().Add(-time.Minute).Unix(),
		StartTime:  time.Now().Add(-time.Minute).Unix(),
		PrivateData: model.TaskPrivateData{
			ImageTaskMode:      dto.ImageTaskModeAsyncTaskBridge,
			RequestContentType: "application/json",
			RequestBodyPath:    bodyPath,
			RequestBodySize:    int64(len(`{"model":"gpt-image-1","stream":false}`)),
			UpstreamTaskID:     "upstream_oversize_error",
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, pollAsyncTaskBridgeImageTask(context.Background(), task))

	var updated model.Task
	require.NoError(t, db.First(&updated, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), updated.Status)
	require.Equal(t, model.TaskSettlementStatusReview, updated.SettlementStatus)
	require.Contains(t, updated.FailReason, "status=400")
	require.Equal(t, "upstream_oversize_error", updated.PrivateData.UpstreamTaskID)
	require.FileExists(t, bodyPath)

	upstreamStatus.Store(http.StatusUnauthorized)
	oversizedResponse.Store(false)
	secondBodyPath, err := common.WriteImageTaskBodyCacheFile([]byte(`{"model":"gpt-image-1","stream":false}`))
	require.NoError(t, err)
	t.Cleanup(func() { _ = common.RemoveDiskCacheFile(secondBodyPath) })
	secondTask := &model.Task{
		TaskID:     "task_unauthorized_poll",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusSubmitted,
		Progress:   "0%",
		SubmitTime: time.Now().Add(-time.Minute).Unix(),
		StartTime:  time.Now().Add(-time.Minute).Unix(),
		PrivateData: model.TaskPrivateData{
			ImageTaskMode:      dto.ImageTaskModeAsyncTaskBridge,
			RequestContentType: "application/json",
			RequestBodyPath:    secondBodyPath,
			RequestBodySize:    int64(len(`{"model":"gpt-image-1","stream":false}`)),
			UpstreamTaskID:     "upstream_unauthorized",
		},
	}
	require.NoError(t, db.Create(secondTask).Error)
	require.NoError(t, pollAsyncTaskBridgeImageTask(context.Background(), secondTask))

	var secondUpdated model.Task
	require.NoError(t, db.First(&secondUpdated, secondTask.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), secondUpdated.Status)
	require.Equal(t, model.TaskSettlementStatusReview, secondUpdated.SettlementStatus)
	require.Contains(t, secondUpdated.FailReason, "status=401")
	require.Equal(t, "upstream_unauthorized", secondUpdated.PrivateData.UpstreamTaskID)
	require.FileExists(t, secondBodyPath)
}

func TestPollAsyncTaskBridgeDiskCapacityUnavailableRetries(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}))

	oldDB := model.DB
	oldUsingSQLite := common.UsingSQLite
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldMaxFileDownloadMB := constant.MaxFileDownloadMB
	oldDiskCacheConfig := common.GetDiskCacheConfig()
	model.DB = db
	common.UsingSQLite = true
	common.MemoryCacheEnabled = false
	constant.MaxFileDownloadMB = 2
	common.ResetDiskCacheUsage()
	common.ResetDiskCacheStats()
	common.SetDiskCacheConfig(common.DiskCacheConfig{
		MaxSizeMB: 1,
		Path:      t.TempDir(),
	})
	t.Cleanup(func() {
		model.DB = oldDB
		common.UsingSQLite = oldUsingSQLite
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		constant.MaxFileDownloadMB = oldMaxFileDownloadMB
		common.ResetDiskCacheUsage()
		common.ResetDiskCacheStats()
		common.SetDiskCacheConfig(oldDiskCacheConfig)
		_ = sqlDB.Close()
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"upstream_capacity","status":"success","data":[{"url":"https://example.com/a.png"}]}]}`))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      1,
		Type:    constant.ChannelTypeOpenAI,
		Key:     "upstream-key",
		Status:  common.ChannelStatusEnabled,
		Name:    "async-task-bridge",
		Group:   "default",
		Models:  "gpt-image-1",
		BaseURL: &baseURL,
	}).Error)
	body := []byte(`{"model":"gpt-image-1","stream":false}`)
	bodyPath, err := common.WriteImageTaskBodyCacheFile(body)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = common.RemoveDiskCacheFile(bodyPath)
	})
	task := &model.Task{
		TaskID:     "task_capacity_poll",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusInProgress,
		Progress:   "1%",
		SubmitTime: time.Now().Add(-time.Minute).Unix(),
		StartTime:  time.Now().Add(-time.Minute).Unix(),
		PrivateData: model.TaskPrivateData{
			ImageTaskMode:      dto.ImageTaskModeAsyncTaskBridge,
			RequestContentType: "application/json",
			RequestBodyPath:    bodyPath,
			RequestBodySize:    int64(len(body)),
			UpstreamTaskID:     "upstream_capacity",
		},
	}
	require.NoError(t, db.Create(task).Error)

	err = pollAsyncTaskBridgeImageTask(context.Background(), task)

	require.Error(t, err)
	require.Contains(t, err.Error(), "disk cache capacity unavailable")
	var updated model.Task
	require.NoError(t, db.First(&updated, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), updated.Status)
	require.Empty(t, updated.SettlementStatus)
	require.Empty(t, updated.FailReason)
	require.Empty(t, updated.PrivateData.ResultBodyPath)
	require.Equal(t, bodyPath, updated.PrivateData.RequestBodyPath)
	require.FileExists(t, bodyPath)
}

func TestSubmitAsyncTaskBridgeOversizeResponseKeepsSubmissionUncertain(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.Task{},
		&model.TaskSettlementRecord{},
		&model.User{},
		&model.Token{},
		&model.Channel{},
		&model.Log{},
		&model.QuotaData{},
		&model.TokenUsageDaily{},
	))

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldUsingSQLite := common.UsingSQLite
	oldRedisEnabled := common.RedisEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldMaxFileDownloadMB := constant.MaxFileDownloadMB
	oldDiskCacheConfig := common.GetDiskCacheConfig()
	diskCacheConfig := oldDiskCacheConfig
	diskCacheConfig.Path = t.TempDir()
	model.DB = db
	model.LOG_DB = db
	common.UsingSQLite = true
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	constant.MaxFileDownloadMB = 1
	common.SetDiskCacheConfig(diskCacheConfig)
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.UsingSQLite = oldUsingSQLite
		common.RedisEnabled = oldRedisEnabled
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		constant.MaxFileDownloadMB = oldMaxFileDownloadMB
		common.SetDiskCacheConfig(oldDiskCacheConfig)
		_ = sqlDB.Close()
	})

	var submissionMarkerPersisted atomic.Bool
	var upstreamStatus atomic.Int64
	upstreamStatus.Store(http.StatusOK)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/image-tasks/generations" {
			http.Error(w, "unexpected path", http.StatusBadRequest)
			return
		}
		var persisted model.Task
		if err := db.Where("task_id = ?", "task_submit_oversize").First(&persisted).Error; err == nil &&
			persisted.PrivateData.UpstreamSubmitUncertainAt > 0 &&
			persisted.PrivateData.UpstreamSubmitUncertainCount == 1 {
			submissionMarkerPersisted.Store(true)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(int(upstreamStatus.Load()))
		_, _ = w.Write(bytes.Repeat([]byte("a"), (1<<20)+1))
	}))
	defer upstream.Close()

	require.NoError(t, db.Create(&model.User{
		Id:       1,
		Username: "image-user",
		Password: "password123",
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    100000,
	}).Error)
	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      1,
		Type:    constant.ChannelTypeOpenAI,
		Key:     "upstream-key",
		Status:  common.ChannelStatusEnabled,
		Name:    "async-task-bridge",
		Group:   "default",
		Models:  "gpt-image-1",
		BaseURL: &baseURL,
	}).Error)
	body := []byte(`{"model":"gpt-image-1","prompt":"cat","stream":false}`)
	bodyPath, err := common.WriteImageTaskBodyCacheFile(body)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove(bodyPath)
	})
	task := &model.Task{
		TaskID:     "task_submit_oversize",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Action:     constant.TaskActionImageGeneration,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: time.Now().Unix(),
		Properties: model.Properties{
			OriginModelName: "gpt-image-1",
		},
		PrivateData: model.TaskPrivateData{
			ImageTaskMode:      dto.ImageTaskModeAsyncTaskBridge,
			RequestPath:        "/v1/images/generations",
			RequestMethod:      http.MethodPost,
			RequestContentType: "application/json",
			RequestBodyPath:    bodyPath,
			RequestBodySize:    int64(len(body)),
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, submitAsyncTaskBridgeImageTask(context.Background(), task))

	var updated model.Task
	require.NoError(t, db.First(&updated, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), updated.Status)
	require.Equal(t, "1%", updated.Progress)
	require.Empty(t, updated.PrivateData.UpstreamTaskID)
	require.NotZero(t, updated.PrivateData.UpstreamSubmitUncertainAt)
	require.Equal(t, 1, updated.PrivateData.UpstreamSubmitUncertainCount)
	require.True(t, submissionMarkerPersisted.Load(), "submission uncertainty must be durable before the upstream POST is sent")
	require.Equal(t, bodyPath, updated.PrivateData.RequestBodyPath)
	require.FileExists(t, bodyPath)

	require.NoError(t, db.Create(&model.Token{
		Id:          1,
		UserId:      1,
		Key:         "submit-reject-token",
		Name:        "submit-reject-token",
		Status:      common.TokenStatusEnabled,
		RemainQuota: 0,
		UsedQuota:   800,
	}).Error)
	upstreamStatus.Store(http.StatusBadRequest)
	rejectedBodyPath, err := common.WriteImageTaskBodyCacheFile(body)
	require.NoError(t, err)
	t.Cleanup(func() { _ = common.RemoveDiskCacheFile(rejectedBodyPath) })
	rejectedTask := &model.Task{
		TaskID:     "task_submit_oversize_rejected",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Action:     constant.TaskActionImageGeneration,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: time.Now().Unix(),
		Quota:      800,
		Properties: model.Properties{OriginModelName: "gpt-image-1"},
		PrivateData: model.TaskPrivateData{
			ImageTaskMode:            dto.ImageTaskModeAsyncTaskBridge,
			RequestPath:              "/v1/images/generations",
			RequestMethod:            http.MethodPost,
			RequestContentType:       "application/json",
			RequestBodyPath:          rejectedBodyPath,
			RequestBodySize:          int64(len(body)),
			BillingSource:            service.BillingSourceWallet,
			TokenId:                  1,
			PreConsumedUsageCaptured: true,
			PreConsumedUsageRecorded: false,
		},
	}
	require.NoError(t, db.Create(rejectedTask).Error)
	require.NoError(t, submitAsyncTaskBridgeImageTask(context.Background(), rejectedTask))

	var rejectedUpdated model.Task
	require.NoError(t, db.First(&rejectedUpdated, rejectedTask.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), rejectedUpdated.Status)
	require.Empty(t, rejectedUpdated.SettlementStatus)
	require.Zero(t, rejectedUpdated.Quota)
	require.False(t, rejectedUpdated.RefundPending)
	require.Zero(t, rejectedUpdated.PrivateData.UpstreamSubmitUncertainAt)
	require.Zero(t, rejectedUpdated.PrivateData.UpstreamSubmitUncertainCount)
	require.Empty(t, rejectedUpdated.PrivateData.RequestBodyPath)
	require.NoFileExists(t, rejectedBodyPath)
	var refundedUser model.User
	require.NoError(t, db.First(&refundedUser, 1).Error)
	require.Equal(t, 100800, refundedUser.Quota)
	var refundedToken model.Token
	require.NoError(t, db.First(&refundedToken, 1).Error)
	require.Equal(t, 800, refundedToken.RemainQuota)
	require.Zero(t, refundedToken.UsedQuota)
}

func TestAsyncTaskBridgeRepeatedSubmissionHTTPFailureNeedsReview(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.User{}, &model.Channel{}))

	oldDB := model.DB
	oldUsingSQLite := common.UsingSQLite
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldDiskCacheConfig := common.GetDiskCacheConfig()
	diskCacheConfig := oldDiskCacheConfig
	diskCacheConfig.Path = t.TempDir()
	model.DB = db
	common.UsingSQLite = true
	common.MemoryCacheEnabled = false
	common.SetDiskCacheConfig(diskCacheConfig)
	t.Cleanup(func() {
		model.DB = oldDB
		common.UsingSQLite = oldUsingSQLite
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.SetDiskCacheConfig(oldDiskCacheConfig)
		_ = sqlDB.Close()
	})

	var recoverRequests atomic.Int32
	var submitRequests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/image-tasks":
			recoverRequests.Add(1)
			_, _ = w.Write([]byte(`{"items":[],"missing_ids":["task_repeated_submit_http_failure"]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/image-tasks/generations":
			submitRequests.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"repeated submission rejected"}}`))
		default:
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer upstream.Close()

	require.NoError(t, db.Create(&model.User{
		Id:       1,
		Username: "repeated-submit-user",
		Password: "password123",
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    0,
	}).Error)
	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      1,
		Type:    constant.ChannelTypeOpenAI,
		Key:     "upstream-key",
		Status:  common.ChannelStatusEnabled,
		Name:    "async-task-bridge",
		Group:   "default",
		Models:  "gpt-image-1",
		BaseURL: &baseURL,
	}).Error)
	body := []byte(`{"model":"gpt-image-1","prompt":"cat","stream":false}`)
	bodyPath, err := common.WriteImageTaskBodyCacheFile(body)
	require.NoError(t, err)
	t.Cleanup(func() { _ = common.RemoveDiskCacheFile(bodyPath) })

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:     "task_repeated_submit_http_failure",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Action:     constant.TaskActionImageGeneration,
		Status:     model.TaskStatusInProgress,
		Progress:   "1%",
		Quota:      800,
		SubmitTime: now - 60,
		StartTime:  now - 60,
		Properties: model.Properties{OriginModelName: "gpt-image-1"},
		PrivateData: model.TaskPrivateData{
			ImageTaskMode:                dto.ImageTaskModeAsyncTaskBridge,
			RequestPath:                  "/v1/images/generations",
			RequestMethod:                http.MethodPost,
			RequestContentType:           "application/json",
			RequestBodyPath:              bodyPath,
			RequestBodySize:              int64(len(body)),
			Key:                          "upstream-key",
			UpstreamSubmitUncertainAt:    now - int64(imageTaskUncertainSubmissionRetryCooldown.Seconds()) - 1,
			UpstreamSubmitUncertainCount: 1,
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, recoverAsyncTaskBridgeSubmission(context.Background(), task))

	var updated model.Task
	require.NoError(t, db.First(&updated, task.ID).Error)
	require.EqualValues(t, 1, recoverRequests.Load())
	require.EqualValues(t, 1, submitRequests.Load())
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), updated.Status)
	require.Equal(t, model.TaskSettlementStatusReview, updated.SettlementStatus)
	require.Equal(t, 800, updated.Quota)
	require.False(t, updated.RefundPending)
	require.Contains(t, updated.FailReason, "repeated submission rejected")
	require.Equal(t, 2, updated.PrivateData.UpstreamSubmitUncertainCount)
	require.Equal(t, bodyPath, updated.PrivateData.RequestBodyPath)
	require.FileExists(t, bodyPath)
}

func TestFailImageTaskRejectsLostLeaseOwner(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:     "task_lost_owner_fail",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     model.TaskStatusInProgress,
		Progress:   "1%",
		SubmitTime: now,
		LockOwner:  "owner-b",
		LockUntil:  now + 60,
	}
	require.NoError(t, db.Create(task).Error)

	stale := *task
	ctx := service.ContextWithImageTaskLeaseOwner(context.Background(), "owner-a")
	require.ErrorContains(t, failImageTask(ctx, &stale, model.TaskStatusInProgress, "stale failure", false, false), "lost CAS")

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), reloaded.Status)
	require.Equal(t, "owner-b", reloaded.LockOwner)
	require.Empty(t, reloaded.FailReason)
}

func TestSettleImageTaskSuccessFinalizesAppliedSettlementWithoutResult(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})

	bodyPath, err := common.WriteImageTaskBodyCacheFile([]byte(`{"model":"gpt-image-1"}`))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = common.RemoveDiskCacheFile(bodyPath)
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_applied_settlement_finalize",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusApplied,
		PrivateData: model.TaskPrivateData{
			RequestBodyPath: bodyPath,
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, settleImageTaskSuccess(context.Background(), task, imageTaskSettlementPayload{}))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskSettlementStatusSettled, reloaded.SettlementStatus)
	require.Empty(t, reloaded.PrivateData.RequestBodyPath)
	require.Empty(t, reloaded.PrivateData.RequestBodyBase64)
	require.Zero(t, reloaded.RetryCount)
	require.Empty(t, reloaded.LockOwner)
	require.Zero(t, reloaded.LockUntil)
	_, statErr := os.Stat(bodyPath)
	require.True(t, os.IsNotExist(statErr))
}

func TestMarkImageTaskSettlementReviewRetainsEvidenceAndBoundsRequestRetention(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_review_retains_settlement_evidence",
		Platform:         constant.TaskPlatformImage,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusPending,
		PrivateData: model.TaskPrivateData{
			Key:                          "upstream-secret-key",
			RequestHeaders:               map[string]string{"X-Provider-Secret": "secret"},
			RequestBodyPath:              "/tmp/review-request.json",
			SettlementUsage:              &dto.Usage{PromptTokens: 12, TotalTokens: 12},
			SettlementEvidenceCapturedAt: now,
			TieredBillingSnapshot: &billingexpr.BillingSnapshot{
				BillingMode: "tiered_expr",
				ExprString:  `header("X-Billing-Tier") == "fast" ? tier("fast", p) : tier("normal", p)`,
			},
			BillingRequestInput: &billingexpr.RequestInput{
				Headers: map[string]string{"X-Billing-Tier": "fast", "X-Trace-Secret": "must-not-persist"},
				Params:  map[string]any{"quality": "high"},
			},
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, markImageTaskSettlementReview(context.Background(), task, "manual review"))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.NotNil(t, reloaded.PrivateData.SettlementUsage)
	require.Equal(t, 12, reloaded.PrivateData.SettlementUsage.PromptTokens)
	require.Equal(t, now, reloaded.PrivateData.SettlementEvidenceCapturedAt)
	require.Equal(t, "high", reloaded.PrivateData.BillingRequestInput.Params["quality"])
	require.Equal(t, map[string]string{"x-billing-tier": "fast"}, reloaded.PrivateData.BillingRequestInput.Headers)
	require.Empty(t, reloaded.PrivateData.Key)
	require.Empty(t, reloaded.PrivateData.RequestHeaders)
	require.True(t, reloaded.RequestCleanupPending)
	require.GreaterOrEqual(t, reloaded.RequestDeleteAfter, now+int64((12*time.Hour).Seconds())-2)
	require.LessOrEqual(t, reloaded.RequestDeleteAfter, now+int64((12*time.Hour).Seconds())+2)
}

func TestSettleImageTaskSuccessRejectsAppliedRecordForDifferentOperation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskSettlementRecord{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})

	task := &model.Task{
		TaskID:           "task_applied_wrong_operation",
		Platform:         constant.TaskPlatformImage,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusPending,
	}
	require.NoError(t, db.Create(task).Error)
	appliedQuota := 100
	require.NoError(t, db.Create(&model.TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        model.TaskSettlementRecordStatusApplied,
		Operation:     "refund",
		AppliedQuota:  &appliedQuota,
	}).Error)

	err = settleImageTaskSuccess(context.Background(), task, imageTaskSettlementPayload{})

	require.ErrorContains(t, err, "expected image_consumption")
	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
}

func TestMarkImageTaskSettlementSettledDefersRequestDeletionToOwnerNode(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	oldDB := model.DB
	oldNodeName := common.NodeName
	oldShared := constant.ImageTaskFileCacheShared
	oldTrusted := constant.ImageTaskFileCacheSharedTrusted
	model.DB = db
	common.NodeName = "worker-node-b"
	constant.ImageTaskFileCacheShared = false
	constant.ImageTaskFileCacheSharedTrusted = false
	t.Cleanup(func() {
		model.DB = oldDB
		common.NodeName = oldNodeName
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskFileCacheSharedTrusted = oldTrusted
		_ = sqlDB.Close()
	})

	bodyPath := filepath.Join(t.TempDir(), "foreign-request.json")
	require.NoError(t, os.WriteFile(bodyPath, []byte(`{"prompt":"private input"}`), 0o600))
	task := &model.Task{
		TaskID:           "task_settled_foreign_request_owner",
		Platform:         constant.TaskPlatformImage,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusApplied,
		StorageNode:      "worker-node-a",
		PrivateData: model.TaskPrivateData{
			NodeName:        "worker-node-a",
			RequestBodyPath: bodyPath,
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, markImageTaskSettlementSettled(context.Background(), task, model.TaskSettlementStatusApplied))
	_, err = os.Stat(bodyPath)
	require.NoError(t, err)

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskSettlementStatusSettled, reloaded.SettlementStatus)
	require.Equal(t, bodyPath, reloaded.PrivateData.RequestBodyPath)
	require.True(t, reloaded.RequestCleanupPending)
	require.LessOrEqual(t, reloaded.RequestDeleteAfter, time.Now().Unix())

	common.NodeName = "worker-node-a"
	require.NoError(t, service.CleanupDueImageTaskRequestFile(context.Background(), &reloaded))
	_, err = os.Stat(bodyPath)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Empty(t, reloaded.PrivateData.RequestBodyPath)
	require.False(t, reloaded.RequestCleanupPending)
}

func TestMarkImageTaskStorageNodePortableAfterSubmission(t *testing.T) {
	t.Run("releases node binding once upstream accepted", func(t *testing.T) {
		task := &model.Task{StorageNode: "worker-node-a"}
		markImageTaskStorageNodePortableAfterSubmission(task)
		require.Equal(t, model.ImageTaskPortableStorageNode, task.StorageNode)
	})

	t.Run("keeps node binding when tiered billing evidence is not captured", func(t *testing.T) {
		task := &model.Task{
			StorageNode: "worker-node-a",
			PrivateData: model.TaskPrivateData{
				TieredBillingSnapshot:       &billingexpr.BillingSnapshot{BillingMode: "tiered_expr"},
				BillingRequestInputCaptured: false,
			},
		}
		markImageTaskStorageNodePortableAfterSubmission(task)
		require.Equal(t, "worker-node-a", task.StorageNode)
	})

	t.Run("releases node binding when tiered billing evidence is captured", func(t *testing.T) {
		task := &model.Task{
			StorageNode: "worker-node-a",
			PrivateData: model.TaskPrivateData{
				TieredBillingSnapshot:       &billingexpr.BillingSnapshot{BillingMode: "tiered_expr"},
				BillingRequestInputCaptured: true,
			},
		}
		markImageTaskStorageNodePortableAfterSubmission(task)
		require.Equal(t, model.ImageTaskPortableStorageNode, task.StorageNode)
	})
}

func TestSaveAsyncTaskBridgeSubmissionMarksTaskPortable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:      "task_submit_portable",
		Platform:    constant.TaskPlatformImage,
		UserId:      1,
		Group:       "default",
		ChannelId:   1,
		Status:      model.TaskStatusInProgress,
		Progress:    "1%",
		SubmitTime:  now,
		StartTime:   now,
		StorageNode: "worker-node-a",
		PrivateData: model.TaskPrivateData{NodeName: "worker-node-a"},
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, saveAsyncTaskBridgeSubmission(context.Background(), task, "upstream_task_1"))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSubmitted), reloaded.Status)
	require.Equal(t, "upstream_task_1", reloaded.PrivateData.UpstreamTaskID)
	require.Equal(t, model.ImageTaskPortableStorageNode, reloaded.StorageNode)
	// 请求体归属仍留在创建节点，清理由该节点负责。
	require.Equal(t, "worker-node-a", reloaded.PrivateData.NodeName)
}

func TestSubmittedImageTaskIsRunnableFromForeignNode(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskDispatchState{}))

	oldDB := model.DB
	oldNodeName := common.NodeName
	oldShared := constant.ImageTaskFileCacheShared
	oldAffinity := constant.ImageTaskLocalFileCacheAffinity
	model.DB = db
	constant.ImageTaskFileCacheShared = false
	constant.ImageTaskLocalFileCacheAffinity = true
	common.NodeName = "worker-node-a"
	t.Cleanup(func() {
		model.DB = oldDB
		common.NodeName = oldNodeName
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskLocalFileCacheAffinity = oldAffinity
		_ = sqlDB.Close()
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:      "task_submit_foreign_pickup",
		Platform:    constant.TaskPlatformImage,
		UserId:      1,
		Group:       "default",
		ChannelId:   1,
		Status:      model.TaskStatusInProgress,
		Progress:    "1%",
		SubmitTime:  now,
		StartTime:   now,
		NextPollAt:  now - 1,
		StorageNode: "worker-node-a",
		PrivateData: model.TaskPrivateData{NodeName: "worker-node-a"},
	}
	require.NoError(t, db.Create(task).Error)
	require.NoError(t, saveAsyncTaskBridgeSubmission(context.Background(), task, "upstream_task_2"))
	require.NoError(t, db.Model(&model.Task{}).Where("id = ?", task.ID).Update("next_poll_at", now-1).Error)

	common.NodeName = "worker-node-b"
	runnable := model.GetRunnableImageTasks(10, now)
	require.Len(t, runnable, 1)
	require.Equal(t, "task_submit_foreign_pickup", runnable[0].TaskID)
}

func TestSettleImageTaskSuccessSkipsConsumptionWhenSettlementAlreadyApplying(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskSettlementRecord{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_settlement_already_applying",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusPending,
	}
	require.NoError(t, db.Create(task).Error)
	require.NoError(t, db.Create(&model.TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        model.TaskSettlementRecordStatusApplying,
	}).Error)

	err = settleImageTaskSuccess(context.Background(), task, imageTaskSettlementPayload{
		Result: json.RawMessage(`{"data":[{"url":"https://example.com/image.png"}]}`),
	})

	require.ErrorContains(t, err, "already applying")
	require.Equal(t, 1, task.RetryCount)

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskSettlementStatusPending, reloaded.SettlementStatus)
	require.Zero(t, reloaded.RetryCount)

	var record model.TaskSettlementRecord
	require.NoError(t, db.Where("task_primary_id = ?", task.ID).First(&record).Error)
	require.Equal(t, model.TaskSettlementRecordStatusApplying, record.Status)
}

func TestSettleImageTaskSuccessResumesAtomicApplyingSettlement(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.Task{},
		&model.TaskSettlementRecord{},
		&model.User{},
		&model.Channel{},
		&model.Log{},
		&model.TokenUsageDaily{},
	))

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldUsingSQLite := common.UsingSQLite
	oldRedisEnabled := common.RedisEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	model.DB = db
	model.LOG_DB = db
	common.UsingSQLite = true
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	common.BatchUpdateEnabled = false
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.UsingSQLite = oldUsingSQLite
		common.RedisEnabled = oldRedisEnabled
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
		_ = sqlDB.Close()
	})

	require.NoError(t, db.Create(&model.User{
		Id:       1,
		Username: "image-atomic-resume-user",
		Password: "password123",
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:     1,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "upstream-key",
		Status: common.ChannelStatusEnabled,
		Name:   "image-atomic-resume-channel",
		Group:  "default",
		Models: "gpt-image-1",
	}).Error)

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_settlement_resume_atomic",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Action:           constant.TaskActionImageGeneration,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusPending,
		Properties: model.Properties{
			OriginModelName: "gpt-image-1",
		},
		PrivateData: model.TaskPrivateData{
			BillingSource: service.BillingSourceWallet,
			BillingContext: &model.TaskBillingContext{
				OriginModelName: "gpt-image-1",
				PerCallBilling:  true,
			},
		},
	}
	require.NoError(t, db.Create(task).Error)
	require.NoError(t, db.Create(&model.TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        model.TaskSettlementRecordStatusApplying,
		Operation:     model.TaskSettlementOperationImageAtomic,
	}).Error)

	require.NoError(t, settleImageTaskSuccess(context.Background(), task, imageTaskSettlementPayload{
		Result: json.RawMessage(`{"data":[{"url":"https://example.com/image.png"}]}`),
	}))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskSettlementStatusSettled, reloaded.SettlementStatus)
	var record model.TaskSettlementRecord
	require.NoError(t, db.Where("task_primary_id = ?", task.ID).First(&record).Error)
	require.Equal(t, model.TaskSettlementRecordStatusApplied, record.Status)
	require.NotEmpty(t, record.LogPayload)
}

func TestSettleImageTaskSuccessMarksReviewWhenSettlementAlreadyApplyingIsStale(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskSettlementRecord{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})

	now := time.Now().Unix()
	bodyPath, err := common.WriteImageTaskBodyCacheFile([]byte(`{"model":"gpt-image-1"}`))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = common.RemoveDiskCacheFile(bodyPath)
	})

	task := &model.Task{
		TaskID:           "task_settlement_stale_applying",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusPending,
		PrivateData: model.TaskPrivateData{
			RequestBodyPath: bodyPath,
		},
	}
	require.NoError(t, db.Create(task).Error)
	staleAt := now - 3600
	require.NoError(t, db.Create(&model.TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        model.TaskSettlementRecordStatusApplying,
		CreatedAt:     staleAt,
		UpdatedAt:     staleAt,
	}).Error)

	require.NoError(t, settleImageTaskSuccess(context.Background(), task, imageTaskSettlementPayload{
		Result: json.RawMessage(`{"data":[{"url":"https://example.com/image.png"}]}`),
	}))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.Contains(t, reloaded.FailReason, "manual review")
	require.Zero(t, reloaded.NextPollAt)
	require.Equal(t, bodyPath, reloaded.PrivateData.RequestBodyPath)
	require.FileExists(t, bodyPath)

	var record model.TaskSettlementRecord
	require.NoError(t, db.Where("task_primary_id = ?", task.ID).First(&record).Error)
	require.Equal(t, model.TaskSettlementRecordStatusReview, record.Status)
	require.Contains(t, record.Error, "manual review")
}

func TestSettleImageTaskSuccessFinalizesExistingAppliedSettlementRecord(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskSettlementRecord{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})

	bodyPath, err := common.WriteImageTaskBodyCacheFile([]byte(`{"model":"gpt-image-1"}`))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = common.RemoveDiskCacheFile(bodyPath)
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_settlement_record_applied",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Quota:            50,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusPending,
		PrivateData: model.TaskPrivateData{
			RequestBodyPath: bodyPath,
		},
	}
	require.NoError(t, db.Create(task).Error)
	appliedQuota := 200
	require.NoError(t, db.Create(&model.TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        model.TaskSettlementRecordStatusApplied,
		Operation:     "image_consumption",
		AppliedQuota:  &appliedQuota,
		AppliedAt:     now,
	}).Error)

	require.NoError(t, settleImageTaskSuccess(context.Background(), task, imageTaskSettlementPayload{
		Result: json.RawMessage(`{"data":[{"url":"https://example.com/image.png"}]}`),
	}))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskSettlementStatusSettled, reloaded.SettlementStatus)
	require.Equal(t, appliedQuota, reloaded.Quota)
	require.Empty(t, reloaded.PrivateData.RequestBodyPath)
	require.Empty(t, reloaded.PrivateData.RequestBodyBase64)
	require.Zero(t, reloaded.RetryCount)
	require.Empty(t, reloaded.LockOwner)
	require.Zero(t, reloaded.LockUntil)
	require.NoFileExists(t, bodyPath)
}

func TestSettleImageTaskSuccessMarksReviewWhenAppliedRecordLacksQuotaEvidence(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskSettlementRecord{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_settlement_record_applied_missing_result",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusPending,
		PrivateData: model.TaskPrivateData{
			ResultBodyPath: "missing-result.json",
		},
		Data: json.RawMessage(`{"_newapi_result_file":true}`),
	}
	require.NoError(t, db.Create(task).Error)
	require.NoError(t, db.Create(&model.TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        model.TaskSettlementRecordStatusApplied,
		AppliedAt:     now,
	}).Error)

	err = settleImageTaskSuccess(context.Background(), task, imageTaskSettlementPayload{})
	require.ErrorContains(t, err, "applied quota")

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
}

func TestFailImageTaskClearsExecutionSecretsAtTerminalState(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})

	task := &model.Task{
		TaskID:   "task_terminal_secret_cleanup",
		Platform: constant.TaskPlatformImage,
		Status:   model.TaskStatusQueued,
		PrivateData: model.TaskPrivateData{
			Key:            "upstream-secret-key",
			RequestHeaders: map[string]string{"X-Provider-Secret": "secret"},
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, failImageTask(context.Background(), task, model.TaskStatusQueued, "upstream failed", false, false))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Empty(t, reloaded.PrivateData.Key)
	require.Empty(t, reloaded.PrivateData.RequestHeaders)
}

func TestSettleImageTaskSuccessMarksReviewForMissingStoredResultBeforeCreatingSettlementRecord(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskSettlementRecord{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_missing_result_before_settlement_record",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusPending,
		PrivateData: model.TaskPrivateData{
			ResultBodyPath: "missing-result.json",
		},
		Data: json.RawMessage(`{"_newapi_result_file":true}`),
	}
	require.NoError(t, db.Create(task).Error)

	require.Error(t, settleImageTaskSuccess(context.Background(), task, imageTaskSettlementPayload{}))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.Contains(t, reloaded.FailReason, "settlement result unavailable")

	var recordCount int64
	require.NoError(t, db.Model(&model.TaskSettlementRecord{}).Where("task_primary_id = ?", task.ID).Count(&recordCount).Error)
	require.Zero(t, recordCount)
}

func TestSettleImageTaskSuccessUsesCapturedEvidenceAfterResultCleanup(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.Task{},
		&model.TaskSettlementRecord{},
		&model.User{},
		&model.Channel{},
		&model.Log{},
		&model.TokenUsageDaily{},
	))

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldRedisEnabled := common.RedisEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	oldLogConsumeEnabled := common.LogConsumeEnabled
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = false
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.RedisEnabled = oldRedisEnabled
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
		common.LogConsumeEnabled = oldLogConsumeEnabled
		_ = sqlDB.Close()
	})

	require.NoError(t, db.Create(&model.User{
		Id:       1,
		Username: "evidence-user",
		Password: "password123",
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}).Error)

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_settlement_evidence_without_result",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		Quota:            0,
		Action:           constant.TaskActionImageGeneration,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusPending,
		Data:             json.RawMessage(`{"_newapi_result_file":true,"removed":true}`),
		PrivateData: model.TaskPrivateData{
			SettlementEvidenceCapturedAt: now,
			BillingSource:                service.BillingSourceWallet,
			BillingContext: &model.TaskBillingContext{
				OriginModelName: "gpt-image-1",
				PerCallBilling:  true,
			},
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, settleImageTaskSuccess(context.Background(), task, imageTaskSettlementPayload{}))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskSettlementStatusSettled, reloaded.SettlementStatus)
	require.NotContains(t, reloaded.FailReason, "settlement result unavailable")
}

func TestSettleImageTaskSuccessMarksReviewForStoredResultMarkerWithoutPath(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskSettlementRecord{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_marker_without_result_path",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusPending,
		Data:             json.RawMessage(`{"_newapi_result_file":true}`),
	}
	require.NoError(t, db.Create(task).Error)

	require.Error(t, settleImageTaskSuccess(context.Background(), task, imageTaskSettlementPayload{}))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.Contains(t, reloaded.FailReason, "stored result body path is missing")

	var recordCount int64
	require.NoError(t, db.Model(&model.TaskSettlementRecord{}).Where("task_primary_id = ?", task.ID).Count(&recordCount).Error)
	require.Zero(t, recordCount)
}

func TestSettleImageTaskSuccessMarksReviewWhenTieredBillingBodyMissing(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskSettlementRecord{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		_ = sqlDB.Close()
	})

	now := time.Now().Unix()
	expr := `param("quality") == "high" ? tier("high", p * 4) : tier("normal", p)`
	task := &model.Task{
		TaskID:           "task_missing_billing_body",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusPending,
		Data: json.RawMessage(`{
			"data": [{"url": "https://example.com/image.png"}],
			"usage": {"prompt_tokens": 100, "completion_tokens": 0, "total_tokens": 100}
		}`),
		PrivateData: model.TaskPrivateData{
			RequestContentType: "application/json",
			RequestBodyPath:    "missing-request-body.json",
			TieredBillingSnapshot: &billingexpr.BillingSnapshot{
				BillingMode:               "tiered_expr",
				ExprString:                expr,
				ExprHash:                  billingexpr.ExprHashString(expr),
				GroupRatio:                1,
				EstimatedPromptTokens:     100,
				EstimatedCompletionTokens: 0,
				EstimatedQuotaAfterGroup:  50,
				EstimatedTier:             "normal",
				QuotaPerUnit:              common.QuotaPerUnit,
				ExprVersion:               billingexpr.ExprVersion(expr),
			},
		},
	}
	require.NoError(t, db.Create(task).Error)
	require.NoError(t, db.Create(&model.TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        model.TaskSettlementRecordStatusPrepared,
	}).Error)

	require.Error(t, settleImageTaskSuccess(context.Background(), task, imageTaskSettlementPayload{}))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.Contains(t, reloaded.FailReason, "billing request body unavailable")

	var record model.TaskSettlementRecord
	require.NoError(t, db.Where("task_primary_id = ?", task.ID).First(&record).Error)
	require.Equal(t, model.TaskSettlementRecordStatusReview, record.Status)
	require.Contains(t, record.Error, "billing request body unavailable")
}

func TestSettleImageTaskSuccessKeepsSettlementWhenFixedPriceLogDeliveryFails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.Task{},
		&model.TaskSettlementRecord{},
		&model.User{},
		&model.Channel{},
		&model.TokenUsageDaily{},
	))

	brokenLogDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	brokenSQLDB, err := brokenLogDB.DB()
	require.NoError(t, err)

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldUsingSQLite := common.UsingSQLite
	oldRedisEnabled := common.RedisEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	oldLogConsumeEnabled := common.LogConsumeEnabled
	oldDataExportEnabled := common.DataExportEnabled
	model.DB = db
	model.LOG_DB = brokenLogDB
	common.UsingSQLite = true
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	common.DataExportEnabled = false
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.UsingSQLite = oldUsingSQLite
		common.RedisEnabled = oldRedisEnabled
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
		common.LogConsumeEnabled = oldLogConsumeEnabled
		common.DataExportEnabled = oldDataExportEnabled
		_ = brokenSQLDB.Close()
		_ = sqlDB.Close()
	})

	require.NoError(t, db.Create(&model.User{
		Id:       1,
		Username: "image-fixed-price-user",
		Password: "password123",
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    100000,
		Email:    "fixed-price@example.com",
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:     1,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "upstream-key",
		Status: common.ChannelStatusEnabled,
		Name:   "image-fixed-price-channel",
		Group:  "default",
		Models: "gpt-image-1",
	}).Error)

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:           "task_fixed_price_log_fails",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Quota:            200,
		Action:           constant.TaskActionImageGeneration,
		Status:           model.TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		SettlementStatus: model.TaskSettlementStatusPending,
		Properties: model.Properties{
			OriginModelName: "gpt-image-1",
		},
		PrivateData: model.TaskPrivateData{
			BillingSource: service.BillingSourceWallet,
			BillingContext: &model.TaskBillingContext{
				ModelPrice:      0.02,
				ModelRatio:      1,
				CompletionRatio: 1,
				GroupRatio:      1,
				OriginModelName: "gpt-image-1",
				PerCallBilling:  true,
			},
		},
	}
	require.NoError(t, db.Create(task).Error)
	require.NoError(t, db.Create(&model.TaskSettlementRecord{
		TaskPrimaryID: task.ID,
		PublicTaskID:  task.TaskID,
		Status:        model.TaskSettlementRecordStatusPrepared,
	}).Error)

	err = settleImageTaskSuccess(context.Background(), task, imageTaskSettlementPayload{
		Result: json.RawMessage(`{"data":[{"url":"https://example.com/image.png"}]}`),
	})
	require.NoError(t, err)

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	require.Equal(t, model.TaskSettlementStatusSettled, reloaded.SettlementStatus)
	require.Empty(t, reloaded.FailReason)

	var record model.TaskSettlementRecord
	require.NoError(t, db.Where("task_primary_id = ?", task.ID).First(&record).Error)
	require.Equal(t, model.TaskSettlementRecordStatusApplied, record.Status)
	require.NotEmpty(t, record.LogPayload)
	require.Zero(t, record.LogDeliveredAt)
	require.Equal(t, 1, record.LogAttemptCount)
	require.NotEmpty(t, record.LogError)
	var logPayload struct {
		Content string                 `json:"content"`
		Other   map[string]interface{} `json:"other"`
	}
	require.NoError(t, json.Unmarshal([]byte(record.LogPayload), &logPayload))
	require.Equal(t, true, logPayload.Other["is_task"])
	require.Equal(t, "/v1/images/generations", logPayload.Other["request_path"])
	require.EqualValues(t, 0.02, logPayload.Other["model_price"])
	require.Contains(t, logPayload.Content, string(constant.TaskActionImageGeneration))

	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	require.Equal(t, 100000-(reloaded.Quota-200), user.Quota)
	require.Equal(t, reloaded.Quota, user.UsedQuota)
	require.Equal(t, 1, user.RequestCount)

	var channel model.Channel
	require.NoError(t, db.First(&channel, 1).Error)
	require.Equal(t, int64(reloaded.Quota), channel.UsedQuota)
}

func TestPollAsyncTaskBridgeSuccessSettlesTieredBillingWithStoredBody(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.Task{},
		&model.TaskSettlementRecord{},
		&model.User{},
		&model.Token{},
		&model.Channel{},
		&model.Log{},
		&model.Option{},
		&model.TokenUsageDaily{},
	))

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldUsingSQLite := common.UsingSQLite
	oldRedisEnabled := common.RedisEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	oldLogConsumeEnabled := common.LogConsumeEnabled
	oldDataExportEnabled := common.DataExportEnabled
	oldQuotaRemindThreshold := common.QuotaRemindThreshold
	oldImageTaskFileCacheShared := constant.ImageTaskFileCacheShared
	oldImageTaskFileCacheSharedTrusted := constant.ImageTaskFileCacheSharedTrusted
	oldImageTaskSharedCacheDisabled := common.ImageTaskSharedCacheDisabled()
	oldDiskCacheConfig := common.GetDiskCacheConfig()
	diskCacheConfig := oldDiskCacheConfig
	diskCacheConfig.Path = t.TempDir()
	model.DB = db
	model.LOG_DB = db
	common.UsingSQLite = true
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	common.DataExportEnabled = false
	common.QuotaRemindThreshold = 0
	constant.ImageTaskFileCacheShared = true
	constant.ImageTaskFileCacheSharedTrusted = true
	common.SetImageTaskSharedCacheDisabled(false)
	common.SetDiskCacheConfig(diskCacheConfig)
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.UsingSQLite = oldUsingSQLite
		common.RedisEnabled = oldRedisEnabled
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
		common.LogConsumeEnabled = oldLogConsumeEnabled
		common.DataExportEnabled = oldDataExportEnabled
		common.QuotaRemindThreshold = oldQuotaRemindThreshold
		constant.ImageTaskFileCacheShared = oldImageTaskFileCacheShared
		constant.ImageTaskFileCacheSharedTrusted = oldImageTaskFileCacheSharedTrusted
		common.SetImageTaskSharedCacheDisabled(oldImageTaskSharedCacheDisabled)
		common.SetDiskCacheConfig(oldDiskCacheConfig)
		_ = sqlDB.Close()
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/image-tasks" || r.URL.Query().Get("ids") != "upstream_123" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": [{
				"task_id": "upstream_123",
				"status": "completed",
				"progress": "100%",
				"result": {
					"data": [{"b64_json": "test-b64-payload"}],
					"usage": {
						"prompt_tokens": 100,
						"completion_tokens": 0,
						"total_tokens": 100
					}
				}
			}]
		}`))
	}))
	defer upstream.Close()

	require.NoError(t, db.Create(&model.User{
		Id:       1,
		Username: "image-user",
		Password: "password123",
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    100000,
		Email:    "image@example.com",
	}).Error)
	require.NoError(t, db.Create(&model.Token{
		Id:             1,
		UserId:         1,
		Key:            "token-key",
		Status:         common.TokenStatusEnabled,
		Name:           "image-token",
		ExpiredTime:    -1,
		RemainQuota:    100000,
		UnlimitedQuota: false,
		Group:          "default",
	}).Error)
	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      1,
		Type:    constant.ChannelTypeOpenAI,
		Key:     "upstream-key",
		Status:  common.ChannelStatusEnabled,
		Name:    "async-task-bridge",
		Group:   "default",
		Models:  "gpt-image-1",
		BaseURL: &baseURL,
	}).Error)

	body := []byte(`{"model":"gpt-image-1","quality":"high","stream":false}`)
	bodyPath, err := common.WriteImageTaskBodyCacheFile(body)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove(bodyPath)
	})

	expr := `param("quality") == "high" ? tier("high", p * 4) : tier("normal", p)`
	task := &model.Task{
		TaskID:     "task_local_123",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Quota:      50,
		Action:     constant.TaskActionImageGeneration,
		Status:     model.TaskStatusSubmitted,
		Progress:   "0%",
		SubmitTime: time.Now().Add(-time.Minute).Unix(),
		StartTime:  time.Now().Add(-time.Minute).Unix(),
		Properties: model.Properties{
			OriginModelName: "gpt-image-1",
		},
		PrivateData: model.TaskPrivateData{
			ImageTaskMode:      dto.ImageTaskModeAsyncTaskBridge,
			RequestPath:        "/v1/images/generations",
			RequestMethod:      http.MethodPost,
			RequestContentType: "application/json",
			RequestBodyPath:    bodyPath,
			RequestBodySize:    int64(len(body)),
			UpstreamTaskID:     "upstream_123",
			BillingSource:      service.BillingSourceWallet,
			TokenId:            1,
			BillingContext: &model.TaskBillingContext{
				ModelRatio:      1,
				CompletionRatio: 1,
				GroupRatio:      1,
				OriginModelName: "gpt-image-1",
			},
			TieredBillingSnapshot: &billingexpr.BillingSnapshot{
				BillingMode:               "tiered_expr",
				ExprString:                expr,
				ExprHash:                  billingexpr.ExprHashString(expr),
				GroupRatio:                1,
				EstimatedPromptTokens:     100,
				EstimatedCompletionTokens: 0,
				EstimatedQuotaAfterGroup:  50,
				EstimatedTier:             "normal",
				QuotaPerUnit:              common.QuotaPerUnit,
				ExprVersion:               billingexpr.ExprVersion(expr),
			},
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, pollAsyncTaskBridgeImageTask(context.Background(), task))

	var updated model.Task
	require.NoError(t, db.First(&updated, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), updated.Status)
	require.Equal(t, model.TaskSettlementStatusSettled, updated.SettlementStatus)
	require.Empty(t, updated.PrivateData.RequestBodyPath)
	require.NoFileExists(t, bodyPath)
	require.NotContains(t, string(updated.Data), "test-b64-payload")
	require.Contains(t, string(updated.Data), imageTaskStoredResultMarker)
	require.NotEmpty(t, updated.PrivateData.ResultBodyPath)
	require.NotZero(t, updated.PrivateData.ResultBodySize)
	require.NotEmpty(t, updated.PrivateData.ResultBodySHA256)
	require.Equal(t, "application/json", updated.PrivateData.ResultContentType)
	require.FileExists(t, updated.PrivateData.ResultBodyPath)
	t.Cleanup(func() {
		_ = os.Remove(updated.PrivateData.ResultBodyPath)
	})
	storedResult, err := os.ReadFile(updated.PrivateData.ResultBodyPath)
	require.NoError(t, err)
	require.Contains(t, string(storedResult), "test-b64-payload")
	require.Equal(t, int64(len(storedResult)), updated.PrivateData.ResultBodySize)

	var log model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeConsume).First(&log).Error)
	require.Equal(t, 200, log.Quota)
	require.Equal(t, "gpt-image-1", log.ModelName)
	other, err := common.StrToMap(log.Other)
	require.NoError(t, err)
	require.Equal(t, task.TaskID, other["task_id"])

	require.Eventually(t, func() bool {
		var user model.User
		if err := db.First(&user, 1).Error; err != nil {
			return false
		}
		return user.UsedQuota == 200 && user.RequestCount == 1
	}, time.Second, 10*time.Millisecond)
}

func TestRecoverAsyncTaskBridgeSubmissionRetriesCreateWhenRecoverMissing(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.Task{},
		&model.User{},
		&model.Channel{},
	))

	oldDB := model.DB
	oldUsingSQLite := common.UsingSQLite
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldDiskCacheConfig := common.GetDiskCacheConfig()
	diskCacheConfig := oldDiskCacheConfig
	diskCacheConfig.Path = t.TempDir()
	model.DB = db
	common.UsingSQLite = true
	common.MemoryCacheEnabled = false
	common.SetDiskCacheConfig(diskCacheConfig)
	t.Cleanup(func() {
		model.DB = oldDB
		common.UsingSQLite = oldUsingSQLite
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.SetDiskCacheConfig(oldDiskCacheConfig)
		_ = sqlDB.Close()
	})

	var recoverRequests int
	var submitRequests int
	var submitBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/image-tasks":
			recoverRequests++
			if r.URL.Query().Get("ids") != "task_local_retry" {
				http.Error(w, "unexpected recover id", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"items":[],"missing_ids":["task_local_retry"]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/image-tasks/generations":
			submitRequests++
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			submitBody = string(body)
			_, _ = w.Write([]byte(`{"id":"task_local_retry","task_id":"task_local_retry","status":"queued"}`))
		default:
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer upstream.Close()

	require.NoError(t, db.Create(&model.User{
		Id:       1,
		Username: "image-user",
		Password: "password123",
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    100000,
	}).Error)
	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      1,
		Type:    constant.ChannelTypeOpenAI,
		Key:     "upstream-key",
		Status:  common.ChannelStatusEnabled,
		Name:    "async-task-bridge",
		Group:   "default",
		Models:  "gpt-image-1",
		BaseURL: &baseURL,
	}).Error)

	body := []byte(`{"model":"gpt-image-1","prompt":"cat","stream":false}`)
	bodyPath, err := common.WriteImageTaskBodyCacheFile(body)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove(bodyPath)
	})

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:     "task_local_retry",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Action:     constant.TaskActionImageGeneration,
		Status:     model.TaskStatusInProgress,
		Progress:   "1%",
		SubmitTime: now - 10,
		StartTime:  now - 5,
		Properties: model.Properties{
			OriginModelName: "gpt-image-1",
		},
		PrivateData: model.TaskPrivateData{
			ImageTaskMode:      dto.ImageTaskModeAsyncTaskBridge,
			RequestPath:        "/v1/images/generations",
			RequestMethod:      http.MethodPost,
			RequestContentType: "application/json",
			RequestBodyPath:    bodyPath,
			RequestBodySize:    int64(len(body)),
			Key:                "upstream-key",
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, recoverAsyncTaskBridgeSubmission(context.Background(), task))

	var updated model.Task
	require.NoError(t, db.First(&updated, task.ID).Error)
	require.Equal(t, 1, recoverRequests)
	require.Equal(t, 1, submitRequests)
	require.JSONEq(t, `{
		"model":"gpt-image-1",
		"prompt":"cat",
		"client_task_id":"task_local_retry",
		"stream":false
	}`, submitBody)
	require.Equal(t, model.TaskStatus(model.TaskStatusSubmitted), updated.Status)
	require.Equal(t, "task_local_retry", updated.PrivateData.UpstreamTaskID)
}

func TestImageTaskCanResubmitUncertainSubmissionHonorsCooldownAndLimit(t *testing.T) {
	now := time.Now().Unix()
	task := &model.Task{
		PrivateData: model.TaskPrivateData{
			UpstreamSubmitUncertainAt:    now,
			UpstreamSubmitUncertainCount: 1,
		},
	}

	require.False(t, imageTaskCanResubmitUncertainSubmission(task, now))

	task.PrivateData.UpstreamSubmitUncertainAt = now - int64(imageTaskUncertainSubmissionRetryCooldown.Seconds()) - 1
	require.True(t, imageTaskCanResubmitUncertainSubmission(task, now))

	task.PrivateData.UpstreamSubmitUncertainCount = imageTaskUncertainSubmissionMaxAttempts
	require.False(t, imageTaskCanResubmitUncertainSubmission(task, now))

	task.PrivateData.UpstreamSubmitUncertainAt = 0
	task.PrivateData.UpstreamSubmitUncertainCount = 0
	require.True(t, imageTaskCanResubmitUncertainSubmission(task, now))
}

func TestBuildAsyncTaskBridgeCreateBodyAppliesModelMappingAndParamOverride(t *testing.T) {
	storage, err := common.CreateBodyStorage([]byte(`{
		"model": "public-model",
		"prompt": "hello",
		"size": "1024x1024"
	}`))
	require.NoError(t, err)
	defer storage.Close()

	relayInfo := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ParamOverride: map[string]any{
				"size": "512x512",
			},
		},
	}
	outbound, err := buildAsyncTaskBridgeCreateBody(storage, "application/json", "task_local", &dto.ImageRequest{
		Model: "mapped-model",
	}, relayInfo)
	require.NoError(t, err)
	defer outbound.Close()

	body, err := io.ReadAll(outbound.Reader)
	require.NoError(t, err)
	require.Equal(t, "application/json", outbound.ContentType)
	require.JSONEq(t, `{
		"model": "mapped-model",
		"prompt": "hello",
		"size": "512x512",
		"client_task_id": "task_local",
		"stream": false
	}`, string(body))
}

func TestBuildAsyncTaskBridgeMultipartBodyStreamsToDiskAndInjectsClientTaskID(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("prompt", "hello"))
	require.NoError(t, writer.WriteField("client_task_id", "old"))
	require.NoError(t, writer.WriteField("stream", "true"))
	part, err := writer.CreateFormFile("image", "image.txt")
	require.NoError(t, err)
	_, err = part.Write([]byte("image-bytes"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	storage, err := common.CreateBodyStorage(body.Bytes())
	require.NoError(t, err)
	defer storage.Close()

	outbound, err := buildAsyncTaskBridgeCreateBody(storage, writer.FormDataContentType(), "task_local", nil, nil)
	require.NoError(t, err)
	defer outbound.Close()
	require.Greater(t, outbound.ContentLength, int64(0))

	_, params, err := mime.ParseMediaType(outbound.ContentType)
	require.NoError(t, err)
	reader := multipart.NewReader(outbound.Reader, params["boundary"])
	form, err := reader.ReadForm(32 << 20)
	require.NoError(t, err)
	defer form.RemoveAll()

	require.Equal(t, []string{"hello"}, form.Value["prompt"])
	require.Equal(t, []string{"task_local"}, form.Value["client_task_id"])
	require.Equal(t, []string{"false"}, form.Value["stream"])
	require.Len(t, form.File["image"], 1)
	file, err := form.File["image"][0].Open()
	require.NoError(t, err)
	defer file.Close()
	content, err := io.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, []byte("image-bytes"), content)
}

func TestBuildAsyncTaskBridgeMultipartBodyAppliesModelMappingAndParamOverride(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "public-model"))
	require.NoError(t, writer.WriteField("prompt", "hello"))
	require.NoError(t, writer.WriteField("size", "1024x1024"))
	part, err := writer.CreateFormFile("image", "image.txt")
	require.NoError(t, err)
	_, err = part.Write([]byte("image-bytes"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	storage, err := common.CreateBodyStorage(body.Bytes())
	require.NoError(t, err)
	defer storage.Close()

	relayInfo := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ParamOverride: map[string]any{
				"size": "512x512",
			},
		},
	}
	outbound, err := buildAsyncTaskBridgeCreateBody(storage, writer.FormDataContentType(), "task_local", &dto.ImageRequest{
		Model: "mapped-model",
	}, relayInfo)
	require.NoError(t, err)
	defer outbound.Close()

	_, params, err := mime.ParseMediaType(outbound.ContentType)
	require.NoError(t, err)
	reader := multipart.NewReader(outbound.Reader, params["boundary"])
	form, err := reader.ReadForm(32 << 20)
	require.NoError(t, err)
	defer form.RemoveAll()

	require.Equal(t, []string{"mapped-model"}, form.Value["model"])
	require.Equal(t, []string{"hello"}, form.Value["prompt"])
	require.Equal(t, []string{"512x512"}, form.Value["size"])
	require.Equal(t, []string{"task_local"}, form.Value["client_task_id"])
	require.Equal(t, []string{"false"}, form.Value["stream"])
	require.Len(t, form.File["image"], 1)
	file, err := form.File["image"][0].Open()
	require.NoError(t, err)
	defer file.Close()
	content, err := io.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, []byte("image-bytes"), content)
}

func TestImageTaskRelayStartTimePrefersExecutionStart(t *testing.T) {
	task := &model.Task{
		SubmitTime: time.Now().Add(-10 * time.Second).Unix(),
		StartTime:  time.Now().Add(-3 * time.Second).Unix(),
	}

	startTime := imageTaskRelayStartTime(task)

	require.Equal(t, task.StartTime, startTime.Unix())
}

func TestImageTaskRelayStartTimeFallsBackToSubmitTime(t *testing.T) {
	task := &model.Task{
		SubmitTime: time.Now().Add(-10 * time.Second).Unix(),
	}

	startTime := imageTaskRelayStartTime(task)

	require.Equal(t, task.SubmitTime, startTime.Unix())
}
