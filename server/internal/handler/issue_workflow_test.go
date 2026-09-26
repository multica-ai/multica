package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
)

// Project workflow tests (MUL-7420).

// workflowTestSuffix keeps names unique across tests sharing the workspace.
func workflowTestSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func workflowRequest(method, path string, body any) *http.Request {
	return newRequest(method, path+"?workspace_id="+testWorkspaceID, body)
}

// createTestWorkflow creates a workflow through the API and deletes it after
// the test, detaching any project still using it first.
func createTestWorkflow(t *testing.T, name, initial string, steps []map[string]any) IssueWorkflowResponse {
	t.Helper()
	withFeatureFlag(t, testHandler, featureflags.ProjectWorkflowsV1, true)
	var created IssueWorkflowResponse
	testutil.Call(t, testHandler.CreateIssueWorkflow, workflowRequest("POST", "/api/issue-workflows", map[string]any{
		"name":               name,
		"initial_status_key": initial,
		"steps":              steps,
	})).Want(http.StatusCreated).JSON(&created)
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `UPDATE project SET workflow_id = NULL WHERE workflow_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM issue_workflow WHERE id = $1`, created.ID)
	})
	return created
}

func createWorkflowTestProject(t *testing.T, title string) string {
	t.Helper()
	projectID := dbfx.Project(t, title)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE project_id = $1`, projectID)
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})
	return projectID
}

func setProjectWorkflow(t *testing.T, projectID string, body map[string]any) *testutil.Response {
	t.Helper()
	req := withURLParam(workflowRequest("POST", "/api/projects/"+projectID+"/workflow", body), "id", projectID)
	return testutil.Call(t, testHandler.SetProjectWorkflow, req)
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

// deliveryWorkflowSteps is the example from the design: the agent implements,
// then the project lead reviews.
func deliveryWorkflowSteps(agentID string) []map[string]any {
	return []map[string]any{
		{"status_key": "todo", "handler": map[string]any{"type": "none"}},
		{"status_key": "in_progress", "handler": map[string]any{"type": "agent", "id": agentID},
			"instructions": "Implement it and open a PR.", "next_status_key": "in_review"},
		{"status_key": "in_review", "handler": map[string]any{"type": "none"},
			"next_status_key": "done", "back_status_key": "in_progress"},
		{"status_key": "done", "handler": map[string]any{"type": "none"}},
	}
}

func TestCreateIssueWorkflowRequiresFlag(t *testing.T) {
	withFeatureFlag(t, testHandler, featureflags.ProjectWorkflowsV1, false)
	testutil.Call(t, testHandler.CreateIssueWorkflow, workflowRequest("POST", "/api/issue-workflows", map[string]any{
		"name":               "Flag off " + workflowTestSuffix(),
		"initial_status_key": "todo",
		"steps":              []map[string]any{{"status_key": "todo"}},
	})).Want(http.StatusForbidden)
}

func TestCreateIssueWorkflowValidates(t *testing.T) {
	seedTestCatalog(t)
	withFeatureFlag(t, testHandler, featureflags.ProjectWorkflowsV1, true)
	name := "Validate " + workflowTestSuffix()
	created := createTestWorkflow(t, name, "todo", []map[string]any{{"status_key": "todo"}, {"status_key": "done"}})
	if len(created.Steps) != 2 || created.Steps[0].Handler.Type != "none" {
		t.Fatalf("created steps = %+v, want two manual steps", created.Steps)
	}

	for label, body := range map[string]map[string]any{
		"unknown status": {"name": "x" + workflowTestSuffix(), "initial_status_key": "todo",
			"steps": []map[string]any{{"status_key": "todo"}, {"status_key": "no_such_status"}}},
		"initial not a step": {"name": "y" + workflowTestSuffix(), "initial_status_key": "backlog",
			"steps": []map[string]any{{"status_key": "todo"}}},
		"reserved name": {"name": "Default", "initial_status_key": "todo",
			"steps": []map[string]any{{"status_key": "todo"}}},
	} {
		t.Run(label, func(t *testing.T) {
			testutil.Call(t, testHandler.CreateIssueWorkflow, workflowRequest("POST", "/api/issue-workflows", body)).
				Want(http.StatusBadRequest)
		})
	}
	testutil.Call(t, testHandler.CreateIssueWorkflow, workflowRequest("POST", "/api/issue-workflows", map[string]any{
		"name": strings.ToUpper(name), "initial_status_key": "todo", "steps": []map[string]any{{"status_key": "todo"}},
	})).Want(http.StatusConflict)
}

func TestSetProjectWorkflowMapsOnlyUnlistedStatuses(t *testing.T) {
	seedTestCatalog(t)
	agentID := seededReadyAgentID(t)
	wf := createTestWorkflow(t, "Map "+workflowTestSuffix(), "todo", []map[string]any{
		{"status_key": "todo"},
		{"status_key": "in_review", "handler": map[string]any{"type": "agent", "id": agentID}},
		{"status_key": "done"},
	})
	projectID := createWorkflowTestProject(t, "Workflow map project")
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
	// in_progress has no started-category step but in_review does.
	if got := dry.Plan.Required[0].SuggestedStatusKey; got != "in_review" {
		t.Fatalf("suggested target = %q, want the same-category step in_review", got)
	}

	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": wf.ID}).Want(http.StatusConflict)
	setProjectWorkflow(t, projectID, map[string]any{
		"workflow_id":    wf.ID,
		"status_mapping": map[string]string{"in_progress": "backlog"},
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
	// Mapping is administrative: entering a handoff step this way neither
	// reassigns the issue nor starts a run.
	if assigneeType != "" || taskCountFor(t, moved, agentID) != 0 {
		t.Fatalf("a workflow switch handed the issue off (assignee type %q)", assigneeType)
	}

	// Switching back to Default needs no mapping and works with the flag off.
	withFeatureFlag(t, testHandler, featureflags.ProjectWorkflowsV1, false)
	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": nil}).Want(http.StatusOK)
}

func TestUpdateIssueRejectsStatusOutsideWorkflow(t *testing.T) {
	seedTestCatalog(t)
	wf := createTestWorkflow(t, "Reject "+workflowTestSuffix(), "todo", []map[string]any{{"status_key": "todo"}, {"status_key": "done"}})
	projectID := createWorkflowTestProject(t, "Workflow reject project")
	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": wf.ID}).Want(http.StatusOK)
	issueID := dbfx.Issue(t, "reject blocked", testutil.Cols{"project_id": projectID, "status": "todo"})

	body := updateIssueForTest(t, issueID, map[string]any{"status": "blocked"}).Want(http.StatusBadRequest).Map()
	if body["code"] != "status_not_in_workflow" {
		t.Fatalf("code = %v, want status_not_in_workflow", body["code"])
	}
	if msg, _ := body["error"].(string); !strings.Contains(msg, "todo, done") {
		t.Fatalf("error %q should list the allowed keys", msg)
	}
	updateIssueForTest(t, issueID, map[string]any{"status": "done"}).Want(http.StatusOK)
}

func TestStatusChangeHandsOffToStepHandler(t *testing.T) {
	seedTestCatalog(t)
	agentID := seededReadyAgentID(t)
	wf := createTestWorkflow(t, "Handoff "+workflowTestSuffix(), "todo", deliveryWorkflowSteps(agentID))
	projectID := createWorkflowTestProject(t, "Workflow handoff project")
	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": wf.ID}).Want(http.StatusOK)
	issueID := dbfx.Issue(t, "hand me off", testutil.Cols{"project_id": projectID, "status": "todo"})

	updateIssueForTest(t, issueID, map[string]any{"status": "in_progress"}).Want(http.StatusOK)

	_, assigneeType, assigneeID := issueStatusAndAssignee(t, issueID)
	if assigneeType != "agent" || assigneeID != agentID {
		t.Fatalf("assignee = %s/%s, want the step's agent %s", assigneeType, assigneeID, agentID)
	}
	tasks, err := testHandler.Queries.ListTasksByIssue(context.Background(), util.MustParseUUID(issueID))
	if err != nil || len(tasks) != 1 {
		t.Fatalf("tasks = %d (%v), want one handoff run", len(tasks), err)
	}
	note := tasks[0].HandoffNote.String
	for _, want := range []string{"Implement it and open a PR.", "`in_review`", "Step done      → multica issue status"} {
		if !strings.Contains(note, want) {
			t.Fatalf("handoff note is missing %q:\n%s", want, note)
		}
	}

	// A caller-written note replaces the step's brief.
	back := dbfx.Issue(t, "explicit note", testutil.Cols{"project_id": projectID, "status": "todo"})
	updateIssueForTest(t, back, map[string]any{"status": "in_progress", "handoff_note": "Only the login flow."}).Want(http.StatusOK)
	backTasks, _ := testHandler.Queries.ListTasksByIssue(context.Background(), util.MustParseUUID(back))
	if len(backTasks) != 1 || backTasks[0].HandoffNote.String != "Only the login flow." {
		t.Fatalf("explicit handoff note was not kept: %+v", backTasks)
	}
}

func TestExplicitAssigneeWinsOverStepHandler(t *testing.T) {
	seedTestCatalog(t)
	agentID := seededReadyAgentID(t)
	wf := createTestWorkflow(t, "Explicit "+workflowTestSuffix(), "todo", deliveryWorkflowSteps(agentID))
	projectID := createWorkflowTestProject(t, "Workflow explicit project")
	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": wf.ID}).Want(http.StatusOK)
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
	agentID := seededReadyAgentID(t)
	wf := createTestWorkflow(t, "Move "+workflowTestSuffix(), "todo", []map[string]any{
		{"status_key": "todo"},
		{"status_key": "in_review", "handler": map[string]any{"type": "agent", "id": agentID}},
		{"status_key": "done"},
	})
	projectID := createWorkflowTestProject(t, "Workflow move project")
	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": wf.ID}).Want(http.StatusOK)
	issueID := dbfx.Issue(t, "moving in", testutil.Cols{"status": "in_progress"})

	updateIssueForTest(t, issueID, map[string]any{"project_id": projectID}).Want(http.StatusOK)
	status, assigneeType, _ := issueStatusAndAssignee(t, issueID)
	if status != "in_review" {
		t.Fatalf("status after move = %q, want the same-category step in_review", status)
	}
	if assigneeType != "" || taskCountFor(t, issueID, agentID) != 0 {
		t.Fatal("moving an issue into a project must not hand it off")
	}
}

func TestCreateIssueInWorkflowProjectStartsAtInitialStep(t *testing.T) {
	seedTestCatalog(t)
	agentID := seededReadyAgentID(t)
	wf := createTestWorkflow(t, "Create "+workflowTestSuffix(), "in_progress", deliveryWorkflowSteps(agentID))
	projectID := createWorkflowTestProject(t, "Workflow create project")
	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": wf.ID}).Want(http.StatusOK)

	created := createIssueForTest(t, map[string]any{"title": "starts in the workflow", "project_id": projectID})
	if created.Status != "in_progress" {
		t.Fatalf("status = %q, want the workflow's starting status", created.Status)
	}
	if created.AssigneeID == nil || *created.AssigneeID != agentID {
		t.Fatalf("assignee = %v, want the starting step's agent", created.AssigneeID)
	}
	tasks, _ := testHandler.Queries.ListTasksByIssue(context.Background(), util.MustParseUUID(created.ID))
	if len(tasks) != 1 || !strings.Contains(tasks[0].HandoffNote.String, "Implement it and open a PR.") {
		t.Fatalf("create did not start the step's run with its brief: %+v", tasks)
	}

	w := testutil.Call(t, testHandler.CreateIssue, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "outside the workflow", "project_id": projectID, "status": "blocked",
	}))
	w.Want(http.StatusBadRequest)
}

func TestArchiveStatusUsedByWorkflowIsRejected(t *testing.T) {
	entry := createTestCustomStatus(t, "wf_guard_"+workflowTestSuffix()[10:], "started")
	createTestWorkflow(t, "Guard "+workflowTestSuffix(), "todo", []map[string]any{{"status_key": "todo"}, {"status_key": entry.Key}})

	req := withURLParam(newRequest("DELETE", "/api/issue-statuses/"+util.UUIDToString(entry.ID), nil), "id", util.UUIDToString(entry.ID))
	body := testutil.Call(t, testHandler.ArchiveIssueStatus, req).Want(http.StatusConflict).Map()
	if body["code"] != "issue_status_in_workflow" {
		t.Fatalf("code = %v, want issue_status_in_workflow", body["code"])
	}
}

func TestClaimCarriesProjectWorkflow(t *testing.T) {
	seedTestCatalog(t)
	agentID := seededReadyAgentID(t)
	wf := createTestWorkflow(t, "Claim "+workflowTestSuffix(), "todo", deliveryWorkflowSteps(agentID))
	projectID := createWorkflowTestProject(t, "Workflow claim project")
	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": wf.ID}).Want(http.StatusOK)
	issueID := dbfx.Issue(t, "claim context", testutil.Cols{"project_id": projectID, "status": "in_progress"})

	issue, err := testHandler.Queries.GetIssue(context.Background(), util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	got := testHandler.claimProjectWorkflow(context.Background(), issue)
	if got == nil || got.CurrentStatusKey != "in_progress" || len(got.Steps) != 4 {
		t.Fatalf("claim workflow = %+v", got)
	}
	current := got.Steps[1]
	if current.Handler == "" || current.Handler == "agent" || current.Instructions == "" || current.NextStatusKey != "in_review" {
		t.Fatalf("current step = %+v, want a named handler, its instructions and next status", current)
	}
	if got.Steps[2].Instructions != "" {
		t.Fatal("only the current step's instructions ride the claim")
	}

	plain := dbfx.Issue(t, "no workflow", testutil.Cols{"status": "todo"})
	plainIssue, _ := testHandler.Queries.GetIssue(context.Background(), util.MustParseUUID(plain))
	if testHandler.claimProjectWorkflow(context.Background(), plainIssue) != nil {
		t.Fatal("an issue outside a workflow project must not carry one")
	}
}

func TestPreviewWorkflowHandoffNamesHandlerRunsAndBrief(t *testing.T) {
	seedTestCatalog(t)
	agentID := seededReadyAgentID(t)
	wf := createTestWorkflow(t, "Preview "+workflowTestSuffix(), "todo", deliveryWorkflowSteps(agentID))
	projectID := createWorkflowTestProject(t, "Workflow preview project")
	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": wf.ID}).Want(http.StatusOK)
	issueID := dbfx.Issue(t, "preview me", testutil.Cols{"project_id": projectID, "status": "todo"})

	call := func(status string) *testutil.Response {
		req := withURLParam(newRequest("GET", "/api/issues/"+issueID+"/workflow-handoff?status="+status, nil), "id", issueID)
		return testutil.Call(t, testHandler.PreviewWorkflowHandoff, req)
	}
	var got WorkflowHandoffPreviewResponse
	call("in_progress").Want(http.StatusOK).JSON(&got)
	if !got.Handoff || got.HandlerType != "agent" || got.HandlerID != agentID || got.WorkflowName != wf.Name {
		t.Fatalf("preview = %+v, want a handoff to the step's agent", got)
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
		t.Fatalf("a manual step previewed a handoff: %+v", manual)
	}
	call("blocked").Want(http.StatusBadRequest)
}

func TestPreviewIssueWorkflowBriefUsesTheDraft(t *testing.T) {
	seedTestCatalog(t)
	agentID := seededReadyAgentID(t)
	var got struct {
		Brief string `json:"brief"`
	}
	testutil.Call(t, testHandler.PreviewIssueWorkflowBrief, workflowRequest("POST", "/api/issue-workflows/preview-brief", map[string]any{
		"name": "Draft", "initial_status_key": "todo", "steps": deliveryWorkflowSteps(agentID), "status_key": "in_progress",
	})).Want(http.StatusOK).JSON(&got)
	for _, want := range []string{"This project's workflow — Draft", "Implement it and open a PR.", "← current", "Step done      →"} {
		if !strings.Contains(got.Brief, want) {
			t.Fatalf("draft brief is missing %q:\n%s", want, got.Brief)
		}
	}
	testutil.Call(t, testHandler.PreviewIssueWorkflowBrief, workflowRequest("POST", "/api/issue-workflows/preview-brief", map[string]any{
		"name": "Draft", "steps": deliveryWorkflowSteps(agentID), "status_key": "blocked",
	})).Want(http.StatusBadRequest)
}

// reviewWorkflowSteps: the agent implements, anyone reviews, and the issue can
// be parked.
func reviewWorkflowSteps(agentID string) []map[string]any {
	return []map[string]any{
		{"status_key": "todo", "handler": map[string]any{"type": "none"}},
		{"status_key": "in_progress", "handler": map[string]any{"type": "agent", "id": agentID}, "next_status_key": "in_review"},
		{"status_key": "in_review", "handler": map[string]any{"type": "none"}, "next_status_key": "done", "back_status_key": "in_progress"},
		{"status_key": "blocked", "handler": map[string]any{"type": "none"}},
		{"status_key": "done", "handler": map[string]any{"type": "none"}},
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

// A run works on the step the issue was at when it was queued. Once someone
// else moves the issue, the run's status changes are refused — they were
// decided for a step the issue has left — and the refusal names the move.
func TestAgentStatusChangeRefusedAfterIssueMoved(t *testing.T) {
	seedTestCatalog(t)
	agentID := seededReadyAgentID(t)
	wf := createTestWorkflow(t, "Stale "+workflowTestSuffix(), "todo", reviewWorkflowSteps(agentID))
	projectID := createWorkflowTestProject(t, "Workflow stale step project")
	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": wf.ID}).Want(http.StatusOK)
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

// A handoff away from a squad offers to stop — and stops — the runs made on
// the squad's behalf, recording who stopped them.
func TestHandoffStopsPreviousSquadRuns(t *testing.T) {
	seedTestCatalog(t)
	agentID := seededReadyAgentID(t)
	squadID := dbfx.Squad(t, "Workflow review squad "+workflowTestSuffix(), agentID)
	wf := createTestWorkflow(t, "Squad stop "+workflowTestSuffix(), "todo", []map[string]any{
		{"status_key": "todo", "handler": map[string]any{"type": "none"}},
		{"status_key": "in_progress", "handler": map[string]any{"type": "member", "id": testUserID}},
		{"status_key": "in_review", "handler": map[string]any{"type": "squad", "id": squadID}},
		{"status_key": "done", "handler": map[string]any{"type": "none"}},
	})
	projectID := createWorkflowTestProject(t, "Workflow squad stop project")
	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": wf.ID}).Want(http.StatusOK)
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

	wf := createTestWorkflow(t, "Wakeup switch "+workflowTestSuffix(), "todo", []map[string]any{
		{"status_key": "todo"}, {"status_key": "done"},
	})
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
// as its run, so the agent's event wakeups skip it; other agents' wakeups
// still capture it. Holds for workflow handoffs and plain assignment alike.
func TestHandoffDoesNotAlsoWakeTheHandler(t *testing.T) {
	seedTestCatalog(t)
	handler := seededReadyAgentID(t)
	var runtimeID string
	dbfx.QueryRow(t, `SELECT runtime_id::text FROM agent WHERE id = $1`, handler).Scan(&runtimeID)
	watcher := dbfx.Agent(t, "Workflow wakeup watcher "+workflowTestSuffix(), runtimeID)
	wf := createTestWorkflow(t, "Wakeup dedupe "+workflowTestSuffix(), "todo", deliveryWorkflowSteps(handler))
	projectID := createWorkflowTestProject(t, "Workflow wakeup dedupe project")
	setProjectWorkflow(t, projectID, map[string]any{"workflow_id": wf.ID}).Want(http.StatusOK)
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
	updateIssueForTest(t, handedOff, map[string]any{"status": "in_progress"}).Want(http.StatusOK)
	if n := pending(handlerWakeup); n != 0 {
		t.Fatalf("the handoff also woke its handler: %d receipt(s)", n)
	}
	if n := pending(watcherWakeup); n == 0 {
		t.Fatal("another agent's wakeup missed the handoff")
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
