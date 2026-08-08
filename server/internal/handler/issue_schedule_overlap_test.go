package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestListIssues_ScheduleOverlapFilter(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Schedule Overlap %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE project_id = $1`, projectID)
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	mustDate := func(s string) time.Time {
		t.Helper()
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatalf("parse date %q: %v", s, err)
		}
		return d
	}

	insertIssue := func(title string, startDate, dueDate *time.Time) string {
		var number int
		if err := testPool.QueryRow(ctx, `
			UPDATE workspace
			SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
			WHERE id = $1 RETURNING issue_counter
		`, testWorkspaceID).Scan(&number); err != nil {
			t.Fatalf("next issue number: %v", err)
		}
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number, project_id, start_date, due_date)
			VALUES ($1, $2, 'todo', 'none', 'member', $3, 0, $4, $5, $6, $7) RETURNING id
		`, testWorkspaceID, title, testUserID, number, projectID, startDate, dueDate).Scan(&id); err != nil {
			t.Fatalf("create issue %q: %v", title, err)
		}
		return id
	}

	beforeStart := mustDate("2026-06-01")
	beforeDue := mustDate("2026-06-07")
	overlapStart := mustDate("2026-06-01")
	overlapDue := mustDate("2026-06-10")
	insideStart := mustDate("2026-06-09")
	insideDue := mustDate("2026-06-12")
	afterStart := mustDate("2026-06-15")
	afterDue := mustDate("2026-06-20")

	beforeID := insertIssue(fmt.Sprintf("before-window-%d", suffix), &beforeStart, &beforeDue)
	carryID := insertIssue(fmt.Sprintf("carry-into-window-%d", suffix), &overlapStart, &overlapDue)
	insideID := insertIssue(fmt.Sprintf("inside-window-%d", suffix), &insideStart, &insideDue)
	afterID := insertIssue(fmt.Sprintf("after-window-%d", suffix), &afterStart, &afterDue)
	noDatesID := insertIssue(fmt.Sprintf("no-dates-%d", suffix), nil, nil)

	path := fmt.Sprintf("/api/issues?workspace_id=%s&project_id=%s&limit=50&schedule_start=2026-06-08&schedule_end=2026-06-14",
		testWorkspaceID, projectID)
	w := httptest.NewRecorder()
	testHandler.ListIssues(w, newRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssues: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Issues []IssueResponse `json:"issues"`
		Total  int64           `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	ids := make([]string, 0, len(resp.Issues))
	for _, iss := range resp.Issues {
		ids = append(ids, iss.ID)
	}

	for _, want := range []string{carryID, insideID} {
		if !containsIssueID(ids, want) {
			t.Fatalf("overlap list missing %s; got %v", want, ids)
		}
	}
	for _, notWant := range []string{beforeID, afterID, noDatesID} {
		if containsIssueID(ids, notWant) {
			t.Fatalf("overlap list unexpectedly includes %s; got %v", notWant, ids)
		}
	}
	if resp.Total != 2 {
		t.Fatalf("total: want 2, got %d", resp.Total)
	}
}
