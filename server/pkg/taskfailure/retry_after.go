package taskfailure

import (
	"regexp"
	"strconv"
	"time"
)

// Retry-After bounds for provider rate-limit recovery. Providers (and the
// CLIs that surface their errors) express the hint either as delay-seconds
// ("retry after 30s", "retry-after: 120") or as an HTTP-date; only
// delay-seconds is machine-recoverable from an error string, so that is all
// this parser honors. The ceiling keeps a pathological hint (or a date-like
// value misparsed as a huge number) from parking a retry for hours — the
// backoff schedule's own cap stays authoritative (issue #8911).
const (
	maxRetryAfterHint = 10 * time.Minute
)

var retryAfterPatterns = []*regexp.Regexp{
	// "retry after 30s" / "Retry-After: 120" / "please retry after 2 minutes"
	regexp.MustCompile(`(?i)retry[-_ ]?after\s*:?\s*(\d+)\s*(seconds?|secs?|s|minutes?|mins?|m)?`),
	// bare "retry in 45s" wording some CLIs emit
	regexp.MustCompile(`(?i)retry[-_ ]?in\s+(\d+)\s*(seconds?|secs?|s|minutes?|mins?|m)`),
}

// RetryAfterHint extracts a retry-after delay from a provider error string,
// when the CLI echoed one. Returns 0 when no parseable hint is present — the
// caller's backoff schedule then remains the only delay source. Any hint is
// clamped to maxRetryAfterHint.
func RetryAfterHint(errText string) time.Duration {
	if errText == "" {
		return 0
	}
	if len(errText) > 8192 {
		// Failure payloads can embed entire rollouts; the hint, when present,
		// always sits in the provider's leading error block.
		errText = errText[:8192]
	}
	for _, re := range retryAfterPatterns {
		m := re.FindStringSubmatch(errText)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil || n <= 0 {
			continue
		}
		// Unit is optional for the header form ("Retry-After: 120" is
		// delay-seconds per RFC 7231); everything without an explicit m/min
		// marker is seconds.
		unit := time.Second
		if len(m[2]) > 0 && (m[2][0] == 'm' || m[2][0] == 'M') {
			unit = time.Minute
		}
		d := time.Duration(n) * unit
		if d > maxRetryAfterHint {
			d = maxRetryAfterHint
		}
		return d
	}
	return 0
}
