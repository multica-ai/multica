package handler

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// workingAgentsFacetCounts posts one facets request and returns the
// working-agents facet as agent id -> running task count.
func workingAgentsFacetCounts(
	t *testing.T,
	request *http.Request,
) map[string]int64 {
	t.Helper()

	var response issueTableFacetsResponse
	testutil.Call(t, testHandler.ListIssueTableFacets, request).Want(http.StatusOK).JSON(&response)
	counts := map[string]int64{}
	for _, facet := range response.Facets {
		if facet.Kind != "working_agents" {
			continue
		}
		if facet.Values == nil {
			t.Fatal("working-agents values must be an array, including when empty")
		}
		for _, value := range facet.Values {
			counts[value.Key] = value.Count
		}
	}
	return counts
}

func TestIssueTableWorkingAgentsFacetCountsOnlyEligibleRuns(t *testing.T) {
	projectID := dbfx.Project(t, "working facet eligibility")
	issueID := dbfx.Issue(t, "working facet issue", testutil.Cols{"project_id": projectID})
	agentID := dbfx.Agent(t, "working facet active", "")
	archivedID := dbfx.Agent(t, "working facet archived", "", testutil.Cols{"archived_at": testutil.Raw("now()")})
	systemID := dbfx.Agent(t, "working facet system", "", testutil.Cols{"kind": "system"})
	foreignWorkspaceID := dbfx.Workspace(t, "working facet foreign", fmt.Sprintf("working-facet-%d", time.Now().UnixNano()))
	foreignAgentID := dbfx.Agent(t, "working facet foreign", "", testutil.Cols{"workspace_id": foreignWorkspaceID})
	// Deliberately cross-link both sides: task agents and issues must each be
	// bounded to the request workspace, even if their other ids match.
	foreignIssueID := dbfx.Issue(t, "working facet foreign issue", testutil.Cols{
		"workspace_id": foreignWorkspaceID, "project_id": projectID,
	})
	chatID := dbfx.ChatSession(t, agentID)
	autopilotID := insertListTestAutopilot(t, agentID, "working-facet-autopilot")
	runID := dbfx.Insert(t, "autopilot_run", testutil.Cols{
		"autopilot_id": autopilotID, "source": "manual", "status": "running",
	})

	// Two tasks on the same issue are two runs, not one distinct issue.
	for range 2 {
		dbfx.Task(t, agentID, testutil.Cols{"runtime_id": testRuntimeID, "issue_id": issueID, "status": "running"})
	}
	for _, excludedID := range []string{archivedID, systemID, foreignAgentID} {
		dbfx.Task(t, excludedID, testutil.Cols{"runtime_id": testRuntimeID, "issue_id": issueID, "status": "running"})
	}
	for _, status := range []string{"queued", "dispatched", "completed", "failed", "cancelled"} {
		statusIssueID := dbfx.Issue(t, "working facet "+status, testutil.Cols{"project_id": projectID})
		dbfx.Task(t, agentID, testutil.Cols{"runtime_id": testRuntimeID, "issue_id": statusIssueID, "status": status})
	}
	for _, cols := range []testutil.Cols{
		{"issue_id": issueID, "chat_session_id": chatID},
		{"issue_id": issueID, "autopilot_run_id": runID},
		{"issue_id": issueID, "chat_session_id": chatID, "autopilot_run_id": runID},
		{"issue_id": foreignIssueID},
		{}, // Quick-create work has no issue yet.
	} {
		cols["status"] = "running"
		cols["runtime_id"] = testRuntimeID
		dbfx.Task(t, agentID, cols)
	}
	for _, scope := range []map[string]any{
		{"kind": "project", "project_id": projectID},
		{"kind": "workspace"},
	} {
		counts := workingAgentsFacetCounts(t, workingAgentsFacetRequest(scope, nil))
		if !maps.Equal(counts, map[string]int64{agentID: 2}) {
			t.Errorf("scope %v: counts = %v, want only active agent with 2 issue runs", scope, counts)
		}
	}

	// The final running -> completed transition must be visible on the next
	// request, without changing query identity or waiting for a cache TTL.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed' WHERE agent_id = $1`, agentID)
	counts := workingAgentsFacetCounts(t, workingAgentsFacetRequest(map[string]any{"kind": "workspace"}, nil))
	if len(counts) != 0 {
		t.Errorf("after completion: counts = %v, want empty", counts)
	}
}

