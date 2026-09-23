package service

import (
	"html"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
)

func TestBuildTelegramPushTextKeepsShortMessagesUnchanged(t *testing.T) {
	got := BuildTelegramPushText("RKAPI", "标题", "内容")
	want := "<b>" + html.EscapeString("[RKAPI]标题") + "</b>\n" + html.EscapeString("内容")
	if got != want {
		t.Fatalf("short telegram text = %q, want %q", got, want)
	}
}

func TestBuildTelegramPushTextFitsTelegramLimitForEscapedContent(t *testing.T) {
	content := strings.Repeat("<", 2000)
	got := BuildTelegramPushText("RKAPI", "公告", content)
	if got == "" {
		t.Fatal("expected truncated telegram text, got empty")
	}
	if telegramUTF16Len(got) > 4096 {
		t.Fatalf("telegram text length = %d, want <= 4096", telegramUTF16Len(got))
	}
	if !strings.HasPrefix(got, "<b>") || !strings.Contains(got, "</b>") {
		t.Fatalf("truncated telegram text lost HTML header: %q", got)
	}
	if strings.Count(got, "<b>") != 1 || strings.Count(got, "</b>") != 1 {
		t.Fatalf("truncated telegram text has broken bold tags: %q", got)
	}
}

func TestNormalizeTelegramPushDisplayNameStaysRKAPI(t *testing.T) {
	if got := NormalizeTelegramPushDisplayName(""); got != "RKAPI" {
		t.Fatalf("empty display name = %q, want RKAPI", got)
	}
	if got := NormalizeTelegramPushDisplayName("RKAPI"); got != "RKAPI" {
		t.Fatalf("display name = %q, want RKAPI", got)
	}
}

func TestSendTelegramPushHidesUpstreamEnglish(t *testing.T) {
	err := SendTelegramPush("", "", "RKAPI", "hello", "")
	loc, ok := common.AsLocalizedError(err)
	if !ok {
		t.Fatalf("empty config error=%v, want LocalizedError", err)
	}
	if loc.Key != i18n.MsgTelegramPushInvalidConfig {
		t.Fatalf("empty config key=%q want %q", loc.Key, i18n.MsgTelegramPushInvalidConfig)
	}

	err = SendTelegramPush("token", "chat", "RKAPI", "", "")
	loc, ok = common.AsLocalizedError(err)
	if !ok {
		t.Fatalf("empty content error=%v, want LocalizedError", err)
	}
	if loc.Key != i18n.MsgTelegramPushContentEmpty {
		t.Fatalf("empty content key=%q want %q", loc.Key, i18n.MsgTelegramPushContentEmpty)
	}
}
