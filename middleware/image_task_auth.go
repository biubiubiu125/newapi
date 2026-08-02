package middleware

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const imageTaskTokenStatusContextKey = "image_task_token_status"

// TokenAuthForImageTaskCreation permits exhausted tokens through authentication
// so a matching idempotency key can replay its accepted task. New work is
// rejected later by RejectExhaustedTokenForImageTaskCreation.
func TokenAuthForImageTaskCreation() func(c *gin.Context) {
	return imageTaskTokenAuth()
}

// TokenAuthForTaskAccess permits an exhausted token to read, acknowledge, or
// cancel work that was already created with that token.
func TokenAuthForTaskAccess() func(c *gin.Context) {
	return imageTaskTokenAuth()
}

func imageTaskTokenAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		key, parts := imageTaskAuthorizationKey(c)
		token, err := model.ValidateUserTokenForTaskAccess(key)
		if token != nil && c.GetInt("id") == 0 {
			c.Set("id", token.UserId)
		}
		if err != nil {
			if errors.Is(err, model.ErrDatabase) || (!errors.Is(err, model.ErrTokenInvalid) && !errors.Is(err, model.ErrTokenNotProvided)) {
				common.SysLog("image task token authentication failed: " + err.Error())
				abortWithOpenAiMessage(c, http.StatusInternalServerError, common.TranslateMessage(c, i18n.MsgDatabaseError))
				return
			}
			abortWithOpenAiMessage(c, http.StatusUnauthorized, common.TranslateMessage(c, i18n.MsgTokenInvalid))
			return
		}
		if !applyImageTaskTokenContext(c, token, parts...) {
			return
		}
		c.Set(imageTaskTokenStatusContextKey, token.Status)
		c.Next()
	}
}

func imageTaskAuthorizationKey(c *gin.Context) (string, []string) {
	key := c.GetHeader("Authorization")
	if strings.HasPrefix(key, "Bearer ") || strings.HasPrefix(key, "bearer ") {
		key = strings.TrimSpace(key[7:])
	}
	key = strings.TrimPrefix(key, "sk-")
	parts := strings.Split(key, "-")
	if len(parts) > 0 {
		key = parts[0]
	}
	return key, parts
}

func applyImageTaskTokenContext(c *gin.Context, token *model.Token, parts ...string) bool {
	allowIPs := token.GetIpLimits()
	if len(allowIPs) > 0 {
		clientIP := c.ClientIP()
		ip := net.ParseIP(clientIP)
		if ip == nil {
			abortWithOpenAiMessage(c, http.StatusForbidden, "unable to parse client IP")
			return false
		}
		if !common.IsIpInCIDRList(ip, allowIPs) {
			abortWithOpenAiMessage(c, http.StatusForbidden, "client IP is not allowed", types.ErrorCodeAccessDenied)
			return false
		}
	}

	userCache, err := model.GetUserCache(token.UserId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			abortWithOpenAiMessage(c, http.StatusUnauthorized, common.TranslateMessage(c, i18n.MsgTokenInvalid))
			return false
		}
		common.SysLog(fmt.Sprintf("image task token user cache error for user %d: %v", token.UserId, err))
		abortWithOpenAiMessage(c, http.StatusInternalServerError, common.TranslateMessage(c, i18n.MsgDatabaseError))
		return false
	}
	if userCache.Status != common.UserStatusEnabled {
		abortWithOpenAiMessage(c, http.StatusForbidden, common.TranslateMessage(c, i18n.MsgAuthUserBanned))
		return false
	}
	userCache.WriteContext(c)

	userGroup := userCache.Group
	if token.Group != "" {
		if _, ok := service.GetUserUsableGroups(userGroup)[token.Group]; !ok {
			abortWithOpenAiMessage(c, http.StatusForbidden, fmt.Sprintf("no access to group %s", token.Group))
			return false
		}
		if !ratio_setting.ContainsGroupRatio(token.Group) && token.Group != "auto" {
			abortWithOpenAiMessage(c, http.StatusForbidden, fmt.Sprintf("group %s is unavailable", token.Group))
			return false
		}
		userGroup = token.Group
	}
	common.SetContextKey(c, constant.ContextKeyUsingGroup, userGroup)

	if err := SetupContextForToken(c, token, parts...); err != nil {
		return false
	}
	return true
}

func RejectExhaustedTokenForImageTaskCreation() func(c *gin.Context) {
	return func(c *gin.Context) {
		if c.GetBool("token_unlimited_quota") {
			c.Next()
			return
		}
		if c.GetInt(imageTaskTokenStatusContextKey) == common.TokenStatusExhausted || c.GetInt("token_quota") <= 0 {
			abortWithOpenAiMessage(
				c,
				http.StatusForbidden,
				common.TranslateMessage(c, i18n.MsgQuotaInsufficient),
				types.ErrorCodePreConsumeTokenQuotaFailed,
			)
			return
		}
		c.Next()
	}
}
