package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupDashboardTaskArtifactsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		_ = sqlDB.Close()
	})
	return db
}

func TestGetDashboardTaskArtifactsAllowsAdminWithSessionToken(t *testing.T) {
	setupDashboardTaskArtifactsTestDB(t)
	gin.SetMode(gin.TestMode)

	task := &model.Task{
		TaskID:   "dashboard-admin-task",
		UserId:   2026,
		Status:   model.TaskStatusSuccess,
		Platform: "image",
	}
	require.NoError(t, task.Insert())

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/task/"+task.TaskID+"/artifacts", nil)
	ctx.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	ctx.Set("id", 1)
	ctx.Set("role", common.RoleAdminUser)
	ctx.Set("token_id", 12345)

	GetDashboardTaskArtifacts(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			TaskID    string `json:"task_id"`
			Artifacts []any  `json:"artifacts"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	require.True(t, body.Success)
	require.Equal(t, task.TaskID, body.Data.TaskID)
	require.Empty(t, body.Data.Artifacts)
}

func TestGetDashboardTaskArtifactsRejectsDuplicateTaskIDForAdmin(t *testing.T) {
	setupDashboardTaskArtifactsTestDB(t)
	gin.SetMode(gin.TestMode)

	require.NoError(t, model.DB.Create(&model.Task{
		TaskID:   "dashboard-collision-task",
		UserId:   201,
		Status:   model.TaskStatusSuccess,
		Platform: "image",
	}).Error)
	require.NoError(t, model.DB.Create(&model.Task{
		TaskID:   "dashboard-collision-task",
		UserId:   202,
		Status:   model.TaskStatusSuccess,
		Platform: "image",
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/task/dashboard-collision-task/artifacts", nil)
	ctx.Params = gin.Params{{Key: "task_id", Value: "dashboard-collision-task"}}
	ctx.Set("id", 1)
	ctx.Set("role", common.RoleAdminUser)

	GetDashboardTaskArtifacts(ctx)

	require.Equal(t, http.StatusNotFound, recorder.Code, recorder.Body.String())
}
