package model

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to open test db: " + err.Error())
	}
	DB = db
	LOG_DB = db

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	initCol()

	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get sql.DB: " + err.Error())
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&Task{},
		&TaskDispatchState{},
		&ImageTaskChannelLease{},
		&ImageTaskClientTaskIDLock{},
		&ImageTaskCreateGuard{},
		&ImageTaskCreateRateBucket{},
		&ImageTaskCreateReservation{},
		&TaskSettlementRecord{},
		&User{},
		&UserLoginIdentifier{},
		&UserSession{},
		&AuthFlow{},
		&ExternalIdentityClaim{},
		&PasskeyCredential{},
		&TwoFA{},
		&TwoFABackupCode{},
		&Token{},
		&TokenUsageReset{},
		&TokenUsageDaily{},
		&Log{},
		&Channel{},
		&QuotaData{},
		&Ability{},
		&TopUp{},
		&Redemption{},
		&SubscriptionPlan{},
		&SubscriptionOrder{},
		&UserSubscription{},
		&CustomOAuthProvider{},
		&UserOAuthBinding{},
		&ReferralAffiliate{},
		&ReferralBinding{},
		&ReferralClick{},
		&ReferralCommissionAccount{},
		&ReferralCommission{},
		&ReferralCommissionLedger{},
		&ReferralAsset{},
		&ReferralWithdrawal{},
		&ReferralWithdrawalItem{},
		&ReferralSettlementBatch{},
		&ReferralCommissionJob{},
		&ReferralAdminAuditLog{},
		&PerfMetric{},
		&MidjourneySettlementRecord{},
		&Ticket{},
		&TicketMessage{},
		&TicketAttachment{},
		&TicketSequence{},
		&TelegramPushRecord{},
		&SystemInstance{},
		&SystemTask{},
		&SystemTaskLock{},
	); err != nil {
		panic("failed to migrate: " + err.Error())
	}

	os.Exit(m.Run())
}

func truncateTables(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		DB.Exec("DELETE FROM tasks")
		DB.Exec("DELETE FROM task_dispatch_states")
		DB.Exec("DELETE FROM image_task_channel_leases")
		DB.Exec("DELETE FROM image_task_client_task_id_locks")
		DB.Exec("DELETE FROM image_task_create_reservations")
		DB.Exec("DELETE FROM image_task_create_rate_buckets")
		DB.Exec("DELETE FROM image_task_create_guards")
		DB.Exec("DELETE FROM task_settlement_records")
		DB.Exec("DELETE FROM users")
		DB.Exec("DELETE FROM user_login_identifiers")
		DB.Exec("DELETE FROM user_sessions")
		DB.Exec("DELETE FROM auth_flows")
		DB.Exec("DELETE FROM external_identity_claims")
		DB.Exec("DELETE FROM passkey_credentials")
		DB.Exec("DELETE FROM two_fa_backup_codes")
		DB.Exec("DELETE FROM two_fas")
		DB.Exec("DELETE FROM tokens")
		DB.Exec("DELETE FROM token_usage_resets")
		DB.Exec("DELETE FROM token_usage_dailies")
		DB.Exec("DELETE FROM logs")
		DB.Exec("DELETE FROM channels")
		DB.Exec("DELETE FROM quota_data")
		DB.Exec("DELETE FROM abilities")
		DB.Exec("DELETE FROM top_ups")
		DB.Exec("DELETE FROM redemptions")
		DB.Exec("DELETE FROM subscription_orders")
		DB.Exec("DELETE FROM subscription_plans")
		DB.Exec("DELETE FROM user_subscriptions")
		DB.Exec("DELETE FROM custom_oauth_providers")
		DB.Exec("DELETE FROM user_oauth_bindings")
		DB.Exec("DELETE FROM referral_admin_audit_logs")
		DB.Exec("DELETE FROM referral_settlement_batches")
		DB.Exec("DELETE FROM referral_withdrawal_items")
		DB.Exec("DELETE FROM referral_withdrawals")
		DB.Exec("DELETE FROM referral_assets")
		DB.Exec("DELETE FROM referral_clicks")
		DB.Exec("DELETE FROM referral_bindings")
		DB.Exec("DELETE FROM referral_affiliates")
		DB.Exec("DELETE FROM referral_commission_ledgers")
		DB.Exec("DELETE FROM referral_commissions")
		DB.Exec("DELETE FROM referral_commission_accounts")
		DB.Exec("DELETE FROM referral_commission_jobs")
		DB.Exec("DELETE FROM midjourney_settlement_records")
		DB.Exec("DELETE FROM perf_metrics")
		DB.Exec("DELETE FROM ticket_attachments")
		DB.Exec("DELETE FROM ticket_messages")
		DB.Exec("DELETE FROM ticket_sequences")
		DB.Exec("DELETE FROM tickets")
		DB.Exec("DELETE FROM telegram_push_records")
		DB.Exec("DELETE FROM system_instances")
		DB.Exec("DELETE FROM system_task_locks")
		DB.Exec("DELETE FROM system_tasks")
	})
}

func insertTask(t *testing.T, task *Task) {
	t.Helper()
	task.CreatedAt = time.Now().Unix()
	task.UpdatedAt = time.Now().Unix()
	require.NoError(t, DB.Create(task).Error)
}

