package wecom

// outbound_two_replica_db_test.go — GH #7215 reproduced against a real
// database, with two replicas.
//
// The unit-level reproduction (outbound_cross_replica_test.go) drives a fake
// queries layer, which proves the subscriber's branching but not that a real
// deployment reaches those branches: every lookup ahead of the sender —
// the binding row, the task, the immutable channel_ingested stamp on the
// input batch, the installation's status — is answered by a mock there. If any
// of them behaved differently against real SQL, the "reply is dropped" verdict
// would be an artefact of the double.
//
// So this one keeps the fakes only where the WeCom platform would be, and runs
// everything else for real: *db.Queries against a migrated database, two
// Outbound subscribers on two independent event buses, each with its own
// sendersRegistry. That IS the production shape. events.Bus is in-process
// (internal/events/bus.go), and a chat completion is published by whichever
// replica served the daemon's POST /tasks/{id}/complete — a load-balancer
// decision, unrelated to which replica won that installation's WebSocket
// lease.
//
// Skips when no migrated database is reachable, same as the other _db_ tests
// in this package.

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// frameCount reads the recorded frames under the double's own lock, so these
// tests stay clean under -race even where a delivery runs on its own goroutine.
func (c *recordingConn) frameCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.frames)
}

func twoReplicaDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("no database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database not reachable: %v", err)
	}
	var present bool
	if err := pool.QueryRow(ctx,
		"SELECT to_regclass('public.channel_chat_session_binding') IS NOT NULL").Scan(&present); err != nil || !present {
		pool.Close()
		t.Skip("channel tables not present (database not migrated)")
	}
	t.Cleanup(pool.Close)
	return pool
}

// boundTurn is one WeCom conversation as the database holds it after a user
// asked a question in the room and the agent finished answering: an active
// installation, a session bound to a chat, and a task whose input batch
// carries the immutable channel_ingested stamp.
type boundTurn struct {
	sessionID string
	taskID    string
	instID    string
	chatID    string
}

func seedBoundTurn(t *testing.T, pool *pgxpool.Pool) boundTurn {
	t.Helper()
	ctx := context.Background()
	// Ids come from the database rather than a Go uuid package: this package's
	// tests already bind the identifier `uuid` for their own fixtures, and
	// gen_random_uuid() is already what every one of these tables defaults to.
	newID := func() string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
			t.Fatalf("seed: mint id: %v", err)
		}
		return id
	}
	tag := strings.ReplaceAll(newID(), "-", "")[:12]

	turn := boundTurn{
		sessionID: newID(), taskID: newID(), instID: newID(),
		chatID: "CHAT_" + tag,
	}
	wsID, userID, agentID, inputTaskID := newID(), newID(), newID(), newID()

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %s: %v", strings.SplitN(strings.TrimSpace(sql), "\n", 2)[0], err)
		}
	}
	// Reverse order of creation, so foreign keys hold on the way out.
	t.Cleanup(func() {
		for _, sql := range []string{
			`DELETE FROM chat_message WHERE chat_session_id = $1`,
			`DELETE FROM channel_chat_session_binding WHERE chat_session_id = $1`,
		} {
			_, _ = pool.Exec(ctx, sql, turn.sessionID)
		}
		_, _ = pool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = ANY($1)`,
			[]string{turn.taskID, inputTaskID})
		_, _ = pool.Exec(ctx, `DELETE FROM chat_session WHERE id = $1`, turn.sessionID)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_installation WHERE id = $1`, turn.instID)
		_, _ = pool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID)
		_, _ = pool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	exec(`INSERT INTO workspace (id, name, slug) VALUES ($1, $2, $3)`,
		wsID, "repro7215 "+tag, "repro7215-"+tag)
	exec(`INSERT INTO "user" (id, name, email) VALUES ($1, $2, $3)`,
		userID, "Repro "+tag, "repro-"+tag+"@example.com")
	exec(`INSERT INTO agent (id, workspace_id, name, runtime_mode) VALUES ($1, $2, $3, 'local')`,
		agentID, wsID, "repro-agent-"+tag)
	exec(`INSERT INTO chat_session (id, workspace_id, agent_id, creator_id) VALUES ($1, $2, $3, $4)`,
		turn.sessionID, wsID, agentID, userID)
	exec(`INSERT INTO channel_installation
	        (id, workspace_id, agent_id, channel_type, status, installer_user_id)
	      VALUES ($1, $2, $3, 'wecom', 'active', $4)`,
		turn.instID, wsID, agentID, userID)
	exec(`INSERT INTO channel_chat_session_binding
	        (chat_session_id, installation_id, channel_type, channel_chat_id, chat_type)
	      VALUES ($1, $2, 'wecom', $3, 'p2p')`,
		turn.sessionID, turn.instID, turn.chatID)

	// The turn the user's message belongs to, and the turn that answered it.
	// A reply task reaches the provenance verdict through chat_input_task_id,
	// which is what an auto-retry clone inherits — so the stamp is read off
	// the batch owner, not off the answering task.
	// completed_at is not decoration: agent_task_queue_active_requires_runtime
	// insists a row is either attached to a runtime or finished.
	exec(`INSERT INTO agent_task_queue (id, agent_id, chat_session_id, status, completed_at)
	      VALUES ($1, $2, $3, 'completed', now())`, inputTaskID, agentID, turn.sessionID)
	exec(`INSERT INTO agent_task_queue (id, agent_id, chat_session_id, status, completed_at, chat_input_task_id)
	      VALUES ($1, $2, $3, 'completed', now(), $4)`, turn.taskID, agentID, turn.sessionID, inputTaskID)
	exec(`INSERT INTO chat_message (chat_session_id, role, content, task_id, channel_ingested)
	      VALUES ($1, 'user', 'S270 的价格', $2, true)`, turn.sessionID, inputTaskID)

	return turn
}

