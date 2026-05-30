package middleware

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
)

func SetupRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !constant.Setup {
			common.ApiErrorMsg(c, "system is not initialized")
			c.Abort()
			return
		}
		c.Next()
	}
}