func resetImageTaskFairChannelCursorForTest(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Where(taskDispatchStateKeyEq(imageTaskFairChannelCursorKey)).Delete(&TaskDispatchState{}).Error)
}

func TestTaskDispatchStateKeyPredicateQuotesColumn(t *testing.T) {
	stmt := DB.Session(&gorm.Session{DryRun: true}).
		Where(taskDispatchStateKeyEq(imageTaskFairChannelCursorKey)).
		Find(&TaskDispatchState{}).Statement

	require.Contains(t, stmt.SQL.String(), "`key`")
}

func TestTaskSettlementUpdatesTreatNoopRowsAffectedAsExistingRow(t *testing.T) {
	body, err := os.ReadFile("task.go")
	require.NoError(t, err)
	source := string(body)

	for _, fn := range []string{
		"func (t *Task) UpdateSubmitSettlementError() error",
		"func (t *Task) UpdateQuota() error",
	} {
		start := strings.Index(source, fn)
		require.NotEqual(t, -1, start, fn)
		end := strings.Index(source[start+len(fn):], "\nfunc ")
		if end == -1 {
			end = len(source)
		} else {
			end += start + len(fn)
		}
		block := source[start:end]
		require.Contains(t, block, "taskRowExists(t.ID)")
		require.Contains(t, block, "return nil")
	}
}

func TestGetRunnableImageTasksFairByChannelBoundsChannelSampling(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM tasks").Error)
	resetImageTaskFairChannelCursorForTest(t)

	now := time.Now().Unix()
	for channelID := 1; channelID <= 10; channelID++ {
		for i := 0; i < 2; i++ {
			insertTask(t, &Task{
				TaskID:     "task_image_fallback",
				Platform:   constant.TaskPlatformImage,
				UserId:     1,
				Group:      "default",
				ChannelId:  channelID,
				Status:     TaskStatusQueued,
				Progress:   "0%",
				SubmitTime: now,
				NextPollAt: now - 1,
			})
		}
	}

	tasks := getRunnableImageTasksFairByChannel(3, now)

	require.Len(t, tasks, 3)
	require.Equal(t, 1, tasks[0].ChannelId)
	require.Equal(t, 2, tasks[1].ChannelId)
	require.Equal(t, 3, tasks[2].ChannelId)
}

func TestGetRunnableImageTasksFairRotatesChannelsAcrossPasses(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM tasks").Error)
	resetImageTaskFairChannelCursorForTest(t)

	now := time.Now().Unix()
	for channelID := 1; channelID <= 6; channelID++ {
		for i := 0; i < 3; i++ {
			insertTask(t, &Task{
				TaskID:     "task_image_round_robin",
				Platform:   constant.TaskPlatformImage,
				UserId:     1,
				Group:      "default",
				ChannelId:  channelID,
				Status:     TaskStatusQueued,
				Progress:   "0%",
				SubmitTime: now,
				NextPollAt: now - 1,
			})
		}
	}

	firstPass := getRunnableImageTasksFair(3, now)
	secondPass := getRunnableImageTasksFair(3, now)
	thirdPass := getRunnableImageTasksFair(3, now)

	require.Equal(t, []int{1, 2, 3}, taskChannelIDs(firstPass))
	require.Equal(t, []int{4, 5, 6}, taskChannelIDs(secondPass))
	require.Equal(t, []int{1, 2, 3}, taskChannelIDs(thirdPass))
	require.Equal(t, int64(3), getImageTaskFairChannelCursor())
}

func TestGetRunnableImageTasksFairUsesPersistedCursor(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM tasks").Error)
	resetImageTaskFairChannelCursorForTest(t)

	now := time.Now().Unix()
	for channelID := 1; channelID <= 6; channelID++ {
		insertTask(t, &Task{
			TaskID:     "task_image_persisted_cursor",
			Platform:   constant.TaskPlatformImage,
			UserId:     1,
			Group:      "default",
			ChannelId:  channelID,
			Status:     TaskStatusQueued,
			Progress:   "0%",
			SubmitTime: now,
			NextPollAt: now - 1,
		})
	}

	setImageTaskFairChannelCursor(3)

	tasks := getRunnableImageTasksFair(3, now)

	require.Equal(t, []int{4, 5, 6}, taskChannelIDs(tasks))
}

func TestGetRunnableImageTasksFairByChannelRotatesFallbackChannelsAcrossPasses(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM tasks").Error)
	resetImageTaskFairChannelCursorForTest(t)

	now := time.Now().Unix()
	for channelID := 1; channelID <= 7; channelID++ {
		for i := 0; i < 2; i++ {
			insertTask(t, &Task{
				TaskID:     "task_image_fallback_round_robin",
				Platform:   constant.TaskPlatformImage,
				UserId:     1,
				Group:      "default",
				ChannelId:  channelID,
				Status:     TaskStatusQueued,
				Progress:   "0%",
				SubmitTime: now,
				NextPollAt: now - 1,
			})
		}
	}

	firstPass := getRunnableImageTasksFairByChannel(3, now)
	secondPass := getRunnableImageTasksFairByChannel(3, now)
	thirdPass := getRunnableImageTasksFairByChannel(3, now)

	require.Equal(t, []int{1, 2, 3}, taskChannelIDs(firstPass))
	require.Equal(t, []int{4, 5, 6}, taskChannelIDs(secondPass))
	require.Equal(t, []int{7, 1, 2}, taskChannelIDs(thirdPass))
}

