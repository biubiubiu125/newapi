package controller

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/i18n"
)

func TestTaskPluginConflictMessagesKeepNamesInChinese(t *testing.T) {
	if err := i18n.Init(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		err  error
		want string
	}{
		{
			err:  fmt.Errorf("plugin alpha model %q conflicts with plugin beta model %q", "model-a", "model-b"),
			want: `插件 alpha 的模型 "model-a" 与插件 beta 的模型 "model-b" 冲突`,
		},
		{
			err:  fmt.Errorf("plugin alpha channelType %d conflicts with plugin beta", 80),
			want: "插件 alpha 的渠道类型 80 与插件 beta 冲突",
		},
		{
			err:  fmt.Errorf("plugin alpha route %s %s conflicts with plugin beta route %s", "POST", "/v1/tasks", "/v1/jobs"),
			want: "插件 alpha 的路由 POST /v1/tasks 与插件 beta 的路由 /v1/jobs 冲突",
		},
		{
			err:  fmt.Errorf("plugin alpha protocol %s %s model %q conflicts with plugin beta", "POST", "/v1/videos", "vid"),
			want: `插件 alpha 的协议 POST /v1/videos 模型 "vid" 与插件 beta 冲突`,
		},
	}
	for _, tc := range cases {
		key, data, ok := taskPluginConflictMessage(tc.err)
		if !ok {
			t.Fatalf("unclassified %q", tc.err)
		}
		got := i18n.Translate(i18n.LangZhCN, key, data)
		if got != tc.want || strings.Contains(got, "conflicts with") {
			t.Fatalf("Translate=%q want %q", got, tc.want)
		}
	}

	key, _, ok := taskPluginConflictMessage(fmt.Errorf("syntax error near token"))
	if ok || key != "" {
		t.Fatalf("unknown compile error was classified as %q", key)
	}
}
