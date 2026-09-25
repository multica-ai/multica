package taskfailure

import "strings"

// TerminalReasonPromptTooLong is the value Claude Code writes into the
// stream-json result event's `terminal_reason` when the turn ended because the
// request no longer fits the model's context window — either the conversation
// was already over the limit on resume, or auto-compaction ran and could not
// get it back under.
//
// It is a structured enum value, not prose: the CLI carries it alongside
// image_error / model_error / max_turns / completed and friends, on the
// SUCCESS-subtype result frame. It is the only field on that frame that states
// why the turn ended. `is_error` answers a different question — the CLI sets it
// from whether the LAST message it rendered was an API-error message — and the
// two are computed independently, so a run's real terminal condition must be
// read from this field rather than inferred from that flag.
//
// Captured from Claude Code 2.1.220 (the version GH #6402 reports) by resuming
// a saturated transcript against an endpoint returning the provider's 400
// prompt-too-long shape; the frames are fixtured verbatim in
// pkg/agent/testdata/claude-code-2.1.220-context-exhausted-resume.jsonl. Auto-
// compaction runs, reports compact_result=failed / compact_error=exhausted, and
// the turn ends:
//
//	{"type":"result","subtype":"success","is_error":true,
//	 "terminal_reason":"prompt_too_long","api_error_status":400,
//	 "result":"Prompt is too long"}
//
// 2.1.221 produces the same shape. Two things follow, and the second is the
// reason this constant exists:
//
//   - The field is present on the reported version, so keying on it reaches the
//     hosts that hit the bug rather than only future ones.
//   - On this frame is_error and terminal_reason BOTH fire. Whichever the
//     runtime reads first decides the failure's label, and only terminal_reason
//     is guaranteed to name the condition — the `result` prose is empty in some
//     shapes and rewords between releases.
//
// Note what the capture does not show: is_error false. The exact escape that
// produced a `completed` task on the reporter's host is not reproduced here, so
// the text-side predicate below stays a bounded backstop rather than the
// load-bearing part of the fix.
const TerminalReasonPromptTooLong = "prompt_too_long"

// contextExhaustedOutputMaxLen caps how long a reported-successful output can
// be and still be re-read as a context-exhaustion notice. Every wording below
// is a terse one-liner the CLI emits INSTEAD of an answer; an agent that
// actually did the work and then discussed context limits writes far more than
// this. Same rationale and same value as poisonedOutputMaxLen in
// internal/daemon — a miss costs the pre-fix behaviour, a false positive turns
// a real result into a failure and retires a healthy session.
const contextExhaustedOutputMaxLen = 320

// ContextExhaustedCompletion reports whether an output the agent runtime
// reported as a SUCCESSFUL final answer is really the provider telling us the
// session's context window is full.
//
// This is the text-side counterpart to TerminalReasonPromptTooLong, and it
// exists for the two places the structured field is not available:
//
//   - Backends whose CLI has no equivalent structured field.
//   - The server's /complete boundary, where the caller may be an installed
//     daemon too old to carry the structured check. Daemons upgrade on their
//     own cadence, so a daemon-only fix reaches nobody until every host
//     updates — and one un-upgraded host means a permanently stuck (agent,
//     issue) pair, not just a mislabelled row (same argument as
//     NormalizeDaemonReason, MUL-5370).
//
// EVERY clause is composite, and none is a bare natural-language sentence. The
// GitHub report paraphrased the CLI as "context too long, please run /compact",
// and neither half of that is safe alone: "/compact" appears in ordinary Claude
// Code advice and "context too long" is a phrase an agent can legitimately
// write about its own run. Each clause below pins a full, distinctive wording
// taken from the Claude Code binary itself rather than from the report:
//
//	"Prompt is too long · … A single-exchange conversation cannot be compacted; …"   (3 variants, all carrying "cannot be compacted")
//	"Conversation too long. Press esc twice to go up a few messages and try again."
//	"Compaction failed · conversation could not be reduced below the context limit"
//
// The CLI's bare "Prompt is too long" is deliberately NOT matched here, even
// though it is a real terminal result — matching it would mean declaring a
// task failed because its whole answer was one common English sentence, which
// an agent asked "is my prompt too long?" can legitimately produce. And this
// predicate only ever sees output a caller believed was a SUCCESS, so a match
// costs a real task and a healthy session.
//
// Leaving it out costs nothing on the evidence we have: in the 2.1.220 and
// 2.1.221 frames captured for GH #6402 the bare form arrives with is_error set,
// which routes it through /fail, where Classify's rule 1 has always mapped
// "prompt is too long" to context_overflow. That is a statement about those
// captures, not about every build — if a frame ever carries the bare sentence
// as a clean success, it will be missed here, and that is the deliberate
// direction to err in.
func ContextExhaustedCompletion(output string) bool {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" || len(trimmed) > contextExhaustedOutputMaxLen {
		return false
	}
	lowered := strings.ToLower(trimmed)
	switch {
	case strings.Contains(lowered, "prompt is too long") &&
		strings.Contains(lowered, "cannot be compacted"):
		return true
	case strings.Contains(lowered, "conversation too long") &&
		strings.Contains(lowered, "press esc twice"):
		return true
	case strings.Contains(lowered, "compaction failed") &&
		strings.Contains(lowered, "reduced below the context limit"):
		return true
	}
	return false
}
