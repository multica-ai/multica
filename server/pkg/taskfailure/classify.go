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
// bucket, masking the real cause on failure dashboards. (A misfire here still
// can't cause a spurious retry: the auth / quota / capacity buckets these
// regexes guard are all non-retryable. The only agent_error.* reason on
// internal/service/task.go's retryableReasons allowlist is provider_network
// — MUL-4910 — and these regexes never route into it.) The 5xx bucket was
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
		// but that bucket is not part of the canonical 22. In-flight
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
		containsAny(lower, contextWindowExceededWitnesses...),
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
	//    timeout below the HTTP layer. "connection closed" / "mid-response"
	//    catch the Claude Code CLI's mid-stream disconnect
	//    ("API Error: Connection closed mid-response. ...") so a transient cut
	//    lands in the retryable provider_network bucket (with session resume)
	//    instead of falling through to agent_error.unknown / process_failure
	//    and terminating the task (MUL-4910). Checked before rule 13 so the
	//    "... exited with error: exit status N ..." variant still routes here.
	//
	//    "deadline exceeded" covers every Go-side context deadline that
	//    reaches the classifier as text — `context deadline exceeded` from a
	//    cancelled request, and net/http's `Client.Timeout exceeded while
	//    awaiting headers` variant. Before MUL-5370 these all landed in
	//    agent_error.unknown, which is not on the retry allowlist, so a
	//    transient stall became a terminal failure with no usable label.
	//    Note this only catches deadlines that arrive as a bare string;
	//    callers holding the error value should classify structurally
	//    instead (see taskRunFailureReason in daemon/daemon.go).
	//    "opencode stream ended" is the shared prefix of every failure the
	//    OpenCode terminal-signal guard raises (pkg/agent/opencode.go): a step
	//    left open at EOF, a continuation that never started, and a run that
	//    ended on a step with no text, no tool call and no reported usage.
	//    All three mean the same thing — the provider stream died and
	//    `opencode run` still exited 0 — which is this bucket by definition,
	//    and being resume-safe the retry continues the truncated session
	//    instead of redoing the work. Before this they landed in
	//    agent_error.process_failure (the word "signal" in "terminal signal"
	//    matching rule 13 by accident) and agent_error.unknown respectively;
	//    neither is on the retry allowlist, so a transient cut ended the task
	//    outright and max_attempts never applied (#6522).
	//    Mirror these substrings into the MUL-1949 offline backfill SQL.
	case containsAny(lower,
		"stream disconnected",
		opencodeStreamEndedPrefix,
		"connection closed",
		"mid-response",
		"error sending request",
		"unable to connect",
		"dial tcp",
		"connection refused",
		"connectionrefused",
		"dns",
		"i/o timeout",
		"deadline exceeded",
		"timeout exceeded while awaiting",
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

// contextWindowExceededWitnesses are the two wordings for an overflow reported
// on the RESPONSE rather than as a 400 on the request: the provider accepts the
// call and ends the turn with stop_reason "model_context_window_exceeded", which
// Claude Code 2.1.x surfaces verbatim as "API Error: The model has reached its
// context window limit." (GH #6360). Both are matched so a backend forwarding
// the raw stop reason classifies the same way as one forwarding the CLI's copy.
//
// Neither carries "token" nor any of rule 1's other phrases, so before this the
// failure landed in agent_error.unknown — a reason no resume blacklist covers,
// which left the over-full session pinned as the resume pointer and made every
// later comment on that issue replay the same overflow.
//
// Each is an unambiguous witness on its own, which is what lets
// NormalizeDaemonReason reuse them to upgrade an older daemon's catchall
// server-side. Matched against pre-lowercased text.
// Mirror these substrings into the MUL-1949 offline backfill SQL.
//
// terminal_reason=prompt_too_long joins them for GH #6402: it is the structured
// enum value Claude Code puts on the result frame when the turn ended because
// the context window is full, and the daemon quotes it verbatim into the error
// it reports (see claudeTerminalReasonFailure in pkg/agent/claude.go). Being an
// enum token rather than prose, it is at least as unambiguous as the two above
// — no free-form provider message produces it by accident — so a run classified
// from it lands in context_overflow even when the CLI's accompanying copy is
// empty or reworded between releases.
var contextWindowExceededWitnesses = []string{
	"context window limit",
	"model_context_window_exceeded",
	TerminalReasonPromptTooLong,
}

// legacySkillBundlePrefix is the exact wrapper a pre-MUL-5370 daemon put on a
// failed skill-bundle download. It is an unambiguous witness: no other code
// path ever produced it, and a current daemon writes "skill bundle
// unavailable: ..." instead.
const legacySkillBundlePrefix = "resolve skill bundles:"

// legacySkillBundleReasons are the buckets an older daemon's own classifier
// could land that failure in. All three mean "we only knew it was some
// transport fault": agent_error.unknown from a daemon predating the deadline
// rule, agent_error.provider_network from one that has it, and the
// pre-MUL-1949 coarse agent_error. None of them carries information that
// upgrading would discard.
var legacySkillBundleReasons = map[string]bool{
	string(ReasonAgentUnknown):         true,
	string(ReasonAgentProviderNetwork): true,
	"agent_error":                      true,
}

// legacyContextOverflowReasons are the buckets an older daemon lands the
// response-side context overflow in: agent_error.unknown from its own
// classifier (its rule 1 predates contextWindowExceededWitnesses) and the
// pre-MUL-1949 coarse agent_error.
//
// Deliberately narrower than legacySkillBundleReasons. A refined reason means
// the old daemon matched an earlier rule on the same text — process_failure on
// a crash marker, provider_network on a stream cut — and that says more about
// what actually ended the run than a witness appearing somewhere in the same
// blob does. Upgrading those would discard information; leaving them alone
// costs at most the pre-existing behaviour.
var legacyContextOverflowReasons = map[string]bool{
	string(ReasonAgentUnknown): true,
	"agent_error":              true,
}

// opencodeStreamEndedPrefix opens every failure the OpenCode terminal-signal
// guard raises (pkg/agent/opencode.go). Exactly one code path emits it, and it
// is a PREFIX of the whole error rather than a phrase somewhere inside it, so
// its presence identifies the failure outright.
const opencodeStreamEndedPrefix = "opencode stream ended"

// legacyOpencodeStreamEndedReasons are the buckets a daemon predating rule 7's
// entry lands these errors in: process_failure for the two "terminal signal"
// variants, whose word "signal" its rule 13 matches by accident, unknown for
// anything its rules miss, and the pre-MUL-1949 coarse agent_error.
//
// Wider than legacyContextOverflowReasons on purpose, and the witness is why.
// That rule leaves refined reasons alone because a phrase appearing somewhere
// in an error blob says less than the bucket an earlier rule already picked.
// Here the witness is the guard's own message from its first character, so the
// old bucket cannot be describing some other, better-identified cause — it is
// the same failure under a label that predates knowing what it was.
var legacyOpencodeStreamEndedReasons = map[string]bool{
	string(ReasonAgentProcessFailure): true,
	string(ReasonAgentUnknown):        true,
	"agent_error":                     true,
}

// NormalizeDaemonReason upgrades a failure_reason reported by an older daemon
// onto the taxonomy this server understands, using the raw error text as the
// witness. It returns the reason unchanged when nothing applies.
//
// Why this exists (MUL-5370): installed daemons upgrade on their own cadence,
// so a fix that only labels a failure correctly on the daemon side reaches
// nobody until every host updates. The daemon reports a non-empty reason, so
// FailTask's "classify when empty" guard does not fire, and the server would
// persist the stale label — no auto-retry, and the chat bubble falls back to
// generic copy. Recognising the wire shape an old daemon produces closes that
// gap the moment the server deploys.
//
// This is a boundary compatibility shim, not internal fallback logic: each rule
// can be deleted once no daemon old enough to produce its wire shape is still
// reporting.
func NormalizeDaemonReason(reason, rawError string) Reason {
	if legacySkillBundleReasons[reason] &&
		strings.HasPrefix(strings.TrimSpace(rawError), legacySkillBundlePrefix) {
		return ReasonSkillBundleUnavailable
	}
	// GH #6360: the same mixed-version gap, on a failure where waiting for
	// every host to update is more expensive. A daemon whose rule 1 predates
	// contextWindowExceededWitnesses reports the catchall, and the catchall is
	// on no resume blacklist — so the over-full session stays pinned as the
	// resume pointer and every later comment on that issue replays the same
	// overflow. One un-upgraded host means a permanently stuck (agent, issue)
	// pair, not just a missing label; upgrading here retires the session the
	// moment the server deploys.
	if legacyContextOverflowReasons[reason] &&
		containsAny(strings.ToLower(rawError), contextWindowExceededWitnesses...) {
		return ReasonAgentContextOverflow
	}
	// #6522: the same gap once more. Rule 7 only decides where these land when
	// THIS server classifies them, and it classifies only when the daemon sent
	// no reason at all. An installed daemon predating that entry reports a
	// non-empty agent_error.process_failure instead, which the empty-reason
	// branch in FailTask deliberately skips — so the run stays off the retry
	// allowlist on exactly the un-upgraded hosts most likely to be hitting a
	// flaky provider. Upgrading here makes the retry work the moment the server
	// deploys, without waiting on the daemon fleet.
	if legacyOpencodeStreamEndedReasons[reason] &&
		strings.HasPrefix(strings.ToLower(strings.TrimSpace(rawError)), opencodeStreamEndedPrefix) {
		return ReasonAgentProviderNetwork
	}
	return Reason(reason)
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
