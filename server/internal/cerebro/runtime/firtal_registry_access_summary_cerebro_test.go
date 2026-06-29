package runtime

import (
	"strings"
	"testing"
)

// FIR-2208: after retiring the allowlist (System A), registryAccessSummary is a
// generic no-arg function — access scoping is now entirely in the tool-policy
// chain, so the summary just nudges the model to call list_data_sources.
func TestRegistryAccessSummary(t *testing.T) {
	got := registryAccessSummary()
	if got == "" {
		t.Fatal("expected a non-empty access summary")
	}
	if !strings.Contains(got, "list_data_sources") {
		t.Fatalf("expected summary to mention list_data_sources, got: %q", got)
	}
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected %q to contain %q", haystack, needle)
	}
}
