package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"gorm.io/gorm"
)

const (
	dropLegacyRiskTablesEnv            = "DROP_LEGACY_RISK_TABLES"
	dropLegacyConversationArtifactsEnv = "DROP_LEGACY_CONVERSATION_ARTIFACTS"
	legacyImageTaskModeValue           = "gpt_image2api_async"
)

var legacyConversationArtifactTables = []string{
	"conversation_snapshots",
	"conversation_export_tasks",
}

var legacyConversationArtifactOptions = []string{
	"ConversationSnapshotRetentionDays",
}

func migrateRiskCleanup() error {
	if !common.GetEnvOrDefaultBool(dropLegacyRiskTablesEnv, false) {
		common.SysLog("legacy risk table cleanup skipped; set " + dropLegacyRiskTablesEnv + "=true to enable")
		return nil
	}

	for _, tableName := range []string{
		"risk_whitelists",
		"risk_actions",
		"risk_events",
	} {
		if !DB.Migrator().HasTable(tableName) {
			continue
		}
		if err := DB.Migrator().DropTable(tableName); err != nil {
			return fmt.Errorf("drop legacy risk table %s: %w", tableName, err)
		}
		common.SysLog("dropped legacy risk table: " + tableName)
	}
	return nil
}

func ensureChannelOpenAIOrganizationColumn() error {
	const (
		tableName    = "channels"
		columnName   = "openai_organization"
		legacyColumn = "open_ai_organization"
	)

	if !DB.Migrator().HasTable(&Channel{}) {
		return nil
	}
	if !DB.Migrator().HasColumn(&Channel{}, columnName) {
		if err := DB.Migrator().AddColumn(&Channel{}, "OpenAIOrganization"); err != nil {
			return fmt.Errorf("failed to add %s.%s column: %w", tableName, columnName, err)
		}
		common.SysLog(fmt.Sprintf("added %s.%s column", tableName, columnName))
	}
	if !DB.Migrator().HasColumn(&Channel{}, legacyColumn) {
		return nil
	}

	result := DB.Exec(fmt.Sprintf(
		"UPDATE %s SET %s = %s WHERE (%s IS NULL OR %s = '') AND %s IS NOT NULL AND %s <> ''",
		tableName,
		columnName,
		legacyColumn,
		columnName,
		columnName,
		legacyColumn,
		legacyColumn,
	))
	if result.Error != nil {
		return fmt.Errorf("failed to migrate %s.%s from %s: %w", tableName, columnName, legacyColumn, result.Error)
	}
	if result.RowsAffected > 0 {
		common.SysLog(fmt.Sprintf("migrated %d channel openai organization values from %s to %s", result.RowsAffected, legacyColumn, columnName))
	}
	return nil
}

func migrateImageTaskResultLifecycle() error {
	if !DB.Migrator().HasTable(&Task{}) {
		return nil
	}
	columns := []string{
		"result_expires_at",
		"result_acknowledged_at",
		"result_delete_after",
		"result_cleaned_at",
		"result_cleanup_pending",
		"request_cleanup_pending",
		"request_delete_after",
		"refund_pending",
		"execution_secrets_cleaned_at",
		"sync_submission_started_at",
		"public_image_task",
		"public_image_task_token_id",
		"image_task_cancelled_at",
		"image_task_result_stored",
		"image_task_result_stored_at",
	}
	for _, column := range columns {
		if !DB.Migrator().HasColumn(&Task{}, column) {
			continue
		}
		zeroValue := "0"
		if column == "result_cleanup_pending" ||
			column == "request_cleanup_pending" ||
			column == "refund_pending" ||
			column == "public_image_task" ||
			column == "image_task_result_stored" {
			zeroValue = "false"
		}
		result := DB.Exec(fmt.Sprintf("UPDATE tasks SET %s = %s WHERE %s IS NULL", column, zeroValue, column))
		if result.Error != nil {
			return fmt.Errorf("backfill tasks.%s failed: %w", column, result.Error)
		}
	}
	if err := migrateImageTaskPublicMetadata(); err != nil {
		return err
	}
	return migrateImageTaskTerminalExecutionSecrets()
}

