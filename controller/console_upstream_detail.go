package controller

import (
	"strings"
	"unicode"

	"github.com/QuantumNous/new-api/i18n"

	"github.com/gin-gonic/gin"
)

// consoleUpstreamDetail is the admin JSON text for an upstream failure.
// Chinese catalogs drop an untranslated detail and keep the fallback sentence.
// Chinese details and non-Chinese catalogs stay unchanged.
func consoleUpstreamDetail(c *gin.Context, raw, fallbackKey string) string {
	text := strings.TrimSpace(raw)
	lang := i18n.GetLangFromContext(c)
	if text == "" || ((lang == i18n.LangZhCN || lang == i18n.LangZhTW) && !consoleTextHasHan(text)) {
		return i18n.T(c, fallbackKey)
	}
	return text
}

func consoleTextHasHan(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}
