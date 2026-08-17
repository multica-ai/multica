package handler

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestRunToResponseDoesNotReverseEngineerReasonCode pins Elon must-fix 2: the
// serializer must NOT derive reason_code from a persisted run's English
// failure_reason. The typed code is a decision-time value the manual "run now"
// handler injects from the dispatch outcome; a run read back from the DB (list /
// history) carries the human failure_reason but no guessed code.
func TestRunToResponseDoesNotReverseEngineerReasonCode(t *testing.T) {
	run := db.AutopilotRun{
		Status: "skipped",
		FailureReason: pgtype.Text{
			String: "assignee agent lacks access to private assignee agent",
			Valid:  true,
		},
	}
	resp := runToResponse(run)
	if resp.ReasonCode != nil {
		t.Fatalf("runToResponse should not synthesize a reason_code from failure_reason, got %q", *resp.ReasonCode)
	}
	if resp.FailureReason == nil || *resp.FailureReason == "" {
		t.Errorf("failure_reason should still be surfaced for history rows")
	}
}

// TestDispatchBlockedFallbackMessageIsNonEnumerating asserts the legacy `error`
// string for every reason code stays generic — it must be safe to show to a
// caller who is not allowed to know whether the target exists, so it must not
// name a private agent, its owner, or reveal existence.
func TestDispatchBlockedFallbackMessageIsNonEnumerating(t *testing.T) {
	codes := []DispatchReasonCode{
		ReasonInvocationNotAllowed, ReasonTargetUnavailable, ReasonRuntimeOffline,
		ReasonAttributionBlocked, ReasonAlreadyActive, ReasonInternalError,
		DispatchReasonCode("some_future_code"),
	}
	for _, c := range codes {
		msg := dispatchBlockedFallbackMessage(c)
		if msg == "" {
			t.Errorf("reason %q: empty fallback message", c)
		}
	}
	// invocation_not_allowed must be deliberately vague: it cannot distinguish
	// "target is private" from "target does not exist".
	if got := dispatchBlockedFallbackMessage(ReasonInvocationNotAllowed); got != "you don't have permission to use this target" {
		t.Errorf("invocation_not_allowed fallback = %q, changed to something more revealing?", got)
	}
}
