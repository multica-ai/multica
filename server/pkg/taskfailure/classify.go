package taskfailure

import (
	"regexp"
	"strings"
)

// providerHTTP5xxRe matches a 3-digit number starting with 5 (5xx HTTP
// status code) that isn't surrounded by other digits. Mirrors the SQL
// regex `(^|[^0-9])5[0-9][0-9]([^0-9]|$)` from MUL-1949 — keeps phrases
// like "1500ms" or "1.5.0" from accidentally landing in
// provider_server_error.
//
// Compiled at package init: the classifier is on the in-flight write
// path for every failed task, so paying the regex compile cost at
// startup rather than per-call matters.
var providerHTTP5xxRe = regexp.MustCompile(`(^|[^0-9])5[0-9][0-9]([^0-9]|$)`)

// httpAuthCodeRe / httpQuotaCodeRe / httpCapacityCodeRe match specific 3-digit
// HTTP status codes only when they are NOT embedded in a longer number, using
// the same digit-boundary guard as providerHTTP5xxRe. Without this guard the
// bare substrings "401"/"402"/"403"/"429"/"529" fire on unrelated numbers —
// e.g. "402913 tokens", "15290ms", "exit status 4030" — misclassifying process
// or unknown failures as provider billing / rate-limit errors. That pollutes
// failure observability: a genuine process crash gets filed under a provider
// bucket, masking the real cause on failure dashboards. (It does not change
// auto-retry — Classify only ever returns agent_error.* reasons, none of which
// are in internal/service/task.go's retryableReasons set.) The 5xx bucket was
// already anchored for exactly this reason (MUL-1949); these codes were not.
var (
	httpAuthCodeRe     = regexp.MustCompile(`(^|[^0-9])(401|403)([^0-9]|$)`)
	httpQuotaCodeRe    = regexp.MustCompile(`(^|[^0-9])402([^0-9]|$)`)
	httpCapacityCodeRe = regexp.MustCompile(`(^|[^0-9])(429|529)([^0-9]|$)`)
)

