package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func oneShotRuntime(t *testing.T) string {
	t.Helper()
	if testHandler == nil {
		t.Skip("database not available")
	}
	_, runtimeID, _ := createRuntimeGuardAgent(t, t.Context())
	dbfx.Exec(t, `UPDATE agent_runtime SET provider = 'codex' WHERE id = $1`, runtimeID)
	return runtimeID
}

func submitOneShot(t *testing.T, input service.OneShotRunInput) string {
	t.Helper()
	var resp struct {
		TaskID  string `json:"task_id"`
		LogsURI string `json:"logs_uri"`
	}
	testutil.Call(t, testHandler.CreateOneShotTask, newRequest("POST", "/api/tasks/oneshot", input)).Want(http.StatusAccepted).JSON(&resp)
	if resp.TaskID == "" || !strings.HasSuffix(resp.LogsURI, "/runs/"+resp.TaskID) {
		t.Fatalf("invalid accepted response: %+v", resp)
	}
	task, err := testHandler.Queries.GetAgentTask(t.Context(), parseUUID(resp.TaskID))
	if err != nil {
		t.Fatal(err)
	}
	dbfx.Cleanup(t, "DELETE FROM agent WHERE id = $1", task.AgentID)
	return resp.TaskID
}

func TestOneShotSubmissionClaimAndCompletion(t *testing.T) {
	runtimeID := oneShotRuntime(t)
	skillID := dbfx.Insert(t, "skill", testutil.Cols{"workspace_id": testWorkspaceID, "name": "one-shot-review", "content": "Review the patch."})
	prompt := "<request>\r\nReview the patch.\r\n</request>\n"
	id := submitOneShot(t, service.OneShotRunInput{RuntimeID: runtimeID, ModelID: "gpt-6.2-sol", Effort: "high", Skills: []string{skillID}, SystemPrompt: []string{"You are a reviewer."}, UserPrompt: prompt})
	queued, err := testHandler.Queries.GetAgentTask(t.Context(), parseUUID(id))
	if err != nil {
		t.Fatal(err)
	}
	if queued.Status != "queued" || queued.IssueID.Valid || queued.ChatSessionID.Valid || queued.AutopilotRunID.Valid || queued.MaxAttempts != 1 || !queued.ForceFreshSession {
		t.Fatalf("unexpected queued task: %+v", queued)
	}
	carrier, err := testHandler.Queries.GetAgent(t.Context(), queued.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if carrier.Kind != "system" || uuidToString(carrier.OwnerID) != testUserID {
		t.Fatalf("incorrect execution carrier: %+v", carrier)
	}
	testutil.Call(t, testHandler.UpdateAgent, withURLParam(newRequest("PUT", "/api/agents/"+uuidToString(carrier.ID), map[string]string{"model": "other"}), "id", uuidToString(carrier.ID))).Want(http.StatusNotFound)
	var listed []AgentResponse
	testutil.Call(t, testHandler.ListAgents, newRequest("GET", "/api/agents", nil)).Want(200).JSON(&listed)
	for _, a := range listed {
		if a.ID == uuidToString(carrier.ID) {
			t.Fatal("one-shot carrier leaked into agent catalog")
		}
	}

	req := withURLParam(newOwnerDaemonRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/claim", nil), "runtimeId", runtimeID)
	req.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilityOneShotRunsV1)
	var claimed struct {
		Task *AgentTaskResponse `json:"task"`
	}
	testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(200).JSON(&claimed)
	if claimed.Task == nil || claimed.Task.ID != id || claimed.Task.OneShotPrompt != prompt || claimed.Task.Agent.Model != "gpt-6.2-sol" || claimed.Task.Agent.ThinkingLevel != "high" || claimed.Task.Agent.Instructions != "You are a reviewer." {
		t.Fatalf("claim lost run input: %+v", claimed.Task)
	}
	found := false
	for _, skill := range claimed.Task.Agent.Skills {
		found = found || skill.ID == skillID
	}
	for _, skill := range claimed.Task.Agent.SkillRefs {
		found = found || skill.ID == skillID
	}
	if !found {
		t.Fatal("claim lost selected skill")
	}

	testutil.Call(t, testHandler.StartTask, daemonTaskRequest(t, "/start", id, map[string]any{})).Want(200)
	testutil.Call(t, testHandler.ReportTaskMessages, daemonTaskRequest(t, "/messages", id, TaskMessageBatchRequest{Messages: []TaskMessageRequest{{Seq: 1, Type: "text", Content: "Reviewing patch."}}})).Want(200)

	var delivered events.Event
	testHandler.Bus.Subscribe(protocol.EventTaskCompleted, func(e events.Event) {
		if e.TaskID == id {
			delivered = e
		}
	})
	testutil.Call(t, testHandler.CompleteTask, daemonTaskRequest(t, "/complete", id, TaskCompleteRequest{Output: "Reviewed."})).Want(200)

	if delivered.MemberPrivateUserID != testUserID {
		t.Fatalf("result event is not private to submitter: %+v", delivered)
	}
	var result OneShotRunResponse
	testutil.Call(t, testHandler.GetOneShotRun, withChatTestWorkspaceCtx(t, withURLParam(newRequest("GET", "/api/tasks/oneshot/"+id, nil), "taskId", id))).Want(200).JSON(&result)
	if result.Task.Kind != "oneshot" || result.Task.Status != "completed" || result.Input.UserPrompt != prompt {
		t.Fatalf("run readback = %+v", result)
	}
	if output, ok := result.Task.Result.(map[string]any); !ok || output["output"] != "Reviewed." {
		t.Fatalf("completed run lost result: %+v", result.Task.Result)
	}
}

func TestOneShotRejectsUnauthorizedAndInvalidSubmission(t *testing.T) {
	runtimeID := oneShotRuntime(t)
	base := service.OneShotRunInput{RuntimeID: runtimeID, UserPrompt: "Review the patch."}
	for _, tc := range []struct {
		name    string
		prepare func()
		input   service.OneShotRunInput
		want    int
	}{
		{"bad skill", func() {}, service.OneShotRunInput{RuntimeID: runtimeID, UserPrompt: "review", Skills: []string{"not-a-uuid"}}, 400},
		{"missing skill", func() {}, service.OneShotRunInput{RuntimeID: runtimeID, UserPrompt: "review", Skills: []string{"33333333-3333-4333-8333-333333333333"}}, 404},
		{"NUL prompt", func() {}, service.OneShotRunInput{RuntimeID: runtimeID, UserPrompt: "review\x00"}, 400},
		{"empty prompt", func() {}, service.OneShotRunInput{RuntimeID: runtimeID}, 400},
		{"offline runtime", func() { dbfx.Exec(t, `UPDATE agent_runtime SET status='offline' WHERE id=$1`, runtimeID) }, base, 409},
		{"private runtime", func() {
			dbfx.Exec(t, `UPDATE agent_runtime SET status='online', owner_id=NULL, visibility='private' WHERE id=$1`, runtimeID)
		}, base, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.prepare()
			var before int64
			dbfx.QueryRow(t, `SELECT count(*) FROM agent WHERE runtime_id=$1 AND system_key LIKE 'oneshot:%'`, runtimeID).Scan(&before)
			testutil.Call(t, testHandler.CreateOneShotTask, newRequest("POST", "/api/tasks/oneshot", tc.input)).Want(tc.want)
			var after int64
			dbfx.QueryRow(t, `SELECT count(*) FROM agent WHERE runtime_id=$1 AND system_key LIKE 'oneshot:%'`, runtimeID).Scan(&after)
			if before != after {
				t.Fatal("rejected submission left an execution carrier")
			}
		})
	}
}

