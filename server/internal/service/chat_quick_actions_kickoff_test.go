package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestAdoptOrphanOnboardingKickoff pins the handoff the instant opening depends
// on: the kickoff row is written with no task, and the member's first real send
// is what delivers it to a runtime. If this stops working the profile block and
// the "you already greeted them" instruction never reach the model, and Mika
// introduces herself a second time (MUL-5827).
func TestAdoptOrphanOnboardingKickoff(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)

	var chatSessionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3) RETURNING id`, workspaceID, agentID, userID).Scan(&chatSessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM chat_message WHERE chat_session_id = $1`, chatSessionID)
		pool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, chatSessionID)
	})

	// The kickoff as OpenMikaOnboardingChat writes it: a user row with no task.
	if _, err := pool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content, message_kind)
		VALUES ($1, 'user', 'kickoff context', 'onboarding_kickoff')`, chatSessionID); err != nil {
		t.Fatalf("seed orphan kickoff: %v", err)
	}

	var firstTaskID string
	if err := pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&firstTaskID); err != nil {
		t.Fatalf("new task id: %v", err)
	}
	if err := q.AdoptOrphanOnboardingKickoff(ctx, db.AdoptOrphanOnboardingKickoffParams{
		ChatSessionID: util.MustParseUUID(chatSessionID),
		TaskID:        util.MustParseUUID(firstTaskID),
	}); err != nil {
		t.Fatalf("AdoptOrphanOnboardingKickoff: %v", err)
	}

	input, err := q.ListChatInputMessages(ctx, util.MustParseUUID(firstTaskID))
	if err != nil {
		t.Fatalf("ListChatInputMessages: %v", err)
	}
	if len(input) != 1 || input[0].MessageKind != protocol.ChatMessageKindOnboardingKickoff {
		t.Fatalf("first turn's input batch = %+v, want the adopted kickoff", input)
	}

	// A second send must not steal it back: the batch is immutable once sealed,
	// and re-delivering the "you already greeted them" context on every turn
	// would have Mika re-reading the opening for the rest of the conversation.
	var secondTaskID string
	if err := pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&secondTaskID); err != nil {
		t.Fatalf("new task id: %v", err)
	}
	if err := q.AdoptOrphanOnboardingKickoff(ctx, db.AdoptOrphanOnboardingKickoffParams{
		ChatSessionID: util.MustParseUUID(chatSessionID),
		TaskID:        util.MustParseUUID(secondTaskID),
	}); err != nil {
		t.Fatalf("AdoptOrphanOnboardingKickoff (second send): %v", err)
	}
	second, err := q.ListChatInputMessages(ctx, util.MustParseUUID(secondTaskID))
	if err != nil {
		t.Fatalf("ListChatInputMessages (second send): %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("second send adopted %d row(s), want none", len(second))
	}
	still, err := q.ListChatInputMessages(ctx, util.MustParseUUID(firstTaskID))
	if err != nil {
		t.Fatalf("ListChatInputMessages (first send, after): %v", err)
	}
	if len(still) != 1 {
		t.Fatalf("first turn lost its kickoff: %+v", still)
	}
}

// TestWriteChatCompletionOutcomeNeverStampsOnboardingOpening covers the rule
// that replaced the old kickoff-input stamping. The server writes the opening
// itself now, and the kickoff rides into the member's FIRST REAL turn — so a
// completion that sees kickoff input must still persist a plain 'message'.
// Stamping it would render the starter cards a second time, under a reply that
// is not an opening (MUL-5827).
func TestWriteChatCompletionOutcomeNeverStampsOnboardingOpening(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)

	var chatSessionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3) RETURNING id`, workspaceID, agentID, userID).Scan(&chatSessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM chat_message WHERE chat_session_id = $1`, chatSessionID)
		pool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, chatSessionID)
	})

	// The member's first real turn, shaped exactly as the send path leaves it:
	// the adopted kickoff and their own message share one input batch.
	var taskID string
	if err := pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&taskID); err != nil {
		t.Fatalf("new task id: %v", err)
	}
	for _, row := range []struct{ kind, content string }{
		{"onboarding_kickoff", "kickoff context"},
		{"message", "Turn our current goals into a project board"},
	} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO chat_message (chat_session_id, role, content, message_kind, task_id)
			VALUES ($1, 'user', $2, $3, $4)`, chatSessionID, row.content, row.kind, taskID); err != nil {
			t.Fatalf("seed %s input: %v", row.kind, err)
		}
	}

	svc := &TaskService{Queries: q, TxStarter: pool}
	result, _ := json.Marshal(protocol.TaskCompletedPayload{Output: "Here's the board I'd create."})
	row, err := svc.writeChatCompletionOutcome(ctx, q, db.AgentTaskQueue{
		ID:            util.MustParseUUID(taskID),
		ChatSessionID: util.MustParseUUID(chatSessionID),
		AgentID:       util.MustParseUUID(agentID),
	}, result)
	if err != nil {
		t.Fatalf("writeChatCompletionOutcome: %v", err)
	}
	if row == nil {
		t.Fatal("writeChatCompletionOutcome wrote no row")
	}
	if row.MessageKind == protocol.ChatMessageKindOnboardingOpening {
		t.Errorf("the first real turn's reply was stamped as the opening; starter cards would render twice")
	}
	if row.MessageKind != protocol.ChatMessageKindMessage {
		t.Errorf("reply kind = %q, want %q", row.MessageKind, protocol.ChatMessageKindMessage)
	}
}