func TestIssueTableWorkingAgentsFacetVisibility(t *testing.T) {
	memberID := dbfx.User(t, "working facet member", "working-facet-member@multica.test")
	dbfx.Member(t, testWorkspaceID, memberID, "member")
	issueID := dbfx.Issue(t, "working facet visibility")
	privateID := dbfx.Agent(t, "working facet private", "")
	ownedID := dbfx.Agent(t, "working facet member-owned", "", testutil.Cols{"owner_id": memberID})
	workspaceID := dbfx.Agent(t, "working facet workspace", "", testutil.Cols{"permission_mode": "public_to"})
	allowedID := dbfx.Agent(t, "working facet allow-listed", "", testutil.Cols{"permission_mode": "public_to"})
	deniedID := dbfx.Agent(t, "working facet other allow-list", "", testutil.Cols{"permission_mode": "public_to"})
	for _, target := range []struct{ agentID, kind, id string }{
		{workspaceID, "workspace", testWorkspaceID},
		{allowedID, "member", memberID},
		{deniedID, "member", testUserID},
	} {
		dbfx.Insert(t, "agent_invocation_target", testutil.Cols{
			"agent_id": target.agentID, "target_type": target.kind, "target_id": target.id,
		})
	}
	all := map[string]int64{}
	for _, id := range []string{privateID, ownedID, workspaceID, allowedID, deniedID} {
		dbfx.Task(t, id, testutil.Cols{"runtime_id": testRuntimeID, "issue_id": issueID, "status": "running"})
		all[id] = 1
	}
	for _, tc := range []struct {
		name    string
		userID  string
		asAgent bool
		want    map[string]int64
	}{
		{"member", memberID, false, map[string]int64{ownedID: 1, workspaceID: 1, allowedID: 1}},
		{"workspace owner", testUserID, false, all},
		{"agent actor", memberID, true, all},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := workingAgentsFacetRequest(map[string]any{"kind": "workspace"}, nil)
			req.Header.Set("X-User-ID", tc.userID)
			if tc.asAgent {
				// Emulate the trusted identity stamped by task-token middleware.
				req.Header.Set("X-Actor-Source", "task_token")
				req.Header.Set("X-Agent-ID", ownedID)
			}
			if got := workingAgentsFacetCounts(t, req); !maps.Equal(got, tc.want) {
				t.Errorf("counts = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIssueTableWorkingAgentsFacetMyRelations(t *testing.T) {
	otherID := dbfx.User(t, "working facet other member", "working-facet-other@multica.test")
	dbfx.Member(t, testWorkspaceID, otherID, "admin")
	agentID := dbfx.Agent(t, "working facet my agent", "")
	for _, cols := range []testutil.Cols{
		{"creator_id": otherID, "assignee_type": "member", "assignee_id": testUserID},
		{"creator_id": testUserID},
		{"creator_id": otherID, "assignee_type": "agent", "assignee_id": agentID},
		{"creator_id": otherID, "assignee_type": "member", "assignee_id": otherID},
	} {
		issueID := dbfx.Issue(t, "working facet my relation", cols)
		dbfx.Task(t, agentID, testutil.Cols{"runtime_id": testRuntimeID, "issue_id": issueID, "status": "running"})
	}
	for _, tc := range []struct {
		userID   string
		relation string
		want     int64
	}{
		{testUserID, "assigned", 1}, {testUserID, "created", 1},
		{testUserID, "involved", 1}, {testUserID, "any", 3}, {testUserID, "", 3},
		{otherID, "assigned", 1}, {otherID, "created", 3},
		{otherID, "involved", 0}, {otherID, "any", 3},
	} {
		req := workingAgentsFacetRequest(map[string]any{"kind": "my", "relation": tc.relation}, nil)
		req.Header.Set("X-User-ID", tc.userID)
		got := workingAgentsFacetCounts(t, req)
		want := map[string]int64{}
		if tc.want > 0 {
			want[agentID] = tc.want
		}
		if !maps.Equal(got, want) {
			t.Errorf("user %s relation %q: counts = %v, want %v", tc.userID, tc.relation, got, want)
		}
	}
}

func TestIssueTableWorkingAgentsFacetNoActiveAgents(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "working facet empty", fmt.Sprintf("working-facet-empty-%d", time.Now().UnixNano()))
	dbfx.Member(t, workspaceID, testUserID, "owner")
	agentID := dbfx.Agent(t, "working facet only archived", "", testutil.Cols{
		"workspace_id": workspaceID, "archived_at": testutil.Raw("now()"),
	})
	issueID := dbfx.Issue(t, "working facet archived issue", testutil.Cols{"workspace_id": workspaceID})
	dbfx.Task(t, agentID, testutil.Cols{"runtime_id": testRuntimeID, "issue_id": issueID, "status": "running"})
	req := workingAgentsFacetRequest(map[string]any{"kind": "workspace"}, nil)
	req.Header.Set("X-Workspace-ID", workspaceID)
	if got := workingAgentsFacetCounts(t, req); len(got) != 0 {
		t.Errorf("counts = %v, want empty for workspace with only archived agents", got)
	}
}

func workingAgentsFacetRequest(scope, filters map[string]any) *http.Request {
	if filters == nil {
		filters = map[string]any{}
	}
	return newRequest(http.MethodPost, "/api/issues/table/facets", map[string]any{
		"query": map[string]any{
			"scope":   scope,
			"filters": filters,
			"sort":    map[string]any{"field": "position", "direction": "asc"},
		},
		"facets":        []map[string]any{{"kind": "working_agents"}},
		"include_total": false,
	})
}

// MUL-5525. The header chip's count used to come from a workspace-wide
// projection while the list came from the surface's own compiled query, so a
// project page could advertise "2 agents working" and then open an empty list.
// The `working_agents` facet answers the same question against the same scope
// and filters the rows come from.
func TestIssueTableWorkingAgentsFacetFollowsSurfaceScopeAndFilters(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	insideAgentID := createHandlerTestAgent(t, "facet-working-inside", []byte(`{}`))
	outsideAgentID := createHandlerTestAgent(t, "facet-working-outside", []byte(`{}`))
	idleAgentID := createHandlerTestAgent(t, "facet-working-idle", []byte(`{}`))

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, 'Working Agents Facet Project')
		RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	var finalNumber int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET issue_counter = GREATEST(
			issue_counter,
			(SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)
		) + 4
		WHERE id = $1
		RETURNING issue_counter
	`, testWorkspaceID).Scan(&finalNumber); err != nil {
		t.Fatalf("reserve issue numbers: %v", err)
	}

	insertIssue := func(title, status string, number int, project any, parent any) string {
		t.Helper()
		var issueID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (
				workspace_id, title, status, priority, creator_type, creator_id,
				project_id, parent_issue_id, position, number
			)
			VALUES ($1, $2, $3, 'none', 'member', $4, $5, $6, $7, $8)
			RETURNING id
		`,
			testWorkspaceID, title, status, testUserID, project, parent, number, number,
		).Scan(&issueID); err != nil {
			t.Fatalf("insert issue %q: %v", title, err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
		})
		return issueID
	}

	inProjectTodoID := insertIssue("in project, todo", "todo", finalNumber-3, projectID, nil)
	inProjectDoneID := insertIssue("in project, done", "done", finalNumber-2, projectID, nil)
	outsideProjectID := insertIssue("outside the project", "todo", finalNumber-1, nil, nil)
	subIssueID := insertIssue("in project, sub-issue", "todo", finalNumber, projectID, inProjectTodoID)

	// insideAgent holds two running tasks inside the project (one on each
	// status) plus one on the sub-issue; outsideAgent works only outside it;
	// idleAgent has no running task at all.
	createHandlerTestTaskForAgentOnIssue(t, insideAgentID, inProjectTodoID)
	createHandlerTestTaskForAgentOnIssue(t, insideAgentID, inProjectDoneID)
	createHandlerTestTaskForAgentOnIssue(t, insideAgentID, subIssueID)
	createHandlerTestTaskForAgentOnIssue(t, outsideAgentID, outsideProjectID)

	projectScope := map[string]any{"kind": "project", "project_id": projectID}

	t.Run("project scope excludes agents working elsewhere", func(t *testing.T) {
		counts := workingAgentsFacetCounts(t, workingAgentsFacetRequest(projectScope, nil))
		if counts[insideAgentID] != 3 {
			t.Errorf("inside agent count = %d, want 3", counts[insideAgentID])
		}
		if _, present := counts[outsideAgentID]; present {
			t.Errorf("agent working outside the project was counted: %s", outsideAgentID)
		}
		if _, present := counts[idleAgentID]; present {
			t.Errorf("agent with no running task was counted: %s", idleAgentID)
		}
	})

	t.Run("a status filter narrows the count like it narrows the rows", func(t *testing.T) {
		counts := workingAgentsFacetCounts(t, workingAgentsFacetRequest(projectScope, map[string]any{
			"statuses": []string{"done"},
		}))
		if counts[insideAgentID] != 1 {
			t.Errorf("inside agent count under status=done = %d, want 1", counts[insideAgentID])
		}
	})

	t.Run("hiding sub-issues drops their tasks from the count", func(t *testing.T) {
		counts := workingAgentsFacetCounts(t, workingAgentsFacetRequest(projectScope, map[string]any{
			"include_sub_issues": false,
		}))
		if counts[insideAgentID] != 2 {
			t.Errorf("inside agent count without sub-issues = %d, want 2", counts[insideAgentID])
		}
	})

	t.Run("a filter that matches nothing reports a real zero", func(t *testing.T) {
		counts := workingAgentsFacetCounts(t, workingAgentsFacetRequest(projectScope, map[string]any{
			"statuses": []string{"cancelled"},
		}))
		if len(counts) != 0 {
			t.Errorf("counts = %v, want empty", counts)
		}
	})

	// The facet drops only its OWN dimension, so the answer is identical whether
	// the agents-working filter is on or off. That is what lets the chip's number
	// stay put when you click it.
	t.Run("the working filter itself does not change the answer", func(t *testing.T) {
		off := workingAgentsFacetCounts(t, workingAgentsFacetRequest(projectScope, nil))
		on := workingAgentsFacetCounts(t, workingAgentsFacetRequest(projectScope, map[string]any{
			"working_issue_ids": []string{inProjectTodoID},
		}))
		if off[insideAgentID] != on[insideAgentID] {
			t.Errorf("count moved with the filter: off=%d on=%d", off[insideAgentID], on[insideAgentID])
		}
		matchNone := workingAgentsFacetCounts(t, workingAgentsFacetRequest(projectScope, map[string]any{
			"working_issue_ids": []string{},
		}))
		if matchNone[insideAgentID] != off[insideAgentID] {
			t.Errorf(
				"an explicit match-none working filter changed the count: %d vs %d",
				matchNone[insideAgentID], off[insideAgentID],
			)
		}
		// The inverse filter is dropped by the same rule, otherwise clicking
		// the chip into "Not working" would shrink its own number.
		notWorking := workingAgentsFacetCounts(t, workingAgentsFacetRequest(projectScope, map[string]any{
			"not_working_issue_ids": []string{inProjectTodoID},
		}))
		if notWorking[insideAgentID] != off[insideAgentID] {
			t.Errorf(
				"the not-working filter changed the count: %d vs %d",
				notWorking[insideAgentID], off[insideAgentID],
			)
		}
	})

	t.Run("workspace scope still sees both agents", func(t *testing.T) {
		counts := workingAgentsFacetCounts(t, workingAgentsFacetRequest(
			map[string]any{"kind": "workspace"}, nil,
		))
		if counts[insideAgentID] != 3 || counts[outsideAgentID] != 1 {
			t.Errorf(
				"workspace counts inside=%d outside=%d, want 3 and 1",
				counts[insideAgentID], counts[outsideAgentID],
			)
		}
	})
}

