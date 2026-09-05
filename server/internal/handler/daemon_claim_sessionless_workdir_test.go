package handler

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// A backend that never reports a session id used to lose its workdir too.
//
// PriorWorkDir and PriorSessionID answer different questions. The session is
// the model's transcript; the workdir is the directory holding the previous
// turn's branch, its uncommitted edits and any commits not yet pushed. The
// issue claim path used to gate both on the same GetLastTaskSession row, whose
// latest_per_session CTE filters `session_id IS NOT NULL` — so a backend that
// deliberately reports no session (Prime Agent: every turn is a fresh
// session/new, so a resume pointer would claim continuity it cannot honour)
// matched zero rows and every follow-up on the issue got a brand-new workdir,
// stranding the previous turn's work in an orphaned directory.
//
// Nothing here is provider-specific: the claim handler never branches on the
// backend, it branches on whether a session id was recorded. These fixtures
// write the rows a sessionless backend produces, which is the condition that
// actually drives the code.

// TestClaimTask_IssueKeepsWorkDirWithoutSession is the reported case: terminal
// row with a workdir and no session at all.
func TestClaimTask_IssueKeepsWorkDirWithoutSession(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)
	issueID := dbfx.Issue(t, "sessionless workdir fixture", testutil.Cols{
		"status": "in_progress",
		"number": 86641,
	})

	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id,
			status, priority, started_at, completed_at, session_id, work_dir
		)
		VALUES ($1, $2, $3, 'completed', 0, now(), now(), NULL, '/tmp/sessionless-workdir')
	`, agentID, runtimeID, issueID)
	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
	`, agentID, runtimeID, issueID)

	task := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	if task.PriorWorkDir != "/tmp/sessionless-workdir" {
		t.Fatalf("PriorWorkDir = %q, want /tmp/sessionless-workdir (a turn with no session still has a directory to carry forward)", task.PriorWorkDir)
	}
	if task.PriorSessionID != "" {
		t.Fatalf("PriorSessionID = %q, want empty — there is no session to resume and claiming one would be a lie", task.PriorSessionID)
	}
}

// TestClaimTask_IssueKeepsBothWhenSessionExists pins that the split changes
// nothing for the 15 backends that do report a session.
func TestClaimTask_IssueKeepsBothWhenSessionExists(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)
	issueID := dbfx.Issue(t, "session plus workdir fixture", testutil.Cols{
		"status": "in_progress",
		"number": 86642,
	})

	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id,
			status, priority, started_at, completed_at, session_id, work_dir
		)
		VALUES ($1, $2, $3, 'completed', 0, now(), now(), 'real-session', '/tmp/session-workdir')
	`, agentID, runtimeID, issueID)
	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
	`, agentID, runtimeID, issueID)

	task := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	if task.PriorSessionID != "real-session" {
		t.Fatalf("PriorSessionID = %q, want real-session", task.PriorSessionID)
	}
	if task.PriorWorkDir != "/tmp/session-workdir" {
		t.Fatalf("PriorWorkDir = %q, want /tmp/session-workdir", task.PriorWorkDir)
	}
}

// TestClaimTask_IssueOffersNoWorkDirWhenNoneRecorded covers the row that failed
// before it ever prepared a directory: there is nothing to carry forward and
// the fallback must not invent one.
func TestClaimTask_IssueOffersNoWorkDirWhenNoneRecorded(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)
	issueID := dbfx.Issue(t, "no workdir fixture", testutil.Cols{
		"status": "in_progress",
		"number": 86643,
	})

	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id,
			status, priority, started_at, completed_at, session_id, work_dir
		)
		VALUES ($1, $2, $3, 'failed', 0, now(), now(), NULL, NULL)
	`, agentID, runtimeID, issueID)
	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
	`, agentID, runtimeID, issueID)

	task := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	if task.PriorWorkDir != "" {
		t.Fatalf("PriorWorkDir = %q, want empty — no prior turn recorded a directory", task.PriorWorkDir)
	}
	if task.PriorSessionID != "" {
		t.Fatalf("PriorSessionID = %q, want empty", task.PriorSessionID)
	}
}

// TestClaimTask_IssueSessionWorkDirOutranksNewerSessionlessRow is the guard on
// the fallback's reach. A resumable session's directory and its transcript
// belong to the same turn, so a newer sessionless row must not pull the claim
// onto a different directory than the session it is resuming.
func TestClaimTask_IssueSessionWorkDirOutranksNewerSessionlessRow(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)
	issueID := dbfx.Issue(t, "session outranks fallback fixture", testutil.Cols{
		"status": "in_progress",
		"number": 86644,
	})

	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id,
			status, priority, started_at, completed_at, session_id, work_dir
		)
		VALUES ($1, $2, $3, 'completed', 0, now() - interval '5 minutes', now() - interval '4 minutes',
		        'resumable-session', '/tmp/session-owned-workdir')
	`, agentID, runtimeID, issueID)
	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id,
			status, priority, started_at, completed_at, session_id, work_dir
		)
		VALUES ($1, $2, $3, 'completed', 0, now() - interval '2 minutes', now() - interval '1 minutes',
		        NULL, '/tmp/newer-sessionless-workdir')
	`, agentID, runtimeID, issueID)
	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
	`, agentID, runtimeID, issueID)

	task := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	if task.PriorSessionID != "resumable-session" {
		t.Fatalf("PriorSessionID = %q, want resumable-session", task.PriorSessionID)
	}
	if task.PriorWorkDir != "/tmp/session-owned-workdir" {
		t.Fatalf("PriorWorkDir = %q, want /tmp/session-owned-workdir — the resumed session's own directory, not a newer unrelated one", task.PriorWorkDir)
	}
}

// TestClaimTask_IssueFallbackStaysScopedToItsOwnIssue is the guard the WHERE
// clause carries alone.
//
// The sessionless fallback is reached precisely when GetLastTaskSession found
// nothing, so it runs on issues that have no resumable history — the state in
// which a query scoped one column too loosely would silently hand over another
// issue's directory, and the claim would look successful while the agent woke
// up in the wrong repository checkout.
//
// The unrelated row is deliberately the NEWER one and shares the agent and the
// runtime, so issue_id is the only column left that can keep them apart.
func TestClaimTask_IssueFallbackStaysScopedToItsOwnIssue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)

	otherIssueID := dbfx.Issue(t, "unrelated issue fixture", testutil.Cols{
		"status": "in_progress",
		"number": 86645,
	})
	targetIssueID := dbfx.Issue(t, "target issue fixture", testutil.Cols{
		"status": "in_progress",
		"number": 86646,
	})

	// Same agent, same runtime, no session, a real workdir — and newer than
	// anything on the target issue.
	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id,
			status, priority, started_at, completed_at, session_id, work_dir
		)
		VALUES ($1, $2, $3, 'completed', 0, now(), now(), NULL, '/tmp/other-issue-workdir')
	`, agentID, runtimeID, otherIssueID)

	// The target issue's first ever run: nothing of its own to carry forward.
	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
	`, agentID, runtimeID, targetIssueID)

	task := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	if task.PriorWorkDir != "" {
		t.Fatalf("PriorWorkDir = %q, want empty — the fallback reached another issue's directory", task.PriorWorkDir)
	}
	if task.PriorSessionID != "" {
		t.Fatalf("PriorSessionID = %q, want empty", task.PriorSessionID)
	}
}
