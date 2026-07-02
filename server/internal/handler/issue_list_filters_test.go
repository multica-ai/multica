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

func TestListIssuesAppliesActorFiltersToRowsAndTotal(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, $2)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("List issue filters %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE project_id = $1`, projectID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	var assigneeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "List Issues Filter User", fmt.Sprintf("list-filter-%d@multica.ai", suffix)).Scan(&assigneeID); err != nil {
		t.Fatalf("create assignee user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, assigneeID)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, assigneeID); err != nil {
		t.Fatalf("create assignee member: %v", err)
	}

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
		RETURNING id
	`, testWorkspaceID, "List Issues Filter Agent", testRuntimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	createIssue := func(title string, position float64, assigneeType string, assigneeID string) {
		t.Helper()
		var number int32
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

		var assigneeTypeArg any
		var assigneeIDArg any
		if assigneeType != "" {
			assigneeTypeArg = assigneeType
			assigneeIDArg = assigneeID
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO issue (
				workspace_id, title, description, status, priority,
				assignee_type, assignee_id, creator_type, creator_id,
				position, number, project_id
			)
			VALUES ($1, $2, NULL, 'todo', 'none', $3, $4, 'member', $5, $6, $7, $8)
		`, testWorkspaceID, title, assigneeTypeArg, assigneeIDArg, testUserID, position, number, projectID); err != nil {
			t.Fatalf("create issue %q: %v", title, err)
		}
	}

	createIssue("List filter unassigned", 0, "", "")
	createIssue("List filter member one", 1, "member", assigneeID)
	createIssue("List filter member two", 2, "member", assigneeID)
	createIssue("List filter agent", 3, "agent", agentID)

	path := fmt.Sprintf(
		"/api/issues?workspace_id=%s&status=todo&project_id=%s&limit=2&assignee_filters=member:%s,agent:%s",
		testWorkspaceID,
		projectID,
		assigneeID,
		agentID,
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
		t.Fatalf("decode response: %v", err)
	}
	if resp.Total != 3 {
		t.Fatalf("filtered total: want 3, got %d", resp.Total)
	}
	if len(resp.Issues) != 2 {
		t.Fatalf("page length: want 2, got %d", len(resp.Issues))
	}
	for _, issue := range resp.Issues {
		if issue.Title == "List filter unassigned" {
			t.Fatalf("unassigned issue should be filtered out: %#v", resp.Issues)
		}
	}
}
