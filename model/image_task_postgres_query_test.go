package model

import (
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgreSQLImageTaskQueriesAcceptBooleanResultFlag(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		dsn = "host=/var/run/postgresql user=biubiubiu dbname=newapi_image_task_sql sslmode=disable"
	}
	pg, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := pg.DB()
	require.NoError(t, err)
	require.NoError(t, pg.AutoMigrate(&Task{}, &TaskDispatchState{}))

	previousDB := DB
	previousLogDB := LOG_DB
	previousMain := common.MainDatabaseType()
	previousLog := common.LogDatabaseType()
	previousShared := constant.ImageTaskFileCacheShared
	previousAffinity := constant.ImageTaskLocalFileCacheAffinity
	DB = pg
	LOG_DB = pg
	common.SetDatabaseTypes(common.DatabaseTypePostgreSQL, common.DatabaseTypePostgreSQL)
	constant.ImageTaskFileCacheShared = false
	constant.ImageTaskLocalFileCacheAffinity = false
	t.Cleanup(func() {
		DB = previousDB
		LOG_DB = previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		constant.ImageTaskFileCacheShared = previousShared
		constant.ImageTaskLocalFileCacheAffinity = previousAffinity
		_ = sqlDB.Close()
	})

	require.NoError(t, pg.Exec("DELETE FROM tasks").Error)
	now := time.Now().Unix()
	queued := &Task{
		TaskID:     "pg_runnable_image",
		Platform:   constant.TaskPlatformImage,
		ChannelId:  9101,
		Status:     TaskStatusQueued,
		Progress:   "0%",
		NextPollAt: now - 10,
		SubmitTime: now,
	}
	require.NoError(t, pg.Create(queued).Error)

	stored := &Task{
		TaskID:                "pg_bridge_stored_success",
		Platform:              constant.TaskPlatformImage,
		ChannelId:             9101,
		Status:                TaskStatusSuccess,
		Progress:              "100%",
		SettlementStatus:      TaskSettlementStatusPending,
		ImageTaskResultStored: true,
		Data:                  []byte(`{"_newapi_result_file":true}`),
		PrivateData: TaskPrivateData{
			ImageTaskMode:  dto.ImageTaskModeAsyncTaskBridge,
			ResultBodyPath: "/tmp/newapi-pg-missing-result.json",
		},
	}
	require.NoError(t, pg.Create(stored).Error)

	review := &Task{
		TaskID:           "pg_bridge_review_inline",
		Platform:         constant.TaskPlatformImage,
		ChannelId:        9101,
		Status:           TaskStatusSuccess,
		Progress:         "100%",
		SettlementStatus: TaskSettlementStatusReview,
		NextPollAt:       now - 5,
		Data:             []byte(`{"data":[{"b64_json":"abc"}]}`),
		PrivateData: TaskPrivateData{
			ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
		},
	}
	require.NoError(t, pg.Create(review).Error)

	tasks := GetRunnableImageTasks(10, now)
	require.NotEmpty(t, tasks, "boolean image_task_result_stored must not abort the runnable UNION")
	got := map[int64]bool{}
	for _, task := range tasks {
		got[task.ID] = true
	}
	require.True(t, got[queued.ID], "queued image task was not dispatched")
	require.True(t, got[review.ID], "retryable success review was not dispatched")

	bridgeTasks, err := GetRemovedImageTaskBridgeUnsettledSuccesses(0, 10)
	require.NoError(t, err)
	require.Len(t, bridgeTasks, 1)
	require.Equal(t, stored.ID, bridgeTasks[0].ID)

	nested := &Task{
		TaskID:           "pg_bridge_nested_decoy",
		Platform:         constant.TaskPlatformImage,
		ChannelId:        9101,
		Status:           TaskStatusFailure,
		Progress:         "100%",
		SettlementStatus: TaskSettlementStatusReview,
		Quota:            500,
		PrivateData: TaskPrivateData{
			ImageTaskMode: dto.ImageTaskModeSyncWrapper,
			PluginState:   []byte(`{"copied":{"image_task_mode":"async_task_bridge"}}`),
		},
	}
	require.NoError(t, pg.Create(nested).Error)
	realBridge := &Task{
		TaskID:           "pg_bridge_real_failure",
		Platform:         constant.TaskPlatformImage,
		ChannelId:        9101,
		Status:           TaskStatusFailure,
		Progress:         "100%",
		SettlementStatus: TaskSettlementStatusReview,
		Quota:            700,
		PrivateData: TaskPrivateData{
			ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
		},
	}
	require.NoError(t, pg.Create(realBridge).Error)

	holds, holdErr := GetRemovedImageTaskBridgeRefundHolds(0, 10)
	require.NoError(t, holdErr)
	got = map[int64]bool{}
	for _, task := range holds {
		got[task.ID] = true
	}
	require.False(t, got[nested.ID], "nested image_task_mode must not select a PostgreSQL refund hold")
	require.True(t, got[realBridge.ID])
}
