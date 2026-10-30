package engine

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// MUL-7234 / #8234, driven from the top. Migration 461 moved the reply trigger
// onto channel_chat_context_generation and CreateChannelTaskDeliveryFromSession
// reads it by the task's context revision; the DB-level tests in
// session_db_test.go pin that query by calling SetChannelChatContextReplyTarget
// directly.
//
// These tests cover the half that cannot: that the PRODUCTION path actually
// records a trigger, on the right generation, for the right turns. A test that
// writes the generation row itself stays green even if no inbound turn ever
// populates it — and a never-populated trigger reads as "cannot attribute",
// which degrades silently to an unquoted chat-level send rather than failing.
//
// Everything below the platform edge is real here: the Router parses `/clear`
// and `/new`, the real ChatSession writes, the real TaskService enqueues, a live
// database resolves the delivery, and the debounce runs on an injected timer.
// Only the transport boundary is faked, because that is what a webhook supplies.

// realSessionBinder adapts the shared ChatSession to SessionBinder exactly the
// way a platform adapter does (compare feishuSessionBinder). Keeping the
// mapping in the test is the point: a Router test that skipped it could not
// prove ThreadID and SenderChannelID reach the database.
type realSessionBinder struct {
	session *ChatSession
}

func (b realSessionBinder) EnsureSession(ctx context.Context, p EnsureSessionParams) (pgtype.UUID, error) {
	return b.session.EnsureSession(ctx, EnsureSessionInput{
		WorkspaceID: p.Installation.WorkspaceID, AgentID: p.Installation.AgentID,
		InstallationID: p.Installation.ID, Sender: p.Sender,
		BindingKey: p.Message.Source.ChatID, ChatType: p.Message.Source.ChatType,
	})
}

func (b realSessionBinder) StartSession(ctx context.Context, p StartSessionParams) (StartSessionResult, error) {
	return b.session.StartSession(ctx, StartSessionInput{
		EnsureSessionInput: EnsureSessionInput{
			WorkspaceID: p.Installation.WorkspaceID, AgentID: p.Installation.AgentID,
			InstallationID: p.Installation.ID, Sender: p.Sender,
			BindingKey: p.Message.Source.ChatID, ChatType: p.Message.Source.ChatType,
		},
		Body:            p.Message.Text,
		MessageID:       p.Message.MessageID,
		ThreadID:        p.Message.Source.ThreadID,
		SenderChannelID: p.Message.Source.SenderID,
		PersistMessage:  p.PersistMessage,
		Initiator:       p.Sender,
	})
}

func (b realSessionBinder) MarkPendingFresh(ctx context.Context, sessionID pgtype.UUID, messageID string) error {
	return b.session.MarkPendingFresh(ctx, sessionID, messageID)
}

func (b realSessionBinder) AppendMessage(ctx context.Context, p AppendParams) (AppendResult, error) {
	commandText := p.Message.CommandText
	if commandText == "" {
		commandText = p.Message.Text
	}
	return b.session.AppendUserMessage(ctx, AppendInput{
		SessionID:       p.SessionID,
		Sender:          p.Sender,
		InstallationID:  p.InstallationID,
		Body:            p.Message.Text,
		CommandText:     commandText,
		MessageID:       p.Message.MessageID,
		ThreadID:        p.Message.Source.ThreadID,
		SenderChannelID: p.Message.Source.SenderID,
		ForceFresh:      p.Message.ForceFresh,
	})
}

func (b realSessionBinder) BindMedia(context.Context, BindMediaParams) (BindMediaResult, error) {
	return BindMediaResult{}, nil
}

type anchorHarness struct {
	pool    *pgxpool.Pool
	router  *Router
	timers  *fakeTimerFactory
	fixture sessionPersistenceFixture
}