func TestGetRunnableImageTasksHonorsLocalFileCacheAffinity(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM tasks").Error)
	resetImageTaskFairChannelCursorForTest(t)
	oldShared := constant.ImageTaskFileCacheShared
	oldAffinity := constant.ImageTaskLocalFileCacheAffinity
	oldNode := common.NodeName
	constant.ImageTaskFileCacheShared = false
	constant.ImageTaskLocalFileCacheAffinity = true
	common.NodeName = "node-a"
	t.Cleanup(func() {
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskLocalFileCacheAffinity = oldAffinity
		common.NodeName = oldNode
	})

	now := time.Now().Unix()
	insertTask(t, &Task{
		TaskID:      "task_image_local_node",
		Platform:    constant.TaskPlatformImage,
		UserId:      1,
		Group:       "default",
		ChannelId:   1,
		Status:      TaskStatusQueued,
		Progress:    "0%",
		SubmitTime:  now,
		NextPollAt:  now - 1,
		StorageNode: "node-a",
	})
	insertTask(t, &Task{
		TaskID:      "task_image_other_node",
		Platform:    constant.TaskPlatformImage,
		UserId:      1,
		Group:       "default",
		ChannelId:   2,
		Status:      TaskStatusQueued,
		Progress:    "0%",
		SubmitTime:  now,
		NextPollAt:  now - 1,
		StorageNode: "node-b",
	})
	insertTask(t, &Task{
		TaskID:     "task_image_empty_node",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  3,
		Status:     TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		NextPollAt: now - 1,
	})

	tasks := GetRunnableImageTasks(10, now)

	require.Equal(t, []string{"task_image_local_node"}, taskIDs(tasks))
}

func TestGetRunnableImageTasksRestrictsToNodeWhenSharedCacheDisabled(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM tasks").Error)
	resetImageTaskFairChannelCursorForTest(t)
	oldShared := constant.ImageTaskFileCacheShared
	oldAffinity := constant.ImageTaskLocalFileCacheAffinity
	oldNode := common.NodeName
	oldSharedDisabled := common.ImageTaskSharedCacheDisabled()
	constant.ImageTaskFileCacheShared = true
	constant.ImageTaskLocalFileCacheAffinity = true
	common.NodeName = "node-a"
	common.SetImageTaskSharedCacheDisabled(true)
	t.Cleanup(func() {
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskLocalFileCacheAffinity = oldAffinity
		common.NodeName = oldNode
		common.SetImageTaskSharedCacheDisabled(oldSharedDisabled)
	})

	now := time.Now().Unix()
	insertTask(t, &Task{
		TaskID:      "task_image_shared_local_node",
		Platform:    constant.TaskPlatformImage,
		UserId:      1,
		Group:       "default",
		ChannelId:   1,
		Status:      TaskStatusQueued,
		Progress:    "0%",
		SubmitTime:  now,
		NextPollAt:  now - 1,
		StorageNode: "node-a",
	})
	insertTask(t, &Task{
		TaskID:      "task_image_shared_other_node",
		Platform:    constant.TaskPlatformImage,
		UserId:      1,
		Group:       "default",
		ChannelId:   2,
		Status:      TaskStatusQueued,
		Progress:    "0%",
		SubmitTime:  now,
		NextPollAt:  now - 1,
		StorageNode: "node-b",
	})
	insertTask(t, &Task{
		TaskID:     "task_image_shared_portable_node",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  3,
		Status:     TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		NextPollAt: now - 1,
		PrivateData: TaskPrivateData{
			RequestBodyBase64:   "e30=",
			RequestBodyPortable: true,
		},
	})
	insertTask(t, &Task{
		TaskID:      "task_image_shared_portable_sentinel",
		Platform:    constant.TaskPlatformImage,
		UserId:      1,
		Group:       "default",
		ChannelId:   4,
		Status:      TaskStatusQueued,
		Progress:    "0%",
		SubmitTime:  now,
		NextPollAt:  now - 1,
		StorageNode: ImageTaskPortableStorageNode,
		PrivateData: TaskPrivateData{
			RequestBodyBase64:   "e30=",
			RequestBodyPortable: true,
		},
	})
	insertTask(t, &Task{
		TaskID:     "task_image_shared_empty_nonportable",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  5,
		Status:     TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		NextPollAt: now - 1,
	})
	require.NoError(t, migrateImageTaskPortableStorageNodes())

	tasks := GetRunnableImageTasks(10, now)

	require.ElementsMatch(t, []string{"task_image_shared_local_node", "task_image_shared_portable_node", "task_image_shared_portable_sentinel"}, taskIDs(tasks))
	var migrated Task
	require.NoError(t, DB.Where("task_id = ?", "task_image_shared_portable_node").First(&migrated).Error)
	require.Equal(t, ImageTaskPortableStorageNode, migrated.StorageNode)
}

