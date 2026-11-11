package handler

import (
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// "Created by me" covers issues a member filed directly and issues an agent
// filed for them through quick-create (#4673). Provenance is the run that
// filed the issue: only a run a human started directly counts, so autopilot,
// webhook and owner-fallback runs, and sub-issues an agent decided to file,
// stay out.
func TestIssueTableCreatedIncludesQuickCreateForRequester(t *testing.T) {
	suffix := time.Now().UnixNano()
	otherUser := dbfx.User(t, fmt.Sprintf("on-behalf other %d", suffix), fmt.Sprintf("on-behalf-%d@example.com", suffix))
	// Every fixture issue lives in its own project so the page limit never
	// hides one behind rows other tests left in the shared workspace.
	project := dbfx.Project(t, fmt.Sprintf("on-behalf project %d", suffix))
	runtime := dbfx.Runtime(t, fmt.Sprintf("on-behalf runtime %d", suffix))
	agent := dbfx.Agent(t, fmt.Sprintf("on-behalf agent %d", suffix), runtime, testutil.Cols{"owner_id": otherUser})

	run := func(originator, source string) string {
		return dbfx.Task(t, agent, testutil.Cols{
			"runtime_id":          runtime,
			"status":              "completed",
			"originator_user_id":  originator,
			"accountable_user_id": originator,
			"originator_source":   source,
		})
	}
	issue := func(title string, cols testutil.Cols) string {
		merged := testutil.Cols{"project_id": project}
		for k, v := range cols {
			merged[k] = v
		}
		return dbfx.Issue(t, fmt.Sprintf("on-behalf %s %d", title, suffix), merged)
	}
	filedBy := func(originType, taskID string) testutil.Cols {
		return testutil.Cols{
			"creator_type": "agent", "creator_id": agent,
			"origin_type": originType, "origin_id": taskID,
		}
	}

	mine := issue("mine", nil)
	quickForMe := issue("quick for me", filedBy("quick_create", run(testUserID, "direct_human")))
	quickForOther := issue("quick for other", filedBy("quick_create", run(otherUser, "direct_human")))
	quickOwnerFallback := issue("quick owner fallback", filedBy("quick_create", run(testUserID, "owner_fallback")))
	agentSubIssue := issue("agent sub-issue", filedBy("agent_create", run(testUserID, "direct_human")))
	byOtherMember := issue("by other member", testutil.Cols{"creator_id": otherUser})

	fixture := map[string]struct{}{
		mine: {}, quickForMe: {}, quickForOther: {}, quickOwnerFallback: {},
		agentSubIssue: {}, byOtherMember: {},
	}
	rows := func(scope issueTableScope, filters issueTableFiltersRequest) []string {
		t.Helper()
		filters.ProjectIDs = []string{project}
		var response issueTableRowsResponse
		testutil.Call(t, testHandler.ListIssueTableRows,
			newRequest(http.MethodPost, "/api/issues/table/rows", issueTableRowsRequest{
				Query: issueTableQuerySpec{
					Scope:   scope,
					Filters: filters,
					Sort:    issueTableSortRequest{Field: "title", Direction: "asc"},
				},
				Group: issueTableGroupSpec{Kind: "none"},
				Page:  issueTablePageRequest{Limit: 100},
			}),
		).Want(http.StatusOK).JSON(&response)
		ids := make([]string, 0, len(response.Rows))
		for _, row := range response.Rows {
			if _, ok := fixture[row.Issue.ID]; ok {
				ids = append(ids, row.Issue.ID)
			}
		}
		sort.Strings(ids)
		return ids
	}
	assertRows := func(name string, scope issueTableScope, filters issueTableFiltersRequest, want ...string) {
		t.Helper()
		got := rows(scope, filters)
		sort.Strings(want)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%s: ids = %v, want %v", name, got, want)
		}
	}

	workspace := issueTableScope{Kind: "workspace"}
	assertRows("my issues: created",
		issueTableScope{Kind: "my", Relation: "created"}, issueTableFiltersRequest{},
		mine, quickForMe)
	assertRows("my issues: all includes issues filed on my behalf",
		issueTableScope{Kind: "my", Relation: "any"}, issueTableFiltersRequest{},
		mine, quickForMe)
	assertRows("relation filter: created",
		workspace, issueTableFiltersRequest{Relations: []string{"created"}},
		mine, quickForMe)
	assertRows("relation filter: not created",
		workspace, issueTableFiltersRequest{Relations: []string{"created"}, Negate: []string{"relations"}},
		quickForOther, quickOwnerFallback, agentSubIssue, byOtherMember)
}

