package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issueworkflow"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Project workflow tests (MUL-7420).

// workflowTestSuffix keeps names unique across tests sharing the workspace.
func workflowTestSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func workflowRequest(method, path string, body any) *http.Request {
	return newRequest(method, path+"?workspace_id="+testWorkspaceID, body)
}

// statusSteps names a workflow's own steps by status key.
func statusSteps(keys ...string) []map[string]any {
	out := make([]map[string]any, len(keys))
	for i, key := range keys {
		out[i] = map[string]any{"status_key": key}
	}
	return out
}

// createTestWorkflow creates a workflow with backlog and blocked through the
// API and deletes it after the test, detaching any project still using it
// first.
func createTestWorkflow(t *testing.T, name string, middle ...string) IssueWorkflowResponse {
	t.Helper()
	return createTestWorkflowSteps(t, name, statusSteps(append(append([]string{"backlog"}, middle...), "blocked")...))
}

func createTestWorkflowSteps(t *testing.T, name string, steps []map[string]any) IssueWorkflowResponse {
	t.Helper()
	var created IssueWorkflowResponse
	testutil.Call(t, testHandler.CreateIssueWorkflow, workflowRequest("POST", "/api/issue-workflows", map[string]any{
		"name":  name,
		"steps": steps,
	})).Want(http.StatusCreated).JSON(&created)
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `UPDATE project SET workflow_id = NULL WHERE workflow_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM issue_workflow WHERE id = $1`, created.ID)
	})
	return created
}

// resetDefaultWorkflow drops the shared workspace's stored Default after the
// test, so the next test snapshots its own.
func resetDefaultWorkflow(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_workflow WHERE workspace_id = $1 AND is_default`, testWorkspaceID)
	})
}

func createWorkflowTestProject(t *testing.T, title string) string {
	t.Helper()
	projectID := dbfx.Project(t, title)
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM issue_workflow WHERE id = (SELECT workflow_id FROM project WHERE id = $1)
			AND NOT EXISTS (SELECT 1 FROM project p WHERE p.workflow_id = issue_workflow.id AND p.id <> $1)`, projectID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE project_id = $1`, projectID)
		testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectID)
	})
	return projectID
}

func setProjectWorkflow(t *testing.T, projectID string, body map[string]any) *testutil.Response {
	t.Helper()
	req := withURLParam(workflowRequest("POST", "/api/projects/"+projectID+"/workflow", body), "id", projectID)
	return testutil.Call(t, testHandler.SetProjectWorkflow, req)
}

// projectWorkflowID is the workflow a project uses. For a project on Default
// it stores Default and drops it after the test, since Default's handoffs
// apply to every test project on it.
func projectWorkflowID(t *testing.T, projectID string) string {
	t.Helper()
	var id *string
	dbfx.QueryRow(t, `SELECT workflow_id::text FROM project WHERE id = $1`, projectID).Scan(&id)
	if id != nil {
		return *id
	}
	resetDefaultWorkflow(t)
	var list struct {
		Workflows []IssueWorkflowResponse `json:"workflows"`
	}
	testutil.Call(t, testHandler.ListIssueWorkflows, workflowRequest("GET", "/api/issue-workflows", nil)).Want(http.StatusOK).JSON(&list)
	return list.Workflows[0].ID
}

func setWorkflowHandoff(t *testing.T, workflowID, statusKey string, body map[string]any) *testutil.Response {
	t.Helper()
	req := withURLParams(workflowRequest("PUT", "/api/issue-workflows/"+workflowID+"/handoffs/"+statusKey, body),
		"id", workflowID, "status", statusKey)
	return testutil.Call(t, testHandler.SetIssueWorkflowHandoff, req)
}

// setColumnHandoff sets a column's handoff on the workflow a project uses.
func setColumnHandoff(t *testing.T, projectID, statusKey string, body map[string]any) *testutil.Response {
	t.Helper()
	return setWorkflowHandoff(t, projectWorkflowID(t, projectID), statusKey, body)
}

func handToAgent(agentID, instructions string) map[string]any {
	return map[string]any{"handler": map[string]any{"type": "agent", "id": agentID}, "instructions": instructions}
}

func updateIssueForTest(t *testing.T, issueID string, body map[string]any) *testutil.Response {
	t.Helper()
	req := withURLParam(newRequest("PUT", "/api/issues/"+issueID, body), "id", issueID)
	return testutil.Call(t, testHandler.UpdateIssue, req)
}

func issueStatusAndAssignee(t *testing.T, issueID string) (status, assigneeType, assigneeID string) {
	t.Helper()
	var at, aid *string
	dbfx.QueryRow(t, `SELECT status, assignee_type, assignee_id::text FROM issue WHERE id = $1`, issueID).Scan(&status, &at, &aid)
	if at != nil {
		assigneeType = *at
	}
	if aid != nil {
		assigneeID = *aid
	}
	return status, assigneeType, assigneeID
}

// deliveryProject is the design's simplest case: a project on a Default that
// hands in_progress to an agent and keeps everything else as is.
func deliveryProject(t *testing.T, title, agentID string) string {
	t.Helper()
	projectID := createWorkflowTestProject(t, title)
	setColumnHandoff(t, projectID, "in_progress", map[string]any{
		"handler": map[string]any{"type": "agent", "id": agentID}, "instructions": "Implement it and open a PR.",
	}).Want(http.StatusOK)
	return projectID
}

func TestProjectWorkflowsAvailableWithoutFlag(t *testing.T) {
	seedTestCatalog(t)
	resetDefaultWorkflow(t)
	withFeatureFlag(t, testHandler, "project_workflows_v1", false)

	var list struct {
		Workflows []IssueWorkflowResponse `json:"workflows"`
	}
	testutil.Call(t, testHandler.ListIssueWorkflows, workflowRequest("GET", "/api/issue-workflows", nil)).Want(http.StatusOK).JSON(&list)
	if len(list.Workflows) == 0 || !list.Workflows[0].IsDefault {
		t.Fatalf("list = %+v, want Default without feature flag configuration", list.Workflows)
	}

	wf := createTestWorkflow(t, "Available "+workflowTestSuffix(), "in_progress")
	projectID := createWorkflowTestProject(t, "Workflow available project")
	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": wf.ID}).Want(http.StatusOK)
	agentID := seededReadyAgentID(t)
	setWorkflowHandoff(t, wf.ID, "in_progress", handToAgent(agentID, "")).Want(http.StatusOK)

	var project ProjectResponse
	testutil.Call(t, testHandler.CreateProject, workflowRequest("POST", "/api/projects", map[string]any{
		"title": "Created with workflow", "workflow_id": wf.ID,
	})).Want(http.StatusCreated).JSON(&project)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, project.ID)
	})
	if project.WorkflowID == nil || *project.WorkflowID != wf.ID {
		t.Fatalf("workflow_id = %v, want %s", project.WorkflowID, wf.ID)
	}
}

func TestProjectWorkflowsKeepWorkspaceIsolation(t *testing.T) {
	seedTestCatalog(t)
	resetDefaultWorkflow(t)
	wf := createTestWorkflow(t, "Isolated "+workflowTestSuffix(), "in_progress")
	projectID := createWorkflowTestProject(t, "Isolated workflow project")
	outsiderID := dbfx.User(t, "Workflow outsider", "workflow-outsider-"+workflowTestSuffix()+"@example.com")
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req := workflowRequest(method, "/api/issue-workflows", map[string]any{"name": "Unauthorized", "steps": statusSteps()})
		req.Header.Set("X-User-ID", outsiderID)
		if method == http.MethodGet {
			testutil.Call(t, testHandler.ListIssueWorkflows, req).Want(http.StatusNotFound)
		} else {
			testutil.Call(t, testHandler.CreateIssueWorkflow, req).Want(http.StatusNotFound)
		}
	}

	otherWorkspaceID := dbfx.Workspace(t, "Other workflow workspace", "wf-isolated-"+workflowTestSuffix())
	dbfx.Member(t, otherWorkspaceID, testUserID, "owner")
	otherRequest := func(method, path string, body any) *http.Request {
		req := newRequest(method, path, body)
		req.Header.Set("X-Workspace-ID", otherWorkspaceID)
		return req
	}
	get := withURLParam(otherRequest("GET", "/api/issue-workflows/"+wf.ID, nil), "id", wf.ID)
	testutil.Call(t, testHandler.GetIssueWorkflow, get).Want(http.StatusNotFound)
	switchReq := withURLParam(otherRequest("POST", "/api/projects/"+projectID+"/workflow", map[string]any{"workflow_id": wf.ID}), "id", projectID)
	testutil.Call(t, testHandler.SetProjectWorkflow, switchReq).Want(http.StatusNotFound)
	handoff := withURLParams(otherRequest("PUT", "/api/issue-workflows/"+wf.ID+"/handoffs/in_progress", map[string]any{"handler": map[string]any{"type": "none"}}), "id", wf.ID, "status", "in_progress")
	testutil.Call(t, testHandler.SetIssueWorkflowHandoff, handoff).Want(http.StatusNotFound)
	testutil.Call(t, testHandler.CreateProject, otherRequest("POST", "/api/projects", map[string]any{
		"title": "Cross-workspace workflow", "workflow_id": wf.ID,
	})).Want(http.StatusBadRequest)
}

func TestCreateIssueWorkflowPlacesFixedStatusesAndValidates(t *testing.T) {
	seedTestCatalog(t)
	resetDefaultWorkflow(t)
	name := "Validate " + workflowTestSuffix()
	created := createTestWorkflow(t, name, "done", "in_review")
	keys := make([]string, len(created.Steps))
	for i, s := range created.Steps {
		keys[i] = s.StatusKey
	}
	if got := strings.Join(keys, ","); got != "backlog,todo,in_review,blocked,done,cancelled" {
		t.Fatalf("steps = %s, want the fixed statuses around in_review", got)
	}

	for label, body := range map[string]map[string]any{
		"unknown status": {"name": "x" + workflowTestSuffix(), "steps": statusSteps("no_such_status")},
		"reserved name":  {"name": "Default", "steps": statusSteps()},
		"blank new":      {"name": "z" + workflowTestSuffix(), "steps": []map[string]any{{"new_status": map[string]any{"name": " "}}}},
	} {
		t.Run(label, func(t *testing.T) {
			testutil.Call(t, testHandler.CreateIssueWorkflow, workflowRequest("POST", "/api/issue-workflows", body)).
				Want(http.StatusBadRequest)
		})
	}
	testutil.Call(t, testHandler.CreateIssueWorkflow, workflowRequest("POST", "/api/issue-workflows", map[string]any{
		"name": strings.ToUpper(name), "steps": statusSteps(),
	})).Want(http.StatusConflict)
}

func TestCreateIssueWorkflowCreatesNewStatusesOutsideDefault(t *testing.T) {
	seedTestCatalog(t)
	resetDefaultWorkflow(t)
	statusName := "Spec " + workflowTestSuffix()[10:]
	var created IssueWorkflowResponse
	testutil.Call(t, testHandler.CreateIssueWorkflow, workflowRequest("POST", "/api/issue-workflows", map[string]any{
		"name":  "Template " + workflowTestSuffix(),
		"steps": []map[string]any{{"new_status": map[string]any{"name": statusName, "category": "started"}}, {"status_key": "in_review"}},
	})).Want(http.StatusCreated).JSON(&created)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_workflow WHERE id = $1`, created.ID)
		testPool.Exec(context.Background(), `DELETE FROM issue_status WHERE workspace_id = $1 AND name = $2`, testWorkspaceID, statusName)
	})
	newKey := created.Steps[1].StatusKey
	if issuestatusName := lookupStatusName(t, newKey); issuestatusName != statusName {
		t.Fatalf("step 2 = %q (%q), want the new status", newKey, issuestatusName)
	}

	var list struct {
		Workflows []IssueWorkflowResponse `json:"workflows"`
	}
	testutil.Call(t, testHandler.ListIssueWorkflows, workflowRequest("GET", "/api/issue-workflows", nil)).Want(http.StatusOK).JSON(&list)
	if len(list.Workflows) == 0 || !list.Workflows[0].IsDefault {
		t.Fatalf("list = %+v, want Default first", list.Workflows)
	}
	for _, s := range list.Workflows[0].Steps {
		if s.StatusKey == newKey {
			t.Fatalf("a status made for a workflow joined Default: %+v", list.Workflows[0].Steps)
		}
	}
}

