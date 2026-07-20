package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
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
	// Pick the next per-workspace number to avoid colliding with the
	// uq_issue_workspace_number unique constraint when multiple fixtures
	// coexist in the same test (e.g. TestRerunIssueRejectsCrossIssueTask).
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id, number)
		SELECT $1, 'Rerun test issue', 'todo', 'none', 'member', m.user_id, 'agent', $2,
		       (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1)
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

// TestGetLastTaskSessionExcludesCodexSemanticInactivity covers Codex
// semantic inactivity timeouts: the failed task did establish a session, but
// resuming it can replay the same stuck Codex state. The resume lookup must
// skip that session.
func TestGetLastTaskSessionExcludesCodexSemanticInactivity(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '2 minutes', now() - interval '2 minutes', 'HEALTHY-SESSION', '/tmp/healthy', 'timeout')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert healthy failed task: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute', 'CODEX-STUCK-SESSION', '/tmp/codex-stuck', 'codex_semantic_inactivity',
		        'codex semantic inactivity timeout after 10m0s without agent progress (last activity: tool-result:exec_command)')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert codex semantic inactivity task: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		IssueID: pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
	})
	if err != nil {
		t.Fatalf("GetLastTaskSession failed: %v", err)
	}
	if prior.SessionID.String != "HEALTHY-SESSION" {
		t.Fatalf("expected HEALTHY-SESSION, got %q", prior.SessionID.String)
	}
}

func TestCreateRetryTaskFreshensCodexSemanticInactivity(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()

	var parentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			started_at, completed_at, session_id, work_dir, failure_reason,
			attempt, max_attempts
		)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute',
		        'CODEX-STUCK-SESSION', '/tmp/codex-stuck', 'codex_semantic_inactivity', 1, 2)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&parentID); err != nil {
		t.Fatalf("insert codex semantic inactivity parent task: %v", err)
	}

	queries := db.New(testPool)
	child, err := queries.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: pgtype.UUID{Bytes: parseUUIDBytes(parentID), Valid: true}})
	if err != nil {
		t.Fatalf("CreateRetryTask failed: %v", err)
	}
	if child.SessionID.Valid {
		t.Fatalf("expected retry child to drop poisoned session_id, got %q", child.SessionID.String)
	}
	if child.WorkDir.Valid {
		t.Fatalf("expected retry child to drop poisoned work_dir, got %q", child.WorkDir.String)
	}
	if !child.ForceFreshSession {
		t.Fatal("expected retry child to force a fresh session")
	}
	if child.Attempt != 2 {
		t.Fatalf("expected attempt 2, got %d", child.Attempt)
	}
}

func TestCreateRetryTaskKeepsOrdinaryTimeoutSession(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()

	var parentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			started_at, completed_at, session_id, work_dir, failure_reason,
			attempt, max_attempts
		)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute',
		        'ORDINARY-TIMEOUT-SESSION', '/tmp/ordinary-timeout', 'timeout', 1, 2)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&parentID); err != nil {
		t.Fatalf("insert ordinary timeout parent task: %v", err)
	}

	queries := db.New(testPool)
	child, err := queries.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: pgtype.UUID{Bytes: parseUUIDBytes(parentID), Valid: true}})
	if err != nil {
		t.Fatalf("CreateRetryTask failed: %v", err)
	}
	if !child.SessionID.Valid || child.SessionID.String != "ORDINARY-TIMEOUT-SESSION" {
		t.Fatalf("expected retry child to inherit session_id, got %+v", child.SessionID)
	}
	if !child.WorkDir.Valid || child.WorkDir.String != "/tmp/ordinary-timeout" {
		t.Fatalf("expected retry child to inherit work_dir, got %+v", child.WorkDir)
	}
	if child.ForceFreshSession {
		t.Fatal("expected ordinary timeout retry child to keep resume enabled")
	}
	if child.Attempt != 2 {
		t.Fatalf("expected attempt 2, got %d", child.Attempt)
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

	task, err := taskService.RerunIssue(ctx, pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true}, pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, nil)
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

