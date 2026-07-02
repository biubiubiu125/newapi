package service

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
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
	previousRequireApproval := common.ReferralRequireApproval
	previousSettlementCurrency := common.ReferralSettlementCurrency
	previousSettlementFxRates := common.ReferralSettlementFxRates
	previousRedemptionUSDToCNYRate := common.ReferralRedemptionUSDToCNYRate
	previousQuotaPerUnit := common.QuotaPerUnit
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
	common.ReferralRedemptionUSDToCNYRate = 1
	common.QuotaPerUnit = 500000
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
		&model.UserLoginIdentifier{},
		&model.Option{},
		&model.Log{},
		&model.TopUp{},
		&model.Redemption{},
		&model.SubscriptionPlan{},
		&model.SubscriptionOrder{},
		&model.UserSubscription{},
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
		common.ReferralRequireApproval = previousRequireApproval
		common.ReferralSettlementCurrency = previousSettlementCurrency
		common.ReferralSettlementFxRates = previousSettlementFxRates
		common.ReferralRedemptionUSDToCNYRate = previousRedemptionUSDToCNYRate
		common.QuotaPerUnit = previousQuotaPerUnit
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

func TestProcessRedemptionCommissionUsesConfiguredExchangeRate(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	common.ReferralDefaultRate = 10
	common.ReferralRedemptionUSDToCNYRate = 2

	invitee := &model.User{Username: "redemption-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	affiliateUser := &model.User{Username: "redemption-affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))

	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "REDEEM01",
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

	redemption := &model.Redemption{
		Key:          "redemption-test-key-0000000001",
		Name:         "paid redemption code",
		Quota:        int(common.QuotaPerUnit * 100),
		Status:       common.RedemptionCodeStatusUsed,
		UsedUserId:   invitee.Id,
		RedeemedTime: time.Now().Unix(),
	}
	require.NoError(t, db.Create(redemption).Error)
	tradeNo := redemptionCommissionTradeNo(redemption.Id)

	for i := 0; i < 3; i++ {
		require.NoError(t, service.ProcessRedemptionCommission(redemption.Id))
	}
	require.NoError(t, service.RetryCommissionJob("redemption", tradeNo))

	var commissions int64
	require.NoError(t, db.Model(&model.ReferralCommission{}).Where("source_type = ? AND source_trade_no = ?", "redemption", tradeNo).Count(&commissions).Error)
	require.EqualValues(t, 1, commissions)

	commission := &model.ReferralCommission{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", tradeNo).First(commission).Error)
	require.Equal(t, redemption.Id, commission.SourceOrderId)
	require.Equal(t, "redemption", commission.OrderType)
	require.Equal(t, 200.0, commission.PaidAmount)
	require.Equal(t, "CNY", commission.PaidCurrency)
	require.Equal(t, 200.0, commission.SettlementBaseAmount)
	require.Equal(t, 1.0, commission.SettlementFxRate)
	require.Equal(t, 10.0, commission.Rate)
	require.Equal(t, 20.0, commission.CommissionAmount)

	account := &model.ReferralCommissionAccount{}
	require.NoError(t, db.Where("affiliate_id = ?", affiliate.Id).First(account).Error)
	require.Equal(t, 20.0, account.PendingAmount)

	var ledgers int64
	require.NoError(t, db.Model(&model.ReferralCommissionLedger{}).Where("ref_type = ? AND ref_id = ?", "redemption", tradeNo).Count(&ledgers).Error)
	require.EqualValues(t, 1, ledgers)

	reloadedRedemption := &model.Redemption{}
	require.NoError(t, db.Where("id = ?", redemption.Id).First(reloadedRedemption).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSucceeded, reloadedRedemption.ReferralCommissionStatus)
	require.Equal(t, affiliate.Id, reloadedRedemption.ReferralAffiliateId)
	require.Equal(t, 10.0, reloadedRedemption.ReferralRate)
}

func TestBackfillRedemptionCommissionJobsProcessesUsedCodesWithoutJobs(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	common.ReferralDefaultRate = 10
	common.ReferralRedemptionUSDToCNYRate = 2

	invitee := &model.User{Username: "backfill-redemption-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	affiliateUser := &model.User{Username: "backfill-redemption-affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))

	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "BACKFILL01",
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

	redemption := &model.Redemption{
		Key:          "backfill-redemption-key-0001",
		Name:         "historical used code",
		Quota:        int(common.QuotaPerUnit * 100),
		Status:       common.RedemptionCodeStatusUsed,
		UsedUserId:   invitee.Id,
		RedeemedTime: time.Now().Unix(),
	}
	require.NoError(t, db.Create(redemption).Error)
	disabledRedemption := &model.Redemption{
		Key:                      "backfill-disabled-redeemed-key-0001",
		Name:                     "historical disabled redeemed code",
		Quota:                    int(common.QuotaPerUnit * 100),
		Status:                   common.RedemptionCodeStatusDisabled,
		UsedUserId:               invitee.Id,
		RedeemedTime:             time.Now().Unix(),
		ReferralCommissionStatus: model.ReferralCommissionJobStatusFailed,
		ReferralCommissionError:  "temporary error",
	}
	require.NoError(t, db.Create(disabledRedemption).Error)
	succeededIncompleteRedemption := &model.Redemption{
		Key:                      "backfill-redemption-succeeded-0001",
		Name:                     "already processed code",
		Quota:                    int(common.QuotaPerUnit),
		Status:                   common.RedemptionCodeStatusUsed,
		UsedUserId:               invitee.Id,
		RedeemedTime:             time.Now().Unix(),
		ReferralCommissionStatus: model.ReferralCommissionJobStatusSucceeded,
	}
	require.NoError(t, db.Create(succeededIncompleteRedemption).Error)

	result, err := service.BackfillRedemptionCommissionJobs(10)
	require.NoError(t, err)
	require.Equal(t, 3, result.Scanned)
	require.Equal(t, 3, result.Processed)
	require.Equal(t, 0, result.Failed)

	tradeNo := redemptionCommissionTradeNo(redemption.Id)
	var jobs int64
	require.NoError(t, db.Model(&model.ReferralCommissionJob{}).Where("source_type = ? AND source_trade_no = ?", "redemption", tradeNo).Count(&jobs).Error)
	require.EqualValues(t, 1, jobs)

	commission := &model.ReferralCommission{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", tradeNo).First(commission).Error)
	require.Equal(t, 200.0, commission.PaidAmount)
	require.Equal(t, 20.0, commission.CommissionAmount)
	disabledTradeNo := redemptionCommissionTradeNo(disabledRedemption.Id)
	disabledCommission := &model.ReferralCommission{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", disabledTradeNo).First(disabledCommission).Error)
	require.Equal(t, 200.0, disabledCommission.PaidAmount)
	require.Equal(t, 20.0, disabledCommission.CommissionAmount)

	account := &model.ReferralCommissionAccount{}
	require.NoError(t, db.Where("affiliate_id = ?", affiliate.Id).First(account).Error)
	require.InDelta(t, 40.2, account.PendingAmount, 0.000001)

	reloadedRedemption := &model.Redemption{}
	require.NoError(t, db.Where("id = ?", redemption.Id).First(reloadedRedemption).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSucceeded, reloadedRedemption.ReferralCommissionStatus)
	reloadedDisabledRedemption := &model.Redemption{}
	require.NoError(t, db.Where("id = ?", disabledRedemption.Id).First(reloadedDisabledRedemption).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSucceeded, reloadedDisabledRedemption.ReferralCommissionStatus)
	succeededIncompleteTradeNo := redemptionCommissionTradeNo(succeededIncompleteRedemption.Id)
	succeededIncompleteCommission := &model.ReferralCommission{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", succeededIncompleteTradeNo).First(succeededIncompleteCommission).Error)
	require.Equal(t, 2.0, succeededIncompleteCommission.PaidAmount)
	require.Equal(t, 0.2, succeededIncompleteCommission.CommissionAmount)
	reloadedSucceededIncomplete := &model.Redemption{}
	require.NoError(t, db.Where("id = ?", succeededIncompleteRedemption.Id).First(reloadedSucceededIncomplete).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSucceeded, reloadedSucceededIncomplete.ReferralCommissionStatus)
}

func TestBackfillRedemptionCommissionJobsUsesSucceededCursor(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	common.QuotaPerUnit = 100
	common.ReferralDefaultRate = 10
	common.ReferralRedemptionUSDToCNYRate = 2

	invitee := &model.User{Username: "backfill-cursor-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	affiliateUser := &model.User{Username: "backfill-cursor-affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))

	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "BFCURSOR01",
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
		AffiliateId:   affiliate.Id,
		UserId:        affiliateUser.Id,
		PendingAmount: 0.4,
	}).Error)

	createCompleteSucceededRedemption := func(key string) *model.Redemption {
		redemption := &model.Redemption{
			Key:                      key,
			Name:                     key,
			Quota:                    int(common.QuotaPerUnit),
			Status:                   common.RedemptionCodeStatusUsed,
			UsedUserId:               invitee.Id,
			RedeemedTime:             time.Now().Unix(),
			ReferralAffiliateId:      affiliate.Id,
			ReferralRate:             10,
			ReferralBaseAmount:       2,
			ReferralBaseCurrency:     "CNY",
			ReferralCommissionStatus: model.ReferralCommissionJobStatusSucceeded,
		}
		require.NoError(t, db.Create(redemption).Error)
		tradeNo := redemptionCommissionTradeNo(redemption.Id)
		commission := &model.ReferralCommission{
			AffiliateId:          affiliate.Id,
			AffiliateUserId:      affiliateUser.Id,
			InviteeUserId:        invitee.Id,
			SourceType:           "redemption",
			SourceOrderId:        redemption.Id,
			SourceTradeNo:        tradeNo,
			OrderType:            "redemption",
			BaseAmount:           2,
			PaidAmount:           2,
			PaidCurrency:         "CNY",
			SettlementCurrency:   "CNY",
			SettlementFxRate:     1,
			SettlementBaseAmount: 2,
			Rate:                 10,
			CommissionAmount:     0.2,
			Status:               model.ReferralCommissionStatusPending,
		}
		require.NoError(t, db.Create(commission).Error)
		require.NoError(t, db.Create(&model.ReferralCommissionLedger{
			AffiliateId:        affiliate.Id,
			UserId:             affiliateUser.Id,
			CommissionId:       commission.Id,
			Type:               "commission_accrue",
			RefType:            "redemption",
			RefId:              tradeNo,
			ExternalRefId:      fmt.Sprintf("accrue:redemption:%s", tradeNo),
			SettlementCurrency: "CNY",
			DeltaPending:       0.2,
			Operator:           "system",
			CreatedAt:          time.Now().Unix(),
		}).Error)
		return redemption
	}

	createCompleteSucceededRedemption("backfill-cursor-complete-0001")
	secondComplete := createCompleteSucceededRedemption("backfill-cursor-complete-0002")
	incompleteRedemption := &model.Redemption{
		Key:                      "backfill-cursor-incomplete-0001",
		Name:                     "backfill cursor incomplete",
		Quota:                    int(common.QuotaPerUnit),
		Status:                   common.RedemptionCodeStatusUsed,
		UsedUserId:               invitee.Id,
		RedeemedTime:             time.Now().Unix(),
		ReferralCommissionStatus: model.ReferralCommissionJobStatusSucceeded,
	}
	require.NoError(t, db.Create(incompleteRedemption).Error)

	result, err := service.BackfillRedemptionCommissionJobsWithOptions(ReferralRedemptionBackfillOptions{
		Limit:              10,
		SucceededScanLimit: 2,
	})
	require.NoError(t, err)
	require.Equal(t, 0, result.Scanned)
	require.Equal(t, 0, result.Processed)
	require.Equal(t, 2, result.SucceededScanned)
	require.Equal(t, secondComplete.Id, result.NextSucceededCursorID)
	require.True(t, result.HasMoreSucceeded)

	var incompleteCommissionCount int64
	require.NoError(t, db.Model(&model.ReferralCommission{}).
		Where("source_type = ? AND source_trade_no = ?", "redemption", redemptionCommissionTradeNo(incompleteRedemption.Id)).
		Count(&incompleteCommissionCount).Error)
	require.Zero(t, incompleteCommissionCount)

	result, err = service.BackfillRedemptionCommissionJobsWithOptions(ReferralRedemptionBackfillOptions{
		Limit:              10,
		SucceededCursorID:  result.NextSucceededCursorID,
		SucceededScanLimit: 10,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.Scanned)
	require.Equal(t, 1, result.Processed)
	require.Equal(t, 1, result.SucceededScanned)
	require.False(t, result.HasMoreSucceeded)

	require.NoError(t, db.Model(&model.ReferralCommission{}).
		Where("source_type = ? AND source_trade_no = ?", "redemption", redemptionCommissionTradeNo(incompleteRedemption.Id)).
		Count(&incompleteCommissionCount).Error)
	require.EqualValues(t, 1, incompleteCommissionCount)
}

func TestProcessRedemptionCommissionUsesQuotaPerUnitSnapshot(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	common.ReferralDefaultRate = 10
	common.ReferralRedemptionUSDToCNYRate = 2
	common.QuotaPerUnit = 500

	invitee := &model.User{Username: "redemption-snapshot-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	affiliateUser := &model.User{Username: "redemption-snapshot-affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))

	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "SNAP0001",
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

	redemption := &model.Redemption{
		Key:                  "redemption-snapshot-key-000001",
		Name:                 "snapshot redemption code",
		Quota:                1000,
		QuotaPerUnitSnapshot: 100,
		Status:               common.RedemptionCodeStatusUsed,
		UsedUserId:           invitee.Id,
		RedeemedTime:         time.Now().Unix(),
	}
	require.NoError(t, db.Create(redemption).Error)
	require.NoError(t, service.ProcessRedemptionCommission(redemption.Id))

	commission := &model.ReferralCommission{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", redemptionCommissionTradeNo(redemption.Id)).First(commission).Error)
	require.Equal(t, 20.0, commission.PaidAmount)
	require.Equal(t, 20.0, commission.SettlementBaseAmount)
	require.Equal(t, 2.0, commission.CommissionAmount)
}

