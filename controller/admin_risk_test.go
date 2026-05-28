package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestBuildPaymentRiskEventsIgnoresSkippedReferralWithoutBinding(t *testing.T) {
	setupPaymentCallbackGuardDB(t)
	now := time.Now().Unix()

	require.NoError(t, model.DB.Create(&model.User{Id: 701, Username: "risk_no_binding_user"}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:                   701,
		Amount:                   10,
		Money:                    10,
		PaidAmount:               10,
		PaidCurrency:             "CNY",
		TradeNo:                  "risk-no-binding-topup",
		PaymentMethod:            "alipay",
		PaymentProvider:          model.PaymentProviderEpay,
		Status:                   common.TopUpStatusSuccess,
		CreateTime:               now,
		ReferralCommissionStatus: model.ReferralCommissionJobStatusSkipped,
		ReferralCommissionError:  "no_binding",
	}).Error)
	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		UserId:                   701,
		PlanId:                   1,
		Money:                    20,
		PaidAmount:               20,
		PaidCurrency:             "USD",
		TradeNo:                  "risk-no-binding-subscription",
		PaymentMethod:            model.PaymentMethodStripe,
		PaymentProvider:          model.PaymentProviderStripe,
		Status:                   common.TopUpStatusSuccess,
		CreateTime:               now,
		ReferralCommissionStatus: model.ReferralCommissionJobStatusSkipped,
		ReferralCommissionError:  "no_binding",
	}).Error)

	events, err := buildPaymentRiskEvents(now-3600, 24)
	require.NoError(t, err)
	require.Empty(t, events)
}

func TestBuildPaymentRiskEventsIncludesFailedReferralCommission(t *testing.T) {
	setupPaymentCallbackGuardDB(t)
	now := time.Now().Unix()

	require.NoError(t, model.DB.Create(&model.User{Id: 702, Username: "risk_failed_referral_user"}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:                   702,
		Amount:                   10,
		Money:                    10,
		PaidAmount:               10,
		PaidCurrency:             "CNY",
		TradeNo:                  "risk-failed-referral-topup",
		PaymentMethod:            "alipay",
		PaymentProvider:          model.PaymentProviderEpay,
		Status:                   common.TopUpStatusSuccess,
		CreateTime:               now,
		ReferralCommissionStatus: model.ReferralCommissionJobStatusFailed,
		ReferralCommissionError:  "fx_rate_missing",
	}).Error)

	events, err := buildPaymentRiskEvents(now-3600, 24)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "payment_anomaly", events[0].Type)
	require.Equal(t, "risk-failed-referral-topup", events[0].TradeNo)
	require.Equal(t, model.RiskSeverityWarning, events[0].Severity)
}

func TestBuildPaymentRiskEventsIgnoresPendingReferralFailure(t *testing.T) {
	setupPaymentCallbackGuardDB(t)
	now := time.Now().Unix()

	require.NoError(t, model.DB.Create(&model.User{Id: 703, Username: "risk_pending_referral_user"}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:                   703,
		Amount:                   10,
		Money:                    10,
		PaidAmount:               10,
		PaidCurrency:             "CNY",
		TradeNo:                  "risk-pending-referral-topup",
		PaymentMethod:            "alipay",
		PaymentProvider:          model.PaymentProviderEpay,
		Status:                   common.TopUpStatusPending,
		CreateTime:               now,
		ReferralCommissionStatus: model.ReferralCommissionJobStatusFailed,
		ReferralCommissionError:  "fx_rate_missing",
	}).Error)
	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		UserId:                   703,
		PlanId:                   1,
		Money:                    20,
		PaidAmount:               20,
		PaidCurrency:             "USD",
		TradeNo:                  "risk-pending-referral-subscription",
		PaymentMethod:            model.PaymentMethodStripe,
		PaymentProvider:          model.PaymentProviderStripe,
		Status:                   common.TopUpStatusPending,
		CreateTime:               now,
		ReferralCommissionStatus: model.ReferralCommissionJobStatusFailed,
		ReferralCommissionError:  "fx_rate_missing",
	}).Error)

	events, err := buildPaymentRiskEvents(now-3600, 24)
	require.NoError(t, err)
	require.Empty(t, events)
}

func TestBuildPaymentRiskEventsDoesNotDuplicateSubscriptionShadowTopup(t *testing.T) {
	setupPaymentCallbackGuardDB(t)
	now := time.Now().Unix()
	tradeNo := "risk-subscription-shadow-topup"

	require.NoError(t, model.DB.Create(&model.User{Id: 704, Username: "risk_shadow_topup_user"}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:                   704,
		Amount:                   20,
		Money:                    20,
		PaidAmount:               20,
		PaidCurrency:             "USD",
		TradeNo:                  tradeNo,
		PaymentMethod:            model.PaymentMethodStripe,
		PaymentProvider:          model.PaymentProviderStripe,
		Status:                   common.TopUpStatusSuccess,
		CreateTime:               now,
		ReferralCommissionStatus: model.ReferralCommissionJobStatusFailed,
		ReferralCommissionError:  "fx_rate_missing",
	}).Error)
	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		UserId:                   704,
		PlanId:                   1,
		Money:                    20,
		PaidAmount:               20,
		PaidCurrency:             "USD",
		TradeNo:                  tradeNo,
		PaymentMethod:            model.PaymentMethodStripe,
		PaymentProvider:          model.PaymentProviderStripe,
		Status:                   common.TopUpStatusSuccess,
		CreateTime:               now,
		ReferralCommissionStatus: model.ReferralCommissionJobStatusFailed,
		ReferralCommissionError:  "fx_rate_missing",
	}).Error)

	events, err := buildPaymentRiskEvents(now-3600, 24)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "subscription", events[0].OrderType)
	require.Equal(t, tradeNo, events[0].TradeNo)
}

