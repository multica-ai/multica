package wecom

// relay_outbound.go — how a reply reaches the replica that can send it.
//
// WeCom is the one channel with no outbound REST path: every write goes over
// the aibot WebSocket, and the WS lease means exactly one replica holds it.
// chat:done, meanwhile, is published on the in-process events.Bus by whichever
// replica served the daemon's POST /tasks/{id}/complete — a load-balancer
// decision. Off-lease, outbound.go used to have nothing to do but drop the
// reply (GH #7215, #6890; SELF_HOSTING.md states the resulting constraint).
//
// This routes it over the Redis Stream relay the repository already runs for
// exactly this shape of problem: daemonws.RelayNotifier wakes a daemon
// connected to another replica by publishing under ScopeDaemonRuntime, and
// every node's XREAD loop hands the frame to whoever holds that connection.
//
// THREE THINGS THAT MECHANISM REQUIRES OF ITS CONSUMERS, and that a naive copy
// of the daemon-wakeup shape does not satisfy:
//
//  1. It replays. A shard reader starts from (now - ReplayGrace), not "$", so
//     events published while a pod was down are re-read — its own doc says
//     "downstream consumers must be idempotent". A per-process seen-set is not
//     idempotent across a restart, and does not span two replicas either. The
//     claim therefore lives in Redis, keyed on the turn, with a lifetime that
//     comfortably outlasts the replay window. Redis is not a new dependency
//     here: it IS the transport. No Redis means no relay, which means no
//     cross-replica delivery and nothing to deduplicate.
//
//  2. It calls the consumer SYNCHRONOUSLY on the shard read loop. The daemon
//     wakeup's work there is a map lookup and a buffered write; ours is a
//     network round trip that waits up to ackTimeout for the platform's
//     verdict. Doing that inline would let one unhealthy bot stall browser
//     realtime traffic, daemon wakeups, and every other bot on that shard. So
//     DeliverWecomOutbound only hands the frame to a bounded queue and
//     returns.
//
//  3. Its readers start early. Registration therefore has to be possible
//     before the senders registry and the subscriber exist: this object is
//     built and registered first, and Attach supplies the handler afterwards.
//     Frames that arrive in between wait in the queue rather than being
//     dropped against a nil field.
//
// ORDERING: every reply for one installation is carried by one queue and one
// worker, in the order it arrived — INCLUDING across a re-offer. A frame that
// has to be tried again does not go to the back of the queue; it parks at the
// head of its own installation's line and everything behind it waits, because
// two answers to the same person arriving in the wrong order is the defect
// this ordering exists to prevent.
//
// Sharding is a CONCURRENCY bound, not an isolation guarantee: installations
// are hashed onto RelayConfig.Shards queues, so two bots that collide on a
// shard do wait behind each other. What the shard count buys is that one slow
// bot cannot occupy every worker.

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/util"
)

// relayPublisher is the slice of the realtime relay this package needs.
// *realtime.ShardedStreamRelay and *realtime.RedisRelay both satisfy it.
type relayPublisher interface {
	PublishWithID(scopeType, scopeID, exclude string, frame []byte, id string) error
}

// DedupeStore is the at-most-once claim that spans a restart and two replicas.
// Backed by Redis in production (see NewRedisDedupe); nil leaves only the
// in-process gate, which is correct for a single-replica deployment because
// there the relay is never used at all.
type DedupeStore interface {
	// Claim reports whether the caller is the first to take key. It must be
	// atomic across processes.
	Claim(ctx context.Context, key string, ttl time.Duration) (bool, error)
	// Release gives a claim back, for a delivery that provably did not happen.
	Release(ctx context.Context, key string)
	// Held reports whether key is claimed right now. It is how the publisher
	// learns, after the fact, whether ANY replica took a delivery it routed —
	// see RelayOutbound.watchOutcomes.
	Held(ctx context.Context, key string) (bool, error)
	// ClaimBudget is the longest ONE round trip above may take. The store
	// states it because the store enforces it: outcomeGrace pays this budget
	// once per offer, and a copy of the number kept by the dispatcher would be
	// free to drift from the timeout actually applied.
	ClaimBudget() time.Duration
}

// minDedupeTTL floors the claim lifetime. The invariant the claim upholds is
// that it outlives the relay's replay window — a claim that expires while the
// frame is still replayable is not a claim — and that window is an OPERATOR
// KNOB (REALTIME_RELAY_REPLAY_GRACE), not a constant, so the TTL cannot be one
// either: a 2h grace against a fixed 1h TTL would re-send an answer the user
// already read on any restart between the two. dedupeTTLFor derives it.
const minDedupeTTL = time.Hour

// dedupeTTLFor is max(minDedupeTTL, 2×replayGrace): twice the grace so a claim
// comfortably outlives the window it guards, floored so a tiny grace does not
// produce a claim shorter than the hour the default always had.
func dedupeTTLFor(replayGrace time.Duration) time.Duration {
	if ttl := 2 * replayGrace; ttl > minDedupeTTL {
		return ttl
	}
	return minDedupeTTL
}

