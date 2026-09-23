package oauth

import "testing"

func TestRenderAccessDeniedMessageEmptyLeavesDefaultToCaller(t *testing.T) {
	got := renderAccessDeniedMessage("  ", "Acme", "{}", nil)
	if got != "" {
		t.Fatalf("empty template returned %q, want empty so console i18n can fill it", got)
	}
}

func TestRenderAccessDeniedMessageKeepsAdminCopy(t *testing.T) {
	got := renderAccessDeniedMessage("你的账号未加入白名单", "Acme", "{}", nil)
	if got != "你的账号未加入白名单" {
		t.Fatalf("got %q", got)
	}
}
