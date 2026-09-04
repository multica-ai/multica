package handler

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// MUL-6920. Every resume pointer the claim hands back is runtime-gated: a
// session id belongs to a provider process on one machine and means nothing on
// another. Moving an agent to a different runtime therefore drops the pointer —
// correctly — but until now it dropped it in silence, and silence is
// indistinguishable from "this conversation has no history". The run started
// fresh, answered as if the earlier turns had never happened, and the user had
// to notice and tell it to go read the record (GitHub #7738).
//
// The disclosure flag already existed for the MUL-5305 rollout-missing case and
// already routes the run to the surface-correct continuity notice — the issue's
// comment history, Slack's channel, or `multica chat history` for a stored
// transcript. These tests pin the runtime-move case onto it, and pin the
// negatives that keep the notice from becoming per-turn noise.
func TestClaimTask_RuntimeMoveDisclosesDroppedSession(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	t.Run("issue follow-up across runtimes discloses", func(t *testing.T) {
		agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)
		oldRuntimeID := createRuntimeGuardRuntime(t, ctx, "kimi")

		issueID := dbfx.Issue(t, "runtime move issue", testutil.Cols{
			"status": "in_progress",
			"number": 81301,
		})
		// A healthy, resume-safe turn — on the machine the agent no longer runs on.
		dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id": oldRuntimeID,
			"issue_id":   issueID,
			"status":     "completed",
			"session_id": "moved-issue-session",
			"work_dir":   "/tmp/moved-issue-workdir",
		})
		dbfx.Exec(t, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
			VALUES ($1, $2, $3, 'queued', 1000)
		`, agentID, runtimeID, issueID)

		task := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
		if task.PriorSessionID != "" {
			t.Fatalf("PriorSessionID = %q, want empty (the session lives on the old runtime)", task.PriorSessionID)
		}
		// The workdir is still offered — a shared mount may resolve it — so the
		// response alone cannot tell the daemon whether context carried over.
		if task.PriorWorkDir != "/tmp/moved-issue-workdir" {
			t.Fatalf("PriorWorkDir = %q, want /tmp/moved-issue-workdir", task.PriorWorkDir)
		}
		if !task.PriorSessionResumeUnavailable {
			t.Fatal("issue follow-up on a new runtime must disclose the session it could not resume")
		}
	})

	t.Run("issue follow-up on the same runtime stays quiet", func(t *testing.T) {
		agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)

		issueID := dbfx.Issue(t, "runtime stay issue", testutil.Cols{
			"status": "in_progress",
			"number": 81302,
		})
		dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id": runtimeID,
			"issue_id":   issueID,
			"status":     "completed",
			"session_id": "same-issue-session",
			"work_dir":   "/tmp/same-issue-workdir",
		})
		dbfx.Exec(t, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
			VALUES ($1, $2, $3, 'queued', 1000)
		`, agentID, runtimeID, issueID)

		task := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
		if task.PriorSessionID != "same-issue-session" {
			t.Fatalf("PriorSessionID = %q, want same-issue-session", task.PriorSessionID)
		}
		if task.PriorSessionResumeUnavailable {
			t.Fatal("a resume that worked must not disclose a gap")
		}
	})

	// A first turn has nothing to lose. Without this the flag would fire on
	// every brand-new conversation, and a notice that always appears teaches
	// the agent to ignore the one that matters.
	t.Run("chat with no prior session stays quiet", func(t *testing.T) {
		agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)

		chatID := dbfx.ChatSession(t, agentID, testutil.Cols{
			"title": "first turn chat",
		})
		dbfx.Exec(t, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority)
			VALUES ($1, $2, $3, 'queued', 1000)
		`, agentID, runtimeID, chatID)

		task := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
		if task.PriorSessionResumeUnavailable {
			t.Fatal("a chat with no history must not disclose a continuity gap")
		}
	})

	// "Start a new context" is the user asking for the fresh session, not the
	// platform losing one. Telling the agent its context could not be restored
	// there would be a false alarm about a loss the user chose.
	t.Run("chat force-fresh across runtimes stays quiet", func(t *testing.T) {
		agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)
		oldRuntimeID := createRuntimeGuardRuntime(t, ctx, "kimi")

		chatID := dbfx.ChatSession(t, agentID, testutil.Cols{
			"title":      "force fresh chat",
			"session_id": "force-fresh-old-session",
			"work_dir":   "/tmp/force-fresh-workdir",
			"runtime_id": oldRuntimeID,
		})
		dbfx.Exec(t, `
			INSERT INTO agent_task_queue (
				agent_id, runtime_id, chat_session_id, status, priority, force_fresh_session
			)
			VALUES ($1, $2, $3, 'queued', 1000, TRUE)
		`, agentID, runtimeID, chatID)

		task := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
		if task.PriorSessionID != "" {
			t.Fatalf("PriorSessionID = %q, want empty on an explicit fresh start", task.PriorSessionID)
		}
		if task.PriorSessionResumeUnavailable {
			t.Fatal("an explicitly requested fresh session is not a continuity gap")
		}
	})
}