func lookupStatusName(t *testing.T, key string) string {
	t.Helper()
	var name string
	dbfx.QueryRow(t, `SELECT name FROM issue_status WHERE workspace_id = $1 AND key = $2`, testWorkspaceID, key).Scan(&name)
	return name
}

func TestDefaultWorkflowIsEditableButNotDeletable(t *testing.T) {
	seedTestCatalog(t)
	resetDefaultWorkflow(t)
	var list struct {
		Workflows []IssueWorkflowResponse `json:"workflows"`
	}
	testutil.Call(t, testHandler.ListIssueWorkflows, workflowRequest("GET", "/api/issue-workflows", nil)).Want(http.StatusOK).JSON(&list)
	def := list.Workflows[0]
	if !def.IsDefault || def.Name != "Default" {
		t.Fatalf("first workflow = %+v, want Default", def)
	}
	projectID := createWorkflowTestProject(t, "Default usage project")
	issueID := dbfx.Issue(t, "stays on in_review", testutil.Cols{"project_id": projectID, "status": "in_review"})

	var updated IssueWorkflowResponse
	req := withURLParam(workflowRequest("PATCH", "/api/issue-workflows/"+def.ID, map[string]any{
		"name": "Renamed", "steps": statusSteps("backlog", "in_progress", "blocked"),
	}), "id", def.ID)
	testutil.Call(t, testHandler.UpdateIssueWorkflow, req).Want(http.StatusOK).JSON(&updated)
	if updated.Name != "Default" || len(updated.Steps) != 6 {
		t.Fatalf("updated Default = %+v, want its name kept and in_review removed", updated)
	}
	// Default accepts every status, so dropping one moves no issue.
	if status, _, _ := issueStatusAndAssignee(t, issueID); status != "in_review" {
		t.Fatalf("editing Default moved an issue to %q", status)
	}
	updateIssueForTest(t, issueID, map[string]any{"status": "in_review"}).Want(http.StatusOK)

	del := withURLParam(workflowRequest("DELETE", "/api/issue-workflows/"+def.ID, nil), "id", def.ID)
	testutil.Call(t, testHandler.DeleteIssueWorkflow, del).Want(http.StatusBadRequest)
}

func TestSetProjectWorkflowMapsOnlyUnlistedStatuses(t *testing.T) {
	seedTestCatalog(t)
	resetDefaultWorkflow(t)
	agentID := seededReadyAgentID(t)
	wf := createTestWorkflow(t, "Map "+workflowTestSuffix(), "in_review")
	projectID := createWorkflowTestProject(t, "Workflow map project")
	setWorkflowHandoff(t, wf.ID, "in_review", handToAgent(agentID, "")).Want(http.StatusOK)
	keep := dbfx.Issue(t, "stays on todo", testutil.Cols{"project_id": projectID, "status": "todo"})
	moved := dbfx.Issue(t, "leaves in_progress", testutil.Cols{"project_id": projectID, "status": "in_progress"})

	var dry struct {
		Plan workflowMappingPlan `json:"plan"`
	}
	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": wf.ID, "dry_run": true}).Want(http.StatusOK).JSON(&dry)
	if len(dry.Plan.Required) != 1 || dry.Plan.Required[0].StatusKey != "in_progress" || dry.Plan.Required[0].IssueCount != 1 {
		t.Fatalf("plan = %+v, want only in_progress with one issue", dry.Plan)
	}
	if len(dry.Plan.Unchanged) != 1 || dry.Plan.Unchanged[0].StatusKey != "todo" {
		t.Fatalf("unchanged = %+v, want todo kept", dry.Plan.Unchanged)
	}
	if got := dry.Plan.Required[0].SuggestedStatusKey; got != "in_review" {
		t.Fatalf("suggested target = %q, want the same-category step in_review", got)
	}

	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": wf.ID}).Want(http.StatusConflict)
	setProjectWorkflow(t, projectID, map[string]any{
		"workflow_id":    wf.ID,
		"status_mapping": map[string]string{"in_progress": "code_review_nope"},
	}).Want(http.StatusConflict)

	setProjectWorkflow(t, projectID, map[string]any{
		"workflow_id":    wf.ID,
		"status_mapping": map[string]string{"in_progress": "in_review"},
	}).Want(http.StatusOK)

	if status, _, _ := issueStatusAndAssignee(t, keep); status != "todo" {
		t.Fatalf("an issue on a listed status moved to %q", status)
	}
	status, assigneeType, _ := issueStatusAndAssignee(t, moved)
	if status != "in_review" {
		t.Fatalf("mapped issue status = %q, want in_review", status)
	}
	// Mapping is administrative: entering a handoff column this way neither
	// reassigns the issue nor starts a run.
	if assigneeType != "" || taskCountFor(t, moved, agentID) != 0 {
		t.Fatalf("a workflow switch handed the issue off (assignee type %q)", assigneeType)
	}

	// Switching back to Default needs no mapping.
	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": nil}).Want(http.StatusOK)
}

