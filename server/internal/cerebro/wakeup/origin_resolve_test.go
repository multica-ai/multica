package wakeup

// Tests for resolveWakeupOrigin: a fired wakeup must carry the human principal
// it acts on behalf of, so the woken agent inherits provenance and may fan out
// to other agents. Resolution order: wakeup creator (human) → issue creator
// (member) → latest member comment. Skips cleanly when no test DB is reachable.

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

func newWakeupWithCreator(t *testing.T, ctx context.Context, svc *Service, issueID, createdBy pgtype.UUID) cerebrodb.CerebroAgentWakeup {
	t.Helper()
	row, err := svc.Cerebro.CreateCerebroAgentWakeup(ctx, cerebrodb.CreateCerebroAgentWakeupParams{
		WorkspaceID: wkWorkspaceID,
		AgentID:     wkAgentID,
		IssueID:     issueID,
		Prompt:      "check back later",
		TriggerType: TriggerTime,
		FireAt:      pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		CreatedByID: createdBy,
	})
	if err != nil {
		t.Fatalf("create wakeup: %v", err)
	}
	return row
}

// A human-created wakeup carries that human regardless of issue authorship.
func TestResolveWakeupOrigin_CreatorIsHuman(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	svc := wkService()
	row := newWakeupWithCreator(t, ctx, svc, wkIssueID, wkUserID)
	issue, err := svc.Queries.GetIssue(ctx, wkIssueID)
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	got := svc.resolveWakeupOrigin(ctx, row, issue)
	if !got.OriginalUserID.Valid || util.UUIDToString(got.OriginalUserID) != util.UUIDToString(wkUserID) {
		t.Fatalf("origin = %v, want creator %s", util.UUIDToString(got.OriginalUserID), util.UUIDToString(wkUserID))
	}
}

// No creator recorded, but the issue was created by a member → inherit it.
func TestResolveWakeupOrigin_FallsBackToIssueCreator(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	svc := wkService()
	row := newWakeupWithCreator(t, ctx, svc, wkIssueID, pgtype.UUID{})
	issue, err := svc.Queries.GetIssue(ctx, wkIssueID)
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	got := svc.resolveWakeupOrigin(ctx, row, issue)
	if !got.OriginalUserID.Valid || util.UUIDToString(got.OriginalUserID) != util.UUIDToString(wkUserID) {
		t.Fatalf("origin = %v, want issue creator %s", util.UUIDToString(got.OriginalUserID), util.UUIDToString(wkUserID))
	}
}

// No creator, agent-created issue, but a human commented → inherit the commenter.
func TestResolveWakeupOrigin_FallsBackToLatestHumanComment(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	svc := wkService()

	var agentIssue pgtype.UUID
	if err := wkPool.QueryRow(ctx,
		`INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		 VALUES ($1, $2, 'todo', 'none', 'agent', $3, 3) RETURNING id`,
		wkWorkspaceID, "Agent-created issue", wkAgentID).Scan(&agentIssue); err != nil {
		t.Fatalf("create agent issue: %v", err)
	}
	t.Cleanup(func() {
		wkPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, agentIssue)
		wkPool.Exec(context.Background(), `DELETE FROM cerebro_agent_wakeup WHERE issue_id = $1`, agentIssue)
		wkPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, agentIssue)
	})
	if _, err := wkPool.Exec(ctx,
		`INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content, type)
		 VALUES ($1, $2, 'member', $3, 'a human asks', 'comment')`,
		wkWorkspaceID, agentIssue, wkUserID); err != nil {
		t.Fatalf("insert member comment: %v", err)
	}

	row := newWakeupWithCreator(t, ctx, svc, agentIssue, pgtype.UUID{})
	issue, err := svc.Queries.GetIssue(ctx, agentIssue)
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	got := svc.resolveWakeupOrigin(ctx, row, issue)
	if !got.OriginalUserID.Valid || util.UUIDToString(got.OriginalUserID) != util.UUIDToString(wkUserID) {
		t.Fatalf("origin = %v, want latest human commenter %s", util.UUIDToString(got.OriginalUserID), util.UUIDToString(wkUserID))
	}
}

// No creator, agent-created issue, no human comment → no origin (caller then
// enqueues without provenance and the comment-time fallback applies).
func TestResolveWakeupOrigin_NoHumanReturnsEmpty(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	svc := wkService()

	var agentIssue pgtype.UUID
	if err := wkPool.QueryRow(ctx,
		`INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		 VALUES ($1, $2, 'todo', 'none', 'agent', $3, 4) RETURNING id`,
		wkWorkspaceID, "Agent-created silent issue", wkAgentID).Scan(&agentIssue); err != nil {
		t.Fatalf("create agent issue: %v", err)
	}
	t.Cleanup(func() {
		wkPool.Exec(context.Background(), `DELETE FROM cerebro_agent_wakeup WHERE issue_id = $1`, agentIssue)
		wkPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, agentIssue)
	})

	row := newWakeupWithCreator(t, ctx, svc, agentIssue, pgtype.UUID{})
	issue, err := svc.Queries.GetIssue(ctx, agentIssue)
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	got := svc.resolveWakeupOrigin(ctx, row, issue)
	if got.OriginalUserID.Valid {
		t.Fatalf("expected empty origin, got %s", util.UUIDToString(got.OriginalUserID))
	}
}
