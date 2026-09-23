package console_setting

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
)

func announcementJSON(t *testing.T, content string) string {
	t.Helper()
	payload, err := json.Marshal([]map[string]any{
		{
			"id":          1,
			"content":     content,
			"publishDate": "2026-03-25T00:00:00Z",
			"type":        "default",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func TestValidateAnnouncementsAllows2000Characters(t *testing.T) {
	err := ValidateConsoleSettings(announcementJSON(t, strings.Repeat("a", 2000)), "Announcements")
	if err != nil {
		t.Fatalf("expected 2000-character announcement to pass, got %v", err)
	}
}

func TestValidateAnnouncementsAllowsContentBelowFormer500Limit(t *testing.T) {
	err := ValidateConsoleSettings(announcementJSON(t, strings.Repeat("a", 500)), "Announcements")
	if err != nil {
		t.Fatalf("expected 500-character announcement to pass, got %v", err)
	}
}

func TestValidateAnnouncementsAllowsMarkupWithinLimit(t *testing.T) {
	content := "<p>hello</p>\n**bold** [link](https://example.com) <script>alert(1)</script>"
	err := ValidateConsoleSettings(announcementJSON(t, content), "Announcements")
	if err != nil {
		t.Fatalf("expected markup within 2000 characters to pass, got %v", err)
	}
}

func TestValidateAnnouncementsRejects2001Characters(t *testing.T) {
	err := ValidateConsoleSettings(announcementJSON(t, strings.Repeat("a", 2001)), "Announcements")
	if err == nil {
		t.Fatal("expected 2001-character announcement to fail")
	}
	loc, ok := common.AsLocalizedError(err)
	if !ok || loc.Key != i18n.MsgConsoleAnnouncementContentTooLong {
		t.Fatalf("expected LocalizedError %s, got %v", i18n.MsgConsoleAnnouncementContentTooLong, err)
	}
	if len(loc.Args) == 0 || loc.Args[0]["Max"] != maxAnnouncementContentCharacters {
		t.Fatalf("expected Max=%d, got %#v", maxAnnouncementContentCharacters, loc.Args)
	}
}

func announcementJSONWithTitle(t *testing.T, title string) string {
	t.Helper()
	payload, err := json.Marshal([]map[string]any{
		{
			"id":          1,
			"title":       title,
			"content":     "ok",
			"publishDate": "2026-03-25T00:00:00Z",
			"type":        "default",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func TestValidateAnnouncementsAllows100CharacterTitle(t *testing.T) {
	err := ValidateConsoleSettings(announcementJSONWithTitle(t, strings.Repeat("t", 100)), "Announcements")
	if err != nil {
		t.Fatalf("expected 100-character title to pass, got %v", err)
	}
}

func TestValidateAnnouncementsRejects101CharacterTitle(t *testing.T) {
	err := ValidateConsoleSettings(announcementJSONWithTitle(t, strings.Repeat("t", 101)), "Announcements")
	if err == nil {
		t.Fatal("expected 101-character title to fail")
	}
	loc, ok := common.AsLocalizedError(err)
	if !ok || loc.Key != i18n.MsgConsoleAnnouncementTitleTooLong {
		t.Fatalf("expected LocalizedError %s, got %v", i18n.MsgConsoleAnnouncementTitleTooLong, err)
	}
	if len(loc.Args) == 0 || loc.Args[0]["Max"] != maxAnnouncementTitleCharacters {
		t.Fatalf("expected Max=%d, got %#v", maxAnnouncementTitleCharacters, loc.Args)
	}
}
