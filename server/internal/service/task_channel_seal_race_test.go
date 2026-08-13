package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestEnqueueChatTaskSealsMessageStrandedByPredecessorReply reproduces the
// lost-turn window observed on a self-hosted tenant:
//
//	18:31:17.7  the user's next message lands   -> chat_message, task_id NULL
//	18:31:20.2  the PREVIOUS turn completes     -> assistant row, newer than it
//	18:31:20.7  the 3s debounce fires           -> EnqueueChatTask seals NOTHING
//	18:31:20.7  chat claim: task-owned direct task has no user input; cancelling
//
// The race is the debounce window itself: the message waits ~3s unowned, and
// whether it is ever answered depends on whether the predecessor's completion
// lands inside that wait. The two transactions cannot interleave more finely
// than this — CompleteTask takes chat_session FOR UPDATE and the task INSERT
// takes the FK's FOR KEY SHARE on the same row — so "predecessor commits, then
// the flush runs" IS the reachable interleaving, and it is what the tenant hit.
//
// The seal excluded the message because a non-user row existed after it, even
// though that row was the predecessor's reply to an OLDER, already-sealed
// batch. The new task then owned an empty input batch, and the claim guard
// cancelled it: the message was accepted and never answered.
func TestEnqueueChatTaskSealsMessageStrandedByPredecessorReply(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)
	chatSessionID := seedChannelChatSession(t, ctx, pool, workspaceID, agentID, userID)

	session := db.ChatSession{ID: util.MustParseUUID(chatSessionID), AgentID: util.MustParseUUID(agentID)}
	initiator := util.MustParseUUID(userID)
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}

	// Turn 1: an inbound channel message lands unowned; the debounce fires and
	// EnqueueChatTask seals it into its own task, which then starts running.
	appendChannelUserMessage(t, ctx, pool, chatSessionID, "测试下")
	first, err := svc.EnqueueChatTask(ctx, session, initiator, false)
	if err != nil {
		t.Fatalf("EnqueueChatTask (turn 1): %v", err)
	}
	if got := sealedInputCount(t, ctx, pool, first.ID); got != 1 {
		t.Fatalf("turn 1 sealed %d user messages, want 1", got)
	}
	markChannelTaskRunning(t, ctx, pool, first.ID)

	// The user's next message arrives WHILE turn 1 is still running, so it waits
	// out the debounce window unowned.
	stranded := appendChannelUserMessage(t, ctx, pool, chatSessionID, "挺好的 正常了")

	// Turn 1 completes inside that window: its reply row is NEWER than the
	// waiting message, but it answers turn 1's batch, not this message.
	if _, err := svc.CompleteTask(ctx, first.ID, []byte(`{"output":"通了。"}`), "", "", "", false, ""); err != nil {
		t.Fatalf("complete turn 1: %v", err)
	}

	// The debounce fires.
	next, err := svc.EnqueueChatTask(ctx, session, initiator, false)
	if err != nil {
		t.Fatalf("EnqueueChatTask (turn 2): %v", err)
	}

	var owner pgtype.UUID
	if err := pool.QueryRow(ctx, `SELECT task_id FROM chat_message WHERE id = $1`, stranded).Scan(&owner); err != nil {
		t.Fatalf("load waiting message: %v", err)
	}
	if !owner.Valid || owner.Bytes != next.ID.Bytes {
		t.Fatalf("waiting user message task_id = %q, want the new turn %q — a predecessor reply that landed after the message must not seal it out",
			util.UUIDToString(owner), util.UUIDToString(next.ID))
	}
	if got := sealedInputCount(t, ctx, pool, next.ID); got != 1 {
		t.Fatalf("turn 2 owns %d user messages, want 1 — an empty input batch is what the claim guard cancels", got)
	}
	// Sealing is not retroactive: turn 1 keeps exactly the batch it ran on.
	if got := sealedInputCount(t, ctx, pool, first.ID); got != 1 {
		t.Fatalf("turn 1 batch now holds %d user messages, want its original 1", got)
	}
}

// TestEnqueueChatTaskSealsMessageWhenReplyCommitsDuringEnqueue closes the same
// boundary one level tighter: the predecessor's reply row commits on another
// connection AFTER the enqueue transaction has begun, so the seal statement's
// READ COMMITTED snapshot is the first thing in that transaction to see it.
// Same prior art as the media-deferral race above.
func TestEnqueueChatTaskSealsMessageWhenReplyCommitsDuringEnqueue(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)
	chatSessionID := seedChannelChatSession(t, ctx, pool, workspaceID, agentID, userID)

	session := db.ChatSession{ID: util.MustParseUUID(chatSessionID), AgentID: util.MustParseUUID(agentID)}
	initiator := util.MustParseUUID(userID)
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}

	appendChannelUserMessage(t, ctx, pool, chatSessionID, "测试下")
	first, err := svc.EnqueueChatTask(ctx, session, initiator, false)
	if err != nil {
		t.Fatalf("EnqueueChatTask (turn 1): %v", err)
	}
	markChannelTaskRunning(t, ctx, pool, first.ID)
	stranded := appendChannelUserMessage(t, ctx, pool, chatSessionID, "挺好的 正常了")

	injected := false
	racing := &TaskService{
		Queries: q,
		Bus:     events.New(),
		TxStarter: &raceInjectTxStarter{pool: pool, inject: func() {
			// Turn 1's reply row, committed by a concurrent completion.
			if _, err := pool.Exec(ctx, `
				INSERT INTO chat_message (chat_session_id, role, content, task_id)
				VALUES ($1, 'assistant', '通了。', $2)`, chatSessionID, first.ID); err != nil {
				t.Errorf("inject predecessor reply: %v", err)
				return
			}
			injected = true
		}},
	}
	next, err := racing.EnqueueChatTask(ctx, session, initiator, false)
	if err != nil {
		t.Fatalf("EnqueueChatTask (turn 2): %v", err)
	}
	if !injected {
		t.Fatal("race injection did not run")
	}

	var owner pgtype.UUID
	if err := pool.QueryRow(ctx, `SELECT task_id FROM chat_message WHERE id = $1`, stranded).Scan(&owner); err != nil {
		t.Fatalf("load waiting message: %v", err)
	}
	if !owner.Valid || owner.Bytes != next.ID.Bytes {
		t.Fatalf("waiting user message task_id = %q, want the new turn %q",
			util.UUIDToString(owner), util.UUIDToString(next.ID))
	}
}

