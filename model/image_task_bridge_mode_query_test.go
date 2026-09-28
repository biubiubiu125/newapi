package model

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRemovedImageTaskBridgeRefundHoldsMatchModeFieldOnly(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Task{}))

	previousDB := DB
	previousMain := common.MainDatabaseType()
	previousLog := common.LogDatabaseType()
	DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		DB = previousDB
		common.SetDatabaseTypes(previousMain, previousLog)
		_ = sqlDB.Close()
	})

	now := time.Now().Unix()
	decoy := &Task{
		TaskID:           "bridge_mode_decoy",
		Platform:         constant.TaskPlatformImage,
		ChannelId:        41,
		Status:           TaskStatusFailure,
		Progress:         "100%",
		SettlementStatus: TaskSettlementStatusReview,
		Quota:            500,
		SubmitTime:       now,
		PrivateData: TaskPrivateData{
			ImageTaskMode: dto.ImageTaskModeSyncWrapper,
			PluginState:   json.RawMessage(`{"copied":{"image_task_mode":"async_task_bridge"},"legacy":"gpt_image2api_async"}`),
		},
	}
	require.NoError(t, db.Create(decoy).Error)

	bridge := &Task{
		TaskID:           "bridge_mode_real",
		Platform:         constant.TaskPlatformImage,
		ChannelId:        41,
		Status:           TaskStatusFailure,
		Progress:         "100%",
		SettlementStatus: TaskSettlementStatusReview,
		Quota:            700,
		SubmitTime:       now,
		PrivateData: TaskPrivateData{
			ImageTaskMode: dto.ImageTaskModeAsyncTaskBridge,
		},
	}
	require.NoError(t, db.Create(bridge).Error)

	spaced := &Task{
		TaskID:           "bridge_mode_spaced",
		Platform:         constant.TaskPlatformImage,
		ChannelId:        41,
		Status:           TaskStatusFailure,
		Progress:         "100%",
		SettlementStatus: TaskSettlementStatusReview,
		Quota:            300,
		SubmitTime:       now,
		PrivateData:      TaskPrivateData{ImageTaskMode: dto.ImageTaskModeSyncWrapper},
	}
	require.NoError(t, db.Create(spaced).Error)
	require.NoError(t, db.Exec(
		`UPDATE tasks SET private_data = ? WHERE id = ?`,
		`{"image_task_mode": "gpt_image2api_async", "request_headers":{"X-Note":"not the field"}}`,
		spaced.ID,
	).Error)

	var stored string
	require.NoError(t, db.Raw("SELECT private_data FROM tasks WHERE id = ?", decoy.ID).Scan(&stored).Error)
	require.Contains(t, stored, `"image_task_mode":"async_task_bridge"`)
	require.Contains(t, stored, `"image_task_mode":"sync_wrapper"`)

	tasks, err := GetRemovedImageTaskBridgeRefundHolds(0, 10)
	require.NoError(t, err)
	got := map[int64]bool{}
	for _, task := range tasks {
		got[task.ID] = true
	}
	require.False(t, got[decoy.ID], "a nested copy of the removed mode must not release a sync-wrapper review hold")
	require.True(t, got[bridge.ID])
	require.True(t, got[spaced.ID], "historical spacing around image_task_mode must still match")
}
