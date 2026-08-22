package main

import (
	"testing"
	"time"
)

// TestQueuedTTLFromEnv pins the env parsing for the queued TTL (the same
// helper and default main passes into runRuntimeSweeper): unset,
// unparseable, or non-positive values fall back to the documented default
// so existing deployments behave identically. A zero TTL must stay invalid:
// it would expire every queued task immediately.
func TestQueuedTTLFromEnv(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{name: "unset keeps the built-in default", value: "", want: 2 * time.Hour},
		{name: "positive duration overrides the default", value: "12h", want: 12 * time.Hour},
		{name: "unparseable value falls back to the default", value: "not-a-duration", want: 2 * time.Hour},
		{name: "non-positive value falls back to the default", value: "0s", want: 2 * time.Hour},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MULTICA_TASK_QUEUED_TTL", tc.value)

			got := envDuration("MULTICA_TASK_QUEUED_TTL", defaultTaskQueuedTTL)
			if got != tc.want {
				t.Errorf("MULTICA_TASK_QUEUED_TTL=%q -> %s, want %s", tc.value, got, tc.want)
			}
		})
	}
}

// TestDefaultTaskQueuedTTL pins the built-in default so the documented 2h
// window and the code cannot drift apart.
func TestDefaultTaskQueuedTTL(t *testing.T) {
	if defaultTaskQueuedTTL != 2*time.Hour {
		t.Errorf("defaultTaskQueuedTTL = %s, want 2h", defaultTaskQueuedTTL)
	}
}
