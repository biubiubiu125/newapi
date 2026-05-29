package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupReferralControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.ReferralEnabled = true
	common.ReferralCookieTTLDays = 30
	common.ReferralRedirectPath = "/sign-up"
	common.CryptoSecret = "test-secret"
	common.SessionSecret = "test-session-secret"
	common.SetTheme("default")

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.UserLoginIdentifier{},
		&model.Log{},
		&model.ReferralAffiliate{},
		&model.ReferralBinding{},
		&model.ReferralClick{},
		&model.ReferralAsset{},
	))
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestReferralLandingRedirectsToDefaultSignUp(t *testing.T) {
	db := setupReferralControllerTestDB(t)
	require.NoError(t, db.Create(&model.ReferralAffiliate{
		UserId:             1,
		InviteCode:         "TESTCODE",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
		CreatedAt:          time.Now().Unix(),
		UpdatedAt:          time.Now().Unix(),
	}).Error)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "code", Value: "TESTCODE"}}
	c.Request = httptest.NewRequest(http.MethodGet, "/api/r/TESTCODE", nil)

	ReferralLanding(c)
	require.Equal(t, http.StatusFound, w.Code)
	require.Contains(t, w.Header().Get("Location"), "/sign-up")
	require.Contains(t, w.Header().Get("Location"), "aff=TESTCODE")
	require.NotEmpty(t, w.Header().Values("Set-Cookie"))
}

func TestReferralLandingRejectsProtocolRelativeRedirect(t *testing.T) {
	setupReferralControllerTestDB(t)
	require.Equal(t, "", sanitizeReferralRedirectPath("//evil.com"))
	require.Equal(t, "", sanitizeReferralRedirectPath("https://evil.com"))
	require.Equal(t, "", sanitizeReferralRedirectPath("\\\\evil.com"))
}

func TestReferralLandingAlwaysUsesSignUpPath(t *testing.T) {
	db := setupReferralControllerTestDB(t)
	common.SetTheme("classic")
	common.ReferralRedirectPath = "/register"
	require.NoError(t, db.Create(&model.ReferralAffiliate{
		UserId:             1,
		InviteCode:         "CLASSIC01",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
		CreatedAt:          time.Now().Unix(),
		UpdatedAt:          time.Now().Unix(),
	}).Error)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "code", Value: "CLASSIC01"}}
	c.Request = httptest.NewRequest(http.MethodGet, "/api/r/CLASSIC01", nil)

	ReferralLanding(c)
	require.Equal(t, http.StatusFound, w.Code)
	require.Contains(t, w.Header().Get("Location"), "/sign-up")
	require.Contains(t, w.Header().Get("Location"), "aff=CLASSIC01")
}

func TestReferralRegisterRedirectUsesFixedSignUpPath(t *testing.T) {
	common.SetTheme("default")
	require.Contains(t, referralRegisterRedirect("/pricing", "TEST123"), "/sign-up?aff=TEST123")
	require.Contains(t, referralRegisterRedirect("", "TEST123"), "/sign-up?aff=TEST123")
}

func TestReferralLandingIgnoresRedirectQuery(t *testing.T) {
	db := setupReferralControllerTestDB(t)
	require.NoError(t, db.Create(&model.ReferralAffiliate{
		UserId:             1,
		InviteCode:         "REDIR001",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
		CreatedAt:          time.Now().Unix(),
		UpdatedAt:          time.Now().Unix(),
	}).Error)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "code", Value: "REDIR001"}}
	c.Request = httptest.NewRequest(http.MethodGet, "/api/r/REDIR001?redirect=/pricing", nil)

	ReferralLanding(c)
	require.Equal(t, http.StatusFound, w.Code)
	require.Contains(t, w.Header().Get("Location"), "/sign-up")
	require.Contains(t, w.Header().Get("Location"), "aff=REDIR001")
}

func TestReferralLandingIgnoresUnsafeRedirectQuery(t *testing.T) {
	db := setupReferralControllerTestDB(t)
	require.NoError(t, db.Create(&model.ReferralAffiliate{
		UserId:             1,
		InviteCode:         "REDIR002",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
		CreatedAt:          time.Now().Unix(),
		UpdatedAt:          time.Now().Unix(),
	}).Error)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "code", Value: "REDIR002"}}
	c.Request = httptest.NewRequest(http.MethodGet, "/api/r/REDIR002?redirect=//evil.com", nil)

	ReferralLanding(c)
	require.Equal(t, http.StatusFound, w.Code)
	require.Contains(t, w.Header().Get("Location"), "/sign-up")
	require.Contains(t, w.Header().Get("Location"), "aff=REDIR002")
}

