package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Aggregates over many issues (MUL-8472) must count a private project only for
// viewers who can see it, and must answer a hidden project_id exactly like a
// project that does not exist.

func TestPrivateProjectDashboardsHideProjectRows(t *testing.T) {
	f := newPrivateProjectFixture(t)
	agentID := createHandlerTestAgent(t, f.marker+"-dash", []byte("[]"))
	runtimeID := handlerTestRuntimeID(t)
	for _, status := range []string{"completed", "failed"} {
		dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id": runtimeID, "issue_id": f.secretIssue, "status": status,
			"failure_reason": "agent_error.process_failure",
			"started_at":     testutil.Raw("now() - interval '2 minutes'"),
			"completed_at":   testutil.Raw("now() - interval '1 minute'"),
		})
	}
	dbfx.InsertNoID(t, "task_usage_hourly", testutil.Cols{
		"bucket_hour": testutil.Raw("date_trunc('hour', now())"), "workspace_id": testWorkspaceID,
		"runtime_id": runtimeID, "agent_id": agentID, "project_id": f.projectID,
		"provider": "anthropic", "model": f.marker, "input_tokens": 10, "task_count": 1,
	}, "agent_id = $1", agentID)

	get := func(h http.HandlerFunc, path, userID, query string) []map[string]any {
		t.Helper()
		rec := httptest.NewRecorder()
		h(rec, asUser(newRequest(http.MethodGet, path+"?days=7"+query, nil), userID))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s%s as %s = %d: %s", path, query, userID, rec.Code, rec.Body.String())
		}
		var rows []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		return rows
	}
	sum := func(rows []map[string]any, key string) float64 {
		var n float64
		for _, row := range rows {
			v, _ := row[key].(float64)
			n += v
		}
		return n
	}
	hasAgent := func(rows []map[string]any) bool {
		for _, row := range rows {
			if row["agent_id"] == agentID {
				return true
			}
		}
		return false
	}
	missing := "&project_id=00000000-0000-0000-0000-0000000000ff"
	secret := "&project_id=" + f.projectID
	for _, ep := range []struct {
		path    string
		h       http.HandlerFunc
		byAgent bool
		count   string
	}{
		{"/api/dashboard/usage/daily", testHandler.GetDashboardUsageDaily, false, "input_tokens"},
		{"/api/dashboard/usage/by-agent", testHandler.GetDashboardUsageByAgent, true, ""},
		{"/api/dashboard/agent-runtime", testHandler.GetDashboardAgentRunTime, true, ""},
		{"/api/dashboard/runtime/daily", testHandler.GetDashboardRunTimeDaily, false, "task_count"},
		{"/api/dashboard/failures/daily", testHandler.GetDashboardFailuresDaily, false, "task_count"},
		{"/api/dashboard/failures/by-agent", testHandler.GetDashboardFailuresByAgent, true, ""},
	} {
		t.Run(ep.path, func(t *testing.T) {
			if rows := get(ep.h, ep.path, f.insider, secret); len(rows) == 0 {
				t.Fatal("project member: the project rollup is empty")
			}
			if rows := get(ep.h, ep.path, f.outsider, secret); len(rows) != len(get(ep.h, ep.path, f.outsider, missing)) || len(rows) != 0 {
				t.Fatalf("outsider: hidden project_id returned %d rows, want the missing-project answer (0)", len(rows))
			}
			inside, outside := get(ep.h, ep.path, f.insider, ""), get(ep.h, ep.path, f.outsider, "")
			if ep.byAgent {
				if !hasAgent(inside) || hasAgent(outside) {
					t.Fatalf("workspace rollup names the agent: member=%t outsider=%t, want true/false", hasAgent(inside), hasAgent(outside))
				}
				return
			}
			want := 2.0
			if ep.count == "input_tokens" {
				want = 10
			}
			if d := sum(inside, ep.count) - sum(outside, ep.count); d != want {
				t.Fatalf("workspace %s: member - outsider = %v, want %v (the private project's share)", ep.count, d, want)
			}
		})
	}
}

