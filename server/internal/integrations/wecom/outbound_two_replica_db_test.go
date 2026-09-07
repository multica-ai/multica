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
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
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
		_, _ = pool.Exec(ctx, `DELETE FROM channel_task_delivery WHERE task_id = $1`, turn.taskID)
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
	bindingID := newID()
	exec(`INSERT INTO channel_chat_session_binding
	        (id, chat_session_id, installation_id, channel_type, channel_chat_id, chat_type)
	      VALUES ($1, $2, $3, 'wecom', $4, 'p2p')`,
		bindingID, turn.sessionID, turn.instID, turn.chatID)

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
	// Since MUL-6661 (#7468) the outbound address is a per-task delivery
	// snapshot, not the session binding: processEvent resolves the route via
	// GetChannelTaskDelivery(task_id) and returns silently without a row. The
	// engine writes this row when it ingests the message; a seed that skips it
	// is a turn the outbound path correctly ignores.
	exec(`INSERT INTO channel_task_delivery
	        (task_id, binding_id, installation_id, channel_type, channel_chat_id, chat_type, route_revision, config)
	      VALUES ($1, $2, $3, 'wecom', $4, 'p2p', 1, '{}')`,
		turn.taskID, bindingID, turn.instID, turn.chatID)
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

// fanoutRelay is the Redis Stream relay reduced to the two properties this
// depends on: every node sees every frame published under a scope, and a node
// that starts late replays what it missed. Registered deliverers stand in for
// the other replicas' shard read loops.
type fanoutRelay struct {
	mu         sync.Mutex
	deliverers []*RelayOutbound
	log        []queued
	published  int
}

func (f *fanoutRelay) register(r *RelayOutbound) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deliverers = append(f.deliverers, r)
}

func (f *fanoutRelay) PublishWithID(_, scopeID, _ string, frame []byte, id string) error {
	var decoded relayFrame
	if err := json.Unmarshal(frame, &decoded); err != nil {
		return err
	}
	f.mu.Lock()
	f.published++
	f.log = append(f.log, queued{frame: decoded, eventID: id})
	targets := append([]*RelayOutbound(nil), f.deliverers...)
	f.mu.Unlock()
	for _, d := range targets {
		d.DeliverWecomOutbound(scopeID, frame, id)
	}
	return nil
}

// replayTo re-delivers everything published so far to one node, which is what
// a shard reader does on startup: it begins at (now - ReplayGrace), not "$".
func (f *fanoutRelay) replayTo(r *RelayOutbound) {
	f.mu.Lock()
	entries := append([]queued(nil), f.log...)
	f.mu.Unlock()
	for _, e := range entries {
		body, _ := json.Marshal(e.frame)
		r.DeliverWecomOutbound(e.frame.InstallationID, body, e.eventID)
	}
}

// sharedDedupe stands in for the Redis claim: one map, shared by every replica
// in the test, surviving a "restart" because the process it models does not
// own it. That is the whole property the in-process seen-set lacked.
type sharedDedupe struct {
	mu    sync.Mutex
	held  map[string]bool
	fail  bool
	calls int
}

func newSharedDedupe() *sharedDedupe { return &sharedDedupe{held: map[string]bool{}} }

func (d *sharedDedupe) Claim(_ context.Context, key string, _ time.Duration) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	if d.fail {
		return false, errors.New("dedupe unavailable")
	}
	if d.held[key] {
		return false, nil
	}
	d.held[key] = true
	return true, nil
}

func (d *sharedDedupe) holds(key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.held[key]
}

func (d *sharedDedupe) claimCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

func (d *sharedDedupe) Release(_ context.Context, key string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.held, key)
}

// ClaimBudget is a fake's budget: small, because it is what sizes the outcome
// grace these tests wait out and nothing here talks to a real server.
func (d *sharedDedupe) ClaimBudget() time.Duration { return 20 * time.Millisecond }

func (d *sharedDedupe) Held(_ context.Context, key string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.fail {
		return false, errors.New("dedupe unavailable")
	}
	return d.held[key], nil
}

// relayReplica is one backend process wired for cross-replica routing.
type relayReplica struct {
	*replica
	router *RelayOutbound
	reg    *sendersRegistry
	instID pgtype.UUID
}

