package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// MUL-6704, second half. Moving an agent never rewrote
// agent_task_queue.runtime_id, and since #7571 the claim fence requires the two to
// agree — so queued rows became invisible to the new machine and refused by the
// old one, then failed hours later as `queued_expired`, which describes a busy
// queue rather than a rebind.

// Both halves of the decision: settle what can no longer be claimed, leave what is
// already running alone.
func TestUpdateAgentRebind_SettlesStrandedQueuedTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	oldRuntimeID := dbfx.Runtime(t, "Rebind Old Runtime")
	newRuntimeID := dbfx.Runtime(t, "Rebind New Runtime")
	agentID := dbfx.Agent(t, "Rebind Agent", oldRuntimeID)

	queued := insertFixtureTask(t, ctx, oldRuntimeID, agentID, "queued", false)
	waiting := insertFixtureTask(t, ctx, oldRuntimeID, agentID, "waiting_local_directory", false)
	running := insertFixtureTask(t, ctx, oldRuntimeID, agentID, "running", false)

	if w := rebindAgent(t, agentID, newRuntimeID); w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent rebind: got %d, want 200: %s", w.Code, w.Body.String())
	}

	for _, tc := range []struct {
		name   string
		taskID string
		status string
		reason string
	}{
		{"queued row is unclaimable after the move", queued, "cancelled", string(taskfailure.ReasonAgentRuntimeChanged)},
		{"waiting row is unclaimable after the move", waiting, "cancelled", string(taskfailure.ReasonAgentRuntimeChanged)},
		// Already handed to the old machine and executing there. CompleteAgentTask
		// does not check the binding, so it still finishes correctly — cancelling
		// it would throw away work the user never asked to stop.
		{"running row keeps executing on the old runtime", running, "running", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var status string
			var reason, errText pgtype.Text
			dbfx.QueryRow(t, `SELECT status, failure_reason, error FROM agent_task_queue WHERE id = $1`, tc.taskID).
				Scan(&status, &reason, &errText)
			if status != tc.status {
				t.Fatalf("status = %q, want %q", status, tc.status)
			}
			if tc.reason == "" {
				return
			}
			if reason.String != tc.reason {
				t.Fatalf("failure_reason = %q, want %q", reason.String, tc.reason)
			}
			if errText.String != RebindStrandedTaskError {
				t.Fatalf("error text = %q, want the shared rebind sentence", errText.String)
			}
		})
	}

	// A no-op resubmit of the current runtime is not a rebind: a PATCH-as-PUT
	// client echoing the unchanged runtime_id back must not cancel anything.
	survivor := insertFixtureTask(t, ctx, newRuntimeID, agentID, "queued", false)
	if w := rebindAgent(t, agentID, newRuntimeID); w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent no-op runtime resubmit: got %d, want 200: %s", w.Code, w.Body.String())
	}
	if status, _, _ := taskOutcome(t, survivor); status != "queued" {
		t.Fatalf("no-op resubmit cancelled a live task (status %q)", status)
	}
}

// Whether an agent may live on a private machine is decided by the AGENT OWNER,
// not the operator: the workspace owner here may edit the agent and owns the
// runtime, and is still refused, because the claim fence would refuse every task.
func TestUpdateAgentRebind_RefusesForeignPrivateRuntime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	privateRuntimeID := dbfx.Runtime(t, "Rebind Private Runtime")
	publicRuntimeID := dbfx.Runtime(t, "Rebind Public Runtime", testutil.Cols{"visibility": "public"})
	teammateID := dbfx.User(t, "Rebind Teammate", "rebind-teammate-"+privateRuntimeID+"@multica.ai")
	dbfx.Member(t, testWorkspaceID, teammateID, "member")
	foreignAgentID := dbfx.Agent(t, "Rebind Foreign Agent", publicRuntimeID, testutil.Cols{"owner_id": teammateID})

	w := rebindAgent(t, foreignAgentID, privateRuntimeID)
	if w.Code != http.StatusForbidden {
		t.Fatalf("binding a teammate's agent onto my private runtime: got %d, want 403: %s", w.Code, w.Body.String())
	}
	var boundRuntime string
	dbfx.QueryRow(t, `SELECT runtime_id FROM agent WHERE id = $1`, foreignAgentID).Scan(&boundRuntime)
	if boundRuntime != publicRuntimeID {
		t.Fatalf("refused rebind must not move the agent; runtime_id = %s", boundRuntime)
	}
}

// rebindAgent drives the real PATCH that moves an agent to another runtime.
func rebindAgent(t *testing.T, agentID, runtimeID string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/agents/"+agentID, map[string]any{"runtime_id": runtimeID})
	testHandler.UpdateAgent(w, withURLParam(req, "id", agentID))
	return w
}