func TestBuildReferralRiskEventsIncludesWithdrawalAnomalyBeyondTopInvitees(t *testing.T) {
	setupPaymentCallbackGuardDB(t)
	now := time.Now().Unix()
	since := now - 3600

	for i := 0; i < 25; i++ {
		userID := 7200 + i
		affiliateID := 8200 + i
		require.NoError(t, model.DB.Create(&model.User{Id: userID, Username: fmt.Sprintf("risk_inviter_%d", i)}).Error)
		require.NoError(t, model.DB.Create(&model.ReferralAffiliate{
			Id:         affiliateID,
			UserId:     userID,
			InviteCode: fmt.Sprintf("risk_invite_%d", i),
			Status:     model.ReferralAffiliateStatusApproved,
		}).Error)
		for j := 0; j < 10; j++ {
			require.NoError(t, model.DB.Create(&model.ReferralBinding{
				InviteeUserId: 90000 + i*100 + j,
				InviterUserId: userID,
				AffiliateId:   affiliateID,
				CreatedAt:     now,
			}).Error)
		}
	}

	withdrawalUserID := 7500
	withdrawalAffiliateID := 8500
	require.NoError(t, model.DB.Create(&model.User{Id: withdrawalUserID, Username: "risk_withdrawal_only"}).Error)
	require.NoError(t, model.DB.Create(&model.ReferralAffiliate{
		Id:         withdrawalAffiliateID,
		UserId:     withdrawalUserID,
		InviteCode: "risk_withdrawal_only",
		Status:     model.ReferralAffiliateStatusApproved,
	}).Error)
	for i := 0; i < 3; i++ {
		require.NoError(t, model.DB.Create(&model.ReferralWithdrawal{
			AffiliateId:    withdrawalAffiliateID,
			UserId:         withdrawalUserID,
			Amount:         100,
			Status:         model.ReferralWithdrawalStatusPending,
			IdempotencyKey: fmt.Sprintf("risk-withdrawal-only-%d", i),
			CreatedAt:      now,
		}).Error)
	}

	events, err := buildReferralRiskEvents(since, 24)
	require.NoError(t, err)
	var found *model.RiskEventUpsert
	for i := range events {
		if events[i].UserId == withdrawalUserID {
			found = &events[i]
			break
		}
	}
	require.NotNil(t, found)
	require.Equal(t, "referral_anomaly", found.Type)
	require.Equal(t, model.RiskSeverityInfo, found.Severity)
}

func TestCollectRiskSignalsSkipsWhitelistedTargets(t *testing.T) {
	setupPaymentCallbackGuardDB(t)
	now := time.Now().Unix()

	require.NoError(t, model.DB.Create(&model.User{Id: 705, Username: "risk_whitelist_signal_user"}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:          705,
		Amount:          1000,
		Money:           1000,
		PaidAmount:      1000,
		PaidCurrency:    "CNY",
		TradeNo:         "risk-whitelist-signal-topup",
		PaymentMethod:   "alipay",
		PaymentProvider: model.PaymentProviderEpay,
		Status:          common.TopUpStatusSuccess,
		CreateTime:      now,
	}).Error)
	require.NoError(t, model.DB.Create(&model.RiskWhitelist{
		TargetType: model.RiskTargetUser,
		TargetId:   "705",
		Reason:     "trusted internal account",
	}).Error)

	signals, err := collectRiskSignals(now-3600, 24)
	require.NoError(t, err)
	require.Empty(t, signals)
}

func TestDeleteRiskWhitelistCreatesAction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	whitelist := model.RiskWhitelist{
		TargetType: model.RiskTargetIP,
		TargetId:   "203.0.113.7",
		Reason:     "trusted office",
	}
	require.NoError(t, model.DB.Create(&whitelist).Error)

	router := gin.New()
	router.DELETE("/api/user/admin/risk/whitelist/:id", func(c *gin.Context) {
		c.Set("id", 1)
		c.Set("username", "admin")
		c.Set("role", common.RoleRootUser)
		DeleteRiskWhitelist(c)
	})
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/user/admin/risk/whitelist/%d", whitelist.Id), strings.NewReader(`{"reason":"manual cleanup"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "risk-center-test")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var remaining int64
	require.NoError(t, model.DB.Model(&model.RiskWhitelist{}).Where("id = ?", whitelist.Id).Count(&remaining).Error)
	require.Zero(t, remaining)
	var action model.RiskAction
	require.NoError(t, model.DB.Where("action = ? AND target_type = ? AND target_id = ?", model.RiskActionRemoveWhitelist, model.RiskTargetIP, whitelist.TargetId).First(&action).Error)
	require.Equal(t, 1, action.OperatorUserId)
	require.Equal(t, "admin", action.OperatorName)
	require.Equal(t, "203.0.113.7", action.Ip)
	require.Equal(t, "manual cleanup", action.Reason)
	require.Equal(t, model.RiskTargetIP+":203.0.113.7", action.OldValue)
}

func TestDeleteRiskWhitelistRequiresReason(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	whitelist := model.RiskWhitelist{
		TargetType: model.RiskTargetIP,
		TargetId:   "203.0.113.8",
		Reason:     "trusted office",
	}
	require.NoError(t, model.DB.Create(&whitelist).Error)

	router := gin.New()
	router.DELETE("/api/user/admin/risk/whitelist/:id", func(c *gin.Context) {
		c.Set("id", 1)
		c.Set("username", "admin")
		DeleteRiskWhitelist(c)
	})
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/user/admin/risk/whitelist/%d", whitelist.Id), strings.NewReader(`{"reason":"   "}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":false`)
	var remaining int64
	require.NoError(t, model.DB.Model(&model.RiskWhitelist{}).Where("id = ?", whitelist.Id).Count(&remaining).Error)
	require.Equal(t, int64(1), remaining)
}

func TestRiskWhitelistsForDetailIncludesEventTarget(t *testing.T) {
	setupPaymentCallbackGuardDB(t)

	require.NoError(t, model.DB.Create(&model.RiskWhitelist{
		TargetType: model.RiskTargetOrder,
		TargetId:   "risk-order-whitelist",
		Reason:     "manual order review",
	}).Error)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("role", common.RoleRootUser)
	whitelists, err := riskWhitelistsForDetail(c, 0, 0, "", model.RiskTargetOrder, "risk-order-whitelist")
	require.NoError(t, err)
	require.Len(t, whitelists, 1)
	require.Equal(t, model.RiskTargetOrder, whitelists[0].TargetType)
	require.Equal(t, "risk-order-whitelist", whitelists[0].TargetId)
}

func TestGetRiskDetailIncludesOrderActionsAndWhitelistsWithoutEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)
	now := time.Now().Unix()

	require.NoError(t, model.DB.Create(&model.User{Id: 713, Username: "risk_order_detail_user"}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:          713,
		Amount:          10,
		Money:           10,
		PaidAmount:      10,
		PaidCurrency:    "CNY",
		TradeNo:         "risk-order-detail",
		PaymentMethod:   "alipay",
		PaymentProvider: model.PaymentProviderEpay,
		Status:          common.TopUpStatusSuccess,
		CreateTime:      now,
	}).Error)
	require.NoError(t, model.DB.Create(&model.RiskWhitelist{
		TargetType: model.RiskTargetOrder,
		TargetId:   "risk-order-detail",
		Reason:     "trusted order",
	}).Error)
	require.NoError(t, model.DB.Create(&model.RiskAction{
		Action:     model.RiskActionWhitelist,
		TargetType: model.RiskTargetOrder,
		TargetId:   "risk-order-detail",
		Reason:     "trusted order",
	}).Error)

	router := gin.New()
	router.GET("/api/user/admin/risk/detail", func(c *gin.Context) {
		c.Set("role", common.RoleRootUser)
		GetRiskDetail(c)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/user/admin/risk/detail?type=order_detail&trade_no=risk-order-detail", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":true`)
	require.Contains(t, w.Body.String(), `"target_id":"risk-order-detail"`)
	require.Contains(t, w.Body.String(), `"trusted order"`)
}

