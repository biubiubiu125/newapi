package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCreateUserStoresPasswordBeyondLegacyLimit(t *testing.T) {
	setupModelListControllerTestDB(t)
	require.NoError(t, i18n.Init())

	password := strings.Repeat("😀", 19)
	body, err := json.Marshal(map[string]any{
		"username":     "long-pass-user",
		"password":     password,
		"display_name": "long-pass-user",
		"role":         common.RoleCommonUser,
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Set("role", common.RoleRootUser)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/", strings.NewReader(string(body)))
	ctx.Request.Header.Set("Content-Type", "application/json")

	CreateUser(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`, recorder.Body.String())

	var user model.User
	require.NoError(t, model.DB.Where("username = ?", "long-pass-user").First(&user).Error)
	require.True(t, common.ValidatePasswordAndHash(password, user.Password))
}

func TestUpdateUserStoresPasswordBeyondLegacyLimit(t *testing.T) {
	setupModelListControllerTestDB(t)
	require.NoError(t, i18n.Init())

	user := &model.User{
		Username:    "update-pass-user",
		Password:    "12345678",
		DisplayName: "update-pass-user",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	require.NoError(t, user.Insert(0))

	password := strings.Repeat("名", 30)
	body, err := json.Marshal(map[string]any{
		"id":           user.Id,
		"username":     user.Username,
		"display_name": user.DisplayName,
		"password":     password,
		"email":        "",
		"group":        "default",
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Set("role", common.RoleRootUser)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/user/", strings.NewReader(string(body)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	attachAdminStepUpProof(t, ctx, service.VerificationScopeAdminUserUpdate, service.AdminUserContext{UserID: user.Id})

	UpdateUser(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`, recorder.Body.String())

	var stored model.User
	require.NoError(t, model.DB.Where("id = ?", user.Id).First(&stored).Error)
	require.True(t, common.ValidatePasswordAndHash(password, stored.Password))
}

func TestCreateUserRejectsDisplayNameAndEmailOverBackendLimits(t *testing.T) {
	setupModelListControllerTestDB(t)
	require.NoError(t, i18n.Init())

	cases := []map[string]any{
		{
			"username":     "name-too-long",
			"password":     "12345678",
			"display_name": strings.Repeat("名", 21),
			"role":         common.RoleCommonUser,
		},
		{
			"username": "email-too-long",
			"password": "12345678",
			"email":    strings.Repeat("a", 40) + "@example.com",
			"role":     common.RoleCommonUser,
		},
	}
	for _, payload := range cases {
		body, err := json.Marshal(payload)
		require.NoError(t, err)
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Set("id", 1)
		ctx.Set("role", common.RoleRootUser)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/", strings.NewReader(string(body)))
		ctx.Request.Header.Set("Content-Type", "application/json")

		CreateUser(ctx)

		require.Contains(t, recorder.Body.String(), `"success":false`, recorder.Body.String())
	}
	var count int64
	require.NoError(t, model.DB.Model(&model.User{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestCreateUserRejectsRolesOutsideWhitelist(t *testing.T) {
	setupModelListControllerTestDB(t)
	require.NoError(t, i18n.Init())

	for _, role := range []int{7, 99, -1} {
		body, err := json.Marshal(map[string]any{
			"username":     fmt.Sprintf("illegal-role-%d", role),
			"password":     "12345678",
			"display_name": "Illegal Role",
			"role":         role,
		})
		require.NoError(t, err)
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Set("id", 1)
		ctx.Set("role", common.RoleRootUser)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/", strings.NewReader(string(body)))
		ctx.Request.Header.Set("Content-Type", "application/json")

		CreateUser(ctx)

		require.Contains(t, recorder.Body.String(), `"success":false`, recorder.Body.String())
	}
	var count int64
	require.NoError(t, model.DB.Model(&model.User{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestUpdateUserStoresLiteralSentinelPassword(t *testing.T) {
	setupModelListControllerTestDB(t)
	user := &model.User{
		Username:    "sentinel-admin-edit",
		Password:    "12345678",
		DisplayName: "Sentinel",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	require.NoError(t, user.Insert(0))

	body, err := json.Marshal(map[string]any{
		"id":           user.Id,
		"username":     user.Username,
		"display_name": user.DisplayName,
		"password":     "$I_LOVE_U",
		"email":        "",
		"group":        "default",
	})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Set("role", common.RoleRootUser)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/user/", strings.NewReader(string(body)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	attachAdminStepUpProof(t, ctx, service.VerificationScopeAdminUserUpdate, service.AdminUserContext{UserID: user.Id})

	UpdateUser(ctx)

	require.Contains(t, recorder.Body.String(), `"success":true`, recorder.Body.String())
	stored := &model.User{Username: user.Username, Password: "$I_LOVE_U"}
	require.NoError(t, stored.ValidateAndFill())
	require.Equal(t, user.Id, stored.Id)
	old := &model.User{Username: user.Username, Password: "12345678"}
	require.Error(t, old.ValidateAndFill())
}

func TestUpdateSelfStoresLiteralSentinelPassword(t *testing.T) {
	user, identity := setupSecurityEnrollmentTest(t)
	proof := issueSecurityEnrollmentProof(t, identity, service.VerificationOperation{Scope: service.VerificationScopePasswordChange}, service.VerificationMethodPassword)
	body, err := json.Marshal(map[string]any{
		"original_password": "enrollment-password",
		"password":          "$I_LOVE_U",
	})
	require.NoError(t, err)

	recorder := securityEnrollmentRequest(http.MethodPut, "/api/user/self", string(body), proof, identity, UpdateSelf)

	require.Contains(t, recorder.Body.String(), `"success":true`, recorder.Body.String())
	var stored model.User
	require.NoError(t, model.DB.Where("id = ?", user.Id).First(&stored).Error)
	require.True(t, common.ValidatePasswordAndHash("$I_LOVE_U", stored.Password))
}
