package model

import "testing"

func TestNormalizeOptionValueForStorageRewritesLegacySystemName(t *testing.T) {
	got, err := normalizeOptionValueForStorage("SystemName", "New API")
	if err != nil {
		t.Fatalf("normalize SystemName: %v", err)
	}
	if got != "RK API" {
		t.Fatalf("normalized SystemName = %q, want %q", got, "RK API")
	}

	custom, err := normalizeOptionValueForStorage("SystemName", "My Site")
	if err != nil {
		t.Fatalf("normalize custom SystemName: %v", err)
	}
	if custom != "My Site" {
		t.Fatalf("custom SystemName = %q, want %q", custom, "My Site")
	}
}
