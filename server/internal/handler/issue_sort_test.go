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

func TestListIssuesSortsByStatusAndUpdatedAt(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Issue table sort %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	type fixture struct {
		title     string
		status    string
		updatedAt time.Time
	}
	fixtures := []fixture{
		{"sort-done", "done", time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)},
		{"sort-backlog", "backlog", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"sort-progress", "in_progress", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
	}
	for index, item := range fixtures {
		var number int
		if err := testPool.QueryRow(ctx, `
			UPDATE workspace
			SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
			WHERE id = $1 RETURNING issue_counter
		`, testWorkspaceID).Scan(&number); err != nil {
			t.Fatalf("next issue number: %v", err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO issue (
				workspace_id, title, status, priority, creator_type, creator_id,
				position, number, project_id, created_at, updated_at
			)
			VALUES ($1, $2, $3, 'none', 'member', $4, $5, $6, $7, $8, $8)
		`, testWorkspaceID, item.title, item.status, testUserID, index, number, projectID, item.updatedAt); err != nil {
			t.Fatalf("create issue %q: %v", item.title, err)
		}
	}

	listTitles := func(sort, direction string) []string {
		t.Helper()
		path := fmt.Sprintf(
			"/api/issues?workspace_id=%s&project_id=%s&limit=50&sort=%s&direction=%s",
			testWorkspaceID,
			projectID,
			sort,
			direction,
		)
		w := httptest.NewRecorder()
		testHandler.ListIssues(w, newRequest("GET", path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("ListIssues: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var response struct {
			Issues []IssueResponse `json:"issues"`
		}
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		titles := make([]string, 0, len(response.Issues))
		for _, issue := range response.Issues {
			titles = append(titles, issue.Title)
		}
		return titles
	}

	assertTitles := func(got, want []string) {
		t.Helper()
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}

	assertTitles(listTitles("status", "asc"), []string{
		"sort-backlog",
		"sort-progress",
		"sort-done",
	})
	assertTitles(listTitles("status", "desc"), []string{
		"sort-done",
		"sort-progress",
		"sort-backlog",
	})
	assertTitles(listTitles("updated_at", "asc"), []string{
		"sort-backlog",
		"sort-progress",
		"sort-done",
	})
	assertTitles(listTitles("updated_at", "desc"), []string{
		"sort-done",
		"sort-progress",
		"sort-backlog",
	})
}
