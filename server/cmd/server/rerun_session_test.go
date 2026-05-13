package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// setupRerunTestFixture creates an issue assigned to the integration test
// agent and returns (issueID, agentID, runtimeID).
func setupRerunTestFixture(t *testing.T) (string, string, string) {
	t.Helper()
	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1
		  AND a.archived_at IS NULL
		LIMIT 1
	`, integrationTestEmail).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id)
		SELECT $1, 'Rerun test issue', 'todo', 'none', 'member', m.user_id, 'agent', $2
		FROM member m WHERE m.workspace_id = $1 LIMIT 1
		RETURNING id
	`, testWorkspaceID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("failed to create test issue: %v", err)
	}

	return issueID, agentID, runtimeID
}

func cleanupRerunFixture(t *testing.T, issueID string) {
	t.Helper()
	ctx := context.Background()
	testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
}

// TestGetLastTaskSessionExcludesPoisonedFailures asserts that the
// (agent_id, issue_id) resume lookup skips failed tasks whose
// failure_reason classifies them as poisoned terminal output. This is the
// SQL-level half of the rerun-poisoned-session fix: without the filter, a
// rerun would inherit the same session and replay the same bad output.
func TestGetLastTaskSessionExcludesPoisonedFailures(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()

	// Insert an older failed task with a poisoned classifier and a session_id.
	// The poisoned task is the *most recent* one, so without the filter the
	// resume lookup would return its session_id.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '2 minutes', now() - interval '2 minutes', 'HEALTHY-SESSION', '/tmp/healthy', 'timeout')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert healthy failed task: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute', 'POISONED-SESSION', '/tmp/poisoned', 'iteration_limit')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert poisoned failed task: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		IssueID: pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
	})
	if err != nil {
		t.Fatalf("GetLastTaskSession failed: %v", err)
	}
	if !prior.SessionID.Valid {
		t.Fatal("expected to fall back to the healthy failed session, got no session")
	}
	if prior.SessionID.String == "POISONED-SESSION" {
		t.Fatal("rerun would inherit poisoned session — filter is not active")
	}
	if prior.SessionID.String != "HEALTHY-SESSION" {
		t.Fatalf("expected HEALTHY-SESSION, got %q", prior.SessionID.String)
	}
}

// TestGetLastTaskSessionFallbackPoisonedClassifier covers the second
// poisoned classifier so adding a third doesn't silently break this rule.
func TestGetLastTaskSessionFallbackPoisonedClassifier(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '5 seconds', now() - interval '5 seconds', 'POISONED-FALLBACK', '/tmp/poisoned', 'agent_fallback_message')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert poisoned failed task: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		IssueID: pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
	})
	if err == nil && prior.SessionID.Valid {
		t.Fatalf("expected no resumable session, got %q", prior.SessionID.String)
	}
}

// TestGetLastTaskSessionExcludesAPIInvalidRequest covers the MUL-1921
// case: an Anthropic 400 invalid_request_error (e.g. an oversized or
// malformed image baked into the conversation) bakes the bad message
// into the session history, so resuming would replay the same 400
// forever. The daemon classifies these as 'api_invalid_request' and the
// SQL filter must skip them on the resume lookup.
func TestGetLastTaskSessionExcludesAPIInvalidRequest(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '5 seconds', now() - interval '5 seconds', 'POISONED-API400', '/tmp/poisoned', 'api_invalid_request')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert poisoned failed task: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		IssueID: pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
	})
	if err == nil && prior.SessionID.Valid {
		t.Fatalf("expected no resumable session for api_invalid_request, got %q", prior.SessionID.String)
	}
}

