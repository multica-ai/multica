package main

import (
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
)

// TestCapacityRetryPolicyFromEnv pins the MULTICA_CAPACITY_RETRY_* states.
// When none of the three is set the zero policy is returned — the deployment
// stays opted out and upstream terminates on capacity failures. Setting ANY
// one of them opts in; each knob then acts independently, and an unset (or
// invalid) one is reported as 0 so the service falls back to the upstream
// schedule for what it governs (warn-and-default, like envDuration — a typo'd
// backoff narrows a retry window, it does not invalidate a security boundary).
func TestCapacityRetryPolicyFromEnv(t *testing.T) {
	t.Run("all unset keeps the upstream behavior", func(t *testing.T) {
		t.Setenv("MULTICA_CAPACITY_RETRY_MAX_ATTEMPTS", "")
		t.Setenv("MULTICA_CAPACITY_RETRY_BASE_DELAY", "")
		t.Setenv("MULTICA_CAPACITY_RETRY_MAX_DELAY", "")
		if got := capacityRetryPolicyFromEnv(); got != (service.CapacityRetryPolicy{}) {
			t.Fatalf("capacityRetryPolicyFromEnv() = %+v, want the zero policy (opted out)", got)
		}
	})

	t.Run("setting any single knob opts in", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			set  func(t *testing.T)
		}{
			{"max attempts", func(t *testing.T) {
				t.Setenv("MULTICA_CAPACITY_RETRY_MAX_ATTEMPTS", "8")
			}},
			{"base delay", func(t *testing.T) {
				t.Setenv("MULTICA_CAPACITY_RETRY_BASE_DELAY", "20s")
			}},
			{"max delay", func(t *testing.T) {
				t.Setenv("MULTICA_CAPACITY_RETRY_MAX_DELAY", "3m")
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Setenv("MULTICA_CAPACITY_RETRY_MAX_ATTEMPTS", "")
				t.Setenv("MULTICA_CAPACITY_RETRY_BASE_DELAY", "")
				t.Setenv("MULTICA_CAPACITY_RETRY_MAX_DELAY", "")
				tc.set(t)
				got := capacityRetryPolicyFromEnv()
				if got == (service.CapacityRetryPolicy{}) {
					t.Fatalf("capacityRetryPolicyFromEnv() = zero policy, want opt-in")
				}
			})
		}
	})

	t.Run("valid values map to their fields", func(t *testing.T) {
		t.Setenv("MULTICA_CAPACITY_RETRY_MAX_ATTEMPTS", "6")
		t.Setenv("MULTICA_CAPACITY_RETRY_BASE_DELAY", "45s")
		t.Setenv("MULTICA_CAPACITY_RETRY_MAX_DELAY", "90s")
		got := capacityRetryPolicyFromEnv()
		want := service.CapacityRetryPolicy{MaxAttempts: 6, BaseDelay: 45 * time.Second, MaxDelay: 90 * time.Second}
		if got != want {
			t.Fatalf("capacityRetryPolicyFromEnv() = %+v, want %+v", got, want)
		}
	})

	t.Run("invalid values read as unset, per field", func(t *testing.T) {
		t.Setenv("MULTICA_CAPACITY_RETRY_MAX_ATTEMPTS", "ten")
		t.Setenv("MULTICA_CAPACITY_RETRY_BASE_DELAY", "soon")
		t.Setenv("MULTICA_CAPACITY_RETRY_MAX_DELAY", "-5m")
		got := capacityRetryPolicyFromEnv()
		if got != (service.CapacityRetryPolicy{}) {
			t.Fatalf("capacityRetryPolicyFromEnv() = %+v, want the zero policy: invalid input reads as unset", got)
		}
	})

	t.Run("partial configuration keeps unset knobs at zero", func(t *testing.T) {
		t.Setenv("MULTICA_CAPACITY_RETRY_MAX_ATTEMPTS", "12")
		t.Setenv("MULTICA_CAPACITY_RETRY_BASE_DELAY", "")
		t.Setenv("MULTICA_CAPACITY_RETRY_MAX_DELAY", "")
		got := capacityRetryPolicyFromEnv()
		if got.MaxAttempts != 12 {
			t.Errorf("MaxAttempts = %d, want 12 (explicit override)", got.MaxAttempts)
		}
		if got.BaseDelay != 0 {
			t.Errorf("BaseDelay = %s, want 0 (unset keeps immediate retries)", got.BaseDelay)
		}
		if got.MaxDelay != 0 {
			t.Errorf("MaxDelay = %s, want 0 (unset keeps the backoff uncapped)", got.MaxDelay)
		}
	})
}
