package service

import (
	"bytes"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBillingSessionShouldTrustUsesConfiguredTrustQuota(t *testing.T) {
	oldTrustQuota := common.TrustQuota
	t.Cleanup(func() {
		common.TrustQuota = oldTrustQuota
	})

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("token_quota", 101)
	session := &BillingSession{
		relayInfo: &relaycommon.RelayInfo{
			UserId:    1,
			UserQuota: 101,
		},
		funding: &WalletFunding{userId: 1},
	}

	common.TrustQuota = 100
	require.True(t, session.shouldTrust(ctx, 50))

	common.TrustQuota = 0
	require.False(t, session.shouldTrust(ctx, 50))

	common.TrustQuota = 200
	require.False(t, session.shouldTrust(ctx, 50))
}

func TestBillingSessionPreConsumeDoesNotTrustWhenRequiredQuotaExceedsAvailableQuota(t *testing.T) {
	truncate(t)
	oldTrustQuota := common.TrustQuota
	t.Cleanup(func() {
		common.TrustQuota = oldTrustQuota
	})
	require.NoError(t, model.DB.Create(&model.User{
		Id:       9601,
		Username: "trust-owner",
		Password: "password123",
		Quota:    150,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, model.DB.Create(&model.Token{
		Id:          9602,
		UserId:      9601,
		Key:         "trust-token",
		Name:        "trust-token",
		RemainQuota: 150,
		Status:      common.TokenStatusEnabled,
	}).Error)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("token_quota", 150)
	session := &BillingSession{
		relayInfo: &relaycommon.RelayInfo{
			UserId:    9601,
			UserQuota: 150,
			TokenId:   9602,
			TokenKey:  "trust-token",
		},
		funding: &WalletFunding{userId: 9601},
	}

	common.TrustQuota = 100
	err := session.preConsume(ctx, 200)

	require.NotNil(t, err)
	require.False(t, session.trusted)
}

type failingSettlementFunding struct {
	err error
}

func (f *failingSettlementFunding) Source() string { return BillingSourceWallet }
func (f *failingSettlementFunding) PreConsume(amount int) error {
	return nil
}
func (f *failingSettlementFunding) Settle(delta int) error {
	return f.err
}
func (f *failingSettlementFunding) Refund() error {
	return nil
}

type failingRefundFunding struct {
	err    error
	called chan struct{}
}

func (f *failingRefundFunding) Source() string { return BillingSourceWallet }
func (f *failingRefundFunding) PreConsume(amount int) error {
	return nil
}
func (f *failingRefundFunding) Settle(delta int) error {
	return nil
}
func (f *failingRefundFunding) Refund() error {
	close(f.called)
	return f.err
}

func TestBillingSessionSettleLogsErrorWhenFundingFails(t *testing.T) {
	var output bytes.Buffer
	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultWriter
	gin.DefaultWriter = &output
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultWriter = oldWriter
		common.LogWriterMu.Unlock()
	})

	session := &BillingSession{
		relayInfo: &relaycommon.RelayInfo{
			UserId:       1,
			TokenId:      2,
			RequestId:    "settle-request",
			IsPlayground: true,
		},
		funding:          &failingSettlementFunding{err: errors.New("settlement failed")},
		preConsumedQuota: 10,
	}

	err := session.Settle(20)

	require.Error(t, err)
	logOutput := output.String()
	require.Contains(t, logOutput, "billing_settle")
	require.Contains(t, logOutput, "status=error")
	require.Contains(t, logOutput, "error=settlement failed")
	require.False(t, strings.Contains(logOutput, "status=success"))
}

func TestBillingSessionRefundRollsBackTokenWhenFundingRefundFails(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Create(&model.Token{
		Id:          9722,
		UserId:      9721,
		Key:         "refund-token-rollback",
		Name:        "refund-token-rollback",
		RemainQuota: 0,
		UsedQuota:   10,
		Status:      common.TokenStatusEnabled,
	}).Error)
	funding := &failingRefundFunding{
		err:    errors.New("refund failed"),
		called: make(chan struct{}),
	}
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	session := &BillingSession{
		relayInfo: &relaycommon.RelayInfo{
			UserId:   9721,
			TokenId:  9722,
			TokenKey: "refund-token-rollback",
		},
		funding:          funding,
		preConsumedQuota: 10,
		tokenConsumed:    10,
	}

	session.Refund(ctx)

	select {
	case <-funding.called:
	case <-time.After(2 * time.Second):
		t.Fatal("refund funding was not called")
	}
	require.Eventually(t, func() bool {
		var token model.Token
		if err := model.DB.Select("remain_quota", "used_quota").First(&token, 9722).Error; err != nil {
			return false
		}
		return token.RemainQuota == 0 && token.UsedQuota == 10
	}, 2*time.Second, 10*time.Millisecond)
	require.True(t, session.NeedsRefund())
}

