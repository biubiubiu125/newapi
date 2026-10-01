package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupCustomOAuthControllerTestDB(t *testing.T) {
	t.Helper()

	previousDB := model.DB
	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.CustomOAuthProvider{}))

	t.Cleanup(func() {
		oauth.UnregisterCustomProvider("form-loop-provider")
		model.DB = previousDB
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
}

func TestUpdateCustomOAuthProviderClearsOptionalFieldsWithoutDroppingSecret(t *testing.T) {
	setupCustomOAuthControllerTestDB(t)
	gin.SetMode(gin.TestMode)

	require.NoError(t, model.DB.Create(&model.CustomOAuthProvider{
		Id:                    71,
		Name:                  "Acme",
		Slug:                  "form-loop-provider",
		Icon:                  "github",
		Enabled:               true,
		ClientId:              "client-id",
		ClientSecret:          "keep-me",
		AuthorizationEndpoint: "https://idp.example/authorize",
		TokenEndpoint:         "https://idp.example/token",
		UserInfoEndpoint:      "https://idp.example/userinfo",
		Scopes:                "repo",
		UserIdField:           "id",
		UsernameField:         "login",
		DisplayNameField:      "full_name",
		EmailField:            "mail",
		WellKnown:             "https://idp.example/.well-known",
		AuthStyle:             1,
		AccessPolicy:          `{"logic":"and","conditions":[{"field":"active","op":"eq","value":true}]}`,
		AccessDeniedMessage:   "denied",
	}).Error)

	enabled := false
	authStyle := 0
	empty := ""
	body, err := json.Marshal(UpdateCustomOAuthProviderRequest{
		Name:                  "Acme Updated",
		Slug:                  "form-loop-provider",
		Icon:                  &empty,
		Enabled:               &enabled,
		ClientId:              "client-id",
		ClientSecret:          "",
		AuthorizationEndpoint: "https://idp.example/authorize",
		TokenEndpoint:         "https://idp.example/token",
		UserInfoEndpoint:      "https://idp.example/userinfo",
		Scopes:                "",
		UserIdField:           "id",
		UsernameField:         "",
		DisplayNameField:      "",
		EmailField:            "",
		WellKnown:             &empty,
		AuthStyle:             &authStyle,
		AccessPolicy:          &empty,
		AccessDeniedMessage:   &empty,
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "71"}}
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/custom-oauth-provider/71", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	UpdateCustomOAuthProvider(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool                        `json:"success"`
		Data    CustomOAuthProviderResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Equal(t, "openid profile email", response.Data.Scopes)
	require.Equal(t, "preferred_username", response.Data.UsernameField)
	require.Equal(t, "name", response.Data.DisplayNameField)
	require.Equal(t, "email", response.Data.EmailField)
	require.Empty(t, response.Data.Icon)
	require.Empty(t, response.Data.WellKnown)
	require.Empty(t, response.Data.AccessPolicy)
	require.False(t, response.Data.Enabled)

	var saved model.CustomOAuthProvider
	require.NoError(t, model.DB.First(&saved, 71).Error)
	require.Equal(t, "keep-me", saved.ClientSecret)
	require.Equal(t, "openid profile email", saved.Scopes)
	require.Equal(t, "preferred_username", saved.UsernameField)
	require.Equal(t, "name", saved.DisplayNameField)
	require.Equal(t, "email", saved.EmailField)
	require.Equal(t, "Acme Updated", saved.Name)

	enabled = true
	body, err = json.Marshal(UpdateCustomOAuthProviderRequest{
		Name:                  "Acme Updated",
		Slug:                  "form-loop-provider",
		Enabled:               &enabled,
		ClientId:              "client-id",
		ClientSecret:          "",
		AuthorizationEndpoint: "https://idp.example/authorize",
		TokenEndpoint:         "https://idp.example/token",
		UserInfoEndpoint:      "https://idp.example/userinfo",
		Scopes:                "repo",
		UserIdField:           "id",
		UsernameField:         "login",
		DisplayNameField:      "full_name",
		EmailField:            "mail",
	})
	require.NoError(t, err)

	recorder = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "71"}}
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/custom-oauth-provider/71", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	UpdateCustomOAuthProvider(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NoError(t, model.DB.First(&saved, 71).Error)
	require.Equal(t, "repo", saved.Scopes)
	require.Equal(t, "login", saved.UsernameField)
	require.Equal(t, "full_name", saved.DisplayNameField)
	require.Equal(t, "mail", saved.EmailField)
	require.Equal(t, "keep-me", saved.ClientSecret)
}
