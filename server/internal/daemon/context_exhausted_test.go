package daemon

import (
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// TestContextExhaustionNeverReplaysTheTask pins the boundary between the two
// questions GH #6402's fix has to keep apart.
//
// "Did this run produce an answer?" is decided by the output alone —
// classifyPoisonedOutput takes no tool count, and it must not grow one: a
// context window fills up mid-task far more often than on the first turn, so a
// tools == 0 gate would miss the common case and leave exactly the silent
// stall the issue reports.
//
// "May we re-run it?" is where the tool count belongs, and the answer here is
// always no. A saturated session is not a rejected resume, so there is nothing
// a fresh-session retry of THIS task would cure that letting the NEXT task
// start clean does not — while replaying a run that already used tools
// duplicates its side effects.
func TestContextExhaustionNeverReplaysTheTask(t *testing.T) {
	failed := agent.Result{
		Status:    "failed",
		SessionID: "sess-full",
		Error: "claude ended the turn with terminal_reason=" + taskfailure.TerminalReasonPromptTooLong +
			": the session's context window is exhausted and compaction could not recover it (Prompt is too long)",
	}

	for _, tools := range []int32{0, 1, 17} {
		if shouldRetryWithFreshSession(failed, "sess-full", tools, "claude") {
			t.Errorf("tools=%d: a full context window must not trigger an in-task replay", tools)
		}
	}
}

// TestContextExhaustionClassifiesRegardlessOfToolUse is the other half: the
// reclassification itself is reachable for a run that got real work done before
// the window filled, because it is decided by the output and nothing else.
func TestContextExhaustionClassifiesRegardlessOfToolUse(t *testing.T) {
	// The daemon's completed branch calls this with the run's output and no
	// other state; a mid-task overflow reaches it identically to a first-turn
	// one.
	reason, ok := classifyPoisonedOutput(
		"Prompt is too long · this conversation is a single exchange and cannot be compacted — " +
			"the request size comes mostly from system prompt, tool definitions, or attachments.")
	if !ok {
		t.Fatal("expected the context-exhaustion notice to be classified as poisoned output")
	}
	if reason != string(taskfailure.ReasonAgentContextOverflow) {
		t.Fatalf("reason = %q, want %q", reason, taskfailure.ReasonAgentContextOverflow)
	}
	// The reason must be one the platform refuses to auto-retry: the run may
	// have executed tools, and re-running it has no idempotency key.
	if retryableFromDaemonPerspective(reason) {
		t.Fatalf("%q must not be auto-retryable", reason)
	}
}

// retryableFromDaemonPerspective mirrors the server's retryableReasons
// allowlist for the reasons the daemon can emit. Kept local so this package's
// test does not import internal/service, and deliberately exhaustive: if a
// future change adds context_overflow to the server allowlist, this test still
// documents why it should not be there.
func retryableFromDaemonPerspective(reason string) bool {
	switch reason {
	case "runtime_offline", "runtime_recovery", "timeout", "codex_semantic_inactivity",
		string(taskfailure.ReasonAgentProviderNetwork),
		string(taskfailure.ReasonSkillBundleUnavailable):
		return true
	default:
		return false
	}
}
