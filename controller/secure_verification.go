package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const (
	secureVerificationMethod2FA     = "2fa"
	secureVerificationMethodPasskey = "passkey"
)

type UniversalVerifyRequest struct {
	Method string `json:"method"`
	Code   string `json:"code,omitempty"`
	Scope  string `json:"scope"`
}

func UniversalVerify(c *gin.Context) {
	identity, ok := middleware.GetSessionAuthIdentity(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": i18n.T(c, i18n.MsgPasskeyAuthMethodUnsupported)})
		return
	}
	var request UniversalVerifyRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if request.Method != secureVerificationMethod2FA {
		common.ApiErrorI18n(c, i18n.MsgSecurePasskeyMustVerifyFlow)
		return
	}
	if !isAllowedSecurityProofScope(request.Scope) {
		common.ApiErrorI18n(c, i18n.MsgSecureScopeUnsupported)
		return
	}
	if strings.TrimSpace(request.Code) == "" {
		common.ApiErrorI18n(c, i18n.MsgSecureCodeEmpty)
		return
	}
	twoFA, err := model.GetTwoFAByUserId(identity.UserID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if twoFA == nil || !twoFA.IsEnabled {
		common.ApiErrorI18n(c, i18n.MsgTwoFANotEnabled)
		return
	}
	if !validateTwoFactorAuth(twoFA, request.Code) {
		common.ApiErrorI18n(c, i18n.MsgSecureVerifyFailed)
		return
	}
	proofToken, expiresAt, err := service.IssueSecurityProof(identity, request.Method, []string{request.Scope})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(identity.UserID, model.LogTypeSystem, "通用安全验证成功 (验证方式: 2FA)")
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": i18n.T(c, i18n.MsgSecureVerifySuccess),
		"data": gin.H{
			"proof_token": proofToken,
			"expires_at":  expiresAt,
			"method":      request.Method,
			"scope":       request.Scope,
		},
	})
}

func isAllowedSecurityProofScope(scope string) bool {
	switch scope {
	case securityProofScopeChannelKeyRead, securityProofScopePasskeyRegister, securityProofScopePasskeyDelete:
		return true
	default:
		return false
	}
}