func TestBillingSessionRefundFundingFailureRollsBackTrackedTokenDelta(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Create(&model.Token{
		Id:          9774,
		UserId:      9773,
		Key:         "refund-clamp-token",
		Name:        "refund-clamp-token",
		RemainQuota: 100,
		UsedQuota:   10,
		Status:      common.TokenStatusEnabled,
	}).Error)
	funding := &failingRefundFunding{
		err:    errors.New("refund failed"),
		called: make(chan struct{}),
	}
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	session := &BillingSession{
		relayInfo: &relaycommon.RelayInfo{
			UserId:   9773,
			TokenId:  9774,
			TokenKey: "refund-clamp-token",
		},
		funding:          funding,
		preConsumedQuota: 60,
		tokenConsumed:    60,
	}

	session.Refund(ctx)

	select {
	case <-funding.called:
	case <-time.After(2 * time.Second):
		t.Fatal("refund funding was not called")
	}
	require.Eventually(t, func() bool {
		var token model.Token
		if err := model.DB.Select("remain_quota", "used_quota").First(&token, 9774).Error; err != nil {
			return false
		}
		return token.RemainQuota == 100 && token.UsedQuota == 10
	}, 2*time.Second, 10*time.Millisecond)
}

func TestBillingSessionRefundRefundsWalletWhenTokenDeleted(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Create(&model.User{
		Id:       9751,
		Username: "refund-deleted-token-owner",
		Password: "password123",
		Quota:    900,
		Status:   common.UserStatusEnabled,
	}).Error)
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	session := &BillingSession{
		relayInfo: &relaycommon.RelayInfo{
			UserId:   9751,
			TokenId:  9752,
			TokenKey: "deleted-token",
		},
		funding:          &WalletFunding{userId: 9751, consumed: 100},
		preConsumedQuota: 100,
		tokenConsumed:    100,
	}

	session.Refund(ctx)

	require.Eventually(t, func() bool {
		var user model.User
		if err := model.DB.Select("quota").First(&user, 9751).Error; err != nil {
			return false
		}
		return user.Quota == 1000
	}, 2*time.Second, 10*time.Millisecond)
}

func TestBillingSessionSettleDoesNotDebitWalletWhenTokenAdjustmentFails(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Create(&model.User{
		Id:       9701,
		Username: "settle-owner",
		Password: "password123",
		Quota:    100,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, model.DB.Create(&model.Token{
		Id:          9702,
		UserId:      9701,
		Key:         "settle-token-fail",
		Name:        "settle-token-fail",
		RemainQuota: 0,
		Status:      common.TokenStatusEnabled,
	}).Error)
	session := &BillingSession{
		relayInfo: &relaycommon.RelayInfo{
			UserId:   9701,
			TokenId:  9702,
			TokenKey: "settle-token-fail",
		},
		funding:          &WalletFunding{userId: 9701},
		preConsumedQuota: 10,
	}

	err := session.Settle(20)

	require.Error(t, err)
	var user model.User
	require.NoError(t, model.DB.Select("quota").First(&user, 9701).Error)
	require.Equal(t, 100, user.Quota)
	var token model.Token
	require.NoError(t, model.DB.Select("remain_quota").First(&token, 9702).Error)
	require.Equal(t, 0, token.RemainQuota)
}

func TestBillingSessionSettleRefundsWalletWhenTokenDeleted(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Create(&model.User{
		Id:       9731,
		Username: "settle-refund-owner",
		Password: "password123",
		Quota:    900,
		Status:   common.UserStatusEnabled,
	}).Error)
	session := &BillingSession{
		relayInfo: &relaycommon.RelayInfo{
			UserId:   9731,
			TokenId:  9732,
			TokenKey: "deleted-token",
		},
		funding:          &WalletFunding{userId: 9731},
		preConsumedQuota: 100,
	}

	err := session.Settle(40)

	require.NoError(t, err)
	var user model.User
	require.NoError(t, model.DB.Select("quota").First(&user, 9731).Error)
	require.Equal(t, 960, user.Quota)
}