func migrateImageTaskPublicMetadata() error {
	if !DB.Migrator().HasTable(&Task{}) {
		return nil
	}
	for _, column := range []string{
		"public_image_task",
		"public_image_task_token_id",
		"image_task_cancelled_at",
		"image_task_result_stored",
		"image_task_result_stored_at",
	} {
		if !DB.Migrator().HasColumn(&Task{}, column) {
			return nil
		}
	}

	const batchSize = 100
	var lastID int64
	privateDataColumn := imageTaskPrivateDataTextColumn()
	for {
		var tasks []Task
		err := DB.Model(&Task{}).
			Select(
				"id",
				"private_data",
				"public_image_task",
				"public_image_task_token_id",
				"image_task_cancelled_at",
				"image_task_result_stored",
				"image_task_result_stored_at",
				"finish_time",
			).
			Where("id > ? AND platform = ?", lastID, constant.TaskPlatformImage).
			Where(fmt.Sprintf(`(
				(public_image_task = ? AND %s LIKE ?) OR
				(image_task_cancelled_at = 0 AND %s LIKE ?) OR
				(image_task_result_stored = ? AND %s LIKE ?) OR
				(image_task_result_stored_at = 0 AND %s LIKE ?)
			)`, privateDataColumn, privateDataColumn, privateDataColumn, privateDataColumn),
				false, `%"public_image_task"%`,
				`%"cancelled_at"%`,
				false, `%"result_body_path"%`,
				`%"result_stored_at"%`,
			).
			Order("id ASC").
			Limit(batchSize).
			Find(&tasks).Error
		if err != nil {
			return fmt.Errorf("load image task public metadata migration candidates: %w", err)
		}
		if len(tasks) == 0 {
			return nil
		}

		for i := range tasks {
			task := &tasks[i]
			resultStoredAt := task.PrivateData.ResultStoredAt
			if resultStoredAt <= 0 {
				resultStoredAt = task.FinishTime
			}
			updates := map[string]any{
				"public_image_task":           task.PrivateData.PublicImageTask,
				"public_image_task_token_id":  task.PrivateData.TokenId,
				"image_task_cancelled_at":     task.PrivateData.CancelledAt,
				"image_task_result_stored":    strings.TrimSpace(task.PrivateData.ResultBodyPath) != "",
				"image_task_result_stored_at": resultStoredAt,
			}
			if err := DB.Model(&Task{}).Where("id = ?", task.ID).Updates(updates).Error; err != nil {
				return fmt.Errorf("backfill image task public metadata task %d: %w", task.ID, err)
			}
			lastID = task.ID
		}
	}
}

func migrateImageTaskTerminalExecutionSecrets() error {
	if !DB.Migrator().HasTable(&Task{}) || !DB.Migrator().HasColumn(&Task{}, "execution_secrets_cleaned_at") {
		return nil
	}
	const batchSize = 100
	var lastID int64
	for {
		var taskIDs []int64
		if err := DB.Model(&Task{}).
			Select("id").
			Where("id > ? AND platform = ? AND COALESCE(execution_secrets_cleaned_at, 0) = 0", lastID, constant.TaskPlatformImage).
			Where("status IN ?", []TaskStatus{TaskStatusSuccess, TaskStatusFailure}).
			Order("id ASC").
			Limit(batchSize).
			Pluck("id", &taskIDs).Error; err != nil {
			return fmt.Errorf("load terminal image task execution secret migration candidates: %w", err)
		}
		if len(taskIDs) == 0 {
			return nil
		}
		for _, taskID := range taskIDs {
			if err := DB.Transaction(func(tx *gorm.DB) error {
				var task Task
				if err := lockForUpdate(tx).
					Select("id", "status", "settlement_status", "private_data", "execution_secrets_cleaned_at").
					Where("id = ? AND platform = ? AND COALESCE(execution_secrets_cleaned_at, 0) = 0", taskID, constant.TaskPlatformImage).
					Where("status IN ?", []TaskStatus{TaskStatusSuccess, TaskStatusFailure}).
					First(&task).Error; err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						return nil
					}
					return err
				}
				minimizeImageTaskTerminalExecutionSecrets(&task)
				return tx.Model(&Task{}).
					Where("id = ? AND COALESCE(execution_secrets_cleaned_at, 0) = 0", task.ID).
					Updates(map[string]any{
						"private_data":                 task.PrivateData,
						"execution_secrets_cleaned_at": task.ExecutionSecretsCleanedAt,
						"settlement_status":            task.SettlementStatus,
						"fail_reason":                  task.FailReason,
					}).Error
			}); err != nil {
				return fmt.Errorf("scrub terminal image task execution secrets task %d: %w", taskID, err)
			}
		}
		lastID = taskIDs[len(taskIDs)-1]
	}
}

