package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupReferralServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousOptionMap := common.OptionMap
	previousCryptoSecret := common.CryptoSecret
	previousSessionSecret := common.SessionSecret
	previousReferralEnabled := common.ReferralEnabled
	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL
	previousRedisEnabled := common.RedisEnabled
	previousCookieTTLDays := common.ReferralCookieTTLDays
	previousDefaultRate := common.ReferralDefaultRate
	previousSettleFreezeDays := common.ReferralSettleFreezeDays
	previousMinWithdrawAmount := common.ReferralMinWithdrawAmount
	previousWithdrawFee := common.ReferralWithdrawFee
	previousRedirectPath := common.ReferralRedirectPath
	previousSettlementCurrency := common.ReferralSettlementCurrency
	previousSettlementFxRates := common.ReferralSettlementFxRates
	previousUSDExchangeRate := operation_setting.USDExchangeRate

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.ReferralEnabled = true
	common.ReferralCookieTTLDays = 30
	common.ReferralDefaultRate = 20
	common.ReferralSettleFreezeDays = 7
	common.ReferralMinWithdrawAmount = 0
	common.ReferralWithdrawFee = 0
	common.ReferralSettlementCurrency = "CNY"
	common.ReferralSettlementFxRates = map[string]float64{"CNY": 1}
	operation_setting.USDExchangeRate = 7.3
	common.CryptoSecret = "test-secret"
	common.SessionSecret = "test-session-secret"

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Option{},
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
	common.OptionMap = map[string]string{}

	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.OptionMap = previousOptionMap
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		common.RedisEnabled = previousRedisEnabled
		common.CryptoSecret = previousCryptoSecret
		common.SessionSecret = previousSessionSecret
		common.ReferralEnabled = previousReferralEnabled
		common.ReferralCookieTTLDays = previousCookieTTLDays
		common.ReferralDefaultRate = previousDefaultRate
		common.ReferralSettleFreezeDays = previousSettleFreezeDays
		common.ReferralMinWithdrawAmount = previousMinWithdrawAmount
		common.ReferralWithdrawFee = previousWithdrawFee
		common.ReferralRedirectPath = previousRedirectPath
		common.ReferralSettlementCurrency = previousSettlementCurrency
		common.ReferralSettlementFxRates = previousSettlementFxRates
		operation_setting.USDExchangeRate = previousUSDExchangeRate
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

	common.ReferralSettlementFxRates = map[string]float64{"CNY": 1, "USD": 7.2}
	operation_setting.USDExchangeRate = 2

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
	require.Equal(t, 4.0, account.PendingAmount)
	require.Equal(t, "CNY", account.SettlementCurrency)

	commission := &model.ReferralCommission{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "topup", topup.TradeNo).First(commission).Error)
	require.Equal(t, "USD", commission.PaidCurrency)
	require.Equal(t, 10.0, commission.PaidAmount)
	require.Equal(t, "CNY", commission.SettlementCurrency)
	require.Equal(t, 2.0, commission.SettlementFxRate)
	require.Equal(t, 20.0, commission.SettlementBaseAmount)
	require.Equal(t, 4.0, commission.CommissionAmount)
}

