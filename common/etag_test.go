package common

import "testing"

func TestETagMatchesUsesWeakComparison(t *testing.T) {
	etag := ETagFor("public-content:v1", "notice")

	if !ETagMatches(etag, etag) {
		t.Fatalf("expected weak validator %q to match %q", etag, etag)
	}
	if !ETagMatches(`"other", `+etag, etag) {
		t.Fatalf("expected a matching validator in a list to match")
	}
	if !ETagMatches("*", etag) {
		t.Fatalf("expected wildcard validator to match")
	}
	if ETagMatches(`"other"`, etag) {
		t.Fatalf("did not expect unrelated validator to match")
	}
}

func TestETagForIsStableForSemanticContent(t *testing.T) {
	first := ETagFor("public-content:v1", "same content")
	second := ETagFor("public-content:v1", "same content")
	if first != second {
		t.Fatalf("expected stable ETag, got %q and %q", first, second)
	}
	if first == ETagFor("public-content:v1", "changed content") {
		t.Fatalf("expected changed content to produce a different ETag")
	}
}
