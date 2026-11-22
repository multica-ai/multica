package handler

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"net/http"
	"testing"
)

func sourceTestProject(t *testing.T, body map[string]any) ProjectResponse {
	t.Helper()
	var p ProjectResponse
	testutil.Call(t, testHandler.CreateProject, newRequest("POST", "/api/projects", body)).Want(201).JSON(&p)
	t.Cleanup(func() { testPool.Exec(context.Background(), "DELETE FROM project WHERE id=$1", p.ID) })
	return p
}
func sourceTestAssign(t *testing.T, issue, agent string, decisions map[string]string, code int) {
	t.Helper()
	testutil.Call(t, testHandler.UpdateIssue, withURLParam(newRequest("PUT", "/api/issues/"+issue, map[string]any{"assignee_type": "agent", "assignee_id": agent, "source_decision": "ask", "source_decisions": decisions}), "id", issue)).Want(code)
}
func TestProjectSourceIntentPersistence(t *testing.T) {
	legacy := sourceTestProject(t, map[string]any{"title": "Unspecified source"})
	if legacy.SourceIntent != "unspecified" {
		t.Fatalf("old default = %q", legacy.SourceIntent)
	}
	p := sourceTestProject(t, map[string]any{"title": "Explicit no code", "source_intent": "no_code"})
	if p.SourceIntent != "no_code" {
		t.Fatalf("create intent = %q", p.SourceIntent)
	}
	var got ProjectResponse
	testutil.Call(t, testHandler.GetProject, withURLParam(newRequest("GET", "/api/projects/"+p.ID, nil), "id", p.ID)).Want(200).JSON(&got)
	if got.SourceIntent != "no_code" {
		t.Fatalf("readback = %q", got.SourceIntent)
	}
	testutil.Call(t, testHandler.CreateProject, newRequest("POST", "/api/projects", map[string]any{"title": "Invalid mix", "source_intent": "no_code", "resources": []any{map[string]any{"resource_type": "github_repo", "resource_ref": map[string]any{"url": "https://github.com/example/code"}}}})).Want(400)
	var resource ProjectResourceResponse
	testutil.Call(t, testHandler.CreateProjectResource, withURLParam(newRequest("POST", "/api/projects/"+p.ID+"/resources", map[string]any{"resource_type": "github_repo", "resource_ref": map[string]any{"url": "https://github.com/example/code", "ref": "release"}}), "id", p.ID)).Want(http.StatusCreated).JSON(&resource)
	testutil.Call(t, testHandler.GetProject, withURLParam(newRequest("GET", "/api/projects/"+p.ID, nil), "id", p.ID)).Want(200).JSON(&got)
	if got.SourceIntent != "resources" || got.ResourceCount != 1 {
		t.Fatalf("resource switch: %+v", got)
	}
	testutil.Call(t, testHandler.UpdateProject, withURLParam(newRequest("PUT", "/api/projects/"+p.ID, map[string]any{"source_intent": "no_code"}), "id", p.ID)).Want(400)
	testutil.Call(t, testHandler.DeleteProjectResource, withURLParams(newRequest("DELETE", "/api/projects/"+p.ID+"/resources/"+resource.ID, nil), "id", p.ID, "resourceId", resource.ID)).Want(204)
	testutil.Call(t, testHandler.GetProject, withURLParam(newRequest("GET", "/api/projects/"+p.ID, nil), "id", p.ID)).Want(200).JSON(&got)
	if got.SourceIntent != "unspecified" {
		t.Fatalf("removing last resource must restore undecided, got %q", got.SourceIntent)
	}
	testutil.Call(t, testHandler.UpdateProject, withURLParam(newRequest("PUT", "/api/projects/"+p.ID, map[string]any{"source_intent": "no_code"}), "id", p.ID)).Want(200)
	testutil.Call(t, testHandler.UpdateProject, withURLParam(newRequest("PUT", "/api/projects/"+p.ID, map[string]any{"title": "Unrelated edit"}), "id", p.ID)).Want(200).JSON(&got)
	if got.SourceIntent != "no_code" {
		t.Fatal("unrelated update lost intent")
	}
}

