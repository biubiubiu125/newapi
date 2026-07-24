package model

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func resetLogUsernameTestTables(t *testing.T) {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM quota_data").Error)
	require.NoError(t, DB.Exec("DELETE FROM logs").Error)
	require.NoError(t, DB.Exec("DELETE FROM users").Error)
}

func TestRecordConsumeLogFallsBackToUserIdUsername(t *testing.T) {
	resetLogUsernameTestTables(t)
	oldDataExportEnabled := common.DataExportEnabled
	common.DataExportEnabled = false
	t.Cleanup(func() {
		common.DataExportEnabled = oldDataExportEnabled
	})

	require.NoError(t, DB.Create(&User{
		Id:       9101,
		Username: "consume-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	RecordConsumeLog(ctx, 9101, RecordConsumeLogParams{
		ModelName:        "gpt-4o",
		TokenName:        "prod-token",
		PromptTokens:     10,
		CompletionTokens: 5,
		Quota:            100,
	})

	var log Log
	require.NoError(t, LOG_DB.Where("user_id = ? AND type = ?", 9101, LogTypeConsume).First(&log).Error)
	require.Equal(t, "consume-owner", log.Username)
}

func TestRecordConsumeLogReturnsCreateError(t *testing.T) {
	resetLogUsernameTestTables(t)
	oldLogDB := LOG_DB
	brokenLogDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	LOG_DB = brokenLogDB
	t.Cleanup(func() {
		LOG_DB = oldLogDB
	})

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	err = RecordConsumeLog(ctx, 9191, RecordConsumeLogParams{
		ModelName: "gpt-4o",
		TokenName: "prod-token",
		Quota:     100,
	})

	require.Error(t, err)
}

func TestRecordErrorLogFallsBackToUserIdUsername(t *testing.T) {
	resetLogUsernameTestTables(t)
	require.NoError(t, DB.Create(&User{
		Id:       9102,
		Username: "error-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	RecordErrorLog(ctx, 9102, 3, "gpt-4o", "prod-token", "upstream failed", 7, 2, false, "default", nil)

	var log Log
	require.NoError(t, LOG_DB.Where("user_id = ? AND type = ?", 9102, LogTypeError).First(&log).Error)
	require.Equal(t, "error-owner", log.Username)
}

func TestMigrateLogUsernamesBackfillsLogsAndQuotaData(t *testing.T) {
	resetLogUsernameTestTables(t)
	require.NoError(t, DB.Create(&User{
		Id:       9103,
		Username: "backfill-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, DB.Create(&User{
		Id:       9104,
		Username: "existing-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{UserId: 9103, Type: LogTypeConsume, Username: "", ModelName: "gpt-4o"}).Error)
	require.NoError(t, LOG_DB.Create(&Log{UserId: 9104, Type: LogTypeConsume, Username: "kept-name", ModelName: "gpt-4o"}).Error)
	require.NoError(t, DB.Create(&QuotaData{UserID: 9103, Username: "", ModelName: "gpt-4o", CreatedAt: 1}).Error)
	require.NoError(t, DB.Create(&QuotaData{UserID: 9104, Username: "kept-quota-name", ModelName: "gpt-4o", CreatedAt: 1}).Error)

	require.NoError(t, migrateLogUsernames())

	var missingLog Log
	require.NoError(t, LOG_DB.Where("user_id = ?", 9103).First(&missingLog).Error)
	require.Equal(t, "backfill-owner", missingLog.Username)
	var existingLog Log
	require.NoError(t, LOG_DB.Where("user_id = ?", 9104).First(&existingLog).Error)
	require.Equal(t, "kept-name", existingLog.Username)

	var missingQuota QuotaData
	require.NoError(t, DB.Where("user_id = ?", 9103).First(&missingQuota).Error)
	require.Equal(t, "backfill-owner", missingQuota.Username)
	var existingQuota QuotaData
	require.NoError(t, DB.Where("user_id = ?", 9104).First(&existingQuota).Error)
	require.Equal(t, "kept-quota-name", existingQuota.Username)
}

func TestGetQuotaDataByUserIdMergesRenamedUsernameRows(t *testing.T) {
	resetLogUsernameTestTables(t)
	require.NoError(t, DB.Create(&User{
		Id:       9301,
		Username: "new-name",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, DB.Create(&QuotaData{
		UserID:    9301,
		Username:  "old-name",
		ModelName: "gpt-4o",
		CreatedAt: 3600,
		Count:     1,
		Quota:     100,
		TokenUsed: 30,
	}).Error)
	require.NoError(t, DB.Create(&QuotaData{
		UserID:    9301,
		Username:  "new-name",
		ModelName: "gpt-4o",
		CreatedAt: 3600,
		Count:     2,
		Quota:     200,
		TokenUsed: 70,
	}).Error)
	require.NoError(t, DB.Create(&QuotaData{
		UserID:    9302,
		Username:  "other-user",
		ModelName: "gpt-4o",
		CreatedAt: 3600,
		Count:     9,
		Quota:     900,
		TokenUsed: 900,
	}).Error)

	rows, err := GetQuotaDataByUserId(9301, 0, 7200)

	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, 9301, rows[0].UserID)
	require.Equal(t, "new-name", rows[0].Username)
	require.Equal(t, "gpt-4o", rows[0].ModelName)
	require.EqualValues(t, 3600, rows[0].CreatedAt)
	require.Equal(t, 3, rows[0].Count)
	require.Equal(t, 300, rows[0].Quota)
	require.Equal(t, 100, rows[0].TokenUsed)
}

func TestGetQuotaDataGroupByUserMergesRenamedUsernameRows(t *testing.T) {
	resetLogUsernameTestTables(t)
	require.NoError(t, DB.Create(&User{
		Id:       9303,
		Username: "new-name",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, DB.Create(&QuotaData{
		UserID:    9303,
		Username:  "old-name",
		ModelName: "gpt-4o",
		CreatedAt: 3600,
		Count:     1,
		Quota:     100,
		TokenUsed: 30,
	}).Error)
	require.NoError(t, DB.Create(&QuotaData{
		UserID:    9303,
		Username:  "new-name",
		ModelName: "gpt-4o",
		CreatedAt: 3600,
		Count:     2,
		Quota:     200,
		TokenUsed: 70,
	}).Error)

	rows, err := GetQuotaDataGroupByUser(0, 7200)

	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, 9303, rows[0].UserID)
	require.Equal(t, "new-name", rows[0].Username)
	require.EqualValues(t, 3600, rows[0].CreatedAt)
	require.Equal(t, 3, rows[0].Count)
	require.Equal(t, 300, rows[0].Quota)
	require.Equal(t, 100, rows[0].TokenUsed)
}

func TestGetAllQuotaDatesUsernameFilterUsesCurrentUserIdAcrossRenames(t *testing.T) {
	resetLogUsernameTestTables(t)
	require.NoError(t, DB.Create(&User{
		Id:       9304,
		Username: "new-name",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, DB.Create(&QuotaData{
		UserID:    9304,
		Username:  "old-name",
		ModelName: "gpt-4o",
		CreatedAt: 3600,
		Count:     1,
		Quota:     100,
		TokenUsed: 30,
	}).Error)
	require.NoError(t, DB.Create(&QuotaData{
		UserID:    9304,
		Username:  "new-name",
		ModelName: "gpt-4o",
		CreatedAt: 3600,
		Count:     2,
		Quota:     200,
		TokenUsed: 70,
	}).Error)

	rows, err := GetAllQuotaDates(0, 7200, "new-name")

	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, 9304, rows[0].UserID)
	require.Equal(t, "new-name", rows[0].Username)
	require.Equal(t, "gpt-4o", rows[0].ModelName)
	require.EqualValues(t, 3600, rows[0].CreatedAt)
	require.Equal(t, 3, rows[0].Count)
	require.Equal(t, 300, rows[0].Quota)
	require.Equal(t, 100, rows[0].TokenUsed)
}

func TestMigrateClickHouseLogUsernamesProcessesAllUserBatches(t *testing.T) {
	var fetchCalls []int
	var updated []int
	batches := map[int][]int{
		0: []int{1, 2},
		2: []int{3, 4},
		4: []int{},
	}

	total, err := migrateClickHouseLogUsernamesInBatches(
		func(lastUserID int, limit int) ([]int, error) {
			fetchCalls = append(fetchCalls, lastUserID)
			return batches[lastUserID], nil
		},
		func(userIDs []int) (map[int]string, error) {
			return map[int]string{
				1: "alice",
				2: "bob",
				4: "dana",
			}, nil
		},
		func(userID int, username string) (int64, error) {
			updated = append(updated, userID)
			return 1, nil
		},
	)

	require.NoError(t, err)
	require.Equal(t, []int{0, 2, 4}, fetchCalls)
	require.Equal(t, []int{1, 2, 4}, updated)
	require.EqualValues(t, 3, total)
}

func TestSumUsedQuotaByUserIdIncludesRenamedLogs(t *testing.T) {
	resetLogUsernameTestTables(t)
	now := time.Now().Unix()
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:           9201,
		Username:         "old-name",
		CreatedAt:        now,
		Type:             LogTypeConsume,
		ModelName:        "gpt-4o",
		TokenName:        "prod-token",
		Quota:            100,
		PromptTokens:     10,
		CompletionTokens: 5,
		Group:            "default",
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:           9201,
		Username:         "new-name",
		CreatedAt:        now,
		Type:             LogTypeConsume,
		ModelName:        "gpt-4o",
		TokenName:        "prod-token",
		Quota:            200,
		PromptTokens:     20,
		CompletionTokens: 10,
		Group:            "default",
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    9202,
		Username:  "other-user",
		CreatedAt: now,
		Type:      LogTypeConsume,
		ModelName: "gpt-4o",
		TokenName: "prod-token",
		Quota:     999,
		Group:     "default",
	}).Error)

	stat, err := SumUsedQuotaByUserId(LogTypeConsume, 0, 0, "gpt-4o", 9201, "prod-token", 0, "default")

	require.NoError(t, err)
	require.Equal(t, 300, stat.Quota)
	require.Equal(t, 2, stat.Rpm)
	require.Equal(t, 45, stat.Tpm)
}

func TestGetAllLogsFiltersCurrentUsernameByUserId(t *testing.T) {
	resetLogUsernameTestTables(t)
	require.NoError(t, DB.Create(&User{
		Id:       9203,
		Username: "new-name",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)
	now := time.Now().Unix()
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    9203,
		Username:  "old-name",
		CreatedAt: now,
		Type:      LogTypeConsume,
		ModelName: "gpt-4o",
		Quota:     100,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    9204,
		Username:  "new-name",
		CreatedAt: now,
		Type:      LogTypeConsume,
		ModelName: "gpt-4o",
		Quota:     999,
	}).Error)

	logs, total, err := GetAllLogs(LogTypeConsume, 0, 0, "", "new-name", "", 0, 10, 0, "", "", "")

	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, logs, 1)
	require.Equal(t, 9203, logs[0].UserId)
	require.Equal(t, "new-name", logs[0].Username)
}

func TestGetUserLogsFillsCurrentUsername(t *testing.T) {
	resetLogUsernameTestTables(t)
	require.NoError(t, DB.Create(&User{
		Id:       9210,
		Username: "self-current-name",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)
	now := time.Now().Unix()
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    9210,
		Username:  "self-old-name",
		CreatedAt: now,
		Type:      LogTypeConsume,
		ModelName: "gpt-4o",
		Quota:     100,
	}).Error)

	logs, total, err := GetUserLogs(9210, LogTypeConsume, 0, 0, "", "", 0, 10, "", "", "")

	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, logs, 1)
	require.Equal(t, "self-current-name", logs[0].Username)
}

func TestGetLogByTokenIdFillsCurrentUsername(t *testing.T) {
	resetLogUsernameTestTables(t)
	require.NoError(t, DB.Create(&User{
		Id:       9211,
		Username: "token-current-name",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)
	now := time.Now().Unix()
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    9211,
		Username:  "token-old-name",
		TokenId:   9212,
		CreatedAt: now,
		Type:      LogTypeConsume,
		ModelName: "gpt-4o",
		Quota:     100,
	}).Error)

	logs, err := GetLogByTokenId(9212)

	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, "token-current-name", logs[0].Username)
}

func TestSumUsedQuotaFiltersCurrentUsernameByUserId(t *testing.T) {
	resetLogUsernameTestTables(t)
	require.NoError(t, DB.Create(&User{
		Id:       9205,
		Username: "current-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)
	now := time.Now().Unix()
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:           9205,
		Username:         "old-owner",
		CreatedAt:        now,
		Type:             LogTypeConsume,
		ModelName:        "gpt-4o",
		TokenName:        "prod-token",
		Quota:            100,
		PromptTokens:     10,
		CompletionTokens: 5,
		Group:            "default",
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:           9206,
		Username:         "current-owner",
		CreatedAt:        now,
		Type:             LogTypeConsume,
		ModelName:        "gpt-4o",
		TokenName:        "prod-token",
		Quota:            999,
		PromptTokens:     99,
		CompletionTokens: 9,
		Group:            "default",
	}).Error)

	stat, err := SumUsedQuota(LogTypeConsume, 0, 0, "gpt-4o", "current-owner", "prod-token", 0, "default")

	require.NoError(t, err)
	require.Equal(t, 100, stat.Quota)
	require.Equal(t, 1, stat.Rpm)
	require.Equal(t, 15, stat.Tpm)
}

func TestSumUsedTokenUsesCurrentUsernameByUserId(t *testing.T) {
	resetLogUsernameTestTables(t)
	require.NoError(t, DB.Create(&User{
		Id:       9213,
		Username: "token-stat-current",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)
	now := time.Now().Unix()
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:           9213,
		Username:         "token-stat-old",
		CreatedAt:        now,
		Type:             LogTypeConsume,
		ModelName:        "gpt-4o",
		TokenName:        "prod-token",
		PromptTokens:     11,
		CompletionTokens: 7,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:           9214,
		Username:         "token-stat-current",
		CreatedAt:        now,
		Type:             LogTypeConsume,
		ModelName:        "gpt-4o",
		TokenName:        "prod-token",
		PromptTokens:     99,
		CompletionTokens: 1,
	}).Error)

	token := SumUsedToken(LogTypeConsume, 0, 0, "gpt-4o", "token-stat-current", "prod-token")

	require.Equal(t, 18, token)
}

