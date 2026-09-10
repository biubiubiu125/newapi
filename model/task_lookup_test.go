package model

import (
	"context"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTaskLookupsFailClosedOnDuplicatePublicTaskID(t *testing.T) {
	previousDB := DB
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Task{}))
	DB = db
	t.Cleanup(func() {
		DB = previousDB
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})

	first := &Task{TaskID: "task_duplicate", UserId: 7, Platform: constant.TaskPlatformImage}
	second := &Task{TaskID: "task_duplicate", UserId: 7, Platform: constant.TaskPlatformImage}
	require.NoError(t, db.Create(first).Error)
	require.NoError(t, db.Create(second).Error)

	task, exists, err := GetByOnlyTaskId("task_duplicate")
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Nil(t, task)

	task, exists, err = GetUniqueByOnlyTaskId("task_duplicate")
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Nil(t, task)

	task, exists, err = GetByTaskId(7, "task_duplicate")
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Nil(t, task)

	task, exists, err = GetTaskForProtocolObservation(context.Background(), 7, constant.TaskPlatformImage, "task_duplicate")
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Nil(t, task)
}
