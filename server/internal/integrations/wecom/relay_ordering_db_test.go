package wecom

// relay_ordering_db_test.go — the four things the cross-replica dispatcher
// promises beyond "the reply arrives", each against the real thing it depends
// on: a migrated database for the turn, and a real Redis for the claim.
//
//  1. Two answers to the same person arrive in the order they were produced,
//     INCLUDING when the first one has to be offered again.
//  2. A re-offer belongs to the worker, so nothing can enqueue after shutdown.
//  3. A reply nobody delivered is counted once, by one owner.
//  4. The sizing constants hold to the operational bounds they claim.
//
// The claim store here is redisDedupe against a real server, not a map: the
// property under test in (1) and (3) is what SET NX / EXISTS actually do
// across processes, and a map cannot be wrong about that in the same ways.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// wecomTestRedis is the same gate the rest of the repository uses for
// Redis-backed tests: REDIS_TEST_URL, a dedicated DB index, flushed around
// each test so one run cannot see another's claims.
//
// The index has to be unique across PACKAGES, not just within this one. `go
// test ./...` runs packages concurrently, every Redis-backed suite flushes its
// own DB on entry and exit, and a flush is indiscriminate: sharing an index
// means another package can delete a live delivery claim mid-test, and the
// outcome watch then reports a reply that was delivered as lost. Current
// allocation — 11 internal/auth, 12 internal/service, 13 internal/middleware,
// 14 internal/handler, 15 here.
const wecomRelayTestRedisDB = 15

// testClaimBudget is the claim round trip these tests give the real store. It
// sizes outcomeGrace (once per offer), so the production 2s would make the
// outcome-watch tests below sleep for a quarter minute apiece.
const testClaimBudget = 20 * time.Millisecond

func wecomTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	url := os.Getenv("REDIS_TEST_URL")
	if url == "" {
		t.Skip("REDIS_TEST_URL not set")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("parse REDIS_TEST_URL: %v", err)
	}
	opts.DB = wecomRelayTestRedisDB
	rdb := redis.NewClient(opts)
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("REDIS_TEST_URL unreachable: %v", err)
	}
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flushdb: %v", err)
	}
	t.Cleanup(func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	})
	return rdb
}

// blipOnce wraps a REAL claim store and fails its first Claim, which is the
// one fault a live Redis cannot be asked to produce on cue. Everything else —
// the SET NX, the TTL, the EXISTS behind the outcome watch — is the real
// implementation against the real server.
type blipOnce struct {
	DedupeStore
	mu   sync.Mutex
	done bool
}

func (b *blipOnce) Claim(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	b.mu.Lock()
	first := !b.done
	b.done = true
	b.mu.Unlock()
	if first {
		return false, errors.New("wecom test: redis blip")
	}
	return b.DedupeStore.Claim(ctx, key, ttl)
}

// sentTexts is what actually reached the chat, in order.
func sentTexts(t *testing.T, c *recordingConn) []string {
	t.Helper()
	c.mu.Lock()
	frames := append([]frameEnvelope(nil), c.frames...)
	c.mu.Unlock()
	var out []string
	for _, f := range frames {
		var body struct {
			Markdown struct {
				Content string `json:"content"`
			} `json:"markdown"`
		}
		if err := json.Unmarshal(f.Body, &body); err != nil {
			continue
		}
		if body.Markdown.Content != "" {
			out = append(out, body.Markdown.Content)
		}
	}
	return out
}

