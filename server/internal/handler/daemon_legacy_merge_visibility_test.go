package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// The third way an agent could end up bound to a machine that refuses it
// (MUL-6704): a daemon switching to a UUID identity registers a NEW row, which
// defaults to private, and the merge moves every agent onto it — silently
// un-sharing a public machine and stranding teammates' agents there. An identity
// migration is the same machine, so the surviving row keeps the sharing the owner
// already chose; reclaiming it goes through the confirmed revoke.
func TestDaemonRegister_LegacyMergeVisibility(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// Inheritance is one-way: a shared machine stays shared across the identity
	// change, and a private one is never widened by a daemon restart.
	for _, tc := range []struct {
		name           string
		legacy         string
		wantVisibility string
	}{
		{"public legacy row keeps the machine shared", "public", "public"},
		{"private legacy row stays private", "private", "private"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			legacyRuntimeID, agentID, ownerID := legacyRuntimeWithAgent(t, tc.legacy)
			newRuntimeID := registerUnderNewIdentity(t, tc.legacy+"Machine.local", legacyRuntimeID)

			var agentRuntimeID string
			dbfx.QueryRow(t, `SELECT runtime_id FROM agent WHERE id = $1`, agentID).Scan(&agentRuntimeID)
			if agentRuntimeID != newRuntimeID {
				t.Fatalf("agent not reassigned by the merge: runtime_id=%s, want %s", agentRuntimeID, newRuntimeID)
			}
			if got := runtimeVisibility(t, newRuntimeID); got != tc.wantVisibility {
				t.Fatalf("merged runtime visibility = %q, want %q", got, tc.wantVisibility)
			}
			if tc.legacy != "public" {
				return
			}
			// The invariant the inheritance exists to protect, asserted the way the
			// rest of the system checks it: the migrated foreign agent must still be
			// runnable, or its work would queue and never be claimed.
			ctx := context.Background()
			runtime, err := testHandler.Queries.GetAgentRuntime(ctx, parseUUID(newRuntimeID))
			if err != nil {
				t.Fatalf("load merged runtime: %v", err)
			}
			agent, err := testHandler.Queries.GetAgent(ctx, parseUUID(agentID))
			if err != nil {
				t.Fatalf("load agent: %v", err)
			}
			if !service.RuntimeAllowsAgentOwner(runtime, agent.OwnerID) {
				t.Fatalf("the merged runtime refuses an agent it just absorbed")
			}
			verdict, err := service.AgentReadiness(ctx, testHandler.Queries, agent)
			if err != nil {
				t.Fatalf("AgentReadiness: %v", err)
			}
			if verdict.Blocked() {
				t.Fatalf("admission blocks the migrated agent (reason %q); a daemon restart must not cost a teammate their runtime", verdict.Reason)
			}
			_ = ownerID
		})
	}
}

