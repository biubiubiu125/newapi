package common

import "testing"

func TestDefaultSystemNameIsRKAPI(t *testing.T) {
	if SystemName != "RK API" {
		t.Fatalf("default SystemName = %q, want %q", SystemName, "RK API")
	}
}

func TestNormalizeSystemNameRewritesLegacyBranding(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{input: "", want: "RK API"},
		{input: "  ", want: "RK API"},
		{input: "New API", want: "RK API"},
		{input: "NEW API", want: "RK API"},
		{input: " new api ", want: "RK API"},
		{input: "NewAPI", want: "RK API"},
		{input: "RKAPI", want: "RK API"},
		{input: "rkapi", want: "RK API"},
		{input: "RK API", want: "RK API"},
		{input: "My Site", want: "My Site"},
	}
	for _, tc := range cases {
		if got := NormalizeSystemName(tc.input); got != tc.want {
			t.Errorf("NormalizeSystemName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