// seedSiblingTurn adds a SECOND completed turn to a seeded conversation, so two
// replies compete for one installation's queue — which is where ordering means
// anything at all.
func seedSiblingTurn(t *testing.T, pool *pgxpool.Pool, turn boundTurn) boundTurn {
	t.Helper()
	ctx := context.Background()
	newID := func() string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
			t.Fatalf("seed sibling: mint id: %v", err)
		}
		return id
	}
	var agentID, bindingID string
	if err := pool.QueryRow(ctx,
		`SELECT agent_id::text FROM chat_session WHERE id = $1`, turn.sessionID).Scan(&agentID); err != nil {
		t.Fatalf("seed sibling: read agent: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT binding_id::text FROM channel_task_delivery WHERE task_id = $1`, turn.taskID).Scan(&bindingID); err != nil {
		t.Fatalf("seed sibling: read binding: %v", err)
	}
	sibling := boundTurn{
		sessionID: turn.sessionID, taskID: newID(),
		instID: turn.instID, chatID: turn.chatID,
	}
	inputTaskID := newID()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM chat_message WHERE task_id = $1`, inputTaskID)
		_, _ = pool.Exec(ctx, `DELETE FROM channel_task_delivery WHERE task_id = $1`, sibling.taskID)
		_, _ = pool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = ANY($1)`,
			[]string{sibling.taskID, inputTaskID})
	})
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed sibling: %s: %v", strings.SplitN(strings.TrimSpace(sql), "\n", 2)[0], err)
		}
	}
	exec(`INSERT INTO agent_task_queue (id, agent_id, chat_session_id, status, completed_at)
	      VALUES ($1, $2, $3, 'completed', now())`, inputTaskID, agentID, sibling.sessionID)
	exec(`INSERT INTO agent_task_queue (id, agent_id, chat_session_id, status, completed_at, chat_input_task_id)
	      VALUES ($1, $2, $3, 'completed', now(), $4)`, sibling.taskID, agentID, sibling.sessionID, inputTaskID)
	exec(`INSERT INTO channel_task_delivery
	        (task_id, binding_id, installation_id, channel_type, channel_chat_id, chat_type, route_revision, config)
	      VALUES ($1, $2, $3, 'wecom', $4, 'p2p', 1, '{}')`,
		sibling.taskID, bindingID, sibling.instID, sibling.chatID)
	exec(`INSERT INTO chat_message (chat_session_id, role, content, task_id, channel_ingested)
	      VALUES ($1, 'user', '第二个问题', $2, true)`, sibling.sessionID, inputTaskID)
	return sibling
}

func chatDoneWith(turn boundTurn, content string) events.Event {
	e := chatDoneFor(turn)
	e.Payload = protocol.ChatDonePayload{
		ChatSessionID: turn.sessionID,
		TaskID:        turn.taskID,
		Content:       content,
	}
	return e
}

// ---------------------------------------------------------------------------
// 1. Ordering across a re-offer
// ---------------------------------------------------------------------------

// Two answers for one bot, produced in order, where the FIRST one's claim hits
// a Redis blip and has to be offered again. The second must not overtake it.
//
// This is the defect the review reproduced: the old dispatcher scheduled the
// re-offer with time.AfterFunc and moved straight on to the next item, so the
// wire order was [second, first] — two answers arriving in a chat in the
// opposite order to the questions that produced them.
func TestRelay_ARetriedReplyKeepsItsPlaceInLine(t *testing.T) {
	pool := twoReplicaDB(t)
	rdb := wecomTestRedis(t)
	first := seedBoundTurn(t, pool)
	second := seedSiblingTurn(t, pool, first)

	relay := &fanoutRelay{}
	dedupe := &blipOnce{DedupeStore: NewRedisDedupe(rdb, testClaimBudget, slog.Default())}

	// One shard, so both replies are provably on the same worker: with the
	// default eight the two installations would be the same anyway, but a
	// single queue says so rather than relying on the hash.
	cfg := RelayConfig{Shards: 1, LeaseSettle: 400 * time.Millisecond, RetryBackoff: 20 * time.Millisecond}
	holder := newRelayReplicaWith(t, pool, first.instID, true, relay, dedupe, cfg)
	publisher := newRelayReplicaWith(t, pool, first.instID, false, relay, dedupe, cfg)

	const firstAnswer, secondAnswer = "答案一", "答案二"
	publisher.bus.Publish(chatDoneWith(first, firstAnswer))
	publisher.bus.Publish(chatDoneWith(second, secondAnswer))

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if len(sentTexts(t, holder.conn)) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	got := sentTexts(t, holder.conn)
	if len(got) != 2 {
		t.Fatalf("the chat received %v, want both answers", got)
	}
	if got[0] != firstAnswer || got[1] != secondAnswer {
		t.Fatalf("delivery order = %v, want [%q %q] — the retried reply was overtaken",
			got, firstAnswer, secondAnswer)
	}
}

