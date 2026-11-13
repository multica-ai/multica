package handler

import (
	"net/http"
	"slices"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func idleTableRowsRequest(projectID string, extra map[string]any) *http.Request {
	filters := map[string]any{"idle_only": true, "project_ids": []string{projectID}}
	for key, value := range extra {
		filters[key] = value
	}
	return newRequest(http.MethodPost, "/api/issues/table/rows", map[string]any{
		"query": map[string]any{
			"scope": map[string]any{"kind": "workspace"}, "filters": filters,
			"sort": map[string]any{"field": "position", "direction": "asc"},
		},
		"group": map[string]any{"kind": "none"}, "group_key": nil,
		"hierarchy": map[string]any{"enabled": false}, "parent_id": nil,
		"page": map[string]any{"limit": 100},
	})
}

func idleTableIDs(t *testing.T, request *http.Request) []string {
	t.Helper()
	var response issueTableRowsResponse
	testutil.Call(t, testHandler.ListIssueTableRows, request).Want(http.StatusOK).JSON(&response)
	ids := make([]string, 0, len(response.Rows))
	for _, row := range response.Rows {
		ids = append(ids, row.Issue.ID)
	}
	slices.Sort(ids)
	return ids
}

func TestIssueTableIdleMatchesInboxActivity(t *testing.T) {
	projectID := dbfx.Project(t, "idle filter")
	agentID := dbfx.Agent(t, "idle activity", testRuntimeID)
	otherAgentID := dbfx.Agent(t, "other idle activity", testRuntimeID)
	untouched := dbfx.Issue(t, "never started", testutil.Cols{"project_id": projectID, "assignee_type": "agent", "assignee_id": agentID})
	want := []string{untouched}
	for _, status := range []string{"running", "queued", "dispatched", "waiting_local_directory", "deferred", "completed", "failed", "cancelled"} {
		issueID := dbfx.Issue(t, status, testutil.Cols{"project_id": projectID})
		dbfx.Task(t, agentID, testutil.Cols{"runtime_id": testRuntimeID, "issue_id": issueID, "status": status})
		if slices.Contains([]string{"deferred", "completed", "failed", "cancelled"}, status) {
			want = append(want, issueID)
		}
	}
	wakeupIssue := dbfx.Issue(t, "deferred wakeup", testutil.Cols{"project_id": projectID})
	dbfx.Task(t, agentID, testutil.Cols{"runtime_id": testRuntimeID, "issue_id": wakeupIssue, "status": "deferred", "context": `{"wakeup_id":"01900000-0000-7000-8000-000000000001"}`})
	sharedIssue := dbfx.Issue(t, "two agents", testutil.Cols{"project_id": projectID})
	first := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": testRuntimeID, "issue_id": sharedIssue, "status": "running"})
	second := dbfx.Task(t, otherAgentID, testutil.Cols{"runtime_id": testRuntimeID, "issue_id": sharedIssue, "status": "queued"})
	check := func() {
		t.Helper()
		slices.Sort(want)
		if got := idleTableIDs(t, idleTableRowsRequest(projectID, nil)); !slices.Equal(got, want) {
			t.Fatalf("idle rows = %v, want %v", got, want)
		}
	}
	check()
	dbfx.Exec(t, "UPDATE agent_task_queue SET status='completed' WHERE id=$1", first)
	check() // A second queued run still keeps the issue busy.
	dbfx.Exec(t, "UPDATE agent_task_queue SET status='completed' WHERE id=$1", second)
	want = append(want, sharedIssue)
	check()
	dbfx.Task(t, agentID, testutil.Cols{"runtime_id": testRuntimeID, "issue_id": untouched, "status": "running"})
	want = slices.DeleteFunc(want, func(id string) bool { return id == untouched })
	check()
	for _, conflict := range []map[string]any{{"working_only": true}, {"working_issue_ids": []string{}}} {
		testutil.Call(t, testHandler.ListIssueTableRows, idleTableRowsRequest(projectID, conflict)).Want(http.StatusBadRequest)
	}
}

func TestIssueTableIdleRespectsAgentVisibilityAndWorkspace(t *testing.T) {
	projectID := dbfx.Project(t, "idle visibility")
	memberID := dbfx.User(t, "idle member", "idle-member@multica.test")
	dbfx.Member(t, testWorkspaceID, memberID, "member")
	privateAgent := dbfx.Agent(t, "private idle agent", testRuntimeID)
	visibleAgent := dbfx.Agent(t, "visible idle agent", testRuntimeID, testutil.Cols{"owner_id": memberID})
	privateIssue := dbfx.Issue(t, "private run", testutil.Cols{"project_id": projectID})
	visibleIssue := dbfx.Issue(t, "visible run", testutil.Cols{"project_id": projectID})
	foreignIssue := dbfx.Issue(t, "foreign run", testutil.Cols{"project_id": projectID})
	foreignWorkspace := dbfx.Workspace(t, "idle foreign", "idle-foreign")
	foreignAgent := dbfx.Agent(t, "foreign idle agent", "", testutil.Cols{"workspace_id": foreignWorkspace})
	for agent, issue := range map[string]string{privateAgent: privateIssue, visibleAgent: visibleIssue, foreignAgent: foreignIssue} {
		dbfx.Task(t, agent, testutil.Cols{"runtime_id": testRuntimeID, "issue_id": issue, "status": "running"})
	}
	req := idleTableRowsRequest(projectID, nil)
	req.Header.Set("X-User-ID", memberID)
	want := []string{privateIssue, foreignIssue}
	slices.Sort(want)
	if got := idleTableIDs(t, req); !slices.Equal(got, want) {
		t.Fatalf("member idle rows = %v, want %v", got, want)
	}
	if got := idleTableIDs(t, idleTableRowsRequest(projectID, nil)); !slices.Equal(got, []string{foreignIssue}) {
		t.Fatalf("owner idle rows = %v, want only foreign-linked issue", got)
	}
	// The header count is independent of the chosen activity filter.
	scope := map[string]any{"kind": "workspace"}
	all := workingAgentsFacetCounts(t, workingAgentsFacetRequest(scope, map[string]any{"project_ids": []string{projectID}}))
	idle := workingAgentsFacetCounts(t, workingAgentsFacetRequest(scope, map[string]any{"project_ids": []string{projectID}, "idle_only": true}))
	if len(all) != 2 || len(idle) != 2 {
		t.Fatalf("working facet changed under Idle: all=%v idle=%v", all, idle)
	}
}