func TestGetRunnableImageTasksIncludesForeignNodeSettlementTasks(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM tasks").Error)
	resetImageTaskFairChannelCursorForTest(t)
	oldShared := constant.ImageTaskFileCacheShared
	oldAffinity := constant.ImageTaskLocalFileCacheAffinity
	oldNode := common.NodeName
	constant.ImageTaskFileCacheShared = false
	constant.ImageTaskLocalFileCacheAffinity = true
	common.NodeName = "node-a"
	t.Cleanup(func() {
		constant.ImageTaskFileCacheShared = oldShared
		constant.ImageTaskLocalFileCacheAffinity = oldAffinity
		common.NodeName = oldNode
	})

	now := time.Now().Unix()
	// 已消失节点上的待结算任务必须能被其他节点接管结算。
	insertTask(t, &Task{
		TaskID:           "task_image_dead_node_settlement",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           TaskStatusSuccess,
		SettlementStatus: TaskSettlementStatusPending,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		NextPollAt:       now - 1,
		StorageNode:      "node-dead",
	})
	// 已消失节点上的执行中任务仍受 storage_node 过滤（其他节点没有请求体文件）。
	insertTask(t, &Task{
		TaskID:      "task_image_dead_node_queued",
		Platform:    constant.TaskPlatformImage,
		UserId:      1,
		Group:       "default",
		ChannelId:   2,
		Status:      TaskStatusQueued,
		Progress:    "0%",
		SubmitTime:  now,
		NextPollAt:  now - 1,
		StorageNode: "node-dead",
	})

	tasks := GetRunnableImageTasks(10, now)
	require.Equal(t, []string{"task_image_dead_node_settlement"}, taskIDs(tasks))

	require.True(t, HasRunnableImageTasks(now))
	nextAt, ok := GetNextRunnableImageTaskAt(now)
	require.False(t, ok)
	require.Zero(t, nextAt)
}

func TestGetOpenImageTaskCachePathsOnlyKeepsActiveReferences(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM tasks").Error)
	now := time.Now().Unix()
	insertTask(t, &Task{
		TaskID:     "task_image_open",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		PrivateData: TaskPrivateData{
			RequestBodyPath: "/tmp/body-open",
		},
	})
	insertTask(t, &Task{
		TaskID:           "task_image_pending_settlement",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		SettlementStatus: TaskSettlementStatusPending,
		PrivateData: TaskPrivateData{
			RequestBodyPath: "/tmp/body-pending",
			ResultBodyPath:  "/tmp/result-pending",
		},
	})
	insertTask(t, &Task{
		TaskID:           "task_image_pending_expired_result",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now - int64((13 * time.Hour).Seconds()),
		SettlementStatus: TaskSettlementStatusPending,
		ResultExpiresAt:  now - 1,
		PrivateData: TaskPrivateData{
			ResultBodyPath: "/tmp/result-pending-expired",
		},
	})
	insertTask(t, &Task{
		TaskID:           "task_image_settled",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		SettlementStatus: TaskSettlementStatusSettled,
		PrivateData: TaskPrivateData{
			RequestBodyPath: "/tmp/body-settled",
			ResultBodyPath:  "/tmp/result-settled",
		},
	})
	insertTask(t, &Task{
		TaskID:           "task_image_review",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		SettlementStatus: TaskSettlementStatusReview,
		PrivateData: TaskPrivateData{
			RequestBodyPath: "/tmp/body-review",
			ResultBodyPath:  "/tmp/result-review",
		},
	})
	insertTask(t, &Task{
		TaskID:           "task_image_settled_retained",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now - int64((13 * time.Hour).Seconds()),
		SettlementStatus: TaskSettlementStatusSettled,
		ResultExpiresAt:  now + 3600,
		PrivateData: TaskPrivateData{
			RequestBodyPath: "/tmp/body-settled-retained",
			ResultBodyPath:  "/tmp/result-settled-retained",
		},
	})
	insertTask(t, &Task{
		TaskID:                "task_image_execution_review_retained",
		Platform:              constant.TaskPlatformImage,
		UserId:                1,
		Group:                 "default",
		ChannelId:             1,
		Status:                TaskStatusFailure,
		Progress:              "100%",
		SubmitTime:            now - int64((24 * time.Hour).Seconds()),
		SettlementStatus:      TaskSettlementStatusReview,
		RequestCleanupPending: true,
		RequestDeleteAfter:    now + 3600,
		PrivateData: TaskPrivateData{
			RequestBodyPath: "/tmp/body-execution-review-retained",
			ResultBodyPath:  "/tmp/result-execution-review-retained",
		},
	})

	bodyPaths, resultPaths, err := GetOpenImageTaskCachePaths(2)

	require.NoError(t, err)
	require.Contains(t, bodyPaths, filepath.Clean("/tmp/body-open"))
	require.Contains(t, bodyPaths, filepath.Clean("/tmp/body-pending"))
	require.Contains(t, bodyPaths, filepath.Clean("/tmp/body-review"))
	require.Contains(t, bodyPaths, filepath.Clean("/tmp/body-execution-review-retained"))
	require.NotContains(t, bodyPaths, filepath.Clean("/tmp/body-settled-retained"))
	require.NotContains(t, bodyPaths, filepath.Clean("/tmp/body-settled"))
	require.Contains(t, resultPaths, filepath.Clean("/tmp/result-pending"))
	require.NotContains(t, resultPaths, filepath.Clean("/tmp/result-pending-expired"))
	require.Contains(t, resultPaths, filepath.Clean("/tmp/result-review"))
	require.Contains(t, resultPaths, filepath.Clean("/tmp/result-settled-retained"))
	require.NotContains(t, resultPaths, filepath.Clean("/tmp/result-execution-review-retained"))
	require.NotContains(t, resultPaths, filepath.Clean("/tmp/result-settled"))
}

