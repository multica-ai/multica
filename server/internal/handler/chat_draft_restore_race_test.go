package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// chat_draft_restore has no FK to chat_session (MUL-3515), so an INSERT into it
// takes no lock on the session, and a deleter's prune only sees what committed
// before its snapshot. Prune-then-delete alone therefore leaves a window: a
// restore committed inside it outlives its cascade-deleted session, unreachable
// and undeletable, with the user's prompt in it (#5219 review).
//
// The close is a mutual-exclusion protocol on the chat_session row lock — every
// deleter locks before it sweeps, FinalizeDeferredCancelledChat locks before it
// inserts. Each half is useless without the other, so each has its own test
// below: pin one side, assert the other one blocks.

type draftRestoreRaceFixture struct {
	workspaceID   string
	chatSessionID string
	taskID        string
}

// seedDraftRestoreRaceFixture builds a disposable workspace holding one chat
// task that FinalizeDeferredCancelledChat will settle as "still empty": a
// deferred marker, an empty transcript, and the triggering user message it turns
// into a chat_draft_restore row.
func seedDraftRestoreRaceFixture(t *testing.T, slug string) draftRestoreRaceFixture {
	t.Helper()
	ctx := context.Background()

	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description)
		VALUES ($1, $2, '')
		RETURNING id
	`, "Draft Restore Race", slug).Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, wsID, testUserID); err != nil {
		t.Fatalf("create owner member: %v", err)
	}

	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, 'Draft Restore Race Runtime', 'cloud', 'isolated_test', 'online', '', '{}'::jsonb, now())
		RETURNING id
	`, wsID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'Draft Restore Race Agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id
	`, wsID, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	var sessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status)
		VALUES ($1, $2, $3, 'Draft Restore Race Session', 'active')
		RETURNING id
	`, wsID, agentID, testUserID).Scan(&sessionID); err != nil {
		t.Fatalf("create chat session: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, chat_session_id, status, priority, context, runtime_id,
			started_at, chat_finalize_deferred_at
		)
		VALUES ($1, $2, 'cancelled', 0, '{}'::jsonb, $3, now(), now())
		RETURNING id
	`, agentID, sessionID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("create deferred chat task: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content, task_id)
		VALUES ($1, 'user', 'the prompt the cancel ate', $2)
	`, sessionID, taskID); err != nil {
		t.Fatalf("create user chat message: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		testPool.Exec(cleanupCtx, `DELETE FROM chat_draft_restore WHERE chat_session_id = $1`, sessionID)
		testPool.Exec(cleanupCtx, `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	return draftRestoreRaceFixture{workspaceID: wsID, chatSessionID: sessionID, taskID: taskID}
}

// waitForBlockedBackend reports whether some backend parked on a lock — the
// observable proof that the side under test honoured the protocol. It gives up
// the moment the racing goroutine finishes instead: a side that never blocks
// skipped its lock, which is the bug these tests exist to catch.
func waitForBlockedBackend(t *testing.T, done <-chan struct{}) bool {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			return false
		default:
		}
		var blocked int
		if err := testPool.QueryRow(context.Background(), `
			SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock'
		`).Scan(&blocked); err == nil && blocked > 0 {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// The writer's half. Without its own chat_session lock the finalizer can commit
// a restore in the gap between a deleter's lock and that deleter's sweep — the
// sweep's snapshot predates the commit and the row is stranded. Holding the
// session lock from another tx is what makes that observable: a finalizer that
// respects the protocol parks on it; one that doesn't sails past and writes.
func TestFinalizeDeferredCancelledChat_TakesTheChatSessionLockBeforeInserting(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	f := seedDraftRestoreRaceFixture(t, "handler-tests-draft-restore-race-writer")

	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock holder tx: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT id FROM chat_session WHERE id = $1 FOR UPDATE`, f.chatSessionID); err != nil {
		t.Fatalf("lock chat session: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		testHandler.TaskService.FinalizeDeferredCancelledChat(context.Background(), parseUUID(f.taskID))
	}()

	if !waitForBlockedBackend(t, done) {
		t.Fatal("finalizer settled while the chat_session row was locked: it never took the lock, so nothing stops it from inserting a restore behind a deleter's sweep")
	}

	// Releasing the lock must let it settle normally — the protocol adds
	// exclusion, not a dropped restore.
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("release chat session lock: %v", err)
	}
	<-done

	if n := countDraftRestores(t, f.chatSessionID); n != 1 {
		t.Errorf("expected the finalizer to write 1 restore once the lock cleared, got %d", n)
	}
}

// The deleters' half. A restore commits while the workspace teardown is parked
// on the session lock; the teardown's sweep runs on a snapshot taken after it
// wins the lock, so it must still see — and remove — that row.
func TestDeleteWorkspace_SweepsRestoreCommittedByAConcurrentFinalizer(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	f := seedDraftRestoreRaceFixture(t, "handler-tests-draft-restore-race-deleter")

	// A finalizer held open mid-flight: session locked, restore written, not yet
	// committed. (That the real finalizer takes this lock is pinned by the test
	// above; here it is the deleter that is under test.)
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin finalizer tx: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT id FROM chat_session WHERE id = $1 FOR UPDATE`, f.chatSessionID); err != nil {
		t.Fatalf("lock chat session: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO chat_draft_restore (id, chat_session_id, task_id, content, attachment_ids)
		VALUES ($1, $2, $3, 'the prompt the cancel ate', '{}'::uuid[])
	`, uuid.NewString(), f.chatSessionID, f.taskID); err != nil {
		t.Fatalf("insert draft restore: %v", err)
	}

	code := make(chan int, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodDelete, fmt.Sprintf("/api/workspaces/%s", f.workspaceID), nil), "id", f.workspaceID)
		testHandler.DeleteWorkspace(w, req)
		code <- w.Code
	}()

	if !waitForBlockedBackend(t, done) {
		t.Fatal("DeleteWorkspace finished while a finalizer held the session lock: it never took the lock, so its sweep cannot see a restore committed behind it")
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit finalizer tx: %v", err)
	}
	<-done

	if got := <-code; got != http.StatusNoContent {
		t.Fatalf("DeleteWorkspace: expected 204, got %d", got)
	}
	if n := countDraftRestores(t, f.chatSessionID); n != 0 {
		t.Errorf("a restore committed during the teardown survived it (%d row(s)) — orphaned, holding the user's prompt", n)
	}
}

