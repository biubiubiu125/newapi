package model

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetRunnableImageTasksLogsDatabaseError(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Task{}, &TaskDispatchState{}))

	previousDB := DB
	previousMain := common.MainDatabaseType()
	previousLog := common.LogDatabaseType()
	DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, sqlDB.Close())
	t.Cleanup(func() {
		DB = previousDB
		common.SetDatabaseTypes(previousMain, previousLog)
	})

	var buf bytes.Buffer
	common.LogWriterMu.Lock()
	previousWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &buf
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = previousWriter
		common.LogWriterMu.Unlock()
	})

	require.Nil(t, GetRunnableImageTasks(10, time.Now().Unix()))
	require.Contains(t, buf.String(), "runnable image task query failed")
	require.Contains(t, buf.String(), "runnable image task query failed (cursor)")
}

func TestGetRunnableImageTasksLogsTaskLoadError(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Task{}, &TaskDispatchState{}))
	now := time.Now().Unix()
	require.NoError(t, db.Create(&Task{
		TaskID:     "runnable_load_failure",
		Platform:   constant.TaskPlatformImage,
		ChannelId:  77,
		Status:     TaskStatusQueued,
		Progress:   "0%",
		NextPollAt: now - 10,
		SubmitTime: now,
	}).Error)

	previousDB := DB
	previousMain := common.MainDatabaseType()
	previousLog := common.LogDatabaseType()
	DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		DB = previousDB
		common.SetDatabaseTypes(previousMain, previousLog)
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})

	var buf bytes.Buffer
	common.LogWriterMu.Lock()
	previousWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &buf
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = previousWriter
		common.LogWriterMu.Unlock()
	})

	visible := GetRunnableImageTasks(10, now)
	require.Len(t, visible, 1)
	require.Equal(t, "runnable_load_failure", visible[0].TaskID)

	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("fail_task_load", func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Task" {
			tx.AddError(errors.New("task load failed"))
		}
	}))
	require.Empty(t, GetRunnableImageTasks(10, now))
	require.Contains(t, buf.String(), "runnable image task query failed (load)")
}

func TestGetRunnableImageTasksDoesNotLogMissingFairCursor(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Task{}, &TaskDispatchState{}))

	previousDB := DB
	previousMain := common.MainDatabaseType()
	previousLog := common.LogDatabaseType()
	DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		DB = previousDB
		common.SetDatabaseTypes(previousMain, previousLog)
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})

	var buf bytes.Buffer
	common.LogWriterMu.Lock()
	previousWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &buf
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = previousWriter
		common.LogWriterMu.Unlock()
	})

	require.Nil(t, GetRunnableImageTasks(10, time.Now().Unix()))
	require.NotContains(t, buf.String(), "runnable image task query failed (cursor)")
}
