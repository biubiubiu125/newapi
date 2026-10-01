package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestManageUserDisableDoesNotRestoreStaleIdentity(t *testing.T) {
	db := setupManageUserTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(2)

	user := model.User{
		Username:    "snapshot-manage",
		Password:    "old-hash",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		GitHubId:    "gh-old",
		Email:       "old@example.com",
		AuthVersion: 1,
	}
	require.NoError(t, db.Create(&user).Error)

	const callbackName = "manage_user_disable_snapshot"
	fired := false
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if fired || tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "users" {
			return
		}
		if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
			return
		}
		fired = true
		require.NoError(t, tx.Session(&gorm.Session{NewDB: true, SkipHooks: true}).Exec(
			`UPDATE users SET password = ?, github_id = ?, email = ? WHERE id = ?`,
			"new-hash", "gh-new", "new@example.com", user.Id,
		).Error)
	}))
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove(callbackName)
	})

	body, err := json.Marshal(ManageRequest{Id: user.Id, Action: "disable"})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/manage", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("id", 1)
	ctx.Set("role", common.RoleAdminUser)
	attachAdminStepUpProof(t, ctx, service.VerificationScopeAdminUserManage, service.AdminUserManageContext{UserID: user.Id, Action: "disable"})
	ManageUser(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, true, response["success"])
	require.True(t, fired, "expected the access update to read the user inside a transaction")

	var got model.User
	require.NoError(t, db.Select("password", "github_id", "email", "status", "auth_version").First(&got, user.Id).Error)
	require.Equal(t, "new-hash", got.Password)
	require.Equal(t, "gh-new", got.GitHubId)
	require.Equal(t, "new@example.com", got.Email)
	require.Equal(t, common.UserStatusDisabled, got.Status)
	require.Greater(t, got.AuthVersion, int64(1))
}
