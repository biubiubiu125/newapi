package controller

import (
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

var (
	failedChannelChecksPattern  = regexp.MustCompile(`(?i)^failed channel checks: (\d+)$`)
	failedChannelUpdatesPattern = regexp.MustCompile(`(?i)^failed channel updates: (\d+)$`)
)

func consoleStoredChinese(c *gin.Context) bool {
	lang := i18n.GetLangFromContext(c)
	return lang == i18n.LangZhCN || lang == i18n.LangZhTW
}

func consoleHanDetail(detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" || !consoleTextHasHan(detail) {
		return ""
	}
	return "：" + detail
}

func consoleStoredTaskError(c *gin.Context, raw string, fallback bool) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	if !consoleStoredChinese(c) {
		return text
	}
	if match := failedChannelChecksPattern.FindStringSubmatch(text); match != nil {
		return i18n.T(c, i18n.MsgSystemTaskFailedChannelChecks, map[string]any{"Count": match[1]})
	}
	if match := failedChannelUpdatesPattern.FindStringSubmatch(text); match != nil {
		return i18n.T(c, i18n.MsgSystemTaskFailedChannelUpdates, map[string]any{"Count": match[1]})
	}
	if rest, ok := cutStoredPrefix(text, "runtime channel cache refresh failed:"); ok {
		return i18n.T(c, i18n.MsgSystemTaskCacheRefreshFailed) + consoleHanDetail(rest)
	}
	if rest, ok := cutStoredPrefix(text, "batch apply persisted but runtime cache refresh failed:"); ok {
		return i18n.T(c, i18n.MsgSystemTaskBatchCacheRefreshFailed) + consoleHanDetail(rest)
	}
	if rest, ok := cutStoredPrefix(text, "failed to persist task terminal result:"); ok {
		return i18n.T(c, i18n.MsgSystemTaskPersistFailed) + consoleHanDetail(rest)
	}
	switch strings.ToLower(text) {
	case "context canceled", "context cancelled", "task cancelled by user", "task canceled by user":
		return i18n.T(c, i18n.MsgSystemTaskCancelled)
	case "context deadline exceeded":
		return i18n.T(c, i18n.MsgSystemTaskTimedOut)
	}
	if consoleTextHasHan(text) {
		return text
	}
	if fallback {
		return i18n.T(c, i18n.MsgSystemTaskFailed)
	}
	return ""
}

func cutStoredPrefix(text, prefix string) (string, bool) {
	if len(text) < len(prefix) || !strings.EqualFold(text[:len(prefix)], prefix) {
		return "", false
	}
	return text[len(prefix):], true
}

func localizeSystemTaskResponse(c *gin.Context, resp model.SystemTaskResponse) model.SystemTaskResponse {
	resp.Error = consoleStoredTaskError(c, resp.Error, true)
	result, ok := resp.Result.(map[string]any)
	if !ok {
		return resp
	}
	raw, ok := result["runtime_cache_refresh_error"].(string)
	if !ok {
		return resp
	}
	next := make(map[string]any, len(result))
	for key, value := range result {
		next[key] = value
	}
	filtered := consoleStoredTaskError(c, raw, false)
	if filtered == "" {
		delete(next, "runtime_cache_refresh_error")
	} else {
		next["runtime_cache_refresh_error"] = filtered
	}
	resp.Result = next
	return resp
}

var paymentOrphanReasonKeys = map[string]string{
	"BEpusdt subscription payment requires manual review after payment succeeded":                                      i18n.MsgPaymentOrphanBepusdtSubscriptionReview,
	"Epay subscription payment requires manual review after payment succeeded":                                         i18n.MsgPaymentOrphanEpaySubscriptionReview,
	"Epay top-up payment requires manual review after payment succeeded":                                               i18n.MsgPaymentOrphanEpayTopupReview,
	"BEpusdt top-up payment requires manual review after payment succeeded":                                            i18n.MsgPaymentOrphanBepusdtTopupReview,
	"Creem payment succeeded but local reference_id is missing":                                                        i18n.MsgPaymentOrphanCreemReferenceMissing,
	"Creem payment facts are invalid after payment succeeded":                                                          i18n.MsgPaymentOrphanCreemFactsInvalid,
	"Creem subscription payment requires manual review after payment succeeded":                                        i18n.MsgPaymentOrphanCreemSubscriptionReview,
	"Creem payment succeeded but no matching subscription order exists; requires manual review after payment succeeded": i18n.MsgPaymentOrphanCreemSubscriptionMissing,
	"Creem payment succeeded but the local top-up order is missing; requires manual review after payment succeeded":   i18n.MsgPaymentOrphanCreemTopupMissing,
	"Creem top-up payment requires manual review after payment succeeded":                                              i18n.MsgPaymentOrphanCreemTopupReview,
	"Stripe payment facts are invalid after payment succeeded":                                                         i18n.MsgPaymentOrphanStripeFactsInvalid,
	"Stripe subscription payment requires manual review after payment succeeded":                                       i18n.MsgPaymentOrphanStripeSubscriptionReview,
	"subscription purchase limit reached after payment succeeded":                                                      i18n.MsgPaymentOrphanPurchaseLimit,
	"Stripe subscription payment succeeded but payment facts do not match the order":                                   i18n.MsgPaymentOrphanStripeSubscriptionMismatch,
	"local order not found after stripe payment succeeded":                                                             i18n.MsgPaymentOrphanStripeLocalOrderMissing,
	"Stripe top-up payment succeeded but payment facts do not match the order":                                         i18n.MsgPaymentOrphanStripeTopupMismatch,
	"Stripe top-up payment requires manual review after payment succeeded":                                             i18n.MsgPaymentOrphanStripeTopupReview,
	"Waffo top-up payment requires manual review after payment succeeded":                                              i18n.MsgPaymentOrphanWaffoTopupReview,
	"Waffo Pancake payment succeeded but the local order could not be resolved; requires manual review after payment succeeded": i18n.MsgPaymentOrphanWaffoPancakeUnresolved,
	"Waffo Pancake subscription payment requires manual review after payment succeeded":                                i18n.MsgPaymentOrphanWaffoPancakeSubscriptionReview,
	"Waffo Pancake top-up payment requires manual review after payment succeeded":                                      i18n.MsgPaymentOrphanWaffoPancakeTopupReview,
	"Waffo Pancake webhook environment does not match the receiving endpoint":                                          i18n.MsgPaymentOrphanWaffoPancakeEnvMismatch,
	"Waffo Pancake webhook store does not match the configured merchant store":                                         i18n.MsgPaymentOrphanWaffoPancakeStoreMismatch,
	"Waffo Pancake top-up payment succeeded but the local order could not be resolved; requires manual review after payment succeeded": i18n.MsgPaymentOrphanWaffoPancakeTopupUnresolved,
}

func consolePaymentOrphanReason(c *gin.Context, raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	if !consoleStoredChinese(c) {
		return text
	}
	if rest, ok := cutStoredPrefix(text, "local order insert failed:"); ok {
		return i18n.T(c, i18n.MsgPaymentOrphanLocalOrderInsertFailed) + consoleHanDetail(rest)
	}
	if key, ok := paymentOrphanReasonKeys[text]; ok {
		return i18n.T(c, key)
	}
	if consoleTextHasHan(text) {
		return text
	}
	return i18n.T(c, i18n.MsgPaymentOrphanReviewRequired)
}

func consolePaymentOrphanError(c *gin.Context, raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	if !consoleStoredChinese(c) {
		return text
	}
	if consoleTextHasHan(text) {
		return text
	}
	return ""
}
