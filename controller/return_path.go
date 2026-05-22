package controller

import (
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

func paymentReturnPath(suffix string) string {
	base := paymentPublicBaseURL()
	return base + common.ThemeAwarePath(suffix)
}

func paymentWalletReturnSuffix(status string, provider string, orderType string, tradeNo string) string {
	values := url.Values{}
	values.Set("show_history", "true")
	values.Set("pay", status)
	values.Set("payment_provider", provider)
	values.Set("order_type", orderType)
	if strings.TrimSpace(tradeNo) != "" {
		values.Set("trade_no", strings.TrimSpace(tradeNo))
	}
	return "/console/topup?" + values.Encode()
}

func paymentWalletReturnPath(status string, provider string, orderType string, tradeNo string) string {
	return paymentReturnPath(paymentWalletReturnSuffix(status, provider, orderType, tradeNo))
}

func paymentWalletReturnPathForRequest(c *gin.Context, status string, provider string, orderType string, tradeNo string) string {
	return paymentReturnPathForRequest(c, paymentWalletReturnSuffix(status, provider, orderType, tradeNo))
}

func paymentReturnPathForRequest(c *gin.Context, suffix string) string {
	base := paymentPublicBaseURLForRequest(c)
	if base == "" {
		return ""
	}
	return base + common.ThemeAwarePath(suffix)
}

func paymentPublicBaseURLForRequest(c *gin.Context) string {
	if base := paymentPublicBaseURL(); base != "" {
		return base
	}
	if c == nil || c.Request == nil {
		return ""
	}
	scheme := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto"))
	if scheme == "" {
		scheme = strings.TrimSpace(c.GetHeader("X-Forwarded-Scheme"))
	}
	if scheme == "" {
		if c.Request.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	host := strings.TrimSpace(c.GetHeader("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(c.Request.Host)
	}
	if host == "" {
		return ""
	}
	if idx := strings.Index(host, ","); idx >= 0 {
		host = strings.TrimSpace(host[:idx])
	}
	base := strings.ToLower(strings.TrimSpace(scheme)) + "://" + host
	if isLocalPaymentBaseURL(base) {
		return ""
	}
	return strings.TrimRight(base, "/")
}

func paymentPublicBaseURL() string {
	base := strings.TrimSpace(operation_setting.CustomCallbackAddress)
	if base == "" || isLocalPaymentBaseURL(base) {
		base = strings.TrimSpace(system_setting.ServerAddress)
	}
	if isLocalPaymentBaseURL(base) {
		return ""
	}
	return strings.TrimRight(base, "/")
}

func isLocalPaymentBaseURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return true
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "" || host == "localhost" || host == "127.0.0.1" || host == "::1"
}
