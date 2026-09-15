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

func TestListIssuesStatusSortCountsCustomStatuses(t *testing.T) {
	const key = "sort_custom_started"
	createTestCustomStatus(t, key, "started")
	mustCreateIssue(t, "Custom sorting first", key)
	mustCreateIssue(t, "Custom sorting second", key)
	w := httptest.NewRecorder()
	testHandler.ListIssues(w, newRequest(http.MethodGet, "/api/issues?status="+key+"&sort=status&limit=1", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status sort: %d %s", w.Code, w.Body.String())
	}
	var response struct {
		Issues []IssueResponse
		Total  int
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Total != 2 || len(response.Issues) != 1 {
		t.Fatalf("total/rows = %d/%d", response.Total, len(response.Issues))
	}
}

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
		title      string
		status     string
		updatedAt  time.Time
		activityAt time.Time
	}
	fixtures := []fixture{
		{"sort-done", "done", time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
		{"sort-backlog", "backlog", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC)},
		{"sort-progress", "in_progress", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC)},
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
				position, number, project_id, created_at, updated_at, last_activity_at
			)
			VALUES ($1, $2, $3, 'none', 'member', $4, $5, $6, $7, $8, $8, $9)
		`, testWorkspaceID, item.title, item.status, testUserID, index, number, projectID, item.updatedAt, item.activityAt); err != nil {
			t.Fatalf("create issue %q: %v", item.title, err)
		}
	}

	listTitles := func(sort, direction string) []string {
		t.Helper()
		path := fmt.Sprintf(
			"/api/issues?workspace_id=%s&project_id=%s&limit=50&sort=%s",
			testWorkspaceID,
			projectID,
			sort,
		)
		if direction != "" {
			path += "&direction=" + direction
		}
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
	groupedTitles := func(sort, direction string) []string {
		t.Helper()
		path := fmt.Sprintf(
			"/api/issues/grouped?workspace_id=%s&project_id=%s&limit=50&sort=%s",
			testWorkspaceID,
			projectID,
			sort,
		)
		if direction != "" {
			path += "&direction=" + direction
		}
		w := httptest.NewRecorder()
		testHandler.ListGroupedIssues(w, newRequest("GET", path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("ListGroupedIssues: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var response GroupedIssuesResponse
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("decode grouped response: %v", err)
		}
		if len(response.Groups) != 1 {
			t.Fatalf("ListGroupedIssues groups = %d, want 1", len(response.Groups))
		}
		titles := make([]string, 0, len(response.Groups[0].Issues))
		for _, issue := range response.Groups[0].Issues {
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
	assertTitles(listTitles("last_activity", "asc"), []string{
		"sort-done",
		"sort-progress",
		"sort-backlog",
	})
	assertTitles(listTitles("last_activity", "desc"), []string{
		"sort-backlog",
		"sort-progress",
		"sort-done",
	})
	assertTitles(listTitles("last_activity", ""), []string{
		"sort-backlog",
		"sort-progress",
		"sort-done",
	})
	assertTitles(groupedTitles("last_activity", "asc"), []string{
		"sort-done",
		"sort-progress",
		"sort-backlog",
	})
	assertTitles(groupedTitles("last_activity", ""), []string{
		"sort-backlog",
		"sort-progress",
		"sort-done",
	})
}

// TestListIssuesStatusSortFollowsCatalogOrder pins that `sort=status` ranks by
// the CONCRETE status key in catalog order, not by lifecycle category
// (MUL-7379).
//
// Sibling test TestListIssuesSortsByStatusAndUpdatedAt cannot catch the
// regression this guards: it uses one issue per lifecycle category, so a
// four-bucket ranking and a per-status ranking produce the same output. Here
// the unstarted group holds backlog AND todo, and the started group holds
// in_progress, in_review and blocked — collapsing to categories ties every one
// of those and lets the created_at tiebreak decide.
//
// The custom Started status is wedged BETWEEN two built-ins by position, which
// is what separates "ranks by the catalog" from "ranks by a hardcoded list of
// the seven built-ins": no static ordering of the built-ins can produce it.
func TestListIssuesStatusSortFollowsCatalogOrder(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	customKey := fmt.Sprintf("sort_gate_%d", suffix%1_000_000)
	createTestCustomStatus(t, customKey, "started")

	// Built-ins all seed at position 0 and tie-break on their fixed rank, so the
	// custom status can only land before or after the whole group until the
	// Started positions are spread out. Restore them afterwards: the workspace
	// catalog is shared with every other test in this package.
	setPosition := func(key string, position float64) {
		t.Helper()
		if _, err := testPool.Exec(ctx,
			`UPDATE issue_status SET position = $3 WHERE workspace_id = $1 AND key = $2`,
			testWorkspaceID, key, position,
		); err != nil {
			t.Fatalf("set position for %q: %v", key, err)
		}
	}
	for _, key := range []string{"in_progress", "in_review", "blocked"} {
		t.Cleanup(func() { setPosition(key, 0) })
	}
	setPosition("in_progress", 10)
	setPosition("in_review", 20)
	setPosition(customKey, 30)
	setPosition("blocked", 40)

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Status sort order %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	// Inserted newest-first so created_at DESC — the tiebreak a category
	// ranking would fall through to — is the REVERSE of the expected order.
	// A four-bucket ranking therefore cannot accidentally pass this.
	boardOrder := []string{
		"backlog", "todo", "in_progress", "in_review", customKey, "blocked", "done", "cancelled",
	}
	titles := make([]string, 0, len(boardOrder))
	for i, status := range boardOrder {
		title := fmt.Sprintf("order-%02d-%s", i, status)
		titles = append(titles, title)
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
				position, number, project_id, created_at, updated_at, last_activity_at
			)
			VALUES ($1, $2, $3, 'none', 'member', $4, $5, $6, $7, $8, $8, $8)
		`, testWorkspaceID, title, status, testUserID, i, number, projectID,
			time.Now().Add(-time.Duration(i)*time.Minute),
		); err != nil {
			t.Fatalf("create issue %q: %v", title, err)
		}
	}

	got := func(direction string) []string {
		t.Helper()
		path := fmt.Sprintf(
			"/api/issues?workspace_id=%s&project_id=%s&limit=50&sort=status&direction=%s",
			testWorkspaceID, projectID, direction,
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
		out := make([]string, 0, len(response.Issues))
		for _, issue := range response.Issues {
			out = append(out, issue.Title)
		}
		return out
	}

	if asc := got("asc"); fmt.Sprint(asc) != fmt.Sprint(titles) {
		t.Fatalf("sort=status asc\n got  %v\n want %v", asc, titles)
	}

	reversed := make([]string, len(titles))
	for i, title := range titles {
		reversed[len(titles)-1-i] = title
	}
	if desc := got("desc"); fmt.Sprint(desc) != fmt.Sprint(reversed) {
		t.Fatalf("sort=status desc\n got  %v\n want %v", desc, reversed)
	}
}