func TestUpdateIssueRejectsStatusOutsideNamedWorkflow(t *testing.T) {
	seedTestCatalog(t)
	resetDefaultWorkflow(t)
	wf := createTestWorkflow(t, "Reject "+workflowTestSuffix())
	projectID := createWorkflowTestProject(t, "Workflow reject project")
	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": wf.ID}).Want(http.StatusOK)
	issueID := dbfx.Issue(t, "reject in_progress", testutil.Cols{"project_id": projectID, "status": "todo"})

	body := updateIssueForTest(t, issueID, map[string]any{"status": "in_progress"}).Want(http.StatusBadRequest).Map()
	if body["code"] != "status_not_in_workflow" {
		t.Fatalf("code = %v, want status_not_in_workflow", body["code"])
	}
	if msg, _ := body["error"].(string); !strings.Contains(msg, "backlog, todo, blocked, done, cancelled") {
		t.Fatalf("error %q should list the allowed keys", msg)
	}
	updateIssueForTest(t, issueID, map[string]any{"status": "blocked"}).Want(http.StatusOK)
}

func TestStatusChangeHandsOffOnDefaultProject(t *testing.T) {
	seedTestCatalog(t)
	agentID := seededReadyAgentID(t)
	projectID := deliveryProject(t, "Workflow handoff project", agentID)
	issueID := dbfx.Issue(t, "hand me off", testutil.Cols{"project_id": projectID, "status": "todo"})

	updateIssueForTest(t, issueID, map[string]any{"status": "in_progress"}).Want(http.StatusOK)

	_, assigneeType, assigneeID := issueStatusAndAssignee(t, issueID)
	if assigneeType != "agent" || assigneeID != agentID {
		t.Fatalf("assignee = %s/%s, want the column's agent %s", assigneeType, assigneeID, agentID)
	}
	tasks, err := testHandler.Queries.ListTasksByIssue(context.Background(), util.MustParseUUID(issueID))
	if err != nil || len(tasks) != 1 {
		t.Fatalf("tasks = %d (%v), want one handoff run", len(tasks), err)
	}
	// The run reads the step's brief from its claim; the handoff note is
	// left for a caller's own instruction.
	if tasks[0].HandoffNote.String != "" {
		t.Fatalf("handoff note = %q, want none: the brief rides the claim", tasks[0].HandoffNote.String)
	}
	brief := claimBriefForTest(t, issueID, tasks[0])
	for _, want := range []string{"Implement it and open a PR.", "This project's workflow — Default (Workflow handoff project)", "Step done      → multica issue status"} {
		if !strings.Contains(brief, want) {
			t.Fatalf("claim brief is missing %q:\n%s", want, brief)
		}
	}

	// A caller-written note rides beside the step's brief.
	back := dbfx.Issue(t, "explicit note", testutil.Cols{"project_id": projectID, "status": "todo"})
	updateIssueForTest(t, back, map[string]any{"status": "in_progress", "handoff_note": "Only the login flow."}).Want(http.StatusOK)
	backTasks, _ := testHandler.Queries.ListTasksByIssue(context.Background(), util.MustParseUUID(back))
	if len(backTasks) != 1 || backTasks[0].HandoffNote.String != "Only the login flow." {
		t.Fatalf("explicit handoff note was not kept: %+v", backTasks)
	}
	if !strings.Contains(claimBriefForTest(t, back, backTasks[0]), "Implement it and open a PR.") {
		t.Fatal("a caller-written note must not cost the run its step brief")
	}

	// A project on a workflow without handoffs hands nothing off.
	plainProject := createWorkflowTestProject(t, "Workflow no handoff project")
	setProjectWorkflow(t, plainProject, map[string]any{"workflow_id": createTestWorkflow(t, "Plain "+workflowTestSuffix(), "in_progress").ID}).Want(http.StatusOK)
	plain := dbfx.Issue(t, "no handoff", testutil.Cols{"project_id": plainProject, "status": "todo"})
	updateIssueForTest(t, plain, map[string]any{"status": "in_progress"}).Want(http.StatusOK)
	if _, at, _ := issueStatusAndAssignee(t, plain); at != "" {
		t.Fatalf("a project without handoffs reassigned the issue to a %s", at)
	}
}

func TestSetWorkflowHandoffValidatesAndClears(t *testing.T) {
	seedTestCatalog(t)
	agentID := seededReadyAgentID(t)
	wf := createTestWorkflow(t, "Handoff validation "+workflowTestSuffix(), "in_progress")
	setWorkflowHandoff(t, wf.ID, "done", map[string]any{"handler": map[string]any{"type": "project_lead"}}).Want(http.StatusBadRequest)
	setWorkflowHandoff(t, wf.ID, "code_review_nope", handToAgent(agentID, "")).Want(http.StatusBadRequest)
	setWorkflowHandoff(t, wf.ID, "in_progress", map[string]any{"handler": map[string]any{"type": "creator"}}).Want(http.StatusBadRequest)
	setWorkflowHandoff(t, wf.ID, "in_progress", map[string]any{
		"handler": map[string]any{"type": "agent", "id": agentID}, "next_status_key": "in_progress",
	}).Want(http.StatusBadRequest)

	step := func(resp IssueWorkflowResponse) IssueWorkflowStepPayload {
		for _, s := range resp.Steps {
			if s.StatusKey == "in_progress" {
				return s
			}
		}
		t.Fatalf("in_progress missing from %+v", resp.Steps)
		return IssueWorkflowStepPayload{}
	}
	var saved IssueWorkflowResponse
	setWorkflowHandoff(t, wf.ID, "in_progress", handToAgent(agentID, "Build it")).Want(http.StatusOK).JSON(&saved)
	if got := step(saved); got.Handler.ID != agentID || got.Instructions != "Build it" || len(saved.Steps) != 6 {
		t.Fatalf("saved = %+v", saved.Steps)
	}
	setWorkflowHandoff(t, wf.ID, "in_progress", map[string]any{"handler": map[string]any{"type": "none"}}).Want(http.StatusOK).JSON(&saved)
	if got := step(saved); got.Handler.Type != "none" || got.Instructions != "" {
		t.Fatalf("clearing left %+v", got)
	}
	var stored string
	dbfx.QueryRow(t, `SELECT steps::text FROM issue_workflow WHERE id = $1`, wf.ID).Scan(&stored)
	if strings.Contains(stored, "handler") {
		t.Fatalf("cleared handoff still stored: %s", stored)
	}
}

