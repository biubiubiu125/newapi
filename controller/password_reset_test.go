package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestResetPasswordUsesNormalizedEmailVerificationKey(t *testing.T) {
	setupReferralControllerTestDB(t)

	user := &model.User{
		Username:    "reset-password-user",
		Password:    "old-password",
		DisplayName: "reset-password-user",
		Email:       "reset@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, user.Insert(0))

	common.DeleteKey("reset@example.com", common.PasswordResetPurpose)
	common.RegisterVerificationCodeWithKey("reset@example.com", "reset-token", common.PasswordResetPurpose)
	t.Cleanup(func() {
		common.DeleteKey("reset@example.com", common.PasswordResetPurpose)
	})

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := bytes.NewBufferString(`{"email":" RESET@example.COM ","token":"reset-token"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/reset", body)

	ResetPassword(c)

	require.Equal(t, http.StatusOK, w.Code)
	var response struct {
		Success bool   `json:"success"`
		Data    string `json:"data"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.True(t, response.Success, response.Message)
	require.NotEmpty(t, response.Data)
	require.False(t, common.VerifyCodeWithKey("reset@example.com", "reset-token", common.PasswordResetPurpose))

	login := &model.User{
		Username: "RESET@example.COM",
		Password: response.Data,
	}
	require.NoError(t, login.ValidateAndFill())
	require.Equal(t, user.Id, login.Id)
}

func TestResetPasswordRejectsMalformedJSONWithoutConsumingToken(t *testing.T) {
	setupReferralControllerTestDB(t)

	user := &model.User{
		Username:    "reset-malformed-user",
		Password:    "old-password",
		DisplayName: "reset-malformed-user",
		Email:       "reset-malformed@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, user.Insert(0))

	common.DeleteKey("reset-malformed@example.com", common.PasswordResetPurpose)
	common.RegisterVerificationCodeWithKey("reset-malformed@example.com", "reset-token", common.PasswordResetPurpose)
	t.Cleanup(func() {
		common.DeleteKey("reset-malformed@example.com", common.PasswordResetPurpose)
	})

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := bytes.NewBufferString(`{"email":"reset-malformed@example.com","token":"reset-token"`)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/reset", body)

	ResetPassword(c)

	require.Equal(t, http.StatusOK, w.Code)
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.False(t, response.Success)
	require.True(t, common.VerifyCodeWithKey("reset-malformed@example.com", "reset-token", common.PasswordResetPurpose))
}

func TestResetPasswordRejectsSoftDeletedEmail(t *testing.T) {
	setupReferralControllerTestDB(t)

	user := &model.User{
		Username:    "deleted-reset-user",
		Password:    "old-password",
		DisplayName: "deleted-reset-user",
		Email:       "deleted-reset@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, user.Insert(0))
	require.NoError(t, user.Delete())

	common.DeleteKey("deleted-reset@example.com", common.PasswordResetPurpose)
	common.RegisterVerificationCodeWithKey("deleted-reset@example.com", "reset-token", common.PasswordResetPurpose)
	t.Cleanup(func() {
		common.DeleteKey("deleted-reset@example.com", common.PasswordResetPurpose)
	})

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := bytes.NewBufferString(`{"email":"deleted-reset@example.com","token":"reset-token"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/reset", body)

	ResetPassword(c)

	require.Equal(t, http.StatusOK, w.Code)
	var response struct {
		Success bool   `json:"success"`
		Data    string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.False(t, response.Success)
	require.Empty(t, response.Data)
	require.True(t, common.VerifyCodeWithKey("deleted-reset@example.com", "reset-token", common.PasswordResetPurpose))
}

func TestResetPasswordKeepsCodeWhenPasswordUpdateFails(t *testing.T) {
	db := setupReferralControllerTestDB(t)
	user := &model.User{
		Username:    "reset-rollback-user",
		Password:    common.GenerateVerificationCode(12),
		DisplayName: "Reset Rollback",
		Email:       "reset-rollback@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	oldPassword := user.Password
	require.NoError(t, user.Insert(0))
	require.NoError(t, common.RegisterVerificationCodeWithKey("reset-rollback@example.com", "reset-token", common.PasswordResetPurpose))

	const callbackName = "password_reset_update_fail"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "users" {
			tx.AddError(errors.New("password update failed"))
		}
	}))
	t.Cleanup(func() {
		_ = db.Callback().Update().Remove(callbackName)
	})

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := bytes.NewBufferString(`{"email":"RESET-ROLLBACK@example.com","token":"reset-token"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/reset", body)
	ResetPassword(c)

	require.Equal(t, http.StatusOK, w.Code)
	var response struct {
		Success bool   `json:"success"`
		Data    string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.False(t, response.Success)
	require.Empty(t, response.Data)
	require.True(t, common.VerifyCodeWithKey("reset-rollback@example.com", "reset-token", common.PasswordResetPurpose))

	login := &model.User{Username: "reset-rollback-user", Password: oldPassword}
	require.NoError(t, login.ValidateAndFill())
}

func TestResetPasswordReturnsNewPasswordWhenSessionRevokeFails(t *testing.T) {
	db := setupReferralControllerTestDB(t)
	user := &model.User{
		Username:    "reset-revoke-user",
		Password:    common.GenerateVerificationCode(12),
		DisplayName: "Reset Revoke",
		Email:       "reset-revoke@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, user.Insert(0))
	require.NoError(t, db.Create(&model.UserSession{
		SID:          "reset-revoke-session",
		UserID:       user.Id,
		Status:       model.UserSessionStatusActive,
		RefreshHash:  "reset-revoke-hash",
		LoginMethod:  "password",
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
		LastActiveAt: time.Now().Unix(),
	}).Error)
	require.NoError(t, common.RegisterVerificationCodeWithKey("reset-revoke@example.com", "reset-token", common.PasswordResetPurpose))

	const callbackName = "password_reset_revoke_fail"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "user_sessions" {
			tx.AddError(errors.New("session revoke failed"))
		}
	}))
	t.Cleanup(func() {
		_ = db.Callback().Update().Remove(callbackName)
	})

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := bytes.NewBufferString(`{"email":"reset-revoke@example.com","token":"reset-token"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/reset", body)
	ResetPassword(c)

	require.Equal(t, http.StatusOK, w.Code)
	var response struct {
		Success bool   `json:"success"`
		Data    string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.NotEmpty(t, response.Data)
	require.False(t, common.VerifyCodeWithKey("reset-revoke@example.com", "reset-token", common.PasswordResetPurpose))

	login := &model.User{Username: "reset-revoke-user", Password: response.Data}
	require.NoError(t, login.ValidateAndFill())
}
