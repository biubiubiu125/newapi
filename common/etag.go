package common

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
)

type digestAnchor struct{}

func modulePath() string {
	return reflect.TypeOf(digestAnchor{}).PkgPath()
}

var digestSeed = func() (s [sha256.Size]byte) {
	return sha256.Sum256([]byte(modulePath()))
}()

// ETagFor returns a weak ETag derived from a namespace and semantic content.
// Hashing the semantic value rather than a package-specific JSON byte stream
// keeps validators stable when equivalent payloads use different encoders.
func ETagFor(namespace, content string) string {
	buf := make([]byte, 0, sha256.Size+1+len(namespace)+1+len(content))
	buf = append(buf, digestSeed[:]...)
	buf = append(buf, 0)
	buf = append(buf, namespace...)
	buf = append(buf, 0)
	buf = append(buf, content...)
	digest := sha256.Sum256(buf)
	return `W/"` + hex.EncodeToString(digest[:]) + `"`
}

// ETagMatches reports whether an If-None-Match header matches etag under weak
// comparison (RFC 9110 section 13.1.2). The W/ prefix is ignored on both
// sides, and * matches everything.
func ETagMatches(ifNoneMatch, etag string) bool {
	ifNoneMatch = strings.TrimSpace(ifNoneMatch)
	if ifNoneMatch == "" {
		return false
	}
	if ifNoneMatch == "*" {
		return true
	}
	etag = strings.TrimPrefix(etag, "W/")
	for _, candidate := range strings.Split(ifNoneMatch, ",") {
		if strings.TrimPrefix(strings.TrimSpace(candidate), "W/") == etag {
			return true
		}
	}
	return false
}