func newRelayReplica(t *testing.T, pool *pgxpool.Pool, instID string, holdsSocket bool, relay *fanoutRelay, dedupe DedupeStore) *relayReplica {
	return newRelayReplicaWith(t, pool, instID, holdsSocket, relay, dedupe, RelayConfig{})
}

// newRelayReplicaWith is the same, with the dispatcher sized by the caller.
func newRelayReplicaWith(t *testing.T, pool *pgxpool.Pool, instID string, holdsSocket bool, relay *fanoutRelay, dedupe DedupeStore, cfg RelayConfig) *relayReplica {
	t.Helper()
	base := &replica{bus: events.New(), logs: &strings.Builder{}, mx: newCountingMetrics()}
	reg := newSendersRegistry()
	var pg pgtype.UUID
	if err := pg.Scan(instID); err != nil {
		t.Fatalf("parse installation id: %v", err)
	}
	if holdsSocket {
		base.conn = &recordingConn{}
		reg.set(pg, base.conn.autoAck(newWSSender(base.conn, nil)))
	}
	log := slog.New(slog.NewTextHandler(base.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Registered and started BEFORE the subscriber exists, which is the real
	// boot order: the relay's readers open on a replay window and anything
	// registered after them misses it.
	router := NewRelayOutbound(relay, dedupe, cfg, log)
	router.SetMetrics(base.mx)
	relay.register(router)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); router.Wait() })
	router.Start(ctx)

	o := NewOutbound(db.New(pool), reg, log, WithOutboundMetrics(base.mx), WithRelay(router))
	o.Register(base.bus)
	router.Attach(o)
	return &relayReplica{replica: base, router: router, reg: reg, instID: pg}
}

// TestTwoReplicas_RelayCarriesTheReplyToTheLeaseHolder is the fix. Same two
// replicas and the same event on the same (wrong) one — and the answer arrives.
func TestTwoReplicas_RelayCarriesTheReplyToTheLeaseHolder(t *testing.T) {
	pool := twoReplicaDB(t)
	turn := seedBoundTurn(t, pool)
	relay, dedupe := &fanoutRelay{}, newSharedDedupe()

	holder := newRelayReplica(t, pool, turn.instID, true, relay, dedupe)
	publisher := newRelayReplica(t, pool, turn.instID, false, relay, dedupe)

	publisher.bus.Publish(chatDoneFor(turn))

	waitFor(t, "the lease holder to deliver", func() bool { return holder.frames() == 1 })
	body := holder.conn.sendBody(t, 0)
	if body["chatid"] != turn.chatID {
		t.Errorf("chatid = %v, want %s", body["chatid"], turn.chatID)
	}
	waitFor(t, "the delivery to be counted once", func() bool {
		return holder.mx.get("outbound_delivered") == 1
	})
	if got := publisher.mx.get("outbound_dropped"); got != 0 {
		t.Errorf("publisher recorded %d drops for a reply it routed", got)
	}
}

// TestRelay_StartupReplayDoesNotResend — the review finding. A shard reader
// starts at (now - ReplayGrace), so a restarted replica re-reads frames it may
// already have delivered. The claim has to outlive the process for that to be
// safe; an in-process set is empty exactly when it matters.
func TestRelay_StartupReplayDoesNotResend(t *testing.T) {
	pool := twoReplicaDB(t)
	turn := seedBoundTurn(t, pool)
	relay, dedupe := &fanoutRelay{}, newSharedDedupe()

	holder := newRelayReplica(t, pool, turn.instID, true, relay, dedupe)
	publisher := newRelayReplica(t, pool, turn.instID, false, relay, dedupe)
	publisher.bus.Publish(chatDoneFor(turn))
	waitFor(t, "the first delivery", func() bool { return holder.frames() == 1 })

	// The lease holder restarts: a brand new process, an empty in-process set,
	// the same socket, and the relay replays its window into it.
	restarted := newRelayReplica(t, pool, turn.instID, true, relay, dedupe)
	relay.replayTo(restarted.router)

	// Two claim attempts, not three: the publisher holds no socket and — since
	// the ownership gate — never competes for the claim at all. The two are
	// the original holder's winning claim and the restarted replica's losing
	// one against the replayed frame.
	waitFor(t, "the replay to be consumed", func() bool { return dedupe.claimCount() >= 2 })
	if n := restarted.frames(); n != 0 {
		t.Fatalf("the restarted replica re-sent %d already-delivered replies", n)
	}
}

