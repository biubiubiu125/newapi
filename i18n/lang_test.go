package i18n

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

func TestNormalizeLangFrontendAndRegionalCodes(t *testing.T) {
	cases := map[string]string{
		"zhTW":    LangZhTW,
		"zhtw":    LangZhTW,
		"zh-TW":   LangZhTW,
		"zh-tw":   LangZhTW,
		"zh_TW":   LangZhTW,
		"zh-HK":   LangZhTW,
		"zh-Hant": LangZhTW,
		"zhCN":    LangZhCN,
		"zhcn":    LangZhCN,
		"zh-CN":   LangZhCN,
		"zh-SG":   LangZhCN,
		"zh_SG":   LangZhCN,
		"zh":      LangZhCN,
		"en":      LangEn,
		"en-US":   LangEn,
		"fr":      LangFr,
		"fr-FR":   LangFr,
		"ja":      LangJa,
		"ja-JP":   LangJa,
		"ru":      LangRu,
		"ru-RU":   LangRu,
		"vi":      LangVi,
		"vi-VN":   LangVi,
	}
	for in, want := range cases {
		if got := normalizeLang(in); got != want {
			t.Fatalf("normalizeLang(%q)=%q want %q", in, got, want)
		}
	}
}

func TestTranslateZhTWFrontendCodeUsesTraditional(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	got := Translate("zhTW", "common.invalid_params")
	tw := Translate("zh-TW", "common.invalid_params")
	cn := Translate("zh-CN", "common.invalid_params")
	if got != tw {
		t.Fatalf("zhTW translated %q, want traditional %q", got, tw)
	}
	if got == cn {
		t.Fatalf("zhTW should not use simplified %q", cn)
	}
}

func TestDefaultLangIsSimplifiedChinese(t *testing.T) {
	if DefaultLang != LangZhCN {
		t.Fatalf("DefaultLang=%q want %q", DefaultLang, LangZhCN)
	}
}

func TestNormalizeLangUnknownFallsBackToSimplifiedChinese(t *testing.T) {
	cases := []string{"", "pt-BR", "de", "ko"}
	for _, in := range cases {
		if got := normalizeLang(in); got != LangZhCN {
			t.Fatalf("normalizeLang(%q)=%q want %q", in, got, LangZhCN)
		}
	}
}

func TestIsSupportedUnknownFallsBackThroughNormalize(t *testing.T) {
	// Write paths must use RecognizedLang. IsSupported normalizes first, so
	// unknown tags fall back to DefaultLang and therefore return true.
	if !IsSupported("de") {
		t.Fatal(`IsSupported("de") should stay true via DefaultLang fallback`)
	}
	if !IsSupported("") {
		t.Fatal(`IsSupported("") should stay true via DefaultLang fallback`)
	}
}

func TestRecognizedLangRejectsUnknownAndCanonicalizesSupported(t *testing.T) {
	cases := map[string]string{
		"zhCN":  LangZhCN,
		"zh-TW": LangZhTW,
		"en-US": LangEn,
		"fr":    LangFr,
		"ja-JP": LangJa,
		"ru-RU": LangRu,
		"vi":    LangVi,
	}
	for in, want := range cases {
		got, ok := RecognizedLang(in)
		if !ok || got != want {
			t.Fatalf("RecognizedLang(%q)=(%q,%v) want (%q,true)", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "de", "pt-BR", "ko"} {
		if got, ok := RecognizedLang(in); ok {
			t.Fatalf("RecognizedLang(%q)=(%q,true) want unrecognized", in, got)
		}
	}
}

func TestProtocolMessageAlwaysEnglish(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	zh := Translate(LangZhCN, MsgTokenInvalid)
	en := ProtocolMessage(MsgTokenInvalid)
	if zh == en {
		t.Fatalf("Chinese token error should differ from protocol English, both %q", en)
	}
	if en != "Invalid token" {
		t.Fatalf("ProtocolMessage=%q want %q", en, "Invalid token")
	}
	for _, r := range en {
		if unicode.In(r, unicode.Han) {
			t.Fatalf("protocol English contains Chinese: %q", en)
		}
	}
}

func TestProtocolQuotaAndPriceMessagesStayEnglish(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		key  string
		want string
		args map[string]any
	}{
		{MsgProtocolInsufficientUserQuota, "Insufficient user quota, remaining: $1", map[string]any{"Quota": "$1"}},
		{MsgProtocolInsufficientSubscriptionQuota, "Insufficient subscription quota or subscription is not configured", nil},
		{MsgProtocolModelPriceNotConfigured, "Model gpt-x has not been priced by the administrator yet. Please contact the site administrator to enable this model.", map[string]any{"Model": "gpt-x"}},
		{MsgProtocolFileTooLarge, "file demo.png exceeds the size limit of 10 MB", map[string]any{"Name": "demo.png", "MaxMB": 10}},
		{MsgProtocolPreConsumeFailed, "Failed to pre-consume quota, remaining: $1, required: $2", map[string]any{"Quota": "$1", "Required": "$2"}},
		{MsgQuotaNegative, "Quota cannot be negative!", nil},
	}
	for _, tc := range cases {
		got := ProtocolMessage(tc.key, tc.args)
		if got != tc.want {
			t.Fatalf("ProtocolMessage(%s)=%q want %q", tc.key, got, tc.want)
		}
		for _, r := range got {
			if unicode.In(r, unicode.Han) {
				t.Fatalf("ProtocolMessage(%s) contains Chinese: %q", tc.key, got)
			}
		}
	}
}

func TestTranslateFrontendConsoleLanguages(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		LangFr: "Nom d'utilisateur ou mot de passe incorrect, ou l'utilisateur a été banni",
		LangJa: "ユーザー名またはパスワードが間違っているか、ユーザーは停止されています",
		LangRu: "Неверное имя пользователя или пароль, или пользователь заблокирован",
		LangVi: "Tên người dùng hoặc mật khẩu không đúng, hoặc người dùng đã bị cấm",
	}
	zh := Translate(LangZhCN, MsgUserUsernameOrPasswordError)
	for lang, want := range cases {
		got := Translate(lang, MsgUserUsernameOrPasswordError)
		if got == zh {
			t.Fatalf("Translate(%q) fell back to simplified Chinese %q", lang, got)
		}
		if got != want {
			t.Fatalf("Translate(%q)=%q want %q", lang, got, want)
		}
	}
}

