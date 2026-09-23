package controller

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type wechatLoginResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

func getWeChatIdByCode(code string) (string, error) {
	if code == "" {
		return "", oauthInvalidParams()
	}
	wechatConnectErr := common.Localized(i18n.MsgOAuthConnectFailed, map[string]any{"Provider": "WeChat"})
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/wechat/user?code=%s", common.WeChatServerAddress, url.QueryEscape(code)), nil)
	if err != nil {
		common.SysLog(fmt.Sprintf("wechat request build failed: %s", err.Error()))
		return "", wechatConnectErr
	}
	req.Header.Set("Authorization", common.WeChatServerToken)
	client := http.Client{
		Timeout: 5 * time.Second,
	}
	httpResponse, err := client.Do(req)
	if err != nil {
		common.SysLog(fmt.Sprintf("wechat connect failed: %s", err.Error()))
		return "", wechatConnectErr
	}
	defer httpResponse.Body.Close()
	var res wechatLoginResponse
	err = common.DecodeJson(httpResponse.Body, &res)
	if err != nil {
		common.SysLog(fmt.Sprintf("wechat response decode failed: %s", err.Error()))
		return "", common.Localized(i18n.MsgOAuthGetUserErr)
	}
	if !res.Success {
		common.SysLog(fmt.Sprintf("wechat login rejected: %s", res.Message))
		return "", common.Localized(i18n.MsgUserVerificationCodeError)
	}
	if res.Data == "" {
		return "", common.Localized(i18n.MsgUserVerificationCodeError)
	}
	return res.Data, nil
}

func WeChatAuth(c *gin.Context) {
	if !common.WeChatAuthEnabled {
		respondOAuthDisabled(c, "WeChat")
		return
	}
	state := strings.TrimSpace(c.Query("state"))
	wechatStateMatch := model.AuthFlowMatch{
		Purpose:  model.AuthFlowPurposeOAuth,
		Provider: "wechat",
		Intent:   model.AuthFlowIntentLogin,
	}
	if state == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgOAuthStateInvalid),
		})
		return
	}
	session := sessions.Default(c)
	savedState, _ := session.Get("oauth_login_state").(string)
	if strings.TrimSpace(savedState) == "" || savedState != state {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgOAuthStateInvalid),
		})
		return
	}
	if _, err := model.ConsumeAuthFlow(state, wechatStateMatch); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgOAuthStateInvalid),
		})
		return
	}
	session.Delete("oauth_login_state")
	_ = session.Save()
	code := c.Query("code")
	wechatId, err := getWeChatIdByCode(code)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	user := model.User{
		WeChatId: wechatId,
	}
	if model.IsWeChatIdAlreadyTaken(wechatId) {
		err := user.FillUserByWeChatId()
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if user.Id == 0 {
			respondOAuthUserDeleted(c)
			return
		}
	} else {
		if common.RegisterEnabled {
			user.Username = model.GenerateNewUserUsername("wechat")
			user.DisplayName = "WeChat User"
			user.Role = common.RoleCommonUser
			user.Status = common.UserStatusEnabled
			applyStoredInterfaceLanguageToNewUser(&user, oauthSessionInterfaceLanguage(c))
			session := sessions.Default(c)
			sessionCode := ""
			if raw := session.Get("aff"); raw != nil {
				if value, ok := raw.(string); ok {
					sessionCode = value
				}
			}
			referralCode := referralService.ResolveAffiliateCode(c.Query("aff"), referralCookieValue(c), sessionCode)
			if err := model.DB.Transaction(func(tx *gorm.DB) error {
				if err := user.InsertWithTx(tx, 0); err != nil {
					return err
				}
				if err := user.ClaimExternalIdentityWithTx(tx, model.ExternalIdentityProviderWeChat, wechatId); err != nil {
					return err
				}
				return referralService.BindInviteeByCodeWithTx(tx, user.Id, referralCode, referralBindSource(strings.TrimSpace(c.Query("aff"))))
			}); err != nil {
				if model.IsUserEmailUniqueError(err) {
					common.ApiErrorI18n(c, i18n.MsgUserExists)
					return
				}
				common.ApiError(c, err)
				return
			}
			user.FinalizeOAuthUserCreation(0)
		} else {
			respondRegisterDisabled(c)
			return
		}
	}

	if user.Status != common.UserStatusEnabled {
		respondOAuthUserBanned(c)
		return
	}
	setupLoginOrRequire2FA(&user, c)
}

type wechatBindRequest struct {
	Code string `json:"code"`
}

func WeChatBind(c *gin.Context) {
	if !common.WeChatAuthEnabled {
		respondOAuthDisabled(c, "WeChat")
		return
	}
	if _, ok := middleware.GetSessionAuthIdentity(c); !ok {
		common.ApiError(c, common.Localized(i18n.MsgOAuthWeChatBindUnsupported))
		return
	}
	var req wechatBindRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	code := req.Code
	wechatId, err := getWeChatIdByCode(code)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if model.IsWeChatIdAlreadyTaken(wechatId) {
		respondOAuthAlreadyBound(c, "WeChat")
		return
	}
	userId := c.GetInt("id")
	if userId == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgAuthLoginRequired),
		})
		return
	}
	user := model.User{Id: userId}
	err = user.FillUserById()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	err = user.ClaimExternalIdentity(model.ExternalIdentityProviderWeChat, wechatId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}
