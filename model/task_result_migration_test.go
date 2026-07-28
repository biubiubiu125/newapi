package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/require"
)

type legacyImageTaskResultLifecycle struct {
	ID                   int64  `gorm:"primaryKey"`
	TaskID               string `gorm:"column:task_id"`
	Platform             string
	Status               string
	SettlementStatus     string
	FinishTime           int64
	PrivateData          string
	Data                 string
	ResultExpiresAt      *int64
	ResultAcknowledgedAt *int64
	ResultDeleteAfter    *int64
	ResultCleanedAt      *int64
}

func (legacyImageTaskResultLifecycle) TableName() string {
	return "tasks"
}

func TestMigrateImageTaskResultLifecycleBackfillsLegacyNulls(t *testing.T) {
	setupRiskCleanupTestDB(t)
	require.NoError(t, DB.AutoMigrate(&legacyImageTaskResultLifecycle{}))
	require.NoError(t, DB.Create(&legacyImageTaskResultLifecycle{
		ID:               1,
		TaskID:           "legacy_result_lifecycle",
		Platform:         string(constant.TaskPlatformImage),
		Status:           TaskStatusSuccess,
		SettlementStatus: TaskSettlementStatusSettled,
		FinishTime:       time.Now().Unix(),
		PrivateData:      `{"public_image_task":true,"token_id":81,"cancelled_at":123,"result_body_path":"/cache/legacy-result.json"}`,
		Data:             `{"data":[{"url":"https://example.com/legacy.png"}]}`,
	}).Error)

	require.NoError(t, migrateImageTaskResultLifecycle())
	require.NoError(t, DB.AutoMigrate(&Task{}))
	require.NoError(t, migrateImageTaskResultLifecycle())

	var nullCount int64
	require.NoError(t, DB.Raw(`
SELECT COUNT(*) FROM tasks WHERE
	result_expires_at IS NULL OR
	result_acknowledged_at IS NULL OR
	result_delete_after IS NULL OR
	result_cleaned_at IS NULL OR
	result_cleanup_pending IS NULL OR
	request_cleanup_pending IS NULL OR
	request_delete_after IS NULL OR
	refund_pending IS NULL OR
	execution_secrets_cleaned_at IS NULL`).Scan(&nullCount).Error)
	require.Zero(t, nullCount)
	var migrated Task
	require.NoError(t, DB.Select("public_image_task", "public_image_task_token_id", "image_task_cancelled_at", "image_task_result_stored").First(&migrated, 1).Error)
	require.True(t, migrated.PublicImageTask)
	require.Equal(t, 81, migrated.PublicImageTaskTokenID)
	require.Equal(t, int64(123), migrated.ImageTaskCancelledAt)
	require.True(t, migrated.ImageTaskResultStored)

	now := time.Now().Unix()
	first, err := AcknowledgeImageTaskResult(1, now, now+120)
	require.NoError(t, err)
	require.True(t, first)
}