// RelayConfig sizes the dispatcher. None of these is a taste: each one has an
// operational bound behind it, named in its comment, and the two whose bound
// lives OUTSIDE this package are passed in rather than guessed — the relay's
// replay window and the supervisor's lease poll interval.
//
// A zero field takes its documented default.
type RelayConfig struct {
	// Shards is how many independent queues carry frames, and it is a
	// concurrency bound rather than an ordering one: ordering comes from the
	// hash putting one installation on one queue, and installations that
	// collide on a shard DO wait behind each other. The number to size against
	// is therefore how many bots may be slow at once before a healthy one
	// queues behind them. Eight covers a deployment's worth of bots against
	// the one thing that makes a delivery slow — ackTimeout, 5s — while
	// keeping eight idle goroutines rather than one per installation.
	Shards int

	// QueueDepth is per shard, and the burst it must absorb is a restart's
	// replay: the shard reader opens at (now - ReplayGrace) and hands over
	// everything published in that window at once. 256 per shard is 2048
	// frames in flight across the default eight — more than a replay window
	// of WeCom answers can hold for one deployment, and small enough that a
	// wedged bot sheds instead of growing the heap without bound.
	QueueDepth int

	// DrainBudget bounds the WHOLE post-shutdown drain, not one delivery.
	// Its bound is external: the drain runs inside the process's own
	// shutdown, so it must fit under the channel supervisor's ShutdownTimeout
	// (engine.Config, 15s by default) with room for the supervisor's own
	// join. Ten seconds is that fit; whatever misses it is left to the claim
	// TTL and the next replica's replay window.
	DrainBudget time.Duration

	// ReplayGrace is the relay's configured startup lookback. It sizes the
	// claim TTL (dedupeTTLFor) and it is an operator knob
	// (REALTIME_RELAY_REPLAY_GRACE), which is exactly why it is a parameter.
	ReplayGrace time.Duration

	// LeaseSettle is how long a WebSocket lease takes to finish moving to
	// another replica, and it sizes the retry chain. Its bound is the channel
	// supervisor's PollInterval (CHANNEL_WS_LEASE_POLL_INTERVAL, 30s by
	// default): a replica learns it should hold an installation on its next
	// sweep, so a frame that arrives mid-move has to be re-offered for at
	// least that long or it is given up on while the holder is still on its
	// way. The previous fixed five attempts at 200ms covered three seconds —
	// a tenth of one poll — and expired long before the move it existed for.
	LeaseSettle time.Duration

	// RetryBackoff is the FIRST wait between re-offers; the chain doubles from
	// there, capped at a quarter of LeaseSettle so a lease that lands early is
	// not sat out. Small on purpose: the common reason a re-offer succeeds is
	// that the move has already finished.
	RetryBackoff time.Duration
}

const (
	defaultRelayShards      = 8
	defaultRelayQueueDepth  = 256
	defaultRelayDrainBudget = 10 * time.Second
	// defaultRelayLeaseSettle is the supervisor's own default poll interval,
	// referenced rather than repeated: it is the same quantity, and a copy
	// here would be free to drift from the thing it is meant to outlast.
	defaultRelayLeaseSettle  = engine.DefaultPollInterval
	defaultRelayRetryBackoff = 200 * time.Millisecond
)

// withDefaults fills the zero fields and returns the completed config.
func (c RelayConfig) withDefaults() RelayConfig {
	if c.Shards <= 0 {
		c.Shards = defaultRelayShards
	}
	if c.QueueDepth <= 0 {
		c.QueueDepth = defaultRelayQueueDepth
	}
	if c.DrainBudget <= 0 {
		c.DrainBudget = defaultRelayDrainBudget
	}
	if c.LeaseSettle <= 0 {
		c.LeaseSettle = defaultRelayLeaseSettle
	}
	if c.RetryBackoff <= 0 {
		c.RetryBackoff = defaultRelayRetryBackoff
	}
	return c
}

// retryPlan is the re-offer chain, computed once from the settle window rather
// than written down: delays double from RetryBackoff, each capped at a quarter
// of LeaseSettle, and the chain is long enough that its TOTAL covers one and a
// half settle windows — the move itself, plus half of one for the reconnect
// and subscribe that follow it.
//
// Bounded twice over so a pathological config cannot produce an unbounded
// chain: at most relayRetryChainCap entries.
const relayRetryChainCap = 24

func (c RelayConfig) retryPlan() []time.Duration {
	target := c.LeaseSettle + c.LeaseSettle/2
	cap := c.LeaseSettle / 4
	if cap < c.RetryBackoff {
		cap = c.RetryBackoff
	}
	var (
		plan  []time.Duration
		total time.Duration
		delay = c.RetryBackoff
	)
	for total < target && len(plan) < relayRetryChainCap {
		if delay > cap {
			delay = cap
		}
		plan = append(plan, delay)
		total += delay
		delay *= 2
	}
	return plan
}

