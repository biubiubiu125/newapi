package controller

import (
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
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
	_ = c
	return paymentPublicBaseURL()
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
	return service.IsLocalCallbackAddress(raw)
}