func newAnchorHarness(t *testing.T) *anchorHarness {
	t.Helper()
	pool := sessionPersistenceTestDB(t)
	fixture := seedSessionPersistenceFixture(t, pool)
	queries := db.New(pool)

	tasks := &service.TaskService{Queries: queries, TxStarter: pool, Bus: events.New()}
	binder := realSessionBinder{session: NewChatSession(queries, pool, channel.Type("lark"), SessionTitles{})}
	router := NewRouter(&fakeIssues{}, tasks, queries, RouterConfig{Logger: discardLogger()})
	timers := &fakeTimerFactory{}
	router.batcher = newTestBatcher(timers)
	router.Register(channel.TypeFeishu, ResolverSet{
		Installation: &fakeInstaller{inst: ResolvedInstallation{
			ID:              fixture.installationID,
			WorkspaceID:     fixture.workspaceID,
			AgentID:         fixture.agentID,
			InstallerUserID: fixture.userID,
			Active:          true,
		}},
		Identity:   &fakeIdentity{id: ResolvedIdentity{UserID: fixture.userID}},
		Dedup:      &fakeDedup{},
		Session:    binder,
		Audit:      &fakeAuditor{},
		Replier:    &fakeReplier{},
		Typing:     &fakeTyping{},
		Media:      &fakeMedia{},
		OriginType: "lark_chat",
	})
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, `
			DELETE FROM channel_task_delivery WHERE task_id IN (
				SELECT id FROM agent_task_queue WHERE chat_session_id = $1)`, fixture.sessionID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM agent_task_queue WHERE chat_session_id = $1`, fixture.sessionID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM chat_message WHERE chat_session_id = $1`, fixture.sessionID)
	})
	return &anchorHarness{pool: pool, router: router, timers: timers, fixture: fixture}
}

func (h *anchorHarness) inbound(messageID, threadID, senderID, text string) channel.InboundMessage {
	return channel.InboundMessage{
		EventID:     "evt-" + messageID,
		MessageID:   messageID,
		Type:        channel.MsgTypeText,
		Text:        text,
		CommandText: text,
		Source: channel.Source{
			ChannelType: channel.TypeFeishu,
			ChatID:      h.fixture.channelChatID,
			ChatType:    channel.ChatTypeP2P,
			SenderID:    senderID,
			ThreadID:    threadID,
		},
	}
}

// generationTrigger reads back what the PRODUCTION write path recorded for one
// context generation of this session.
func (h *anchorHarness) generationTrigger(t *testing.T, sessionID pgtype.UUID, revision int64) (messageID, threadID, senderID pgtype.Text) {
	t.Helper()
	if err := h.pool.QueryRow(context.Background(), `
		SELECT last_message_id, last_thread_id, last_sender_id
		FROM channel_chat_context_generation
		WHERE chat_session_id = $1 AND revision = $2`, sessionID, revision,
	).Scan(&messageID, &threadID, &senderID); err != nil {
		t.Fatalf("load generation %d trigger: %v", revision, err)
	}
	return messageID, threadID, senderID
}

func (h *anchorHarness) deliveryForRevision(t *testing.T, revision int64) (taskID pgtype.UUID, messageID, threadID, senderID pgtype.Text) {
	t.Helper()
	if err := h.pool.QueryRow(context.Background(), `
		SELECT task.id, delivery.channel_message_id, delivery.channel_thread_id, delivery.channel_sender_id
		FROM agent_task_queue AS task
		JOIN channel_task_delivery AS delivery ON delivery.task_id = task.id
		WHERE task.chat_session_id = $1 AND task.channel_context_revision = $2`,
		h.fixture.sessionID, revision,
	).Scan(&taskID, &messageID, &threadID, &senderID); err != nil {
		t.Fatalf("load delivery for context revision %d: %v", revision, err)
	}
	return taskID, messageID, threadID, senderID
}

