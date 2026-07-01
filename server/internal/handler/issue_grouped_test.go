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

func TestListGroupedIssuesAssigneePaginatesPerGroup(t *testing.T) {
	ctx := context.Background()

	suffix := time.Now().UnixNano()
	var assigneeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Grouped Issues Test User", fmt.Sprintf("grouped-%d@multica.ai", suffix)).Scan(&assigneeID); err != nil {
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
	`, testWorkspaceID, "Grouped Issues Test Agent", testRuntimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	createIssue := func(title, assigneeType, assigneeID string, position float64) string {
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

		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (
				workspace_id, title, description, status, priority,
				assignee_type, assignee_id, creator_type, creator_id,
				position, number
			)
			VALUES ($1, $2, NULL, 'todo', 'none', $3, $4, 'member', $5, $6, $7)
			RETURNING id
		`, testWorkspaceID, title, assigneeType, assigneeID, testUserID, position, number).Scan(&id); err != nil {
			t.Fatalf("create issue %q: %v", title, err)
		}
		t.Cleanup(func() {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id)
		})
		return id
	}

	createIssue("Grouped member one", "member", assigneeID, 1)
	createIssue("Grouped member two", "member", assigneeID, 2)
	createIssue("Grouped member three", "member", assigneeID, 3)
	createIssue("Grouped agent one", "agent", agentID, 1)

	path := fmt.Sprintf(
		"/api/issues/grouped?workspace_id=%s&group_by=assignee&statuses=todo&limit=2&assignee_filters=member:%s,agent:%s",
		testWorkspaceID,
		assigneeID,
		agentID,
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

	memberGroupID := "assignee:member:" + assigneeID
	agentGroupID := "assignee:agent:" + agentID
	groups := map[string]IssueAssigneeGroupResponse{}
	for _, group := range resp.Groups {
		groups[group.ID] = group
	}

	memberGroup, ok := groups[memberGroupID]
	if !ok {
		t.Fatalf("missing member group %s in %#v", memberGroupID, resp.Groups)
	}
	if memberGroup.Total != 3 || len(memberGroup.Issues) != 2 {
		t.Fatalf("member group total/page mismatch: total=%d len=%d", memberGroup.Total, len(memberGroup.Issues))
	}
	if memberGroup.Issues[0].Title != "Grouped member one" || memberGroup.Issues[1].Title != "Grouped member two" {
		t.Fatalf("member group order mismatch: %#v", memberGroup.Issues)
	}

	agentGroup, ok := groups[agentGroupID]
	if !ok {
		t.Fatalf("missing agent group %s in %#v", agentGroupID, resp.Groups)
	}
	if agentGroup.Total != 1 || len(agentGroup.Issues) != 1 {
		t.Fatalf("agent group total/page mismatch: total=%d len=%d", agentGroup.Total, len(agentGroup.Issues))
	}

	nextPath := fmt.Sprintf(
		"/api/issues/grouped?workspace_id=%s&group_by=assignee&statuses=todo&limit=2&offset=2&group_assignee_type=member&group_assignee_id=%s",
		testWorkspaceID,
		assigneeID,
	)
	next := httptest.NewRecorder()
	testHandler.ListGroupedIssues(next, newRequest("GET", nextPath, nil))
	if next.Code != http.StatusOK {
		t.Fatalf("ListGroupedIssues next page: expected 200, got %d: %s", next.Code, next.Body.String())
	}

	var nextResp GroupedIssuesResponse
	if err := json.NewDecoder(next.Body).Decode(&nextResp); err != nil {
		t.Fatalf("decode next grouped response: %v", err)
	}
	if len(nextResp.Groups) != 1 {
		t.Fatalf("expected one next-page group, got %#v", nextResp.Groups)
	}
	if nextResp.Groups[0].ID != memberGroupID || nextResp.Groups[0].Total != 3 || len(nextResp.Groups[0].Issues) != 1 {
		t.Fatalf("unexpected next-page group: %#v", nextResp.Groups[0])
	}
	if nextResp.Groups[0].Issues[0].Title != "Grouped member three" {
		t.Fatalf("unexpected next-page issue: %#v", nextResp.Groups[0].Issues[0])
	}
}

func TestListGroupedIssuesFiltersByTeam(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano() % 1_000_000
	keyA := fmt.Sprintf("A%06d", suffix)
	keyB := fmt.Sprintf("B%06d", suffix)

	var workspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, '', $3)
		RETURNING id
	`, "Grouped Team Filter", fmt.Sprintf("grouped-team-filter-%d", suffix), keyA).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})

	var teamA, teamB string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace_team (workspace_id, name, key, is_default)
		VALUES ($1, 'Team A', $2, true)
		RETURNING id
	`, workspaceID, keyA).Scan(&teamA); err != nil {
		t.Fatalf("create team A: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace_team (workspace_id, name, key, is_default)
		VALUES ($1, 'Team B', $2, false)
		RETURNING id
	`, workspaceID, keyB).Scan(&teamB); err != nil {
		t.Fatalf("create team B: %v", err)
	}

	createIssue := func(title, teamID string, position float64) {
		t.Helper()
		var number int32
		if err := testPool.QueryRow(ctx, `
			UPDATE workspace
			SET issue_counter = issue_counter + 1
			WHERE id = $1
			RETURNING issue_counter
		`, workspaceID).Scan(&number); err != nil {
			t.Fatalf("next issue number: %v", err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO issue (
				workspace_id, team_id, title, description, status, priority,
				assignee_type, assignee_id, creator_type, creator_id,
				position, number
			)
			VALUES ($1, $2, $3, NULL, 'todo', 'none', 'member', $4, 'member', $4, $5, $6)
		`, workspaceID, teamID, title, testUserID, position, number); err != nil {
			t.Fatalf("create issue %q: %v", title, err)
		}
	}

	createIssue("Team A first", teamA, 1)
	createIssue("Team A second", teamA, 2)
	createIssue("Team B hidden", teamB, 3)

	path := fmt.Sprintf(
		"/api/issues/grouped?workspace_id=%s&group_by=assignee&statuses=todo&team_id=%s&limit=10",
		workspaceID,
		teamA,
	)
	w := httptest.NewRecorder()
	req := newRequest("GET", path, nil)
	req.Header.Set("X-Workspace-ID", workspaceID)
	testHandler.ListGroupedIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListGroupedIssues: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp GroupedIssuesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode grouped response: %v", err)
	}
	if len(resp.Groups) != 1 {
		t.Fatalf("expected one assignee group, got %#v", resp.Groups)
	}
	if resp.Groups[0].Total != 2 || len(resp.Groups[0].Issues) != 2 {
		t.Fatalf("expected only Team A issues, total=%d len=%d", resp.Groups[0].Total, len(resp.Groups[0].Issues))
	}
	for _, issue := range resp.Groups[0].Issues {
		if issue.TeamID == nil || *issue.TeamID != teamA {
			t.Fatalf("grouped team filter leaked wrong team issue: %#v", issue)
		}
		if issue.Title == "Team B hidden" {
			t.Fatalf("grouped team filter leaked Team B issue: %#v", issue)
		}
	}
}