// TestGetLastTaskSessionExcludesLegacyAPI400 is the MUL-1921 legacy
// regression: pre-fix rows are tagged failure_reason='agent_error' even
// though their error text contains the canonical Anthropic 400
// invalid_request_error marker. The daemon-side classifier only fires
// on new failures, so without a defensive ILIKE clause the resume query
// would happily return one of those rows on the next claim and
// re-poison every retry of an already-broken issue (e.g. MUL-1918,
// which already has three poisoned 'agent_error' rows when this PR
// merges). The SQL must skip the bad row on text shape alone.
func TestGetLastTaskSessionExcludesLegacyAPI400(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()

	// Legacy poisoned row: failure_reason was the pre-fix default
	// 'agent_error' but the error text shows it was an API 400
	// invalid_request_error. Migration 079 backfills these to
	// 'api_invalid_request', but the SQL filter must still exclude
	// them via ILIKE on the off chance a row escapes the migration
	// (deploy window, manual relabel, etc.).
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '2 minutes', now() - interval '2 minutes', 'LEGACY-POISONED', '/tmp/legacy', 'agent_error',
		        'API Error: 400 {"type":"error","error":{"type":"invalid_request_error","message":"Could not process image"}}')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert legacy poisoned task: %v", err)
	}

	// Newly classified poisoned row coexisting with the legacy one.
	// Without the ILIKE clause, ORDER BY completed_at DESC would
	// skip this row (failure_reason filter fires) and fall back to
	// the legacy row (failure_reason filter MISSES) — the exact
	// wormhole GPT-Boy flagged on PR review.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute', 'NEW-POISONED', '/tmp/new', 'api_invalid_request',
		        'API Error: 400 {"type":"error","error":{"type":"invalid_request_error","message":"Could not process image"}}')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert new poisoned task: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		IssueID: pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
	})
	if err == nil && prior.SessionID.Valid {
		t.Fatalf("expected no resumable session, but query fell back to %q", prior.SessionID.String)
	}
}

// TestGetLastTaskSessionKeepsBenignAgentErrorWithSession asserts the
// ILIKE clause is narrow enough that ordinary 'agent_error' failures
// (timeouts, tool errors, transient glue failures) still let the next
// task resume the prior session. Without this guard rail, the MUL-1921
// fix would regress MUL-1128's resume contract for everything else.
func TestGetLastTaskSessionKeepsBenignAgentErrorWithSession(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '30 seconds', now() - interval '30 seconds', 'HEALTHY-RESUMABLE', '/tmp/healthy', 'agent_error',
		        'tool execution failed: connection refused')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert benign failed task: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		IssueID: pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
	})
	if err != nil {
		t.Fatalf("GetLastTaskSession failed: %v", err)
	}
	if !prior.SessionID.Valid || prior.SessionID.String != "HEALTHY-RESUMABLE" {
		t.Fatalf("expected to resume HEALTHY-RESUMABLE, got %q (valid=%v)", prior.SessionID.String, prior.SessionID.Valid)
	}
}

// TestRerunIssueSetsForceFreshSession asserts the manual rerun flow flags
// the new task so the daemon claim handler skips the resume lookup. This
// is the call-site half of the fix: even if the SQL filter ever misses a
// poisoned classifier, manual rerun never resumes.
func TestRerunIssueSetsForceFreshSession(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, _, _ := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()
	queries := db.New(testPool)
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	taskService := service.NewTaskService(queries, nil, hub, bus)

	task, err := taskService.RerunIssue(ctx, pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true}, pgtype.UUID{})
	if err != nil {
		t.Fatalf("RerunIssue failed: %v", err)
	}
	if task == nil {
		t.Fatal("RerunIssue returned nil task")
	}
	if !task.ForceFreshSession {
		t.Fatal("expected manual rerun to set force_fresh_session=true")
	}
}

// TestEnqueueTaskForIssueDoesNotForceFreshSession is the negative control
// for the rerun flag: the normal enqueue path must leave the flag false so
// auto-retry / comment-triggered tasks keep resuming the prior session
// (MUL-1128 contract).
func TestEnqueueTaskForIssueDoesNotForceFreshSession(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, _, _ := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()
	queries := db.New(testPool)
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	taskService := service.NewTaskService(queries, nil, hub, bus)

	issue, err := queries.GetIssue(ctx, pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true})
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	task, err := taskService.EnqueueTaskForIssue(ctx, issue)
	if err != nil {
		t.Fatalf("EnqueueTaskForIssue failed: %v", err)
	}
	if task.ForceFreshSession {
		t.Fatal("expected normal enqueue to leave force_fresh_session=false")
	}
}