func TestReferralLandingDisabledUsesThemeRegisterPath(t *testing.T) {
	setupReferralControllerTestDB(t)
	common.ReferralEnabled = false
	common.SetTheme("default")

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "code", Value: "ANYCODE"}}
	c.Request = httptest.NewRequest(http.MethodGet, "/api/r/ANYCODE", nil)

	ReferralLanding(c)
	require.Equal(t, http.StatusFound, w.Code)
	require.Equal(t, "/sign-up", w.Header().Get("Location"))
}

func TestResolveReferralAssetUploadPurpose(t *testing.T) {
	purpose, createdBy, err := resolveReferralAssetUploadPurpose(service.ReferralUserUploadPath, "")
	require.NoError(t, err)
	require.Equal(t, model.ReferralAssetPurposeWithdrawalQR, purpose)
	require.Equal(t, "user", createdBy)

	purpose, createdBy, err = resolveReferralAssetUploadPurpose(service.ReferralAdminUploadPath, model.ReferralAssetPurposePaymentProof)
	require.NoError(t, err)
	require.Equal(t, model.ReferralAssetPurposePaymentProof, purpose)
	require.Equal(t, "admin", createdBy)

	_, _, err = resolveReferralAssetUploadPurpose(service.ReferralUserUploadPath, model.ReferralAssetPurposePaymentProof)
	require.Error(t, err)

	_, _, err = resolveReferralAssetUploadPurpose("/api/user/referral/unknown", "")
	require.Error(t, err)
}

func TestUploadReferralAssetAcceptsWithdrawalQRMultipart(t *testing.T) {
	db := setupReferralControllerTestDB(t)
	require.NoError(t, db.Create(&model.User{
		Id:          1,
		Username:    "asset-user",
		Password:    "12345678",
		DisplayName: "asset-user",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}).Error)

	wd := t.TempDir()
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(wd))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(oldWD))
	})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("purpose", model.ReferralAssetPurposeWithdrawalQR))
	part, err := writer.CreateFormFile("file", "qr.png")
	require.NoError(t, err)
	_, err = part.Write([]byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
		0x42, 0x60, 0x82,
	})
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, router := gin.CreateTestContext(w)
	router.POST(service.ReferralUserUploadPath, func(c *gin.Context) {
		c.Set("id", 1)
		UploadReferralAsset(c)
	})
	req := httptest.NewRequest(http.MethodPost, service.ReferralUserUploadPath, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	c.Request = req

	router.HandleContext(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":true`)
	require.Contains(t, w.Body.String(), `/referral-assets/a/`)
	require.Contains(t, w.Body.String(), `expires=`)
	require.Contains(t, w.Body.String(), `sig=`)

	var count int64
	require.NoError(t, db.Model(&model.ReferralAsset{}).Where("owner_user_id = ? AND purpose = ?", 1, model.ReferralAssetPurposeWithdrawalQR).Count(&count).Error)
	require.Equal(t, int64(1), count)
	require.NotEmpty(t, mustGlob(t, filepath.Join(wd, "uploads", "referral-assets", "withdrawal-qr-*.png")))
}

func mustGlob(t *testing.T, pattern string) []string {
	t.Helper()
	matches, err := filepath.Glob(pattern)
	require.NoError(t, err)
	return matches
}

func TestReferralCookieValueRejectsUnsignedCookie(t *testing.T) {
	setupReferralControllerTestDB(t)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/api/user/register", nil)
	req.AddCookie(&http.Cookie{
		Name:  service.ReferralCookieName,
		Value: "UNSIGNEDCODE",
	})
	c.Request = req

	require.Equal(t, "", referralCookieValue(c))
}

func TestRegisterBindsReferralCodeFromRequestBody(t *testing.T) {
	db := setupReferralControllerTestDB(t)

	affiliateUser := &model.User{
		Username:    "aff-owner",
		Password:    "12345678",
		DisplayName: "aff-owner",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, db.Create(affiliateUser).Error)
	require.NoError(t, db.Create(&model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "BODYAFF1",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
		CreatedAt:          time.Now().Unix(),
		UpdatedAt:          time.Now().Unix(),
	}).Error)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, router := gin.CreateTestContext(w)
	store := cookie.NewStore([]byte(common.SessionSecret))
	router.Use(sessions.Sessions("session", store))
	router.POST("/api/user/register", Register)

	body := []byte(`{"username":"invitee-body","password":"12345678","email":"invitee-body@example.com","aff":"BODYAFF1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/user/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	router.HandleContext(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":true`)

	invitee := &model.User{}
	require.NoError(t, db.Where("username = ?", "invitee-body").First(invitee).Error)
	require.Equal(t, "invitee-body@example.com", invitee.Email)

	binding := &model.ReferralBinding{}
	require.NoError(t, db.Where("invitee_user_id = ?", invitee.Id).First(binding).Error)
	require.Equal(t, affiliateUser.Id, binding.InviterUserId)
	require.Equal(t, "BODYAFF1", binding.BindCode)
	require.Equal(t, "code", binding.BindSource)
}

