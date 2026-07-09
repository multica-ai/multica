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

// ListIssues gained a multi-select `priorities` filter so the status board can
// push priority server-side and get per-status totals that reflect the filter.
// The single `priority` param must keep working for older clients. Both filter
// the returned rows AND the `total` count.
func TestListIssues_PrioritiesFilter(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	// Dedicated project so parallel tests' issues don't pollute the counts.
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Priorities %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID) })

	insertIssue := func(title, priority string) string {
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
			INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number, project_id)
			VALUES ($1, $2, 'todo', $3, 'member', $4, 0, $5, $6) RETURNING id
		`, testWorkspaceID, title, priority, testUserID, number, projectID).Scan(&id); err != nil {
			t.Fatalf("create issue %q: %v", title, err)
		}
		t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id) })
		return id
	}

	urgentID := insertIssue(fmt.Sprintf("urgent-%d", suffix), "urgent")
	highID := insertIssue(fmt.Sprintf("high-%d", suffix), "high")
	lowID := insertIssue(fmt.Sprintf("low-%d", suffix), "low")

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

	// Baseline: no priority filter returns all three.
	allIDs, allTotal := list("")
	for _, want := range []string{urgentID, highID, lowID} {
		if !containsIssueID(allIDs, want) {
			t.Fatalf("baseline list missing %s — all=%v", want, allIDs)
		}
	}
	if allTotal != 3 {
		t.Fatalf("baseline total: want 3, got %d", allTotal)
	}

	// Multi-select: priorities=high,urgent narrows rows AND total, drops low.
	multiIDs, multiTotal := list("&priorities=high,urgent")
	for _, want := range []string{urgentID, highID} {
		if !containsIssueID(multiIDs, want) {
			t.Fatalf("priorities filter missing %s — got %v", want, multiIDs)
		}
	}
	if containsIssueID(multiIDs, lowID) {
		t.Fatalf("priorities filter unexpectedly includes low issue %s", lowID)
	}
	if multiTotal != 2 {
		t.Fatalf("priorities total: want 2, got %d", multiTotal)
	}

	// Back-compat: old single `priority=high` still filters rows AND total.
	singleIDs, singleTotal := list("&priority=high")
	if !containsIssueID(singleIDs, highID) {
		t.Fatalf("single priority filter missing %s — got %v", highID, singleIDs)
	}
	for _, unwanted := range []string{urgentID, lowID} {
		if containsIssueID(singleIDs, unwanted) {
			t.Fatalf("single priority filter unexpectedly includes %s — got %v", unwanted, singleIDs)
		}
	}
	if singleTotal != 1 {
		t.Fatalf("single priority total: want 1, got %d", singleTotal)
	}
}
