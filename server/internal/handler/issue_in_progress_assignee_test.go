package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// I4127.DP: the server must not mint or leave issues in the in_progress
// category without an assignee. WillEnqueueRun only starts a run for a valid
// assignee, so such an issue becomes a zombie: in_progress forever, no run,
// no comments, no owner — while still counting against the queue ceiling.
// These tests pin the application-layer guard on CreateIssue, UpdateIssue and
// BatchUpdateIssues (the status column's enum CHECK cannot express the
// dependency).

func issueRow(t *testing.T, id string) (status string, assigneeType, assigneeID *string) {
	t.Helper()
	var st string
	var at, aid *string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status, assignee_type, assignee_id FROM issue WHERE id = $1`, id,
	).Scan(&st, &at, &aid); err != nil {
		t.Fatalf("read issue %s: %v", id, err)
	}
	return st, at, aid
}

func TestCreateIssue_InProgressWithoutAssigneeRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// Zombie attempt: in_progress, no assignee.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "zombie attempt",
		"status":   "in_progress",
		"priority": "low",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateIssue in_progress without assignee: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Control: unassigned todo is still fine.
	todo := createIssueForTest(t, map[string]any{"title": "unassigned todo", "status": "todo"})
	if st, at, aid := issueRow(t, todo.ID); st != "todo" || at != nil || aid != nil {
		t.Fatalf("control todo: got status=%s assignee_type=%v assignee_id=%v", st, at, aid)
	}

	// Control: in_progress WITH an assignee is fine.
	agentID := createHandlerTestAgent(t, "InProgress Create Agent", nil)
	assigned := createIssueForTest(t, map[string]any{
		"title":         "in_progress assigned",
		"status":        "in_progress",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	if st, at, aid := issueRow(t, assigned.ID); st != "in_progress" || at == nil || aid == nil {
		t.Fatalf("control in_progress+assignee: got status=%s assignee_type=%v assignee_id=%v", st, at, aid)
	}
}

func TestUpdateIssue_MoveUnassignedToInProgressRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	issue := createIssueForTest(t, map[string]any{"title": "move to in_progress", "status": "todo"})

	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PATCH", "/api/issues/"+issue.ID, map[string]any{
		"status": "in_progress",
	}), "id", issue.ID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateIssue unassigned -> in_progress: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if st, _, _ := issueRow(t, issue.ID); st != "todo" {
		t.Fatalf("issue must remain todo after rejected move, got %s", st)
	}
}

func TestUpdateIssue_UnassignWhileInProgressRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "Unassign While Running Agent", nil)
	issue := createIssueForTest(t, map[string]any{
		"title":         "unassign while running",
		"status":        "in_progress",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})

	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PATCH", "/api/issues/"+issue.ID, map[string]any{
		"assignee_type": nil,
		"assignee_id":   nil,
	}), "id", issue.ID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateIssue unassign while in_progress: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if _, at, aid := issueRow(t, issue.ID); at == nil || aid == nil {
		t.Fatalf("assignee must survive rejected unassign, got type=%v id=%v", at, aid)
	}
}

func TestUpdateIssue_StatusOnlyToInProgressKeepsExistingAssignee(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "Status Only Agent", nil)
	issue := createIssueForTest(t, map[string]any{
		"title":         "status only move",
		"status":        "todo",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})

	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PATCH", "/api/issues/"+issue.ID, map[string]any{
		"status": "in_progress",
	}), "id", issue.ID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue status-only move with existing assignee: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if st, at, aid := issueRow(t, issue.ID); st != "in_progress" || at == nil || aid == nil {
		t.Fatalf("expected in_progress with preserved assignee, got status=%s type=%v id=%v", st, at, aid)
	}
}

func TestUpdateIssue_UnassignAllowedWhenLeavingInProgress(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "Leave Running Agent", nil)
	issue := createIssueForTest(t, map[string]any{
		"title":         "unassign when leaving",
		"status":        "in_progress",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})

	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PATCH", "/api/issues/"+issue.ID, map[string]any{
		"status":        "todo",
		"assignee_type": nil,
		"assignee_id":   nil,
	}), "id", issue.ID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue unassign while leaving in_progress: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if st, at, aid := issueRow(t, issue.ID); st != "todo" || at != nil || aid != nil {
		t.Fatalf("expected todo + unassigned, got status=%s type=%v id=%v", st, at, aid)
	}
}

// TestUpdateIssue_LegacyZombieForcesRepairOnNextWrite pins the guard's third
// duty: a legacy zombie (in_progress, no assignee — minted before the guard
// existed) can no longer be touched without repairing it. The first write
// that leaves it in_progress unassigned is refused; moving it to another
// status succeeds and unblocks the issue.
//
// Migration 349 adds a DB CHECK that makes it impossible to INSERT a zombie
// in the first place — which is the point — so this test fabricates the
// legacy row by dropping that constraint for the insert and restoring it
// afterwards. The test pins the application-layer guard in isolation; the
// DB layer's own behavior is covered by the migration test in
// server/internal/migrations.
func TestUpdateIssue_LegacyZombieForcesRepairOnNextWrite(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	const zombieCheck = "issue_in_progress_requires_assignee_check"
	var hadZombieCheck bool
	if err := testPool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = $1)`, zombieCheck,
	).Scan(&hadZombieCheck); err != nil {
		t.Fatalf("check for %s: %v", zombieCheck, err)
	}
	if hadZombieCheck {
		if _, err := testPool.Exec(ctx, `ALTER TABLE issue DROP CONSTRAINT `+zombieCheck); err != nil {
			t.Fatalf("drop %s for legacy-row fabrication: %v", zombieCheck, err)
		}
		t.Cleanup(func() {
			if _, err := testPool.Exec(ctx, `
				ALTER TABLE issue ADD CONSTRAINT issue_in_progress_requires_assignee_check
				CHECK (
					issue_effective_status(workspace_id, status) <> 'in_progress'
					OR (assignee_type IS NOT NULL AND assignee_id IS NOT NULL)
				)
			`); err != nil {
				t.Errorf("restore %s: %v", zombieCheck, err)
			}
		})
	}
	suffix := time.Now().UnixNano()
	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number)
		VALUES ($1, $2, 'in_progress', 'none', 'member', $3, 0, (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("legacy zombie %d", suffix), testUserID).Scan(&id); err != nil {
		t.Fatalf("insert legacy zombie: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, id) })

	// Title-only write: resulting state is still in_progress + unassigned.
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PATCH", "/api/issues/"+id, map[string]any{
		"title": "still a zombie",
	}), "id", id)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("legacy zombie title write: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Repair by moving out of in_progress is allowed.
	w = httptest.NewRecorder()
	req = withURLParam(newRequest("PATCH", "/api/issues/"+id, map[string]any{
		"status": "backlog",
	}), "id", id)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("legacy zombie repair to backlog: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if st, _, _ := issueRow(t, id); st != "backlog" {
		t.Fatalf("expected backlog after repair, got %s", st)
	}
}

