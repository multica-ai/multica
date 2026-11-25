package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// A step's handler that can no longer take work (an archived agent, a member
// who left) must not stop issues from entering the step: the issue moves and
// keeps its assignee, as when a relative handler has nobody to resolve to.
func TestWorkflowStepWithUnassignableHandlerSkipsHandoff(t *testing.T) {
	seedTestCatalog(t)
	var runtimeID string
	dbfx.QueryRow(t, `SELECT runtime_id::text FROM agent WHERE id = $1`, seededReadyAgentID(t)).Scan(&runtimeID)

	cases := []struct {
		name  string
		setup func(t *testing.T, projectID string) (handler map[string]any, retire func())
	}{
		{"archived agent", func(t *testing.T, _ string) (map[string]any, func()) {
			agentID := dbfx.Agent(t, "Retired handler "+workflowTestSuffix(), runtimeID)
			return map[string]any{"type": "agent", "id": agentID}, func() {
				dbfx.Exec(t, `UPDATE agent SET archived_at = now() WHERE id = $1`, agentID)
			}
		}},
		{"active squad with an archived leader", func(t *testing.T, _ string) (map[string]any, func()) {
			leaderID := dbfx.Agent(t, "Retired squad leader "+workflowTestSuffix(), runtimeID)
			squadID := dbfx.Squad(t, "Squad with retired leader "+workflowTestSuffix(), leaderID)
			return map[string]any{"type": "squad", "id": squadID}, func() {
				dbfx.Exec(t, `UPDATE agent SET archived_at = now() WHERE id = $1`, leaderID)
			}
		}},
		{"removed member", func(t *testing.T, _ string) (map[string]any, func()) {
			userID, _ := addWorkflowTestMember(t, "leaver")
			return map[string]any{"type": "member", "id": userID}, func() {
				dbfx.Exec(t, `DELETE FROM member WHERE user_id = $1 AND workspace_id = $2`, userID, testWorkspaceID)
			}
		}},
		{"project lead is an archived agent", func(t *testing.T, projectID string) (map[string]any, func()) {
			agentID := dbfx.Agent(t, "Retired lead "+workflowTestSuffix(), runtimeID)
			dbfx.Exec(t, `UPDATE project SET lead_type = 'agent', lead_id = $1 WHERE id = $2`, agentID, projectID)
			return map[string]any{"type": "project_lead"}, func() {
				dbfx.Exec(t, `UPDATE agent SET archived_at = now() WHERE id = $1`, agentID)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wf := createTestWorkflow(t, "Stale handler "+workflowTestSuffix(), "in_progress")
			projectID := createWorkflowTestProject(t, "Stale handler")
			setProjectWorkflow(t, projectID, map[string]any{"workflow_id": wf.ID}).Want(http.StatusOK)
			handler, retire := tc.setup(t, projectID)
			setWorkflowHandoff(t, wf.ID, "in_progress", map[string]any{"handler": handler}).Want(http.StatusOK)
			retire()

			issueID := dbfx.Issue(t, "Enters a stale step", testutil.Cols{"project_id": projectID, "status": "todo"})
			updateIssueForTest(t, issueID, map[string]any{"status": "in_progress"}).Want(http.StatusOK)
			if status, assigneeType, assigneeID := issueStatusAndAssignee(t, issueID); status != "in_progress" || assigneeType != "" || assigneeID != "" {
				t.Fatalf("moved issue = %s, %q %q; want in_progress and still unassigned", status, assigneeType, assigneeID)
			}

			var created IssueResponse
			testutil.Call(t, testHandler.CreateIssue, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
				"title": "Created in a stale step", "status": "in_progress", "project_id": projectID,
			})).Want(http.StatusCreated).JSON(&created)
			if created.AssigneeType != nil || created.AssigneeID != nil {
				t.Fatalf("created issue assignee = %v %v; want unassigned", created.AssigneeType, created.AssigneeID)
			}
		})
	}
}
