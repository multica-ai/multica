package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestCommentMergePreservesRuntimeExecutionUser(t *testing.T) {
	for _, status := range []string{"queued", "running"} {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			agentID, ownerID, defaultID, personalID := personalRuntimeFixture(t)
			issueID := dbfx.Issue(t, "Separate execution users", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agentID})
			original := dbfx.Comment(t, issueID, "first user's input")
			second := dbfx.Comment(t, issueID, "second user's input", testutil.Cols{"author_id": ownerID, "parent_id": original})
			snapshot, _ := json.Marshal(service.RuntimeRouting{Version: 1, ExecutionUserID: testUserID, Routes: map[string]service.RuntimeRoute{agentID: {RuntimeID: personalID, RuntimeOwnerID: testUserID, Provider: "codex", Source: "personal"}}})
			taskID := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": personalID, "status": status, "runtime_routing": snapshot, "trigger_comment_id": original, "originator_user_id": testUserID, "accountable_user_id": testUserID, "runtime_mcp_overlay": testutil.Raw(`'{"personal":"original"}'::jsonb`)})
			dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id=$1`, issueID)
			agent, err := testHandler.Queries.GetAgent(ctx, parseUUID(agentID))
			if err != nil {
				t.Fatal(err)
			}
			issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
			if err != nil {
				t.Fatal(err)
			}
			trigger := commentAgentTrigger{Agent: agent, Source: commentTriggerSourceIssueAssignee}
			result := testHandler.mergeCommentIntoPendingTask(ctx, issue, trigger, parseUUID(second), pgtype.Text{})
			if result != commentMergeDeferred {
				t.Fatalf("cross-user comment result=%v", result)
			}
			task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
			if err != nil {
				t.Fatal(err)
			}
			if task.TriggerCommentID != parseUUID(original) || task.OriginatorUserID != parseUUID(testUserID) || len(task.CoalescedCommentIds) != 0 || len(task.ReconciliationCommentIds) != 1 {
				t.Fatalf("cross-user input changed original execution: %+v", task)
			}
			// Failure replays only the deferred author's request, with its own default route.
			dbfx.Exec(t, `UPDATE agent_task_queue SET status='failed',completed_at=now() WHERE id=$1`, taskID)
			task, err = testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
			if err != nil {
				t.Fatal(err)
			}
			testHandler.reconcileCommentsOnFailure(ctx, &task)
			var count int
			dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND trigger_comment_id=$2 AND runtime_id=$3 AND runtime_routing->>'execution_user_id'=$4`, issueID, second, defaultID, ownerID).Scan(&count)
			if count != 1 {
				t.Fatalf("deferred user's execution count=%d", count)
			}
		})
	}
}

func TestSameUserCommentMergeKeepsPersonalRuntimeSnapshot(t *testing.T) {
	ctx := context.Background()
	agentID, _, _, personalID := personalRuntimeFixture(t)
	issueID := dbfx.Issue(t, "Frozen model merge", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agentID})
	original := dbfx.Comment(t, issueID, "original")
	next := dbfx.Comment(t, issueID, "same user followup", testutil.Cols{"parent_id": original})
	model := "private-model"
	snapshot, _ := json.Marshal(service.RuntimeRouting{Version: 1, ExecutionUserID: testUserID, Routes: map[string]service.RuntimeRoute{agentID: {RuntimeID: personalID, RuntimeOwnerID: testUserID, Provider: "codex", Source: "personal", Model: &model}}})
	taskID := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": personalID, "runtime_routing": snapshot, "trigger_comment_id": original, "originator_user_id": testUserID, "accountable_user_id": testUserID, "runtime_mcp_overlay": testutil.Raw(`'{"personal":"original"}'::jsonb`)})
	agent, err := testHandler.Queries.GetAgent(ctx, parseUUID(agentID))
	if err != nil {
		t.Fatal(err)
	}
	issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	if got := testHandler.mergeCommentIntoPendingTask(ctx, issue, commentAgentTrigger{Agent: agent, Source: commentTriggerSourceIssueAssignee}, parseUUID(next), pgtype.Text{}); got != commentMergeSucceeded {
		t.Fatalf("same-user merge=%v", got)
	}
	task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	route, err := service.ParseRuntimeRouting(task.RuntimeRouting)
	if err != nil {
		t.Fatal(err)
	}
	if route.Routes[agentID].Model == nil || *route.Routes[agentID].Model != model || !strings.Contains(string(task.RuntimeMcpOverlay), "original") {
		t.Fatal("merge replaced frozen execution settings")
	}
}

