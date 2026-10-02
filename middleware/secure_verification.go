package middleware

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// SecureVerificationRequired protects channel key disclosure. Other sensitive
// operations validate their narrower proof scopes in their controller.
func SecureVerificationRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		channelID, err := strconv.Atoi(c.Param("id"))
		if err != nil || channelID <= 0 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "code": "SECURITY_CONTEXT_INVALID", "message": service.ErrVerificationContextInvalid.Error()})
			return
		}
		context, err := common.Marshal(service.ChannelKeyReadContext{ChannelID: channelID})
		if err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		if RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeChannelKeyRead, Context: context}) == nil {
			return
		}
		c.Set("secure_verified", true)
		c.Next()
	}
}

// RequireSecurityProof validates a proof against the authenticated dashboard
// session or scoped access token and writes the shared proof error contract on
// failure.
func RequireSecurityProof(c *gin.Context, operation service.VerificationOperation) *model.AuthFlowAuthorization {
	identity, ok := GetStepUpIdentity(c)
	if !ok {
		securityProofError(c, "SECURITY_PROOF_INVALID", i18n.T(c, i18n.MsgSecureInvalid))
		return nil
	}
	raw := strings.TrimSpace(c.GetHeader("X-Security-Proof"))
	if raw == "" {
		securityProofError(c, "SECURITY_PROOF_REQUIRED", i18n.T(c, i18n.MsgSecureRequired))
		return nil
	}
	authorization, err := service.ConsumeOperationProof(raw, identity, operation)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrAuthTokenExpired):
			securityProofError(c, "SECURITY_PROOF_EXPIRED", i18n.T(c, i18n.MsgSecureExpired))
		case errors.Is(err, service.ErrProofScope):
			securityProofError(c, "SECURITY_PROOF_SCOPE_MISMATCH", i18n.T(c, i18n.MsgSecureScopeMismatch))
		case errors.Is(err, service.ErrVerificationContextInvalid):
			c.Set("security_error_code", "SECURITY_CONTEXT_INVALID")
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "code": "SECURITY_CONTEXT_INVALID", "message": service.ErrVerificationContextInvalid.Error()})
		case errors.Is(err, service.ErrProofMethod):
			securityProofError(c, "SECURITY_PROOF_METHOD_MISMATCH", i18n.T(c, i18n.MsgSecureMethodMismatch))
		case errors.Is(err, service.ErrVerificationUnavailable):
			securityProofError(c, "SECURITY_METHOD_UNAVAILABLE", err.Error())
		case errors.Is(err, service.ErrProofConsumed):
			securityProofError(c, "SECURITY_PROOF_CONSUMED", err.Error())
		case errors.Is(err, service.ErrProofContext):
			securityProofError(c, "SECURITY_PROOF_CONTEXT_MISMATCH", err.Error())
		case errors.Is(err, service.ErrVerificationForbidden):
			securityProofError(c, "SECURITY_ACTION_FORBIDDEN", err.Error())
		case errors.Is(err, service.ErrAuthTokenInvalid), errors.Is(err, model.ErrAuthFlowInvalid):
			securityProofError(c, "SECURITY_PROOF_INVALID", i18n.T(c, i18n.MsgSecureInvalid))
		default:
			c.Set("security_error_code", "AUTH_INTERNAL_ERROR")
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "authentication state could not be verified",
				"code":    "AUTH_INTERNAL_ERROR",
			})
			c.Abort()
		}
		return nil
	}
	return authorization
}

func securityProofError(c *gin.Context, code, message string) {
	c.Set("security_error_code", code)
	c.JSON(http.StatusForbidden, gin.H{
		"success": false,
		"message": message,
		"code":    code,
	})
	c.Abort()
}
