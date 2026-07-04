package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUpdateUserStoresAdminPermissionOverrides(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}))
	require.NoError(t, authz.Init(db))
	t.Cleanup(func() {
		require.NoError(t, authz.Init(db))
	})

	user := &model.User{
		Username:    "managed-admin",
		Password:    "12345678",
		DisplayName: "Managed Admin",
		Role:        common.RoleAdminUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	require.NoError(t, user.Insert(0))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Set("role", common.RoleRootUser)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/user/", strings.NewReader(`{
		"id": `+strconv.Itoa(user.Id)+`,
		"username": "managed-admin",
		"display_name": "Managed Admin",
		"email": "",
		"group": "default",
		"admin_permissions": {
			"channel": {
				"read": true,
				"operate": true,
				"write": true,
				"sensitive_write": true,
				"secret_view": false
			}
		}
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	UpdateUser(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)
	require.True(t, authz.Can(user.Id, common.RoleAdminUser, authz.ChannelSensitiveWrite))
}