// addWorkflowTestMember adds a plain member to the test workspace and returns
// its user id with a request decorator acting as that member.
func addWorkflowTestMember(t *testing.T, label string) (string, func(*http.Request) *http.Request) {
	t.Helper()
	suffix := workflowTestSuffix()
	userID := dbfx.User(t, "Workflow "+label+" "+suffix, "workflow-"+label+"-"+suffix+"@example.com")
	dbfx.Member(t, testWorkspaceID, userID, "member")
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE user_id = $1`, userID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID, func(r *http.Request) *http.Request {
		r.Header.Set("X-User-ID", userID)
		return r
	}
}

// Any member may create a workflow and becomes its owner. Only the owner and
// workspace admins edit or delete it, handoffs included (MUL-8120); a
// project's lead may still switch its workflow.
func TestWorkflowOwnerPermissions(t *testing.T) {
	seedTestCatalog(t)
	agentID := seededReadyAgentID(t)
	ownerID, asOwner := addWorkflowTestMember(t, "owner")
	_, asOther := addWorkflowTestMember(t, "other")

	var wf IssueWorkflowResponse
	create := asOwner(workflowRequest("POST", "/api/issue-workflows", map[string]any{
		"name":  "Owned " + workflowTestSuffix(),
		"steps": statusSteps("backlog", "in_progress", "blocked"),
	}))
	testutil.Call(t, testHandler.CreateIssueWorkflow, create).Want(http.StatusCreated).JSON(&wf)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE project SET workflow_id = NULL WHERE workflow_id = $1`, wf.ID)
		testPool.Exec(context.Background(), `DELETE FROM issue_workflow WHERE id = $1`, wf.ID)
	})
	if wf.OwnerID == nil || *wf.OwnerID != ownerID {
		t.Fatalf("owner_id = %v, want the creating member %s", wf.OwnerID, ownerID)
	}

	patch := func(as func(*http.Request) *http.Request, id, name string) *testutil.Response {
		return testutil.Call(t, testHandler.UpdateIssueWorkflow,
			withURLParam(as(workflowRequest("PATCH", "/api/issue-workflows/"+id, map[string]any{"name": name})), "id", id))
	}
	handoff := func(as func(*http.Request) *http.Request) *testutil.Response {
		return testutil.Call(t, testHandler.SetIssueWorkflowHandoff,
			withURLParams(as(workflowRequest("PUT", "/api/issue-workflows/"+wf.ID+"/handoffs/in_progress", handToAgent(agentID, ""))),
				"id", wf.ID, "status", "in_progress"))
	}
	remove := func(as func(*http.Request) *http.Request) *testutil.Response {
		return testutil.Call(t, testHandler.DeleteIssueWorkflow,
			withURLParam(as(workflowRequest("DELETE", "/api/issue-workflows/"+wf.ID, nil)), "id", wf.ID))
	}
	asAdmin := func(r *http.Request) *http.Request { return r }

	// Another member can read but not change it.
	patch(asOther, wf.ID, "Taken "+workflowTestSuffix()).Want(http.StatusForbidden)
	handoff(asOther).Want(http.StatusForbidden)
	remove(asOther).Want(http.StatusForbidden)

	// The owner and a workspace admin can.
	patch(asOwner, wf.ID, "Renamed "+workflowTestSuffix()).Want(http.StatusOK)
	handoff(asOwner).Want(http.StatusOK)
	patch(asAdmin, wf.ID, "Admin renamed "+workflowTestSuffix()).Want(http.StatusOK)

	// Default has no owner, so members cannot change it.
	var list struct {
		Workflows []IssueWorkflowResponse `json:"workflows"`
	}
	testutil.Call(t, testHandler.ListIssueWorkflows, asOther(workflowRequest("GET", "/api/issue-workflows", nil))).Want(http.StatusOK).JSON(&list)
	for _, row := range list.Workflows {
		if row.IsDefault {
			if row.OwnerID != nil {
				t.Fatalf("Default owner_id = %s, want null", *row.OwnerID)
			}
			patch(asOwner, row.ID, "").Want(http.StatusForbidden)
		}
	}

	// Statuses are shared by every board, so a member's workflow write may
	// reuse an existing status, by key or by name, but not add one.
	existing := createTestCustomStatus(t, "wfreuse"+workflowTestSuffix(), "started")
	newSteps := func(name string) map[string]any {
		return map[string]any{"steps": []map[string]any{
			{"status_key": "in_progress"},
			{"new_status": map[string]any{"name": name, "category": "started"}},
		}}
	}
	statusCount := func() int {
		var n int
		dbfx.QueryRow(t, `SELECT count(*) FROM issue_status WHERE workspace_id = $1`, testWorkspaceID).Scan(&n)
		return n
	}
	before := statusCount()
	novel := "Member status " + workflowTestSuffix()
	patchSteps := func(as func(*http.Request) *http.Request, body map[string]any) *testutil.Response {
		return testutil.Call(t, testHandler.UpdateIssueWorkflow,
			withURLParam(as(workflowRequest("PATCH", "/api/issue-workflows/"+wf.ID, body)), "id", wf.ID))
	}
	patchSteps(asOwner, newSteps(novel)).Want(http.StatusForbidden)
	createBody := newSteps(novel)
	createBody["name"] = "Member new " + workflowTestSuffix()
	testutil.Call(t, testHandler.CreateIssueWorkflow, asOwner(workflowRequest("POST", "/api/issue-workflows", createBody))).Want(http.StatusForbidden)
	if got := statusCount(); got != before {
		t.Fatalf("a member's workflow write added %d status(es)", got-before)
	}
	patchSteps(asOwner, newSteps(existing.Name)).Want(http.StatusOK)
	if got := statusCount(); got != before {
		t.Fatal("reusing a status by name must not add one")
	}
	var adminWF IssueWorkflowResponse
	patchSteps(asAdmin, newSteps(novel)).Want(http.StatusOK).JSON(&adminWF)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_status WHERE workspace_id = $1 AND name = $2`, testWorkspaceID, novel)
	})
	if got := statusCount(); got != before+1 {
		t.Fatalf("an admin's workflow write added %d status(es), want 1", got-before)
	}
	// Back to the steps the handoff checks below expect.
	patchSteps(asAdmin, map[string]any{"steps": statusSteps("backlog", "in_progress", "blocked")}).Want(http.StatusOK)

	// Copying the workflow for one project switches that project, which
	// needs the project's lead or an admin, even for the workflow's owner.
	projectID := createWorkflowTestProject(t, "Workflow owner project")
	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": wf.ID}).Want(http.StatusOK)
	body := handToAgent(agentID, "Only here")
	body["copy_for_project_id"] = projectID
	copyReq := func() *http.Request {
		return withURLParams(asOwner(workflowRequest("PUT", "/api/issue-workflows/"+wf.ID+"/handoffs/in_progress", body)),
			"id", wf.ID, "status", "in_progress")
	}
	testutil.Call(t, testHandler.SetIssueWorkflowHandoff, copyReq()).Want(http.StatusForbidden)
	dbfx.Exec(t, `UPDATE project SET lead_type = 'member', lead_id = $1 WHERE id = $2`, ownerID, projectID)
	var copied IssueWorkflowResponse
	testutil.Call(t, testHandler.SetIssueWorkflowHandoff, copyReq()).Want(http.StatusCreated).JSON(&copied)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_workflow WHERE id = $1`, copied.ID)
	})
	if copied.OwnerID == nil || *copied.OwnerID != ownerID {
		t.Fatalf("copy owner_id = %v, want %s", copied.OwnerID, ownerID)
	}

	// A lead who does not own the workflow switches the project but cannot
	// edit the workflow.
	otherID, _ := addWorkflowTestMember(t, "lead")
	asLead := func(r *http.Request) *http.Request { r.Header.Set("X-User-ID", otherID); return r }
	dbfx.Exec(t, `UPDATE project SET lead_type = 'member', lead_id = $1 WHERE id = $2`, otherID, projectID)
	sw := withURLParam(asLead(workflowRequest("POST", "/api/projects/"+projectID+"/workflow", map[string]any{"workflow_id": wf.ID})), "id", projectID)
	testutil.Call(t, testHandler.SetProjectWorkflow, sw).Want(http.StatusOK)
	patch(asLead, wf.ID, "Lead renamed "+workflowTestSuffix()).Want(http.StatusForbidden)

	// The owner deletes it once no project uses it.
	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": nil}).Want(http.StatusOK)
	remove(asOwner).Want(http.StatusNoContent)
}

// Backlog and blocked are optional: a workflow without them rejects them, and
// dropping one maps the issues still on it.
func TestWorkflowWithoutBacklogOrBlocked(t *testing.T) {
	seedTestCatalog(t)
	resetDefaultWorkflow(t)
	lean := createTestWorkflowSteps(t, "Lean "+workflowTestSuffix(), statusSteps("in_progress"))
	keys := make([]string, len(lean.Steps))
	for i, s := range lean.Steps {
		keys[i] = s.StatusKey
	}
	if got := strings.Join(keys, ","); got != "todo,in_progress,done,cancelled" {
		t.Fatalf("lean steps = %s", got)
	}
	projectID := createWorkflowTestProject(t, "Workflow lean project")
	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": lean.ID}).Want(http.StatusOK)
	issueID := dbfx.Issue(t, "lean", testutil.Cols{"project_id": projectID, "status": "todo"})
	updateIssueForTest(t, issueID, map[string]any{"status": "blocked"}).Want(http.StatusBadRequest)

	full := createTestWorkflow(t, "Full "+workflowTestSuffix(), "in_progress")
	other := createWorkflowTestProject(t, "Workflow drop blocked project")
	setProjectWorkflow(t, other, map[string]any{"workflow_id": full.ID}).Want(http.StatusOK)
	stuck := dbfx.Issue(t, "stuck", testutil.Cols{"project_id": other, "status": "blocked"})
	edit := func(body map[string]any) *testutil.Response {
		return testutil.Call(t, testHandler.UpdateIssueWorkflow, withURLParam(workflowRequest("PATCH", "/api/issue-workflows/"+full.ID, body), "id", full.ID))
	}
	var conflict struct {
		Plan workflowMappingPlan `json:"plan"`
	}
	edit(map[string]any{"steps": statusSteps("backlog", "in_progress")}).Want(http.StatusConflict).JSON(&conflict)
	if len(conflict.Plan.Required) != 1 || conflict.Plan.Required[0].StatusKey != "blocked" || conflict.Plan.Required[0].SuggestedStatusKey != "in_progress" {
		t.Fatalf("plan = %+v, want blocked mapped to the same-category in_progress", conflict.Plan)
	}
	edit(map[string]any{"steps": statusSteps("backlog", "in_progress"), "status_mapping": map[string]string{"blocked": "in_progress"}}).Want(http.StatusOK)
	if status, _, _ := issueStatusAndAssignee(t, stuck); status != "in_progress" {
		t.Fatalf("issue on the dropped blocked moved to %q", status)
	}
}