func migrateImageTaskModeAsyncTaskBridge() error {
	if err := migrateChannelImageTaskModeAsyncTaskBridge(); err != nil {
		return err
	}
	return migrateTaskPrivateDataImageTaskModeAsyncTaskBridge()
}

func migrateChannelImageTaskModeAsyncTaskBridge() error {
	const batchSize = 500
	if !DB.Migrator().HasTable(&Channel{}) {
		return nil
	}

	var total int64
	var lastID int
	for {
		var channels []Channel
		err := DB.Model(&Channel{}).
			Select("id", "settings").
			Where("id > ?", lastID).
			Where("settings LIKE ?", "%"+legacyImageTaskModeValue+"%").
			Order("id ASC").
			Limit(batchSize).
			Find(&channels).Error
		if err != nil {
			return err
		}
		if len(channels) == 0 {
			if total > 0 {
				common.SysLog(fmt.Sprintf("migrated %d channel image task modes to %s", total, dto.ImageTaskModeAsyncTaskBridge))
			}
			return nil
		}

		for _, channel := range channels {
			lastID = channel.Id
			settings, changed, err := replaceLegacyImageTaskModeRaw(channel.OtherSettings)
			if err != nil {
				common.SysLog(fmt.Sprintf("skip channel #%d image_task_mode migration: %s", channel.Id, err.Error()))
				continue
			}
			if !changed {
				continue
			}
			result := DB.Model(&Channel{}).Where("id = ?", channel.Id).Update("settings", settings)
			if result.Error != nil {
				return result.Error
			}
			total += result.RowsAffected
		}
	}
}

func migrateTaskPrivateDataImageTaskModeAsyncTaskBridge() error {
	const batchSize = 500
	if !DB.Migrator().HasTable(&Task{}) {
		return nil
	}

	type taskPrivateDataRow struct {
		ID          int64  `gorm:"column:id"`
		PrivateData string `gorm:"column:private_data"`
	}

	var total int64
	var lastID int64
	for {
		var tasks []taskPrivateDataRow
		query := DB.Table("tasks").
			Select("id", "private_data").
			Where("id > ?", lastID)
		query = applyImageTaskPrivateDataCandidateFilter(query, []string{legacyImageTaskModeValue})
		if err := query.Order("id ASC").Limit(batchSize).Find(&tasks).Error; err != nil {
			return err
		}
		if len(tasks) == 0 {
			if total > 0 {
				common.SysLog(fmt.Sprintf("migrated %d task image task modes to %s", total, dto.ImageTaskModeAsyncTaskBridge))
			}
			return nil
		}

		for _, task := range tasks {
			lastID = task.ID
			privateData, changed, err := replaceLegacyImageTaskModeRaw(task.PrivateData)
			if err != nil {
				common.SysLog(fmt.Sprintf("skip task #%d image_task_mode migration: %s", task.ID, err.Error()))
				continue
			}
			if !changed {
				continue
			}
			result := DB.Table("tasks").Where("id = ?", task.ID).
				Update("private_data", imageTaskPrivateDataMigrationValue(privateData))
			if result.Error != nil {
				return result.Error
			}
			total += result.RowsAffected
		}
	}
}

func imageTaskPrivateDataMigrationValue(privateData string) any {
	return privateData
}