// TestEnqueueChatTaskLeavesAlreadyPassedMessagesUnowned is the other half of
// the boundary, and the reason the fix qualifies the reply instead of dropping
// the check. Two shapes of old unowned row must stay out of a new batch:
//
//   - an orphan the pre-fix seal skipped, which later turns have already run
//     and replied past. Resurrecting it minutes or hours later would replay a
//     message the conversation has moved on from.
//   - a pre-ownership transcript, whose replies carry no task at all. Sweeping
//     those would hand the agent the whole history as fresh input.
func TestEnqueueChatTaskLeavesAlreadyPassedMessagesUnowned(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)
	chatSessionID := seedChannelChatSession(t, ctx, pool, workspaceID, agentID, userID)

	// A pre-ownership pair: user row and reply, neither carrying a task.
	legacy := appendChannelUserMessage(t, ctx, pool, chatSessionID, "老消息")
	if _, err := pool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content)
		VALUES ($1, 'assistant', '老回复')`, chatSessionID); err != nil {
		t.Fatalf("seed pre-ownership reply: %v", err)
	}

	// The orphan the pre-fix seal stranded, then a later turn (created after it)
	// that ran on its own input and replied.
	orphan := appendChannelUserMessage(t, ctx, pool, chatSessionID, "被漏掉的")
	var laterTurn pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, completed_at)
		SELECT id, runtime_id, $2, 'completed', 2, now() FROM agent WHERE id = $1
		RETURNING id`, agentID, chatSessionID).Scan(&laterTurn); err != nil {
		t.Fatalf("seed later turn: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET chat_input_task_id = id WHERE id = $1`, laterTurn); err != nil {
		t.Fatalf("stamp later turn input owner: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content, task_id)
		VALUES ($1, 'user', '后一条', $2), ($1, 'assistant', '后一条的回复', $2)`,
		chatSessionID, laterTurn); err != nil {
		t.Fatalf("seed later turn transcript: %v", err)
	}

	session := db.ChatSession{ID: util.MustParseUUID(chatSessionID), AgentID: util.MustParseUUID(agentID)}
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	fresh := appendChannelUserMessage(t, ctx, pool, chatSessionID, "新消息")
	task, err := svc.EnqueueChatTask(ctx, session, util.MustParseUUID(userID), false)
	if err != nil {
		t.Fatalf("EnqueueChatTask: %v", err)
	}

	for _, tc := range []struct{ label, id string }{
		{"a pre-ownership message whose reply carries no task", legacy},
		{"an orphan a later turn has already replied past", orphan},
	} {
		var owner pgtype.UUID
		if err := pool.QueryRow(ctx, `SELECT task_id FROM chat_message WHERE id = $1`, tc.id).Scan(&owner); err != nil {
			t.Fatalf("load %s: %v", tc.label, err)
		}
		if owner.Valid {
			t.Fatalf("%s was swept into task %s; it must stay out of the new batch",
				tc.label, util.UUIDToString(owner))
		}
	}
	var freshOwner pgtype.UUID
	if err := pool.QueryRow(ctx, `SELECT task_id FROM chat_message WHERE id = $1`, fresh).Scan(&freshOwner); err != nil {
		t.Fatalf("load fresh message: %v", err)
	}
	if !freshOwner.Valid || freshOwner.Bytes != task.ID.Bytes {
		t.Fatalf("fresh message task_id = %q, want %q", util.UUIDToString(freshOwner), util.UUIDToString(task.ID))
	}
	if got := sealedInputCount(t, ctx, pool, task.ID); got != 1 {
		t.Fatalf("new turn owns %d user messages, want only the fresh one", got)
	}
}

func seedChannelChatSession(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workspaceID, agentID, userID string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3) RETURNING id`, workspaceID, agentID, userID).Scan(&id); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, id)
	})
	return id
}

// appendChannelUserMessage writes an inbound channel message the way
// ChatSession.AppendUserMessage does: durable, unowned, and stamped with the
// immutable channel_ingested provenance. The debounced flush is what later
// seals it to a task.
func appendChannelUserMessage(t *testing.T, ctx context.Context, pool *pgxpool.Pool, chatSessionID, body string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content, channel_ingested)
		VALUES ($1, 'user', $2, TRUE) RETURNING id`, chatSessionID, body).Scan(&id); err != nil {
		t.Fatalf("append channel user message %q: %v", body, err)
	}
	return id
}

func markChannelTaskRunning(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskID pgtype.UUID) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		UPDATE agent_task_queue SET status = 'running', dispatched_at = now(), started_at = now()
		WHERE id = $1`, taskID); err != nil {
		t.Fatalf("mark task running: %v", err)
	}
}

func sealedInputCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskID pgtype.UUID) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM chat_message WHERE task_id = $1 AND role = 'user'`, taskID).Scan(&n); err != nil {
		t.Fatalf("count sealed input: %v", err)
	}
	return n
}
