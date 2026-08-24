package main

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
)

// TestCodexCapacityRetryCountBounds pins the two constants the boundary cases
// below are written against. They are spelled as literals in those cases on
// purpose — the ceiling is a product decision, so moving it should be a
// deliberate edit that updates the expectations, not a silent re-derivation.
func TestCodexCapacityRetryCountBounds(t *testing.T) {
	if service.DefaultCodexCapacityRetryCount != 6 {
		t.Fatalf("DefaultCodexCapacityRetryCount = %d, want 6", service.DefaultCodexCapacityRetryCount)
	}
	if service.MaxCodexCapacityRetryCount != 20 {
		t.Fatalf("MaxCodexCapacityRetryCount = %d, want 20", service.MaxCodexCapacityRetryCount)
	}
}

func TestParseCodexCapacityRetryCountAccepts(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int32
	}{
		{"empty uses default", "", 6},
		{"blank uses default", "   ", 6},
		{"zero disables the policy", "0", 0},
		{"positive count", "3", 3},
		{"surrounding whitespace", " 3 ", 3},
		{"default is inside the ceiling", "6", 6},
		{"one below maximum", "19", 19},
		{"maximum accepted", "20", 20},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseCodexCapacityRetryCount(tc.raw)
			if err != nil {
				t.Fatalf("parseCodexCapacityRetryCount(%q) returned error: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("parseCodexCapacityRetryCount(%q) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}

// TestParseCodexCapacityRetryCountRejects covers the blocker this ceiling
// exists for: capacity retries fire with zero delay and each one writes a task
// row, launches a runtime and calls the provider, so a value past the maximum
// must stop the boot rather than fall back to the default and look configured.
// The three int32-scale cases are the values the pre-ceiling parser accepted.
func TestParseCodexCapacityRetryCountRejects(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"negative", "-1"},
		{"malformed", "six"},
		{"trailing garbage", "3x"},
		{"one past maximum", "21"},
		{"former int32 ceiling", "2147483646"},
		{"overflowing total", "2147483647"},
		{"int32 overflow", "2147483648"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseCodexCapacityRetryCount(tc.raw)
			if err == nil {
				t.Fatalf("parseCodexCapacityRetryCount(%q) = %d, want error", tc.raw, got)
			}
			if got != 0 {
				t.Fatalf("parseCodexCapacityRetryCount(%q) = %d alongside an error, want 0", tc.raw, got)
			}
		})
	}
}

// TestCodexCapacityRetryCountFallsBackWhenUnset pins the nil-versus-zero
// distinction RouterOptions carries: tests and NewRouter leave the pointer nil
// and must get the default, while an explicit zero disables the policy.
func TestCodexCapacityRetryCountFallsBackWhenUnset(t *testing.T) {
	if got := codexCapacityRetryCount(RouterOptions{}); got != service.DefaultCodexCapacityRetryCount {
		t.Fatalf("unset = %d, want %d", got, service.DefaultCodexCapacityRetryCount)
	}
	disabled := int32(0)
	if got := codexCapacityRetryCount(RouterOptions{CodexCapacityRetryCount: &disabled}); got != 0 {
		t.Fatalf("explicit zero = %d, want 0", got)
	}
	configured := int32(11)
	if got := codexCapacityRetryCount(RouterOptions{CodexCapacityRetryCount: &configured}); got != 11 {
		t.Fatalf("explicit value = %d, want 11", got)
	}
}
