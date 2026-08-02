package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

func abortWithOpenAiMessage(c *gin.Context, statusCode int, message string, code ...types.ErrorCode) {
	if isPublicImageTaskRequest(c) {
		abortWithImageTaskMessage(c, statusCode, imageTaskMiddlewareErrorCode(statusCode, code...), message)
		return
	}
	codeStr := ""
	if len(code) > 0 {
		codeStr = string(code[0])
	}
	userId := c.GetInt("id")
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"message": common.MessageWithRequestId(message, c.GetString(common.RequestIdKey)),
			"type":    "new_api_error",
			"code":    codeStr,
		},
	})
	c.Abort()
	logger.LogError(c.Request.Context(), fmt.Sprintf("user %d | %s", userId, message))
}

func isPublicImageTaskRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	path := c.Request.URL.Path
	return path == "/v1/image-tasks" || strings.HasPrefix(path, "/v1/image-tasks/")
}

func imageTaskMiddlewareErrorCode(statusCode int, internalCodes ...types.ErrorCode) string {
	var internalCode types.ErrorCode
	if len(internalCodes) > 0 {
		internalCode = internalCodes[0]
	}
	code := "internal_error"
	switch statusCode {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge:
		code = "invalid_request"
	case http.StatusUnauthorized:
		code = "unauthorized"
	case http.StatusForbidden:
		switch internalCode {
		case types.ErrorCodeInsufficientUserQuota:
			code = "insufficient_quota"
		case types.ErrorCodePreConsumeTokenQuotaFailed:
			code = "insufficient_token_quota"
		default:
			code = "access_denied"
		}
	case http.StatusNotFound:
		code = "task_not_found"
	case http.StatusConflict:
		code = "request_conflict"
	case http.StatusTooManyRequests:
		code = "rate_limit_exceeded"
	case http.StatusServiceUnavailable:
		code = "image_task_unavailable"
	}
	return code
}

func abortWithImageTaskMessage(c *gin.Context, statusCode int, code string, message string) {
	if strings.TrimSpace(code) == "" {
		code = "internal_error"
	}
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"message": common.MessageWithRequestId(message, c.GetString(common.RequestIdKey)),
			"type":    "image_task_error",
			"code":    code,
		},
	})
	c.Abort()
	logger.LogError(c.Request.Context(), fmt.Sprintf("user %d | %s", c.GetInt("id"), message))
}

func abortWithMidjourneyMessage(c *gin.Context, statusCode int, code int, description string) {
	c.JSON(statusCode, gin.H{
		"description": description,
		"type":        "new_api_error",
		"code":        code,
	})
	c.Abort()
	logger.LogError(c.Request.Context(), description)
}
