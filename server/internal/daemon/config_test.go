package daemon

import (
	"reflect"
	"testing"
)

func TestPatternsFromEnv_DefaultsWhenUnset(t *testing.T) {
	t.Setenv("MULTICA_GC_ARTIFACT_PATTERNS", "")
	defaults := []string{"node_modules", ".next", ".turbo"}
	got := patternsFromEnv("MULTICA_GC_ARTIFACT_PATTERNS", defaults)
	if !reflect.DeepEqual(got, defaults) {
		t.Fatalf("expected defaults %v, got %v", defaults, got)
	}
	// Ensure callers get a copy, not a shared backing array.
	got[0] = "mutated"
	if defaults[0] == "mutated" {
		t.Fatal("patternsFromEnv must not return a slice aliased with defaults")
	}
}

func TestPatternsFromEnv_DropsSeparatorBearingEntries(t *testing.T) {
	t.Setenv("MULTICA_GC_ARTIFACT_PATTERNS", "node_modules, .next ,foo/bar, ../etc, ,target")
	got := patternsFromEnv("MULTICA_GC_ARTIFACT_PATTERNS", nil)
	want := []string{"node_modules", ".next", "target"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestFirtalGatewayAgentEntryFromURLAndKey(t *testing.T) {
	// CEREBRO-PATCH(daemon-config-test-firtal-gateway): validate managed gateway registration env handling.
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL", "https://registry.example.com")
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY", "rk_test")
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_MODEL", "claude-sonnet-4-6")

	entry, ok, err := firtalGatewayAgentEntry()
	if err != nil {
		t.Fatalf("firtalGatewayAgentEntry error: %v", err)
	}
	if !ok {
		t.Fatal("expected firtal gateway to be enabled")
	}
	if entry.Path != "" {
		t.Fatalf("Path = %q, want empty managed HTTP path", entry.Path)
	}
	if entry.Model != "claude-sonnet-4-6" {
		t.Fatalf("Model = %q", entry.Model)
	}
}

func TestFirtalGatewayAgentEntryFailsFastWhenExplicitlyEnabledWithoutKey(t *testing.T) {
	t.Setenv("MULTICA_FIRTAL_GATEWAY_ENABLED", "true")
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL", "https://registry.example.com")

	_, _, err := firtalGatewayAgentEntry()
	if err == nil {
		t.Fatal("expected missing key error")
	}
}