func TestProcessRedemptionCommissionUsesRedeemTimeSnapshot(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	common.QuotaPerUnit = 100
	common.ReferralDefaultRate = 10
	common.ReferralRedemptionUSDToCNYRate = 2

	invitee := &model.User{Username: "redemption-redeem-snapshot-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	affiliateUser := &model.User{Username: "redemption-redeem-snapshot-affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "SNAPRD01",
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
	redemption := &model.Redemption{
		Key:         "redemption-redeem-snapshot-key",
		Name:        "redeem snapshot code",
		Quota:       1000,
		Status:      common.RedemptionCodeStatusEnabled,
		CreatedTime: time.Now().Unix(),
	}
	require.NoError(t, db.Create(redemption).Error)

	redeemResult, err := model.Redeem(redemption.Key, invitee.Id)
	require.NoError(t, err)
	common.ReferralDefaultRate = 80
	common.ReferralRedemptionUSDToCNYRate = 99

	require.NoError(t, service.ProcessRedemptionCommission(redeemResult.RedemptionId))

	commission := &model.ReferralCommission{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", redemptionCommissionTradeNo(redeemResult.RedemptionId)).First(commission).Error)
	require.Equal(t, 20.0, commission.PaidAmount)
	require.Equal(t, 20.0, commission.SettlementBaseAmount)
	require.Equal(t, 10.0, commission.Rate)
	require.Equal(t, 2.0, commission.CommissionAmount)
}

func TestProcessRedemptionCommissionRetryUsesRedeemTimeReferralSnapshot(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	common.QuotaPerUnit = 100
	common.ReferralDefaultRate = 10
	common.ReferralRedemptionUSDToCNYRate = 0

	invitee := &model.User{Username: "redemption-failed-snapshot-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	affiliateUser := &model.User{Username: "redemption-failed-snapshot-affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "FAILSNAP",
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
	redemption := &model.Redemption{
		Key:         "redemption-failed-snapshot-key",
		Name:        "failed snapshot code",
		Quota:       1000,
		Status:      common.RedemptionCodeStatusEnabled,
		CreatedTime: time.Now().Unix(),
	}
	require.NoError(t, db.Create(redemption).Error)

	redeemResult, err := model.Redeem(redemption.Key, invitee.Id)
	require.NoError(t, err)
	tradeNo := redemptionCommissionTradeNo(redeemResult.RedemptionId)

	reloadedRedemption := &model.Redemption{}
	require.NoError(t, db.Where("id = ?", redeemResult.RedemptionId).First(reloadedRedemption).Error)
	require.Equal(t, model.ReferralCommissionJobStatusFailed, reloadedRedemption.ReferralCommissionStatus)
	require.Equal(t, affiliate.Id, reloadedRedemption.ReferralAffiliateId)
	require.Equal(t, 10.0, reloadedRedemption.ReferralRate)
	require.Zero(t, reloadedRedemption.ReferralBaseAmount)
	require.Contains(t, reloadedRedemption.ReferralCommissionError, "redemption_usd_to_cny_rate")

	job := &model.ReferralCommissionJob{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", tradeNo).First(job).Error)
	require.Equal(t, model.ReferralCommissionJobStatusFailed, job.Status)
	require.Equal(t, affiliate.Id, job.AffiliateId)

	common.ReferralDefaultRate = 80
	common.ReferralRedemptionUSDToCNYRate = 2

	require.NoError(t, service.RetryCommissionJob("redemption", tradeNo))

	commission := &model.ReferralCommission{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", tradeNo).First(commission).Error)
	require.Equal(t, 20.0, commission.PaidAmount)
	require.Equal(t, 20.0, commission.SettlementBaseAmount)
	require.Equal(t, 10.0, commission.Rate)
	require.Equal(t, 2.0, commission.CommissionAmount)

	require.NoError(t, db.Where("id = ?", redeemResult.RedemptionId).First(reloadedRedemption).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSucceeded, reloadedRedemption.ReferralCommissionStatus)
	require.Equal(t, affiliate.Id, reloadedRedemption.ReferralAffiliateId)
	require.Equal(t, 10.0, reloadedRedemption.ReferralRate)
	require.Equal(t, 20.0, reloadedRedemption.ReferralBaseAmount)
	require.Empty(t, reloadedRedemption.ReferralCommissionError)
}

func TestProcessRedemptionCommissionCreatesSkippedJobWithoutBinding(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	invitee := &model.User{Username: "redemption-no-binding", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	redemption := &model.Redemption{
		Key:                      "redemption-no-binding-key-001",
		Name:                     "no binding redemption code",
		Quota:                    int(common.QuotaPerUnit),
		Status:                   common.RedemptionCodeStatusUsed,
		UsedUserId:               invitee.Id,
		RedeemedTime:             time.Now().Unix(),
		ReferralCommissionStatus: model.ReferralCommissionJobStatusPending,
	}
	require.NoError(t, db.Create(redemption).Error)
	tradeNo := redemptionCommissionTradeNo(redemption.Id)
	require.NoError(t, db.Create(&model.ReferralCommissionJob{
		SourceType:    "redemption",
		SourceTradeNo: tradeNo,
		Status:        model.ReferralCommissionJobStatusPending,
	}).Error)

	require.NoError(t, service.ProcessRedemptionCommission(redemption.Id))

	job := &model.ReferralCommissionJob{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", tradeNo).First(job).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSkipped, job.Status)
	require.Equal(t, "no_binding", job.LastError)

	reloadedRedemption := &model.Redemption{}
	require.NoError(t, db.Where("id = ?", redemption.Id).First(reloadedRedemption).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSkipped, reloadedRedemption.ReferralCommissionStatus)
	require.Equal(t, "no_binding", reloadedRedemption.ReferralCommissionError)

	var commissions int64
	require.NoError(t, db.Model(&model.ReferralCommission{}).Where("source_type = ? AND source_trade_no = ?", "redemption", tradeNo).Count(&commissions).Error)
	require.Zero(t, commissions)
}

func TestProcessRedemptionCommissionRebuildsFailedMissingSnapshot(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	common.ReferralDefaultRate = 10
	common.ReferralRedemptionUSDToCNYRate = 2
	common.QuotaPerUnit = 100

	invitee := &model.User{Username: "redemption-retry-snapshot-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	affiliateUser := &model.User{Username: "redemption-retry-snapshot-affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "RTRYFAIL",
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

	redemption := &model.Redemption{
		Key:                      "redemption-retry-missing-snapshot",
		Name:                     "retry missing snapshot",
		Quota:                    1000,
		QuotaPerUnitSnapshot:     100,
		Status:                   common.RedemptionCodeStatusUsed,
		UsedUserId:               invitee.Id,
		RedeemedTime:             time.Now().Unix(),
		ReferralBaseAmount:       20,
		ReferralBaseCurrency:     "CNY",
		ReferralCommissionStatus: model.ReferralCommissionJobStatusFailed,
		ReferralCommissionError:  "temporary binding lookup error",
		ReferralCommissionAt:     time.Now().Unix(),
	}
	require.NoError(t, db.Create(redemption).Error)
	tradeNo := redemptionCommissionTradeNo(redemption.Id)
	require.NoError(t, db.Create(&model.ReferralCommissionJob{
		SourceType:    "redemption",
		SourceTradeNo: tradeNo,
		Status:        model.ReferralCommissionJobStatusFailed,
		LastError:     "temporary binding lookup error",
		FailedAt:      time.Now().Unix(),
	}).Error)

	require.NoError(t, service.RetryCommissionJob("redemption", tradeNo))

	commission := &model.ReferralCommission{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", tradeNo).First(commission).Error)
	require.Equal(t, affiliate.Id, commission.AffiliateId)
	require.Equal(t, 20.0, commission.PaidAmount)
	require.Equal(t, 2.0, commission.CommissionAmount)

	reloadedRedemption := &model.Redemption{}
	require.NoError(t, db.Where("id = ?", redemption.Id).First(reloadedRedemption).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSucceeded, reloadedRedemption.ReferralCommissionStatus)
	require.Equal(t, affiliate.Id, reloadedRedemption.ReferralAffiliateId)
	require.Equal(t, 10.0, reloadedRedemption.ReferralRate)
	require.Empty(t, reloadedRedemption.ReferralCommissionError)
}

func TestProcessRedemptionCommissionBackfillsTerminalAt(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	common.ReferralDefaultRate = 10
	common.QuotaPerUnit = 100
	common.ReferralRedemptionUSDToCNYRate = 1

	invitee := &model.User{Username: "redemption-terminal-backfill-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	affiliateUser := &model.User{Username: "redemption-terminal-backfill-affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "TERMAT01",
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

	redemption := &model.Redemption{
		Key:                      "redemption-terminal-at-backfill",
		Name:                     "terminal at backfill",
		Quota:                    100,
		QuotaPerUnitSnapshot:     100,
		Status:                   common.RedemptionCodeStatusUsed,
		UsedUserId:               invitee.Id,
		RedeemedTime:             time.Now().Unix(),
		ReferralCommissionStatus: model.ReferralCommissionJobStatusSkipped,
		ReferralCommissionError:  "no_binding",
	}
	require.NoError(t, db.Create(redemption).Error)

	require.NoError(t, service.ProcessRedemptionCommission(redemption.Id))

	reloadedRedemption := &model.Redemption{}
	require.NoError(t, db.Where("id = ?", redemption.Id).First(reloadedRedemption).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSkipped, reloadedRedemption.ReferralCommissionStatus)
	require.NotZero(t, reloadedRedemption.ReferralCommissionAt)
	require.Zero(t, reloadedRedemption.ReferralAffiliateId)
	require.Zero(t, reloadedRedemption.ReferralRate)
	require.Zero(t, reloadedRedemption.ReferralBaseAmount)
	require.Empty(t, reloadedRedemption.ReferralBaseCurrency)
}

func TestProcessRedemptionCommissionCreatesMissingSkippedTerminalJob(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	invitee := &model.User{Username: "redemption-skipped-missing-job-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))

	now := time.Now().Unix()
	redemption := &model.Redemption{
		Key:                      "redemption-skipped-missing-job",
		Name:                     "skipped missing job",
		Quota:                    int(common.QuotaPerUnit),
		Status:                   common.RedemptionCodeStatusUsed,
		UsedUserId:               invitee.Id,
		RedeemedTime:             now,
		ReferralCommissionStatus: model.ReferralCommissionJobStatusSkipped,
		ReferralCommissionError:  "no_binding",
		ReferralCommissionAt:     now,
	}
	require.NoError(t, db.Create(redemption).Error)
	tradeNo := redemptionCommissionTradeNo(redemption.Id)

	require.NoError(t, service.ProcessRedemptionCommission(redemption.Id))

	job := &model.ReferralCommissionJob{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", tradeNo).First(job).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSkipped, job.Status)
	require.Equal(t, "no_binding", job.LastError)
	require.Equal(t, now, job.SucceededAt)
	require.NoError(t, model.DeleteRedemptionById(redemption.Id))
}

func TestBackfillRedemptionCommissionJobsProcessesSkippedTerminalWithoutJob(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	invitee := &model.User{Username: "redemption-backfill-skipped-missing-job", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))

	now := time.Now().Unix()
	redemption := &model.Redemption{
		Key:                      "redemption-backfill-skipped-missing-job",
		Name:                     "backfill skipped missing job",
		Quota:                    int(common.QuotaPerUnit),
		Status:                   common.RedemptionCodeStatusUsed,
		UsedUserId:               invitee.Id,
		RedeemedTime:             now,
		ReferralCommissionStatus: model.ReferralCommissionJobStatusSkipped,
		ReferralCommissionError:  "no_binding",
		ReferralCommissionAt:     now,
	}
	require.NoError(t, db.Create(redemption).Error)
	tradeNo := redemptionCommissionTradeNo(redemption.Id)

	result, err := service.BackfillRedemptionCommissionJobsWithOptions(ReferralRedemptionBackfillOptions{
		Limit:              10,
		SucceededScanLimit: 10,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.Scanned)
	require.Equal(t, 1, result.Processed)
	require.Equal(t, 1, result.SucceededScanned)

	job := &model.ReferralCommissionJob{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", tradeNo).First(job).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSkipped, job.Status)
	require.Equal(t, "no_binding", job.LastError)
	require.Equal(t, now, job.SucceededAt)
	require.NoError(t, model.DeleteRedemptionById(redemption.Id))
}

func TestRetryCommissionJobSyncsTerminalRedemptionJob(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	invitee := &model.User{Username: "redemption-terminal-job-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	affiliateUser := &model.User{Username: "redemption-terminal-job-affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "TERMJOB1",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, db.Create(affiliate).Error)

	now := time.Now().Unix()
	succeededAt := now - 60
	succeededRedemption := &model.Redemption{
		Key:                      "redemption-terminal-succeeded-job",
		Name:                     "terminal succeeded stale job",
		Quota:                    int(common.QuotaPerUnit),
		Status:                   common.RedemptionCodeStatusUsed,
		UsedUserId:               invitee.Id,
		RedeemedTime:             now,
		ReferralAffiliateId:      affiliate.Id,
		ReferralRate:             10,
		ReferralBaseAmount:       1,
		ReferralBaseCurrency:     "CNY",
		ReferralCommissionStatus: model.ReferralCommissionJobStatusSucceeded,
		ReferralCommissionAt:     succeededAt,
	}
	require.NoError(t, db.Create(succeededRedemption).Error)
	succeededTradeNo := redemptionCommissionTradeNo(succeededRedemption.Id)
	require.NoError(t, db.Create(&model.ReferralCommissionAccount{
		AffiliateId:   affiliate.Id,
		UserId:        affiliateUser.Id,
		PendingAmount: 0.1,
	}).Error)
	succeededCommission := &model.ReferralCommission{
		AffiliateId:          affiliate.Id,
		AffiliateUserId:      affiliateUser.Id,
		InviteeUserId:        invitee.Id,
		SourceType:           "redemption",
		SourceOrderId:        succeededRedemption.Id,
		SourceTradeNo:        succeededTradeNo,
		OrderType:            "redemption",
		BaseAmount:           1,
		PaidAmount:           1,
		PaidCurrency:         "CNY",
		SettlementCurrency:   "CNY",
		SettlementFxRate:     1,
		SettlementBaseAmount: 1,
		Rate:                 10,
		CommissionAmount:     0.1,
		Status:               model.ReferralCommissionStatusPending,
		CreatedAt:            now - 60,
	}
	require.NoError(t, db.Create(succeededCommission).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionLedger{
		AffiliateId:        affiliate.Id,
		UserId:             affiliateUser.Id,
		CommissionId:       succeededCommission.Id,
		Type:               "commission_accrue",
		RefType:            "redemption",
		RefId:              succeededTradeNo,
		ExternalRefId:      fmt.Sprintf("accrue:redemption:%s", succeededTradeNo),
		SettlementCurrency: "CNY",
		DeltaPending:       0.1,
		Operator:           "system",
		CreatedAt:          succeededAt,
	}).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionJob{
		SourceType:    "redemption",
		SourceTradeNo: succeededTradeNo,
		Status:        model.ReferralCommissionJobStatusFailed,
		LastError:     "stale failure",
		FailedAt:      now,
		LockedAt:      now,
	}).Error)

	require.NoError(t, service.RetryCommissionJob("redemption", succeededTradeNo))

	succeededJob := &model.ReferralCommissionJob{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", succeededTradeNo).First(succeededJob).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSucceeded, succeededJob.Status)
	require.Equal(t, affiliate.Id, succeededJob.AffiliateId)
	require.Empty(t, succeededJob.LastError)
	require.Equal(t, succeededAt, succeededJob.SucceededAt)
	require.Zero(t, succeededJob.FailedAt)
	require.Zero(t, succeededJob.LockedAt)
	require.NoError(t, model.DeleteRedemptionById(succeededRedemption.Id))

	skippedRedemption := &model.Redemption{
		Key:                      "redemption-terminal-skipped-job",
		Name:                     "terminal skipped stale job",
		Quota:                    int(common.QuotaPerUnit),
		Status:                   common.RedemptionCodeStatusUsed,
		UsedUserId:               902,
		RedeemedTime:             now,
		ReferralCommissionStatus: model.ReferralCommissionJobStatusSkipped,
		ReferralCommissionError:  "no_binding",
	}
	require.NoError(t, db.Create(skippedRedemption).Error)
	skippedTradeNo := redemptionCommissionTradeNo(skippedRedemption.Id)
	require.NoError(t, db.Create(&model.ReferralCommissionJob{
		SourceType:    "redemption",
		SourceTradeNo: skippedTradeNo,
		Status:        model.ReferralCommissionJobStatusProcessing,
		LastError:     "stale processing",
		FailedAt:      now,
		LockedAt:      now,
	}).Error)

	require.NoError(t, service.RetryCommissionJob("redemption", skippedTradeNo))

	skippedJob := &model.ReferralCommissionJob{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", skippedTradeNo).First(skippedJob).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSkipped, skippedJob.Status)
	require.Equal(t, "no_binding", skippedJob.LastError)
	require.NotZero(t, skippedJob.SucceededAt)
	require.Zero(t, skippedJob.FailedAt)
	require.Zero(t, skippedJob.LockedAt)
	reloadedSkipped := &model.Redemption{}
	require.NoError(t, db.Where("id = ?", skippedRedemption.Id).First(reloadedSkipped).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSkipped, reloadedSkipped.ReferralCommissionStatus)
	require.NotZero(t, reloadedSkipped.ReferralCommissionAt)
	require.NoError(t, model.DeleteRedemptionById(skippedRedemption.Id))
}

func TestRetryCommissionJobReconcilesMismatchedTerminalRedemptionJob(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	invitee := &model.User{Username: "redemption-mismatch-terminal-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	affiliateUser := &model.User{Username: "redemption-mismatch-terminal-affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "TERMMIS1",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, db.Create(affiliate).Error)

	now := time.Now().Unix()
	succeededAt := now - 90
	succeededRedemption := &model.Redemption{
		Key:                      "redemption-terminal-mismatch-success",
		Name:                     "terminal mismatch success",
		Quota:                    int(common.QuotaPerUnit),
		Status:                   common.RedemptionCodeStatusUsed,
		UsedUserId:               invitee.Id,
		RedeemedTime:             now,
		ReferralAffiliateId:      affiliate.Id,
		ReferralRate:             10,
		ReferralBaseAmount:       1,
		ReferralBaseCurrency:     "CNY",
		ReferralCommissionStatus: model.ReferralCommissionJobStatusSucceeded,
		ReferralCommissionAt:     succeededAt,
	}
	require.NoError(t, db.Create(succeededRedemption).Error)
	succeededTradeNo := redemptionCommissionTradeNo(succeededRedemption.Id)
	require.NoError(t, db.Create(&model.ReferralCommissionAccount{
		AffiliateId:   affiliate.Id,
		UserId:        affiliateUser.Id,
		PendingAmount: 0.1,
	}).Error)
	commission := &model.ReferralCommission{
		AffiliateId:          affiliate.Id,
		AffiliateUserId:      affiliateUser.Id,
		InviteeUserId:        invitee.Id,
		SourceType:           "redemption",
		SourceOrderId:        succeededRedemption.Id,
		SourceTradeNo:        succeededTradeNo,
		OrderType:            "redemption",
		BaseAmount:           1,
		PaidAmount:           1,
		PaidCurrency:         "CNY",
		SettlementCurrency:   "CNY",
		SettlementFxRate:     1,
		SettlementBaseAmount: 1,
		Rate:                 10,
		CommissionAmount:     0.1,
		Status:               model.ReferralCommissionStatusPending,
		CreatedAt:            now - 60,
	}
	require.NoError(t, db.Create(commission).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionLedger{
		AffiliateId:        affiliate.Id,
		UserId:             affiliateUser.Id,
		CommissionId:       commission.Id,
		Type:               "commission_accrue",
		RefType:            "redemption",
		RefId:              succeededTradeNo,
		ExternalRefId:      fmt.Sprintf("accrue:redemption:%s", succeededTradeNo),
		SettlementCurrency: "CNY",
		DeltaPending:       0.1,
		Operator:           "system",
		CreatedAt:          succeededAt,
	}).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionJob{
		SourceType:    "redemption",
		SourceTradeNo: succeededTradeNo,
		AffiliateId:   affiliate.Id,
		Status:        model.ReferralCommissionJobStatusSkipped,
		LastError:     "no_binding",
		SucceededAt:   now - 60,
		FailedAt:      now,
		LockedAt:      now,
	}).Error)

	require.NoError(t, service.RetryCommissionJob("redemption", succeededTradeNo))

	succeededJob := &model.ReferralCommissionJob{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", succeededTradeNo).First(succeededJob).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSucceeded, succeededJob.Status)
	require.Empty(t, succeededJob.LastError)
	require.Equal(t, succeededAt, succeededJob.SucceededAt)
	require.Zero(t, succeededJob.FailedAt)
	require.Zero(t, succeededJob.LockedAt)
	require.NoError(t, model.DeleteRedemptionById(succeededRedemption.Id))

	accountedSkippedAt := now - 45
	accountedSkippedRedemption := &model.Redemption{
		Key:                      "redemption-terminal-skipped-with-commission",
		Name:                     "terminal skipped with completed commission",
		Quota:                    int(common.QuotaPerUnit),
		Status:                   common.RedemptionCodeStatusUsed,
		UsedUserId:               invitee.Id,
		RedeemedTime:             now,
		ReferralAffiliateId:      affiliate.Id + 999,
		ReferralRate:             99,
		ReferralBaseAmount:       99,
		ReferralBaseCurrency:     "USD",
		ReferralCommissionStatus: model.ReferralCommissionJobStatusSkipped,
		ReferralCommissionError:  "no_binding",
		ReferralCommissionAt:     accountedSkippedAt,
	}
	require.NoError(t, db.Create(accountedSkippedRedemption).Error)
	accountedSkippedTradeNo := redemptionCommissionTradeNo(accountedSkippedRedemption.Id)
	require.NoError(t, db.Model(&model.ReferralCommissionAccount{}).
		Where("affiliate_id = ?", affiliate.Id).
		Update("pending_amount", 0.2).Error)
	accountedSkippedCommission := &model.ReferralCommission{
		AffiliateId:          affiliate.Id,
		AffiliateUserId:      affiliateUser.Id,
		InviteeUserId:        invitee.Id,
		SourceType:           "redemption",
		SourceOrderId:        accountedSkippedRedemption.Id,
		SourceTradeNo:        accountedSkippedTradeNo,
		OrderType:            "redemption",
		BaseAmount:           1,
		PaidAmount:           1,
		PaidCurrency:         "CNY",
		SettlementCurrency:   "CNY",
		SettlementFxRate:     1,
		SettlementBaseAmount: 1,
		Rate:                 10,
		CommissionAmount:     0.1,
		Status:               model.ReferralCommissionStatusPending,
	}
	require.NoError(t, db.Create(accountedSkippedCommission).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionLedger{
		AffiliateId:        affiliate.Id,
		UserId:             affiliateUser.Id,
		CommissionId:       accountedSkippedCommission.Id,
		Type:               "commission_accrue",
		RefType:            "redemption",
		RefId:              accountedSkippedTradeNo,
		ExternalRefId:      fmt.Sprintf("accrue:redemption:%s", accountedSkippedTradeNo),
		SettlementCurrency: "CNY",
		DeltaPending:       0.1,
		Operator:           "system",
		CreatedAt:          accountedSkippedAt,
	}).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionJob{
		SourceType:    "redemption",
		SourceTradeNo: accountedSkippedTradeNo,
		AffiliateId:   affiliate.Id,
		Status:        model.ReferralCommissionJobStatusSucceeded,
		LastError:     "stale terminal status",
		SucceededAt:   now - 60,
		FailedAt:      now,
		LockedAt:      now,
	}).Error)

	require.NoError(t, service.RetryCommissionJob("redemption", accountedSkippedTradeNo))

	accountedSkippedJob := &model.ReferralCommissionJob{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", accountedSkippedTradeNo).First(accountedSkippedJob).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSucceeded, accountedSkippedJob.Status)
	require.Equal(t, affiliate.Id, accountedSkippedJob.AffiliateId)
	require.Empty(t, accountedSkippedJob.LastError)
	require.Zero(t, accountedSkippedJob.FailedAt)
	require.Zero(t, accountedSkippedJob.LockedAt)
	reloadedAccountedSkipped := &model.Redemption{}
	require.NoError(t, db.Where("id = ?", accountedSkippedRedemption.Id).First(reloadedAccountedSkipped).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSucceeded, reloadedAccountedSkipped.ReferralCommissionStatus)
	require.Empty(t, reloadedAccountedSkipped.ReferralCommissionError)
	require.Equal(t, affiliate.Id, reloadedAccountedSkipped.ReferralAffiliateId)
	require.Equal(t, 10.0, reloadedAccountedSkipped.ReferralRate)
	require.Equal(t, 1.0, reloadedAccountedSkipped.ReferralBaseAmount)
	require.Equal(t, "CNY", reloadedAccountedSkipped.ReferralBaseCurrency)
	require.NoError(t, model.DeleteRedemptionById(accountedSkippedRedemption.Id))

	staleSuccessAt := now - 40
	staleSuccessRedemption := &model.Redemption{
		Key:                      "redemption-terminal-success-stale-snapshot",
		Name:                     "terminal success stale snapshot",
		Quota:                    int(common.QuotaPerUnit),
		Status:                   common.RedemptionCodeStatusUsed,
		UsedUserId:               invitee.Id,
		RedeemedTime:             now,
		ReferralAffiliateId:      affiliate.Id + 999,
		ReferralRate:             99,
		ReferralBaseAmount:       99,
		ReferralBaseCurrency:     "USD",
		ReferralCommissionStatus: model.ReferralCommissionJobStatusSucceeded,
		ReferralCommissionError:  "stale snapshot",
		ReferralCommissionAt:     now - 5,
	}
	require.NoError(t, db.Create(staleSuccessRedemption).Error)
	staleSuccessTradeNo := redemptionCommissionTradeNo(staleSuccessRedemption.Id)
	require.NoError(t, db.Model(&model.ReferralCommissionAccount{}).
		Where("affiliate_id = ?", affiliate.Id).
		Update("pending_amount", 0.3).Error)
	staleSuccessCommission := &model.ReferralCommission{
		AffiliateId:          affiliate.Id,
		AffiliateUserId:      affiliateUser.Id,
		InviteeUserId:        invitee.Id,
		SourceType:           "redemption",
		SourceOrderId:        staleSuccessRedemption.Id,
		SourceTradeNo:        staleSuccessTradeNo,
		OrderType:            "redemption",
		BaseAmount:           1,
		PaidAmount:           1,
		PaidCurrency:         "CNY",
		SettlementCurrency:   "CNY",
		SettlementFxRate:     1,
		SettlementBaseAmount: 1,
		Rate:                 10,
		CommissionAmount:     0.1,
		Status:               model.ReferralCommissionStatusPending,
		CreatedAt:            staleSuccessAt,
	}
	require.NoError(t, db.Create(staleSuccessCommission).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionLedger{
		AffiliateId:        affiliate.Id,
		UserId:             affiliateUser.Id,
		CommissionId:       staleSuccessCommission.Id,
		Type:               "commission_accrue",
		RefType:            "redemption",
		RefId:              staleSuccessTradeNo,
		ExternalRefId:      fmt.Sprintf("accrue:redemption:%s", staleSuccessTradeNo),
		SettlementCurrency: "CNY",
		DeltaPending:       0.1,
		Operator:           "system",
		CreatedAt:          staleSuccessAt,
	}).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionJob{
		SourceType:    "redemption",
		SourceTradeNo: staleSuccessTradeNo,
		AffiliateId:   affiliate.Id + 999,
		Status:        model.ReferralCommissionJobStatusSucceeded,
		LastError:     "stale snapshot",
		SucceededAt:   now - 5,
		FailedAt:      now,
		LockedAt:      now,
	}).Error)

	require.NoError(t, service.RetryCommissionJob("redemption", staleSuccessTradeNo))

	staleSuccessJob := &model.ReferralCommissionJob{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", staleSuccessTradeNo).First(staleSuccessJob).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSucceeded, staleSuccessJob.Status)
	require.Equal(t, affiliate.Id, staleSuccessJob.AffiliateId)
	require.Empty(t, staleSuccessJob.LastError)
	require.Equal(t, staleSuccessAt, staleSuccessJob.SucceededAt)
	require.Zero(t, staleSuccessJob.FailedAt)
	require.Zero(t, staleSuccessJob.LockedAt)
	reloadedStaleSuccess := &model.Redemption{}
	require.NoError(t, db.Where("id = ?", staleSuccessRedemption.Id).First(reloadedStaleSuccess).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSucceeded, reloadedStaleSuccess.ReferralCommissionStatus)
	require.Empty(t, reloadedStaleSuccess.ReferralCommissionError)
	require.Equal(t, affiliate.Id, reloadedStaleSuccess.ReferralAffiliateId)
	require.Equal(t, 10.0, reloadedStaleSuccess.ReferralRate)
	require.Equal(t, 1.0, reloadedStaleSuccess.ReferralBaseAmount)
	require.Equal(t, "CNY", reloadedStaleSuccess.ReferralBaseCurrency)
	require.Equal(t, staleSuccessAt, reloadedStaleSuccess.ReferralCommissionAt)
	require.NoError(t, model.DeleteRedemptionById(staleSuccessRedemption.Id))

	skippedAt := now - 30
	skippedRedemption := &model.Redemption{
		Key:                      "redemption-terminal-mismatch-skipped",
		Name:                     "terminal mismatch skipped",
		Quota:                    int(common.QuotaPerUnit),
		Status:                   common.RedemptionCodeStatusUsed,
		UsedUserId:               invitee.Id,
		RedeemedTime:             now,
		ReferralCommissionStatus: model.ReferralCommissionJobStatusSkipped,
		ReferralCommissionError:  "no_binding",
		ReferralCommissionAt:     skippedAt,
	}
	require.NoError(t, db.Create(skippedRedemption).Error)
	skippedTradeNo := redemptionCommissionTradeNo(skippedRedemption.Id)
	require.NoError(t, db.Create(&model.ReferralCommissionJob{
		SourceType:    "redemption",
		SourceTradeNo: skippedTradeNo,
		AffiliateId:   affiliate.Id,
		Status:        model.ReferralCommissionJobStatusSucceeded,
		LastError:     "stale terminal status",
		SucceededAt:   now - 60,
		FailedAt:      now,
		LockedAt:      now,
	}).Error)

	require.NoError(t, service.RetryCommissionJob("redemption", skippedTradeNo))

	skippedJob := &model.ReferralCommissionJob{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", skippedTradeNo).First(skippedJob).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSkipped, skippedJob.Status)
	require.Equal(t, "no_binding", skippedJob.LastError)
	require.Equal(t, skippedAt, skippedJob.SucceededAt)
	require.Zero(t, skippedJob.AffiliateId)
	require.Zero(t, skippedJob.FailedAt)
	require.Zero(t, skippedJob.LockedAt)
	require.NoError(t, model.DeleteRedemptionById(skippedRedemption.Id))
}

func TestRetryCommissionJobRebuildsIncompleteTerminalRedemptionSuccess(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	invitee := &model.User{Username: "redemption-incomplete-terminal-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	affiliateUser := &model.User{Username: "redemption-incomplete-terminal-affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "TERMJOB2",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, db.Create(affiliate).Error)

	now := time.Now().Unix()
	redemption := &model.Redemption{
		Key:                      "redemption-incomplete-terminal-success",
		Name:                     "incomplete terminal success",
		Quota:                    int(common.QuotaPerUnit),
		Status:                   common.RedemptionCodeStatusUsed,
		UsedUserId:               invitee.Id,
		RedeemedTime:             now,
		ReferralAffiliateId:      affiliate.Id,
		ReferralRate:             10,
		ReferralBaseAmount:       1,
		ReferralBaseCurrency:     "CNY",
		ReferralCommissionStatus: model.ReferralCommissionJobStatusSucceeded,
		ReferralCommissionAt:     now - 60,
	}
	require.NoError(t, db.Create(redemption).Error)
	tradeNo := redemptionCommissionTradeNo(redemption.Id)
	require.NoError(t, db.Create(&model.ReferralCommissionJob{
		SourceType:    "redemption",
		SourceTradeNo: tradeNo,
		AffiliateId:   affiliate.Id,
		Status:        model.ReferralCommissionJobStatusFailed,
		LastError:     "stale failure",
		FailedAt:      now,
		LockedAt:      now,
	}).Error)

	require.NoError(t, service.RetryCommissionJob("redemption", tradeNo))

	job := &model.ReferralCommissionJob{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", tradeNo).First(job).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSucceeded, job.Status)
	require.Equal(t, affiliate.Id, job.AffiliateId)
	require.Empty(t, job.LastError)
	require.NotZero(t, job.SucceededAt)
	require.Zero(t, job.FailedAt)
	require.Zero(t, job.LockedAt)

	commission := &model.ReferralCommission{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", tradeNo).First(commission).Error)
	require.Equal(t, affiliate.Id, commission.AffiliateId)
	require.Equal(t, invitee.Id, commission.InviteeUserId)
	require.Equal(t, 1.0, commission.SettlementBaseAmount)
	require.Equal(t, 0.1, commission.CommissionAmount)

	account := &model.ReferralCommissionAccount{}
	require.NoError(t, db.Where("affiliate_id = ?", affiliate.Id).First(account).Error)
	require.Equal(t, 0.1, account.PendingAmount)

	var ledgers int64
	require.NoError(t, db.Model(&model.ReferralCommissionLedger{}).Where("commission_id = ? AND ref_type = ? AND ref_id = ?", commission.Id, "redemption", tradeNo).Count(&ledgers).Error)
	require.EqualValues(t, 1, ledgers)

	reloadedRedemption := &model.Redemption{}
	require.NoError(t, db.Where("id = ?", redemption.Id).First(reloadedRedemption).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSucceeded, reloadedRedemption.ReferralCommissionStatus)
	require.Empty(t, reloadedRedemption.ReferralCommissionError)
	require.NoError(t, model.DeleteRedemptionById(redemption.Id))
}

func TestRetryCommissionJobSyncsSourceWhenSucceededJobChainIsComplete(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	invitee := &model.User{Username: "redemption-succeeded-job-source-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	affiliateUser := &model.User{Username: "redemption-succeeded-job-source-affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "TERMJOB4",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, db.Create(affiliate).Error)

	now := time.Now().Unix()
	redemption := &model.Redemption{
		Key:                      "redemption-succeeded-job-source-stale",
		Name:                     "stale failed source with succeeded job",
		Quota:                    int(common.QuotaPerUnit),
		Status:                   common.RedemptionCodeStatusUsed,
		UsedUserId:               invitee.Id,
		RedeemedTime:             now,
		ReferralAffiliateId:      affiliate.Id,
		ReferralRate:             10,
		ReferralBaseAmount:       1,
		ReferralBaseCurrency:     "CNY",
		ReferralCommissionStatus: model.ReferralCommissionJobStatusFailed,
		ReferralCommissionError:  "stale source failure",
		ReferralCommissionAt:     now - 120,
	}
	require.NoError(t, db.Create(redemption).Error)
	tradeNo := redemptionCommissionTradeNo(redemption.Id)
	require.NoError(t, db.Create(&model.ReferralCommissionAccount{
		AffiliateId:   affiliate.Id,
		UserId:        affiliateUser.Id,
		PendingAmount: 0.1,
	}).Error)
	commission := &model.ReferralCommission{
		AffiliateId:          affiliate.Id,
		AffiliateUserId:      affiliateUser.Id,
		InviteeUserId:        invitee.Id,
		SourceType:           "redemption",
		SourceOrderId:        redemption.Id,
		SourceTradeNo:        tradeNo,
		OrderType:            "redemption",
		BaseAmount:           1,
		PaidAmount:           1,
		PaidCurrency:         "CNY",
		SettlementCurrency:   "CNY",
		SettlementFxRate:     1,
		SettlementBaseAmount: 1,
		Rate:                 10,
		CommissionAmount:     0.1,
		Status:               model.ReferralCommissionStatusPending,
		CreatedAt:            now - 60,
	}
	require.NoError(t, db.Create(commission).Error)
	completedAt := commission.CreatedAt
	require.NotZero(t, completedAt)
	require.NoError(t, db.Create(&model.ReferralCommissionLedger{
		AffiliateId:        affiliate.Id,
		UserId:             affiliateUser.Id,
		CommissionId:       commission.Id,
		Type:               "commission_accrue",
		RefType:            "redemption",
		RefId:              tradeNo,
		ExternalRefId:      fmt.Sprintf("accrue:redemption:%s", tradeNo),
		SettlementCurrency: "CNY",
		DeltaPending:       0.1,
		Operator:           "system",
		CreatedAt:          completedAt,
	}).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionJob{
		SourceType:    "redemption",
		SourceTradeNo: tradeNo,
		AffiliateId:   affiliate.Id,
		Status:        model.ReferralCommissionJobStatusSucceeded,
		SucceededAt:   completedAt,
	}).Error)

	require.NoError(t, service.RetryCommissionJob("redemption", tradeNo))

	reloadedRedemption := &model.Redemption{}
	require.NoError(t, db.Where("id = ?", redemption.Id).First(reloadedRedemption).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSucceeded, reloadedRedemption.ReferralCommissionStatus)
	require.Empty(t, reloadedRedemption.ReferralCommissionError)
	require.Equal(t, completedAt, reloadedRedemption.ReferralCommissionAt)

	job := &model.ReferralCommissionJob{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", tradeNo).First(job).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSucceeded, job.Status)
	require.NoError(t, model.DeleteRedemptionById(redemption.Id))
}

func TestRetryCommissionJobReconcilesNonTerminalRedemptionSourceWithCompletedCommissionChain(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	invitee := &model.User{Username: "redemption-non-terminal-chain-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	affiliateUser := &model.User{Username: "redemption-non-terminal-chain-affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "NONTERM1",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, db.Create(affiliate).Error)

	now := time.Now().Unix()
	completedAt := now - 90
	redemption := &model.Redemption{
		Key:                      "redemption-non-terminal-complete-chain",
		Name:                     "non terminal source with completed chain",
		Quota:                    int(common.QuotaPerUnit),
		Status:                   common.RedemptionCodeStatusUsed,
		UsedUserId:               invitee.Id,
		RedeemedTime:             now,
		ReferralAffiliateId:      affiliate.Id + 999,
		ReferralRate:             99,
		ReferralBaseAmount:       99,
		ReferralBaseCurrency:     "USD",
		ReferralCommissionStatus: model.ReferralCommissionJobStatusFailed,
		ReferralCommissionError:  "stale failure",
		ReferralCommissionAt:     now - 5,
	}
	require.NoError(t, db.Create(redemption).Error)
	tradeNo := redemptionCommissionTradeNo(redemption.Id)
	require.NoError(t, db.Create(&model.ReferralCommissionAccount{
		AffiliateId:   affiliate.Id,
		UserId:        affiliateUser.Id,
		PendingAmount: 0.1,
	}).Error)
	commission := &model.ReferralCommission{
		AffiliateId:          affiliate.Id,
		AffiliateUserId:      affiliateUser.Id,
		InviteeUserId:        invitee.Id,
		SourceType:           "redemption",
		SourceOrderId:        redemption.Id,
		SourceTradeNo:        tradeNo,
		OrderType:            "redemption",
		BaseAmount:           1,
		PaidAmount:           1,
		PaidCurrency:         "CNY",
		SettlementCurrency:   "CNY",
		SettlementFxRate:     1,
		SettlementBaseAmount: 1,
		Rate:                 10,
		CommissionAmount:     0.1,
		Status:               model.ReferralCommissionStatusPending,
		CreatedAt:            completedAt,
	}
	require.NoError(t, db.Create(commission).Error)
	completedAt = commission.CreatedAt
	require.NotZero(t, completedAt)
	require.NoError(t, db.Create(&model.ReferralCommissionLedger{
		AffiliateId:        affiliate.Id,
		UserId:             affiliateUser.Id,
		CommissionId:       commission.Id,
		Type:               "commission_accrue",
		RefType:            "redemption",
		RefId:              tradeNo,
		ExternalRefId:      fmt.Sprintf("accrue:redemption:%s", tradeNo),
		SettlementCurrency: "CNY",
		DeltaPending:       0.1,
		Operator:           "system",
		CreatedAt:          completedAt,
	}).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionJob{
		SourceType:    "redemption",
		SourceTradeNo: tradeNo,
		AffiliateId:   affiliate.Id + 999,
		Status:        model.ReferralCommissionJobStatusSkipped,
		LastError:     "no_binding",
		SucceededAt:   now - 5,
		FailedAt:      now,
		LockedAt:      now,
	}).Error)

	require.NoError(t, service.RetryCommissionJob("redemption", tradeNo))

	job := &model.ReferralCommissionJob{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", tradeNo).First(job).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSucceeded, job.Status)
	require.Equal(t, affiliate.Id, job.AffiliateId)
	require.Empty(t, job.LastError)
	require.Equal(t, completedAt, job.SucceededAt)
	require.Zero(t, job.FailedAt)
	require.Zero(t, job.LockedAt)

	reloadedRedemption := &model.Redemption{}
	require.NoError(t, db.Where("id = ?", redemption.Id).First(reloadedRedemption).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSucceeded, reloadedRedemption.ReferralCommissionStatus)
	require.Empty(t, reloadedRedemption.ReferralCommissionError)
	require.Equal(t, affiliate.Id, reloadedRedemption.ReferralAffiliateId)
	require.Equal(t, 10.0, reloadedRedemption.ReferralRate)
	require.Equal(t, 1.0, reloadedRedemption.ReferralBaseAmount)
	require.Equal(t, "CNY", reloadedRedemption.ReferralBaseCurrency)
	require.Equal(t, completedAt, reloadedRedemption.ReferralCommissionAt)
	require.NoError(t, model.DeleteRedemptionById(redemption.Id))
}

func TestRetryCommissionJobSyncsSourceWhenSkippedJobIsTerminal(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	invitee := &model.User{Username: "redemption-skipped-job-source-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))

	now := time.Now().Unix()
	redemption := &model.Redemption{
		Key:                      "redemption-skipped-job-source-stale",
		Name:                     "stale failed source with skipped job",
		Quota:                    int(common.QuotaPerUnit),
		Status:                   common.RedemptionCodeStatusUsed,
		UsedUserId:               invitee.Id,
		RedeemedTime:             now,
		ReferralCommissionStatus: model.ReferralCommissionJobStatusFailed,
		ReferralCommissionError:  "stale source failure",
		ReferralCommissionAt:     now - 120,
	}
	require.NoError(t, db.Create(redemption).Error)
	tradeNo := redemptionCommissionTradeNo(redemption.Id)
	require.NoError(t, db.Create(&model.ReferralCommissionJob{
		SourceType:    "redemption",
		SourceTradeNo: tradeNo,
		Status:        model.ReferralCommissionJobStatusSkipped,
		SucceededAt:   now - 60,
	}).Error)

	require.NoError(t, service.RetryCommissionJob("redemption", tradeNo))

	reloadedRedemption := &model.Redemption{}
	require.NoError(t, db.Where("id = ?", redemption.Id).First(reloadedRedemption).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSkipped, reloadedRedemption.ReferralCommissionStatus)
	require.Equal(t, "no_binding", reloadedRedemption.ReferralCommissionError)
	require.Equal(t, now-120, reloadedRedemption.ReferralCommissionAt)

	job := &model.ReferralCommissionJob{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", tradeNo).First(job).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSkipped, job.Status)
	require.Equal(t, "no_binding", job.LastError)
	require.NotZero(t, job.SucceededAt)
	require.NoError(t, model.DeleteRedemptionById(redemption.Id))
}

func TestRetryCommissionJobRejectsSkippedRedemptionSourceWithResidualCommission(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	invitee := &model.User{Username: "redemption-skipped-residual-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	affiliateUser := &model.User{Username: "redemption-skipped-residual-affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "SKIPRES1",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, db.Create(affiliate).Error)

	now := time.Now().Unix()
	redemption := &model.Redemption{
		Key:                      "redemption-skipped-residual-commission",
		Name:                     "skipped source with residual commission",
		Quota:                    int(common.QuotaPerUnit),
		Status:                   common.RedemptionCodeStatusUsed,
		UsedUserId:               invitee.Id,
		RedeemedTime:             now,
		ReferralCommissionStatus: model.ReferralCommissionJobStatusSkipped,
		ReferralCommissionError:  "no_binding",
		ReferralCommissionAt:     now - 120,
	}
	require.NoError(t, db.Create(redemption).Error)
	tradeNo := redemptionCommissionTradeNo(redemption.Id)
	require.NoError(t, db.Create(&model.ReferralCommission{
		AffiliateId:          affiliate.Id,
		AffiliateUserId:      affiliateUser.Id,
		InviteeUserId:        invitee.Id,
		SourceType:           "redemption",
		SourceOrderId:        redemption.Id,
		SourceTradeNo:        tradeNo,
		OrderType:            "redemption",
		BaseAmount:           1,
		PaidAmount:           1,
		PaidCurrency:         "CNY",
		SettlementCurrency:   "CNY",
		SettlementFxRate:     1,
		SettlementBaseAmount: 1,
		Rate:                 10,
		CommissionAmount:     0.1,
		Status:               model.ReferralCommissionStatusPending,
	}).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionJob{
		SourceType:    "redemption",
		SourceTradeNo: tradeNo,
		Status:        model.ReferralCommissionJobStatusSkipped,
		LastError:     "no_binding",
		SucceededAt:   now - 60,
	}).Error)

	require.NoError(t, service.RetryCommissionJob("redemption", tradeNo))

	job := &model.ReferralCommissionJob{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", tradeNo).First(job).Error)
	require.Equal(t, model.ReferralCommissionJobStatusFailed, job.Status)
	require.Equal(t, "redemption_commission_chain_incomplete", job.LastError)
	require.Zero(t, job.SucceededAt)
	require.NotZero(t, job.FailedAt)

	reloadedRedemption := &model.Redemption{}
	require.NoError(t, db.Where("id = ?", redemption.Id).First(reloadedRedemption).Error)
	require.Equal(t, model.ReferralCommissionJobStatusFailed, reloadedRedemption.ReferralCommissionStatus)
	require.Equal(t, "redemption_commission_chain_incomplete", reloadedRedemption.ReferralCommissionError)
	require.ErrorContains(t, model.DeleteRedemptionById(redemption.Id), "unresolved")
}

func TestRetryCommissionJobRejectsExistingRedemptionCommissionWithoutLedger(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	invitee := &model.User{Username: "redemption-existing-commission-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	affiliateUser := &model.User{Username: "redemption-existing-commission-affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "TERMJOB3",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, db.Create(affiliate).Error)

	now := time.Now().Unix()
	redemption := &model.Redemption{
		Key:                      "redemption-existing-commission-no-ledger",
		Name:                     "existing commission without ledger",
		Quota:                    int(common.QuotaPerUnit),
		Status:                   common.RedemptionCodeStatusUsed,
		UsedUserId:               invitee.Id,
		RedeemedTime:             now,
		ReferralAffiliateId:      affiliate.Id,
		ReferralRate:             10,
		ReferralBaseAmount:       1,
		ReferralBaseCurrency:     "CNY",
		ReferralCommissionStatus: model.ReferralCommissionJobStatusFailed,
		ReferralCommissionError:  "stale failure",
		ReferralCommissionAt:     now - 60,
	}
	require.NoError(t, db.Create(redemption).Error)
	tradeNo := redemptionCommissionTradeNo(redemption.Id)
	require.NoError(t, db.Create(&model.ReferralCommissionAccount{
		AffiliateId: affiliate.Id,
		UserId:      affiliateUser.Id,
	}).Error)
	commission := &model.ReferralCommission{
		AffiliateId:          affiliate.Id,
		AffiliateUserId:      affiliateUser.Id,
		InviteeUserId:        invitee.Id,
		SourceType:           "redemption",
		SourceOrderId:        redemption.Id,
		SourceTradeNo:        tradeNo,
		OrderType:            "redemption",
		BaseAmount:           1,
		PaidAmount:           1,
		PaidCurrency:         "CNY",
		SettlementCurrency:   "CNY",
		SettlementFxRate:     1,
		SettlementBaseAmount: 1,
		Rate:                 10,
		CommissionAmount:     0.1,
		Status:               model.ReferralCommissionStatusPending,
	}
	require.NoError(t, db.Create(commission).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionLedger{
		AffiliateId:        affiliate.Id,
		UserId:             affiliateUser.Id,
		CommissionId:       commission.Id,
		Type:               "commission_accrue",
		RefType:            "redemption",
		RefId:              tradeNo,
		ExternalRefId:      fmt.Sprintf("accrue:redemption:%s", tradeNo),
		SettlementCurrency: "CNY",
		DeltaPending:       0.1,
		Operator:           "system",
		CreatedAt:          now,
	}).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionJob{
		SourceType:    "redemption",
		SourceTradeNo: tradeNo,
		AffiliateId:   affiliate.Id,
		Status:        model.ReferralCommissionJobStatusFailed,
		LastError:     "stale failure",
		FailedAt:      now,
	}).Error)

	require.NoError(t, service.RetryCommissionJob("redemption", tradeNo))

	job := &model.ReferralCommissionJob{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", tradeNo).First(job).Error)
	require.Equal(t, model.ReferralCommissionJobStatusFailed, job.Status)
	require.Equal(t, "referral_commission_chain_incomplete", job.LastError)
	require.Zero(t, job.LockedAt)
	require.Zero(t, job.SucceededAt)
	require.NotZero(t, job.FailedAt)

	reloadedRedemption := &model.Redemption{}
	require.NoError(t, db.Where("id = ?", redemption.Id).First(reloadedRedemption).Error)
	require.Equal(t, model.ReferralCommissionJobStatusFailed, reloadedRedemption.ReferralCommissionStatus)
	require.Equal(t, "referral_commission_chain_incomplete", reloadedRedemption.ReferralCommissionError)
	require.ErrorContains(t, model.DeleteRedemptionById(redemption.Id), "unresolved")
}

func TestDeleteInvalidRedemptionsKeepsUnresolvedRedemptionCommissions(t *testing.T) {
	db := setupReferralServiceTestDB(t)

	failedRedemption := &model.Redemption{
		Key:                      "redemption-delete-protect-001",
		Name:                     "failed redemption code",
		Quota:                    int(common.QuotaPerUnit),
		Status:                   common.RedemptionCodeStatusUsed,
		UsedUserId:               1,
		RedeemedTime:             time.Now().Unix(),
		ReferralCommissionStatus: model.ReferralCommissionJobStatusFailed,
		ReferralCommissionError:  "temporary error",
	}
	require.NoError(t, db.Create(failedRedemption).Error)
	unprocessedRedemption := &model.Redemption{
		Key:          "redemption-delete-protect-empty-001",
		Name:         "unprocessed redemption code",
		Quota:        int(common.QuotaPerUnit),
		Status:       common.RedemptionCodeStatusUsed,
		UsedUserId:   3,
		RedeemedTime: time.Now().Unix(),
	}
	require.NoError(t, db.Create(unprocessedRedemption).Error)
	succeededInvitee := &model.User{Username: "redemption-delete-succeeded-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, succeededInvitee.Insert(0))
	succeededAffiliateUser := &model.User{Username: "redemption-delete-succeeded-affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, succeededAffiliateUser.Insert(0))
	succeededAffiliate := &model.ReferralAffiliate{
		UserId:             succeededAffiliateUser.Id,
		InviteCode:         "DELREDM1",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, db.Create(succeededAffiliate).Error)
	succeededRedemption := &model.Redemption{
		Key:                      "redemption-delete-allowed-001",
		Name:                     "succeeded redemption code",
		Quota:                    int(common.QuotaPerUnit),
		Status:                   common.RedemptionCodeStatusUsed,
		UsedUserId:               succeededInvitee.Id,
		RedeemedTime:             time.Now().Unix(),
		ReferralAffiliateId:      succeededAffiliate.Id,
		ReferralCommissionStatus: model.ReferralCommissionJobStatusSucceeded,
	}
	require.NoError(t, db.Create(succeededRedemption).Error)
	succeededTradeNo := redemptionCommissionTradeNo(succeededRedemption.Id)
	require.NoError(t, db.Create(&model.ReferralCommissionAccount{
		AffiliateId:   succeededAffiliate.Id,
		UserId:        succeededAffiliateUser.Id,
		PendingAmount: 0.1,
	}).Error)
	succeededCommission := &model.ReferralCommission{
		AffiliateId:          succeededAffiliate.Id,
		AffiliateUserId:      succeededAffiliateUser.Id,
		InviteeUserId:        succeededInvitee.Id,
		SourceType:           "redemption",
		SourceOrderId:        succeededRedemption.Id,
		SourceTradeNo:        succeededTradeNo,
		OrderType:            "redemption",
		BaseAmount:           1,
		PaidAmount:           1,
		PaidCurrency:         "CNY",
		SettlementCurrency:   "CNY",
		SettlementFxRate:     1,
		SettlementBaseAmount: 1,
		Rate:                 10,
		CommissionAmount:     0.1,
		Status:               model.ReferralCommissionStatusPending,
	}
	require.NoError(t, db.Create(succeededCommission).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionLedger{
		AffiliateId:        succeededAffiliate.Id,
		UserId:             succeededAffiliateUser.Id,
		CommissionId:       succeededCommission.Id,
		Type:               "commission_accrue",
		RefType:            "redemption",
		RefId:              succeededTradeNo,
		ExternalRefId:      fmt.Sprintf("accrue:redemption:%s", succeededTradeNo),
		SettlementCurrency: "CNY",
		DeltaPending:       0.1,
		Operator:           "system",
		CreatedAt:          time.Now().Unix(),
	}).Error)

	rows, err := model.DeleteInvalidRedemptions()
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)

	var kept model.Redemption
	require.NoError(t, db.Where("id = ?", failedRedemption.Id).First(&kept).Error)
	var keptUnprocessed model.Redemption
	require.NoError(t, db.Where("id = ?", unprocessedRedemption.Id).First(&keptUnprocessed).Error)
	var deleted model.Redemption
	require.Error(t, db.Where("id = ?", succeededRedemption.Id).First(&deleted).Error)
}

func TestRetryCommissionJobLoadsSoftDeletedRedemption(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	common.ReferralDefaultRate = 10
	common.ReferralRedemptionUSDToCNYRate = 2

	invitee := &model.User{Username: "redemption-soft-deleted-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	affiliateUser := &model.User{Username: "redemption-soft-deleted-affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "SOFTDEL1",
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

	redemption := &model.Redemption{
		Key:                      "redemption-soft-deleted-key-01",
		Name:                     "soft deleted redemption code",
		Quota:                    int(common.QuotaPerUnit * 10),
		Status:                   common.RedemptionCodeStatusUsed,
		UsedUserId:               invitee.Id,
		RedeemedTime:             time.Now().Unix(),
		ReferralCommissionStatus: model.ReferralCommissionJobStatusFailed,
		ReferralCommissionError:  "temporary error",
	}
	require.NoError(t, db.Create(redemption).Error)
	tradeNo := redemptionCommissionTradeNo(redemption.Id)
	require.NoError(t, db.Create(&model.ReferralCommissionJob{
		SourceType:    "redemption",
		SourceTradeNo: tradeNo,
		AffiliateId:   affiliate.Id,
		Status:        model.ReferralCommissionJobStatusFailed,
		LastError:     "temporary error",
		FailedAt:      time.Now().Unix(),
	}).Error)
	require.NoError(t, db.Delete(redemption).Error)

	require.NoError(t, service.RetryCommissionJob("redemption", tradeNo))

	job := &model.ReferralCommissionJob{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", tradeNo).First(job).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSucceeded, job.Status)

	commission := &model.ReferralCommission{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "redemption", tradeNo).First(commission).Error)
	require.Equal(t, 20.0, commission.SettlementBaseAmount)
	require.Equal(t, 2.0, commission.CommissionAmount)
}

func TestRetryCommissionJobRejectsBareRedemptionId(t *testing.T) {
	setupReferralServiceTestDB(t)
	service := NewReferralService()

	require.ErrorContains(t, service.RetryCommissionJob("redemption", "123"), "invalid redemption trade_no")
}

func TestProcessTopUpCommissionConcurrentCallbacksAreIdempotent(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	service := NewReferralService()
	common.ReferralSettlementFxRates = map[string]float64{"CNY": 1}
	operation_setting.USDExchangeRate = 1

	invitee := &model.User{Username: "concurrent-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	affiliateUser := &model.User{Username: "concurrent-affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))

	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "CONCUR01",
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
		Money:               100,
		PaidAmount:          100,
		PaidCurrency:        "CNY",
		TradeNo:             "topup-concurrent",
		Status:              common.TopUpStatusSuccess,
		ReferralAffiliateId: affiliate.Id,
		ReferralRate:        10,
		ReferralBaseAmount:  100,
		PaymentMethod:       "alipay",
		PaymentProvider:     model.PaymentProviderEpay,
		CreateTime:          time.Now().Unix(),
	}
	require.NoError(t, db.Create(topup).Error)

	const attempts = 50
	errCh := make(chan error, attempts)
	var wg sync.WaitGroup
	wg.Add(attempts)
	for i := 0; i < attempts; i++ {
		go func() {
			defer wg.Done()
			errCh <- service.ProcessTopUpCommission(topup.TradeNo)
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}

	var commissions int64
	require.NoError(t, db.Model(&model.ReferralCommission{}).Where("source_type = ? AND source_trade_no = ?", "topup", topup.TradeNo).Count(&commissions).Error)
	require.EqualValues(t, 1, commissions)

	var ledgers int64
	require.NoError(t, db.Model(&model.ReferralCommissionLedger{}).Where("external_ref_id = ?", "accrue:topup:topup-concurrent").Count(&ledgers).Error)
	require.EqualValues(t, 1, ledgers)

	account := &model.ReferralCommissionAccount{}
	require.NoError(t, db.Where("affiliate_id = ?", affiliate.Id).First(account).Error)
	require.Equal(t, 10.0, account.PendingAmount)
	require.Zero(t, account.AvailableAmount)
	require.Zero(t, account.FrozenAmount)
	require.Zero(t, account.WithdrawnAmount)

	job := &model.ReferralCommissionJob{}
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "topup", topup.TradeNo).First(job).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSucceeded, job.Status)
}

func TestProcessTopUpCommissionSkipsWhenAffiliateDisabledAfterOrderSnapshot(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	invitee := &model.User{Username: "disabled-snapshot-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	affiliateUser := &model.User{Username: "disabled-snapshot-affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))

	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "DISABLED1",
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
		Money:               100,
		PaidAmount:          100,
		PaidCurrency:        "CNY",
		TradeNo:             "topup-disabled-after-snapshot",
		Status:              common.TopUpStatusSuccess,
		ReferralAffiliateId: affiliate.Id,
		ReferralRate:        10,
		ReferralBaseAmount:  100,
		PaymentMethod:       "alipay",
		PaymentProvider:     model.PaymentProviderEpay,
		CreateTime:          time.Now().Unix(),
	}
	require.NoError(t, db.Create(topup).Error)

	require.NoError(t, db.Model(affiliate).Updates(map[string]any{
		"status":              model.ReferralAffiliateStatusDisabled,
		"acquisition_enabled": false,
		"settlement_enabled":  false,
		"withdrawal_enabled":  false,
		"risk_reason":         "order farming",
	}).Error)

	require.NoError(t, service.ProcessTopUpCommission(topup.TradeNo))

	var commissions int64
	require.NoError(t, db.Model(&model.ReferralCommission{}).Where("source_type = ? AND source_trade_no = ?", "topup", topup.TradeNo).Count(&commissions).Error)
	require.Zero(t, commissions)

	account := &model.ReferralCommissionAccount{}
	require.NoError(t, db.Where("affiliate_id = ?", affiliate.Id).First(account).Error)
	require.Zero(t, account.PendingAmount)
	require.Zero(t, account.AvailableAmount)
	require.Zero(t, account.FrozenAmount)

	reloadedTopup := &model.TopUp{}
	require.NoError(t, db.Where("trade_no = ?", topup.TradeNo).First(reloadedTopup).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSkipped, reloadedTopup.ReferralCommissionStatus)
	require.Equal(t, "affiliate_not_eligible", reloadedTopup.ReferralCommissionError)
}

func TestGetOrCreateAccountTxRejectsConflictingUserUniqueIndex(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	user := &model.User{Username: "account-conflict", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, user.Insert(0))

	existing := &model.ReferralCommissionAccount{
		AffiliateId:        101,
		UserId:             user.Id,
		SettlementCurrency: "CNY",
	}
	require.NoError(t, db.Create(existing).Error)

	err := db.Transaction(func(tx *gorm.DB) error {
		account, err := service.getOrCreateAccountTx(tx, 202, user.Id)
		require.Nil(t, account)
		require.Error(t, err)
		require.Contains(t, err.Error(), "referral account uniqueness mismatch")
		return nil
	})
	require.NoError(t, err)

	var accounts int64
	require.NoError(t, db.Model(&model.ReferralCommissionAccount{}).Where("user_id = ?", user.Id).Count(&accounts).Error)
	require.EqualValues(t, 1, accounts)
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

func TestRetryCommissionJobRoutesSyntheticTopUpTradeNoToSubscription(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	invitee := &model.User{Username: "sub-retry-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	affiliateUser := &model.User{Username: "sub-retry-affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))

	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "SUBRETRY",
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

	tradeNo := "same-trade-subscription"
	order := &model.SubscriptionOrder{
		UserId:                   invitee.Id,
		PlanId:                   1,
		Money:                    9.9,
		PaidAmount:               9.9,
		PaidCurrency:             "CNY",
		TradeNo:                  tradeNo,
		PaymentMethod:            model.PaymentProviderEpay,
		PaymentProvider:          model.PaymentProviderEpay,
		Status:                   common.TopUpStatusSuccess,
		ReferralAffiliateId:      affiliate.Id,
		ReferralRate:             10,
		ReferralBaseAmount:       9.9,
		ReferralBaseCurrency:     "CNY",
		ReferralCommissionStatus: model.ReferralCommissionJobStatusFailed,
		ReferralCommissionError:  model.ReferralCommissionErrorFxRateMissing,
		CreateTime:               time.Now().Unix(),
	}
	require.NoError(t, db.Create(order).Error)
	require.NoError(t, db.Create(&model.TopUp{
		UserId:                   invitee.Id,
		Amount:                   0,
		Money:                    order.Money,
		PaidAmount:               order.PaidAmount,
		PaidCurrency:             order.PaidCurrency,
		TradeNo:                  order.TradeNo,
		PaymentMethod:            order.PaymentMethod,
		PaymentProvider:          order.PaymentProvider,
		Status:                   common.TopUpStatusSuccess,
		ReferralAffiliateId:      order.ReferralAffiliateId,
		ReferralRate:             order.ReferralRate,
		ReferralBaseAmount:       order.ReferralBaseAmount,
		ReferralBaseCurrency:     order.ReferralBaseCurrency,
		ReferralCommissionStatus: model.ReferralCommissionJobStatusFailed,
		ReferralCommissionError:  model.ReferralCommissionErrorFxRateMissing,
		CreateTime:               order.CreateTime,
	}).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionJob{
		SourceType:    "topup",
		SourceTradeNo: tradeNo,
		AffiliateId:   affiliate.Id,
		Status:        model.ReferralCommissionJobStatusFailed,
		AttemptCount:  3,
		LastError:     model.ReferralCommissionErrorFxRateMissing,
		FailedAt:      time.Now().Unix(),
	}).Error)

	require.NoError(t, service.RetryCommissionJob("topup", tradeNo))

	var commission model.ReferralCommission
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "subscription", tradeNo).First(&commission).Error)
	require.Equal(t, order.Id, commission.SourceOrderId)
	require.Equal(t, "subscription", commission.OrderType)
	require.InDelta(t, 0.99, commission.CommissionAmount, 0.000001)

	var canonicalJob model.ReferralCommissionJob
	require.NoError(t, db.Where("source_type = ? AND source_trade_no = ?", "subscription", tradeNo).First(&canonicalJob).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSucceeded, canonicalJob.Status)

	var topUpJobCount int64
	require.NoError(t, db.Model(&model.ReferralCommissionJob{}).Where("source_type = ? AND source_trade_no = ?", "topup", tradeNo).Count(&topUpJobCount).Error)
	require.Zero(t, topUpJobCount)

	var reloadedOrder model.SubscriptionOrder
	require.NoError(t, db.Where("trade_no = ?", tradeNo).First(&reloadedOrder).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSucceeded, reloadedOrder.ReferralCommissionStatus)
	require.Empty(t, reloadedOrder.ReferralCommissionError)

	var reloadedTopUp model.TopUp
	require.NoError(t, db.Where("trade_no = ?", tradeNo).First(&reloadedTopUp).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSucceeded, reloadedTopUp.ReferralCommissionStatus)
	require.Empty(t, reloadedTopUp.ReferralCommissionError)
}

func TestListCommissionJobsResolvesRealOrderTypeAndTradeNo(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	invitee := &model.User{Username: "job-list-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	affiliateUser := &model.User{Username: "job-list-affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))

	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "JOBLIST",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, db.Create(affiliate).Error)

	subscriptionTradeNo := "job-list-subscription"
	require.NoError(t, db.Create(&model.SubscriptionOrder{
		UserId:            invitee.Id,
		PlanId:            1,
		Money:             9.9,
		PaidAmount:        9.9,
		PaidCurrency:      "CNY",
		TradeNo:           subscriptionTradeNo,
		PaymentMethod:     model.PaymentProviderEpay,
		PaymentProvider:   model.PaymentProviderEpay,
		Status:            common.TopUpStatusSuccess,
		PlanTitleSnapshot: "测试订阅套餐",
		CreateTime:        time.Now().Unix(),
	}).Error)
	require.NoError(t, db.Create(&model.TopUp{
		UserId:          invitee.Id,
		Money:           9.9,
		PaidAmount:      9.9,
		PaidCurrency:    "CNY",
		TradeNo:         subscriptionTradeNo,
		PaymentMethod:   model.PaymentProviderEpay,
		PaymentProvider: model.PaymentProviderEpay,
		Status:          common.TopUpStatusSuccess,
		CreateTime:      time.Now().Unix(),
	}).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionJob{
		SourceType:    "topup",
		SourceTradeNo: subscriptionTradeNo,
		AffiliateId:   affiliate.Id,
		Status:        model.ReferralCommissionJobStatusFailed,
		LastError:     model.ReferralCommissionErrorFxRateMissing,
	}).Error)

	topupTradeNo := "job-list-topup"
	require.NoError(t, db.Create(&model.TopUp{
		UserId:          invitee.Id,
		Money:           12,
		PaidAmount:      12,
		PaidCurrency:    "CNY",
		TradeNo:         topupTradeNo,
		PaymentMethod:   model.PaymentProviderBEpusdt,
		PaymentProvider: model.PaymentProviderBEpusdt,
		Status:          common.TopUpStatusSuccess,
		CreateTime:      time.Now().Unix(),
	}).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionJob{
		SourceType:    "topup",
		SourceTradeNo: topupTradeNo,
		AffiliateId:   affiliate.Id,
		Status:        model.ReferralCommissionJobStatusFailed,
		LastError:     model.ReferralCommissionErrorFxRateMissing,
	}).Error)

	items, total, err := service.ListCommissionJobs(ReferralListParams{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Equal(t, int64(2), total)

	byTradeNo := make(map[string]ReferralCommissionJobView, len(items))
	for _, item := range items {
		byTradeNo[item.SourceTradeNo] = item
	}

	subscriptionJob := byTradeNo[subscriptionTradeNo]
	require.Equal(t, "topup", subscriptionJob.SourceType)
	require.Equal(t, "subscription", subscriptionJob.OrderType)
	require.Equal(t, "subscription", subscriptionJob.RetrySourceType)
	require.Equal(t, subscriptionTradeNo, subscriptionJob.OrderTradeNo)
	require.True(t, subscriptionJob.OrderExists)
	require.Equal(t, "测试订阅套餐", subscriptionJob.OrderLabel)

	topupJob := byTradeNo[topupTradeNo]
	require.Equal(t, "topup", topupJob.SourceType)
	require.Equal(t, "topup", topupJob.OrderType)
	require.Equal(t, "topup", topupJob.RetrySourceType)
	require.Equal(t, topupTradeNo, topupJob.OrderTradeNo)
	require.True(t, topupJob.OrderExists)
}

func TestBuildOrderSnapshotPreservesAffiliateWhenFxRateMissing(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	operation_setting.USDExchangeRate = 0
	common.ReferralSettlementFxRates = map[string]float64{"CNY": 1}

	invitee := &model.User{Username: "snapshot-fx-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	affiliateUser := &model.User{Username: "snapshot-fx-affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))
	rateOverride := 15.0
	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "SNAPFX01",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
		RateOverride:       &rateOverride,
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
	require.Equal(t, affiliate.Id, snapshot.AffiliateId)
	require.Equal(t, 15.0, snapshot.Rate)
	require.Equal(t, model.ReferralCommissionJobStatusFailed, snapshot.Status)
	require.Equal(t, model.ReferralCommissionErrorFxRateMissing, snapshot.Error)
}

func TestProcessSubscriptionCommissionSyncsSyntheticTopUpState(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	invitee := &model.User{Username: "sub-sync-invitee", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, invitee.Insert(0))
	affiliateUser := &model.User{Username: "sub-sync-affiliate", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, affiliateUser.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             affiliateUser.Id,
		InviteCode:         "SUBSYNC1",
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

	order := &model.SubscriptionOrder{
		UserId:                   invitee.Id,
		PlanId:                   1,
		Money:                    100,
		PaidAmount:               100,
		PaidCurrency:             "CNY",
		TradeNo:                  "sub-sync-order",
		PaymentMethod:            model.PaymentProviderEpay,
		PaymentProvider:          model.PaymentProviderEpay,
		Status:                   common.TopUpStatusSuccess,
		ReferralAffiliateId:      affiliate.Id,
		ReferralRate:             20,
		ReferralBaseAmount:       100,
		ReferralBaseCurrency:     "CNY",
		ReferralCommissionStatus: model.ReferralCommissionJobStatusPending,
		CreateTime:               time.Now().Unix(),
	}
	require.NoError(t, db.Create(order).Error)
	require.NoError(t, db.Create(&model.TopUp{
		UserId:                   invitee.Id,
		Amount:                   0,
		Money:                    order.Money,
		PaidAmount:               order.PaidAmount,
		PaidCurrency:             order.PaidCurrency,
		TradeNo:                  order.TradeNo,
		PaymentMethod:            order.PaymentMethod,
		PaymentProvider:          order.PaymentProvider,
		Status:                   common.TopUpStatusSuccess,
		ReferralAffiliateId:      order.ReferralAffiliateId,
		ReferralRate:             order.ReferralRate,
		ReferralBaseAmount:       order.ReferralBaseAmount,
		ReferralBaseCurrency:     order.ReferralBaseCurrency,
		ReferralCommissionStatus: model.ReferralCommissionJobStatusPending,
		CreateTime:               order.CreateTime,
	}).Error)

	require.NoError(t, service.ProcessSubscriptionCommission(order.TradeNo))

	var topUp model.TopUp
	require.NoError(t, db.Where("trade_no = ?", order.TradeNo).First(&topUp).Error)
	require.Equal(t, model.ReferralCommissionJobStatusSucceeded, topUp.ReferralCommissionStatus)
	require.Empty(t, topUp.ReferralCommissionError)
	require.NotZero(t, topUp.ReferralCommissionAt)
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

func TestReferralAdminBadgeCursorTracksReappliedAffiliate(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()
	common.ReferralRequireApproval = true

	lowUser := &model.User{Username: "reapply-low", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, lowUser.Insert(0))
	highUser := &model.User{Username: "reapply-high", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, highUser.Insert(0))

	_, err := service.ApplyAffiliate(ReferralApplyInput{UserId: lowUser.Id})
	require.NoError(t, err)
	_, err = service.ApplyAffiliate(ReferralApplyInput{UserId: highUser.Id})
	require.NoError(t, err)
	var lowAffiliate model.ReferralAffiliate
	require.NoError(t, db.Where("user_id = ?", lowUser.Id).First(&lowAffiliate).Error)
	var highAffiliate model.ReferralAffiliate
	require.NoError(t, db.Where("user_id = ?", highUser.Id).First(&highAffiliate).Error)

	before, err := service.GetAdminBadgeCounts()
	require.NoError(t, err)
	require.Equal(t, highAffiliate.Id, before.LatestPendingAffiliateID)
	require.NotEmpty(t, before.LatestPendingAffiliateCursor)

	require.NoError(t, db.Model(&model.ReferralAffiliate{}).
		Where("user_id = ?", lowUser.Id).
		Updates(map[string]any{
			"status": model.ReferralAffiliateStatusRejected,
		}).Error)
	_, err = service.ApplyAffiliate(ReferralApplyInput{UserId: lowUser.Id})
	require.NoError(t, err)

	after, err := service.GetAdminBadgeCounts()
	require.NoError(t, err)
	require.Equal(t, int64(2), after.PendingAffiliates)
	require.Equal(t, lowAffiliate.Id, after.LatestPendingAffiliateID)
	require.Contains(t, after.LatestPendingAffiliateCursor, ":")
	beforeCursor, err := strconv.ParseInt(strings.Split(before.LatestPendingAffiliateCursor, ":")[0], 10, 64)
	require.NoError(t, err)
	afterCursor, err := strconv.ParseInt(strings.Split(after.LatestPendingAffiliateCursor, ":")[0], 10, 64)
	require.NoError(t, err)
	require.Greater(t, afterCursor, beforeCursor)
}

func TestPendingAffiliateBadgeCursorRowFallsBackWithinSummaryCursor(t *testing.T) {
	db := setupReferralServiceTestDB(t)

	newer := &model.ReferralAffiliate{
		UserId:              1,
		InviteCode:          "NEWCUR01",
		Status:              model.ReferralAffiliateStatusApproved,
		PendingReviewCursor: 300,
	}
	require.NoError(t, db.Create(newer).Error)
	older := &model.ReferralAffiliate{
		UserId:              2,
		InviteCode:          "OLDCUR01",
		Status:              model.ReferralAffiliateStatusPending,
		PendingReviewCursor: 200,
	}
	require.NoError(t, db.Create(older).Error)

	row, err := pendingAffiliateBadgeCursorRow(300)
	require.NoError(t, err)
	require.Equal(t, older.Id, row.ID)
	require.Equal(t, int64(200), row.CursorValue)

	require.NoError(t, db.Model(&model.ReferralAffiliate{}).
		Where("id = ?", older.Id).
		Update("status", model.ReferralAffiliateStatusRejected).Error)
	row, err = pendingAffiliateBadgeCursorRow(300)
	require.NoError(t, err)
	require.Zero(t, row.ID)
	require.Zero(t, row.CursorValue)
}

func TestPendingWithdrawalBadgeCursorRowFallsBackWithinSummaryCursor(t *testing.T) {
	db := setupReferralServiceTestDB(t)

	older := &model.ReferralWithdrawal{
		AffiliateId: 1,
		UserId:      1,
		Status:      model.ReferralWithdrawalStatusPending,
		Amount:      10,
		NetAmount:   10,
	}
	require.NoError(t, db.Create(older).Error)
	newer := &model.ReferralWithdrawal{
		AffiliateId: 2,
		UserId:      2,
		Status:      model.ReferralWithdrawalStatusApproved,
		Amount:      20,
		NetAmount:   20,
	}
	require.NoError(t, db.Create(newer).Error)

	row, err := pendingWithdrawalBadgeCursorRow(int64(newer.Id))
	require.NoError(t, err)
	require.Equal(t, older.Id, row.ID)
	require.Equal(t, int64(older.Id), row.CursorValue)

	require.NoError(t, db.Model(&model.ReferralWithdrawal{}).
		Where("id = ?", older.Id).
		Update("status", model.ReferralWithdrawalStatusRejected).Error)
	row, err = pendingWithdrawalBadgeCursorRow(int64(newer.Id))
	require.NoError(t, err)
	require.Zero(t, row.ID)
	require.Zero(t, row.CursorValue)
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

	aliasURL := service.SignAssetAliasURL(1)
	require.Contains(t, aliasURL, "/referral-assets/a/1")

	view, err := service.CreateWithdrawal(ReferralWithdrawalCreateInput{
		UserId:         affiliateUser.Id,
		Amount:         10,
		AccountType:    "alipay",
		AccountName:    "tester",
		AccountNo:      "abc123",
		QRImageURL:     aliasURL,
		IdempotencyKey: "wd-strip-signature",
	})
	require.NoError(t, err)
	require.NotNil(t, view)
	require.Contains(t, stripAssetSignature(view.QRImageURL), "/referral-assets/a/")

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

func TestCreateWithdrawalUSDTIdempotencyUsesCanonicalPayload(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	user := &model.User{Username: "wd-usdt-idem", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, user.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             user.Id,
		InviteCode:         "WDUIDEM1",
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

	first, err := service.CreateWithdrawal(ReferralWithdrawalCreateInput{
		UserId:         user.Id,
		Amount:         10,
		AccountType:    "usdt",
		AccountName:    "ignored name",
		AccountNo:      "TTESTADDRESS",
		AccountNetwork: "trc20",
		IdempotencyKey: "wd-usdt-idem",
	})
	require.NoError(t, err)

	second, err := service.CreateWithdrawal(ReferralWithdrawalCreateInput{
		UserId:         user.Id,
		Amount:         10,
		AccountType:    "USDT",
		AccountName:    "different ignored name",
		AccountNo:      "TTESTADDRESS",
		AccountNetwork: "TRC20",
		IdempotencyKey: "wd-usdt-idem",
	})
	require.NoError(t, err)
	require.Equal(t, first.Id, second.Id)

	account := &model.ReferralCommissionAccount{}
	require.NoError(t, db.Where("affiliate_id = ?", affiliate.Id).First(account).Error)
	require.Equal(t, 90.0, account.AvailableAmount)
	require.Equal(t, 10.0, account.FrozenAmount)

	var count int64
	require.NoError(t, db.Model(&model.ReferralWithdrawal{}).Where("idempotency_key = ?", "wd-usdt-idem").Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestCreateWithdrawalFreezesAccountAndAllocatesCommission(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	user := &model.User{Username: "withdraw-freeze", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, user.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             user.Id,
		InviteCode:         "WDFRZ001",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, db.Create(affiliate).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionAccount{
		AffiliateId:        affiliate.Id,
		UserId:             user.Id,
		AvailableAmount:    100,
		SettlementCurrency: "CNY",
	}).Error)
	commission := &model.ReferralCommission{
		AffiliateId:        affiliate.Id,
		AffiliateUserId:    user.Id,
		InviteeUserId:      user.Id + 10,
		SourceType:         "topup",
		SourceTradeNo:      "withdraw-freeze-trade",
		OrderType:          "topup",
		BaseAmount:         100,
		PaidAmount:         100,
		PaidCurrency:       "CNY",
		SettlementCurrency: "CNY",
		SettlementFxRate:   1,
		Rate:               10,
		CommissionAmount:   100,
		Status:             model.ReferralCommissionStatusAvailable,
		AvailableAt:        time.Now().Unix(),
	}
	require.NoError(t, db.Create(commission).Error)

	view, err := service.CreateWithdrawal(ReferralWithdrawalCreateInput{
		UserId:         user.Id,
		Amount:         40,
		AccountType:    "alipay",
		AccountName:    "tester",
		AccountNo:      "acct-freeze",
		IdempotencyKey: "wd-freeze-key",
	})
	require.NoError(t, err)
	require.Equal(t, model.ReferralWithdrawalStatusPending, view.Status)

	account := &model.ReferralCommissionAccount{}
	require.NoError(t, db.Where("affiliate_id = ?", affiliate.Id).First(account).Error)
	require.Equal(t, 60.0, account.AvailableAmount)
	require.Equal(t, 40.0, account.FrozenAmount)

	item := &model.ReferralWithdrawalItem{}
	require.NoError(t, db.Where("withdrawal_id = ? AND commission_id = ?", view.Id, commission.Id).First(item).Error)
	require.Equal(t, 40.0, item.AllocatedAmount)
	require.Equal(t, model.ReferralWithdrawalItemStatusFrozen, item.Status)

	ledger := &model.ReferralCommissionLedger{}
	require.NoError(t, db.Where("external_ref_id = ?", "withdrawal_freeze:wd-freeze-key").First(ledger).Error)
	require.Equal(t, -40.0, ledger.DeltaAvailable)
	require.Equal(t, 40.0, ledger.DeltaFrozen)
}

func TestRejectWithdrawalReleasesFrozenBalanceAndItems(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	user := &model.User{Username: "withdraw-reject", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, user.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             user.Id,
		InviteCode:         "WDREJ001",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, db.Create(affiliate).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionAccount{
		AffiliateId:     affiliate.Id,
		UserId:          user.Id,
		FrozenAmount:    30,
		AvailableAmount: 70,
	}).Error)
	commission := &model.ReferralCommission{
		AffiliateId:        affiliate.Id,
		AffiliateUserId:    user.Id,
		InviteeUserId:      user.Id + 20,
		SourceType:         "topup",
		SourceTradeNo:      "withdraw-reject-trade",
		OrderType:          "topup",
		BaseAmount:         100,
		PaidAmount:         100,
		PaidCurrency:       "CNY",
		SettlementCurrency: "CNY",
		SettlementFxRate:   1,
		Rate:               10,
		CommissionAmount:   100,
		Status:             model.ReferralCommissionStatusFrozen,
		AvailableAt:        time.Now().Unix(),
		FrozenAt:           time.Now().Unix(),
	}
	require.NoError(t, db.Create(commission).Error)
	withdrawal := &model.ReferralWithdrawal{
		AffiliateId:        affiliate.Id,
		UserId:             user.Id,
		SettlementCurrency: "CNY",
		Amount:             30,
		NetAmount:          30,
		AccountType:        "alipay",
		AccountName:        "tester",
		AccountNo:          "acct-reject",
		Status:             model.ReferralWithdrawalStatusPending,
		SubmittedAt:        time.Now().Unix(),
	}
	require.NoError(t, db.Create(withdrawal).Error)
	require.NoError(t, db.Create(&model.ReferralWithdrawalItem{
		WithdrawalId:    withdrawal.Id,
		CommissionId:    commission.Id,
		AllocatedAmount: 30,
		Status:          model.ReferralWithdrawalItemStatusFrozen,
	}).Error)

	view, err := service.RejectWithdrawal(ReferralWithdrawalReviewInput{
		WithdrawalId: withdrawal.Id,
		AdminUserId:  100,
		RejectReason: "invalid account",
	})
	require.NoError(t, err)
	require.Equal(t, model.ReferralWithdrawalStatusRejected, view.Status)

	account := &model.ReferralCommissionAccount{}
	require.NoError(t, db.Where("affiliate_id = ?", affiliate.Id).First(account).Error)
	require.Equal(t, 100.0, account.AvailableAmount)
	require.Zero(t, account.FrozenAmount)

	item := &model.ReferralWithdrawalItem{}
	require.NoError(t, db.Where("withdrawal_id = ?", withdrawal.Id).First(item).Error)
	require.Equal(t, model.ReferralWithdrawalItemStatusReleased, item.Status)
	require.Zero(t, item.AllocatedAmount)

	reloadedCommission := &model.ReferralCommission{}
	require.NoError(t, db.First(reloadedCommission, commission.Id).Error)
	require.Equal(t, model.ReferralCommissionStatusAvailable, reloadedCommission.Status)
}

func TestMarkWithdrawalPaidMovesFrozenToWithdrawnAndMarksItems(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	user := &model.User{Username: "withdraw-paid-flow", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, user.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             user.Id,
		InviteCode:         "WDPAID01",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, db.Create(affiliate).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionAccount{
		AffiliateId:  affiliate.Id,
		UserId:       user.Id,
		FrozenAmount: 25,
	}).Error)
	commission := &model.ReferralCommission{
		AffiliateId:        affiliate.Id,
		AffiliateUserId:    user.Id,
		InviteeUserId:      user.Id + 30,
		SourceType:         "topup",
		SourceTradeNo:      "withdraw-paid-flow-trade",
		OrderType:          "topup",
		BaseAmount:         100,
		PaidAmount:         100,
		PaidCurrency:       "CNY",
		SettlementCurrency: "CNY",
		SettlementFxRate:   1,
		Rate:               10,
		CommissionAmount:   25,
		Status:             model.ReferralCommissionStatusFrozen,
		AvailableAt:        time.Now().Unix(),
		FrozenAt:           time.Now().Unix(),
	}
	require.NoError(t, db.Create(commission).Error)
	withdrawal := &model.ReferralWithdrawal{
		AffiliateId:        affiliate.Id,
		UserId:             user.Id,
		SettlementCurrency: "CNY",
		Amount:             25,
		NetAmount:          25,
		AccountType:        "alipay",
		AccountName:        "tester",
		AccountNo:          "acct-paid",
		Status:             model.ReferralWithdrawalStatusApproved,
		SubmittedAt:        time.Now().Unix(),
		ApprovedAt:         time.Now().Unix(),
	}
	require.NoError(t, db.Create(withdrawal).Error)
	require.NoError(t, db.Create(&model.ReferralWithdrawalItem{
		WithdrawalId:    withdrawal.Id,
		CommissionId:    commission.Id,
		AllocatedAmount: 25,
		Status:          model.ReferralWithdrawalItemStatusFrozen,
	}).Error)

	view, err := service.MarkWithdrawalPaid(ReferralWithdrawalPayInput{
		WithdrawalId: withdrawal.Id,
		AdminUserId:  100,
		PaymentTxnNo: "txn-paid-flow",
	})
	require.NoError(t, err)
	require.Equal(t, model.ReferralWithdrawalStatusPaid, view.Status)

	account := &model.ReferralCommissionAccount{}
	require.NoError(t, db.Where("affiliate_id = ?", affiliate.Id).First(account).Error)
	require.Zero(t, account.FrozenAmount)
	require.Equal(t, 25.0, account.WithdrawnAmount)

	item := &model.ReferralWithdrawalItem{}
	require.NoError(t, db.Where("withdrawal_id = ?", withdrawal.Id).First(item).Error)
	require.Equal(t, model.ReferralWithdrawalItemStatusWithdrawn, item.Status)

	reloadedCommission := &model.ReferralCommission{}
	require.NoError(t, db.First(reloadedCommission, commission.Id).Error)
	require.Equal(t, model.ReferralCommissionStatusPaid, reloadedCommission.Status)
}

func TestCancelWithdrawalWithinThirtyMinutesReleasesFrozenBalanceAndItems(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	user := &model.User{Username: "withdraw-cancel", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, user.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             user.Id,
		InviteCode:         "WDCAN001",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, db.Create(affiliate).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionAccount{
		AffiliateId:     affiliate.Id,
		UserId:          user.Id,
		FrozenAmount:    20,
		AvailableAmount: 30,
	}).Error)
	commission := &model.ReferralCommission{
		AffiliateId:        affiliate.Id,
		AffiliateUserId:    user.Id,
		InviteeUserId:      user.Id + 10,
		SourceType:         "topup",
		SourceTradeNo:      "withdraw-cancel-trade",
		OrderType:          "topup",
		BaseAmount:         100,
		PaidAmount:         100,
		PaidCurrency:       "CNY",
		SettlementCurrency: "CNY",
		SettlementFxRate:   1,
		Rate:               10,
		CommissionAmount:   20,
		Status:             model.ReferralCommissionStatusFrozen,
		AvailableAt:        time.Now().Unix(),
		FrozenAt:           time.Now().Unix(),
	}
	require.NoError(t, db.Create(commission).Error)
	withdrawal := &model.ReferralWithdrawal{
		AffiliateId: affiliate.Id,
		UserId:      user.Id,
		Amount:      20,
		NetAmount:   20,
		AccountType: "alipay",
		AccountName: "tester",
		AccountNo:   "cancel-acct",
		Status:      model.ReferralWithdrawalStatusPending,
		SubmittedAt: time.Now().Unix(),
	}
	require.NoError(t, db.Create(withdrawal).Error)
	require.NoError(t, db.Create(&model.ReferralWithdrawalItem{
		WithdrawalId:    withdrawal.Id,
		CommissionId:    commission.Id,
		AllocatedAmount: 20,
		Status:          model.ReferralWithdrawalItemStatusFrozen,
	}).Error)

	view, err := service.CancelWithdrawal(withdrawal.Id, user.Id)
	require.NoError(t, err)
	require.Equal(t, model.ReferralWithdrawalStatusCanceled, view.Status)

	account := &model.ReferralCommissionAccount{}
	require.NoError(t, db.Where("affiliate_id = ?", affiliate.Id).First(account).Error)
	require.Equal(t, 50.0, account.AvailableAmount)
	require.Zero(t, account.FrozenAmount)

	item := &model.ReferralWithdrawalItem{}
	require.NoError(t, db.Where("withdrawal_id = ?", withdrawal.Id).First(item).Error)
	require.Equal(t, model.ReferralWithdrawalItemStatusReleased, item.Status)
	require.Zero(t, item.AllocatedAmount)
}

func TestCancelWithdrawalAfterThirtyMinutesRejected(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	user := &model.User{Username: "withdraw-cancel-expired", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, user.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             user.Id,
		InviteCode:         "WDCANEX1",
		Status:             model.ReferralAffiliateStatusApproved,
		AcquisitionEnabled: true,
		SettlementEnabled:  true,
		WithdrawalEnabled:  true,
	}
	require.NoError(t, db.Create(affiliate).Error)
	require.NoError(t, db.Create(&model.ReferralCommissionAccount{AffiliateId: affiliate.Id, UserId: user.Id, FrozenAmount: 10}).Error)
	withdrawal := &model.ReferralWithdrawal{
		AffiliateId: affiliate.Id,
		UserId:      user.Id,
		Amount:      10,
		NetAmount:   10,
		AccountType: "alipay",
		AccountName: "tester",
		AccountNo:   "expired-acct",
		Status:      model.ReferralWithdrawalStatusPending,
		SubmittedAt: time.Now().Add(-31 * time.Minute).Unix(),
	}
	require.NoError(t, db.Create(withdrawal).Error)

	_, err := service.CancelWithdrawal(withdrawal.Id, user.Id)
	require.ErrorContains(t, err, "within 30 minutes")
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

func TestWithdrawalViewIncludesFullAccountNumberAndMaskedCopy(t *testing.T) {
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
	require.Equal(t, "acct1234567890", userView.AccountNo)
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

func TestUpdateSettingsForcesFixedRedirectPath(t *testing.T) {
	setupReferralServiceTestDB(t)
	service := NewReferralService()

	settings, err := service.UpdateSettings(ReferralSettings{
		Enabled:                true,
		CookieTTLDays:          30,
		DefaultRate:            10,
		SettleFreezeDays:       7,
		MinWithdrawAmount:      1,
		WithdrawFee:            0,
		RedirectPath:           "//evil.com",
		RequireApproval:        true,
		SettlementCurrency:     "CNY",
		SettlementFxRates:      `{"CNY":1}`,
		RedemptionUSDToCNYRate: 6.8,
	}, 1, "127.0.0.1", "unit-test")
	require.NoError(t, err)
	require.Equal(t, "/sign-up", settings.RedirectPath)
	require.Equal(t, 6.8, settings.RedemptionUSDToCNYRate)
}

func TestUpdateSettingsNormalizesReferralFxRates(t *testing.T) {
	setupReferralServiceTestDB(t)
	service := NewReferralService()

	settings, err := service.UpdateSettings(ReferralSettings{
		Enabled:                true,
		CookieTTLDays:          30,
		DefaultRate:            10,
		SettleFreezeDays:       7,
		MinWithdrawAmount:      1,
		WithdrawFee:            0,
		RedirectPath:           "/sign-up",
		RequireApproval:        true,
		SettlementCurrency:     "CNY",
		SettlementFxRates:      `{"usd":7.1,"EUR":8}`,
		RedemptionUSDToCNYRate: 1,
	}, 1, "127.0.0.1", "unit-test")
	require.NoError(t, err)
	require.Equal(t, "CNY", settings.SettlementCurrency)
	require.JSONEq(t, `{"CNY":1,"USD":7.1,"EUR":8}`, settings.SettlementFxRates)
}

func TestUpdateSettingsRejectsUnsupportedSettlementCurrency(t *testing.T) {
	setupReferralServiceTestDB(t)
	service := NewReferralService()

	_, err := service.UpdateSettings(ReferralSettings{
		Enabled:                true,
		CookieTTLDays:          30,
		DefaultRate:            10,
		SettleFreezeDays:       7,
		MinWithdrawAmount:      1,
		WithdrawFee:            0,
		RedirectPath:           "/sign-up",
		RequireApproval:        true,
		SettlementCurrency:     "USD",
		SettlementFxRates:      `{"USD":7.1}`,
		RedemptionUSDToCNYRate: 1,
	}, 1, "127.0.0.1", "unit-test")
	require.Error(t, err)
	require.Contains(t, err.Error(), "settlement_currency")
}

func TestUpdateSettingsPreservesFxRatesWhenOmitted(t *testing.T) {
	setupReferralServiceTestDB(t)
	service := NewReferralService()

	common.ReferralSettlementFxRates = map[string]float64{"CNY": 1, "USD": 7.3}

	settings, err := service.UpdateSettings(ReferralSettings{
		Enabled:                true,
		CookieTTLDays:          30,
		DefaultRate:            10,
		SettleFreezeDays:       7,
		MinWithdrawAmount:      1,
		WithdrawFee:            0,
		RedirectPath:           "/sign-up",
		RequireApproval:        true,
		SettlementCurrency:     "CNY",
		RedemptionUSDToCNYRate: 1,
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

func TestMarkWithdrawalPaidAllowsEmptyTxnAndProof(t *testing.T) {
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

	view, err := service.MarkWithdrawalPaid(ReferralWithdrawalPayInput{
		WithdrawalId: 1,
		AdminUserId:  100,
		AdminNote:    "offline paid",
	})
	require.NoError(t, err)
	require.NotNil(t, view)
	require.Equal(t, model.ReferralWithdrawalStatusPaid, view.Status)
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

func TestRejectApprovedWithdrawalStoresOptionalProofAndReleasesFrozen(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	user := &model.User{Username: "withdraw-reject-proof", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, user.Insert(0))
	affiliate := &model.ReferralAffiliate{
		UserId:             user.Id,
		InviteCode:         "RJPROOF1",
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
		StoragePath: "/referral-assets/reject-proof.png",
		ContentType: "image/png",
		Size:        123,
		CreatedBy:   "admin",
		CreatedAt:   time.Now().Unix(),
	}).Error)
	commission := &model.ReferralCommission{
		AffiliateId:        affiliate.Id,
		AffiliateUserId:    user.Id,
		InviteeUserId:      user.Id + 40,
		SourceType:         "topup",
		SourceTradeNo:      "withdraw-reject-proof-trade",
		OrderType:          "topup",
		BaseAmount:         100,
		PaidAmount:         100,
		PaidCurrency:       "CNY",
		SettlementCurrency: "CNY",
		SettlementFxRate:   1,
		Rate:               10,
		CommissionAmount:   10,
		Status:             model.ReferralCommissionStatusFrozen,
		AvailableAt:        time.Now().Unix(),
		FrozenAt:           time.Now().Unix(),
	}
	require.NoError(t, db.Create(commission).Error)
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
	require.NoError(t, db.Create(&model.ReferralWithdrawalItem{
		WithdrawalId:    withdrawal.Id,
		CommissionId:    commission.Id,
		AllocatedAmount: 10,
		Status:          model.ReferralWithdrawalItemStatusFrozen,
	}).Error)

	view, err := service.RejectWithdrawal(ReferralWithdrawalReviewInput{
		WithdrawalId:   withdrawal.Id,
		AdminUserId:    100,
		RejectReason:   "risk account",
		RejectProofURL: service.SignAssetAliasURL(1),
	})
	require.NoError(t, err)
	require.Equal(t, model.ReferralWithdrawalStatusRejected, view.Status)
	require.Contains(t, view.RejectProofURL, "/referral-assets/a/")

	stored := &model.ReferralWithdrawal{}
	require.NoError(t, db.First(stored, withdrawal.Id).Error)
	require.Equal(t, "/referral-assets/reject-proof.png", stored.RejectProofURL)

	account := &model.ReferralCommissionAccount{}
	require.NoError(t, db.Where("affiliate_id = ?", affiliate.Id).First(account).Error)
	require.Equal(t, 10.0, account.AvailableAmount)
	require.Zero(t, account.FrozenAmount)
}

func TestApproveAffiliateCreatesAuditLogWhenAdminCreatesAffiliate(t *testing.T) {
	db := setupReferralServiceTestDB(t)
	service := NewReferralService()

	user := &model.User{Username: "approve-created", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, user.Insert(0))

	view, err := service.ApproveAffiliate(user.Id, 9001, nil, "manual enable", "127.0.0.1", "unit-test")
	require.NoError(t, err)
	require.NotNil(t, view)
	require.Equal(t, model.ReferralAffiliateStatusApproved, view.Status)

	var audit model.ReferralAdminAuditLog
	require.NoError(t, db.Where("action = ? AND target_user_id = ?", "referral_affiliate_approve", user.Id).First(&audit).Error)
	require.Equal(t, 9001, audit.AdminUserId)
	require.Equal(t, "manual enable", audit.Reason)
	require.Equal(t, "127.0.0.1", audit.Ip)
	require.Equal(t, "unit-test", audit.UserAgent)
}
