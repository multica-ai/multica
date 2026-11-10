package service

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/projectaccess"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// An autopilot bound to a private project (MUL-8472) must not run for a human
// who cannot see the project: create_issue used to file the issue into it and
// only then have the enqueue refuse, and run_only handed the project's context
// to a run with no check at all.
func TestAutopilotDispatch_PrivateProjectNeedsTheRunsHuman(t *testing.T) {
	for _, mode := range []string{"run_only", "create_issue"} {
		t.Run(mode, func(t *testing.T) {
			fx, creatorA := newPrincipalFixture(t)
			triggerOwnerB := fx.member(t, "project-b")
			projectID := fx.Project(t, "private ap project", testutil.Cols{"visibility": projectaccess.VisibilityPrivate})
			fx.InsertNoID(t, "project_member", testutil.Cols{
				"workspace_id": fx.WorkspaceID,
				"project_id":   projectID,
				"user_id":      creatorA,
			}, "project_id = $1 AND user_id = $2", projectID, creatorA)

			// B owns the agent and the trigger, so invocation admits B; only
			// the project stands between B and the run.
			agentID := fx.privateAgentOwnedBy(t, triggerOwnerB, "project-"+mode)
			autopilotID, triggerID := fx.autopilotWithTrigger(t, agentID, creatorA, triggerOwnerB)
			fx.Exec(t, `UPDATE autopilot SET project_id = $2, execution_mode = $3, issue_title_template = 'ap issue' WHERE id = $1`,
				autopilotID, projectID, mode)

			run := fx.dispatch(t, autopilotID, triggerID)
			if run.Status != "skipped" || run.ReasonCode.String != "project_access_blocked" {
				t.Fatalf("dispatch = %q (%q), want skipped with project_access_blocked", run.Status, run.ReasonCode.String)
			}
			if n := fx.Count(t, `SELECT count(*) FROM issue WHERE project_id = $1`, projectID); n != 0 {
				t.Errorf("refused dispatch still filed %d issues into the private project", n)
			}
			if n := fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE autopilot_run_id = $1`, run.ID); n != 0 {
				t.Errorf("refused dispatch still enqueued %d tasks", n)
			}

			// Once B can see the project, the same autopilot runs.
			fx.InsertNoID(t, "project_member", testutil.Cols{
				"workspace_id": fx.WorkspaceID,
				"project_id":   projectID,
				"user_id":      triggerOwnerB,
			}, "project_id = $1 AND user_id = $2", projectID, triggerOwnerB)
			fx.Cleanup(t, `DELETE FROM issue WHERE project_id = $1`, projectID)
			fx.Cleanup(t, `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)
			run = fx.dispatch(t, autopilotID, triggerID)
			if run.Status == "skipped" {
				t.Fatalf("dispatch refused (%q) for a project member", run.FailureReason.String)
			}
		})
	}
}
