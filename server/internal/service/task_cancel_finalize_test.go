package service

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func newCancelFinalizePool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database unreachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

type cancelFinalizeFixture struct {
	pool          *pgxpool.Pool
	workspaceID   string
	agentID       string
	chatSessionID string
	taskID        string
	userMessageID string
}

// createCancelFinalizeFixture seeds a chat task in the given status with one
// triggering user chat_message bound to it. started marks the task as picked
// up by a daemon (started_at set).
func createCancelFinalizeFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, status string, started bool) cancelFinalizeFixture {
	t.Helper()

	suffix := time.Now().UnixNano()
	email := fmt.Sprintf("cancel-finalize-%d@multica.ai", suffix)
	slug := fmt.Sprintf("cancel-finalize-%d", suffix)

	var userID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Cancel Finalize Test", email).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}

	var workspaceID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Cancel Finalize Test", slug, "temporary cancel finalize test workspace", "CFT").Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}

	var runtimeID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider,
			status, device_info, metadata, last_seen_at, visibility, owner_id
		)
		VALUES ($1, NULL, $2, 'cloud', 'cancel_finalize_test', 'online', 'test runtime', '{}'::jsonb, now(), 'private', $3)
		RETURNING id
	`, workspaceID, "Cancel Finalize Runtime", userID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	var agentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4)
		RETURNING id
	`, workspaceID, "Cancel Finalize Agent", runtimeID, userID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	var chatSessionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title)
		VALUES ($1, $2, $3, 'cancel finalize test')
		RETURNING id
	`, workspaceID, agentID, userID).Scan(&chatSessionID); err != nil {
		t.Fatalf("create chat session: %v", err)
	}

	startedAt := "NULL"
	if started {
		startedAt = "now()"
	}
	var taskID string
	if err := pool.QueryRow(ctx, fmt.Sprintf(`
		INSERT INTO agent_task_queue (agent_id, chat_session_id, status, priority, context, runtime_id, started_at)
		VALUES ($1, $2, $3, 0, '{}'::jsonb, $4, %s)
		RETURNING id
	`, startedAt), agentID, chatSessionID, status, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}

	var userMessageID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content, task_id)
		VALUES ($1, 'user', 'run the thing', $2)
		RETURNING id
	`, chatSessionID, taskID).Scan(&userMessageID); err != nil {
		t.Fatalf("create user chat message: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		pool.Exec(cleanupCtx, `DELETE FROM task_message WHERE task_id = $1`, taskID)
		pool.Exec(cleanupCtx, `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)
		pool.Exec(cleanupCtx, `DELETE FROM chat_message WHERE chat_session_id = $1`, chatSessionID)
		pool.Exec(cleanupCtx, `DELETE FROM chat_session WHERE id = $1`, chatSessionID)
		pool.Exec(cleanupCtx, `DELETE FROM agent WHERE id = $1`, agentID)
		pool.Exec(cleanupCtx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
		pool.Exec(cleanupCtx, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, workspaceID, userID)
		pool.Exec(cleanupCtx, `DELETE FROM workspace WHERE id = $1`, workspaceID)
		pool.Exec(cleanupCtx, `DELETE FROM "user" WHERE id = $1`, userID)
	})

	return cancelFinalizeFixture{
		pool:          pool,
		workspaceID:   workspaceID,
		agentID:       agentID,
		chatSessionID: chatSessionID,
		taskID:        taskID,
		userMessageID: userMessageID,
	}
}

func (f cancelFinalizeFixture) insertTranscriptRow(t *testing.T, ctx context.Context) {
	t.Helper()
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO task_message (task_id, seq, type, content)
		VALUES ($1, 1, 'text', 'partial output')
	`, f.taskID); err != nil {
		t.Fatalf("insert task message: %v", err)
	}
}

func (f cancelFinalizeFixture) chatFinalizeDeferredAt(t *testing.T, ctx context.Context) *time.Time {
	t.Helper()
	var deferredAt *time.Time
	if err := f.pool.QueryRow(ctx, `
		SELECT chat_finalize_deferred_at FROM agent_task_queue WHERE id = $1
	`, f.taskID).Scan(&deferredAt); err != nil {
		t.Fatalf("read chat_finalize_deferred_at: %v", err)
	}
	return deferredAt
}

func (f cancelFinalizeFixture) userMessageExists(t *testing.T, ctx context.Context) bool {
	t.Helper()
	var count int
	if err := f.pool.QueryRow(ctx, `
		SELECT count(*) FROM chat_message WHERE id = $1
	`, f.userMessageID).Scan(&count); err != nil {
		t.Fatalf("count user message: %v", err)
	}
	return count > 0
}

// attachUserMessageAttachment binds an attachment row to the fixture's user
// message, mirroring a prompt sent with a file.
func (f cancelFinalizeFixture) attachUserMessageAttachment(t *testing.T, ctx context.Context) string {
	t.Helper()
	var attachmentID string
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO attachment (
			workspace_id, chat_session_id, chat_message_id,
			uploader_type, uploader_id, filename, url, content_type, size_bytes
		)
		SELECT $1, $2, $3, 'member', creator_id, 'notes.txt', 'https://files.test/notes.txt', 'text/plain', 12
		FROM chat_session WHERE id = $2
		RETURNING id
	`, f.workspaceID, f.chatSessionID, f.userMessageID).Scan(&attachmentID); err != nil {
		t.Fatalf("create attachment: %v", err)
	}
	return attachmentID
}

func (f cancelFinalizeFixture) draftRestores(t *testing.T, ctx context.Context) []db.ChatDraftRestore {
	t.Helper()
	rows, err := db.New(f.pool).ListChatDraftRestoresBySession(ctx, util.MustParseUUID(f.chatSessionID))
	if err != nil {
		t.Fatalf("list draft restores: %v", err)
	}
	return rows
}

func (f cancelFinalizeFixture) assistantMessages(t *testing.T, ctx context.Context) []string {
	t.Helper()
	rows, err := f.pool.Query(ctx, `
		SELECT content FROM chat_message WHERE task_id = $1 AND role = 'assistant' ORDER BY created_at
	`, f.taskID)
	if err != nil {
		t.Fatalf("list assistant messages: %v", err)
	}
	defer rows.Close()
	var contents []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("scan assistant message: %v", err)
		}
		contents = append(contents, c)
	}
	return contents
}

// eventRecorder captures chat:cancel_finalized events published on the bus.
type eventRecorder struct {
	mu     sync.Mutex
	events []events.Event
}

func recordCancelFinalizedEvents(bus *events.Bus) *eventRecorder {
	rec := &eventRecorder{}
	bus.Subscribe(protocol.EventChatCancelFinalized, func(e events.Event) {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		rec.events = append(rec.events, e)
	})
	return rec
}

func (r *eventRecorder) snapshot() []events.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]events.Event(nil), r.events...)
}

// Cancelled before any daemon started the task: the transcript can never gain
// rows, so the empty judgment is final and the draft restore stays synchronous.
func TestCancelTask_NotStarted_RestoresDraftSynchronously(t *testing.T) {
	ctx := context.Background()
	pool := newCancelFinalizePool(t)
	f := createCancelFinalizeFixture(t, ctx, pool, "queued", false)
	svc := NewTaskService(db.New(pool), pool, nil, events.New())

	result, err := svc.CancelTaskWithResult(ctx, util.MustParseUUID(f.taskID), CancelTaskOptions{ClientSupportsDraftRestore: true})
	if err != nil {
		t.Fatalf("cancel task: %v", err)
	}
	if result.CancelledChatMessage == nil {
		t.Fatal("expected a synchronous CancelledChatMessage restore result")
	}
	if !result.CancelledChatMessage.RestoreToInput {
		t.Error("expected RestoreToInput=true")
	}
	if result.CancelledChatMessage.Content != "run the thing" {
		t.Errorf("restored content = %q, want %q", result.CancelledChatMessage.Content, "run the thing")
	}
	if f.userMessageExists(t, ctx) {
		t.Error("user chat message should have been deleted")
	}
	if got := f.chatFinalizeDeferredAt(t, ctx); got != nil {
		t.Errorf("chat_finalize_deferred_at should stay NULL, got %v", got)
	}
}

// Cancelled after start with transcript rows already persisted: "non-empty"
// is monotonic (late rows only append), so the Stopped. outcome stays
// synchronous.
func TestCancelTask_StartedNonEmptyTranscript_StopsSynchronously(t *testing.T) {
	ctx := context.Background()
	pool := newCancelFinalizePool(t)
	f := createCancelFinalizeFixture(t, ctx, pool, "running", true)
	f.insertTranscriptRow(t, ctx)
	svc := NewTaskService(db.New(pool), pool, nil, events.New())

	result, err := svc.CancelTaskWithResult(ctx, util.MustParseUUID(f.taskID), CancelTaskOptions{ClientSupportsDraftRestore: true})
	if err != nil {
		t.Fatalf("cancel task: %v", err)
	}
	if result.CancelledChatMessage != nil {
		t.Fatalf("expected no restore result, got %+v", result.CancelledChatMessage)
	}
	if got := f.assistantMessages(t, ctx); len(got) != 1 || got[0] != "Stopped." {
		t.Errorf("assistant messages = %v, want [Stopped.]", got)
	}
	if !f.userMessageExists(t, ctx) {
		t.Error("user chat message should be preserved")
	}
	if got := f.chatFinalizeDeferredAt(t, ctx); got != nil {
		t.Errorf("chat_finalize_deferred_at should stay NULL, got %v", got)
	}
}

// Cancelled after start with an empty transcript: the daemon may still be
// flushing, so no judgment is made — the task is marked deferred instead.
func TestCancelTask_StartedEmptyTranscript_DefersJudgment(t *testing.T) {
	ctx := context.Background()
	pool := newCancelFinalizePool(t)
	f := createCancelFinalizeFixture(t, ctx, pool, "running", true)
	svc := NewTaskService(db.New(pool), pool, nil, events.New())

	result, err := svc.CancelTaskWithResult(ctx, util.MustParseUUID(f.taskID), CancelTaskOptions{ClientSupportsDraftRestore: true})
	if err != nil {
		t.Fatalf("cancel task: %v", err)
	}
	if result.CancelledChatMessage != nil {
		t.Fatalf("expected no synchronous restore result, got %+v", result.CancelledChatMessage)
	}
	if !f.userMessageExists(t, ctx) {
		t.Error("user chat message must not be deleted while deferred")
	}
	if got := f.assistantMessages(t, ctx); len(got) != 0 {
		t.Errorf("no assistant message should exist while deferred, got %v", got)
	}
	if got := f.chatFinalizeDeferredAt(t, ctx); got == nil {
		t.Error("chat_finalize_deferred_at should be set")
	}
}

// Deferred finalize with a transcript that stayed empty: the user message is
// deleted, a durable chat_draft_restore row is persisted in the same tx, and
// a content-free restored-outcome event is broadcast as an invalidation hint.
func TestFinalizeDeferredCancelledChat_StillEmpty_RestoresDraft(t *testing.T) {
	ctx := context.Background()
	pool := newCancelFinalizePool(t)
	f := createCancelFinalizeFixture(t, ctx, pool, "running", true)
	bus := events.New()
	rec := recordCancelFinalizedEvents(bus)
	svc := NewTaskService(db.New(pool), pool, nil, bus)

	if _, err := svc.CancelTaskWithResult(ctx, util.MustParseUUID(f.taskID), CancelTaskOptions{ClientSupportsDraftRestore: true}); err != nil {
		t.Fatalf("cancel task: %v", err)
	}

	svc.FinalizeDeferredCancelledChat(ctx, util.MustParseUUID(f.taskID))

	if f.userMessageExists(t, ctx) {
		t.Error("user chat message should have been deleted")
	}
	if got := f.chatFinalizeDeferredAt(t, ctx); got != nil {
		t.Errorf("chat_finalize_deferred_at should be cleared, got %v", got)
	}
	restores := f.draftRestores(t, ctx)
	if len(restores) != 1 {
		t.Fatalf("expected 1 chat_draft_restore row, got %d", len(restores))
	}
	if got := util.UUIDToString(restores[0].ID); got != f.userMessageID {
		t.Errorf("restore id = %q, want deleted message id %q", got, f.userMessageID)
	}
	if restores[0].Content != "run the thing" {
		t.Errorf("restore content = %q, want %q", restores[0].Content, "run the thing")
	}
	if got := util.UUIDToString(restores[0].TaskID); got != f.taskID {
		t.Errorf("restore task_id = %q, want %q", got, f.taskID)
	}
	evts := rec.snapshot()
	if len(evts) != 1 {
		t.Fatalf("expected 1 chat:cancel_finalized event, got %d", len(evts))
	}
	payload, ok := evts[0].Payload.(protocol.ChatCancelFinalizedPayload)
	if !ok {
		t.Fatalf("payload type = %T", evts[0].Payload)
	}
	if payload.Outcome != protocol.ChatCancelOutcomeRestored {
		t.Errorf("outcome = %q, want restored", payload.Outcome)
	}
	if payload.MessageID != f.userMessageID {
		t.Errorf("message_id = %q, want %q", payload.MessageID, f.userMessageID)
	}
	if payload.Content != "" {
		t.Errorf("restored event must not carry the prompt content, got %q", payload.Content)
	}
	if payload.ChatSessionID != f.chatSessionID {
		t.Errorf("chat_session_id = %q, want %q", payload.ChatSessionID, f.chatSessionID)
	}
}

// The recoverability guarantee behind the restored outcome: the draft-restore
// row commits with the settlement tx, so a client that never receives the
// chat:cancel_finalized broadcast (offline, or the server dies between commit
// and publish — simulated here by a bus with no subscribers) still finds the
// restore, with its detached attachment ids, on the next fetch.
func TestFinalizeDeferredCancelledChat_BroadcastLost_RestoreIsRecoverable(t *testing.T) {
	ctx := context.Background()
	pool := newCancelFinalizePool(t)
	f := createCancelFinalizeFixture(t, ctx, pool, "running", true)
	attachmentID := f.attachUserMessageAttachment(t, ctx)
	queries := db.New(pool)
	svc := NewTaskService(queries, pool, nil, events.New())

	if _, err := svc.CancelTaskWithResult(ctx, util.MustParseUUID(f.taskID), CancelTaskOptions{ClientSupportsDraftRestore: true}); err != nil {
		t.Fatalf("cancel task: %v", err)
	}

	svc.FinalizeDeferredCancelledChat(ctx, util.MustParseUUID(f.taskID))

	restores, err := queries.ListChatDraftRestoresBySession(ctx, util.MustParseUUID(f.chatSessionID))
	if err != nil {
		t.Fatalf("list draft restores: %v", err)
	}
	if len(restores) != 1 {
		t.Fatalf("expected 1 recoverable draft restore, got %d", len(restores))
	}
	if restores[0].Content != "run the thing" {
		t.Errorf("restore content = %q, want %q", restores[0].Content, "run the thing")
	}
	if len(restores[0].AttachmentIds) != 1 || util.UUIDToString(restores[0].AttachmentIds[0]) != attachmentID {
		t.Errorf("restore attachment_ids = %v, want [%s]", restores[0].AttachmentIds, attachmentID)
	}
	// The attachment row itself must have survived the message delete
	// (detached, so the restored draft can re-bind it on re-send).
	var chatMessageID *string
	if err := pool.QueryRow(ctx, `
		SELECT chat_message_id FROM attachment WHERE id = $1
	`, attachmentID).Scan(&chatMessageID); err != nil {
		t.Fatalf("attachment row should survive: %v", err)
	}
	if chatMessageID != nil {
		t.Errorf("attachment should be detached, still bound to %v", *chatMessageID)
	}

	// Consume is idempotent: first delete claims the row, second is a no-op.
	for i, want := range []int64{1, 0} {
		n, err := queries.DeleteChatDraftRestore(ctx, db.DeleteChatDraftRestoreParams{
			ID:            restores[0].ID,
			ChatSessionID: util.MustParseUUID(f.chatSessionID),
		})
		if err != nil {
			t.Fatalf("consume draft restore (call %d): %v", i+1, err)
		}
		if n != want {
			t.Errorf("consume rows (call %d) = %d, want %d", i+1, n, want)
		}
	}
}

// Deferred finalize after transcript rows landed late: the judgment flips to
// non-empty, a Stopped. row is written, and a stopped-outcome event is
// broadcast.
func TestFinalizeDeferredCancelledChat_RowsLanded_WritesStopped(t *testing.T) {
	ctx := context.Background()
	pool := newCancelFinalizePool(t)
	f := createCancelFinalizeFixture(t, ctx, pool, "running", true)
	bus := events.New()
	rec := recordCancelFinalizedEvents(bus)
	svc := NewTaskService(db.New(pool), pool, nil, bus)

	if _, err := svc.CancelTaskWithResult(ctx, util.MustParseUUID(f.taskID), CancelTaskOptions{ClientSupportsDraftRestore: true}); err != nil {
		t.Fatalf("cancel task: %v", err)
	}
	// The daemon's late flush lands after the cancel commit.
	f.insertTranscriptRow(t, ctx)

	svc.FinalizeDeferredCancelledChat(ctx, util.MustParseUUID(f.taskID))

	if !f.userMessageExists(t, ctx) {
		t.Error("user chat message should be preserved")
	}
	if got := f.assistantMessages(t, ctx); len(got) != 1 || got[0] != "Stopped." {
		t.Errorf("assistant messages = %v, want [Stopped.]", got)
	}
	if got := f.chatFinalizeDeferredAt(t, ctx); got != nil {
		t.Errorf("chat_finalize_deferred_at should be cleared, got %v", got)
	}
	evts := rec.snapshot()
	if len(evts) != 1 {
		t.Fatalf("expected 1 chat:cancel_finalized event, got %d", len(evts))
	}
	payload, ok := evts[0].Payload.(protocol.ChatCancelFinalizedPayload)
	if !ok {
		t.Fatalf("payload type = %T", evts[0].Payload)
	}
	if payload.Outcome != protocol.ChatCancelOutcomeStopped {
		t.Errorf("outcome = %q, want stopped", payload.Outcome)
	}
	if payload.Content != "Stopped." {
		t.Errorf("content = %q, want Stopped.", payload.Content)
	}
	if payload.MessageID == "" {
		t.Error("message_id should carry the new assistant row id")
	}
}

// The marker is an atomic claim: a second finalize call must be a no-op.
func TestFinalizeDeferredCancelledChat_SecondCallIsNoop(t *testing.T) {
	ctx := context.Background()
	pool := newCancelFinalizePool(t)
	f := createCancelFinalizeFixture(t, ctx, pool, "running", true)
	bus := events.New()
	rec := recordCancelFinalizedEvents(bus)
	svc := NewTaskService(db.New(pool), pool, nil, bus)

	if _, err := svc.CancelTaskWithResult(ctx, util.MustParseUUID(f.taskID), CancelTaskOptions{ClientSupportsDraftRestore: true}); err != nil {
		t.Fatalf("cancel task: %v", err)
	}
	f.insertTranscriptRow(t, ctx)

	svc.FinalizeDeferredCancelledChat(ctx, util.MustParseUUID(f.taskID))
	svc.FinalizeDeferredCancelledChat(ctx, util.MustParseUUID(f.taskID))

	if got := f.assistantMessages(t, ctx); len(got) != 1 {
		t.Errorf("assistant messages = %v, want exactly one Stopped.", got)
	}
	if got := len(rec.snapshot()); got != 1 {
		t.Errorf("events = %d, want 1", got)
	}
}

// Sweeper query: only markers older than the grace period are returned.
func TestListChatFinalizeDeferredExpired_HonorsGrace(t *testing.T) {
	ctx := context.Background()
	pool := newCancelFinalizePool(t)
	f := createCancelFinalizeFixture(t, ctx, pool, "running", true)
	queries := db.New(pool)
	svc := NewTaskService(queries, pool, nil, events.New())

	if _, err := svc.CancelTaskWithResult(ctx, util.MustParseUUID(f.taskID), CancelTaskOptions{ClientSupportsDraftRestore: true}); err != nil {
		t.Fatalf("cancel task: %v", err)
	}

	fresh, err := queries.ListChatFinalizeDeferredExpired(ctx, db.ListChatFinalizeDeferredExpiredParams{
		GraceSecs:  60,
		MaxPerTick: 100,
	})
	if err != nil {
		t.Fatalf("list expired: %v", err)
	}
	for _, row := range fresh {
		if util.UUIDToString(row.ID) == f.taskID {
			t.Fatal("fresh marker must not be returned within the grace period")
		}
	}

	if _, err := pool.Exec(ctx, `
		UPDATE agent_task_queue SET chat_finalize_deferred_at = now() - interval '2 minutes' WHERE id = $1
	`, f.taskID); err != nil {
		t.Fatalf("backdate marker: %v", err)
	}

	expired, err := queries.ListChatFinalizeDeferredExpired(ctx, db.ListChatFinalizeDeferredExpiredParams{
		GraceSecs:  60,
		MaxPerTick: 100,
	})
	if err != nil {
		t.Fatalf("list expired: %v", err)
	}
	found := false
	for _, row := range expired {
		if util.UUIDToString(row.ID) == f.taskID {
			found = true
		}
	}
	if !found {
		t.Error("backdated marker should be returned once past the grace period")
	}
}