// TestWriteChatCompletionOutcomeStampsAPreDeployKickoffTurn covers the rolling
// deploy: a kickoff task the PREVIOUS server enqueued, claimed by this one. Its
// input is a kickoff and nothing else — the old shape — and its reply really is
// that member's opening, so it must still be stamped. message_kind is
// persisted and nothing recomputes it, so getting this wrong costs that session
// its starter cards permanently.
func TestWriteChatCompletionOutcomeStampsAPreDeployKickoffTurn(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)

	var chatSessionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3) RETURNING id`, workspaceID, agentID, userID).Scan(&chatSessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM chat_message WHERE chat_session_id = $1`, chatSessionID)
		pool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, chatSessionID)
	})

	var taskID string
	if err := pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&taskID); err != nil {
		t.Fatalf("new task id: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content, message_kind, task_id)
		VALUES ($1, 'user', 'kickoff context', 'onboarding_kickoff', $2)`, chatSessionID, taskID); err != nil {
		t.Fatalf("seed pre-deploy kickoff turn: %v", err)
	}

	svc := &TaskService{Queries: q, TxStarter: pool}
	result, _ := json.Marshal(protocol.TaskCompletedPayload{Output: "Hi, I'm Mika."})
	row, err := svc.writeChatCompletionOutcome(ctx, q, db.AgentTaskQueue{
		ID:            util.MustParseUUID(taskID),
		ChatSessionID: util.MustParseUUID(chatSessionID),
		AgentID:       util.MustParseUUID(agentID),
	}, result)
	if err != nil {
		t.Fatalf("writeChatCompletionOutcome: %v", err)
	}
	if row == nil {
		t.Fatal("writeChatCompletionOutcome wrote no row")
	}
	if row.MessageKind != protocol.ChatMessageKindOnboardingOpening {
		t.Fatalf("pre-deploy kickoff reply kind = %q, want %q — that session loses its starter cards",
			row.MessageKind, protocol.ChatMessageKindOnboardingOpening)
	}
}

// TestOnboardingKickoffSurvivesCancelAndReanchor covers the two shared chat
// mechanisms that a two-row input batch newly exposes. Both used to be safe by
// assumption ("a direct send owns exactly one user row"), and both would fail
// silently: the member would simply find Mika introducing herself again, or
// reading their message before the product's context.
func TestOnboardingKickoffSurvivesCancelAndReanchor(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)

	var chatSessionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3) RETURNING id`, workspaceID, agentID, userID).Scan(&chatSessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM chat_message WHERE chat_session_id = $1`, chatSessionID)
		pool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, chatSessionID)
	})

	var taskID string
	if err := pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&taskID); err != nil {
		t.Fatalf("new task id: %v", err)
	}
	// The first real turn: an adopted kickoff written earlier, then the member's
	// own message. Timestamps are explicit so the order under test is the one
	// the send path actually produces.
	if _, err := pool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content, message_kind, task_id, created_at)
		VALUES ($1, 'user', 'kickoff context', 'onboarding_kickoff', $2, now() - interval '5 minutes'),
		       ($1, 'user', 'my first message', 'message', $2, now())`, chatSessionID, taskID); err != nil {
		t.Fatalf("seed first real turn: %v", err)
	}

	// Reanchoring must not move the kickoff. It is older than every visible row,
	// so an unguarded query reanchors it to dispatch time — after the member's
	// message, inverting the batch the runtime reads.
	if err := q.ReanchorClaimedDirectChatInput(ctx, db.ReanchorClaimedDirectChatInputParams{
		DispatchedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		TaskID:       util.MustParseUUID(taskID),
	}); err != nil {
		t.Fatalf("ReanchorClaimedDirectChatInput: %v", err)
	}
	batch, err := q.ListChatInputMessages(ctx, util.MustParseUUID(taskID))
	if err != nil {
		t.Fatalf("ListChatInputMessages: %v", err)
	}
	if len(batch) != 2 {
		t.Fatalf("input batch = %d row(s), want the kickoff plus the member's message", len(batch))
	}
	if batch[0].MessageKind != protocol.ChatMessageKindOnboardingKickoff {
		t.Fatalf("the runtime would read the member's message before the product context: %+v", batch)
	}

	// Cancelling that turn must give back what the member typed — never the
	// product's internal prompt — and must return the kickoff to unowned so the
	// next send re-adopts it.
	deleted, err := deleteUserChatInput(ctx, q, util.MustParseUUID(taskID))
	if err != nil {
		t.Fatalf("deleteUserChatInput: %v", err)
	}
	if deleted.Content != "my first message" {
		t.Fatalf("cancel returned %q, want the member's own message", deleted.Content)
	}

	var orphanKind string
	if err := pool.QueryRow(ctx, `
		SELECT message_kind FROM chat_message
		 WHERE chat_session_id = $1 AND task_id IS NULL AND role = 'user'`, chatSessionID).Scan(&orphanKind); err != nil {
		t.Fatalf("the kickoff did not survive the cancel as an unowned row: %v", err)
	}
	if orphanKind != protocol.ChatMessageKindOnboardingKickoff {
		t.Fatalf("unowned row kind = %q, want the kickoff", orphanKind)
	}
}