// TestRerunIssueTargetsSourceTaskAgent asserts that when a source task ID is
// supplied (the execution-log retry-button path), the rerun targets the agent
// that ran that specific past task — not the issue's current assignee.
// Without this, clicking retry on a row whose agent has since been displaced
// (squad worker, @-mention agent, or a prior assignee) re-fires the new
// assignee instead, which is the MUL-2457 bug.
func TestRerunIssueTargetsSourceTaskAgent(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, primaryAgentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()

	// Create a second agent in the same workspace + runtime so we can stand
	// in as a "row whose agent is no longer the issue assignee" — e.g. a
	// squad worker or an @-mentioned agent. The issue's assignee is still
	// the primary agent; the rerun must target this secondary one because
	// that's whose task row the user clicked.
	var secondaryAgentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		SELECT a.workspace_id, 'Rerun Secondary Agent', '', 'cloud', '{}'::jsonb,
		       a.runtime_id, 'workspace', 1, a.owner_id
		FROM agent a WHERE a.id = $1
		RETURNING id
	`, primaryAgentID).Scan(&secondaryAgentID); err != nil {
		t.Fatalf("create secondary agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE agent_id = $1`, secondaryAgentID)
		testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, secondaryAgentID)
	})

	// Insert a failed past task on this issue under the secondary agent —
	// the row the user is about to click retry on.
	var sourceTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority,
		                              started_at, completed_at, failure_reason)
		VALUES ($1, $2, $3, 'failed', 0,
		        now() - interval '1 minute', now() - interval '30 seconds', 'agent_error')
		RETURNING id
	`, secondaryAgentID, runtimeID, issueID).Scan(&sourceTaskID); err != nil {
		t.Fatalf("insert source task: %v", err)
	}

	queries := db.New(testPool)
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	taskService := service.NewTaskService(queries, nil, hub, bus)

	task, err := taskService.RerunIssue(
		ctx,
		pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
		pgtype.UUID{Bytes: parseUUIDBytes(sourceTaskID), Valid: true},
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
	)
	if err != nil {
		t.Fatalf("RerunIssue failed: %v", err)
	}
	if task == nil {
		t.Fatal("RerunIssue returned nil task")
	}

	gotAgent := util.UUIDToString(task.AgentID)
	if gotAgent != secondaryAgentID {
		t.Fatalf("rerun targeted wrong agent: got %s, want %s (issue assignee is %s — must not be picked)",
			gotAgent, secondaryAgentID, primaryAgentID)
	}
	if !task.ForceFreshSession {
		t.Fatal("expected per-row rerun to also set force_fresh_session=true")
	}
}

// TestRerunIssueRejectsCrossIssueTask asserts a source task whose IssueID
// does not match the rerun target is rejected — both as defense-in-depth
// against malicious requests and because picking up an unrelated task's
// agent would silently misroute the rerun.
func TestRerunIssueRejectsCrossIssueTask(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueAID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueAID) })

	ctx := context.Background()

	// Second issue in the same workspace, with a task that does NOT belong
	// to issue A. The handler must reject this. Take the next available
	// per-workspace number so the uq_issue_workspace_number constraint
	// (both issues default to number=0 otherwise) doesn't fire before the
	// rerun assertion can.
	var issueBID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id, number)
		SELECT $1, 'Rerun cross-issue test', 'todo', 'none', 'member', m.user_id, 'agent', $2,
		       (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1)
		FROM member m WHERE m.workspace_id = $1 LIMIT 1
		RETURNING id
	`, testWorkspaceID, agentID).Scan(&issueBID); err != nil {
		t.Fatalf("create second issue: %v", err)
	}
	t.Cleanup(func() { cleanupRerunFixture(t, issueBID) })

	var crossTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority,
		                              started_at, completed_at, failure_reason)
		VALUES ($1, $2, $3, 'failed', 0,
		        now() - interval '1 minute', now() - interval '30 seconds', 'agent_error')
		RETURNING id
	`, agentID, runtimeID, issueBID).Scan(&crossTaskID); err != nil {
		t.Fatalf("insert cross task: %v", err)
	}

	queries := db.New(testPool)
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	taskService := service.NewTaskService(queries, nil, hub, bus)

	_, err := taskService.RerunIssue(
		ctx,
		pgtype.UUID{Bytes: parseUUIDBytes(issueAID), Valid: true},
		pgtype.UUID{Bytes: parseUUIDBytes(crossTaskID), Valid: true},
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
	)
	if err == nil {
		t.Fatal("expected RerunIssue to reject a source task from a different issue")
	}
}

// TestRerunIssueInheritsTriggerCommentFromSourceTask locks the trigger
// provenance contract: a per-row rerun of a comment- or mention-triggered
// task must carry the original trigger_comment_id through to the new task.
// Otherwise the daemon's buildCommentPrompt path (which keys on
// TriggerCommentID) is skipped and the rerun degrades into a generic
// issue run that has lost the original comment context — see MUL-2457
// review feedback.
func TestRerunIssueInheritsTriggerCommentFromSourceTask(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })

	ctx := context.Background()

	// Create a comment to stand in as the original mention / reply trigger.
	var triggerCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		SELECT $1, $2, 'member', m.user_id, 'please retry this', 'comment'
		FROM member m WHERE m.workspace_id = $2 LIMIT 1
		RETURNING id
	`, issueID, testWorkspaceID).Scan(&triggerCommentID); err != nil {
		t.Fatalf("insert trigger comment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, triggerCommentID)
	})

	// Source task carries the trigger_comment_id — this is the row whose
	// retry button the user clicks in the execution log.
	var sourceTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority,
		                              started_at, completed_at, failure_reason,
		                              trigger_comment_id)
		VALUES ($1, $2, $3, 'failed', 0,
		        now() - interval '1 minute', now() - interval '30 seconds', 'agent_error',
		        $4)
		RETURNING id
	`, agentID, runtimeID, issueID, triggerCommentID).Scan(&sourceTaskID); err != nil {
		t.Fatalf("insert source task: %v", err)
	}

	queries := db.New(testPool)
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	taskService := service.NewTaskService(queries, nil, hub, bus)

	task, err := taskService.RerunIssue(
		ctx,
		pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
		pgtype.UUID{Bytes: parseUUIDBytes(sourceTaskID), Valid: true},
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
	)
	if err != nil {
		t.Fatalf("RerunIssue failed: %v", err)
	}
	if task == nil {
		t.Fatal("RerunIssue returned nil task")
	}
	if !task.TriggerCommentID.Valid {
		t.Fatal("expected per-row rerun to inherit trigger_comment_id from source task, got NULL")
	}
	if got := util.UUIDToString(task.TriggerCommentID); got != triggerCommentID {
		t.Fatalf("trigger_comment_id mismatch: got %s, want %s", got, triggerCommentID)
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
