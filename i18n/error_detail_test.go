package i18n

import (
	"strings"
	"testing"
)

func TestChineseTemplatesDropUntranslatedErrorDetail(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	english := "dial tcp 127.0.0.1:11434: connect: connection refused"
	cases := []struct {
		lang string
		key  string
		data map[string]any
		want string
	}{
		{LangZhCN, "channel.get_models_failed", map[string]any{"Error": english}, "获取模型列表失败"},
		{LangZhTW, "channel.get_models_failed", map[string]any{"Error": english}, "取得模型列表失敗"},
		{LangZhCN, "payment.bepusdt_rejected", map[string]any{"Error": english}, "BEpusdt 网关拒绝订单"},
		{LangZhCN, "console.api_info_json_invalid", map[string]any{"Error": english}, "API信息格式错误"},
		{LangZhCN, "plugin.invalid_argument", map[string]any{"Index": 2, "Error": english}, "无效参数 2"},
		{LangZhCN, "user.input_invalid", map[string]any{"Error": english}, "输入不合法"},
	}
	for _, tc := range cases {
		got := Translate(tc.lang, tc.key, tc.data)
		if got != tc.want || strings.Contains(got, "dial tcp") || strings.Contains(got, "connection refused") {
			t.Fatalf("Translate(%q,%q)=%q want %q without the English detail", tc.lang, tc.key, got, tc.want)
		}
	}

	kept := Translate(LangZhCN, "payment.bepusdt_rejected", map[string]any{"Error": "金额过小"})
	if kept != "BEpusdt 网关拒绝订单：金额过小" {
		t.Fatalf("Chinese gateway detail was dropped: %q", kept)
	}

	mixed := "连接失败: dial tcp 127.0.0.1:11434: connect: connection refused"
	hid := Translate(LangZhCN, "channel.get_models_failed", map[string]any{"Error": mixed})
	if hid != "获取模型列表失败: 连接失败" || strings.Contains(hid, "dial tcp") || strings.Contains(hid, "connection refused") {
		t.Fatalf("mixed Chinese and English detail leaked: %q", hid)
	}
	timeout := Translate(LangZhCN, "channel.get_models_failed", map[string]any{"Error": "连接失败: timeout"})
	if timeout != "获取模型列表失败: 连接失败" || strings.Contains(timeout, "timeout") {
		t.Fatalf("single English error word leaked: %q", timeout)
	}

	brand := Translate(LangZhCN, "channel.get_models_failed", map[string]any{"Error": "不是合法 JSON"})
	if brand != "获取模型列表失败: 不是合法 JSON" {
		t.Fatalf("Chinese detail with one Latin token was dropped: %q", brand)
	}

	en := Translate(LangEn, "channel.get_models_failed", map[string]any{"Error": english})
	if en != "Failed to get model list: "+english {
		t.Fatalf("English catalog should keep the detail, got %q", en)
	}

	proto := ProtocolMessage("distributor.invalid_request", map[string]any{"Error": "bad json"})
	if proto != "Invalid request: bad json" {
		t.Fatalf("protocol English changed: %q", proto)
	}
}