func TestGetOpenImageTaskCachePathsForCandidatesFiltersBeforeScanning(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM tasks").Error)
	now := time.Now().Unix()
	bodyOpen := filepath.Clean("/tmp/body-candidate-open")
	bodyOther := filepath.Clean("/tmp/body-candidate-other")
	resultPending := filepath.Clean("/tmp/result-candidate-pending")
	resultSettled := filepath.Clean("/tmp/result-candidate-settled")
	insertTask(t, &Task{
		TaskID:     "task_image_candidate_open",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		PrivateData: TaskPrivateData{
			RequestBodyPath: bodyOpen,
		},
	})
	insertTask(t, &Task{
		TaskID:     "task_image_candidate_other",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		PrivateData: TaskPrivateData{
			RequestBodyPath: bodyOther,
		},
	})
	insertTask(t, &Task{
		TaskID:           "task_image_candidate_pending",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		SettlementStatus: TaskSettlementStatusPending,
		PrivateData: TaskPrivateData{
			ResultBodyPath: resultPending,
		},
	})
	insertTask(t, &Task{
		TaskID:           "task_image_candidate_settled",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		SettlementStatus: TaskSettlementStatusSettled,
		ResultExpiresAt:  now + 3600,
		PrivateData: TaskPrivateData{
			ResultBodyPath: resultSettled,
		},
	})

	bodyPaths, resultPaths, err := GetOpenImageTaskCachePathsForCandidates(
		map[string]struct{}{
			bodyOpen: {},
		},
		map[string]struct{}{
			resultPending: {},
			resultSettled: {},
		},
		1,
	)

	require.NoError(t, err)
	require.Equal(t, map[string]struct{}{bodyOpen: {}}, bodyPaths)
	require.Equal(t, map[string]struct{}{resultPending: {}, resultSettled: {}}, resultPaths)
}

func TestGetRunnableImageTasksTreatsNullProgressAsUnfinished(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM tasks").Error)
	resetImageTaskFairChannelCursorForTest(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:     "task_image_null_progress",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		NextPollAt: now - 1,
	}
	insertTask(t, task)
	require.NoError(t, DB.Exec("UPDATE tasks SET progress = NULL WHERE id = ?", task.ID).Error)

	require.True(t, HasRunnableImageTasks(now))
	tasks := GetRunnableImageTasks(10, now)
	require.Equal(t, []string{task.TaskID}, taskIDs(tasks))
	claimed, ok, err := ClaimTaskLease(task.ID, "owner-null-progress", now, 60)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, claimed)
}

func TestGetTimedOutUnfinishedTasksTreatsNullProgressAsUnfinished(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM tasks").Error)
	now := time.Now().Unix()
	task := &Task{
		TaskID:     "task_timeout_null_progress",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     TaskStatusInProgress,
		Progress:   "0%",
		SubmitTime: now - 3600,
		StartTime:  now - 3600,
		NextPollAt: now - 1,
	}
	insertTask(t, task)
	require.NoError(t, DB.Exec("UPDATE tasks SET progress = NULL WHERE id = ?", task.ID).Error)

	tasks := GetTimedOutUnfinishedTasks(now-60, 10)

	require.Equal(t, []string{task.TaskID}, taskIDs(tasks))
}

func TestRunnableImageTasksIgnoreTerminalTaskWithPendingSettlementResidue(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM tasks").Error)
	resetImageTaskFairChannelCursorForTest(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:           "task_image_failed_pending_residue",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           TaskStatusFailure,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		NextPollAt:       now - 1,
		SettlementStatus: TaskSettlementStatusPending,
	}
	insertTask(t, task)

	tasks := GetRunnableImageTasks(10, now)
	claimed, ok, err := ClaimTaskLease(task.ID, "owner-terminal-residue", now, 60)

	require.Empty(t, tasks)
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, claimed)
}

func TestClaimTaskLeaseRequiresImagePlatform(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM tasks").Error)
	now := time.Now().Unix()
	task := &Task{
		TaskID:     "task_non_image_not_claimed_by_image_lease",
		Platform:   constant.TaskPlatformMidjourney,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		NextPollAt: now - 1,
	}
	insertTask(t, task)

	claimed, ok, err := ClaimTaskLease(task.ID, "owner-non-image", now, 60)

	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, claimed)
	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	require.Empty(t, reloaded.LockOwner)
	require.Zero(t, reloaded.LockUntil)
}

