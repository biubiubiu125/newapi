package middleware

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
)

func SetupRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !constant.Setup {
			common.ApiErrorI18n(c, i18n.MsgSetupNotInitialized)
			c.Abort()
			return
		}
		c.Next()
	}
}
