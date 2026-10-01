package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	oauthAuthFlowTTL          = 10 * time.Minute
	oauthRedirectPathMaxBytes = 2048
)

type oauthStateRequest struct {
	Provider string          `json:"provider"`
	Intent   string          `json:"intent"`
	Aff      string          `json:"aff,omitempty"`
	Redirect string          `json:"redirect,omitempty"`
	Language string          `json:"language,omitempty"`
	Scope    string          `json:"scope,omitempty"`
	Context  json.RawMessage `json:"context,omitempty"`
}

type oauthFlowPayload struct {
	AffiliateCode   string                         `json:"affiliate_code,omitempty"`
	Redirect        string                         `json:"redirect,omitempty"`
	Language        string                         `json:"language,omitempty"`
	Verification    *service.OAuthVerificationFlow `json:"verification,omitempty"`
	Telegram        *oauth.TelegramOAuthFlow       `json:"telegram,omitempty"`
	SessionIdentity *service.AuthIdentity          `json:"session_identity,omitempty"`
	Authorization   *model.AuthFlowAuthorization   `json:"authorization,omitempty"`
}

func buildOAuthUsername(provider oauth.Provider, oauthUser *oauth.OAuthUser) string {
	return model.SelectNewUserUsername(oauthUser.Username, strings.TrimSuffix(provider.GetProviderPrefix(), "_"))
}

// providerParams returns map with Provider key for i18n templates
func providerParams(name string) map[string]any {
	return map[string]any{"Provider": name}
}

func respondOAuthDisabled(c *gin.Context, provider string) {
	common.ApiErrorI18n(c, i18n.MsgOAuthNotEnabled, providerParams(provider))
}

func respondOAuthAlreadyBound(c *gin.Context, provider string) {
	common.ApiErrorI18n(c, i18n.MsgOAuthAlreadyBound, providerParams(provider))
}

func respondOAuthUserDeleted(c *gin.Context) {
	common.ApiErrorI18n(c, i18n.MsgOAuthUserDeleted)
}

func respondOAuthUserBanned(c *gin.Context) {
	common.ApiErrorI18n(c, i18n.MsgOAuthUserBanned)
}

func respondRegisterDisabled(c *gin.Context) {
	common.ApiErrorI18n(c, i18n.MsgUserRegisterDisabled)
}

func respondOAuthStateInvalid(c *gin.Context) {
	c.JSON(http.StatusForbidden, gin.H{
		"success": false,
		"message": i18n.T(c, i18n.MsgOAuthStateInvalid),
	})
}

func oauthInvalidParams() error {
	return common.Localized(i18n.MsgInvalidParams)
}

func oauthConnectFailed(provider string) error {
	return common.Localized(i18n.MsgOAuthConnectFailed, providerParams(provider))
}

func oauthTokenFailed(provider string) error {
	return common.Localized(i18n.MsgOAuthTokenFailed, providerParams(provider))
}

func oauthUserInfoEmpty(provider string) error {
	return common.Localized(i18n.MsgOAuthUserInfoEmpty, providerParams(provider))
}

func oauthServerAddressRequired() error {
	return common.Localized(i18n.MsgOAuthServerAddressRequired)
}

func oauthGetUserError() error {
	return common.Localized(i18n.MsgOAuthGetUserErr)
}

func captureOAuthInterfaceLanguage(c *gin.Context, requested string) string {
	if canonical, ok := i18n.RecognizedLang(requested); ok {
		return canonical
	}
	return i18n.GetLangFromContext(c)
}

func oauthSessionInterfaceLanguage(c *gin.Context) string {
	if c == nil {
		return ""
	}
	rawSession, exists := c.Get(sessions.DefaultKey)
	if !exists {
		return ""
	}
	session, ok := rawSession.(sessions.Session)
	if !ok || session == nil {
		return ""
	}
	if raw := session.Get("oauth_language"); raw != nil {
		if value, ok := raw.(string); ok {
			return value
		}
	}
	return ""
}

func isOAuthStateProviderAllowed(provider string) bool {
	if oauth.GetProvider(provider) != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(provider), "wechat")
}

