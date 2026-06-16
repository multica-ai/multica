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

func TestListGroupedIssuesArchiveFilter(t *testing.T) {
	ctx := context.Background()

	suffix := time.Now().UnixNano()
	var assigneeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Archive Filter Test User", fmt.Sprintf("grouped-archive-%d@multica.ai", suffix)).Scan(&assigneeID); err != nil {
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

	createIssue := func(title string) string {
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
			VALUES ($1, $2, NULL, 'todo', 'none', 'member', $3, 'member', $4, 1.0, $5)
			RETURNING id
		`, testWorkspaceID, title, assigneeID, testUserID, number).Scan(&id); err != nil {
			t.Fatalf("create issue %q: %v", title, err)
		}
		t.Cleanup(func() {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id)
		})
		return id
	}

	createArchivedIssue := func(title string) string {
		id := createIssue(title)
		if _, err := testPool.Exec(ctx, `
			UPDATE issue
			SET archived_at = $1
			WHERE id = $2
		`, time.Now(), id); err != nil {
			t.Fatalf("archive issue %s: %v", id, err)
		}
		return id
	}

	createIssue("Active Grouped Issue")
	createArchivedIssue("Archived Grouped Issue")

	// 1. 默认查询 (不传 archived 与 include_archived): 仅包含 Active Grouped Issue
	path := fmt.Sprintf(
		"/api/issues/grouped?workspace_id=%s&group_by=assignee&statuses=todo&assignee_filters=member:%s",
		testWorkspaceID,
		assigneeID,
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
	if len(resp.Groups) == 1 {
		group := resp.Groups[0]
		if group.Total != 1 || len(group.Issues) != 1 {
			t.Fatalf("expected 1 active issue, got total=%d, len=%d", group.Total, len(group.Issues))
		}
		if group.Issues[0].Title != "Active Grouped Issue" {
			t.Fatalf("expected 'Active Grouped Issue', got %q", group.Issues[0].Title)
		}
	} else if len(resp.Groups) > 1 {
		t.Fatalf("expected at most 1 group, got %d", len(resp.Groups))
	}

	// 2. 传入 archived=true: 仅包含 Archived Grouped Issue
	pathArchived := fmt.Sprintf(
		"/api/issues/grouped?workspace_id=%s&group_by=assignee&statuses=todo&assignee_filters=member:%s&archived=true",
		testWorkspaceID,
		assigneeID,
	)
	w2 := httptest.NewRecorder()
	testHandler.ListGroupedIssues(w2, newRequest("GET", pathArchived, nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("ListGroupedIssues archived=true: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var respArchived GroupedIssuesResponse
	if err := json.NewDecoder(w2.Body).Decode(&respArchived); err != nil {
		t.Fatalf("decode grouped response: %v", err)
	}
	if len(respArchived.Groups) == 1 {
		group := respArchived.Groups[0]
		if group.Total != 1 || len(group.Issues) != 1 {
			t.Fatalf("expected 1 archived issue, got total=%d, len=%d", group.Total, len(group.Issues))
		}
		if group.Issues[0].Title != "Archived Grouped Issue" {
			t.Fatalf("expected 'Archived Grouped Issue', got %q", group.Issues[0].Title)
		}
	} else {
		t.Fatalf("expected exactly 1 group for archived, got %d", len(respArchived.Groups))
	}

	// 3. 传入 include_archived=true: 包含两者
	pathInclude := fmt.Sprintf(
		"/api/issues/grouped?workspace_id=%s&group_by=assignee&statuses=todo&assignee_filters=member:%s&include_archived=true",
		testWorkspaceID,
		assigneeID,
	)
	w3 := httptest.NewRecorder()
	testHandler.ListGroupedIssues(w3, newRequest("GET", pathInclude, nil))
	if w3.Code != http.StatusOK {
		t.Fatalf("ListGroupedIssues include_archived=true: expected 200, got %d: %s", w3.Code, w3.Body.String())
	}
	var respInclude GroupedIssuesResponse
	if err := json.NewDecoder(w3.Body).Decode(&respInclude); err != nil {
		t.Fatalf("decode grouped response: %v", err)
	}
	if len(respInclude.Groups) == 1 {
		group := respInclude.Groups[0]
		if group.Total != 2 || len(group.Issues) != 2 {
			t.Fatalf("expected 2 issues, got total=%d, len=%d", group.Total, len(group.Issues))
		}
	} else {
		t.Fatalf("expected exactly 1 group for include_archived, got %d", len(respInclude.Groups))
	}
}

