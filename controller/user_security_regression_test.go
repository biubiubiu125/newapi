package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCreateUserRejectsRoleSeven(t *testing.T) {
	require.NoError(t, i18n.Init())
	setupModelListControllerTestDB(t)
	body := `{"username":"odd-role","password":"12345678","display_name":"odd-role","role":7}`
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Set("role", common.RoleRootUser)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	CreateUser(ctx)

	require.Contains(t, recorder.Body.String(), `"success":false`)
	var count int64
	require.NoError(t, model.DB.Model(&model.User{}).Where("username = ?", "odd-role").Count(&count).Error)
	require.Zero(t, count)
}

func TestCreateAdminRequiresStepUpProof(t *testing.T) {
	_, identity := setupSecurityEnrollmentTest(t)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", identity.UserID).Update("role", common.RoleRootUser).Error)
	body := `{"username":"new-admin","password":"12345678","display_name":"new-admin","role":10}`
	response := securityEnrollmentRequest(http.MethodPost, "/api/user/", body, "", identity, func(c *gin.Context) {
		c.Set("role", common.RoleRootUser)
		CreateUser(c)
	})

	require.Equal(t, http.StatusForbidden, response.Code)
	require.Contains(t, response.Body.String(), "SECURITY_PROOF_REQUIRED")
	var count int64
	require.NoError(t, model.DB.Model(&model.User{}).Where("username = ?", "new-admin").Count(&count).Error)
	require.Zero(t, count)
}

func TestEmailBindWithoutFlowTokenDoesNotChangeEmail(t *testing.T) {
	user, identity := setupSecurityEnrollmentTest(t)
	require.NoError(t, common.RegisterVerificationCodeWithKey("legacy-bind@example.com", "123456", common.EmailVerificationPurpose))
	response := securityEnrollmentRequest(http.MethodPost, "/api/user/email", `{"email":"legacy-bind@example.com","code":"123456"}`, "", identity, EmailBind)

	require.Contains(t, response.Body.String(), `"success":false`)
	var reloaded model.User
	require.NoError(t, model.DB.First(&reloaded, user.Id).Error)
	require.Empty(t, reloaded.Email)
	require.True(t, common.VerifyCodeWithKey("legacy-bind@example.com", "123456", common.EmailVerificationPurpose))
}

func TestLegacyOAuthBindDoesNotBind(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/oauth/github?code=abc", nil)

	handleLegacyOAuthBind(ctx, "github", nil, 1)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), "OAUTH_LEGACY_BIND_REMOVED")
}

func TestRegisterConsumesEmailCodeOnlyAfterSuccess(t *testing.T) {
	require.NoError(t, i18n.Init())
	db := setupReferralControllerTestDB(t)
	previous := common.EmailVerificationEnabled
	common.EmailVerificationEnabled = true
	t.Cleanup(func() { common.EmailVerificationEnabled = previous })
	require.NoError(t, common.RegisterVerificationCodeWithKey("taken-name@example.com", "123456", common.EmailVerificationPurpose))
	require.NoError(t, (&model.User{
		Username:    "taken-name",
		Password:    "12345678",
		DisplayName: "taken-name",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}).Insert(0))

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte(common.SessionSecret))))
	router.POST("/api/user/register", Register)
	failed := httptest.NewRecorder()
	failedRequest := httptest.NewRequest(http.MethodPost, "/api/user/register", strings.NewReader(`{"username":"taken-name","password":"12345678","email":"taken-name@example.com","verification_code":"123456"}`))
	failedRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(failed, failedRequest)
	require.Contains(t, failed.Body.String(), `"success":false`)
	require.True(t, common.VerifyCodeWithKey("taken-name@example.com", "123456", common.EmailVerificationPurpose))

	created := httptest.NewRecorder()
	createdRequest := httptest.NewRequest(http.MethodPost, "/api/user/register", strings.NewReader(`{"username":"fresh-name","password":"12345678","email":"taken-name@example.com","verification_code":"123456"}`))
	createdRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(created, createdRequest)
	require.Contains(t, created.Body.String(), `"success":true`)
	require.False(t, common.VerifyCodeWithKey("taken-name@example.com", "123456", common.EmailVerificationPurpose))
	var count int64
	require.NoError(t, db.Model(&model.User{}).Where("username = ?", "fresh-name").Count(&count).Error)
	require.EqualValues(t, 1, count)
}