func (h *anchorHarness) waitForDeliveries(t *testing.T, want int) {
	t.Helper()
	if !waitFor(5*time.Second, func() bool {
		var n int
		if err := h.pool.QueryRow(context.Background(), `
			SELECT count(*) FROM channel_task_delivery AS delivery
			JOIN agent_task_queue AS task ON task.id = delivery.task_id
			WHERE task.chat_session_id = $1`, h.fixture.sessionID).Scan(&n); err != nil {
			return false
		}
		return n == want
	}) {
		t.Fatalf("did not observe %d delivery snapshots", want)
	}
}

// TestRouterAnchorsPendingGenerationToItsOwnTriggerAcrossClear drives the exact
// sequence from the issue through the production pipeline:
//
//	A lands in generation 1 and arms its debounce
//	`/clear` + B advance the session to generation 2 and move the binding cursor
//	the generation-1 window finally fires
//
// The generation-1 task answers A, so its reply must quote A, thread into A's
// thread, and mention A's sender. Sourcing any of the three from the binding
// cursor — which by flush time reads B — is the defect.
func TestRouterAnchorsPendingGenerationToItsOwnTriggerAcrossClear(t *testing.T) {
	h := newAnchorHarness(t)
	ctx := context.Background()

	if err := h.router.Handle(ctx, h.inbound("msg-A", "thread-A", "ou_alice", "帮我看下这个报错")); err != nil {
		t.Fatalf("handle A: %v", err)
	}
	if got := h.router.batcher.pendingCount(); got != 1 {
		t.Fatalf("message A armed %d run windows, want 1", got)
	}

	// The Router parses `/clear` itself off CommandText and turns it into the
	// generation boundary — the adapter only forwards the text.
	if err := h.router.Handle(ctx, h.inbound("msg-B", "thread-B", "ou_bob", "/clear 换个话题")); err != nil {
		t.Fatalf("handle B: %v", err)
	}
	if got := h.router.batcher.pendingCount(); got != 2 {
		t.Fatalf("pending run windows after /clear = %d, want 2 — the pre-boundary flush must survive", got)
	}

	// The production writers put each turn's trigger on its own generation.
	msgID, threadID, senderID := h.generationTrigger(t, h.fixture.sessionID, 1)
	if msgID.String != "msg-A" || threadID.String != "thread-A" || senderID.String != "ou_alice" {
		t.Errorf("generation 1 trigger = (%q, %q, %q), want A's — AppendUserMessage must record the turn it just wrote",
			msgID.String, threadID.String, senderID.String)
	}

	// Pin the hazard: the session-wide cursor has moved to B, so reading the
	// delivery from the binding would hand generation 1 B's message.
	var cursorMessage pgtype.Text
	if err := h.pool.QueryRow(ctx, `
		SELECT last_message_id FROM channel_chat_session_binding WHERE chat_session_id = $1`,
		h.fixture.sessionID).Scan(&cursorMessage); err != nil {
		t.Fatalf("load binding cursor: %v", err)
	}
	if cursorMessage.String != "msg-B" {
		t.Fatalf("binding cursor = %q, want it moved to B — otherwise this test is not reproducing the race", cursorMessage.String)
	}

	h.timers.fireArmed()
	h.waitForDeliveries(t, 2)

	_, deliveryMessage, deliveryThread, deliverySender := h.deliveryForRevision(t, 1)
	if deliveryMessage.String != "msg-A" {
		t.Errorf("generation-1 delivery quotes %q, want msg-A", deliveryMessage.String)
	}
	if deliveryThread.String != "thread-A" {
		t.Errorf("generation-1 delivery threads into %q, want thread-A", deliveryThread.String)
	}
	if deliverySender.String != "ou_alice" {
		t.Errorf("generation-1 delivery mentions %q, want ou_alice — answering A must not @ the member who typed /clear",
			deliverySender.String)
	}

	// The newer generation still targets its own trigger, so the fix does not
	// simply freeze every task onto the oldest turn.
	_, gen2Message, gen2Thread, gen2Sender := h.deliveryForRevision(t, 2)
	if gen2Message.String != "msg-B" || gen2Thread.String != "thread-B" || gen2Sender.String != "ou_bob" {
		t.Errorf("generation-2 delivery = (%q, %q, %q), want B's", gen2Message.String, gen2Thread.String, gen2Sender.String)
	}
}

