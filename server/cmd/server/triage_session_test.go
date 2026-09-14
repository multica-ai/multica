package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Triage session isolation (MUL-7189 §5.6).
//
// A triage run and the execution run that follows accept share an
// (agent_id, issue_id) pair, which is the whole key GetLastTaskSession resumes
// on. Without the exclusion the first execution run would pick up the
// conversation in which the agent was still deciding whether the entry was
// worth keeping. From the execution side an accepted issue is a new issue, so
// it starts cold.

func insertTriageTask(t *testing.T, agentID, runtimeID, issueID, sessionID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, context)
		VALUES ($1, $2, $3, 'completed', 0, now() - interval '1 minute', now() - interval '1 minute', $4, '/tmp/triage', '{"type":"triage"}'::jsonb)
	`, agentID, runtimeID, issueID, sessionID); err != nil {
		t.Fatalf("insert triage task: %v", err)
	}
}

func lastTaskSession(t *testing.T, agentID, issueID string) (db.GetLastTaskSessionRow, error) {
	t.Helper()
	return db.New(testPool).GetLastTaskSession(context.Background(), db.GetLastTaskSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		IssueID: pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
	})
}

// The triage session is the newest one on the issue, so before the exclusion
// this lookup returned it.
func TestGetLastTaskSessionExcludesTriageRuns(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	insertTriageTask(t, agentID, runtimeID, issueID, "TRIAGE-SESSION")

	prior, err := lastTaskSession(t, agentID, issueID)
	requireSessionExcluded(t, prior.SessionID, err)
}

// An execution session next to a triage one is still resumable: the filter
// removes triage runs, not the issue.
func TestGetLastTaskSessionKeepsExecutionSessionBesideTriage(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir)
		VALUES ($1, $2, $3, 'completed', 0, now() - interval '5 minutes', now() - interval '5 minutes', 'EXECUTION-SESSION', '/tmp/execution')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert execution task: %v", err)
	}
	insertTriageTask(t, agentID, runtimeID, issueID, "TRIAGE-SESSION")

	prior, err := lastTaskSession(t, agentID, issueID)
	if err != nil {
		t.Fatalf("GetLastTaskSession failed: %v", err)
	}
	if prior.SessionID.String != "EXECUTION-SESSION" {
		t.Fatalf("resumed session = %q, want EXECUTION-SESSION", prior.SessionID.String)
	}
}

// The three CTEs are excluded independently, so each needs its own proof that a
// triage run cannot reach through it and invalidate a healthy session.
func TestTriageRunsCannotInvalidateAnExecutionSession(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()

	// retired_sessions: a triage run retiring the execution session must not
	// remove it. Only a run that actually abandoned it may.
	t.Run("retired_sessions", func(t *testing.T) {
		issueID, agentID, runtimeID := setupRerunTestFixture(t)
		t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

		if _, err := testPool.Exec(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir)
			VALUES ($1, $2, $3, 'completed', 0, now() - interval '5 minutes', now() - interval '5 minutes', 'EXECUTION-SESSION', '/tmp/execution')
		`, agentID, runtimeID, issueID); err != nil {
			t.Fatalf("insert execution task: %v", err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, retired_session_id, context)
			VALUES ($1, $2, $3, 'completed', 0, now() - interval '1 minute', now() - interval '1 minute', 'TRIAGE-SESSION', '/tmp/triage', 'EXECUTION-SESSION', '{"type":"triage"}'::jsonb)
		`, agentID, runtimeID, issueID); err != nil {
			t.Fatalf("insert triage task: %v", err)
		}

		prior, err := lastTaskSession(t, agentID, issueID)
		if err != nil {
			t.Fatalf("GetLastTaskSession failed: %v", err)
		}
		if prior.SessionID.String != "EXECUTION-SESSION" {
			t.Fatalf("resumed session = %q, want EXECUTION-SESSION", prior.SessionID.String)
		}
	})

	// resume_overflow_at blocks by TIME, so a failed triage run would otherwise
	// blank every execution session that terminated before it.
	t.Run("resume_overflow_at", func(t *testing.T) {
		issueID, agentID, runtimeID := setupRerunTestFixture(t)
		t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

		if _, err := testPool.Exec(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir)
			VALUES ($1, $2, $3, 'completed', 0, now() - interval '5 minutes', now() - interval '5 minutes', 'EXECUTION-SESSION', '/tmp/execution')
		`, agentID, runtimeID, issueID); err != nil {
			t.Fatalf("insert execution task: %v", err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, work_dir, failure_reason, context)
			VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute', '/tmp/triage', 'codex_resume_oversized', '{"type":"triage"}'::jsonb)
		`, agentID, runtimeID, issueID); err != nil {
			t.Fatalf("insert overflowed triage task: %v", err)
		}

		prior, err := lastTaskSession(t, agentID, issueID)
		if err != nil {
			t.Fatalf("GetLastTaskSession failed: %v", err)
		}
		if prior.SessionID.String != "EXECUTION-SESSION" {
			t.Fatalf("resumed session = %q, want EXECUTION-SESSION", prior.SessionID.String)
		}
	})

	// latest_per_session judges a session by its newest terminal row. A triage
	// run reusing an execution session id must not be that row.
	t.Run("latest_per_session", func(t *testing.T) {
		issueID, agentID, runtimeID := setupRerunTestFixture(t)
		t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

		if _, err := testPool.Exec(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir)
			VALUES ($1, $2, $3, 'completed', 0, now() - interval '5 minutes', now() - interval '5 minutes', 'SHARED-SESSION', '/tmp/execution')
		`, agentID, runtimeID, issueID); err != nil {
			t.Fatalf("insert execution task: %v", err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, context)
			VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute', 'SHARED-SESSION', '/tmp/triage', 'iteration_limit', '{"type":"triage"}'::jsonb)
		`, agentID, runtimeID, issueID); err != nil {
			t.Fatalf("insert poisoned triage task: %v", err)
		}

		prior, err := lastTaskSession(t, agentID, issueID)
		if err != nil {
			t.Fatalf("GetLastTaskSession failed: %v", err)
		}
		if prior.SessionID.String != "SHARED-SESSION" {
			t.Fatalf("resumed session = %q, want SHARED-SESSION", prior.SessionID.String)
		}
	})
}
