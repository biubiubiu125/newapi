package i18n

import "testing"

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
