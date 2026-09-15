package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
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
	refundRelayBillingAfterPanic(c)
	common.SysLog(fmt.Sprintf("panic detected: %v", recovered))
	common.SysLog(fmt.Sprintf("stacktrace from panic: %s", string(debug.Stack())))
	if c == nil {
		return
	}
	if c.Writer != nil && c.Writer.Written() {
		c.Abort()
		return
	}
	c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
		"error": gin.H{
			"message": "Internal server error",
			"type":    "new_api_panic",
		},
	})
}

func refundRelayBillingAfterPanic(c *gin.Context) {
	if c == nil {
		return
	}
	value, ok := c.Get("relay_info")
	if !ok {
		return
	}
	info, ok := value.(*relaycommon.RelayInfo)
	if !ok || info == nil || info.Billing == nil {
		return
	}
	if err := info.Billing.Refund(c); err != nil {
		common.SysLog("refund billing after panic failed: " + err.Error())
	}
}
