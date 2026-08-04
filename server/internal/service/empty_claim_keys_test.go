package service

import (
	"strings"
	"testing"
)

// hashTag extracts the Redis Cluster hash tag from a key: the substring
// between the first '{' and the first '}' that follows it, when non-empty.
// Mirrors the algorithm Redis uses to pick the slot-determining part of a key.
func hashTag(key string) string {
	open := strings.IndexByte(key, '{')
	if open < 0 {
		return ""
	}
	rest := key[open+1:]
	closeIdx := strings.IndexByte(rest, '}')
	if closeIdx <= 0 {
		return ""
	}
	return rest[:closeIdx]
}

// TestEmptyClaimKeysColocate asserts that a runtime's empty-verdict key and its
// version key carry the same {runtimeID} hash tag. IsEmpty reads both in one
// MGET, which returns CROSSSLOT on Redis Cluster if the keys span slots; the
// shared tag keeps that MGET valid.
func TestEmptyClaimKeysColocate(t *testing.T) {
	const runtimeID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	verdict := emptyClaimKey(runtimeID)
	version := emptyClaimVersion(runtimeID)

	vt := hashTag(verdict)
	if vt != runtimeID {
		t.Fatalf("verdict key %q hash tag = %q, want %q", verdict, vt, runtimeID)
	}
	if pt := hashTag(version); pt != vt {
		t.Fatalf("hash tag mismatch: verdict %q (%q) vs version %q (%q)", verdict, vt, version, pt)
	}
}
