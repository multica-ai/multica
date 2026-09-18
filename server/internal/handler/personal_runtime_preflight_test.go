package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestPersonalRuntimePreflightUsesSelectedMachine(t *testing.T) {
	agentID, _, defaultID, personalID := personalRuntimeFixture(t)
	dbfx.Exec(t, `UPDATE agent_runtime SET status='offline' WHERE id=$1`, defaultID)
	preferenceRequest(t, agentID, testUserID, "PUT", map[string]any{"runtime_id": personalID}).Want(http.StatusOK)
	var out QuickCreateIssueResponse
	req := newRequest("POST", "/api/issues/quick-create?workspace_id="+testWorkspaceID, map[string]any{"agent_id": agentID, "prompt": "create a task"})
	testutil.Call(t, testHandler.QuickCreateIssue, req).Want(http.StatusAccepted).JSON(&out)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE id=$1`, out.TaskID)
	task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(out.TaskID))
	if err != nil {
		t.Fatal(err)
	}
	if task.RuntimeID != parseUUID(personalID) {
		t.Fatal("quick-create selected shared default")
	}
	squadID := dbfx.Squad(t, "Personal preflight", agentID)
	preview := previewIssueTrigger(t, map[string]any{"is_create": true, "assignee_type": "squad", "assignee_id": squadID, "status": "todo"})
	if len(preview.Triggers) != 1 {
		t.Fatalf("preview ignored online personal runtime: %+v", preview)
	}
	dbfx.Exec(t, `UPDATE agent_runtime SET metadata='{"cli_version":"0.0.1"}' WHERE id=$1`, personalID)
	req = newRequest("POST", "/api/issues/quick-create?workspace_id="+testWorkspaceID, map[string]any{"agent_id": agentID, "prompt": "create a task"})
	response := testutil.Call(t, testHandler.QuickCreateIssue, req)
	if response.Code < 400 || response.Code >= 500 {
		t.Fatalf("old personal daemon accepted: %d %s", response.Code, response.Text())
	}
}

func TestPersonalRuntimeParentRequiresTaskToken(t *testing.T) {
	agentID, _, _, personalID := personalRuntimeFixture(t)
	snapshot, _ := json.Marshal(service.RuntimeRouting{Version: 1, ExecutionUserID: testUserID, Routes: map[string]service.RuntimeRoute{agentID: {RuntimeID: personalID, RuntimeOwnerID: testUserID, Provider: "codex", Source: "personal"}}})
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": personalID, "runtime_routing": snapshot, "originator_user_id": testUserID, "accountable_user_id": testUserID})
	agent, err := testHandler.Queries.GetAgent(context.Background(), parseUUID(agentID))
	if err != nil {
		t.Fatal(err)
	}
	req := newRequest("POST", "/api/issues", nil)
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	if _, err := testHandler.runtimeForRequest(req, agent, "agent", agentID); !errors.Is(err, service.ErrTaskRuntimeUnavailable) {
		t.Fatalf("untrusted task header accepted: %v", err)
	}
	req.Header.Set("X-Actor-Source", "task_token")
	got, err := testHandler.runtimeForRequest(req, agent, "agent", agentID)
	if err != nil || got != parseUUID(personalID) {
		t.Fatalf("verified task lost frozen route: %v %v", got, err)
	}
	// A mismatched token agent cannot borrow the source task.
	if _, err := testHandler.runtimeForRequest(req, agent, "agent", "00000000-0000-0000-0000-000000000001"); !errors.Is(err, service.ErrTaskRuntimeUnavailable) {
		t.Fatalf("mismatched task identity accepted: %v", err)
	}
}

func TestPersonalRuntimeWithoutDefaultRuntime(t *testing.T) {
	agentID, _, _, personalID := personalRuntimeFixture(t)
	preferenceRequest(t, agentID, testUserID, "PUT", map[string]any{"runtime_id": personalID}).Want(http.StatusOK)
	dbfx.Exec(t, `UPDATE agent SET runtime_id=NULL WHERE id=$1`, agentID)
	var agent AgentResponse
	testutil.Call(t, testHandler.GetAgent, withURLParam(newRequest("GET", "/api/agents/"+agentID, nil), "id", agentID)).Want(http.StatusOK).JSON(&agent)
	if agent.RuntimeBound || agent.RuntimeID != "" || agent.PersonalRuntimeID != personalID || agent.PersonalRuntimeAvailability != "online" {
		t.Fatalf("effective personal availability lost: %+v", agent)
	}
	var quick QuickCreateIssueResponse
	testutil.Call(t, testHandler.QuickCreateIssue, newRequest("POST", "/api/issues/quick-create?workspace_id="+testWorkspaceID, map[string]any{"agent_id": agentID, "prompt": "personal quick-create"})).Want(http.StatusAccepted).JSON(&quick)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE id=$1`, quick.TaskID)
	issueID := dbfx.Issue(t, "Unbound default", testutil.Cols{"assignee_type": "agent", "assignee_id": agentID})
	preview := previewCommentTriggersForTest(t, issueID, map[string]any{"content": "[agent](mention://agent/" + agentID + ") please help"})
	if len(preview.Agents) != 1 || len(preview.Blocked) != 0 {
		t.Fatalf("comment preview refused personal runtime: %+v", preview)
	}
	sessionID := createHandlerTestChatSession(t, agentID)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE chat_session_id=$1`, sessionID)
	dbfx.Cleanup(t, `DELETE FROM chat_message WHERE chat_session_id=$1`, sessionID)
	req := withChatTestWorkspaceCtx(t, withURLParam(newRequest("POST", "/api/chat-sessions/"+sessionID+"/messages", map[string]any{"content": "personal chat"}), "sessionId", sessionID))
	testutil.Call(t, testHandler.SendChatMessage, req).Want(http.StatusCreated)
}

// Assigning an existing issue continues the authenticated run, even when the
// issue's creator and the user's current preference differ from that run.
func TestAgentAssignmentInheritsFrozenRuntimeOnExistingIssue(t *testing.T) {
	for _, assigneeType := range []string{"agent", "squad"} {
		for _, batch := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/batch=%t", assigneeType, batch), func(t *testing.T) {
				ctx := context.Background()
				targetID, ownerID, _, personalID := personalRuntimeFixture(t)
				preferenceRequest(t, targetID, testUserID, "PUT", map[string]any{"runtime_id": personalID, "model_mode": "custom", "model": "frozen-model"}).Want(http.StatusOK)
				driverID := dbfx.Agent(t, "Delegating agent", handlerTestRuntimeID(t))
				parentIssueID := dbfx.Issue(t, "Parent execution", testutil.Cols{"assignee_type": "agent", "assignee_id": driverID})
				dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id=$1`, parentIssueID)
				parentIssue, err := testHandler.Queries.GetIssue(ctx, parseUUID(parentIssueID))
				if err != nil {
					t.Fatal(err)
				}
				parent, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, parentIssue)
				if err != nil {
					t.Fatal(err)
				}
				dbfx.Exec(t, `UPDATE agent_task_queue SET status='running' WHERE id=$1`, parent.ID)
				preferenceRequest(t, targetID, testUserID, "PUT", map[string]any{"runtime_id": nil}).Want(http.StatusOK)

				targetIssueID := dbfx.Issue(t, "Another member's existing issue", testutil.Cols{"creator_id": ownerID, "status": "todo"})
				dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id=$1`, targetIssueID)
				assigneeID := targetID
				if assigneeType == "squad" {
					assigneeID = seedSquad(t, targetID, "Delegated squad", false)
				}
				updates := map[string]any{"assignee_type": assigneeType, "assignee_id": assigneeID}
				if batch {
					req := asRun(newRequest("PATCH", "/api/issues/batch?workspace_id="+testWorkspaceID, map[string]any{"issue_ids": []string{targetIssueID}, "updates": updates}), driverID, uuidToString(parent.ID))
					testutil.Call(t, testHandler.BatchUpdateIssues, req).Want(http.StatusOK)
				} else {
					req := withURLParam(asRun(newRequest("PATCH", "/api/issues/"+targetIssueID, updates), driverID, uuidToString(parent.ID)), "id", targetIssueID)
					testutil.Call(t, testHandler.UpdateIssue, req).Want(http.StatusOK)
				}
				var taskID string
				dbfx.QueryRow(t, `SELECT id FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2`, targetIssueID, targetID).Scan(&taskID)
				child, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
				if err != nil {
					t.Fatal(err)
				}
				snapshot, err := service.ParseRuntimeRouting(child.RuntimeRouting)
				if err != nil || snapshot == nil {
					t.Fatalf("missing child route: %v", err)
				}
				route, err := snapshot.Route(targetID)
				if err != nil {
					t.Fatal(err)
				}
				if child.DelegatedFromTaskID != parent.ID || child.OriginatorUserID != parseUUID(testUserID) || child.RuntimeID != parseUUID(personalID) || snapshot.ExecutionUserID != testUserID || route.Model == nil || *route.Model != "frozen-model" {
					t.Fatalf("child replaced parent's execution identity: runtime=%s user=%s model=%v parent=%s", uuidToString(child.RuntimeID), snapshot.ExecutionUserID, route.Model, uuidToString(child.DelegatedFromTaskID))
				}
				storedIssue, err := testHandler.Queries.GetIssue(ctx, parseUUID(targetIssueID))
				if err != nil {
					t.Fatal(err)
				}
				if storedIssue.CreatorID != parseUUID(ownerID) || storedIssue.OriginID.Valid {
					t.Fatal("dispatch rewrote issue provenance")
				}
			})
		}
	}
}
