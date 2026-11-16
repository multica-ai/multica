package main

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"

	"github.com/jackc/pgx/v5/pgtype"
)

// newPoisonTestChatSession creates a chat session for the retired-session
// regressions and registers its cleanup.
func newPoisonTestChatSession(t *testing.T, agentID, runtimeID, title string) string {
	t.Helper()
	ctx := context.Background()
	var chatSessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, runtime_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID, title, runtimeID).Scan(&chatSessionID); err != nil {
		t.Fatalf("insert chat_session: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE chat_session_id = $1`, chatSessionID)
		testPool.Exec(ctx, `DELETE FROM chat_session WHERE id = $1`, chatSessionID)
	})
	return chatSessionID
}

func lastChatTaskSessionParams(chatSessionID string) db.GetLastChatTaskSessionParams {
	return db.GetLastChatTaskSessionParams{
		ChatSessionID: pgtype.UUID{Bytes: parseUUIDBytes(chatSessionID), Valid: true},
	}
}

// TestGetLastChatTaskSessionDoesNotResurrectFromOlderCompletedRow is the Chat
// half of the GH #5975 wormhole, which the issue query closed and the chat
// query never did. Task A completed on session S; task B then resumed S, hit
// the poisoned history and failed. A row-level filter drops B and falls back to
// A — same dead session, now looking perfectly healthy. Judging each session by
// its LATEST terminal state is what stops that.
func TestGetLastChatTaskSessionDoesNotResurrectFromOlderCompletedRow(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	_, agentID, runtimeID := setupRerunTestFixture(t)
	chatSessionID := newPoisonTestChatSession(t, agentID, runtimeID, "poison-resurrect")
	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, started_at, completed_at, session_id, work_dir)
		VALUES ($1, $2, $3, 'completed', 0, now() - interval '2 minutes', now() - interval '2 minutes', 'POISONED-CHAT', '/tmp/chat')
	`, agentID, runtimeID, chatSessionID); err != nil {
		t.Fatalf("insert task A: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute', 'POISONED-CHAT', '/tmp/chat', 'api_invalid_request',
		        'Invalid request: the message at position 37 with role ''assistant'' must not be empty')
	`, agentID, runtimeID, chatSessionID); err != nil {
		t.Fatalf("insert task B: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastChatTaskSession(ctx, lastChatTaskSessionParams(chatSessionID))
	requireSessionExcluded(t, prior.SessionID, err)
}

// TestGetLastChatTaskSessionKeepsHealthyDistinctSession is the narrowness
// counterpart: retiring one session must not cost the chat its memory of a
// genuinely different, healthy one.
func TestGetLastChatTaskSessionKeepsHealthyDistinctSession(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	_, agentID, runtimeID := setupRerunTestFixture(t)
	chatSessionID := newPoisonTestChatSession(t, agentID, runtimeID, "poison-distinct")
	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '2 minutes', now() - interval '2 minutes', 'CHAT-DEAD', '/tmp/chat', 'api_invalid_request',
		        'Invalid request: the message at position 37 with role ''assistant'' must not be empty')
	`, agentID, runtimeID, chatSessionID); err != nil {
		t.Fatalf("insert poisoned task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, started_at, completed_at, session_id, work_dir)
		VALUES ($1, $2, $3, 'completed', 0, now() - interval '1 minute', now() - interval '1 minute', 'CHAT-HEALTHY', '/tmp/chat')
	`, agentID, runtimeID, chatSessionID); err != nil {
		t.Fatalf("insert healthy task: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastChatTaskSession(ctx, lastChatTaskSessionParams(chatSessionID))
	if err != nil {
		t.Fatalf("GetLastChatTaskSession failed: %v", err)
	}
	if !prior.SessionID.Valid || prior.SessionID.String != "CHAT-HEALTHY" {
		t.Fatalf("expected the healthy session to remain resumable, got %+v", prior.SessionID)
	}
}

// TestGetLastChatTaskSessionExcludesAuthResolutionFailure is the chat half of
// the auth-resolution resume fix, mirroring GetLastTaskSession: a provider that
// cannot resolve its own authentication method must not be handed back to the
// next task on the same chat. The row carries agent_error.unknown — what every
// daemon version writes for this text — so only the ILIKE guard drops it.
func TestGetLastChatTaskSessionExcludesAuthResolutionFailure(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	_, agentID, runtimeID := setupRerunTestFixture(t)
	chatSessionID := newPoisonTestChatSession(t, agentID, runtimeID, "poison-auth-resolve")
	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute', 'POISONED-CHAT-AUTH', '/tmp/chat', 'agent_error.unknown',
		        'hermes provider error: "Could not resolve authentication method. Expected either api_key or auth_token to be set. Or for one of the X-Api-Key or Authorization headers to be explicitly omitted"')
	`, agentID, runtimeID, chatSessionID); err != nil {
		t.Fatalf("insert poisoned task: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastChatTaskSession(ctx, lastChatTaskSessionParams(chatSessionID))
	requireSessionExcluded(t, prior.SessionID, err)
}

// TestGetLastChatTaskSessionKeepsSessionOnAuthAdjacentError is the chat
// narrowness half: the ILIKE guard must match the exact provider phrase, not an
// error that merely resembles it, or a healthy conversation loses its resume
// context on every follow-up.
func TestGetLastChatTaskSessionKeepsSessionOnAuthAdjacentError(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	_, agentID, runtimeID := setupRerunTestFixture(t)
	chatSessionID := newPoisonTestChatSession(t, agentID, runtimeID, "poison-auth-adjacent")
	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute', 'CHAT-HEALTHY-AUTH', '/tmp/chat', 'agent_error.unknown',
		        'hermes provider error: could not resolve an authentication method for this model')
	`, agentID, runtimeID, chatSessionID); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastChatTaskSession(ctx, lastChatTaskSessionParams(chatSessionID))
	if err != nil {
		t.Fatalf("GetLastChatTaskSession failed: %v", err)
	}
	if !prior.SessionID.Valid || prior.SessionID.String != "CHAT-HEALTHY-AUTH" {
		t.Fatalf("an auth-adjacent but non-matching error must not retire the session, got %+v", prior.SessionID)
	}
}

