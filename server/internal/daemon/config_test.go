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

func TestParseBareRepoMap(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantLen int
		wantErr bool
	}{
		{"empty → nil map, no error", "", 0, false},
		{"whitespace-only → nil map, no error", "   ", 0, false},
		{"happy path", `{"rabbeet/Pulse":"/srv/pulse-bare.git","rabbeet/multica":"/srv/multica-bare.git"}`, 2, false},
		{"single entry", `{"rabbeet/Pulse":"/srv/pulse-bare.git"}`, 1, false},
		{"malformed JSON", `not-json`, 0, true},
		{"key without slash", `{"justname":"/srv/x.git"}`, 0, true},
		{"empty value", `{"rabbeet/Pulse":"   "}`, 0, true},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got, err := parseBareRepoMap(c.input)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got map=%v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != c.wantLen {
				t.Fatalf("got len=%d want=%d (map=%v)", len(got), c.wantLen, got)
			}
		})
	}
}