func TestOneShotOwnerOnlyReadAndCancel(t *testing.T) {
	runtimeID := oneShotRuntime(t)
	id := submitOneShot(t, service.OneShotRunInput{RuntimeID: runtimeID, UserPrompt: "Private request"})
	other := dbfx.User(t, "One-shot other member", "oneshot-other@example.test")
	dbfx.Member(t, testWorkspaceID, other, "member")
	for _, read := range []http.HandlerFunc{testHandler.GetOneShotRun, testHandler.ListTaskMessagesByUser} {
		testutil.Call(t, read, withChatTestWorkspaceCtx(t, withURLParam(newRequestAs(other, "GET", "/api/tasks/"+id, nil), "taskId", id))).Want(404)
	}
	testutil.Call(t, testHandler.CancelTaskByUser, cancelTaskByUserRequest(t, other, id)).Want(403)
	testutil.Call(t, testHandler.CancelTaskByUser, cancelTaskByUserRequest(t, testUserID, id)).Want(200)
	task, err := testHandler.Queries.GetAgentTask(t.Context(), parseUUID(id))
	if err != nil || task.Status != "cancelled" {
		t.Fatalf("cancelled task=%+v err=%v", task, err)
	}
}

func TestOneShotOldDaemonDoesNotExecute(t *testing.T) {
	runtimeID := oneShotRuntime(t)
	id := submitOneShot(t, service.OneShotRunInput{RuntimeID: runtimeID, UserPrompt: "review"})
	req := withURLParam(newOwnerDaemonRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/claim", nil), "runtimeId", runtimeID)
	testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(409)
	task, err := testHandler.Queries.GetAgentTask(t.Context(), parseUUID(id))
	if err != nil || task.Status != "queued" {
		t.Fatalf("unsupported daemon consumed task: %+v err=%v", task, err)
	}
}

func TestOneShotCannotBorrowMemberIdentity(t *testing.T) {
	runtimeID := oneShotRuntime(t)
	input := service.OneShotRunInput{RuntimeID: runtimeID, UserPrompt: "review"}
	for _, source := range []string{"task_token", "cloud_pat"} {
		t.Run(source, func(t *testing.T) {
			req := newRequest("POST", "/api/tasks/oneshot", input)
			req.Header.Set("X-Actor-Source", source)
			testutil.Call(t, testHandler.CreateOneShotTask, req).Want(403)
		})
	}
	guest := dbfx.User(t, "One-shot guest", "oneshot-guest@example.test")
	dbfx.Member(t, testWorkspaceID, guest, "guest")
	testutil.Call(t, testHandler.CreateOneShotTask, newRequestAs(guest, "POST", "/api/tasks/oneshot", input)).Want(403)
}

func TestOneShotConfigurationsAreIndependent(t *testing.T) {
	runtimeID := oneShotRuntime(t)
	first := submitOneShot(t, service.OneShotRunInput{RuntimeID: runtimeID, UserPrompt: "First", ModelID: "first-model", SystemPrompt: []string{"First system prompt"}})
	second := submitOneShot(t, service.OneShotRunInput{RuntimeID: runtimeID, UserPrompt: "Second", ModelID: "second-model", SystemPrompt: []string{"Second system prompt"}})
	firstTask, err := testHandler.Queries.GetAgentTask(t.Context(), parseUUID(first))
	if err != nil {
		t.Fatal(err)
	}
	secondTask, err := testHandler.Queries.GetAgentTask(t.Context(), parseUUID(second))
	if err != nil {
		t.Fatal(err)
	}
	if firstTask.AgentID == secondTask.AgentID {
		t.Fatal("runs share mutable execution configuration")
	}
	for id, model := range map[string]string{first: "first-model", second: "second-model"} {
		var run OneShotRunResponse
		req := withChatTestWorkspaceCtx(t, withURLParam(newRequest("GET", "/", nil), "taskId", id))
		testutil.Call(t, testHandler.GetOneShotRun, req).Want(200).JSON(&run)
		if run.Input.ModelID != model {
			t.Fatalf("configuration overwritten: %+v", run.Input)
		}
	}
}

func TestOneShotRejectsForeignResources(t *testing.T) {
	runtimeID := oneShotRuntime(t)
	foreignWorkspace := dbfx.Workspace(t, "Foreign one-shot", "foreign-oneshot")
	foreignRuntime := dbfx.Insert(t, "agent_runtime", testutil.Cols{"workspace_id": foreignWorkspace, "name": "Foreign Codex", "provider": "codex", "runtime_mode": "local", "status": "online"})
	foreignSkill := dbfx.Insert(t, "skill", testutil.Cols{"workspace_id": foreignWorkspace, "name": "Foreign review", "content": "Review"})
	for _, input := range []service.OneShotRunInput{
		{RuntimeID: foreignRuntime, UserPrompt: "Review"},
		{RuntimeID: runtimeID, UserPrompt: "Review", Skills: []string{foreignSkill}},
	} {
		testutil.Call(t, testHandler.CreateOneShotTask, newRequest("POST", "/api/tasks/oneshot", input)).Want(404)
	}
}