// TestRelay_LeaseMoveDoesNotDoubleSend — two replicas both holding a socket for
// the same installation is the window a lease transfer opens. Both read the
// frame; exactly one may send it.
func TestRelay_LeaseMoveDoesNotDoubleSend(t *testing.T) {
	pool := twoReplicaDB(t)
	turn := seedBoundTurn(t, pool)
	relay, dedupe := &fanoutRelay{}, newSharedDedupe()

	a := newRelayReplica(t, pool, turn.instID, true, relay, dedupe)
	b := newRelayReplica(t, pool, turn.instID, true, relay, dedupe)
	publisher := newRelayReplica(t, pool, turn.instID, false, relay, dedupe)

	publisher.bus.Publish(chatDoneFor(turn))

	waitFor(t, "exactly one of them to send", func() bool { return a.frames()+b.frames() == 1 })
	time.Sleep(50 * time.Millisecond) // give the loser a chance to be wrong
	if got := a.frames() + b.frames(); got != 1 {
		t.Fatalf("frames across both lease holders = %d, want exactly 1", got)
	}
}

// TestRelay_DispatchNeverBlocksTheShardReader — DeliverWecomOutbound runs on
// the shared realtime read loop. If it waited for the platform, one unhealthy
// bot would hold up browser traffic, daemon wakeups and every other bot on the
// shard. The double here never answers, so a synchronous implementation would
// sit in ackTimeout.
func TestRelay_DispatchNeverBlocksTheShardReader(t *testing.T) {
	t.Parallel()
	silent := &recordingConn{} // never acks
	reg := newSendersRegistry()
	instID := mustTestUUID(t)
	reg.set(instID, newWSSender(silent, nil))

	router := NewRelayOutbound(nil, nil, RelayConfig{}, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); router.Wait() }()
	router.Start(ctx)
	router.Attach(&Outbound{senders: reg, logger: slog.Default()})

	body, _ := json.Marshal(relayFrame{
		Kind: relayKindReply, InstallationID: util.UUIDToString(instID),
		ChatID: "CHAT_1", ChatType: chatTypeSingleInt, Content: "hello",
	})
	start := time.Now()
	router.DeliverWecomOutbound(util.UUIDToString(instID), body, "ev-1")
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("DeliverWecomOutbound blocked the shard reader for %s", elapsed)
	}
}

// TestRelay_ShedsRatherThanStalls — when a bot's queue is full the frame is
// dropped and counted, because the alternative is stalling the shard.
//
// Asserted at the BOUNDARY rather than approximately: QueueDepth frames are
// accepted and the next one is not, so the configured depth is the depth.
func TestRelay_ShedsRatherThanStalls(t *testing.T) {
	t.Parallel()
	mx := newCountingMetrics()
	const depth = 4
	router := NewRelayOutbound(nil, nil, RelayConfig{QueueDepth: depth}, slog.Default())
	router.SetMetrics(mx)
	// Attached but never started: nothing drains, so the queue fills.
	router.Attach(&ownsSocketHandler{owns: true})
	body, _ := json.Marshal(relayFrame{Kind: relayKindReply, InstallationID: "inst-1"})
	for i := 0; i < depth; i++ {
		router.DeliverWecomOutbound("inst-1", body, "ev")
	}
	if got := mx.get("relay_shed"); got != 0 {
		t.Fatalf("shed %d frames while the queue still had room for them", got)
	}
	router.DeliverWecomOutbound("inst-1", body, "ev")
	if got := mx.get("relay_shed:reply"); got != 1 {
		t.Fatalf("relay_shed:reply = %d, want 1 — the frame past the depth must be shed and counted", got)
	}
	// The admission decision is ALL it records. Whether that shed cost the user
	// the reply is settled once, later, by the publisher's outcome watch — no
	// replica can tell from here. See TestRelayShed_NeverMovesTheReplyCounter.
	if got := mx.get("outbound_dropped"); got != 0 {
		t.Fatalf("outbound_dropped = %d, want 0 — a shed is an admission decision, not a reply outcome", got)
	}
}

