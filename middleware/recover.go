package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

func RelayPanicRecover() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				HandlePanic(c, recovered)
			}
		}()
		c.Next()
	}
}

// HandlePanic logs diagnostic details server-side but never reflects the raw
// panic value or stack trace to the caller.
func HandlePanic(c *gin.Context, recovered any) {
	common.SysLog(fmt.Sprintf("panic detected: %v", recovered))
	common.SysLog(fmt.Sprintf("stacktrace from panic: %s", string(debug.Stack())))
	c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
		"error": gin.H{
			"message": "Internal server error",
			"type":    "new_api_panic",
		},
	})
}
