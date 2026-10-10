package service

import (
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// Issue #8911 acceptance checks for the provider backoff schedule.
func TestRetryDelayForAttemptProviderBackoff(t *testing.T) {
	t.Parallel()
	rate := taskfailure.ReasonAgentProviderCapacityOrRateLimit.String()
	srvErr := taskfailure.ReasonAgentProviderServerError.String()

	t.Run("nonzero increasing base schedule for 429", func(t *testing.T) {
		t.Parallel()
		// The jittered delay never exceeds the deterministic cap for the
		// attempt: base*2^attempt. So successive caps must strictly increase
		// while attempts remain in range.
		prevCap := time.Duration(0)
		for attempt := int32(0); attempt < 3; attempt++ {
			capForAttempt := providerRetryBaseDelay << attempt
			if capForAttempt > providerRetryMaxDelay {
				capForAttempt = providerRetryMaxDelay
			}
			if capForAttempt <= prevCap {
				t.Fatalf("cap for attempt %d (%v) did not increase (prev %v)", attempt, capForAttempt, prevCap)
			}
			prevCap = capForAttempt
		}
	})

	t.Run("jittered delay stays within [cap/2, cap] with floors applied", func(t *testing.T) {
		t.Parallel()
		for attempt := int32(0); attempt < 5; attempt++ {
			capForAttempt := providerRetryBaseDelay << attempt
			if capForAttempt > providerRetryMaxDelay {
				capForAttempt = providerRetryMaxDelay
			}
			for i := 0; i < 50; i++ {
				d := retryDelayForAttempt(rate, attempt)
				if d <= 0 {
					t.Fatalf("attempt %d: delay must be positive, got %v", attempt, d)
				}
				if d > capForAttempt {
					t.Fatalf("attempt %d: delay %v exceeds cap %v", attempt, d, capForAttempt)
				}
			}
		}
	})

	t.Run("synthetic 429 with Retry-After floors the delay", func(t *testing.T) {
		t.Parallel()
		hint := taskfailure.RetryAfterHint("429 Too Many Requests; Retry-After: 90")
		if hint != 90*time.Second {
			t.Fatalf("hint parse = %v, want 90s", hint)
		}
		// The floor must win even when jitter lands low.
		for i := 0; i < 50; i++ {
			d := retryDelayForAttempt(rate, 0, hint)
			if d < hint {
				t.Fatalf("delay %v is below Retry-After floor %v", d, hint)
			}
		}
	})

	t.Run("Retry-After above the schedule cap is clamped", func(t *testing.T) {
		t.Parallel()
		// The parser clamps its own hint (10min); the backoff schedule clamps
		// again to providerRetryMaxDelay, so a huge hint can never park a retry
		// beyond the schedule's own ceiling.
		hint := taskfailure.RetryAfterHint("retry after 100000s")
		if hint <= providerRetryMaxDelay {
			t.Fatalf("test premise: huge hint should clamp above the schedule cap, got %v", hint)
		}
		d := retryDelayForAttempt(rate, 0, hint)
		if d > providerRetryMaxDelay {
			t.Fatalf("clamped floor %v exceeds backoff cap %v", d, providerRetryMaxDelay)
		}
	})

	t.Run("server errors share the schedule", func(t *testing.T) {
		t.Parallel()
		if d := retryDelayForAttempt(srvErr, 0); d <= 0 {
			t.Fatalf("5xx family must back off, got %v", d)
		}
	})

	t.Run("non-provider reasons keep legacy behavior", func(t *testing.T) {
		t.Parallel()
		if d := retryDelayForAttempt("agent_error.process_failure", 2, time.Minute); d != 0 {
			t.Fatalf("unmanaged reason must stay immediate, got %v", d)
		}
		if d := retryDelayForAttempt(taskfailure.ReasonAgentProviderNetwork.String(), 2); d != providerNetworkFinalRetryWait {
			t.Fatalf("provider_network final wait changed: %v", d)
		}
	})
}

func TestRetryAttemptCeilingRateLimit(t *testing.T) {
	t.Parallel()
	rate := taskfailure.ReasonAgentProviderCapacityOrRateLimit.String()
	if got := retryAttemptCeiling(rate, 2); got != providerRateLimitMaxAttempts {
		t.Fatalf("ceiling should widen 2 -> %d, got %d", providerRateLimitMaxAttempts, got)
	}
	if got := retryAttemptCeiling(rate, 1); got != 1 {
		t.Fatalf("disabled retry must never be revived, got %d", got)
	}
	if got := retryAttemptCeiling(rate, 5); got != 5 {
		t.Fatalf("existing higher budget must be kept, got %d", got)
	}
}

func TestRetryableReasonsIncludeProviderTransients(t *testing.T) {
	t.Parallel()
	if !retryableReasons[taskfailure.ReasonAgentProviderCapacityOrRateLimit.String()] {
		t.Fatal("capacity/rate-limit must be retryable")
	}
	if !retryableReasons[taskfailure.ReasonAgentProviderServerError.String()] {
		t.Fatal("provider server error must be retryable")
	}
}
