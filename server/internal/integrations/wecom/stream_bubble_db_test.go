package wecom

// stream_bubble_db_test.go — the bubble end to end against a real database.
//
// Every bubble test in this package drives a fake queries layer, which proves
// the store and the two subscribers agree with each other and nothing about
// what a real row does to them: the origin gate, the delivery row, the
// installation's status are all answered by a double there. This one keeps the
// fake only where the WeCom platform would be — the socket — and runs
// everything else for real: *db.Queries against a migrated database, the real
// TypingIndicatorManager, the real Outbound, one events.Bus.
//
// Skips when no migrated database is reachable, same as the other _db_ tests
// in this package.

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// bubbleReplica is one backend process holding the bot's socket: its own bus,
// registry, store, typing indicator and outbound subscriber, all over the one
// shared database.
type bubbleReplica struct {
	conn    *recordingConn
	streams *streamStore
	typing  *TypingIndicatorManager
	bus     *events.Bus
	logs    *strings.Builder
}

func newBubbleReplica(t *testing.T, q *db.Queries, instID pgtype.UUID) *bubbleReplica {
	t.Helper()
	r := &bubbleReplica{conn: &recordingConn{}, streams: newStreamStore(), bus: events.New(), logs: &strings.Builder{}}
	reg := newSendersRegistry()
	reg.set(instID, r.conn.autoAck(newWSSender(r.conn, nil)))
	logger := slog.New(slog.NewTextHandler(r.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r.typing = NewTypingIndicator(TypingIndicatorConfig{
		Senders:    reg,
		Streams:    r.streams,
		Tasks:      q,
		Deliveries: q,
		Languages:  q,
		Identities: q,
		Logger:     logger,
		GuardAfter: -1,
	})
	r.typing.Register(r.bus)
	NewOutbound(q, reg, r.streams, logger).Register(r.bus)
	return r
}

// asked is the Router's half of a WeCom question arriving: the message is
// ingested (OnIngested, with the callback's req_id in the raw envelope) and the
// debounced flush names the run it created (OnRunStarted).
func (r *bubbleReplica) asked(t *testing.T, turn boundTurn, reqID string) {
	t.Helper()
	raw, err := json.Marshal(InboundMessage{
		BotID: "BOT", ChatID: turn.chatID, ChatType: "single", SenderUserID: "USER_1", ReqID: reqID,
	})
	if err != nil {
		t.Fatalf("marshal raw: %v", err)
	}
	sessionID := mustParseTaskUUID(t, turn.sessionID)
	r.typing.OnIngested(context.Background(),
		engine.ResolvedInstallation{ID: mustParseTaskUUID(t, turn.instID)},
		channel.InboundMessage{
			Text:   "S270 的价格",
			Source: channel.Source{ChannelType: TypeWecom, ChatID: turn.chatID, ChatType: channel.ChatTypeP2P, SenderID: "USER_1"},
			Raw:    raw,
		},
		sessionID, 1)
	r.typing.OnRunStarted(context.Background(), sessionID, 1, mustParseTaskUUID(t, turn.taskID))
}

// wire decodes what the socket carried: the stream frames in order, and the
// bodies of every aibot_send_msg.
func (r *bubbleReplica) wire(t *testing.T) (streams []map[string]any, sends []map[string]any) {
	t.Helper()
	r.conn.mu.Lock()
	defer r.conn.mu.Unlock()
	for _, f := range r.conn.frames {
		var body map[string]any
		if err := json.Unmarshal(f.Body, &body); err != nil {
			t.Fatalf("decode frame body: %v", err)
		}
		switch f.Cmd {
		case cmdRespondMsg:
			if stream, _ := body["stream"].(map[string]any); stream != nil {
				streams = append(streams, stream)
			}
		case cmdSendMsg:
			sends = append(sends, body)
		}
	}
	return streams, sends
}

// TestABubbleAnswersInPlaceAgainstARealDatabase is the whole feature on real
// rows: the question opens a bubble, the origin gate and the round matcher
// read the seeded task, and the answer is ONE closing frame on the stream the
// opener chose — no aibot_send_msg anywhere.
//
// REVERSE VERIFICATION: make takeAtLocked never hand the bubble back (drop the
// `turn.Handle, turn.HasBubble = entry.handle, true` line) and this fails: the
// answer arrives as an aibot_send_msg under a bubble that is never sealed.
func TestABubbleAnswersInPlaceAgainstARealDatabase(t *testing.T) {
	pool := twoReplicaDB(t)
	turn := seedBoundTurn(t, pool)
	q := db.New(pool)
	r := newBubbleReplica(t, q, mustParseTaskUUID(t, turn.instID))

	r.asked(t, turn, "REQ-DB-1")
	r.bus.Publish(chatDoneFor(turn))

	streams, sends := r.wire(t)
	if len(streams) != 2 {
		t.Fatalf("the socket carried %d stream frames, want 2 (open + seal). log:\n%s", len(streams), r.logs.String())
	}
	if streams[0]["finish"] != false || streams[0]["content"] != streamThinkingPlaceholder {
		t.Fatalf("the opener is %v, want the thinking placeholder with finish=false", streams[0])
	}
	if streams[1]["id"] != streams[0]["id"] {
		t.Fatalf("the answer sealed stream %v, want the opener's %v — it opened a second bubble and the first spins forever",
			streams[1]["id"], streams[0]["id"])
	}
	if streams[1]["finish"] != true || streams[1]["content"] != "S270 的价格是 1280 元。" {
		t.Fatalf("the closing frame is %v, want finish=true carrying the answer", streams[1])
	}
	if len(sends) != 0 {
		t.Fatalf("the answer also went out as %d aibot_send_msg; the user reads it twice: %v", len(sends), sends)
	}
	if r.streams.depth() != 0 {
		t.Errorf("store holds %d rounds after the answer, want 0", r.streams.depth())
	}
}

// TestAnAnswerAfterARestartGoesOutAsAMessageAgainstARealDatabase is the
// bubble lost: a fresh store on the same database — the process restarted
// mid-run — has nothing for the round, and the answer takes the plain path,
// addressed by channel_task_delivery.
//
// REVERSE VERIFICATION: make sendAsMessage return errNothingToSay before the
// send (or make taskAddress report skipNoDeliveryRow for every row) and this
// fails with no aibot_send_msg at all.
func TestAnAnswerAfterARestartGoesOutAsAMessageAgainstARealDatabase(t *testing.T) {
	pool := twoReplicaDB(t)
	turn := seedBoundTurn(t, pool)
	q := db.New(pool)
	instID := mustParseTaskUUID(t, turn.instID)

	// The process that painted the bubble.
	before := newBubbleReplica(t, q, instID)
	before.asked(t, turn, "REQ-DB-2")
	if streams, _ := before.wire(t); len(streams) != 1 {
		t.Fatalf("the question painted %d stream frames, want 1", len(streams))
	}

	// The process that answers: same database, new store, nothing on file.
	after := newBubbleReplica(t, q, instID)
	after.bus.Publish(chatDoneFor(turn))

	streams, sends := after.wire(t)
	if len(streams) != 0 {
		t.Fatalf("the restarted process wrote %d stream frames for a bubble it never painted: %v", len(streams), streams)
	}
	if len(sends) != 1 {
		t.Fatalf("the restarted process sent %d aibot_send_msg, want 1. log:\n%s", len(sends), after.logs.String())
	}
	if sends[0]["chatid"] != turn.chatID {
		t.Errorf("the message went to chatid %v, want %s — the delivery row names the chat that asked", sends[0]["chatid"], turn.chatID)
	}
	md, _ := sends[0]["markdown"].(map[string]any)
	if md == nil || md["content"] != "S270 的价格是 1280 元。" {
		t.Errorf("the message carries %v, want the answer", sends[0]["markdown"])
	}
	// And the bubble the first process painted is left as it was: nothing
	// here could have sealed it, and nothing pretends to.
	if streams, _ := before.wire(t); len(streams) != 1 {
		t.Errorf("the first process's socket now carries %d stream frames, want the 1 it painted", len(streams))
	}
}
