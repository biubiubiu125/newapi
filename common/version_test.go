package common

import "testing"

func TestBakedVersionIgnoresEnvironmentOverride(t *testing.T) {
	if got := applyVersionOverride("main-8910ca098", "v9.9.9"); got != "main-8910ca098" {
		t.Fatalf("baked commit version = %q", got)
	}
	if got := applyVersionOverride("v1.2.3", "main-aaaaaaaaa"); got != "v1.2.3" {
		t.Fatalf("baked release version = %q", got)
	}
	if got := applyVersionOverride("main-8910ca098", ""); got != "main-8910ca098" {
		t.Fatalf("empty override = %q", got)
	}
	if got := applyVersionOverride("v0.0.0", "main-aaaaaaaaa"); got != "main-aaaaaaaaa" {
		t.Fatalf("placeholder override = %q", got)
	}
	if got := applyVersionOverride("0.0.0", "main-aaaaaaaaa"); got != "main-aaaaaaaaa" {
		t.Fatalf("numeric placeholder override = %q", got)
	}
	if got := applyVersionOverride("", "main-aaaaaaaaa"); got != "main-aaaaaaaaa" {
		t.Fatalf("empty baked version = %q", got)
	}
	if got := applyVersionOverride("v0.0.0", "  "); got != "v0.0.0" {
		t.Fatalf("blank override = %q", got)
	}
}
