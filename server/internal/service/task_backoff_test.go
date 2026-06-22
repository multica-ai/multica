package service

import (
	"testing"
	"time"
)

func TestRateLimitBackoff(t *testing.T) {
	cases := []struct {
		attempt int32
		min     time.Duration
		max     time.Duration
	}{
		// First retry (attempt 1): 60s ±20% → [48s, 72s]
		{attempt: 1, min: 48 * time.Second, max: 72 * time.Second},
		// Second retry: 180s ±20% → [144s, 216s]
		{attempt: 2, min: 144 * time.Second, max: 216 * time.Second},
		// Third retry: 540s ±20% → [432s, 648s]
		{attempt: 3, min: 432 * time.Second, max: 648 * time.Second},
		// Fourth retry: 1620s ±20% → [1296s, 1944s] (clamped at 1800s by cap)
		{attempt: 4, min: 1296 * time.Second, max: 1800 * time.Second},
		// Fifth retry: pre-jitter is 4860s, capped at 1800s after jitter
		{attempt: 5, min: 1800 * time.Second, max: 1800 * time.Second},
		{attempt: 10, min: 1800 * time.Second, max: 1800 * time.Second},
		// Zero/negative attempt: treated as attempt 1
		{attempt: 0, min: 48 * time.Second, max: 72 * time.Second},
		{attempt: -1, min: 48 * time.Second, max: 72 * time.Second},
	}

	for _, tc := range cases {
		got := rateLimitBackoff(tc.attempt)
		if got < tc.min || got > tc.max {
			t.Errorf("rateLimitBackoff(%d) = %v, want [%v, %v]",
				tc.attempt, got, tc.min, tc.max)
		}
	}
}