// TestRouterKeepsCommandTurnOutOfGenerationTrigger covers the `cmd == nil`
// guard from the production side. A `/issue` turn is durable and advances the
// binding cursor, but it is excluded from agent input — so it is not the
// question being answered and must not become the message the answer quotes.
// Nothing about a context revision distinguishes this case: the command lands
// in the SAME generation as the question.
func TestRouterKeepsCommandTurnOutOfGenerationTrigger(t *testing.T) {
	h := newAnchorHarness(t)
	ctx := context.Background()

	if err := h.router.Handle(ctx, h.inbound("msg-user", "thread-user", "ou_alice", "这个 bug 怎么修")); err != nil {
		t.Fatalf("handle question: %v", err)
	}
	if err := h.router.Handle(ctx, h.inbound("msg-command", "thread-command", "ou_bob", "/issue 建个跟进")); err != nil {
		t.Fatalf("handle /issue: %v", err)
	}

	msgID, threadID, senderID := h.generationTrigger(t, h.fixture.sessionID, 1)
	if msgID.String != "msg-user" || threadID.String != "thread-user" || senderID.String != "ou_alice" {
		t.Fatalf("generation trigger = (%q, %q, %q), want the question's — a command turn is not agent input and must not overwrite it",
			msgID.String, threadID.String, senderID.String)
	}

	// The binding cursor DOES advance for the command, which is what makes the
	// generation trigger the only trustworthy source here.
	var cursorMessage pgtype.Text
	if err := h.pool.QueryRow(ctx, `
		SELECT last_message_id FROM channel_chat_session_binding WHERE chat_session_id = $1`,
		h.fixture.sessionID).Scan(&cursorMessage); err != nil {
		t.Fatalf("load binding cursor: %v", err)
	}
	if cursorMessage.String != "msg-command" {
		t.Fatalf("binding cursor = %q, want msg-command — the divergence this guard exists for", cursorMessage.String)
	}

	h.timers.fireArmed()
	h.waitForDeliveries(t, 1)

	_, deliveryMessage, _, deliverySender := h.deliveryForRevision(t, 1)
	if deliveryMessage.String != "msg-user" || deliverySender.String != "ou_alice" {
		t.Errorf("delivery = (%q, %q), want the question's message and sender", deliveryMessage.String, deliverySender.String)
	}
}

// TestRouterRecordsGenerationTriggerOnNewRouteFirstTurn covers the second
// production writer. `/new` rotates the route and persists its first message
// through StartSession, not AppendUserMessage, so that path needs its own proof
// that it seeds the new generation's trigger.
func TestRouterRecordsGenerationTriggerOnNewRouteFirstTurn(t *testing.T) {
	h := newAnchorHarness(t)
	ctx := context.Background()

	if err := h.router.Handle(ctx, h.inbound("msg-new", "thread-new", "ou_alice", "/new 开个新话题")); err != nil {
		t.Fatalf("handle /new: %v", err)
	}

	// `/new` rotates to a fresh session; its generation 1 is the one that must
	// carry the trigger.
	var rotatedSession pgtype.UUID
	if err := h.pool.QueryRow(ctx, `
		SELECT chat_session_id FROM channel_chat_session_binding
		WHERE installation_id = $1 AND channel_chat_id = $2 AND retired_at IS NULL`,
		h.fixture.installationID, h.fixture.channelChatID).Scan(&rotatedSession); err != nil {
		t.Fatalf("load rotated route: %v", err)
	}

	msgID, threadID, senderID := h.generationTrigger(t, rotatedSession, 1)
	if msgID.String != "msg-new" || threadID.String != "thread-new" || senderID.String != "ou_alice" {
		t.Errorf("StartSession recorded generation trigger (%q, %q, %q), want the /new turn's",
			msgID.String, threadID.String, senderID.String)
	}
}
