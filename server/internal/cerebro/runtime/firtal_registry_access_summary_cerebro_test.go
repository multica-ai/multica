package runtime

import (
	"strings"
	"testing"
)

// FIR-1914: the per-agent firtal_registry access line must be derived purely
// from the grant config, must stay deny-by-default (no claim of access the
// allowlist does not grant), and must always nudge the model to call
// list_data_sources instead of guessing it has no database.
func TestRegistryAccessSummary(t *testing.T) {
	t.Run("allowed_all advertises full access", func(t *testing.T) {
		got := registryAccessSummary(firtalRegistryGrantConfig{AllowedAll: true})
		if got == "" {
			t.Fatal("expected a summary for allowed_data_sources_all=true, got empty")
		}
		assertContains(t, got, "ALL data sources")
		assertContains(t, got, "list_data_sources")
	})

	t.Run("counts allowed sources and pluralizes", func(t *testing.T) {
		got := registryAccessSummary(firtalRegistryGrantConfig{
			AllowedDataSources: []string{"a", "b", "c"},
		})
		assertContains(t, got, "3 data sources")
		assertContains(t, got, "list_data_sources")
	})

	t.Run("single source is singular", func(t *testing.T) {
		got := registryAccessSummary(firtalRegistryGrantConfig{
			AllowedDataSources: []string{"only-one"},
		})
		assertContains(t, got, "1 data source")
		if strings.Contains(got, "1 data sources") {
			t.Fatalf("expected singular noun, got: %q", got)
		}
	})

	t.Run("deny-by-default yields no claim of access", func(t *testing.T) {
		got := registryAccessSummary(firtalRegistryGrantConfig{})
		if got != "" {
			t.Fatalf("expected empty summary when nothing is granted, got: %q", got)
		}
	})
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected %q to contain %q", haystack, needle)
	}
}
