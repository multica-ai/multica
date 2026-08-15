package agent

import (
	"log/slog"
	"strings"
	"testing"
)

// TestSupportedTypesLockstepWithNew guards the iron-rule whitelist: every type
// in SupportedTypes must be constructable by New, and New must reject anything
// not in SupportedTypes. SupportedTypes is the readable factory/migration
// whitelist; public profile creation uses the narrower
// CreatableRuntimeProfileTypes. If a backend is added to New, it must be added
// here too — and to the migration CHECK.
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

	// A type outside the whitelist must be rejected by both.
	const bogus = "definitely-not-a-real-backend"
	if IsSupportedType(bogus) {
		t.Errorf("IsSupportedType(%q) = true, want false", bogus)
	}
	if _, err := New(bogus, cfg); err == nil {
		t.Errorf("New(%q) succeeded, want error for an unsupported type", bogus)
	} else if !strings.Contains(err.Error(), "prime") || !strings.Contains(err.Error(), "claude") {
		t.Errorf("New(%q) error does not include the supported provider list: %v", bogus, err)
	}
}

// TestSupportedTypesMatchesMigrationWhitelist pins the exact set so a drift
// from the runtime_profile.protocol_family CHECK fails loudly.
func TestSupportedTypesMatchesMigrationWhitelist(t *testing.T) {
	want := map[string]bool{
		"claude": true, "codebuddy": true, "codex": true, "copilot": true,
		"opencode": true, "deveco": true, "openclaw": true, "hermes": true,
		"pi": true, "cursor": true, "kimi": true, "reasonix": true, "dsh": true, "kiro": true, "antigravity": true,
		"qoder": true, "qoderclicn": true, "traecli": true, "grok": true, "qwen": true, "qwenpaw": true, "prime": true,
	}
	if len(SupportedTypes) != len(want) {
		t.Fatalf("SupportedTypes has %d entries, migration whitelist has %d; keep them in lockstep", len(SupportedTypes), len(want))
	}
	for _, typ := range SupportedTypes {
		if !want[typ] {
			t.Errorf("SupportedTypes contains %q which is not in the latest protocol_family CHECK", typ)
		}
	}
}

func TestPrimeIsReadableButNotCreatableAsRuntimeProfile(t *testing.T) {
	if !IsSupportedType("prime") {
		t.Fatal("Prime must remain readable for migrated runtime_profile rows")
	}
	if IsCreatableRuntimeProfileType("prime") {
		t.Fatal("admission-disabled Prime must not be publicly creatable")
	}
	wantCreatable := map[string]bool{}
	for _, typ := range SupportedTypes {
		if typ != "prime" {
			wantCreatable[typ] = true
		}
	}
	if len(CreatableRuntimeProfileTypes) != len(wantCreatable) {
		t.Fatalf("creatable profile families=%d, want every supported family except Prime (%d)", len(CreatableRuntimeProfileTypes), len(wantCreatable))
	}
	for _, typ := range CreatableRuntimeProfileTypes {
		if !IsSupportedType(typ) {
			t.Fatalf("creatable runtime profile family %q is not readable/supported", typ)
		}
		if !wantCreatable[typ] {
			t.Fatalf("unexpected creatable runtime profile family %q", typ)
		}
	}
}
