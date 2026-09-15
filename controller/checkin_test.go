package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func enableCheckinForTest(t *testing.T) {
	t.Helper()
	setting := operation_setting.GetCheckinSetting()
	previous := *setting
	setting.Enabled = true
	setting.MinQuota = 1000
	setting.MaxQuota = 1000
	t.Cleanup(func() { *setting = previous })
}

func TestGetCheckinStatusHidesDatabaseError(t *testing.T) {
	_ = setupModelListControllerTestDB(t)
	enableCheckinForTest(t)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/user/checkin/status?month=2026-03", nil)
	ctx.Set("id", 7)

	GetCheckinStatus(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	body := recorder.Body.String()
	require.NotContains(t, strings.ToLower(body), "no such table")
	require.NotContains(t, strings.ToLower(body), "sqlite")
	require.NotContains(t, body, "checkins")
	require.NotContains(t, strings.ToLower(body), "sql")
	var payload map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, false, payload["success"])
	message := fmt.Sprint(payload["message"])
	require.NotEqual(t, "", message)
}

func TestDoCheckinKeepsAlreadyCheckedInMessage(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Checkin{}))
	enableCheckinForTest(t)

	user := &model.User{
		Username: "checkin-user",
		Password: "password-placeholder",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    10000,
	}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Create(&model.Checkin{
		UserId:       user.Id,
		CheckinDate:  time.Now().Format("2006-01-02"),
		QuotaAwarded: 1000,
		CreatedAt:    time.Now().Unix(),
	}).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/checkin", nil)
	ctx.Set("id", user.Id)

	DoCheckin(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, false, payload["success"])
	require.Equal(t, "今日已签到", payload["message"])
}
