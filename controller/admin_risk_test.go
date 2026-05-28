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
		DeleteRiskWhitelist(c)
	})
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/user/admin/risk/whitelist/%d", whitelist.Id), nil)
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
	require.Equal(t, model.RiskTargetIP+":203.0.113.7", action.OldValue)
}

func TestRiskWhitelistsForDetailIncludesEventTarget(t *testing.T) {
	setupPaymentCallbackGuardDB(t)

	require.NoError(t, model.DB.Create(&model.RiskWhitelist{
		TargetType: model.RiskTargetOrder,
		TargetId:   "risk-order-whitelist",
		Reason:     "manual order review",
	}).Error)

	whitelists, err := riskWhitelistsForDetail(0, 0, "", model.RiskTargetOrder, "risk-order-whitelist")
	require.NoError(t, err)
	require.Len(t, whitelists, 1)
	require.Equal(t, model.RiskTargetOrder, whitelists[0].TargetType)
	require.Equal(t, "risk-order-whitelist", whitelists[0].TargetId)
}

func TestGetRiskDetailWithoutEventIDDoesNotPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPaymentCallbackGuardDB(t)

	require.NoError(t, model.DB.Create(&model.User{Id: 706, Username: "risk_detail_user"}).Error)

	router := gin.New()
	router.GET("/api/user/admin/risk/detail", GetRiskDetail)

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