func TestRegisterRejectsDuplicateEmailCaseInsensitive(t *testing.T) {
	db := setupReferralControllerTestDB(t)
	existing := &model.User{
		Username:    "existing-user",
		Password:    "12345678",
		DisplayName: "existing-user",
		Email:       "dupe@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, existing.Insert(0))

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	_, router := gin.CreateTestContext(w)
	store := cookie.NewStore([]byte(common.SessionSecret))
	router.Use(sessions.Sessions("session", store))
	router.POST("/api/user/register", Register)

	body := []byte(`{"username":"new-user","password":"12345678","email":"DUPE@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/user/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, false, response["success"])

	var count int64
	require.NoError(t, db.Model(&model.User{}).Where("email_canonical = ?", "dupe@example.com").Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestRegisterRejectsDuplicateUsernameWithoutServerError(t *testing.T) {
	db := setupReferralControllerTestDB(t)
	existing := &model.User{
		Username:    "same-user",
		Password:    "12345678",
		DisplayName: "same-user",
		Email:       "same-user@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, existing.Insert(0))

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	_, router := gin.CreateTestContext(w)
	store := cookie.NewStore([]byte(common.SessionSecret))
	router.Use(sessions.Sessions("session", store))
	router.POST("/api/user/register", Register)

	body := []byte(`{"username":"same-user","password":"12345678","email":"same-user-2@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/user/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, false, response["success"])

	var count int64
	require.NoError(t, db.Model(&model.User{}).Where("username = ?", "same-user").Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestRegisterRejectsEmailThatMatchesExistingUsername(t *testing.T) {
	db := setupReferralControllerTestDB(t)
	existing := &model.User{
		Username:    "owner@example.com",
		Password:    "12345678",
		DisplayName: "owner",
		Email:       "owner-real@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, existing.Insert(0))

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	_, router := gin.CreateTestContext(w)
	store := cookie.NewStore([]byte(common.SessionSecret))
	router.Use(sessions.Sessions("session", store))
	router.POST("/api/user/register", Register)

	body := []byte(`{"username":"new-user","password":"12345678","email":"owner@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/user/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, false, response["success"])

	var count int64
	require.NoError(t, db.Model(&model.User{}).Where("username = ?", "new-user").Count(&count).Error)
	require.Equal(t, int64(0), count)
}

func TestRegisterRejectsUsernameThatMatchesExistingEmail(t *testing.T) {
	db := setupReferralControllerTestDB(t)
	existing := &model.User{
		Username:    "email-owner",
		Password:    "12345678",
		DisplayName: "email-owner",
		Email:       "owner@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, existing.Insert(0))

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	_, router := gin.CreateTestContext(w)
	store := cookie.NewStore([]byte(common.SessionSecret))
	router.Use(sessions.Sessions("session", store))
	router.POST("/api/user/register", Register)

	body := []byte(`{"username":"owner@example.com","password":"12345678","email":"new-user@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/user/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, false, response["success"])

	var count int64
	require.NoError(t, db.Model(&model.User{}).Where("username = ?", "owner@example.com").Count(&count).Error)
	require.Equal(t, int64(0), count)
}

func TestRegisterRejectsSameUsernameAndEmail(t *testing.T) {
	db := setupReferralControllerTestDB(t)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	_, router := gin.CreateTestContext(w)
	store := cookie.NewStore([]byte(common.SessionSecret))
	router.Use(sessions.Sessions("session", store))
	router.POST("/api/user/register", Register)

	body := []byte(`{"username":"same@example.com","password":"12345678","email":"same@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/user/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, false, response["success"])

	var count int64
	require.NoError(t, db.Model(&model.User{}).Where("username = ?", "same@example.com").Count(&count).Error)
	require.Equal(t, int64(0), count)
}

func TestRegisterRejectsInvalidEmailWithoutCreatingUser(t *testing.T) {
	db := setupReferralControllerTestDB(t)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	_, router := gin.CreateTestContext(w)
	store := cookie.NewStore([]byte(common.SessionSecret))
	router.Use(sessions.Sessions("session", store))
	router.POST("/api/user/register", Register)

	body := []byte(`{"username":"bad-email","password":"12345678","email":"bad-email"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/user/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, false, response["success"])

	var count int64
	require.NoError(t, db.Model(&model.User{}).Where("username = ?", "bad-email").Count(&count).Error)
	require.Equal(t, int64(0), count)
}

func TestEmailBindRejectsEmailOwnedByAnotherUser(t *testing.T) {
	db := setupReferralControllerTestDB(t)
	first := &model.User{
		Username:    "email-owner",
		Password:    "12345678",
		DisplayName: "email-owner",
		Email:       "taken@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	second := &model.User{
		Username:    "email-binder",
		Password:    "12345678",
		DisplayName: "email-binder",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, first.Insert(0))
	require.NoError(t, second.Insert(0))
	common.RegisterVerificationCodeWithKey("taken@example.com", "123456", common.EmailVerificationPurpose)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	_, router := gin.CreateTestContext(w)
	store := cookie.NewStore([]byte(common.SessionSecret))
	router.Use(sessions.Sessions("session", store))
	router.Use(func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", second.Id)
		require.NoError(t, session.Save())
		c.Next()
	})
	router.POST("/api/user/email", EmailBind)

	body := []byte(`{"email":"TAKEN@example.com","code":"123456"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/user/email", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, false, response["success"])

	var reloaded model.User
	require.NoError(t, db.Where("id = ?", second.Id).First(&reloaded).Error)
	require.Empty(t, reloaded.Email)
}

func TestEmailBindRejectsMissingLoginSessionWithoutPanic(t *testing.T) {
	setupReferralControllerTestDB(t)
	common.RegisterVerificationCodeWithKey("bind-no-session@example.com", "123456", common.EmailVerificationPurpose)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	_, router := gin.CreateTestContext(w)
	store := cookie.NewStore([]byte(common.SessionSecret))
	router.Use(sessions.Sessions("session", store))
	router.POST("/api/user/email", EmailBind)

	body := []byte(`{"email":"bind-no-session@example.com","code":"123456"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/user/email", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, false, response["success"])
}

func TestEmailBindRejectsEmailMatchingAnotherUsername(t *testing.T) {
	db := setupReferralControllerTestDB(t)
	first := &model.User{
		Username:    "taken@example.com",
		Password:    "12345678",
		DisplayName: "taken",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	second := &model.User{
		Username:    "email-binder-cross",
		Password:    "12345678",
		DisplayName: "email-binder-cross",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, first.Insert(0))
	require.NoError(t, second.Insert(0))
	common.RegisterVerificationCodeWithKey("taken@example.com", "123456", common.EmailVerificationPurpose)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	_, router := gin.CreateTestContext(w)
	store := cookie.NewStore([]byte(common.SessionSecret))
	router.Use(sessions.Sessions("session", store))
	router.Use(func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", second.Id)
		require.NoError(t, session.Save())
		c.Next()
	})
	router.POST("/api/user/email", EmailBind)

	body := []byte(`{"email":"TAKEN@example.com","code":"123456"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/user/email", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, false, response["success"])

	var reloaded model.User
	require.NoError(t, db.Where("id = ?", second.Id).First(&reloaded).Error)
	require.Empty(t, reloaded.Email)
}

func TestEmailBindRejectsEmailMatchingOwnUsername(t *testing.T) {
	db := setupReferralControllerTestDB(t)
	user := &model.User{
		Username:    "own@example.com",
		Password:    "12345678",
		DisplayName: "own",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, user.Insert(0))
	common.RegisterVerificationCodeWithKey("own@example.com", "123456", common.EmailVerificationPurpose)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	_, router := gin.CreateTestContext(w)
	store := cookie.NewStore([]byte(common.SessionSecret))
	router.Use(sessions.Sessions("session", store))
	router.Use(func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", user.Id)
		require.NoError(t, session.Save())
		c.Next()
	})
	router.POST("/api/user/email", EmailBind)

	body := []byte(`{"email":"OWN@example.com","code":"123456"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/user/email", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, false, response["success"])

	var reloaded model.User
	require.NoError(t, db.Where("id = ?", user.Id).First(&reloaded).Error)
	require.Empty(t, reloaded.Email)
}

func TestAdminHardDeleteSoftDeletedUserReleasesLoginIdentifiers(t *testing.T) {
	setupReferralControllerTestDB(t)
	user := &model.User{
		Username:    "deleted@example.com",
		Password:    "12345678",
		DisplayName: "deleted",
		Email:       "deleted-real@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, user.Insert(0))
	require.NoError(t, model.DeleteUserById(user.Id))

	exists, err := model.IsLoginIdentifierTakenByOther("", "deleted@example.com", 0)
	require.NoError(t, err)
	require.True(t, exists)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: strconv.Itoa(user.Id)}}
	c.Set("role", common.RoleAdminUser)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/user/"+strconv.Itoa(user.Id), nil)

	DeleteUser(c)

	require.Equal(t, http.StatusOK, w.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, true, response["success"])

	exists, err = model.IsLoginIdentifierTakenByOther("", "deleted@example.com", 0)
	require.NoError(t, err)
	require.False(t, exists)
	reuse := &model.User{
		Username:    "reuse-after-hard-delete",
		Password:    "12345678",
		DisplayName: "reuse",
		Email:       "deleted@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, reuse.Insert(0))
}

func TestAdminCreateUserWithEmailAllowsEmailLogin(t *testing.T) {
	setupReferralControllerTestDB(t)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("role", common.RoleAdminUser)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/", bytes.NewReader([]byte(`{
		"username":"admin-created",
		"display_name":"Admin Created",
		"password":"12345678",
		"email":"Admin-Created@Example.com",
		"role":1
	}`)))

	CreateUser(c)

	require.Equal(t, http.StatusOK, w.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, true, response["success"])

	login := &model.User{
		Username: "admin-created@example.com",
		Password: "12345678",
	}
	require.NoError(t, login.ValidateAndFill())
	require.Equal(t, "admin-created", login.Username)
}

func TestAdminUpdateUserEmailAllowsEmailLogin(t *testing.T) {
	setupReferralControllerTestDB(t)
	user := &model.User{
		Username:    "admin-edit-email",
		Password:    "12345678",
		DisplayName: "admin-edit-email",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, user.Insert(0))

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("role", common.RoleAdminUser)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/user/", bytes.NewReader([]byte(fmt.Sprintf(`{
		"id":%d,
		"username":"admin-edit-email",
		"display_name":"admin-edit-email",
		"password":"",
		"email":"Edited@Example.com",
		"role":1,
		"group":"default"
	}`, user.Id))))

	UpdateUser(c)

	require.Equal(t, http.StatusOK, w.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, true, response["success"])

	login := &model.User{
		Username: "edited@example.com",
		Password: "12345678",
	}
	require.NoError(t, login.ValidateAndFill())
	require.Equal(t, user.Id, login.Id)
}

func TestAdminUpdateUserCanClearEmail(t *testing.T) {
	db := setupReferralControllerTestDB(t)
	user := &model.User{
		Username:    "admin-clear-email",
		Password:    "12345678",
		DisplayName: "admin-clear-email",
		Email:       "clear-me@example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	require.NoError(t, user.Insert(0))

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("role", common.RoleAdminUser)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/user/", bytes.NewReader([]byte(fmt.Sprintf(`{
		"id":%d,
		"username":"admin-clear-email",
		"display_name":"admin-clear-email",
		"password":"",
		"email":"",
		"role":1,
		"group":"default"
	}`, user.Id))))

	UpdateUser(c)

	require.Equal(t, http.StatusOK, w.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, true, response["success"])

	var reloaded model.User
	require.NoError(t, db.First(&reloaded, user.Id).Error)
	require.Empty(t, reloaded.Email)
	require.Nil(t, reloaded.EmailCanonical)

	login := &model.User{
		Username: "clear-me@example.com",
		Password: "12345678",
	}
	require.ErrorIs(t, login.ValidateAndFill(), model.ErrUserPasswordIncorrect)
}

func TestAdminCreateUserRejectsEmailMatchingExistingUsername(t *testing.T) {
	setupReferralControllerTestDB(t)
	existing := &model.User{
		Username:    "taken-admin@example.com",
		Password:    "12345678",
		DisplayName: "taken",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, existing.Insert(0))

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("role", common.RoleAdminUser)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/", bytes.NewReader([]byte(`{
		"username":"new-admin-user",
		"display_name":"New Admin User",
		"password":"12345678",
		"email":"taken-admin@example.com",
		"role":1
	}`)))

	CreateUser(c)

	require.Equal(t, http.StatusOK, w.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, false, response["success"])
}
