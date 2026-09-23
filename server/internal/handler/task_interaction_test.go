package handler

import (
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/interaction"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/agent"
)

func interactiveFixture(t *testing.T) (string, string, string, string) {
	t.Helper()
	runtime := dbfx.Runtime(t, "interactive runtime", testutil.Cols{"provider": "pi"})
	worker := dbfx.Agent(t, "interactive worker", runtime, testutil.Cols{"runtime_config": testutil.Raw(`'{"interactive_task_sessions":true}'::jsonb`)})
	issue := dbfx.Issue(t, "interactive issue", testutil.Cols{"assignee_type": "agent", "assignee_id": worker})
	task := dbfx.Task(t, worker, testutil.Cols{"runtime_id": runtime, "issue_id": issue, "status": "running", "started_at": testutil.Raw("now()")})
	dbfx.Insert(t, "task_token", testutil.Cols{"token_hash": "interactive-" + task, "task_id": task, "agent_id": worker, "workspace_id": testWorkspaceID, "user_id": testUserID, "expires_at": time.Now().Add(time.Hour)})
	return runtime, worker, issue, task
}

func interactionRequest(t *testing.T, task, runtime, method string, body any) *http.Request {
	t.Helper()
	req := withURLParam(newRequest(method, "/interaction", body), "taskId", task)
	chi.RouteContext(req.Context()).URLParams.Add("runtimeId", runtime)
	return withChatTestWorkspaceCtx(t, req)
}

func TestTaskInteractionInboxAndFinishBarrier(t *testing.T) {
	runtime, _, _, task := interactiveFixture(t)
	state := agent.InteractionSnapshot{State: agent.InteractionWorking, Activity: 1}
	sync := interaction.Sync{Owner: "process", Register: true, Deadline: time.Now().Add(24 * time.Hour), State: state}
	var record interaction.Record
	testutil.Call(t, testHandler.SyncTaskInteraction, interactionRequest(t, task, runtime, "POST", sync)).Want(200).JSON(&record)
	if record.Deadline.After(time.Now().Add(time.Hour)) {
		t.Fatal("credential bound was not enforced")
	}
	command := map[string]any{"owner": "process", "id": "input", "kind": "input", "activity": 1, "text": "change direction"}
	for i := 0; i < 2; i++ {
		testutil.Call(t, testHandler.SendTaskInteraction, interactionRequest(t, task, runtime, "POST", command)).Want(202).JSON(&record)
	}
	if len(record.Commands) != 1 {
		t.Fatal("duplicate input")
	}
	sync.Register = false
	sync.Finish = true
	testutil.Call(t, testHandler.SyncTaskInteraction, interactionRequest(t, task, runtime, "POST", sync)).Want(200).JSON(&record)
	if record.Finishing {
		t.Fatal("finish overtook accepted input")
	}
	sync.Acknowledgement = &interaction.Input{InteractionCommand: agent.InteractionCommand{ID: "input"}, Receipt: &agent.InteractionReceipt{Outcome: "applied", Snapshot: state}}
	testutil.Call(t, testHandler.SyncTaskInteraction, interactionRequest(t, task, runtime, "POST", sync)).Want(200).JSON(&record)
	if !record.Finishing || record.Pending() {
		t.Fatal("finish did not close inbox after acknowledgement")
	}
	command["id"] = "late"
	testutil.Call(t, testHandler.SendTaskInteraction, interactionRequest(t, task, runtime, "POST", command)).Want(409)
	command["id"] = "input"
	dbfx.Exec(t, "UPDATE agent_task_queue SET status='completed' WHERE id=$1", task)
	testutil.Call(t, testHandler.SendTaskInteraction, interactionRequest(t, task, runtime, "POST", command)).Want(202)
	testutil.Call(t, testHandler.GetTaskInteraction, interactionRequest(t, task, runtime, "GET", nil)).Want(200).JSON(&record)
	if record.State.State != agent.InteractionFinished {
		t.Fatal("terminal task still appears interactive")
	}
}

func TestTaskInteractionAcceptsClaudeRuntime(t *testing.T) {
	runtime, _, _, task := interactiveFixture(t)
	dbfx.Exec(t, "UPDATE agent_runtime SET provider='claude' WHERE id=$1", runtime)
	sync := interaction.Sync{Owner: "claude-process", Register: true, Deadline: time.Now().Add(time.Hour), State: agent.InteractionSnapshot{State: agent.InteractionWorking, Activity: 1}}
	testutil.Call(t, testHandler.SyncTaskInteraction, interactionRequest(t, task, runtime, "POST", sync)).Want(http.StatusOK)
}