func TestBillingSessionSettleNegativeDeltaFundingFailureRollsBackTrackedTokenDelta(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Create(&model.Token{
		Id:          9772,
		UserId:      9771,
		Key:         "settle-refund-clamp-token",
		Name:        "settle-refund-clamp-token",
		RemainQuota: 100,
		UsedQuota:   10,
		Status:      common.TokenStatusEnabled,
	}).Error)
	session := &BillingSession{
		relayInfo: &relaycommon.RelayInfo{
			UserId:   9771,
			TokenId:  9772,
			TokenKey: "settle-refund-clamp-token",
		},
		funding:          &failingSettlementFunding{err: errors.New("settlement failed")},
		preConsumedQuota: 100,
	}

	err := session.Settle(40)

	require.Error(t, err)
	var token model.Token
	require.NoError(t, model.DB.Select("remain_quota", "used_quota").First(&token, 9772).Error)
	require.Equal(t, 100, token.RemainQuota)
	require.Equal(t, 10, token.UsedQuota)
}

func TestBillingSessionRollbackRefundsWalletAfterZeroDeltaSettlement(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Create(&model.User{
		Id:       9761,
		Username: "zero-delta-owner",
		Password: "password123",
		Quota:    900,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, model.DB.Create(&model.Token{
		Id:          9762,
		UserId:      9761,
		Key:         "zero-delta-token",
		Name:        "zero-delta-token",
		RemainQuota: 900,
		UsedQuota:   100,
		Status:      common.TokenStatusEnabled,
	}).Error)
	session := &BillingSession{
		relayInfo: &relaycommon.RelayInfo{
			UserId:   9761,
			TokenId:  9762,
			TokenKey: "zero-delta-token",
		},
		funding:          &WalletFunding{userId: 9761, consumed: 100},
		preConsumedQuota: 100,
		tokenConsumed:    100,
	}

	require.NoError(t, session.Settle(100))
	require.NoError(t, session.Rollback(100))

	var user model.User
	require.NoError(t, model.DB.Select("quota").First(&user, 9761).Error)
	require.Equal(t, 1000, user.Quota)
	var token model.Token
	require.NoError(t, model.DB.Select("remain_quota", "used_quota").First(&token, 9762).Error)
	require.Equal(t, 1000, token.RemainQuota)
	require.Equal(t, 0, token.UsedQuota)
}

func TestBillingSessionRollbackRefundsSubscriptionPreConsumeRecord(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Create(&model.SubscriptionPlan{Id: 9765, Title: "rollback-sub-plan"}).Error)
	require.NoError(t, model.DB.Create(&model.User{
		Id:       9766,
		Username: "rollback-sub-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, model.DB.Create(&model.Token{
		Id:          9767,
		UserId:      9766,
		Key:         "rollback-sub-token",
		Name:        "rollback-sub-token",
		RemainQuota: 900,
		UsedQuota:   100,
		Status:      common.TokenStatusEnabled,
	}).Error)
	require.NoError(t, model.DB.Create(&model.UserSubscription{
		Id:          9768,
		UserId:      9766,
		PlanId:      9765,
		Status:      "active",
		AmountTotal: 1000,
		AmountUsed:  100,
		StartTime:   time.Now().Add(-time.Hour).Unix(),
		EndTime:     time.Now().Add(time.Hour).Unix(),
	}).Error)
	require.NoError(t, model.DB.Create(&model.SubscriptionPreConsumeRecord{
		RequestId:          "rollback-sub-request",
		UserId:             9766,
		UserSubscriptionId: 9768,
		PreConsumed:        100,
		Status:             "consumed",
	}).Error)

	relayInfo := &relaycommon.RelayInfo{
		UserId:          9766,
		TokenId:         9767,
		TokenKey:        "rollback-sub-token",
		RequestId:       "rollback-sub-request",
		OriginModelName: "gpt-4o",
		UsingGroup:      "default",
	}
	session := &BillingSession{
		relayInfo:        relayInfo,
		funding:          &SubscriptionFunding{requestId: "rollback-sub-request", subscriptionId: 9768, preConsumed: 100},
		preConsumedQuota: 100,
		tokenConsumed:    100,
		fundingSettled:   true,
		settled:          true,
	}

	require.NoError(t, session.Rollback(100))

	var sub model.UserSubscription
	require.NoError(t, model.DB.First(&sub, 9768).Error)
	require.Zero(t, sub.AmountUsed)
	var record model.SubscriptionPreConsumeRecord
	require.NoError(t, model.DB.Where("request_id = ?", "rollback-sub-request").First(&record).Error)
	require.Equal(t, "refunded", record.Status)

	_, err := model.PreConsumeUserSubscription("rollback-sub-request", 9766, "gpt-4o", 0, 100, "default")
	require.Error(t, err)
	require.Contains(t, err.Error(), "already refunded")
}

