package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestPendingRecoveryConvertedTerminalHistoryDoesNotStarveAnotherWorkspace(t *testing.T) {
	ctx := context.Background()
	history, historyService := seedDelegatedFailureFixture(t)
	historyRows := testutil.New(history.pool, history.workspaceID, history.userID)
	historyRows.Insert(t, "issue_status", testutil.Cols{
		"workspace_id": history.workspaceID, "key": "legacy_closed", "name": "Closed", "category": "closed", "color": "#123456",
	})
	signal := func(f *delegatedFailureFixture, svc *TaskService) pgtype.UUID {
		t.Helper()
		failed := f.insertWorkerTask(t, "failed", "comment", 1, 2)
		rows := testutil.New(f.pool, f.workspaceID, f.userID)
		rows.Exec(t, `UPDATE agent_task_queue SET failure_reason='agent_error.process_failure', completed_at=now() WHERE id=$1`, failed)
		target, created, err := svc.ensureDelegatedFailureRecoveryComment(ctx, failed)
		if err != nil || !created || target == nil {
			t.Fatalf("create signal: %v, %v", created, err)
		}
		return target.comment.ID
	}
	for range 100 {
		signal(history, historyService)
	}
	historyRows.Exec(t, `UPDATE issue SET status='legacy_closed' WHERE id=$1`, history.issueID)
	live, svc := seedDelegatedFailureFixture(t)
	target := signal(live, svc)
	pending, err := svc.Queries.ListPendingDelegatedFailureRecoveries(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range pending {
		if row.ID == target {
			found = true
		}
	}
	if !found {
		t.Fatal("100 old terminal signals starved another workspace's live recovery")
	}
	if len(pending) != 1 {
		t.Fatalf("scan returned %d signals, want only the live signal", len(pending))
	}
	result, err := svc.RecoverPendingDelegatedFailures(ctx, 100)
	if err != nil || result.Replayed != 1 {
		t.Fatalf("live recovery = %+v, %v; want one replay", result, err)
	}
}
