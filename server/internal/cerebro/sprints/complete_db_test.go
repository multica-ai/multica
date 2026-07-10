package sprints

// FIR-2828: integration tests for CompleteSprint — there was previously no
// way to end a sprint at all, so these cover the three ways the operator can
// choose to handle issues still assigned to it. Reuses the sweeper test
// fixture (TestMain, sweeperTestPool/WorkspaceID/ProjectID/UserID) defined in
// sweeper_db_test.go — only one TestMain is allowed per package.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Fixed reference date for sprint start/end fixtures in this file — the
// CompleteSprint tests don't depend on sweep timing, only on stable dates.
var completeSprintTestDate = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

func newCompleteSprintHandler() *Handler {
	return NewHandler(cerebrodb.New(sweeperTestPool), sweeperTestPool, db.New(sweeperTestPool))
}

// seedSprintIssue creates an issue in the sweeper test project with the given
// status and assigns it to sprintID via cerebro_sprint_issue.
func seedSprintIssue(t *testing.T, ctx context.Context, sprintID pgtype.UUID, status string) pgtype.UUID {
	t.Helper()
	var issueID pgtype.UUID
	if err := sweeperTestPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, project_id, title, status, creator_type, creator_id)
		VALUES ($1, $2, 'Sprint completion test issue', $3, 'member', $4)
		RETURNING id
	`, sweeperTestWorkspaceID, sweeperTestProjectID, status, sweeperTestUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	q := cerebrodb.New(sweeperTestPool)
	if err := q.AssignIssueToCerebroSprint(ctx, cerebrodb.AssignIssueToCerebroSprintParams{
		IssueID:  issueID,
		SprintID: sprintID,
	}); err != nil {
		t.Fatalf("assign issue to sprint: %v", err)
	}
	return issueID
}

func seedPlannedSprint(t *testing.T, ctx context.Context, seq int32) cerebrodb.CerebroSprint {
	t.Helper()
	q := cerebrodb.New(sweeperTestPool)
	sprint, err := q.CreateCerebroSprint(ctx, cerebrodb.CreateCerebroSprintParams{
		WorkspaceID: sweeperTestWorkspaceID,
		ProjectID:   sweeperTestProjectID,
		Name:        fmt.Sprintf("Sprint %d", seq),
		SequenceNo:  seq,
		Status:      StatusPlanned,
		StartDate:   pgtype.Date{Time: completeSprintTestDate, Valid: true},
		EndDate:     pgtype.Date{Time: completeSprintTestDate.AddDate(0, 0, 14), Valid: true},
	})
	if err != nil {
		t.Fatalf("create planned sprint: %v", err)
	}
	return sprint
}

func callCompleteSprint(h *Handler, sprintID string, body map[string]any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(raw))
	req.Header.Set("X-User-ID", util.UUIDToString(sweeperTestUserID))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("sprintID", sprintID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = middleware.SetMemberContext(ctx, util.UUIDToString(sweeperTestWorkspaceID), db.Member{})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.CompleteSprint(rec, req)
	return rec
}

func TestCompleteSprint_LeaveKeepsIssuesInPlace(t *testing.T) {
	if sweeperTestPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	resetSweeperState(t, ctx)

	sprint := seedActiveSprintEndingOn(t, ctx, completeSprintTestDate)
	issueID := seedSprintIssue(t, ctx, sprint.ID, "in_progress")

	h := newCompleteSprintHandler()
	rec := callCompleteSprint(h, util.UUIDToString(sprint.ID), map[string]any{
		"incomplete_issues_action": "leave",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	q := cerebrodb.New(sweeperTestPool)
	updated, err := q.GetCerebroSprint(ctx, sprint.ID)
	if err != nil {
		t.Fatalf("get sprint: %v", err)
	}
	if updated.Status != StatusDone {
		t.Fatalf("expected sprint status done, got %q", updated.Status)
	}

	link, err := q.GetCerebroSprintForIssue(ctx, issueID)
	if err != nil {
		t.Fatalf("issue should still be assigned to the sprint: %v", err)
	}
	if link.SprintID != sprint.ID {
		t.Fatalf("issue moved to a different sprint unexpectedly")
	}
}

func TestCompleteSprint_BacklogMovesIssuesOut(t *testing.T) {
	if sweeperTestPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	resetSweeperState(t, ctx)

	sprint := seedActiveSprintEndingOn(t, ctx, completeSprintTestDate)
	issueID := seedSprintIssue(t, ctx, sprint.ID, "in_progress")

	h := newCompleteSprintHandler()
	rec := callCompleteSprint(h, util.UUIDToString(sprint.ID), map[string]any{
		"incomplete_issues_action": "backlog",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var status string
	if err := sweeperTestPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status); err != nil {
		t.Fatalf("load issue status: %v", err)
	}
	if status != "backlog" {
		t.Fatalf("expected issue status backlog, got %q", status)
	}

	q := cerebrodb.New(sweeperTestPool)
	if _, err := q.GetCerebroSprintForIssue(ctx, issueID); err == nil {
		t.Fatalf("expected issue to be unassigned from the sprint, but it is still linked")
	}
}

func TestCompleteSprint_MoveToSprintReassigns(t *testing.T) {
	if sweeperTestPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	resetSweeperState(t, ctx)

	source := seedActiveSprintEndingOn(t, ctx, completeSprintTestDate)
	target := seedPlannedSprint(t, ctx, 2)
	issueID := seedSprintIssue(t, ctx, source.ID, "todo")

	h := newCompleteSprintHandler()
	rec := callCompleteSprint(h, util.UUIDToString(source.ID), map[string]any{
		"incomplete_issues_action": "move_to_sprint",
		"target_sprint_id":         util.UUIDToString(target.ID),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	q := cerebrodb.New(sweeperTestPool)
	link, err := q.GetCerebroSprintForIssue(ctx, issueID)
	if err != nil {
		t.Fatalf("get issue sprint assignment: %v", err)
	}
	if link.SprintID != target.ID {
		t.Fatalf("expected issue reassigned to target sprint, still on %v", link.SprintID)
	}

	var status string
	if err := sweeperTestPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status); err != nil {
		t.Fatalf("load issue status: %v", err)
	}
	if status != "todo" {
		t.Fatalf("move_to_sprint must not change issue status, got %q", status)
	}
}

func TestCompleteSprint_RejectsAlreadyCompleted(t *testing.T) {
	if sweeperTestPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	resetSweeperState(t, ctx)

	sprint := seedActiveSprintEndingOn(t, ctx, completeSprintTestDate)

	h := newCompleteSprintHandler()
	first := callCompleteSprint(h, util.UUIDToString(sprint.ID), map[string]any{
		"incomplete_issues_action": "leave",
	})
	if first.Code != http.StatusOK {
		t.Fatalf("expected first complete to succeed, got %d: %s", first.Code, first.Body.String())
	}

	second := callCompleteSprint(h, util.UUIDToString(sprint.ID), map[string]any{
		"incomplete_issues_action": "leave",
	})
	if second.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 completing an already-done sprint, got %d: %s", second.Code, second.Body.String())
	}
}
