package wakeup

// FIR-3098: the wakeup loop guard caps self-wakeups without objective progress.
// These tests use wkOtherIssue so the initial progress window is empty, then
// add each accepted progress signal to prove the reset.
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
	var commentaryID pgtype.UUID
	if err := wkPool.QueryRow(ctx,
		`INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content, type)
		 VALUES ($1, $2, 'agent', $3, 'I am checking again now', 'comment') RETURNING id`,
		wkWorkspaceID, wkOtherIssue, wkAgentID).Scan(&commentaryID); err != nil {
		t.Fatalf("insert empty-loop commentary: %v", err)
	}
	t.Cleanup(func() {
		_, _ = wkPool.Exec(context.Background(), `DELETE FROM comment WHERE id = $1`, commentaryID)
	})

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

func TestLoopGuardResetsAfterIssueStatusProgress(t *testing.T) {
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

	var commentID pgtype.UUID
	if err := wkPool.QueryRow(ctx,
		`INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content, type)
		 VALUES ($1, $2, 'agent', $3, 'status changed from todo to in_progress', 'status_change') RETURNING id`,
		wkWorkspaceID, wkOtherIssue, wkAgentID).Scan(&commentID); err != nil {
		t.Fatalf("insert status progress: %v", err)
	}
	t.Cleanup(func() {
		_, _ = wkPool.Exec(context.Background(), `DELETE FROM comment WHERE id = $1`, commentID)
	})

	if _, err := svc.Create(ctx, wkWorkspaceID, CreateRequest{
		AgentID:     wkAgentID,
		IssueID:     wkOtherIssue,
		Prompt:      "allowed after status progress",
		TriggerType: TriggerTime,
		FireAt:      pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}); err != nil {
		t.Fatalf("expected wakeup allowed after issue status progress, got: %v", err)
	}
}

func TestLoopGuardDoesNotResetAfterLinkedPullRequestUpdate(t *testing.T) {
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

	var pullRequestID pgtype.UUID
	if err := wkPool.QueryRow(ctx,
		`INSERT INTO github_pull_request
		   (workspace_id, installation_id, repo_owner, repo_name, pr_number, title, state,
		    html_url, pr_created_at, pr_updated_at, head_sha)
		 VALUES ($1, 1, 'firtal-group', 'wakeup-progress-test', 3098, 'FIR-3098 progress',
		         'open', 'https://example.test/pr/3098', now(), now(), 'new-head')
		 RETURNING id`, wkWorkspaceID).Scan(&pullRequestID); err != nil {
		t.Fatalf("insert pull request progress: %v", err)
	}
	if _, err := wkPool.Exec(ctx,
		`INSERT INTO issue_pull_request (issue_id, pull_request_id) VALUES ($1, $2)`,
		wkOtherIssue, pullRequestID); err != nil {
		t.Fatalf("link pull request progress: %v", err)
	}
	t.Cleanup(func() {
		_, _ = wkPool.Exec(context.Background(), `DELETE FROM github_pull_request WHERE id = $1`, pullRequestID)
	})

	_, err := svc.Create(ctx, wkWorkspaceID, CreateRequest{
		AgentID:     wkAgentID,
		IssueID:     wkOtherIssue,
		Prompt:      "blocked despite pull request churn",
		TriggerType: TriggerTime,
		FireAt:      pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	})
	if err == nil {
		t.Fatal("expected pull-request churn not to reset the empty-loop guard")
	}
	if !strings.Contains(err.Error(), "loop guard") {
		t.Fatalf("expected a loop-guard error, got: %v", err)
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
