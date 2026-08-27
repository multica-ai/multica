package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// The revoke leaves `kind='system'` carriers bound on purpose — unbinding one
// strands a row with no UI to repair it. Admission refuses them, but that check
// runs BEFORE the write transaction, so a revoke committing in between used to let
// a send insert a `queued` row that no runtime can ever claim: the exact
// silent-queue failure MUL-6704 removes, re-entered through the one agent kind the
// teardown deliberately keeps.
//
// Two independent fences close it, and both are pinned here because they protect
// different writers:
//
//   - ensureAgentRuntimeAccessTx, inside the chat/channel write transactions;
//   - the access predicate on CreateAgentTask, which covers every issue/mention
//     enqueue without each caller repeating the check.
//
// Neither takes a new lock: the writer already holds the agent row (FOR UPDATE for
// chat, FOR KEY SHARE via lock_task_owner_rows for the issue path), and locking the
// runtime afterwards would invert the revoke's runtime → agents order.
func TestRevokedRuntimeRefusesNewWork(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// A machine reclaimed as private with a foreign agent still bound — what the
	// teardown leaves for a retained carrier, reproduced directly so the test does
	// not depend on the revoke endpoint's own behaviour.
	runtimeID := dbfx.Runtime(t, "Revoked Access Runtime", testutil.Cols{"visibility": "private"})
	foreignUserID := dbfx.User(t, "Revoked Access Teammate", "revoked-access-"+runtimeID+"@multica.ai")
	dbfx.Member(t, testWorkspaceID, foreignUserID, "member")
	carrierID := dbfx.Agent(t, "Revoked Access Carrier", runtimeID, testutil.Cols{
		"owner_id":   foreignUserID,
		"kind":       "system",
		"system_key": "agent_builder:revoked-access",
	})

	carrier, err := testHandler.Queries.GetAgent(ctx, parseUUID(carrierID))
	if err != nil {
		t.Fatalf("load carrier: %v", err)
	}

	t.Run("admission refuses it", func(t *testing.T) {
		verdict, err := service.AgentReadiness(ctx, testHandler.Queries, carrier)
		if err != nil {
			t.Fatalf("AgentReadiness: %v", err)
		}
		if !verdict.Blocked() {
			t.Fatalf("verdict = %v, want blocked", verdict.Availability)
		}
	})

	// The issue / mention enqueue path. Its callers pin agent.RuntimeID and run
	// outside any agent-row transaction of their own, so the predicate on the
	// INSERT is what stops the doomed row.
	t.Run("the issue enqueue writes no row", func(t *testing.T) {
		issueID := dbfx.Issue(t, "revoked access enqueue")
		_, err := testHandler.Queries.CreateAgentTask(ctx, db.CreateAgentTaskParams{
			ID:        dbid.NewV7(),
			AgentID:   carrier.ID,
			RuntimeID: carrier.RuntimeID,
			IssueID:   parseUUID(issueID),
			Priority:  0,
		})
		if err == nil {
			t.Fatalf("CreateAgentTask succeeded; a task pinned to a runtime that refuses this agent can never be claimed")
		}
		if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE agent_id = $1`, carrierID); n != 0 {
			t.Fatalf("agent_task_queue rows = %d, want 0", n)
		}
	})

	// And the same predicate must NOT refuse the legitimate cases it sits next to.
	t.Run("still enqueues for runtimes that do permit the agent", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			cols testutil.Cols
		}{
			// Offline is a queue-and-wait, not a refusal: the daemon claims it on
			// reconnect. Refusing here would break every "assign work to an agent
			// whose laptop is asleep" flow.
			{"an offline private runtime owned by the agent's owner",
				testutil.Cols{"status": "offline", "owner_id": foreignUserID}},
			// Sharing is what public means, so a foreign owner is fine here.
			{"a public runtime owned by someone else", testutil.Cols{"visibility": "public"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				rt := dbfx.Runtime(t, "Revoked Access OK "+tc.name, tc.cols)
				agentID := dbfx.Agent(t, "Revoked Access OK Agent "+tc.name, rt, ownedBy(foreignUserID))
				agent, err := testHandler.Queries.GetAgent(ctx, parseUUID(agentID))
				if err != nil {
					t.Fatalf("load agent: %v", err)
				}
				issueID := dbfx.Issue(t, "revoked access ok "+tc.name)
				task, err := testHandler.Queries.CreateAgentTask(ctx, db.CreateAgentTaskParams{
					ID:        dbid.NewV7(),
					AgentID:   agent.ID,
					RuntimeID: agent.RuntimeID,
					IssueID:   parseUUID(issueID),
					Priority:  0,
				})
				if err != nil {
					t.Fatalf("CreateAgentTask refused a legitimate enqueue: %v", err)
				}
				dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE id = $1`, uuidToString(task.ID))
			})
		}
	})

	// The direct-chat write transaction, through the real service entry point.
	t.Run("the chat send writes neither message nor task", func(t *testing.T) {
		if testHandler.TaskService == nil {
			t.Skip("task service not wired")
		}
		sessionID := dbfx.ChatSession(t, carrierID, testutil.Cols{
			"creator_id": foreignUserID,
			"runtime_id": runtimeID,
		})
		session, err := testHandler.Queries.GetChatSession(ctx, parseUUID(sessionID))
		if err != nil {
			t.Fatalf("load chat session: %v", err)
		}

		_, err = testHandler.TaskService.SendDirectChatMessage(
			ctx, session, carrier, parseUUID(foreignUserID), "does this enqueue?", nil, "member", parseUUID(foreignUserID))
		if !errors.Is(err, service.ErrChatTaskRuntimeAccessRevoked) {
			t.Fatalf("SendDirectChatMessage error = %v, want ErrChatTaskRuntimeAccessRevoked", err)
		}
		if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE chat_session_id = $1`, sessionID); n != 0 {
			t.Fatalf("chat tasks = %d, want 0 — the send must not enqueue what nothing can claim", n)
		}
		if n := dbfx.Count(t, `SELECT count(*) FROM chat_message WHERE chat_session_id = $1`, sessionID); n != 0 {
			t.Fatalf("chat messages = %d, want 0 — a refused send must not leave the user's message behind either", n)
		}
	})
}