func TestGetRiskDetailWithoutEventIDDoesNotPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	require.NoError(t, model.DB.Create(&model.User{Id: 706, Username: "risk_detail_user"}).Error)

	router := gin.New()
	router.GET("/api/user/admin/risk/detail", func(c *gin.Context) {
		c.Set("role", common.RoleRootUser)
		GetRiskDetail(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/user/admin/risk/detail?type=user_detail&user_id=706", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":true`)
	require.Contains(t, w.Body.String(), `"user_id":706`)
}

func TestRiskTokensForDetailUsesTokenStatusFromTokenTable(t *testing.T) {
	setupPaymentCallbackGuardDB(t)
	now := time.Now().Unix()

	require.NoError(t, model.DB.Create(&model.User{Id: 707, Username: "risk_token_user"}).Error)
	require.NoError(t, model.DB.Create(&model.Token{
		Id:     8801,
		UserId: 707,
		Key:    "risk-token-status-key",
		Name:   "risk-token-status",
		Status: common.TokenStatusEnabled,
	}).Error)
	require.NoError(t, model.LOG_DB.Create(&model.Log{
		UserId:    707,
		Username:  "risk_token_user",
		TokenId:   8801,
		TokenName: "risk-token-status",
		Type:      model.LogTypeConsume,
		CreatedAt: now,
	}).Error)

	rows, err := riskTokensForDetail([]int{707}, 0, now-3600)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, 8801, rows[0].TokenID)
	require.Equal(t, common.TokenStatusEnabled, rows[0].Status)
}

func TestMarkRiskEventViewedFailsWhenActionCannotBeWritten(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	event := model.RiskEvent{
		EventKey:   "risk-view-action-failure",
		Type:       "shared_ip",
		TargetType: model.RiskTargetIP,
		TargetId:   "203.0.113.9",
		Ip:         "203.0.113.9",
		Severity:   model.RiskSeverityWarning,
		Status:     model.RiskEventStatusOpen,
		Title:      "view action failure",
	}
	require.NoError(t, model.DB.Create(&event).Error)
	require.NoError(t, model.DB.Migrator().DropTable(&model.RiskAction{}))

	router := gin.New()
	router.POST("/api/user/admin/risk/events/:event_id/view", func(c *gin.Context) {
		c.Set("id", 1)
		c.Set("username", "admin")
		MarkRiskEventViewed(c)
	})
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/user/admin/risk/events/%d/view", event.Id), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":false`)
	var reloaded model.RiskEvent
	require.NoError(t, model.DB.Where("id = ?", event.Id).First(&reloaded).Error)
	require.Equal(t, model.RiskEventStatusOpen, reloaded.Status)
}

func TestMarkRiskEventViewedRejectsHigherRoleTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	require.NoError(t, model.DB.Create(&model.User{
		Id:       724,
		Username: "risk_root_view_owner",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
	}).Error)
	event := model.RiskEvent{
		EventKey:   "risk-view-root-target",
		Type:       "high_error_count",
		TargetType: model.RiskTargetUser,
		TargetId:   "724",
		UserId:     724,
		Username:   "risk_root_view_owner",
		Status:     model.RiskEventStatusOpen,
		Severity:   model.RiskSeverityWarning,
		Title:      "root user risk",
	}
	require.NoError(t, model.DB.Create(&event).Error)

	router := gin.New()
	router.POST("/api/user/admin/risk/events/:event_id/view", func(c *gin.Context) {
		c.Set("id", 2)
		c.Set("username", "admin")
		c.Set("role", common.RoleAdminUser)
		MarkRiskEventViewed(c)
	})
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/user/admin/risk/events/%d/view", event.Id), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":false`)
	var reloaded model.RiskEvent
	require.NoError(t, model.DB.Where("id = ?", event.Id).First(&reloaded).Error)
	require.Equal(t, model.RiskEventStatusOpen, reloaded.Status)
	var count int64
	require.NoError(t, model.DB.Model(&model.RiskAction{}).Where("event_id = ?", event.Id).Count(&count).Error)
	require.Zero(t, count)
}

func TestResolveRiskEventRejectsIPWithHigherRoleUserBeyondSampleLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)
	now := time.Now().Unix()
	ip := "203.0.113.53"

	for i := 0; i < 200; i++ {
		userID := 3000 + i
		require.NoError(t, model.DB.Create(&model.User{
			Id:       userID,
			Username: fmt.Sprintf("risk_common_ip_%d", i),
			Role:     common.RoleCommonUser,
			Status:   common.UserStatusEnabled,
		}).Error)
		require.NoError(t, model.LOG_DB.Create(&model.Log{
			UserId:    userID,
			Username:  fmt.Sprintf("risk_common_ip_%d", i),
			Ip:        ip,
			Type:      model.LogTypeConsume,
			CreatedAt: now,
		}).Error)
	}
	require.NoError(t, model.DB.Create(&model.User{
		Id:       3999,
		Username: "risk_root_ip_hidden",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, model.LOG_DB.Create(&model.Log{
		UserId:    3999,
		Username:  "risk_root_ip_hidden",
		Ip:        ip,
		Type:      model.LogTypeConsume,
		CreatedAt: now,
	}).Error)
	event := model.RiskEvent{
		EventKey:   "risk-shared-ip-hidden-root",
		Type:       "shared_ip",
		TargetType: model.RiskTargetIP,
		TargetId:   ip,
		Ip:         ip,
		Status:     model.RiskEventStatusOpen,
		Severity:   model.RiskSeverityWarning,
		Title:      "shared ip",
	}
	require.NoError(t, model.DB.Create(&event).Error)

	router := gin.New()
	router.POST("/api/user/admin/risk/events/:event_id/resolve", func(c *gin.Context) {
		c.Set("id", 2)
		c.Set("username", "admin")
		c.Set("role", common.RoleAdminUser)
		ResolveRiskEvent(c)
	})
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/user/admin/risk/events/%d/resolve", event.Id), strings.NewReader(`{"reason":"manual review"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":false`)
	var reloaded model.RiskEvent
	require.NoError(t, model.DB.Where("id = ?", event.Id).First(&reloaded).Error)
	require.Equal(t, model.RiskEventStatusOpen, reloaded.Status)
}

func TestResolveRiskEventRejectsIPWithSeparateLogDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)
	now := time.Now().Unix()
	ip := "203.0.113.54"

	previousLogDB := model.LOG_DB
	logDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"-logs?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, logDB.AutoMigrate(&model.Log{}))
	model.LOG_DB = logDB
	t.Cleanup(func() {
		model.LOG_DB = previousLogDB
	})

	require.NoError(t, model.DB.Create(&model.User{
		Id:       4100,
		Username: "risk_root_separate_log",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, model.LOG_DB.Create(&model.Log{
		UserId:    4100,
		Username:  "risk_root_separate_log",
		Ip:        ip,
		Type:      model.LogTypeConsume,
		CreatedAt: now,
	}).Error)
	event := model.RiskEvent{
		EventKey:   "risk-shared-ip-separate-log-root",
		Type:       "shared_ip",
		TargetType: model.RiskTargetIP,
		TargetId:   ip,
		Ip:         ip,
		Status:     model.RiskEventStatusOpen,
		Severity:   model.RiskSeverityWarning,
		Title:      "shared ip",
	}
	require.NoError(t, model.DB.Create(&event).Error)

	router := gin.New()
	router.POST("/api/user/admin/risk/events/:event_id/resolve", func(c *gin.Context) {
		c.Set("id", 2)
		c.Set("username", "admin")
		c.Set("role", common.RoleAdminUser)
		ResolveRiskEvent(c)
	})
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/user/admin/risk/events/%d/resolve", event.Id), strings.NewReader(`{"reason":"manual review"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":false`)
	var reloaded model.RiskEvent
	require.NoError(t, model.DB.Where("id = ?", event.Id).First(&reloaded).Error)
	require.Equal(t, model.RiskEventStatusOpen, reloaded.Status)
}

func TestIsRiskWhitelistedReturnsDatabaseError(t *testing.T) {
	setupPaymentCallbackGuardDB(t)
	require.NoError(t, model.DB.Migrator().DropTable(&model.RiskWhitelist{}))

	whitelisted, err := isRiskWhitelisted(model.RiskTargetIP, "203.0.113.10")
	require.False(t, whitelisted)
	require.Error(t, err)
	require.False(t, errors.Is(err, gorm.ErrRecordNotFound))
}

func TestBanRiskUserActionUsesActualUserTargetForIpEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	require.NoError(t, model.DB.Create(&model.User{
		Id:       708,
		Username: "risk_ban_target_user",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}).Error)
	event := model.RiskEvent{
		EventKey:   "risk-shared-ip-action-target",
		Type:       "shared_ip",
		TargetType: model.RiskTargetIP,
		TargetId:   "203.0.113.21",
		UserId:     708,
		Username:   "risk_ban_target_user",
		Ip:         "203.0.113.21",
		Severity:   model.RiskSeverityWarning,
		Status:     model.RiskEventStatusOpen,
		Title:      "shared ip",
	}
	require.NoError(t, model.DB.Create(&event).Error)

	router := gin.New()
	router.POST("/api/user/admin/risk/users/:user_id/ban", func(c *gin.Context) {
		c.Set("id", 1)
		c.Set("username", "admin")
		c.Set("role", common.RoleRootUser)
		BanRiskUser(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/user/admin/risk/users/708/ban", strings.NewReader(fmt.Sprintf(`{"event_id":%d,"reason":"manual review"}`, event.Id)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":true`)
	var user model.User
	require.NoError(t, model.DB.Where("id = ?", 708).First(&user).Error)
	require.Equal(t, common.UserStatusDisabled, user.Status)
	var action model.RiskAction
	require.NoError(t, model.DB.Where("action = ?", model.RiskActionBanUser).First(&action).Error)
	require.Equal(t, event.Id, action.EventId)
	require.Equal(t, model.RiskTargetUser, action.TargetType)
	require.Equal(t, "708", action.TargetId)
	require.Equal(t, 708, action.UserId)
	require.Equal(t, "203.0.113.21", action.Ip)
}

