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

func TestIssueListsSortByUpdatedAt(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, $2)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Updated sort %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	insertIssue := func(title string, updatedAt time.Time) string {
		t.Helper()
		var number int
		if err := testPool.QueryRow(ctx, `
			UPDATE workspace
			SET issue_counter = GREATEST(
				issue_counter,
				(SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)
			) + 1
			WHERE id = $1
			RETURNING issue_counter
		`, testWorkspaceID).Scan(&number); err != nil {
			t.Fatalf("next issue number: %v", err)
		}

		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (
				workspace_id, title, status, priority,
				assignee_type, assignee_id, creator_type, creator_id,
				position, number, project_id, updated_at
			)
			VALUES ($1, $2, 'todo', 'none', 'member', $3, 'member', $3, 0, $4, $5, $6)
			RETURNING id
		`, testWorkspaceID, title, testUserID, number, projectID, updatedAt).Scan(&id); err != nil {
			t.Fatalf("create issue %q: %v", title, err)
		}
		t.Cleanup(func() {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id)
		})
		return id
	}

	olderID := insertIssue("Updated sort older", time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC))
	newerID := insertIssue("Updated sort newer", time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC))

	flat := httptest.NewRecorder()
	testHandler.ListIssues(flat, newRequest("GET", fmt.Sprintf(
		"/api/issues?workspace_id=%s&project_id=%s&sort=updated_at&direction=desc",
		testWorkspaceID,
		projectID,
	), nil))
	if flat.Code != http.StatusOK {
		t.Fatalf("ListIssues: expected 200, got %d: %s", flat.Code, flat.Body.String())
	}
	var flatResp struct {
		Issues []IssueResponse `json:"issues"`
	}
	if err := json.NewDecoder(flat.Body).Decode(&flatResp); err != nil {
		t.Fatalf("decode flat response: %v", err)
	}
	if len(flatResp.Issues) != 2 || flatResp.Issues[0].ID != newerID || flatResp.Issues[1].ID != olderID {
		t.Fatalf("flat updated_at order = %#v, want [%s, %s]", flatResp.Issues, newerID, olderID)
	}

	grouped := httptest.NewRecorder()
	testHandler.ListGroupedIssues(grouped, newRequest("GET", fmt.Sprintf(
		"/api/issues/grouped?workspace_id=%s&group_by=assignee&project_ids=%s&sort=updated_at&direction=asc",
		testWorkspaceID,
		projectID,
	), nil))
	if grouped.Code != http.StatusOK {
		t.Fatalf("ListGroupedIssues: expected 200, got %d: %s", grouped.Code, grouped.Body.String())
	}
	var groupedResp GroupedIssuesResponse
	if err := json.NewDecoder(grouped.Body).Decode(&groupedResp); err != nil {
		t.Fatalf("decode grouped response: %v", err)
	}
	if len(groupedResp.Groups) != 1 ||
		len(groupedResp.Groups[0].Issues) != 2 ||
		groupedResp.Groups[0].Issues[0].ID != olderID ||
		groupedResp.Groups[0].Issues[1].ID != newerID {
		t.Fatalf("grouped updated_at order = %#v, want [%s, %s]", groupedResp.Groups, olderID, newerID)
	}
}
