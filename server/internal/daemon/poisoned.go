package daemon

import (
	"strings"

	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// FailureReason values for tasks whose session is "poisoned" — i.e.
// resuming the same conversation on a follow-up task would deterministically
// reproduce the same failure. Listed here so the server-side query
// GetLastTaskSession can filter them out and the next task starts from
// a fresh agent session instead of inheriting the bad state.
//
// Two flavors:
//   - Output-side: agent "completed" with output that is actually a known
//     fallback marker (gave up mid-thought, emitted a meta message) or the
//     provider's context-window-exhausted notice (GH #6402). Detected
//     via classifyPoisonedOutput.
//   - Error-side: the LLM API itself rejected the request with a 400
//     invalid_request_error (oversized payload, malformed image, etc.).
//     The bad message is already baked into the conversation history, so
//     every resume hits the same 400. Detected via classifyPoisonedError.
//   - Timeout-side: Codex reported semantic inactivity after the session got
//     stuck without agent progress. Resuming that Codex session can replay the
//     same stuck state, while a fresh manual rerun may succeed. Detected via
//     classifyResumeUnsafeTimeout.
//   - Transport-side: a Codex thread/resume response too large to read back.
//     The thread only grows, so every later resume overflows identically.
//     Detected via classifyResumeUnsafeTransport.
//
// MUL-2946: ReasonIterationLimit and ReasonAPIInvalidRequest are aliased
// to the canonical taskfailure values so the daemon and the in-flight
// classifier (used by every other failure path) share a single source
// of truth. agent_fallback_message and codex_semantic_inactivity are
// pre-existing operational reasons not in the canonical 21 — kept as
// string literals here until a follow-up PR migrates them or extends
// the taxonomy.
const (
	FailureReasonIterationLimit          = string(taskfailure.ReasonIterationLimit)
	FailureReasonAgentFallbackMsg        = "agent_fallback_message"
	FailureReasonAPIInvalidRequest       = string(taskfailure.ReasonAPIInvalidRequest)
	FailureReasonCodexSemanticInactivity = "codex_semantic_inactivity"
	FailureReasonCodexResumeOversized    = "codex_resume_oversized"
)

// poisonedOutputMaxLen caps how long an output can be and still be
// classified as a poisoned fallback. Real fallback messages are short,
// one-sentence affairs; a long output that happens to mention a marker
// is almost certainly a real conclusion (e.g. a code-review reply
// quoting these strings, like the one currently quoting them in
// MUL-1630). The cap intentionally errs on the side of NOT classifying
// — a missed poisoned task gets retried by user action, but a
// false-positive turns a successful task into a failure and a system
// comment.
const poisonedOutputMaxLen = 320

// poisonedMarkers maps a substring fingerprint of a known agent fallback
// terminal message to its failure_reason classifier. Match is case-
// insensitive and substring-based; the cap above prevents long outputs
// that quote a marker from being misclassified.
var poisonedMarkers = []struct {
	Substring string
	Reason    string
}{
	{"i reached the iteration limit", FailureReasonIterationLimit},
	{"put your final update inside the content string", FailureReasonAgentFallbackMsg},
}

// classifyPoisonedOutput reports whether output matches a known agent
// fallback terminal message and, if so, returns the failure_reason that
// should be persisted on the task row. Long outputs are never
// classified: a real fallback is the agent's only utterance for the
// turn, so anything beyond ~one paragraph is treated as a real result
// even if it contains a marker substring.
func classifyPoisonedOutput(output string) (string, bool) {
	// GH #6402: the provider's "this session's context window is full" notice,
	// reported as the run's successful answer. Same poisoning shape as the
	// markers below — resuming reproduces it forever, since a session already
	// over the limit cannot compact its way back under — but recognised by the
	// shared classifier so the server's /complete boundary applies the identical
	// rule to daemons too old to carry this check.
	//
	// Note what is NOT a condition here: how many tools the run executed. A
	// context window fills up mid-task far more often than on the first turn, so
	// gating on tools == 0 would miss the common case. The tool count governs
	// only whether a run may be REPLAYED, and nothing here replays anything —
	// agent_error.context_overflow is absent from retryableReasons, so the task
	// fails once, its session is retired, and the next trigger starts fresh.
	if taskfailure.ContextExhaustedCompletion(output) {
		return string(taskfailure.ReasonAgentContextOverflow), true
	}
	trimmed := strings.TrimSpace(output)
	if trimmed == "" || len(trimmed) > poisonedOutputMaxLen {
		return "", false
	}
	lowered := strings.ToLower(trimmed)
	for _, m := range poisonedMarkers {
		if strings.Contains(lowered, m.Substring) {
			return m.Reason, true
		}
	}
	return "", false
}

// classifyPoisonedError reports whether an agent error message indicates
// the LLM API itself rejected the request body — i.e. the conversation
// history contains content the API will not accept (oversized image,
// malformed base64, prompt-too-long, etc.). The conversation cannot be
// resumed: every retry replays the same body and reproduces the same 400.
// The classifier returns FailureReasonAPIInvalidRequest so GetLastTaskSession
// excludes the task from the (agent_id, issue_id) resume lookup, and the
// next task on the issue starts a fresh session instead of permanently
// inheriting the bad state.
//
// Match shape: the Claude Code SDK and similar backends surface upstream
// API failures verbatim, e.g.
//
//	API Error: 400 {"type":"error","error":{"type":"invalid_request_error","message":"Could not process image"},"request_id":"..."}
//
// Matching on both "400" and "invalid_request_error" keeps the classifier
// narrow: 429 rate-limits, 5xx overloads, and tool-shaped errors are
// transient and SHOULD resume on retry.
//
// That shape is Anthropic's, though, and it is not the only way a provider
// reports an unprocessable transcript. The final clause delegates to
// taskfailure.UnresumableHistory, which detects an empty message baked into
// the conversation by ANY backend — see its doc comment for why the defect
// has to be recognised by wording rather than by status code or provider.
func classifyPoisonedError(errMsg string) (string, bool) {
	if errMsg == "" {
		return "", false
	}
	lowered := strings.ToLower(errMsg)
	// Kiro/ACP replays images baked into a resumed conversation's history;
	// one exceeding the provider's max pixel dimensions is rejected on every
	// session/prompt and cannot be resumed away (GH #5975). The daemon's
	// in-task fresh-session retry recovers the CURRENT turn, but this marks
	// the conversation resume-unsafe so GetLastTaskSession excludes it and no
	// later task re-selects the poisoned session. The offending
	// messages[n].content[m] path and the pixel limit stay in the surfaced
	// error; base64 payloads are never logged. Requiring the image-content
	// marker alongside the dimension phrase keeps this narrow — an unrelated
	// error mentioning dimensions won't trip it.
	if strings.Contains(lowered, "image dimensions exceed max allowed size") &&
		strings.Contains(lowered, "image.source.base64.data") {
		return FailureReasonAPIInvalidRequest, true
	}
	// Both markers must be present: "400" alone is too generic (a tool
	// could surface a 400 from anywhere) and "invalid_request_error"
	// alone could in theory appear in non-poisoning contexts. The
	// combination is the canonical Anthropic error shape and indicates
	// the request body — i.e. the conversation history — is the problem.
	if strings.Contains(lowered, "invalid_request_error") && strings.Contains(lowered, "400") {
		return FailureReasonAPIInvalidRequest, true
	}
	// The same defect reported by a provider that words it differently.
	// The clause above only fires on the Anthropic shape, so an empty
	// message baked into the transcript by any other backend used to fall
	// through to taskfailure.Classify as agent_error.unknown — resume-safe
	// by omission, which permanently bricked the (agent, issue) pair
	// (GH #6066, GH #5760). taskfailure.UnresumableHistory recognises the
	// defect by what the provider says is wrong rather than by which
	// provider said it.
	if taskfailure.UnresumableHistory(errMsg) {
		return FailureReasonAPIInvalidRequest, true
	}
	return "", false
}

// classifyResumeUnsafeTransport reports whether a transport-level failure means
// the recorded session must not be resumed again.
//
// The one case today is a Codex thread/resume whose response overflowed our
// stdout line buffer. That session is not merely unlucky, it is finished: codex
// serializes the whole thread into that one response and its rollout file only
// ever grows, so every future resume of the same thread reproduces the same
// overflow. Leaving it as the (agent, issue) resume pointer is what turned this
// into a permanent stall (MUL-5722).
//
// This is the backstop for the in-turn recovery, not a replacement for it. The
// codex backend reports the same failure as Result.ResumeRejected, which lets
// the daemon retry the CURRENT turn on a fresh session (see
// shouldRetryWithFreshSession) — the outcome users actually want. But that
// retry is gated on tools == 0 and can itself fail, and either way the task
// lands here with the oversized thread still recorded. Classifying it keeps the
// NEXT task off that thread even when the current one could not be saved.
//
// Provider-specific on purpose, exactly like classifyResumeUnsafeTimeout below:
// no other backend replays its entire history through a single line, so for
// them an oversized line is a one-off event and the session is still good.
func classifyResumeUnsafeTransport(provider, errMsg string) (string, bool) {
	if strings.ToLower(strings.TrimSpace(provider)) != "codex" {
		return "", false
	}
	if agent.CodexResumeOverflowError(errMsg) {
		return FailureReasonCodexResumeOversized, true
	}
	return "", false
}

// classifyResumeUnsafeTimeout reports whether a timeout means the recorded
// session should not be resumed. Keep this intentionally provider-specific:
// ordinary daemon/backend timeouts are infrastructure-shaped and should keep
// the resume pointer so retries can continue the in-flight conversation.
func classifyResumeUnsafeTimeout(provider, errMsg string) (string, bool) {
	if strings.ToLower(strings.TrimSpace(provider)) != "codex" || errMsg == "" {
		return "", false
	}
	lowered := strings.ToLower(errMsg)
	if strings.Contains(lowered, strings.ToLower(agent.CodexSemanticInactivityMarker)) ||
		strings.Contains(lowered, strings.ToLower(agent.CodexFirstTurnNoProgressMarker)) {
		return FailureReasonCodexSemanticInactivity, true
	}
	return "", false
}
