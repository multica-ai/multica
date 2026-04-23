package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/redis/go-redis/v9"
)

// Stream / registry key naming. Centralised so tests can introspect.
func StreamKey(scopeType, scopeID string) string {
	return fmt.Sprintf("ws:scope:%s:%s:stream", scopeType, scopeID)
}
func NodesKey(scopeType, scopeID string) string {
	return fmt.Sprintf("ws:scope:%s:%s:nodes", scopeType, scopeID)
}
func HeartbeatKey(nodeID string) string {
	return fmt.Sprintf("ws:node:%s:heartbeat", nodeID)
}

const (
	streamMaxLen        int64 = 10000
	heartbeatTTL              = 90 * time.Second
	heartbeatPeriod           = 30 * time.Second
	consumerIdleGrace         = 10 * time.Minute
	consumerSweepPeriod       = 5 * time.Minute
)

// envelope is what we serialise into each XADD message. It is opaque to the
// hub: the relay decodes payload_json before fanning out.
type envelope struct {
	EventID     string `json:"event_id"`
	EventType   string `json:"event_type"`
	Scope       string `json:"scope"`
	ScopeID     string `json:"scope_id"`
	WorkspaceID string `json:"workspace_id"`
	ActorID     string `json:"actor_id"`
	CreatedAt   string `json:"created_at"`
	NodeID      string `json:"node_id"`
	PayloadJSON string `json:"payload_json"` // raw JSON of the original ws frame
}

// RedisRelay is a Broadcaster implementation that writes every message to a
// per-scope Redis Stream and consumes streams for which there are local
// subscribers. Local fanout is delegated to the wrapped *Hub.
type RedisRelay struct {
	hub    *Hub
	rdb    *redis.Client
	nodeID string

	mu        sync.Mutex
	consumers map[scopeKey]*scopeConsumer
}

type scopeConsumer struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// NewRedisRelay constructs a relay. The caller is responsible for invoking
// Start before producing messages.
func NewRedisRelay(hub *Hub, rdb *redis.Client) *RedisRelay {
	return &RedisRelay{
		hub:       hub,
		rdb:       rdb,
		nodeID:    ulid.Make().String(),
		consumers: make(map[scopeKey]*scopeConsumer),
	}
}

// NodeID returns this relay's randomly-assigned node identifier.
func (r *RedisRelay) NodeID() string { return r.nodeID }

// Start wires the hub→relay subscription callbacks, kicks off the heartbeat
// goroutine, and spins up consumers for any scopes the hub already knows
// about. ctx controls all background goroutines: cancelling it shuts the
// relay down.
func (r *RedisRelay) Start(ctx context.Context) {
	M.NodeID.Store(r.nodeID)
	if err := r.rdb.Ping(ctx).Err(); err != nil {
		slog.Error("realtime/redis: initial ping failed", "error", err)
		M.RedisConnected.Store(false)
		M.SetRedisLastError(err.Error())
	} else {
		M.RedisConnected.Store(true)
	}

	r.hub.SetSubscriptionCallbacks(
		func(scopeType, scopeID string) { r.startConsumer(ctx, scopeType, scopeID) },
		func(scopeType, scopeID string) { r.stopConsumer(scopeType, scopeID) },
	)

	for _, key := range r.hub.LocalScopes() {
		r.startConsumer(ctx, key.Type, key.ID)
	}

	go r.heartbeatLoop(ctx)
	go r.consumerSweeper(ctx)
}

// BroadcastToScope publishes message into the scope's Redis stream. The
// envelope contains an event_id for client-side dedup. Local fanout happens
// when this node consumes its own write back through XREADGROUP — except in
// the dual-write configuration where the local hub is invoked directly.
func (r *RedisRelay) BroadcastToScope(scopeType, scopeID string, message []byte) {
	r.publish(scopeType, scopeID, "", message)
}