func TestExplicitAssigneeWinsOverStepHandler(t *testing.T) {
	seedTestCatalog(t)
	agentID := seededReadyAgentID(t)
	projectID := deliveryProject(t, "Workflow explicit project", agentID)
	issueID := dbfx.Issue(t, "keep my assignee", testutil.Cols{"project_id": projectID, "status": "todo"})

	updateIssueForTest(t, issueID, map[string]any{
		"status": "in_progress", "assignee_type": "member", "assignee_id": testUserID,
	}).Want(http.StatusOK)
	_, assigneeType, assigneeID := issueStatusAndAssignee(t, issueID)
	if assigneeType != "member" || assigneeID != testUserID {
		t.Fatalf("assignee = %s/%s, want the explicit member", assigneeType, assigneeID)
	}
	if n := taskCountFor(t, issueID, agentID); n != 0 {
		t.Fatalf("the step handler ran %d time(s) although the write chose an assignee", n)
	}
}

func TestMoveIntoWorkflowProjectKeepsEquivalentStatusWithoutHandoff(t *testing.T) {
	seedTestCatalog(t)
	resetDefaultWorkflow(t)
	agentID := seededReadyAgentID(t)
	wf := createTestWorkflow(t, "Move "+workflowTestSuffix(), "in_review")
	projectID := createWorkflowTestProject(t, "Workflow move project")
	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": wf.ID}).Want(http.StatusOK)
	setColumnHandoff(t, projectID, "in_review", handToAgent(agentID, "")).Want(http.StatusOK)
	issueID := dbfx.Issue(t, "moving in", testutil.Cols{"status": "in_progress"})

	updateIssueForTest(t, issueID, map[string]any{"project_id": projectID}).Want(http.StatusOK)
	status, assigneeType, _ := issueStatusAndAssignee(t, issueID)
	if status != "in_review" {
		t.Fatalf("status after move = %q, want the same-category step in_review", status)
	}
	if assigneeType != "" || taskCountFor(t, issueID, agentID) != 0 {
		t.Fatal("moving an issue into a project must not hand it off")
	}

	// A move confirmed in the UI enters the column it lands on.
	entering := dbfx.Issue(t, "entering", testutil.Cols{"status": "in_progress"})
	var preview WorkflowHandoffPreviewResponse
	req := withURLParam(newRequest("GET", "/api/issues/"+entering+"/workflow-handoff?project_id="+projectID, nil), "id", entering)
	testutil.Call(t, testHandler.PreviewWorkflowHandoff, req).Want(http.StatusOK).JSON(&preview)
	if !preview.Handoff || preview.ToStatus != "in_review" || preview.HandlerID != agentID {
		t.Fatalf("move preview = %+v, want a handoff to in_review's agent", preview)
	}
	updateIssueForTest(t, entering, map[string]any{"project_id": projectID, "enter_workflow_step": true}).Want(http.StatusOK)
	status, assigneeType, assigneeID := issueStatusAndAssignee(t, entering)
	if status != "in_review" || assigneeType != "agent" || assigneeID != agentID || taskCountFor(t, entering, agentID) != 1 {
		t.Fatalf("entering move = %s %s/%s, want in_review handed to the agent with one run", status, assigneeType, assigneeID)
	}
}

// Saving a shared workflow's handoff for one project copies the workflow for
// that project; the other projects keep the original.
func TestWorkflowHandoffCopyForProject(t *testing.T) {
	seedTestCatalog(t)
	agentID := seededReadyAgentID(t)
	wf := createTestWorkflow(t, "Shared "+workflowTestSuffix(), "in_progress")
	mine := createWorkflowTestProject(t, "Workflow copy mine")
	theirs := createWorkflowTestProject(t, "Workflow copy theirs")
	elsewhere := createWorkflowTestProject(t, "Workflow copy elsewhere")
	setProjectWorkflow(t, mine, map[string]any{"workflow_id": wf.ID}).Want(http.StatusOK)
	setProjectWorkflow(t, theirs, map[string]any{"workflow_id": wf.ID}).Want(http.StatusOK)

	body := handToAgent(agentID, "Only here")
	body["copy_for_project_id"] = elsewhere
	setWorkflowHandoff(t, wf.ID, "in_progress", body).Want(http.StatusConflict)

	body["copy_for_project_id"] = mine
	var copied IssueWorkflowResponse
	setWorkflowHandoff(t, wf.ID, "in_progress", body).Want(http.StatusCreated).JSON(&copied)
	if copied.ID == wf.ID || !strings.HasPrefix(copied.Name, wf.Name+" (") || len(copied.ProjectIDs) != 1 || copied.ProjectIDs[0] != mine {
		t.Fatalf("copy = %+v, want a new workflow used only by the project", copied)
	}
	handlerOf := func(id string) string {
		var steps string
		dbfx.QueryRow(t, `SELECT steps::text FROM issue_workflow WHERE id = $1`, id).Scan(&steps)
		return steps
	}
	if !strings.Contains(handlerOf(copied.ID), agentID) || strings.Contains(handlerOf(wf.ID), agentID) {
		t.Fatal("the handoff must land on the copy only")
	}
	var theirsWorkflow string
	dbfx.QueryRow(t, `SELECT workflow_id::text FROM project WHERE id = $1`, theirs).Scan(&theirsWorkflow)
	if theirsWorkflow != wf.ID {
		t.Fatalf("the other project moved to %s", theirsWorkflow)
	}
}

func TestProjectCopyName(t *testing.T) {
	taken := map[string]bool{"delivery (payments)": true}
	if got := projectCopyName("Delivery", "Payments", taken); got != "Delivery (Payments 2)" {
		t.Fatalf("name = %q", got)
	}
	long := strings.Repeat("x", 70)
	if got := projectCopyName(long, "P", nil); utf8.RuneCountInString(got) > issueworkflow.MaxNameRunes || !strings.HasSuffix(got, " (P)") {
		t.Fatalf("long name = %q", got)
	}
}

func TestCreateIssueHandsOffItsStartingColumn(t *testing.T) {
	seedTestCatalog(t)
	resetDefaultWorkflow(t)
	agentID := seededReadyAgentID(t)
	projectID := createWorkflowTestProject(t, "Workflow create project")
	setColumnHandoff(t, projectID, "todo", map[string]any{
		"handler": map[string]any{"type": "agent", "id": agentID}, "instructions": "Triage it.",
	}).Want(http.StatusOK)

	created := createIssueForTest(t, map[string]any{"title": "starts in todo", "project_id": projectID})
	if created.Status != "todo" {
		t.Fatalf("status = %q, want todo", created.Status)
	}
	if created.AssigneeID == nil || *created.AssigneeID != agentID {
		t.Fatalf("assignee = %v, want the todo column's agent", created.AssigneeID)
	}
	tasks, _ := testHandler.Queries.ListTasksByIssue(context.Background(), util.MustParseUUID(created.ID))
	if len(tasks) != 1 || !strings.Contains(claimBriefForTest(t, created.ID, tasks[0]), "Triage it.") {
		t.Fatalf("create did not start the column's run with its brief: %+v", tasks)
	}

	wf := createTestWorkflow(t, "Create "+workflowTestSuffix())
	named := createWorkflowTestProject(t, "Workflow create named project")
	setProjectWorkflow(t, named, map[string]any{"workflow_id": wf.ID}).Want(http.StatusOK)
	testutil.Call(t, testHandler.CreateIssue, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "outside the workflow", "project_id": named, "status": "in_progress",
	})).Want(http.StatusBadRequest)
}

func TestArchiveStatusGuardsNamedWorkflowsOnly(t *testing.T) {
	seedTestCatalog(t)
	resetDefaultWorkflow(t)
	entry := createTestCustomStatus(t, "wf_guard_"+workflowTestSuffix()[10:], "started")
	createTestWorkflow(t, "Guard "+workflowTestSuffix(), entry.Key)

	req := withURLParam(newRequest("DELETE", "/api/issue-statuses/"+util.UUIDToString(entry.ID), nil), "id", util.UUIDToString(entry.ID))
	body := testutil.Call(t, testHandler.ArchiveIssueStatus, req).Want(http.StatusConflict).Map()
	if body["code"] != "issue_status_in_workflow" {
		t.Fatalf("code = %v, want issue_status_in_workflow", body["code"])
	}

	// A status only Default lists archives and leaves Default.
	loose := createTestCustomStatus(t, "wf_loose_"+workflowTestSuffix()[10:], "started")
	var list struct {
		Workflows []IssueWorkflowResponse `json:"workflows"`
	}
	testutil.Call(t, testHandler.ListIssueWorkflows, workflowRequest("GET", "/api/issue-workflows", nil)).Want(http.StatusOK).JSON(&list)
	def := list.Workflows[0]
	patch := withURLParam(workflowRequest("PATCH", "/api/issue-workflows/"+def.ID, map[string]any{
		"steps": statusSteps("in_progress", loose.Key),
	}), "id", def.ID)
	testutil.Call(t, testHandler.UpdateIssueWorkflow, patch).Want(http.StatusOK)
	archive := withURLParam(newRequest("DELETE", "/api/issue-statuses/"+util.UUIDToString(loose.ID), nil), "id", util.UUIDToString(loose.ID))
	testutil.Call(t, testHandler.ArchiveIssueStatus, archive).Want(http.StatusOK)
	var steps string
	dbfx.QueryRow(t, `SELECT steps::text FROM issue_workflow WHERE id = $1`, def.ID).Scan(&steps)
	if strings.Contains(steps, loose.Key) {
		t.Fatalf("Default still lists the archived status: %s", steps)
	}
}

