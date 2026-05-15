package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
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

func TestReferralLandingUsesClassicRegisterPath(t *testing.T) {
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
	require.Contains(t, w.Header().Get("Location"), "/register")
	require.Contains(t, w.Header().Get("Location"), "aff=CLASSIC01")
}

func TestReferralRegisterRedirectAcceptsWhitelistedPath(t *testing.T) {
	common.SetTheme("default")
	require.Contains(t, referralRegisterRedirect("/pricing", "TEST123"), "/pricing?aff=TEST123")
	require.Contains(t, referralRegisterRedirect("", "TEST123"), "/sign-up?aff=TEST123")
}

func TestReferralLandingHonorsSafeRedirectQuery(t *testing.T) {
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
	require.Contains(t, w.Header().Get("Location"), "/pricing")
	require.Contains(t, w.Header().Get("Location"), "aff=REDIR001")
}

func TestReferralLandingRejectsUnsafeRedirectQuery(t *testing.T) {
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
	require.Contains(t, w.Header().Get("Location"), "referral_error=invalid")
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
