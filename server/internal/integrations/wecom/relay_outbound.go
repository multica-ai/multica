package wecom

// relay_outbound.go — how a reply reaches the replica that can send it.
//
// WeCom is the one channel with no outbound REST path: every write goes over
// the aibot WebSocket, and the WS lease means exactly one replica holds it.
// chat:done, meanwhile, is published on the in-process events.Bus by whichever
// replica served the daemon's POST /tasks/{id}/complete — a load-balancer
// decision with no relation to the lease. Off-lease, outbound.go used to have
// nothing to do but drop the reply (GH #7215, #6890; SELF_HOSTING.md states
// the resulting single-replica constraint).
//
// This routes it instead, over the Redis Stream relay the repository already
// runs for exactly this shape of problem: daemonws.RelayNotifier wakes a
// daemon that is connected to some other replica by publishing under
// ScopeDaemonRuntime, and every node's XREAD loop hands the frame to whoever
// holds that connection. ScopeWecomOutbound is the same idea with the
// installation id as the scope.
//
// WHY IT NEEDS NO DEDUPLICATION LEDGER: the guard on the receiving side is
// "do I hold this installation's socket", and the lease guarantees at most one
// replica can answer yes. The publisher only reaches here after finding its own
// registry empty, so the copy that comes back to it is ignored by the same
// test. A lease that moves between publish and consume is the one window where
// two replicas could both answer yes, and the bounded seen-set below closes it.
//
// WHAT IT DOES NOT DO: the relay's consumers are tail-followers (XREAD from a
// replay anchor, advancing lastID), not a consumer group with pending-entry
// reclaim. If NO replica holds the socket at that moment — every one of them
// mid-reconnect — the frame is read by nobody and the reply is still lost.
// That window is the durable-queue problem, and it is deliberately not solved
// here: routing and durability are two problems, and conflating them is what
// made the outbound queue too large to land twice over.

import (
	"encoding/json"
	"log/slog"
	"sync"

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

// relayFrame is one reply in transit between replicas. Deliberately the
// rendered address and text rather than an event id to look up: the replica
// that receives it must be able to send without another database round trip,
// and by the time it arrives the turn is already finished and immutable.
type relayFrame struct {
	InstallationID string `json:"installation_id"`
	ChatID         string `json:"chat_id"`
	ChatType       int    `json:"chat_type"`
	Content        string `json:"content"`
	TaskID         string `json:"task_id"`
}

// seenEvents is a bounded set of relay event ids this process has already
// acted on. Bounded because it must not grow without limit on a long-lived
// process, and small because the only thing it protects against is a lease
// that moved within one delivery.
type seenEvents struct {
	mu    sync.Mutex
	ids   map[string]struct{}
	order []string
	limit int
}

func newSeenEvents(limit int) *seenEvents {
	return &seenEvents{ids: make(map[string]struct{}, limit), limit: limit}
}

// claim reports whether this is the first time the caller has seen id. An
// empty id is always claimable: without one there is nothing to deduplicate on,
// and refusing would drop the reply outright.
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

// RelayOutbound publishes a reply to the replica holding the bot's socket, and
// delivers the ones other replicas publish. One instance per process; boot
// registers it as the relay's WecomOutboundDeliverer.
type RelayOutbound struct {
	senders   *sendersRegistry
	publisher relayPublisher
	logger    *slog.Logger
	metrics   Metrics
	seen      *seenEvents
}

// NewRelayOutbound builds the cross-replica router. A nil publisher turns
// publishing off and leaves delivery working, which is what a single-replica
// deployment with no REDIS_URL gets: it never needs to publish, because the
// replica that produced the reply is the one holding the socket.
func NewRelayOutbound(senders *sendersRegistry, publisher relayPublisher, logger *slog.Logger, metrics Metrics) *RelayOutbound {
	if logger == nil {
		logger = slog.Default()
	}
	return &RelayOutbound{
		senders:   senders,
		publisher: publisher,
		logger:    logger,
		metrics:   orNopMetrics(metrics),
		seen:      newSeenEvents(4096),
	}
}

// publish hands a reply to the other replicas. Reports whether it went out, so
// the caller can tell "routed, somebody else will send it" from "nowhere to
// route it to" and count them differently.
func (r *RelayOutbound) publish(f relayFrame, eventID string) bool {
	if r == nil || r.publisher == nil {
		return false
	}
	body, err := json.Marshal(f)
	if err != nil {
		r.logger.Warn("wecom relay: marshal outbound frame", "error", err)
		return false
	}
	// Claimed before publishing, not on arrival: this process is also a
	// consumer of its own stream, and it has already decided it cannot send.
	r.seen.claim(eventID)
	if err := r.publisher.PublishWithID(realtime.ScopeWecomOutbound, f.InstallationID, "", body, eventID); err != nil {
		r.logger.Warn("wecom relay: publish failed",
			"error", err, "installation_id", f.InstallationID, "task_id", f.TaskID)
		return false
	}
	return true
}

// DeliverWecomOutbound is the realtime.WecomOutboundDeliverer side. Every
// replica receives every frame; this one sends only if it holds the socket.
func (r *RelayOutbound) DeliverWecomOutbound(scopeID string, frame []byte, eventID string) {
	if r == nil || r.senders == nil {
		return
	}
	if !r.seen.claim(eventID) {
		return
	}
	var f relayFrame
	if err := json.Unmarshal(frame, &f); err != nil {
		r.logger.Warn("wecom relay: undecodable frame", "error", err, "installation_id", scopeID)
		return
	}
	instID, err := util.ParseUUID(f.InstallationID)
	if err != nil || !instID.Valid {
		return
	}
	sender := r.senders.get(instID)
	if sender == nil {
		return // not ours; the replica holding the lease will take it
	}
	// One frame, the same way processEvent sends one: this path is a change of
	// route, not of wire format, and anything it did differently would be a
	// second definition of what a reply looks like.
	if err := sender.sendText(f.ChatID, f.ChatType, f.Content); err != nil {
		r.logger.Warn("wecom relay: delivery failed on the lease holder",
			"error", err, "installation_id", f.InstallationID, "task_id", f.TaskID)
		r.metrics.RecordOutboundDropped(string(classifyDrop(err)))
		return
	}
	r.metrics.RecordOutboundDelivered()
	r.logger.Debug("wecom relay: delivered a reply published on another replica",
		"installation_id", f.InstallationID, "task_id", f.TaskID)
}

var _ realtime.WecomOutboundDeliverer = (*RelayOutbound)(nil)

// WithRelay attaches the cross-replica router. Without it the subscriber keeps
// the behaviour it had: a reply produced off-lease is dropped where it stands.
func WithRelay(r *RelayOutbound) OutboundOption {
	return func(o *Outbound) { o.relay = r }
}

// relayEventID is the id the relay deduplicates on. Derived from the turn
// rather than minted, so a republish of the same completion — a retry of the
// publish, a second subscriber — cannot become a second message in the chat.
func relayEventID(e events.Event, taskID pgtype.UUID) string {
	return "wecom:" + e.Type + ":" + util.UUIDToString(taskID)
}
