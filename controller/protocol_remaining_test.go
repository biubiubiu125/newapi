package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
)

func TestPlaygroundAccessTokenStaysEnglish(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.I18n())
	engine.Use(func(c *gin.Context) {
		c.Set("use_access_token", true)
		c.Next()
	})
	engine.POST("/pg/chat/completions", Playground)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/pg/chat/completions", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", "zh-CN")
	engine.ServeHTTP(rec, req)

	msg := openaiProtocolErrorMessage(t, rec)
	require.Contains(t, msg, "Access token is not supported")
	assertNoHan(t, msg)
}

func TestGetChannelRetryMissingStaysEnglish(t *testing.T) {
	_ = setupModelListControllerTestDB(t)
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	c.Request.Header.Set("Accept-Language", "zh-CN")
	middleware.I18n()(c)

	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-4",
		ChannelMeta:     &relaycommon.ChannelMeta{},
	}
	channel, apiErr := getChannel(c, info, &service.RetryParam{
		Ctx:        c,
		TokenGroup: "missing-group",
		ModelName:  "gpt-4",
	})
	require.Nil(t, channel)
	require.NotNil(t, apiErr)
	msg := apiErr.ToOpenAIError().Message
	require.Contains(t, msg, "retry")
	require.Contains(t, strings.ToLower(msg), "no available channel")
	assertNoHan(t, msg)
}

func TestRespondTaskErrorTooManyRequestsStaysEnglish(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{}`))
	c.Request.Header.Set("Accept-Language", "zh-CN")
	middleware.I18n()(c)

	respondTaskError(c, &dto.TaskError{
		StatusCode: http.StatusTooManyRequests,
		Code:       "upstream_overloaded",
		Message:    "whatever",
	})
	require.Equal(t, http.StatusTooManyRequests, rec.Code)

	var body dto.TaskError
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())
	require.Contains(t, body.Message, "saturated")
	assertNoHan(t, body.Message)
}

func openaiProtocolErrorMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())
	return body.Error.Message
}

func assertNoHan(t *testing.T, msg string) {
	t.Helper()
	for _, r := range msg {
		if unicode.In(r, unicode.Han) {
			t.Fatalf("protocol message contains Chinese: %q", msg)
		}
	}
}

func TestSanitizeMidjourneyFailReasonsStaysEnglish(t *testing.T) {
	items := []*model.Midjourney{
		{FailReason: "上游任务ID为空"},
		{FailReason: "获取渠道信息失败，请联系管理员，渠道ID：7"},
		{FailReason: "上游任务超时（超过1小时）"},
	}
	sanitizeMidjourneyFailReasons(items)
	require.Equal(t, "upstream task id is empty", items[0].FailReason)
	require.Equal(t, "failed to get channel information, please contact the administrator, channel id: 7", items[1].FailReason)
	require.Equal(t, "upstream task timed out (over 1 hour)", items[2].FailReason)
	for _, item := range items {
		assertNoHan(t, item.FailReason)
	}
}