// ---------------------------------------------------------------------------
// 2. A pending re-offer belongs to the worker
// ---------------------------------------------------------------------------

// Wait() must mean it, and the way to see whether it does is to ask what
// happens to a reply that is between offers when the process stops.
//
// The old re-offer lived on a time.AfterFunc that nothing joined and nothing
// drained: it checked the context, found it cancelled, and returned — taking
// the frame with it. So a reply that had failed once and was waiting to be
// tried again was silently discarded by the very shutdown that runs a drain
// specifically so replies are not discarded. Owning the wait inside the worker
// is what puts that frame somewhere the drain can find it.
func TestRelay_AReplyWaitingToBeRetriedIsStillDrainedOnShutdown(t *testing.T) {
	t.Parallel()
	h := &failsOnceHandler{}
	router := NewRelayOutbound(&fanoutRelay{}, nil,
		// A backoff far longer than the test, so the frame is provably still
		// waiting when the shutdown arrives rather than racing it.
		RelayConfig{Shards: 1, LeaseSettle: 40 * time.Second, RetryBackoff: 20 * time.Second},
		slog.Default())
	router.Attach(h)
	ctx, cancel := context.WithCancel(context.Background())
	router.Start(ctx)

	body, _ := json.Marshal(relayFrame{Kind: relayKindReply, InstallationID: "inst-1", Content: "hi"})
	router.DeliverWecomOutbound("inst-1", body, "ev-1")

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && h.calls() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if h.calls() != 1 {
		t.Fatalf("first attempt ran %d times, want 1", h.calls())
	}

	cancel()
	router.Wait()

	if got := h.calls(); got != 2 {
		t.Fatalf("delivery attempts = %d, want 2 — a reply waiting to be retried was "+
			"discarded by the shutdown instead of drained", got)
	}
	if !h.deliveredOK() {
		t.Error("the drained retry did not reach the wire")
	}
	// And nothing is left behind it.
	for i, q := range router.queues {
		if n := len(q); n != 0 {
			t.Errorf("shard %d still holds %d frames after Wait", i, n)
		}
	}
}

// failsOnceHandler refuses the first delivery in a way that provably put
// nothing on the wire — so the frame is owed another offer — and accepts the
// second.
type failsOnceHandler struct {
	mu        sync.Mutex
	n         int
	delivered bool
}

func (h *failsOnceHandler) ownsSocket(string) bool { return true }
func (h *failsOnceHandler) deliverRelayed(context.Context, relayFrame) deliveryOutcome {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.n++
	if h.n == 1 {
		return outcomeProvablyNotSent
	}
	h.delivered = true
	return outcomeDone
}
func (h *failsOnceHandler) calls() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.n
}
func (h *failsOnceHandler) deliveredOK() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.delivered
}

// ---------------------------------------------------------------------------
// 3. One owner for the end-to-end outcome
// ---------------------------------------------------------------------------