// The session locks only cover sessions that exist when the teardown enumerates
// them. A chat_session created inside the delete window is invisible to that set,
// so a finalizer could lock it and insert a restore after the sweep's snapshot —
// and the restore would outlive the workspace cascade exactly like the TOCTOU
// case above. The window is closed by an EXPLICIT protocol: DeleteWorkspace locks
// the workspace row FOR UPDATE, and every session creator takes a conflicting
// FOR KEY SHARE on it (LockWorkspaceForChatSessionCreate) inside its own tx before
// inserting — so the block does not lean on the chat_session.workspace_id FK's
// implicit lock, which the codebase is moving off of (MUL-3515).
//
// This drives the REAL creator entry point (Handler.CreateChatSession), not a raw
// INSERT: holding the workspace row FOR UPDATE (the exact lock DeleteWorkspace
// takes) must park the handler on its explicit lock, and releasing it must let the
// create proceed. A handler that skipped the lock would sail straight through.
func TestCreateChatSession_BlocksWhileTheWorkspaceDeleteLockIsHeld(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	f := seedDraftRestoreRaceFixture(t, "handler-tests-draft-restore-race-newsession")

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT agent_id FROM chat_session WHERE id = $1`, f.chatSessionID).Scan(&agentID); err != nil {
		t.Fatalf("read fixture agent: %v", err)
	}
	// The fixture's owner member, so the creator request carries a realistic
	// workspace context for the fixture workspace (not the shared test one).
	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(testUserID),
		WorkspaceID: parseUUID(f.workspaceID),
	})
	if err != nil {
		t.Fatalf("load fixture owner member: %v", err)
	}

	// Hold the workspace row FOR UPDATE — exactly what DeleteWorkspace's
	// LockWorkspaceForDelete takes at the top of its teardown tx. This holder is
	// not itself blocked, so the only backend that can park is the creator.
	holder, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin workspace-lock holder tx: %v", err)
	}
	defer holder.Rollback(ctx)
	if _, err := holder.Exec(ctx, `SELECT id FROM workspace WHERE id = $1 FOR UPDATE`, f.workspaceID); err != nil {
		t.Fatalf("lock workspace row: %v", err)
	}

	code := make(chan int, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		w := httptest.NewRecorder()
		req := newRequestAs(testUserID, http.MethodPost, "/api/chat/sessions", map[string]any{
			"agent_id": agentID,
			"title":    "Session racing the teardown",
		})
		req = req.WithContext(middleware.SetMemberContext(req.Context(), f.workspaceID, member))
		testHandler.CreateChatSession(w, req)
		code <- w.Code
	}()

	if !waitForBlockedBackend(t, done) {
		t.Fatal("CreateChatSession returned while the workspace delete lock was held: it never took LockWorkspaceForChatSessionCreate, so a session can be created into a workspace mid-delete and its restore can outlive the cascade")
	}

	// Releasing the delete lock must let the create proceed — the protocol adds
	// exclusion against an in-progress delete, not a blanket refusal.
	if err := holder.Rollback(ctx); err != nil {
		t.Fatalf("release workspace lock: %v", err)
	}
	<-done
	if got := <-code; got != http.StatusCreated {
		t.Fatalf("CreateChatSession: expected 201 once the delete lock cleared, got %d", got)
	}
}

// The settle-against-a-gone-session case the lock protocol produces: the loser
// of the race finds no session to lock, so it must clear its marker and write
// nothing rather than strand a restore against a dead id.
func TestFinalizeDeferredCancelledChat_SkipsTheInsertWhenTheSessionIsGone(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	f := seedDraftRestoreRaceFixture(t, "handler-tests-draft-restore-race-gone")

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodDelete, fmt.Sprintf("/api/workspaces/%s", f.workspaceID), nil), "id", f.workspaceID)
	testHandler.DeleteWorkspace(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteWorkspace: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	testHandler.TaskService.FinalizeDeferredCancelledChat(ctx, parseUUID(f.taskID))

	if n := countDraftRestores(t, f.chatSessionID); n != 0 {
		t.Errorf("finalizer wrote %d restore(s) for a session that no longer exists", n)
	}
}