func TestClaimCarriesProjectWorkflow(t *testing.T) {
	seedTestCatalog(t)
	resetDefaultWorkflow(t)
	agentID := seededReadyAgentID(t)
	quietProject := createWorkflowTestProject(t, "Workflow quiet project")
	quiet := dbfx.Issue(t, "no handoffs", testutil.Cols{"project_id": quietProject, "status": "todo"})
	quietIssue, _ := testHandler.Queries.GetIssue(context.Background(), util.MustParseUUID(quiet))
	if testHandler.claimProjectWorkflow(context.Background(), quietIssue, nil, false) != nil {
		t.Fatal("a Default project without handoffs must not carry a workflow")
	}

	projectID := deliveryProject(t, "Workflow claim project", agentID)
	issueID := dbfx.Issue(t, "claim context", testutil.Cols{
		"project_id": projectID, "status": "in_progress", "assignee_type": "agent", "assignee_id": agentID,
	})

	// The assignee's run gets the step's instructions and its commands.
	brief := claimBriefForTest(t, issueID, db.AgentTaskQueue{AgentID: util.MustParseUUID(agentID)})
	for _, want := range []string{
		"This project's workflow — Default (Workflow claim project)",
		"Implement it and open a PR.",
		"   ← current",
		"Step done      → multica issue status",
	} {
		if !strings.Contains(brief, want) {
			t.Fatalf("assignee brief is missing %q:\n%s", want, brief)
		}
	}

	// Any other agent on the issue — mentioned in a comment, say — gets the
	// statuses and is told to leave them to the assignee.
	other := claimBriefForTest(t, issueID, db.AgentTaskQueue{AgentID: util.MustParseUUID(dbfx.Agent(t, "Workflow observer", handlerTestRuntimeID(t)))})
	if !strings.Contains(other, "You are not its assignee") || strings.Contains(other, "Step done") || strings.Contains(other, "Implement it and open a PR.") {
		t.Fatalf("observer brief must leave the step to its assignee:\n%s", other)
	}

	plain := dbfx.Issue(t, "no workflow", testutil.Cols{"status": "todo"})
	plainIssue, _ := testHandler.Queries.GetIssue(context.Background(), util.MustParseUUID(plain))
	if testHandler.claimProjectWorkflow(context.Background(), plainIssue, nil, false) != nil {
		t.Fatal("an issue outside a project must not carry a workflow")
	}
}