// relayFrame is one delivery in transit between replicas. It carries
// identifiers rather than rendered payloads for anything the lease holder can
// read for itself, so an attachment is fetched by the replica that will send
// it rather than shipped through Redis.
type relayFrame struct {
	Kind           string `json:"kind"` // relayKindReply | relayKindInbox
	InstallationID string `json:"installation_id"`
	ChatID         string `json:"chat_id"`
	ChatType       int    `json:"chat_type"`
	Content        string `json:"content"`
	TaskID         string `json:"task_id,omitempty"`
	MessageID      string `json:"message_id,omitempty"`
	WorkspaceID    string `json:"workspace_id,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	CarriesFiles   bool   `json:"carries_files,omitempty"`
}

const (
	relayKindReply = "reply"
	relayKindInbox = "inbox"
)

// relayHandler performs a delivery on the replica that holds the socket.
// *Outbound implements it; the indirection is what lets this object be
// registered with the relay before the subscriber exists.
type relayHandler interface {
	deliverRelayed(ctx context.Context, f relayFrame) deliveryOutcome
	// ownsSocket answers "does this process hold that installation's live
	// connection right now". It gates the global claim — see perform.
	ownsSocket(installationID string) bool
}

// deliveryOutcome tells the dispatcher whether the claim may be released.
type deliveryOutcome int

const (
	// outcomeNotOurs — this replica holds no socket for that installation.
	// Another one will take the frame; the claim must go back.
	outcomeNotOurs deliveryOutcome = iota
	// outcomeDone — delivered, or failed in a way that must not be retried.
	outcomeDone
	// outcomeProvablyNotSent — nothing reached the wire, so the claim goes
	// back and a replay or another replica may try again. Deliberately NOT
	// used for an ack timeout: errAckTimeout means the frame may well have
	// arrived, and this adapter's standing rule is that a caller retries such
	// a send at its own risk.
	outcomeProvablyNotSent
)

// seenEvents is a bounded, per-process set of relay event ids already acted on.
// It is a cheap first gate in front of the Redis claim — the publisher's own
// copy of its own frame is the common case and never needs a round trip. It is
// NOT the idempotency mechanism; DedupeStore is.
type seenEvents struct {
	mu    sync.Mutex
	ids   map[string]struct{}
	order []string
	limit int
}

func newSeenEvents(limit int) *seenEvents {
	return &seenEvents{ids: make(map[string]struct{}, limit), limit: limit}
}

func (s *seenEvents) claim(id string) bool {
	if id == "" {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.ids[id]; dup {
		return false
	}
	s.ids[id] = struct{}{}
	s.order = append(s.order, id)
	if len(s.order) > s.limit {
		delete(s.ids, s.order[0])
		s.order = s.order[1:]
	}
	return true
}

func (s *seenEvents) forget(id string) {
	if id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.ids, id)
}

// queued is one frame waiting for a worker, with the id its claims are under
// and how many times a worker has already picked it up.
type queued struct {
	frame    relayFrame
	eventID  string
	attempts int
}

// A released claim has nobody to retry it EXCEPT this process: each replica
// reads a given stream frame exactly once and replays only on restart, so
// "release so the holder can take it" strands the reply whenever the holder
// already processed its copy — it lost the claim race, returned, and nothing
// will ever wake it again. The party that released is therefore the party
// that re-offers it, on RelayConfig.retryPlan, until the lease settles.

// hold is one installation's line while its head is waiting out a re-offer.
// items[0] is the frame being retried; everything after it arrived later and
// must not overtake it. Owned by exactly one worker goroutine, so unlocked.
type hold struct {
	items   []queued
	readyAt time.Time
}

// RelayOutbound publishes a delivery to the replica holding the bot's socket,
// and performs the ones other replicas publish. One per process.
type RelayOutbound struct {
	publisher relayPublisher
	dedupe    DedupeStore
	dedupeTTL time.Duration
	cfg       RelayConfig
	retryPlan []time.Duration
	logger    *slog.Logger
	seen      *seenEvents

	// verify carries a routed reply's id to the outcome watcher — the one
	// owner of "did anybody end up delivering this". Buffered and shed rather
	// than blocking: the publisher is on a bus subscriber's goroutine.
	verify chan pendingOutcome

	// metrics arrives after construction: this object is built before the
	// metrics registry exists, because the relay's shard readers start before
	// both. Atomic because DeliverWecomOutbound runs on the shard reader while
	// boot is still wiring.
	metrics atomic.Value // Metrics

	ready   chan struct{}
	handler relayHandler

	queues []chan queued
	wg     sync.WaitGroup
}

// NewRelayOutbound builds the cross-replica router. replayGrace is the relay's
// configured startup replay window, which sizes the claim TTL (dedupeTTLFor).
// Call Attach with the subscriber and Start with the process context;
// registering it on the relay is safe before either.
func NewRelayOutbound(publisher relayPublisher, dedupe DedupeStore, cfg RelayConfig, logger *slog.Logger) *RelayOutbound {
	if logger == nil {
		logger = slog.Default()
	}
	cfg = cfg.withDefaults()
	r := &RelayOutbound{
		publisher: publisher,
		dedupe:    dedupe,
		dedupeTTL: dedupeTTLFor(cfg.ReplayGrace),
		cfg:       cfg,
		retryPlan: cfg.retryPlan(),
		logger:    logger,
		seen:      newSeenEvents(4096),
		verify:    make(chan pendingOutcome, cfg.QueueDepth),
		ready:     make(chan struct{}),
		queues:    make([]chan queued, cfg.Shards),
	}
	for i := range r.queues {
		r.queues[i] = make(chan queued, cfg.QueueDepth)
	}
	return r
}

// SetMetrics installs the health sink. Called once during boot, before any
// delivery can run (the workers wait on Attach), so the only concurrent reader
// is the shed counter on the shard reader — which is why it is atomic.
func (r *RelayOutbound) SetMetrics(m Metrics) {
	if r != nil && m != nil {
		r.metrics.Store(m)
	}
}

func (r *RelayOutbound) mx() Metrics {
	if v := r.metrics.Load(); v != nil {
		return v.(Metrics)
	}
	return nopMetrics{}
}

// Attach supplies the handler that performs deliveries. Called once, after the
// subscriber exists. Frames received before this wait in their queue.
func (r *RelayOutbound) Attach(h relayHandler) {
	if r == nil {
		return
	}
	r.handler = h
	close(r.ready)
}

// Start runs the workers until ctx is done, then drains what is already
// queued so a graceful shutdown does not strand a reply somebody is waiting
// for.
func (r *RelayOutbound) Start(ctx context.Context) {
	if r == nil {
		return
	}
	for i := range r.queues {
		r.wg.Add(1)
		go r.work(ctx, r.queues[i])
	}
	r.wg.Add(1)
	go r.watchOutcomes(ctx)
}

// handlerReady blocks until Attach has run, and reports whether there is a
// handler to deliver with at all.
func (r *RelayOutbound) handlerReady(ctx context.Context) bool {
	select {
	case <-r.ready:
		return true
	case <-ctx.Done():
		// Cancelled before (or while) the handler arrived. If it IS already
		// attached — the select picks between two ready channels arbitrarily —
		// what is queued can still be drained; without it there is nothing a
		// drain could deliver with.
		select {
		case <-r.ready:
			return true
		default:
			return false
		}
	}
}

// work is one shard's whole life.
//
// Everything it touches belongs to this goroutine alone: the per-installation
// lines and the one timer that wakes them. That is deliberate on both counts.
// The lines are what keep a re-offered frame AT THE HEAD of its installation
// rather than at the back of the queue, so a later reply for the same bot
// cannot overtake it. The timer being the worker's own is what makes a pending
// re-offer part of the worker — a timer scheduled outside it could fire after
// the worker had drained and exited, and put a frame into a queue nobody is
// reading.
func (r *RelayOutbound) work(ctx context.Context, q chan queued) {
	defer r.wg.Done()
	if !r.handlerReady(ctx) {
		return
	}
	lines := map[string]*hold{}
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		wait := time.Hour
		if next, ok := earliestDue(lines); ok {
			if wait = time.Until(next); wait < 0 {
				wait = 0
			}
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(wait)

		select {
		case item := <-q:
			// The select is fair, so an item can win against an already-fired
			// ctx.Done. Performing it on the cancelled context would misfile it
			// as a dedupe outage; it belongs to the drain.
			if ctx.Err() != nil {
				r.drainRemaining(ctx, q, lines, &item)
				return
			}
			r.offer(ctx, lines, item)
		case <-timer.C:
			if ctx.Err() != nil {
				r.drainRemaining(ctx, q, lines, nil)
				return
			}
			r.fireDue(ctx, lines)
		case <-ctx.Done():
			r.drainRemaining(ctx, q, lines, nil)
			return
		}
	}
}

// earliestDue is the soonest moment any line's head may be offered again.
func earliestDue(lines map[string]*hold) (time.Time, bool) {
	var soonest time.Time
	found := false
	for _, h := range lines {
		if len(h.items) == 0 {
			continue
		}
		if !found || h.readyAt.Before(soonest) {
			soonest, found = h.readyAt, true
		}
	}
	return soonest, found
}

// offer takes one frame off the shard queue. If its installation already has a
// line waiting out a re-offer, it joins the BACK of that line: overtaking the
// frame in front of it is exactly the reordering this exists to prevent.
func (r *RelayOutbound) offer(ctx context.Context, lines map[string]*hold, item queued) {
	if h := lines[item.frame.InstallationID]; h != nil {
		if len(h.items) >= r.cfg.QueueDepth {
			r.shed(item, "the installation's line is full behind a retry")
			return
		}
		h.items = append(h.items, item)
		return
	}
	r.step(ctx, lines, item)
}

// step runs one delivery and files what it means for the line. A frame that
// needs another offer stays at the head of its installation's line with the
// next delay on it; a finished one lets whatever queued behind it move up.
func (r *RelayOutbound) step(ctx context.Context, lines map[string]*hold, item queued) {
	inst := item.frame.InstallationID
	if r.perform(ctx, item) {
		r.advance(lines, inst)
		return
	}
	delay, more := r.delayFor(item.attempts)
	if !more {
		// By far the likeliest reason to exhaust the chain is a claim held
		// because another replica already delivered, so this is not counted as
		// a drop. The publisher's outcome watch is what names a reply nobody
		// delivered, and it names it once.
		r.logger.Warn("wecom relay: giving up on a routed delivery after retries",
			"installation_id", inst, "task_id", item.frame.TaskID,
			"attempts", item.attempts+1)
		r.advance(lines, inst)
		return
	}
	item.attempts++
	h := lines[inst]
	if h == nil {
		h = &hold{items: []queued{item}}
		lines[inst] = h
	} else {
		h.items[0] = item // it is the head; step is only ever called on one
	}
	h.readyAt = time.Now().Add(delay)
}

// advance retires a finished head and lets the next frame for that
// installation become due immediately.
func (r *RelayOutbound) advance(lines map[string]*hold, inst string) {
	h := lines[inst]
	if h == nil {
		return // it came straight off the queue and never joined a line
	}
	h.items = h.items[1:]
	if len(h.items) == 0 {
		delete(lines, inst)
		return
	}
	h.readyAt = time.Time{} // due now; the next loop turn picks it up
}

// fireDue offers every line whose head has waited out its backoff.
func (r *RelayOutbound) fireDue(ctx context.Context, lines map[string]*hold) {
	now := time.Now()
	for inst, h := range lines {
		if len(h.items) == 0 {
			delete(lines, inst)
			continue
		}
		if h.readyAt.After(now) {
			continue
		}
		r.step(ctx, lines, h.items[0])
	}
}

// delayFor is how long the next offer waits, and whether there is one.
func (r *RelayOutbound) delayFor(attempts int) (time.Duration, bool) {
	if attempts < 0 || attempts >= len(r.retryPlan) {
		return 0, false
	}
	return r.retryPlan[attempts], true
}

// shed records a frame refused for want of queue space. It is an ADMISSION
// decision and nothing more: relay_shed, labelled by kind, on whichever
// replica refused it.
//
// It deliberately does not touch the reply counters. Every replica reads every
// frame, so no replica can tell from here whether a shed cost the user
// anything — the one that sheds may not be the one that would have sent it,
// and during a lease handoff two replicas can hold a sender at once, so even
// "do I hold the socket" is not proof of being the only one who could. Each
// replica answering that question for itself is what produced one reply
// counted as delivered and dropped at the same time.
//
// So the reply outcome stays with the single owner that can settle it after
// the fact: the publisher's watchOutcomes, which asks whether ANY replica ever
// claimed the delivery and counts the loss once if none did. A shed that
// really did cost the user the reply reaches that owner as an unclaimed
// delivery, and is counted there.
func (r *RelayOutbound) shed(item queued, why string) {
	r.mx().RecordRelayShed(item.frame.Kind)
	r.logger.Warn("wecom relay: shedding a routed delivery, "+why,
		"kind", item.frame.Kind,
		"installation_id", item.frame.InstallationID, "task_id", item.frame.TaskID)
}

// drainRemaining performs what is already queued so a graceful shutdown does
// not strand a reply — under ONE bounded budget for the whole drain, so a full
// shard cannot stack sequential ack waits. Deliveries started near the edge
// inherit the deadline and abort with it. first is an item a worker had
// already taken off the queue when the cancel won the race.
// Frames parked in a line waiting out a re-offer go first and without their
// backoff: the wait existed to give a lease time to move, and shutdown has
// overtaken that.
func (r *RelayOutbound) drainRemaining(ctx context.Context, q chan queued, lines map[string]*hold, first *queued) {
	drainCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.cfg.DrainBudget)
	defer cancel()
	if first != nil {
		// Join the back of its installation's line rather than jump it. This
		// frame was taken off the queue AFTER whatever is parked at that
		// line's head, and sending it first would invert two answers in the
		// user's chat — the exact reordering offer() exists to prevent, undone
		// on the way out. With no line for that installation there is nothing
		// to overtake.
		if h := lines[first.frame.InstallationID]; h != nil {
			h.items = append(h.items, *first)
		} else {
			r.perform(drainCtx, *first)
		}
	}
	for inst, h := range lines {
		for _, item := range h.items {
			if drainCtx.Err() != nil {
				return
			}
			r.perform(drainCtx, item)
		}
		delete(lines, inst)
	}
	for {
		if drainCtx.Err() != nil {
			return
		}
		select {
		case item := <-q:
			r.perform(drainCtx, item)
		default:
			return
		}
	}
}

// Wait blocks until every worker has stopped. For tests and shutdown.
func (r *RelayOutbound) Wait() {
	if r != nil {
		r.wg.Wait()
	}
}

// publish hands a delivery to the other replicas. Reports whether it went out.
func (r *RelayOutbound) publish(f relayFrame, eventID string) bool {
	if r == nil || r.publisher == nil {
		return false
	}
	body, err := json.Marshal(f)
	if err != nil {
		r.logger.Warn("wecom relay: marshal outbound frame", "error", err)
		return false
	}
	if err := r.publisher.PublishWithID(realtime.ScopeWecomOutbound, f.InstallationID, "", body, eventID); err != nil {
		r.logger.Warn("wecom relay: publish failed",
			"error", err, "installation_id", f.InstallationID, "task_id", f.TaskID)
		return false
	}
	// A published reply is now owed an outcome by somebody. watchOutcomes is
	// that somebody — see its comment for why it cannot be anyone else.
	if f.Kind == relayKindReply {
		r.awaitOutcome(f, eventID)
	}
	return true
}

// DeliverWecomOutbound is realtime.WecomOutboundDeliverer. It must not block:
// the caller is the shared shard read loop, and everything else on that shard
// waits behind it.
func (r *RelayOutbound) DeliverWecomOutbound(scopeID string, frame []byte, eventID string) {
	if r == nil {
		return
	}
	var f relayFrame
	if err := json.Unmarshal(frame, &f); err != nil {
		r.logger.Warn("wecom relay: undecodable frame", "error", err, "installation_id", scopeID)
		return
	}
	select {
	case r.queues[r.shardFor(f.InstallationID)] <- queued{frame: f, eventID: eventID}:
	default:
		// Shed rather than stall the shard. Counted, because a reply nobody
		// gets is exactly what this whole path exists to stop being silent.
		r.shed(queued{frame: f, eventID: eventID}, "dispatch queue full")
	}
}

func (r *RelayOutbound) shardFor(installationID string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(installationID))
	return int(h.Sum32() % uint32(len(r.queues)))
}

// perform runs one delivery under an at-most-once claim, and reports whether
// this frame is FINISHED — delivered, refused, or not this replica's business.
// False means it is owed another offer, which the caller schedules at the head
// of the installation's line.
func (r *RelayOutbound) perform(ctx context.Context, item queued) bool {
	// Ownership FIRST, the global claim second. The claim is a cross-replica
	// SET NX: every replica reads every frame, and if one that cannot send
	// were allowed to win it, the replica that can send would lose the race,
	// conclude somebody else has it, and return — while the winner discovers
	// it holds no socket, releases the key, and nothing ever wakes the loser
	// again. Every party behaves correctly and the reply is still lost. So a
	// replica competes for the claim only after establishing it could honour
	// it. The socket can still vanish between this check and the send; that
	// window is why deliverRelayed re-checks and why a not-ours outcome
	// releases the claim.
	if !r.handler.ownsSocket(item.frame.InstallationID) {
		return true
	}
	if !r.seen.claim(item.eventID) {
		return true
	}
	key := dedupeKey(item.eventID)
	if r.dedupe != nil {
		won, err := r.dedupe.Claim(ctx, key, r.dedupeTTL)
		if err != nil {
			// A dedupe store that cannot answer must not silently become an
			// at-least-once path: a duplicate answer in a room is worse than a
			// late one. But nothing external retries a running process either
			// — replay happens only on restart — so the retry is ours.
			r.seen.forget(item.eventID)
			r.logger.WarnContext(ctx, "wecom relay: dedupe unavailable, retrying locally",
				"error", err, "installation_id", item.frame.InstallationID)
			return false
		}
		if !won {
			// Losing the claim does NOT mean somebody else will deliver. In a
			// mid-flight lease move the loser here can be the replica that
			// NOW holds the socket, while the winner is about to discover it
			// no longer does and release — and each replica reads a stream
			// frame exactly once, so nothing external ever hands it back.
			// The loser therefore checks again, bounded and backed off; the
			// common case (the claim is held because the reply was
			// delivered) burns the chain out and stops.
			r.seen.forget(item.eventID)
			return false
		}
	}
	outcome := r.handler.deliverRelayed(ctx, item.frame)
	if outcome == outcomeDone {
		return true
	}
	// Not ours, or provably never written: give the claim back — and offer it
	// again locally too, because release alone wakes nobody (see !won above).
	r.seen.forget(item.eventID)
	if r.dedupe != nil {
		r.dedupe.Release(ctx, key)
	}
	return false
}

func dedupeKey(eventID string) string { return "wecom:outbound:claim:" + eventID }

// pendingOutcome is one routed reply whose end-to-end fate is not known yet.
type pendingOutcome struct {
	key       string
	sessionID string
	instID    string
	taskID    string
	dueAt     time.Time
}

// outcomeGrace is how long a routed reply is given to find a holder before the
// publisher concludes nobody had one. It has to outlast the WHOLE re-offer
// chain — a reply still being retried is in flight, not lost — plus the claim
// round trip each of those offers pays for.
func (r *RelayOutbound) outcomeGrace() time.Duration {
	// One claim round trip per OFFER, not one for the chain. Every re-offer
	// makes its own Claim call and each can burn the store's full claim budget, so a
	// grace built from the backoffs plus a single round trip expires while the
	// chain is still running against a slow store — and the watcher then
	// records a loss that the next attempt contradicts by delivering. Sizing
	// it for the worst case costs a later settle on a healthy store, which is
	// the harmless direction.
	budget := defaultClaimBudget
	if r.dedupe != nil {
		budget = r.dedupe.ClaimBudget()
	}
	total := budget * time.Duration(len(r.retryPlan)+1)
	for _, d := range r.retryPlan {
		total += d
	}
	return total
}

// watchOutcomes is the ONE owner of a routed reply's final outcome.
//
// Nobody else can be. The publisher hands the frame to Redis and returns; every
// replica that reads it and holds no socket returns silently, correctly, and
// counts nothing; the replica that does deliver counts the delivery. So when
// NO replica holds the socket — all of them mid-reconnect — the reply is lost
// with every party behaving properly and no counter moving. That is the window
// SELF_HOSTING.md describes and that nothing could previously size.
//
// The claim key is what makes it observable after the fact: it is set by
// whoever took the delivery and released only by a replica that proved it sent
// nothing. So once the re-offer chain can no longer be running, a key that is
// absent means the reply reached nobody. One Redis EXISTS per routed reply,
// and routed replies are the off-lease minority.
func (r *RelayOutbound) watchOutcomes(ctx context.Context) {
	defer r.wg.Done()
	if r.dedupe == nil {
		// No claim store means no relay either (see DedupeStore), so there is
		// no routed reply whose outcome could be in question.
		for {
			select {
			case <-r.verify:
			case <-ctx.Done():
				return
			}
		}
	}
	var pending []pendingOutcome
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		wait := time.Hour
		if len(pending) > 0 {
			if wait = time.Until(pending[0].dueAt); wait < 0 {
				wait = 0
			}
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(wait)

		select {
		case p := <-r.verify:
			pending = append(pending, p) // the grace is constant, so this stays sorted
		case <-timer.C:
			now := time.Now()
			for len(pending) > 0 && !pending[0].dueAt.After(now) {
				r.settle(ctx, pending[0])
				pending = pending[1:]
			}
		case <-ctx.Done():
			// Shutdown is not evidence. A reply still inside its grace may yet
			// be delivered by the replica that has it, and this process is
			// leaving; counting it here would report a loss that did not
			// happen.
			if len(pending) > 0 {
				r.logger.Info("wecom relay: shutting down with routed replies still unresolved",
					"count", len(pending))
			}
			return
		}
	}
}

// settle asks whether anybody ever claimed one routed reply, and records the
// loss if nobody did.
func (r *RelayOutbound) settle(ctx context.Context, p pendingOutcome) {
	held, err := r.dedupe.Held(ctx, p.key)
	if err != nil {
		// An unreadable claim store proves nothing either way. Saying "lost"
		// here would turn a Redis blip into a fleet of phantom drops.
		r.logger.WarnContext(ctx, "wecom relay: could not settle a routed reply's outcome",
			"error", err, "installation_id", p.instID, "task_id", p.taskID)
		return
	}
	if held {
		return // somebody took it; the replica that did is what counts it
	}
	r.mx().RecordOutboundDropped(string(dropNoConnection))
	r.logger.WarnContext(ctx, "wecom outbound: reply not delivered",
		"reason", string(dropNoConnection),
		"chat_session_id", p.sessionID,
		"installation_id", p.instID,
		"task_id", p.taskID,
		"detail", "routed to the replicas and none took the delivery: either no replica "+
			"held a live connection, or the one that did could not admit the frame "+
			"(see outbound_relay_shed_total)")
}

// awaitOutcome enrolls a routed reply for the check above. Never blocks: the
// caller is a bus subscriber's goroutine.
func (r *RelayOutbound) awaitOutcome(f relayFrame, eventID string) {
	if r.dedupe == nil {
		return
	}
	select {
	case r.verify <- pendingOutcome{
		key:       dedupeKey(eventID),
		sessionID: f.SessionID,
		instID:    f.InstallationID,
		taskID:    f.TaskID,
		dueAt:     time.Now().Add(r.outcomeGrace()),
	}:
	default:
		r.logger.Warn("wecom relay: outcome watch full, a routed reply's fate will go unrecorded",
			"installation_id", f.InstallationID, "task_id", f.TaskID)
	}
}

// WithRelay attaches the cross-replica router to the subscriber. Without it the
// subscriber keeps the behaviour it had: a reply produced off-lease is dropped
// where it stands.
func WithRelay(r *RelayOutbound) OutboundOption {
	return func(o *Outbound) { o.relay = r }
}

// relayEventID is the id every claim is keyed on. Derived from the turn rather
// than minted, so a republish of the same completion — a retry of the publish,
// a replayed stream entry, a second subscriber — is the same claim and cannot
// become a second message in the chat.
func relayEventID(e events.Event, taskID pgtype.UUID) string {
	return "wecom:" + e.Type + ":" + util.UUIDToString(taskID)
}

// relayInboxEventID is the same rule for an inbox push, which has no task.
func relayInboxEventID(itemID, recipientID string) string {
	return "wecom:inbox:" + itemID + ":" + recipientID
}

// deliverRelayed performs a delivery published by another replica. It is the
// relayHandler half, and it runs on a dispatcher worker — never on the shard
// read loop.
//
// The frame carries identifiers, not payloads, for anything readable here: the
// attachment rows are fetched by this replica, which is the one that can send
// them, and are never shipped through Redis.
func (o *Outbound) deliverRelayed(ctx context.Context, f relayFrame) deliveryOutcome {
	instID, err := util.ParseUUID(f.InstallationID)
	if err != nil || !instID.Valid {
		return outcomeDone // unaddressable; a retry cannot make it addressable
	}
	if o.senders == nil {
		return outcomeNotOurs
	}
	sender := o.senders.get(instID)
	if sender == nil {
		return outcomeNotOurs // the replica holding the lease will take it
	}
	if f.Content != "" {
		if err := sender.sendTextCtx(ctx, f.ChatID, f.ChatType, f.Content); err != nil {
			// The reply counters are for AGENT REPLIES — their documented
			// unit. An inbox push routed here must not move them: the same
			// push would otherwise count as a delivered reply, a dropped
			// reply, or nothing at all depending on which replica happened to
			// hold the socket, and the delivered/dropped ratio would track
			// socket placement instead of outcomes.
			if f.Kind == relayKindReply {
				if reason := unconfirmedReason(err); reason != "" {
					o.unconfirmedFor(ctx, f.SessionID, f.Kind, reason, err)
				} else {
					o.droppedFor(ctx, f.SessionID, f.Kind, classifyDrop(err), err)
				}
			} else {
				o.logger.WarnContext(ctx, "wecom relay: inbox push failed on the lease holder",
					"error", err, "installation_id", f.InstallationID)
			}
			// Only a failure PROVEN to precede the write releases the claim.
			// Everything past that point — an attempted write, a verdict that
			// never came, a context that expired while waiting — may have
			// reached the peer, and releasing the claim there turns a retry
			// into a duplicate answer in the user's chat.
			if provablyNotSent(err) {
				return outcomeProvablyNotSent
			}
			return outcomeDone
		}
		if f.Kind == relayKindReply {
			o.delivered()
		}
	}
	if f.Kind == relayKindInbox {
		return outcomeDone
	}
	if f.CarriesFiles {
		o.deliverAttachmentsByID(f.MessageID, f.WorkspaceID, attachmentTarget{
			InstallationID: instID,
			ChatID:         f.ChatID,
			ChatType:       f.ChatType,
			SessionID:      f.SessionID,
		}, f.Content == "")
	}
	return outcomeDone
}

// ownsSocket is the pre-claim ownership gate. Cheap by design: one map read.
func (o *Outbound) ownsSocket(installationID string) bool {
	if o.senders == nil {
		return false
	}
	id, err := util.ParseUUID(installationID)
	if err != nil || !id.Valid {
		return false
	}
	return o.senders.get(id) != nil
}

// provablyNotSent reports whether a send error is one that certainly occurred
// before any byte could leave. ws_sender marks the boundary itself: a failure
// raised by the write is wrapped in errWriteAttempted, a missing verdict is
// errAckTimeout, and a stated refusal is a *wecomAPIError — all three mean the
// peer may have (or, for a refusal, definitely did) see the frame. A bare
// context error is ambiguous — request() returns one both from its pre-write
// check and from the post-write wait — so it is treated as possibly sent,
// which costs an un-retried delivery rather than a duplicate.
func provablyNotSent(err error) bool {
	var apiErr *wecomAPIError
	switch {
	case err == nil:
		return false
	case errors.As(err, &apiErr):
		return false
	case errors.Is(err, errAckTimeout):
		return false
	case errors.Is(err, errWriteAttempted):
		return false
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return false
	default:
		return true
	}
}

var _ relayHandler = (*Outbound)(nil)

// itemIDOf is the inbox item's own id, which is what makes a routed push
// idempotent: two replicas reading the same replayed frame key their claims on
// the same notification rather than on the moment they read it.
func itemIDOf(item map[string]any) string {
	if s, _ := item["id"].(string); s != "" {
		return s
	}
	// Older payloads carry no id. Falling back to the type plus the issue it
	// belongs to is weaker than an id but still stable for one notification,
	// which is what the claim needs.
	t, _ := item["type"].(string)
	ref, _ := item["issue_id"].(string)
	return t + ":" + ref
}
