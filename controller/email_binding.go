package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type emailBindRequest struct {
	Email     string `json:"email"`
	Code      string `json:"code"`
	FlowToken string `json:"flow_token"`
	NewCode   string `json:"new_code"`
	OldCode   string `json:"old_code"`
}

func EmailBindStart(c *gin.Context) {
	identity, ok := middleware.GetSessionAuthIdentity(c)
	if !ok {
		writeSecurityOperationError(c, service.ErrAuthTokenInvalid)
		return
	}
	succeeded, notificationFailed := false, false
	defer func() {
		recordUserSecurityAudit(c, identity.UserID, "user.binding_start", map[string]any{"provider": "email", "success": succeeded, "notification_failed": notificationFailed})
	}()
	var request emailBindRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		writeSecurityOperationError(c, service.ErrVerificationContextInvalid)
		return
	}
	email, err := service.ValidateAccountEmail(request.Email)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	context, err := common.Marshal(service.AccountBindingContext{Provider: "email", Email: email})
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	authorization := middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeAccountBind, Context: context})
	if authorization == nil {
		return
	}
	data, err := service.StartEmailBinding(identity, authorization, email)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	succeeded, notificationFailed = true, data.NotificationWarning
	common.ApiSuccess(c, data)
}

func EmailBindResend(c *gin.Context) {
	identity, ok := middleware.GetSessionAuthIdentity(c)
	if !ok {
		writeSecurityOperationError(c, service.ErrAuthTokenInvalid)
		return
	}
	succeeded := false
	defer func() {
		recordUserSecurityAudit(c, identity.UserID, "user.email_binding_resend", map[string]any{"success": succeeded})
	}()
	var request emailBindRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || request.FlowToken == "" {
		writeSecurityOperationError(c, model.ErrAuthFlowInvalid)
		return
	}
	data, err := service.ResendAccountEmailBinding(identity, request.FlowToken)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	succeeded = true
	common.ApiSuccess(c, data)
}

func EmailBind(c *gin.Context) {
	var request emailBindRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if c.GetBool("use_access_token") {
		common.ApiErrorI18n(c, i18n.MsgUserEmailMethodUnsupported)
		return
	}
	identity, ok := middleware.GetSessionAuthIdentity(c)
	if !ok {
		common.ApiErrorI18n(c, i18n.MsgUserEmailMethodUnsupported)
		return
	}
	succeeded, notificationFailed := false, false
	defer func() {
		recordUserSecurityAudit(c, identity.UserID, "user.binding_bind", map[string]any{"provider": "email", "success": succeeded, "notification_failed": notificationFailed})
	}()
	if request.FlowToken == "" {
		succeeded = bindLegacyEmail(c, identity.UserID, request)
		return
	}
	state, err := service.FinishEmailBinding(identity, request.FlowToken, request.NewCode, request.OldCode)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	succeeded = true
	notificationFailed = service.NotifyAccountSecurityChange(state.CurrentEmail, "Email address changed") != nil
	if err := service.NotifyAccountSecurityChange(state.Email, "Email address confirmed"); err != nil {
		notificationFailed = true
	}
	if err := model.PublishUserAuthCache(identity.UserID); err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"notification_warning": notificationFailed}})
}

func bindLegacyEmail(c *gin.Context, userID int, request emailBindRequest) bool {
	email := model.NormalizeUserEmail(request.Email)
	if !common.VerifyCodeWithKey(email, request.Code, common.EmailVerificationPurpose) {
		common.ApiErrorI18n(c, i18n.MsgUserVerificationCodeError)
		return false
	}
	if userID <= 0 {
		common.ApiErrorI18n(c, i18n.MsgAuthNotLoggedIn)
		return false
	}
	user := model.User{Id: userID}
	if err := user.FillUserById(); err != nil {
		common.ApiError(c, err)
		return false
	}
	if exists, err := model.IsLoginIdentifierTakenByOther(user.Username, email, user.Id); err != nil {
		common.ApiError(c, err)
		return false
	} else if exists {
		common.ApiErrorI18n(c, i18n.MsgUserExists)
		return false
	}
	user.Email = email
	if err := user.Update(false); err != nil {
		if model.IsUserEmailUniqueError(err) {
			common.ApiErrorI18n(c, i18n.MsgUserExists)
			return false
		}
		common.ApiError(c, err)
		return false
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
	return true
}
