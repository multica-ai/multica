package wakeup

// FIR-2679 Spor 1a: the wakeup loop-guard caps how many self-wakeups an agent
// may chain on one issue since the last human (member) comment. These tests use
// wkOtherIssue (no member comment in the fixture) so the "since last human"
// window is the whole history, then add a member comment to prove the reset.
//
// Skips cleanly when no test DB is reachable, same pattern as service_db_test.go.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// seedDispatchedWakeups inserts n already-fired (dispatched) self-wakeups for the
// fixture agent on the given issue, created now. Dispatched rows do not trip the
// min-interval "recent pending" check, so Create reaches the loop guard.
func seedDispatchedWakeups(t *testing.T, ctx context.Context, issueID pgtype.UUID, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := wkPool.Exec(ctx,
			`INSERT INTO cerebro_agent_wakeup
			   (workspace_id, agent_id, issue_id, prompt, trigger_type, fire_at, state, dispatched_at)
			 VALUES ($1, $2, $3, 'chained', 'time', now() + interval '1 hour', 'dispatched', now())`,
			wkWorkspaceID, wkAgentID, issueID); err != nil {
			t.Fatalf("seed dispatched wakeup: %v", err)
		}
	}
}

func setLoopCap(t *testing.T, ctx context.Context, svc *Service, loops int32) {
	t.Helper()
	if err := svc.Cerebro.UpsertCerebroWorkspaceWakeupLimits(ctx, cerebrodb.UpsertCerebroWorkspaceWakeupLimitsParams{
		WorkspaceID:               wkWorkspaceID,
		WakeupMaxSelfPerIssue:     defaultMaxSelfWakeupsPerIssue,
		WakeupMinIntervalMinutes:  defaultMinWakeupIntervalMin,
		WakeupMaxConsecutiveLoops: loops,
		UpdatedBy:                 wkUserID,
	}); err != nil {
		t.Fatalf("set loop cap: %v", err)
	}
	t.Cleanup(func() {
		_, _ = wkPool.Exec(context.Background(), `DELETE FROM cerebro_workspace_settings WHERE workspace_id = $1`, wkWorkspaceID)
		_, _ = wkPool.Exec(context.Background(), `DELETE FROM cerebro_agent_wakeup WHERE agent_id = $1 AND issue_id = $2`, wkAgentID, wkOtherIssue)
	})
}

func TestLoopGuardBlocksAfterCap(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	svc := wkService()
	if _, err := wkPool.Exec(ctx, `DELETE FROM cerebro_agent_wakeup WHERE agent_id = $1 AND issue_id = $2`, wkAgentID, wkOtherIssue); err != nil {
		t.Fatalf("clear wakeups: %v", err)
	}
	setLoopCap(t, ctx, svc, 2)
	seedDispatchedWakeups(t, ctx, wkOtherIssue, 2)

	_, err := svc.Create(ctx, wkWorkspaceID, CreateRequest{
		AgentID:     wkAgentID,
		IssueID:     wkOtherIssue,
		Prompt:      "one more loop",
		TriggerType: TriggerTime,
		FireAt:      pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	})
	if err == nil {
		t.Fatal("expected loop guard to block the 3rd consecutive wakeup, got nil")
	}
	if !strings.Contains(err.Error(), "loop guard") {
		t.Fatalf("expected a loop-guard error, got: %v", err)
	}
}

func TestLoopGuardResetsAfterMemberComment(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	svc := wkService()
	if _, err := wkPool.Exec(ctx, `DELETE FROM cerebro_agent_wakeup WHERE agent_id = $1 AND issue_id = $2`, wkAgentID, wkOtherIssue); err != nil {
		t.Fatalf("clear wakeups: %v", err)
	}
	setLoopCap(t, ctx, svc, 2)
	seedDispatchedWakeups(t, ctx, wkOtherIssue, 2)

	// A human replies -> the streak resets, so the next wakeup is allowed again.
	var commentID pgtype.UUID
	if err := wkPool.QueryRow(ctx,
		`INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content, type)
		 VALUES ($1, $2, 'member', $3, 'human breaking the loop', 'comment') RETURNING id`,
		wkWorkspaceID, wkOtherIssue, wkUserID).Scan(&commentID); err != nil {
		t.Fatalf("insert member comment: %v", err)
	}
	t.Cleanup(func() {
		_, _ = wkPool.Exec(context.Background(), `DELETE FROM comment WHERE id = $1`, commentID)
	})

	if _, err := svc.Create(ctx, wkWorkspaceID, CreateRequest{
		AgentID:     wkAgentID,
		IssueID:     wkOtherIssue,
		Prompt:      "allowed after human reply",
		TriggerType: TriggerTime,
		FireAt:      pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}); err != nil {
		t.Fatalf("expected wakeup allowed after member comment reset, got: %v", err)
	}
}

func TestLoopGuardDisabledWhenZero(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	svc := wkService()
	if _, err := wkPool.Exec(ctx, `DELETE FROM cerebro_agent_wakeup WHERE agent_id = $1 AND issue_id = $2`, wkAgentID, wkOtherIssue); err != nil {
		t.Fatalf("clear wakeups: %v", err)
	}
	setLoopCap(t, ctx, svc, 0)
	seedDispatchedWakeups(t, ctx, wkOtherIssue, 3)

	if _, err := svc.Create(ctx, wkWorkspaceID, CreateRequest{
		AgentID:     wkAgentID,
		IssueID:     wkOtherIssue,
		Prompt:      "guard off",
		TriggerType: TriggerTime,
		FireAt:      pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}); err != nil {
		t.Fatalf("expected no guard when cap is 0, got: %v", err)
	}
}
