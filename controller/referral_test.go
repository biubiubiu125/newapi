package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
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
