package workflows

// FIR-2283 followup point (b) regression guard: the deterministic session
// phase stamper. Before this, a plan-phase run_skill dispatch relied on the
// agent calling rename_session to badge its session "Plan" — when it forgot,
// the session carried no phase (0 rows on staging). StampOnComplete anchors the
// run's thread root server-side and badges it, so the badge no longer depends
// on the agent. These DB tests drive the real pool path.

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// newAgentUUID mints a fresh UUID for a synthetic agent author. comment.author_id
// is polymorphic with no FK (system rows even use a zero UUID), so a plain
// generated UUID is a valid agent author for these tests.
func newAgentUUID(t *testing.T, pool *pgxpool.Pool) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := pool.QueryRow(context.Background(), `SELECT gen_random_uuid()`).Scan(&id); err != nil {
		t.Fatalf("mint agent uuid: %v", err)
	}
	return id
}

// insertComment inserts one comment and returns its id. parent is the zero UUID
// for a top-level (session-root) comment.
func insertComment(t *testing.T, pool *pgxpool.Pool, issueID, workspaceID, authorID pgtype.UUID, authorType, content string, parent pgtype.UUID) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, parent_id)
		VALUES ($1, $2, $3, $4, $5, 'comment', $6)
		RETURNING id`,
		issueID, workspaceID, authorType, authorID, content, parent).Scan(&id); err != nil {
		t.Fatalf("insert comment: %v", err)
	}
	return id
}

// planTask builds an in-memory completed run_skill task carrying loop_phase.
func planTask(agentID, issueID pgtype.UUID, phase, sessionName string) db.AgentTaskQueue {
	ctxMap := map[string]any{
		"type":                     "quick_create",
		"loop_phase":               phase,
		"workflow_target_issue_id": uuidString(issueID),
	}
	if sessionName != "" {
		ctxMap["loop_session_name"] = sessionName
	}
	return db.AgentTaskQueue{
		AgentID:   agentID,
		StartedAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true},
		Context:   mustJSON(ctxMap),
	}
}

func TestStampOnComplete_BadgesPlanSessionRoot(t *testing.T) {
	pool := openWorkflowIntegrationPool(t)
	ctx := context.Background()
	f := setupWorkflowIntegrationFixture(t, pool)
	issueID := insertWorkflowIntegrationIssue(t, pool, f, "Plan me", "todo", 1, pgtype.UUID{})

	agentID := newAgentUUID(t, pool)
	other := newAgentUUID(t, pool)

	// A member's top-level comment (wrong author) and an earlier agent reply must
	// both be ignored; the agent's top-level plan comment is the session root.
	insertComment(t, pool, issueID, f.workspaceID, f.userID, "member", "Please plan this.", pgtype.UUID{})
	insertComment(t, pool, issueID, f.workspaceID, other, "agent", "Unrelated agent thread.", pgtype.UUID{})
	rootID := insertComment(t, pool, issueID, f.workspaceID, agentID, "agent", "Here is the plan.", pgtype.UUID{})
	insertComment(t, pool, issueID, f.workspaceID, agentID, "agent", "A reply inside the thread.", rootID)

	NewSessionPhaseStamper(pool).StampOnComplete(ctx, planTask(agentID, issueID, "plan", ""))

	var gotRoot pgtype.UUID
	var gotPhase, gotName, gotMode string
	if err := pool.QueryRow(ctx, `
		SELECT root_comment_id, phase, name, mode FROM cerebro_session WHERE issue_id = $1`,
		issueID).Scan(&gotRoot, &gotPhase, &gotName, &gotMode); err != nil {
		t.Fatalf("load stamped session: %v", err)
	}
	if uuidString(gotRoot) != uuidString(rootID) {
		t.Fatalf("stamped root = %s, want the agent's top-level plan comment %s", uuidString(gotRoot), uuidString(rootID))
	}
	if gotPhase != "plan" {
		t.Fatalf("phase badge = %q, want \"plan\"", gotPhase)
	}
	if gotName != "Plan" {
		t.Fatalf("session name = %q, want \"Plan\"", gotName)
	}
	if gotMode != "plan" {
		t.Fatalf("session mode = %q, want \"plan\"", gotMode)
	}
}

func TestStampOnComplete_UpdatesBadgeButKeepsExistingName(t *testing.T) {
	pool := openWorkflowIntegrationPool(t)
	ctx := context.Background()
	f := setupWorkflowIntegrationFixture(t, pool)
	issueID := insertWorkflowIntegrationIssue(t, pool, f, "Plan me", "todo", 1, pgtype.UUID{})

	agentID := newAgentUUID(t, pool)
	rootID := insertComment(t, pool, issueID, f.workspaceID, agentID, "agent", "Here is the plan.", pgtype.UUID{})

	// A human already named this session; the stamp must set the badge without
	// clobbering the chosen name.
	if _, err := pool.Exec(ctx, `
		INSERT INTO cerebro_session (issue_id, root_comment_id, position, name, phase)
		VALUES ($1, $2, 0, 'My chosen name', NULL)`, issueID, rootID); err != nil {
		t.Fatalf("seed existing session: %v", err)
	}

	NewSessionPhaseStamper(pool).StampOnComplete(ctx, planTask(agentID, issueID, "plan", ""))

	var gotPhase, gotName, gotMode string
	if err := pool.QueryRow(ctx, `
		SELECT phase, name, mode FROM cerebro_session WHERE issue_id = $1 AND root_comment_id = $2`,
		issueID, rootID).Scan(&gotPhase, &gotName, &gotMode); err != nil {
		t.Fatalf("load session: %v", err)
	}
	if gotPhase != "plan" {
		t.Fatalf("phase badge = %q, want \"plan\"", gotPhase)
	}
	if gotName != "My chosen name" {
		t.Fatalf("session name = %q, want the pre-existing \"My chosen name\"", gotName)
	}
	if gotMode != "plan" {
		t.Fatalf("session mode = %q, want \"plan\"", gotMode)
	}
}

func TestStampOnComplete_NoLoopPhaseIsNoOp(t *testing.T) {
	pool := openWorkflowIntegrationPool(t)
	ctx := context.Background()
	f := setupWorkflowIntegrationFixture(t, pool)
	issueID := insertWorkflowIntegrationIssue(t, pool, f, "Ordinary run", "todo", 1, pgtype.UUID{})

	agentID := newAgentUUID(t, pool)
	insertComment(t, pool, issueID, f.workspaceID, agentID, "agent", "An ordinary reply.", pgtype.UUID{})

	// An ordinary (non-workflow) completed task carries no loop_phase.
	task := db.AgentTaskQueue{
		AgentID:   agentID,
		StartedAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true},
		Context:   mustJSON(map[string]any{"type": "quick_create", "prompt": "do a thing"}),
	}
	NewSessionPhaseStamper(pool).StampOnComplete(ctx, task)

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM cerebro_session WHERE issue_id = $1`, issueID).Scan(&count); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no session rows for a non-phase task, got %d", count)
	}
}
