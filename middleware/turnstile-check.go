package middleware

import (
	"net/http"
	"net/url"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
)

type turnstileCheckResponse struct {
	Success bool `json:"success"`
}

// PostTurnstileForm is replaced in tests to simulate network failures.
var PostTurnstileForm = http.PostForm

func TurnstileCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		if common.TurnstileCheckEnabled {
			response := c.Query("turnstile")
			if response == "" {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": i18n.T(c, i18n.MsgTurnstileTokenEmpty),
				})
				c.Abort()
				return
			}
			rawRes, err := PostTurnstileForm("https://challenges.cloudflare.com/turnstile/v0/siteverify", url.Values{
				"secret":   {common.TurnstileSecretKey},
				"response": {response},
				"remoteip": {common.GetClientIP(c)},
			})
			if err != nil {
				common.SysLog("turnstile verify request failed: " + err.Error())
				common.ApiErrorI18n(c, i18n.MsgTurnstileNetworkFailed)
				c.Abort()
				return
			}
			defer rawRes.Body.Close()
			var res turnstileCheckResponse
			err = common.DecodeJson(rawRes.Body, &res)
			if err != nil {
				common.SysLog("turnstile verify decode failed: " + err.Error())
				common.ApiErrorI18n(c, i18n.MsgTurnstileDecodeFailed)
				c.Abort()
				return
			}
			if !res.Success {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": i18n.T(c, i18n.MsgTurnstileVerifyFailed),
				})
				c.Abort()
				return
			}
		}
		c.Next()
	}
}
