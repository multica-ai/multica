package cursorusage

import (
	"strings"
	"testing"
	"time"
)

func TestOpaqueClaimKeyDeterministicAndNonEmpty(t *testing.T) {
	t.Parallel()
	a := OpaqueClaimKey("user_01ABC")
	b := OpaqueClaimKey("user_01ABC")
	if a == "" || a != b {
		t.Fatalf("expected stable digest, got %q / %q", a, b)
	}
	if a == "user_01ABC" || strings.Contains(a, "user_") {
		t.Fatalf("digest must not preserve plaintext: %q", a)
	}
	if OpaqueClaimKey("user_01ABC") == OpaqueClaimKey("user_01DEF") {
		t.Fatal("different inputs must not collide")
	}
}

func TestOccurrenceKeyIsOpaque(t *testing.T) {
	t.Parallel()
	e := UsageEvent{
		Timestamp:       time.UnixMilli(1_700_000_000_000),
		Model:           "composer-1",
		IsHeadless:      true,
		InputTokens:     3,
		OutputTokens:    2,
		ChargedCents:    1.5,
		HasChargedCents: true,
		OccurrenceIndex: 0,
	}
	key := e.OccurrenceKey()
	if key == "" {
		t.Fatal("empty occurrence key")
	}
	if strings.Contains(key, "composer-1") || strings.Contains(key, "|") || strings.Contains(key, "#") {
		t.Fatalf("occurrence key leaked fingerprint fields: %q", key)
	}
	if key != OpaqueClaimKey(e.fingerprint()+"#0") {
		t.Fatalf("occurrence key must hash fingerprint#index, got %q", key)
	}
}

func TestOccurrenceKeyIgnoresUnreliableHeadlessFlag(t *testing.T) {
	t.Parallel()
	e := UsageEvent{
		Timestamp: time.UnixMilli(1_700_000_000_000),
		Model:     "cursor-grok-4.5-high-fast", InputTokens: 3, OutputTokens: 2,
		ChargedCents: 1.5, HasChargedCents: true,
	}
	nonHeadless := e.OccurrenceKey()
	e.IsHeadless = true
	if got := e.OccurrenceKey(); got != nonHeadless {
		t.Fatalf("isHeadless changed occurrence identity: %q != %q", got, nonHeadless)
	}
}
