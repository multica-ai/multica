package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func seededHandlerTestAgent(t *testing.T) string {
	t.Helper()
	var agentID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = 'Handler Test Agent'`, testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load seeded agent: %v", err)
	}
	return agentID
}

// Runs without an issue read a private project too: a run-only autopilot on
// it and a quick-create into it. Their transcript, cancel and run history
// follow the project exactly like a run on one of its issues.
func TestPrivateProjectIssuelessRunsNeedVisibility(t *testing.T) {
	f := newPrivateProjectFixture(t)
	agentID := seededHandlerTestAgent(t)

	runOnly := func() string {
		autopilotID := dbfx.Insert(t, "autopilot", testutil.Cols{
			"workspace_id":    testWorkspaceID,
			"title":           f.marker + " secret autopilot",
			"assignee_id":     agentID,
			"execution_mode":  "run_only",
			"project_id":      f.projectID,
			"created_by_type": "member",
			"created_by_id":   f.insider,
		})
		runID := dbfx.Insert(t, "autopilot_run", testutil.Cols{
			"autopilot_id": autopilotID,
			"source":       "manual",
			"status":       "running",
		})
		return dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id":       testRuntimeID,
			"autopilot_run_id": runID,
			"result":           testutil.Raw(`'{"output":"secret result"}'::jsonb`),
		})
	}
	quickCreate := func() string {
		ctx, err := json.Marshal(map[string]string{
			"type":         "quick_create",
			"prompt":       f.marker + " secret prompt",
			"requester_id": f.insider,
			"workspace_id": testWorkspaceID,
			"project_id":   f.projectID,
		})
		if err != nil {
			t.Fatal(err)
		}
		return dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id": testRuntimeID,
			"context":    ctx,
		})
	}

	for _, tc := range []struct {
		name string
		run  func() string
	}{
		{"run-only autopilot", runOnly},
		{"quick-create", quickCreate},
	} {
		t.Run(tc.name, func(t *testing.T) {
			taskID := tc.run()

			transcript := func(userID string) int {
				req := withURLParam(asUser(newRequest(http.MethodGet, "/api/tasks/"+taskID+"/messages", nil), userID), "taskId", taskID)
				req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{}))
				rec := httptest.NewRecorder()
				testHandler.ListTaskMessagesByUser(rec, req)
				return rec.Code
			}
			if got := transcript(f.outsider); got != http.StatusNotFound {
				t.Errorf("outsider transcript = %d, want 404", got)
			}
			if got := transcript(f.insider); got != http.StatusOK {
				t.Errorf("insider transcript = %d, want 200", got)
			}

			history := func(userID string) AgentTaskResponse {
				req := withURLParam(asUser(newRequest(http.MethodGet, "/api/agents/"+agentID+"/tasks", nil), userID), "id", agentID)
				var tasks []AgentTaskResponse
				testutil.Call(t, testHandler.ListAgentTasks, req).Want(http.StatusOK).JSON(&tasks)
				for _, task := range tasks {
					if task.ID == taskID {
						return task
					}
				}
				t.Fatalf("run %s missing from %s's history", taskID, userID)
				return AgentTaskResponse{}
			}
			if hidden := history(f.outsider); !hidden.Restricted || hidden.Result != nil || hidden.AutopilotRunID != "" {
				t.Errorf("outsider run = restricted %v result %v autopilot run %q; want a redacted run",
					hidden.Restricted, hidden.Result, hidden.AutopilotRunID)
			}
			if visible := history(f.insider); visible.Restricted {
				t.Errorf("insider run is restricted")
			}

			rec := httptest.NewRecorder()
			testHandler.CancelTaskByUser(rec, cancelTaskByUserRequest(t, f.outsider, taskID))
			if rec.Code != http.StatusNotFound {
				t.Errorf("outsider cancel = %d, want 404", rec.Code)
			}
			if got := taskStatus(t, taskID); got != "queued" {
				t.Errorf("run after outsider cancel = %q, want queued", got)
			}
			rec = httptest.NewRecorder()
			testHandler.CancelTaskByUser(rec, cancelTaskByUserRequest(t, f.insider, taskID))
			if rec.Code != http.StatusOK {
				t.Errorf("insider cancel = %d, want 200: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// Workspace views of what agents are working on name the issues they run on;
// a private project's issues drop out for members who cannot see it, while
// the agent still reads as working.
func TestPrivateProjectWorkingIssuesHiddenFromAggregates(t *testing.T) {
	f := newPrivateProjectFixture(t)
	agentID := createHandlerTestAgent(t, f.marker+"-worker", []byte(`{}`))
	dbfx.Task(t, agentID, testutil.Cols{
		"issue_id":      f.secretIssue,
		"runtime_id":    handlerTestRuntimeID(t),
		"status":        "running",
		"dispatched_at": testutil.Raw("now()"),
		"started_at":    testutil.Raw("now()"),
	})

	workingIssues := func(userID string) (issueIDs []string, found bool) {
		req := asUser(newRequest(http.MethodGet, "/api/working-agents?type=issue", nil), userID)
		var agents []WorkspaceWorkingAgent
		testutil.Call(t, testHandler.ListWorkspaceWorkingAgents, req).Want(http.StatusOK).JSON(&agents)
		for _, a := range agents {
			if a.ID == agentID {
				return a.IssueIDs, true
			}
		}
		return nil, false
	}
	if ids, found := workingIssues(f.outsider); !found || slices.Contains(ids, f.secretIssue) {
		t.Errorf("outsider working agent found=%v issue_ids=%v; want the agent without the private issue", found, ids)
	}
	if ids, found := workingIssues(f.insider); !found || !slices.Contains(ids, f.secretIssue) {
		t.Errorf("insider working agent found=%v issue_ids=%v; want the private issue", found, ids)
	}

	squadID := createCommentTriggerPreviewSquad(t, f.marker+" squad", agentID)
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO squad_member (squad_id, member_type, member_id) VALUES ($1, 'agent', $2)`, squadID, agentID,
	); err != nil {
		t.Fatalf("insert squad member: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad_member WHERE squad_id = $1`, squadID)
	})
	squadMember := func(userID string) SquadMemberStatusResponse {
		req := squadScopeReq(userID, http.MethodGet, "/api/squads/"+squadID+"/members/status", nil, map[string]string{"id": squadID})
		var resp SquadMemberStatusListResponse
		testutil.Call(t, testHandler.ListSquadMemberStatus, req).Want(http.StatusOK).JSON(&resp)
		if len(resp.Members) != 1 {
			t.Fatalf("squad members = %+v, want one", resp.Members)
		}
		return resp.Members[0]
	}
	hidden := squadMember(f.outsider)
	if len(hidden.ActiveIssues) != 0 || hidden.Status == nil || *hidden.Status != "working" {
		t.Errorf("outsider squad member = status %v issues %+v; want working with no issues", hidden.Status, hidden.ActiveIssues)
	}
	visible := squadMember(f.insider)
	if len(visible.ActiveIssues) != 1 || visible.ActiveIssues[0].IssueID != f.secretIssue {
		t.Errorf("insider squad member issues = %+v, want the private issue", visible.ActiveIssues)
	}
}