// replica is one backend process: its own event bus, its own senders registry,
// its own subscriber. Everything below the adapter — the database — is shared,
// exactly as it is in production.
type replica struct {
	bus  *events.Bus
	conn *recordingConn // nil when this replica holds no socket
	logs *strings.Builder
	mx   *countingMetrics
}

func newReplica(t *testing.T, pool *pgxpool.Pool, instID string, holdsSocket bool) *replica {
	t.Helper()
	r := &replica{bus: events.New(), logs: &strings.Builder{}, mx: newCountingMetrics()}
	reg := newSendersRegistry()
	if holdsSocket {
		r.conn = &recordingConn{}
		var pg pgtype.UUID
		if err := pg.Scan(instID); err != nil {
			t.Fatalf("parse installation id: %v", err)
		}
		reg.set(pg, r.conn.autoAck(newWSSender(r.conn, nil)))
	}
	o := NewOutbound(db.New(pool), reg,
		slog.New(slog.NewTextHandler(r.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		WithOutboundMetrics(r.mx))
	o.Register(r.bus)
	return r
}

func chatDoneFor(turn boundTurn) events.Event {
	return events.Event{
		Type:          protocol.EventChatDone,
		WorkspaceID:   "",
		ActorType:     "system",
		ChatSessionID: turn.sessionID,
		Payload: protocol.ChatDonePayload{
			ChatSessionID: turn.sessionID,
			TaskID:        turn.taskID,
			Content:       "S270 的价格是 1280 元。",
		},
	}
}

func (r *replica) frames() int {
	if r.conn == nil {
		return 0
	}
	return r.conn.frameCount()
}

// TestTwoReplicas_ReplyPublishedOffLeaseIsDropped is the reproduction. Replica
// A holds the bot's socket; replica B served the daemon's completion callback
// and therefore publishes chat:done. Every database lookup on the way is real
// and every one of them succeeds — the turn is impeccable. The reply still
// never leaves, because the socket is in the other process.
func TestTwoReplicas_ReplyPublishedOffLeaseIsDropped(t *testing.T) {
	pool := twoReplicaDB(t)
	turn := seedBoundTurn(t, pool)

	leaseHolder := newReplica(t, pool, turn.instID, true)
	publisher := newReplica(t, pool, turn.instID, false)

	publisher.bus.Publish(chatDoneFor(turn))

	if n := leaseHolder.frames(); n != 0 {
		t.Fatalf("the lease holder's socket carried %d frames; the publishing replica cannot reach it", n)
	}
	if got := publisher.mx.get("outbound_dropped:no_live_connection"); got != 1 {
		t.Errorf("drop counter = %d, want 1. log:\n%s", got, publisher.logs.String())
	}
	if got := publisher.mx.get("outbound_delivered"); got != 0 {
		t.Errorf("publisher counted %d deliveries; it holds no socket", got)
	}
	// The lease holder never heard about this turn at all: the bus does not
	// cross processes, which is the whole mechanism.
	if got := leaseHolder.mx.get("outbound_delivered") + leaseHolder.mx.get("outbound_dropped"); got != 0 {
		t.Errorf("the lease holder saw %d outcomes for an event published elsewhere", got)
	}
}

// TestTwoReplicas_SameReplicaDelivers is the control. Identical rows, identical
// event, identical code. The only thing that changed is which of the two
// processes published it.
func TestTwoReplicas_SameReplicaDelivers(t *testing.T) {
	pool := twoReplicaDB(t)
	turn := seedBoundTurn(t, pool)

	leaseHolder := newReplica(t, pool, turn.instID, true)
	_ = newReplica(t, pool, turn.instID, false)

	leaseHolder.bus.Publish(chatDoneFor(turn))

	if n := leaseHolder.frames(); n != 1 {
		t.Fatalf("frames = %d, want 1. log:\n%s", n, leaseHolder.logs.String())
	}
	body := leaseHolder.conn.sendBody(t, 0)
	if body["chatid"] != turn.chatID {
		t.Errorf("chatid = %v, want %s", body["chatid"], turn.chatID)
	}
	if got := leaseHolder.mx.get("outbound_delivered"); got != 1 {
		t.Errorf("delivered counter = %d, want 1", got)
	}
	if got := leaseHolder.mx.get("outbound_dropped"); got != 0 {
		t.Errorf("dropped counter = %d, want 0", got)
	}
}

// fanoutRelay is the Redis Stream relay reduced to what it guarantees: every
// node's XREAD loop sees every frame published under a scope. Registered
// deliverers stand in for the other replicas' consumer loops.
//
// It is synchronous, which the real relay is not — but the property under test
// is routing, not latency, and a fake that delivers on the caller's goroutine
// keeps the test deterministic.
type fanoutRelay struct {
	mu         sync.Mutex
	deliverers []*RelayOutbound
	published  int
}

func (f *fanoutRelay) register(r *RelayOutbound) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deliverers = append(f.deliverers, r)
}

func (f *fanoutRelay) PublishWithID(_, scopeID, _ string, frame []byte, id string) error {
	f.mu.Lock()
	f.published++
	targets := append([]*RelayOutbound(nil), f.deliverers...)
	f.mu.Unlock()
	for _, d := range targets {
		d.DeliverWecomOutbound(scopeID, frame, id)
	}
	return nil
}

// newRelayedReplica is newReplica with the cross-replica router attached, and
// its consumer side registered on the shared relay.
func newRelayedReplica(t *testing.T, pool *pgxpool.Pool, instID string, holdsSocket bool, relay *fanoutRelay) *replica {
	t.Helper()
	r := &replica{bus: events.New(), logs: &strings.Builder{}, mx: newCountingMetrics()}
	reg := newSendersRegistry()
	if holdsSocket {
		r.conn = &recordingConn{}
		var pg pgtype.UUID
		if err := pg.Scan(instID); err != nil {
			t.Fatalf("parse installation id: %v", err)
		}
		reg.set(pg, r.conn.autoAck(newWSSender(r.conn, nil)))
	}
	log := slog.New(slog.NewTextHandler(r.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	router := NewRelayOutbound(reg, relay, log, r.mx)
	relay.register(router)
	o := NewOutbound(db.New(pool), reg, log, WithOutboundMetrics(r.mx), WithRelay(router))
	o.Register(r.bus)
	return r
}

// TestTwoReplicas_RelayCarriesTheReplyToTheLeaseHolder is the fix for the
// reproduction above. Same two replicas, same rows, same event on the same
// (wrong) replica — and the answer now arrives, because the replica that
// cannot send hands it to the one that can.
func TestTwoReplicas_RelayCarriesTheReplyToTheLeaseHolder(t *testing.T) {
	pool := twoReplicaDB(t)
	turn := seedBoundTurn(t, pool)
	relay := &fanoutRelay{}

	leaseHolder := newRelayedReplica(t, pool, turn.instID, true, relay)
	publisher := newRelayedReplica(t, pool, turn.instID, false, relay)

	publisher.bus.Publish(chatDoneFor(turn))

	if n := leaseHolder.frames(); n != 1 {
		t.Fatalf("the lease holder's socket carried %d frames, want 1.\npublisher log:\n%s\nholder log:\n%s",
			n, publisher.logs.String(), leaseHolder.logs.String())
	}
	body := leaseHolder.conn.sendBody(t, 0)
	if body["chatid"] != turn.chatID {
		t.Errorf("chatid = %v, want %s", body["chatid"], turn.chatID)
	}
	md, _ := body["markdown"].(map[string]any)
	if got, _ := md["content"].(string); got != "S270 的价格是 1280 元。" {
		t.Errorf("content = %q", got)
	}
	// Counted once, on the replica that actually sent it.
	if got := leaseHolder.mx.get("outbound_delivered"); got != 1 {
		t.Errorf("lease holder delivered counter = %d, want 1", got)
	}
	if got := publisher.mx.get("outbound_delivered"); got != 0 {
		t.Errorf("the publishing replica counted %d deliveries; it sent nothing", got)
	}
	// And no longer counted as a drop, which is the whole point.
	if got := publisher.mx.get("outbound_dropped"); got != 0 {
		t.Errorf("publisher still recorded %d drops", got)
	}
}

// TestTwoReplicas_RelayDeliversExactlyOnce — every replica reads every frame,
// so "exactly one sends it" has to come from somewhere. It comes from the
// lease: only one registry can answer with a sender. The publisher's own copy
// of its own frame must not turn into a second message either.
func TestTwoReplicas_RelayDeliversExactlyOnce(t *testing.T) {
	pool := twoReplicaDB(t)
	turn := seedBoundTurn(t, pool)
	relay := &fanoutRelay{}

	leaseHolder := newRelayedReplica(t, pool, turn.instID, true, relay)
	publisher := newRelayedReplica(t, pool, turn.instID, false, relay)
	// A third replica, also holding nothing. It reads the frame too.
	_ = newRelayedReplica(t, pool, turn.instID, false, relay)

	publisher.bus.Publish(chatDoneFor(turn))

	if relay.published != 1 {
		t.Fatalf("published %d frames, want 1", relay.published)
	}
	if n := leaseHolder.frames(); n != 1 {
		t.Fatalf("frames on the wire = %d, want exactly 1", n)
	}
}

// TestTwoReplicas_NoRelayStillDrops pins that the router is opt-in: a
// deployment with no Redis gets exactly the old behaviour, and the old
// reproduction above keeps meaning what it says.
func TestTwoReplicas_NoRelayStillDrops(t *testing.T) {
	pool := twoReplicaDB(t)
	turn := seedBoundTurn(t, pool)

	leaseHolder := newReplica(t, pool, turn.instID, true)
	publisher := newReplica(t, pool, turn.instID, false)

	publisher.bus.Publish(chatDoneFor(turn))

	if n := leaseHolder.frames(); n != 0 {
		t.Fatalf("frames = %d, want 0 without a relay", n)
	}
	if got := publisher.mx.get("outbound_dropped:no_live_connection"); got != 1 {
		t.Errorf("drop counter = %d, want 1", got)
	}
}
