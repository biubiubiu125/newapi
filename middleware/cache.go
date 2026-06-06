package middleware

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

func frontendCacheVersion() string {
	return common.GetEnvOrDefaultString("FRONTEND_CACHE_VERSION", common.Version)
}

func Cache() func(c *gin.Context) {
	return func(c *gin.Context) {
		if c.Request.RequestURI == "/" {
			c.Header("Cache-Control", "no-cache")
		} else {
			c.Header("Cache-Control", "max-age=604800") // one week
		}
		c.Header("Cache-Version", frontendCacheVersion())
		c.Next()
	}
}
