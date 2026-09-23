package i18n

import (
	"embed"
	"io/fs"
	"strings"
	"sync"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

const (
	LangZhCN    = "zh-CN"
	LangZhTW    = "zh-TW"
	LangEn      = "en"
	LangFr      = "fr"
	LangJa      = "ja"
	LangRu      = "ru"
	LangVi      = "vi"
	DefaultLang = LangZhCN // Fallback to simplified Chinese if language not supported
)

//go:embed locales/*.yaml
var localeFS embed.FS

var (
	bundle      *i18n.Bundle
	localizers  = make(map[string]*i18n.Localizer)
	mu          sync.RWMutex
	initMu      sync.Mutex
	initialized bool
	// emptyLocalizer is used when Init has not loaded a catalog yet.
	emptyLocalizer = i18n.NewLocalizer(i18n.NewBundle(language.English), LangEn)
)

// Init initializes the i18n bundle and loads all translation files.
// A failed load stays retryable so a later successful catalog can still be used.
func Init() error {
	return initFromFS(localeFS)
}

func initFromFS(fsys fs.FS) error {
	initMu.Lock()
	defer initMu.Unlock()
	if initialized {
		return nil
	}

	b := i18n.NewBundle(language.Chinese)
	b.RegisterUnmarshalFunc("yaml", yaml.Unmarshal)

	entries, err := fs.ReadDir(fsys, "locales")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		if _, err := b.LoadMessageFileFS(fsys, "locales/"+entry.Name()); err != nil {
			return err
		}
	}

	locs := make(map[string]*i18n.Localizer, len(SupportedLanguages()))
	for _, lang := range SupportedLanguages() {
		if lang == LangZhCN || lang == LangZhTW || lang == LangEn {
			locs[lang] = i18n.NewLocalizer(b, lang)
			continue
		}
		locs[lang] = i18n.NewLocalizer(b, lang, LangEn)
	}

	mu.Lock()
	bundle = b
	localizers = locs
	mu.Unlock()
	common.TranslateMessage = T
	common.DashboardLanguage = GetLangFromContext
	initialized = true
	return nil
}

// GetLocalizer returns a localizer for the specified language.
// If Init has not loaded a catalog yet, it returns a non-nil empty localizer
// so Translate can fall back to the message key instead of panicking.
func GetLocalizer(lang string) *i18n.Localizer {
	lang = normalizeLang(lang)

	mu.RLock()
	if loc, ok := localizers[lang]; ok && loc != nil {
		mu.RUnlock()
		return loc
	}
	b := bundle
	mu.RUnlock()

	if b == nil {
		return emptyLocalizer
	}

	// Create new localizer for unknown language (fallback to default)
	mu.Lock()
	defer mu.Unlock()

	if loc, ok := localizers[lang]; ok && loc != nil {
		return loc
	}
	if bundle == nil {
		return emptyLocalizer
	}
	if localizers == nil {
		localizers = make(map[string]*i18n.Localizer)
	}

	loc := i18n.NewLocalizer(bundle, lang, DefaultLang)
	localizers[lang] = loc
	return loc
}

// T translates a message key using the language from gin context
func T(c *gin.Context, key string, args ...map[string]any) string {
	lang := GetLangFromContext(c)
	return Translate(lang, key, args...)
}

// ProtocolMessage always returns the English catalog. OpenAI-compatible and
// public image-task envelopes must stay English regardless of Accept-Language.
func ProtocolMessage(key string, args ...map[string]any) string {
	return Translate(LangEn, key, args...)
}

// Translate translates a message key for the specified language
func Translate(lang, key string, args ...map[string]any) string {
	lang = normalizeLang(lang)
	loc := GetLocalizer(lang)
	if loc == nil {
		return key
	}

	config := &i18n.LocalizeConfig{
		MessageID: key,
	}

	if len(args) > 0 && args[0] != nil {
		config.TemplateData = publicTemplateData(lang, args[0])
	}

	msg, err := loc.Localize(config)
	if err != nil {
		// Return key as fallback if translation not found
		return key
	}
	return msg
}

// publicTemplateData drops an untranslated Error detail on Chinese catalogs.
// The Chinese sentence stays; English, French, and the protocol catalog keep it.
// English clauses glued onto a Chinese reason are removed, and one brand or
// format token such as "不是合法 JSON" is kept.
func publicTemplateData(lang string, data map[string]any) map[string]any {
	if lang != LangZhCN && lang != LangZhTW {
		return data
	}
	var out map[string]any
	for key, value := range data {
		text, ok := value.(string)
		if !ok || !strings.EqualFold(key, "Error") {
			continue
		}
		cleaned := common.SanitizeChineseConsoleText(text)
		if cleaned != "" && cleaned == strings.TrimSpace(text) {
			continue
		}
		if out == nil {
			out = make(map[string]any, len(data))
			for existingKey, existingValue := range data {
				out[existingKey] = existingValue
			}
		}
		out[key] = cleaned
	}
	if out == nil {
		return data
	}
	return out
}

