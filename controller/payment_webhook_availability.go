package controller

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

func isPaymentComplianceConfirmed() bool {
	return operation_setting.IsPaymentComplianceConfirmed()
}

func isStripeTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	return isStripeAPISecretConfigured() &&
		stripeWebhookSecret() != "" &&
		stripePriceId() != ""
}

func isStripeAPISecretConfigured() bool {
	secret := stripeAPISecret()
	return strings.HasPrefix(secret, "sk_") || strings.HasPrefix(secret, "rk_")
}

func isStripeSubscriptionEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	return isStripeAPISecretConfigured() && stripeWebhookSecret() != ""
}

func isStripeWebhookConfigured() bool {
	return stripeWebhookSecret() != ""
}

func isStripeWebhookEnabled() bool {
	return isStripeWebhookConfigured()
}

func isCreemTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	products := strings.TrimSpace(setting.CreemProducts)
	return strings.TrimSpace(setting.CreemApiKey) != "" &&
		products != "" &&
		products != "[]" &&
		isCreemWebhookConfigured()
}

func isCreemSubscriptionEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	return strings.TrimSpace(setting.CreemApiKey) != "" && isCreemWebhookConfigured()
}

func isCreemWebhookConfigured() bool {
	if isReferralTestCreemSandboxEnabled() {
		return true
	}
	return strings.TrimSpace(setting.CreemWebhookSecret) != ""
}

func isCreemWebhookEnabled() bool {
	return isCreemWebhookConfigured()
}

func isReferralTestCreemSandboxEnabled() bool {
	return setting.CreemTestMode && common.ReferralTestMode
}

func isWaffoTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	if !setting.WaffoEnabled {
		return false
	}

	return isWaffoWebhookConfigured()
}

func isWaffoWebhookConfigured() bool {
	if setting.WaffoSandbox {
		return strings.TrimSpace(setting.WaffoSandboxApiKey) != "" &&
			strings.TrimSpace(setting.WaffoSandboxPrivateKey) != "" &&
			strings.TrimSpace(setting.WaffoSandboxPublicCert) != ""
	}

	return strings.TrimSpace(setting.WaffoApiKey) != "" &&
		strings.TrimSpace(setting.WaffoPrivateKey) != "" &&
		strings.TrimSpace(setting.WaffoPublicCert) != ""
}

func isWaffoWebhookEnabled() bool {
	return isWaffoWebhookConfigured()
}

func isWaffoPancakeTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	// Presence-of-credentials = enabled. Webhook public keys ship inside
	// the SDK; mode (test/prod) is read from each event.
	return strings.TrimSpace(setting.WaffoPancakeMerchantID) != "" &&
		strings.TrimSpace(setting.WaffoPancakePrivateKey) != "" &&
		strings.TrimSpace(setting.WaffoPancakeStoreID) != "" &&
		strings.TrimSpace(setting.WaffoPancakeProductID) != ""
}

func isWaffoPancakeSubscriptionEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	return strings.TrimSpace(setting.WaffoPancakeMerchantID) != "" &&
		strings.TrimSpace(setting.WaffoPancakePrivateKey) != "" &&
		strings.TrimSpace(setting.WaffoPancakeStoreID) != ""
}

func isWaffoPancakeWebhookConfigured() bool {
	return strings.TrimSpace(setting.WaffoPancakeMerchantID) != "" &&
		strings.TrimSpace(setting.WaffoPancakePrivateKey) != ""
}

func isWaffoPancakeWebhookEnabled() bool {
	// Pancake signs webhook payloads with its platform public keys, selected
	// by the payload mode. Receipt must remain available even if local
	// checkout credentials were rotated or removed, so verified paid events
	// can be placed in manual review instead of being retried forever.
	return true
}

func isEpayTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	return isEpayWebhookConfigured() && len(operation_setting.PayMethods) > 0
}

func isEpayWebhookConfigured() bool {
	return strings.TrimSpace(operation_setting.PayAddress) != "" &&
		strings.TrimSpace(operation_setting.EpayId) != "" &&
		strings.TrimSpace(operation_setting.EpayKey) != ""
}

func isEpayWebhookEnabled() bool {
	return isEpayWebhookConfigured()
}

func stripeAPISecret() string {
	return strings.TrimSpace(setting.StripeApiSecret)
}

func stripeWebhookSecret() string {
	return strings.TrimSpace(setting.StripeWebhookSecret)
}

func stripePriceId() string {
	return strings.TrimSpace(setting.StripePriceId)
}
