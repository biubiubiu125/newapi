package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAdminUserResponsesOmitPasswordAndNotificationSecrets(t *testing.T) {
	db := setupManageUserTestDB(t)
	gin.SetMode(gin.TestMode)

	admin := model.User{
		Username: "admin-secret-viewer",
		Password: "admin-password-hash",
		Role:     common.RoleAdminUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	target := model.User{
		Username: "secret-target",
		Password: "target-password-hash",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	target.SetSetting(dto.UserSetting{
		Language:      "zhCN",
		WebhookUrl:    "https://hook.example/secret",
		WebhookSecret: "whsec-secret",
		BarkUrl:       "https://api.day.app/bark-secret",
		GotifyUrl:     "https://gotify.example",
		GotifyToken:   "gotify-secret",
	})
	require.NoError(t, db.Create(&admin).Error)
	require.NoError(t, db.Create(&target).Error)

	detailRecorder := httptest.NewRecorder()
	detailCtx, _ := gin.CreateTestContext(detailRecorder)
	detailCtx.Request = httptest.NewRequest(http.MethodGet, "/api/user/1", nil)
	detailCtx.Params = gin.Params{{Key: "id", Value: itoa(target.Id)}}
	detailCtx.Set("id", admin.Id)
	detailCtx.Set("role", admin.Role)
	GetUser(detailCtx)

	require.Equal(t, http.StatusOK, detailRecorder.Code)
	var detail struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(detailRecorder.Body.Bytes(), &detail))
	require.True(t, detail.Success)
	require.Empty(t, detail.Data["password"])
	assertNotificationSecretsHidden(t, detail.Data["setting"])

	listRecorder := httptest.NewRecorder()
	listCtx, _ := gin.CreateTestContext(listRecorder)
	listCtx.Request = httptest.NewRequest(http.MethodGet, "/api/user/?p=1&page_size=10", nil)
	GetAllUsers(listCtx)
	require.Equal(t, http.StatusOK, listRecorder.Code)
	var list struct {
		Success bool `json:"success"`
		Data    struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(listRecorder.Body.Bytes(), &list))
	require.True(t, list.Success)
	require.NotEmpty(t, list.Data.Items)
	var sawTarget bool
	for _, item := range list.Data.Items {
		require.Empty(t, item["password"])
		setting, ok := item["setting"].(string)
		require.True(t, ok)
		require.NotContains(t, setting, "whsec-secret")
		require.NotContains(t, setting, "gotify-secret")
		if item["username"] == "secret-target" {
			sawTarget = true
			assertNotificationSecretsHidden(t, setting)
		}
	}
	require.True(t, sawTarget)
}

func assertNotificationSecretsHidden(t *testing.T, raw any) {
	t.Helper()
	setting, ok := raw.(string)
	require.True(t, ok)
	require.NotContains(t, setting, "https://hook.example/secret")
	require.NotContains(t, setting, "whsec-secret")
	require.NotContains(t, setting, "bark-secret")
	require.NotContains(t, setting, "gotify-secret")
	require.NotContains(t, setting, "https://gotify.example")
	require.Contains(t, setting, "zhCN")
}

func itoa(value int) string {
	return jsonNumber(value)
}

func jsonNumber(value int) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}
