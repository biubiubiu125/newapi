package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	passkeysvc "github.com/QuantumNous/new-api/service/passkey"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/protocol"
	webauthnlib "github.com/go-webauthn/webauthn/webauthn"
)

const (
	securityProofScopeChannelKeyRead  = "channel.key.read"
	securityProofScopePasskeyRegister = "passkey.register"
	securityProofScopePasskeyDelete   = "passkey.delete"
)

type passkeyFinishRequest struct {
	FlowToken  string          `json:"flow_token"`
	Credential json.RawMessage `json:"credential"`
}

type passkeyVerifyBeginRequest struct {
	Scope string `json:"scope"`
}

func parsePasskeyFinishRequest(c *gin.Context) (*passkeyFinishRequest, error) {
	var request passkeyFinishRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		return nil, err
	}
	if request.FlowToken == "" || len(request.Credential) == 0 {
		return nil, common.Localized(i18n.MsgPasskeyParamsIncomplete)
	}
	return &request, nil
}

func PasskeyRegisterBegin(c *gin.Context) {
	if !system_setting.GetPasskeySettings().Enabled {
		common.ApiErrorI18n(c, i18n.MsgPasskeyNotEnabled)
		return
	}

	user, err := getAuthenticatedUser(c)
	if err != nil {
		common.ApiErrorWithStatus(c, http.StatusUnauthorized, err)
		return
	}

	if !requirePasskeyRegistrationVerification(c, user.Id) {
		return
	}

	credential, err := model.GetPasskeyByUserID(user.Id)
	if err != nil && !errors.Is(err, model.ErrPasskeyNotFound) {
		common.ApiError(c, err)
		return
	}
	if errors.Is(err, model.ErrPasskeyNotFound) {
		credential = nil
	}

	wa, err := passkeysvc.BuildWebAuthn(c.Request)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	waUser := passkeysvc.NewWebAuthnUser(user, credential)
	var options []webauthnlib.RegistrationOption
	if credential != nil {
		descriptor := credential.ToWebAuthnCredential().Descriptor()
		options = append(options, webauthnlib.WithExclusions([]protocol.CredentialDescriptor{descriptor}))
	}

	creation, sessionData, err := wa.BeginRegistration(waUser, options...)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	identity, ok := middleware.GetSessionAuthIdentity(c)
	if !ok {
		common.ApiError(c, common.Localized(i18n.MsgPasskeyAuthMethodUnsupported))
		return
	}
	flowToken, expiresAt, err := passkeysvc.CreateSessionDataFlow(
		model.AuthFlowPurposePasskeyRegister,
		user.Id,
		identity.SessionID,
		securityProofScopePasskeyRegister,
		sessionData,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"options":    creation,
			"flow_token": flowToken,
			"expires_at": expiresAt,
		},
	})
}

