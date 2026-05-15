package account

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// rateLimitPairRe matches x-ratelimit-limit-* and x-ratelimit-remaining-* header
// values emitted by Claude Code's verbose log stream. We use these to compute a
// usage percentage when no explicit *-pct header is present.
//
// The "tokens" family takes priority over "requests" since it is more granular.
// Named captures: "kind" (tokens|requests), "val" (integer value).
var rateLimitLimitRe = regexp.MustCompile(
	`(?i)x-ratelimit-limit-(input-)?tokens[^\d]*(\d+)`,
)
var rateLimitRemainingRe = regexp.MustCompile(
	`(?i)x-ratelimit-remaining-(input-)?tokens[^\d]*(\d+)`,
)

// AdapterEvent is what the daemon emits to the server when it observes an
// account-relevant signal (429 or usage-percent) in adapter output. Both
// fields are pointer-typed so a single observation can patch either one
// (or both) without clobbering an unrelated previous value. A non-nil
// pointer that wraps a nil/zero is treated as an explicit clear by the
// server — see Service.UpdateUsage.
type AdapterEvent struct {
	// ThrottledUntil is the parsed retry-after target. Set when the adapter
	// surfaced an HTTP 429 with a parseable reset hint.
	ThrottledUntil *time.Time
	// UsageWindowPct is the % of the current usage window consumed,
	// 0..100. Set when the adapter surfaced an Anthropic-style usage
	// header / system message (e.g. "limits.input_tokens_pct=87").
	UsageWindowPct *float32
}

// HasSignal reports whether the event carries at least one field worth
// POSTing.
func (e AdapterEvent) HasSignal() bool {
	return e.ThrottledUntil != nil || e.UsageWindowPct != nil
}

// ParseAdapterOutput examines a single chunk of adapter output for
// account-state signals. Returns an event with whatever signals it could
// parse; an empty event (HasSignal() == false) means there was nothing
// to report.
//
// Two signal families:
//
//  1. 429 → ThrottledUntil. Uses ParseRateLimitReset (epoch, ISO-8601,
//     wall-clock with TZ hint, relative "try again in 5m").
//
//  2. Usage percent → UsageWindowPct. Matches:
//     - Anthropic-style `limits.x_tokens_pct=NN` / `pct: NN` / `NN%`
//     headers and SDK system messages.
//     - "Used 87% of your weekly usage window" prose lines that some
//     Claude Code CLI versions print.
//     Values are clamped to [0, 100] and returned as float32 to match
//     the DB column type.
//
// The parser is intentionally permissive — false positives are cheap
// (we overwrite usage_window_pct on the next signal anyway), false
// negatives mean we lose a single update.
func ParseAdapterOutput(output string, now time.Time) AdapterEvent {
	var ev AdapterEvent
	if strings.TrimSpace(output) == "" {
		return ev
	}

	if reset, ok := ParseRateLimitReset(output, now); ok {
		ev.ThrottledUntil = &reset
	}
	if pct, ok := parseUsagePct(output); ok {
		ev.UsageWindowPct = &pct
	} else if pct, ok := parseRateLimitPairPct(output); ok {
		// Fallback: compute pct from x-ratelimit-limit-* / x-ratelimit-remaining-*
		// absolute header values that Claude Code's --verbose stream emits on every
		// Anthropic API call. This ensures quota data is reported even when the
		// agent never emits a prose usage-window warning (JEH-1365).
		ev.UsageWindowPct = &pct
	}
	return ev
}

// usagePctRe captures usage-percent signals in the shapes we have
// observed across Claude API responses and Claude Code CLI prose. The
// first numeric capture group is the percent (integer or decimal,
// 0..100). Anchored by a "usage" / "limit" / "window" context word to
// keep the parser from grabbing every "87%" that appears in agent
// chatter.
//
// Examples it catches:
//
//	x-ratelimit-input-tokens-pct: 87
//	"limits": {"input_tokens_pct": 87.5}
//	You have used 87% of your usage window
//	weekly usage: 42.3%
//	usage_window: 95
//
// Examples it intentionally misses:
//
//	"the agent answered 100% of the questions"   (no usage context)
//	"87% reduction in latency"                   (no usage context)
var usagePctRe = regexp.MustCompile(
	`(?i)(?:usage[_\s-]?(?:window|limit|pct)|\w*_tokens_pct|window[_\s-]?pct|x-ratelimit-[\w-]*pct)` +
		`[^\d-]{0,40}?(\d{1,3}(?:\.\d+)?)\s*%?`,
)

// usagePctProseRe matches the human-readable "used NN% of your usage
// window" shape. Separate from usagePctRe so the percent appears BEFORE
// the context word.
var usagePctProseRe = regexp.MustCompile(
	`(?i)(\d{1,3}(?:\.\d+)?)\s*%\s*of\s+(?:your\s+)?(?:weekly\s+|current\s+)?usage(?:[\s_-]window)?`,
)

func parseUsagePct(output string) (float32, bool) {
	for _, re := range []*regexp.Regexp{usagePctRe, usagePctProseRe} {
		m := re.FindStringSubmatch(output)
		if m == nil {
			continue
		}
		v, err := strconv.ParseFloat(m[1], 32)
		if err != nil {
			continue
		}
		if v < 0 || v > 100 {
			continue
		}
		return float32(v), true
	}
	return 0, false
}

// parseRateLimitPairPct computes a usage percentage from the Anthropic API's
// absolute x-ratelimit-limit-* and x-ratelimit-remaining-* headers that Claude
// Code logs in --verbose mode. Returns (pct, true) when both limit and remaining
// are present and limit > 0; otherwise (0, false).
//
// This is a best-effort approximation: the per-minute token rate-limit window is
// narrower than the 5-hour Claude Pro usage window, but it still gives the server
// non-null data after every run. The prose usage-warning parser (parseUsagePct)
// takes priority when a proper window-percentage is available.
func parseRateLimitPairPct(output string) (float32, bool) {
	lm := rateLimitLimitRe.FindStringSubmatch(output)
	rm := rateLimitRemainingRe.FindStringSubmatch(output)
	if lm == nil || rm == nil {
		return 0, false
	}
	limit, err := strconv.ParseFloat(lm[2], 64)
	if err != nil || limit <= 0 {
		return 0, false
	}
	remaining, err := strconv.ParseFloat(rm[2], 64)
	if err != nil || remaining < 0 {
		return 0, false
	}
	pct := float32((1.0 - remaining/limit) * 100.0)
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct, true
}