// No replica holds a socket. Every party behaves correctly — the publisher
// routes it, each consumer reads it and correctly declines — and the user gets
// nothing. Somebody has to say so, exactly once.
//
// Before this, nobody did: the publisher returned after a successful publish
// and every consumer returned at the ownership gate, so the reply vanished
// with no counter moving, while the PR text claimed this counter could size
// the window.
func TestRelay_AReplyNoReplicaCouldSendIsCountedOnce(t *testing.T) {
	pool := twoReplicaDB(t)
	rdb := wecomTestRedis(t)
	turn := seedBoundTurn(t, pool)

	relay := &fanoutRelay{}
	dedupe := NewRedisDedupe(rdb, testClaimBudget, slog.Default())
	// A short chain so the grace the watch waits out is a test's worth of time
	// rather than a lease poll's.
	cfg := RelayConfig{Shards: 1, LeaseSettle: 120 * time.Millisecond, RetryBackoff: 20 * time.Millisecond}

	// Neither replica holds the socket: both are mid-reconnect, which is the
	// residual window SELF_HOSTING.md describes.
	a := newRelayReplicaWith(t, pool, turn.instID, false, relay, dedupe, cfg)
	b := newRelayReplicaWith(t, pool, turn.instID, false, relay, dedupe, cfg)

	a.bus.Publish(chatDoneWith(turn, "nobody can send this"))

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if a.mx.get("outbound_dropped:no_live_connection")+b.mx.get("outbound_dropped:no_live_connection") > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	total := a.mx.get("outbound_dropped:no_live_connection") + b.mx.get("outbound_dropped:no_live_connection")
	if total != 1 {
		t.Fatalf("no_live_connection = %d across both replicas, want exactly 1 — "+
			"a reply nobody could send must be counted once, by one owner", total)
	}
	if delivered := a.mx.get("outbound_delivered") + b.mx.get("outbound_delivered"); delivered != 0 {
		t.Errorf("outbound_delivered = %d, want 0 — nothing reached anybody", delivered)
	}
}

