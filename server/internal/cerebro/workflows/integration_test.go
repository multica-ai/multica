package workflows

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func openWorkflowIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database unreachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

type workflowIntegrationFixture struct {
	workspaceID pgtype.UUID
	userID      pgtype.UUID
}

func setupWorkflowIntegrationFixture(t *testing.T, pool *pgxpool.Pool) workflowIntegrationFixture {
	t.Helper()
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var f workflowIntegrationFixture
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Workflow Integration", "workflow-integration-"+suffix+"@multica.test").Scan(&f.userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Workflow Integration", "workflow-integration-"+suffix, "Temporary workflow integration fixture", "WFI").Scan(&f.workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, f.workspaceID, f.userID); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, f.workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, f.userID)
	})
	return f
}

func insertWorkflowIntegrationIssue(t *testing.T, pool *pgxpool.Pool, f workflowIntegrationFixture, title, status string, number int, parentID pgtype.UUID) pgtype.UUID {
	t.Helper()
	var issueID pgtype.UUID
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_type, creator_id,
			parent_issue_id, number, kind
		)
		VALUES ($1, $2, $3, 'none', 'member', $4, $5, $6, 'issue')
		RETURNING id
	`, f.workspaceID, title, status, f.userID, parentID, number).Scan(&issueID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	return issueID
}

func workflowFromRow(row cerebrodb.CerebroWorkflow) workflow {
	return workflow{
		id:             row.ID,
		workspaceID:    row.WorkspaceID,
		projectID:      row.ProjectID,
		name:           row.Name,
		triggerType:    row.TriggerType,
		triggerConfig:  row.TriggerConfig,
		conditions:     row.Conditions,
		actionType:     row.ActionType,
		actionConfig:   row.ActionConfig,
		createdByID:    row.CreatedByID,
		createdByType:  row.CreatedByType,
		outboundSecret: row.OutboundWebhookSecret.String,
	}
}

func TestWorkflowIntegration_StatusChangedCommentCreatesOneCommentAndSuccessfulRun(t *testing.T) {
	pool := openWorkflowIntegrationPool(t)
	ctx := context.Background()
	f := setupWorkflowIntegrationFixture(t, pool)
	issueID := insertWorkflowIntegrationIssue(t, pool, f, "Needs QA", "todo", 1, pgtype.UUID{})

	cq := cerebrodb.New(pool)
	row, err := cq.CreateCerebroWorkflow(ctx, cerebrodb.CreateCerebroWorkflowParams{
		WorkspaceID:   f.workspaceID,
		ProjectID:     pgtype.UUID{},
		Name:          "Comment when moved to review",
		Enabled:       true,
		TriggerType:   TriggerStatusChanged,
		TriggerConfig: mustJSON(TriggerConfigStatusChanged{ToStatus: "in_review"}),
		Conditions:    mustJSON([]Condition{}),
		ActionType:    ActionCommentOnIssue,
		ActionConfig:  mustJSON(ActionConfigCommentOnIssue{Target: CommentTargetSelf, Content: "Workflow saw {{issue.title}}"}),
		CreatedByID:   f.userID,
		CreatedByType: "member",
		EditorMode:    "form",
		EditorLayout:  []byte(`null`),
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}

	svc := &Service{queries: cq, issues: db.New(pool), enabled: true}
	te := TriggerEvent{
		EventID:     "integration-status-comment",
		WorkspaceID: uuidString(f.workspaceID),
		IssueID:     uuidString(issueID),
		Type:        TriggerStatusChanged,
		FromStatus:  "todo",
		ToStatus:    "in_review",
		Raw: map[string]any{
			"issue": map[string]any{
				"id":     uuidString(issueID),
				"title":  "Needs QA",
				"status": "in_review",
			},
		},
	}
	if err := svc.Execute(ctx, workflowFromRow(row), te); err != nil {
		t.Fatalf("execute workflow: %v", err)
	}
	if err := svc.Execute(ctx, workflowFromRow(row), te); err != nil {
		t.Fatalf("replay workflow: %v", err)
	}

	comments, err := db.New(pool).ListCommentsForIssue(ctx, db.ListCommentsForIssueParams{
		IssueID:     issueID,
		WorkspaceID: f.workspaceID,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("comment count = %d, want 1", len(comments))
	}
	if comments[0].Content != "Workflow saw Needs QA" {
		t.Fatalf("comment content = %q", comments[0].Content)
	}

	runs, err := cq.ListCerebroWorkflowRuns(ctx, cerebrodb.ListCerebroWorkflowRunsParams{
		WorkflowID: row.ID,
		Limit:      10,
		Offset:     0,
	})
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("run count = %d, want 1", len(runs))
	}
	if runs[0].Status != "success" {
		t.Fatalf("run status = %q, want success", runs[0].Status)
	}
	if runs[0].TargetIssueID != issueID {
		t.Fatalf("target_issue_id = %v, want %v", runs[0].TargetIssueID, issueID)
	}
}

func TestWorkflowIntegration_AllChildrenDoneSetsParentStatusAndSuccessfulRun(t *testing.T) {
	pool := openWorkflowIntegrationPool(t)
	ctx := context.Background()
	f := setupWorkflowIntegrationFixture(t, pool)
	parentID := insertWorkflowIntegrationIssue(t, pool, f, "Parent", "in_progress", 11, pgtype.UUID{})
	insertWorkflowIntegrationIssue(t, pool, f, "Child", "done", 12, parentID)

	cq := cerebrodb.New(pool)
	counts, err := cq.CountNonDoneChildrenLocked(ctx, cerebrodb.CountNonDoneChildrenLockedParams{
		ParentIssueID: parentID,
		WorkspaceID:   f.workspaceID,
	})
	if err != nil {
		t.Fatalf("count non-done children: %v", err)
	}
	if counts.OpenCount != 0 {
		t.Fatalf("open child count = %d, want 0", counts.OpenCount)
	}
	if !counts.LastDoneAt.Valid {
		t.Fatal("last_done_at must be set when done children exist")
	}

	row, err := cq.CreateCerebroWorkflow(ctx, cerebrodb.CreateCerebroWorkflowParams{
		WorkspaceID:   f.workspaceID,
		ProjectID:     pgtype.UUID{},
		Name:          "Close parent",
		Enabled:       true,
		TriggerType:   TriggerAllChildrenDone,
		TriggerConfig: mustJSON(map[string]any{}),
		Conditions:    mustJSON([]Condition{}),
		ActionType:    ActionSetStatus,
		ActionConfig:  mustJSON(ActionConfigSetStatus{Status: "done"}),
		CreatedByID:   f.userID,
		CreatedByType: "member",
		EditorMode:    "form",
		EditorLayout:  []byte(`null`),
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}

	svc := &Service{queries: cq, issues: db.New(pool), enabled: true}
	if err := svc.Execute(ctx, workflowFromRow(row), TriggerEvent{
		EventID:     "all_children_done:" + uuidString(parentID) + ":" + counts.LastDoneAt.Time.UTC().Format(rfcNano),
		WorkspaceID: uuidString(f.workspaceID),
		IssueID:     uuidString(parentID),
		Type:        TriggerAllChildrenDone,
		Raw:         map[string]any{"issue": map[string]any{"id": uuidString(parentID)}},
	}); err != nil {
		t.Fatalf("execute workflow: %v", err)
	}

	parent, err := db.New(pool).GetIssue(ctx, parentID)
	if err != nil {
		t.Fatalf("get parent: %v", err)
	}
	if parent.Status != "done" {
		t.Fatalf("parent status = %q, want done", parent.Status)
	}
	runs, err := cq.ListCerebroWorkflowRuns(ctx, cerebrodb.ListCerebroWorkflowRunsParams{
		WorkflowID: row.ID,
		Limit:      10,
		Offset:     0,
	})
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 || runs[0].Status != "success" {
		t.Fatalf("runs = %+v, want one successful run", runs)
	}
}