func TestProcessTopUpCommissionFailsWhenFxRateMissingAndRetriesAfterConfigured(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	invitee := &model.User{Username: "fx-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	affiliateUser := &model.User{Username: "fx-affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))

	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "FXRATE01",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, db.Create(affiliate).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionAccount{
		AffiliateId: affiliate.Id,
		UserId:      affiliateUser.Id,
	}).Error)

	topup := &model.TopUp{
		UserId:              invitee.Id,
		Amount:              100,
		Money:               10,
		PaidAmount:          10,
		PaidCurrency:        "EUR",
		TradeNo:             "topup-fx-missing",
		Status:              common.TopUpStatusSuccess,
		ReferralAffiliateId: affiliate.Id,
		ReferralRate:        10,
		ReferralBaseAmount:  10,
		PaymentMethod:       model.PaymentMethodStripe,
		PaymentProvider:     model.PaymentProviderStripe,
		CreateTime:          time.Now().Unix(),
	}
	require.NoError(t, db.Create(topup).Error)

	require.NoError(t, service.ProcessTopUpCommission(topup.TradeNo))

	var commissions int64
	require.NoError(t, db.Model(&model.ReferralCommission{}).Where("source_type = ? AND source_trade_no = ?", "topup", topup.TradeNo).Count(&commissions).Error)
	require.Zero(t, commissions)

	reloadedTopup := &model.TopUp{}
	require.NoError(t, db.Where("trade_no = ?", topup.TradeNo).First(reloadedTopup).Error)
	require.Equal(t, model.ReferralCommissionJobStatusFailed, reloadedTopup.ReferralCommissionStatus)
	require.Equal(t, model.ReferralCommissionErrorFxRateMissing, reloadedTopup.ReferralCommissionError)

	job := &model.ReferralCommissionJob{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "topup", topup.TradeNo).First(job).Error)
	require.Equal(t, model.ReferralCommissionJobStatusFailed, job.Status)
	require.Equal(t, model.ReferralCommissionErrorFxRateMissing, job.LastError)

	common.ReferralSettlementFxRates = map[string]float64{"CNY": 1, "EUR": 7}
	require.NoError(t, service.RetryCommissionJob("topup", topup.TradeNo))

	commission := &model.ReferralCommission{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "topup", topup.TradeNo).First(commission).Error)
	require.Equal(t, 70.0, commission.SettlementBaseAmount)
	require.Equal(t, 7.0, commission.SettlementFxRate)
	require.Equal(t, 7.0, commission.CommissionAmount)

	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "topup", topup.TradeNo).First(job).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSucceeded, job.Status)
}

func TestProcessTopUpCommissionFallsBackToUSDExchangeRate(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	common.ReferralSettlementFxRates = map[string]float64{"CNY": 1}
	operation_setting.USDExchangeRate = 2

	invitee := &model.User{Username: "usd-fallback-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	affiliateUser := &model.User{Username: "usd-fallback-affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))

	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "USDFX001",
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
		TradeNo:             "topup-usd-fallback",
		Status:              common.TopUpStatusSuccess,
		ReferralAffiliateId: affiliate.Id,
		ReferralRate:        10,
		ReferralBaseAmount:  10,
		PaymentMethod:       model.PaymentMethodStripe,
		PaymentProvider:     model.PaymentProviderStripe,
		CreateTime:          time.Now().Unix(),
	}
	require.NoError(t, db.Create(topup).Error)
	require.NoError(t, service.ProcessTopUpCommission(topup.TradeNo))

	commission := &model.ReferralCommission{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "topup", topup.TradeNo).First(commission).Error)
	require.Equal(t, "USD", commission.PaidCurrency)
	require.Equal(t, 10.0, commission.PaidAmount)
	require.Equal(t, "CNY", commission.SettlementCurrency)
	require.Equal(t, 2.0, commission.SettlementFxRate)
	require.Equal(t, 20.0, commission.SettlementBaseAmount)
	require.Equal(t, 2.0, commission.CommissionAmount)
}

