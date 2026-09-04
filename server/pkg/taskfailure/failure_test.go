package taskfailure

import (
	"testing"
)

// TestAllReasonsIsDefensiveCopy guards the contract that mutating the
// returned slice cannot corrupt the package-level fixture. Without
// this, two callers (e.g. two Prometheus collectors at startup) could
// race on a shared slice.
func TestAllReasonsIsDefensiveCopy(t *testing.T) {
	t.Parallel()

	first := AllReasons()
	if len(first) == 0 {
		t.Fatal("AllReasons() returned empty slice")
	}
	original := first[0]
	first[0] = "tampered"

	second := AllReasons()
	if second[0] == "tampered" {
		t.Fatalf("AllReasons() leaked package state: second call returned tampered value %q", second[0])
	}
	if second[0] != original {
		t.Fatalf("AllReasons()[0] = %q, want %q", second[0], original)
	}
}
