package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/issueworkflow"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

func TestWorkflowStatusSnapshotName(t *testing.T) {
	for _, tc := range []struct {
		name, key, want string
	}{
		{name: "Todo", key: "todo", want: ""},
		{name: "Ready for Agent", key: "todo", want: "Ready for Agent"},
		{name: "Customer Review", key: "customer_review", want: "Customer Review"},
	} {
		status := db.IssueWorkflowStatus{
			Name:            tc.name,
			LegacyStatusKey: pgtype.Text{String: tc.key, Valid: true},
		}
		if got := workflowStatusSnapshotName(status); got != tc.want {
			t.Errorf("workflowStatusSnapshotName(%q, %q) = %q, want %q", tc.key, tc.name, got, tc.want)
		}
	}
}

func TestTransitionIssueRecordsImmutableHistoryAndRejectsStaleAgent(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("integration test requires Postgres at DATABASE_URL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to Postgres: %v", err)
	}
	defer pool.Close()
	q := db.New(pool)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var workspaceID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, issue_prefix)
		VALUES ('Workflow transition test', $1, 'LCT')
		RETURNING id
	`, "workflow-transition-"+suffix).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM automation_execution WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue_transition WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue_workflow_status WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue_workflow WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue_status WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})
	if err := q.SeedIssueStatusEntries(ctx, workspaceID); err != nil {
		t.Fatalf("seed status catalog: %v", err)
	}
	if _, err := q.CreateIssueStatusEntry(ctx, db.CreateIssueStatusEntryParams{
		WorkspaceID: workspaceID,
		Key:         "human_review",
		Name:        "Human Review",
		Description: "Custom review gate",
		Category:    "in_review",
		Color:       "#22c55e",
	}); err != nil {
		t.Fatalf("create custom review status: %v", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin workflow bootstrap: %v", err)
	}
	if _, err := issueworkflow.EnsureDefault(ctx, q.WithTx(tx), workspaceID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("bootstrap workflow: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit workflow bootstrap: %v", err)
	}

	// Simulate an issue inserted by an older binary during a rolling deploy:
	// the legacy status/revision exist, but workflow pins and transition do
	// not. The first new transition repairs the binding and still resolves its
	// from-node instead of losing that audit edge.
	rollingCreator := dbid.NewV7()
	rollingCreated, err := NewIssueService(q, pool, nil, nil, nil).Create(ctx, IssueCreateParams{
		WorkspaceID: workspaceID,
		Title:       "rolling deploy repair",
		Status:      "todo",
		Priority:    "none",
		CreatorType: "member",
		CreatorID:   rollingCreator,
	}, IssueCreateOpts{})
	if err != nil {
		t.Fatalf("create rolling fixture issue: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM issue_transition WHERE issue_id = $1`, rollingCreated.Issue.ID); err != nil {
		t.Fatalf("strip rolling fixture transition: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE issue
		SET workflow_id = NULL, workflow_status_id = NULL, last_transition_id = NULL
		WHERE id = $1
	`, rollingCreated.Issue.ID); err != nil {
		t.Fatalf("strip rolling fixture workflow projection: %v", err)
	}
	rollingResult, err := TransitionIssue(ctx, q, pool, IssueTransitionParams{
		IssueID:     rollingCreated.Issue.ID,
		WorkspaceID: workspaceID,
		Status:      "in_progress",
		Actor:       issueworkflow.TransitionActor{Type: "system"},
		Cause:       "rolling_deploy_repair",
	})
	if err != nil {
		t.Fatalf("repair rolling fixture on transition: %v", err)
	}
	if !rollingResult.Transition.FromStatusID.Valid {
		t.Fatal("rolling repair transition lost its legacy from-status node")
	}
	rollingFrom, err := q.GetIssueWorkflowStatusByID(ctx, db.GetIssueWorkflowStatusByIDParams{
		WorkspaceID: workspaceID,
		WorkflowID:  rollingResult.Transition.WorkflowID,
		ID:          rollingResult.Transition.FromStatusID,
	})
	if err != nil || !rollingFrom.LegacyStatusKey.Valid || rollingFrom.LegacyStatusKey.String != "todo" {
		t.Fatalf("rolling repair from-status = %#v, err=%v", rollingFrom, err)
	}

	creatorID := dbid.NewV7()
	created, err := NewIssueService(q, pool, nil, nil, nil).Create(ctx, IssueCreateParams{
		WorkspaceID: workspaceID,
		Title:       "protect human transition",
		Status:      "todo",
		Priority:    "none",
		CreatorType: "member",
		CreatorID:   creatorID,
	}, IssueCreateOpts{})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if !created.Issue.WorkflowID.Valid || !created.Issue.WorkflowStatusID.Valid || !created.Issue.LastTransitionID.Valid {
		t.Fatalf("created issue missing workflow pins: %#v", created.Issue)
	}

	human := IssueTransitionParams{
		IssueID:              created.Issue.ID,
		WorkspaceID:          workspaceID,
		Status:               "human_review",
		Actor:                issueworkflow.TransitionActor{Type: "member", ID: creatorID},
		Cause:                "human_review",
		ExpectedRevision:     pgtype.Int8{Int64: created.Issue.Revision, Valid: true},
		ExpectedTransitionID: created.Issue.LastTransitionID,
	}
	humanResult, err := TransitionIssue(ctx, q, pool, human)
	if err != nil {
		t.Fatalf("human transition: %v", err)
	}
	if !humanResult.Changed || humanResult.Issue.Status != "human_review" {
		t.Fatalf("human transition result = %#v", humanResult)
	}
	if humanResult.Transition.ActorType != "member" || humanResult.Transition.Cause != "human_review" ||
		humanResult.Transition.IssueRevisionBefore != created.Issue.Revision ||
		humanResult.Transition.IssueRevisionAfter != humanResult.Issue.Revision {
		t.Fatalf("transition audit fields = %#v", humanResult.Transition)
	}

	staleAgent := human
	staleAgent.Status = "in_progress"
	staleAgent.Actor = issueworkflow.TransitionActor{Type: "agent", ID: dbid.NewV7()}
	staleAgent.Cause = "agent_progress"
	if _, err := TransitionIssue(ctx, q, pool, staleAgent); !errors.Is(err, ErrIssueTransitionConflict) {
		t.Fatalf("stale agent transition error = %v, want conflict", err)
	}
	current, err := q.GetIssue(ctx, created.Issue.ID)
	if err != nil {
		t.Fatalf("reload issue: %v", err)
	}
	if current.Status != "human_review" || current.LastTransitionID != humanResult.Transition.ID {
		t.Fatalf("stale write changed issue: status=%q transition=%v", current.Status, current.LastTransitionID)
	}

	// Move off the review node, then rename and archive it. The node ID remains
	// stable so the already-committed historical transition still resolves,
	// while the workflow revision records the catalog edit.
	done := human
	done.Status = "done"
	done.Cause = "human_completed"
	done.ExpectedRevision = pgtype.Int8{Int64: humanResult.Issue.Revision, Valid: true}
	done.ExpectedTransitionID = humanResult.Issue.LastTransitionID
	doneResult, err := TransitionIssue(ctx, q, pool, done)
	if err != nil {
		t.Fatalf("complete issue: %v", err)
	}
	reviewNodeBefore, err := q.GetIssueWorkflowStatusByID(ctx, db.GetIssueWorkflowStatusByIDParams{
		WorkspaceID: workspaceID,
		WorkflowID:  humanResult.Transition.WorkflowID,
		ID:          humanResult.Transition.ToStatusID,
	})
	if err != nil {
		t.Fatalf("load historical review node: %v", err)
	}
	workflowBefore, err := q.GetIssueWorkflowByID(ctx, db.GetIssueWorkflowByIDParams{
		ID: reviewNodeBefore.WorkflowID, WorkspaceID: workspaceID,
	})
	if err != nil {
		t.Fatalf("load workflow before catalog edit: %v", err)
	}
	catalogTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin catalog edit: %v", err)
	}
	if _, err := catalogTx.Exec(ctx, `
		UPDATE issue_status
		SET name = 'Review archived', archived_at = now(), updated_at = now()
		WHERE workspace_id = $1 AND key = 'human_review'
	`, workspaceID); err != nil {
		_ = catalogTx.Rollback(ctx)
		t.Fatalf("update legacy catalog: %v", err)
	}
	if err := issueworkflow.SyncDefault(ctx, q.WithTx(catalogTx), workspaceID); err != nil {
		_ = catalogTx.Rollback(ctx)
		t.Fatalf("sync workflow catalog: %v", err)
	}
	if err := catalogTx.Commit(ctx); err != nil {
		t.Fatalf("commit catalog edit: %v", err)
	}
	reviewNodeAfter, err := q.GetIssueWorkflowStatusByID(ctx, db.GetIssueWorkflowStatusByIDParams{
		WorkspaceID: workspaceID,
		WorkflowID:  humanResult.Transition.WorkflowID,
		ID:          humanResult.Transition.ToStatusID,
	})
	if err != nil {
		t.Fatalf("reload historical review node: %v", err)
	}
	if reviewNodeAfter.ID != reviewNodeBefore.ID || reviewNodeAfter.Name != "Review archived" || !reviewNodeAfter.ArchivedAt.Valid {
		t.Fatalf("catalog projection lost stable node identity: before=%#v after=%#v", reviewNodeBefore, reviewNodeAfter)
	}
	workflowAfter, err := q.GetIssueWorkflowByID(ctx, db.GetIssueWorkflowByIDParams{
		ID: reviewNodeAfter.WorkflowID, WorkspaceID: workspaceID,
	})
	if err != nil {
		t.Fatalf("load workflow after catalog edit: %v", err)
	}
	if workflowAfter.Revision != workflowBefore.Revision+1 {
		t.Fatalf("workflow revision = %d, want %d", workflowAfter.Revision, workflowBefore.Revision+1)
	}
	historical, err := q.GetIssueTransition(ctx, db.GetIssueTransitionParams{
		ID: humanResult.Transition.ID, WorkspaceID: workspaceID,
	})
	if err != nil {
		t.Fatalf("reload historical transition: %v", err)
	}
	if historical.ToStatusID != reviewNodeBefore.ID || historical.WorkflowRevision != workflowBefore.Revision {
		t.Fatalf("historical transition drifted after catalog edit: %#v", historical)
	}
	current = doneResult.Issue
	transitions, err := q.ListIssueTransitions(ctx, db.ListIssueTransitionsParams{IssueID: current.ID, WorkspaceID: workspaceID})
	if err != nil {
		t.Fatalf("list transitions: %v", err)
	}
	if len(transitions) != 3 {
		t.Fatalf("transition count = %d, want creation + review + completion", len(transitions))
	}
	consistency, err := q.GetIssueWorkflowConsistency(ctx, workspaceID)
	if err != nil {
		t.Fatalf("audit consistency: %v", err)
	}
	if consistency.WorkspacesWithoutDefault != 0 || consistency.IssuesWithoutBinding != 0 ||
		consistency.IssuesWithStatusMismatch != 0 || consistency.IssuesWithTransitionMismatch != 0 {
		t.Fatalf("consistency report = %#v", consistency)
	}
}