// GenerateOAuthCode generates a state code for OAuth CSRF protection
func GenerateOAuthCode(c *gin.Context) {
	if c.Request.Method == http.MethodGet {
		generateLegacyOAuthState(c)
		return
	}

	var request oauthStateRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	request.Provider = strings.TrimSpace(request.Provider)
	request.Intent = strings.TrimSpace(request.Intent)
	request.Aff = strings.TrimSpace(request.Aff)
	request.Redirect = strings.TrimSpace(request.Redirect)
	redirectPath, redirectOK := normalizeOAuthRedirectPath(request.Redirect)
	if !isOAuthStateProviderAllowed(request.Provider) ||
		(request.Intent != model.AuthFlowIntentLogin && request.Intent != model.AuthFlowIntentBind && request.Intent != model.AuthFlowIntentVerify) ||
		len(request.Aff) > 32 ||
		!redirectOK ||
		(request.Intent == model.AuthFlowIntentBind && (request.Aff != "" || request.Redirect != "")) ||
		(request.Intent == model.AuthFlowIntentVerify && (request.Aff != "" || request.Redirect != "")) ||
		(request.Intent != model.AuthFlowIntentVerify && (request.Scope != "" || len(request.Context) != 0)) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	var telegramFlow *oauth.TelegramOAuthFlow
	if request.Provider == "telegram" {
		flow, flowErr := oauth.NewTelegramOAuthFlow()
		if flowErr != nil {
			common.ApiError(c, flowErr)
			return
		}
		telegramFlow = flow
	}

	userID := 0
	sessionID := ""
	lang := captureOAuthInterfaceLanguage(c, request.Language)
	flowPayload := oauthFlowPayload{
		AffiliateCode: strings.TrimSpace(request.Aff),
		Redirect:      redirectPath,
		Language:      lang,
		Telegram:      telegramFlow,
	}
	if request.Intent == model.AuthFlowIntentBind || request.Intent == model.AuthFlowIntentVerify {
		identity, ok := middleware.GetSessionAuthIdentity(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": i18n.T(c, i18n.MsgOAuthBindRequiresLogin),
			})
			return
		}
		userID = identity.UserID
		sessionID = identity.SessionID
		flowPayload.SessionIdentity = &identity
		if request.Intent == model.AuthFlowIntentBind {
			bindContext, contextErr := common.Marshal(service.AccountBindingContext{Provider: request.Provider})
			if contextErr != nil {
				writeSecurityOperationError(c, contextErr)
				return
			}
			flowPayload.Authorization = middleware.RequireSecurityProof(c, service.VerificationOperation{
				Scope:   service.VerificationScopeAccountBind,
				Context: bindContext,
			})
			if flowPayload.Authorization == nil {
				return
			}
		}
		if request.Intent == model.AuthFlowIntentVerify {
			verification, verifyErr := service.StartOAuthVerification(identity, service.VerificationOperation{
				Scope:   request.Scope,
				Context: request.Context,
			}, request.Provider)
			if verifyErr != nil {
				writeSecurityOperationError(c, verifyErr)
				return
			}
			flowPayload.Verification = verification
		}
	}

	payload, err := common.Marshal(flowPayload)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	expiresAt := time.Now().Add(oauthAuthFlowTTL)
	state, _, err := model.CreateAuthFlow(model.AuthFlowCreate{
		Purpose:   model.AuthFlowPurposeOAuth,
		Provider:  request.Provider,
		Intent:    request.Intent,
		UserId:    userID,
		SessionId: sessionID,
		Payload:   string(payload),
		ExpiresAt: expiresAt,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if strings.EqualFold(request.Provider, "wechat") && request.Intent == model.AuthFlowIntentLogin {
		session := sessions.Default(c)
		session.Set("oauth_login_state", state)
		session.Set("oauth_language", lang)
		if err := session.Save(); err != nil {
			common.SysError("save wechat oauth state failed: " + err.Error())
			common.ApiErrorI18n(c, i18n.MsgDatabaseError)
			return
		}
	}
	data := gin.H{"flow_token": state, "expires_at": expiresAt.Unix()}
	if telegramFlow != nil {
		data["authorization_url"] = telegramFlow.AuthorizationURL(state)
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    data,
	})
}