func TestSumUsedQuotaRespectsLogTypeFilter(t *testing.T) {
	resetLogUsernameTestTables(t)
	require.NoError(t, DB.Create(&User{
		Id:       9207,
		Username: "stats-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)
	now := time.Now().Unix()
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:           9207,
		Username:         "stats-owner",
		CreatedAt:        now,
		Type:             LogTypeConsume,
		ModelName:        "gpt-4o",
		TokenName:        "prod-token",
		Quota:            100,
		PromptTokens:     10,
		CompletionTokens: 5,
		Group:            "default",
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    9207,
		Username:  "stats-owner",
		CreatedAt: now,
		Type:      LogTypeRefund,
		ModelName: "gpt-4o",
		TokenName: "prod-token",
		Quota:     40,
		Group:     "default",
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    9207,
		Username:  "stats-owner",
		CreatedAt: now,
		Type:      LogTypeConsume,
		ModelName: "gpt-4o",
		TokenName: "prod-token",
		Quota:     25,
		Group:     "default",
		Other:     `{"pre_consumed_quota":100,"actual_quota":125}`,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    9208,
		Username:  "other-owner",
		CreatedAt: now,
		Type:      LogTypeConsume,
		ModelName: "gpt-4o",
		TokenName: "prod-token",
		Quota:     999,
		Group:     "default",
	}).Error)

	allStat, err := SumUsedQuota(LogTypeUnknown, 0, 0, "gpt-4o", "stats-owner", "prod-token", 0, "default")
	require.NoError(t, err)
	require.Equal(t, 85, allStat.Quota)
	require.Equal(t, 1, allStat.Rpm)
	require.Equal(t, 15, allStat.Tpm)

	consumeStat, err := SumUsedQuota(LogTypeConsume, 0, 0, "gpt-4o", "stats-owner", "prod-token", 0, "default")
	require.NoError(t, err)
	require.Equal(t, 125, consumeStat.Quota)
	require.Equal(t, 1, consumeStat.Rpm)
	require.Equal(t, 15, consumeStat.Tpm)

	refundStat, err := SumUsedQuota(LogTypeRefund, 0, 0, "gpt-4o", "stats-owner", "prod-token", 0, "default")
	require.NoError(t, err)
	require.Equal(t, 40, refundStat.Quota)
	require.Zero(t, refundStat.Rpm)
	require.Zero(t, refundStat.Tpm)
}

func TestSumUsedQuotaByUserIdRespectsLogTypeFilter(t *testing.T) {
	resetLogUsernameTestTables(t)
	now := time.Now().Unix()
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:           9209,
		Username:         "self-owner",
		CreatedAt:        now,
		Type:             LogTypeConsume,
		ModelName:        "gpt-4o",
		TokenName:        "prod-token",
		Quota:            100,
		PromptTokens:     10,
		CompletionTokens: 5,
		Group:            "default",
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    9209,
		Username:  "self-owner",
		CreatedAt: now,
		Type:      LogTypeRefund,
		ModelName: "gpt-4o",
		TokenName: "prod-token",
		Quota:     40,
		Group:     "default",
	}).Error)

	refundStat, err := SumUsedQuotaByUserId(LogTypeRefund, 0, 0, "gpt-4o", 9209, "prod-token", 0, "default")
	require.NoError(t, err)
	require.Equal(t, 40, refundStat.Quota)
	require.Zero(t, refundStat.Rpm)
	require.Zero(t, refundStat.Tpm)
}