func TestClaimPersonalRuntimeOmitsSharedSecrets(t *testing.T) {
	withComposioMCPAppsFlag(t, testHandler, true)
	ctx := context.Background()
	agentID, _, _, personalID := personalRuntimeFixture(t)
	dbfx.Exec(t, `UPDATE agent SET custom_env='{"TOKEN":"private-owner-secret"}',custom_args='["--token=private-owner-secret"]',mcp_config='{"mcpServers":{"private":{"url":"private-owner-secret"}}}',runtime_config='{"token":"private-owner-secret"}',thinking_level='high',service_tier='fast' WHERE id=$1`, agentID)
	serverID := createWorkspaceMcpServerForTest(t, "owner-bound-secret", `{"url":"https://shared.example","headers":{"Authorization":"private-owner-secret"},"env":{"TOKEN":"private-owner-secret"}}`)
	dbfx.Exec(t, `INSERT INTO agent_mcp_server(agent_id,server_id) VALUES($1,$2)`, agentID, serverID)
	dbfx.Cleanup(t, `DELETE FROM agent_mcp_server WHERE agent_id=$1 AND server_id=$2`, agentID, serverID)
	preferenceRequest(t, agentID, testUserID, "PUT", map[string]any{"runtime_id": personalID, "model_mode": "custom", "model": "private-model"}).Want(http.StatusOK)
	issueID := dbfx.Issue(t, "Personal claim", testutil.Cols{"assignee_type": "agent", "assignee_id": agentID})
	issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	task, err := testHandler.TaskService.EnqueueTaskForIssue(ctx, issue)
	if err != nil {
		t.Fatal(err)
	}
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE id=$1`, task.ID)
	task.RuntimeMcpOverlay = []byte(`{"mcpServers":{"execution-user":{"url":"https://personal.example","headers":{"Authorization":"execution-user-token"}}}}`)
	runtime, err := testHandler.Queries.GetAgentRuntime(ctx, parseUUID(personalID))
	if err != nil {
		t.Fatal(err)
	}
	req := newRequest("POST", "/api/daemon/runtimes/"+personalID+"/claim", nil)
	response, _, _, _, _, failure := testHandler.buildClaimedTaskResponse(req, &task, runtime, personalID, testWorkspaceID)
	if failure != nil {
		t.Fatalf("personal claim rejected: %+v", failure)
	}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private-owner-secret") {
		t.Fatal("source-owner credential reached personal runtime")
	}
	if !strings.Contains(string(raw), "execution-user-token") {
		t.Fatal("personal overlay was dropped with shared credentials")
	}
	if response.Agent == nil || response.Agent.Model != "private-model" || response.Agent.ThinkingLevel != "" || response.Agent.ServiceTier != "" {
		t.Fatalf("incompatible model settings survived: %+v", response.Agent)
	}
	if response.RuntimeID != personalID || response.RuntimeExecutionUserID != testUserID {
		t.Fatal("claim lost execution identity")
	}
}

// A shared machine belongs to the agent owner; mat_ API authority belongs to
// the human whose frozen route is executing, on both claim endpoints.
func TestClaimTaskTokenUsesFrozenExecutionUser(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "batch"}[batch], func(t *testing.T) {
			agentID, ownerID, runtimeID, _ := personalRuntimeFixture(t)
			dbfx.Exec(t, `UPDATE agent_runtime SET daemon_id=$2,visibility='public' WHERE id=$1`, runtimeID, batchClaimTestDaemonID)
			issueID := dbfx.Issue(t, "Execution token", testutil.Cols{"assignee_type": "agent", "assignee_id": agentID})
			issue, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(issueID))
			if err != nil {
				t.Fatal(err)
			}
			task, err := testHandler.TaskService.EnqueueTaskForIssue(context.Background(), issue)
			if err != nil {
				t.Fatal(err)
			}
			dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE id=$1`, task.ID)
			dbfx.Cleanup(t, `DELETE FROM task_token WHERE task_id=$1`, task.ID)
			var claimed AgentTaskResponse
			if batch {
				var response struct {
					Tasks []AgentTaskResponse `json:"tasks"`
				}
				testutil.Call(t, testHandler.ClaimTasksByRuntime, batchClaimRequest(testWorkspaceID, []string{runtimeID}, 1, "")).Want(http.StatusOK).JSON(&response)
				if len(response.Tasks) != 1 {
					t.Fatalf("claimed tasks=%d", len(response.Tasks))
				}
				claimed = response.Tasks[0]
			} else {
				var response struct {
					Task *AgentTaskResponse `json:"task"`
				}
				req := withURLParam(newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/claim", nil, testWorkspaceID, batchClaimTestDaemonID), "runtimeId", runtimeID)
				testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK).JSON(&response)
				if response.Task == nil {
					t.Fatal("no claimed task")
				}
				claimed = *response.Task
			}
			token, err := testHandler.Queries.GetTaskTokenByHash(context.Background(), auth.HashToken(claimed.AuthToken))
			if err != nil {
				t.Fatal(err)
			}
			if uuidToString(token.UserID) != testUserID || uuidToString(token.UserID) == ownerID {
				t.Fatal("task token inherited machine owner authority")
			}
		})
	}
}