func replaceLegacyImageTaskModeRaw(jsonText string) (string, bool, error) {
	fields := map[string]json.RawMessage{}
	if err := common.UnmarshalJsonStr(jsonText, &fields); err != nil {
		return "", false, err
	}
	rawMode, ok := fields["image_task_mode"]
	if !ok {
		return "", false, nil
	}
	var mode string
	if err := common.Unmarshal(rawMode, &mode); err != nil {
		return "", false, err
	}
	if mode != legacyImageTaskModeValue {
		return "", false, nil
	}
	replacement, err := common.Marshal(dto.ImageTaskModeAsyncTaskBridge)
	if err != nil {
		return "", false, err
	}
	fields["image_task_mode"] = json.RawMessage(replacement)
	data, err := common.Marshal(fields)
	if err != nil {
		return "", false, err
	}
	return string(data), true, nil
}

func migrateImageTaskPortableStorageNodes() error {
	const batchSize = 1000
	var total int64
	for {
		var ids []int64
		privateDataWhere, privateDataArgs := imageTaskPortablePrivateDataWhere()
		err := DB.Model(&Task{}).
			Where("platform = ?", constant.TaskPlatformImage).
			Where("(storage_node = '' OR storage_node IS NULL)").
			Where(privateDataWhere, privateDataArgs...).
			Order("id ASC").
			Limit(batchSize).
			Pluck("id", &ids).Error
		if err != nil || len(ids) == 0 {
			if total > 0 {
				common.SysLog(fmt.Sprintf("migrated %d portable image task storage nodes", total))
			}
			return err
		}
		result := DB.Model(&Task{}).Where("id IN ?", ids).Updates(map[string]any{
			"storage_node": ImageTaskPortableStorageNode,
			"updated_at":   time.Now().Unix(),
		})
		if result.Error != nil {
			return result.Error
		}
		total += result.RowsAffected
		if len(ids) < batchSize {
			if total > 0 {
				common.SysLog(fmt.Sprintf("migrated %d portable image task storage nodes", total))
			}
			return nil
		}
	}
}

func imageTaskPortablePrivateDataWhere() (string, []any) {
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		return "(private_data ->> 'request_body_portable') = ? AND COALESCE(private_data ->> 'request_body_base64', '') <> ''", []any{"true"}
	}
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		return "JSON_UNQUOTE(JSON_EXTRACT(private_data, '$.request_body_portable')) = ? AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(private_data, '$.request_body_base64')), '') <> ''", []any{"true"}
	}
	column := "CAST(private_data AS TEXT)"
	return "(" + column + " LIKE ? OR " + column + " LIKE ?) AND (" + column + " LIKE ? OR " + column + " LIKE ?)", []any{
		`%"request_body_portable":true%`,
		`%"request_body_portable": true%`,
		`%"request_body_base64":"%`,
		`%"request_body_base64": "%`,
	}
}

func cleanupConversationArtifacts() {
	if !common.GetEnvOrDefaultBool(dropLegacyConversationArtifactsEnv, false) {
		common.SysLog("legacy conversation artifact cleanup skipped; set " + dropLegacyConversationArtifactsEnv + "=true to enable")
		return
	}
	if LOG_DB == nil {
		return
	}
	for _, tableName := range legacyConversationArtifactTables {
		if !LOG_DB.Migrator().HasTable(tableName) {
			continue
		}
		if err := LOG_DB.Migrator().DropTable(tableName); err != nil {
			common.SysLog(fmt.Sprintf("failed to drop legacy conversation table %s: %v", tableName, err))
		}
	}
	if err := os.RemoveAll("data/conversation-exports"); err != nil {
		common.SysLog("failed to remove legacy conversation export files: " + err.Error())
	}
}

func cleanupConversationArtifactOptions() {
	if !common.GetEnvOrDefaultBool(dropLegacyConversationArtifactsEnv, false) {
		common.SysLog("legacy conversation artifact option cleanup skipped; set " + dropLegacyConversationArtifactsEnv + "=true to enable")
		return
	}
	if DB == nil {
		return
	}
	if err := DB.Delete(&Option{}, legacyConversationArtifactOptions).Error; err != nil {
		common.SysLog("failed to remove legacy conversation options: " + err.Error())
	}
}