func TestBillingSessionRollbackFundingFailureRollsBackTrackedTokenDelta(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Create(&model.Token{
		Id:          9776,
		UserId:      9775,
		Key:         "rollback-clamp-token",
		Name:        "rollback-clamp-token",
		RemainQuota: 100,
		UsedQuota:   10,
		Status:      common.TokenStatusEnabled,
	}).Error)
	session := &BillingSession{
		relayInfo: &relaycommon.RelayInfo{
			UserId:   9775,
			TokenId:  9776,
			TokenKey: "rollback-clamp-token",
		},
		funding:          &failingSettlementFunding{err: errors.New("rollback funding failed")},
		preConsumedQuota: 60,
		tokenConsumed:    60,
		fundingSettled:   true,
		settled:          true,
	}

	err := session.Rollback(60)

	require.Error(t, err)
	var token model.Token
	require.NoError(t, model.DB.Select("remain_quota", "used_quota").First(&token, 9776).Error)
	require.Equal(t, 100, token.RemainQuota)
	require.Equal(t, 10, token.UsedQuota)
}

func TestPostConsumeQuotaDoesNotDebitWalletWhenTokenAdjustmentFails(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Create(&model.User{
		Id:       9711,
		Username: "legacy-owner",
		Password: "password123",
		Quota:    100,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, model.DB.Create(&model.Token{
		Id:          9712,
		UserId:      9711,
		Key:         "legacy-token-fail",
		Name:        "legacy-token-fail",
		RemainQuota: 0,
		Status:      common.TokenStatusEnabled,
	}).Error)
	relayInfo := &relaycommon.RelayInfo{
		UserId:   9711,
		TokenId:  9712,
		TokenKey: "legacy-token-fail",
	}

	err := PostConsumeQuota(relayInfo, 10, 0, false)

	require.Error(t, err)
	var user model.User
	require.NoError(t, model.DB.Select("quota").First(&user, 9711).Error)
	require.Equal(t, 100, user.Quota)
	var token model.Token
	require.NoError(t, model.DB.Select("remain_quota").First(&token, 9712).Error)
	require.Equal(t, 0, token.RemainQuota)
}

func TestPostConsumeQuotaNegativeDeltaFundingFailureRollsBackTrackedTokenDelta(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Create(&model.Token{
		Id:          9778,
		UserId:      9777,
		Key:         "post-consume-refund-clamp-token",
		Name:        "post-consume-refund-clamp-token",
		RemainQuota: 100,
		UsedQuota:   10,
		Status:      common.TokenStatusEnabled,
	}).Error)
	relayInfo := &relaycommon.RelayInfo{
		UserId:   9777,
		TokenId:  9778,
		TokenKey: "post-consume-refund-clamp-token",
	}

	err := PostConsumeQuota(relayInfo, -60, 100, false)

	require.Error(t, err)
	var token model.Token
	require.NoError(t, model.DB.Select("remain_quota", "used_quota").First(&token, 9778).Error)
	require.Equal(t, 100, token.RemainQuota)
	require.Equal(t, 10, token.UsedQuota)
}

func TestPostConsumeQuotaRefundsWalletWhenTokenDeleted(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Create(&model.User{
		Id:       9741,
		Username: "legacy-refund-owner",
		Password: "password123",
		Quota:    900,
		Status:   common.UserStatusEnabled,
	}).Error)
	relayInfo := &relaycommon.RelayInfo{
		UserId:   9741,
		TokenId:  9742,
		TokenKey: "deleted-token",
	}

	err := PostConsumeQuota(relayInfo, -60, 100, false)

	require.NoError(t, err)
	var user model.User
	require.NoError(t, model.DB.Select("quota").First(&user, 9741).Error)
	require.Equal(t, 960, user.Quota)
}
