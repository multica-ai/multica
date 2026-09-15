package handler

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Exercise the real assign/promote and daemon claim paths: Bob's agent must
// inherit Bob, even when Alice created the target issue.
func TestTaskTokenAssignmentPreservesActingHuman(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTaskTokenCatalog(t, `[{"id":"erp","label":"ERP","env":"BOT_TOKEN_ERP","claims":{"sub":"{{identity.email}}"}}]`)
	for _, targetType := range []string{"agent", "squad"} {
		for _, action := range []string{"assign", "batch_assign", "promote", "batch_promote"} {
			t.Run(targetType+"/"+action, func(t *testing.T) {
				email := fmt.Sprintf("assignment-bob-%d@example.com", time.Now().UnixNano())
				bob := dbfx.User(t, "Bob", email)
				dbfx.Member(t, testWorkspaceID, bob, "member")
				rt := dbfx.Runtime(t, "identity-claim", testutil.Cols{"device_info": "JWT regression", "owner_id": bob})
				actorRT := handlerTestRuntimeID(t)
				actor := dbfx.Agent(t, "Bob's actor", actorRT, testutil.Cols{"owner_id": bob})
				parent := dbfx.Task(t, actor, testutil.Cols{"runtime_id": actorRT, "status": "running", "originator_user_id": bob, "accountable_user_id": bob, "originator_source": "direct_human"})
				target := dbfx.Agent(t, "Bob's target", rt, testutil.Cols{"owner_id": bob, "task_token_templates": `["erp"]`})
				assignee := target
				if targetType == "squad" {
					assignee = dbfx.Squad(t, "Identity squad", target)
				}
				cols := testutil.Cols{"creator_type": "member", "creator_id": testUserID, "status": "todo"}
				updates := map[string]any{"assignee_type": targetType, "assignee_id": assignee}
				if action == "promote" || action == "batch_promote" {
					cols["status"], cols["assignee_type"], cols["assignee_id"] = "backlog", targetType, assignee
					updates = map[string]any{"status": "todo"}
				}
				issue := dbfx.Issue(t, "Alice's issue", cols)
				dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issue)
				dbfx.Cleanup(t, `DELETE FROM activity_log WHERE issue_id = $1`, issue)
				req := withURLParam(newRequestAs(bob, http.MethodPatch, "/api/issues/"+issue, updates), "id", issue)
				handler := testHandler.UpdateIssue
				if action == "batch_assign" || action == "batch_promote" {
					req = newRequestAs(bob, http.MethodPatch, "/api/issues/batch", map[string]any{"issue_ids": []string{issue}, "updates": updates})
					handler = testHandler.BatchUpdateIssues
				}
				req.Header.Set("X-Actor-Source", "task_token")
				req.Header.Set("X-Agent-ID", actor)
				req.Header.Set("X-Task-ID", parent)
				testutil.Call(t, handler, req).Want(http.StatusOK)
				taskID, originator, ok := queuedTaskFor(t, issue, target)
				if !ok {
					t.Fatal("assignment did not enqueue")
				}
				task := loadTaskRow(t, taskID)
				if uuidToString(originator) != bob || task.OriginatorSource.String != "delegation" || uuidToString(task.DelegatedFromTaskID) != parent {
					t.Fatalf("assignment lost acting human or lineage: originator=%s source=%s parent=%s", uuidToString(originator), task.OriginatorSource.String, uuidToString(task.DelegatedFromTaskID))
				}
				claimReq := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+rt+"/tasks/claim", nil, testWorkspaceID, "identity-test")
				claimReq.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilityTaskIdentityTokensV1)
				claimReq = withURLParams(claimReq, "runtimeId", rt)
				var out struct {
					Task *AgentTaskResponse `json:"task"`
				}
				testutil.Call(t, testHandler.ClaimTaskByRuntime, claimReq).Want(http.StatusOK).JSON(&out)
				if out.Task == nil || out.Task.TaskTokens["BOT_TOKEN_ERP"] == "" {
					t.Fatal("claim did not deliver identity token")
				}
				claims := decodeJWTPayload(t, out.Task.TaskTokens["BOT_TOKEN_ERP"])
				if claims["sub"] != email {
					t.Fatalf("claim signed subject %v, want acting human %s", claims["sub"], email)
				}
			})
		}
	}
}

func TestTaskTokenAssignmentUnprovenActorCannotBorrowCreator(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withTaskTokenCatalog(t, `[{"id":"erp","label":"ERP","env":"BOT_TOKEN_ERP","claims":{"sub":"{{identity.email}}"}}]`)
	for _, state := range []string{"missing", "completed", "failed", "cancelled", "unattributed", "wrong_agent", "foreign_workspace"} {
		t.Run(state, func(t *testing.T) {
			rt := handlerTestRuntimeID(t)
			actor := dbfx.Agent(t, "Unproven actor", rt)
			target := dbfx.Agent(t, "Unproven target", rt, testutil.Cols{"task_token_templates": `["erp"]`})
			issueID := dbfx.Issue(t, "Do not borrow creator", testutil.Cols{"creator_type": "member", "creator_id": testUserID, "assignee_type": "agent", "assignee_id": target})
			issue, err := testHandler.Queries.GetIssue(t.Context(), parseUUID(issueID))
			if err != nil {
				t.Fatal(err)
			}
			cols := testutil.Cols{"runtime_id": rt, "status": "running", "originator_user_id": testUserID, "accountable_user_id": testUserID, "originator_source": "direct_human"}
			if state == "completed" || state == "failed" || state == "cancelled" {
				cols["status"] = state
			}
			if state == "unattributed" {
				cols["originator_user_id"], cols["originator_source"] = nil, "owner_fallback"
			}
			sourceAgent := actor
			if state == "wrong_agent" {
				sourceAgent = target
			}
			if state == "foreign_workspace" {
				ws := dbfx.Workspace(t, "Foreign identity workspace", fmt.Sprintf("foreign-identity-%d", time.Now().UnixNano()))
				sourceAgent = dbfx.Agent(t, "Foreign actor", rt, testutil.Cols{"workspace_id": ws})
			}
			parent := parseUUID(dbfx.Task(t, sourceAgent, cols))
			if state == "missing" {
				parent.Valid = false
			}
			attr := testHandler.TaskService.AttributionForIssueAssignment(t.Context(), issue, "agent", actor, parent)
			task, err := testHandler.TaskService.EnqueueTaskForIssueWithHandoff(t.Context(), issue, "", attr)
			dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
			if err != nil {
				t.Fatal(err)
			}
			if task.OriginatorUserID.Valid {
				t.Fatal("unproven actor borrowed a human")
			}
			if got := testHandler.issueTaskTokens(t.Context(), &task, loadAgentRow(t, target), testWorkspaceID); len(got) != 0 {
				t.Fatal("unproven assignment received token")
			}
		})
	}
}
