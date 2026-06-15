package service

import (
	"testing"
	"time"
)

func TestRateLimitBackoff(t *testing.T) {
	cases := []struct {
		attempt int32
		want    time.Duration
	}{
		// First retry (attempt 1): 60s
		{attempt: 1, want: 60 * time.Second},
		// Second retry: 180s
		{attempt: 2, want: 180 * time.Second},
		// Third retry: 540s
		{attempt: 3, want: 540 * time.Second},
		// Fourth retry: 1620s (below cap)
		{attempt: 4, want: 1620 * time.Second},
		// Fifth retry and beyond: capped at 1800s
		{attempt: 5, want: 1800 * time.Second},
		{attempt: 10, want: 1800 * time.Second},
		// Zero/negative attempt: treated as attempt 1
		{attempt: 0, want: 60 * time.Second},
		{attempt: -1, want: 60 * time.Second},
	}

	for _, tc := range cases {
		got := rateLimitBackoff(tc.attempt)
		if got != tc.want {
			t.Errorf("rateLimitBackoff(%d) = %v, want %v", tc.attempt, got, tc.want)
		}
	}
}
