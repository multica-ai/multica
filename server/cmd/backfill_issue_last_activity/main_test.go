package main

import (
	"strings"
	"testing"
)

func TestStalledPassGuardFailsAfterConsecutiveNoProgressPasses(t *testing.T) {
	guard := stalledPassGuard{max: 2}

	if err := guard.observe(0, 7); err != nil {
		t.Fatalf("first stalled pass: %v", err)
	}
	if err := guard.observe(3, 4); err != nil {
		t.Fatalf("progressing pass: %v", err)
	}
	if guard.consecutive != 0 {
		t.Fatalf("progress did not reset stalled passes: got %d", guard.consecutive)
	}
	if err := guard.observe(0, 4); err != nil {
		t.Fatalf("first stalled pass after progress: %v", err)
	}
	err := guard.observe(0, 4)
	if err == nil || !strings.Contains(err.Error(), "2 consecutive passes") || !strings.Contains(err.Error(), "4 rows remaining") {
		t.Fatalf("second stalled pass error = %v, want count and remaining backlog", err)
	}
}

func TestStalledPassGuardCanBeExplicitlyDisabled(t *testing.T) {
	guard := stalledPassGuard{max: 0}
	for range 100 {
		if err := guard.observe(0, 1); err != nil {
			t.Fatalf("disabled guard: %v", err)
		}
	}
}