func normalizeOAuthRedirectPath(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", true
	}
	if len(raw) > oauthRedirectPathMaxBytes || strings.Contains(raw, `\`) || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" {
		return "", false
	}
	normalized := parsed.String()
	if !strings.HasPrefix(normalized, "/") || strings.HasPrefix(normalized, "//") || strings.Contains(normalized, `\`) {
		return "", false
	}
	return normalized, true
}

func generateLegacyOAuthState(c *gin.Context) {
	session := sessions.Default(c)
	state := common.GetRandomString(12)
	affCode := referralService.ResolveAffiliateCode(c.Query("aff"), referralCookieValue(c))
	if affCode != "" {
		session.Set("aff", affCode)
	}
	session.Set("oauth_state", state)
	session.Set("oauth_language", i18n.GetLangFromContext(c))
	err := session.Save()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    state,
	})
}

// HandleOAuth handles OAuth callback for all standard OAuth providers
func HandleOAuth(c *gin.Context) {
	providerName := c.Param("provider")
	provider := oauth.GetProvider(providerName)
	if provider == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgOAuthUnknownProvider),
		})
		return
	}

	// 1. Validate state (CSRF protection)
	state := strings.TrimSpace(c.Query("state"))
	pendingFlow, err := model.GetAuthFlow(state, model.AuthFlowMatch{
		Purpose:  model.AuthFlowPurposeOAuth,
		Provider: providerName,
	})
	if err != nil {
		if handleLegacyOAuth(c, providerName, provider, state) {
			return
		}
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgOAuthStateInvalid),
		})
		return
	}
	pinStoredOAuthLanguage(c, pendingFlow.Payload)

	consumeMatch := model.AuthFlowMatch{
		Purpose:  model.AuthFlowPurposeOAuth,
		Provider: providerName,
		Intent:   pendingFlow.Intent,
	}
	var bindIdentity service.AuthIdentity
	// Bind and verification callbacks must use the dashboard session that started them.
	if pendingFlow.Intent == model.AuthFlowIntentBind || pendingFlow.Intent == model.AuthFlowIntentVerify {
		identity, ok, identityErr := getOAuthBindSessionIdentity(c)
		if identityErr != nil {
			common.SysError("OAuth bind session validation failed: " + identityErr.Error())
		}
		if !ok || identity.UserID != pendingFlow.UserId || identity.SessionID != pendingFlow.SessionId {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": i18n.T(c, i18n.MsgOAuthStateInvalid),
			})
			return
		}
		bindIdentity = identity
		consumeMatch.UserId = identity.UserID
		consumeMatch.SessionId = identity.SessionID
	} else if pendingFlow.Intent != model.AuthFlowIntentLogin {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	if providerName == "telegram" {
		var telegramPayload oauthFlowPayload
		if err := common.UnmarshalJsonStr(pendingFlow.Payload, &telegramPayload); err != nil || telegramPayload.Telegram == nil {
			writeSecurityOperationError(c, model.ErrAuthFlowInvalid)
			return
		}
		c.Set(oauth.TelegramOAuthFlowContextKey, telegramPayload.Telegram)
	}

	// 3. Check if provider is enabled
	if !provider.IsEnabled() {
		common.ApiErrorI18n(c, i18n.MsgOAuthNotEnabled, providerParams(provider.GetName()))
		return
	}

	// 4. Handle error from provider
	errorCode := c.Query("error")
	if errorCode != "" {
		if _, err := model.ConsumeAuthFlow(state, consumeMatch); err != nil {
			c.JSON(http.StatusForbidden, gin.H{"success": false, "message": i18n.T(c, i18n.MsgOAuthStateInvalid)})
			return
		}
		respondOAuthProviderQueryError(c)
		return
	}
	if pendingFlow.Intent == model.AuthFlowIntentBind {
		handleOAuthBind(c, provider, pendingFlow, state, bindIdentity)
		return
	}

	// 5. Exchange code for token
	code := c.Query("code")
	token, err := provider.ExchangeToken(c.Request.Context(), code, c)
	if err != nil {
		respondOAuthProviderError(c, providerName, err)
		return
	}

	// 6. Get user info
	oauthUser, err := provider.GetUserInfo(c.Request.Context(), token)
	if err != nil {
		respondOAuthProviderError(c, providerName, err)
		return
	}
	if pendingFlow.Intent == model.AuthFlowIntentVerify {
		flow, consumeErr := model.ConsumeAuthFlow(state, consumeMatch)
		if consumeErr != nil {
			c.JSON(http.StatusForbidden, gin.H{"success": false, "message": i18n.T(c, i18n.MsgOAuthStateInvalid)})
			return
		}
		handleOAuthVerification(c, providerName, oauthUser, flow)
		return
	}
	flow, err := model.ConsumeAuthFlow(state, consumeMatch)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": i18n.T(c, i18n.MsgOAuthStateInvalid)})
		return
	}

	// 7. Find or create user
	var payload oauthFlowPayload
	if err := common.UnmarshalJsonStr(flow.Payload, &payload); err != nil {
		common.ApiError(c, err)
		return
	}
	user, migration, err := findOrCreateOAuthUser(c, pendingFlow.Provider, provider, token, oauthUser, payload.AffiliateCode, payload.Language)
	if err != nil {
		if errors.Is(err, model.ErrEmailAlreadyTaken) {
			common.ApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
			return
		}
		if errors.Is(err, oauth.ErrTelegramAccountNotBound) || errors.Is(err, oauth.ErrTelegramOAuthFailed) {
			writeSecurityOperationError(c, err)
			return
		}
		switch err.(type) {
		case *OAuthUserDeletedError:
			common.ApiErrorI18n(c, i18n.MsgOAuthUserDeleted)
		case *OAuthRegistrationDisabledError:
			common.ApiErrorI18n(c, i18n.MsgUserRegisterDisabled)
		case *OAuthEmailAlreadyTakenError:
			common.ApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
		case *OAuthLegacyBindingNotConfirmedError:
			common.ApiErrorI18n(c, i18n.MsgOAuthNotAutoLinked, providerParams(provider.GetName()))
		default:
			if model.IsUserEmailUniqueError(err) {
				common.ApiErrorI18n(c, i18n.MsgUserExists)
				return
			}
			common.ApiError(c, err)
		}
		return
	}

	// 8. Check user status
	if user.Status != common.UserStatusEnabled {
		common.ApiErrorI18n(c, i18n.MsgOAuthUserBanned)
		return
	}

	// 9. Setup login
	extraData := gin.H{}
	if payload.Redirect != "" {
		extraData["redirect"] = payload.Redirect
	}
	setupLoginOrRequire2FAWithMigration(user, c, extraData, migration)
}

func pinStoredOAuthLanguage(c *gin.Context, payloadRaw string) {
	var payload oauthFlowPayload
	if err := common.UnmarshalJsonStr(payloadRaw, &payload); err != nil {
		return
	}
	i18n.PinRequestLanguage(c, payload.Language)
}

func handleLegacyOAuth(c *gin.Context, providerName string, provider oauth.Provider, state string) bool {
	rawSession, exists := c.Get(sessions.DefaultKey)
	if !exists {
		return false
	}
	session, ok := rawSession.(sessions.Session)
	if !ok || session == nil {
		return false
	}
	if state == "" || session.Get("oauth_state") == nil || state != session.Get("oauth_state").(string) {
		return false
	}
	// Pin only after the legacy state matches. A stale oauth_language must not
	// override Accept-Language when this request is rejected as invalid.
	i18n.PinRequestLanguage(c, oauthSessionInterfaceLanguage(c))
	if session.Get("username") != nil {
		identity, ok, err := middleware.GetLegacySessionAuthIdentity(c)
		if err != nil || !ok {
			common.ApiErrorI18n(c, i18n.MsgAuthNotLoggedIn)
			return true
		}
		handleLegacyOAuthBind(c, providerName, provider, identity.UserID)
		return true
	}
	if !provider.IsEnabled() {
		common.ApiErrorI18n(c, i18n.MsgOAuthNotEnabled, providerParams(provider.GetName()))
		return true
	}
	if errorCode := c.Query("error"); errorCode != "" {
		respondOAuthProviderQueryError(c)
		return true
	}
	code := c.Query("code")
	token, err := provider.ExchangeToken(c.Request.Context(), code, c)
	if err != nil {
		handleOAuthError(c, err)
		return true
	}
	oauthUser, err := provider.GetUserInfo(c.Request.Context(), token)
	if err != nil {
		handleOAuthError(c, err)
		return true
	}
	affiliateCode := ""
	if raw := session.Get("aff"); raw != nil {
		if value, ok := raw.(string); ok {
			affiliateCode = value
		}
	}
	user, migration, err := findOrCreateOAuthUser(c, providerName, provider, token, oauthUser, affiliateCode, oauthSessionInterfaceLanguage(c))
	if err != nil {
		if errors.Is(err, model.ErrEmailAlreadyTaken) {
			common.ApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
			return true
		}
		switch err.(type) {
		case *OAuthUserDeletedError:
			common.ApiErrorI18n(c, i18n.MsgOAuthUserDeleted)
		case *OAuthRegistrationDisabledError:
			common.ApiErrorI18n(c, i18n.MsgUserRegisterDisabled)
		case *OAuthEmailAlreadyTakenError:
			common.ApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
		case *OAuthLegacyBindingNotConfirmedError:
			common.ApiErrorI18n(c, i18n.MsgOAuthNotAutoLinked, providerParams(provider.GetName()))
		default:
			if model.IsUserEmailUniqueError(err) {
				common.ApiErrorI18n(c, i18n.MsgUserExists)
				return true
			}
			common.ApiError(c, err)
		}
		return true
	}
	if user.Status != common.UserStatusEnabled {
		common.ApiErrorI18n(c, i18n.MsgOAuthUserBanned)
		return true
	}
	setupLoginOrRequire2FAWithMigration(user, c, nil, migration)
	return true
}

func getOAuthBindSessionIdentity(c *gin.Context) (service.AuthIdentity, bool, error) {
	if identity, ok := middleware.GetSessionAuthIdentity(c); ok {
		return identity, true, nil
	}
	if rawAccessToken, ok := dashboardBearer(c.GetHeader("Authorization")); ok {
		identity, internal, err := service.ParseDashboardAccessToken(rawAccessToken)
		if !internal {
			return service.AuthIdentity{}, false, nil
		}
		if err != nil {
			return service.AuthIdentity{}, false, err
		}
		if _, _, err := service.ValidateLoginSession(identity); err != nil {
			return service.AuthIdentity{}, false, err
		}
		return identity, true, nil
	}
	return middleware.GetLegacySessionAuthIdentity(c)
}

func handleOAuthVerification(c *gin.Context, provider string, oauthUser *oauth.OAuthUser, flow *model.AuthFlow) {
	var payload oauthFlowPayload
	if err := common.UnmarshalJsonStr(flow.Payload, &payload); err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	identity, ok := middleware.GetSessionAuthIdentity(c)
	if !ok {
		writeSecurityOperationError(c, service.ErrAuthTokenInvalid)
		return
	}
	proof, err := service.FinishOAuthVerification(identity, provider, oauthUser.ProviderUserID, payload.Verification)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	recordUserSecurityAudit(c, identity.UserID, "user.security_verify", map[string]any{"method": proof.Method, "scope": proof.Scope, "provider": provider})
	common.ApiSuccess(c, proof)
}

// handleOAuthBind handles binding OAuth account to existing user
func handleOAuthBind(c *gin.Context, provider oauth.Provider, pendingFlow *model.AuthFlow, flowToken string, identity service.AuthIdentity) {
	// Exchange code for token
	code := c.Query("code")
	token, err := provider.ExchangeToken(c.Request.Context(), code, c)
	if err != nil {
		respondOAuthProviderError(c, pendingFlow.Provider, err)
		return
	}

	// Get user info
	oauthUser, err := provider.GetUserInfo(c.Request.Context(), token)
	if err != nil {
		respondOAuthProviderError(c, pendingFlow.Provider, err)
		return
	}
	var payload oauthFlowPayload
	if err := common.UnmarshalJsonStr(pendingFlow.Payload, &payload); err != nil {
		writeSecurityOperationError(c, model.ErrAuthFlowInvalid)
		return
	}
	bindContext, err := common.Marshal(service.AccountBindingContext{Provider: pendingFlow.Provider})
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	if err := service.ValidateFlowAuthorization(identity, service.VerificationOperation{Scope: service.VerificationScopeAccountBind, Context: bindContext}, payload.Authorization); err != nil {
		writeSecurityOperationError(c, err)
		return
	}

	// Check if this OAuth account is already bound (check both new ID and legacy ID)
	if provider.IsUserIDTaken(oauthUser.ProviderUserID) {
		common.ApiErrorI18n(c, i18n.MsgOAuthAlreadyBound, providerParams(provider.GetName()))
		return
	}
	if pendingFlow.Provider == "telegram" {
		if _, err := model.ConsumeAuthFlowWithAction(flowToken, model.AuthFlowMatch{
			Purpose:   model.AuthFlowPurposeOAuth,
			Provider:  pendingFlow.Provider,
			Intent:    model.AuthFlowIntentBind,
			UserId:    pendingFlow.UserId,
			SessionId: pendingFlow.SessionId,
		}, func(tx *gorm.DB, _ *model.AuthFlow) error {
			return model.BindTelegramForSessionWithTx(tx, identity, oauthUser.ProviderUserID)
		}); err != nil {
			if errors.Is(err, model.ErrAuthFlowInvalid) ||
				errors.Is(err, model.ErrAuthFlowExpired) ||
				errors.Is(err, model.ErrAuthFlowConsumed) {
				c.JSON(http.StatusForbidden, gin.H{"success": false, "message": i18n.T(c, i18n.MsgOAuthStateInvalid)})
				return
			}
			writeSecurityOperationError(c, err)
			return
		}
		common.ApiSuccessI18n(c, i18n.MsgOAuthBindSuccess, gin.H{"action": "bind"})
		return
	}

	if _, ok := builtInOAuthExternalIdentityProvider(pendingFlow.Provider); ok {
		var user model.User
		if _, err := model.ConsumeAuthFlowWithAction(flowToken, model.AuthFlowMatch{
			Purpose:   model.AuthFlowPurposeOAuth,
			Provider:  pendingFlow.Provider,
			Intent:    model.AuthFlowIntentBind,
			UserId:    pendingFlow.UserId,
			SessionId: pendingFlow.SessionId,
		}, func(tx *gorm.DB, flow *model.AuthFlow) error {
			if err := tx.First(&user, flow.UserId).Error; err != nil {
				return err
			}
			claimed, err := claimBuiltInOAuthIdentityWithTx(tx, &user, pendingFlow.Provider, oauthUser.ProviderUserID)
			if err != nil {
				return err
			}
			if !claimed {
				return updateOAuthProviderColumnWithTx(tx, user.Id, provider, oauthUser.ProviderUserID, true)
			}
			return nil
		}); err != nil {
			if errors.Is(err, model.ErrAuthFlowInvalid) ||
				errors.Is(err, model.ErrAuthFlowExpired) ||
				errors.Is(err, model.ErrAuthFlowConsumed) {
				c.JSON(http.StatusForbidden, gin.H{"success": false, "message": i18n.T(c, i18n.MsgOAuthStateInvalid)})
				return
			}
			common.ApiError(c, err)
			return
		}
		cacheErr := model.PublishUserAuthCacheAfterCommit(identity.UserID)
		if !continueAfterCommittedUserAuthStateError("OAuth bind", cacheErr) {
			common.ApiError(c, cacheErr)
			return
		}
		bundle, err := service.AdvanceCurrentSessionToUserVersion(identity, "user_security_changed")
		if err != nil {
			common.ApiError(c, err)
			return
		}
		persistLegacyLoginSession(c, &user, bundle.Session)
		data := authRotationData(bundle)
		data["action"] = "bind"
		common.ApiSuccessI18n(c, i18n.MsgOAuthBindSuccess, data)
		return
	}

	if _, err := model.ConsumeAuthFlowWithAction(flowToken, model.AuthFlowMatch{
		Purpose:   model.AuthFlowPurposeOAuth,
		Provider:  pendingFlow.Provider,
		Intent:    model.AuthFlowIntentBind,
		UserId:    pendingFlow.UserId,
		SessionId: pendingFlow.SessionId,
	}, func(tx *gorm.DB, _ *model.AuthFlow) error {
		if genericProvider, ok := provider.(*oauth.GenericOAuthProvider); ok {
			return model.UpdateUserOAuthBindingForSessionWithTx(tx, identity, genericProvider.GetProviderId(), oauthUser.ProviderUserID)
		}
		column := provider.ProviderUserIDColumn()
		if column == "" {
			return model.ErrAccountBindingChanged
		}
		return model.UpdateUserBindColumnForSessionWithTx(tx, identity, column, oauthUser.ProviderUserID)
	}); err != nil {
		if errors.Is(err, model.ErrAuthFlowInvalid) ||
			errors.Is(err, model.ErrAuthFlowExpired) ||
			errors.Is(err, model.ErrAuthFlowConsumed) {
			c.JSON(http.StatusForbidden, gin.H{"success": false, "message": i18n.T(c, i18n.MsgOAuthStateInvalid)})
			return
		}
		writeSecurityOperationError(c, err)
		return
	}

	common.ApiSuccessI18n(c, i18n.MsgOAuthBindSuccess, gin.H{
		"action": "bind",
	})
}

func handleLegacyOAuthBind(c *gin.Context, _ string, _ oauth.Provider, _ int) {
	c.JSON(http.StatusForbidden, gin.H{
		"success": false,
		"code":    "OAUTH_LEGACY_BIND_REMOVED",
		"message": "This account binding flow is no longer available. Start binding again from account security.",
	})
}

// findOrCreateOAuthUser finds existing user or creates new user.
// A pending legacy GitHub rewrite is returned only when login verification must
// finish before the stored login name can be replaced.
func respondOAuthProviderError(c *gin.Context, providerName string, err error) {
	if providerName == "telegram" {
		writeSecurityOperationError(c, err)
		return
	}
	handleOAuthError(c, err)
}

func findOrCreateOAuthUser(c *gin.Context, providerName string, provider oauth.Provider, token *oauth.OAuthToken, oauthUser *oauth.OAuthUser, affiliateCode, language string) (*model.User, *service.LegacyGitHubMigration, error) {
	user := &model.User{}
	// Telegram login only enters an account that is already bound. It must not
	// register a new user or attach the Telegram id to a different account.
	if provider.ProviderUserIDColumn() == "telegram_id" {
		err := provider.FillUserByProviderID(user, oauthUser.ProviderUserID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, oauth.ErrTelegramAccountNotBound
		}
		return user, nil, err
	}

	// Check if user already exists with new ID
	if provider.IsUserIDTaken(oauthUser.ProviderUserID) {
		err := provider.FillUserByProviderID(user, oauthUser.ProviderUserID)
		if err != nil {
			return nil, nil, err
		}
		// Check if user has been deleted
		if user.Id == 0 {
			return nil, nil, &OAuthUserDeletedError{}
		}
		return user, nil, nil
	}

	// GitHub used to store the login name. A numeric legacy value, or a login
	// name whose row is gone, is not the current account. A live login-name row
	// migrates only after verified email evidence, or after login verification.
	if legacyID, ok := oauthUser.Extra["legacy_id"].(string); ok && legacyGitHubLoginName(legacyID) && provider.IsUserIDTaken(legacyID) {
		candidate := &model.User{}
		err := provider.FillUserByProviderID(candidate, legacyID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, err
		}
		if err == nil && candidate.Id != 0 {
			migration, err := confirmLegacyGitHubBinding(c, provider, token, candidate, legacyID, oauthUser.ProviderUserID)
			return candidate, migration, err
		}
	}

	// User doesn't exist, create new user if registration is enabled
	if !common.RegisterEnabled {
		return nil, nil, &OAuthRegistrationDisabledError{}
	}

	// Set up new user
	user.Username = buildOAuthUsername(provider, oauthUser)

	if oauthUser.DisplayName != "" {
		user.DisplayName = oauthUser.DisplayName
	} else if oauthUser.Username != "" {
		user.DisplayName = oauthUser.Username
	} else {
		user.DisplayName = provider.GetName() + " User"
	}
	if oauthUser.Email != "" {
		user.Email = model.NormalizeEmail(oauthUser.Email)
		if err := model.EnsureEmailAvailable(user.Email, 0); err != nil {
			if errors.Is(err, model.ErrEmailAlreadyTaken) {
				return nil, nil, &OAuthEmailAlreadyTakenError{}
			}
			return nil, nil, err
		}
	}
	user.Role = common.RoleCommonUser
	user.Status = common.UserStatusEnabled
	if language == "" {
		language = oauthSessionInterfaceLanguage(c)
	}
	applyStoredInterfaceLanguageToNewUser(user, language)

	// Handle affiliate code
	referralCode := referralService.ResolveAffiliateCode(affiliateCode, c.Query("aff"), referralCookieValue(c))
	bindSource := referralBindSource(affiliateCode)
	if bindSource == "" {
		bindSource = referralBindSource(c.Query("aff"))
	}

	// Use transaction to ensure user creation and OAuth binding are atomic
	if genericProvider, ok := provider.(*oauth.GenericOAuthProvider); ok {
		// Custom provider: create user and binding in a transaction
		err := model.DB.Transaction(func(tx *gorm.DB) error {
			// Create user
			if err := user.InsertWithTx(tx, 0); err != nil {
				return err
			}
			if err := referralService.BindInviteeByCodeWithTx(tx, user.Id, referralCode, bindSource); err != nil {
				return err
			}

			// Create OAuth binding
			binding := &model.UserOAuthBinding{
				UserId:         user.Id,
				ProviderId:     genericProvider.GetProviderId(),
				ProviderUserId: oauthUser.ProviderUserID,
			}
			if err := model.CreateUserOAuthBindingWithTx(tx, binding); err != nil {
				return err
			}

			return nil
		})
		if err != nil {
			return nil, nil, err
		}

		// Perform post-transaction tasks (logs, sidebar config, inviter rewards)
		user.FinalizeOAuthUserCreation(0)
	} else {
		// Built-in provider: create user and update provider ID in a transaction
		err := model.DB.Transaction(func(tx *gorm.DB) error {
			// Create user
			if err := user.InsertWithTx(tx, 0); err != nil {
				return err
			}
			if err := referralService.BindInviteeByCodeWithTx(tx, user.Id, referralCode, bindSource); err != nil {
				return err
			}

			claimed, err := claimBuiltInOAuthIdentityWithTx(tx, user, providerName, oauthUser.ProviderUserID)
			if err != nil {
				return err
			}
			if !claimed {
				if err := updateOAuthProviderColumnWithTx(tx, user.Id, provider, oauthUser.ProviderUserID, false); err != nil {
					return err
				}
			}

			return nil
		})
		if err != nil {
			return nil, nil, err
		}

		// Perform post-transaction tasks
		user.FinalizeOAuthUserCreation(0)
	}

	return user, nil, nil
}

// legacyGitHubLoginName reports a stored GitHub login. Numeric account IDs are
// the current identifier and must not be treated as a login-name binding.
func legacyGitHubLoginName(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return true
		}
	}
	return false
}

func verifiedOAuthEmails(provider oauth.Provider, c *gin.Context, token *oauth.OAuthToken) ([]string, error) {
	emailProvider, ok := provider.(oauth.VerifiedEmailProvider)
	if !ok {
		return nil, errors.New("verified emails unavailable")
	}
	return emailProvider.GetVerifiedEmails(c.Request.Context(), token)
}

// confirmLegacyGitHubBinding rewrites a live login-name binding when the
// provider proves the same verified email. Two-factor and passkey accounts wait
// until that login verification completes. Anything else is refused.
func confirmLegacyGitHubBinding(c *gin.Context, provider oauth.Provider, token *oauth.OAuthToken, candidate *model.User, legacyID, providerUserID string) (*service.LegacyGitHubMigration, error) {
	state, err := model.GetUserVerificationState(candidate.Id)
	if err != nil {
		return nil, err
	}
	if state.HasTwoFA || state.HasPasskey {
		return &service.LegacyGitHubMigration{GitHubID: providerUserID, LegacyID: legacyID}, nil
	}
	reason := ""
	if strings.TrimSpace(candidate.Email) == "" {
		reason = "no_matching_evidence"
	} else {
		emails, emailErr := verifiedOAuthEmails(provider, c, token)
		matched := false
		if emailErr == nil {
			normalized := model.NormalizeEmail(candidate.Email)
			for _, email := range emails {
				if normalized != "" && model.NormalizeEmail(email) == normalized {
					matched = true
					break
				}
			}
		}
		if !matched {
			reason = "no_matching_evidence"
			if emailErr != nil {
				reason = "verified_emails_unavailable"
			}
		}
	}
	if reason != "" {
		recordLegacyGitHubBindingAudit(c, candidate, false, map[string]any{
			"legacy_id": legacyID, "provider_user_id": providerUserID, "reason": reason,
		})
		return nil, &OAuthLegacyBindingNotConfirmedError{}
	}
	written, err := model.MigrateLegacyGitHubBindingWithTx(model.DB, candidate.Id, legacyID, providerUserID)
	if err != nil {
		return nil, err
	}
	if written {
		candidate.GitHubId = providerUserID
		recordLegacyGitHubBindingAudit(c, candidate, true, map[string]any{
			"legacy_id": legacyID, "provider_user_id": providerUserID,
			"verified_email_matched": true,
			"notification_failed":    service.NotifyAccountSecurityChange(candidate.Email, "GitHub binding migrated") != nil,
		})
	}
	return nil, nil
}

func builtInOAuthExternalIdentityProvider(providerName string) (string, bool) {
	switch strings.TrimSpace(strings.ToLower(providerName)) {
	case model.ExternalIdentityProviderGitHub:
		return model.ExternalIdentityProviderGitHub, true
	case model.ExternalIdentityProviderDiscord:
		return model.ExternalIdentityProviderDiscord, true
	case model.ExternalIdentityProviderOIDC:
		return model.ExternalIdentityProviderOIDC, true
	case model.ExternalIdentityProviderLinuxDO:
		return model.ExternalIdentityProviderLinuxDO, true
	default:
		return "", false
	}
}

func claimBuiltInOAuthIdentity(user *model.User, providerName string, providerUserID string) (bool, error) {
	identityProvider, ok := builtInOAuthExternalIdentityProvider(providerName)
	if !ok {
		return false, nil
	}
	return true, user.ClaimExternalIdentity(identityProvider, providerUserID)
}

func claimBuiltInOAuthIdentityWithTx(tx *gorm.DB, user *model.User, providerName string, providerUserID string) (bool, error) {
	identityProvider, ok := builtInOAuthExternalIdentityProvider(providerName)
	if !ok {
		return false, nil
	}
	return true, user.ClaimExternalIdentityWithTx(tx, identityProvider, providerUserID)
}

func updateOAuthProviderColumnWithTx(tx *gorm.DB, userID int, provider oauth.Provider, providerUserID string, bumpAuth bool) error {
	if provider == nil {
		return errors.New("invalid oauth binding")
	}
	return model.UpdateOAuthProviderColumnWithTx(tx, userID, provider.ProviderUserIDColumn(), providerUserID, bumpAuth)
}

// Error types for OAuth
type OAuthUserDeletedError struct{}

func (e *OAuthUserDeletedError) Error() string {
	return "user has been deleted"
}

type OAuthRegistrationDisabledError struct{}

func (e *OAuthRegistrationDisabledError) Error() string {
	return "registration is disabled"
}

type OAuthEmailAlreadyTakenError struct{}

func (e *OAuthEmailAlreadyTakenError) Error() string {
	return "email is already in use"
}

type OAuthLegacyBindingNotConfirmedError struct{}

func (e *OAuthLegacyBindingNotConfirmedError) Error() string {
	return "legacy binding was not confirmed"
}

func respondOAuthProviderQueryError(c *gin.Context) {
	code := strings.ToLower(strings.TrimSpace(c.Query("error")))
	common.SysLog("oauth provider returned error code " + code)
	switch code {
	case "access_denied":
		common.ApiErrorI18n(c, i18n.MsgOAuthAuthorizationCancelled)
	default:
		common.ApiErrorI18n(c, i18n.MsgOAuthAuthorizationFailed)
	}
}

// handleOAuthError handles OAuth errors and returns translated message
func handleOAuthError(c *gin.Context, err error) {
	switch e := err.(type) {
	case *oauth.OAuthError:
		if e.Params != nil {
			common.ApiErrorI18n(c, e.MsgKey, e.Params)
		} else {
			common.ApiErrorI18n(c, e.MsgKey)
		}
	case *oauth.AccessDeniedError:
		if e.UseCatalogDefault() {
			common.ApiErrorI18n(c, i18n.MsgOAuthAccessDenied)
			return
		}
		common.ApiErrorMsg(c, e.Message)
	case *oauth.TrustLevelError:
		common.ApiErrorI18n(c, i18n.MsgOAuthTrustLevelLow)
	default:
		common.ApiError(c, err)
	}
}
