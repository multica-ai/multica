package taskfailure

import (
	"strings"
	"testing"
	"time"
)

func TestRetryAfterHint(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want time.Duration
	}{
		{"empty", "", 0},
		{"no hint", "Error: exceeded retry limit, last status: 429 Too Many Requests", 0},
		{"delay seconds colon", "429 Too Many Requests; Retry-After: 120", 120 * time.Second},
		{"delay seconds prose", "rate limited, please retry after 30s", 30 * time.Second},
		{"retry in wording", "retry in 45s: rate limit exceeded", 45 * time.Second},
		{"minutes unit", "Retry-After: 2 minutes", 2 * time.Minute},
		{"underscore variant", "retry_after 90 s", 90 * time.Second},
		{"case insensitive header", "RETRY-AFTER: 15", 15 * time.Second},
		{"hint beyond 8KB ignored", "x" + strings.Repeat("a", 9000) + " retry after 30s", 0},
		{"zero hint ignored", "retry after 0s", 0},
		{"negative ignored", "retry after -5s", 0},
		{"huge hint clamped to ceiling", "retry after 100000s", maxRetryAfterHint},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := RetryAfterHint(tc.in); got != tc.want {
				t.Fatalf("RetryAfterHint(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