// TestRetiredSessionExcludedFromIssueResume covers the case the poison filters
// structurally cannot see (GH #6066): a fresh-session retry that SUCCEEDS. The
// recovered task is 'completed' and carries no failure to classify, so the only
// trace of the abandoned session is retired_session_id — without it, the older
// completed row still pointing at that session hands it straight back.
func TestRetiredSessionExcludedFromIssueResume(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	issueID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, issueID) })
	ctx := context.Background()

	// Task A: an ordinary successful turn on session S.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir)
		VALUES ($1, $2, $3, 'completed', 0, now() - interval '2 minutes', now() - interval '2 minutes', 'RETIRED-S', '/tmp/wd')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert task A: %v", err)
	}
	// Task B: resumed S, found it unresumable, retried fresh and succeeded
	// without the backend emitting a new session id.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at, completed_at, session_id, work_dir, retired_session_id)
		VALUES ($1, $2, $3, 'completed', 0, now() - interval '1 minute', now() - interval '1 minute', NULL, '/tmp/wd', 'RETIRED-S')
	`, agentID, runtimeID, issueID); err != nil {
		t.Fatalf("insert task B: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		IssueID: pgtype.UUID{Bytes: parseUUIDBytes(issueID), Valid: true},
	})
	requireSessionExcluded(t, prior.SessionID, err)
}

// TestRetiredSessionExcludedFromChatResume is the chat mirror of the above.
func TestRetiredSessionExcludedFromChatResume(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	_, agentID, runtimeID := setupRerunTestFixture(t)
	chatSessionID := newPoisonTestChatSession(t, agentID, runtimeID, "poison-retired")
	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, started_at, completed_at, session_id, work_dir)
		VALUES ($1, $2, $3, 'completed', 0, now() - interval '2 minutes', now() - interval '2 minutes', 'RETIRED-CHAT-S', '/tmp/chat')
	`, agentID, runtimeID, chatSessionID); err != nil {
		t.Fatalf("insert task A: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, started_at, completed_at, session_id, work_dir, retired_session_id)
		VALUES ($1, $2, $3, 'completed', 0, now() - interval '1 minute', now() - interval '1 minute', NULL, '/tmp/chat', 'RETIRED-CHAT-S')
	`, agentID, runtimeID, chatSessionID); err != nil {
		t.Fatalf("insert task B: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastChatTaskSession(ctx, lastChatTaskSessionParams(chatSessionID))
	requireSessionExcluded(t, prior.SessionID, err)
}

// TestFailTaskClearsPoisonedChatPointer covers the bypass that made the chat
// SQL guard unreachable (GH #6066). The claim handler reads
// chat_session.session_id FIRST and only consults GetLastChatTaskSession when
// it is empty, so a poisoned pointer there is resumed no matter what the query
// filters. Declining to OVERWRITE the pointer — the old behaviour — left it in
// place; the fail transaction has to actively clear it.
func TestFailTaskClearsPoisonedChatPointer(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	_, agentID, runtimeID := setupRerunTestFixture(t)
	chatSessionID := newPoisonTestChatSession(t, agentID, runtimeID, "poison-pointer")
	ctx := context.Background()

	// The last good turn pinned S as the chat-level resume pointer.
	if _, err := testPool.Exec(ctx,
		`UPDATE chat_session SET session_id = 'CHAT-PTR-S', runtime_id = $2 WHERE id = $1`,
		chatSessionID, runtimeID); err != nil {
		t.Fatalf("pin chat pointer: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, started_at, session_id, work_dir)
		VALUES ($1, $2, $3, 'running', 0, now(), 'CHAT-PTR-S', '/tmp/chat')
		RETURNING id::text
	`, agentID, runtimeID, chatSessionID).Scan(&taskID); err != nil {
		t.Fatalf("insert running task: %v", err)
	}

	queries := db.New(testPool)
	taskSvc := service.NewTaskService(queries, testPool, nil, events.New())

	// The un-upgraded-daemon shape: catchall reason, poisoning only in the text.
	if _, err := taskSvc.FailTask(ctx, pgtype.UUID{Bytes: parseUUIDBytes(taskID), Valid: true},
		"Invalid request: the message at position 37 with role 'assistant' must not be empty",
		"CHAT-PTR-S", "/tmp/chat", "", "agent_error.unknown", false, "", ""); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	var pointer *string
	if err := testPool.QueryRow(ctx, `SELECT session_id FROM chat_session WHERE id = $1`, chatSessionID).Scan(&pointer); err != nil {
		t.Fatalf("read chat pointer: %v", err)
	}
	if pointer != nil {
		t.Fatalf("poisoned chat pointer must be cleared, still %q", *pointer)
	}
}