func TestImageTaskChannelLeaseEnforcesSlotsAndExpiry(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM image_task_channel_leases").Error)
	now := time.Now().Unix()

	acquired, err := TryAcquireImageTaskChannelLease(7, 101, "owner-a", now, 60, 1)
	require.NoError(t, err)
	require.True(t, acquired)

	acquired, err = TryAcquireImageTaskChannelLease(7, 102, "owner-b", now, 60, 1)
	require.NoError(t, err)
	require.False(t, acquired)
	count, err := CountActiveImageTaskChannelLeases(7, now)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	renewed, err := RenewImageTaskChannelLease("owner-a", now+10, 60)
	require.NoError(t, err)
	require.True(t, renewed)
	count, err = CountActiveImageTaskChannelLeases(7, now+69)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	acquired, err = TryAcquireImageTaskChannelLease(7, 102, "owner-b", now+71, 60, 1)
	require.NoError(t, err)
	require.True(t, acquired)
	count, err = CountActiveImageTaskChannelLeases(7, now+71)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
}

func taskChannelIDs(tasks []*Task) []int {
	channelIDs := make([]int, 0, len(tasks))
	for _, task := range tasks {
		if task == nil {
			continue
		}
		channelIDs = append(channelIDs, task.ChannelId)
	}
	return channelIDs
}

func taskIDs(tasks []*Task) []string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if task == nil {
			continue
		}
		ids = append(ids, task.TaskID)
	}
	return ids
}

// ---------------------------------------------------------------------------
// Snapshot / Equal — pure logic tests (no DB)
// ---------------------------------------------------------------------------

func TestSnapshotEqual_Same(t *testing.T) {
	s := taskSnapshot{
		Status:     TaskStatusInProgress,
		Progress:   "50%",
		StartTime:  1000,
		FinishTime: 0,
		FailReason: "",
		ResultURL:  "",
		Data:       json.RawMessage(`{"key":"value"}`),
	}
	assert.True(t, s.Equal(s))
}

func TestSnapshotEqual_DifferentStatus(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{}`)}
	b := taskSnapshot{Status: TaskStatusSuccess, Data: json.RawMessage(`{}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_DifferentProgress(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Progress: "30%", Data: json.RawMessage(`{}`)}
	b := taskSnapshot{Status: TaskStatusInProgress, Progress: "60%", Data: json.RawMessage(`{}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_DifferentData(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{"a":1}`)}
	b := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{"a":2}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_NilVsEmpty(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Data: nil}
	b := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage{}}
	// bytes.Equal(nil, []byte{}) == true
	assert.True(t, a.Equal(b))
}

func TestSnapshot_Roundtrip(t *testing.T) {
	task := &Task{
		Status:     TaskStatusInProgress,
		Progress:   "42%",
		StartTime:  1234,
		FinishTime: 5678,
		FailReason: "timeout",
		PrivateData: TaskPrivateData{
			ResultURL: "https://example.com/result.mp4",
		},
		Data: json.RawMessage(`{"model":"test-model"}`),
	}
	snap := task.Snapshot()
	assert.Equal(t, task.Status, snap.Status)
	assert.Equal(t, task.Progress, snap.Progress)
	assert.Equal(t, task.StartTime, snap.StartTime)
	assert.Equal(t, task.FinishTime, snap.FinishTime)
	assert.Equal(t, task.FailReason, snap.FailReason)
	assert.Equal(t, task.PrivateData.ResultURL, snap.ResultURL)
	assert.JSONEq(t, string(task.Data), string(snap.Data))
}

// ---------------------------------------------------------------------------
// UpdateWithStatus CAS — DB integration tests
// ---------------------------------------------------------------------------

func TestUpdateWithStatus_Win(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID:   "task_cas_win",
		Status:   TaskStatusInProgress,
		Progress: "50%",
		Data:     json.RawMessage(`{}`),
	}
	insertTask(t, task)

	task.Status = TaskStatusSuccess
	task.Progress = "100%"
	won, err := task.UpdateWithStatus(TaskStatusInProgress)
	require.NoError(t, err)
	assert.True(t, won)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, TaskStatusSuccess, reloaded.Status)
	assert.Equal(t, "100%", reloaded.Progress)
}

func TestUpdateWithStatus_Lose(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID: "task_cas_lose",
		Status: TaskStatusFailure,
		Data:   json.RawMessage(`{}`),
	}
	insertTask(t, task)

	task.Status = TaskStatusSuccess
	won, err := task.UpdateWithStatus(TaskStatusInProgress) // wrong fromStatus
	require.NoError(t, err)
	assert.False(t, won)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, TaskStatusFailure, reloaded.Status) // unchanged
}

func TestApplyImageTaskCancelBeforeExecutionRejectsUpstreamSubmissionEvidence(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:     "task_cancel_upstream_guard",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
		PrivateData: TaskPrivateData{
			UpstreamTaskID: "upstream-already-submitted",
		},
	}
	insertTask(t, task)

	cancel := *task
	cancel.Status = TaskStatusFailure
	cancel.Progress = "100%"
	cancel.FailReason = "image task cancelled by client"
	cancel.FinishTime = now
	cancel.PrivateData.CancelledAt = now
	cancel.PrivateData.UpstreamTaskID = ""

	won, err := ApplyImageTaskCancelBeforeExecution(&cancel, TaskStatusQueued, now)
	require.NoError(t, err)
	require.False(t, won)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	require.Equal(t, TaskStatus(TaskStatusQueued), reloaded.Status)
	require.Equal(t, "upstream-already-submitted", reloaded.PrivateData.UpstreamTaskID)
	require.Zero(t, reloaded.PrivateData.CancelledAt)
}