func TestTaskInteractionRejectsWrongOwnerRuntimeAndActor(t *testing.T) {
	runtime, _, _, task := interactiveFixture(t)
	sync := interaction.Sync{Owner: "process", Register: true, Deadline: time.Now().Add(time.Hour), State: agent.InteractionSnapshot{State: agent.InteractionWorking, Activity: 1}}
	testutil.Call(t, testHandler.SyncTaskInteraction, interactionRequest(t, task, runtime, "POST", sync)).Want(200)
	sync.Owner = "replacement"
	testutil.Call(t, testHandler.SyncTaskInteraction, interactionRequest(t, task, runtime, "POST", sync)).Want(409)
	other := dbfx.Runtime(t, "other", testutil.Cols{"provider": "pi"})
	testutil.Call(t, testHandler.SyncTaskInteraction, interactionRequest(t, task, other, "POST", sync)).Want(403)
	req := interactionRequest(t, task, runtime, "POST", map[string]any{"owner": "process", "id": "send", "kind": "input", "activity": 1, "text": "hello"})
	req.Header.Set("X-Actor-Source", "task_token")
	testutil.Call(t, RequireHumanActor(http.HandlerFunc(testHandler.SendTaskInteraction)).ServeHTTP, req).Want(403)
}

func TestTaskInteractionFollowupPinsSourceAndDeduplicates(t *testing.T) {
	runtime, _, _, task := interactiveFixture(t)
	sync := interaction.Sync{Owner: "process", Register: true, Deadline: time.Now().Add(time.Hour), State: agent.InteractionSnapshot{State: agent.InteractionWorking, Activity: 1}}
	testutil.Call(t, testHandler.SyncTaskInteraction, interactionRequest(t, task, runtime, "POST", sync)).Want(200)
	dbfx.Exec(t, "UPDATE agent_task_queue SET status='completed', session_id='persistent-session' WHERE id=$1", task)
	input := map[string]string{"id": "followup", "text": "one more change"}
	var first, second map[string]string
	testutil.Call(t, testHandler.FollowupTaskInteraction, interactionRequest(t, task, runtime, "POST", input)).Want(201).JSON(&first)
	t.Cleanup(func() { dbfx.Exec(t, "DELETE FROM agent_task_queue WHERE id=$1", first["run_id"]) })
	testutil.Call(t, testHandler.FollowupTaskInteraction, interactionRequest(t, task, runtime, "POST", input)).Want(200).JSON(&second)
	if first["run_id"] != second["run_id"] {
		t.Fatal("duplicate follow-up run")
	}
	var source, note string
	dbfx.QueryRow(t, "SELECT rerun_of_task_id,handoff_note FROM agent_task_queue WHERE id=$1", first["run_id"]).Scan(&source, &note)
	if source != task || note != input["text"] {
		t.Fatalf("wrong continuation source/instruction: %s %s", source, note)
	}
	var context []byte
	dbfx.QueryRow(t, "SELECT context FROM agent_task_queue WHERE id=$1", first["run_id"]).Scan(&context)
	var continuation struct {
		Required bool `json:"require_session_resume"`
	}
	if err := json.Unmarshal(context, &continuation); err != nil || !continuation.Required {
		t.Fatalf("resume requirement not persisted: %s", context)
	}
	input["id"] = "concurrent"
	testutil.Call(t, testHandler.FollowupTaskInteraction, interactionRequest(t, task, runtime, "POST", input)).Want(409)
}

func TestTaskInteractionAdminViewDoesNotGrantInvocation(t *testing.T) {
	runtime, worker, _, task := interactiveFixture(t)
	dbfx.Exec(t, "UPDATE agent SET visibility='workspace', permission_mode='private' WHERE id=$1", worker)
	member := createWorkspaceMemberUser(t, "observer", "interactive-observer@test.invalid")
	dbfx.Exec(t, "UPDATE member SET role='admin' WHERE workspace_id=$1 AND user_id=$2", testWorkspaceID, member)
	req := interactionRequest(t, task, runtime, "POST", map[string]string{})
	req.Header.Set("X-User-ID", member)
	testutil.Call(t, testHandler.SendTaskInteraction, req).Want(http.StatusForbidden)
	req = interactionRequest(t, task, runtime, "GET", nil)
	req.Header.Set("X-User-ID", member)
	testutil.Call(t, testHandler.GetTaskInteraction, req).Want(http.StatusOK)
}