func TestPrivateProjectWorkflowsHideProject(t *testing.T) {
	f := newPrivateProjectFixture(t)
	type workflowList struct {
		Workflows []IssueWorkflowResponse `json:"workflows"`
	}
	list := func(userID string) workflowList {
		var out workflowList
		testutil.Call(t, testHandler.ListIssueWorkflows, asUser(workflowRequest(http.MethodGet, "/api/issue-workflows", nil), userID)).
			Want(http.StatusOK).JSON(&out)
		return out
	}
	names := func(wfs []IssueWorkflowResponse) bool {
		for _, wf := range wfs {
			for _, id := range wf.ProjectIDs {
				if id == f.projectID {
					return true
				}
			}
		}
		return false
	}
	insider, outsider := list(f.insider), list(f.outsider)
	if !names(insider.Workflows) {
		t.Fatal("project member: no workflow lists the private project")
	}
	if names(outsider.Workflows) {
		t.Fatal("outsider: a workflow lists the private project")
	}

	var def IssueWorkflowResponse
	for _, wf := range insider.Workflows {
		if wf.IsDefault {
			def = wf
		}
	}
	if def.ID == "" || len(def.Steps) == 0 {
		t.Fatalf("no Default workflow with steps: %+v", insider.Workflows)
	}
	var one IssueWorkflowResponse
	testutil.Call(t, testHandler.GetIssueWorkflow, withURLParam(asUser(workflowRequest(http.MethodGet, "/api/issue-workflows/"+def.ID, nil), f.outsider), "id", def.ID)).
		Want(http.StatusOK).JSON(&one)
	if names([]IssueWorkflowResponse{one}) {
		t.Fatal("outsider: GET workflow lists the private project")
	}

	brief := func(userID string) string {
		var out struct {
			Brief string `json:"brief"`
		}
		body := map[string]any{"status_key": def.Steps[0].StatusKey, "project_id": f.projectID}
		req := withURLParam(asUser(workflowRequest(http.MethodPost, "/api/issue-workflows/"+def.ID+"/brief", body), userID), "id", def.ID)
		testutil.Call(t, testHandler.PreviewIssueWorkflowBrief, req).Want(http.StatusOK).JSON(&out)
		return out.Brief
	}
	if b := brief(f.insider); !strings.Contains(b, f.marker+" secret project") {
		t.Fatalf("project member's brief lacks the project: %q", b)
	}
	if b := brief(f.outsider); strings.Contains(b, f.marker) {
		t.Fatalf("outsider's brief names the private project: %q", b)
	}
}

func TestPrivateProjectSearchIndexManifestLeavesOutHiddenRows(t *testing.T) {
	f := newPrivateProjectFixture(t)
	manifest := func(userID string) SearchIndexManifestResponse {
		return testutil.Decode[SearchIndexManifestResponse](t, testHandler.GetSearchIndexManifest,
			asUser(searchIndexRequest(http.MethodGet, "/api/search-index/manifest", testWorkspaceID, nil), userID), http.StatusOK)
	}
	inside, outside, owner := manifest(f.insider), manifest(f.outsider), manifest(testUserID)
	if d := inside.IssueCount - outside.IssueCount; d != 1 {
		t.Errorf("issue_count member - outsider = %d, want 1", d)
	}
	if d := inside.CommentCount - outside.CommentCount; d != 1 {
		t.Errorf("comment_count member - outsider = %d, want 1", d)
	}
	if d := inside.ProjectCount - outside.ProjectCount; d != 1 {
		t.Errorf("project_count member - outsider = %d, want 1", d)
	}
	if inside.TextBytes <= outside.TextBytes {
		t.Errorf("text_bytes member %d <= outsider %d", inside.TextBytes, outside.TextBytes)
	}
	if owner.IssueCount != inside.IssueCount || owner.ProjectCount != inside.ProjectCount {
		t.Errorf("owner counts %d/%d, want the member's %d/%d", owner.IssueCount, owner.ProjectCount, inside.IssueCount, inside.ProjectCount)
	}
}
