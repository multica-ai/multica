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
// ORDERING: queues are sharded by installation, so two replies to the same bot
// keep their order while unrelated bots cannot block each other.

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

// relayShards is how many independent queues carry frames. Per-installation
// ordering comes from hashing onto one of them; the count bounds how many
// unhealthy bots it takes to occupy every worker.
const relayShards = 8

// relayQueueDepth is per shard. Deep enough to absorb a replay burst, shallow
// enough that a wedged bot sheds rather than hoards memory.
const relayQueueDepth = 256

// relayDrainBudget bounds the WHOLE post-shutdown drain, not one delivery.
// Without it a full shard could extend shutdown by queue-depth sequential
// ack waits; with it, whatever is not delivered inside the budget is left to
// the claim TTL and the next replica's replay window.
const relayDrainBudget = 10 * time.Second

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
// that re-enqueues, bounded and backed off, until the lease settles.
const (
	relayMaxAttempts  = 5
	relayRetryBackoff = 200 * time.Millisecond
)

// RelayOutbound publishes a delivery to the replica holding the bot's socket,
// and performs the ones other replicas publish. One per process.
type RelayOutbound struct {
	publisher relayPublisher
	dedupe    DedupeStore
	dedupeTTL time.Duration
	logger    *slog.Logger
	seen      *seenEvents

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
func NewRelayOutbound(publisher relayPublisher, dedupe DedupeStore, replayGrace time.Duration, logger *slog.Logger) *RelayOutbound {
	if logger == nil {
		logger = slog.Default()
	}
	r := &RelayOutbound{
		publisher: publisher,
		dedupe:    dedupe,
		dedupeTTL: dedupeTTLFor(replayGrace),
		logger:    logger,
		seen:      newSeenEvents(4096),
		ready:     make(chan struct{}),
		queues:    make([]chan queued, relayShards),
	}
	for i := range r.queues {
		r.queues[i] = make(chan queued, relayQueueDepth)
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
		go func(q chan queued) {
			defer r.wg.Done()
			select {
			case <-r.ready:
			case <-ctx.Done():
				// Cancelled before (or while) the handler arrived. If it IS
				// already attached — the select picks between two ready
				// channels arbitrarily — what is queued can still be drained;
				// without it there is nothing a drain could deliver with.
				select {
				case <-r.ready:
				default:
					return
				}
			}
			for {
				select {
				case item := <-q:
					// The select is fair, so an item can win against an
					// already-fired ctx.Done. Performing it on the cancelled
					// context would misfile it as a dedupe outage; it belongs
					// to the drain.
					if ctx.Err() != nil {
						r.drainRemaining(ctx, q, &item)
						return
					}
					r.perform(ctx, item)
				case <-ctx.Done():
					r.drainRemaining(ctx, q, nil)
					return
				}
			}
		}(r.queues[i])
	}
}

// drainRemaining performs what is already queued so a graceful shutdown does
// not strand a reply — under ONE bounded budget for the whole drain, so a full
// shard cannot stack sequential ack waits. Deliveries started near the edge
// inherit the deadline and abort with it. first is an item a worker had
// already taken off the queue when the cancel won the race.
func (r *RelayOutbound) drainRemaining(ctx context.Context, q chan queued, first *queued) {
	drainCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), relayDrainBudget)
	defer cancel()
	if first != nil {
		r.perform(drainCtx, *first)
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
	case r.queues[shardFor(f.InstallationID)] <- queued{frame: f, eventID: eventID}:
	default:
		// Shed rather than stall the shard. Counted, because a reply nobody
		// gets is exactly what this whole path exists to stop being silent.
		r.mx().RecordOutboundDropped(string(dropRelayOverflow))
		r.logger.Warn("wecom relay: dispatch queue full, shedding a routed reply",
			"installation_id", f.InstallationID, "task_id", f.TaskID)
	}
}

func shardFor(installationID string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(installationID))
	return int(h.Sum32() % uint32(relayShards))
}

// perform runs one delivery under an at-most-once claim.
func (r *RelayOutbound) perform(ctx context.Context, item queued) {
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
		return
	}
	if !r.seen.claim(item.eventID) {
		return
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
			r.requeue(ctx, item)
			return
		}
		if !won {
			// Losing the claim does NOT mean somebody else will deliver. In a
			// mid-flight lease move the loser here can be the replica that
			// NOW holds the socket, while the winner is about to discover it
			// no longer does and release — and each replica reads a stream
			// frame exactly once, so nothing external ever hands it back.
			// The loser therefore checks again, bounded and backed off; the
			// common case (the claim is held because the reply was
			// delivered) burns out at relayMaxAttempts and stops.
			r.seen.forget(item.eventID)
			r.requeue(ctx, item)
			return
		}
	}
	outcome := r.handler.deliverRelayed(ctx, item.frame)
	if outcome == outcomeDone {
		return
	}
	// Not ours, or provably never written: give the claim back — and retry
	// locally too, because release alone wakes nobody (see the !won comment).
	r.seen.forget(item.eventID)
	if r.dedupe != nil {
		r.dedupe.Release(ctx, key)
	}
	r.requeue(ctx, item)
}

// requeue schedules one more attempt at item, after a backoff that doubles per
// attempt, without ever blocking the worker. It gives up quietly past
// relayMaxAttempts: by far the likeliest reason to exhaust the budget is a
// claim held because another replica already delivered, and counting that as
// a drop would be false.
func (r *RelayOutbound) requeue(ctx context.Context, item queued) {
	if item.attempts+1 >= relayMaxAttempts {
		r.logger.Warn("wecom relay: giving up on a routed delivery after retries",
			"installation_id", item.frame.InstallationID, "task_id", item.frame.TaskID,
			"attempts", item.attempts+1)
		return
	}
	item.attempts++
	delay := relayRetryBackoff << (item.attempts - 1)
	time.AfterFunc(delay, func() {
		if ctx.Err() != nil {
			return // shutting down; the claim TTL and the next replay own it now
		}
		select {
		case r.queues[shardFor(item.frame.InstallationID)] <- item:
		default:
			r.mx().RecordOutboundDropped(string(dropRelayOverflow))
			r.logger.Warn("wecom relay: retry shed, dispatch queue full",
				"installation_id", item.frame.InstallationID, "task_id", item.frame.TaskID)
		}
	})
}

func dedupeKey(eventID string) string { return "wecom:outbound:claim:" + eventID }

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