// The other half of the same rule, and the one that keeps it honest: when a
// replica DOES deliver, the publisher's watch must stay quiet. A claim that is
// held is the evidence that somebody took it.
func TestRelay_ADeliveredReplyIsCountedOnceAndNotAlsoLost(t *testing.T) {
	pool := twoReplicaDB(t)
	rdb := wecomTestRedis(t)
	turn := seedBoundTurn(t, pool)

	relay := &fanoutRelay{}
	dedupe := NewRedisDedupe(rdb, testClaimBudget, slog.Default())
	cfg := RelayConfig{Shards: 1, LeaseSettle: 120 * time.Millisecond, RetryBackoff: 20 * time.Millisecond}
	holder := newRelayReplicaWith(t, pool, turn.instID, true, relay, dedupe, cfg)
	publisher := newRelayReplicaWith(t, pool, turn.instID, false, relay, dedupe, cfg)

	publisher.bus.Publish(chatDoneWith(turn, "答案"))

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && len(sentTexts(t, holder.conn)) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if got := sentTexts(t, holder.conn); len(got) != 1 {
		t.Fatalf("the chat received %v, want the one answer", got)
	}
	// Past the whole grace, so the watch has run and had its say.
	time.Sleep(600 * time.Millisecond)

	if got := holder.mx.get("outbound_delivered"); got != 1 {
		t.Errorf("outbound_delivered on the holder = %d, want 1", got)
	}
	if got := publisher.mx.get("outbound_dropped"); got != 0 {
		t.Errorf("outbound_dropped on the publisher = %d, want 0 — it was delivered", got)
	}
	if got := holder.mx.get("outbound_dropped"); got != 0 {
		t.Errorf("outbound_dropped on the holder = %d, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// 4. The sizing constants against the bounds they claim
// ---------------------------------------------------------------------------

// The re-offer chain exists to outlast a lease move, and how long that takes is
// the channel supervisor's poll interval — an external bound, and a knob. The
// old fixed five attempts at 200ms covered about three seconds against a 30s
// default poll: it expired while the replica that would deliver was still on
// its way.
func TestRelayRetryPlan_OutlastsALeaseMove(t *testing.T) {
	t.Parallel()
	for _, settle := range []time.Duration{
		5 * time.Second,
		30 * time.Second, // the CHANNEL_WS_LEASE_POLL_INTERVAL default
		2 * time.Minute,
	} {
		cfg := RelayConfig{LeaseSettle: settle}.withDefaults()
		plan := cfg.retryPlan()
		var total time.Duration
		for _, d := range plan {
			total += d
			if d > settle {
				t.Errorf("settle=%s: a single backoff of %s is longer than the move it waits for", settle, d)
			}
		}
		if total < settle {
			t.Errorf("settle=%s: the chain covers only %s — it gives up while the lease is still moving",
				settle, total)
		}
		if len(plan) > relayRetryChainCap {
			t.Errorf("settle=%s: %d attempts exceeds the chain cap %d", settle, len(plan), relayRetryChainCap)
		}
	}
}

// A default deployment's chain has to cover the default poll interval. Stated
// as its own assertion because this is the number that was wrong.
func TestRelayRetryPlan_DefaultsCoverTheDefaultPollInterval(t *testing.T) {
	t.Parallel()
	cfg := RelayConfig{}.withDefaults()
	if cfg.LeaseSettle != engine.DefaultPollInterval {
		t.Fatalf("default LeaseSettle = %s, want the supervisor's default poll interval %s",
			cfg.LeaseSettle, engine.DefaultPollInterval)
	}
	var total time.Duration
	for _, d := range cfg.retryPlan() {
		total += d
	}
	if total < engine.DefaultPollInterval {
		t.Fatalf("the default chain covers %s against a %s lease move", total, engine.DefaultPollInterval)
	}
}

// The drain runs inside the process's own shutdown, so its budget has to fit
// under the channel supervisor's — the thing that is joined after it. A drain
// budget larger than that would push shutdown past the supervisor's own bound
// and cost the final lease release, which is what makes the next replica wait
// out a whole LeaseTTL after a redeploy.
func TestRelayDrainBudget_FitsUnderTheSupervisorShutdownTimeout(t *testing.T) {
	t.Parallel()
	cfg := RelayConfig{}.withDefaults()
	if cfg.DrainBudget >= engine.DefaultShutdownTimeout {
		t.Fatalf("drain budget %s does not fit under the supervisor's shutdown timeout %s",
			cfg.DrainBudget, engine.DefaultShutdownTimeout)
	}
}

// The claim has to outlive the replay window it guards, at every grace an
// operator can set — including one longer than the floor.
func TestDedupeTTL_OutlivesEveryReplayWindow(t *testing.T) {
	t.Parallel()
	for _, grace := range []time.Duration{0, time.Minute, 5 * time.Minute, 2 * time.Hour} {
		if ttl := dedupeTTLFor(grace); ttl <= grace {
			t.Errorf("grace=%s: claim TTL %s expires inside the window it guards", grace, ttl)
		}
	}
}

// The outcome watch must not call a reply lost while it is still being
// re-offered: the grace has to sit past the whole chain.
func TestRelayOutcomeGrace_OutlastsTheReOfferChain(t *testing.T) {
	t.Parallel()
	r := NewRelayOutbound(nil, nil, RelayConfig{}, slog.Default())
	var chain time.Duration
	for _, d := range r.retryPlan {
		chain += d
	}
	if r.outcomeGrace() <= chain {
		t.Fatalf("outcome grace %s is inside the %s re-offer chain — a reply still in flight would be counted lost",
			r.outcomeGrace(), chain)
	}
}

// shardFor must stay inside the configured queue count, including for a
// non-default shard count. A hash that indexed past the slice would panic on
// the shard read loop, which is the one goroutine that must never fail.
func TestRelayShardFor_StaysInsideTheConfiguredQueues(t *testing.T) {
	t.Parallel()
	for _, shards := range []int{1, 3, 8, 64} {
		r := NewRelayOutbound(nil, nil, RelayConfig{Shards: shards}, slog.Default())
		for i := 0; i < 500; i++ {
			id := util.UUIDToString(mustTestUUID(t))
			if got := r.shardFor(id); got < 0 || got >= shards {
				t.Fatalf("shards=%d: shardFor(%s) = %d, outside the queues", shards, id, got)
			}
		}
	}
}