// The facet keys are agent ids, so it discloses agent identity and has to pass
// the same visibility gate as /api/working-agents: a plain member must not learn
// that someone else's private agent exists, not even as a count.
func TestIssueTableWorkingAgentsFacetHidesInaccessibleAgents(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	privateAgentID, ownerID, plainMemberID := privateAgentTestFixture(t)

	var finalNumber int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET issue_counter = GREATEST(
			issue_counter,
			(SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)
		) + 1
		WHERE id = $1
		RETURNING issue_counter
	`, testWorkspaceID).Scan(&finalNumber); err != nil {
		t.Fatalf("reserve issue numbers: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_type, creator_id,
			position, number
		)
		VALUES ($1, 'worked on by a private agent', 'todo', 'none', 'member', $2, $3, $4)
		RETURNING id
	`, testWorkspaceID, testUserID, finalNumber, finalNumber).Scan(&issueID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})
	createHandlerTestTaskForAgentOnIssue(t, privateAgentID, issueID)

	facetRequest := func(userID string) *http.Request {
		request := workingAgentsFacetRequest(map[string]any{"kind": "workspace"}, nil)
		request.Header.Set("X-User-ID", userID)
		return request
	}

	if counts := workingAgentsFacetCounts(t, facetRequest(plainMemberID)); len(counts) != 0 {
		t.Errorf("plain member saw inaccessible agents: %v", counts)
	}
	if counts := workingAgentsFacetCounts(t, facetRequest(ownerID)); counts[privateAgentID] != 1 {
		t.Errorf("agent owner count = %d, want 1", counts[privateAgentID])
	}
}
