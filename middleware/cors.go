package middleware

import (
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func CORS() gin.HandlerFunc {
	config := cors.DefaultConfig()
	config.AllowCredentials = true
	config.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	config.AllowHeaders = []string{"*"}
	config.AllowOrigins = allowedCORSOrigins()
	if len(config.AllowOrigins) == 0 {
		config.AllowOriginFunc = sameOriginOrNoOrigin
	}
	return cors.New(config)
}

func allowedCORSOrigins() []string {
	raw := os.Getenv("CORS_ALLOWED_ORIGINS")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	origins := make([]string, 0)
	for _, item := range strings.Split(raw, ",") {
		origin := strings.TrimSpace(item)
		if origin != "" && origin != "*" {
			origins = append(origins, origin)
		}
	}
	return origins
}

func sameOriginOrNoOrigin(origin string) bool {
	return strings.TrimSpace(origin) == ""
}

func PoweredBy() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-New-Api-Version", common.Version)
		c.Next()
	}
}
