package daemon

import (
	"os"
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

func TestLoadConfigAoneRuntimeOverridesLocalAgents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MULTICA_AONE_RUNTIME_URL", "http://127.0.0.1:3211")
	t.Setenv("MULTICA_AONE_RUNTIME_TOKEN", "secret")
	t.Setenv("MULTICA_AONE_RUNTIME_PROFILE", "smoke")
	t.Setenv("MULTICA_AONE_RUNTIME_MODEL", "default")
	t.Setenv("PATH", os.Getenv("PATH"))

	cfg, err := LoadConfig(Overrides{DaemonID: "daemon-test"})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.Agents) != 1 {
		t.Fatalf("agents = %#v, want exactly one aone runtime", cfg.Agents)
	}
	entry, ok := cfg.Agents["aone_cloud_cli"]
	if !ok {
		t.Fatalf("agents = %#v, missing aone_cloud_cli", cfg.Agents)
	}
	if entry.Path != "http://127.0.0.1:3211" || entry.Token != "secret" || entry.Profile != "smoke" || entry.Model != "default" {
		t.Fatalf("entry = %+v", entry)
	}
}