// claimBriefForTest renders the workflow brief a claim of task would carry
// for the issue as it is now.
func claimBriefForTest(t *testing.T, issueID string, task db.AgentTaskQueue) string {
	t.Helper()
	issue, err := testHandler.Queries.GetIssue(context.Background(), util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	got := testHandler.claimProjectWorkflow(context.Background(), issue, &task, false)
	if got == nil {
		t.Fatalf("claim of %s carries no workflow", issueID)
	}
	return got.Brief
}

func TestPreviewWorkflowHandoffNamesHandlerRunsAndBrief(t *testing.T) {
	seedTestCatalog(t)
	agentID := seededReadyAgentID(t)
	projectID := deliveryProject(t, "Workflow preview project", agentID)
	issueID := dbfx.Issue(t, "preview me", testutil.Cols{"project_id": projectID, "status": "todo"})

	call := func(status string) *testutil.Response {
		req := withURLParam(newRequest("GET", "/api/issues/"+issueID+"/workflow-handoff?status="+status, nil), "id", issueID)
		return testutil.Call(t, testHandler.PreviewWorkflowHandoff, req)
	}
	var got WorkflowHandoffPreviewResponse
	call("in_progress").Want(http.StatusOK).JSON(&got)
	if !got.Handoff || got.HandlerType != "agent" || got.HandlerID != agentID || got.WorkflowName != "Default" {
		t.Fatalf("preview = %+v, want a handoff to the column's agent", got)
	}
	if !strings.Contains(got.Brief, "Implement it and open a PR.") {
		t.Fatalf("preview brief is missing the step instructions:\n%s", got.Brief)
	}
	if status, _, _ := issueStatusAndAssignee(t, issueID); status != "todo" {
		t.Fatalf("a preview moved the issue to %q", status)
	}

	var manual WorkflowHandoffPreviewResponse
	call("done").Want(http.StatusOK).JSON(&manual)
	if manual.Handoff {
		t.Fatalf("a column without a handoff previewed one: %+v", manual)
	}
}

func TestPreviewIssueWorkflowBriefUsesTheDraft(t *testing.T) {
	seedTestCatalog(t)
	agentID := seededReadyAgentID(t)
	projectID := createWorkflowTestProject(t, "Workflow brief project")
	workflowID := projectWorkflowID(t, projectID)
	var got struct {
		Brief string `json:"brief"`
	}
	preview := func(body map[string]any) *testutil.Response {
		req := withURLParam(workflowRequest("POST", "/api/issue-workflows/"+workflowID+"/brief", body), "id", workflowID)
		return testutil.Call(t, testHandler.PreviewIssueWorkflowBrief, req)
	}
	preview(map[string]any{
		"status_key": "in_progress", "handler": map[string]any{"type": "agent", "id": agentID}, "instructions": "Draft step.",
		"project_id": projectID,
	}).Want(http.StatusOK).JSON(&got)
	for _, want := range []string{"Default (Workflow brief project)", "Draft step.", "← current", "in_review\nCan't proceed"} {
		if !strings.Contains(got.Brief, want) {
			t.Fatalf("draft brief is missing %q:\n%s", want, got.Brief)
		}
	}
	preview(map[string]any{"status_key": "no_such_column"}).Want(http.StatusBadRequest)
}

func TestPreviewIssueWorkflowBriefRejectsMemberOutsideWorkspace(t *testing.T) {
	seedTestCatalog(t)
	projectID := createWorkflowTestProject(t, "Workflow brief member project")
	workflowID := projectWorkflowID(t, projectID)
	preview := func(userID string) *testutil.Response {
		body := map[string]any{"status_key": "in_review", "handler": map[string]any{"type": "member", "id": userID}}
		req := withURLParam(workflowRequest("POST", "/api/issue-workflows/"+workflowID+"/brief", body), "id", workflowID)
		return testutil.Call(t, testHandler.PreviewIssueWorkflowBrief, req)
	}

	const outsiderName = "Workflow Brief Outsider"
	outsiderID := dbfx.User(t, outsiderName, fmt.Sprintf("brief-outsider-%d@example.com", time.Now().UnixNano()))
	resp := preview(outsiderID).Want(http.StatusBadRequest)
	if strings.Contains(resp.Body.String(), outsiderName) {
		t.Fatalf("preview leaked the name of a user outside the workspace: %s", resp.Body.String())
	}

	const memberName = "Workflow Brief Member"
	memberID := dbfx.User(t, memberName, fmt.Sprintf("brief-member-%d@example.com", time.Now().UnixNano()))
	dbfx.Member(t, testWorkspaceID, memberID, "member")
	var got struct {
		Brief string `json:"brief"`
	}
	preview(memberID).Want(http.StatusOK).JSON(&got)
	if !strings.Contains(got.Brief, memberName) {
		t.Fatalf("brief does not name the workspace member:\n%s", got.Brief)
	}
}

// agentUpdateIssue changes an issue as the agent running taskID, the way a
// task token reaches the handler.
func agentUpdateIssue(t *testing.T, issueID, agentID, taskID string, body map[string]any) *testutil.Response {
	t.Helper()
	req := withURLParam(newRequest("PUT", "/api/issues/"+issueID, body), "id", issueID)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	return testutil.Call(t, testHandler.UpdateIssue, req)
}

func taskWorkflowStep(t *testing.T, taskID string) string {
	t.Helper()
	var step *string
	dbfx.QueryRow(t, `SELECT workflow_step FROM agent_task_queue WHERE id = $1`, taskID).Scan(&step)
	if step == nil {
		return ""
	}
	return *step
}

// The validation report: an agent handling two adjacent columns moves the
// issue into the second from its own run. Entering the step is new work, so a
// run for it is queued although the agent is still running.
func TestSameAgentNextStepQueuesARun(t *testing.T) {
	seedTestCatalog(t)
	agentID := seededReadyAgentID(t)
	projectID := deliveryProject(t, "Workflow same agent project", agentID)
	setColumnHandoff(t, projectID, "in_review", handToAgent(agentID, "Review your own PR.")).Want(http.StatusOK)
	issueID := dbfx.Issue(t, "same agent twice", testutil.Cols{"project_id": projectID, "status": "todo"})

	updateIssueForTest(t, issueID, map[string]any{"status": "in_progress"}).Want(http.StatusOK)
	var taskID string
	dbfx.QueryRow(t, `SELECT id::text FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`, issueID, agentID).Scan(&taskID)
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, taskID)

	agentUpdateIssue(t, issueID, agentID, taskID, map[string]any{"status": "in_review"}).Want(http.StatusOK)
	var queued int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`, issueID, agentID).Scan(&queued)
	if queued != 1 {
		t.Fatalf("queued runs for the next step = %d, want one", queued)
	}
	if brief := claimBriefForTest(t, issueID, db.AgentTaskQueue{AgentID: util.MustParseUUID(agentID)}); !strings.Contains(brief, "Review your own PR.") {
		t.Fatalf("the next step's run would not read its instructions:\n%s", brief)
	}
}

// A run works on the step the issue was at when it was queued. Once someone
// else moves the issue, the run's status changes are refused — they were
// decided for a step the issue has left — and the refusal names the move.
func TestAgentStatusChangeRefusedAfterIssueMoved(t *testing.T) {
	seedTestCatalog(t)
	agentID := seededReadyAgentID(t)
	projectID := deliveryProject(t, "Workflow stale step project", agentID)
	issueID := dbfx.Issue(t, "stale step", testutil.Cols{"project_id": projectID, "status": "todo"})

	updateIssueForTest(t, issueID, map[string]any{"status": "in_progress"}).Want(http.StatusOK)
	var taskID string
	dbfx.QueryRow(t, `SELECT id::text FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`, issueID, agentID).Scan(&taskID)
	if got := taskWorkflowStep(t, taskID); got != "in_progress" {
		t.Fatalf("handoff run step = %q, want in_progress", got)
	}

	// The run's own move is allowed and carries its step along.
	agentUpdateIssue(t, issueID, agentID, taskID, map[string]any{"status": "in_review"}).Want(http.StatusOK)
	if got := taskWorkflowStep(t, taskID); got != "in_review" {
		t.Fatalf("step after the run's own move = %q, want in_review", got)
	}

	// A member parks the issue while the run is still going. The activity row
	// is what the server's listener records for that move; handler tests run
	// without the listeners.
	updateIssueForTest(t, issueID, map[string]any{"status": "blocked"}).Want(http.StatusOK)
	dbfx.Exec(t, `INSERT INTO activity_log (workspace_id, issue_id, actor_type, actor_id, action, details)
		VALUES ($1, $2, 'member', $3, 'status_changed', '{"from":"in_review","to":"blocked"}')`, testWorkspaceID, issueID, testUserID)

	body := agentUpdateIssue(t, issueID, agentID, taskID, map[string]any{"status": "done"}).Want(http.StatusConflict).Map()
	if body["code"] != "workflow_step_moved" || body["step"] != "in_review" || body["current_status"] != "blocked" {
		t.Fatalf("refusal = %v", body)
	}
	var memberName string
	dbfx.QueryRow(t, `SELECT name FROM "user" WHERE id = $1`, testUserID).Scan(&memberName)
	msg, _ := body["error"].(string)
	for _, want := range []string{memberName + " moved it to", `"blocked"`, "not applied"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("refusal %q is missing %q", msg, want)
		}
	}
	if status, _, _ := issueStatusAndAssignee(t, issueID); status != "blocked" {
		t.Fatalf("status = %q, the refused change must not apply", status)
	}

	// Anything but a status change still goes through.
	agentUpdateIssue(t, issueID, agentID, taskID, map[string]any{"title": "stale step, noted"}).Want(http.StatusOK)
	agentUpdateIssue(t, issueID, agentID, taskID, map[string]any{"status": "blocked"}).Want(http.StatusOK)

	// A run queued after the move works on the new step.
	var runtimeID string
	dbfx.QueryRow(t, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID)
	fresh := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id": issueID, "runtime_id": runtimeID, "status": "running", "started_at": time.Now(), "workflow_step": "blocked",
	})
	agentUpdateIssue(t, issueID, agentID, fresh, map[string]any{"status": "in_review"}).Want(http.StatusOK)
}

// A project on Default with no handoffs keeps the pre-workflow behavior: a run
// may still change the status after someone else moved the issue.
func TestAgentStatusChangeOnDefaultIgnoresStep(t *testing.T) {
	seedTestCatalog(t)
	agentID := seededReadyAgentID(t)
	projectID := createWorkflowTestProject(t, "Workflow default step project")
	issueID := dbfx.Issue(t, "default step", testutil.Cols{"project_id": projectID, "status": "blocked"})
	var runtimeID string
	dbfx.QueryRow(t, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id": issueID, "runtime_id": runtimeID, "status": "running", "started_at": time.Now(), "workflow_step": "todo",
	})

	agentUpdateIssue(t, issueID, agentID, taskID, map[string]any{"status": "in_review"}).Want(http.StatusOK)
}

// Squads can't be handed to, but a handoff away from an issue a squad owns
// offers to stop — and stops — the runs made on
// the squad's behalf, recording who stopped them.
func TestHandoffStopsPreviousSquadRuns(t *testing.T) {
	seedTestCatalog(t)
	agentID := seededReadyAgentID(t)
	squadID := dbfx.Squad(t, "Workflow review squad "+workflowTestSuffix(), agentID)
	projectID := createWorkflowTestProject(t, "Workflow squad stop project")
	setColumnHandoff(t, projectID, "in_progress", map[string]any{"handler": map[string]any{"type": "member", "id": testUserID}}).Want(http.StatusOK)
	setColumnHandoff(t, projectID, "in_review", map[string]any{"handler": map[string]any{"type": "squad", "id": squadID}}).Want(http.StatusBadRequest)
	issueID := dbfx.Issue(t, "squad reviewing", testutil.Cols{
		"project_id": projectID, "status": "in_review", "assignee_type": "squad", "assignee_id": squadID,
	})
	var runtimeID string
	dbfx.QueryRow(t, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID)
	running := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id": issueID, "squad_id": squadID, "runtime_id": runtimeID, "status": "running", "started_at": time.Now(),
	})

	var preview WorkflowHandoffPreviewResponse
	req := withURLParam(newRequest("GET", "/api/issues/"+issueID+"/workflow-handoff?status=in_progress", nil), "id", issueID)
	testutil.Call(t, testHandler.PreviewWorkflowHandoff, req).Want(http.StatusOK).JSON(&preview)
	if len(preview.PreviousRuns) != 1 || preview.PreviousRuns[0].TaskID != running {
		t.Fatalf("preview runs = %+v, want the squad's run", preview.PreviousRuns)
	}

	updateIssueForTest(t, issueID, map[string]any{"status": "in_progress", "stop_previous_assignee_runs": true}).Want(http.StatusOK)
	var status string
	var stoppedBy *string
	dbfx.QueryRow(t, `SELECT status, cancelled_by_name FROM agent_task_queue WHERE id = $1`, running).Scan(&status, &stoppedBy)
	var memberName string
	dbfx.QueryRow(t, `SELECT name FROM "user" WHERE id = $1`, testUserID).Scan(&memberName)
	if status != "cancelled" || stoppedBy == nil || *stoppedBy != memberName {
		t.Fatalf("squad run = %s stopped by %v, want cancelled by %s", status, stoppedBy, memberName)
	}
}

// Remapping an issue into a done status on a workflow switch ends its wakeups,
// as any status write into done does.
func TestWorkflowSwitchIntoDoneEndsWakeups(t *testing.T) {
	seedTestCatalog(t)
	resetDefaultWorkflow(t)
	agentID := seededReadyAgentID(t)
	projectID := createWorkflowTestProject(t, "Workflow switch wakeups project")
	issueID := dbfx.Issue(t, "wakeups end with the issue", testutil.Cols{"project_id": projectID, "status": "in_review"})
	svc := service.IssueWakeupService{Tasks: testHandler.TaskService}
	wakeup, err := svc.Create(context.Background(), parseUUID(issueID), parseUUID(testUserID), pgtype.UUID{},
		service.WakeupInput{AgentID: agentID, Kind: "at", AfterSeconds: 600, Instruction: "check the PR"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue_wakeup WHERE issue_id = $1`, issueID) })
	var runtimeID string
	dbfx.QueryRow(t, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID)
	pending := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id": issueID, "runtime_id": runtimeID, "status": "queued",
		"context": testutil.Raw("jsonb_build_object('wakeup_id', '" + uuidToString(wakeup.ID) + "')"),
	})

	wf := createTestWorkflow(t, "Wakeup switch "+workflowTestSuffix())
	setProjectWorkflow(t, projectID, map[string]any{
		"workflow_id": wf.ID, "status_mapping": map[string]string{"in_review": "done"},
	}).Want(http.StatusOK)

	var enabled bool
	var taskStatus string
	dbfx.QueryRow(t, `SELECT enabled FROM issue_wakeup WHERE id = $1`, uuidToString(wakeup.ID)).Scan(&enabled)
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, pending).Scan(&taskStatus)
	if enabled || taskStatus != "cancelled" {
		t.Fatalf("after the switch into done: wakeup enabled=%v, pending wakeup run %s", enabled, taskStatus)
	}
}