// BroadcastToWorkspace / SendToUser / Broadcast satisfy the back-compat
// portion of Broadcaster.
func (r *RedisRelay) BroadcastToWorkspace(workspaceID string, message []byte) {
	r.publish(ScopeWorkspace, workspaceID, "", message)
}
func (r *RedisRelay) SendToUser(userID string, message []byte, excludeWorkspace ...string) {
	exclude := ""
	if len(excludeWorkspace) > 0 {
		exclude = excludeWorkspace[0]
	}
	r.publish(ScopeUser, userID, exclude, message)
}
func (r *RedisRelay) Broadcast(message []byte) {
	// Daemon broadcast — write to a special "global" stream so other nodes
	// can fan out to all clients regardless of subscriptions.
	r.publish("global", "all", "", message)
}

func (r *RedisRelay) publish(scopeType, scopeID, exclude string, frame []byte) {
	ev := envelope{
		EventID:     ulid.Make().String(),
		Scope:       scopeType,
		ScopeID:     scopeID,
		NodeID:      r.nodeID,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		PayloadJSON: string(frame),
	}
	if exclude != "" {
		ev.WorkspaceID = exclude
	}
	// Best-effort: peek inside the JSON for event_type / actor_id.
	if t, a := peekTypeActor(frame); t != "" {
		ev.EventType = t
		ev.ActorID = a
	}

	args := &redis.XAddArgs{
		Stream: StreamKey(scopeType, scopeID),
		MaxLen: streamMaxLen,
		Approx: true,
		Values: map[string]any{
			"event_id":     ev.EventID,
			"event_type":   ev.EventType,
			"scope":        ev.Scope,
			"scope_id":     ev.ScopeID,
			"workspace_id": ev.WorkspaceID,
			"actor_id":     ev.ActorID,
			"created_at":   ev.CreatedAt,
			"node_id":      ev.NodeID,
			"payload_json": ev.PayloadJSON,
		},
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := r.rdb.XAdd(ctx, args).Err(); err != nil {
		M.RedisXAddErrors.Add(1)
		M.SetRedisLastError(err.Error())
		slog.Warn("realtime/redis: XADD failed", "error", err, "scope", scopeType, "scope_id", scopeID)
		return
	}
	M.RedisXAddTotal.Add(1)
	M.RedisLastXAddLagMicros.Store(time.Since(start).Microseconds())
}

// startConsumer kicks off a single per-scope XREADGROUP loop if not already
// running.
func (r *RedisRelay) startConsumer(parent context.Context, scopeType, scopeID string) {
	key := sk(scopeType, scopeID)
	r.mu.Lock()
	if _, exists := r.consumers[key]; exists {
		r.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	c := &scopeConsumer{cancel: cancel, done: make(chan struct{})}
	r.consumers[key] = c
	r.mu.Unlock()

	go r.runConsumer(ctx, c, scopeType, scopeID)
}

func (r *RedisRelay) stopConsumer(scopeType, scopeID string) {
	key := sk(scopeType, scopeID)
	r.mu.Lock()
	c, ok := r.consumers[key]
	if ok {
		delete(r.consumers, key)
	}
	r.mu.Unlock()
	if !ok {
		return
	}
	c.cancel()
}

func (r *RedisRelay) runConsumer(ctx context.Context, c *scopeConsumer, scopeType, scopeID string) {
	defer close(c.done)

	stream := StreamKey(scopeType, scopeID)
	group := "node:" + r.nodeID
	consumerName := r.nodeID

	// MKSTREAM ensures the stream exists. Ignore BUSYGROUP.
	createCtx, createCancel := context.WithTimeout(ctx, 2*time.Second)
	if err := r.rdb.XGroupCreateMkStream(createCtx, stream, group, "$").Err(); err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		slog.Warn("realtime/redis: XGROUP CREATE failed", "error", err, "scope", scopeType, "scope_id", scopeID)
	}
	createCancel()

	// Register ourselves as a node interested in this scope.
	regCtx, regCancel := context.WithTimeout(ctx, 2*time.Second)
	r.rdb.ZAdd(regCtx, NodesKey(scopeType, scopeID), redis.Z{Score: float64(time.Now().Add(heartbeatTTL).Unix()), Member: r.nodeID})
	regCancel()

	for {
		if ctx.Err() != nil {
			break
		}
		readCtx, readCancel := context.WithTimeout(ctx, 6*time.Second)
		res, err := r.rdb.XReadGroup(readCtx, &redis.XReadGroupArgs{
			Group:    group,
			Consumer: consumerName,
			Streams:  []string{stream, ">"},
			Count:    32,
			Block:    5 * time.Second,
		}).Result()
		readCancel()
		if errors.Is(err, redis.Nil) || (err != nil && (errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled))) {
			continue
		}
		if err != nil {
			M.RedisXReadErrors.Add(1)
			M.SetRedisLastError(err.Error())
			// Brief backoff to avoid busy-looping on a flapping connection.
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		for _, s := range res {
			for _, msg := range s.Messages {
				M.RedisXReadTotal.Add(1)
				r.deliverMessage(scopeType, scopeID, msg)
				ackCtx, ackCancel := context.WithTimeout(ctx, time.Second)
				if err := r.rdb.XAck(ackCtx, stream, group, msg.ID).Err(); err != nil {
					slog.Debug("realtime/redis: XACK failed", "error", err, "id", msg.ID)
				} else {
					M.RedisAckTotal.Add(1)
				}
				ackCancel()
			}
		}
	}

	// Best-effort consumer cleanup.
	cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 2*time.Second)
	r.rdb.XGroupDelConsumer(cleanCtx, stream, group, consumerName)
	cleanCancel()
}

