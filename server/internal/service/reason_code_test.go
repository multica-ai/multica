package service

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/dispatch"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestDispatchFailReasonCode is the regression for Elon must-fix 2, case 1: a
// dispatch that fails fail-closed on attribution must be classified
// attribution_blocked, not internal_error — decided by a TYPED errors.Is check,
// not by substring-matching an English message.
func TestDispatchFailReasonCode(t *testing.T) {
	if got := dispatchFailReasonCode(ErrAttributionFailClosed); got != dispatch.ReasonAttributionBlocked {
		t.Errorf("bare fail-closed: got %q, want attribution_blocked", got)
	}
	// The real dispatch path wraps the sentinel (create_issue enqueue → %w);
	// errors.Is must still see through the wrap.
	wrapped := fmt.Errorf("dispatch create_issue: enqueue task for issue: %w", ErrAttributionFailClosed)
	if got := dispatchFailReasonCode(wrapped); got != dispatch.ReasonAttributionBlocked {
		t.Errorf("wrapped fail-closed: got %q, want attribution_blocked", got)
	}
	if got := dispatchFailReasonCode(errors.New("some other failure")); got != dispatch.ReasonInternalError {
		t.Errorf("generic error: got %q, want internal_error", got)
	}
}

// TestAgentReadinessReasonCode is the regression for Elon must-fix 2, case 2: a
// runtime-availability failure (no runtime bound, or a bound-but-offline
// runtime) must map to runtime_offline, and an archived agent to
// target_unavailable — decided from the agent's own state, not the reason text.
func TestAgentReadinessReasonCode(t *testing.T) {
	validRuntime := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	archivedAt := pgtype.Timestamptz{Valid: true}

	// No runtime bound (the "agent has no runtime bound" case that previously
	// fell through the substring classifier to generic).
	if got := agentReadinessReasonCode(db.Agent{}); got != dispatch.ReasonRuntimeOffline {
		t.Errorf("no runtime bound: got %q, want runtime_offline", got)
	}
	// Bound runtime that is offline at dispatch time.
	if got := agentReadinessReasonCode(db.Agent{RuntimeID: validRuntime}); got != dispatch.ReasonRuntimeOffline {
		t.Errorf("bound offline runtime: got %q, want runtime_offline", got)
	}
	// Archived agent cannot run at all.
	if got := agentReadinessReasonCode(db.Agent{ArchivedAt: archivedAt, RuntimeID: validRuntime}); got != dispatch.ReasonTargetUnavailable {
		t.Errorf("archived agent: got %q, want target_unavailable", got)
	}
}
