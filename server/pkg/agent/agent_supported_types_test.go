package agent

import (
	"log/slog"
	"testing"
)

// TestSupportedTypesLockstepWithNew guards the iron-rule whitelist: every type
// in SupportedTypes must be constructable by New. Qoder Cloud is the one
// intentional built-in-only exception because it has no custom CLI protocol.
// This is the single source of truth the custom runtime
// profile protocol_family validation (handler) and the runtime_profile
// protocol_family CHECK (migration 120 plus later tightening migrations) are aligned to. If a custom-CLI backend is
// added to New, it must be added here too — and to the migration CHECK.
func TestSupportedTypesLockstepWithNew(t *testing.T) {
	cfg := Config{Logger: slog.Default()}

	for _, typ := range SupportedTypes {
		if !IsSupportedType(typ) {
			t.Errorf("IsSupportedType(%q) = false, but it is in SupportedTypes", typ)
		}
		if _, err := New(typ, cfg); err != nil {
			t.Errorf("New(%q) returned error for a SupportedTypes entry: %v", typ, err)
		}
	}

	if IsSupportedType("qodercloud") {
		t.Error("qodercloud must remain outside the custom runtime profile whitelist")
	}
	if _, err := New("qodercloud", cfg); err != nil {
		t.Fatalf("New(qodercloud) returned error for the built-in provider: %v", err)
	}

	// An unknown type outside both the custom and built-in-only sets must be
	// rejected by both.
	const bogus = "definitely-not-a-real-backend"
	if IsSupportedType(bogus) {
		t.Errorf("IsSupportedType(%q) = true, want false", bogus)
	}
	if _, err := New(bogus, cfg); err == nil {
		t.Errorf("New(%q) succeeded, want error for an unsupported type", bogus)
	}
}

// TestSupportedTypesMatchesMigrationWhitelist pins the exact set so a drift
// from the runtime_profile.protocol_family CHECK fails loudly.
func TestSupportedTypesMatchesMigrationWhitelist(t *testing.T) {
	want := map[string]bool{
		"claude": true, "codebuddy": true, "codex": true, "copilot": true,
		"opencode": true, "deveco": true, "openclaw": true, "hermes": true,
		"pi": true, "cursor": true, "kimi": true, "reasonix": true, "dsh": true, "kiro": true, "antigravity": true,
		"qoder": true, "qoderclicn": true, "traecli": true, "grok": true, "qwen": true, "qwenpaw": true,
	}
	if len(SupportedTypes) != len(want) {
		t.Fatalf("SupportedTypes has %d entries, migration whitelist has %d; keep them in lockstep", len(SupportedTypes), len(want))
	}
	for _, typ := range SupportedTypes {
		if !want[typ] {
			t.Errorf("SupportedTypes contains %q which is not in the migration 313 protocol_family CHECK", typ)
		}
	}
}