func (r *RedisRelay) deliverMessage(scopeType, scopeID string, msg redis.XMessage) {
	payloadAny, ok := msg.Values["payload_json"]
	if !ok {
		return
	}
	payload, _ := payloadAny.(string)
	if payload == "" {
		return
	}
	exclude, _ := msg.Values["workspace_id"].(string)
	eventID, _ := msg.Values["event_id"].(string)

	// Inject event_id into the outgoing frame for client-side dedup.
	frame := injectEventID([]byte(payload), eventID)

	switch scopeType {
	case "global":
		r.hub.fanoutAllDedup(frame, "", eventID)
	case ScopeUser:
		r.hub.fanoutUser(scopeID, frame, exclude, eventID)
	default:
		r.hub.BroadcastToScopeDedup(scopeType, scopeID, frame, eventID)
	}
}

// fanoutUser is implemented in hub.go.

func (r *RedisRelay) heartbeatLoop(ctx context.Context) {
	t := time.NewTicker(heartbeatPeriod)
	defer t.Stop()
	for {
		r.heartbeatOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (r *RedisRelay) heartbeatOnce(ctx context.Context) {
	hbCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := r.rdb.Set(hbCtx, HeartbeatKey(r.nodeID), time.Now().UTC().Format(time.RFC3339Nano), heartbeatTTL).Err(); err != nil {
		M.RedisConnected.Store(false)
		M.SetRedisLastError(err.Error())
		return
	}
	M.RedisConnected.Store(true)
	expiry := float64(time.Now().Add(heartbeatTTL).Unix())
	for _, key := range r.hub.LocalScopes() {
		r.rdb.ZAdd(hbCtx, NodesKey(key.Type, key.ID), redis.Z{Score: expiry, Member: r.nodeID})
	}
}

// consumerSweeper periodically drops stale ZSET entries (nodes whose TTL
// expired). Best-effort: we only sweep the scopes this node currently has
// local subscribers for, since they're the only ones we can reason about
// without scanning all keys.
func (r *RedisRelay) consumerSweeper(ctx context.Context) {
	t := time.NewTicker(consumerSweepPeriod)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		now := float64(time.Now().Unix())
		for _, key := range r.hub.LocalScopes() {
			swCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			r.rdb.ZRemRangeByScore(swCtx, NodesKey(key.Type, key.ID), "-inf", fmt.Sprintf("%f", now))
			cancel()
		}
	}
}

// peekTypeActor parses the WS frame just enough to lift event_type / actor_id
// for the envelope. Failures yield empty strings — the envelope still works.
func peekTypeActor(frame []byte) (string, string) {
	var probe struct {
		Type    string `json:"type"`
		ActorID string `json:"actor_id"`
	}
	_ = json.Unmarshal(frame, &probe)
	return probe.Type, probe.ActorID
}

// injectEventID inserts the event_id field into an existing JSON object frame
// without re-encoding the payload. The frame must be a JSON object.
func injectEventID(frame []byte, eventID string) []byte {
	if eventID == "" || len(frame) == 0 || frame[0] != '{' {
		return frame
	}
	// Decode-encode round-trip is simplest and avoids edge cases with
	// trailing whitespace / nested escapes. A few extra allocations per
	// message are fine relative to the network cost.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(frame, &obj); err != nil {
		return frame
	}
	if _, exists := obj["event_id"]; exists {
		return frame
	}
	idJSON, _ := json.Marshal(eventID)
	obj["event_id"] = idJSON
	out, err := json.Marshal(obj)
	if err != nil {
		return frame
	}
	return out
}

