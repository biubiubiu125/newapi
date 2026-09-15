package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupSetupControllerTest(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.Setup{}))
	model.InitOptionMap()

	previousSetup := constant.Setup
	previousSelfUse := operation_setting.SelfUseModeEnabled
	previousDemo := operation_setting.DemoSiteEnabled
	constant.Setup = false
	t.Cleanup(func() {
		constant.Setup = previousSetup
		operation_setting.SelfUseModeEnabled = previousSelfUse
		operation_setting.DemoSiteEnabled = previousDemo
	})
	return db
}

func TestPostSetupCreatesRootWhenMissing(t *testing.T) {
	setupSetupControllerTest(t)

	body := `{"username":"admin","password":"newpassword","confirmPassword":"newpassword"}`
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	PostSetup(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, true, resp["success"])
	require.True(t, constant.Setup)
	require.True(t, model.RootUserExists())

	root := model.GetRootUser()
	require.NotNil(t, root)
	require.Equal(t, "admin", root.Username)
	require.True(t, common.ValidatePasswordAndHash("newpassword", root.Password))
}

func TestPostSetupRotatesExistingRootPassword(t *testing.T) {
	setupSetupControllerTest(t)

	hashed, err := common.Password2Hash("oldpassword")
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.User{
		Username: "root",
		Password: hashed,
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
	}).Error)

	body := `{"username":"ignored","password":"newpassword","confirmPassword":"newpassword"}`
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	PostSetup(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, true, resp["success"], recorder.Body.String())
	require.True(t, constant.Setup)

	root := model.GetRootUser()
	require.NotNil(t, root)
	require.Equal(t, "root", root.Username)
	require.True(t, common.ValidatePasswordAndHash("newpassword", root.Password))
	require.False(t, common.ValidatePasswordAndHash("oldpassword", root.Password))
}

func TestPostSetupRejectsShortPasswordWhenRootExists(t *testing.T) {
	setupSetupControllerTest(t)

	hashed, err := common.Password2Hash("oldpassword")
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.User{
		Username: "root",
		Password: hashed,
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
	}).Error)

	body := `{"password":"123456","confirmPassword":"123456"}`
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	PostSetup(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, false, resp["success"])
	require.False(t, constant.Setup)
	root := model.GetRootUser()
	require.True(t, common.ValidatePasswordAndHash("oldpassword", root.Password))
}

func TestPostSetupDoesNotMarkInitializedWhenSetupRowCreateFails(t *testing.T) {
	db := setupSetupControllerTest(t)
	const callbackName = "test:fail_setup_create"
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "setups" {
			_ = tx.AddError(errors.New("forced setup create failure"))
		}
	}))
	t.Cleanup(func() {
		_ = db.Callback().Create().Remove(callbackName)
	})

	body := `{"username":"admin","password":"newpassword","confirmPassword":"newpassword"}`
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	PostSetup(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, false, resp["success"], recorder.Body.String())
	require.Equal(t, "系统初始化失败", resp["message"])
	require.NotContains(t, recorder.Body.String(), "forced setup create failure")
	require.False(t, constant.Setup)
	require.True(t, model.RootUserExists())
	require.Nil(t, model.GetSetup())
}