// TestFailedFirstTurnReleasesTheOnboardingKickoff covers the failure this was
// caught by in a live environment: the member's first turn died in preparation,
// the adopted kickoff stayed bound to that dead task, and their next message
// reached Mika with no onboarding context — so she introduced herself a second
// time, in a conversation she had already opened.
func TestFailedFirstTurnReleasesTheOnboardingKickoff(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)

	var chatSessionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3) RETURNING id`, workspaceID, agentID, userID).Scan(&chatSessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM chat_message WHERE chat_session_id = $1`, chatSessionID)
		pool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, chatSessionID)
	})

	var taskID string
	if err := pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&taskID); err != nil {
		t.Fatalf("new task id: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content, message_kind, task_id)
		VALUES ($1, 'user', 'kickoff context', 'onboarding_kickoff', $2)`, chatSessionID, taskID); err != nil {
		t.Fatalf("seed adopted kickoff: %v", err)
	}

	if err := q.ReleaseOnboardingKickoffFromTask(ctx, util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("ReleaseOnboardingKickoffFromTask: %v", err)
	}

	// Unowned again, so the member's next send re-adopts it.
	var orphans int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM chat_message
		 WHERE chat_session_id = $1 AND message_kind = 'onboarding_kickoff' AND task_id IS NULL`,
		chatSessionID).Scan(&orphans); err != nil {
		t.Fatalf("count orphaned kickoffs: %v", err)
	}
	if orphans != 1 {
		t.Fatalf("kickoff orphan count = %d, want 1 — the next turn would lose its onboarding context", orphans)
	}

	var nextTaskID string
	if err := pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&nextTaskID); err != nil {
		t.Fatalf("new task id: %v", err)
	}
	if err := q.AdoptOrphanOnboardingKickoff(ctx, db.AdoptOrphanOnboardingKickoffParams{
		ChatSessionID: util.MustParseUUID(chatSessionID),
		TaskID:        util.MustParseUUID(nextTaskID),
	}); err != nil {
		t.Fatalf("AdoptOrphanOnboardingKickoff: %v", err)
	}
	batch, err := q.ListChatInputMessages(ctx, util.MustParseUUID(nextTaskID))
	if err != nil {
		t.Fatalf("ListChatInputMessages: %v", err)
	}
	if len(batch) != 1 || batch[0].MessageKind != protocol.ChatMessageKindOnboardingKickoff {
		t.Fatalf("the retry-after-failure turn did not pick the kickoff back up: %+v", batch)
	}
}

