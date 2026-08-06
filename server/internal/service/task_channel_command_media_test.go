package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// seedChannelCommandSession creates the chat session these tests enqueue
// against and drops the rows they leave behind in it.
func seedChannelCommandSession(t *testing.T, pool *pgxpool.Pool, workspaceID, agentID, userID string) string {
	t.Helper()
	ctx := context.Background()
	var chatSessionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3) RETURNING id`, workspaceID, agentID, userID).Scan(&chatSessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM agent_task_queue WHERE chat_session_id = $1`, chatSessionID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM chat_message WHERE chat_session_id = $1`, chatSessionID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM chat_session WHERE id = $1`, chatSessionID)
	})
	return chatSessionID
}

// TestEnqueueChatTaskIgnoresHandledCommandMedia is the counterpart of
// TestEnqueueChatTaskDefersForChannelMediaAndPromotesWhenReady, which pins the
// ordinary case. A channel `/issue` turn is answered synchronously by the
// router and is deliberately excluded from every later input batch, so the
// media it carries gates nothing: the next, unrelated message must not wait for
// the command's attachments to bind — nor for the whole fallback budget on the
// create-failure path, where no binder ever runs to clear the marker.
func TestEnqueueChatTaskIgnoresHandledCommandMedia(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)
	chatSessionID := seedChannelCommandSession(t, pool, workspaceID, agentID, userID)

	var commandID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content, message_kind, channel_media_pending_until)
		VALUES ($1, 'user', '/issue login is broken [Image]', 'channel_command', $2)
		RETURNING id`, chatSessionID, time.Now().Add(time.Minute)).Scan(&commandID); err != nil {
		t.Fatalf("seed handled command: %v", err)
	}
	// The message that arms this flush: plain text of its own, no media.
	var questionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content)
		VALUES ($1, 'user', 'unrelated question') RETURNING id`, chatSessionID).Scan(&questionID); err != nil {
		t.Fatalf("seed ordinary message: %v", err)
	}

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	task, err := svc.EnqueueChatTask(ctx, db.ChatSession{
		ID:      util.MustParseUUID(chatSessionID),
		AgentID: util.MustParseUUID(agentID),
	}, util.MustParseUUID(userID), false)
	if err != nil {
		t.Fatalf("EnqueueChatTask: %v", err)
	}
	if task.Status != "queued" || task.FireAt.Valid {
		t.Fatalf("task = status %q fire_at %v, want queued at once: the handled command's media gates no batch",
			task.Status, task.FireAt)
	}

	var owner pgtype.UUID
	if err := pool.QueryRow(ctx, `SELECT task_id FROM chat_message WHERE id = $1`, commandID).Scan(&owner); err != nil {
		t.Fatalf("load command owner: %v", err)
	}
	if owner.Valid {
		t.Fatalf("handled command was sealed into a later task: %s", util.UUIDToString(owner))
	}
	if err := pool.QueryRow(ctx, `SELECT task_id FROM chat_message WHERE id = $1`, questionID).Scan(&owner); err != nil {
		t.Fatalf("load question owner: %v", err)
	}
	if !owner.Valid || owner.Bytes != task.ID.Bytes {
		t.Fatalf("question task_id = %s, want %s", util.UUIDToString(owner), util.UUIDToString(task.ID))
	}
}

// TestPromoteChannelChatTasksIgnoresHandledCommandMedia covers the second gate.
// A task deferred for its OWN media must be promoted as soon as that media
// binds, even when a command turn landed meanwhile and is still resolving its
// attachments: the command is not part of the deferred task's input.
func TestPromoteChannelChatTasksIgnoresHandledCommandMedia(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)
	chatSessionID := seedChannelCommandSession(t, pool, workspaceID, agentID, userID)

	var mediaMessageID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content, channel_media_pending_until)
		VALUES ($1, 'user', '[Image] what is this?', $2) RETURNING id`,
		chatSessionID, time.Now().Add(time.Minute)).Scan(&mediaMessageID); err != nil {
		t.Fatalf("seed pending media message: %v", err)
	}

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	task, err := svc.EnqueueChatTask(ctx, db.ChatSession{
		ID:      util.MustParseUUID(chatSessionID),
		AgentID: util.MustParseUUID(agentID),
	}, util.MustParseUUID(userID), false)
	if err != nil {
		t.Fatalf("EnqueueChatTask: %v", err)
	}
	if task.Status != "deferred" {
		t.Fatalf("task status = %q, want deferred behind its own media", task.Status)
	}

	// A `/issue` command arrives while the task waits, and starts resolving its
	// own attachments.
	if _, err := pool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content, message_kind, channel_media_pending_until)
		VALUES ($1, 'user', '/issue ship the fix [Image]', 'channel_command', $2)`,
		chatSessionID, time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("seed handled command: %v", err)
	}
	// The deferred task's own media finishes.
	if err := q.ClearChatMessageChannelMediaPending(ctx, db.ClearChatMessageChannelMediaPendingParams{
		ID:            util.MustParseUUID(mediaMessageID),
		ChatSessionID: util.MustParseUUID(chatSessionID),
	}); err != nil {
		t.Fatalf("clear pending media: %v", err)
	}
	if err := svc.PromoteChannelChatTasksIfMediaReady(ctx, util.MustParseUUID(chatSessionID)); err != nil {
		t.Fatalf("PromoteChannelChatTasksIfMediaReady: %v", err)
	}

	var status string
	var fireAt pgtype.Timestamptz
	if err := pool.QueryRow(ctx, `SELECT status, fire_at FROM agent_task_queue WHERE id = $1`, task.ID).
		Scan(&status, &fireAt); err != nil {
		t.Fatalf("load promoted task: %v", err)
	}
	if status != "queued" || fireAt.Valid {
		t.Fatalf("task = status %q fire_at %v, want queued once its own media bound", status, fireAt)
	}
}