// A machine that changed hands is not one machine changing its identity key, and
// the merge must abandon it wholesale — BEFORE anything moves.
//
// The previous version of this fix only blocked the `public` inheritance, so the
// merge still reassigned the old owner's agents and tasks onto the new owner's
// private row: bound, unclaimable, and without the confirmation the revoke flow
// requires. That is the exact state this issue exists to remove, so the assertion
// here is zero writes on every side — agent, tasks, and the legacy row itself.
func TestDaemonRegister_LegacyMergeAbortsOnOwnerChange(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	previousOwnerID := dbfx.User(t, "Legacy Merge Previous Owner",
		fmt.Sprintf("legacy-merge-prev-owner-%d@multica.ai", time.Now().UnixNano()))
	dbfx.Member(t, testWorkspaceID, previousOwnerID, "member")
	legacyPublicID := dbfx.Runtime(t, "legacy-public-other-owner", testutil.Cols{
		"daemon_id":    "OwnerChange.local",
		"runtime_mode": "local",
		"provider":     "claude",
		"status":       "offline",
		"device_info":  "OwnerChange.local",
		"visibility":   "public",
		"owner_id":     previousOwnerID,
		"last_seen_at": testutil.Raw("now() - interval '1 hour'"),
	})
	agentID := dbfx.Agent(t, "legacy-owner-change-agent", legacyPublicID, ownedBy(previousOwnerID))
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": legacyPublicID, "status": "completed",
		"completed_at": testutil.Raw("now()")})

	// The fresh registration belongs to the suite user, not previousOwnerID.
	newRuntimeID := registerUnderNewIdentity(t, "OwnerChange.local", legacyPublicID)

	if got := runtimeVisibility(t, newRuntimeID); got != "private" {
		t.Fatalf("merged runtime visibility = %q, want 'private': sharing does not transfer with the machine", got)
	}
	// Nothing moved, and the legacy row survives — the agents keep running where
	// they already work, and resolving this stays an explicit action.
	var agentRuntimeID, taskRuntimeID string
	dbfx.QueryRow(t, `SELECT runtime_id FROM agent WHERE id = $1`, agentID).Scan(&agentRuntimeID)
	dbfx.QueryRow(t, `SELECT runtime_id FROM agent_task_queue WHERE id = $1`, taskID).Scan(&taskRuntimeID)
	if agentRuntimeID != legacyPublicID {
		t.Fatalf("agent runtime_id = %s, want it left on the legacy row %s", agentRuntimeID, legacyPublicID)
	}
	if taskRuntimeID != legacyPublicID {
		t.Fatalf("task runtime_id = %s, want it left on the legacy row %s", taskRuntimeID, legacyPublicID)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_runtime WHERE id = $1`, legacyPublicID); n != 1 {
		t.Fatalf("legacy runtime rows = %d, want it kept: deleting it would strand the history it still owns", n)
	}
	// And the agent is still runnable where it is — a public machine owned by its
	// own owner — which is the point of refusing the merge.
	ctx := context.Background()
	legacyRuntime, err := testHandler.Queries.GetAgentRuntime(ctx, parseUUID(legacyPublicID))
	if err != nil {
		t.Fatalf("load legacy runtime: %v", err)
	}
	agent, err := testHandler.Queries.GetAgent(ctx, parseUUID(agentID))
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}
	if !service.RuntimeAllowsAgentOwner(legacyRuntime, agent.OwnerID) {
		t.Fatalf("the agent must remain runnable on the machine it was already using")
	}
}

// legacyRuntimeWithAgent seeds a machine under its old hostname identity with one
// agent on it. For the public case the agent belongs to a teammate — the binding
// that is legal on a shared machine and must survive the merge.
func legacyRuntimeWithAgent(t *testing.T, visibility string) (runtimeID, agentID, ownerID string) {
	t.Helper()
	daemonID := visibility + "Machine.local"
	runtimeID = dbfx.Runtime(t, "legacy-"+visibility+"-runtime", testutil.Cols{
		"daemon_id":    daemonID,
		"runtime_mode": "local",
		"provider":     "claude",
		"status":       "offline",
		"device_info":  daemonID,
		"visibility":   visibility,
		"last_seen_at": testutil.Raw("now() - interval '1 hour'"),
	})
	cols := testutil.Cols{}
	if visibility == "public" {
		ownerID = dbfx.User(t, "Legacy Merge Teammate", "legacy-merge-"+runtimeID+"@multica.ai")
		dbfx.Member(t, testWorkspaceID, ownerID, "member")
		cols = ownedBy(ownerID)
	}
	agentID = dbfx.Agent(t, "legacy-merge-agent-"+runtimeID, runtimeID, cols)
	return runtimeID, agentID, ownerID
}

// registerUnderNewIdentity re-registers the same machine under a stable UUID,
// declaring the old hostname id as legacy, and returns the surviving runtime id.
func registerUnderNewIdentity(t *testing.T, legacyDaemonID, legacyRuntimeID string) string {
	t.Helper()
	req := newRequest("POST", "/api/daemon/register", map[string]any{
		"workspace_id":      testWorkspaceID,
		"daemon_id":         "0192a7a0-9ab3-7c3f-9f1c-" + legacyRuntimeID[len(legacyRuntimeID)-12:],
		"legacy_daemon_ids": []string{legacyDaemonID},
		"device_name":       legacyDaemonID,
		"runtimes": []map[string]any{
			{"name": "merged-runtime", "type": "claude", "version": "1.0.0", "status": "online"},
		},
	})
	w := testutil.Call(t, testHandler.DaemonRegister, req).Want(http.StatusOK)

	var resp map[string]any
	w.JSON(&resp)
	newRuntimeID := resp["runtimes"].([]any)[0].(map[string]any)["id"].(string)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE runtime_id = $1`, newRuntimeID)
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, newRuntimeID)
	})
	if newRuntimeID == legacyRuntimeID {
		t.Fatalf("expected a new runtime row, got the legacy id back")
	}
	return newRuntimeID
}