func TestApplyImageTaskCancelBeforeExecutionAllowsUnlockedQueuedTask(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:     "task_cancel_ok",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: now,
	}
	insertTask(t, task)

	cancel := *task
	cancel.Status = TaskStatusFailure
	cancel.Progress = "100%"
	cancel.FailReason = "image task cancelled by client"
	cancel.FinishTime = now
	cancel.PrivateData.CancelledAt = now

	won, err := ApplyImageTaskCancelBeforeExecution(&cancel, TaskStatusQueued, now)
	require.NoError(t, err)
	require.True(t, won)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	require.Equal(t, TaskStatus(TaskStatusFailure), reloaded.Status)
	require.Equal(t, now, reloaded.PrivateData.CancelledAt)
}

func TestUpdateWithStatusAndLeaseRejectsLostOwner(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:     "task_lease_owner",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     TaskStatusInProgress,
		Progress:   "1%",
		SubmitTime: now,
		LockOwner:  "owner-b",
		LockUntil:  now + 60,
	}
	insertTask(t, task)

	stale := *task
	stale.Status = TaskStatusSuccess
	stale.Progress = "100%"
	stale.LockOwner = ""
	stale.LockUntil = 0

	won, err := stale.UpdateWithStatusAndLease(TaskStatusInProgress, "owner-a", now)

	require.NoError(t, err)
	require.False(t, won)
	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	require.Equal(t, TaskStatus(TaskStatusInProgress), reloaded.Status)
	require.Equal(t, "owner-b", reloaded.LockOwner)
}

func TestUpdateWithStatusAndLeaseRejectsExpiredLease(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:     "task_lease_expired",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     TaskStatusInProgress,
		Progress:   "1%",
		SubmitTime: now,
		LockOwner:  "owner-a",
		LockUntil:  now - 1,
	}
	insertTask(t, task)

	task.Status = TaskStatusSuccess
	task.Progress = "100%"
	task.LockOwner = ""
	task.LockUntil = 0
	won, err := task.UpdateWithStatusAndLease(TaskStatusInProgress, "owner-a", now)

	require.NoError(t, err)
	require.False(t, won)
	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	require.Equal(t, TaskStatus(TaskStatusInProgress), reloaded.Status)
	require.Equal(t, "owner-a", reloaded.LockOwner)
}

func TestRenewTaskLeaseExtendsOnlyCurrentOwner(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:     "task_lease_renew",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     TaskStatusInProgress,
		Progress:   "1%",
		SubmitTime: now,
		LockOwner:  "owner-a",
		LockUntil:  now + 10,
	}
	insertTask(t, task)

	renewed, err := RenewTaskLease(task.ID, "owner-b", now, 60)
	require.NoError(t, err)
	require.False(t, renewed)

	renewed, err = RenewTaskLease(task.ID, "owner-a", now, 60)
	require.NoError(t, err)
	require.True(t, renewed)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	require.Equal(t, now+60, reloaded.LockUntil)
}

func TestRenewTaskLeaseRejectsBoundaryExpiredLease(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:     "task_lease_renew_boundary",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     TaskStatusInProgress,
		Progress:   "1%",
		SubmitTime: now,
		LockOwner:  "owner-a",
		LockUntil:  now,
	}
	insertTask(t, task)

	renewed, err := RenewTaskLease(task.ID, "owner-a", now, 60)

	require.NoError(t, err)
	require.False(t, renewed)
	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	require.Equal(t, now, reloaded.LockUntil)
}

func TestReleaseTaskLeaseAllowsExpiredLeaseForSameOwner(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:     "task_lease_release_expired",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     TaskStatusInProgress,
		Progress:   "1%",
		SubmitTime: now,
		LockOwner:  "owner-a",
		LockUntil:  now - 1,
		NextPollAt: now - 10,
	}
	insertTask(t, task)

	require.NoError(t, ReleaseTaskLease(task.ID, "owner-a", now+30, 3))

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	require.Empty(t, reloaded.LockOwner)
	require.Zero(t, reloaded.LockUntil)
	require.Equal(t, now+30, reloaded.NextPollAt)
	require.Equal(t, 3, reloaded.RetryCount)
}

func TestTaskStatusQueryTreatsImageSettlementReviewAsFailure(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	reviewTask := &Task{
		TaskID:           "task_image_review_status_query",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		Status:           TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		SettlementStatus: TaskSettlementStatusReview,
	}
	videoReviewTask := &Task{
		TaskID:           "task_video_review_status_query",
		Platform:         constant.TaskPlatform("video"),
		UserId:           1,
		Group:            "default",
		Status:           TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		SettlementStatus: TaskSettlementStatusReview,
	}
	successTask := &Task{
		TaskID:           "task_image_success_status_query",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		Status:           TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		SettlementStatus: TaskSettlementStatusSettled,
	}
	failedTask := &Task{
		TaskID:     "task_image_failed_status_query",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		Status:     TaskStatusFailure,
		Progress:   "100%",
		SubmitTime: now,
	}
	insertTask(t, reviewTask)
	insertTask(t, videoReviewTask)
	insertTask(t, successTask)
	insertTask(t, failedTask)

	successIDs := taskIDSet(TaskGetAllTasks(0, 10, SyncTaskQueryParams{Status: string(TaskStatusSuccess)}))
	require.Contains(t, successIDs, successTask.TaskID)
	require.NotContains(t, successIDs, reviewTask.TaskID)
	require.NotContains(t, successIDs, videoReviewTask.TaskID)
	require.Equal(t, int64(1), TaskCountAllTasks(SyncTaskQueryParams{Status: string(TaskStatusSuccess)}))

	failureIDs := taskIDSet(TaskGetAllTasks(0, 10, SyncTaskQueryParams{Status: string(TaskStatusFailure)}))
	require.Contains(t, failureIDs, failedTask.TaskID)
	require.Contains(t, failureIDs, reviewTask.TaskID)
	require.Contains(t, failureIDs, videoReviewTask.TaskID)
	require.Equal(t, int64(3), TaskCountAllTasks(SyncTaskQueryParams{Status: string(TaskStatusFailure)}))
}

