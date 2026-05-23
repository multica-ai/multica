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

func TestListIssues_ReferenceFilter(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Reference Filter %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID) })

	insertIssue := func(title string) string {
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
			VALUES ($1, $2, 'todo', 'none', 'member', $3, 0, $4, $5) RETURNING id
		`, testWorkspaceID, title, testUserID, number, projectID).Scan(&id); err != nil {
			t.Fatalf("create issue %q: %v", title, err)
		}
		t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id) })
		return id
	}

	matching := insertIssue(fmt.Sprintf("reference-match-%d", suffix))
	other := insertIssue(fmt.Sprintf("reference-other-%d", suffix))
	if _, err := testPool.Exec(ctx, `
		INSERT INTO cerebro_issue_reference (
			issue_id, object, ref_id, label, metadata, created_by_type, created_by_id
		)
		VALUES ($1, 'github_pr', 'firtal-group/firtal-cerebro#525', 'PR 525', '{}'::jsonb, 'member', $2)
	`, matching, testUserID); err != nil {
		t.Fatalf("create reference: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM cerebro_issue_reference WHERE issue_id = $1`, matching)
	})

	path := fmt.Sprintf(
		"/api/issues?workspace_id=%s&project_id=%s&limit=500&reference=github_pr:firtal-group/firtal-cerebro%%23525",
		testWorkspaceID,
		projectID,
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
	if !containsIssueID(issueResponseIDs(resp.Issues), matching) {
		t.Fatalf("filtered list missing matching issue %s", matching)
	}
	if containsIssueID(issueResponseIDs(resp.Issues), other) {
		t.Fatalf("filtered list unexpectedly contains non-matching issue %s", other)
	}
}

func TestListGroupedIssues_ReferenceFilter(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Grouped Reference Filter %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID) })

	insertIssue := func(title string) string {
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

	matching := insertIssue(fmt.Sprintf("grouped-reference-match-%d", suffix))
	other := insertIssue(fmt.Sprintf("grouped-reference-other-%d", suffix))
	if _, err := testPool.Exec(ctx, `
		INSERT INTO cerebro_issue_reference (
			issue_id, object, ref_id, label, metadata, created_by_type, created_by_id
		)
		VALUES ($1, 'github_pr', 'firtal-group/firtal-cerebro#525', 'PR 525', '{}'::jsonb, 'member', $2)
	`, matching, testUserID); err != nil {
		t.Fatalf("create reference: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM cerebro_issue_reference WHERE issue_id = $1`, matching)
	})

	path := fmt.Sprintf(
		"/api/issues/grouped?workspace_id=%s&group_by=assignee&statuses=todo&project_id=%s&limit=500&reference=github_pr:firtal-group/firtal-cerebro%%23525",
		testWorkspaceID,
		projectID,
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
	if !containsIssueID(ids, matching) {
		t.Fatalf("filtered grouped list missing matching issue %s", matching)
	}
	if containsIssueID(ids, other) {
		t.Fatalf("filtered grouped list unexpectedly contains non-matching issue %s", other)
	}
}

func issueResponseIDs(issues []IssueResponse) []string {
	ids := make([]string, 0, len(issues))
	for _, issue := range issues {
		ids = append(ids, issue.ID)
	}
	return ids
}