func TestBanRiskUserRequiresReason(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	require.NoError(t, model.DB.Create(&model.User{
		Id:       718,
		Username: "risk_ban_requires_reason",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}).Error)

	router := gin.New()
	router.POST("/api/user/admin/risk/users/:user_id/ban", func(c *gin.Context) {
		c.Set("id", 1)
		c.Set("username", "admin")
		c.Set("role", common.RoleRootUser)
		BanRiskUser(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/user/admin/risk/users/718/ban", strings.NewReader(`{"reason":"   "}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":false`)
	var user model.User
	require.NoError(t, model.DB.Where("id = ?", 718).First(&user).Error)
	require.Equal(t, common.UserStatusEnabled, user.Status)
}

func TestDisableRiskTokenActionUsesActualTokenTargetForOrderEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	require.NoError(t, model.DB.Create(&model.User{
		Id:       709,
		Username: "risk_disable_token_user",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, model.DB.Create(&model.Token{
		Id:     8802,
		UserId: 709,
		Key:    "risk-disable-token-key",
		Name:   "risk-disable-token",
		Status: common.TokenStatusEnabled,
	}).Error)
	event := model.RiskEvent{
		EventKey:   "risk-payment-action-target",
		Type:       "payment_anomaly",
		TargetType: model.RiskTargetOrder,
		TargetId:   "risk-payment-action-order",
		UserId:     709,
		Username:   "risk_disable_token_user",
		OrderType:  "topup",
		TradeNo:    "risk-payment-action-order",
		Severity:   model.RiskSeverityWarning,
		Status:     model.RiskEventStatusOpen,
		Title:      "payment anomaly",
	}
	require.NoError(t, model.DB.Create(&event).Error)

	router := gin.New()
	router.POST("/api/user/admin/risk/tokens/:token_id/disable", func(c *gin.Context) {
		c.Set("id", 1)
		c.Set("username", "admin")
		c.Set("role", common.RoleRootUser)
		DisableRiskToken(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/user/admin/risk/tokens/8802/disable", strings.NewReader(fmt.Sprintf(`{"event_id":%d,"reason":"manual review"}`, event.Id)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":true`)
	var token model.Token
	require.NoError(t, model.DB.Unscoped().Where("id = ?", 8802).First(&token).Error)
	require.Equal(t, common.TokenStatusDisabled, token.Status)
	var action model.RiskAction
	require.NoError(t, model.DB.Where("action = ?", model.RiskActionDisableToken).First(&action).Error)
	require.Equal(t, event.Id, action.EventId)
	require.Equal(t, model.RiskTargetToken, action.TargetType)
	require.Equal(t, "8802", action.TargetId)
	require.Equal(t, 8802, action.TokenId)
	require.Equal(t, 709, action.UserId)
	require.Equal(t, strconv.Itoa(common.TokenStatusEnabled), action.OldValue)
	require.Equal(t, strconv.Itoa(common.TokenStatusDisabled), action.NewValue)
}

func TestDisableRiskTokenRequiresReason(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	require.NoError(t, model.DB.Create(&model.User{
		Id:       719,
		Username: "risk_disable_requires_reason",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, model.DB.Create(&model.Token{
		Id:     8804,
		UserId: 719,
		Key:    "risk-disable-reason-key",
		Name:   "risk-disable-reason",
		Status: common.TokenStatusEnabled,
	}).Error)

	router := gin.New()
	router.POST("/api/user/admin/risk/tokens/:token_id/disable", func(c *gin.Context) {
		c.Set("id", 1)
		c.Set("username", "admin")
		c.Set("role", common.RoleRootUser)
		DisableRiskToken(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/user/admin/risk/tokens/8804/disable", strings.NewReader(`{"reason":"   "}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":false`)
	var token model.Token
	require.NoError(t, model.DB.Unscoped().Where("id = ?", 8804).First(&token).Error)
	require.Equal(t, common.TokenStatusEnabled, token.Status)
}

func TestCreateRiskWhitelistRejectsHigherRoleUserTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	require.NoError(t, model.DB.Create(&model.User{
		Id:       710,
		Username: "risk_root_target",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
	}).Error)

	router := gin.New()
	router.POST("/api/user/admin/risk/whitelist", func(c *gin.Context) {
		c.Set("id", 2)
		c.Set("username", "admin")
		c.Set("role", common.RoleAdminUser)
		CreateRiskWhitelist(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/user/admin/risk/whitelist", strings.NewReader(`{"target_type":"user","target_id":"710","reason":"trusted"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":false`)
	var count int64
	require.NoError(t, model.DB.Model(&model.RiskWhitelist{}).Where("target_type = ? AND target_id = ?", model.RiskWhitelistUser, "710").Count(&count).Error)
	require.Zero(t, count)
}

func TestCreateRiskWhitelistRejectsInvalidTargetType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	router := gin.New()
	router.POST("/api/user/admin/risk/whitelist", func(c *gin.Context) {
		c.Set("id", 1)
		c.Set("username", "root")
		c.Set("role", common.RoleRootUser)
		CreateRiskWhitelist(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/user/admin/risk/whitelist", strings.NewReader(`{"target_type":"unknown","target_id":"abc","reason":"trusted"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":false`)
}

func TestCreateRiskWhitelistRequiresReason(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	router := gin.New()
	router.POST("/api/user/admin/risk/whitelist", func(c *gin.Context) {
		c.Set("id", 1)
		c.Set("username", "root")
		c.Set("role", common.RoleRootUser)
		CreateRiskWhitelist(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/user/admin/risk/whitelist", strings.NewReader(`{"target_type":"ip","target_id":"203.0.113.9","reason":"   "}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":false`)
	var count int64
	require.NoError(t, model.DB.Model(&model.RiskWhitelist{}).Where("target_type = ? AND target_id = ?", model.RiskWhitelistIP, "203.0.113.9").Count(&count).Error)
	require.Zero(t, count)
}

func TestCreateRiskWhitelistRejectsMissingOrderTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	router := gin.New()
	router.POST("/api/user/admin/risk/whitelist", func(c *gin.Context) {
		c.Set("id", 1)
		c.Set("username", "root")
		c.Set("role", common.RoleRootUser)
		CreateRiskWhitelist(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/user/admin/risk/whitelist", strings.NewReader(`{"target_type":"order","target_id":"missing-order","reason":"trusted"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":false`)
}

func TestCreateRiskWhitelistRejectsHigherRoleOrderTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)
	now := time.Now().Unix()

	require.NoError(t, model.DB.Create(&model.User{
		Id:       712,
		Username: "risk_root_order_owner",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:          712,
		Amount:          10,
		Money:           10,
		PaidAmount:      10,
		PaidCurrency:    "CNY",
		TradeNo:         "risk-root-order",
		PaymentMethod:   "alipay",
		PaymentProvider: model.PaymentProviderEpay,
		Status:          common.TopUpStatusSuccess,
		CreateTime:      now,
	}).Error)

	router := gin.New()
	router.POST("/api/user/admin/risk/whitelist", func(c *gin.Context) {
		c.Set("id", 2)
		c.Set("username", "admin")
		c.Set("role", common.RoleAdminUser)
		CreateRiskWhitelist(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/user/admin/risk/whitelist", strings.NewReader(`{"target_type":"order","target_id":"risk-root-order","reason":"trusted"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":false`)
	var count int64
	require.NoError(t, model.DB.Model(&model.RiskWhitelist{}).Where("target_type = ? AND target_id = ?", model.RiskTargetOrder, "risk-root-order").Count(&count).Error)
	require.Zero(t, count)
}

func TestCreateRiskWhitelistRejectsHigherRoleIPTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)
	now := time.Now().Unix()

	require.NoError(t, model.DB.Create(&model.User{
		Id:       721,
		Username: "risk_root_ip_owner",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, model.LOG_DB.Create(&model.Log{
		UserId:    721,
		Username:  "risk_root_ip_owner",
		Ip:        "203.0.113.50",
		Type:      model.LogTypeConsume,
		CreatedAt: now,
	}).Error)

	router := gin.New()
	router.POST("/api/user/admin/risk/whitelist", func(c *gin.Context) {
		c.Set("id", 2)
		c.Set("username", "admin")
		c.Set("role", common.RoleAdminUser)
		CreateRiskWhitelist(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/user/admin/risk/whitelist", strings.NewReader(`{"target_type":"ip","target_id":"203.0.113.50","reason":"trusted"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":false`)
	var count int64
	require.NoError(t, model.DB.Model(&model.RiskWhitelist{}).Where("target_type = ? AND target_id = ?", model.RiskWhitelistIP, "203.0.113.50").Count(&count).Error)
	require.Zero(t, count)
}

func TestCreateRiskWhitelistRejectsIPTargetForNonRootWithoutCurrentUsers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	router := gin.New()
	router.POST("/api/user/admin/risk/whitelist", func(c *gin.Context) {
		c.Set("id", 2)
		c.Set("username", "admin")
		c.Set("role", common.RoleAdminUser)
		CreateRiskWhitelist(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/user/admin/risk/whitelist", strings.NewReader(`{"target_type":"ip","target_id":"203.0.113.51","reason":"trusted"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":false`)
	var count int64
	require.NoError(t, model.DB.Model(&model.RiskWhitelist{}).Where("target_type = ? AND target_id = ?", model.RiskWhitelistIP, "203.0.113.51").Count(&count).Error)
	require.Zero(t, count)
}

func TestRootCanCreateIPWhitelistWithoutCurrentUsers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	router := gin.New()
	router.POST("/api/user/admin/risk/whitelist", func(c *gin.Context) {
		c.Set("id", 1)
		c.Set("username", "root")
		c.Set("role", common.RoleRootUser)
		CreateRiskWhitelist(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/user/admin/risk/whitelist", strings.NewReader(`{"target_type":"ip","target_id":"203.0.113.52","reason":"trusted"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":true`)
	var count int64
	require.NoError(t, model.DB.Model(&model.RiskWhitelist{}).Where("target_type = ? AND target_id = ?", model.RiskWhitelistIP, "203.0.113.52").Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestDisableRiskTokenRejectsMissingOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	require.NoError(t, model.DB.Create(&model.Token{
		Id:     8803,
		UserId: 999999,
		Name:   "orphan token",
		Key:    "orphan-key",
		Status: common.TokenStatusEnabled,
	}).Error)

	router := gin.New()
	router.POST("/api/user/admin/risk/tokens/:token_id/disable", func(c *gin.Context) {
		c.Set("id", 1)
		c.Set("username", "root")
		c.Set("role", common.RoleRootUser)
		DisableRiskToken(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/user/admin/risk/tokens/8803/disable", strings.NewReader(`{"reason":"manual review"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":false`)
	var token model.Token
	require.NoError(t, model.DB.Unscoped().Where("id = ?", 8803).First(&token).Error)
	require.Equal(t, common.TokenStatusEnabled, token.Status)
}

func TestDisableRiskTokenRejectsUnmanageableEventID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	require.NoError(t, model.DB.Create(&model.User{
		Id:       726,
		Username: "risk_disable_normal_owner",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, model.DB.Create(&model.Token{
		Id:     8805,
		UserId: 726,
		Name:   "normal token",
		Key:    "normal-token-key",
		Status: common.TokenStatusEnabled,
	}).Error)
	require.NoError(t, model.DB.Create(&model.User{
		Id:       727,
		Username: "risk_disable_root_event",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
	}).Error)
	event := model.RiskEvent{
		EventKey:   "risk-disable-root-event",
		Type:       "high_error_count",
		TargetType: model.RiskTargetUser,
		TargetId:   "727",
		UserId:     727,
		Username:   "risk_disable_root_event",
		Status:     model.RiskEventStatusOpen,
		Severity:   model.RiskSeverityWarning,
		Title:      "root user risk",
	}
	require.NoError(t, model.DB.Create(&event).Error)

	router := gin.New()
	router.POST("/api/user/admin/risk/tokens/:token_id/disable", func(c *gin.Context) {
		c.Set("id", 2)
		c.Set("username", "admin")
		c.Set("role", common.RoleAdminUser)
		DisableRiskToken(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/user/admin/risk/tokens/8805/disable", strings.NewReader(fmt.Sprintf(`{"event_id":%d,"reason":"manual review"}`, event.Id)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":false`)
	var token model.Token
	require.NoError(t, model.DB.Unscoped().Where("id = ?", 8805).First(&token).Error)
	require.Equal(t, common.TokenStatusEnabled, token.Status)
	var count int64
	require.NoError(t, model.DB.Model(&model.RiskAction{}).Where("event_id = ?", event.Id).Count(&count).Error)
	require.Zero(t, count)
}

func TestDisableRiskTokenRejectsMissingEventID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	require.NoError(t, model.DB.Create(&model.User{
		Id:       728,
		Username: "risk_disable_missing_event_owner",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, model.DB.Create(&model.Token{
		Id:     8806,
		UserId: 728,
		Name:   "missing event token",
		Key:    "missing-event-token-key",
		Status: common.TokenStatusEnabled,
	}).Error)

	router := gin.New()
	router.POST("/api/user/admin/risk/tokens/:token_id/disable", func(c *gin.Context) {
		c.Set("id", 1)
		c.Set("username", "root")
		c.Set("role", common.RoleRootUser)
		DisableRiskToken(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/user/admin/risk/tokens/8806/disable", strings.NewReader(`{"event_id":999999,"reason":"manual review"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":false`)
	var token model.Token
	require.NoError(t, model.DB.Unscoped().Where("id = ?", 8806).First(&token).Error)
	require.Equal(t, common.TokenStatusEnabled, token.Status)
}

func TestResolveRiskEventRequiresReason(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	event := model.RiskEvent{
		EventKey:   "risk-resolve-empty-reason",
		Type:       "shared_ip",
		TargetType: model.RiskTargetIP,
		TargetId:   "203.0.113.40",
		Ip:         "203.0.113.40",
		Status:     model.RiskEventStatusOpen,
		Severity:   model.RiskSeverityWarning,
		Title:      "shared ip",
	}
	require.NoError(t, model.DB.Create(&event).Error)

	router := gin.New()
	router.POST("/api/user/admin/risk/events/:event_id/resolve", func(c *gin.Context) {
		c.Set("id", 1)
		c.Set("username", "root")
		c.Set("role", common.RoleRootUser)
		ResolveRiskEvent(c)
	})
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/user/admin/risk/events/%d/resolve", event.Id), strings.NewReader(`{"reason":"   "}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":false`)
	var reloaded model.RiskEvent
	require.NoError(t, model.DB.Where("id = ?", event.Id).First(&reloaded).Error)
	require.Equal(t, model.RiskEventStatusOpen, reloaded.Status)
}

func TestResolveRiskEventRejectsClosedEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	event := model.RiskEvent{
		EventKey:    "risk-resolve-closed",
		Type:        "shared_ip",
		TargetType:  model.RiskTargetIP,
		TargetId:    "203.0.113.41",
		Ip:          "203.0.113.41",
		Status:      model.RiskEventStatusResolved,
		Severity:    model.RiskSeverityWarning,
		Title:       "shared ip",
		ResolvedAt:  100,
		ResolvedBy:  1,
		ResolveNote: "first decision",
	}
	require.NoError(t, model.DB.Create(&event).Error)

	router := gin.New()
	router.POST("/api/user/admin/risk/events/:event_id/resolve", func(c *gin.Context) {
		c.Set("id", 2)
		c.Set("username", "admin")
		c.Set("role", common.RoleRootUser)
		ResolveRiskEvent(c)
	})
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/user/admin/risk/events/%d/resolve", event.Id), strings.NewReader(`{"reason":"second decision"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":false`)
	var reloaded model.RiskEvent
	require.NoError(t, model.DB.Where("id = ?", event.Id).First(&reloaded).Error)
	require.Equal(t, model.RiskEventStatusResolved, reloaded.Status)
	require.Equal(t, int64(100), reloaded.ResolvedAt)
	require.Equal(t, 1, reloaded.ResolvedBy)
	require.Equal(t, "first decision", reloaded.ResolveNote)
	var count int64
	require.NoError(t, model.DB.Model(&model.RiskAction{}).Where("event_id = ?", event.Id).Count(&count).Error)
	require.Zero(t, count)
}

func TestResolveRiskEventRejectsHigherRoleTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	require.NoError(t, model.DB.Create(&model.User{
		Id:       722,
		Username: "risk_root_event_owner",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
	}).Error)
	event := model.RiskEvent{
		EventKey:   "risk-resolve-root-target",
		Type:       "high_error_count",
		TargetType: model.RiskTargetUser,
		TargetId:   "722",
		UserId:     722,
		Username:   "risk_root_event_owner",
		Status:     model.RiskEventStatusOpen,
		Severity:   model.RiskSeverityWarning,
		Title:      "root user risk",
	}
	require.NoError(t, model.DB.Create(&event).Error)

	router := gin.New()
	router.POST("/api/user/admin/risk/events/:event_id/resolve", func(c *gin.Context) {
		c.Set("id", 2)
		c.Set("username", "admin")
		c.Set("role", common.RoleAdminUser)
		ResolveRiskEvent(c)
	})
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/user/admin/risk/events/%d/resolve", event.Id), strings.NewReader(`{"reason":"manual review"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":false`)
	var reloaded model.RiskEvent
	require.NoError(t, model.DB.Where("id = ?", event.Id).First(&reloaded).Error)
	require.Equal(t, model.RiskEventStatusOpen, reloaded.Status)
}

func TestGetRiskDetailReturnsErrorForMissingEventID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	router := gin.New()
	router.GET("/api/user/admin/risk/detail", GetRiskDetail)

	req := httptest.NewRequest(http.MethodGet, "/api/user/admin/risk/detail?event_id=999999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":false`)
}

func TestRiskWhitelistsForDetailOnlyReturnsActiveEntries(t *testing.T) {
	setupPaymentCallbackGuardDB(t)
	now := time.Now().Unix()

	require.NoError(t, model.DB.Create(&model.RiskWhitelist{
		TargetType: model.RiskWhitelistUser,
		TargetId:   "711",
		Reason:     "expired user whitelist",
		ExpiresAt:  now - 60,
	}).Error)
	require.NoError(t, model.DB.Create(&model.RiskWhitelist{
		TargetType: model.RiskWhitelistIP,
		TargetId:   "203.0.113.30",
		Reason:     "active ip whitelist",
		ExpiresAt:  now + 60,
	}).Error)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("role", common.RoleRootUser)
	whitelists, err := riskWhitelistsForDetail(c, 711, 0, "203.0.113.30", "", "")
	require.NoError(t, err)
	require.Len(t, whitelists, 1)
	require.Equal(t, model.RiskWhitelistIP, whitelists[0].TargetType)
	require.Equal(t, "203.0.113.30", whitelists[0].TargetId)
}

func TestRiskReferralsForDetailDoesNotReturnGlobalRowsWithoutTargetUsers(t *testing.T) {
	setupPaymentCallbackGuardDB(t)
	now := time.Now().Unix()

	require.NoError(t, model.DB.Create(&model.User{Id: 725, Username: "risk_referral_global"}).Error)
	require.NoError(t, model.DB.Create(&model.ReferralAffiliate{
		UserId:     725,
		InviteCode: "risk-global",
		Status:     model.ReferralAffiliateStatusApproved,
		ApprovedAt: now,
	}).Error)
	for i := 0; i < 12; i++ {
		require.NoError(t, model.DB.Create(&model.ReferralBinding{
			InviteeUserId: 92000 + i,
			InviterUserId: 725,
			CreatedAt:     now,
		}).Error)
	}

	rows, err := riskReferralsForDetail(nil, now-3600)

	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestGetRiskDetailRejectsHigherRoleUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	require.NoError(t, model.DB.Create(&model.User{
		Id:       723,
		Username: "risk_root_detail_user",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
	}).Error)

	router := gin.New()
	router.GET("/api/user/admin/risk/detail", func(c *gin.Context) {
		c.Set("id", 2)
		c.Set("username", "admin")
		c.Set("role", common.RoleAdminUser)
		GetRiskDetail(c)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/user/admin/risk/detail?type=user_detail&user_id=723", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":false`)
}

func TestGetRiskDetailFiltersIPActionsForNonRoot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)
	now := time.Now().Unix()
	ip := "203.0.113.81"

	require.NoError(t, model.DB.Create(&model.User{
		Id:       729,
		Username: "risk_ip_detail_normal",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, model.DB.Create(&model.User{
		Id:       730,
		Username: "risk_ip_detail_root",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, model.LOG_DB.Create(&model.Log{
		UserId:    729,
		Username:  "risk_ip_detail_normal",
		Ip:        ip,
		Type:      model.LogTypeConsume,
		CreatedAt: now,
	}).Error)
	require.NoError(t, model.LOG_DB.Create(&model.Log{
		UserId:    730,
		Username:  "risk_ip_detail_root",
		Ip:        ip,
		Type:      model.LogTypeConsume,
		CreatedAt: now - 40*24*3600,
	}).Error)
	require.NoError(t, model.DB.Create(&model.RiskAction{
		Action:     model.RiskActionViewed,
		TargetType: model.RiskTargetIP,
		TargetId:   ip,
		Ip:         ip,
		UserId:     730,
		Reason:     "root-only note",
	}).Error)
	require.NoError(t, model.DB.Create(&model.RiskWhitelist{
		TargetType: model.RiskWhitelistIP,
		TargetId:   ip,
		Reason:     "root-only whitelist",
	}).Error)

	router := gin.New()
	router.GET("/api/user/admin/risk/detail", func(c *gin.Context) {
		c.Set("id", 2)
		c.Set("username", "admin")
		c.Set("role", common.RoleAdminUser)
		GetRiskDetail(c)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/user/admin/risk/detail?type=ip_detail&window_hours=24&ip="+ip, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success":true`)
	require.NotContains(t, w.Body.String(), "root-only note")
	require.NotContains(t, w.Body.String(), "root-only whitelist")
}

func TestFilterRiskEventInputsAppliesLimitAfterPermissionAndWhitelist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	require.NoError(t, model.DB.Create(&model.User{Id: 731, Username: "risk_visible_candidate", Role: common.RoleCommonUser}).Error)
	for i := 0; i < riskScanCandidateLimit; i++ {
		targetID := fmt.Sprintf("blocked-%d", i)
		require.NoError(t, model.DB.Create(&model.RiskWhitelist{
			TargetType: model.RiskTargetUser,
			TargetId:   targetID,
			Reason:     "trusted",
		}).Error)
	}
	inputs := make([]model.RiskEventUpsert, 0, riskScanCandidateLimit+1)
	for i := 0; i < riskScanCandidateLimit; i++ {
		targetID := fmt.Sprintf("blocked-%d", i)
		inputs = append(inputs, model.RiskEventUpsert{
			EventKey:   "high_error_count:" + targetID,
			Type:       "high_error_count",
			TargetType: model.RiskTargetUser,
			TargetId:   targetID,
			UserId:     731,
		})
	}
	inputs = append(inputs, model.RiskEventUpsert{
		EventKey:   "high_error_count:731",
		Type:       "high_error_count",
		TargetType: model.RiskTargetUser,
		TargetId:   "731",
		UserId:     731,
	})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("role", common.RoleAdminUser)

	filtered, err := filterRiskEventInputsForContext(c, inputs)

	require.NoError(t, err)
	require.Len(t, filtered, 1)
	require.Equal(t, "high_error_count:731", filtered[0].EventKey)
}

func TestRiskReferralsForDetailFindsTargetBeyondGlobalTopLimit(t *testing.T) {
	setupPaymentCallbackGuardDB(t)
	now := time.Now().Unix()
	targetUserID := 850

	for i := 0; i < 90; i++ {
		userID := 900 + i
		require.NoError(t, model.DB.Create(&model.User{Id: userID, Username: fmt.Sprintf("risk_top_ref_%d", i)}).Error)
		require.NoError(t, model.DB.Create(&model.ReferralAffiliate{
			UserId:     userID,
			InviteCode: fmt.Sprintf("risk-top-ref-%d", i),
			Status:     model.ReferralAffiliateStatusApproved,
			ApprovedAt: now,
		}).Error)
		for j := 0; j < 30; j++ {
			require.NoError(t, model.DB.Create(&model.ReferralBinding{
				InviteeUserId: 200000 + i*100 + j,
				InviterUserId: userID,
				CreatedAt:     now,
			}).Error)
		}
	}
	require.NoError(t, model.DB.Create(&model.User{Id: targetUserID, Username: "risk_target_referral"}).Error)
	require.NoError(t, model.DB.Create(&model.ReferralAffiliate{
		UserId:     targetUserID,
		InviteCode: "risk-target-referral",
		Status:     model.ReferralAffiliateStatusApproved,
		ApprovedAt: now,
	}).Error)
	for i := 0; i < 3; i++ {
		require.NoError(t, model.DB.Create(&model.ReferralWithdrawal{
			UserId:         targetUserID,
			Amount:         10,
			Status:         model.ReferralWithdrawalStatusPending,
			IdempotencyKey: fmt.Sprintf("risk-target-withdrawal-%d", i),
			CreatedAt:      now,
		}).Error)
	}

	rows, err := riskReferralsForDetail([]int{targetUserID}, now-3600)

	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, targetUserID, rows[0].InviterUserID)
	require.Equal(t, int64(3), rows[0].WithdrawalCount)
}