// The same queue, an inbox notification instead of a reply. It must be shed the
// same way and counted in a DIFFERENT place: outbound_dropped counts agent
// replies, and an inbox push filed there makes the delivered/dropped ratio
// track which replica held a socket rather than what happened to anyone.
func TestRelay_AnInboxPushShedDoesNotMoveTheReplyCounters(t *testing.T) {
	t.Parallel()
	mx := newCountingMetrics()
	const depth = 2
	router := NewRelayOutbound(nil, nil, RelayConfig{QueueDepth: depth}, slog.Default())
	router.SetMetrics(mx)
	router.Attach(&ownsSocketHandler{owns: true})
	body, _ := json.Marshal(relayFrame{Kind: relayKindInbox, InstallationID: "inst-1"})
	for i := 0; i < depth+3; i++ {
		router.DeliverWecomOutbound("inst-1", body, "ev")
	}
	if got := mx.get("outbound_dropped"); got != 0 {
		t.Fatalf("outbound_dropped = %d, want 0 — an inbox push is not an agent reply", got)
	}
	if got := mx.get("relay_shed:inbox"); got != 3 {
		t.Fatalf("relay_shed:inbox = %d, want 3 — the sheds must still be visible somewhere", got)
	}
}

// TestRelay_DedupeOutageWithholdsRatherThanDuplicates — a claim store that
// cannot answer must not silently become an at-least-once path. The frame stays
// replayable, so withholding costs latency; delivering costs a duplicate
// answer in somebody's room.
func TestRelay_DedupeOutageWithholdsRatherThanDuplicates(t *testing.T) {
	pool := twoReplicaDB(t)
	turn := seedBoundTurn(t, pool)
	relay, dedupe := &fanoutRelay{}, newSharedDedupe()
	dedupe.fail = true

	holder := newRelayReplica(t, pool, turn.instID, true, relay, dedupe)
	publisher := newRelayReplica(t, pool, turn.instID, false, relay, dedupe)

	publisher.bus.Publish(chatDoneFor(turn))

	waitFor(t, "the claim attempt", func() bool { return dedupe.claimCount() > 0 })
	time.Sleep(50 * time.Millisecond)
	if n := holder.frames(); n != 0 {
		t.Fatalf("delivered %d frames with no working claim store", n)
	}
}

// TestTwoReplicas_NoRelayStillDrops pins that the router is opt-in: a
// deployment with no Redis gets exactly the old behaviour.
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

// ---- the second review round's three orderings ---------------------------

// TestRelay_NonHolderNeverCompetesForTheClaim — the stranding found in review.
// The claim is a cross-replica SET NX: if a replica without the socket were
// allowed to win it, the real holder would lose the race and return, the
// winner would discover it cannot send and release, and nothing would ever
// wake the loser again — every party correct, the reply lost. The gate is
// therefore ownership BEFORE the claim: a non-holder must never touch it.
func TestRelay_NonHolderNeverCompetesForTheClaim(t *testing.T) {
	t.Parallel()
	relay, dedupe := &fanoutRelay{}, newSharedDedupe()

	// Only a non-holder is registered: empty registry.
	reg := newSendersRegistry()
	router := NewRelayOutbound(relay, dedupe, RelayConfig{}, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); router.Wait() })
	router.Start(ctx)
	router.Attach(&Outbound{senders: reg, logger: slog.Default()})
	relay.register(router)

	instID := mustTestUUID(t)
	body, _ := json.Marshal(relayFrame{
		Kind: relayKindReply, InstallationID: util.UUIDToString(instID),
		ChatID: "CHAT_1", ChatType: chatTypeSingleInt, Content: "hello",
	})
	router.DeliverWecomOutbound(util.UUIDToString(instID), body, "ev-strand")

	time.Sleep(80 * time.Millisecond)
	if got := dedupe.claimCount(); got != 0 {
		t.Fatalf("a replica with no socket made %d claim attempts; the real holder would have been stranded", got)
	}

	// The holder arrives later — a replay hands it the same frame, and because
	// the claim was never consumed, it delivers.
	conn := &recordingConn{}
	holderReg := newSendersRegistry()
	holderReg.set(instID, conn.autoAck(newWSSender(conn, nil)))
	holderRouter := NewRelayOutbound(relay, dedupe, RelayConfig{}, slog.Default())
	hctx, hcancel := context.WithCancel(context.Background())
	t.Cleanup(func() { hcancel(); holderRouter.Wait() })
	holderRouter.Start(hctx)
	holderRouter.Attach(&Outbound{senders: holderReg, logger: slog.Default()})
	holderRouter.DeliverWecomOutbound(util.UUIDToString(instID), body, "ev-strand")

	waitFor(t, "the late holder to deliver", func() bool { return conn.frameCount() == 1 })
}