func taskIDSet(tasks []*Task) map[string]bool {
	ids := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		if task != nil {
			ids[task.TaskID] = true
		}
	}
	return ids
}

func TestUpdateWithStatusAndLeasePreservesRenewedLease(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:     "task_lease_update_preserve_renew",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     TaskStatusInProgress,
		Progress:   "1%",
		SubmitTime: now,
		LockOwner:  "owner-a",
		LockUntil:  now + 10,
	}
	insertTask(t, task)

	renewedUntil := now + 121
	renewed, err := RenewTaskLease(task.ID, "owner-a", now+1, 120)
	require.NoError(t, err)
	require.True(t, renewed)

	stale := *task
	stale.Status = TaskStatusSubmitted
	stale.Progress = "50%"
	stale.LockOwner = ""
	stale.LockUntil = 0
	won, err := stale.UpdateWithStatusAndLease(TaskStatusInProgress, "owner-a", now+2)

	require.NoError(t, err)
	require.True(t, won)
	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	require.Equal(t, TaskStatus(TaskStatusSubmitted), reloaded.Status)
	require.Equal(t, "50%", reloaded.Progress)
	require.Equal(t, "owner-a", reloaded.LockOwner)
	require.Equal(t, renewedUntil, reloaded.LockUntil)
}

func TestUpdateWithStatusAndLeaseClearsLeaseOnTerminalStatus(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:     "task_lease_update_terminal_clear",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  1,
		Status:     TaskStatusInProgress,
		Progress:   "1%",
		SubmitTime: now,
		LockOwner:  "owner-a",
		LockUntil:  now + 60,
		NextPollAt: now - 1,
	}
	insertTask(t, task)

	task.Status = TaskStatusSuccess
	task.Progress = "100%"
	task.LockOwner = ""
	task.LockUntil = 0
	task.NextPollAt = 0
	won, err := task.UpdateWithStatusAndLease(TaskStatusInProgress, "owner-a", now)

	require.NoError(t, err)
	require.True(t, won)
	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	require.Equal(t, TaskStatus(TaskStatusSuccess), reloaded.Status)
	require.Empty(t, reloaded.LockOwner)
	require.Zero(t, reloaded.LockUntil)
	require.Zero(t, reloaded.NextPollAt)
}

func TestUpdateSettlementStatusIgnoresExpiredLease(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	task := &Task{
		TaskID:           "task_settlement_ignores_expired_lease",
		Platform:         constant.TaskPlatformImage,
		UserId:           1,
		Group:            "default",
		ChannelId:        1,
		Status:           TaskStatusSuccess,
		Progress:         "100%",
		SubmitTime:       now,
		FinishTime:       now,
		LockOwner:        "owner-a",
		LockUntil:        now - 1,
		SettlementStatus: TaskSettlementStatusPending,
	}
	insertTask(t, task)

	task.SettlementStatus = TaskSettlementStatusSettled
	task.LockOwner = ""
	task.LockUntil = 0
	won, err := task.UpdateSettlementStatus(TaskStatusSuccess, TaskSettlementStatusPending)

	require.NoError(t, err)
	require.True(t, won)
	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	require.Equal(t, TaskStatus(TaskStatusSuccess), reloaded.Status)
	require.Equal(t, TaskSettlementStatusSettled, reloaded.SettlementStatus)
	require.Empty(t, reloaded.LockOwner)
	require.Zero(t, reloaded.LockUntil)
}

func TestUpdateWithStatus_ConcurrentWinner(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID: "task_cas_race",
		Status: TaskStatusInProgress,
		Quota:  1000,
		Data:   json.RawMessage(`{}`),
	}
	insertTask(t, task)

	const goroutines = 5
	wins := make([]bool, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			t := &Task{}
			*t = Task{
				ID:       task.ID,
				TaskID:   task.TaskID,
				Status:   TaskStatusSuccess,
				Progress: "100%",
				Quota:    task.Quota,
				Data:     json.RawMessage(`{}`),
			}
			t.CreatedAt = task.CreatedAt
			t.UpdatedAt = time.Now().Unix()
			won, err := t.UpdateWithStatus(TaskStatusInProgress)
			if err == nil {
				wins[idx] = won
			}
		}(i)
	}
	wg.Wait()

	winCount := 0
	for _, w := range wins {
		if w {
			winCount++
		}
	}
	assert.Equal(t, 1, winCount, "exactly one goroutine should win the CAS")
}