// Classify maps a free-form error string from the agent runtime / CLI
// to one of the 14 agent_error.* sub-reasons. Always returns a valid
// Reason; falls back to ReasonAgentUnknown when no rule matches and for
// empty input.
//
// The rule order mirrors the SQL CASE expression in MUL-1949
// (db-boy's offline backfill query). The SQL is the source of truth:
// when the two diverge, this Go classifier is wrong and should be
// updated to match. Keeping them in lock-step is required so that
// in-flight rows and historically backfilled rows share the same
// taxonomy.
//
// Matching is case-insensitive substring against the lowercased input.
// More-specific rules come before more-generic ones (e.g.
// context_overflow before provider_quota_limit, because "token limit"
// would otherwise be claimed by the quota bucket via "limit").
//
// Why a substring classifier rather than structured error codes: the
// 11 backend wrappers (server/pkg/agent/*) all surface upstream API
// failures verbatim in Result.Error, often as `API Error: 400 {...}`
// or as raw stderr tails. Insisting on structured codes would require
// touching every backend; a string classifier gives us refined
// failure_reason today and lets per-backend structured upgrades land
// independently.
func Classify(rawError string) Reason {
	trimmed := strings.TrimSpace(rawError)
	if trimmed == "" {
		// SQL maps NULL/empty to a separate bucket ("empty_error"),
		// but that bucket is not part of the canonical 21. In-flight
		// callers should never hand us empty input — if they do, the
		// safest landing is the catchall.
		return ReasonAgentUnknown
	}
	lower := strings.ToLower(trimmed)

	switch {
	// 1. Context / token window overflow. Checked early so "token
	//    limit" doesn't get swallowed by the broader "limit" / "quota"
	//    rule below.
	case containsAny(lower,
		"context length",
		"context_length_exceeded",
		"maximum context",
		"prompt is too long",
		"context size has been exceeded",
	),
		// SQL had `%token%limit%` — ILIKE wildcard between tokens. We
		// approximate with both substrings present, which catches
		// "token limit", "tokens per minute limit", etc., without the
		// false positives a naive `Contains("token") || Contains("limit")`
		// would generate.
		strings.Contains(lower, "token") && strings.Contains(lower, "limit"):
		return ReasonAgentContextOverflow

	// 2. Missing config / API key. Checked before auth because
	//    "missing API key" partly overlaps with "invalid api key"
	//    wording but is structurally a config error, not an auth
	//    rejection.
	case strings.Contains(lower, "missing environment variable"),
		strings.Contains(lower, "missing") && strings.Contains(lower, "api_key"),
		strings.Contains(lower, "api key") && strings.Contains(lower, "required"),
		strings.Contains(lower, "no llm provider configured"),
		strings.Contains(lower, "no provider configured"):
		return ReasonAgentMissingConfig

	// 3. Auth / access. 401 / 403 / "Not logged in" / invalid token
	//    / lacks access to the model. Status codes use a digit boundary
	//    so "4030" / "1401ms" don't spuriously land here.
	case httpAuthCodeRe.MatchString(lower),
		containsAny(lower,
			"unauthorized",
			"login required",
			"not logged in",
			"please login again",
			"refresh token",
			"invalid api key",
			"access token",
			"subscription access",
			"does not have access",
			"you may not have access",
		):
		return ReasonAgentProviderAuthOrAccess

	// 4. Quota / billing. 402 / insufficient balance / monthly usage
	//    limit / credits exhausted.
	case httpQuotaCodeRe.MatchString(lower),
		containsAny(lower,
			"insufficient_balance",
			"balance is too low",
			"monthly usage limit",
			"usage limit",
			"you've hit your limit",
			// Curly apostrophe variant: providers and copy-pasted error
			// strings sometimes use U+2019 instead of ASCII '. SQL ILIKE
			// would not match the curly form either, so this is a small
			// in-flight improvement on top of the SQL classifier.
			"you\u2019ve hit your limit",
			"credits",
			"quota",
		):
		return ReasonAgentProviderQuotaLimit

	// 5. Capacity / rate limit. 429 / 529 / overloaded / rate limit.
	case httpCapacityCodeRe.MatchString(lower),
		containsAny(lower,
			"rate limit",
			"overloaded",
			"no capacity available",
		):
		return ReasonAgentProviderCapacityOrRateLimit

	// 6. Provider 5xx / server error. The 5xx regex is checked here
	//    rather than as plain string matches because the SQL uses an
	//    anchored regex — see providerHTTP5xxRe's docstring.
	case containsAny(lower,
		"server had an error",
		"provider returned error",
		"internal error",
		"service unavailable",
		"bad gateway",
	),
		providerHTTP5xxRe.MatchString(lower):
		return ReasonAgentProviderServerError

	// 7. Provider network. Stream cut, dial failures, DNS / I/O
	//    timeout below the HTTP layer.
	case containsAny(lower,
		"stream disconnected",
		"error sending request",
		"unable to connect",
		"dial tcp",
		"connection refused",
		"connectionrefused",
		"dns",
		"i/o timeout",
	):
		return ReasonAgentProviderNetwork

	// 8. Model not found / unavailable. The SQL uses `%model%not%found%`,
	//    which matches "model … not found" with anything in between;
	//    we approximate with both substrings present, which captures
	//    typical phrasings like "model X not found" and "the requested
	//    model was not found".
	case strings.Contains(lower, "model") && strings.Contains(lower, "not found"),
		containsAny(lower,
			"unknown model",
			"selected model",
			"http 404",
			"404 page not found",
		):
		return ReasonAgentModelNotFoundOrUnavailable

	// 9. Empty / unparseable output from the agent CLI itself. These
	//    strings come from server/pkg/agent/*.go wrappers and are
	//    stable.
	case containsAny(lower,
		"returned empty output",
		"returned no parseable output",
	):
		return ReasonAgentEmptyOrUnparseableOutput

	// 10. Agent subprocess hard timeout (per-task wall clock).
	case strings.Contains(lower, "timed out after"):
		return ReasonAgentTimeout

	// 11. Runner CLI binary missing.
	case strings.Contains(lower, "executable not found"):
		return ReasonAgentRuntimeMissingExecutable

	// 12. Runner CLI version too old / incompatible protocol.
	case containsAny(lower,
		"below the minimum supported version",
		"requires a newer version",
	):
		return ReasonAgentRuntimeVersionUnsupported

	// 13. Agent / runner process-level failure. Checked last among
	//     specific rules because "exit status" / "signal" can co-occur
	//     with more specific upstream errors that SHOULD win (e.g. an
	//     agent that crashed *because* the provider rate-limited it
	//     should be classified as rate-limited, not as a process
	//     failure).
	case containsAny(lower,
		"exit status",
		"signal",
		"panic",
		"sigsegv",
		"process exited",
		"pipe has been ended",
		"file already closed",
		"initialize failed",
	):
		return ReasonAgentProcessFailure
	}

	return ReasonAgentUnknown
}

// containsAny reports whether s contains any of the supplied substrings.
// Caller is responsible for lowercasing s ahead of time so the helper
// stays cheap on the hot path — pre-lowercasing once is faster than
// case-folding inside each substring scan.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
