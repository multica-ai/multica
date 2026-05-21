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

// Backs the Project Gantt view: only issues with at least one of
// start_date / due_date should come back when scheduled=true, regardless of
// status or assignee. The unfiltered call must keep returning everything.
func TestListIssues_ScheduledFilter(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	// Seed three issues in a fresh project — one with start_date only, one
	// with due_date only, and one with neither. Using a dedicated project so
	// the assertion isn't polluted by other issues seeded by parallel tests.
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Gantt Scheduled %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID) })

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
		t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id) })
		return id
	}

	start := time.Now().UTC().Truncate(24 * time.Hour)
	due := start.Add(72 * time.Hour)
	withStart := insertIssue(fmt.Sprintf("with-start-%d", suffix), &start, nil)
	withDue := insertIssue(fmt.Sprintf("with-due-%d", suffix), nil, &due)
	withBoth := insertIssue(fmt.Sprintf("with-both-%d", suffix), &start, &due)
	noDates := insertIssue(fmt.Sprintf("no-dates-%d", suffix), nil, nil)

	list := func(query string) (ids []string, total int64) {
		path := fmt.Sprintf("/api/issues?workspace_id=%s&project_id=%s&limit=500%s",
			testWorkspaceID, projectID, query)
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
		for _, iss := range resp.Issues {
			ids = append(ids, iss.ID)
		}
		return ids, resp.Total
	}

	// Without the filter every project issue comes back.
	allIDs, allTotal := list("")
	for _, want := range []string{withStart, withDue, withBoth, noDates} {
		if !containsIssueID(allIDs, want) {
			t.Fatalf("baseline list missing %s — all=%v", want, allIDs)
		}
	}
	if allTotal != 4 {
		t.Fatalf("baseline total: want 4, got %d", allTotal)
	}

	// With scheduled=true only the three dated issues should surface, and
	// CountIssues must agree so the frontend pagination logic stays sane.
	scheduledIDs, scheduledTotal := list("&scheduled=true")
	for _, want := range []string{withStart, withDue, withBoth} {
		if !containsIssueID(scheduledIDs, want) {
			t.Fatalf("scheduled list missing %s — got %v", want, scheduledIDs)
		}
	}
	if containsIssueID(scheduledIDs, noDates) {
		t.Fatalf("scheduled list unexpectedly includes undated issue %s", noDates)
	}
	if scheduledTotal != 3 {
		t.Fatalf("scheduled total: want 3, got %d", scheduledTotal)
	}
}
