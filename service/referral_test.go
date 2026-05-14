package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupReferralServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousCryptoSecret := common.CryptoSecret
	previousSessionSecret := common.SessionSecret
	previousReferralEnabled := common.ReferralEnabled
	previousCookieTTLDays := common.ReferralCookieTTLDays
	previousDefaultRate := common.ReferralDefaultRate
	previousSettleFreezeDays := common.ReferralSettleFreezeDays
	previousRedirectPath := common.ReferralRedirectPath

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.ReferralEnabled = true
	common.ReferralCookieTTLDays = 30
	common.ReferralDefaultRate = 20
	common.ReferralSettleFreezeDays = 7
	common.CryptoSecret = "test-secret"
	common.SessionSecret = "test-session-secret"

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.TopUp{},
		&model.ReferralAffiliate{},
		&model.ReferralBinding{},
		&model.ReferralClick{},
		&model.ReferralCommissionAccount{},
		&model.ReferralCommission{},
		&model.ReferralCommissionLedger{},
		&model.ReferralWithdrawal{},
		&model.ReferralWithdrawalItem{},
		&model.ReferralSettlementBatch{},
		&model.ReferralCommissionJob{},
		&model.ReferralAdminAuditLog{},
		&model.ReferralAsset{},
	))

	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.CryptoSecret = previousCryptoSecret
		common.SessionSecret = previousSessionSecret
		common.ReferralEnabled = previousReferralEnabled
		common.ReferralCookieTTLDays = previousCookieTTLDays
		common.ReferralDefaultRate = previousDefaultRate
		common.ReferralSettleFreezeDays = previousSettleFreezeDays
		common.ReferralRedirectPath = previousRedirectPath
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func TestReferralLandingRejectsUnsafeRedirect(t *testing.T) {
	setupReferralServiceTestDB(t)
	service := NewReferralService()

	common.ReferralRedirectPath = "//evil.com"
	_, err := service.HandleLanding("missing", model.ReferralClick{})
	require.Error(t, err)

	require.Equal(t, "", sanitizeReferralRedirectPath("//evil.com"))
	require.Equal(t, "", sanitizeReferralRedirectPath("https://evil.com"))
	require.Equal(t, "", sanitizeReferralRedirectPath("\\\\evil.com"))
	require.Equal(t, "/sign-up", sanitizeReferralRedirectPath("/sign-up"))
}

func TestReferralAssetSignatureRequiresSecretAndUsesConstantTimeVerify(t *testing.T) {
	setupReferralServiceTestDB(t)
	service := NewReferralService()

	url := service.SignAssetURL("/referral-assets/test.png")
	require.Contains(t, url, "sig=")
	require.True(t, service.VerifyAssetURL("/referral-assets/test.png", fmt.Sprint(time.Now().Add(1*time.Hour).Unix()), common.HmacSha256(fmt.Sprintf("%s|%d", "/referral-assets/test.png", time.Now().Add(1*time.Hour).Unix()), service.cookieSecret())))

	common.CryptoSecret = ""
	common.SessionSecret = ""
	require.Empty(t, service.SignAssetURL("/referral-assets/test.png"))
	require.False(t, service.VerifyAssetURL("/referral-assets/test.png", "1", "abc"))
}

