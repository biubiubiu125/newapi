package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupNoticeTest(t *testing.T, legacyNotice string, announcements string, enabled bool) {
	t.Helper()

	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	oldNotice := common.OptionMap["Notice"]
	common.OptionMap["Notice"] = legacyNotice
	common.OptionMapRWMutex.Unlock()

	cs := console_setting.GetConsoleSetting()
	oldAnnouncements := cs.Announcements
	oldAnnouncementsEnabled := cs.AnnouncementsEnabled
	cs.Announcements = announcements
	cs.AnnouncementsEnabled = enabled

	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap["Notice"] = oldNotice
		common.OptionMapRWMutex.Unlock()
		cs.Announcements = oldAnnouncements
		cs.AnnouncementsEnabled = oldAnnouncementsEnabled
	})
}

func performGetNotice() map[string]interface{} {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/notice", nil)

	GetNotice(ctx)

	var payload map[string]interface{}
	_ = common.Unmarshal(recorder.Body.Bytes(), &payload)
	return payload
}

func TestGetNoticePrefersConsoleAnnouncementsOverSystemNotice(t *testing.T) {
	setupNoticeTest(t, "system notice", `[{
		"id":1,
		"content":"console announcement",
		"publishDate":"2026-05-29T01:00:00Z",
		"type":"default",
		"extra":""
	}]`, true)

	payload := performGetNotice()

	require.Equal(t, true, payload["success"])
	require.Contains(t, payload["data"], "console announcement")
	require.Contains(t, payload["notice"], "console announcement")
	require.NotContains(t, payload["data"], "system notice")
	require.Len(t, payload["announcements"], 1)
}

func TestGetNoticeFallsBackToAnnouncementTextWhenLegacyNoticeIsEmpty(t *testing.T) {
	setupNoticeTest(t, "", `[{
		"id":2,
		"content":"structured announcement",
		"publishDate":"2026-05-29T02:00:00Z",
		"type":"success",
		"extra":"extra note"
	}]`, true)

	payload := performGetNotice()

	require.Equal(t, true, payload["success"])
	require.Contains(t, payload["data"], "structured announcement")
	require.Contains(t, payload["notice"], "extra note")
	require.Len(t, payload["announcements"], 1)
}

func TestGetNoticeFallsBackToSystemNoticeWhenAnnouncementsDisabled(t *testing.T) {
	setupNoticeTest(t, "1", `[{
		"id":3,
		"content":"real announcement",
		"publishDate":"2026-05-29T03:00:00Z",
		"type":"default",
		"extra":""
	}]`, false)

	payload := performGetNotice()

	require.Equal(t, true, payload["success"])
	require.Equal(t, "1", payload["data"])
	require.Equal(t, "1", payload["notice"])
	require.Len(t, payload["announcements"], 0)
}