func TestProjectSourceChoiceBeforeAssignment(t *testing.T) {
	p := sourceTestProject(t, map[string]any{"title": "Choose source"})
	a := createHandlerTestAgent(t, "Source consent agent", []byte("[]"))
	issue := dbfx.Issue(t, "Source consent issue", testutil.Cols{"project_id": p.ID, "status": "todo"})
	sourceTestAssign(t, issue, a, nil, 409)
	var assignee pgtype.UUID
	dbfx.QueryRow(t, "SELECT assignee_id FROM issue WHERE id=$1", issue).Scan(&assignee)
	if assignee.Valid || queuedTaskCountFor(t, issue, a) != 0 {
		t.Fatal("choice required must not assign or enqueue")
	}
	sourceTestAssign(t, issue, a, map[string]string{"another-project": "no_code_once"}, 409)
	sourceTestAssign(t, issue, a, map[string]string{p.ID: "no_code_once"}, 200)
	if n := queuedTaskCountFor(t, issue, a); n != 1 {
		t.Fatalf("once enqueued %d", n)
	}
	var ctx []byte
	dbfx.QueryRow(t, "SELECT context FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2", issue, a).Scan(&ctx)
	var choice map[string]any
	json.Unmarshal(ctx, &choice)
	if choice["source_no_code"] != true {
		t.Fatalf("missing per-task choice: %s", ctx)
	}
	saved, err := testHandler.Queries.GetProjectInWorkspace(context.Background(), db.GetProjectInWorkspaceParams{ID: parseUUID(p.ID), WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil || saved.SourceIntent != "unspecified" {
		t.Fatalf("once changed project intent: %+v %v", saved, err)
	}
	second := dbfx.Issue(t, "Must choose again", testutil.Cols{"project_id": p.ID, "status": "todo"})
	sourceTestAssign(t, second, a, nil, 409)
	testutil.Call(t, testHandler.UpdateProject, withURLParam(newRequest("PUT", "/api/projects/"+p.ID, map[string]any{"source_intent": "no_code"}), "id", p.ID)).Want(200)
	sourceTestAssign(t, second, a, nil, 200)
	if queuedTaskCountFor(t, second, a) != 1 {
		t.Fatal("persistent choice did not queue exactly once")
	}
}

func TestProjectSourceBatchAdmissionHasNoPartialWrites(t *testing.T) {
	a := createHandlerTestAgent(t, "Batch source consent", []byte("[]"))
	first := sourceTestProject(t, map[string]any{"title": "Known no code", "source_intent": "no_code"})
	second := sourceTestProject(t, map[string]any{"title": "Unknown source"})
	one := dbfx.Issue(t, "Batch first", testutil.Cols{"project_id": first.ID, "status": "todo"})
	two := dbfx.Issue(t, "Batch second", testutil.Cols{"project_id": second.ID, "status": "todo"})
	body := map[string]any{"issue_ids": []string{one, two}, "updates": map[string]any{"assignee_type": "agent", "assignee_id": a, "source_decision": "ask"}}
	testutil.Call(t, testHandler.BatchUpdateIssues, newRequest("POST", "/api/issues/batch-update", body)).Want(409)
	if queuedTaskCountFor(t, one, a) != 0 || queuedTaskCountFor(t, two, a) != 0 {
		t.Fatal("batch partially queued before all decisions")
	}
	body["updates"].(map[string]any)["source_decisions"] = map[string]string{second.ID: "no_code_once"}
	testutil.Call(t, testHandler.BatchUpdateIssues, newRequest("POST", "/api/issues/batch-update", body)).Want(200)
	if queuedTaskCountFor(t, one, a) != 1 || queuedTaskCountFor(t, two, a) != 1 {
		t.Fatal("batch did not enqueue exactly once per issue")
	}
}

func TestProjectNoCodeClaimAndHistoricalSource(t *testing.T) {
	setHandlerTestWorkspaceRepos(t, []map[string]string{{"url": "https://github.com/example/workspace-default"}})
	p := sourceTestProject(t, map[string]any{"title": "No workspace fallback", "source_intent": "no_code"})
	resolved, err := testHandler.resolveClaimProjectContext(context.Background(), parseUUID(p.ID), parseUUID(testWorkspaceID))
	if err != nil || len(resolved.Repos) != 0 || !resolved.NoCode {
		t.Fatalf("no code used fallback: %+v %v", resolved, err)
	}
	a := createHandlerTestAgent(t, "History source agent", []byte("[]"))
	issue := dbfx.Issue(t, "Source history issue", testutil.Cols{"project_id": p.ID, "status": "todo"})
	taskID := dbfx.Task(t, a, testutil.Cols{"issue_id": issue, "runtime_id": testRuntimeID})
	task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := testHandler.Queries.GetAgentRuntime(context.Background(), parseUUID(testRuntimeID))
	if err != nil {
		t.Fatal(err)
	}
	resp := AgentTaskResponse{PriorSessionID: "old-code-session", PriorWorkDir: "/old/code"}
	resolved.applyTo(&resp)
	if err := testHandler.saveClaimExecutionSource(newRequest("POST", "/claim", nil), &task, &resp, runtime); err != nil {
		t.Fatal(err)
	}
	if resp.PriorSessionID != "" || resp.PriorWorkDir != "" || resp.ExecutionSource.Kind != "no_code" {
		t.Fatalf("no code reused code context: %+v", resp)
	}
	testutil.Call(t, testHandler.UpdateProject, withURLParam(newRequest("PUT", "/api/projects/"+p.ID, map[string]any{"source_intent": "unspecified"}), "id", p.ID)).Want(200)
	stored, err := testHandler.Queries.GetAgentTask(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	historical := taskToResponse(stored, testWorkspaceID)
	if historical.ExecutionSource == nil || historical.ExecutionSource.Kind != "no_code" {
		t.Fatal("historical source changed with project")
	}
	if restrictedTaskResponse(historical).ExecutionSource != nil {
		t.Fatal("hidden project leaked source")
	}
}

func TestProjectSourceCreateAndQuickCreateAdmission(t *testing.T) {
	p := sourceTestProject(t, map[string]any{"title": "Create admission"})
	a := createHandlerTestAgent(t, "Create source agent", []byte("[]"))
	runtimeID := dbfx.Runtime(t, "Source quick-create capable runtime", testutil.Cols{"metadata": testutil.Raw(`'{"cli_version":"9.9.9"}'::jsonb`)})
	dbfx.Exec(t, "UPDATE agent SET runtime_id=$1 WHERE id=$2", runtimeID, a)
	body := map[string]any{"title": "New source choice issue", "project_id": p.ID, "assignee_type": "agent", "assignee_id": a, "source_decision": "ask"}
	testutil.Call(t, testHandler.CreateIssue, newRequest("POST", "/api/issues", body)).Want(409)
	var count int
	dbfx.QueryRow(t, "SELECT count(*) FROM issue WHERE project_id=$1", p.ID).Scan(&count)
	if count != 0 {
		t.Fatal("rejected create persisted an issue")
	}
	body["source_decisions"] = map[string]string{p.ID: "no_code_once"}
	var created IssueResponse
	testutil.Call(t, testHandler.CreateIssue, newRequest("POST", "/api/issues", body)).Want(201).JSON(&created)
	if queuedTaskCountFor(t, created.ID, a) != 1 {
		t.Fatal("create did not enqueue exactly once")
	}
	dbfx.Cleanup(t, "DELETE FROM issue WHERE id=$1", created.ID)
	parent := dbfx.Issue(t, "Parent inherits source", testutil.Cols{"project_id": p.ID})
	quick := map[string]any{"prompt": "Create a synthetic child", "agent_id": a, "parent_issue_id": parent, "source_decision": "ask"}
	testutil.Call(t, testHandler.QuickCreateIssue, newRequest("POST", "/api/issues/quick-create", quick)).Want(409)
	quick["source_decisions"] = map[string]string{p.ID: "no_code_once"}
	var queued QuickCreateIssueResponse
	testutil.Call(t, testHandler.QuickCreateIssue, newRequest("POST", "/api/issues/quick-create", quick)).Want(http.StatusAccepted).JSON(&queued)
	dbfx.Cleanup(t, "DELETE FROM agent_task_queue WHERE id=$1", queued.TaskID)
	task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(queued.TaskID))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		ProjectID string `json:"project_id"`
		NoCode    bool   `json:"source_no_code"`
	}
	if json.Unmarshal(task.Context, &payload) != nil || payload.ProjectID != p.ID || !payload.NoCode {
		t.Fatalf("inherited quick-create source was not recorded: %s", task.Context)
	}
}

func TestProjectSourceSquadChoice(t *testing.T) {
	p := sourceTestProject(t, map[string]any{"title": "Squad source"})
	leader := createHandlerTestAgent(t, "Source squad leader", []byte("[]"))
	squad := dbfx.Squad(t, "Source consent squad", leader)
	issue := dbfx.Issue(t, "Squad source choice", testutil.Cols{"project_id": p.ID, "status": "todo"})
	body := map[string]any{"assignee_type": "squad", "assignee_id": squad, "source_decision": "ask"}
	testutil.Call(t, testHandler.UpdateIssue, withURLParam(newRequest("PUT", "/api/issues/"+issue, body), "id", issue)).Want(409)
	if queuedTaskCountFor(t, issue, leader) != 0 {
		t.Fatal("squad ran before consent")
	}
	body["source_decisions"] = map[string]string{p.ID: "no_code_once"}
	testutil.Call(t, testHandler.UpdateIssue, withURLParam(newRequest("PUT", "/api/issues/"+issue, body), "id", issue)).Want(200)
	if queuedTaskCountFor(t, issue, leader) != 1 {
		t.Fatal("squad did not queue exactly once")
	}
	var raw []byte
	dbfx.QueryRow(t, "SELECT context FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2", issue, leader).Scan(&raw)
	var choice map[string]any
	json.Unmarshal(raw, &choice)
	if choice["source_no_code"] != true {
		t.Fatalf("squad lost request-only consent: %s", raw)
	}
}

func TestProjectSourceSnapshotKeepsRepositoryRefsAndLocalMode(t *testing.T) {
	a := createHandlerTestAgent(t, "Snapshot source agent", []byte("[]"))
	runtimeID := dbfx.Runtime(t, "Snapshot source machine", testutil.Cols{"daemon_id": "source-machine"})
	runtime, err := testHandler.Queries.GetAgentRuntime(context.Background(), parseUUID(runtimeID))
	if err != nil {
		t.Fatal(err)
	}
	for _, local := range []bool{false, true} {
		issue := dbfx.Issue(t, "Snapshot source selection")
		taskID := dbfx.Task(t, a, testutil.Cols{"issue_id": issue, "runtime_id": runtimeID})
		task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(taskID))
		if err != nil {
			t.Fatal(err)
		}
		repos := []RepoData{{URL: "https://github.com/example/one", Ref: "release/v2"}, {URL: "https://github.com/example/two", Ref: "feature/next"}}
		response := AgentTaskResponse{Repos: repos}
		if local {
			response.ProjectResources = []ProjectResourceData{{ResourceType: "local_directory", ResourceRef: json.RawMessage(`{"daemon_id":"source-machine","local_path":"/synthetic/source-project","execution_mode":"worktree"}`)}}
		}
		if err := testHandler.saveClaimExecutionSource(newRequest("POST", "/claim", nil), &task, &response, runtime); err != nil {
			t.Fatal(err)
		}
		stored, err := testHandler.Queries.GetAgentTask(context.Background(), task.ID)
		if err != nil {
			t.Fatal(err)
		}
		got := taskToResponse(stored, testWorkspaceID).ExecutionSource
		if got == nil {
			t.Fatal("source receipt not persisted")
		}
		if local {
			if got.Kind != "local_directory" || got.Mode != "worktree" || got.Label != "source-project" || got.DaemonID != "source-machine" {
				t.Fatalf("local effective source: %+v", got)
			}
		} else if got.Kind != "github_repo" || len(got.Repositories) != 2 || got.Repositories[0].Ref != "release/v2" || got.Repositories[1].Ref != "feature/next" {
			t.Fatalf("repository refs lost: %+v", got)
		}
	}
}

func TestProjectSourceBatchInvalidUUIDKeepsBatchValidation(t *testing.T) {
	agent := createHandlerTestAgent(t, "Invalid batch source agent", []byte("[]"))
	var result struct {
		Updated int `json:"updated"`
	}
	testutil.Call(t, testHandler.BatchUpdateIssues, newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{"not-a-uuid"}, "updates": map[string]any{"assignee_type": "agent", "assignee_id": agent, "source_decision": "ask"},
	})).Want(200).JSON(&result)
	if result.Updated != 0 {
		t.Fatal("invalid batch issue id changed a row")
	}
}