func TestNewConsoleI18nKeysAreTranslated(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		key  string
		lang string
		want string
	}{
		{MsgOAuthAccessDenied, LangZhCN, "访问被拒绝：当前账号不满足该登录方式的访问要求。"},
		{MsgOAuthAccessDenied, LangEn, "Access denied: your account does not meet this provider's access requirements."},
		{MsgOAuthAuthorizationCancelled, LangZhCN, "授权已取消，或该登录方式拒绝了访问。"},
		{MsgOAuthAuthorizationCancelled, LangEn, "Authorization was cancelled or access was denied."},
		{MsgOAuthAuthorizationFailed, LangZhCN, "授权失败，请重试。"},
		{MsgOAuthAuthorizationFailed, LangEn, "Authorization failed. Please try again."},
		{MsgTelegramPushContentEmpty, LangZhCN, "推送内容不能为空"},
		{MsgTelegramPushSendFailed, LangZhCN, "Telegram 推送失败，请检查 Bot Token 和 Chat ID。"},
		{MsgTelegramPushSendFailed, LangEn, "Telegram push failed. Please check the bot token and chat ID."},
		{MsgPluginCompileFailed, LangZhCN, "插件无法加载"},
		{MsgPluginCompileFailed, LangEn, "Failed to load plugin: boom"},
		{MsgPluginRuntimeFailed, LangZhCN, "插件执行失败"},
		{MsgPaymentBepusdtRejected, LangZhCN, "BEpusdt 网关拒绝订单"},
		{MsgPaymentBepusdtRejected, LangEn, "BEpusdt rejected the order: boom"},
	}
	for _, tc := range cases {
		got := Translate(tc.lang, tc.key, map[string]any{"Error": "boom"})
		if got != tc.want {
			t.Fatalf("Translate(%q,%q)=%q want %q", tc.lang, tc.key, got, tc.want)
		}
	}
}

func TestBackendLocalesHaveTheSameKeys(t *testing.T) {
	files := []string{
		"locales/en.yaml",
		"locales/zh-CN.yaml",
		"locales/zh-TW.yaml",
		"locales/fr.yaml",
		"locales/ja.yaml",
		"locales/ru.yaml",
		"locales/vi.yaml",
	}
	keysByFile := make([]map[string]struct{}, 0, len(files))
	for _, file := range files {
		body, err := os.ReadFile(filepath.Join("i18n", file))
		if err != nil {
			body, err = os.ReadFile(file)
		}
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		keys := map[string]struct{}{}
		for _, line := range strings.Split(string(body), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, _, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			keys[strings.TrimSpace(key)] = struct{}{}
		}
		if len(keys) == 0 {
			t.Fatalf("%s has no keys", file)
		}
		keysByFile = append(keysByFile, keys)
	}
	base := keysByFile[0]
	for i, keys := range keysByFile[1:] {
		file := files[i+1]
		for key := range base {
			if _, ok := keys[key]; !ok {
				t.Errorf("%s missing key %s", file, key)
			}
		}
		for key := range keys {
			if _, ok := base[key]; !ok {
				t.Errorf("%s extra key %s", file, key)
			}
		}
	}
}

func TestParseAcceptLanguageEmptyUsesSimplifiedChinese(t *testing.T) {
	if got := ParseAcceptLanguage(""); got != LangZhCN {
		t.Fatalf("ParseAcceptLanguage empty=%q want %q", got, LangZhCN)
	}
	if got := ParseAcceptLanguage("en-US,en;q=0.9"); got != LangEn {
		t.Fatalf("ParseAcceptLanguage en-US=%q want %q", got, LangEn)
	}
}
