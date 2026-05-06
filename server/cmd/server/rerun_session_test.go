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
	// Filter on archived_at IS NULL because sibling integration tests
	// (e.g. comment_trigger_integration_test.go) archive their throwaway
	// agents in the same workspace at cleanup time. Without this filter
	// `LIMIT 1` can land on a stale archived row and the rerun service
	// rejects it with "agent is archived".
	if err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1 AND a.archived_at IS NULL
		ORDER BY a.created_at ASC
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