// A write that hands an issue to an agent delivers the change to that agent
// as its run, so the agent's event wakeups skip it; other agents' wakeups and
// the platform's own rules (which have no agent) still capture it. Holds for
// workflow handoffs and plain assignment alike.
func TestHandoffDoesNotAlsoWakeTheHandler(t *testing.T) {
	seedTestCatalog(t)
	handler := seededReadyAgentID(t)
	var runtimeID string
	dbfx.QueryRow(t, `SELECT runtime_id::text FROM agent WHERE id = $1`, handler).Scan(&runtimeID)
	watcher := dbfx.Agent(t, "Workflow wakeup watcher "+workflowTestSuffix(), runtimeID)
	projectID := deliveryProject(t, "Workflow wakeup dedupe project", handler)
	svc := service.IssueWakeupService{Tasks: testHandler.TaskService}
	subscribe := func(issueID, agentID string) string {
		t.Helper()
		w, err := svc.Create(context.Background(), parseUUID(issueID), parseUUID(testUserID), pgtype.UUID{}, service.WakeupInput{
			AgentID: agentID, Kind: "event", EventTypes: []string{"issue.status_changed", "issue.assignee_changed"}, Instruction: "look again",
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM issue_wakeup_receipt WHERE wakeup_id = $1`, w.ID)
		})
		return uuidToString(w.ID)
	}
	pending := func(wakeupID string) int {
		t.Helper()
		var n int
		dbfx.QueryRow(t, `SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id = $1 AND processed_at IS NULL`, wakeupID).Scan(&n)
		return n
	}

	handedOff := dbfx.Issue(t, "handed off", testutil.Cols{"project_id": projectID, "status": "todo"})
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue_wakeup WHERE issue_id = $1`, handedOff) })
	handlerWakeup, watcherWakeup := subscribe(handedOff, handler), subscribe(handedOff, watcher)
	platformRule := func(issueID string) string {
		t.Helper()
		var id string
		dbfx.QueryRow(t, `INSERT INTO issue_wakeup (id, workspace_id, issue_id, instruction, kind, mode, event_types, system_rule)
			VALUES (gen_random_uuid(), $1, $2, 'stage closed', 'event', 'continuous', ARRAY['issue.status_changed'], 'child_done')
			RETURNING id::text`, testWorkspaceID, issueID).Scan(&id)
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM issue_wakeup_receipt WHERE wakeup_id = $1`, id)
		})
		return id
	}
	handedOffRule := platformRule(handedOff)
	updateIssueForTest(t, handedOff, map[string]any{"status": "in_progress"}).Want(http.StatusOK)
	if n := pending(handlerWakeup); n != 0 {
		t.Fatalf("the handoff also woke its handler: %d receipt(s)", n)
	}
	if n := pending(watcherWakeup); n == 0 {
		t.Fatal("another agent's wakeup missed the handoff")
	}
	if n := pending(handedOffRule); n == 0 {
		t.Fatal("the platform rule missed the handoff")
	}

	parked := dbfx.Issue(t, "parked without a handoff", testutil.Cols{"status": "todo"})
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue_wakeup WHERE issue_id = $1`, parked) })
	parkedRule := platformRule(parked)
	updateIssueForTest(t, parked, map[string]any{"status": "backlog"}).Want(http.StatusOK)
	if n := pending(parkedRule); n == 0 {
		t.Fatal("the platform rule missed a status change that handed the issue to nobody")
	}

	assigned := dbfx.Issue(t, "assigned", testutil.Cols{"status": "todo"})
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue_wakeup WHERE issue_id = $1`, assigned) })
	handlerWakeup, watcherWakeup = subscribe(assigned, handler), subscribe(assigned, watcher)
	updateIssueForTest(t, assigned, map[string]any{"assignee_type": "agent", "assignee_id": handler}).Want(http.StatusOK)
	if n := pending(handlerWakeup); n != 0 {
		t.Fatalf("assignment also woke the assignee: %d receipt(s)", n)
	}
	if n := pending(watcherWakeup); n == 0 {
		t.Fatal("another agent's wakeup missed the assignment")
	}
}

func TestDescriptionUpdatePreservesWorkflowHandoff(t *testing.T) {
	seedTestCatalog(t)
	targetAgent := seededReadyAgentID(t)
	sourceAgent := dbfx.Agent(t, "Description handoff Agent A", handlerTestRuntimeID(t))
	projectID := deliveryProject(t, "Description handoff project", targetAgent)
	setColumnHandoff(t, projectID, "in_review", handToAgent(targetAgent, "Process the saved description.")).Want(http.StatusOK)
	issueID := dbfx.Issue(t, "Saved result handoff", testutil.Cols{
		"project_id": projectID, "status": "in_progress", "assignee_type": "agent", "assignee_id": sourceAgent,
	})

	updateIssueForTest(t, issueID, map[string]any{"status": "in_review", "description": "Updated description."}).Want(http.StatusOK)
	_, kind, id := issueStatusAndAssignee(t, issueID)
	if kind != "agent" || id != targetAgent {
		t.Fatalf("saved assignee = %s/%s, want workflow-selected agent %s", kind, id, targetAgent)
	}
	if n := taskCountFor(t, issueID, targetAgent); n != 1 {
		t.Fatalf("target agent runs = %d, want one without any follow-up comment", n)
	}
	if n := taskCountFor(t, issueID, sourceAgent); n != 0 {
		t.Fatalf("source agent runs = %d, want none", n)
	}
	issue, err := testHandler.Queries.GetIssue(context.Background(), util.MustParseUUID(issueID))
	if err != nil || issue.Description.String != "Updated description." {
		t.Fatalf("saved result = %q, err = %v", issue.Description.String, err)
	}
	updateIssueForTest(t, issueID, map[string]any{"status": "in_review", "description": "Updated description."}).Want(http.StatusOK)
	if n := taskCountFor(t, issueID, targetAgent); n != 1 {
		t.Fatalf("unchanged-status retry created %d target agent runs, want one", n)
	}
}

func TestBatchDescriptionUpdatePreservesWorkflowHandoff(t *testing.T) {
	seedTestCatalog(t)
	targetAgent := seededReadyAgentID(t)
	sourceAgent := dbfx.Agent(t, "Batch description handoff Agent A", handlerTestRuntimeID(t))
	projectID := deliveryProject(t, "Batch description handoff project", targetAgent)
	setColumnHandoff(t, projectID, "in_review", handToAgent(targetAgent, "Process the saved description.")).Want(http.StatusOK)
	issueID := dbfx.Issue(t, "Batch saved result handoff", testutil.Cols{
		"project_id": projectID, "status": "in_progress", "assignee_type": "agent", "assignee_id": sourceAgent,
	})
	testutil.Call(t, testHandler.BatchUpdateIssues, workflowRequest(http.MethodPost, "/api/issues/batch", map[string]any{
		"issue_ids": []string{issueID}, "updates": map[string]any{"status": "in_review", "description": "Batch updated description."},
	})).Want(http.StatusOK)
	_, kind, id := issueStatusAndAssignee(t, issueID)
	if kind != "agent" || id != targetAgent || taskCountFor(t, issueID, targetAgent) != 1 || taskCountFor(t, issueID, sourceAgent) != 0 {
		t.Fatalf("batch saved assignee = %s/%s; want target agent %s and one target-agent-only run", kind, id, targetAgent)
	}
}

func TestDescriptionHandoffExplicitMemberOverride(t *testing.T) {
	seedTestCatalog(t)
	targetAgent := seededReadyAgentID(t)
	projectID := deliveryProject(t, "Description explicit member project", targetAgent)
	setColumnHandoff(t, projectID, "in_review", handToAgent(targetAgent, "Process the saved description.")).Want(http.StatusOK)
	issueID := dbfx.Issue(t, "Explicit member assignment", testutil.Cols{"project_id": projectID, "status": "in_progress"})
	updateIssueForTest(t, issueID, map[string]any{
		"status": "in_review", "description": "Updated description for the assigned member.", "assignee_type": "member", "assignee_id": testUserID,
	}).Want(http.StatusOK)
	_, kind, id := issueStatusAndAssignee(t, issueID)
	if kind != "member" || id != testUserID || taskCountFor(t, issueID, targetAgent) != 0 {
		t.Fatalf("explicit member overridden: assignee=%s/%s", kind, id)
	}
}