// TestFailTaskKeepsChatPointerOnTransientFailure is the narrowness guard: a
// transient failure must keep the pointer, because resuming is exactly how the
// conversation survives a blip.
func TestFailTaskKeepsChatPointerOnTransientFailure(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	_, agentID, runtimeID := setupRerunTestFixture(t)
	chatSessionID := newPoisonTestChatSession(t, agentID, runtimeID, "transient-pointer")
	ctx := context.Background()

	if _, err := testPool.Exec(ctx,
		`UPDATE chat_session SET session_id = 'CHAT-KEEP-S', runtime_id = $2 WHERE id = $1`,
		chatSessionID, runtimeID); err != nil {
		t.Fatalf("pin chat pointer: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, started_at, session_id, work_dir)
		VALUES ($1, $2, $3, 'running', 0, now(), 'CHAT-KEEP-S', '/tmp/chat')
		RETURNING id::text
	`, agentID, runtimeID, chatSessionID).Scan(&taskID); err != nil {
		t.Fatalf("insert running task: %v", err)
	}

	queries := db.New(testPool)
	taskSvc := service.NewTaskService(queries, testPool, nil, events.New())

	if _, err := taskSvc.FailTask(ctx, pgtype.UUID{Bytes: parseUUIDBytes(taskID), Valid: true},
		"Connection closed mid-response", "CHAT-KEEP-S", "/tmp/chat", "",
		"agent_error.provider_network", false, "", ""); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	var pointer *string
	if err := testPool.QueryRow(ctx, `SELECT session_id FROM chat_session WHERE id = $1`, chatSessionID).Scan(&pointer); err != nil {
		t.Fatalf("read chat pointer: %v", err)
	}
	if pointer == nil || *pointer != "CHAT-KEEP-S" {
		t.Fatal("a transient failure must keep the chat resume pointer")
	}
}

// TestGetLastChatTaskSessionExcludesOverflowedResumeFromOlderCompletedRow is
// the chat half of the MUL-5722 topology. Same shape as the issue side: the
// overflowed resume records no session, so only the older completed row names
// the oversized thread.
//
// Scope worth stating: this only guards the FALLBACK query. The claim handler
// reads chat_session.session_id first, so a pointer still naming the oversized
// thread shadows everything asserted here — clearing that pointer at fail time
// is what covers the rest, and for a daemon too old to report the failure it is
// still open (see the PR notes).
func TestGetLastChatTaskSessionExcludesOverflowedResumeFromOlderCompletedRow(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	_, agentID, runtimeID := setupRerunTestFixture(t)
	chatSessionID := newPoisonTestChatSession(t, agentID, runtimeID, "overflow-resume")
	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, started_at, completed_at, session_id, work_dir)
		VALUES ($1, $2, $3, 'completed', 0, now() - interval '2 minutes', now() - interval '2 minutes', 'THR-CHAT-OVERSIZED', '/tmp/chat')
	`, agentID, runtimeID, chatSessionID); err != nil {
		t.Fatalf("insert completed task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute', NULL, '/tmp/chat', 'agent_error.process_failure',
		        'codex thread/resume failed: codex process exited: bufio.Scanner: token too long')
	`, agentID, runtimeID, chatSessionID); err != nil {
		t.Fatalf("insert overflow failed task: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastChatTaskSession(ctx, lastChatTaskSessionParams(chatSessionID))
	requireSessionExcluded(t, prior.SessionID, err)
}