func TestBatchUpdateIssues_SkipsUnassignedInProgressMove(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "Batch InProgress Agent", nil)
	unassigned := createIssueForTest(t, map[string]any{"title": "batch zombie candidate", "status": "todo"})
	assigned := createIssueForTest(t, map[string]any{
		"title":         "batch assigned",
		"status":        "todo",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})

	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/issues/batch?workspace_id="+testWorkspaceID, map[string]any{
		"issue_ids": []string{unassigned.ID, assigned.ID},
		"updates":   map[string]any{"status": "in_progress"},
	})
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("BatchUpdateIssues: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if st, _, _ := issueRow(t, unassigned.ID); st != "todo" {
		t.Fatalf("unassigned batch issue must be skipped, got status %s", st)
	}
	if st, _, _ := issueRow(t, assigned.ID); st != "in_progress" {
		t.Fatalf("assigned batch issue must move, got status %s", st)
	}
}

// TestCreateIssue_CustomStatusInProgressCategoryRequiresAssignee verifies the
// guard runs on the EFFECTIVE status (MUL-6243): a custom status whose
// category is in_progress is held to the same rule as the built-in, while a
// custom status in another category stays open to unassigned issues.
func TestCreateIssue_CustomStatusInProgressCategoryRequiresAssignee(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	suffix := time.Now().UnixNano() % 1_000_000
	progressKey := fmt.Sprintf("custom_progress_%d", suffix)
	doneKey := fmt.Sprintf("custom_done_%d", suffix)
	for _, st := range []struct{ key, category string }{
		{progressKey, "in_progress"},
		{doneKey, "todo"},
	} {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO issue_status (workspace_id, key, name, description, category, color, position)
			VALUES ($1, $2, $3, '', $4, '#123456', 1)
		`, testWorkspaceID, st.key, st.key, st.category); err != nil {
			t.Fatalf("create custom status %s: %v", st.key, err)
		}
		t.Cleanup(func() {
			testPool.Exec(ctx, `DELETE FROM issue_status WHERE workspace_id = $1 AND key = $2`, testWorkspaceID, st.key)
		})
	}

	// Custom in_progress-category status without assignee -> zombie, refused.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "custom progress zombie",
		"status": progressKey,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("custom in_progress-category without assignee: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Custom in_progress-category status WITH assignee -> fine.
	agentID := createHandlerTestAgent(t, "Custom Progress Agent", nil)
	assigned := createIssueForTest(t, map[string]any{
		"title":         "custom progress assigned",
		"status":        progressKey,
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	if st, _, _ := issueRow(t, assigned.ID); st != progressKey {
		t.Fatalf("expected custom status %s, got %s", progressKey, st)
	}

	// Custom todo-category status without assignee -> not our concern, allowed.
	unassigned := createIssueForTest(t, map[string]any{
		"title":  "custom todo unassigned",
		"status": doneKey,
	})
	if st, _, _ := issueRow(t, unassigned.ID); st != doneKey {
		t.Fatalf("expected custom status %s, got %s", doneKey, st)
	}
}