func TestProcessTopUpCommissionIsIdempotent(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	invitee := &model.User{Username: "invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	affiliateUser := &model.User{Username: "affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))

	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "CODE1234",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, db.Create(affiliate).Error)
	require.NoError(t, db.Create(&model.ReferralBinding{
		InviteeUserId: invitee.Id,
		InviterUserId: affiliateUser.Id,
		AffiliateId:   affiliate.Id,
		BindSource:    "code",
		BindCode:      affiliate.InviteCode,
		BoundAt:       time.Now().Unix(),
	}).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionAccount{
		AffiliateId: affiliate.Id,
		UserId:      affiliateUser.Id,
	}).Error)

	topup := &model.TopUp{
		UserId:              invitee.Id,
		Amount:              100,
		Money:               10,
		PaidAmount:          10,
		PaidCurrency:        "USD",
		TradeNo:             "topup-1",
		Status:              common.TopUpStatusSuccess,
		ReferralAffiliateId: affiliate.Id,
		ReferralRate:        20,
		ReferralBaseAmount:  10,
		PaymentMethod:       model.PaymentMethodStripe,
		PaymentProvider:     model.PaymentProviderStripe,
		CreateTime:          time.Now().Unix(),
	}
	require.NoError(t, db.Create(topup).Error)

	for i := 0; i < 10; i++ {
		require.NoError(t, service.ProcessTopUpCommission(topup.TradeNo))
	}

	var commissions int64
	require.NoError(t, db.Model(&model.ReferralCommission{}).Where("source_type = ? AND source_trade_no = ?", "topup", topup.TradeNo).Count(&commissions).Error)
	require.EqualValues(t, 1, commissions)

	var ledgers int64
	require.NoError(t, db.Model(&model.ReferralCommissionLedger{}).Where("external_ref_id = ?", "accrue:topup:topup-1").Count(&ledgers).Error)
	require.EqualValues(t, 1, ledgers)

	account := &model.ReferralCommissionAccount{}
	require.NoError(t, db.Where("affiliate_id = ?", affiliate.Id).First(account).Error)
	require.Equal(t, 2.0, account.PendingAmount)
}

func TestBindInviteeRejectsSelfInvite(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	user := &model.User{Username: "self", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, user.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             user.Id,
		InviteCode:         "SELF1234",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, db.Create(affiliate).Error)

	err := service.BindInviteeByCode(user.Id, affiliate.InviteCode, "code")
	require.Error(t, err)
}

func TestBindInviteeRejectsCycle(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	userA := &model.User{Username: "a", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	userB := &model.User{Username: "b", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, userA.Insert(0))
	require.NoError(t, userB.Insert(0))

	affA := &model.ReferralAffiliate{UserId: userA.Id, InviteCode: "AFFA1234", Status: model.ReferralAffiliateStatusApproved, AcquisitionEnabled: true, SettlementEnabled: true, WithdrawalEnabled: true}
	affB := &model.ReferralAffiliate{UserId: userB.Id, InviteCode: "AFFB1234", Status: model.ReferralAffiliateStatusApproved, AcquisitionEnabled: true, SettlementEnabled: true, WithdrawalEnabled: true}
	require.NoError(t, db.Create(affA).Error)
	require.NoError(t, db.Create(affB).Error)
	require.NoError(t, db.Create(&model.ReferralBinding{
		InviteeUserId: userA.Id,
		InviterUserId: userB.Id,
		AffiliateId:   affB.Id,
		BindSource:    "code",
		BindCode:      affB.InviteCode,
		BoundAt:       time.Now().Unix(),
	}).Error)

	err := service.BindInviteeByCode(userB.Id, affA.InviteCode, "code")
	require.Error(t, err)
}

func TestCreateWithdrawalValidatesAssetOwnershipAndPurpose(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	affiliateUser := &model.User{Username: "withdraw-user", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "WD123456",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, db.Create(affiliate).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionAccount{
		AffiliateId:     affiliate.Id,
		UserId:          affiliateUser.Id,
		AvailableAmount: 100,
	}).Error)
	require.NoError(t, db.Create(&model.ReferralAsset{
		OwnerUserId: affiliateUser.Id,
		Purpose:     model.ReferralAssetPurposePaymentProof,
		StoragePath: "/referral-assets/proof.png",
		ContentType: "image/png",
		Size:        123,
		CreatedBy:   "admin",
		CreatedAt:   time.Now().Unix(),
	}).Error)

	_, err := service.CreateWithdrawal(ReferralWithdrawalCreateInput{
		UserId:         affiliateUser.Id,
		Amount:         10,
		AccountType:    "alipay",
		AccountName:    "tester",
		AccountNo:      "abc",
		QRImageURL:     "/referral-assets/proof.png",
		IdempotencyKey: "wd-key-1",
	})
	require.Error(t, err)
}