func TestBuildOrderSnapshotMarksMissingFxRate(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	invitee := &model.User{Username: "snapshot-fx-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	affiliateUser := &model.User{Username: "snapshot-fx-affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "SNAPFX01",
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

	snapshot, err := service.BuildOrderSnapshot(invitee.Id, 10, "EUR")
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	require.Equal(t, model.ReferralCommissionJobStatusFailed, snapshot.Status)
	require.Equal(t, model.ReferralCommissionErrorFxRateMissing, snapshot.Error)
}

func TestBuildOrderSnapshotWithoutBindingReturnsSkippedReason(t *testing.T) {
	setupReferralServiceTestDB(t)
	service := NewReferralService()

	user := &model.User{Username: "nobind", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, user.Insert(0))

	snapshot, err := service.BuildOrderSnapshot(user.Id, 12.34, "usd")
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	require.Equal(t, model.ReferralCommissionJobStatusSkipped, snapshot.Status)
	require.Equal(t, "no_binding", snapshot.Error)
	require.Equal(t, 12.34, snapshot.BaseAmount)
	require.Equal(t, "USD", snapshot.Currency)
}

func TestBuildOrderSnapshotAllowsZeroRateOverride(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	invitee := &model.User{Username: "invitee-zero", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	affiliateUser := &model.User{Username: "affiliate-zero", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	require.NoError(t, affiliateUser.Insert(0))

	zero := 0.0
	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "ZERO1234",
		Status:             model.ReferralAffiliateStatusApproved,
		RateOverride:       &zero,
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

	snapshot, err := service.BuildOrderSnapshot(invitee.Id, 20, "usd")
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	require.Equal(t, model.ReferralCommissionJobStatusSkipped, snapshot.Status)
	require.Equal(t, "invalid_rate", snapshot.Error)
	require.Zero(t, snapshot.Rate)
}

func TestBindInviteeByLegacyAffCodeLikeValueDoesNotCreateBinding(t *testing.T) {
	setupReferralServiceTestDB(t)
	service := NewReferralService()

	user := &model.User{Username: "legacy-aff-user", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, user.Insert(0))

	require.NoError(t, service.BindInviteeByCode(user.Id, "OLDAFF01", "code"))

	var count int64
	require.NoError(t, model.DB.Model(&model.ReferralBinding{}).Where("invitee_user_id = ?", user.Id).Count(&count).Error)
	require.Zero(t, count)
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

func TestBindInviteeRequiresBothUsersForBindingLock(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	affiliateUser := &model.User{Username: "lock-aff", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "LOCK1234",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, db.Create(affiliate).Error)

	err := service.BindInviteeByCode(affiliateUser.Id+99999, affiliate.InviteCode, "code")
	require.Error(t, err)
}

func TestApplyAffiliateDoesNotDowngradeApprovedAffiliate(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()
	common.ReferralRequireApproval = true

	user := &model.User{Username: "approved-reapply", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, user.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             user.Id,
		InviteCode:         "REAPPLY1",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
		ApprovedAt:         time.Now().Unix(),
	}
	require.NoError(t, db.Create(affiliate).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionAccount{
		AffiliateId: affiliate.Id,
		UserId:      user.Id,
	}).Error)

	profile, err := service.ApplyAffiliate(ReferralApplyInput{
		UserId:        user.Id,
		ApplicantNote: "repeat application",
	})
	require.NoError(t, err)
	require.NotNil(t, profile)
	require.Equal(t, model.ReferralAffiliateStatusApproved, profile.Status)
	require.True(t, profile.AcquisitionEnabled)
	require.True(t, profile.SettlementEnabled)
	require.True(t, profile.WithdrawalEnabled)
	require.Equal(t, "REAPPLY1", profile.InviteCode)

	stored := &model.ReferralAffiliate{}
	require.NoError(t, db.Where("user_id = ?", user.Id).First(stored).Error)
	require.Equal(t, model.ReferralAffiliateStatusApproved, stored.Status)
	require.True(t, stored.AcquisitionEnabled)
	require.True(t, stored.SettlementEnabled)
	require.True(t, stored.WithdrawalEnabled)
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

func TestCreateWithdrawalStripsSignedQRURLBeforePersisting(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	affiliateUser := &model.User{Username: "withdraw-signature", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "WDSIGN01",
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
		Purpose:     model.ReferralAssetPurposeWithdrawalQR,
		StoragePath: "/referral-assets/withdraw-qr.png",
		ContentType: "image/png",
		Size:        123,
		CreatedBy:   "user",
		CreatedAt:   time.Now().Unix(),
	}).Error)

	view, err := service.CreateWithdrawal(ReferralWithdrawalCreateInput{
		UserId:         affiliateUser.Id,
		Amount:         10,
		AccountType:    "alipay",
		AccountName:    "tester",
		AccountNo:      "abc123",
		QRImageURL:     "/referral-assets/withdraw-qr.png?expires=1&sig=test",
		IdempotencyKey: "wd-strip-signature",
	})
	require.NoError(t, err)
	require.NotNil(t, view)
	require.Equal(t, "/referral-assets/withdraw-qr.png", stripAssetSignature(view.QRImageURL))

	stored := &model.ReferralWithdrawal{}
	require.NoError(t, db.First(stored, view.Id).Error)
	require.Equal(t, "/referral-assets/withdraw-qr.png", stored.QRImageURL)
}

func TestCreateWithdrawalRejectsWechatAndNormalizesUSDTNetwork(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	user := &model.User{Username: "withdraw-usdt", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, user.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             user.Id,
		InviteCode:         "WDUSDT01",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, db.Create(affiliate).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionAccount{
		AffiliateId:     affiliate.Id,
		UserId:          user.Id,
		AvailableAmount: 100,
	}).Error)

	_, err := service.CreateWithdrawal(ReferralWithdrawalCreateInput{
		UserId:         user.Id,
		Amount:         10,
		AccountType:    "wechat",
		AccountName:    "tester",
		AccountNo:      "wx-1",
		IdempotencyKey: "wd-wechat",
	})
	require.ErrorContains(t, err, "invalid withdraw type")

	view, err := service.CreateWithdrawal(ReferralWithdrawalCreateInput{
		UserId:         user.Id,
		Amount:         10,
		AccountType:    "usdt",
		AccountNo:      "0x1234567890",
		AccountNetwork: "polygon",
		IdempotencyKey: "wd-usdt-polygon",
	})
	require.NoError(t, err)
	require.Equal(t, "Polygon", view.AccountNetwork)

	stored := &model.ReferralWithdrawal{}
	require.NoError(t, db.First(stored, view.Id).Error)
	require.Equal(t, "Polygon", stored.AccountNetwork)
	require.Empty(t, stored.AccountName)
}

func TestCancelWithdrawalRejectedForUserSubmittedRequests(t *testing.T) {
	setupReferralServiceTestDB(t)
	service := NewReferralService()

	_, err := service.CancelWithdrawal(1, 1)
	require.ErrorContains(t, err, "cannot be manually canceled")
}

func TestGetSummaryReturnsEmptySummaryWhenAffiliateMissing(t *testing.T) {
	setupReferralServiceTestDB(t)
	service := NewReferralService()

	user := &model.User{Username: "no-affiliate", Password: "12345678", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}
	require.NoError(t, user.Insert(0))

	summary, err := service.GetSummary(user.Id)
	require.NoError(t, err)
	require.NotNil(t, summary)
	require.Equal(t, "CNY", summary.SettlementCurrency)
	require.False(t, summary.AcquisitionEnabled)
	require.Zero(t, summary.AvailableAmount)

	commissions, commissionTotal, err := service.ListUserCommissions(user.Id, ReferralListParams{})
	require.NoError(t, err)
	require.Zero(t, commissionTotal)
	require.Empty(t, commissions)

	withdrawals, withdrawalTotal, err := service.ListUserWithdrawals(user.Id, ReferralListParams{})
	require.NoError(t, err)
	require.Zero(t, withdrawalTotal)
	require.Empty(t, withdrawals)
}

func TestUserWithdrawalViewMasksAccountNumber(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	user := &model.User{Username: "withdraw-mask", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, user.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             user.Id,
		InviteCode:         "MASK0001",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, db.Create(affiliate).Error)
	withdrawal := &model.ReferralWithdrawal{
		AffiliateId: affiliate.Id,
		UserId:      user.Id,
		Amount:      10,
		NetAmount:   10,
		AccountType: "alipay",
		AccountName: "tester",
		AccountNo:   "acct1234567890",
		Status:      model.ReferralWithdrawalStatusPending,
		SubmittedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(withdrawal).Error)

	userView, err := service.GetWithdrawalById(withdrawal.Id, false)
	require.NoError(t, err)
	require.Equal(t, "acct******7890", userView.AccountNo)
	require.Equal(t, "acct******7890", userView.AccountNoMasked)

	adminView, err := service.GetWithdrawalById(withdrawal.Id, true)
	require.NoError(t, err)
	require.Equal(t, "acct1234567890", adminView.AccountNo)
	require.Equal(t, "acct******7890", adminView.AccountNoMasked)
}

func TestAdjustAffiliateCommissionRejectsConflictingIdempotencyPayload(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	affiliateUser := &model.User{Username: "adjust-user", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "ADJUST01",
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

	first, err := service.AdjustAffiliateCommission(ReferralAdjustInput{
		UserId:         affiliateUser.Id,
		AdminUserId:    999,
		Delta:          10,
		Remark:         "bonus",
		IdempotencyKey: "same-key",
	})
	require.NoError(t, err)
	require.NotNil(t, first)

	_, err = service.AdjustAffiliateCommission(ReferralAdjustInput{
		UserId:         affiliateUser.Id,
		AdminUserId:    999,
		Delta:          9,
		Remark:         "bonus",
		IdempotencyKey: "same-key",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "idempotency key conflicts")
}

func TestUpdateSettingsRejectsUnsafeRedirectPath(t *testing.T) {
	setupReferralServiceTestDB(t)
	service := NewReferralService()

	_, err := service.UpdateSettings(ReferralSettings{
		Enabled:            true,
		CookieTTLDays:      30,
		DefaultRate:        10,
		SettleFreezeDays:   7,
		MinWithdrawAmount:  1,
		WithdrawFee:        0,
		RedirectPath:       "//evil.com",
		RequireApproval:    true,
		SettlementCurrency: "CNY",
		SettlementFxRates:  `{"CNY":1}`,
	}, 1, "127.0.0.1", "unit-test")
	require.Error(t, err)
	require.Contains(t, err.Error(), "redirect_path")
}

func TestUpdateSettingsNormalizesReferralFxRates(t *testing.T) {
	setupReferralServiceTestDB(t)
	service := NewReferralService()

	settings, err := service.UpdateSettings(ReferralSettings{
		Enabled:            true,
		CookieTTLDays:      30,
		DefaultRate:        10,
		SettleFreezeDays:   7,
		MinWithdrawAmount:  1,
		WithdrawFee:        0,
		RedirectPath:       "/sign-up",
		RequireApproval:    true,
		SettlementCurrency: "CNY",
		SettlementFxRates:  `{"usd":7.1,"EUR":8}`,
	}, 1, "127.0.0.1", "unit-test")
	require.NoError(t, err)
	require.Equal(t, "CNY", settings.SettlementCurrency)
	require.JSONEq(t, `{"CNY":1,"USD":7.1,"EUR":8}`, settings.SettlementFxRates)
}

func TestUpdateSettingsRejectsUnsupportedSettlementCurrency(t *testing.T) {
	setupReferralServiceTestDB(t)
	service := NewReferralService()

	_, err := service.UpdateSettings(ReferralSettings{
		Enabled:            true,
		CookieTTLDays:      30,
		DefaultRate:        10,
		SettleFreezeDays:   7,
		MinWithdrawAmount:  1,
		WithdrawFee:        0,
		RedirectPath:       "/sign-up",
		RequireApproval:    true,
		SettlementCurrency: "USD",
		SettlementFxRates:  `{"USD":7.1}`,
	}, 1, "127.0.0.1", "unit-test")
	require.Error(t, err)
	require.Contains(t, err.Error(), "settlement_currency")
}

func TestUpdateSettingsPreservesFxRatesWhenOmitted(t *testing.T) {
	setupReferralServiceTestDB(t)
	service := NewReferralService()

	common.ReferralSettlementFxRates = map[string]float64{"CNY": 1, "USD": 7.3}

	settings, err := service.UpdateSettings(ReferralSettings{
		Enabled:            true,
		CookieTTLDays:      30,
		DefaultRate:        10,
		SettleFreezeDays:   7,
		MinWithdrawAmount:  1,
		WithdrawFee:        0,
		RedirectPath:       "/sign-up",
		RequireApproval:    true,
		SettlementCurrency: "CNY",
	}, 1, "127.0.0.1", "unit-test")
	require.NoError(t, err)
	require.JSONEq(t, `{"CNY":1,"USD":7.3}`, settings.SettlementFxRates)
}

func TestListCommissionsDoesNotAutoSettle(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	affiliateUser := &model.User{Username: "settle-aff", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "SETTLE01",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, db.Create(affiliate).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionAccount{
		AffiliateId:   affiliate.Id,
		UserId:        affiliateUser.Id,
		PendingAmount: 5,
	}).Error)
	commission := &model.ReferralCommission{
		AffiliateId:      affiliate.Id,
		AffiliateUserId:  affiliateUser.Id,
		InviteeUserId:    affiliateUser.Id + 100,
		SourceType:       "topup",
		SourceOrderId:    1,
		SourceTradeNo:    "trade-auto-settle",
		OrderType:        "topup",
		BaseAmount:       50,
		PaidAmount:       50,
		PaidCurrency:     "CNY",
		Rate:             10,
		CommissionAmount: 5,
		Status:           model.ReferralCommissionStatusPending,
		SettleAt:         time.Now().Add(-time.Hour).Unix(),
	}
	require.NoError(t, db.Create(commission).Error)

	items, total, err := service.ListAffiliateCommissions(affiliateUser.Id, ReferralListParams{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.EqualValues(t, 1, total)

	reloaded := &model.ReferralCommission{}
	require.NoError(t, db.First(reloaded, commission.Id).Error)
	require.Equal(t, model.ReferralCommissionStatusPending, reloaded.Status)
}

func TestGetProfileSanitizesPendingAffiliateFlags(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	user := &model.User{Username: "pending-aff", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, user.Insert(0))
	require.NoError(t, db.Create(&model.ReferralAffiliate{
		UserId:             user.Id,
		InviteCode:         "PEND1234",
		Status:             model.ReferralAffiliateStatusPending,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}).Error)

	profile, err := service.GetProfile(user.Id)
	require.NoError(t, err)
	require.NotNil(t, profile)
	require.False(t, profile.AcquisitionEnabled)
	require.False(t, profile.SettlementEnabled)
	require.False(t, profile.WithdrawalEnabled)
}

func TestApplyAffiliateAutoApprovesWhenApprovalDisabled(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	common.ReferralRequireApproval = false

	user := &model.User{Username: "auto-approve", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, user.Insert(0))

	profile, err := service.ApplyAffiliate(ReferralApplyInput{
		UserId:        user.Id,
		ApplicantNote: "auto approve",
	})
	require.NoError(t, err)
	require.NotNil(t, profile)
	require.Equal(t, model.ReferralAffiliateStatusApproved, profile.Status)
	require.True(t, profile.AcquisitionEnabled)
	require.True(t, profile.SettlementEnabled)
	require.True(t, profile.WithdrawalEnabled)

	stored := &model.ReferralAffiliate{}
	require.NoError(t, db.Where("user_id = ?", user.Id).First(stored).Error)
	require.Equal(t, model.ReferralAffiliateStatusApproved, stored.Status)
	require.True(t, stored.AcquisitionEnabled)
	require.True(t, stored.SettlementEnabled)
	require.True(t, stored.WithdrawalEnabled)
	require.NotZero(t, stored.ApprovedAt)
}

func TestMarkWithdrawalPaidRequiresTxnOrProof(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	user := &model.User{Username: "withdraw-paid", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, user.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             user.Id,
		InviteCode:         "PAID0001",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, db.Create(affiliate).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionAccount{
		AffiliateId:  affiliate.Id,
		UserId:       user.Id,
		FrozenAmount: 10,
	}).Error)
	require.NoError(t, db.Create(&model.ReferralWithdrawal{
		AffiliateId: affiliate.Id,
		UserId:      user.Id,
		Amount:      10,
		NetAmount:   10,
		AccountType: "alipay",
		AccountName: "tester",
		AccountNo:   "abc",
		Status:      model.ReferralWithdrawalStatusApproved,
		SubmittedAt: time.Now().Unix(),
		ApprovedAt:  time.Now().Unix(),
	}).Error)

	_, err := service.MarkWithdrawalPaid(ReferralWithdrawalPayInput{
		WithdrawalId: 1,
		AdminUserId:  100,
		AdminNote:    "offline paid",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "payment_txn_no or payment_proof_url is required")
}

func TestApproveWithdrawalCreatesLedgerEntry(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	user := &model.User{Username: "withdraw-approve-ledger", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, user.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             user.Id,
		InviteCode:         "APRVLED1",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, db.Create(affiliate).Error)
	withdrawal := &model.ReferralWithdrawal{
		AffiliateId: affiliate.Id,
		UserId:      user.Id,
		Amount:      10,
		NetAmount:   10,
		AccountType: "alipay",
		AccountName: "tester",
		AccountNo:   "abc",
		Status:      model.ReferralWithdrawalStatusPending,
		SubmittedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(withdrawal).Error)

	view, err := service.ApproveWithdrawal(ReferralWithdrawalReviewInput{
		WithdrawalId: withdrawal.Id,
		AdminUserId:  100,
		AdminNote:    "checked",
	})
	require.NoError(t, err)
	require.NotNil(t, view)

	var ledger model.ReferralCommissionLedger
	require.NoError(t, db.Where("external_ref_id = ?", fmt.Sprintf("withdrawal_approve:%d", withdrawal.Id)).First(&ledger).Error)
	require.Equal(t, "withdrawal_approve", ledger.Type)
	require.Equal(t, "admin", ledger.Operator)
}

func TestMarkWithdrawalPaidStripsSignedProofURLBeforePersisting(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	user := &model.User{Username: "withdraw-proof-strip", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, user.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             user.Id,
		InviteCode:         "PRFSTR01",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, db.Create(affiliate).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionAccount{
		AffiliateId:  affiliate.Id,
		UserId:       user.Id,
		FrozenAmount: 10,
	}).Error)
	require.NoError(t, db.Create(&model.ReferralAsset{
		OwnerUserId: user.Id,
		Purpose:     model.ReferralAssetPurposePaymentProof,
		StoragePath: "/referral-assets/payment-proof.png",
		ContentType: "image/png",
		Size:        123,
		CreatedBy:   "admin",
		CreatedAt:   time.Now().Unix(),
	}).Error)
	withdrawal := &model.ReferralWithdrawal{
		AffiliateId: affiliate.Id,
		UserId:      user.Id,
		Amount:      10,
		NetAmount:   10,
		AccountType: "alipay",
		AccountName: "tester",
		AccountNo:   "abc",
		Status:      model.ReferralWithdrawalStatusApproved,
		SubmittedAt: time.Now().Unix(),
		ApprovedAt:  time.Now().Unix(),
	}
	require.NoError(t, db.Create(withdrawal).Error)

	view, err := service.MarkWithdrawalPaid(ReferralWithdrawalPayInput{
		WithdrawalId:    withdrawal.Id,
		AdminUserId:     100,
		AdminNote:       "paid",
		PaymentProofURL: "/referral-assets/payment-proof.png?expires=1&sig=test",
	})
	require.NoError(t, err)
	require.NotNil(t, view)

	stored := &model.ReferralWithdrawal{}
	require.NoError(t, db.First(stored, withdrawal.Id).Error)
	require.Equal(t, "/referral-assets/payment-proof.png", stored.PaymentProofURL)
}

func TestApproveAffiliateCreatesAuditLogWhenAdminCreatesAffiliate(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	user := &model.User{Username: "approve-created", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, user.Insert(0))

	view, err := service.ApproveAffiliate(user.Id, 9001, nil, "127.0.0.1", "unit-test")
	require.NoError(t, err)
	require.NotNil(t, view)
	require.Equal(t, model.ReferralAffiliateStatusApproved, view.Status)

	var audit model.ReferralAdminAuditLog
	require.NoError(t, db.Where("action = ? AND target_user_id = ?", "referral_affiliate_approve", user.Id).First(&audit).Error)
	require.Equal(t, 9001, audit.AdminUserId)
	require.Equal(t, "127.0.0.1", audit.Ip)
	require.Equal(t, "unit-test", audit.UserAgent)
}
