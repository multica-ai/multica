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

// Backs the workspace Members/Agents tabs: assignee_types on ListIssues must
// filter server-side (same semantics as the ListGroupedIssues param) so the
// client no longer post-filters loaded pages, and `total` must agree with the
// filter so per-status pagination counts stay correct.
func TestListIssues_AssigneeTypesFilter(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	// Dedicated project so counts aren't polluted by other tests sharing the
	// workspace.
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Assignee Types %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID) })

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Assignee Types Agent %d", suffix), testRuntimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID) })

	insertIssue := func(title string, assigneeType, assigneeID *string) string {
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
			INSERT INTO issue (workspace_id, title, status, priority, assignee_type, assignee_id, creator_type, creator_id, position, number, project_id)
			VALUES ($1, $2, 'todo', 'none', $3, $4, 'member', $5, 0, $6, $7) RETURNING id
		`, testWorkspaceID, title, assigneeType, assigneeID, testUserID, number, projectID).Scan(&id); err != nil {
			t.Fatalf("create issue %q: %v", title, err)
		}
		t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id) })
		return id
	}

	memberType, agentType := "member", "agent"
	memberIssue := insertIssue(fmt.Sprintf("at-member-%d", suffix), &memberType, &testUserID)
	agentIssue := insertIssue(fmt.Sprintf("at-agent-%d", suffix), &agentType, &agentID)
	unassignedIssue := insertIssue(fmt.Sprintf("at-none-%d", suffix), nil, nil)

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

	// Baseline: every project issue comes back without the filter.
	allIDs, allTotal := list("")
	for _, want := range []string{memberIssue, agentIssue, unassignedIssue} {
		if !containsIssueID(allIDs, want) {
			t.Fatalf("baseline list missing %s — all=%v", want, allIDs)
		}
	}
	if allTotal != 3 {
		t.Fatalf("baseline total: want 3, got %d", allTotal)
	}

	// Members tab: only member-assigned issues, and total agrees.
	memberIDs, memberTotal := list("&assignee_types=member")
	if !containsIssueID(memberIDs, memberIssue) {
		t.Fatalf("member filter missing %s — got %v", memberIssue, memberIDs)
	}
	if containsIssueID(memberIDs, agentIssue) || containsIssueID(memberIDs, unassignedIssue) {
		t.Fatalf("member filter leaked non-member issues: %v", memberIDs)
	}
	if memberTotal != 1 {
		t.Fatalf("member total: want 1, got %d", memberTotal)
	}

	// Agents tab: agent+squad kinds — squad has no rows here, param must
	// still parse and return the agent-assigned issue only.
	agentIDs, agentTotal := list("&assignee_types=agent,squad")
	if !containsIssueID(agentIDs, agentIssue) {
		t.Fatalf("agent filter missing %s — got %v", agentIssue, agentIDs)
	}
	if containsIssueID(agentIDs, memberIssue) || containsIssueID(agentIDs, unassignedIssue) {
		t.Fatalf("agent filter leaked non-agent issues: %v", agentIDs)
	}
	if agentTotal != 1 {
		t.Fatalf("agent total: want 1, got %d", agentTotal)
	}

	// Unknown actor kinds are a client bug — reject, don't coerce.
	bad := httptest.NewRecorder()
	testHandler.ListIssues(bad, newRequest("GET", fmt.Sprintf(
		"/api/issues?workspace_id=%s&assignee_types=bogus", testWorkspaceID), nil))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid assignee_types: expected 400, got %d: %s", bad.Code, bad.Body.String())
	}
}