// writeFailConn fails inside WriteMessage — the ambiguous point: the frame may
// have reached the peer before the local side surfaced the error, which is
// exactly what ws_sender's errWriteAttempted marks.
type writeFailConn struct{}

func (writeFailConn) ReadMessage() (int, []byte, error) { select {} }
func (writeFailConn) WriteMessage(int, []byte) error    { return errTestWriteBoom }
func (writeFailConn) SetReadDeadline(time.Time) error   { return nil }
func (writeFailConn) SetWriteDeadline(time.Time) error  { return nil }
func (writeFailConn) Close() error                      { return nil }

var errTestWriteBoom = errors.New("boom mid-write")

// TestRelay_AttemptedWriteKeepsTheClaim — the second finding. A failure raised
// by the write itself is not proof of non-delivery; releasing the claim there
// lets the replay put the same answer in the chat twice. The claim must stay.
func TestRelay_AttemptedWriteKeepsTheClaim(t *testing.T) {
	t.Parallel()
	relay, dedupe := &fanoutRelay{}, newSharedDedupe()

	instID := mustTestUUID(t)
	reg := newSendersRegistry()
	reg.set(instID, newWSSender(writeFailConn{}, nil))
	router := NewRelayOutbound(relay, dedupe, RelayConfig{}, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); router.Wait() })
	router.Start(ctx)
	mx := newCountingMetrics()
	router.Attach(NewOutbound(nil, reg, slog.Default(), WithOutboundMetrics(mx)))

	body, _ := json.Marshal(relayFrame{
		Kind: relayKindReply, InstallationID: util.UUIDToString(instID),
		ChatID: "CHAT_1", ChatType: chatTypeSingleInt, Content: "hello",
	})
	router.DeliverWecomOutbound(util.UUIDToString(instID), body, "ev-attempted")

	// An attempted write is an UNKNOWN outcome, so it files under unconfirmed
	// — never under dropped, whose contract promises the answer is not coming.
	waitFor(t, "the unconfirmed outcome to be recorded", func() bool { return mx.get("outbound_unconfirmed") == 1 })
	if got := mx.get("outbound_dropped"); got != 0 {
		t.Errorf("dropped = %d, want 0 — the peer may already have the frame", got)
	}
	if !dedupe.holds(dedupeKey("ev-attempted")) {
		t.Fatal("the claim was released after an ATTEMPTED write; a retry would resend a frame the peer may already have")
	}
}

