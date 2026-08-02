package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/utils/tests"
)

func TestChannelAutoMigrateUsesOpenAIOrganizationColumnName(t *testing.T) {
	setupRiskCleanupTestDB(t)

	require.NoError(t, DB.AutoMigrate(&Channel{}))
	require.True(t, DB.Migrator().HasColumn(&Channel{}, "openai_organization"))
	require.False(t, DB.Migrator().HasColumn(&Channel{}, "open_ai_organization"))
}

func TestEnsureChannelOpenAIOrganizationColumnMigratesLegacyColumn(t *testing.T) {
	setupRiskCleanupTestDB(t)

	require.NoError(t, DB.Exec(`
CREATE TABLE channels (
	id integer primary key,
	open_ai_organization text
)`).Error)
	require.NoError(t, DB.Exec(
		"INSERT INTO channels (id, open_ai_organization) VALUES (?, ?)",
		1,
		"org-legacy",
	).Error)
	require.False(t, DB.Migrator().HasColumn(&Channel{}, "openai_organization"))

	require.NoError(t, ensureChannelOpenAIOrganizationColumn())
	require.True(t, DB.Migrator().HasColumn(&Channel{}, "openai_organization"))

	var organization string
	require.NoError(t, DB.Raw(
		"SELECT openai_organization FROM channels WHERE id = ?",
		1,
	).Scan(&organization).Error)
	require.Equal(t, "org-legacy", organization)

	require.NoError(t, ensureChannelOpenAIOrganizationColumn())
}

func TestMigrateImageTaskModeAsyncTaskBridgeRenamesLegacyValue(t *testing.T) {
	setupRiskCleanupTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Channel{}))

	require.NoError(t, DB.Create(&Channel{
		Id:            1,
		Name:          "legacy image task mode",
		OtherSettings: `{"image_task_mode":"gpt_image2api_async","disable_task_polling_sleep":true,"custom_large_id":9007199254740993}`,
	}).Error)
	require.NoError(t, DB.Create(&Channel{
		Id:            2,
		Name:          "new image task mode",
		OtherSettings: `{"image_task_mode":"` + dto.ImageTaskModeAsyncTaskBridge + `"}`,
	}).Error)

	require.NoError(t, migrateImageTaskModeAsyncTaskBridge())

	var legacy Channel
	require.NoError(t, DB.First(&legacy, "id = ?", 1).Error)
	require.JSONEq(t, `{"image_task_mode":"async_task_bridge","disable_task_polling_sleep":true,"custom_large_id":9007199254740993}`, legacy.OtherSettings)
	require.Contains(t, legacy.OtherSettings, `"custom_large_id":9007199254740993`)

	var current Channel
	require.NoError(t, DB.First(&current, "id = ?", 2).Error)
	require.JSONEq(t, `{"image_task_mode":"async_task_bridge"}`, current.OtherSettings)
}

func TestMigrateImageTaskModeAsyncTaskBridgeRenamesTaskPrivateDataLegacyValue(t *testing.T) {
	setupRiskCleanupTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Channel{}))
	require.NoError(t, DB.AutoMigrate(&Task{}))

	require.NoError(t, DB.Create(&Task{
		ID:          1,
		TaskID:      "legacy_async_task",
		Platform:    constant.TaskPlatformImage,
		Status:      TaskStatusSubmitted,
		PrivateData: TaskPrivateData{},
	}).Error)
	rawPrivateData := `{"image_task_mode":"gpt_image2api_async","upstream_task_id":"upstream_123","custom_large_id":9007199254740993,"unknown_object":{"keep":true}}`
	require.NoError(t, DB.Exec(
		"UPDATE tasks SET private_data = ? WHERE id = ?",
		rawPrivateData,
		1,
	).Error)

	require.NoError(t, migrateImageTaskModeAsyncTaskBridge())

	var privateDataRaw string
	require.NoError(t, DB.Raw(
		"SELECT private_data FROM tasks WHERE id = ?",
		1,
	).Scan(&privateDataRaw).Error)
	require.JSONEq(t, `{"image_task_mode":"async_task_bridge","upstream_task_id":"upstream_123","custom_large_id":9007199254740993,"unknown_object":{"keep":true}}`, privateDataRaw)
	require.Contains(t, privateDataRaw, `"custom_large_id":9007199254740993`)

	var task Task
	require.NoError(t, DB.First(&task, "id = ?", 1).Error)
	require.Equal(t, dto.ImageTaskModeAsyncTaskBridge, task.PrivateData.ImageTaskMode)
	require.Equal(t, "upstream_123", task.PrivateData.UpstreamTaskID)
}

func TestImageTaskPrivateDataMigrationValueUsesJSONString(t *testing.T) {
	value := imageTaskPrivateDataMigrationValue(`{"image_task_mode":"async_task_bridge"}`)

	require.IsType(t, "", value)
}

func TestApplyImageTaskPrivateDataCandidateFilterUsesDatabaseSpecificTextCast(t *testing.T) {
	dummyDB, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{DryRun: true})
	require.NoError(t, err)
	t.Cleanup(func() {
		common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	})

	tests := []struct {
		name     string
		database common.DatabaseType
		wantSQL  string
	}{
		{
			name:     "sqlite",
			database: common.DatabaseTypeSQLite,
			wantSQL:  "CAST(private_data AS TEXT) LIKE ?",
		},
		{
			name:     "mysql",
			database: common.DatabaseTypeMySQL,
			wantSQL:  "CAST(private_data AS CHAR) LIKE ?",
		},
		{
			name:     "postgres",
			database: common.DatabaseTypePostgreSQL,
			wantSQL:  "private_data::text LIKE ?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			common.SetDatabaseTypes(tt.database, common.DatabaseTypeSQLite)
			var rows []struct {
				ID int64
			}
			tx := applyImageTaskPrivateDataCandidateFilter(
				dummyDB.Table("tasks").Select("id"),
				[]string{legacyImageTaskModeValue},
			).Find(&rows)

			require.NoError(t, tx.Error)
			require.Contains(t, tx.Statement.SQL.String(), tt.wantSQL)
			require.Equal(t, []any{"%" + legacyImageTaskModeValue + "%"}, tx.Statement.Vars)
		})
	}
}