func PasskeyRegisterFinish(c *gin.Context) {
	if !system_setting.GetPasskeySettings().Enabled {
		common.ApiErrorI18n(c, i18n.MsgPasskeyNotEnabled)
		return
	}

	user, err := getAuthenticatedUser(c)
	if err != nil {
		common.ApiErrorWithStatus(c, http.StatusUnauthorized, err)
		return
	}
	if !requirePasskeyRegistrationVerification(c, user.Id) {
		return
	}

	request, err := parsePasskeyFinishRequest(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	parsedCredential, err := protocol.ParseCredentialCreationResponseBytes(request.Credential)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	wa, err := passkeysvc.BuildWebAuthn(c.Request)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	credentialRecord, err := model.GetPasskeyByUserID(user.Id)
	if err != nil && !errors.Is(err, model.ErrPasskeyNotFound) {
		common.ApiError(c, err)
		return
	}
	if errors.Is(err, model.ErrPasskeyNotFound) {
		credentialRecord = nil
	}

	identity, ok := middleware.GetSessionAuthIdentity(c)
	if !ok {
		common.ApiError(c, common.Localized(i18n.MsgPasskeyAuthMethodUnsupported))
		return
	}
	sessionData, _, err := passkeysvc.PopSessionDataFlow(
		request.FlowToken,
		model.AuthFlowPurposePasskeyRegister,
		user.Id,
		identity.SessionID,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	waUser := passkeysvc.NewWebAuthnUser(user, credentialRecord)
	credential, err := wa.CreateCredential(waUser, *sessionData, parsedCredential)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	passkeyCredential := model.NewPasskeyCredentialFromWebAuthn(user.Id, credential)
	if passkeyCredential == nil {
		common.ApiErrorI18n(c, i18n.MsgPasskeyCreateFailed)
		return
	}

	if err := model.UpsertPasskeyCredentialWithAuthVersion(passkeyCredential); !continueAfterCommittedUserAuthStateError("passkey register", err) {
		common.ApiError(c, err)
		return
	}
	bundle, err := service.AdvanceCurrentSessionToUserVersion(identity, "passkey_registered")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !persistAuthRotationLegacyLoginSession(c, user.Id, bundle) {
		return
	}

	recordUserSecurityAudit(c, user.Id, "user.passkey_register", nil)
	common.ApiSuccessI18n(c, i18n.MsgPasskeyRegisterSuccess, authRotationData(bundle))
}

func PasskeyDelete(c *gin.Context) {
	user, err := getAuthenticatedUser(c)
	if err != nil {
		common.ApiErrorWithStatus(c, http.StatusUnauthorized, err)
		return
	}

	if !requirePasskeyDeleteVerification(c, user.Id) {
		return
	}

	identity, ok := middleware.GetSessionAuthIdentity(c)
	if !ok {
		common.ApiError(c, common.Localized(i18n.MsgPasskeyAuthMethodUnsupported))
		return
	}
	if err := model.DeletePasskeyByUserIDWithAuthVersion(user.Id); !continueAfterCommittedUserAuthStateError("passkey delete", err) {
		common.ApiError(c, err)
		return
	}
	bundle, err := service.AdvanceCurrentSessionToUserVersion(identity, "passkey_deleted")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !persistAuthRotationLegacyLoginSession(c, user.Id, bundle) {
		return
	}

	recordUserSecurityAudit(c, user.Id, "user.passkey_delete", nil)
	common.ApiSuccessI18n(c, i18n.MsgPasskeyUnbound, authRotationData(bundle))
}

func PasskeyStatus(c *gin.Context) {
	user, err := getAuthenticatedUser(c)
	if err != nil {
		common.ApiErrorWithStatus(c, http.StatusUnauthorized, err)
		return
	}

	credential, err := model.GetPasskeyByUserID(user.Id)
	if errors.Is(err, model.ErrPasskeyNotFound) {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data": gin.H{
				"enabled": false,
			},
		})
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}

	data := gin.H{
		"enabled":      true,
		"last_used_at": credential.LastUsedAt,
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    data,
	})
}

func PasskeyLoginBegin(c *gin.Context) {
	if !system_setting.GetPasskeySettings().Enabled {
		common.ApiErrorI18n(c, i18n.MsgPasskeyNotEnabled)
		return
	}

	wa, err := passkeysvc.BuildWebAuthn(c.Request)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	assertion, sessionData, err := wa.BeginDiscoverableLogin()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	flowToken, expiresAt, err := passkeysvc.CreateSessionDataFlow(
		model.AuthFlowPurposePasskeyLogin,
		0,
		"",
		"",
		sessionData,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"options":    assertion,
			"flow_token": flowToken,
			"expires_at": expiresAt,
		},
	})
}

