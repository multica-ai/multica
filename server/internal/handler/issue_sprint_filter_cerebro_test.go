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

// TECH-3620: the project board filters by sprint via ?sprint_id, which the
// server applies as an EXISTS join on cerebro_sprint_issue BEFORE pagination.
// The earlier frontend approach filtered the first page client-side, so a
// sprint member sorted past the first 50 in its column vanished. These tests
// lock the server-side behavior — including the >50 case that exposed the bug.

func insertSprintTestIssue(t *testing.T, ctx context.Context, projectID, title string) string {
	t.Helper()
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
		INSERT INTO issue (
			workspace_id, title, status, priority, assignee_type, assignee_id,
			creator_type, creator_id, position, number, project_id
		)
		VALUES ($1, $2, 'todo', 'none', 'member', $3, 'member', $3, 0, $4, $5)
		RETURNING id
	`, testWorkspaceID, title, testUserID, number, projectID).Scan(&id); err != nil {
		t.Fatalf("create issue %q: %v", title, err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id) })
	return id
}

func createSprintWithMember(t *testing.T, ctx context.Context, projectID, memberIssueID string, suffix int64) string {
	t.Helper()
	var sprintID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO cerebro_sprint (workspace_id, project_id, name, sequence_no, status, start_date, end_date)
		VALUES ($1, $2, $3, 1, 'active', '2026-01-01', '2026-01-14') RETURNING id
	`, testWorkspaceID, projectID, fmt.Sprintf("Sprint %d", suffix)).Scan(&sprintID); err != nil {
		t.Fatalf("create sprint: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM cerebro_sprint WHERE id = $1`, sprintID) })
	if _, err := testPool.Exec(ctx, `
		INSERT INTO cerebro_sprint_issue (issue_id, sprint_id) VALUES ($1, $2)
	`, memberIssueID, sprintID); err != nil {
		t.Fatalf("assign issue to sprint: %v", err)
	}
	return sprintID
}

func TestListIssues_SprintFilter(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Sprint Filter %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID) })

	member := insertSprintTestIssue(t, ctx, projectID, fmt.Sprintf("sprint-member-%d", suffix))
	other := insertSprintTestIssue(t, ctx, projectID, fmt.Sprintf("sprint-other-%d", suffix))
	sprintID := createSprintWithMember(t, ctx, projectID, member, suffix)

	path := fmt.Sprintf(
		"/api/issues?workspace_id=%s&project_id=%s&limit=500&sprint_id=%s",
		testWorkspaceID, projectID, sprintID,
	)
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
	if resp.Total != 1 {
		t.Fatalf("total: want 1, got %d", resp.Total)
	}
	if !containsIssueID(issueResponseIDs(resp.Issues), member) {
		t.Fatalf("filtered list missing sprint member %s", member)
	}
	if containsIssueID(issueResponseIDs(resp.Issues), other) {
		t.Fatalf("filtered list unexpectedly contains non-member %s", other)
	}
}

// The regression Sara caught: on a project with >50 issues in a column, the
// old client-side filter dropped sprint members past the first page. With the
// server-side filter the member is returned even when limit=50 and 60 decoys
// share its column.
func TestListIssues_SprintFilter_SurvivesPagination(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Sprint Pagination %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID) })

	member := insertSprintTestIssue(t, ctx, projectID, fmt.Sprintf("paged-member-%d", suffix))
	sprintID := createSprintWithMember(t, ctx, projectID, member, suffix)
	for i := 0; i < 60; i++ {
		insertSprintTestIssue(t, ctx, projectID, fmt.Sprintf("paged-decoy-%d-%d", suffix, i))
	}

	// Sanity: without the sprint filter, a 50-page caps the 61-issue column.
	unfiltered := fmt.Sprintf("/api/issues?workspace_id=%s&project_id=%s&limit=50", testWorkspaceID, projectID)
	wu := httptest.NewRecorder()
	testHandler.ListIssues(wu, newRequest("GET", unfiltered, nil))
	var unfilteredResp struct {
		Issues []IssueResponse `json:"issues"`
		Total  int64           `json:"total"`
	}
	if err := json.NewDecoder(wu.Body).Decode(&unfilteredResp); err != nil {
		t.Fatalf("decode unfiltered response: %v", err)
	}
	if len(unfilteredResp.Issues) != 50 || unfilteredResp.Total < 61 {
		t.Fatalf("precondition failed: want a capped 50-of-61 page, got %d of %d", len(unfilteredResp.Issues), unfilteredResp.Total)
	}

	// With the sprint filter and the same small page, the member still comes back.
	filtered := fmt.Sprintf("/api/issues?workspace_id=%s&project_id=%s&limit=50&sprint_id=%s", testWorkspaceID, projectID, sprintID)
	wf := httptest.NewRecorder()
	testHandler.ListIssues(wf, newRequest("GET", filtered, nil))
	if wf.Code != http.StatusOK {
		t.Fatalf("ListIssues: expected 200, got %d: %s", wf.Code, wf.Body.String())
	}
	var filteredResp struct {
		Issues []IssueResponse `json:"issues"`
		Total  int64           `json:"total"`
	}
	if err := json.NewDecoder(wf.Body).Decode(&filteredResp); err != nil {
		t.Fatalf("decode filtered response: %v", err)
	}
	if filteredResp.Total != 1 {
		t.Fatalf("sprint total: want 1, got %d", filteredResp.Total)
	}
	if !containsIssueID(issueResponseIDs(filteredResp.Issues), member) {
		t.Fatalf("sprint member dropped by pagination — the bug is not fixed")
	}
}

func TestListGroupedIssues_SprintFilter(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Grouped Sprint Filter %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID) })

	member := insertSprintTestIssue(t, ctx, projectID, fmt.Sprintf("grouped-sprint-member-%d", suffix))
	other := insertSprintTestIssue(t, ctx, projectID, fmt.Sprintf("grouped-sprint-other-%d", suffix))
	sprintID := createSprintWithMember(t, ctx, projectID, member, suffix)

	path := fmt.Sprintf(
		"/api/issues/grouped?workspace_id=%s&group_by=assignee&statuses=todo&project_id=%s&limit=500&sprint_id=%s",
		testWorkspaceID, projectID, sprintID,
	)
	w := httptest.NewRecorder()
	testHandler.ListGroupedIssues(w, newRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListGroupedIssues: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp GroupedIssuesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode grouped response: %v", err)
	}
	var ids []string
	for _, group := range resp.Groups {
		ids = append(ids, issueResponseIDs(group.Issues)...)
	}
	if !containsIssueID(ids, member) {
		t.Fatalf("filtered grouped list missing sprint member %s", member)
	}
	if containsIssueID(ids, other) {
		t.Fatalf("filtered grouped list unexpectedly contains non-member %s", other)
	}
}