// TestDeliverRelayed_ContextErrorsDoNotRelease — a bare context error is
// ambiguous: request() returns one both before the write and while waiting for
// the verdict after it. Treated as possibly-sent, so the outcome must not be
// the claim-releasing one.
func TestDeliverRelayed_ContextErrorsDoNotRelease(t *testing.T) {
	t.Parallel()
	instID := mustTestUUID(t)
	reg := newSendersRegistry()
	conn := &recordingConn{}
	reg.set(instID, conn.autoAck(newWSSender(conn, nil)))
	o := NewOutbound(nil, reg, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already expired: sendTextCtx fails on its pre-write check
	got := o.deliverRelayed(ctx, relayFrame{
		Kind: relayKindReply, InstallationID: util.UUIDToString(instID),
		ChatID: "CHAT_1", ChatType: chatTypeSingleInt, Content: "hello",
	})
	if got == outcomeProvablyNotSent {
		t.Fatal("a context error released the claim; it is ambiguous and must not")
	}
}

// TestProvablyNotSent_Classification pins the boundary the claim release
// rides on.
func TestProvablyNotSent_Classification(t *testing.T) {
	t.Parallel()
	if provablyNotSent(&wecomAPIError{Cmd: cmdSendMsg, Code: 45002}) {
		t.Error("a stated refusal was answered — the frame was seen")
	}
	if provablyNotSent(errAckTimeout) {
		t.Error("a missing verdict may still be a delivery")
	}
	if provablyNotSent(fmt.Errorf("%w: %w", errWriteAttempted, errTestWriteBoom)) {
		t.Error("an attempted write may have reached the peer")
	}
	if provablyNotSent(context.Canceled) || provablyNotSent(context.DeadlineExceeded) {
		t.Error("a bare context error is ambiguous")
	}
	if !provablyNotSent(errors.New("wecom: send_msg requires chat_id")) {
		t.Error("a pre-write local failure is the one case that IS provably unsent")
	}
}

// ---- round-3 review regressions (Bohan) ----------------------------------

// TestRelay_LeaseMoveMidFlightIsRescuedByRetry — the stranding: A passes the
// ownership check, the lease moves to B, A wins the claim, B loses it and
// returns, A discovers it no longer holds the socket and releases. Without a
// local retry nobody ever comes back — each replica reads a stream frame
// exactly once. The bounded requeue is what rescues it: B's losing attempt
// retries, finds the claim free, and delivers.
func TestRelay_LeaseMoveMidFlightIsRescuedByRetry(t *testing.T) {
	t.Parallel()
	relay, dedupe := &fanoutRelay{}, newSharedDedupe()
	instID := mustTestUUID(t)

	// Replica A: passes ownsSocket, then finds the sender gone at delivery —
	// a handler whose ownership answer and whose delivery disagree, which is
	// exactly what a lease moving between the two moments looks like.
	aRouter := NewRelayOutbound(relay, dedupe, RelayConfig{}, slog.Default())
	actx, acancel := context.WithCancel(context.Background())
	t.Cleanup(func() { acancel(); aRouter.Wait() })
	aRouter.Start(actx)
	aRouter.Attach(&flipHandler{})

	// Replica B: genuinely holds the socket throughout.
	conn := &recordingConn{}
	bReg := newSendersRegistry()
	bReg.set(instID, conn.autoAck(newWSSender(conn, nil)))
	bRouter := NewRelayOutbound(relay, dedupe, RelayConfig{}, slog.Default())
	bctx, bcancel := context.WithCancel(context.Background())
	t.Cleanup(func() { bcancel(); bRouter.Wait() })
	bRouter.Start(bctx)
	bRouter.Attach(&Outbound{senders: bReg, logger: slog.Default()})

	relay.register(aRouter)
	relay.register(bRouter)

	body, _ := json.Marshal(relayFrame{
		Kind: relayKindReply, InstallationID: util.UUIDToString(instID),
		ChatID: "CHAT_1", ChatType: chatTypeSingleInt, Content: "hello",
	})
	// Both replicas read the frame, as every replica does.
	aRouter.DeliverWecomOutbound(util.UUIDToString(instID), body, "ev-move")
	bRouter.DeliverWecomOutbound(util.UUIDToString(instID), body, "ev-move")

	// Whichever ordering the race takes, the retry must land the reply.
	waitLong(t, "the reply to be rescued", func() bool { return conn.frameCount() == 1 })
	time.Sleep(100 * time.Millisecond)
	if got := conn.frameCount(); got != 1 {
		t.Fatalf("frames = %d, want exactly 1", got)
	}
}

// flipHandler owns the socket when asked and does not when delivering — the
// deterministic shape of a lease that moved between the check and the send.
type flipHandler struct{}

func (*flipHandler) ownsSocket(string) bool { return true }
func (*flipHandler) deliverRelayed(context.Context, relayFrame) deliveryOutcome {
	return outcomeNotOurs
}

// waitLong is waitFor with room for the retry backoff schedule.
func waitLong(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestDedupeTTL_CoversTheReplayWindow — the invariant the TTL exists for: a
// claim must outlive the window in which its frame can be replayed, and that
// window is an operator knob, not a constant.
func TestDedupeTTL_CoversTheReplayWindow(t *testing.T) {
	t.Parallel()
	if got := dedupeTTLFor(0); got != minDedupeTTL {
		t.Errorf("zero grace: ttl = %v, want the floor %v", got, minDedupeTTL)
	}
	if got := dedupeTTLFor(5 * time.Minute); got != minDedupeTTL {
		t.Errorf("default grace: ttl = %v, want the floor %v", got, minDedupeTTL)
	}
	if got := dedupeTTLFor(2 * time.Hour); got != 4*time.Hour {
		t.Errorf("2h grace: ttl = %v, want 4h — the claim must outlive the replay window", got)
	}
	if got := dedupeTTLFor(2 * time.Hour); got <= 2*time.Hour {
		t.Errorf("ttl %v does not cover a 2h replay window", got)
	}
}

// TestRelayedInboxPushDoesNotMoveTheReplyCounters — an inbox push is not an
// agent reply; the delivered/dropped ratio must not track socket placement.
func TestRelayedInboxPushDoesNotMoveTheReplyCounters(t *testing.T) {
	t.Parallel()
	instID := mustTestUUID(t)

	// Success path.
	okConn := &recordingConn{}
	okReg := newSendersRegistry()
	okReg.set(instID, okConn.autoAck(newWSSender(okConn, nil)))
	okMx := newCountingMetrics()
	ok := NewOutbound(nil, okReg, slog.Default(), WithOutboundMetrics(okMx))
	if got := ok.deliverRelayed(context.Background(), relayFrame{
		Kind: relayKindInbox, InstallationID: util.UUIDToString(instID),
		ChatID: "T-USER", ChatType: chatTypeSingleInt, Content: "you were mentioned",
	}); got != outcomeDone {
		t.Fatalf("outcome = %v, want done", got)
	}
	if n := okConn.frameCount(); n != 1 {
		t.Fatalf("frames = %d, want 1 — the push itself must go out", n)
	}
	if got := okMx.get("outbound_delivered"); got != 0 {
		t.Errorf("delivered = %d, want 0 — an inbox push is not a reply", got)
	}

	// Failure path.
	badConn := &recordingConn{refuseCode: 45002, refuseMsg: "too long"}
	badReg := newSendersRegistry()
	badReg.set(instID, badConn.autoAck(newWSSender(badConn, nil)))
	badMx := newCountingMetrics()
	bad := NewOutbound(nil, badReg, slog.Default(), WithOutboundMetrics(badMx))
	bad.deliverRelayed(context.Background(), relayFrame{
		Kind: relayKindInbox, InstallationID: util.UUIDToString(instID),
		ChatID: "T-USER", ChatType: chatTypeSingleInt, Content: "you were mentioned",
	})
	if got := badMx.get("outbound_dropped") + badMx.get("outbound_unconfirmed"); got != 0 {
		t.Errorf("reply counters moved (%d) for a failed inbox push", got)
	}
}

// TestDrainDeliversOnlyWhileSocketsLive — why main stops the relay BEFORE the
// channel supervisor: each supervised connection clears its sender on exit, so
// a drain that runs after that teardown finds no socket and discards
// everything it exists to save. Component-level: the same queued frame drains
// to the wire with the socket registered and to nothing with it cleared. The
// production ordering itself lives in main.go's stopRelay call site.
func TestDrainDeliversOnlyWhileSocketsLive(t *testing.T) {
	t.Parallel()
	for _, live := range []bool{true, false} {
		instID := mustTestUUID(t)
		conn := &recordingConn{}
		reg := newSendersRegistry()
		sender := conn.autoAck(newWSSender(conn, nil))
		reg.set(instID, sender)
		router := NewRelayOutbound(nil, nil, RelayConfig{}, slog.Default())
		ctx, cancel := context.WithCancel(context.Background())
		router.Attach(&Outbound{senders: reg, logger: slog.Default()})

		body, _ := json.Marshal(relayFrame{
			Kind: relayKindReply, InstallationID: util.UUIDToString(instID),
			ChatID: "CHAT_1", ChatType: chatTypeSingleInt, Content: "queued before shutdown",
		})
		router.DeliverWecomOutbound(util.UUIDToString(instID), body, "ev-drain")
		if !live {
			reg.clear(instID, sender) // what supervisor teardown does
		}
		// Workers start already cancelled: everything runs as drain.
		cancel()
		router.Start(ctx)
		router.Wait()

		want := 0
		if live {
			want = 1
		}
		if got := conn.frameCount(); got != want {
			t.Fatalf("live=%v: frames = %d, want %d", live, got, want)
		}
	}
}