func PasskeyLoginFinish(c *gin.Context) {
	if !system_setting.GetPasskeySettings().Enabled {
		common.ApiErrorI18n(c, i18n.MsgPasskeyNotEnabled)
		return
	}

	request, err := parsePasskeyFinishRequest(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	parsedCredential, err := protocol.ParseCredentialRequestResponseBytes(request.Credential)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	wa, err := passkeysvc.BuildWebAuthn(c.Request)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	sessionData, _, err := passkeysvc.PopSessionDataFlow(
		request.FlowToken,
		model.AuthFlowPurposePasskeyLogin,
		0,
		"",
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	handler := func(rawID, userHandle []byte) (webauthnlib.User, error) {
		// 首先通过凭证ID查找用户
		credential, err := model.GetPasskeyByCredentialID(rawID)
		if err != nil {
			return nil, common.Localized(i18n.MsgPasskeyCredentialNotFound)
		}

		// 通过凭证获取用户
		user := &model.User{Id: credential.UserID}
		if err := user.FillUserById(); err != nil {
			return nil, common.Localized(i18n.MsgPasskeyUserInfoFailed)
		}

		if user.Status != common.UserStatusEnabled {
			return nil, common.Localized(i18n.MsgUserDisabled)
		}

		if len(userHandle) > 0 {
			userID, parseErr := strconv.Atoi(string(userHandle))
			if parseErr != nil {
				// 记录异常但继续验证，因为某些客户端可能使用非数字格式
				common.SysLog(fmt.Sprintf("PasskeyLogin: userHandle parse error for credential, length: %d", len(userHandle)))
			} else if userID != user.Id {
				return nil, common.Localized(i18n.MsgPasskeyHandleMismatch)
			}
		}

		return passkeysvc.NewWebAuthnUser(user, credential), nil
	}

	waUser, credential, err := wa.ValidatePasskeyLogin(handler, *sessionData, parsedCredential)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	userWrapper, ok := waUser.(*passkeysvc.WebAuthnUser)
	if !ok {
		common.ApiErrorI18n(c, i18n.MsgPasskeyLoginAbnormal)
		return
	}

	modelUser := userWrapper.ModelUser()
	if modelUser == nil {
		common.ApiErrorI18n(c, i18n.MsgPasskeyLoginAbnormal)
		return
	}

	if modelUser.Status != common.UserStatusEnabled {
		common.ApiErrorI18n(c, i18n.MsgUserDisabled)
		return
	}

	if err := model.UpdatePasskeyAssertionState(modelUser.Id, credential, time.Now()); err != nil {
		common.ApiError(c, err)
		return
	}

	setupLoginOrRequire2FA(modelUser, c)
}

func AdminResetPasskey(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgPasskeyInvalidUserId)
		return
	}

	user := &model.User{Id: id}
	if err := user.FillUserById(); err != nil {
		common.ApiError(c, err)
		return
	}
	myRole := c.GetInt("role")
	if !canManageTargetRole(myRole, user.Role) {
		common.ApiErrorI18n(c, i18n.MsgAuthInsufficientPrivilege)
		return
	}

	if _, err := model.GetPasskeyByUserID(user.Id); err != nil {
		if errors.Is(err, model.ErrPasskeyNotFound) {
			common.ApiErrorI18n(c, i18n.MsgPasskeyNotBound)
			return
		}
		common.ApiError(c, err)
		return
	}

	if err := model.DeletePasskeyByUserIDWithAuthVersion(user.Id); !continueAfterCommittedUserAuthStateError("admin passkey reset", err) {
		common.ApiError(c, err)
		return
	}
	if _, err := model.RevokeAllUserSessions(user.Id, "admin_passkey_reset"); err != nil {
		common.ApiError(c, err)
		return
	}

	recordManageAuditFor(c, user.Id, "user.reset_passkey", map[string]interface{}{
		"username": user.Username,
		"id":       user.Id,
	})
	common.ApiSuccessI18n(c, i18n.MsgPasskeyResetSuccess, nil)
}

func PasskeyVerifyBegin(c *gin.Context) {
	if !system_setting.GetPasskeySettings().Enabled {
		common.ApiErrorI18n(c, i18n.MsgPasskeyNotEnabled)
		return
	}

	user, err := getAuthenticatedUser(c)
	if err != nil {
		common.ApiErrorWithStatus(c, http.StatusUnauthorized, err)
		return
	}
	var request passkeyVerifyBeginRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiError(c, common.Localized(i18n.MsgPasskeyInvalidVerifyRequest))
		return
	}
	if !isAllowedSecurityProofScope(request.Scope) {
		common.ApiError(c, common.Localized(i18n.MsgPasskeyUnsupportedScope))
		return
	}

	credential, err := model.GetPasskeyByUserID(user.Id)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgPasskeyNotBound)
		return
	}

	wa, err := passkeysvc.BuildWebAuthn(c.Request)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	waUser := passkeysvc.NewWebAuthnUser(user, credential)
	assertion, sessionData, err := wa.BeginLogin(waUser)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	identity, ok := middleware.GetSessionAuthIdentity(c)
	if !ok {
		common.ApiError(c, common.Localized(i18n.MsgPasskeyAuthMethodUnsupported))
		return
	}
	flowToken, expiresAt, err := passkeysvc.CreateSessionDataFlow(
		model.AuthFlowPurposePasskeyStepUp,
		user.Id,
		identity.SessionID,
		request.Scope,
		sessionData,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"options":    assertion,
			"flow_token": flowToken,
			"expires_at": expiresAt,
		},
	})
}

