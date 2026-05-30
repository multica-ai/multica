// Package account contains cerebro-only account management code, including
// the rate-limit reset parser used to schedule auto-unpause for 429 responses.
package account

import (
	"time"

	cerebratelimit "github.com/multica-ai/multica/server/internal/cerebro/ratelimit"
)

// DefaultRateLimitBackoff is the fallback pause duration when no reset
// hint is parseable. Delegates to cerebratelimit.DefaultBackoff.
const DefaultRateLimitBackoff = cerebratelimit.DefaultBackoff

// ParseRateLimitReset extracts a runtime-unpause timestamp from a
// free-form provider error string. Delegates to cerebratelimit.ParseReset.
func ParseRateLimitReset(errText string, now time.Time) (time.Time, bool) {
	return cerebratelimit.ParseReset(errText, now)
}

// ClassifyRateLimitReset is the three-valued variant: (resetAt, hasReset,
// pauseWorthy). Used by the auto-pause circuit breaker to tell a concrete
// provider reset time apart from the no-hint fallback so it can apply a
// growing backoff in the fallback case. Delegates to cerebratelimit.ClassifyReset.
func ClassifyRateLimitReset(errText string, now time.Time) (time.Time, bool, bool) {
	return cerebratelimit.ClassifyReset(errText, now)
}