// TestFailedFirstTurnHandsTheKickoffToAQueuedSuccessor covers the case send-time
// adoption structurally cannot: the member queues a second message WHILE the
// first turn is still running, so that send finds nothing to adopt and never
// gets another chance. If the dying turn only cleared the kickoff to NULL, the
// already-sealed successor would execute with no onboarding skill, no profile
// block, and no record that Mika had already greeted them — and a message sent
// later still could pick the kickoff up, delivering it to the wrong turn.
func TestFailedFirstTurnHandsTheKickoffToAQueuedSuccessor(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)

	var chatSessionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3) RETURNING id`, workspaceID, agentID, userID).Scan(&chatSessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM chat_message WHERE chat_session_id = $1`, chatSessionID)
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE chat_session_id = $1`, chatSessionID)
		pool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, chatSessionID)
	})

	// Both turns own their own input batch, as a direct send always does.
	seedTask := func(status string) string {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, chat_input_task_id)
			VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, $3, 2, gen_random_uuid())
			RETURNING id::text`, agentID, chatSessionID, status).Scan(&id); err != nil {
			t.Fatalf("seed %s task: %v", status, err)
		}
		if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET chat_input_task_id = id WHERE id = $1`, id); err != nil {
			t.Fatalf("stamp input owner: %v", err)
		}
		return id
	}

	// A: the member's first real message, running, having adopted the kickoff.
	taskA := seedTask("running")
	if _, err := pool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content, message_kind, task_id, created_at)
		VALUES ($1, 'user', 'kickoff context', 'onboarding_kickoff', $2, now() - interval '5 minutes'),
		       ($1, 'user', 'first message', 'message', $2, now() - interval '2 minutes')`,
		chatSessionID, taskA); err != nil {
		t.Fatalf("seed turn A: %v", err)
	}

	// B: queued behind A. Its send found the kickoff already owned, so adoption
	// was a no-op — this is the turn that must inherit it.
	taskB := seedTask("queued")
	if _, err := pool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content, message_kind, task_id, created_at)
		VALUES ($1, 'user', 'second message', 'message', $2, now() - interval '1 minute')`,
		chatSessionID, taskB); err != nil {
		t.Fatalf("seed turn B: %v", err)
	}

	// A dies.
	if err := q.ReleaseOnboardingKickoffFromTask(ctx, util.MustParseUUID(taskA)); err != nil {
		t.Fatalf("ReleaseOnboardingKickoffFromTask: %v", err)
	}

	batch, err := q.ListChatInputMessages(ctx, util.MustParseUUID(taskB))
	if err != nil {
		t.Fatalf("ListChatInputMessages(B): %v", err)
	}
	if len(batch) != 2 {
		t.Fatalf("B's input batch = %d row(s), want the kickoff plus B's message: %+v", len(batch), batch)
	}
	if batch[0].MessageKind != protocol.ChatMessageKindOnboardingKickoff {
		t.Errorf("B would read the member's message before the product context: %+v", batch)
	}
	if batch[1].Content != "second message" {
		t.Errorf("B's own message is missing from its batch: %+v", batch)
	}

	// Nothing is left unowned, so a later message cannot steal the kickoff.
	var orphans int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM chat_message
		 WHERE chat_session_id = $1 AND message_kind = 'onboarding_kickoff' AND task_id IS NULL`,
		chatSessionID).Scan(&orphans); err != nil {
		t.Fatalf("count orphans: %v", err)
	}
	if orphans != 0 {
		t.Errorf("kickoff was orphaned instead of handed to the queued successor")
	}
}

// A running successor has already had its prompt built from its input batch, so
// handing it the kickoff would consume the context without ever delivering it.
// It must fall back to unowned and wait for the member's next send.
func TestReleaseSkipsAnAlreadyStartedSuccessor(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)

	var chatSessionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3) RETURNING id`, workspaceID, agentID, userID).Scan(&chatSessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM chat_message WHERE chat_session_id = $1`, chatSessionID)
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE chat_session_id = $1`, chatSessionID)
		pool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, chatSessionID)
	})

	seedTask := func(status string) string {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, chat_input_task_id)
			VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, $3, 2, gen_random_uuid())
			RETURNING id::text`, agentID, chatSessionID, status).Scan(&id); err != nil {
			t.Fatalf("seed %s task: %v", status, err)
		}
		if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET chat_input_task_id = id WHERE id = $1`, id); err != nil {
			t.Fatalf("stamp input owner: %v", err)
		}
		return id
	}

	taskA := seedTask("running")
	if _, err := pool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content, message_kind, task_id)
		VALUES ($1, 'user', 'kickoff context', 'onboarding_kickoff', $2)`, chatSessionID, taskA); err != nil {
		t.Fatalf("seed kickoff: %v", err)
	}
	seedTask("dispatched") // successor already claimed

	if err := q.ReleaseOnboardingKickoffFromTask(ctx, util.MustParseUUID(taskA)); err != nil {
		t.Fatalf("ReleaseOnboardingKickoffFromTask: %v", err)
	}

	var orphans int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM chat_message
		 WHERE chat_session_id = $1 AND message_kind = 'onboarding_kickoff' AND task_id IS NULL`,
		chatSessionID).Scan(&orphans); err != nil {
		t.Fatalf("count orphans: %v", err)
	}
	if orphans != 1 {
		t.Fatalf("kickoff should wait unowned for the next send, got %d orphan(s)", orphans)
	}
}
