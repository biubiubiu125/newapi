package service

import (
	"html"
	"strings"
	"testing"
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