// DualWriteBroadcaster delivers each message both to the local hub (immediate
// fanout) AND to the Redis relay (cross-node fanout). It dedups via
// Client.markSeen so the same client doesn't see the same event twice when
// the Redis relay loops the message back.
type DualWriteBroadcaster struct {
	local *Hub
	relay *RedisRelay
}

func NewDualWriteBroadcaster(local *Hub, relay *RedisRelay) *DualWriteBroadcaster {
	return &DualWriteBroadcaster{local: local, relay: relay}
}

func (d *DualWriteBroadcaster) BroadcastToScope(scopeType, scopeID string, message []byte) {
	id := ulid.Make().String()
	frame := injectEventID(message, id)
	// Local fast path: BroadcastToScopeDedup marks each client as having
	// seen `id`, so the Redis loopback for the same id will be ignored.
	d.local.BroadcastToScopeDedup(scopeType, scopeID, frame, id)
	d.relay.publishWithID(scopeType, scopeID, "", message, id)
}

func (d *DualWriteBroadcaster) BroadcastToWorkspace(workspaceID string, message []byte) {
	d.BroadcastToScope(ScopeWorkspace, workspaceID, message)
}

func (d *DualWriteBroadcaster) SendToUser(userID string, message []byte, excludeWorkspace ...string) {
	exclude := ""
	if len(excludeWorkspace) > 0 {
		exclude = excludeWorkspace[0]
	}
	id := ulid.Make().String()
	frame := injectEventID(message, id)
	d.local.fanoutUser(userID, frame, exclude, id)
	d.relay.publishWithID(ScopeUser, userID, exclude, message, id)
}

func (d *DualWriteBroadcaster) Broadcast(message []byte) {
	id := ulid.Make().String()
	frame := injectEventID(message, id)
	d.local.fanoutAllDedup(frame, "", id)
	d.relay.publishWithID("global", "all", "", message, id)
}

// publishWithID is like publish but uses a caller-supplied event id so the
// dual-write path can dedup.
func (r *RedisRelay) publishWithID(scopeType, scopeID, exclude string, frame []byte, id string) {
	ev := envelope{
		EventID:     id,
		Scope:       scopeType,
		ScopeID:     scopeID,
		NodeID:      r.nodeID,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		PayloadJSON: string(frame),
	}
	if exclude != "" {
		ev.WorkspaceID = exclude
	}
	if t, a := peekTypeActor(frame); t != "" {
		ev.EventType = t
		ev.ActorID = a
	}
	args := &redis.XAddArgs{
		Stream: StreamKey(scopeType, scopeID),
		MaxLen: streamMaxLen,
		Approx: true,
		Values: map[string]any{
			"event_id":     ev.EventID,
			"event_type":   ev.EventType,
			"scope":        ev.Scope,
			"scope_id":     ev.ScopeID,
			"workspace_id": ev.WorkspaceID,
			"actor_id":     ev.ActorID,
			"created_at":   ev.CreatedAt,
			"node_id":      ev.NodeID,
			"payload_json": ev.PayloadJSON,
		},
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := r.rdb.XAdd(ctx, args).Err(); err != nil {
		M.RedisXAddErrors.Add(1)
		M.SetRedisLastError(err.Error())
		return
	}
	M.RedisXAddTotal.Add(1)
	M.RedisLastXAddLagMicros.Store(time.Since(start).Microseconds())
}

var _ Broadcaster = (*RedisRelay)(nil)
var _ Broadcaster = (*DualWriteBroadcaster)(nil)