func PasskeyVerifyFinish(c *gin.Context) {
	if !system_setting.GetPasskeySettings().Enabled {
		common.ApiErrorI18n(c, i18n.MsgPasskeyNotEnabled)
		return
	}

	user, err := getAuthenticatedUser(c)
	if err != nil {
		common.ApiErrorWithStatus(c, http.StatusUnauthorized, err)
		return
	}

	request, err := parsePasskeyFinishRequest(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	parsedCredential, err := protocol.ParseCredentialRequestResponseBytes(request.Credential)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	wa, err := passkeysvc.BuildWebAuthn(c.Request)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	credential, err := model.GetPasskeyByUserID(user.Id)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgPasskeyNotBound)
		return
	}

	identity, ok := middleware.GetSessionAuthIdentity(c)
	if !ok {
		common.ApiError(c, common.Localized(i18n.MsgPasskeyAuthMethodUnsupported))
		return
	}
	sessionData, scope, err := passkeysvc.PopSessionDataFlow(
		request.FlowToken,
		model.AuthFlowPurposePasskeyStepUp,
		user.Id,
		identity.SessionID,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	waUser := passkeysvc.NewWebAuthnUser(user, credential)
	validatedCredential, err := wa.ValidateLogin(waUser, *sessionData, parsedCredential)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	if err := model.UpdatePasskeyAssertionState(user.Id, validatedCredential, time.Now()); err != nil {
		common.ApiError(c, err)
		return
	}

	proofToken, proofExpiresAt, err := service.IssueSecurityProof(identity, secureVerificationMethodPasskey, []string{scope})
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccessI18n(c, i18n.MsgPasskeyVerifySuccess, gin.H{
		"proof_token": proofToken,
		"expires_at":  proofExpiresAt,
		"method":      secureVerificationMethodPasskey,
		"scope":       scope,
	})
}

func getAuthenticatedUser(c *gin.Context) (*model.User, error) {
	id := c.GetInt("id")
	if id == 0 {
		return nil, common.Localized(i18n.MsgAuthLoginRequired)
	}
	user := &model.User{Id: id}
	if err := user.FillUserById(); err != nil {
		return nil, common.Localized(i18n.MsgAuthLoginRequired)
	}
	if user.Status != common.UserStatusEnabled {
		return nil, common.Localized(i18n.MsgUserDisabled)
	}
	return user, nil
}

func requirePasskeyRegistrationVerification(c *gin.Context, userID int) bool {
	twoFA, err := model.GetTwoFAByUserId(userID)
	if err != nil {
		common.ApiError(c, err)
		return false
	}
	if twoFA == nil || !twoFA.IsEnabled {
		return true
	}
	return middleware.RequireSecurityProof(c, securityProofScopePasskeyRegister, []string{secureVerificationMethod2FA})
}

func requirePasskeyDeleteVerification(c *gin.Context, userID int) bool {
	twoFA, err := model.GetTwoFAByUserId(userID)
	if err != nil {
		common.ApiError(c, err)
		return false
	}
	if twoFA != nil && twoFA.IsEnabled {
		return middleware.RequireSecurityProof(c, securityProofScopePasskeyDelete, []string{secureVerificationMethod2FA})
	}

	_, err = model.GetPasskeyByUserID(userID)
	if err != nil {
		if errors.Is(err, model.ErrPasskeyNotFound) {
			common.ApiErrorI18n(c, i18n.MsgPasskeyNotBound)
			return false
		}
		common.ApiError(c, err)
		return false
	}

	return middleware.RequireSecurityProof(c, securityProofScopePasskeyDelete, []string{secureVerificationMethodPasskey})
}
