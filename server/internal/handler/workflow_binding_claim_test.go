package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Exercise the native claim response over HTTP, then decode that very task
// into the installed daemon's DTO and render its actual per-turn prompt.
func TestClaimWorkflowBindingReachesDaemonPrompt(t *testing.T) {
	for _, tc := range []struct {
		name, workflow, role, bound, current string
		legacy, null                         bool
	}{
		{name: "named assignee", workflow: "named", bound: "in_progress", current: "blocked"},
		{name: "named legacy", workflow: "named", bound: "in_progress", current: "blocked", legacy: true},
		{name: "default handoff", workflow: "handoff", bound: "in_progress", current: "blocked"},
		{name: "default legacy", workflow: "handoff", bound: "in_progress", current: "blocked", legacy: true},
		{name: "observer", workflow: "named", role: "observer", bound: "in_progress", current: "blocked"},
		{name: "owning squad", workflow: "named", role: "squad", bound: "in_progress", current: "blocked"},
		{name: "owning squad legacy", workflow: "handoff", role: "squad", bound: "in_progress", current: "blocked", legacy: true},
		{name: "handlerless current", workflow: "named", bound: "in_progress", current: "todo"},
		{name: "removed old key", workflow: "named", bound: "removed_step", current: "blocked"},
		{name: "same step", workflow: "named", bound: "in_progress", current: "in_progress"},
		{name: "null binding", workflow: "named", current: "in_progress", null: true},
		{name: "quiet default", workflow: "quiet", bound: "todo", current: "in_progress"},
		{name: "no project", bound: "todo", current: "in_progress"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seedTestCatalog(t)
			resetDefaultWorkflow(t)
			ctx := context.Background()
			runtimeID := createClaimReclaimRuntime(t, ctx, tc.name)
			agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, tc.name)
			projectID := ""
			if tc.workflow != "" {
				projectID = createWorkflowTestProject(t, "Binding "+tc.name)
				if tc.workflow == "named" {
					wf := createTestWorkflowSteps(t, "Binding "+workflowTestSuffix(), []map[string]any{
						{"status_key": "todo"},
						{"status_key": "in_progress", "handler": map[string]any{"type": "agent", "id": agentID}, "instructions": "Current instructions must not leak."},
						{"status_key": "blocked", "instructions": "Current instructions must not leak."},
					})
					setProjectWorkflow(t, projectID, map[string]any{"workflow_id": wf.ID}).Want(http.StatusOK)
				} else if tc.workflow == "handoff" {
					setColumnHandoff(t, projectID, "in_progress", handToAgent(agentID, "Current instructions must not leak.")).Want(http.StatusOK)
				}
				dbfx.Exec(t, `UPDATE issue SET project_id=$2 WHERE id=$1`, issueID, projectID)
			}
			assigneeID, assigneeType, squadID := agentID, "agent", ""
			if tc.role == "observer" {
				assigneeID = dbfx.Agent(t, "Binding owner", runtimeID)
			} else if tc.role == "squad" {
				squadID = dbfx.Squad(t, "Binding squad", agentID)
				assigneeID, assigneeType = squadID, "squad"
			}
			dbfx.Exec(t, `UPDATE issue SET assignee_type=$2,assignee_id=$3,status='in_progress' WHERE id=$1`, issueID, assigneeType, assigneeID)
			cols := testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "handoff_note": "Original task scope."}
			if !tc.null {
				cols["workflow_step"] = tc.bound
			}
			if squadID != "" {
				cols["squad_id"], cols["is_leader_task"] = squadID, true
			}
			taskID := dbfx.Task(t, agentID, cols)
			// A member moves the issue after the task has its saved binding.
			updateIssueForTest(t, issueID, map[string]any{"status": tc.current}).Want(http.StatusOK)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				testHandler.ClaimTaskByRuntime(w, withURLParam(r, "runtimeId", runtimeID))
			}))
			defer srv.Close()
			req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("X-User-ID", testUserID)
			req.Header.Set("X-Workspace-ID", testWorkspaceID)
			if !tc.legacy {
				req.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilityWorkflowStatusRulesV1)
			}
			res, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil || res.StatusCode != http.StatusOK {
				t.Fatalf("claim: %d %s / %v", res.StatusCode, raw, err)
			}
			var wire struct {
				Task json.RawMessage `json:"task"`
			}
			if err := json.Unmarshal(raw, &wire); err != nil {
				t.Fatal(err)
			}
			var task daemon.Task
			if err := json.Unmarshal(wire.Task, &task); err != nil {
				t.Fatal(err)
			}
			if task.ID != taskID || task.IssueID != issueID || task.HandoffNote != "Original task scope." {
				t.Fatalf("claim lost task metadata: %s", raw)
			}
			prompt := daemon.BuildPrompt(task, "codex")
			if !strings.Contains(prompt, "Original task scope.") {
				t.Fatal("prompt lost handoff note")
			}
			guarded := tc.workflow == "named" || tc.workflow == "handoff"
			if !guarded {
				if task.ProjectWorkflow != nil || strings.Contains(prompt, "[Project workflow]") {
					t.Fatalf("unexpected workflow: %s", raw)
				}
				return
			}
			var fields struct {
				Workflow map[string]json.RawMessage `json:"project_workflow"`
			}
			if err := json.Unmarshal(wire.Task, &fields); err != nil {
				t.Fatal(err)
			}
			if tc.name == "named assignee" || tc.name == "owning squad legacy" {
				workflowJSON, err := json.Marshal(fields.Workflow)
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("HTTP claim project_workflow JSON: %s\nActual daemon BuildPrompt:\n%s", workflowJSON, prompt)
			}
			var current string
			if err := json.Unmarshal(fields.Workflow["current_status_key"], &current); err != nil || current != tc.current {
				t.Errorf("current status = %q / %v", current, err)
			}
			bound, present := fields.Workflow["task_workflow_step"]
			if !present {
				t.Error("missing nullable binding")
			}
			if present && tc.null {
				if string(bound) != "null" {
					t.Fatalf("NULL invented a binding: %s", raw)
				}
			} else if present {
				var got string
				if err := json.Unmarshal(bound, &got); err != nil || got != tc.bound {
					t.Fatalf("bound = %q / %v", got, err)
				}
			}
			moved := !tc.null && tc.bound != tc.current
			if moved {
				for _, want := range []string{"[Project workflow]", "issue current step:", tc.current, "this task's bound step:", tc.bound, "Do not change the issue's status in this run"} {
					if !strings.Contains(prompt, want) {
						t.Errorf("prompt missing %q:\n%s", want, prompt)
					}
				}
				for _, forbidden := range []string{"Your moves:", "Step done", "Needs changes", "Can't proceed", "Starting work", "multica issue status", "Current instructions must not leak."} {
					if strings.Contains(task.ProjectWorkflow.Brief, forbidden) {
						t.Errorf("stale brief includes %q:\n%s", forbidden, prompt)
					}
				}
				if tc.role == "squad" {
					var native AgentTaskResponse
					if err := json.Unmarshal(wire.Task, &native); err != nil {
						t.Fatal(err)
					}
					if native.Agent == nil || !strings.Contains(native.Agent.Instructions, "this run has no status-move commands") || strings.Contains(native.Agent.Instructions, "run that block's Step done command") {
						t.Fatalf("contradictory squad instruction: %s", raw)
					}
				}
				if tc.role == "observer" && !strings.Contains(prompt, "You are not its assignee") {
					t.Fatal("observer gained ownership")
				}
				if tc.legacy && !strings.Contains(prompt, "replaces the status rules in your runtime instructions") {
					t.Fatal("lost legacy override")
				}
				var taskBefore, issueBefore string
				dbfx.QueryRow(t, `SELECT to_jsonb(t)::text FROM agent_task_queue t WHERE id=$1`, taskID).Scan(&taskBefore)
				dbfx.QueryRow(t, `SELECT to_jsonb(i)::text FROM issue i WHERE id=$1`, issueID).Scan(&issueBefore)
				body := agentUpdateIssue(t, issueID, agentID, taskID, map[string]any{"status": "done"}).Want(http.StatusConflict).Map()
				if body["code"] != "workflow_step_moved" || body["step"] != tc.bound || body["current_status"] != tc.current {
					t.Fatalf("guard changed: %v", body)
				}
				var taskAfter, issueAfter string
				dbfx.QueryRow(t, `SELECT to_jsonb(t)::text FROM agent_task_queue t WHERE id=$1`, taskID).Scan(&taskAfter)
				dbfx.QueryRow(t, `SELECT to_jsonb(i)::text FROM issue i WHERE id=$1`, issueID).Scan(&issueAfter)
				if taskAfter != taskBefore || issueAfter != issueBefore {
					t.Fatal("rejected status move mutated the issue or task row")
				}
			} else if !strings.Contains(prompt, "Your moves:") || strings.Contains(prompt, "this task's bound step:") {
				t.Fatalf("normal brief changed:\n%s", prompt)
			}
			if status, _, _ := issueStatusAndAssignee(t, issueID); status != tc.current {
				t.Fatalf("claim or rejection moved issue to %s", status)
			}
			var saved *string
			dbfx.QueryRow(t, `SELECT workflow_step FROM agent_task_queue WHERE id=$1`, taskID).Scan(&saved)
			if tc.null && saved != nil || !tc.null && (saved == nil || *saved != tc.bound) {
				t.Fatalf("claim or rejection rebound task: %v", saved)
			}
		})
	}
}