// The working-agents "mine" filter shares the definition: an agent running on
// an issue quick-created for the member counts under relation=created.
func TestListWorkspaceWorkingAgentsCreatedIncludesQuickCreateForRequester(t *testing.T) {
	suffix := time.Now().UnixNano()
	otherUser := dbfx.User(t, fmt.Sprintf("working on-behalf other %d", suffix), fmt.Sprintf("working-on-behalf-%d@example.com", suffix))
	runtime := dbfx.Runtime(t, fmt.Sprintf("working on-behalf runtime %d", suffix))
	agent := dbfx.Agent(t, fmt.Sprintf("working on-behalf agent %d", suffix), runtime, testutil.Cols{"owner_id": otherUser})

	quickCreated := func(title, originator string) string {
		origin := dbfx.Task(t, agent, testutil.Cols{
			"runtime_id":          runtime,
			"status":              "completed",
			"originator_user_id":  originator,
			"accountable_user_id": originator,
			"originator_source":   "direct_human",
		})
		issueID := dbfx.Issue(t, fmt.Sprintf("working on-behalf %s %d", title, suffix), testutil.Cols{
			"creator_type": "agent", "creator_id": agent,
			"origin_type": "quick_create", "origin_id": origin,
		})
		dbfx.Task(t, agent, testutil.Cols{
			"runtime_id": runtime, "status": "running", "issue_id": issueID,
			"started_at": testutil.Raw("now()"),
		})
		return issueID
	}
	forMe := quickCreated("for me", testUserID)
	quickCreated("for other", otherUser)

	var agents []WorkspaceWorkingAgent
	testutil.Call(t, testHandler.ListWorkspaceWorkingAgents,
		newRequest(http.MethodGet, "/api/working-agents?type=issue&scope=mine&relation=created", nil),
	).Want(http.StatusOK).JSON(&agents)

	for _, a := range agents {
		if a.ID != agent {
			continue
		}
		if fmt.Sprint(a.IssueIDs) != fmt.Sprint([]string{forMe}) {
			t.Fatalf("issue_ids = %v, want only the issue quick-created for me (%s)", a.IssueIDs, forMe)
		}
		return
	}
	t.Fatalf("agent %s running on an issue quick-created for me was not returned", agent)
}

// The id list backs "created" on client-filtered surfaces (swimlanes, Gantt),
// so it must name exactly the agent-filed issues the table query counts: the
// caller's direct quick-creates in this workspace, nothing else.
func TestListIssueIDsCreatedForMe(t *testing.T) {
	suffix := time.Now().UnixNano()
	otherUser := dbfx.User(t, fmt.Sprintf("ids on-behalf other %d", suffix), fmt.Sprintf("ids-on-behalf-%d@example.com", suffix))
	otherWorkspace := dbfx.Workspace(t, fmt.Sprintf("ids on-behalf ws %d", suffix), fmt.Sprintf("ids-on-behalf-ws-%d", suffix))

	n := 0
	filedBy := func(workspaceID, originType, originator, source string) string {
		n++
		runtime := dbfx.Runtime(t, fmt.Sprintf("ids on-behalf runtime %d-%d", n, suffix), testutil.Cols{"workspace_id": workspaceID})
		agent := dbfx.Agent(t, fmt.Sprintf("ids on-behalf agent %d-%d", n, suffix), runtime, testutil.Cols{"workspace_id": workspaceID})
		origin := dbfx.Task(t, agent, testutil.Cols{
			"runtime_id":          runtime,
			"status":              "completed",
			"originator_user_id":  originator,
			"accountable_user_id": originator,
			"originator_source":   source,
		})
		return dbfx.Issue(t, fmt.Sprintf("ids on-behalf %s %d-%d", originType, n, suffix), testutil.Cols{
			"workspace_id": workspaceID,
			"creator_type": "agent", "creator_id": agent,
			"origin_type": originType, "origin_id": origin,
		})
	}

	quickForMe := filedBy(testWorkspaceID, "quick_create", testUserID, "direct_human")
	excluded := map[string]string{
		filedBy(testWorkspaceID, "quick_create", otherUser, "direct_human"):    "quick-created for someone else",
		filedBy(testWorkspaceID, "quick_create", testUserID, "owner_fallback"): "owner-fallback quick-create",
		filedBy(testWorkspaceID, "agent_create", testUserID, "direct_human"):   "agent-filed sub-issue",
		filedBy(otherWorkspace, "quick_create", testUserID, "direct_human"):    "quick-created in another workspace",
		dbfx.Issue(t, fmt.Sprintf("ids on-behalf direct %d", suffix)):          "created directly (decided by the client)",
	}

	var out struct {
		IssueIDs []string `json:"issue_ids"`
	}
	req := newRequest(http.MethodGet, "/api/issues/created-for-me", nil)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{}))
	testutil.Call(t, testHandler.ListIssueIDsCreatedForMe, req).Want(http.StatusOK).JSON(&out)

	listed := make(map[string]struct{}, len(out.IssueIDs))
	for _, id := range out.IssueIDs {
		listed[id] = struct{}{}
	}
	if _, ok := listed[quickForMe]; !ok {
		t.Fatalf("issue quick-created for me (%s) is missing from %v", quickForMe, out.IssueIDs)
	}
	for id, why := range excluded {
		if _, ok := listed[id]; ok {
			t.Fatalf("%s (%s) must not be listed", why, id)
		}
	}
}