func containsHan(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// userLangLoaderFunc is a function that loads user language from database/cache
// It's set by the model package to avoid circular imports
var userLangLoaderFunc func(userId int) string

// SetUserLangLoader sets the function to load user language (called from model package)
func SetUserLangLoader(loader func(userId int) string) {
	userLangLoaderFunc = loader
}

// PinRequestLanguage forces this request to use a recognized interface language.
// Unknown values are ignored so Accept-Language and user settings still apply.
func PinRequestLanguage(c *gin.Context, lang string) {
	if c == nil {
		return
	}
	canonical, ok := RecognizedLang(lang)
	if !ok {
		return
	}
	c.Set(string(constant.ContextKeyPinnedLanguage), canonical)
}

// GetLangFromContext extracts the language setting from gin context
// It checks multiple sources in priority order:
// 1. Language pinned for this request (OAuth callback flow language)
// 2. User settings (ContextKeyUserSetting) - if already loaded (e.g., by TokenAuth)
// 3. Lazy load user language from cache/DB using user ID
// 4. Language set by middleware (ContextKeyLanguage) - from Accept-Language header
// 5. Default language (simplified Chinese)
func GetLangFromContext(c *gin.Context) string {
	if c == nil {
		return DefaultLang
	}

	if lang := c.GetString(string(constant.ContextKeyPinnedLanguage)); lang != "" {
		if canonical, ok := RecognizedLang(lang); ok {
			return canonical
		}
	}

	// 1. Try to get language from user settings (if already loaded by TokenAuth or other middleware)
	if userSetting, ok := common.GetContextKeyType[dto.UserSetting](c, constant.ContextKeyUserSetting); ok {
		if userSetting.Language != "" {
			normalized := normalizeLang(userSetting.Language)
			if IsSupported(normalized) {
				return normalized
			}
		}
	}

	// 2. Lazy load user language using user ID (for session-based auth where full settings aren't loaded)
	if userLangLoaderFunc != nil {
		if userId, exists := c.Get("id"); exists {
			if uid, ok := userId.(int); ok && uid > 0 {
				lang := userLangLoaderFunc(uid)
				if lang != "" {
					normalized := normalizeLang(lang)
					if IsSupported(normalized) {
						return normalized
					}
				}
			}
		}
	}

	// 3. Try to get language from context (set by I18n middleware from Accept-Language)
	if lang := c.GetString(string(constant.ContextKeyLanguage)); lang != "" {
		normalized := normalizeLang(lang)
		if IsSupported(normalized) {
			return normalized
		}
	}

	// 4. Try Accept-Language header directly (fallback if middleware didn't run)
	if acceptLang := c.GetHeader("Accept-Language"); acceptLang != "" {
		lang := ParseAcceptLanguage(acceptLang)
		if IsSupported(lang) {
			return lang
		}
	}

	return DefaultLang
}

// ParseAcceptLanguage parses the Accept-Language header and returns the preferred language
func ParseAcceptLanguage(header string) string {
	if header == "" {
		return DefaultLang
	}

	// Simple parsing: take the first language tag
	parts := strings.Split(header, ",")
	if len(parts) == 0 {
		return DefaultLang
	}

	// Get the first language and remove quality value
	firstLang := strings.TrimSpace(parts[0])
	if idx := strings.Index(firstLang, ";"); idx > 0 {
		firstLang = firstLang[:idx]
	}

	return normalizeLang(firstLang)
}

// CanonicalLang normalizes frontend and BCP-47 language tags onto supported codes.
func CanonicalLang(lang string) string {
	return normalizeLang(lang)
}

// RecognizedLang maps a frontend or BCP-47 tag onto a supported code.
// Unknown or empty values return false instead of silently falling back.
func RecognizedLang(lang string) (string, bool) {
	mapped := mapSupportedLang(lang)
	if mapped == "" {
		return "", false
	}
	return mapped, true
}

func mapSupportedLang(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang == "" {
		return ""
	}
	lang = strings.ReplaceAll(lang, "_", "-")
	compact := strings.ReplaceAll(lang, "-", "")

	switch {
	case compact == "zhtw" ||
		strings.HasPrefix(lang, "zh-tw") ||
		strings.HasPrefix(lang, "zh-hk") ||
		strings.HasPrefix(lang, "zh-mo") ||
		strings.HasPrefix(lang, "zh-hant"):
		return LangZhTW
	case compact == "zhcn" || strings.HasPrefix(lang, "zh"):
		return LangZhCN
	case strings.HasPrefix(lang, "en"):
		return LangEn
	case strings.HasPrefix(lang, "fr"):
		return LangFr
	case strings.HasPrefix(lang, "ja"):
		return LangJa
	case strings.HasPrefix(lang, "ru"):
		return LangRu
	case strings.HasPrefix(lang, "vi"):
		return LangVi
	default:
		return ""
	}
}

// normalizeLang normalizes language code to supported format
func normalizeLang(lang string) string {
	if mapped := mapSupportedLang(lang); mapped != "" {
		return mapped
	}
	return DefaultLang
}

// SupportedLanguages returns a list of supported language codes
func SupportedLanguages() []string {
	return []string{LangZhCN, LangZhTW, LangEn, LangFr, LangJa, LangRu, LangVi}
}

// IsSupported reports whether lang maps onto a catalog after normalizeLang.
// Unknown tags therefore return true because they fall back to DefaultLang.
// Write paths that must reject unknown tags should call RecognizedLang.
func IsSupported(lang string) bool {
	lang = normalizeLang(lang)
	for _, supported := range SupportedLanguages() {
		if lang == supported {
			return true
		}
	}
	return false
}