func TestMigrateImageTaskTerminalExecutionSecretsScrubsLegacyRows(t *testing.T) {
	setupRiskCleanupTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Task{}))

	legacyFailure := &Task{
		TaskID:   "legacy_terminal_secret_failure",
		Platform: constant.TaskPlatformImage,
		Status:   TaskStatusFailure,
		PrivateData: TaskPrivateData{
			Key:            "failure-key",
			RequestHeaders: map[string]string{"Authorization": "failure-secret"},
			BillingRequestInput: &billingexpr.RequestInput{
				Headers: map[string]string{"authorization": "failure-secret"},
				Body:    []byte(`{"secret":"failure-body"}`),
			},
		},
	}
	legacyPending := &Task{
		TaskID:           "legacy_terminal_secret_pending",
		Platform:         constant.TaskPlatformImage,
		Status:           TaskStatusSuccess,
		SettlementStatus: TaskSettlementStatusPending,
		PrivateData: TaskPrivateData{
			Key: "pending-key",
			RequestHeaders: map[string]string{
				"X-Billing-Tier": "gold",
				"Authorization":  "pending-secret",
			},
			TieredBillingSnapshot: &billingexpr.BillingSnapshot{
				BillingMode: "tiered_expr",
				ExprString:  `header("X-Billing-Tier") == "gold" && param("size") == "1024x1024" ? 1 : 2`,
			},
			BillingRequestInput: &billingexpr.RequestInput{
				Body: []byte(`{"size":"1024x1024","secret":"pending-body"}`),
			},
		},
	}
	legacySettled := &Task{
		TaskID:           "legacy_terminal_secret_settled",
		Platform:         constant.TaskPlatformImage,
		Status:           TaskStatusSuccess,
		SettlementStatus: TaskSettlementStatusSettled,
		PrivateData: TaskPrivateData{
			Key:            "settled-key",
			RequestHeaders: map[string]string{"Authorization": "settled-secret"},
			BillingRequestInput: &billingexpr.RequestInput{
				Headers: map[string]string{"authorization": "settled-secret"},
				Body:    []byte(`{"secret":"settled-body"}`),
			},
		},
	}
	legacyInvalidEvidence := &Task{
		TaskID:           "legacy_terminal_secret_invalid_evidence",
		Platform:         constant.TaskPlatformImage,
		Status:           TaskStatusSuccess,
		SettlementStatus: TaskSettlementStatusPending,
		PrivateData: TaskPrivateData{
			Key:            "invalid-evidence-key",
			RequestHeaders: map[string]string{"Authorization": "invalid-evidence-secret"},
			TieredBillingSnapshot: &billingexpr.BillingSnapshot{
				BillingMode: "tiered_expr",
				ExprString:  `header(param("header_name")) == "gold" ? 1 : 2`,
			},
			BillingRequestInput: &billingexpr.RequestInput{
				Body: []byte(`{"header_name":"X-Billing-Tier","secret":"invalid-body"}`),
			},
		},
	}
	active := &Task{
		TaskID:   "active_execution_secret",
		Platform: constant.TaskPlatformImage,
		Status:   TaskStatusInProgress,
		PrivateData: TaskPrivateData{
			Key:            "active-key",
			RequestHeaders: map[string]string{"Authorization": "active-secret"},
		},
	}
	for _, task := range []*Task{legacyFailure, legacyPending, legacySettled, legacyInvalidEvidence, active} {
		require.NoError(t, task.Insert())
	}

	require.NoError(t, migrateImageTaskTerminalExecutionSecrets())

	for _, task := range []*Task{legacyFailure, legacyPending, legacySettled, legacyInvalidEvidence} {
		reloaded, exists, err := GetTaskByID(task.ID)
		require.NoError(t, err)
		require.True(t, exists)
		require.Empty(t, reloaded.PrivateData.Key)
		require.Empty(t, reloaded.PrivateData.RequestHeaders)
		require.Greater(t, reloaded.ExecutionSecretsCleanedAt, int64(0))
	}
	pending, _, err := GetTaskByID(legacyPending.ID)
	require.NoError(t, err)
	require.NotNil(t, pending.PrivateData.BillingRequestInput)
	require.Equal(t, map[string]string{"x-billing-tier": "gold"}, pending.PrivateData.BillingRequestInput.Headers)
	require.Equal(t, map[string]any{"size": "1024x1024"}, pending.PrivateData.BillingRequestInput.Params)
	require.Empty(t, pending.PrivateData.BillingRequestInput.Body)
	settled, _, err := GetTaskByID(legacySettled.ID)
	require.NoError(t, err)
	require.Nil(t, settled.PrivateData.BillingRequestInput)
	failure, _, err := GetTaskByID(legacyFailure.ID)
	require.NoError(t, err)
	require.Nil(t, failure.PrivateData.BillingRequestInput)
	invalidEvidence, _, err := GetTaskByID(legacyInvalidEvidence.ID)
	require.NoError(t, err)
	require.Equal(t, TaskSettlementStatusReview, invalidEvidence.SettlementStatus)
	require.Contains(t, invalidEvidence.FailReason, "billing evidence migration requires manual review")
	require.NotNil(t, invalidEvidence.PrivateData.BillingRequestInput)
	require.Empty(t, invalidEvidence.PrivateData.BillingRequestInput.Body)
	require.Empty(t, invalidEvidence.PrivateData.BillingRequestInput.Headers)
	reloadedActive, _, err := GetTaskByID(active.ID)
	require.NoError(t, err)
	require.Equal(t, "active-key", reloadedActive.PrivateData.Key)
	require.Zero(t, reloadedActive.ExecutionSecretsCleanedAt)
}

func TestClearImageTaskExecutionSecretsDropsRawBodyButKeepsCapturedEvidence(t *testing.T) {
	task := &Task{
		PrivateData: TaskPrivateData{
			Key:            "upstream-key",
			RequestHeaders: map[string]string{"Authorization": "secret"},
			BillingRequestInput: &billingexpr.RequestInput{
				Headers: map[string]string{"x-billing-tier": "gold"},
				Body:    []byte(`{"secret":"raw-body"}`),
				Params:  map[string]any{"size": "1024x1024"},
			},
		},
	}

	task.ClearImageTaskExecutionSecrets()

	require.Empty(t, task.PrivateData.Key)
	require.Empty(t, task.PrivateData.RequestHeaders)
	require.Empty(t, task.PrivateData.BillingRequestInput.Body)
	require.Equal(t, map[string]string{"x-billing-tier": "gold"}, task.PrivateData.BillingRequestInput.Headers)
	require.Equal(t, map[string]any{"size": "1024x1024"}, task.PrivateData.BillingRequestInput.Params)
	require.Greater(t, task.ExecutionSecretsCleanedAt, int64(0))
}
